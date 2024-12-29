# tinydock

A proof-of-concept container runtime implementation in Go. Built for educational purposes to understand core containerization concepts.

This project implements basic container functionalities, including:
- Process isolation
- Resource limits
- Volume support
- Networking

## Setup

### Prerequisites

- Linux (tested on Ubuntu 22.04)
- Go 1.23 or newer
- Root privileges

### Steps

```bash
# Clone the repository
$ git clone https://github.com/lutaod/tinydock.git
$ cd tinydock

# Initialize Go module
$ go mod init tinydock
$ go mod tidy

# Build the binary
$ cd cmd
$ go build -o tinydock

# To check available commands
$ sudo ./tinydock -help

# To run a container
$ sudo ./tinydock run -it -rm busybox sh
```

## Custom Images

The project uses `busybox` as the default base image, but you can use other images by providing their filesystem tarballs. Here’s how to prepare a custom image:

```bash
# Pull the desired image from Docker Hub
$ docker pull alpine:latest

# Create a container and export its filesystem
$ docker create --name temp alpine:latest
$ docker export temp | gzip > alpine.tar.gz
$ docker rm temp

# Move the tarball to tinydock's image registry directory
$ sudo mkdir -p /var/lib/tinydock/image/registry
$ sudo mv alpine.tar.gz /var/lib/tinydock/image/registry/

# Now you can use the image with tinydock
$ sudo ./tinydock run alpine sh
```

NOTE: Docker images with preset entrypoints are not yet supported by this implementation. Users must explicitly provide the command to run in the container.
