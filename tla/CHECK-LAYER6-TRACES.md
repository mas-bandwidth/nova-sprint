# Fixed-seed Layer6 trace gate

`check-layer6-traces.py` emits traces from the current implementation, then checks
all 83 inputs through the current shared `check-trace.sh`. Run it only under an
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

The seven focused Linux tests cover false success without a raw log, warning/error
rejection, missing cases, inherited trace-knob removal, and descendant termination
on timeout, plus approved CI host/runner routing. They use benign fake commands and do not execute TLC. Full integration
requires running emission and all 83 checks at the exact integrated commit; unit
checks alone do not satisfy that gate.


Rowan's7278111 ruling additionally authorizes the existing amd GitHub CI benches.
Read-only hostname/service checks establish b1 runs amd-1-nova-sprint-1 and b3
runs amd-3-nova-sprint-1. The helper admits only those exact host/RUNNER_NAME pairs
with GITHUB_ACTIONS=true. These environment values are routing markers, not a
security boundary or independent authorization. Local amd TLC remains disallowed;
vision/hetzner2 local routing is unchanged and Studio remains prohibited. The
caller still acquires the approved reservation and installs the pinned runtime.
