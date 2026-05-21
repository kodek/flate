package cli

import (
	"testing"

	"github.com/home-operations/flate/pkg/change"
	"github.com/home-operations/flate/pkg/manifest"
)

func TestScopedNamespaces_ExplicitNamespaceWins(t *testing.T) {
	c := &commonFlags{namespace: "media"}
	got := c.scopedNamespaces(&change.Filter{})
	if _, ok := got["media"]; !ok || len(got) != 1 {
		t.Errorf("explicit -n media not honored: %v", got)
	}
}

func TestScopedNamespaces_PathOrigAutoScopesToKeepSet(t *testing.T) {
	c := &commonFlags{namespace: ""}
	f := &change.Filter{Changes: change.NewSet([]string{"file"})}
	f.Keep = map[manifest.NamedResource]struct{}{
		{Kind: "K", Namespace: "media", Name: "x"}:      {},
		{Kind: "K", Namespace: "networking", Name: "y"}: {},
	}
	got := c.scopedNamespaces(f)
	for _, want := range []string{"media", "networking"} {
		if _, ok := got[want]; !ok {
			t.Errorf("auto-scope missing %q: got=%v", want, got)
		}
	}
}

func TestScopedNamespaces_NoFilterMeansAll(t *testing.T) {
	c := &commonFlags{namespace: ""}
	// Disabled filter (Changes == nil) → no scope (all namespaces).
	if got := c.scopedNamespaces(&change.Filter{}); got != nil {
		t.Errorf("expected nil (all-namespaces), got %v", got)
	}
}

func TestIncludeNamespace_ClusterScopedAlwaysIncluded(t *testing.T) {
	c := &commonFlags{namespace: "media"}
	if !c.includeNamespace(&change.Filter{}, "") {
		t.Error("cluster-scoped (empty) namespace must always pass")
	}
}

func TestIncludeNamespace_RespectsExplicitFilter(t *testing.T) {
	c := &commonFlags{namespace: "media"}
	if !c.includeNamespace(&change.Filter{}, "media") {
		t.Error("matching namespace must pass")
	}
	if c.includeNamespace(&change.Filter{}, "default") {
		t.Error("non-matching namespace must fail")
	}
}
