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

.PHONY: all build test check lint verify-ui web-build web-dev dev clean docker

all: web-build build

build: web-build copy-dist
	CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/matrixctrl

web-build:
	cd web && npm ci && npm run build

copy-dist:
	rm -rf cmd/matrixctrl/dist
	cp -r web/dist cmd/matrixctrl/dist
	# Restore the tracked placeholder that `rm -rf` above just removed. Without this
	# line every `make build` deletes a tracked file, leaving the tree dirty and the
	# next clean checkout unable to compile (//go:embed needs a non-empty dist).
	touch cmd/matrixctrl/dist/.gitkeep

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
	./scripts/check-changelog.sh
	./scripts/check-commands.sh
	# gofmt is a CI gate, and `make check` did not run it until 2026-08-17 — so
	# "check green" did not imply "CI green", and E51 shipped unformatted code that
	# only the pipeline would have caught. A local check that omits a remote gate
	# answers a narrower question than the one being asked of it (§4.52).
	@unformatted=$$($(GO)fmt -l ./cmd ./internal); \
	  test -z "$$unformatted" || { echo "gofmt needed:"; echo "$$unformatted"; exit 1; }

lint:
	golangci-lint run ./...

# Post-deploy UI check: every functional route in a real browser (PROZESS §4).
#
# A target because until 2026-08-17 this existed only as an incantation copied out
# of a plan file from E13, so it was effectively never run — and the four Phase 2
# screens had drifted out of its route list unnoticed (§4.48).
#
#   make verify-ui BASE=https://panel.example.com
#   make verify-ui BASE=… ROOM_ID='!abc:example.org'   # adds the room detail screen
#
# MATRIXCTRL_TOKEN must be set to reach the authenticated routes; without it the
# run exits non-zero rather than passing on a handful of skips.
verify-ui:
	@test -n "$(BASE)" || (echo "BASE is required, e.g. make verify-ui BASE=https://panel.example.com" && exit 2)
	cd web && node scripts/verify-ui.mjs --base "$(BASE)" \
		$(if $(ROOM_ID),--room-id "$(ROOM_ID)") \
		$(if $(OUT),--out "$(OUT)") \
		$(VERIFY_FLAGS)

clean:
	rm -rf $(BUILD_DIR) web/dist

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		-t ghcr.io/bxnnyg/matrixctrl:$(VERSION) \
		-t ghcr.io/bxnnyg/matrixctrl:latest \
		.
