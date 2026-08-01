package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"slayer/internal/config"
)

var configPath string
var cfg *config.Config

func main() {
	rootCmd := &cobra.Command{
		Use:   "slayer",
		Short: "Provision and manage the baremetal Talos/Kubernetes homelab cluster",
	}
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "cluster.yaml", "path to cluster.yaml config file")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "version" {
			return nil
		}
		loaded, err := config.Load(configPath)
		if err != nil {
			return err
		}
		cfg = loaded
		return nil
	}

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the slayer version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "slayer dev")
			return nil
		},
	})

	rootCmd.AddCommand(newProvisionCmd())
	rootCmd.AddCommand(newBootstrapCmd())
	rootCmd.AddCommand(newAddonsCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newStopCmd())
	rootCmd.AddCommand(newDestroyCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
