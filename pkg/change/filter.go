package change

import (
	"slices"

	"github.com/buroa/fluxrr/pkg/manifest"
)

// Filter answers "should I reconcile this resource?" by checking
// against a keep-set resolved from a file-level diff. The
// orchestrator populates SourceFiles + Coverage, then calls Resolve.
type Filter struct {
	// Changes is the file-level diff from change.Detect.
	Changes *Set
	// SourceFiles maps every loaded resource to the file it came from
	// (slash-separated, relative to the repo root).
	SourceFiles map[manifest.NamedResource]string
	// RepoRoot is the absolute path that SourceFiles paths are
	// relative to. Needed to anchor on-disk kustomization.yaml
	// lookups during ownership resolution.
	RepoRoot string
	// Keep is the resolved set of resources whose work fluxrr must
	// still do.
	Keep map[manifest.NamedResource]struct{}

	// keepByName: (Kind, Name) presence set used as an O(1) fallback
	// when either side of a lookup has an empty namespace.
	keepByName map[nameKey]struct{}
}

type nameKey struct{ kind, name string }

func (f *Filter) Enabled() bool { return f != nil && f.Changes != nil }

// ShouldReconcile reports whether the controller for id should do work
// (true when filtering is disabled). Lookups tolerate an empty
// namespace on either side because parent-Kustomization targetNamespace
// inheritance is applied lazily.
func (f *Filter) ShouldReconcile(id manifest.NamedResource) bool {
	if !f.Enabled() {
		return true
	}
	if _, ok := f.Keep[id]; ok {
		return true
	}
	if id.Namespace != "" {
		if _, ok := f.keepByName[nameKey{id.Kind, id.Name}]; ok {
			return true
		}
	}
	return false
}

func (f *Filter) SourceFile(id manifest.NamedResource) string {
	if f == nil {
		return ""
	}
	return f.SourceFiles[id]
}

// Resolve expands the file-level change set into a keep-set:
//
//  1. Every resource whose source file changed is kept.
//  2. For each changed file, the most-specific Flux Kustomization that
//     owns it (longest matching spec.path, including spec.components)
//     is kept — along with every resource whose source file shares
//     the same owner.
//  3. BFS over chart sources, sourceRef, dependsOn, and valuesFrom
//     to pull in upstream dependencies.
func (f *Filter) Resolve(objs ObjectLister) {
	if !f.Enabled() {
		return
	}
	keep := make(map[manifest.NamedResource]struct{})
	var queue []manifest.NamedResource
	enqueue := func(id manifest.NamedResource) {
		if _, seen := keep[id]; seen {
			return
		}
		keep[id] = struct{}{}
		queue = append(queue, id)
	}

	owners := buildOwnership(objs, f.RepoRoot)
	ownersHit := make(map[manifest.NamedResource]struct{})

	for _, file := range f.Changes.Paths() {
		for _, owner := range owners.ownersOf(file) {
			ownersHit[owner] = struct{}{}
			enqueue(owner)
		}
	}
	for id, src := range f.SourceFiles {
		if f.Changes.Contains(src) {
			enqueue(id)
			continue
		}
		// Pull in every sibling resource that shares an affected owner.
		for _, owner := range owners.ownersOf(src) {
			if _, hit := ownersHit[owner]; hit {
				enqueue(id)
				break
			}
		}
	}

	for head := 0; head < len(queue); head++ {
		for _, d := range transitiveDeps(objs, queue[head]) {
			enqueue(d)
		}
	}
	f.Keep = keep

	f.keepByName = make(map[nameKey]struct{}, len(keep))
	for id := range keep {
		if id.Namespace == "" {
			f.keepByName[nameKey{id.Kind, id.Name}] = struct{}{}
		}
	}
}

// KeepNames returns the resolved keep-set as sorted strings for logs.
func (f *Filter) KeepNames() []string {
	if f == nil || f.Keep == nil {
		return nil
	}
	out := make([]string, 0, len(f.Keep))
	for id := range f.Keep {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return out
}

// KeepNamespaces returns the namespaces represented in the keep-set,
// or nil when no scope can be derived (disabled, empty, or
// cluster-scoped only).
func (f *Filter) KeepNamespaces() map[string]struct{} {
	if f == nil || f.Keep == nil {
		return nil
	}
	out := make(map[string]struct{})
	for id := range f.Keep {
		if id.Namespace != "" {
			out[id.Namespace] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ObjectLister is the Store surface filter resolution needs.
type ObjectLister interface {
	GetObject(manifest.NamedResource) manifest.BaseManifest
	ListObjects(kind string) []manifest.BaseManifest
}

// transitiveDeps returns the references id needs to render — chart
// sources, KS sourceRef, valuesFrom. dependsOn is intentionally
// excluded: it's a reconcile-ordering signal in real Flux, not a
// content dependency, so it adds nothing to an offline render.
// Skipped resources still get marked Ready by their controllers, so
// downstream depwait completes naturally.
func transitiveDeps(objs ObjectLister, id manifest.NamedResource) []manifest.NamedResource {
	switch id.Kind {
	case manifest.KindHelmRelease:
		hr, _ := objs.GetObject(id).(*manifest.HelmRelease)
		if hr == nil {
			return nil
		}
		out := []manifest.NamedResource{{
			Kind: hr.Chart.RepoKind, Namespace: hr.Chart.RepoNamespace, Name: hr.Chart.RepoName,
		}}
		for _, ref := range hr.ValuesFrom {
			out = append(out, manifest.NamedResource{
				Kind: ref.Kind, Namespace: hr.Namespace, Name: ref.Name,
			})
		}
		return out

	case manifest.KindKustomization:
		ks, _ := objs.GetObject(id).(*manifest.Kustomization)
		if ks == nil {
			return nil
		}
		if ks.SourceKind == "" || ks.SourceName == "" {
			return nil
		}
		return []manifest.NamedResource{{
			Kind: ks.SourceKind, Namespace: ks.SourceNamespace, Name: ks.SourceName,
		}}
	}
	return nil
}
