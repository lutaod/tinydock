package network

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultSubnet = "172.26.0.0/16"
	networkDir    = "/var/lib/tinydock/network"
)

var drivers = map[string]Driver{
	"bridge": &BridgeDriver{},
}

var allocator *ipAllocator

// Network represents network configuration.
type Network struct {
	Name   string     `json:"name"`
	Subnet *net.IPNet `json:"subnet"`
	Driver string     `json:"driver"`
}

// Endpoint represents network endpoint configuration for single container.
type Endpoint struct {
	IPNet *net.IPNet `json:"ipnet"`
	// TODO: Add port mapping
}

// ConnectConfig provides required parameters for connecting container to network.
type ConnectConfig struct {
	Network string
	ID      string
	PID     int
}

// init initializes global IP allocator during package load.
func init() {
	var err error
	allocator, err = newIPAllocator()
	if err != nil {
		panic(err)
	}
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
		return err
	}

	gatewayIPNet, err := allocator.requestIP(ipNet, true)
	if err != nil {
		return err
	}
	ipNet.IP = gatewayIPNet.IP

	nw, err := d.create(name, ipNet)
	if err != nil {
		if releaseErr := allocator.releasePrefix(ipNet); releaseErr != nil {
			log.Printf("failed to release IP after failed network creation: %v", releaseErr)
		}
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

	if err := allocator.releasePrefix(nw.Subnet); err != nil {
		return err
	}

	return os.Remove(filepath.Join(networkDir, name+".json"))
}

// List displays all configured networks.
func List() error {
	networks, err := loadAll()
	if err != nil {
		return err
	}

	fmt.Printf("%-15s %-10s %s\n", "NAME", "DRIVER", "SUBNET")

	for _, nw := range networks {
		fmt.Printf("%-15s %-10s %s\n",
			nw.Name,
			nw.Driver,
			nw.Subnet.String(),
		)
	}

	return nil
}

func Connect(config ConnectConfig) (*Endpoint, error) {
	nw, err := load(config.Network)
	if err != nil {
		return nil, fmt.Errorf("failed to load network: %w", err)
	}

	ipNet, err := allocator.requestIP(nw.Subnet, false)
	if err != nil {
		return nil, err
	}

	ep := &Endpoint{
		IPNet: ipNet,
	}

	d, ok := drivers[nw.Driver]
	if !ok {
		// TODO: release IP
		return nil, fmt.Errorf("driver not found: %s", nw.Driver)
	}

	if err := d.connect(nw, ep, config.PID); err != nil {
		// TODO: release IP
		return nil, err
	}

	return ep, nil
}

// save persists network information to disk.
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

// loadAll retrieves all network information from disk.
func loadAll() ([]*Network, error) {
	files, err := os.ReadDir(networkDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read network directory: %w", err)
	}

	var networks []*Network
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}

		name := strings.TrimSuffix(f.Name(), ".json")
		nw, err := load(name)
		if err != nil {
			return nil, err
		}
		networks = append(networks, nw)
	}

	return networks, nil
}
