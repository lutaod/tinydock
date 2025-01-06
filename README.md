# tinydock

A proof-of-concept container runtime implementation in Go. Built for better understanding of core containerization concepts, including:
- Process isolation
- Resource limits
- File system layering
- Bridge networking
- Base image management

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

NOTE: Docker images with preset entrypoints are not supported by this implementation. Users must explicitly provide the command to run in the container.

## Multi-Container Example with Redis

Before trying this example, follow the previous section to prepare a `redis` image.

```bash
# Create persistent data directory
$ export REDIS_DATA=<YOUR_PREFERRED_LOCATION>

# Create bridge network
$ sudo ./tinydock network create -driver bridge -subnet 172.27.0.0/16 redis-nw

# Start redis server container with persistence and network
$ sudo ./tinydock run -d \
   -network redis-nw \
   -e LC_ALL=C.UTF-8 \
   -v $REDIS_DATA:/data \
   redis redis-server \
   --bind 0.0.0.0 \
   --dir /data \
   --appendonly yes 

# Inspect redis server container and check its assigned IP (should be 172.27.0.2)
$ sudo ./tinydock ls

# Test connection
$ sudo ./tinydock run -it -rm -network redis-nw \
   redis redis-cli -h 172.27.0.2 ping

# Store and retrieve data
$ sudo ./tinydock run -it -rm -network redis-nw \
   redis redis-cli -h 172.27.0.2 set greeting "hello"
$ sudo ./tinydock run -it -rm -network redis-nw \
   redis redis-cli -h 172.27.0.2 get greeting

# Verify data persistence
$ ls -l $REDIS_DATA

# Cleanup
$ sudo ./tinydock rm -f <REDIS_SERVER_CONTAINER_ID>
$ sudo ./tinydock network rm redis-nw
```
