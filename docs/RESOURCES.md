# Resources

The layer under everything else: what a machine has, and what work takes from it.

This page is a design. Nothing on it is built. The rules marked *settled* are
Glenn's, 2026-11-15; the section [Not settled](#not-settled) lists what is still
his to decide.

## The idea

Work requires resources, and can run anywhere those resources are provided.

Glenn: "The actual pattern is that cards require resources, and can run anywhere
those resources are provided. Be it CPU, memory, GPU, dev tools whatever..."

## What a resource is

A resource is a name and a whole number. *Settled.*

There is one kind of resource. CPU, memory, a GPU, a toolchain and an attached
device are all the same record: a name and an integer. Amounts are never
fractions.

| Resource | Unit | Example on a machine |
| --- | --- | --- |
| `cpu` | CPU units | `cpu 16` |
| `memory` | gigabytes | `memory 64` |
| a thing the machine has or does not have | 0 or 1 | `ps5 1` |

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

A thing with no count is a resource whose number is 0 or 1: the machine has it
or it does not. *Settled.*

Glenn: "for a thing that has no counts, just give it 0, eg. 0/1 -- have or not
have. boolean."

## Machines provide

Each machine provides a set of resources: for each name, a capacity. A resource
a machine does not list has capacity 0 there.

At any moment each resource on a machine also has an amount in use: the sum of
what the work running there has taken.

## Work requires

Each piece of work requires a set of resources: for each name, an amount.

Work fits on a machine when, for every resource it requires:

1. the machine provides it (capacity of at least 1), and
2. the amount in use plus the amount required is at most the capacity.

Work acquires everything it requires before it starts, holds it while it runs,
and releases all of it when it ends, however it ends.

A requirement of 0 means the work needs the resource to be there but takes none
of it. Many pieces of work can share it. A requirement of 1 against a capacity
of 1 means one at a time.

| Requirement | Meaning | Example |
| --- | --- | --- |
| `cpu 4` | takes 4 of the machine's CPU units | a build |
| `memory 8` | takes 8 GB | a build |
| `ps5 0` | needs the toolchain present, takes nothing | compiling for the device |
| `ps5 1` | takes the device itself | running on the one attached devkit |

## What takes resources

A consumer of a card (each work attempt and each read) and a GitHub runner's
job. Both state what they require in the same names and units, and both draw on
the same capacity of the machine they run on.

## What this replaces

Today a bench has a width: its core count, at most `fleet_width_cap`, and at
most its `nova-reserve` slots. Every working consumer takes one of the width,
whatever it does. A consumer can state a tier and routes and nothing else.

With resources there is no width. How many consumers a machine works at once
follows from what each one requires and what the machine has left.
`fleet_width_cap` has nothing to bound. A route a machine serves is a thing it
has.

## Not settled

These are Glenn's to decide before this layer is built.

0. **What one CPU unit is worth, and how a machine is scored.** The numbers on
   machines and on cards need a shared anchor, so that `cpu 4` means the same
   amount to everyone who writes it. A benchmark would define it: one CPU unit
   is a fixed score. Which benchmark, where it lives (nova-tools is the likely
   home), and whether a machine's number can also be set by hand are not
   chosen.
1. **Where a machine's resources are declared.** nova-config holds machines and
   their core counts today, so it is the likely home. That is a change in
   nova-tools, under this layer.
2. **Requirement 0.** The rule above (present, takes nothing) is Rowan's
   reading of "0/1". The other reading is that a thing is always taken.
3. **Work that states nothing.** What a consumer requires when its card names
   no resources.
4. **Friends.** A friend's width is how many sessions it runs at once, not a
   core count. It either stays a width or becomes a resource like any other.
5. **The machine-local reservation.** `nova-reserve` has one capacity per
   machine, in slots, shared with people and CI. It either learns named
   resources or gives way to the accounting here.
6. **From CPU units to cores.** A machine turns the CPU units a piece of work
   holds into a number of its own cores, which is also the work's parallel job
   count. The likely rule is the machine's cores times the units held, divided
   by the machine's CPU units, rounded up. It is not settled.
7. **Single-threaded work.** A CPU unit measures a machine's total. Work that
   cannot be split runs slower on weak cores however many units it holds.
