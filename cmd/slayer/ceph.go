package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"slayer/internal/cluster"
)

func newCephCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ceph",
		Short: "Install Rook-Ceph and claim worker OSD disks for storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cluster.Ceph(cmd.Context(), cfg, "talos/kubeconfig"); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ceph cluster applied — check `kubectl -n rook-ceph get cephcluster` for health")
			return nil
		},
	}
}
