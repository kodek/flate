package testrunner

import (
	"bytes"
	"strings"
	"testing"

	"github.com/buroa/fluxrr/pkg/manifest"
	"github.com/buroa/fluxrr/pkg/store"
)

func TestRun_AllPass(t *testing.T) {
	s := store.New()
	s.AddObject(&manifest.Kustomization{Name: "apps", Namespace: "flux-system"})
	s.AddObject(&manifest.HelmRelease{Name: "demo", Namespace: "apps"})
	s.UpdateStatus(manifest.NamedResource{Kind: manifest.KindKustomization, Namespace: "flux-system", Name: "apps"}, store.StatusReady, "")
	s.UpdateStatus(manifest.NamedResource{Kind: manifest.KindHelmRelease, Namespace: "apps", Name: "demo"}, store.StatusReady, "")

	rep := Run(Job{Store: s})
	if rep.AnyFailed() || rep.Passed != 2 {
		t.Errorf("expected 2 passed, got %+v", rep)
	}
	var b bytes.Buffer
	rep.Write(&b)
	if !strings.Contains(b.String(), "2 passed") {
		t.Errorf("missing summary: %s", b.String())
	}
}

func TestRun_OneFailed(t *testing.T) {
	s := store.New()
	s.AddObject(&manifest.Kustomization{Name: "apps", Namespace: "flux-system"})
	s.UpdateStatus(manifest.NamedResource{Kind: manifest.KindKustomization, Namespace: "flux-system", Name: "apps"}, store.StatusFailed, "boom")

	rep := Run(Job{Store: s})
	if !rep.AnyFailed() {
		t.Errorf("expected failure: %+v", rep)
	}
	if rep.Cases[0].Reason != "boom" {
		t.Errorf("reason: %q", rep.Cases[0].Reason)
	}
}

func TestRun_NoStatus(t *testing.T) {
	s := store.New()
	s.AddObject(&manifest.Kustomization{Name: "x", Namespace: "ns"})
	rep := Run(Job{Store: s})
	if !rep.AnyFailed() {
		t.Errorf("expected failure for no-status case")
	}
}
