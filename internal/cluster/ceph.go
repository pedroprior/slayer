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
	rookOperatorManifest = "manifests/rook-ceph-operator.yaml"
	rookClusterTemplate  = "manifests/rook-ceph-cluster.yaml.tmpl"
)

// Ceph installs Rook (a pinned, vendored upstream manifest — see
// manifests/rook-ceph-operator.yaml) and a CephCluster that claims the
// worker nodes' second raw disk (/dev/vdb, from cfg.Worker.OSDDiskGB — see
// docs/superpowers/specs/2026-07-14-worker-osd-disk-design.md) as OSDs,
// plus a CephBlockPool and a default RBD StorageClass. Safe to re-run —
// server-side apply is idempotent.
//
// Returns once the CephCluster/CephBlockPool/StorageClass are accepted by
// the API server, not once Ceph reports HEALTH_OK — mons forming quorum and
// OSDs coming up takes real minutes. Poll `kubectl -n rook-ceph get
// cephcluster` for that.
func Ceph(ctx context.Context, cfg *config.Config, kubeconfigPath string) error {
	if cfg.Worker.OSDDiskGB <= 0 {
		return fmt.Errorf("worker.osdDiskGB is 0 in cluster.yaml — set it to a nonzero value and re-provision before running `slayer ceph` (there is no raw disk for OSDs)")
	}

	if err := k8s.ApplyManifest(ctx, kubeconfigPath, rookOperatorManifest); err != nil {
		return fmt.Errorf("installing rook operator: %w", err)
	}

	clusterYAML, err := renderCephCluster(cfg)
	if err != nil {
		return err
	}

	// The CephCluster/CephBlockPool/StorageClass CRs can't be applied until
	// the operator's CRDs (just applied above) reach Established in the API
	// server. Retry rather than fail on the near-certain first-run race —
	// same pattern as Addons()'s MetalLB pool step.
	const attempts = 20
	const delay = 3 * time.Second

	var applyErr error
	for i := 0; i < attempts; i++ {
		if applyErr = k8s.ApplyManifestBytes(ctx, kubeconfigPath, clusterYAML); applyErr == nil {
			return nil
		}
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("applying ceph cluster config (rook CRDs may not have become established in time): %w", applyErr)
}

// renderCephCluster renders rookClusterTemplate from cfg.Worker, so the
// applied CephCluster's storage.nodes always matches the workers slayer
// actually provisioned (talos-worker-01..NN) instead of a value hardcoded
// separately in the manifest.
func renderCephCluster(cfg *config.Config) ([]byte, error) {
	tmplBytes, err := os.ReadFile(rookClusterTemplate)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rookClusterTemplate, err)
	}

	tmpl, err := template.New("rook-ceph-cluster").Parse(string(tmplBytes))
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", rookClusterTemplate, err)
	}

	// Ceph mons need an odd quorum size; a homelab with fewer than 3
	// workers can't sanely run 3, so fall back to a single (non-HA) mon.
	monCount := 1
	if cfg.Worker.Count >= 3 {
		monCount = 3
	}

	// The failureDomain is "host", so the pool can't have more replicas
	// than there are worker hosts to place them on.
	poolSize := cfg.Worker.Count
	if poolSize > 3 {
		poolSize = 3
	}
	// requireSafeReplicaSize rejects a pool with size 1 outright — only
	// disable that guard when size 1 is unavoidable (a single-worker
	// homelab explicitly opting into no redundancy), not as a default.
	requireSafeReplicaSize := poolSize >= 2

	var buf bytes.Buffer
	data := struct {
		WorkerNodes            []string
		MonCount               int
		PoolSize               int
		RequireSafeReplicaSize bool
	}{
		WorkerNodes:            nodeNames("talos-worker", cfg.Worker.Count),
		MonCount:               monCount,
		PoolSize:               poolSize,
		RequireSafeReplicaSize: requireSafeReplicaSize,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", rookClusterTemplate, err)
	}

	return buf.Bytes(), nil
}
