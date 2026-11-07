# Layer 4 (the edges): cold review by rowan (Fable 5.1), 2026-11-07

Base `43d849e2` on `v2-stitch`. Read: `docs/LAYER-4-EDGES.md`, `docs/SEAMS.md`,
`docs/ENFORCED.md` (it ledgers layers 1, 2, 5 only; no layer-4 row), every file
of `edge/**` and `cmd/nova-worker/**` (non-test lines: edge 880, bus 138, git
1110, forge 833, worker 4199, cmd 55), the tests, `tla/Runner.tla`,
`tla/RunnerTrace.tla`, `tla/LandEdge.tla`, `tla/LandEdgeTrace.{tla,cfg}`,
`tla/Runner.cfg`. Extra weight on the worker's job lifecycle and the forge
and landing edge, as asked.

Commands run (darwin/arm64, go1.26.10), first lines:

```
$ go build ./... ; echo build exit $?      -> build exit 0
$ go vet ./... ; echo vet exit $?          -> vet exit 0
$ go test -count=1 ./edge/                 -> ok   0.841s
$ go test -count=1 ./edge/bus/             -> FAIL 5.188s  (TestFunctionalNovaBusWhoAndSend: "Condition never satisfied ... expected sprint and amd-1.ds in nova-bus who")
$ go test -count=1 ./edge/git/             -> ok   2.766s
$ go test -count=1 ./edge/forge/           -> ok   0.123s  (TestFunctionalScratchForge skipped: NOVA_FORGE_SCRATCH_TEST unset)
$ go test -count=1 ./edge/worker/          -> ok   8.526s
$ go test -count=1 ./cmd/nova-worker/      -> FAIL 8.405s  (TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot: "no report reached nova-sprint (exit 0 ... sprint log "unhook ... next --as r1 --json\nnext --as r1 --json\n")")
```

Two packages are red at the base commit (defects 22 and 23 below). The
"unit" run is not clean of real time, sockets or processes: `edge/bus`
starts a throwaway `redis-server` and the real `nova-bus` with no gate but
binary presence (`edge/bus/functional_test.go:20-70`); `edge/worker` runs
real git and shell processes in `TestFunctionalRunnerEndToEndWithRebase`
(4.16 s) and `TestFunctionalRunnerRebaseConflict` (4.08 s) in the default
run; `edge/break_test.go:45` sleeps 100 ms of wall time;
`edge/worker/break_test.go:244` reads `time.Now()`. None is `-short`-gated
or tagged, so `go test ./edge/...` is the functional tier whether the
caller wanted it or not.

## Verdict

The layer is close to sound in its happy path and its git mechanics are
careful (the lease on every push, the group branch, the retire path, the
sanitised wall environment, the key proxy), but the worker's job lifecycle
does not keep three of its own guarantees at the edges where a process
dies or is stopped, and the model that is supposed to bind it is mostly
re-enacted by the test rather than driven through the runner. The single
worst thing: `OSProcessTable.Alive` (`edge/worker/runner.go:93-104`) can
only prove a harness alive from in-process memory or `/proc/<pid>/environ`,
so on every macOS bench (the Mac Studio, the minis) a restarted worker never
adopts a live harness; `prepareResume_internal` then treats the running
record as "harness gone" and runs the harness again, two harnesses on one
job directory, which is exactly the reversed witness
`RunnerRestartSpawnsSecondHarness` the model promises cannot happen
(guarantee 7). Close behind it: a stop that arrives during rebase, tests or
push is recorded and resent as a `fail` or `fault` (guarantee 11,
`StopIsNeverAFault`), and the landing turns every transient forge failure
(a 429, a 30 s `gh` timeout) into a judgement per card instead of the
spent-nothing fault the doc promises.

## Defects

Ranked, most severe first. Each: file:line, the defect, why it matters,
the repair.

1. **serious** `edge/worker/runner.go:93-104` (`OSProcessTable.Alive`).
   After `syscall.Kill(pid, 0)` the nonce is proved only from
   `liveProcNonces_internal` (an in-process `sync.Map`, empty after a
   restart) or `/proc/<pid>/environ`, which does not exist on darwin or the
   BSDs; so after a restart on a Mac bench `Alive` is false for a live
   harness. `edge/worker/recover.go:122-123` then never adopts, and
   `edge/worker/recover.go:168-171` resumes the `running` record at `staged`
   with `PID 0`; `edge/worker/pass.go:392-394` runs the harness again while
   the first still runs in its slot. Guarantee 7 ("One live harness per job
   directory") and `Runner.tla:289-293` (`Restart` adopts) are broken on the
   fleet's own Macs; the two harnesses then race on one worktree and one
   `result.json`. Repair: prove the nonce with a per-platform read
   (`ps -o command= -p <pid>` or `sysctl kern.procargs2` on darwin; keep
   `/proc` on Linux) and refuse to resume a `running` record whose pid is
   alive but unprovable, raising it instead of re-running.

2. **serious** `edge/worker/runner.go:650-701` (`rebaseAndTestStep_internal`)
   and `edge/worker/pass.go:392-400`. The run step checks `ctx.Err()` before
   harvest (`pass.go:429-430`) but nothing after it does: a stop (ctx
   cancelled) during `RebaseOntoStream` (`runner.go:656-671`, up to 2 min),
   the DONE-WHEN tests (`runner.go:676-688`, 2 min each) or the push
   (`runner.go:689-697`, 60 s) is an exec error classified `canceled`, which
   this code writes to the record as `fail: rebase failed: ...`, `fail:
   tests red (...): ... stopped by its caller` or `fault: post-rebase push
   fault` at state `reporting` (`runner.go:668-670, 683-684, 693-694`,
   `sendReportAndClean_internal` at `runner.go:703-706` writes `reporting`
   before the send fails on the dead ctx). The next start finds `reporting`
   and resends it (`recover.go:119-121`): a worker stop spends a bounce
   (`fail`) or a fault. The same window exists in
   `harvestWorkHead_internal` (`runner.go:621-626`, a push on a cancelled
   ctx is `fault: push fault`). This breaks guarantee 11, the doc's "A job
   the worker's own stop cuts short is 'runner stopped': not reported"
   (`LAYER-4-EDGES.md:303-304`) and `Runner.tla:402-405` `StopIsNeverAFault`
   (the model's `Rebase`, `Conflict`, `Push` are atomic, so TLC cannot see
   it). Repair: after each git or wall call in harvest, rebase, test and
   push, `if ctx.Err() != nil { return r.stopped_internal(p) }` before any
   outcome is written, and make `stopped_internal` reset a `harvested`/
   `rebased` record's outcome the way it resets `reporting`.

3. **serious** `edge/worker/proxy.go:31, 35-45, 258-267` with
   `edge/worker/runner.go:559, 585` and `edge/worker/probe.go:203`. The key
   proxy keeps one `lastStatus` for the whole worker and `ConsumeStatus`
   swaps it to 0; with width 2 or more (and the probe beside a job) the
   provider status of job A's requests is consumed by whichever
   classification runs first. A 503/429 that hit A is read by B (B with no
   verdict becomes `fault: provider HTTP 503`, retried under the launch
   grace, instead of its `fail: no verdict`), and A then sees 0 and reports
   `fail: no verdict`, a bounce for a provider fault (guarantee 11, "a
   rate-limit or credit refusal" is a fault, `LAYER-4-EDGES.md:298-300`).
   Repair: key the recorded status by the route and the requesting job (the
   proxy already has the first path segment; add the consumer as a path
   segment or a header the worker sets in `ProxyBaseURL`), and consume per
   job.

4. **serious** `edge/worker/runner.go:866-886` (`workClone_internal`) under
   the 120 s stage context (`runner.go:491`, `pass.go:314`). The first use of
   a repository runs `git clone --mirror` inside `Timeouts.Stage`; a
   repository whose mirror clone takes longer (rocketnet, the VG assets) is
   killed by the process-group SIGKILL, leaving a half-written
   `<mirror-root>/<name>.git`. The next pass finds `fileExists_internal(mirror)`
   true (`runner.go:869`), skips the clone and runs `git clone --reference
   <broken mirror>` (`runner.go:876-879`), which fails on every pass until
   someone removes the directory by hand; every card of that stream dealt to
   the bench faults forever ("staging fault"), and nothing names the mirror
   as the cause. Repair: clone the mirror into a temporary directory and
   rename it into place, give the first-use clone its own bound (not the
   per-job stage timeout), and on failure remove what was left.

5. **serious** `edge/git/land.go:194-202, 223-227, 236-241` with
   `edge/forge/forge.go:179-197, 204-222`, `run/edges.go:177-183` and
   `steps/land.go:20-21, 74-91`. Every forge error inside `LandPass` is an
   outcome (`pr_unopenable`, `merge_refused`, `ci_fault`), and run hands
   every non-landed outcome and every error to layer 3, which opens one land
   judgement per card. So a token-bucket or GitHub rate limit
   (`forge.FaultError`, `forge.go:184, 193, 219`), a 30 s `gh` timeout or
   a network blip becomes a human question for every card in the group,
   answered only by `retry`. The doc promises the opposite: "a rate limit
   rests the identity and fails no card" (`LAYER-4-EDGES.md:30-31`), "A 403
   or 429 ... a fault to its caller that spends nothing" (`:229-232`), "A
   landing refuses for this pass, spending nothing ... a typed result that
   layer 3 counts toward the stall alarm" (`:166-171`). Repair: return a
   `*forge.FaultError` (and any `edge.ExecError` of kind timeout/canceled)
   as an error from `LandPass`, and have `askAgain_internal`
   (`run/edges.go:158-173`) ask the pass again after `RetryAfter` for those,
   bounded by the request's `Wait`, before any judgement.

6. **serious** `edge/worker/recover.go:62-76` with `edge/worker/pass.go:216`
   and `edge/worker/retire.go:161-164`. A job directory from an earlier
   epoch (the sprint was cleared and card ids reused, which the doc says
   happens, `LAYER-4-EDGES.md:1135-1137`) is never in `next.Final` of the new
   epoch, fails `sameEpoch_internal`, and is logged
   `identity-mismatch-kept` every pass ("An epoch mismatch is still kept",
   `:1165`); meanwhile `planPass_internal` skips any card whose directory
   exists, so the same-named card dealt to this row in the new epoch is never
   taken, never released, and sits in the row's hand until the sprint's
   silence rules withdraw it, every attempt. Repair: retire a record whose
   epoch is below `next.Epoch` the way a stale generation is retired
   (`recover.go:68-69`), keyed by epoch in `RetiredDir` as it already is.

7. **serious** `edge/worker/runner.go:105-119` (`OSProcessTable.Kill`).
   SIGTERM (`:109`) and SIGKILL (`:116`) go to `pid` alone, the direct child
   `nova-reserve` started with `Setpgid` (`edge/exec.go:112`); the live
   harness is a grandchild under `nova-sandbox`. `edge.Exec` kills the group
   (`exec.go:113-119`) but the adopted-harness path
   (`recover.go:133-134`, a harness past its deadline) and the withdraw path
   (`runner.go:255-256`) use `Kill`. Unless `nova-reserve` forwards signals
   and waits for its child, the harness runs on without its slot (guarantee
   8, "freed when it ends") after the worker reported the fault. Repair:
   signal `-pid` (the group `edge.Exec` created) as `exec.go` does, then the
   pid.

8. **serious** `edge/worker/trace_test.go:286-341, 457-530` with
   `tla/make-traces.sh:35`. The "trace walk" that binds the worker to
   `Runner.tla` writes the job record by hand for `Stage` (`:307-316`), sets
   the pid and state itself for `Run` (`:317-333`), writes the outcome for
   `Harvest`/`Die` (`:457-477`), rebases, conflicts, reports (its own store
   op at `:513-520`) and cleans (`os.RemoveAll` at `:525`) by hand; only
   `stopped_internal` (`:383`), `pushBranch_internal` (`:405, 497`) and
   `finalJob_internal` (`:442`) are the runner's code. The projected states
   therefore match the model by construction, and the walk cannot find
   defects 1, 2 or 6, which all live in the code the walk re-enacts. Glenn's
   rule ("the code is bound to the model or the model does not count") is
   not met for the lifecycle; `driveRunnerEntrypoints_internal` (`:755-791`)
   runs the real runner once but records nothing from it. Repair: drive
   each step through the runner (`prepare_internal`, `stageStep_internal`,
   `runAndHarvest_internal`, `rebaseAndTestStep_internal`,
   `sendReportAndClean_internal`, `cleanJobDir_internal`, a stop via ctx)
   against the fake wall, git and sprint, and project the record the runner
   wrote.

9. **serious** `edge/worker/harness.go:107-115, 146-162` with
   `edge/worker/serve.go:80` and `cmd/nova-worker/main.go:26-35`. The claude
   and codex adapters grant `--write` on `h.ConfigDir` and export
   `CLAUDE_CONFIG_DIR`/`CODEX_HOME` only when `ConfigDir` is set, but
   nothing sets it: `NewHarness` returns `&ClaudeHarness{}` and there is no
   flag. Inside the wall `HOME` is `<job>/home` (`harness.go:434, 461`), so
   on a Linux bench `claude` finds no `~/.claude/.credentials.json` and
   `codex` no `~/.codex`; the probe fails on every backoff and the row never
   hooks. The doc says their credentials are "granted read-only into the
   wall" (`LAYER-4-EDGES.md:326-328`). Repair: a `--harness-config <dir>`
   flag (or the seat's `CLAUDE_CONFIG_DIR`/`CODEX_HOME` read from the
   worker's environment) that fills `ConfigDir`, granted `--read` not
   `--write` as the doc says, and a probe refusal that names it when absent.

10. **serious** (promise) `edge/worker/runner.go:1056-1068`
    (`runGit_internal`). The worker's git runs with `PATH`, the bench user's
    `HOME` and `GIT_TERMINAL_PROMPT=0` only; no token and no credential
    helper is configured, so a push to `https://github.com/...` is answered
    by whatever the bench user's `~/.gitconfig` holds (a keychain, `gh auth
    setup-git`), or fails with "could not read Username" as a `push fault`
    on every card. The doc promises "push the branch with the row's token"
    (`LAYER-4-EDGES.md:275-276`), "Workers push ... with their row's token"
    (`:218`), "The git token is used only by the runner, outside the wall"
    (`:328-329`); the run's git edge has `Config.Token`/`GitHubCredential`
    (`edge/git/git.go:78-83, url.go:84-91`) and the worker's git has
    neither. Repair: give `OSGitOps` the same `credentialEnv_internal`
    (gh as the host's login, every GitHub credential unset) or a token from
    the seat, and have the probe push nothing but `git ls-remote` the
    stream's repo so a missing credential refuses at the probe, not per card.

11. **minor** `edge/worker/runner.go:1159-1163` and `:570-573`. "No slot" is
    detected by `strings.Contains(combined, "nova-reserve:")` on the
    harness's combined stdout/stderr with exit 1, and an infrastructure
    fault by `"SANDBOX REFUSED"` in the output; a harness that exits 1 and
    prints `nova-reserve:` (its `command.txt` is in its job directory, and
    an agent that quotes an error from `ps` will) is "no slot" and run again
    on every pass for ever, never a result. The doc says the classifier
    never substring-scans agent prose (`:536`). Repair: have `nova-reserve`
    exit with a distinct code for "no slot within wait" and read the code.

12. **minor** `edge/forge/forge.go:443-449` with `edge/git/land.go:223-227`.
    `gh pr merge` exits 0, then `gh pr view` fails (a timeout, a 5xx): the
    error reads "merged PR ... but could not read its merge commit" and
    `LandPass` reports `merge_refused` ("GitHub refused the merge"), a
    judgement per card, though base moved. The next pass would find every
    head an ancestor of base and land, but only after a human answers
    `retry`. Repair: on a view failure after a successful merge, return an
    error (fault, asked again), not a refusal; or read the merge commit from
    `git ls-remote` of base.

13. **minor** `edge/forge/forge.go:412-414`. A rollup with one cancelled or
    timed-out check and the rest still queued is `CheckFault` at once (the
    `pending` flag is dropped on that branch), so `LandPass` answers
    `ci_fault` while the run goes on; a re-run of the cancelled job never
    gets read. Repair: return `CheckPending` (or `Waiting: true`) while any
    node is pending, fault only when nothing is.

14. **minor** `edge/worker/runner.go:611-613` with `:1003-1021`. A harness
    that says ok, commits some work and leaves the rest uncommitted has
    `sha != base && sha != prior`, so the tree is not committed; the pushed
    head lacks the dirty files, the DONE-WHEN tests then run in the dirty
    worktree (`:676-688`, a tree that is not the head), and `git rebase`
    refuses a dirty tree, so the card is bounced with `fail: rebase failed`
    for a worker-side condition. Repair: commit a dirty tree whenever the
    harness said ok (not only when HEAD is at base), or fail before the
    rebase naming the uncommitted files.

15. **minor** `edge/worker/runner.go:226-238` with `pass.go:216`. A job
    directory whose `job.json` does not parse (or is missing after a crash
    between `MkdirAll` and the record, `runner.go:445-454`) is silently
    dropped by `loadRowJobs_internal` (`:233`, `err == nil`), while
    `planPass_internal` skips the card because the directory exists: the
    card is neither resumed, released nor raised. Repair: a directory with
    no readable record is quarantined and raised like a refused one.

16. **minor** `edge/worker/verbs.go:143, 147, 201, 224`. `CLISprintClient.Epoch`
    is written by `Next` and read by `Take`, `Report`, `Release` from
    concurrent goroutines (passes, jobs, the beat loop) with no lock: a data
    race the race detector will report; a torn read is unlikely on an int
    but the first `Take` of a pass can run with epoch 1 before the first
    `Next` returns. Repair: carry the epoch in the request (`next` already
    prints it; `ReportRequest.Epoch` exists) and drop the field.

17. **minor** `edge/worker/verbs.go:220` with `runner.go:726`. `Report`
    fills a missing `op` in the sprint's JSON with the id it sent, so the
    "receipt matching the result ID" check (`receipt.Op == req.ResultID`,
    doc `:543`) passes on any `{"accepted":true}`; the cmd test's fake sprint
    relies on it (`cmd/nova-worker/break_test.go:61`). Repair: require `op`
    in the receipt and refuse a receipt without it.

18. **minor** `edge/worker/runner.go:445-449`. `MkdirAll` failures for
    `tmp` and `data` are swallowed (`err != nil && sub == "home"`); the wall
    sets `TMPDIR` and `XDG_DATA_HOME` to those paths (`harness.go:461`), so
    the harness fails inside the wall with an unrelated error. Repair: fail
    the take on any of the three.

19. **minor** `edge/worker/probe.go:201`. The probe runs the harness with
    `r.Timeouts.LaunchGrace` (15 s, the measured launch-death window) as its
    whole timeout, though it is "a one-turn job on the same path": a model
    call plus a file write. A slow provider turns the row held for the
    backoff ladder with "exit -1 ... timed out after 15s". Repair: a probe
    bound of its own (a policy number, say 2 min), with LaunchGrace kept for
    the death classification.

20. **minor** `edge/worker/pass.go:92-94` with `edge/worker/hook.go:126-136`.
    The bus hook's death ends the wake loop, which cancels every job
    (`:59-65`): each running harness is killed and its hour of work
    discarded for a Redis or nova-bus hiccup, then run again from scratch
    after the service restart. The doc says "The runner gates on no other
    daemon ... its harnesses run on" (`LAYER-4-EDGES.md:371-373`) and, two
    sections later, that the hook's death exits the worker (`:649-651`):
    the two sentences disagree and the code does the second. Repair: on
    `hook.Done()` restart the hook with the probe's backoff while the jobs
    run on; exit only when ctx ends.

21. **minor** `edge/worker/runner.go:565` and `:716-722`. The launch
    backoff (`r.Sleep`, up to 60 s) and the report retries sleep on the
    runner's clock without watching ctx, so a stop waits out the backoff
    and a wall death in the backoff window is still "stopped" late. Repair:
    `wait_internal(ctx, d)` (`pass.go:109-118`) in both.

22. **minor** (red test) `cmd/nova-worker/break_test.go:59`. The fake
    `nova-sprint`'s `next` lists the consumer under `"ready"`; since the
    deal is the take (`pass.go:208-222` runs only `next.Working`) the
    worker takes nothing, runs two passes and exits 0, and the test fails
    at the base commit. The fixture is from before defect 52. Repair: list
    the consumer under `"working"` (and gate the test as functional: it
    runs real git and shell).

23. **minor** (red test) `edge/bus/functional_test.go:92-123`. Starts a
    real `redis-server` and two real `nova-bus hook` processes in the
    default `go test` run; here `nova-bus who` never listed the hooks within
    5 s. Whether the installed `nova-bus` (`/Users/glenn/.local/bin`) has
    the output this test expects could not be decided cold. Repair: gate it
    on an environment flag like `TestFunctionalScratchForge`
    (`edge/forge/functional_test.go:21`) and print `who`'s output in the
    failure.

24. **nit** `edge/worker/proxy.go:88-91, 216-223`. `X-Nova-Route` and
    `X-Target-Host` let the harness inside the wall pick another route or
    another port of the allowed host; nothing in the adapters sets either.
    Repair: delete both; the first path segment is the one route selector.

25. **nit** `edge/worker/runner.go:31-36, 540-544, 1133-1135, 1139-1146`.
    Before the child starts, `runAndHarvest_internal` writes a made-up pid
    (`40000+n`) and timestamped pre-start nonce `n-<pid>-<UnixNano>`
    (`runner.go:540-544`); `runWallCmd_internal` then allocates a second
    counter value and counter-only nonce `n-<fallbackPID>`
    (`runner.go:1133-1135`) and persists that counter-only nonce (with the
    real or fallback pid) in `OnStart` after the child starts
    (`runner.go:1139-1146`). Because `nextWallPID_internal` restarts at
    40001 in every worker process, the persisted post-start nonce
    `n-<fallbackPID>` is not unique across worker restarts. Repair: write
    the pid once, from `OnStart`, and derive the persisted nonce from a
    random or timestamped source rather than the per-process counter alone.

26. **nit** `docs/LAYER-4-EDGES.md:23, 465-467, 545, 585-586` still cite
    `tla/Push.tla` and `PushCursorBeforeConfirm`, which the stitch deleted;
    `:197-201` and `:512` describe a landing host, ssh and `nova-sprint
    land-step` that no longer exist (`LandPass` runs in run's process,
    `run/edges.go:120-156`). Repair: cut the paragraphs.

## Promise gaps

Doc sentence, then what the code does.

- "A `working` consumer whose harness is alive is adopted by pid and nonce"
  (`LAYER-4-EDGES.md:361-362`); `Runner.tla:292` "adopt live harnesses by
  pid and nonce, never spawn a second". `edge/worker/runner.go:93-104`
  proves the nonce only from this process's memory or `/proc`; on darwin
  the harness is never adopted and `recover.go:168-171` re-runs it (defect 1).
- "A job the worker's own stop cuts short is 'runner stopped': not
  reported, kept to resume" (`:303-304`); guarantee 11. Only the stage and
  run steps check ctx (`pass.go:379-380, 386-387, 429-430`); a stop during
  rebase, tests or push writes `fail`/`fault` and the next start sends it
  (defect 2).
- "a rate limit rests the identity and fails no card" (`:30-31`); "A 403 or
  429 with a retry time rests that identity until then: a fault to its
  caller that spends nothing" (`:229-232`); "A landing refuses for this
  pass, spending nothing" (`:166-171`). `LandPass` turns the forge's
  `FaultError` into `pr_unopenable`/`ci_fault`/`merge_refused`
  (`land.go:194-202, 223-227, 236-241`) and run gives every such outcome to
  layer 3, which opens a judgement per card (`steps/land.go:74-91`): a
  rate limit costs a human answer (defect 5).
- "A job directory whose consumer is final, or whose generation is stale,
  is retired on the first pass that sees it" (`:366-368`). A directory of a
  previous epoch is kept for ever and blocks the same-named card
  (`recover.go:68-71`, `pass.go:216`; defect 6).
- "push the branch with the row's token" (`:275-276`), "Workers push and
  open PRs with their row's token" (`:218`). The worker's git carries no
  credential at all (`runner.go:1056-1063`; defect 10).
- "A harness that logs in (claude, codex on a subscription) keeps its
  credentials in its own store, granted read-only into the wall"
  (`:326-328`). `ConfigDir` is never set and would be a `--write`
  (`harness.go:108, 114`; defect 9).
- "`ClassifyJob` ... never by substring-scanning agent prose or test
  output" (`:536`). `runWallCmd_internal` scans the combined output for
  `nova-reserve:` and `isInfraFault_internal` for `SANDBOX REFUSED`
  (`runner.go:571, 1161`; defect 11).
- "The runner gates on no other daemon ... with the store unreachable its
  harnesses run on" (`:371-373`). The bus hook is a daemon it gates on: its
  death cancels every job (`pass.go:59-65, 92-94`; defect 20).
- "The model is nova-config's for (row, tier), chosen by code and recorded
  in the report" (`:272-274`). `ReportRequest` (`verbs.go:55-60`) carries no
  model; `nova-sprint report` is not given one (`verbs.go:200-211`). The
  store's `cards.model` from the deal stands in; the doc's "recorded in the
  report" is not what happens.
- "every refusal names its fix" (guarantee 13). `stageStep_internal`'s
  "staging fault: <git error>" (`runner.go:499`) and the mirror path
  (`runner.go:870-871, 879-880`) name no fix, and the half-mirror case
  (defect 4) names nothing a reader can act on.
- "Unit (no clock, sockets, network or outside program ...)" (`:471-472`).
  See the test run above: `edge/bus` and two `edge/worker` tests run real
  processes and sockets in the default run; `edge/break_test.go:45` sleeps.
- "the identity's own credentials" for the forge (`:29`) and "The human's
  credentials are never used" (`:215-216`) against `forge.HostEnv()`
  (`forge/forge.go:117-122`): gh runs as the host's own login, which on the
  coordinator's machine is a person's. The stitch note (`:951-964`)
  records this as the chosen rule (intent rule 32); the earlier paragraph
  still says the opposite. A doc defect, not a code one.

## Model vs code

`tla/Runner.tla` (checked with `Runner.cfg`: width 2, 1 slot, 1 crash):

- `Withdraw(c)` (`Runner.tla:205-212`) is enabled only with
  `harnessAlive[c] = 0` and `jobState[c] \in RetireStates` (no `running`,
  `reporting`). The code withdraws a running job
  (`runner.go:249-262`, `pass.go:425-428`) and, worse, has a window the
  model forbids by construction: `holds_internal` (`retire.go:61-65`) knows
  only what the last `next` read said, so a job between its `next` and its
  push can push after the sprint withdrew it (by lease, so it rewinds
  nothing the new holder wrote, but `NoPushAfterFinal`, `:409-410`, is
  `finalPushes = 0`). The model's precondition hides the race; the lease is
  the real guard and the model should state it (a push after final that is
  refused by lease, never one that lands).
- `Stop` (`:243-252`) kills only `running` jobs and leaves the rest where
  they are; `Rebase`, `Conflict`, `Push` are atomic. The code's rebase,
  tests and push take minutes and a stop inside them writes an outcome
  (defect 2). `StopIsNeverAFault` holds in the model and not in the code.
- `Restart` (`:289-293`) keeps `harnessAlive` ("adopt ... never spawn a
  second"); on darwin the code spawns a second (defect 1). The reversed
  witness `RunnerRestartSpawnsSecondHarness` is the live behaviour.
- `Report(c)` (`:166-177`) is enabled whatever `storeState[c]` is and moves
  a `fail` consumer's job to `recorded` (`:175`, `IF @ = "working" THEN
  "done" ELSE @`). The store refuses a report on a final consumer; the code
  counts the refusal and quarantines at 3 (`recover.go:212-231`) or retires
  (`recover.go:63-66`). The model has no refused report, no quarantine, no
  stale generation, no epoch (the doc admits the first, `:666-667`); the
  three are where the live defects 69 and 78 were.
- `Die(c)` (`:119-127`) needs `~env.crashed`; a harness that dies while the
  worker is down is unmodelled, and the code's answer (a `running` record
  with no live harness and no `result.json` is run again,
  `recover.go:168-171`) is neither `Die` (a fault) nor `Stop`.
- `WorkingWithinWidth` (`:388-389`) is vacuous: `Init` has exactly `Width`
  consumers working and no action adds one. It checks nothing.
- `PidRecordedAtStart` (`:367-368`) says the pid is `PidFor(c)` while
  running; the code writes a made-up pid and pre-start nonce
  `n-<pid>-<UnixNano>` first (`runner.go:540-544`), then overwrites the
  record in `OnStart` (`:1139-1148`) with the real pid and counter-only
  nonce `n-<fallbackPID>` created at `:1133-1135` (and re-persists after the
  run at `:554-557`). The projection in the walk never shows this because the walk
  writes the pid itself (`trace_test.go:322`).
- The trace walk (`edge/worker/trace_test.go`) re-enacts the model in the
  test for every lifecycle step but three (defect 8), so "the code's own
  random walk emits traces" is not true of this layer's runner; the
  `LandEdge` walk (`edge/git/trace_test.go:16-21`) does drive `LandPass`
  and is a real binding.

`tla/LandEdge.tla`:

- `Pass(s)` (`LandEdge.tla:111-132`) is atomic up to its outcome, so
  `BaseMoves` cannot fall between the gate's base read and the merge. The
  code reads base (`land.go:215-221`) and then merges (`:223`) in two steps;
  `gh pr merge --match-head-commit` fences the head, not the base. The doc
  says the base is fenced by branch protection "requires branches up to
  date" and that `stream add` refuses a base without it
  (`LAYER-4-EDGES.md:147-149`); `verbs/admit_stream.go:15-70` checks
  `CanAct`, the base tip and parent branches, not that setting. Either the
  model should have the window or admission should check the setting.
- Guarantee 4 says base advances "only to a sha whose checks passed at that
  exact sha"; `forge.Merge` (`forge.go:434-463`) returns GitHub's merge
  commit (method `merge` first, `:481-483`), a sha CI never saw. The model's
  tree-as-a-set abstraction makes this invisible (`NothingLandsUnpassed`,
  `LandEdge.tla:185-189`, compares card sets). True by content, not by sha;
  the guarantee's wording should say "tree".
- The model's `absent` stands for "a fault, a PR not opened, a branch not
  created" (`LandEdge.tla:12-13`) and every non-green gate is one judgement
  (`Tick`, `:79-94`). That is what the code does, and it is what defect 5
  objects to: the model encodes the behaviour the layer-4 doc promises
  against. One of the two has to move.

## Simplifications

- `Runner.QueryEdge`, `evalEdgeQuery_internal`, the `dedup` field and all
  of `edge.Deduplicator` (`runner.go:160-163, 762-791`,
  `edge/request.go:38-46, 73-101`) have no caller outside tests. Delete
  them (and seam ruling 3's in-flight dedup text).
- `next.Ready` and `card.Ready` in the worker (`verbs.go:36`,
  `runner.go:778`): no consumer is ever ready since the deal is the take.
  Drop the list from the worker's view.
- `r.adopted` (`runner.go:160, 186, 259`; `recover.go:136, 142`) is
  write-only. Delete it; adoption is the step log line.
- `CLISprintClient.OpenJudgement` (`verbs.go:231-234`, `probe.go:60-67`)
  runs a verb that does not exist on every failed probe and appends its
  failure to every refusal. Delete until `judge-open` exists.
- `FormatResultID` ignores `attempt` and `FormatTakeOpID` ignores `row`
  (`worker.go:31-36`); the parameters mislead. Drop them.
- Two `next --json` per pass (`recover.go:52`, `pass.go:198`): one process
  and one store read would do, and the two reads can disagree.
- `jobMatchesSummary_internal` (`runner.go:222-225`) and
  `sameEpoch_internal` (`retire.go:162-164`) treat a zero epoch or attempt
  as a wildcard for records from before those fields; every record on the
  fleet now has them. Make the fields required.
- `effectiveConfig_internal`'s `gitdir:` branch (`runner.go:1101-1107`) is
  dead: staging makes a `--shared` clone with a `.git` directory, never a
  `.git` file.
- `launchBackoff_internal` (`runner.go:515-531`) hand-rolls a hash mixer
  for jitter; `math/rand/v2` on the runner's seed is one line.
- `OSGitOps` has both `Timeout` and `Timeouts` (`runner.go:802-803, 835`):
  one.
- `Timeouts.BusClaim` (`worker.go:48, 57`) and `WallConfig.WritePaths`
  (`harness.go:336`) are set nowhere but defaults.
- Size: the doc estimated nova-worker at 1,900 non-test lines
  (`LAYER-4-EDGES.md:493-494`); `edge/worker` is 4,199 (runner.go alone
  1,170). The three pid/nonce paths, the two proxy override headers, the
  dead edge query and the duplicated `next` read are where to start.

## Missing tests

- A restart on a host with no `/proc` (`OSProcessTable.Alive` has no unit
  test with a fake process table of its own; `fakeProcTable` bypasses it).
  Expected: a live but unprovable harness is raised, not run twice.
- A stop (ctx cancelled) during rebase, during a DONE-WHEN test and during
  the post-rebase push; `TestLiveAStoppedJobIsKeptNotReported` covers only
  the harness run.
- A withdraw that lands between a job's `next` read and its harvest push:
  the push must be refused by lease or by `holds_internal`, and the test
  must show which.
- Two concurrent jobs and a proxy status from one of them: each job's
  classification reads only its own status.
- A first-use mirror clone that times out, then the next pass.
- A job directory of epoch 1 with the same consumer id dealt in epoch 2.
- `Kill` of an adopted harness reaching the grandchild (functional, with a
  real `nova-reserve`).
- `LandPass` on a `forge.FaultError` from `PR`, `Checks` and `Merge`: the
  expected outcome, whatever the ruling is.
- `forge.Merge` with `pr merge` ok and `pr view` failing.
- A rollup with one cancelled check and the rest queued.
- A corrupt or missing `job.json` beside a dealt card.
- The runner trace walk driving the runner's own steps (defect 8).
- Fix `cmd/nova-worker/break_test.go:59` (`ready` -> `working`) and gate it
  and `edge/bus/functional_test.go` and the two `TestFunctionalRunner*`
  tests out of the unit run (an env flag, as the forge test does), so the
  unit tier matches `LAYER-4-EDGES.md:471-472`.

## Questions for the coordinator

- Does `nova-reserve run` forward SIGTERM/SIGKILL to its child and hold the
  slot until the child exits? Defect 7 is serious only if it does not.
- Has any bench run the `claude` or `codex` harness through this worker
  (defect 9)? The stitch notes record only opencode runs.
- How is the worker's `git push` authenticated on the fleet today: the
  bench user's `gh auth setup-git`, a keychain, or something nova-fleet's
  play writes? Defect 10 is a promise gap if the fleet config is the design.
- Does a working consumer's generation ever move between its take and its
  report (what moves it)? If nothing does, the stale-generation retire
  (`recover.go:68-69`) is dead and `holds_internal`'s gen comparison is
  only the final/not-final bit.
- Are the stream repositories' base branches protected with "require
  branches to be up to date before merging"? That setting is the only
  thing fencing the base in `gateAndMerge_internal` (`land.go:215-227`).
- Is the ruling that every non-landed outcome is a judgement (LANDING,
  `steps/land.go:20-21`) meant to include transient forge faults and rate
  limits, superseding `LAYER-4-EDGES.md:30-31, 166-171, 229-232`? If so the
  layer-4 doc is wrong; if not, defect 5 stands.
- Is the `nova-bus` on this Mac (`~/.local/bin/nova-bus`) the version
  `edge/bus/functional_test.go` targets? Its `who` never listed the test's
  hooks here; I did not run it against the live Redis.
