# Caravel, as a container image.
#
#   docker build --build-arg VERSION="$(scripts/version.sh)" -t caravel .
#   podman build --build-arg VERSION="$(scripts/version.sh)" -t caravel .
#
# Both work; nothing here is BuildKit-specific. The compose files pass the same
# argument, and the publishing workflow passes the resolved tag.

# --platform=$BUILDPLATFORM pins the build stage to the *builder's* architecture
# and cross-compiles from there, rather than emulating the target. For a Go
# binary that is a large difference: an emulated arm64 `go build` under QEMU
# takes minutes, and this takes as long as any other build.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build

WORKDIR /src

# Dependencies first, so editing source does not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# The build's identity, reported by GET /api/health and the startup banner. It
# has to be passed in: scripts/version.sh derives it from git, and .git is not
# in the build context (see .dockerignore), so without this argument the binary
# would honestly but uselessly call itself "unknown".
ARG VERSION=unknown

# Supplied by the builder for each requested platform. Defaulted so a plain
# `docker build` with no platform flag still works.
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# CGO off because modernc.org/sqlite is pure Go: the result is a static binary
# that runs on a distroless base with no libc at all, and cross-compiles without
# a toolchain per architecture. -w -s drop DWARF and the symbol table, which
# this binary has no use for in production.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
        -ldflags "-w -s -X caravel/internal/buildinfo.Version=${VERSION}" \
        -o /caravel ./cmd/caravel

# The data directories are created here so they can be copied in with the right
# ownership: the runtime image has no shell, so there is no RUN mkdir there, and
# an empty named volume inherits the ownership of the image directory it covers.
RUN mkdir -p /empty/data /empty/uploads

# distroless/static: no shell, no package manager, no libc — a few megabytes,
# and nothing to exploit that is not our own binary. :nonroot runs as uid 65532.
FROM gcr.io/distroless/static:nonroot

COPY --from=build /caravel /caravel
COPY --from=build --chown=nonroot:nonroot /empty/data /data
COPY --from=build --chown=nonroot:nonroot /empty/uploads /uploads

# The frontend is embedded in the binary (see embed.go), so there is no web/ to
# copy and CARAVEL_WEB_DIR is deliberately unset: pointing it at a directory
# that does not exist here would serve an empty app.
ENV CARAVEL_DB_DSN=/data/caravel.db \
    CARAVEL_UPLOAD_DIR=/uploads

EXPOSE 8080

# Both are declared so `docker run -v` and a bare `docker run` behave sensibly;
# the compose files name them explicitly. Back them up together — the database
# references uploaded files by id.
VOLUME ["/data", "/uploads"]

USER nonroot:nonroot

# The binary checks itself: there is no curl or shell in this image. See the
# health() function in cmd/caravel.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/caravel", "-health"]

ENTRYPOINT ["/caravel"]
