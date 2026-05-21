package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/home-operations/flate/internal/format"
	"github.com/home-operations/flate/pkg/kustomize"
	"github.com/home-operations/flate/pkg/manifest"
	"github.com/home-operations/flate/pkg/orchestrator"
	"github.com/home-operations/flate/pkg/store"
)

// buildFlags holds flags shared across `build ks`, `build hr`, and
// `build all` that aren't part of commonFlags.
type buildFlags struct {
	onlyCRDs bool
}

func bindBuildFlags(fs *pflag.FlagSet, b *buildFlags) {
	fs.BoolVar(&b.onlyCRDs, "only-crds", false,
		"emit only CustomResourceDefinition resources (implies --skip-crds=false)")
}

func newBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Render Flux objects to YAML",
	}
	cmd.AddCommand(newBuildKSCmd(), newBuildHRCmd(), newBuildAllCmd())
	return cmd
}

func newBuildKSCmd() *cobra.Command {
	c := &commonFlags{}
	h := &helmFlags{}
	b := &buildFlags{}
	cmd := &cobra.Command{
		Use:     "ks [name]",
		Aliases: []string{"kustomization", "kustomizations"},
		Short:   "Render Kustomizations",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			applyBuildFlags(c, b)
			o, err := runOrchestrator(cmdContext(cmd), *c, *h)
			if err != nil && o == nil {
				return err
			}
			return emitRendered(cmd.OutOrStdout(), o, manifest.KindKustomization, firstArg(args), c, b)
		},
	}
	bindCommon(cmd.Flags(), c)
	bindHelmFlags(cmd.Flags(), h)
	bindBuildFlags(cmd.Flags(), b)
	return cmd
}

func newBuildHRCmd() *cobra.Command {
	c := &commonFlags{}
	h := &helmFlags{}
	b := &buildFlags{}
	c.enableHelm = true // implicit
	cmd := &cobra.Command{
		Use:     "hr [name]",
		Aliases: []string{"helmrelease", "helmreleases"},
		Short:   "Render HelmReleases",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			applyBuildFlags(c, b)
			o, err := runOrchestrator(cmdContext(cmd), *c, *h)
			if err != nil && o == nil {
				return err
			}
			return emitRendered(cmd.OutOrStdout(), o, manifest.KindHelmRelease, firstArg(args), c, b)
		},
	}
	bindCommon(cmd.Flags(), c)
	bindHelmFlags(cmd.Flags(), h)
	bindBuildFlags(cmd.Flags(), b)
	return cmd
}

func newBuildAllCmd() *cobra.Command {
	c := &commonFlags{}
	h := &helmFlags{}
	b := &buildFlags{}
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Render all Kustomization and HelmRelease objects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			applyBuildFlags(c, b)
			o, err := runOrchestrator(cmdContext(cmd), *c, *h)
			if err != nil && o == nil {
				return err
			}
			w, closeFn, err := c.resolveWriter(cmd.OutOrStdout())
			if err != nil {
				return err
			}
			defer func() { _ = closeFn() }()
			if err := writeRendered(w, o, manifest.KindKustomization, "", c, b); err != nil {
				return err
			}
			if c.enableHelm {
				return writeRendered(w, o, manifest.KindHelmRelease, "", c, b)
			}
			return nil
		},
	}
	bindCommon(cmd.Flags(), c)
	bindHelmFlags(cmd.Flags(), h)
	bindBuildFlags(cmd.Flags(), b)
	return cmd
}

func applyBuildFlags(c *commonFlags, b *buildFlags) {
	if b.onlyCRDs {
		c.skipCRDs = false
	}
}

func emitRendered(stdout io.Writer, o *orchestrator.Orchestrator, kind, name string, c *commonFlags, b *buildFlags) error {
	w, closeFn, err := c.resolveWriter(stdout)
	if err != nil {
		return err
	}
	defer func() { _ = closeFn() }()
	return writeRendered(w, o, kind, name, c, b)
}

// writeRendered is the writer-injectable inner half of emitRendered;
// `build all` shares one writer across two calls.
func writeRendered(w io.Writer, o *orchestrator.Orchestrator, kind, name string, c *commonFlags, b *buildFlags) error {
	var all []map[string]any
	for _, obj := range o.Store().ListObjects(kind) {
		id := obj.Named()
		if name != "" && id.Name != name {
			continue
		}
		if !c.includeNamespace(o.Filter(), id.Namespace) {
			continue
		}
		switch a := o.Store().GetArtifact(id).(type) {
		case *store.KustomizationArtifact:
			all = append(all, a.Manifests...)
		case *store.HelmReleaseArtifact:
			all = append(all, a.Manifests...)
		}
	}
	if b.onlyCRDs {
		all = kustomize.FilterKinds(all, []string{manifest.KindCustomResourceDefinition})
	}
	return format.YAMLMulti(w, all)
}
