package cgroups

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

const (
	cgroupRoot   = "/sys/fs/cgroup"
	cgroupSlice  = "system.slice"
	cgroupPrefix = "tinydock-"
	cgroupSuffix = ".scope"
)

// Create creates a cgroup directory for container.
func Create(containerID string) error {
	cgroupPath := filepath.Join(cgroupRoot, cgroupSlice, cgroupPrefix+containerID+cgroupSuffix)

	if err := os.MkdirAll(cgroupPath, 0755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create cgroup for container %s: %w", containerID, err)
	}

	return nil
}

// AddProcess adds container process to cgroup.
func AddProcess(containerID string, pid int) error {
	procsPath := filepath.Join(
		cgroupRoot,
		cgroupSlice,
		cgroupPrefix+containerID+cgroupSuffix,
		"cgroup.procs",
	)

	if err := writeFile(procsPath, strconv.Itoa(pid)); err != nil {
		return fmt.Errorf("failed to add cgroup for container %s: %w", containerID, err)
	}

	return nil
}

// Remove deletes cgroup directory after container process ends.
func Remove(containerID string) error {
	cgroupPath := filepath.Join(cgroupSlice, cgroupPrefix+containerID+cgroupSuffix)

	cmd := exec.Command("cgdelete", "-g", fmt.Sprintf("cpu,memory:%s", cgroupPath))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove cgroup for container %s: %w", containerID, err)
	}

	return nil
}

// SetMemoryLimit sets memory limit for container.
func SetMemoryLimit(containerID, limit string) error {
	memoryLimitPath := filepath.Join(
		cgroupRoot,
		cgroupSlice,
		cgroupPrefix+containerID+cgroupSuffix,
		"memory.max",
	)

	if err := writeFile(memoryLimitPath, limit); err != nil {
		return fmt.Errorf("failed to set memory limit for container %s: %w", containerID, err)
	}

	return nil
}

// SetCPULimit sets CPU limit for container.
func SetCPULimit(containerID string, limit float64) error {
	availableCores := runtime.NumCPU()
	if limit > float64(availableCores) {
		return fmt.Errorf(
			"specified CPU limit (%.2f) exceeds available cores (%d)",
			limit,
			availableCores,
		)
	}

	cpuLimitPath := filepath.Join(
		cgroupRoot,
		cgroupSlice,
		cgroupPrefix+containerID+cgroupSuffix,
		"cpu.max",
	)

	// Convert limit to standard format
	period := 100000
	quota := int(limit * float64(period))
	formattedLimit := fmt.Sprintf("%d %d", quota, period)

	if err := writeFile(cpuLimitPath, formattedLimit); err != nil {
		return fmt.Errorf("failed to set CPU limit for container %s: %w", containerID, err)
	}

	return nil
}

// writeFile writes content to file at specified path.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
