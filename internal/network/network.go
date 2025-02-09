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

	"github.com/lutaod/tinydock/internal/config"
	"github.com/lutaod/tinydock/pkg/ipam"
)

const (
	defaultDriver = "bridge"
	defaultSubnet = "172.26.0.0/16"
)

var (
	networkDir = filepath.Join(config.Root, "network")

	drivers = map[string]Driver{
		"bridge": &BridgeDriver{},
	}

	ipamer *ipam.IPAM
)

// Network represents network configuration.
type Network struct {
	Name    string     `json:"name"`
	Gateway *net.IPNet `json:"gateway"`
	Driver  string     `json:"driver"`
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
	ipamer, err = ipam.New(filepath.Join(networkDir, "ipam", "ipam.json"))
	if err != nil {
		panic(err)
	}
}

// Setup enables loopback interface for container and connects it to network if specified.
func Setup(pid int, nw string, pms PortMappings) (*Endpoint, error) {
	var endpoint *Endpoint

	if nw != "" {
		ep, err := Connect(pid, nw, pms)
		if err != nil {
			return nil, err
		}
		endpoint = ep
	}

	if err := EnableLoopback(pid); err != nil {
		return nil, err
	}

	return endpoint, nil
}

// Create sets up and saves a network with given name, driver, and subnet.
func Create(name, driver, subnet string) error {
	if driver == "" {
		driver = defaultDriver
	}
	d, ok := drivers[driver]
	if !ok {
		return fmt.Errorf("unsupported driver: %s", driver)
	}

	if subnet == "" {
		subnet = defaultSubnet
	}
	_, prefixNet, err := net.ParseCIDR(subnet)
	if err != nil {
		return fmt.Errorf("failed to parse subnet: %w", err)
	}

	// First create the prefix
	if err := ipamer.CreatePrefix(subnet); err != nil {
		return fmt.Errorf("failed to create prefix: %w", err)
	}

	// Request gateway IP from prefix
	gatewayIPNet, err := ipamer.RequestIP(prefixNet)
	if err != nil {
		if releaseErr := ipamer.ReleasePrefix(prefixNet); releaseErr != nil {
			log.Printf("failed to release prefix after IP request failure: %v", releaseErr)
		}
		return fmt.Errorf("failed to request gateway IP: %w", err)
	}

	nw, err := d.create(name, gatewayIPNet)
	if err != nil {
		// Clean up IP and prefix on failure
		if releaseErr := ipamer.ReleaseIP(gatewayIPNet); releaseErr != nil {
			log.Printf("failed to release gateway IP after network creation failure: %v", releaseErr)
		}
		if releaseErr := ipamer.ReleasePrefix(prefixNet); releaseErr != nil {
			log.Printf("failed to release prefix after network creation failure: %v", releaseErr)
		}
		return fmt.Errorf("failed to set up network: %w", err)
	}

	if err := enableExternalAccess(nw); err != nil {
		// Clean up network resources, IP, and prefix on failure
		if releaseErr := ipamer.ReleaseIP(gatewayIPNet); releaseErr != nil {
			log.Printf("failed to release gateway IP after external access failure: %v", releaseErr)
		}
		if releaseErr := ipamer.ReleasePrefix(prefixNet); releaseErr != nil {
			log.Printf("failed to release prefix after external access failure: %v", releaseErr)
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

	_, prefix, err := net.ParseCIDR(nw.Gateway.String())
	if err != nil {
		return fmt.Errorf("invalid gateway network %s: %w", nw.Gateway, err)
	}

	// Release gateway IP first
	if err := ipamer.ReleaseIP(nw.Gateway); err != nil {
		log.Printf("failed to release gateway IP: %v", err) // Log but continue
	}

	// Then release the prefix
	if err := ipamer.ReleasePrefix(prefix); err != nil {
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

	fmt.Printf("%-15s %-10s %s\n", "NAME", "DRIVER", "GATEWAY")

	for _, nw := range networks {
		fmt.Printf("%-15s %-10s %s\n",
			nw.Name,
			nw.Driver,
			nw.Gateway.String(),
		)
	}

	return nil
}

// Connect creates a network endpoint between network of given name and container specified by pid.
func Connect(pid int, name string, pms PortMappings) (*Endpoint, error) {
	nw, err := load(name)
	if err != nil {
		return nil, fmt.Errorf("failed to load network: %w", err)
	}

	d, ok := drivers[nw.Driver]
	if !ok {
		return nil, fmt.Errorf("driver not found: %s", nw.Driver)
	}

	_, prefix, err := net.ParseCIDR(nw.Gateway.String())
	if err != nil {
		return nil, fmt.Errorf("invalid gateway network %s: %w", nw.Gateway, err)
	}

	ipNet, err := ipamer.RequestIP(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to request IP: %w", err)
	}

	ep := &Endpoint{
		IPNet:        ipNet,
		PortMappings: pms,
	}

	if err := d.connect(nw, ep, pid); err != nil {
		if releaseErr := ipamer.ReleaseIP(ep.IPNet); releaseErr != nil {
			log.Printf("Error release IP %s: %v", ep.IPNet.String(), releaseErr)
		}
		return nil, fmt.Errorf("failed to connect to network: %w", err)
	}

	if len(pms) > 0 {
		if err := setupPortForwarding(ep); err != nil {
			if releaseErr := ipamer.ReleaseIP(ep.IPNet); releaseErr != nil {
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

	return ipamer.ReleaseIP(ep.IPNet)
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
