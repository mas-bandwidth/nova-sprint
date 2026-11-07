# Cold review: layer 4 (edges) — v2r-l4-pro1

Cold read of the edges layer: `edge/**` (exec, argv, failure, bus, git, forge, worker), `cmd/nova-worker/**`, `docs/LAYER-4-EDGES.md`, `docs/SEAMS.md`, and the models `tla/LandEdge.tla`, `tla/Runner.tla`. Build/vet clean; unit tests of the layer pass under `-race`; one functional test is red (below).

## Verdict

The layer is structurally sound: the worker lifecycle (passes apart from jobs, lease-only pushes, retire/release, restart-adopt) and the landing ladder are well modelled, and the unit tier passes under the race detector. The single worst thing is that the only functional test that pins Guarantee 8 (harness inside a nova-reserve slot) is stale and now fails before it asserts anything, so the shipped worker's slot wiring has no passing guard; and the worker never sends `--slots <weight>` at all, so a card's weight is never conveyed to nova-reserve. No blocker defect was found in the hot paths, but three promises are either unguarded, dead, or weaker than their doc sentence.

## Defects

1. **[serious] `cmd/nova-worker/break_test.go` `TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot` is stale and fails red.**
   - Evidence: `go test ./cmd/nova-worker/ -run TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot` → `break_test.go:80: no report reached nova-sprint (exit 0, stderr "", sprint log "unhook --as r1\nhook ... \nbeat ... \nnext --as r1 --json\nnext --as r1 --json\n")`. The fake `next` response (break_test.go:59) deals a consumer in state `ready` (`"state":"ready","gen":1`), but since the stitch "deal is take" the worker no longer consumes `ready` cards: `planPass_internal` only launches `next.Working` (`edge/worker/pass.go:215-222`), so nothing is taken and no report is ever sent. The test therefore never reaches its real assertion (that `nova-reserve` was invoked, break_test.go:91-93); Guarantee 8 currently has no functional guard and the layer's `go test ./cmd/nova-worker/...` is red.
   - Why it matters: the test purports to prove the worker runs the harness inside a slot; it now proves only that the worker ignores `ready` cards. A regression back to running the harness outside a slot would go unseen.
   - Repair: deal the fake consumer in `working` (drop the `ready` array), and assert the recorded wall argv contains `nova-reserve run` (and `--slots`).

2. **[serious] the runner never sends `--slots <weight>`; `Job.SlotWeight` is a dead field.**
   - Evidence: `Job.SlotWeight` is declared (`edge/worker/worker.go:74`) and consumed only under `if j.SlotWeight > 0` (`edge/worker/harness.go:356-358`), but it is assigned nowhere in non-test code — the only assignment in the tree is `edge/worker/worker_test.go:305`. So `BuildWallArgv` never appends `--slots`, and every job runs at nova-reserve's default weight.
   - Why it matters: LAYER-4 step 4 is "Run under `nova-reserve run --slots <weight>`", and the width-vs-slots split ("width caps working; slots cap running programs") depends on a job claiming a weight. With the field dead, a heavy card claims the same as a light one, and the slots accounting the `Runner.tla` `SlotsWithinCapacity` invariant models does not reflect the real weight.
   - Repair: set `SlotWeight` from the take receipt or the active policy (or delete the field and document the default weight explicitly).

3. **[minor] the probe's judgement opener is dead: `OpenJudgement` calls a verb the binary does not have.**
   - Evidence: `edge/worker/probe.go:58-69` calls `r.Sprint.OpenJudgement`, which runs `nova-sprint judge-open ...` (`edge/worker/verbs.go:231-234`). There is no `judge-open` verb in `cmd/` or `verbs/` (only `judge`), so the call always fails with "unknown verb" and every failed probe appends "and its judgement could not be opened: unknown verb"; no probe judgement is ever created. The doc itself records this ("Judgement errors (30): nova-sprint v2 has no judge-open verb").
   - Why it matters: the comment promise "A failure opens the row's probe judgement" (probe.go:25) is not kept, and the refusal text is misleading.
   - Repair: implement a `judge-open` verb (layer 6) or open the judgement through an existing engine path.

4. **[minor] `Runner.QueryEdge` / `evalEdgeQuery_internal` is unreferenced dead code.**
   - Evidence: `edge/worker/runner.go:762-791` defines `QueryEdge` and `evalEdgeQuery_internal`; a whole-tree grep finds no caller outside the package. It reads `nova-sprint next --json` and job records to answer `EdgeRequest`s, a second pathway to store state the worker otherwise reaches only through verbs.
   - Why it matters: it is a 30-line special-case surface (the "no special cases" rule) that nothing exercises, and it holds a `Sprint` read that is not part of the deal-to-report lifecycle.
   - Repair: delete it and its `strings`/`card` imports until layer 6 actually dispatches edge queries through it.

5. **[nit] `runWallCmd_internal` special-cases exit 1 + "nova-reserve:" but explicitly excludes the `HELD` form.**
   - Evidence: `edge/worker/runner.go:1161-1163`: `if res.ExitCode == 1 && strings.Contains(combined, "nova-reserve:") && !strings.Contains(combined, "nova-reserve: HELD") { return out, ErrNoSlot }`. If `HELD` is how nova-reserve reports that the `--wait` expired with no free slot, those runs fall through to be classified as harness outcomes (`fail: no verdict`) instead of `ErrNoSlot`, violating "No slot within the wait is no start yet, never a failure."
   - Why it matters: a miss here turns a "wait for a slot" into a card `fail` (a bounce) or a fault.
   - Repair: confirm nova-reserve's exact no-slot wording and include the `HELD` form in the `ErrNoSlot` branch (see Questions).

## Promise gaps

- `docs/LAYER-4-EDGES.md` step 4: "Run under `nova-reserve run --slots <weight>` — `--slots` is never emitted; the weight field is never set (Defect 2).
- `edge/worker/probe.go:25` "A failure opens the row's probe judgement" — no such verb exists; the call always fails (Defect 3).
- Guarantee 8 "A harness runs only inside a nova-reserve slot, freed when it ends" — structurally still true (every harness runs through `nova-reserve run`), but the only functional guard test is broken (Defect 1).
- Guarantee 10 "A report is recorded at most once per result id" — kept: `FormatResultID` is stable per `(epoch, consumer, gen)`, and `rec.Op != req.ResultID` keeps the job at `reporting` to resend (runner.go:726-729).

## Model vs code

- `LandEdge.tla` models an absent CI as a terminal refusal (`ci # "green" → Finish(s, ci)`, LandEdge.tla:124), whereas the code re-asks `ci_absent` until the request's `Wait` and then answers `no-ci` (run/edges.go; documented in LAYER-4 "Defect 40"). This is a documented simplification, not a contradiction; the code is a superset of the model.
- `Runner.tla` `Clean` requires `jobDir[c].pushed` (Runner.tla:196-200), but the code's `cleanJobDir_internal` removes a directory whenever `PushedHead == ""` (runner.go:737-746 skips the origin-head check) — so a recorded fault/fail with nothing pushed can be cleaned although the model forbids it. Benign (there is nothing to preserve), but a divergence.
- `Runner.tla` has no refused-report action; the keep-and-resend rule has only a unit test, no model (self-documented in LAYER-4's stitch).
- `Runner.tla` `Take` is strongly fair; the code's `planPass_internal` takes every `working` card with no job directory each pass, so the fairness assumption holds.

## Simplifications

- `Job.SlotWeight` is a full field used by `BuildWallArgv` but never set — dead special-casing (Defect 2).
- `Runner.QueryEdge`/`evalEdgeQuery_internal` implement an edge-query seam nobody calls — dead code (Defect 4).
- `CLISprintClient.OpenJudgement` is a verb call with no implementing verb — a dead call site kept for the future (Defect 3).

## Missing tests

- No test pins that `--slots <weight>` appears in the wall argv for a real job (which is why Defect 2 slipped through: grep shows only `worker_test.go:305` touches `SlotWeight`).
- No unit test parses `runWallCmd`'s `ErrNoSlot` detection from a textual `HELD`/no-slot output; the existing `ErrNoSlot` tests inject the error directly from a fake wall (`coldread2_test.go:238-240`, `live_test.go:264-265`).
- No test covers a work card whose `result.json` says `{"result":"fix"}` (the store refuses `fix` for work; the report would loop refusals to the quarantine path) — if that is intended, it should be asserted.
- No test asserts that a failed probe does not append the "its judgement could not be opened" text (i.e. that `judge-open` exists).

## Questions for the coordinator

- What does `nova-reserve run --wait <d>` print when the wait elapses with no free slot — is the exact line `nova-reserve: HELD`? If so, `edge/worker/runner.go:1161-1163` maps the genuine no-slot case to a harness fail, not `ErrNoSlot`.
- Where is a job's slot weight supposed to come from? No seam (receipt, policy, roster) currently carries per-tier cores, so `SlotWeight` has no source; should it be policy data, or is the reserve default the intended weight?
- Is `Runner.QueryEdge` intended to be a public seam that layer 6 (`run/edges.go`) should call? It currently has no callers; if it is dead, may it be removed?
