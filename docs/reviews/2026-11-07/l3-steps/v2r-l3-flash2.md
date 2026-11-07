## Verdict

The layer is not sound. The worst fault is that `Tick` does not return if two merging cards name each other in `After`: `eligible` swaps forever, so the run plans nothing, lands nothing, and judges nothing. Add and Relink refuse that cycle, and `waits.go` already says a store op can write one anyway. On states the card machine accepts, the worst fault is that a blocker in a full tick claims deals the packer then drops, and `undealt` treats the claim as a deal, so a ready producer with room is neither started nor named.

## Defects

1. `steps/land.go:203`. `eligible` reorders `After` with `for i, c := range out` and swaps `out[i]` while `c` stays the old card, and it has no seen-set. A 2-cycle (`A` after `B`, `B` after `A`) never reaches `changed == false`; the same loop, copied and capped at 80 ms, was still spinning after about 915000 passes, and a 3-cycle did the same. Both cards are eligible together: each is merging, neither is held by an outside merging target, and the rung then takes `picks[0]` only after this loop. `steps/waits.go:141` ends a needs cycle on purpose ("a store op could still write one"); this loop does not. One such pair freezes every tick. Acyclic graphs I tried (a chain, a diamond, two dependants of one target) did finish in order, so this is the cycle, not the sort in general. Repair: walk with a seen set, as `frontOf` does, and open an `engine` judgement instead of spinning. Severity: blocker.

2. `steps/tick.go:122` and `steps/pack.go:85`. `group` does not add a `pre` group or a blocker `start` to `planned`, and `steps/reports.go:80` never calls `group`, so a report group is not added either. `admit` does add every blocker group's size to `touched`, keeps the first non-blocker even past the budget, then stops at the first later group that does not fit. The groups `group` already appended are claimed at `steps/tick.go:99`, and `steps/deal.go:125` has already counted their rows in the in-tick tally, but they are not in the admitted plan. `undealt` (`steps/hand.go:97`) skips a claimed producer, so those producers stay ready, the room the dropped deal did not take is still there, and no `undealt` judgement names them. `tla/Steps.tla:749` counts `und` from the state after the accepted ops, not from a group that was only planned. It shows up when a tick both preempts or starts a blocker and fills the non-blocker budget (the dropped tail is about the blocker's size in cards). It also shows up if a `Kind: "report"` item is in the inbox; `run/` does not enqueue those today (only `land` and `stop`). Repair: charge `planned` the same way `admit` charges `touched`, and open `undealt` from the admitted plan. Severity: serious.

3. `steps/judge.go:180`. `cannot-read` is cleared when any open serving row is not the author. `steps/deal.go:139` opens it when every such row is the author or already holds a live read of that head (`readingOn`). The clear is wider than the open. With `reads` greater than 1 (the engine default is 1; policy may say 2 to 5) and one other row, that row is in `serving` while `readingOn` is true: the judgement is expired on the next tick while the cause still holds, and the tick after that opens a new episode. The review spends the intervening tick with no open `cannot-read`. Episodes grow for as long as that read stays live. Nothing under `steps/` tests `cannot-read`. Repair: clear only on the predicate `dealRead` uses (not the author, not `readingOn`, and a model can be named). Severity: serious.

4. `steps/request.go:136`. `Verb` on `card.Report` plans `consumerMove` only. The inbox path (`steps/reports.go:78`) plans `card.ReportPlan`, which moves the producer with `Finished`, `Broken`, or `Faulted`. After the verb, the work consumer is done and the producer is still `working`. No later step looks for that pair, and a repeated report is then only an ack (`steps/reports.go:58`). `steps/request_test.go:107` asserts the producer stays `working` and does not tick again. The live `finish`, `read`, and `report` verbs call `card.ReportPlan` from `verbs/report.go` and do not hit this path; `verbs/moves.go` will run whatever event it is given through `Verb`. Repair: plan `card.ReportPlan` in the verb, and stop the test at the producer having moved. Severity: serious.

## Promise gaps

"A ready producer is started when a row serving it has room under its width, in rung order, or an `undealt` judgement names it within `UndealtAfter`" (Guarantees, item 8; ENFORCED L3-G10). A producer claimed by a group `admit` then drops is not started and is not judged. See defect 2. `Steps.tla` `und` does name it, from the post-apply state.

"A review no row may read opens `cannot-read`, so a review never waits on nothing" (Deal and reads). The judgement is opened and then expired while the only other row still holds a live read of the head. See defect 3.

"for the current work consumer of a working producer the same group moves the producer (`Finished`, `Broken`, `Faulted` ...), so no producer is left working under a reported consumer" (Reports). The inbox path does this. `steps.Verb` does not. See defect 4.

"`rows` ... for a read, one read on a row whose model differs from the author's when one is open, LESSONS 3.8" (Deal and reads). `ranked` (`steps/deal.go:256`) scores desired rank, an idle lane, room, and a row the card was withdrawn from. It never reads the author's model. `tla/Steps.tla` `Score` is the same ranking and has no model. When `rows` is absent, the engine can deal the read to the author's model while another model is open. The code and the model agree with each other and not with that sentence.

"after a tick no merging card is without a request or a judgement" (ENFORCED L3-L3, citing `LandEdge.MergingCovered`). `afterHeld` (`steps/land.go:164`) leaves a merging card with neither while an `After` target is merging. That matches `Steps.tla` `MergingCovered`, which has the after clause, and the layer doc's rule that an after holds only a target that is itself queued. `LandEdge.tla` does not model `After`, so its `MergingCovered` is the stricter sentence and the code does not satisfy it.

## Model vs code

`tla/Steps.tla` `Tick` builds every resolve and every deal, then `Admit` keeps a prefix. The code stops calling `group` once `planned` reaches the budget, and the two totals are not the same number: blockers and inbox report groups are outside `planned` and inside `Admit`'s `t` (defect 2). Overlap is not a second bug of that kind. `ResolveFold` adds a producer to `res` with no budget check, and `Held` judges overlap from that full `res`; `steps/needs.go:42` marks `resolvedP` before `group` returns, which matches, including when the resolve is not admitted.

The model's inbox is land results and stop acks. Report items are a code path the model header says are store ops before `Snap`. `run/edges.go` enqueues only `land` and `stop`, which matches the model. The code path is still there and untested.

`und` / `ju` count ticks (`UndealtTicks` is 2). The code counts `UndealtAfter` seconds (2, a guess in the doc) and does not put `ju` in the trace record. The module header says this. Nudges and `unacked` are not in the model; the header says the model has no clock for them. This review did not run TLC and does not claim a trace match. `go test ./steps/` includes `TestTraceFixtures` and `TestTraceWalk` and was green.

`LandEdge.tla` `Tick` asks one card when fewer than `BackedUp` are eligible and every eligible card otherwise. `steps/land.go:135` is that comparison (`BackedUp > len(picks)` keeps the first). `BaseNeverRed` and `NothingLandsUnpassed` are about the edge pass writing the base, which this package does not do. Inherited red (`steps/land.go:88`) stays one `land` judgement and moves nothing; `Steps.tla` is unchanged there, as the doc says.

The youngest victim in the model is the last slot in `tko`. The code (`steps/preempt.go:98`) uses `TakenAt`, then the greater id when the times are equal, which is what the built note says for a take the fold did not witness.

## Simplifications

The after reorder at `steps/land.go:203` is a special case. A seen-set walk, already used by `frontOf` and `depthOf`, replaces it and is the repair for defect 1.

`steps/reports.go` is a second report machine beside `card.ReportPlan` as `verbs/report.go` already calls it, and beside `Verb`. The run loop does not feed the inbox machine. One planner would do.

`riding` (`steps/judge.go:113`) stores the judgement with `episode: -1` so `judgementOp` will not write it again. A flag on the group would say the same thing without a sentinel episode.

## Missing tests

No test builds an `After` cycle and ticks. `TestWaitsSurvivesACycle` only calls `Waits` on a needs cycle. A tick test would hang today; after the repair it should open an `engine` judgement and return.

No test opens `cannot-read` while the only non-author row already holds a live read of that head and `reads` is 2. No test name in `steps/` mentions `cannot-read`.

No test fills the budget in a tick that also preempts, then checks that a ready producer the packer did not admit is still named by `undealt`. `TestTickBudgetAndCarry` uses budget 1 and carries land results; it does not combine a blocker with the cut.

No test sends `InboxItem{Kind: "report"}`. The refusals, the producer move, and the budget charge on that path are untested. `TestVerbAddReport` locks the producer staying `working`.

`steps/load_test.go` is the only file under `steps/` that uses real time, a process, or a socket: `time.Now`, `exec.Command` for `initdb` and `pg_ctl`, and postgres on `127.0.0.1`. Both tests return unless `NOVA_LOAD=1`. This run did not set it, so they did not run. The rest of `go test ./steps/` takes `Now` as an input. The 534s is the in-process walk, not a sleep.

Commands, on hulk, in this clone, `nice -n 19`, `GOCACHE` inside the clone, `GOFLAGS=-mod=readonly`:

```
go build ./steps/...
go vet ./steps/...
go test -p 2 -count=1 -timeout 600s ./steps/...
```

```
BUILD_OK
VET_OK
ok  	github.com/mas-bandwidth/nova-sprint/steps	534.531s
```

No test failed. Nothing was fixed.

## Questions for the coordinator

Is an `After` cycle something `Tick` must survive, or is a stored cycle already a broken store the steps may assume away? `waits.go` survives one. `eligible` does not. Defect 1 is written as a blocker on that reading.

Is `steps.Verb` on `card.Report` supposed to leave the producer `working`? The test says the tick or `ReportPlan` will carry it. Nothing in the tick does, once the consumer is already done.

Should inbox `report` items stay in this package? The run loop does not send them. If they stay, defect 2's report half is live the day layer 6 routes a report here.
