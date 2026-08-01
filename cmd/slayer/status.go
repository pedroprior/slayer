package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"slayer/internal/cluster"
	"slayer/internal/libvirt"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show libvirt-level status of the cluster's VMs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := libvirt.Connect("qemu:///system")
			if err != nil {
				return err
			}
			defer c.Close()

			report, err := cluster.Status(c, cfg)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), report)
			return nil
		},
	}
}
