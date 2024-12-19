package network

import (
	"fmt"
	"strconv"
	"strings"
)

// PortMapping represents a port mapping between host and container.
type PortMapping struct {
	HostPort      uint16
	ContainerPort uint16
}

// PortMapping is a slice of PortMapping that implements flag.Value interface.
type PortMappings []PortMapping

func (p *PortMappings) String() string {
	return fmt.Sprintf("%v", *p)
}

func (p *PortMappings) Set(value string) error {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return fmt.Errorf("expect /host_port:/container_port")
	}

	hostPort, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return fmt.Errorf("invalid host port: %w", err)
	}
	containerPort, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return fmt.Errorf("invalid container port: %w", err)
	}

	*p = append(*p, PortMapping{
		HostPort:      uint16(hostPort),
		ContainerPort: uint16(containerPort),
	})
	return nil
}
