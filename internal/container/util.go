package container

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// generateID creates a random ID for container.
func generateID() string {
	const chars = "0123456789abcdef"

	result := make([]byte, idLength)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}

	return string(result)
}

// createContainerDir creates container directory if it doesn't exist.
func createContainerDir(id string) error {
	containerDir := filepath.Join(containerDir, id)
	if _, err := os.Stat(containerDir); os.IsNotExist(err) {
		if err := os.MkdirAll(containerDir, 0755); err != nil {
			return fmt.Errorf("failed to create container directory: %w", err)
		}
	}

	return nil
}

// prepareCmd initializes and returns an exec.Cmd for running container process.
func prepareCmd(
	id string,
	envs Envs,
	interactive bool,
	detached bool,
	reader *os.File,
) (*exec.Cmd, error) {
	// Prepare to re-execute current program with "init" argument
	cmd := exec.Command("/proc/self/exe", "init")

	// Pass read end of pipe as fd 3 to container process
	cmd.ExtraFiles = []*os.File{reader}

	cmd.Env = append(os.Environ(), envs...)

	// Set up namespace isolation for container
	// NOTE: CLONE_NEWUSER is removed for mounting procfs
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWIPC |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET,
		Setpgid: detached,
	}

	if interactive {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		logPath := filepath.Join(containerDir, id, "container.log")
		logFile, err := os.Create(logPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create log file: %w", err)
		}
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	return cmd, nil
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
	// Make container mounts private to prevent propagation to host
	mountPropagationFlags := syscall.MS_SLAVE | syscall.MS_REC
	if err := syscall.Mount("", "/", "", uintptr(mountPropagationFlags), ""); err != nil {
		return fmt.Errorf("failed to modify root mount propagation: %w", err)
	}

	// Get new root (set by cmd.Dir in parent)
	newRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
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

	// Mount /dev using tmpfs for device isolation
	mountDevFlags := syscall.MS_NOSUID | syscall.MS_STRICTATIME
	if err := syscall.Mount("tmpfs", "/dev", "tmpfs", uintptr(mountDevFlags), "mode=755"); err != nil {
		return fmt.Errorf("failed to mount /dev: %w", err)
	}

	return nil
}

// parseSignal parses common literal signals (e.g., SIGTERM, SIGKILL) and numeric signals.
func parseSignal(sig string) (syscall.Signal, error) {
	if strings.HasPrefix(sig, "SIG") {
		s := strings.ToUpper(sig)
		switch s {
		case "SIGINT":
			return syscall.SIGINT, nil
		case "SIGTERM":
			return syscall.SIGTERM, nil
		case "SIGKILL":
			return syscall.SIGKILL, nil
		default:
			return 0, fmt.Errorf("unsupported signal: %s", sig)
		}
	}

	sigNum, err := strconv.Atoi(sig)
	if err != nil {
		return 0, fmt.Errorf("invalid signal: %s", sig)
	}
	return syscall.Signal(sigNum), nil
}

// verifyProcess checks if process with given PID belongs to specified container.
//
// Required for stopping detached containers, as without a daemon, an exited
// container's PID could be reused by the system.
func verifyProcess(pid int, id string) bool {
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(data), id)
}
