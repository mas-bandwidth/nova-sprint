# Your first lap

Check that the installed command runs and see its current usage:

```sh
nova-sprint version
nova-sprint help
```

The help output includes a no-Redis card-flow example using a local `mem:`
twin. That example is currently blocked at its first card: `nova-sprint add`
requires a live, proven sprint and bus hook, and refuses before admission when
no hook is connected. Do not use the twin example as a working first-card
exercise until that behavior is corrected.

For now, try a first card only in a configured sprint. Set
`NOVA_SPRINT_SERVER`, `NOVA_BUS_REDIS`, and `NOVA_SPRINT_ACTOR` in the
coordinator's interactive shell; make sure the server process also has
`NOVA_BUS_REDIS`. Connect and prove `nova-sprint hook`, then follow the
[coordinator's live start](SPRINT-COORDINATOR.md#a-first-live-start). Keep the
first task small and use a sandbox repository and sprint when you are learning
the workflow. The [environment reference](ENVIRONMENT.md) explains how the
server address, store address, and actor are selected.

To build the command from a checkout, open a shell in the repository root and
use Go 1.26.6 or newer:

```sh
go install ./cmd/nova-sprint
```

Make sure your Go binary directory is on `PATH`. See
[Contributing](../CONTRIBUTING.md) for source build and check commands.
