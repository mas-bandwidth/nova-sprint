# Testing

The same commands CI runs (`.github/workflows/ci.yml`), from the repository root.

Lint:

    test -z "$(gofmt -l .)" || gofmt -l .
    go build ./...
    go vet ./...

Unit tests (no clock, sockets, network or outside service):

    go test -count=1 -race -skip Functional ./...

Functional tests (each starts its own PostgreSQL with `initdb`; CI fails if `initdb` is missing):

    command -v initdb
    go test -count=1 -race -run Functional ./...

On a Mac with Homebrew PostgreSQL, put its tools on PATH first:

    export PATH=/opt/homebrew/opt/postgresql@18/bin:$PATH

TLC (Store must pass; every witness and reach check must report a violation).
Run it on a bench host, never on the working machine:

    bash tla/ci-tlc.sh

Print the TLC commands without fetching or running anything:

    DRY_RUN=1 bash tla/ci-tlc.sh

`tla/ci-tlc.sh` downloads the pinned `tla2tools.jar` (v1.8.0) to
`~/.cache/nova-sprint` (override with `TLA_CACHE`) and checks its sha256.
