package libvirt

import (
	"strings"
	"testing"

	"clusterctl/internal/config"
)

func TestBuildNetworkXML(t *testing.T) {
	net := config.NetworkConfig{
		Gateway:   "192.168.122.1",
		Netmask:   "255.255.255.0",
		DHCPStart: "192.168.122.2",
		DHCPEnd:   "192.168.122.149",
	}

	xml := buildNetworkXML(net)

	for _, want := range []string{
		`<name>default</name>`,
		`address="192.168.122.1"`,
		`netmask="255.255.255.0"`,
		`start="192.168.122.2"`,
		`end="192.168.122.149"`,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("buildNetworkXML() missing %q in:\n%s", want, xml)
		}
	}
}
