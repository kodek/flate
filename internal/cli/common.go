package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/home-operations/flate/pkg/change"
	"github.com/home-operations/flate/pkg/helm"
	"github.com/home-operations/flate/pkg/orchestrator"
)

// commonFlags collect path/namespace/selection flags shared across
// subcommands.
type commonFlags struct {
	path           string
	pathOrig       string
	namespace      string
	labels         map[string]string
	skipCRDs       bool
	skipSecrets    bool
	skipKinds      []string
	output         string
	outputFile     string
	enableOCI      bool
	enableHelm     bool
	registryConfig string
}

func bindCommon(fs *pflag.FlagSet, f *commonFlags) {
	fs.StringVar(&f.path, "path", ".", "path to the Flux cluster directory")
	fs.StringVar(&f.pathOrig, "path-orig", "",
		"baseline path; when set, every command runs in changed-only mode")
	fs.StringVarP(&f.namespace, "namespace", "n", "",
		"limit to this namespace (default: every namespace, or just the changed ones when --path-orig is set)")
	fs.StringToStringVarP(&f.labels, "selector", "l", nil, "label selector (key=value, repeatable)")
	fs.BoolVar(&f.skipCRDs, "skip-crds", true, "exclude CRD objects from rendered output")
	fs.BoolVar(&f.skipSecrets, "skip-secrets", true, "exclude Secret objects from rendered output")
	fs.StringSliceVar(&f.skipKinds, "skip-kinds", nil, "extra kinds to drop from rendered output")
	fs.StringVarP(&f.output, "output", "o", "table", "output format: table, wide, yaml, json, name")
	fs.StringVar(&f.outputFile, "output-file", "",
		"write output to this file instead of stdout (use \"-\" for stdout; convenient for containerised use without a shell)")
	fs.BoolVar(&f.enableOCI, "enable-oci", true, "reconcile OCIRepository objects")
	fs.BoolVar(&f.enableHelm, "enable-helm", true, "render HelmReleases via the helm SDK")
	fs.StringVar(&f.registryConfig, "registry-config", "", "docker config.json for OCI authentication")
}

// scopedNamespaces returns the namespace filter the command should
// honor. nil ↦ "no filter" (every namespace).
//
//   - An explicit --namespace value always wins.
//   - In --path-orig mode, the namespaces of the resolved keep-set
//     are used so commands automatically focus on what actually
//     changed without the user having to set -n.
//   - Otherwise every namespace is included.
func (c *commonFlags) scopedNamespaces(filter *change.Filter) map[string]struct{} {
	if c.namespace != "" {
		return map[string]struct{}{c.namespace: {}}
	}
	if filter.Enabled() {
		if ns := filter.KeepNamespaces(); len(ns) > 0 {
			return ns
		}
	}
	return nil
}

// includeNamespace reports whether ns passes the effective filter.
// Empty namespace (cluster-scoped resources) is always included.
func (c *commonFlags) includeNamespace(filter *change.Filter, ns string) bool {
	if ns == "" {
		return true
	}
	scope := c.scopedNamespaces(filter)
	if scope == nil {
		return true
	}
	_, ok := scope[ns]
	return ok
}

// resolveWriter returns the writer commands should emit to. When
// --output-file is empty or "-", fallback is used (typically the
// cobra command's stdout). The returned close func must be invoked.
func (c *commonFlags) resolveWriter(fallback io.Writer) (io.Writer, func() error, error) {
	if c.outputFile == "" || c.outputFile == "-" || c.outputFile == "/dev/stdout" {
		return fallback, func() error { return nil }, nil
	}
	f, err := os.Create(c.outputFile)
	if err != nil {
		return nil, nil, fmt.Errorf("--output-file %q: %w", c.outputFile, err)
	}
	return f, f.Close, nil
}

// helmFlags collect the helm template options. Mirrors flux-local's
// --kube-version/--api-versions/--no-hooks/etc.
type helmFlags struct {
	kubeVersion string
	apiVersions string
	isUpgrade   bool
	noHooks     bool
	showOnly    []string
	enableDNS   bool
}

func bindHelmFlags(fs *pflag.FlagSet, h *helmFlags) {
	// Default to the Kubernetes minor version bundled with the k8s.io/api
	// dependency. Charts gated on KubeVersion (e.g. >=1.33 for ImageVolume)
	// then render against the latest version flate knows about, which
	// matches what a freshly-`flux install`'d cluster would see.
	fs.StringVar(&h.kubeVersion, "kube-version", helm.BundledKubeVersion(),
		"Kubernetes version for .Capabilities.KubeVersion (default: version bundled with flate)")
	fs.StringVarP(&h.apiVersions, "api-versions", "a", "", "comma-separated API versions for .Capabilities.APIVersions")
	fs.BoolVar(&h.isUpgrade, "is-upgrade", false, "set .Release.IsUpgrade instead of .Release.IsInstall")
	fs.BoolVar(&h.noHooks, "no-hooks", false, "exclude hook-annotated templates")
	fs.StringSliceVarP(&h.showOnly, "show-only", "s", nil, "only show specific template paths (repeatable)")
	fs.BoolVar(&h.enableDNS, "enable-dns", false, "enable DNS lookups during helm template")
}

// helmOptions assembles helm.Options from CLI flags.
func (c commonFlags) helmOptions(h helmFlags) helm.Options {
	return helm.Options{
		SkipCRDs:    c.skipCRDs,
		SkipSecrets: c.skipSecrets,
		SkipKinds:   c.skipKinds,
		KubeVersion: h.kubeVersion,
		APIVersions: h.apiVersions,
		IsUpgrade:   h.isUpgrade,
		NoHooks:     h.noHooks,
		ShowOnly:    h.showOnly,
		EnableDNS:   h.enableDNS,
		SkipTests:   true,
	}
}

// buildOrchCfg materializes an orchestrator.Config from the parsed
// CLI flags.
func buildOrchCfg(c commonFlags, h helmFlags) orchestrator.Config {
	return orchestrator.Config{
		Path:           c.path,
		PathOrig:       c.pathOrig,
		HelmOptions:    c.helmOptions(h),
		WipeSecrets:    true,
		EnableOCI:      c.enableOCI,
		RegistryConfig: c.registryConfig,
	}
}

// runOrchestrator constructs and runs the orchestrator against the
// supplied path. Returns the Orchestrator (so the caller can introspect
// the Store) and any error.
func runOrchestrator(ctx context.Context, c commonFlags, h helmFlags) (*orchestrator.Orchestrator, error) {
	if c.path == "" {
		return nil, errors.New("path is required")
	}
	return runOrchestratorCfg(ctx, buildOrchCfg(c, h))
}

func runOrchestratorCfg(ctx context.Context, cfg orchestrator.Config) (*orchestrator.Orchestrator, error) {
	o, err := orchestrator.New(cfg)
	if err != nil {
		return nil, err
	}
	if err := o.Bootstrap(ctx); err != nil {
		return nil, err
	}
	if err := o.Run(ctx); err != nil {
		return o, err
	}
	return o, nil
}

// cmdContext returns the cobra command's context — set up by Run() with
// signal cancellation, so Ctrl-C terminates work mid-flight.
func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
