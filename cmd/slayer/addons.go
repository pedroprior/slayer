package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"slayer/internal/cluster"
)

func newAddonsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "addons",
		Short: "Apply Kubernetes addon manifests (MetalLB)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cluster.Addons(cmd.Context(), cfg, "talos/kubeconfig"); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "addons applied")
			return nil
		},
	}
}
