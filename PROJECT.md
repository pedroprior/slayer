# baremetal-k8s / slayer

A Go CLI (`slayer`) that provisions a highly-available **Talos Linux**
Kubernetes cluster on **libvirt/QEMU**, entirely from declarative config —
no state file, no SSH, everything re-derived live from libvirt and Talos on
every run.

For the conceptual "why" behind the design decisions (VIP, MetalLB L2 mode,
IP planning, debugging war-stories), see [`LEARNINGS.md`](LEARNINGS.md). This
document covers *what's in the repo* and *how to drive it*.

---

## 1. Architecture

```
libvirt/QEMU (host)
 └── virtual network "default" (NAT, 192.168.122.0/24)
      ├── 3× control-plane VMs   (talos-cp-01..03)
      ├── 3× worker VMs          (talos-worker-01..03)
      ├── Control-plane VIP      192.168.122.100  (kube-apiserver HA endpoint)
      └── MetalLB pool           192.168.122.150–199 (LoadBalancer Service IPs)
```

- **OS**: [Talos Linux](https://www.talos.dev/) — immutable, API-managed
  (no shell, no package manager). Configured entirely via YAML "machine
  configs" pushed over gRPC with `talosctl`/the Talos Go client.
- **Provisioner**: `slayer`, a Go binary built on native
  [`go-libvirt`](https://github.com/digitalocean/go-libvirt), the Talos
  machinery client, and `client-go`'s dynamic client — no shelling out to
  `virsh`/`talosctl`/`kubectl` binaries.
- **Networking add-on**: [MetalLB](https://metallb.universe.tf/) in L2 mode,
  giving `Service type: LoadBalancer` real IPs on bare metal.
- **No state file, by design**: every command re-queries libvirt (and, for
  `bootstrap`, reuses Talos configs already on disk) for current reality
  instead of trusting a cached record of a previous run. This makes every
  command safe to interrupt and re-run. The one thing to watch: `bootstrap`'s
  reuse check now requires `controlplane.yaml`, `worker.yaml`, *and*
  `talosconfig` to all be present, since they're generated from a single
  secrets bundle and must stay consistent with each other — a partial set
  (e.g. `talosconfig` deleted on its own) forces a full regeneration rather
  than mixing an old CA with new client certs.

---

## 2. Repository layout

```
cluster.yaml              # the cluster's declarative config (topology, network ranges)
go.mod / go.sum           # Go module: slayer
Makefile                  # build/test/lifecycle wrapper (see §5)
LEARNINGS.md              # conceptual notes / gotchas
script.sh                 # original bash prototype this CLI replaced (bootstraps everything by shelling out to virt-install/talosctl)
export_path.sh            # `export KUBECONFIG=.../talos/kubeconfig` one-liner

manifests/
  metallb-native.yaml      # vendored, pinned upstream MetalLB install (CRDs, controller, speaker, webhook)
  metal-lb.yaml.tmpl       # IPAddressPool + L2Advertisement template, rendered from cluster.yaml's network config

talos/
  controlplane.yaml         # generated Talos control-plane machine config
  worker.yaml                # generated Talos worker machine config
  talosconfig                 # generated talosctl client config (endpoints/certs)
  kubeconfig                   # generated admin kubeconfig for the cluster

cmd/slayer/            # cobra CLI wiring — one file per subcommand
  main.go                    # root command, --config flag, loads cluster.yaml
  provision.go                # `slayer provision`
  bootstrap.go                 # `slayer bootstrap`
  addons.go                     # `slayer addons`
  status.go                      # `slayer status`
  stop.go                          # `slayer stop`
  destroy.go                      # `slayer destroy`

internal/
  config/config.go           # cluster.yaml schema + Load()/Validate()
  libvirt/
    client.go                  # thin go-libvirt connection wrapper
    domain.go                   # VM (domain) XML + EnsureDomain/Stop/WaitForIP/DomainMAC
    disk.go                      # qcow2 disk image lifecycle: EnsureDisk/DeleteDisk
    mac.go                        # DeterministicMAC — stable per-node-name MAC for VIP device selectors
    network.go                     # libvirt "default" NAT network XML + EnsureDefaultNetwork
  talos/
    configgen.go                # Talos machine-config generation (secrets bundle, CP/worker configs,
                                 # talosconfig, install-disk, MAC-based VIP patch)
    client.go                    # ApplyConfig / Bootstrap / FetchKubeconfig via Talos's Go client
  cluster/
    provision.go                 # orchestrates libvirt network + VM creation for both node groups
    bootstrap.go                  # orchestrates config-gen -> apply -> etcd bootstrap -> kubeconfig fetch
    addons.go                      # installs MetalLB, renders + retry-applies the pool config from cluster.yaml
    status.go                       # libvirt-level running/IP report
    stop.go                          # graceful shutdown of all cluster domains, keeping them defined
    destroy.go                       # stop + undefine all cluster domains, deletes disk images
  k8s/apply.go                # generic server-side-apply of a multi-doc YAML manifest or byte blob (dynamic client + REST mapper)
```

---

## 3. Configuration: `cluster.yaml`

Everything about cluster shape and IP layout is declared here and validated
on load (`internal/config/config.go`):

```yaml
name: talos-homelab               # cluster name, passed to talosctl gen config
iso:
  src: /tmp/metal-amd64.iso        # Talos installer ISO on the host
  dest: /var/lib/libvirt/images/metal-amd64.iso  # where it's copied for libvirt to read
libvirt:
  network: default                 # libvirt network name VMs attach to
  diskDir: /var/lib/libvirt/images  # where VM qcow2 disks live
controlPlane:
  count: 3
  ramMB: 4096
  vcpus: 2
  diskGB: 30
worker:
  count: 3
  ramMB: 4096
  vcpus: 2
  diskGB: 40                       # OS/install disk
  osdDiskGB: 0                     # raw disk for Rook-Ceph OSDs (0/omit = none)
network:
  subnet: 192.168.122.0/24
  gateway: 192.168.122.1
  netmask: 255.255.255.0
  dhcpStart: 192.168.122.2
  dhcpEnd: 192.168.122.149          # DHCP pool for VM node addresses
  controlPlaneVIP: 192.168.122.100  # shared HA endpoint for kube-apiserver
  metalLBRangeStart: 192.168.122.150 # MetalLB LoadBalancer IP pool
  metalLBRangeEnd: 192.168.122.199
```

**IP planning rule**: `dhcpEnd` must stay below `metalLBRangeStart` (and the
VIP must sit in a gap DHCP won't hand out) — overlap causes silent ARP
conflicts. See `LEARNINGS.md` §4 for the full rationale.

Validation (`Config.Validate`) enforces: `controlPlane.count >= 1`,
`worker.count >= 1`, and that `network.subnet`, `network.controlPlaneVIP`,
`network.metalLBRangeStart/End` are non-empty. Node names are derived
automatically as `talos-cp-01..NN` / `talos-worker-01..NN` (zero-padded,
`internal/cluster.nodeNames`) — not configurable per-node.

Every `slayer` subcommand accepts `--config <path>` (default
`cluster.yaml`) via the persistent root flag in `main.go`.

---

## 4. Commands

All commands talk to libvirt over `qemu:///system` (the system-wide libvirtd
socket — requires the invoking user to be in the `libvirt` group or run as
root).

### `slayer provision`
Ensures the libvirt `default` network exists (defining it with the
configured DHCP range if missing — left untouched if it already exists), then
for each control-plane and worker node: ensures its qcow2 disk image exists
(`libvirt.EnsureDisk` — creates it at `<diskDir>/<name>.qcow2` sized per
`cluster.yaml` if missing, leaves an existing one untouched), assigns it a
MAC via `libvirt.DeterministicMAC(name)` (see §7 for why), defines the VM
domain if it doesn't exist, starts it, and polls (`WaitForIP`, 30 attempts /
5s) until it has a DHCP-leased IPv4 address. Reads the domain's *actual* MAC
back from libvirt (`Client.DomainMAC`) rather than trusting the requested
value. Prints each node's name → IP.

Idempotent: re-running defines VMs that don't exist yet and (re-)starts any
that are currently shut off, but leaves already-running VMs — and their disks
— untouched (`EnsureDomain`/`EnsureDisk`) — so `provision` also doubles as
"start" after a `stop`.

### `slayer bootstrap`
1. Re-runs `Provision` internally to get current node IPs (and MACs — see
   below) live (no trust in a prior run's output — see "no state file"
   above). Each VM's MAC comes from `Client.DomainMAC`, read back from live
   libvirt state after `EnsureDomain`, not assumed from what was requested.
2. Generates Talos control-plane/worker machine configs plus the talosctl
   client config (`talos.GenerateConfigs`) — reused as-is if
   `talos/controlplane.yaml`, `talos/worker.yaml`, *and* `talos/talosconfig`
   already exist on disk (all three, or none are reused — a partial set
   forces regeneration). Generation explicitly sets `install.disk: /dev/vda`
   (these VMs' single virtio disk — Talos has nothing to default to and
   rejects a config missing it) and an `EndpointList` derived from the
   cluster endpoint host.
3. For each control-plane node, strategic-merges a VIP patch
   (`talos.ApplyVIPPatch`, built via `configpatcher`) into the control-plane
   config, targeting that node's own NIC with a MAC-based
   `deviceSelector.hardwareAddr` rather than an interface name — see
   `internal/libvirt/mac.go`'s `DeterministicMAC` doc comment for why
   (OS-assigned names like `eth0`/`ens2` are an emergent property of PCI
   topology, not something to hardcode). Because the selector is per-node,
   the patched config is built once per control-plane node inside the apply
   loop, not shared across all 3. **Never concatenate a patch onto the base
   config as a second `---`-separated YAML document** — Talos rejects that
   with "duplicate document /v1alpha1/ is not allowed" since the config is
   already a multi-document stream (machine config +
   `ExtensionServiceConfig`/`HostnameConfig`/etc.); `configpatcher.Apply`
   merges into the existing document instead of appending one.
4. Pushes the (patched, for CP nodes) config to every node's IP in
   **insecure/maintenance-mode** (`talos.ApplyConfig`, no established trust
   yet — equivalent to `talosctl apply-config --insecure`). This only works
   while the node is still in Talos maintenance mode; a node that already has
   a config applied rejects it with `tls: certificate required` instead —
   there's no authenticated retry path today, so recovering from that means
   `slayer destroy` (which now also removes the node's disk — see below)
   and re-provisioning from scratch.
5. Bootstraps etcd on the *first* control-plane node, retrying up to 20
   times / 10s apart (the apiserver needs time after reboot) and treating
   "etcd data-dir is not empty" as success so re-running is safe.
6. Fetches the admin kubeconfig from that same node and writes it to
   `talos/kubeconfig`.

Output: `bootstrap complete, kubeconfig written to talos/kubeconfig`.

> After bootstrap, the control-plane VIP typically takes a little longer than
> the individual node IPs to become reachable (it only comes up once a node's
> apiserver is healthy) — `no route to host` against the VIP for the first
> ~30-60s is normal; poll rather than treat it as a failure.

### `slayer addons`
Two steps, both against `talos/kubeconfig` via
`internal/k8s.ApplyManifest`/`ApplyManifestBytes` — a hand-rolled
multi-document YAML apply built on `client-go`'s dynamic client + discovery
REST mapper (field manager `"slayer"`, `Force: true`), so both are safe
to re-run:

1. Installs MetalLB itself from the vendored, version-pinned
   `manifests/metallb-native.yaml` (CRDs, controller Deployment, speaker
   DaemonSet, validating webhook, `metallb-system` namespace/RBAC). Pinned
   rather than fetched at runtime so `addons` works offline and
   reproducibly; see that file's header comment for the upstream source URL
   and upgrade instructions.
2. Renders `manifests/metal-lb.yaml.tmpl` (an `IPAddressPool` +
   `L2Advertisement`) with `cfg.Network.MetalLBRangeStart/End` — the applied
   pool always matches `cluster.yaml`, it's not a value hardcoded separately
   in the manifest — and applies it, retrying up to 20 times / 3s apart.
   Retrying matters because these CRs are intercepted by the webhook step 1
   just installed, whose Service/endpoints take a few seconds to come up
   after the Deployment is created; the very first attempt failing is the
   expected common case, not an error.

> Known gotcha (see `LEARNINGS.md` §5): Talos labels control-plane nodes
> `node.kubernetes.io/exclude-from-external-load-balancers`, which MetalLB's
> **speaker** DaemonSet honors by default. In a 3-worker homelab this can
> matter if workers are down; the fix is patching `speaker` (not
> `controller`!) with `--ignore-exclude-lb`. `slayer addons` does not do
> this patch automatically — it's a manual `kubectl patch` step today.

### `slayer status`
For each expected control-plane/worker node name, looks it up in libvirt and
reports whether the domain exists/is running and its current DHCP IP (best
effort — a domain that's down obviously has no lease). Purely read-only,
libvirt-level — it does **not** check Kubernetes/Talos health.

### `slayer stop`
Gracefully shuts down (`DomainShutdown`, an ACPI power signal — Talos gets a
chance to exit cleanly) every expected control-plane and worker domain,
polling up to 12 times / 5s (~60s total) for it to reach the shut-off state.
If a domain is still running once that budget is exhausted, it's forcibly
powered off (`DomainDestroy`). Domains are **not** undefined — disks and
config are left in place so a later `slayer provision` starts them again
without recreating anything. A domain that's already stopped or doesn't
exist is left as-is. Continues past per-node failures and reports all of
them at the end, same as `destroy`.

### `slayer destroy --yes`
Stops (`DomainDestroy`) and undefines (`DomainUndefineFlags`, clearing
managed-save state and NVRAM) every expected control-plane and worker domain,
**and deletes its backing qcow2 disk image** (`libvirt.DeleteDisk`). Disk
removal is always attempted, even for a domain that was already gone, so a
disk left over from an earlier partial/failed destroy can't linger.
**Requires `--yes`** — without it the command refuses to run. Continues past
per-node failures and reports all of them at the end, so one broken domain
doesn't block cleanup of the rest. A domain (or disk) that's already gone is
treated as success (nothing to do).

> Disk deletion matters for correctness, not just cleanliness:
> `EnsureDisk`/`EnsureDomain` are idempotent and leave an existing qcow2 or
> domain untouched. Without deleting the disk, a "fresh" VM created after
> `destroy` would silently reuse the old disk — which already has Talos
> installed and its own PKI on it — and reject `slayer bootstrap`'s
> insecure/maintenance-mode config push with `tls: certificate required`
> instead of accepting it as a new node. `destroy` followed by `bootstrap`
> must produce a genuinely fresh install every time.

### `slayer version`
Prints `slayer dev`. The only subcommand that skips loading
`cluster.yaml` (see the `PersistentPreRunE` special-case in `main.go`).

---

## 5. Makefile targets

```
make help          # list all targets with descriptions
make build          # go build -> ./bin/slayer
make install         # go install ./cmd/slayer (to $GOPATH/bin)
make test             # go test ./...
make vet               # go vet ./...
make fmt                 # go fmt ./...
make tidy                 # go mod tidy
make clean                 # rm -rf bin

make provision       # ./bin/slayer provision
make bootstrap        # ./bin/slayer bootstrap
make addons            # ./bin/slayer addons
make status              # ./bin/slayer status
make stop                  # ./bin/slayer stop
make destroy               # ./bin/slayer destroy --yes   (DESTRUCTIVE)

make kubeconfig      # print `export KUBECONFIG=.../talos/kubeconfig`
make nodes             # kubectl get nodes -o wide  (KUBECONFIG=talos/kubeconfig)
make cluster-info       # kubectl cluster-info        (KUBECONFIG=talos/kubeconfig)
```

Every lifecycle target (`provision`/`bootstrap`/`addons`/`status`/`stop`/`destroy`)
depends on `build`, so the binary is always rebuilt from current source
before use, and each reads `--config cluster.yaml` (override by editing the
`Makefile`'s `CONFIG` variable if you use a different config file).

---

## 6. End-to-end walkthrough

```bash
# 0. Prerequisites: libvirt/qemu running, talosctl not required (native Go
#    client is used), kubectl installed, Talos ISO at cluster.yaml's iso.src.

# 1. Build + test
make build
make test

# 2. Bring up the VMs
make provision
#   Control planes:
#     talos-cp-01 -> 192.168.122.x
#     ...
#   Workers:
#     talos-worker-01 -> 192.168.122.y
#     ...

# 3. Install Talos + Kubernetes, fetch kubeconfig
make bootstrap
#   bootstrap complete, kubeconfig written to talos/kubeconfig

# 4. Point kubectl at the new cluster
eval "$(make kubeconfig)"     # or: export KUBECONFIG=$PWD/talos/kubeconfig
make nodes

# 5. Install MetalLB so LoadBalancer Services get real IPs
make addons

# 6. (if needed) work around the exclude-from-external-load-balancers gotcha
kubectl patch daemonset speaker -n metallb-system --type json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--ignore-exclude-lb"}]'

# 7. Check on things later
make status
make cluster-info

# 8. Shut the lab down for the night without losing VM state
make stop

# 9. Bring it back up later (starts the existing, already-defined VMs)
make provision

# 10. Tear it all down permanently
make destroy
```

---

## 7. Design notes worth knowing before modifying the code

- **No SSH, no shell-out to `talosctl`/`virsh`**: everything goes through
  native Go clients (`go-libvirt`, Talos's `pkg/machinery/client`,
  `client-go`'s dynamic client). If you add a new capability, prefer
  extending these clients over shelling out — that's the pattern the whole
  codebase follows.
- **Idempotency over state tracking**: there is intentionally no
  `slayer.state.json` or similar. Every command re-derives truth from
  libvirt/disk on each invocation (`EnsureDomain`, `EnsureDefaultNetwork`,
  `GenerateConfigs`'s "reuse if present" check, `isAlreadyBootstrapped`,
  server-side-apply's inherent idempotency). Keep new features consistent
  with this if possible, rather than introducing a cache that can drift from
  reality.
- **`--insecure` in `talos.ApplyConfig`** is only safe because it targets a
  node still in Talos maintenance mode on a private/NAT'd libvirt network —
  don't reuse that TLS config path for anything reachable outside the lab.
- **Never build a Talos config by string-concatenating a patch document onto
  the base config.** The generated config is already a
  multi-document YAML stream (machine config + `ExtensionServiceConfig` /
  `HostnameConfig` / etc.), so appending another bare document — even one
  Talos would coerce to `v1alpha1` by default — produces a *second*
  `v1alpha1` document and gets rejected with "duplicate document /v1alpha1/
  is not allowed". Always merge patches through
  `github.com/siderolabs/talos/pkg/machinery/config/configpatcher`
  (`configpatcher.LoadPatch` + `configpatcher.Apply`), as
  `talos.ApplyVIPPatch` does.
- **Target NICs by MAC, not by OS-assigned interface name.** The VIP patch
  (`talos.BuildVIPPatch`) uses a `deviceSelector.hardwareAddr` matching
  `libvirt.DeterministicMAC(nodeName)` — a stable, per-node-name-derived
  `52:54:00:xx:xx:xx` address embedded in the domain XML at define time —
  rather than an interface name like `eth0` or `ens2`. Interface names are
  assigned by Linux's predictable network naming based on PCI topology; they
  happen to be consistently `ens2` on these VMs today, but that's an
  emergent property of the current single-NIC `domain.go` XML, not a
  contract. A hardcoded name silently leaves the VIP unbound (no error — the
  interface selector just never matches) if the topology ever changes.
  Because each control-plane node has a distinct MAC, the patched config
  must be built once per node (see `cluster.Bootstrap`'s apply loop), not
  shared across all of them the way the unpatched worker config is.
- **`Client.DomainMAC` re-reads the MAC from live libvirt state** (parsing
  `DomainGetXMLDesc`'s output) rather than trusting the value passed to
  `EnsureDomain`. This matters because `EnsureDomain` leaves an
  already-defined domain's XML untouched — a domain created before
  `DeterministicMAC` existed, or defined by some other tool, may carry a
  different, libvirt-auto-assigned MAC. Note libvirt normalizes attribute
  quoting to single quotes in the XML it returns
  (`<mac address='...'/>`), regardless of the double-quoted form
  `buildDomainXML` wrote — the parsing regex has to accept both.
- **Never hardcode a value in an applied manifest that also exists in
  `cluster.yaml`.** `manifests/metal-lb.yaml.tmpl`'s address range used to be
  a literal `192.168.122.150-192.168.122.199` that happened to match the
  defaults — changing `cluster.yaml`'s `metalLBRangeStart/End` had zero
  effect on what got applied, silently. It's now a `text/template` rendered
  from `cfg.Network` in `cluster.Addons` (`renderMetalLBPool`). If you add
  another manifest that needs a value already expressed in `cluster.yaml`,
  template it the same way rather than duplicating the literal.
- **Installing a CRD/webhook and applying a CR it validates is two apply
  calls with an inherent race, not one.** `slayer addons` applies
  `metallb-native.yaml` (which creates MetalLB's controller Deployment and
  `ValidatingWebhookConfiguration`) and then the `IPAddressPool`/
  `L2Advertisement` CRs the webhook intercepts. The webhook's Service has no
  endpoints until the controller pod is actually up, so the very first CR
  apply attempt failing is the expected common case — `cluster.Addons`
  retries (20 attempts / 3s) rather than treating that as an error. If you
  add another addon with the same CRD-then-CR shape, follow this pattern
  instead of assuming a single apply will succeed.
- **Node naming is not configurable per-node** — only counts/sizing are.
  `nodeNames()` in `internal/cluster` always produces `talos-cp-01..NN` /
  `talos-worker-01..NN`. Anything that needs per-node customization (e.g.
  per-node disk sizes) would need schema changes in `internal/config`.
- **`script.sh`** is the original bash prototype (kept for reference/diffing
  behavior) — the Go CLI is a faithful rewrite of the same sequence
  (network → VMs → IP discovery → gen config → apply → bootstrap →
  kubeconfig), so if something in `slayer` behaves unexpectedly,
  `script.sh`'s comments are a good source of "what was this supposed to
  do."

---

## 8. Testing

Unit tests exist per-package (`*_test.go` alongside each `internal/*`
package) and run with `make test` / `go test ./...`. There is no
integration/e2e test that actually spins up libvirt VMs — provisioning
behavior is validated manually against a real libvirt host.

Most tests exercise pure logic (XML building, name generation, YAML
splitting) with no libvirt connection at all. The exception is
`internal/libvirt/domain_test.go`'s coverage of `EnsureDomain`/`Stop`: their
branching (define-vs-skip, start-vs-leave-running, graceful-shutdown-vs-
force-destroy) is tested against a `fakeDomainClient` — an in-memory stand-in
implementing the small `domainLifecycler` interface (`DomainLookupByName`,
`DomainDefineXML`, `DomainIsActive`, `DomainCreate`, `DomainShutdown`,
`DomainDestroy`) that `*Client` also happens to satisfy via its embedded
`*golibvirt.Libvirt`. The exported `Client` methods are thin wrappers
(`ensureDomain`/`stopDomain`) so the logic is tested without touching a real
libvirtd. If you extend this pattern to other `Client` methods (e.g.
`WaitForIP`, network lifecycle), follow the same shape: extract the minimal
interface used, put the testable logic in an unexported function taking that
interface, keep the `Client` method as a one-line delegator.

Same shape applies to `Client.DomainMAC`: the actual XML-attribute parsing
lives in an unexported `parseDomainMAC(xml string)`, tested directly in
`domain_test.go` against both single- and double-quoted `<mac address=.../>`
forms (`TestParseDomainMAC_HandlesSingleAndDoubleQuotes`) — this is a
regression test for a real bug hit during development: libvirt's
`DomainGetXMLDesc` normalizes to single quotes regardless of how
`buildDomainXML` wrote the domain, and a regex anchored to double quotes only
silently failed to find a live domain's MAC.

`internal/libvirt/mac_test.go` covers `DeterministicMAC`: same name → same
MAC across calls, different names → different MACs, and the expected
`52:54:00:` OUI prefix.

`internal/talos/configgen_test.go`'s `TestApplyVIPPatch` generates a real
control-plane config via the actual `generate` package (not a fixture) and
round-trips it through `ApplyVIPPatch`, asserting the result still parses as
a single valid Talos config (`configloader.NewFromBytes`) and contains both
the VIP and the MAC device selector — this is the regression test for the
"duplicate document /v1alpha1/" bug: it would fail immediately if
`ApplyVIPPatch` ever went back to concatenating YAML documents instead of
merging through `configpatcher`.

`internal/cluster/addons_test.go`'s `TestRenderMetalLBPool_UsesConfiguredRange`
is the regression test for the hardcoded-IP-range bug above: it renders
`manifests/metal-lb.yaml.tmpl` with a range that doesn't match the file's old
literal default, and asserts both that the configured range appears and that
no trace of the old `192.168.122...` literal survives. Since the template is
read from a path relative to cwd (matching how the real binary always runs,
per the Makefile), the test's `chdirRepoRoot` helper temporarily `os.Chdir`s
to the repo root (resolved via `runtime.Caller`) rather than assuming
`go test`'s default per-package working directory happens to be right.

None of this replaces spinning up a real cluster to check end-to-end
behavior (VIP convergence timing, node `Ready` status, MetalLB webhook
readiness racing the pool apply, etc.) — that's still manual, against a real
libvirt host, as noted above. The MetalLB install/pool-templating change was
verified this way: applied against a live 6-node cluster, confirmed the
controller + 6 speaker pods reached `Running` and the `IPAddressPool` picked
up the range from `cluster.yaml`, then confirmed changing
`metalLBRangeStart` in `cluster.yaml` and re-running `addons` actually
changed the live `IPAddressPool` (and changing it back restored it) — proof
the template is truly wired to config, not just passing a unit test in
isolation.
