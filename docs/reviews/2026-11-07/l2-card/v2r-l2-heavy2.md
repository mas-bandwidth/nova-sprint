## Verdict

The layer is not sound. One Red, one Finished, and the single-move tables match the later rulings, and the loss is in composition: `Merge` keeps the last write of the field `review`, while `SetCardReview` is an increment, so a second `Red` in the same op does not raise review and the reads from before that red still count.

## Defects

1. `card/replay.go:290` (`absorb`) drops the earlier write when a later write names the same field. `SetCardReview` carries no new value. The fold counts it as `c.Review++` (`card/replay.go:963`) and the store does the same (`store/ops.go:535`). `Finished` emits one (`card/move.go:500`) and `Red` emits one (`card/move.go:767`). `WaiveRed` emits none (`card/move.go:688`). A merge of `Red`, `WaiveRed`, `Red` on one producer is accepted. The plan that comes back has a single `SetCardReview` (with the second red's reason and finding, and state `ready`). Applied, review rises by one. The second red was supposed to make the done ok read of the review it left stop counting (`card/move.go:701`, `docs/LAYER-2-CARD-MACHINE.md:498`). A later `WaiveRed` still finds that read. `idleWrite` never drops a review write (`card/replay.go:184`), and `dupWrites` runs after the collapse (`card/replay.go:88`), so the store sees one legal increment rather than the two it would refuse. This three-move op sits inside the model's op length (`tla/CardMachine.tla:101`, `MaxOp` 3 when `Scenario = "merge"`) and `ReviewOnEntry` counts one per `Red` step (`tla/CardMachine.tla:292`). A throwaway program outside this clone, calling `Move` then `Merge` on that triple, printed one `SetCardReview`. Repair: refuse a second `SetCardReview` on the same card in one op, or write the absolute review the fold already counted. Severity: serious.

2. `card/move.go:95` treats a sha as 40 characters from `0-9a-f` only. The layer doc asks for a full 40-hex commit (`docs/LAYER-2-CARD-MACHINE.md:218`). An uppercase hex sha is refused as not a full commit sha, on report, withdraw, start base, and red head. Repair: accept `A-F` in `isSHA`. Severity: nit.

## Promise gaps

The standing review rule and the code diverge on the defect above. "`Red{head, reason}`: `merging -> ready` (`waiting` when the stream is held), review +1 (reads before the red never count)" (`docs/LAYER-2-CARD-MACHINE.md:498`, and the same increment on the Red row at line 105). One `Red` does that. Two `Red`s in one `Merge` leave a single increment.

These sentences in the same doc, and the ledger rows that quote them, describe an older machine. The code follows the later rulings in this file.

- The Start row still says the cut writes `reason=p.reason` (`docs/LAYER-2-CARD-MACHINE.md:97`), and lines 184-187 say `Start` copies the producer's head and reason onto the new work consumer as `prior_head` and `reason`. `start` copies `prior_head` from the producer head and branch and base from the event (`card/move.go:406`), and `cut` copies `finding` onto a work consumer and does not write `reason` (`card/move.go:373`). `card/README.md:111` and `TestCold2DefectBrokenReasonNeverReachesTheNextAttempt` state that rule.
- The ReadFix row says the write is `reason=the findings policy chose` (`docs/LAYER-2-CARD-MACHINE.md:103`). `readFix` writes `finding` (`card/move.go:677`) and writes `reason` only when the stream is held, and then the text is the hold phrase (`card/move.go:674`).
- The row table still lists `Width{n}` (`docs/LAYER-2-CARD-MACHINE.md:205`). `MoveRow` takes Seen, Gone, Hold, and Unhold (`card/move.go:1294`). `docs/ENFORCED.md` L2-M22 still says the machine updates width. The deal section of the layer doc (line 547) and store round 22 put width on the dealer and the roster.
- `docs/ENFORCED.md` L2-M9 says `AskRead` cuts the consumer in `ready`. `cut` writes `working` (`card/move.go:377`). L2-M19 in the same ledger matches the code.
- `docs/ENFORCED.md` L2-M20 says a read accepts `ok`, `fix`, `fail`, or `fault`. A read result `fail` is refused (`card/move.go:1109`). The layer doc's cold-read ruling (lines 378-379) matches the code.
- `docs/ENFORCED.md` CC-2 says `AskRead` copies `branch` and `base_sha` onto the read, and attributes that to `CARD-CONTRACT.md`. That page is the brief lint. `oneRead` copies head and author (`card/move.go:603`) and `cut` copies brief, prose, paths, done_when, and the read's review (`card/move.go:368`).
- `docs/ENFORCED.md` L2-U1 through L2-U19 are all marked `nothing`. The code holds the rulings those rows describe (AskReads, Merge, AtSeq, Relink `InThisOp`, finding, the id cap, sentinel needs, Replace fences, Withdraw release, UnholdStream, WaiveRed). L2-U2, L2-U3, L2-U18, and L2-U19 are the tick, the liveness check, and soft `after`, which this layer does not implement and the layer doc assigns to layer 3.

## Model vs code

`ReviewOnEntry` (`tla/CardMachine.tla:292`) sets `ps'[q].review` to the old review plus one when the step's event is `Finished` or `Red`. The header (line 59) says the counter rises once per Finished or Red however the op is nested. The code's fold does that per move, then `absorb` (`card/replay.go:290`) emits one `SetCardReview` for the whole op. I did not run TLC and I do not claim a trace match.

`Width` is still an action (`tla/CardMachine.tla:749`) and `WidthKept` still bounds working consumers by `rs[r].width`. The Go machine has no Width event and no `RoomOn` check. The layer doc (line 547) says the dealer checks width and the model keeps the guard. The card tests' `overWidth` is that dealer check on the fake, labeled as such (`card/conformance_test.go:103`).

`HoldStream` in the model parks every ready producer (`tla/CardMachine.tla:764`). `holdStream` parks the caller's list (`card/move.go:1245`). On a merge re-run it replaces that list with `readyOn`, every ready producer of the stream the fold can see (`card/move.go:1250`). The layer doc says both things: the caller lists the ready set (line 478), and a hold merged after a resolve parks what that resolve readied (line 483). `docs/ENFORCED.md` L2-M23 says every ready producer.

The model says a sequence that is legal one move at a time is one op, and a second write of a field is not refused (`tla/CardMachine.tla:819`). `coveredWorking` (`card/replay.go:306`) refuses a later `SetCardState` on a card this op already moved to `working`, so `AskRead` then `Report` is not one op in the code. That refusal is the safe direction: the store will not take two writes of `state`, and dropping the move into `working` would hide the cut.

The model header (lines 43-45) says finding is left out because no guard reads it. The spec tracks `finding` and `red`, and `FixSaysWhat` and `ReviewOnEntry` read them.

`AskReads` in the model requires two or more rows. The code accepts a list of one, and the trace renders that one row as `AskRead`.

## Simplifications

`edit` refuses a reason-only change while the producer is `waiting` in a held stream, with "reason is the hold's while parked" (`card/move.go:955`). An `Edit` that also changes prose, paths, or done_when still writes the new reason (`card/move.go:967`). The stream flag still refuses `Resolve`. That branch is the only use of the phrase, and nothing in `card/` tests it. Deleting it leaves `Edit` as the table describes it: prose, paths, done_when, and reason, with no live consumer.

## Missing tests

Nothing in `card/` merges two review increments and counts `SetCardReview`. `TestProducerTable/Red` asserts one `Red` in isolation (`card/move_test.go:115`). The hold-reason edit refusal has no test. The package walk (`TestColdread5Walk`, 100 seeds, 2000 steps, `t.Parallel` at `card/coldread5_test.go:1046`) did not finish under the commanded timeout, so this run did not show that walk green, and it did not start `TestFunctionalColdread5PG`.

## Questions for the coordinator

Should a second `SetCardReview` in one op be refused, or should the write carry the absolute review?

`go test ./card/...` under `nice -n 19` and `-timeout 600s` died on the package alarm inside `TestColdread5Walk`, with no assertion failure printed. Is that timeout the budget for this walk, or does the walk belong outside this gate?

`docs/ENFORCED.md` L2-M9, L2-M20, L2-M22, CC-2, and the L2-U rows marked `nothing` disagree with the later rulings in `docs/LAYER-2-CARD-MACHINE.md` that the code follows. Which of those ledger rows should be rewritten?

Test run, on hulk, in `/home/gaffer/johnny-jobs/v2r-l2-heavy2.w1`:

```
export PATH=/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin:$PATH
mkdir -p .gocache
nice -n 19 env GOCACHE="$PWD/.gocache" GOFLAGS=-mod=readonly go build ./card/...
BUILD_OK
nice -n 19 env GOCACHE="$PWD/.gocache" GOFLAGS=-mod=readonly go vet ./card/...
VET_OK
nice -n 19 env GOCACHE="$PWD/.gocache" GOFLAGS=-mod=readonly go test -p 2 -count=1 -timeout 600s ./card/...
panic: test timed out after 10m0s
        running tests:
                TestColdread5Walk (2m27s)
                TestColdread5Walk/seeds/16 (10s)
                ... many other seeds still running, up to seeds/73 (1m11s) ...
FAIL    github.com/mas-bandwidth/nova-sprint/card    601.867s
FAIL
TEST_EXIT:1
```

No test printed an assertion failure before the alarm. `TestColdread5Walk` is 100 parallel seeds of 2000 steps (`card/coldread5_test.go:291`). `TestFunctionalColdread5PG` is declared after it and did not start. That test, when it runs, starts a real PostgreSQL (`initdb`, `pg_ctl`, `net.Listen` on `127.0.0.1:0` at `card/coldread5_test.go:1080`) and waits on `require.Eventually` for 30 seconds (`card/coldread5_test.go:1117`). The other card tests pass time in as `store.Op.At`. I did not re-run the package and I did not change a test.
