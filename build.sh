#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0-local")}"
OUTPUT="${1:-${ROOT_DIR}/dockdeploy}"

echo "=========================================================="
echo "  Building dockdeploy single standalone binary (${VERSION})"
echo "=========================================================="

command -v node >/dev/null 2>&1 || { echo "error: node is required to build frontend" >&2; exit 1; }
command -v npm >/dev/null 2>&1 || { echo "error: npm is required to build frontend" >&2; exit 1; }
command -v go >/dev/null 2>&1 || { echo "error: go is required to compile binary" >&2; exit 1; }

echo "==> [1/3] Checking frontend dependencies..."
if [ ! -d "${ROOT_DIR}/frontend/node_modules" ]; then
    echo "Installing frontend node_modules..."
    (cd "${ROOT_DIR}/frontend" && npm ci)
fi

echo "==> [2/3] Building production frontend assets..."
(cd "${ROOT_DIR}/frontend" && npm run build)

echo "==> [3/3] Compiling standalone Go single binary..."
mkdir -p "$(dirname "${OUTPUT}")"
(cd "${ROOT_DIR}/server" && \
    CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags="-s -w -X github.com/sirtheprogrammer/docker-deployments/server/internal/api.Version=${VERSION}" \
        -o "${OUTPUT}" \
        ./cmd/dockdeploy)

chmod +x "${OUTPUT}"

echo ""
echo "=========================================================="
echo "  SUCCESS: Standalone single binary built at:"
echo "    ${OUTPUT}"
echo ""
echo "  To run locally with zero dependency setup:"
echo "    ${OUTPUT}"
echo ""
echo "  Available CLI options:"
echo "    -port 8081      (custom HTTP port, default 8081)"
echo "    -db path.db     (custom SQLite db path, default dockdeploy.db)"
echo "    -version        (print version and exit)"
echo "=========================================================="
