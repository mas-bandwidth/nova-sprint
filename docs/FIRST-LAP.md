# Your first lap

Check that the installed command runs and see its current usage:

```sh
nova-sprint version
nova-sprint help
```

You can learn the command flow in a local twin without a separate Redis service
or sprint server. This sequence was run with the production CLI built from
v1.2.20 source revision `db09a68ac1a58474206999231d72aa12e7f35437` on Linux
amd64 with Go 1.26.6; it is not the test-binary transcript in
[TESTS.md](TESTS.md#nova-sprint). Check `nova-sprint version` if you already
have a binary; an older build may not support this flow. Use the paired
`reader-m2` and `m2` names: a fleet reader row is tied to its member. A twin
beats every declared member and reader at each command; it does not run between
commands, so you advance it by hand. The explicit `--redis` flag keeps every
command on the twin, even if this machine has a saved seat or a server
environment setting. Run this in a fresh working directory, or choose a new
twin filename.

```sh
export NOVA_SPRINT_ACTOR=boss
unset NOVA_SPRINT_SERVER NOVA_BUS_REDIS
nova-sprint init --redis mem:sprint.twin --readers reader-m2 --members m1,m2
nova-sprint add --redis mem:sprint.twin --stream s1 --count 1 --one
nova-sprint start --redis mem:sprint.twin
nova-sprint tick --redis mem:sprint.twin
nova-sprint tick --redis mem:sprint.twin
nova-sprint take --redis mem:sprint.twin --as m1 --epoch 0
nova-sprint finish --redis mem:sprint.twin --as m1 s1-1.w1@1 --epoch 0 --report done
nova-sprint tick --redis mem:sprint.twin
nova-sprint take --redis mem:sprint.twin --as m2 --epoch 0
nova-sprint read --redis mem:sprint.twin --as m2 --ok s1-1.r1.m2@1 --epoch 0
nova-sprint tick --redis mem:sprint.twin
nova-sprint merge --redis mem:sprint.twin --stream s1 --batch 1
nova-sprint tick --redis mem:sprint.twin
nova-sprint where --redis mem:sprint.twin
```

On a live sprint, the reader process beats with `queue --as <reader>` before
the server tick asks for a review. A twin handles those beats itself. Commands
without `--redis` may select a configured server from `NOVA_SPRINT_SERVER` or
the saved seat record. See the [environment reference](ENVIRONMENT.md) for
connection precedence.

This walkthrough shows the command flow, not a code change: the sample card has
no brief, so the worker has no task to implement. For actual work, provide a
brief with `add --brief-file <path>` before starting the sprint. The twin also
has no git checkout: `finish` omits `--head` and `merge` records a simulated
landing. A live landing needs a pushed commit and the `land` verb.

For a live sprint, set `NOVA_SPRINT_SERVER`, `NOVA_BUS_REDIS`, and
`NOVA_SPRINT_ACTOR` in the coordinator's interactive shell; the server process
also needs `NOVA_BUS_REDIS`. Hook in with `nova-hook hook --as <you> --role
coordinator` and answer its challenge with `nova-hook prove`, then follow
the [coordinator's live start](SPRINT-COORDINATOR.md#a-first-live-start). The
[environment reference](ENVIRONMENT.md) explains the connection choices.

To build the command from a checkout, open a shell in the repository root and
use Go 1.26.6 or newer:

```sh
go install ./cmd/nova-sprint
```

Make sure your Go binary directory is on `PATH`. See
[Contributing](../CONTRIBUTING.md) for source build and check commands.
