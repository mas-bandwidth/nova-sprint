## Verdict

The write path holds against the later layer doc and `Store.tla`: one operation is one transaction, an id applies once, writers take the epoch lock, a fresh store is stopped with reason `never started`, and the store holds no width. The single worst thing is that `checkRebuild`, which guarantee 5 and the 2026-11-07 stop entry say must show `Rebuild(events)` equal to the snapshot, compares identity and leaves out `Stopped` and `StopReason`, so an `UPDATE` of `sprint.epoch` that flips the stop still passes `ValidateHistory` and `RestoreEnd`. This run's `go test ./store/...` is red in four functional tests; those assertions still describe the snapshot and schema from before the fresh stop, the heartbeat `tiers` column, and round 24's identity columns. `go build` and `go vet` of `./store/...` were clean. TLC was not run.

## Defects

### 1. `checkRebuild` does not compare the stop

`store/restore.go:216` (the part list ends at `store/restore.go:219`).

`checkRebuild` deep-equals seq, epoch, policy, streams, cards, rows, judgements, cursors, and identity. `Snapshot.Stopped` and `Snapshot.StopReason` are absent from that list. `Rebuild` (`store/store.go:348`) folds from `freshSnapshot`, which starts at `FreshStop`, and `Fold` (`store/store.go:377`) keeps the stop across a clear and applies each event's stop. A direct `UPDATE sprint.epoch SET stopped, stop_reason` therefore leaves the log and the row apart while `ValidateHistory` (`store/history.go:57`) and `RestoreEnd` still return nil. `store/history_test.go:37` mutates a dropped event, an orphan receipt, the epoch number, an extra policy, and a cursor past the end. None of those mutations touch the stop columns.

The stop is the bit `Running` reads (`store/apply.go:265`). A fresh sprint is stopped so a tick cannot land before `nova-sprint start`. An integrity check that cannot see that bit will accept a store whose fence has been turned off, or turned on, beside the log.

Repair: add `Stopped` and `StopReason` to the compared parts, and add a history-refusal case that updates those two columns.

Severity: serious.

### 2. Four functional assertions describe an older schema

`store/functional_test.go:126`, `store/functional_test.go:161`, `store/functional_test.go:192`, `store/restore_test.go:140`.

`TestFunctionalMigrateTwice` compares the snapshot after `Migrate` to `emptySnapshot(1)` (`store/store.go:344`), whose `Stopped` is false and whose `StopReason` is empty. `freshStop` (`store/pg.go:244`) writes `FreshStop` when meta did not exist. The actual snapshot is `Stopped: true`, `StopReason: "never started"`.

`TestFunctionalMigrateRound16` and `TestFunctionalMigrateRound21` count `sprint.rows` columns named `kind`, `width`, `tiers`, `model`, or `harness` and expect 0. `store/schema.sql:86` drops those five, then `store/schema.sql:103` adds `tiers text` again as the heartbeat list. The count is 1. The other four names are gone.

`TestFunctionalRestore` expects `COPY sprint.epoch (id, n, stopped, stop_reason)` and the row `1`, epoch 2, stopped false, empty reason. The dump's column list is `id, n, stopped, stop_reason, commit_name, commit_email` and the row is `1`, epoch 2, stopped true, reason `never started`, empty name, empty email. The meta hash assertion on the line above passed.

These four failures are the tests asserting the pre-stop, pre-heartbeat, pre-identity shape. The product matches the later Built sections (fresh stop, heartbeat `tiers`, commit identity).

Repair: expect `FreshStop` from `emptySnapshot`'s successor, count the four dropped configuration names and leave the heartbeat `tiers` column out of that count, and match the epoch `COPY` that includes `commit_name` and `commit_email` with the stopped fresh row.

Severity: minor.

### 3. `cards.model` is not frozen at add

`store/schema.sql:174` and `store/schema.sql:190`.

The column comment says the consumer's model is fixed at add, like holder. `cards_fixed` refuses an `UPDATE` that changes id, epoch, kind, of, stream, holder, tier, a read's review, or a work's finding. `model` is not in that guard. `putCard` (`store/pgread.go:294`) sets `model` on insert and omits it from the `ON CONFLICT` update, so the Go path does not rewrite it, and there is no `SetCardModel`. `TestFunctionalSchemaEnforces` (`store/functional_test.go:815`) asserts the holder and tier refusals and has no `UPDATE` of `cards.model`.

A SQL `UPDATE` of `cards.model` succeeds. `checkRebuild` compares whole cards, so the naked update fails the history check until a later logged write of that card. That write reads the row (`store/writes.go:28`) and records the new model on the event's after-image (`store/apply.go:358`), and the history check then passes with the model change attributed to a write that did not mean to change it.

Repair: add `model` to the `cards_fixed` update guard, and assert that refusal next to the holder cases.

Severity: minor.

## Promise gaps

Guarantee 5 (`docs/LAYER-1-STORE.md:378`): "The log matches the state: folding every accepted event from the first in seq order rebuilds the snapshot, policy included." The 2026-11-07 stop entry (`docs/LAYER-1-STORE.md:825`) says `check --integrity` or a backup's verification compares the log and the stop row. `checkRebuild` (`store/restore.go:216`) compares the parts listed in defect 1 and skips `Stopped` and `StopReason`. `Fold` itself does keep the stop (`store/store.go:377`).

`docs/ENFORCED.md` L1-G6: "Every operation checks the epoch; `Clear` advances the epoch by one, carries rows forward as `down` with `gen + 1` and their `width`, `held`, `tiers`, `model`, and `harness` kept, leaves cards and judgements behind, and keeps cursors and policy versions." `runClear` (`store/apply.go:164`) sets state `down`, flaps 0, and gen + 1, then writes the rest of the row. Round 22 dropped kind, width, model, and harness. What is still carried is session, role, held, reason, the heartbeat columns, and the heartbeat `tiers` string. `Fold` also keeps cursors, the stop, identity, and the active policy. The round-22 entry (`docs/LAYER-1-STORE.md:642`) says it supersedes the sections above where they disagree.

`docs/ENFORCED.md` L1-R6: caller bugs include "empty `ID`/`Actor`/`Kind`", "text `> 64 KiB`", "`Writes > 500` or `Expect > 500`", and "duplicate tier in `PutRow`/`SetRowTiers`". `check` (`store/check.go:41`) refuses an empty id, a zero `At`, and a bad epoch. `name` (`store/check.go:116`) allows an empty actor and an empty kind. `text` (`store/check.go:108`) has no length cap. There is no cap on how many writes or expects an op carries. `SetRowTiers` is gone. The layer doc (`docs/LAYER-1-STORE.md:308`) says brief, reason, body, head, branch, and note are unbounded text.

`docs/ENFORCED.md` L1-S8: "`Events(afterSeq, limit)` returns events in `seq` order up to `MaxEvents` (1000) and rejects negative `afterSeq` or `limit > MaxEvents` with `ErrInvalid`." `capEvents` (`store/fake.go:152`) turns `max <= 0` and `max > MaxEvents` into `MaxEvents`. `readEvents` (`store/pgread.go:190`) uses that cap. A negative `afterSeq` is the lower bound of `seq > $1`. The interface comment (`store/store.go:31`) and the layer doc (`docs/LAYER-1-STORE.md:196`) describe the cap.

`docs/ENFORCED.md` L1-R4: "`Needs` or `After` naming the card itself or an absent card or a consumer" is refused. `checkList` (`store/writes.go:184`) refuses a consumer that has a list, a self-name, a duplicate, and an absent card. It does not read the named card's kind. The layer doc (`docs/LAYER-1-STORE.md:221`) and `WriteRule` (`tla/Store.tla:278`) require a different card that exists.

`docs/ENFORCED.md` L1-P2: "One backup and restore verb, tested on a populated sprint (`LESSONS 1.16`)", held by `nothing`. `RestoreBegin` and `RestoreEnd` are in `store/restore.go`. `Store` has no `Backup` method. The layer doc (`docs/LAYER-1-STORE.md:364` and `:431`) still owes the backup verb to layer 6.

`docs/ENFORCED.md` L1-S7: judgements "close (`answered` or `expired`) at most once in an epoch with non-empty `answer` and `answered_by`". `closeJudgement` (`store/writes.go:285`) stores the answer it is given, including an empty one, and stores `actor`. `sprint.judgements` (`store/schema.sql:317`) has `answer text NOT NULL DEFAULT ''` and no `answered_by` column. `judgements_closed_check` (`store/schema.sql:332`) ties `open` to a null `closed_at`. The layer doc does not require a non-empty answer.

`docs/ENFORCED.md` L1-S3: "a row has no kind, width, tiers, model or harness (the roster's)." Kind, width, model, and harness are dropped (`store/schema.sql:86`). `tiers text` is added again for the heartbeat (`store/schema.sql:98`). The round-23 Built section names that column.

`docs/ENFORCED.md` L1-R8: the stop "refuses no op (people's ops still land; Run calls no Tick while stopped)." `Running` (`store/apply.go:265`) refuses while `stopped` is true, with the reason on the row. An op that does not carry `Running` still lands. The Built section (`docs/LAYER-1-STORE.md:830`) and `Store.tla`'s `Running` rule match that split.

The early layer-doc sentences that a working count never exceeds a row's width (`docs/LAYER-1-STORE.md:227`) are the text the round-22 entry (`docs/LAYER-1-STORE.md:642`) says it supersedes. The code follows that entry: `checkHolder` does not look at width or tier.

No store promise in the layer doc uses the phrase "within one tick". The one-second tick bound is the load-test bar (`docs/LAYER-1-STORE.md:419`), and `TestLoad` (`store/load_test.go:52`) skips unless `NOVA_LOAD=1`. This run did not measure it.

## Model vs code

On the rules `Store.tla` states for the operations it projects, the code agrees: fresh `stopped` is TRUE, a `Running` condition fails while stopped, an absent cursor is 0 so setting it to 0 is refused, a clear keeps `stopped` and the row's held, session, and role, and the store refuses nothing for width. The header of `tla/Store.tla` and `store/trace.go:16` name what the projection leaves out (streams, heartbeat, identity, stop reason, most card columns).

`tracePutCard` (`store/trace.go:243`) refuses `After`, a non-zero `Attempt`, `Review`, `Finding`, and a non-normal `Priority`. It accepts a `Brief` on a sentinel and a `Model` on a producer or sentinel. The store refuses both (`store/writes.go:212` and `store/writes.go:228`). The walks do not send those fields, so a generated trace stays inside the abstraction. That is a hole in the refinement map. TLC was not run, and this review claims no trace match.

The stop-audit hole in defect 1 sits outside the model. `Store.tla` has one `stopped` variable. It has no separate epoch-row value to drift from the log.

## Simplifications

No special case in this layer is safe to delete. The round-hash upgrades and the fresh-stop `UPDATE` after `CREATE` (`store/pg.go:239`, column default left false so the schema hash stays stable) are the migration path the functional tests exercise.

## Missing tests

Nothing fails `ValidateHistory` when `sprint.epoch.stopped` or `stop_reason` disagrees with the fold. `store/history_test.go:37` covers five other mutations.

Nothing asserts that `UPDATE sprint.cards SET model` is refused. Holder and tier are asserted at `store/functional_test.go:815`.

The four red tests in defect 2 assert the wrong golden. They are recorded above and were left red.

`TestLoad` (`store/load_test.go:52`) skipped: `NOVA_LOAD` was unset. The L1-P1 bars were not remeasured.

Functional tests start a throwaway PostgreSQL: `net.Listen` on `127.0.0.1:0` and `initdb` (`store/functional_test.go:31`), and a `time.Now` deadline loop (`store/functional_test.go:652`). `store/schema_test.go:173` sleeps 50 ms and 100 ms waiting for a stats flush. `store/load_test.go` uses `time.Now` and the same server, and it skipped. Trace walks (`store/trace_test.go:50`) skip when `NOVA_TRACE_DIR` is unset. Unit tests against `Fake` stay in process.

## Questions for the coordinator

Should `docs/ENFORCED.md` be refreshed to round 24, so the ledger matches the later layer doc on the stop comparison, the 64 KiB and 500 caps, what `Clear` carries, `Events` capping instead of `ErrInvalid`, L1-P2, a need that names a consumer, the heartbeat `tiers` column, and an empty judgement answer?

Is leaving `cards.model` out of `cards_fixed` accepted because the contract is the Go path only?

Should the four red functional goldens be updated to the fresh stop, the heartbeat `tiers` column, and the identity columns, as test fixes, with the product left as it is?

Commands, on hulk, in `/home/gaffer/johnny-jobs/v2r-l1-pro1.w1`, with `PATH` prefixed by `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`, `GOCACHE` inside the clone, and `GOFLAGS=-mod=readonly`:

```
nice -n 19 go build ./store/...
nice -n 19 go vet ./store/...
nice -n 19 go test -p 2 -count=1 -timeout 600s ./store/...
```

```
GOCACHE=/home/gaffer/johnny-jobs/v2r-l1-pro1.w1/.gocache
GOFLAGS=-mod=readonly
BUILD_EXIT=0
VET_EXIT=0
--- FAIL: TestFunctionalMigrateRound16 (5.12s)
    functional_test.go:161: expected 0, actual 1
    Messages: round 22's columns are gone from a round-16 store too
--- FAIL: TestFunctionalMigrateTwice (6.19s)
    functional_test.go:126: expected Stopped false, StopReason ""
    actual Stopped true, StopReason "never started"
--- FAIL: TestFunctionalMigrateRound21 (7.72s)
    functional_test.go:192: expected 0, actual 1
    Messages: the five configuration columns are dropped
--- FAIL: TestFunctionalRestore (9.44s)
    restore_test.go:140: dump has
    COPY sprint.epoch (id, n, stopped, stop_reason, commit_name, commit_email)
    and row 1, epoch 2, stopped true, reason "never started"
    assertion wanted COPY sprint.epoch (id, n, stopped, stop_reason)
    and row 1, epoch 2, stopped false, empty reason
FAIL
FAIL github.com/mas-bandwidth/nova-sprint/store 25.709s
FAIL
```

`pg_dump` printed circular foreign-key warnings on `sprint.cards` during the restore test. The test comment already treats that shape as expected. Those lines are warnings, not a fifth failure.
