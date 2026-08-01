# baremetal-k8s / slayer

A Go CLI (`slayer`) that provisions a highly-available **Talos Linux**
Kubernetes cluster on **libvirt/QEMU**, entirely from declarative config —
no state file, no SSH, everything re-derived live from libvirt and Talos on
every run.

For the full architecture, command reference, and design notes, see
[`PROJECT.md`](PROJECT.md).

## Prerequisites

- libvirt/QEMU running locally, invoking user in the `libvirt` group (or root)
- Go toolchain (see `go.mod` for the version)
- `kubectl` installed
- Talos `metal-amd64.iso` available at the path configured in `cluster.yaml`
  (`iso.src`) — `make download-talos-iso` can fetch it for you

## Quickstart

```bash
# Build + test
make build
make test

# Bring up the VMs
make provision

# Install Talos + Kubernetes, fetch kubeconfig
make bootstrap

# Point kubectl at the new cluster
eval "$(make kubeconfig)"
make nodes

# Install MetalLB so LoadBalancer Services get real IPs
make addons

# Optional: install Rook-Ceph for PVC-backed storage (requires
# worker.osdDiskGB set in cluster.yaml first)
make ceph

# Check on things later
make status
make cluster-info

# Shut the lab down without losing VM state
make stop

# Tear it all down permanently
make destroy
```

Run `make help` to list all available targets.

## Configuration

Cluster shape and IP layout are declared in [`cluster.yaml`](cluster.yaml).
See [`PROJECT.md` §3](PROJECT.md#3-configuration-clusteryaml) for the full
schema and validation rules.

## Repository layout

See [`PROJECT.md` §2](PROJECT.md#2-repository-layout) for a full breakdown of
`cmd/slayer/`, `internal/`, `manifests/`, and `talos/`.
