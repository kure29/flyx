# Built from the repository root (the built-in status page theme comes from
# vite-frontend):  docker build -f go-backend/Dockerfile .
# Files sent to the build are listed in Dockerfile.dockerignore.

# The built-in status page theme, embedded in paneld (internal/statustheme).
FROM --platform=$BUILDPLATFORM node:22-alpine AS status-theme
WORKDIR /app
COPY vite-frontend/package.json vite-frontend/pnpm-lock.yaml vite-frontend/pnpm-workspace.yaml ./
RUN corepack enable pnpm && pnpm install --frozen-lockfile
COPY vite-frontend/ ./
RUN pnpm run build:status-theme

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder
WORKDIR /src

COPY go-backend/go.mod go-backend/go.sum ./
RUN go mod download

COPY go-backend/ ./
COPY --from=status-theme /app/dist-status-theme/ ./internal/statustheme/builtin/dist/
ARG TARGETOS
ARG TARGETARCH
# owner/repo whose releases the panel installs nodes and upgrades from.
ARG GITHUB_REPO
RUN test -f internal/statustheme/builtin/dist/index.html
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} env ${TARGETARCH:+GOARCH=${TARGETARCH}} go build -ldflags="${GITHUB_REPO:+-X 'go-backend/internal/http/handler.githubRepo=${GITHUB_REPO}'}" -o /out/paneld ./cmd/paneld

FROM docker:27-cli AS dockercli

FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/paneld /app/paneld
COPY --from=dockercli /usr/local/bin/docker /usr/local/bin/docker
COPY --from=dockercli /usr/local/libexec/docker/cli-plugins/docker-compose /usr/local/libexec/docker/cli-plugins/docker-compose

ENV SERVER_ADDR=:6365
EXPOSE 6365
ENTRYPOINT ["/app/paneld"]
