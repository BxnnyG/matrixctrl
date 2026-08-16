BINARY     := matrixctrl
MODULE     := github.com/bxnnyg/matrixctrl
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS    := -w -s \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT)

# Go is installed at /usr/local/go/bin on the deployment host and is not on the
# default PATH there (CLAUDE.md), so `make test` failed with "go: No such file or
# directory" for anyone who had not exported it themselves. Fall back to the known
# location when `go` is not resolvable; an overriding GO=… still wins.
GO         ?= $(shell command -v go 2>/dev/null || echo /usr/local/go/bin/go)
GOFLAGS    :=
BUILD_DIR  := bin

.PHONY: all build test check lint web-build web-dev dev clean docker

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

# Everything that must be green before an image is built.
#
# It exists so the *correct* invocation is also the shortest one. The typecheck
# needs `-b`, because web/tsconfig.json is `"files": []` plus project references
# and plain `tsc --noEmit` checks nothing at all and exits 0 — which is how two
# etappes came to be recorded as built while their image did not exist (§4.40).
check:
	$(GO) test ./...
	cd web && ./node_modules/.bin/tsc -b --noEmit
	./scripts/check-sensitive.sh

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR) web/dist

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		-t ghcr.io/bxnnyg/matrixctrl:$(VERSION) \
		-t ghcr.io/bxnnyg/matrixctrl:latest \
		.
