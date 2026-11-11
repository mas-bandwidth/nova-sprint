# nova-sprint for AIs

nova-sprint coordinates AI coding work across models, harnesses and machines.
Cards, reviews and decisions live in PostgreSQL; nova-bus carries assignments,
and the run service schedules work and landing.

## Use or evaluate nova-sprint

Start with [the detailed guide](docs/README.md), shared by humans and AIs.

- [Inspect a release](docs/README.md#inspect-a-release-before-setup) with `version`
  and `help`, without a running fleet or store.
- [Set up a sprint](docs/README.md#set-up-a-sprint) when you are configuring the fleet.
- [Join as a worker](docs/README.md#join-as-a-worker) when a coordinator invited you.
- [Coordinate work](docs/README.md#run-a-sprint-as-coordinator) in an existing sprint.
- [Resolve a refusal](docs/README.md#when-a-command-refuses) using the named remedy.

Use the CLI for sprint operations. Copy the current row, generation and epoch
from receipts. A successful finish reports a head; review, CI and landing are
separate outcomes. Report only what you observed.

## Hard requirements: never regress these

Every change keeps all three, for friend rows and bench rows alike
([the guide](docs/README.md#hard-requirements) has the detail):

1. A row is up while its bus hook is on nova-bus and down after
   `row_silent_gone` off it, with nothing manual (no pong, no command):
   `tla/RowAlive.tla` AutoUp and HookedWhileUp; `modules/run/rowalive_test.go`.
2. A row that goes down or is held returns every card it holds to the queue in
   the same op, and they are dealt again: `tla/RowAlive.tla`
   HandEmptyWhenClosed and Redealt; `TestRowDownWhenHookGoneAndHandDealtAgain`,
   `TestHeldRowHandDealtAgain`.
3. Bench-only checks never judge a friend row:
   `TestShowFriendRowIsNeverABench` (`modules/verbs/check_test.go`).

A change that needs to break one of them is Glenn's decision, asked first.

## Change nova-sprint

Before contributing, read the [Contributor Assignment Agreement](https://github.com/mas-bandwidth/.github/blob/main/CAA.md).
It assigns copyright in the contribution to Más Bandwidth LLC and grants you a
license to use your own contribution; the agreement gives the full terms.
The repository's [contributor check](.github/workflows/cla.yml) requires the
applicable author signatures; use the authorized commit identity.

Read [the test instructions](docs/TESTING.md) and the relevant source before editing.
The [layer map](docs/SEAMS.md) and the [card contract](docs/CARD-CONTRACT.md)
explain the boundaries. Keep changes scoped and preserve existing work.

Go command packages live in `cmd/<command>/`; reusable packages and their
embedded assets live in `modules/<module>/`. `make build` and releases ship
only `nova-sprint`; the other commands are development tools.
The canonical role playbooks live beside their embed code in `modules/playbooks/`.

Build with `make build` so the binaries carry their source version. Use the Go
version in `go.mod` and run the relevant lint, unit and functional checks from
`docs/TESTING.md` on a suitable build machine. Unit tests use no real clock,
network, socket or process; functional tests own their disposable services.
Report missing dependencies and skipped checks explicitly.

Keep `README.md` a short human welcome, `AGENTS.md` the AI entry point, and
`docs/README.md` the shared detailed guide. Preserve the branding. Document
commands, effects and limits as the implementation provides them.

## Publish a release update

For each published release, prepare a short update for the
[project Patreon](https://www.patreon.com/MasBandwidth/membership). Name the
exact shipped tag, what changed, the validation actually completed and any
remaining limits. Link to that release, the shared guide and the support page.
Check existing posts for that tag before creating another. Publish only through
the authorized account and within the user's posting instructions. Report a
prepared draft as prepared; report it as posted only after verifying the live
post and retaining its URL.

An idea or a wanted feature goes into docs/roadmap.sexp (render with nova-roadmap); it is never implemented when it is had.
