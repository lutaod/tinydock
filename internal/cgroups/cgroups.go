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

// Configure initializes cgroups for a container with the given id, pid, and resource limits.
func Configure(id string, pid int, cpuLimit float64, memoryLimit string) error {
	if err := create(id); err != nil {
		return err
	}

	if err := addProcess(id, pid); err != nil {
		return err
	}

	if memoryLimit != "" {
		if err := setMemoryLimit(id, memoryLimit); err != nil {
			return err
		}
	}

	if cpuLimit != 0 {
		if err := setCPULimit(id, cpuLimit); err != nil {
			return err
		}
	}

	return nil
}

// create creates a cgroup directory for container.
func create(containerID string) error {
	cgroupPath := filepath.Join(cgroupRoot, cgroupSlice, cgroupPrefix+containerID+cgroupSuffix)

	if err := os.MkdirAll(cgroupPath, 0755); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create cgroup for container %s: %w", containerID, err)
	}

	return nil
}

// addProcess adds container process to cgroup.
func addProcess(containerID string, pid int) error {
	procsPath := filepath.Join(
		cgroupRoot,
		cgroupSlice,
		cgroupPrefix+containerID+cgroupSuffix,
		"cgroup.procs",
	)

	if err := os.WriteFile(procsPath, []byte(strconv.Itoa(pid)), 0644); err != nil {
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

// setCPULimit sets CPU limit for container.
func setCPULimit(containerID string, limit float64) error {
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

	if err := os.WriteFile(cpuLimitPath, []byte(formattedLimit), 0644); err != nil {
		return fmt.Errorf("failed to set CPU limit for container %s: %w", containerID, err)
	}

	return nil
}

// setMemoryLimit sets memory limit for container.
func setMemoryLimit(containerID, limit string) error {
	memoryLimitPath := filepath.Join(
		cgroupRoot,
		cgroupSlice,
		cgroupPrefix+containerID+cgroupSuffix,
		"memory.max",
	)

	if err := os.WriteFile(memoryLimitPath, []byte(limit), 0644); err != nil {
		return fmt.Errorf("failed to set memory limit for container %s: %w", containerID, err)
	}

	return nil
}
