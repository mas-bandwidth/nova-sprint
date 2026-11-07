# Layer 3 steps — cold review (v2r-l3-frontier2)

Pin `43d849e29d0e4568ba6a1348a076a59259639938` (`v2-stitch`). Read: `docs/LAYER-3-STEPS.md`, `docs/ENFORCED.md` section 7, every file under `steps/`, `tla/Steps.tla`, `tla/StepsTrace.tla`, and the `Steps*` reversed witnesses (each is `Steps.tla` with one fault). TLC was not run. No trace match is claimed.

Commands, on hulk, `GOCACHE` inside the clone, `GOFLAGS=-mod=readonly`, `nice -n 19`:

```
go build ./steps/...
go vet ./steps/...
go test -p 2 -count=1 -timeout 600s ./steps/...
```

```
BUILD_OK
VET_OK
ok  	github.com/mas-bandwidth/nova-sprint/steps	443.640s
```

`go vet` printed nothing. The package test does not open a socket. `steps/load_test.go` is skipped unless `NOVA_LOAD=1` (it was not set). When enabled it uses `time.Now`, `os/exec` (`initdb`, `pg_ctl`), and a TCP connection to PostgreSQL on `127.0.0.1`.

## Verdict

The layer is not sound. The worst defect is `steps/land.go`: reordering merging cards by `after` does not terminate when those edges cycle, so `Tick` never returns a plan and the run loop stops. Next is the deal-candidate cache, filled in `questions()` before `rest()` and before any row is claimed, so the fault-rest filter never removes a row that is actually being dealt.

## Defects

### 1. An `after` cycle among merging cards hangs the tick

`steps/land.go:203` (`eligible`). Severity: blocker.

The reorder is `for changed := true; changed; { ... }` with no bound. On each pass, for `i, c := range out`, it swaps `out[i]` with a later `out[j]` when `c.After` names `out[j]`. `c` is the card from the start of that iteration, and a swap does not stop the scan. Reading that loop, two cards in `out` that name each other (or a longer cycle) leave `changed` true on every pass, so the scan does not stop. That non-termination, and that an acyclic `after` graph would finish, is source reasoning from `steps/land.go:203`. This report has no command or output for a run of the loop on `["A","B"]` with `A` after `B` and `B` after `A`, and no such receipt is in the tree.

Layer 3 reads `after` off the view. Admission already refuses a cycle through needs or after: `MoveAdd` returns the plan refused at `card/add.go:28-32` when `addCycle` finds a loop. `docs/ENFORCED.md` line 95 (`L2-U1`) does not say nothing checks `after` acyclicity. It leaves unheld only acyclicity of `after` on later `SetCardAfter` writes, beyond self-dependency in `store` and `MoveAdd` in `card`, and in `tla/CardMachine.tla`. The unbounded reorder is reachable only on a path past that admission guard: a cycle written by a later `SetCardAfter`, which `L2-U1` leaves unchecked. A tick that does not return on that view never lands, deals, or judges.

Repair: order with a bounded topological walk and, on a cycle, stop and open an `engine` judgement instead of swapping again.

### 2. The candidate cache is built before rest and before row claims

`steps/tick.go:49` calls `questions()`, which calls `candidates` (`steps/tick.go:223`). `rest()` runs after that, at `steps/tick.go:51` and `steps/limits.go:71`. Severity: serious.

`candidates` memoizes by tier and routes the first time it is called. That first call is while `t.rested` is empty and nothing is claimed. The fill skips a row only when it is already resting or already claimed (`steps/tick.go:223`). Later `rest()` marks rows that took `RouteRestAfter` faults inside `RouteRestFor`, and `limits` can `Gone` a row and claim it. `serving` and the deal reuse the cached list, so neither fact is applied.

What that does: a row the fault rule says is resting is still offered and can be dealt, for the whole rest window, on every tick that has a ready producer (those are exactly the ticks that fill the cache). A row this tick marks `Gone` stays a deal candidate. `Start` fences that row (`card/move.go:277`: `RowGen`, `RowIs` up, not held) while `Gone` writes it. The built notes say a claimed row is not written again in the tick. The cache defeats that. In one packed op both expects can pass against the pre-state, and the op can cut a consumer on the row it also marks gone. In two ops the later `Start` is refused on fresh state with no outside race (`L3-E2`).

Repair: when the cached list is returned, drop rows that are resting or claimed now; do not trust the list frozen inside `questions()`.

## Promise gaps

`docs/LAYER-3-STEPS.md:221`: "fault_limit (3 faults of the last 10 on a row in 30 minutes rest it 30 minutes, LPM R34: a deal filter in the view, never a stored hold". `rest()` does mark `t.rested`. The deal never sees it, because of defect 2. The filter is not stored, and it also does not filter.

`docs/LAYER-3-STEPS.md:324`: "A ready blocker with no room is dealt by preemption in the tick that finds it ready, unless no row serves its tier, which is a judgement in the same tick." `noFreeWidth` (`steps/preempt.go:82`) is roster width (`Working < width`). The deal's room is policy work (`steps/deal.go:174`, `used < room`). The width answer accepts `Work` from 0 through the roster width. A blocker whose serving rows are filled to `work` but still under roster width is not preempted (`preempt` assumes the deal will start it), `pickRow` returns empty with no judgement, and `undealt` also requires `withRoom`, so it does not name the card. It waits until some in-hand consumer finishes and frees a work slot. `docs/ENFORCED.md` `L3-G4` and `tla/Steps.tla` `NoFreeWidth` both say roster width, so the ledger matches the code and not the words "no room" if room means policy work.

The tick list (step 2) says the order is preemption, then reports and takes. `Tick` (`steps/tick.go:53`) runs `reports` then `preempt`. The Built section says that order on purpose, so a consumer that has reported is not preempted. The model has no report inbox; its `Tick` still preempts before land results, which the code also does. The two passages in the layer doc disagree; the code follows the later one.

`docs/LAYER-3-STEPS.md:219` still says `bounce_limit` 3. The Built section says layer 5's later word is 6, and `Engine.Limits.Bounces` is 6 (`steps/asker.go:157`). The code matches the Built sentence, not the earlier one.

## Model vs code

`tla/Steps.tla` `LandElig` is a set filter (drop a card whose `after` target is still merging). It does not reorder by swapping, and `After` in the model is an acyclic constant (`q` after `p` only in `land` and `burst`). The hang in defect 1 is not a behaviour the model can show. The witnesses `StepsAfterHolds` and `StepsAfterIgnored` flip that filter; they do not add a cycle.

`tla/Steps.tla` has no fault-rest and no candidate cache. `RoomIn` and `NoFreeWidth` are roster width only. The code's deal room is policy work. The model cannot see defect 2, and it cannot see a blocker waiting under a work cap.

`ResolveFold` adds a producer to the overlap tally before `Admit`, and `Held` opens an overlap judgement for a later overlapping producer that the tally blocked. `steps/needs.go:42` does the same, including when `group` then returns nil because the budget is already spent and the resolve is not planned. That matches the model. It can still name "held behind" a producer this tick did not plan to resolve. Not filed as a separate defect because the spec does it too.

Documented, and the code matches the Built notes, not silent bugs: rung 2 (every eligible head once `backed_up` is reached) is Go (`TestTickLadderRungs`); the model asks for one head and says so in the module header. Undealt is ticks in the model and seconds in the code; `ju` is not in the trace record. Inbox reports are outside the model. `flowing` is validated and unused. I did not run TLC.

## Simplifications

The candidate cache (`steps/tick.go:216`) is the special case. The comment says the list is built once row claims exist and that it skips rested rows. The call in `questions()` makes both clauses false. Computing the list at the deal, which is a handful of rows, is the filter the comment already describes.

`Limits.Faults`, `SameFinding`, `ReportsPerTick`, and `RouteRestWindow` are range-checked as part of the one `limits` dict (`limitsOK` in `steps/asker.go`) and no step reads them. A bad value rejects `budget` and `pack` with them. The Built section already says `same_finding` is the `brief` judgement, not a count, and that `base_regate` belongs to the edge. `RouteRestWindow` cannot change `rest()` while `RouteRestAfter` is at most `RouteRestWindow` and the only window is `RouteRestFor`, so the field is ceremony. `LandingAnswer.Flowing` is the same kind of unused key; the layer doc already says so.

## Missing tests

No test deals a ready producer while the only serving row is over the fault-rest count, so defect 2 stays green. No test plans `Gone` and a `Start` on that row in one tick. No test builds a cyclic `after` among two merging cards and expects `Tick` to return. No test sets policy work below roster width, fills the row to that cap, and expects the blocker either to preempt or to be named in that tick. `TestTickPreemption` covers width, which is the case the model has.

`TestLintPure` forbids `time.Now`, `time.Sleep`, `net`, and `os/exec` in non-test files. The unit tests that ran obey that. The load test, above, does not, and it is skipped here.

## Questions for the coordinator

Should `Tick` survive a cyclic `after`, or is `L2-U1` (refuse the cycle in the store) the only fix you want? As written, one such pair of merging cards stops the sprint.

Is guarantee 4's "no room" policy work, or roster width as `L3-G4` and `NoFreeWidth` say? If it is width, the blocker waiting under a lower work cap is not a defect. If it is work, `noFreeWidth` is the wrong check, and a full work cap with spare roster width opens no judgement.

Is the candidate list inside `questions()` intentional? `Ask` has to run before the steps, but the memoized row list does not have to be the list the deal uses after `rest` and `Gone`.
