package container

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/lutaod/tinydock/internal/cgroups"
)

// Create spawns a container process that initially acts as the init process (PID 1) before
// being replaced by user command.
func Create(interactive bool, memoryLimit string, cpuLimit float64, args []string) error {
	// Create unnamed pipe for passing user command
	reader, writer, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %w", err)
	}

	// Prepare to re-execute current program with "init" argument
	cmd := exec.Command("/proc/self/exe", "init")

	// Pass read end of pipe as fd 3 to container process
	cmd.ExtraFiles = []*os.File{reader}

	// Set up namespace isolation for container
	// NOTE: CLONE_NEWUSER is removed for mounting procfs
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET,
	}

	if interactive {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// NOTE: fs extracted from busybox image will be used as container root
	cmd.Dir = "/root/busybox"

	// Spawn container process
	if err := cmd.Start(); err != nil {
		reader.Close()
		return fmt.Errorf("failed to initialize container: %w", err)
	}
	reader.Close()

	// Write user command to container process
	writeArgsToPipe(writer, args)

	pid := cmd.Process.Pid
	log.Printf("Container process started with PID %d", cmd.Process.Pid)

	// Generate a unique container ID and initialize its corresponding cgroup
	containerID, err := cgroups.Create()
	if err != nil {
		return err
	}

	// Ensure cgroup is removed after container process exits
	defer func() {
		if err := cgroups.Remove(containerID); err != nil {
			log.Printf("Container %s cleanup error: %v", containerID, err)
		}
	}()

	if err := cgroups.AddProcess(containerID, pid); err != nil {
		return err
	}

	// Set memory and CPU limits if provided
	if memoryLimit != "" {
		if err := cgroups.SetMemoryLimit(containerID, memoryLimit); err != nil {
			return err
		}
	}
	if cpuLimit != 0 {
		if err := cgroups.SetCPULimit(containerID, cpuLimit); err != nil {
			return err
		}
	}

	log.Printf("Container %s cgroups initialized", containerID)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("failed to wait for conatiner: %w", err)
	}

	return nil
}

// Run takes over after container creation and executes user command inside container.
func Run() error {
	// Retrieve command arguments written by parent process
	argv, err := readArgsFromPipe()
	if err != nil {
		return err
	}

	if err := setupMounts(); err != nil {
		return err
	}

	// Find absolute path of command
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("command not found: %w", err)
	}

	// Execute user command in place of current process
	if err := syscall.Exec(path, argv, os.Environ()); err != nil {
		return err
	}

	return nil
}

// writeArgsToPipe writes command arguments to write end of a pipe.
func writeArgsToPipe(writer *os.File, args []string) error {
	// Write args as single string with newline separators
	argsString := strings.Join(args, "\n")
	if _, err := writer.Write([]byte(argsString)); err != nil {
		return fmt.Errorf("failed to write to pipe: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close pipe: %w", err)
	}

	return nil
}

// readArgsFromPipe reads command arguments from pipe on fd 3.
func readArgsFromPipe() ([]string, error) {
	reader := os.NewFile(uintptr(3), "pipe")
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read from pipe: %w", err)
	}

	// Expect newline-separated values
	args := strings.Split(strings.TrimSpace(string(data)), "\n")

	return args, nil
}

// setupMounts configures container mounts and root filesystem.
func setupMounts() error {
	// Get new root (set by cmd.Dir in parent)
	newRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Make container mounts private to prevent propagation to host
	mountPropagationFlags := syscall.MS_SLAVE | syscall.MS_REC
	if err := syscall.Mount("", "/", "", uintptr(mountPropagationFlags), ""); err != nil {
		return fmt.Errorf("failed to modify root mount propagation: %w", err)
	}

	// Create bind mount of new rootfs for pivot_root
	mountBindFlags := syscall.MS_BIND | syscall.MS_REC
	if err := syscall.Mount(newRoot, newRoot, "", uintptr(mountBindFlags), ""); err != nil {
		return fmt.Errorf("failed to create bind mount: %w", err)
	}

	// Change working directory to new root before pivot_root
	if err := os.Chdir(newRoot); err != nil {
		return fmt.Errorf("failed to change directory: %w", err)
	}

	// Create temporary directory for old root
	putOld := ".old_root"
	if err := os.MkdirAll(putOld, 0700); err != nil {
		return fmt.Errorf("failed to create temporary root dir: %w", err)
	}

	// Move root mount from old root to new root
	if err := syscall.PivotRoot(".", putOld); err != nil {
		return fmt.Errorf("failed to pivot root: %w", err)
	}

	// Unmount old root
	if err := syscall.Unmount(putOld, syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("failed to unmount old root: %w", err)
	}

	// Remove old root mount point
	if err := os.RemoveAll(putOld); err != nil {
		return fmt.Errorf("failed to remove old root: %w", err)
	}

	// Mount procfs for process information
	mountProcFlags := syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV
	if err := syscall.Mount("proc", "/proc", "proc", uintptr(mountProcFlags), ""); err != nil {
		return fmt.Errorf("failed to mount procfs: %w", err)
	}

	return nil
}
