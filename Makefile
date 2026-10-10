GO ?= go
GOTEST_P ?= 4

.PHONY: build vet test functional help

help:
	@echo "make build       go build ./..."
	@echo "make vet         go vet ./..."
	@echo "make test        go vet ./... and go test -p GOTEST_P ./... (the unit tier)"
	@echo "make functional  go test -tags functional ./... (needs redis-server on PATH)"

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

test: vet
	$(GO) test -p $(GOTEST_P) ./...

functional:
	$(GO) test -p $(GOTEST_P) -tags functional ./...
