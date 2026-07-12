package libvirt

import (
	"crypto/sha256"
	"fmt"
)

// DeterministicMAC derives a stable MAC address for a domain name, so the
// same VM name always gets the same MAC across recreations (destroy +
// re-provision) without persisting any state. This lets Talos config
// patches (e.g. the control-plane VIP) target a specific node's NIC via a
// hardwareAddr deviceSelector, which is robust to interface renames — unlike
// matching by OS-assigned name (e.g. "ens2"), which is an emergent property
// of the PCI topology and not guaranteed to stay put if the domain XML ever
// changes (extra NIC, different bus, etc).
//
// The address uses libvirt/QEMU's conventional 52:54:00 OUI prefix (the same
// one libvirt auto-assigns) with the trailing 3 bytes derived from a hash of
// the domain name.
func DeterministicMAC(name string) string {
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", sum[0], sum[1], sum[2])
}
