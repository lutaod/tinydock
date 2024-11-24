package container

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lutaod/tinydock/internal/cgroups"
	"github.com/lutaod/tinydock/internal/overlay"
	"github.com/lutaod/tinydock/internal/volume"
)

// Create spawns a container process that initially acts as the init process (PID 1)
// before being replaced by user command.
func Create(
	interactive bool,
	autoRemove bool,
	detached bool,
	name string,
	cpuLimit float64,
	memoryLimit string,
	volumes volume.Volumes,
	args []string,
	envs Envs,
) error {
	// Create unnamed pipe for passing user command
	reader, writer, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %w", err)
	}

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
	}

	id := generateID()

	if name == "" {
		name = id
	}

	// Initialize overlay filesystem for container
	mergedDir, err := overlay.Setup(id, volumes)
	if err != nil {
		return fmt.Errorf("failed to setup overlay: %w", err)
	}

	// Set merged overlay directory as working directory for container's root filesystem
	cmd.Dir = mergedDir

	log.Printf("Container %s overlayfs initialized", id)

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

	// Record container information locally
	info := &info{
		ID:        id,
		Name:      name,
		PID:       pid,
		Status:    running,
		Command:   args,
		CreatedAt: time.Now(),
		Volumes:   volumes,
	}

	if err := saveInfo(info); err != nil {
		return err
	}

	// Initialize cgroup for container
	if err := cgroups.Create(id); err != nil {
		return err
	}

	if err := cgroups.AddProcess(id, pid); err != nil {
		return err
	}

	// Set memory and CPU limits if provided
	if memoryLimit != "" {
		if err := cgroups.SetMemoryLimit(id, memoryLimit); err != nil {
			return err
		}
	}
	if cpuLimit != 0 {
		if err := cgroups.SetCPULimit(id, cpuLimit); err != nil {
			return err
		}
	}

	log.Printf("Container %s cgroups initialized", id)

	if detached {
		if err := cmd.Process.Release(); err != nil {
			return fmt.Errorf("failed to release container: %w", err)
		}

		log.Println(id)
		return nil
	}

	defer func() {
		info.Status = exited
		if err := saveInfo(info); err != nil {
			log.Print(err)
		}

		if autoRemove {
			if err := Remove(id, false); err != nil {
				log.Print(err)
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("failed to wait for container: %w", err)
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

// List prints all containers, or only running ones if showAll is false.
func List(showAll bool) error {
	return listInfo(showAll)
}

// Stop sends a signal to specified container and waits for it to terminate.
//
// Interactive containers may not properly handle SIGTERM/SIGINT signals when
// running in foreground, instead, users should exit them directly.
func Stop(id, sig string) error {
	info, err := loadInfo(id)
	if err != nil {
		return fmt.Errorf("no such container: %w", err)
	}

	if info.Status == exited {
		return fmt.Errorf("container is not running")
	}

	if err := syscall.Kill(info.PID, 0); err != nil || !verifyProcess(info.PID, id) {
		info.Status = exited
		if err := saveInfo(info); err != nil {
			return fmt.Errorf("failed to update container status: %w", err)
		}

		return fmt.Errorf("container already stopped")
	}

	signal := syscall.SIGTERM
	if sig != "" {
		signal, err = parseSignal(sig)
		if err != nil {
			return fmt.Errorf("failed to parse signal: %w", err)
		}
	}

	if err := syscall.Kill(info.PID, signal); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Wait for up to a second for container to stop
	for i := 0; i < 10; i++ {
		if err := syscall.Kill(info.PID, 0); err != nil {
			info.Status = exited
			if err := saveInfo(info); err != nil {
				return fmt.Errorf("failed to update container status: %w", err)
			}

			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("container did not stop")
}

// Remove deletes container resources.
func Remove(id string, force bool) error {
	info, err := loadInfo(id)
	if err != nil {
		return err
	}

	if info.Status == running {
		if force {
			if err := Stop(id, "SIGKILL"); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("container is running: stop it before removing")
		}
	}

	if err := cgroups.Remove(id); err != nil {
		return err
	}

	if err := overlay.Cleanup(id, info.Volumes); err != nil {
		return err
	}

	if err := removeInfo(id); err != nil {
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
