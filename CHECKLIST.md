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

13. Fetch BASE again. If it moved, read what changed, merge, and re-run every gate whose inputs
    changed. A gate whose inputs did not change may stand on source equivalence. Then the report
    lists the SHA it ran at, the final SHA, the files that changed between them, and why the
    gate's inputs are the same.
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
