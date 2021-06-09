# Build the manager binary
FROM golang:1.15 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
#RUN go mod download

# Copy the go source
COPY sidecar/main.go main.go
COPY vendor/ vendor/

ARG VERSION
# Build
RUN CGO_ENABLED=0 GO111MODULE=on go build -ldflags "-X main.Version=${VERSION}" -mod=vendor -a -o manager main.go

FROM busybox:latest

COPY --from=builder /workspace/manager ./
COPY migrate.sh ./
ENTRYPOINT ["/manager"]
