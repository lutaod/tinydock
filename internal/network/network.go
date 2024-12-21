package network

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
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
//
// NOTE: No need to keep track of devices as kernel automatically cleans up veth devices
// when container exits.
type Endpoint struct {
	IPNet         *net.IPNet   `json:"ipnet"`
	HostInterface string       `json:"host_interface"`
	PortMappings  PortMappings `json:"port_mappings"`
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
		return fmt.Errorf("failed to parse subnet: %w", err)
	}

	gatewayIPNet, err := allocator.requestIP(ipNet, true)
	if err != nil {
		return fmt.Errorf("failed to request IP: %w", err)
	}
	ipNet.IP = gatewayIPNet.IP

	nw, err := d.create(name, ipNet)
	if err != nil {
		if releaseErr := allocator.releasePrefix(ipNet); releaseErr != nil {
			log.Printf("failed to release IP after failed network creation: %v", releaseErr)
		}
		return fmt.Errorf("failed to set up network: %w", err)
	}

	if err := enableExternalAccess(nw); err != nil {
		if releaseErr := allocator.releasePrefix(ipNet); releaseErr != nil {
			log.Printf("failed to release IP after failed network creation: %v", releaseErr)
		}
		return fmt.Errorf("failed to enable external access: %w", err)
	}

	return save(nw)
}

// Remove tears down network infrastructure specified by given name.
func Remove(name string) error {
	nw, err := load(name)
	if err != nil {
		return fmt.Errorf("failed to load network: %w", err)
	}

	d, ok := drivers[nw.Driver]
	if !ok {
		return fmt.Errorf("unsupported driver: %s", nw.Driver)
	}

	if err := disableExternalAccess(nw); err != nil {
		return fmt.Errorf("disable external access: %w", err)
	}

	if err := allocator.releasePrefix(nw.Subnet); err != nil {
		return fmt.Errorf("failed to release prefix: %w", err)
	}

	if err := d.delete(nw); err != nil {
		return fmt.Errorf("failed to delete network: %w", err)
	}

	return os.Remove(filepath.Join(networkDir, name+".json"))
}

// List displays all configured networks.
func List() error {
	networks, err := loadAll()
	if err != nil {
		return fmt.Errorf("failed to load networks: %w", err)
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

// Connect creates a network endpoint between network of given name and container specified by pid.
func Connect(name string, pid int, pms PortMappings) (*Endpoint, error) {
	nw, err := load(name)
	if err != nil {
		return nil, fmt.Errorf("failed to load network: %w", err)
	}

	d, ok := drivers[nw.Driver]
	if !ok {
		return nil, fmt.Errorf("driver not found: %s", nw.Driver)
	}

	ipNet, err := allocator.requestIP(nw.Subnet, false)
	if err != nil {
		return nil, fmt.Errorf("failed to request IP: %w", err)
	}

	ep := &Endpoint{
		IPNet:        ipNet,
		PortMappings: pms,
	}

	if err := d.connect(nw, ep, pid); err != nil {
		if releaseErr := allocator.releaseIP(ep.IPNet); releaseErr != nil {
			log.Printf("Error release IP %s: %v", ep.IPNet.String(), releaseErr)
		}
		return nil, fmt.Errorf("failed to connect to network: %w", err)
	}

	if len(pms) > 0 {
		if err := setupPortForwarding(ep); err != nil {
			if releaseErr := allocator.releaseIP(ep.IPNet); releaseErr != nil {
				log.Printf("Error releasing IP %s: %v", ep.IPNet.String(), releaseErr)
			}
			return nil, err
		}
	}

	return ep, nil
}

// Disconnect removes network endpoint and releases its resources.
func Disconnect(ep *Endpoint) error {
	if err := cleanupPortForwarding(ep); err != nil {

	}

	return allocator.releaseIP(ep.IPNet)
}

// EnableLoopback sets up loopback interface in container's network namespace.
func EnableLoopback(pid int) error {
	return withContainerNS(pid, func() error {
		lo, err := netlink.LinkByName("lo")
		if err != nil {
			return fmt.Errorf("failed to find loopback interface: %w", err)
		}

		if err := netlink.LinkSetUp(lo); err != nil {
			return fmt.Errorf("failed to set loopback up: %w", err)
		}

		return nil
	})
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
			return nil, fmt.Errorf("failed to load network: %w", err)
		}
		networks = append(networks, nw)
	}

	return networks, nil
}

// withContainerNS runs fn in target pid's network namespace.
func withContainerNS(pid int, fn func() error) error {
	hostNS, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get host namespace: %w", err)
	}
	defer hostNS.Close()

	containerNS, err := netns.GetFromPid(pid)
	if err != nil {
		return fmt.Errorf("failed to get container namespace: %w", err)
	}
	defer containerNS.Close()

	if err = netns.Set(containerNS); err != nil {
		return fmt.Errorf("failed to enter container namespace: %w", err)
	}
	defer netns.Set(hostNS)

	return fn()
}
