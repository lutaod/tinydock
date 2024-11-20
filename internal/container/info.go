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
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	PID       int       `json:"pid"`
	Status    status    `json:"status"`
	Command   []string  `json:"command"`
	CreatedAt time.Time `json:"createdAt"`
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

// save persists container information to disk.
func save(info *info) error {
	infoDir := filepath.Join(containersDir, info.ID)
	if _, err := os.Stat(infoDir); os.IsNotExist(err) {
		if err := os.MkdirAll(infoDir, 0755); err != nil {
			return fmt.Errorf("failed to create containers directory: %w", err)
		}
	}

	infoPath := filepath.Join(infoDir, infoFile)
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal container info: %w", err)
	}

	if err := os.WriteFile(infoPath, data, 0644); err != nil {
		return fmt.Errorf("failed to save container info: %w", err)
	}

	return nil
}

// load retrieves container information of given ID from disk.
func load(id string) (*info, error) {
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

// list fetches container info matching the filter condition and prints them.
func list(showAll bool) error {
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

		info, err := load(entry.Name())
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
