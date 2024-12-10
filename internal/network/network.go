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

// Remove tears down network infrastructure specified by given name.
func Remove(name string) error {
	nw, err := load(name)
	if err != nil {
		return err
	}

	d, ok := drivers[nw.Driver]
	if !ok {
		return fmt.Errorf("unsupported driver: %s", nw.Driver)
	}

	if err := d.delete(nw); err != nil {
		return err
	}

	return os.Remove(filepath.Join(networkDir, name+".json"))
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

// load retrieves network information from disk by name.
func load(name string) (*Network, error) {
	path := filepath.Join(networkDir, name+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("network %s not found", name)
		}
		return nil, fmt.Errorf("failed to read network file: %w", err)
	}

	var nw Network
	if err := json.Unmarshal(data, &nw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal network info: %w", err)
	}

	return &nw, nil
}
