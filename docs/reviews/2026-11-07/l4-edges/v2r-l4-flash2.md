# Review: layer 4 (the edges), cold read, v2r-l4-flash2

Base: 43d849e2 (v2-stitch). Reviewed: edge/** (exec, bus, git, forge, worker), cmd/nova-worker/**, docs/LAYER-4-EDGES.md, tla/LandEdge.tla. Every non-test file of edge/git, edge/forge and edge/bus was read; edge/worker was read in the parts that touch the wall, the job record, result classification and ErrNoSlot (runner.go, harness.go, probe.go, outcome.go); pass.go, recover.go, retire.go, proxy.go and verbs.go were read only at the call sites named below, not in full. Nothing else was changed.

## Verdict

Layer 4 builds and vets clean (`go build ./... && go vet ./...`, exit 0). Unit tests pass for edge, edge/forge, edge/git and edge/worker. Two functional tests are red on this bench: `go test ./edge/bus -run TestFunctionalNovaBusWhoAndSend` fails with "expected sprint and amd-1.ds in nova-bus who" (5.05 s), and `go test ./cmd/nova-worker -run TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot` fails with "no report reached nova-sprint" (5.75 s). I did not isolate either cause; both look environmental (live nova-bus presence, nova-reserve/nova-sandbox) and need a bench check. The layer mostly keeps its contracts: exec argv-only with timeouts and capped output, the bus cursor rule, and the forge rate bucket all match their docs. The single worst thing is in the landing: `edge/git/land.go:215-223` re-reads the base tip and then calls `fg.Merge`, which takes no base argument, so a base that moves in the window lands the PR onto a base CI never gated. The model `tla/LandEdge.tla:125-131` treats the check and the merge as one atomic step, so it cannot see this window.

## Defects

Ranked, most severe first. Severity: blocker | serious | minor | nit.

1. **serious** `edge/git/land.go:215-223`, `edge/forge/forge.go:434-464`. The base is re-read (`g.LsRemote` at land.go:215, compared at 219) and then `fg.Merge(ctx, req.Repo, pr.Number, res.GatedSHA)` runs at 223. GitHub's `gh pr merge --match-head-commit` (forge.go:443) checks only the PR head, and `Merge` (forge.go:434) has no base parameter. A landing from another stream that moves base between the read at 215 and the merge at 223 is merged onto the new base, whose tree CI never gated. Why it matters: the promise "only onto the base it was gated on" (LAYER-4-EDGES.md item 4) breaks in a window the model does not show. The prevention boundary is the comparison at land.go:219. Past that read this merge cannot be refused for a moved base: `Merge` takes no base tip, and `--match-head-commit` matches only the head. Repair: passing the gated base tip into `Merge` and reading the PR's base ref oid still only refuses the merge when that read happens before the call, on the same side of that boundary. A check after the merge, that the merge commit's first parent is the gated base tip, returning `base_moved` if not, is detection and recovery: the merge has already been recorded on the base GitHub used.

2. **serious** `edge/git/git.go:150-151` (Fetch) and `edge/git/git.go:542-543` (Push). A nonzero exit of `git fetch` is wrapped as `ErrMissingRef` (git.go:150-151) and a nonzero exit of `git push` becomes `PushRefusedError` (git.go:542-543), including authentication failure, DNS failure, and a host that git itself reports. An execution error or a timeout is not reclassified: Fetch returns it unchanged at git.go:147-148 and Push at git.go:539-540. The doc promises a typed result for "host silent" (LAYER-4-EDGES.md:26-27), and no such kind exists. Why it matters: a nonzero fetch is `ErrMissingRef` ("git: ref not on origin") even when the ref was not what failed, and a nonzero push is `PushRefusedError` even when the server did not reject the update. Fetch's error is what land.go:134, git.go:215 and git.go:228 return; a nonzero push is what land.go:184 records as `branch_failed`. A timeout stays the exec error. land.go:217 does not consume Fetch's `ErrMissingRef`. It calls `LsRemote` (land.go:215) and propagates that call's error: `cmp.Or(err, ErrMissingRef)` is `ErrMissingRef` only when `LsRemote` returns no error and the ref is absent (git.go:163-164, git.go:171). Repair: on a nonzero exit, classify git's stderr into a `HostSilent` kind (could not resolve host, connection timed out, authentication failed) and reserve `PushRefusedError` for the server's "rejected" lines. Leave the execution and timeout errors returned at git.go:147-148 and git.go:539-540 unchanged.

3. **serious** `edge/worker/runner.go:1161`. `runWallCmd_internal` returns `ErrNoSlot` when the command exits 1, its combined output contains "nova-reserve:", and it does not contain "nova-reserve: HELD". That function is shared by `RunHarness` (runner.go:1088) and `RunTest` (runner.go:1095), so a gate can be classified `ErrNoSlot` by the same sniff. The restage at runner.go:549-552 belongs to `RunHarness`: `runStep_internal` sets the job back to `JobStaged` and returns the error. It does not run for `RunTest`. The `RunTest` caller at runner.go:678-686 marks the gate fail (`Result: "fail"`, reason `tests red`) when `RunTest` returns an error or a nonzero exit, including an `ErrNoSlot`. Output that contains "nova-reserve: HELD" is not `ErrNoSlot`. That same caller marks the gate fail for a nonzero exit or another error from `RunTest`; it does not restage. Repair: have the reserve call return a dedicated exit code for "no slot" and check it only for the harness reserve call, never on a wrapped command's output.

4. **minor** `edge/git/git.go:268`. `if ok, err := c.IsAncestor(ctx, repoDir, onto, heads[0]); err == nil && ok {` discards the error and falls through to the merge path (git.go:279). That does not by itself create a merge commit. Only if that later merge succeeds, where the reuse path would have returned the head (git.go:276), does the group SHA become the merge commit instead of the head, so CI runs on the new SHA rather than the reused one (land.go:163-165 is the unchanged-SHA case, which does not restart CI). If the later merge does not succeed, the ancestry error is gone and the group SHA does not change: a checkout failure returns at git.go:279, and a merge error or conflict returns at git.go:294-295 (the conflict at git.go:334). Repair: check `err` first and return it.

5. **minor** `edge/exec.go:120-124`. With `CleanEnv: true`, `c.Env = slices.Clone(cmd.Env)` (exec.go:121). A nil `Env` stays nil (`slices.Clone` preserves nil), and os/exec then uses the parent's environment. A non-nil empty `Env` clones to a non-nil empty slice, which os/exec uses as an empty environment. No live caller hits a nil `Env`: runner.go:1061, runner.go:1138, probe.go:170, forge.go:208 and cmd/nova-sprint admit.go:61 all pass a non-empty `Env`. Repair: when `CleanEnv` is set, use a non-nil slice (`append([]string{}, cmd.Env...)`) so a nil `Env` and an empty one both mean an empty environment.

6. **minor** `edge/forge/actions.go:105, 109, 162`. Runs and jobs are fetched with `per_page=100` and no pagination, so a head with more than 100 runs drops the rest. This fails safe: a red check with no matching run goes to `unmatched_internal` (land.go:309-325) as new red. It does mean the inherited/new split can be wrong. Repair: follow pages or read `total_count` and refuse above 100.

7. **minor** `edge/forge/forge.go:434-464`, `edge/git/land.go:223-226`. If `gh pr merge` returns an error after GitHub has already merged, `Merge` reports refusal and the landing records `merge_refused`, though the PR is merged. Repair: on error, re-read the PR's state (MERGED plus mergeCommit) before returning a refusal.

8. **minor** `edge/git/git.go:228-239` (`lsRemoteSHA_internal`). Every call fetches every remote branch tip (one `git fetch` per ref) to test whether a sha is contained. A landing read therefore costs one fetch per origin ref. Repair: one `git fetch` of the needed tips, or `git branch -r --contains` after a single fetch.

9. **minor** `edge/worker/runner.go:571`. `SANDBOX REFUSED` as a substring, and exit codes 125-127 from the harness process itself, are all classed as infrastructure faults, so a harness's own inner "command not found" becomes a fault. It is labelled, so not silent. Repair: scope the fault test to the wall's own exit path.

10. **nit** `edge/bus/bus.go:118-119`. `len(lines) == 0` is never true, because `strings.Split` returns at least one element. `bus.go:135` lets a duplicate presence name silently overwrite the earlier one. Repair: delete the dead check; reject duplicates with an error.

11. **nit** `edge/git/git.go:155-171` (`LsRemote`). Exit 0 with no sha on the first line returns "absent" silently. Repair: return an error naming the output.

12. **nit** `edge/forge/actions.go:258`. `l[:min(len(l), LineMax)]` cuts bytes and can split a UTF-8 sequence. Repair: cut at a rune boundary.

## Promise gaps

- LAYER-4-EDGES.md:26-27: "a failure is a typed result (conflict with files, push refused, host silent)". The code has conflict (`MergeConflict`, git/git.go:334), push refused (`PushRefusedError`, git.go:542-543, a nonzero exit only), and no host-silent type. A nonzero fetch exit is `ErrMissingRef` (git.go:150-151); an execution error or a timeout is returned unchanged (git.go:147-148, git.go:539-540). See defect 2.
- LAYER-4-EDGES.md item 4: "merges only the exact sha CI passed, only onto the base it was gated on". The sha is checked by GitHub (`--match-head-commit`, forge.go:443). The base is checked by the code at land.go:219, but not at the merge itself. See defect 1.
- LAYER-4-EDGES.md item 2: "Checked by `tla/Push.tla`". There is no `tla/Push.tla` in the tree (`ls tla` lists LandEdge, Runner, Setup and others, no Push). The bus cursor rule has no model in this tree.
- LAYER-4-EDGES.md item 1: "a timeout or missing binary is an error naming the program and the fix". This holds: exec.go:142-165 builds it, and a non-zero exit is returned as a result, not an error (exec.go:158-160). No gap found.

## Model vs code

- `tla/LandEdge.tla:125-131` models "gate green, then merge while base is still the gated tip" as one atomic action. In the code the base is read at land.go:215, compared at 219, and merged at 223, with the forge call between. The model cannot produce the interleaving in defect 1. Add an outside action "base moves between the recheck and the merge" and check the invariant "base is never red from a landing" (`tla/LandEdge.tla:177-180`) against it.
- `tla/LandEdge.tla:182-188` (nothing lands that CI did not pass) holds only if the window in defect 1 is closed.
- The single-head reuse path (git.go:268) and its swallowed error (defect 4) are not modelled.

## Simplifications

- Two git runners exist side by side: `OSGitOps.runGit_internal` (runner.go:1056-1069) with its own environment and `git.Client` (git/git.go:122-136) with the token environment. Both carry timeout and error wrapping. One runner would have one place to get git right (the clean env, the hook disabling, the credential path).
- `edge/exec.go:180-190`: the `max < 64` branch in `cappedBuffer.Write` is a second code path. Requiring a minimum cap would delete it.
- `edge/exec.go:classifyExecErr_internal`: sets `res.ExitCode = -1` in four branches; set it once before the switch.
- `edge/git/git.go:133-135` and `:146-151`: `failed_internal` is built at four sites with the same argument shape; a single helper over (argv, code, out, err) would do.

## Missing tests

- A forge/git fake that moves base inside `Merge` (defect 1). No test I found covers a base that moves between the recheck and the merge.
- A `Fetch` and a `Push` whose stderr says the host cannot be reached (defect 2): expected a host-silent kind, not `ErrMissingRef` or `PushRefused`.
- `exec`: `CleanEnv` with a nil `Env` must produce an empty environment (defect 5).
- `forge/actions`: a head with more than 100 runs (defect 6).
- `worker`: a RunTest gate that exits 1 and prints "nova-reserve:" must not be classed as no-slot (defect 3).
- The two red functional tests in "Verdict": their preconditions (a hooked sprint in nova-bus who; a live nova-reserve/nova-sandbox) need to be stated in the test or the test skipped when absent.

## Questions for the coordinator

- Is a consumer's job directory `jobs/<consumer ID>` ever reused across attempts? `edge/worker/harness.go:287` reads `result.json` from it, and nothing in the runner removes a stale file before a harness run (grep shows no removal). If a directory is reused, a previous attempt's verdict is read as this one's.
- `edge/git/git.go:535-536`: with an empty `expectOld` on a group ref, `--force-with-lease=<ref>:` is sent. Does that mean "the ref must not exist" on this git version, or does it mean something else? I could not confirm it from the code alone.
- `edge/worker/runner.go:549-552`: a job re-staged on ErrNoSlot has no visible bound. Is the staging bounded elsewhere, or can a job stay staged forever on a full bench?
- Are the two red functional tests (bus presence, shipped-worker break test) expected to fail on this bench, or is this a real regression? I did not isolate it.
