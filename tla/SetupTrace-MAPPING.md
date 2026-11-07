# Actual setup walk trace mapping

The emitter must drive real verbs.up/setupStep code and observe real adapter
calls/results. Fake external dependencies own the world and durable-answer
fixture; they are not a TLA transition simulator. This binding proves finite
observed walker executions, not production external services or liveness.

## Explicit initial boundary

Trace begins at the actual under-lock walk after the readiness prefix. Prefix
and handoff lease ordering belong to a separate SetupHandoff binding. Initial
world/answers are observed fixture state, verified is empty, pc=1, phase=probe.
Asked counters and key-sink history start at zero/empty for this execution;
pre-existing answers do not imply invented earlier prompts. SetupWalk.WalkInit
is a new, explicitly admitted initial condition. Setup.Init and Setup.Next
remain unchanged. SetupWalk.cfg checks invariants and model liveness under all
8-step initial boolean worlds and all subsets of four initial answers, with
one bounded disruption. That is model evidence, distinct from actual traces.

## Trace schema and projection

Emit a TLA module SetupTraceLog defining TraceLog as a finite sequence. Every
record requires i (1-based), action, world, verified, answers, asked, pc, phase,
disruptions and keySinks. Ask additionally requires q; Regress requires k.

| Field | Observed refinement |
| --- | --- |
| world | Full fake dependency readiness function over steps1..8, sampled from its actual state. |
| answers | Actual durable fixture answers among coordinator/dashboard/services/key. |
| asked | Per-question prompt/save counts in this observed execution, not historical totals. |
| verified | Steps whose actual Verify returned true in the current under-lock invocation. |
| pc, phase | Actual callback step and return: false Verify retains step in repair; true Verify moves to next step/probe (step8 gives9/done); completed Act returns to same-step probe. |
| disruptions | Number of actual injected regressions/stops recorded in this trace. |
| keySinks | Actual observed secret transport destinations within this execution; never secret bytes. |

A successful Verify maps Probe; a failing readiness Verify also maps Probe
with phase repair. Successful Act maps Do only when the observed dependency
state satisfies that action's postcondition; do not set world true merely
because Act returned nil. Durable answer events map Ask and retain their actual
order within Act. An actual fixture regression maps Regress(k). Actual process
stop maps Crash; a following actual inner invocation maps Restart. Acquisition
is never mislabeled Crash. A completed walk's next invocation may also Restart.

Unchanged projections may use Stutter only for genuinely hidden callbacks or
read-only transport steps. A changed world, answer, receipt, counter or walk
position cannot be silently hidden. The first emitter checkpoint uses already
saved answers, a stable ready step1 prefix, random other world bits and actual
repairs/regressions/repeated invocations. It therefore does not bind Ask,
secret transports, cancellation, or two-child handoff effects.

## Contract gaps and refusal diagnosis

Setup.Need(6) unconditionally requires services and key. Real services=none
requires no key. The first trace uses service-requires-key only: never fabricate
Ask(key) to fit none. A separate explicit conditional-obligation model extension
is required before covering that supported branch. Also, an Act that returns
before external readiness is true cannot be forced into Do; preserve the first
refused transition and diagnose whether the model or contract needs correction.

SetupTrace requires all projected fields and derives invariant monitors itself.
It uses unchanged Setup actions (or explicit stuttering) at every matched step.
TraceInit assigns the observed first record, then TraceInitialAllowed checks
WalkInit as an invariant; invalid initial state cannot pass by yielding zero
initial states. TraceMatched is finite reachability of the last record, not
implementation liveness. At a temporal refusal with last matched index l, the
first refused record is l+1. Malformed records/initial conditions fail their own
invariants or parsing and must not be reported as a behavioral counterexample.
