# Layer 2 card machine — v2r-l2-heavy1

Reviewed pin `43d849e29d0e4568ba6a1348a076a59259639938` on `sprint/v2-review/v2r-l2-heavy1`. TLC was not run. No trace match is claimed.

## Verdict

The card machine is sound on the transitions the later rulings name: a deal cuts the consumer straight into working, a red returns the producer to ready or waiting with the finding kept and `red` set, WaiveRed leaves from ready or waiting, a missing-route fault blocks on the first fault, and Merge re-runs each move against the fold for every fence `expectField` knows. It fails closed. The single worst thing is that Merge still emits `NeedsLanded` after an earlier write of the same op has landed that need, so the store refuses a plan the re-run accepted (`card/replay.go:517`, `card/move.go:213`, `store/apply.go:131`). The tick does not build that op (`steps/needs.go:19`). The required `go test ./card/...` timed out at 600s inside `TestFunctionalColdread5PG` with no assertion failure; that is recorded below and is not a machine defect.

## Defects

### 1. Merge keeps NeedsLanded across a land in the same op

Severity: minor

`card/replay.go:517` `expectField` maps `CardState`, `CardHolder`, `CardRed`, `RowIs`, `RowHeld`, and `StreamHeld`. It does not map `NeedsLanded`. `card/replay.go:454` `keepExpects` drops a condition only when `expectField` names a field an earlier write of the op touched. `card/move.go:213` `landedNeeds` always emits `store.NeedsLanded`, and both `Resolve` (`card/move.go:227`) and `Reached` (`card/move.go:256`) call it. On a re-run, `sealMove` (`card/replay.go:680`) calls `Move` with `cardsOr` (`card/replay.go:767`), which is the folded card map, and `foldWrite` (`card/replay.go:953`) has already set the need's state to landed, so the Go guard passes and the expect is kept. `store/apply.go:131` checks every expect before any write, and `store/apply.go:314` `needsLanded` reads the pre-write card. `Merge(Landed(need), Resolve(dependant))` and the same with `Reached` are plans the re-run accepts and the store refuses, with no partial apply.

The current tick does not build this pair. `steps/needs.go:19` resolves from the view, never from this tick's land events, and `steps/group.go:44` is the Merge site. The hole is latent and fails closed. It still breaks the promise that a plan the folded re-run accepted is a plan the store will take.

Repair: drop `NeedsLanded` in `keepExpects` when an earlier write of the op has set every named need to landed, the same way an `InThisOp` `CardState` fail fence is dropped.

### 2. The package README states the red transition rule 30 removed

Severity: minor

`card/README.md:172` says `Red` writes state `review`. `card/README.md:173` says `WaiveRed` is from `review`. `card/move.go:764` `red` calls `back`, which writes ready, or waiting when the stream is held, then `SetCardReview`, the finding, and `SetCardRed`. `card/move.go:689` `waiveRed` accepts only `Ready` and `Waiting`. `docs/LAYER-2-CARD-MACHINE.md:497` and `:504` match the code. `card/move_test.go:115` locks Red's destination as ready, and `card/round4_test.go:61` accepts WaiveRed after a real Red. A reader of the package table gets the old transition, the one that sent a red head back into review.

Repair: change those two README rows to the from-states and writes in `red` and `waiveRed`.

### 3. failedNeed's comment says the InThisOp plan carries no fail fence

Severity: nit

`card/move.go:830` says that with `InThisOp` "the plan carries no `need is fail` condition". `card/move.go:846` then emits `store.CardState{Card: n, State: Fail}`. The inner comment at `card/move.go:844` is the true rule: the fence refuses the plan applied alone, and Merge drops it when an earlier write failed the need. `card/README.md:177` repeats the outer comment ("none with InThisOp"). Someone following the outer comment would omit the fence, and a lone `NeedFailed{InThisOp: true}` would pass the store while the need is not fail.

Repair: make the comment and the README row say the fence is emitted and that Merge drops it.

## Promise gaps

`docs/LAYER-2-CARD-MACHINE.md:186` says `Start` copies the producer's head and reason onto the new work consumer as `prior_head` and `reason`. `card/move.go:373` copies `finding` onto a work consumer and `card/move.go:407` copies `prior_head` from `p.Head` and does not copy reason. `card/README.md:136` states that rule. Intent rule 30 (`docs/LAYER-2-CARD-MACHINE.md:497`) and `docs/ENFORCED.md` L2-M5 also say the cut carries `p.finding`. The sentence at line 186 is the stale one.

`docs/LAYER-2-CARD-MACHINE.md:469` says "finding is written only by a read (`Broken` carries the reader's text, `Faulted` does not) and `Start` copies it". `card/move.go:527` `returned` writes `reason` and does not write `finding`, for both Broken and Faulted. `card/move.go:769` `red` also writes `finding`. `card/README.md:134` matches the code: finding is written by ReadFix and by Red, and Broken and Faulted do not touch it. `docs/ENFORCED.md` L2-U10 still states "Broken carries the reader's text" and is held by nothing.

`docs/LAYER-2-CARD-MACHINE.md:444` says every return into a held stream, and Unblock, parks with `"held: <the stream's reason>"`. `card/move.go:480` says a stream has no reason column, and `holdPhrase` is `"held: stream " + name + " is held"`. `card/move.go:1244` HoldStream's own default is `"stream " + name + " is held"`, without the `held: ` prefix, unless the caller passed a reason. `card/README.md:133` matches the code.

`docs/LAYER-2-CARD-MACHINE.md:205` still lists `Width{n}` as a row event, and `card/README.md:206` still has a Width row. `card/move.go:1294` refuses anything but Seen, Gone, Hold, and Unhold. `docs/LAYER-2-CARD-MACHINE.md:544` gives width to the dealer. The code matches the later ruling. `docs/ENFORCED.md` L2-M22 still describes an update-width move.

`docs/ENFORCED.md` L2-M5 says Start cuts `<p>.w<attempt>` in `ready`. L2-M9 says AskRead cuts `<p>.r<review>.<n>` in `ready`. `card/move.go:377` `cut` writes the consumer `working`. `docs/LAYER-2-CARD-MACHINE.md:544` says Start and AskRead cut the consumer working and that no consumer is ever cut ready. `docs/ENFORCED.md` L2-M19 states that same rule. L2-M5 and L2-M9 were left on the old word. The code matches L2-M19.

`docs/LAYER-2-CARD-MACHINE.md:435` says Merge validates the merged plan so no plan the machine emits is ever invalid at the store. Defect 1 is that sentence, for one composition. `docs/ENFORCED.md` L2-U7 records the same sentence as held by nothing.

## Model vs code

TLC was not run.

`tla/CardMachine.tla:447` `AskReads` requires `Cardinality(R) >= 2`. `card/move.go:546` refuses only `len(e.Rows) == 0`, so a one-row `AskReads` is a legal Go plan. The model cannot take that action.

`tla/CardMachine.tla:749` still has a `Width` action, and `tla/CardMachine.tla:375` `RoomOn` guards the cuts. `card/move.go:1294` refuses `Width`, and `card/move.go:381` `start` does not read a row's width. `docs/LAYER-2-CARD-MACHINE.md:544` says the dealer counts width and the model keeps `RoomOn`. That split is written down. Go and the model do not share the width check.

`tla/CardMachine.tla:176` `NeedsLanded` is a guard on the state the action sees. Go keeps it as a store expect checked before any write (defect 1). A model step that lands a need and then resolves the dependant sees the landed state. The Go plan of that pair is refused by the store. This is a divergence of the expect, not a claim that a trace was checked.

`tla/CardMachine.tla:619` `Edit` records history and leaves the card record unchanged. `card/move.go:958` writes prose, paths, done_when, and reason. The model comment says the brief is left out. The code treats the brief as immutable (`card/README.md:180`) and does write the other fields. Reason and prose are data the model does not carry (`docs/LAYER-2-CARD-MACHINE.md:487`).

`tla/CardMachine.tla:746` enables `Gone` only while `flaps < 1`. `card/move.go:1288` increments flaps with no such cap; `vals.num` is what stops an overflow. The bound looks like a small-instance limit in the model.

`docs/LAYER-2-CARD-MACHINE.md:533` says `CardMachine.tla` does not carry Landed's merge sha or reason text. I did not re-open the model's `Landed` action to re-derive that, and I am not claiming the traces match.

## Simplifications

`card/move.go:955` refuses a reason-only `Edit` while the producer is waiting and the stream is held, with "reason is the hold's while parked". `card/move.go:967` still writes `e.Reason` whenever prose, paths, or done_when also change, so the hold phrase is overwritten as soon as any other field changes. The special case does not protect the phrase. Nothing else in the move needs it: a parked producer's reason is already the phrase HoldStream or `returnReason` wrote, and an edit that changes no field is already refused at `card/move.go:968`.

## Missing tests

`card/coldread_test.go:64` lists `WaiveRed` as legal only from `review`. `card/coldread_test.go:110` asserts that list only when the move returns nil. The sweep's producer has review 0 and red false, so WaiveRed from ready or waiting is refused earlier ("no review yet" / "no red") and the stale allow-list is never checked. `card/coldread_test.go:382` builds the random walk from `coldAllowed`, so the walk does not try WaiveRed from ready or waiting. If `waiveRed` accepted `review` again, this assert would pass. The live from-state is locked by `card/round4_test.go:68`, not by this table.

`card/coldread3_test.go:34` `TestColdread3DefectWaiveRedSkipsTheReadsOfANewAttempt` drives the producer to review 2 by `Finished` (`card/coldread3_test.go:48`) and asserts WaiveRed is refused. `card/move.go:689` refuses `review` before the red check and the prior-read check. The test passes because the from-state is wrong for WaiveRed, which is not the reason the comment names (entered review by Finished, and review 2 has no read).

No test merges `Landed` of a need with `Resolve` or `Reached` of the dependant, which is defect 1. No test covers the reason-only edit refusal at `card/move.go:955`.

`card/coldread5_test.go:1080` `net.Listen("tcp", "127.0.0.1:0")`, `card/coldread5_test.go:1076` `initdb`, and `card/coldread5_test.go:1117` `require.Eventually` of 30 seconds. `TestFunctionalColdread5PG` is a socket, a real PostgreSQL, and real time. A grep of `card/*_test.go` for `time.Sleep`, `net.Listen`, `net.Dial`, `http.`, `initdb`, `Eventually`, and `exec.Command` hits only this test. It is part of `./card/...`, which is why the command below went red. The timeout is recorded, not fixed.

Commands, on hulk, in `/home/gaffer/johnny-jobs/v2r-l2-heavy1.w1`, `nice -n 19`, `GOCACHE` inside the clone, `GOFLAGS=-mod=readonly`, `PATH` prefixed with `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`:

```
go version
go version go1.26.10 linux/amd64

nice -n 19 go build ./card/...
BUILD_OK

nice -n 19 go vet ./card/...
VET_OK

nice -n 19 go test -p 2 -count=1 -timeout 600s ./card/...
panic: test timed out after 10m0s
	running tests:
		TestFunctionalColdread5PG (15s)
		TestFunctionalColdread5PG/seeds/1 (15s)
		TestFunctionalColdread5PG/seeds/3 (15s)
		TestFunctionalColdread5PG/seeds/4 (15s)
FAIL	github.com/mas-bandwidth/nova-sprint/card	600.094s
FAIL
```

The alarm fired while seeds were inside `cold5Run.step` (`card/coldread5_test.go:734`) applying through `store.(*PG).Apply` (`card/coldread5_test.go:371`). No assertion `FAIL` line was printed. Seed 2 was not in the running set when the alarm fired. `-p 2` does not cap the four `t.Parallel` seeds inside that one test.

## Questions for the coordinator

`docs/ENFORCED.md` L2-U6, L2-U8, and L2-U12 through L2-U17 say held by nothing. At this pin the code has them: AskReads (`card/move.go:542`), HoldStream duplicate refusal and `AtSeq` (`card/move.go:1229`, `card/move.go:1242`), the 104-byte cap (`card/add.go:73`), a sentinel as a need (`card/add.go:87`), Replace's kind and gen fence (`card/move.go:818`), Withdraw releasing a working producer (`card/move.go:1165`) and refusing a head equal to the base (`card/move.go:1200`), UnholdStream (`card/move.go:1257`), and WaiveRed from ready or waiting (`card/move.go:689`). L2-U7 is the Merge promise, held in form (`card/replay.go:75` `compose`) and broken for defect 1. L2-U9 says `InThisOp` omits the `CardState` fence; `card/move.go:846` emits it and Merge drops it. L2-U10 and L2-U11 match the promise gaps above and are still open as written. L2-U1, L2-U3, L2-U18, and L2-U19 are the after-cycle and the layer 3 liveness and landing rules, still outside this package. Should the ledger be refreshed before the next review treats "nothing" as the work list?

Is `TestFunctionalColdread5PG` (4 seeds, 800 steps, its own PostgreSQL, `t.Parallel`) expected to finish inside the package's 600s at `-p 2`, or should that functional test sit outside `go test ./card/...`?
