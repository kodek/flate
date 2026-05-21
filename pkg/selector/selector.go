// Package selector implements path + metadata filtering used by every
// fluxrr command. Selectors are translated from CLI flags by the cli
// package and consumed by controllers and the orchestrator to decide
// which Kustomizations/HelmReleases to consider.
package selector

import "github.com/buroa/fluxrr/pkg/manifest"

// Metadata filters resources by Kubernetes-style metadata.
type Metadata struct {
	Name          string
	Namespace     string
	AllNamespaces bool
	Labels        map[string]string
	SkipCRDs      bool
	SkipSecrets   bool
	SkipKinds     []string
}

// Matches reports whether obj passes the metadata filter.
func (m Metadata) Matches(obj manifest.BaseManifest) bool {
	if obj == nil {
		return false
	}
	id := obj.Named()
	if m.Name != "" && id.Name != m.Name {
		return false
	}
	if !m.AllNamespaces && m.Namespace != "" && id.Namespace != m.Namespace {
		return false
	}
	for _, sk := range m.SkipKinds {
		if id.Kind == sk {
			return false
		}
	}
	if len(m.Labels) > 0 {
		labels := labelsOf(obj)
		for k, v := range m.Labels {
			if labels[k] != v {
				return false
			}
		}
	}
	return true
}

func labelsOf(obj manifest.BaseManifest) map[string]string {
	switch o := obj.(type) {
	case *manifest.Kustomization:
		return o.Labels
	case *manifest.HelmRelease:
		return o.Labels
	}
	return nil
}

// Path filters by repository path or sourceRef.
type Path struct {
	// Root is the local filesystem path containing flux manifests.
	Root string
	// Sources optionally remaps GitRepository/OCIRepository sourceRefs
	// to local paths. Format: "namespace/name=./relative/path".
	Sources []SourceMapping
}

// SourceMapping ties a source name (namespace/name) to an on-disk path.
type SourceMapping struct {
	Namespace string
	Name      string
	Path      string
}
