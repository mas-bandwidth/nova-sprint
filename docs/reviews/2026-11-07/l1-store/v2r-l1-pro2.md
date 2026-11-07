## Verdict

The apply protocol, the schema CHECKs, and the round-22 through round-24 rules (no width in the store, the hook, a fresh store stopped with reason "never started", a heartbeat that does not bump gen, commit identity, restore unhook) match the current layer promises and the state `tla/Store.tla` actually holds. The single worst thing is that every `Migrate` drops and recreates `sprint.rows.tiers`, so a second migrate, and an upgrade of a round-23 store, wipe the advertised tiers the schema file calls an idempotent no-op. The history check also omits the sprint stop. The suite is red: four functional tests still expect the pre-heartbeat column set and a running empty store. TLC was not run. This review does not claim a trace match.

## Defects

### 1. Every Migrate wipes `sprint.rows.tiers` — blocker

`store/schema.sql:86-87` drops `tiers` on every apply, and `store/schema.sql:107` adds it back as `tiers text NOT NULL DEFAULT ''`. `store/pg.go:220-226` executes that SQL on every `Migrate`, including when `sprint.meta` already holds this binary's hash: `metaRefusal` (`store/schema.go:74-81`) accepts `schemaHash` and there is no early return. `store/schema.sql:2` says applying the file twice is a no-op, and `store/schema.sql:85` says `DROP COLUMN IF EXISTS` is a no-op on a second run. That is true for `kind`, `width`, `model`, and `harness`, which are not added back. The name `tiers` was reused in round 23 as the heartbeat text column: `store/testdata/schema-round23.sql:101` already has `ADD COLUMN IF NOT EXISTS tiers text NOT NULL DEFAULT ''`. A second `Migrate` of a current store, and `Migrate` of a round-23 store that already stored a value, drop the column and add it empty. `load1_centi`, `cores`, `slots_used`, and `slots_total` survive, because they are only `ADD COLUMN IF NOT EXISTS` (`store/schema.sql:103-106`). Advertised tiers are what the roster deals from (`docs/LAYER-1-STORE.md:873-882`).

A routine migrate erases every worker's advertisement, and the deal then sees a worker that never advertised.

Repair: drop `tiers` only while it is still the old configuration array, and leave the heartbeat text column in place, so a written value survives a second `Migrate`.

### 2. `checkRebuild` omits the sprint stop — serious

`store/restore.go:216-219` compares `seq`, `epoch`, `policy`, `streams`, `cards`, `rows`, `judgements`, `cursors`, and `identity`. `Stopped` and `StopReason` are absent from that list. `store/history.go:57` and `store/restore.go:136` treat the comparison as `Rebuild(events) == Snapshot`. `store/store.go:349-350` states Guarantee 5 as `Rebuild(every event) == Snapshot()`, and `Fold` carries the stop (`store/store.go:377-380`) from `freshSnapshot`, which starts stopped with `FreshStop`'s reason (`store/store.go:353-357`). `docs/LAYER-1-STORE.md:822-825` tells the operator to stop or start a pre-change store before `check --integrity` because that check compares the log and the row. An `UPDATE` of `sprint.epoch.stopped` that leaves the log untouched still passes `ValidateHistory`. `tla/Store.tla:601-602` (`LogMatchesState`) requires the folded state, which includes `stopped` (`tla/Store.tla:441`, and `Empty.stopped` at `tla/Store.tla:241`), to equal `Current`, and `ClearCarriesRows` requires `a.stopped = b.stopped` (`tla/Store.tla:672`). `store/history_test.go:37-43` mutates a dropped event, an orphan receipt, the epoch number, a policy, and a cursor, and has no mutation of the stop.

A stop that disagrees with the log is invisible to the integrity check the layer doc says to run before trusting a backup.

Repair: compare `Stopped` and `StopReason` in `checkRebuild`, and add the same one-mutation history case the other five checks already have.

## Promise gaps

The two defects above are also broken sentences in the layer's own text: `store/schema.sql:2` and `store/pg.go:193` ("Twice is a no-op"), and `docs/LAYER-1-STORE.md:822-825` ("before `check --integrity` or a backup's verification compares them"). `docs/ENFORCED.md` L1-S1 (line 32) and L1-S15 (line 46) record those same sentences as held.

Three further ledger rows disagree with `docs/LAYER-1-STORE.md` and with the code. The layer doc is the later text; the code follows it.

- L1-R6 (`docs/ENFORCED.md:52`) says text over 64 KiB, and `Writes` or `Expect` over 500, are `ErrInvalid`. `store/check.go:108-113` accepts any length of UTF-8 with no NUL, and `store/check.go:41-76` does not count writes or conditions. `docs/LAYER-1-STORE.md:306-309` says ids and names are at most 128 bytes and that brief, reason, body, head, branch, and note are unbounded.
- L1-S8 (`docs/ENFORCED.md:39`) says `Events` rejects a negative `afterSeq` or a `limit` over `MaxEvents` with `ErrInvalid`. `store/store.go:31-32` and `docs/LAYER-1-STORE.md:196` say `max` is capped at 1000 and `max <= 0` means `MaxEvents`. `store/fake.go:152-156` and `store/pgread.go:190` cap. A negative `afterSeq` is `seq > $1`, which returns events from the start. `store/store_test.go:104-107` expects the cap.
- L1-S7 (`docs/ENFORCED.md:37`) says a close needs a non-empty answer and `answered_by`. `store/writes.go:297` stores the answer and actor it is given. `store/ops.go:463-465` lets an empty `Actor` fall back to `op.Actor`, and `store/check.go:116-120` allows that actor to be empty. `Answer` is checked as text, with no non-empty requirement. `store/schema.sql:326-332` defaults `answer` and `actor` to `''`. `docs/LAYER-1-STORE.md:134-138` refuses a close when the judgement is not open, and when `At` is before `opened_at`.

L1-S3 (`docs/ENFORCED.md:33`) still says a row has no kind, width, tiers, model, or harness. Round 22 dropped those configuration columns (`docs/LAYER-1-STORE.md:642-649`). Round 23 reused the name `tiers` as heartbeat text (`docs/LAYER-1-STORE.md:873-883`). L1-G6 (`docs/ENFORCED.md:25`) still says a clear carries width, tiers, model, and harness. The round-22 entry says that where it disagrees with the sections above, it stands (`docs/LAYER-1-STORE.md:642-647`). `store/apply.go:164-169` carries the row it read, with state `down`, flaps 0, and gen + 1.

The width and tier sentences earlier in the layer doc (`docs/LAYER-1-STORE.md:161`, `docs/LAYER-1-STORE.md:184`) are the text round 22 supersedes. The code matches the later entry: the store refuses nothing for width or for a row's tiers.

L1-P2 (`docs/ENFORCED.md:56`) records the backup and restore verb as held by nothing. `docs/LAYER-1-STORE.md:364-368` places that verb in layer 6. This package has `RestoreBegin` and `RestoreEnd`.

## Model vs code

`store/trace.go:25-59` states the projection. Streams, `AtSeq`, `SetActivePolicy`, most card columns, `SetRowLoad` (load, cores, slots, advertised tiers), flaps and reason, `SetIdentity`, and the stop's reason are outside `tla/Store.tla`. `Stopped` is projected (`store/trace.go:71`). The model's log is built from the steps; the code stores touched rows and `Fold` overlays them. TLC was not run, and this review does not claim a trace match.

The divergence that is also defect 2: `tla/Store.tla:601-602` includes `stopped` in the state that must match, and `store/restore.go:216-219` does not.

`tla/Store.tla:412` accepts a clear without `Holds`. `store/apply.go:147-150` runs the op's `Expect`, including `AtSeq` (`store/apply.go:270-273`), before the clear. `store/trace.go:33-34` says `AtSeq` is outside the model and `TraceOp` refuses it.

`tla/Store.tla:327` says a put on an existing row leaves its session out. `Do` at `tla/Store.tla:337-340` sets `sess` and `role`. The action matches the code on those two fields. Heartbeat columns stay outside the model (`store/trace.go:44-46`).

`tla/Store.tla:333` writes a consumer's tier as its producer's. `store/writes.go:270` refuses a consumer whose tier differs. `store/trace.go:41-43` records that, and says a walk sends the producer's tier.

`tla/Store.tla:312` refuses `setStopped` when the flag already matches. `store/writes.go:359-360` does the same, so a reason-only change while the flag matches is refused.

## Simplifications

`store/ops.go:10-14` still says the state write into `working` is the width check and that put-card checks the holder's tiers. `store/ops.go:154-161` still describes a tier refusal and a width cap on `SetCardState`. Round 22 removed both refusals (`docs/LAYER-1-STORE.md:653-656`). The comments describe checks the write does not do.

`store/schema.sql:172-176` says `cards.model` is "Fixed at add like holder." The `cards_fixed` trigger (`store/schema.sql:188-195`) names id, epoch, kind, of, stream, holder, tier, a read's review, and a work's finding. `model` is not in that list. `cards_model_check` (`store/schema.sql:178`) only requires a producer or sentinel to have an empty model.

## Missing tests

Commands, on hulk, in `/home/gaffer/johnny-jobs/v2r-l1-pro2.w1`, `nice -n 19`, `GOFLAGS=-mod=readonly`, `GOCACHE` inside the clone, `PATH` prefixed with `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`. `go version` was `go1.26.10 linux/amd64`. `go test ./...` of the whole module was not run.

```
export PATH=/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin:$PATH
export GOCACHE=/home/gaffer/johnny-jobs/v2r-l1-pro2.w1/.gocache
export GOFLAGS=-mod=readonly
cd /home/gaffer/johnny-jobs/v2r-l1-pro2.w1
nice -n 19 go build ./store/...
nice -n 19 go vet ./store/...
nice -n 19 go test -p 2 -count=1 -timeout 600s ./store/...
```

```
BUILD_EXIT:0
VET_EXIT:0
--- FAIL: TestFunctionalMigrateRound21 (3.82s)
    functional_test.go:192:
        expected: 0
        actual  : 1
        Messages: the five configuration columns are dropped
--- FAIL: TestFunctionalMigrateRound16 (4.77s)
    functional_test.go:161:
        expected: 0
        actual  : 1
        Messages: round 22's columns are gone from a round-16 store too
--- FAIL: TestFunctionalRestore (5.14s)
    restore_test.go:140:
        assert.Contains expected the dump to hold
        COPY sprint.epoch (id, n, stopped, stop_reason) and the row 1, 2, f, empty reason.
        The dump's epoch COPY lists commit_name and commit_email as well,
        and the row is 1, 2, t, "never started".
        The full dump is omitted here: pg_dump wrapped it in a \restrict token.
--- FAIL: TestFunctionalMigrateTwice (5.28s)
    functional_test.go:126:
        expected Stopped:false StopReason:""
        actual   Stopped:true  StopReason:"never started"
pg_dump: warning: there are circular foreign-key constraints on this table:
pg_dump: detail: cards
FAIL
FAIL	github.com/mas-bandwidth/nova-sprint/store	33.742s
FAIL
TEST_EXIT:1
```

The four failures are stale oracles, recorded and left red. `TestFunctionalMigrateRound21` (`store/functional_test.go:191-192`) and `TestFunctionalMigrateRound16` (`store/functional_test.go:160-161`) count columns named `kind`, `width`, `tiers`, `model`, or `harness` and expect 0. The count is 1 because the heartbeat column `tiers` exists. Neither test writes a tiers value and reads it back, so neither would see defect 1. `TestFunctionalMigrateTwice` (`store/functional_test.go:126`) expects `emptySnapshot` (`store/store.go:344-345`), which leaves `Stopped` false. The store `Migrate` just made is `FreshStop` (`store/store.go:353-357`), which `docs/LAYER-1-STORE.md:810-819` requires. `TestFunctionalRestore` (`store/restore_test.go:140`) fails on that `Contains` before the restore, unhook, and snapshot comparison at `store/restore_test.go:151-179` run.

Nothing in the suite writes a non-empty `rows.tiers` and then runs `Migrate` again, or upgrades a round-23 store, and asserts the value is still there. `TestFunctionalMigrateRound23` (`store/functional_test.go:235-259`) checks the stop and the identity only.

Nothing in `store/history_test.go:37-43` flips `stopped` or `stop_reason` and expects `ValidateHistory` or `RestoreEnd` to refuse.

`TestFunctionalSchemaEnforces` (`store/functional_test.go:825-827`) looks for the substrings `"tiers"`, `"model"`, and `"harness"` in the error text (`store/functional_test.go:912`). A missing column (`column "model" does not exist`) satisfies the same check as a NOT NULL violation. `model` and `harness` are dropped at `store/schema.sql:86-87` and not re-added. `tiers` is re-added NOT NULL at `store/schema.sql:107`. The test is green and does not show which of those happened.

Real time, sockets, and a real server: `startPG` (`store/functional_test.go:31`, `:58`, `:66`, `:78`) listens on `127.0.0.1`, runs `initdb`, and starts PostgreSQL with `fsync=off`. `pgHarness` and `startPG` are what the `TestFunctional*` tests in `store/functional_test.go`, `store/restore_test.go`, `store/history_test.go`, `store/schema_test.go`, `store/round19_test.go`, `store/coldread4_test.go`, `store/coldread5_test.go`, and `store/coldread7_test.go` use, and the PostgreSQL walk in `store/trace_test.go:73`. `store/load_test.go:77-102` starts its own PostgreSQL the same way, stamps `time.Now` into ops (`store/load_test.go:198`), and measures wall clock (`store/load_test.go:204` and the later `time.Now` lines in that file). `TestFunctionalEventsTailNoGap` (`store/functional_test.go:652`) polls until a 30s wall-clock deadline (`store/functional_test.go:27`). `store/schema_test.go:173-186` uses `time.Now` and `time.Sleep` (50ms, then 100ms) while waiting for writer statistics. The Fake conformance tests pass time in `Op.At` and open no socket.

## Questions for the coordinator

L1-R6 (64 KiB text, and more than 500 writes or expects), L1-S8 (`Events` returns `ErrInvalid` for a negative `afterSeq` or a limit over `MaxEvents`), and L1-S7 (a close needs a non-empty answer and actor) disagree with `docs/LAYER-1-STORE.md` and with the code the tests already lock. Should those three ledger rows be struck, or should the code be tightened to the ledger?
