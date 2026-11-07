## Verdict

Layer 5 is mostly coherent across its validators, fallback flow, and `Policy.tla` model, but the process evaluator does not enforce the documented one-deadline bound from the tick's first byte: request serialization and the evaluator grace can extend the response window. This is the most consequential issue because a slow evaluator can hold the tick past the policy's stated deadline.

## Defects

1. **serious — process evaluation can return after the tick deadline.** At `policy/evaluator.go:105`, the remaining budget is computed before `send` serializes the request; `policy/evaluator.go:114` then performs that send, and `policy/evaluator.go:129` starts a fresh wait for the stale budget plus `evalGrace`. Thus serialization time is added to the original budget and a nonresponsive child may be awaited for a further 100 ms after it; a large snapshot can make the first overrun significant. This conflicts with the contract that the tick deadline is counted from its first byte and the caller stops waiting at it. Carry an absolute deadline into the child and make the parent stop awaiting policy answers at that same deadline, reserving any grace only for cleanup that does not delay the tick.

## Promise gaps

- The doc says, “The deadline and the early fallback are on the engine's `Clock`, counted from the tick's first byte” (`docs/LAYER-5-POLICY.md:815`). The in-process path accounts for input construction when setting its waits, but the process path computes a relative budget before gob encoding and then waits that full duration plus grace after encoding; see the defect above.

## Model vs code

`Policy.tla`'s `Set`, `Activate`, `Pick`, and `Tick` correspond to the version lifecycle and fallback behavior in `policy/verbs.go` and `policy/tick.go`; the trace refinement includes `everActive` and checks projected walks. The TLA+ model abstracts elapsed time and process transport, so it does not expose the evaluator deadline overrun described above.

## Simplifications

No clear policy-layer special case with correctness or maintainability cost was established from this cold review. The embedded `prefer` helper is a narrow optimization for ordering candidate rows and preserves the documented ordering.

## Missing tests

- Add a process-engine test that makes request serialization consume a material part of the tick budget and verifies the result is refused by the original deadline, not by a later `remaining budget + evalGrace` wait. Existing `TestFallbackCountsFromTheFirstByte` exercises the local engine's clock accounting; `TestFunctionalEvaluator` covers a child that runs past its deadline but does not check the parent-side serialization interval or return-time bound.

## Questions for the coordinator

- Should the evaluator grace be limited to child cleanup after the tick has already returned, or may a policy tick's caller wait beyond its declared deadline to collect a failure receipt?

## Review scope and validation

- Reviewed the layer 5 contract, enforcement ledger rows L5-1 through L5-17, all production files under `policy/`, `policy/default.star`, the policy package test inventory and relevant deadline/evaluator tests, `tla/Policy.tla`, and `tla/PolicyTrace.tla`.
- Did not run `go build`, `go vet`, or `go test`: the parent is running the shared Linux validation receipt for the fixed base, and JOB.md prohibits duplicate gates. No Studio tests were run. No model checker was run as part of this card.
- The finding is a source-path analysis; no timing reproduction was run. The defect's lines are from fixed base `43d849e29d0e4568ba6a1348a076a59259639938`.

### Final coordinator validation receipt

This postscript supersedes earlier statements that shared validation or help capture was pending. On the unchanged source base `43d849e29d0e4568ba6a1348a076a59259639938`, the parent ran Linux checks under a shared reservation with `GOMAXPROCS=4`: `go build -p 4 ./...` and `go vet -p 4 ./...` passed. `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` failed in policy's `TestDeadlineOnTheClock` (`policy/round5_test.go:26` and `:27`: expected version -1/three refusals, observed version 0/two refusals); run, verbs, command and dash packages passed. All 44 existing CLI verb/subverb/playbook help queries exited zero. These are parent-run results, not independent reviewer executions. Functional tests, live service setup, fuzzing and TLC were not run; source-review coverage limits above remain. The source archive SHA256 is `73b811e91414713188e0c24dbb6fdafa8580cd476d3e69104751b7c2e573788c`; the 53-file shared evidence manifest SHA256 is `49e12541e3756608ae7205d68b95f85105d510ca9b5e4c26ce003477209e3e3e`.
