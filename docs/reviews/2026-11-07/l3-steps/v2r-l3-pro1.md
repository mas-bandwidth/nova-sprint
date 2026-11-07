# Cold review, layer 3 (steps), v2r-l3-pro1

Reviewer: rowan (sonnet). Base 43d849e2 (v2-stitch). Read: docs/LAYER-3-STEPS.md, docs/ENFORCED.md section 7, every non-test file of `steps/` (about 4,100 lines), `tla/Steps.tla` (tick, expiry, invariants), the trace mapping, and the tests' names and set-ups. Ran `go build ./... && go vet ./...` (both clean, no output) and `go test ./steps/` (ok, 86 s; slowest TestTraceWalk 20.7 s, TestWalk 4 subtests about 3.5 s each; none touches a socket or the real clock, TestLintPure enforces it). Four findings below were reproduced with a throwaway test in `steps/` (removed before commit; only this file changed); each says so.

## Verdict

The layer is structurally sound: pure, deterministic, one group per card per tick, ops named by body hash, the budget and carry logic matches the model, and the happy, fix, land, hold and preemption paths are well tested. It is not yet sound at its edges. The single worst thing is the per-tick candidate cache (`steps/tick.go:216-228`) that is filled while the tick gathers its policy questions (`tick.go:49`), before the rested rows (`tick.go:51`) and before any row claim exist, so the deal offers rows that the same tick has just marked Gone or rested: a card is cut working on a row that goes down in the same op (reproduced), and the LPM R34 fault rest never filters anything (reproduced). Behind that stand a documented promise that is not implemented (a group refused alone on fresh state is to be left out with an `engine` judgement; the code retries it every tick forever), a valid policy answer that is reported as a policy failure, a blocker that waits silently when policy's `work` is under the row width, and the layer 2 preemption pair guard that is never engaged because the steps never set `Withdraw.Preempt`.

Count: blocker 0, serious 6, minor 7, nit 2.

## Defects

Ranked, most severe first. "Reproduced" means a throwaway test in `steps/` (removed) showed it.

1. **serious. The candidate cache is built before rest and before claims, so rested and just-claimed rows are dealt to.**
   `steps/tick.go:49` runs `t.questions()` first (to feed the asker), and `questions()` calls `t.candidates` (`tick.go:~143-146`), which fills `candCache` (`tick.go:219-228`) with the rows that are open, serve the tier, and are `unclaimed` at that instant. Only afterwards does `Tick` run `t.rest()` (`tick.go:51`, which needs `t.lim` from the oracle, so it cannot run earlier) and the steps that claim rows (`limits.go:64` Gone). `deal`, `preempt`, `undealt` and `cleared("unserved")` then read the stale cache through `serving` (`preempt.go:69-76`). The comment at `tick.go:213-215` ("row claims are made before the deal") is false for any tier that has a ready producer, because `questions()` already cached it.
   Matters: (a) LPM R34 / LAYER-3 "Retire and fail limits" ("a deal filter in the view") is dead: three faults on r1 within the window and a ready producer is still dealt to r1 (reproduced: `s.v.Faults["r1"]` with 3 recent entries, next tick `p1` is `working`). (b) A row that the same tick marks `Gone` is still dealt to: reproduced, one accepted op containing group `gone{row r1}` and group `start{p1, p1.w1}`, afterwards row r1 `down`, `p1.w1` `working` holder r1 (the work then waits until `holds.go:29` withdraws it after RowSilentGone, 600 s). It also defeats `candidates`' own `t.unclaimed(n)` test (`tick.go:222`) and guarantee 2/`NeverBelowTier`-style intent "a row not open is never dealt to".
   Repair: build the candidate cache after `rest()` and after the row-claiming steps (ask the asker later, or recompute `candidates` at the deal rather than caching across claims), or filter `rested`/`claimed` at the point of use.

2. **serious. A group refused alone on fresh state is not left out, and no `engine` judgement is raised.**
   Doc "The tick" step 7: "A group refused alone on fresh state is left out until its cards change, with an `engine` judgement naming the plan and the store's reason". `Inputs.Refused` is read in exactly one place, `steps/pack.go:105-113`, and only for ops with `len(r.Groups) > 1`. A refused op of one group (`len == 1`) is simply planned again the next tick, with the same body and fence, and (for an inbox item, `sim_test.go` puts it back) the item is replayed too. A deterministic refusal (a rule the pre-checks missed) therefore loops forever, one refused op a tick, with nothing for the coordinator to see; `RefusedOnlyByRace` (L3-E2) only holds in the model because the model has no such rule gap. No test covers a single-group refusal (TestTickRefusedOpSplitByGroup and TestTickPreemptionRefusedByRace use multi-group ops).
   Repair: in `ops()` (or a step before it) treat a `RefusedOp` with one group whose cards have not changed gen since as skipped, and `t.engine(card, reason)` with `RefusedOp.Reason`.

3. **serious. A valid policy `rows` answer is reported as a policy failure when the row it names is full.**
   `steps/deal.go:224-226` (`pickRows`): `answered != nil && !ContainsFunc(answered, in rows)` calls `t.o.refuse("rows")`, where `rows` is already `withRoom(named)`. So when policy names `r1` (a legitimate candidate, validated by `oracle.rows` at `asker.go:229-245`) and `r1` is at its `work` limit, the answer is counted as a miss, a `policy` judgement opens and the default answers. Reproduced: Answers{Version 1, Rows {p1:[r1], p2:[r1]}, row r1 width 1}; after `run(4)` there is an open `policy.rows.1` with question "policy v1 failed question rows: ; the default answered this tick: run nova-sprint policy check" (the reason after the colon is empty because nothing failed). A busy fleet with any `rows` policy raises a false `policy` alarm every time the preferred row is full; layer 5's contract (LAYER-5 `rows`, "those with room first, all of them when none has room") treats a full row as valid.
   Repair: only `refuse("rows")` for a name not in the offered candidates (already done in `oracle.rows`); a full row is just "no room".

4. **serious. Preemption and the deal measure "room" differently, so a blocker can sit ready with no preemption and no `undealt` judgement.**
   `preempt.go:80-87` (`noFreeWidth`) compares `Working(r)` with the roster width (`t.width`); the deal's `withRoom` (`deal.go:171-181`) uses `used(r) < room(r)` with `room` = policy's `width.Work` (`deal.go:184-191`), and `Work` may be anything from 0 to the width (`asker.go:279-288`). With `Work < width` and `Working >= Work`: preempt says "the deal starts it" (`preempt.go:33-35`), the deal finds no room (`deal.go:121`), and `hand.go:104` (`undealt`) counts room the same way as the deal so it says nothing either. Reproduced: row width 2, policy `Work: 1`, one card working, a `blocker` producer ready: after 5 ticks 50 s apart it is still `ready`, `p1.w1` working, no `undealt` judgement. Guarantee 4 ("A ready blocker with no room is dealt by preemption in the tick that finds it ready") and guarantee 9 (named waiter) both break under a legal policy answer.
   Repair: `noFreeWidth` should use `room(r)` and the same `used(r)`.

5. **serious. The steps never set `Withdraw.Preempt`, so layer 2's preemption pair guard never runs on the engine's own preemptions.**
   `card/card.go:161-166` ("Preempt marks the blocker's preempt. The pair guard and the working-consumer check read this field") and `card/replay.go:537,579-583` ("Only a Withdraw with Preempt set is checked") and `card/move.go:1156`. `steps/group.go:253` builds `card.Withdraw{Reason, Head, Stream}` for every withdraw and `steps/preempt.go:58` uses it unchanged; `grep -rn "Preempt:"` finds no setter anywhere in the repository. The guard that a preempt's victim is `working` and on the same row as the blocker's Start is therefore a no-op for the one caller that exists, and the step's own pre-checks (`preempt.go:91-103`) are the only defence, against the layer-2 doc ("A Withdraw whose reason is preempt refuses unless the consumer is ...", LAYER-2 line 475).
   Repair: `preemptOn` builds the withdraw with `Preempt: true` (a `withdrawPreempt` variant of `t.withdraw`).

6. **serious. Policy's steering keys are validated by layer 5 and then ignored here.**
   `policy/validate.go:178-205` requires `escalate_after`, `frontier_after`, `flap_limit`, `idle_floor`, `land_target` (and `faults`, `same_finding`, `reports_per_tick`, `route_rest_window`) in `limits()`, and LAYER-3 "Escalation" says "Policy: `rows`, `escalate_after`". `steps.Limits` (`asker.go:108-111`) carries none of the first five, and the steps read `Engine.EscalateAfter/FrontierAfter` (`deal.go:302-304`), `Engine.FlapLimit` (`limits.go:48-49`), `Engine.IdleFloor` (`judge.go:268`) and `Engine.LandTarget` (`judge.go:206,262`) directly. Of the other four, `lim.Faults`, `SameFinding`, `ReportsPerTick`, `RouteRestWindow` are never read (`grep "lim\." steps/*.go` lists only Budget, Pack, Bounces, RouteRestAfter). So a policy author changing `idle_floor` or `escalate_after` sees `policy set` succeed and nothing change. The Built section admits `escalate_after` is the engine's but says nothing of the others.
   Repair: carry the keys in `steps.Limits` and read them from `t.lim`, or reduce layer 5's required key list; either way one source.

7. **minor. `owed` is computed with the engine default before the oracle exists, so policy's `reads` > 1 can lose its `rows` question.**
   `tick.go:49` evaluates `t.questions()` while `t.o` is nil; `questions()` gates the read-rows question on `t.owed(c) > 0` (`tick.go:151`), and `owed` -> `readsWanted` (`fix.go:92-95`) returns `Engine.Reads` (1) when `t.o == nil`. After the oracle exists `owed` uses policy's count (`cands` `deal.go:58`). For a review with 2 reads live and policy `reads` 3 (or a withdrawn read being asked again) the question has no `Rows` entry for the card, `oracle.rows` sees it absent (`asker.go:230-233`), records a miss, and a false `policy` judgement opens (same symptom as 3). Not run; follows from the cited lines.
   Repair: ask `reads` in a first call (or two-phase the questions), or build `Questions.Rows` for every review and let owed gate at the deal.

8. **minor. Reports run before preemption, against the doc's tick list and the model; the preemption then evicts a non-youngest consumer.**
   `tick.go:53-54` runs `reports()` then `preempt()`; "The tick" step 2 and `Steps.tla` (`Tick`: `pre == PreStep`, then `InboxFold(V, inbox, 1, ... Cards(pre.gs))`) put preemption first. The Built text says the order is deliberate ("a consumer that has reported is never preempted") but then `victim()` (`preempt.go:91-103`) skips the claimed youngest and takes the next one. Reproduced: r1 width 2, p1.w1 then p2.w1 (youngest) working, a `blocker` ready, p2.w1 reports ok in the same tick: the op evicts p1.w1 (`pre` group cards p1, p1.w1, pb, pb.w1) while p2.w1 finishes, so two consumers leave the row in one tick and the doc's "youngest" promise (L3-G4 `PreemptYoungest`) is not what the code does.
   Repair: either preempt first as the model has it, or when the youngest is claimed by a report this tick do not preempt at all (the report frees the slot; the deal starts the blocker next tick).

9. **minor. `admit`'s "first non-blocker group always" is spent by a zero-size `none` group.**
   `pack.go:84-95`: `case !stop && (nonBlockers == 0 || ...)`; a `none` group (a refusal or ack for an inbox item, `reports.go:27-37`, size 0 since `key.Cards` is empty) is a non-blocker, so `nonBlockers` becomes 1 and a following group larger than the budget (a start is 2 cards, `Budget` may be 1 per `asker.go:318`; a drop closure may be hundreds) fails `touched+size <= Budget` and `stop` is set. The group is retried the next tick and starves only while a refusal/ack arrives every tick before it (layer 4 may redeliver, doc "so layer 4 may redeliver"). Not run.
   Repair: count only groups with `size() > 0` in `nonBlockers`.

10. **minor. `eligible` never terminates on a mutual `after` pair.**
    `steps/land.go:203-213`: the `for changed` loop swaps `out[i]` and `out[j]` whenever `out[i].After` contains `out[j]`; for a cycle a<->b it swaps forever. Reproduced: two merging cards with `After` each other, `eligible` did not return in 2 s. `card.addCycle` / `Relink` refuse cycles (`card/add.go:28`, `card/move.go:886`), so only a store-level write can create one, but `waits.go:140-141` and `TestWaitsSurvivesACycle` already guard the same case ("a store op could still write one"); here a hang of the whole tick has no guard and no timeout.
    Repair: use a topological sort with a visited set, or cap the passes at `len(out)`.

11. **minor. A `landed` result can be consumed with no move, and any land result can be lost with its judgement.**
    `land.go:57-68`: `landGroup` skips every card with `!t.free(p)` (a card claimed by an earlier step this tick) exactly as it skips a stale head, and when nothing is left it calls `t.consume(it.ID)` — so a landed result whose cards were merely claimed this tick (not stale) is dropped, the merge sha is lost, and the card is re-requested next tick under the same request id (`land.go:233`). Similarly `landJudge` (`land.go:77-92`) consumes the item (`land.go:38`) while its `land` judgement is written by the separate `judgementOp` (`pack.go:55-57`): if that op is refused by a race the result is gone and the next tick just asks the landing again. Both are recovered by `landRequests`, at the cost of another CI run; neither is a lost card.
    Repair: carry the item when the skip is only the claim; put the judgement into the item's group.

12. **minor. The fault rest is not "3 of the last 10" and the fault list grows without bound.**
    Doc: "`fault_limit` (3 faults of the last 10 on a row in 30 minutes rest it 30 minutes ...)". `limits.go:71-87` counts any faults in the last `RouteRestFor` seconds and never reads `RouteRestWindow`; `view.go:153` appends every fault forever (`Faults map[string][]time.Time`), re-scanned in full per row per tick (`limits.go:72-82`). It also reads `t.lim.RouteRestAfter` for the count, not `lim.Faults` (finding 6). Together with finding 1 the rest does not act at all today.
    Repair: keep the last `RouteRestWindow` stamps per row in the fold.

13. **nit. The overlap tally resolves waiting producers in id order, so the lower id holds a shared file even when the other is a `blocker`.**
    `steps/needs.go:24-48` loops `t.v.Producers("waiting")` in id order and adds each resolved producer to `t.resolvedP` (`needs.go:41-45`); `heldBehind`/`leastHolder` (`needs.go:79-93`) then hold every later producer naming the same concrete file behind it, and neither reads rung or score. So of two waiting producers with landed needs sharing a file, the lower id resolves first even if the higher id is a `blocker`. The doc only fixes the holder as "the least such card by id" for naming (LAYER-3 defect 50 text); it makes no promise that the tally follows the ladder, so this is an observed order, not a broken sentence (see Questions 3). Not run; follows from the cited lines.
    Repair: iterate the waiting producers in deal-ladder order (rung, score, then id) in `needs()` instead of id order, if the coordinator confirms the ladder should govern overlap.

14. **minor. `actorOK` is a deny list.**
    `request.go:35-38,81-109`: only the named events need a coordinator, `Seen`/`Gone` the run loop and `Report` the holder; every other `card.Event` (Start, Resolve, Withdraw, NeedFailed, Park, Reached, ReadYes, ReadFix, AskReads, Released, Finished, Broken, Faulted) passes `actorOK` for any actor. Today the only callers are `verbs/moves.go:20` (a table of verbs with their own roles) and `run/tick.go:200`, so no hole is reachable; but the layer's own claim "a verb refused for its actor writes nothing" (list item 8) depends on callers never building those events. Repair: an allow list (default refuse).

15. **nit. Escalation ranking does not implement "nearest first".**
    `deal.go:259-266` (`ranked`): all rows above the wanted rank score 50 and all below score 0; LAYER-5's default says "the wanted rank and up, nearest first, then the ranks between the tier and the wanted one, highest first". Among rows above the wanted rank, the idle lane / room / name tie-break decides, so a frontier row with more room can beat a nearer heavy row. Repair: add the distance to the score.

## Promise gaps

- Doc "The tick" step 7: "A group refused alone on fresh state is left out until its cards change, with an `engine` judgement naming the plan and the store's reason". Code: no such branch; `pack.go:105-113` only splits multi-group refusals (finding 2).
- Doc "Preemption", guarantee 4: "A ready blocker with no room is dealt by preemption in the tick that finds it ready". Code: room for preemption is the roster width, for the deal policy's `work` (finding 4).
- Doc "Retire and fail limits": "`fault_limit` (3 faults of the last 10 on a row in 30 minutes rest it 30 minutes ...: a deal filter in the view". Code: the filter is built (`limits.go:71-87`) but never takes effect (finding 1), ignores the window of 10 (finding 12).
- Doc "Escalation": "Policy: `rows`, `escalate_after`; an answer below the card's tier or off the row's list is the default." Code: `escalate_after`, `frontier_after`, `flap_limit`, `idle_floor`, `land_target` come from `Engine`, never policy (finding 6).
- Doc "The tick" step 2: "preemption, reports and takes, land results ..."; Built: "reports and takes (so a consumer that has reported is never preempted), preemption". The two sentences of one document contradict each other; the code follows the second (finding 8).
- Doc "Reports": "Takes ride the same inbox: `MoveConsumer(Take)`, at most width minus working per row by an in-tick tally ... the excess is refused ("row R is at width 2: finish C first")". Code: `reports.go:13-14` "There is no take item"; no `Take` event, no tally refusal exists (`grep "is at width"` finds nothing in steps/). Stale doc text.
- Doc "Retire and fail limits": "`bounce_limit` (3 ...)", "`silence` (2 hours of running time)". Code: `Engine.Limits.Bounces` 6, `RowSilentWithdraw` 1800 s (`asker.go:157-158`); the Built section says layer 5 is the later word for bounces only; silence is still 2 hours in the first text.
- Doc "Policy: `victim(blocker, candidates)`; default the youngest by take time", "`ahead(row)`", "`regrace(row)`": none is a question any more (Built says `victim` is the engine's); stale list entries.
- Doc "Land": "`flowing` (validated, unused)", Built "(backed up at 4, flowing at 1)" vs `Engine.Landing{BackedUp: 2}` (`asker.go:159`) and the same section's "default 2". Two numbers in one doc.
- Doc guarantee 10: "No policy answer ... makes a step plan nothing where the default would have planned". Findings 3 and 7 are answers/absences that raise a false `policy` judgement; the planned moves are still the defaults, so the guarantee holds, the judgement part ("one `policy` judgement per version and question" for a failure) does not.
- ENFORCED L3-G4: "the stop request goes out only with the accepted op" — kept (`pack.go:36-41` sets `After`), but the victim withdraw lacks the layer 2 guard flag (finding 5).

## Model vs code

- Step order differs: `Steps.tla` Tick puts `PreStep` before `InboxFold`; `Tick` in `tick.go:53-54` runs reports first (finding 8). The trace walk does not exercise a report for the youngest victim in the tick that preempts, so the check passes.
- The model has no actions or invariants for sentinels and `Reached`, for drops and `NeedFailed` closures, for the holds step's withdraw of held-row and down-row consumers, for the limits step (Block at bounces, silent withdraw, Gone, flap, rest), for the nudge/`unacked` judgement, for the stall/idle/land-rate alarms, or for routes/`no-model` (grep of `tla/Steps.tla` for NeedFailed, Reached, Sentinel, Silent, Flap, Unacked gives 0 hits each). These are the steps with the most unmodelled code (needs.go 139 lines, limits.go 88, holds.go 53, hand.go 148 partially).
- The trace binding is narrower than the code it binds: `trace_test.go:98-100` answers `limits` with Bounces/Faults/RouteRest* at 100 and every timeout at one day, so the silence, Gone and rest paths never fire in any trace; `trace.go:209` maps only six judgement kinds; `trace.go:173-176` (`ReqID`) uses `Heads[0]`, so a rung 2 request of several cards is not traced; `trace.go:23-24` says blocked is not in the model. Rung 2 is checked only by `TestTickLadderRungs` (a Go test, not the model).
- The model counts undealt in ticks (`UndealtTicks`), the code in seconds (`hand.go:101`, `UndealtAfter` 2); the model comments acknowledge it, no check ties them.
- Model `Expired` (`Steps.tla` before `Tick`) covers unserved, cannot-read, overlap, no-policy; the code also expires blocked, unacked, undealt, stall, idle, land-rate, no-model (`judge.go:166-236`) with no model counterpart.
- Everything about the candidate cache (finding 1) is invisible to the model because the model computes candidates from the snapshot per action, with no cache; a cache bug cannot show in a trace of the model.

## Simplifications

- Dead code: `judgeAt`'s sibling `judgeAgain` (`judge.go:37-41`, no caller since base-red went); `shares` (`needs.go:96-103`, only `walk_test.go:417`); `View.Overlaps` (`view.go:299`, only tests); `Defaults.IdleCeiling` (`asker.go:141`) while `judge.go:202` hard-codes `working*4 >= open*3`; `LandingAnswer.Flowing`; `Limits.Faults`, `SameFinding`, `ReportsPerTick`, `RouteRestWindow` (finding 6); `RowFacts.Model/Harness` are never filled by `rowFacts` (`tick.go:~165`).
- `Engine` is a mutable package-level `var` (`asker.go:154`) read throughout the steps and the model default switch (`trace_test.go:27`); making it a field of `tick` (it is already passed to `newOracle`) removes global state.
- Two different accountings of the budget: `tick.planned` (`tick.go:80,99-107`, excludes blockers) and `admit`'s `touched` (`pack.go:79-98`, includes them); they agree only when no blocker precedes. One could drive the other.
- Six per-tick caches in `tick` (`rowsCache`, `rfacts`, `roster`, `candCache`, `roomCache`, `rankCache`) are where finding 1 lives; `roster`/`rfacts`/`rowsCache` could be one `rowInfo` per row built once after the claims.
- `group.go:106-154` `written` is a 22-arm type switch that mirrors the store's write types; an interface method on `store.Write` (`Card() string`) would delete it and its omissions (a new `SetCard*` write would silently be unclaimed).
- The closure walks in `needs.go:108-139` (`drops`) and `request.go:210-239` (`dropMoves`) are the same BFS with two small differences; one helper with a callback.

## Missing tests

- A row rested by faults is not dealt to (LPM R34, `limits.go:71-87`): no test sets `View.Faults` or reports three faults on one row; `grep -n "rested\|Faults" steps/*_test.go` finds only limits-struct literals.
- A row `Gone` in a tick is not dealt to in that tick (finding 1).
- A single-group refused op is left out with an `engine` judgement (finding 2).
- A valid `rows` answer naming a full row raises no `policy` judgement (finding 3); `reads` > 1 with a re-asked read (finding 7).
- Blocker with policy `work` < width is preempted or named (finding 4).
- `Withdraw.Preempt` set on the engine's preemption, and layer 2 refusing a cross-row victim (finding 5).
- Report and preemption in one tick (the youngest reporting), finding 8.
- `admit` with a `none` group before a group larger than the budget (finding 9); a drop closure larger than the budget.
- `eligible` with `after` among merging cards of several heads, and on a cycle (finding 10).
- Landing: a `landed` result whose card is claimed by another step; a refused judgement op for a `land` result.
- `land-rate` and `cannot-read` judgements and their expiry (`grep -il` finds no test mentioning either); `idle` clearing at three quarters; `stall` expiring when the stream lands.
- Escalation in a unit test: `desired()` for bounces at `EscalateAfter` and `FrontierAfter` with real ranks (only the trace test sets `EscalateAfter` to 1).
- `actorOK` for an event outside both lists (finding 14).

## Questions for the coordinator

1. Reports before preemption: is the Built text ("a consumer that has reported is never preempted") the intended rule, in which case `Steps.tla` should change to match, or the model's? I could not decide which one is canonical; the doc contradicts itself.
2. Should `escalate_after`, `frontier_after`, `flap_limit`, `idle_floor` and `land_target` be policy's (layer 5 requires them) or the engine's (Built says the engine's)? One of layer 3 or layer 5 is wrong.
3. Finding 13 (uncertain priority policy, so a nit with a conditional repair): should the overlap tally resolve in rung/score order rather than id order? The ladder promises blocker work first among ready cards; the tally can invert it for waiting ones. The doc has no sentence either way (only "the least such card by id" for naming the holder), so I have no contract basis to call it a defect.
4. `Seen` absent for a row (`limits.go:51-54`): "nothing fires on it" means a row layer 6 never reports as seen holds acknowledged work for ever (the `unacked` path covers only unacknowledged cards). Is that intended, and does layer 6 always put every row in `Seen`?
5. `RowFacts.Model` and `Harness` are declared (`asker.go:45`) but never set by the steps; does layer 5 read them from its own roster (as `policy/inputs.go:43-46` suggests) so the fields can go?
6. Is the 86 s unit-test run acceptable as "unit", or should the 100-seed TestWalk/TestTraceWalk move to the functional tier? They use no real time or sockets, so I treated them as unit.
