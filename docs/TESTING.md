# Testing

These are the commands CI runs, from the repository root. Per commit (`.github/workflows/ci.yml`) every job finishes inside 2 minutes; the long runs are nightly (`.github/workflows/nightly.yml`, no time limit). `make lint` runs the lint block; `make test` runs the per-commit unit tier; `make build` stamps the version as CI does.

Rules:

- Unit tests run in parallel, finish under 2 minutes (under 1 minute is the aim), and use no real time, sleep, network or process. Functional tests (a name containing `Functional`) run in parallel, finish under 2 minutes, and may use them.
- Every test calls `t.Parallel()` first. `tools/unitlint` refuses a test that does not, and a unit test that calls the clock, sleeps, dials, listens or starts a process. An exception is a comment on the line that says `unitlint:` and why.
- A clock in a test starts at 1000, never 0.

## Lint

    test -z "$(gofmt -l .)" || gofmt -l .
    go build -ldflags "-X main.binaryVersion=$(git describe --always --dirty --tags --match 'v[0-9]*')" ./...
    go vet ./...
    go run ./tools/silentlint store card steps edge policy roster run verbs cmd dash tools
    go run ./tools/unitlint

`silentlint` refuses the silent error forms: S1 a fixed-string error in an err branch, S2 a refusal re-wrapped without `errors.As`/`AsRefusal`/`RefusalOr`, S3 a `_ =` discard outside its named cleanups, S4 a bare exit that never writes the error (`tools/silentlint/lint.go` names each).

## Unit tests

No clock, sockets, network, subprocesses or outside service. Time is an argument. Per commit they run `-short`: the random walks and sweeps take a fraction of their seeds and steps; the nightly runs every seed.

    go test -count=1 -race -short -skip Functional ./...   # per commit
    go test -count=1 -race -skip Functional ./...          # nightly

## Functional tests

Named `TestFunctional*`. Each starts its own PostgreSQL with `initdb`; CI fails if `initdb` is missing.

    command -v initdb
    go test -count=1 -race -run Functional ./...

On a Mac with Homebrew PostgreSQL, put its tools on `PATH` first and limit parallelism. Every functional test starts its own server and each server takes System V shared memory; macOS's default limits (`kern.sysv.shmmni` 32) run out at the default parallelism (one per core), and tests fail with `could not create shared memory segment`. Linux needs no flag.

    export PATH=/opt/homebrew/opt/postgresql@18/bin:$PATH
    go test -count=1 -race -parallel 4 -run Functional ./...

## Trace checks

The code's own walks emit traces that TLC checks against the models in `tla/` (see `tla/README.md`, "Trace validation"). They need `initdb`, Java and a bench host. Per commit the run is short (the store walks at 150 steps); the nightly runs the store walks at 500 steps and the Layer 6 traces too (`tla/check-layer6-traces.py`; its modes and arguments are in `tla/CHECK-LAYER6-TRACES.md`).

    GOFLAGS=-short NOVA_TRACE_PG=1 bash tla/make-traces.sh check   # per commit
    NOVA_TRACE_PG=1 bash tla/make-traces.sh check                  # nightly

`make-traces.sh check` writes the traces of the store (Fake and PostgreSQL), card, policy and steps walks on fixed seeds under `tla/traces/`, checks each with `tla/check-trace.sh` in parallel, and requires the divergence breakers to be refused.

## TLC

Nightly. Run on a bench host, never on the working machine. `Store` and the other models must pass (TLC exit 0); every witness and reach check must report a violation (TLC exit 12).

    bash tla/ci-tlc.sh

The nightly splits the set into six jobs on the runners labelled `tlc` (`TLC_SHARD=i/6` runs every sixth run), each held to half its host's cores (`TLC_CORES` caps every run's `-workers` and the sum of the live runs'):

    TLC_SHARD=2/6 TLC_CORES=8 bash tla/ci-tlc.sh

`DRY_RUN=1 bash tla/ci-tlc.sh` prints the TLC commands without fetching or running anything. The script downloads the pinned `tla2tools.jar` (v1.8.0) from this repository's `tla-tools-1.8.0` release to `~/.cache/nova-sprint` (override with `TLA_CACHE`) and checks its sha256.

## CI jobs

`lint`, `unit`, `functional` and `trace` are separate per-commit jobs that run in parallel, each with a 2-minute timeout (`go test` gets `-timeout 100s`). The nightly workflow runs the unit tier without `-short`, the full-length traces, and `tlc` in six shards on runners labelled `tlc`, because the TLC runs need far more memory than a bench has.
