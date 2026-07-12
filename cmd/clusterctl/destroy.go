package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"clusterctl/internal/cluster"
	"clusterctl/internal/libvirt"
)

func newDestroyCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Stop and undefine all control-plane and worker VMs",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return fmt.Errorf("refusing to destroy without --yes (this stops and undefines all cluster VMs)")
			}

			c, err := libvirt.Connect("qemu:///system")
			if err != nil {
				return err
			}
			defer c.Close()

			errs := cluster.Destroy(c, cfg)
			if len(errs) > 0 {
				for _, e := range errs {
					fmt.Fprintln(cmd.ErrOrStderr(), e)
				}
				return fmt.Errorf("destroy completed with %d error(s)", len(errs))
			}

			fmt.Fprintln(cmd.OutOrStdout(), "all cluster VMs destroyed")
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm destructive VM teardown")
	return cmd
}
