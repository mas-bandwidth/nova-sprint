# Take your first lap

Try one card from start to finish before connecting a live team. This example
uses a **twin**: a local file that simulates the sprint's store. You play the
coordinator, worker, and reader in turn. No model runs, no Git branch changes,
and no Redis server is needed.

## Get the command

If `nova-sprint` is already installed, run `nova-sprint help`. To build from
this repository, use Go 1.26.6 or newer:

```sh
go install ./cmd/nova-sprint
```

Make sure your Go binary directory is on `PATH`. See
[Contributing](../CONTRIBUTING.md) for the source build and test commands.
The following example uses the command surface in this checkout; the
[v1.0.0 introduction](../README.md) describes the complete product.

## Give the example its own space

Run the following blocks in the same shell. The opening parenthesis starts a
subshell, so your usual sprint settings are restored when you close it at the
end. The temporary directory keeps this exercise away from an existing twin.

```sh
(
  cd "$(mktemp -d)" || exit 1
  unset NOVA_SPRINT_SERVER
  export NOVA_SPRINT_REDIS=mem:sprint.twin
  export NOVA_SPRINT_ACTOR=boss
```

## Add one task

Create a sprint with one member and two available reader identities, add a
single demonstration card, and start it:

```sh
  nova-sprint init --readers reader-a,reader-b --members m1
  nova-sprint add --stream s1 --count 1 --one
  nova-sprint start
  nova-sprint tick
  nova-sprint tick
```

A **tick** advances the simulation. The first brings the member up; the
second assigns the card. A live server ticks for you. The twin deliberately
runs only when you ask it to, one command at a time.

This demonstration card has no brief. Real workers need one: the task,
repository, allowed paths, checks, and finish condition. The command prints
a reminder about that when it adds the card.

## Play the worker

Take the assignment as `m1`, then report it done:

```sh
  nova-sprint take --as m1 --epoch 0
  nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --report done
  nova-sprint tick
```

`s1-1.w1@1` identifies this work attempt and assignment generation. The
**epoch** identifies the current sprint generation; both prevent an old
worker's report from being mistaken for current work. In a real sprint,
use the identifiers from the assignment packet and report the pushed commit.
Here, `finish` without a commit is just part of the simulation.

## Give it a review

Act as an independent reader, begin the review, and record an OK result:

```sh
  nova-sprint read --as reader-a --begin --epoch 0
  nova-sprint read --as reader-a --ok --epoch 0
  nova-sprint tick
```

This is a flash-tier card, so one passing read is enough for this exercise.
Having two available readers does not mean every card uses both. The
[review rules](SPEC-SPRINT.md#6-the-readers) explain how tier and eligibility
change the requirement.

## Record the landing

```sh
  nova-sprint merge --stream s1 --batch 1   # twin only, a real store refuses a bare merge, use land
  nova-sprint tick
  nova-sprint where
)
```

Look for the card in **landed**. `merge` records a simulated landing; the
last tick applies it. This has not changed a repository or tested any code.
The [executable transcript](TESTS.md#nova-sprint) shows the intermediate
output if you want to compare each step.

## Move on to a real team

A live sprint needs a store and one sprint server, configured workers and
readers, repository access, and model credentials for the routes you use.
Nova Tools supplies the shared runtime services. The
[coordinator's guide](SPRINT-COORDINATOR.md) explains how to connect to a live
sprint and the [credential guide](SPRINT-COORDINATOR-SEAT.md) covers local
configuration commands.

Give an AI coordinator one small change for the first real card. Read the
brief together, then let it follow the worker, review, and landing. Once that
lap works, give it several independent tasks and the boundaries for continuing
while you are away. Increase concurrency as those handoffs work well and your
machines have room.

For help deciding what belongs in each card, continue with
[Working with AI teams](WORKING-WITH-AI-TEAMS.md).
