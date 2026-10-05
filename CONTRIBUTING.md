# Help make nova-sprint easier to work with

Thanks for helping. A useful change can be a clearer error, a better example,
a repaired handoff, or a feature that makes working with an AI team easier.
Describe the problem from the user's point of view, then show how your change
helps.

## Find the right part of the repository

| Path | What lives there |
|---|---|
| `cmd/nova-sprint` | The sprint commands, server, and dashboard entry point. |
| `internal/card`, `internal/cardgen` | Card generation and linting behind `nova-sprint card`. |
| `cmd/nova-work` | GitHub issue import and verification. |
| `internal/sprint`, `internal/sprintdash` | Coordination and the browser view. |
| `internal/cardgen`, `internal/workfile`, `internal/workgh`, `internal/worklang` | Card and work-tree implementation. |
| `internal/work` | The proposed durable sprint-program store; see its [status note](internal/work/NOTE.md). |
| `docs` | Guides, specifications, command examples, and recorded reviews. |
| `tla` | State-machine models and their recorded runs. |
| `assets` | Project artwork, with [provenance](docs/ASSET-PROVENANCE.md). |

This repository was split from Nova Tools on 2026-10-04. It still contains
copies of shared Nova Tools packages while public API imports replace them.
For a fix to a shared package, check the upstream Nova Tools implementation
first and carry the fix through both repositories as needed. Keep that
migration detail out of the product introduction.

## Build and check a change

Use Go 1.26.6 or newer. Run build, test, and vet gates on a Linux bench.

```sh
make build
make test
```

`make test` runs vet and the unit tests, with four package processes by
default. The functional tier also needs `redis-server`:

```sh
make functional
```

Start with the packages your change affects. For a documentation edit, check
links, examples, and the tests that read those documents. Some documentation
is executable: the transcripts in [TESTS.md](docs/TESTS.md) are run by command
tests, and the [dashboard specification](docs/SPEC-SPRINT-DASHBOARD.md) is
compared with the page by `TestDashboardPageIsTheSpec`.

The [test harness guide](internal/testkit/HARNESS.md) explains shared helpers.
The [model guide](tla/README.md) explains the TLA+ checks; a recorded model run
is evidence about that model and revision, not a fresh test of your change.

## Keep the explanation close to the change

Tell readers what changed, why, and how you checked it. Include the specific
command and result for a test you ran. Say when something was inspected but
not executed. Keep observed failures, historical review reports, and owner
quotations intact; add context around them instead of rewriting the evidence.

Write guides for the programmer trying to get something done. Introduce a
term before relying on it, use a small example, and link to the precise
contract when the detail would interrupt the explanation. The
[brand and voice guide](docs/BRAND.md) has examples.

The README is written for the v1.0.0 product. Keep interim command names,
unbuilt designs, and migration status in the relevant technical references.
Do not turn a planned command into a copy-and-paste example before it exists.
