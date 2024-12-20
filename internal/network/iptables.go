package network

import (
	"fmt"
	"os/exec"
)

// execIptables executes iptables command with given arguments and returns error if any.
func execIptables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iptables %v: %w: %s", args, err, out)
	}

	return nil
}

// enableExternalAccess allows given network's containers to access external networks.
func enableExternalAccess(nw *Network) error {
	return execIptables(
		"-t", "nat",
		"-A", "POSTROUTING",
		"-s", nw.Subnet.String(),
		"!", "-o", "br-"+nw.Name,
		"-j", "MASQUERADE",
	)
}

// disableExternalAccess removes iptables rule for given network's external access.
func disableExternalAccess(nw *Network) error {
	return execIptables(
		"-t", "nat",
		"-D", "POSTROUTING",
		"-s", nw.Subnet.String(),
		"!", "-o", "br-"+nw.Name,
		"-j", "MASQUERADE",
	)
}
