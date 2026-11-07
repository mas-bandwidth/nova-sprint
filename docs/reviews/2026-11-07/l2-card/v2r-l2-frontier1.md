## Verdict

The card machine is sound on the transitions the later rulings in `docs/LAYER-2-CARD-MACHINE.md` name: `Start` and `AskRead` cut a consumer already `working`, a `missing-route:` fault blocks on the first fault, a read report refuses `fail`, there is no `Width` move, and `Merge` refuses a second write of one field. The single worst thing is `docs/ENFORCED.md` section 4, which still marks those rulings unheld (`nothing`) while the opening tables and several `L2-M*` rows still describe a `Width` move, a consumer cut in `ready`, and a read that accepts `fail`, so a repair aimed at the ledger would change code that already follows the later rulings. The required `go test ./card/...` is red by the package timer, not by an assertion: it panicked at 600s while `TestFunctionalColdread5PG/seeds/1` had been running 13s inside a real PostgreSQL `Apply`, and no test printed an assertion failure. TLC was not run. This review does not claim a trace match.

```
ssh -o BatchMode=yes hulk 'cd /home/gaffer/johnny-jobs/v2r-l2-frontier1.w1 && mkdir -p .gocache && nice -n 19 env GOCACHE="$PWD/.gocache" GOFLAGS=-mod=readonly PATH="/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin:$PATH" go build ./card/... && echo BUILD_OK && nice -n 19 env GOCACHE="$PWD/.gocache" GOFLAGS=-mod=readonly PATH="/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin:$PATH" go vet ./card/... && echo VET_OK && nice -n 19 env GOCACHE="$PWD/.gocache" GOFLAGS=-mod=readonly PATH="/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin:$PATH" go test -p 2 -count=1 -timeout 600s ./card/... && echo TEST_OK'
```

```
BUILD_OK
VET_OK
panic: test timed out after 10m0s
	running tests:
		TestFunctionalColdread5PG (13s)
		TestFunctionalColdread5PG/seeds/1 (13s)
FAIL	github.com/mas-bandwidth/nova-sprint/card	600.136s
FAIL
```

`go build ./card/...` and `go vet ./card/...` both exited 0. The alarm's running list contained only `TestFunctionalColdread5PG`, so no other test was in progress when the process died. Tests registered after that function in source order had not started. The stop of that PostgreSQL is `t.Cleanup` in `cold5StartPG` (`card/coldread5_test.go:1092`), which a package-alarm crash does not run.

## Defects

None. Every mismatch I could pin to a line is a stale sentence in the ledger or the opening tables, a model bound the trace module already names, or a special case listed below. I am not counting those as defects of the machine.

## Promise gaps

`docs/LAYER-2-CARD-MACHINE.md` line 205: "`Width{n}` | any | same | width, from nova-config only (LESSONS 1.12); refused below the row's `working` count by layer 1". `docs/ENFORCED.md` `L2-M22` says row moves "update `width` (`Width`)". `MoveRow` (`card/move.go:1282-1295`) accepts `Seen`, `Gone`, `Hold`, and `Unhold`, and the default refusal says "MoveRow takes Seen, Gone, Hold and Unhold". There is no `Width` event in `card/`. That matches store round 22 (the store holds no width). The row table and `L2-M22` are the stale promise.

`docs/ENFORCED.md` `L2-M5`: "`Start` ... cuts `<p>.w<attempt>` in `ready`". `L2-M9` says `AskRead` cuts `<p>.r<review>.<n>` in `ready`. The same file's `L2-M19` says "`Start` and `AskRead` cut the consumer `working`" and "no consumer is ever `ready`". `cut` (`card/move.go:377`) writes `SetCardState` `working`. The code follows `L2-M19` and the "Deal is take" section (`docs/LAYER-2-CARD-MACHINE.md` lines 543-550).

`docs/ENFORCED.md` `L2-M8`: `Faulted` "moves a `working` producer ... to `ready` (or `waiting` if the stream is held) without raising `bounces`". The same file's `L2-M24` says a work fault whose reason starts `missing-route:` "blocks its producer on the first fault, never back to ready". `returned` (`card/move.go:514-522`) writes `blocked` on that fault and returns before `back()`. Other faults go through `back()` (`card/move.go:524-527`), which parks when the stream is held. The code follows `L2-M24`. `L2-M8` omits that branch.

`docs/ENFORCED.md` `L2-M18`: `Block` "moves a `ready` or `review` producer (or a `working` producer whose current work consumer was withdrawn to `fail`)". `block` (`card/move.go:1009-1013`) also accepts `waiting`, and refuses a sentinel with "sentinel %s is never dealt; there is nothing to block". That is `L2-M24` and the layer-doc `Block` row (line 114). `L2-M18` omits both.

`docs/ENFORCED.md` `L2-M20`: "a read consumer accepts `ok`, `fix`, `fail`, or `fault` and requires `head == p.head`". `reportHead` (`card/move.go:1109-1110`) refuses `fail` on a read: "a read consumer reports ok, fix or fault; fail is a work's result". The first-cold-read ruling (`docs/LAYER-2-CARD-MACHINE.md` lines 378-379) says the same. A read `fix` or `fault` with an empty head is accepted (`card/move.go:1117-1120`); `head` writes only when the new value is non-empty (`card/move.go:39-42`), so the stored head stays the cut head. The refusal text in the layer doc (line 219) is "producer X is at head H1, the read was of H2". The code emits "read %s was cut at %s, the report names %s" or "read %s was cut at %s; producer %s is at %s" (`card/move.go:1138-1141`).

`docs/ENFORCED.md` `CC-2`: "`AskRead` copies `paths`, `done_when`, `prose`, `branch`, `base_sha`, `head`, `author`, and `review` to the read consumer". `oneRead` (`card/move.go:603-607`) writes `SetCardHead` and, when the author is non-empty, `SetCardAuthor`. `cut` (`card/move.go:368-371`) copies brief, prose, paths, done-when, stream, tier, and review. It does not write `branch` or `base_sha` on a read. The layer-doc `AskRead` row (line 101) lists "kind read, of p, holder row, head=p.head, author=p.author, state working" and does not list branch or base. The code follows that row. The opening sentence (lines 22-24) calls a consumer "the brief plus its place to work (repository, base commit, branch, done-when, how to report)". `Start` does write branch, base, and prior head on the work consumer (`card/move.go:406-410`).

`docs/LAYER-2-CARD-MACHINE.md` line 98, the `Start` row: the cut carries "prior_head=p.head, reason=p.reason". Lines 185-186: "`Start` copies them to the new work consumer as `prior_head` and `reason`". The later ruling (lines 440-442, replaced in part by lines 469-472) says `Start` copies `finding`. `cut` (`card/move.go:373`) sets `PutCard.Finding` from `p.Finding` for a work consumer and does not copy `p.Reason`.

`docs/LAYER-2-CARD-MACHINE.md` lines 443-444: every return into a held stream, and `Unblock`, "parks with a reason that names the hold (`held: <the stream's reason>`)". `holdPhrase` (`card/move.go:480-482`) returns `"held: stream " + s.Name + " is held"` because, as that comment says, a stream has no reason column. `Park` with an empty reason (`card/move.go:242-244`) and `HoldStream` with an empty reason (`card/move.go:1244`) write `"stream " + name + " is held"`, without the `held: ` prefix. Returns, a releasing `Withdraw`, and `Unblock` in a held stream use `holdPhrase`. `docs/ENFORCED.md` `L2-U11` still says this is held by `nothing`.

`docs/ENFORCED.md` section 4 marks `L2-U6` through `L2-U17` held by `nothing`, and the "Nothing Rows" list (lines 196-207) still says to implement `AskReads`, sequential `Merge`, `HoldStream` duplicate and `AtSeq` fences, `Relink` `InThisOp`, finding rules, the 104-byte id cap, sentinel needs, `Replace` checks, `Withdraw` base and release, `UnholdStream`, and `WaiveRed`. Those moves are in `card/` now (`askReads` at `card/move.go:542`, `holdStream` at `card/move.go:1222`, `MoveAdd`'s 104-byte cap in the comment at `card/add.go:13`). `L2-U1`, `L2-U2`, `L2-U3`, `L2-U4`, `L2-U18`, and `L2-U19` are about a later `SetCardAfter`, the tick, or layer 3's landing step. The layer doc (lines 429-430 and 481-482) assigns "nothing stalls" and "a landing never holds on a soft after" to layer 3. This package has no tick, so "`ready` lasts one tick" (lines 180-183 and the table at line 271) is not a move this layer can miss.

`docs/ENFORCED.md` `L2-M23`: "`HoldStream` ... parks every `ready` producer in the stream". The later ruling (lines 478-480) says it "parks exactly the ready set the caller lists" and a stale list refuses rather than filling in. `holdStream` (`card/move.go:1227-1248`) parks `v.Ready` and fences `AtSeq`. The re-run (`card/move.go:1250-1253`) replaces that list with `readyOn` of producers present in the fold, which is the third-cold-read rule that a hold merged after a resolve parks what the op readied (lines 483-484).

## Model vs code

`ReadSlot` (`tla/CardMachine.tla:444-445`) numbers an `AskReads` set by `RowIdx`. `askReads` (`card/move.go:552-560`) cuts in `e.Rows` order, and `readID` (`card/move.go:339-351`) assigns `<p>.r<review>.<n>` in that order. `tlaSet` (`card/trace_tla_test.go:20-26`) sorts the names, and a one-row `AskReads` is encoded as model `AskRead` (`card/trace_tla_test.go:89-93`). A request whose rows are not in name order is a state `AskReads(p, R)` cannot produce, because the model action has no request order. The walks I opened pass name order: `cold3Ask` and `cold4Readers` build `r1`, `r2`, `r3` and take a prefix (`card/coldread3_test.go:347-355`, `card/coldread4_test.go:825-836`). Invariants do not read which row received which index. I did not run TLC and I do not claim a trace match.

`ReportHeads` for a read (`tla/CardMachine.tla:671-673`) is only `{ps[c[1]].head}`. `reportHead` (`card/move.go:1117-1120`) also accepts an empty head on a non-ok read. The stored head is unchanged in that case (`card/move.go:39-42`). Model `ReportResults` for a read (`tla/CardMachine.tla:679`) is `ok`, `fix`, and `fault`, which matches the code and not `L2-M20`.

Model `AskReads` requires `Cardinality(R) >= 2` (`tla/CardMachine.tla:447`). The code allows one row (`card/move.go:546-548` refuses only an empty list). The trace maps that one row to `AskRead`, so a recorded trace does not contain a one-row `AskReads`.

Model `HoldStream` is specified to park every ready producer (`docs/ENFORCED.md` `L2-M23` cites `CardMachine.ParkedUnderHold`). The Go move parks the caller's list, as the later ruling says. Model `Start`, `AskRead`, `AskReads`, and `Preempt` guard with `RoomOn` (`tla/CardMachine.tla:433` and `:450`). Go `cut` does not (`card/move.go:354-357`); the layer doc (line 98 and lines 571-575) says width is the dealer's check and the model's guard. `Gone` in the model stops at flaps 1; `MoveRow` (`card/move.go:1288`) adds one with no cap. `trace.go:42-43` says `CardMachineTrace.tla` `GoneAt` lifts that bound. `Landed` in the model takes a head; Go `landed` (`card/move.go:732-745`) also writes "landed at <merge>" when merge or reason is set. `trace.go:44-46` says reason is left out of the model because no guard reads it.

## Simplifications

`edit` (`card/move.go:955-957`) refuses a reason-only change when the producer is `waiting` and `m.s.Held` is true, with "reason is the hold's while parked". It does not check `m.s.Name == m.p.Stream`, which `stream` does (`card/move.go:188-192`). A change to prose, paths, or done-when still writes `e.Reason` (`card/move.go:967`). `Resolve` keys off the stream flag, not the reason text, so a parked card is not stuck if the phrase is overwritten. The guard is a special case the other moves do not need.

`AskReads` in `tla/CardMachine.tla:458-459` repeats `UNCHANGED <<rs, held>>`. The second line does not change the action.

`MoveAdd` (`card/add.go:30`) joins a cycle path with `" needs "` for both `needs` and `after` edges. The layer doc's example (line 227) is a needs cycle, "c1 needs c2 needs c1". An after-edge is named the same way.

## Missing tests

`TestCutCopiesTask` (`card/move_test.go:524-538`) asserts brief, prose, paths, and review on the read `PutCard`. It does not assert `branch` or `base_sha`. `docs/ENFORCED.md` `CC-2` lists that test as holding the promise that `AskRead` copies branch and base. The test does not check the fields the code omits.

No test I opened calls `AskReads` with rows out of name order and checks which `<p>.r<review>.<n>` each row receives. `TestModelCarriedByTheDeal` (`card/model_test.go:22-29`) asks `r2`, `r3` and checks the model by holder.

`TestFunctionalColdread5PG` (`card/coldread5_test.go:1107-1136`) is the functional exception the file header names (lines 13-16). It uses real time (`require.Eventually` with 30s and 50ms, line 1117), a TCP socket (`net.Listen("tcp", "127.0.0.1:0")`, line 1080), `pg_ctl` (line 1087), and `store.Open` to that PostgreSQL. It skips when `initdb` is absent (lines 1066-1073). On this run it did not skip. The other `card` tests I searched take `At` as `t0` plus the op number, or no clock. The package alarm fired during seed 1 of this test, so this run does not say whether the four seeds of 800 steps would have matched the Fake.

## Questions for the coordinator

Which text should a later card treat as the promise: `docs/ENFORCED.md` section 4 and rows `L2-M5`, `L2-M8`, `L2-M9`, `L2-M18`, `L2-M20`, `L2-M22`, and `CC-2`, or the later rulings in `docs/LAYER-2-CARD-MACHINE.md` (deal is take, defect 48, the third cold read)? The code follows the later rulings. I did not edit either document.

The mandated `go test -p 2 -count=1 -timeout 600s ./card/...` under `nice -n 19` did not finish. The walks registered before `TestFunctionalColdread5PG` had already used the budget. Do you want that functional walk outside this package timeout, or is a red timer on a loaded host an accepted result of this card?

I did not run TLC. I did not read any other review file.
