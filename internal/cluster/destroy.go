package cluster

import (
	"fmt"

	golibvirt "github.com/digitalocean/go-libvirt"

	"clusterctl/internal/config"
	"clusterctl/internal/libvirt"
)

func collectErrors(errs ...error) []error {
	out := make([]error, 0, len(errs))
	for _, e := range errs {
		if e != nil {
			out = append(out, e)
		}
	}
	return out
}

func destroyNode(c *libvirt.Client, diskDir, name string) error {
	dom, err := c.DomainLookupByName(name)

	var destroyErr, undefineErr error
	if err == nil {
		destroyErr = c.DomainDestroy(dom)
		undefineErr = c.DomainUndefineFlags(dom,
			golibvirt.DomainUndefineManagedSave|golibvirt.DomainUndefineNvram)
	}

	// Always attempt disk removal, even if the domain was already gone, so a
	// leftover qcow2 (e.g. from a previously failed destroy) can't cause the
	// next provision to silently reuse a disk with an old machine config and
	// PKI already installed on it.
	diskErr := c.DeleteDisk(diskDir, name)

	// Also remove the OSD disk unconditionally: DeleteDisk is a no-op when
	// the volume doesn't exist, so this is harmless for control-plane nodes
	// and workers provisioned before osdDiskGB existed.
	osdDiskErr := c.DeleteDisk(diskDir, name+"-osd")

	errs := collectErrors(destroyErr, undefineErr, diskErr, osdDiskErr)
	if len(errs) > 0 {
		return fmt.Errorf("destroying %s: %v", name, errs)
	}
	return nil
}

// Destroy stops and undefines every expected control-plane and worker
// domain, and removes their backing disk images. It continues past
// per-node failures and returns all of them so one bad domain doesn't
// block cleanup of the rest.
func Destroy(c *libvirt.Client, cfg *config.Config) []error {
	var errs []error
	for _, name := range nodeNames("talos-cp", cfg.ControlPlane.Count) {
		if err := destroyNode(c, cfg.Libvirt.DiskDir, name); err != nil {
			errs = append(errs, err)
		}
	}
	for _, name := range nodeNames("talos-worker", cfg.Worker.Count) {
		if err := destroyNode(c, cfg.Libvirt.DiskDir, name); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}
