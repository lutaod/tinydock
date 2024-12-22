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
	overlayDir  = filepath.Join(config.Root, "overlay")
	imageDir    = filepath.Join(config.Root, "image")
	RegistryDir = filepath.Join(imageDir, "registry")
	rootfsDir   = filepath.Join(imageDir, "rootfs")
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

// SaveImage creates a new tarball image from a container's merged directory.
func SaveImage(containerID, imageName string) error {
	tarballPath := filepath.Join(RegistryDir, imageName+".tar.gz")
	if _, err := os.Stat(tarballPath); err == nil {
		return fmt.Errorf("image '%s' already exists", imageName)
	}

	mergedPath := filepath.Join(overlayDir, containerID, merged)
	if _, err := os.Stat(mergedPath); err != nil {
		return fmt.Errorf("container filesystem not found: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(tarballPath), 0755); err != nil {
		return fmt.Errorf("failed to create tarball directory: %w", err)
	}

	cmd := exec.Command("tar", "czf", tarballPath, "-C", mergedPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tarballPath)
		return fmt.Errorf("failed to create image tarball: %s", out)
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
//   - registry/: stores compressed images (.tar.gz).
//     Custom images and committed images should be placed here.
//   - rootfs/: stores uncompressed filesystems to be used as lower directories for overlayfs.
//
// If base image tarball is missing, it will be copied from project assets.
func extractImage(image string) (string, error) {
	registryPath := filepath.Join(RegistryDir, image+".tar.gz")
	rootfsPath := filepath.Join(rootfsDir, image)

	// Check if already extracted
	if _, err := os.Stat(rootfsPath); err == nil {
		return rootfsPath, nil
	}

	// Check if tarball exists, base image can be copied from embedded assets if not
	if _, err := os.Stat(registryPath); err != nil {
		if image == baseImage {
			src, err := assets.Files.Open(baseImage + ".tar.gz")
			if err != nil {
				return "", fmt.Errorf("failed to open embedded tarball file: %w", err)
			}
			defer src.Close()

			if err := os.MkdirAll(filepath.Dir(registryPath), 0755); err != nil {
				return "", fmt.Errorf("failed to create tarball directory: %w", err)
			}

			dst, err := os.Create(registryPath)
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
	if err := os.MkdirAll(rootfsPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create extracted directory: %w", err)
	}

	cmd := exec.Command("tar", "xzf", registryPath, "-C", rootfsPath)
	if err := cmd.Run(); err != nil {
		os.RemoveAll(rootfsPath)
		return "", fmt.Errorf("failed to extract image: %w", err)
	}

	return rootfsPath, nil
}
