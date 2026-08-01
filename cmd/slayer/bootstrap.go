package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"slayer/internal/cluster"
	"slayer/internal/libvirt"
)

func newBootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Generate and apply Talos configs, bootstrap etcd, fetch kubeconfig",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := libvirt.Connect("qemu:///system")
			if err != nil {
				return err
			}
			defer c.Close()

			// Re-derive current node IPs live rather than trusting a prior
			// `provision` run's output (no state file, per project constraints).
			provisioned, err := cluster.Provision(c, cfg)
			if err != nil {
				return fmt.Errorf("re-checking provisioned nodes before bootstrap: %w", err)
			}

			workDir := "talos"
			if err := cluster.Bootstrap(cmd.Context(), cfg, provisioned, workDir); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "bootstrap complete, kubeconfig written to %s/kubeconfig\n", workDir)
			return nil
		},
	}
}
