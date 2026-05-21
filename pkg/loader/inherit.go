package loader

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/buroa/fluxrr/pkg/manifest"
	"github.com/buroa/fluxrr/pkg/store"
)

// ApplyNamespaceInheritance fills empty metadata.namespace fields on
// loaded resources from the nearest enclosing namespace directive —
// either a Flux Kustomization's spec.targetNamespace or a
// kustomization.yaml `namespace:` field. This is the load-time analog
// of kustomize-controller's apply-time behavior, without which the
// store ends up with two copies of the same resource (one with the
// inherited namespace, one with namespace=""). repoRoot anchors the
// kustomization.yaml lookups; sourceFiles is mutated as ids are
// rewritten.
func ApplyNamespaceInheritance(s *store.Store, sourceFiles map[manifest.NamedResource]string, repoRoot string) {
	if len(sourceFiles) == 0 {
		return
	}

	// Index #1 — Flux Kustomizations with a targetNamespace.
	fluxByPath := indexFluxTargetNamespaces(s)

	// Index #2 — kustomize.yaml `namespace:` directives, keyed by
	// the directory containing the kustomization file.
	kustomizeByDir := indexKustomizeNamespaces(sourceFiles, repoRoot)

	type update struct {
		old, new manifest.NamedResource
		file     string
	}
	var updates []update
	for id, file := range sourceFiles {
		if id.Namespace != "" {
			continue
		}
		ns := resolveNamespace(file, fluxByPath, kustomizeByDir)
		if ns == "" {
			continue
		}
		next := id
		next.Namespace = ns
		updates = append(updates, update{old: id, new: next, file: file})
	}
	for _, u := range updates {
		obj := s.GetObject(u.old)
		if obj == nil {
			continue
		}
		setNamespace(obj, u.new.Namespace)
		if hr, ok := obj.(*manifest.HelmRelease); ok && hr.Chart.RepoNamespace == "" {
			// chartRef.namespace wasn't explicit in the YAML so it
			// implicitly tracks the HR's namespace; carry the new
			// namespace through.
			hr.Chart.RepoNamespace = u.new.Namespace
		}
		s.DeleteObject(u.old)
		s.AddObject(obj)
		delete(sourceFiles, u.old)
		sourceFiles[u.new] = u.file
	}
}

// pathEntry pairs a slash-suffixed directory prefix with the namespace
// to apply to anything underneath it.
type pathEntry struct {
	prefix string
	ns     string
}

// resolveNamespace returns the most-specific namespace that should
// apply to the resource at file. Flux Kustomizations win over
// kustomize.yaml directives only when their prefix is longer — the
// "deepest wins" rule.
func resolveNamespace(file string, flux, kust []pathEntry) string {
	slashFile := filepath.ToSlash(file)
	var best pathEntry
	for _, group := range [...][]pathEntry{flux, kust} {
		for _, e := range group {
			if !strings.HasPrefix(slashFile, e.prefix) {
				continue
			}
			if len(e.prefix) > len(best.prefix) {
				best = e
			}
		}
	}
	return best.ns
}

// indexFluxTargetNamespaces returns one pathEntry per Flux
// Kustomization with a non-empty targetNamespace. resolveNamespace
// already picks the longest-prefix match, so the slice can stay
// unsorted.
func indexFluxTargetNamespaces(s *store.Store) []pathEntry {
	var out []pathEntry
	for _, obj := range s.ListObjects(manifest.KindKustomization) {
		ks, ok := obj.(*manifest.Kustomization)
		if !ok || ks.TargetNamespace == "" {
			continue
		}
		out = append(out, pathEntry{
			prefix: normalizePrefix(ks.Path),
			ns:     ks.TargetNamespace,
		})
	}
	return out
}

// indexKustomizeNamespaces reads every ancestor kustomization.yaml of
// each source file and returns one pathEntry per `namespace:`
// directive. Slash-normalized dir keys match the sourceFiles
// coordinate; repoRoot anchors the on-disk reads.
func indexKustomizeNamespaces(sourceFiles map[manifest.NamedResource]string, repoRoot string) []pathEntry {
	dirs := map[string]struct{}{}
	for _, file := range sourceFiles {
		for d := path.Dir(file); d != "." && d != "/" && d != ""; d = path.Dir(d) {
			dirs[d] = struct{}{}
		}
	}
	var out []pathEntry
	for dir := range dirs {
		ns := readKustomizeNamespace(repoRoot, dir)
		if ns == "" {
			continue
		}
		out = append(out, pathEntry{prefix: strings.TrimSuffix(dir, "/") + "/", ns: ns})
	}
	return out
}

// readKustomizeNamespace returns the top-level `namespace:` value of
// a kustomization.yaml in dir (resolved relative to repoRoot), or ""
// if no kustomize file exists or the namespace key is absent.
func readKustomizeNamespace(repoRoot, dir string) string {
	for _, name := range []string{"kustomization.yaml", "kustomization.yml", "Kustomization"} {
		path := filepath.Join(repoRoot, dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc struct {
			Namespace string `yaml:"namespace"`
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			continue
		}
		return doc.Namespace
	}
	return ""
}

// normalizePrefix turns a Kustomization spec.path into a slash-
// terminated repo-relative prefix suitable for HasPrefix matching.
func normalizePrefix(p string) string {
	p = strings.TrimPrefix(p, "./")
	return strings.TrimSuffix(p, "/") + "/"
}

func setNamespace(obj manifest.BaseManifest, ns string) {
	switch o := obj.(type) {
	case *manifest.Kustomization:
		o.Namespace = ns
	case *manifest.HelmRelease:
		o.Namespace = ns
	case *manifest.HelmRepository:
		o.Namespace = ns
	case *manifest.OCIRepository:
		o.Namespace = ns
	case *manifest.GitRepository:
		o.Namespace = ns
	case *manifest.HelmChartSource:
		o.Namespace = ns
	case *manifest.ConfigMap:
		o.Namespace = ns
	case *manifest.Secret:
		o.Namespace = ns
	}
}
