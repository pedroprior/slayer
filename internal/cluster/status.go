package cluster

import (
	"fmt"
	"strings"

	"slayer/internal/config"
	"slayer/internal/libvirt"
)

type NodeStatus struct {
	Name    string
	IP      string
	Running bool
}

func formatStatusReport(cp, worker []NodeStatus) string {
	var b strings.Builder
	writeGroup := func(label string, nodes []NodeStatus) {
		fmt.Fprintf(&b, "%s:\n", label)
		for _, n := range nodes {
			state := "not running"
			if n.Running {
				state = "running"
			}
			ip := n.IP
			if ip == "" {
				ip = "-"
			}
			fmt.Fprintf(&b, "  %-20s %-16s %s\n", n.Name, ip, state)
		}
	}
	writeGroup("Control plane", cp)
	writeGroup("Workers", worker)
	return b.String()
}

func statusForGroup(c *libvirt.Client, names []string) []NodeStatus {
	statuses := make([]NodeStatus, 0, len(names))
	for _, name := range names {
		dom, err := c.DomainLookupByName(name)
		if err != nil {
			statuses = append(statuses, NodeStatus{Name: name, Running: false})
			continue
		}
		running, _ := c.DomainIsActive(dom)
		ip, _ := c.WaitForIP(name, 1, 0)
		statuses = append(statuses, NodeStatus{Name: name, IP: ip, Running: running == 1})
	}
	return statuses
}

// Status reports the current libvirt-level running state and IP of every
// expected control-plane and worker node.
func Status(c *libvirt.Client, cfg *config.Config) (string, error) {
	cp := statusForGroup(c, nodeNames("talos-cp", cfg.ControlPlane.Count))
	worker := statusForGroup(c, nodeNames("talos-worker", cfg.Worker.Count))
	return formatStatusReport(cp, worker), nil
}
