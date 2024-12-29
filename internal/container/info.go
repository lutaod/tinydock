package container

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lutaod/tinydock/internal/config"
	"github.com/lutaod/tinydock/internal/network"
	"github.com/lutaod/tinydock/internal/volume"
)

const (
	infoFile = "info.json"

	idLength                = 6
	maxPrintCmdLength       = 30
	truncatedPrintCmdLength = maxPrintCmdLength - 3 // Reserve space for "..."
)

var containerDir = filepath.Join(config.Root, "container")

// status represents the runtime state of container.
type status string

const (
	// NOTE: For detached containers, the actual process state cannot be monitored
	// without daemon. Their status will remain "running" until explicitly stopped.
	running status = "running"
	exited  status = "exited"
)

// info stores relevant information of a container.
type info struct {
	ID        string            `json:"id"`
	PID       int               `json:"pid"`
	Status    status            `json:"status"`
	Image     string            `json:"image"`
	Command   []string          `json:"command"`
	CreatedAt time.Time         `json:"createdAt"`
	Volumes   volume.Volumes    `json:"volumes"`
	Endpoint  *network.Endpoint `json:"endpoint"`
}

// saveInfo persists container information to disk.
func saveInfo(info *info) error {
	infoPath := filepath.Join(containerDir, info.ID, infoFile)
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal container info: %w", err)
	}

	if err := os.WriteFile(infoPath, data, 0644); err != nil {
		return fmt.Errorf("failed to save container info: %w", err)
	}

	return nil
}

// loadInfo retrieves container information of given ID from disk.
func loadInfo(id string) (*info, error) {
	infoPath := filepath.Join(containerDir, id, infoFile)
	data, err := os.ReadFile(infoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read container info: %w", err)
	}

	var info info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal container info: %w", err)
	}

	return &info, nil
}

// listInfo fetches container information matching the filter condition and prints them.
func listInfo(showAll bool) error {
	entries, err := os.ReadDir(containerDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read containers directory: %w", err)
	}

	fmt.Printf("%-10s %-10s %-15s %-15s %-15s %-8s %-20s %s\n",
		"ID", "STATUS", "IMAGE", "IP", "PORTS", "PID", "CREATED", "COMMAND")

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		info, err := loadInfo(entry.Name())
		if err != nil {
			log.Printf("Warning: failed to load container info for %s: %v", entry.Name(), err)
			continue
		}

		if !showAll && info.Status != running {
			continue
		}

		var ip, ports string
		if info.Endpoint != nil {
			ip = info.Endpoint.IPNet.IP.String()
			if len(info.Endpoint.PortMappings) > 0 {
				mappings := make([]string, 0, len(info.Endpoint.PortMappings))
				for _, p := range info.Endpoint.PortMappings {
					mappings = append(mappings, fmt.Sprintf("%d->%d", p.HostPort, p.ContainerPort))
				}
				ports = strings.Join(mappings, ",")
			}
		}

		cmd := strings.Join(info.Command, " ")
		if len(cmd) > maxPrintCmdLength {
			cmd = cmd[:truncatedPrintCmdLength] + "..."
		}

		fmt.Printf("%-10s %-10s %-15s %-15s %-15s %-8d %-20s %s\n",
			info.ID, info.Status, info.Image, ip, ports, info.PID,
			info.CreatedAt.Format("2006-01-02 15:04:05"), cmd)
	}

	return nil
}

// removeInfo deletes container information from disk.
func removeInfo(id string) error {
	infoDir := filepath.Join(containerDir, id)
	if err := os.RemoveAll(infoDir); err != nil {
		return fmt.Errorf("failed to remove container directory: %w", err)
	}

	return nil
}

// handleLifecycle manages container process lifecycle, including cleanup and status updates.
func handleLifecycle(cmd *exec.Cmd, info *info, detached bool, autoRemove bool) error {
	if detached {
		if err := cmd.Process.Release(); err != nil {
			return fmt.Errorf("failed to release container: %w", err)
		}

		fmt.Println(info.ID)
		return nil
	}

	defer func() {
		info.Status = exited
		if err := saveInfo(info); err != nil {
			log.Print(err)
		}

		if autoRemove {
			if err := Remove(info.ID, false); err != nil {
				log.Print(err)
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("failed to wait for container: %w", err)
	}

	return nil
}
