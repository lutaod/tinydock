package container

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lutaod/tinydock/internal/cgroups"
	"github.com/lutaod/tinydock/internal/network"
	"github.com/lutaod/tinydock/internal/overlay"
	"github.com/lutaod/tinydock/internal/volume"
)

// Init spawns a container process that initially acts as the init process (PID 1)
// before being replaced by user command.
func Init(
	image string,
	args []string,
	interactive bool,
	autoRemove bool,
	detached bool,
	nw string,
	ports network.PortMappings,
	volumes volume.Volumes,
	envs Envs,
	cpuLimit float64,
	memoryLimit string,
) error {
	// Create unnamed pipe for passing user command
	reader, writer, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create pipe: %w", err)
	}

	id := generateID()
	if err := createContainerDir(id); err != nil {
		return err
	}

	cmd, err := prepareCmd(id, envs, interactive, detached, reader)
	if err != nil {
		return err
	}

	mergedDir, err := overlay.Setup(image, id, volumes)
	if err != nil {
		return err
	}
	cmd.Dir = mergedDir

	if err := cmd.Start(); err != nil {
		reader.Close()
		return fmt.Errorf("failed to initialize container: %w", err)
	}
	reader.Close()

	if err := writeArgsToPipe(writer, args); err != nil {
		return err
	}

	info := &info{
		ID:        id,
		PID:       cmd.Process.Pid,
		Status:    running,
		Image:     image,
		Command:   args,
		CreatedAt: time.Now(),
		Volumes:   volumes,
	}

	if err := cgroups.Configure(id, info.PID, cpuLimit, memoryLimit); err != nil {
		return err
	}

	endpoint, err := network.Setup(info.PID, nw, ports)
	if err != nil {
		return err
	}
	info.Endpoint = *endpoint

	if err := saveInfo(info); err != nil {
		return err
	}

	if err := handleLifecycle(cmd, info, detached, autoRemove); err != nil {
		return err
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
		return fmt.Errorf("error loading container %s: %w", id, err)
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

	if info.Endpoint.IPNet != nil {
		if err := network.Disconnect(&info.Endpoint); err != nil {
			return err
		}
	}

	if err := removeInfo(id); err != nil {
		return err
	}

	return nil
}

// Logs displays container logs.
func Logs(id string, follow bool) error {
	info, err := loadInfo(id)
	if err != nil {
		return fmt.Errorf("error loading container %s: %w", id, err)
	}

	logPath := filepath.Join(containerDir, id, "container.log")
	if _, err := os.Stat(logPath); err != nil {
		return fmt.Errorf("no logs for container")
	}

	if !follow {
		content, err := os.ReadFile(logPath)
		if err != nil {
			return fmt.Errorf("failed to read logs: %w", err)
		}

		fmt.Print(string(content))
		return nil
	}

	file, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer file.Close()

	// Seek to end for follow mode
	if _, err := file.Seek(0, 2); err != nil {
		return fmt.Errorf("failed to seek log file: %w", err)
	}

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read log: %w", err)
		}

		if line != "" {
			fmt.Print(line)
		}

		if err == io.EOF {
			if info.Status == exited {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
	}
}

// Exec executes a command in a running container.
//
// A new process is forked to enter container namespaces before executing the
// command due to Linux kernel restrictions on mount namespace transitions in
// multi-threaded processes.
func Exec(id string, command []string) error {
	if os.Getenv("TINYDOCK_PID") != "" {
		// Second run: C constructor will have handled namespace entry as env
		// vars are set
		return nil
	}

	// First run
	info, err := loadInfo(id)
	if err != nil {
		return fmt.Errorf("error loading container %s: %w", id, err)
	}

	if info.Status != running {
		return fmt.Errorf("container is not running")
	}

	cmd := exec.Command("/proc/self/exe", append([]string{"exec", id}, command...)...)

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	envs, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", info.PID))
	if err != nil {
		return fmt.Errorf("failed to read environment variables: %w", err)
	}

	cmd.Env = append(strings.Split(string(envs), "\x00"),
		// Set env vars for C constructor
		fmt.Sprintf("TINYDOCK_PID=%d", info.PID),
		fmt.Sprintf("TINYDOCK_CMD=%s", strings.Join(command, " ")),
	)

	return cmd.Run()
}

// Commit creates a new image from a container's filesystem.
func Commit(id, name string) error {
	_, err := loadInfo(id)
	if err != nil {
		return fmt.Errorf("error loading container %s: %w", id, err)
	}

	if err := overlay.SaveImage(id, name); err != nil {
		return fmt.Errorf("failed to commit container: %w", err)
	}

	return nil
}

// ListImages prints information about available images.
func ListImages() error {
	entries, err := os.ReadDir(overlay.RegistryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read image registry: %w", err)
	}

	fmt.Printf("%-20s %-20s %s\n", "IMAGE", "CREATED", "SIZE")

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".tar.gz")
		size := fmt.Sprintf("%.2f MB", float64(info.Size())/1024/1024)
		created := info.ModTime().Format("2006-01-02 15:04:05")

		fmt.Printf("%-20s %-20s %s\n", name, created, size)
	}

	return nil
}
