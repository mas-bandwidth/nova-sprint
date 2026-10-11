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

# the tick's wall-clock gate runs alone, after the rest: beside sibling packages its
# timing measures the machine's load, not the deal (.github/workflows/ci.yml)
TICK_GATE_TEST := ^TestTickDrainAndDealUnderTheGateAtLoad$$

test: vet
	$(GO) test -p $(GOTEST_P) -skip '$(TICK_GATE_TEST)' ./...
	$(GO) test -count=1 -run '$(TICK_GATE_TEST)' ./internal/sprint/

functional:
	$(GO) test -p $(GOTEST_P) -tags functional ./...

roadmap:
	$(GO) run ./tools/roadmap --file docs/roadmap.sexp --out ROADMAP.md
	$(GO) run ./tools/roadmap --kind fixes --file docs/fixes.sexp --out FIXES.md
