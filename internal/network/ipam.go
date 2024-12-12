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

// requestIP acquires an unused IP from given subnet.
func (a *ipAllocator) requestIP(subnet *net.IPNet) (net.IP, error) {
	ctx := context.Background()
	prefix, err := a.ipamer.NewPrefix(ctx, subnet.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create prefix: %w", err)
	}

	ip, err := a.ipamer.AcquireIP(ctx, prefix.Cidr)
	if err != nil {
		return nil, err
	}

	return net.ParseIP(ip.IP.String()), nil
}

// releasePrefix releases a subnet and all its allocated IPs.
func (a *ipAllocator) releasePrefix(subnet *net.IPNet) error {
	ctx := context.Background()

	if err := a.ipamer.ReleaseIPFromPrefix(ctx, subnet.String(), subnet.IP.String()); err != nil {
		return err
	}

	if _, err := a.ipamer.DeletePrefix(ctx, subnet.String()); err != nil {
		return err
	}

	return nil
}
