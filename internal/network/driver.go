package network

import (
	"fmt"
	"net"
	"time"

	"github.com/vishvananda/netlink"
)

const bridgePrefix = "br-"

type Driver interface {
	// create sets up network infrastructure using given subnet.
	create(name string, subnet *net.IPNet) (*Network, error)

	// delete tears down network infrastructure for given network.
	delete(nw *Network) error

	// connect establishes connectivity between given network and namespace of specified pid.
	connect(nw *Network, ep *Endpoint, pid int) error
}

type BridgeDriver struct{}

func (d *BridgeDriver) create(name string, subnet *net.IPNet) (*Network, error) {
	bridgeName := bridgePrefix + name

	linkAttrs := netlink.NewLinkAttrs()
	linkAttrs.Name = bridgeName
	bridge := &netlink.Bridge{LinkAttrs: linkAttrs}

	if err := netlink.LinkAdd(bridge); err != nil {
		return nil, fmt.Errorf("failed to create bridge: %w", err)
	}

	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   subnet.IP,
			Mask: subnet.Mask,
		},
	}
	if err := netlink.AddrAdd(bridge, addr); err != nil {
		return nil, fmt.Errorf("failed to set bridge IP: %w", err)
	}

	if err := netlink.LinkSetUp(bridge); err != nil {
		return nil, fmt.Errorf("failed to set bridge up: %w", err)
	}

	return &Network{
		Name:   name,
		Subnet: subnet,
		Driver: "bridge",
	}, nil
}

func (d *BridgeDriver) delete(nw *Network) error {
	bridgeName := bridgePrefix + nw.Name

	link, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("failed to find bridge: %w", err)
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete bridge: %w", err)
	}

	return nil
}

func (d *BridgeDriver) connect(nw *Network, ep *Endpoint, pid int) error {
	veth, err := d.createVethPair()
	if err != nil {
		return err
	}

	if err := d.configureHostNetwork(veth, nw, pid); err != nil {
		return err
	}

	return withContainerNS(pid, func() error {
		return d.configureContainerNetwork(veth.PeerName, ep, nw)
	})
}

// createVethPair generates a new virtual ethernet pair with unique names.
func (d *BridgeDriver) createVethPair() (*netlink.Veth, error) {
	hostVethName := fmt.Sprintf("veth-%x", time.Now().UnixNano()&0xFFFFFF)
	containerVethName := "c" + hostVethName[1:]

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: hostVethName,
		},
		PeerName: containerVethName,
	}

	if err := netlink.LinkAdd(veth); err != nil {
		return nil, fmt.Errorf("failed to create veth: %w", err)
	}

	return veth, nil
}

// configureHostNetwork moves peer interface to container and connects host interface to bridge.
func (d *BridgeDriver) configureHostNetwork(veth *netlink.Veth, nw *Network, pid int) error {
	// Move container end to container namespace
	peer, err := netlink.LinkByName(veth.PeerName)
	if err != nil {
		return fmt.Errorf("failed to find peer interface: %w", err)
	}

	if err = netlink.LinkSetNsPid(peer, pid); err != nil {
		return fmt.Errorf("failed to move peer to container namespace: %w", err)
	}

	// Connect host end to bridge
	bridge, err := netlink.LinkByName(bridgePrefix + nw.Name)
	if err != nil {
		return fmt.Errorf("failed to find bridge: %w", err)
	}

	if err = netlink.LinkSetMaster(veth, bridge); err != nil {
		return fmt.Errorf("failed to connect to bridge: %w", err)
	}

	if err = netlink.LinkSetUp(veth); err != nil {
		return fmt.Errorf("failed to set host veth up: %w", err)
	}

	return nil
}

// configureContainerNetwork configures interface name, IP and routing inside container.
func (d *BridgeDriver) configureContainerNetwork(containerVeth string, ep *Endpoint, nw *Network) error {
	peer, err := netlink.LinkByName(containerVeth)
	if err != nil {
		return fmt.Errorf("failed to find container interface: %w", err)
	}

	// Rename interface to eth0 for consistency
	if err := netlink.LinkSetName(peer, "eth0"); err != nil {
		return fmt.Errorf("failed to rename peer interface: %w", err)
	}

	addr := &netlink.Addr{IPNet: ep.IPNet}
	if err := netlink.AddrAdd(peer, addr); err != nil {
		return fmt.Errorf("failed to set container IP: %w", err)
	}

	if err := netlink.LinkSetUp(peer); err != nil {
		return fmt.Errorf("failed to set container interface up: %w", err)
	}

	// Add default route
	route := &netlink.Route{
		Scope:     netlink.SCOPE_UNIVERSE,
		LinkIndex: peer.Attrs().Index,
		Gw:        nw.Subnet.IP,
		Dst:       nil,
	}
	if err := netlink.RouteAdd(route); err != nil {
		return fmt.Errorf("failed to add default route: %w", err)
	}

	return nil
}
