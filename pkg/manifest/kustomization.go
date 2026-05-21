package manifest

import (
	"log/slog"
	"maps"
	"slices"
)

// SubstituteReference contains a reference to a resource supplying the
// variable name/value pairs used by postBuild.substitute.
type SubstituteReference struct {
	Kind     string `json:"kind" yaml:"kind"`
	Name     string `json:"name" yaml:"name"`
	Optional bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// Kustomization is the Flux Kustomization CR. It bundles the path of a
// local kustomize tree together with the in-cluster materials it produces
// (HelmReleases, HelmRepositories, ConfigMaps, Secrets, ...).
type Kustomization struct {
	Name      string `json:"name" yaml:"name"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Path      string `json:"path" yaml:"path"`

	HelmRepos        []*HelmRepository  `json:"helmRepos,omitempty" yaml:"helmRepos,omitempty"`
	OCIRepos         []*OCIRepository   `json:"ociRepos,omitempty" yaml:"ociRepos,omitempty"`
	HelmReleases     []*HelmRelease     `json:"helmReleases,omitempty" yaml:"helmReleases,omitempty"`
	ConfigMaps       []*ConfigMap       `json:"configMaps,omitempty" yaml:"configMaps,omitempty"`
	Secrets          []*Secret          `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	HelmChartSources []*HelmChartSource `json:"helmChartSources,omitempty" yaml:"helmChartSources,omitempty"`

	// Internal-only fields (not emitted to YAML output).
	SourcePath              string                `json:"-" yaml:"-"`
	SourceKind              string                `json:"-" yaml:"-"`
	SourceName              string                `json:"-" yaml:"-"`
	SourceNamespace         string                `json:"-" yaml:"-"`
	TargetNamespace         string                `json:"-" yaml:"-"`
	Contents                map[string]any        `json:"-" yaml:"-"`
	PostBuildSubstitute     map[string]any        `json:"-" yaml:"-"`
	PostBuildSubstituteFrom []SubstituteReference `json:"-" yaml:"-"`
	DependsOn               []string              `json:"-" yaml:"-"`
	Labels                  map[string]string     `json:"-" yaml:"-"`
	// Components is Flux v1's spec.components — paths to kustomize
	// components injected on top of spec.path at reconcile time.
	Components []string `json:"-" yaml:"-"`

	Images []string `json:"images,omitempty" yaml:"images,omitempty"`
}

// Named identifies the Kustomization.
func (k *Kustomization) Named() NamedResource {
	return NamedResource{Kind: KindKustomization, Namespace: k.Namespace, Name: k.Name}
}

// IDName is the test-friendly identifier (the path).
func (k *Kustomization) IDName() string { return k.Path }

// NamespacedName is "<namespace>/<name>".
func (k *Kustomization) NamespacedName() string { return k.Namespace + "/" + k.Name }

// ValidateDependsOn drops any dependency that is not present in allKS.
// allKS is a set of "namespace/name" identifiers.
func (k *Kustomization) ValidateDependsOn(allKS map[string]struct{}) {
	if len(k.DependsOn) == 0 {
		return
	}
	kept := slices.DeleteFunc(slices.Clone(k.DependsOn), func(dep string) bool {
		_, ok := allKS[dep]
		return !ok
	})
	if missing := len(k.DependsOn) - len(kept); missing > 0 {
		// Demoted to Debug: dependsOn references often dangle in a
		// statically-loaded view because parent-Kustomization
		// targetNamespace inheritance happens lazily. Real Flux resolves
		// them at apply time, and dropping them here only affects the
		// wait order during fluxrr's reconcile.
		slog.Debug("kustomization dependsOn entries dropped",
			"kustomization", k.NamespacedName(),
			"dropped", missing, "kept", len(kept))
	}
	k.DependsOn = kept
}

// UpdatePostBuildSubstitutions merges the given map into the substitution
// table AND into the raw contents doc, mirroring upstream behavior so the
// raw document is consistent for serialization.
func (k *Kustomization) UpdatePostBuildSubstitutions(subs map[string]any) {
	if k.PostBuildSubstitute == nil {
		k.PostBuildSubstitute = make(map[string]any, len(subs))
	}
	maps.Copy(k.PostBuildSubstitute, subs)
	if k.Contents == nil {
		return
	}
	spec, _ := k.Contents["spec"].(map[string]any)
	if spec == nil {
		spec = make(map[string]any)
		k.Contents["spec"] = spec
	}
	post, _ := spec["postBuild"].(map[string]any)
	if post == nil {
		post = make(map[string]any)
		spec["postBuild"] = post
	}
	sub, _ := post["substitute"].(map[string]any)
	if sub == nil {
		sub = make(map[string]any)
		post["substitute"] = sub
	}
	maps.Copy(sub, subs)
}

// ParseKustomization decodes a Flux Kustomization CR.
func ParseKustomization(doc map[string]any) (*Kustomization, error) {
	if err := checkAPIVersion(doc, FluxKustomizeDomain); err != nil {
		return nil, err
	}
	metadata, name, ns, err := requireMetadata("Kustomization", doc)
	if err != nil {
		return nil, err
	}
	// namespace is optional — a parent Kustomization's
	// spec.targetNamespace may fill it in at apply time.
	spec, err := requireSpec("Kustomization", doc)
	if err != nil {
		return nil, err
	}

	path, _ := spec["path"].(string)

	var sourcePath string
	if annotations, ok := metadata["annotations"].(map[string]any); ok {
		sourcePath, _ = annotations["config.kubernetes.io/path"].(string)
	}

	sourceRef, _ := spec["sourceRef"].(map[string]any)
	postBuild, _ := spec["postBuild"].(map[string]any)

	var substituteFrom []SubstituteReference
	if raw, ok := postBuild["substituteFrom"].([]any); ok {
		for _, e := range raw {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			ref := SubstituteReference{
				Kind: stringOr(m, "kind", ""),
				Name: stringOr(m, "name", ""),
			}
			if opt, ok := m["optional"].(bool); ok {
				ref.Optional = opt
			}
			substituteFrom = append(substituteFrom, ref)
		}
	}

	var dependsOn []string
	if raw, ok := spec["dependsOn"].([]any); ok {
		for _, e := range raw {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			depName, _ := m["name"].(string)
			if depName == "" {
				return nil, inputf("Kustomization missing dependsOn.name")
			}
			depNS := stringOr(m, "namespace", ns)
			dependsOn = append(dependsOn, depNS+"/"+depName)
		}
	}

	srcKind, _ := sourceRef["kind"].(string)
	srcName, _ := sourceRef["name"].(string)
	srcNamespace := stringOr(sourceRef, "namespace", ns)

	target, _ := spec["targetNamespace"].(string)
	subst, _ := postBuild["substitute"].(map[string]any)

	var components []string
	if raw, ok := spec["components"].([]any); ok {
		for _, e := range raw {
			if s, ok := e.(string); ok && s != "" {
				components = append(components, s)
			}
		}
	}

	return &Kustomization{
		Name:                    name,
		Namespace:               ns,
		Path:                    path,
		SourcePath:              sourcePath,
		SourceKind:              srcKind,
		SourceName:              srcName,
		SourceNamespace:         srcNamespace,
		TargetNamespace:         target,
		Contents:                doc,
		PostBuildSubstitute:     subst,
		PostBuildSubstituteFrom: substituteFrom,
		DependsOn:               dependsOn,
		Labels:                  asStringMap(metadata["labels"]),
		Components:              components,
	}, nil
}
