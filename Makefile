# nova-sprint builds: every command stamped with git describe, so a build
# from source reports where it came from (a backup records it; a build with
# no stamp and no VCS stamp is "dev" and cannot back up). Releases are built
# by .github/workflows/release.yml, stamped with the tag.

VERSION ?= $(shell git describe --always --dirty --tags --match 'v[0-9]*')
LDFLAGS := -X main.binaryVersion=$(VERSION) -X github.com/mas-bandwidth/nova-sprint/dash.Version=$(VERSION)

.PHONY: build install test lint version

build:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/ ./cmd/...

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/...

# the unit tier (docs/TESTING.md): no real time, sockets or network
test:
	go test -skip Functional ./...

lint:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go run ./tools/silentlint store card steps edge policy roster run verbs cmd dash tools

version:
	@echo $(VERSION)
