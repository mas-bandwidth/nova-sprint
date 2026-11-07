# Cold review: layer 1 (store), v2r-l1-flash1

Base: `43d849e29d0e4568ba6a1348a076a59259639938`. Report only; no code was changed.
Read cold: `docs/LAYER-1-STORE.md`, `docs/ENFORCED.md`, every file under `store/`,
`tla/Store.tla`, `tla/StoreTrace.tla`, and the store witnesses under `tla/witnesses/`.

Commands run (first lines; last lines follow):

```
$ go build ./...
$ go vet ./...
$ go test ./store/... -count=1 -timeout 600s
$ go test ./store/ -run 'TestConformanceFake$|TestStore|TestEventsCap|TestTrace' -count=1 -timeout 300s
$ go test ./store/ -run 'TestFunctionalConformanceICU' -count=1 -timeout 240s
$ go test ./store/ -run 'TestFunctionalConformanceICU/writes' -count=1 -timeout 120s -v
$ go test ./store/ -count=1 -timeout 540s -skip 'TestFunctionalConformanceICU'
```

Run results, last lines verbatim:

- `go build ./...` — clean (no output).
- `go vet ./...` — clean (no output).
- Unit subset — `ok  github.com/mas-bandwidth/nova-sprint/store 0.439s`.
- Full `go test ./store/...` — timed out: goroutines blocked 9 minutes in
  `sync.Mutex.Lock` at `store/coldread4_test.go:235`, ending
  `FAIL github.com/mas-bandwidth/nova-sprint/store 600.226s`.
- ICU single scenario — `--- FAIL: TestFunctionalConformanceICU/writes (0.01s)`,
  cause `ERROR: option "locale_provider" not recognized (SQLSTATE 42601)`
  (this bench's `initdb` on PATH is PostgreSQL 14; the suite needs 18).
- Suite with only the ICU test skipped — 43 functional failures in 58s, each an
  `initdb` failure (`could not create shared memory segment: No space left on
  device` under parallel servers, on the PATH PostgreSQL 14). These read as
  bench/environment failures, not store logic; they are NOT RUN-equivalent for
  the PG18 gate and must be re-run on a PG18 bench.

## Verdict

Not lock-clean. No blocker was found in the store's transaction logic (the seven
guarantees hold in the code as read), but one serious integrity-check gap and one
serious test-suite hang stand, and the mandated full-suite run does not pass on
this bench. Worst finding: `checkRebuild` never compares the sprint's stop, so
`ValidateHistory`/`RestoreEnd` accept a dump whose stop disagrees with its log
(serious). Defects: 0 blocker, 2 serious, 4 minor, 1 nit.

## Defects

1. **serious** — `store/restore.go` `checkRebuild`:
   the differ list (lines 212–220) compares seq, epoch, policy, streams, cards,
   rows, judgements, cursors and identity, but never `Stopped`/`StopReason`.
   `Rebuild` folds the stop (`store/store.go:379`), and `snapshotTx` reads it
   from the epoch row (`store/pgread.go:115`), so a dump (or live store) whose
   epoch-row stop was changed without a `SetStopped` event passes
   `ValidateHistory` and `RestoreEnd` — the exact tamper the history check
   exists to refuse. Repair: add the stop and its reason to the compared parts.
2. **serious** — `store/coldread4_test.go`
   `TestFunctionalConformanceICU`: the store factory holds `mu` across
   `icuDB(...)` (lines 234–237), and `icuDB` fails via `require.NoError`
   (line 207). On any PostgreSQL that cannot create the ICU database, the first
   subtest exits its goroutine with the mutex held and every other subtest
   blocks forever: one environmental failure becomes a 600s hang that hides the
   failure (reproduced above). Repair: unlock with `defer` immediately after
   locking, or return the error instead of asserting inside the factory.
3. **minor** — `store/check.go` `check`:
   no bound on `len(op.Writes)` or `len(op.Expect)`, and `text` (line 109) has
   no length bound. One op can therefore hold the single writer lock for an
   unbounded number of sequential write checks (each a transaction read on PG),
   stalling every writer behind it. Repair: refuse more than a fixed count
   (the ledger's 500) of writes/conditions with `ErrInvalid` in `check`.
4. **minor** — `store/ops.go`
   `SetCardReview` overflow: at `review = MaxInt32` the kind rule returns a
   logged refusal reason, while the sibling internal counter overflows are
   plain errors (`next`, `store/apply.go:183`, used for gen, epoch and policy
   version) that log nothing. Same class of counter, two outcome classes; a
   caller replay-loops differently on each. Repair: return a plain error from
   the review rule, as `next` does.
5. **minor** — `store/fake.go`
   `Fake.Apply`/`Clear` ignore the context (`_ context.Context`), while PG
   reports a done context as `ErrUnknown` (`store/pg.go:261`). The Fake claims
   "the same semantics of PG" (line 14); a layer above cannot test its
   cancellation path against it. Repair: return `ErrUnknown` when
   `ctx.Err() != nil` at entry.
6. **minor** — `store/functional_test.go`
   `pgBin` prefers `initdb` on PATH over the Homebrew `postgresql@18` install.
   On a bench with an older PostgreSQL on PATH (14 here) the whole functional
   tier silently runs the wrong major version and fails in unrelated-looking
   places (ICU syntax, shared memory). Repair: prefer the versioned
   `postgresql@18` install, or refuse a major version below 18 with the
   requirement named.
7. **nit** — `store/apply.go`
   `needsLanded`: if a need card were missing, the reason names its state as
   empty (`need %s of card %s is %s, not landed` with a zero card). Unreachable
   while needs are checked at write time; the message would mislead if a
   future path reached it. Repair: name the missing need explicitly.

## Promise gaps

- Doc: "`SetCursor` refuses moving a cursor backward or past the last event
  `seq`; `Events(afterSeq, limit)` returns events in `seq` order up to
  `MaxEvents` (1000) and rejects negative `afterSeq` or `limit > MaxEvents`
  with `ErrInvalid`" (`docs/ENFORCED.md` L1-S8). Code: `Events` caps instead
  of rejecting — "max is capped at MaxEvents, and max <= 0 means MaxEvents"
  (`store/store.go`,
  `capEvents` at `store/fake.go:152`), a negative `afterSeq` is not checked,
  and `TestEventsCap` asserts the capping. The layer doc's own interface text
  agrees with the code; the ledger row does not hold.
- Doc: caller bugs include "empty `ID`/`Actor`/`Kind`", "`Writes > 500` or
  `Expect > 500`", "text `> 64 KiB`", "duplicate tier in `PutRow`/`SetRowTiers`"
  (`docs/ENFORCED.md` L1-R6). Code: only the id's emptiness is refused
  (`store/check.go`);
  `Actor`/`Kind` pass through `checker.name` (length and text only, line 51),
  there are no 500/64KiB bounds (defect 3), `SetRowTiers` no longer exists, and
  the layer doc says brief/reason/body/head/branch/note are "unbounded text"
  (`docs/LAYER-1-STORE.md` "Ids and names"). The ledger row is stale in four
  places; the code matches the layer doc except the missing count bound.
- Doc: the store "refuses a consumer whose holder row does not serve its tier"
  and caps `working` by width (`docs/LAYER-1-STORE.md` tier paragraph and
  writes paragraph; repeated in the `SetCardState` comment,
  `store/ops.go`).
  Code: round 22 removed both — `specOf` gives `SetCardState` a plain state
  write with no tier or width rule (`store/ops.go:489`), and `checkHolder`
  states width and tiers are "never the store's"
  (`store/writes.go`).
  The round-22 entry supersedes the older text on paper, but the main text and
  the code comment still promise a store refusal the store deliberately does
  not make; a reader of either is misled.
- Doc: `ValidateHistory` ends with "`Rebuild` over the events is the snapshot
  (`checkRebuild`)" (`docs/LAYER-1-STORE.md` round 21). Code: the comparison
  omits the stop (defect 1), so "is the snapshot" is not what is checked.

## Model vs code

- The model carries the same counter arithmetic as the code for the counters
  it has: gen rises once per op that writes an existing card or row
  (`Bump`, `tla/Store.tla:370`; code `store/apply.go:357`), epoch advances by
  one on clear, cursor is bounded by the log's length before the op
  (`CursorsFit`, `tla/Store.tla:404`; code `store/writes.go:305`). There is no
  ring, wrapping counter, or hand-rolled index arithmetic in `store/` for a
  layout model to mirror; the one search over an index space,
  `Fake.EventIdentity`'s binary search (`store/fake.go:145`), is sound because
  the fake's log is append-only in seq order. No layout divergence found.
- Code the model cannot see (all named in `store/trace.go:25`, checked here
  against `tla/Store.tla`): streams and every stream write, the `AtSeq`
  condition, `SetActivePolicy` and policy activation, most card columns and
  their set writes, `SetRowLoad`, `SetIdentity`, and a judgement's
  close-before-opened refusal. `TraceOp` refuses ops using them, so traces
  stay honest, but no TLC run says anything about those paths; the schema
  CHECKs and the conformance suite are their only holders.
- Divergence, model wider than code: `Outcome` accepts a clear
  unconditionally once the epoch matches (`tla/Store.tla:411`) and never
  checks its conditions, while the code refuses a clear whose `AtSeq` fence
  is stale (`store/apply.go:147`, `checkClear`). The fixed op table never
  fences a clear, so no checked run can show it; the model as written would
  accept a behaviour the code refuses.
- Divergence, code wider than the model's fixed instance, by design: the
  model takes a consumer's tier from its producer (`Do`, `tla/Store.tla:333`)
  and has no streams, so producer-stream equality for consumers is held only
  by `checkOf` and the `cards_fixed` trigger, not by any invariant.

## Simplifications

- Replace `checkRebuild`'s hand-kept part list with a whole-snapshot
  comparison (the conformance suite already asserts
  `assert.Equal(snap, Rebuild(events))` in thirty places). A list that must
  name every new snapshot field will omit the next one the same way it
  omitted the stop; one equality check cannot drift.
- Delete the tier/width sentences from the `SetCardState` comment
  (`store/ops.go:154`) and the superseded tier/width paragraphs of the layer
  doc's main text, leaving the round-22 wording as the only statement. Two
  texts currently say opposite things about the store's most-checked refusal.
- Collapse the ledger's L1-R6 row to what `check` actually enforces, once the
  coordinator rules on the count bound (defect 3); four of its clauses name
  behaviour or writes that no longer exist.

## Missing tests

- A `ValidateHistory`/`RestoreEnd` mutation that flips the epoch row's stop
  (or stop reason) with no matching event: refused today by nothing
  (defect 1). The five existing mutations in
  `store/history_test.go:36` cover seq, receipt, epoch, policy and cursor —
  not the stop the same round added to the snapshot.
- `SetCardReview` at `MaxInt32`: no test pins whether the outcome is a logged
  refusal or a plain error (defect 4), so the two counter classes can drift
  again unnoticed.
- A cancelled-context `Apply` against the Fake, asserting `ErrUnknown`
  parity with PG (defect 5). The unit tests otherwise keep the unit rules:
  fixed time `t0` (`store/conformance_test.go:19`), no sockets, no sleeps;
  real time and PostgreSQL appear only in the functional, schema-writes and
  load files, which skip without `initdb` or `NOVA_LOAD=1`.
- The ICU factory failure itself: on a PostgreSQL without the ICU provider
  the test should fail in seconds naming the provider, not hang (defect 2);
  the single-scenario run above is the reproduction, and no test guards the
  fast-failure shape.

## Questions for the coordinator

- Which text rules on `Events`: the layer doc and code (cap at 1000, no
  error) or the ledger (reject with `ErrInvalid`)? One of them should change
  so the next reader is not promised both.
- Is an op-size bound (writes/conditions per op, the ledger's 500) a store
  rule or not? If yes, defect 3 is a code fix in `check`; if no, the bound
  the lock's stall time rests on should be stated where the writer lock is
  described.
- Should counter overflow be uniformly a plain error (gen, epoch, policy
  version today) or uniformly a logged refusal? `SetCardReview` is the one
  counter on the other side (defect 4); either ruling is one line, but a
  caller's replay behaviour depends on the answer.
- What is the functional tier's PostgreSQL floor, and may the harness enforce
  it (defect 6)? On this bench the mandated suite cannot go green until the
  PG18 install is the one the harness picks.
