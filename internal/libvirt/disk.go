package libvirt

import (
	"fmt"

	golibvirt "github.com/digitalocean/go-libvirt"
)

// storagePoolName is the libvirt storage pool used to hold VM disk images.
// It is a "dir" pool rooted at cfg.Libvirt.DiskDir. libvirt ships a pool
// named "default" pointed at /var/lib/libvirt/images on most installs
// (that's what script.sh relied on implicitly via virt-install), so reuse
// it by that name rather than defining a second pool over the same path,
// which libvirt rejects as a source conflict.
const storagePoolName = "default"

func buildStoragePoolXML(diskDir string) string {
	return fmt.Sprintf(`<pool type="dir">
  <name>%s</name>
  <target>
    <path>%s</path>
  </target>
</pool>`, storagePoolName, diskDir)
}

func buildVolumeXML(name string, sizeGB int) string {
	return fmt.Sprintf(`<volume>
  <name>%s.qcow2</name>
  <capacity unit="G">%d</capacity>
  <target>
    <format type="qcow2"/>
  </target>
</volume>`, name, sizeGB)
}

// ensureStoragePool defines, builds, and starts the dir-type storage pool
// backing diskDir if it doesn't already exist. libvirtd (running as root)
// owns the resulting directory permissions, which is what lets an
// unprivileged member of the libvirt group create volumes under a
// root-owned path like /var/lib/libvirt/images.
func (c *Client) ensureStoragePool(diskDir string) (golibvirt.StoragePool, error) {
	pool, err := c.StoragePoolLookupByName(storagePoolName)
	if err == nil {
		return pool, nil
	}

	pool, err = c.StoragePoolDefineXML(buildStoragePoolXML(diskDir), 0)
	if err != nil {
		return golibvirt.StoragePool{}, fmt.Errorf("defining storage pool %s: %w", storagePoolName, err)
	}
	if err := c.StoragePoolBuild(pool, 0); err != nil {
		return golibvirt.StoragePool{}, fmt.Errorf("building storage pool %s: %w", storagePoolName, err)
	}
	if err := c.StoragePoolCreate(pool, 0); err != nil {
		return golibvirt.StoragePool{}, fmt.Errorf("starting storage pool %s: %w", storagePoolName, err)
	}
	return pool, nil
}

// EnsureDisk creates a qcow2 disk image of the given size at
// <diskDir>/<name>.qcow2 via the libvirt storage API, if it doesn't already
// exist. An existing image is left untouched (matching script.sh's prior
// behavior of skipping VM creation entirely when the domain already exists).
func (c *Client) EnsureDisk(diskDir, name string, sizeGB int) error {
	pool, err := c.ensureStoragePool(diskDir)
	if err != nil {
		return err
	}

	if _, err := c.StorageVolLookupByName(pool, name+".qcow2"); err == nil {
		return nil
	}

	if _, err := c.StorageVolCreateXML(pool, buildVolumeXML(name, sizeGB), 0); err != nil {
		return fmt.Errorf("creating disk image %s.qcow2: %w", name, err)
	}
	return nil
}

// DeleteDisk removes the qcow2 disk image <diskDir>/<name>.qcow2 from the
// storage pool, if it exists. Called by destroy so a re-provisioned VM boots
// the install ISO into a fresh, unconfigured Talos (maintenance mode)
// instead of silently reusing a disk that already has a machine config and
// PKI installed on it.
func (c *Client) DeleteDisk(diskDir, name string) error {
	pool, err := c.ensureStoragePool(diskDir)
	if err != nil {
		return err
	}

	vol, err := c.StorageVolLookupByName(pool, name+".qcow2")
	if err != nil {
		return nil // already gone, nothing to do
	}

	if err := c.StorageVolDelete(vol, 0); err != nil {
		return fmt.Errorf("deleting disk image %s.qcow2: %w", name, err)
	}
	return nil
}
