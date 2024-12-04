package overlay

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/lutaod/tinydock/assets"
	"github.com/lutaod/tinydock/internal/volume"
)

const (
	tinydockRoot = "/var/lib/tinydock"
	imageDir     = "image"
	overlayDir   = "overlay"
	upperDir     = "upper"
	workDir      = "work"
	mergedDir    = "merged"
)

// Setup prepares overlay filesystem and mount volumes for a container.
func Setup(containerID string, volumes volume.Volumes) (string, error) {
	paths := map[string]string{
		upperDir:  filepath.Join(tinydockRoot, overlayDir, containerID, upperDir),
		workDir:   filepath.Join(tinydockRoot, overlayDir, containerID, workDir),
		mergedDir: filepath.Join(tinydockRoot, overlayDir, containerID, mergedDir),
	}

	for _, dir := range paths {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create overlay directory %s: %w", dir, err)
		}
	}

	lowerDir, err := extractBaseImage()
	if err != nil {
		return "", err
	}

	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		lowerDir,
		paths[upperDir],
		paths[workDir],
	)

	if err := syscall.Mount("overlay", paths[mergedDir], "overlay", 0, opts); err != nil {
		return "", fmt.Errorf("failed to mount overlayfs: %w", err)
	}

	for _, v := range volumes {
		target := filepath.Join(paths[mergedDir], v.Target)

		// Create host source directory if does not exist
		if _, err := os.Stat(v.Source); os.IsNotExist(err) {
			if err := os.MkdirAll(v.Source, 0755); err != nil {
				return "", fmt.Errorf("failed to create volume source %s: %w", v.Source, err)
			}
		} else if err != nil {
			return "", fmt.Errorf("failed to check volume source %s: %w", v.Source, err)
		}

		if err := os.MkdirAll(target, 0755); err != nil {
			return "", fmt.Errorf("failed to create volume target %s: %w", target, err)
		}

		if err := syscall.Mount(v.Source, target, "", uintptr(syscall.MS_BIND), ""); err != nil {
			return "", fmt.Errorf("failed to mount volume %s to %s: %w", v.Source, target, err)
		}
	}

	return paths[mergedDir], nil
}

// SaveImage creates a new image from a container's merged directory.
func SaveImage(containerID, imageName string) error {
	imagePath := filepath.Join(tinydockRoot, imageDir, imageName)
	if _, err := os.Stat(imagePath); err == nil {
		return fmt.Errorf("image '%s' already exists", imageName)
	}

	mergedPath := filepath.Join(tinydockRoot, overlayDir, containerID, mergedDir)
	if _, err := os.Stat(mergedPath); err != nil {
		return fmt.Errorf("container filesystem not found: %w", err)
	}

	if err := copyDir(mergedPath, imagePath); err != nil {
		os.RemoveAll(imagePath)
		return fmt.Errorf("failed to save filesystem: %w", err)
	}

	return nil
}

// Cleanup unmounts any volumes and removes all overlay filesystem resources for a container.
func Cleanup(containerID string, volumes volume.Volumes) error {
	mergedPath := filepath.Join(tinydockRoot, overlayDir, containerID, mergedDir)

	for _, v := range volumes {
		target := filepath.Join(mergedPath, v.Target)
		if err := syscall.Unmount(target, 0); err != nil {
			return fmt.Errorf("failed to unmount volume %s: %w", target, err)
		}
	}

	if err := syscall.Unmount(mergedPath, 0); err != nil {
		return fmt.Errorf("failed to unmount overlayfs: %w", err)
	}

	containerDir := filepath.Join(tinydockRoot, overlayDir, containerID)
	if err := os.RemoveAll(containerDir); err != nil {
		return fmt.Errorf("failed to remove overlay directory: %w", err)
	}

	return nil
}

// extractBaseImage extracts embedded base image tar to image directory.
func extractBaseImage() (string, error) {
	baseImageDir := filepath.Join(tinydockRoot, imageDir, "busybox")

	// Skip if already extracted
	if _, err := os.Stat(baseImageDir); err == nil {
		return baseImageDir, nil
	}

	if err := os.MkdirAll(baseImageDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create image directory: %w", err)
	}

	// Create temp file for embedded tar so that 'tar' command has file path to extract from
	tmpFile, err := os.CreateTemp("", "busybox-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	src, err := assets.Files.Open("busybox.tar.gz")
	if err != nil {
		return "", fmt.Errorf("failed to open embedded file: %w", err)
	}
	defer src.Close()

	if _, err := io.Copy(tmpFile, src); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	cmd := exec.Command("tar", "xzf", tmpFile.Name(), "-C", baseImageDir)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to extract base image: %w", err)
	}

	return baseImageDir, nil
}

// copyDir copies the contents of src directory to dst directory.
func copyDir(src, dst string) error {
	cmd := exec.Command("cp", "-r", src+"/.", dst)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy failed: %s", output)
	}

	return nil
}
