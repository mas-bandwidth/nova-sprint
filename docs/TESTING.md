# Testing

The same commands CI runs, from the repository root. Per commit
(`.github/workflows/ci.yml`) every job finishes inside 2 minutes; the long runs
are nightly (`.github/workflows/nightly.yml`, no time limit).

The rules: unit tests run in parallel, under 2 minutes
(under 1 minute is the aim), and use no real time, sleep, network or process;
functional tests (a name containing `Functional`) run in parallel, under 2
minutes, and may. Every test calls `t.Parallel()` first. `tools/unitlint`
refuses a test that does not, and a unit test that calls the clock, sleeps,
dials, listens or starts a process; an exception is a comment on the line that
says `unitlint:` and why. A clock in a test starts at 1000, never 0.

Lint:

    test -z "$(gofmt -l .)" || gofmt -l .
    go build -ldflags "-X main.binaryVersion=$(git describe --always --dirty --tags --match 'v[0-9]*')" ./...
    go vet ./...
    go run ./tools/silentlint store card steps edge policy roster run verbs cmd dash tools
    go run ./tools/unitlint

(`make lint` runs the same; `make build` stamps the version as CI does.)
silentlint refuses the silent error forms: S1 a fixed-string error in an err
branch, S2 a refusal re-wrapped without errors.As/AsRefusal/RefusalOr, S3 a
`_ =` discard outside its named cleanups, S4 a bare exit that never writes
the error (tools/silentlint/lint.go names each).

Unit tests (no clock, sockets, network or outside service). Per commit they run
`-short`: the random walks and sweeps take a fifth of their seeds; the nightly
runs every seed:

    go test -count=1 -race -short -skip Functional ./...   # per commit
    go test -count=1 -race -skip Functional ./...          # nightly

Functional tests (each starts its own PostgreSQL with `initdb`; CI fails if `initdb` is missing):

    command -v initdb
    go test -count=1 -race -run Functional ./...

On a Mac with Homebrew PostgreSQL, put its tools on PATH first:

    export PATH=/opt/homebrew/opt/postgresql@18/bin:$PATH

and run them with `-parallel 4`. Every functional test starts its own server,
and each server takes System V shared memory; macOS's default limits
(`kern.sysv.shmmni` 32) run out at the default parallelism (one per core) and
tests fail with "could not create shared memory segment". Linux CI needs no
flag.

    go test -count=1 -race -parallel 4 -run Functional ./...

Trace validation (the code's walks against the models). Per commit it is
short (the store walks at 150 steps); the nightly runs them at 500 and the
Layer 6 traces too (`tla/check-layer6-traces.py`):

    GOFLAGS=-short NOVA_TRACE_PG=1 bash tla/make-traces.sh check   # per commit
    NOVA_TRACE_PG=1 bash tla/make-traces.sh check                  # nightly

TLC, nightly (Store must pass; every witness and reach check must report a
violation). Run it on a bench host, never on the working machine:

    bash tla/ci-tlc.sh

The nightly splits the set into six jobs on the runners labelled `tlc`
(`TLC_SHARD=i/6`, every sixth run), each held to half its host's cores
(`TLC_CORES`, which caps every run's `-workers` and the live runs' sum):

    TLC_SHARD=2/6 TLC_CORES=8 bash tla/ci-tlc.sh

Print the TLC commands without fetching or running anything:

    DRY_RUN=1 bash tla/ci-tlc.sh

`tla/ci-tlc.sh` downloads the pinned `tla2tools.jar` (v1.8.0) from this
repository's `tla-tools-1.8.0` release (upstream replaced its v1.8.0 asset
with a different binary) to `~/.cache/nova-sprint` (override with
`TLA_CACHE`) and checks its sha256.
