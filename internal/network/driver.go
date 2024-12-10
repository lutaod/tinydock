package network

import (
	"fmt"
	"math/rand"
	"net"

	"github.com/vishvananda/netlink"
)

type Driver interface {
	// create sets up network infrastructure using given subnet.
	create(name string, subnet *net.IPNet) (*Network, error)

	// delete tears down network infrastructure for given network.
	// TODO: delete(nw *Network) error
}

type BridgeDriver struct{}

func (bd *BridgeDriver) create(name string, subnet *net.IPNet) (*Network, error) {
	bridgeName := fmt.Sprintf("br-%05x", rand.Intn(0x100000))

	linkAttrs := netlink.NewLinkAttrs()
	linkAttrs.Name = bridgeName
	bridge := &netlink.Bridge{LinkAttrs: linkAttrs}

	if err := netlink.LinkAdd(bridge); err != nil {
		return nil, fmt.Errorf("failed to create bridge: %w", err)
	}

	addr := &netlink.Addr{
		IPNet: subnet,
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
