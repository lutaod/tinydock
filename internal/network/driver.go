package network

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

type Driver interface {
	// create sets up network infrastructure using given subnet.
	create(name string, subnet *net.IPNet) (*Network, error)

	// delete tears down network infrastructure for given network.
	delete(nw *Network) error
}

type BridgeDriver struct{}

const bridgePrefix = "br-"

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
			IP:   net.ParseIP(subnet.IP.String()),
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
