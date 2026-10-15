RESULT: stream-stop-and-resume-push-a-blocker-to-the-seat sha=<sha12> tier: frontier
REPO: mas-bandwidth/nova-sprint
BASE: v1.3/comms
STOP: a stream stop (landing conflict, refusal, red batch or red base) and a stream resume each reach the coordinator seat as their own kind=blocker bus message within one log event of the unit that commits them, not on the coordinator push's 5s look and not as a folded judgment; the friends with cards queued on the stream get a kind=status line; the 5s look stays as the quiet-log backstop; TLC and the tests pin it.
PATHS: cmd/nova-sprint/coordinator_push.go, cmd/nova-sprint/coordinator_push_test.go, internal/sprint/steps_merge.go, internal/sprint/stops.go, internal/sprint/notes.go, tla/CoordinatorPush.tla, tla/MCCoordinatorPush.tla, tla/MCCoordinatorPushLive.cfg, tla/CASES.tsv, tla/RUNS.tsv, tla/RUNS-HISTORY.tsv, docs/SPRINT-COORDINATOR.md, docs/CLI.md, docs/SPEC-SPRINT.md
NEW: cmd/nova-sprint/stream_stop_push_test.go, tla/MCCoordinatorPushBrokenLookOnly.cfg, tla/MCCoordinatorPushBrokenResumeSilent.cfg
SHARED: tla/CASES.tsv, tla/RUNS.tsv, tla/RUNS-HISTORY.tsv, docs/SPRINT-COORDINATOR.md, docs/CLI.md, docs/SPEC-SPRINT.md
TEST: cmd/nova-sprint TestStreamStopAndResumePushABlockerToTheSeat
DEPENDS-ON: friend-status-transitions-push-to-the-coordinator
SOURCE: Glenn 13:57Z 2026-10-15: "Event driven push notifications are vastly superior to polling. What else is like this? A poll where it should be a push notification to you, across nova tools and nova sprint?"; the push audit of that hour (nova-tools abd4a6d, nova-sprint 3e51974), item 1.
PRIORITY: blocker
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 90 minutes; report what you have with the verdict not-done rather than push past it.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.
Unit tests use no real time, no sockets, no network and no outside services: time is an argument (a fake clock passed in, never sleep), the store and the bus are faked (internal/sprint/store's mem twin, pkg/bus/bustest).
A refusal or a check is never routed around, and no existing check is weakened: the change adds a check or moves one earlier, it never turns one off.

THE TASK. The space stream sat stopped for 57 minutes on 2026-10-15 and the coordinator did not see it. A stop is committed in one unit with its stream-level judgment (internal/sprint/steps_merge.go: the stop helper near line ~519 for a conflict or a refusal, baseRedStep near line ~160 for a red base), and the judgment reaches the seat only through the coordinator push, a look every coordPushLook = 5s (cmd/nova-sprint/coordinator_push.go coordPushLoop, line ~83), as a judgment that nova-sprint watch --wake rate-limits to 20 minutes. Resume (steps_merge.go Resume, line ~745) writes only a Moved log line, plus a happened note when the stream landed: nobody is told the stream runs again. Line numbers are at the v1.3/comms tip; re-find them on your base.
The change:
(1) coordPushLoop blocks on the sprint log (Store.WaitLog, internal/sprint/store/waitlog.go (read), the XREAD BLOCK the run loop already uses) and runs its pass on every new line; coordPushLook stays, as the quiet-log backstop and the bound of each wait.
(2) Resume adds a happened note, NStreamResumed (a new note type, internal/sprint/notes.go), naming the stream, its new state, who resumed it and --did.
(3) The pass sends each stream stop and each resume as its own bus message to the seat: kind blocker, subject `STREAM STOPPED <stream> cause=<cause> waiting=<n> at=<RFC3339 store TIME>` or `STREAM RESUMED <stream> -> <state> at=<...>`, under a once-key of stream and since (the told marks coordPush already keeps), never joined with another stream or folded into the judgment group message. The owners of the cards queued or stuck on that stream get one kind=status line each with the same subject.
(4) The stop judgment itself stays as it is (still answered by the coordinator); the blocker line is the notice, not the decision.
The seat sees these lines once the hook stops dropping self-sent machine messages (card friend-status-transitions-push-to-the-coordinator, which this card needs).
THE MODEL. A state machine changes here, so its TLA+ comes first (STEP 2), with the liveness property that every change is eventually pushed and a broken witness that fails it.
BACKSTOP. The slow timer or sweep named above stays: a lost push must still be caught. Do not remove it or lengthen it past its value on the base.
Libraries considered: none: WaitLog, the told marks and the once-key send already exist.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Write the TLA+ first. Extend tla/CoordinatorPush.tla with stream stop and resume events committed to the log, a per-stream told mark, and the blocker channel to the seat. Add the liveness property EveryStopAndResumeIsPushed: every stop and every resume committed is eventually delivered to the seat as its own line (a stop then a resume in one interval is two lines), under the existing fairness; and the invariant PushedWithinOneEvent: no Look-timer action fires while a committed stop or resume is unpushed and the log wake is enabled. Keep every existing invariant and property. Reversed witnesses: tla/MCCoordinatorPushBrokenLookOnly.cfg sends only on the Look timer and must violate PushedWithinOneEvent; tla/MCCoordinatorPushBrokenResumeSilent.cfg never tells a resume and must violate EveryStopAndResumeIsPushed. Run the changed groups on a Linux TLC bench (make tlc TLC_GROUP=<group>), then tlacheck merge --keep tla/RUNS.tsv as tla/README.md says under "Refreshing the records after a model edit"; tlacheck groups --stale is then empty. Check each counterexample against the code by hand before writing it down.
STEP 3. Red test first: TestStreamStopAndResumePushABlockerToTheSeat in cmd/nova-sprint/stream_stop_push_test.go, opening with t.Parallel(), over the mem store and the faked bus with a fake clock: a landing refusal stops stream s with two cards queued, then resume --did; assert the seat gets STREAM STOPPED then STREAM RESUMED as two kind=blocker messages, each card owner one status line, and a retried pass sends nothing twice. It proves the push arrives within one event: after the event's commit, the receiver sees it after one step of the fake transport, with the fake clock never advanced, so no poll cadence is needed to see it. A second case drops the push on the fake transport and advances the fake clock past the backstop interval: the backstop sweep catches it, once. Run go test -count=1 -timeout 600s ./cmd/nova-sprint/ -run TestStreamStopAndResumePushABlockerToTheSeat and keep the failing line.
STEP 4. Make it pass in the PATHS and NEW files only. Cite the TLA+ module from each changed rule.
STEP 5. Gate: go test -count=1 -timeout 600s ./cmd/nova-sprint/ ./internal/sprint/... ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it (run it on the unchanged base to say so).
STEP 6. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, each test and what it pins, the TLC counts per config, and what was not done.
STEP 7. End as JOB.md says.
