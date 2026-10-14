# nova-work

## What it is

nova-work: every issue of an organization's repositories in one tree file, verified field for field

## Why use it

Keep every GitHub issue of an organization in one file you can check. nova-work is pre-alpha: not ready for production use.

## Install

```sh
go install github.com/mas-bandwidth/nova-tools/cmd/nova-work@latest
nova-work version
```

## First run

The one first-run transcript, and the recorded-conversation notes that explain
it, live in [docs/TESTS.md](../../docs/TESTS.md#nova-work), which
`cmd/nova-work/firstrun_test.go` executes line for line; this README keeps no copy.

## Verbs

The [nova-work section of the command reference](../../docs/CLI.md#nova-work) documents every verb's flags, effect and exit codes.

- `import`
- `verify`
- `version`
- `help`

## Spec

The contract is [docs/SPEC-WORK-V1.md](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-WORK-V1.md).
