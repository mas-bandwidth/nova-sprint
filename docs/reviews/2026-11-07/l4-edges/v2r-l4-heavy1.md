# Layer 4 (edges): cold review, v2r-l4-heavy1

Reviewer: rowan (opus), 2026-11-07, at 43d849e2 (branch v2-stitch). Scope: edge/**,
cmd/nova-worker/**, docs/LAYER-4-EDGES.md, docs/SEAMS.md section 6, tla/Runner.tla,
tla/LandEdge.tla. docs/ENFORCED.md covers layers 1, 2 and 5 only: layer 4 has no
enforcement ledger, so every promise below was checked against the layer doc itself.

Commands run (the checkout; nothing touched the live store or any service):

```
$ go build ./... && go vet ./... && echo BUILD-VET-OK
BUILD-VET-OK
$ for p in $(go list ./edge/... ./cmd/nova-worker/...); do go test -count=1 $p; done
ok   .../edge          0.842s
FAIL .../edge/bus      5.156s   TestFunctionalNovaBusWhoAndSend: expected sprint and amd-1.ds in nova-bus who
ok   .../edge/forge    0.108s
ok   .../edge/git      3.194s
ok   .../edge/worker   4.014s
FAIL .../cmd/nova-worker 5.652s TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot: no report reached nova-sprint
$ go test -count=1 -race ./edge/worker/
ok   .../edge/worker   5.781s
```

Both reds are stale functional tests, not the code (see Missing tests 1 and 2). The
layer's `go test` runs functional tests too: no build tag separates them, so
`go test ./edge/...` starts redis-server, real nova-bus hooks, real git and real
`sh` processes on whatever machine runs it (Missing tests 3).

Two throwaway probe tests were run and deleted (only the report file is committed):

```
$ go test -run TestZZProbe ./edge/          # edge.OSExec with Dir "/nonexistent-dir-zz"
missing dir: exec git (missing_binary): binary "git" not found: install git on PATH or run: nova-fleet check
redacted: push <redacted> to refs/heads/x rejected     # RedactN on "push 0123...4567 to refs/heads/x rejected"
$ go test -run TestZZProbe ./edge/worker/   # CLISprintClient on a FakeExec
report: nova-sprint report --json: parse failed at byte 31: invalid character 'w' after top-level value;
  first bytes "{\"accepted\":true,\"op\":\"res-1\"}warning: roster read slow" refused=false down=false
release exit 2: down=true
```

## Verdict

Not sound yet. The pieces that were rebuilt after live defects (the lease on every
worker push, retire, deal-is-take, the read outcome) are careful and well tested,
but four parts of the layer hold their promise only under conditions the code never
checks. The worst: on a macOS bench a restarted worker cannot see its own live
harness. `OSProcessTable.Alive` only knows a process through the in-process nonce
map or `/proc/<pid>/environ`, so after a restart on darwin it always answers false,
and recovery runs the card a second time in the same job directory while the first
harness is still running (guarantee 7). Close behind: two streams of one repository
land concurrently in one shared working tree with no lock; a GitHub rate limit
becomes a coordinator judgement instead of a no-cost fault; a done-when test that
gets no slot is reported `fail` and spends an attempt; and one key-proxy status
shared by every concurrent job misattributes provider faults between cards. The
model `Runner.tla` hides the first defect because its `Restart` adopts every live
harness by construction.

## Defects

Ranked, most severe first.

1. **blocker**: edge/worker/runner.go:93-104 (`OSProcessTable.Alive`), recover.go:122, recover.go:168-172.
   `Alive` returns true only when the pid is in `liveProcNonces_internal` (an
   in-memory map, empty after a restart) or `/proc/<pid>/environ` holds the nonce.
   macOS has no `/proc`, so after any worker restart on a Mac every live harness
   reads as dead. `recoverWorkingJob_internal` then returns `resume`, and
   `prepareResume_internal` sets a `running` job without result.json back to
   `staged` (line 171), which runs the harness again in the same job directory
   while the first one (its own process group via `Setpgid`, so it outlives the
   worker) is still writing there. This breaks guarantee 7 ("One live harness per
   job directory") and the restart paragraph ("adopted by pid and nonce"); benches
   include Macs (beat.go reads `sysctl -n vm.loadavg` for macOS). Repair: read
   another process's environment portably (`ps -E -p <pid>` or `sysctl kern.procargs2` on
   darwin) or write the nonce to a pidfile the harness's wrapper holds locked, and
   test `Alive` against a real child after clearing the map.

2. **blocker**: run/edges.go:84-88 and 271-279 with edge/git/git.go:279-307, 309-341.
   `Dispatch` runs every land request with `go e.land(...)`, and `clone` keys the
   working clone by URL only, so two streams on one repository get the same
   `RepoDir`. `MergeGroup` mutates that one working tree (`checkout --detach`,
   `merge`, `rev-parse HEAD`) with no lock. Two passes interleave: stream A can push
   stream B's merge as its group sha, gate it, merge it, and report `LandedHeads =
   A's heads` for content that holds B's. `readHeads` (every 10 s) also calls
   `Git.Clone` into the same directory, so the first clone can race too. The doc
   promises "one attempt per stream at a time, streams in parallel" (LAYER-4, git
   edge) and "puts two streams in one branch" is a never. Repair: one mutex per
   clone directory around a whole `LandPass`, or one clone per stream.

3. **serious**: edge/git/land.go:195-201, 236-240, 223-226 with edge/forge/forge.go:179-197, 215-219.
   A rate limit (the token bucket's own refusal, or a 403/429 from gh) is a
   `*forge.FaultError`, but `gateAndMerge_internal` turns any error from `PR` or
   `OpenPR` into `pr_unopenable`, from `Checks` into `ci_fault`, and from `Merge`
   into `merge_refused`. SEAMS section 6 says each of those opens one land
   judgement per card. The doc promises "a rate limit rests the identity and fails
   no card" and "a fault to its caller that spends nothing". Repair: return a
   `FaultError` from `LandPass` as an error (run's `fault`), or add a `rate_limited`
   outcome that run asks again after `RetryAfter`.

4. **serious**: edge/worker/runner.go:676-686 with runner.go:1161-1163.
   `rebaseAndTestStep_internal` treats any error from `Wall.RunTest` as red tests:
   `ErrNoSlot` (no nova-reserve slot within `ReserveWait`, 5 s), a sandbox refusal,
   a missing binary or a timeout all become `fail: tests red (...)`, which spends an
   attempt and bounces the card. Runner step 4 says "no slot within the wait is no
   start yet, never a failure", and guarantee 11 says a bench fault spends nothing.
   On a busy bench a finished card fails because the test could not get a slot.
   Repair: on `ErrNoSlot` keep the record at `harvested` for the next pass, and
   report a wall or exec error from `RunTest` as `fault`.

5. **serious**: edge/worker/proxy.go:31-44, 260-267 with runner.go:559, 585.
   The key proxy keeps one `lastStatus` for the whole worker, and each job calls
   `r.Proxy.ConsumeStatus()` (a swap to 0) after its harness exits. Jobs run
   concurrently (pass.go:283-286), so job B can consume the 429 that job A's
   provider returned. B is then classified a rate-limit fault (spending nothing)
   and A, with no result file, becomes "fail: no verdict" (spending an attempt).
   That breaks guarantee 11 for A and misattributes cost for both. Repair: key the
   recorded status by route and caller (a per-job path segment or header the
   harness's base URL carries), and consume only the job's own.

6. **serious**: edge/worker/verbs.go:94-107, 140, 154, 217.
   `run_internal` returns `stdout + stderr` and `Next`, `Take` and `Report`
   `json.Unmarshal` that concatenation. Any stderr line on success (a warning, a
   banner) makes the receipt unparseable (probe above). For `Report` the error is
   neither `ErrRefused` nor `ErrStoreDown`, so `resendErr_internal`
   (recover.go:212-231) counts an *accepted* report as a refusal and quarantines
   the record after three. For `Next` every pass fails. Repair: parse
   `res.Stdout` only and keep stderr for the refusal text.

7. **serious**: edge/worker/verbs.go:113-125 with pass.go:150-155 and recover.go:213-214.
   Every exit code other than 0 and 1 (2, a usage error from an older or newer
   nova-sprint; -1, a missing binary or timeout) is wrapped as `ErrStoreDown`
   (probe above: `release` exit 2 gives `down=true`). Store-down is swallowed
   silently by `ignoreStoreDown_internal` and by `resendErr_internal`, so a worker
   whose nova-sprint rejects a flag retries forever with no log line. That is
   "silent failure" under the brief's rule (b), and against guarantee 13 ("every
   refusal names its fix"). Repair: only a connection failure the verb reports
   (its own exit code for "store unreachable") is `ErrStoreDown`; exit 2 and exec
   errors are refusals naming the command.

8. **serious**: edge/git/land.go:215-223, verbs/admit_stream.go:36-46.
   The base is read (`LsRemote`), compared with the gated tip, then merged with
   `gh pr merge --match-head-commit`, which fences the PR's head, not the base. If
   base moves between the two calls, GitHub merges onto the new base unless the
   branch requires PRs to be up to date. The doc says "`stream add` refuses a
   protected base without it, naming it", but `admitStream` reads only
   `CanAct`, the tip and parent branches. No strict-protection check exists
   anywhere (grep for strict, up-to-date or protection finds none). Guarantee 4,
   "Base advances only to a sha whose checks passed ... from the base tip it was
   gated on", then depends on a repository setting nobody verifies. Repair: read
   `required_status_checks.strict` (or the ruleset equivalent) in admission and
   refuse without it, or merge with `--match-head-commit` plus a base check
   GitHub enforces (merge queue).

9. **serious**: edge/git/land.go:223-229 with forge.go:481-487.
   After `Merge` the pass returns `landed` with `LandedHeads = req.Heads` and never
   checks that each head is an ancestor of the new base. LAYER-4 git edge step 7
   says each landed head is checked an ancestor of the pushed base tip (LPM M21).
   `method_internal` falls back to squash or rebase when merge commits are
   disallowed, and then no landed head is an ancestor. Layer 3 records the cards
   landed, and the next `allAncestors_internal` (land.go:85) never sees them on base.
   Repair: check `IsAncestor(head, mergeSHA)` for each head after the merge, and
   refuse a repository whose only methods are squash or rebase at stream add.

10. **serious**: edge/worker/recover.go:62-75 with pass.go:215-218 and runner.go:226-236.
    `planPass_internal` skips every working card whose `jobs/<id>` directory exists
    ("One with a job on disk is recovery's"). Recovery acts only on directories
    whose job.json parses (`loadRowJobs_internal` drops the others), and it keeps a
    record of another epoch for ever ("identity-mismatch-kept", line 70-71). A
    cleared sprint's next epoch reuses consumer ids (doc, defect 78), so a new
    card with an old directory is never run, released or reported: the card sits
    working and holds width until policy withdraws it, and is then dealt back to
    the same blocked row. The doc promises "one whose job cannot be rebuilt, is
    released with no blame". Repair: retire a record of another epoch (its
    consumer is gone with the epoch), and treat a directory with no readable
    job.json as one that cannot be rebuilt.

11. **serious**: edge/worker/runner.go:1161-1163.
    `ErrNoSlot` is detected by `res.ExitCode == 1 && strings.Contains(combined,
    "nova-reserve:")` on the combined output, which includes the harness's or the
    test's own output. An agent working on nova-tools (or any card that prints
    nova-reserve's words) that exits 1 is read as "no slot": the record goes back
    to staged and the harness runs again on every pass, never reported. The Built
    notes promise outcomes are classified "never by substring-scanning agent prose
    or test output". Repair: run nova-reserve with a distinct exit code for "no
    slot" (or `--wait` failure on its own stream) and match only that.

12. **serious**: edge/exec.go:112-119, 137-138.
    The process group is killed only by `Cancel` (timeout or caller cancel). On a
    normal exit a child that left a background process (a test server, a daemon)
    holding the output pipe gets `exec.ErrWaitDelay` after 200 ms, which line 137
    treats as success, and the group keeps running. For the wall that is a process
    still running after the slot's owner exited (guarantee 8: "freed when it ends";
    runner step 4: "the slot lives exactly as long as the harness"), unless
    nova-reserve or nova-sandbox kill their own group, which the edge does not
    check. Repair: `syscall.Kill(-pid, SIGKILL)` after every `Wait`, and test it with
    `sh -c "sleep 20 & exit 0"`.

13. **serious**: run/edges.go:137-147 and 162-174 (the case at 167).
    `askAgain_internal` asks a pass again for ever on `ci_pending` and
    `base_moved`. A check that stays queued (no runner picks it up) is pending,
    never absent, so the landing goroutine polls for ever and the stream's cards
    never reach a judgement. LAYER-4 lists "CI wait, the repository's job timeout
    plus 10 min" and says "a gate that did not run (no CI runner, a cancelled job,
    a timeout) is a fault". Repair: bound pending by `Wait` plus the job timeout
    as absent is bounded, then answer `ci_fault`.

14. **minor**: edge/exec.go:152-155.
    A missing working directory is classified `missing_binary` with the fix
    "install git on PATH" (probe above): `chdir` fails with ENOENT, which
    `errors.Is(os.ErrNotExist)` matches. Guarantee 13 ("every refusal names its
    fix") is broken for a deleted job directory. Repair: check `*os.PathError`
    with `Op == "chdir"` first and name the directory.

15. **minor**: edge/failure.go:40, 77-81.
    `keyLike` matches any 20+ character run that holds a digit, so every 40-hex
    sha in a failure line becomes `<redacted>` (probe above). Push refusals,
    `head_moved` reasons and conflict lines lose the very shas a coordinator needs.
    Repair: exempt `^[0-9a-f]{40}$` tokens (a sha is not a secret).

16. **minor**: edge/git/git.go:375-389 with run/edges.go:131.
    Ledger merging is unreachable: `LandRequest.Ledgers` is never set by run (the
    only caller), so policy's ledger list does nothing and a ledger conflict is a
    plain `conflict`. The union merge it would run is also wrong for ledgers: it
    drops every blank line and every repeated line (`seen[line]`), so a
    CHANGELOG's section headers and spacing are destroyed. LAYER-4 promises
    ledgers "merged entry by entry or regenerated". Repair: delete the path (see
    Simplifications) or wire it with a real entry-wise merge.

17. **minor**: edge/git/git.go:150-151.
    `Fetch` wraps every non-zero exit (auth failure, network, a bad refspec) as
    `ErrMissingRef`, "ref not on origin". Repair: only a "couldn't find remote ref"
    stderr is `ErrMissingRef`; the rest stay command failures.

18. **minor**: edge/git/git.go:168-171.
    `LsRemote` returns "absent" (`"", false, nil`) when exit 0 output holds no sha,
    while `Tip` (line 194-197) refuses the same output. In `LandPass` that silent
    default becomes `missing_head` naming a branch that exists. Repair: return an
    error as `Tip` does.

19. **minor**: edge/worker/runner.go:105-118.
    `OSProcessTable.Kill` signals only `pid` (nova-reserve's), not its process
    group, so an adopted harness past its deadline (recover.go:133-134) or a
    withdrawn one may leave nova-sandbox and the harness running. Repair: signal
    `-pid` (edge.Exec started it with `Setpgid`).

20. **minor**: edge/worker/runner.go:31-36, 1133-1135.
    The nonce is `n-<40000+counter>`, and the counter restarts with the process,
    so nonces repeat across worker lifetimes. The nonce exists to make a reused
    pid not look like the job's harness; a repeated nonce weakens exactly that.
    Repair: a random 128-bit nonce.

21. **minor**: edge/worker/verbs.go:143, 147, 201, 224.
    `CLISprintClient.Epoch` is written by `Next` (each pass) and read by `Take`,
    `Report` and `Release` from detached job goroutines with no lock. The race
    detector run above did not hit it because the unit tests use other clients.
    `Take` also takes its epoch from the last `Next`, not the one the consumer was
    read under. Repair: pass the epoch from `NextResponse` into each call.

22. **minor**: edge/worker/verbs.go:220 with runner.go:726.
    `Report` fills a receipt with no `op` with the worker's own id, so the check
    `receipt.Op != req.ResultID` can never fail on a receipt that omits it.
    Repair: leave `Op` as the store printed it.

23. **minor**: edge/worker/runner.go:756 with pass.go:406-409 and recover.go:212-231.
    The normal clean after an accepted report uses plain `os.RemoveAll`, which
    fails on a read-only Go module cache inside the job's HOME (the cause of
    defect 78). The error goes back through `send_internal` to
    `resendErr_internal`, which logs "refused" and counts it as a report refusal.
    It heals only because the next pass retires the final consumer. Repair: use
    `removeTree_internal` here as retire does, and do not count a clean failure as
    a refusal.

24. **minor**: edge/worker/outcome.go:38-41.
    A read harness's own `fail` whose reason mentions "result.json" (agent prose,
    for instance a finding about a file of that name) is turned into a fault and
    loses its finding. That is substring scanning of agent prose again. Repair:
    rely on `NoVerdict` alone, and heal old records by a version field.

25. **minor**: edge/worker/worker.go:74, harness.go:356-358.
    `Job.SlotWeight` is never set anywhere, so `--slots <weight>` (runner step 4)
    is never passed and every harness takes nova-reserve's default. Repair: carry
    a weight per tier from the receipt or policy, or drop the field and the doc's
    `<weight>`.

26. **minor**: edge/worker/runner.go:902 and 875-878.
    The work clone fetches from the repository's URL (the network), while runner
    step 3 says it "fetches from the mirror, never the network". The work clone is
    also made `--reference` the mirror without `--dissociate`, and each worktree
    appends the work clone's alternates (932-940): a gc in the mirror after
    branches are pruned can remove objects the work clones still need. Repair:
    either say the network fetch in the doc, or fetch from the mirror; and
    `--dissociate` the work clone.

27. **minor**: edge/forge/actions.go:216-220.
    Inheritance is decided per workflow: if the base's run of a workflow failed,
    a new failure in a different job of that workflow at the head is filed as
    inherited. That makes `ci_red_inherited` where it should be new red. It is
    still a judgement, so nothing red lands, but the coordinator is told the wrong
    cause. Repair: compare failed job names, not workflow conclusions.

28. **minor**: edge/worker/harness.go:62, 107-115 and serve.go:80.
    `NewHarness` returns `ClaudeHarness{}` and `CodexHarness{}` with an empty
    `ConfigDir`, and nothing sets it, so the wall grants no login directory and
    HOME is the job's. LAYER-4 says the login store is "granted read-only into the
    wall", and the Built note says "their config directory a `--write`". The
    shipped wiring does neither, so either claude cannot authenticate in the wall
    or it reaches a login some other way the doc does not name. Repair: set
    `ConfigDir` from a flag, grant it read-only as the guarantee paragraph says,
    and make the doc say one thing.

29. **minor**: edge/worker/runner.go:1039-1048.
    `IsDirty` counts untracked files, and `commitLeftTree_internal` commits with
    `git add -A`. A harness that says ok, commits nothing and leaves build output
    gets that output committed to the card branch. The same untracked files also
    keep the directory at "DIRTY WORKTREE KEPT" (runner.go:748-750). Repair: commit
    only tracked changes plus files the harness staged, or respect `.gitignore`
    with `git status --porcelain --untracked-files=no` for the dirty test.

30. **minor**: edge/request.go:184-196.
    `FakeExec` maps a cancelled context to success (only `DeadlineExceeded` is
    handled), while `OSExec` returns `ErrCanceled`. A unit test of "runner
    stopped" on the fake therefore tests a different contract from the real exec.
    Repair: return `ErrCanceled` for `context.Canceled` as `classifyExecErr_internal`
    does.

31. **nit**: edge/exec.go:108. A zero `Cmd.Timeout` quietly becomes 30 s; the Built
    note calls the timeouts "mandatory". Refuse a zero timeout, or call it a default.

32. **nit**: edge/worker/worker.go:31-33. `FormatResultID` ignores `attempt` and
    hashes, while LAYER-4 Built says `res-e<epoch>-<consumer>-a<attempt>-g<gen>`. Fix
    the doc.

33. **nit**: edge/failure.go:83-84 and edge/forge/actions.go:258. Byte slicing can cut
    a UTF-8 rune in half. Cut at a rune boundary.

34. **nit**: edge/git/land.go:313. `append(slices.Clone(split.Inherited), split.New...)`
    is rebuilt for each failing check; build it once.

## Promise gaps

- "One live harness per job directory" (guarantee 7) and "A `working` consumer whose
  harness is alive is adopted by pid and nonce": on darwin the code cannot see a live
  harness after a restart and runs a second one (defect 1).
- "one attempt per stream at a time, streams in parallel": nothing stops two streams of
  one repository sharing one working tree at once (defect 2).
- "a rate limit rests the identity and fails no card" / "A 403 or 429 with a retry time
  rests that identity until then: a fault to its caller that spends nothing": a rate
  limit inside `LandPass` becomes `pr_unopenable`, `ci_fault` or `merge_refused`, each
  a judgement (defect 3).
- "no slot within the wait is no start yet, never a failure" (runner step 4) and
  guarantee 11: true for the harness, false for the done-when test (defect 4).
- "Base advances only to a sha whose checks passed at that exact sha, from the base tip
  it was gated on" (guarantee 4) and "`stream add` refuses a protected base without it,
  naming it": no such refusal exists (defect 8). Even with it, base advances to a merge
  or squash commit, never to the gated sha itself; the guarantee should say "a tree".
- "**Record**: each landed head is checked an ancestor of the pushed base tip (LPM M21)":
  not done (defect 9).
- "A `working` consumer with no job directory, or one whose job cannot be rebuilt, is
  released with no blame": a record of another epoch, or a directory without a readable
  job.json, is neither released nor run (defect 10).
- "never by substring-scanning agent prose or test output" (Built, ClassifyJob): the
  no-slot test (defect 11) and the read healing rule (defect 24) both scan it.
- "the slot lives exactly as long as the harness" / guarantee 8: edge.Exec leaves the
  group running after a normal exit (defect 12).
- "CI wait, the repository's job timeout plus 10 min": pending is unbounded (defect 13).
- "every refusal names its fix" (guarantee 13): a missing directory names the wrong fix
  (defect 14); a non-0/1 exit of a sprint verb is retried silently (defect 7).
- "Ledger files (policy's list per repository) are merged entry by entry or regenerated
  by the repository's ledger script": never wired (defect 16).
- "fetches from the mirror, never the network" (runner step 3): the work clone fetches
  from the repository URL (defect 26).
- "nova-reserve run --slots <weight>": never passed (defect 25).
- "keeps its credentials in its own store, granted read-only into the wall": no grant
  at all in the shipped wiring, and the Built note says `--write` (defect 28).
- Guarantee 9, "committed work is pushed before any non-ok end", against the WITHDRAWN
  row ("the runner pushes nothing"): a withdrawn job's commits are never pushed, by
  design since defect 78. The guarantee needs "while the job holds its card".
- "Functional, ... Every test isolated and t.Parallel(), run on a bench, never on the
  working machine": no build tag keeps functional tests out of `go test ./edge/...`
  (Missing tests 3).

## Model vs code

1. `tla/Runner.tla` `Restart` (lines 289-293) keeps `harnessAlive` and `Run` requires
   `harnessAlive[c] = 0`, so a second harness after a restart is impossible by
   construction. The code's adoption depends on `OSProcessTable.Alive`, which is
   false on darwin after a restart (defect 1). The model needs a restart that can
   lose sight of a live harness (an `AliveUnknown` branch) and an invariant that
   says resumption waits until the pid is proven gone.
2. `Runner.tla` `Rebase`/`Conflict` (lines 139-154) model the rebase and its conflict,
   but not the done-when test run in a slot. The code can turn a missing slot into a
   bounce (defect 4), and `HarnessInsideSlot` and `FaultSpendsNoAttemptOrBounce` do
   not cover the test step.
3. `Runner.tla` has no refused or unparseable report (the doc says so for the
   refused one). The code's keep-and-resend and quarantine at `RefusalLimit`
   (recover.go:212-255) and the parse-error path (defect 6) have unit tests only.
4. `Runner.tla` `RetireStates` (line 41) is {none, taken, staged, harvested, rebased},
   but the code retires a final consumer's record in any state, `reporting` and
   `running` included (recover.go:63-66, `finalJob_internal`).
5. `Runner.tla` has one slot pool and no shared proxy: the cross-job status
   (defect 5) is outside the model.
6. `tla/LandEdge.tla` takes a `Pass` as atomic and gives each stream its own state,
   so two passes sharing a working tree (defect 2) and a base that moves between the
   check and the merge without strict protection (defect 8) are both out of the
   model. Its "GitHub refuses" branch assumes the protection the code never verifies.
7. `LandEdge.tla` lands the gated tree; the code's squash or rebase fallback lands a
   different commit whose heads are not ancestors (defect 9). Ancestry after a
   landing (LPM M21) is in neither the model nor the code.
8. LAYER-4 Built says `Runner.tla`'s `CleanOnlyAfterRecordedAndPushed` now asks for
   the push only of a job that ended done, which is the model following the code's
   change to guarantee 9; the doc's guarantee 9 was not changed with it (Promise
   gaps, last but one).

## Simplifications

- edge/request.go:18-101 (`RequestID`, `EdgeRequest`, `EdgeResult`, `Deduplicator`),
  edge/worker/worker.go:19-26, runner.go:162, 186 and 762-791 (`QueryEdge`,
  `evalEdgeQuery_internal`, `dedup`): no production caller (`grep -rn QueryEdge` and
  `EdgeRequest{` outside tests find only steps' own `steps.EdgeRequest`). About 120
  lines of seam that nothing crosses.
- edge/worker/verbs.go:71, 231-234 and probe.go:60: `OpenJudgement` runs a verb
  (`judge-open`) that does not exist; every call fails (LAYER-4 Built, defect 30
  admits it). Delete it until the verb exists.
- edge/git/git.go:289-374 (ledger merge, `LedgerRule`, `MergeLedgerFailed`) and
  land.go:40: unreachable from run (defect 16).
- edge/git/git.go:156-157 and 200-242 (`LsRemote` given a sha, `lsRemoteSHA_internal`,
  which fetches every remote tip one by one): every caller passes `refs/heads/...`.
- edge/git/git.go:256-262, 418-437 (`TreeHash` as API, `Rebase`, `RebaseResult`,
  `MergeResult.Tree`, `ReusedHead`): no production caller of `Client.Rebase`; the
  worker rebases through `OSGitOps`.
- Two `CloneURL` functions: edge/git/url.go and edge/worker/runner.go:812-823 (the
  doc calls the second "a local helper"). One is enough.
- edge/forge/forge.go:274-301 batches up to 100 shas and paginates contexts, but the
  only caller asks for one sha per pass (land.go:237).
- edge/worker/pass.go:244-251 `RunConsumer` is used only by tests.
- edge/worker/runner.go:515-531: a hand-rolled splitmix hash for jitter, where
  `math/rand/v2` seeded from the runner's clock is one line.

## Missing tests

1. cmd/nova-worker/break_test.go:62 is stale: its fake `next` lists the card under
   `ready`, but since deal-is-take (defect 52) the worker runs only `working` cards,
   so it takes nothing and fails "no report reached nova-sprint". The breaker for
   guarantee 8 therefore checks nothing. Move the card to `working`.
2. edge/bus/functional_test.go:85-101 starts `nova-bus hook` with no stdout reader;
   the installed nova-bus now exits with "nova-bus hook: writing stdout: stdout
   closed: its reader is gone" (seen by giving the hook `os.Stderr` in a throwaway
   edit), so `who` stays empty. Give the hook a stdout pipe and drain it.
3. No build tag separates tiers: edge/bus/functional_test.go, edge/forge/functional_test.go,
   edge/git/functional_test.go, edge/worker/functional_test.go,
   edge/worker/hook_functional_test.go and cmd/nova-worker/break_test.go run real
   processes, sockets and redis-server under plain `go test`. edge/worker/break_test.go:239-257
   (`TestBreakHarnessTimeoutHangsOnBackgroundChild`, not named functional) runs real
   `sh`, `time.Now` and `time.After(5 * time.Second)`: a unit-tier file using real time
   and processes, against the unit-test rule.
4. No test of `OSProcessTable.Alive` against a real child after the in-process map is
   cleared (defect 1); it would fail on darwin today.
5. No test of two `LandPass` calls on one `RepoDir` at once (defect 2).
6. No test of a `forge.FaultError` from `PR`, `Checks` or `Merge` inside `LandPass`
   (defect 3).
7. No test of `RunTest` returning `ErrNoSlot` or a wall fault in
   `rebaseAndTestStep_internal` (defect 4).
8. No test with two concurrent jobs sharing one `KeyProxy` (defect 5).
9. No test of `CLISprintClient` with stderr on success, or with exit 2 (defect 6, 7).
10. No test that the landed heads are ancestors of the merge commit, or of the squash
    fallback (defect 9).
11. No test of a working card whose job directory is from another epoch, or has no
    job.json (defect 10).
12. No test of edge.Exec after a normal exit with a background grandchild
    (`sh -c "sleep 20 & exit 0"`): the existing breaker covers only the timeout path
    (defect 12).
13. No test that a pending CI is bounded (defect 13).
14. No test that a normal clean removes a job directory holding a read-only module
    cache (defect 23); retire_test covers only the retire path.

## Questions for the coordinator

1. Are several streams on one repository supported? If yes, defect 2 is a blocker as
   ranked; if a stream always owns its repository, it drops to a doc sentence.
2. Is "require branches to be up to date" assumed to be set by hand on every stream's
   base? If so, should stream add read it (defect 8), and should a repository that
   allows only squash or rebase be refused (defect 9)?
3. A work harness may write `{"result":"fault"}` and it is honoured
   (harness.go:318-319), which spends nothing. JOB.md tells only reads and missing
   routes to use it. Is an agent allowed to self-classify a work card as a fault, and
   what bounds the faults of one card?
4. Do nova-reserve and nova-sandbox kill their own process group when the harness
   exits? If they do, defect 12 is minor; nothing in this repository proves it.
5. Does mirror-refresh `gc` the bench mirrors? If it does, defect 26's alternates are a
   live risk; I could not see that repository from here.
6. Guarantee 9's second clause against defect 78's "a withdrawn job pushes nothing":
   which one is the contract? The model already follows defect 78.
7. Layer 4 has no ledger in docs/ENFORCED.md. Is one owed, as for layers 1, 2 and 5?
