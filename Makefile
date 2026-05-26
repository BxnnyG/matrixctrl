BINARY     := matrixctrl
MODULE     := github.com/bxnny/matrixctrl
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS    := -w -s \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT)

GO         := go
GOFLAGS    :=
BUILD_DIR  := bin

.PHONY: all build test lint web-build web-dev dev clean docker

all: web-build build

build: web-build copy-dist
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/matrixctrl

web-build:
	cd web && npm ci && npm run build

copy-dist:
	rm -rf cmd/matrixctrl/dist
	cp -r web/dist cmd/matrixctrl/dist

web-dev:
	cd web && npm run dev

dev:
	docker compose -f deploy/dev/docker-compose.yaml up -d
	MATRIXCTRL_DB_URL="postgres://matrixctrl:dev@localhost:5432/matrixctrl?sslmode=disable" \
	MATRIXCTRL_AUTH_MODE=bootstrap \
	MATRIXCTRL_ESS_NAMESPACE=ess \
	MATRIXCTRL_ESS_RELEASE=ess \
	$(GO) run ./cmd/matrixctrl

test:
	$(GO) test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR) web/dist

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		-t ghcr.io/bxnny/matrixctrl:$(VERSION) \
		-t ghcr.io/bxnny/matrixctrl:latest \
		.
