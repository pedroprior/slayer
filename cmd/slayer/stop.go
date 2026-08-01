package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"slayer/internal/cluster"
	"slayer/internal/libvirt"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Gracefully shut down all control-plane and worker VMs, keeping them defined",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := libvirt.Connect("qemu:///system")
			if err != nil {
				return err
			}
			defer c.Close()

			errs := cluster.Stop(c, cfg)
			if len(errs) > 0 {
				for _, e := range errs {
					fmt.Fprintln(cmd.ErrOrStderr(), e)
				}
				return fmt.Errorf("stop completed with %d error(s)", len(errs))
			}

			fmt.Fprintln(cmd.OutOrStdout(), "all cluster VMs stopped")
			return nil
		},
	}
}
