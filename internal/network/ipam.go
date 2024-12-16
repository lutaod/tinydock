package network

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"

	goipam "github.com/metal-stack/go-ipam"
)

const (
	ipamDir         = "ipam"
	ipamStorageFile = "ipam.json"
)

// ipAllocator wraps IPAM operations for IP management.
type ipAllocator struct {
	ipamer goipam.Ipamer
}

// newIPAllocator creates an IP allocator with persistent storage.
func newIPAllocator() (*ipAllocator, error) {
	path := filepath.Join(networkDir, ipamDir, ipamStorageFile)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	ctx := context.Background()
	storage := goipam.NewLocalFile(ctx, path)
	ipam := goipam.NewWithStorage(storage)

	return &ipAllocator{ipamer: ipam}, nil
}

// getPrefix returns existing prefix or creates new one if allowCreate is true.
func (a *ipAllocator) getPrefix(subnet *net.IPNet, allowCreate bool) (string, error) {
	ctx := context.Background()
	if allowCreate {
		prefix, err := a.ipamer.NewPrefix(ctx, subnet.String())
		if err != nil {
			return "", fmt.Errorf("failed to create prefix: %w", err)
		}
		return prefix.Cidr, nil
	}

	prefix, err := a.ipamer.PrefixFrom(ctx, subnet.String())
	if err != nil {
		return "", fmt.Errorf("failed to get prefix: %w", err)
	}
	return prefix.Cidr, nil
}

// requestIP acquires an unused IP from given subnet.
func (a *ipAllocator) requestIP(subnet *net.IPNet, allowCreate bool) (*net.IPNet, error) {
	prefix, err := a.getPrefix(subnet, allowCreate)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	ip, err := a.ipamer.AcquireIP(ctx, prefix)
	if err != nil {
		return nil, err
	}

	return &net.IPNet{
		IP:   net.ParseIP(ip.IP.String()),
		Mask: subnet.Mask,
	}, nil
}

// releaseIP releases the IP address specified by ipNet.
func (a *ipAllocator) releaseIP(ipNet *net.IPNet) error {
	ctx := context.Background()

	if err := a.ipamer.ReleaseIPFromPrefix(ctx, ipNet.String(), ipNet.IP.String()); err != nil {
		return err
	}

	return nil
}

// releasePrefix releases a subnet. If any IP is still allocated, the operation would fail.
func (a *ipAllocator) releasePrefix(subnet *net.IPNet) error {
	ctx := context.Background()

	if err := a.releaseIP(subnet); err != nil {
		return err
	}

	if _, err := a.ipamer.DeletePrefix(ctx, subnet.String()); err != nil {
		// Prefix deletion failed due to other IP in use, neeed to reclaim bridge IP
		bridgeIP := subnet.IP.String()
		_, reclaimErr := a.ipamer.AcquireSpecificIP(ctx, subnet.String(), bridgeIP)
		if reclaimErr != nil {
			return fmt.Errorf("bridge IP reclaim failed: %v", reclaimErr)
		}
		return err
	}

	return nil
}
