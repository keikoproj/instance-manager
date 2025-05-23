# Build the manager binary
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETOS TARGETARCH
WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GO111MODULE=on go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:latest

# Add ARG declarations to receive build args
ARG CREATED
ARG VERSION
ARG REVISION

WORKDIR /
COPY --from=builder /workspace/manager .
ENTRYPOINT ["/manager"]
LABEL org.opencontainers.image.source="https://github.com/keikoproj/instance-manager"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${CREATED}"
LABEL org.opencontainers.image.revision="${REVISION}"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.url="https://github.com/keikoproj/instance-manager/blob/master/README.md"
LABEL org.opencontainers.image.description="A Kubernetes controller for creating and managing worker node instance groups across multiple providers"
