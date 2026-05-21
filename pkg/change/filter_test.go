package change

import (
	"slices"
	"testing"

	"github.com/buroa/fluxrr/pkg/manifest"
)

// emptyLister is enough for filter resolution tests where transitiveDeps
// would otherwise fail to find the parent KS in a real store.
type emptyLister struct{}

func (emptyLister) GetObject(manifest.NamedResource) manifest.BaseManifest { return nil }
func (emptyLister) ListObjects(string) []manifest.BaseManifest             { return nil }

// mapLister returns canned objects from a map for transitive-deps testing.
type mapLister map[manifest.NamedResource]manifest.BaseManifest

func (m mapLister) GetObject(id manifest.NamedResource) manifest.BaseManifest { return m[id] }
func (m mapLister) ListObjects(kind string) []manifest.BaseManifest {
	out := make([]manifest.BaseManifest, 0, len(m))
	for id, obj := range m {
		if id.Kind == kind {
			out = append(out, obj)
		}
	}
	return out
}

func TestFilter_DisabledKeepsEverything(t *testing.T) {
	var f Filter
	id := manifest.NamedResource{Kind: manifest.KindHelmRelease, Namespace: "ns", Name: "x"}
	if !f.ShouldReconcile(id) {
		t.Fatal("disabled filter must keep everything")
	}
}

func TestFilter_ResolveDirectMatch(t *testing.T) {
	hr := manifest.NamedResource{Kind: manifest.KindHelmRelease, Namespace: "ns", Name: "x"}
	f := &Filter{
		Changes:     NewSet([]string{"apps/x/helmrelease.yaml"}),
		SourceFiles: map[manifest.NamedResource]string{hr: "apps/x/helmrelease.yaml"},
	}
	f.Resolve(emptyLister{})
	if !f.ShouldReconcile(hr) {
		t.Fatalf("expected direct-match keep; keep=%v", f.KeepNames())
	}
}

func TestFilter_SharedComponentPropagatesToAllConsumers(t *testing.T) {
	plex := &manifest.Kustomization{
		Name: "plex", Namespace: "media",
		Path:       "apps/media/plex/app",
		Components: []string{"../../../../components/volsync"},
	}
	atuin := &manifest.Kustomization{
		Name: "atuin", Namespace: "default",
		Path:       "apps/default/atuin/app",
		Components: []string{"../../../../components/volsync"},
	}
	hrPlex := manifest.NamedResource{Kind: manifest.KindHelmRelease, Namespace: "media", Name: "plex"}
	hrAtuin := manifest.NamedResource{Kind: manifest.KindHelmRelease, Namespace: "default", Name: "atuin"}
	ksPlex, ksAtuin := plex.Named(), atuin.Named()

	f := &Filter{
		Changes: NewSet([]string{"components/volsync/pvc.yaml"}),
		SourceFiles: map[manifest.NamedResource]string{
			ksPlex:  "apps/media/plex/app/ks.yaml",
			hrPlex:  "apps/media/plex/app/helmrelease.yaml",
			ksAtuin: "apps/default/atuin/app/ks.yaml",
			hrAtuin: "apps/default/atuin/app/helmrelease.yaml",
		},
	}
	f.Resolve(mapLister{ksPlex: plex, ksAtuin: atuin})

	for _, id := range []manifest.NamedResource{ksPlex, ksAtuin, hrPlex, hrAtuin} {
		if !f.ShouldReconcile(id) {
			t.Errorf("expected %s in keep; keep=%v", id, f.KeepNames())
		}
	}
}

func TestFilter_LongestPrefixOwnerWins(t *testing.T) {
	// A meta-KS at apps/ and a specific KS at apps/media/plex/app —
	// changes inside plex must belong to plex, not the meta-KS.
	meta := &manifest.Kustomization{Name: "cluster-apps", Namespace: "flux-system", Path: "apps"}
	plex := &manifest.Kustomization{Name: "plex", Namespace: "media", Path: "apps/media/plex/app"}
	hrPlex := manifest.NamedResource{Kind: manifest.KindHelmRelease, Namespace: "media", Name: "plex"}
	metaID, plexID := meta.Named(), plex.Named()

	f := &Filter{
		Changes: NewSet([]string{"apps/media/plex/app/helmrelease.yaml"}),
		SourceFiles: map[manifest.NamedResource]string{
			metaID: "flux/cluster/ks.yaml",
			plexID: "apps/media/plex/app/ks.yaml",
			hrPlex: "apps/media/plex/app/helmrelease.yaml",
		},
	}
	f.Resolve(mapLister{metaID: meta, plexID: plex})

	if !f.ShouldReconcile(plexID) || !f.ShouldReconcile(hrPlex) {
		t.Errorf("plex tree should be kept: %v", f.KeepNames())
	}
	if f.ShouldReconcile(metaID) {
		t.Errorf("meta KS leaked into keep set despite a deeper owner: %v", f.KeepNames())
	}
}

func TestFilter_KeepNamespaces(t *testing.T) {
	f := &Filter{
		Changes:     NewSet([]string{"a"}),
		SourceFiles: map[manifest.NamedResource]string{},
	}
	f.Keep = map[manifest.NamedResource]struct{}{
		{Kind: "K", Namespace: "ns-a", Name: "x"}:         {},
		{Kind: "K", Namespace: "ns-b", Name: "y"}:         {},
		{Kind: "K", Namespace: "", Name: "cluster-scope"}: {},
	}
	ns := f.KeepNamespaces()
	got := make([]string, 0, len(ns))
	for k := range ns {
		got = append(got, k)
	}
	slices.Sort(got)
	want := []string{"ns-a", "ns-b"}
	if !slices.Equal(got, want) {
		t.Errorf("KeepNamespaces=%v want %v", got, want)
	}
}

func TestFilter_TransitiveDepsHelmRelease(t *testing.T) {
	hr := &manifest.HelmRelease{
		Name: "plex", Namespace: "media",
		Chart: manifest.HelmChart{
			RepoKind: manifest.KindOCIRepository, RepoName: "app-template", RepoNamespace: "flux-system",
		},
		ValuesFrom: []manifest.ValuesReference{
			{Kind: manifest.KindConfigMap, Name: "plex-values"},
		},
	}
	hrID := hr.Named()
	repoID := manifest.NamedResource{Kind: manifest.KindOCIRepository, Namespace: "flux-system", Name: "app-template"}
	cmID := manifest.NamedResource{Kind: manifest.KindConfigMap, Namespace: "media", Name: "plex-values"}

	f := &Filter{
		Changes:     NewSet([]string{"hr.yaml"}),
		SourceFiles: map[manifest.NamedResource]string{hrID: "hr.yaml"},
	}
	f.Resolve(mapLister{hrID: hr})

	if !f.ShouldReconcile(repoID) {
		t.Errorf("chart source not pulled in by HR; keep=%v", f.KeepNames())
	}
	if !f.ShouldReconcile(cmID) {
		t.Errorf("valuesFrom ref not pulled in by HR; keep=%v", f.KeepNames())
	}
}

func TestFilter_TransitiveDepsKustomization(t *testing.T) {
	ks := &manifest.Kustomization{
		Name: "apps", Namespace: "flux-system",
		SourceKind: manifest.KindGitRepository, SourceName: "flux-system", SourceNamespace: "flux-system",
		DependsOn: []string{"flux-system/repositories"},
	}
	ksID := ks.Named()
	gitID := manifest.NamedResource{Kind: manifest.KindGitRepository, Namespace: "flux-system", Name: "flux-system"}
	depID := manifest.NamedResource{Kind: manifest.KindKustomization, Namespace: "flux-system", Name: "repositories"}

	f := &Filter{
		Changes:     NewSet([]string{"ks.yaml"}),
		SourceFiles: map[manifest.NamedResource]string{ksID: "ks.yaml"},
	}
	f.Resolve(mapLister{ksID: ks})

	if !f.ShouldReconcile(gitID) {
		t.Errorf("sourceRef not pulled in by KS; keep=%v", f.KeepNames())
	}
	// dependsOn is reconcile-ordering only; unchanged ancestors must
	// stay OUT of the keep set so offline diffs don't render unrelated
	// trees.
	if f.ShouldReconcile(depID) {
		t.Errorf("dependsOn leaked into keep set; keep=%v", f.KeepNames())
	}
}

func TestFilter_DependsOnNotFollowed(t *testing.T) {
	// dependsOn is reconcile-ordering only. A change to `a` must not
	// drag `b` into the keep set just because `a` depends on `b`.
	a := &manifest.Kustomization{
		Name: "a", Namespace: "flux-system",
		SourceKind: manifest.KindGitRepository, SourceName: "src", SourceNamespace: "flux-system",
		DependsOn: []string{"flux-system/b"},
	}
	b := &manifest.Kustomization{Name: "b", Namespace: "flux-system"}
	aID, bID := a.Named(), b.Named()

	f := &Filter{
		Changes:     NewSet([]string{"a.yaml"}),
		SourceFiles: map[manifest.NamedResource]string{aID: "a.yaml"},
	}
	f.Resolve(mapLister{aID: a, bID: b})

	if !f.ShouldReconcile(aID) {
		t.Fatalf("a should be kept; keep=%v", f.KeepNames())
	}
	if f.ShouldReconcile(bID) {
		t.Errorf("dependsOn dragged b into keep set; keep=%v", f.KeepNames())
	}
}

func TestFilter_ShouldReconcileEmptyNamespaceFallback(t *testing.T) {
	hrLoaded := manifest.NamedResource{Kind: manifest.KindHelmRelease, Namespace: "", Name: "x"}
	hrLookup := manifest.NamedResource{Kind: manifest.KindHelmRelease, Namespace: "media", Name: "x"}
	f := &Filter{
		Changes:     NewSet([]string{"f"}),
		SourceFiles: map[manifest.NamedResource]string{hrLoaded: "f"},
	}
	f.Resolve(emptyLister{})
	// keep contains hrLoaded (namespace=""); a lookup with namespace=media
	// must still hit via the (Kind, Name) fallback index.
	if !f.ShouldReconcile(hrLookup) {
		t.Fatalf("(Kind, Name) fallback didn't match; keep=%v", f.KeepNames())
	}
}
