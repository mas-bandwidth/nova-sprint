# Resources

The layer under everything else: what a machine has, and what work takes from it.

This page is a design. Nothing on it is built. The rules marked *settled* are
Glenn's, 2026-11-15; the section [Not settled](#not-settled) lists what is still
his to decide.

## The idea

Work requires resources, and can run anywhere those resources are provided.

Glenn: "The actual pattern is that cards require resources, and can run anywhere
those resources are provided. Be it CPU, memory, GPU, dev tools whatever..."

The purpose is to spread work across the fleet better than a count of cards per
machine can. Glenn: "basically, we want to distribute work better."

## What a resource is

A resource is a name and a whole number. *Settled.*

There is one kind of resource. CPU, memory, a GPU and an attached device are
all the same record: a name and an integer amount. Amounts are never fractions.

| Resource | Unit | Example on a machine |
| --- | --- | --- |
| `cpu` | CPU units | `cpu 16` |
| `memory` | gigabytes | `memory 64` |
| a thing the machine has or does not have | amount 1 | `ps5 1` |

### CPU units

CPU is measured in CPU units, not cores. *Settled.*

Glenn: "i like cpu units", "not cores."

A CPU unit is an amount of compute. It is not a core, because cores differ in
power from machine to machine. A machine has one CPU number: how much compute
it has, in CPU units. A stronger machine has a higher number than a weaker one
with the same core count. Fitting work onto machines uses that number and
nothing else.

The number is approximate, and that is accepted. Glenn: "this will all be
approximate, but better than what we have which is weirdly non-scientific
around guessing the # of cards per-machine".

Where the number comes from is likely a benchmark, run on each machine, that
gives a CPU score per core. Glenn: "we might need to benchmark machines to work
out their CPU score per-core, and also consider hyperthreading." Two
hyperthreads on one core are not two cores, so the benchmark measures the whole
machine with every thread busy and the machine's number is what it measured,
not a thread count times a single-thread score.

The unit is coarse. Work asks for a small whole number, and there is nothing
smaller than one unit.

Glenn: "the idea is to keep it simple and not have it super fine grained."

Work says how much compute it needs, and the machine it lands on works out how
many of its own cores that is. The same request is more cores on a slow machine
than on a fast one.

Glenn's example: "we could reserve say, 4 cores for a CI runner doing golang
tests that must finish in under 2m on a slower machine like hulk". The runner's
job states its CPU units once. On hulk that reserves 4 cores; on a faster
machine it reserves fewer, and the tests still finish in time.

### Memory

Memory is in whole gigabytes. *Settled.*

### Things

A thing is a resource with amount 1. It cannot be split: work has it or does
not. *Settled.*

Glenn: "the resource would be defined as having amount=1, so you can't split it
up, you have it or not."

Work that needs a PS5 requires `ps5 1` and holds the whole of it while it runs.
Work that does not need one does not name it at all.

## Machines provide

Each machine provides a set of resources: for each name, an amount. A machine
that does not list a resource does not have it.

The fleet's resources are kept in nova-config, beside its machines. *Settled.*

The link runs both ways. *Settled.* From a resource, the fleet can be asked
which machines have it. From a machine, it can be asked which resources it has.

Glenn: "resources point to machines that have them in fleet. machines can also
walk to the resources tehy have. bidirectional."

At any moment each resource on a machine also has an amount in use: the sum of
what the work running there holds.

## A uniform fleet

The default set of tools is installed on every machine with ansible, so the
fleet is as uniform as it can be made. *Settled.*

Glenn: "the default set of stuff done with ansible. we try to push the fleet to
be as uniform as we can, for flexibility."

So a resource is not a list of what is installed. Work can assume the default
set anywhere. A resource names what is counted (CPU, memory) or what only some
machines have (a GPU, a console and its toolchain).

## Work requires

Each piece of work requires a set of resources: for each name, an amount of at
least 1.

Work fits on a machine when, for every resource it requires, the amount in use
plus the amount required is at most what the machine provides.

Work acquires everything it requires before it starts, holds it while it runs,
and releases all of it when it ends, however it ends.

| Requirement | Meaning |
| --- | --- |
| `cpu 4` | holds 4 of the machine's CPU units |
| `memory 8` | holds 8 GB |
| `ps5 1` | holds the machine's PS5 |

## What takes resources

A consumer of a card (each work attempt and each read), a GitHub runner's job,
and a friend's work. All state what they require in the same names and units,
and all draw on the same amounts of the machine they run on.

## CI runners

A CI runner is a thing in the fleet that has tags and takes resources, like
any other work. *Settled.*

Glenn: "previously, they were sort of an invisible resource. now they should
become a thing that has tags, and grabs resources too".

Today nova-config gives a machine a count of runners and their labels, and what
a runner's jobs use is counted nowhere. Card work only stays out of CI's way by
running at a lower priority.

With resources, a runner states what it requires in the same names and units
as a card, and what it holds comes out of the same amounts on its machine. Its
tags are the labels a workflow selects it by.

## What this replaces

Today a bench has a width: its core count, at most `fleet_width_cap`, and at
most its `nova-reserve` slots. Every working consumer takes one of the width,
whatever it does. A consumer can state a tier and routes and nothing else.

A machine's width goes away, replaced by its `cpu` resource. *Settled.* How
many consumers a machine works at once follows from what each one requires and
what the machine has left. `fleet_width_cap` has nothing to bound. A route a
machine serves is a thing it has.

Glenn: "machine width in fleet definitely goes away and is replaced by cpu
resource."

A friend keeps its width for now. *Settled.* A friend's work still acquires
resources to run.

Glenn: "leave friend width the same for now, but yes, friends work will acquire
resources to do work, and maybe, they have a tag [codex] that ties to
particular machine or whatever."

## Not settled

These are Glenn's to decide before this layer is built.

1. **What one CPU unit is worth, and how a machine is scored.** The numbers on
   machines and on cards need a shared anchor, so that `cpu 4` means the same
   amount to everyone who writes it. A benchmark would define it: one CPU unit
   is a fixed score. Which benchmark, where it lives (nova-tools is the likely
   home), and whether a machine's number can also be set by hand are not
   chosen.
2. **From CPU units to cores.** A machine turns the CPU units a piece of work
   holds into a number of its own cores, which is also the work's parallel job
   count. The likely rule is the machine's cores times the units held, divided
   by the machine's CPU units, rounded up.
3. **CPU and memory for work that names none.** The default tools need no
   naming, but every piece of work uses some CPU and memory. What a consumer
   holds when its card states no `cpu` or `memory` is not chosen.
4. **Where a friend's work acquires.** Two ways are on the table. In one, a
   friend carries a tag (such as `codex`) that ties its work to a particular
   machine. In the other, resources place a friend's work exactly as they
   place a bench's: the work acquires on whichever machine has what it needs,
   and the friend reaches that machine over ssh. Glenn: "or we can use
   resources fully to distribute friend work across the machine, eg. the
   friend ssh's there". The second needs no friend width at all.
5. **When a runner holds its resources.** GitHub picks the runner for a job,
   not the fleet, so a runner cannot be moved to where there is room. Either a
   runner holds what it requires all the time, busy or idle, or it acquires
   when a job starts and the job waits if the machine is full. Holding all the
   time is simple and keeps CI fast; it also keeps those resources from cards
   while the runner is idle.
6. **The machine-local reservation.** `nova-reserve` has one capacity per
   machine, in slots, shared with people and CI. It either learns named
   resources or gives way to the accounting here.
7. **A thing several can use at once.** A thing has amount 1, so one piece of
   work holds it at a time. If some thing outside the default set can serve
   several at once, whether it is given a higher amount is not decided.
8. **Single-threaded work.** A CPU unit measures a machine's total. Work that
   cannot be split runs slower on weak cores however many units it holds.
