package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/home-operations/flate/internal/testrunner"
)

func newTestCmd() *cobra.Command {
	c := &commonFlags{}
	h := &helmFlags{}
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Validate cluster resources (reports Kustomization + HelmRelease reconcile status)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := runOrchestrator(cmdContext(cmd), *c, *h)
			if err != nil && o == nil {
				return err
			}
			report := testrunner.Run(testrunner.Job{Store: o.Store()})
			report.Write(cmd.OutOrStdout())
			if report.AnyFailed() {
				return fmt.Errorf("test failures detected")
			}
			return nil
		},
	}
	bindCommon(cmd.Flags(), c)
	bindHelmFlags(cmd.Flags(), h)
	return cmd
}
