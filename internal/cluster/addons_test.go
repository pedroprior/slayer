package cluster

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"slayer/internal/config"
)

// chdirRepoRoot points the working directory at the repo root for the
// duration of the test, since renderMetalLBPool reads metallbPoolTemplate
// via a path relative to cwd (matching how the real binary is always run,
// per the Makefile) rather than an absolute one.
func chdirRepoRoot(t *testing.T) {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed to resolve test file location")
	}
	// internal/cluster/addons_test.go -> repo root is two levels up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")

	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolving current directory: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to repo root %s: %v", root, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// Regression test: the IPAddressPool used to hardcode
// 192.168.122.150-192.168.122.199 directly in manifests/metal-lb.yaml,
// independent of cluster.yaml's network.metalLBRangeStart/End. Changing the
// range in cluster.yaml silently had no effect on what got applied.
// renderMetalLBPool must reflect cfg.Network, not a fixed value.
func TestRenderMetalLBPool_UsesConfiguredRange(t *testing.T) {
	chdirRepoRoot(t)

	cfg := &config.Config{}
	cfg.Network.MetalLBRangeStart = "10.0.0.50"
	cfg.Network.MetalLBRangeEnd = "10.0.0.99"

	out, err := renderMetalLBPool(cfg)
	if err != nil {
		t.Fatalf("renderMetalLBPool() error = %v", err)
	}

	got := string(out)
	if !strings.Contains(got, "10.0.0.50-10.0.0.99") {
		t.Errorf("renderMetalLBPool() = %q, want it to contain the configured range %q", got, "10.0.0.50-10.0.0.99")
	}
	if strings.Contains(got, "192.168.122") {
		t.Errorf("renderMetalLBPool() = %q, want no trace of the old hardcoded range", got)
	}
}
