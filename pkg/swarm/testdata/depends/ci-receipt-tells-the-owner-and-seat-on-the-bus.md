RESULT: ci-receipt-tells-the-owner-and-seat-on-the-bus sha=<sha12> tier: frontier
REPO: mas-bandwidth/nova-sprint
BASE: v1.3/comms
STOP: every ci receipt row on ev:github (workflow_run, as nova-tools internal/cireceipt writes it) reaches the coordinator seat and the owner of the card whose head it tested as one bus message (repo, sha, PR, conclusion, run id) within one stream event, read by XREAD BLOCK with a cursor advanced only after the send; a slow sweep re-reads past the cursor so a lost send is caught; TLC and the tests pin it.
PATHS: cmd/nova-sprint/serve.go, tla/CASES.tsv, tla/RUNS.tsv, tla/RUNS-HISTORY.tsv, docs/CLI.md, docs/SPEC-SPRINT.md
NEW: cmd/nova-sprint/citell.go, cmd/nova-sprint/citell_test.go, tla/CIReceiptTell.tla, tla/MCCIReceiptTell.tla, tla/MCCIReceiptTell.cfg, tla/MCCIReceiptTellBrokenCursorFirst.cfg
SHARED: cmd/nova-sprint/serve.go, tla/CASES.tsv, tla/RUNS.tsv, tla/RUNS-HISTORY.tsv, docs/CLI.md, docs/SPEC-SPRINT.md
TEST: cmd/nova-sprint TestCIReceiptTellsTheOwnerAndSeat
DEPENDS-ON: friend-status-transitions-push-to-the-coordinator
SOURCE: Glenn 13:57Z 2026-10-15: "Event driven push notifications are vastly superior to polling. What else is like this? A poll where it should be a push notification to you, across nova tools and nova sprint?"; the push audit of that hour (nova-tools abd4a6d, nova-sprint 3e51974), item 3.
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
Unit tests use no real time, no sockets, no network and no outside services: time is an argument (a fake clock passed in, never sleep), the store and the bus are faked (internal/sprint/store's mem twin, pkg/bus/bustest).
A refusal or a check is never routed around, and no existing check is weakened: the change adds a check or moves one earlier, it never turns one off.

THE TASK. When a CI run finishes, nova-tools nova-ci github receipt writes one ev:github row of the workflow_run shape (repo, number, head, run id, workflow, status, conclusion, sender runner) to the store's Redis (nova-tools internal/cireceipt/cireceipt.go and internal/ghevent/event.go; the stream name ev:github is nova-tools internal/ghevent/wire/stream.go). Nobody consumes those rows for people: the coordinator runs gh run list by hand and the card's owner learns nothing until a read or a land. The event already exists; this card adds its consumer to the sprint server, which already holds the store's Redis login and a bus client.
The change: a new server loop in cmd/nova-sprint/citell.go, started by serve.go beside coordPushLoop: (1) XREAD BLOCK on ev:github from a cursor kept on the server row (the stream key is a constant pinned by a test to the value "ev:github", since nova-tools' internal package cannot be imported); (2) for each workflow_run row with status completed, find the card whose recorded head (finish --head) is the row's head sha, else whose branch's pull request is the row's number; (3) send one bus message, kind status, subject `CI <conclusion> <repo>@<sha12> pr=<n> run=<id> card=<card or ->`, to the seat and to that card's owner (the friend or member name, friend.<name> read as <name>), under a once-key of repo, sha and run id; (4) advance the cursor only after the send succeeded, so a failed send is retried; (5) a sweep every 10 minutes re-reads ev:github after the last told id with XRANGE and sends what was missed: the backstop for a lost push; docs/SPEC-SPRINT.md says what the server tells on a receipt. Rows of other kinds are passed over. The seat sees the line once friend-status-transitions-push-to-the-coordinator lands, which this card needs.
THE MODEL. A state machine changes here, so its TLA+ comes first (STEP 2), with the liveness property that every change is eventually pushed and a broken witness that fails it.
BACKSTOP. The slow timer or sweep named above stays: a lost push must still be caught. Do not remove it or lengthen it past its value on the base.
Libraries considered: the server's go-redis client (XREAD BLOCK, XRANGE) and its existing bus send; no GitHub API and no webhook, by design.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Write the TLA+ first. Write tla/CIReceiptTell.tla: variables are the ev:github rows, the cursor, the told set, the in-flight send and the clock; actions are Receipt (a row appended), Read (XREAD BLOCK returns a row), Send, SendLost, AdvanceCursor and Sweep. Liveness EveryReceiptIsPushed: every completed receipt row is eventually told to the seat and the owner; invariant NeverToldTwice (the once-key) and CursorNeverPassesUntold; under weak fairness of Read, Send and Sweep. Reversed witness: tla/MCCIReceiptTellBrokenCursorFirst.cfg advances the cursor before the send and disables Sweep, and must violate EveryReceiptIsPushed. Run the changed groups on a Linux TLC bench (make tlc TLC_GROUP=<group>), then tlacheck merge --keep tla/RUNS.tsv as tla/README.md says under "Refreshing the records after a model edit"; tlacheck groups --stale is then empty. Check each counterexample against the code by hand before writing it down.
STEP 3. Red test first: TestCIReceiptTellsTheOwnerAndSeat in cmd/nova-sprint/citell_test.go, opening with t.Parallel(), over the mem store, a faked stream reader and the faked bus with a fake clock: a card finished with head H owned by friend ada; one workflow_run row for H with conclusion failure; assert one CI line reaches the seat and ada after one stream read, the clock not advanced, and a retried read sends nothing twice. It proves the push arrives within one event: after the event's commit, the receiver sees it after one step of the fake transport, with the fake clock never advanced, so no poll cadence is needed to see it. A second case drops the push on the fake transport and advances the fake clock past the backstop interval: the backstop sweep catches it, once. Run go test -count=1 -timeout 600s ./cmd/nova-sprint/ -run TestCIReceiptTellsTheOwnerAndSeat and keep the failing line.
STEP 4. Make it pass in the PATHS and NEW files only. Cite the TLA+ module from each changed rule.
STEP 5. Gate: go test -count=1 -timeout 600s ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it (run it on the unchanged base to say so).
STEP 6. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, each test and what it pins, the TLC counts per config, and what was not done.
STEP 7. End as JOB.md says.
