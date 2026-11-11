# Layer 6: run, worker, verbs, the dashboard, and setup

Draft for Glenn, 2026-11-06 (Rowan), not approved. Choices inside the layer are Rowan's and
easy to overrule; Glenn is quoted only where the other designs quote him.

This layer is the processes that run layers 1 to 5 and the verbs that drive them. PostgreSQL
owns the state. `nova-sprint run` is the one chooser; every other process is a client of the
store through a verb. Whatever needs someone to act moves by push, by exec of `nova-bus
send`; nobody polls (CORRECT-USE, "There is ONE WAY").

## 1. The list for Glenn (bottom up: structure, guarantee, how checked)

1. **Chooser lock.** One `run` applies ticks; a second refuses, naming the holder. A PostgreSQL advisory lock on run's own session, plus the bus name `sprint` (nova-bus allows one hook per name); `Run.tla` and a functional test starting two.
2. **Reports as ops.** A row's report (`report`, `finish`, `read`) is one store op applied by the verb itself: its checks, then the card machine's plan for consumer and producer together, then `Apply`, one transaction, fenced by gen, its op id fixed by the consumer and gen so a repeat replays. Layer 2's conformance tests; the functional race against a running tick.
3. **The tick.** Every second: fold the events since the cursor (the reports included), call the layer 3 steps, merge their plans within the budget, apply one op, carry the rest. Unit tests on a fake clock and `store.Fake`; the load test at the bar.
4. **The pusher.** Every accepted event that needs someone is pushed per recipient per tick after commit; the cursor moves only after `nova-bus send` exits 0. `Run.tla`; functional tests with a real bus.
5. **Verbs and their prerequisite tables.** Each verb checks what must be true before it acts, refuses in one line with the fix, ends with `next:`, exits 0, 1 or 2. Table tests, one per refusal, exact text.
6. **`next`.** From the store, the bus and the machine, the one thing this name should do now. A table test over the seven questions of CORRECT-USE; the transcript in section 4 as a golden test.
7. **Judgements.** Opened by layer 3, pushed once per group, answered only by `judge`, expired never silently. Layer 1's `JudgementClosedOnce`; functional tests.
8. **`up`.** Setup as a resumable walk; no step before its prerequisites verify; each question asked once; keys never printed. `Setup.tla`; the cold-AI dogfood gate, twice.
9. **The dashboard.** v1's page over the shim of branch `v2-dash-shim`, fed from run's local copy each second, one poller however many viewers, public by pull with a cache. The shim's tests and screenshots; a traffic test.
10. **Backup and restore.** `pg_dump` of schema `sprint`, verified by rebuilding from the log; restore only into an empty schema with no chooser. Functional round trip.
11. **Release pin.** `check` refuses a nova-tools below the version the binary was built for. Unit test on version parsing; functional on the installed tools.

## 2. The run loop

**Start.** `run` is a service on one host, installed by `up` (step 6). It runs its
prerequisite table (section 3), then takes the chooser lock, `pg_try_advisory_lock` on a
connection held for the process's life. Held, it refuses, naming the holder from
`pg_stat_activity` (`application_name` is `nova-sprint run <host> <pid>`): `SPRINT REFUSED
run: run already chooses on studio (pid 4121, since 14:02): stop it there, or nova-sprint
show run`. Then it hooks in as `sprint`, which nova-bus requires before `sprint` can send
("name already hooked" releases the lock and refuses the same way). The hook is only a
wake-up: run never parses a message body; the sprint's pushes are receipts, so a reply to
`sprint` is never answered, nor is any other message: one log line each (verbs.BusAnswer,
directive item 45: answering a REPLY with a REPLY made a loop).
Then one cold `Snapshot`, and the ticks.

**Reports are ordinary ops.** A row's `report` (nova-worker's form), `finish` and `read` run
their prerequisite table, build the card machine's plan for the consumer and its producer
together (`Report` with `Finished`, `Broken` or `Faulted`; a read's `Report` moves only its
consumer, since `ReadYes` and `ReadFix` are policy's from all reads, in the tick) and `Apply`
it themselves: one transaction, fenced on both gens. The op id is fixed by the consumer and
its gen, so a repeat or a retry after an unknown outcome replays the stored receipt (layer
1). A refused report is not final at that gen (the producer can move while the consumer
does not): a retry applies a fresh plan under the next id of the refused one's chain
(`store.ConsumerRetryID`), and an accepted report at any link replays. The verb's wait is that receipt: it prints accepted or the refusal and exits. Nothing
reaches run over the bus; the tick sees each report in its next fold. A person's or the
coordinator's verbs (`add`, `priority`, `judge`, `hold`, `drop`, `edit`) apply the same way.
The DSN reaches only the verb's process, by `nova-secrets exec`, never a harness or agent.

**The tick**, once a second on the wall clock:

1. Check the lock connection is alive; if not, stop applying, log `LOCK LOST`, exit 2 (the service restarts it, and it takes the lock again or refuses).
2. Fold `Events(cursor)` into the local copy (incremental, never a snapshot per tick; layer 1, "Scale"): every report applied since the last tick arrives here.
3. Read presence once: `nova-bus who` (one exec). Every few ticks, the widths and tiers from nova-config (layer 1: width has one source).
4. Pass the copy, presence, the edge results finished since the last tick (CI status, git, landing; layer 4 runs them outside the tick) and the active policy to the layer 3 steps; they return plans.
5. Merge plans up to the budget, in the order policy gives (blocker first; LAYER-2's ladder); a plan past the budget is carried, never dropped, and goes first next tick.
6. Apply the merged plan as one op (`Kind: tick`). A refusal means a verb moved a fenced card between the fold and the apply; the receipt names the condition, the plans fenced on that card are put back, and the rest is applied again within the time line. The put-back plans are re-planned from the next fold.
7. Hand the receipt to the edges (landings, branch fast-forwards) and to the pusher.
8. Log the tick line.

**The budget** is policy's number (README; layer 5) and applies to the ops the steps emit:
moves per tick, and a time line (default 600 ms) past which no plan is merged that tick.
Reports are not under it; each is its own op, applied when the row runs its verb. The carry
is the engine's, FIFO: a burst of moves costs each a tick or two and is never reordered.

**Pushing.** A goroutine beside the tick reads the accepted events after each recipient's
push cursor (`push:<name>` in `sprint.cursors`) and sends one message per recipient per
tick, one line per item (always batch), by `nova-bus send --as sprint --to <name> <subject>
<body>`, and sets the cursor only after the send exits 0. A crash in between resends on
restart, except the take-over notice to the coordinator it unhooked (section 5, Taking the
seat), and every line is safe twice (`take` of a card already held prints the same brief).
Bodies are bounded: logs, diffs and findings travel as the command that shows them (LESSONS
4.4). No send, exec, git or HTTP call runs under the store's writer lock (LESSONS 3.23).

**What it logs**, to `~/.local/state/nova-sprint/run.log`, one line per tick, `TICK n=8812
seq=125044 took=131ms folded=212 moves=388/0 pushed=17 putback=0`, and when they happen:
`OVERRUN n=8813 took=1.42s`, `CARRY moves=1788`, `REFUSED op=<id> <reason>`, `PUSH FAILED
to=<name> <nova-bus's line>` (sent again next tick from the unmoved cursor; nova-bus itself
never retries), `LOCK LOST`, and the stitch block (section 7). The store gets no heartbeat.

**Restart with nothing lost.** Reports are in PostgreSQL when their verb's op commits; push
cursors trail confirmed sends; edge actions replay by op id (layer 4). A restarted run takes
the lock and one cold snapshot and pushes on from each cursor.

**`--shadow`** plans one tick on the live store without the lock, applies and sends nothing,
and prints the plan and whether layer 1 would accept it; it checks a new build (section 5).

**Cost per tick, stated.** Layer 1 measured at 5,000 producers, after round 15 (hetzner2,
2026-11-06): tick 119 ms median, 153 max; 132 median while 200 single-report ops a second
arrive, each waiting 2.1 ms median for the writer lock; incremental read 13 ms; a 2,000-report
burst 2.8 s. This layer adds `nova-bus who` (about 10 ms, an estimate) and pushes off the
tick's path. The risk this layer owns, unmeasured: reports landing between fold and apply
refuse the tick's fenced op; step 6's put-back keeps the rest moving, and the load test must
show the tick is not starved. **The bar**, measured by the load test before v2 is done:
thousands of producers, every worker reporting at once, the tick under one second at p99 with
put-backs re-planned within two ticks, the dashboard advancing every second.

## 3. The verbs

v2 keeps the verbs the six promises need. Every verb takes `--as <name>` (else
`NOVA_SPRINT_AS`), ends with `next:`, exits 0, 1 or 2, and refuses in one line, `SPRINT
REFUSED <verb>: <what is not true>: <the command that makes it true>`, naming any unknown
card, row, stream or sha (LESSONS 2.26). "Hooked" is presence in `nova-bus who` and a role
recorded by `hook` (below); its refusal prints the step that is missing.

| verb | arguments | refuses, and the fix it names | next it prints | exit |
|---|---|---|---|---|
| (none) | | runs `next --as <me>` | | 0 |
| `next` | `[--as <name>]` | never; a broken prerequisite is its answer | the one command (section 4) | 0; 2 if it cannot look |
| `help` | `[<verb>\|<group>]`, or `-h` | unknown verb: the index | `nova-sprint` | 0 |
| `hook` | `--as <name> --role coordinator\|worker` | the name not in `nova-bus who`: "hook in to the bus first: `NOVA_BUS_REDIS=<addr> nova-bus hook --as <name>` under your session's Monitor tool"; `coordinator` while another row holds the seat (the fix names that coordinator's `unhook`, or, only when the operator says so, taking the seat with `up --take-over --reason`); `worker` on a sprint with no commit identity: `identity` | `next --as <name>` | 0, 1 |
| `identity` | `[--name <git user.name> --email <git user.email> --reason <why>]` | coordinator only; none set and none given; a quote, backslash, `$`, backtick or angle bracket, or an email that is not one address; no reason | `check` | 0, 1 |
| `help playbook` | `<worker\|coordinator>` | an unknown role: the two roles | `nova-sprint hook --as <name> --role <role>` | 0, 1 |
| `check` | `[--integrity]` | prints every line; red (exit 1, or 2 when a read failed): fleet not green; nova-tools below the pin (above: a warning); `NOVA_PG_DSN`, `NOVA_BUS_REDIS` unset or unreachable; a stream whose repository or base cannot be read; an open judgement (one line per kind); nothing runs the sprint; ready producers and nothing dealt (with why); a landing waiting on a judgement, run or a hold | `next`, or the first red line's fix | 0, 1, 2 |
| `up` | `[--as <coordinator>] [--dash tailnet\|public=<machine>[:<port>]] [--services <a,b>] [--key <service>] [--run-on <host>] [--version <v>] [--take-over --reason "<why>"]` | the first unverified step, with its fix (section 5); another coordinator recorded (ask them to `unhook`, or, only when the operator says so, take the seat with `--take-over`, which needs nothing from them and the run tells them `TAKEN OVER`); `--take-over` without `--reason` | the next step, then `stream add` | 0, 1 |
| `stream add` | `<s> --repo <owner/name> [--base main] [--mode auto\|hand] [--forge github\|none]` | store not at schema: `up`; repo not readable: layer 4's line; stream exists: `show <s>`; with a forge, the group branch `sprint-group/<s>` cannot be created (a name git refuses, a branch at origin in its way, origin's branches unreadable, the push refused), naming the fix | `add --stream <s> --brief-file <f>` | 0, 1 |
| `add` | `--stream <s> --brief-file <f>... [--dry-run]`, or `--stream <s> --sentinel <id> --needs <a,b>` | unknown stream: `stream add`; a brief missing contract sections, by name; an unknown need; a cycle, naming the loop | `nothing; wait for the bus`, or `up` when no row is up | 0, 1 |
| `run` | `[--listen <addr>]` | `check` fails; another run chooses, naming host and pid; `sprint` hooked elsewhere | at start: `worker --as <row>` when no row is up | 0 on SIGTERM, 2 on lock lost |
| `worker` | `--as <row>` | not a host in nova-config's inventory: `nova-fleet join`; name hooked elsewhere; row serves no tier | `nothing; wait for the bus` | 0, 1 |
| `take` | `<card>@<gen> --as <row>` | not hooked; not dealt to this row at this gen: `show <card>`; at width: `finish` one first | `finish <card>@<gen> --head <sha>` or `read <card>@<gen> yes\|fix` | 0, 1 |
| `finish` | `<card>@<gen> --head <sha> --as <row>`, or `--failed --reason <why>` (no `--head`: the dealt branch's tip at origin is the head, unless it is the base or the prior head) | not hooked; not the holder, naming who; not 40 hex; not on the dealt branch at origin: `git push origin <branch>`; head equals base, or a prior head an earlier attempt finished (a head on origin is all it needs: no PR, no forge) | `nothing; wait for the bus` | 0, 1 |
| `read` | `<card>@<gen> yes\|fix --as <row> [--finding <text>]` | not hooked; the reader wrote the head; head moved: `show <card>`; `fix` with no finding | `nothing; wait for the bus` | 0, 1 |
| `release` | `<card>@<gen> --as <row> --reason <why>` (recorded `released: <why>`, no result: neither ok nor fail; the re-deal is a new attempt, no blame) | not the holder | `nothing; wait for the bus` | 0, 1 |
| `release` | `<card>@<gen> --as <row> --reason <why>` | not the holder | `nothing; wait for the bus` | 0, 1 |
| `beat` | `--load1 <loadavg> [--cpu-peak <percent>] --cores <n> --slots-used <n> --slots-total <n> --as <row>` | not hooked; a flag missing or not a number; slots used over the total | `nothing; wait for the bus` | 0, 1, 2 |
| `show` | `[<card>\|<stream>\|<row>\|run] [--stitch]` | unknown name | what `next` says for that name | 0, 1 |
| `land` | `<card> --reason <why> [--head <sha>]` (coordinator) | not the coordinator; no reason; not a merging producer; `--head` not its finished head; the merge refused (a conflict: merge it into the base yourself, push, land again), naming the fix | `nothing; wait for the bus` | 0, 1, 2 |
| `judge` | `[<id> <choice> --reason <why>]` | not the coordinator, or not hooked; judgement not open, naming who closed it; a choice it does not offer, listing them | the next open judgement, or `nothing; wait for the bus` | 0, 1 |
| `priority` | `<card> <level> --reason <why>` | a consumer id (`priority` is the producer's); a final card; a level outside `low normal high critical blocker` (`read` is a rung, not a level) | `nothing; wait for the bus` | 0, 1 |
| `edit` | `<card> --brief-file <f> --reason <why>` | a live consumer: `nova-bus send --as <me> --to <holder> ...` instead | `nothing; wait for the bus` | 0, 1 |
| `drop` | `<card>... --reason <why> [--waive <need> \| --relink <need>=<card>] [--dry-run]` | an open judgement on it: `judge <id> drop` (a live consumer is released by the drop itself) | `nothing; wait for the bus` | 0, 1 |
| `hold`, `unhold` | `<row\|stream>... --reason <why> [--dry-run]` | unknown name | `nothing; wait for the bus` | 0, 1 |
| `dash` | `--listen <addr> --pull <tailnet addr>` | no `--pull`; the pulled run not answering | the URL | 0 on SIGTERM |
| `backup` | `[--out <dir>]` | `pg_dump` missing or older than the server | `nothing` | 0, 1, 2 |
| `restore` | `<file>` | schema `sprint` not empty; a run holds the lock; the dump fails its rebuild check | `run` | 0, 1, 2 |
| `clear` | `--confirm <epoch> [--dry-run]` | wrong epoch; a live consumer, listing them; the backup it takes first failed | `stream add` | 0, 1, 2 |
| `policy` | layer 5's design | | | |
| `report` | `<card>@<gen> --result ok\|fail\|fault [--head <sha>] [--cost <usd>] --reason <why> --as <row>` (nova-worker's form of `finish`) | as `finish`; `fault` only for a harness, bench or route failure | `nothing; wait for the bus` | 0, 1 |

**What each verb needs from `up` (2026-11-07).** No verb is gated on `up`'s step; a verb
refuses only for a piece it needs and names that piece (`nova-sprint help <verb>` prints the
line as `up:`).

| verbs | need from `up` | without it |
|---|---|---|
| `help`, `version`, `up` | nothing; they run before `up` | |
| `check` | nothing; it names the first piece that is missing | |
| every other verb (`stop`, `start`, `hook`, `show`, `next` included) | the store `up` makes at its step 4 (an epoch), at any later step of `up` | `the sprint is not set up: nova-sprint up --as <coordinator>`; `next` answers it, exit 0 |
| every verb that reads the store | the sprint credential in the process | `this process has no sprint credential: run under nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...` |

A fresh store is stopped, its reason `never started`, until the coordinator runs `start`: the
run comes up on it and ticks nothing, the board shows `STOPPED never started`, `next` tells the
coordinator `nova-sprint start --as <coordinator> --reason "<why>"` once the streams need
nothing first, and tells a worker to wait. `start` on a running sprint and `stop` on a stopped
one change nothing and succeed (exit 0): `RUNNING already: the sprint is already running;
nothing to start`, next `nova-sprint next --as <me>`, and `STOPPED already: ...`,
next `nova-sprint start`.

`hook` does not run the bus hook: the bus hook (`nova-bus hook --as <name>` under the session's
Monitor) is the earlier step it requires, because every push of the sprint travels over the bus
(Glenn, 2026-11-06: "you have to be hooked in to the message bus FIRST before you hook into the
sprint"). `hook` is the one defined entry point where an AI is fed how to use the sprint: it
records the name and role in the row's session state in the store, which subscribes the name to
the sprint's pushes over the bus (deals, read asks, withdraws, landing results, and for the
coordinator every judgement), and prints `HOOKED <name> role=<role>`, the commit identity, the
line `playbook: nova-sprint help playbook <role>`, then its `next:` line. The playbook itself is
not printed on every hook (a hook follows every wrap-around; 23 to 53 KB each time, V2 fix list
item 25): `help playbook <role>` prints modules/playbooks/PLAYBOOK-WORKER.md for a worker, or
modules/playbooks/PLAYBOOK-WORKER.md followed by modules/playbooks/PLAYBOOK-COORDINATOR.md for the coordinator, who is a
worker too ("so they get both playbooks!", Glenn 2026-11-06), compiled into the binary from those
files. Until the hook has run, every acting verb refuses with `hook in first: nova-sprint hook
--as <name> --role worker` (or `coordinator` for a coordinator verb). The hook records the
session id `nova-bus who` shows for the name's bus hook (the row's `Session`; before 2026-11-11
an id the sprint minted, compared with nothing), and the hooked check compares it with who's on
every verb: after the bus hook is started again the refusal is the same `hook in first`, naming
the role the row has, and the hook replaces the old one in place (`PutRow` with `Replaces`:
seat, hold and hand kept) instead of refusing `already hooked in`. Model: `tla/Hook.tla`
(`NoStaleVerb`, `HandKept`; witnesses `HookAnySession`, `HookReplaceUnhooks`), bound to the
verbs by the walk in `modules/verbs/hook_trace_test.go` (`tla/make-traces.sh check hook`).
`TestPlaybookCommandsParse`
holds every `nova-sprint` command in the playbooks, CORRECT-USE, SETUP and CARD-CONTRACT to a
verb and its flags (LESSONS 6.7).

Every verb that can withdraw or fail other cards (`drop`, `hold` of a row, `clear`) takes
explicit ids, never "all", has `--dry-run`, and prints what it withdraws and which
dependants fail before it does it (LESSONS 6.5; LESSONS-PRIORITY-AND-MERGE D33). One verb
per thing (LESSONS 6.6): a judgement is answered only by `judge`, so the answers v1 spread
over `rework`, `return`, `accept`, `waive`, `rank`, `resume` and the rest are the choices a
judgement offers (`rework`, `drop`, `waive`, `relink`, `rescore`, `unhold`, `retry`,
`by-hand`). `check` is the one doctor. Owed to CORRECT-USE, a one-line edit (the coordinator
is making it): its "Notifications name their verb" example becomes `judge <id> <choice>`.

**v1 verbs dropped** (from `nova-sprint help` on v1), with what replaces them: `init install
units seat live adopt server-switch` by `up`; `tick start stop play selftest` by `run` (the
simulation becomes a test driver); `queue where view inbox handover machinery settings audit
reads held sentinels bases streams card needs log` by `next` and `show`; `accept rework
return redo continue ack answer wait resume promoted merge land landed verify-landed promote
ci merge-window` by the landing step and `judge`; `fleet friend reader lane collect gc
budget routes funded` by nova-config, the bus, policy and the worker; `rank move recut twin
tier brief relink defer unpin rebase` by `edit`, `drop`, `priority` and policy; `quack
preflight` by `add --dry-run`; `snapshot demo repair fsck doctor` by `backup` and `check
--integrity`; `cost stats rules set goal coordinator teardown remind` by the dashboard,
policy, `up` and `clear` (reminders deferred, section 12).

**The prerequisite chain**, as CORRECT-USE's table, with the verb that makes each step true:

```
nova-fleet check green, tools >= pin ......... nova-fleet up
  -> check ................................... nova-sprint check
    -> up: runner-packages, netwatch, store, coordinator hooked, run, dashboard, services, workers, first deal
      -> stream add <s>
        -> add --stream <s> (brief passes the contract, needs known, no cycle)
          -> a row hooked (worker --as <row>  |  nova-bus hook --as <kin>)
            -> run deals: DEAL pushed to the row
              -> take <id>@<gen>      (hooked; dealt to me at gen; room in width)
                -> finish <id>@<gen> --head <sha>   (holder; full sha; on origin; PR)
                  -> read <id>@<gen> yes|fix        (another hooked row; not the author)
                    -> landing (a yes at this head; CI green at this head; LANDING.md)
                      -> STREAM LANDED pushed to the coordinator when the stream is done
```

## 4. `next` and the first time

`next` answers CORRECT-USE's seven questions in order and prints the first that is not
settled, as one `NEXT` line and one `next:` line. Help is a short grouped index; the long
walkthrough is this transcript, and a test holds every command line in it to a verb that
exists (LESSONS 6.7). What a cold AI sees, from an empty fleet to its first landed card
(names are examples):

```
$ nova-sprint
nova-sprint deals cards to AIs over nova-bus, reads them, and lands them. It stands on nova-tools.
NEXT the fleet is not up: nova-fleet check reports tailscale, redis, postgres missing
next: nova-fleet check

$ nova-sprint                      # after nova-fleet up
NEXT nova-tools is up; the sprint is not set up
next: nova-sprint up --as rowan

$ nova-sprint up --as rowan
UP 1/10 nova-tools: fleet green; nova-bus, nova-fleet, nova-config, nova-secrets at or above the pin: VERIFIED
UP 2/10 runner-packages: the packages a card's gates need (nova-fleet runner-packages, nothing to change) on amd-1,amd-2,titan,captain: VERIFIED
UP 3/10 netwatch: a Linux bench that loses its network or the tailnet recovers by itself (a Mac is skipped) (nova-fleet netwatch, nothing to change) on amd-1,amd-2,titan,captain: VERIFIED
UP 4/10 store: schema sprint version 2 on studio: VERIFIED (made now)
UP 5/10 coordinator rowan: not hooked
SPRINT REFUSED up: rowan is not hooked in: run nova-bus hook --as rowan under your session's Monitor tool

# the AI hooks in and runs up again; steps 1 to 5 verify; up stops where the human chooses
UP 6/10 run: installed on studio; chooser lock held; sprint hooked: VERIFIED
UP 7/10 dashboard: not chosen
ASK the human: the dashboard on the tailnet (default), or public on a machine with a public address in nova-config?
next: nova-sprint up --dash tailnet      or      nova-sprint up --dash public=<machine>[:<port>]
# the human says tailnet; up --dash tailnet verifies 1 to 7 and stops again
UP 8/10 services: deepseek chosen; key not set
ASK the human to type the deepseek API key at the hidden prompt in a terminal they can see:
next: nova-sprint up --key deepseek
# the human types the key; up verifies 1 to 8 and goes on
UP 9/10 workers: amd-1, amd-2, titan, captain hooked and up: VERIFIED
UP 10/10 first deal: one work card and its read dealt and released within one tick: VERIFIED
UP DONE run on studio; dashboard http://studio.tail1234.ts.net:8080; 4 workers up; coordinator rowan
next: nova-sprint stream add <name> --repo <owner/name>

$ nova-sprint stream add docs --repo mas-bandwidth/example
STREAM ADDED docs repo=mas-bandwidth/example base=main branch=sprint/docs
next: nova-sprint add --stream docs --brief-file <file>
$ nova-sprint add --stream docs --brief-file cards/fix-typos.md
ADDED fix-typos stream=docs tier=flash priority=normal needs=-
next: nothing; wait for the bus

# pushed to the row (a worker here; for a kin the same message wakes its session)
DEAL card=fix-typos.w1 attempt=1 kind=work stream=docs epoch=1
run: nova-sprint take fix-typos.w1@0 --as amd-1
$ nova-sprint take fix-typos.w1@0 --as amd-1
TAKEN fix-typos.w1 repo=mas-bandwidth/example base=<40 hex> branch=sprint/docs/fix-typos
brief, done-when and where tests run follow, then:
next: nova-sprint finish fix-typos.w1@1 --head <sha> --as amd-1
$ nova-sprint finish fix-typos.w1@1 --head <40 hex> --as amd-1
FINISHED fix-typos.w1 head=<40 hex> applied at seq 2210
next: nothing; wait for the bus

# a read is dealt to another row; the landing gates and lands; the coordinator, who hears
# only of streams (LESSONS 6.9), is pushed when the stream's last card is final:
STREAM LANDED docs 1 landed; run: nova-sprint show docs --stitch
```

## 5. `up`: the setup walk

`up` is SETUP.md as a verb, resumable by construction: nothing records "step k done"; each
run re-verifies from step 1 and stops at the first that fails, so a step that broke since is
found again. An answer is stored and never asked again: the coordinator, the dashboard's
place and the services as policy data (layer 5's `kind data`), the keys in nova-secrets.
Each step's action is idempotent.

| step | does | verified when | asked |
|---|---|---|---|
| 1 nova-tools | nothing itself; prints the nova-tools walkthrough (`nova-fleet check`, the human's one paste of the Tailscale API token, `nova-fleet up`) | `nova-fleet check` exits 0; every tool's version at or above the pin | no |
| 2 runner-packages | runs nova-fleet's `runner-packages` play on every bench (`-l` the inventory's benches), with the play's defaults | the play in check mode (`--check`) reports `changed=0`, `unreachable=0` and `failed=0` for every bench in its PLAY RECAP; a play that fails is the refusal, in the play's own words, with its fix | no |
| 3 netwatch | runs nova-fleet's `netwatch` play on every bench the same way (a Mac is skipped by the play) | as step 2, for `netwatch` | no |
| 4 store | `Migrate`; the role `sprint` through nova-fleet's postgres play | `sprint.meta` at this binary's version and hash; the role owns the schema | no |
| 5 coordinator | records the name: the caller, the hooked coordinator (again after a take-over) | that name is in `nova-bus who` | the name, once (`--as`) |
| 6 run | installs the `run` service on the chosen host (default: this one) | the advisory lock is held (`pg_locks`) and `sprint` is in `nova-bus who` | the host, only with `--run-on` |
| 7 dashboard | installs `run --listen` on the tailnet, or `dash --pull` on the public host | `/healthz` 200 and `/api/sprint` at the store's epoch, fetched from this machine; for public, from the public address, with the cache headers | tailnet or public, once |
| 8 services | for each chosen service, the key at a hidden prompt, piped into `nova-secrets set <SERVICE>_API_KEY` on stdin, then the grants, seats, worker rows and service rows | one request to the service's model list through `nova-secrets exec`, judged by its exit code only | which services, once; each key, once |
| 9 workers | the sprint credential granted to each bench seat; one service record `sprint-worker-<machine>` per worker bench (every bench but the run machine) running `nova-sprint worker --as <machine>`, applied in one `nova-fleet services` play limited to those benches | each worker bench present on the bus and `up` in the store; the refusal names each that is not and its unit's last log line | no |
| 10 first deal | nothing: a fresh sprint is stopped until `start` and moves nothing, so up deals no card | every worker row hooked, `up` and dealable (cores set in nova-config; a bench without is held at width 0) | no |

**Taking the seat** (Glenn, 2026-11-10: "Add takeover to up"). The coordinator's seat is the
row hooked in with role `coordinator`, and `unhook` frees it: by that row, or by the
coordinator, which is that row. `up --as <you> --take-over --reason "<why>"` moves the seat
with nothing from that row, on the bus or gone; it is for when the operator says so, and the
refusal of a second coordinator says that. It refuses without `--reason`. Where `up` hooks the
caller in (after step 4, the store), one op logged under kind `take-over` with the reason as its
note clears the old coordinator's hook as `unhook` clears it, returns its hand to the queue
(each card released with its branch and head kept; a card that cannot be withdrawn in that op
is named and returned by the run's next tick), and hooks the caller in as coordinator, refused
if any hook changed since the seq it read (one coordinator). The run sends the old coordinator
`TAKEN OVER by=<you>: ...` with the reason and its `WITHDRAWN` lines, and the new coordinator a
`RETURNED` line per card; a failed send to the old coordinator is sent again each tick while
the run stays up, but a restart before it is sent may not send it. The walk then goes on, and
step 5 records the caller, the hooked coordinator, as the coordinator. With no coordinator
recorded, `--take-over` is the plain hook. No model covers the seat
(`docs/roadmap.sexp`, `coordinator-seat-model`): Setup.tla has the walk's steps, Store.tla the
PutRow write; the guard is unit-tested (`modules/verbs/takeover_test.go`).

**The key.** The prompt turns off echo on the human's terminal and reads one line. Without a
terminal (an AI's shell) `up --key` refuses and names the terminal it must be run in. The
key goes from the prompt to `nova-secrets set`'s stdin and nowhere else: never in argv, the
environment, a log, the store or the bus; a leaked key is rotated. The human types nothing
else for that service (SETUP section 3).

**Adopting a new build** is `up --version <v>`: install the new binary beside the old; `run
--shadow` with it against the live store (one tick planned, nothing applied); on a clean
shadow, swap the service; watch three ticks; on an `OVERRUN`, a crash, or a tick whose op is
refused, swap back to the old binary and print why (LESSONS 6.19). nova-sprint releases
separately from nova-tools and carries the minimum nova-tools version it needs; step 1 and
`check` refuse below it, naming `nova-fleet up` (SETUP, last paragraph).

**Backup and restore** (owed by layer 1, LESSONS 1.16). `backup` runs `pg_dump -n sprint
-Fc` as the `sprint` role into `sprint-e<epoch>-s<seq>.dump`, then checks it: restored into
a throwaway PostgreSQL (`initdb` in a temporary directory), the log is rebuilt and compared
with the snapshot at that seq; a dump that fails is deleted and reported, exit 1. `restore
<file>` refuses unless schema `sprint` is empty and no run holds the lock, restores, runs
the same check, and prints `next: nova-sprint run`. `clear`, the one op that leaves rows
behind, takes a backup first and refuses if it fails. The first real store's `up` takes one.

## 6. The dashboard

Glenn, 2026-11-06: "The dashboard design should
be identical to the v1 in visual, excluding the unimplemented stats part at bottom", and "it
should be exactly matching what we currently have, visually. no changes except i think you
added fail state." So v1's page (`index.html`, `app.js`, routes, JSON envelope) is carried as
is behind the shim on branch `v2-dash-shim`, matched against the live v1 board at the shim
README's widths, by eye and `tools/abshot`; any difference is a defect. `fail` is black, after
landed (Glenn, 2026-11-06, in the coordinator's ruling: "You can make fail black"); there is
no column for a card with a problem (2026-11-07: a held card is waiting); the stats panel is left off.

**Served by run.** `run --listen <addr>` rebuilds the view once a tick from the tick's own
local copy, adding no store read. A request is answered from the cached view, the copy
advances every tick whether or not anyone looks, and the stale alarm and `/healthz` 503 are
the shim's. Another host shows the board with `dash --pull` (below), never from the store.

**Public, with the cache.** A public host never holds the DSN. `dash --pull <tailnet addr>
--listen :443` fetches run's `/api/sprint` once a second over the tailnet and serves that
one gzipped body to everyone (`Cache-Control: max-age=1`, an ETag), the page as static
bytes, `/public.html` without the charts, a per-address request limit, and nothing that
writes. A thousand viewers cost one fetch a second.

**What it reads.** The one record, never a derived status (LESSONS 6.12): a row's pill is
down if down, else held if held, else up; read consumers are in the working counts;
sentinels are out of the ETA and the rates (LESSONS 6.15; both as built). The merge strip's
`stuck` and `ci red` cells, blank in the shim, are filled from layer 3's landing step
(LANDING step 5). The coordinator chip is filled from `up` step 5's record. Nothing is added or
removed unless Glenn asks (LESSONS 6.14).

**No "needs you" column.** The board stays an exact v1 match, and v1 has none. The
judgements queue is reached by `next` and by the bus push (section 7); the later
nativization may add a column on Glenn's word, and not otherwise.

**Nativization is the final step.** After the dogfood gates pass with the board matching,
the v1-shaped envelope gives way to a v2 view model, the adapter is deleted, and
`tools/abshot` shows no visual difference at any width. Not before: the shim is the check.

**Deferred, named:** control from a phone, iPad or watch over Tailscale (README). The
channel is the tailnet dashboard host, by Tailscale identity (whois): one page of open
judgements and one POST that runs `judge <id> <choice>` as that person. Only a judgement's
offered choices can be answered from it; nothing else is reachable. Built after v2 is done.

## 7. Judgements

A judgement is a row of `sprint.judgements` (layer 1), opened by a named layer 3 step in the
same op as the move that raised it.

- **Open.** The step writes it with its kind, `about`, the question, the exact `judge`
  command, a group key and the deadline policy states.
- **Pushed.** The pusher reads the open event after the coordinator's cursor and sends one
  message per group per tick:

  ```
  NEEDS YOU judgement=j-0412 kind=red about=docs/fix-typos due=15:40
  CI red at 9c41e0...: run nova-sprint show fix-typos --ci
  run: nova-sprint judge j-0412 rework --reason "<why>"   choices: rework | drop | retry
  ```

  A group of n is one message, `NEEDS YOU group=<key> size=<n> ids=<j-1,...>`, answered by
  one command whose `--expect <n>` refuses a group that changed. Pushed once; an open one
  is listed by `next` and `judge`, never pushed again by a timer.
- **Answered.** `judge <id> <choice> --reason <why>` closes it and applies the choice in one
  op, and pushes `ANSWERED judgement=<id> by=<actor> choice=<c>` to the coordinator when
  someone else answered. A second answer is refused naming the first.
- **Expired.** At its deadline the step expires it and applies policy's declared safe
  default for that kind, which never drops, accepts, merges or lands (LESSONS 5.7), and
  pushes `EXPIRED judgement=<id>: <why>`. A cause a step still detects is reopened in the
  same op as the next episode with the repeat count (`UNANSWERED n TIMES, ESCALATED: ...`,
  pushed as NEEDS YOU); one no step detects is recorded as cleared. A `held` judgement has no deadline by
  Glenn's ruling ("sits there until you review it").

Landings, a stream done, a red, a hold, a stall and an orphan reach the coordinator by the
same pusher. **The stitch.** When the last producer of a stream lands, run logs and pushes:

```
STREAM LANDED docs range=3f2a...9c41e0 cards=14 took=1h52m cost=$3.10
run: nova-sprint show docs --stitch
```

and `show docs --stitch` prints what the stitch pass needs in one place: the diff range
since the stream's base, each card with the row and model that wrote it, and every reader's
findings, by card. The pass itself is manual, over the bus (README, "The stitch pass").

**An orphan** (LESSONS 6.10): a `working` consumer whose holder is absent from `nova-bus who`
past policy's silence is a judgement (`orphan`: `release` or `wait`), pushed, never found.

## 8. Guarantees

1. At most one `run` applies tick ops at a time; a second refuses, naming the first. Even across a lost lock, a plan applies at most once (layer 1's gen fences).
2. Each tick applies at most the budget of the ops its steps emit; moves past it, and plans put back after a fence refusal, are carried in order and applied on a later tick, never dropped.
3. A row's report is one op applied by its own verb, consumer and producer together, fenced by gen: whole or not at all, applied at most once by its op id, and in the store the moment the verb prints its receipt.
4. Nothing is pushed before its op commits; every committed event that needs someone is pushed at least once; a push cursor never passes a send that did not exit 0.
5. No exec, git, HTTP or bus call runs while the store's writer lock is held.
6. A restart of `run` loses no state, report or push; run holds nothing that is not in the store.
7. Every verb runs its prerequisite table before acting; a refusal is one line with the fix and exit 1; a verb that could not run exits 2; nothing exits 0 on a failure.
8. Every successful verb's last line is `next: ...`.
9. A verb that acts as a name refuses until that name is hooked in.
10. Only `run` chooses: a verb applies the one move it was asked for, through the card machine, and never deals, asks a read or lands.
11. A judgement is pushed once per group, closed at most once, and never expires silently.
12. `up` never runs a step before the steps above it verify; each question is asked once; a key is never printed, logged or passed in argv or the environment.
13. The dashboard reads the store at most once a second per serving process, however many viewers; a public host holds no store credentials and serves nothing that writes.
14. `clear` takes a verified backup first; `restore` writes only into an empty schema while no run holds the lock.
15. `check` and `up` refuse a nova-tools below the pin.

## 9. The models

`tla/Run.tla` (Rowan's shape, for review). Variables: `lock` (none or one of two runs),
`alive` per run, `cards` (the gens of two cards), `applied` (op ids), `events`, `copy` (the
run's fold), `carry`, `tickApplied`, `cursor` and `sent` for one recipient. Actions:
`Report(c)` a row's verb applies its op, raising `c`'s gen; `Take(p)` take the lock if free;
`Crash(p)` drops the lock and the copy; `Fold(p)`; `Tick(p)` with the lock, apply up to
`Budget` of planned moves fenced on the folded gens; a refused fence puts back only the plans
on that card; `Push(p)` sends the events after the cursor; `SaveCursor(p)` after a send.
Invariants: `OneChooser`; `AppliedOnce`; `BudgetKept`; `PushAfterCommit`;
`CursorTrailsSent`; `NoStaleMove` (no tick move lands on a gen it did not plan against).
Liveness, with weak fairness on `Tick`, `Fold` and `Push` and reports eventually pausing:
every planned move is applied or re-planned; every event is sent. Reversed witnesses, each a
required violation: the lock not checked (two runs apply one plan without the fence); the
fence dropped (a tick move on a reported card); the cursor saved before the send (a lost
deal); the budget ignored; the carry dropped; a refusal that puts back every plan (the tick
starves while reports arrive). Run small: two runs, two cards, three reports, `Budget` 2.

`tla/Setup.tla`. Variables: `verified[1..8]`, `answers` (asked and stored), `asked` (a count
per question), `pc`, `printedKey`. Actions: `Walk` finds the first unverified step; `Do(k)`
only when every step below k verifies; `Verify(k)`; `Ask(q)` only when `q` has no stored
answer; `Regress(k)` the world breaks a step; `Crash` resets `pc`, keeps answers.
Invariants: `NeverAhead`, `AskedOnce`, `KeyNeverPrinted`. Liveness: once the world stops
regressing, `up` reaches done. Reversed witnesses: a step marked done without `Verify`;
answers lost on a crash (asked twice); resume skipping a step that regressed. TLC runs on a
bench; each counterexample is checked against the code by hand; the code cites the model.

## 10. Tests

**Unit** (no real time, sockets, network or outside service; the fake clock starts at 1000,
never 0): the tick against `store.Fake`, a fake `Bus` (`Send`, `Who`); budget, carry and
put-back; every row of the verb table, each refusal with its exact
text and exit code; `next` over every answer to its seven questions; the section 4
transcript as a golden test; the help index and every command line in this file and
CORRECT-USE held to a verb that exists; `up` over fake steps (order, resume, ask once, key
never in output); version and pin parsing; judgement push grouping.

**Functional** (each test starts its own PostgreSQL with `initdb` and its own Redis, uses
the real `nova-bus`, runs in parallel, and finishes in two minutes or moves to the load
tier): two runs, one refused; kill -9 of run mid-tick and mid-push, then every deal
delivered and no move applied twice; reports racing a running tick, every report and every
move applied once; a row's verbs end to end with the deal over the bus; `up` on a
throwaway store and bus, then again with nothing to do; backup, restore, rebuild equals
snapshot; the dashboard under a thousand requests a second with one store read a second;
`--shadow` against a live store changing nothing. A seeded simulation driver plays the
outside world (fail, fix, stuck, down, up, red, flap) through the real verbs (LESSONS 6.20),
and the same driver at thousands of producers is the load test that measures the bar in
section 2.

**Dogfood gates.** By the v2.0.0 release gate (Glenn, 2026-11-06), v2.0.0 ships when every
layer is built, modelled, cold-read and green and gate 1 has happened at least once (one card
dealt, worked, read, merged and shown landed on the live board); gates 2 to 5 are 2.0.x point
releases, each one a gate met.

1. Our own sprint set up with `up` and run with the board live, Rowan and the kin as rows (SETUP, Glenn's gate in his words).
2. A cold AI with no memory of this project takes a fresh fleet of VMs from zero to a running sprint with a dashboard, from docs/README.md and the tool's help alone, and then does it again.
3. The friends adopt it: each kin hooks in and works cards with `take` and `finish` on their own harness.
4. 10/10 from every friend and model on the six promises, read cold.
5. Vanilla against sprint: the same cards, chosen before either run, done once in one long session and once through the sprint; wall clock and cost for each, printed by `show <stream>` and recorded. The bar is Glenn's to set; this layer only measures.

## 11. Size

About 6,000 lines of new Go plus the shim's 800 (measured on `v2-dash-shim`, tests
excluded): verbs, their prerequisite tables and help 2,000 (the report verbs apply their own
plans); `next` 250; the tick, lock, put-back, budget and log 600; the pusher and judgements
500; `up` with adoption 900; serving and `--pull` 300; backup and restore 250; `check` with
the pin and `--integrity` 350; glue 850. An estimate from the verb count, not a measurement.
The bar is the minimal code for what the tool does, no bloat; the point releases delete what
is not essential (LESSONS "How we work" 4).

## 12. Lessons placed

Section 6 of LESSONS.md, by line:

- 6.1, 6.2, 6.3, 6.8: the verb table, exit codes, "hooked" and `next` (sections 3, 4).
- 6.4: `up` step 8, one work and one read dealt within a tick; `run` refuses on a failed `check`.
- 6.5: explicit ids, `--dry-run`, and what it withdraws, on `drop`, `hold` and `clear`.
- 6.6: `judge` is the one answer path, `check` the one doctor, nova-bus the one channel.
- 6.7: the short index, and the test that holds every command line here to the help.
- 6.9: pushes and `next` count producers; a read is told only to its reader.
- 6.10: the `orphan` judgement (section 7).
- 6.11: width only from nova-config; nova-worker takes only when it starts a run, so
  `working` is work running; a kin's `next` shows its working count against its width.
- 6.12, 6.13, 6.15: as built in the shim, and kept by section 6.
- 6.14: nothing added to the board; no "needs you" column without Glenn's word.
- 6.16: the merge strip's stuck and CI cells filled from layer 3.
- 6.17: `dash --pull` and the cache.
- 6.18: `up` and dogfood gate 2.
- 6.19: `run --shadow`, and adoption with rollback in `up --version`.
- 6.20: the seeded simulation driver, a test tool, not a verb.
- 6.21: `check --integrity`, read-only: every pushed head recorded, every landed producer on
  its base (`git merge-base --is-ancestor`), each worker's holdings equal to the store's.
- 6.22: owed to layer 4 (nova-worker removes a done consumer's worktree after its report is
  applied); `check --integrity` lists a dirty one and never removes it.
- 6.23: deferred: timers and reminders. Judgement deadlines cover the waits the six promises
  need; a general reminder needs a table.

"How we work", by line:

- W.1, W.2: dogfood runs only through these verbs; a nova-sprint bug found there goes to an
  owed list fixed outside the sprint, never a card, and no watcher or side loop is added.
- W.3: v2 runs only on its own store until the gates pass, never in v1's live sprint.
- W.4, W.5: section 11; a fix deletes its workaround and is made at the source (a slow bus
  send in nova-bus, never a second channel).
- W.6: the verb table, the transcript and the setup table are the cases, written as table
  tests before the code.
- W.7, W.8, W.10, W.11, W.14, W.20: already placed; kept in sections 9 and 10.
- W.9: the fake clock starts at 1000.
- W.12: an `OVERRUN` in the load test gets a profile before any change.
- W.13: each dogfood gate keeps an attempts record; the third attempt at one gate is
  reported to Glenn as stuck.
- W.15: the v1 verbs dropped above are never ported; nothing live in v1 is deleted for v2.
- W.16: cold readers and audits report; Rowan cuts the fixes.
- W.17: the hooked-before-acting refusal and the chooser lock carry a DO NOT REMOVE comment
  and a test; neither is weakened without Glenn.
- W.18: this file quotes Glenn only where the other designs do; the rest is marked Rowan's.
- W.19: each dogfood row and child agent works in its own directory, cheapest fitting model.

## Decided inside the layer (Rowan, 2026-11-06), easy to overrule

The put-back of fenced plans after a refusal; one push per recipient per tick; setup answers
as policy data; `up` keeping no progress record; the public dashboard pulling
with no DSN; `judge` with choices in place of v1's answer verbs.

## Built: check says the sprint's health (stitch, 2026-11-07)

`modules/verbs/check.go`, `check_sprint.go`, `check_width.go`, `check_landing.go`
(item 43). `check` collected the plumbing only and refused at its first
failure. It now prints one line per thing checked (`CHECK`, `CHECK WARN`,
`CHECK RED <what>: <fix>`), every line even when an early one is red, and
ends with `next:` when nothing is red or with one `SPRINT REFUSED check`
line naming the first red's fix (exit 2 when a red line is a read that
failed, else 1). With the store at schema it reads one snapshot and adds:
each stream's repository and base through the git edge at its clone address
(`Origin.Tip`, one `git ls-remote` each, no clone, every stream at once); the open judgements, one line
per kind; who holds the chooser lock and whether the name `sprint` is on the
bus; the width through the steps' own view (`steps.Build`: `Open`,
`Working`, `Live`) and the roster at policy's cap, red when producers are
ready and none is dealt, naming the causes the deal meets (stopped, no run,
no open row, held streams, streams with no base head or an open engine
judgement about them, tiers no open row serves, the steps' idle judgement,
and, when no machine cause applies, the count of ready producers with none);
and one line per stream with producers merging, from what the store records
(a judgement about the stream, its base or the producer; run; the hold;
mode hand or no forge), else "waits for its landing PR's CI" (rung 1 the
card's branch, rung 2 `sprint-group/<s>`; since 2026-11-07 check reads no
branch at origin for it). The landing line reads no in-flight state
(run keeps it in memory), so "how long" is the judgement's open time when a
stall judgement says it, and nothing otherwise. Upward the pin: lower is
red, higher a warning that exits 0. Unit tests (`modules/verbs/check_test.go`): the
all-green output line for line, and each red line with fakes. Not measured:
check's time on a fleet with 25 streams (25 ls-remotes in parallel; a guess
of 1 to 3 s over the network).

## Built: foundation checkpoint (2026-11-06)

This is a checkpoint, not a completed layer. `run/` contains the chooser session, an event-folding tick seam, a scheduler, notification batching, and the run-fed dashboard adapter; the actual layer 3–5 adapters, public verb/run CLI integration, setup, backup/restore parser, integrated load gate and dogfood remain open.

- The chooser takes the PostgreSQL advisory lock on its dedicated session before the `sprint` hook. It checks both before applying; after observed lock loss it stops. The check and a store `Apply` on another session are not atomic: a loss between them can duplicate work or pushes. Store conditions and generations protect the state, and pushes are at least once. This is the weaker chooser guarantee ruled for this layer; `tla/Run.tla` models it. The real hook adapter is wired and tested against an isolated bus; the integrated crash gate remains open.
- Layer 3 owns ordering, budget, packing and carry. Its provisional consumed seam returns ordered ops, effects and answers with `After` op IDs, and carried edge results. Run applies the ops in order and emits an effect only after all of its named ops accept; refused op IDs and edge results return for replanning. An unknown outcome pins the op until Replay or a same-ID retry. Edge facts are assumed re-queryable by stable key after restart; layer 4 must validate that assumption. The real layer 3 and 4 interfaces are not wired yet.
- The scheduler reads nova-bus presence once per tick, checks the `sprint` name is present and observes its hook process for exit; `who` does not expose a lease token, so this is not proof that the live process owns that name after a disconnect. The checkpoint still passes folded store rows to its provisional steps seam. Rowan’s later ruling makes nova-config the authoritative machine and friend roster; store round 22 and roster integration are pending, so this checkpoint is not compliant with that later ruling.
- Policy is loaded by snapshot version. The run log records the planned version; the event's Policy is the version active at commit. The op's Note remains the layer 3 group JSON. There is no atomic policy cutover. The store's conditions and generation checks still apply.
- The pusher sends by recipient, subject and judgement group, requires a matching recipient and bus message ID, and advances its cursor only after all batches in the prefix succeed. A message is bounded to 4 KB and 20 card lines, with `and N more: nova-sprint next --as <row>` for overflow. The no-loss interpretation of this summary depends on the future `next` enumerating every outstanding obligation; that integration is not yet tested. The key is `push:<name>` when it fits 128 bytes; longer names use `push-hash:<full SHA-256>` (Rowan's ruling), avoiding a store change.
- For terminal stream notices, the pusher reconstructs a recipient's historical cursor once in bounded pages and folds forward. It renders the notice at the event's sequence, not from the latest state: all landed gives `STREAM LANDED`; failures give `STREAM DONE WITH FAILURES` with landed/failed counts and bounded failed IDs. The stitch detail is requested by `show <stream> --stitch` rather than embedded in the bounded push. Once Clear is seen, old-epoch pending notices are cancelled and the recipient gets `epoch <n> cleared: <k> pending notices dropped (some may have reached you); run nova-sprint next`. This is the explicit exception to individual delivery; partial sends can make the count include a notice already received.
- Run's dashboard adapter publishes only completed event folds, including successful idle folds; a failed fold does not refresh the clock. The no-viewer monitor raises the existing stale alarm without another store read. The page assets are unchanged; the baseline golden's two build hashes were corrected separately for the already-merged page.
- Restore's chooser-lock primitive opens one session, uses store `RestoreBegin` and `RestoreEnd` inside one transaction, preserves an existing empty owned schema, and passes parser-verified individual SQL statements to that same session. Its extended-protocol guard rejects multiple commands, but the parser must validate dump grammar, reject psql directives and all transaction control; the leading-word check is not a parser. The verified dump parser and an isolated populated backup/verify/restore round trip are built and tested. Full run and setup integration is still open; this primitive alone is not a complete deployment.

- The service runs the scheduler, push worker and dashboard from the folded local view, with graceful HTTP shutdown. The public pull cache fetches once a second, serves cached JSON with gzip and ETag, rejects writes, validates the upstream envelope and limits requests by address. `nova-sprint dash` and the existing dashboard binary support the cache; the public CLI dispatches the available verbs, while `run` and `worker` still await their real layer adapters. The hidden bus callback was exercised through a real isolated hook and correlated reply.
- Stream settings are recovered from `stream:<name>` policy-data events through the cold snapshot sequence, then updated with each event fold. Steps receive a copied map for current streams. The shadow seam plans without the chooser or effects and validates ordered ops against `store.NewFakeFrom`; it reports that concurrent changes and IDs used before the snapshot are not modelled. The actual layer 3 adapter and CLI entry are still pending.

- The local manual-test seam `modules/run/filestore` persists a Fake snapshot and event history in a file with per-operation locking and a separate chooser lease. It is explicitly a test store, not PostgreSQL or a power-loss durability substitute; the fake CLI can simulate setup, store verbs and a verified file backup/clear. The planner-driven terminal lifecycle is not yet wired, and fake restore remains unavailable.

- The CLI keeps help and version independent of the store, uses the canonical sprint credential explicitly and a passwordless sprint-role DSN for store access, and never exports it to unrelated tool commands. `check --history` uses the store owner’s read-only repeatable-read validator; `check --integrity` refuses while its external comparisons remain unavailable. Dirty or unknown builds do not claim a commit-derived identity.
- Setup has a two-phase, separately leased continuation: the non-secret wrapper launches an admin-only child, receives only a typed ID over a private channel after lease release, then launches a sprint-only child that rechecks the persisted epoch/actor/arguments before and under a newly acquired lease. The constructor uses actual inventory/service records and isolated config credentials; remote run is deferred. Public dashboard installation still needs the owner’s fleet installer interface and remains required for 2.0.0. No live setup or deployment is claimed.
- Remaining consumed dependencies include the actual layer 3 planner and roster, layer 4 worker/edges and its receipt-based machine CLI, and layer 5 policy evaluator/activator/catalog. Production repository and brief verification, report head/fault verification, judgement planning, stitch/CI views, workload setup and external integrity comparison remain unwired. Handoff/model trace fixtures are scoped evidence, not a final integrated-head lock.

**Historical foundation measurement (before the later CLI checkpoint):** on reserved amd-1, Go 1.26.10, PostgreSQL 18.6 and Redis 8.10.2, focused `run` and dashboard race tests passed, including isolated PostgreSQL, a real nova-bus/Redis receipt by ID and body, and stale/recovery checks. The broad build, vet and `go test -race ./...` passed again after the store round 20 merge; the earlier inherited-ledger failure and the corrected gates are preserved. A bounded loopback burst served 1,000 requests from 40 source addresses in 21.95 ms with one initial upstream fetch; this is not a sustained or integrated load gate. No layer 6 p99 load measurement, real layer integration, cold read, production deployment or dogfood is claimed here. The logs are retained in the validation workspace; later gates must run on the exact integrated head.

## Built: the run on a fresh store, and the verbs mid-up (2026-11-07)

- A fresh store is stopped "never started" (layer 1's Built entry of the same date). The run
  comes up on it and ticks nothing until `start`: `modules/run/fresh_test.go` ticks a fresh Fake and
  checks no sequence change, no ask, no send and no row moved, then one deal after `start`.
  The dashboard shows `machine: STOPPED never started` (`modules/dash/stop_test.go`).
- No verb is gated on `up`'s step (section 3's matrix). The field refusal of `stop` while `up`
  was at step 7 was the missing credential reported as "not set up"; that is now its own
  refusal, `this process has no sprint credential` with the `nova-secrets exec` line as its fix
  (`verbs.NoCredential`), from `open.go` and the doctor's schema check alike. "Not set up" now
  means only a store without the schema. `modules/verbs/fresh_test.go` runs show, next, hook, stop,
  start and stop on a store as `up` leaves it before step 8, and each verb before the store
  exists.
- `start` on a running sprint is a success that changes nothing and names `next`; `stop` on a
  stopped one names `start`. `next` on a stopped sprint with nothing owed tells the coordinator
  to start it and a worker to wait; a card in hand or an open judgement still comes first.
- `up`'s step 8 smoke needs a running sprint, so it needs `start` first; its refusal should name
  `nova-sprint start` (owed to the setup steps' change).
- `tla/Run.tla` gains `started` (set by Start; Init has `started = ~InitiallyStopped`) and the
  invariant `NeverStartedMovesNothing`: before the first start no op, edge, push or tick has
  applied. The witness `RunFreshRunning` (Init running though the store is fresh) violates it.
  The generated cfgs now also check `NoTickWhileStopped`, which the generator had dropped.
  `RunTrace.cfg` sets `InitiallyStopped = TRUE`, and `TestEmitRunTrace` starts from the fresh
  store: one stopped tick, then Start, in all three trace modes.
- `tla/SetupRun.tla` puts `up`'s walk (Setup.tla, unchanged) beside the sprint's stop and the
  run's ticks: step 2 makes the store stopped "never started", the run ticks from step 4 at any
  later step, and start and stop work at any step. `NothingMovesUnstarted` holds;
  `SetupRunFreshRunning` (the fresh store running) violates it; three reach checks show the run
  up on a fresh store, a stop and work moving, each before the walk is done.
### Built: up steps 6 to 8 on a real fleet, the role password (stitch, 2026-11-07)

- `cmd/nova-sprint/setup_workload.go` is the production `verbs.SetupWorkload`; until it, every
  step from 6 refused "worker setup is unavailable in this build". Step 6: `none` verifies; a
  chosen service's key is proven by its model probe, and nothing else is installed at 6 (the
  provider keys are opened from each bench's own seat; granting them from the control seat
  would put a name in both a seat's file and its grants, which `nova-secrets exec` refuses).
  Step 7: after `Role.GrantSeat` for every seat, one record `sprint-worker-<machine>` per worker
  bench: argv `nova-sprint worker --as <machine> --work-clone .local/share/nova-sprint/work
  --coordinator-hosts <run machine> --harness opencode --root .local/state/nova-worker/<machine>
  --state-dir .local/state` (built in one function, `workerArgv`; directories under the bench
  login's home, which `nova-sprint worker` now resolves there), seat the machine's, keys
  `NOVA_SPRINT_PG_PASSWORD`, `NOVA_BUS_PASSWORD` (when the bus has a user, as sprint-run),
  `DEEPSEEK_API_KEY`, `INCEPTION_API_KEY`, `OPENCODE_API_KEY`, `OPENROUTER_API_KEY` (Ultramarine left out, not a fit), env `NOVA_PG_DSN`,
  `NOVA_BUS_REDIS`, `NOVA_BUS_USER` as sprint-run's. All are applied in one play (`nova-fleet
  services -- -l <benches> -e nova_service_only=<records>`), not one per machine. Every bench
  gets a worker, the run machine too when it has the bench role (merged with the worker's
  change of the same night, which dropped its coordinator-host refusal); the UP DONE count is of
  worker benches. The record's argv carries the worker's per-repo roots (`--work-root`) and its
  provider routes (`--providers`: DeepSeek, Inception, OpenCode Zen and OpenRouter; Ultramarine left out, not a fit). `harness` is opencode because nova-config's machines carry
  no harness field.
- Step 7 verifies from one inventory read, one snapshot and one `nova-bus who`
  (`ExecEnvironment.Present`); a not-ready step is a `verbs.WorkloadNotReady`: up acts once, then
  refuses naming each bench and its unit's last log line, the logs read over the inventory's
  pinned ssh concurrently.
- Step 8 is not the smoke deal this section's table first described: section 37's fresh store
  is stopped until `start`, so a card dealt during up would be the sprint moving before it was
  started. Step 8 verifies every worker row is dealable when the sprint starts (hooked, up,
  cores set); the run's first tick after `start` is the first deal.
- `productionSetup.Completion` forwards the configured runtime's (section 40); a compile-time
  assertion covers every seam the engine type-asserts (`Setup`, `SetupHandoff`,
  `SetupCompletion`, `SetupLock`) and the workload.
- The public board on a bench: the refusal is gone (Glenn, 2026-11-07); a bench serving the
  pull-only board keeps its seat for its worker. The run machine still never serves it.
- Defect 37, the role's password: step 2's verify and the role child prove nova_sprint's
  password with a SCRAM login (pgx `require_auth=scram-sha-256`); where pg_hba trusts the
  connection (a wrong password logs in) the stored `pg_authid` verifier is recomputed from the
  seat's value. Not proven, the password is set from the seat (ALTER ROLE with a SCRAM
  verifier) and proven again; the refusal names the set or the verify. Where the admin cannot
  read `pg_authid` on a trust host, the password is set on every up: nothing can observe it,
  and a missing one cannot survive that. The stale-secret refusal ("delete the stale
  NOVA_SPRINT_PG_PASSWORD") is gone: the seat's value is the one every service reads.
- Unit tests: a fake fleet records every argv (`setup_workload_test.go`), a fake PostgreSQL
  for the password (`setup_runtime_role_password_test.go`), and the steps-7/8 binding to
  `tla/Setup.tla` over every state of a two-bench fleet. The functional role tests
  (`TestFunctionalSprintRolePasswordOnATrustHost` among them) were changed but not run here;
  they run on a bench.

## Built: the landing to intent rule 32 (stitch, 2026-11-07, branch `v2-stitch-w2L`)

- `land <card> --reason <why> [--head <sha>]` (`modules/verbs/land.go`, coordinator):
  the hand merge of a merging producer's own head through `verbs.Lander`
  (the host's gh identity), recorded `card.Landed{Head, Merge, Reason: "by
  hand: <why>"}` in one op that also answers its open land judgement `land`.
  `judge land.<card>.<n> land` runs the same path; `fix` sends `card.Red`
  with the reason as the finding; `retry` is the answer alone.
- `stream add` creates `sprint-group/<s>` at the base tip for a stream with a
  forge (one call, `ensureGroup`), refusing with the fix when it cannot;
  `finish` checks only the head at origin (the group-branch containment
  check and `Origin.Contains` are gone); `take`'s `stream_ref` is the base
  branch; `show --stitch`'s diff range is `origin/<base>..origin/sprint-group/<s>`.
- `release` records the reason `released: <why>` with an empty result
  (`verbs.ReleasedPrefix`; no schema change): neither ok nor fail.
- Run (`modules/run/edges.go`): LANDING.md "The ladder, as built"; `modules/run/asker.go`
  passes policy's `ci_wait`. `cmd`: the forge and git act as the host's gh
  login (`GH_GATE_TOKEN` optional), `GH_GATE_TOKEN` is no longer a key the
  sprint-run service needs, and the lander is wired into the engine.
- Tests: `modules/verbs/land_test.go` (`TestLandByHand`, `TestLandRefusals`,
  `TestJudgeLandAnswers`, `TestStreamAddCreatesTheGroupBranch`),
  `TestReleaseIsNoBlame`, the verb list and the role matrix with `land`.
## Built: admission (stitch w2A, 2026-11-07; intent rule 34, defect 49)

What can be found at add is found at add, each refusal with one fix. The
checks are listed in CARD-CONTRACT.md "Admission"; here is where they live.

- `stream add` (`modules/verbs/admit_stream.go`): the card branch names against
  git's ref-name rules; then three reads at once: the base tip, no `sprint`
  or `sprint/<s>` branch at origin, and with forge github the host's gh
  identity at write or above (`Engine.Forge`, `cmd/nova-sprint/admit.go`:
  `gh repo view --json viewerPermission`). The group branch is not named or
  made here; the call site carries one comment for the landing's check.
- `add` and its dry run (`modules/verbs/admit_card.go`): after the plan, every
  producer is admitted. Streams are read at once (base tip, the tree at the
  tip by `git ls-tree` in the state dir's clone, the card branches by one
  `git ls-remote`); a card is refused for its lint (tier and routes served,
  DONE-WHEN as one command, a named bench), then its PATHS, then
  work already at the base (its branch, HEAD, or PR: `gh pr view`). The brief
  parse (`modules/verbs/brief.go`, `modules/verbs/brief_lint.go`) refuses the bad forms and
  the retired keys with the line.
- `hook`: the snapshot read proves the store credential (none is
  `CredentialFix`); then every stream's base is read at once from this
  process, which is what finish and read will need. A worker is asked for no
  forge token.
- `up` step 7: `WorkersReady` also checks every bench's binary is this
  version (at once over the benches); the worker runs its probe (harness
  lists the policy's model ids, seat holds the provider keys) before its
  sprint hook, so a hooked row is a probed row, and a failed probe leaves it
  unhooked.
- `restore`: the dump confine refuses a card record whose state is not in
  `store.CardStates`, naming the record; after the load, one `ADMIT` line per
  waiting or ready producer. The restore stands on a red admission line (a
  recovery refused for a moved path would leave no store); check repeats it.
- `check` (`modules/verbs/check_admission.go`): the stream lines use stream add's
  admission; then the policy line (none active is red; a policy failing the
  order or rows question is red with `policy check`), the cards line (up to
  five red cards and a count), and the working line (a working consumer on a
  row unhooked, down or off the bus is red; a job dead on a present bench is
  not visible, said on the ok line).
- The bus hook (`modules/verbs/bus_answer.go`, item 45): a reply is never answered,
  a non-verb is logged once and ignored, a verb gets one reply naming the
  command to run.
- Costs: admission is one ls-remote, one fetch and one ls-tree per stream,
  and one `gh pr view` per PR line; at a few streams this is a few seconds
  per add (a guess, not measured). Unit tests use a fake origin, fake forge
  and fake exec: no network.
- Owed upstream: nova-config has no routes field; routes ride on machine
  roles (`route:<name>`) until one exists, and kin serve none. No per-job
  heartbeat reaches the store.
## Built: gates, drop and the agreement warning (stitch w3A, 2026-11-07; directives 77, 63)

- DONE-WHEN is one argv (`edge.ParseGate`, shared with the worker): `modules/verbs/brief.go`
  (the brief parse, with the reason and `edge.GateFix` on the lint) and `cardLint`
  (`modules/verbs/admit_card.go`, add, check and restore) refuse shell syntax outside quotes
  and a first word that is not a program; the old `shellProblem` (which allowed
  `&&` and `|`, so a chain was admitted and then failed on the bench) is gone.
- `drop`/`replace`: `steps.dropMoves` withdraws the dropped producer's live
  consumers first (`card.ReleasedPrefix + the drop's reason`), then plans the
  Cancel or Replace with them gone, all one `card.Merge`. The card machine is
  unchanged: Cancel still refuses a live consumer, the op carries the Withdraw
  before it, the same composition the machine's merge model checks. Tests:
  `TestVerbDropReleasesLiveConsumers`, `TestDropReleasesItsLiveConsumers`.
- The agreement warning: `verbs.Forge.RequiredChecks` (`hostForge` reads
  `gh api repos/<r>/branches/<b>` for the protection contexts and
  `repos/<r>/rules/branches/<b>` for rulesets); `modules/verbs/admit_cla.go` matches the
  names and prints the identities (`worker.CommitEmailDomain`). Two extra gh
  reads per github stream at `stream add` and `check` (not measured).

## Built: the board's load, ok% and ready alarm; a flaky test (stitch, 2026-11-07)

- `beat` is the worker's heartbeat verb (a worker verb, so a row not hooked in is refused): it
  sets the row's 1-minute load average (hundredths), cores and nova-reserve slots in use and in
  all (layer 1, `SetRowLoad`). A beat that repeats the row's last values succeeds and writes
  nothing. It is the one push the load cell needs (INTENT 7); nova-worker sends it every
  30 s (a guess) and at start. Built.
- `TestServeTicksPushesAndLogs` failed about one run in three: the deal waits for the head
  watch's first read of the stream's base tip, which ran in its own goroutine while the test
  drove only the tick beat. The test's clock now says when that read has finished (the watch asks
  for its beat only after reading), and the test waits on it before its first tick. No
  production code changed; `go test -run TestServeTicksPushesAndLogs -count=2000 -cpu 1,4 ./modules/run/`
  and 100 runs under `-race` pass.

**Stitch w3I (2026-11-07).** The deal's push names the one model the deal
named for the consumer (`DEAL ... tier=<t> model=<m> ...`, `none` for a
card dealt before models were named), beside w2D2's repo, base, branch and
clipped brief. Test: `TestEventNoticesUseCommittedImagesAndRecipients`.
## Built: the landing's red split in the facts; the host identity (stitch w3L, 2026-11-07)

Directives 59, 76; LANDING.md "A red base". `modules/run/edges.go`:
`landFacts` leads a red judgement on a red base with "the base itself is
red at its tip <sha> on: ...; the card's head is red on: ..."; the rest is
as before. (The inherited/new split, the agreement authors and
`ci_red_inherited` were removed 2026-11-08.)
`cmd/nova-sprint/edges.go`: the forge's `Identity` is `identity(env)`
(`forge.HostLogin`, or `GH_GATE_TOKEN` named as the override), and git's
github.com credential is always gh as the host's login (no switch on
`GH_TOKEN`/`GH_CONFIG_DIR`); `admit.go`'s `hostForge` runs gh in
`forge.HostEnv()`. Tests: `TestLandItemSplitsRedByTheBase`,
`TestIdentityNamesTheChoice`.
## Built: the roster takes a worker's advertisement (stitch w3W, 2026-11-07)

- `beat` gains `--tiers <t,t>`: the tiers the worker's harness proved it serves (each a known
  tier; stored in `Tiers`' order). nova-worker sends one beat at hook, before its first pass,
  and every beat after carries it.
- `roster.Advertised(rows)`: a worker row that advertised tiers is dealt exactly those, at width
  min(nova-config's width, its slots) when it read its slots; one that advertised nothing keeps
  nova-config's shape; kin are untouched. The run tick applies it after its fold, every verb's
  roster read (`Engine.roster`, now given the snapshot) and the board's feed too.
  `roster.Advertised(rows, version)` (defect D3): in the tick, `version` is the run's release
  (`Config.Version`, `Observed.Version`) and a worker row whose beat's binary is below it, or
  did not say, serves no tier, so it is dealt nothing; a build that is not a release pins
  nothing, and the probe and the board pass "". `beat` records the beating build's version;
  `check` prints `CHECK RED worker <row>: binary <v> below run's <v>: nova-fleet install
  --repo mas-bandwidth/nova-sprint --tag <v> <row>`.
- `release` gains `--head <sha>`: a row releasing a working work card it holds names the partial
  head it pushed, checked as a finish's head is; the producer goes back at that head with no
  bounce. nova-worker uses it for a job its restart cannot rebuild.

### Built: the installer's model retired (stitch, 2026-11-07)

- The installer (`modules/verbs/install*.go`) went with the verbs rewrite (c8b859a6): installing a release
  onto a machine is nova-fleet's install, and `make install` places a local build through
  `cmd/placebin`. Its model (`tla/Install*.tla`, its witnesses, `Install-runs.json`) and the 64
  install trace cases in `tla/check-layer6-traces.py` described code that no longer exists, and
  the trace script failed before checking anything (its emitter `TestInstallTraceWalk` was gone).
  Both are removed; the layer 6 trace check now emits and checks run, setup, handoff and choices.

### Built: the Run traces match again after deal-is-take (stitch w4T, 2026-11-07)

- The layer 6 trace check refused run-0 at record 43 of 132, the blocker raise of p3. The cause was
  the emitter, not the model or the run: deal-is-take added a retake of w0 (ready, then working, so
  w0 stays the youngest working consumer on r1 and the preemption's victim) as two ops of their
  own before the raise. Each is a store append the model has no action for, so the code's
  physicalSeq went 18 to 21 at the raise where the model's Report allows 19. The gens matched
  (Report raises card 3 to gen 1); the brief's reading of the gens was not the refusal.
- Fix, fixture side (`modules/run/run_trace_test.go`): the raise is one outside op, as the model's Report
  is one append. It retires w0 and cuts p0 a fresh consumer w1 working on r1 (put, then working, as
  a deal cuts one; a card's state is written once per op, so w0 cannot go ready and back), with
  the priority raise. w1 is the youngest working consumer whether the fold witnessed its take (at
  the raise, the latest take) or not (unwitnessed takes tie and the larger id wins: `w1` sorts
  after `p<n>.w1`). The emitter now refuses a stop request for any consumer but w1. Leaving w0
  ready from the setup instead was tried and refused: steps withdraws a ready consumer as dealt
  before the deal became the take (seed 1).
- No change to `tla/Run.tla`, `tla/RunTrace.tla` or `run/`: neither violated the other.
  `tla/RunTrace-MAPPING.md` names w1 as the victim. physicalSeq steps checked locally on all four
  run traces (run-0..2, run-stop: 0 mismatches, measured) before TLC on the bench.

### Built: one commit identity for the sprint (stitch w4I, 2026-11-07)

- New verb `identity` (coordinator, group operate): `--name --email --reason` sets the store's
  identity (one `SetIdentity` op fenced `AtSeq`, since 2026-11-09 `UnchangedSince` on the
  identity, so a beat does not refuse it); the same identity again changes nothing; no
  flags prints it with its rule. Refusals: none set and none given, a value `ValidIdentity`
  refuses, no reason. How the coordinator sets it for a live fleet:
  `nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint identity
  --as <coordinator> --name "<account's git user.name>" --email "<account's noreply address>"
  --reason "<why>"`, then each worker re-hooks (a bench worker's restart does it; a friend runs
  hook again).
- `hook`: a worker on a sprint with none is refused (`noIdentity`); every hook prints
  `COMMIT AS <name> <email>` and the rule under `HOOKED`, a coordinator on a sprint with none
  `NO COMMIT IDENTITY: ...` and the command. `take`: refused with none; the receipt gains
  `commit_name`, `commit_email`; the text gains the rule. `finish` (and `release --head`, which
  checks a head as finish does): `madeAsIdentity` reads the head's commits not on the stream's
  base at origin and not reachable from the attempt's base sha (`Origin.Commits`, a clone under
  the state dir fetched first, as `Contains` and `Tree`) and refuses any whose author or
  committer email is not the identity's (case-folded), naming up to ten commits and both
  emails, the fix the two config lines and a `--reset-author` rebase.
- `check`: a line `identity: every commit is made as <name> <email>`, red with the command when
  unset. The agreement warning (w3A, `admit_cla.go`) names the identity instead of a list of
  per-row identities, or that none is set yet.
- `up` step 7: before the benches, the identity; none is a question for the human (`ASK the
  human which forge account ...`, next: the identity command, then up again), like an unchosen
  dashboard; the VERIFIED line names it. No change to the setup walk's actions (a step that does
  not verify and has no act was already modelled), so no TLC was run for it; `Run.tla` and
  `Runner.tla` are untouched.
- Tests (unit, fakes): `TestIdentityVerb`, `TestTakeCarriesTheIdentity`,
  `TestFinishRefusesAnotherIdentity`, `TestVerbsWorkMidUp` (the worker hook refused, then
  allowed), `TestStreamAddWarnsOfAnAgreement`, `TestCheckAllGreen`, the setup walk and
  completion tests, `TestWorkersReadyNamesEachBenchAndItsLog`.

### Built: why the fleet is idle, in check, add, up and the board (stitch w4V, 2026-11-07)

- `check` (`check_waits.go`, `check_width.go`): with nothing ready and the
  fleet under its open width, the width line is a `CHECK WARN ... nothing
  dealable: <steps.IdleWhy>` naming the command for the top card; then one
  `CHECK waits <s>: <n> wait on <card> (<state>), ...` per stream whose producers wait (the three largest of each, then
  a count), and a `CHECK WARN stream <s>: its longest NEEDS chain is K cards:
  a -> b -> ... -> z; at the stream's land rate (R an hour over its last N
  landings) that is about H hours` per stream longer than the policy's
  `chain_limit` (3 by default). Warnings, never red: an idle fleet is not a
  fault of the tool.
- `add` prints `CHAIN stream <s>: the longest NEEDS chain through these cards
  is K cards (chain_limit L): ...` (dry run too), and refuses an add whose
  chain is longer than `chain_limit`, naming the chain, its hours at the land
  rate and how to flatten it (`steps.ChainThrough`: a chain elsewhere in the
  stream does not refuse an add beside it).
- The land rate reads the store's last 5,000 events of the epoch (a guess,
  about an hour of a busy sprint; bounded however long the sprint): each
  producer an accepted event moved into landed. Under two landings, or none
  apart in time: "no land rate yet". An unreadable history is said with its
  cause in the line.
- `read` takes `--cost <usd>` exactly as `finish` does: it already shared
  `reportFlags` and `readCost` and the card machine's report writes
  `SetCardCost` for either consumer kind, so only its usage and help said
  nothing of it; now they do, and `TestReadTakesCost` proves the write. The
  board's Cost breakdown (the total, the pie per tier and the top streams by
  tier) was carried with the v1 page and already counts reads
  (`TestReadCostIsInTheTierSpend`).
- `up`'s UP DONE is followed by one `BEFORE CARDS: keep everybody at width:
  ...` line naming add's CHAIN line and check's width, waits and chain lines;
  the coordinator playbook (section 1 step 3, section 4) and CORRECT-USE
  ("Keep everybody at width") say the same.
- Tests (unit, fakes): `TestCheckSaysWhyTheFleetIsIdle`,
  `TestCheckQuietWithoutWaits`, `TestAddReportsTheLongestChain`,
  `TestChainTime`, `TestLandingsCountsEachLandingOnce`, `TestReadTakesCost`,
  `TestSetupVerifiesActionsAndRestarts` (the advice line).
