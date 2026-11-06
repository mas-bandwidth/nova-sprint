# Layer 6 model evidence

Written alongside layer 6 from v2 `2432fd0e`, on branch `v2-l6-models`.
A model result does not establish an implementation commit, runtime gate,
or completion of layer 6.

## Scope and boundaries

`Run.tla` composes chooser, delegated L3 scheduling, store contract, and pusher.
The bounded cases use two cards, up to two processes, three work operations,
one recipient, and three reports. FIFO sequences and cursor index ranges are
explicit. Each packed operation has one card move; multi-write atomicity and
same-ID replay belong to layer 1, not a new proof here.

- PostgreSQL lock exclusivity is an input primitive represented by one owner.
  `OneChooser` does not prove PG itself or exclusion across separate health
  check and commit calls. Unobserved lock/hook loss permits a final Apply;
  observed loss stops subsequent applies. `RunLockCheckRace` deliberately
  violates the stronger `CommitUnderCurrentLock` property, recording the
  accepted limitation. Different choosers can have different policy/presence.
- L3 owns ordering, packing, budget and carry. Run applies independently packed
  operations in order and returns whole refused ops next tick. It never splits
  a packed transaction. FIFO/budget actions describe that delegated interface.
- Restart loses volatile FIFO history. Store-derived work is replanned from a
  cold snapshot, in a fixed enumeration in this instance. L4 facts must be
  requeryable by stable `(kind,card,sha)`: CI from forge at a head, landing from
  git ancestry/base tip, runner from store state/head and restart reports.
  Steps reemit queries for awaiting cards; L4 tests this assumption. No new
  durable inbox or preservation of pre-crash FIFO history is claimed.
- SendOK requires exit 0 AND a confirmed message id for the intended recipient.
  Cursor save follows confirmation. A crash between them permits duplicates;
  delivery to a human or execution of a suggested verb is not implied.
- Dependent edges require accepted causal op IDs. The instance has one
  dependency per edge; code checks every `After` id, with empty meaning
  independent. Apply is atomic and external calls are separate actions; the
  store owns its internal writer-lock behavior.
- The log abstraction includes accepted events that all need the one recipient.
  Filtering unrelated/refused events, message chunking, group deduplication,
  and multiple recipients still require pusher unit/functional coverage.
- Liveness assumes eventual stability (bounded reports/failures), weak fairness,
  positive budget, and eventual confirmed sends. This is no wall-clock or
  two-tick latency proof. Transport-unknown retry plumbing remains code-tested.

`Setup.tla` includes all eight steps. Every invocation probes from step one;
verified is evidence from this invocation, never a persistent completion map.
A service may regress after its probe: this is not a distributed transaction
across eight services. AskedOnce counts successfully stored durable answers,
not prompts interrupted before storage. Prompt plus successful secret save is
one abstraction; interrupted input can require reentry. No secret bytes are
modeled, only allowed sinks (secret setter stdin). Liveness assumes external
prerequisites eventually become repairable and humans supply required answers.

## Reproduction

Generate artifacts without TLC on the working machine:

```sh
python3 tla/make-run-witnesses.py
```

On hetzner2, under its existing eight-slot capacity:

```sh
nova-reserve run --slots 8 --wait 60s --why stella-l6-tlc -- python3 /home/nova/jobs/stella-v2-l6-models-20261106/tla/run-layer6.py
```

The runner uses two TLC workers, a 1 GiB heap, isolated temp/state directories,
and a two-minute timeout per invocation. It verifies v1.8.0 jar SHA-256
`7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04`.
TLC prints `TLC2 Version 2026.11.06.100558 (rev: 3ec1cc4)`.
Pass requires exit 0 and the completed/no-error marker; a safety witness needs
exit 12 and its exact invariant; a temporal witness needs exit 13 and its exact
property. Parse errors, timeouts, and different targets fail. results.json
records each source/config SHA-256.

Witnesses live in `witnesses/layer6/` to avoid the old top-level invariant-only
runner misclassifying temporal exit 13. Shared CI must explicitly invoke the
layer 6 manifest; no shared CI files were changed. Every TLC run here used
reserved hetzner2. No TLC or Go build/test/vet ran on the Studio.

## Actual checked results

Acceptance pointer: `layer6-evidence/review/`, including results.json and full
logs. It contains 17 checked invocations: four passing configurations and
thirteen required counterexamples, including the accepted lock limitation.
Development logs (first/second/aligned/final) are preserved outside the
checkout at `/Volumes/nova/ai/stella/working/l6-model-development-evidence-20261106/`
and are not the acceptance pointer. The first run exposed an incorrect monitor
assignment written as a guard; this was corrected before acceptance runs.

| Configuration | Generated states | Distinct states | Scope |
|---|---:|---:|---|
| Run | 68,175 | 14,007 | safety and eventual work/sends |
| RunFailover | 173,361 | 27,268 | safety and liveness with one crash, PG/hook loss, or hook failure |
| RunReports | 529,052 | 125,152 | safety and liveness with three reports |
| Setup | 525 | 304 | safety and eventual repeated done after one disruption |

These are measured bounded TLC enumerations, not production performance.

## Handchecked counterexamples

Traces were read after TLC named the exact target, not inferred from exit code.
The source comparison is against the implementation checkout
`/Volumes/nova/ai/stella/working/nova-sprint-v2-l6-run-20261106`.
Chooser/tick and pusher source have been read. Pusher source hashes are in
`layer6-evidence/pusher-source-checkpoint.json`; these identify the provisional
source reading, not a runtime-tested commit. The setup walker has also been checked at the separate verbs checkout below;
its concrete persistence/secret adapters remain pending. Model results alone are not runtime
verification.

| Case | Concrete trace | Code correspondence |
|---|---|---|
| RunLockCheckRace | Acquire, fold/select, lose lock, Apply before noticing | Chooser.Check and Engine.applyPending check separately from store.Apply; accepted limitation |
| RunIgnoresObservedLoss | Observe loss, retain pending work, apply with checked false | applyPending returns on guard.Check error before Apply; mutant bypasses stop |
| RunFenceDropped | Fold gen 0, report raises it, apply old gen | store/apply.go run checks expects before writes; condOf(CardGen) rejects mismatch; layer 1 owns correctness |
| RunBudgetIgnored | Select/apply three operations under budget two | L3 contract; run does not pack/enforce move budget |
| RunCarryDropped | Apply two, discard third, fair empty ticks loop forever | applyPending passes Plan.Carry first into mergeInbox; L3 retains/replans obligation |
| RunRefusedLost | Report invalidates last op, refusal forgets it, fair empty ticks loop | applyPending records each RefusedOp and retains original inbox facts after refusal; next L3 Tick must rederive work |
| RunCursorBeforeSend | Prepare range 1..1 then save cursor while sent is empty | Push validates Sender error, recipient and message id before creating the cursor op; resolve retries/replays the fixed cursor op |
| RunPushBeforeCommit | Send range includes next nonexistent event | collect reads only contiguous accepted events through the supplied committed snapshot Seq before Send |
| RunEdgeBeforeCommit | Dispatch dependent edge with no committed op | committedPlan/hasAll requires accepted receipts for every After id |
| SetupUnverified | First probe marks missing prerequisite verified | setupStep calls Verify, optionally Act, then Verify again; advances only on Verified |
| SetupAnswersLost | Save coordinator answer, crash forgets it, restart asks twice | Trace checked; durable answers belong to the unfinished Setup adapter |
| SetupResumeSkips | Crash then restart at step eight | up starts its for loop at step 1 on every invocation |
| SetupKeyInArgv | Ask key and record argv as its sink | Trace checked; hidden prompt and secret transport adapter are unfinished |

The old “put back every plan starves forever” claim is not made: refusing a
whole packed op is correct, and finite reports do not imply permanent starvation.
RunRefusedLost supplies a genuine temporal violation consistent with the seam.
No assertion of single-current-chooser commits or crash-persistent FIFO was
silently carried over from the superseded design wording.

## Pusher correspondence checkpoint

The source reading checks Push, collect, and resolve: events above the
snapshot Seq are excluded, rejected events are not passed to Notifications,
sequence gaps refuse, and no cursor op is made until every batch returns a
matching recipient and syntactically valid bus id. A failed/partial send keeps
the persistent cursor behind all batches and can repeat confirmed batches.
An uncertain cursor write stays pending and is resolved by Replay/same-ID
Apply before a new send. The unrelated-event scan cache is volatile; it writes
no cursor heartbeat and can be safely repeated after restart. The sender
interface must faithfully parse exit status and the transport receipt; merely
fabricating a syntactically valid id would not satisfy SendOK.

The pusher caps a subject/group body at 4 KiB and 20 item lines. Omitted items
become a count and nova-sprint next command before the cursor passes them.
This is the L4 summary contract, not literal delivery of every original line.
Refinement of AllEventsSent remains conditional on next/store exposing every
omitted actionable obligation. A bounded summary whose command cannot recover
an omitted obligation does not satisfy the model. Tests inspect formatting,
receipts, unknown cursor replay, and basic real-bus/PG transport, but this
reading does not establish the omitted-obligation integration gate.

The initial source reading found shared scan/pending maps protected only by
a per-recipient calling convention. The updated Push holds a mutex across
the complete call and explicitly serializes all recipients; this closes the
source-level ownership gap. The updated file hash is in the checkpoint. The
model assumes atomic steps; a Go race test remains runtime evidence, not a
result of TLC.

## Setup walker correspondence checkpoint

The walker is in
`/Volumes/nova/ai/stella/working/nova-sprint-v2-l6-verbs-20261106/verbs/setup.go`;
its inspected SHA-256 is in layer6-evidence/setup-source-checkpoint.json.
`up` always loops from step one, and `setupStep` probes before action, repeats
Verify after Act, and advances only on Verified. These correspond to
NeverAhead, OnlyVerified, and RestartFromOne. The concrete Setup adapter is
not supplied by this source, so AskedOnce and KeyNeverPrinted remain unverified
implementation obligations: look up durable answers before prompting, never
repeat a successfully saved answer, and send hidden input only to secrets
stdin. Adapter-controlled Line/Next output must also exclude secret bytes.
Liveness assumes callers reinvoke up after a stop and external prerequisites
and human answers eventually permit progress; the walker itself installs no
automatic retry. No runtime setup completion is claimed.
