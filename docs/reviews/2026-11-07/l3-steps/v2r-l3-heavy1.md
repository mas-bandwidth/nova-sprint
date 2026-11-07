# Layer 3 steps, cold review

Card v2r-l3-heavy1.w1. Pin 43d849e29d0e4568ba6a1348a076a59259639938, branch sprint/v2-review/v2r-l3-heavy1, read on hulk. TLC was not run. This review does not claim a trace match.

## Verdict

The layer is not sound. The deal ladder, the width tally, the overlap rule on concrete paths, the fix oracle, the pack and carry, and the actor check on verbs hold together under the tests that ran, but `eligible` can fail to return: two merging cards of one stream that name each other in `after` make the reorder spin, so the tick never finishes. That is the worst thing. Three further defects are serious: a judgement clears on a weaker row test than the deal uses, policy's steering counts never reach the steps, and a row rests on an absolute fault count rather than three of the last ten.

## Defects

### 1. A same-stream after cycle never returns from the tick

`steps/land.go:203`. `eligible` walks `for changed := true; changed;` and swaps a later card forward when it is in `c.After`. `c` is the range copy from the start of that outer step, so it is not the card now sitting at `i` after a swap. On a two-cycle (A after B, B after A) the two cards trade places forever. A three-cycle does the same. `Tick` calls this from `landRequests` with no bound. The card machine's Relink tries to refuse a cycle, and `docs/ENFORCED.md` L2-U1 records that `store.SetCardAfter` still does not, so a snapshot can hold the graph. A cross-stream cycle does not spin: each side is held because the other is merging and not in this stream's list (`steps/land.go:195`), and `afterHeld` (`steps/land.go:164`) then makes `unlanded` skip both, so neither a request nor a judgement is opened and both stay merging. Why it matters: one tick is the run loop's turn; a hang stops the sprint, and the quiet cycle leaves producers with nobody moving them. Repair: do not reorder in an unbounded loop; on a cycle open one `engine` judgement and stop, and do not treat that cycle as an after-hold that explains the wait. Severity: blocker.

### 2. `unserved` and `cannot-read` clear on rows the deal will not use

`steps/judge.go:171` and `steps/judge.go:180`. `unserved` clears when any row is `Open`, lists the tier, and serves the routes. `Open` is up, not held, and hooked (`steps/view.go:315`). It does not look at `t.rested`. The deal's candidates skip a resting row (`steps/tick.go:223`), so the only row for a tier can be resting for `route_rest_for` (30 minutes at the default) while the judgement expires and the next tick opens it again. `cannot-read` clears when `serving` names any row that is not the author. `dealRead` also drops a row already reading this head (`steps/deal.go:139`) and a row it cannot name a model for. With `reads` above 1, the only other row can be mid-read, `owed` stays positive, and the judgement flaps instead of standing. Why it matters: the layer's promise that a review never waits on nothing, and that an unserved tier stays named, is the open judgement; a one-tick blink does not keep that. Repair: clear each kind only when the deal's own filter would now name a row (not resting, a model can be named, and for a read not already on this head). Severity: serious.

### 3. Policy's steering numbers are dropped before the steps read them

`steps/deal.go:302`, `steps/limits.go:48`, `steps/judge.go:262`, `steps/judge.go:268`, `steps/asker.go:108`, `run/asker.go:120`. `desired` escalates at `Engine.EscalateAfter` and `Engine.FrontierAfter`. Flaps open at `Engine.FlapLimit`. Idle and land-rate use `Engine.IdleFloor`, `Engine.LandTarget`, and `Engine.LandSample`. `steps.Limits` has no fields for those counts. `stepsAnswers` copies bounces, faults, the rest-window fields, budget, and pack, and leaves `EscalateAfter`, `FrontierAfter`, `FlapLimit`, `IdleFloor`, and `LandTarget` behind, even though `policy.Limits` documents them as the numbers layer 3 reads (round 5). A policy that sets `escalate_after` to 10 still escalates at 2. Why it matters: the coordinator's policy is the place those bounds were given, and a change there does not change a tick. Repair: carry the five numbers on `steps.Limits`, copy them in `stepsAnswers`, and read them from the oracle, falling back to `Engine` only when the answer is absent or out of range. Severity: serious.

### 4. Rest counts every recent fault, not three of the last ten

`steps/limits.go:71`. `rest` counts faults inside `RouteRestFor` and rests the row when that count reaches `RouteRestAfter`. `RouteRestWindow` is validated (`steps/asker.go:318`) and copied from policy (`run/asker.go:121`) and then never read. Three faults in thirty minutes rest the row for thirty minutes even when they are three of hundreds of reports. `Limits.Faults`, `SameFinding`, and `ReportsPerTick` are likewise stored and never consulted; the rest-window one is the one the rule names. Why it matters: a busy row is taken out of the deal on a stricter rule than the one policy is allowed to set, and setting `route_rest_window` does nothing. Repair: among the last `RouteRestWindow` fault-or-not outcomes on the row, rest only when `RouteRestAfter` of them are faults and fall inside the time bound. Severity: serious.

## Promise gaps

"A ready blocker with every open serving row at width is dealt by preemption of the youngest working consumer in the tick that finds it" (`docs/ENFORCED.md` L3-G4, and guarantee 4). The code does that from the view: `preempt` runs before the deal, `victim` is the latest `TakenAt`, and the stop edge is stamped `After` the op id in `pack`. No gap found on that sentence.

"`fault_limit` (3 faults of the last 10 on a row in 30 minutes rest it 30 minutes" (`docs/LAYER-3-STEPS.md`, Retire and fail limits). The code rests on an absolute count inside the time bound and never reads the 10. See defect 4.

"policy's `rows` answer picks the rank for each candidate" and "once the producer's bounces reach `escalate_after`" (`docs/LAYER-3-STEPS.md`, Escalation); "`flap_limit` (3)"; "`idle_floor` (0.5), `land_target` (0.9 over 20)" (Judgements). Those are `Engine` constants, not the policy answer. See defect 3. The opening "`bounce_limit` (3)" and "`silence` (2 hours)" are not open gaps: the Built section says bounces 6 because layer 5 is the later word, layer 5's `row_silent_withdraw` is 1800, and `Engine` matches that later word (`steps/asker.go:157`).

"a review no row may read opens `cannot-read`, so a review never waits on nothing" and "A ready producer whose tier no open row lists opens an `unserved` judgement" (`docs/LAYER-3-STEPS.md`, Deal and reads). Both open, and both clear on a row the deal still will not use. See defect 2.

"for a read, one read on a row whose model differs from the author's when one is open, LESSONS 3.8" (`docs/LAYER-3-STEPS.md`, Deal and reads). `ranked` (`steps/deal.go:256`) scores desired rank, then an idle bonus of 10, then `room-used`, then a withdrawn penalty. It never reads a model. Policy's `prefer` orders by rank and room the same way (`policy/prefer.go:55`). The author is excluded, and the read carries the row's model; a different model is not preferred when one is open.

"Every non-final producer is waited on by a named actor, the next tick or an open judgement" (guarantee 9). A cross-stream `after` cycle is waited on by neither a request nor a judgement. See defect 1.

"at the deadline with the kind's safe default, which never drops, accepts, merges or lands" (Judgements). `judgements` expires with that sentence as the reason and writes only `ExpireJudgement` (`steps/judge.go:159`). No move is attached. No gap found on that sentence.

"No row is over width" (guarantee 2) and "Ready never depends on row load" (guarantee 8). `withRoom` compares live consumers plus the in-tick tally to policy `work`, capped at roster width by `oracle.width` (`steps/asker.go:278`). `needs` resolves with no room test, and overlap counts only `Concrete` paths (`steps/view.go:293`). No gap found on those two sentences. The idle-before-room order in `ranked` is a weighted sum, not a lexicographic key; with width at most 10 the idle bonus of 10 still wins, and the roster's worker width in tests is 8, so this was not filed as a defect.

## Model vs code

`tla/Steps.tla` `LandElig` keeps a merging producer only when no after target is still merging, same stream or not, and `LandPicks` takes the first of those (rung 1). `steps/land.go:186` keeps a same-stream pair whose target is also queued and tries to move the target ahead inside one request. That reorder is not in the model, and it is the loop in defect 1. `tla/LandEdge.tla` models rung 2 as every eligible card once `BackedUp` is reached, and it has no `after` at all. The Go rung test is `TestTickLadderRungs`. I did not run TLC, and this is not a trace claim. `TestTraceFixtures` and `TestTraceWalk` passed in the `./steps/...` run below.

`Steps.tla` counts `DealtWithinKTicks` in ticks (`UndealtTicks == 2`) and says the code counts seconds (`UndealtAfter`). Nudges and `unacked` are outside the model; the header says the model has no clock for them. Reports are already store ops in the model and are also an inbox path in `steps/reports.go`. The Built section of the layer doc says these three on purpose. They match what I read.

`Steps.tla` `Tick` runs `PreStep` before `InboxFold`. The code runs `reports` before `preempt` (`steps/tick.go:53`). The Built section says that is so a consumer that has reported is not preempted. Because planning does not fold its own groups, a report only claims the consumer; the view still shows it working, so a blocker on a full row waits one tick if that report was the only victim. I did not find a lost blocker, and I am not calling it a second defect.

## Simplifications

The same-stream reorder is the special case. `LandElig` does not need it. Deleting it removes the hang and leaves one rule: a card whose after target is merging is not in this request.

`steps/roster.go:33` turns an empty kind into `worker`. `roster.Row` returns `KindWorker` or `KindKin` when `ok` is true, and `ok` false otherwise (`roster/roster.go:217`). The branch does not run.

`Limits.Faults`, `SameFinding`, and `ReportsPerTick` are checked and copied and never read. Budget replaced `reports_per_tick` in the Built section. The byte-equal brief judgement does not consult `SameFinding`.

## Missing tests

Nothing in `steps/` builds two merging cards of one stream with a cyclic `after` and asserts that `Tick` returns. Nothing keeps `unserved` open while the only serving row is resting, or keeps `cannot-read` open when `reads` is 2 and the only other row is already reading that head. Nothing sets policy `escalate_after`, `flap_limit`, `idle_floor`, `land_target`, or `route_rest_window` and shows the tick change; `TestTraceWalk` assigns `Engine.EscalateAfter` directly, which is the constant, not the answer.

`steps/load_test.go` `TestLoad` uses `time.Now`, `os/exec`, and a PostgreSQL connection on 127.0.0.1. It skips unless `NOVA_LOAD=1`. This run did not set that, so it did not start postgres. The other tests in the package take time as `Inputs.Now`. `TestLintPure` rejects `time.Now`, `net`, and `os/exec` in non-test files. No unit test I read opens a socket.

## Questions for the coordinator

Should a same-stream `after` land in one request, ordered, or wait until the target is not merging, which is what `Steps.tla` `LandElig` does? The hang is in the first reading. The second reading does not need the loop.

The Built section says `escalate_after` is the engine's because layer 5 had no such question. `policy.Limits` now says layer 3 reads it. Which contract should the next edit follow?

Commands on hulk, `GOCACHE` inside the clone, `GOFLAGS=-mod=readonly`, `nice -n 19`, go 1.26.10:

```
go build ./steps/...
```

exit 0, no output.

```
go vet ./steps/...
```

exit 0, no output.

```
go test -p 2 -count=1 -timeout 600s ./steps/...
```

```
ok  	github.com/mas-bandwidth/nova-sprint/steps	526.363s
```

exit 0. No test was red. None were fixed.
