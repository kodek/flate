package manifest

import "fmt"

// HelmChart is the embedded chart template inside a HelmRelease.spec.chart
// (or the resolved form of HelmRelease.spec.chartRef). It is NOT the
// stand-alone HelmChart CRD — see HelmChartSource for that.
type HelmChart struct {
	// Name is the chart name within the source repository.
	Name string `json:"name" yaml:"name"`
	// Version is omitted from output for tidy diffs but kept in memory.
	Version string `json:"-" yaml:"-"`
	// RepoName, RepoNamespace, RepoKind identify the sourceRef.
	RepoName      string `json:"repoName" yaml:"repoName"`
	RepoNamespace string `json:"repoNamespace" yaml:"repoNamespace"`
	RepoKind      string `json:"repoKind" yaml:"repoKind"`
}

// RepoFullName is "<namespace>-<name>" — the canonical id of the source.
func (h HelmChart) RepoFullName() string {
	return h.RepoNamespace + "-" + h.RepoName
}

// ChartName is "<repoFullName>/<chartName>" — used as the helm chart ref.
func (h HelmChart) ChartName() string {
	return h.RepoFullName() + "/" + h.Name
}

// ParseHelmChart pulls the chart template out of a HelmRelease document.
// `defaultNamespace` is used when the chart ref omits a namespace.
func ParseHelmChart(doc map[string]any, defaultNamespace string) (HelmChart, error) {
	if err := checkAPIVersion(doc, HelmReleaseDomain); err != nil {
		return HelmChart{}, err
	}
	spec, err := requireSpec("HelmRelease", doc)
	if err != nil {
		return HelmChart{}, err
	}

	if chartRef, ok := spec["chartRef"].(map[string]any); ok {
		kind, _ := chartRef["kind"].(string)
		if kind == "" {
			return HelmChart{}, inputf("HelmRelease missing spec.chartRef.kind")
		}
		name, _ := chartRef["name"].(string)
		if name == "" {
			return HelmChart{}, inputf("HelmRelease missing spec.chartRef.name")
		}
		ns, _ := chartRef["namespace"].(string)
		if ns == "" {
			ns = defaultNamespace
		}
		return HelmChart{
			Name:          name,
			RepoName:      name,
			RepoNamespace: ns,
			RepoKind:      kind,
		}, nil
	}

	chart, ok := spec["chart"].(map[string]any)
	if !ok {
		return HelmChart{}, inputf("HelmRelease missing spec.chart or spec.chartRef")
	}
	chartSpec, ok := chart["spec"].(map[string]any)
	if !ok {
		return HelmChart{}, inputf("HelmRelease missing spec.chart.spec")
	}
	chartName, _ := chartSpec["chart"].(string)
	if chartName == "" {
		return HelmChart{}, inputf("HelmRelease missing spec.chart.spec.chart")
	}
	version, _ := chartSpec["version"].(string)
	sourceRef, ok := chartSpec["sourceRef"].(map[string]any)
	if !ok {
		return HelmChart{}, inputf("HelmRelease missing spec.chart.spec.sourceRef")
	}
	srcName, _ := sourceRef["name"].(string)
	if srcName == "" {
		return HelmChart{}, inputf("HelmRelease missing spec.chart.spec.sourceRef.name")
	}
	srcNamespace, _ := sourceRef["namespace"].(string)
	if srcNamespace == "" {
		srcNamespace = defaultNamespace
	}
	repoKind := stringOr(sourceRef, "kind", KindHelmRepository)
	return HelmChart{
		Name:          chartName,
		Version:       version,
		RepoName:      srcName,
		RepoNamespace: srcNamespace,
		RepoKind:      repoKind,
	}, nil
}

// HelmChartFromSource constructs a HelmChart from a resolved HelmChartSource.
func HelmChartFromSource(src *HelmChartSource) HelmChart {
	return HelmChart{
		Name:          src.Chart,
		Version:       src.Version,
		RepoName:      src.RepoName,
		RepoNamespace: src.RepoNamespace,
		RepoKind:      src.RepoKind,
	}
}

// HelmChartSource is the standalone HelmChart CRD
// (source.toolkit.fluxcd.io/v1 HelmChart).
type HelmChartSource struct {
	Name          string `json:"name" yaml:"name"`
	Namespace     string `json:"namespace" yaml:"namespace"`
	Chart         string `json:"chart" yaml:"chart"`
	Version       string `json:"version,omitempty" yaml:"version,omitempty"`
	RepoName      string `json:"repoName" yaml:"repoName"`
	RepoNamespace string `json:"repoNamespace" yaml:"repoNamespace"`
	RepoKind      string `json:"repoKind" yaml:"repoKind"`
}

// Named identifies the chart resource.
func (h *HelmChartSource) Named() NamedResource {
	return NamedResource{Kind: KindHelmChart, Namespace: h.Namespace, Name: h.Name}
}

// ResourceFullName is "<namespace>-<name>".
func (h *HelmChartSource) ResourceFullName() string {
	return h.Namespace + "-" + h.Name
}

// ParseHelmChartSource decodes a standalone HelmChart CRD.
func ParseHelmChartSource(doc map[string]any) (*HelmChartSource, error) {
	if err := checkAPIVersion(doc, SourceDomain); err != nil {
		return nil, err
	}
	metadata, name, ns, err := requireMetadata("HelmChart", doc)
	if err != nil {
		return nil, err
	}
	if ns == "" {
		ns = DefaultNamespace
	}
	spec, err := requireSpec("HelmChart", doc)
	if err != nil {
		return nil, err
	}
	chart, _ := spec["chart"].(string)
	if chart == "" {
		return nil, inputf("HelmChart missing spec.chart")
	}
	version, _ := spec["version"].(string)
	sourceRef, ok := spec["sourceRef"].(map[string]any)
	if !ok {
		return nil, inputf("HelmChart missing spec.sourceRef")
	}
	srcName, _ := sourceRef["name"].(string)
	if srcName == "" {
		return nil, inputf("HelmChart missing spec.sourceRef.name")
	}
	srcNamespace, _ := sourceRef["namespace"].(string)
	if srcNamespace == "" {
		srcNamespace = ns
	}
	repoKind := stringOr(sourceRef, "kind", KindHelmRepository)
	_ = metadata
	return &HelmChartSource{
		Name:          name,
		Namespace:     ns,
		Chart:         chart,
		Version:       version,
		RepoName:      srcName,
		RepoNamespace: srcNamespace,
		RepoKind:      repoKind,
	}, nil
}

// ValuesReference points at a ConfigMap or Secret supplying values to a
// HelmRelease via .spec.valuesFrom.
type ValuesReference struct {
	Kind       string `json:"kind" yaml:"kind"`
	Name       string `json:"name" yaml:"name"`
	ValuesKey  string `json:"valuesKey,omitempty" yaml:"valuesKey,omitempty"`
	TargetPath string `json:"targetPath,omitempty" yaml:"targetPath,omitempty"`
	Optional   bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// EffectiveValuesKey returns ValuesKey or the default "values.yaml".
func (v ValuesReference) EffectiveValuesKey() string {
	if v.ValuesKey == "" {
		return "values.yaml"
	}
	return v.ValuesKey
}

// LocalObjectReference matches Kubernetes' core/v1 LocalObjectReference.
type LocalObjectReference struct {
	Name string `json:"name" yaml:"name"`
}

// HelmRelease is the Flux HelmRelease CRD.
type HelmRelease struct {
	Name                     string            `json:"name" yaml:"name"`
	Namespace                string            `json:"namespace" yaml:"namespace"`
	Chart                    HelmChart         `json:"chart" yaml:"chart"`
	TargetNamespace          string            `json:"-" yaml:"-"`
	Values                   map[string]any    `json:"-" yaml:"-"`
	ValuesFrom               []ValuesReference `json:"-" yaml:"-"`
	Images                   []string          `json:"images,omitempty" yaml:"images,omitempty"`
	Labels                   map[string]string `json:"-" yaml:"-"`
	DisableSchemaValidation  bool              `json:"-" yaml:"-"`
	DisableOpenAPIValidation bool              `json:"-" yaml:"-"`
}

// Named identifies the release.
func (h *HelmRelease) Named() NamedResource {
	return NamedResource{Kind: KindHelmRelease, Namespace: h.Namespace, Name: h.Name}
}

// ReleaseName is "<namespace>-<name>" — the canonical id.
func (h *HelmRelease) ReleaseName() string { return h.Namespace + "-" + h.Name }

// ReleaseNamespace returns TargetNamespace when set, otherwise Namespace.
func (h *HelmRelease) ReleaseNamespace() string {
	if h.TargetNamespace != "" {
		return h.TargetNamespace
	}
	return h.Namespace
}

// RepoName is the HelmRepository identifier (namespace-name).
func (h *HelmRelease) RepoName() string {
	return h.Chart.RepoNamespace + "-" + h.Chart.RepoName
}

// NamespacedName is "<namespace>/<name>".
func (h *HelmRelease) NamespacedName() string { return h.Namespace + "/" + h.Name }

// ResourceDependencies returns the resources whose readiness gates this
// HelmRelease's reconciliation: the release itself, its chart repo, and
// any valuesFrom references.
func (h *HelmRelease) ResourceDependencies() []NamedResource {
	deps := []NamedResource{h.Named()}
	deps = append(deps, NamedResource{Kind: h.Chart.RepoKind, Namespace: h.Chart.RepoNamespace, Name: h.Chart.RepoName})
	seen := make(map[string]struct{})
	for _, ref := range h.ValuesFrom {
		if _, ok := seen[ref.Name]; ok {
			continue
		}
		seen[ref.Name] = struct{}{}
		deps = append(deps, NamedResource{Kind: ref.Kind, Namespace: h.Namespace, Name: ref.Name})
	}
	return deps
}

// ResolveChartRef replaces a chartRef placeholder with the resolved source
// when version was not pinned. helmCharts is keyed by ResourceFullName.
func (h *HelmRelease) ResolveChartRef(helmCharts map[string]*HelmChartSource) error {
	if h.Chart.RepoKind != KindHelmChart || h.Chart.Version != "" {
		return nil
	}
	src, ok := helmCharts[h.Chart.RepoFullName()]
	if !ok {
		return fmt.Errorf("%w: HelmChartSource %s not found for HelmRelease %s",
			ErrObjectNotFound, h.Chart.RepoFullName(), h.NamespacedName())
	}
	if src.Version != "" {
		h.Chart = HelmChartFromSource(src)
	}
	return nil
}

// ParseHelmRelease decodes a HelmRelease CR from a raw YAML document.
func ParseHelmRelease(doc map[string]any) (*HelmRelease, error) {
	if err := checkAPIVersion(doc, HelmReleaseDomain); err != nil {
		return nil, err
	}
	metadata, name, ns, err := requireMetadata("HelmRelease", doc)
	if err != nil {
		return nil, err
	}
	// metadata.namespace is optional — Flux Kustomizations commonly
	// inject it via spec.targetNamespace. Treat the empty string as
	// "inherit later".
	chart, err := ParseHelmChart(doc, ns)
	if err != nil {
		return nil, err
	}
	spec := doc["spec"].(map[string]any)

	var vfs []ValuesReference
	if raw, ok := spec["valuesFrom"].([]any); ok {
		vfs = make([]ValuesReference, 0, len(raw))
		for _, e := range raw {
			m, ok := e.(map[string]any)
			if !ok {
				continue
			}
			vr := ValuesReference{
				Kind:       stringOr(m, "kind", ""),
				Name:       stringOr(m, "name", ""),
				ValuesKey:  stringOr(m, "valuesKey", ""),
				TargetPath: stringOr(m, "targetPath", ""),
			}
			if opt, ok := m["optional"].(bool); ok {
				vr.Optional = opt
			}
			vfs = append(vfs, vr)
		}
	}

	var values map[string]any
	if v, ok := spec["values"].(map[string]any); ok {
		values = v
	}

	disableSchema := readBoolAny(spec, "install", "disableSchemaValidation") ||
		readBoolAny(spec, "upgrade", "disableSchemaValidation")
	disableOpenAPI := readBoolAny(spec, "install", "disableOpenAPIValidation") ||
		readBoolAny(spec, "upgrade", "disableOpenAPIValidation")

	target, _ := spec["targetNamespace"].(string)
	return &HelmRelease{
		Name:                     name,
		Namespace:                ns,
		Chart:                    chart,
		TargetNamespace:          target,
		Values:                   values,
		ValuesFrom:               vfs,
		Labels:                   asStringMap(metadata["labels"]),
		DisableSchemaValidation:  disableSchema,
		DisableOpenAPIValidation: disableOpenAPI,
	}, nil
}

// readBoolAny safely reads spec[parent][key] as bool.
func readBoolAny(spec map[string]any, parent, key string) bool {
	bag, ok := spec[parent].(map[string]any)
	if !ok {
		return false
	}
	v, _ := bag[key].(bool)
	return v
}

// HelmRepository is the Flux HelmRepository CRD.
type HelmRepository struct {
	Name      string `json:"name" yaml:"name"`
	Namespace string `json:"namespace" yaml:"namespace"`
	URL       string `json:"url" yaml:"url"`
	// RepoType is "default" or "oci".
	RepoType string `json:"repoType,omitempty" yaml:"repoType,omitempty"`
}

// Named identifies the repo.
func (h *HelmRepository) Named() NamedResource {
	return NamedResource{Kind: KindHelmRepository, Namespace: h.Namespace, Name: h.Name}
}

// RepoName is "<namespace>-<name>".
func (h *HelmRepository) RepoName() string { return h.Namespace + "-" + h.Name }

// HelmChartName returns the chart ref used with the helm SDK. For OCI
// repos the chart name is appended to the URL.
func (h *HelmRepository) HelmChartName(chart HelmChart) string {
	if h.RepoType == RepoTypeOCI {
		return h.URL + "/" + chart.Name
	}
	return chart.ChartName()
}

// ParseHelmRepository decodes a HelmRepository CR.
func ParseHelmRepository(doc map[string]any) (*HelmRepository, error) {
	if err := checkAPIVersion(doc, SourceDomain); err != nil {
		return nil, err
	}
	_, name, ns, err := requireMetadata("HelmRepository", doc)
	if err != nil {
		return nil, err
	}
	spec, err := requireSpec("HelmRepository", doc)
	if err != nil {
		return nil, err
	}
	url, _ := spec["url"].(string)
	if url == "" {
		return nil, inputf("HelmRepository missing spec.url")
	}
	return &HelmRepository{
		Name:      name,
		Namespace: ns,
		URL:       url,
		RepoType:  stringOr(spec, "type", RepoTypeDefault),
	}, nil
}
