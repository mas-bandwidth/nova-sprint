# Layer 6 trace gate

`tla/check-layer6-traces.py` emits traces from the current implementation on fixed seeds and checks each one with TLC against its trace model through `check-trace.sh`. It binds the Run, Setup, SetupChoices and SetupHandoff models to the code. A pass is finite execution evidence for those runs, not a proof of arbitrary behaviour.

## Cases

| Case | Model | Emitter |
|---|---|---|
| `run-0`, `run-1`, `run-2` | `RunTrace` | `TestEmitRunTrace` in `./run`, seeds 0 to 2 (`NOVA_RUN_TRACE_SEED`) |
| `run-stop` | `RunTrace` | the same test with `NOVA_RUN_TRACE_STOP=1`: Stop, an ordinary report between stopped ticks, Start |
| `setup` | `SetupTrace` | `TestSetupActualTrace` in `./verbs` |
| `handoff-0`, `handoff-1` | `SetupHandoffTrace` | `TestEmitHandoffTrace` in `./cmd/nova-sprint`, success and stale epoch |
| `choices-none`, `choices-configured` | `SetupChoicesTrace` | `TestSetupChoicesActualTrace` in `./verbs` |

The refinement mapping for each model is in its `*Trace-MAPPING.md`.

## Usage

```
python3 tla/check-layer6-traces.py --mode emit  --repo /checkout --out /var/tmp/l6-traces
python3 /var/tmp/l6-traces/spec/check-layer6-traces.py --mode check --out /var/tmp/l6-traces
python3 tla/check-layer6-traces.py --mode all   --repo /checkout --out /var/tmp/l6-traces
```

`emit` needs a clean Git checkout and an output directory outside it. It archives every tracked file before running, records HEAD and per-file hashes, and verifies both again afterwards. The helper that runs must match the checkout's copy. Every Go emitter runs with `-race -count=1` and two workers, with inherited `NOVA_*TRACE*` variables removed; each case supplies its own seed and output path. `check` runs in an isolated input directory against a spec directory with no sample TraceLogs. `--mode all` does both. `--go` selects the Go executable. Output paths are never reused and failed output stays for inspection. A timeout kills the job's process group.

A missing case, mismatched artifact bytes, a missing raw log, a TLC warning or error, a refusal, a timeout or an inconsistent record count fails the gate. `check-results.json` keeps every attempted verdict, including the first failure. The checker uses the pinned TLC jar.

The CI `trace` job runs `--mode all` under `nova-reserve`. `test_check_layer6_traces.py` tests the helper with benign fake commands (false success without a raw log, warning and error rejection, missing cases, trace-knob removal, descendant termination on timeout, host and runner routing, explicit stopped-case seed and mode); it does not run TLC.
