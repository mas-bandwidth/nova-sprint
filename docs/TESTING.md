# Testing

The same commands CI runs, from the repository root. Per commit
(`.github/workflows/ci.yml`) every job finishes inside 2 minutes; the long runs
are nightly (`.github/workflows/nightly.yml`, no time limit).

The rules: unit tests run in parallel, under 2 minutes
(under 1 minute is the aim), and use no real time, sleep, network or process;
functional tests (a name containing `Functional`) run in parallel, under 2
minutes, and may. Every test calls `t.Parallel()` first. `cmd/unitlint`
refuses a test that does not, and a unit test that calls the clock, sleeps,
dials, listens or starts a process; an exception is a comment on the line that
says `unitlint:` and why. A clock in a test starts at 1000, never 0.

Lint:

    test -z "$(gofmt -l .)" || gofmt -l .
    go build -ldflags "-X main.binaryVersion=$(git describe --always --dirty --tags --match 'v[0-9]*')" ./...
    go vet ./...
    go run ./cmd/silentlint modules/store modules/card modules/steps modules/edge modules/policy modules/roster modules/run modules/verbs cmd modules/dash
    go run ./cmd/unitlint
    go tool staticcheck ./...
    go tool deadcode -test ./... | grep -v -e '_test\.go:' -e 'unreachable func: Cache\.SetRoster$'   # prints nothing

(`make lint` runs the same; `make build` stamps the version as CI does.)
staticcheck and deadcode run at the versions go.mod pins as tools. deadcode
reports a function nothing reaches from a main or a test: delete it, unless
it is a seam a test uses to observe or perturb the code (fault injection, a
probe, an oracle). A test file's fakes implement a whole interface, so their
lines are not counted. dash's `Cache.SetRoster` is unwired rather than dead
(the store's own board never sets a roster) and is listed until it is wired.
silentlint refuses the silent error forms: S1 a fixed-string error in an err
branch, S2 a refusal re-wrapped without errors.As/AsRefusal/RefusalOr, S3 a
`_ =` discard outside its named cleanups, S4 a bare exit that never writes
the error (cmd/silentlint/lint.go names each).

Unit tests (no clock, sockets, network, process or outside service). Per
commit they run `-short`: the random walks and sweeps take a fifth of their
seeds; the nightly runs every seed. The test binaries run with an empty PATH
and a HOME that does not exist, so a unit test that starts a program (by name,
or from `~/.local/bin`) fails on every host, not only on one without the
program:

    go test -count=1 -race -short -skip Functional -exec "env PATH=/nonexistent HOME=/nonexistent" ./...   # per commit
    go test -count=1 -race -skip Functional -exec "env PATH=/nonexistent HOME=/nonexistent" ./...          # nightly

Per commit, CI splits the unit and functional tiers into shards, one job
each (`ci.yml`, the matrices): the same tests, each run once, so each job
fits 2 minutes on a loaded runner. A long test is split by its subtests with
one complement shard, and a shard named `rest` runs every package no other
shard names. `go run ./cmd/cishards` (in the lint job) checks that every test
runs in exactly one shard; a new test or package needs no change there.

Functional tests (each starts its own PostgreSQL with `initdb`; CI fails if `initdb` is missing).
`TestFunctionalREADMECommandsRunCold` builds the binary and runs the commands
README.md and docs/README.md show that run cold (help, version, `help <verb>`
for every verb they name, the refusals), with the guide's exit-code table as
the oracle.

What runs where:

- Per commit (`ci.yml`, the `functional` jobs, for pull requests and pushes
  to `main`; one job each for `./cmd/...`, `modules/store` and
  `modules/verbs`, and one for every other package): every test named
  `Functional`, with `NOVA_FUNCTIONAL=1` (the
  tests behind it start their own PostgreSQL: backup round trips, clear,
  setup lock, sprint role, schema identity, and the `up` child test) and
  `NOVA_CONFIG_SCHEMA_FILE` set to nova-config's `config/schema.sql` from
  the nova-tools version go.mod links (the roster test pins its sha256). The end to
  end test and the bus tests also need `redis-server`, `nova-bus`, `pg_ctl`,
  `pg_dump`, `git` and `go` on PATH.
- Nightly (`nightly.yml`, the `functional` job): `TestFunctionalSetupBusRedis`
  with `NOVA_FUNCTIONAL_BUS=1`; it also needs `age-keygen`, `nova-secrets`,
  `nova-bus` and `redis-server`.
- By hand: `TestFunctionalPublicVersionSSH` with `NOVA_FUNCTIONAL_SSH=1`
  (it needs an isolated local `sshd`); no CI job runs it.

When `CI` is set, a functional test whose tool is missing fails and names
the tool; without `CI` it skips.

    command -v initdb
    go mod download github.com/mas-bandwidth/nova-tools/v2
    export NOVA_CONFIG_SCHEMA_FILE=$(go list -m -f '{{.Dir}}' github.com/mas-bandwidth/nova-tools/v2)/config/schema.sql
    NOVA_FUNCTIONAL=1 go test -count=1 -race -run Functional ./...                                       # per commit
    NOVA_FUNCTIONAL_BUS=1 go test -count=1 -race -run '^TestFunctionalSetupBusRedis$' ./cmd/nova-sprint  # nightly
    NOVA_FUNCTIONAL_SSH=1 go test -count=1 -race -run '^TestFunctionalPublicVersionSSH$' ./cmd/nova-sprint # by hand

On a Mac with Homebrew PostgreSQL, put its tools on PATH first:

    export PATH=/opt/homebrew/opt/postgresql@18/bin:$PATH

and run them with `-parallel 4`. Every functional test starts its own server,
and each server takes System V shared memory; macOS's default limits
(`kern.sysv.shmmni` 32) run out at the default parallelism (one per core) and
tests fail with "could not create shared memory segment". Linux CI needs no
flag.

    NOVA_FUNCTIONAL=1 go test -count=1 -race -parallel 4 -run Functional ./...

Trace validation (the code's walks against the models). Per commit it is
short (the store walks at 150 steps); the nightly runs them at 500 and the
Layer 6 traces too (`tla/check-layer6-traces.py`):

    GOFLAGS=-short NOVA_TRACE_PG=1 bash tla/make-traces.sh check <group>   # per commit, one CI job per group
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
