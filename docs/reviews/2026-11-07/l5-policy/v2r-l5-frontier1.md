# Cold review: layer 5, policy (v2r-l5-frontier1)

Reviewer: rowan (Claude Fable 5.1), 2026-11-07, cold, on `43d849e2` (branch `v2-stitch`).
Read: `docs/LAYER-5-POLICY.md`, `docs/ENFORCED.md` (L5-1..L5-17), every file of `policy/`
(18 non-test, 17 test), `tla/Policy.tla`, `tla/Policy.cfg`, `tla/PolicyTrace.tla`, the ten
`tla/witnesses/Policy*.tla`, and the callers in `run/`, `verbs/`, `steps/`, `cmd/`, `roster/`.

Commands run (this Mac, no live store or service touched; the functional tier starts a
throwaway PostgreSQL per test under `t.TempDir()` on 127.0.0.1 and the evaluator child):

```
$ go build ./... && echo exit $?        -> exit 0
$ go vet ./... && echo exit $?          -> exit 0
$ go test -count=1 ./policy/            -> ok  github.com/mas-bandwidth/nova-sprint/policy  5.785s
$ go test -count=1 -v ./policy/ | grep -E '^\s*--- (PASS|FAIL|SKIP)' | ...
  103 PASS, 0 FAIL, 17 SKIP (the NOVA_LOAD timing probes, TestFuzzPolicyRandom2000,
  TestFuzzPolicyDefaultTickUnderLoad, TestTraceWalk behind NOVA_TRACE_DIR)
```

On the test tiers: `go test ./policy/` with no tag runs the functional tier too
(`TestFunctionalVerbsPG` 1.8-2.6 s each on a PostgreSQL it starts, `TestFunctionalEvaluator`,
`TestFunctionalEvaluatorMemoryCap`, and `TestFunctionalLoad`, which reads `time.Now`); the unit
tests proper are on `fakeClock` (`policy/helpers_test.go:58-74`) and use no real time, socket
or service. Nothing was red; nothing slow beyond the PostgreSQL starts.

Probes I wrote to show a finding were a scratch `_test.go` inside the package, run with
`go test -run TestProbe ./policy/`, and removed before this commit; their output is pasted
under the finding. Only this file changes.

## Verdict

The layer is sound in the parts a wrong answer could hurt: every answer is checked against a
closed domain with an exact refusal naming version, question, subject and line
(`policy/validate.go`, `policy/engine.go:247-280`), the tick's fallback race keeps "the first
version in order that passes answers" under one deadline (`policy/tick.go:74-118`), the
answer cache is bounded and committed only on a whole passing tick (`policy/tick.go:146-156`,
`policy/engine.go:146-156`), and the verbs are fenced `AtSeq` with every string checked
before an op is built (`policy/verbs.go:78-116, 163-175, 183-216`). Build, vet and the whole
package are green. The single worst thing is that the stitch moved the code off the model:
`policy set` with no active version now stores and activates in one op and answers judgements
(`policy/verbs.go:118-136`), and a tick now runs with no active version at all
(`policy/answer.go:27-36`, gated by `run/asker.go:82-84`), while `tla/Policy.tla` has no
"no active" state (`TypeOK` says `active \in Versions`, line 49; `Init` is after `up`, lines
61-67) and its `Set` changes no judgement (lines 74-81); the trace walk starts after `up` and
never records that path. By the standing rule (the code is bound to the model or the model
does not count) the model no longer binds the code on the sprint's first minutes. Close
behind it: after a `run` restart the fallback's "previous active" rung is empty until the
next activation, so guarantee 4's chain is active -> built-in, not active -> previous ->
built-in.

## Defects

Ranked, most severe first. Severity: blocker | serious | minor | nit.

### D1. serious — The first-set activation and the no-active tick are outside `tla/Policy.tla`

- `policy/verbs.go:121-136` (`setWrites`): when `snap.Policy == 0`, `set` appends
  `SetActivePolicy{v}` and `AnswerJudgement` for every open `policy:v0` judgement, in the
  same op as the store. `policy/verbs.go:106-109` returns `Activated: true`.
- `policy/answer.go:27-36` (`tried`): with `Active == nil` the tick is answered by the
  built-in (Version 0); `run/asker.go:82-84` then sets `NoDeal` so nothing is dealt.
- `tla/Policy.tla:49` `active \in Versions` (no None); `:61-67` `Init` has `active = 1`;
  `:74-81` `Set` leaves `judgements` unchanged and never activates; `:110-114` `Pick` assumes
  an active version. `policy/trace_test.go:71-90` builds the walk after `w.up()`, so no trace
  record can carry the no-active state, and `TraceState` (`policy/trace.go:58-61`) refuses to
  project a store with no active version (`upFirst("trace", ...)`).
- Why it matters: LAYER-5 "The model" and L5-14 claim the code is a behaviour of the model;
  guarantee 3 ("From `up` on exactly one version is active; before `up` nothing runs",
  doc 291-292) is no longer what the code does (a run ticks before any `up`/`set`), and the
  one new transition that writes the store (`set` that activates and answers judgements) has
  no model, no witness and no trace.
- Repair: let `active` range over `Versions \cup {None}`, add `SetFirst(v)` (stores,
  activates, zeroes `judgements[Default]`) and a `Tick` branch where `active = None` answers by
  `Default` and cannot move the card to working (the `NoDeal` gate), extend `TraceState` to
  project `active = -1`, and start some walk seeds from an empty store.

### D2. serious — The previous-active fallback rung is lost on every run restart

- `policy/verbs.go:303-329` `LoadVersions` reads `Prev` by walking the log to the snapshot's
  seq, and the doc (`docs/LAYER-5-POLICY.md:472-473`) tells layer 6 to use `PrevOf` as it folds
  instead. `run/tick.go:82-91` `NewTicker` reads one cold snapshot and never walks from seq 0
  ("no walk from seq 0, drift row 23"); `run/tick.go:45` `prev` starts at 0 and is set only in
  `Fold` (`run/tick.go:116-119`) from events after that snapshot; `run/asker.go:31-39` builds
  `Versions` from it. `LoadVersions` has no non-test caller outside `policy/` (grep below).
- Why it matters: guarantee 4 (doc 293-295, L5-4), "Every tick is answered by the active, the
  previous or the built-in version": after a restart, an active version that breaks falls to
  the built-in, whose numbers, `_SECURITY_STREAMS`, prices and bench map are not the
  coordinator's, and `FallbackWasActive` is trivially true because `prev` is None, not because
  the code kept it.
- Repair: seed `prev` once in `NewTicker` from `policy.LoadVersions` (one walk at start, the
  verb already exists), or have `SetActivePolicy` write a `previous` beside `Snapshot.Policy`
  so the cold snapshot carries it. (The fix is in layer 6 or layer 1; the promise is this
  layer's.)

```
$ grep -rn --include='*.go' -F 'LoadVersions(' . | grep -v '^./policy/' | grep -v _test.go
(no output)
```

### D3. serious — A killed evaluator child fails, and judges, versions that never ran

- `policy/evaluator.go:104-134` `remote`: on the child's death (`errc`), on the parent's
  timeout (`req.Budget + evalGrace`) or on `ctx.Done()`, it returns
  `killed(failing, ...)` with `failing = tried` (`policy/answer.go:60-62`): active, previous
  and the built-in, each a `Failure` at line 1. `policy/evaluator.go:162-169` `killed`.
  `policy/answer.go:82-90` `Judgements` then opens one judgement per failure, `policy:v0`
  included.
- Why it matters: the doc's own sentence is "every version it ran fails" (doc 840-841 and
  952-953); the parent fails every version it *might* have run. What has started inside the
  child is decided by `race` (`policy/tick.go:83-95`): run i+1 starts the moment run i's
  `await` returns a failure (`launch(i)` at the top of the loop, `:84`), and every later run
  starts at `spec` (`:86-88`), which is `fallbackAfter` from the child's own `start`
  (`policy/answer.go:56`, reached from `policy/evaluator.go:307`), a clock the parent never
  sees. So the started set is any of {active}, {active, previous} or all three at any moment
  of the tick: a fast refusal in the active version starts the previous at once, a second
  one starts the built-in. The parent cannot tell them apart by time, and today does not try:
  a memory-cap death or a hang while only the active (or active and previous) had started
  opens a `policy:v0` judgement ("the built-in failed") for a version that never ran, while
  `tla/Policy.tla:119-122` `Failing` puts `Default` in the failing set only when active and
  prev both break on the snapshot.
- Repair: track execution explicitly, since only the child knows it. In `Engine.start`
  (`policy/tick.go:59-67`) the child writes one line, `started <label>`, to stderr as each run
  launches (`capped`, `policy/evaluator.go:226-248`, already collects the child's stderr live
  in the parent); `killed` (`policy/evaluator.go:162-169`) then fails only the labels seen
  there, and `watchMemory` (`policy/evalmem.go:55-64`) names the run in hand before
  `os.Exit(3)`. A second form that needs no stderr parsing: the child sends a `reply{Started:
  label}` on the gob stream at each launch and the parent's decode goroutine
  (`policy/evaluator.go:180-187`) keeps the set until the final reply.

### D4. minor — `Answers.Timeouts.Consumer` is one map shared across ticks through the cache

- `policy/validate.go:284-296` `perTier` allocates the map once at validation;
  `policy/answer.go:130` the `timeouts` setter clones `BaseRegate` but not `Consumer`;
  `policy/tick.go:167-182` replays the cached closure on a hit, so every later tick's
  `Answers.Timeouts.Consumer` is the same map object.
- Why it matters: `policy/policy.go:67-71` promises "Each tick's Answers are the caller's own,
  except the Rows lists"; `verbs/take.go:172` reads `a.Timeouts.Consumer[c.Tier]` (read-only
  today, so no harm yet); a future write by a caller silently rewrites every later tick of
  that version.
- Repair: `a.Timeouts.Consumer = maps.Clone(t.Consumer)` in the setter (one line), as `Prices`
  and `Catalog` already do.

```
$ go test -count=1 -v -run TestProbeTimeoutsConsumerAliased ./policy/
zz_review_probe_test.go:22: tick 2 Timeouts.Consumer[pro] = 1 (2700 if each tick's Answers are its own)
```

### D5. minor — `Up` takes any caller and checks no string it stores

- `policy/verbs.go:53-68` `Up` builds and applies an op from `c.Actor` and `c.OpID` with no
  `coordinator(...)` gate (`policy/verbs.go:220-228`) and no `unstorable` check.
- Why it matters: doc 254 "All are the coordinator's"; L5-17 "A verb refuses, naming the
  field, any string the store would refuse before it builds an op". An actor over
  `store.MaxName` or with a NUL reaches the store as `ErrInvalid` from this verb alone.
- Repair: `if r := coordinator("up", c, "up: the built-in default"); r != nil { return ... }`,
  or document `up` as layer 6's (`verbs/setup_runtime.go:268` is its one caller) and drop it
  from the "coordinator's" sentence.

```
$ go test -count=1 -v -run TestProbeUpAnyCaller ./policy/
zz_review_probe_test.go:57: Up as kin1 (not the coordinator): err=<nil> version=1 active=1
```

### D6. minor — The evaluator's remaining budget is computed before the child's lock

- `policy/evaluator.go:105` computes `req.Budget = e.budget - (now - start)` and
  `policy/evaluator.go:110-111` then takes `ev.mu`; `:129` starts `After(req.Budget +
  evalGrace)` after the lock. A tick that waits on the lock (behind a `check` or `catalog`
  request on the same engine, `policy/show.go:89-90`, `policy/catalog.go:60-61`) hands the
  child a budget that ignores the wait and waits that much again.
- Why it matters: L5-16 and doc 596-599, "One deadline per tick ... from the first byte of
  input construction to the last answer returned"; `TestDeadlineOnTheClock` cannot see it
  (the local engine has no lock).
  Today `run` holds one engine and ticks serially, so it is a latent bound, not a live one.
- Repair: compute `req.Budget` after `ev.mu.Lock()`, and return `killed` at once when it is
  at or below zero.

### D7. minor — `check` reports a dict answer with the same keys in another order as a change

- `policy/tick.go:211` records `v.String()`; `policy/show.go:123-128` compares those strings
  between the file and the active version.
- Why it matters: doc 263 "prints every answer that would differ from the active version's";
  `{"ahead": 0, "work": 2}` differs from nothing, and a coordinator reading "4 answers change"
  before `activate` is told the wrong thing (the count is `Result.Changes`,
  `policy/verbs.go:215`).
- Repair: compare the validated typed answers (`Width`, `Landing`, ...) or print dicts with
  sorted keys in `fresh`.

```
$ go test -count=1 -v -run TestProbeCheckKeyOrderIsAChange ./policy/
zz_review_probe_test.go:32: changes: 4 refused: 0; first: ["width(r1): {\"work\": 2, \"ahead\": 0} -> {\"ahead\": 0, \"work\": 2}" ...]
```

### D8. nit — `Live` can read a healthy run as abandoned

- `policy/tick.go:62-65`: `defer e.live.Add(-1)` runs after `r.ready <- r.exec()`;
  `policy/evaluator.go:313` reads `e.live.Load() > 0` as soon as `AnswerContext` returns, and
  the parent kills the child on `Live` (`policy/evaluator.go:122-124`). A benign race: the
  cost is a cold child on the next tick. Not observed in-process (0 of 3000 ticks, probe C).
- Repair: `o := r.exec(); e.live.Add(-1); r.ready <- o`.

### D9. nit — The refusal text names `TickTime`, not the budget in force

- `policy/tick.go:56` `overTime()` prints the constant; in the child the budget is what the
  parent left (`policy/evaluator.go:300`), and a fallback started at `fallbackAfter` had half.
- Repair: carry `e.budget` (or the run's remaining time) into the message.

### D10. nit — `Activate`'s stale "already active" check falls to the wrong fix text

- `policy/verbs.go:188-193` reads `pv.Active` before the snapshot; a racing activation is
  then refused by the store (`policy/verbs.go:255-266` `apply`) with fix "nova-sprint policy
  show --list" rather than the pre-check's "nova-sprint policy show".
- Why it matters: the coordinator is sent to the list to find a version that is already
  active, and the `What` is the store's same-value text, not this verb's; the doc (260) says
  the refusal is layer 1's text, so only the fix is wrong. Nothing is written either way
  (`AtSeq` at `policy/verbs.go:213`).
- Repair: drop the pre-read and check `snap.Policy == v` after `st.Snapshot` (`:195-198`),
  which gives the one refusal with the right fix and one store read fewer.

## Promise gaps

Doc sentence, then what the code does.

1. "Every tick is answered by the active, the previous or the built-in version" (doc 293-295;
   L5-4). After a `run` restart the previous is None until the next activation
   (`run/tick.go:45, 82-91, 116-119`); see D2.
2. "The active version is always a stored version. From `up` on exactly one version is
   active; before `up` nothing runs" (doc 291-292). A run ticks with none active and the
   built-in answers (`policy/answer.go:27-36`; `run/asker.go:82-84` turns the deal off); see D1.
3. "every version it ran fails" (doc 840-841, 952-953). `policy/evaluator.go:162-169` fails
   every version tried; see D3.
4. "Each tick's Answers are the caller's own, except the Rows lists" (`policy/policy.go:67-71`).
   `Timeouts.Consumer` is shared; see D4.
5. "All are the coordinator's" (doc 254) and L5-17 "A verb refuses ... any string the store
   would refuse before it builds an op". `Up` has neither gate (`policy/verbs.go:53-68`); D5.
6. "One deadline per tick ... from the first byte of input construction to the last answer
   returned" (doc 596-599, L5-16). Through the evaluator the wait for `ev.mu` is outside it;
   D6.
7. `policy set` refuses "`up` not run" (doc 259, the verbs table). It no longer does: the
   first `set` activates (`policy/verbs.go:118-136`, doc 989-996). The table is stale.
8. `timeouts()` "dict of seconds ... `row_silent_gone`, `row_silent_withdraw`, `landing_stall`,
   `base_regate`, `judgement`, `route_rest_for`" (doc 104). The validator also requires `tick`
   and `consumer` (`policy/validate.go:260`); the question table does not list them (the
   stitch section does, doc 959-965). `limits()` (doc 102) lists `fleet_width_cap` in the
   default column, not the domain column.
9. "Beyond the per-answer domain, the validator checks the answers together, so no policy
   can leave the sprint unable to deal" (doc 108-110; L5-3). The read count the joint check
   needs is the tick's `reads` answer (`policy/answer.go:168-172`); when a caller asks
   `rows(read)` without `reads` for the card it needs 1. Layer 3 asks both together
   (`steps/tick.go:148-153`), and `check` does (`policy/inputs.go:245-247`), so the promise
   holds by the callers' pairing, not by the engine. Probe E:

```
$ go test -count=1 -v -run TestProbeJointReadsNotAsked ./policy/
zz_review_probe_test.go:72: full questions (reads asked): 1 failure(s): [POLICY REFUSED v2 line 33 rows(p5 read): reads is 2, and 1 row(s) named have work above 0 in width, while 2 candidate(s) could take a read]
zz_review_probe_test.go:75: rows(read) asked without reads: version 2, 0 failure(s): [], rows(p5 read)=[r5]
```

Promises checked and kept (so the reader need not): the closed name set and arity with the
keyword-only case (`policy/engine.go:249-265`); every domain refusal in the table with exact
text (`policy/validate_test.go:35-99`); `yes` with no ok read, `fix` with no fix read, `retry`
with an ok or fix read (`policy/validate.go:122-135`); reads of the current review *and* head
only (`policy/inputs.go:92`); the author never a read candidate (`policy/inputs.go:138`); no
`while`, `load`, `set`, recursion, global reassignment, clock, random or file
(`policy/engine_test.go:19-100`); the 64 KiB cap through a `LimitReader`
(`policy/verbs.go:239-253`); `Diff` refused above 10,000 lines and linear in space
(`policy/diff.go:17, 43-46, 117-130`); one judgement per version by group key
(`policy/answer.go:85`); the 4 MiB tick cap and a cache that holds one passing tick
(`policy/tick.go:178-180`, `policy/engine.go:146-156`); `activate` answering the old, the new
and the built-in's judgements in one `AtSeq` op (`policy/verbs.go:206-214`); the `data`-kind
refusal through one helper (`policy/verbs.go:279-285`); `check` asking both rungs
(`policy/show.go:101-108`); the stale-`Versions` refusal (`policy/answer.go:57-59`).

## Model vs code

- **A state and a transition the model lacks** (D1): no-active ticks, and `set` that
  activates and answers judgements. `tla/Policy.tla:49, 61-67, 74-81`.
- **A `NoAnswer` the model does not allow**: `policy/answer.go:57-59` answers `Version -1`
  for a `Versions` read at another seq with a failure of no version (no judgement), while
  `TickAlwaysAnswered` (`tla/Policy.tla:156-159`) says `NoAnswer` only when every version
  tried is in `tickBreaks`. `run/asker.go:31-32` sets `Seq` from the same snapshot, so run
  never reaches it; the walk never records it; it is the code's state, not the model's.
- **`Failing` is per version; a kill is per child** (D3): `tla/Policy.tla:119-122` versus
  `policy/evaluator.go:162-169`.
- **`breaks` in the refinement mapping is check's set, a tick's is smaller**:
  `policy/trace.go:81-86` computes `breaks` by `checkRun` on `FullQuestions` with both rungs
  (`policy/show.go:94`); a tick asks a subset (`run/asker.go:54-67`). A version failing only
  on a question the tick does not ask would be recorded as answering while in `breaks`, and
  `AnsweredByUsable` would refuse an honest trace. The walk's drift breaks only `width`
  (`policy/trace_test.go:55-58`), which every tick asks, so it never fires: a weakness of the
  mapping, not a wrong trace.
- **The early fallback is not in the model** and need not be: `Pick` is the first passing in
  order and `race` keeps that (`policy/tick.go:83-95`); the doc says so (650-652). Agreed.
- **`WaitKeepsItsBound`, `waitBound`, `startBound`** are layer 3's and unprojected (doc
  872-876); agreed, and the ledger says so (L5-8).
- **`Activate` as one step**: `tla/Policy.tla:86-98` and `policy/verbs.go:206-214` agree,
  including `judgements[Default] = 0`.
- Witnesses: the ten `tla/witnesses/Policy*.tla` are each the model with one invariant
  reversed; I did not run TLC (no Java here); the doc's last run (917-921, 149 ok) is the
  evidence on record.

## Simplifications

Glenn's bar: tight engine, no special cases. What is bigger than it needs to be:

1. **Dead exported surface.** `Catalog.Unknown`, `unknownHarness` and `CatalogFix`
   (`policy/catalog.go:32-33, 101-121`) and `ReadCatalog` with the evaluator's third request
   kind (`policy/catalog.go:35-66`, `policy/evaluator.go:310-311`) have no non-test caller
   outside `policy/`; the row verbs take the bench model from a tick's `Catalog`
   (`verbs/apply.go:89-101`). About 90 lines and one wire case.

```
$ for s in '.Unknown(' 'ReadCatalog(' 'CatalogFix'; do grep -rn --include='*.go' -F "$s" . | grep -v '^./policy/' | grep -v _test.go; done
(no output)
```

2. **`unsafe` in the policy layer.** `policy/prefer.go:42` keys the memo on
   `unsafe.SliceData(cands)`; `&cands[0]` (guarding `len == 0`) is the same address with no
   `unsafe` import, or the engine, which built the tuple (`policy/inputs.go:143-146`), can hand
   the builtin the set's digest as a thread-local it already has.
3. **The check mode threaded through the tick.** `all` reaches `run`, `exec`, `one`,
   `fresh`, `refuse` and `joint` (`policy/tick.go:28, 141-146, 151-155, 210-212, 220-222,
   244-246`): six branches so one verb can keep going past a refusal and keep answer text. A
   separate `checkAll` loop over the same `one` would leave the tick path with no branch.
4. **Three walks per `set`.** `sameBody` twice and `setWrites` each call `versions()`
   (`policy/verbs.go:93, 112, 126`), which reads `st.Policy(v)` for v = 1.. until not found;
   `reasons()` walks the whole event log for `show` and `list` (`policy/show.go:178-189`). A
   store `Policies()` listing would be one read.
5. **`map[bool]string{...}[cond]` as a conditional** (`policy/answer.go:206`,
   `policy/validate.go:301`, `policy/trace.go:79`) and `b2i` for counting in ten places: an
   `if` reads faster and costs nothing.
6. **`sibling()` starts a child per `TraceState`** for a process engine
   (`policy/trace.go:81`, `policy/evaluator.go:138-145`), cleaned only by the GC; `TraceState`
   runs only under the walk on a local engine, so the branch is unreachable code.
7. **Two refusal types and a string list.** `*Failure` (policy), `*Refusal` (verb) and
   `Report.Refusals []string` (`policy/show.go:19-23`) say "no" three ways; `Report` could carry
   `[]Failure`.
8. Size: 3,141 non-test lines against a 1.5k estimate is on record (doc 906-909) and is
   Glenn's call; items 1, 3 and 4 are the honest cuts I see (about 250 lines).

## Missing tests

1. No test that run's `prev` survives a restart, or that `LoadVersions` seeds it (D2):
   `run/tick_test.go` and `policy/verbs_test.go:153-172` both start from a live ticker.
2. No test of which versions a child kill fails (D3): `TestFunctionalEvaluatorMemoryCap`
   (`policy/round5_test.go:143-146`) accepts a failure per version tried, the wrong promise.
3. No test that a cache hit hands the caller its own `Timeouts` (D4); `TestDeterministic`
   (`policy/engine_test.go:118-131`) compares values, which alias equally.
4. No `Up` refusal row (D5): `testUp` (`policy/verbs_test.go:43-57`) has success only.
5. No test that `check` reports no change for an equal dict in another key order (D7).
6. The trace walk never records: a no-active tick, the first `set` that activates, the
   stale-seq refusal, a child kill, a version failing on a question the tick did not ask
   (`policy/trace_test.go:92-104`). Seeds that start from an empty store and a drift that
   breaks `landing` on `backed_up` only would cover four of the five.
7. `Diff` has no correctness property: `testShow` (`policy/verbs_test.go:193-203`) asserts two
   lines and `TestColdread2DefectDiffQuadraticMemory` memory only. "Apply the diff to a and get
   b" over random bodies would hold `hirschberg` (`policy/diff.go:79-107`).
8. `PrevOf` on the first-set event: `testSetFirst` (`policy/verbs_test.go:62-76`) does not
   call `LoadVersions` to show `Prev` stays nil.
9. The functional tier is not separable from the unit tier: no build tag or `-short` guard on
   `TestFunctionalVerbsPG`, `TestFunctionalEvaluator*`, `TestFunctionalLoad`
   (`policy/functional_test.go:24, 93`; `policy/round5_test.go:73, 146`), so `go test ./policy/`
   starts PostgreSQL and reads the wall clock. The standing rule asks that these be the
   functional tier by construction.

## Questions for the coordinator

1. D2: is "no walk from seq 0" (drift row 23) meant to accept losing the previous active on a
   restart, or should the store carry it beside `Snapshot.Policy`? Which layer owns the fix?
2. D1: extend `Policy.tla` to the no-active state and the first-set transition, or declare
   the lock waits on it? The walk needs seeds that start before `up` either way.
3. Escalation to frontier is unreachable for a heavy card unless some frontier row lists
   `heavy`: `policy/inputs.go:138` admits only rows whose tiers hold the card's tier, and
   `TestDefaultAnswersEverySnapshot` (`policy/answer_test.go:138`) shows a heavy card at 2
   bounces dealt to the heavy row because the frontier kin lists only `frontier`. The doc owes
   this to layer 1 ("a row ranked above a tier lists it", doc 358-360); is a roster lint owed,
   or is a frontier kin expected to list every tier below it in nova-config?
4. D5: is `up` layer 6's verb (setup) so the coordinator gate belongs there, or this layer's?
5. Item 1 under Simplifications: is `ReadCatalog` kept for the 2.0.x row verbs, or dropped?
6. Promise gap 9: should the engine require `reads` beside a `rows(read)` ask for the same
   card (refuse the ask otherwise), so the joint check's promise does not rest on the caller?
7. D3: is the memory-cap death allowed to name the version in hand on stderr (one line from
   the child), or must the parent infer it from `fallbackAfter`?
