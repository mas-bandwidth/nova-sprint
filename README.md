# nova-sprint

**nova-sprint is the opinionated system: a work processor for teams of AIs.
nova-tools are the general tools that support it**
([nova-tools](https://github.com/mas-bandwidth/nova-tools)).

nova-tools are the general tools, each usable on its own in any AI workflow: run
an AI task in a sandbox with a budget (nova-swarm), cheap typed decisions
(nova-decide), a bus between AIs (nova-bus), tables in Redis (nova-table),
secrets, configuration, the sandbox, updates. If you want to build your own
workflow, build it from those.

nova-sprint is one system built on them, with its opinions written in:
work is cut into **cards**, cards run in **streams** behind **sentinels**, each
card is dealt by **tier** to a fleet of machines and friends, finished work is
**read** by independent readers before the **lander** merges it, and the feed
rules keep every machine at its width. The sprint dashboard shows all of it.

This repository moved out of nova-tools on 2026-10-04 (mas-bandwidth/ideas#850).
It holds:

- `cmd/nova-sprint`: the one binary: the server (`nova-sprint run --listen`), the
  coordinator's verbs, the dashboard (`nova-sprint dashboard`).
- `cmd/nova-card` and `cmd/nova-work`: the card generator and the work tree, being
  folded into `nova-sprint` as verbs (until then they build as separate binaries).
- `internal/sprint`, `internal/sprintdash`, `internal/cardgen`, `internal/provbalance`,
  `internal/workfile`, `internal/workgh`, `internal/worklang`: the sprint's own packages.
- `internal/...` (everything else): **copies** of the nova-tools packages nova-sprint
  uses, taken at the nova-tools commit named in the first commit. They are replaced,
  one at a time, by imports of a public nova-tools API; until then a fix to one of
  them is made in nova-tools first.
- `docs/`: the sprint's specs (`SPEC-SPRINT.md`, `SPEC-SPRINT-DASHBOARD.md`), the
  coordinator's runbooks, the nova-work specs, and the command and test references.
- `tla/`: the TLA+ models of the sprint (`SprintEvents`, `DirtyTick`, `DirtyTickRead`,
  `RouteIndex`, `Level`, `Land`) and of nova-work (`WorkImport`), with their
  `CASES.tsv` and `RUNS.tsv` rows. The TLC runner is still nova-tools' `tools/tlacheck`.

## Building

Go 1.26.6 or newer.

```sh
make build   # go build ./...
make test    # go vet ./... and go test -p 4 ./...
go install ./cmd/nova-sprint
```

Running a sprint needs the rest of nova-tools on the machine: nova-swarm (the
members), nova-sandbox (the wall), nova-table and nova-redis (the store),
nova-config (the fleet), nova-secrets (the seat's credentials). Start with
`nova-sprint help` and [the sprint contract](docs/SPEC-SPRINT.md).

## License

MIT; see [LICENSE](LICENSE).
