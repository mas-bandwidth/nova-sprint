RESULT: friend-daemon-tells-state-transitions-on-the-bus sha=<sha12> tier: frontier
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
STOP: the friend daemon tells the coordinator seat each working-state transition of its friend (busy, working, idle, idle with cards) as its own kind=status bus message within one daemon beat of the change, in the FRIEND STATE line shape friend-status-transitions-push-to-the-coordinator sets for hook states; status.json stays as the pulled record and the backstop; TLC and the tests pin it.
PATHS: pkg/friend/daemon.go, pkg/friend/state.go, pkg/friend/daemon_test.go, pkg/friend/state_test.go, docs/FRIENDS.md, tla/CASES.tsv, tla/RUNS.tsv
NEW: pkg/friend/state_tell_test.go, tla/FriendStateTell.tla, tla/MCFriendStateTell.tla, tla/MCFriendStateTell.cfg, tla/MCFriendStateTellBrokenPullOnly.cfg, tla/MCFriendStateTellBrokenCollapsed.cfg
SHARED: docs/FRIENDS.md, tla/CASES.tsv, tla/RUNS.tsv
TEST: pkg/friend TestFriendDaemonTellsStateTransitions
DEPENDS-ON: friend-status-transitions-push-to-the-coordinator
SOURCE: Glenn 13:57Z 2026-10-15: "Event driven push notifications are vastly superior to polling. What else is like this? A poll where it should be a push notification to you, across nova tools and nova sprint?"; the push audit of that hour (nova-tools abd4a6d, nova-sprint 3e51974), item 5.
PRIORITY: high
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 90 minutes; report what you have with the verdict not-done rather than push past it.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.
Unit tests use no real time, no sockets, no network and no outside services: time is an argument (a fake clock passed in, never sleep), the store and the bus are faked (hook/hooktest, pkg/bus/bustest).
A refusal or a check is never routed around, and no existing check is weakened: the change adds a check or moves one earlier, it never turns one off.

THE TASK. The coordinator runs nova-hook status every 60s to see whether a friend is busy, working or idle. Those states live only in the friend daemon's status.json, rewritten every StatusEvery = 5s (pkg/friend/state.go line ~33, pkg/friend/daemon.go line ~1065) and read by pull. Up and down (the hook states) are told by nova-hook serve and are the subject of friend-status-transitions-push-to-the-coordinator, which this card needs and does not duplicate: that card covers the heartbeat server's hook states; this one covers the daemon's working states, which serve never sees. Line numbers are at the v1.3/comms tip.
The change:
(1) The daemon keeps the working state it last told (in memory and in status.json, so a restart does not re-tell an old state as new).
(2) On each beat that writes status.json, when the state differs from the last told one, it sends one bus message to the coordinator seat over the bus client it already holds: kind status, subject `FRIEND STATE <name> <old>-><new> at=<RFC3339 store TIME>` (the shape the hook-state card sets; idle with cards waiting is its own state, idle+cards), and marks it told after the send succeeds; a failed send is retried next beat; a state that flips and flips back between beats is two lines, never collapsed.
(3) status.json and nova-hook status stay as they are, the pulled record and the backstop for a lost push.
(4) docs/FRIENDS.md says the daemon tells transitions.
THE MODEL. A state machine changes here, so its TLA+ comes first (STEP 2), with the liveness property that every change is eventually pushed and a broken witness that fails it.
BACKSTOP. The slow timer or sweep named above stays: a lost push must still be caught. Do not remove it or lengthen it past its value on the base.
Libraries considered: none: the daemon's bus client and its status write exist.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Write the TLA+ first. Write tla/FriendStateTell.tla: variables are the friend's working state, the last told state, the bus to the seat and the status file; actions are Change (busy, working, idle, idle+cards), Beat (write status.json and tell a differing state), Send, SendLost and Pull (the seat reads status.json). Liveness EveryTransitionIsPushed: every change of the working state is eventually shown to the seat as its own line; invariant PushedWithinOneEvent: no second Beat passes with a changed state untold unless its send was lost. Reversed witnesses: tla/MCFriendStateTellBrokenPullOnly.cfg removes the tell (the seat learns only by Pull) and must violate EveryTransitionIsPushed; tla/MCFriendStateTellBrokenCollapsed.cfg tells only the latest state of a beat and must violate it on idle->working->idle. Run the changed groups on a Linux TLC bench (make tlc TLC_GROUP=<group>), then tlacheck merge --keep tla/RUNS.tsv as tla/README.md says under "Refreshing the records after a model edit"; tlacheck groups --stale is then empty. Check each counterexample against the code by hand before writing it down.
STEP 3. Red test first: TestFriendDaemonTellsStateTransitions in pkg/friend/state_tell_test.go, opening with t.Parallel(), over the faked bus with a fake clock passed to the daemon's beat: the friend goes idle -> working -> idle+cards; assert one FRIEND STATE line per transition reaches the seat at the beat that saw it, none folded, and a send failed once is sent at the next beat and never twice. It proves the push arrives within one event: after the event's commit, the receiver sees it after one step of the fake transport, with the fake clock never advanced, so no poll cadence is needed to see it. A second case drops the push on the fake transport and advances the fake clock past the backstop interval: the backstop sweep catches it, once. Run go test -count=1 -timeout 600s ./pkg/friend/ -run TestFriendDaemonTellsStateTransitions and keep the failing line.
STEP 4. Make it pass in the PATHS and NEW files only. Cite the TLA+ module from each changed rule.
STEP 5. Gate: go test -count=1 -timeout 600s ./pkg/friend/ ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it (run it on the unchanged base to say so).
STEP 6. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, each test and what it pins, the TLC counts per config, and what was not done.
STEP 7. End as JOB.md says.
