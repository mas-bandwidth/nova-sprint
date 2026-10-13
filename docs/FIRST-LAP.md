# Your first lap

Check that the installed command runs and see its current usage:

```sh
nova-sprint version
nova-sprint help
```

You can learn the command flow in a local twin without Redis or a sprint
server. Use a fresh directory, and choose a reader name that is separate from
the worker names. The explicit `--redis` flag keeps every command on the twin,
even if this machine has a saved seat or a server environment setting.

```sh
export NOVA_SPRINT_ACTOR=boss
unset NOVA_SPRINT_SERVER NOVA_BUS_REDIS
nova-sprint init --redis mem:sprint.twin --readers reader-a --members m1,m2
nova-sprint add --redis mem:sprint.twin --stream s1 --count 1 --one
nova-sprint start --redis mem:sprint.twin
nova-sprint tick --redis mem:sprint.twin
nova-sprint tick --redis mem:sprint.twin
nova-sprint take --redis mem:sprint.twin --as m1 --epoch 0
nova-sprint finish --redis mem:sprint.twin --as m1 s1-1.w1@1 --epoch 0 --report done
nova-sprint queue --redis mem:sprint.twin --as reader-a
nova-sprint tick --redis mem:sprint.twin
nova-sprint queue --redis mem:sprint.twin --as reader-a
nova-sprint read --redis mem:sprint.twin --as reader-a --ok s1-1.r1.reader-a --epoch 0
nova-sprint tick --redis mem:sprint.twin
nova-sprint merge --redis mem:sprint.twin --stream s1 --batch 1
nova-sprint tick --redis mem:sprint.twin
nova-sprint where --redis mem:sprint.twin
```

The first `queue --as reader-a` is the reader's heartbeat; the next tick asks
for the read. Use the read-card ID shown by the second queue if it differs
from this one. A twin has no background tick, so advance it after each handoff.
Commands without `--redis` may select a configured server from
`NOVA_SPRINT_SERVER` or the saved seat record. See the
[environment reference](ENVIRONMENT.md) for connection precedence.

For a live sprint, set `NOVA_SPRINT_SERVER`, `NOVA_BUS_REDIS`, and
`NOVA_SPRINT_ACTOR` in the coordinator's interactive shell; the server process
also needs `NOVA_BUS_REDIS`. Connect and prove `nova-sprint hook`, then follow
the [coordinator's live start](SPRINT-COORDINATOR.md#a-first-live-start). The
[environment reference](ENVIRONMENT.md) explains the connection choices.

To build the command from a checkout, open a shell in the repository root and
use Go 1.26.6 or newer:

```sh
go install ./cmd/nova-sprint
```

Make sure your Go binary directory is on `PATH`. See
[Contributing](../CONTRIBUTING.md) for source build and check commands.
