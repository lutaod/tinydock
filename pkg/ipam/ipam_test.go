package ipam

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// mustParseCIDR parses a CIDR string and fails the test if parsing fails.
func mustParseCIDR(t *testing.T, cidr string) *net.IPNet {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("Failed to parse CIDR %s: %v", cidr, err)
	}
	return ipNet
}

func TestCreatePrefix(t *testing.T) {
	tests := []struct {
		name      string
		cidr      string
		wantError bool
	}{
		{
			name:      "valid prefix",
			cidr:      "192.168.1.0/24",
			wantError: false,
		},
		{
			name:      "invalid CIDR format",
			cidr:      "invalid",
			wantError: true,
		},
		{
			name:      "invalid mask",
			cidr:      "192.168.1.0/33",
			wantError: true,
		},
		{
			name:      "single IP",
			cidr:      "192.168.1.1/32",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipam, err := New(filepath.Join(t.TempDir(), "test.json"))
			if err != nil {
				t.Fatalf("Failed to create IPAM: %v", err)
			}

			err = ipam.CreatePrefix(tt.cidr)
			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestPrefixOverlap(t *testing.T) {
	tests := []struct {
		name      string
		first     string
		second    string
		wantError bool
	}{
		{
			name:      "identical networks",
			first:     "10.0.0.0/24",
			second:    "10.0.0.0/24",
			wantError: true,
		},
		{
			name:      "supernet contains subnet",
			first:     "10.0.0.0/16",
			second:    "10.0.1.0/24",
			wantError: true,
		},
		{
			name:      "subnet in supernet",
			first:     "10.0.1.0/24",
			second:    "10.0.0.0/16",
			wantError: true,
		},
		{
			name:      "adjacent networks",
			first:     "10.0.0.0/24",
			second:    "10.0.1.0/24",
			wantError: false,
		},
		{
			name:      "different networks",
			first:     "10.0.0.0/24",
			second:    "192.168.1.0/24",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipam, err := New(filepath.Join(t.TempDir(), "test.json"))
			if err != nil {
				t.Fatalf("Failed to create IPAM: %v", err)
			}

			if err := ipam.CreatePrefix(tt.first); err != nil {
				t.Fatalf("Failed to create first prefix: %v", err)
			}

			err = ipam.CreatePrefix(tt.second)
			if tt.wantError && err == nil {
				t.Error("Expected overlap error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestRequestIP(t *testing.T) {
	tests := []struct {
		name      string
		cidr      string
		prealloc  int // number of IPs to preallocate
		wantError bool
		errorMsg  string // expected error message substring
	}{
		{
			name:      "request from existing prefix",
			cidr:      "192.168.1.0/24",
			wantError: false,
		},
		{
			name:      "request from non-existent prefix",
			cidr:      "192.168.2.0/24",
			wantError: true,
			errorMsg:  "not found",
		},
		{
			name:      "request from single IP prefix",
			cidr:      "192.168.1.1/32",
			wantError: true,
			errorMsg:  "cannot allocate from /32",
		},
		{
			name:      "sequential allocation",
			cidr:      "192.168.1.0/24",
			prealloc:  3,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipam, err := New(filepath.Join(t.TempDir(), "test.json"))
			if err != nil {
				t.Fatalf("Failed to create IPAM: %v", err)
			}

			prefix := mustParseCIDR(t, tt.cidr)

			// Create prefix if we expect operations to succeed
			if !tt.wantError || tt.errorMsg != "not found" {
				if err := ipam.CreatePrefix(tt.cidr); err != nil {
					t.Fatalf("Failed to create prefix: %v", err)
				}

				// Handle preallocation
				allocated := make([]*net.IPNet, 0, tt.prealloc)
				for i := 0; i < tt.prealloc; i++ {
					ip, err := ipam.RequestIP(prefix)
					if err != nil {
						t.Fatalf("Failed preallocation: %v", err)
					}
					allocated = append(allocated, ip)
				}

				// Verify all preallocated IPs are different
				seen := make(map[string]bool)
				for _, ip := range allocated {
					if seen[ip.IP.String()] {
						t.Errorf("Duplicate IP allocated: %s", ip.IP)
					}
					seen[ip.IP.String()] = true
				}
			}

			// Perform test allocation
			ip, err := ipam.RequestIP(prefix)
			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q but got: %v", tt.errorMsg, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if ip == nil {
				t.Error("Got nil IP without error")
				return
			}

			// Verify IP is in prefix
			if !prefix.Contains(ip.IP) {
				t.Errorf("Allocated IP %s not in prefix %s", ip.IP, prefix)
			}
		})
	}
}

func TestRequestIPExhaustion(t *testing.T) {
	ipam, err := New(filepath.Join(t.TempDir(), "test.json"))
	if err != nil {
		t.Fatalf("Failed to create IPAM: %v", err)
	}

	// Create a small prefix (/30 = 4 IPs, 2 usable)
	cidr := "192.168.1.0/30"
	if err := ipam.CreatePrefix(cidr); err != nil {
		t.Fatalf("Failed to create prefix: %v", err)
	}

	prefix := mustParseCIDR(t, cidr)

	// Request IPs until exhaustion
	allocated := make(map[string]bool)
	for i := 0; i < 3; i++ {
		ip, err := ipam.RequestIP(prefix)
		if err != nil {
			if i < 2 {
				t.Fatalf("Failed to allocate IP %d: %v", i+1, err)
			}
			if !strings.Contains(err.Error(), "no available IPs") {
				t.Errorf("Expected exhaustion error, got: %v", err)
			}
			break
		}
		// Verify uniqueness
		if allocated[ip.IP.String()] {
			t.Errorf("Duplicate IP allocated: %s", ip.IP)
		}
		allocated[ip.IP.String()] = true
	}

	if len(allocated) != 2 {
		t.Errorf("Expected 2 allocated IPs in /30, got %d", len(allocated))
	}
}

func TestReleaseIP(t *testing.T) {
	tests := []struct {
		name      string
		cidr      string
		setup     func(*IPAM, *net.IPNet) *net.IPNet // returns IP to release if needed
		wantError bool
		errorMsg  string
	}{
		{
			name: "release allocated IP",
			cidr: "192.168.1.0/24",
			setup: func(ipam *IPAM, prefix *net.IPNet) *net.IPNet {
				ipam.CreatePrefix(prefix.String())
				ip, _ := ipam.RequestIP(prefix)
				return ip
			},
			wantError: false,
		},
		{
			name: "release unallocated IP",
			cidr: "192.168.1.0/24",
			setup: func(ipam *IPAM, prefix *net.IPNet) *net.IPNet {
				ipam.CreatePrefix(prefix.String())
				return &net.IPNet{
					IP:   net.ParseIP("192.168.1.5"),
					Mask: prefix.Mask,
				}
			},
			wantError: true,
			errorMsg:  "not allocated",
		},
		{
			name: "release IP from non-existent prefix",
			cidr: "192.168.2.0/24",
			setup: func(ipam *IPAM, prefix *net.IPNet) *net.IPNet {
				return &net.IPNet{
					IP:   net.ParseIP("192.168.2.5"),
					Mask: prefix.Mask,
				}
			},
			wantError: true,
			errorMsg:  "no prefix found",
		},
		{
			name: "release IP then reallocate",
			cidr: "192.168.1.0/24",
			setup: func(ipam *IPAM, prefix *net.IPNet) *net.IPNet {
				ipam.CreatePrefix(prefix.String())
				ip, _ := ipam.RequestIP(prefix)
				return ip
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipam, err := New(filepath.Join(t.TempDir(), "test.json"))
			if err != nil {
				t.Fatalf("Failed to create IPAM: %v", err)
			}

			prefix := mustParseCIDR(t, tt.cidr)
			ipToRelease := tt.setup(ipam, prefix)

			err = ipam.ReleaseIP(ipToRelease)
			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q but got: %v", tt.errorMsg, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// For the reallocation test
			if tt.name == "release IP then reallocate" {
				newIP, err := ipam.RequestIP(prefix)
				if err != nil {
					t.Errorf("Failed to reallocate IP: %v", err)
				}
				if !newIP.IP.Equal(ipToRelease.IP) {
					t.Errorf("Expected to get released IP %s back, got %s", ipToRelease.IP, newIP.IP)
				}
			}
		})
	}
}

func TestReleasePrefix(t *testing.T) {
	tests := []struct {
		name      string
		cidr      string
		setup     func(*IPAM, *net.IPNet)
		wantError bool
		errorMsg  string
	}{
		{
			name: "release empty prefix",
			cidr: "192.168.1.0/24",
			setup: func(ipam *IPAM, prefix *net.IPNet) {
				ipam.CreatePrefix(prefix.String())
			},
			wantError: false,
		},
		{
			name: "release prefix with allocated IPs",
			cidr: "192.168.1.0/24",
			setup: func(ipam *IPAM, prefix *net.IPNet) {
				ipam.CreatePrefix(prefix.String())
				ipam.RequestIP(prefix)
			},
			wantError: true,
			errorMsg:  "has 1 allocated IPs",
		},
		{
			name:      "release non-existent prefix",
			cidr:      "192.168.2.0/24",
			setup:     func(ipam *IPAM, prefix *net.IPNet) {},
			wantError: true,
			errorMsg:  "not found",
		},
		{
			name: "recreate after release",
			cidr: "192.168.1.0/24",
			setup: func(ipam *IPAM, prefix *net.IPNet) {
				ipam.CreatePrefix(prefix.String())
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipam, err := New(filepath.Join(t.TempDir(), "test.json"))
			if err != nil {
				t.Fatalf("Failed to create IPAM: %v", err)
			}

			prefix := mustParseCIDR(t, tt.cidr)
			tt.setup(ipam, prefix)

			err = ipam.ReleasePrefix(prefix)
			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q but got: %v", tt.errorMsg, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// For recreate test
			if tt.name == "recreate after release" {
				err = ipam.CreatePrefix(tt.cidr)
				if err != nil {
					t.Errorf("Failed to recreate prefix: %v", err)
				}
			}
		})
	}
}
