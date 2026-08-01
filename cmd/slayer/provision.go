package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"slayer/internal/cluster"
	"slayer/internal/libvirt"
)

func newProvisionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "provision",
		Short: "Create/start the control-plane and worker VMs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := libvirt.Connect("qemu:///system")
			if err != nil {
				return err
			}
			defer c.Close()

			result, err := cluster.Provision(c, cfg)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Control planes:")
			for _, n := range result.ControlPlane {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s -> %s\n", n.Name, n.IP)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Workers:")
			for _, n := range result.Worker {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s -> %s\n", n.Name, n.IP)
			}
			return nil
		},
	}
}
