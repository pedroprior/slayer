# Rook-Ceph storage (`slayer ceph`)

## Purpose

`docs/superpowers/specs/2026-07-14-worker-osd-disk-design.md` gave worker
VMs an optional second raw disk (`/dev/vdb`) sized by `cluster.yaml`'s
`worker.osdDiskGB`, explicitly deferring "anything Rook/Ceph-side" as a
follow-up. This is that follow-up: install Rook, bring up a `CephCluster`
that claims `/dev/vdb` on every worker as an OSD, and expose an RBD
`StorageClass` so pods can actually get `PersistentVolumeClaim`s backed by
it.

Scope: a new `slayer ceph` subcommand, separate from `slayer addons`.
In scope — Rook operator + CRDs, a `CephCluster` CR targeting worker nodes
only, a replicated `CephBlockPool`, and a default RBD `StorageClass`.
Out of scope — Ceph dashboard, toolbox pod, CephFS/shared filesystem,
object storage (RGW), Prometheus integration. Separate follow-ups if wanted
later.

## Guard: `osdDiskGB`

`cluster.Ceph` checks `cfg.Worker.OSDDiskGB > 0` before doing anything else,
returning an error such as:

```
worker.osdDiskGB is 0 in cluster.yaml — set it to a nonzero value and
re-provision before running `slayer ceph` (there is no raw disk for OSDs)
```

Without this guard, the command would apply a `CephCluster` with zero
usable disks and leave the user diagnosing a `HEALTH_ERR` cluster instead of
a config error.

## Manifest vendoring

Following the existing `manifests/metallb-native.yaml` convention (vendored,
version-pinned, offline-safe):

- `manifests/rook-ceph-operator.yaml` — a single vendored file combining
  upstream's `crds.yaml` + `common.yaml` + `operator.yaml` at Rook **v1.19.8**,
  not the newer v1.20.x line: v1.20 made a second "ceph-csi-operator" (its
  own CRDs + Deployment, vendored upstream as `csi-operator.yaml`)
  mandatory, which would mean two operators and a CRD-establishment
  ordering dependency between two vendored files. v1.19.8 still supports
  classic in-operator CSI driver management via the `ROOK_USE_CSI_OPERATOR`
  toggle — flipped to `"false"` in the vendored copy (upstream default is
  `"true"`) to use it, keeping this a single self-contained manifest. Header
  comment documents the source URLs, version, and this rationale, matching
  `metallb-native.yaml`'s header format, with upgrade instructions (checking
  whether `ROOK_USE_CSI_OPERATOR` is still honored at a newer tag before
  blindly re-pinning).
- `manifests/rook-ceph-cluster.yaml.tmpl` — a Go-template (parallel to
  `manifests/metal-lb.yaml.tmpl`) rendering three documents from
  `cluster.yaml` values:
  - `CephCluster` (namespace `rook-ceph`): `storage.useAllNodes: false`,
    with an explicit `storage.nodes` list built from
    `nodeNames("talos-worker", cfg.Worker.Count)` (the existing unexported
    helper in `internal/cluster/provision.go`, same package — no new
    naming logic), each entry declaring device `vdb`. `mon.count` is 3 when
    `cfg.Worker.Count >= 3`, else 1 — Ceph mons need an odd quorum size, and
    a homelab with fewer than 3 workers can't sanely run 3.
  - `CephBlockPool` named `replicapool`: `replicated.size` = `min(3,
    cfg.Worker.Count)`, `failureDomain: host`.
  - `StorageClass` named `rook-ceph-block`: provisioner
    `rook-ceph.rbd.csi.ceph.com`, `pool: replicapool`, `reclaimPolicy:
    Delete`, `allowVolumeExpansion: true`, annotated
    `storageclass.kubernetes.io/is-default-class: "true"` (the only
    StorageClass in this homelab, so PVCs work with no `storageClassName`
    set).

## Orchestration

`internal/cluster/ceph.go`, mirroring `addons.go`'s shape:

```go
func Ceph(ctx context.Context, cfg *config.Config, kubeconfigPath string) error
```

1. Guard: `cfg.Worker.OSDDiskGB > 0`, else the error above.
2. `k8s.ApplyManifest` the vendored `rook-ceph-operator.yaml` (CRDs, common
   RBAC/namespace, operator Deployment).
3. Render `rook-ceph-cluster.yaml.tmpl` from `cfg` (a `renderCephCluster`
   helper, parallel to `renderMetalLBPool`).
4. Retry-apply the rendered bytes with `k8s.ApplyManifestBytes` — 20
   attempts / 3s apart, the same constants and rationale as `Addons()`'s
   MetalLB pool step: `CephCluster`/`CephBlockPool`/`StorageClass` can't be
   applied until the operator's CRDs reach `Established`, and
   `ApplyManifestBytes` already builds a fresh discovery client + RESTMapper
   on every call, so retrying the call is sufficient — no new
   cache-invalidation mechanism needed.
5. Returns once the CRs are accepted by the API server. Does **not** block
   on the Ceph cluster reaching `HEALTH_OK` (that takes real minutes as OSDs
   format and mons form quorum) — prints a hint pointing at `kubectl -n
   rook-ceph get cephcluster` for the caller to poll manually, consistent
   with `bootstrap`'s "VIP takes a little longer" note rather than adding
   blocking poll logic for something this slow.

## CLI wiring

- `cmd/slayer/ceph.go`: `newCephCmd()` cobra command `ceph`, calling
  `cluster.Ceph(cmd.Context(), cfg, "talos/kubeconfig")`, registered in
  `main.go` next to `addons`. Same shape as `newAddonsCmd()`.
- `Makefile`: new `ceph` target (`./bin/$(BIN) --config $(CONFIG) ceph`),
  added to the `.PHONY` list and `help` output next to `addons`.
- `PROJECT.md` §4 (commands): new `### slayer ceph` section documenting the
  guard, what gets installed, and the "doesn't wait for HEALTH_OK" caveat.
  §2 (repository layout): note the two new `manifests/` files and
  `internal/cluster/ceph.go`.

## Testing

`internal/cluster/ceph_test.go`, pure-function tests requiring no live
cluster (same style as `addons_test.go`'s `TestRenderMetalLBPool_...`,
reusing its `chdirRepoRoot` helper):

- `renderCephCluster` with `Worker.Count: 3` produces a `storage.nodes` list
  of exactly `talos-worker-01`, `talos-worker-02`, `talos-worker-03`, each
  with device `vdb`, and `mon.count: 3`.
- `renderCephCluster` with `Worker.Count: 1` produces `mon.count: 1` (not 3)
  — regression test for the quorum fallback.
- `renderCephCluster` with `Worker.Count: 5` produces `replicated.size: 3`
  (clamped), not 5.
- `Ceph()` returns an error without touching the cluster when
  `cfg.Worker.OSDDiskGB == 0` (no kubeconfig/manifest needed for this case —
  the guard runs first).

## Docs

- `cluster.yaml`: no changes needed — `osdDiskGB: 0` already present from
  the prior design; the guard is what tells the user to change it.
- `README.md`: add a `make ceph` step to the quickstart, after `make
  addons`, noting it's optional and requires `worker.osdDiskGB` to be set.

## Out of scope

- Ceph dashboard, toolbox pod, CephFS, RGW object storage, Prometheus
  integration — no current requirement for them.
- Waiting for/reporting `HEALTH_OK` — deferred to a manual `kubectl`
  check; revisit if that proves too manual in practice.
- Multiple StorageClasses / non-default pools — one pool, one class, matches
  today's single-cluster homelab scope.
- Resizing or migrating an existing `CephCluster` after `osdDiskGB` or
  `worker.count` changes post-install — same caveat as the OSD-disk design's
  "no resize" note; changing shape after first install needs manual Ceph
  operations, not something `slayer ceph` re-running will handle.
