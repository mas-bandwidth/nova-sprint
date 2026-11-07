# Layer 6 cold review — v2r-l6-pro2

Reviewed against fixed base `43d849e29d0e4568ba6a1348a076a59259639938`.

## Verdict

Layer 6 has one confirmed failure-handling defect in the run loop and one unresolved model-coverage gap. The run loop silently converts a failed bus-presence read into “nobody present”; after the configured silence threshold this can mark live rows down and later withdraw unacknowledged work. The separate crash-recovery question is whether L4's stable re-query contract suffices for every carried inbox item.

## Defects

1. **[serious]** `run/serve.go:175-181` discards the error from `Bus.Who` and returns a nil presence list. The tick then retains old `Seen` timestamps (`run/tick.go:158-160`) and the steps treat that stale timestamp as proof of absence: `steps/limits.go:51-65` issues `Gone` after `RowSilentGone`, and `steps/holds.go:29-41` can later withdraw unacknowledged consumers. A prolonged `nova-bus who` outage can therefore mark present rows down and trigger redeals without an observed absence; retain presence as unknown and skip absence-driven transitions when the read fails (or propagate the read error so the tick does not plan those transitions).

I did not reproduce hostile input or run tests or gates; the shared build/vet/unit receipt is parent-run and is not evidence from this review.

## Promise gaps

> “The carry is the engine's, FIFO: a burst of moves costs each a tick or two and is never reordered.” (`docs/LAYER-6-RUN.md:62-65`)

The per-tick implementation retains carry in `Ticker.inbox` (`run/tick.go:272-300`), but the restart contract does not say how this volatile ordering is preserved across process loss. The model explicitly excludes that case (`tla/Run.tla:24-28`), so the FIFO and restart promises have no combined crash/recovery argument in the reviewed evidence.

> “A restart of `run` loses no state, report or push; run holds nothing that is not in the store.” (`docs/LAYER-6-RUN.md:81-83`, and guarantee 6 at `docs/LAYER-6-RUN.md:407`)

`Ticker` initializes an empty inbox and in-flight map on cold start (`run/tick.go:90-91`); edge results are drained from `Edges.results` by `Results` (`run/edges.go:63-69`) and then held in the in-memory inbox. The source relies on re-query/replanning rather than durable storage for this work. The model itself says it does not prove the external fact re-query assumption (`tla/Run.tla:24-28`). This is a proof/acceptance gap rather than a confirmed loss: I did not establish that a particular result cannot be reissued after a crash.

> “A row absent from `Seen` is unknown, and nothing fires on it.” (`steps/limits.go:44-45`)

At the run boundary, a failed `nova-bus who` call is represented as an empty successful presence set (`run/serve.go:175-181`), while `Seen` retains prior timestamps (`run/tick.go:158-160`). This is not the “unknown” case described by the steps comment: the old timestamp keeps aging, allowing `Gone` and subsequent silence actions to proceed. No run test injects a failing `Who` result (`run/tick_test.go:51-65` defines the fake bus with no `Who` error field; the inspected run tests exercise send failure but not presence-read failure).

## Model vs code

`Run.tla` does not model recovery of pre-crash FIFO history and requires external layer-4 facts to be re-queryable by stable `(kind, card, sha)` (`tla/Run.tla:24-28`). The model evidence records a bounded `RunFailover` run with one crash, but explicitly says no pre-crash FIFO preservation is claimed and that the external re-query assumption belongs to L4 tests (`tla/LAYER6-EVIDENCE.md:28-33, 95-100`). The run implementation carries inbox items in memory (`run/tick.go:272-300`) and starts a new ticker with empty carry/in-flight state (`run/tick.go:90-91`). Thus the model's stated scope is narrower than a literal reading of the restart guarantee; no trace in the inspected model establishes preservation of a carried edge result's FIFO position. This review did not run TLC or inspect all L4 edge implementations, so it does not claim a counterexample. The live run path also diverges from the `Seen` unknown-row rule: `present` turns any `Who` error into an empty slice (`run/serve.go:175-181`), leaving the previous timestamps in `Ticker.seen` (`run/tick.go:158-160`). On the next planner call, `steps/limits.go:51-65` has no representation for “presence lookup failed”; old observations are treated as elapsed silence. That failure mode is absent from `Run.tla` and the run tests inspected here.

## Simplifications

No simplification recommendation was supported by the bounded source pass.

## Missing tests

The model and tests need a crash/restart case with multiple carried inbox items and an edge result drained just before restart. It should assert either durable FIFO restoration or stable re-query/replanning of every item, with no omitted outcome and no duplicate state transition. Existing per-tick FIFO evidence is in `steps/paths_test.go:200-235`; it does not cover process restart. Add a `Who`-failure case at the run boundary: when a row was previously seen and its last-seen time crosses `RowSilentGone` during an unavailable presence read, assert the tick does not write `Gone` or withdraw work on that unavailable observation.

## Questions for the coordinator

- Is FIFO order required to survive a `run` process restart, or does “never reordered” apply only between live ticks?
- Which layer-4 test proves a drained but not yet accepted result is reissued after run crashes, and does that cover stop acknowledgements as well as landing results?
- Should the Layer 6 model add an explicit recovery abstraction, or should the restart/carry promise be narrowed to store-derived work plus stable external re-query?

## Coverage and validation

The follow-up expanded the pass across the run loop, chooser and restore code, edge dispatch, pusher/notices, roster adapter, `next`, dashboard Go production files and browser assets, broad verb/CLI/setup source, and the Layer 6 run/clear/stop evidence and trace-mapping documents. I read the cited docs, the run model and selected model evidence; I did not read every verb, every CLI/setup file, all dashboard tests, or every Layer 6 model/witness. This is not a complete file-by-file certification. The run tests checked for this finding include the fake bus and `TestTickRaisesSeenForAHookedRow`; the fake's `Who` always succeeds, and no inspected run test injects a failed presence read (`run/tick_test.go:51-65, 134-175`). The existing bounded crash model is useful context but does not model a `Who` error. I did not run `go build`, `go vet`, unit tests, or TLC because the coordinator owns shared validation and the card forbids duplicate gates. No shared validation receipt was available to this review at writing.

### Final coordinator validation receipt

This postscript supersedes earlier statements that shared validation or help capture was pending. On the unchanged source base `43d849e29d0e4568ba6a1348a076a59259639938`, the parent ran Linux checks under a shared reservation with `GOMAXPROCS=4`: `go build -p 4 ./...` and `go vet -p 4 ./...` passed. `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` failed in policy's `TestDeadlineOnTheClock` (`policy/round5_test.go:26` and `:27`: expected version -1/three refusals, observed version 0/two refusals); run, verbs, command and dash packages passed. All 44 existing CLI verb/subverb/playbook help queries exited zero. These are parent-run results, not independent reviewer executions. Functional tests, live service setup, fuzzing and TLC were not run; source-review coverage limits above remain. The source archive SHA256 is `73b811e91414713188e0c24dbb6fdafa8580cd476d3e69104751b7c2e573788c`; the 53-file shared evidence manifest SHA256 is `49e12541e3756608ae7205d68b95f85105d510ca9b5e4c26ce003477209e3e3e`.
