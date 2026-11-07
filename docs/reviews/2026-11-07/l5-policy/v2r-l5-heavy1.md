# Cold review: layer 5, the policy (v2r-l5-heavy1)

Reviewer: rowan (Claude Opus), 2026-11-07, at 43d849e2 on v2-stitch. Read:
docs/LAYER-5-POLICY.md, docs/ENFORCED.md section 6, every file of policy/
(code, tests, default.star), tla/Policy.tla, tla/PolicyTrace.tla,
tla/Policy.cfg, and the callers in run/asker.go, run/tick.go, run/serve.go,
verbs/apply.go, verbs/check_admission.go and cmd/nova-sprint/open.go.

Commands run:

```
$ go build ./... && go vet ./... && echo BUILD_VET_OK
BUILD_VET_OK
$ go test -count=1 ./policy/
ok  	github.com/mas-bandwidth/nova-sprint/policy	3.216s
$ go test -count=1 -v -run 'TestFunctional' ./policy/
--- PASS: TestFunctionalLoad (0.15s)
--- PASS: TestFunctionalEvaluatorMemoryCap (0.07s)
--- PASS: TestFunctionalEvaluator (0.52s)
--- PASS: TestFunctionalVerbsPG (0.00s)      (its subtests skip: no initdb here)
```

Nothing is red. The plain unit run is not pure: it runs `TestFunctionalLoad`,
`TestFunctionalEvaluator` and `TestFunctionalEvaluatorMemoryCap`, which start
child processes, and `TestFunctionalLoad` reads the wall clock and asserts a
timing (defect S5). Every other wall-clock probe skips without `NOVA_LOAD`.

One probe was written for this review in a temporary `policy/zz_probe_test.go`
(removed before commit; nothing but this report changes):

```go
e := NewEngine(RealClock{}); vs := Versions{Active: pv(1, defaultBody)}
snap, ro := loadSnap(2000, 10); q := FullQuestions(snap, nil)
e.Answer(vs, snap, ro, q)                 // the child starts and answers
_ = e.proc.cmd.Process.Kill()             // the child dies between ticks; the parent does not reap it
time.Sleep(100 * time.Millisecond)
for i := range 3 { a, fs := e.Answer(vs, snap, ro, q); t.Logf(...) }
```
```
$ go test -count=1 -v -run TestZZProbeEncodeFailNoRestart ./policy/
zz_probe_test.go:25: tick 1 after the kill: version -1, 2 failures, first: write |1: broken pipe
zz_probe_test.go:25: tick 2 after the kill: version -1, 2 failures, first: write |1: broken pipe
zz_probe_test.go:25: tick 3 after the kill: version -1, 2 failures, first: write |1: broken pipe
```

## Verdict

The in-process engine is sound: the closed question set, the domain
validators, the per-call step budget, the one-deadline race with an early
fallback, the joint check and the verbs' fences all do what the layer doc
says, and I found no way for an in-domain answer to corrupt the store. The
process evaluator, which is how the binary actually runs every version
(`cmd/nova-sprint/open.go:52`), is not: its failure handling breaks the
layer's central promise that a tick is always answered by active, previous or
built-in. The single worst thing: when the evaluator child dies between
requests, `send`'s write error is returned without stopping the child
(`policy/evaluator.go:114-116`), so the dead child is never replaced and
every later tick, check and catalog read fails with "broken pipe" until the
process restarts (reproduced above): no version answers, the run deals
nothing, and nothing in the layer heals it. Second, a child death fails every
version tried, the healthy built-in included, so the fallback never runs in
exactly the case it exists for (S2). The model and its trace check bind only
the in-process engine and a dealer that deals on a fallback, which the binary
does not (S3); the layer should not lock until the evaluator paths are
modelled or traced.

## Defects

Ranked, most severe first. B = blocker, S = serious, M = minor, N = nit.

**B1. A child that dies between requests is never replaced: every later
request fails forever.** `policy/evaluator.go:114-116`: when `ev.send` fails
(the write of the request to a dead child's stdin, "write |1: broken pipe"),
`remote` returns `killed(...)` without `ev.stop()`, so `ev.cmd` stays set and
`send` (`:174`) reuses the dead pipe on every later request. Only the
decode-error and timeout branches (`:126-133`) stop the child. Breaks
guarantee 4 ("A runtime error never stalls a tick", LAYER-5:293-295) and
L5-16 ("The next request starts a new child", LAYER-5:841): after an OOM
kill, an operator's kill or any crash while idle, every tick is
`Version -1`, run deals nothing (`run/asker.go:81-96`), `verbs/apply.go:77-80`
reports "no policy version answered, not even the built-in" to every verb
that needs answers, and `Judgements` opens a false judgement for each version
including the built-in. Reproduced above. Repair: call `ev.stop()` on any
`send` error (and on a `startChild` error), so the next request starts a
fresh child.

**S1. The evaluator's deadline does not count the wait for the child.**
`policy/evaluator.go:105`, `:110`, `:129`: `req.Budget` is computed before
`ev.mu.Lock()`, and the parent's own timer `e.clock.After(req.Budget +
evalGrace)` starts only after the lock is taken; the child starts its
deadline when it reads the request. A caller queued behind another on one
engine (the doc: "Concurrent ticks on one process engine are served one at a
time", LAYER-5:847; the binary shares one engine as `Answers`, `Policy` and
the services' `policy`, `cmd/nova-sprint/open.go:52-57`) therefore waits for
the holder (up to its Budget + 100 ms, twice for a `check`, whose `report`
runs a second `checkRun`, `policy/show.go:123`) and then gets a full budget of
its own. Breaks L5-5 ("A tick has one deadline, TickTime, from input
construction to the last answer"). Repair: take the lock with the deadline in
hand (a lock acquired by select on a channel with the deadline timer), and
compute the budget sent to the child after the lock is held.

**S2. A dead or late child fails every version tried, the built-in that
never ran included.** `policy/evaluator.go:126-133` with `killed`
(`:162-169`) fail every source in `failing`, which for a tick is all of
active, previous and built-in (`policy/answer.go:60-62`). When the active
version allocates past `EvalMemory` (`policy/evalmem.go:55-63` exits the
child), the previous and the built-in are never run, the tick is `NoAnswer`,
and `Judgements` opens a `policy:v0` judgement blaming the built-in for
"over the evaluator's memory cap". It recurs every tick while that version
is active. `policy/round5_test.go:153-154` asserts this outcome ("a dead
child answers nothing"). The model says the opposite: `Pick` is the previous
or `Default` whenever they are not in `breaks` (`tla/Policy.tla:110-114`), and
`Failing` never includes a version not reached (`:119-122`). Breaks
LAYER-5:233-237 ("the whole tick is answered by the version active before it
... else by the built-in default") and L5-4. Repair: have the child report
which version it is running (or run each version in its own request), fail
only that one, and retry the remaining versions in a fresh child within the
remaining budget.

**S3. The model and the trace check assume a fallback deals; the binary does
not.** `tla/Policy.tla:131-132` lets a tick move the card from ready to
working whenever `Pick # NoAnswer`, and the policy walk deals on any answer
(`policy/trace_test.go:160`, `a.Version != -1`). The binary's run deals
nothing unless the active version itself answered (`run/asker.go:81-96`,
`noDeal`, defect 53). So `TestTraceWalk` binds a dealer that does not exist,
and the doc's fallback ("its safe default is already in force",
LAYER-5:241-243) is not what runs: when the active breaks, the fallback
answers only the argument-free numbers. Not a corruption (the binary is
stricter), but a model/code divergence on the layer's main action. Repair:
either add the rule to `Tick` (`c = "working" /\ card = "ready" => Pick =
active`) and to the walk, or have run deal on a fallback; and say which in
LAYER-5 "The tick's answer".

**S4. The no-active-version state and the first set's activation are outside
the model.** `policy/verbs.go:99-111` and `:121-136`: `Set` with no version
active stores and activates in one op; ticks before it are answered by the
built-in with `Versions.Active == nil` (`policy/answer.go:27-36`). The model
starts after `up` with `active = 1` (`tla/Policy.tla:61-62`), `TypeOK`
requires `active \in Versions` (`:49`), and `Set` never activates (`:74-81`).
The walk (`policy/trace_test.go:85`) always runs `up` first. So the path the
stitch added for defect 53 is neither modelled nor traced, against the
standing rule that the code is bound to the model. Repair: add a `None`
active state to `Init` and a `SetFirst(v)` action (store and activate, answer
`judgements[Default]`), with a walk that starts without `up`.

**S5. The load promise L5-7 is checked by nothing in CI, and the one test
that checks it runs ungated in the unit run.**
`policy/functional_test.go:93-112`: `TestFunctionalLoad` calls `breakTiming`
only under -race (`:94-97`), so without -race it runs in a plain `go test
./policy/` with child processes and the wall clock and asserts
`require.Less(cold, TickTime)` (`:112`), a flake on a loaded bench, against
the doc's "the wall-clock timing probes run only with NOVA_LOAD set"
(LAYER-5:879-880). In CI it never runs: the unit job uses `-skip Functional`
and the functional job runs with `-race` (`.github/workflows/ci.yml:69`,
`:91`), where it skips. The other two L5-7 tests skip without `NOVA_LOAD`
(`policy/break_test.go:25-29`). Repair: gate `TestFunctionalLoad` on
`breakTiming` always, and add a job that runs the `NOVA_LOAD` probes without
-race on a reserved bench, or drop L5-7's claim to CI.

**M1. Any argument-free ask on an engine throws away the tick's answer
cache.** `policy/tick.go:146-156`: `commit` empties every program's cache and
sets the passing program's to this run's answers only. A call with
`Questions{}` (run's seed, `run/serve.go:131`; `verbs/apply.go:76`;
`verbs/check_admission.go:52`) on the same engine replaces the active's
thousands of cached answers with four, so the next tick is cold. Breaks
LAYER-5:226-228 ("a steady tick asks only about what changed") whenever such
calls interleave with ticks. Repair: merge a passing run's answers into its
program's cache under the 4 MiB cap instead of replacing it.

**M2. A read's finding is unbounded text in the verdict input, its cache key
and the evaluator's request.** `policy/inputs.go:215` puts `c.Reason` in
every read; `unworded` drops only `Brief` and `Prose`
(`policy/evaluator.go:152-160`), and the store has no length cap on a reason
(`store/check.go` caps only names, `MaxName`). The same class as rowan2's
probes 4 and 5 (the prose), which were fixed for briefs only. Repair: cap the
finding at a fixed size in `read` (as `storable` does), or drop it from the
input.

**M3. `Up` builds its op from unchecked strings.** `policy/verbs.go:53-67`
does not run `coordinator`/`unstorable` on `c.Actor` or `c.OpID`, so an actor
or op id the store refuses reaches it as `ErrInvalid`. Breaks L5-17 ("A verb
refuses, naming the field, any string the store would refuse before it builds
an op"). Repair: call `unstorable("up", ...)` on the actor and op id before
the snapshot.

**M4. A rows question of an unknown kind is dropped without a word.**
`policy/answer.go:145`: a `RowsQuestion` whose `Kind` is neither `work` nor
`read` is skipped, so its answer is absent and no failure names it: a caller's
typo reads as "policy said nothing". Repair: return a `Failure` of no version
(as the stale-versions case does, `:57-58`) naming the question.

**M5. A refused `Versions` is a `NoAnswer` the model does not have.**
`policy/answer.go:57-58` returns `Version -1` without trying any version when
`vs.Seq != snap.Seq`; `TickAlwaysAnswered` (`tla/Policy.tla:156-159`) allows
`NoAnswer` only when every tried version failed. Unreachable from run today
(it passes the snapshot's seq, `run/asker.go:32`), but a code path outside the
model. Repair: say in the model's header that a stale read is a caller error
outside the spec, or reload instead of refusing.

**M6. An activation leaves the previous version's judgement open.**
`policy/verbs.go:207` answers the judgements of the old active, `v` and the
built-in, not of the previous active; after active v2 and previous v1 both
fail and v3 is activated, v1 is no longer tried but its judgement stays open
until its deadline. The model does the same (`tla/Policy.tla:93`), so the two
agree; the doc's "Activating a version that runs clean answers the
judgement" (LAYER-5:244-245) reads as all of them. Repair: answer the open
judgement of `vs.Prev` too, in code and model.

**M7. The built-in's compile error is misreported after a million steps.**
`policy/engine.go:185` calls `errAt(th, 0, err)`; `inherit`
(`policy/tick.go:259`) compiles the built-in on the version's thread, which
may have run more than `CallSteps` steps that tick, so `errAt`'s test
`th.ExecutionSteps() >= 0+CallSteps` (`policy/engine.go:317`) replaces the
real message with "over the step budget". Repair: pass the step count read
before `SetMaxExecutionSteps` in `compile` as `start`.

**M8. The default catalog serves heavy cards on a bench with a flash model.**
`policy/default.star:124` lists `deepseek-flash` at tier heavy and `:146`
makes it the heavy bench model, so a heavy card dealt to a bench is worked by
a flash model; `checkCatalog` cannot see it (`policy/catalog.go:180`). It
defeats the boundary's "never a row whose rank is below the card's tier ...
below-tier work wastes hours and lands weak code" (LAYER-5:54) by labelling.
Marked a guess in the file. Repair: leave heavy without a bench model (a
`no-model` judgement, LAYER-5:980-981) until a heavy model is measured.

**M9. The layer doc no longer describes the code.** LAYER-5:124-200 (the
listing of the built-in: no `prefer`, a `verdict` without `retry`, `limits`
missing six keys, `timeouts` without `tick` and `consumer`, no `catalog`)
against `policy/default.star:33-149`, while LAYER-5:475 says "the design's
text unchanged"; LAYER-5:101 gives `width`'s default as "ahead = width",
code is 0 (`policy/default.star:53`); LAYER-5:259 says `set` refuses when "`up`
not run", code activates the first set (`policy/verbs.go:107-108`);
LAYER-5:104 lacks `tick` and `consumer`, required by
`policy/validate.go:260`; LAYER-5:223 batches a `tier` question that does not
exist (`policy/policy.go:26-29`). Repair: regenerate the listing from
`default.star` and fix the four table cells.

**N1. Check mode can un-cancel a deadline.** `policy/tick.go:151-154`: if the
deadline's `th.Cancel` lands between the `r.ctx.Err()` test and
`r.th.Uncancel()`, the cancel is cleared and the next call runs up to
`CallSteps` past the deadline before `one` (`:163`) stops it. Bounded by one
call. Repair: test `r.ctx.Err()` again after `Uncancel`.

**N2. `live` is decremented after the outcome is sent.**
`policy/tick.go:62-65`: `defer e.live.Add(-1)` runs after `r.ready <-
r.exec()`, so the child's `Live: e.live.Load() > 0`
(`policy/evaluator.go:313`) can see a finished run as live and the parent
kills a healthy child. Not reproduced (200 ticks on one child). Repair:
decrement before the send.

**N3. A refusal's subject is not the doc's.** `policy/answer.go:161` makes a
rows subject `"<card> <kind>"`, so a refusal prints `rows(c12 work)`; the
doc's form is `rows(c12)` (LAYER-5:86). Repair: one of the two.

**N4. Both rungs write one landing answer.** `policy/answer.go:206-207`: the
subject carries the rung but `a.Landing[lq.Stream]` does not, so in a check
the backed-up answer overwrites the flowing one. No caller reads check's
`Answers.Landing` today. Repair: key `Landing` by `LandingQuestion`.

## Promise gaps

- LAYER-5:293-295, guarantee 4: "A runtime error never stalls a tick. Every
  tick is answered by the active, the previous or the built-in version."
  The process engine answers no version on any tick after the child dies idle
  (B1), and none on any tick whose active version kills the child (S2).
- LAYER-5:233-237: "the whole tick is answered by the version active before
  it ... else by the built-in default." A child death fails the built-in
  without running it (`policy/evaluator.go:162-169`), S2.
- ENFORCED L5-5: "A tick has one deadline, TickTime, from input construction
  to the last answer." The evaluator path restarts the clock after its lock
  (S1).
- ENFORCED L5-16: "killed ... when it misses the deadline"; "The next request
  starts a new child" (LAYER-5:841). Not after a write error (B1).
- LAYER-5:241-243: "its safe default is already in force (the fallback)".
  For dealing it is not: run deals nothing unless the active answered
  (`run/asker.go:81-96`), S3.
- LAYER-5:226-228: "determinism makes the cache exact, so a steady tick asks
  only about what changed." An argument-free ask on the same engine empties
  the cache (M1).
- LAYER-5:879-880: "the wall-clock timing probes run only with `NOVA_LOAD`
  set". `TestFunctionalLoad` runs without it (S5).
- ENFORCED L5-17: "A verb refuses, naming the field, any string the store
  would refuse before it builds an op." Not `Up` (M3).
- LAYER-5:801-805 (rowan2 4 and 5): "the prose is no input". A read's
  `finding` is the same kind of text and is an input (M2).
- LAYER-5:297-299, guarantee 6: "`check` on a snapshot predicts the tick on
  that snapshot." Only for the walk's question set: `check` asks
  `FullQuestions` on both rungs (`policy/show.go:94`), run asks landing on
  `flowing` only and never `stitch` (`run/asker.go:51-73`), so a version can
  fail check on a subject no tick asks. Conservative, not unsafe; the doc
  should say "check is at least as strict as the tick".

## Model vs code

- **Child death (S2).** `Pick` (`tla/Policy.tla:110-114`) answers with the
  first usable version; `Failing` (`:119-122`) holds only versions reached.
  The evaluator's `killed` fails every tried version
  (`policy/evaluator.go:126-133`, `:162-169`), so the code produces
  `NoAnswer` and a `Default` judgement in states where the model has
  `Pick = Default` and no `Default` judgement.
- **Dealing on a fallback (S3).** Model and walk deal on any answer
  (`tla/Policy.tla:132`, `policy/trace_test.go:160`); the binary only on the
  active's (`run/asker.go:81-96`).
- **No active version and the first set (S4).** Not in the model
  (`tla/Policy.tla:49`, `:61-62`, `:74-81`); in the code
  (`policy/verbs.go:99-136`, `policy/answer.go:27-36`).
- **Stale versions (M5).** `NoAnswer` with nothing tried
  (`policy/answer.go:57-58`), outside `TickAlwaysAnswered`.
- **What the trace binds.** `TestTraceWalk` runs the local engine
  (`policy/trace_test.go:86`, `testEngine()`) on `FullQuestions` with the
  flowing rung (`:154`), while `breaks` is computed by `checkRun` on both rungs
  (`policy/trace.go:82-86`, `policy/show.go:94`). The evaluator (the
  binary's path, `cmd/nova-sprint/open.go:52`), run's question set and the
  concurrent-caller path are not traced. A version that fails only on the
  backed-up rung would be in `breaks` and still answer the walk's tick; the
  walk's `breaking` body (`policy/trace_test.go:54-57`) never produces one,
  so the gap is latent.
- **`WaitKeepsItsBound`** is checked of the model only; the ledger says so
  (L5-8). No code action implements `waitBound`/`startBound`.
- Matches I checked by hand: `Versions.tried` order and the de-duplication of
  previous == active (`policy/answer.go:27-36`) are `Pick`'s order;
  `Activate` answering active, `v` and `Default`
  (`policy/verbs.go:207`) is the model's `judgements'` (`:93`); `Judgements`
  opening one per failing version with none open (`policy/answer.go:82-90`)
  is `Tick`'s `judgements'` (`:130`); the early fallback never changing the
  answering version holds because `race` awaits in order
  (`policy/tick.go:83-95`).

## Simplifications

- `policy/policy.go:26-35`: `Arity` and `params` are two maps over one key
  set; one map of question to parameter list gives both (arity is the count).
- `policy/validate.go:359-361`: `dictOf` is a one-line wrapper of `dictWith`;
  and `checkLanding` (`:229-239`) hand-builds the optional `ci_wait` key that
  `dictWith`'s `optional` already supports (`dictWith(v, []string{"backed_up",
  "flowing"}, "ci_wait")`).
- `policy/validate.go:217-218`: `starlark.AsFloat` succeeds only for `Int`
  and `Float`, so `!isNum && v.Type() != "int"` never changes the result.
- `policy/answer.go:82`: `Judgements` takes a roster it ignores (`_
  roster.Roster`); every caller builds one for nothing.
- `policy/prefer.go:32-43`: the memo keys on `unsafe.SliceData` of the
  candidate tuple; the Go side already has the candidate set's identity
  (`candidateSet.digest`, `policy/inputs.go:147-148`); passing a key (or
  memoising in `index`) would drop `unsafe`.
- Two deadline machines: with the process evaluator in the binary, the
  in-process race (`policy/tick.go:74-129`: goroutine per version, early
  fallback, `AfterFunc` cancel, `late()`) runs inside a child that the parent
  also times and kills (`policy/evaluator.go:104-134`). If the child ran the
  versions in order, one per request, and the parent owned the one deadline
  and the kill, S1 and S2 would go and so would most of tick.go's
  concurrency. Glenn's call against the size budget: about 3,337 lines of
  non-test Go now (`cat policy/*.go` without tests), against the 1.5k the doc
  set (LAYER-5:526-527, :906-909).
- `policy/verbs.go:289-301`: `versions` reads every version one round trip
  at a time, and `Up`, `Set` (twice: `sameBody` and `setWrites`) and `List`
  each call it. A store call listing versions would do.

## Missing tests

- The evaluator child dying between requests, then a request: expects a new
  child and an answer (B1). None exists; the probe above fails it.
- Two callers on one process engine: the second's answer within TickTime of
  its own first byte (S1).
- A child killed by the memory cap with a clean built-in: expects the
  built-in to answer and no `policy:v0` judgement (S2).
  `policy/round5_test.go:153-154` asserts the opposite.
- A tick and a set with no version active, in the walk and in `TestVerbsFake`
  against the model (S4); `testSetFirst` covers the verb, not the trace.
- `FuzzAnswers`' "every answer used in domain" (LAYER-5:343-346) checks only
  widths and rows (`policy/validate_test.go:209-221`): order, reads,
  verdict, limits, landing, timeouts, prices and catalog answers that pass are
  not re-checked against their domains.
- The answer cache across an argument-free ask: a tick, a `Questions{}` ask,
  a tick, asserting the second tick is warm (M1).
- `Up` with an unstorable actor or op id (M3).
- A rows question with an unknown kind (M4).
- A long read finding through the evaluator (M2), as rowan2's prose probes
  do for briefs.
- L5-7 in CI (S5).

## Questions for the coordinator

1. Is "deal nothing unless the active answered" (`run/asker.go:81-96`,
   defect 53) the ruling for the whole system? If so, the layer's fallback
   is only for the argument-free numbers, and LAYER-5 "The tick's answer",
   guarantee 4 and `tla/Policy.tla`'s `Tick` should say so (S3). If not, run
   should deal on a fallback.
2. On a child death, should only the version in hand fail (S2's repair), or
   is "a dead child answers nothing" (`policy/round5_test.go:154`) a ruling?
   I could not find one in the doc.
3. Two writers open judgements of kind `policy` in group `policy:v<n>`:
   `policy.Judgements` (via `run/tick.go:68-79`) and the steps
   (`steps/judge.go:126-128`, `:149`). Does the store's group key keep
   `OneJudgementPerVersion` across both when they land in one tick? Outside
   this layer's files, so not checked here.
4. `landing`'s `rung` input: run always asks `flowing`
   (`run/asker.go:66`) and never asks `stitch`. Is the backed-up rung owed by
   layer 3, or should `check` stop asking it?
5. The default's heavy bench model (M8): a guess the coordinator wants, or
   should heavy go undealt on benches until measured?
6. Size: 3,337 lines against 1.5k. Is the in-process race kept now that every
   version runs in the child?
