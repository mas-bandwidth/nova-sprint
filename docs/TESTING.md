# Testing

These are the commands CI runs (`.github/workflows/ci.yml`), from the repository root. `make lint` runs the lint block; `make test` runs the unit tier; `make build` stamps the version as CI does.

## Lint

    test -z "$(gofmt -l .)" || gofmt -l .
    go build -ldflags "-X main.binaryVersion=$(git describe --always --dirty --tags --match 'v[0-9]*')" ./...
    go vet ./...
    go run ./tools/silentlint store card steps edge policy roster run verbs cmd dash tools

`silentlint` refuses the silent error forms: S1 a fixed-string error in an err branch, S2 a refusal re-wrapped without `errors.As`/`AsRefusal`/`RefusalOr`, S3 a `_ =` discard outside its named cleanups, S4 a bare exit that never writes the error (`tools/silentlint/lint.go` names each).

## Unit tests

No clock, sockets, network, subprocesses or outside service. Time is an argument.

    go test -count=1 -race -skip Functional ./...

## Functional tests

Named `TestFunctional*`. Each starts its own PostgreSQL with `initdb`; CI fails if `initdb` is missing.

    command -v initdb
    go test -count=1 -race -run Functional ./...

On a Mac with Homebrew PostgreSQL, put its tools on `PATH` first and limit parallelism. Every functional test starts its own server and each server takes System V shared memory; macOS's default limits (`kern.sysv.shmmni` 32) run out at the default parallelism (one per core), and tests fail with `could not create shared memory segment`. Linux needs no flag.

    export PATH=/opt/homebrew/opt/postgresql@18/bin:$PATH
    go test -count=1 -race -parallel 4 -run Functional ./...

## Trace checks

The code's own walks emit traces that TLC checks against the models in `tla/` (see `tla/README.md`, "Trace validation"). They need `initdb`, Java and a bench host:

    bash tla/make-traces.sh check

writes the traces of the store (Fake and PostgreSQL), card, policy and steps walks on fixed seeds under `tla/traces/`, checks each with `tla/check-trace.sh` in parallel, and requires the divergence breakers to be refused. Layer 6 (Run and Setup fixtures) runs through `tla/check-layer6-traces.py`; its modes and arguments are in `tla/CHECK-LAYER6-TRACES.md`.

## TLC

Run on a bench host, never on the working machine. `Store` and the other models must pass (TLC exit 0); every witness and reach check must report a violation (TLC exit 12).

    bash tla/ci-tlc.sh

`DRY_RUN=1 bash tla/ci-tlc.sh` prints the TLC commands without fetching or running anything. The script downloads the pinned `tla2tools.jar` (v1.8.0) to `~/.cache/nova-sprint` (override with `TLA_CACHE`) and checks its sha256.

## CI jobs

`lint`, `unit`, `functional`, `trace` and `tlc` are separate jobs that run in parallel. Timeouts: unit 30 minutes (each package gets `-timeout 20m`), functional 20, trace 60, tlc 180. The `tlc` job runs on a runner labelled `tlc` because the TLC runs need far more memory than a bench has.
