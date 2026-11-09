# Actual setup walk trace mapping

The emitter (`TestSetupActualTrace`, modules/verbs/setup_trace_test.go) drives
the real verbs.runUp/setupStep/coordinatorStep code and observes its
SetupObserver events. A fake Setup owns the world of steps 1, 2 and 4 to 7;
step 3's world is the store's coordinator row (hooked in the fixture). This
binding proves finite observed walker executions, not production external
services or liveness.

## Explicit initial boundary

Initial world/answers are observed fixture state, verified is empty, pc=1,
phase=probe, asked counters zero for this execution; a pre-existing identity
does not imply an invented earlier question. SetupWalk.WalkInit is the
admitted initial condition; Setup.Init and Setup.Next remain unchanged.
SetupWalk.cfg checks the invariants and liveness under all 7-step initial
worlds, with and without the identity, and one bounded disruption.

## Trace schema and projection

Emit a TLA module SetupTraceLog defining TraceLog as a finite sequence. Every
record requires i (1-based), action, world, verified, answers, asked, pc, phase
and disruptions. Ask additionally requires q; Regress requires k.

| Field | Observed refinement |
| --- | --- |
| world | The fake's readiness over steps 1..7, sampled from its actual state. |
| answers | The identity, when the fixture's store holds one. |
| asked | Questions asked in this observed execution, not historical totals. |
| verified | Steps whose actual verify returned true in the current invocation. |
| pc, phase | The observed step and result: a false verify stays at the step in repair; a true verify moves on (step 7 gives 8/done); a completed act returns to probe. |
| disruptions | Injected regressions and handoffs recorded in this trace. |

Observer events map: `begin` to Init (first) or Restart; `verify` to Probe;
`act` to Do, only when the fake's world is then true; `store-only` (the
credential-less process stopping once step 2 verifies, UpStoreOnly) to
Handoff; an injected fixture regression to Regress(k). A step-7 refusal maps
to Settle. A changed world, answer, counter or position is never hidden by a
stutter. The emitter does not bind Ask (the identity is in the fixture) or
cancellation.

SetupTrace requires all projected fields and derives invariant monitors
itself. TraceInit assigns the observed first record, then TraceInitialAllowed
checks WalkInit as an invariant. TraceMatched is finite reachability of the
last record, not implementation liveness. At a refusal with last matched
index l, the first refused record is l+1.

## The workload steps (6 and 7)

world[6] and world[7] are bound to the production workload adapter
(cmd/nova-sprint/setup_workload.go) by TestWorkloadStepsMatchTheSetupModel,
over every state of a two-bench fleet: world[6] is every worker bench on the
bus and up in the store; world[7] is world[6] and every worker bench with
cores. A step-6 not-ready is a `*verbs.WorkloadNotReady`, the model's repair:
up acts once (InstallWorkers), and the second not-ready is the refusal naming
each bench. Step 7 has no act: its not-ready is the refusal (Settle). That
binding checks the step conditions, not a trace.
