package network

import (
	"fmt"
	"os/exec"
	"strconv"
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
		"-s", nw.Gateway.String(),
		"!", "-o", "br-"+nw.Name,
		"-j", "MASQUERADE",
	)
}

// disableExternalAccess removes iptables rule for given network's external access.
func disableExternalAccess(nw *Network) error {
	return execIptables(
		"-t", "nat",
		"-D", "POSTROUTING",
		"-s", nw.Gateway.String(),
		"!", "-o", "br-"+nw.Name,
		"-j", "MASQUERADE",
	)
}

// setupPortForwarding configures iptables rules for port forwarding to container.
//
// NOTE: Set `net.ipv4.conf.all.route_localnet=1` to enable localhost access.
// Without this setting, the kernel blocks localhost port forwarding after DNAT.
func setupPortForwarding(ep *Endpoint) error {
	containerIP := ep.IPNet.IP.String()

	for _, pm := range ep.PortMappings {
		if err := execIptables(
			"-t", "nat",
			"-A", "PREROUTING",
			"!", "-i", ep.HostInterface,
			"-p", "tcp",
			"--dport", strconv.Itoa(int(pm.HostPort)),
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("%s:%d", containerIP, pm.ContainerPort),
		); err != nil {
			return err
		}

		if err := execIptables(
			"-t", "nat",
			"-A", "OUTPUT",
			"-p", "tcp",
			"-d", "127.0.0.1",
			"--dport", strconv.Itoa(int(pm.HostPort)),
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("%s:%d", containerIP, pm.ContainerPort),
		); err != nil {
			return err
		}

		if err := execIptables(
			"-t", "nat",
			"-A", "POSTROUTING",
			"-p", "tcp",
			"-d", containerIP,
			"--dport", strconv.Itoa(int(pm.ContainerPort)),
			"-j", "MASQUERADE",
		); err != nil {
			return err
		}
	}

	return nil
}

// cleanupPortForwarding removes iptables rules configured for port forwarding to container.
func cleanupPortForwarding(ep *Endpoint) error {
	containerIP := ep.IPNet.IP.String()

	for _, pm := range ep.PortMappings {
		if err := execIptables(
			"-t", "nat",
			"-D", "PREROUTING",
			"!", "-i", ep.HostInterface,
			"-p", "tcp",
			"--dport", strconv.Itoa(int(pm.HostPort)),
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("%s:%d", containerIP, pm.ContainerPort),
		); err != nil {
			return err
		}

		if err := execIptables(
			"-t", "nat",
			"-D", "OUTPUT",
			"-p", "tcp",
			"-d", "127.0.0.1",
			"--dport", strconv.Itoa(int(pm.HostPort)),
			"-j", "DNAT",
			"--to-destination", fmt.Sprintf("%s:%d", containerIP, pm.ContainerPort),
		); err != nil {
			return err
		}

		if err := execIptables(
			"-t", "nat",
			"-D", "POSTROUTING",
			"-p", "tcp",
			"-d", containerIP,
			"--dport", strconv.Itoa(int(pm.ContainerPort)),
			"-j", "MASQUERADE",
		); err != nil {
			return err
		}
	}

	return nil
}
