package cluster

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"clusterctl/internal/config"
	"clusterctl/internal/talos"
)

// Bootstrap generates Talos machine configs, applies them to every
// provisioned node, bootstraps etcd on the first control-plane node, and
// fetches the resulting kubeconfig. Safe to re-run: GenerateConfigs reuses
// existing configs on disk, and talos.Bootstrap treats "already
// bootstrapped" as success.
func Bootstrap(ctx context.Context, cfg *config.Config, provisioned *ProvisionResult, workDir string) error {
	if len(provisioned.ControlPlane) == 0 {
		return fmt.Errorf("no control-plane nodes in provision result")
	}

	endpoint := fmt.Sprintf("https://%s:6443", cfg.Network.ControlPlaneVIP)
	cpBytes, workerBytes, err := talos.GenerateConfigs(cfg.Name, endpoint, workDir)
	if err != nil {
		return fmt.Errorf("generating talos configs: %w", err)
	}

	// The VIP patch targets a specific NIC via a MAC-based device selector
	// (see talos.BuildVIPPatch), and each control-plane node has a distinct
	// MAC, so the patch — and therefore the patched config — must be built
	// per node rather than shared across all of them.
	for _, n := range provisioned.ControlPlane {
		patchedCPBytes, err := talos.ApplyVIPPatch(cpBytes, cfg.Network.ControlPlaneVIP, n.MAC)
		if err != nil {
			return fmt.Errorf("applying VIP patch for %s (%s): %w", n.Name, n.IP, err)
		}

		if err := talos.ApplyConfig(ctx, n.IP, patchedCPBytes); err != nil {
			return fmt.Errorf("applying control-plane config to %s (%s): %w", n.Name, n.IP, err)
		}
	}

	for _, n := range provisioned.Worker {
		if err := talos.ApplyConfig(ctx, n.IP, workerBytes); err != nil {
			return fmt.Errorf("applying worker config to %s (%s): %w", n.Name, n.IP, err)
		}
	}

	firstCP := provisioned.ControlPlane[0]
	talosconfigPath := filepath.Join(workDir, "talosconfig")

	if err := talos.Bootstrap(ctx, talosconfigPath, firstCP.IP, 20, 10*time.Second); err != nil {
		return fmt.Errorf("bootstrapping etcd on %s (%s): %w", firstCP.Name, firstCP.IP, err)
	}

	kubeconfigPath := filepath.Join(workDir, "kubeconfig")
	if err := talos.FetchKubeconfig(ctx, talosconfigPath, firstCP.IP, kubeconfigPath); err != nil {
		return fmt.Errorf("fetching kubeconfig: %w", err)
	}

	return nil
}
