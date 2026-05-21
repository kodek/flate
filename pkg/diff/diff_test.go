package diff

import (
	"strings"
	"testing"
)

func cm(name, ns, key, value string) Doc {
	return Doc{
		Manifest: map[string]any{
			"kind": "ConfigMap",
			"metadata": map[string]any{
				"name": name, "namespace": ns,
			},
			"data": map[string]any{"k": value},
		},
		Parent: Parent{Kind: "HelmRelease", Namespace: ns, Name: key},
	}
}

func TestRun_Simple(t *testing.T) {
	left := []Doc{cm("a", "ns", "owner", "v1")}
	right := []Doc{cm("a", "ns", "owner", "v2")}
	diffs, err := Run(left, right, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if !strings.Contains(diffs[0].Diff, "-  k: v1") || !strings.Contains(diffs[0].Diff, "+  k: v2") {
		t.Errorf("unexpected diff body: %s", diffs[0].Diff)
	}
}

func TestRun_NoChange(t *testing.T) {
	d := cm("a", "", "owner", "v1")
	diffs, err := Run([]Doc{d}, []Doc{d}, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(diffs) != 0 {
		t.Errorf("expected zero diffs, got %d", len(diffs))
	}
}

func TestRun_AddedAndRemoved(t *testing.T) {
	left := []Doc{cm("a", "", "owner", "v")}
	right := []Doc{cm("b", "", "owner", "v")}
	diffs, err := Run(left, right, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(diffs) != 2 {
		t.Errorf("expected 2 diffs for add+remove, got %d", len(diffs))
	}
}

func TestRun_DeletionHasNoNullCounterpart(t *testing.T) {
	left := []Doc{cm("a", "ns", "owner", "v1")}
	diffs, err := Run(left, nil, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for deletion, got %d", len(diffs))
	}
	body := diffs[0].Diff
	if strings.Contains(body, "+null") {
		t.Errorf("deletion diff contained spurious +null line:\n%s", body)
	}
	if !strings.Contains(body, "----\n") {
		t.Errorf("expected '---' YAML document separator in removed body:\n%s", body)
	}
}

func TestRun_AdditionHasNoNullCounterpart(t *testing.T) {
	right := []Doc{cm("a", "ns", "owner", "v1")}
	diffs, err := Run(nil, right, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for addition, got %d", len(diffs))
	}
	body := diffs[0].Diff
	if strings.Contains(body, "-null") {
		t.Errorf("addition diff contained spurious -null line:\n%s", body)
	}
	if !strings.Contains(body, "+---\n") {
		t.Errorf("expected '---' YAML document separator in added body:\n%s", body)
	}
}

func TestFormat_JSON(t *testing.T) {
	diffs := []ResourceDiff{{Kind: "ConfigMap", Name: "a", Diff: "..."}}
	out, err := Render(diffs, FormatJSON)
	if err != nil {
		t.Fatalf("Format_: %v", err)
	}
	if !strings.Contains(string(out), `"kind": "ConfigMap"`) {
		t.Errorf("json output: %s", out)
	}
}

func TestRun_LimitBytes(t *testing.T) {
	left := []Doc{cm("a", "", "owner", strings.Repeat("x", 1000))}
	right := []Doc{cm("a", "", "owner", strings.Repeat("y", 1000))}
	diffs, err := Run(left, right, Options{LimitBytes: 200})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(diffs[0].Diff) > 220 {
		t.Errorf("LimitBytes not honored: %d", len(diffs[0].Diff))
	}
	if !strings.Contains(diffs[0].Diff, "truncated") {
		t.Errorf("missing truncated marker")
	}
}

func TestResourceDiff_Header(t *testing.T) {
	hrDiff := ResourceDiff{
		Parent: Parent{Kind: "HelmRelease", Namespace: "media", Name: "qui"},
		Kind:   "Deployment", Namespace: "media", Name: "qui",
	}
	want := "HelmRelease: media/qui Deployment: media/qui"
	if got := hrDiff.Header(); got != want {
		t.Errorf("HR header:\n got %q\nwant %q", got, want)
	}

	ksDiff := ResourceDiff{
		Parent: Parent{
			Kind: "Kustomization", Namespace: "media", Name: "qui",
			Path: "kubernetes/apps/media/qui/app",
		},
		Kind: "HelmRelease", Namespace: "media", Name: "qui",
	}
	want = "kubernetes/apps/media/qui/app Kustomization: media/qui HelmRelease: media/qui"
	if got := ksDiff.Header(); got != want {
		t.Errorf("KS header:\n got %q\nwant %q", got, want)
	}
}

func TestRun_DistinguishesByParent(t *testing.T) {
	// Same Deployment name rendered by two HRs: must produce two
	// separate diff entries (one per parent), not collapse into one.
	left := []Doc{
		{
			Manifest: map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": "x", "namespace": "ns"}, "spec": "old"},
			Parent:   Parent{Kind: "HelmRelease", Namespace: "ns", Name: "a"},
		},
		{
			Manifest: map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": "x", "namespace": "ns"}, "spec": "old"},
			Parent:   Parent{Kind: "HelmRelease", Namespace: "ns", Name: "b"},
		},
	}
	right := []Doc{
		{
			Manifest: map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": "x", "namespace": "ns"}, "spec": "new"},
			Parent:   Parent{Kind: "HelmRelease", Namespace: "ns", Name: "a"},
		},
		{
			Manifest: map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": "x", "namespace": "ns"}, "spec": "old"},
			Parent:   Parent{Kind: "HelmRelease", Namespace: "ns", Name: "b"},
		},
	}
	diffs, err := Run(left, right, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff (only HR=a's Deployment changed), got %d: %+v", len(diffs), diffs)
	}
	if diffs[0].Parent.Name != "a" {
		t.Errorf("wrong parent: %+v", diffs[0].Parent)
	}
}
