# syntax=docker/dockerfile:1

# ---- build stage ---------------------------------------------------------
# Pure Go build. The web UI is served from the COMMITTED web/dist (embedded
# via go:embed), so there is no Node toolchain in this image.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

# TARGETOS/TARGETARCH are provided by buildx for each target platform.
ARG TARGETOS
ARG TARGETARCH
# VERSION is stamped into the binary via -ldflags; defaults to "dev".
ARG VERSION=dev

WORKDIR /src

# Cache modules first for faster incremental builds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# CGO disabled -> fully static binary that runs on distroless/static.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
      -trimpath \
      -ldflags "-s -w -X github.com/hellboundg/nexus/internal/core/version.value=${VERSION}" \
      -o /out/nexus ./cmd/nexus

# Pre-create an empty /data owned by the nonroot uid. distroless has no shell,
# so we cannot mkdir/chown in the runtime stage; we build the directory here
# with the right ownership and COPY it across.
RUN mkdir -p /data && chown 65532:65532 /data

# ---- runtime stage -------------------------------------------------------
# distroless/static:nonroot -> no shell, no package manager; includes CA
# certificates (needed for outbound HTTPS to TMDb + indexers) and runs as a
# non-root user (uid 65532).
FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/nexus /nexus

# Pre-create the data dir owned by the nonroot uid so a fresh named volume
# mounted at /data inherits writable ownership (the distroless + volume-perms
# gotcha: an empty named volume adopts the mountpoint's ownership).
COPY --from=build --chown=65532:65532 /data /data

ENV NEXUS_DATA_DIR=/data \
    NEXUS_PORT=9494

EXPOSE 9494
VOLUME /data
USER nonroot:nonroot

# distroless has no shell/curl, so the healthcheck runs the binary's own
# `healthcheck` subcommand (GET /health, exit 0/1).
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/nexus", "healthcheck"]

ENTRYPOINT ["/nexus"]
