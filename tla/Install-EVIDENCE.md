# Install staged activation model evidence

This is a separate process-crash model of release installation. `Setup.tla`
models the eight-step setup walk and does not establish installation layout or
publication safety. Existing Run/Setup/Clear models and their 23 accepted cases
are unchanged. This evidence does not claim deployment or a completed Layer 6
runtime gate.

## Checked execution

Executed on authorized vision fallback under `nova-reserve` shared slot 3/16,
with two TLC workers and a 1 GiB Java heap, on 2026-11-07 00:33:56–00:34:03 UTC
(2026-11-06 local). Runner and pinned TLC v1.8.0 jar are unchanged. Jar SHA-256:
`7a0a1e90628ebff2e3e8733375f8b684fa59504d2e02bddc4cb51a7679bb7e04`.

```text
ssh vision nova-reserve run --slots 16 --wait 60s --why stella-l6-install-tlc -- python3 /home/glenn/jobs/stella-v2-l6-install-model-20261106/tla/run-layer6.py --manifest Install-runs.json --tag install-review
```

`layer6-evidence/install-review/results.json` records the exact module and
configuration hashes, exit statuses and host. The adjacent raw logs retain
complete counterexamples. The two passing cases each explored 24 generated /
24 distinct states, depth 9, including temporal checking. Each reversed case
exited 12 with its named invariant violated, not a parser failure or timeout.

| Case | Checked result and handchecked trace |
| --- | --- |
| Install | Previous active release: all ten invariants and EventuallyStopped pass. |
| InstallFresh | No previous active release: all ten invariants and EventuallyStopped pass. |
| InstallCheckSkipped | State 8 Activate selects candidate while checked=FALSE; CheckBeforeActivation fails. |
| InstallVersionIgnored | State 6 reads other-version; state 8 activates it; ExactVersionBeforeActivation fails. |
| InstallPartialPublished | State 4 Publish exposes candidateBytes=1; PublishedCandidateComplete fails. |
| InstallFailureSwitches | State 5 CheckFail changes active to candidate; FailurePreservesActive fails. |
| InstallUnlinksFirst | State 7 PrepareLink removes the old active link before Activate; NoActiveGap fails. |
| InstallDeletesPrevious | State 8 Activate removes the prior release; PreviousRetained fails. |

These six variants deliberately change the protocol. They are sensitivity
checks of the invariants, not findings that the reviewed implementation has
these faults. `Install-generate.py` reproduces their modules, configurations
and manifest, asserting each source replacement is unique.

## Source correspondence and limits

Read-only source checkpoint hashes are in `Install-source-checkpoint.json`.
The checkpoint is bounded to those file bytes; it is not a claim about later
edits or a final integrated binary. No Go tests were executed by this modeler.
Real-filesystem test execution and lock behavior require their separate
implementation evidence.

- `installStage` writes into a private directory, waits for successful
  WriteFile, then renames that directory to the version path. Begin,
  FinishWrite and Publish represent this order. A partial private stage may
  remain after process death but is never a published executable target.
- `verifyCandidate` runs candidate `check`, then requires candidate `version`
  output to equal the requested version plus one newline. CheckPass/CheckFail
  and ReadVersion abstract these results. Neither a failing check nor an
  incorrect version reaches the real switch call. Failed published candidates
  remain inspectable.
- `installSwitch` creates a temporary symlink under the same bin directory
  and renames it over the old symlink. PrepareLink/Activate capture this
  separation. It refuses a non-symlink destination; Fail abstracts that return.
  No step deletes the previous version directory.
- Crash can occur at every non-failed phase, including before and after
  activation. Under atomic same-filesystem rename, the active path remains
  the initial target or a fully published candidate. A fresh install may
  still have no active link before activation. An already-broken initial
  symlink is outside the initial-complete-target assumption.
- The source now uses nonblocking OS advisory file locks on stable inodes,
  released by Close or process exit, instead of mkdir ownership. Exclusive
  ownership is a model assumption, not a proof of the OS primitive. The model
  does not establish reacquisition, retry scheduling or simultaneous installer
  progress. Noncooperating filesystem writers and mutation of a checked
  candidate are excluded.
- Existing matching version reuse is abstracted to a complete published
  candidate followed by verification; byte comparison, archive parsing,
  checksums, path validation and permissions are implementation obligations,
  not established by this state machine. No cryptographic guarantee is claimed.
- No fsync is present. These claims concern process-crash visibility and
  atomic rename, not machine crash, filesystem corruption or power-loss
  durability. Private temporary files left by abrupt exit may remain.
- EventuallyStopped says one attempt eventually succeeds, fails, or crashes,
  assuming weakly fair Progress (external calls eventually return). It does
  not guarantee successful installation, bound time, or prove recovery from
  hung checks. Crash is terminal in this model; automatic retry, three-tick
  adoption and automatic rollback are outside the current release scope.

The model has no filesystem operation after Activate that can report a new
installation failure; this matches the reviewed function's immediate success
return after installSwitch. Post-activation telemetry, adoption or other work
would require revisiting FailurePreservesActive before extending its claim.
