# AGENTS.md — nova-sprint

Read this before writing any code here.

## What this repository is

nova-sprint holds two commands, `nova-sprint` and `nova-card`, and only the packages that serve them. Nothing else lives here.

Everything general-purpose lives in [nova-tools](https://github.com/mas-bandwidth/nova-tools) and is imported from it. That covers the bus, config, the friend and swarm seats, safe paths, TLC and TLA+ running, roadmaps, GitHub capture, sexp, tables and the shared helpers.

## Never copy anything across from nova-tools

The owner, 2026-10-18: "This keeps happening. We really need to make sure that nova-sprint does not copy across anything."

- **Never copy** a nova-tools package, file, function or helper into this repository. That includes a copy made "for now", to break an import cycle, to change one line, or because it is only a few lines long.
- **Look first.** Before writing code, search nova-tools (`pkg/`, `internal/`, `cmd/`, `tools/`) and the recent git log of both repositories. If something close exists, extend it in nova-tools.
- **Change nova-tools first.** A change to a shared package lands in nova-tools. This repository then pins that exact nova-tools commit in `go.mod`.
- **Fix blocks at the source.** If an import cycle, an unexported name or a missing option stops you, fix it in nova-tools: export the name, split the package or add the option. Never by copying.
- **A standalone tool is a nova-tools command.** Anything that does not need the sprint's own state is its own command in nova-tools, not a package here. Examples: `nova-tla`, `nova-roadmap`, `nova-work`.
- **Generated code is the compiler's.** The schema-generated wire packages (`*wire/`) are compiler output. Never hand-edit them or copy their runtime. Sharing that runtime is tracked as its own card.

## Copies still being removed

A cross-repository comparison on 2026-10-18 found these diverged copies of nova-tools code here. Each is being removed by moving its differences into nova-tools. Do not edit them meanwhile: change the nova-tools package instead.

- `pkg/tlc`, `pkg/safepath` and `tools/tlacheck`, moving to nova-tools `pkg/tlc`, `pkg/safepath` and the `nova-tla` command.
- `pkg/config`, `pkg/swarm` and `pkg/friend`, moving to the nova-tools packages of the same names.
- `cmd/nova-work`, `internal/workgh` and `internal/workfile`, moving to nova-tools.
- `internal/roadmap`, moving into `nova-roadmap` and nova-tools `pkg/roadmap`.
- The dashboard's page lock (`internal/sprintdash/pagelock*`), moving to a nova-tools command.

Remove an entry from this list in the same change that deletes its copy.
