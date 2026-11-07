## Verdict

The layer is sound. The producer, consumer, and row transitions, the gen fences, the deal-is-take cut into `working`, the missing-route block, and red-to-fix match the later rulings in the layer doc, and `go build` and `go vet` of `./card/...` are clean. The worst thing is a merged `Unhold` then `Hold` of one row: the plan is accepted and the new hold reason is dropped, so the row stays held for the old reason. The assigned `go test` of `./card/...` did not finish; it timed out inside `TestColdread5Walk`, with no assertion failure in the dump.

## Defects

### 1. Merged unhold then hold drops the new reason

`card/replay.go:171` (with `card/values.go:321` and `card/replay.go:290`)

`SetRowHeld` is one field, `held`. `absorb` keeps the later write, and `idleWrite` then drops that write when the flag already equals the snapshot, without looking at `Reason`. `Unhold` then `Hold` on a row that started held is legal one step at a time: the row ends held, and the second reason is what the row shows. Merged, both writes collapse to one `SetRowHeld` whose flag matches the snapshot, `quietWrites` drops it, and the store accepts a plan that only fences `RowGen`. The reason stays the first hold's. `store/ops.go:322` refuses a reason change that does not flip the flag, so the merge cannot emit the update either; dropping it is a silent accept.

It matters because a caller that reholds a row in one op, to replace the reason, gets a receipt and the old text.

Repair: refuse a hold-flag round trip whose reason differs, instead of dropping the write.

Severity: minor.

### 2. The parked-reason guard does not guard the reason

`card/move.go:955`

`edit` refuses a reason-only change while the producer is `waiting` and `m.s.Held` is true ("reason is the hold's while parked"). The same function then writes `e.Reason` at `card/move.go:967` whenever prose, paths, or done_when also change. The guard does not call `stream`, so it never fences `StreamHeld` (`card/move.go:188` is what fences a hold) and it does not check that `m.s` is the producer's stream. A prose edit while parked replaces the hold phrase the return moves just wrote.

It matters because that phrase is the status reason a parked producer shows, and the refusal claims the column belongs to the hold.

Repair: while the stream is held, fence `StreamHeld` and leave the hold phrase in place, whatever else the edit changes.

Severity: minor.

## Promise gaps

The code follows the later ruling in each case below. The earlier sentence is what a reader of the table or of `docs/ENFORCED.md` still sees.

- `docs/LAYER-2-CARD-MACHINE.md:97` says the `Start` cut carries `reason=p.reason`, and line 186 says `Start` copies the producer's reason onto the new work consumer as `reason`. `cut` (`card/move.go:373`) copies `finding` onto a work consumer and does not write the producer's reason. `TestCold2DefectBrokenReasonNeverReachesTheNextAttempt` locks the empty reason. The finding-column ruling is what the code does.

- `docs/LAYER-2-CARD-MACHINE.md:111` says `Edit` writes `brief`. Line 366 says the brief as given is immutable. `edit` writes prose, paths, and done_when, and never the brief. `docs/ENFORCED.md:80` (`L2-M17`) still says `Edit` updates `brief`.

- `docs/LAYER-2-CARD-MACHINE.md:205` lists `Width{n}` as a row move that writes width. `MoveRow` (`card/move.go:1295`) refuses anything but `Seen`, `Gone`, `Hold`, and `Unhold`. Line 544 says width moved with the take: the dealer checks the roster, and the machine does not count a row. `docs/ENFORCED.md:85` (`L2-M22`) still lists `Width` as a row move.

- `docs/ENFORCED.md:68` (`L2-M5`) and line 72 (`L2-M9`) say `Start` and `AskRead` cut the consumer in `ready`. `cut` writes `working` (`card/move.go:377`). `docs/LAYER-2-CARD-MACHINE.md:543` and line 550 say the cut is `working` and no consumer is ever cut `ready`. `docs/ENFORCED.md:82` (`L2-M19`) says the same.

- `docs/ENFORCED.md:83` (`L2-M20`) says a read accepts `ok`, `fix`, `fail`, or `fault`. `reportHead` (`card/move.go:1109`) refuses `fail` on a read. `docs/LAYER-2-CARD-MACHINE.md:378` says a read reports `ok`, `fix`, or `fault`.

- `docs/ENFORCED.md:81` (`L2-M18`) allows `Block` from `ready`, `review`, or `working`. `block` (`card/move.go:1009`) also allows `waiting`, and refuses a sentinel. That is defect 47 (`docs/LAYER-2-CARD-MACHINE.md:564`), and `docs/ENFORCED.md:87` (`L2-M24`) says the same.

- `docs/LAYER-2-CARD-MACHINE.md:180` says `ready` lasts one tick, and the who-waits table gives `ready`, `review`, and `AskRead` a bound of one tick. This package has no tick. It offers `Start`, `Park`, and `AskRead`. The bound is the next tick's, in layer 3.

## Model vs code

TLC was not run. This is not a trace match.

- `tla/CardMachine.tla:749` still has `Width(r, n)`, and `RoomOn` (`tla/CardMachine.tla:375`) guards a cut on row width. The Go machine has no width move and does not count a row. The card tests' `overWidth` is the dealer's check, outside the package. The model was not cut down when width left the store.

- `AskReads` in the model requires `Cardinality(R) >= 2` (`tla/CardMachine.tla:447`). `askReads` (`card/move.go:546`) refuses only an empty list, so one row is a legal `AskReads` in Go and an `AskRead` in the model.

- `Gone` stops at `flaps < 1` (`tla/CardMachine.tla:745`). The code counts on, up to the `values.num` bound. `tla/CardMachineTrace.tla` lifts that bound with `GoneAt`. The trace module says so.

- `HoldStream` in the model parks every ready producer. `holdStream` parks the caller's `Ready` list, and the merge re-run replaces that list with `readyOn` (`card/replay.go:815`), every ready producer in the fold. A standalone partial list is Go-only. The layer doc tells layer 3 to pass the full ready set.

- `ReviewOnEntry` (`tla/CardMachine.tla:292`) adds one to `review` on each `Finished` or `Red` inside an op. `SetCardReview` is a bare increment (`card/replay.go:964` folds it with `c.Review++`; the store does the same), and `absorb` keeps one write of the field. `MaxOp` is 3 (`tla/CardMachine.tla:101`), so the model never puts `Finished` and `Red` in one op. The Go path that would, a new read cut and reported between them, is refused earlier by `coveredWorking` (`card/replay.go:308`): a later state write may not cover a move into `working`, so the take stays visible to the dealer. The model keeps that intermediate state as its own step and can let the last write win. I did not find a plan `Merge` accepts in which a review increment is lost.

- Heads in the model use `0` for both "no head" and the base commit. The Go report refuses an explicit base sha on any nonempty work head, including a non-`ok` result (`card/move.go:1124`).

## Simplifications

The parked-reason check (`card/move.go:955`) is the special case that does not earn its place. It is the only mention of that sentence in the tree, and defect 2 is the hole in it. Either fence the hold and keep the phrase, or delete the guard.

`UnholdStream.Reason` is checked as text (`card/move.go:1263`) and then dropped. The write is `SetStreamHeld` with the flag only (`card/move.go:1270`). A stream has no reason column. The field can leave the event.

## Missing tests

No test merges `Unhold` then `Hold` on one row and checks the reason. That is defect 1. No test edits prose while the stream is held and checks the hold phrase. That is defect 2.

The assigned package test does not finish in 600s. `TestColdread5Walk` (`card/coldread5_test.go:1041`) is 100 seeds by 2,000 steps (`card/coldread5_test.go:291`), each seed `t.Parallel` on its own `store.Fake`. The timeout dump shows that walk still running, including `seeds/100` at 58s, and does not show `TestFunctionalColdread5PG`. No card test calls `time.Now` or `time.Sleep`. The walk's ops take `At` from the caller. `TestFunctionalColdread5PG` (`card/coldread5_test.go:1107`), which the timeout did not reach, starts a real PostgreSQL: `cold5StartPG` (`card/coldread5_test.go:1063`) runs `initdb`, `net.Listen("tcp", "127.0.0.1:0")` at `card/coldread5_test.go:1080`, and `pg_ctl`, and the test polls `store.Open` with `require.Eventually` for up to 30s. It is skipped only when `initdb` is missing.

Command:

```
cd /home/gaffer/johnny-jobs/v2r-l2-flash1.w1 && export PATH=/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin:$PATH && export GOCACHE=/home/gaffer/johnny-jobs/v2r-l2-flash1.w1/.gocache && export GOFLAGS=-mod=readonly && mkdir -p "$GOCACHE" && echo "=== go build ===" && nice -n 19 go build ./card/... && echo BUILD_OK && echo "=== go vet ===" && nice -n 19 go vet ./card/... && echo VET_OK && echo "=== go test ===" && nice -n 19 go test -p 2 -count=1 -timeout 600s ./card/... && echo TEST_OK
```

Result lines:

```
=== go build ===
BUILD_OK
=== go vet ===
VET_OK
=== go test ===
panic: test timed out after 10m0s
	running tests:
		TestColdread5Walk (58s)
		TestColdread5Walk/seeds/100 (58s)
FAIL	github.com/mas-bandwidth/nova-sprint/card	601.078s
FAIL
```

The panic lists many earlier seeds still inside `store.Fake.Apply`. The harness aborted the package. Those lines are the result, not an assertion failure, and nothing was changed to make the walk finish.

## Questions for the coordinator

`docs/ENFORCED.md` `L2-U6` through `L2-U17` are still held by `nothing`, and the package already contains those moves (`AskReads`, `Merge`, `HoldStream`'s `AtSeq` fence, `Relink` `InThisOp`, the 104-byte id cap, `WaiveRed`, `UnholdStream`, and the rest of that run). `L2-U1` through `L2-U5`, `L2-U18`, and `L2-U19` are still outside this package: a later `SetCardAfter` cycle check, and the tick and landing rules that live in layer 3. A ledger pass would stop the next reader from rebuilding what is already here.

Does layer 3 ever merge `Unhold` and `Hold` of one row in one op? If it does, defect 1 is a live lost reason. If it never does, the loss stays on the `Merge` contract only.
