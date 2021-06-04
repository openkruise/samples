# Platforms to build the image for
PLATFORMS ?= linux/amd64,linux/arm64,linux/arm

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Build the docker image
docker-build: fmt vet
	docker build --pull --no-cache --build-arg VERSION=v1 -f sidecar.Dockerfile . -t openkruise/hotupgrade-sample:sidecarv1
	docker build --pull --no-cache --build-arg VERSION=v2 -f sidecar.Dockerfile . -t openkruise/hotupgrade-sample:sidecarv2
	docker build --pull --no-cache -f busybox.Dockerfile . -t openkruise/hotupgrade-sample:busybox
	docker build --pull --no-cache --build-arg VERSION=empty -f sidecar.Dockerfile . -t openkruise/hotupgrade-sample:empty
