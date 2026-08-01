package talos

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// BuildVIPPatch assigns vip to the NIC with hardware address mac, matched
// via a deviceSelector rather than an interface name like "eth0" or "ens2".
// OS-assigned interface names are an emergent property of the PCI topology
// (Linux's predictable network naming) and aren't guaranteed to stay put if
// the domain XML ever changes (extra NIC, different bus, etc); the MAC is
// fixed by libvirt at domain-definition time (see
// internal/libvirt.DeterministicMAC), so matching on it is robust to that.
func BuildVIPPatch(vip, mac string) string {
	return fmt.Sprintf(`machine:
  network:
    interfaces:
      - deviceSelector:
          hardwareAddr: %q
        dhcp: true
        vip:
          ip: %s
`, mac, vip)
}

// BuildHostnamePatch pins the node's Kubernetes/system hostname to name
// instead of Talos's default "auto: stable" generated hostname. Without
// this, the k8s Node object registers under a random-looking generated
// name (e.g. "talos-3zc-2pl") rather than the VM's domain name, which
// breaks anything that references nodes by the names slayer provisioned
// them under — e.g. the CephCluster storage.nodes list (see
// internal/cluster/ceph.go), which is built from those domain names.
//
// generate.NewInput emits a separate "HostnameConfig" document with `auto:
// stable` alongside the main v1alpha1 Config document. Talos rejects a
// config that sets machine.network.hostname while that document is still
// present ("static hostname is already set in v1alpha1 config"), so the
// patch must delete it rather than just setting the static hostname.
func BuildHostnamePatch(name string) string {
	return fmt.Sprintf(`machine:
  network:
    hostname: %s
---
apiVersion: v1alpha1
kind: HostnameConfig
$patch: delete
`, name)
}

// ApplyHostnamePatch strategic-merges a hostname patch into cfgBytes, the
// same way ApplyVIPPatch does for the VIP patch.
func ApplyHostnamePatch(cfgBytes []byte, name string) ([]byte, error) {
	patch, err := configpatcher.LoadPatch([]byte(BuildHostnamePatch(name)))
	if err != nil {
		return nil, fmt.Errorf("loading hostname patch: %w", err)
	}

	out, err := configpatcher.Apply(configpatcher.WithBytes(cfgBytes), []configpatcher.Patch{patch})
	if err != nil {
		return nil, fmt.Errorf("applying hostname patch: %w", err)
	}

	patched, err := out.Bytes()
	if err != nil {
		return nil, fmt.Errorf("marshaling patched config: %w", err)
	}

	return patched, nil
}

// GenerateConfigs produces (or reuses, if already on disk) the Talos
// control-plane and worker machine configs plus the talosctl client config
// (talosconfig) for clusterName/endpoint, writing them to
// outDir/controlplane.yaml, outDir/worker.yaml, and outDir/talosconfig.
func GenerateConfigs(clusterName, endpoint, outDir string) (controlPlaneCfg, workerCfg []byte, err error) {
	cpPath := filepath.Join(outDir, "controlplane.yaml")
	workerPath := filepath.Join(outDir, "worker.yaml")
	talosconfigPath := filepath.Join(outDir, "talosconfig")

	if existingCP, cpErr := os.ReadFile(cpPath); cpErr == nil {
		if existingWorker, workerErr := os.ReadFile(workerPath); workerErr == nil {
			if _, tcErr := os.Stat(talosconfigPath); tcErr == nil {
				return existingCP, existingWorker, nil
			}
		}
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("creating output dir %s: %w", outDir, err)
	}

	secretsBundle, err := secrets.NewBundle(secrets.NewClock(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("generating secrets bundle: %w", err)
	}

	endpointHost := endpoint
	if u, parseErr := url.Parse(endpoint); parseErr == nil && u.Hostname() != "" {
		endpointHost = u.Hostname()
	}

	// VMs use a single virtio disk (see internal/libvirt/domain.go's
	// `<target dev="vda" bus="virtio"/>`), so install there explicitly —
	// Talos has no disk to default to and rejects a config missing an
	// install disk/diskSelector.
	input, err := generate.NewInput(clusterName, endpoint, "",
		generate.WithSecretsBundle(secretsBundle),
		generate.WithInstallDisk("/dev/vda"),
		generate.WithEndpointList([]string{endpointHost}),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("building generate input: %w", err)
	}

	cpProvider, err := input.Config(machine.TypeControlPlane)
	if err != nil {
		return nil, nil, fmt.Errorf("generating control plane config: %w", err)
	}
	workerProvider, err := input.Config(machine.TypeWorker)
	if err != nil {
		return nil, nil, fmt.Errorf("generating worker config: %w", err)
	}

	cpBytes, err := cpProvider.Bytes()
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling control plane config: %w", err)
	}
	workerBytes, err := workerProvider.Bytes()
	if err != nil {
		return nil, nil, fmt.Errorf("marshaling worker config: %w", err)
	}

	talosconfig, err := input.Talosconfig()
	if err != nil {
		return nil, nil, fmt.Errorf("generating talosconfig: %w", err)
	}

	if err := os.WriteFile(cpPath, cpBytes, 0o600); err != nil {
		return nil, nil, fmt.Errorf("writing %s: %w", cpPath, err)
	}
	if err := os.WriteFile(workerPath, workerBytes, 0o600); err != nil {
		return nil, nil, fmt.Errorf("writing %s: %w", workerPath, err)
	}
	if err := talosconfig.Save(talosconfigPath); err != nil {
		return nil, nil, fmt.Errorf("writing %s: %w", talosconfigPath, err)
	}

	return cpBytes, workerBytes, nil
}

// ApplyVIPPatch strategic-merges a VIP patch targeting the NIC with hardware
// address mac into cfgBytes, producing a single valid v1alpha1 document.
// Naively concatenating the patch as a second "---"-separated YAML document
// is rejected by Talos with "duplicate document /v1alpha1/ is not allowed".
func ApplyVIPPatch(cfgBytes []byte, vip, mac string) ([]byte, error) {
	patch, err := configpatcher.LoadPatch([]byte(BuildVIPPatch(vip, mac)))
	if err != nil {
		return nil, fmt.Errorf("loading VIP patch: %w", err)
	}

	out, err := configpatcher.Apply(configpatcher.WithBytes(cfgBytes), []configpatcher.Patch{patch})
	if err != nil {
		return nil, fmt.Errorf("applying VIP patch: %w", err)
	}

	patched, err := out.Bytes()
	if err != nil {
		return nil, fmt.Errorf("marshaling patched config: %w", err)
	}

	return patched, nil
}
