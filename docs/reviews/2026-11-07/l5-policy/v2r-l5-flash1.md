# Layer 5 policy: cold review by rowan (haiku), v2r-l5-flash1

Base 43d849e2. Scope read: policy/clock.go, tick.go, evaluator.go, evalmem.go, validate.go (rows, width, limits, landing, verdict helpers), engine.go (call, TickTime), default.star, docs/LAYER-5-POLICY.md (the list, the question table, the default), docs/ENFORCED.md (L5 rows and section 6). Build and vet are green (`go build ./... && go vet ./...`, exit 0). `go test -count=1 ./policy/` passes (ok, 4.5 s). Not read, so not reviewed: answer.go, inputs.go, prefer.go, verbs.go, catalog.go, policy.go, show.go, diff.go, the TLA+ model, and most test files. Time-boxed to 15 minutes.

## Verdict

The layer is structurally careful: one deadline per tick, a per-call step budget, a validator that names line and version, and a fallback that never stalls. The worst thing I found is in the child-process evaluator: when a send to a child that has already died fails, `remote` returns the error without stopping the child, so the dead child stays installed and every later tick fails the same way until the process restarts. It is a serious liveness defect, not a safety one, and the repair is one line. I did not run it; the finding is from reading the code path.

## Defects

1. **serious**: `policy/evaluator.go:114-116` (and `:188`). When `ev.send` returns an error (the encode fails), `remote` returns `killed(failing, err.Error())` without calling `ev.stop()`. `ev.cmd` stays non-nil, so the next request skips `startChild` and writes to the same broken pipe, and fails again. Every later tick is answered by the fallback with the same error. Repair: call `ev.stop()` before returning in that branch, as the other error branches do (`:123`, `:127`, `:132`). To reproduce: start a tick so the child exists, kill the child while idle, run two ticks.
2. **minor**: `docs/LAYER-5-POLICY.md:101` (the `width(row)` row) says the default answers `ahead = width` ("2 x width held"). `policy/default.star:53` returns `"ahead": 0` with the comment "no deal-ahead ... (defect 74)". The code is the newer decision; the doc is stale. Repair: change the doc default column to `ahead = 0`, citing defect 74.

## Promise gaps

- `docs/LAYER-5-POLICY.md:101`: "work = width, ahead = width (PRIORITY D21: 2 x width held)". Code (`default.star:53`): `ahead` is 0. See defect 2.
- `docs/LAYER-5-POLICY.md` (the tick's answer): "a version over it fails and the fallback answers" with the deadline "from input construction to the last answer". `tick.go:70-81`: the fallback starts at `fallbackAfter` from the tick's first byte, not on failure, and `evaluator.go:105` gives the child a budget counted from the same start. Consistent with the doc's "from input construction", but the doc does not mention the early start. Doc gap only.

## Model vs code

Not checked. I did not open `tla/Policy.tla`, so I cannot say whether the `ev.stop()` path in defect 1 is covered by any witness. The ENFORCED rows L5-4 and L5-5 cite `invariant Policy.*` and `witness Policy*` names; I did not verify those names exist.

## Simplifications

- `evaluator.go:104-134`: the `remote` select has four exits, each with its own `ev.stop()` decision. One rule (stop the child on any error) would make defect 1 hard to repeat.
- `clock.go:27-40`: `deadline` starts one goroutine per tick that waits on the clock or the context. Fine, but one timer could serve every version of a tick.

## Missing tests

- No test was found (not searched in full) for a child that dies while idle, followed by a tick. Defect 1 would need one.
- The width default's `ahead` value is not pinned by a test I looked at; defect 2 would not be caught by the suite.

## Questions for the coordinator

- Is the `ev.stop()` omission on send error deliberate (to keep a child warm after a transient write failure)? I read it as an oversight, since the child has no way to resume a half-written request.
- Is `docs/LAYER-5-POLICY.md:101` meant to be the default, or the older v1 value? Which should be fixed?
