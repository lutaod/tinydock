# tinydock

## Custom Images

While tinydock comes with busybox as the default base image, you can use other images by providing their filesystem tarballs. Here's how to prepare a custom image:

1. Pull the desired image from Docker Hub:
```bash
$ docker pull alpine:latest
```

2. Create a container and export its filesystem:
```bash
$ docker create --name temp alpine:latest
$ docker export temp | gzip > alpine.tar.gz
$ docker rm temp
```

3. Move the tarball to tinydock's image registry directory:
```bash
$ sudo mkdir -p /var/lib/tinydock/image/registry
$ sudo mv alpine.tar.gz /var/lib/tinydock/image/registry/
```

4. Now you can use the image with tinydock:
```bash
$ sudo tinydock run alpine sh
```
