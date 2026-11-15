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

The number comes from a benchmark run on each machine
([Scoring a machine](#scoring-a-machine)). Glenn: "we might need to benchmark machines to work
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

## Tags

A tag is a name a machine has or does not have, such as `windows`, `mac` or
`linux`. *Settled.* Work names the tags it needs, and runs only on a machine
that has all of them.

Glenn: "i would like also tags like 'windows', 'mac', 'linux' etc.", "so we can
also say, we need this sort of os", "and each fleet machine registers with
these tags, again, it's just configuration".

A tag is not an amount and is never held. Any number of pieces of work can run
on a `linux` machine at once; the tag only says where work may go. A resource
says how much of something work takes while it is there.

Tags are not only for operating systems. `gpu` and `metal` are tags too
(Glenn: "we could also add tags like 'gpu' 'metal'"): work that renders with
Metal names `metal` and goes only to a machine that has it.

On the fleet (Glenn's example):

| Machine | Tags |
| --- | --- |
| captain | `gpu` |
| studio | `gpu`, `metal` |
| mini-m5 | `gpu`, `metal` |

Work that names `gpu` can go to any of the three. Work that names `gpu` and
`metal` can go to studio or mini-m5, and never to captain.

The same hardware can be a tag or a resource, and the choice says whether work
shares it. `gpu` as a tag means "this machine has a GPU", and everything sent
there may use it at once. `gpu 1` as a resource means the work holds the GPU
and nothing else gets it until the work ends.

Each machine's tags are configuration, kept in nova-config with its resources.

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

### How a card states it

A card describes what it needs as tags and numbers, and a number left out
means 1. *Settled.*

Glenn: "card describes the resources it needs via tags and numbers (a number
omitted assume 1)".

A card that needs four CPU units, eight gigabytes, a Mac with Metal and the
PS5 says:

    cpu 4, memory 8, mac, metal, ps5

`cpu 4` and `memory 8` are amounts. `ps5` has no number, so it is `ps5 1`.
`mac` and `metal` are tags. The card's writer does not have to know which
names are tags and which are resources: the fleet's configuration says what
each name is, and a name with no number reads correctly either way.

### Work across several machines

An idea, not settled. Glenn: "maybe a card could even grab resources across
mulitple machines for a friend to work on".

Some work needs more than one machine at once: a renderer checked on a Mac
with Metal and on a Windows machine with a GPU. A card could then state more
than one set of needs, each set held on one machine:

    mac, metal, cpu 4
    windows, gpu, cpu 4

The friend working the card holds both and works across them from its one
session.

A card that holds part of what it needs while it waits for the rest can block
another card doing the same, for ever. So the rule would be all or nothing: a
card holds every set it states, or none of them.

### Who says what work requires

The coordinator, by judgement, for each piece of work. *Settled.* There is no
default amount of CPU or memory.

Glenn: "it depends on the work. a read is cheaper yes, but coding might require
compiling and tests and so on. you really need to just use your judgement. in
short, 'what resources do you want for this work, as coordinator?'"

A read needs less than the work it reads. Work that compiles and runs tests
needs more than work that edits a page.

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

## On the machine: nova-reserve

nova-reserve is how a machine holds its resources. *Settled.* It is kept and
upgraded.

Glenn: "nova-reserve is superceded by this design, or implements it. it is a
preview of this design, basically. keep and upgrade nova-reserve".

Today nova-reserve already does the whole job for one unnamed resource. A
machine has a capacity in slots. A command run through it waits for a slot,
holds it while it runs, and frees it when it ends, however it ends. On Linux it
pins the command to that slot's share of the CPUs, so `nproc`, `make -j` and Go
size themselves to what was reserved. People, AIs and CI on the machine all go
through the same capacity.

The upgrade is from one number to named numbers. The machine's capacity becomes
its resources from nova-config. A command states what it requires by name, and
nova-reserve holds exactly that.

nova-reserve on the machine is the authority on what is held there. The sprint
decides where to send work from what each machine reports it has free. That
report can be a moment old, so work that arrives to find no room waits for it
there, as it does for a slot today.

## Scoring a machine

The anchor and the benchmark are Rowan's choice, at Glenn's ask ("you
decide."). His to overrule.

**The benchmark** is one small Go program in nova-tools, with no dependencies,
the same binary on every machine. It does a fixed mix of the kind of work a
compiler and a test run do: building and walking trees and maps, compressing,
hashing and sorting. It runs for a few seconds and counts how much it got done.

It runs twice:

1. **Every thread busy.** This is the machine's score. Hyperthreads and slow
   cores are in the measurement, so neither needs a rule of its own.
2. **One thread.** This is the speed of the machine's best core, and it is
   what decides `fast-core` (below).

It runs when a machine is set up and whenever its owner asks, holding the whole
machine through nova-reserve so nothing else is running while it measures.

**The anchor.** One CPU unit is a fixed score, written as a constant in the
benchmark. The constant is set once, so that one CPU unit is one core of hulk
with every core busy, and then it does not move. So a request reads as "this
many cores of our slow machine", every faster machine has more CPU units than
cores, and a machine that is retired takes nothing with it.

**A machine's CPU units** are its every-thread score divided by the constant,
rounded down to a whole number, at least 1. Rounding down means a machine never
claims compute it did not show.

The number is written to nova-config. An owner can also set it by hand, and
nova-config's history says which it was.

## From CPU units to cores

This rule is Rowan's, at Glenn's ask ("work out something good.
implementatino detail really."). His to overrule.

A machine knows two numbers about its CPU: its CPU units and its cores (the
CPUs the operating system shows, hyperthreads included). Work that asks for
some CPU units holds a whole number of the machine's cores:

    cores held = units asked x machine's cores / machine's CPU units, rounded up, at least 1

The machine counts in its own cores. Work fits when the cores already held plus
the cores it would hold are at most the cores the machine has. So a machine
never gives out more cores than it has, and work never gets less compute than
it asked for. On a machine with strong cores it can get a little more.

nova-reserve pins the work to the cores it holds, so the work's own tools see
that many and run that many jobs.

With made-up numbers: hulk has 16 cores and 16 CPU units, so `cpu 4` holds 4
cores there. A machine with 16 stronger cores and 48 CPU units holds 2 cores
for the same `cpu 4`.

## Work that cannot be split

This rule is Rowan's, at Glenn's ask ("think up something good."). His to
overrule.

More cores do not help work that runs on one thread. It needs one fast core.

The benchmark that scores a machine also times one thread on each kind of core
it has. A machine gets a resource, `fast-core`, whose amount is how many of its
cores clear a fixed speed. A machine with no such cores has none.

Work that cannot be split requires `cpu 1` and `fast-core 1`. It is sent only
to a machine with a fast core free, holds that core while it runs, and is
pinned to it. Nothing new is needed to do this: `fast-core` is a resource like
any other, counted and held the same way.

## What this replaces

Today a bench has a width: its core count, at most `fleet_width_cap`, and at
most its `nova-reserve` slots. Every working consumer takes one of the width,
whatever it does. A consumer can state a tier and routes and nothing else.

A machine's width goes away, replaced by its `cpu` resource. *Settled.* How
many consumers a machine works at once follows from what each one requires and
what the machine has left. `fleet_width_cap` has nothing to bound. A route a
machine serves is a tag it has.

Glenn: "machine width in fleet definitely goes away and is replaced by cpu
resource."

A friend's work is placed like any other work: by the card and its resources,
on whichever machine in the fleet has what it needs. *Settled as the aim.*

Glenn: "my ideal would be that friend work can be done across the fleet,
wherever it needs to do its work, based on the card and resources."

Glenn: "the goal is, friends can run work and grab resources to do that work,
as they see fit, across the entire fleet, even if they are kinda driving from
studio (doing the session there)".

So a friend is not tied to one machine, and it is the friend that grabs. Its
session stays where it is. When it has work to run, it asks the fleet for the
resources that work needs, gets a hold on a machine that has them, and runs
the work there over ssh. It may do this as often as its work calls for: a
build on one machine, a GPU test on another.

This needs one thing the machine-local nova-reserve does not have: a way to
ask the whole fleet. A friend, a person or a worker names tags and amounts,
and is given a machine and a hold on it.

A friend keeps its width for now. *Settled.* It is the friend's own limit: how
many pieces of work it takes on at once. It says nothing about where they run.

Glenn: "leave friend width the same for now".

## Not settled

These are Glenn's to decide before this layer is built.

1. **The speed that makes a core fast.** `fast-core` needs one line: the
   one-thread score a core must reach. It is set after the fleet has been
   measured, from the numbers, and not before.
2. **A card's reads, and a card that names no CPU.** A read is cheaper than the
   work it reads, so whether a card states its reads' needs apart from its
   work's is not chosen. Nor is what a card holds when it does not name `cpu`
   or `memory` at all. Rowan's reading of "a number omitted assume 1" is that
   it then holds 1 of each, so no card is refused for leaving them out.
3. **How a friend works on a machine it is sent to.** The aim is settled; the
   means is not designed. Friends today run in apps on studio and the macbook
   (Glenn: "this is tempered somewhat by the fact that right now, friends run
   on studio and macbook via apps."), and a friend works where its session
   runs. The fleet-wide ask (name tags and amounts, get a machine and a hold),
   how the hold is kept while the friend's command runs over ssh (Glenn: "eg.
   the friend ssh's there"), and how it is freed if the friend's session dies
   are not designed. Until they exist, a friend's work runs on the machine its
   app is on.
4. **When a runner holds its resources.** GitHub picks the runner for a job,
   not the fleet, so a runner cannot be moved to where there is room. Either a
   runner holds what it requires all the time, busy or idle, or it acquires
   when a job starts and the job waits if the machine is full. Holding all the
   time is simple and keeps CI fast; it also keeps those resources from cards
   while the runner is idle.
5. **Operating system and architecture as tags.** nova-config already holds
   each machine's operating system and architecture. Whether those become its
   tags automatically, or every tag is written by hand, is not chosen.
