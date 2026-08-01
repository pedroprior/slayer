package cluster

import (
	"time"

	"slayer/internal/config"
	"slayer/internal/libvirt"
)

// Stop gracefully shuts down every expected control-plane and worker domain,
// leaving them defined so a later Provision can start them again without
// recreating disks or config. It continues past per-node failures and
// returns all of them so one bad domain doesn't block stopping the rest.
func Stop(c *libvirt.Client, cfg *config.Config) []error {
	var errs []error
	for _, name := range nodeNames("talos-cp", cfg.ControlPlane.Count) {
		if err := c.Stop(name, 12, 5*time.Second); err != nil {
			errs = append(errs, err)
		}
	}
	for _, name := range nodeNames("talos-worker", cfg.Worker.Count) {
		if err := c.Stop(name, 12, 5*time.Second); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
