# Review v2r-l3-frontier1 — layer 3, the steps

Pin `43d849e29d0e4568ba6a1348a076a59259639938` on hulk, clone `/home/gaffer/johnny-jobs/v2r-l3-frontier1.w1`. Read: `docs/LAYER-3-STEPS.md`, `docs/ENFORCED.md` (section 7), every file under `steps/`, `tla/Steps.tla`, `tla/StepsTrace.tla`, `tla/LandEdge.tla`, and the `Steps*` witnesses. TLC was not run.

Command (hulk, `nice -n 19`, `GOCACHE` inside the clone, `GOFLAGS=-mod=readonly`, PATH prefixed with `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`):

```
go version
go build ./steps/...
go vet ./steps/...
go test -p 2 -count=1 -timeout 600s ./steps/...
```

```
go version go1.26.10 linux/amd64
BUILD_OK
VET_OK
ok  	github.com/mas-bandwidth/nova-sprint/steps	544.473s
TEST_OK
```

`TestLoad` and `TestLoadResolve` skip unless `NOVA_LOAD=1`, so this run did not execute them. When they run they call `time.Now` and `TestLoad` starts a local PostgreSQL (`exec` of `initdb`/`pg_ctl`, `pgx` to `127.0.0.1`). The tests that did run take time as `Inputs.Now` and use `store.Fake` only. No red test.

## Verdict

The layer is not sound. The worst defect is that `eligible` never returns when two or more merging producers form an after-cycle, so `Tick` does not finish and the run loop stops; every directed cycle on four nodes hangs, and every acyclic graph of that size does finish in a real order. Behind that, a review whose reads are not all `ok` is given the wrong default, a blocker is not preempted when policy's work cap is below roster width, a holder `Report` through `Verb` leaves its producer working, and a one-group store refusal is planned again with no engine judgement.

## Defects

1. **Blocker.** `steps/land.go:203` loops until a pass swaps nothing, and the card it compares (`c` from `range`) is the card from the start of that pass. If A comes after B and B comes after A, and both are merging and otherwise eligible (`steps/land.go:192`), each pass swaps them and `changed` stays true. `Tick` never returns. `waits.go` already stops on a needs cycle a store write can create; `SetCardAfter` is not cycle-checked (ENFORCED L2-U1), and `card/emma_read2_test.go` writes one with a raw store op. Repair: order with a bounded walk that treats a cycle as an engine judgement and leaves those cards out of the request.

2. **Serious.** The default verdict is yes unless some read said `fix` (`steps/asker.go:272`). The layer text and `tla/Steps.tla:267` say yes only when every done read says ok. A done read whose result is `fault` or `fail` is not `ok` and not `fix`, so the default plans `ReadYes` (`steps/fix.go:27`). The machine accepts that only when some read said ok (`card/move.go:652`) and accepts `ReadFix` only when some read said fix (`card/move.go:664`). A review whose reads are all fault or fail is refused, recorded as an engine judgement, and not asked again: `owed` counts every read whose state is not `fail` (`steps/fix.go:83`), so a done fault read fills the count. A review with one ok and one fault is sent to merging. Repair: default yes only when every settled read is ok; otherwise, if any read said fix, `ReadFix` with the joined findings, and if none did, cut the reads again (do not count a fault or fail read as owed).

3. **Serious.** Preemption treats a row as full only when working consumers have reached roster width (`steps/preempt.go:82`). The deal treats it as full when live consumers have reached policy's work (`steps/deal.go:174`, `steps/deal.go:188`), and work may be any integer from 0 through width (`steps/asker.go:282`). Undealt uses the deal's room (`steps/hand.go:104`). A ready blocker on a row that is at work and still under width is not started, not preempted, and not named: no `unserved` (a serving row exists) and no `undealt` (no deal room). With the default, work equals width, so the suite does not see it. Repair: use one room for the deal, for preemption, and for undealt, the minimum of roster width and policy work.

4. **Serious.** `Verb` plans a holder `Report` as the consumer move alone (`steps/request.go:132`). The inbox path plans `card.ReportPlan`, which moves the producer in the same group (`steps/reports.go:78`). Nothing else in the package plans `Finished`, `Broken`, or `Faulted`. After the verb is accepted the producer stays `working` under a `done` consumer and holds its width. `steps/request_test.go:107` asserts that state and says the tick will carry the producer; `reports` only reads inbox items, so a later tick does not. Repair: plan `ReportPlan` from `Verb`, as the inbox path does, and assert the producer leaves `working` in that op.

5. **Serious.** A refused op is split by group only when it held more than one group (`steps/pack.go:106`). `RefusedOp.Reason` is never read. A one-group refusal is planned again on the next tick, and if the body is unchanged the id is unchanged, so the store replays the refusal and no `engine` judgement is opened. The type's own comment states the missing half (`steps/inputs.go:61`). Repair: when a refused group is planned again with the same gens, leave it out and open one `engine` judgement naming the plan and `Reason`, until one of its cards changes.

6. **Minor.** Inbox reports are planned through `newGroup` and `add` (`steps/reports.go:80`), not `group`, so they do not add to `planned` and do not stop at the budget. Admit still keeps a prefix and carries the rest, so the applied set is right, but a burst is planned in full before the cut. The cost bound says planning stops once non-blocker groups touch the budget. Repair: plan reports through `group`, so the budget stops the walk and the unplanned tail is what Carry holds.

7. **Minor.** Route rest counts every fault inside `RouteRestFor` (`steps/limits.go:76`) and compares that to `RouteRestAfter`. `Limits.Faults` and `RouteRestWindow` are range-checked (`steps/asker.go:316`) and then unused, so "3 of the last 10" is not what the filter does: three faults in the window rest the row however many oks sit between them. Repair: either count faults in the last `RouteRestWindow` reports, or drop the two fields and change the layer sentence.

8. **Minor.** An inbox item whose kind is `take` (still named on `InboxItem`, `steps/inputs.go:28`) is not a report (`steps/reports.go:17`) and not a land or stop result (`steps/land.go:27`), so it is never consumed and is copied to `Carry` on every later tick. Repair: refuse a `take` in one line (the deal is the take; there is no take item) and consume it.

## Promise gaps

- "default yes when every done read says ok, else fix with every finding joined" (`docs/LAYER-3-STEPS.md`, Fix). The code returns yes when no read said fix, including when no read said ok (`steps/asker.go:272`).
- "A group refused alone on fresh state is left out until its cards change, with an `engine` judgement naming the plan and the store's reason" (The tick, step 7). Only a refused op of two or more groups is treated, and only by packing those cards alone (`steps/pack.go:106`). No judgement is opened from the receipt.
- "for the current work consumer of a working producer the same group moves the producer (`Finished`, `Broken`, `Faulted` …), so no producer is left working under a reported consumer" (Reports). True of the inbox path. False of `Verb` (`steps/request.go:136`).
- "with no free width on an open serving row, one group: Withdraw …" and "A ready blocker with no room is dealt by preemption in the tick that finds it ready, unless no row serves its tier" (Preemption, guarantee 4). Free width in the code is roster width (`steps/preempt.go:82`). The deal's room is policy work (`steps/deal.go:188`). A row at work and under width produces neither the preemption nor a judgement in that tick.
- "3 faults of the last 10 on a row in 30 minutes rest it 30 minutes" (Retire and fail limits). The code rests on `RouteRestAfter` faults inside `RouteRestFor` (`steps/limits.go:84`) and does not look at a last-N sample.
- "Every report and take that arrives is applied or answered" (guarantee 7). A `take` item is neither (`steps/reports.go:17`).
- "once the non-blocker groups planned touch the budget's count no more are planned" (Built, the cost bound). Reports are planned without that counter (`steps/reports.go:80`).

These are already written down as differences, and the code matches the write-up: no active policy deals nothing (the stated exception to guarantee 10); `undealt` is seconds in Go and ticks in `Steps.tla`, and `ju` is not in the trace; inbox reports are not in the model; rung 2 is `TestTickLadderRungs` plus `LandEdge.tla`, and `Steps.tla` picks one producer per stream.

## Model vs code

`tla/Steps.tla:267` defines the default as `AllOk`: every done read of the current review has result ok, and `FixSet` plans fix when that is false. `steps/asker.go:272` plans fix only when a read's result is `fix`, and yes otherwise. That is the one divergence that is not already named in the Built section. A fault read is in the model's result set (`Steps.tla` around the `res` declaration) and would take the fix branch there, and the yes branch in Go.

`LandElig` drops a merging producer while any after target is still merging, and `LandPicks` keeps one card per stream. The Go rung-2 path keeps every eligible card and orders after targets ahead (`steps/land.go:203`). `LandEdge.tla` models the backed-up count and has no after edges, so order among a batch is not checked by either model. On an acyclic after graph the Go order is a real topological order (checked on every DAG of four nodes); the model never has to order one. The hang in defect 1 cannot appear in `Steps.tla`, because a card whose after target is merging is not a pick, so the model has no swap loop.

No trace match is claimed. The witnesses were read, not run.

## Simplifications

`Limits.Faults`, `Limits.SameFinding`, `Limits.ReportsPerTick`, and `Timeouts.BaseRegate` are accepted from policy and never read by a step. `SameFinding` is already called out as not a count; the other three can go, or a step has to use them. The after reorder does not need its own swap loop: the cycle-guarded walk in `waits.go` is the same problem. Two report implementations (`ReportPlan` on the inbox, `MoveConsumer` on `Verb`) are one report.

## Missing tests

- Two merging producers after each other. `Tick` must return. Nothing in `steps/` builds an after cycle.
- A review whose only done read is `fault`, and one with `ok` and `fault`. The producer must move, be re-read, or sit under a judgement whose default does that. Today's default plans a `ReadYes` the machine refuses, or merges.
- Roster width 2, policy work 1, one working consumer, a ready blocker. The tick must preempt or open a judgement. No test sets work below width.
- One group, store refusal, same view next tick. The plan must omit that group and open `engine`. `TestTickRefusedOpSplitByGroup` only covers two groups.
- `TestVerbAddReport` stops at `p1` still `working` (`steps/request_test.go:107`). It records the strand and does not ask whether any later tick finishes it.
- An inbox item of kind `take` must be refused once, not carried for the life of the process.

## Questions for the coordinator

- Is policy work below roster width a pause, with no preemption and no undealt, or should preemption and undealt use the deal's room? Guarantee 4 says the blocker is dealt in the tick that finds it ready.
- Is `Verb`'s consumer-only `Report` deliberate because layer 6 always applies `ReportPlan` itself? If so, `Verb` should refuse `Report` rather than accept an op that strands the producer. The test comment says the tick finishes it, and the tick does not.
- A store `SetCardAfter` can close an after cycle (L2-U1). Should layer 3 treat that as an engine judgement and keep ticking, or is a non-terminating `eligible` acceptable until layer 2 refuses the write?
