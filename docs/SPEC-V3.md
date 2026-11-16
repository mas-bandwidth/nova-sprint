# nova-sprint v3.0.0: resources

A specification. Reconciled and settled for v3.0.0.

v3 is experimental. v2 is still the version in use, and it keeps being fixed
and worked in. This specification changes one thing about v2 and leaves the
rest alone, landing included.

Each rule is numbered and marked with who decided it: **[G]** Glenn, settled on
2026-11-15, or **[R]** Rowan, settled and reconciled in review by Emma
Antigravity on 2026-11-15.

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

In v3.0.0:
- **All resources are identical [G]:** CPU, memory, and devices obey one
  identical fit rule ([R11](#5-what-work-needs)). If any needed resource is
  unavailable, work cannot start.
- **Pause only between commands [G]:** Running processes are never killed or
  frozen mid-command ([R34](#8-ci-wins)). CI takes its holds immediately; card
  holds yield at command boundaries, run at low OS priority (`CPUWeight=1` +
  `SCHED_IDLE` on Linux, `PRIO_DARWIN_BG` on macOS vs `CPUWeight=10000` /
  `IOWeight=10000` on CI), and do not start their next command until CI ends.
- **Reads are pure reviews [G]:** Reads do not run `DONE-WHEN` or
  `STREAM GATE` commands and always take the light default budget (`cpu 1, memory 1`,
  zero hardware/OS tags) ([R42](#101-the-card), [R43](#101-the-card)).
- **Atomic fleet reservation [G]:** Friends acquire reservations atomically
  before connecting ([R55](#105-friends)), replacing racy check-then-SSH.
- **Unix/server boundary [G]:** Server tools run strictly on Linux, macOS, and
  WSL2. There are no native Windows benches and no `nova-reserve.exe` ([R16](#6-holds)).
- **Retire `NEEDS-ROUTE:` [G]:** `NEEDS-ROUTE:` is retired in favor of
  `RESOURCES:` ([R41](#101-the-card)), and OS/arch tags are auto-derived from
  `nova-config` ([R1](#4-tags-and-resources)).

## 2. Why

**Width is a guess.** A bench's width is its core count, capped by
`fleet_width_cap` (8 by default), and capped again by its nova-reserve slots.
The default policy then works at most 4 consumers on a bench. None of these
numbers is measured, and none knows what a consumer does: a read that waits on
a model costs the same as a build that uses every core.

**CI is not counted.** A runner's jobs take no reservation, and the dealer
cannot see them. The one rule between CI and cards was that card work ran at
a lower POSIX priority (`nice`) than CI.

**That rule was not enough on 2026-11-15,** as measured in that day's sprint:
hulk ran at a load of 70 with four or five CI runners and card work sharing
it, and CI shards there were cancelled at the two-minute limit for an hour.
Under Linux cgroup v2, `github-runner` ran in `/system.slice` while bench
workers ran in `/user.slice/user-<uid>.slice`; because both slices defaulted to
`cpu.weight=100`, POSIX `nice` inside the user slice did not constrain CPU
shares against the system slice.

In v3, CI runners take explicit holds, card holds yield at command boundaries,
and OS slices enforce real cross-slice priority (`CPUWeight=10000` / `IOWeight=10000`
on CI vs `CPUWeight=1` / `IOWeight=1` / `SCHED_IDLE` on bench work).

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
| waiter | Work that has asked for a hold and not yet been given it. A waiter holds nothing. Time spent waiting is excluded from deadlines ([R39b](#8-ci-wins)). |
| paused | The state of a hold that cannot start its next command because CI holds resources on the machine ([8](#8-ci-wins)). A paused hold holds no resources on the ledger while paused, and resumes before new work. Time spent paused is excluded from deadlines ([R39b](#8-ci-wins)). |
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
The machine's operating system (`linux`, `mac`, `windows`, with `darwin`
normalized to `mac`) and architecture (`amd64`, `arm64`) are auto-derived as
tags from `nova-config` ([Q8](#11-open-questions)). Conflicting manual OS or
arch tags are refused.

**R2 [G].** A resource is a name and a whole number on a machine: `cpu 16`,
`memory 64`, `ps5 1`. Amounts are whole numbers. There are no fractions.

**R3 [G].** There is one kind of resource. CPU, memory and a device are the
same record. A thing a machine has one of is a resource with amount 1: work
holds all of it or does not name it.

**R4 [G].** `memory` is in gigabytes (GiB). `cpu` is in CPU units.

**R5 [R].** Within a fleet a name is a tag or a resource, the same on every
machine. A fleet picks one.
Naming convention: shareable toolchains or SDKs that allow concurrent builds
use a tag with the `-sdk` suffix (e.g. `ps5-sdk` tag allows 10 compile cards
to run concurrently). Exclusive physical hardware uses a resource with
amount 1 (e.g. `ps5 1` ensures only one card accesses the physical devkit).

**R6 [G].** A machine's tags and resources are kept in nova-config with the
machine. The link runs both ways: from a machine to what it has, and from a
name to the machines that have it (`nova-config resources`).

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
work needs, and for every resource the work needs, the amount other unpaused
holds count for plus the amount needed is at most the machine's amount:

    sum(unpaused holds on r) + needed(r) <= machine's amount(r)

All resources are identical: CPU, memory, and devices obey this same rule. If
any needed resource does not fit, the work cannot start.

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
the hold. There are no tokens and no leases. (A CI job holds across its run:
[Q1](#11-open-questions).)

**R16 [R].** nova-reserve on a machine is the one authority on what is held
there. Two askers can never hold the same resources, whoever sent them.
Every caller on a machine (sprint bench worker, CI runner, friend SSH session,
human) uses the single shared state directory `/var/tmp/nova-reserve` with
shared permissions.
Unix/server boundary: server tools run strictly on Linux, macOS, and WSL2.
There are no native Windows benches and no `nova-reserve.exe`. On WSL2 hosts,
Windows CI runner hooks invoke `wsl.exe ... nova-reserve hold --holder ci` so
one Unix ledger governs the physical machine ([Q6](#11-open-questions)).

**R17 [R].** A hold steers and sets process environment. It exports
`NOVA_RESERVE_DIR`, `NOVA_RESERVE_CPUS=<cores>`, `GOMAXPROCS=<cores>`,
`CARGO_BUILD_JOBS=<cores>`, `RAYON_NUM_THREADS=<cores>`,
`CMAKE_BUILD_PARALLEL_LEVEL=<cores>`, and `MAKEFLAGS`.
On Linux it sets OS priority (`CPUWeight=1`, `IOWeight=1`, `SCHED_IDLE`) for
bench card holds, while CI runs at `CPUWeight=10000`, `IOWeight=10000`, `Nice=-10`.
On macOS it sets `setpriority` `PRIO_DARWIN_BG` for bench holds.

**R18 [R].** Work that fits a machine but finds no room now waits in one line
per machine (`line/<seq>.lock`). Waiters are served strictly in arrival order:
a later, smaller request does not pass an earlier one. A waiter that gives up
leaves the line. Two things do not use the line: a CI job ([R33](#8-ci-wins))
and unpausing ([R36](#8-ci-wins)).
Re-entrancy guard: if `NOVA_RESERVE_DIR` in the process environment matches the
target directory, `nova-reserve` executes the child command directly without
taking a nested hold, preventing self-deadlocks inside CI scripts.

**R19 [R].** A hold carries explicit metadata:
- `--holder <bench|friend|ci|person>`
- `--consumer <card.attempt@epoch>`
- `--label <ci|blocker|critical|fix|read|work|custom>`
- `--priority <blocker|critical|read|high|normal|low>`
- `--why <text>`

Priority levels run in strict order:
`blocker > critical > read > high > normal > low`.
A hold whose asker sets no priority defaults to `normal`.

**R20 [R].** Asking for the whole machine (`--exclusive`) holds everything the
machine has.

**R21 [G].** nova-tools has no global view of resources. It answers questions:
what tags and resources exist and on which machines (`nova-config resources`),
what is held now and by what on a machine (`nova-reserve status --json`), and
fleet-wide reservations (`nova-fleet reserve`, `nova-fleet resources`). The
visual picture is the sprint's board ([10.6](#106-the-board)).

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

**R27 [R].** A machine is measured once, when it joins a fleet. The benchmark
auto-runs on `run`/`hold` only when `<dir>` is the system default `/var/tmp/nova-reserve`
and `capacity.json` is missing; it never auto-runs on custom or temporary
directories.
The benchmark holds `--exclusive` while it runs. Usable memory is read from the
machine at the same time (cgroup v2 `memory.max` if bounded, else physical GiB,
at least 1). If a CI job arrives during a benchmark, calibration aborts with
non-zero status rather than recording an invalid score ([Q12](#11-open-questions)).

**R28 [R].** CPU is counted in CPU units ([R11](#5-what-work-needs)). Cores
are not counted. They are only what work is told to use:

    cores to use = units held x usable cores / machine's CPU units, rounded down, at least 1

Where nova-reserve can pin (Linux), it dynamically assigns disjoint physical
cores and SMT pairs across currently unpaused holds. Elsewhere the number is
only what the work's tools are told via environment variables.

**R29 [R].** The conversion is approximate, and that is accepted. On a fast
machine small pieces of work share cores. What work is told to use is within
one core of what it asked for.

**R30 [R].** Work that cannot be split across cores asks for `cpu 1` and is told
to use one core. `fast-core` is cut from v3.0.0 ([Q7](#11-open-questions)).

## 8. CI wins

**R31 [G].** A CI runner has tags (the labels a workflow selects it by) and
takes a hold for each job, like any other work.

**R32 [G].** A runner holds only while it runs a job. An idle runner holds
nothing, so no CPU is wasted while CI is not running.

**R33 [G].** CI wins over cards. A CI job never waits for other work. It does
not use the line ([R18](#6-holds)). It takes its hold at once, even when
other work fills the machine.

**R34 [G].** When a CI job arrives, it takes its hold immediately. Running
commands are never killed and never frozen mid-command with `SIGSTOP` or
`cgroup.freeze` ([Q3](#11-open-questions)).
Instead, active holds whose needed resources plus CI exceed the machine's
capacity become logically **paused** on the ledger: lowest priority first, and
youngest first on ties. Paused holds finish their already-running command at low
OS priority (`CPUWeight=1` + `SCHED_IDLE` on Linux, `PRIO_DARWIN_BG` on macOS),
and **cannot start their next command** until CI finishes and all their needed
resources fit again. While paused, a hold counts for zero resources on the
ledger.

**R35 [G].** A machine is never counted over what it has. For all resources,
what CI holds and what unpaused holds hold come to at most the machine's
amounts:

    sum(unpaused holds on r) + sum(ci holds on r) <= machine's amount(r)

**R36 [G].** When a CI job ends, paused holds come back first. While any hold
on a machine is paused, no waiter and no new work is given a hold there. Each
paused hold is unpaused as soon as all its needed resources fit, highest
priority first. A continuing consumer unpauses ahead of `line/` waiters.

**R37 [G].** By default CI can take all of a machine.

**R38 [R].** A hold is in one of two states, held or paused, and its size
never changes.

**R39 [DELETED].** Mid-command stopping/killing is deleted. All resources are
identical ([R11](#5-what-work-needs)): if all needed resources are not
available, work cannot start its next command. In-flight commands run to
completion under OS priority steering.

**R39a [R].** CI does not pause or stop CI. A machine's runners are sized so
that all runner scopes fit in the machine:

    sum over scopes s (runners_s * needs_s) <= machine's amount(r)

for every resource `r`.

**R39b [R].** Time a consumer spends paused or waiting in line is excluded
from all worker, step, and stall deadlines (`working_max`, `DeadlineAt`,
step execution timeouts, and stream landing stall detection).

Glenn: "The design I want does not waste CPU while we are not running CI, so
it does work, but then prioritizes CI ahead of work, when we do.", "the card
yields to CI", "the CPU usage of CI and cards sums to 100% always on that
machine.", "unpausing cards are done, before new cards are dealt."

## 9. The check

**R40 [G].** Each machine still measures its real CPU use, as a bench's
heartbeat does in v2. The measurement is kept internal for coordinator
awareness and health checking; it is not drawn on the board.

## 10. What changes in nova-sprint

Tier, routes and a friend's width do not change **[G]**. Producer cards do
not change at all **[G]**. Streams, needs between cards, review and landing
do not change.

### 10.1 The card

Today a brief's first line is its tier, and a card names a route it needs with
a `NEEDS-ROUTE:` line.

**R41 [R].** `NEEDS-ROUTE:` is retired in v3.0.0 ([Q5](#11-open-questions)).
A card states what its work needs with a `RESOURCES:` line in the brief header:

    RESOURCES: cpu 4, memory 8, mac, metal

A card with no `RESOURCES:` line states nothing, so its work's needs are the
defaults: `cpu 1` and `memory 1`.

**R42 [G].** The coordinator sets the `RESOURCES:` line for each card.
**Reads are purely code and diff reviews [G]**: reads do not run `DONE-WHEN`
or `STREAM GATE` commands (stripped in both v2 PR #835 and v3). Work cards
and CI own building and running tests; reads inspect the diff against base,
invariants, code structure, and test quality ([Q4](#11-open-questions)).

**R43 [G].** Every `read` consumer uses the light default read budget:
`cpu 1, memory 1` with zero hardware or OS tags, regardless of the producer
card's `RESOURCES:` line. Work consumers use the card's `RESOURCES:` line.

**R44 [R].** `add` refuses a card whose list is malformed. It also refuses a
card whose needs no single bench row in `nova-config` can ever satisfy jointly
across tier, tags, and resource amounts ([R12](#5-what-work-needs)).

### 10.2 The roster

Today a bench is a row whose width is its cores, capped by `fleet_width_cap`,
and capped again by the nova-reserve slots it reports.

**R45 [G].** A bench has no width. Its row carries its tags and resources
from nova-config. An unmeasured bench has `Held: "unmeasured resources in nova-config"`.

**R46 [G].** `fleet_width_cap` is removed. It has nothing to bound.

**R47 [G].** A friend keeps its width: how many consumers it takes on at
once. It says nothing about where their work runs.

### 10.3 The heartbeat

Today a bench's heartbeat carries its load, its CPU peak, its cores, its
nova-reserve slots used and in all, and the tiers it serves.

**R48 [R].** A bench's heartbeat carries:
- `resources`: machine capacity amounts
- `held_sprint`: amounts held by unpaused sprint bench consumers
- `held_other`: amounts held by non-sprint work (CI, friends, people)
- `held_by_label`: unpaused amounts grouped by label (`ci`, `blocker`, `critical`, `fix`, `read`, `work`)
- `paused_by_label`: paused amounts grouped by label
- `any_paused`: boolean, true if **any** hold on the machine is paused
- `sprint_consumers`: list of consumer IDs holding on the bench
- `paused_consumers`: list of paused consumer IDs
- `paused_millis`: monotonic cumulative pause+wait duration per consumer ID

The heartbeat emits edge-triggered updates immediately upon hold or pause state
changes. Cores, slots, and load are dropped from the board.

### 10.4 The deal

**R49 [G].** The dealer decides where work it places on benches runs.

**R50 [R].** The dealer deals a consumer to a bench only when:
1. The bench has every tag the card needs.
2. The bench has `any_paused == false` ([R52](#104-the-deal)).
3. For every resource `r` the card needs:

       amount in all(r) - used(r) >= card's need(r)

   where `used(r)` is calculated by exact consumer-ID union sum:

       Let C_active be the union of:
         - consumer IDs currently in state "working" on bench B (including deals earlier in this tick)
         - consumer IDs in row.Resources.SprintConsumers (still holding during wind-down)

       sprint_used(B, r) = sum_{c in C_active} card_needs(c)[r]
       used(B, r) = row.Resources.HeldOther[r] + sprint_used(B, r)

Two deals in one tick cannot take the same room.

**R51 [R].** The hold itself is taken on the bench, through nova-reserve, when
the command starts. A consumer waits in the bench's line ([R18](#6-holds))
until room is available or the sprint withdraws it.

**R51a [R].** A consumer's run consists of several command steps (harness, seal,
apply, gate). Each step takes a hold via `nova-reserve` carrying the consumer ID
and needed resources. Command boundaries serve as clean yield gates: if CI has
arrived, the completed step exits its hold, the consumer transitions to paused
on the ledger, and once CI ends, the unpausing consumer resumes before new
waiters ([R36](#8-ci-wins)).

**R52 [G].** The dealer deals nothing new to a bench while `any_paused` is true
on that bench ([R36](#8-ci-wins)).

**R53 [R].** Deal order is unchanged: v2's ladder (blocker, critical, read,
high, normal, low). Preemption stays for blockers only: when no open bench that
has the blocker's tags has its needs free, preemption checks that
`total(r) - held_other(r) >= blocker.needs(r)` and `!any_paused`, and withdraws
sprint victims on the bench in v2's order with `fault` until the blocker fits.
Friend-row preemption is retained.

**R54 [R].** The idle alarm measures fleet utilization as the average across
open rows of:
- on open benches: `min(1, cpu_held / cpu_total)`
- on open friends: `working / width`

The alarm is suppressed when any bench is paused or when ready cards are blocked
on unavailable tags or resources ([Q14](#11-open-questions)).

### 10.5 Friends

**R55 [G].** Friends use **atomic fleet reservation** ([Q2](#11-open-questions)).
A friend does not guess or use racy check-then-SSH advice. It requests an
atomic reservation from `nova-tools`:

    nova-fleet reserve --needs "<spec>" [--holder friend] [--consumer <id>]

`nova-fleet` finds an eligible machine, acquires the `nova-reserve` hold
atomically over SSH, holds it open, and prints the assigned host. The hold
auto-releases on disconnect.

**R56 [G].** A friend's work holds resources like any other. The reservation
covers the friend's work on that host.

**R57 [R].** Because the reservation is held atomically across the friend's
work on that host, the dealer cannot fill the bench between steps while the
friend's worktree is staged there.

**R58 [R].** A friend is dealt cards as in v2: by tier, routes and its width.
When executing, it obtains an atomic reservation via `nova-fleet reserve`.

### 10.6 The board

Today the board has a Fleet table and a Friends table.

**R59 [G].** The Fleet section becomes Resources. Its rows are the machines.
Columns are: `machine`, `status`, `cpu`, `memory`, `tags`.
Columns `width`, `working/width`, `ready`, `load`, and visual `ok%`/`done` are
dropped from the Resources table. (Machine `ok%` remains internal for
coordinator health checks.)

**R60 [G].** Resources answers one question: are we using the fleet's
resources fully, yes or no. It shows everything that holds, CI included.

**R61 [G].** A machine's bars are split by label: `ci`, `blocker`, `critical`,
`fix`, `read`, and `work`.

**R62 [G].** `ci` is colored steel blue / cyan (`#38bdf8`), outside the card
severity spectrum. Paused work is shown on the machine's bar behind the CI that
displaced it.

**R63 [G].** The load and ready columns are removed.

**R64 [G].** The Friends section is **100% unchanged**. It draws each friend's
consumer cards against its width.

### 10.7 Policy

**R65 [R].** `width(row)` is asked only for friends (`kin`). For a bench there
is no width. `limits()` no longer carries `fleet_width_cap`.
In Layer 5 policy:
- `jointRows` treats candidate benches as inherently capable (`capable = true`),
  never comparing them against `a.Width`.
- `inputs.go` attaches `tags` and `resources` to `card` before `unworded()`
  blanks the brief, and attaches `tags`, `resources`, and `free` room to bench
  candidates.

### 10.8 Removed

- A bench's width, and every count against it (roster width, check verb width line, working-over-width column).
- `fleet_width_cap`.
- The heartbeat's cores and slots.
- The board's load and ready columns, and visual `ok%`/`done` on Resources.
- `NEEDS-ROUTE:` in briefs.
- Mid-command process stopping or killing (`R39`).

## 11. Open questions (Settled Decisions)

**Q1. A CI job's hold.** Settled: `job-started.sh` spawns a detached
`nova-reserve hold --holder ci --label ci ...` process watching `Runner.Worker`
via Linux `pidfd_open` or macOS `kqueue`, holding an OS `flock` and exiting on
`job-completed.sh`. If `job-started.sh` fails, `Runner.Worker` is terminated
so unheld steps cannot execute. Runner job needs are configured per scope in
`nova-config`.

**Q2. Asking the whole fleet.** Settled: Friends and scripts use atomic
reservation via `nova-fleet reserve --needs ...`, which probes candidates and
locks the hold on a winning host atomically before returning the hostname.
Fleet status is queried via `nova-fleet resources [--json]`.

**Q3. What paused does to the process.** Settled: Pause only between commands
([R34](#8-ci-wins)). No mid-command `SIGSTOP` or `cgroup.freeze`. CI holds
take resources immediately; card holds yield at command boundaries, run at low
OS priority (`CPUWeight=1` + `SCHED_IDLE` on Linux, `PRIO_DARWIN_BG` on macOS
vs `CPUWeight=10000` / `IOWeight=10000` / `Nice=-10` on CI), and do not start
their next command until CI finishes and all needed resources fit again.

**Q4. Reads.** Settled: Reads do not run `DONE-WHEN` or `STREAM GATE` commands
(stripped in PR #835 and v3). Reads are purely code/diff reviews and always
take the default read budget (`cpu 1, memory 1`, zero hardware/OS tags),
regardless of the producer card's `RESOURCES:` line ([R42](#101-the-card),
[R43](#101-the-card)).

**Q5. Devices vs routes.** Settled: `NEEDS-ROUTE:` is retired in v3.0.0.
Shareable toolchains use tags with the `-sdk` suffix (`ps5-sdk`); exclusive
hardware devices use amount-1 resources (`ps5 1`) ([R5](#4-tags-and-resources)).

**Q6. Windows.** Settled: Strictly no native Windows benches and no
`nova-reserve.exe`. Server tools run on Linux, macOS, and WSL2. On WSL2 hosts,
Windows CI runner hooks call `wsl.exe ... nova-reserve hold --holder ci` so one
Unix ledger governs the physical host. Native Windows clients interact only
via `nexus-client`.

**Q7. Work that cannot be split.** Settled: `fast-core` is cut from v3.0.0.
Single-threaded work asks for `cpu 1` and runs on one core ([R30](#7-cpu-units)).

**Q8. Operating system and architecture as tags.** Settled: `os` (`linux`, `mac`,
`windows`, with `darwin` normalized to `mac`) and `arch` (`amd64`, `arm64`)
are automatically derived as tags from `nova-config` rows.

**Q9. Blocker preemption.** Settled: Retain v2 withdraw (`fault`, [R53](#104-the-deal)).
Preemption withdraws sprint victims on a bench only when
`total(r) - held_other(r) >= blocker.needs(r)` and `!any_paused`, and retains
friend-row preemption.

**Q10. Board columns on Resources table.** Settled: Drop `ready`, `load`, and
`width`. Move machine `ok%` internal for coordinator checks. Friends table
remains 100% unchanged ([R59](#106-the-board), [R64](#106-the-board)).

**Q11. Color for CI.** Settled: Steel blue / cyan (`#38bdf8`), distinct from
the sprint card severity colors ([R62](#106-the-board)).

**Q12. CI vs `--exclusive` / benchmark.** Settled: Calibration benchmarks run
on an idle machine before enabling runners. If CI arrives during a benchmark,
the benchmark aborts with non-zero exit status rather than recording an
invalid measurement ([R27](#7-cpu-units)).

**Q13. Command yield gates.** Settled: Command boundaries serve as clean yield
gates ([R51a](#104-the-deal)). Each step carries the consumer ID; unpausing
consumers resume ahead of new waiters once CI clears ([R36](#8-ci-wins)).

**Q14. Unified idle alarm.** Settled: Average of bench CPU unit utilization
and friend working/width, suppressed when any bench is paused or cards are
blocked on tags/resources ([R54](#104-the-deal)).

## 12. What has to hold

The state machines here get TLA+ models before code, and the code is bound to
the models by trace checks, as in v2. These are the properties the models
must check.

1. **Never over.** On every machine, for every resource, the amounts all
   unpaused holds count for come to at most the machine's amount ([R35](#8-ci-wins)).
2. **CI never waits.** A CI job's hold is taken in the step it is asked for.
   With 1, this needs [R39a](#8-ci-wins): runner scopes fit in the machine.
3. **Paused counts for no resources** while paused, yields between commands,
   and unpauses to its original size ([R34](#8-ci-wins), [R38](#8-ci-wins)).
4. **Unpause first.** While any hold on a machine is paused, no waiter and no
   new deal is given a hold there ([R36](#8-ci-wins), [R52](#104-the-deal)).
5. **Arrival order.** Among waiters on a machine, none is given a hold before
   one that arrived earlier and is still waiting ([R18](#6-holds)).
6. **Never-fit is refused,** not queued ([R12](#5-what-work-needs)).
7. **Every hold ends.** A hold whose command has ended is released
   ([R13](#6-holds), [R15](#6-holds)).
8. **Every paused hold is unpaused,** and every waiter that does not give up
   is given its hold, provided CI jobs stop arriving for long enough
   ([R36](#8-ci-wins), [R37](#8-ci-wins)).
9. **No double deal.** Two consumers dealt in one tick never count the same
   room; dealer room uses the exact consumer-ID union sum ([R50](#104-the-deal)).
10. **No deal behind a pause.** While `any_paused` is true on a bench, no new
    cards are dealt to it ([R52](#104-the-deal)).

Models that exist and change: nova-reserve's SlotCount, SlotFiles and
Capacity (one number becomes named numbers; a paused state and a line are
added); nova-sprint's Runner (its width and slots), Steps (the deal's room)
and CardMachine (the cut's guard on width).

## 13. Order of building

Bottom up, each layer secured before the next starts.

In nova-tools:
1. `modules/config`: tags, resources, auto OS/arch tags, schema migration (Track 1).
2. `cmd/nova-reserve`: named holds, measurement, `/var/tmp/nova-reserve`, FIFO line, paused holds, OS priority steering (Track 2).
3. `cmd/nova-fleet`: atomic reservation `nova-fleet reserve`, fleet resources status, runner integration (Track 3).

In nova-sprint:
4. `modules/card`, `modules/roster`, `modules/store`: `RESOURCES:` parser, `card.Needs`, `roster.Machine`/`Worker`, `resources_json` store schema (Track 4).
5. `modules/steps`, `modules/policy`: exact consumer-ID union sum dealer room, `any_paused` guard, Layer 5 `jointRows`/`inputs.go` (Track 5).
6. `modules/edge`, `modules/run`, `modules/verbs`: worker per-step holds, yield gates, direct pgx config reads, deadline pause subtractions (Track 6).
7. `modules/dash`: Resources table rendering, cyan CI, paused behind CI, untouched Friends table (Track 7).

## 14. Not in v3.0.0

- **Mid-command process freezing or killing.** No `SIGSTOP`, `cgroup.freeze`,
  or memory-based termination of in-flight commands.
- **Native Windows benches or `nova-reserve.exe`.** Windows runs `nexus-client`
  only; server tools run on Linux, macOS, and WSL2.
- **`fast-core` single-core prioritization.**
- **Work across several machines.** A card holding sets of needs on more than
  one machine at once.
- **A deadline in place of an amount.** Work says "finish in two minutes" and
  the system picks how much to hold.
- **Splitting work across machines.** A test suite's CPU taken as shards on
  several machines.
- **Renting what the fleet lacks.**
- **A most that CI may hold on a machine** ([R37](#8-ci-wins)).
- **Anything about landing.** v2's landing is fixed in v2.
