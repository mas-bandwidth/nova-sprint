# AGENTS.md

Rules for AI contributors to nova-sprint. Read `docs/README.md` for what the tool does; the sections below describe how each layer works.

## Tests

- Per-commit CI finishes in under 2 minutes per job (aim for 1). Nightly (schedule) jobs have no limit.
- Unit tests use no real time, sleep, network, sockets or subprocesses. Time is an argument, and clocks start at 1000.
- Every test calls `t.Parallel()`. Functional tests are named `TestFunctional*`; anything that needs a real clock, socket, process, PostgreSQL or Redis is functional.
- Name test files and functions for the behaviour they check.
- Keep fakes in `_test.go` files, never in production files.
- Cover every new store condition, write and refusal in the store conformance suite so Fake and PostgreSQL run the same scenario.
- Give each card-machine table row an accepting test with its plan asserted exactly, and a refusing test from every other state.
- Every gate has a negative control that plants its fault in the real artifact the gate covers. A gate that skips, cannot compile or excludes by default fails.
- A random-walk trace test calls the real entry points (`LandPass`, `RunConsumer`, `StepRow`, `RecoverRow`); it does not reimplement their steps.
- Timing measurements run only behind `NOVA_LOAD=1`, never per commit.
- Regenerate a golden with `UPDATE_GOLDEN=1`; never edit one by hand.
- Mark every unmeasured number as a guess where it is defined.

## Design

- nova-sprint works the same on 10,000 benches as on 10. A worker's cost on the server is O(its own cards). Idle costs about nothing: push, never poll. Reads are row-scoped.
- Nothing is hardcoded to one deployment: no org, host, provider, model, identity or person.
- Engine and policy are split. The engine holds only what, if wrong, could corrupt state. Policy owns every number, name, catalog, price and timeout.
- Every state machine has a TLA+ model in `tla/`, and the code is bound to it by trace checks. A model that does not match the code is a defect: change both together. A trace check never weakens or widens the model's `Next` to accept a trace.
- Every model has reversed witnesses (the model with one fault, which TLC must reject) and reach checks. Generate them from the model with the script in `tla/`.
- Run TLC on a bench or CI runner, never on the coordinator machine. Builds and test suites never run there either.
- One writer per fact. Readers read that record and never recompute it.
- One operation is one transaction with its event row in the same commit. Operations are idempotent by op id. Replaying the log rebuilds the state.
- Time is an argument. There is one clock comparison for every deadline, timer and overdue mark.
- Run no network, git, bus or exec call while the store writer lock is held.
- The card machine is pure: it returns a plan or a refusal that names the fix, and never touches a store, clock or bus.
- Configuration (a row's kind, width, tiers, model, harness) lives in the roster, never in the store.
- Every plan is fenced on the generation of each row and card it read. A card moves at most once per tick.
- Nothing stalls: every unlanded card is held by a named actor before a deadline. Every judgement and wait has a deadline and a safe default that never drops, accepts, merges or lands.
- A refusal names the one step that fixes it. No error is silent. An unknown name, a bad sha or a typo is a refusal, never an empty answer.
- Every verb refuses in one line (`SPRINT REFUSED verb: what: fix`, exit 1), exits 2 only when it could not run, and ends a success with a `next:` line.
- Reach the bus only by executing `nova-bus`. Do not open Redis, keep an inbox or add a second channel.
- A worker's git push is always a lease from the last pushed head, made only while the job holds its card. Never a plain force.
- A policy answer is validated against its domain before a step uses it. A failing version falls back to the previous version, then the built-in, then the engine defaults.
- Harness, environment and provider faults spend no attempt and fail no card.
- Cost is recorded per card and sums to the stream and the sprint. An unpriced run is shown as unpriced, never as $0.
- A change to `store/schema.sql` changes the schema hash: keep the previous schema in `store/testdata`, list its hash as a known older build, and test the in-place migration.
- Keep no dead code. Every exported name is used outside its declaration.

## Change discipline

- Fixes are minimal and surgical: the best code, not the most. No feature creep.
- Fix a failure at its source and delete the workaround it replaces. Do not add a watcher, side loop or second mechanism around the tool.
- Commits use the one commit identity the contributor agreement allows.
- Never delete live code because it looks unused; delete only with evidence.
- A gate marked DO NOT REMOVE is never removed or weakened.
- Check for stuck by count. The same problem again, a third pass over the same area, or no gain in a measurement over three fixes means stop and report.
- Zero compiler and vet warnings. `gofmt -l .` prints nothing.

## Docs and comments

- Document the code, not the conversation. A comment states what the code does and the technical reason; delete a comment that is only history.
- Docs describe the tool as it is, never its history: no "used to", "now" or "no longer", no version or round numbers, no status notes or to-do lists.
- Every command, output line and refusal quoted in a doc must match what the binary prints. Change the doc in the same commit as the string.
- Keep the playbooks, `docs/README.md` and the sections below in step with the code they describe. The enforced-rules ledger names real tests.
- No people's names in code, comments, docs or tests. Use neutral placeholders (`alice`, `bench-1`, `friendA`) in examples and fixtures.


## Store

Package `store` (layer 1): sole writer of sprint state (PostgreSQL schema `sprint`, pgx). Applies one checked `Op` per transaction and logs every op, accepted or refused; knows nothing of dealing or landing.

```
go test -count=1 -skip Functional ./store/...
go test -count=1 -parallel 4 -run Functional ./store/  # needs initdb on PATH, else skips
bash tla/ci-tlc.sh
```

API: `Open(ctx, $NOVA_PG_DSN, $NOVA_PG_PASSWORD)`, `Migrate`, `NewFake()`, `NewFakeFrom`, `CheckSchema`, `RestoreBegin/End`, `ValidateHistory`, `Rebuild/Fold`, `Balances`, `Landings`. `Store`: `Snapshot`, `Apply`, `Replay(opID)`, `Events(afterSeq, max)` (cap `MaxEvents` 1000), `Restored`, `Clear`, `Policy(version)`.

**Migrate** (one transaction, advisory lock; hash = sha256 of `schema.sql` minus comments/blank lines): no schema creates it stopped (`store.FreshStop`); same hash no-op; hash in `knownOlder` reruns `schema.sql`. Refusals: `the store is at schema version N, this binary knows schema version M`; `schema sprint exists and was not made by nova-sprint; drop it or point NOVA_PG_DSN elsewhere`. Fenced ops on a stopped store: `the sprint is stopped (never started): nova-sprint start`.

### Data model

All tables except `meta policy cursors balances` are scoped by `epoch`; nothing is deleted. Names/ids are at most `MaxName` 128 bytes, valid UTF-8, no NUL; counters are `integer`; out of range is `ErrInvalid`. `events` is append-only with unique `op`; `policy` versions are immutable but `active`.

Cards: kinds `producer`, `sentinel` (never held, no brief), `work`, `read`; states `waiting ready working review merging landed fail blocked done`; tiers `flash pro heavy frontier`; `gen` 0 at add, +1 per op writing the card. Producer/sentinel only: `needs`, `after`, `attempt`, `bounces`, `red`, `priority`. Consumer only: `of`, `holder` (fixed at add), `result` (`ok|fix|fail|fault`), `cost`, `model`; work only `prior_head`. Kind rules live in CHECKs, trigger `cards_fixed` and the Go writes (a violation is a logged refusal, never a constraint error). No width or tier check in the store (roster config). Stop and `Identity` survive a clear; ops that must not land while stopped carry `Running`.

### Operations

`Op{ID, Epoch, Actor, Kind, At, Expect, Writes, Note}`: `ID` caller-chosen, unique forever; `Epoch` required (from 1), equal to the store's; `At` passed in (the store never reads a clock); `Kind` is a log label. Conditions are checked in the writes' transaction; writes apply in order.

`Expect`: `CardGen CardState CardHolder CardRed NeedsLanded CardAbsent RowIs RowGen RowHeld StreamHeld JudgementOpen Running AtSeq`; a condition on a missing target is a refusal. Writes are a closed set, one field each (`PutCard`, `SetCard<Field>`, `PutRow`, `SetRow<...>`, `PutJudgement`, `SetCursor`, `PutPolicy`, ...); the authoritative table (fields, body check, kind rule, change test) is `specOf` in `ops.go`, reasons in `writes.go`.

- Two writes to one field, or a state outside the set: `ErrInvalid`. **A set write that changes nothing is refused** (`row r1 is already up`).
- `PutRow` is refused when another session holds the row (`row w1 is hooked by session sess-a; unhook it first`). `SetCursor` is refused at its value, below (`cannot move back`) and above the log (`cannot pass the log at N`).
- `PutCard` adds in `waiting`, gen 0. Needs/after: other epoch cards, no duplicates, not itself; cycles are layer 2's refusal.

### Outcomes, log, guarantees

- `Receipt{Accepted: true}`: `Seq` is the event. `Receipt{Accepted: false, Reason}`: nothing changed, logged refused, op id used; re-plan with a new id (CLI exit 1). `ErrInvalid`: never entered, nothing logged. `ErrUnknown` (ctx done, connection lost, error during/after COMMIT): `Replay(opID)`, if not found `Apply` again with the same id (CLI exit 2). Any other error: rolled back, never `ErrUnknown`.
- `events`: trigger refuses UPDATE/DELETE/TRUNCATE; one row per op, gapless `seq`, before/after images (none on refusal). `Rebuild`, `Fold` and trace checks use the one `Fold`. A repeated `Apply` of a known id returns the stored receipt.
- `Clear`: op of kind `clear`, no writes, at most `AtSeq`, idempotent by id, receipt `NewEpoch`. Empties streams, cards, judgements (still in `Events`); rows carry over `down`, flaps 0, gen +1; policy, cursors, stop, identity are kept.
- A snapshot is one `REPEATABLE READ` read carrying `Seq` and `Epoch`. `Apply`/`Clear` take `LOCK TABLE sprint.epoch IN SHARE ROW EXCLUSIVE MODE` and commit at `READ COMMITTED`: one writer, no serialization retries (this makes the model's atomic step one SQL transaction). Nothing that waits on the outside world runs under the lock. Readers fold `Events(afterSeq)`; a snapshot per tick is a defect.

### Schema identity, restore, history

`CheckSchema` is read-only with Migrate's checks (password via `PGPASSWORD`/pgpass). Restore of a `pg_dump --data-only --schema=sprint` dump runs in one caller-owned `pgx.Tx`: `RestoreBegin(ctx, tx, digest)` (refuses a foreign schema or any row present; disables `cards_fixed`), caller loads the dump, `RestoreEnd(ctx, tx)` (re-enables `cards_fixed`, runs `ValidateHistory`, unhooks hooked rows in one logged op `restore-unhook-<digest>-<seq>`); commit only if `RestoreEnd` succeeds. `ValidateHistory` (prefix `store: history:`) checks seq gaps, receipts, epochs, policy versions, cursors and `Rebuild` = snapshot.

### Fake, tests, model, files

`NewFake()` shares protocol, conditions, writes and refusal reasons with `PG`; starts stopped; `Fake.Drop(opID, landed)` makes the next Apply/Clear of that id `ErrUnknown`. 

Model `tla/Store.tla` (cfgs `Store.cfg`, `StoreRecords.cfg`), witnesses `tla/witnesses/Store*.tla`, reach `tla/reach/`; `TraceState`/`TraceOp` in `trace.go` project state onto it and refuse an op writing a field the model abstracts (`trace_tla_test.go`). **A new write touching modeled state needs a model action and a witness.**

Files: `store.go` types, Fold/Rebuild; `ops.go`; `check.go`; `apply.go`; `writes.go`; `pg.go pgread.go`; `fake.go fakefrom.go`; `schema.go schema.sql`; `restore.go history.go`; `landings.go balances.go`; `trace.go`.

## Card machine

Package `card` (layer 2): every legal move as a pure function (no store, clock, bus, I/O, globals) from the records it reads and one event to a `Plan{Expect, Writes}`, or a `*Refusal{Reason}` (a value, never an error; each names the fix). Layer 3 decides who may raise events.

```
go test -count=1 -skip Functional ./card/...
go test -count=1 -run Functional ./card/...
```

**Entry points**: `Move(p, consumers, rows, stream, needs, e)` producer/sentinel events; `MoveConsumer(c, p, r, e)` `Report`, `Withdraw`; `MoveRow(r, e)` `Seen Gone Hold Unhold`; `MoveStream(s, e)` `HoldStream`, `UnholdStream`; `MoveAdd(e, cards)`; `Merge(plans...)`; `ReportPlan(c, p, consumers, rows, s, needs, report)`; `RowFromStore(r, width, tiers)` etc. convert records.

Callers pass `consumers` = every card whose `Of` is `p`, `rows` = every row the event may name, `needs` = every card it reads (`Relink`: all reachable); an unpassed card is refused. Every plan fences each card read with `CardGen` and each row with `RowGen`.

### States

Producer states: `waiting` (need unlanded or stream held), `ready` (lasts one tick), `working` (one live work consumer of the current attempt), `review`, `merging`, `landed`/`fail` (final), `blocked` (only `Unblock` leaves). Consumers are `working` (never `ready`: the deal is the take), `done` or `fail`. *Live* = `ready` or `working`. Width is layer 3's check; the model guards it with `RoomOn`.

### Producer events (each fences the producer's `CardGen`; full preconditions in `move.go`)

- `Resolve` waiting->ready. `Park` ready->waiting. `Reached` waiting->landed (sentinel). `Released` working->ready.
- `Start{Row, Branch, Base, Model}` ready->working: `attempt`+1, cuts the work consumer; stream free (checked before state); `Base` full sha; row `up`, not held, serves the tier (fences `RowGen`, `RowIs up`, `RowHeld false`).
- `Finished{Consumer}` ->review (consumer done `ok`; sets head, author, `review`+1, clears red). `Broken` (`bounces`+1) and `Faulted` (reason prefixed `missing-route:` (`card.MissingRoute`) blocks on the first fault) go to ready.
- `AskRead` / `AskReads{Rows, Models}`: row is not `p.author` and holds no live read of this head. `ReadYes` ->merging (done-`ok` read at current review+head). `ReadFix{Finding}` ->ready (`bounces`+1). `WaiveRed` ->merging.
- `Landed{Head, Merge, Reason}` (head = p.head). `Red{Head, Reason}` ->ready (`review`+1, `finding`=reason, `red`=true).
- `Cancel`, `Replace{By, Reason}`, `NeedFailed{Need, InThisOp}`: non-final->fail, no live consumer. `Waive` / `Relink{Need, To}`, `Edit` (a brief changes only by `Edit`), `Rescore`, `SetPriority`, `Block`, `Unblock`.

Moves back to ready write `waiting` (reason `held: stream S is held`) when the stream is held: no producer is `ready` in a held stream. `InThisOp` fences the need `Merge` drops when an earlier move failed it (else `merge it with the drop`).

### Consumer, stream, row, add events

`Report{Row, Result, Head, Reason, Cost, Priced, Usage}` working->done: `Row` is the holder; work reports `ok|fail|fault`, read `ok|fix|fault`; `ok` needs a full 40-hex head; a work head differs from `base_sha` and `prior_head`; a read's head equals its cut head and the producer's. Withdrawing the current work consumer of a `working` producer also releases the producer. No `Take`; a duplicate report is one op.

`HoldStream{Reason, Ready, Seq}` parks every producer in `Ready` (fenced `StreamHeld false`, `AtSeq`, each gen). `MoveAdd` is fenced `CardAbsent`; it refuses existing or dotted/over-long ids, a sentinel with brief or score, bad needs/after and cycles.

### Invariants and Merge

Final is final. `attempt` rises only on `Start`; a producer event accepts only the consumer of its current attempt and kind. Priority is declared, never computed; reads rank between `high` and `critical`. A blocker preempts the youngest live consumer on a serving row (`Withdraw{Preempt}` merged with the blocker's `Start`); the victim returns to `ready`, no bounce. Only `Broken` and `ReadFix` raise `bounces`.

`Merge(plans...)` is sequential composition, not union: each move is re-planned against the records read before the op, with earlier writes folded in; last write of a field wins. So two relinks from one snapshot cannot close a cycle; a start after a hold is refused. A no-change write is dropped; a later state write erasing a move into `working` is refused. Two plans cutting one card are refused (use `AskReads`).

### Refusal wording (exact, quoted by tests)

`producer X is landed; nothing moves a landed card`; `consumer C is held by r1, not r2`; `head abc is not a full commit sha`; `head H is the base; nothing was done`; `head H is the prior head; nothing new was pushed`; `read C was cut at H1, the report names H2`; `r1 wrote producer X's head and may not read it`; `stream S is held; no new consumers start in a held stream`; `producer X has live consumer C on row r; withdraw it first`. More in `move.go`, `replay.go`, `add.go`, `report.go`.

### Tests, model

`move_test.go` accepts each event once with its exact plan and refuses it from every other state with its exact reason; `conformance_test.go` drives all paths on `store.Fake`. Model `tla/CardMachine.tla` (cfgs `CardMachine CardDeps CardPreempt CardReads CardMerge CardMergePre CardMergeDeps`), witnesses `tla/witnesses/Card*.tla`, reach `tla/reach/Card*`; `card/trace.go` maps records onto it. **A new event needs a table row, a model action, a witness and a trace mapping.**

## Steps

Package `steps` (layer 3): pure Go, no store, clock, bus, git, `os/exec` (`TestLintPure` also bans `time.Now`, `net`, `store.Store` and any function over 60 lines). `Tick` turns one `View`, `Inputs` and policy answers into a `Plan`; every move is a layer 2 move. If being wrong could corrupt it is the engine (legality, needs, holds, width, overlap, coordinator set, op ids); if it can only be slow it is policy (ordering, row choice, verdicts, limits, timeouts), and an absent or bad answer falls back to the engine default.

```go
func Tick(v *View, in Inputs, ask Asker) Plan
func Verb(v *View, in Inputs, r Request) (store.Op, *card.Refusal)

```

```
go test -count=1 -skip Functional ./steps/...
NOVA_LOAD=1 go test -count=1 -timeout 60m -run TestFunctionalLoad -v ./steps/
bash tla/ci-tlc.sh ; bash tla/make-traces.sh check
```

### View and inputs

`Build(snapshot, now)` once, then `Fold(events)` (equals a fresh `Build` at the same seq); never a snapshot per tick. Witnessed facts (zero for records older than the view; bounds then measure from `View.Since`): `TakenAt`, `ReadyAt`, `Landed[stream]`, `Down[row]`, `Faults[row]`, ack cursors `took:<epoch>:<consumer>`. **Open row**: `up`, not held, `Session` set, `Role` `worker` or `coordinator`, in the roster. **Overlap**: a shared *concrete* path (`Concrete`: non-empty, no `*?[`, no trailing `/`) between producers in `ready working review merging`.

`Inputs{Now, Inbox, Inflight, Heads map[stream]sha, Seen map[row]time, Refused, Roster}`. A row absent from `Seen` fires no silence rule. The roster is the only source of kind, width, tiers, model, routes; a row it lacks is never dealt to and gets a `roster` judgement. Inbox items: `report`, `land`, `stop` ack; no `take`. `GroupKey` rides in an op's `Note` (`OpGroups`) so a refused op can be split by group.

### Asker and defaults

`Asker.Ask(Questions) Answers`: one call per tick, budget `AskBudget` 500 ms. An absent subject, failure, out-of-domain answer, unoffered row/card or exhausted budget yields the engine default, recorded once as a `policy` judgement per question and version; version 0 is no policy. Questions (domains and defaults in `asker.go`): `order`, `rows`, `reads` (1..5), `verdict`, `width`, `landing`, `limits`, `timeouts`; `Answers.Models` maps tier to `provider/model`. **The gate**: `Answers.NoDeal` (no active policy, or it failed as a whole) deals nothing and opens one `no-policy` judgement; everything else still runs.

### The tick

Steps in order, each skipping cards and rows an earlier step claimed (a card moves once per tick): `rest unlisted reports preempt landResults needs holds limits fix deal undealt landRequests unacked judgements`. Groups are then ordered (blockers first, never budget-cut), budgeted, packed, named; the rest of the inbox carries.

- **Group**: one producer's move with the consumers it touches, one `card.Merge`; never split by the packer. Key = every card its plan writes plus the card it is about, with gens.
- **Budget**: every blocker group, the first non-blocker always, then each while cards touched stay within `Limits.Budget`. **Pack**: up to `Limits.Pack` groups per op; an oversize group, or one touching a card of a last-tick refused op, is its own op.
- **Refused op**: rebuild from the folded view (never resend the body), repack its groups each alone; a group refused alone on fresh state is left out with an `engine` judgement. An op is refused only by a race.

### Step rules

- **Reports** (one group each, via `card.ReportPlan`): a refusal is answered to the row and consumes the item (wording in `reports.go`); a report matching a done consumer's result and head is acked (`applied at seq N`), never applied twice.
- **Preempt**: a ready blocker in a free stream whose open serving rows all lack width gets one group: `Withdraw` of the youngest working consumer there (latest `TakenAt`, then id) merged with its `Start`, plus a `stop` edge; one per row per tick, else `unserved`.
- **Land results**: `landed` -> a `Landed{Head, Merge}` group per card still `merging` at that head; any other outcome -> a `land` judgement per card, nothing moves.
- **Needs** (from view state): non-final dependants of a `fail` producer fail as one group. A `waiting` producer with needs landed and a free stream: sentinel -> `Reached`; concrete-path overlap with a ready/started producer -> `overlap` judgement; else `Resolve`. Row load never gates ready.
- **Holds**: `Park` ready producers in a held stream; withdraw `ready` and unacknowledged consumers of a held row.
- **Limits**: `Block` + `blocked` judgement at `bounces >= Limits.Bounces` (faults never count). Per row in `Seen`: `Withdraw` after `row_silent_withdraw`; `Gone` after `row_silent_gone`; `flap` judgement at `FlapLimit`; a row with `RouteRestAfter`+ consumer faults rests (a deal filter only).
- **Fix**: when a review's reads are all final, `verdict` decides `ReadYes`, `ReadFix` or another read; a repeated finding opens a `brief` judgement.
- **Deal**: down the ladder `blocker, critical, read, high, normal, low`; within a rung policy `order`, then id. Work `Start`: candidates are open, unclaimed, not resting, serve the tier and every `NEEDS-ROUTE:` route (`card.NeedsRoutes`); none -> `unserved`; no nameable model -> `no-model`; row = policy's first answered row with room else engine ranking (tier rises at `EscalateAfter`/`FrontierAfter` bounces; a lower row is never offered); room = live consumers + this tick's deals under `width.work`. Branch `sprint/<stream>/<producer>`; base from `Inputs.Heads`. Reads: distinct non-author rows, else `cannot-read`.
- **In hand**: the deal is the take; `take` writes only the ack cursor. An unacknowledged consumer gets a `NUDGE` line after `ready_nag` and is redealt at `ready_take`, else an `unacked` judgement. A producer ready `UndealtAfter` with a serving row with room opens `undealt`.
- **Land requests**: per stream with none in flight, `merging` producers not under a land judgement, in score order; an `after` target merging outside the list holds its dependant. PR, gate and merge are layer 4's.

### Verb and judgements

`Verb(view, inputs, Request{Actor, Coordinators, Card, Row, Stream, Event, Judgement, Answer, Drop})` -> one op: actor check, card-machine move, op. Coordinator only: `Block Unblock Waive Relink SetPriority WaiveRed Cancel Replace Edit Rescore Hold Unhold HoldStream UnholdStream Landed Red` and every judgement answer; `Report`: the consumer's holder; `Add`: anyone hooked in. Refusal: `SetPriority is the coordinator's; ask on the bus: nova-bus send --as A --to C ...`. Judgement answers are fenced `JudgementOpen`: `judgement J was answered by A; nothing to answer`, `judgement J needs a choice: ...`.

Judgements: id `kind.about.episode`, one open per cause, answered with `nova-sprint judge <id> <choice> --reason <why>`. Deadline `timeouts.judgement` except `blocked land no-policy`; expiry takes the kind's safe default (nothing dropped, accepted, merged or landed). Choices: `blocked` unblock|drop; `cannot-read` retry|drop; `land` land|fix|retry; `brief` rework|drop; others retry. `ChainWarn` (4) is the `NEEDS` chain length that warns.

### Guarantees, tests, model, files

No producer dealt twice (one group per card per tick, `Start` fenced on gen). No row over width. A refused op never half-applies. A stale report moves nothing; a repeated one applies once. No policy absence or error makes a step plan an illegal move (the gate is the exception). Nothing per tick is O(C) after the cold build.

Unit tests use `Inputs.Now` and `store.Fake` (`asker_test.go` every question absent/failing/slow/lying; `lint_test.go` purity; `request_test.go` every verb and actor). `walk_test.go` checks model invariants per step. Model `tla/Steps.tla` composes `CardMachine.tla` with the tick (cfgs `StepsPre StepsPrePack1 StepsDeal StepsLand StepsBurst`), witnesses `tla/witnesses/Steps*.tla`, reach `tla/reach/Steps*.cfg`.

Files: `tick.go`; `view.go`; `inputs.go`; `asker.go` (`Engine` defaults, validation); `group.go pack.go`; `reports.go preempt.go needs.go holds.go limits.go fix.go` the steps; `deal.go hand.go model.go` deal/ranking, acknowledgement/nudges, models and the gate; `land.go judge.go request.go roster.go waits.go`.

## Edges and worker

An edge is what nova-sprint does outside the store (bus, git, GitHub, a bench): a Go interface with a fake (unit tier) and one real adapter (functional tier). No edge runs under the store's writer lock; edges report facts and decide no move; outside programs run only via `edge.Exec` (argv, never a shell string); every refusal names its fix. Only `run/` calls the bus, git and forge edges (`run/edges.go`, `run/push.go`, `run/notices.go`). Code: `edge/{bus,git,forge,worker}`, `cmd/nova-worker` (= `nova-sprint worker`).

### Exec, bus, git, GitHub
- `Cmd{Argv, Dir, Env, CleanEnv, Timeout, MaxBytes, OnStart(pid)}`; `ValidateCmd` refuses empty `Argv[0]`/NUL. stdin closed; process group killed on timeout/cancel; defaults 30 s, 64 KiB per stream. Non-zero exit is `Result.ExitCode`, not an error. `*ExecError.Kind`: `invalid_argv`, `missing_binary`, `timeout`, `start_failed`, `canceled`. `Redact`/`RedactN` mask secrets; `Refusal{Problem, Fix}` reaches the user unchanged; `RequestID(epoch, kind, card, sha)` = `req-` + 24 hex SHA-256.
- `ParseGate(line)` is the one DONE-WHEN parser (admission and worker): words split at white space, quotes join, no escapes. Refused outside quotes: backquote, `; & | < > ( )`, backslash, `$(`, `${`, `$` before letter/`_`; first word must be a program (path or plain name), so builtins, `~` paths, `NAME=value` are refused. Fix text `GateFix`: write `sh -c '...'`.
- Bus: `Send(to, subject, body)` runs `nova-bus send --as sprint --to <to> <subject> <body>` (5 s); refuses empty recipient/subject and body > 4096 B (`MaxMessageBytes`); returns the id only on exit 0 with a line `<recipient> <id>`. Messages come from committed events only, one per recipient per tick, cut at 4 KB, never logs, diffs, secrets or the full brief; cursor `push:<row>` moves only after `Send` returned an id; no retry in the tick; at-least-once, every message safe twice.
- Git: `Git` = `Fetch, LsRemote, IsAncestor, TreeHash, MergeGroup(onto, heads, ledgers), Rebase, Push(sha, ref, expectOld)`. `LsRemote`: `(sha,true)` branch, `("",false)` absent; for a 40-hex sha, whether an origin tip contains it; an unfetchable tip is an error, never "absent". `MergeGroup`: detached `onto`, `--no-ff` per head, sprint identity dated at `onto`'s commit date (same inputs, same sha; no identity = refused); conflict outside `ledgers` aborts (`MergeConflict`); ledger conflicts resolve by the rule's script or union (`MergeLedgerFailed`). `Push`: `sprint-group/` refs use `--force-with-lease=<ref>:<expectOld>`; other refs with `expectOld` must be fast-forward; refusal `*PushRefusedError`; never a plain force. Credentials: with `Token` one helper answers from the child's env (`GIT_CONFIG_COUNT`; never argv), else only `gh auth git-credential` with GitHub credentials unset.
- `git.CloneURL(repo, forge)` maps `<org>/<repo>` + forge `github` to the GitHub URL and refuses other shapes/forges; rung 2 uses `sprint-group/<stream>` beside `sprint/`.
- GitHub: `Forge` = `PR, OpenPR, Checks, Merge, FailedLines, Inheritance, Authors`; `Client` runs `gh` with GitHub credential variables removed (`forge.HostEnv`); `Identity.Token` (`GH_GATE_TOKEN`) optionally overrides; a worker row's token never reaches it. `Checks` takes <= 100 shas (`MaxChecksBatch`), 40-hex only; `Merge(pr, sha)` uses `--match-head-commit <sha>`, a PR left open is an error; a rate limit rests the client (`*FaultError{Kind: rate_limit}`).

### Landing pass (`git.LandPass(ctx, g, fg, LandRequest) (LandResult, error)`)
One pass, one stream; never retries; non-landed results are outcomes with facts, an error means git/forge failed. Mode `hand` or no forge answers `hand`. One head whose branch contains base's tip is rung 1; any other group rung 2 (`MergeGroup` onto base tip, pushed to `sprint-group/<stream>` under lease). Outcomes include `landed`, `head_moved`, `missing_head`, `conflict`, `ci_pending`, `ci_absent`, `ci_red`, `ci_red_inherited` (every failing workflow also red on base), `ci_fault` (gate did not run; blames no head), `base_moved`, `merge_refused`. Run re-asks `ci_pending`/`base_moved` (`LandPoll`) and `ci_absent` (until policy `ci_wait`, then `no-ci`); the rest become a judgement (`landFacts`). A landing never force-pushes base, merges a sha CI did not pass, merges when base is not the gated tip, rebases a card branch, or runs under the writer lock.

### nova-worker
One service per bench serving one row. Flags: `--as <row>` (required), `--host`, `--root`, `--state-dir`, `--harness claude|codex|opencode|script`, `--work-root`, `--mirror-root`, `--providers <name>=<url>@<KEY>[,...]`, `--once`, `--sprint-bin` (nova-worker only). It runs `nova-sprint` verbs with the seat's env (`nova-secrets exec`); the DSN never reaches a harness.

Startup: `nova-bus hook --as <row>` as the one long-lived child (only `NOVA_BUS_*` env); `nova-sprint unhook --as <row>`; the probe must pass; `nova-sprint hook --as <row> --role worker`; heartbeat every 30 s, pass, wake loop. A bus line is only a wake: a pass reads `nova-sprint next --as <row> --json` and acts on it, so lost/late/repeated messages change nothing; passes also run every 10 s and only plan. Probe (same wall as a card): every route key present, else refuse naming key; per served tier the first `tier_models` candidate the harness serves is chosen (none: "no model for tier ..."); a take whose model the probe proved unserved is a `fault`.

Take receipt (`take --json`, `receipt.json`; carries `model`, `deadline`, `forge`, `commit_name`, `commit_email`): a missing `model`, `deadline`, `forge` or identity is refused naming `nova-sprint take <card>@<gen> --as <row> --json`, the card is reported `fault` and the worker exits non-zero.

Job `<root>/jobs/<consumer>/` (atomic `job.json`; never reused, a take is refused if the dir exists). Stage: `git clone --shared` from a mirror, `origin` = `worker.CloneURL`; work branch at `prior_head` else `base_sha`; read detached with push URL `no-push-on-read`; clone identity = receipt identity (none = refused). Run inside `nova-reserve` + `nova-sandbox`; the slot lives as long as the harness; deadline = start + receipt `deadline`. Harvest (outside the wall): `git push --force-with-lease=refs/heads/<branch>:<expect> origin <sha>:refs/heads/<branch>` (`<expect>` = head this job last pushed, else the tip at stage) through one function that refuses a job no longer holding its card; `ok` with uncommitted changes: commit (`git add -A`, `--no-verify`, receipt identity) and push, nothing to commit = `fail`; on wall death or kill past deadline push whatever commits exist. Work `ok` then rebases on `stream_ref` and runs each done-when line (via `ParseGate`; shell syntax is an error, never run) in a slot: conflict or red tests = `fail`, failed fetch or refused push = `fault`. Report `nova-sprint report <consumer>@<gen> --result ... --head ... --id <id> --as <row>` (id fixed before the first try, resent every pass until acknowledged). Clean only after the report is recorded, and for a work card only if origin's branch is at the pushed head (else `kept-unconfirmed-head`) and the worktree is clean (else `DIRTY WORKTREE KEPT`). No PR, no CI watching.

Results: `ClassifyJob` reads `result.json`; with none, a killed harness, provider 402/429/5xx or `SANDBOX REFUSED` = `fault`, else work = `fail: no verdict`.

Harnesses: `worker.NewHarness(name)` (interface `Name, Command, Probe, Classify, Usage, Needs`; `script` is the test double). `Needs` is declared once and `BuildWallArgv` honours it; nothing else widens a wall (only the job dir plus declared writes is writable; env is an exact allow-list, `SanitizeWallEnv`). Key proxy: API keys never enter the wall; `--providers <name>=<upstream url>@<KEY NAME>` (key read from the env var per request, never a flag) listens on `127.0.0.1:0` and forwards only to the route's upstream host (403 otherwise); the harness gets `OPENAI_BASE_URL` and a placeholder key.

Restart (`RecoverRow`): owed reports are resent under their id; a live harness (pid and nonce match) is adopted, killed and faulted past its deadline; else resume from receipt by state; no readable receipt: push held commits once under lease and `nova-sprint release <card>@<gen> --reason ... --head <pushed>`. A job whose consumer is final, stale, withdrawn or released is retired, pushing nothing. A report refused 3 times the same way (`RefusalLimit`) is quarantined. No silence kill: removing a running card's dirs destroys its work.

Guarantees: no edge runs under the writer lock; a bus cursor passes an event only after a confirmed send; base advances only to a sha whose checks passed at that exact sha from the gated base tip; a group lands whole or not at all and a gate that did not run blames no head; a card branch is never rebased after its report and moves only by the job holding its card, by lease; one live harness per job dir, dir never reused; a job dir is removed only after its report is recorded and its head is on origin, and committed work is pushed before any non-ok end; a report is recorded at most once per result id; a fault spends no attempt or bounce; no key or DSN in the wall's env or files.

Models: `tla/LandEdge.tla` (+ `LandEdgeTrace.tla`, `edge/git/trace_test.go`), `tla/Runner.tla` (+ `RunnerTrace.tla`, `edge/worker/trace_test.go`); witnesses `tla/witnesses/`; `tla/ci-tlc.sh`, `tla/make-traces.sh check`. Doubles `edge.FakeExec`, `git.Fake`, `forge.Fake`; `TestFunctional*` need real git, PostgreSQL, Redis and the nova-bus/reserve/sandbox tools.

## Policy

Policy is a Starlark file stored as an immutable version in `sprint.policy`; it answers a closed set of questions the steps ask each tick, never writes the store or raises a move; the engine checks every answer against its domain. Package `policy` is pure Go (go.starlark.net); only `verbs.go` touches the store. Wrong could corrupt = engine and modelled (move legality, priority ladder `blocker > critical > read > high > normal > low`, width, tier minimum, reader != author, `verdict` needing a matching read, CI green at the exact base head, the active version); only slow or badly ordered = policy.

### Questions (closed set)
A top-level name is a question or starts with `_`; a misspelt one is refused at check. A brief is never an input. `policy/default.star` (embedded as `policy.DefaultBody`) is version 1 and the built-in fallback; its numbers are stated guesses.
- `order(card)` -> tuple of <= 4 ints/strings. `rows(card, kind, candidates)` (kind `"work"|"read"`; candidates up, not held, serving the tier) -> names in preference order, none twice, none ranked below the tier, non-empty while any candidate exists, and including a row whose `width` answer has `work > 0` if any candidate has `width > 0`. `reads(card)` -> int 1..5. `verdict(card, reads)` -> `"yes"` (needs an ok read), `"fix"` (needs a fix read), `"retry"` (only when every read faulted). `width(row)` -> `{"work": w, "ahead": a}`, `0<=w<=width`, `0<=a<=4*width`. `landing(stream)` -> `{"backed_up": b, "flowing": f}`, `0<=f<b<=1000`, optional `"ci_wait"` 1..86400.
- `limits()` and `timeouts()` (all keys required, zero/negative/absent refused so no wait is unbounded; keys and ranges in `policy/`): run passes the first six `limits` to layer 3 and reads `fleet_width_cap`; `escalate_after frontier_after flap_limit idle_floor land_target` are validated and shown by `check` but steps use `steps.Engine` constants; `timeouts` includes `tick`, `base_regate` (strictly rising list), `consumer` (per-tier deadline, read from the take receipt).
- `prices()` maps model to micro-dollars per million tokens; an unlisted API model is recorded unpriced, never $0. `catalog()` (optional) -> `{"models": {provider: [{"name","tier"}]}, "harnesses": [...], "bench": {tier: "provider/model"}}`: tiers from `TIERS` (`flash pro heavy frontier`), nothing listed twice, each `bench` model one of its tier's; row verbs refuse a model/harness outside it.

Refusal: `POLICY REFUSED v7 line 31 rows(c12 work): row r4 has rank pro, below the card's tier heavy`. Engine defaults per question are in `steps.Engine` (`steps/asker.go`); layer 3 re-checks every answer, and an unusable one (absent, failed, out of domain, naming an unoffered row) is the default that tick and, under a real version, opens one policy judgement per question+version. Version 0 (none) applies defaults silently.

### Embedding and verbs
Dialect: zero `syntax.FileOptions` (no `while`, recursion, top-level `if/for`, global reassignment, `set`); `load` fails; no clock/random/file/network. File <= 64 KiB (`MaxBody`); 1,000,000 steps per call (`CallSteps`, "over the step budget"); tick deadline `TickTime` 500 ms on an injected `Clock`. `NewEngine` runs versions in a child, `nova-sprint policy-eval` (heap cap 512 MiB, `EvalMemory`); a run burning at the deadline or a child silent 100 ms past it is killed and every version in it fails. `Engine.Answer(AnswerContext)` (`policy/answer.go`) answers a tick from one version: active, previous active, then built-in (version 0), at one snapshot seq (another is refused, "reload"); each starts when the prior fails or after 250 ms (`fallbackAfter`); the first to pass before the deadline answers; any error, budget, deadline or refused answer voids that version. `Answers.Version` is the answering version or -1; `policy.Judgements` opens one `policy` judgement per failing version.

Verbs (`verbs/policy.go`; refusals `SPRINT REFUSED policy <verb>: <what>: <fix>`): `policy set <file> --reason` (coordinator) checks on the current snapshot and stores the next version (kind `script`), active only if none is; refused for a syntax/resolve error (with line), missing question, unknown top-level name, wrong arity, an answer refused on the snapshot, or a body equal to a stored version (`v4 has this body: nova-sprint policy activate 4`). `policy activate <version> --reason` (coordinator) re-checks, then one op fenced at the snapshot seq moves the active flag and answers open policy judgements; refused for unknown, already active, kind `data`. `policy show [<version>] [--list]`; `policy diff <version|default> [<version|default>]`; `policy check <file|version>` is read-only over every `FullQuestions` question: prints answers that would differ from active, refusals, warnings (never change exit); exit 0 when nothing refused, else 1. Versions number from 1 without gaps, never edited or deleted; rollback is activating an old number; adopt a newer default only via `set` + `activate`; `up` stores the built-in and activates it when none is active.

### Roster, cost, guarantees, tests
`roster.Load(ctx, src, cap)` (`roster/`): a `bench` machine is a worker with width `min(cores, cap)`; `Roster.Advertised(rows)`: a worker that advertised tiers gets exactly those, width `min(width, slots)`. `nova-sprint cost reconcile` (`cost/`; coordinator; hourly from run) records provider balances with the store seq (`sprint.balances`); a gap against recorded priced API records > 5% (`GapShare`) and > $1 (`GapFloor`) opens one `cost-gap` judgement (`ack|retry`), one at a time.

Guarantees: a policy change never moves a card; out-of-domain answers are refused, never used; the active version is a stored version and before `up` nothing runs; a runtime error never stalls a tick; one version answers a whole tick; deterministic, so `check` predicts the tick; at most one open policy judgement per version; the evaluator child bounds time and memory.

`tla/Policy.tla` (witnesses `tla/witnesses/Policy*.tla`; bound by `policy.TraceState` and the package trace test against `tla/PolicyTrace.tla`).

## Run loop and verbs

Layer 6 = the processes running layers 1-5 and the CLI; PostgreSQL owns the state, everything else is a store client. Pieces: `nova-sprint run` (`run/`, the one chooser), `worker`, `dash` (`dash/`, page in `dash/page`), verbs (`verbs/`), `up` (`verbs/setup*.go`, `cmd/nova-sprint/setup*.go`). Whatever needs someone moves by push (`nova-bus send`); nothing polls. `cmd/nova-sprint/main.go` dispatches `up` via `up_wrapper.go` and verbs via `verbs.Engine.Execute` on the engine `open.go` builds (lazy store/nova-config; `help`/`version` open nothing); hidden entries `policy-eval`, `bus-reply`, `--internal-*`.

### Run
Start: machine name `NOVA_SPRINT_MACHINE` else the hostname's first label. Chooser lock `pg_try_advisory_lock` on a dedicated connection for the process life (`application_name` `nova-sprint run <machine> <pid>`); held = `SPRINT REFUSED run: run already chooses on <host>: stop it there, or nova-sprint show run`. Then hook the bus as `sprint` (`nova-bus hook --as sprint --exec '<nova-sprint> bus-reply'`; a second hook is refused and the lock released): lock before bus name, the chooser starts only with both. Replies to `sprint` are never answered (would loop); `bus-reply` (`verbs.BusAnswer`) ignores replies and non-verb subjects.

Tick (`run.TickEvery` 1 s; `loop.tick`, `Ticker.Tick`): read roster and `nova-bus who` once; check lock session and `sprint` hook alive, else log `LOCK LOST`, stop applying, exit non-zero; fold `Events(cursor)`; a stopped sprint ends the tick here; `steps.Tick(...)` returns ordered ops, edge requests, carry, nudges; apply ops in order, one `Apply` each, folding after each; dispatch edge requests whose `After` op was accepted; push, publish the board, log. Deadline = policy `timeouts().tick`; plans past budget are carried, never dropped. Edges (`run/edges.go`): `land` = `git.LandPass` until not pending, then one inbox item; `stop` = `nova-bus send --to <row> STOP`; stream heads every 10 s. A fresh store is stopped (`never started`); `stop`/`start` are one idempotent write fenced at the snapshot seq.

`report`, `finish`, `read` run the prerequisite table in the verb's process and `Apply` the card machine plan for consumer and producer together: one transaction fenced on both generations; op id fixed by consumer+gen so repeats and unknown outcomes replay the stored receipt.  A verb applies the one move asked; it never deals, asks a read or lands. Only `run` chooses.

### Verbs
`nova-sprint <verb>`; `--as <name>` (else `NOVA_SPRINT_AS`); no args = `next`. Exit 0 done, 1 refused, 2 usage or store/bus/edge failure. Refusal: `SPRINT REFUSED <verb>: <what is not true>: <command that makes it true>` on stdout; unknown name/id/sha is a refusal, never empty. Success ends `next: <command>`. Verbs that can withdraw/fail other cards (`drop`, `replace`, `hold` of a row, `clear`) take explicit ids (never "all"), have `--dry-run`, and print what they withdraw. `help`, `help <verb>`, `help playbook worker|coordinator` are the contract for flags; the prerequisite table is `verbs/table*.go` and a test pins every row's refusal text and exit code. Roles (cold, worker = on the bus AND hooked, coordinator-only) are in `verbs/table.go`. Acting as a name off the bus or unhooked is refused (`SPRINT REFUSED take: demo is not on the bus: hook in to the bus first: NOVA_BUS_REDIS=<host:port> nova-bus hook --as demo under your session's Monitor tool`; `... add: add is the coordinator's, and demo is a worker: ask <coordinator>: nova-bus send ...`). Only `help version up check` need no store; the rest refuse with `the sprint is not set up: nova-sprint up --as <coordinator>` or `this process has no sprint credential: run under nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...`.

Verbs: `up --as <coord> [--dash tailnet|public=<machine>[:<port>]] [--services <a,b>|none] [--key <service>] [--run-on <host>] [--version <v>]`; `check [--integrity]`; `next [--json]`; `show [<card>|<stream>|<row>|run] [--stitch]`; `queue [--stream]`; `unhook [<row>]`; `wake --as <you>`. Worker verbs: `take <card>@<gen> [--epoch] [--json]`; `finish <card>@<gen> --head <sha> [--usage] [--cost]` or `--failed --reason`; `read <card>@<gen> yes|fix [--finding] [--usage] [--cost]` (`fix` needs `--finding "<path>:<line>: <defect and repair>"`); `report <card>@<gen> --result ok|fix|fail|fault [--head] [--reason] [--finding] [--cost ...]` (`--cost` not with `--cost-microusd`/`--priced`); `release <card>@<gen> --reason [--head]` or `--row <row> --all --reason [--dry-run]`; `beat --load1 --cores --slots-used --slots-total [--tiers]`; `hook --as <name> --role worker|coordinator`. `take` writes cursor `took:<epoch>:<card>` once; not acknowledged by `ready_take` = withdrawn and redealt, or `unacked` judgement. `finish` checks in order: holds the card at this gen; 40-hex head; tip of the dealt branch at origin (`push first: git push origin <branch>`); not the starting head; every commit since base authored and committed by the sprint identity (refusal names <= 10 commits and both emails; fix = two `git config` lines and a `--reset-author` rebase). `hook` (after `nova-bus hook --as <name>`) prints `HOOKED <name> role=<role>`, the commit identity (`By:`/`Co-Authored-By:` rule) and the role's playbook (`docs/PLAYBOOK-*.md`); one coordinator per sprint. `wake` rewrites `nova-sprint take` lines in a `sprint` bus message to `take`'s output and never fails.

Coordinator verbs: `stream add <s> --repo <org/repo> [--base main] [--mode auto|group|hand] [--forge github]` (creates `sprint-group/<s>`; refuses an existing `sprint` or `sprint/<s>` branch at origin); `stream set <s> [--mode] [--forge github|none]`; `add --stream <s> --brief-file <f>... [--needs] [--after] [--priority] [--dry-run]` or `--sentinel <id> --needs <a,b>`; `judge [<id> <choice> --reason]`, `<group> <choice> --expect <n> --reason`; `land <card> --reason [--head]`; `hold|unhold <row|stream|card>... --reason [--dry-run]`; `priority <card> low|normal|high|critical|blocker --reason`; `drop <card>... --reason [--waive <dep>]... [--relink <dep>=<card>]... [--dry-run]` / `replace <card> --by <card> ...` (fail, waive or relink dependants; first release live consumers, `released: <reason>`); `relink <card> --need <failed need> --to <card> --reason`; `identity [--name --email --reason]` (one git identity for the sprint; `hook --role worker` and `take` refuse until set; `finish` and `release --head` refuse others); `stop|start --reason`; `backup [--out <dir>]`; `restore <file> --as <coord>`; `clear --confirm <epoch> [--dry-run]`. `add` admits every producer before writing (per-card lint: tier and routes served, DONE-WHEN as one command, named benches, PATHS at base; rules in `verbs/brief.go`, a refusal names the section); card id = brief filename without `.md`; `CHAIN` warns from 4, never refuses. `check` never stops at the first red (`CHECK <what>`, `CHECK WARN <what>: <fix>`, `CHECK RED <what>: <fix>`; ends `next:` or `SPRINT REFUSED check: <n> lines are red`; exit 1, or 2 if a red is a failed read; starts nothing); `--integrity` rebuilds the tables from the log.

### up, backup, judgements
`up` is resumable: each run re-verifies from step 1 and stops at the first step that does not verify; `Act` is idempotent and never proves its own step (only `Verify`); a step runs only when all below verify; a setup lease serialises walks. A key goes only from a hidden prompt (no terminal = `up --key` refuses) to `nova-secrets set` stdin, never argv, env, log, store or bus. The admin-only bootstrap and the sprint-credential-only child (handoff over `NOVA_SPRINT_HANDOFF_FD`, args re-checked under a new lease) never see each other's secret. nova-sprint pins the minimum nova-tools (`toolPin` in `cmd/nova-sprint/main.go`); step 1 and `check` refuse below it naming `nova-fleet install --tag <pin> <machine>`. `backup` (`pg_dump --schema=sprint --data-only --format=plain --inserts` into `sprint-e<epoch>-s<seq>.sql`) is verified in a throwaway PostgreSQL before publishing; `restore` needs empty schema `sprint` and no run holding the lock; `clear --confirm <epoch>` takes a verified backup, refuses a wrong epoch, live consumer or a store that moved meanwhile, and opens epoch+1.

Judgements (`sprint.judgements`) are opened by a layer-3 step in the same op as the move and pushed once (`NEEDS YOU id=<id>: ...; run: nova-sprint judge <id> <choice> --reason <why>`), never re-pushed by a timer. `judge` closes and applies in one op; a group is answered member by member (`--expect <n>`); a non-coordinator's answer is pushed as `ANSWERED judgement=<id> by=<actor> choice=<c>`; a second answer is refused naming the first. At its deadline the step applies policy's safe default (never drops, accepts, merges or lands) and pushes `EXPIRED judgement=<id>: <what the default did>`. Kinds and choices: `steps/judge.go`. `show <s> --stitch` prints `origin/<base>..origin/sprint-group/<s>` with each card's writer and every reader's findings.

### Guarantees, models, tests
One `run` applies tick ops at a time; across a lost lock a plan applies at most once (generation fences), while lock check and `Apply` are not atomic so pushes are at-least-once. A report is one op, at most once by op id, in the store when the receipt prints. Nothing is pushed before its op commits; every committed event that needs someone is pushed at least once; a cursor never passes an unconfirmed send. No exec/git/HTTP/bus call under the writer lock. Every verb runs its prerequisite table first (refusal = one line with fix, exit 1; could-not-run = exit 2; every success ends `next: ...`). Only `run` chooses. A judgement is pushed once per group, closed at most once, never expires silently. `up` never runs a step before those below verify, asks each question once, never prints/logs/argv/env a key. The board reads the store at most once a second; a public host holds no credential. `clear` takes a verified backup first; `restore` writes only into an empty schema with no run holding the lock. A fresh store moves nothing until `start`.

`tla/Run.tla`, `tla/Setup.tla`, `SetupChoices*.tla`, `SetupRun.tla`; bound by `tla/check-layer6-traces.py` (on a bench). Unit doubles: `store.Fake`, fake bus and exec. Functional: kill -9 of run mid-tick/mid-push loses no deal and moves nothing twice.

## Layer seams

The lower layer's types are the contract; the upper layer adapts. No layer defines a type for another's use. Adapters live in the upper layer, or in `run` when joining neighbours.

```
verbs / cmd (6)
run (6)
steps (3)   policy (5)   roster (cfg)   edge (4)   dash (page)
card (2)
store (1)
```

- **store -> all**: `store.Store` is the only road to state (`Snapshot`, `Apply`, `Replay`, `Events`, `Restored`, `Clear`, `Policy`). One `Apply` = one op = one transaction, fenced by its expects. `Row` is sprint state only; width, tiers, model and harness belong to the roster. `Snapshot.Stopped`/`StopReason` are the machine line; `Snapshot.Identity` is the commit identity.
- **card -> steps, verbs**: pure. `Move*`, `ReportPlan`, `Merge` return a `card.Plan` (writes+expects) or a `*card.Refusal` naming the fix. No clock, no store.
- **steps -> run**: `steps.Build(snap, now)` once at cold start, `(*View).Fold(events)` every tick, `steps.Tick(v, Inputs, Asker) Plan`, `steps.Verb(v, Inputs, Request)`. Run applies `Plan.Ops` in order, one `Apply` each, folding after each; a refused op returns as `RefusedOp` for the next tick. `Carry` heads the next inbox; `Nudges` go to rows outside the log. An edge request goes out only once its `After` op was accepted; an unsent one is reissued with the same ID. No tick runs while `Snapshot.Stopped`. Person verbs use `steps.Verb` on a View from one snapshot, so the same actor rules apply.
- **policy -> steps** (adapter `run/asker.go`): `policy.NewEngine(clock)`, `AnswerContext`, `Judgements`, `PrevOf`. `run.asker` implements `steps.Asker` under the tick budget; failures go in as one judgement op. `Versions` = the active version plus the previous one, folded from `PrevOf` (none at cold start: the built-in backs the active version). Policy owns `limits().fleet_width_cap`, `timeouts().tick`, `timeouts().consumer[tier]`.
- **roster -> run** (`run/config_source.go`): `roster.Load(ctx, Source, cap)`; run takes one config `Snapshot(ctx)` per tick (one REPEATABLE READ transaction). A nil `Cores` holds the bench at width 0. A failed read fails that tick and shows on the board.
- **edge -> run** (`run/edges.go`): run is the one pusher and the one `nova-bus` caller. A `land` request becomes one `git.LandRequest` on its own goroutine, never under the tick; mode hand or no forge answers `hand` at once. The pass repeats on `ci_pending`/`base_moved`, and on `ci_absent` until `Wait`, then `no-ci`. The result is an `InboxItem{Kind:"land", ...}`: `landed`, or an outcome name with facts in `Reason`; layer 3 opens one `land` judgement per card for each. A `stop` request is a bus `STOP <consumer>` then an ack item. Hand landing goes through `verbs.Lander` (`cmd/nova-sprint/lander.go`).
- **worker seam**: `nova-worker` uses only verbs (`hook`, `next --json`, `take --json`, `report`). The take receipt carries consumer, card fields, `deadline` (from `timeouts().consumer[tier]`), `commit_*` (from `Snapshot.Identity`; take refuses without) and the `model` the deal wrote (take never chooses; a consumer with no model is refused). The worker holds no deadline, model or forge of its own. The deal is the take: a consumer is `working` from the deal; `take` acknowledges, repeats print the same receipt, unacknowledged cards are nudged each tick. A fault reason starting `missing-route:` blocks the producer for the coordinator.
- **dash -> run**: `dash.Feed`, `dash.NewFromFeed`, `(*Cache).Publish`, `CheckFreshness`, `dash.Handler`. Run publishes snapshot+roster as one pair every tick; a failed roster read shows every row at width 0 with the alarm. The Why-waiting panel, `check` and the idle judgement share `steps.Waits` and `steps.IdleWhy`.
- **verbs -> run**: `run.Serve(ctx, run.Config)` (lock, hook, cold start, ticks); `run.InspectChooser` serves `show run`; `run.OpenRestoreLock` serves `restore`.

## Enforced rules

Ledger of the guarantees, rules and refusals of layers 1, 2, 3 and 5 and the card contract. `tests/enforced/enforced_test.go` parses the rows (`| ID | Rule | Held by |`, holders comma-separated in backticks) and fails when a named holder is missing. A new rule gets a row in the same change.

- `invariant <Module>.<Name>`: TLA+ invariant in `tla/<Module>.tla`, listed in a `tla/*.cfg`
- `witness <Name>`: `tla/witnesses/<Name>.tla` or reach check `tla/reach/<Name>.cfg`
- `test <TestName>[/<scenario>]`: Go test in `store`, `card`, `steps` or `policy`; scenario is a string literal in its tests
- `check <Name>`: constraint, index or trigger in `store/schema.sql`

### Layer 1 - Store Guarantees

| ID | Rule | Held by |
| :--- | :--- | :--- |
| L1-G1 | Every card has one state and at most one holder | `invariant Store.SingleStateSingleHolder`, `test TestConformanceFake/writes` |
| L1-G2 | Every change is one atomic operation whose conditions (`Expect`) are checked | `invariant Store.ConditionsAtomic`, `test TestConformanceFake/conditions` |
| L1-G3 | The store holds no width: a row's width is the roster's (nova-config) | `invariant Store.NoSpuriousRefusal`, `witness StoreWidthInStore`, `test TestConformanceFake/no_width_in_the_store` |
| L1-G4 | Every operation is idempotent by its `op_id`: a second `Apply` of a remembered | `invariant Store.IdempotentById`, `test TestConformanceFake/idempotent` |
| L1-G5 | Every accepted and refused operation is recorded in the append-only event log | `invariant Store.LogMatchesState`, `witness StoreNoEvent`, `test TestConformanceFake/rebuild` |
| L1-G6 | Every operation checks the epoch; `Clear` advances the epoch by one | `invariant Store.OpsInTheirEpoch`, `witness StoreEpochUnfenced`, `test TestConformanceFake/clear` |

### Layer 1 - Store Schema, Operations, Conditions, Writes, and Refusals

| ID | Rule | Held by |
| :--- | :--- | :--- |
| L1-S1 | `Migrate` applies `store/schema.sql` idempotently | `check meta_id_check`, `test TestFunctionalMigrateTwice` |
| L1-S2 | Every card names an existing stream of the current epoch; a stream's `name` | `check streams_pos_check`, `test TestConformanceFake/writes` |
| L1-S3 | Row `state` is `up` or `down`, `role` is `worker` | `check rows_state_check`, `test TestFunctionalSchemaEnforces` |
| L1-S4 | Card `kind`, `state`, `result`, `priority`, and `tier` are in their closed sets | `check cards_kind_check`, `test TestFunctionalSchemaEnforces` |
| L1-S5 | Producer/sentinel vs consumer column partitioning in `sprint.cards`: | `invariant Store.ProducerNeverHeld`, `test TestConformanceFake/card_contract` |
| L1-S6 | Policy versions are append-only (`version`, `kind`, `name`, `body`, `actor` | `invariant Store.PolicyImmutable`, `witness StorePolicyRewritten`, `test TestConformanceFake/policy` |
| L1-S7 | Judgements start `open` and close (`answered` or `expired`) at most once | `invariant Store.JudgementClosedOnce`, `witness StoreJudgementTwice`, `test TestConformanceFake/judgements` |
| L1-S8 | `SetCursor` refuses moving a cursor backward or past the last event `seq` | `invariant Store.CursorMonotone`, `witness StoreCursorBack`, `test TestConformanceFake/cursors` |
| L1-S9 | `sprint.events.receipt` stores the receipt without repeating `after` | `test TestFunctionalReceiptAfterStoredOnce`, `test TestReceiptFidelityFake` |
| L1-S10 | Beyond primary and unique keys, `sprint.cards` defines only the partial index | `check cards_working`, `test TestFunctionalIndexes` |
| L1-S11 | `sprint.cards.red` is a boolean column (`DEFAULT false`) restricted | `invariant Store.RedOnlyOnProducers`, `witness StoreRedOnConsumer`, `test TestConformanceFake/red` |
| L1-S12 | `CheckSchema(ctx, dsn)` and `Schema(ctx)` report `SchemaIdentity` (`Present` | `test TestConformanceFake/schema_identity`, `test TestFunctionalConformanceICU/schema_identity` |
| L1-S13 | `NewFakeFrom(snapshot, policies...)` constructs a `Fake` whose `Snapshot` | `test TestConformanceFake/fake_from_snapshot`, `test TestFunctionalConformanceICU/fake_from_snapshot` |
| L1-S14 | `RestoreBegin(ctx, tx, digest)` and `RestoreEnd(ctx, tx)` restore | `test TestRestoreDDL`, `test TestUnhookOp` |
| L1-S15 | `ValidateHistory(ctx, tx)` (and `RestoreEnd`) verifies in a read-only check | `test TestFunctionalValidateHistoryLive`, `test TestFunctionalValidateHistoryRefuses/dropped_event` |
| L1-R1 | Wrong epoch (`op.Epoch != store.epoch`) is refused and logged | `invariant Store.OpsInTheirEpoch`, `witness StoreEpochUnfenced`, `test TestConformanceFake/seq_fence_and_epoch` |
| L1-R2 | Unmet `Expect` condition | `invariant Store.ConditionsAtomic`, `witness StoreStaleCheck`, `test TestConformanceFake/conditions` |
| L1-R3 | Tier not served is not a store refusal: a row's tiers are the roster's | `test TestConformanceFake/tiers`, `test TestProducerRefusals` |
| L1-R4 | Missing target or bad dependency at the store: a set write on an absent | `invariant Store.ConsumersWellFormed`, `test TestConformanceFake/write_refusals` |
| L1-R5 | Every set write (`SetCard*`, `SetRow*`, `SetStreamHeld`, `SetStreamMode` | `invariant Store.GenPerOp`, `witness NeverSameRedRefused`, `test TestGuardsFake` |
| L1-R6 | Caller bugs (`ErrInvalid`): empty `ID`/`Actor`/`Kind`, zero `At`, `Epoch < 1` | `test TestConformanceFake/invalid`, `test TestConformanceFake/name_lengths` |
| L1-R7 | `PutRow{Name, Session, Role}` is the sprint hook's one write: session | `invariant Store.HookKept`, `witness StoreHookStolen`, `test TestConformanceFake/hook` |
| L1-R8 | `SetStopped` stops or starts the sprint (`Snapshot.Stopped`, `StopReason`) | `invariant Store.ClearCarriesRows`, `witness StoreStopDropped`, `test TestConformanceFake/stop` |
| L1-P1 | Load test at 2,000 and 5,000 producers (4x sprint size) exercises tick apply | `test TestFunctionalLoad/build`, `test TestFunctionalLoad/tick` |

### Layer 2 - Producer, Consumer, Row, and Stream Moves

| ID | Rule | Held by |
| :--- | :--- | :--- |
| L2-M1 | `MoveAdd` creates a producer or sentinel in `waiting` at `attempt 0` once every | `invariant CardMachine.OrderAcyclic`, `witness CardNeverAdded`, `test TestAdd` |
| L2-M2 | `Resolve` moves a `waiting` producer (never a sentinel) to `ready` | `invariant CardMachine.SentinelShape`, `test TestProducerTable/Resolve` |
| L2-M3 | `Park` moves a `ready` producer back to `waiting` when its stream is `held` | `test TestProducerTable/Park`, `test TestProducerRefusals` |
| L2-M4 | `Reached` moves a `waiting` sentinel to `landed` | `invariant CardMachine.SentinelShape`, `witness CardNeverSentinelReached`, `test TestProducerTable/Reached` |
| L2-M5 | `Start` moves a `ready` producer to `working`, increments `attempt` by 1 | `invariant CardMachine.WorkingOneWorkConsumer`, `test TestProducerTable/Start` |
| L2-M6 | `Finished` moves a `working` producer to `review` | `invariant CardMachine.OldAttemptNeverMoves`, `test TestProducerTable/Finished` |
| L2-M7 | `Broken` moves a `working` producer whose current-attempt work consumer | `invariant CardMachine.OldAttemptNeverMoves`, `test TestProducerTable/Broken` |
| L2-M8 | `Faulted` moves a `working` producer whose current-attempt work consumer | `invariant CardMachine.FaultSpendsNothing`, `test TestProducerTable/Faulted` |
| L2-M9 | `AskRead` on a `review` producer cuts `<p>.r<review>.<n>` in `ready` on an `up` | `invariant CardMachine.ReadOnlyInReview`, `test TestProducerTable/AskRead` |
| L2-M10 | `ReadYes` moves a `review` producer to `merging` | `invariant CardMachine.ReadYesNeedsYes`, `witness CardYesOnZero`, `test TestProducerTable/ReadYes` |
| L2-M11 | `ReadFix` moves a `review` producer to `ready` (or `waiting` if the stream | `invariant CardMachine.FixKeepsScore`, `test TestProducerTable/ReadFix` |
| L2-M12 | `Landed` moves a `merging` producer to `landed` | `witness CardNeverLanded`, `test TestProducerTable/Landed` |
| L2-M13 | `Red` moves a `merging` producer to fix (`ready`, `waiting` when its stream | `invariant CardMachine.RedNeverAdvances`, `test TestProducerTable/Red` |
| L2-M14 | `Released` moves a `working` producer whose current-attempt work consumer | `invariant CardMachine.OldAttemptNeverMoves`, `test TestProducerTable/Released` |
| L2-M15 | `Cancel` and `Replace{by}` move any non-final producer or sentinel with no live | `invariant CardMachine.FinalNeverMoves`, `witness CardLandedMoves`, `test TestProducerTable/Cancel` |
| L2-M16 | When a need `n` is `fail`, `NeedFailed` moves a non-final dependant with no | `invariant CardMachine.NeedFailedTakesDependants`, `test TestProducerTable/NeedFailed` |
| L2-M17 | `Edit`, `Rescore`, and `SetPriority` update a non-final card in place while | `invariant CardMachine.EditNoLive`, `witness CardEditUnderLive`, `test TestProducerTable/Edit` |
| L2-M18 | `Block` moves a `ready` or `review` producer (or a `working` producer whose | `invariant CardMachine.BlockNoLive`, `witness CardNeverBlocked`, `test TestProducerTable/Block` |
| L2-M19 | `Start` and `AskRead` cut the consumer `working` | `invariant CardMachine.OnlyHolderActs`, `test TestSequenceWidth` |
| L2-M20 | `Report` moves a `working` consumer to `done` | `invariant CardMachine.OnlyHolderActs`, `test TestConsumerTable/Report_work` |
| L2-M21 | `Withdraw` moves a `ready` or `working` consumer to `fail` (recording a pushed | `invariant CardMachine.WorkingOneWorkConsumer`, `witness CardWithdrawLeavesWorking`, `test TestConsumerTable/Withdraw_work` |
| L2-M22 | Row moves transition `down -> up` (`Seen`) | `test TestRowTable`, `test TestMachineRowsEveryState` |
| L2-M23 | `HoldStream` sets the stream's `held = true` and parks every `ready` producer | `invariant CardMachine.ParkedUnderHold`, `witness CardHeldStreamReady`, `test TestHoldStream` |
| L2-M24 | A work fault whose reason starts `missing-route:` blocks its producer | `invariant CardMachine.RouteFaultBlocks`, `witness CardRouteRedealt`, `test TestHandRouteFaultBlocks` |

### Card Contract Rules Held by Layers 1 and 2

| ID | Rule | Held by |
| :--- | :--- | :--- |
| CC-1 | Card contract fields (`brief`, `paths`, `done_when`, `prose`, `tier`) | `check cards_brief_check`, `test TestConformanceFake/card_contract` |
| CC-2 | `Start` copies `paths`, `done_when`, `prose`, `finding`, `branch`, `base_sha` | `test TestProducerTable/Start`, `test TestProducerTable/AskRead` |
| CC-3 | Priority is a closed five-level enum (`low`, `normal`, `high`, `critical` | `invariant CardMachine.FixKeepsScore`, `check cards_priority_check`, `test TestConformanceFake/lesson_columns` |

### Layer 5 - Policy

| ID | Rule | Held by |
| :--- | :--- | :--- |
| L5-1 | The question set is closed: a top-level name is a question of its arity | `test TestCompileRefusesNames`, `test TestCompileRefusesForbidden` |
| L5-2 | Every answer is checked against its domain | `test TestRefusals`, `test TestRefuseYesWithNoOk` |
| L5-3 | No policy can leave the sprint unable to deal: the joint check refuses `width` | `invariant Policy.TickAlwaysAnswered`, `witness PolicyNoAnswerDeals`, `test TestDefaultRefusedByJointCheck` |
| L5-4 | A tick is answered by the first passing version of active, previous active | `invariant Policy.AnsweredByUsable`, `test TestFallbackOrder` |
| L5-5 | A tick has one deadline, `TickTime`, from input construction to the last answer | `witness PolicyTickStalls`, `test TestFunctionalSingleStepOverrunsTickTime` |
| L5-6 | One tick's answers, and so the answer cache, are capped at 4 MiB | `test TestCacheRetainsUnboundedBytes`, `test TestBudgetOutcomeDependsOnCacheHistory` |
| L5-7 | The built-in answers 5,000 producers over 100 rows inside the tick's budget | `test TestFunctionalLoad`, `test TestFunctionalFuzzPolicyDefaultTickUnderLoad` |
| L5-8 | Only a stored version that runs clean is activated | `invariant Policy.ActiveStored`, `witness PolicyActivateUnstored`, `test TestVerbsFake` |
| L5-9 | `Diff` is linear-space or refused above 10,000 lines | `test TestDiffQuadraticMemory`, `test TestReadCatalogUnknownVersion` |
| L5-10 | A judgement's question text is storable and bounded however long the failure | `test TestJudgementStorable`, `test TestJudgementTextNotStorable` |
| L5-11 | Answering is safe under concurrent ticks and verbs | `test TestConcurrentAnswer`, `test TestRaceVerbsAndAnswer` |
| L5-12 | `verdict`'s reads are the done reads of the producer's current review and head | `test TestVerdictCountsDoneReadsFromDifferentHead`, `test TestRefuseYesWithNoOk` |
| L5-13 | `check` prints a refused answer as refused, never as a change of value | `test TestCheckReportsRefusedJointAnswerAsValidChangeAndCascadesFromRefusedWidth` |
| L5-14 | The code is a behaviour of `tla/Policy.tla`: the policy walk's traces | `test TestTraceWalk`, `test TestTraceRefusesDivergence` |
| L5-15 | Every row fact a question reads (kind, width, tiers, model, harness) | `test TestRosterIsTheSourceOfRowFacts`, `test TestDefaultAnswersEverySnapshot` |
| L5-16 | A process engine runs every version in a child process killed | `test TestFunctionalEvaluator`, `test TestDeadlineOnTheClock` |
| L5-17 | A verb refuses, naming the field, any string the store would refuse before it | `test TestSetBodyOrNameTheStoreRefusesReachesItAsErrInvalid`, `test TestReasonInInvalidUTF8ReachesTheStoreAsErrInvalid` |

### Layer 3 - The Steps

| ID | Rule | Held by |
| :--- | :--- | :--- |
| L3-G1 | No producer is dealt twice: at most one group touches a card in a tick | `invariant Steps.NeverDealtTwice`, `witness StepsDealtTwice`, `test TestWalk` |
| L3-G2 | No row is over width: takes tallied against width minus working | `invariant Steps.WidthKept`, `witness StepsWidthOver`, `test TestWalk` |
| L3-G3 | No landing holds on an unqueued soft `after` | `invariant Steps.AfterKept`, `witness StepsAfterIgnored` |
| L3-G4 | A ready blocker with every open serving row at width is dealt by preemption | `invariant Steps.BlockerPreemptsInOneTick`, `test TestTickPreemption` |
| L3-G5 | A refused op never half-applies; a group is never split across ops | `invariant Steps.GroupWhole`, `test TestTickRefusedOpSplitByGroup` |
| L3-G6 | A report for a stale head, attempt or session is refused to the row | `invariant CardMachine.OldAttemptNeverMoves`, `test TestTickHappyPath` |
| L3-G7 | The budget is kept past the first non-blocker group; only the inbox carries | `invariant Steps.BudgetKept`, `witness StepsBudgetIgnored`, `test TestTickBudgetAndCarry` |
| L3-G8 | Ready never depends on row load; two overlapping producers of one stream | `invariant Steps.ReadyNeverFlipsWithLoad`, `witness StepsReadyOnRoom`, `test TestViewOverlaps` |
| L3-G9 | No policy answer, absence or error makes a step plan an illegal move or plan | `invariant Steps.PolicyFailsToDefault`, `witness StepsPolicyStall`, `test TestAskerAbsent` |
| L3-E1 | No tier is ever written; a consumer is never on a row below its card's tier | `invariant Steps.NeverBelowTier`, `witness StepsTierUnchecked`, `test TestWalk` |
| L3-E2 | The steps pre-check every store rule, so an op is refused only by a race | `invariant Steps.RefusedOnlyByRace`, `witness StepsSelfRefusal`, `test TestWalk` |
| L3-E3 | After any fold the view equals a fresh build at the same seq | `test TestViewFoldEqualsBuild`, `test TestViewRefusedEventMovesSeqOnly` |
| L3-E4 | The package reads no clock and no outside world; every function under 60 lines | `test TestLintPure` |
| L3-E5 | A verb refused for its actor writes nothing; coordinator verbs | `test TestVerbActorTable`, `test TestVerbAddReport` |
| L3-G10 | A consumer is in its row's hand from the deal | `invariant Steps.DealIsTake`, `witness StepsCutReady`, `test TestHandNudgesThenJudgesUnacked` |
| L3-T1 | The code is bound to the model: the walk's traces are behaviours of `Steps.tla` | `test TestTraceFixtures`, `test TestTraceWalk` |
| L3-L1 | Anything but a landed result opens one `land` judgement per card still merging | `invariant Steps.MergingCovered`, `witness StepsLandAfterHoldsUntilLanded`, `test TestTickLandJudgementEveryOutcome` |
| L3-L2 | Fewer than `backed_up` merging heads waiting in a stream is one request | `test TestTickLadderRungs` |
| L3-L3 | Base is never red from a landing; nothing lands that CI did not pass except | `invariant LandEdge.BaseNeverRed`, `invariant LandEdge.NothingLandsUnpassed` |

## Testing

CI runs from the repo root. Per commit (`.github/workflows/ci.yml`) the jobs `lint`, `unit`, `functional`, `trace` run in parallel under 2 minutes; nightly (`nightly.yml`) runs unit without `-short`, full traces and `tlc` in six shards. Locally: `make lint`, `make test`, `make build`.

- Unit tests are parallel, aimed under 1 minute, with no real time, sleep, network, sockets, processes or outside services; time is an argument, clocks start at 1000. Functional tests are named `TestFunctional*` and may use them. Every test calls `t.Parallel()` first; `tools/unitlint` refuses violations (exception: a `unitlint: <why>` comment).
- Lint: `gofmt -l`, `go build`, `go vet ./...`, `go run ./tools/silentlint <packages>`, `go run ./tools/unitlint`. `silentlint` refuses S1 fixed-string error in an err branch, S2 refusal re-wrapped without `errors.As`/`AsRefusal`/`RefusalOr`, S3 `_ =` discard outside named cleanups, S4 bare exit that never writes the error.
- Unit: `go test -count=1 -race -short -skip Functional ./...`. Functional: `go test -count=1 -race -run Functional ./...`; each test starts its own PostgreSQL via `initdb`. On macOS put Homebrew PostgreSQL on `PATH` and add `-parallel 4` (default `kern.sysv.shmmni` 32 fails).
- Traces: code walks emit traces that `tla/check-trace.sh` checks against `tla/` models (see "TLA+ models and trace checks" below); needs `initdb`, Java, a bench host. Per commit `GOFLAGS=-short NOVA_TRACE_PG=1 bash tla/make-traces.sh check`; the divergence breakers must be refused.
- TLC (nightly, bench host only): `bash tla/ci-tlc.sh`. Models must exit 0; every witness and reach check must exit 12. Shard with `TLC_SHARD=2/6 TLC_CORES=8`; `DRY_RUN=1` prints commands.

## TLA+ models and trace checks

Every state machine has a TLA+ module in `tla/`, checked with TLC; Go is bound to it by trace checks. Model, witnesses, cfgs and traces change together. Names live in the `.tla`/`.cfg` files (`grep -h INVARIANT tla/<Module>*.cfg`); Go cites invariants by name.

### Check kinds (each module has all three)

- Passing run (`<Module>.cfg`, scenario cfgs): exit 0.
- Reversed witness (`witnesses/`, `witnesses/layer6/`; Setup ones beside their models): module with one fault; TLC must violate the cfg's FIRST `INVARIANT`/`PROPERTY` (exit 12; `PROPERTY`: exit 13). List the target first.
- Reach check (`reach/*.cfg`): `Never<X>` claims a state unreachable; TLC must violate it.

Any other outcome fails the gate. A new invariant needs a witness; a new scenario, reach checks.

### Running (bench host only, never the working machine)

- `bash tla/ci-tlc.sh`: the gate: fetches pinned sha256-checked `tla2tools.jar` (v1.8.0, release `tla-tools-1.8.0`) into `$TLA_CACHE` (default `~/.cache/nova-sprint`). Env: `TLC_SHARD=i/n`, `TLC_CORES` (caps `-workers` and live-worker sum), `TLC_JOBS`, `TLC_HEAP` (per JVM, default `2g`), `DRY_RUN=1`.
- `sh run-tlc.sh JAR [java]` (layers 1-3, logs in `logs/`), `sh run-one.sh JAR JAVA DIR MODULE WORKERS [CFG] [LOG]`, `summarize-logs.sh`, `show-trace.sh <log>`, `show-all-traces.sh`, `last-state.sh <log>`, `python3 -I card-trace.py <log>`.
- `python3 run-layer6.py [--manifest FILE] [--only NAME...]`: sequential, 2 workers/run, 2-min timeout. Manifests: `layer6-runs.json`, `layer6-clear-runs.json`, `RunStop-runs.json`, `SetupWalk-runs.json`, `SetupChoices-runs.json`, `SetupChoicesWalk-runs.json`, `SetupHandoff-runs.json` (entries: module, cfg, outcome `pass`/`invariant`/`temporal`).

### Generated files

A witness is the module renamed plus exact text replacements; a replacement missing its expected count stops the script. Edit the module, rerun the generator. `witnesses/Runner*` are hand-written.

- `make-witnesses.py`: `Store.cfg`, `StoreRecords.cfg`, `witnesses/Store*`, store `reach/`.
- `make-card-witnesses.py` (seven `Card*.cfg`, `witnesses/Card*`, `reach/Card*.cfg`); `make-steps-witnesses.py` (five `Steps*.cfg`, `witnesses/Steps*`, `reach/Steps*.cfg`); `make-policy-witnesses.py`, `make-landedge-witnesses.py` (`witnesses/Policy*`, `witnesses/LandEdge*`, their `reach/`); `make-run-witnesses.py`, `make-run-stop-witnesses.py`, `make-run-clear-witnesses.py` (Run/RunClear cfgs, `witnesses/layer6/`); `SetupChoices-generate.py`, `SetupHandoff-generate.py` (Setup witnesses beside their models); `make-steps-traces.py` (`traces/steps-ok-*`, `traces/steps-bad-*` from TLC logs).

### Layers 1-3

**Store.tla**: one op = one atomic step over `epoch, cards, rows, judgements, cursors, policies, stopped, events, applied`: check epoch and conditions, run writes, append an accepted event; or change nothing and append a refused event. SQL: one transaction, `LOCK TABLE sprint.epoch` to COMMIT at READ COMMITTED (witness `StoreStaleCheck`).
- `Clear`: no writes, epoch-checked, idempotent by id; opens next epoch (from 1), carries rows (held/session/role kept, state `down`, gen+1), keeps cursors, policies, stop; drops cards and judgements. Events append-only (`LogAppendOnly`). A set write that changes nothing is refused.
- Fresh store is stopped, reason "never started" (`FreshMovesNothing`); stop refuses only ops fenced on Running. Cursor never moves back nor past the last seq.
- Conditions: `CardGen`, `CardState`, `HolderIs`, `CardAbsent`, `CardRed`, `NeedsLanded`, `RowIs`, `RowGen`, `RowHeld`, `JudgementOpen`. An op raises each existing card/row it writes by exactly one gen. Caller view: `phase` (`idle`/`unknown`/`done`), `receipt`, `lost`; recovery `Replay` or `Apply`.
- Instance: producers p1 p2, sentinel t1, consumers u1 u2 u3, rows r1 r2, judgement j1, cursor c1, sessions x y, 36 ops; scenario `deal` (`Store.cfg`, `MaxLoss = 1`) or `records` (`StoreRecords.cfg`).
- Invariants: `LogMatchesState`, `ConditionsAtomic`, `NoChangeOnRefusal`, `NoSpuriousRefusal`, `GenPerOp`, `ClearCarriesRows`, `IdempotentById`, `OpsInTheirEpoch`.

**CardMachine.tla**: guard = table's from-state plus conditions; a non-matching event is not enabled (Go refuses, changing nothing). Consumers are slots `<<p, i>>`; Start and AskRead cut the next slot already `working` in its row's hand, guarded by `RoomOn`. ReadYes needs a done ok read (ReadFix a done fix read) of the current head with equal `review`. WaiveRed needs Red, a done ok read of the previous review at the same head, no live consumer.
- Merge: an op is up to `MaxOp` moves, conditions checked against `snap`; `InThisOp` only after an earlier move failed the need. `last` is a step monitor (`StepRule`, `last.bad`). Ids have no dot (`<p>.w<attempt>`, `<p>.r<review>.<n>`).
- Scenarios (cfg): `life` (`CardMachine`), `deps` (`CardDeps`), `pre` (`CardPreempt`), `reads` (`CardReads`), `merge` (`CardMerge`, 3 moves), `mergepre` (`CardMergePre`, 2), `mergedeps` (`CardMergeDeps`, 2); `redw` only in `witnesses/CardWaiveRedWithoutRed.cfg`. Environment moves share a budget; Red spends it.
- Invariants: `WorkingOneWorkConsumer`, `NeverStranded`, `WidthKept`, `DealIsTake`, `NeedsAcyclic`/`OrderAcyclic`, `OneLiveReadPerRow`; step rules `FinalNeverMoves`, `AttemptOnlyOnStart`, `OldAttemptNeverMoves`, `OnlyHolderActs`, `ReadYesNeedsYes`, `ReadFixNeedsFix`, `WaiveRedNeedsRed`, `FencesOnSnapshot`, `ReviewOnEntry`.

**Steps.tla**: state store `w`, view `snap`, `inbox` (edge results only), `inflight` (`<<epoch, kind, card, sha>>`), `js`, `split`, monitor `last`. `CardMachine.tla` cannot be INSTANCEd, so `Steps.tla` restates each guard (`Legal`) and write (`ApplyG`) under the same name; heads are attempt numbers.
- `Tick(a)` is atomic, from the view alone, in order: (1) preemption (ready blocker, every serving row at width: Withdraw youngest working victim, blocker Start, stop request; none open: `unserved`); (2) land results in arrival order; (3) needs Resolve in id order; (4) ReadYes/ReadFix once every read of the review is final; (5) deal and reads (blocker work, owed reads, normal work; row = policy `rows` answer if eligible, else exact tier rank (`escalate_after` bounces: one up; read: one above author's row), then idle lane, most room, name; none: `unserved`/`cannot-read`); (6) land requests per stream with none in flight; (7) judgements (`unserved`, `cannot-read`, one `policy` per failed question).
- Budget counts cards touched. Up to `Pack` groups per op; a group touching a card of a `split` group gets its own op. An op is accepted whole if its records match the plan and every move passes layer 2's guard, else refused whole and its inbox items go back in place.
- Scenarios: `pre`, `pre1` (Pack 1), `deal`, `land`, `burst` (`StepsPre`, `StepsPrePack1`, `StepsDeal`, `StepsLand`, `StepsBurst`). Rows: r1 width 1 tier 1; r2 width 2 tiers 1-2.
- Invariants: `NeverDealtTwice`, `WidthKept`, `ReadyNeverFlipsWithLoad`, `NeverOverlapStarted`, `NeverBelowTier`, `CarryFIFO`, `EdgeAfterAccepted`, `DealtWithinKTicks`; `TickChecks`: `OneGroupPerCard`, `GroupWhole`, `NeverHalfApplied`, `RefusalChangesNothing`, `RefusedOnlyByRace`, `BudgetKept`, `BlockerPreemptsInOneTick`, `PreemptYoungest`, `PolicyFailsToDefault`, `NoDealWithoutPolicy`, `AfterKept`, `AfterNeverHolds`, `MergingCovered`, `ResolveFromState`, `HeldBehindShown`. Liveness `ReadyDealtWhenRoom` under weak fairness on `Snap` and `Tick` only (witness `StepsDealOneShort`, exit 13).
- Fences on predicted gens (`StepsFenceOnView`); preemption counts free width only (`StepsBlockerQueued`).

### Layers 4-6

- `LandEdge.tla`: cards c1 c2 (stream s1), c3 (s2), one base; CI truthful (c1+c3 red). `Pass`: a head holding base's tip gates its own PR, else heads merge onto base's tip in the group branch and its PR gates; a conflict stops the pass; a green gate merges only while base is still the gated tip. Invariants `BaseNeverRed`, `NothingLandsUnpassed`, `MergingCovered`.
- `Runner.tla`: one bench, two consumers, width 2, one slot, one crash; passes and jobs (stage, run, harvest, rebase, push, report, clean) are separate processes. `Stop` keeps jobs at staged (never a fault). Properties `Liveness`, `FinalDirRemoved`; safety `OneLiveHarnessPerJobDir`, `JobDirNeverReused`, `CleanOnlyAfterRecordedAndPushed`, `ReportRecordedAtMostOnce`, `StopIsNeverAFault`, `NoPushAfterFinal`, `BranchOnlyByHolder`.
- `Policy.tla`: versions 1..`MaxVersion` stored in order, never deleted; `Default` built-in, never stored, can fail. `breaks` = failing versions; `Drift` changes it. A tick answers with the first passing version; all fail: `NoAnswer`. Activate is one op fenced at its snapshot seq.
- `Run.tla` (chooser, scheduling, store contract, pusher; cfgs `Run`, `RunFailover`, `RunReports`, `RunStopStart`, `RunInitiallyStopped`): lock exclusivity is an input primitive; `OneChooser` does not prove exclusion across separate health-check and commit calls (witness `RunLockCheckRace` violates `CommitUnderCurrentLock` on purpose). Layer 4 facts must be requeryable by `(kind, card, sha)`. `SendOK` needs exit 0 and a confirmed message id for the recipient; cursor saved after confirmation. A pusher summary caps a subject body at 4 KiB and 20 item lines; omitted items become a count plus `nova-sprint next`, which must list every omitted obligation. A stopped machine plans nothing; accepted tick writes need the store's atomic `AtSeq`; `CaptureFence` saves the observed seq once per pending op. Witness targets `NoTickWhileStopped`, `NoTickAfterObservedStop`, `NoAcceptedTickWhileStopped` (`RunStoppedFenceDropped`).
- `RunClear.tla`: every committed notice is eventually sent, or cancelled by a later Clear and covered by a confirmed per-recipient Clear summary. Summary text: `epoch <n> cleared: <k> pending notices dropped (some may have reached you); run nova-sprint next`. Invariants `CursorHasCoverage`, `NoOldSendAfterClear`; `RunClearCountAmbiguous` (`ClearCountIsUndelivered`) and `RunClearOriginalCancelled` (`EveryOriginalSent`) are accepted limits, not injected bugs.
- `Setup.tla` (variants `SetupWalk`, `SetupRun`, `SetupChoices`, `SetupChoicesWalk`): eight-step resumable walk; every invocation probes from step 1, "verified" never persisted; no secret bytes, only allowed sinks. `Setup.Need(6)` requires services and key; `SetupChoices` makes step 6 a durable `services` choice (`unknown`/`none`/`configured`): `none` has no key obligation, `configured` requires each identity in `KeyQuestions`.
- `SetupHandoff.tla`: two children, no lease spanning both. Phase 1: lease, save credential and schema, persist the record under the lease, release. Phase 2: prevalidate, probe step 1, reacquire, revalidate under the lease, probe step 1 afresh, then the effect (never skips steps 2-8). Invariants `DurableBeforeRecord`, `EffectsOrdered`, `RecordSurvivesCrash`, `ValidatedCurrentRecord`, `ParentProtocolOnly` (parent sees opaque ids and status only).

### Trace validation

Go walks record steps projected onto model variables; TLC checks the recording is a model behaviour. Per model: generated `<Model>TraceLog.tla` (`TraceLog == << r_1, ..., r_n >>`); `<Model>Trace.tla` has variable `l` (last record matched) and `TraceMatched == <>(l = Len(TraceLog))`. Exit 0 = matched; exit 13 leaves `l = k`: record `k+1` is the step the model refuses. Never broaden `Next` or weaken an invariant to accept a trace: diagnose the model/source/mapping mismatch.

Trace modules: `<Model>Trace` for every model above (Steps: partial logging).

```
bash tla/make-traces.sh          # walks write tla/traces/
bash tla/make-traces.sh check    # ... and TLC checks every trace
bash tla/check-trace.sh Store tla/traces/store-fake-1
```
`check-trace.sh <model> <dir>` prints `TRACE OK steps=<n>` or `TRACE REFUSED at k of n: <action> (...)`. A trace cfg has `\* expect: matched` or `\* expect: forbidden at K`; exit 0 expected, 1 not, 2 other TLC failure. CI job `trace` runs `make-traces.sh check` per push with `NOVA_TRACE_PG=1`. Walks write traces only when `NOVA_TRACE_DIR` is set; walk traces (`store-fake-*`, `store-pg-*`, `*-walk-*`) are regenerated. Committed fixtures in `traces/`: `store-breaker`, `card-breaker`, `policy-breaker`, `store-cursor-zero`, `steps-ok-*`/`steps-bad-*`.

Walks and mappings:
- Store (`store/trace_test.go`, `store/trace_gen_test.go`): Fake walk with faults (`Fake.Drop`: `LoseRequest`; lost reply = Apply known=FALSE then Replay/Apply) and a PostgreSQL walk. `store.TraceState` (`store/trace.go`) is the mapping; `store.TraceOp` refuses what the model cannot say. `StoreTrace` lifts `MaxLoss` to 2.
- Card (`card/trace_test.go`, `card.TraceState` in `card/trace.go`); `CardMachineTrace` adds `TraceInit`, `env`, `GoneAt`, `RawHold`.
- Policy: `policy/trace_test.go`; landing/runner: `edge/git`, `edge/worker`.
- Steps (`steps/trace_test.go`, `steps/trace.go`): real steps on `store.Fake`, one random model action per step; `w` logged in parts (`StepsTrace.Parts`).
- Breakers (unit tier, Fake only) write a fixture `check-trace.sh` must refuse at its last step: `store-breaker`, `card-breaker`, `policy-breaker`. `store-cursor-zero` is `matched`.

### Layer 6 trace gate

`python3 tla/check-layer6-traces.py --mode {emit|check|all} --repo /checkout --out /var/tmp/l6-traces` emits traces from the implementation on fixed seeds and checks each via `check-trace.sh`. `emit` needs a clean Git checkout and an output dir outside it. `check` runs from a spec dir with no sample TraceLogs: `python3 <out>/spec/check-layer6-traces.py --mode check --out <out>`. `--go` selects the Go executable. Gate fails on a missing case, mismatched artifact bytes, missing raw log, TLC warning/error, refusal, timeout, record-count mismatch; `check-results.json` keeps verdicts. CI `trace` runs `--mode all` under `nova-reserve`. `test_check_layer6_traces.py` tests the helper.

Cases: `run-0`..`run-2` (`TestEmitRunTrace` in `./run`, `NOVA_RUN_TRACE_SEED`), `run-stop` (`NOVA_RUN_TRACE_STOP=1`) for `RunTrace`; `setup` (`TestSetupActualTrace`, `./verbs`); `handoff-0`, `handoff-1` (`TestEmitHandoffTrace`, `./cmd/nova-sprint`: success, stale epoch); `choices-none`, `choices-configured` (`TestSetupChoicesActualTrace`, `./verbs`).

**RunTrace** consumes `RunTraceLog` (1-based `i`, `action`; `p` process, `r` push, `c` Report, `j` DeliverEdge; `Required(r)` in `RunTrace.tla`). Every record has `physicalSeq` (actual store sequence), separate from the event rank used by `events`, `cursor`, `procFold`; each accepted Stop/Start, report, tick outcome, cursor write advances it by one. `CaptureFence` samples the first appended AtSeq of a pending op once (`expectedSeq`, `procFenced` true); Apply/Refuse carry unchanged `expectedSeq`, `receiptSeq` (= `physicalSeq+1`), `procFenced=false`.

`run/run_trace_test.go` drives `acquireChooser`/`Chooser.Check`, `coldStart`, `Ticker.Tick`, `Ticker.Push`/`Pusher.Push` on `store.Fake`; writes only with `NOVA_RUN_TRACE_PATH` set. Not bound: `AtomicChooser`, unknown Apply outcome, multiple recipients, multi-move ops, `Edges.Dispatch` (`TestStopIsOneMessageThenAck`). Known gap: tick ops carry no AtSeq, so `NOVA_RUN_TRACE_MIDSTOP=1` is refused (`RunStoppedFenceDropped`); not gated.

**SetupTrace** starts at the under-lock walk after the readiness prefix (`SetupWalk.WalkInit`, `TraceInitialAllowed`); `keySinks` are destinations, never bytes; Act maps Do only if the observed state satisfies the postcondition. Real `services=none` needs no key but `Setup.Need(6)` does: never fabricate Ask(key); keep the first refusal and diagnose. Steps 7-8 are bound to `cmd/nova-sprint/setup_workload.go` by `TestWorkloadStepsMatchTheSetupModel` (`*verbs.WorkloadNotReady`).

**SetupChoicesTrace** runs the real walker, `Choices.Save`, `RuntimeSetup.servicesAct`/`EnsureKey`. **SetupHandoffTrace**: a refusal must show no `attemptEffect`; fixture calls the real `runUpChild` twice (stale case: a real `store.Clear` between). **RunClearTrace** is not bound to code.

## Dashboard

Package `dash` serves the sprint board: one embedded page (`dash/page/`) polling `/api/sprint` each second, plus `/healthz`.

Feeds: store polled (`NewRecovering`/`New`, `Cache.Run`; `nova-sprint dash --listen <addr>`); published by `run` (`NewFromFeed`, `Cache.Publish`); pulled from another board (`NewPullCache`, `PullCache.Run`; `--pull <url>`, no store credentials). `--pull` takes `http(s)://host[:port]/path` (no user/query/fragment); `/api/sprint` is appended. Version chip: `dash.Version` (`-ldflags '-X github.com/mas-bandwidth/nova-sprint/dash.Version=v1.2.3'`).

Routes: `/`, `/index.html`; `/public.html` (no charts script); `/api/sprint`; `/healthz` (`ok`, or 503 `stale` once data is 32 s old); `/stream/…`, `/api/stream/…`, `/api/history`, `/events` redirect to `/`; static assets.

A pull board answers GET/HEAD only, limits each client to 60 requests/s (429), serves `/api/sprint` with `ETag` and `Cache-Control: public, max-age=1`, refuses a malformed upstream envelope without refreshing its age, and never lets viewers cause an upstream read. A failed pull keeps the last view, `ok` false, `error` `upstream pull failed`.

**Envelope** (`PageEnvelope`): `ok`, `data`, `fetchedAt`, `dataAgeSeconds`, `attemptAt`, `error`, `readSeconds`, `minInterval` (1), `throughput`, `throughputMinutes`, `build`. Table cells are strings; `unpriced_runs` is numeric. A value the store lacks is absent or `-`, never zero. `data`: `landed`, `all`, `left`, `done`, `epoch`, `at`, `build`, `server_version`, `coordinator`, `machine`, `stopped`, `stop_reason`, `summary`, `eta`, `eta_window_minutes`, `eta_basis`, `eta_landings`, `cost`, `streams`, `stream_costs`, `tables` (`work`, `fleet`, `friends`, `merge`, `readers`).

**Board rules**
- Work: one row per stream counting producers by state `blocked, waiting, ready, working, review, fix, merging, landed, fail`. `ready` after its first attempt, or `working` at attempt 2+, counts as `fix` unless priority is blocker or critical. Unknown state counts as `waiting`, logs `ALARM dashboard: producer "<id>" has unknown state "<state>"`.
- Fleet/Friends: every roster worker/kin is a row, `down` until the store has it. Fix track counts working work consumers at attempt 2+; reads count separately.
- ok%: finished work cards with result `ok` over those with `ok`, `fail` or `fix` (faults, no result, close/release, reads not counted); `-` until one counted card. Load: last heartbeat's 1-minute load average over cores as percent; `slots` = `used / total` nova-reserve slots.
- Machine: `RUNNING` or `STOPPED <reason>` (machine chip, ETA tile, banner).
- Costs: API spend alone, by tier, divided by every landed card in this run (imported landings excluded). Seat runs add `seat_tokens`, never dollars. Unpriced runs are counted (`unpriced_runs`, `missing_prices`). Totals round up to cents.
- ETA: rate from the store's landing times, else cache samples over the last hour (reset when landed falls or epoch changes); last hour's landings when at least three, else sprint average (at least ten minutes, three landings), else `-`. `eta_basis`: `last hour` or `sprint average`.

**Freshness and health.** The store-backed cache takes one Snapshot at startup (30 s deadline), then each second folds later events (5 s, `AdvanceDeadline`) and republishes.
- Failed startup Snapshot: `NewRecovering` returns an unavailable cache (`/healthz` 503); `Run` retries (30 s each). A failed advance keeps the last view; data older than 32 s logs `ALARM stale` and makes `/healthz` 503 until a read succeeds. A log restored behind the cache's position triggers a new Snapshot.
- After consuming input the cache saves the global `dashboard` cursor with `SetCursor` and folds through its own receipt; idle polls write no cursor events. A failed cursor write logs a warning and retries.

`work_grid_layout_test.go` recomputes the Work grid from `page/index.html`.

**Tests.** Unit: `go test -skip Functional ./dash`. `testdata/seed.json` is the golden for `TestFunctionalJSONGolden`; regenerate with `UPDATE_GOLDEN=1 go test -run TestFunctionalJSONGolden ./dash` (needs `initdb` and `pg_ctl` on `PATH`); it carries the page build hash, so changes whenever anything under `page/` does. `testdata/reference-envelope.json` is the wire-format type reference. `TestFunctional*` use throwaway PostgreSQL; `TestFunctionalPublicDashboardThousandRequests` needs `NOVA_DASH_LOAD=1`. Timing: `NOVA_LOAD=1 go test -count=1 -race -v -run '^(TestFunctionalLoadRefresh5000|TestFunctionalPG5000IncrementalAdvance)$' ./dash` on a quiet Linux bench; one advance at 5,000 producers under 100 ms.

## Tools

### tools/abshot

Renders two dashboard URLs in headless Chromium at each width, captures PNGs and a pixel-diff overlay, and attributes differing regions to DOM elements and text. Stdlib only (`cdp.go`).

`go run ./tools/abshot --a <url> --b <url> --out <dir> [--widths 1440,2000,2800] [flags]`

| Flag | Default | Meaning |
|---|---|---|
| `--a`, `--b`, `--out` | required | baseline URL, candidate URL, output dir |
| `--widths` | `1440,2000,2800` | viewport widths, each 200-10000 |
| `--channel-tol` | `8` | per-channel delta (0-255) counted identical |
| `--pixel-tol` | `64` | differing pixels allowed per width |
| `--settle` | `1500ms` | wait after load |
| `--tz` | `America/New_York` | IANA timezone |
| `--chrome` | none | browser path; else `ABSHOT_CHROME`; else first of `chrome-headless-shell`, `google-chrome`, `chromium`, `chrome` on `PATH` |

3-minute limit.

Pages load at `<width>x1000`; after `--settle` the viewport height becomes `max(1000, ceil(scrollHeight))`. Output in `<out>`: `<width>-a.png`, `<width>-b.png`, `<width>-diff.png` (differing pixels red `#ff3050`), `report.md` (summary table, then per differing region `(x, y, w, h)`, `diff_px`, `A has`/`B has`; `(absent)` = region missing from one board). Exit 0 all pass; 1 a width exceeds `--pixel-tol`; 2 usage/runtime error.
