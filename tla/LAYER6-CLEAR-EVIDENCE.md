# Clear delivery refinement and implementation correspondence

This extends, rather than replaces, the seventeen checked cases recorded in
LAYER6-EVIDENCE.md. Run.tla and Setup.tla and their checked configurations are
unchanged. Run/RunReports do not include Clear; their AllEventsSent property
applies while delivery obligations are not cancelled by an epoch change.
RunClear.tla supplies the explicit cancellation refinement.

## Contract

Every committed notice is eventually sent, or cancelled by a later Clear and
covered by a confirmed per-recipient Clear summary. A cursor cannot pass a
notice with neither coverage, or a Clear whose summary did not confirm. Clear
summary duplicates after a crash are permitted under at-least-once delivery.
The exact text is:

```
epoch <n> cleared: <k> pending notices dropped (some may have reached you); run nova-sprint next
```

The count is cursor-pending notices, not certainly undelivered notices. A send
can confirm before its cursor write fails, is refused by Clear, or is lost to
a crash. Such a notice may be counted again. The code's count is notice lines;
the small model has one notice per notice event.

A snapshot/scan observes an epoch; it does not fence a subsequent bus send.
An old notice already prepared may arrive after Clear commits and before the
pusher observes it. An old-epoch cursor write is refused, and the next scan
suppresses old backlog and emits the summary. The pushed verb's original epoch
must refuse a mutation in the new epoch. Neither this model nor the code
promises atomic exclusion of Clear and external sends.

## Actual TLC evidence

Rowan authorized vision as a temporary fallback while hetzner2 reboots. Runs
used vision's existing capacity 16, one nova-reserve slot, two TLC workers and
a 1 GiB heap. The cached v1.8.0 jar matched the original pinned SHA-256
`7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04`.
No pin was changed and no TLC ran on the Studio.

```
python3 tla/make-run-clear-witnesses.py
ssh vision nova-reserve run --slots 16 --wait 60s --why friendG-l6-clear-tlc -- python3 /home/user/jobs/v2-l6-clear-model-20261106/tla/run-layer6.py --manifest layer6-clear-runs.json
```

The passing RunClear case generated 1,659 states, 1,112 distinct, with no states
left. Safety and both liveness properties (AllAccounted, AllCursorsAdvance)
passed. Bounds: two notices, one Clear, one recipient, at most one crash.
One-event scan actions retain the physical prefix/cursor layout; the small
prefix fits the implementation's scan budget. Large-history pagination remains
code-tested. Eventual sends require fair scheduling and eventual confirmation.

Six current cases are indexed in layer6-evidence/clear-review/results.json.
That index explicitly names source_run: it combines the initial six-case run
with a two-case witness refinement run. Full raw run records are preserved at
`/Volumes/nova/ai/friendG/working/l6-model-development-evidence-20261106/clear-review/`
and its clear-witness-review sibling. Only the current six logs are in this
checkout. Every indexed module/config SHA matches the checked local artifact.

| Case | Expected result | Handchecked actual trace |
|---|---|---|
| RunClear | pass, exit 0 | every cursor index covered; eventual delivery or confirmed cancellation; eventual cursor advancement |
| RunClearSummaryLost | CursorHasCoverage, exit 12 | notice 1, Clear 2, suppress notice and omit summary, cursor becomes 2 with sent empty and cancelled={1} |
| RunClearCursorEarly | CursorHasCoverage, exit 12 | Clear 1, prepare/scan, cursor becomes 1 before summary confirmation |
| RunClearInFlight | NoOldSendAfterClear, exit 12 | prepare epoch 1, Clear commits epoch 2, send original notice before observing Clear |
| RunClearCountAmbiguous | ClearCountIsUndelivered, exit 12 | old notice confirms after concurrent Clear; crash occurs before cursor persistence; rescan counts that confirmed-but-cursor-pending notice in the summary |
| RunClearOriginalCancelled | EveryOriginalSent, exit 13 | two old notices, Clear, crash, rescan sends only summary 3 and saves cursor 3; fair stuttering leaves originals unsent |

The final three are accepted limitation demonstrations, not injected code
bugs. The last trace shows why the original unconditional delivery property
cannot be claimed across Clear. The missing-summary mutation was tightened to
require actual cancelled backlog, so its counterexample is not merely a
zero-backlog Clear. No exception, parse error or wrong target counted as a pass.

## Current source correspondence

Read-only source hashes are in layer6-evidence/clear-setup-source-checkpoint.json.
They identify inspected source, not a final integrated or runtime-tested commit.

- Pusher.prepare rebuilds per-recipient history to the persisted cursor.
  collect applies each event's historical after-image before rendering notices.
  This keeps terminal stream notices tied to the event's time, including when
  later cards are added. prepare/collect enforce contiguous sequence numbers.
- collectEvent suppresses notices with event.Epoch below the observed snapshot
  epoch, counts them in the volatile per-epoch dropped map, and emits the Clear
  summary. A crash before persistence replays from the durable cursor and
  reconstructs those pending counts. The small model represents that scan and
  restart; it does not duplicate Store.Rebuild's domain records.
- Push confirms every recipient/id receipt before creating its cursor op;
  resolve replays/retries the fixed operation id and discards cached history
  after an epoch refusal. The cursor op carries the observed snapshot epoch.
- chooser/tick still separate the session check from store.Apply. Scheduler
  stops on observed ErrLockLost; pending unknown ops use Replay before retry;
  refused op facts return to L3; committedPlan checks every causal After id.
- Source tests include historical fold versus Rebuild, later mutations not
  changing an earlier stitch, old backlog cancellation, and a Clear following
  partial send. Their definitions were read; this note does not claim a new
  execution of the Go suite or final layer wiring.

Capped notification summaries still require next to enumerate every remaining
holding and open coordinator judgement. Clear cancellation is a distinct,
explicit withdrawal of old obligations, not a substitute for that overflow
integration gate.

## Setup correspondence update, without a new concurrency proof

Setup.tla remains the single serialized walk. Current source now supplies
Choices (inactive global policy data fenced by AtSeq), EnsureKey (Has before
prompt, durable Set via stdin), TTY hidden input/restoration, and NovaSecrets
with secret bytes excluded from argv/env/output. These source paths support
its durable-answer and secret-sink abstractions; they do not establish a full
RuntimeSetup Verify/Act adapter or successful fleet setup.

The readiness prefix runs before the PostgreSQL setup lease. Once PG is
available, up acquires the separate whole-walk advisory lock and rechecks from
step one. It checks the lease around steps and passes the monitor's cancelling
context to in-flight operations. This matches the observed-loss cancellation
ruling, not atomic exclusion across session death and external completion.
An operation or duplicate prompt may escape before loss is detected. Two
people's overlapping pastes need not be equal; same-value idempotence cannot
justify an arbitrary overwrite by a different value. The existing model's
AskedOnce therefore describes durable answers in a serialized stable-session
walk, not a theorem excluding every physical duplicate prompt under PG loss.
Crash before successful secret storage may require another prompt.

Concrete source primitives for fleet service definitions/grants and dashboard
probes were also read. Their existence is not evidence of an integrated
eight-step setup adapter, successful effects, or final wiring. The source
checkpoint deliberately keeps those boundaries explicit.
