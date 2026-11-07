# Layer 1 (`store`) — Cold Review

Original reviewer: `emma`
Report correction and source countercheck: Stella (Codex), 2026-11-07
Base commit: `43d849e29d0e4568ba6a1348a076a59259639938`
Date: `2026-11-07`

Evidence scope: this correction checked source only; it ran no Go commands, PostgreSQL,
or TLC. The inherited report claimed live store-test failures, but no raw receipts
were available to verify them. D1 and D3 therefore describe source-predicted failures,
not reproduced results. The parent supplied common validation for the exact base above
on hetzner2 Linux (nova-reserve slot 2/8, GOMAXPROCS=4); the receipt at
`/Volumes/nova/ai/stella/working/deal-1794067989302/VALIDATION.txt` records:

```text
go build -p 4 ./... EXIT0; go vet -p 4 ./... EXIT0.
go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/... EXIT1.
```

This is parent-run evidence, not this reviewer's execution. The selected tests excluded
`store`; policy failed `TestDeadlineOnTheClock`, while the other selected packages passed.
No store unit or functional pass is claimed. The store functional tests start a temporary
PostgreSQL, bind a loopback TCP port, and wait in real time (`store/functional_test.go:29-35,
52-80`); the unfiltered package command is not a pure unit gate.

## Verdict

Layer 1 is not sound at the reviewed base: the worst defect is the repeated migration's unconditional drop and recreation of `sprint.rows.tiers` (`store/schema.sql:86-87,107`, executed by `store/pg.go:220-224`), which erases advertised tiers and breaks both migration idempotency and log/state agreement. The shared transaction protocol is a useful structural safeguard, but `checkRebuild` also omits the stop fields (`store/restore.go:209-229`). Existing test assertions contradict the current schema and fresh-store behavior; these failures are source-predicted, with store runtime validation still outstanding. The documented old-store stop reconciliation and explicitly Go-only consumer-attempt rule qualify D4 and D5 below.

## Defects

### 1. [blocker] `store/schema.sql:86-87` and `store/schema.sql:107` — `Migrate` drops and recreates `sprint.rows.tiers` on every run, wiping live row state and breaking `TestFunctionalMigrateRound16` / `TestFunctionalMigrateRound21`

- **Where:** `store/schema.sql:86-87`, `store/schema.sql:107`, `store/pg.go:224`, `store/functional_test.go:160-161`, `store/functional_test.go:191-192`
- **Defect:** `store/schema.sql:86-87` executes `ALTER TABLE sprint.rows DROP COLUMN IF EXISTS kind, DROP COLUMN IF EXISTS width, DROP COLUMN IF EXISTS tiers, DROP COLUMN IF EXISTS model, DROP COLUMN IF EXISTS harness;` (from Round 22, when `tiers` was `text[]`), and `store/schema.sql:107` then executes `ALTER TABLE sprint.rows ... ADD COLUMN IF NOT EXISTS tiers text NOT NULL DEFAULT ''` (from Round 23 / `w3W`, where `tiers` is `text`).
- **Why it matters:**
  1. Every time `Migrate` runs on an existing store (which `LAYER-1-STORE.md:25` and `store/pg.go:193` promise is a no-op when applied twice, and which `nova-sprint up` runs), line 86 drops `sprint.rows.tiers` and line 107 recreates it with default `''`, destroying all `Row.Tiers` values written by `SetRowLoad` (`store/writes.go:100-112`) and causing `Snapshot().Rows[r].Tiers` (`""`) to diverge from `Rebuild(events).Rows[r].Tiers` (violating Guarantee 5 and `ValidateHistory`).
  2. Source predicts that `TestFunctionalMigrateRound16` (`store/functional_test.go:161`) and `TestFunctionalMigrateRound21` (`store/functional_test.go:192`) would compare expected 0 with actual 1 because the queried configuration-column names include the newly reintroduced `tiers`; no execution of these tests is verified here.
- **Repair:** Guard the Round-22 drop of `tiers` in `store/schema.sql:86` so it only drops `tiers` when its catalog type is `text[]` (`data_type = 'ARRAY'`) — or rename the Round-23 heartbeat column in `sprint.rows` to `advertised_tiers` — and update the column-absence assertions in `store/functional_test.go:160,191`.

### 2. [blocker] `store/restore.go:209-229` — `checkRebuild` (used by `ValidateHistory` and `RestoreEnd`) omits `Stopped` and `StopReason`

- **Where:** `store/restore.go:209-229`, `store/history.go:57`
- **Defect:** `checkRebuild(snap Snapshot, events []Event)` checks nine fields of `Snapshot` (`seq`, `epoch`, `policy`, `streams`, `cards`, `rows`, `judgements`, `cursors`, `identity` at `store/restore.go:216-220`) but never compares `snap.Stopped` against `got.Stopped` or `snap.StopReason` against `got.StopReason`.
- **Why it matters:** Guarantee 5 (`docs/LAYER-1-STORE.md:378-380`, `store/store.go:350`) and Round 21's `ValidateHistory` (`docs/LAYER-1-STORE.md:622-640`, `docs/ENFORCED.md:46` `L1-S15`) promise that `ValidateHistory` and `RestoreEnd` verify `Rebuild(events) == Snapshot()`. Moreover, `docs/LAYER-1-STORE.md:820-829` explicitly relies on `check --integrity` and backup verification to catch a mismatch between `sprint.epoch.stopped` and the event log. Because `checkRebuild` omits `Stopped` and `StopReason`, a dump or live database whose `sprint.epoch.stopped` / `stop_reason` contradicts the event log passes `ValidateHistory` and `RestoreEnd` silently.
- **Repair:** Add `{"stopped", snap.Stopped, got.Stopped}` and `{"stop_reason", snap.StopReason, got.StopReason}` to the parts slice in `checkRebuild` (`store/restore.go:216-220`).

### 3. [serious] `store/functional_test.go:126` and `store/restore_test.go:140` — Two functional assertions are stale against the current schema and fresh-store state

- **Where:** `store/functional_test.go:126`, `store/restore_test.go:140`
- **Defect:**
  1. `TestFunctionalMigrateTwice` (`store/functional_test.go:126`) asserts `assert.Equal(t, emptySnapshot(1), s)`, expecting `Stopped: false, StopReason: ""`, whereas `Migrate` on a new store writes `FreshStop` (`Stopped: true, StopReason: "never started"`, `store/pg.go:244-252`, matching `freshSnapshot()`).
  2. `TestFunctionalRestore` (`store/restore_test.go:140`) asserts `assert.Contains(t, d, "COPY sprint.epoch (id, n, stopped, stop_reason) FROM stdin;\n1\t2\tf\t\n")`, which source inspection predicts will fail because Round 24 added `commit_name, commit_email` to `sprint.epoch` (`store/schema.sql:48-49`) and `FreshStop` initializes `stopped, stop_reason` to `t\tnever started` (`COPY sprint.epoch (id, n, stopped, stop_reason, commit_name, commit_email) FROM stdin;\n1\t2\tt\tnever started\t\t\n`).
- **Why it matters:** When the functional harness successfully starts PostgreSQL and reaches these assertions, source predicts failures; this blocks a clean functional gate (`store/functional_test.go:113-126`, `store/restore_test.go:127-140`), but no raw execution receipt is available for these two tests.
- **Repair:** Update `store/functional_test.go:126` to compare against `freshSnapshot()` and update `store/restore_test.go:140` to expect `COPY sprint.epoch (id, n, stopped, stop_reason, commit_name, commit_email) FROM stdin;\n1\t2\tt\tnever started\t\t\n`.

### 4. [minor] `docs/LAYER-1-STORE.md:818-825` and `store/restore.go:209-229` — The documented old-store stop reconciliation is not checked by integrity validation

- **Where:** `docs/LAYER-1-STORE.md:818-825`, `store/pg.go:239-252`, `store/store.go:348-359`, `store/restore.go:209-229`.
- **Defect:** An older store with no stop after-image can retain `Stopped: false, StopReason: ""`, while `Rebuild` begins stopped with reason `never started`. This is explicitly documented compatibility behavior: migration preserves an existing stop, and the operator must issue one stop/start before integrity or backup verification. It is not an accidental migration default. The remaining enforcement gap is that `checkRebuild` does not detect an omitted reconciliation (D2).
- **Why it matters:** The unconditional log/state guarantee (`docs/LAYER-1-STORE.md:378-380`) needs the migration precondition stated beside it, and the integrity check should expose an unmet precondition; silently changing an existing running store to `FreshStop` would alter the documented behavior.
- **Repair:** Keep the documented stop-preservation behavior, state the one-time reconciliation precondition alongside Guarantee 5, and implement D2 so the integrity check reports a stop mismatch with the documented reconciliation guidance.

### 5. [minor] `store/schema.sql:172-195` — The SQL wording does not distinguish model immutability in Go from SQL enforcement

- **Where:** `store/schema.sql:172-195`, `store/pgread.go:289-300`, `store/ops.go:502`, `docs/LAYER-1-STORE.md:343-345,443-444`.
- **Defect:** The schema describes `model` as "Fixed at add like holder", but `cards_fixed` omits it; the Go upsert preserves it by omitting `model` from its update columns. The general SQL-and-Go contract description at doc 443-444 needs a precise boundary. Consumer `attempt` is a separate, explicit exception: schema 185-187 assigns that rule to the store alone, and `SetCardAttempt` refuses consumers (`store/ops.go:502`); its missing SQL guard is not a violated SQL promise.
- **Why it matters:** A reader could mistake model's stated immutability for a SQL guarantee. The source shows no current store-API path changing model; the issue is the documented SQL boundary, not a reproduced API failure. Hook pairing (`store/check.go:166-175`), load bounds (`store/ops.go:570-579`), and identity writes (`store/ops.go:478`) likewise have Go checks without matching SQL cross-field constraints (`store/schema.sql:48-49,69-107`); adding those would be optional defense in depth, not established violations of specific SQL promises.
- **Repair:** Document model's Go-only immutability beside the existing consumer-attempt exception, or deliberately extend the SQL trigger and its tests to enforce model immutability while preserving the documented distinction for attempt.

### 6. [minor] `store/ops.go:460-470` and `store/writes.go:275-299` — Judgement closure accepts empty answer and actor, contradicting `docs/ENFORCED.md` `L1-S7`

- **Where:** `store/ops.go:460-470`, `store/writes.go:275-299`, `store/schema.sql:318-334`
- **Defect:** `PutJudgement` checks `c.name(v.Kind, v.About, v.Group)` (`store/ops.go:461`), which permits `""`, and `AnswerJudgement` / `ExpireJudgement` (`store/ops.go:464-470`) check `c.text(v.Answer)` / `c.text(v.Reason)` and `cmp.Or(v.Actor, w.op.Actor)` without requiring non-empty strings. In SQL, `judgements_closed_check` (`store/schema.sql:332`) only checks `(state = 'open') = (closed_at IS NULL)`.
- **Why it matters:** `docs/ENFORCED.md:38` (`L1-S7`) promises: `"Judgements start open and close (answered or expired) at most once in an epoch with non-empty answer and answered_by at closed_at >= opened_at; an open judgement has empty answer/answered_by and null closed_at"`. A judgement can close with empty answer and effective actor when the op actor is also empty (`store/check.go:51`). Empty `Kind` is accepted too, but the quoted promise does not require kind to be non-empty; that is not counted as a separate defect.
- **Repair:** Reconcile L1-S7 by requiring non-empty closing `Answer`/`Reason` and effective actor in the Go and SQL closure checks, or explicitly revise the ledger if empty closure text is intended.

### 7. [minor] `store/check.go:50-76` and `store/pgread.go:186-191` — `check` and `Events` do not enforce the `ErrInvalid` bounds claimed in `docs/ENFORCED.md` (`L1-S8`, `L1-R6`)

- **Where:** `store/check.go:50-76`, `store/pgread.go:186-191`, `store/fake.go:117-125`, `docs/ENFORCED.md:39,52`
- **Defect:**
  1. `docs/ENFORCED.md:39` (`L1-S8`) states that `Events(afterSeq, limit)` `"rejects negative afterSeq or limit > MaxEvents with ErrInvalid"`, whereas `store/pgread.go:186-191` and `store/fake.go:117-125` accept negative `afterSeq` and silently clamp `max <= 0 || max > MaxEvents` to `MaxEvents` (`capEvents` at `store/fake.go:152-157`).
  2. `docs/ENFORCED.md:52` (`L1-R6`) states that empty `Actor`/`Kind`, text `> 64 KiB`, `Writes > 500`, or `Expect > 500` return `ErrInvalid`, whereas `store/check.go:51` calls `c.name(op.Actor, op.Kind)` (allowing `""`) and imposes no limit on text length, `len(op.Writes)`, or `len(op.Expect)`.
- **Why it matters:** Either callers rely on `ErrInvalid` guards that `store` does not implement, or `docs/ENFORCED.md` overclaims enforcement that is absent from code and tests.
- **Repair:** Either add the `c.given("the op's actor and kind", op.Actor, op.Kind)` / slice-length / `afterSeq >= 0` checks in `store/check.go` and `Events`, or align `docs/ENFORCED.md:39,52` with `docs/LAYER-1-STORE.md:200,306-310`.

### 8. [minor] `store/writes.go:50-58` — `spec.unchanged` marshals full `Card` and `Row` structs to JSON on every `set` write in the PG writer transaction

- **Where:** `store/writes.go:50-58`, `store/fake.go:309-318`
- **Defect:** `func (s spec) unchanged(was, now any) string` checks `if !bytes.Equal(encode(was), encode(now))` (`store/writes.go:50`), which calls `json.Marshal` twice on the entire `Card` or `Row` struct for card/row setters routed through `set` (`store/writes.go:27-42`); `SetRowLoad` is a counterexample that already compares row values directly (`store/writes.go:100-108`).
- **Why it matters:** PG Apply writes execute inside `LOCK TABLE sprint.epoch IN SHARE ROW EXCLUSIVE MODE` (`store/pg.go:331`). Serializing two full `Card` structs (including `Brief`, `Prose`, `Paths`, `DoneWhen`) to JSON per field-set write adds serialization work inside the serialized lock section; its performance impact has not been measured here.
- **Repair:** Replace `bytes.Equal(encode(was), encode(now))` in `store/writes.go:50` with `reflect.DeepEqual(was, now)` (since `normSet` and `normList` already normalize empty slices to `nil` on both `was` and `now`).

### 9. [minor] `store/fakefrom.go:19-54` — `NewFakeFrom` accepts snapshots with broken internal invariants without returning `ErrInvalid`

- **Where:** `store/fakefrom.go:19-54`
- **Defect:** `NewFakeFrom(s Snapshot, policies ...PolicyVersion)` checks only `s.Epoch >= 1`, `s.Seq >= 0`, map key == entry name/ID, and policy version numbers (`store/fakefrom.go:20-53`). It does not check whether `s.Cursors` has a cursor `> s.Seq` or `< 0`, whether `s.Identity` has only one of `Name`/`Email` set, or whether `s.Cards`, `s.Rows`, `s.Streams`, and `s.Judgements` satisfy the store's closed-set and foreign-key invariants.
- **Why it matters:** `docs/LAYER-1-STORE.md:560` promises `"An input that is not a snapshot is ErrInvalid"`. The narrower API comment (`store/fakefrom.go:15-18`) enumerates only structural checks, so the broader document promise overstates validation. A malformed hand-built snapshot can produce a Fake state not reachable through valid store operations; this is not evidence that a snapshot returned by PG fails.
- **Repair:** Narrow the document to the structural checks enumerated in `store/fakefrom.go:15-18` and require a valid store snapshot, or explicitly extend validation to the additional invariants before promising rejection of every inconsistent input.

### 10. [nit] `docs/LAYER-1-STORE.md`, `docs/ENFORCED.md`, and `store/ops.go` — Stale pre-Round-22 descriptions of row `width`/`tiers`/`kind`/`model`/`harness` in main prose and doc comments

- **Where:** `docs/LAYER-1-STORE.md:78-94,106-114,155-168,227-256,397-405`, `docs/ENFORCED.md:24`, `store/ops.go:13-15,127-128,155-160`
- **Defect:** Although Round 22 (`docs/LAYER-1-STORE.md:646-661`) notes that its addendum supersedes earlier sections, the main body of `docs/LAYER-1-STORE.md`, `docs/ENFORCED.md:24` (`L1-G6`), and the Go doc comments on `Expect` (`store/ops.go:13-15`), `PutCard` (`store/ops.go:127-128`), and `SetCardState` (`store/ops.go:155-160`) still state that the store checks row width and tier serving and that `sprint.rows` carries `kind`, `width`, `tiers`, `model`, and `harness`.
- **Why it matters:** A cold reader or caller inspecting `go doc store.SetCardState` or `go doc store.PutCard` is told that the store refuses a consumer when its holder row is at width or does not serve its tier, which is the exact opposite of Round 22's Guarantee 3 (`StoreWidthInStore`).
- **Repair:** Update the doc comments in `store/ops.go:13-15,127-128,155-160`, `docs/ENFORCED.md:24`, and the main sections of `docs/LAYER-1-STORE.md` to match the Round-24 store contract.

## Promise gaps

1. **`docs/LAYER-1-STORE.md:24-25`:**
   > "The schema file is embedded in the binary and applied with `CREATE ... IF NOT EXISTS`, so applying it twice is a no-op."
   - **What the code does:** `store/schema.sql:86` executes `ALTER TABLE sprint.rows ... DROP COLUMN IF EXISTS tiers ...` and `store/schema.sql:107` executes `ALTER TABLE sprint.rows ... ADD COLUMN IF NOT EXISTS tiers text NOT NULL DEFAULT ''`. Applying `schema.sql` a second time via `Migrate` (`store/pg.go:224`) drops and recreates `sprint.rows.tiers`, erasing all stored `Row.Tiers` values (`SetRowLoad`) and, by source inspection, contradicting the assertions in `TestFunctionalMigrateRound16` (`store/functional_test.go:161`) and `TestFunctionalMigrateRound21` (`store/functional_test.go:192`).

2. **`docs/LAYER-1-STORE.md:378-380` and `docs/LAYER-1-STORE.md:606-608, 622-636`:**
   > "5. The log matches the state: folding every accepted event from the first in seq order rebuilds the snapshot, policy included."
   > "End then requires `sprint.meta` at this binary's version and hash, the epoch row, and `checkRebuild`: Rebuild over every loaded event equals the snapshot read in the transaction, naming each part that differs."
   - **What the code does:** `checkRebuild` (`store/restore.go:209-229`) compares `seq`, `epoch`, `policy`, `streams`, `cards`, `rows`, `judgements`, `cursors`, and `identity`, but omits `Stopped` and `StopReason`. As intentionally documented at `docs/LAYER-1-STORE.md:818-825`, `Migrate` (`store/pg.go:244-252`) leaves `stopped = false, stop_reason = ""` when upgrading an existing pre-`FreshStop` store (`meta == true`), whereas `Rebuild` (`store/store.go:352-359`) always starts from `FreshStop{Stopped: true, Reason: "never started"}`; the documented one-time stop/start reconciles this, but D2 prevents integrity validation from detecting its omission.

3. **`docs/LAYER-1-STORE.md:885` (`Built: a worker's advertised tiers`):**
   > "`Row.Tiers`, a fifth heartbeat column (`tiers text`, in schema.sql's round-23 `ALTER TABLE sprint.rows` beside the other four, `ADD COLUMN IF NOT EXISTS`; no DDL outside the file, so the round-23 hash covers it)..."
   - **What the code does:** Reusing the column name `tiers` in `store/schema.sql:107` collides with Round 22's `ALTER TABLE sprint.rows DROP COLUMN IF EXISTS tiers` in `store/schema.sql:86`.

4. **`docs/LAYER-1-STORE.md:343-345` and `store/schema.sql:174`:**
   > "The `cards_fixed` trigger fixes `id`, `epoch`, `kind`, `of`, `stream`, `holder` and `tier` once a card exists..."
   > "`-- model (round 23): ... Fixed at add like holder.`"
   - **What the code does:** `sprint.cards_fixed()` (`store/schema.sql:188-211`) does not guard model changes; Go preserves model through the upsert update list (`store/pgread.go:294-298`). Consumer-attempt immutability is explicitly Go-only (`store/schema.sql:185-187`), so it is not an additional SQL promise gap.

5. **`docs/ENFORCED.md:38` (`L1-S7`):**
   > "Judgements start `open` and close (`answered` or `expired`) at most once in an epoch with non-empty `answer` and `answered_by` at `closed_at >= opened_at`; an open judgement has empty `answer`/`answered_by` and null `closed_at`"
   - **What the code does:** `AnswerJudgement` and `ExpireJudgement` (`store/ops.go:464-470`, `store/writes.go:285-299`) and `judgements_closed_check` (`store/schema.sql:332`) allow closing a judgement with `Answer: ""` / `Reason: ""` and `Actor: ""`.

6. **`docs/ENFORCED.md:39` (`L1-S8`) and `docs/ENFORCED.md:52` (`L1-R6`):**
   > "`Events(afterSeq, limit)` returns events in `seq` order up to `MaxEvents` (1000) and rejects negative `afterSeq` or `limit > MaxEvents` with `ErrInvalid`"
   > "Caller bugs (`ErrInvalid`): empty `ID`/`Actor`/`Kind`, zero `At`, `Epoch < 1`, identifier `> 128` bytes or text `> 64 KiB`, NUL byte or invalid UTF-8, `Writes > 500` or `Expect > 500`..."
   - **What the code does:** `Events` (`store/pgread.go:186-191`, `store/fake.go:117-125,152-157`) silently clamps `max` to `MaxEvents` and allows negative `afterSeq`; `check` (`store/check.go:50-76`) allows empty `op.Actor` and `op.Kind` and places no cap on text length, `len(op.Writes)`, or `len(op.Expect)`.

## Model vs code

1. **`tla/Store.tla:239-241` (`Empty.stopped = TRUE`) and `tla/Store.tla:595-608` (`LogMatchesState`) vs `store/pg.go:244-252` (`freshStop`) and `store/restore.go:209-229` (`checkRebuild`):**
   - In `tla/Store.tla:241`, `Empty` has `stopped |-> TRUE`, and `LogMatchesState` (`tla/Store.tla:601-608`) checks `(IF events = <<>> THEN Empty ELSE events[Len(events)].after) = Current`, which includes `stopped`.
   - In Go, `checkRebuild` (`store/restore.go:209-229`) omits `Stopped` and `StopReason` from its equality check, and `Migrate` (`store/pg.go:244-252`) only sets `stopped = true` when `!meta`, leaving upgraded stores with `stopped = false` while `Rebuild` starts from `FreshStop{Stopped: true}`. The old-store state is an explicitly documented upgrade exception pending stop/start reconciliation (`docs/LAYER-1-STORE.md:818-825`), not a fresh-store transition modeled here.

2. **`tla/Store.tla:387-400` (`ClearState`) vs `store/apply.go:147-178` (`runClear`) and `store/store.go:374-388` (`Fold`):**
   - In `tla/Store.tla:394-395`, `ClearState` carries rows forward as `[st.rows[r] EXCEPT !.state = "down", !.gen = @ + 1]`, preserving `held`, `sess`, and `role`.
   - In Go, `runClear` (`store/apply.go:164-176`) also preserves `Reason`, `Load1Centi`, `Cores`, `SlotsUsed`, `SlotsTotal`, and `Tiers`, and `Fold` (`store/store.go:378-388`) also preserves `Identity` (`s.Identity = keep.Identity`) and `Policy` (`s.Policy = keep.Policy`), none of which are in `Store.tla`. This abstraction is documented in `store/trace.go:25-60`, and `TraceOp` (`store/trace.go:148-251`) refuses ops that touch unmodeled fields (`SetRowLoad`, `SetIdentity`, `SetActivePolicy`, `SetStream*`, etc.).

3. **`tla/Store.tla:274-303` (`WriteRule`) vs `store/writes.go:205-273` (`addCard`, `checkOf`):**
   - In `tla/Store.tla:333`, `Do` for `addCard` sets `tier |-> IF w.kd \in Unheld THEN w.s ELSE st.cards[w.p].tier`, ignoring `w.s` on a consumer instead of refusing in `WriteRule` when a consumer's tier differs from its producer's.
   - In Go, `checkOf` (`store/writes.go:270`) refuses a consumer whose `c.Tier != p.Tier`. As noted in `store/trace.go:41-43`, the trace generator always passes the producer's tier so the trace walks do not hit this divergence, but `WriteRule` in `tla/Store.tla:284-290` is weaker than `store/writes.go:270` (`w.s = st.cards[w.p].tier` is missing from the `ELSE` branch of `addCard` in `WriteRule`).

## Simplifications

1. **`store/writes.go:50-58` (`spec.unchanged`):**
   - `spec.unchanged` calls `bytes.Equal(encode(was), encode(now))` (`json.Marshal` on both structs) to detect no-op `set` writes. Because `Card` and `Row` contain only comparable scalars and normalized `[]string` slices (`nil` when empty via `normSet`/`normList`), `reflect.DeepEqual(was, now)` (or a helper comparing `Card` fields) would avoid `json.Marshal` inside the writer lock; equivalence and performance need validation before replacing the comparison.

2. **`store/restore.go:209-229` (`checkRebuild`):**
   - `checkRebuild` manually lists struct fields in a slice of `{name, a, b}` pairs (`store/restore.go:212-220`), which requires an explicit update whenever Snapshot gains fields; it currently omits `Stopped` and `StopReason` while including `Identity`. Comparing all fields of `Snapshot` (or having a test that mutates every `reflect.TypeFor[Snapshot]()` field and asserts `checkRebuild` fails) eliminates the risk of forgetting a field on `Snapshot`.

3. **`store/pgread.go:245-256` (`pgTx.policies`) and `store/fake.go:262-271` (`fakeTx.policies`):**
   - `policies()` returns `(last int, active []PolicyVersion, err error)` using a slice for `active` even though `policy_one_active` (`store/schema.sql:292`) guarantees at most one active policy version. Returning `(last int, active PolicyVersion, hasActive bool, err error)` would simplify `protocol` (`store/apply.go:79-86`) and `activate` (`store/writes.go:367-389`).

## Missing tests

1. **Second `Migrate` after `SetRowLoad` (`store/functional_test.go`, `store/schema_test.go`):**
   - No test runs `SetRowLoad{Row: "w1", Load1Centi: 100, Cores: 4, SlotsUsed: 1, SlotsTotal: 4, Tiers: "flash,pro"}` and then calls `p.Migrate(ctx)` a second time to verify that `Snapshot().Rows["w1"].Tiers` is preserved (it is currently wiped to `""` by `store/schema.sql:86` and `107`).
2. **`ValidateHistory` / `checkRebuild` on a corrupted `stopped` / `stop_reason` (`store/restore_test.go`, `store/history_test.go`):**
   - `TestFunctionalValidateHistoryRefuses` (`store/restore_test.go:203-270`) tests `dropped_event`, `orphan_receipt`, `epoch_row`, `policy_not_in_the_log`, and `cursor_past_the_end`, but has no subtest mutating `sprint.epoch.stopped` or `sprint.epoch.stop_reason` (which currently passes `ValidateHistory` undetected because `checkRebuild` at `store/restore.go:212-220` omits `Stopped` and `StopReason`).
3. **Every `Snapshot` field covered by `checkRebuild` (`store/history_test.go`):**
   - There is no unit test reflecting over `reflect.TypeFor[Snapshot]()` and verifying that mutating each exported field of `Snapshot` causes `checkRebuild` to return an error.
4. **SQL model immutability, if adopted (D5):**
   - The SQL refusal cases (`store/functional_test.go:812-874`) cover existing fixed-field guards, but not model immutability. Add a model case if that becomes a SQL guarantee; consumer attempt is explicitly assigned to Go (`store/schema.sql:185-187`), so a missing SQL test for it is not a current contract gap.

## Questions for the coordinator

1. **Column name for advertised worker tiers in `sprint.rows` (`store/schema.sql:86,107`):**
   - Should `store/schema.sql:86` keep the column name `tiers` and guard the Round-22 `DROP COLUMN` so it only drops `tiers` when its type is `text[]` (`ARRAY`), or should Round 23's heartbeat column be renamed (e.g., `advertised_tiers`) so the five Round-22 dropped column names (`kind, width, tiers, model, harness`) stay permanently absent?
2. **In-place `Migrate` of pre-`FreshStop` stores (`store/pg.go:244-252`):**
   - The documented compatibility choice preserves the stop and requires one stop/start (`docs/LAYER-1-STORE.md:818-825`); should integrity validation report that prerequisite explicitly when D2 is repaired, or should a separately designed migration record the old initial state without stopping a running store?
3. **`docs/ENFORCED.md` `L1-S7`, `L1-S8`, `L1-R6` vs `store/check.go` / `store/ops.go`:**
   - Should `store` enforce the stricter checks described in `docs/ENFORCED.md` (non-empty `op.Actor`/`op.Kind`, non-empty `Judgement.Answer`/`Actor` on close, `ErrInvalid` on negative `afterSeq` or `max > MaxEvents`), or should `docs/ENFORCED.md` be edited to match `docs/LAYER-1-STORE.md` and `store/check.go`?
