package overlay

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/lutaod/tinydock/assets"
	"github.com/lutaod/tinydock/internal/config"
	"github.com/lutaod/tinydock/internal/volume"
)

const (
	baseImage = "busybox"

	upper  = "upper"
	work   = "work"
	merged = "merged"
)

var (
	overlayDir   = filepath.Join(config.Root, "overlay")
	imageDir     = filepath.Join(config.Root, "image")
	tarballDir   = filepath.Join(imageDir, "tarball")
	extractedDir = filepath.Join(imageDir, "extracted")
)

// Setup prepares overlay filesystem and mount volumes for a container.
func Setup(image, containerID string, volumes volume.Volumes) (string, error) {
	paths := map[string]string{
		upper:  filepath.Join(overlayDir, containerID, upper),
		work:   filepath.Join(overlayDir, containerID, work),
		merged: filepath.Join(overlayDir, containerID, merged),
	}

	for _, dir := range paths {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("failed to create overlay directory %s: %w", dir, err)
		}
	}

	lowerDir, err := extractImage(image)
	if err != nil {
		return "", err
	}

	opts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		lowerDir,
		paths[upper],
		paths[work],
	)

	if err := syscall.Mount("overlay", paths[merged], "overlay", 0, opts); err != nil {
		return "", fmt.Errorf("failed to mount overlayfs: %w", err)
	}

	for _, v := range volumes {
		target := filepath.Join(paths[merged], v.Target)

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

	return paths[merged], nil
}

// SaveImage creates a new image from a container's merged directory.
func SaveImage(containerID, imageName string) error {
	imagePath := filepath.Join(imageDir, imageName)
	if _, err := os.Stat(imagePath); err == nil {
		return fmt.Errorf("image '%s' already exists", imageName)
	}

	mergedPath := filepath.Join(overlayDir, containerID, merged)
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
	mergedPath := filepath.Join(overlayDir, containerID, merged)

	for _, v := range volumes {
		target := filepath.Join(mergedPath, v.Target)
		if err := syscall.Unmount(target, 0); err != nil {
			return fmt.Errorf("failed to unmount volume %s: %w", target, err)
		}
	}

	if err := syscall.Unmount(mergedPath, 0); err != nil {
		return fmt.Errorf("failed to unmount overlayfs: %w", err)
	}

	containerDir := filepath.Join(overlayDir, containerID)
	if err := os.RemoveAll(containerDir); err != nil {
		return fmt.Errorf("failed to remove overlay directory: %w", err)
	}

	return nil
}

// extractImage extracts the specified image tarball if not already extracted.
//
// The function manages two directories:
//   - tarballs/: stores compressed images (.tar.gz).
//     Custom images and committed images should be placed here.
//   - extracted/: stores uncompressed filesystems to be used as lower directories for overlayfs.
//
// If base image tarball is missing, it will be copied from project assets.
func extractImage(image string) (string, error) {
	tarballPath := filepath.Join(tarballDir, image+".tar.gz")
	extractedPath := filepath.Join(extractedDir, image)

	// Check if already extracted
	if _, err := os.Stat(extractedPath); err == nil {
		return extractedPath, nil
	}

	// Check if tarball exists, base image can be copied from embedded assets if not
	if _, err := os.Stat(tarballPath); err != nil {
		if image == baseImage {
			src, err := assets.Files.Open(baseImage + ".tar.gz")
			if err != nil {
				return "", fmt.Errorf("failed to open embedded tarball file: %w", err)
			}
			defer src.Close()

			if err := os.MkdirAll(filepath.Dir(tarballPath), 0755); err != nil {
				return "", fmt.Errorf("failed to create tarball directory: %w", err)
			}

			dst, err := os.Create(tarballPath)
			if err != nil {
				return "", fmt.Errorf("failed to create tarball file: %w", err)
			}
			defer dst.Close()

			if _, err := io.Copy(dst, src); err != nil {
				return "", fmt.Errorf("failed to write tarball file: %w", err)
			}
		} else {
			return "", fmt.Errorf("image '%s' not found", image)
		}
	}

	// Extract tarball
	if err := os.MkdirAll(extractedPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create extracted directory: %w", err)
	}

	cmd := exec.Command("tar", "xzf", tarballPath, "-C", extractedPath)
	if err := cmd.Run(); err != nil {
		os.RemoveAll(extractedPath)
		return "", fmt.Errorf("failed to extract image: %w", err)
	}

	return extractedPath, nil
}

// copyDir copies the contents of src directory to dst directory.
func copyDir(src, dst string) error {
	cmd := exec.Command("cp", "-r", src+"/.", dst)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy failed: %s", output)
	}

	return nil
}
