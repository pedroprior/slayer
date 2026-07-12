package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ISOConfig struct {
	Src  string `yaml:"src"`
	Dest string `yaml:"dest"`
}

type LibvirtConfig struct {
	Network string `yaml:"network"`
	DiskDir string `yaml:"diskDir"`
}

type NodeGroup struct {
	Count  int `yaml:"count"`
	RAMMB  int `yaml:"ramMB"`
	VCPUs  int `yaml:"vcpus"`
	DiskGB int `yaml:"diskGB"`
}

type NetworkConfig struct {
	Subnet            string `yaml:"subnet"`
	Gateway           string `yaml:"gateway"`
	Netmask           string `yaml:"netmask"`
	DHCPStart         string `yaml:"dhcpStart"`
	DHCPEnd           string `yaml:"dhcpEnd"`
	ControlPlaneVIP   string `yaml:"controlPlaneVIP"`
	MetalLBRangeStart string `yaml:"metalLBRangeStart"`
	MetalLBRangeEnd   string `yaml:"metalLBRangeEnd"`
}

type Config struct {
	Name         string        `yaml:"name"`
	ISO          ISOConfig     `yaml:"iso"`
	Libvirt      LibvirtConfig `yaml:"libvirt"`
	ControlPlane NodeGroup     `yaml:"controlPlane"`
	Worker       NodeGroup     `yaml:"worker"`
	Network      NetworkConfig `yaml:"network"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config %s: %w", path, err)
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.ControlPlane.Count < 1 {
		return fmt.Errorf("controlPlane.count must be >= 1, got %d", c.ControlPlane.Count)
	}
	if c.Worker.Count < 1 {
		return fmt.Errorf("worker.count must be >= 1, got %d", c.Worker.Count)
	}
	if c.Network.Subnet == "" {
		return fmt.Errorf("network.subnet is required")
	}
	if c.Network.ControlPlaneVIP == "" {
		return fmt.Errorf("network.controlPlaneVIP is required")
	}
	if c.Network.MetalLBRangeStart == "" || c.Network.MetalLBRangeEnd == "" {
		return fmt.Errorf("network.metalLBRangeStart and network.metalLBRangeEnd are required")
	}
	return nil
}
