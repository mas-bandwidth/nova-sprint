RESULT: deal-tells-on-commit-not-on-friend-sync sha=<sha12> tier: frontier
REPO: mas-bandwidth/nova-sprint
BASE: v1.3/comms
STOP: a card dealt to a friend row (the coordinator seat's own included) is delivered to its inbox and told on the bus within one log event of the tick that commits the deal, not at the next friend sync --every pass; the seat's own row is told as kind=status; --every stays as the quiet-log backstop; TLC and the tests pin it.
PATHS: cmd/nova-sprint/friend_loop.go, cmd/nova-sprint/friendcards.go, cmd/nova-sprint/friends.go, cmd/nova-sprint/friendcards_wake_test.go, tla/CASES.tsv, tla/RUNS.tsv, tla/RUNS-HISTORY.tsv, docs/FRIENDS.md, docs/CLI.md, docs/SPEC-SPRINT.md
NEW: cmd/nova-sprint/friend_sync_wake_test.go, tla/FriendSyncWake.tla, tla/MCFriendSyncWake.tla, tla/MCFriendSyncWake.cfg, tla/MCFriendSyncWakeBrokenClockOnly.cfg, tla/MCFriendSyncWakeBrokenTellLost.cfg
SHARED: tla/CASES.tsv, tla/RUNS.tsv, tla/RUNS-HISTORY.tsv, docs/FRIENDS.md, docs/CLI.md, docs/SPEC-SPRINT.md
TEST: cmd/nova-sprint TestDealIsToldOnCommitNotOnFriendSync
DEPENDS-ON: friend-status-transitions-push-to-the-coordinator
SOURCE: Glenn 13:57Z 2026-10-15: "Event driven push notifications are vastly superior to polling. What else is like this? A poll where it should be a push notification to you, across nova tools and nova sprint?"; the push audit of that hour (nova-tools abd4a6d, nova-sprint 3e51974), item 4 (and the friend-sync hop of 6).
PRIORITY: blocker
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 75 minutes; report what you have with the verdict not-done rather than push past it.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.
Unit tests use no real time, no sockets, no network and no outside services: time is an argument (a fake clock passed in, never sleep), the store and the bus are faked (internal/sprint/store's mem twin, pkg/bus/bustest).
A refusal or a check is never routed around, and no existing check is weakened: the change adds a check or moves one earlier, it never turns one off.

THE TASK. The tick deals cards to friend rows (internal/sprint/friend_deal.go (read) friendDealPass, line ~600) and commits the deal to the sprint log, but the cards reach the friend's inbox directory and the bus only on the next friend sync pass, which runs on a clock: friend sync --every (15s in docs, cmd/nova-sprint/friend_loop.go friendSyncLoop, line ~49, waiting on after(every)). So every friend card waits up to 15s for its delivery, and the coordinator seat, which also has a friend row, polls nova-sprint queue every 30s to see its own. The tell itself (friendcards.go wakeFriend line ~666, wakeFriendPass line ~704) is sent From: st.Actor, the seat, so for the seat's own row From == To and the hook drops it (fixed by friend-status-transitions-push-to-the-coordinator, which this card needs). Line numbers are at the v1.3/comms tip.
The change:
(1) friendSyncLoop waits on the sprint log (Store.WaitLog, internal/sprint/store/waitlog.go (read)) from the last line it has seen, bounded by --every, and runs its pass when a line arrives; the --every wait stays as the quiet-log backstop sweep, and a pass with nothing new still says nothing.
(2) The one-shot tell (wakeFriend) carries kind status, like the batch tell, so the seat's own row is told as a machine status line.
(3) Delivery stays idempotent: a pass woken twice for one deal delivers and tells once (the inbox file and the told note are the record, as today).
(4) docs/FRIENDS.md, the friend sync loop section, says the loop wakes on the log and --every is the backstop.
THE MODEL. A state machine changes here, so its TLA+ comes first (STEP 2), with the liveness property that every change is eventually pushed and a broken witness that fails it.
BACKSTOP. The slow timer or sweep named above stays: a lost push must still be caught. Do not remove it or lengthen it past its value on the base.
Libraries considered: none: WaitLog is the run loop's own wake; the inbox write and the tells exist.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Write the TLA+ first. Write tla/FriendSyncWake.tla: variables are the log (deals committed), the loop's cursor, each friend's inbox (delivered cards) and told set, and the clock; actions are Deal (a tick commits a deal), Wake (a log line wakes the loop), Sweep (the --every backstop), Pass, Tell and a lost Tell. Liveness EveryDealIsPushed: every dealt card is eventually delivered and told to its friend; invariant PushedWithinOneEvent: no Sweep fires while a dealt card is undelivered and Wake is enabled; under weak fairness of Wake, Pass and Tell. Reversed witnesses: tla/MCFriendSyncWakeBrokenClockOnly.cfg removes Wake (the loop runs only on --every) and must violate PushedWithinOneEvent; tla/MCFriendSyncWakeBrokenTellLost.cfg drops a tell with no Sweep and must violate EveryDealIsPushed. Run the changed groups on a Linux TLC bench (make tlc TLC_GROUP=<group>), then tlacheck merge --keep tla/RUNS.tsv as tla/README.md says under "Refreshing the records after a model edit"; tlacheck groups --stale is then empty. Check each counterexample against the code by hand before writing it down.
STEP 3. Red test first: TestDealIsToldOnCommitNotOnFriendSync in cmd/nova-sprint/friend_sync_wake_test.go, opening with t.Parallel(), over the mem store and the faked bus with a fake clock and --every 15s: a tick deals one card to friend ada and one to the seat's own row; assert both inbox files are written and both bus messages (the seat's with kind status) are sent after one log wake, the fake clock not advanced. It proves the push arrives within one event: after the event's commit, the receiver sees it after one step of the fake transport, with the fake clock never advanced, so no poll cadence is needed to see it. A second case drops the push on the fake transport and advances the fake clock past the backstop interval: the backstop sweep catches it, once. Run go test -count=1 -timeout 600s ./cmd/nova-sprint/ -run TestDealIsToldOnCommitNotOnFriendSync and keep the failing line.
STEP 4. Make it pass in the PATHS and NEW files only. Cite the TLA+ module from each changed rule.
STEP 5. Gate: go test -count=1 -timeout 600s ./cmd/nova-sprint/ ./internal/sprint/... ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it (run it on the unchanged base to say so).
STEP 6. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, each test and what it pins, the TLC counts per config, and what was not done.
STEP 7. End as JOB.md says.
