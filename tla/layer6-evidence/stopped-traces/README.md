# Stopped-run finite implementation traces

Four actual-code traces matched the stopped/AtSeq RunTrace extension on reserved
hetzner2 using the retained shared checker and pinned TLC jar. The raw logs have
no warnings or errors. This is exact archived-source finite-execution evidence,
not a clean-head or final integrated CI acceptance and not a general proof of the
implementation or its liveness.

| Input | Records | Generated / distinct states | Observed scope |
| --- | ---: | ---: | --- |
| run-0 | 29 | 31 / 29 | c1 report before first op, push, lock loss and restart |
| run-1 | 29 | 31 / 29 | c2 report between ops, push, lock loss and restart |
| run-2 | 35 | 36 / 35 | c2 report before first op, two distinct refusals, restart completion |
| stop | 31 | 32 / 31 | Stop, three stopped folds with an intervening report, Start and normal completion |

The actual chooser/Engine/Pusher code executes with `store.NewFake()`, a
fixture scheduler, fake lock/hook and transport dependencies. This binds the
run-side control flow against that store implementation; it does not re-prove
production PostgreSQL atomicity. ReadControl samples actual Engine.state.Stopped/Seq, CaptureFence
reads the actual appended AtSeq, and receipt rows bind the unchanged expectation
to receipt.Seq-1. Refused operations and cursor writes advance physicalSeq even
when excluded from notice-event rank. The run-2 trace's first refusal is AtSeq
mismatch on unchanged c1; its second is stale c2 generation with a fresh AtSeq.
No invented card generation or omitted physical receipt makes it pass.

The stop schedule calls the real scheduler three times while stopped: once before
the report and twice after it. Each returns the actual folded stopped observation
without a planner action. Start is a separate accepted write. These traces do not
exercise an uncertain pending op across Stop/Start: the byte-identical retry test
remains separate evidence. Unknown committed Replay, general multi-batch pushes,
overflow discoverability, production L3 carry and atomic external dispatch versus
Stop remain outside this finite binding. Clear has its separate historical model
and trace family.

## Exact source and checker

Emission ran with the race detector on reserved amd-1. The full dirty-source
archive is preserved locally at
`/Volumes/nova/ai/stella/working/l6-model-development-evidence-20261106/stop-trace-20261107/source.tar.gz`
and on amd-1 at `/var/tmp/stella-stop-trace-20261107/source.tar.gz`.
Its SHA-256 is `5db72a67a317cc7104edeb2ed236be3746330f5da895092b3b9e125f50039d48`. `source-sha256.json` lists 101 Go source/dependency files
from that exact archive; the archive preserves the complete source snapshot.
This receipt does not rebind later edits to that snapshot. At packaging, 94 of
101 listed files matched main; go.mod/go.sum and five service/pusher/scheduler
files had moved. The sampled Engine/trace code remained matched. Final integrated
source validation must run separately.

`checked-spec/` preserves the exact model, cfg and shared checker used. `inputs/`
holds unmodified emitted trace bytes, `emit-*.log` the race-run outputs, and
`results-corrected/` the raw TLC and shared-harness verdicts. Artifact hashes cover
these bytes. `check.py` is the archival bench driver; its output directory is
exclusive to an invocation to prevent overwriting evidence. The complete bench
package remains at `/home/nova/jobs/stella-l6-stop-trace-20261107` on hetzner2.

## Preserved first checker failure

The first invocation failed at Init because TLC cannot compare the string
sentinel `"no-seq"` with integer sequence values. This was a wrapper type error,
not a trace refusal or source defect. The sentinel became integer -1 with
Integers imported; Next, required fields and invariants were not weakened. The
same four actual input files then matched. All initial raw failures and their
checked spec remain under the external local evidence path above in
`results-initial-type-error/` and `checked-spec-initial-type-error/`.
Their `initial-type-error-sha256.json` hash is `9ce2c3d92c5d9e6be9854f6cc94bc99b6cd7db4020de8a04ff3f5b4793174c15`.
