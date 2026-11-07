## Verdict

The layer is not sound. The worst hole is `Verb`: a holder's work `Report` is applied as the consumer's move only, the consumer goes `done`, and the producer stays `working` with nothing left in hand and no later step that releases it. Two other stalls sit beside that: a cycle of `after` among merging cards of one stream makes `eligible` swap forever, so `Tick` does not return, and a read that reports `fault` is planned as `ReadYes`, refused by the card machine on the fresh view, and not owed again.

## Defects

1. `steps/request.go:136` (locked in by `steps/request_test.go:107`). `requestMoves` for `card.Report` returns only `consumerMove`. A work report therefore writes the consumer `done` and does not plan `Finished`, `Broken` or `Faulted`. The inbox path does that pair through `card.ReportPlan` (`steps/reports.go:78`); this path does not, and a later inbox redelivery of the same result is only an ack (`steps/reports.go:58`). Nothing else in the tick looks for a producer left `working` with no live consumer, so the card is not waited on by a row, the next tick, or a judgement. Repair: plan the holder's report through `card.ReportPlan`, the same group the inbox uses. Severity: serious.

2. `steps/land.go:203`. `eligible` keeps every merging card whose `after` targets are also in the queued set (`steps/land.go:195`, `!in[a]`), then reorders with an unbounded swap. A 2-cycle (A after B and B after A) swaps the pair on every pass and `changed` stays true, so `Tick` never returns and the sprint plans nothing. `Add` refuses a cycle (`card/add.go` `addCycle`); `store.SetCardAfter` does not, and `steps/waits.go` `frontOf` already treats a cycle a store op wrote as input this sorter does not. The model's `LandElig` excludes anyone whose `after` target is still merging, so a cycle is after-held and `MergingCovered` holds; this loop never gets there. Repair: order with a cycle check, and on a cycle leave those cards out of the request instead of looping. Severity: serious.

3. `steps/asker.go:275` and `steps/fix.go:83`. The default verdict is yes whenever no done read said `fix`. A read may report `fault` (`card/move.go` accepts it). `ReadYes` then refuses on the fresh view because no done read said ok (`card/move.go:652`), and `ReadFix` would also refuse because none said fix (`card/move.go:666`). `owed` counts every read whose state is not `fail`, so the done fault fills the review and no replacement is cut. The step records an `engine` judgement for a refusal of its own plan, which is the `RefusedOnlyByRace` signal, not a fault a row is allowed to report. The model's `AllOk` is the other way: yes only when every done read of the head said ok (`tla/Steps.tla` `AllOk`). Repair: plan `ReadYes` only when every done read of this head said ok, and count a done `fault` the way a withdrawal is counted so another read is cut. Severity: serious.

4. `steps/deal.go:273`. `ranked` adds the rank class (100 or 50), an idle bonus (10) and the free room into one integer. The comment, and the layer text, say the order is the desired rank, then above it, then an idle lane, then most room, then name. Kin width has no cap (`roster/roster.go`, friends at their own width), so a busy row with 11 more free slots outranks an idle row of the same rank, and a higher rank with more than 50 extra free slots outranks the card's own tier. Withdrawn-from still loses, because that penalty is 1000. `tla/Steps.tla` `Score` is the same formula; its rows are width 1 and 2, so TLC cannot see it. Repair: compare `(rank class, idle, free room, name)` in that order, and do not add room into the rank term. Severity: minor.

5. `steps/reports.go:86` against `steps/tick.go:123` and `steps/hand.go:97`. A report group is `add`ed without going through `group`, so it does not increase `planned`. `group` then still plans deals up to the budget, and those deals claim their producers. `admit` counts the report cards first and drops the deals that no longer fit, but `undealt` has already skipped every claimed producer. While inbox reports fill the budget every tick, a ready producer with a row and room is neither dealt nor named. The inbox is the side path (layer 6 applies a row's report as its own op); on that path the undealt promise does not hold. Repair: count a report group in `planned` before later steps run, or have `undealt` ignore a claim whose group `admit` will not keep. Severity: minor.

## Promise gaps

"No policy answer, absence or error makes a step plan an illegal move or plan nothing where the default would have planned" (guarantee 10). `deal` returns at once when `Answers.NoDeal` is set (`steps/deal.go` `gated`), and `preempt` returns before any withdrawal (`steps/preempt.go:20`). Reports, resolve, fix, landing and judgements still run. The Built section calls this the one exception, and `Steps.tla` checks it as `NoDealWithoutPolicy`. The guarantee sentence was not amended, so a failed whole version plans nothing where the default would have dealt.

"default yes when every done read says ok, else fix with every finding joined" (Fix). The code is yes when no read said `fix` (`steps/asker.go:275`), so a `fault`, or an ok beside a fault, is yes. Defect 3 is the case the machine then refuses.

"for the current work consumer of a working producer the same group moves the producer (`Finished`, `Broken`, `Faulted` ...), so no producer is left working under a reported consumer" (Reports). True of the inbox path. False of `Verb`, which is the path `TestVerbAddReport` drives and then stops (defect 1).

"a ready producer with an open serving row with room is dealt within `UndealtAfter`, or an `undealt` judgement names the row and the card" (In hand, L3-G10). True when the deal is the thing that spends `planned`. False for the producers a report group claims and `admit` then drops (defect 5).

The Retire section still says silence of 2 hours and a bounce limit of 3. The code uses `Timeouts.RowSilentWithdraw` 1800s and `Limits.Bounces` 6. The Built section says layer 5 is the later word. Not a second implementation; the section above it is stale.

## Model vs code

`Steps.tla` `AllOk` and the Go default verdict disagree, as in defect 3. The model has no `fault` read result (`ReadResults` is `ok` and `fix` in the deal scenario only).

`LandPicks` is one card per stream, and only a card with no `after` target still merging. The code orders every queued card whose target is also queued, and at `backed_up` sends them as one request. The module header says rung 2 is `TestTickLadderRungs` and `LandEdge.tla`, not this `LandPicks`. The hang in defect 2 is the case the model treats as after-held and the code does not return from.

`Score` matches `ranked`. Both disagree with the prose order once free room exceeds the bonuses. The checked instance cannot show it.

Counted in the module header, and matching what I read: undealt is ticks in the model and seconds in the code, and `ju` is not in the trace; nudges and `unacked` are outside the model; routes are outside the model; inbox reports are outside the model (the model's `Report` is the row's own op); a coordinator drop that releases live consumers is `CardMachine`, not a `Steps` action. I did not run TLC and I do not claim a trace match. `go test ./steps/...` did run the package's trace tests; that is the test result, not a TLC result.

## Simplifications

`judgeAgain` (`steps/judge.go:37`) has no caller. It exists for a base that is red again; those judgement kinds were removed with intent rule 32.

`Limits.Faults`, `SameFinding`, `ReportsPerTick` and `RouteRestWindow`, and `Landing.Flowing`, are range-checked in `steps/asker.go` and never read by a step. The rest window that is read is `Timeouts.RouteRestFor`. `SameFinding` is not a count; the brief judgement compares the finding bytes (`steps/fix.go:36`).

The rank and idle bonuses in `ranked` are the special case. One comparison of the tuple the comment already names replaces them, and is the repair in defect 4.

The withdraw of a consumer still `ready` (`steps/holds.go:38`) is a migration arm: the deal no longer cuts `ready`. It is still the only arm that moves one left over from before deal-is-take. Not dead while such a consumer can be folded.

## Missing tests

`TestVerbAddReport` asserts the producer stays `working` after a successful holder report (`steps/request_test.go:107`). That is the defect, recorded as the expected result. The missing test is the next tick: the producer is released, or the call is refused. As written, the test cannot catch the stall.

No test builds two merging cards with a cyclic `after` and requires `Tick` to return. `TestWaitsSurvivesACycle` covers the board text only.

No test reports `fault` on a read and checks the next plan. The machine tests cover the refusal strings; the steps tests do not cover the verdict choosing `yes` and `owed` not cutting a replacement.

No test of `ranked` with free room above 10, or a higher rank with free room above 50. The model widths are 1 and 2, and the Go deal tests use small rows, so both stay on the side of the bonuses where the weighted sum still matches the prose.

No test fills the budget with inbox reports and checks that a ready producer `admit` did not emit still gets an `undealt` judgement.

`steps/load_test.go` `TestLoad` uses `time.Now` and, on its apply half, PostgreSQL on 127.0.0.1. It skips unless `NOVA_LOAD=1`. This run did not set that, so it did not run. The rest of `go test ./steps/...` takes time as `Inputs.Now`, uses `store.Fake`, and does not dial.

## Questions for the coordinator

Commands, on hulk, in `/home/gaffer/johnny-jobs/v2r-l3-flash1.w1`, `PATH` prefixed with `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`, `GOCACHE` at `.gocache` inside the clone, `GOFLAGS=-mod=readonly`, `nice -n 19`:

```
go version
go version go1.26.10 linux/amd64
go build ./steps/...
BUILD_EXIT=0
go vet ./steps/...
VET_EXIT=0
go test -p 2 -count=1 -timeout 600s ./steps/...
ok  	github.com/mas-bandwidth/nova-sprint/steps	274.509s
TEST_EXIT=0
```

Is `Verb`'s consumer-only `Report` a door layer 6 must not use, or a bug the test has frozen? If layer 6 always uses `card.ReportPlan`, the producer in `TestVerbAddReport` is still stuck for anyone who calls `Verb`.

Is a cyclic `after` supposed to be unreachable by the time a card is merging? `SetCardAfter` accepts one today, and the land step does not survive it.

I did not run TLC.
