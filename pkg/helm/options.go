package helm

import (
	"slices"
	"strings"

	"helm.sh/helm/v3/pkg/chartutil"
)

// Options collects the helm template flags fluxrr supports.
type Options struct {
	// SkipCRDs excludes CRDs from the rendered output.
	SkipCRDs bool
	// SkipTests excludes templates that are helm test hooks.
	SkipTests bool
	// SkipSecrets excludes Secret resources from the output. fluxrr
	// uses placeholder values anyway but stripping makes diffs tidier.
	SkipSecrets bool
	// SkipKinds is an arbitrary list of kinds to drop.
	SkipKinds []string

	// KubeVersion overrides Capabilities.KubeVersion (.Capabilities.KubeVersion).
	KubeVersion string
	// APIVersions overrides Capabilities.APIVersions
	// (.Capabilities.APIVersions). Comma-separated.
	APIVersions string

	// IsUpgrade sets .Release.IsUpgrade instead of .Release.IsInstall.
	IsUpgrade bool
	// NoHooks excludes hook-annotated templates.
	NoHooks bool
	// ShowOnly limits output to specific template paths.
	ShowOnly []string
	// EnableDNS enables DNS lookups during templating.
	EnableDNS bool
}

// SkipResourceKinds returns the union of canonical and user-specified
// kinds to drop from rendered output.
func (o Options) SkipResourceKinds() []string {
	out := append([]string{}, o.SkipKinds...)
	if o.SkipCRDs {
		out = append(out, "CustomResourceDefinition")
	}
	if o.SkipSecrets {
		out = append(out, "Secret")
	}
	return out
}

// capabilities builds chartutil.Capabilities from the supplied options.
// Returns the default capabilities when no overrides are provided.
func (o Options) capabilities() (*chartutil.Capabilities, error) {
	caps := chartutil.DefaultCapabilities.Copy()
	if o.KubeVersion != "" {
		kv, err := chartutil.ParseKubeVersion(o.KubeVersion)
		if err != nil {
			return nil, err
		}
		caps.KubeVersion = *kv
	}
	if o.APIVersions != "" {
		caps.APIVersions = chartutil.VersionSet(splitComma(o.APIVersions))
	}
	return caps, nil
}

// splitComma splits s on commas / whitespace, dropping empty entries.
func splitComma(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	return slices.DeleteFunc(fields, func(p string) bool { return p == "" })
}
