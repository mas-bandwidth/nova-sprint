# nova-sprint builds: every command stamped with git describe, so a build
# from source reports where it came from (a backup records it; a build with
# no stamp and no VCS stamp is "dev" and cannot back up). Releases are built
# by .github/workflows/release.yml, stamped with the tag.

VERSION ?= $(shell git describe --always --dirty --tags --match 'v[0-9]*')
LDFLAGS := -X main.binaryVersion=$(VERSION) -X github.com/mas-bandwidth/nova-sprint/modules/dash.Version=$(VERSION)

.PHONY: build install test lint version

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/ ./cmd/nova-sprint

# install puts the built commands where a service finds them first
# (~/.local/bin, nova-fleet's nova_bin_dir): each as a new file, signed ad hoc
# on macOS, renamed over the old one, never overwritten in place, which macOS
# kills (cmd/placebin). Releases onto benches are nova-fleet's install.
BINDIR ?= $(HOME)/.local/bin

install: build
	go run ./cmd/placebin bin/nova-sprint $(BINDIR)

# the unit tier (docs/TESTING.md): no real time, sockets, network or process;
# the test binaries run with no PATH and no HOME, as CI runs them
test:
	go test -race -short -skip Functional -exec "env PATH=/nonexistent HOME=/nonexistent" ./...

lint:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go run ./cmd/silentlint modules/store modules/card modules/steps modules/edge modules/policy modules/roster modules/run modules/verbs cmd modules/dash
	go run ./cmd/unitlint
	go tool staticcheck ./...
	out=$$(go tool deadcode -test ./... | grep -v -e '_test\.go:' -e 'unreachable func: Cache\.SetRoster$$' || true); test -z "$$out" || { echo "$$out"; exit 1; }

version:
	@echo $(VERSION)
