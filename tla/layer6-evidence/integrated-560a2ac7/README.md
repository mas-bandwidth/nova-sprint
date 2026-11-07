# Integrated helper runtime checkpoint

The full fixed-seed helper emitted and matched all 83 actual traces at clean Git
commit `560a2ac7c3f127c5095dfc9236d7bf50c764c65a`. This was a manually invoked
reserved-bench gate, not a GitHub Actions run or a general Layer6 acceptance claim.
All Go emitter commands used race detection and count=1. The helper checked source
bytes and HEAD before and after emission, then input/spec bytes before and after
TLC. All 83 raw TLC logs were independently inspected for the success marker and
absence of warnings/errors; all 305 emission and 166 log hashes matched.

Emission used amd-1, reservation capacity4, Go1.26.10. Checking used vision,
reservation capacity16, the exact retained helper/shared checker, one TLC worker,
and the existing pinned v1.8.0 jar. No Studio test or TLC execution occurred.

The complete frozen source archive, input files, spec/checker copies, all emitter
and TLC logs are preserved at the exact paths in external-evidence.json. This
checkpoint retains their hash manifests and per-case results without duplicating
the complete raw evidence in Git. The source archive SHA-256 is
`b55b28f18ce53590e1829733a42fb02d2411e59beb7bcdd701e09aec530cd3c8`.

| Family | Matching inputs |
| --- | ---: |
| Run seeded report schedules | 3 |
| Clear fixed case plus enabled-action walks | 11 |
| Setup walk | 1 |
| SetupChoices none/configured | 2 |
| Install attempts | 64 |
| Handoff success/stale-epoch compositions | 2 |

The existing mapping limits apply: fake dependencies, bounded schedules and
sampled endpoint refinements; no arbitrary implementation or liveness proof.
The handoff fixture composes real child-channel/walker/helper endpoints without
the production outer coordinator or real PG/secrets infrastructure. Broad Go
functional/race gates remain separate from this trace result.

The initial GitHub runner inventory had amd-1/amd-3 bench runners, a captain
runner, and Studio's tlc label, with no registered vision/hetzner2 runner. That
historical inventory led to this manual split-bench run. Rowan's later routing
ruling (message suffix7278111, relayed by the parent) authorizes the existing amd
bench for CI trace checking, separately from the local vision rule. Studio
remains prohibited. The actual fixed-seed workflow and its observed run at the
definitive integrated revision are still pending; this manual pass does not
claim they are complete.
