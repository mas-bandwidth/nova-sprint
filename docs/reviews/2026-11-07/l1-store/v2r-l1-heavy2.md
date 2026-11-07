# Layer 1 store, cold review v2r-l1-heavy2

Pin `43d849e29d0e4568ba6a1348a076a59259639938`, branch `sprint/v2-review/v2r-l1-heavy2`. Read `docs/LAYER-1-STORE.md`, `docs/ENFORCED.md`, `store/`, and `tla/Store.tla` with `tla/StoreTrace.tla`. TLC was not run. Round 22 (`docs/LAYER-1-STORE.md:642-647`) stands where it disagrees with the earlier width, tier, and row-kind text; those earlier sentences are not treated as current code defects.

## Verdict

The layer is not sound. The apply path itself holds: one transaction under the epoch lock, four outcomes, a gen raised once per row-writing op, a heartbeat that moves no gen, hook and unhook, a clear that carries row state and keeps the stop and the identity, a fresh store stopped, cursor 0 refused, and no store-side width or tier refusal. The single worst thing is that every `Migrate`, including a second `Migrate` of a store already at this schema, drops `sprint.rows.tiers` and adds the column back empty (`store/schema.sql:86`, `store/schema.sql:107`, run again from `store/pg.go:224`), so every worker's advertised tiers are wiped. `go test ./store/...` is red for four stale expectations; those failures are recorded below and were not fixed.

## Defects

### 1. A second Migrate wipes advertised tiers

`store/schema.sql:86` through `store/schema.sql:87` and `store/schema.sql:107`. Severity: blocker.

`Migrate` accepts the current schema hash (`store/schema.go:74-81` returns nil when `hash == schemaHash`) and then executes `schema.sql` again (`store/pg.go:220-224`). The file drops `tiers` if the column exists and later adds `tiers text NOT NULL DEFAULT ''`. On a store this binary already migrated, the drop removes the heartbeat column round 23 created, and the add creates an empty one. `load1_centi`, `cores`, `slots_used`, and `slots_total` survive (`ADD COLUMN IF NOT EXISTS` only). The comment at `store/schema.sql:85` says that drop is a no-op on a second run, and `store/pg.go:193` says twice is a no-op; both are false once `tiers` is a column the current schema owns. `docs/LAYER-1-STORE.md:875-882` says the roster deals a worker exactly these advertised tiers, and `docs/LAYER-1-STORE.md:879-880` says `PutRow` clears them when the session changes, so a down worker does not write them back. The log's after-images still hold the old list, so `Snapshot` and `Rebuild` disagree until the next `SetRowLoad`. The suite never wrote tiers and migrated again, so this was read off the statements; `TestFunctionalMigrateTwice` only shows that the second `Migrate` of an empty store returns success.

Repair: drop the old `text[]` configuration column only on a known older hash, and never drop the `text` column the current schema owns.

### 2. The history check ignores the stop

`store/restore.go:212-219`. Severity: serious.

`store/store.go:349-350` states guarantee 5 as `Rebuild(every event) == Snapshot()`. `Snapshot` carries `Stopped` and `StopReason` (`store/store.go:339-340`), and `Fold` lays a stop over and keeps it across a clear (`store/store.go:377-380`). `checkRebuild` compares seq, epoch, policy, streams, cards, rows, judgements, cursors, and identity, and leaves the stop out. `ValidateHistory` ends on that check (`store/history.go:21`, `store/history.go:57`), and `RestoreEnd` runs `ValidateHistory`. A direct update of `sprint.epoch.stopped` or `stop_reason`, or a store made before the fresh stop (the row still reads running, the fold starts stopped), passes the check while the snapshot and the fold disagree. `docs/LAYER-1-STORE.md:822-825` tells the operator to run stop or start once so the check can compare them; this check does not compare them.

Repair: compare `Stopped` and `StopReason` in `checkRebuild`.

### 3. The round-21 unhook is not an event

`store/schema.sql:91-93`. Severity: serious.

On every `Migrate`, including the supported `round21Hash` upgrade (`store/schema.go:173-174`, `store/pg.go:188`), the file runs `UPDATE sprint.rows SET session = '' WHERE role = '' AND session <> ''`. The comment says a pre-round-22 hook is unhooked so it hooks again, and `store/functional_test.go:201` expects that snapshot. The fixture inserts the row with raw SQL and no events (`store/functional_test.go:177-181`), so it never folds the log. On a logged round-21 store the last `PutRow` after-image still has the session. After the update, `Snapshot` reads an empty session and `Rebuild` restores the logged one, until some later op rewrites that row. A current store whose hooks are well formed (session and role both set or both empty) updates zero rows; the break is the upgrade path.

Repair: record the unhook as an accepted event in the same transaction, the way `unhookRestored` does (`store/restore.go:170-177`).

## Promise gaps

`docs/LAYER-1-STORE.md:25`: "The schema file is embedded in the binary and applied with `CREATE ... IF NOT EXISTS`, so applying it twice is a no-op." `docs/LAYER-1-STORE.md:517`: "A second `Migrate` is a no-op." The second execution drops and recreates `sprint.rows.tiers` (defect 1). `CREATE TABLE IF NOT EXISTS` is a no-op; the file is not only that.

`docs/LAYER-1-STORE.md:378-380`: "The log matches the state: folding every accepted event from the first in seq order rebuilds the snapshot, policy included." `checkRebuild` omits the stop (defect 2), and the round-21 session update is outside the log (defect 3).

`docs/ENFORCED.md` L1-R4: "Missing target or bad dependency at the store: a set write on an absent card/row/judgement/policy/stream, `PutCard` or `PutJudgement` on an existing ID, `Needs` or `After` naming the card itself or an absent card or a consumer, or a consumer `Needs`/`After` non-empty is refused." `checkList` (`store/writes.go:181-200`) refuses a list on a consumer, a self-name, a duplicate, and an absent card. It does not read the target's kind, so a producer may name a consumer. The standing layer text (round 22, and the needs rule that a cycle is layer 2's) does not require that refusal.

`docs/ENFORCED.md` L1-R6: "Caller bugs (`ErrInvalid`): empty `ID`/`Actor`/`Kind`, zero `At`, `Epoch < 1`, identifier `> 128` bytes or text `> 64 KiB`, NUL byte or invalid UTF-8, `Writes > 500` or `Expect > 500`, ..." `check` (`store/check.go:41-76`) refuses an empty id (via `checkID`), epoch below 1, a zero `At`, a name over 128 bytes, NUL, and invalid UTF-8. `text` (`store/check.go:108-113`) allows any length. `name` allows `""` (`store/check.go:116-119`), so an empty actor or kind passes. Nothing counts `Writes` or `Expect`. The layer doc matches the code: `docs/LAYER-1-STORE.md:306-309` caps ids and names at 128 bytes and says brief, reason, body, head, branch, and note are unbounded.

`docs/ENFORCED.md` L1-G6: "`Clear` advances the epoch by one, carries rows forward as `down` with `gen + 1` and their `width`, `held`, `tiers`, `model`, and `harness` kept, leaves cards and judgements behind, and keeps cursors and policy versions." `runClear` (`store/apply.go:164-175`) sets state `down`, flaps 0, and gen + 1, and copies the rest of the row, which is session, role, held, reason, and the heartbeat columns. Width, model, and harness are gone (round 22). A clear keeps the stop and the identity because they live on the epoch row and `open` does not clear them (`docs/LAYER-1-STORE.md:887-893`). The code follows that later entry.

`docs/ENFORCED.md` L1-S7: "Judgements start `open` and close (`answered` or `expired`) at most once in an epoch with non-empty `answer` and `answered_by` at `closed_at >= opened_at`." `closeJudgement` (`store/writes.go:285-298`) copies the answer and the actor it was given, including empty. `judgements_closed_check` (`store/schema.sql:332`) requires `closed_at` null exactly when the state is open, and `judgements_order_check` (`store/schema.sql:333`) requires `closed_at >= opened_at`. The layer doc does not require a non-empty answer.

`docs/ENFORCED.md` L1-S4 names `created_at` among the fixed card columns. `CREATE TABLE sprint.cards` (`store/schema.sql:114`) has no `created_at` column, and the word does not appear in `store/schema.sql`.

`docs/ENFORCED.md` L1-S8: "`Events(afterSeq, limit)` returns events in `seq` order up to `MaxEvents` (1000) and rejects negative `afterSeq` or `limit > MaxEvents` with `ErrInvalid`." `store/store.go:32` and `store/store.go:51-52` cap `max` at 1000, and `max <= 0` means 1000. `capEvents` (`store/fake.go:152-154`) does that, and `PG.Events` uses it (`store/pgread.go:190`). `store/store_test.go:104-107` expects the cap. The layer doc says the max is capped.

`docs/ENFORCED.md` L1-P2: "One backup and restore verb, tested on a populated sprint" with evidence `nothing`. The store has `RestoreBegin` and `RestoreEnd` (`store/restore.go`) and no `Backup` method. The layer doc defers backup to layer 6. That ledger cell is stale in both directions; it is not a missing verb hiding in this package.

## Model vs code

`tla/Store.tla` matches the round-22 store on the variables it names: no width cap, hook session and role, stop kept by clear, a fresh store stopped, cursor 0 on an absent cursor refused, a same-value set refused, clear carrying session, role, and held. `Do` at `tla/Store.tla:337-340` sets `sess` and `role`. The comment three lines above (`tla/Store.tla:324-327`) still says a put on an existing row leaves the session out; the action does not, and the code does not. Heartbeat, advertised tiers, commit identity, streams, and the unread card columns are outside the model: the header (`tla/Store.tla:69-77`) names load and slots, and `docs/LAYER-1-STORE.md:882-883` and `docs/LAYER-1-STORE.md:901` leave tiers and `SetIdentity` out and say `TraceOp` refuses those writes. The Go checker omitting `Stopped` (defect 2) is the one place the model's stop is in the state and the store's equality check is not. Witnesses were not executed. This review does not claim a trace match.

## Simplifications

No extra code path was found that exists only as a special case. The width and tier refusals are gone (`store/writes.go:160`). What remains is the one-shot upgrade SQL (the `tiers` drop and the session update) still sitting in the file that every `Migrate` re-runs, which is defects 1 and 3, and the early sections of `docs/LAYER-1-STORE.md` that the round-22 entry already superseded.

## Missing tests

No test writes `Row.Tiers`, calls `Migrate` again, and reads the tiers back, or checks that `Rebuild` still matches. No test tampers with `sprint.epoch.stopped` and expects `ValidateHistory` to fail. `TestFunctionalMigrateRound21` (`store/functional_test.go:177-201`) inserts a hooked row with no events, so it cannot see the fold disagree.

Four tests are red because they still expect an older shape. They were not fixed.

- `TestFunctionalMigrateRound16` (`store/functional_test.go:161`): column count of `kind`, `width`, `tiers`, `model`, `harness` expected 0, actual 1. The one column is the heartbeat `tiers` text. Message: "round 22's columns are gone from a round-16 store too".
- `TestFunctionalMigrateRound21` (`store/functional_test.go:192`): the same count, expected 0, actual 1. Message: "the five configuration columns are dropped".
- `TestFunctionalMigrateTwice` (`store/functional_test.go:126`): expected `emptySnapshot(1)` (`Stopped` false, empty reason). Actual `Stopped` true, reason `never started`. The second `Migrate` itself returned success. The code matches `docs/LAYER-1-STORE.md:814-819`.
- `TestFunctionalRestore` (`store/restore_test.go:140`): the golden string is `COPY sprint.epoch (id, n, stopped, stop_reason)` and a row with `f` and an empty reason. The dump is `COPY sprint.epoch (id, n, stopped, stop_reason, commit_name, commit_email)` and `1, 2, t, never started`. Clear keeps the fresh stop. The assertion fails before the restore steps. `pg_dump` also warns about a circular foreign key on `sprint.cards`; that warning is noise.

Nothing in the suite asserts that a producer whose `Needs` names a consumer is refused, or that it is accepted.

`store/functional_test.go:31`, `store/functional_test.go:66`, and `store/functional_test.go:78` start a real PostgreSQL on `127.0.0.1` (a real TCP socket, unix sockets off, `fsync=off`). `store/functional_test.go:652` waits on `time.Now`. `store/restore_test.go` uses that harness. `store/load_test.go:65-102` starts a real PostgreSQL the same way, with `fsync` from `NOVA_LOAD_FSYNC` (default on), and the load times itself with `time.Now` (`store/load_test.go:198`, `store/load_test.go:204`). Unit tests against `Fake` pass time in `Op.At` and open no socket.

The run, on hulk, `nice -n 19`, `GOCACHE` inside the clone, `GOFLAGS=-mod=readonly`, `PATH` prefixed with `/home/gaffer/sdk/bin:/usr/lib/postgresql/18/bin`:

```
nice -n 19 go build ./store/...
BUILD_EXIT:0
nice -n 19 go vet ./store/...
VET_EXIT:0
nice -n 19 go test -p 2 -count=1 -timeout 600s ./store/...
--- FAIL: TestFunctionalMigrateRound21 (4.03s)
    functional_test.go:192: expected: 0, actual: 1
        Messages: the five configuration columns are dropped
--- FAIL: TestFunctionalMigrateRound16 (5.46s)
    functional_test.go:161: expected: 0, actual: 1
        Messages: round 22's columns are gone from a round-16 store too
--- FAIL: TestFunctionalMigrateTwice (8.42s)
    functional_test.go:126: expected Stopped:false StopReason:"", actual Stopped:true StopReason:"never started"
--- FAIL: TestFunctionalRestore (8.96s)
    restore_test.go:140: dump does not contain
        COPY sprint.epoch (id, n, stopped, stop_reason) FROM stdin;
        1	2	f
        actual COPY list includes commit_name, commit_email and the row is
        1	2	t	never started
FAIL
FAIL	github.com/mas-bandwidth/nova-sprint/store	28.050s
FAIL
TEST_EXIT:1
```

## Questions for the coordinator

Should `docs/ENFORCED.md` L1-R4, L1-R6, L1-G6, L1-S4, L1-S7, and L1-S8 be rewritten to the round-22 and later layer text, which the code follows, or should the code be tightened to the ledger?

Is the `tiers` name collision (round 22's dropped configuration column, round 23's heartbeat column, same `DROP COLUMN IF EXISTS` on every `Migrate`) already known?

The round-21 unhook is specified. Does it have to be an event before this layer locks, so a logged upgrade still rebuilds?
