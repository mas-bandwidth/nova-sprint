# Layer 2 cold review — v2r-l2-frontier2

Pin `43d849e29d0e4568ba6a1348a076a59259639938` on `sprint/v2-review/v2r-l2-frontier2`, clone `/home/gaffer/johnny-jobs/v2r-l2-frontier2.w1` on hulk. Read `docs/LAYER-2-CARD-MACHINE.md`, `docs/ENFORCED.md`, package `card`, and `tla/CardMachine.tla`. TLC was not run. This review does not claim a trace match.

Commands, on hulk, `nice -n 19`, `GOCACHE` inside the clone, `GOFLAGS=-mod=readonly`, `PATH` prefixed with `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`. Go `go1.26.10 linux/amd64`.

```
cd /home/gaffer/johnny-jobs/v2r-l2-frontier2.w1
export PATH=/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin:$PATH
export GOCACHE="$PWD/.gocache"
export GOFLAGS=-mod=readonly
nice -n 19 go build ./card/...
# build_exit:0
nice -n 19 go vet ./card/...
# vet_exit:0
nice -n 19 go test -p 2 -count=1 -timeout 600s ./card/...
# ok  	github.com/mas-bandwidth/nova-sprint/card	259.025s
# test_exit:0
```

## Verdict

The layer is sound on the state machine the model describes: cuts land in working, finals do not move, a current-attempt report or withdraw is fenced and applied in one plan, a route fault blocks and spends no bounce, a red landing returns to fix and spends no bounce, and Merge re-checks the folded state. The single worst thing is the Edit special case in `card/move.go:955` that refuses to replace a parked hold reason only when the edit changes nothing else, and does not fence the stream, so an edit of prose, paths, or done_when plus a reason while the stream is held overwrites the hold phrase. Build, vet, and `go test ./card/...` were green.

## Defects

### 1. Edit overwrites a parked hold reason — minor

`card/move.go:955` (the guard) and `card/move.go:967` (`m.reason`).

The guard refuses with "reason is the hold's while parked" only when the producer is `waiting`, `m.s.Held` is true, the edit changes neither prose, paths, nor done_when, and the reason would change. An Edit that also changes prose (`:958`), paths (`:961`), or done_when (`:964`) still calls `m.reason` and writes the new reason over the park phrase. `edit` never calls `stream` (`card/move.go:188`), so it never checks that the passed stream is the producer's and never emits `store.StreamHeld`. The hold still blocks `Resolve` and `Start`; what is lost is the board reason the guard itself treats as load-bearing.

Repair: when the producer is waiting in its held stream, keep the hold reason no matter what else the edit changes, fence `StreamHeld`, and refuse a stream that is not the producer's.

Severity: minor.

### 2. A merged row hold drops a reason-only change — minor

`card/replay.go:169` (`idleWrite` on `store.SetRowHeld`).

`idleWrite` returns true when `r.Held == x.Held` and ignores `Reason`. `writeFields` records that write as the single column `held` (`card/values.go:321`), so `absorb` (`card/replay.go:290`) keeps only the later `SetRowHeld`. `readsOf` (`card/replay.go:267`) keeps the first plan's row via `unionMissing` (`card/replay.go:860`). `Merge(Unhold, Hold)` is therefore: Unhold's snapshot is held (or `MoveRow` would have refused it, `card/move.go:1311`), Hold re-runs against the folded unheld row and writes `SetRowHeld{Held: true, Reason: the new reason}`, absorb drops the Unhold write, and `quietWrites` (`card/replay.go:136`) drops the survivor because the pre-op held bit already matches. The new reason never leaves the machine. The store would refuse that lone write on its own: `SetRowHeld` is refused when the row is already in that held state, whatever the reason (`store/ops.go:320`, the `rowIf` at `store/ops.go:582`). A single Hold or Unhold still records its reason, because the bit flips. One snapshot cannot build both moves; Merge's contract is sequential composition (`docs/LAYER-2-CARD-MACHINE.md:461`).

Repair: compare `Reason` in `idleWrite` and accept a same-held reason change in the store, or refuse the merge when the surviving `SetRowHeld` would not change the held bit; comparing `Reason` alone turns this merge into the store's "already held" refusal.

Severity: minor.

## Promise gaps

The producer table (`docs/LAYER-2-CARD-MACHINE.md:97`) says a Start cut writes `reason=p.reason`, and the rules bullet (`docs/LAYER-2-CARD-MACHINE.md:186`) says `Start` copies the kept head and reason "to the new work consumer as `prior_head` and `reason`, so the next attempt starts from that head with the finding in hand". `start` copies `prior_head` only when `p.Head != ""` (`card/move.go:408`) and `cut` copies `Finding` onto a work consumer (`card/move.go:373`), never `Reason`. The third cold read (`docs/LAYER-2-CARD-MACHINE.md:469`) moved that carry onto `finding` and left the table line behind. The producer-table tests lock the code. A first-attempt fail therefore leaves the next consumer with an empty finding and an empty reason; the fail text sits on the producer reason.

The second cold read (`docs/LAYER-2-CARD-MACHINE.md:443`) says: "Every return into a held stream, and `Unblock`, parks with a reason that names the hold (`"held: <the stream's reason>"`)." `StreamFromStore` keeps `Name` and `Held` only (`card/card.go:197`); the comment at `card/card.go:204` says a stream has no reason column. `holdPhrase` (`card/move.go:482`) is `"held: stream " + name + " is held"`. `Park` with an empty reason (`card/move.go:243`) and `HoldStream` (`card/move.go:1244`, `cmp.Or(v.Reason, "stream "+s.Name+" is held")`) use a different string, without the `held: ` prefix (`card/card.go:305`). `move_test.go:93` locks `"stream s1 is held"` for Park and HoldStream; `move_test.go:602` locks `"held: stream s1 is held"` for a return into a held stream. The caller's `HoldStream.Reason` is stored only on the cards that op parks, and a later return cannot repeat it.

The row table (`docs/LAYER-2-CARD-MACHINE.md:205`) still lists `Width{n}` as a move. `MoveRow` (`card/move.go:1282`) accepts Seen, Gone, Hold, and Unhold, and the default refusal (`card/move.go:1295`) names those four. Width arrives on the roster (`card/card.go:190`). The machine does not read `Row.Width` when it cuts.

`ENFORCED.md:68` (L2-M5) says Start cuts `<p>.w<attempt>` in `ready`, and `ENFORCED.md:72` (L2-M9) says AskRead cuts `<p>.r<review>.<n>` in `ready`. `cut` sets the consumer to `working` (`card/move.go:377`). The layer doc's own table (`docs/LAYER-2-CARD-MACHINE.md:97` and `:101`) already says working. `TestDealIsTake` locks it.

`ENFORCED.md:3` sets the nothing ceiling at 20, and section 4 (`ENFORCED.md:91`) marks L2-U1 through L2-U19 held by nothing. The behaviors behind L2-U6, L2-U7, L2-U8, L2-U10, and L2-U12 through L2-U17 are in package `card` and covered by tests already in the tree (`TestProbeRead2ValueRulesAndBatchAskRead`, `TestMerge`, `TestHoldStream`, `TestConformanceHoldStreamFencedAtSeq`, `TestRelinkFencesTheWalk`, `TestRouteFaultBlocks`, `TestCold2DefectAddedIdTooLongToStart`, `TestWithdrawReleases`, `TestWaiveRedOnReviewZero`, and the producer table). L2-U10's "Broken carries the reader's text" matches the third cold read's preservation rule (`docs/LAYER-2-CARD-MACHINE.md:469`): `Broken` and `Faulted` do not write `finding`. L2-U11 is the hold-phrase gap above, and the ledger is right that the exact sentence is unheld. L2-U1 is a later `SetCardAfter` cycle check; package `card` checks cycles at Add and on Relink's walk and never emits `store.SetCardAfter`. L2-U2 through L2-U5 are the tick, the liveness obligation, and two reversed witnesses; the layer doc assigns "nothing stalls" to layer 3 (`docs/LAYER-2-CARD-MACHINE.md:429`). L2-U18 and L2-U19 are held in section 7 by `Steps.AfterNeverHolds` and `Steps.AfterKept` (`ENFORCED.md:157`), so those two `nothing` cells disagree with L3-G3.

`ENFORCED.md:122` (CC-2) says Start copies `branch` and `base_sha` from the producer and AskRead copies `branch` and `base_sha`, and it cites `CARD-CONTRACT.md` "How a consumer gets its brief". That heading is not in `docs/CARD-CONTRACT.md`. `cut` copies brief, prose, paths, and done_when (`card/move.go:368`). Start writes branch and base from the Start event (`card/move.go:407`). AskRead does not set them. `TestCutCopiesTask` locks the copy that exists.

## Model vs code

`tla/CardMachine.tla` was read. TLC was not run. No trace match is claimed. `card/trace.go` is the refinement mapping the trace module would check; this review did not run that check.

Aligned with the code: `StartCut` and `ReadCut` put the consumer in `working`; there is no Take. `Park` requires `held` (`tla/CardMachine.tla:348`), as `stream(false)` does (`card/move.go:194`). A fault result `route` blocks and leaves `finding` unchanged (`tla/CardMachine.tla:412`); the code blocks on the `missing-route:` prefix, does not write `finding`, and does not bump bounces. `FixCarriesFailure` (`tla/CardMachine.tla:274`) copies `finding` on Start and Preempt and leaves it unchanged on Broken and Faulted, which is what `cut` and `returned` do. Red goes to `Back` with review+1, red set, finding `"fix"`, and no bounce. WaiveRed does not clear red. Unblock goes to `waiting`. Final states do not move. Edit is a no-op on model state (`tla/CardMachine.tla:622`), which matches "the brief is immutable" and matches the header's statement that reason, prose, paths, and done_when are data no guard reads (`tla/CardMachine.tla:43`). The Edit hold-reason hole is therefore outside the model.

Divergences that are the stated split, not a code bug:

- `RoomOn` (`tla/CardMachine.tla:375`) guards Start, AskRead, AskReads, and Preempt. The layer doc says the machine sees one producer's consumers and the dealer checks width. The code does not read `Row.Width`.
- The model has one `held` flag, and `HoldStream` (`tla/CardMachine.tla:764`) parks every ready producer. The code holds one stream and, on a lone call, parks the caller's list. The merge re-run replaces that list with `readyOn` (`card/move.go:1252`), which parks every ready producer of that stream present in the folded records.
- `Gone` requires `flaps < 1` (`tla/CardMachine.tla:745`). `MoveRow` adds one with no cap (`card/move.go:1288`). `card/trace.go:42` says the trace module lifts that bound.
- `AskReads` requires `Cardinality(R) >= 2` (`tla/CardMachine.tla:447`). `askReads` refuses an empty list and accepts one row (`card/move.go:546`). `card/coldread_test.go:54` builds `AskReads{Rows: []string{"r1"}}`.
- `Width(r, n)` is a model action (`tla/CardMachine.tla:749`). `MoveRow` has no Width event.
- The model's `Edit` comment (`tla/CardMachine.tla:619`) still says Edit writes the brief. The action changes nothing, and the header at line 44 says Edit writes prose, paths, and done_when. The code edits those three plus reason, and leaves the brief alone.
- Heads and finding are abstracted (`card/trace.go:32`). Reason is left out because no model rule reads it (`card/trace.go:44`). One stream's hold is projected onto `held` (`card/trace.go:48`).

## Simplifications

The three hold phrases should be one function: `Park`'s default (`card/move.go:243`), `holdPhrase` (`card/move.go:482`), and `HoldStream`'s `cmp.Or` (`card/move.go:1244`). Edit's hold-reason guard is the special case in defect 1; a parked producer should keep that one phrase on every edit.

`Row.Width` is copied in from the roster (`card/card.go:193`) and the move logic never reads it.

`coveredWorking` (`card/replay.go:306`) still says the move into working "is what the store uses to count the row's width". The store holds no width (L1-G3, `ENFORCED.md:21`). Refusing to collapse that state write can stay, so a deal is not erased inside the op; the comment's reason is stale.

`edit` stores `e.Paths` and `e.DoneWhen` by alias (`card/move.go:962` and `:965`). `cut` clones both (`card/move.go:369`).

## Missing tests

No test contains the refusal "reason is the hold's while parked". The string occurs only at `card/move.go:956`. The hole in defect 1, an edit of prose or paths or done_when together with a reason while the producer is waiting and the stream is held, is untested. The producer-table Edit case changes prose and reason from a card that is not parked under a hold.

No test merges Unhold with Hold and checks the row reason that `quietWrites` emits. Defect 2 is untested.

`TestFunctionalColdread5PG` (`card/coldread5_test.go:1107`) is a functional test inside `go test ./card/...`. `cold5StartPG` (`card/coldread5_test.go:1063`) runs `initdb` and `pg_ctl`, listens with `net.Listen("tcp", "127.0.0.1:0")` (`card/coldread5_test.go:1080`), and returns a `postgres://` DSN (`card/coldread5_test.go:1099`). The test polls `store.Open` with `require.Eventually` for `30*time.Second` at `50*time.Millisecond` (`card/coldread5_test.go:1117`). It uses a real socket, a real PostgreSQL, and a real clock. It is skipped when `initdb` is missing; on this run `initdb` was present and the test passed inside the 259.025s suite. The layer doc says the package's unit tests have no store, no clock, and no sockets (`docs/LAYER-2-CARD-MACHINE.md:279`).

The ENFORCED names (`TestAskReads`, `TestMergeSequentialComposition`, `TestHoldStreamRefusesDuplicateAndFencesAtSeq`, `TestFaultDoesNotWriteFinding`) differ from the test names in the tree. The behaviors those rows name are covered under the names in Promise gaps. They are not missing tests.

## Questions for the coordinator

Is the moves-table `reason=p.reason` on Start retired in favor of `finding`, so the doc should be corrected rather than the code?

Should ENFORCED section 4, L2-M5, and L2-M9 be rewritten? The code implements the behaviors behind L2-U6, L2-U7, L2-U8, L2-U10, and L2-U12 through L2-U17. L2-U18 and L2-U19 are already L3-G3. L2-M5 and L2-M9 still say the cut is `ready`. L2-U11's exact hold sentence is unheld.

Is the Edit hold-reason hole in scope to fix? The code refuses a reason-only edit while parked, so it treats that reason as load-bearing, and then replaces it when the same edit also changes the task.

`HoldStream`'s merge re-run sets `v2.Ready = readyOn(...)` (`card/move.go:1252`), so a hold merged after other moves parks every ready producer of that stream in the folded records. The layer doc says the move parks exactly the caller's list and a stale list refuses rather than filling in (`docs/LAYER-2-CARD-MACHINE.md:478`), and also says a hold merged after a start parks what the start readied (`docs/LAYER-2-CARD-MACHINE.md:483`). The model parks every ready producer (`tla/CardMachine.tla:767`). Which sentence is the rule?

CC-2 says AskRead copies `branch` and `base_sha` and cites a CARD-CONTRACT section that is not in the file. The tests lock the omission. Which side should move?
