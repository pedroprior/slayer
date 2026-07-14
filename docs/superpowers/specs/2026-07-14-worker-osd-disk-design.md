# Second raw disk for worker OSDs

## Purpose

Rook-Ceph needs a raw, unformatted block device per node to create OSDs on.
Today `clusterctl provision` gives every VM exactly one disk, used as the
Talos install/root disk (`internal/libvirt/domain.go`, `disk.go`). This
change adds an optional second disk to worker VMs, sized independently from
the OS disk, so a future Rook-Ceph `CephCluster` CR has something to claim.

Scope: worker nodes only. Control-plane nodes stay single-disk (they run
etcd + apiserver, not OSDs).

## Config

`internal/config/config.go`: add `OSDDiskGB int \`yaml:"osdDiskGB"\`` to
`NodeGroup`. Default (zero/omitted) means "no second disk" — existing
`cluster.yaml` files and control-plane config are unaffected. Set it under
`worker:` only when ready to provision Ceph storage:

```yaml
worker:
  count: 3
  ramMB: 4096
  vcpus: 2
  diskGB: 40      # OS/install disk
  osdDiskGB: 40   # raw disk for Rook-Ceph OSDs (0/omit = none)
```

No new `Validate()` rule — 0 is a valid, meaningful value (opt-out).

## Domain XML

`internal/libvirt/domain.go`: add `OSDDiskGB int` to `DomainSpec`.
`buildDomainXML` appends a second `<disk>` stanza — `target dev="vdb"`,
`bus="virtio"`, backed by a qcow2 file at
`<DiskDir>/<Name>-osd.qcow2` — only when `OSDDiskGB > 0`. Omitted entirely
otherwise, so existing single-disk domains (and all control-plane domains)
render identical XML to today.

A qcow2-backed volume is fine here even though Ceph wants "raw": the guest
sees `/dev/vdb` as an ordinary block device regardless of how the host
backs it, the same way `vda` already works for the Talos install disk.

## Disk lifecycle

`internal/libvirt/disk.go`: no code changes. `EnsureDisk`/`DeleteDisk`
already take an arbitrary `name` parameter for the `.qcow2` filename, so the
OSD disk is just another call using name `"<node>-osd"` instead of
`"<node>"`. This keeps the single-purpose shape of that file intact.

## Orchestration

`internal/cluster/provision.go` (`provisionGroup`): when
`group.OSDDiskGB > 0`, call `EnsureDisk(diskDir, name+"-osd", group.OSDDiskGB)`
before `EnsureDomain`, and set `DomainSpec.OSDDiskGB`. Control-plane's
`NodeGroup.OSDDiskGB` will simply be 0 (unset in `cluster.yaml`), so this is
a no-op for control-plane without any group-type branching.

`internal/cluster/destroy.go` (`destroyNode`): unconditionally also call
`DeleteDisk(diskDir, name+"-osd")` for every node. `DeleteDisk` is already a
no-op when the volume doesn't exist, so this is safe for control-plane
nodes (never had one) and workers provisioned before this change (also
never had one) alike — no group-awareness needed here either.

## Docs

- `cluster.yaml`: add `osdDiskGB: 0` under `worker:`, matching the opt-in
  default — a fresh clone provisions no OSD disk until the user
  deliberately raises it, consistent with every other field already being
  live (not commented out) in this file.
- `PROJECT.md` §2 (repository layout) and §3 (`cluster.yaml` reference):
  document the new field next to the existing `diskGB` comment that already
  flags "bump if Rook-Ceph OSDs will live here" — that comment gets
  corrected/expanded to point at `osdDiskGB` instead, since bumping the OS
  disk alone doesn't solve the raw-device requirement.

## Testing

`internal/libvirt/domain_test.go`: extend `buildDomainXML` coverage with
two cases —
- `OSDDiskGB: 40` produces a `vdb` disk stanza pointing at
  `<name>-osd.qcow2`.
- `OSDDiskGB: 0` (today's default) produces no `vdb` stanza at all —
  regression test proving existing single-disk VMs are unaffected.

No new test file for `disk.go` (matches existing pattern — that package has
no `disk_test.go` today, since `EnsureDisk`/`DeleteDisk` are thin RPC calls
against live libvirtd, not pure logic).

## Out of scope

- Anything Rook/Ceph-side (operator manifests, `CephCluster` CR, wiring
  into `clusterctl addons`) — separate follow-up once this disk exists.
- Talos machine-config changes to expose `/dev/vdb` to workloads — Talos
  discovers block devices automatically; no machine-config change is
  expected to be needed, but this is unverified until tested against a
  real cluster.
- Resizing an already-provisioned OSD disk — `EnsureDisk` leaves existing
  volumes untouched (same as the OS disk today), so changing `osdDiskGB`
  after first provision has no effect without a manual/destroy-recreate
  step.
