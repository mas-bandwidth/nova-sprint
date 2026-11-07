## Verdict

The layer has one concrete minor defect in the reviewed scope: the built-in row selector reverses names within below-wanted ranks, breaking its documented deterministic tie-break. I did not establish another source defect in the policy implementation.

## Defects

- **Minor — fallback row ordering reverses names within each below-wanted rank.** At `policy/prefer.go:74`, `slices.Reverse(below)` reverses the entire candidate sequence. `candidateSet` supplies candidates sorted by ascending rank and then ascending name (`policy/inputs.go:72`, `policy/inputs.go:142`); reversing it correctly puts lower ranks later, but also reverses name order within each rank. When candidates at the wanted rank or above are absent, this contradicts the contract's “within a rank, by name” rule and changes the row chosen by the engine. Reverse the rank groups while preserving each group's name order, and add coverage with at least two candidate names at a below-wanted rank.

## Promise gaps

- Contract: `rows` lists “the ranks between the tier and the wanted one, highest first” and “within a rank, by name” (`docs/LAYER-5-POLICY.md:98`). For below-wanted candidates, `preferred` reverses the whole list (`policy/prefer.go:67`), so same-rank names become descending. The rank order is correct; the tie-break is not.

## Model vs code

The Policy TLA+ model covers version selection, fallback, judgements, and one abstract card state; it does not represent candidate rank or row ordering (`tla/Policy.tla:38`, `tla/Policy.tla:41`). The ordering defect is therefore outside that model's coverage, rather than a demonstrated model/code divergence.

## Simplifications

The default's Go `prefer` helper keeps ranking work linear in the candidate list and shares results for repeated candidate sets and wanted ranks. The implementation is compact; its whole-slice reversal is the only simplification found that loses a stated ordering detail.

## Missing tests

- Add a focused ordering test with multiple alphabetically named candidates in each of two below-wanted ranks. Assert higher rank first, ascending names within each rank, and room-first ordering without changing either tie-break.

## Questions for the coordinator

`Up` is separately described as bootstrap setup, outside the five `policy ...` verbs covered by “All are the coordinator's” (`docs/LAYER-5-POLICY.md:254-267`). Should the exported `policy.Up` API still reject a caller with `Coordinator == false`, or is that flag only meaningful for the `policy set` and `policy activate` verbs? The current setup wrapper supplies a coordinator caller after its prerequisite checks (`verbs/setup_runtime.go:267-268`); I did not treat the caller-supplied boolean as authentication or report this as a defect.

## Coverage inventory

- Production: reviewed all files under `policy/` that are not tests, including `default.star`; specifically the evaluator/cache/deadline path, input construction, validation, row preference, catalog and prices, show/check/diff, verbs, and trace projection.
- Tests: reviewed the main answer, compile, validator, catalog, verb, clock/evaluator, PostgreSQL-functional, trace, and breaker/cold-read suites for policy behavior and their defect coverage. The suites use `store.Fake`/hand-built snapshots for unit paths; PostgreSQL and subprocess coverage is marked functional in the test sources. No tests were executed.
- Contract/models: reviewed `docs/LAYER-5-POLICY.md`, the L5 entries in `docs/ENFORCED.md`, `tla/Policy.tla`, and `tla/PolicyTrace.tla`. The model abstracts card state and version lifecycle; it does not model row selection.

## Validation and coverage

- Parent-run shared Linux validation at exact base `43d849e29d0e4568ba6a1348a076a59259639938` used a fresh source archive (SHA256 `73b811e91414713188e0c24dbb6fdafa8580cd476d3e69104751b7c2e573788c`) on hetzner2, `GOMAXPROCS=4`. `go build -p 4 ./...` and `go vet -p 4 ./...` passed.
- Parent-run selected tests: `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` exited 1. Policy, run, verbs, cmd and dash results were respectively: **FAIL** (`TestDeadlineOnTheClock`, `policy/round5_test.go:26-27`, expected version -1 and 3 refusals, actual version 0 and 2 refusals), pass, pass, pass, pass. This is the shared base result, not a reviewer reproduction.
- Functional tests, services, fuzz, TLC, and per-verb help capture were not run / remain pending in the receipt. The review ran no Go commands or tests and did not duplicate the gates.
- Source review covered all non-test files under `policy/` and `default.star`, the layer contract, L5 ENFORCED entries, and `Policy.tla` / `PolicyTrace.tla`. Relevant tests for answer selection, compile/refusal behavior, catalog, verbs, clocks/evaluator, functional paths, trace and breaker/cold-read probes were inspected; other test details remain outside this pass. The model does not represent row selection.

### Final coordinator validation receipt

This postscript supersedes earlier statements that shared validation or help capture was pending. On the unchanged source base `43d849e29d0e4568ba6a1348a076a59259639938`, the parent ran Linux checks under a shared reservation with `GOMAXPROCS=4`: `go build -p 4 ./...` and `go vet -p 4 ./...` passed. `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` failed in policy's `TestDeadlineOnTheClock` (`policy/round5_test.go:26` and `:27`: expected version -1/three refusals, observed version 0/two refusals); run, verbs, command and dash packages passed. All 44 existing CLI verb/subverb/playbook help queries exited zero. These are parent-run results, not independent reviewer executions. Functional tests, live service setup, fuzzing and TLC were not run; source-review coverage limits above remain. The source archive SHA256 is `73b811e91414713188e0c24dbb6fdafa8580cd476d3e69104751b7c2e573788c`; the 53-file shared evidence manifest SHA256 is `49e12541e3756608ae7205d68b95f85105d510ca9b5e4c26ce003477209e3e3e`.
