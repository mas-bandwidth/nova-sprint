# Layer 4 edges — cold review (v2r-l4-flash2)

Base: `43d849e29d0e4568ba6a1348a076a59259639938` (branch `v2-stitch`), read on
`2026-11-07`. Scope: `edge/**`, `cmd/nova-worker/**`, `docs/LAYER-4-EDGES.md`,
`docs/SEAMS.md`, `tla/{LandEdge,Runner}.tla` and their walks.

## Verdict

This layer is **not sound as shipped**. Its shape is right — one `Exec`, small
typed outcomes, fakes that keep the contracts, a `LandPass` that gates the exact
sha and merges only while the base is unchanged, and a worker that mostly keeps
its careful restart story — but two central promises are broken at the edges:
a job directory is deleted even when its commits never reached origin, and the
wall's `--read` allowlist is computed from files inside the card's own write
tree, so a card can widen the sandbox to any host directory. The single worst
thing is the deletion path in `cleanJobDir_internal`: a failed push leaves
`PushedHead` empty, the worktree is clean because the harness committed, and
the directory is removed, silently destroying the only copy of the work
(guarantee 9).

Ran (Go 1.26.6):

```
$ go build ./...
(no output; exit 0)
$ go vet ./...
(no output; exit 0)
$ go test ./edge/... ./cmd/nova-worker/...
ok   github.com/mas-bandwidth/nova-sprint/edge            0.742s
ok   github.com/mas-bandwidth/nova-sprint/edge/bus        0.217s
ok   github.com/mas-bandwidth/nova-sprint/edge/forge      0.009s
ok   github.com/mas-bandwidth/nova-sprint/edge/git        1.066s
ok   github.com/mas-bandwidth/nova-sprint/edge/worker     4.166s
--- FAIL: TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot (0.20s)
    break_test.go:80: no report reached nova-sprint (exit 0, stderr "", sprint log "unhook --as r1\nhook --as r1 --role worker\nbeat --as r1 --load1 12.65 --cores 64 --slots-used 0 --slots-total 0 --tiers flash,pro,heavy,frontier\nnext --as r1 --json\nnext --as r1 --json\n")
FAIL github.com/mas-bandwidth/nova-sprint/cmd/nova-worker 0.215s
```

`go test -race ./edge ./edge/bus ./edge/forge ./edge/git ./edge/worker` is green.

## Defects

Ranked, most severe first. Each cites the lines that contain the defect.

1. **blocker** — A failed push is followed by deletion of the committed tree.
   `edge/worker/runner.go:607-631` (`harvestWorkHead_internal`) calls
   `pushBranch_internal`; on error `rec.PushedHead` is left empty (it is only
   set inside `pushBranch_internal` on success, `runner.go:34-42`), and the
   function returns an `ok` as a `fault` or returns the `fail` unchanged. Then
   `sendReportAndClean_internal` (`runner.go:702-734`) unconditionally calls
   `cleanJobDir_internal` (`runner.go:735-760`), whose work-job origin guard is
   `if rec.Kind == "work" && rec.PushedHead != ""` (line 737). With `PushedHead
   == ""` that guard is skipped, `IsDirty` is false because the harness
   committed, and `RemoveWorktree` + `os.RemoveAll(jobDir)` delete the only copy
   of the commits. **Why it matters:** violates guarantee 9 ("A job directory is
   removed only after its report is recorded and its head is on origin;
   committed work is pushed before any non-ok end", `docs/LAYER-4-EDGES.md:399-400`)
   and runner step 5 ("push whatever commits the worktree holds", lines
   275-278). **Repair:** on any push failure keep the directory (report the
   fault but skip clean), or make `cleanJobDir_internal` verify the worktree's
   current HEAD (not `PushedHead`) is on origin before removing.

2. **serious** — A failed dirty check removes the worktree anyway.
   `edge/worker/runner.go:747-756`: `if dirty, err := r.Git.IsDirty(...); err ==
   nil && dirty { keep } else if err != nil { log "dirty check failed,
   removing" }`, then it falls through to `RemoveWorktree` and `os.RemoveAll`.
   **Why it matters:** a `git status` timeout, a broken index, a missing git or
   a cancelled context turns "I cannot tell" into "delete"; guarantee 9 and
   step 9 ("a worktree with uncommitted changes is logged and kept until the
   next pass", lines 290-292). **Repair:** when dirtiness is unknown, keep the
   directory and raise the error; only remove after a successful clean check.

3. **serious** — The wall's `--read` allowlist trusts a file the card can write.
   `edge/worker/runner.go:1097-1115` (`effectiveConfig_internal`) reads
   `<job>/repo/.git` (line 1101) and `<job>/repo/.git/objects/info/alternates`
   (line 1108), both under the job directory that `BuildWallArgv` grants
   `--write` (`edge/worker/harness.go:365-375`), and appends their parent
   directories to `cfg.ReadPaths`. A harness can append `/etc/anything` (or set
   a `gitdir:` file) and the next `RunHarness`/`RunTest` invocation passes
   `--read /etc` to `nova-sandbox`. **Why it matters:** the wall is documented
   as `--write` the job directory and `--read` toolchains/siblings (lines
   271-274); a card escaping its read confinement can read host files (other
   repos, a home directory) and put them in a report or a commit. **Repair:**
   derive read paths from the worker's own staging record (the work clone and
   mirror roots it created), never from files under `JobDir`; validate every
   path is under those roots.

4. **serious** — Provider statuses are global across every route and job.
   `edge/worker/proxy.go:31,35-45` keeps one `lastStatus atomic.Int32` and
   `ConsumeStatus` swaps it for whoever asks; `runner.go:559,585` and
   `probe.go:203` call it after a run. `runJobs_internal` starts jobs
   concurrently (`edge/worker/pass.go:270-289`). A 402/429/5xx recorded by job
   A's route is consumed by job B, so B can be reported `fault` while A's real
   provider failure is lost; and `cmp.Or(job.ProxyStatus, r.Proxy.ConsumeStatus())`
   evaluates `ConsumeStatus()` even when `job.ProxyStatus` is already set, so a
   second concurrent status is swallowed during harvest. **Why it matters:**
   outcome classification is per job ("a read's provider failure is a fault",
   "results classified by the adapter", lines 294-304); a false fault spends no
   attempt but masks a real failure and can convert a real `fail` into a
   `fault` or vice versa. **Repair:** key the recorded status by route (or by
   job/request) and consume only that job's provider statuses; reset the
   per-job status before each launch.

5. **serious** — A corrupt or partial job directory wedges a working card
   forever. `edge/worker/runner.go:226-238` (`loadRowJobs_internal`) only adds a
   disk entry when `ReadJobRecord` returns `err == nil` (line 233); a malformed
   `job.json`, or a crash after `initJob_internal` made the directory but before
   `WriteJobRecord`, is silently skipped. `planPass_internal`
   (`edge/worker/pass.go:215-218`) then skips every `next.Working` consumer
   whose directory exists, and `recoverRow_internal` (`recover.go:51-78`) has no
   record to resume or release. **Why it matters:** the documented restart rule
   is "A `working` consumer with no job directory, or one whose job cannot be
   rebuilt, is released with no blame" (lines 364-365); here the card stays
   `working` with no job and no warning. **Repair:** surface the record error
   and treat it as "cannot be rebuilt" (release with no blame, or
   quarantine/raise), and remove partial directories when `initJob_internal`
   fails after creating them.

6. **serious** — Restart adoption only works on Linux. `edge/worker/runner.go:93-104`
   (`OSProcessTable.Alive`) first checks an in-memory `liveProcNonces` map, then
   reads `/proc/<pid>/environ`. A new worker process has an empty map, and
   macOS/BSD have no `/proc`, so `Alive` is always false there. **Why it
   matters:** "A `working` consumer whose harness is alive is adopted by pid and
   nonce" (lines 358-361) and guarantee 7 ("One live harness per job
   directory", line 397); on a macOS bench a service restart will believe the
   live harness is gone, resume the job, stage again and can start a second
   harness in the same directory. **Repair:** read the process environment
   portably (`ps eww -p <pid>` on darwin/BSD, or a pid+nonce marker checked with
   a portable API) instead of `/proc`.

7. **serious** — `claude` and `codex` are never given their credential
   directory. `edge/worker/harness.go:76-82` declares `ConfigDir`, but
   `NewHarness` (`harness.go:59-72`) returns `&ClaudeHarness{}` and
   `&CodexHarness{}` with empty fields, and production wiring
   (`edge/worker/serve.go:79-98`) never sets them; the only assignments are in
   `edge/worker/probe_test.go:224`. Consequently `Command`
   (`harness.go:146-162`) never adds `CLAUDE_CONFIG_DIR`/`CODEX_HOME`, and the
   wall sets `HOME` to `<job>/home`, so the default `--harness claude` has no
   login. **Why it matters:** the doc promises "A harness that logs in (claude,
   codex on a subscription) keeps its credentials in its own store, granted
   read-only into the wall" (lines 326-328); the adapters exist but are
   unusable in production. **Repair:** pass the config directory in from a flag
   or the service environment and grant it to the wall, or say only opencode is
   supported.

8. **serious** — The layer's test suite is red, and the failing test asserts the
   old ready/PR contract. `cmd/nova-worker/break_test.go:59` puts the card under
   `"ready"`, but the worker now only starts `next.Working` (deal is take,
   `edge/worker/pass.go:211-222`); line 61's fake `report` returns
   `{"accepted":true}` with no `op` (only accepted because of defect 9); line 88
   expects `next` as the third sprint verb, but `ServeRow` now advertises with
   a `beat` before the pass (`edge/worker/runner.go:390-393`,
   `edge/worker/beat.go:164-171`). Output is in the Verdict block above.
   **Why it matters:** `go test ./cmd/nova-worker/...` is red, and the test
   still encodes "ready" and "report needs no op". **Repair:** list the consumer
   under `working`, return a receipt with `op` equal to the requested id, and
   accept/assert the `beat` before `next` (or delete the obsolete breaker).

9. **minor** — A missing receipt `op` is silently replaced with the requested
   id. `edge/worker/verbs.go:216-221` ends with `rec.Op = cmp.Or(rec.Op, id)`,
   and `sendReportAndClean_internal` then checks `receipt.Op == req.ResultID`
   (`runner.go:726`). A sprint that prints `{"accepted":true}` (or ignores
   `--id`) is treated as having echoed the id, so the record is marked recorded
   and the directory cleaned. **Why it matters:** the documented rule is "an
   accepted store receipt matching the result ID" (line 543); the default takes
   away the check. **Repair:** do not fill `Op`; require the verb to echo it,
   and keep the record when it does not.

10. **minor** — `MergeGroup` swallows an `IsAncestor` error. `edge/git/git.go:267-278`:
    `if ok, err := c.IsAncestor(...); err == nil && ok { ... }` — when the
    ancestor check errors, the code continues into the general merge path
    instead of returning the error. **Why it matters:** an error is silently
    turned into a guess about ancestry; the merge then either succeeds for an
    unrelated reason or fails with a less useful message. **Repair:** return the
    error (a fault) rather than falling through.

11. **minor** — `initJob_internal` drops `MkdirAll` errors for `tmp` and `data`.
    `edge/worker/runner.go:444-450`: `if err := os.MkdirAll(...); err != nil &&
    sub == "home" { return ... }`. A failure to make `<job>/tmp` or `<job>/data`
    leaves the job to run with `TMPDIR`/`XDG_DATA_HOME` pointing at missing
    directories and leaves a partial job directory behind (which then hits
    defect 5). **Why it matters:** a silent failure is later reported as a
    harness failure, and the partial directory is never cleaned. **Repair:**
    return any `MkdirAll` error and remove the partial directory.

## Promise gaps

Each item quotes the doc sentence and says what the code does.

- **"A job directory is removed only after its report is recorded and its head
  is on origin; committed work is pushed before any non-ok end."**
  (`docs/LAYER-4-EDGES.md:399-400`.) `cleanJobDir_internal` skips the origin
  check when `PushedHead == ""` (`edge/worker/runner.go:737`) and removes a
  clean, committed worktree; a failed push leaves `PushedHead == ""`. See
  defects 1 and 2.

- **"A `working` consumer with no job directory, or one whose job cannot be
  rebuilt, is released with no blame."** (lines 364-365.) A directory whose
  `job.json` is unreadable is dropped by `loadRowJobs_internal`
  (`edge/worker/runner.go:226-238`) and then skipped by `planPass_internal`
  (`edge/worker/pass.go:215-218`); it is neither resumed nor released. See
  defect 5.

- **"Workers push and open PRs with their row's token."** (lines 218-219; also
  "push the branch with the row's token", lines 275-276.) The worker's git
  runner (`edge/worker/runner.go:1056-1068`) runs with `CleanEnv: true` and only
  `PATH`, `HOME` and `GIT_TERMINAL_PROMPT`; `OSGitOps` has no token field and
  `newOSRunner_internal` passes none (`edge/worker/serve.go:87-98`). The push
  therefore authenticates with whatever the bench's `HOME` git/ssh config
  provides, or fails; it is not the row's token. Either the doc or the code must
  change.

- **"Stage a worktree ... from the bench's work clone, which borrows the
  mirror's objects and fetches from the mirror, never the network."**
  (lines 257-258.) `OSGitOps.Fetch` runs `git fetch origin ...` in the work
  clone (`edge/worker/runner.go:890-905`), and the work clone's origin is the
  stream's clone URL (`runner.go:866-885`), i.e. the network. The later note at
  line 872 says the same thing the code does ("its fetch ... go to the repo"), so
  the document contradicts itself; code follows the later note, not the promise.

- **"A harness that logs in (claude, codex on a subscription) keeps its
  credentials in its own store, granted read-only into the wall."**
  (lines 326-328.) `ConfigDir` is never set in production (defect 7), and when it
  did get set the adapter puts it in `Needs.Writes`, not a read path
  (`edge/worker/harness.go:107-114`).

- **"Run under `nova-reserve run --slots <weight>` ..."** (line 267.) `Job.SlotWeight`
  is declared (`edge/worker/worker.go:74`) and honoured by `BuildWallArgv`
  (`edge/worker/harness.go:356-358`), but no production path ever sets it:
  `buildJob_internal` (`edge/worker/runner.go:462-487`) does not, and the take
  receipt has no weight. The harness still gets a nova-reserve slot, but not the
  documented weight; nova-reserve falls back to its own capacity rule.

- **"The wall: `--write` the job directory ..., `--read` toolchains and
  siblings ..."** (lines 271-274.) `effectiveConfig_internal` adds parent
  directories of paths read out of the card-writable `.git`/`alternates` files
  (`edge/worker/runner.go:1097-1115`). See defect 3.

## Model vs code

- **`tla/Push.tla` is cited but does not exist.** `docs/LAYER-4-EDGES.md:23`
  says the bus is "Checked by `tla/Push.tla`", and lines 465-467 describe the
  module, but `ls tla/Push.tla` is empty and the stitch note at lines 585-587
  says it was deleted with its witness. The current model list is stale.
- **`Runner.tla` requires a push before clean; the code does not.**
  `tla/Runner.tla:194-200` (`Clean`) requires `jobDir[c].pushed`, and
  `Push` (`:156-165`) sets it. `edge/worker/runner.go:735-760` removes a work
  job with `PushedHead == ""` (defects 1-2), so the code has a clean the model
  forbids.
- **`Runner.tla` models no refused report.** The doc admits it
  (`docs/LAYER-4-EDGES.md:666-667`), while `edge/worker/recover.go:212-231`
  implements count-and-quarantine after `RefusalLimit` identical refusals. The
  keep-and-resend/quarantine behaviour is only covered by a unit test.
- **`Runner.tla`'s `Release` only exists after a crash.** `tla/Runner.tla:273-288`
  requires `env.crashes > 0`, while `releaseNoBlame_internal`
  (`edge/worker/recover.go:151-158,184-202`) is reached by any pass that cannot
  rebuild a job (including a directory from before receipts existed).
- **`LandEdge.tla`'s rung rule is narrower than the code.** `RungOf`
  (`tla/LandEdge.tla:87-88`) is rung 1 only when the request has one card *and*
  the base is still the initial commit; `gatedHead_internal`
  (`edge/git/land.go:141-158`) chooses rung 1 whenever the single head contains
  the current base tip. A rebased card branch on a moved base is rung 1 in code
  and rung 2 in the model. The invariants are unaffected, but this is uncovered
  behaviour.
- The two walks are green (`TestLandEdgeTraceRefinementWalk`,
  `TestRunnerTraceRefinementWalk`), so these are gaps in what is modelled, not
  failing trace checks.

## Simplifications

- **`Job.SlotWeight` is dead.** `edge/worker/worker.go:74`,
  `edge/worker/harness.go:356-358`; nothing in production assigns it. Remove it
  (and document nova-reserve's default) or populate it from policy/receipt.
- **`JobCIWait` is dead.** Declared at `edge/worker/worker.go:40` and read at
  `edge/worker/recover.go:119`, but no code ever writes it (the worker no
  longer waits on CI). Delete it and its recovery case.
- **The linked-worktree branch in `effectiveConfig_internal` is a dead
  special case and the dangerous widening path.** `edge/worker/runner.go:1101-1106`
  handles a `.git` *file*, but job worktrees are `git clone --shared` and have a
  `.git` directory. Delete it and derive the read paths from the worker's own
  clone/mirror record (also fixes defect 3).
- **The key proxy has three route selectors and an extra host override.** It
  routes by `X-Nova-Route`, by the first path segment, and by
  `DefaultRoute`/the single route (`edge/worker/proxy.go:84-104`), and it lets a
  request rewrite the upstream host with `X-Target-Host` (`proxy.go:208-223`).
  The doc describes only the path-prefix route (`ProxyBaseURL`); keep that and
  drop the headers.
- **`FormatResultID`/`FormatTakeOpID` ignore parameters their doc says they
  use.** `edge/worker/worker.go:31-36` ignores `attempt` and `row`, while
  `docs/LAYER-4-EDGES.md:540` documents `res-e<epoch>-<consumer>-a<attempt>-g<gen>`
  and `take-e<epoch>-<consumer>-g<gen>-<row>`. It is safe today only because the
  consumer id already carries the attempt; either remove the parameters or make
  the ids match the document.
- **`FakeExec` still ships in production, and the guard test cannot see it.**
  `FakeExec`, `FakeOutcome`, `FakeCall` and `FakeHandler` live in
  `edge/request.go:47-69,102-197`, while
  `edge/coldread2_test.go:63-71` only reads `exec.go` for `type FakeExec`.
  Move the fake to a test/helper package or widen the guard.

## Missing tests

- A failed push on an `ok` and on a `fail` work job keeps the job directory and
  its commits (guarantee 9). `fakeGit.pushErr` exists
  (`edge/worker/worker_test.go:55,117`) but no test sets it.
- A failed `IsDirty` keeps the worktree instead of removing it
  (`runner.go:747-756`).
- Two concurrent jobs with different provider routes keep their own
  `ProxyStatus` (no cross-talk).
- A corrupt/missing `job.json` or an empty partial job directory is released or
  raised, not skipped forever.
- Restart adoption on a non-Linux host (a portable `Alive`), not only the
  in-memory/fake process table.
- Production wiring of `ClaudeHarness.ConfigDir`/`CodexHarness.ConfigDir` (or a
  test that a claude row can find a login).
- The worker's push carries the row's token (or an explicit test that the
  documented host-identity behaviour is the intended one).
- A card-writable `.git`/`alternates` cannot add `--read` paths outside the
  clone/mirror roots.
- The `cmd/nova-worker` test should be updated, not left red (defect 8).
- `Runner.tla` has no refused-report behaviour; if that rule matters, add it to
  the model, not just `recover.go`.

## Questions for the coordinator

1. Is the worker meant to push with a row-scoped token, as lines 218-219 and
   275-276 say, or with the bench's host git/gh identity, as the code does?
   This decides whether the missing token is a defect or stale documentation.
2. Where should `CLAUDE_CONFIG_DIR`/`CODEX_HOME` come from on a bench — a new
   flag, the service environment, or nova-fleet's play? Nothing in this layer
   supplies it today.
3. On macOS/BSD, what is the intended restart-adoption mechanism if `/proc` is
   unavailable? I could not decide whether the expected fix is `ps`, a pid+nonce
   file, or accepting that adoption is Linux-only.
4. Does omitting `--slots` satisfy guarantee 8 on nova-reserve's current default
   capacity, or must the take receipt carry a slot weight? I did not assume an
   answer.
5. Is the mirror-fetch promise (lines 257-258) superseded by the later note
   (line 872) that the work clone's origin is the repo? The code and the later
   note agree; the earlier sentence does not.
6. For an unreadable job directory, should the recovery be a no-blame release or
   a quarantine-and-raise? The doc says release for "cannot be rebuilt"; a
   corrupt record may deserve the quarantine path because it is not the card's
   fault either.
