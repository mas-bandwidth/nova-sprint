# Take your first lap

Try one card from start to finish before connecting a live team. A **twin** is
a local file-backed simulation of the sprint store. It needs no Redis server,
model account, Git remote, or coordinator hook. You play the coordinator,
worker, and reader in turn. The twin has no process ticking in the background;
you advance it one step at a time.

If `nova-sprint` is installed, run the commands below in one shell. The
temporary directory gives the twin its own file. Each command names the twin
with `--redis`, which keeps it local even if this machine has a server recorded
in its seat configuration:

```sh
cd "$(mktemp -d)" || exit 1
unset NOVA_SPRINT_SERVER
export NOVA_SPRINT_REDIS=mem:sprint.twin
export NOVA_SPRINT_ACTOR=boss

nova-sprint init --redis mem:sprint.twin --readers m2 --members m1,m2
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

`take` prints a `PACKET` with the assigned card, attempt, generation, and
epoch. In this example that is `s1-1.w1`, generation `1`, epoch `0`; use the
printed values when you finish the work. The read assignment is `s1-1.r1.m2`;
`m2` takes it and records its result. The command omits an explicit push
commit and `merge` stands in for a landing, so this exercise does not change a
repository or test code. `where` should show the primary as landed.

Cards added with `--count` have no brief. Real work needs one that names the
task, repository, allowed paths, checks, and finish condition. The reminder
printed by `add` explains how to attach one before the card is dealt. For the
full transcript and the real Git-backed path, see the [executable transcript](TESTS.md#nova-sprint).

To build from this repository instead of installing a release, open a new
shell in the repository root and use Go 1.26.6 or newer:

```sh
go install ./cmd/nova-sprint
```

Make sure your Go binary directory is on `PATH`. See
[Contributing](../CONTRIBUTING.md) for source build and check commands. For a
live sprint, continue with [getting started](GETTING-STARTED.md) and the
[coordinator's guide](SPRINT-COORDINATOR.md).
