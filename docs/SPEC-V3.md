# nova-sprint v3.0.0: resources

A specification. Reconciled and settled for v3.0.0 after cold review, v2 stabilization, and Steward scheduler lessons (2026-11-16).

v3 is experimental. v2 remains the stable version in production while v3 is built bottom up. This specification replaces bench `width` and local slot files with a fleet-wide client/server resource system in `nova-tools` (`nova-reserve`), and makes `nova-sprint` a peer client and live subscriber of that system while leaving streams, producer cards, review, and landing unchanged.

Each rule is numbered and marked with who decided it: **[G]** Glenn (2026-11-15 and 2026-11-16), or **[R]** Rowan / Emma, reconciled with Glenn's rulings.

## 1. The change

In v2 a bench machine has a **width**: how many consumer cards it works at once. Every consumer costs one unit of width, whatever it does. CI jobs on the same machine are not counted at all, and reservations are local per-host slot files that nobody outside the host can see or queue across.

In v3 a machine has **tags** and **resources** in `nova-config`, and **`nova-reserve` in `nova-tools` is a standalone client/server fleet resource arbitrator** (`nova-reserve serve`). Anybody—friends, bench workers, sub-jobs inside a card, CI runners, people, scripts, or `nova-sprint`—can ask `nova-reserve` for resources and get a lease, with or without `nova-sprint` running.

Glenn (2026-11-15):
- *"The actual pattern is that cards require resources, and can run anywhere those resources are provided. Be it CPU, memory, GPU, dev tools whatever..."*

Glenn (2026-11-16):
- *"Yes the idea is that any friends (or even workers...) can grab resources"*
- *"the resource system must function in nova-tools, even without nova-sprint"*
- *"so it might be client/server, with the central server arbitrating over stuff"*
- *"but it must not only give resources to the coordinator, anybody can ask for a resource and get it"*

Summary of settled v3.0.0 architecture:

- **`nova-reserve` is the standalone central arbitrator; `nova-sprint` is a peer client and subscriber [G]:** `nova-reserve serve` arbitrates all fleet machines from `nova-config` over Tailscale TCP (with `--local` Unix-socket mode for single-machine standalone operation). Friends, bench workers, CI runners, people, and `nova-sprint` all talk to `nova-reserve` as peers. There is no coordinator gatekeeping, no bench-to-bench SSH, and no duplicate resource ledger in `sprint.events`.
- **Connection lifetime is lease lifetime [R]:** Active holds live on open client connections (`5s` keepalive, `15s` dead-peer expiry, `15s` `reconnect` grace window across server restarts, and `3s` local fail-open for `--holder ci`).
- **All resources obey one vector fit rule, with deadlock-free nested sub-leases [G, R]:** CPU, memory, and exclusive devices obey the same fit check ([R11](#5-what-work-needs)). Child processes inherit `NOVA_RESERVE_LEASE` (and local PPID fallback) so nested `nova-reserve` calls within an ancestor's grant succeed as zero-extra-debit sub-leases and cyclic extra asks fail fast (`RESERVE DEADLOCK`).
- **CI wins immediately; pause only between commands + real OS priority [G]:** `--holder ci` never waits ([R33](#8-ci-wins-and-user-claims)). Displaced holds become **`paused`** in the central ledger, finish their in-flight command under cross-slice OS priority (`system.slice` `CPUWeight=10000` vs `user.slice` `CPUWeight=1` + `SCHED_IDLE` on Linux; Interactive QoS vs `taskpolicy -b` on macOS), block at the next command boundary, and resume in priority order before any queued or new work starts ([R34](#8-ci-wins-and-user-claims), [R36](#8-ci-wins-and-user-claims)). Running commands are never frozen (`SIGSTOP`) or killed (`R39` deleted).
- **Timed soft user claims (`nova-reserve claim`) [R]:** People can claim a machine or resource vector (`--ttl 4h` default) immediately; available capacity may go negative while running commands finish, nothing new starts on claimed resources until usage drains below the claim, and expired claims clear automatically ([R37a](#8-ci-wins-and-user-claims)).
- **Reads are pure reviews [G]:** `read` consumers run zero `DONE-WHEN` or `STREAM GATE` commands and always take the light default budget (`cpu 1, memory 1`, zero hardware/OS tags) ([R42](#101-the-card), [R43](#101-the-card)).
- **Unix/server boundary [G]:** Server tools run strictly on Linux, macOS, and WSL2. There are no native Windows benches and no `nova-reserve.exe` ([R16](#6-leases-and-the-central-arbitrator)).

## 2. Why

**Width is a guess.** In v2 a bench's width is its core count, capped by `fleet_width_cap` (8 by default), and capped again by its `nova-reserve` slots. None of these numbers is measured, and none knows what a consumer does: a read waiting on a model costs the same as a Rust or C++ build using every core.

**CI was invisible and cross-slice priority was uneven.** On 2026-11-15, `hulk` ran at a load of 70 with four or five CI runners and card work sharing it, cancelling per-commit CI shards at the two-minute limit for an hour. Under Linux cgroup v2, `github-runner` services ran in `/system.slice` while bench workers and SSH sessions ran in `/user.slice/user-<uid>.slice`; because sibling slices both defaulted to `cpu.weight=100`, POSIX `nice -n 15` inside the user slice still allowed card builds up to half the machine's CPU while CI ran.

**Per-host lock files isolated machines from callers.** A local flock directory on each bench forced friends and tools either to guess a host before SSHing (racing the dealer and other friends) or to fan out over SSH, which fleet ACLs forbid between `tag:nova` machines, while forcing `nova-sprint` to reconstruct fleet state through 30-second worker heartbeats into PostgreSQL.

Glenn: *"this will all be approximate, but better than what we have which is weirdly non-scientific around guessing the # of cards per-machine"*.

## 3. Words

| Word | Meaning |
| --- | --- |
| machine | A computer in the fleet (`linux`, `mac`, or `wsl2`), recorded in `nova-config`. |
| bench | A fleet machine running `nova-worker` as a sprint row. |
| open bench | A bench row that is `up`, not held, and hooked in, as in v2. |
| tag | A capability name a machine has or does not have (`linux`, `mac`, `gpu`, `metal`, `ps5-sdk`). Never used up. |
| resource | A counted whole-number capacity on a machine (`cpu 16`, `memory 64`, `ps5 1`). Work holds amounts of it. |
| CPU unit | The unit of the `cpu` resource: one calibrated standard core ([7](#7-cpu-units)). |
| usable core | A CPU core the OS scheduler makes available to `nova-reserve` (excluding kernel-isolated cores). |
| work | Anything that asks `nova-reserve` for resources: a friend's build/test session, a worker step or sub-job, a CI job, a script, or a person's command. |
| needs | The tags and resource amounts work asks for, including the defaults of [R10](#5-what-work-needs). |
| lease (hold) | An active reservation granted by `nova-reserve` on one machine (`lease_id` + `host`), bound to the holder's live connection. |
| sub-lease | A nested reservation inside an existing ancestor lease (`NOVA_RESERVE_LEASE`) on the same machine that fits within the ancestor's remaining grant without debiting the machine a second time. |
| claim | A timed soft reservation by a person (`nova-reserve claim`, `--holder person`) that blocks new conflicting starts while letting running commands finish. |
| waiter | A request queued in `nova-reserve` waiting for an eligible machine with room. A waiter holds nothing. |
| paused | The ledger state of an active lease displaced by `--holder ci` ([8](#8-ci-wins-and-user-claims)). Its running command finishes at background OS priority; its next command blocks until unpaused; while paused it counts for zero resources on the ledger. |
| source | The logical caller identity (`--source <name>`, e.g. `emma`, `rowan`, `b1`, `ci`) used for round-robin fairness within a priority level. |
| producer card | The unit of work in a sprint stream. Unchanged from v2. |
| consumer card | One sprint attempt at a producer (`work`) or one review of its head (`read`). |
| dealer | The `nova-sprint` step (`modules/steps`) that assigns sprint consumer cards to benches and friends. |
| friend | An AI agent (`emma`, `rowan`, `stella`, `johnny`, `freddy`, `aurora`). |
| CI job | One job of a GitHub Actions runner on a fleet machine. |

## 4. Tags and resources

**R1 [G].** A tag is a name a machine has or does not have: `linux`, `mac`, `windows`, `amd64`, `arm64`, `gpu`, `metal`, `ps5-sdk`. A tag is never held or depleted; any number of leases can run concurrently on a machine that has it. A machine's operating system (`linux`, `mac`, `windows`, normalizing `darwin` to `mac`) and architecture (`amd64`, `arm64`) are derived automatically from `nova-config`; manual tags that conflict with a machine's OS or architecture are refused.

**R2 [G].** A resource is a name and a positive whole number on a machine: `cpu 16`, `memory 64`, `ps5 1`. Amounts are whole numbers; there are no fractions.

**R3 [G].** There is one kind of resource. `cpu`, `memory`, and an exclusive device (`ps5 1`, `hil-board 1`) obey the exact same accounting and fit rules. A singleton physical device is a resource with amount `1`: work either holds `1` of it or does not name it.

**R4 [G].** `memory` is measured in usable gibibytes (`GiB`). `cpu` is measured in calibrated CPU units ([7](#7-cpu-units)).

**R5 [R].** Within a fleet, each name is either a tag or a resource across every machine—never both (`nova-config` refuses collisions). Shareable SDKs and toolchains that many builds can read at once use `-sdk` tags (for example, `ps5-sdk`); exclusive hardware that only one task may touch at a time uses an amount-1 resource (`ps5 1`).

**R6 [G].** Every machine's tags and resources live in `nova-config` (`config.machines`). `nova-config` answers both directions (`nova-config resources`): what a given machine has, and which machines carry a given tag or resource. `nova-reserve serve` loads machine capacities and tags from `nova-config` and reloads them live without dropping active leases.

**R7 [G].** Standard open-source build and CLI tools are installed uniformly across fleet benches via `nova-fleet`. Tags and resources name counted capacities (`cpu`, `memory`) and non-uniform hardware, OS/arch targets, or proprietary SDKs—not routine CLI binaries.

**R8 [G].** Nobody edits a machine's `cpu` or `memory` capacity by hand. Both are measured on the machine ([R26](#7-cpu-units), [R27](#7-cpu-units)) and recorded into `nova-config`. Owner-curated tags and custom device counts (`ps5 1`) are set through `nova-config`.

Example. `captain` has `linux`, `windows`, `amd64`, `gpu`. `studio` and `mini-m5` have `mac`, `arm64`, `gpu`, `metal`. A request for `gpu` may land on any of the three; a request for `gpu, metal` may land on `studio` or `mini-m5`, and never on `captain`.

## 5. What work needs

**R9 [G].** Any caller states what its work needs as one comma-separated list of names, some with positive integers:

    cpu 4, memory 8, mac, metal, ps5

Against `nova-config`'s vocabulary, a tag takes no number, a resource with a number requests that integer amount, and a resource written bare (`ps5`) means `1`. Callers do not have to separate tags from resources syntactically.

**R10 [R].** Work that omits `cpu` needs `cpu 1`. Work that omits `memory` needs `memory 1`. These defaults apply everywhere a rule refers to work's "needs".

**R11 [G].** Work **fits** on a machine `M` when:
1. `M` has every tag in the work's needs, and every resource named in the work's needs exists on `M` with total capacity at least the requested amount.
2. For every resource `r` in the work's needs, the unpaused leases and active soft person claims on `M` leave enough room:

       sum(unpaused leases on M for r) + claimed_person(M, r) + needed(r) <= capacity(M, r)

All resources—`cpu`, `memory`, and devices—obey this single all-or-nothing vector rule. If any needed resource does not fit, the work cannot start on `M` yet.

**R12 [R].** A request whose tags or resource amounts cannot fit even an empty machine (`needed(r) > capacity(M, r)` on the requested `--host`, or on **every** machine in `nova-config` for a fleet-any request) can never succeed. `nova-reserve` refuses it immediately with a clear diagnostic naming the missing tag or over-capacity resource; it never enters the wait queue.

## 6. Leases and the central arbitrator

**R13 [G].** Work acquires all of its needed resources before starting, holds the lease while running, and releases all of them when it ends, however it ends.

**R14 [G].** `nova-reserve` (`cmd/nova-reserve` in `nova-tools`) is the standalone resource manager. It operates in two modes behind one CLI and protocol:
1. **Fleet client/server mode (default when `NOVA_RESERVE_ADDR` is configured):** `nova-reserve serve` runs on the control host over Tailscale TCP (`NOVA_RESERVE_ADDR`, authenticated with `NOVA_RESERVE_PASSWORD` via `nova-secrets`), backed by `nova-config` (`config.machines`). It works **100% standalone without `nova-sprint`**.
2. **Local standalone mode (`--local` or when no fleet server is configured):** `nova-reserve serve --local` arbitrates a single machine over `/var/tmp/nova-reserve/reserve.sock` using `/var/tmp/nova-reserve/capacity.json`, with no database and no network required.

**R15 [R].** **Connection lifetime equals lease lifetime.** Every active lease (`lease_id` on `host`) is bound to the open client connection that requested it (`nova-reserve run`, `nova-reserve hold`, `nova-worker`, or `job-started.sh`):
- Clients and server exchange `5s` heartbeats; if a client process crashes, is `SIGKILL`ed, or its host loses power, TCP close or `15s` missed-keepalive expiry frees the lease automatically.
- **Zero-drop server restart (`15s` `reconnect` grace):** When `nova-reserve serve` restarts or upgrades, it enters a `15s` `reconnect` window before admitting new queued leases. Running clients automatically reconnect and re-present their active `{lease_id, host, needs, metadata}` state so server upgrades drop zero active leases.

**R16 [G, R].** `nova-reserve` is the single authority on what is held across the fleet, and **anybody is a first-class peer client**:
- **Friends** (`emma`, `rowan`, `stella`, `johnny`, `freddy`, `aurora`), **bench workers** (`nova-worker` or sub-jobs inside a card), **CI runners**, **people**, **scripts**, and **`nova-sprint`** all request resources directly from `nova-reserve`. The sprint coordinator never gatekeeps fleet resources.
- Callers may ask **fleet-any** (`--needs "cpu 8, memory 16, linux"`, letting `nova-reserve` assign the best available machine from one central fleet queue and return `{lease_id, host}`) or **host-pinned** (`--host b1 --needs "cpu 4, memory 8"`). No bench-to-bench SSH is ever used.
- **Unix/server boundary [G]:** Server tools run strictly on Linux, macOS, and WSL2. There are **no native Windows benches** and **no `nova-reserve.exe`**. Native Windows runs `nexus-client` only; on WSL2 hosts (`captain`), Windows CI runner hooks invoke `wsl.exe ... nova-reserve` so one Unix arbitrator governs the physical machine.

**R17 [R].** A lease steers execution through environment variables and OS scheduling when `nova-reserve run` (or `nova-worker`) spawns a local command on the leased machine:
- Exports `NOVA_RESERVE_LEASE=<lease_id>`, `NOVA_RESERVE_HOST=<host>`, `NOVA_RESERVE_CPUS=<cores>`, `GOMAXPROCS=<cores>`, `CARGO_BUILD_JOBS=<cores>`, `RAYON_NUM_THREADS=<cores>`, `CMAKE_BUILD_PARALLEL_LEVEL=<cores>`, and `MAKEFLAGS=-j<cores>`.
- On Linux: places non-CI card/batch work at `CPUWeight=1`, `IOWeight=1`, `SCHED_IDLE` (in `user.slice`) and pins `sched_setaffinity` to dynamically assigned disjoint physical cores / SMT pairs ([R28](#7-cpu-units)), while CI runs in `system.slice` at `CPUWeight=10000`, `IOWeight=10000`, `Nice=-10`.
- On macOS: places non-CI card/batch work under background QoS (`taskpolicy -b` / `PRIO_DARWIN_BG`) while CI and interactive user work run at normal/interactive QoS.

**R18 [R].** **Central queue, priority, `--source` round-robin, and head-of-line reservation.** Requests that fit in principle ([R12](#5-what-work-needs)) but do not fit right now ([R11](#5-what-work-needs)) wait in `nova-reserve`'s central wait queue (matching either `--host <machine>` or any machine satisfying `--needs`):
1. **Priority order:** `blocker > critical > read > high > normal > low` (`ci` and `person` claims bypass the queue per [R33](#8-ci-wins-and-user-claims) and [R37a](#8-ci-wins-and-user-claims), and `paused` holds resume ahead of the queue per [R36](#8-ci-wins-and-user-claims)).
2. **Fairness within a priority (`--source` round-robin):** Within the same priority level, `nova-reserve` schedules round-robin across distinct `--source` callers (`emma`, `rowan`, `stella`, `b1`, `sprint`, ...) rather than pure FIFO, so one caller submitting a burst of 20 requests never starves another peer at the same priority.
3. **Starvation freedom (head-of-line reservation):** When the winning head request of the highest waiting priority cannot yet fit, `nova-reserve` reserves accumulating capacity on its target candidate machine so smaller requests behind it cannot trickle past indefinitely and starve large (`cpu 16`) or `--exclusive` requests. A waiter whose `--wait` expires or whose connection closes leaves the queue cleanly.

**R18a [R].** **Nested leases (`NOVA_RESERVE_LEASE` + PPID fallback) and deadlock refusal.** Commands running under a lease inherit `NOVA_RESERVE_LEASE=<lease_id>` and `NOVA_RESERVE_HOST=<host>` (preserved across `nova-sandbox`, with a local PPID-tree walk fallback when an intermediate binary scrubs environment variables):
- If a nested `nova-reserve` call targets the **same host** (explicitly or by matching `NOVA_RESERVE_HOST`) and its needs fit within the ancestor lease's remaining un-subleased resources, `nova-reserve` grants a **sub-lease** immediately at **zero extra machine debit**. Self-deadlocks inside CI jobs, Makefiles, or card gates are impossible.
- If a nested call asks for **more** resources on the same host than its ancestor holds, or closes a wait cycle while holding resources on a full pool, `nova-reserve` refuses immediately with `RESERVE DEADLOCK` (exit `2`) naming the ancestor lease rather than hanging or overcommitting memory/devices.
- Independent cross-host requests (for example, a worker or friend holding on `b1` requesting an auxiliary test run on a `mac, metal` machine) execute as normal peer leases when acyclic.

**R19 [R].** Every lease and claim carries structured metadata:
- `--holder <bench|friend|ci|person|tool>`
- `--source <caller>` (defaults to authenticated user/host or friend name)
- `--consumer <card.attempt@epoch>` (optional; set by sprint bench workers and friends working a card)
- `--label <ci|claim|blocker|critical|fix|read|work|custom>`
- `--priority <blocker|critical|read|high|normal|low>` (default `normal`)
- `--why <text>`

**R20 [R].** `--exclusive` on a `--host <machine>` requests the machine's entire resource vector (`all cpu, all memory, all devices`), using head-of-line reservation ([R18](#6-leases-and-the-central-arbitrator)) so it never starves.

**R21 [G].** `nova-tools` renders no web dashboard of its own. It arbitrates leases and answers CLI/JSON queries and streams: `nova-config resources`, `nova-reserve status [--host M] [--json]`, and `nova-reserve subscribe` (live NDJSON state + diff stream). The visual picture is `nova-sprint`'s board ([10.6](#106-the-board)).

## 7. CPU units

**R22 [G].** CPU is counted in calibrated **CPU units**, not raw core or hardware-thread counts, because single-core performance and SMT scaling differ substantially across machines (`studio`, `vision`, `b1`, `hulk`, `mini-m5`).

**R23 [R].** A CPU unit lets a card, friend, or script state compute needs once (`cpu 4`) with portable meaning across the fleet while allowing each machine to translate held units into a local core count ([R28](#7-cpu-units)). It is a coarse scheduling unit, not a hard SLA.

**R24 [R].** One CPU unit equals one standard reference core ("about four ordinary cores" for `cpu 4`). Fast-core machines measure more CPU units than physical cores; slow-core or heavily SMT-contended machines measure fewer.

**R25 [G].** CPU units are positive integers (`>= 1`). There are no fractional units.

**R26 [R].** `nova-tools` measures a machine's CPU units via a deterministic built-in calibration benchmark (`nova-reserve calibrate`): a sustained (~10–15s) all-usable-core mix of compilation, compression, hashing, and parallel test-like work anchored to a fixed reference constant compiled into `nova-tools`. Hyperthreading and thermal power limits (`PL1`) are captured naturally inside the all-core run. `cpu_units = max(1, floor(score / anchor_constant))`.

**R27 [R].** Calibration runs explicitly via `nova-fleet` / `nova-reserve calibrate` when a machine is provisioned or re-calibrated (and on first `--local` standalone init if `/var/tmp/nova-reserve/capacity.json` is absent; never on temporary test directories, and refused on battery power on macOS). Calibration holds `--exclusive` on the machine while running; if a `--holder ci` job arrives mid-calibration, calibration aborts non-zero (`BENCHMARK INVALIDATED`) rather than recording a contended score. Usable `memory` (`GiB`) is measured at the same time from the workload slice limit (Linux cgroup v2 `user-<uid>.slice` `MemoryMax`, e.g. `85%` of physical RAM, or physical RAM where unbounded, rounded down, at least `1`).

**R28 [R].** Accounting uses CPU units ([R11](#5-what-work-needs)); local execution uses core counts:

    cores to use = max(1, floor(units_held * usable_cores / machine_cpu_units))

On Linux, `nova-reserve run` dynamically pins each unpaused lease (`sched_setaffinity`) to disjoint physical cores / SMT pairs across active leases on that host, overlapping only when `max(1, ...)` rounding across many `cpu 1` leases exceeds `usable_cores`. Everywhere, `NOVA_RESERVE_CPUS` and the build parallelism variables ([R17](#6-leases-and-the-central-arbitrator)) pass `cores to use` to compilers and test runners.

**R29 [R].** Rounding within one core is accepted by design.

**R30 [R].** Single-threaded work asks for `cpu 1` and receives at least one core. Dedicated `fast-core` routing is out of scope for v3.0.0.

## 8. CI wins and user claims

### 8.1 CI jobs

**R31 [G].** Every GitHub Actions runner scope on a machine has configured job needs (`cpu`, `memory`, devices) in `nova-config` and acquires a host-pinned lease (`--host <machine> --holder ci --label ci`) for each job.

**R32 [G].** Idle runners hold nothing. Resources are held strictly from `job-started.sh` until `job-completed.sh` (or runner worker exit), so 100% of machine capacity is usable by cards and friends whenever CI is idle.

**R33 [G].** **CI never waits for non-CI work.** A `--holder ci` request bypasses the wait queue ([R18](#6-leases-and-the-central-arbitrator)) and is granted immediately on its host:
- **Local fail-open (`3s`):** If `nova-reserve serve` is unreachable within `3s` during a control-host restart or network partition, `nova-reserve` on the runner host fails open locally (applying local CI priority and logging a warning) so CI jobs never fail from arbitrator downtime.

**R34 [G].** **Soft between-command pause (never freeze or kill in-flight commands).** When `--holder ci` arrives on machine `M` and total active holds would exceed `capacity(M, r)` for **any** resource `r`, `nova-reserve` marks enough non-CI leases on `M` as **`paused`**—lowest priority first, youngest lease first on ties:
- Running processes are **never** frozen with `SIGSTOP` / `cgroup.freeze` and **never** killed mid-command.
- Any command already in flight under a `paused` lease finishes naturally under background OS priority (`CPUWeight=1` + `SCHED_IDLE` on Linux, `taskpolicy -b` on macOS vs `CPUWeight=10000` on CI).
- At the next command/step boundary (`nova-reserve run` or worker step re-entry), a `paused` lease blocks inside `nova-reserve` until unpaused.
- While `paused`, a lease counts for `0` against `M`'s ledger capacity across its displaced resources, and retains its identity and priority for resumption.

**R35 [G].** On every machine `M` and for every resource `r`, active unpaused leases plus CI leases never exceed `capacity(M, r)`:

    sum(unpaused non-CI leases on M for r) + sum(CI leases on M for r) <= capacity(M, r)

**R36 [G].** **Paused holds resume first.** When a CI lease ends, `paused` leases on `M` are unpaused in strict priority order (oldest first on ties) as soon as their full resource vector fits again. While **any** lease on `M` is `paused`, no queued waiter ([R18](#6-leases-and-the-central-arbitrator)) and no new sprint deal ([R52](#104-the-deal)) may start on `M`.

**R37 [G].** By default CI may use up to 100% of a machine's resources.

**R38 [R].** A lease is either `held` or `paused`, and its requested resource vector never mutates after creation.

**R39 [DELETED].** Mid-command memory/device killing (`stopped` for CI) is deleted ([G], 2026-11-15). All resources obey [R11](#5-what-work-needs) and [R34](#8-ci-wins-and-user-claims) uniformly through OS priority + between-command pause.

**R39a [R].** CI does not pause CI. Runner scopes on machine `M` are validated in `nova-config` / `nova-fleet` so simultaneous jobs across all runners on `M` fit within `M`:

    sum over runner scopes s on M (runners_s * needs_s[r]) <= capacity(M, r)

**R39b [R].** Any wall-clock time a consumer or lease spends `paused` or waiting in `nova-reserve`'s queue is excluded from worker and sprint timeouts (`working_max`, `DeadlineAt`, per-step wall timeouts, and `landing_stall`). Any worker command terminated by an external infrastructure signal (`SIGTERM`, `SIGHUP`, `SIGINT`) or pre-hold `nova-reserve` error is classified as `fault` (retried without spending a card bounce).

Glenn: *"The design I want does not waste CPU while we are not running CI, so it does work, but then prioritizes CI ahead of work, when we do."*, *"pause only between commands seems good enough, when combined with CI runners have really high priority."*, *"unpausing cards are done, before new cards are dealt."*

### 8.2 Timed soft user claims (`nova-reserve claim`)

**R37a [R].** A person can reserve part or all of a machine (`nova-reserve claim --host <machine> [--needs <spec> | --exclusive] [--ttl 4h] --why "<reason>"`, `--holder person`):
- **Immediate soft effect:** The claim takes effect at once (`--ttl 4h` default, renewable or releasable early via `nova-reserve unclaim`). Available capacity on `<machine>` may temporarily go negative while already-running commands finish; running work is never killed, and **no new commands or leases** start on the claimed resources until active usage drains below the remaining unclaimed capacity.
- **Automatic expiry:** Every soft claim carries a bounded TTL (`4h` default, `24h` max without renewal) so a forgotten claim at the end of the day never stalls benches overnight.

## 9. The check

**R40 [G].** Each bench still samples its real OS CPU busy percentage (`cpu_peak`), reported lightweightly for health checking and `ok%` tracking; real CPU% is kept internal for coordinator/operator awareness (spotting unheld runaway processes or mis-sized briefs) and is not drawn on the board.

## 10. What changes in nova-sprint

`nova-sprint` becomes a **peer client and live subscriber** of `nova-reserve serve`. Tier, a friend's width, producer cards, streams, dependencies, review, and landing do not change **[G]**.

### 10.1 The card

**R41 [G, R].** `NEEDS-ROUTE:` is retired in v3.0.0 (`add` refuses `NEEDS-ROUTE:` with a pasteable `RESOURCES:` remedy). A card states what its `work` consumer needs with one header line before the first blank line:

    RESOURCES: cpu 4, memory 8, mac, metal

A card with no `RESOURCES:` line defaults to `cpu 1, memory 1` with zero tags ([R10](#5-what-work-needs)).

**R42 [G].** The coordinator chooses `RESOURCES:` for each card by judgement. **Reads are pure code and diff reviews [G]:** `read` consumers never execute `DONE-WHEN` or `STREAM GATE` commands (`work` consumers and CI own building and running tests; `read` inspects the diff against base, invariants, and test coverage).

**R43 [G].** Every `read` consumer always uses the light default read budget (`cpu 1, memory 1`, zero hardware/OS tags), regardless of the producer card's `RESOURCES:` line. `work` consumers use the producer card's `RESOURCES:` line.

**R44 [R].** `add` (and `check`) refuses a malformed `RESOURCES:` line, and refuses any card whose `(tier, tags, resources)` no single machine in `nova-config` (paired with an eligible bench or friend row serving the tier) can ever satisfy ([R12](#5-what-work-needs)).

### 10.2 The roster

**R45 [G].** A bench row has no `width`. Its capabilities (`tags` and measured `resources`) come from `nova-config`. A bench whose `cpu` or `memory` has not yet been calibrated (`0`) is marked held (`unmeasured resources in nova-config`).

**R46 [G].** `fleet_width_cap` is removed everywhere.

**R47 [G].** Each friend (`kin`) row keeps its `width` (how many consumer cards that AI works on concurrently). A friend's width does not constrain which fleet machine(s) the friend leases via `nova-reserve` ([10.5](#105-friends-and-workers-as-direct-peers)).

### 10.3 Live `nova-reserve` subscription (no shadow resource ledger)

**R48 [R].** **Delete the duplicate shadow ledger in `sprint.events`.** `nova-sprint` does **not** store per-machine resource holds in PostgreSQL (`sprint.events` / `SetRowLoad`) and does **not** poll `nova-reserve` via 30-second worker `beat`s:
- `nova-sprint run` maintains a live `nova-reserve subscribe` stream to `nova-reserve serve`, receiving sub-second state snapshots and lease/claim/pause diffs for every machine in the fleet.
- Bench `beat` carries only row liveness, served harness `tiers`, and the internal health `cpu_peak` sample ([R40](#9-the-check)). Cores, slots, load1, and serialized hold tables are gone from `beat`.

### 10.4 The deal

**R49 [G].** The dealer (`modules/steps`) chooses which open bench runs a bench consumer card, guided by the live `nova-reserve` subscription view and policy ranking.

**R50 [R].** The dealer deals a consumer `c` (`needs = DefaultReadNeeds` for `read`, `CardNeeds` for `work`) to an open bench `B` only when:
1. `B` has every tag in `needs`.
2. `B` has **no `paused` leases** (`any_paused(B) == false`, [R52](#104-the-deal)) and non-negative headroom after soft `person` claims ([R37a](#8-ci-wins-and-user-claims)).
3. For every resource `r` in `needs`:

       capacity(B, r) - used(B, r) >= needs[r]

   where `used(B, r)` combines `nova-reserve`'s live non-sprint usage/claims on `B` with every sprint consumer currently assigned to or winding down on `B` (including deals made earlier in the same tick, preventing double-dealing across the brief window before `nova-worker` acquires its first step lease):

       C_sprint(B) = WorkingConsumersOn(B) union SprintConsumersHoldingOn(B)
       used(B, r)  = non_sprint_held_and_claimed(B, r)
                   + sum_{k in C_sprint(B)} max(needs(k)[r], live_held(B, k, r))

**R51 [R].** Dealing a card assigns it to bench `B`; **`nova-worker` on `B` acquires the actual lease directly from `nova-reserve` as a peer client** (`--host B --holder bench --source B --consumer <id> --label <kind> --priority <prio>`). If a friend, person, or CI job took `B`'s room milliseconds before `nova-worker` asked, `nova-worker` waits cleanly in `nova-reserve`'s queue ([R18](#6-leases-and-the-central-arbitrator)) with its deadline clock paused ([R39b](#8-ci-wins-and-user-claims)) until room opens or the card is withdrawn.

**R51a [R].** Each worker command step (`RunHarness`, `seal`, `apply`, and each `DONE-WHEN` / `STREAM GATE` on `work` cards) acquires its lease from `nova-reserve` on `B`, serving as a clean between-command yield gate ([R34](#8-ci-wins-and-user-claims)). Inside each step, `NOVA_RESERVE_LEASE` and `NOVA_RESERVE_HOST` are set so inner build/test tools invoking `nova-reserve` on `B` run as zero-extra-debit sub-leases, and a card that legitimately needs an auxiliary lease on another machine can request one as a peer ([R18a](#6-leases-and-the-central-arbitrator)).

**R52 [G].** The dealer deals nothing new to bench `B` while any lease on `B` is `paused` ([R36](#8-ci-wins-and-user-claims)) or while a soft `person` claim leaves insufficient room.

**R53 [R].** Deal priority order is unchanged (`blocker > critical > read > high > normal > low`), and sprint preemption remains for `blocker` cards only: when no open bench with the blocker's tags has free room, preemption checks that `capacity(B, r) - non_sprint_held_and_claimed(B, r) >= blocker.needs[r]` and `!any_paused(B)` (since withdrawing sprint cards cannot free CI, friend, or `person`-claimed resources), then withdraws lower-priority sprint bench consumers on `B` with `fault` until the blocker fits. Friend-row blocker preemption is unchanged.

**R54 [R].** The sprint idle alarm averages row utilization across open rows:
- Open bench `B`: `min(1.0, active_and_claimed_cpu(B) / capacity_cpu(B))` from `nova-reserve` (counting sprint holds, CI holds, friend holds, tool holds, and active `person` claims so a fleet busy with CI or friend builds never triggers a false `idle` alarm).
- Open friend `F`: `working(F) / width(F)`.
The alarm is suppressed while any bench is `paused` or when all waiting/ready cards are blocked on unserved tags or claimed resources.

### 10.5 Friends and workers as direct peers

**R55 [G].** **Friends acquire resources directly from `nova-reserve`—with or without a sprint card.** A friend (`emma`, `rowan`, `stella`, `johnny`, `freddy`, `aurora`) never guesses a host and never goes through the sprint coordinator to get compute. It asks `nova-reserve` directly:

    nova-reserve hold --needs "cpu 8, memory 16, linux" --holder friend --source emma [--consumer <id>] --why "<task>"

`nova-reserve serve` arbitrates across all eligible machines (or queues the request in the central fleet wait queue if full), grants the lease atomically, prints `{lease_id, host}`, and holds the lease open for the lifetime of that `nova-reserve hold` process.

**R56 [G].** Once granted `{lease_id, host}`, the friend SSHes from its app/workstation host (`studio`, `macbook`) into `host` passing `NOVA_RESERVE_LEASE=<lease_id>` and `NOVA_RESERVE_HOST=<host>`. Commands run inside that SSH session inherit the lease as zero-extra-debit sub-leases ([R18a](#6-leases-and-the-central-arbitrator)). If a friend fans out parallel sub-tasks across multiple machines, each sub-task acquires its own peer lease from `nova-reserve` (`--source <friend>`).

**R57 [R].** Because the friend's `nova-reserve hold` connection stays open across all of its SSH commands on `host`, neither the sprint dealer nor another peer can steal its memory or device room between commands while its worktree is staged on `host`. When the friend finishes (or disconnects), closing `nova-reserve hold` frees `host` immediately.

**R58 [R].** Sprint cards are dealt to friends as in v2: by `tier` and the friend's `width` (`RESOURCES:` does not restrict which friend may take a card, because any friend can lease any eligible fleet machine via `nova-reserve`!).

### 10.6 The board

**R59 [G].** On the sprint dashboard (`modules/dash`), the **Fleet** section becomes **Resources**, fed live (sub-second) from `nova-sprint run`'s `nova-reserve subscribe` stream. Columns are `machine`, `status`, `cpu`, `memory`, and `tags`. Legacy columns `width`, `working/width`, `ready`, `load`, and visual `done` / `ok%` are removed from `Resources` (`ok%` stays internal for coordinator health checks).

**R60 [G].** `Resources` answers at a glance whether the fleet's compute and memory are being used and by what—showing **every** active hold and soft claim on every machine (`ci`, `claim`, `blocker`, `critical`, `fix`, `read`, `work`, and standalone `tool`/`friend` holds), whether or not a sprint is active.

**R61 [G].** Each machine's `cpu` and `memory` bars are segmented proportionally by work label (`ci`, `claim`, `blocker`, `critical`, `fix`, `read`, `work`), without cluttering the bar with individual holder names.

**R62 [G].** `ci` is rendered in steel blue / cyan (`#38bdf8`), distinct from the card severity palette, and `person` claims render as a distinct reserved band. **`paused` work is drawn visually behind / under the CI segment that displaced it** (`Glenn: "this is why we visualize resourcecs and what tehy are doing", "per-machine -- NOT cards."`).

**R63 [G].** The `load` and `ready` columns are removed (`ready` was always `0` under Deal-Is-Take).

**R64 [G].** The **Friends** table is **100% unchanged**: it continues to show each friend's consumer cards against that friend's `width`, `done`, and `ok%`, while the fleet resources those friends hold appear live in `Resources`.

### 10.7 Policy

**R65 [R].** Policy asks `width(row)` **only** for friend (`kin`) rows; bench rows have no width, and `limits()` no longer carries `fleet_width_cap`. Candidate benches passed to Starlark `rows()` are inherently capable (`jointRows` never filters benches through `a.Width`), and policy inputs populate `card.tags` / `card.resources` (before `unworded()` strips brief prose) alongside each candidate bench's `tags`, `resources`, and live `free` room.

### 10.8 Removed

- Bench `width`, `fleet_width_cap`, and `working/width`.
- Per-host isolated slot files (`/home/<bench>/.local/share/nova-reserve` and `slots` counts) and SSH fan-out reservation (`nova-fleet reserve`).
- Duplicate resource-hold state in `sprint.events` / `SetRowLoad` and 30-second resource polling via `beat`.
- `NEEDS-ROUTE:` in card briefs.
- `DONE-WHEN` and `STREAM GATE` execution on `read` consumers.
- Mid-command process freezing (`SIGSTOP` / `cgroup.freeze`) and mid-command memory killing (`R39`).
- Board `load` and `ready` columns, and visual `done`/`ok%` on `Resources`.

## 11. Settled design decisions (formerly open questions Q1–Q14)

Every open question from the initial draft is settled and incorporated into `R1`–`R65`:

- **Q1 (CI job holds):** `job-started.sh` spawns a detached `nova-reserve hold --host <self> --holder ci --label ci --needs "<scope_needs>"` client bound to `Runner.Worker` (watched via Linux `pidfd_open` / macOS `kqueue` and closed cleanly by `job-completed.sh`, with `3s` local fail-open if `nova-reserve serve` is unreachable). Scope needs live in `nova-config` ([R31](#8-ci-wins-and-user-claims)–[R33](#8-ci-wins-and-user-claims), [R39a](#8-ci-wins-and-user-claims)).
- **Q2 (Fleet-wide discovery and reservation):** Solved at the root by making `nova-reserve` client/server (`nova-reserve serve`). Any friend, worker, person, or script asks `nova-reserve` directly (`--needs ...` or `nova-reserve status`), arbitrated centrally in memory with zero racy check-then-SSH ([R14](#6-leases-and-the-central-arbitrator)–[R18](#6-leases-and-the-central-arbitrator), [R55](#105-friends-and-workers-as-direct-peers)).
- **Q3 (What `paused` does to processes):** Pause only between commands, combined with cross-slice OS priority (`system.slice` `CPUWeight=10000` vs `user.slice` `CPUWeight=1` + `SCHED_IDLE` on Linux; `taskpolicy -b` on macOS). Never freeze or kill running commands ([R17](#6-leases-and-the-central-arbitrator), [R34](#8-ci-wins-and-user-claims)).
- **Q4 (Reads):** Reads are pure code/diff reviews with zero gate execution and always use `cpu 1, memory 1` with zero tags ([R42](#101-the-card), [R43](#101-the-card)).
- **Q5 (Devices vs routes):** `NEEDS-ROUTE:` is retired. Shareable SDKs are `-sdk` tags (`ps5-sdk`); exclusive hardware is an amount-1 resource (`ps5 1`) ([R5](#4-tags-and-resources), [R41](#101-the-card)).
- **Q6 (Windows):** No native Windows benches and no `nova-reserve.exe`. Benches are Linux, macOS, or WSL2; WSL2 Windows runner hooks call `wsl.exe ... nova-reserve` ([R16](#6-leases-and-the-central-arbitrator)).
- **Q7 (`fast-core`):** Cut from v3.0.0 ([R30](#7-cpu-units)).
- **Q8 (OS and arch tags):** Auto-derived from `nova-config` (`darwin` normalized to `mac`) ([R1](#4-tags-and-resources)).
- **Q9 (Blocker preemption):** Keeps v2 withdraw (`fault`) for sprint bench/friend victims when non-sprint headroom allows the blocker to fit ([R53](#104-the-deal)).
- **Q10 (Board columns):** `Friends` stays 100% unchanged; `Resources` drops `ready`, `load`, `width`, and visual `done`/`ok%` ([R59](#106-the-board)–[R64](#106-the-board)).
- **Q11 (CI color):** Steel blue / cyan (`#38bdf8`), with `paused` work drawn behind CI ([R62](#106-the-board)).
- **Q12 (CI vs `--exclusive` / calibration):** Calibration runs before enabling runners and aborts (`BENCHMARK INVALIDATED`) if CI preempts it; interactive human reservations use timed soft claims (`nova-reserve claim`, [R27](#7-cpu-units), [R37a](#8-ci-wins-and-user-claims)).
- **Q13 (Consumer step holds vs whole-run holds):** Friends hold one connection-bound lease across their multi-command SSH session ([R56](#105-friends-and-workers-as-direct-peers)); bench `nova-worker` steps act as clean between-command CI yield gates while dealer `C_sprint(B)` accounting reserves the card's bench room across steps and `NOVA_RESERVE_LEASE` allows nested sub-leases inside every step ([R18a](#6-leases-and-the-central-arbitrator), [R50](#104-the-deal), [R51a](#104-the-deal)).
- **Q14 (Unified idle alarm):** Average of open bench CPU utilization (counting all active holds and claims) and open friend `working / width`, suppressed during pauses or unserved/claimed blocks ([R54](#104-the-deal)).

## 12. What has to hold

Every stateful protocol is modeled in TLA+ with faulted/reversed witnesses before code and bound to Go implementation traces in CI:

1. **Never over (`Safety`).** On every machine `M` and resource `r`, `sum(unpaused non-CI leases) + sum(CI leases) <= capacity(M, r)` ([R11](#5-what-work-needs), [R35](#8-ci-wins-and-user-claims)). Sub-leases ([R18a](#6-leases-and-the-central-arbitrator)) never exceed their ancestor lease's grant and add zero duplicate machine debit.
2. **CI never waits (`Immediate CI`).** Every `--holder ci` request on a properly sized host ([R39a](#8-ci-wins-and-user-claims)) transitions to `held` in one step without entering the wait queue ([R33](#8-ci-wins-and-user-claims)).
3. **Soft claims block new starts without killing running work.** While `sum(unpaused leases) + claimed_person > capacity(M, r)`, in-flight leases keep running, no new non-CI lease starts on `r`, and expired TTL claims clear automatically ([R37a](#8-ci-wins-and-user-claims)).
4. **Unpause before queue and deals.** While any lease on `M` is `paused`, zero queued waiters and zero new sprint deals are granted on `M`, and `paused` leases resume in priority order as soon as their full vector fits ([R36](#8-ci-wins-and-user-claims), [R52](#104-the-deal)).
5. **Fairness and no starvation.** Within each priority level, waiting `--source` callers advance round-robin, and head-of-line reservation guarantees large and `--exclusive` waiters are eventually granted once CI and soft claims subside ([R18](#6-leases-and-the-central-arbitrator)).
6. **Never-fit and cyclic nested asks fail fast.** Requests exceeding every machine's static capacity ([R12](#5-what-work-needs)) and cyclic/over-grant nested asks ([R18a](#6-leases-and-the-central-arbitrator)) are refused immediately and never block in the queue.
7. **Every lease ends; restarts drop no live lease.** Disconnected/dead clients expire within `15s`, and server restarts preserve reconnecting leases across the `15s` `reconnect` grace window before admitting new queued work ([R15](#6-leases-and-the-central-arbitrator)).
8. **No double deal.** Two sprint consumers dealt in the same tick never consume the same bench room ([R50](#104-the-deal)).

TLA+ models: `nova-reserve`'s central `Arbitrator.tla` (multi-host vectors, CI pause/unpause, soft claims with TTL, `--source` round-robin + head-of-line reservation, sub-leases/deadlock detection, and `reconnect` grace) in `nova-tools`; and `nova-sprint`'s `Steps.tla` (live resource room + `any_paused`/claim guard), `Policy.tla` (friend-only `width` + bench resource facts), and `Runner.tla` (between-command yield + `fault` classification on external kills).

## 13. Order of building

Bottom up, each layer verified with TLA+ models, `-race` unit/functional suites, and trace checks before the next layer builds on it:

### In `nova-tools` (standalone, zero dependency on `nova-sprint`)

1. **`nova-config` & calibration (`R1`–`R8`, `R22`–`R27`, `R39a`):** Machine `tags` and `resources` schema, automatic OS/arch tag derivation (`darwin` -> `mac`), tag/resource name disjointness, runner scope fit validation, and the built-in CPU/usable-slice-memory benchmark (`nova-reserve calibrate`).
2. **`nova-reserve` client/server arbitrator (`R9`–`R21`, `R28`–`R38`, `R37a`):** `Arbitrator.tla` + trace binding; `nova-reserve serve` (Tailscale TCP + `nova-secrets` auth, plus `--local` Unix-socket mode); `hold`, `run`, `claim`/`unclaim`, `status`, and `subscribe`; `5s`/`15s` connection keepalive and `15s` `reconnect` grace; `NOVA_RESERVE_LEASE` + PPID nested sub-leases and `RESERVE DEADLOCK` detection; `--source` round-robin + head-of-line reservation; Linux cgroup/affinity/SCHED_IDLE and macOS `taskpolicy -b` steering.
3. **Fleet rollout & CI runner hooks (`R31`–`R33`):** Provision `nova-reserve serve` via `nova-fleet`, wire Linux/macOS/WSL2 `job-started.sh` and `job-completed.sh` (`--holder ci` with `3s` local fail-open), and verify standalone friend/CLI/CI arbitration end-to-end without `nova-sprint`.

### In `nova-sprint` (peer client & subscriber of `nova-reserve`)

4. **Card `RESOURCES:` & roster (`R41`–`R47`):** Parse and validate `RESOURCES:`, retire `NEEDS-ROUTE:` and `fleet_width_cap`, enforce `DefaultReadNeeds` (`cpu 1, memory 1`, zero gates) on `read` consumers, and load machine tags/resources from `nova-config`.
5. **Live `nova-reserve subscribe`, dealer, policy, worker, and board (`R48`–`R65`):** Subscribe `nova-sprint run` to `nova-reserve serve` (removing any duplicate resource ledger from `sprint.events`/`beat`); wire dealer room (`R50`–`R54`) and friend-only `width` policy (`R65`); acquire peer leases in `nova-worker` with `PausedMillis` deadline exclusion (`R39b`); update friend playbooks to use `nova-reserve hold` directly (`R55`–`R58`); and render the live `Resources` board alongside the unchanged `Friends` table (`R59`–`R64`).

## 14. Next (`v3.1`)

Planned follow-ups once v3.0.0 is running cleanly in production (informed by Steward scheduler lessons):

1. **Per-machine GNU Make / Ninja / Cargo FIFO jobserver (`--jobserver-auth=fifo:...`):** Managed by `nova-reserve` on each host so running multi-file builds dynamically shed and reclaim compile worker tokens mid-build when CI arrives and leaves, without freezing processes.
2. **Hold-duration history and non-delaying backfill:** Record historical runtime distributions by `(label, source, command/stream)` so short queued tasks can backfill ahead of a waiting head-of-line reservation whenever estimated completion guarantees zero delay to the head waiter.
3. **Named resource presets in `nova-config`:** Optional ergonomic shorthand (for example, `rust-heavy`) expanding deterministically to explicit tag and resource vectors (`cpu 8, memory 16, linux`).

## 15. Not in v3.0.0

- **Coordinator-only resource gatekeeping.** `nova-reserve` serves every authenticated peer directly, with or without `nova-sprint`.
- **Mid-command process freezing (`SIGSTOP` / `cgroup.freeze`) or mid-command OOM/CI killing (`R39`).**
- **Native Windows benches or `nova-reserve.exe`.** Windows runs `nexus-client` only; server tools run on Linux, macOS, and WSL2.
- **`fast-core` single-thread core routing.**
- **Co-allocating one single command across multiple machines simultaneously** (friends and workflows may hold independent peer leases across machines, subject to [R18a](#6-leases-and-the-central-arbitrator)).
- **Deadline-driven auto-sizing** ("finish in two minutes" instead of explicit resource amounts).
- **Cloud bursting / renting machines outside `nova-config`.**
- **Capping how much of a machine CI may hold** ([R37](#8-ci-wins-and-user-claims)).
- **Landing changes.** Landing fixes ship independently in v2 and carry forward cleanly.
