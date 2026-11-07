# Stopped-run contract delta

This extends Run.tla for the stopped-service ruling 1794017426900. Historical
model and 83-trace receipts retain their original checked source/spec hashes.
These are bounded contract-model results. Four archived-source finite traces
now match separately; the final integrated source/CI gate remains open.

The planner action is disabled after observing stopped. A running observation
may race a later Stop, so accepted tick writes additionally require the store's
existing atomic AtSeq condition. `tickStopSafe` monitors newly accepted tick
appends, not historical events or refusal/cursor receipt rows. Reports remain
independent operations. Stop preserves pending work/carry, and the pusher can
continue delivering already committed events. Completion is promised only if
the service eventually remains running, under the existing fairness/stability
assumptions. Stop reason text and the board presentation are outside this model.

`observedFresh` abstracts the equality of the latest folded snapshot sequence to
the current physical store sequence. `CaptureFence` saves that value once for a
new pending operation; `sequenceFresh` and `fenceFrozen` represent its saved
AtSeq. Every modeled physical append, including refusal and cursor metadata,
invalidates both freshness maps. A later ReadControl only refreshes the observed
snapshot and cannot replace the captured fence. This is different from card-gen
freshness. A frozen uncommitted unknown attempt may stutter and retry unchanged;
an unknown outcome that already committed and later Replay are outside this
model. The actual byte-identical retry test remains separate until trace-bound.

The model assumes the folded stop observation is accurate. New RunTrace requires
actual Engine.state observations and raw physical sequences so a stop lost by a
later ordinary delta is refused by the projection. It does not substitute a
fresh authoritative snapshot for a broken Engine fold. The numeric trace wrapper
also verifies captured expectedSeq against the actual receipt's predecessor seq.
External edge dispatch is not atomically fenced with Stop; its existing
commit-dependency guarantee does not establish no-send-after-Stop.

## Checked new cases

All four used the pinned TLC jar, two workers and a nova-reserve slot on
hetzner2. Raw logs and exact checked module/config hashes are retained in
`layer6-evidence/stopped-contract/results-stop.json`.

| Case | Result | Generated / distinct |
| --- | --- | --- |
| RunStopStart | safety and conditional liveness pass | 22,704 / 5,704 |
| RunInitiallyStopped | safety and conditional liveness pass | 5,352 / 1,584 |
| RunStoppedTickGateDropped | expected NoTickAfterObservedStop violation | 66 / 46 |
| RunStoppedFenceDropped | expected NoAcceptedTickWhileStopped violation | 672 / 327 |

The first reversed trace has Take → Hook → ReadControl(stopped) → Fold; removing
the planner guard is the only fault. The second has ReadControl(running) → Stop
→ Fold → Select → CaptureFence(stale) → Apply while stopped. No report/card-gen
change is invented. These correspond to the actual pre-planner stopped check and
the AtSeq appended once before Apply in run/tick.go. They do not prove the rest
of that source. Exact archived-source finite binding and its limits are recorded
in `layer6-evidence/stopped-traces/README.md`.

## Existing Run regression cases

The changed Run family requires its existing three positives and nine reversed
cases to be checked again. All twelve completed with their expected outcomes: three positive passes and
nine intended invariant/temporal counterexamples. RunReports first reached the
runner's 120-second timeout; that raw result is preserved as RunReports-timeout.log
and is not a pass. The identical module/config subsequently passed in 211.207s
with a separately retained runner changing only its timeout to 600s. That is a
bounded functional model result, not timing acceptance. Together with the four
new checks there are five positive passes and eleven expected negative results.
Exact regression and retry hashes are in results-regression.json and
results-reports-longer.json, with a 33-file source manifest. Setup, Clear, Install and handoff model
sources are unchanged and their historical cases were not rerun.
