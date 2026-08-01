package cluster

import (
	"context"
	"strings"
	"testing"

	"slayer/internal/config"
)

// Regression test for the quorum/replica logic in renderCephCluster: with
// the homelab default of 3 workers, storage.nodes must list exactly the 3
// worker names (each with device vdb) and mon.count must be 3.
func TestRenderCephCluster_ThreeWorkers(t *testing.T) {
	chdirRepoRoot(t)

	cfg := &config.Config{}
	cfg.Worker.Count = 3

	out, err := renderCephCluster(cfg)
	if err != nil {
		t.Fatalf("renderCephCluster() error = %v", err)
	}

	got := string(out)
	for _, name := range []string{"talos-worker-01", "talos-worker-02", "talos-worker-03"} {
		if !strings.Contains(got, name) {
			t.Errorf("renderCephCluster() missing worker node %q, got:\n%s", name, got)
		}
	}
	if strings.Count(got, `name: "vdb"`) != 3 {
		t.Errorf("renderCephCluster() = %q, want exactly 3 vdb device entries", got)
	}
	if !strings.Contains(got, "count: 3") {
		t.Errorf("renderCephCluster() = %q, want mon.count: 3", got)
	}
}

// Ceph mons need an odd quorum size; a homelab with fewer than 3 workers
// can't sanely run 3 of them, so renderCephCluster must fall back to a
// single mon rather than requesting 3 mons across 1-2 nodes.
func TestRenderCephCluster_FewerThanThreeWorkers_FallsBackToOneMon(t *testing.T) {
	chdirRepoRoot(t)

	cfg := &config.Config{}
	cfg.Worker.Count = 2

	out, err := renderCephCluster(cfg)
	if err != nil {
		t.Fatalf("renderCephCluster() error = %v", err)
	}

	got := string(out)
	if !strings.Contains(got, "count: 1") {
		t.Errorf("renderCephCluster() = %q, want mon.count: 1 for 2 workers", got)
	}
	if strings.Contains(got, "count: 3") {
		t.Errorf("renderCephCluster() = %q, want no mon.count: 3 for 2 workers", got)
	}
}

// The CephBlockPool's failureDomain is "host", so replica size can't exceed
// the number of worker hosts available to place replicas on — clamp at 3
// even when there are more workers than that.
func TestRenderCephCluster_ClampsPoolReplicaSize(t *testing.T) {
	chdirRepoRoot(t)

	cfg := &config.Config{}
	cfg.Worker.Count = 5

	out, err := renderCephCluster(cfg)
	if err != nil {
		t.Fatalf("renderCephCluster() error = %v", err)
	}

	got := string(out)
	if !strings.Contains(got, "size: 3") {
		t.Errorf("renderCephCluster() = %q, want replicated.size clamped to 3 for 5 workers", got)
	}
	if strings.Contains(got, "size: 5") {
		t.Errorf("renderCephCluster() = %q, want no replicated.size: 5", got)
	}
}

// A single-worker homelab can't meet requireSafeReplicaSize's minimum of 2
// — Ceph's own guard would otherwise reject the pool outright. This is the
// one case where slayer must explicitly disable that guard rather than
// leaving it at its safe default.
func TestRenderCephCluster_SingleWorker_DisablesSafeReplicaGuard(t *testing.T) {
	chdirRepoRoot(t)

	cfg := &config.Config{}
	cfg.Worker.Count = 1

	out, err := renderCephCluster(cfg)
	if err != nil {
		t.Fatalf("renderCephCluster() error = %v", err)
	}

	got := string(out)
	if !strings.Contains(got, "size: 1") {
		t.Errorf("renderCephCluster() = %q, want replicated.size: 1 for 1 worker", got)
	}
	if !strings.Contains(got, "requireSafeReplicaSize: false") {
		t.Errorf("renderCephCluster() = %q, want requireSafeReplicaSize: false for a size-1 pool", got)
	}
}

// Ceph() must refuse to touch the cluster at all when there's no raw OSD
// disk configured — the guard runs before any manifest is read/applied, so
// this must not need a live kubeconfig.
func TestCeph_ErrorsWhenNoOSDDisk(t *testing.T) {
	cfg := &config.Config{}
	cfg.Worker.Count = 3
	cfg.Worker.OSDDiskGB = 0

	err := Ceph(context.Background(), cfg, "/nonexistent/kubeconfig")
	if err == nil {
		t.Fatal("Ceph() error = nil, want an error when worker.osdDiskGB is 0")
	}
	if !strings.Contains(err.Error(), "osdDiskGB") {
		t.Errorf("Ceph() error = %q, want it to mention osdDiskGB", err)
	}
}
