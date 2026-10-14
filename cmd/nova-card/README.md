# nova-card

## What it is

nova-card: writes a directory of pre-linted briefs from a ledger, a findings file or a tool's help

## Why use it

Turn a ledger, a findings file or a tool's help into briefs the sprint admits. nova-card is pre-alpha: not ready for production use.

## Install

```sh
go install github.com/mas-bandwidth/nova-tools/cmd/nova-card@latest
nova-card version
```

## First run

The one first-run transcript lives in [docs/TESTS.md](../../docs/TESTS.md#nova-card),
which the docs tests execute line for line; this README keeps no copy.

## Verbs

The [nova-card section of the command reference](../../docs/CLI.md#nova-card) documents every verb's flags, effect and exit codes.

- `generate`
- `lint`
- `template`
- `version`
- `help`

## Spec

The contract is [docs/SPEC-CARD-CONTRACT.md](../../docs/SPEC-CARD-CONTRACT.md).
