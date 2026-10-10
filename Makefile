GO ?= go
GOTEST_P ?= 4

.PHONY: build vet test functional help roadmap

help:
	@echo "make build       go build ./..."
	@echo "make vet         go vet ./..."
	@echo "make test        go vet ./... and go test -p GOTEST_P ./... (the unit tier)"
	@echo "make functional  go test -tags functional ./... (needs redis-server on PATH)"
	@echo "make roadmap     regenerate ROADMAP.md and FIXES.md from their sexp"

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

test: vet
	$(GO) test -p $(GOTEST_P) ./...

functional:
	$(GO) test -p $(GOTEST_P) -tags functional ./...

roadmap:
	$(GO) run ./tools/roadmap --file docs/roadmap.sexp --out ROADMAP.md
	$(GO) run ./tools/roadmap --kind fixes --file docs/fixes.sexp --out FIXES.md
