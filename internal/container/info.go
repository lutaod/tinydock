package container

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lutaod/tinydock/internal/volume"
)

const (
	containersDir = "/var/lib/tinydock/containers"
	infoFile      = "info.json"

	idLength                = 8
	maxPrintCmdLength       = 30
	truncatedPrintCmdLength = maxPrintCmdLength - 3 // Reserve space for "..."
)

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
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	PID       int            `json:"pid"`
	Status    status         `json:"status"`
	Command   []string       `json:"command"`
	CreatedAt time.Time      `json:"createdAt"`
	Volumes   volume.Volumes `json:"volumes"`
}

// generateID creates a random ID for container.
func generateID() string {
	const chars = "0123456789abcdef"

	result := make([]byte, idLength)
	for i := range result {
		result[i] = chars[rand.Intn(len(chars))]
	}

	return string(result)
}

// saveInfo persists container information to disk.
func saveInfo(info *info) error {
	infoPath := filepath.Join(containersDir, info.ID, infoFile)
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
	infoPath := filepath.Join(containersDir, id, infoFile)
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
	entries, err := os.ReadDir(containersDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read containers directory: %w", err)
	}

	fmt.Printf("%-10s %-20s %-15s %-10s %-20s %s\n",
		"ID", "NAME", "STATUS", "PID", "CREATED", "COMMAND")

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

		cmd := strings.Join(info.Command, " ")
		if len(cmd) > maxPrintCmdLength {
			cmd = cmd[:truncatedPrintCmdLength] + "..."
		}

		fmt.Printf("%-10s %-20s %-15s %-10d %-20s %s\n",
			info.ID, info.Name, info.Status, info.PID,
			info.CreatedAt.Format("2006-01-02 15:04:05"), cmd)
	}

	return nil
}

// removeInfo deletes container information from disk.
func removeInfo(id string) error {
	infoDir := filepath.Join(containersDir, id)
	if err := os.RemoveAll(infoDir); err != nil {
		return fmt.Errorf("failed to remove container directory: %w", err)
	}

	return nil
}
