## Verdict

The layer is sound. The moves match the later rulings in the layer doc: the deal cuts `working`, a red goes to fix with the finding and no bounce, a `missing-route:` fault blocks, `Merge` is sequential composition, and `Start` copies `finding`. I found no lost write, no move out of a final state, and no ring or counter bug in the plans the machine emits. The single worst thing is the ledger, not a transition: `docs/ENFORCED.md` still describes cuts in `ready`, a read result of `fail`, a `Width` move, and a block of second-read rulings held by nothing, while `card/move.go` already implements the later rulings. An editor who trusts the ledger will change a machine that matches the layer doc.

## Defects

**minor.** `card/coldread5_test.go:1107` (`TestFunctionalColdread5PG`) is inside `go test ./card/...` and is not a unit test. `cold5StartPG` (`card/coldread5_test.go:1063`) calls `net.Listen("tcp", "127.0.0.1:0")` at `card/coldread5_test.go:1080`, starts PostgreSQL with `pg_ctl`, and the test polls `store.Open` with `require.Eventually` for 30 seconds every 50 milliseconds at `card/coldread5_test.go:1117`. `docs/LAYER-2-CARD-MACHINE.md:281` says the package's unit tests use no store, no clock, and no sockets; the next sentence allows `store.Fake` only. This run was green (250.157s). A missing `initdb` skips the test, and a slow postmaster fails the package for the bench rather than for the machine. Repair: build-tag it, or move it beside the store functional tests, so `go test ./card/...` stays on the fake. Severity: minor.

## Promise gaps

The later ruling in the layer doc is what the code does. These sentences were left behind.

- `docs/ENFORCED.md:68` (L2-M5) says Start "cuts `<p>.w<attempt>` in `ready`". `docs/ENFORCED.md:72` (L2-M9) says AskRead cuts `<p>.r<review>.<n>` in `ready`. `cut` writes the consumer `working` at `card/move.go:377`. The same ledger row L2-M19 (`docs/ENFORCED.md:82`) and the deal-is-take ruling (`docs/LAYER-2-CARD-MACHINE.md:543`) already say the cut is `working` and that no consumer is cut `ready`. `TestDealIsTake` locks the `working` write.

- `docs/ENFORCED.md:83` (L2-M20) says a read consumer accepts `ok`, `fix`, `fail`, or `fault`. `card/move.go:1109` refuses `fail` on a read: "a read consumer reports ok, fix or fault; fail is a work's result". The first cold read (`docs/LAYER-2-CARD-MACHINE.md:378`) already says that.

- The row table (`docs/LAYER-2-CARD-MACHINE.md:205`) still lists `` `Width{n}` | any | same | width, from nova-config only ``, and L2-M22 (`docs/ENFORCED.md:85`) says `Width` updates width. `MoveRow` (`card/move.go:1277`) accepts Seen, Gone, Hold, and Unhold, and anything else is "not a row event" (`card/move.go:1295`). `card/card.go:40` says width and tiers come from the roster (round 22) and the store keeps neither. `card/coldread_test.go:209` expects Width refused in every state.

- `docs/LAYER-2-CARD-MACHINE.md:444`: "Every return into a held stream, and `Unblock`, parks with a reason that names the hold ("held: <the stream's reason>")." `Stream` has `Name` and `Held` only (`card/card.go:53`). `holdPhrase` (`card/move.go:482`) is `"held: stream " + s.Name + " is held"`, and that is what a return, a withdraw that releases into a hold, and `Unblock` write. `Park` with an empty reason (`card/move.go:243`) and `HoldStream` when the caller passes no reason (`card/move.go:1244`) write `"stream " + name + " is held"`, without the `held: ` prefix. L2-U11 (`docs/ENFORCED.md:105`) and the work item at `docs/ENFORCED.md:201` still ask for a `Reason` field on `Stream` and for that exact phrase. The code names the stream. It does not name a reason the stream does not store.

- The producer table (`docs/LAYER-2-CARD-MACHINE.md:97`) still says a cut carries `reason=p.reason`, and the "fix continues" bullet (`docs/LAYER-2-CARD-MACHINE.md:186`) says `Start` copies `prior_head` and `reason`. `cut` copies `p.Finding` onto the work consumer (`card/move.go:373`) and does not copy `Reason`. The second and third cold reads (`docs/LAYER-2-CARD-MACHINE.md:441` and `:471`) say `Start` copies `finding`. `card/coldread2_test.go:84` locks the same: a broken attempt's reason stays off `finding`.

- L2-U6 through L2-U17 (`docs/ENFORCED.md:100` through `:111`) are marked held by nothing, and `docs/ENFORCED.md:196` through `:207` still lists them as work to do. The code already has them: `AskReads` (`card/move.go:542`), sequential `Merge` (`card/replay.go:31`), `HoldStream` duplicate refusal and `AtSeq` (`card/move.go:1242`), `Relink` `InThisOp` (`card/move.go:872`), `Add` capped at 104 bytes (`card/add.go:63`, `maxCardID` at `card/add.go:73`), a sentinel as a need (`card/add.go:87`), `Replace` of a non-final producer or sentinel (`card/move.go:809`), `Withdraw` releasing a working producer and refusing `base_sha` (`card/move.go:1151`), `UnholdStream` (`card/move.go:1257`), and `WaiveRed` from `ready` or `waiting` (`card/move.go:688`). The work item for L2-U17 (`docs/ENFORCED.md:207`) still says `WaiveRed` moves `review` to `merging`; the rule text on `:111` and the code both use `ready` or `waiting`. L2-U10's "Faulted never writes finding" is what `returned` does (`card/move.go:506` writes reason, head, and bounces on Broken only). "Broken carries the reader's text" is the older sentence; `ReadFix` writes `finding` (`card/move.go:678`).

These unheld rows match the code's boundary. L2-U1 (`docs/ENFORCED.md:95`): the machine never emits `SetCardAfter`; acyclicity of a later `after` write sits outside `card`. L2-U2 through L2-U5 are the tick and the liveness witnesses. "A ready producer is started on the next tick" (`docs/LAYER-2-CARD-MACHINE.md:180`) and the per-state "within one tick" column (`docs/LAYER-2-CARD-MACHINE.md:271`) are layer 3's, which the same doc says at `:429`. L2-U18 and L2-U19 are the landing step's `after` rules; `Landed` does not read `after` (`card/move.go:732`), as `docs/LAYER-2-CARD-MACHINE.md:481` says.

## Model vs code

I read `tla/CardMachine.tla`. I did not run TLC. Nothing here is a trace-match claim.

- `Width(r, n)` is still an action (`tla/CardMachine.tla:749`) and an environment move (`tla/CardMachine.tla:806`). `MoveRow` has no Width case (`card/move.go:1295`). Round 22 retired the move in Go. The model still has it.
- `RoomOn` (`tla/CardMachine.tla:375`) guards `Start` (`:379`), `AskRead`, `AskReads`, and `Preempt`. `cut` (`card/move.go:361`) does not count a row's working consumers. `docs/LAYER-2-CARD-MACHINE.md:547` says that split on purpose: the dealer checks roster width, and the model stands in for the dealer. The card tests' `overWidth` is that check on `store.Fake`.
- `AskReads` in the model requires `Cardinality(R) >= 2` (`tla/CardMachine.tla:446`). `askReads` (`card/move.go:542`) refuses zero rows and accepts one.
- `Gone` stops at `flaps < 1` (`tla/CardMachine.tla:745`). Go writes `r.Flaps + 1` (`card/move.go:1288`) and `values.num` refuses a flaps value outside `0..MaxInt32` (`card/values.go:57`). The model's bound is the TLC bound the header already states.
- `HoldStream` in the model parks every ready producer (`tla/CardMachine.tla:764`). Go parks the ready set the caller lists (`card/move.go:1222`), refuses a duplicate id, and fences `AtSeq` (`card/move.go:1242`). `docs/LAYER-2-CARD-MACHINE.md:478` assigns the full list to layer 3.
- The model result `route` (`tla/CardMachine.tla:137`) is the code's result `fault` plus a reason prefixed `missing-route:`. `TraceResult` (`card/trace.go:131`) maps that pair to `route`. `docs/LAYER-2-CARD-MACHINE.md:580` says so.
- `StartCut` copies `finding` (`tla/CardMachine.tla:362`), `Broken` and `Faulted` leave it unchanged (`tla/CardMachine.tla:275`), and `Red` writes the token `"fix"` (`tla/CardMachine.tla:535`). Go writes the caller's reason string as the finding (`card/move.go:770`). `fixOrNone` (`card/trace.go:149`) collapses every non-empty finding to `"fix"` because `Findings` is `{"-", "fix"}` (`tla/CardMachine.tla:151`). Reasons are not model state. I am not saying a trace was checked.

## Simplifications

`edit` refuses a reason-only change while the producer is `waiting` and the stream argument is held (`card/move.go:956`, "reason is the hold's while parked"). The move does not fence `StreamHeld`, so the guard is whatever stream the caller passed. A prose, paths, or done-when edit on the same call still writes the new reason through `reason()`. No later guard reads the reason text. The special case can go. One hold phrase can replace the two that exist (`card/move.go:243` and `card/move.go:482`).

## Missing tests

One combination is untested. `TestRouteFaultBlocks` (`card/move_test.go:666`) passes `Stream{Name: "s1"}` with `Held` false. The route-fault arm returns before `back()` (`card/move.go:519`), so a held stream is ignored and the producer is blocked with the fault reason. No test passes a held stream into that arm. The rest of the tables, including Width refused (`card/coldread_test.go:209`) and a one-row `AskReads` inside the cold walks (`card/coldread_test.go:54`), are present. I am not listing coverage I did not open.

## Questions for the coordinator

- Retarget L2-M5, L2-M9, L2-M20, and L2-M22 to the code, and mark L2-U6 through L2-U17 held by the tests that already exist? The work list at `docs/ENFORCED.md:196` through `:207` describes edits that are already in `card/move.go`.
- Confirm `Width` stays retired, in the row table and in `CardMachine.tla` (`:749`, `:806`), so a later pass does not put the move back. `RoomOn` stays the model's stand-in for the dealer.
- Move `TestFunctionalColdread5PG` behind a functional build tag so `go test ./card/...` matches `docs/LAYER-2-CARD-MACHINE.md:281`?
- For the hold phrase: amend the layer sentence to `"held: stream <name> is held"`, or add the reason column L2-U11 still asks for?

Cold read of package `card` at `43d849e29d0e4568ba6a1348a076a59259639938` (`v2-stitch`), clone `/home/gaffer/johnny-jobs/v2r-l2-pro2.w1` on hulk. I read `docs/LAYER-2-CARD-MACHINE.md`, `docs/ENFORCED.md`, the files under `card/`, and `tla/CardMachine.tla` (plus the holes `tla/README.md` already names, so I would not re-report them as discoveries). I did not run TLC. This review does not claim a trace match.

Commands, from that clone, `PATH` prefixed with `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`, `GOCACHE` at `$PWD/.gocache`, `GOFLAGS=-mod=readonly`, `nice -n 19`:

```
go build ./card/...
go vet ./card/...
go test -p 2 -count=1 -timeout 600s ./card/...
```

```
=== go build ./card/... ===
build_exit:0
=== go vet ./card/... ===
vet_exit:0
=== go test -p 2 -count=1 -timeout 600s ./card/... ===
ok  	github.com/mas-bandwidth/nova-sprint/card	250.157s
test_exit:0
```

`TestFunctionalColdread5PG` is the one test that uses real time, a socket, and a network call. It is recorded under Defects. It passed.
