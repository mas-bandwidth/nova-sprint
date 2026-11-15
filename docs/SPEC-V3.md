# nova-sprint v3.0.0: resources

A specification. Draft for review. Nothing in it is built.

v3 is experimental. v2 is still the version in use, and it keeps being fixed
and worked in. This specification changes one thing about v2 and leaves the
rest alone, landing included.

**To a reviewer.** Read the whole page before replying. Reply with findings as
section, then finding. The most useful findings are: a rule that contradicts
another, a rule you would have to guess at to build, a claim about v2 that is
wrong, and a case the rules do not cover. [Open questions](#11-open-questions)
lists what the authors already know is undecided; answers to those are welcome
too.

Each rule is numbered and marked with who decided it: **[G]** Glenn, on
2026-11-15, or **[R]** Rowan, a choice Glenn left to Rowan and can overrule.

## 1. The change

In v2 a bench machine has a **width**: how many consumer cards it works at
once. Every consumer costs one of the width, whatever it does. CI jobs on the
same machine are not counted at all.

In v3 a machine has **tags** and **resources**, a consumer card **holds** the
resources its work needs, and CI jobs hold resources too. How many consumers a
machine works follows from what each one needs and what the machine has.

Glenn: "The actual pattern is that cards require resources, and can run
anywhere those resources are provided. Be it CPU, memory, GPU, dev tools
whatever..."

The first thing this is for is sending work to the right machine: work that
needs a GPU, Metal, Windows or a device goes only where that exists. The
second is that CI and cards share one count, so one can no longer starve the
other unseen.

## 2. Why

**Width is a guess.** A bench's width is its core count, capped by
`fleet_width_cap` (8 by default), and capped again by its nova-reserve slots.
The default policy then works at most 4 consumers on a bench. None of these
numbers is measured, and none knows what a consumer does: a read that waits on
a model costs the same as a build that uses every core.

**CI is not counted.** A runner's jobs take no reservation, and the dealer
cannot see them. The one rule between CI and cards is that card work runs at
a lower operating-system priority than CI.

**That rule was not enough on 2026-11-15,** as measured in that day's sprint:
hulk ran at a load of 70 with four or five CI runners and card work sharing
it, and CI shards there were cancelled at the two-minute limit for an hour.
Width said nothing about it.

Glenn: "this will all be approximate, but better than what we have which is
weirdly non-scientific around guessing the # of cards per-machine".

## 3. Words

| Word | Meaning |
| --- | --- |
| machine | A computer in the fleet, a row in nova-config. |
| bench | A machine that takes sprint work. It has a row in the sprint. |
| open bench | A bench that is up, not held, and hooked in, as in v2. |
| tag | A name a machine has or does not have. Never used up. |
| resource | A name and a whole number on a machine. Work holds amounts of it. |
| CPU unit | The unit of the `cpu` resource: one standard core ([7](#7-cpu-units)). |
| usable core | A CPU nova-reserve can use on a machine: one the operating system gives it and the kernel has not isolated. |
| work | Anything that runs on a machine: a step of a consumer card's run, a CI job, a command. |
| needs | What a piece of work must hold: the list it states, plus the defaults of [R10](#5-what-work-needs). |
| hold | The resources one piece of work has on one machine, from start to end. |
| waiter | Work that has asked for a hold and not yet been given it. A waiter holds nothing. |
| paused | The state of a hold whose CPU a CI job has taken ([8](#8-ci-wins)). |
| stopped | Ended by force. Stopped work starts over if it runs again. |
| producer card | The unit of work in a stream. Unchanged from v2. |
| consumer card | One go at a producer's work: a work attempt or a read. |
| dealer | The part of the sprint that hands consumer cards out. |
| tick | One pass of the dealer, as in v2. |
| coordinator | The AI or person who adds cards and runs the sprint. |
| friend | An AI that is dealt cards. A row of kind kin in v2. |
| CI job | One job of a GitHub Actions runner on a fleet machine. |

## 4. Tags and resources

**R1 [G].** A tag is a name a machine has or does not have: `linux`, `mac`,
`windows`, `gpu`, `metal`. A tag is never held. Any amount of work can run on
a machine that has it.

**R2 [G].** A resource is a name and a whole number on a machine: `cpu 16`,
`memory 64`, `ps5 1`. Amounts are whole numbers. There are no fractions.

**R3 [G].** There is one kind of resource. CPU, memory and a device are the
same record. A thing a machine has one of is a resource with amount 1: work
holds all of it or does not name it.

**R4 [G].** `memory` is in gigabytes. `cpu` is in CPU units.

**R5 [R].** Within a fleet a name is a tag or a resource, the same on every
machine. `gpu` as a tag means everything on the machine may use its GPU at
once. `gpu 1` as a resource means one piece of work holds it. A fleet picks
one.

**R6 [G].** A machine's tags and resources are kept in nova-config with the
machine. The link runs both ways: from a machine to what it has, and from a
name to the machines that have it.

**R7 [G].** The default set of tools is installed on every machine, so the
fleet is as uniform as it can be made. Tags and resources name what is
counted (CPU, memory) and what only some machines have. They are not a list
of what is installed.

**R8 [G].** Nobody sets a machine's `cpu` or `memory` by hand. They are
measured ([7](#7-cpu-units)). Tags are written by the machine's owner.

Example. captain has `gpu`. studio and mini-m5 have `gpu` and `metal`. Work
that names `gpu` can go to any of the three. Work that names `gpu` and `metal`
can go to studio or mini-m5, and never to captain.

## 5. What work needs

**R9 [G].** Work states what it needs as one list of names, some with
numbers:

    cpu 4, memory 8, mac, metal, ps5

A name with a number is an amount of a resource. A resource with no number
means 1. A tag has no number. The writer does not have to know which names
are tags and which are resources.

**R10 [R].** Work that names no `cpu` needs `cpu 1`. Work that names no
`memory` needs `memory 1`. Everything that runs uses some of both. These
defaults are part of the work's needs everywhere a rule says "needs".

**R11 [G].** Work **fits** on a machine when the machine has every tag the
work needs, and for every resource the work needs, the amount other holds
count for plus the amount needed is at most the machine's amount. A hold
counts for all of its resources, except that a paused hold counts for no CPU
([R34](#8-ci-wins)). This one rule covers every resource.

**R12 [R].** Work that needs a tag or resource the machine does not have, or
more of a resource than the machine has in all, can never fit there. It is
refused at once. It does not wait.

## 6. Holds

**R13 [G].** Work acquires everything it needs before it starts, holds it
while it runs, and releases all of it when it ends, however it ends.

**R14 [G].** nova-reserve is how a machine holds its resources. It is kept and
upgraded from one number (slots) to named numbers. It still works on one
machine with no fleet and no sprint.

**R15 [R].** A hold belongs to one command running on the machine, as a slot
does today. When the command ends, for any reason, the operating system frees
the hold. There are no tokens and no leases. (A CI job is not one command:
[Q1](#11-open-questions).)

**R16 [R].** nova-reserve on a machine is the one authority on what is held
there. Two askers can never hold the same resources, whoever sent them.

**R17 [R].** A hold steers and does not police. It tells the work how many
cores to use ([R28](#7-cpu-units)). Nothing stops work from using more CPU or
memory than it holds. [R40](#9-the-check) is how that is noticed.

**R18 [R].** Work that fits a machine but finds no room now waits in one line
per machine. Waiters are served strictly in arrival order: a later, smaller
request does not pass an earlier one, so a large request at the head holds
back smaller ones behind it. A waiter that gives up leaves the line. Two
things do not use the line: a CI job ([R33](#8-ci-wins)) and unpausing
([R36](#8-ci-wins)). Sending important work first is the sender's job.

**R19 [R].** A hold carries a label and a priority. The label says what kind
of work it is: for a consumer card, v2's types (blocker, critical, fix, read,
work); for a CI job, `ci`; otherwise whatever its asker says. The priority is
one of v2's levels (low, normal, high, critical, blocker). The sprint sets it
from the card. A hold whose asker sets none is `normal`.

**R20 [R].** Asking for the whole machine stays as it is in nova-reserve
today: it holds everything the machine has.

**R21 [G].** nova-tools has no view of resources. It answers questions: what
tags and resources there are and on which machines, what is held now and by
what, and it gives and releases holds. The picture is the sprint's board
([10.6](#106-the-board)).

## 7. CPU units

**R22 [G].** CPU is measured in CPU units, not cores, because cores differ in
power from machine to machine.

**R23 [R].** A CPU unit has one job: to let work say how much compute it
needs, once, in a way that means the same on every machine. Then work can be
sent to any machine with enough free, the machine can turn the request into
its own cores, and what a machine hands out can be added up. It is not a
promise of speed and not a precise measurement.

**R24 [R].** One CPU unit is one standard core: about one core of an ordinary
machine. `cpu 4` reads as "about four cores". A machine with fast cores has
more CPU units than cores. A machine with slow cores or hyperthreads has
fewer.

**R25 [G].** The unit is coarse. Work asks for a small whole number, and there
is nothing smaller than one unit.

**R26 [R].** nova-tools measures a machine's CPU units with a benchmark built
into the tools: a small fixed mix of the work a compiler and a test run do,
for a few seconds, with every usable core busy. Hyperthreads and slow cores
are inside the measurement and need no rule of their own. The standard core
is a constant in the benchmark, the same in every copy of the tools, so one
CPU unit is the same amount on every fleet. A machine's CPU units are its
score divided by the constant, rounded down, at least 1.

**R27 [R].** A machine is measured once, when it joins a fleet, or the first
time nova-reserve is used on a machine with no fleet. The benchmark holds the
whole machine while it runs ([R20](#6-holds)), so nothing else is running
during the measurement. The number is kept, and measured again only when
asked. Memory is read from the machine at the same time.

**R28 [R].** CPU is counted in CPU units ([R11](#5-what-work-needs)). Cores
are not counted. They are only what work is told to use:

    cores to use = units held x usable cores / machine's CPU units, rounded down, at least 1

Where nova-reserve can pin (Linux), it pins the work to that many cores.
Which cores is nova-reserve's choice, and two holds may be given the same
core. Elsewhere the number is only what the work's tools are told.

With made-up numbers: a machine with 16 ordinary cores measures 16 CPU units,
and `cpu 4` is told to use 4 cores. A machine with 16 stronger cores measures
48 CPU units; there `cpu 4` is told to use 1 core and `cpu 12` to use 4.

**R29 [R].** The conversion is approximate, and that is accepted. On a fast
machine small pieces of work share cores: `cpu 1` on that stronger machine is
a third of a core, and three of them may be told to use the same core. On a
slow machine work is told to use slightly fewer cores than its units are
worth. What work is told to use is within one core of what it asked for.

**R30 [R].** Work that cannot be split across cores is not treated specially
in v3.0.0. It asks for `cpu 1` and is told to use one core, wherever it
lands. [Q7](#11-open-questions) is about sending it to a fast core.

## 8. CI wins

**R31 [G].** A CI runner has tags (the labels a workflow selects it by) and
takes a hold for each job, like any other work.

**R32 [G].** A runner holds only while it runs a job. An idle runner holds
nothing, so no CPU is wasted while CI is not running.

**R33 [G].** CI wins over cards. A CI job never waits for other work. It does
not use the line ([R18](#6-holds)). It takes its hold at once, even when
other work fills the machine.

**R34 [G].** When a CI job takes CPU that other work holds, enough other
holds on that machine yield to it and are **paused**: lowest priority first,
and among equals the youngest first. A paused hold counts for no CPU. It
keeps its other resources. It is not killed and does not start over.

**R35 [G].** A machine is never counted over what it has. For CPU, what CI
holds and what the holds still running hold come to at most the machine's CPU
units.

**R36 [G].** When a CI job ends, paused holds come back first. While any hold
on a machine is paused, no waiter and no new work is given a hold there. Each
paused hold is unpaused as soon as it fits, highest priority first. This can
leave CPU unused for a moment, while a paused hold waits for another CI job
to end; that is accepted, because a CI job is short.

**R37 [G].** By default CI can take all of a machine. A most that CI may hold
on a machine is an idea, not part of this specification.

**R38 [R].** A hold is in one of two states, held or paused, and its size
never changes.

**R39 [R].** Pausing frees CPU, not memory. If a CI job's memory does not fit
([R11](#5-what-work-needs)), other work on the machine is stopped, in the
order of [R34](#8-ci-wins), until it does. A consumer stopped this way is
dealt again later with v2's `fault` result, which blames nobody.

**R39a [R].** CI does not pause or stop CI. A machine's runners are sized so
that all of them running a job at once fit in the machine: runners times one
job's needs is at most the machine's amount, for every resource. So a CI job
never has to wait for another CI job either. A machine set up otherwise is a
configuration error, found when the runners are set up.

**R39b [R].** Time a consumer spends paused does not count toward the
deadline at which v2 withdraws a working consumer.

Glenn: "The design I want does not waste CPU while we are not running CI, so
it does work, but then prioritizes CI ahead of work, when we do.", "the card
yields to CI", "the CPU usage of CI and cards sums to 100% always on that
machine.", "unpausing cards are done, before new cards are dealt."

## 9. The check

**R40 [G].** Each machine still measures its real CPU use, as a bench's
heartbeat does in v2. The measurement is kept for checking and is not drawn
on the board.

What is held is a plan; what the machine measures is what happened. A machine
far busier than its holds has something running without a hold. Work that
uses far more or less than it held was given the wrong numbers.

## 10. What changes in nova-sprint

Each part says what v2 does today and what v3 does. Tier, routes and a
friend's width do not change **[G]**. Producer cards do not change at all
**[G]**. Streams, needs between cards, reads, review and landing do not
change.

### 10.1 The card

Today a brief's first line is its tier, and a card names a route it needs with
a `NEEDS-ROUTE:` line in the block before the first blank line.

**R41 [R].** A card states what its work needs with one line in that same
block:

    RESOURCES: cpu 4, memory 8, mac, metal

The list is as in [R9](#5-what-work-needs). A card with no `RESOURCES:` line
states nothing, so its work's needs are the defaults: `cpu 1` and `memory 1`.

**R42 [G].** The coordinator decides the list for each card, by judgement.
There is no table of defaults. A read is cheaper than the work it reads; work
that compiles and runs tests needs more than work that edits a page.

**R43 [R].** The line is part of the brief, as tier and routes are. The
producer card's record does not change. Every consumer cut from the card has
the same needs ([Q4](#11-open-questions) asks about reads).

**R44 [R].** `add` refuses a card whose list is malformed. It also refuses a
card whose needs no bench in nova-config can ever fit ([R12](#5-what-work-needs)),
as v2 refuses a card whose tier and routes no row serves.

### 10.2 The roster

Today a bench is a row whose width is its cores, capped by `fleet_width_cap`,
and capped again by the nova-reserve slots it reports.

**R45 [G].** A bench has no width. Its row carries its tags and resources
from nova-config.

**R46 [G].** `fleet_width_cap` is removed. It has nothing to bound.

**R47 [G].** A friend keeps its width: how many consumers it takes on at
once. It says nothing about where their work runs.

### 10.3 The heartbeat

Today a bench's heartbeat carries its load, its CPU peak, its cores, its
nova-reserve slots used and in all, and the tiers it serves.

**R48 [R].** A bench's heartbeat carries, for each resource it has: the
amount in all, the amount this sprint's consumers hold there, and the amount
everything else holds there (CI, friends' work, people). It names which of
its consumers are paused. It still carries the load, the CPU peak and the
tiers. It no longer carries cores or slots.

### 10.4 The deal

Today the dealer deals a consumer to a row while the row's live consumers,
plus this tick's deals, are under its width. The store has not checked width
since v2's round 22; the dealer is the only check.

**R49 [G].** The dealer decides where work it places on benches runs.

**R50 [R].** The dealer deals a consumer to a bench only when the bench has
every tag the card needs, and for every resource the card needs:

    amount in all - used >= the card's need

    used = held by everything else
         + the larger of: the needs of consumers dealt there and not finished
                          (this tick's deals included),
                          and what this sprint's consumers hold there

A consumer that has been dealt but has not started counts through its needs.
A consumer the sprint has withdrawn but whose command has not yet ended
counts through what it still holds. Two deals in one tick cannot take the
same room. While a consumer is paused, its CPU is counted once in its needs
and again in the CI job that displaced it; that over-count is harmless,
because of [R52](#104-the-deal).

**R51 [R].** What the heartbeat says is a moment old. The hold itself is
taken on the bench, through nova-reserve, when the work starts. In v2 a
consumer that finds no slot within a few seconds is staged and asks again on
a later pass. In v3 it asks once and keeps its place in the bench's line
([R18](#6-holds)) until it is given room or the sprint withdraws it.

**R51a [R].** A consumer's run is several commands in v2 (the harness, then
the seal, the apply and each gate), and each takes its own nova-reserve slot.
That stays: each step takes its own hold, with the card's needs. The dealer's
count ([R50](#104-the-deal)) keeps the consumer's room for its whole run, so
a later step normally finds it ([Q13](#11-open-questions)).

**R52 [G].** The dealer deals nothing new to a bench while any consumer there
is paused ([R36](#8-ci-wins)).

**R53 [R].** Deal order is unchanged: v2's ladder (blocker, critical, read,
high, normal, low). Preemption stays as v2 has it, for blockers only. Its
test "no free width on an open serving row" becomes "no open bench that has
the card's tags has its needs free", and it withdraws victims in v2's order
until the blocker's needs fit ([Q9](#11-open-questions)).

**R54 [R].** v2's idle alarm measures working consumers over open width,
across benches and friends. For friends that stays. For benches it becomes
CPU units held over CPU units in all ([Q14](#11-open-questions)).

### 10.5 Friends

**R55 [G].** A friend decides where the work it is dealt runs. Where the
friend's session runs does not matter; what matters is the machine it chooses
to do the work on.

**R56 [G].** A friend's work holds resources like any other. It runs each
command on the chosen machine through nova-reserve, over ssh, with the card's
needs. A friend that runs several commands at once for one card divides the
card's needs among them; that is the friend's care, and nothing enforces it.

**R57 [R].** The dealer and a friend can choose the same machine at the same
moment. nova-reserve there gives the room to one and the other waits
([R16](#6-holds)). A friend's work is counted only while a command of it is
running, so between a friend's commands the dealer may fill the bench, and
the friend's next command waits in line.

**R58 [R].** A friend is dealt cards as in v2: by tier, routes and its width.
It chooses a machine that has the card's tags, from nova-config. Until it can
ask the fleet where there is room ([Q2](#11-open-questions)), it chooses
without knowing what is free, and its command waits in that machine's line.

### 10.6 The board

Today the board has a Fleet table and a Friends table drawn by one renderer,
with the columns machine or friend, status, ready, working (one cell per unit
of width), working over width, done, ok% and load.

**R59 [G].** The Fleet section becomes Resources. Its rows are still the
machines. Where a row shows its working cells and width, it shows its CPU,
its memory and its tags.

**R60 [G].** It answers one question: are we using the fleet's resources
fully, yes or no. It shows everything that holds, CI included.

**R61 [G].** A machine's bars are split by type of work, in the colors the
board already uses for working cells: blocker, critical, fix, read and work.
Who is doing the work is not shown.

**R62 [G].** Paused work is shown on the machine's bar, behind the CI that
displaced it. Glenn: "this is why we visualize resourcecs and what tehy are
doing", "per-machine -- NOT cards."

**R63 [G].** The load column goes, from both tables.

**R64 [G].** The Friends section is otherwise unchanged. It still draws each
friend's consumer cards against its width. What those cards hold shows in
Resources.

### 10.7 Policy

Today policy answers `width(row)` for every row with how many consumers the
row works, at most its width, and `limits()` carries `fleet_width_cap`.

**R65 [R].** `width(row)` is asked only for friends. For a bench there is no
width to answer. `limits()` no longer carries `fleet_width_cap`. The rows a
policy sees carry tags and resources where they carried width.

This is why v3 is a major version: a policy written for v2 does not load
unchanged.

### 10.8 Removed

- A bench's width, and every count against it: the roster's width, the `check`
  verb's width line, the working-over-width column.
- `fleet_width_cap`.
- The heartbeat's cores and slots.
- The board's load column.

## 11. Open questions

**Q1. A CI job's hold.** A job is not one command, so [R15](#6-holds) does not
cover it. Runners already run a hook before each job and one after, and the
two-minute timer starts when the first hook ends. The hold could be taken in
the first hook and released in the second. What holds it in between, and what
frees it if the runner dies, is not designed. Who states a job's needs is not
chosen. The fleet sizes a runner at 4 cores today, so `cpu 4` is the natural
default for CPU. A job must state its memory too: with the default of
`memory 1`, [R39](#8-ci-wins) would never act.

**Q2. Asking the whole fleet.** A friend or a person needs to ask which
machines have given tags and amounts free. Which tool answers, from what, and
how fresh the answer must be, is not designed. The dealer does not need this:
it has the heartbeats.

**Q3. What paused does to the process.** [R34](#8-ci-wins) settles the count.
The process could be really stopped until the CI job ends, or left running at
a low priority. Card work already runs at a lower priority than CI in v2, and
on 2026-11-15 that did not keep CI inside two minutes on hulk
([2](#2-why)), which argues for really stopping it. A stopped process cannot
answer a polite request to end until it is continued.

**Q4. Reads.** A read is cheaper than the work it reads. Does a card state
its reads' needs apart from its work's?

**Q5. Devices.** A v2 route already names a device a machine serves, and
routes do not change. A device could also be a resource with amount 1. There
should be one way to say it.

**Q6. Windows.** nova-reserve has no Windows build, and the fleet has Windows
runners and a Windows bench.

**Q7. Work that cannot be split.** More cores do not help it; it needs one
fast core ([R30](#7-cpu-units)). A sketch: the benchmark also times one
thread, and a machine whose cores clear a fixed speed gets a resource
`fast-core`, counted and held like any other. Not designed: how many of a
machine's cores are fast when it has more than one kind, and how a hold of
`fast-core 1` is tied to an actual fast core.

**Q8. Operating system and architecture as tags.** nova-config already holds
both for each machine. Do they become tags automatically?

**Q9. Preemption.** v2 preempts for a blocker by withdrawing one working
consumer, which starts over later. Under v3 a blocker that finds no room
could instead pause lower priority work, as CI does, so nothing starts over.
[R53](#104-the-deal) keeps v2's rule until this is decided.

**Q10. The board's ready, done and ok% columns on a machine's row.** They
count the cards each machine worked. Do they stay?

**Q11. A color for CI** on the board. It is a type of work the board has no
color for.

**Q12. Does CI win over everything?** [R33](#8-ci-wins) says cards. A person
holding the whole machine for a measurement is the hard case, and so is work
waiting to hold the whole machine, which in nova-reserve today holds back new
takers while it waits.

**Q13. One hold for a consumer's whole run.** [R51a](#104-the-deal) keeps
v2's hold per step. Between steps the consumer holds nothing on the machine,
so a CI job or a friend's command can take the room, and the next step waits.
One command that holds across the whole run would close that gap, and is a
larger change to the worker.

**Q14. One figure for the idle alarm.** v2 has one figure across benches and
friends. [R54](#104-the-deal) gives two measures. How they join is not
chosen.

## 12. What has to hold

The state machines here get TLA+ models before code, and the code is bound to
the models by trace checks, as in v2. These are the properties the models
must check.

1. **Never over.** On every machine, for every resource, the amounts all
   holds count for ([R11](#5-what-work-needs)) come to at most the machine's
   amount ([R35](#8-ci-wins)).
2. **CI never waits.** A CI job's hold is taken in the step it is asked for.
   With 1, this needs [R39a](#8-ci-wins): the runners on a machine fit in it.
3. **Paused counts for no CPU**, keeps its other resources, and its size is
   what it was ([R34](#8-ci-wins), [R38](#8-ci-wins)).
4. **Unpause first.** While any hold on a machine is paused, no waiter is
   given a hold there ([R36](#8-ci-wins)).
5. **Arrival order.** Among waiters on a machine, none is given a hold before
   one that arrived earlier and is still waiting ([R18](#6-holds)).
6. **Never-fit is refused,** not queued ([R12](#5-what-work-needs)).
7. **Every hold ends.** A hold whose command has ended is released
   ([R13](#6-holds), [R15](#6-holds)). A CI job's hold is outside this until
   [Q1](#11-open-questions) is designed.
8. **Every paused hold is unpaused,** and every waiter that does not give up
   is given its hold, provided CI jobs stop arriving for long enough. Without
   that, neither is promised ([R37](#8-ci-wins)).
9. **No double deal.** Two consumers dealt in one tick never count the same
   room ([R50](#104-the-deal)).
10. **No deal behind a pause** ([R52](#104-the-deal)).
11. **Stopped work is dealt again.** A consumer stopped for a CI job's memory
    ends with `fault` and is not lost ([R39](#8-ci-wins)).

Models that exist and change: nova-reserve's SlotCount, SlotFiles and
Capacity (one number becomes named numbers; a paused state and a line are
added) and JobControl (stopping and continuing a running command, if
[Q3](#11-open-questions) is answered that way); nova-sprint's Runner (its
width and slots), Steps (the deal's room) and CardMachine (the cut's guard on
width).

## 13. Order of building

Not today. Bottom up, each layer secured before the next starts.

In nova-tools:

1. The benchmark ([R26](#7-cpu-units)).
2. nova-config: a machine's tags, CPU units and memory ([R6](#4-tags-and-resources)).
3. nova-reserve: named holds, refusal at once, the line ([6](#6-holds)).
4. nova-reserve: paused holds, and CI runners taking holds ([8](#8-ci-wins)).
5. Asking the whole fleet ([Q2](#11-open-questions)).

In nova-sprint, on top of 1 to 4:

6. The card's line and admission ([10.1](#101-the-card)).
7. The roster and the heartbeat ([10.2](#102-the-roster), [10.3](#103-the-heartbeat)).
8. The deal and policy ([10.4](#104-the-deal), [10.7](#107-policy)).
9. The board ([10.6](#106-the-board)).

Friends working across the fleet ([10.5](#105-friends)) follows 5.

## 14. Not in v3.0.0

- **Work across several machines.** A card holding sets of needs on more than
  one machine at once.
- **A deadline in place of an amount.** Work says "finish in two minutes" and
  the system picks how much to hold.
- **Splitting work across machines.** A test suite's CPU taken as shards on
  several machines.
- **Renting what the fleet lacks.**
- **A most that CI may hold on a machine** ([R37](#8-ci-wins)).
- **Anything about landing.** v2's landing is fixed in v2.
