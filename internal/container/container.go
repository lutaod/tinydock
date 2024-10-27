package container

import (
	"log"
	"os"
	"os/exec"
	"syscall"

	"github.com/lutaod/tinydock/internal/cgroups"
)

// Create sets up the stage for the container's init process.
func Create(interactive bool, memoryLimit string, cpuLimit float64, args []string) error {
	// Prepare to re-execute the current program with the "init" argument
	cmd := exec.Command("/proc/self/exe", append([]string{"init"}, args...)...)

	// Set up namespace isolation for the container
	// NOTE: CLONE_NEWUSER is removed for mounting proc
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

	// Spawn the container's init process
	if err := cmd.Start(); err != nil {
		return err
	}

	pid := cmd.Process.Pid
	log.Printf("Container process started with PID %d", cmd.Process.Pid)

	// Generate an unique container name and initialize its corresponding cgroup
	containerID, err := cgroups.Create()
	if err != nil {
		return err
	}

	// Ensure the cgroup is removed after the container process ends
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
		return err
	}

	return nil
}

// Run takes over after the Create function and is responsible for executing the user's command
// inside the container.
func Run(command string, args []string) error {
	// Ensure mounts inside the container do not affect the host
	mountPropagationFlags := syscall.MS_SLAVE | syscall.MS_REC
	if err := syscall.Mount("", "/", "", uintptr(mountPropagationFlags), ""); err != nil {
		return err
	}

	// Mount the proc filesystem for process information
	mountProcFlags := syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV
	if err := syscall.Mount("proc", "/proc", "proc", uintptr(mountProcFlags), ""); err != nil {
		return err
	}

	// Transform the init process to run the user-specified command
	argv := append([]string{command}, args...)
	if err := syscall.Exec(command, argv, os.Environ()); err != nil {
		return err
	}

	return nil
}
