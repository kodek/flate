package cli

import (
	"cmp"
	"slices"
	"strings"

	"github.com/buroa/fluxrr/pkg/diff"
	"github.com/buroa/fluxrr/pkg/manifest"
	"github.com/buroa/fluxrr/pkg/orchestrator"
	"github.com/buroa/fluxrr/pkg/store"
)

// firstArg returns the first positional arg, or "" when none was given.
func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// sortRows orders rows by (namespace, name) so table output is
// deterministic across runs.
func sortRows(rows []map[string]string) {
	slices.SortFunc(rows, func(a, b map[string]string) int {
		if c := cmp.Compare(a["namespace"], b["namespace"]); c != 0 {
			return c
		}
		return cmp.Compare(a["name"], b["name"])
	})
}

// gatherArtifacts collects every rendered manifest produced by the
// stored Kustomization or HelmRelease artifacts of the given kind,
// tagged with the parent that produced them. name optionally filters
// to a single resource. When c is non-nil the namespace scope from
// commonFlags + the orchestrator's change filter is honored.
func gatherArtifacts(o *orchestrator.Orchestrator, kind, name string, c *commonFlags) []diff.Doc {
	var out []diff.Doc
	for _, obj := range o.Store().ListObjects(kind) {
		id := obj.Named()
		if name != "" && id.Name != name {
			continue
		}
		if c != nil && !c.includeNamespace(o.Filter(), id.Namespace) {
			continue
		}
		parent := diff.Parent{Kind: id.Kind, Namespace: id.Namespace, Name: id.Name}
		if ks, ok := obj.(*manifest.Kustomization); ok {
			parent.Path = strings.TrimPrefix(ks.Path, "./")
		}
		switch a := o.Store().GetArtifact(id).(type) {
		case *store.KustomizationArtifact:
			for _, m := range a.Manifests {
				out = append(out, diff.Doc{Manifest: m, Parent: parent})
			}
		case *store.HelmReleaseArtifact:
			for _, m := range a.Manifests {
				out = append(out, diff.Doc{Manifest: m, Parent: parent})
			}
		}
	}
	return out
}
