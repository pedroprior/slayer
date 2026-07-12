package libvirt

import (
	"fmt"
	"regexp"
	"time"

	golibvirt "github.com/digitalocean/go-libvirt"
)

type DomainSpec struct {
	Name        string
	RAMMB       int
	VCPUs       int
	DiskGB      int
	DiskDir     string
	ISOPath     string
	NetworkName string
	// MAC is the hardware address to assign the domain's single NIC. Set it
	// via DeterministicMAC so the address is stable across recreations and
	// callers (e.g. Talos VIP device selectors) can address the interface
	// without depending on OS-assigned interface names.
	MAC string
}

func buildDomainXML(spec DomainSpec) string {
	diskPath := fmt.Sprintf("%s/%s.qcow2", spec.DiskDir, spec.Name)
	return fmt.Sprintf(`<domain type="kvm">
  <name>%s</name>
  <memory unit="MiB">%d</memory>
  <vcpu placement="static">%d</vcpu>
  <os>
    <type arch="x86_64">hvm</type>
    <boot dev="hd"/>
    <boot dev="cdrom"/>
  </os>
  <features>
    <acpi/>
    <apic/>
  </features>
  <cpu mode="host-passthrough" check="none"/>
  <devices>
    <disk type="file" device="disk">
      <driver name="qemu" type="qcow2"/>
      <source file="%s"/>
      <target dev="vda" bus="virtio"/>
    </disk>
    <disk type="file" device="cdrom">
      <driver name="qemu" type="raw"/>
      <source file="%s"/>
      <target dev="sda" bus="sata"/>
      <readonly/>
    </disk>
    <interface type="network">
      <source network="%s"/>
      <mac address="%s"/>
      <model type="virtio"/>
    </interface>
    <console type="pty"/>
  </devices>
</domain>`, spec.Name, spec.RAMMB, spec.VCPUs, diskPath, spec.ISOPath, spec.NetworkName, spec.MAC)
}

// domainLifecycler is the subset of the libvirt RPC API used by EnsureDomain
// and Stop. It exists so tests can substitute a fake in place of a live
// libvirtd connection. *Client satisfies it via its embedded *golibvirt.Libvirt.
type domainLifecycler interface {
	DomainLookupByName(name string) (golibvirt.Domain, error)
	DomainDefineXML(xml string) (golibvirt.Domain, error)
	DomainIsActive(dom golibvirt.Domain) (int32, error)
	DomainCreate(dom golibvirt.Domain) error
	DomainShutdown(dom golibvirt.Domain) error
	DomainDestroy(dom golibvirt.Domain) error
}

// EnsureDomain defines spec.Name if it doesn't already exist, then starts it
// if it isn't currently running. The disk image at <DiskDir>/<Name>.qcow2
// must already exist (see EnsureDisk) before calling this — disk creation is
// out of scope for this function, matching the caller's responsibility in
// the orchestration layer.
func (c *Client) EnsureDomain(spec DomainSpec) error {
	return ensureDomain(c, spec)
}

func ensureDomain(c domainLifecycler, spec DomainSpec) error {
	dom, err := c.DomainLookupByName(spec.Name)
	if err != nil {
		xml := buildDomainXML(spec)
		dom, err = c.DomainDefineXML(xml)
		if err != nil {
			return fmt.Errorf("defining domain %s: %w", spec.Name, err)
		}
	}

	active, err := c.DomainIsActive(dom)
	if err != nil {
		return fmt.Errorf("checking state of domain %s: %w", spec.Name, err)
	}
	if active == 1 {
		return nil
	}

	if err := c.DomainCreate(dom); err != nil {
		return fmt.Errorf("starting domain %s: %w", spec.Name, err)
	}
	return nil
}

// libvirt normalizes attribute quoting to single quotes in the XML returned
// by DomainGetXMLDesc, regardless of how buildDomainXML wrote it.
var macAddressRE = regexp.MustCompile(`<mac address=['"]([0-9a-fA-F:]+)['"]\s*/>`)

// DomainMAC returns the hardware address of the named domain's NIC, read
// back from live libvirt state rather than assumed from DomainSpec.MAC —
// EnsureDomain leaves an already-defined domain's XML untouched, so a domain
// created before DeterministicMAC existed (or defined some other way) may
// carry a different, libvirt-auto-assigned MAC. Callers that need to target
// this exact interface (e.g. building a hardwareAddr device selector) must
// use this, not the value they passed to EnsureDomain.
func (c *Client) DomainMAC(name string) (string, error) {
	dom, err := c.DomainLookupByName(name)
	if err != nil {
		return "", fmt.Errorf("looking up domain %s: %w", name, err)
	}

	xml, err := c.DomainGetXMLDesc(dom, 0)
	if err != nil {
		return "", fmt.Errorf("reading XML for domain %s: %w", name, err)
	}

	mac, err := parseDomainMAC(xml)
	if err != nil {
		return "", fmt.Errorf("domain %s: %w", name, err)
	}
	return mac, nil
}

func parseDomainMAC(domainXML string) (string, error) {
	m := macAddressRE.FindStringSubmatch(domainXML)
	if m == nil {
		return "", fmt.Errorf("no MAC address found in XML")
	}
	return m[1], nil
}

// Stop gracefully shuts down the named domain, waiting up to
// attempts*delay for it to reach the shut-off state. If it's still running
// when attempts are exhausted, it is forcibly powered off. A domain that is
// already stopped or undefined is left as-is.
func (c *Client) Stop(name string, attempts int, delay time.Duration) error {
	return stopDomain(c, name, attempts, delay)
}

func stopDomain(c domainLifecycler, name string, attempts int, delay time.Duration) error {
	dom, err := c.DomainLookupByName(name)
	if err != nil {
		return nil // already gone, nothing to do
	}

	active, err := c.DomainIsActive(dom)
	if err != nil {
		return fmt.Errorf("checking state of domain %s: %w", name, err)
	}
	if active == 0 {
		return nil // already stopped
	}

	if err := c.DomainShutdown(dom); err != nil {
		return fmt.Errorf("shutting down domain %s: %w", name, err)
	}

	for i := 0; i < attempts; i++ {
		active, err := c.DomainIsActive(dom)
		if err == nil && active == 0 {
			return nil
		}
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}

	if err := c.DomainDestroy(dom); err != nil {
		return fmt.Errorf("forcing power-off of domain %s after graceful shutdown timed out: %w", name, err)
	}
	return nil
}

// WaitForIP polls the domain's interface addresses (via the DHCP lease
// source) until an IPv4 address is found or attempts are exhausted.
func (c *Client) WaitForIP(name string, attempts int, delay time.Duration) (string, error) {
	dom, err := c.DomainLookupByName(name)
	if err != nil {
		return "", fmt.Errorf("looking up domain %s: %w", name, err)
	}

	for i := 0; i < attempts; i++ {
		ifaces, err := c.DomainInterfaceAddresses(dom, uint32(golibvirt.DomainInterfaceAddressesSrcLease), 0)
		if err == nil {
			for _, iface := range ifaces {
				for _, addr := range iface.Addrs {
					if addr.Type == int32(golibvirt.IPAddrTypeIpv4) {
						return addr.Addr, nil
					}
				}
			}
		}
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}

	return "", fmt.Errorf("no IPv4 address found for domain %s after %d attempts", name, attempts)
}
