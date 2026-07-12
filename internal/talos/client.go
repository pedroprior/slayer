package talos

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

func insecureTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec // matches `talosctl apply-config --insecure` maintenance-mode handshake
}

// ApplyConfig pushes cfgBytes to a node still in maintenance mode (no
// established trust yet), mirroring `talosctl apply-config --insecure`.
func ApplyConfig(ctx context.Context, nodeIP string, cfgBytes []byte) error {
	c, err := client.New(ctx,
		client.WithTLSConfig(insecureTLSConfig()),
		client.WithEndpoints(nodeIP),
	)
	if err != nil {
		return fmt.Errorf("connecting to %s in maintenance mode: %w", nodeIP, err)
	}
	defer c.Close() //nolint:errcheck

	_, err = c.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{Data: cfgBytes})
	if err != nil {
		return fmt.Errorf("applying config to %s: %w", nodeIP, err)
	}
	return nil
}

func isAlreadyBootstrapped(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "etcd data-dir is not empty")
}

// Bootstrap retries etcd bootstrap on nodeIP, tolerating the API server not
// being ready yet right after reboot (matching script.sh's retry loop) and
// treating "already bootstrapped" as success for idempotency.
func Bootstrap(ctx context.Context, talosconfigPath, nodeIP string, attempts int, delay time.Duration) error {
	c, err := authenticatedClient(ctx, talosconfigPath, nodeIP)
	if err != nil {
		return err
	}
	defer c.Close() //nolint:errcheck

	var lastErr error
	for i := 0; i < attempts; i++ {
		lastErr = c.Bootstrap(ctx, nil)
		if lastErr == nil || isAlreadyBootstrapped(lastErr) {
			return nil
		}
		time.Sleep(delay)
	}
	return fmt.Errorf("bootstrap failed after %d attempts: %w", attempts, lastErr)
}

// FetchKubeconfig retrieves the admin kubeconfig from nodeIP and writes it
// to outPath.
func FetchKubeconfig(ctx context.Context, talosconfigPath, nodeIP, outPath string) error {
	c, err := authenticatedClient(ctx, talosconfigPath, nodeIP)
	if err != nil {
		return err
	}
	defer c.Close() //nolint:errcheck

	kubeconfigBytes, err := c.Kubeconfig(ctx)
	if err != nil {
		return fmt.Errorf("fetching kubeconfig from %s: %w", nodeIP, err)
	}

	if err := os.WriteFile(outPath, kubeconfigBytes, 0o600); err != nil {
		return fmt.Errorf("writing kubeconfig to %s: %w", outPath, err)
	}
	return nil
}

func authenticatedClient(ctx context.Context, talosconfigPath, nodeIP string) (*client.Client, error) {
	cfg, err := clientconfig.Open(talosconfigPath)
	if err != nil {
		return nil, fmt.Errorf("reading talosconfig %s: %w", talosconfigPath, err)
	}

	c, err := client.New(ctx,
		client.WithConfig(cfg),
		client.WithEndpoints(nodeIP),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", nodeIP, err)
	}
	return c, nil
}
