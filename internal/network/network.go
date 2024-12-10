package network

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

type Network struct {
	Name   string     `json:"name"`
	Subnet *net.IPNet `json:"subnet"`
	Driver string     `json:"driver"`
}

const (
	defaultSubnet = "172.26.0.0/16"
	networkDir    = "/var/lib/tinydock/network"
)

var drivers = map[string]Driver{
	"bridge": &BridgeDriver{},
}

// Create sets up and saves a network with given name, driver, and subnet.
func Create(name, driver, subnet string) error {
	d, ok := drivers[driver]
	if !ok {
		return fmt.Errorf("unsupported driver: %s", driver)
	}

	if subnet == "" {
		subnet = defaultSubnet
	}
	_, ipNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return fmt.Errorf("invalid subnet format: %w", err)
	}

	nw, err := d.create(name, ipNet)
	if err != nil {
		return err
	}

	return save(nw)
}

// saveInfo persists network information to disk.
func save(nw *Network) error {
	if err := os.MkdirAll(networkDir, 0755); err != nil {
		return fmt.Errorf("failed to create network directory: %w", err)
	}

	data, err := json.Marshal(nw)
	if err != nil {
		return fmt.Errorf("failed to marshal network info: %w", err)
	}

	path := filepath.Join(networkDir, nw.Name+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to save network info: %w", err)
	}

	return nil
}
