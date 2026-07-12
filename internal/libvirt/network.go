package libvirt

import (
	"fmt"
	"log"

	"clusterctl/internal/config"
)

func buildNetworkXML(net config.NetworkConfig) string {
	return fmt.Sprintf(`<network>
  <name>default</name>
  <forward mode="nat"/>
  <bridge name="virbr0" stp="on" delay="0"/>
  <ip address="%s" netmask="%s">
    <dhcp>
      <range start="%s" end="%s"/>
    </dhcp>
  </ip>
</network>`, net.Gateway, net.Netmask, net.DHCPStart, net.DHCPEnd)
}

// EnsureDefaultNetwork defines and starts the libvirt "default" network with
// the configured DHCP range if it doesn't already exist. If it exists, it is
// left untouched (matching script.sh's prior behavior).
func (c *Client) EnsureDefaultNetwork(net config.NetworkConfig) error {
	_, err := c.NetworkLookupByName("default")
	if err == nil {
		log.Println("libvirt network 'default' already exists, leaving it as-is")
		return nil
	}

	xml := buildNetworkXML(net)

	netHandle, err := c.NetworkDefineXML(xml)
	if err != nil {
		return fmt.Errorf("defining libvirt network 'default': %w", err)
	}
	if err := c.NetworkSetAutostart(netHandle, 1); err != nil {
		return fmt.Errorf("setting autostart on network 'default': %w", err)
	}
	if err := c.NetworkCreate(netHandle); err != nil {
		return fmt.Errorf("starting network 'default': %w", err)
	}
	log.Println("defined and started libvirt network 'default'")
	return nil
}
