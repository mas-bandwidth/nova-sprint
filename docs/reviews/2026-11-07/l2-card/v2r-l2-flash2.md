## Verdict

The card machine's transitions match the later rulings in the layer doc: a deal cuts the consumer working, a red goes to fix and spends no bounce, a missing-route fault blocks, Block is allowed from waiting and refused for a sentinel, and finding is written by ReadFix and Red and left unchanged by Broken and Faulted, with Start copying it onto the work consumer. I read StepRule in `tla/CardMachine.tla` beside the matching Go. I did not run TLC, and I do not claim a trace match. The mandated `go test ./card/...` is red. The single worst thing is that the package deadline fires while `TestColdread5Walk` is still inside `step`: 100 seeds of 2,000 steps, each seed `t.Parallel`, had not finished, and no assertion failure was printed.

## Defects

1. **Serious.** `card/coldread5_test.go:291` and `card/coldread5_test.go:1046`. `TestColdread5Walk` is 100 seeds by 2,000 steps, and each seed calls `t.Parallel`. Under `go test -p 2 -count=1 -timeout 600s ./card/...` the process is killed at 10m0s with that walk still in `step`. The parent timer in the panic is 2m6s, so the rest of the package used the budget before the walk finished a wave of seeds. No assertion failure was printed, so this run does not say whether the walk's invariants hold. It matters because the walk is the check that every merged op matches the plans applied one by one, and the required command is red before that check reports. Repair: bound how many seeds run together, or how many steps the default walk takes, so the package finishes inside 600s.

2. **Minor.** `card/move.go:955`. A reason-only `Edit` of a waiting producer is refused with "reason is the hold's while parked" whenever the stream value passed to `Move` is held. The branch does not check that the stream is the producer's, and it writes no `StreamHeld` fence. An `Edit` that also changes prose, paths, or done_when falls through, and `m.reason` at `card/move.go:967` writes the new reason, including over a hold phrase. The producer table at `docs/LAYER-2-CARD-MACHINE.md:111` allows a reason write from any non-final state when no consumer is live. It matters because the guard refuses the wrong caller and fails to keep the phrase it names. Repair: delete the branch, or fence the producer's own stream and apply one rule to every reason write.

## Promise gaps

The opening row table says `` `Width{n}` | any | same | width, from nova-config only (LESSONS 1.12); refused below the row's `working` count by layer 1 `` (`docs/LAYER-2-CARD-MACHINE.md:205`). `MoveRow` refuses that event: "is not a row event; MoveRow takes Seen, Gone, Hold and Unhold" (`card/move.go:1295`). The later section "Deal is take" says the machine sees one producer's consumers and cannot count a row, and that the dealer and the model's `RoomOn` hold the width. The code follows that later paragraph. The opening row still lists `Width` as a machine move.

The consumer table says "a read's head must equal the producer's" (`docs/LAYER-2-CARD-MACHINE.md:121`). `readReportHead` refuses a non-empty report head that differs from the cut, and refuses a cut head that differs from the producer's (`card/move.go:1137`). An empty head on `fix` or `fault` skips the sha check (`card/move.go:1117`), and `head` leaves the stored head unchanged when the new value is empty (`card/move.go:39`). The stored head still has to equal the producer's. The report itself may omit the head.

The producer table says `ReadFix` writes "reason=the findings policy chose" (`docs/LAYER-2-CARD-MACHINE.md:103`). `ReadFix` writes `finding` and `bounces`, and writes `reason` only when the stream is held, and then the reason is `holdPhrase` (`card/move.go:674`). `TestProducerTable` locks the unheld plan as finding plus bounces and no reason write (`card/move_test.go:112`). The third cold read's finding ruling matches the code. The opening cell still names reason.

## Model vs code

`askReads` accepts one or more rows and numbers the new reads in slice order (`card/move.go:546`, `card/move.go:339`). `AskReads` in `tla/CardMachine.tla:447` requires `Cardinality(R) >= 2`, and `ReadSlot` at `tla/CardMachine.tla:444` numbers by `RowIdx`. The trace writer maps a one-row `AskReads` onto `AskRead` (`card/trace_tla_test.go:90`) and records several rows as a set. The walks in this package pass rows in `r1`, `r2`, `r3` order (`card/coldread4_test.go:828`, `card/coldread3_test.go:347`), which matches `RowIdx`, so those walks do not exercise a call order that differs from `RowIdx`. I did not run TLC. I do not claim a trace match.

The model guards a cut with `RoomOn` (`tla/CardMachine.tla:375`) and has a `Width` action (`tla/CardMachine.tla:749`). `MoveRow` has no `Width` event (`card/move.go:1295`). The machine never reads a row's width. The layer doc assigns that count to the dealer and to the model's `RoomOn`.

`Gone` in the model requires `flaps < 1` (`tla/CardMachine.tla:745`). The code increments flaps with no cap (`card/move.go:1288`). `card/trace.go:42` says the trace module lifts that bound.

`HoldStream` in the model parks every ready producer (`tla/CardMachine.tla:767`). `holdStream` parks the list the caller passes (`card/move.go:1245`), and the merge rerun replaces that list with the ready producers the op has read. The third cold read says the full ready set on a direct call is layer 3's duty.

`tla/CardMachine.tla:458` and `tla/CardMachine.tla:459` both state `UNCHANGED <<rs, held>>`. The second conjunct repeats the first.

## Simplifications

The reason-only branch in `edit` (`card/move.go:955`) is the special case. Deleting it leaves `Edit` as the producer table describes it: no live consumer, and a reason write when the reason changed. The other branches I read are the rulings (the deal cuts working, a route fault blocks, Block from waiting, `InThisOp` on a failed need).

## Missing tests

Command, on hulk, in `/home/gaffer/johnny-jobs/v2r-l2-flash2.w1`, with `PATH=/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin:$PATH`, `GOCACHE` inside the clone, and `GOFLAGS=-mod=readonly`:

```
nice -n 19 go build ./card/...
nice -n 19 go vet ./card/...
nice -n 19 go test -p 2 -count=1 -timeout 600s ./card/...
```

```
go version go1.26.10 linux/amd64
=== go build ===
BUILD_EXIT:0
=== go vet ===
VET_EXIT:0
=== go test ===
panic: test timed out after 10m0s
	running tests:
		TestColdread5Walk (2m6s)
		TestColdread5Walk/seeds/10 (16s)
		TestColdread5Walk/seeds/11 (17s)
		TestColdread5Walk/seeds/2 (11s)
		TestColdread5Walk/seeds/3 (12s)
		TestColdread5Walk/seeds/4 (13s)
		TestColdread5Walk/seeds/5 (12s)
		TestColdread5Walk/seeds/6 (13s)
		TestColdread5Walk/seeds/7 (19s)
		TestColdread5Walk/seeds/8 (14s)
		TestColdread5Walk/seeds/9 (16s)
FAIL	github.com/mas-bandwidth/nova-sprint/card	600.731s
FAIL
```

The dump between the panic and `FAIL` is the walk's goroutines inside `cold5Run.step`, `resync`, and `store.Fake` apply and Snapshot. Those goroutines are runnable. The test runner is waiting on the seed subtests. `TestFunctionalColdread5PG` is absent from the running list, so this run did not start PostgreSQL. The script used `set -e`, so `TEST_EXIT` was not printed; the ssh command exited 1.

No test in `card/` contains the refusal "reason is the hold's while parked". The string occurs at `card/move.go:956`. No test reports a read `fix` or `fault` with an empty head, which is the path at `card/move.go:1117` and `card/move.go:1138`.

`TestFunctionalColdread5PG` (`card/coldread5_test.go:1107`) is the exception the file names at `card/coldread5_test.go:13`. It runs `initdb` and `pg_ctl` (`card/coldread5_test.go:1076`, `card/coldread5_test.go:1087`), listens on `127.0.0.1` (`card/coldread5_test.go:1080`), and waits with `require.Eventually` for 30s at 50ms (`card/coldread5_test.go:1117`). That is a real process, a real socket, and a real clock. This run did not reach it. The other card tests use a seeded `math/rand` and a fixed `t0` plus the op index.

The walk that compares every merged op with the plans applied one by one produced neither a pass nor an assertion failure under the required timeout.

## Questions for the coordinator

`docs/ENFORCED.md` marks L2-U1 through L2-U17 as nothing. The code already has `AskReads`, `Merge`, the `HoldStream` duplicate and `AtSeq` fences, `Relink` `InThisOp`, the finding rules, the 104-byte id cap, a sentinel as a need, `Replace`'s fences, `Withdraw`'s release, `UnholdStream`, and `WaiveRed`. L2-M5 and L2-M9 still say the cut lands in ready; L2-M19 and `card/move.go:377` say working. L2-M20 says a read accepts `fail`; `card/move.go:1109` refuses it. L2-M18 omits Block from waiting; L2-M24 and `card/move.go:1009` allow it. L2-M22 still lists a `Width` move. L2-U2, L2-U3, L2-U18, and L2-U19 are named as layer 3. Is the ledger due a refresh against the code at this pin, with those four rows left on layer 3?

I did not run TLC. I do not claim a trace match.
