package container

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

const (
	containersDir = "/var/lib/tinydock/containers"

	idLength = 8
)

// status represents the runtime state of container.
type status string

const (
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

// saveInfo persists container information on disk.
func saveInfo(info *info) error {
	infoDir := filepath.Join(containersDir, info.ID)

	if _, err := os.Stat(infoDir); os.IsNotExist(err) {
		if err := os.MkdirAll(infoDir, 0755); err != nil {
			return fmt.Errorf("failed to create containers directory: %w", err)
		}
	}

	infoFile := filepath.Join(infoDir, "info.json")
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal container info: %w", err)
	}

	if err := os.WriteFile(infoFile, data, 0644); err != nil {
		return fmt.Errorf("failed to save container info: %w", err)
	}

	return nil
}

// loadInfo retrieves container information from disk.
func loadInfo(id string) (*info, error) {
	infoDir := filepath.Join(containersDir, id)

	infoFile := filepath.Join(infoDir, "config.json")
	data, err := os.ReadFile(infoFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read container info: %w", err)
	}

	var info info
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal container info: %w", err)
	}

	return &info, nil
}
