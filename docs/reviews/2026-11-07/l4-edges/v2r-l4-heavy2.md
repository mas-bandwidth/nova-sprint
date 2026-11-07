# Cold review: layer 4, the edges (v2r-l4-heavy2)

Reviewer: rowan (opus), 2026-11-07, at `43d849e2` on `v2-stitch`. Weight on
`edge/git` and `edge/bus`, as the card asked; `edge/worker`, `edge/forge`,
`edge/exec.go` and `cmd/nova-worker` were sampled where git and bus reach
them. Report only: nothing in the tree was changed. Two throwaway functional
probes were written into `edge/git/`, run and deleted (output below).

Commands run:

```
$ go build ./... && go vet ./... && echo BUILD-VET-OK
BUILD-VET-OK
$ for p in ./edge ./edge/bus ./edge/git ./edge/forge ./edge/worker ./cmd/nova-worker; do go test -count=1 -skip Functional $p; done
ok  github.com/mas-bandwidth/nova-sprint/edge          0.788s
ok  github.com/mas-bandwidth/nova-sprint/edge/bus      0.126s
ok  github.com/mas-bandwidth/nova-sprint/edge/git      1.570s
ok  github.com/mas-bandwidth/nova-sprint/edge/forge    0.131s
ok  github.com/mas-bandwidth/nova-sprint/edge/worker   3.445s
ok  github.com/mas-bandwidth/nova-sprint/cmd/nova-worker 0.151s
```

All green, none slow. The unit tier is not clean of real processes and the
wall clock (D14).

## Verdict

Not sound yet. The single pieces are careful: argv only, typed outcomes,
leases on every ref the edges move, and refusals that name their fix. But
the landing is only safe if one pass runs at a time per base, and the code
does not keep that. `run` starts one goroutine per land request
(run/edges.go:86). Every stream on one repository shares one clone with one
working tree (run/edges.go:271-279). The last base check before the merge is
a check-then-act with no lock and no GitHub guarantee behind it
(edge/git/land.go:215-223). The worst case: two streams' heads that are each
green alone, and red together (the model's own c1/c3 pair), are gated
against the same base tip in parallel. Both pass the `now == BaseTip` check,
and both merges land. That breaks `BaseNeverRed` and guarantee 4, and
`tla/LandEdge.tla` cannot show it because its `Pass` is one atomic step. The
bus edge's flaw is narrower: it hands nova-bus the whole process environment,
DSN included, against the allowlist its caller builds.

Defects: 2 blocker, 7 serious, 6 minor, 3 nit.

## Defects

**D1. blocker: parallel landings on one base merge un-gated combinations.**
edge/git/land.go:215-223 reads base (`LsRemote`) and only then calls
`fg.Merge`. run/edges.go:81-90 starts `go e.land(...)` for each request, so
two streams on one base run `LandPass` at the same time. Nothing checks for
the "require branches to be up to date" protection that the doc relies on: a
grep for `strict`/`up to date` in edge/, verbs/ and cmd/ finds only
sql_dump.go, and verbs/admit_stream.go:18-27 asks only for `RequiredChecks`.
Why it matters: s1 (c1) and s2 (c3) are each gated green on base tip B, both
see `now == B`, and both `gh pr merge` calls succeed. Base becomes B+c1+c3,
which no CI ever saw. That breaks guarantee 4 (docs/LAYER-4-EDGES.md:393),
"base pushes one at a time in stream order" (:130-131), and `BaseNeverRed`.
Repair: run the gate check and the merge of every stream on a base under one
per-base serializer in run (or the git edge), and have `stream add`/`check`
refuse a base without strict status checks, as :149 promises.

**D2. blocker: concurrent landings share one working tree.**
run/edges.go:271-279 makes one clone per URL. `MergeGroup` mutates that
clone's HEAD, index and working tree (`checkout --detach`
edge/git/git.go:279, `merge` :310, `rev-parse HEAD` :298). D1's parallel
goroutines run on the same directory, as does `readHeads`' `Clone`
(run/edges.go:318). Why it matters: stream A's `rev-parse HEAD` can return
stream B's merge, which A then pushes to `sprint-group/A` (land.go:184) and
gates. That is two streams in one branch, which the doc says never happens
(:192-193, LPM M5). A's `LandedHeads` (land.go:228) then names A's heads,
which are not in what merged. Two first-time `git clone`s into the same
`dir` also race (git.go:567-570). Repair: give each stream its own `git
worktree` (or its own clone dir keyed by stream), and hold a per-directory
mutex around every `MergeGroup`.

**D3. serious: git output past 64 KB is silently cut, and the cut is used
as data.** edge/git/git.go:123 builds every git `Cmd` with no `MaxBytes`, so
edge/exec.go:108 caps stdout at 64 KB, keeping the head and tail around a
`...[truncated]...` marker (exec.go:191-215). `Result.Truncated` is never
read anywhere in edge/git (grep: no hits outside exec.go/request.go).
Effects:
(a) `mergeLedgerLines_internal` (git.go:365-373) writes the truncated `git
show :2:`/`:3:` output into the ledger file, marker included, and the merge
reports ok.
(b) `Heads` (git.go:488-503) returns a partial map without an error, so
`GroupBlocker` (verbs/land.go:117-121) can miss a blocking branch.
(c) `lsRemoteSHA_internal` (git.go:200-242) can say "not on origin".
Probe output (a throwaway functional test, deleted afterwards):

```
$ go test -count=1 -run TestFunctionalReviewLedgerOver64KB -v ./edge/git/
    zz_review_functional_test.go:38: kind=ok err=<nil> reason=""
    zz_review_functional_test.go:40: original 90000 bytes, merged 65542 bytes, has marker: true, lines 2187
$ go test -count=1 -run TestFunctionalReviewHeadsOver64KB -v ./edge/git/
    zz_review_functional_test.go:32: branches on origin 1501; Heads returned 909 (err <nil>); Branches returned 0 (err git ls-remote --heads ... (a line is not <sha> ...
```

Repair: have `runGitEnv_internal` refuse any result with `Truncated` set
(or read ledger blobs with `git cat-file` into a file, not through Exec's
capped buffer).

**D4. serious: "landed" is recorded without checking that the heads are on
base, and the merge method can make that permanently false.**
edge/git/land.go:228 sets `LandedHeads = req.Heads` right after `fg.Merge`
returns. edge/forge/forge.go:478-488 falls back to `squash` or `rebase` when
merge commits are off. Why it matters: step 7 (docs:151-152, LPM M21) says
"each landed head is checked an ancestor of the pushed base tip". With
squash, no head is ever an ancestor. Layer 3 records `Landed` anyway, and
every later `allAncestors_internal` (land.go:336-343) and the hand lander's
`IsAncestor(head, tip)` (cmd/nova-sprint/lander.go:71) disagree with the
store. Repair: after `Merge`, fetch the merge sha and check each head with
`IsAncestor`, refusing (as a typed outcome) a repository whose only allowed
methods rewrite commits.

**D5. serious: a git process killed by a timeout wedges the lander's clone
for good.** edge/exec.go:113-118 SIGKILLs the process group on timeout. A
`merge` killed mid-run (git.go:310-312 returns on `err != nil` without
cleanup) or a `rebase` (git.go:419-422, same) leaves `.git/index.lock`,
`MERGE_HEAD` or `rebase-merge/` behind. Every later pass then fails at
`checkout --detach` (git.go:279) and is returned as a fault. `Clone`
(git.go:567-568) treats any existing `.git` as a good clone, so there is no
path back. A first clone is also bounded by `RebaseTimeout` (2 min,
git.go:570), so a large repository's clone is the likeliest way to leave a
half `.git` that is then trusted forever. Repair: start every `MergeGroup`
with `merge --abort`/`rebase --abort`/removal of a stale `index.lock` and
`reset --hard`, and have `Clone` verify (`rev-parse --git-dir` plus a fetch)
or re-clone.

**D6. serious: the bus and git edges hand the whole process environment
(DSN, tokens) to their subprocesses.** edge/bus/bus.go:74 and :107 build
`edge.Cmd{... Env: e.env}` without `CleanEnv`. edge/exec.go:120-124 then
runs `append(os.Environ(), cmd.Env...)`. The caller curates `Env` with
`toolEnvironment` (cmd/nova-sprint/open.go:71, main.go:83), whose stated
purpose is "keeps database credentials out of fleet and bus subprocesses"
(cmd/nova-sprint/environment.go:14), and that curation is lost. The same
holds for every git call (git.go:123) and for the repository's ledger script
(git.go:348), which also gets `NOVA_SPRINT_GIT_TOKEN` through
`gitEnv_internal` (git.go:94-97). Why it matters: "The DSN reaches only the
verb process" (docs:121-123). A ledger script is repository code. Repair:
set `CleanEnv: true` in bus.Edge's two Cmds and in the git edge, passing
PATH/HOME explicitly as the worker's `OSGitOps` already does
(edge/worker/runner.go:1058-1061).

**D7. serious: a withdrawn job can still push, because the guard reads the
worker's last view, not the store.** edge/worker/retire.go:61-65
(`holds_internal`) compares against `r.gone`, which only a pass fills in.
Pushes at harvest and after rebase (runner.go:621, :690) happen whenever the
job finishes, which can be up to a periodic wake (10 s) after a withdrawal.
The model's guard is the store itself: `Push(c) == ... storeState[c] =
"working"` (tla/Runner.tla:156-158). Why it matters: "none once the consumer
is final ... a withdrawn job pushes nothing" (docs:281-282, guarantee in
defect 78). If the next holder has staged but not pushed, its own lease
(`StartTip`) is now stale and its push is refused. Repair: before every
push, read the consumer's gen/state from the store (`queue --as`/`show`) and
model that read as a separate step that a `Withdraw` can interleave with.

**D8. serious: the worker pushes with the bench's ambient git credentials,
not the row's token.** edge/worker/runner.go:994-1001 runs `git push
--force-with-lease ...` through `runGit_internal` (runner.go:1056-1066),
whose environment is `PATH`, `HOME` and `GIT_TERMINAL_PROMPT` only. Whatever
credential helper `$HOME/.gitconfig` names answers, which can be a person's
keychain on a Mac bench. Why it matters: "push the branch with the row's
token" (docs:275-276) and "Workers push and open PRs with their row's token"
(:218). The git edge itself takes care that "a person's keychain never
answers for the sprint" (docs:186-190, edge/git/url.go:84-91), and the
worker does not. Repair: give `OSGitOps` the same `credentialEnv_internal`
or `tokenEnv_internal` reset as `git.Client`, with the row's token from
nova-secrets.

**D9. serious: ledger merging is promised but never wired.**
run/edges.go:131 builds `git.LandRequest{...}` with no `Ledgers`, and no
non-test code anywhere builds a `LedgerRule` (grep: only edge/git). Why it
matters: "Ledger files (policy's list per repository) are merged entry by
entry or regenerated by the repository's ledger script" (docs:137-138).
Today every ledger conflict is a rung-3 judgement, and all of git.go:342-389
is code that only tests reach. If it were wired today, D3(a) and D12 would
corrupt ledgers. Repair: wire policy's per-repo ledger list into
`LandRequest` after fixing D3 and D12, or delete the ledger path and the
promise.

**D10. minor: every fetch failure is typed "ref not on origin".**
edge/git/git.go:150-152 wraps any non-zero `git fetch` exit in
`ErrMissingRef`. Why it matters: "a failure is a typed result (conflict with
files, push refused, host silent)" (docs:26-27). An auth failure or an
unreachable host (exit 128) reads as a missing head. Repair: return
`ErrMissingRef` only when stderr says "couldn't find remote ref"/"not our
ref", else the plain command failure.

**D11. minor: pending and base-moved are retried without bound.**
run/edges.go:165-168 returns `true` for `OutcomePending` and
`OutcomeBaseMoved` with no limit. Only `OutcomeAbsent` is bounded by `wait`
(:169-171). Why it matters: a CI queued forever, or a PR whose head never
reads as the gated sha (land.go:205-207, also Pending), keeps a goroutine
re-merging every 30 s forever. The doc bounds the CI wait at "the
repository's job timeout plus 10 min" (docs:352-353). Repair: carry one
deadline across all non-final outcomes of a request, and report `ci_fault`
when it passes.

**D12. minor: the line-union ledger merge loses lines and brings deleted
ones back.** edge/git/git.go:375-389 drops every blank line (`line != ""`)
and every repeated line (`seen`) of both sides, and ignores the merge base,
so an entry one side removed comes back. Why it matters: "merged entry by
entry" (docs:137) is not "set of distinct non-empty lines". A markdown
ledger with blank separators is reflowed. Repair: do a three-way merge
against `:1:` that keeps both sides' additions in order (git's `merge=union`
driver semantics), or require a ledger script.

**D13. minor: an error in the single-head ancestor check is swallowed.**
edge/git/git.go:268: `if ok, err := c.IsAncestor(...); err == nil && ok`.
An error (a timeout, a missing object) silently takes the full-merge path.
Repair: return the error.

**D14. minor: the unit tier runs real processes and the wall clock.**
edge/break_test.go:25-49 (`sh -c "sleep 5 ..."`, `time.Sleep`),
edge/break_test.go:64-65 (`NewOSExec()`), and edge/git/break_test.go:23-34
(real `git clone`, `gc`, `New(Config{})`) are not named `Functional`, so
`go test -skip Functional` (Makefile:26, ci.yml:69) runs them as unit tests.
The lint written for this exempts them by file name
(edge/coldread2_test.go:125). Why it matters: the build rule says unit tests
use no real time and no outside programs. Repair: rename them `...Functional...`
and drop the file-name exemption.

**D15. minor: `lsRemoteSHA_internal` is unreachable, and unbounded if
reached.** edge/git/git.go:156-157 sends a full-sha ref to
`lsRemoteSHA_internal` (:200-242), but no production caller passes a sha
(all six `LsRemote` calls pass `refs/heads/...`). If it were called, it
would fetch every ref origin lists, one at a time, with duplicates and
`refs/pull/*` included, each bounded at 60 s. Repair: delete it; the landing
reads branches by name since the fix break_test.go:13-22 records.

**D16. nit: the bus cap counts the body only.** edge/bus/bus.go:66 checks
`len(body)` against 4096. Guarantee 3 says "A bus message is at most 4 KB"
(docs:391), and the subject travels too. Repair: check `len(subject) +
len(body)`.

**D17. nit: the recipient is trimmed for validation only.**
edge/bus/bus.go:64 validates `strings.TrimSpace(to)`, but argv (:69) and
the confirmation match (:100) use `to` untrimmed. `"amd-1.ds "` passes
validation and then never confirms, so the cursor never moves and the send
repeats every tick. Repair: trim once at the top of `send`.

**D18. nit: `Who` does not check its own header count, and keeps the last
of any duplicate name.** edge/bus/bus.go:119 checks only the `WHO OK`
prefix, not `present=<n>` against the lines read, and :135 overwrites a
repeated name. Repair: parse `n` and refuse on a mismatch or a duplicate.

## Promise gaps

- "base pushes one at a time in stream order" (docs:130-131). The code
  starts a goroutine per request, unordered (run/edges.go:86), with a
  shared clone (D1, D2).
- "`stream add` refuses a protected base without it, naming it"
  (docs:149). No check for strict up-to-date protection exists
  (verbs/admit_stream.go:18-27, D1).
- "A head origin does not hold refuses the landing for this pass, naming
  it" (docs:134-135). Only the branch's existence is checked
  (land.go:79-84). The branch-at-head check covers rung 1 only
  (land.go:148). On rung 2, a head whose branch has moved on is still
  fetched by sha and merged.
- "Ledger files (policy's list per repository) are merged entry by entry"
  (docs:137-138). Never wired (D9).
- "Push the merge to `sprint/<stream>`" (docs:140). The code uses
  `sprint-group/<stream>` (git.go:443). The Built list (docs:532) names a
  third form, `sprint/<stream>/group`, plus `StreamBranch`, `StreamRef` and
  `RunRemoteLandStep`, none of which exist (grep finds only a coldread
  comment).
- "Without PRs: `git push origin <sha>:refs/heads/<base>`" (docs:145-147).
  `LandPass` refuses any stream with no forge (land.go:103-105), and only
  the hand lander pushes base (cmd/nova-sprint/lander.go:106).
- "**Record**: each landed head is checked an ancestor of the pushed base
  tip" (docs:151-152). Not done (D4).
- "The edge runs a fixed binary there (`nova-sprint land-step`, argv only)
  over ssh ... a host that does not answer is skipped" (docs:197-201). There
  is no landing host and no ssh: `LandPass` runs inside the run process on
  its own clone (run/edges.go:120-138).
- "The DSN reaches only the verb process" (docs:121-123) and
  "`toolEnvironment` keeps database credentials out of ... bus subprocesses"
  (environment.go:14). Broken by D6.
- "push the branch with the row's token" (docs:275-276); "Workers push and
  open PRs with their row's token" (:218). Broken by D8.
- "none once the consumer is final or a newer generation holds it"
  (docs:281-282). Only as far as the worker last read (D7).
- "Checked by `tla/Push.tla`, `bus.Fake`" (docs:23-24, :465-467, :472).
  Neither exists (`ls tla | grep -i push`: nothing; no `Fake` in edge/bus).
  The cursor is now run's, so the claim belongs in layer 6, or should go.
- The `STOP` subject that run sends through this edge (run/edges.go:104;
  SEAMS.md:147) is missing from the bus edge's subject table (docs:58-63),
  which reads as the closed list of what is sent.
- docs/ENFORCED.md covers layers 1, 2, 3 and 5 only (its title,
  ENFORCED.md:1). No layer-4 promise has a ledger row, so none of the 13
  guarantees (docs:388-407) is traced to an artifact.

## Model vs code

- `tla/LandEdge.tla` `Pass(s)` (lines 111-135) reads `Tip`, gates and
  appends to `base` in one atomic step, so two streams can never both be
  gated on one tip and both land. The code splits read, gate and merge
  across network calls (land.go:118, :212, :215-223) and runs streams
  concurrently (run/edges.go:86). The model checks a stronger system than
  the one built (D1). It needs `Pass` split into `Gate(s)` and `Merge(s)`
  with interleaving, and a reversed witness of two streams' greens landing
  together.
- The model's green branch sets the new base tree to `Gated(s)` exactly
  (LandEdge.tla:124). The code's squash/rebase fallback
  (forge.go:484-487) gives a base with the same tree but none of the head
  commits, and the trace walk records `landed` from `LandResult`, never
  from the base's ancestry (D4).
- The shared working tree of a clone (D2) is state the model does not
  have. The git fake (edge/git/fake_test.go:338-366) holds a world mutex
  and has no working tree, so neither the trace walk nor the unit tests can
  reach the interleaving.
- The git fake records every merge commit as if it were on origin
  (fake_test.go:409, `f.world.originRefs[sha] = sha`) without any push. A
  real merge that was never pushed would not be on origin, so tests on the
  fake cannot catch a lost group push.
- `tla/Runner.tla` `NoPushAfterFinal` (:409-410) is
  `branch[c].finalPushes = 0`, and no action in the spec ever increments
  `finalPushes` (it appears only at :64, :362 and :410). The invariant
  holds by construction, and only the witnesses can break it. The safety
  actually comes from `Push`'s guard `storeState[c] = "working"` (:158),
  which the code implements as a cached local view (D7).
- `tla/Push.tla` and its witness `PushCursorBeforeConfirm` are cited
  (docs:465-467, :545) and do not exist.

## Simplifications

- Two git implementations with different rules: `edge/git.Client`
  (credential reset, full process env, 64 KB cap, no locks) and
  `edge/worker.OSGitOps` (clean env, ambient credentials, per-clone
  RWMutex). One git runner with one env and credential policy would remove
  D6 and D8 together.
- Delete `lsRemoteSHA_internal` and the full-sha branch of `LsRemote`
  (git.go:156-157, :200-242; D15).
- Delete `bus.Config.AllowNew` and `--new` (bus.go:36, :43, :70-72).
  Nothing sets it (grep: only bus.go).
- git.go:325-327 is dead. Inside `len(nonLedger) > 0 ||
  len(ledgerFiles) == 0`, `len(nonLedger) == 0` implies `conflicts` is
  empty, so `nonLedger = conflicts` assigns nothing.
- `LsRemote` (git.go:155-172) and `Tip` (:177-198) are the same `ls-remote
  --exit-code` with different handling of a sha-less answer: one returns
  ("", false, nil), the other an error. One function with `Tip`'s stricter
  rule would do.
- `Who` goes through `edge.RunArgv` while `send` builds a `Cmd` (bus.go:107
  vs :74). One way is enough.

## Missing tests

- Two streams of one repo landing at once against a real bare origin
  (both green alone, red together): D1, D2.
- Any git output past 64 KB: `Heads` with more than about 1,000 branches,
  a ledger blob past 64 KB (D3; the two probes above are the shape).
- Recovery of a clone left with `index.lock`, `MERGE_HEAD` or
  `rebase-merge/` by a killed git (D5).
- That bus and git subprocesses see only the allowlisted environment: a
  `FakeExec` assertion on `Cmd.CleanEnv`, plus a functional run with a
  canary `NOVA_PG_DSN` (D6). edge/bus/bus_test.go never looks at `Env`.
- A squash-only repository through `LandPass` with `forge.Fake` mirroring
  GitHub's squash, asserting that `landed` is refused or that the heads are
  verified (D4).
- A withdrawal that lands in the store between the worker's last pass and
  the job's harvest push (D7).
- `bus.Send` with a whitespace-padded recipient and with a subject plus
  body over 4 KB (D16, D17).
- The worker's push credential: the env of `OSGitOps.Push` names no
  `$HOME` helper (D8).

## Questions for the coordinator

1. Is the base meant to require GitHub's strict status checks, which would
   make D1's window GitHub's to refuse? Or must the sprint serialize
   landings per base itself? The doc says the first (docs:147-149) and the
   code checks neither.
2. Which merge methods are allowed? If squash/rebase are, "landed" cannot
   be proved by ancestry, and layer 2's `Landed` (`head == p.head`) needs
   another fact (the PR's merged state).
3. Are ledgers in scope for v2? If yes, D3, D9 and D12 come first. If not,
   the ledger code and the promise go together.
4. Should the landing still run on a landing host over ssh (docs:197-201),
   or is landing in the run process the decision, with the doc owed an
   edit?
5. Which credential should a bench's push use: the row's token
   (docs:218, :275) or the host login (w4I made commits carry the sprint's
   identity)? D8 depends on the answer.
