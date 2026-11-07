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

**Start.** `run` is a service on one host, installed by `up` (step 4). It runs its
prerequisite table (section 3), then takes the chooser lock, `pg_try_advisory_lock` on a
connection held for the process's life. Held, it refuses, naming the holder from
`pg_stat_activity` (`application_name` is `nova-sprint run <host> <pid>`): `SPRINT REFUSED
run: run already chooses on studio (pid 4121, since 14:02): stop it there, or nova-sprint
show run`. Then it hooks in as `sprint`, which nova-bus requires before `sprint` can send
("name already hooked" releases the lock and refuses the same way). The hook is only a
wake-up and a reply: run never parses a message, and anything sent to `sprint` is answered
once with `replies are verbs: run nova-sprint next --as <you>` (as in the LAYER-4 draft).
Then one cold `Snapshot`, and the ticks.

**Reports are ordinary ops.** A row's `report` (nova-worker's form), `finish` and `read` run
their prerequisite table, build the card machine's plan for the consumer and its producer
together (`Report` with `Finished`, `Broken` or `Faulted`; a read's `Report` moves only its
consumer, since `ReadYes` and `ReadFix` are policy's from all reads, in the tick) and `Apply`
it themselves: one transaction, fenced on both gens. The op id is fixed by the consumer and
its gen, so a repeat or a retry after an unknown outcome replays the stored receipt (layer
1). The verb's wait is that receipt: it prints accepted or the refusal and exits. Nothing
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
restart, and every line is safe twice (`take` of a card already held prints the same brief).
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
card, row, stream or sha (LESSONS 2.26). "Hooked" is presence in `nova-bus who`; its
refusal prints the hook-in recipe for the harness detected.

| verb | arguments | refuses, and the fix it names | next it prints | exit |
|---|---|---|---|---|
| (none) | | runs `next --as <me>` | | 0 |
| `next` | `[--as <name>]` | never; a broken prerequisite is its answer | the one command (section 4) | 0; 2 if it cannot look |
| `help` | `[<verb>\|<group>]`, or `-h` | unknown verb: the index | `nova-sprint` | 0 |
| `check` | `[--integrity]` | fleet not green: each `nova-fleet` fix; a tool below the pin: `nova-fleet up`; `NOVA_PG_DSN`, `NOVA_BUS_REDIS` unset or unreachable | `up`, or what next says | 0, 1 |
| `up` | `[--as <coordinator>] [--dash tailnet\|public=<host>] [--services <a,b>] [--key <service>] [--run-on <host>] [--version <v>]` | the first unverified step, with its fix (section 5) | the next step, then `stream add` | 0, 1 |
| `stream add` | `<s> --repo <owner/name> [--base main] [--pr]` | store not at schema: `up`; repo not readable: layer 4's line; stream exists: `show <s>` | `add --stream <s> --brief-file <f>` | 0, 1 |
| `add` | `--stream <s> --brief-file <f>... [--needs <a,b>] [--after <a>] [--sentinel <id>] [--priority <level>] [--dry-run]` | unknown stream: `stream add`; a brief missing contract sections, by name; an unknown need; a cycle, naming the loop | `nothing; wait for the bus`, or `up` when no row is up | 0, 1 |
| `run` | `[--listen <addr>] [--shadow]` | `check` fails; another run chooses, naming host and pid; `sprint` hooked elsewhere | at start: `worker --as <row>` when no row is up | 0 on SIGTERM, 2 on lock lost |
| `worker` | `--as <row>` | not a host in nova-config's inventory: `nova-fleet join`; name hooked elsewhere; row serves no tier | `nothing; wait for the bus` | 0, 1 |
| `take` | `<card>@<gen> --as <row>` | not hooked; not dealt to this row at this gen: `show <card>`; at width: `finish` one first | `finish <card>@<gen> --head <sha>` or `read <card>@<gen> yes\|fix` | 0, 1 |
| `finish` | `<card>@<gen> --head <sha> --as <row>`, or `--failed --reason <why>` | not hooked; not the holder, naming who; not 40 hex; not on the dealt branch at origin: `git push origin <branch>`; PR wanted and absent; head equals base or prior head | `nothing; wait for the bus` | 0, 1 |
| `read` | `<card>@<gen> yes\|fix --as <row> [--finding <text>]` | not hooked; the reader wrote the head; head moved: `show <card>`; `fix` with no finding | `nothing; wait for the bus` | 0, 1 |
| `release` | `<card>@<gen> --as <row> --reason <why>` | not the holder | `nothing; wait for the bus` | 0, 1 |
| `show` | `[<card>\|<stream>\|<row>\|run] [--stitch]` | unknown name | what `next` says for that name | 0, 1 |
| `judge` | `[<id> <choice> --reason <why>]` | not the coordinator, or not hooked; judgement not open, naming who closed it; a choice it does not offer, listing them | the next open judgement, or `nothing; wait for the bus` | 0, 1 |
| `priority` | `<card> <level> --reason <why>` | a consumer id (`priority` is the producer's); a final card; a level outside `low normal high critical blocker` (`read` is a rung, not a level) | `nothing; wait for the bus` | 0, 1 |
| `edit` | `<card> --brief-file <f> --reason <why>` | a live consumer: `nova-bus send --as <me> --to <holder> ...` instead | `nothing; wait for the bus` | 0, 1 |
| `drop` | `<card>... --reason <why> [--waive <need> \| --relink <need>=<card>] [--dry-run]` | a live consumer: its holder's `release`; an open judgement on it: `judge <id> drop` | `nothing; wait for the bus` | 0, 1 |
| `hold`, `unhold` | `<row\|stream>... --reason <why> [--dry-run]` | unknown name | `nothing; wait for the bus` | 0, 1 |
| `dash` | `--listen <addr> [--pull <tailnet addr>]` | no store and no `--pull`; the pulled run not answering | the URL | 0 on SIGTERM |
| `backup` | `[--out <dir>]` | `pg_dump` missing or older than the server | `nothing` | 0, 1, 2 |
| `restore` | `<file>` | schema `sprint` not empty; a run holds the lock; the dump fails its rebuild check | `run` | 0, 1, 2 |
| `clear` | `--confirm <epoch> [--dry-run]` | wrong epoch; a live consumer, listing them; the backup it takes first failed | `stream add` | 0, 1, 2 |
| `policy` | layer 5's design | | | |
| `report` | `<card>@<gen> --result ok\|fail\|fault [--head <sha>] [--cost <usd>] --reason <why> --as <row>` (nova-worker's form of `finish`) | as `finish`; `fault` only for a harness, bench or route failure | `nothing; wait for the bus` | 0, 1 |

Every verb that can withdraw or fail other cards (`drop`, `hold` of a row, `clear`) takes
explicit ids, never "all", has `--dry-run`, and prints what it withdraws and which
dependants fail before it does it (LESSONS 6.5; LESSONS-PRIORITY-AND-MERGE D33). One verb
per thing (LESSONS 6.6): a judgement is answered only by `judge`, so the answers v1 spread
over `rework`, `return`, `accept`, `unblock`, `waive`, `rank` and `resume` are the choices a
judgement offers (`rework`, `drop`, `waive`, `relink`, `rescore`, `unblock`, `retry`,
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
    -> up: store, coordinator hooked, run, dashboard, services, workers, first deal
      -> stream add <s>
        -> add --stream <s> (brief passes the contract, needs known, no cycle)
          -> a row hooked (worker --as <row>  |  nova-bus hook --as <kin>)
            -> run deals: DEAL pushed to the row
              -> take <id>@<gen>      (hooked; dealt to me at gen; room in width)
                -> finish <id>@<gen> --head <sha>   (holder; full sha; on origin; PR)
                  -> read <id>@<gen> yes|fix        (another hooked row; not the author)
                    -> landing (a yes at this head; CI green at this head; LANDING.md)
                      -> LANDED pushed to the coordinator; STITCH when the stream is done
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
UP 1/8 nova-tools: fleet green; nova-bus, nova-fleet, nova-config, nova-secrets at or above the pin: VERIFIED
UP 2/8 store: schema sprint version 2 on studio: VERIFIED (made now)
UP 3/8 coordinator rowan: not hooked
SPRINT REFUSED up: rowan is not hooked in: run nova-bus hook --as rowan under your session's Monitor tool

# the AI hooks in and runs up again; steps 1 to 3 verify; up stops where the human chooses
UP 4/8 run: installed on studio; chooser lock held; sprint hooked: VERIFIED
UP 5/8 dashboard: not chosen
ASK the human: the dashboard on the tailnet (default), or public on a host with a public IP?
next: nova-sprint up --dash tailnet      or      nova-sprint up --dash public=<host>
# the human says tailnet; up --dash tailnet verifies 1 to 5 and stops again
UP 6/8 services: deepseek chosen; key not set
ASK the human to type the deepseek API key at the hidden prompt in a terminal they can see:
next: nova-sprint up --key deepseek
# the human types the key; up verifies 1 to 6 and goes on
UP 7/8 workers: amd-1, amd-2, titan, captain hooked and up: VERIFIED
UP 8/8 first deal: one work card and its read dealt and released within one tick: VERIFIED
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
# only of producers (LESSONS 6.9), is pushed:
LANDED card=fix-typos stream=docs head=<40 hex> took=6m12s cost=$0.04
run: nova-sprint show fix-typos
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
| 2 store | `Migrate`; the role `sprint` through nova-fleet's postgres play | `sprint.meta` at this binary's version and hash; the role owns the schema | no |
| 3 coordinator | records the name | that name is in `nova-bus who` | the name, once (`--as`) |
| 4 run | installs the `run` service on the chosen host (default: this one) | the advisory lock is held (`pg_locks`) and `sprint` is in `nova-bus who` | the host, only with `--run-on` |
| 5 dashboard | installs `run --listen` on the tailnet, or `dash --pull` on the public host | `/healthz` 200 and `/api/sprint` at the store's epoch, fetched from this machine; for public, from the public address, with the cache headers | tailnet or public, once |
| 6 services | for each chosen service, the key at a hidden prompt, piped into `nova-secrets set <SERVICE>_API_KEY` on stdin, then the grants, seats, worker rows and service rows | one request to the service's model list through `nova-secrets exec`, judged by its exit code only | which services, once; each key, once |
| 7 workers | `worker --as <row>` on every bench in nova-config's inventory, through nova-fleet | each row present on the bus and `up` in the store | no |
| 8 first deal | a stream `up-smoke` with one work card; run deals it and its read to real rows; both are released and the stream held | one work and one read consumer dealt within one tick (LESSONS 6.4) | no |

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

Glenn, 2026-11-06, as docs/README "The dashboard" carries him: "The dashboard design should
be identical to the v1 in visual, excluding the unimplemented stats part at bottom", and "it
should be exactly matching what we currently have, visually. no changes except i think you
added fail state." So v1's page (`index.html`, `app.js`, routes, JSON envelope) is carried as
is behind the shim on branch `v2-dash-shim`, matched against the live v1 board at the shim
README's widths, by eye and `tools/abshot`; any difference is a defect. `fail` is black, after
landed (Glenn, 2026-11-06, in the coordinator's ruling: "You can make fail black"); `blocked`
is its own token, a red-brown column shown only when nonzero; the stats panel is left off.

**Served two ways, one poller each.** `run --listen <addr>` rebuilds the view once a tick
from the tick's own local copy, adding no store read. `nova-sprint dash --listen <addr>` is
the shim standalone, following the log with its `dashboard` cursor, for another host. Either
way a request is answered from the cached view, the copy advances every second whether or
not anyone looks, and the stale alarm and `/healthz` 503 are the shim's.

**Public, with the cache.** A public host never holds the DSN. `dash --pull <tailnet addr>
--listen :443` fetches run's `/api/sprint` once a second over the tailnet and serves that
one gzipped body to everyone (`Cache-Control: max-age=1`, an ETag), the page as static
bytes, `/public.html` without the charts, a per-address request limit, and nothing that
writes. A thousand viewers cost one fetch a second.

**What it reads.** The one record, never a derived status (LESSONS 6.12): a row's pill is
down if down, else held if held, else up; read consumers are in the working counts;
sentinels are out of the ETA and the rates (LESSONS 6.15; both as built). The merge strip's
`stuck` and `ci red` cells, blank in the shim, are filled from layer 3's landing step
(LANDING step 5). The coordinator chip is filled from step 3's record. Nothing is added or
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
  pushes `EXPIRED judgement=<id>: <what the default did>`. `blocked` has no deadline by
  Glenn's ruling ("sits there until you review it").

Landings, a stream done, a red, a block, a stall and an orphan reach the coordinator by the
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

**Dogfood gates.** By docs/README "The release gate for v2.0.0", v2.0.0 ships when every
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
With layers 1 and 2 at about 5.7k today, v2 stays under the 20k cap only if layers 3 to 5
come to about 7k together; if it lands at the cap, v2.0.0 ships at the cap and the point
releases bring it down (LESSONS "How we work" 4).

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

## Built: foundation checkpoint (2026-11-06)

This is a checkpoint, not a completed layer. `run/` contains the chooser session, an event-folding tick seam, a scheduler, notification batching, and the run-fed dashboard adapter; the actual layer 3–5 adapters, CLI, setup, backup/restore parser, load gate and dogfood remain open.

- The chooser takes the PostgreSQL advisory lock on its dedicated session before the `sprint` hook. It checks both before applying; after observed lock loss it stops. The check and a store `Apply` on another session are not atomic: a loss between them can duplicate work or pushes. Store conditions and generations protect the state, and pushes are at least once. This is the weaker chooser guarantee ruled for this layer; `tla/Run.tla` models it. The real hook adapter and crash gate are still to be wired.
- Layer 3 owns ordering, budget, packing and carry. Its provisional consumed seam returns ordered ops, effects and answers with `After` op IDs, and carried edge results. Run applies the ops in order and emits an effect only after all of its named ops accept; refused op IDs and edge results return for replanning. An unknown outcome pins the op until Replay or a same-ID retry. Edge facts are assumed re-queryable by stable key after restart; layer 4 must validate that assumption. The real layer 3 and 4 interfaces are not wired yet.
- The scheduler reads nova-bus presence once per tick, checks the `sprint` name is present and observes its hook process for exit; `who` does not expose a lease token, so this is not proof that the live process owns that name after a disconnect. It gives steps the folded store rows as the only width, tier, model and harness source. This supersedes the earlier nova-config width wording: nova-config supplies machine inventory for setup, not row settings.
- Policy is loaded by snapshot version. A tick op's note records `planned_policy=<n>`; the event's Policy is the version active at commit. There is no atomic policy cutover. The store's conditions and generation checks still apply.
- The pusher sends by recipient, subject and judgement group, requires a matching recipient and bus message ID, and advances its cursor only after all batches in the prefix succeed. A message is bounded to 4 KB and 20 card lines, with `and N more: nova-sprint next --as <row>` for overflow. The no-loss interpretation of this summary depends on the future `next` enumerating every outstanding obligation; that integration is not yet tested. The key is `push:<name>` when it fits 128 bytes; longer names use `push-hash:<full SHA-256>` (Rowan's ruling), avoiding a store change.
- For terminal stream notices, the pusher reconstructs a recipient's historical cursor once in bounded pages and folds forward. It renders the notice at the event's sequence, not from the latest state: all landed gives `STREAM LANDED`; failures give `STREAM DONE WITH FAILURES` with landed/failed counts and bounded failed IDs. The stitch detail is requested by `show <stream> --stitch` rather than embedded in the bounded push. Once Clear is seen, old-epoch pending notices are cancelled and the recipient gets `epoch <n> cleared: <k> pending notices dropped (some may have reached you); run nova-sprint next`. This is the explicit exception to individual delivery; partial sends can make the count include a notice already received.
- Run's dashboard adapter publishes only completed event folds, including successful idle folds; a failed fold does not refresh the clock. The no-viewer monitor raises the existing stale alarm without another store read. The page assets are unchanged; the baseline golden's two build hashes were corrected separately for the already-merged page.
- Restore's chooser-lock primitive opens one session, checks the schema is empty inside one transaction, and passes parser-verified individual SQL statements to that same session. Its extended-protocol guard rejects multiple commands, but the parser must validate dump grammar, reject psql directives and all transaction control; the leading-word check is not a parser. The verified dump parser, isolated full backup/restore round trip and integration are not yet built; this primitive alone is not a restore verb.

**Measured at this checkpoint:** on reserved amd-1, Go 1.26.10, PostgreSQL 18.6 and Redis 8.10.2, focused `run` and dashboard race tests passed, including isolated PostgreSQL, a real nova-bus/Redis receipt by ID and body, and stale/recovery checks. The broad format, build, vet and `go test -race ./...` passed after merging the owner’s fix to the inherited enforced-ledger mismatch; the earlier failing log is preserved. No layer 6 p99 load measurement, real layer integration, cold read, production deployment or dogfood is claimed here. The logs are retained in the validation workspace; later gates must run on the exact integrated head.
