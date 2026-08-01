package cluster

import (
	"fmt"
	"log"
	"time"

	"slayer/internal/config"
	"slayer/internal/libvirt"
)

type NodeResult struct {
	Name string
	IP   string
	MAC  string
}

type ProvisionResult struct {
	ControlPlane []NodeResult
	Worker       []NodeResult
}

func nodeNames(prefix string, count int) []string {
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = fmt.Sprintf("%s-%02d", prefix, i+1)
	}
	return names
}

func provisionGroup(c *libvirt.Client, cfg *config.Config, names []string, group config.NodeGroup) ([]NodeResult, error) {
	results := make([]NodeResult, 0, len(names))
	for _, name := range names {
		mac := libvirt.DeterministicMAC(name)
		spec := libvirt.DomainSpec{
			Name:        name,
			RAMMB:       group.RAMMB,
			VCPUs:       group.VCPUs,
			DiskGB:      group.DiskGB,
			DiskDir:     cfg.Libvirt.DiskDir,
			ISOPath:     cfg.ISO.Dest,
			NetworkName: cfg.Libvirt.Network,
			OSDDiskGB:   group.OSDDiskGB,
			MAC:         mac,
		}
		if err := c.EnsureDisk(cfg.Libvirt.DiskDir, name, group.DiskGB); err != nil {
			return nil, fmt.Errorf("provisioning %s: %w", name, err)
		}
		if group.OSDDiskGB > 0 {
			if err := c.EnsureDisk(cfg.Libvirt.DiskDir, name+"-osd", group.OSDDiskGB); err != nil {
				return nil, fmt.Errorf("provisioning %s: %w", name, err)
			}
		}

		log.Printf("ensuring domain %s (%dMB RAM, %d vCPU)", name, group.RAMMB, group.VCPUs)
		if err := c.EnsureDomain(spec); err != nil {
			return nil, fmt.Errorf("provisioning %s: %w", name, err)
		}

		log.Printf("waiting for IP address on %s", name)
		ip, err := c.WaitForIP(name, 30, 5*time.Second)
		if err != nil {
			return nil, fmt.Errorf("resolving IP for %s: %w", name, err)
		}
		log.Printf("%s -> %s", name, ip)

		// Read the MAC back from libvirt rather than trusting the value we
		// requested: EnsureDomain leaves an already-defined domain's XML
		// untouched, so a domain created before this deterministic scheme
		// existed may carry a different, libvirt-auto-assigned MAC.
		actualMAC, err := c.DomainMAC(name)
		if err != nil {
			return nil, fmt.Errorf("reading MAC for %s: %w", name, err)
		}

		results = append(results, NodeResult{Name: name, IP: ip, MAC: actualMAC})
	}
	return results, nil
}

// Provision ensures the libvirt network and all control-plane/worker VMs
// exist and are running, waiting for each to receive a DHCP lease.
func Provision(c *libvirt.Client, cfg *config.Config) (*ProvisionResult, error) {
	if err := c.EnsureDefaultNetwork(cfg.Network); err != nil {
		return nil, fmt.Errorf("ensuring libvirt network: %w", err)
	}

	cpResults, err := provisionGroup(c, cfg, nodeNames("talos-cp", cfg.ControlPlane.Count), cfg.ControlPlane)
	if err != nil {
		return nil, err
	}

	workerResults, err := provisionGroup(c, cfg, nodeNames("talos-worker", cfg.Worker.Count), cfg.Worker)
	if err != nil {
		return nil, err
	}

	return &ProvisionResult{ControlPlane: cpResults, Worker: workerResults}, nil
}
