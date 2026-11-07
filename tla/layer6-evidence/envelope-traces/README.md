# One confirmed recipient envelope: finite refinement checkpoint

All twelve distinct emitted Clear/walk trace byte sequences matched the extended
RunClearTrace wrapper on reserved hetzner2, with no raw TLC warnings or errors.
They represent 65 actual emitted inputs: one deterministic Clear scenario and
64 seeded action-selection walks. `cases.json` and `input-groups.json` record
exact SHA-256 aliases; only one identical input per group was checked. This is
12 matching TLC invocations, not 65 independent model runs. The fixed CI suite
still uses ten walk seeds plus the deterministic case; its total remains 84.

The wrapper maps one actual validated envelope confirmation to a deterministic
finite sequence of the existing abstract Send transitions, in increasing member
ID order. No external action interleaves. It then matches the single concrete
row. These are abstract acknowledgements of members in one physical message,
not invented per-group sends. RunClear.tla, its Next and its invariants are
unchanged. Every intermediate abstract state still checks those invariants.

The actual post-validation callback binds recipient/message ID, collection
From/Through/End/Epoch, exact rendered subject/body and original included/omitted
Notice values. Fixtures independently check exact body and unique Notice-to-
obligation membership against the immutable collected events. They execute the
actual pusher with store.NewFake and an injected sender; production bus delivery
and PostgreSQL atomicity are not re-proved. All these fixtures assert zero
omissions and fit the real scan/body budgets. They do not exercise failed sends,
crash, in-flight Clear, general partial scans or general overflow.

## Intended overflow is outside this coverage predicate

Production intentionally advances its cursor after a confirmed overflow summary
that directs the recipient to `next`; omitted actionable obligations remain
queryable under that separate contract. The current RunClear predicate represents
coverage by delivery or a Clear cancellation and has no `next` retrievability
branch. It therefore cannot establish that intended overflow behavior.

`omission-fixture` is an explicitly altered projection of the actual deterministic
input, not an implementation-emitted trace or a reversed implementation bug.
It removes member 3 from rendered/sent/acknowledged sets, labels it omitted, and
retains the proposed cursor advance. TLC reaches the confirmation at record 7
with acknowledged={2}, needed={2,3}, cursor=0 and refuses SaveCursor at record 8.
That expected model refusal demonstrates the unmodeled overflow scope and that
omitted IDs were not silently converted into abstract Sends. It is not a claim
that production forbids the cursor advance or that production is defective.

## Exact source snapshots and retained verdicts

The Clear fixture source archive is
`7163a783147675feeaae7b92d2f08c2b84d83c799352f4e792a6fe4af01ae963`.
It is retained locally under
`/Volumes/nova/ai/stella/working/l6-model-development-evidence-20261106/envelope-20261107/`
and on amd-1 at `/var/tmp/stella-l6-envelope-trace-20261107/`.
The deterministic/default-seed race log is retained. The supplied walk log only
contains the reservation line; the emitter owner reported all 64 seed emissions
successful. This receipt independently verifies their frozen bytes and model
verdicts and does not claim 64 independently retained race-test logs.

A later source snapshot changes the Run fixture's exact expected subject/body to
match the recipient envelope. Its archive is
`735d548a826a99b31b918fd974b483d6c83aaaaf1d9cb7dfde1b92a2e551e3b5`,
retained in the adjacent `envelope-run-20261107/` local directory and amd-1
`/var/tmp/stella-l6-envelope-runtrace-20261107/`. The three emitted seeds 1, 7 and
42 all selected the same existing run-1 trace bytes; the stopped-mode output
matches the retained stop trace. `run-rebinding.json` pins those unchanged input
and raw-verdict hashes. They were not unnecessarily rerun and are not three
different source schedules. The four fresh Go success outputs are retained;
this receipt does not label them race runs.

Each source snapshot has its own 101-file Go/dependency manifest, and the full
archives preserve the remaining tree. `checked-spec`, `inputs`, `results` and
artifact manifests pin the exact model/config/shared-checker/input/raw-log bytes.
The complete TLC package remains on hetzner2 at
`/home/nova/jobs/stella-l6-envelope-20261107/`. These two archived-source finite
checkpoints do not constitute final integrated-source or GitHub CI acceptance.
