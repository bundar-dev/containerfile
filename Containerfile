# Runnable image for the containerfile CLI.
#
#   podman run --rm --network=none --userns=keep-id \
#     -v "$SOURCE:/workspace:Z" -w /workspace \
#     ghcr.io/bundar-dev/containerfile:<version> [options]
#
# Build: podman build -f Containerfile -t ghcr.io/bundar-dev/containerfile:dev .
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26-alpine AS build

WORKDIR /src
COPY . .

# Cross-compile natively for the target platform (no emulation needed).
ARG TARGETOS TARGETARCH VERSION=dev
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/containerfile ./cmd/containerfile

FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev
LABEL org.opencontainers.image.title="containerfile" \
      org.opencontainers.image.description="Generate a Containerfile/Dockerfile by scanning a project directory" \
      org.opencontainers.image.source="https://github.com/bundar-dev/containerfile" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

COPY --from=build /out/containerfile /usr/local/bin/containerfile

# Runs fully offline. With `--userns=keep-id` (podman) or
# `--user "$(id -u):$(id -g)"` (docker) files are owned by the caller.
WORKDIR /workspace
USER 65534:65534

ENTRYPOINT ["/usr/local/bin/containerfile"]
