package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"text/template"
	"time"

	"slayer/internal/config"
	"slayer/internal/k8s"
)

const (
	metallbInstallManifest = "manifests/metallb-native.yaml"
	metallbPoolTemplate    = "manifests/metal-lb.yaml.tmpl"
	traefikInstallManifest = "manifests/traefik.yaml"
)

// Addons installs MetalLB (a pinned, vendored upstream manifest — see
// manifests/metallb-native.yaml) and configures its IP pool from
// cfg.Network.MetalLBRangeStart/End, then installs Traefik and the Gateway
// API CRDs (a pinned, vendored `helm template` output — see
// manifests/traefik.yaml). Safe to re-run — server-side apply is idempotent.
func Addons(ctx context.Context, cfg *config.Config, kubeconfigPath string) error {
	if err := k8s.ApplyManifest(ctx, kubeconfigPath, metallbInstallManifest); err != nil {
		return fmt.Errorf("installing metallb: %w", err)
	}

	if err := k8s.ApplyManifest(ctx, kubeconfigPath, traefikInstallManifest); err != nil {
		return fmt.Errorf("installing traefik: %w", err)
	}

	poolYAML, err := renderMetalLBPool(cfg)
	if err != nil {
		return err
	}

	// The IPAddressPool/L2Advertisement CRs are intercepted by MetalLB's own
	// validating webhook, whose Service/endpoints take a few seconds to come
	// up after metallbInstallManifest's Deployment is created. Retry rather
	// than fail on the near-certain first-run race.
	const attempts = 20
	const delay = 3 * time.Second

	var applyErr error
	for i := 0; i < attempts; i++ {
		if applyErr = k8s.ApplyManifestBytes(ctx, kubeconfigPath, poolYAML); applyErr == nil {
			return nil
		}
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("applying metallb pool config (metallb webhook may not have become ready in time): %w", applyErr)
}

// renderMetalLBPool renders metallbPoolTemplate with the address range from
// cfg.Network, so the applied IPAddressPool always matches cluster.yaml
// instead of a value hardcoded separately in the manifest.
func renderMetalLBPool(cfg *config.Config) ([]byte, error) {
	tmplBytes, err := os.ReadFile(metallbPoolTemplate)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", metallbPoolTemplate, err)
	}

	tmpl, err := template.New("metal-lb-pool").Parse(string(tmplBytes))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", metallbPoolTemplate, err)
	}

	var buf bytes.Buffer
	data := struct{ RangeStart, RangeEnd string }{
		RangeStart: cfg.Network.MetalLBRangeStart,
		RangeEnd:   cfg.Network.MetalLBRangeEnd,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", metallbPoolTemplate, err)
	}

	return buf.Bytes(), nil
}
