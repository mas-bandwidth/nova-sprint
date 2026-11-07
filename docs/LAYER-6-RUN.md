# Layer 6: run, worker, verbs, setup and the board

Layer 6 is the set of processes that run layers 1 to 5 and the command line that drives them.
PostgreSQL owns the state. Everything else is a client of the store.

| piece | package | what it is |
|---|---|---|
| `nova-sprint run` | `run/` | the one chooser: folds the event log, plans with the steps (layer 3), applies the plan, runs the edges (layer 4) and pushes to rows |
| `nova-sprint worker` | `edge/worker`, `cmd/nova-sprint` | a bench's runner: hooks its row, takes what is dealt, runs the harness, reports |
| `nova-sprint dash` | `dash/` | the board, from the store or from a copy pulled from `run` |
| every other verb | `verbs/` | a client of the store: checks what must be true, applies one move, prints the result and `next:` |
| `nova-sprint up` | `verbs/setup*.go`, `cmd/nova-sprint/setup*.go` | the setup walk |

Whatever needs someone to act moves by push: `run` executes `nova-bus send`, and nothing polls.
nova-sprint reaches the bus only by executing `nova-bus` (`send`, `who`, `hook`); it never opens
Redis or keeps an inbox. See [CORRECT-USE.md](CORRECT-USE.md) for the policy the verbs enforce and
[LANDING.md](LANDING.md) for the landing.

## 1. Layout of the layer

The binary is `cmd/nova-sprint`. `main.go` dispatches: `up` goes through the setup wrapper
(`up_wrapper.go`), every other verb through `verbs.Engine.Execute` over the engine `open.go`
builds. Hidden entries exist for the policy engine's child process (`policy-eval`), the bus hook's
reply (`bus-reply`) and the setup walk's phases (`--internal-*`).

`open.go` opens the store and nova-config lazily (`help` and `version` open nothing), so a verb
that never reads the store needs no credential. The store credential comes from `NOVA_PG_DSN` and
`NOVA_SPRINT_PG_PASSWORD`; the bus from `NOVA_BUS_REDIS` (plus `NOVA_BUS_USER` and
`NOVA_BUS_PASSWORD` when the bus has a user). Under `nova-secrets exec` they are in the
environment of the verb's process and never reach a harness or agent. Per-process state is under
`~/.local/state/nova-sprint` (`repos/` holds one clone per stream repository, `land/` the hand
landing's clones).

## 2. The run loop

### Start

`run` is a service on one host, installed by `up`. Startup order:

1. Resolve the machine name run records itself under: `NOVA_SPRINT_MACHINE` (the service record's
   env, written by `up`), else the nova-config machine named by the hostname's first label. A name
   nova-config does not hold is refused.
2. Take the chooser lock: `pg_try_advisory_lock` on a dedicated connection held for the process's
   life, with `application_name` `nova-sprint run <machine> <pid>`. If it is held, run refuses and
   names the holder from `pg_stat_activity`:
   `SPRINT REFUSED run: run already chooses on <host>: stop it there, or nova-sprint show run`.
3. Hook in to the bus as `sprint` (`nova-bus hook --as sprint --exec '<nova-sprint> bus-reply'`).
   nova-bus allows one hook per name; a second is refused and the lock is released.
4. Read one cold snapshot, seed the policy's limits from one question-less ask, and queue each
   recipient's notices since its push cursor (`Pusher.CatchUp`, in bounded pages from the
   cursor, never from sequence 0).

The lock is taken before the bus name and a chooser starts only with both. The hook is a wake-up
and a receipt desk: run never parses a message body. A reply to `sprint` is never answered; a
message whose subject is not a verb is logged once on the hook's stderr; a verb sent to `sprint` is
answered once with the command to run instead (`verbs.BusAnswer`). Answering a reply with a reply
would loop.

### Reports are ordinary ops

A row's `report`, `finish` and `read` run their prerequisite table in the verb's own process, build
the card machine's plan for the consumer and its producer together, and `Apply` it: one
transaction, fenced on both generations. The op id is fixed by the consumer and its generation, so
a repeat, or a retry after an unknown outcome, replays the stored receipt. The verb prints the
receipt and exits. Nothing reaches `run` over the bus; the tick sees the report in its next fold.
The coordinator's verbs (`add`, `priority`, `judge`, `hold`, `drop` and the others) apply
the same way. A verb applies the one move it was asked for through the card machine; it never
deals, asks a read or lands. Only `run` chooses.

### The tick

`run` ticks once a second on the wall clock (`run.TickEvery`). One tick (`loop.tick`,
`Ticker.Tick`):

1. Read nova-config's roster at the active policy's fleet width cap, and `nova-bus who` once.
   A failed roster read plans nothing (an empty roster would judge every row unknown) but still
   folds, pushes and publishes the board.
2. Check the lock session and the sprint hook are alive. If not, stop applying: log `LOCK LOST` and
   return, and the service exits non-zero. The service manager restarts it, and it takes the lock
   again or refuses.
3. Fold `Events(cursor)` into the local copy and the steps' view. The fold is incremental, never a
   snapshot per tick. A `clear` (a new epoch) drops the inbox, the requests in flight and the
   refusals carried for the old epoch.
4. Record who is present. If the sprint is stopped, the tick ends here: nothing is planned or
   applied, and edge results wait in the inbox.
5. For each row recorded down but present on the bus, apply a `Seen` op so that a row that has just
   hooked in is open for this tick's deal.
6. Apply `roster.Advertised`: a worker that beat with tiers is dealt exactly those, at the smaller
   of nova-config's width and its slots; kin are untouched.
7. Call `steps.Tick` with the view, the inbox of edge results, the requests in flight, the stream
   heads, presence, refused ops and the roster. It asks the active policy its questions once, on
   this snapshot (`asker`), and returns ordered ops, edge requests, the carry and nudges.
8. Add one `judgement` op if a policy version failed a question it was asked (`policy.Judgements`).
9. Apply the ops in order, one `Apply` each, folding after each. Every op carries the `Running`
   expectation, so a `stop` that commits after the fold refuses the op. A refused op is kept and
   handed back to the steps next tick (`Inputs.Refused`) so only the plans on the card that moved
   are re-planned. An unknown outcome is replayed by op id before anything else.
10. Retire the inbox: the plan's carry first, then each item grouped into a refused op (answered
    again next tick); every other item was consumed.
11. Dispatch the plan's edge requests whose `After` op was accepted.
12. Push the tick's envelopes, publish the board from the folded copy, log the tick line.

The tick runs under a deadline of the active policy's `timeouts().tick` seconds (600 ms until a
policy has answered); a plan past the budget is carried, never dropped. The budget, packing and carry are layer 3's.

Edge calls run on goroutines of their own, never inside the tick, and no exec, git, HTTP or bus
call runs while the store's writer lock is held.

### Edges

`run/edges.go` runs the steps' edge requests off the tick and keeps their results for the next
tick's inbox.

- **land**: `git.LandPass` until the pass is not pending, then one inbox item. See
  [LANDING.md](LANDING.md). A stream whose mode is `hand`, or that has no forge, is answered at
  once with outcome `hand`.
- **stop**: one `nova-bus send --to <row> STOP "STOP <consumer>"`; sent, it is acknowledged.
- **stream heads**: every stream's base tip is read at start, then every 10 s (`HeadsEvery`) and
  after a landing, through a clone under `~/.local/state/nova-sprint/repos`. The tips feed
  `Inputs.Heads`. A failure is logged when it starts or changes, not every pass.
- **cost reconcile**: on the first beat and hourly, `cost.Reconcile` reads provider balances and
  opens a `cost-gap` judgement on a gap over 5% and $1.

### Pushing

The pusher turns each committed event into notices for recipients (the rows hooked in, the
coordinator among them) as `run` folds it, and sends each recipient at most one envelope per tick
(`nova-bus send --as sprint --to <name> <subject> <body>`, body at most 4 KB, with
`and N more: nova-sprint next --as <name>` for overflow). The recipient's cursor `push:<name>` (or
`push-hash:<sha256>` for a name too long for the store) moves to the last covered sequence only
after the send exits 0. A failed send is not retried within the tick; the unmoved cursor is the
only resend, so every line is safe twice (`take` of a card already held prints the same brief).
Nothing is pushed for an event before its op commits. A crash between a send and its cursor resends
on restart.

A nudge (a card in a row's hand that it has not acknowledged with `take`) is the one notice not
from the log. The steps derive it each tick from `ready_nag` after the deal, it rides in that
tick's envelope and moves no cursor.

The notices (section 7 has the judgement ones):

```
DEAL card=<id> attempt=<n> kind=work|read stream=<s> tier=<t> model=<m|none> epoch=<e> repo=<r> base=<sha> branch=<b> [prior=<sha>] [reason=<text>] brief=<clipped>; in your hand now: run nova-sprint take <id>@<gen> --epoch <e> --as <row> (acknowledges, prints the whole brief), then do it and run: nova-sprint finish <id>@<gen> --head <sha> --as <row> --epoch <e>
WITHDRAWN card=<id> reason=<text>; stop the runner and push its work
NUDGE you have a new ready card. ... run: nova-sprint take <id>@<gen> --as <row> --epoch <e>
LANDED card=<id> stream=<s> head=<sha>; run: nova-sprint show <id>
STREAM LANDED <s> <n> landed; run: nova-sprint show <s> --stitch
STREAM DONE WITH FAILURES <s> <n> landed <m> failed ids=<a,b,...>; run: nova-sprint show <s> --stitch
```

A read's DEAL carries `head=<sha>` in place of `base` and `branch`, and its finish command is
`nova-sprint read <id>@<gen> yes|fix`. Free text is clipped at 300 bytes with
`full text: <command>`. `LANDED` and the stream notices go to the coordinator only. Producers, not
consumers, are what the coordinator hears about; a read is told only to its reader.

### Log

`run` writes to stderr (under a service manager, the unit's log). One line per tick:

```
TICK n=8812 seq=125044 took=131ms folded=212 ops=388/0 edges=1 status=ok
```

(`ops=<applied>/<refused>`), and, when they happen: `OVERRUN n=<n> took=<d>` (a tick over a
second), `LOCK LOST`, `PUSH FAILED to=<name> err="..."` (resent next tick from the unmoved
cursor), `EDGE FAULT <what> err="..."`, `ROSTER REFUSED <kind> name=... reason=...` (once per
change of the refused set), `COST reconcile failed: ...`. Errors are redacted and bounded: no DSN
password, provider key or token value. The store gets no heartbeat.

### Restart

Reports are in PostgreSQL when their verb's op commits; push cursors trail confirmed sends; edge
requests are rebuilt from the store's state every tick (layer 3), so a restarted `run` takes the
lock, one cold snapshot, and goes on from each cursor. `run` holds nothing that is not in the store.

### A fresh or stopped store

A store `up` has just made is stopped with reason `never started`. `run` comes up on it and ticks
nothing until the coordinator runs `start`. `stop --reason <why>` does the same at any time: the
tick folds, pushes and publishes but plans and applies nothing, verbs keep working, and landing
results wait in the inbox. `stop` and `start` are one write each, fenced at the snapshot's
sequence. `start` on a running sprint and `stop` on a stopped one change nothing and succeed.

## 3. The verbs

### Conventions

- `nova-sprint <verb> [flags]`. Every verb takes `--as <name>` (else `NOVA_SPRINT_AS`). With no
  arguments, `nova-sprint` runs `next`.
- Exit 0: done. Exit 1: refused, one line naming the fix. Exit 2: bad usage, or the store, bus or an
  edge failed. Nothing exits 0 on a failure.
- A refusal is one line on stdout:
  `SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>`.
  A name, id or sha that does not exist is a refusal naming it, never an empty answer.
- A success ends with `next: <the command to run now>` or `next: nothing; wait for the bus`.
- A verb that can be hit twice is idempotent: ops carry ids fixed by their inputs, and the store's
  unknown outcome (a reply lost after commit) is replayed by id inside the verb. Callers never see
  an op id.
- Every verb that can withdraw or fail other cards (`drop`, `replace`, `hold` of a row, `clear`)
  takes explicit ids, never "all", has `--dry-run`, and prints what it withdraws and which dependants
  fail.

`nova-sprint help` prints the index; `help <verb>` prints that verb's usage, who may run it, what
it needs from `up`, the refusals and their fixes; `help playbook worker|coordinator` prints a role's
playbook.

### Who may run a verb

The role matrix is in `verbs/table.go` and printed by `help <verb>` as `who:`.

- **cold**: no hook needed. They look, or they hook, or they run a process: `next`, `check`, `up`,
  `hook`, `wake`, `unhook`, `help`, `version`, `run`, `worker`, `dash`, `restore`.
- **worker or coordinator**: the name must be on the bus and hooked in to the sprint. `take`,
  `finish`, `read`, `report`, `release`, `beat`, `show`, `queue`, `policy show|diff|check`.
- **coordinator**: the name must be the one coordinator. `stream add|set`, `add`, `judge`, `land`,
  `hold`, `unhold`, `priority`, `drop`, `relink`, `replace`, `policy set|activate`,
  `cost reconcile`, `stop`, `start`, `identity`, `backup`, `clear`.

"On the bus" is presence in `nova-bus who`; "hooked in" is a session recorded by `hook`. A verb that
acts as a name checks both before anything else, and its refusal names the step that is missing:

```
SPRINT REFUSED take: demo is not on the bus: hook in to the bus first: NOVA_BUS_REDIS=<host:port> nova-bus hook --as demo under your session's Monitor tool
SPRINT REFUSED take: hook in first: nova-sprint hook --as demo --role worker
SPRINT REFUSED add: add is the coordinator's, and demo is a worker: ask <coordinator>: nova-bus send ...
```

A row that cannot be reached cannot hold work. This check and the chooser lock carry a
`DO NOT REMOVE` comment and a test.

### Verb reference

| verb | arguments | refuses when (and names the fix) |
|---|---|---|
| `next` | `[--json]` | never for a missing prerequisite (the answer names it); exit 2 only if it cannot look |
| `help` | `[<verb>\|playbook <role>]`, `-h` | an unknown verb: the index |
| `version` | | |
| `check` | `[--integrity]` | prints every line; any `CHECK RED` exits non-zero (section 3, check) |
| `up` | `--as <coordinator> [--dash tailnet\|public=<machine>[:<port>]] [--services <a,b>\|none] [--key <service>] [--run-on <host>] [--version <v>]` | the first step that does not verify, with its fix (section 5) |
| `hook` | `--as <name> --role worker\|coordinator` | not on the bus; already hooked (`unhook` first); a second coordinator; a worker on a sprint with no commit identity |
| `unhook` | `[<row>]` | not hooked; another row's, and you are not the coordinator |
| `wake` | `--as <you>` | never fails a message (section 3, wake) |
| `take` | `<card>@<gen> [--epoch <e>] [--json]` | not dealt to this row at this generation; withdrawn or already reported; row not in nova-config or not serving the tier; no commit identity |
| `finish` | `<card>@<gen> --head <sha> [--usage ...] [--cost <usd>]`, or `--failed --reason <why>` | not the holder; not a full sha; not the tip of the dealt branch at origin; head equals the attempt's starting head; a commit by another identity |
| `read` | `<card>@<gen> yes\|fix [--finding <text>] [--usage ...] [--cost <usd>]` | the reader wrote the head; the producer moved on from the read's head; `fix` with no finding |
| `report` | `<card>@<gen> --result ok\|fix\|fail\|fault [--head <sha>] [--reason <why>] [--finding <text>] [--cost ...]` | as `finish` and `read`; `--cost` mixed with `--cost-microusd` or `--priced` |
| `release` | `<card>@<gen> --reason <why> [--head <sha>]`, or `--row <row> --all --reason <why> [--dry-run]` | no reason; not held at that generation; another holder's card or `--row` and you are not the coordinator |
| `beat` | `--load1 <loadavg> --cores <n> --slots-used <n> --slots-total <n> [--tiers <t,t>]` | not hooked; a flag missing or not a number; slots used over total |
| `show` | `[<card>\|<stream>\|<row>\|run] [--stitch]` | an unknown name |
| `queue` | `[--stream <s>]` | |
| `stream add` | `<s> --repo <org/repo> [--base main] [--mode auto\|group\|hand] [--forge github]` | stream exists; base not at origin; card branches `sprint/<s>/<card>` cannot be created; with a forge, the host's gh cannot write the repository or the group branch cannot be created |
| `stream set` | `<s> [--mode ...] [--forge github\|none]` | unknown stream |
| `add` | `--stream <s> --brief-file <f>... [--needs ...] [--after ...] [--priority <level>] [--dry-run]`, or `--stream <s> --sentinel <id> --needs <a,b>` | unknown stream; a failed admission (section 3, add) |
| `judge` | `[<id> <choice> --reason <why>]`, `<group> <choice> --expect <n> --reason <why>` | not open (names who closed it); a choice it does not offer; a group that changed |
| `land` | `<card> --reason <why> [--head <sha>]` | not a merging producer; `--head` not its finished head; a merge git or the forge refuses |
| `hold`, `unhold` | `<row\|stream\|card>... --reason <why> [--dry-run]` | a consumer id (hold its producer); a final card; no reason |
| `priority` | `<card> low\|normal\|high\|critical\|blocker --reason <why>` | a consumer id; a final card |
| `drop` | `<card>... --reason <why> [--waive <dependant>]... [--relink <dependant>=<card>]... [--dry-run]` | a consumer id; a live consumer (the drop releases it itself, so none remains); an open judgement on it: `judge <id> drop` |
| `replace` | `<card> --by <card> --reason <why> [--waive ...] [--relink ...] [--dry-run]` | as `drop` |
| `relink` | `<card> --need <failed need> --to <card> --reason <why>` | |
| `policy set\|activate\|show\|diff\|check` | layer 5 | |
| `cost reconcile` | | |
| `identity` | `[--name <git user.name> --email <git user.email> --reason <why>]` | none set and none given; a quote, backslash, `$`, backtick or angle bracket in either, or an email that is not one address; no reason |
| `stop`, `start` | `--reason <why>` | `stop` with no reason |
| `backup` | `[--out <dir>]` | `pg_dump` missing or older than the server; a build with no version stamp |
| `restore` | `<file> --as <coordinator>` | a run holds the lock; the store holds another dump's data (`clear` first) |
| `clear` | `--confirm <epoch> [--dry-run]` | a wrong epoch; a live consumer; the store changed while the backup was made |
| `run` | `[--listen <addr>]` | another run chooses; `sprint` hooked elsewhere |
| `worker` | `--as <row> [--host <h>] [--root <dir>] [--state-dir <dir>] [--harness claude\|codex\|opencode] [--work-root <dir>] [--mirror-root <dir>] [--providers <name>=<url>@<KEY>[,...]] [--once]` | name hooked elsewhere; row not in nova-config |
| `dash` | `--listen <addr> [--pull <url>]` | no store credential and no `--pull` |

`drop`, `replace` and `relink` fail, waive or relink a producer's dependants; a dependant not named
fails with the dropped card. `replace` relinks the dependants it does not name to the replacing card.
`drop` and `replace` release the dropped producer's live consumers first, in the same op, recorded
`released: <drop's reason>` with no result and no blame.

### What each verb needs from `up`

No verb is gated on `up`'s step. A verb refuses only for a piece it needs and names that piece.

| verbs | need | without it |
|---|---|---|
| `help`, `version`, `up`, `check` | nothing; `check` names the first missing piece | |
| every other verb | the store `up` makes at its step 2 (an epoch), at any later step of `up` | `the sprint is not set up: nova-sprint up --as <coordinator>`; `next` answers with the same text and exits 0 |
| every verb that reads the store | the sprint credential in the process | `this process has no sprint credential: run under nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...` |

### check

`check` is the one doctor. It prints one line per thing checked and never stops at the first red:
`CHECK <what>`, `CHECK WARN <what>: <fix>`, `CHECK RED <what>: <fix>`. With nothing red it ends with
`next:`; otherwise with `SPRINT REFUSED check: <n> lines are red` naming the first red's fix (exit 1,
or 2 when a red line is a read that failed). It starts nothing. In order:

1. The fleet (`nova-fleet check` green), nova-tools against the pin (below it is red, above it is
   `CHECK WARN`), the store at this build's schema, the bus answering. With `--integrity`, the event
   log rebuilds the tables (read only).
2. Only with the store at schema, from one snapshot:
   - one line per stream: its repository reached at its clone address by `git ls-remote`, its base
     branch there, its card branches creatable, and (forge github) this host's gh able to act. A
     contributor-agreement requirement on the base is a `CHECK WARN` naming the identity to sign;
   - the commit identity (red when unset);
   - the policy: none active is red (the tick would run on the built-in default and deal nothing); a
     version that fails a question the tick asks is red; a tier with no bench model is red;
   - the cards: each waiting or ready producer admitted as `add` would admit it today (up to five
     red cards and a count);
   - the working cards: each on a hooked row that is up and on the bus. A job dead on a present
     bench is not visible: no per-job heartbeat reaches the store, and the ok line says so;
   - the open judgements, one line per kind with the ids, the oldest's question and its command;
   - who runs the sprint: the chooser lock and the name `sprint` on the bus;
   - the width: working and dealt against the open width, and when producers are ready and none is
     dealt, why (stopped, no run, no open row, held streams, streams with no base head, tiers no open
     row serves, the steps' idle judgement). With nothing ready and the fleet under its open width,
     a `CHECK WARN ... nothing dealable: <why>`, then `CHECK waits <s>: ...` per stream and
     `CHECK WARN stream <s>: its longest NEEDS chain is K cards` from `steps.ChainWarn` (4) cards;
   - one line per stream with producers merging: what its landing waits for (a judgement about it,
     run, its hold, a hand landing, or its landing PR's CI).

### add

`add` reads each brief, plans the cards, and admits every producer before writing: the stream's
base tip, the tree at the tip (`git ls-tree`), the card branches (one `git ls-remote`), then per
card its lint (tier and routes served, DONE-WHEN as one command, retired mechanics, named benches),
its PATHS at the base, and its work not already there. The contract is in
[CARD-CONTRACT.md](CARD-CONTRACT.md). A card's id is its brief file's name without `.md`. The op is
fenced at the snapshot's sequence and planned again on a snapshot read after the admission, which
reads the repository for seconds while ticks move the store. `--dry-run` prints the same lines
without writing.

Output: one `ADD <id> kind=... stream=... tier=... priority=... needs=...` per card, then a
`CHAIN stream <s>: its longest NEEDS chain is K cards: a -> b -> ...` line (a `WARN CHAIN` from 4
cards, with how to flatten it; never a refusal), then `ADDED <n> cards applied at seq <n>`.

### hook, unhook, wake

`hook` does not run the bus hook. The bus hook (`nova-bus hook --as <name>` under the session's
Monitor tool) comes first, because every push travels over the bus. `hook` records the name, a fresh
session id and the role in the store, subscribing the name to the sprint's pushes. It prints
`HOOKED <name> role=<role>`, the commit identity (`COMMIT AS <name> <email>`, the two `git config`
lines and the `By:` / `Co-Authored-By:` rule) and the role's playbook ([PLAYBOOK-WORKER.md](PLAYBOOK-WORKER.md),
or both playbooks for the coordinator, who is a worker too), then `next:`. It also proves the store
credential and that every stream's base is readable from this process, since `finish` and `read`
need it. A worker is asked for no forge token. There is one coordinator per sprint; a worker needs
the commit identity set. `help playbook <role>` prints the same text, compiled into the binary,
and a test holds every command line in the playbooks to a verb and flag that exists.

`wake --as <you>` is for `nova-bus hook --as <you> --exec 'nova-sprint wake --as <you>'`. It reads
one bus line on stdin. If the message is from `sprint`, each `nova-sprint take <ref> --epoch <e>
--as <you>` line (a DEAL or a NUDGE for this row) is replaced by `take`'s own output (`TAKEN` with
the brief, or the refusal); everything else passes unchanged. It never fails a message, because
nova-bus ends the hook on a non-zero exit.

### take, finish, read, release

The deal puts a consumer in the row's hand, `working`, at the deal; the deadline runs from there.
`take` is the row's acknowledgement: it writes a cursor `took:<epoch>:<card>` once, which stops the
nudges, and prints the brief (`TAKEN <card> repo=<r> base=<sha> branch=<b> deadline=<s>s forge=...`,
the commit rules, the brief, and `next: nova-sprint finish <card>@<gen> --head <sha> --as <row>
--epoch <e>`). A repeat prints the same and writes nothing. `take --json` is the receipt
nova-worker reads: card, producer, kind, stream, repo, base, branch, tier, brief, paths,
`done_when`, epoch, gen, attempt, model, deadline, forge, `commit_name`, `commit_email`. If a row
does not acknowledge by `ready_take`, the steps withdraw the card and deal it to another row with
room, or open an `unacked` judgement when none can take it.

`finish` checks, in order: the row holds the card at this generation; the head is a 40-hex sha; it
is the tip of the dealt branch at origin (`push first: git push origin <branch>`); it is not the
attempt's starting head; and every commit on it since the base is authored and committed with the
sprint's identity (compared case-folded; the refusal names up to ten commits and both emails, and
the fix is the two `git config` lines and a `--reset-author` rebase). No PR, forge or token is
needed, and the head need not be up to date with any other branch: the landing does the forge
work. `finish --failed --reason <why>` reports the attempt failed.

`read <card>@<gen> yes|fix` answers a read. The reader must not be the head's author, and the
producer must still be at the head the read was of. `fix` needs `--finding "<path>:<line>: <defect
and repair>"`. `--usage` and `--cost` record what the run cost (the engine prices tokens from the
policy; `--cost` is kept as evidence beside the price, never as the price). `report` is
nova-worker's form of both; `fault` is for a harness, bench or route failure.

`release` gives a card back to the queue with its pushed branch kept, recorded `released: <why>`
with no result: neither ok nor fail. The re-deal is a new attempt of the same card and nothing is
counted against the card or the row. `--head <sha>` lets a row releasing a working work card
carry back the partial head it pushed (checked as a finish's head is). Another row's card, or
`--row <row> --all`, is the coordinator's.

### beat

`beat` is the worker's heartbeat: the bench's 1-minute load average, its cores, its nova-reserve
slots in use and in all, and `--tiers`, the tiers its harness proved it serves. It feeds the board's
load cell and the roster (`roster.Advertised`). A beat that repeats the last values succeeds and
writes nothing. nova-worker sends one at hook, before its first pass, and every 30 s.

### identity

One identity for the whole sprint: a repository's contributor agreement keys off the forge
account, so every agent, friend and bench worker commits as one configured account (typically its
noreply address). A contributor's own name goes only in the message (`By: <name>` and a
`Co-Authored-By:` trailer). The identity is store state set by
`nova-sprint identity --name "<git user.name>" --email "<git user.email>" --reason "<why>"`
(coordinator, one op fenced at the snapshot's sequence; the same identity again changes nothing; with
no flags it prints it). It is enforced in order:

- `up` step 7 asks the human for it before it looks at any bench;
- `hook --role worker` and `take` refuse until it is set, and both print the rule; the `take`
  receipt carries `commit_name` and `commit_email`, which the worker writes into the job's clone;
- `finish` and `release --head` refuse a commit by another identity;
- `check` has a line for it, red when unset; the landing's merge commits use it too.

### stream add, land, drop

`stream add <s> --repo <org/repo>`: with `--forge github`, `<org>/<repo>` is reached at
`https://github.com/<org>/<repo>.git` as this process's GitHub identity; otherwise `--repo` is a
clone URL or path. It checks the card branch names against git's ref-name rules, then at once: the
base tip; that no branch `sprint` or `sprint/<s>` exists at origin; and, with a forge, that the
host's gh identity has write permission. For a stream with a forge it creates the group branch
`sprint-group/<s>` at the base tip (refusing, with the fix, a name git cannot use, a branch in its
way, unlistable branches, or a push that fails). Output
`STREAM <s> repo=... base=... mode=... forge=... applied at seq <n>`, `next: nova-sprint add --stream <s> --brief-file <file>`.

`land` is described in [LANDING.md](LANDING.md). `judge <id> land` runs the same path.

## 4. `next`

`nova-sprint next --as <name>` (and bare `nova-sprint`) prints one `NEXT <message>` line and one
`next:` line: the first of these that is not settled. A missing prerequisite is the answer, exit 0.

1. The store cannot be read: the fleet is not up (the nova-fleet command), the store has no schema
   (`up`), or this process has no credential (the `nova-secrets exec` line).
2. No name given: choose one (`--as`).
3. The name is not on the bus: the `nova-bus hook` recipe.
4. The name is not hooked in: `nova-sprint hook --as <name> --role worker`.
5. The name holds a card: its step (`take`, then `finish` or `read`).
6. (Coordinator) an open judgement: its command, oldest first.
7. The sprint is stopped and nothing else is owed: `start` for the coordinator, wait for a worker.
8. No stream: `stream add`. A stream with no cards: `add`. Every card final: wait. Otherwise
   `nothing needs you`.

More than one thing owed adds `(<n> more: nova-sprint next --as <name> --json lists them)`.
`--json` is the inventory nova-worker reads: message, next, epoch, seq, row, state, role, width,
held, model, `tier_models`, harness, and the row's `ready`, `working`, `final` consumers and the
coordinator's `judgements`, each with its command. Width and model come from nova-config and
are 0 or empty, with `roster_error`, when it cannot be read.

A cold session, from an empty fleet to its first landed card (names are examples):

```
$ nova-sprint
NEXT nova-tools is up; the sprint is not set up
next: nova-sprint up --as coord

$ nova-sprint up --as coord
UP 1/8 nova-tools: fleet green; nova-tools v2.0.3 at or above pin v2.0.3: VERIFIED
UP 2/8 store: schema sprint version <n> and backup: VERIFIED
UP 3/8 coordinator coord: not hooked
SPRINT REFUSED up: coord is not hooked in: hook in to the bus first: ... nova-bus hook --as coord ...

$ nova-sprint up --as coord          # after hooking in; stops where a human must choose
UP 5/8 dashboard: not chosen
ASK the human: the dashboard on the private network (default), or public on a machine with a public address in nova-config?
next: nova-sprint up --dash tailnet      or      nova-sprint up --dash public=<machine>[:<port>]

$ nova-sprint stream add docs --repo example/docs --forge github
STREAM docs repo=example/docs base=main mode=auto forge=github applied at seq 40
next: nova-sprint add --stream docs --brief-file <file>
$ nova-sprint add --stream docs --brief-file cards/fix-typos.md
ADD fix-typos kind=producer stream=docs tier=flash priority=normal needs=-
CHAIN stream docs: ...
ADDED 1 cards applied at seq 41
next: nothing; wait for the bus
$ nova-sprint start --as coord --reason "go"
STARTED applied at seq 42
next: nothing; wait for the bus

# pushed to the row on the next tick
DEAL card=fix-typos.w1 attempt=1 kind=work stream=docs tier=flash model=... epoch=1 repo=example/docs base=<sha> branch=sprint/docs/fix-typos ...
$ nova-sprint take fix-typos.w1@0 --as bench-1 --epoch 1
TAKEN fix-typos.w1 repo=example/docs base=<sha> branch=sprint/docs/fix-typos deadline=...s forge=github
...
next: nova-sprint finish fix-typos.w1@0 --head <sha> --as bench-1 --epoch 1
$ nova-sprint finish fix-typos.w1@0 --head <40 hex> --as bench-1
FINISHED fix-typos.w1 head=<40 hex> applied at seq 50
next: nothing; wait for the bus

# a read is dealt to another row; the landing gates and lands; the coordinator is pushed:
LANDED card=fix-typos stream=docs head=<40 hex>; run: nova-sprint show fix-typos
```

## 5. `up`: the setup walk

`up` is [SETUP.md](SETUP.md) as a verb, resumable by construction: nothing records "step k done".
Each run re-verifies from step 1 and stops at the first step that does not verify, so a step that
broke since is found again. An answer is stored and never asked again (the coordinator, the
dashboard's place, the services and the run host, as inactive `kind data` policy versions named
`setup:<question>`; keys in nova-secrets). Each step's action is idempotent and `Act` never proves
its own step; only `Verify` does. Steps run only when every step below them verifies. A walk holds a
setup lease on the database, so two walks do not interleave across hosts.

| step | does | verified when | asked |
|---|---|---|---|
| 1 nova-tools | prints the nova-tools walkthrough | `nova-fleet check` exits 0; every tool at or above the pin | no |
| 2 store | migrates; creates role `sprint` through nova-fleet's postgres play; sets and proves its password with a SCRAM login | `sprint.meta` at this binary's version and hash; the role owns the schema; with `--version`, the installed build is that release and passes its `check` | no |
| 3 coordinator | records the name | the name is in `nova-bus who` | the name, once (`--as`) |
| 4 run | stores and activates the built-in default policy if none is active; installs the `run` service on the chosen host (default: this one) | the policy is active, the chooser lock is held and `sprint` is on the bus | the host, only with `--run-on` |
| 5 dashboard | installs `run --listen` on the tailnet, or `dash --pull` on the public host | `/healthz` 200 and `/api/sprint` at the store's epoch; for public, fetched from the public address | tailnet or public, once |
| 6 services | for each chosen service, the key at a hidden prompt into `nova-secrets set <SERVICE>_API_KEY` | one request to the service's model list, judged by exit code only | which services (or `none`), once; each key, once |
| 7 workers | the commit identity must be set (a question for the human: `ASK the human which forge account ...`, next: `identity`); grants each bench seat the sprint credential; one service record `sprint-worker-<machine>` per worker bench, applied in one `nova-fleet services` play | every worker bench runs this version, has probed its harness for the policy's model ids and its seat's provider keys, is hooked on the bus and `up` in the store; a refusal names each bench and its unit's last log line | no |
| 8 first deal | nothing: a fresh sprint is stopped until `start` | every worker row hooked, `up` and dealable (cores set in nova-config; a bench without is held at width 0) | no |

`UP DONE run on <host>; dashboard <url>; <n> workers up; coordinator <name>` follows step 8, with
`next: nova-sprint stream add <name> --repo <org/repo>` and a `BEFORE CARDS: keep everybody at
width` line pointing at `add --dry-run` and `check`. The worker record's argv is
`nova-sprint worker --as <machine> --work-root .local/share/nova-sprint/work --harness opencode
--providers <routes> --root .local/state/nova-worker/<machine> --state-dir .local/state`; relative
directories resolve under the bench login's home. The worker runs
its probe before its sprint hook, so a hooked row is a probed row and a failed probe leaves it
unhooked.

**The key.** The prompt turns off echo on the human's terminal and reads one line. Without a
terminal (an AI's shell) `up --key` refuses and names the terminal to run it in. The key goes from
the prompt to `nova-secrets set`'s stdin and nowhere else: never argv, the environment, a log, the
store or the bus. A leaked key is rotated.

**Two phases.** `up` starts as the admin-only bootstrap (it needs the Postgres admin password),
hands off a typed id over a private channel (`NOVA_SPRINT_HANDOFF_FD`) after releasing its lease,
and continues as a sprint-credential-only child that re-checks the persisted epoch, actor and
arguments before and under a newly acquired lease. Neither child sees the other's secret.

**Adopting a new build** is `up --version <v>`. `up` installs nothing: nova-fleet installs
nova-sprint. Step 2 verifies that the `nova-sprint` on PATH reports exactly `<v>`, then runs that
binary's `check` under `nova-secrets exec`. nova-sprint releases separately from nova-tools and carries the minimum nova-tools
version it needs (`toolPin` in `cmd/nova-sprint/main.go`); step 1 and `check` refuse below it,
naming `nova-fleet install --tag <pin> <machine>`.

### Backup, restore, clear

`backup` runs `pg_dump --schema=sprint --data-only --format=plain --inserts` into
`sprint-e<epoch>-s<seq>.sql` (under `--out`, else `NOVA_SPRINT_BACKUP_DIR`, else the current
directory), prefixed with a header naming the schema hash and this build's version. The dump is
loaded into a private throwaway PostgreSQL and verified before it is published; a dump that fails is
not left behind. `restore <file> --as <coordinator>` refuses unless schema `sprint` is empty and no
run holds the chooser lock; it parses the dump against a confined SQL grammar (no psql directives or
transaction control, and a card record whose state this build does not map is refused), loads it in
one transaction on the chooser-lock session, hooks the actor in as coordinator, and prints an `ADMIT`
line per restored waiting or ready producer. Restoring the same dump again only hooks. `clear
--confirm <epoch>` ends the epoch: it refuses a wrong epoch or a live consumer, takes a verified
backup first, and refuses if the store moved while it did, then opens epoch + 1 with `CLEARED
epoch=<n> opened; backup <path>`.

## 6. The dashboard

The board is one page (`dash/page`) and a JSON endpoint, served two ways. Either way a request is
answered from a cached view; the view advances once a second whether or not anyone looks.

- `run --listen <addr>` rebuilds the view each tick from the tick's own folded copy, adding no
  store read. It publishes only completed folds; a failed fold does not refresh the clock. A
  monitor with no viewer raises the stale alarm without another store read.
- `dash --listen <addr>` follows the log with its own cursor, from the store, on another host.
- `dash --listen <addr> --pull <url>` is the public form. It holds no store credential. It fetches
  run's `/api/sprint` once a second over the tailnet and serves that body gzipped
  (`Cache-Control: public, max-age=1`, an ETag), the page as static bytes, `/public.html` without
  the charts, a per-address request limit, and nothing that writes. A thousand viewers cost one
  fetch a second.

Routes: `/`, `/index.html`, `/public.html`, `/api/sprint`, `/healthz` (503 when stale). The view
reads the one record, never a derived status: a row's pill is down if down, else held if held, else
up; read consumers are in the working counts; sentinels are out of the ETA and rates. The merge
strip's `stuck` and `ci red` cells come from the landing step. A fresh or stopped sprint shows
`STOPPED <reason>`. The board has no "needs you" column: the judgement queue is reached by `next`,
`judge` and the bus push. A run machine never serves the pull-only board.

## 7. Judgements and notifications

A judgement is a row of `sprint.judgements` (layer 1), opened by a layer 3 step in the same op as
the move that raised it.

- **Open.** The step writes its kind, `about`, the question, the exact `judge` command, a group key
  and a deadline from policy (none for `blocked`, `land` and `no-policy`, which sit until answered
  or cleared).
- **Pushed.** The coordinator gets one `NEEDS YOU` line per judgement, once, from the open event:

  ```
  NEEDS YOU id=land.fix-typos.1: card fix-typos (stream docs onto main at <sha>, ...) did not land: ci_red: ...; run: nova-sprint judge land.fix-typos.1 <choice> --reason <why>   choices: land | fix | retry
  ```

  The same-kind judgements share a group key. An open judgement is listed by `next` and `judge`
  and never pushed again by a timer.
- **Answered.** `judge <id> <choice> --reason <why>` closes it and applies the choice in one op. A
  group is answered member by member with `judge <group> <choice> --expect <n> --reason <why>`; the
  `--expect` count refuses a group that changed. An answer by someone other than the coordinator is
  pushed as `ANSWERED judgement=<id> by=<actor> choice=<c>`. A second answer is refused naming the
  first.
- **Expired.** At its deadline the step expires it and applies policy's safe default for that kind,
  which never drops, accepts, merges or lands, and pushes `EXPIRED judgement=<id>: <what the
  default did>`.

Kinds and their choices (`steps/judge.go`): `blocked` unblock | drop; `cannot-read` retry | drop;
`brief` rework | drop; `land` land | fix | retry; every other kind (`unserved`, `stall`, `idle`,
`land-rate`, `flap`, `landed-sentinel`, `policy`, `engine`, `roster`, `overlap`, `no-model`,
`no-policy`, `unacked`, `undealt`) retry. A `cost-gap` judgement (opened by `cost reconcile`) takes ack | retry. `unblock` and `drop` move the judgement's card
in the answer's op; a land judgement's `land` is the hand landing and its `fix` sends the producer
back to work with the reason as the finding; the rest are the answer alone, which the next tick
reads.

**The stitch.** When the last producer of a stream is final, `STREAM LANDED` (or `STREAM DONE WITH
FAILURES`) goes to the coordinator, and `show <s> --stitch` prints what the stitch pass needs in one
place: the diff range `origin/<base>..origin/sprint-group/<s>`, each card with the row and model
that wrote it, and every reader's findings, by card.

## 8. Guarantees

1. At most one `run` applies tick ops at a time; a second refuses, naming the first. Across a lost
   lock a plan still applies at most once (the generation fences). The lock check and the store
   `Apply` are not atomic, so a loss between them can duplicate work or pushes; the store's
   conditions protect the state and pushes are at least once.
2. Each tick applies the ops its steps emit within the policy budget; moves past it, and plans
   refused by a fence, are carried and applied on a later tick, never dropped.
3. A row's report is one op applied by its own verb, consumer and producer together, fenced by
   generation: whole or not at all, applied at most once by its op id, and in the store the moment
   the verb prints its receipt.
4. Nothing is pushed before its op commits; every committed event that needs someone is pushed at
   least once; a push cursor never passes a send that did not exit 0.
5. No exec, git, HTTP or bus call runs while the store's writer lock is held.
6. A restart of `run` loses no state, report or push.
7. Every verb runs its prerequisite table before acting; a refusal is one line with the fix and exit
   1; a verb that could not run exits 2.
8. Every successful verb's last line is `next: ...`.
9. A verb that acts as a name refuses until that name is on the bus and hooked in.
10. Only `run` chooses.
11. A judgement is pushed once per group, closed at most once, and never expires silently.
12. `up` never runs a step before the steps below it verify; each question is asked once; a key is
    never printed, logged or passed in argv or the environment.
13. The board reads the store at most once a second per serving process, however many viewers; a
    public host holds no store credential and serves nothing that writes.
14. `clear` takes a verified backup first; `restore` writes only into an empty schema while no run
    holds the lock.
15. `check` and `up` refuse a nova-tools below the pin.
16. A fresh store moves nothing until `start`.

## 9. The models

- `tla/Run.tla` (traces: `RunTrace.tla`, mapping in `RunTrace-MAPPING.md`). Variables: the chooser
  lock and each process's liveness, the cards' generations, applied op ids, the event log, each
  run's folded copy and carry, `tickApplied`, and each recipient's push cursor and sent set. Actions:
  a row's report, `Take`, `Crash`, `Fold`, `Tick`, `Push`, `SaveCursor`, `Stop`, `Start`, and
  lock/hook loss. Invariants: `OneChooser`, `NoApplyAfterObservedLoss`, `CommitUnderCurrentLock`,
  `AppliedOnce`, `BudgetKept`, `PushAfterCommit`, `CursorTrailsSent`, `NoStaleMove`, `FIFO`,
  `NoDroppedPlan`, `EdgesAfterCommit`, `NoTickWhileStopped`, `NeverStartedMovesNothing`. Liveness:
  every planned move is applied or re-planned and every event is sent. Each invariant has a
  reversed witness under `tla/witnesses/layer6/` (the lock not checked, the fence dropped, the
  cursor saved before the send, the budget ignored, the carry dropped, a fresh store running, and
  others).
- `tla/Setup.tla`, `SetupChoices*.tla`, `SetupRun.tla`. `up`'s walk: `verified[1..8]`, stored
  answers, a count of times each question was asked, the program counter and the sinks a key
  reached. Invariants: `NeverAhead`, `OnlyVerified`, `AskedOnce`, `KeyNeverPrinted`,
  `RestartFromOne`; liveness: once the world stops regressing, `up` reaches done. `SetupRun.tla`
  puts the walk beside the run's ticks and `start`/`stop`.
- The code is bound to the models by trace checks: the code's own walk emits traces and TLC checks
  each is a behaviour of the spec (`tla/check-layer6-traces.py`, `tla/CHECK-LAYER6-TRACES.md`).
  Run it on a bench machine.

## 10. Tests

**Unit** (no real time, sockets, network, subprocesses or outside service; the fake clock starts at
1000): the tick against `store.Fake`, a fake bus (`Send`, `Who`) and a fake exec; budget, carry and
refused-op handling; every row of the verb table with each refusal's exact text and exit code; `next` over
each answer; the cold-session transcript; the help index; `up` over fake steps (order, resume, ask
once, key never in output); version and pin parsing; judgement push grouping; the role matrix.

**Functional** (named `TestFunctional*`; each starts its own PostgreSQL with `initdb` and its own
Redis and uses the real `nova-bus`): two runs, one refused; kill -9 of run mid-tick and mid-push,
then every deal delivered and no move applied twice; reports racing a running tick; a row's verbs
end to end with the deal over the bus; `up` on a throwaway store and bus; backup, restore and
rebuild equals snapshot; the board under a thousand requests a second with one store read a
second; the role password on a trust host.

## 11. Source map

- `cmd/nova-sprint`: `main.go` (dispatch, version, `bus-reply`), `open.go` (the engine: lazy store,
  roster, doctor, archive), `services.go` (`run`, `worker`, `dash`), `edges.go`, `admit.go`,
  `lander.go` (the hand landing as the host's gh identity), `setup_*.go` (the production setup).
- `run/`: `chooser*.go` (lock and hook), `serve.go` (start, tick loop), `tick.go` (fold, plan,
  apply), `asker.go` (layer 5 for layer 3), `edges.go`, `push.go`, `notices.go`, `bus_hook.go`,
  `log.go`, `config_roster.go`, `http_server.go`, `restore_*.go`.
- `verbs/`: `table*.go` (the verb table and help), `verbs.go` (`Engine`), `hooked.go`, `result.go`,
  `apply.go`, `next.go`, `check*.go`, `add.go`, `admit_*.go`, `take.go`, `report.go`, `release.go`,
  `judge.go`, `land.go`, `stream.go`, `hook.go`, `identity.go`, `operate.go`, `pg_*.go`,
  `setup*.go`.
