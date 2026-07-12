package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cluster.yaml")
	contents := `
name: talos-homelab
iso:
  src: /tmp/metal-amd64.iso
  dest: /var/lib/libvirt/images/metal-amd64.iso
libvirt:
  network: default
  diskDir: /var/lib/libvirt/images
controlPlane:
  count: 3
  ramMB: 4096
  vcpus: 2
  diskGB: 30
worker:
  count: 3
  ramMB: 4096
  vcpus: 2
  diskGB: 40
network:
  subnet: 192.168.122.0/24
  gateway: 192.168.122.1
  netmask: 255.255.255.0
  dhcpStart: 192.168.122.2
  dhcpEnd: 192.168.122.149
  controlPlaneVIP: 192.168.122.100
  metalLBRangeStart: 192.168.122.150
  metalLBRangeEnd: 192.168.122.199
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.ControlPlane.Count != 3 {
		t.Errorf("ControlPlane.Count = %d, want 3", cfg.ControlPlane.Count)
	}
	if cfg.Network.ControlPlaneVIP != "192.168.122.100" {
		t.Errorf("Network.ControlPlaneVIP = %q, want 192.168.122.100", cfg.Network.ControlPlaneVIP)
	}
}

func TestLoad_MissingRequiredField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cluster.yaml")
	contents := `
name: talos-homelab
controlPlane:
  count: 0
worker:
  count: 3
network:
  subnet: 192.168.122.0/24
  controlPlaneVIP: 192.168.122.100
  metalLBRangeStart: 192.168.122.150
  metalLBRangeEnd: 192.168.122.199
`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want error for ControlPlane.Count == 0")
	}
}
