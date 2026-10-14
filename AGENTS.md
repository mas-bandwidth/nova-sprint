# AGENTS.md — generated map

Do not edit. `make map` regenerates this file.

## Build

```
make build   # go build ./...
make vet     # go vet ./...
```

## Test

```
make test    # go vet ./... then go test -p $(GOTEST_P) ./... (the unit tier)
```

## Command map

| cmd | guard | command |
| --- | --- | --- |
| cmd/nova-card | `go test ./cmd/nova-card` | `make build` |
| cmd/nova-sprint | `go test ./cmd/nova-sprint` | `make build` |
| cmd/nova-work | `go test ./cmd/nova-work` | `make build` |

## CLI rules

- Each tool under `cmd/` builds one binary; run it with `-h` or `help` to list its verbs.
- `nova-sprint` runs a sprint of work cards; `nova-card` reads and reviews cards; `nova-work` files work against a stream.
- Comprehensive verb reference: [docs/CLI.md](docs/CLI.md).
