# CHECKLIST: before you finish a card

Every worker and every friend runs this list between "the work is done" and `nova-sprint finish`.
It is the read you do on yourself before a reader does it for you. An honest HOLD costs one
message. A broken LAND costs a full read and rework cycle.

## 1. Know what you are finishing

1. Read the live card: `nova-sprint card <id> --brief`, and `nova-sprint friend cards <you>` for
   the current job, generation, epoch, lane and branch. The deal can change while you work. Never
   finish a stale generation, and never replay an old finish into a new one.
2. Apply the brief's latest CARRY and FIX lines and any owner ruling it cites. Build on the
   carried head, and keep its ancestry.

## 2. Read your own patch

3. Read the whole effective diff against the current BASE, not just your last commit. Check
   whether it does what the card asks, what happens at the edges, and whether any fix hides a
   failure by weakening a workload, an assertion, a timeout or a bound. Run `git status` and
   `git diff --check`, and make sure no stray script or log enters the patch.
4. Compare every changed path to PATHS and NEW. Any other file goes on a `PATHS-PROPOSED:` line in
   the report. A PATHS-PROPOSED line is a request: the coordinator decides it.
5. For every name or path you renamed or removed, grep the whole repository (code, tests, docs,
   `tla/`, `standard/`). Fix every stale reference. Any reference you keep on purpose (history,
   fixtures) gets one line in the report saying why.
6. Unit tests use no real time, no sockets, no threads, no network and no outside services. Time
   is an argument, and tests start the clock at 1000.0. Anything else belongs in the functional
   tier.

## 3. Run the gates at the exact head

7. Merge the current BASE tip first.
8. Run every DONE-WHEN command at the exact head you will finish, in a **Debug** build, on a bench
   the card allows. Release-only green does not count. For each run, record the command, any
   environment overrides, the full SHA, the platform and toolchain, the exit status and the
   duration.
9. None of these is a pass: a run that was queued, cancelled, timed out, interrupted, has no exit
   status, or only compiled; a test that prints SKIPPED and exits 0; a case that never ran. An XDP,
   BPF or root case passes only inside a vng guest you booted, with the guest's `uname -r` quoted.
10. Runs the owner has deferred (today: anything longer than 2 minutes, until every feature and bug
    has landed) are listed as **OWED**. An OWED run is not a fail and not a pass. Never shorten a
    run, cut its count or loosen its bound to fit the limit and then call it a pass.

## 4. Prove the test catches the bug

11. Show the regression is real. Plant the bug the card fixes (or revert the fix, or run at BASE),
    show the test going red **at its own check line, for the right reason**, then restore the fix
    and show green. Say which kind of proof it was. A crash, a hang or an unrelated failure proves
    nothing. Confirm the plant is gone from the final tree.
12. If you call a red "pre-existing", reproduce it at BASE on the same machine and case, give both
    heads and the raw log, and name the card that owns it. Without that, report it as a red.

## 5. Check before you finish

13. Never chase the base tip. Merge BASE once (step 7), gate at that merged head, and finish, naming the SHA you gated at. The lander re-merges and its CI tests the real tip. Re-merge only if the lander refuses on a conflict.
14. Check every claim in the report against the raw receipts: counts, transports, address
    families, case names, skips. Anything you did not run yourself is labelled as reported by the
    worker or the owner, never as verified.

## 6. Report and finish

15. Report format:
    - line 1: `Verdict: LAND` or `Verdict: HOLD`
    - line 2: `Head: <full 40-hex SHA>`
    - then: branch, base, carried head, PATHS-PROPOSED, what was wrong and what changed, the green
      and red evidence (verbatim tails), the receipts path, OWED runs, and anything not done.
16. **LAND** only when every gate the card still requires is green at the head you finish, and the
    regression proof stands. **HOLD** (`--failed`) when a gate is red or missing, a defect is
    unresolved, scope is unapproved, or evidence is unknown. Name the reason on the first lines.
17. Push your own branch and check that the remote SHA matches. Then
    `nova-sprint finish --as <you> <job>@<gen> --epoch <n> --branch <b> --head <sha> --report ...`.
    Read the reply: a REFUSED or stale finish is not a finish. A finish is not a landing.
18. Keep the receipts. Clean your scratch directory only after the evidence is saved, and only by
    an absolute path under your own card-named scratch directory.

## Before you claim LAND: the self-check

Mandatory before any finish that claims LAND or done. It turns steps 3 to 17 into commands. Run
every item in the job directory, at the head you will finish with, and put each item's evidence
line in the report. Where the card's BASE is not `main`, read it for `main` below. The coordinator
sends this section to a row whose LANDs keep coming back broken from its readers
([the coordinator's playbook](docs/COORDINATOR-PLAYBOOK.md), "Worker quality: first-time LAND
rate").

1. **Merge, never rebase.** `git fetch origin main && git merge origin/main`. If main moved, run
   the gate again at the merged head.
2. **PATHS.** `git diff --name-only $(git merge-base origin/main HEAD) HEAD` lists only files on
   the brief's PATHS and NEW lines. Never commit a report file: no RESULT.md, REPORT.md, JOB.md,
   PROPOSED.patch or any other report. A file you truly need outside PATHS goes on a
   `PATHS-PROPOSED: <file>` line in the report, and the finish is HOLD.
3. **Ledger keys after the merge.** `tla/RUNS.tsv`, `tla/CASES.tsv` and `tla/COVERAGE.tsv` each
   name every key exactly once after merging main: `cut -f1 tla/RUNS.tsv | sort | uniq -d` prints
   nothing, and the same for the other two. A merge that keeps both sides of a row leaves a
   duplicate key.
4. **Banned words.** `git diff $(git merge-base origin/main HEAD) HEAD | grep -E '^\+' | grep -iwE 'peer|snapshot'`
   prints nothing. Never disguise a banned word to get past the check: no character arrays, string
   splitting, concatenation, escapes or spelling games. Rename the thing.
5. **No names.** No person's name, card id, fleet host name or internal note in any committed
   file, file name, title or cite line:
   `git diff $(git merge-base origin/main HEAD) HEAD | grep -E '^\+' | grep -iE '<the fleet's host names and people's names, joined by |>'`
   prints nothing you added. Citing a path that already exists on main is not adding a name; a new
   file name, title or prose line that carries one is.
6. **DONE-WHEN.** Copy every DONE-WHEN item into the report, each with the command you ran and its
   output line as evidence. For a test that must fail at BASE, show it failing at BASE and passing
   at the head. An item you did not meet, or could not run, is unmet.
7. **Bench load.** Build and test load on a shared bench goes through the fleet's slot lock
   (`~/bin/benchslot -- <cmd>`), never straight onto the bench and never onto whichever bench
   looks least loaded, because every worker picks the same one. A timing or functional red taken
   while the bench ran above its core count is not evidence until it is run again under the slot
   lock.
8. **Verdict.** LAND only when items 1 to 7 all hold. Anything unmet, anything not run, and an
   empty head (no change against main) is HOLD, and a not-done or HOLD report always finishes with
   `--failed`: `nova-sprint finish --as <you> <job>@<gen> --epoch <n> --failed --report ...`, or
   `Verdict: HOLD` on the first line of a friend's REPORT.md. A plain finish on an unmet head is
   the failure that costs most: a reader finds it, the attempt is spent, and it counts against
   your row's ok%.
