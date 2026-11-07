# Cold review: layer 4 (edges) — v2r-l4-flash1

Scope read in full: `docs/LAYER-4-EDGES.md`, `docs/ENFORCED.md`,
`docs/SEAMS.md`; every Go file under `edge/` (exec, argv, failure, request,
bus, forge, git, worker) and `cmd/nova-worker/`; `tla/LandEdge.tla`,
`tla/Runner.tla` and their trace bindings. Base
`43d849e29d0e4568ba6a1348a076a59259639938`.

Commands run (first lines; full output in the session log):

```
$ go build ./...
BUILD_EXIT:0
$ go vet ./...
VET_EXIT:0
$ go test ./edge/... ./cmd/nova-worker/...
ok  github.com/mas-bandwidth/nova-sprint/edge  0.875s
--- FAIL: TestFunctionalNovaBusWhoAndSend (5.03s)
ok  github.com/mas-bandwidth/nova-sprint/edge/forge  0.141s
ok  github.com/mas-bandwidth/nova-sprint/edge/git  2.525s
ok  github.com/mas-bandwidth/nova-sprint/edge/worker  3.917s
--- FAIL: TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot (5.41s)
FAIL github.com/mas-bandwidth/nova-sprint/cmd/nova-worker  5.562s
```

Both failures are functional-tier tests needing bench tools (`nova-bus`
with Redis, a shipped worker run) that are absent here; they are reported
under Missing tests / NOT RUN, not counted as code defects. No TLA+ check
was run (no TLC on this machine; see Questions).

## Verdict

The layer is broadly sound — argv-only exec with capped output, leased
pushes, an idempotent report path, and a runner whose pass/job split
matches its model — but it is not yet trustworthy on the bench it will
actually run on: after a restart on macOS the runner cannot prove a
surviving harness is alive, so it can start a second harness in the same
job directory, and separately a failed dirty-check deletes a worktree it
was told to keep. The single worst thing is that restart-adoption hole
(Defect 1, serious): it breaks guarantee 7 (one live harness per job
directory) on the default bench platform.

## Defects

### 1. serious — `edge/worker/runner.go:93` — adopted harness is invisible after a restart on macOS, so a second harness can start in its job directory

`Alive` proves a pid by the in-process `liveProcNonces_internal` map or by
`/proc/<pid>/environ`. On macOS there is no `/proc`, and the map is empty
in a restarted process, so `Alive` returns false for a harness that is
still running. `recoverWorkingJob_internal` then treats the job as gone
and resumes it, starting a second harness against the same worktree.
This breaks guarantee 7 and the model's `OneLiveHarnessPerJobDir` /
`PidRecordedAtStart` on the platform the bench runs. Repair: persist the
nonce check in a form that survives restart on macOS (for example verify
the pid's start time or command line), or refuse to resume a `running`
record whose liveness cannot be proved.

### 2. serious — `edge/worker/runner.go:735` — a failed dirty-check deletes the worktree the contract says to keep

In `cleanJobDir_internal`, when `IsDirty` errors the code logs
"dirty check failed, removing" and falls through to `RemoveWorktree` and
`os.RemoveAll(jobDir)`. Guarantee 9 and the doc's Clean step allow removal
only after the report is recorded and the head is on origin, with a dirty
tree kept; a git failure (missing binary, corrupt worktree) is exactly
when uncommitted work is most at risk. Repair: on a dirty-check error,
keep the directory and return, as the unconfirmed-head branch above it
already does.

### 3. serious — `edge/forge/forge.go:255` — `OpenPR` swallows a failed PR lookup and creates blind

`if existing, ok, err := c.PR(...); err == nil && ok { return existing }`
discards a non-nil `err` (rate limit, auth failure, timeout) and proceeds
to `pr create`. A rate-limited identity therefore spends more calls
instead of resting, and a lookup fault is re-reported, if at all, as a
create failure with the wrong cause. The doc requires a rate limit to
rest the identity and fail no card. Repair: return the lookup error
before attempting the create.

### 4. serious — `edge/git/git.go:139` and `edge/git/git.go:200` — every fetch failure is "ref not on origin", and a SHA lookup fetches every tip without bound

`Fetch` wraps any non-zero exit as `ErrMissingRef`, so a silent host, an
auth failure and a genuinely absent ref are one typed result, against
the doc's typed failures (conflict / push refused / host silent).
`lsRemoteSHA_internal` then loops over every remote tip calling `Fetch`
plus `IsAncestor` per tip, each under its own 60 s timeout with no total
bound: a repository with many branches turns one `LsRemote` into an
unbounded sequence of fetches. Repair: classify fetch failures by cause,
and bound the SHA search (one fetch of all tips, then local ancestry
checks).

### 5. serious — `docs/LAYER-4-EDGES.md:23` — the promised `tla/Push.tla` does not exist

The doc twice binds the bus cursor to `tla/Push.tla` (lines 23 and 465,
and the witness list at line 545 names `PushCursorBeforeConfirm`), but
there is no `tla/Push.tla` and no such witness in the tree, and this
package holds no cursor at all (`edge/bus/bus.go:1` says what to send and
when the cursor moves is run's). Guarantee 2 (no message skipped) therefore
has neither the named model nor a model of the code that does move the
cursor. Repair: write the model against the run-side pusher that owns the
cursor, or correct the doc to name the model that does.

### 6. minor — `edge/request.go:76` — a panic in a deduplicated call wedges its key for the process's life

`Deduplicator.Do` registers the in-flight call, runs `fn`, and only then
deletes the entry and closes `done`, with no `defer`. If `fn` panics, the
entry stays and every later caller with the same key blocks on `done`
until its own context ends; the panic also escapes into the worker.
Repair: delete the entry and close `done` in a `defer` that re-panics or
converts the panic to an error.

### 7. minor — `edge/worker/proxy.go:31` — one shared proxy status is consumed by whichever job harvests first

The key proxy keeps a single `lastStatus` atomic for all routes and jobs;
`runStep_internal` (`edge/worker/runner.go:559`) and
`harvestStep_internal` (`edge/worker/runner.go:585`) race to consume it.
With two jobs in flight, a provider 429 or 5xx raised for one job can be
attributed to the other, misclassifying its outcome and its retry.
Repair: key the recorded status by route or by job, or return it with the
proxied response to the calling job only.

### 8. minor — `edge/worker/proxy.go:217` — `X-Target-Host` lets a walled harness choose the upstream port

`resolveTargetURL_internal` honours the `X-Target-Host` header: the
allowlist check strips the port, but `base.Host = override` keeps it, so
a harness can make the proxy connect to any port of the allowed host,
not the upstream's port. The wall-plus-proxy pair is the egress
allowlist, so the proxy must not widen it. Repair: refuse an override
that names a port, or drop the header entirely.

### 9. minor — `edge/forge/forge.go:397` — a present-but-empty rollup is "pending", never "absent"

`classifyRollup_internal` returns `CheckPending` whenever there are no
nodes, unless the rollup is missing entirely (`CheckAbsent` in
`parseChecksGraphQL_internal`). A commit whose rollup exists but has no
checks yet is therefore re-asked as pending without the absent case's
bounded wait (`ci_absent` until `Wait` in run), so a repository with no
CI can be polled indefinitely instead of faulting as no-CI. Repair:
return `CheckAbsent` when a present rollup has no contexts and is not
pending by state.

### 10. nit — `edge/git/git.go:523` — `Push` names the group-branch test `isStream`

`classifyPushRef_internal` reports whether a ref is under the group
prefix, but `Push` calls it `isStream`, which reads as the stream branch
(the thing that only fast-forwards). The logic is correct; the name
invites the next reader to lease the wrong branch. Repair: rename it to
`isGroup`.

## Promise gaps

- "`tla/Push.tla`, the cursor: events, cursor, sends, lost replies, a
  crash. Invariant: every event below the cursor was confirmed sent."
  (`docs/LAYER-4-EDGES.md:465`) — No such file exists and this layer
  holds no cursor; see Defect 5. Guarantee 2 is unmodelled where the doc
  says it is checked.
- "Checked by unit tests on a fake process table" for `edge.Exec`
  (`docs/LAYER-4-EDGES.md:20`) — Held for the fake table, but the unit
  tier also uses real time and real processes outside the functional
  files: `edge/break_test.go` sleeps, `edge/worker/worker_test.go` waits
  on `time.After`, and `edge/worker/coldread_test.go` execs a real
  command. The doc's Tests section (line 471) promises unit tests use
  "no clock, sockets, network or outside program".
- "A job directory is removed only after its report is recorded and its
  head is on origin" (guarantee 9, `docs/LAYER-4-EDGES.md:399`) — The
  dirty-check error path removes it without either check; see Defect 2.
- "A 403 or 429 with a retry time rests that identity until then: a fault
  to its caller that spends nothing" (`docs/LAYER-4-EDGES.md:231`) —
  `OpenPR` spends a create call after a rate-limited lookup instead of
  returning the rest; see Defect 3.
- "a failure is a typed result (conflict with files, push refused, host
  silent)" (`docs/LAYER-4-EDGES.md:27`) — `Fetch` types every failure as
  ref-not-on-origin; see Defect 4.
- "Every timeout is a policy number with a stated default"
  (guarantee 13, `docs/LAYER-4-EDGES.md:406`) — The hook claim wait,
  probe backoff and wake interval are constants in the worker
  (`WakeEvery`, `ProbeBackoff`, the 60 s claim wait in
  `edge/worker/hook.go`) with guessed defaults stated in comments, not
  policy numbers. They are named and bounded, so this is a wording gap,
  not an unbounded wait.

## Model vs code

- Missing the other way: the doc's `tla/Push.tla` and its witness
  `PushCursorBeforeConfirm` (`docs/LAYER-4-EDGES.md:545`) have no files;
  the cursor lives in run (layer 6), so layer 4's guarantee 2 has no
  model here. See Defect 5.
- `tla/LandEdge.tla:103` defines rung 1 as one card while the base is
  still its first tree (`Tip = Init0`); `edge/git/land.go:141`
  (`gatedHead_internal`) returns rung 1 whenever a single head contains
  the current base tip, at any base. One of them misstates the ladder:
  either the model's `Init0` is a proxy that was never revisited, or the
  code promotes later single heads to rung 1 that the model would gate
  as a group of one. The trace walk should say which.
- `tla/Runner.tla` `Stop` keeps a stopped job at `staged` with no
  outcome; the code's `stopped_internal` also resets a `reporting`
  record back to `taken` and clears its outcome. That reset is not an
  action in the model, so a job stopped mid-report and resumed is a code
  behaviour the model does not cover.
- The model's `Release` pushes once by lease before withdrawing; the
  code's no-receipt release path does the same, but the code also pushes
  a partial head on the fault path (`faultAndReportJob_internal`), which
  has no corresponding model action. The invariant it must keep
  (`NoPushAfterFinal`) is stated, but that push is unmodelled.
- Otherwise the models and code agree in shape: pass/job separation,
  leased pushes only while holding, retire-pushing-nothing, and
  at-most-once report by result id are present on both sides. The layout
  arithmetic the standing rule asks about (rings, wrapping counters,
  in-place compaction) does not occur in this layer; the capped buffers
  in `edge/exec.go` keep head plus tail within the cap and were checked
  by reading, with no off-by-one found.

## Simplifications

- `edge/git/git.go:200`: the per-tip fetch loop in the SHA lookup is the
  largest avoidable cost in the layer; one fetch of all tips followed by
  local ancestry checks replaces it and removes the unbounded wait in
  Defect 4 at the same time.
- `edge/worker/proxy.go:31`: the single global proxy status exists only
  to carry one integer from the proxy to the classifier; keying it by
  route removes both the field and the misattribution in Defect 7.
- Three near-identical "refused: … pass …" validations repeat across
  `edge/forge/forge.go`, `edge/git/git.go` and
  `edge/worker/verbs.go`; they are clear but could share one helper.
  This is a nit-level observation, not a defect: each message names its
  own fix, as the contract requires.

## Missing tests

- No test restarts the runner on a platform without `/proc` with a live
  adopted harness; Defect 1 has no coverage on any platform (the fake
  process table always knows its nonces).
- No test makes `IsDirty` fail during clean; the dirty-kept test covers
  only the dirty-true path, not Defect 2's error path.
- No test makes the `PR` lookup inside `OpenPR` fail; the existing forge
  tests cover create-then-find and the happy lookup, not Defect 3.
- No test bounds `LsRemote` on a many-branch remote or distinguishes a
  silent host from an absent ref (Defect 4).
- No test covers a panic inside `Deduplicator.Do` (Defect 6) or two
  concurrent jobs racing for the proxy status (Defect 7).
- NOT RUN in this session: the functional tier beyond the two failures
  quoted above (throwaway Redis with real `nova-bus`, scratch GitHub
  repository, real `nova-reserve`/`nova-sandbox` on a bench), and all
  TLA+ checks (no TLC available here). The two functional failures are
  environment absences, stated as such, not passes.

## Questions for the coordinator

- Is rung 1 "one head containing the current base tip" (the code) or
  "one head while base is its first tree" (the model)? The answer
  decides whether `tla/LandEdge.tla:103` or `edge/git/land.go:141`
  changes, and the trace binding should be pointed at it.
- Should the bus cursor's model live with run (layer 6), which owns the
  pusher, with layer 4's doc corrected — or is a layer-4 `Push.tla`
  still owed against the `Send` confirmation rule alone?
- The hook ready line is matched as `HOOK OK name=<row> ` with a
  trailing space (`edge/worker/hook.go`). Is that the exact line
  `nova-bus hook` prints when the name is its last field? Cold, this
  cannot be verified without the real binary, and a mismatch would make
  every start time out after 60 s.
- Are the worker's guessed constants (wake 10 s, probe backoff 30 s to
  10 min, claim wait 60 s) accepted as stated defaults under guarantee
  13, or should they move into policy before the first dogfood sprint
  measures them?


