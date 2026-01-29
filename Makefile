# kubestellar-mcp Makefile

MODULE := github.com/kubestellar/klaude
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

LDFLAGS := -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.BuildDate=$(BUILD_DATE) \
	-X $(MODULE)/internal/version.GitCommit=$(GIT_COMMIT)

GO := go
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

.PHONY: all build build-ops build-deploy clean test install lint

all: build

build: build-ops build-deploy

build-ops:
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-ops ./cmd/kubestellar-ops

build-deploy:
	CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-deploy ./cmd/kubestellar-deploy

build-all: build-ops-all build-deploy-all

build-ops-all:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-ops-linux-amd64 ./cmd/kubestellar-ops
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-ops-linux-arm64 ./cmd/kubestellar-ops
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-ops-darwin-amd64 ./cmd/kubestellar-ops
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-ops-darwin-arm64 ./cmd/kubestellar-ops

build-deploy-all:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-deploy-linux-amd64 ./cmd/kubestellar-deploy
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-deploy-linux-arm64 ./cmd/kubestellar-deploy
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-deploy-darwin-amd64 ./cmd/kubestellar-deploy
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags="$(LDFLAGS)" -o bin/kubestellar-deploy-darwin-arm64 ./cmd/kubestellar-deploy

clean:
	rm -rf bin/

test:
	$(GO) test -v ./...

install: build
	cp bin/kubestellar-ops $(GOPATH)/bin/
	cp bin/kubestellar-deploy $(GOPATH)/bin/

lint:
	golangci-lint run ./...

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build         - Build both binaries for current platform"
	@echo "  build-ops     - Build kubestellar-ops for current platform"
	@echo "  build-deploy  - Build kubestellar-deploy for current platform"
	@echo "  build-all     - Build all binaries for all platforms"
	@echo "  clean         - Remove build artifacts"
	@echo "  test          - Run tests"
	@echo "  install       - Install to GOPATH/bin"
	@echo "  lint          - Run linter"
