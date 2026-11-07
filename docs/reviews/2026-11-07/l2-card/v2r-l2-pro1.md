## Verdict

The card machine is sound. Moves, refusals, generation fences, and sequential Merge follow the layer doc's later rulings: a deal cuts the consumer already working, a red goes to fix and spends no bounce, a missing-route fault blocks on the first fault, one live work consumer of the current attempt, and a yes needs a done ok read. On hulk, `go build ./card/...` and `go vet ./card/...` exited 0, and `go test -p 2 -count=1 -timeout 600s ./card/...` finished `ok github.com/mas-bandwidth/nova-sprint/card 573.113s`. The single worst thing is minor: Start does not copy the producer's reason onto the new work consumer (`card/move.go:407-411`), so a fail text that Broken stored on the producer (`card/move.go:527`) is absent from the next attempt's hand. The reader's finding is copied at the cut (`card/move.go:373`); the worker's fail text is not.

## Defects

### Start leaves the producer's reason off the new work consumer

Severity: minor.

`card/move.go:407-411` sets the new work consumer's columns to branch, base sha, and prior head. `card/move.go:373` copies `p.Finding` onto a work put, and `cut` (`card/move.go:361-377`) writes no reason. `docs/LAYER-2-CARD-MACHINE.md:97` lists `reason=p.reason` on that cut, and `docs/LAYER-2-CARD-MACHINE.md:186` says Start copies prior head and reason onto the new work consumer.

`returned` (`card/move.go:527`) writes the consumer's fail or fault text onto the producer's reason and writes no finding. That matches the third cold read for a fault (`docs/LAYER-2-CARD-MACHINE.md:469-471`: a fault's reason goes to reason, and finding is written by a read). After Broken, the fail text therefore lives only on the producer. The next Start hands the row a work consumer with an empty reason and with whatever finding a read or a red wrote earlier. `card/move_test.go:32` builds the producer with `Reason: "old"`, and `card/move_test.go:97-100` expects the Start plan to contain no `SetCardReason` on `p1.w2`, so the table locks the omission.

Repair: when `p.Reason` is non-empty, write `SetCardReason` on the new work consumer from `p.Reason`.

## Promise gaps

The Start sentences above are the defect. They are quoted here so the gap is on the page, and they are not ranked a second time. `docs/LAYER-2-CARD-MACHINE.md:97`: "cuts a work consumer: kind work, of p, holder row, attempt, branch, base_sha, prior_head=p.head, reason=p.reason, state working". `docs/LAYER-2-CARD-MACHINE.md:186`: "Start copies them to the new work consumer as prior_head and reason". The code writes prior head (`card/move.go:408-410`) and finding (`card/move.go:373`), and writes no reason on the consumer. The second cold read's sentence at `docs/LAYER-2-CARD-MACHINE.md:440-442` (Broken and Faulted both write finding) is replaced by the third cold read at `:472-473`. The code follows the replacement for a fault.

`docs/ENFORCED.md:68` (L2-M5) says Start "cuts `<p>.w<attempt>` in `ready`". `docs/ENFORCED.md:72` (L2-M9) says AskRead "cuts `<p>.r<review>.<n>` in `ready`". `cut` writes `Working` (`card/move.go:377`). The layer doc's Start row (`docs/LAYER-2-CARD-MACHINE.md:97`) and AskRead row (`:101`) say state working, and `docs/ENFORCED.md:82` (L2-M19) says the deal cuts the consumer working. L2-M5 and L2-M9 are the stale cells. L2-M5's other half, carrying `p.finding`, matches `card/move.go:373`.

`docs/ENFORCED.md:83` (L2-M20) says "a read consumer accepts `ok`, `fix`, `fail`, or `fault`". `docs/LAYER-2-CARD-MACHINE.md:378-379` says a read consumer reports ok, fix, or fault. `card/move.go:1109-1110` refuses `fail` on a read. The code matches the layer doc.

`docs/LAYER-2-CARD-MACHINE.md:205` still lists a row event `Width{n}` that writes width. `docs/ENFORCED.md:85` (L2-M22) still says the row moves "update `width` (`Width`)". `MoveRow` takes Seen, Gone, Hold, and Unhold (`card/move.go:1295`). `card/coldread_test.go:209` records the later ruling: "Width is refused in every state: it is the roster's (round 22)." `card/card.go:40-42` says the same: width and tiers come from the roster, and the store keeps neither. `Row.Width` is a value the caller passes in. The model still has the action (`tla/CardMachine.tla:749`).

`docs/LAYER-2-CARD-MACHINE.md:443-444`: "Every return into a held stream, and Unblock, parks with a reason that names the hold ("held: <the stream's reason>")." `card.Stream` is name and held (`card/card.go:53-56`); there is no stream reason to interpolate. Returns and Unblock use `holdPhrase` (`card/move.go:482`, used at `:488` and `:1039`): `held: stream NAME is held`. Park's default (`card/move.go:243`) and HoldStream's default (`card/move.go:1244`) use `stream NAME is held`, without the `held:` prefix. The same park is two phrases. `docs/ENFORCED.md:105` (L2-U11) and the prescribed fix at `:201` still ask for a `Reason` field on `card.Stream` and one exact phrase, and still say the claim is held by nothing.

`docs/LAYER-2-CARD-MACHINE.md:180`: "ready lasts one tick: a ready producer is started on the next tick, or parked with the reason it cannot start." This package has no tick. The same bullet says the held stream is the parking this layer defines, and that the other reasons a producer cannot start are owed a decision. `docs/ENFORCED.md:96` (L2-U2) records that the model has no tick and marks the claim held by nothing. That is a gap the doc already assigns onward.

Section 4 marks L2-U6 through L2-U17 held by nothing. The rows opened here are L2-U6 (`docs/ENFORCED.md:100`, AskReads), L2-U7 (`:101`, Merge), and L2-U11 (`:105`, the hold phrase). The Nothing Rows list (`docs/ENFORCED.md:196-207`) still tells someone to implement AskReads, Merge, HoldStream's duplicate check and AtSeq fence, Relink's InThisOp, the finding rule, the 104-byte id cap, sentinel needs, Replace's fences, Withdraw's base and release checks, UnholdStream, and WaiveRed. In this checkout AskReads is `card/move.go:542`, Merge is `card/replay.go:31`, HoldStream fences `AtSeq` at `card/move.go:1242`, Add refuses an id over 104 bytes at `card/add.go:63`, and UnholdStream is `card/move.go:1257`. The ledger is not a map of what is still open. L2-U1 through L2-U5 stay open on the ledger's own terms (a later `SetCardAfter`, the tick, liveness, and two reversed witnesses). Those sit outside this package's moves. L2-U18 and L2-U19 are named as layer 3.

## Model vs code

TLC was not run. Nothing here is a trace match.

`tla/CardMachine.tla:749` still has `Width`. Go has no Width event (`card/move.go:1295`). A trace that never takes Width is allowed by a spec that permits it. `tla/CardMachineTrace.tla:99` still dispatches a Width step.

`tla/CardMachine.tla:743-746` stops `Gone` once `flaps >= 1`. `card/move.go:1288` counts flaps on every Gone. `tla/CardMachineTrace.tla` `GoneAt` (the comment at line 28, the action at line 53) lifts that bound for traces.

`tla/CardMachine.tla:764-767` parks every ready producer on HoldStream. `card/move.go:1245` parks the caller's list, and the merge rerun replaces that list with the ready producers of the folded snapshot (`card/move.go:1250-1253`). `docs/LAYER-2-CARD-MACHINE.md:478-480` states this split: the machine parks the listed set, and passing the full ready set is layer 3's duty. The AtSeq fence refuses a stale list.

`tla/CardMachine.tla:444` assigns an AskReads slot by `RowIdx`. Go assigns read ids in caller order (`card/move.go:542` and the read-id helper above the cut). `card/trace_tla_test.go:93` records AskReads rows as a set. A caller slice that is not in `RowIdx` order would not match the model's slot assignment. This review did not run that check.

A fault whose reason starts `missing-route:` is the model's result `route`. `card/trace.go:37-38` is the mapping. The model does not read branch, base_sha, reason, brief, prose, paths, done_when, cost, priced, tier, or gen (`card/trace.go:44-47`).

## Simplifications

`card/move.go:955-956` refuses an Edit that changes only the reason while the producer is waiting and the stream is held ("reason is the hold's while parked"). The reason write at `card/move.go:967` still runs when the same Edit also changes prose, paths, or done_when. One guard, and a substantive edit steps around it. It reads as deliberate. Dropping it, or applying it to every Edit while parked, would remove the special case.

`live` (`card/move.go:59`) still treats `ready` as live after the deal-is-take cut. The doc says a consumer is cut working, and that an old ready consumer is withdrawn. Counting `ready` keeps a leftover ready consumer live, which is what "no live consumer" has to mean for those leftovers. It earns its place.

## Missing tests

`TestProducerTable`'s Start case (`card/move_test.go:95-100`) uses a producer whose reason is `"old"` (`card/move_test.go:32`) and expects no `SetCardReason` on `p1.w2`. The test locks the omission in the defect above. It does not check the move-table sentence that the cut carries `reason=p.reason`.

`TestFunctionalColdread5PG` (`card/coldread5_test.go:1107`) is inside `./card/...` and it passed in the package run below. It is not a unit test. `cold5StartPG` (`card/coldread5_test.go:1063`) runs `initdb`, listens on `127.0.0.1:0` (`:1080`), and starts PostgreSQL with `pg_ctl`. The test then waits on `require.Eventually` for 30 seconds at 50 milliseconds (`card/coldread5_test.go:1117-1123`) and opens that server. On hulk, `initdb` is on PATH, so the test did not skip. The other card tests opened here stamp ops from a fixed `t0` (`card/conformance_test.go:23`, `card/coldread5_test.go:363`) and do not listen or dial. The trace tests write a TLA log (`card/trace_tla_test.go`) and do not run TLC.

Commands, on hulk, in `/home/gaffer/johnny-jobs/v2r-l2-pro1.w1`, with `PATH` prefixed by `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`, `GOCACHE` set to `$PWD/.gocache`, and `GOFLAGS=-mod=readonly`. `go version` was `go1.26.10 linux/amd64`.

```
nice -n 19 go build ./card/...
BUILD_EXIT:0
nice -n 19 go vet ./card/...
VET_EXIT:0
nice -n 19 go test -p 2 -count=1 -timeout 600s ./card/...
ok  	github.com/mas-bandwidth/nova-sprint/card	573.113s
TEST_EXIT:0
```

## Questions for the coordinator

Is `reason=p.reason` still wanted on the work consumer? The move table (`docs/LAYER-2-CARD-MACHINE.md:97`) and the fix-continues bullet (`:186`) say yes. The later ruling (`:389-391`, `:469-471`) says finding is what a read writes and what Start copies, and that reason stays the status reason. The tests lock the current writes.

Should `docs/ENFORCED.md` L2-M5, L2-M9, L2-M20, L2-M22, and section 4's L2-U6 through L2-U17 be refreshed so the ledger matches the code and the later rulings?

Confirm that round 22 retired `Width` as a row event, so the layer-doc table, L2-M22, and `tla/CardMachine.tla:749` can drop it together.
