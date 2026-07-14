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

# Pre-create an empty /data. distroless has no shell, so we cannot mkdir/chmod
# in the runtime stage; we build the directory here and COPY it across. Mode
# 0777 lets the container run as an ARBITRARY uid (e.g. `user: "1000:1000"` so
# Nexus can write imports into media dirs owned by the host PUID/PGID) while a
# fresh /data named volume — which adopts this mountpoint's mode — stays
# writable. Single-tenant isolated volume, so world-writable is acceptable.
RUN mkdir -p /data && chmod 0777 /data

# ---- runtime stage -------------------------------------------------------
# distroless/static:nonroot -> no shell, no package manager; includes CA
# certificates (needed for outbound HTTPS to TMDb + indexers) and runs as a
# non-root user (uid 65532).
FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/nexus /nexus

# Carry the pre-created /data across with mode 0777 (see build stage). A fresh
# named volume mounted at /data adopts this mountpoint's ownership+mode, so the
# DB is writable whether the container runs as the default nonroot uid or as a
# custom `user:` override.
COPY --from=build --chown=65532:65532 --chmod=0777 /data /data

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
