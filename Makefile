.PHONY: all build build-frontend build-backend run test clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0-local")
LDFLAGS := -s -w -X github.com/sirtheprogrammer/docker-deployments/server/internal/api.Version=$(VERSION)

all: build

build-frontend:
	@echo "==> Building frontend SPA..."
	cd frontend && npm run build

build-backend:
	@echo "==> Building Go single binary with embedded frontend..."
	cd server && CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o ../dockdeploy ./cmd/dockdeploy

build: build-frontend build-backend
	@echo ""
	@echo "=========================================================="
	@echo "  dockdeploy single binary successfully built: ./dockdeploy"
	@echo "  Run locally with zero dependencies:"
	@echo "    ./dockdeploy"
	@echo "  Dashboard will be available at: http://localhost:8081"
	@echo "=========================================================="

run: build
	./dockdeploy

test:
	@echo "==> Running backend tests..."
	cd server && go test ./...

clean:
	rm -f dockdeploy
	rm -f dockdeploy.db*
	rm -rf frontend/dist
