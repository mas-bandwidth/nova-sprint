# Fixed-seed Layer6 trace gate

`check-layer6-traces.py` emits traces from the current implementation, then checks
all 73 inputs through the current shared `check-trace.sh` (84 before the RunClear
cases were withdrawn: see "After the run rewrite" at the end). Run it only under an
authorized nova-reserve Linux reservation. It does not acquire another reservation
inside an existing CI job. Workflow ownership belongs to the main integration.

The emitter requires a clean Git checkout and an output directory outside it.
It archives all tracked source bytes before execution, records HEAD and per-file
hashes, and verifies the source and HEAD again afterward. Use an isolated checkout
without concurrent writers. The executing helper must match the checkout helper.
All Go emitter invocations use race detection, `-count=1`, and two workers.
Inherited NOVA_*TRACE* knobs are removed; each case supplies its own seed/output.

```
python3 /checkout/tla/check-layer6-traces.py --mode emit --repo /checkout --out /var/tmp/l6-traces
```

On the authorized local TLC bench (vision or hetzner2), transfer the complete output
without modifying its contents, then use the exact retained helper:

```
python3 /var/tmp/l6-traces/spec/check-layer6-traces.py --mode check --out /var/tmp/l6-traces
```

`--mode all` combines those steps on a bench authorized for both. The caller wraps
these commands with nova-reserve using that host's configured capacity. The Go
executable can be selected explicitly with `--go`. Output paths are never reused;
failed output remains inspectable. Timeout kills the job's process group.

The fixed inputs are three Run report schedules; one Clear case; ten Clear
notice/Clear/Push walks; one Setup walk; none/configured SetupChoices walks;
64 installer attempts; and successful/stale-epoch handoff compositions. Their
refinement limits remain those in the corresponding mapping documents. Passing
this gate is finite execution evidence, not arbitrary implementation proof.

Each check uses an isolated input directory and a spec directory without sample
TraceLogs. Missing cases, mismatched artifact bytes, missing raw logs, TLC warnings
or errors, a refusal, timeout, or inconsistent record count fail the gate. Raw and
harness hashes are recorded before reporting a failed case. After all checks the
input/spec/emission hashes are verified again. `check-results.json` preserves every
attempted verdict, including the first failure. The shared checker still uses its
pinned jar; no alternate download pin is introduced here.

The eight focused Linux tests cover false success without a raw log, warning/error
rejection, missing cases, inherited trace-knob removal, and descendant termination
on timeout, approved CI host/runner routing, and explicit stopped-case seed/mode selection. They use benign fake commands and do not execute TLC. Full integration
requires running emission and all 84 checks at the exact integrated commit; unit
checks alone do not satisfy that gate.


Rowan's7278111 ruling additionally authorizes the existing amd GitHub CI benches.
Read-only hostname/service checks establish b1 runs amd-1-nova-sprint-1 and b3
runs amd-3-nova-sprint-1. The helper admits only those exact host/RUNNER_NAME pairs
with GITHUB_ACTIONS=true. These environment values are routing markers, not a
security boundary or independent authorization. Local amd TLC remains disallowed;
vision/hetzner2 local routing is unchanged and Studio remains prohibited. The
caller still acquires the approved reservation and installs the pinned runtime.

The stopped-service extension adds `run-stop` to the three seeded Run cases. It
uses seed 0 and an explicit `NOVA_RUN_TRACE_STOP=1`; all inherited NOVA trace knobs
are cleared before each emitter. That case records Stop, an ordinary worker
report between stopped scheduler calls, and Start from actual Engine snapshots.
The suite now has 84 identities. Historical 83-case receipts retain their original
scope; this helper change requires a fresh integrated-source emission/check before
claiming current full-suite acceptance.


## After the run rewrite (f697b05f; this branch, 2026-11-06)

The run core the Run fixtures were bound to (Engine, Scheduler, the old pusher)
was deleted. `TestEmitRunTrace` (run/run_trace_test.go) is rewritten on
`run.Ticker` and `run.Pusher` with the same case names, seeds and knobs:
`run-0`, `run-1`, `run-2` (`NOVA_RUN_TRACE_SEED`) and `run-stop`
(`NOVA_RUN_TRACE_STOP=1`), written to `NOVA_RUN_TRACE_PATH`. What changed in the
mapping, and why, is "The run.Ticker binding" in RunTrace-MAPPING.md:
RunTrace.cfg's Budget is 1 (was 2); RunTrace.tla is unchanged; CaptureFence is
now the view seq in the op's id (the code sends no AtSeq); a failed send is
Stutter. `NOVA_RUN_TRACE_MIDSTOP=1` emits the Stop-between-fold-and-apply case,
which TLC refuses at its Apply (the code accepts a tick op after a Stop); it is
evidence of that finding and not a gate case.

The eleven RunClear cases (`clear`, `clear-walk-0..9`) are withdrawn and owed:
their emitters (TestEmitClearTrace, TestEmitClearActionWalk) went with the old
pusher, and `run.Pusher` has no clear summary or old-backlog suppression for
RunClear's Prepare/Scan/Summary to bind. The suite is 73 identities: run 4,
setup 1, handoff 2, choices 2, install 64.

Checked on hetzner2 at d8d7bd29 (2026-11-06): `run-0` TRACE OK steps=130,
`run-1` 112, `run-2` 109, `run-stop` 148 (emitted by this helper with -race).
At 3f59b99c: `setup` TRACE OK steps=49; installer walks 000 and 063 TRACE OK
(2 and 4 steps; the other 62 were emitted, not checked one by one).

The full gate cannot pass at this base, for two reasons outside the traces:
- `cmd/nova-sprint` does not build (engine.go and handoff_trace_test.go
  import the deleted run/filestore), so the two handoff emitters fail; that
  is the stitch's wiring of the run verb.
- the choices emitter (`TestSetupChoicesActualTrace`) fails at setup step 6
  for both cases: `setup answer refused: row rowan does not exist` (the
  services answer; its fixture predates e0887b13's role hooks).

`ci-tlc.sh` now runs layer 6 too (the models, witnesses/layer6, and the Setup
and Install witnesses beside their models). On hetzner2 at d8d7bd29 (TLC_JOBS=6):
234 results, all `ok`, exit 0; RunTickWhileStoppedGate and
RunTickWhileStoppedFence each violate NoTickWhileStopped, and RunFailover passes.
