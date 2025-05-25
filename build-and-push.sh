podman manifest create $1
podman build --platform linux/amd64,linux/arm64 --manifest $1 -f Dockerfile_fsd .
