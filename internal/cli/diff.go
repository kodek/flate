package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/home-operations/flate/pkg/diff"
	"github.com/home-operations/flate/pkg/manifest"
)

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff rendered output against a previous revision",
	}
	cmd.AddCommand(newDiffKSCmd(), newDiffHRCmd())
	return cmd
}

type diffFlags struct {
	unified    int
	stripAttrs []string
	limitBytes int
}

func bindDiffFlags(cmd *cobra.Command, d *diffFlags) {
	cmd.Flags().IntVarP(&d.unified, "unified", "u", 3, "unified diff context lines")
	cmd.Flags().StringSliceVar(&d.stripAttrs, "strip-attrs", nil, "metadata annotation/label keys to strip before diffing")
	cmd.Flags().IntVar(&d.limitBytes, "limit-bytes", 0, "truncate per-resource diffs (0 = unlimited)")
}

func newDiffKSCmd() *cobra.Command {
	c := &commonFlags{}
	h := &helmFlags{}
	d := &diffFlags{}
	cmd := &cobra.Command{
		Use:     "ks [name]",
		Aliases: []string{"kustomization", "kustomizations"},
		Short:   "Diff Kustomizations against another path",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, c, h, d, manifest.KindKustomization, firstArg(args))
		},
	}
	bindCommon(cmd.Flags(), c)
	bindHelmFlags(cmd.Flags(), h)
	bindDiffFlags(cmd, d)
	return cmd
}

func newDiffHRCmd() *cobra.Command {
	c := &commonFlags{}
	h := &helmFlags{}
	d := &diffFlags{}
	cmd := &cobra.Command{
		Use:     "hr [name]",
		Aliases: []string{"helmrelease", "helmreleases"},
		Short:   "Diff HelmReleases against another path",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, c, h, d, manifest.KindHelmRelease, firstArg(args))
		},
	}
	bindCommon(cmd.Flags(), c)
	bindHelmFlags(cmd.Flags(), h)
	bindDiffFlags(cmd, d)
	return cmd
}

func runDiff(cmd *cobra.Command, c *commonFlags, h *helmFlags, d *diffFlags, kind, name string) error {
	if c.pathOrig == "" {
		return errors.New("diff requires --path-orig")
	}

	// Run two orchestrators — current and baseline — each in
	// changed-only mode against the other. Both sides compute the
	// same change set (it's symmetric) and only render the resources
	// that differ.
	ctx := cmdContext(cmd)
	currentCfg := buildOrchCfg(*c, *h)
	currentOrch, err := runOrchestratorCfg(ctx, currentCfg)
	if err != nil && currentOrch == nil {
		return err
	}

	origCfg := currentCfg
	origCfg.Path, origCfg.PathOrig = c.pathOrig, c.path
	origOrch, err := runOrchestratorCfg(ctx, origCfg)
	if err != nil && origOrch == nil {
		return err
	}

	origDocs := gatherArtifacts(origOrch, kind, name, c)
	currentDocs := gatherArtifacts(currentOrch, kind, name, c)

	outFormat := c.output
	if outFormat == "table" {
		outFormat = "diff"
	}
	diffs, err := diff.Run(origDocs, currentDocs, diff.Options{
		Format:     diff.Format(outFormat),
		Context:    d.unified,
		LimitBytes: d.limitBytes,
		StripAttrs: d.stripAttrs,
	})
	if err != nil {
		return err
	}
	formatted, err := diff.Render(diffs, diff.Format(outFormat))
	if err != nil {
		return err
	}
	w, closeFn, err := c.resolveWriter(cmd.OutOrStdout())
	if err != nil {
		return err
	}
	defer func() { _ = closeFn() }()
	_, err = w.Write(formatted)
	return err
}
