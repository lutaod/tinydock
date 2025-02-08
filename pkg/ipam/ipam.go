package ipam

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// IPAM manages IP address allocation within prefixes.
type IPAM struct {
	statePath string             `json:"-"`
	Prefixes  map[string]*Prefix `json:"prefixes"`
	mu        sync.RWMutex       `json:"-"`
}

// Prefix represents a CIDR block and its allocated IPs.
type Prefix struct {
	CIDR         string   `json:"cidr"`
	AllocatedIPs []string `json:"allocated_ips"`
}

// New creates a new IPAM instance with the given state file path.
func New(statePath string) (*IPAM, error) {
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	ipam := &IPAM{
		statePath: statePath,
		Prefixes:  make(map[string]*Prefix),
	}

	if err := ipam.loadState(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	return ipam, nil
}

func (i *IPAM) loadState() error {
	data, err := os.ReadFile(i.statePath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, i); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}
	return nil
}

func (i *IPAM) saveState() error {
	data, err := json.MarshalIndent(i, "", " ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(i.statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	return nil
}

// CreatePrefix creates a new prefix for IP allocation.
func (i *IPAM) CreatePrefix(cidr string) error {
	_, prefix, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR: %w", err)
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	for existingCIDR := range i.Prefixes {
		_, existingNet, _ := net.ParseCIDR(existingCIDR)
		if prefixesOverlap(prefix, existingNet) {
			return fmt.Errorf("prefix %s overlaps with existing prefix %s", cidr, existingCIDR)
		}
	}

	i.Prefixes[cidr] = &Prefix{
		CIDR:         cidr,
		AllocatedIPs: make([]string, 0),
	}

	return i.saveState()
}

// RequestIP requests an available IP from the given prefix.
func (i *IPAM) RequestIP(prefix *net.IPNet) (*net.IPNet, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	cidr := prefix.String()
	p, exists := i.Prefixes[cidr]
	if !exists {
		return nil, fmt.Errorf("prefix %s not found", cidr)
	}

	ones, bits := prefix.Mask.Size()
	if ones == bits {
		return nil, fmt.Errorf("cannot allocate from /32 prefix")
	}

	ip := ipToUint32(prefix.IP)
	bcast := ip | ^ipToUint32(net.IP(prefix.Mask))

	ip++ // skip network address
	for ip < bcast {
		candidate := uint32ToIP(ip)
		if !contains(p.AllocatedIPs, candidate.String()) {
			p.AllocatedIPs = append(p.AllocatedIPs, candidate.String())
			if err := i.saveState(); err != nil {
				p.AllocatedIPs = p.AllocatedIPs[:len(p.AllocatedIPs)-1]
				return nil, fmt.Errorf("failed to save state: %w", err)
			}
			return &net.IPNet{
				IP:   candidate,
				Mask: prefix.Mask,
			}, nil
		}
		ip++
	}

	return nil, fmt.Errorf("no available IPs in prefix %s", cidr)
}

// ReleaseIP releases a previously allocated IP.
func (i *IPAM) ReleaseIP(ip *net.IPNet) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	var targetPrefix *Prefix
	var prefixCIDR string
	for cidr, prefix := range i.Prefixes {
		_, pfx, _ := net.ParseCIDR(cidr)
		if pfx.Contains(ip.IP) {
			targetPrefix = prefix
			prefixCIDR = cidr
			break
		}
	}

	if targetPrefix == nil {
		return fmt.Errorf("no prefix found containing IP %s", ip.IP)
	}

	ipStr := ip.IP.String()
	lastIdx := -1
	for i, allocIP := range targetPrefix.AllocatedIPs {
		if allocIP == ipStr {
			lastIdx = i
			break
		}
	}

	if lastIdx == -1 {
		return fmt.Errorf("IP %s was not allocated from prefix %s", ipStr, prefixCIDR)
	}

	// Remove IP using swap with last element
	last := len(targetPrefix.AllocatedIPs) - 1
	targetPrefix.AllocatedIPs[lastIdx] = targetPrefix.AllocatedIPs[last]
	targetPrefix.AllocatedIPs = targetPrefix.AllocatedIPs[:last]

	return i.saveState()
}

// ReleasePrefix releases a prefix if it has no allocated IPs.
func (i *IPAM) ReleasePrefix(prefix *net.IPNet) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	cidr := prefix.String()
	p, exists := i.Prefixes[cidr]
	if !exists {
		return fmt.Errorf("prefix %s not found", cidr)
	}

	if len(p.AllocatedIPs) > 0 {
		return fmt.Errorf("cannot release prefix %s: has %d allocated IPs", cidr, len(p.AllocatedIPs))
	}

	delete(i.Prefixes, cidr)
	return i.saveState()
}

func prefixesOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIP(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
