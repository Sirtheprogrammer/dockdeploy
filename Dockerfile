# syntax=docker/dockerfile:1

# --- frontend -----------------------------------------------------------
FROM node:24-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# --- backend ------------------------------------------------------------
FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
# The SPA is embedded by internal/web, so it must be in place before the build.
COPY --from=frontend /app/dist ./internal/web/dist
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X github.com/sirtheprogrammer/docker-deployments/server/internal/api.Version=${VERSION}" \
      -o /out/dockdeploy ./cmd/dockdeploy

# --- runtime ------------------------------------------------------------
FROM alpine:3.21
# git: controller-side clones for the registry build strategy.
# openssh-client: known_hosts/key format helpers and operator debugging.
RUN apk add --no-cache ca-certificates git openssh-client tzdata \
 && adduser -D -u 10001 -h /home/app app
USER app
WORKDIR /home/app
COPY --from=backend /out/dockdeploy /usr/local/bin/dockdeploy

ENV APP_ENV=production PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1

ENTRYPOINT ["dockdeploy"]
