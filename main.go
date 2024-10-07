package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// Constants for cgroup configuration
const (
	cgroupRoot      = "/sys/fs/cgroup"
	cgroupName      = "tinydock"
	memoryLimit     = "100M"
	cgroupMemoryMax = "memory.max"
	cgroupProcs     = "cgroup.procs"
)

// Helper function that writes string to file
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func main() {
	// Create new cgroup directory
	cgroupPath := filepath.Join(cgroupRoot, cgroupName)
	if err := os.Mkdir(cgroupPath, 0755); err != nil && !os.IsExist(err) {
		log.Fatalf("Failed to create cgroup: %v", err)
	}

	// Set memory limit for cgroup
	memoryLimitPath := filepath.Join(cgroupPath, cgroupMemoryMax)
	if err := writeFile(memoryLimitPath, memoryLimit); err != nil {
		log.Fatalf("Failed to set memory limit: %v", err)
	}

	// Prepare stress command and its arguments
	stressCommand := "stress"
	stressArgs := []string{
		"--vm",
		"1",
		"--vm-bytes",
		"200M",
		"--timeout",
		"30s",
	}

	cmd := exec.Command(stressCommand, stressArgs...)

	// Isolate process with new namespaces
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWUSER |
			syscall.CLONE_NEWNET,
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	// Add process to cgroup
	pid := cmd.Process.Pid
	pidStr := strconv.Itoa(pid)
	if err := writeFile(filepath.Join(cgroupPath, cgroupProcs), pidStr); err != nil {
		log.Fatalf("Failed to add process to cgroup: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		log.Fatalf("Command execution failed: %v", err)
	}
}
