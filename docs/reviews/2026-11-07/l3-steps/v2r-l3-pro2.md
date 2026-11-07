# Layer 3 (steps) cold review, v2r-l3-pro2

Checkout `43d849e2` (branch v2-stitch). Read: docs/LAYER-3-STEPS.md, the L3 rows of docs/ENFORCED.md, every non-test file of `steps/` (tick, pack, group, deal, preempt, reports, request, needs, holds, limits, fix, land, judge, hand, model, asker, view, inputs, roster, waits skimmed), the tests that bear on each finding, and the shape of tla/Steps.tla.
`go build ./... && go vet ./...` clean. `go test -count=1 ./steps/` passes (89 s under load, about 19 s alone, nearly all `TestTraceWalk`: 100 seeds x 2000 steps x scenarios, deterministic, no sockets). `TestLoad` and `TestLoadResolve` SKIP here (no `initdb`), so guarantee 11's cost bound was not exercised. Nothing in the unit tests uses sockets or services.
Findings marked (run) were reproduced with a throwaway probe test, deleted before this commit; the output is pasted. The rest are read from the cited lines.

## Verdict

The layer is a tidy, deterministic planner whose central machinery (group claims, packer, budget, carry, op ids, fold equals build) holds up, and whose tests and model are real. It is not sound against its own doc in the places that sit outside the model. The worst single thing is a stale-candidate bug: `questions()` fills the per-tier candidate cache at the top of the tick, before any step claims a row, so the deal can cut a consumer on a row that the same tick marks `Gone` (defect 1, reproduced; the store accepts the op). Close behind: `Verb`'s actor rule is a deny-list, so a non-coordinator can raise `Start` and `Resolve` (defect 2, reproduced); a policy that answers `rows` validly raises a false `policy` judgement every tick that all rows are full (defect 3, reproduced); a withdrawn read silently lowers the review quorum (defect 4, reproduced); a blocker is never preempted when policy's `work` is below the row width (defect 5, reproduced); and layer 5's steering numbers are accepted and ignored (defect 6).

## Defects

Ranked, most severe first. Severity: blocker | serious | minor | nit. None is a blocker (no data corruption path was found inside the layer); defects 1 and 2 would be blockers if layer 6 does not catch them (see Questions).

1. **serious. A row can be dealt to and sent `Gone` in the same tick (run).** `tick.questions()` (tick.go:145, via `t.candidates`, tick.go:216-229) builds `candCache` before any step runs, so the `t.unclaimed(n)` filter at tick.go:223 sees no claims. `limits()` then claims the row with `t.group("gone", "", t.rowMove(n, card.Gone{}))` (limits.go:63-64; `add` claims `g.key.Row`, tick.go:103-105), and `deal()` reads the same cache through `serving` (preempt.go:69-76) and cuts a Start on that row. The comment at tick.go:214-215 ("row claims are made before the deal") is true only for the claim, not for the cache. Why it matters: contradicts "a row ... not hooked in is never dealt to" (ENFORCED L3-E1), and the work sits on a dead row for the whole redeal grace (`RowSilentGone`, 10 min, holds.go:29). Repro: row r1 (width 2) with `Seen` 11 minutes old, one ready producer, `Tick(w.v, in, benchAsk{})`:
   ```
   OP t1.6.9b2f027395c4a104 kind=tick
      store.SetRowState {Row:r1 State:down}
      store.SetRowFlaps {Row:r1 Flaps:1}
      store.SetCardState {Card:p1 State:working}
      ... store.PutCard {ID:p1.w1 Kind:work Of:p1 ... Holder:r1 ...}
   apply t1.6.9b2f027395c4a104 true
   ```
   Repair: do not cache claim-dependent filtering (cache the claim-free list at the top, filter `t.unclaimed(n)` on every read in `candidates`/`serving`), or run `limits()`'s row groups before `questions()`.

2. **serious. `Verb`'s actor rule is a deny-list; engine events are open to anyone (run).** `coordinatorEvents` (request.go:35) lists only the human verbs; `actorOK` (request.go:85-110) lets every other event through for any actor. `Start`, `Resolve`, `Park`, `Finished`, `ReadYes`, `Reached`, `AskReads`, `NeedFailed`, `Released` are the tick's own events and are not listed. LAYER-3 "Requests" says the per-card verbs are the coordinator's, Report the holder's, Add anyone. Repro (actor `mallory`, a non-coordinator, `w.verb(Request{Actor:"mallory", Card:"p1", Event: ev})`):
   ```
   card.Resolve -> ACCEPTED as an op
   card.Start{Row:"r1", Branch:"b", Base:base, Model:"m"} -> ACCEPTED as an op
   card.Park / Finished / ReadYes / Reached -> refused only by the card machine's state check
   ```
   `TestVerbActorTable` (request_test.go:29-72) checks only the listed events, which is why this passes. Repair: invert it: an allowlist of events a non-coordinator may raise (`Add`; `Report` by its holder), everything else the coordinator's.

3. **serious. A valid `rows` answer is recorded as a policy failure whenever no candidate has room (run).** `pickRows` (deal.go:223-226) calls `t.o.refuse("rows")` when `answered` names no row in `rows`, but `rows` is `withRoom(named)` (deal.go:121; dealRead passes the same list to `pickRows`), which is empty exactly when the fleet is saturated and a queue exists. `RowsQuestion` documents candidates "with room" (asker.go:51-52) while `candidates` (tick.go:216) filters none for room, so the policy cannot know. Result: one `policy.rows.N` judgement ("policy vN failed question rows") with a deadline, every hour, for a correct policy. Repro: 1 row, width 1, p1 working, p2 ready, `Answers{Version:1, Rows:{"p2":{"r1"}}}`: plan contains `PutJudgement {ID:policy.rows.1 ... failed question rows}`; the control with width 2 (room) contains none. Repair: refuse only when the answered rows are absent from `named` (the offered candidates), not from `withRoom(named)`; or offer candidates with room.

4. **serious. A withdrawn read silently lowers the review quorum (run).** `settled` (fix.go:46-57) returns the done reads once nothing is live, and `fix()` (fix.go:16-32) then plans `ReadYes` on them; it never compares the count with `readsWanted`. `owed` (fix.go:77-88) says a withdrawn read "is owed again", but `fix` runs before `deal` and the review has no live consumer, so the verdict fires first. LAYER-3 "Fix" says "once every read ... is done or withdrawn", which is the text being followed, and contradicts "a withdrawn one is owed again" in the same file. Repro: policy `Reads{p1:2}`, two reads cut, r1 reports ok, the second is `Withdraw`n, next `Tick`: `p1 = merging` with one ok read where policy wanted two. Repair: `settled` returns nil while `t.owed(c) > 0`, so the owed read is cut first.

5. **serious. Preemption and the deal disagree about room, so a blocker can wait forever (run).** `noFreeWidth` (preempt.go:80-87) compares `Working` with the roster width; the deal's `withRoom` (deal.go:171-179) compares live plus tally with policy's `width.Work` (deal.go:184-190), which may be below the width. With width 2, `Work` 1 and one working consumer, `preempt` says "the deal starts it" (preempt.go:33-35) and the deal says no room. Guarantee 4 ("a ready blocker with no room is dealt by preemption in the tick that finds it ready") fails. Repro: r1 width 2, p1 working, blocker `b1` ready, `Answers{Version:1, Width:{"r1":{Work:1}}}`, three ticks: `tick 0..2 b1 ready p1 working`. The doc's own guarantee 2 text counts "width minus working" while the deal counts live. Repair: `noFreeWidth` uses `t.withRoom`/`t.room` (one definition of room), and the model gets a `Work < Width` constant.

6. **serious. Layer 5's steering numbers are accepted and ignored.** LAYER-5-POLICY.md:888-897 says `limits()` carries `escalate_after`, `frontier_after`, `flap_limit`, `idle_floor`, `land_target` and that layer 3 reads them. `Limits` (asker.go:108-111) has none of them and the code reads the global `Engine` (deal.go:302,304; limits.go:48-49; judge.go:206,262-268). `policy/policy.go:92-101` defines them and `policy/validate.go:179` validates them, so a policy that sets `flap_limit 5` is validated, stored and has no effect. Repair: add the five fields to `Limits`/`limitsOK`, read `t.lim`.

7. **serious. A group refused alone is replanned forever, with no judgement (tick step 7 unkept).** LAYER-3 "The tick" step 7: "A group refused alone on fresh state is left out until its cards change, with an `engine` judgement naming the plan and the store's reason." The only consumer of `Inputs.Refused` is `ops()` (pack.go:105-113), which acts only on `len(r.Groups) > 1`; `RefusedOp.Reason` (inputs.go:62-65) is never read (`grep Refused steps/*.go`: pack.go:105 only). A group that the store refuses for a rule the steps do not pre-check is rebuilt, identically, and refused every tick. Repair: when `len(r.Groups) == 1`, skip that group's cards until `touched` moves past the refused seq and open `engine` with `r.Reason`.

8. **minor. Reports are outside the budget tally, so a deal can be planned and then dropped by the packer, and `undealt` blames the engine.** `reportGroup` appends with `t.add` (reports.go:85-86), not `t.group`, so `t.planned` (tick.go:123) excludes them, while `admit` counts them in `touched` (pack.go:88). After a burst of reports the deal plans starts (`overBudget` false, tick.go:80) that `admit` drops; `undealt` (hand.go:109-111) then prints "an engine defect" instead of "budget spent". Repair: count `g.size()` into `t.planned` in `reportGroup`.

9. **minor. A land `landed` result that the card machine refuses is never consumed.** `landGroup` (land.go:69-71) does nothing when `t.group` returns nil on a refusal (tick.go:117-121 opens `engine` once); no `none` group is added, so the inbox item stays in `Carry` and is re-tried and re-refused every tick, forever, with the request stuck in flight (`busy`, land.go:168). Repair: on a nil group that was not budget-nil, `consume(it.ID)` with a refusal answer.

10. **minor. A land result for an unknown request is dropped silently.** land.go:30-33: `!ok` -> `consume`. If layer 6 restarts and rebuilds `Inputs.Inflight` without the request, a `landed` result is thrown away; the cards stay merging, a new request with the same id (land.go:233) is issued and the PR may be merged twice. Repair: unknown-request `landed` results open an `engine` judgement instead of vanishing (see Questions).

11. **minor. `drops()` ranges a map, so op bodies, and so op ids, are not deterministic.** needs.go:123 `for id := range t.v.consOf[d]` appends withdraw moves in map order; `opID` hashes writes in order (pack.go:161-170), against "one view, one id". request.go:246 sorts for the same job. It also visits every failed producer and its dependants every tick (needs.go:109), O(failed history) per tick. Repair: `sorted(...)`, and drive from dependants of cards that went `fail` since the last tick.

12. **minor. The fault-rest rule is not "3 of the last 10".** `rest` (limits.go:71-87) counts every fault in the 30 minute window; `RouteRestWindow` is validated (asker.go:318) but never read, and `v.Faults` (view.go:153) grows without bound and is rescanned each tick. LAYER-3 "Retire and fail limits": "3 faults of the last 10 on a row in 30 minutes". A row with 3 faults and 200 good reports rests. Repair: keep the last `RouteRestWindow` outcomes per row.

13. **minor. `RowFacts.Model`/`Harness` are never filled, and the read "different model" preference does not exist.** `rowFacts` (tick.go:184) leaves them empty; `ranked` (deal.go:256-281) has no model term. LAYER-3 "Deal and reads": "for a read, one read on a row whose model differs from the author's when one is open (LESSONS 3.8)". Repair: set `Model` from `t.model`, add the term to `score`.

14. **minor. Guarantee 11 (O(G log C + R)) is not what the code does.** `questions()` builds `Order` and `Rows` for every ready producer (tick.go:139-146) and `cands()` builds, keys and sorts every ready producer and review each tick (deal.go:52-73), then the deal loop walks them all past the budget. That is O(ready) per tick, and policy is asked about all of them (the 500 ms `AskBudget`). The load test that would show it is skipped here. Repair: bound `questions()` in tick.go and `cands()` in deal.go to the first `t.lim.Budget` ready or review producers in ladder order and order/ask only those, leaving the rest for the next tick, so per-tick work is O(Budget log Budget) instead of O(ready).

15. **minor. The judgement op is outside the budget and the pack.** pack.go:51-53 appends one op of every judgement and expiry; judge.go:57-59 notes 4,908 overlap judgements in one tick. That is one 5,000-write op against "ceil(G/pack) ops". Repair: chunk it by `Pack`.

17. **nit. Dead or hard-coded config.** `Faults`, `SameFinding`, `ReportsPerTick`, `RouteRestWindow` are validated (asker.go:317-318) and never read; `IdleCeiling` is a literal 3/4 (judge.go:202). `fix.go:36` judges `brief` on the first repeat, not at `SameFinding` 2. Repair: wire the validated-but-unread fields where they belong — `RouteRestWindow` and `Faults` in `rest()` in limits.go, `SameFinding` as the repeat threshold in `fix()` in fix.go, `Engine.IdleCeiling` in the idle check in judge.go and `ReportsPerTick` in `reports()` in reports.go — or remove any field left unread from `Limits` and `limitsOK` in asker.go.

18. **nit. Restart resets the redeal and unacked clocks.** `downAt` (holds.go:48-53) and `dealtAt` (hand.go:40-45) fall back to `v.Since`, so an engine that restarts more often than every 10 minutes never redeals a down row's work. Repair: make `downAt` in holds.go and `dealtAt` in hand.go return the zero time when `Down`/`TakenAt` has no witness instead of `v.Since`, so `RowSilentGone` and the nudge/unacked bounds are already elapsed after a restart rather than restarting.

19. **nit. `eligible`'s swap loop (land.go:203-213) does not terminate on an `after` cycle.** Unreachable through the card machine (`Add` and `Relink` refuse cycles), reachable by a raw store write; waits.go says so for its own scan. A pass counter bound removes the risk.

## Promise gaps

- "A group refused alone on fresh state is left out until its cards change, with an `engine` judgement" (LAYER-3 The tick, step 7): not implemented (defect 7).
- "No policy answer, absence or error makes a step plan an illegal move or plan nothing where the default would have planned" (guarantee 10): a valid answer opens a failure judgement (defect 3); a quorum change is not a policy matter (defect 4).
- "A ready blocker with no room is dealt by preemption in the tick that finds it ready" (guarantee 4): fails with policy `width` below the roster width (defect 5).
- "Every non-final producer is waited on by a named actor" (guarantee 9) / ENFORCED L3-E1 "a row ... not hooked in is never dealt to": a row sent Gone this tick is still dealt to (defect 1).
- "The actor rule ... coordinator's" (Requests): deny-list (defect 2).
- "Policy: `escalate_after`, `flap_limit`, `idle_floor`, `land_target`" (Escalation, Retire, Judgements): constants (defect 6).
- "3 faults of the last 10 on a row in 30 minutes rest it" (Retire): window ignored (defect 12).
- "Nothing per tick is O(C) after the cold build" (guarantee 11): O(ready) per tick (defect 14).
- "Every report and take that arrives is applied or answered" (guarantee 7): a poisoned land item is neither (defect 9); a report refused by `reportGroup` is answered (fine).
- "for a read, one read on a row whose model differs" (Deal and reads): absent (defect 13).

## Model vs code

- tla/Steps.tla `Admit` (line 532-540) matches `admit` (pack.go:80-97) exactly, including blockers passing after `stop`. The model counts blocker size in `t` as the code does; the planner's `t.planned` (tick.go:123) does not, and reports are in neither (defect 8).
- The model has no row groups: `Gone`, silent-withdraw, flaps, `rest`, and the row claim (`GroupKey.Row`) are not modelled, so defect 1 (a row claimed and dealt in one tick) is invisible to `Steps.tla` and to `TestTraceWalk`. Also not modelled: the verdict/quorum (0 hits for `Verdict`/`Quorum` in Steps.tla), sentinels (0), nudge/unacked, the stall/idle/land-rate alarms, and policy `width.Work < Width` (defect 5).
- ENFORCED L3-T1 cites `test TestTraceFixtures` for "the walk's traces are behaviours of Steps.tla". That Go test (trace_test.go:536-558) writes traces only when `NOVA_TRACE_DIR` is set and otherwise only runs the walk; the TLC check is `bash tla/make-traces.sh check` in CI (.github/workflows/ci.yml:127). The row should cite the script, or the test should skip when the variable is absent, so green does not mean checked.
- Guarantee 11 (cost) has no model; its only check is the skipped load test.

## Simplifications

- `Engine` is a mutable package var read from six files; fold the steering numbers into `Limits` (defect 6) and the four unused `Limits` fields can go (defect 17).
- `rowsAnswer`, `pickRow`, `pickRows`, `preferred`, `named` repeat one idea (policy's answer, filtered, else ranked) in three places with three different filters (preempt.go:107-118 refuses on a different condition from deal.go:224). One function that returns the usable answered rows would remove defects 3 and the divergence.
- `ranked` has a separate n==1 path (deal.go:~275) that duplicates the sort's score; a stable sort of the (usually short) candidate list is the same code.
- Two ways to put a group in the tick: `group()` (budgeted, counted) and `add()` direct (reports.go:85, `consume`, `refuse`). The second is why defect 8 exists.
- `room`, `used`, `Working`, `width`, `Live` are four notions of occupancy (deal.go:171-190, view.go:275, preempt.go:80, judge.go:274); one `occupancy(row)` would do.
- `leastHolder`/`heldBehind`/`shares`: `shares` (needs.go:96) is called only by a test (walk_test.go:417); move it there.

## Missing tests

- A Verb by a non-coordinator for each engine event (defect 2); the table covers only listed events.
- `Tick` with a silent row and a ready producer in its tier: no Start on a row that the same plan sends Gone (defect 1).
- A policy that answers `rows` while every row is full: no `policy` judgement (defect 3).
- `Reads` 2 with one done and one withdrawn: no `ReadYes` until the owed read is cut (defect 4).
- Blocker with policy `Work` below width: preempts (defect 5).
- A single-group refused op in `Inputs.Refused`: the group is left out and `engine` is opened (defect 7).
- A `landed` result whose Landed move the machine refuses: item consumed (defect 9); a landed result with an unknown request (defect 10).
- Two live consumers of a dependant under `drops()`: same op id on two runs (defect 11).
- Policy `limits()` carrying `flap_limit`/`escalate_after`: the steps obey (defect 6).
- `TestLoad`/`TestLoadResolve` need PG and skip; a pure-Go cost test of `Tick` at 5,000 ready producers (planning only, no store) would keep guarantee 11 checked in unit CI.

## Questions for the coordinator

1. Does layer 6 gate `Verb` by its own event allowlist before calling `steps.Verb`? If it does, defect 2 is a defence-in-depth gap; if not, it is a blocker.
2. Is `Inputs.Inflight` rebuilt from the store after a restart, or can a landed result arrive for a request layer 3 no longer knows (defect 10)? The land request id (land.go:233) is the same after a `retry` when nothing changed; does layer 4 treat a repeated id as new work?
3. Defect 4: is "withdrawn counts as final, so the verdict uses what is done" the intended LAYER-3 reading (the doc says so) or the bug (`owed` says the opposite)? I read the contradiction as a bug.
4. Reports run before preemption in code and after it in the doc/model order: which is intended?
5. Are the bounce limit (3 in the Retire text, 6 in asker.go and LAYER-5) and the silence bound (2 hours vs 1800 s) decided numbers? I could not tell which side of the doc is current.
6. Guarantee 11 is stated for the steady state; is a tick that asks policy about every ready producer (defect 14) accepted at 5,000 ready, given `AskBudget` is 500 ms?
7. Doc and code disagree. bounce limit "3" (LAYER-3:219) vs `Bounces: 6` (asker.go:157, doc line 476); silence "2 hours" (doc:219) vs `RowSilentWithdraw: 1800` (asker.go:158); tick order lists preemption before reports (doc:284-290) while `Tick` runs `reports()` then `preempt()` (tick.go:53-54); `RowsQuestion` comment says candidates are "with room" (asker.go:51-52). Which side is current in each case?
