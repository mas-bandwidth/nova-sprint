# Layer 6 finite code-trace checks

Reserved Linux vision checks, pinned TLC v1.8.0 jar SHA-256
`7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04`.
All 82 retained harness outcomes were checked against the raw TLC logs; no TLC
warnings appeared. There are **81 matching actual traces and one deliberately
corrupted checker fixture**, not 82 implementation safety proofs.

| Input family | Cases | Recorded steps | Scope |
| --- | ---: | --- | --- |
| Install | 64 | 2–4 each | Seeded scenario selection; actual filesystem/callback/return endpoints |
| SetupChoices | 2 | configured 53, none 52 | Actual walker, durable choices, successful key save; fake dependencies; three seeded regressions each |
| Setup | 1 | 49 | Final frozen3 actual walker, preexisting answers, fake dependency regressions |
| Run | 3 | 21 each | Actual tick/refusal/push/loss/restart, three seeded report schedules, fixture scheduler |
| Clear stable case | 1 | 9 | Actual collector, summary/send and persisted cursor; 12 model states include deterministic scan |
| Clear action-selection walks | 10 | 13,16,8,12,13,12,12,13,13,10 | Actual enabled notice/Clear/Push choices, bounded two notices and one Clear |
| Corrupted Install result | 1 | refuses 4 of 4 | Deliberate wrong active target after success; checker validation only |

The shared harness is based on v2-trace
`30e2007a272e82b69b940472aef80ac9db9c4c9d`, with two narrow additions: optional
TRACE_LOG retains the raw TLC output, and zero recognized record lines are an
error. Exit 0 means the expected verdict; the corrupted fixture therefore exits
0 after TLC's temporal counterexample. Other TLC errors remain harness exit 2.
No invariant or Next relation was weakened. Exact runner bytes are in checked-spec.

The specification directory contained no TraceLog module. Each supplied input
was resolved only from its isolated input directory; the negative result confirms
that a changed supplied projection is checked, rather than a root sample being
silently loaded. Records use the shared two-space indentation format.

`artifacts.json` binds exact checked model dependencies, configs, inputs, raw TLC
logs, harness stdout, source manifests and emitter test logs. `external-sources.json`
locates the preserved full source archives/tree. All listed source manifests
were independently verified against those archived bytes. Install source2's
repaired emitter produced all 64 byte-identical inputs, independently compared.
Final model source has only explanatory comment corrections in SetupChoicesWalk
and SetupChoicesTrace compared with their archived trace-check bytes; the exact
checked versions are retained, and the admitted-initial-state check uses the
corrected Walk source. No behavioral transition changed.

The earlier setup-trace-first provenance is explicitly nonreproducible: its
source was overwritten. It remains historical trace-file evidence only and is
not included in these 81 source-bound traces. Frozen3 supersedes it for the
walk-only claim.

Finite matching proves these recorded executions under the documented refinement
relations. It does not establish arbitrary Go behavior or liveness. Run uses a
fixture scheduler, one recipient and one obligation per push batch; unknown
Apply/replay, overflow discovery, multi-batch/multi-recipient delivery and edge
protocol traces remain outside these inputs. Choices covers none/one provider,
not Supply, multiple providers or crash-before-save. The full typed two-child
handoff trace is still pending. Install has no observed crash, concurrency or
power-loss execution. See the corresponding *Trace-MAPPING.md files.
