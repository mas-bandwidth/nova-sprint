; nova-sprint roadmap: work planned for the release after the current one.
; Moved out of the nova-sprint sprint (epoch 15) on 2026-10-04 by the coordinator, on Glenn Fiedler's word:
; "for the later release work, I'd like you to remove this from this sprint, making sure it is stored
;  somewhere in a sexp data structure". Each card keeps its whole brief, so it can be added back as it is:
;  nova-sprint add --stream <stream> --brief-file <the card's :brief written to <id>.md>.
; ROADMAP.md is generated from this file.
(:roadmap 1 :repo "mas-bandwidth/nova-sprint" :product "nova-sprint" :generated "2026-10-04"
 :source "nova-sprint store, epoch 15, cards waiting when the release was cut"
 :releases
 ((:release "v1.1.0" :status :planned :cards 128
   :streams
   ((:stream "sprint-v1-verbs" :cards
     ((:id "land-verify-landed-ancestry" :tier "-" :needs ("land-clone-self-heals" "seat-key-follows-record" "read-asked-again-after-takeback")
      :title "Two cards are recorded landed but are not on their branch: harness-pkg-delayproxy-tools-tlacheck-t and fix-general-tla-tableorder-tla"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: seat-key-follows-record,read-asked-again-after-takeback
PATHS: cmd/nova-sprint/landverify*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/coordinator.go,internal/sprint/steps_review.go,internal/sprint/store/steps.go,tla/Land.tla,tla/RUNS.tsv,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestReopenMovesAFalseLandedCardBackToMerging
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Two cards are recorded landed but are not on their branch: harness-pkg-delayproxy-tools-tlacheck-t and fix-general-tla-tableorder-tla. The store refuses every change to them (landed is final), so the coordinator has no way to put the work back. Add a new verb verify-landed [--stream <s>] (read class, in its own file cmd/nova-sprint/landverify.go; land.go is not changed): for each landed card it checks that the recorded merge (else the head) is an ancestor of origin/<base> at its tip, by git and no model, and prints one LANDED-MISSING line per card that is not, exit 1. Add reopen <card>... --reason <text> (the coordinator): only for a card verify-landed proves missing, it moves the card back to its stream merge queue at its recorded head, a new generation, logged reopened: the landed record is not on <base>; a card found on the branch is refused, and landed stays final for every other path. Extend tla/Land.tla with the reopen action guarded by not-on-branch, keep its invariants, run the changed TLC groups on a Linux bench and merge the records into tla/RUNS.tsv with tlacheck merge --keep. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/landverify*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/coordinator.go,internal/sprint/steps_review.go,internal/sprint/store/steps.go,tla/Land.tla,tla/RUNS.tsv,docs/SPEC-SPRINT.md
  COMMIT: land-verify-landed-ancestry: verify-landed finds landed records missing from the branch; reopen puts them back
  VERDICT: with a temporary origin, a card recorded landed whose merge is not on the base is listed by verify-landed and reopened into the merge queue; a card on the branch is refused.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestReopenMovesAFalseLandedCardBackToMerging' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestReopenMovesAFalseLandedCardBackToMerging` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friend-deal-most-roomb" :tier "-" :needs ("sn-overload-alarm-friendsb" "fix-late-rule-progress-stampb")
      :title "The deal tops up a friend who is already full while another friend of her class is idle"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: sn-overload-alarm-friendsb,fix-late-rule-progress-stampb
PATHS: internal/sprint/friend_deal.go,internal/sprint/friend_level.go,internal/sprint/steps_tick.go,internal/sprint/friend_deal_room_test.go,docs/SPEC-SPRINT.md,internal/sprint/store/tick.go,internal/sprint/store/friends.go,internal/sprint/store/*_test.go
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestFriendDealPrefersTheFriendWithTheMostRoom
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. The deal tops up a friend who is already full while another friend of her class is idle. Observed 3:57 PM: the coordinator took 8 unstarted cards back from Alex, Zhi and Johnny (friend take) so they would go to idle Emma (5 working of 8) and Stella (1 of 16); the next tick dealt them back and refilled Alex to ready 8 beside working 8. Change the friend deal (internal/sprint/friend_deal.go): a card for any friend (WHO: friend) goes to the up friend of its class with the most free room, room = 2 x width - working - ready, ties broken by the order the deal uses today; a friend whose working + ready is at or over 2 x width is never dealt while a same-class friend has room; a card taken back from a friend never returns to her (as now). Run friend level inside every tick after the deal so a backlog evens itself without the coordinator. Cite docs/SPEC-SPRINT.md from the function; one paragraph in its friend-deal section. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/friend_deal.go,internal/sprint/friend_level.go,internal/sprint/steps_tick.go,internal/sprint/friend_deal_room_test.go,docs/SPEC-SPRINT.md,internal/sprint/store/tick.go,internal/sprint/store/friends.go,internal/sprint/store/*_test.go
  COMMIT: friend-deal-most-room: friend cards go to the friend with the most room, and friend level runs every tick
  VERDICT: on the twin store three friends of one class, one at working 8 ready 8 width 8 and two idle, five new friend cards all go to the idle two by room, none to the full one; a tick evens a backlog without a verb.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFriendDealPrefersTheFriendWithTheMostRoom' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFriendDealPrefersTheFriendWithTheMostRoom` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

RE-CUT NOTE (coordinator decisions, 4:28 PM). Start from the pushed branch sprint/friend-deal-most-room.w1.g1.e15 at head 08e26209f24bc06ceef68585a06b28da86d00ecd. (1) The levelling runs from the tick: internal/sprint/store/tick.go (now in PATHS). (2) store/friends.go friendSeats reads friend rows whenever the roster has a friend; that fix comes from sn-overload-alarm-friendsb, which this card now depends on, so build on it. (3) \"Started\" is store data, never a git ls-remote in the tick: a card is started when the friend's beat lists it as running, or when it has a progress stamp (fix-late-rule-progress-stampb, also a dependency). Store tests go in internal/sprint/store/*_test.go. Finish with the full 40-hex head.")
      (:id "land-record-unreported-push" :tier "-" :needs ("land-verify-landed-ancestry" "accept-heavy-verdict")
      :title "The reverse of a false landed record: work that is on the branch but not recorded landed"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: accept-heavy-verdict,land-verify-landed-ancestry
PATHS: cmd/nova-sprint/landverify.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/coordinator.go,internal/sprint/steps_review.go,internal/sprint/store/steps.go,cmd/nova-sprint/landed_sha_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestLandedRecordsAPushFoundOnTheBranch
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. The reverse of a false landed record: work that is on the branch but not recorded landed. Two causes today: a push that happened and was never reported (a pass cut short after the push), and work landed outside the deal by a pull request (dash-tier-costs-now, done by PR 5323, which the coordinator could only drop). verify-landed (the verb its dependency adds) also lists LANDED-UNRECORDED for each card in review, merging or dropped whose head is an ancestor of origin/<base> at its tip. Add landed <card>... --sha <commit> --reason <text> (the coordinator): it records the card landed at that commit only when the card head is an ancestor of it and the commit is on origin/<base>, by git and no model; otherwise refused naming what git found. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/landverify.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/coordinator.go,internal/sprint/steps_review.go,internal/sprint/store/steps.go,cmd/nova-sprint/landed_sha_test.go,docs/SPEC-SPRINT.md
  COMMIT: land-record-unreported-push: landed records work found on the branch; verify-landed lists it
  VERDICT: with a temporary origin, a merging card whose head is already on the base is listed and recorded landed by landed --sha; a head not on the base is refused.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestLandedRecordsAPushFoundOnTheBranch' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestLandedRecordsAPushFoundOnTheBranch` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friends-okpct-verified" :tier "-" :needs ("verb-seat-install-pushb" "friend-session-liveness")
      :title "A friend card counts as ok only once a reader accepted it or it landed"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
DEPENDS-ON: verb-seat-install-pushb,friend-session-liveness
PATHS: internal/sprint/**,cmd/nova-sprint/**,docs/SPEC-SPRINT.md,TABLES.lock
TEST: internal/sprint TestAFriendCardCountsOkOnlyOnceReadOrLanded
DEADLINE: finish within 180 minutes
You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/<your name>-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/<your name>-working/jobs/<job>/`. Where this card says JOB.md, read BRIEF.md.
Libraries considered: Go standard library and testify, already in tree; no new dependency.
ATTRIBUTION: commits and reports name the friend (By: Rowan) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. A friend card counts as ok only once a reader accepted it or it landed. Today a card a friend reports finished (Verdict: LAND) counts as ok at once, which flatters the friends table's ok%. Change the meaning: a friend card that is only reported finished counts toward neither ok nor failed until it is read; ok is read-accepted or landed, failed is read-broken, HOLD/FAIL, or dropped after work. Change the meaning in internal/sprint/schema.go (and whatever computes the friends row's ok, failed and ok%), amend TABLES.lock citing Glenn's 2026-10-04 rule (\"Friend work is its own cost category\") plus this rule, state it in docs/SPEC-SPRINT.md, and add the test. This builds on verb-seat-install-pushb.

STEP 1. Set `JOB=~/<your name>-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of BASE; export GOCACHE=~/<your name>-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read docs/STANDARD.md and the spec sections the task names before the first edit.
STEP 2. Red first: write the test the TEST line names (t.Parallel(), no real time, no sockets: an injected clock and an in-memory store), see it fail, then make the change. Where the change touches a table's shape or meaning, amend TABLES.lock in the same commit with the reason and the rule it cites.
STEP 3. Gate: `gofmt -l` on every changed Go file prints nothing; `nice -n 19 go vet` and `nice -n 19 go test -p 2 -count=1 -timeout 600s` on every package you changed plus ./internal/ci/ and ./internal/docs/; keep the last line of each. A ledger under internal/ci/testdata that the gate names only shrinks.
STEP 4. END. Commit only PATHS; push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`, never force; verify `git ls-remote` equals `git rev-parse HEAD`; write `~/<your name>-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, with one paragraph of the change and the exact gate lines. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Create no pull request.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
Report what was not done.
No `rm -rf` outside the job directory.
Every new test opens with t.Parallel(); unit tests use no real time.
Touch only the files this card names; a fix that needs another file goes into your report as a proposed diff.")
      (:id "resume-many-streams" :tier "-" :needs ("add-tla-edit-refreshes-records")
      :title "The coordinator resumed five streams in a loop at 1:35 PM (debt, lint, findings, libs, missed-2026-10-04) after one transient tree-gate failure"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 60 minutes
DEPENDS-ON: add-tla-edit-refreshes-records
PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/resume_many_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestResumeTakesSeveralStreams
Deadline: finish within 60 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. The coordinator resumed five streams in a loop at 1:35 PM (debt, lint, findings, libs, missed-2026-10-04) after one transient tree-gate failure. resume takes --stream more than once or comma separated, with one --did for all; each stream is resumed or refused on its own line; exit 1 when any is refused. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/resume_many_test.go,docs/SPEC-SPRINT.md
  COMMIT: resume-many-streams: resume takes several streams
  VERDICT: on the twin store resume --stream a,b with a running and b stopped resumes b and refuses a on its own line with exit 1.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestResumeTakesSeveralStreams' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestResumeTakesSeveralStreams` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "land-one-lander" :tier "-" :needs ("land-clone-self-heals")
      :title "Two landers ran in one checkout: a hand land pass at 3:21 PM beside the server run --land, which left the cache clone dirty and refused every stream"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: land-clone-self-heals
PATHS: cmd/nova-sprint/land.go,cmd/nova-sprint/landlock.go,cmd/nova-sprint/landlock_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestLandRefusesWhileAnotherLanderHoldsTheClone
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Two landers ran in one checkout: a hand land pass at 3:21 PM beside the server run --land, which left the cache clone dirty and refused every stream. land takes an exclusive lock per clone for the whole pass (a lock file beside the clone, holding the pid and the verb); a second land, by hand or by the server, is refused while it is held, naming the holder; a lock whose holder pid is gone is taken over and logged. land refuses outright, naming the server, when the server is running with --land and the caller is not it. land --dry-run takes no lock. This may duplicate a lock another child is building today; if a lock already landed on the base, add only the missing refusal and the test. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/land.go,cmd/nova-sprint/landlock.go,cmd/nova-sprint/landlock_test.go,docs/SPEC-SPRINT.md
  COMMIT: land-one-lander: land refuses while another lander holds the clone or the server lands
  VERDICT: two land passes on one temporary clone: the second is refused naming the first; a stale lock from a dead pid is taken over; a hand land beside a server with --land is refused.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestLandRefusesWhileAnotherLanderHoldsTheClone' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestLandRefusesWhileAnotherLanderHoldsTheClone` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "store-latency-row" :tier "-" :needs ("seat-key-follows-record")
      :title "The store latency was measured by hand with redis-cli (20 pings, then 500 on one connection) at 3:26 PM while landing was slow"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 90 minutes
DEPENDS-ON: seat-key-follows-record
PATHS: cmd/nova-sprint/run.go,cmd/nova-sprint/reads.go,internal/sprint/store/where.go,internal/sprint/store/rtt_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint/store TestWhereReportsTheStoreRoundTrip
Deadline: finish within 90 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. The store latency was measured by hand with redis-cli (20 pings, then 500 on one connection) at 3:26 PM while landing was slow. The server measures it: every 10 s one store round trip, timed by the injected clock, kept as p50 and p99 over the last minute; where --json carries store_rtt_p50_ms and store_rtt_p99_ms, where prints them on its store line. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/run.go,cmd/nova-sprint/reads.go,internal/sprint/store/where.go,internal/sprint/store/rtt_test.go,docs/SPEC-SPRINT.md
  COMMIT: store-latency-row: where shows the store round trip measured by the server
  VERDICT: with a fake store and clock the p50 and p99 are computed and printed.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint/store -run 'TestWhereReportsTheStoreRoundTrip' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint/store ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint/store -run TestWhereReportsTheStoreRoundTrip` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "fleet-quiet-machine" :tier "-" :needs ("stream-set-base" "land-record-unreported-push")
      :title "Quieting a machine is a hand bus broadcast"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: stream-set-base,land-record-unreported-push
PATHS: cmd/nova-sprint/fleetquiet.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/coordinator.go,cmd/nova-sprint/view.go,internal/sprint/steps_work.go,internal/sprint/held.go,internal/sprint/fleet_quiet*.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestFleetQuietDealsNothingAndTellsWorkersUntilItEnds
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Quieting a machine is a hand bus broadcast. At 3:39 PM the coordinator sent all friends a message to start no Go on the Studio until 3:50 PM, because its load was 64 and the macOS CI legs of the dev promotion (PR 5321) timed out on it. Add fleet quiet <member> --for <duration> --reason <text> (the coordinator; --until <RFC3339> also taken): the deal gives the member nothing until then (dealt work finishes), view worker shows every member and friend a QUIET line naming the machine, the end time and the reason (run no go build or test there), and it ends by itself at the time, logged; fleet quiet <member> --end ends it early. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/fleetquiet.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/coordinator.go,cmd/nova-sprint/view.go,internal/sprint/steps_work.go,internal/sprint/held.go,internal/sprint/fleet_quiet*.go,docs/SPEC-SPRINT.md
  COMMIT: fleet-quiet-machine: fleet quiet holds a machine for a time and tells every worker
  VERDICT: on the twin store with an injected clock, a quiet member is dealt nothing and every worker view carries the QUIET line until the time, then deals resume by themselves.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFleetQuietDealsNothingAndTellsWorkersUntilItEnds' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFleetQuietDealsNothingAndTellsWorkersUntilItEnds` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "wait-many-notes" :tier "-" :needs ("resume-many-streams")
      :title "The coordinator waited judgments one at a time in a loop at 8:33 AM (wait <id> --for 3h per note)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 60 minutes
DEPENDS-ON: resume-many-streams
PATHS: cmd/nova-sprint/verbs.go,internal/sprint/inbox.go,cmd/nova-sprint/wait_many_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestWaitTakesSeveralNotes
Deadline: finish within 60 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. The coordinator waited judgments one at a time in a loop at 8:33 AM (wait <id> --for 3h per note). wait takes <note>[,<note>]... and --group <id> --expect <n> as ack does; each note is set or refused on its own line. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/verbs.go,internal/sprint/inbox.go,cmd/nova-sprint/wait_many_test.go,docs/SPEC-SPRINT.md
  COMMIT: wait-many-notes: wait takes several notes and a group
  VERDICT: on the twin store wait n1,n2 --for 3h sets both; --group with a changed size is refused and nothing changes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestWaitTakesSeveralNotes' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestWaitTakesSeveralNotes` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "add-tla-edit-refreshes-records" :tier "-" :needs ("fleet-quiet-machine")
      :title "Cards that edit tla/*.tla land without refreshing tla/RUNS.tsv, and the TLC records class test (internal/ci/tlc_records_class_test.go) then calls the records stale on the base"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 90 minutes
DEPENDS-ON: fleet-quiet-machine
PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/add_tla_records_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestAddRefusesATlaEditWithoutARecordsRefresh
Deadline: finish within 90 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Cards that edit tla/*.tla land without refreshing tla/RUNS.tsv, and the TLC records class test (internal/ci/tlc_records_class_test.go) then calls the records stale on the base. add refuses a brief whose PATHS cover a tla/*.tla model but do not cover tla/RUNS.tsv, or whose STEPs do not name tlacheck merge --keep; the refusal names the remedy (run the changed groups on a Linux bench, then tlacheck merge --keep tla/RUNS.tsv, as tla/README.md says). A card that only reads tla/ is untouched. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/add_tla_records_test.go,docs/SPEC-SPRINT.md
  COMMIT: add-tla-edit-refreshes-records: add refuses a model edit that does not refresh the TLC records
  VERDICT: add of a brief with PATHS tla/Land.tla and no records step is refused with the remedy; the same brief with tla/** and the tlacheck step is added.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestAddRefusesATlaEditWithoutARecordsRefresh' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestAddRefusesATlaEditWithoutARecordsRefresh` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "stream-set-base" :tier "-" :needs ("fleet-test-process-alarm")
      :title "A stream whose base branch is gone, merged or red has no verb to move to a live base"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: fleet-test-process-alarm
PATHS: cmd/nova-sprint/verbs.go,internal/sprint/streams.go,internal/sprint/steps_edit.go,internal/sprint/stream_set_base_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestStreamSetBaseRepointsQueuedCards
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. A stream whose base branch is gone, merged or red has no verb to move to a live base. sprint-next sat on base rowan/bus-rename from 12:04 PM; the only remedy was a re-cut of each card. Add stream set <stream>... --base <branch> (the coordinator): every card of the stream not yet dealt, and every card queued to merge, gets its BASE line rewritten to the branch (a brief revision recorded per card, as brief records one); refused, nothing written, when origin has no such branch or when a card PATHS are absent at its tip (the check add runs). Dealt and working cards keep their base and are listed. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/verbs.go,internal/sprint/streams.go,internal/sprint/steps_edit.go,internal/sprint/stream_set_base_test.go,docs/SPEC-SPRINT.md
  COMMIT: stream-set-base: stream set --base re-points a stream to a live base
  VERDICT: on the twin store with a temporary origin, stream set --base re-points ready and queued cards and lists dealt ones; a missing branch is refused and nothing changes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestStreamSetBaseRepointsQueuedCards' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestStreamSetBaseRepointsQueuedCards` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "fleet-test-process-alarm" :tier "-" :needs ("land-record-unreported-push" "friend-deal-most-room")
      :title "Runaway test processes are found only by hand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: land-record-unreported-push,friend-deal-most-room
PATHS: cmd/nova-sprint/fleet.go,cmd/nova-sprint/friends.go,cmd/nova-sprint/verbs.go,internal/sprint/presence.go,internal/sprint/settings.go,internal/sprint/steps_tick.go,internal/sprint/runaway_tests_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestFleetBeatRunawayTestsRaisesOneAlarm
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Runaway test processes are found only by hand. At 3:37 PM the Studio had 289 nova-sprint.test processes (children run as nova-sprint.test init of one go-build test binary) at load 64, and the coordinator killed them by hand. Add a test-process count to the fleet beat: fleet beat <member> --tests <n>, counted by the beat agent as the live processes whose name ends in .test, and the same field on friend beat; the tick raises one judgment per episode, runaway test processes on <member>: <n>, when n is over a threshold (the sprint row in nova-config, default 4 x the member width), naming the oldest parent pid; the episode ends when n falls under half the threshold. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/fleet.go,cmd/nova-sprint/friends.go,cmd/nova-sprint/verbs.go,internal/sprint/presence.go,internal/sprint/settings.go,internal/sprint/steps_tick.go,internal/sprint/runaway_tests_test.go,docs/SPEC-SPRINT.md
  COMMIT: fleet-test-process-alarm: the fleet beat counts test processes and alarms on a runaway
  VERDICT: on the twin store a beat with tests over the threshold raises one judgment, a second beat raises none, a low beat ends the episode.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFleetBeatRunawayTestsRaisesOneAlarm' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFleetBeatRunawayTestsRaisesOneAlarm` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "land-clone-self-heals" :tier "-" :needs ("land-base-gate-stops-stream")
      :title "A dirty land clone refused every stream"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: land-base-gate-stops-stream
PATHS: cmd/nova-sprint/land.go,cmd/nova-sprint/land_clean_clone_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestLanderRestoresItsOwnDirtyCacheClone
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. A dirty land clone refused every stream. At 3:21 PM a hand land pass ran beside the server lander in the same cache clone (~/Library/Caches/nova-sprint/land/github.com-mas-bandwidth-nova-tools-<hash>); a pass cut short left modified files there, and every later pass refused all 11 streams with: the clone is not clean. The lander owns that cache clone: it computed and created the path. Before each batch, when its own cache clone is not clean, it aborts any merge in progress, resets the clone to the fetched base and removes untracked files, inside that clone only, and logs one LAND CLEANED line naming the files; a clone the caller gave with --repo-dir is never cleaned and is refused as today. Test with a temporary clone. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/land.go,cmd/nova-sprint/land_clean_clone_test.go,docs/SPEC-SPRINT.md
  COMMIT: land-clone-self-heals: the lander restores its own dirty cache clone before a batch
  VERDICT: a temporary cache clone with a modified file and a half merge is restored and the batch lands; a dirty --repo-dir clone is still refused.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestLanderRestoresItsOwnDirtyCacheClone' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestLanderRestoresItsOwnDirtyCacheClone` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "coordinator-ping-verb" :tier "-" :needs ("friend-session-liveness")
      :title "Replace the coordinator's hand ping loop (a zsh loop that pinged every few minutes; one friend's daemon reported \"no ping for 3m\") with an installed verb of nova-friend that pings every friend each second, as Glenn designed it: the coordina"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
DEPENDS-ON: friend-session-liveness
PATHS: internal/friend/**,cmd/nova-friend/**,fleet/loops.tsv,docs/SPEC-FRIEND.md,docs/CLI.md,docs/TESTS.md
TEST: cmd/nova-friend TestPingMarksAFriendDownAfterTenSecondsWithoutAPong
DEADLINE: finish within 180 minutes
You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/<your name>-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/<your name>-working/jobs/<job>/`. Where this card says JOB.md, read BRIEF.md.
Libraries considered: Go standard library and testify, already in tree; no new dependency.
ATTRIBUTION: commits and reports name the friend (By: Rowan) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. Replace the coordinator's hand ping loop (a zsh loop that pinged every few minutes; one friend's daemon reported \"no ping for 3m\") with an installed verb of nova-friend that pings every friend each second, as Glenn designed it: the coordinator is the server, both sides ping each second, and a friend is down after 10 s without a pong. Include the buds (rowan-space, rowan-mas, rowan-next, rowan-personal) by reading the friend rows, never a hard-coded list. The verb is a loop with state (last pong per friend), tests with an injected clock and a fake transport, a spec paragraph in docs/SPEC-FRIEND.md and its usage in docs/CLI.md; it is installable the way the other nova loops are (a loop row, not a hand plist), and its output is one line per state change, not per ping.

STEP 1. Set `JOB=~/<your name>-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of BASE; export GOCACHE=~/<your name>-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read docs/STANDARD.md and the spec sections the task names before the first edit.
STEP 2. Red first: write the test the TEST line names (t.Parallel(), no real time, no sockets: an injected clock and an in-memory store), see it fail, then make the change. Where the change touches a table's shape or meaning, amend TABLES.lock in the same commit with the reason and the rule it cites.
STEP 3. Gate: `gofmt -l` on every changed Go file prints nothing; `nice -n 19 go vet` and `nice -n 19 go test -p 2 -count=1 -timeout 600s` on every package you changed plus ./internal/ci/ and ./internal/docs/; keep the last line of each. A ledger under internal/ci/testdata that the gate names only shrinks.
STEP 4. END. Commit only PATHS; push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`, never force; verify `git ls-remote` equals `git rev-parse HEAD`; write `~/<your name>-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, with one paragraph of the change and the exact gate lines. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Create no pull request.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
Report what was not done.
No `rm -rf` outside the job directory.
Every new test opens with t.Parallel(); unit tests use no real time.
Touch only the files this card names; a fix that needs another file goes into your report as a proposed diff.")
      (:id "accept-heavy-verdict" :tier "-" :needs ("land-verify-landed-ancestry" "read-asked-again-after-takeback")
      :title "The coordinator cannot record its own heavy read"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: land-verify-landed-ancestry,read-asked-again-after-takeback
PATHS: cmd/nova-sprint/verbs.go,internal/sprint/readers.go,internal/sprint/steps_review.go,internal/sprint/accept_heavy_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestAcceptHeavyRecordsTheCoordinatorsReadNotAReaders
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. The coordinator cannot record its own heavy read. Today a reader bounced a card as broken on a true model trailer (a false bounce), and accept was refused at 1 of 2 ok reads, so the card waited for another read or a re-cut. Add accept <id>... --heavy --evidence <path> --reason <text> (the coordinator): it records the coordinator heavy read as its own read row (reader coordinator:<actor>, kind heavy, the evidence path and its sha256), which counts as one ok read toward the card read rule, and the card shows it as the coordinator verdict with its evidence. It is never written as a reader ok and never under a reader name; it is refused without a readable evidence file, and refused for a card not in review. A reader broken verdict on the same attempt stays on the card, marked overruled by the coordinator heavy read. Cite docs/SPEC-SPRINT.md. Counts toward nova-sprint v1.0.0 (Glenn 2026-10-04 3:55 PM: \"Keep looking for verbs you are missing\"); found by hand on the Studio on 2026-10-04. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of the spec.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/verbs.go,internal/sprint/readers.go,internal/sprint/steps_review.go,internal/sprint/accept_heavy_test.go,docs/SPEC-SPRINT.md
  COMMIT: accept-heavy-verdict: accept --heavy records the coordinator heavy read with its evidence
  VERDICT: on the twin store a pro card with one ok and one broken read is accepted with --heavy and an evidence file; card shows the coordinator read and the overruled reader; no reader row is forged.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestAcceptHeavyRecordsTheCoordinatorsReadNotAReaders' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestAcceptHeavyRecordsTheCoordinatorsReadNotAReaders` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friends-tokens-column" :tier "-" :needs ("verb-seat-install-pushb" "friends-okpct-verified")
      :title "Each friend card's RESULT.md carries `usage: in=<n> out=<n> cache=<n>`"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
DEPENDS-ON: verb-seat-install-pushb,friends-okpct-verified
PATHS: internal/sprint/**,cmd/nova-sprint/**,internal/sprintdash/**,docs/SPEC-SPRINT.md,TABLES.lock
TEST: internal/sprint TestFriendTokensSumPerFriendFromTheResultUsage
DEADLINE: finish within 180 minutes
You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/<your name>-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/<your name>-working/jobs/<job>/`. Where this card says JOB.md, read BRIEF.md.
Libraries considered: Go standard library and testify, already in tree; no new dependency.
ATTRIBUTION: commits and reports name the friend (By: Rowan) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. Each friend card's RESULT.md carries `usage: in=<n> out=<n> cache=<n>`. The store sums it per friend, and the friends table gets a \"tokens\" column in a compact form (1.2M). It shows tokens for subscription friends; for api-billed friends (by the friend row's billing field, which the cost child is adding; until it exists, treat every friend as subscription and leave a TODO-free switch on that field) the column shows dollars to the cent, rounded up. Changes: the shape change in internal/sprint/schema.go, a TABLES.lock amendment, the store sum, the column on the friends table and the dashboard (internal/sprintdash), and tests (parse, sum, render). This builds on verb-seat-install-pushb.

STEP 1. Set `JOB=~/<your name>-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of BASE; export GOCACHE=~/<your name>-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read docs/STANDARD.md and the spec sections the task names before the first edit.
STEP 2. Red first: write the test the TEST line names (t.Parallel(), no real time, no sockets: an injected clock and an in-memory store), see it fail, then make the change. Where the change touches a table's shape or meaning, amend TABLES.lock in the same commit with the reason and the rule it cites.
STEP 3. Gate: `gofmt -l` on every changed Go file prints nothing; `nice -n 19 go vet` and `nice -n 19 go test -p 2 -count=1 -timeout 600s` on every package you changed plus ./internal/ci/ and ./internal/docs/; keep the last line of each. A ledger under internal/ci/testdata that the gate names only shrinks.
STEP 4. END. Commit only PATHS; push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`, never force; verify `git ls-remote` equals `git rev-parse HEAD`; write `~/<your name>-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, with one paragraph of the change and the exact gate lines. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Create no pull request.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
Report what was not done.
No `rm -rf` outside the job directory.
Every new test opens with t.Parallel(); unit tests use no real time.
Touch only the files this card names; a fix that needs another file goes into your report as a proposed diff.")))
    (:stream "sprint-v1-1-0" :cards
     ((:id "v11-batch-selectors" :tier "-" :needs ()
      :title "On 2026-10-04 the coordinator changed hundreds of cards one process at a time (brief edits, recuts, reworks, re-tiers, releases), and a recut of 195 pinned cards took too long to finish"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: cmd/nova-sprint/verbs.go (the verb table), the brief, recut, rework, return, release, rank and drop verbs, docs/SPEC-SPRINT.md
STOP: those verbs take a selector (--stream, --who, --state, --ids-file) and change N cards in one store step, or the report names what blocks it
PATHS: cmd/nova-sprint/select*.go,cmd/nova-sprint/select*_test.go,cmd/nova-sprint/verbs.go,internal/sprint/select*.go,internal/sprint/select*_test.go,docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./cmd/nova-sprint TestOneCallRecutsEveryCardTheSelectorNames
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 150 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 the coordinator changed hundreds of cards one process at a time (brief edits, recuts, reworks, re-tiers, releases), and a recut of 195 pinned cards took too long to finish. The owner: \"BATCH EVERYTHING\". Give brief, recut, rework, return, release, rank and drop one selector grammar: --stream <s>, --who <friend|none>, --state <ready|waiting|held|merging>, --ids-file <path>, combinable, with --dry-run listing what would change; each verb applies to every selected card in one store step and prints one line per card plus a total. A brief change by selector takes a transform (--set-base <branch>, --drop-who, --tier <t>) instead of a whole file.
Libraries considered: none new; the tree's own sprint store steps and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store: one recut call with --who friend.x --drop-who re-cuts every card pinned to x, and their dependants follow the twins.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md from the selector code.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./cmd/nova-sprint/ ./internal/sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-dev-sync-every-cycle" :tier "-" :needs ()
      :title "On 2026-10-04 the base and the development branch drifted for an afternoon while hundreds of cards landed on each; folding them took 105 conflicts and an evening (the owner: \"promote every cycle or drift causes a big fuckup\")"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: internal/sprint (the land round and the dev-is-behind rule), docs/SPEC-SPRINT.md
STOP: each cycle the machine merges the development branch into the base, and a conflict stops landing with one judgment while it is small, or the report names what blocks it
PATHS: internal/sprint/sync*.go,internal/sprint/sync*_test.go,internal/sprint/land*.go,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestTheBaseTakesTheDevelopmentBranchEveryCycle
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 the base and the development branch drifted for an afternoon while hundreds of cards landed on each; folding them took 105 conflicts and an evening (the owner: \"promote every cycle or drift causes a big fuckup\"). Each land cycle (or every N landings), the machine merges the development branch into the base; a clean merge lands like a batch through the tree gate, a conflict stops landing with ONE judgment naming the files while they are few, and the drift (commits each side lacks, minutes since the last sync) is on `where --json` and the dashboard's merge row.
Libraries considered: none new; the tree's own sprint land code and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store and a twin repository.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-streams-by-repo" :tier "-" :needs ()
      :title "On 2026-10-04 the owner asked which open work was not for nova-tools or nova-sprint, so it could be removed, and which of the rest was essential to the next release"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: internal/sprint (streams, the add path that reads each brief's REPO: and BASE:), cmd/nova-sprint (where, needs, card, drop, hold), docs/SPEC-SPRINT.md
STOP: every stream records the repository and base its cards name, `nova-sprint streams` lists each stream's repo, base, release and every card with its state in one call, and drop and hold take --repo, or the report names what blocks it
PATHS: internal/sprint/stream*.go,internal/sprint/stream*_test.go,internal/sprint/steps_add*.go,cmd/nova-sprint/streams*.go,cmd/nova-sprint/streams*_test.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./cmd/nova-sprint TestStreamsListsEveryStreamByRepoInOneCall
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 the owner asked which open work was not for nova-tools or nova-sprint, so it could be removed, and which of the rest was essential to the next release. The coordinator answered by hand: `where` to list the streams, `needs --stream` once per stream for its card ids (roots and edges only, so not every card), then `card <id> --json` once per card to read REPO: out of its brief: a thousand-odd round trips, minutes of wall time, and the owner stopped it as too slow. Make it one call. (1) A stream records the repository and base of its cards when a card is added (each brief's REPO: and BASE:; a stream whose cards name more than one keeps them all, which is itself a finding to print). (2) A new verb `nova-sprint streams [--repo <owner/name>] [--release <name>] [--cards] [--json]` prints each stream with its repo(s), base(s), open and landed counts, and with --cards every card: id, state, tier, title (the first sentence of THE TASK), its needs. (3) `drop` and `hold` take `--repo <owner/name>` (and `--stream` many), refusing without --expect <n>, so \"remove everything not for nova-tools\" is one command the owner can read before it runs. (4) A release tag on a stream (`stream set <s> --release <name>`) so \"what is essential to the next release\" is a filter, not a hand sort. One server round trip for the whole listing on the live store (measure it: under 1 s for 3,000 cards).
Libraries considered: none new; the tree's own sprint store, add and where code and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store: three streams over two repos, one stream mixing both.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-sprint/ ./internal/ci/ ./internal/docs/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-test-writes-no-source" :tier "-" :needs ()
      :title "On 2026-10-04 the adoption pass found that a cmd/nova-sprint test writes cmd/nova-sprint/wake.json into the source tree; the dirty checkout made `nova-update release build` record no commit"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: cmd/nova-sprint tests (grep wake.json), the release build's commit stamp (nova-update release build)
STOP: no cmd/nova-sprint test writes into the source tree, and a class test proves no test leaves a file behind, or the report names what blocks it
PATHS: cmd/nova-sprint/*_test.go,internal/ci/testwrites_class_test.go
TEST: ./internal/ci TestNoTestWritesIntoTheSourceTree
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 90 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 the adoption pass found that a cmd/nova-sprint test writes cmd/nova-sprint/wake.json into the source tree; the dirty checkout made `nova-update release build` record no commit. Make that test write under t.TempDir(), and add a class test that runs no test itself but checks the tree is clean (git status --porcelain) after the package tests in CI's own run, naming any file a test left.
Libraries considered: none new; testing's TempDir and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first.
STEP 3. Make it pass.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-base-red-auto-resume" :tier "-" :needs ()
      :title "On 2026-10-04 the base went red for a few minutes and every stream that tried to land was stopped, each with its own judgment; once the base was fixed the coordinator resumed eight streams by hand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: internal/sprint (the land base-red fact and the stream stop it sets), docs/SPEC-SPRINT.md
STOP: a red base stops landing once with one judgment, and every stream resumes by itself when the base gate passes again, or the report names what blocks it
PATHS: internal/sprint/land*.go,internal/sprint/land*_test.go,internal/sprint/rules*.go,internal/sprint/rules*_test.go,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestStreamsResumeWhenTheBaseGatePassesAgain
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 the base went red for a few minutes and every stream that tried to land was stopped, each with its own judgment; once the base was fixed the coordinator resumed eight streams by hand. Make the base's health one fact: the base tree gate failing stops all landing with ONE judgment naming the failing test; each land pass re-checks the base tip, and when it passes, every stream stopped only by the red base resumes by rule, recorded in the log.
Libraries considered: none new; the tree's own sprint land and rules code and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store and a twin repository.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-reland-from-commits" :tier "-" :needs ()
      :title "On 2026-10-04 a merge left 94 landed card commits out of the tree, and the coordinator wrote their re-land briefs with a hand loop (one per commit: cherry-pick it onto the current base, keep its intent, finish with no change when the code a"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: cmd/nova-card (generate --from ledger|findings|help), docs/SPEC-CARD-CONTRACT.md
STOP: nova-card generate --from commits writes one pre-linted re-land brief per commit, or the report names what blocks it
PATHS: cmd/nova-card/**,internal/card*/**,docs/CLI.md
TEST: ./cmd/nova-card TestGenerateFromCommitsWritesOneRelandBriefPerCommit
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 a merge left 94 landed card commits out of the tree, and the coordinator wrote their re-land briefs with a hand loop (one per commit: cherry-pick it onto the current base, keep its intent, finish with no change when the code already does it). Add `nova-card generate --from commits (--range <a>..<b> [--paths <glob>] | --file <list>)`: one brief per commit with PATHS the commit's files, the gate its packages plus ./internal/ci/, held to the card lint, written with manifest.tsv, in commit order so a stream lands them oldest first.
Libraries considered: none new; the tree's own card planner and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin repository.
STEP 3. Make it pass.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./cmd/nova-card/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-conflict-rule-in-tick" :tier "-" :needs ()
      :title "On 2026-10-04 every head that did not merge, changed files outside its PATHS, or failed the tree gate stopped its whole stream until the coordinator answered (return, rework on the tip, resume), so 19 streams sat stopped and the coordinator"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: internal/sprint (the land refusal facts: conflict, checks, tree gate; the rules the tick answers by), docs/SPEC-SPRINT.md (judgments answered by rule)
STOP: a head that does not merge or fails the gate is returned and redone on the tip by rule while its stream keeps landing, with a judgment only on a repeat, or the report names what blocks it
PATHS: internal/sprint/rules*.go,internal/sprint/rules*_test.go,internal/sprint/land*.go,internal/sprint/land*_test.go,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestAConflictingHeadIsRedoneOnTheTipAndItsStreamKeepsLanding
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 150 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 every head that did not merge, changed files outside its PATHS, or failed the tree gate stopped its whole stream until the coordinator answered (return, rework on the tip, resume), so 19 streams sat stopped and the coordinator ran a hand loop answering them every two minutes. Make it a rule of the tick: such a head is returned, its next attempt is staged on the base tip at flash with the refusal as its fix, and the stream skips it and lands the rest of its batch; the stream stops and a judgment is raised only when the same card is refused the same way twice (a brief defect). The owner: \"the machine keeps itself fed\"; \"a hand step is a missing instruction\".
Libraries considered: none new; the tree's own sprint rules and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store and a twin repository.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-promote-verb" :tier "-" :needs ()
      :title "On 2026-10-04 the coordinator promoted by hand three times: a PR whose head was the base branch got the base auto-deleted on merge (the repository deletes merged heads), the deleted-test rule then failed on the promotion merge commit itself"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: cmd/nova-sprint/promote.go, internal/ci (the deleted-test rule reads the tip commit), docs/SPEC-SPRINT.md (promotion)
STOP: promote opens and merges the promotion from a throwaway branch after a whole-tree gate, never naming the base as a PR head, or the report names what blocks it
PATHS: cmd/nova-sprint/promote*.go,internal/sprint/promote*.go,internal/ci/classtests_class_test.go,docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./cmd/nova-sprint TestPromoteMergesFromAThrowawayBranchAndKeepsTheBase
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 150 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 the coordinator promoted by hand three times: a PR whose head was the base branch got the base auto-deleted on merge (the repository deletes merged heads), the deleted-test rule then failed on the promotion merge commit itself, which stopped every stream until a plain commit was pushed over it, and CI never ran on sprint/* heads so the PR sat blocked. Make it one verb: promote gates the base tip (build, vet, every package's tests and the functional whole-tree checks), pushes promote/<date>-<n> at that tip, opens the PR to the development branch and merges it, and never names the base as a head; and make the deleted-test rule read every commit since the last gated promotion, not only the tip, so a promotion merge is never refused for deletions declared below it.
Libraries considered: none new; the tree's own promote, ci and forge client code and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first with a twin repository and a fake forge.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-hold-withdraws-started" :tier "-" :needs ()
      :title "On 2026-10-04 a held friend still showed two working cards, because friend take and friend down keep any card with a push: \"a held friend keeps started cards\" (the owner: \"Notice how alex is held, but still has working cards? nonono.\")"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: cmd/nova-sprint/hold.go, internal/sprint (friend down, friend take, the started-card rule), docs/SPEC-SPRINT.md
STOP: holding or downing a friend returns its started cards to ready and the next taker starts from the pushed branch, or the report names what blocks it
PATHS: internal/sprint/hold*.go,internal/sprint/hold*_test.go,internal/sprint/friend*.go,internal/sprint/friend*_test.go,cmd/nova-sprint/hold.go,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestHoldingAFriendReturnsItsStartedCards
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 a held friend still showed two working cards, because friend take and friend down keep any card with a push: \"a held friend keeps started cards\" (the owner: \"Notice how alex is held, but still has working cards? nonono.\"). Held and down mean no cards: hold, friend down and a usage limit return the friend's started cards to ready; the next attempt is staged from the card's pushed branch (its head), so no work is lost; recorded in the log.
Libraries considered: none new; the tree's own sprint hold and friend code and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-stream-batch-from-branches" :tier "-" :needs ()
      :title "The owner, 2026-10-04: \"the key to merging is to merge batch into a work stream branch in work order, then merge that branch into dev\"; \"don't merge a card at a time when you get behind, do it in batches\""
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: internal/sprint (the lander's batch and its generated-ledger handling), docs/SPEC-SPRINT.md (merging in batches, per stream, in work order)
STOP: when a stream is behind, the lander merges the base into each member's branch, takes the base's side of generated ledgers and regenerates them once, and lands the stream in work order as one batch, or the report names what blocks it
PATHS: internal/sprint/land*.go,internal/sprint/land*_test.go,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestABehindStreamLandsAsOneBatchInWorkOrder
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 150 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. The owner, 2026-10-04: \"the key to merging is to merge batch into a work stream branch in work order, then merge that branch into dev\"; \"don't merge a card at a time when you get behind, do it in batches\". That evening the lint stream sat 34 deep for hours on generated-ledger conflicts, and the coordinator cleared it by hand: merge the base into each member branch, the base's side of every generated ledger, a code conflict parks that one card for a redo on the tip, then the lander took 25 cards as one batch. Make it the lander's own behaviour when a stream has more than a batch waiting: the same steps, one regeneration of the generated ledgers on the batch tip, the tree gate once, one push. Where the merge-tree cards already build this, extend them rather than duplicate.
Libraries considered: none new; the tree's own sprint land code and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store and a twin repository.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-merge-row-on-dashboard" :tier "-" :needs ()
      :title "On 2026-10-04 the owner asked several times what percentage of the merge backlog was cleared, and the hand merge was invisible on the dashboard (\"Is this progress visible in the sprint dashboard yet?\")"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: internal/sprintdash (the page and its pull routes), cmd/nova-sprint where --json, docs/SPEC-SPRINT-DASHBOARD.md
STOP: the dashboard and where --json show the merge state: in merging, landed per 30 minutes, the oldest merge's age, base drift, last sync and the base gate's state, or the report names what blocks it
PATHS: internal/sprintdash/**,cmd/nova-sprint/reads.go,docs/SPEC-SPRINT-DASHBOARD.md
TEST: ./internal/sprintdash TestTheMergeRowCarriesTheBaseGateAndTheDrift
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 the owner asked several times what percentage of the merge backlog was cleared, and the hand merge was invisible on the dashboard (\"Is this progress visible in the sprint dashboard yet?\"). Add one merge row (dark, the house style): cards in merging and review, landed per 30 minutes, the oldest merging card's age, the base's gate state (green/red with the failing test), the drift between the base and the development branch, the minutes since the last sync and the last promotion; the same fields on where --json for the coordinator's role view.
Libraries considered: none new; the tree's own dashboard and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first.
STEP 3. Make it pass; cite docs/SPEC-SPRINT-DASHBOARD.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprintdash/ ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-tla-paths-frontier" :tier "-" :needs ()
      :title "The owner, 2026-10-04: \"When we do TLA+ modeling work, I would like that to go to frontier models.\" Make it mechanical: nova-sprint add and nova-card generate set tier frontier on a card whose PATHS name tla/ (a .tla module or an MC config)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: the card tier logic in nova-sprint add and in nova-card's planner, docs/SPEC-SPRINT.md (tiers)
STOP: a card whose PATHS include tla/ is tiered frontier at add and in nova-card, or the report names what blocks it
PATHS: internal/sprint/tier*.go,internal/sprint/tier*_test.go,cmd/nova-card/**,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestACardThatWritesAModelIsTieredFrontier
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 90 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. The owner, 2026-10-04: \"When we do TLA+ modeling work, I would like that to go to frontier models.\" Make it mechanical: nova-sprint add and nova-card generate set tier frontier on a card whose PATHS name tla/ (a .tla module or an MC config), and refuse a lower explicit tier on such a card with the reason; TLC run records (tla/RUNS.tsv, tla/CASES.tsv) alone do not count, since running the checker is mechanical.
Libraries considered: none new; the tree's own tier code and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-card/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-rebase-verb" :tier "-" :needs ()
      :title "On 2026-10-04 an integration branch was merged and auto-deleted while 29 cards still named it as BASE, and 118 held cards named dev; every landing on the deleted branch failed its fetch, and the coordinator rewrote 147 briefs by hand during"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: the BASE: line of a card brief (docs/SPEC-CARD-CONTRACT.md), the lander's fetch of a card's base, cmd/nova-sprint/verbs.go
STOP: one verb moves every unlanded card from one base branch to another, dealt cards included, or the report names what blocks it
PATHS: cmd/nova-sprint/rebase*.go,internal/sprint/rebase*.go,internal/sprint/rebase*_test.go,cmd/nova-sprint/verbs.go,docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./internal/sprint TestRebaseMovesEveryUnlandedCardToTheNewBase
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 an integration branch was merged and auto-deleted while 29 cards still named it as BASE, and 118 held cards named dev; every landing on the deleted branch failed its fetch, and the coordinator rewrote 147 briefs by hand during a stopped sprint, then recreated the branch to land the 8 dealt cards it could not rewrite. Add `nova-sprint rebase --from <branch> --to <branch> [--dry-run]`: every unlanded card whose BASE is --from moves to --to on a RUNNING machine (an undealt card's brief changes; a dealt or merging card keeps its head and lands on the new base, which must contain the old one, checked with git merge-base --is-ancestor), recorded in the log, one line per card. Also: a land whose base branch is missing raises one judgment naming every card on that base and the rebase line that fixes it.
Libraries considered: none new; the tree's own sprint store, land and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store and a twin repository.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-client-default-server" :tier "-" :needs ()
      :title "On 2026-10-04 the coordinator ran every nova-sprint verb through a shell wrapper that decrypted the store's Redis password per call and pointed the verb at Redis directly"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: cmd/nova-sprint/main.go (how a verb picks the server or the store), docs/SPEC-SPRINT.md (the server and its clients), docs/CLI.md
STOP: every coordinator verb reaches the local sprint server as a client with no Redis password and no wrapper, by default, or the report names the verb that cannot
PATHS: cmd/nova-sprint/main.go,cmd/nova-sprint/main_test.go,cmd/nova-sprint/client*.go,docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./cmd/nova-sprint TestACoordinatorVerbReachesTheLocalServerWithNoSecret
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04 the coordinator ran every nova-sprint verb through a shell wrapper that decrypted the store's Redis password per call and pointed the verb at Redis directly. The owner: \"I don't like the ns.sh and you should replace that with proper verbs\"; \"Golang nova-tools and nova-sprint verbs only\". The server is the one writer, and NOVA_SPRINT_SERVER=127.0.0.1:6390 already works with no secret. Make that the default: with neither NOVA_SPRINT_REDIS nor NOVA_SPRINT_SERVER set, a verb tries the local server (127.0.0.1:6390) and says which it used; --redis stays for the server itself and twins. Document it, so a cold coordinator runs `nova-sprint <verb>` and nothing else.
Libraries considered: none new; the tree's own client and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first: with no store or server variable set, a coordinator verb against a fake local server succeeds and asks for no secret.
STEP 3. Make it pass in the files this card names; cite docs/SPEC-SPRINT.md from the function.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")
      (:id "v11-stuck-pin-fallback" :tier "-" :needs ()
      :title "On 2026-10-04, 212 cards sat pinned to executors that were down or held (four Claude accounts out of weekly usage, a held friend), 65 of them ready, while the fleet idled; the coordinator un-pinned them by hand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
START: internal/sprint (the friend deal, WHO pins and preference), docs/SPEC-SPRINT.md (friends first, the fleet as overflow)
STOP: a card pinned to a friend who is down, held or out of usage falls to the next executor by rule after a bound, with one judgment, or the report names what blocks it
PATHS: internal/sprint/friend*.go,internal/sprint/friend*_test.go,internal/sprint/deal*.go,internal/sprint/deal*_test.go,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestACardPinnedToADownFriendFallsToTheNextExecutor
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 120 minutes.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. On 2026-10-04, 212 cards sat pinned to executors that were down or held (four Claude accounts out of weekly usage, a held friend), 65 of them ready, while the fleet idled; the coordinator un-pinned them by hand. Rule: a WHO preference whose friend is down, held or limited is skipped at the deal (subscription friends with room first, then the fleet); a hard `WHO: only friend <name>` card waits but raises ONE judgment per friend after 30 minutes naming its cards. Where the who-preference change already does part of this, extend it rather than duplicate it.
Libraries considered: none new; the tree's own sprint deal code and testify.

STEP 1. Enter the worktree your job names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; go commands run where the job says, never on a machine that forbids them.
STEP 2. Write the red test first on a twin store with a fake clock.
STEP 3. Make it pass; cite docs/SPEC-SPRINT.md.
STEP 4. Run the gate: go test -count=1 -timeout 600s ./internal/sprint/ ./cmd/nova-sprint/ ./internal/ci/ and read the last line of each.
STEP 5. Commit on your own branch with the trailer. Nothing reaches the forge from inside the wall: in the job the git shim records a push, the pull request is the finish JOB.md names (STEP 6).
STEP 6. End as JOB.md says (docs/SPEC-CARD-CONTRACT.md): the gate's lines in the report.")))
    (:stream "sprint-next" :cards
     ((:id "sn-config-dsn-redf" :tier "-" :needs () :who "friend.freddy"
      :title "internal/config TestDsnCoverResolveDSNKeywordFlagWithPasswordStands is red on dev: fix it (the test or the resolver, whichever is wrong, and say which)"
      :brief "RESULT: sn-config-dsn-redf sha=2d720d219ff5 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
WHO: friend freddy
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: -
PATHS: internal/config/*.go
TEST: ./internal/config TestDsnCoverResolveDSNKeywordFlagWithPasswordStands
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify as the tree uses it; nova-bus2's client package for the Redis bus where the item moves off the git bus; no new dependency.

ATTRIBUTION: commits and reports name the friend (By: Freddy) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/freddy-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/freddy-working/jobs/<job>/`, using `~/freddy-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md.

THE TASK. internal/config TestDsnCoverResolveDSNKeywordFlagWithPasswordStands is red on dev: fix it (the test or the resolver, whichever is wrong, and say which). (Glenn, 2026-10-04 11:20 AM: nova-sprint related work goes through nova-sprint now.)

STEP 1. Set `JOB=~/freddy-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/sprint/mechanical-2026-10-02. Export GOCACHE=~/freddy-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the governing spec section first; cite it from every function that implements a rule.

STEP 2. Do the task above, writing the red test first.
  PATHS: internal/config/*.go
  COMMIT: sn-config-dsn-redf: internal/config TestDsnCoverResolveDSNKeywordFlagWithPasswordStands is red on dev: fix it 
  VERDICT: the named test is green on dev with the package gate green.

STEP 3. Obtain the shared Go-slot grant, then run `nice -n 19 env GOMAXPROCS=2 go test -p 2 ./internal/config -run 'TestDsnCoverResolveDSNKeywordFlagWithPasswordStands' -count=1 -timeout 600s`, then `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/config ./internal/ci`, record exact last lines, release the slot, and commit only PATHS with honest Freddy and actual model/harness attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/freddy-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/config -run TestDsnCoverResolveDSNKeywordFlagWithPasswordStands` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sn-deadline-rule-unifiedb" :tier "-" :needs ()
      :title "Unify the friend deadline rule of PR 5306 (the larger of 2 h and 3x her median run wall) with PR 5300 member deadline rule into one rule once both are on dev; one function, one spec paragraph, both tables"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: -
PATHS: internal/sprint/*.go,internal/sprint/*_test.go,docs/SPEC-SPRINT.md,TABLES.lock
TEST: ./internal/sprint TestOneDeadlineRuleForMembersAndFriends
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint packages and testify; no new dependency. This card changes a locked table shape (TABLES.lock): Glenn's words are the amendment: \"trust but VERIFY\"; \"I want to trust the ok%\"; \"Mechanical. You know the drill.\"; \"Are they actually doing the work that is shown in the friend table? Really?\"; \"i don't want dollar amounts for friends. token counts are fine.\"

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model; reports identify Rowan and the harness. Commits may use the machine's git identity for now, but every commit body carries a line `By: Rowan` above the Co-Authored-By trailer.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/rowan-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/rowan-working/jobs/<job>/`, using `~/rowan-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md.

THE TASK. Unify the friend deadline rule of PR 5306 (the larger of 2 h and 3x her median run wall) with PR 5300 member deadline rule into one rule once both are on dev; one function, one spec paragraph, both tables. Held until PRs 5300 and 5306 land (Rowan 3:15 PM).

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/dev. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and TABLES.lock first; cite the spec from every function that implements a rule; the lock amendment quotes Glenn's words above.

STEP 2. Do the task above, writing the red test first.
  PATHS: internal/sprint/*.go,internal/sprint/*_test.go,docs/SPEC-SPRINT.md,TABLES.lock
  COMMIT: sn-deadline-rule-unifiedb: Unify the friend deadline rule of PR 5306 (the larger of 2 h and 3x he
  VERDICT: one deadline function serves members and friends; the spec names it once; the test pins both tables.

STEP 3. Obtain the shared Go-slot grant, then run `nice -n 19 env GOMAXPROCS=2 go test -p 2 ./internal/sprint -run 'TestOneDeadlineRuleForMembersAndFriends' -count=1 -timeout 600s`, then `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci`, record exact last lines, release the slot, and commit only PATHS with honest Rowan and actual model/harness attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestOneDeadlineRuleForMembersAndFriends` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.
")
      (:id "sn-tokens-update-bus-leftoversf" :tier "-" :needs () :who "friend.freddy"
      :title "Leftovers of sn-tokens-redis-bus and sn-update-redis-busb (both LAND; base origin/rowan/bus-rename until PR 5303 merges): docs/SPEC-TOKENS.md says the Redis bus for --bus <host:port>; the docs/TESTS.md transcript near line 609 (--bus ./bus)"
      :brief "RESULT: sn-tokens-update-bus-leftoversf sha=2d720d219ff5 tier: flash
REPO: mas-bandwidth/nova-tools
BASE: rowan/bus-rename
KIND: fix-red
CLASS: model
WHO: friend freddy
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: -
PATHS: docs/SPEC-TOKENS.md,docs/TESTS.md,cmd/nova-tokens/testdata/**,cmd/nova-tokens/*_test.go,internal/update/*.go,internal/ci/testdata/deleted-tests.txt
TEST: ./internal/config TestTokensBusAndUpdateSenderAreOnTheRedisBus
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify as the tree uses it; nova-bus2's client package for the Redis bus where the item moves off the git bus; no new dependency.

ATTRIBUTION: commits and reports name the friend (By: Freddy) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/freddy-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/freddy-working/jobs/<job>/`, using `~/freddy-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md.

THE TASK. Leftovers of sn-tokens-redis-bus and sn-update-redis-busb (both LAND; base origin/rowan/bus-rename until PR 5303 merges): docs/SPEC-TOKENS.md says the Redis bus for --bus <host:port>; the docs/TESTS.md transcript near line 609 (--bus ./bus) is updated; the dead fixture cmd/nova-tokens/testdata/example-bench/bus and the dead boundary_functional_test.go helpers in gitfixture_test.go are deleted with rows in internal/ci/testdata/deleted-tests.txt; nova-update bus sender is checked against the store login user as nova-bus does.

STEP 1. Set `JOB=~/freddy-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/bus-rename. Export GOCACHE=~/freddy-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the governing spec section first; cite it from every function that implements a rule.

STEP 2. Do the task above, writing the red test first.
  PATHS: docs/SPEC-TOKENS.md,docs/TESTS.md,cmd/nova-tokens/testdata/**,cmd/nova-tokens/*_test.go,internal/update/*.go,internal/ci/testdata/deleted-tests.txt
  COMMIT: sn-tokens-update-bus-leftoversf: the Redis bus in SPEC-TOKENS and TESTS, dead git-bus fixtures gone, update sender checked.
  VERDICT: the spec and transcript say the Redis bus; the fixture and helpers are gone with ledger rows; the sender check has a test; the gate is green.

STEP 3. Obtain the shared Go-slot grant, then run `nice -n 19 env GOMAXPROCS=2 go test -p 2 ./cmd/nova-tokens -run 'TestTokensBusAndUpdateSenderAreOnTheRedisBus' -count=1 -timeout 600s`, then `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/config ./internal/ci`, record exact last lines, release the slot, and commit only PATHS with honest Freddy and actual model/harness attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/freddy-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-tokens -run TestTokensBusAndUpdateSenderAreOnTheRedisBus` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "dash-always-darkb" :tier "-" :needs ()
      :title "Glenn, 2:21 PM on 2026-10-04: \"just remove the toggle light/dark"
      :brief "RESULT: dash-always-darkb sha=2d720d219ff5 tier: flash
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 90 minutes
DEPENDS-ON: -
PATHS: internal/sprintdash/**, docs/SPEC-SPRINT-DASHBOARD.md
TEST: ./internal/sprintdash TestPageHasNoThemeToggleAndStaysDark
Deadline: finish within 90 minutes.
Libraries considered: the standard library (strings, embed) and testify, already in tree; no new dependency. This card changes no locked table shape (TABLES.lock); the page's look is Glenn's call, quoted below.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model; reports identify the harness. Commits may use the machine's git identity for now, but every commit body carries a line `By: <who did the work>` above the Co-Authored-By trailer: the friend's name for a friend card (By: Rowan, By: Emma, ...), the route's model name for a fleet child (e.g. By: deepseek-v4.1-flash).

You are a child of the coordinator: one task, one checkout, one branch, unattended. This card is the whole task. Read $JOB/JOB.md first. Verify the defect still exists before editing; if already fixed report not-done with exact evidence rather than duplicate work. One change, one test that is red before and green after. Write the draft early and commit it before any extra probe.

THE TASK. Glenn, 2:21 PM on 2026-10-04: \"just remove the toggle light/dark. always dark.\" The sprint dashboard page is internal/sprintdash/page (nova-tools while PR 5309 is held; it carries to mas-bandwidth/nova-sprint). Remove the theme toggle and the light theme: in internal/sprintdash/page/index.html delete the `<button class=\"toggle\" id=\"theme\" ...>Light</button>` element (line 302 on the base), the `:root[data-theme=\"light\"] { ... }` token block (from line 45) and the comment sentence that says light is a toggle; the `<html lang=\"en\" data-theme=\"dark\">` attribute stays, and the `:root, :root[data-theme=\"dark\"]` tokens stay as the only tokens. The inline script in the head (line 8) no longer reads \"sprint-theme\" into the page; it only clears any stored value: `<script>try{localStorage.removeItem(\"sprint-theme\")}catch(e){}</script>`. In internal/sprintdash/page/app.js delete the theme block (lines 484 to 492 on the base: the comment, syncThemeButton, the click handler and its localStorage.setItem, and the syncThemeButton() call); nothing else in app.js changes. Any `.toggle` CSS rule used only by that button goes with it; one used by another element stays. Red test first: TestPageHasNoThemeToggleAndStaysDark in ./internal/sprintdash, t.Parallel, reading the embedded page files: index.html contains no `id=\"theme\"`, no `data-theme=\"light\"`, and `data-theme=\"dark\"` on the html element; app.js contains no \"sprint-theme\" and no `$(\"theme\")`; index.html's inline script contains `removeItem(\"sprint-theme\")` and no `getItem`. The live page on the Studio (~/nova-bench/dashboard/live) already has this as of 2:21 PM and may be consulted for the intended result if it exists on this machine (read-only); otherwise this paragraph is the whole spec. docs under internal/sprintdash (a README or spec_test fixture naming the toggle) change in the same commit; a doc outside PATHS that names the toggle is a proposed diff in the report.

STEP 1. Set `JOB` to the job directory this machine gives (the directory of JOB.md: `export JOB=$PWD` where JOB.md is), never a home directory; create nothing outside it, and clone inside it: `cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact branch JOB.md names from the current remote tip of origin/sprint/mechanical-2026-10-02. Export GOCACHE=$JOB/cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Scratch belongs under $JOB/scratch. Read AGENTS.md first.

STEP 2. Make it red first: write the test, run `nice -n 19 go test -count=1 -timeout 600s ./internal/sprintdash/ -run '^TestPageHasNoThemeToggleAndStaysDark$'` on the base, and keep the failing line as evidence.
  PATHS: internal/sprintdash/**, docs/SPEC-SPRINT-DASHBOARD.md
  COMMIT: dash-always-dark: the dashboard is always dark; the theme toggle and the light tokens go
  VERDICT: the page has no toggle button, no light tokens and no theme read; the inline script only clears a stored sprint-theme; data-theme stays dark; the test is red on the base and green at the head.

STEP 3. Make it green by changing only the named PATHS, minimally. Commit the draft on your own branch as soon as the test is green, before any further probe. Then run the gate: `nice -n 19 go test -count=1 -race -timeout 600s ./internal/sprintdash/` and `nice -n 19 go vet ./internal/sprintdash/`, keep the last line of each; `gofmt -l` on the changed Go files prints nothing. No Redis or external service is needed.

STEP 4. END. Finish exactly through JOB.md's protocol: push the branch JOB.md names normally, never force, and verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. The step line of RESULT.md carries the full 40-hex head (`git rev-parse HEAD`), never a short one. Report the actual gates, the red-to-green evidence, the diff stat, and what was not done. Create no pull request; the sprint consumes the branch and result.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprintdash/ -run TestPageHasNoThemeToggleAndStaysDark` passes at the head and fails on the base.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

RE-CUT NOTE. Re-cut of dash-always-dark: start from its pushed head 36debbfbbf5785700176e44c6e59dd44c8606180 (branch sprint/dash-always-dark.w1.g4.e15). The rule is Glenn's, given 2026-10-04: \"just remove the toggle light/dark. always dark.\" Update docs/SPEC-SPRINT-DASHBOARD.md (now in PATHS) to say the page is always dark with no theme toggle (Glenn, 2026-10-04), so the spec and the test agree. Do not weaken the test. Finish with the full 40-hex head.")
      (:id "sn-overload-alarm-friendsb" :tier "-" :needs ()
      :title "The overload alarm (cards timing out on a member; overload.go in PR 5283) covers friends too: a friend whose cards time out raises the same judgment as a machine, with the same numbers"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: rowan/rework-bound
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: -
PATHS: internal/sprint/*.go,internal/sprint/*_test.go,docs/SPEC-SPRINT.md,TABLES.lock,internal/sprint/store/*.go,internal/sprint/store/*_test.go
TEST: ./internal/sprint TestOverloadAlarmCoversFriends
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint packages and testify; no new dependency. This card changes a locked table shape (TABLES.lock): Glenn's words are the amendment: \"trust but VERIFY\"; \"I want to trust the ok%\"; \"Mechanical. You know the drill.\"; \"Are they actually doing the work that is shown in the friend table? Really?\"; \"i don't want dollar amounts for friends. token counts are fine.\"

ATTRIBUTION: commits and reports name the friend (By: Rowan) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/rowan-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/rowan-working/jobs/<job>/`, using `~/rowan-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md.

THE TASK. The overload alarm (cards timing out on a member; overload.go in PR 5283) covers friends too: a friend whose cards time out raises the same judgment as a machine, with the same numbers.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/rework-bound. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and TABLES.lock first; cite the spec from every function that implements a rule; the lock amendment quotes Glenn's words above.

STEP 2. Do the task above, writing the red test first.
  PATHS: internal/sprint/*.go,internal/sprint/*_test.go,docs/SPEC-SPRINT.md,TABLES.lock,internal/sprint/store/*.go,internal/sprint/store/*_test.go
  COMMIT: sn-overload-alarm-friends: The overload alarm (cards timing out on a member; overload.go in PR 52
  VERDICT: a friend with cards timing out raises the overload judgment; the test drives it through the friend row.

STEP 3. Obtain the shared Go-slot grant, then run `nice -n 19 env GOMAXPROCS=2 go test -p 2 ./internal/sprint -run 'TestOverloadAlarmCoversFriends' -count=1 -timeout 600s`, then `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci`, record exact last lines, release the slot, and commit only PATHS with honest Rowan and actual model/harness attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestOverloadAlarmCoversFriends` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

RE-CUT NOTE. Re-cut of sn-overload-alarm-friends (attempt 2 HOLD: brief defect, PATHS). Start from attempt 1's head fceb9d66a3a9a0a06f56c64cbe7e2e3c5d5b633e (branch sprint/sn-overload-alarm-friends.w1.*). PATHS now adds internal/sprint/store/*.go and internal/sprint/store/*_test.go. In the store, friendSeats (internal/sprint/store/friends.go:244) must read the friend rows whenever the roster has a friend, not only when a friend card is ready; add a store test that three timeouts in the window with nothing ready still raise the judgment. Run the gate and finish with the full 40-hex head.")
      (:id "sn-dash-container-runtime" :tier "-" :needs ("sn-functional-in-container")
      :title "Layer 8 of the container-sandboxed functional tier; counts toward nova-sprint v1.0.0"
      :brief "RESULT: sn-dash-container-runtime sha=33900515a627 tier: flash
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
DEPENDS-ON: sn-functional-in-container
PATHS: internal/sprintdash/**, docs/SPEC-SPRINT-DASHBOARD.md
TEST: internal/sprintdash TestFleetTableShowsContainerRuntime
Deadline: finish within 45 minutes.
You are a child of the coordinator: one task, one staged checkout, one branch, unattended. This card is the whole task. Read $JOB/JOB.md first. Start at the current BASE tip; admission inspected exact base 33900515a627161bc17425d8857bf74db1ab300a. Verify the column is still missing before editing; if already built report not-done with exact evidence rather than duplicate work. One change, one test that is red before and green after. Write the draft early and commit it before any extra probe.
Libraries considered: Go standard library and testify, already in tree; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> (or the route's own model trailer) as nova-tools AGENTS.md requires, naming the actual model. Commits may use the machine's git identity for now, but every commit body carries a line `By: <who did the work>` above the Co-Authored-By trailer: the friend's name for a friend card (By: Rowan), the route's model name for a fleet child (e.g. By: deepseek-v4.1-flash).

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. Layer 8 of the container-sandboxed functional tier; counts toward nova-sprint v1.0.0. mas-bandwidth/ideas#826 (https://github.com/mas-bandwidth/ideas/issues/826). Glenn, 2026-10-04 3:10 PM: \"Please add podman support for functional tests to nova-sprint\". 3:11 PM: \"We have to get this stuff reliable end-to-end before we can make a 1.0.0\". The dashboard's fleet table (internal/sprintdash, docs/SPEC-SPRINT-DASHBOARD.md) gains one column, `runtime`, per machine: the machine row's container_runtime (podman, docker or none, from card sn-machine-container-runtime) and, when card sn-functional-in-container's member row says its runtime is missing or failed its probe, that word instead (`missing`), so a machine taking no functional cards is visible at a glance. Read the value from what the dashboard already pulls (internal/sprintdash/pull.go); add no new store read if the fleet rows carry it, and if they do not, say so in the report with the field the server must add. The page stays dark by default; the column is narrow and right of the width column; the SPEC's table of columns names it.
STEP 1. Enter the staged checkout JOB.md names, no clone; inspect git status and the actual full head. Work only on its own branch. Export GOCACHE=$JOB/cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any Go command. Scratch belongs under $JOB/scratch. Read AGENTS.md and docs/SPEC-SPRINT-DASHBOARD.md; cite the spec from the change.
STEP 2. Make it red first: write TestFleetTableShowsContainerRuntime in internal/sprintdash (three machines: podman, docker, none; one member reporting missing), run go test -count=1 -timeout 600s ./internal/sprintdash/ -run '^TestFleetTableShowsContainerRuntime$' and keep the failing line as evidence.
STEP 3. Make it green by changing only the named PATHS, minimally. Commit the draft on your own branch as soon as the test is green, before any further probe; a later commit may refine it. A change any other file needs goes in the report as a proposed diff, never a commit.
STEP 4. Run the gate: go test -count=1 -race -timeout 600s ./internal/sprintdash/ and keep the last line. Run gofmt -l on every changed Go file; it must print nothing. No Redis or external service is needed; a functional test that needs Redis runs only through tools/functionalrun --fresh-gocache --deadline 15m, never a host store.
STEP 5. Inspect the diff and commit only the named files on your own branch. Present-tense comments and docs; no names of people, machines or friends. Finish exactly through JOB.md's protocol, no direct external publication; the STEP3 SHA in RESULT.md is the exact 40-hex git rev-parse HEAD. Report the actual gates, the red-to-green evidence, the diff stat, and what was not done. PR body ends with 🤖 Generated with [Claude Code](https://claude.com/claude-code). Normal independent review is required before landing; never merge yourself.")
      (:id "sn-functional-in-container" :tier "-" :needs ("sn-machine-container-runtime")
      :title "Design card, heavy, layer 7 of the container-sandboxed functional tier; counts toward nova-sprint v1.0.0"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: sn-machine-container-runtime
PATHS: cmd/nova-sprint/**,internal/sprint/**,internal/member/**,cmd/nova-swarm/nativegate.go,docs/SPEC-SPRINT.md,tla/**
TEST: ./cmd/nova-sprint TestFunctionalGateRunsInAContainerNeverBare
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint packages, internal/ci/functionalrun (card sbx-runner-verb) and testify; no new dependency. If this card changes a locked table shape (internal/sprint/TABLES.lock), Glenn's words are the amendment: \"Please add podman support for functional tests to nova-sprint\"; \"We have to get this stuff reliable end-to-end before we can make a 1.0.0\".

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model; reports identify Rowan and the harness. Commits may use the machine's git identity for now, but every commit body carries a line `By: <who did the work>` above the Co-Authored-By trailer: the friend's name for a friend card (By: Rowan), the route's model name for a fleet child (e.g. By: deepseek-v4.1-flash).

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/rowan-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/rowan-working/jobs/<job>/`, using `~/rowan-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md.

THE TASK. Design card, heavy, layer 7 of the container-sandboxed functional tier; counts toward nova-sprint v1.0.0. (Home: nova-tools while PR 5309 is held; when it lands the home is mas-bandwidth/nova-sprint main and the card is re-pointed.) mas-bandwidth/ideas#826 (https://github.com/mas-bandwidth/ideas/issues/826). Glenn, 2026-10-04 3:09 PM: \"functional tests should probably be sandboxed (like we talked about) using say, podman.\" 3:10 PM: \"Please add podman support for functional tests to nova-sprint\". 3:11 PM: \"We have to get this stuff reliable end-to-end before we can make a 1.0.0\". BASE is the integration branch because the lander's tree gate (cmd/nova-sprint/landgo.go, gateRuns, added in d77b57b3d) is on dev and integration and not yet on sprint/mechanical-2026-10-02.

Wherever the sprint runs a functional tier, it runs through the runner of card sbx-runner-verb (internal/ci/functionalrun, `nova-ci functional --in-container`) in one container on the machine's runtime, never on the bare machine:
1. The lander: the tree gate (landgo.go gateRuns, gateCard) and the batch --check (land.go runCheck) when a card's TEST names a functional test (a test in a `//go:build functional` file), or its gate names the tier (-tags functional, test-functional, test-functional-container), run that part in a container on the lander's machine; the unit part stays as it is.
2. The member's post step: where a member (internal/member, cmd/nova-swarm/nativegate.go) runs a card's TEST after the child, a functional TEST runs in a container the same way.
3. The land self-test: any land-path test that is functional (see sprint-next card land-tests-to-functional-tierb, which moves the lander's real-git tests behind the functional tag) runs only in a container.
4. A member whose machine row says container_runtime none or unset (card sn-machine-container-runtime), or whose runtime fails a start probe, says so on its member row (`functional=no-runtime` or `functional=probe-failed <why>`) and is dealt no functional card: the deal's filter reads the card (its TEST or gate names functional) and the member's runtime. A functional card with no capable member waits with a reason on its row, never runs bare.
5. The model: extend the deal model (tla/CardMachine.tla, or a new tla/FunctionalDeal.tla with MC cfg) with the invariant that no functional card is dealt to a member without a runtime and no functional gate runs outside a container, plus a reversed witness (a Broken cfg TLC refutes) and a tla/RUNS.tsv row; TLC on a bench machine, never the Studio; check every counterexample against the code by hand; cite the model from the code.
Write the rule in docs/SPEC-SPRINT.md with Glenn's words.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04, then merge the landed heads of sbx-runner-verb and sn-machine-container-runtime if the base does not carry them yet (name the merges in the report). Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md, docs/SPEC-CI.md and internal/sprint/TABLES.lock first; cite the spec from every function that implements a rule.

STEP 2. Do the task above, writing the red test first: a fake engine records every argv, and a functional gate on a card is asserted to go through it, never through a bare `go test -tags functional`.
  PATHS: cmd/nova-sprint/**,internal/sprint/**,internal/member/**,cmd/nova-swarm/nativegate.go,docs/SPEC-SPRINT.md,tla/**
  COMMIT: sn-functional-in-container: the sprint's functional gates run in a container; no runtime, no functional card
  VERDICT: lander tree gate, --check, member post step and land self-test run functional tests only in a container; a member without a runtime says so and is dealt no functional card; TLC green and its Broken cfg red; SPEC-SPRINT says it.

STEP 3. Obtain the shared Go-slot grant, then run `nice -n 19 env GOMAXPROCS=2 go test -p 2 ./cmd/nova-sprint -run 'TestFunctionalGateRunsInAContainerNeverBare' -count=1 -timeout 600s`, then `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint ./internal/member ./internal/ci`, record exact last lines, release the slot, and commit only PATHS with honest Rowan and actual model/harness attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestFunctionalGateRunsInAContainerNeverBare` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sn-machine-container-runtime" :tier "-" :needs ("sbx-ci-legs")
      :title "Layer 6 of the container-sandboxed functional tier, the first nova-sprint layer; counts toward nova-sprint v1.0.0"
      :brief "RESULT: sn-machine-container-runtime sha=33900515a627 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
DEPENDS-ON: sbx-ci-legs
PATHS: internal/config/**, cmd/nova-config/**, docs/SPEC-CONFIG.md
TEST: internal/config TestMachineRowCarriesContainerRuntime
Deadline: finish within 60 minutes.
You are a child of the coordinator: one task, one staged checkout, one branch, unattended. This card is the whole task. Read $JOB/JOB.md first. Start at the current BASE tip; admission inspected exact base 33900515a627161bc17425d8857bf74db1ab300a. Verify the field is still missing before editing; if already built report not-done with exact evidence rather than duplicate work. One change, one test that is red before and green after. Write the draft early and commit it before any extra probe.
Libraries considered: Go standard library and testify, already in tree; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> (or the route's own model trailer) as nova-tools AGENTS.md requires, naming the actual model. Commits may use the machine's git identity for now, but every commit body carries a line `By: <who did the work>` above the Co-Authored-By trailer: the friend's name for a friend card (By: Rowan), the route's model name for a fleet child (e.g. By: deepseek-v4.1-flash).

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

THE TASK. Layer 6 of the container-sandboxed functional tier, the first nova-sprint layer; counts toward nova-sprint v1.0.0. mas-bandwidth/ideas#826 (https://github.com/mas-bandwidth/ideas/issues/826). Glenn, 2026-10-04 3:10 PM: \"Please add podman support for functional tests to nova-sprint\". 3:11 PM: \"We have to get this stuff reliable end-to-end before we can make a 1.0.0\". The nova-config machine kind (internal/config/kind.go, KindMachine, its Fields list beside `width`) gains one field, `container_runtime`: an enum of exactly `podman`, `docker` or `none`, nullable, cleared with `default`; unset reads as `none` (no runtime stated is no runtime: the sprint deals that machine no functional card). Its help says so in one sentence and names ideas#826. `nova-config` sets it like the other machine fields (`--container-runtime podman|docker|none|default`); any other value is refused in one line naming the three. Add a reader beside Widths in internal/config (ContainerRuntimes or a method on the machine row) that the sprint will call in card sn-functional-in-container. docs/SPEC-CONFIG.md gets the field in the machine kind's table. The value is what a person states for the machine (ansible installs the runtime, fleet/roles/container-runtime); nothing probes or guesses it here.
STEP 1. Enter the staged checkout JOB.md names, no clone; inspect git status and the actual full head. Work only on its own branch. Export GOCACHE=$JOB/cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any Go command. Scratch belongs under $JOB/scratch. Read AGENTS.md and docs/SPEC-CONFIG.md; cite the spec from the change.
STEP 2. Make it red first: write TestMachineRowCarriesContainerRuntime in internal/config (podman, docker, none set and read back; unset reads none; `rkt` refused; `default` clears), run go test -count=1 -timeout 600s ./internal/config/ -run '^TestMachineRowCarriesContainerRuntime$' and keep the failing line as evidence.
STEP 3. Make it green by changing only the named PATHS, minimally. Commit the draft on your own branch as soon as the test is green, before any further probe; a later commit may refine it. A change any other file needs goes in the report as a proposed diff, never a commit.
STEP 4. Run the gate: go test -count=1 -race -timeout 600s ./internal/config/ ./cmd/nova-config/ and keep the last line of each. Run gofmt -l on every changed Go file; it must print nothing. No Redis or external service is needed; a functional test that needs Redis or Postgres runs only through tools/functionalrun --fresh-gocache --deadline 15m, never a host store.
STEP 5. Inspect the diff and commit only the named files on your own branch. Present-tense comments and docs; no names of people, machines or friends. Finish exactly through JOB.md's protocol, no direct external publication; the STEP3 SHA in RESULT.md is the exact 40-hex git rev-parse HEAD. Report the actual gates, the red-to-green evidence, the diff stat, and what was not done. PR body ends with 🤖 Generated with [Claude Code](https://claude.com/claude-code). Normal independent review is required before landing; never merge yourself.")))
    (:stream "sprint-v1-processor" :cards
     ((:id "wait-replaces-sentinel" :tier "-" :needs ("processor-counters" "isa-kind-line")
      :title "Layer 3 of the processor, the reduction: make hold, sentinel and wave one wait in the code, as docs/SPEC-ISA.md already says on paper"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: processor-counters,isa-kind-line
PATHS: internal/sprint/steps_sentinel.go,internal/sprint/held.go,internal/sprint/needs*.go,internal/sprint/sentinel*_test.go,internal/sprint/held*_test.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md
SHARED: internal/sprint/held.go,internal/sprint/needs*.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md
TEST: ./internal/sprint TestHoldSentinelAndWaveAreOneWait
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint, swarm and tla packages, testify, and TLC from tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

CONTEXT. Glenn accepted the processor design on 2026-10-04: nova-sprint is a processor, cards are its instructions, and the coordinator is the front end that issues work and handles exceptions, never executes it. The design is built in eight layers, bottom up, each secured (TLA+ module with reversed witnesses, tests, lock) before the next starts. THE SIMPLICITY TEST gates every layer: a feature enters code only if it (a) removes concepts or code that exist today, (b) shows a measured gain in cost, wall clock or reliability from the counters, or (c) makes the machine easier to model and check in TLA+. Anything that is only metaphor goes in docs/PROCESSOR.md, not in code.

THE TASK. Layer 3 of the processor, the reduction: make hold, sentinel and wave one wait in the code, as docs/SPEC-ISA.md already says on paper. A held card is a wait on a release; a sentinel is a card whose wait is a release and that others need; a wave loading behind a held sentinel is cards needing that wait. One code path decides \"waits, and why\" for all of them, and steps_sentinel.go's special cases go. The verbs keep their spelling for callers. The existing sentinel and held tests keep passing unchanged (they are the contract), and the new test drives the three through one function.

GATE. (a) removes concepts and code: the report gives lines deleted versus added in internal/sprint (net negative required) and the number of wait paths before and after.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, writing the red test first. Keep the add --held, --sentinel and release verbs working for their callers as spellings of wait; delete the special paths behind them.
  PATHS: internal/sprint/steps_sentinel.go,internal/sprint/held.go,internal/sprint/needs*.go,internal/sprint/sentinel*_test.go,internal/sprint/held*_test.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md
  COMMIT: wait-replaces-sentinel: hold, sentinel and wave are one wait in the code
  VERDICT: every existing sentinel and held test passes unchanged, and internal/sprint is smaller.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestHoldSentinelAndWaveAreOneWait' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, which of (a), (b), (c) it met with the evidence, and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestHoldSentinelAndWaveAreOneWait` passes at the head, and `git diff --stat` over internal/sprint at the head versus the base is net negative in non-test lines (the (a) check), with the existing sentinel and held tests unchanged and green.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "processor-counters" :tier "-" :needs ("isa-tla" "cycle-time-breakdown")
      :title "Layer 2 of the processor: performance counters, built on cycle-time-breakdown's per-card stage stamps (do not duplicate them)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: isa-tla,cycle-time-breakdown
PATHS: internal/sprint/cycletime*.go,internal/sprint/counters*.go,internal/sprint/store/where.go,internal/sprintdash/dashboard.go,docs/SPEC-SPRINT.md,tla/*CardISA*,tla/*.tsv
SHARED: docs/SPEC-SPRINT.md,tla/*.tsv
TEST: ./internal/sprint TestCountersGiveIPCAndStallReasonsThatSumToWallTime
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint, swarm and tla packages, testify, and TLC from tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

CONTEXT. Glenn accepted the processor design on 2026-10-04: nova-sprint is a processor, cards are its instructions, and the coordinator is the front end that issues work and handles exceptions, never executes it. The design is built in eight layers, bottom up, each secured (TLA+ module with reversed witnesses, tests, lock) before the next starts. THE SIMPLICITY TEST gates every layer: a feature enters code only if it (a) removes concepts or code that exist today, (b) shows a measured gain in cost, wall clock or reliability from the counters, or (c) makes the machine easier to model and check in TLA+. Anything that is only metaphor goes in docs/PROCESSOR.md, not in code.

THE TASK. Layer 2 of the processor: performance counters, built on cycle-time-breakdown's per-card stage stamps (do not duplicate them). From those stamps, where --json gets per stream and overall: IPC as landed cards per slot-hour (slots from the fleet widths over the window), and for each card the time it stalled by reason (waiting on a need, waiting on a release, waiting on an external operand, waiting for a slot, in read, in merge, in rework), whose parts sum to its wall time. The dashboard gets one row: IPC and the top stall reason. The counters are the evidence the later layers are gated on (speculation and renaming are kept only if these show their stall dominating). Extend tla/CardISA.tla with the stall-reason variable and the invariant StallsPartitionWallTime (one reason at a time), with a reversed witness.

GATE. (b) measured gain: this layer is the instrument the (b) gate of every later layer reads; it earns its place by the design's list. (c) the stall-reason partition is one invariant in CardISA.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, writing the red test first. Extend the stage-time stamps of cycle-time-breakdown (landed before this card is dealt); add no second store of times.
  PATHS: internal/sprint/cycletime*.go,internal/sprint/counters*.go,internal/sprint/store/where.go,internal/sprintdash/dashboard.go,docs/SPEC-SPRINT.md,tla/*CardISA*,tla/*.tsv
  COMMIT: processor-counters: IPC and stall reasons from the stage stamps
  VERDICT: on the twin store, IPC matches a hand count and each card's stall parts sum to its wall time.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestCountersGiveIPCAndStallReasonsThatSumToWallTime' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, which of (a), (b), (c) it met with the evidence, and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestCountersGiveIPCAndStallReasonsThatSumToWallTime` passes at the head, and on a twin-store fixture of known cards the stall parts of every card sum to its wall time and IPC equals the hand count (the (b) instrument check).

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "rename-shared-paths" :tier "-" :needs ("processor-counters" "merge-tree-switch")
      :title "ON PROBATION"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: processor-counters,merge-tree-switch
PATHS: internal/sprint/needs*.go,internal/sprint/sharedpaths*.go,cmd/nova-sprint/add*.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
SHARED: internal/sprint/needs*.go,cmd/nova-sprint/add*.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
TEST: ./internal/sprint TestCardsSharingPathsRunInParallelAndResolveAtRetire
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint, swarm and tla packages, testify, and TLC from tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

CONTEXT. Glenn accepted the processor design on 2026-10-04: nova-sprint is a processor, cards are its instructions, and the coordinator is the front end that issues work and handles exceptions, never executes it. The design is built in eight layers, bottom up, each secured (TLA+ module with reversed witnesses, tests, lock) before the next starts. THE SIMPLICITY TEST gates every layer: a feature enters code only if it (a) removes concepts or code that exist today, (b) shows a measured gain in cost, wall clock or reliability from the counters, or (c) makes the machine easier to model and check in TLA+. Anything that is only metaphor goes in docs/PROCESSOR.md, not in code.

THE TASK. ON PROBATION. Layer 6 of the processor: register renaming, on top of the merge tree (merge-tree-node, merge-tree-root, merge-tree-shadow, merge-tree-switch in sprint-v1-yes, landed before this card is dealt; this card adds nothing to them). Cards that share PATHS and have no real data dependency run in parallel on their own branches (they already have per-attempt branches) and resolve at retire: the merge tree lands them in order, and a conflict at merge sends the later card back for a replay on the new base. The SHARED: line and the shared-paths serialization it forces are deleted.

GATE. (a) removes concepts, on probation: only if it deletes the DEPENDS-ON chains and the SHARED: handling that exist just because of shared PATHS; the DONE-WHEN checks the deletion first.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, writing the red test first. First count, on the live table, the DEPENDS-ON edges that exist only because two cards share PATHS (the SHARED: lines and add's shared-paths refusal). If they are not a measurable share of wait time in the counters, write Verdict: HOLD with the figures and the sentence \"on probation: drop\", and change nothing.
  PATHS: internal/sprint/needs*.go,internal/sprint/sharedpaths*.go,cmd/nova-sprint/add*.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
  COMMIT: rename-shared-paths: cards sharing PATHS run in parallel and resolve at retire
  VERDICT: either HOLD with the counts showing it is not justified, or: the SHARED: handling is deleted and two cards sharing a file land in order on the twin store, the second replayed on a conflict.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestCardsSharingPathsRunInParallelAndResolveAtRetire' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, which of (a), (b), (c) it met with the evidence, and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: the counters show waits forced by shared PATHS are a measurable share of wait time; otherwise drop (Verdict: HOLD, nothing changed). When they do: the SHARED: handling is deleted, net negative lines, figures in the report. Then `go test -count=1 -timeout 600s ./internal/sprint -run TestCardsSharingPathsRunInParallelAndResolveAtRetire` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "spend-governor" :tier "-" :needs ("spend-circuit-breaker" "vector-cards")
      :title "Layer 8 of the processor: the spend governor, which is the spend circuit breaker with per-tier budgets"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: spend-circuit-breaker,vector-cards
PATHS: internal/sprint/route_rest.go,internal/sprint/cost.go,internal/sprint/spend*.go,internal/config/kind.go,internal/config/migrations/*.sql,docs/SPEC-SPRINT.md,docs/SPEC-CONFIG.md
SHARED: internal/sprint/route_rest.go,internal/sprint/cost.go,internal/config/kind.go,docs/SPEC-SPRINT.md,docs/SPEC-CONFIG.md
TEST: ./internal/sprint TestTheGovernorThrottlesATierOnItsSpendRate
Deadline: finish within 150 minutes.
Libraries considered: the tree's own sprint, swarm and tla packages, testify, and TLC from tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

CONTEXT. Glenn accepted the processor design on 2026-10-04: nova-sprint is a processor, cards are its instructions, and the coordinator is the front end that issues work and handles exceptions, never executes it. The design is built in eight layers, bottom up, each secured (TLA+ module with reversed witnesses, tests, lock) before the next starts. THE SIMPLICITY TEST gates every layer: a feature enters code only if it (a) removes concepts or code that exist today, (b) shows a measured gain in cost, wall clock or reliability from the counters, or (c) makes the machine easier to model and check in TLA+. Anything that is only metaphor goes in docs/PROCESSOR.md, not in code.

THE TASK. Layer 8 of the processor: the spend governor, which is the spend circuit breaker with per-tier budgets. Beside the per-route hourly cap, each tier (flash, pro, heavy, frontier) gets a budget per hour in nova-config; as a tier's spend rate approaches its budget the tick narrows that tier's dealing (fewer slots, never below one) rather than stopping it, and at the budget it rests as the breaker rests a route, with one judgment per episode. The throttle is the breaker's cap logic with a second key, not a second mechanism.

GATE. (b) measured gain in cost: replaying the last 24 h of costs, spend per hour stays under each tier's budget, figures in the report. (a) the per-route cap and the per-tier budget share one code path.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, writing the red test first. Extend the per-route caps of spend-circuit-breaker (landed before this card is dealt); add no second mechanism.
  PATHS: internal/sprint/route_rest.go,internal/sprint/cost.go,internal/sprint/spend*.go,internal/config/kind.go,internal/config/migrations/*.sql,docs/SPEC-SPRINT.md,docs/SPEC-CONFIG.md
  COMMIT: spend-governor: per-tier budgets throttle dealing on spend rate
  VERDICT: on the twin store a tier past its budget throttles then rests with one judgment; the per-route caps still behave as before.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestTheGovernorThrottlesATierOnItsSpendRate' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci ./internal/config`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, which of (a), (b), (c) it met with the evidence, and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestTheGovernorThrottlesATierOnItsSpendRate` passes at the head, and a replay of a day of recorded costs keeps every tier under its budget (the (b) check) and the breaker's own tests pass unchanged.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "route-predictor-shadow" :tier "-" :needs ("external-depends-on" "wait-replaces-sentinel")
      :title "Layer 4 of the processor: the route and tier predictor, in shadow"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: external-depends-on,wait-replaces-sentinel
PATHS: internal/sprint/predict*.go,internal/sprint/decide.go,internal/sprint/store/where.go,docs/SPEC-SPRINT.md,docs/SPEC-NOVA-DECIDE.md
SHARED: internal/sprint/decide.go,docs/SPEC-SPRINT.md,docs/SPEC-NOVA-DECIDE.md
TEST: ./internal/sprint TestThePredictorRecordsBesideTheDealAndChangesNothing
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint, swarm and tla packages, testify, and TLC from tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

CONTEXT. Glenn accepted the processor design on 2026-10-04: nova-sprint is a processor, cards are its instructions, and the coordinator is the front end that issues work and handles exceptions, never executes it. The design is built in eight layers, bottom up, each secured (TLA+ module with reversed witnesses, tests, lock) before the next starts. THE SIMPLICITY TEST gates every layer: a feature enters code only if it (a) removes concepts or code that exist today, (b) shows a measured gain in cost, wall clock or reliability from the counters, or (c) makes the machine easier to model and check in TLA+. Anything that is only metaphor goes in docs/PROCESSOR.md, not in code.

THE TASK. Layer 4 of the processor: the route and tier predictor, in shadow. At each deal it records what it would have chosen (route and tier, from past landings of the same kind) beside what the escalation rule chose, and at the card's end whether its choice would have landed cheaper. where --json shows its misprediction rate and its cost per landed card against the rule's, over the last 24 h. It lives where SPEC-ISA.md and nova-decide put it (the dealer calls it); it never changes a deal. Promotion out of shadow is a separate card, cut only when the counters show it beating the escalation rule on cost per landed card.

GATE. (b) measured gain, not yet claimed: shadow only, until it beats the simple escalation rule on cost per landed card; this card builds the measurement and nothing else.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, writing the red test first. Shadow only: no code path lets the prediction change a deal; a test asserts it.
  PATHS: internal/sprint/predict*.go,internal/sprint/decide.go,internal/sprint/store/where.go,docs/SPEC-SPRINT.md,docs/SPEC-NOVA-DECIDE.md
  COMMIT: route-predictor-shadow: the route predictor records beside the deal and changes nothing
  VERDICT: on the twin store, deals are identical with the predictor on and off, and where --json shows its misprediction rate.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestThePredictorRecordsBesideTheDealAndChangesNothing' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, which of (a), (b), (c) it met with the evidence, and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestThePredictorRecordsBesideTheDealAndChangesNothing` passes at the head, and deals are byte-identical with the predictor on and off on the twin store (shadow only), and where --json reports its cost per landed card beside the rule's (the (b) measurement).

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "speculative-dispatch" :tier "-" :needs ("processor-counters" "route-predictor-shadow")
      :title "ON PROBATION"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: processor-counters,route-predictor-shadow
PATHS: internal/sprint/specul*.go,internal/sprint/needs*.go,internal/sprint/steps_merge.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
SHARED: internal/sprint/needs*.go,internal/sprint/steps_merge.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
TEST: ./internal/sprint TestASpeculativeCardIsSquashedWhenItsNeedBounces
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint, swarm and tla packages, testify, and TLC from tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

CONTEXT. Glenn accepted the processor design on 2026-10-04: nova-sprint is a processor, cards are its instructions, and the coordinator is the front end that issues work and handles exceptions, never executes it. The design is built in eight layers, bottom up, each secured (TLA+ module with reversed witnesses, tests, lock) before the next starts. THE SIMPLICITY TEST gates every layer: a feature enters code only if it (a) removes concepts or code that exist today, (b) shows a measured gain in cost, wall clock or reliability from the counters, or (c) makes the machine easier to model and check in TLA+. Anything that is only metaphor goes in docs/PROCESSOR.md, not in code.

THE TASK. ON PROBATION. Layer 5 of the processor: speculative dispatch. A card whose only unmet need is in review or merging may be dealt as if that need will land, on its need's head; if the need bounces (rework or drop), the speculative card is squashed (its attempt withdrawn, its branch left, its cost counted as flush) and dealt again when the need lands. Model it first in CardISA: a squashed card never retires (NoSpeculativeRetire), and a speculative card retires only after its need retired on the same head; reversed witnesses for both.

GATE. (b) measured gain, on probation: only if the counters show read and merge wait dominating cycle time; the DONE-WHEN checks it before anything else. (c) is not claimed: speculation makes the model larger.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, writing the red test first. First read the counters on the live table (where --json, the last 24 h). If read and merge wait do not dominate cycle time, write Verdict: HOLD with the figures and the sentence \"on probation: the counters do not justify it; drop\", and change nothing.
  PATHS: internal/sprint/specul*.go,internal/sprint/needs*.go,internal/sprint/steps_merge.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
  COMMIT: speculative-dispatch: deal past a need in review, squash on bounce
  VERDICT: either HOLD with the counter figures showing it is not justified, or: on the twin store a speculative card lands after its need, and is squashed and redealt when the need bounces.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestASpeculativeCardIsSquashedWhenItsNeedBounces' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, which of (a), (b), (c) it met with the evidence, and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: the counters show read and merge wait dominate cycle time (over half of median wall time over the last 24 h, figures in the report); otherwise drop (Verdict: HOLD, nothing changed). When they do: NoSpeculativeRetire holds in TLC with its reversed witness refused. Then `go test -count=1 -timeout 600s ./internal/sprint -run TestASpeculativeCardIsSquashedWhenItsNeedBounces` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "vector-cards" :tier "-" :needs ("merge-tree-switch" "processor-counters")
      :title "Layer 7 of the processor: vector cards"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: merge-tree-switch,processor-counters
PATHS: internal/sprint/vector*.go,internal/sprint/steps_merge.go,cmd/nova-sprint/add*.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
SHARED: internal/sprint/steps_merge.go,cmd/nova-sprint/add*.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
TEST: ./internal/sprint TestAVectorCardRetiresEachSubCardOnItsOwnVerdict
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint, swarm and tla packages, testify, and TLC from tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

CONTEXT. Glenn accepted the processor design on 2026-10-04: nova-sprint is a processor, cards are its instructions, and the coordinator is the front end that issues work and handles exceptions, never executes it. The design is built in eight layers, bottom up, each secured (TLA+ module with reversed witnesses, tests, lock) before the next starts. THE SIMPLICITY TEST gates every layer: a feature enters code only if it (a) removes concepts or code that exist today, (b) shows a measured gain in cost, wall clock or reliability from the counters, or (c) makes the machine easier to model and check in TLA+. Anything that is only metaphor goes in docs/PROCESSOR.md, not in code.

THE TASK. Layer 7 of the processor: vector cards. One deal carries N sub-cards (the vector kind SPEC-ISA.md reserved); the worker commits once per sub-card and reports a verdict per sub-card; the merge tree retires each sub-card on its own verdict, so one bad sub-card is held without holding the rest. The tracer is the Opus batch merges done by hand today: the report shows one of them run as a vector card.

GATE. (b) measured gain: the report gives cost and wall time per landed sub-card against the same work as single cards (from the counters). (a) the hand-run batch merges become one kind.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, writing the red test first. Model first in CardISA: a vector card retires its sub-cards one by one, and each sub-card retires at most once.
  PATHS: internal/sprint/vector*.go,internal/sprint/steps_merge.go,cmd/nova-sprint/add*.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
  COMMIT: vector-cards: one deal, N sub-cards, a commit and verdict each
  VERDICT: on the twin store a vector of three sub-cards with one red lands two and holds one; the report has the per-sub-card cost against single cards.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestAVectorCardRetiresEachSubCardOnItsOwnVerdict' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, which of (a), (b), (c) it met with the evidence, and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestAVectorCardRetiresEachSubCardOnItsOwnVerdict` passes at the head, and the report shows cost per landed sub-card below the single-card cost for the same kind from the counters (the (b) check), and CardISA's per-sub-card retire invariant holds with its reversed witness refused.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "external-depends-on" :tier "-" :needs ("processor-counters")
      :title "Layer 3 of the processor: external operands"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: processor-counters
PATHS: internal/sprint/needs*.go,internal/sprint/external*.go,internal/sprint/tick*.go,internal/swarm/lintdepends.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
SHARED: internal/sprint/needs*.go,cmd/nova-sprint/add*.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
TEST: ./internal/sprint TestAnExternalDependsOnReleasesOnTheTickWhenItHolds
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint, swarm and tla packages, testify, and TLC from tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

CONTEXT. Glenn accepted the processor design on 2026-10-04: nova-sprint is a processor, cards are its instructions, and the coordinator is the front end that issues work and handles exceptions, never executes it. The design is built in eight layers, bottom up, each secured (TLA+ module with reversed witnesses, tests, lock) before the next starts. THE SIMPLICITY TEST gates every layer: a feature enters code only if it (a) removes concepts or code that exist today, (b) shows a measured gain in cost, wall clock or reliability from the counters, or (c) makes the machine easier to model and check in TLA+. Anything that is only metaphor goes in docs/PROCESSOR.md, not in code.

THE TASK. Layer 3 of the processor: external operands. DEPENDS-ON takes, beside card ids, three external forms: `pr <repo>#<n> merged`, `<branch> contains <sha>`, `after <RFC3339>`. A card with one is admitted, waits, and is released by the tick (the interrupt controller) the first tick its operand holds, with where and the dashboard saying what it waits for (\"waits for nova-tools#5303 merged\"); the coordinator never re-checks by hand. A malformed operand is refused at add and at lint with the remedy. The operand is the wait kind's operand from docs/SPEC-ISA.md, not a new mechanism. The tick checks each distinct operand once per tick (one gh or git call per operand, not per card) and caches the answer for that tick.

GATE. (a) removes concepts: the hand-polled waits the coordinator keeps today (held cards released by hand when a PR merges) become one operand of wait; the report names at least one current held card this replaces. (c) the operand is one guard in CardISA.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, writing the red test first. Model first: extend tla/CardISA.tla's wait operand with the external forms before the code.
  PATHS: internal/sprint/needs*.go,internal/sprint/external*.go,internal/sprint/tick*.go,internal/swarm/lintdepends.go,docs/SPEC-SPRINT.md,docs/SPEC-ISA.md,tla/*CardISA*,tla/*.tsv
  COMMIT: external-depends-on: DEPENDS-ON takes pr merged, branch contains, and after operands released by the tick
  VERDICT: on the twin store with a fake PR source, a card waiting on pr X merged is released on the first tick after X merges and not before; a bad operand is refused at add.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestAnExternalDependsOnReleasesOnTheTickWhenItHolds' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/ci ./internal/swarm`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, which of (a), (b), (c) it met with the evidence, and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestAnExternalDependsOnReleasesOnTheTickWhenItHolds` passes at the head, and a card with each of the three forms is released by the tick exactly when its operand holds, and CardISA's WaitDispatchesOnlyWhenOperandHolds covers the external form with a reversed witness (the (a) and (c) checks).

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-docs" :cards
     ((:id "sprint-flagship-readme" :tier "-" :needs ("sprint-brand-sheet") :who "friend.stella"
      :title "Glenn, 2026-10-04 4:54 PM ET: \"I want Stella to design the branding and write the README.md.\" Write nova-sprint's flagship README in the brand of your sheet (sprint-brand-sheet, docs/sprint/BRAND.md)"
      :brief "RESULT: sprint-flagship-readme tier: pro
REPO: mas-bandwidth/nova-tools
BASE: rowan/integration-2026-10-04
KIND: fix-red
CLASS: model
WHO: friend stella
LEG: go
DEADLINE: finish within 240 minutes
DEPENDS-ON: sprint-brand-sheet
PATHS: docs/sprint/README.md,assets/sprint/**,docs/ASSET-PROVENANCE.md,internal/ci/sprint_readme_class_test.go
TEST: ./internal/ci TestSprintReadmeIsTheFlagship
Deadline: finish within 240 minutes.
Libraries considered: none for the prose; the class test uses the standard library and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: Stella) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/stella-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/stella-working/jobs/<job>/`, using `~/stella-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md.

THE TASK. Glenn, 2026-10-04 4:54 PM ET: \"I want Stella to design the branding and write the README.md.\" Write nova-sprint's flagship README in the brand of your sheet (sprint-brand-sheet, docs/sprint/BRAND.md). It is yours to write; it needs, in whatever order and voice you choose: what nova-sprint is (the opinionated work processor for teams of AIs, built from nova-tools), why someone would use it, a 60-second tour with a screenshot of the dark dashboard, install, a first sprint (the twin store, `--redis mem:<file>`, needs no Redis: nova-sprint help shows it), and links to the doc suite (getting started, concepts, coordinator guide and runbook, friends and buds guide, card authoring, the CLI reference, the dashboard guide, operations, FAQ, glossary, the specs; a link to a page not yet written points at docs/sprint/ and is listed in your report). The screenshot: take it from the dashboard Glenn made public (ask the coordinator over the bus for its address), never by starting a server here; record it in docs/ASSET-PROVENANCE.md with its date and source. Sources: the seed README of mas-bandwidth/nova-sprint, nova-tools README.md (its table of \"What do you want to do?\"), mas-bandwidth/nova README.md, docs/SPEC-SPRINT.md, docs/SPRINT-COORDINATOR.md, docs/SPEC-SPRINT-DASHBOARD.md. LAYOUT: the code lives in nova-tools today, so the README lands as docs/sprint/README.md and becomes nova-sprint's root README.md at the split (plan: /Volumes/nova/ai/rowan/working/rowan-new/tmp/nova-sprint-split/PLAN.md). The red test first: internal/ci/sprint_readme_class_test.go fails while docs/sprint/README.md lacks the tour, install and first-sprint sections, a relative link does not resolve, or an image has no provenance row. Edit only the files PATHS names; a change that needs another file is a HOLD naming it.

STEP 1. Set `JOB=~/stella-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/stella-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/sprint/README.md,assets/sprint/**,docs/ASSET-PROVENANCE.md,internal/ci/sprint_readme_class_test.go
  COMMIT: sprint-flagship-readme: the nova-sprint flagship README in the brand
  VERDICT: docs/sprint/README.md carries what it is, why, the 60-second tour with the dashboard screenshot, install, the first sprint and the links to the suite; every relative link resolves and every image has a provenance row.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestSprintReadmeIsTheFlagship' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/stella-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestSprintReadmeIsTheFlagship` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sdocs-stella-prose-pass" :tier "-" :needs ("sdocs-processor" "tdocs-glossary-terminology-lint") :who "friend.stella"
      :title "Glenn, 2026-10-04: \"I want Stella to design the branding and write the README.md.\" The suite was written by several hands after your brand sheet and flagship README; this card is your final pass over all of it, Stella, so it reads as one vo"
      :brief "RESULT: sdocs-stella-prose-pass tier: pro
REPO: mas-bandwidth/nova-tools
BASE: rowan/integration-2026-10-04
KIND: fix-red
CLASS: model
WHO: friend stella
LEG: go
DEADLINE: finish within 240 minutes
DEPENDS-ON: sdocs-processor,tdocs-glossary-terminology-lint
PATHS: docs/sprint/*.md,assets/sprint/**,docs/ASSET-PROVENANCE.md
SHARED: docs/ASSET-PROVENANCE.md
TEST: ./internal/docs TestSprintDocSuitePagesExistLinkAndNameRealVerbs
Deadline: finish within 240 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: Stella) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/stella-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/stella-working/jobs/<job>/`, using `~/stella-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md.

THE TASK. Glenn, 2026-10-04: \"I want Stella to design the branding and write the README.md.\" The suite was written by several hands after your brand sheet and flagship README; this card is your final pass over all of it, Stella, so it reads as one voice, yours. Read every page under docs/sprint/ (INDEX, GETTING-STARTED, CONCEPTS, FAQ, COORDINATOR-GUIDE, FRIENDS-AND-BUDS, CARD-AUTHORING, RUNBOOK, CLI, DASHBOARD, OPERATIONS, SPECS, PROCESSOR, GLOSSARY) and edit the prose: voice, rhythm, order, headings, cuts, your art where a page wants it (original only, each with its docs/ASSET-PROVENANCE.md row in the same commit). Change no fact, command, verb, flag or number: a fact you believe is wrong goes in REPORT.md with its page and line for a fix card, never edited here. Leave the generated blocks of docs/sprint/CLI.md (between the clidoc markers) as they are. The docs tests are already in the tree; they must stay green. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/ASSET-PROVENANCE.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/stella-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/stella-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/sprint/*.md,assets/sprint/**,docs/ASSET-PROVENANCE.md
  COMMIT: sdocs-stella-prose-pass: the final prose pass over the nova-sprint doc suite
  VERDICT: every page of docs/sprint/ reads in one voice, the brand's, with no fact changed; the suite, runbook, processor, CLI and brand tests stay green.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/docs -run 'TestSprintDocSuitePagesExistLinkAndNameRealVerbs' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/docs ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/stella-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/docs -run TestSprintDocSuitePagesExistLinkAndNameRealVerbs` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sdocs-suite-reference" :tier "-" :needs ("sdocs-coordinator-runbook" "tdocs-cli-generated")
      :title "nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 240 minutes
DEPENDS-ON: sdocs-coordinator-runbook,tdocs-cli-generated
PATHS: docs/sprint/INDEX.md,docs/sprint/CLI.md,docs/sprint/DASHBOARD.md,docs/sprint/OPERATIONS.md,docs/sprint/SPECS.md,internal/docs/sprint_cli_test.go
SHARED: docs/sprint/INDEX.md
TEST: ./internal/docs TestSprintCLIReferenceIsGeneratedFromHelp
Deadline: finish within 240 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand. The README.md, full doc suite.\"). Write the reference pages. docs/sprint/CLI.md: nova-sprint's whole command reference, generated by tools/clidoc (card tdocs-cli-generated, which this follows) into its markers, with short hand-written groupings around them (coordinator verbs, worker verbs, read verbs, the server). docs/sprint/DASHBOARD.md: the dashboard, what each table and colour means, how to open it, from docs/SPEC-SPRINT-DASHBOARD.md. docs/sprint/OPERATIONS.md: running the server, its loops and lanes, install and switch, backups of the store, the chaos suites and what they prove (cards friend-chaos-suite and sprint-chaos-suite), fsck, the release check; link docs/sprint/RUNBOOK.md for incidents. docs/sprint/SPECS.md: the spec index, every SPEC-*.md that governs nova-sprint with one line each and the TLA+ modules (tla/) that model it. The red test first: internal/docs/sprint_cli_test.go fails while docs/sprint/CLI.md differs from what tools/clidoc writes for nova-sprint. Write in the brand of Stella's sheet, docs/sprint/BRAND.md (card sprint-brand-sheet, which this follows): its name treatment, voice and do/don't examples; read it and her flagship docs/sprint/README.md first. The pages live under docs/sprint/ while the code is in nova-tools and move to nova-sprint's docs/ at the split (plan: /Volumes/nova/ai/rowan/working/rowan-new/tmp/nova-sprint-split/PLAN.md), so a link between suite pages is relative within docs/sprint/, and a link to a nova-tools page is its absolute URL (https://github.com/mas-bandwidth/nova-tools/blob/main/docs/<page>), which still resolves after the split. Every fact comes from the specs (docs/SPEC-SPRINT.md, docs/SPEC-SPRINT-DASHBOARD.md, docs/SPRINT-COORDINATOR.md, docs/FRIENDS.md) and the help (`nova-sprint help`, `<verb> -h`), never memory; each command shown exists (the test checks). Add each page's line to docs/sprint/INDEX.md (the brand index page; SHARED) and change no other line of it. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/sprint/INDEX.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/sprint/INDEX.md,docs/sprint/CLI.md,docs/sprint/DASHBOARD.md,docs/sprint/OPERATIONS.md,docs/sprint/SPECS.md,internal/docs/sprint_cli_test.go
  COMMIT: sdocs-suite-reference: the nova-sprint reference pages: CLI, dashboard, operations and chaos, spec index
  VERDICT: docs/sprint/CLI.md is generated from nova-sprint's help by tools/clidoc and checked like docs/CLI.md; DASHBOARD.md, OPERATIONS.md and SPECS.md exist and are listed; the suite test passes over them.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/docs -run 'TestSprintCLIReferenceIsGeneratedFromHelp' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/docs`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/docs -run TestSprintCLIReferenceIsGeneratedFromHelp` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sdocs-coordinator-runbook" :tier "-" :needs ("sdocs-suite-guides")
      :title "nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 240 minutes
DEPENDS-ON: sdocs-suite-guides
PATHS: docs/sprint/INDEX.md,docs/sprint/RUNBOOK.md,internal/docs/sprint_runbook_test.go
SHARED: docs/sprint/INDEX.md
TEST: ./internal/docs TestRunbookHasAnEntryForEveryAlarmAndEveryEntryNamesItsVerbs
Deadline: finish within 240 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand. The README.md, full doc suite.\"). The coordinator answers the same situations every day by memory: merges behind, a friend down, a stuck card, a reader bouncing, a stopped machine, a red base, a store slow. Write docs/sprint/RUNBOOK.md: one entry per situation, headed with the exact name the tool uses for it (the judgment kinds, the wake kinds of `nova-sprint watch --wake` if it has landed, the where table's alarms), each with: what it means, how to confirm (read-only verbs, exact lines), what to do (verbs, exact lines, in order), how to see it worked, and when to tell the person in charge. Gather the kinds from the code (the judgment kinds in internal/sprint and the remedy table in cmd/nova-sprint/verbs.go, `a reader found it broken` and the rest) and from today's stopgap /Volumes/nova/ai/rowan/working/tmp/buswatch/watch.sh, whose wake kinds are today's alarms. The red test first: internal/docs/sprint_runbook_test.go collects the judgment kinds from the code and fails for one with no runbook heading. Write in the brand of Stella's sheet, docs/sprint/BRAND.md (card sprint-brand-sheet, which this follows): its name treatment, voice and do/don't examples; read it and her flagship docs/sprint/README.md first. The pages live under docs/sprint/ while the code is in nova-tools and move to nova-sprint's docs/ at the split (plan: /Volumes/nova/ai/rowan/working/rowan-new/tmp/nova-sprint-split/PLAN.md), so a link between suite pages is relative within docs/sprint/, and a link to a nova-tools page is its absolute URL (https://github.com/mas-bandwidth/nova-tools/blob/main/docs/<page>), which still resolves after the split. Every fact comes from the specs (docs/SPEC-SPRINT.md, docs/SPEC-SPRINT-DASHBOARD.md, docs/SPRINT-COORDINATOR.md, docs/FRIENDS.md) and the help (`nova-sprint help`, `<verb> -h`), never memory; each command shown exists (the test checks). Add each page's line to docs/sprint/INDEX.md (the brand index page; SHARED) and change no other line of it. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/sprint/INDEX.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/sprint/INDEX.md,docs/sprint/RUNBOOK.md,internal/docs/sprint_runbook_test.go
  COMMIT: sdocs-coordinator-runbook: the coordinator runbook: each alarm, what it means, the verbs that answer it
  VERDICT: docs/sprint/RUNBOOK.md has one entry per alarm, wake and judgment kind the tool raises, each with symptoms, the read-only verbs to diagnose, the verbs to act and how to see it worked; the test is red for a kind the code raises with no entry.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/docs -run 'TestRunbookHasAnEntryForEveryAlarmAndEveryEntryNamesItsVerbs' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/docs`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/docs -run TestRunbookHasAnEntryForEveryAlarmAndEveryEntryNamesItsVerbs` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sdocs-processor" :tier "-" :needs ("sdocs-suite-reference")
      :title "nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: sdocs-suite-reference
PATHS: docs/sprint/INDEX.md,docs/sprint/PROCESSOR.md,internal/docs/sprint_processor_test.go
SHARED: docs/sprint/INDEX.md
TEST: ./internal/docs TestProcessorPageMapsEveryPartAndSaysWhereTheAnalogyBreaks
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand. The README.md, full doc suite.\"). Write docs/sprint/PROCESSOR.md, \"nova-sprint is a processor\", for AI readers: a model that already knows how a pipelined CPU works learns the sprint in one page. The mapping, each with the exact verbs and spec sections that make it true: cards are instructions; the tick is the clock; the store is the registers; the server is the control unit; the pipeline runs deal, work, read, merge, land (stage by stage, with the card states of docs/SPEC-SPRINT.md); rework is a pipeline flush; DEPENDS-ON is a data hazard (a card waits for the result it needs); shared PATHS are structural hazards (two cards want one file; SHARED declares a safe share); judgments are interrupts (the coordinator's handler). Then each role's turn as one cycle: the coordinator, a member, a friend, a bud, a reader, the lander. Then where the analogy breaks (say plainly where it misleads: workers are not deterministic, a stage can take hours, an instruction can be rewritten, and whatever else is true). The red test first: internal/docs/sprint_processor_test.go fails while a mapping named above is missing or a named nova-sprint verb is not in the verb table. Write in the brand of Stella's sheet, docs/sprint/BRAND.md (card sprint-brand-sheet, which this follows): its name treatment, voice and do/don't examples; read it and her flagship docs/sprint/README.md first. The pages live under docs/sprint/ while the code is in nova-tools and move to nova-sprint's docs/ at the split (plan: /Volumes/nova/ai/rowan/working/rowan-new/tmp/nova-sprint-split/PLAN.md), so a link between suite pages is relative within docs/sprint/, and a link to a nova-tools page is its absolute URL (https://github.com/mas-bandwidth/nova-tools/blob/main/docs/<page>), which still resolves after the split. Every fact comes from the specs (docs/SPEC-SPRINT.md, docs/SPEC-SPRINT-DASHBOARD.md, docs/SPRINT-COORDINATOR.md, docs/FRIENDS.md) and the help (`nova-sprint help`, `<verb> -h`), never memory; each command shown exists (the test checks). Add each page's line to docs/sprint/INDEX.md (the brand index page; SHARED) and change no other line of it. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/sprint/INDEX.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/sprint/INDEX.md,docs/sprint/PROCESSOR.md,internal/docs/sprint_processor_test.go
  COMMIT: sdocs-processor: PROCESSOR.md: nova-sprint is a processor, for AI readers
  VERDICT: docs/sprint/PROCESSOR.md maps each part of nova-sprint to a part of a processor, walks each role's turn, and says where the analogy breaks; the test is red for a mapping it omits or a verb it names that does not exist.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/docs -run 'TestProcessorPageMapsEveryPartAndSaysWhereTheAnalogyBreaks' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/docs`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/docs -run TestProcessorPageMapsEveryPartAndSaysWhereTheAnalogyBreaks` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sdocs-suite-guides" :tier "-" :needs ("sdocs-suite-start")
      :title "nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 240 minutes
DEPENDS-ON: sdocs-suite-start
PATHS: docs/sprint/INDEX.md,docs/sprint/COORDINATOR-GUIDE.md,docs/sprint/FRIENDS-AND-BUDS.md,docs/sprint/CARD-AUTHORING.md
SHARED: docs/sprint/INDEX.md
TEST: ./internal/docs TestSprintDocSuitePagesExistLinkAndNameRealVerbs
Deadline: finish within 240 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand. The README.md, full doc suite.\"). Write three guides. docs/sprint/COORDINATOR-GUIDE.md: the coordinator's job (the seat, adding streams and cards, watching, answering judgments, landing, stopping), drawn from docs/SPRINT-COORDINATOR.md and docs/SPRINT-COORDINATOR-SEAT.md, which it links rather than repeats. docs/sprint/FRIENDS-AND-BUDS.md: what a friend and a bud are, how they take and finish cards, lanes, tiers, going up and down, honest attribution; link nova-tools docs/FRIEND-ONBOARDING.md by its absolute URL if it exists. docs/sprint/CARD-AUTHORING.md: how to write a card that converges: the header lines (RESULT, REPO, BASE, KIND, WHO, PATHS at most 8, SHARED, TEST, DEPENDS-ON), a test that fails first, DONE-WHEN, the rules block, sizing, and why; with one complete example card that `nova-swarm lint --card <f> --child-rules` passes (run it and put the LINT OK line under the example). Write in the brand of Stella's sheet, docs/sprint/BRAND.md (card sprint-brand-sheet, which this follows): its name treatment, voice and do/don't examples; read it and her flagship docs/sprint/README.md first. The pages live under docs/sprint/ while the code is in nova-tools and move to nova-sprint's docs/ at the split (plan: /Volumes/nova/ai/rowan/working/rowan-new/tmp/nova-sprint-split/PLAN.md), so a link between suite pages is relative within docs/sprint/, and a link to a nova-tools page is its absolute URL (https://github.com/mas-bandwidth/nova-tools/blob/main/docs/<page>), which still resolves after the split. Every fact comes from the specs (docs/SPEC-SPRINT.md, docs/SPEC-SPRINT-DASHBOARD.md, docs/SPRINT-COORDINATOR.md, docs/FRIENDS.md) and the help (`nova-sprint help`, `<verb> -h`), never memory; each command shown exists (the test checks). Add each page's line to docs/sprint/INDEX.md (the brand index page; SHARED) and change no other line of it. The suite's test (card sdocs-suite-start) covers the new pages once they are in INDEX.md; red first means adding them to the index before writing them. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/sprint/INDEX.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/sprint/INDEX.md,docs/sprint/COORDINATOR-GUIDE.md,docs/sprint/FRIENDS-AND-BUDS.md,docs/sprint/CARD-AUTHORING.md
  COMMIT: sdocs-suite-guides: the nova-sprint guides: the coordinator guide, friends and buds, card authoring
  VERDICT: the three guides exist, are listed in INDEX.md, and the suite test passes over them; card authoring shows a card that nova-swarm lint --child-rules passes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/docs -run 'TestSprintDocSuitePagesExistLinkAndNameRealVerbs' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/docs`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/docs -run TestSprintDocSuitePagesExistLinkAndNameRealVerbs` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sdocs-suite-start" :tier "-" :needs ("sprint-brand-sheet" "sprint-flagship-readme")
      :title "nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 240 minutes
DEPENDS-ON: sprint-brand-sheet,sprint-flagship-readme
PATHS: docs/sprint/INDEX.md,docs/sprint/GETTING-STARTED.md,docs/sprint/CONCEPTS.md,docs/sprint/FAQ.md,internal/docs/sprint_suite_test.go
SHARED: docs/sprint/INDEX.md
TEST: ./internal/docs TestSprintDocSuitePagesExistLinkAndNameRealVerbs
Deadline: finish within 240 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens docs (Glenn 2026-10-04 4:53 PM: \"nova-sprint needs the whole branding treatment, as an extension of the nova-sprint brand. The README.md, full doc suite.\"). This card starts the suite and its test; the next cards add pages to it. Write docs/sprint/INDEX.md (the brand index page: the mark, one paragraph of what nova-sprint is, then every page of the suite with one line each, grouped as Start, Guides, Reference, Operations), docs/sprint/GETTING-STARTED.md (from nothing to a first sprint on the twin store, `--redis mem:<file>`, every step a command and what it prints), docs/sprint/CONCEPTS.md (cards, streams, members and friends, the tick, the deal, reads, merge and land, judgments, sentinels, epochs; each a short section with the verb that shows it) and docs/sprint/FAQ.md (the questions a new coordinator and a new friend ask; find them in docs/SPRINT-COORDINATOR.md and the help's remedies). Write in the brand of Stella's sheet, docs/sprint/BRAND.md (card sprint-brand-sheet, which this follows): its name treatment, voice and do/don't examples; read it and her flagship docs/sprint/README.md first. The pages live under docs/sprint/ while the code is in nova-tools and move to nova-sprint's docs/ at the split (plan: /Volumes/nova/ai/rowan/working/rowan-new/tmp/nova-sprint-split/PLAN.md), so a link between suite pages is relative within docs/sprint/, and a link to a nova-tools page is its absolute URL (https://github.com/mas-bandwidth/nova-tools/blob/main/docs/<page>), which still resolves after the split. Every fact comes from the specs (docs/SPEC-SPRINT.md, docs/SPEC-SPRINT-DASHBOARD.md, docs/SPRINT-COORDINATOR.md, docs/FRIENDS.md) and the help (`nova-sprint help`, `<verb> -h`), never memory; each command shown exists (the test checks). Add each page's line to docs/sprint/INDEX.md (the brand index page; SHARED) and change no other line of it. The red test first: internal/docs/sprint_suite_test.go reads INDEX.md and fails for a listed page that is missing, any relative link in docs/sprint/ that does not resolve, or a fenced `nova-sprint <verb> --flag` line whose verb or flag is not in cmd/nova-sprint's verb table and FlagSet (parsed, no binary run). Later pages are listed in INDEX.md only when they exist. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/sprint/INDEX.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/sprint/INDEX.md,docs/sprint/GETTING-STARTED.md,docs/sprint/CONCEPTS.md,docs/sprint/FAQ.md,internal/docs/sprint_suite_test.go
  COMMIT: sdocs-suite-start: the nova-sprint doc suite begins: the brand index page, getting started, concepts, FAQ
  VERDICT: docs/sprint/INDEX.md is the suite's index in the brand, and GETTING-STARTED.md, CONCEPTS.md and FAQ.md exist; the suite test is red for a page in the index that is missing, a relative link that does not resolve, or a nova-sprint verb or flag shown that the tool does not have.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/docs -run 'TestSprintDocSuitePagesExistLinkAndNameRealVerbs' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/docs`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/docs -run TestSprintDocSuitePagesExistLinkAndNameRealVerbs` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-models" :cards
     ((:id "tla-card-lifecycle" :tier "-" :needs ("tla-coverage-map")
      :title "nova-sprint v1.0.0, lens tla-coverage"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 210 minutes
DEPENDS-ON: tla-coverage-map
PATHS: tla/CardLifecycle.tla,tla/MCCardLifecycle*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/lifecycle.go,internal/ci/tla_coverage_class_test.go
TEST: ./internal/ci TestTLAModelIsCurrentCardLifecycle
Deadline: finish within 210 minutes.
Libraries considered: TLA+ and TLC (the pinned jar tla/tla2tools.sha256 names) through tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens tla-coverage. Re-derive the card lifecycle from the code, since CardMachine and SprintEvents describe designs no longer run (tla/README.md): a primary's columns (waiting, ready, working, review, merging, landed, dropped) and the held flag, with every move the store allows (deal, finish, read ok or broken, rework, accept, land, drop, twin replacement, take-back) and the outside events (a worker vanishes, a push fails, the coordinator drops). Invariants at least: landed is final except by the reopen of card land-verify-landed-ancestry; a card is in exactly one column; a waiting card with an open need is never dealt; a replaced card's dependents need its twin. Reversed witnesses at least: a landed card dealt again; a card in two columns; a dependent dealt before its need landed. Keep CardMachine and SprintEvents as they are; their COVERAGE rows say stale, superseded by CardLifecycle, and whether to delete them goes to Glenn in the report. Model first from the code as it is at BASE (read the Go before writing a line of TLA+): VARIABLES are the state the code owns, actions are its operations and the outside events, invariants and liveness come from the rules docs/SPEC-SPRINT.md states. Every model has reversed witnesses: MC<Module>Broken*.cfg configurations that break one rule each and are declared expected to fail in tla/CASES.tsv, so the invariant is shown to bite. Check with TLC on a small instance on a Linux bench through tools/tlacheck (tla/README.md: run the changed groups, then tlacheck merge --keep into tla/RUNS.tsv), never on the Studio. Verify every counterexample against the code by hand before writing it down; a real code defect found is reported in REPORT.md as a finding with the trace, never fixed in this card. Cite the model from the Go file it describes (one comment line naming tla/<Module>.tla) and flip this machine's row of tla/COVERAGE.tsv to current, and add the one-line test TestTLAModelIsCurrentCardLifecycle to internal/ci/tla_coverage_class_test.go, calling the class test's row helper on the card-lifecycle row, red until the row is current. Glenn's standing rule (2026-09-27): TLA+ for every state machine, in every project. Edit only the files PATHS names; a change that needs another file is a HOLD naming it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: tla/CardLifecycle.tla,tla/MCCardLifecycle*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/lifecycle.go,internal/ci/tla_coverage_class_test.go
  COMMIT: tla-card-lifecycle: a current model of the card lifecycle as the store runs it, with reversed witnesses
  VERDICT: tla/CardLifecycle.tla checks the card's columns and moves as internal/sprint/lifecycle.go and store/steps.go make them; three reversed witnesses fail as declared; TestTLAModelIsCurrentCardLifecycle passes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestTLAModelIsCurrentCardLifecycle' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestTLAModelIsCurrentCardLifecycle` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "tla-holds-sentinels-stops" :tier "-" :needs ("tla-merge-tree-promotion")
      :title "nova-sprint v1.0.0, lens tla-coverage"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: tla-merge-tree-promotion
PATHS: tla/Holds.tla,tla/MCHolds*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/held.go,internal/ci/tla_coverage_class_test.go
TEST: ./internal/ci TestTLAModelIsCurrentHoldsSentinelsStops
Deadline: finish within 180 minutes.
Libraries considered: TLA+ and TLC (the pinned jar tla/tla2tools.sha256 names) through tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens tla-coverage. Model what keeps work from moving: a card admitted held (add --held) until release, a sentinel (a stop in line that the coordinator releases; what sorts after it waits for it), a wave loaded behind a held sentinel, and a stream stopped and resumed (internal/sprint/held.go, steps_sentinel.go, stopped.go, cmd/nova-sprint/held.go, and the stop reason of card verb-stop-reason-until if it has landed at BASE). Invariants: no card behind an unreleased sentinel is dealt; only the coordinator's release moves a held card; a stopped stream deals nothing and loses nothing; a release with nothing behind it is said, never silent (docs/SPEC-SPRINT.md section 16). Reversed witnesses: a deal past a held sentinel; a release by a worker; work lost across a stop. Model first from the code as it is at BASE (read the Go before writing a line of TLA+): VARIABLES are the state the code owns, actions are its operations and the outside events, invariants and liveness come from the rules docs/SPEC-SPRINT.md states. Every model has reversed witnesses: MC<Module>Broken*.cfg configurations that break one rule each and are declared expected to fail in tla/CASES.tsv, so the invariant is shown to bite. Check with TLC on a small instance on a Linux bench through tools/tlacheck (tla/README.md: run the changed groups, then tlacheck merge --keep into tla/RUNS.tsv), never on the Studio. Verify every counterexample against the code by hand before writing it down; a real code defect found is reported in REPORT.md as a finding with the trace, never fixed in this card. Cite the model from the Go file it describes (one comment line naming tla/<Module>.tla) and flip this machine's row of tla/COVERAGE.tsv to current, and add the one-line test TestTLAModelIsCurrentHoldsSentinelsStops to internal/ci/tla_coverage_class_test.go, calling the class test's row helper on the holds-sentinels-stops row, red until the row is current. Glenn's standing rule (2026-09-27): TLA+ for every state machine, in every project. Edit only the files PATHS names; a change that needs another file is a HOLD naming it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: tla/Holds.tla,tla/MCHolds*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/held.go,internal/ci/tla_coverage_class_test.go
  COMMIT: tla-holds-sentinels-stops: a current model of holds, sentinels, releases and stream stops
  VERDICT: tla/Holds.tla checks that nothing behind a held sentinel is dealt, only the coordinator releases, a stopped stream deals nothing and resumes where it stopped; reversed witnesses fail as declared; TestTLAModelIsCurrentHoldsSentinelsStops passes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestTLAModelIsCurrentHoldsSentinelsStops' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestTLAModelIsCurrentHoldsSentinelsStops` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "tla-merge-tree-promotion" :tier "-" :needs ("tla-friend-lanes-and-take")
      :title "nova-sprint v1.0.0, lens tla-coverage"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: tla-friend-lanes-and-take
PATHS: tla/MergeTree.tla,tla/MCMergeTree*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/promotion.go,internal/ci/tla_coverage_class_test.go
TEST: ./internal/ci TestTLAModelIsCurrentMergeTreePromotion
Deadline: finish within 180 minutes.
Libraries considered: TLA+ and TLC (the pinned jar tla/tla2tools.sha256 names) through tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens tla-coverage. Model the merge tree as the code runs it: the lander merges a card's head into its stream's BASE (a sprint/* or integration branch), and promotion (internal/sprint/promotion.go, cmd/nova-sprint/promote.go) carries a branch onto dev, recorded in the store; the tick raises dev is behind until a promotion is recorded (memory merge-to-dev-continually). Include the knot of 2026-10-04 as a reachability case: the same code moving on two lines at once (memory the-knot-of-2026-10-04: one line of a thing at a time). Invariants: every card landed on a source branch is on dev after the promotion that follows it; a promotion is recorded only when its push is on the remote; dev-is-behind is raised while a source is ahead with no recorded promotion. Reversed witnesses: a promotion recorded without its push; a landed card lost by a promotion; dev behind with no alarm. Model first from the code as it is at BASE (read the Go before writing a line of TLA+): VARIABLES are the state the code owns, actions are its operations and the outside events, invariants and liveness come from the rules docs/SPEC-SPRINT.md states. Every model has reversed witnesses: MC<Module>Broken*.cfg configurations that break one rule each and are declared expected to fail in tla/CASES.tsv, so the invariant is shown to bite. Check with TLC on a small instance on a Linux bench through tools/tlacheck (tla/README.md: run the changed groups, then tlacheck merge --keep into tla/RUNS.tsv), never on the Studio. Verify every counterexample against the code by hand before writing it down; a real code defect found is reported in REPORT.md as a finding with the trace, never fixed in this card. Cite the model from the Go file it describes (one comment line naming tla/<Module>.tla) and flip this machine's row of tla/COVERAGE.tsv to current, and add the one-line test TestTLAModelIsCurrentMergeTreePromotion to internal/ci/tla_coverage_class_test.go, calling the class test's row helper on the merge-tree-promotion row, red until the row is current. Glenn's standing rule (2026-09-27): TLA+ for every state machine, in every project. Edit only the files PATHS names; a change that needs another file is a HOLD naming it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: tla/MergeTree.tla,tla/MCMergeTree*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/promotion.go,internal/ci/tla_coverage_class_test.go
  COMMIT: tla-merge-tree-promotion: a current model of the merge tree: card heads into sprint branches, promotion to dev
  VERDICT: tla/MergeTree.tla checks that work reaches dev only through a recorded promotion, a promotion carries every landed card of its source, and dev-is-behind is raised until one is recorded; reversed witnesses fail as declared; TestTLAModelIsCurrentMergeTreePromotion passes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestTLAModelIsCurrentMergeTreePromotion' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestTLAModelIsCurrentMergeTreePromotion` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "tla-presence-and-leases" :tier "-" :needs ("tla-attempts-and-epochs")
      :title "nova-sprint v1.0.0, lens tla-coverage"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: tla-attempts-and-epochs
PATHS: tla/Presence.tla,tla/MCPresence*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/presence.go,internal/ci/tla_coverage_class_test.go
TEST: ./internal/ci TestTLAModelIsCurrentPresenceAndLeases
Deadline: finish within 180 minutes.
Libraries considered: TLA+ and TLC (the pinned jar tla/tla2tools.sha256 names) through tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens tla-coverage. Model presence: each fleet member, friend and reader row beats (internal/sprint/presence.go, internal/sprint/store/presence.go); a row is down after the window without a beat (Glenn 2026-10-04: a friend is down after 10 s without a pong; one word, down, never asleep); a down row's dealt cards and asked reads are taken back by the next tick and redealt, and it is dealt nothing; a row coming back starts empty. The card and read held by a row is its lease; model its expiry by the beat, with time as a bounded counter. Invariants: no card is working on a down row after one tick; nothing is dealt to a down row; a card taken back is in exactly one place. Reversed witnesses: a deal to a down row; a lease that survives its row; a card in two rows after a take-back. Today's phantom working=4 on a friend with nothing running is the motivating trace. Model first from the code as it is at BASE (read the Go before writing a line of TLA+): VARIABLES are the state the code owns, actions are its operations and the outside events, invariants and liveness come from the rules docs/SPEC-SPRINT.md states. Every model has reversed witnesses: MC<Module>Broken*.cfg configurations that break one rule each and are declared expected to fail in tla/CASES.tsv, so the invariant is shown to bite. Check with TLC on a small instance on a Linux bench through tools/tlacheck (tla/README.md: run the changed groups, then tlacheck merge --keep into tla/RUNS.tsv), never on the Studio. Verify every counterexample against the code by hand before writing it down; a real code defect found is reported in REPORT.md as a finding with the trace, never fixed in this card. Cite the model from the Go file it describes (one comment line naming tla/<Module>.tla) and flip this machine's row of tla/COVERAGE.tsv to current, and add the one-line test TestTLAModelIsCurrentPresenceAndLeases to internal/ci/tla_coverage_class_test.go, calling the class test's row helper on the presence-and-leases row, red until the row is current. Glenn's standing rule (2026-09-27): TLA+ for every state machine, in every project. Edit only the files PATHS names; a change that needs another file is a HOLD naming it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: tla/Presence.tla,tla/MCPresence*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/presence.go,internal/ci/tla_coverage_class_test.go
  COMMIT: tla-presence-and-leases: a current model of beats, the down window and what a down worker holds
  VERDICT: tla/Presence.tla checks that a worker past the down window is down, holds no dealt card after the next tick, and is dealt nothing new; reversed witnesses fail as declared; TestTLAModelIsCurrentPresenceAndLeases passes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestTLAModelIsCurrentPresenceAndLeases' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestTLAModelIsCurrentPresenceAndLeases` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "tla-attempts-and-epochs" :tier "-" :needs ("tla-card-lifecycle")
      :title "nova-sprint v1.0.0, lens tla-coverage"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: tla-card-lifecycle
PATHS: tla/CardAttempts.tla,tla/MCCardAttempts*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/round.go,internal/ci/tla_coverage_class_test.go
TEST: ./internal/ci TestTLAModelIsCurrentAttemptsAndEpochs
Deadline: finish within 180 minutes.
Libraries considered: TLA+ and TLC (the pinned jar tla/tla2tools.sha256 names) through tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens tla-coverage. Model the counters around a card: the attempt (each deal), the generation (each rework or reopen), the epoch (each sprint clear), as the branch suffix .w1.g1.e15 shows them, with the redeal of a no-result attempt on another route within three redeals (memory no-result-work-is-redealt-by-the-machine), the rework bound, and the tier escalation of a card that failed twice (flash to pro). Read internal/sprint/round.go, internal/sprint/store/steps.go, internal/sprint/store/epoch.go and internal/sprint/route.go. Invariants: no two attempts share a suffix; a step carrying an older epoch is refused; redeals never exceed the cap; a card escalates only upward. Reversed witnesses: an uncapped redeal; a step of a cleared epoch accepted; a reused suffix. Model first from the code as it is at BASE (read the Go before writing a line of TLA+): VARIABLES are the state the code owns, actions are its operations and the outside events, invariants and liveness come from the rules docs/SPEC-SPRINT.md states. Every model has reversed witnesses: MC<Module>Broken*.cfg configurations that break one rule each and are declared expected to fail in tla/CASES.tsv, so the invariant is shown to bite. Check with TLC on a small instance on a Linux bench through tools/tlacheck (tla/README.md: run the changed groups, then tlacheck merge --keep into tla/RUNS.tsv), never on the Studio. Verify every counterexample against the code by hand before writing it down; a real code defect found is reported in REPORT.md as a finding with the trace, never fixed in this card. Cite the model from the Go file it describes (one comment line naming tla/<Module>.tla) and flip this machine's row of tla/COVERAGE.tsv to current, and add the one-line test TestTLAModelIsCurrentAttemptsAndEpochs to internal/ci/tla_coverage_class_test.go, calling the class test's row helper on the attempts-and-epochs row, red until the row is current. Glenn's standing rule (2026-09-27): TLA+ for every state machine, in every project. Edit only the files PATHS names; a change that needs another file is a HOLD naming it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: tla/CardAttempts.tla,tla/MCCardAttempts*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/round.go,internal/ci/tla_coverage_class_test.go
  COMMIT: tla-attempts-and-epochs: a current model of attempts, generations, epochs and the redeal and rework bounds
  VERDICT: tla/CardAttempts.tla checks that an attempt's branch suffix .w<attempt>.g<generation>.e<epoch> is unique, redeals stop at the cap, a cleared epoch refuses every older step; reversed witnesses fail as declared; TestTLAModelIsCurrentAttemptsAndEpochs passes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestTLAModelIsCurrentAttemptsAndEpochs' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestTLAModelIsCurrentAttemptsAndEpochs` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "tla-friend-lanes-and-take" :tier "-" :needs ("tla-presence-and-leases" "friend-tla-delivery-states")
      :title "nova-sprint v1.0.0 and nova-tools v1.2.0, lens tla-coverage"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: tla-presence-and-leases,friend-tla-delivery-states
PATHS: tla/FriendLanes.tla,tla/MCFriendLanes*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/friend/lanes.go,internal/ci/tla_coverage_class_test.go
TEST: ./internal/ci TestTLAModelIsCurrentFriendLanesAndTake
Deadline: finish within 180 minutes.
Libraries considered: TLA+ and TLC (the pinned jar tla/tla2tools.sha256 names) through tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0 and nova-tools v1.2.0, lens tla-coverage. Model a friend's side of the deal: the one-shot lanes of internal/friend/lanes.go (and adapter_opencode_lanes.go): a job delivered to inbox/<job>/BRIEF.md, a lane started for it at most once, the lane ending with or without outbox REPORT.md, the friend sync collecting the report once; and the sprint's friend take and give (cmd/nova-sprint/friendtake.go, internal/sprint/friend_take.go, friend_deal.go) with the ready queue held at twice the friend's width (PR 5304). Friend.tla (with card friend-tla-delivery-states, which this card follows) owns the connection and delivery; this module owns lanes and take, and instances Friend only if it needs its states. Invariants: lanes running never exceed the width; a job is started at most once; a report is collected at most once; take never deals past the queue bound. Reversed witnesses: a job run twice; a report collected twice; a take past the bound. Model first from the code as it is at BASE (read the Go before writing a line of TLA+): VARIABLES are the state the code owns, actions are its operations and the outside events, invariants and liveness come from the rules docs/SPEC-SPRINT.md states. Every model has reversed witnesses: MC<Module>Broken*.cfg configurations that break one rule each and are declared expected to fail in tla/CASES.tsv, so the invariant is shown to bite. Check with TLC on a small instance on a Linux bench through tools/tlacheck (tla/README.md: run the changed groups, then tlacheck merge --keep into tla/RUNS.tsv), never on the Studio. Verify every counterexample against the code by hand before writing it down; a real code defect found is reported in REPORT.md as a finding with the trace, never fixed in this card. Cite the model from the Go file it describes (one comment line naming tla/<Module>.tla) and flip this machine's row of tla/COVERAGE.tsv to current, and add the one-line test TestTLAModelIsCurrentFriendLanesAndTake to internal/ci/tla_coverage_class_test.go, calling the class test's row helper on the friend-lanes-and-take row, red until the row is current. Glenn's standing rule (2026-09-27): TLA+ for every state machine, in every project. Edit only the files PATHS names; a change that needs another file is a HOLD naming it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: tla/FriendLanes.tla,tla/MCFriendLanes*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/friend/lanes.go,internal/ci/tla_coverage_class_test.go
  COMMIT: tla-friend-lanes-and-take: a current model of a friend's one-shot lanes and of friend take, give and the 2x ready queue
  VERDICT: tla/FriendLanes.tla checks that a friend runs at most its width of one-shot lanes, a job's REPORT.md is collected once, take never exceeds the friend's ready queue of twice its width; reversed witnesses fail as declared; TestTLAModelIsCurrentFriendLanesAndTake passes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestTLAModelIsCurrentFriendLanesAndTake' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestTLAModelIsCurrentFriendLanesAndTake` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "tla-judgments" :tier "-" :needs ("tla-holds-sentinels-stops")
      :title "nova-sprint v1.0.0, lens tla-coverage"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: tla-holds-sentinels-stops
PATHS: tla/Judgments.tla,tla/MCJudgments*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/inbox.go,internal/ci/tla_coverage_class_test.go
TEST: ./internal/ci TestTLAModelIsCurrentJudgments
Deadline: finish within 180 minutes.
Libraries considered: TLA+ and TLC (the pinned jar tla/tla2tools.sha256 names) through tools/tlacheck; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens tla-coverage. Model the judgment as the coordinator's interrupt: raised by the tick (internal/sprint/inbox.go, decide.go, rules.go), pushed to the seat's inbox (the push loop of verb-seat-install-pushe), answered by the coordinator or by a rule (SprintRules.tla models five rules alone: instance it, never duplicate it), and stale past a bound (the acceptance check accept-self-feeding wants no judgment older than 15 minutes). Invariants: each judgment pushed once and answered at most once; an answer by rule only for a judgment a rule names; a judgment past the bound raises one alarm. Reversed witnesses: a double answer; a push lost with the judgment marked pushed; a stale judgment with no alarm. Model first from the code as it is at BASE (read the Go before writing a line of TLA+): VARIABLES are the state the code owns, actions are its operations and the outside events, invariants and liveness come from the rules docs/SPEC-SPRINT.md states. Every model has reversed witnesses: MC<Module>Broken*.cfg configurations that break one rule each and are declared expected to fail in tla/CASES.tsv, so the invariant is shown to bite. Check with TLC on a small instance on a Linux bench through tools/tlacheck (tla/README.md: run the changed groups, then tlacheck merge --keep into tla/RUNS.tsv), never on the Studio. Verify every counterexample against the code by hand before writing it down; a real code defect found is reported in REPORT.md as a finding with the trace, never fixed in this card. Cite the model from the Go file it describes (one comment line naming tla/<Module>.tla) and flip this machine's row of tla/COVERAGE.tsv to current, and add the one-line test TestTLAModelIsCurrentJudgments to internal/ci/tla_coverage_class_test.go, calling the class test's row helper on the judgments row, red until the row is current. Glenn's standing rule (2026-09-27): TLA+ for every state machine, in every project. Edit only the files PATHS names; a change that needs another file is a HOLD naming it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: tla/Judgments.tla,tla/MCJudgments*,tla/CASES.tsv,tla/RUNS.tsv,tla/COVERAGE.tsv,internal/sprint/inbox.go,internal/ci/tla_coverage_class_test.go
  COMMIT: tla-judgments: a current model of judgments: raised, pushed, answered by the coordinator or by rule, and stale
  VERDICT: tla/Judgments.tla checks that every open judgment is pushed to the seat once, answered at most once, answered by rule only where a rule exists, and never older than the stale bound without an alarm; reversed witnesses fail as declared; TestTLAModelIsCurrentJudgments passes.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestTLAModelIsCurrentJudgments' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestTLAModelIsCurrentJudgments` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-comfort" :cards
     ((:id "stop-is-instant" :tier "-" :needs ("reads-ran-false-silent")
      :title "stop failed today on a store timeout while the machine was busy, and the coordinator retried it"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: reads-ran-false-silent
PATHS: cmd/nova-sprint/run.go,internal/sprint/stopped.go,internal/sprint/steps_tick.go,internal/sprint/stop_instant_test.go,tla/SprintEvents.tla,tla/RUNS.tsv,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestStopRecordsStoppedAtOnceAndTheTickDrains
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. stop failed today on a store timeout while the machine was busy, and the coordinator retried it. stop writes STOPPED in one store step and returns at once; nothing in the verb waits for the tick. The tick, on its next part, sees STOPPED and drains: it deals, asks and lands nothing new, lets dealt work finish, and logs one line the machine stopped (asked by <actor>). A stop sent to the server is answered as soon as the step is written; a store that does not answer within the verb's bound is reported as that, exit 2, never as a failed stop that half-happened. Extend tla/SprintEvents.tla with the separation of the stop step from the drain (invariant: no deal, ask or land after a recorded STOPPED), run the changed TLC groups on a Linux bench and merge the records into tla/RUNS.tsv with tlacheck merge --keep. Comfort lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:39 PM): what the coordinator does by hand, or waits on, becomes the tool's own behaviour. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/run.go,internal/sprint/stopped.go,internal/sprint/steps_tick.go,internal/sprint/stop_instant_test.go,tla/SprintEvents.tla,tla/RUNS.tsv,docs/SPEC-SPRINT.md
  COMMIT: stop-is-instant: stop records STOPPED at once; the tick drains
  VERDICT: on the twin store stop returns after one step while a tick is mid-part; that tick deals nothing more; TLC passes the changed SprintEvents cases.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestStopRecordsStoppedAtOnceAndTheTickDrains' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestStopRecordsStoppedAtOnceAndTheTickDrains` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "add-first-check" :tier "-" :needs ("bases-view")
      :title "Cards were added and dealt whose work was already on their base (dash-tier-costs-now, done by PR 5323; several re-cuts of landed work)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: bases-view
PATHS: cmd/nova-sprint/adddone*.go,cmd/nova-sprint/verbs.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestAddRefusesACardWhoseWorkIsAlreadyOnTheBase
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Cards were added and dealt whose work was already on their base (dash-tier-costs-now, done by PR 5323; several re-cuts of landed work). At add, by git and no model, in the lander's cache clone at the base tip: a card is already done when the base holds a commit whose subject begins with its COMMIT line's id prefix (<id>:), or a pushed branch of the card whose head is an ancestor of the base tip, or when every file in its PATHS that its brief says it creates already exists with its named TEST function. Such a card is refused as already done, with the evidence (the commit sha and subject, or the file and the test found); --allow-done adds it anyway. The card's TEST is not run at add (no go runs on the coordinator's machine); say so in the spec. Cite docs/SPEC-SPRINT.md. Comfort lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:39 PM): what the coordinator does by hand, or waits on, becomes the tool's own behaviour. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/adddone*.go,cmd/nova-sprint/verbs.go,docs/SPEC-SPRINT.md
  COMMIT: add-first-check: add refuses a card whose work is already on its base, with evidence
  VERDICT: with a temporary origin holding a commit \"x: ...\" an add of card x is refused naming the sha; a fresh card is added; --allow-done adds the done one.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestAddRefusesACardWhoseWorkIsAlreadyOnTheBase' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestAddRefusesACardWhoseWorkIsAlreadyOnTheBase` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "land-live-progress" :tier "-" :needs ("land-one-lander")
      :title "A land pass ran 25 to 40 minutes today with no line until its end, so the coordinator could not tell a slow pass from a stuck one"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: land-one-lander
PATHS: cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,cmd/nova-sprint/landstatus*.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestLandPrintsEachPhaseAndStatusShowsIt
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. A land pass ran 25 to 40 minutes today with no line until its end, so the coordinator could not tell a slow pass from a stuck one. The lander prints LANDING stream=<s> phase=<fetch|merge|check|queue|push|report> cards=<n> at=<time> as each stream starts and as each phase begins, and records the current one (stream, phase, since, pid) in a status file beside its cache clone, removed at the pass's end. nova-sprint land --status prints the current pass, phase and how long it has run, or no land pass running; with the server running --land it reads the server's status file. Cite docs/SPEC-SPRINT.md. Comfort lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:39 PM): what the coordinator does by hand, or waits on, becomes the tool's own behaviour. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,cmd/nova-sprint/landstatus*.go,docs/SPEC-SPRINT.md
  COMMIT: land-live-progress: the lander prints each phase; land --status shows the current one
  VERDICT: a land pass over a temporary origin prints a LANDING line per phase; land --status during a blocked fake check names the check phase; after the pass it says none.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestLandPrintsEachPhaseAndStatusShowsIt' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestLandPrintsEachPhaseAndStatusShowsIt` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "bases-view" :tier "-" :needs ("coordinator-wake-verb" "land-base-gate-stops-stream")
      :title "Cards today sat on bases nobody was watching (sprint-next on rowan/bus-rename, red from 12:04 PM)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: coordinator-wake-verb,land-base-gate-stops-stream
PATHS: cmd/nova-sprint/bases*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/coordinator.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestBasesListsEveryBaseInUseAndAddRefusesAPersonalOne
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Cards today sat on bases nobody was watching (sprint-next on rowan/bus-rename, red from 12:04 PM). Add nova-sprint bases [--json] (read class): one row per BASE named by a card not landed or dropped, with its cards by state, ahead and behind origin/dev (git rev-list in the lander's cache clone, fetched once per call), and the gate at its tip as the lander last recorded it (green, red with the time, or - when never gated). add refuses a card whose BASE is a personal branch (rowan/*, stella/* or any <friend>/* of the friends table) unless --allow-personal-base is given, naming the base and the flag. Cite docs/SPEC-SPRINT.md. Comfort lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:39 PM): what the coordinator does by hand, or waits on, becomes the tool's own behaviour. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/bases*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/coordinator.go,docs/SPEC-SPRINT.md
  COMMIT: bases-view: nova-sprint bases lists each base in use; add refuses a personal base
  VERDICT: with a temporary origin, bases prints each base with counts, ahead/behind and gate; add of a rowan/x base is refused without the flag and taken with it.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestBasesListsEveryBaseInUseAndAddRefusesAPersonalOne' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestBasesListsEveryBaseInUseAndAddRefusesAPersonalOne` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "reads-ran-false-silent" :tier "-" :needs ("coordinator-wake-verb" "read-asked-again-after-takebackc")
      :title "A read whose child did not run (the member reports read --return with the reason no verdict (ran=false ...): a harness failure, not a finding) is today handed back, counted toward the reads bound and can end in reads exhausted or show as a "
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: coordinator-wake-verb,read-asked-again-after-takeback
PATHS: internal/sprint/readers.go,internal/sprint/steps_review.go,internal/sprint/stats.go,internal/sprint/read_ranfalse_test.go,tla/DirtyTick.tla,tla/RUNS.tsv,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestAReadThatDidNotRunIsAskedAgainSilently
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. A read whose child did not run (the member reports read --return with the reason no verdict (ran=false ...): a harness failure, not a finding) is today handed back, counted toward the reads bound and can end in reads exhausted or show as a failed read. Such a return (its reason begins no verdict (ran=false) is asked again at once of another reader up with room (the same reader only when no other is up), is not counted toward the attempt's reads bound, never counts as broken in the reader's ok percent or the card's reads, and never raises a judgment; it is logged once per return as a NOTE. After five such returns on one attempt the card raises one judgment, reads cannot run, naming the readers and reasons. Extend tla/DirtyTick.tla's hand-back part with the uncounted return and its bound of five, keep the invariants, run the changed TLC groups on a Linux bench and merge the records into tla/RUNS.tsv with tlacheck merge --keep. Comfort lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:39 PM): what the coordinator does by hand, or waits on, becomes the tool's own behaviour. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/sprint/readers.go,internal/sprint/steps_review.go,internal/sprint/stats.go,internal/sprint/read_ranfalse_test.go,tla/DirtyTick.tla,tla/RUNS.tsv,docs/SPEC-SPRINT.md
  COMMIT: reads-ran-false-silent: a read that did not run is asked again silently, never counted as broken
  VERDICT: on the twin store a ran=false return is re-asked of another reader, the bound and ok percent are unchanged and no judgment is raised; the sixth such return raises one.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestAReadThatDidNotRunIsAskedAgainSilently' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestAReadThatDidNotRunIsAskedAgainSilently` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "recut-widen" :tier "-" :needs ("verb-recute")
      :title "A HOLD for PATHS too narrow is answered today by the coordinator reading the report and re-cutting by hand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: verb-recute
PATHS: cmd/nova-sprint/recut*.go,cmd/nova-sprint/friendcards.go,internal/member/member.go,docs/SPEC-CARD-CONTRACT.md,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-CARD-CONTRACT.md,docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestRecutWidenAppliesPathsProposed
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. A HOLD for PATHS too narrow is answered today by the coordinator reading the report and re-cutting by hand. A HOLD report may carry one machine-readable line PATHS-PROPOSED: <glob>[,<glob>...] (docs/SPEC-CARD-CONTRACT.md says when and how); the member (internal/member/member.go) and friend collection (cmd/nova-sprint/friendcards.go) carry that line into the finish report kept on the card. recut <id> --widen (the verb verb-recute adds; if it has not landed at your base, HOLD naming it) replaces the brief's PATHS with the union of the old PATHS and the proposed ones, keeps the attempt's pushed head as the next attempt's start (NEXT starts from that head), and refuses a card with no PATHS-PROPOSED line or a proposed glob that climbs out with .. or names a file absent at the base and not created by the head. Cite docs/SPEC-SPRINT.md. Comfort lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:39 PM): what the coordinator does by hand, or waits on, becomes the tool's own behaviour. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-CARD-CONTRACT.md, docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/recut*.go,cmd/nova-sprint/friendcards.go,internal/member/member.go,docs/SPEC-CARD-CONTRACT.md,docs/SPEC-SPRINT.md
  COMMIT: recut-widen: recut --widen applies a HOLD's PATHS-PROPOSED line and keeps the head
  VERDICT: on the twin store a held card whose report proposes a path is recut with the union and its next attempt starts from the held head; a report with no line is refused.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestRecutWidenAppliesPathsProposed' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestRecutWidenAppliesPathsProposed` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-install" :cards
     ((:id "install-canary-shadow-tick" :tier "-" :needs ("verb-selftest-land-server-switcheb")
      :title "nova-sprint v1.0.0, lens install"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: verb-selftest-land-server-switcheb
PATHS: cmd/nova-sprint/*switch*.go,cmd/nova-sprint/shadow*.go,cmd/nova-sprint/run.go,internal/sprint/store/tick.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
SHARED: docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./cmd/nova-sprint TestServerSwitchRunsAShadowTickAndRefusesABrokenBinary
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens install. A canary before every server swap. `nova-sprint server switch <binary>` (card verb-selftest-land-server-switcheb, which this card follows; read its code first and extend it, never a second switch) first runs `<binary> tick --shadow` against the live store: the tick computes its plan (plan and apply are separate, docs/SPEC-SPRINT.md) and prints it, applying nothing, under a read-only store user (no write command is reachable: the verb opens the store read-only and a write is a refusal, tested). The switch refuses, naming why, when the shadow tick exits non-zero, panics, or exceeds the tick deadline; it records the shadow's plan size and time beside the switch record. Today a release build broke the lander for 13 minutes and a cold server crash-looped from 4:28 to 4:31 PM; both would have shown in a shadow tick. Cite docs/SPEC-SPRINT.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/*switch*.go,cmd/nova-sprint/shadow*.go,cmd/nova-sprint/run.go,internal/sprint/store/tick.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
  COMMIT: install-canary-shadow-tick: server switch runs the new binary's shadow tick against the live store, read-only, before the swap
  VERDICT: on the twin store a candidate binary's shadow tick plans and writes nothing; a candidate whose shadow tick errors, panics or misses the tick deadline is refused and the old server keeps running.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestServerSwitchRunsAShadowTickAndRefusesABrokenBinary' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint/store`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestServerSwitchRunsAShadowTickAndRefusesABrokenBinary` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "install-rollback-drill" :tier "-" :needs ("install-server-unit-by-verb" "install-cold-twin-warmup")
      :title "nova-sprint v1.0.0, lens install"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: install-server-unit-by-verb,install-cold-twin-warmup
PATHS: cmd/nova-sprint/*switch*.go,cmd/nova-sprint/rollback_drill_test.go,cmd/nova-sprint/testdata/drill/**,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestRollbackDrillPutsThePreviousBinaryBack
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens install. The rollback path is only trusted if it is exercised. Add a drill: `server switch --drill` swaps in a candidate that is built to miss its tick deadline after READY (a test-only build tag in cmd/nova-sprint/testdata/drill), lets the canary pass (the shadow tick is fine; the failure shows only under load), swaps, and asserts the rollback of install-rollback-on-missed-ticks puts the previous binary back, the unit of install-server-unit-by-verb unchanged, the cold warm-up of install-cold-twin-warmup not mistaken for a failure, and the seat pushed once. The test TestRollbackDrillPutsThePreviousBinaryBack runs the same sequence with the fake supervisor and an injected clock. The release check (stream sprint-v1-release) will ask for a drill recorded since the release base: record each drill's result in the store. Cite docs/SPEC-SPRINT.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/*switch*.go,cmd/nova-sprint/rollback_drill_test.go,cmd/nova-sprint/testdata/drill/**,docs/SPEC-SPRINT.md
  COMMIT: install-rollback-drill: a rollback drill: a broken candidate is canaried, swapped, rolled back, and the old server serves
  VERDICT: the drill test builds a good and a deliberately broken server, runs canary, swap, probation and rollback on the twin store with the fake supervisor, and asserts the previous binary serves within N ticks with every step logged; `server switch --drill` runs the same sequence and prints DRILL OK.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestRollbackDrillPutsThePreviousBinaryBack' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestRollbackDrillPutsThePreviousBinaryBack` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "install-server-unit-by-verb" :tier "-" :needs ("install-version-skew-check" "seat-key-follows-record")
      :title "nova-sprint v1.0.0, lens install"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: install-version-skew-check,seat-key-follows-record
PATHS: cmd/nova-sprint/serverunit*.go,cmd/nova-sprint/*switch*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
SHARED: docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./cmd/nova-sprint TestServerInstallWritesAUnitWithNoActorAndRefusesAHandEdit
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens install. Today's server unit (com.nova.loop.sprint-server-studio) was written and edited by hand, and its NOVA_SPRINT_ACTOR=stella line moved the seat on every restart. Add `nova-sprint server install [--dry-run] [--dir <unit dir>]`: it renders the unit from a Go template (launchd plist on darwin, systemd on linux, the shape fleet/templates/nova-loop.*.j2 use), with no actor in it (the server reads the seat from the record, card seat-key-follows-record), the store credentials through `nova-secrets exec --only` naming key names only, and a hash of the rendered unit recorded beside it. Installing over a unit whose bytes differ from the recorded hash is refused with the diff unless `--replace-hand-edit` is given; server switch uses the same writer. The supervisor's load and unload sit behind the interface install-rollback-on-missed-ticks made; tests use the fake and a temp dir. Never print a secret's value. Cite docs/SPEC-SPRINT.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/serverunit*.go,cmd/nova-sprint/*switch*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
  COMMIT: install-server-unit-by-verb: server install writes the server's launchd or systemd unit itself; a hand-edited unit is a drift
  VERDICT: server install --dry-run prints a unit with no NOVA_SPRINT_ACTOR and the secrets wrapped by nova-secrets exec; installing over a hand-edited unit refuses and prints the diff.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestServerInstallWritesAUnitWithNoActorAndRefusesAHandEdit' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestServerInstallWritesAUnitWithNoActorAndRefusesAHandEdit` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "install-cold-twin-warmup" :tier "-" :needs ("install-rollback-on-missed-ticks")
      :title "nova-sprint v1.0.0, lens install"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: install-rollback-on-missed-ticks
PATHS: internal/sprint/store/twin.go,internal/sprint/store/tick.go,cmd/nova-sprint/serve.go,cmd/nova-sprint/run.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint/store TestTheTickDeadlineClockStartsOnlyAfterTheTwinIsWarm
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens install. From 4:28 to 4:31 PM on 2026-10-04 a restarted server crash-looped: its first ticks ran against a cold twin (the in-memory mirror of the store, internal/sprint/store/twin.go), missed the tick deadline, the server exited, the supervisor restarted it cold, and so on. Read the server's start path first (serve.go, run.go) and confirm the cause on the twin store with a large fixture (2,000 cards, as the live store holds today). Change: start-up loads the twin fully (and the store functions) under its own bounded warm-up budget, prints one `READY warm=<ms> rows=<n>` line, and only then starts the deadline clock; a warm-up over its budget exits once with a line naming the budget, and the next start backs off (the supervisor's restart interval doubles to a cap, recorded in a small state file under the sprint root). The test uses an injected clock and a slow fake store, no sleeps. This is the cold start that card install-rollback-on-missed-ticks must not mistake for a broken binary: say in the spec that probation counts ticks after READY. Cite docs/SPEC-SPRINT.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/store/twin.go,internal/sprint/store/tick.go,cmd/nova-sprint/serve.go,cmd/nova-sprint/run.go,docs/SPEC-SPRINT.md
  COMMIT: install-cold-twin-warmup: the server warms its twin before the tick deadline clock starts
  VERDICT: a server started on a large store loads its twin within a warm-up budget, prints one READY line, and only then starts the tick deadline clock; a warm-up over budget exits once with the reason instead of crash-looping.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint/store -run 'TestTheTickDeadlineClockStartsOnlyAfterTheTwinIsWarm' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint/store ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint/store -run TestTheTickDeadlineClockStartsOnlyAfterTheTwinIsWarm` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "install-rollback-on-missed-ticks" :tier "-" :needs ("install-canary-shadow-tick")
      :title "nova-sprint v1.0.0, lens install"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: install-canary-shadow-tick
PATHS: cmd/nova-sprint/*switch*.go,cmd/nova-sprint/run.go,tla/ServerInstall.tla,tla/MCServerInstall*,tla/CASES.tsv,tla/RUNS.tsv,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestASwappedServerThatMissesItsFirstTicksIsRolledBack
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens install. After a swap the new server is on probation for its first N ticks (`server switch --probation <n>`, default 5). If any of them misses the tick deadline, or the process exits, the previous binary (which switch keeps) is put back and restarted, the rollback is logged with the tick that failed, and the seat is pushed one note. A rollback never loops: a binary rolled back is refused by switch until a new binary is named. Model the install first, tla/ServerInstall.tla (states: running old, canary, swapped on probation, kept, rolled back; actions: shadow, swap, tick ok, tick missed, exit, probation end), with invariants (exactly one server serving; a rolled-back binary is never served again by switch) and reversed witnesses (rollback after probation ended; no rollback on exit; two servers), the cases in tla/CASES.tsv, TLC on a Linux bench, merged into tla/RUNS.tsv with tools/tlacheck merge --keep. The supervisor (launchd or systemd) sits behind an interface, faked in tests. Cite docs/SPEC-SPRINT.md and the model. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/*switch*.go,cmd/nova-sprint/run.go,tla/ServerInstall.tla,tla/MCServerInstall*,tla/CASES.tsv,tla/RUNS.tsv,docs/SPEC-SPRINT.md
  COMMIT: install-rollback-on-missed-ticks: a swapped server on probation is rolled back when its first N ticks miss their deadline
  VERDICT: with a fake supervisor and an injected clock, a new server that misses a tick deadline or exits within its first N ticks is replaced by the previous binary, logged and pushed; one that passes N ticks ends probation; the TLA+ model and its reversed witnesses pass.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestASwappedServerThatMissesItsFirstTicksIsRolledBack' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestASwappedServerThatMissesItsFirstTicksIsRolledBack` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "install-version-skew-check" :tier "-" :needs ("verb-seat-checke" "install-cold-twin-warmup")
      :title "nova-sprint v1.0.0, lens install"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: verb-seat-checke,install-cold-twin-warmup
PATHS: cmd/nova-sprint/seat*.go,internal/sprint/presence.go,internal/friend/daemon.go,internal/sprintwire/**,docs/SPEC-SPRINT.md,docs/SPEC-FRIEND.md
SHARED: docs/SPEC-SPRINT.md,docs/SPEC-FRIEND.md
TEST: ./cmd/nova-sprint TestSeatCheckNamesEveryVersionSkew
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens install. A version-skew row in `nova-sprint seat check` (card verb-seat-checke, which this card follows; it already has an installed-versions row, so extend it, never a second): compare the server's build with each fleet member's nova-swarm build, each friend's nova-friend build and each bud's one-shot lane build, each read from its beat (internal/sprint/presence.go; nova-friend adds its build to its beat in internal/friend/daemon.go when it does not carry it). The wire (internal/sprintwire) gets a minimum compatible version constant; a component below it is FAIL and the deal refuses it with a line naming the member and the install command; one that differs but is compatible is WARN. Today a server ran another build than the cards were cut on, and a friend's runner was days behind; nobody saw it. Cite docs/SPEC-SPRINT.md and docs/SPEC-FRIEND.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md,docs/SPEC-FRIEND.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/seat*.go,internal/sprint/presence.go,internal/friend/daemon.go,internal/sprintwire/**,docs/SPEC-SPRINT.md,docs/SPEC-FRIEND.md
  COMMIT: install-version-skew-check: the seat check reports version skew across the server, members, friends and buds
  VERDICT: with beats carrying builds, seat check prints one versions row per component, WARN for a build that differs from the server's release, FAIL for one below the wire's minimum, and the deal refuses a member below the minimum, naming it.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestSeatCheckNamesEveryVersionSkew' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint ./internal/friend ./internal/sprintwire`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestSeatCheckNamesEveryVersionSkew` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-integrity" :cards
     ((:id "fsck-pushed-heads-recorded" :tier "-" :needs ("fsck-verb-landed-on-base" "land-record-unreported-push")
      :title "nova-sprint v1.0.0, lens integrity"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: fsck-verb-landed-on-base,land-record-unreported-push
PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/fsck*.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestFsckFindsAPushedSprintHeadNoCardRecords
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens integrity. Second fsck check, pushed-head-recorded: every refs/heads/sprint/* head on the card's REPO is the recorded head of that card's attempt (the branch name carries the card id and its .w<attempt>.g<generation>.e<epoch> suffix), or of a landed or dropped card. Today three streams pushed their work and never recorded it (memory: merges-never-fall-behind, 3:20 PM: the cards sat \"merging\" forever), and nothing compared the remote with the store. A branch whose card is unknown, or whose head differs from the recorded head, is one violation naming the fix verb of card land-record-unreported-push (which this card follows). Branches of dropped cards older than the cleanup window are not violations (the lazy cleanup removes them). Cite docs/SPEC-SPRINT.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/fsck*.go,docs/SPEC-SPRINT.md
  COMMIT: fsck-pushed-heads-recorded: fsck: every pushed sprint/* head is recorded on its card
  VERDICT: with a fake remote listing a sprint/<card>.w1.g1.e15 head that no card attempt records, fsck reports pushed-head-recorded with the fix verb; a head the card records is clean.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFsckFindsAPushedSprintHeadNoCardRecords' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFsckFindsAPushedSprintHeadNoCardRecords` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "fsck-server-runs-and-pushes" :tier "-" :needs ("fsck-friend-queue-agreement" "verb-seat-install-pushe")
      :title "nova-sprint v1.0.0, lens integrity"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: fsck-friend-queue-agreement,verb-seat-install-pushe
PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/serve.go,cmd/nova-sprint/servelanes.go,tla/Fsck.tla,tla/MCFsck*,tla/CASES.tsv,tla/RUNS.tsv,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestTheServerRunsFsckAndPushesEachNewViolationOnce
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens integrity. The server runs fsck every N minutes (`serve --fsck-every`, default 10m, 0 off) and on demand, in one of its lanes (cmd/nova-sprint/servelanes.go), never inside the tick: git work is long and a tick step is never long (Glenn 2026-10-01). Each violation has a key (check and subject); a new key is pushed once to the seat as a note (the push of card verb-seat-install-pushe), a key that stays is not pushed again, a key that goes away is pushed once as cleared. Each run's time and count is recorded in the store (one small key with the last 48 hours of runs), so the release gate can ask \"fsck clean for 24 hours\". The violation lifecycle (absent, open, pushed, cleared) is a state machine: model it first in tla/Fsck.tla with MCFsck.cfg and reversed witnesses (a pushed key pushed again, a cleared key never said, a run inside the tick), add the cases to tla/CASES.tsv, run the changed TLC groups on a Linux bench with tools/tlacheck and merge with `tlacheck merge --keep` into tla/RUNS.tsv (tla/README.md says how). Cite docs/SPEC-SPRINT.md and the model from the code. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/serve.go,cmd/nova-sprint/servelanes.go,tla/Fsck.tla,tla/MCFsck*,tla/CASES.tsv,tla/RUNS.tsv,docs/SPEC-SPRINT.md
  COMMIT: fsck-server-runs-and-pushes: the server runs fsck every N minutes in a lane and pushes each new violation to the seat once
  VERDICT: with an injected clock the server runs fsck at the interval in a lane (never inside the tick), pushes a new violation once, says when it clears, and records each run's result for the release gate; the TLA+ model and its reversed witnesses pass.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestTheServerRunsFsckAndPushesEachNewViolationOnce' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestTheServerRunsFsckAndPushesEachNewViolationOnce` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "fsck-seat-agreement" :tier "-" :needs ("fsck-held-without-beat" "seat-key-follows-record" "adopt-apply-never-moves-the-seat")
      :title "nova-sprint v1.0.0, lens integrity"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: fsck-held-without-beat,seat-key-follows-record,adopt-apply-never-moves-the-seat
PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/fsck*.go,cmd/nova-sprint/serve.go,internal/sprint/seat.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestFsckFindsTheSeatKeyAgainstTheRecord
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens integrity. Fourth fsck check, seat-agreement: four values name the same coordinator: the store key sprint:coordinator, the seat record's holder, the actor the running server was started with, and the nova-config sprint row's coordinator. Today they split twice: the server's launchd unit carried NOVA_SPRINT_ACTOR=stella from the 10-03 handover and every restart wrote sprint:coordinator=stella against a record of rowan (fixed by hand at 3:39 and 3:42 PM), and `nova-config apply` moved the seat because the Postgres sprint row still named yesterday's coordinator. The server records the actor it started with in its own beat or info key (cmd/nova-sprint/serve.go) so fsck can read it; the config row is read through an injected reader (the command uses nova-config's library, the test a fake). The fixes are named, never done: `seat --repair` (card seat-key-follows-record) for the key, and the nova-config sprint set line for the row. Fixture: today's values (key stella, record rowan approved by glenn). Cite docs/SPEC-SPRINT.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/fsck*.go,cmd/nova-sprint/serve.go,internal/sprint/seat.go,docs/SPEC-SPRINT.md
  COMMIT: fsck-seat-agreement: fsck: the seat key, the seat record, the server's actor and the config row agree
  VERDICT: with sprint:coordinator=stella against a seat record of rowan, fsck reports seat-agreement naming all four values and seat --repair as the fix; four equal values are clean.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFsckFindsTheSeatKeyAgainstTheRecord' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFsckFindsTheSeatKeyAgainstTheRecord` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "fsck-held-without-beat" :tier "-" :needs ("fsck-pushed-heads-recorded")
      :title "nova-sprint v1.0.0, lens integrity"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: fsck-pushed-heads-recorded
PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/fsck*.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestFsckFindsACardHeldByAWorkerWithNoBeat
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens integrity. Third fsck check, held-has-beat: no card is dealt or working on a fleet member, a friend or a reader row whose beat (internal/sprint/presence.go) is older than the down window, or that has no beat at all. Today a friend row read working=4 with nothing running (phantom counts), and readers held reads after their process was gone. The check reads the rows and the beats from the snapshot with an injected clock (no wall clock in the test). The violation names the fix the store already has for the case: the redeal the tick does for a down member, `friend reconcile <friend>` for a friend, the reader take-back for a read; name the exact verb line by reading cmd/nova-sprint/verbs.go. Cite docs/SPEC-SPRINT.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/fsck*.go,docs/SPEC-SPRINT.md
  COMMIT: fsck-held-without-beat: fsck: no card is held by a worker with no beat
  VERDICT: on the twin store a card working on a member or friend whose beat is older than the down window is one violation naming the fix; the same card on a beating worker is clean.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFsckFindsACardHeldByAWorkerWithNoBeat' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFsckFindsACardHeldByAWorkerWithNoBeat` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "fsck-verb-landed-on-base" :tier "-" :needs ("land-verify-landed-ancestry")
      :title "nova-sprint v1.0.0, lens integrity (Glenn 2026-10-04 4:50 PM: more lenses for the highest-quality release)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: land-verify-landed-ancestry
PATHS: cmd/nova-sprint/fsck*.go,internal/sprint/fsck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
SHARED: docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./internal/sprint TestFsckFindsLandedRecordsMissingFromTheBase
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens integrity (Glenn 2026-10-04 4:50 PM: more lenses for the highest-quality release). The store and git disagreed today and nothing noticed: harness-pkg-delayproxy-tools-tlacheck-t and fix-general-tla-tableorder-tla are recorded landed but their heads are not on the base. Add `nova-sprint fsck [--json] [--check <name>]...`, a read-class verb that writes nothing: the checks are pure functions in internal/sprint/fsck.go over a snapshot (the store's rows, and git facts behind a small interface the command fills with git ls-remote and merge-base --is-ancestor against a clone it keeps under the sprint root), so unit tests use fakes and no socket. This card builds the frame (the check registry, one line per violation: `FSCK VIOLATION check=<name> subject=<id> want=<...> got=<...> fix=<verb line>`, a summary `FSCK OK checks=<n>` or `FSCK FAIL violations=<n>`, exit 0/1/2) and the first check, landed-on-base: every landed record's merge (else head) is an ancestor of origin/<its base>. Reuse the ancestry test of verify-landed (card land-verify-landed-ancestry, which this card follows; do not duplicate it) and name its `reopen <card> --reason` as the fix. Fixes are never done by fsck: a violation is reported, a separate verb repairs it. Use today's two cards as the test fixtures (their ids, a base without their heads). Cite docs/SPEC-SPRINT.md from the function; a spec subsection \"fsck\" lists the invariants, and docs/CLI.md gets the verb's usage. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/fsck*.go,internal/sprint/fsck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
  COMMIT: fsck-verb-landed-on-base: nova-sprint fsck: the store-vs-git invariant checker, first check landed-on-base
  VERDICT: on the twin store with a temporary origin, fsck prints one FSCK VIOLATION line for a card recorded landed whose head is not an ancestor of its base, names the fix verb, and exits 1; a clean store prints FSCK OK and exits 0.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFsckFindsLandedRecordsMissingFromTheBase' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFsckFindsLandedRecordsMissingFromTheBase` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "fsck-friend-queue-agreement" :tier "-" :needs ("fsck-seat-agreement" "friend-reconcile-every-tick")
      :title "nova-sprint v1.0.0, lens integrity"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: fsck-seat-agreement,friend-reconcile-every-tick
PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/fsck*.go,internal/friend/state.go,internal/friend/daemon.go,docs/SPEC-SPRINT.md,docs/SPEC-FRIEND.md
SHARED: docs/SPEC-SPRINT.md,docs/SPEC-FRIEND.md
TEST: ./internal/sprint TestFsckFindsAFriendQueueThatDisagreesWithTheStore
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens integrity. Fifth fsck check, friend-queue: for every friend, the cards the store has dealt to friend.<name> equal the ids in its QUEUE.json (internal/friend/state.go) and every REPORT.md in its outbox belongs to a dealt or finished card. The friend's files live on its own machine, so the nova-friend daemon (internal/friend/daemon.go) carries the queue ids and the outbox ids in its beat, and fsck compares those with the store. friend-reconcile-every-tick (which this card follows) repairs the ordinary drift every tick; fsck reports only a disagreement that survives two reconcile passes, so a violation means the repair is broken, and it names `friend reconcile <friend>` as the fix. Reuse the comparison of internal/sprint/friend_reconcile.go; never write a second one. Cite docs/SPEC-SPRINT.md and docs/SPEC-FRIEND.md. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md,docs/SPEC-FRIEND.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/fsck*.go,cmd/nova-sprint/fsck*.go,internal/friend/state.go,internal/friend/daemon.go,docs/SPEC-SPRINT.md,docs/SPEC-FRIEND.md
  COMMIT: fsck-friend-queue-agreement: fsck: every friend's dealt cards match its QUEUE.json and outbox
  VERDICT: a friend whose daemon reports a QUEUE.json and outbox that differ from the store's dealt cards across two reconcile passes is one violation per card; an agreeing friend is clean.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFsckFindsAFriendQueueThatDisagreesWithTheStore' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./internal/friend ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFsckFindsAFriendQueueThatDisagreesWithTheStore` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-release" :cards
     ((:id "release-check-merge-queue-p90" :tier "-" :needs ("release-check-acceptance")
      :title "nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: release-check-acceptance
PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
SHARED: cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
TEST: ./internal/sprint TestReleaseCheckFailsWhenTheMergeQueueAgeP90IsOverTheBar
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done). Merges fell behind today (Glenn 3:20 PM 2026-10-04: \"We cannot let merges get behind like this\"). Add the check merge-queue-p90: over the last 24 hours (--window), the age each card spent in merging (entered merging to landed or left merging, from the store's log; a card still merging counts with its age now), its p90 by the nearest-rank method, compared with --merge-p90 (default 30m; say why in the spec). Fail prints the p90, how many cards, and the oldest card still merging. The percentile function is tested on its own with known lists (empty list: the check is ok with n=0 said). Each check is a pure function in internal/sprint/releasecheck.go over a snapshot behind a small interface (the store's rows and log, git facts), so its unit tests use the twin store or fakes and no socket; it prints `RELEASE CHECK <name> ok|fail <evidence>` and on fail the evidence names what to look at. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
  COMMIT: release-check-merge-queue-p90: release check: the merge queue's age p90 is under its bar
  VERDICT: release check computes the p90 of the time cards spent in merging over the last 24 hours, fails above the bar (a flag with a default stated in the spec) and prints the p90, the count and the oldest card.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestReleaseCheckFailsWhenTheMergeQueueAgeP90IsOverTheBar' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestReleaseCheckFailsWhenTheMergeQueueAgeP90IsOverTheBar` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "release-check-cold-audit" :tier "-" :needs ("release-check-fsck-24h")
      :title "nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: release-check-fsck-24h
PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
SHARED: cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
TEST: ./internal/sprint TestReleaseCheckColdAuditSamplesTwentyLandedCardsAndNeedsEveryReadOk
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done). Add the check cold-audit: 20 cards landed since the last release (the base's last release tag; --since to override), sampled uniformly with a seed that is printed and recorded so the sample can be redrawn; each is asked as a cold read through the existing read machinery (the readers and `read` verbs; a reader that never saw the card, its packet the landed merge against its base), never a new reader path. `release check --audit` asks the reads and records the audit (seed, ids, asked time) in the store; a plain `release check` reads it: ok only when the audit is under 48 hours old and all 20 reads are ok; it names any broken read with its finding and any still unanswered. Asking reads is a write: --audit is the coordinator's, and the plain check stays read-only. Each check is a pure function in internal/sprint/releasecheck.go over a snapshot behind a small interface (the store's rows and log, git facts), so its unit tests use the twin store or fakes and no socket; it prints `RELEASE CHECK <name> ok|fail <evidence>` and on fail the evidence names what to look at. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
  COMMIT: release-check-cold-audit: release check: a cold audit of 20 landed cards
  VERDICT: release check --audit asks cold reads of 20 landed cards sampled since the last release and is ok only when every one reads ok; the sample is seeded and recorded, and a plain release check reports the last audit's result and age.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestReleaseCheckColdAuditSamplesTwentyLandedCardsAndNeedsEveryReadOk' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestReleaseCheckColdAuditSamplesTwentyLandedCardsAndNeedsEveryReadOk` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "release-check-tools" :tier "-" :needs ("release-check-cold-audit")
      :title "nova-tools v1.2.0, lens release: the same release gate for nova-tools"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: release-check-cold-audit
PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
SHARED: cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
TEST: ./internal/sprint TestReleaseCheckForNovaToolsScopesEveryCheckToItsStreamsAndHead
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-tools v1.2.0, lens release: the same release gate for nova-tools. Add `--product nova-sprint|nova-tools` (default nova-sprint) to release check: a product names its streams (nova-tools: tools-v1-2-0-*; nova-sprint: sprint-v1-*; a flag overrides) and its release head; every check of the registry runs scoped to them, and a check that has no meaning for a product (if any; e.g. a suite that does not exist for it) prints `RELEASE CHECK <name> n/a <why>`, never a silent ok. The product table is data in one place, not branches in each check. Each check is a pure function in internal/sprint/releasecheck.go over a snapshot behind a small interface (the store's rows and log, git facts), so its unit tests use the twin store or fakes and no socket; it prints `RELEASE CHECK <name> ok|fail <evidence>` and on fail the evidence names what to look at. Run both products on the twin store with a fixture of several streams and put the output in REPORT.md, with the exact line the coordinator runs against the live store. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
  COMMIT: release-check-tools: release check --product nova-tools: the same gate for nova-tools
  VERDICT: release check --product nova-tools runs every check scoped to the tools-v1-2-0-* streams and the nova-tools release head, says which checks do not apply and why, and is red on a fixture where a tools stream breaks one.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestReleaseCheckForNovaToolsScopesEveryCheckToItsStreamsAndHead' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestReleaseCheckForNovaToolsScopesEveryCheckToItsStreamsAndHead` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "release-check-fsck-24h" :tier "-" :needs ("release-check-chaos-green" "fsck-server-runs-and-pushes")
      :title "nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: release-check-chaos-green,fsck-server-runs-and-pushes
PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
SHARED: cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
TEST: ./internal/sprint TestReleaseCheckFailsUnlessFsckWasCleanForTwentyFourHours
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done). Add the check fsck-24h: card fsck-server-runs-and-pushes (which this follows) makes the server run fsck every interval and record each run's time and violation count, the last 48 hours, in one store key. The check is ok only when the record covers the last 24 hours, every run in it is clean, and no gap between runs is over three times the interval (a server that stopped running fsck is not clean). Fail names the first unclean run (its time, the count) or the gap. Read the record through the same accessor fsck uses; never run fsck inside the check. Each check is a pure function in internal/sprint/releasecheck.go over a snapshot behind a small interface (the store's rows and log, git facts), so its unit tests use the twin store or fakes and no socket; it prints `RELEASE CHECK <name> ok|fail <evidence>` and on fail the evidence names what to look at. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
  COMMIT: release-check-fsck-24h: release check: fsck clean for 24 hours
  VERDICT: release check reads the server's recorded fsck runs and fails when any run in the last 24 hours had a violation, when a gap between runs exceeds three intervals, or when the record covers less than 24 hours.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestReleaseCheckFailsUnlessFsckWasCleanForTwentyFourHours' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestReleaseCheckFailsUnlessFsckWasCleanForTwentyFourHours` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "release-check-acceptance" :tier "-" :needs ("release-check-frame")
      :title "nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: release-check-frame
PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
SHARED: cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
TEST: ./internal/sprint TestReleaseCheckRunsTheAcceptanceSentinelsSixChecks
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done). A stream's acceptance sentinel lands by the coordinator's acceptance alone (CHANGELOG #4318), and the coordinator accepts against six checks it runs by hand. Make them release-check checks (card release-check-frame built the registry; this card follows it). The six are not written in the repository today: ask the coordinator over the bus (`nova-bus send --to rowan --subject \"acceptance sentinel: the six checks\" --body \"card release-check-acceptance needs the list\"`) and wait up to 30 minutes for the answer; with no answer, finish HOLD naming the question, never guess the list. Write the six into docs/SPEC-RELEASE.md's \"release check\" section with their source (the coordinator's answer and its date), then implement each as a check with a red twin-store fixture and a green one. Each check is a pure function in internal/sprint/releasecheck.go over a snapshot behind a small interface (the store's rows and log, git facts), so its unit tests use the twin store or fakes and no socket; it prints `RELEASE CHECK <name> ok|fail <evidence>` and on fail the evidence names what to look at. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
  COMMIT: release-check-acceptance: release check runs the acceptance sentinel's six checks
  VERDICT: release check carries the six checks of the coordinator's acceptance sentinel, each its own RELEASE CHECK line, each red on a twin-store fixture that breaks it and green on one that keeps it.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestReleaseCheckRunsTheAcceptanceSentinelsSixChecks' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestReleaseCheckRunsTheAcceptanceSentinelsSixChecks` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "release-check-chaos-green" :tier "-" :needs ("release-check-merge-queue-p90" "sprint-chaos-suite")
      :title "nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: release-check-merge-queue-p90,sprint-chaos-suite
PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
SHARED: cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
TEST: ./internal/sprint TestReleaseCheckFailsWithoutAGreenChaosRunAtTheReleaseHead
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens release (Glenn 2026-10-04: the highest-quality release; a release ships when the tool says so, not when someone feels it is done). Add the check chaos-green: the friend chaos suite (card friend-chaos-suite) and the sprint chaos suite (card sprint-chaos-suite, which this follows) each have a recorded green run at the release head. Read how those suites report (their `CHAOS <suite> <fault> recovered=<bool>` lines) and where a functional run's result is recorded (the CI receipt of internal/cireceipt, or what the suites record); read that record, never re-run the suite inside the check. A run at an older head counts only when `git diff --name-only <run head> <release head>` touches no .go file (say so in the evidence). Fail names the suite, its last run's head and result. If the suites record nothing a check can read, add the smallest record (one line per run, in the place the suites already write) only if it is inside PATHS; otherwise HOLD naming the file. Each check is a pure function in internal/sprint/releasecheck.go over a snapshot behind a small interface (the store's rows and log, git facts), so its unit tests use the twin store or fakes and no socket; it prints `RELEASE CHECK <name> ok|fail <evidence>` and on fail the evidence names what to look at. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/releasecheck*.go,internal/sprint/releasecheck*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/SPEC-RELEASE.md,docs/CLI.md
  COMMIT: release-check-chaos-green: release check: the chaos suites are green at the release head
  VERDICT: release check fails unless the friend and sprint chaos suites have a green recorded run at the release head (or a head whose diff to it touches no Go file), naming the suite and the head it last ran at.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestReleaseCheckFailsWithoutAGreenChaosRunAtTheReleaseHead' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestReleaseCheckFailsWithoutAGreenChaosRunAtTheReleaseHead` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-simplicity" :cards
     ((:id "simp-retire-bud-runners" :tier "-" :needs ("simp-retire-buswatch" "claude-oneshot-lanesb")
      :title "nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: simp-retire-buswatch,claude-oneshot-lanesb
PATHS: docs/STOPGAPS.md,internal/ci/stopgap_bud_runners_retired_test.go,docs/FRIENDS.md,internal/friend/adapter_claude*.go,internal/friend/claude_lanes*_test.go
SHARED: docs/STOPGAPS.md,docs/FRIENDS.md
TEST: ./internal/ci TestStopgapBudRunnersAreRetired
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap. Retire the buds' /Volumes/nova/ai/buds/<bud>/runner.zsh and reader.zsh (rowan-space, rowan-mas, rowan-next; their headers say \"STOPGAP until card claude-oneshot-lanes lands\"). Card claude-oneshot-lanesb gave nova-friend's claude harness one-shot lanes. Walk both rows of docs/STOPGAPS.md: the tier-to-model map (frontier claude-fable-5-1, heavy claude-opus-5-5, pro claude-sonnet-5-5, flash claude-haiku-4-5-20251001), at most MAX lanes and at most one frontier lane, never rerunning a started job, the usage-limit pause that marks the friend down until the reset epoch, the reader beat and read verdicts (`read --ok`, `--broken --finding`, `--return` when no verdict was written), the bench rule sentence added to every prompt. For each, find the internal/friend test that proves it or write it here with a fake claude binary (a Go test binary, never a script); a behaviour the lanes cannot do is a HOLD naming it. Mark both rows retired with their test citations; docs/FRIENDS.md shows the bud as a friend row. The scripts live outside the repository: do not touch them; REPORT.md ends with the adopter steps per bud. The red test first: internal/ci/stopgap_bud_runners_retired_test.go asserts, with the register's parser from internal/ci/stopgaps_class_test.go, that the runner.zsh and reader.zsh row is STATUS: retired and every behaviour cites a test that exists; it is red until the row is retired. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/STOPGAPS.md,docs/FRIENDS.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/STOPGAPS.md,internal/ci/stopgap_bud_runners_retired_test.go,docs/FRIENDS.md,internal/friend/adapter_claude*.go,internal/friend/claude_lanes*_test.go
  COMMIT: simp-retire-bud-runners: retire the buds' runner.zsh and reader.zsh: nova-friend's claude one-shot lanes cover each behaviour, cited by test
  VERDICT: the runner.zsh and reader.zsh rows are STATUS: retired with every behaviour citing an existing internal/friend test, and docs/FRIENDS.md documents the bud as a plain claude one-shot friend row.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestStopgapBudRunnersAreRetired' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/friend`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestStopgapBudRunnersAreRetired` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "simp-retire-opencode-runners" :tier "-" :needs ("simp-retire-bud-runners" "friend-token-cap")
      :title "nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: simp-retire-bud-runners,friend-token-cap
PATHS: docs/STOPGAPS.md,internal/ci/stopgap_opencode_runners_retired_test.go,docs/FRIENDS.md,internal/friend/adapter_opencode_lanes.go,internal/friend/opencode_lanes*_test.go
SHARED: docs/STOPGAPS.md,docs/FRIENDS.md
TEST: ./internal/ci TestStopgapOpencodeRunnersAreRetired
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap. Retire /Volumes/nova/ai/freddy/runner.zsh and /Volumes/nova/ai/alex/runner.zsh (copied from a bud runner; headers say \"STOPGAP until card claude-oneshot-lanes generalizes to opencode one-shot lanes\"). Read both; they differ only in their settings block (filter flash or security, beat or not, MAX, raise rules, token cap). Walk their rows in docs/STOPGAPS.md: the card filter (security: stream security* or ids fp-sec*/sec-*/security-*; flash: flash cards, a pro or higher card dealt and not started is taken back with friend take), lanes held to 3 above a load, one-shot sessions (never --session), the cost line from opencode's database priced by the route row and never guessed, the 6M token cap (card friend-token-cap, which this follows), the provider failure (402, 429, out of funds) pausing every lane and holding the friend down until a person clears it, the refusing go on PATH. Each is a setting of the friend row or a lane behaviour of internal/friend/adapter_opencode_lanes.go, proved by a test with a fake opencode (a Go test binary); add what is missing in PATHS, and HOLD naming any behaviour that needs another file. Mark both rows retired with citations. Do not touch the scripts; REPORT.md ends with the adopter steps. The red test first: internal/ci/stopgap_opencode_runners_retired_test.go asserts, with the register's parser from internal/ci/stopgaps_class_test.go, that the Freddy and Alex runner.zsh row is STATUS: retired and every behaviour cites a test that exists; it is red until the row is retired. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/STOPGAPS.md,docs/FRIENDS.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/STOPGAPS.md,internal/ci/stopgap_opencode_runners_retired_test.go,docs/FRIENDS.md,internal/friend/adapter_opencode_lanes.go,internal/friend/opencode_lanes*_test.go
  COMMIT: simp-retire-opencode-runners: retire Freddy's and Alex's runner.zsh: the opencode one-shot lanes cover each behaviour, cited by test
  VERDICT: the two opencode runner rows are STATUS: retired with each behaviour citing an existing internal/friend test; the filter, take-back, cost, token cap and provider pause are lane behaviours.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestStopgapOpencodeRunnersAreRetired' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/friend`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestStopgapOpencodeRunnersAreRetired` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "simp-retire-buswatch" :tier "-" :needs ("simp-stopgap-register" "coordinator-wake-verb")
      :title "nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: simp-stopgap-register,coordinator-wake-verb
PATHS: docs/STOPGAPS.md,internal/ci/stopgap_buswatch_retired_test.go,docs/SPRINT-COORDINATOR.md,cmd/nova-sprint/watchwake*.go
SHARED: docs/STOPGAPS.md,docs/SPRINT-COORDINATOR.md
TEST: ./internal/ci TestStopgapWatchShIsRetired
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap. Retire /Volumes/nova/ai/rowan/working/tmp/buswatch/watch.sh. Card coordinator-wake-verb (which this follows) built `nova-sprint watch --wake`. Walk the watch.sh row of docs/STOPGAPS.md behaviour by behaviour (judgment wake rate-limited to 20 minutes, unasked stop, merge backlog over 30 or no land pass for 15 minutes, review/ready/fleet backlog, friend down twice except those down on purpose, real bus message with the noise filter, the 10-minute check, one line per wake then exit 0) and for each one find the test of the verb that proves it; a behaviour with no test gets the test here in cmd/nova-sprint/watchwake*_test.go (a gap in the verb itself is a HOLD naming it, never a new script). Then mark the row `STATUS: retired 2026-10-..` with each behaviour's `(test: TestX)` so the register's class test (simp-stopgap-register) goes green with it, and replace the watch.sh instructions in docs/SPRINT-COORDINATOR.md with the verb's line. The script itself lives outside the repository: do not touch it; REPORT.md ends with the exact adopter steps (stop the background run, run the verb, delete the file) for the coordinator. The red test first: internal/ci/stopgap_buswatch_retired_test.go asserts, with the register's parser from internal/ci/stopgaps_class_test.go, that the watch.sh row is STATUS: retired and every behaviour cites a test that exists; it is red until the row is retired. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/STOPGAPS.md,docs/SPRINT-COORDINATOR.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/STOPGAPS.md,internal/ci/stopgap_buswatch_retired_test.go,docs/SPRINT-COORDINATOR.md,cmd/nova-sprint/watchwake*.go
  COMMIT: simp-retire-buswatch: retire the coordinator's watch.sh: every behaviour is nova-sprint watch --wake's, each cited by its test
  VERDICT: the watch.sh row of docs/STOPGAPS.md is STATUS: retired, every behaviour cites an existing test of nova-sprint watch --wake, and docs/SPRINT-COORDINATOR.md tells the coordinator to run the verb, not the script.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestStopgapWatchShIsRetired' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestStopgapWatchShIsRetired` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "simp-retire-ping-and-beat-loops" :tier "-" :needs ("simp-retire-opencode-runners" "coordinator-ping-verb")
      :title "nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: simp-retire-opencode-runners,coordinator-ping-verb
PATHS: docs/STOPGAPS.md,internal/ci/stopgap_ping_beat_loops_retired_test.go,docs/FRIENDS.md,fleet/loops.yml,internal/friend/ping*_test.go
SHARED: docs/STOPGAPS.md,docs/FRIENDS.md
TEST: ./internal/ci TestStopgapPingAndBeatLoopsAreRetired
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens simplicity: one deletion card per stopgap. Retire the hand loops in ~/Library/LaunchAgents on the Studio: com.nova.loop.friend-ping-rowan (zsh -c \"while :; do for f in alex emma freddy johnny stella zhi; do nova-friend ping --as rowan --to $f; done; sleep 600; done\", a hard-coded list) and com.nova.loop.friend-beat-<name> for alex, emma, johnny, stella and zhi (zsh -c \"while :; do if pgrep -qf <app>; then nova-sprint friend beat <name>; fi; sleep 1; done\": a friend is up while its app runs). Card coordinator-ping-verb (which this follows) made the ping a verb. For the beat loops: the behaviour is \"beat while a named process runs\"; find where nova-friend or nova-sprint already does this, or add it as a flag of the existing beat or ping verb with a test using an injected process lister (no pgrep exec in tests), never as a script. Both become loop records (nova-config's loop kind, installed by fleet/loops.yml and its templates), never a hand plist; document the record in docs/FRIENDS.md. Mark the rows retired with citations. Do not touch the plists; REPORT.md ends with the adopter steps (unload, delete, apply the loop records). The red test first: internal/ci/stopgap_ping_beat_loops_retired_test.go asserts, with the register's parser from internal/ci/stopgaps_class_test.go, that the friend-ping-rowan and friend-beat row is STATUS: retired and every behaviour cites a test that exists; it is red until the row is retired. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/STOPGAPS.md,docs/FRIENDS.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: docs/STOPGAPS.md,internal/ci/stopgap_ping_beat_loops_retired_test.go,docs/FRIENDS.md,fleet/loops.yml,internal/friend/ping*_test.go
  COMMIT: simp-retire-ping-and-beat-loops: retire the hand ping and beat loops: nova-friend's ping verb and loop records replace the zsh while loops
  VERDICT: the friend-ping-rowan and friend-beat-<name> rows are STATUS: retired, each behaviour cites a test, and the replacement runs as a nova-config loop record, never a hand plist.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestStopgapPingAndBeatLoopsAreRetired' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci ./internal/friend ./internal/fleet`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestStopgapPingAndBeatLoopsAreRetired` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "simp-unused-verbs-flags" :tier "-" :needs ("simp-duplicate-paths")
      :title "nova-sprint v1.0.0 and nova-tools v1.2.0, lens simplicity: unused verbs and flags"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: simp-duplicate-paths
PATHS: internal/ci/unused_surface_class_test.go,internal/ci/testdata/unused-surface-ledger.txt,docs/STOPGAPS.md
SHARED: docs/STOPGAPS.md
TEST: ./internal/ci TestUnusedVerbsAndFlagsLedgerOnlyShrinks
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0 and nova-tools v1.2.0, lens simplicity: unused verbs and flags. Every verb and flag is surface a stranger must read and a reader must check. Write internal/ci/unused_surface_class_test.go: for each tool under cmd/, read its verb table and its registered flags (go/ast over the flag.FlagSet calls, as internal/ci/flagusage_class_test.go does; reuse its walker, never a second one), then search the tree for a use of each: a test that passes the flag or calls the verb, a docs/*.md example, a fleet/ file, an exec line of another tool, internal/sprintdash/page/app.js. One with no use outside its registration and help text is unused. The ledger internal/ci/testdata/unused-surface-ledger.txt is shrink-only (one line `<tool> <verb> [--flag] <keep|delete> <why>`); the test fails on an unused item not in it and on a line for an item now used or gone. The red test first with a fixture tool. In docs/STOPGAPS.md add a section \"unused surface\" listing the delete lines grouped by tool as proposed deletion cards (id, PATHS). Delete nothing here; the deletions are their own cards. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/STOPGAPS.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/ci/unused_surface_class_test.go,internal/ci/testdata/unused-surface-ledger.txt,docs/STOPGAPS.md
  COMMIT: simp-unused-verbs-flags: a shrink-only ledger of verbs and flags nothing uses
  VERDICT: the class test lists every verb and flag of every cmd/ tool that no test, doc, fleet file, other tool or dashboard names, fails on one not in the ledger, and today's ledger carries each with a keep-or-delete line.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestUnusedVerbsAndFlagsLedgerOnlyShrinks' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestUnusedVerbsAndFlagsLedgerOnlyShrinks` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "simp-duplicate-paths" :tier "-" :needs ("simp-retire-ping-and-beat-loops")
      :title "nova-sprint v1.0.0, lens simplicity: duplicate paths (two ways of doing one thing are two places to get it wrong)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: simp-retire-ping-and-beat-loops
PATHS: internal/ci/duplicate_paths_class_test.go,internal/ci/testdata/duplicate-paths-ledger.txt,docs/STOPGAPS.md
SHARED: docs/STOPGAPS.md
TEST: ./internal/ci TestDuplicatePathsLedgerOnlyShrinks
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens simplicity: duplicate paths (two ways of doing one thing are two places to get it wrong). Write internal/ci/duplicate_paths_class_test.go: parse with go/ast (standard library only) every non-test function of cmd/nova-sprint, internal/sprint/..., cmd/nova-friend and internal/friend; normalise each body (identifiers renamed in order of first use, literals kept, comments dropped) and hash it; a body of at least 8 statements whose hash occurs twice is a duplicate pair. It also flags two verbs whose help usage line is the same verb with different names (an alias left behind). The ledger internal/ci/testdata/duplicate-paths-ledger.txt is shrink-only (the style of the dead-code and flag-usage ledgers in internal/ci): one line per pair `<file:func> <file:func> <why>`; the test fails on a pair not in the ledger and on a ledger line whose pair is gone (so it shrinks). The red test first: a fixture under the test's testdata with one duplicated function. Then record today's pairs, and in docs/STOPGAPS.md add a section \"duplicate paths\" listing each with a one-line deletion card proposal (id, PATHS, which side stays and why). Delete nothing here. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/STOPGAPS.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/ci/duplicate_paths_class_test.go,internal/ci/testdata/duplicate-paths-ledger.txt,docs/STOPGAPS.md
  COMMIT: simp-duplicate-paths: a shrink-only ledger of duplicate code paths in the sprint and friend packages
  VERDICT: the class test finds every pair of function bodies in cmd/nova-sprint, internal/sprint, cmd/nova-friend and internal/friend that are the same after normalising names, and fails on any pair not in the ledger; the ledger lists today's pairs, each with the deletion card that removes one side.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/ci -run 'TestDuplicatePathsLedgerOnlyShrinks' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/ci -run TestDuplicatePathsLedgerOnlyShrinks` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "friend-reserve" :cards
     ((:id "bounded-fleet-trial" :tier "-" :needs () :who "friend.emma"
      :title "bounded-fleet-trial"
      :brief "RESULT: bounded-fleet-trial sha=780483886728 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: guard
CLASS: model
WHO: friend emma
LEG: go
DEADLINE: finish within 60 minutes
DEPENDS-ON: -
PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/fleettrial*.go,internal/sprint/trial*.go,internal/sprint/steps_work.go,internal/sprint/store/*.go,internal/sprint/refmodel/trial*.go,docs/CLI.md,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestTrialLimitFiveWidthTwoNeverTakesSixth
Deadline: finish within 60 minutes.
Libraries considered: reuse the sprint table fields, store single-writer steps, and standard flag parsing; add no dependency and no sidecar counter.

ATTRIBUTION OVERRIDE. The injected child rules' Claude trailer and Claude-Code PR footer are inapplicable to this friend card. Commits and reports identify Emma Antigravity and the actual model/harness; never claim Claude or Claude Code. This sentence overrides only those two attribution lines; every safety and quality rule remains in force.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md). Friend sync delivers it as `~/emma-working/inbox/<job>/BRIEF.md`. Its STATUS line names the epoch, attempt, and branch to push; its next line names the working directory. Work only under `~/emma-working/jobs/<job>/`, using `~/emma-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md. The END step replaces the injected fleet-card PR ending: push normally to the STATUS branch and write the outbox report; do not create a pull request. The sprint alone reviews and lands that remote branch.

STEP 1. Set `JOB=~/emma-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of `origin/sprint/mechanical-2026-10-02`; that tip is 780483886728 or a later commit of BASE, never an older pinned checkout. A later attempt starts from the continuation head BRIEF.md names. Export `GOCACHE=~/emma-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1`. Read fleet command dispatch, take admission, persisted sprint fields, the reference model/property tests, help, and SPEC-SPRINT. Do not rebase, force-push, or work outside `$JOB`.

STEP 2. Implement a persisted bounded fleet trial invoked as `nova-sprint fleet trial <member> --limit 5 --width 2`. Put parsing/execution in the new `fleettrial.go`; `verbs.go` owns only its registration/help line. Put trial request, persisted field constants, admission plan, and invariants in new `trial.go`; add the explicit store dispatcher in `store/steps.go` and the minimal take-admission hook in `steps_work.go` so ordinary takes enforce the persisted trial cap. The limit counts cumulative work cards admitted by that trial, not concurrent width. Once five have been admitted, the same single-writer plan that moves the fifth card atomically closes further trial admission, holds new dispatch to that member, and lets at most the already-running two drain. Retries and process restarts read the persisted count. Refuse limit below width and conflicting active trials with a remedy. The store wildcard is limited to steps.go and new trial_test.go; other store files remain outside this task. Extend the reference model through dedicated `trial*.go` files and cover crash/restart, fifth/sixth interleaving, and drain/hold.
  PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/fleettrial*.go,internal/sprint/trial*.go,internal/sprint/steps_work.go,internal/sprint/store/*.go,internal/sprint/refmodel/trial*.go,docs/CLI.md,docs/SPEC-SPRINT.md
  COMMIT: Add bounded fleet trial admission.
  VERDICT: a width-two simulation completes exactly five admissions, drains its final work, remains held, and observes zero sixth takes across a restart.

STEP 3. Obtain the coordinator's explicit shared Go-slot grant before running Go. Then run `nice -n 19 env GOMAXPROCS=2 go test -p 2 ./internal/sprint ./cmd/nova-sprint -run 'TestTrialLimitFiveWidthTwoNeverTakesSixth' -count=1 -timeout 600s`, then `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/ci`, record exact last lines, release the slot, and commit only PATHS with honest Emma Antigravity and actual model/harness attribution. The END step handles delivery.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/emma-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint -run TestTrialLimitFiveWidthTwoNeverTakesSixth` passes.")
      (:id "all-members-staging-refusal" :tier "-" :needs () :who "friend.emma"
      :title "all-members-staging-refusal"
      :brief "RESULT: all-members-staging-refusal sha=780483886728 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: guard
CLASS: model
WHO: friend emma
LEG: go
DEADLINE: finish within 50 minutes
DEPENDS-ON: -
PATHS: internal/sprint/held.go,internal/sprint/held_test.go,internal/sprint/steps_tick.go,internal/sprint/refmodel/actions.go,internal/sprint/refmodel/scenario_test.go,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestAllEligibleMembersRefuseStagingOpensOneJudgment
Deadline: finish within 50 minutes.
Libraries considered: extend existing HeldState, Unheld, and judgment records; add no dependency and no external refusal ledger.

ATTRIBUTION OVERRIDE. The injected child rules' Claude trailer and Claude-Code PR footer are inapplicable to this friend card. Commits and reports identify Emma Antigravity and the actual model/harness; never claim Claude or Claude Code. This sentence overrides only those two attribution lines; every safety and quality rule remains in force.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md). Friend sync delivers it as `~/emma-working/inbox/<job>/BRIEF.md`. Its STATUS line names the epoch, attempt, and branch to push; its next line names the working directory. Work only under `~/emma-working/jobs/<job>/`, using `~/emma-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md. The END step replaces the injected fleet-card PR ending: push normally to the STATUS branch and write the outbox report; do not create a pull request. The sprint alone reviews and lands that remote branch.

STEP 1. Set `JOB=~/emma-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of `origin/sprint/mechanical-2026-10-02`; that tip is 780483886728 or a later commit of BASE, never an older pinned checkout. A later attempt starts from the continuation head BRIEF.md names. Export `GOCACHE=~/emma-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1`. Read staging-refusal records, eligibility, holds, judgments, and the reference model/property tests. Do not rebase, force-push, or work outside `$JOB`.

STEP 2. Detect when every currently eligible member for a card has produced a staging refusal for the same generation. Atomically hold that generation and open one coordinator judgment containing member-by-member sanitized evidence and concrete retry/drop choices. A later eligible member, a new generation, or an explicit retry clears the aggregate condition. Repeated ticks must not create duplicate judgments or silently leave the card ready forever.
  PATHS: internal/sprint/held.go,internal/sprint/held_test.go,internal/sprint/steps_tick.go,internal/sprint/refmodel/actions.go,internal/sprint/refmodel/scenario_test.go,docs/SPEC-SPRINT.md
  COMMIT: Escalate fleet-wide staging refusal.
  VERDICT: simulations cover one-member fallback, all-member exhaustion, repeated ticks, new generation, and recovery.

STEP 3. Obtain an explicit shared Go-slot grant, run `nice -n 19 env GOMAXPROCS=2 go test -p 2 ./internal/sprint -run 'TestAllEligibleMembersRefuseStagingOpensOneJudgment' -count=1 -timeout 600s`, then `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/ci`, record exact last lines, release the slot, and commit only PATHS with honest Emma Antigravity and actual model/harness attribution.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/emma-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestAllEligibleMembersRefuseStagingOpensOneJudgment` passes.")
      (:id "inbox-single-judgment" :tier "-" :needs () :who "friend.emma"
      :title "inbox-single-judgment"
      :brief "RESULT: inbox-single-judgment sha=780483886728 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: guard
CLASS: model
WHO: friend emma
LEG: go
DEADLINE: finish within 40 minutes
DEPENDS-ON: -
PATHS: cmd/nova-sprint/reads.go,cmd/nova-sprint/inboxonly*.go,cmd/nova-sprint/verbs.go,docs/CLI.md,docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestInboxOnlyPrintsOneBoundedJudgment
Deadline: finish within 40 minutes.
Libraries considered: reuse FindGroup, groupText, the existing max/list rendering, and standard flag parsing; add no dependency and no second inbox read.

ATTRIBUTION OVERRIDE. The injected child rules' Claude trailer and Claude-Code PR footer are inapplicable to this friend card. Commits and reports identify Emma Antigravity and the actual model/harness; never claim Claude or Claude Code. This sentence overrides only those two attribution lines; every safety and quality rule remains in force.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md). Friend sync delivers it as `~/emma-working/inbox/<job>/BRIEF.md`. Its STATUS line names the epoch, attempt, and branch to push; its next line names the working directory. Work only under `~/emma-working/jobs/<job>/`, using `~/emma-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md. The END step replaces the injected fleet-card PR ending: push normally to the STATUS branch and write the outbox report; do not create a pull request. The sprint alone reviews and lands that remote branch.

STEP 1. Set `JOB=~/emma-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of `origin/sprint/mechanical-2026-10-02`; that tip is 780483886728 or a later commit of BASE, never an older pinned checkout. A later attempt starts from the continuation head BRIEF.md names. Export `GOCACHE=~/emma-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1`. Read cmdInbox, inbox rendering, verb registration/help, and the inbox spec. Do not rebase, force-push, or work outside `$JOB`.

STEP 2. Add `nova-sprint inbox --only <group-id>` as bounded inspection of exactly one currently open judgment. Keep selection/rendering and its tests in new `inboxonly*.go`; `reads.go` owns only flag validation and the call, and `verbs.go` only its public synopsis. It prints that group's finding, hint, exact answer commands, members, needs, and notes, but no unrelated open group or happened notification. Preserve `--open <id>` byte-for-byte as the existing full-inbox expansion. Refuse an unknown ID with bounded known IDs and a literal retry command; refuse `--only` with `--open`, `--read`, `--wait`, or `--push`. JSON returns the same single group and actionable commands in a stable object. Update CLI and SPEC-SPRINT.
  PATHS: cmd/nova-sprint/reads.go,cmd/nova-sprint/inboxonly*.go,cmd/nova-sprint/verbs.go,docs/CLI.md,docs/SPEC-SPRINT.md
  COMMIT: Bound single-judgment inbox inspection.
  VERDICT: a fixture with many large groups emits only the requested judgment under a fixed output bound, while legacy --open output remains identical.

STEP 3. Obtain the coordinator's explicit shared Go-slot grant before any Go command. Run `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint -run TestInboxOnlyPrintsOneBoundedJudgment` plus the existing inbox compatibility tests and `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/ci`, record exact last lines, release the slot promptly, and commit only PATHS with honest Emma Antigravity and actual model/harness attribution.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/emma-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestInboxOnlyPrintsOneBoundedJudgment` passes and the compatibility assertion proves --open is unchanged.")
      (:id "sprint-seat-credential" :tier "-" :needs () :who "friend.emma"
      :title "sprint-seat-credential"
      :brief "RESULT: sprint-seat-credential sha=780483886728 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: guard
CLASS: model
WHO: friend emma
LEG: go
DEADLINE: finish within 45 minutes
DEPENDS-ON: -
PATHS: cmd/nova-sprint/main.go,cmd/nova-sprint/main_test.go,cmd/nova-sprint/verbhelp.go,docs/CLI.md,docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestSprintSeatResolvesRedisWithoutPasswordEnvironment
Deadline: finish within 45 minutes.
Libraries considered: reuse internal/seatcred and the existing sprint flag/store setup; add no dependency or duplicate credential parser.

ATTRIBUTION OVERRIDE. The injected child rules' Claude trailer and Claude-Code PR footer are inapplicable to this friend card. Commits and reports identify Emma Antigravity and the actual model/harness; never claim Claude or Claude Code. This sentence overrides only those two attribution lines; every safety and quality rule remains in force.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md). Friend sync delivers it as `~/emma-working/inbox/<job>/BRIEF.md`. Its STATUS line names the epoch, attempt, and branch to push; its next line names the working directory. Work only under `~/emma-working/jobs/<job>/`, using `~/emma-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md. The END step replaces the injected fleet-card PR ending: push normally to the STATUS branch and write the outbox report; do not create a pull request. The sprint alone reviews and lands that remote branch.

STEP 1. Set `JOB=~/emma-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of `origin/sprint/mechanical-2026-10-02`; that tip is 780483886728 or a later commit of BASE, never an older pinned checkout. A later attempt starts from the continuation head BRIEF.md names. Export `GOCACHE=~/emma-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1`. Read internal/seatcred plus sprint connection flags, dispatch/help, and the CLI and sprint specs. Do not rebase, force-push, or work outside `$JOB`.

STEP 2. Wire nova-sprint store/coordinator connection setup to accept `--seat <name>` with `NOVA_SEAT` as the default, using the existing seatcred contract. Keep an explicit `--redis` URL authoritative. Resolve credentials only at connection time, redact every refusal/output, and preserve commands that need no Redis password. Add a focused test whose fake credential source proves the password reaches only the connector and never stdout, stderr, argv, or an error.
  PATHS: cmd/nova-sprint/main.go,cmd/nova-sprint/main_test.go,cmd/nova-sprint/verbhelp.go,docs/CLI.md,docs/SPEC-SPRINT.md
  COMMIT: Resolve sprint Redis credentials by seat.
  VERDICT: coordinator verbs work with a seat and no password environment variable while transcript assertions find no secret.

STEP 3. Request and receive the shared Go slot before running `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint -run TestSprintSeatResolvesRedisWithoutPasswordEnvironment` and `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/ci`; record exact last lines, release it promptly, and commit only PATHS with honest Emma Antigravity and actual model/harness attribution.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/emma-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestSprintSeatResolvesRedisWithoutPasswordEnvironment` passes.")
      (:id "width-one-operation" :tier "-" :needs () :who "friend.emma"
      :title "width-one-operation"
      :brief "RESULT: width-one-operation sha=780483886728 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: guard
CLASS: model
WHO: friend emma
LEG: go
DEADLINE: finish within 45 minutes
DEPENDS-ON: -
PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/fleetwidth*.go,cmd/nova-sprint/fleetsync.go,internal/config/redis.go,internal/config/redis_test.go,docs/CLI.md,docs/SPEC-SPRINT.md,docs/SPEC-CONFIG.md
TEST: ./cmd/nova-sprint TestFleetSyncWidthAppliesConfigAndLiveRowTogether
Deadline: finish within 45 minutes.
Libraries considered: reuse config.Store.Update, config.Apply, the config Redis Applier, and fleet sync's store step; add no dependency, subprocess, or duplicate apply client.

ATTRIBUTION OVERRIDE. The injected child rules' Claude trailer and Claude-Code PR footer are inapplicable to this friend card. Commits and reports identify Emma Antigravity and the actual model/harness; never claim Claude or Claude Code. This sentence overrides only those two attribution lines; every safety and quality rule remains in force.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md). Friend sync delivers it as `~/emma-working/inbox/<job>/BRIEF.md`. Its STATUS line names the epoch, attempt, and branch to push; its next line names the working directory. Work only under `~/emma-working/jobs/<job>/`, using `~/emma-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md. The END step replaces the injected fleet-card PR ending: push normally to the STATUS branch and write the outbox report; do not create a pull request. The sprint alone reviews and lands that remote branch.

STEP 1. Set `JOB=~/emma-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of `origin/sprint/mechanical-2026-10-02`; that tip is 780483886728 or a later commit of BASE, never an older pinned checkout. A later attempt starts from the continuation head BRIEF.md names. Export `GOCACHE=~/emma-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1`. Read fleet sync, its verb registration/help, the config width contract, and sprint CLI/spec text. Do not rebase, force-push, or work outside `$JOB`.

STEP 2. Add `nova-sprint fleet width <member> <n> --pg <dsn>` as the one explicit operation for the current three-step width change: update the nova-config machine row, apply the machine view into Redis, then sync the sprint fleet row. Put orchestration and tests in new `fleetwidth*.go`; `verbs.go` owns only registration/help. Refactor only the reusable validated sync call out of `fleetsync.go`. Expose from `internal/config/redis.go` the smallest credential-safe Applier constructor needed by existing `config.Apply`; do not duplicate apply logic or introduce a subprocess. Validate member, width, config schema, actor, and both endpoints before the first write. The operation is ordered and idempotent rather than falsely atomic across PostgreSQL and Redis: after any later-stage failure, print which stages committed and one exact rerun command; the rerun converges all three. Preserve ordinary `fleet sync` byte-for-byte.
  PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/fleetwidth*.go,cmd/nova-sprint/fleetsync.go,internal/config/redis.go,internal/config/redis_test.go,docs/CLI.md,docs/SPEC-SPRINT.md,docs/SPEC-CONFIG.md
  COMMIT: Apply configured and live width together.
  VERDICT: tests pin three-stage success, pre-write validation refusal, each recoverable partial-failure boundary, and idempotent rerun without silently reporting convergence.

STEP 3. Obtain the shared Go-slot grant, run `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint -run TestFleetSyncWidthAppliesConfigAndLiveRowTogether`, then `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/ci`, record exact last lines, release the slot, and commit only PATHS with honest Emma Antigravity and actual model/harness attribution.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/emma-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestFleetSyncWidthAppliesConfigAndLiveRowTogether` passes.")))
    (:stream "sprint-v1-sre" :cards
     ((:id "deal-subscription-first" :tier "-" :needs ("friend-deal-most-roomb" "claude-oneshot-lanesb")
      :title "The buds (rowan-space, rowan-mas, rowan-next: Claude subscription accounts, Opus 5.5) are effectively free, yet heavy and pro work is dealt to per-token routes while they sit idle"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: friend-deal-most-room,claude-oneshot-lanes
PATHS: internal/sprint/friend_deal.go,internal/sprint/route.go,internal/sprint/deal_subscription_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestHeavyAndProCardsGoToSubscriptionFriendsFirst
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. The buds (rowan-space, rowan-mas, rowan-next: Claude subscription accounts, Opus 5.5) are effectively free, yet heavy and pro work is dealt to per-token routes while they sit idle. The friend billing field already exists on origin/rowan/cost-tier-at-source (commit e9d4c003a2: nova-config friend rows billing api|subscription, default subscription, migration 0027_friend_billing.sql, read as config.BillingAPI); this card adds no field and no migration. If e9d4c003a2 is not an ancestor of your base, HOLD naming it. The deal offers a heavy or pro card first to an up friend with billing subscription, of a tier that can do it, with room (the room rule of friend-deal-most-room), and uses fleet routes and api friends only for what is left. A card naming a WHO keeps it. Cite docs/SPEC-SPRINT.md. Safety and SRE lenses on nova-sprint for v1.0.0 (Glenn 2026-10-04), coordinator decisions of 5:00 PM. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/sprint/friend_deal.go,internal/sprint/route.go,internal/sprint/deal_subscription_test.go,docs/SPEC-SPRINT.md
  COMMIT: deal-subscription-first: heavy and pro cards go to subscription friends first, routes and api friends overflow
  VERDICT: on the twin store with one subscription friend with room 4, an api friend and a route, five pro cards go four to the subscription friend and one elsewhere.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestHeavyAndProCardsGoToSubscriptionFriendsFirst' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestHeavyAndProCardsGoToSubscriptionFriendsFirst` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friend-token-cap" :tier "-" :needs ("friend-token-efficiency")
      :title "nova-sprint v1.0.0, lens SRE (Glenn ~5:30 PM 2026-10-04, from Freddy's runner: \"Mercury re-sends its whole context, a long card snowballs\")"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: friend-token-efficiency
PATHS: internal/friend/adapter_opencode_lanes.go,internal/friend/tokencap*.go,internal/friend/adapter_claude*.go,docs/SPEC-FRIEND.md,docs/FRIENDS.md
SHARED: docs/SPEC-FRIEND.md,docs/FRIENDS.md
TEST: ./internal/friend TestOneShotLaneStopsAtTheTokenCapAndHoldsWithTheReason
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-next) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens SRE (Glenn ~5:30 PM 2026-10-04, from Freddy's runner: \"Mercury re-sends its whole context, a long card snowballs\"). Today the cap lives only in a stopgap, /Volumes/nova/ai/freddy/runner.zsh (TOKEN_CAP=6000000, per card, all kinds): read how it counts and stops. Make it the product's: every one-shot lane of nova-friend (the opencode lanes in internal/friend/adapter_opencode_lanes.go and the claude one-shot lanes) counts a card's tokens as it runs (input, cached input, output and reasoning, summed; from the harness's own usage record: opencode's session rows, claude -p's stream-json usage events) behind a small Usage interface so tests use a fake; when the sum passes the cap the lane stops its own child process (never another), writes the outbox REPORT.md `Verdict: HOLD` with the reason `token cap <cap> reached at <n> tokens` and the usage so far, and the friend's next card proceeds. The cap is per friend row (default 6000000; 0 means none), read where the lane reads its other settings; say it in docs/SPEC-FRIEND.md and the friend row example in docs/FRIENDS.md. The red test first with a fake harness (a Go test binary that reports growing usage) and an injected clock: over the cap it is stopped and held with the reason; under it it finishes normally. Card friend-token-efficiency (which this follows) edits the same lanes; read its change first and build on it. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-FRIEND.md,docs/FRIENDS.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/friend/adapter_opencode_lanes.go,internal/friend/tokencap*.go,internal/friend/adapter_claude*.go,docs/SPEC-FRIEND.md,docs/FRIENDS.md
  COMMIT: friend-token-cap: a per-card token cap for one-shot friends, 6M by default, then HOLD with the reason
  VERDICT: a one-shot lane whose card's tokens pass the cap is stopped, the card finishes HOLD with `token cap <cap> reached at <n> tokens`, and a lane under the cap is untouched; the cap is a friend-row setting with 6000000 as its default.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/friend -run 'TestOneShotLaneStopsAtTheTokenCapAndHoldsWithTheReason' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/friend`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/friend -run TestOneShotLaneStopsAtTheTokenCapAndHoldsWithTheReason` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "store-latency-alarm" :tier "-" :needs ("store-latency-row")
      :title "Builds on store-latency-row (the server's measured p50 and p99)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 90 minutes
DEPENDS-ON: store-latency-row
PATHS: cmd/nova-sprint/view.go,cmd/nova-sprint/seat.go,internal/sprint/steps_tick.go,internal/sprint/store_rtt_alarm_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestStoreRoundTripOverFiveMillisecondsRaisesOneAlarm
Deadline: finish within 90 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Builds on store-latency-row (the server's measured p50 and p99). The seat verb and view coordinator show the store round trip as the median of 20 PINGs on the live connection, measured when they run; above 5 ms (a sprint setting, default 5 ms) the tick raises one judgment per episode, the store is slow: <median> ms, which ends when the median falls under half the bar. The measurement uses an injected pinger in tests. Cite docs/SPEC-SPRINT.md. SRE lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:42 PM): the machine stays fast, bounded and recoverable under load. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/view.go,cmd/nova-sprint/seat.go,internal/sprint/steps_tick.go,internal/sprint/store_rtt_alarm_test.go,docs/SPEC-SPRINT.md
  COMMIT: store-latency-alarm: the store round trip is shown and alarms above 5 ms
  VERDICT: with a fake pinger at 9 ms one judgment is raised, a second tick raises none, 2 ms ends the episode; seat and view print the median.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestStoreRoundTripOverFiveMillisecondsRaisesOneAlarm' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestStoreRoundTripOverFiveMillisecondsRaisesOneAlarm` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friend-token-efficiency" :tier "-" :needs ("claude-oneshot-lanesb")
      :title "One-shot and api friends (Freddy on a Mercury flash route, Alex) pay per token, and each delivery spends tokens that do not land work"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: claude-oneshot-lanesb
PATHS: cmd/nova-sprint/friendcards.go,cmd/nova-sprint/friendprompt*.go,internal/friend/adapter.go,internal/friend/adapter_opencode_lanes.go,docs/FRIENDS.md,docs/SPEC-FRIEND.md
SHARED: docs/FRIENDS.md,docs/SPEC-FRIEND.md
TEST: ./cmd/nova-sprint TestFriendDeliveryIsLeanCachedAndTargeted
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. One-shot and api friends (Freddy on a Mercury flash route, Alex) pay per token, and each delivery spends tokens that do not land work. Cut tokens per landed card: (1) a delivered brief cites the child rules by reference (the rules file's path and version) instead of a copied RULES block; (2) the prompt of every one-shot lane starts with one stable prefix (the friend's standing instructions, byte-identical across cards, the card-specific part after it) so the provider's prompt cache hits; (3) the delivery adds a TARGETS section: for each file in PATHS that exists at the base, the line numbers of the functions and identifiers THE TASK names (by go/ast for Go, by grep otherwise), so fewer exploration turns are needed; (4) the gate wrapper the delivery writes into the job prints only the last 40 lines of a gate's output plus each failing test's block; (5) an api friend's card is never delivered into a long-lived session: each card is a fresh one-shot session. The friend billing and usage reader are on origin/rowan/cost-tier-at-source (e9d4c003a2: cmd/nova-sprint/friendusage.go reads an api friend's OpenCode sessions); if it is not an ancestor of your base, HOLD naming it. Measure with it: input, cached input and output tokens and dollars per landed card for Freddy's last ten landed cards (before) and give the command the adopter runs for the next ten (after) in REPORT.md. Safety and SRE lenses on nova-sprint for v1.0.0 (Glenn 2026-10-04), coordinator decisions of 5:00 PM. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/FRIENDS.md, docs/SPEC-FRIEND.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/friendcards.go,cmd/nova-sprint/friendprompt*.go,internal/friend/adapter.go,internal/friend/adapter_opencode_lanes.go,docs/FRIENDS.md,docs/SPEC-FRIEND.md
  COMMIT: friend-token-efficiency: friend deliveries are lean, cache-friendly and targeted, cutting tokens per landed card
  VERDICT: two deliveries of different cards share a byte-identical prefix, carry no copied rules block, list targets with line numbers, and the gate wrapper prints a tail with the failing test.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestFriendDeliveryIsLeanCachedAndTargeted' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestFriendDeliveryIsLeanCachedAndTargeted` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "restart-keeps-reads" :tier "-" :needs ("script-cards-self-verify" "read-asked-again-after-takebackc")
      :title "A server restart takes back every read in flight, so reads are lost and cards strand in review"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: script-cards-self-verify,read-asked-again-after-takeback
PATHS: tla/ServerLanes.tla,tla/MCServerLanes*,tla/RUNS.tsv,internal/sprint/readers.go,cmd/nova-sprint/serve.go,cmd/nova-sprint/run.go,internal/sprint/read_lease_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestAServerRestartResumesReadsInFlight
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. A server restart takes back every read in flight, so reads are lost and cards strand in review. A read asked of a reader is held by a lease (the reader's read --begin starts it, its beat renews it, default 10 minutes); on start the server keeps every read whose lease is live and only takes back reads whose lease lapsed. Model it first beside tla/ServerLanes.tla (a Restart action, a lease per read, invariants: a read with a live lease is never taken back, and every lapsed read is taken back), add the configurations, run TLC on a Linux bench and merge the records into tla/RUNS.tsv with tlacheck merge --keep; cite the model from the code. SRE lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:42 PM): the machine stays fast, bounded and recoverable under load. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: tla/ServerLanes.tla,tla/MCServerLanes*,tla/RUNS.tsv,internal/sprint/readers.go,cmd/nova-sprint/serve.go,cmd/nova-sprint/run.go,internal/sprint/read_lease_test.go,docs/SPEC-SPRINT.md
  COMMIT: restart-keeps-reads: a server restart keeps reads in flight under a lease
  VERDICT: on the twin store a restart keeps a read begun a minute ago and takes back one whose lease lapsed; TLC passes the new ServerLanes cases.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestAServerRestartResumesReadsInFlight' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestAServerRestartResumesReadsInFlight` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-wallclock" :cards
     ((:id "reads-start-on-finish" :tier "-" :needs ("cycle-time-breakdown")
      :title "A finished card can wait a tick cycle or more for its read"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: cycle-time-breakdown
PATHS: internal/sprint/readers.go,internal/sprint/steps_work.go,internal/sprint/read_push_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestAFinishAsksAFreeReaderInTheSameTick
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. A finished card can wait a tick cycle or more for its read. On finish, in the same step's tick part, the card is asked of a free reader with room at once; the target is finish to read start under 30 s when readers have room, measured by the stage times of cycle-time-breakdown and shown as a row of where. Test with an injected clock: a finish with a free reader is asked in the same tick. Cite docs/SPEC-SPRINT.md. Wall-clock lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:47 PM): the time from add to landed, measured per stage, and the waits removed. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/sprint/readers.go,internal/sprint/steps_work.go,internal/sprint/read_push_test.go,docs/SPEC-SPRINT.md
  COMMIT: reads-start-on-finish: a finished card is asked of a free reader at once
  VERDICT: on the twin store with a fake clock a finished card is asked in the tick of its finish when a reader has room; with none free it is asked on the first free one.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestAFinishAsksAFreeReaderInTheSameTick' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestAFinishAsksAFreeReaderInTheSameTick` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friend-idle-wake" :tier "-" :needs ("friend-session-liveness")
      :title "Builds on friend-session-liveness (a friend's session activity, not just its daemon pong)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: friend-session-liveness
PATHS: internal/friend/daemon.go,internal/friend/machine.go,internal/friend/idle_wake_test.go,docs/SPEC-FRIEND.md
SHARED: docs/SPEC-FRIEND.md
TEST: ./internal/friend TestAnIdleFriendWithCardsGetsAWakeTurnThenANote
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Builds on friend-session-liveness (a friend's session activity, not just its daemon pong). A friend holding dealt cards whose session has shown no activity for N minutes (default 10, a friend row setting) gets one wake turn from her daemon (the harness's resume with a short prompt: you hold <n> cards, continue the oldest); a second miss after another N minutes sends one note to the coordinator on the bus (kind blocker when bus-message-kinds has landed) naming the friend and her cards. Test with an injected clock and a fake harness. Wall-clock lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:47 PM): the time from add to landed, measured per stage, and the waits removed. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-FRIEND.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/friend/daemon.go,internal/friend/machine.go,internal/friend/idle_wake_test.go,docs/SPEC-FRIEND.md
  COMMIT: friend-idle-wake: an idle friend with cards gets a wake turn, then the coordinator a note
  VERDICT: with a fake clock and harness, 10 idle minutes give one wake turn, 20 give one note, activity resets both.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/friend -run 'TestAnIdleFriendWithCardsGetsAWakeTurnThenANote' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/friend`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/friend -run TestAnIdleFriendWithCardsGetsAWakeTurnThenANote` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "rework-goes-to-the-same-worker" :tier "-" :needs ("reads-start-on-finish")
      :title "A bounced card is redealt anywhere and recloned from nothing"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: reads-start-on-finish
PATHS: internal/sprint/steps_work.go,internal/sprint/friend_deal.go,internal/sprint/rework_same_worker_test.go,cmd/nova-swarm/lazyclean.go,cmd/nova-swarm/slotclean.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestABouncedCardGoesBackToItsWorker
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. A bounced card is redealt anywhere and recloned from nothing. The deal of a rework prefers the member or friend that did attempt N, when it is up and has room, for a hold window (default 10 minutes, a sprint setting), then falls back to the normal deal. The member keeps that card's job dir and clone until the rework is dealt or the window ends (lazyclean and slotclean skip a dir tagged keep-for-rework with its expiry), so the next attempt starts from the clone it left. Test with an injected clock on the twin store. Cite docs/SPEC-SPRINT.md. Wall-clock lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:47 PM): the time from add to landed, measured per stage, and the waits removed. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/sprint/steps_work.go,internal/sprint/friend_deal.go,internal/sprint/rework_same_worker_test.go,cmd/nova-swarm/lazyclean.go,cmd/nova-swarm/slotclean.go,docs/SPEC-SPRINT.md
  COMMIT: rework-goes-to-the-same-worker: a rework goes back to the worker that did the attempt, with its clone kept
  VERDICT: on the twin store a bounced card is dealt to its worker with room inside the window and to another after it; the cleaner keeps a tagged dir until its expiry.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestABouncedCardGoesBackToItsWorker' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestABouncedCardGoesBackToItsWorker` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "deal-latency" :tier "-" :needs ("rework-goes-to-the-same-worker")
      :title "Measure ready to dealt to started (the worker's take) per card, and show the median and p90 per member and friend in where; the targets are under 60 s for fleet members and under 5 minutes for friends, alarmed once per episode when the p90 "
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: rework-goes-to-the-same-worker
PATHS: internal/sprint/steps_work.go,cmd/nova-sprint/run.go,internal/sprint/deal_latency_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestReadyToStartedIsMeasuredAndNotTickBound
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Measure ready to dealt to started (the worker's take) per card, and show the median and p90 per member and friend in where; the targets are under 60 s for fleet members and under 5 minutes for friends, alarmed once per episode when the p90 of the last hour is over. Find the waits bound to the tick interval in the run loop (a deal that waits for the next one-second tick, a take that waits for a poll) and remove them: a card becoming ready is dealt in the tick part it became ready in. Report the before and after figures from a bench run in REPORT.md. Test with an injected clock. Cite docs/SPEC-SPRINT.md. Wall-clock lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:47 PM): the time from add to landed, measured per stage, and the waits removed. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/sprint/steps_work.go,cmd/nova-sprint/run.go,internal/sprint/deal_latency_test.go,docs/SPEC-SPRINT.md
  COMMIT: deal-latency: ready to started is measured, alarmed and no longer bound to the tick
  VERDICT: on the twin store with a fake clock a card made ready is dealt in the same tick and its ready-to-dealt time is zero ticks; a slow take raises one alarm.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestReadyToStartedIsMeasuredAndNotTickBound' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestReadyToStartedIsMeasuredAndNotTickBound` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "shared-paths-run-parallel" :tier "-" :needs ("cycle-time-breakdown" "land-live-progress")
      :title "add already accepts overlapping PATHS when both briefs name the file on a SHARED line (cmd/nova-sprint/verbs.go sharedPaths); that part is done"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: cycle-time-breakdown,land-live-progress
PATHS: cmd/nova-sprint/land.go,internal/sprint/steps_merge.go,cmd/nova-sprint/land_overlap_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestAConflictFreeOverlapLandsAndARealConflictIsReworked
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. add already accepts overlapping PATHS when both briefs name the file on a SHARED line (cmd/nova-sprint/verbs.go sharedPaths); that part is done. What is missing is at the merge: a conflict there stops the whole stream for the coordinator. When the lander merges a head that overlaps an earlier landed head on a file and git merges it cleanly, it lands as today. When the merge really conflicts, the card alone is returned for a rework redo the same change on the current tip, naming the files in conflict, and the stream goes on landing the rest; only a conflict in a ledger keeps today's regeneration. Cite docs/SPEC-SPRINT.md. Wall-clock lens on nova-sprint for v1.0.0 (Glenn 2026-10-04 4:47 PM): the time from add to landed, measured per stage, and the waits removed. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/land.go,internal/sprint/steps_merge.go,cmd/nova-sprint/land_overlap_test.go,docs/SPEC-SPRINT.md
  COMMIT: shared-paths-run-parallel: a real merge conflict reworks the card; the stream keeps landing
  VERDICT: with a temporary origin, two cards editing different hunks of one file both land; two editing the same line land one and rework the other, the stream not stopped.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestAConflictFreeOverlapLandsAndARealConflictIsReworked' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestAConflictFreeOverlapLandsAndARealConflictIsReworked` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-friends" :cards
     ((:id "sprint-chaos-suite" :tier "-" :needs ("friend-chaos-suite" "land-one-lander")
      :title "nova-sprint v1.0.0, stream sprint-v1-friends"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 240 minutes
DEPENDS-ON: friend-chaos-suite,land-one-lander
PATHS: internal/chaos/**,cmd/nova-sprint/chaos_test.go,cmd/nova-sprint/chaos_functional_test.go,infra/functional-image/Containerfile,tools/functionalrun/**,docs/SPEC-SPRINT.md,docs/TESTING.md
SHARED: docs/SPEC-SPRINT.md,docs/TESTING.md
TEST: ./cmd/nova-sprint TestSprintChaosDeclaresEveryFaultWithABound
Deadline: finish within 240 minutes.
Libraries considered: the standard library (a loopback TCP proxy for latency and partition, in the functional tier only), the tree's own chaos, sprint and testkit packages, and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, stream sprint-v1-friends. Glenn, 2026-10-04 4:47 PM: the chaos approach for the sprint itself, on the internal/chaos harness friend-chaos-suite built, against a THROWAWAY store and server inside the functional container on a bench, never the live store or server (the container has no network; its throwaway Redis and nova-sprint server are child processes of the test). A throwaway sprint (init inside the container): init on the throwaway Redis, two members and one stream of quack cards, the server run with --land. Faults, each asserting recovery within its bound AND, after every fault, the invariant: no card lost (every card is ready, working, in review, merging or landed) and none landed twice (one landing per card on the throwaway bare repo). (1) stop the server abruptly mid-tick (SIGKILL from the test to its own child inside a tick; the supervisor in the test restarts it; ticks resume within 60s). (2) restart Redis (ticks resume within 60s, no tick crash-loops). (3) add 50 ms of latency to every Redis round trip through a loopback proxy (today's real failure: the cold twin's first tick missed its 10 s deadline, TickDeadline in cmd/nova-sprint/run.go, and the loop crash-looped; the assertion is that the first tick completes or the loop keeps ticking without exit 4 in a row, within 2m). (4) a network partition between a member and the server, 30s, through the same proxy (healed within 2m; the member's cards are not dealt twice). (5) two landers on one checkout (the second is refused naming the first, per land-one-lander; nothing lands twice). (6) a fork-bomb test as a card's post step under the wall (the container's --pids cap and the step's process-group signal contain it; the card fails cleanly, the member and server stay up within 60s). (7) a full disk in a job dir (a small tmpfs; the card fails with the disk-full reason, the member stays up, other cards land). New faults live in internal/chaos where they are general (the proxy, the disk filler); functionalrun gains only what the faults need (a per-run tmpfs size for the job dir), stated in its usage. The unit test declares the seven faults by name, each with a bound, and fails while any is missing. docs/TESTING.md gives the exact nova-config loop record line (kind loop, on hetzner) that runs the suite nightly through functionalrun; add no live record. Nothing that ships is a shell script: every loop or helper is a Go verb (Glenn, 2026-10-04 5:03 PM). Cite docs/SPEC-SPRINT.md; one section. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit the SHARED files: add your paragraph as its own subsection headed with this card id.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/TESTING.md, docs/SPEC-CHAOS.md, docs/SPEC-SPRINT.md and infra/functional-image/README.md first.

STEP 2. Do the task above, writing the red unit test first; the unit tier opens no socket and sleeps on no wall clock; real processes, sockets and the throwaway store are the functional tier's alone.
  PATHS: internal/chaos/**,cmd/nova-sprint/chaos_test.go,cmd/nova-sprint/chaos_functional_test.go,infra/functional-image/Containerfile,tools/functionalrun/**,docs/SPEC-SPRINT.md,docs/TESTING.md
  COMMIT: sprint-chaos-suite: seven sprint faults on the chaos harness, throwaway store and server, recovery bounds and no lost or double-landed card
  VERDICT: the unit test names seven faults with bounds; the functional suite in the container prints seven CHAOS lines, each recovered=true inside its bound, and the no-lost, no-double-landed invariant holds after each.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestSprintChaosDeclaresEveryFaultWithABound' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/chaos ./cmd/nova-sprint ./tools/functionalrun ./internal/ci`, then `go run ./tools/functionalrun run --deadline 30m ./cmd/nova-sprint` on hetzner; record exact last lines and every CHAOS line, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestSprintChaosDeclaresEveryFaultWithABound` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friend-reconcile-every-tick" :tier "-" :needs ("verb-friend-reconcile")
      :title "nova-sprint v1.0.0, stream sprint-v1-friends"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: verb-friend-reconcile
PATHS: cmd/nova-sprint/run.go,cmd/nova-sprint/friends.go,cmd/nova-sprint/friendreconcile*.go,internal/sprint/friend_reconcile*.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestRunReconcilesFriendsEveryTick
Deadline: finish within 120 minutes.
Libraries considered: the tree's own sprint packages (the twin store, ReadFriendQueue, FriendReconcileOf, FriendReturn) and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, stream sprint-v1-friends. `nova-sprint friend reconcile <friend>` (the card verb-friend-reconcile: cmd/nova-sprint/friendreconcile.go and internal/sprint/friend_reconcile.go, comparing the store with the friend's inbox/QUEUE.json and outbox, collecting a finished card and returning an abandoned one) runs only when the coordinator types it, so phantom working counts stay until someone looks: 2026-10-04, one friend's row read working=4 with nothing running. Run the reconcile for every friend automatically from the server's run loop (cmd/nova-sprint/run.go), once a tick and after the friends' cards are delivered (friendCardsOf, called from cmd/nova-sprint/friends.go), with the same plan the verb applies, so each fix is the verb's history line and nothing new decides. Bound it: one stat walk per friend directory per tick, skipped (one record line, never an error) for a friend whose directory is not reachable from the server; a reconcile that moves a card pushes one note naming the card and why. A friend whose working count on her row disagrees with her QUEUE.json after the pass is one judgment per episode, never one per tick. If verb-friend-reconcile has not reached this card's base when you start, HOLD naming it. Cite docs/SPEC-SPRINT.md; one paragraph in the friends section. Nothing that ships is a shell script: every loop or helper is a Go verb (Glenn, 2026-10-04 5:03 PM). Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and docs/SPEC-SPRINT.md first.

STEP 2. Do the task above, writing the red test first on the twin store with temporary friend directories and an injected clock; no test opens a socket or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/run.go,cmd/nova-sprint/friends.go,cmd/nova-sprint/friendreconcile*.go,internal/sprint/friend_reconcile*.go,docs/SPEC-SPRINT.md
  COMMIT: friend-reconcile-every-tick: the run loop reconciles every friend each tick, so phantom working counts vanish
  VERDICT: on the twin store a friend row with working=4 and an empty QUEUE.json is reconciled by the next tick with one history line per card; a finished card is collected; an unreachable directory is one record line; a lasting disagreement is one judgment.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestRunReconcilesFriendsEveryTick' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestRunReconcilesFriendsEveryTick` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friend-stall-ladder" :tier "-" :needs ("friend-session-liveness" "fix-late-rule-progress-stampb")
      :title "nova-sprint v1.0.0, stream sprint-v1-friends"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: friend-session-liveness,fix-late-rule-progress-stampb
PATHS: internal/sprint/friend_stall*.go,internal/sprint/steps_tick.go,internal/sprint/settings.go,cmd/nova-sprint/friendcards.go,tla/*StallLadder*,tla/*.tsv,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md,tla/*.tsv,internal/sprint/steps_tick.go
TEST: ./internal/sprint TestFriendStallLadderClimbsAndTakesBackUnstarted
Deadline: finish within 180 minutes.
Libraries considered: the tree's own sprint packages (the twin store, FriendTake, the notes) and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, stream sprint-v1-friends. A friend that stalls holds her dealt cards until the coordinator notices by hand (2026-10-04: one friend's session idle 2:40 to 4:34 PM with 8 cards dealt). Make the stall ladder fully mechanical, a tick part in internal/sprint/friend_stall.go added to TickParts (internal/sprint/steps_tick.go), with no step needing the coordinator. A friend is stalled when she holds dealt cards and neither her session activity (the signal friend-session-liveness puts on her friends row) nor a progress stamp on any of her cards (FieldProgress, written by `nova-sprint progress` since fix-late-rule-progress-stampb) is newer than `friend_stall_after` (a setting in internal/sprint/settings.go, default 20m). The ladder, one rung per `friend_stall_step` (default 5m) while she stays stalled: (1) a wake turn: a bus message to her (the server's existing send, `wakeFriend` and `sendBus` in cmd/nova-sprint/friendcards.go) that her daemon pushes in as a turn; (2) a second wake; (3) a coordinator note (a pushed judgment, \"friend <f> stalled <d>: two wakes unanswered\"); (4) every card of hers she has not started taken back, as `friend take --all-unstarted` takes them (FriendTake with All; a started card stays with her and finishes), so the next deal places them elsewhere; (5) she is marked down with the reason `stalled`, and released to up by the tick itself at her first session activity after it. Every rung is a happened note pushed like the rest, so the dashboard and the seat see it; any session activity or progress resets her to rung 0. Model it first in tla/StallLadder.tla with MCStallLadder.tla and its cfgs: the variables are each friend's rung, her cards' states (dealt, started, taken), activity and the clock; invariants: no card is held by a stalled friend for more than the bound (stall_after plus four steps), no card is redealt while it has started, and a friend is released only by her own activity; reversed witnesses for each that TLC catches; rows in tla/CASES.tsv and runs in tla/RUNS.tsv. Cite the spec and the model from each function; one section in docs/SPEC-SPRINT.md. Nothing that ships is a shell script: every loop or helper is a Go verb (Glenn, 2026-10-04 5:03 PM). Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit the SHARED files: add your paragraph as its own subsection headed with this card id, append rows only, and add one entry to TickParts without reordering the rest.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-SPRINT.md and tla/README.md first.

STEP 2. Do the task above, the model first, then the red test on the twin store with an injected clock; no test opens a socket or sleeps on the wall clock.
  PATHS: internal/sprint/friend_stall*.go,internal/sprint/steps_tick.go,internal/sprint/settings.go,cmd/nova-sprint/friendcards.go,tla/*StallLadder*,tla/*.tsv,docs/SPEC-SPRINT.md
  COMMIT: friend-stall-ladder: a stalled friend is woken twice, noted, her unstarted cards taken back and she is marked down, all by the tick
  VERDICT: on the twin store a friend silent past stall_after climbs every rung, one note each; her unstarted cards are redealt and a started one stays; activity resets her and releases the down; TLC passes the model and catches each reversed witness.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestFriendStallLadderClimbsAndTakesBackUnstarted' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint ./internal/ci`, then `go run ./tools/tlacheck run --dir \"$JOB/tlc\" --jar /opt/tla/tla2tools.jar` for the StallLadder cases; record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestFriendStallLadderClimbsAndTakesBackUnstarted` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "friend-chaos-suite" :tier "-" :needs ("daemon-supervised")
      :title "nova-sprint v1.0.0, stream sprint-v1-friends"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: daemon-supervised
PATHS: internal/chaos/**,internal/friend/chaos_functional_test.go,docs/SPEC-CHAOS.md,docs/TESTING.md
SHARED: docs/TESTING.md
TEST: ./internal/chaos TestHarnessRunsNamedFaultsWithinBounds
Deadline: finish within 180 minutes.
Libraries considered: the standard library (os/exec, net for a loopback proxy in the functional tier only), the tree's own testkit, bus and friend packages, and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, stream sprint-v1-friends. Glenn, 2026-10-04 4:47 PM: \"loving the chaos monkey (chaos engineering) approach here.\" Two parts. (1) A small reusable chaos harness, internal/chaos, general and never friend-specific, so the sprint uses it too (sprint-chaos-suite): a Fault is a name, an Inject function, a Heal function (or none), a Recovered predicate, and a Bound; Run injects, polls Recovered on an injected clock until it holds or the bound passes, heals, and returns one report line per fault, `CHAOS <suite> <fault> recovered=<bool> took=<d> bound=<d>`, plus an Invariant hook checked after each fault (here: no message lost, none pushed twice). The unit tier tests the harness with fake faults and a fake clock: a fault that recovers inside its bound passes, one past it fails with the line, a broken invariant fails. (2) The friend suite, functional tier (`//go:build functional`), run in the container per the functional-test rule (tools/functionalrun; its throwaway Redis is a child process of the test inside the container, never a shared store): a real nova-friend daemon on a throwaway bus, a fake harness that is a Go test binary (never a shell script) standing in for the session, and a supervisor in the test standing in for launchd's KeepAlive with its five second throttle. Faults and bounds: stop the daemon abruptly, SIGKILL from the test to its own child (session pong again within 1m); end the session process abruptly (recovered by session-recovery within 5m); drop the bus (stop and restart the throwaway Redis, the test's own child; every pending message delivered within 2m of its return, none lost); fill the context (the fake answers a context-limit error; a fresh session with the handoff within 5m); inject a rate limit (the fake answers a usage limit with a reset 30s ahead; down within 1m, up within 1m of the reset). docs/SPEC-CHAOS.md states the harness and each fault; docs/TESTING.md gives the exact nova-config loop record line (kind loop, on hetzner) that runs the suite nightly through functionalrun; add no live record. Nothing that ships is a shell script: every loop or helper is a Go verb (Glenn, 2026-10-04 5:03 PM). Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/TESTING.md (the SHARED line): add your paragraph as its own subsection headed with this card id.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/TESTING.md, docs/SPEC-FRIEND.md and infra/functional-image/README.md first.

STEP 2. Do the task above, writing the red unit test of the harness first with fakes and an injected clock; the unit tier opens no socket and sleeps on no wall clock; real processes and sockets are the functional tier's alone.
  PATHS: internal/chaos/**,internal/friend/chaos_functional_test.go,docs/SPEC-CHAOS.md,docs/TESTING.md
  COMMIT: friend-chaos-suite: internal/chaos, named faults with recovery bounds, and the friend suite on it
  VERDICT: the harness unit tests pass; the friend suite in the container prints five CHAOS lines, each recovered=true inside its bound, with no message lost or pushed twice.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/chaos -run 'TestHarnessRunsNamedFaultsWithinBounds' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/chaos ./internal/friend ./internal/ci`, then `go run ./tools/functionalrun run --deadline 20m ./internal/friend` on hetzner; record exact last lines and every CHAOS line, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/chaos -run TestHarnessRunsNamedFaultsWithinBounds` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-jev" :cards
     ((:id "false-bounce-detector" :tier "-" :needs ("hold-reason-classifier" "backfill-2026-10-04")
      :title "Readers bounced cards today for things that were not defects in the work: harness failures (ran=false, a git shim that swallowed a push, loopback denied, the wall refusing an exec) and trailer-only findings (a true model trailer read as fal"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: hold-reason-classifier,backfill-2026-10-04
PATHS: internal/decide/falsebounce*.go,internal/decide/testdata/falsebounce/**,cmd/nova-decide/main.go,docs/SPEC-NOVA-DECIDE.md
SHARED: docs/SPEC-NOVA-DECIDE.md
TEST: ./internal/decide TestFalseBounceFlagsHarnessFailuresAndTrailerOnlyFindings
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Readers bounced cards today for things that were not defects in the work: harness failures (ran=false, a git shim that swallowed a push, loopback denied, the wall refusing an exec) and trailer-only findings (a true model trailer read as false). A nova-decide decision falsebounce (schema plus verb nova-decide falsebounce --finding <file> --card <file>) flags a reader finding as harness-failure, trailer-only or real, with a probability. Seed the fixture set with today's cases (from the backfill's reports and judgments; at least one per class), evaluate with calibrate, report per-class precision and recall in REPORT.md. Shadow only. Jev and nova-decide lens for nova-sprint v1.0.0 (Glenn 2026-10-04 4:50 PM): support today's decisions, then shadow, evaluate, train and eventually use Jev; built on the existing nova-decide verbs (ask, read, score, attempt, grade, gate, brief, outcome, calibrate, findings), never duplicating them. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-NOVA-DECIDE.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/decide/falsebounce*.go,internal/decide/testdata/falsebounce/**,cmd/nova-decide/main.go,docs/SPEC-NOVA-DECIDE.md
  COMMIT: false-bounce-detector: nova-decide flags reader findings that are harness failures or trailer-only
  VERDICT: with the fixed backend the seeded harness-failure, trailer-only and real findings are classified as labelled; calibrate runs.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/decide -run 'TestFalseBounceFlagsHarnessFailuresAndTrailerOnlyFindings' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/decide`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/decide -run TestFalseBounceFlagsHarnessFailuresAndTrailerOnlyFindings` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "decide-eval-harness" :tier "-" :needs ("false-bounce-detector")
      :title "nova-decide eval --kind <k> [--set <file>] [--record <file>] [--every <duration>] evaluates a decision kind on its labelled set (default: the record's cases with outcomes): per-class precision and recall, calibration at each p threshold (0."
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: false-bounce-detector
PATHS: cmd/nova-decide/main.go,internal/decide/eval*.go,internal/decide/calibrate.go,cmd/nova-sprint/view.go,docs/SPEC-NOVA-DECIDE.md,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-NOVA-DECIDE.md,docs/SPEC-SPRINT.md
TEST: ./internal/decide TestEvalGivesPerClassPrecisionRecallCalibrationCostAndLatency
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. nova-decide eval --kind <k> [--set <file>] [--record <file>] [--every <duration>] evaluates a decision kind on its labelled set (default: the record's cases with outcomes): per-class precision and recall, calibration at each p threshold (0.5 to 0.95 by 0.05: coverage and accuracy above it), cost and latency (mean and p90) from the record; --every runs it as an installed loop (nightly by default) writing each result as a record. view coordinator shows the latest eval line per kind. Builds on calibrate (internal/decide/calibrate.go), never a second calibration. Jev and nova-decide lens for nova-sprint v1.0.0 (Glenn 2026-10-04 4:50 PM): support today's decisions, then shadow, evaluate, train and eventually use Jev; built on the existing nova-decide verbs (ask, read, score, attempt, grade, gate, brief, outcome, calibrate, findings), never duplicating them. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-NOVA-DECIDE.md, docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-decide/main.go,internal/decide/eval*.go,internal/decide/calibrate.go,cmd/nova-sprint/view.go,docs/SPEC-NOVA-DECIDE.md,docs/SPEC-SPRINT.md
  COMMIT: decide-eval-harness: nova-decide eval scores each kind per class, per threshold, with cost and latency
  VERDICT: on a fixture set eval prints per-class precision and recall and the threshold table; --every with a fake clock writes one record per period.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/decide -run 'TestEvalGivesPerClassPrecisionRecallCalibrationCostAndLatency' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/decide`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/decide -run TestEvalGivesPerClassPrecisionRecallCalibrationCostAndLatency` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "hold-reason-classifier" :tier "-" :needs ("jev-shadow-heavy-read" "backfill-2026-10-04")
      :title "HOLD reports are read by hand"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: jev-shadow-heavy-read,backfill-2026-10-04
PATHS: internal/decide/holdreason*.go,internal/decide/testdata/holdreason/**,cmd/nova-decide/main.go,docs/SPEC-NOVA-DECIDE.md
SHARED: docs/SPEC-NOVA-DECIDE.md
TEST: ./internal/decide TestHoldReasonClassesAndProposedPaths
Deadline: finish within 150 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. HOLD reports are read by hand. A nova-decide decision holdreason (a schema file in internal/decide/testdata/holdreason and a verb nova-decide hold --report <file>) classifies a HOLD report as paths-too-narrow, missing-dependency, already-done, work-defect or harness-failure, with a probability, and extracts the PATHS it proposes (a PATHS-PROPOSED line when present, else the paths the report names as needed). Label today's HOLDs (the backfill set) by hand into a fixture file, evaluate with nova-decide calibrate and report precision and recall per class in REPORT.md. Shadow only: nothing acts on it. Jev and nova-decide lens for nova-sprint v1.0.0 (Glenn 2026-10-04 4:50 PM): support today's decisions, then shadow, evaluate, train and eventually use Jev; built on the existing nova-decide verbs (ask, read, score, attempt, grade, gate, brief, outcome, calibrate, findings), never duplicating them. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-NOVA-DECIDE.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/decide/holdreason*.go,internal/decide/testdata/holdreason/**,cmd/nova-decide/main.go,docs/SPEC-NOVA-DECIDE.md
  COMMIT: hold-reason-classifier: nova-decide classifies HOLD reports and extracts proposed paths
  VERDICT: with the fixed backend each class's fixture is classified and its proposed paths extracted; calibrate runs over the labelled set.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/decide -run 'TestHoldReasonClassesAndProposedPaths' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/decide`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/decide -run TestHoldReasonClassesAndProposedPaths` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "shadow-to-act-promotion" :tier "-" :needs ("jev-shadow-judgments" "jev-shadow-heavy-read" "decide-eval-harness")
      :title "Each decision kind has a mode in nova-config: off, shadow or act (a decide_mode row per kind, default shadow; a migration at the next free number at your base, renumbered at land if taken)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: jev-shadow-judgments,jev-shadow-heavy-read,decide-eval-harness
PATHS: internal/config/migrations/*.sql,internal/config/kind.go,internal/decide/mode*.go,cmd/nova-sprint/answer.go,cmd/nova-sprint/decidelane.go,tla/*DecideMode*,tla/RUNS.tsv,docs/SPEC-NOVA-DECIDE.md
SHARED: docs/SPEC-NOVA-DECIDE.md
TEST: ./internal/decide TestAKindPromotesOnlyWithApprovalAndDemotesItself
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Each decision kind has a mode in nova-config: off, shadow or act (a decide_mode row per kind, default shadow; a migration at the next free number at your base, renumbered at land if taken). A kind moves to act only when its eval shows at least N cases (default 200) above X percent agreement (default 95) at p 0.95 and above, a cold audit is recorded passed, and Glenn's approval is recorded by name: never automatically up. It demotes to shadow automatically when rolling agreement over the last N cases drops under X, with one note to the coordinator. The answer and decide lanes act only on kinds in act. Model the mode first in tla/DecideMode.tla (states off, shadow, act; actions Promote guarded by evidence and approval, Demote on low agreement, Audit; invariant: act is never entered without an approval), run TLC on a Linux bench and merge the record into tla/RUNS.tsv with tlacheck merge --keep; cite the model from the code. docs/SPEC-NOVA-DECIDE.md also states the nova-config field. Jev and nova-decide lens for nova-sprint v1.0.0 (Glenn 2026-10-04 4:50 PM): support today's decisions, then shadow, evaluate, train and eventually use Jev; built on the existing nova-decide verbs (ask, read, score, attempt, grade, gate, brief, outcome, calibrate, findings), never duplicating them. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-NOVA-DECIDE.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/config/migrations/*.sql,internal/config/kind.go,internal/decide/mode*.go,cmd/nova-sprint/answer.go,cmd/nova-sprint/decidelane.go,tla/*DecideMode*,tla/RUNS.tsv,docs/SPEC-NOVA-DECIDE.md
  COMMIT: shadow-to-act-promotion: decision kinds have an off, shadow, act mode; promotion needs evidence and approval, demotion is automatic
  VERDICT: a kind with evidence but no approval stays shadow, with approval moves to act, and falls back to shadow when agreement drops; TLC passes DecideMode.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/decide -run 'TestAKindPromotesOnlyWithApprovalAndDemotesItself' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/decide`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/decide -run TestAKindPromotesOnlyWithApprovalAndDemotesItself` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-yes" :cards
     ((:id "merge-tree-root" :tier "-" :needs ("merge-tree-node" "merge-records-by-id-and-head") :who "friend.johnny"
      :title "nova-sprint \"feels good\" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator only answers judgments)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
WHO: friend johnny
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: merge-tree-node,merge-records-by-id-and-head
PATHS: cmd/nova-sprint/landroot*.go,cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,cmd/nova-sprint/landgate.go,cmd/nova-sprint/landledger.go,docs/SPEC-SPRINT.md
SHARED: cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestLanderGathersGreenStreamBranchesSerially
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint \"feels good\" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator only answers judgments). The merge tree, root level. The server's lander gathers the green land/<stream> branches the nodes pushed (card merge-tree-node) serially onto the base: one stream at a time, merge land/<stream> onto the current base, and push to the base with a non-forced push, which is the compare-and-swap (a refused push means the base moved). If the base moved since the node gated, the root runs the full gate again on the merged result before pushing; a red re-gate sends that stream back to its node, naming the gate line, and the root goes on to the next stream. Every landing is recorded by card id plus head, using the records of card merge-records-by-id-and-head. Keep the existing per-card lander working unchanged; the switch between them is card merge-tree-switch. Read the REPORT of branch rowan/merge-tree-proof first (tla/LandTwoLevel.tla, the two-level land model); cite tla/LandTwoLevel.tla from the function; where the code and the model disagree, that is a HOLD naming the disagreement, never a silent change to either. Cite docs/SPEC-SPRINT.md from the function. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, the REPORT and tla/LandTwoLevel.tla on origin/rowan/merge-tree-proof, and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes and a temporary origin; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/landroot*.go,cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,cmd/nova-sprint/landgate.go,cmd/nova-sprint/landledger.go,docs/SPEC-SPRINT.md
  COMMIT: merge-tree-root: the lander gathers green land/<stream> branches serially onto the base
  VERDICT: on the twin store with a temporary origin, the root lands two green land/<stream> branches one after the other onto the base, each by a non-forced push; when the base moves between the gate and the push the push is refused, the root re-gates the whole result on the new base and lands it; each landing records the cards by id plus head.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestLanderGathersGreenStreamBranchesSerially' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestLanderGathersGreenStreamBranchesSerially` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "merge-tree-switch" :tier "-" :needs ("merge-tree-shadow") :who "friend.johnny"
      :title "nova-sprint \"feels good\" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator only answers judgments)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
WHO: friend johnny
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: merge-tree-shadow
PATHS: internal/sprint/settings.go,internal/sprint/settings*_test.go,cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
SHARED: cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./cmd/nova-sprint TestSettingSwitchesLandingToTheTreeAndBack
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint \"feels good\" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator only answers judgments). The merge tree, switch. A sprint setting in internal/sprint/settings.go, `nova-sprint set --land <tree|default>`, beside dealt_max and read_tier, chooses the landing path: tree (land-node per stream, then the root) or default (the existing per-card lander). Rollback is setting it back: a switch takes effect between landings, never inside one; a stream with a pushed land/<stream> not yet gathered when the setting returns to default has its cards re-queued for the lander, none lost and none landed twice. where and the dashboard show the mode. A clear starts the next epoch on default, as it does every property. Read the REPORT of branch rowan/merge-tree-proof first (tla/LandTwoLevel.tla, the two-level land model); cite tla/LandTwoLevel.tla from the function; where the code and the model disagree, that is a HOLD naming the disagreement, never a silent change to either. Cite docs/SPEC-SPRINT.md from the function. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, the REPORT and tla/LandTwoLevel.tla on origin/rowan/merge-tree-proof, and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes and a temporary origin; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/settings.go,internal/sprint/settings*_test.go,cmd/nova-sprint/land.go,cmd/nova-sprint/landloop.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
  COMMIT: merge-tree-switch: a sprint setting switches landing to the merge tree, with rollback
  VERDICT: on the twin store, set --land tree makes the next landing go through the tree (nodes then root), set --land default returns to the per-card lander with no card lost or landed twice, and a landing in flight at the switch finishes under the mode it started in.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestSettingSwitchesLandingToTheTreeAndBack' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestSettingSwitchesLandingToTheTreeAndBack` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "merge-tree-shadow" :tier "-" :needs ("merge-tree-root") :who "friend.johnny"
      :title "nova-sprint \"feels good\" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator only answers judgments)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
WHO: friend johnny
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: merge-tree-root
PATHS: cmd/nova-sprint/landshadow*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
SHARED: cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
TEST: ./cmd/nova-sprint TestTreeShadowReportsTheGapAgainstTheLander
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint \"feels good\" (Glenn 2026-10-04: high-priority cards; Rowan's bar: the merge tree is landed and drains the queue within a bound; fsck clean for 24 hours; no friend stalls; the coordinator only answers judgments). The merge tree, shadow. A read-only verb `nova-sprint land shadow [--json]` (or the nearest verb shape the tree already uses) that computes what the merge tree (cards merge-tree-node and merge-tree-root) would land on the live queue, against what the current lander lands, and reports the gap (cards landed by one and not the other, and order differences) and the timing (per stream and end to end, the queue drain time of each). It writes nothing: no store row, no push, no ref outside a clone it keeps under the sprint root. The report is what Rowan reads before card merge-tree-switch is released. Read the REPORT of branch rowan/merge-tree-proof first (tla/LandTwoLevel.tla, the two-level land model); cite tla/LandTwoLevel.tla from the function; where the code and the model disagree, that is a HOLD naming the disagreement, never a silent change to either. Cite docs/SPEC-SPRINT.md from the function. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, the REPORT and tla/LandTwoLevel.tla on origin/rowan/merge-tree-proof, and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes and a temporary origin; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/landshadow*.go,cmd/nova-sprint/verbs.go,cmd/nova-sprint/verbhelp.go,docs/SPEC-SPRINT.md,docs/CLI.md
  COMMIT: merge-tree-shadow: tree shadow: what the merge tree would land against what the lander lands, read-only
  VERDICT: on the twin store with a temporary origin, the shadow computes the tree's landings for the live queue and the lander's actual landings, prints the gap (cards one lands and the other does not, and the order) and the timing of each, and writes nothing to the store or to any remote branch.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestTreeShadowReportsTheGapAgainstTheLander' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestTreeShadowReportsTheGapAgainstTheLander` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "dead" :cards
     ((:id "dead-pkg-sprint-store-t2b" :tier "-" :needs ("dead-wave3b" "dead-pkg-subproc-pkg-sprint-t2b")
      :title "A tree of 1 work step (STEP 2 to STEP 2), walked in order"
      :brief "RESULT: dead-pkg-sprint-store-t2b sha=36f4c5ba6371 tier: flash
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: sweep
CLASS: model
DEPENDS-ON: dead-wave3b,dead-pkg-subproc-pkg-sprint-t2b
PATHS: internal/sprint/store/*.go, internal/ci/testdata/dead_code_allowlist.txt, internal/ci/testdata/deleted-tests.txt
TEST: internal/sprint/store TestTheDecidedLineTheStoreWritesForAnAckNamesItsSubjects
Deadline: finish within 30 minutes.

You are a child of the coordinator: one task, one checkout, one branch, unattended; this card is the whole task. You start in the job directory (export JOB=$PWD): read $JOB/JOB.md first, work in the staged checkout it names (a full clone at BASE), commit as usual and finish as it says. No gh, no push: JOB.md's finish does both.
Libraries considered: none; this card deletes unreachable code and writes nothing new.
The contract is docs/STANDARD.md (embedded in AGENTS.md): read the sections a step names first; where this card and it disagree, it wins and the PR body says so.
Steps marked (hard) were dealt pro in the flat wave and may need pro; the card is dealt flash first and the machine escalates, never you.

THE TASK. A tree of 1 work step (STEP 2 to STEP 2), walked in order. Each work step carries its own PATHS:, COMMIT: and VERDICT: lines: do it, run its check, commit only its PATHS with its COMMIT: line as the message, and write its verdict line in the PR body and RESULT, `step <n>: <ok|broken|not-done|skipped> <the full 40-hex commit sha pasted from git rev-parse HEAD, or - when nothing changed; a short sha is refused: e.g. step 2: ok 3b1f0c2a9d4e7f6a5c8b1d0e2f3a4b5c6d7e8f90 the change> <one line>` (ok with `-` and the commit that did it when the head already holds the step). The first step that is not ok stops the walk: steps before it land, and the coordinator turns the rest into a new card. Each step deletes code no shipped tool reaches in one package (docs/STANDARD.md section 7; class test TestDeadCode, ledger internal/ci/testdata/dead_code_allowlist.txt). Deadcode runs as the class test runs it, built natively: `go tool deadcode -h >/dev/null 2>&1; B=$(go tool -n deadcode); GOOS=<os> $B ./cmd/...` for linux, darwin and windows (`GOOS=<os> go tool deadcode` cannot run: exec format error). Per listed name: confirm it unreachable on all three, delete it and every helper, type, variable and constant only it used. A test pinning only deleted code goes; a test file left empty is deleted with a row in internal/ci/testdata/deleted-tests.txt (`<path> dead: <why>`). KEEP and name in the PR body: names the step marks KEEP, interface methods (String, Error, MarshalJSON and the like), functions another package's tests call. Each step's commit carries its own ledger lines: after its deletions run `NOVA_CI_UPDATE=1 nice -n 19 go test -p 2 -count=1 -timeout 600s ./internal/ci/ -run 'TestDeadCode|TestDeletedTests'` and keep only its package's row and its deleted-tests.txt rows.

HOW THIS CARD IS JUDGED. (1) Only code no shipped tool reaches is deleted: deadcode under linux, darwin and windows agrees at the head. (2) An interface method (String, Format, MarshalJSON, Error and the like) and a helper another package's tests call are kept and named in the PR body. (3) Nothing is rewritten: the diff is deletions, plus the ledger row and any deleted-tests.txt rows. (4) Every package that imports these still builds (`go build ./...`, `go vet ./...`). (5) Vet and build hold under linux, windows and darwin.

STEP 1. cd into the staged checkout JOB.md names (cd <that path>) and run git log --oneline -1: the head is 36f4c5ba6371 or a later commit of BASE. Export GOCACHE=$JOB/gocache GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Sites and counts here were read at 36f4c5ba6371: find a moved line by its text.
STEP 2. (hard) dead code in internal/sprint/store.
  PATHS: internal/sprint/store/*.go, internal/ci/testdata/dead_code_allowlist.txt, internal/ci/testdata/deleted-tests.txt
  COMMIT: dead-pkg-sprint-store-t2b: dead code in internal/sprint/store
  VERDICT: ok when its package's per-OS deadcode counts fell and its tests pass; its commit carries its ledger row; not-done or broken naming what remains
In `internal/sprint/store`, dead at the base under linux, darwin and windows alike: `DealStep` (internal/sprint/store/steps.go:55, named in this package's tests); `SentinelsDueStep` (internal/sprint/store/steps.go:186, named in this package's tests); `TwinDiff` (internal/sprint/store/twin.go:758, named in this package's tests); `count` (internal/sprint/store/ops.go:308, named in this package's tests).
STEP 3. Read `git diff -U0` from the start commit against HOW THIS CARD IS JUDGED, hunk by hunk; fix in the step that owns the hunk.
STEP 4. Run the gate once, each line as written, keeping each last line: `gofmt -l internal/sprint/store   (prints nothing)` ; `nice -n 19 go vet ./internal/sprint/store/` ; `nice -n 19 go test -p 2 -count=1 -timeout 600s ./internal/sprint/store/ ./internal/ci/` ; `go tool deadcode -h >/dev/null 2>&1; B=$(go tool -n deadcode); for os in linux darwin windows; do echo $os; GOOS=$os $B ./cmd/... | grep -oE \"(internal/sprint/store)/\" | sort | uniq -c; done   (per OS and package, lower by what went; no line for a package means none is left; an error is a failed run, never a pass)` ; `for os in linux windows darwin; do GOOS=$os go vet ./internal/sprint/store/ && GOOS=$os go build ./... || echo FAIL $os; done   (prints no FAIL)`. If a test in ./internal/ci/ fails naming a ledger row (\"delete the stale entry\", \"below allowed count\", \"shrink the count\", \"lower the row\"), run `NOVA_CI_UPDATE=1 nice -n 19 go test -p 2 -count=1 -timeout 600s ./internal/ci/ -run '<the failing tests, joined with |>'`, then the gate again. Keep only the ledger lines of files PATHS names and the shards internal/ci/testdata/<ledger>/<package>.txt of its packages; restore any other it rewrote; name a needed one (partial). A row only shrinks. Not yours: TestGeneralityText red on two stale SPEC-BUS-DELIVERY.md rows (dead-generality-text-t2 deletes them).
STEP 5. The pull request: title `dead-pkg-sprint-store-t2b: dead code in internal/sprint/store`; the body gives the diff stat, what was deleted, each step's verdict line, the gate lines with their last lines, each test added and what it pins, the three measures of docs/STANDARD.md (minimal: lines before and after; performant: a number the gate prints; correct: the test or model holding the behaviour) and what was not done, and ends with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
STEP 6. End as JOB.md says: its `gh pr create` is the end, or RESULT.md in JOB.md's shape (head, branch, verdict, gate, output, report, and the step lines); never a green claim.

AS A READ (only when JOB.md's first line is `# JOB: read <card>, attempt <n>`; otherwise you are the work): change nothing; diff from the start commit JOB.md names. THE GATE PROVES: per-OS deadcode counts fell by what went, vet and build hold on all three OSes, tests and class tests pass, ledger rows fell. Run it once (STEP 4). READ ONLY: each KEEP named in the PR body is one the step marks, an interface method or a cross-package test helper; and spot-check two deletions per step with `git grep -nw <name>` at the start commit, OS-specific files included: no caller outside the deleted code. And: only PATHS, NEW and STEP 4's ledger lines changed; a deleted test has its deleted-tests.txt row; one commit per work step. Approve (`gh pr review --approve`) only when all hold, else `gh pr review --request-changes --body <file:line and what is wrong>`.

RULES.
Work only in the staged checkout JOB.md names; do not clone.
Commit on the checkout's own branch, as usual; touch no other branch.
Export a private GOCACHE (the path this card names) and GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command.
Never `go clean`, and never clean a shared cache.
NEVER start a redis-server on this machine.
Never kill a process you did not start.
Every `go test` gets `-timeout 600s`.
No `rm -rf` outside the job directory.
Never force-push.
Never rebase.
Do not use git stash (the stash list is shared by every worktree).
Functional tests (any test that needs Redis) run ONLY inside the container through `tools/functionalrun` (`--fresh-gocache --deadline 15m`), never against any other store.
Every new test opens with `t.Parallel()`.
Run `go test -count=1 -timeout 600s ./internal/ci/` before you finish.
No names of people, machines or friends in code, comments or docs.
Docs and comments in the present tense.
Cite the model or the design section from every function that implements a rule.
Touch only the files this card names; a fix that needs another file goes into your report as a proposed diff, not a commit.
Keep the diff minimal: every added line traceable to one sentence of this card.
Commit messages end with `Co-Authored-By: Claude <your model> <noreply@anthropic.com>`.
PR bodies end with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
Finish as JOB.md says, with `gh pr create` against the base this card names; never merge.
The PR body states the diff stat and what was deleted.
The PR body lists the tests, each with what it pins, and every local helper added.
The report is the PR: its title is the one-line report, its body every test package line and what you could not do and why.
\"Not done\" is a welcome report; a green claim you did not run is not.
A card that builds code carries a filled line `Libraries considered: <what the standard library and the adopted modules offered, and why each was used or not>` (docs/STANDARD.md section 7).
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never start a server on this machine.
Report what was not done.")
      (:id "dead-pkg-subproc-pkg-sprint-t2b" :tier "-" :needs ("dead-wave3b" "dead-pkg-fleet-pkg-nogh-t2b")
      :title "A tree of 2 work steps (STEP 2 to STEP 3), walked in order"
      :brief "RESULT: dead-pkg-subproc-pkg-sprint-t2b sha=36f4c5ba6371 tier: flash
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: sweep
CLASS: model
DEPENDS-ON: dead-wave3b,dead-pkg-fleet-pkg-nogh-t2b
PATHS: internal/subproc/*.go, internal/ci/testdata/dead_code_allowlist.txt, internal/ci/testdata/deleted-tests.txt, internal/sprint/*.go
TEST: internal/subproc TestEveryKindKillsASlowChildAtItsDeadline
Deadline: finish within 60 minutes.

You are a child of the coordinator: one task, one checkout, one branch, unattended; this card is the whole task. You start in the job directory (export JOB=$PWD): read $JOB/JOB.md first, work in the staged checkout it names (a full clone at BASE), commit as usual and finish as it says. No gh, no push: JOB.md's finish does both.
Libraries considered: none; this card deletes unreachable code and writes nothing new.
The contract is docs/STANDARD.md (embedded in AGENTS.md): read the sections a step names first; where this card and it disagree, it wins and the PR body says so.
Steps marked (hard) were dealt pro in the flat wave and may need pro; the card is dealt flash first and the machine escalates, never you.

THE TASK. A tree of 2 work steps (STEP 2 to STEP 3), walked in order. Each work step carries its own PATHS:, COMMIT: and VERDICT: lines: do it, run its check, commit only its PATHS with its COMMIT: line as the message, and write its verdict line in the PR body and RESULT, `step <n>: <ok|broken|not-done|skipped> <the full 40-hex commit sha pasted from git rev-parse HEAD, or - when nothing changed; a short sha is refused: e.g. step 2: ok 3b1f0c2a9d4e7f6a5c8b1d0e2f3a4b5c6d7e8f90 the change> <one line>` (ok with `-` and the commit that did it when the head already holds the step). The first step that is not ok stops the walk: steps before it land, and the coordinator turns the rest into a new card. Each step deletes code no shipped tool reaches in one package (docs/STANDARD.md section 7; class test TestDeadCode, ledger internal/ci/testdata/dead_code_allowlist.txt). Deadcode runs as the class test runs it, built natively: `go tool deadcode -h >/dev/null 2>&1; B=$(go tool -n deadcode); GOOS=<os> $B ./cmd/...` for linux, darwin and windows (`GOOS=<os> go tool deadcode` cannot run: exec format error). Per listed name: confirm it unreachable on all three, delete it and every helper, type, variable and constant only it used. A test pinning only deleted code goes; a test file left empty is deleted with a row in internal/ci/testdata/deleted-tests.txt (`<path> dead: <why>`). KEEP and name in the PR body: names the step marks KEEP, interface methods (String, Error, MarshalJSON and the like), functions another package's tests call. Each step's commit carries its own ledger lines: after its deletions run `NOVA_CI_UPDATE=1 nice -n 19 go test -p 2 -count=1 -timeout 600s ./internal/ci/ -run 'TestDeadCode|TestDeletedTests'` and keep only its package's row and its deleted-tests.txt rows.

HOW THIS CARD IS JUDGED. (1) Only code no shipped tool reaches is deleted: deadcode under linux, darwin and windows agrees at the head. (2) An interface method (String, Format, MarshalJSON, Error and the like) and a helper another package's tests call are kept and named in the PR body. (3) Nothing is rewritten: the diff is deletions, plus the ledger row and any deleted-tests.txt rows. (4) Every package that imports these still builds (`go build ./...`, `go vet ./...`). (5) Vet and build hold under linux, windows and darwin.

STEP 1. cd into the staged checkout JOB.md names (cd <that path>) and run git log --oneline -1: the head is 36f4c5ba6371 or a later commit of BASE. Export GOCACHE=$JOB/gocache GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Sites and counts here were read at 36f4c5ba6371: find a moved line by its text.
STEP 2. (hard) dead code in internal/subproc.
  PATHS: internal/subproc/*.go, internal/ci/testdata/dead_code_allowlist.txt, internal/ci/testdata/deleted-tests.txt
  COMMIT: dead-pkg-subproc-pkg-sprint-t2b: dead code in internal/subproc
  VERDICT: ok when its package's per-OS deadcode counts fell and its tests pass; its commit carries its ledger row; not-done or broken naming what remains
In `internal/subproc`, dead at the base under linux, darwin and windows alike: `Kind.String` (internal/subproc/subproc.go:79, named in this package's tests).
STEP 3. (hard) dead code in internal/sprint.
  PATHS: internal/sprint/*.go, internal/ci/testdata/dead_code_allowlist.txt, internal/ci/testdata/deleted-tests.txt
  COMMIT: dead-pkg-subproc-pkg-sprint-t2b: dead code in internal/sprint
  VERDICT: ok when its package's per-OS deadcode counts fell and its tests pass; its commit carries its ledger row; not-done or broken naming what remains
In `internal/sprint`, dead at the base under linux, darwin and windows alike: `IsState` (internal/sprint/lifecycle.go:81); `NeedsCycle` (internal/sprint/cycle.go:300, named in this package's tests); `ParseWorkCard` (internal/sprint/schema.go:261, named in this package's tests); `SentinelsDue` (internal/sprint/steps_sentinel.go:195, named in this package's tests); `Tick` (internal/sprint/steps_tick.go:255, named in this package's tests); `ValidSync` (internal/sprint/fleet_sync.go:94, named in this package's tests).
STEP 4. Read `git diff -U0` from the start commit against HOW THIS CARD IS JUDGED, hunk by hunk; fix in the step that owns the hunk.
STEP 5. Run the gate once, each line as written, keeping each last line: `gofmt -l internal/subproc internal/sprint   (prints nothing)` ; `nice -n 19 go vet ./internal/subproc/ ./internal/sprint/` ; `nice -n 19 go test -p 2 -count=1 -timeout 600s ./internal/subproc/ ./internal/sprint/ ./internal/ci/` ; `go tool deadcode -h >/dev/null 2>&1; B=$(go tool -n deadcode); for os in linux darwin windows; do echo $os; GOOS=$os $B ./cmd/... | grep -oE \"(internal/subproc|internal/sprint)/\" | sort | uniq -c; done   (per OS and package, lower by what went; no line for a package means none is left; an error is a failed run, never a pass)` ; `for os in linux windows darwin; do GOOS=$os go vet ./internal/subproc/ ./internal/sprint/ && GOOS=$os go build ./... || echo FAIL $os; done   (prints no FAIL)`. If a test in ./internal/ci/ fails naming a ledger row (\"delete the stale entry\", \"below allowed count\", \"shrink the count\", \"lower the row\"), run `NOVA_CI_UPDATE=1 nice -n 19 go test -p 2 -count=1 -timeout 600s ./internal/ci/ -run '<the failing tests, joined with |>'`, then the gate again. Keep only the ledger lines of files PATHS names and the shards internal/ci/testdata/<ledger>/<package>.txt of its packages; restore any other it rewrote; name a needed one (partial). A row only shrinks. Not yours: TestGeneralityText red on two stale SPEC-BUS-DELIVERY.md rows (dead-generality-text-t2 deletes them).
STEP 6. The pull request: title `dead-pkg-subproc-pkg-sprint-t2b: dead code in internal/subproc and 1 more step`; the body gives the diff stat, what was deleted, each step's verdict line, the gate lines with their last lines, each test added and what it pins, the three measures of docs/STANDARD.md (minimal: lines before and after; performant: a number the gate prints; correct: the test or model holding the behaviour) and what was not done, and ends with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
STEP 7. End as JOB.md says: its `gh pr create` is the end, or RESULT.md in JOB.md's shape (head, branch, verdict, gate, output, report, and the step lines); never a green claim.

AS A READ (only when JOB.md's first line is `# JOB: read <card>, attempt <n>`; otherwise you are the work): change nothing; diff from the start commit JOB.md names. THE GATE PROVES: per-OS deadcode counts fell by what went, vet and build hold on all three OSes, tests and class tests pass, ledger rows fell. Run it once (STEP 5). READ ONLY: each KEEP named in the PR body is one the step marks, an interface method or a cross-package test helper; and spot-check two deletions per step with `git grep -nw <name>` at the start commit, OS-specific files included: no caller outside the deleted code. And: only PATHS, NEW and STEP 5's ledger lines changed; a deleted test has its deleted-tests.txt row; one commit per work step. Approve (`gh pr review --approve`) only when all hold, else `gh pr review --request-changes --body <file:line and what is wrong>`.

RULES.
Work only in the staged checkout JOB.md names; do not clone.
Commit on the checkout's own branch, as usual; touch no other branch.
Export a private GOCACHE (the path this card names) and GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command.
Never `go clean`, and never clean a shared cache.
NEVER start a redis-server on this machine.
Never kill a process you did not start.
Every `go test` gets `-timeout 600s`.
No `rm -rf` outside the job directory.
Never force-push.
Never rebase.
Do not use git stash (the stash list is shared by every worktree).
Functional tests (any test that needs Redis) run ONLY inside the container through `tools/functionalrun` (`--fresh-gocache --deadline 15m`), never against any other store.
Every new test opens with `t.Parallel()`.
Run `go test -count=1 -timeout 600s ./internal/ci/` before you finish.
No names of people, machines or friends in code, comments or docs.
Docs and comments in the present tense.
Cite the model or the design section from every function that implements a rule.
Touch only the files this card names; a fix that needs another file goes into your report as a proposed diff, not a commit.
Keep the diff minimal: every added line traceable to one sentence of this card.
Commit messages end with `Co-Authored-By: Claude <your model> <noreply@anthropic.com>`.
PR bodies end with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
Finish as JOB.md says, with `gh pr create` against the base this card names; never merge.
The PR body states the diff stat and what was deleted.
The PR body lists the tests, each with what it pins, and every local helper added.
The report is the PR: its title is the one-line report, its body every test package line and what you could not do and why.
\"Not done\" is a welcome report; a green claim you did not run is not.
A card that builds code carries a filled line `Libraries considered: <what the standard library and the adopted modules offered, and why each was used or not>` (docs/STANDARD.md section 7).
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never start a server on this machine.
Report what was not done.")))
    (:stream "sprint-v1-safety" :cards
     ((:id "buds-in-the-wall" :tier "-" :needs ("claude-oneshot-lanesb")
      :title "The bud runners on the Studio (/Volumes/nova/ai/buds/<bud>/runner.zsh and reader.zsh, stopgaps until claude-oneshot-lanes lands) run claude -p --permission-mode bypassPermissions with no wall: a bud can write anywhere the user can, includin"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: claude-oneshot-lanes
PATHS: internal/friend/lanes.go,internal/friend/adapter.go,internal/friend/lane_wall_test.go,internal/sandbox/profile.go,internal/sandbox/policy.go,cmd/nova-friend/main.go,docs/SPEC-FRIEND.md,docs/SPEC-SANDBOX.md
SHARED: docs/SPEC-FRIEND.md,docs/SPEC-SANDBOX.md
TEST: ./internal/friend TestALaneRunsInsideItsWallProfile
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. The bud runners on the Studio (/Volumes/nova/ai/buds/<bud>/runner.zsh and reader.zsh, stopgaps until claude-oneshot-lanes lands) run claude -p --permission-mode bypassPermissions with no wall: a bud can write anywhere the user can, including the coordinator's own self. A nova-friend lane takes a wall profile from its friend row (profile <name>, default the friend profile): every lane child runs inside the wall with writes allowed only to the friend's working dir, its job dirs and its config dir (CLAUDE_CONFIG_DIR), network only to github.com and to the bench hosts over ssh, and writes DENIED, whatever else is allowed, to ~/rowan-working/rowan-new, /Volumes/nova/ai/rowan/working/rowan-new, and in particular their memory/, identity/ and MEMORY-*.md (the deny list is part of the profile in internal/sandbox/profile.go, documented in docs/SPEC-SANDBOX.md). The test runs a fake harness inside a lane's wall on a Linux bench and asserts a write to the job dir succeeds and a write to a denied path and an outside network connection are refused. The runner scripts themselves are outside this repository: say in REPORT.md the lane flags that replace them, for the adopter. Safety lens on nova-sprint and its tools for v1.0.0 (Glenn 2026-10-04 4:45 PM): no worker can reach what is not its own, and nothing destructive happens by pattern or by accident. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-FRIEND.md, docs/SPEC-SANDBOX.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: internal/friend/lanes.go,internal/friend/adapter.go,internal/friend/lane_wall_test.go,internal/sandbox/profile.go,internal/sandbox/policy.go,cmd/nova-friend/main.go,docs/SPEC-FRIEND.md,docs/SPEC-SANDBOX.md
  COMMIT: buds-in-the-wall: nova-friend lanes run inside a wall profile that denies the coordinator's self
  VERDICT: a fake harness in a lane writes its job dir and is refused a write under rowan-new/memory and a connection outside the allow list.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/friend -run 'TestALaneRunsInsideItsWallProfile' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/friend`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/friend -run TestALaneRunsInsideItsWallProfile` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "protected-bases" :tier "-" :needs ("sn-landing-branch-is-sprint")
      :title "Cards with BASE dev were landed straight onto dev today and ejected the promotion cut three times"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 120 minutes
DEPENDS-ON: sn-landing-branch-is-sprint
PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/land.go,internal/sprint/streams.go,cmd/nova-sprint/protected_base_test.go,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-SPRINT.md
TEST: ./cmd/nova-sprint TestAddAndLandRefuseDevAndMainOutsideAPromotionStream
Deadline: finish within 120 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits end with the trailer Co-Authored-By: Claude <model> <noreply@anthropic.com> as nova-tools AGENTS.md and docs/STANDARD.md require, naming the actual model, with a line `By: <who did the work>` above it; name your actual model and harness, never claim one you are not.

THE TASK. Cards with BASE dev were landed straight onto dev today and ejected the promotion cut three times. sn-landing-branch-is-sprint (in review at this cut, not landed) refuses an add with BASE dev outside the promotion stream; read what it landed at your base first and add only what is missing: the same refusal for main; stream set <s> --promotion (and --promotion=false), kept on the stream and shown in where; land refusing, before any git, a batch whose base is dev or main on a stream not marked promotion, naming the stream and the flag. If sn-landing-branch-is-sprint has not landed at your base, HOLD naming it. Cite docs/SPEC-SPRINT.md. Safety lens on nova-sprint and its tools for v1.0.0 (Glenn 2026-10-04 4:45 PM): no worker can reach what is not its own, and nothing destructive happens by pattern or by accident. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards also edit docs/SPEC-SPRINT.md (the SHARED line): add your text as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Write the red test first and see it fail at the base, then do the task above; tests use the twin store, fakes and an injected clock, open no socket to a shared service and never sleep on the wall clock.
  PATHS: cmd/nova-sprint/verbs.go,cmd/nova-sprint/land.go,internal/sprint/streams.go,cmd/nova-sprint/protected_base_test.go,docs/SPEC-SPRINT.md
  COMMIT: protected-bases: add and land refuse dev and main outside a promotion stream
  VERDICT: on the twin store add of a BASE main card to a plain stream is refused, taken on a promotion stream; land of a dev batch on a plain stream is refused before git.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./cmd/nova-sprint -run 'TestAddAndLandRefuseDevAndMainOutsideAPromotionStream' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./cmd/nova-sprint ./internal/sprint`, record exact last lines (the red run at the base too), and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestAddAndLandRefuseDevAndMainOutsideAPromotionStream` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "sprint-v1-setup" :cards
     ((:id "sprint-local-only-mode" :tier "-" :needs ("dep-tailnet" "setup-nova-up-local")
      :title "nova-sprint v1.0.0, lens setup (Glenn 2026-10-04: a stranger sets the whole thing up from the docs and the tools, with nothing hidden; no bash, zsh or Python in anything that ships; nothing the coordinator needs is a Rowan-only tool)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: dep-tailnet,setup-nova-up-local
PATHS: internal/sprint/addr*.go,cmd/nova-sprint/main.go,internal/doctor/check_tailnet.go,docs/SPEC-SPRINT.md,docs/SETUP.md
SHARED: docs/SPEC-SPRINT.md,docs/SETUP.md
TEST: ./internal/sprint TestLocalOnlyModeRefusesEveryNonLoopbackAddressAndNeedsNoTailnet
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-mas) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens setup (Glenn 2026-10-04: a stranger sets the whole thing up from the docs and the tools, with nothing hidden; no bash, zsh or Python in anything that ships; nothing the coordinator needs is a Rowan-only tool). nova-sprint and its stores accept only loopback or tailnet addresses (nova-bus's help says so; find where nova-sprint checks an address and gather it into one function in internal/sprint/addr.go, cited from each caller). A stranger on one machine has no tailnet. Add a local-only mode (a sprint row setting and `NOVA_SPRINT_LOCAL=1` for a process; one of them, say which and why in the spec): every address must be loopback, a tailnet or other address is refused naming the mode, and nothing asks for a tailnet; nova-up --local (card setup-nova-up-local) sets it; nova-doctor's tailnet check (card dep-tailnet, which this follows) skips under it with the reason. docs/SPEC-SPRINT.md states the address rule and the mode; docs/SETUP.md says when to use it. The red test first: in local-only mode a tailnet address is refused and a loopback one accepted; outside it both are accepted and a public address is refused. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-SPRINT.md,docs/SETUP.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: internal/sprint/addr*.go,cmd/nova-sprint/main.go,internal/doctor/check_tailnet.go,docs/SPEC-SPRINT.md,docs/SETUP.md
  COMMIT: sprint-local-only-mode: the tailnet requirement, stated, with a local-only mode that needs none
  VERDICT: nova-sprint states its address rule in one place (loopback or tailnet), a local-only mode refuses every non-loopback address and needs no tailnet, and nova-doctor --local skips the tailnet check naming this mode.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprint -run 'TestLocalOnlyModeRefusesEveryNonLoopbackAddressAndNeedsNoTailnet' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprint ./cmd/nova-sprint ./internal/doctor`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprint -run TestLocalOnlyModeRefusesEveryNonLoopbackAddressAndNeedsNoTailnet` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")
      (:id "sprint-dashboard-verb" :tier "-" :needs ("setup-nova-doctor" "simp-stopgap-register")
      :title "nova-sprint v1.0.0, lens setup (Glenn 2026-10-04: a stranger sets the whole thing up from the docs and the tools, with nothing hidden; no bash, zsh or Python in anything that ships; nothing the coordinator needs is a Rowan-only tool)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 240 minutes
DEPENDS-ON: setup-nova-doctor,simp-stopgap-register
PATHS: cmd/nova-sprint/dashboard*.go,internal/sprintdash/**,internal/doctor/check_dashboard*.go,docs/SPEC-SPRINT-DASHBOARD.md,docs/STOPGAPS.md,docs/SETUP.md
SHARED: docs/STOPGAPS.md,docs/SETUP.md
TEST: ./internal/sprintdash TestDashboardServesWhatServerPyServedFromOnePoller
Deadline: finish within 240 minutes.
Libraries considered: the tree's own packages and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: rowan-space) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-sprint v1.0.0, lens setup (Glenn 2026-10-04: a stranger sets the whole thing up from the docs and the tools, with nothing hidden; no bash, zsh or Python in anything that ships; nothing the coordinator needs is a Rowan-only tool). The public dashboard Glenn looks at is served by a Python stopgap, ~/nova-bench/dashboard/server.py (371 lines; launchd com.nova.dashboard.public), beside the Go verb `nova-sprint dashboard` (cmd/nova-sprint/dashboard.go, internal/sprintdash) that serves the same page. Read server.py and list its behaviours in its docs/STOPGAPS.md row (card simp-stopgap-register made the register): one poller running the snapshot read back to back with a minimum interval so reads never overlap however many pages are open, pages reading only the cache, a failed poll keeping the last good snapshot and marking it stale, the logo, raster logo, tiles and favicon routes, the landed sample, the build id, a read-time log line once a minute, and whatever else it does. Give the verb each one it lacks, each with a test on the twin store with an injected clock (no listener in unit tests; the existing functional tests show how the verb is run), then mark the row retired with each behaviour's test. Add the doctor check internal/doctor/check_dashboard.go (card setup-nova-doctor's frame): the dashboard's loop record is installed and its port answers on loopback; docs/SETUP.md gets its section. The dashboard runs as a loop record, never a hand plist. Do not touch server.py or its plist; REPORT.md ends with the adopter steps to switch the public dashboard to the verb. Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/STOPGAPS.md,docs/SETUP.md (the SHARED line): add your paragraph as its own subsection headed with this card id, under the section it belongs to, and change no other paragraph of it.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the docs named in PATHS first.

STEP 2. Do the task above, writing the red test first on the twin store or with fakes; no test opens a real socket to a shared service or sleeps on the wall clock.
  PATHS: cmd/nova-sprint/dashboard*.go,internal/sprintdash/**,internal/doctor/check_dashboard*.go,docs/SPEC-SPRINT-DASHBOARD.md,docs/STOPGAPS.md,docs/SETUP.md
  COMMIT: sprint-dashboard-verb: the live dashboard is nova-sprint dashboard alone; server.py is retired
  VERDICT: `nova-sprint dashboard` does everything ~/nova-bench/dashboard/server.py does (one back-to-back poller with a floor, a cached snapshot, stale marking on a failed poll, the logo and favicon routes, the landed sample, the read-time log), each behaviour with a test; a doctor check covers the dashboard; the server.py row of docs/STOPGAPS.md is retired with its citations.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/sprintdash -run 'TestDashboardServesWhatServerPyServedFromOnePoller' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/sprintdash ./cmd/nova-sprint ./internal/doctor`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/sprintdash -run TestDashboardServesWhatServerPyServedFromOnePoller` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "frictions2" :cards
     ((:id "fp-fric-alex3-report-first-two-lines" :tier "-" :needs ("fp-fric-alex2-gate-names-whose-file") :who "friend.stella"
      :title "A friend's REPORT.md is read by the sprint for its first Verdict: and Head: lines wherever they sit (docs/FRIENDS.md, a sprint card), and friends have lost cards by putting a heading or prose before them"
      :brief "RESULT: fp-fric-alex3-report-first-two-lines sha=2d720d219ff5 tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
WHO: friend stella
DEPENDS-ON: fp-fric-alex2-gate-names-whose-file
PATHS: internal/cardgen/cardgen.go,internal/cardgen/cardgen_test.go,cmd/nova-sprint/friendcards.go,cmd/nova-sprint/friendcards_report_test.go,docs/FRIENDS.md
NEW: cmd/nova-sprint/friendcards_report_test.go
TEST: ./cmd/nova-sprint TestAFriendReportWhoseFirstTwoLinesAreNotPinnedIsReadAllTheSame
Deadline: finish within 60 minutes; the judgment of a card that runs past it is the coordinator's, so report HOLD with what you have before then rather than push past it.
Libraries considered: the Go standard library and testify, already in the tree; the package's own seams and helpers; no new dependency, and no helper over thirty lines without first searching the package for one.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md). Friend sync delivers it as `~/stella-working/inbox/<job>/BRIEF.md`; its STATUS line names the epoch, the attempt and the branch to push, and its next line the working directory. Work only under `~/stella-working/jobs/<job>/`, with GOCACHE=~/stella-working/.cache/go-build (your own, warm after the first card). In this card JOB.md means the delivered BRIEF.md. The sprint reviews and lands the pushed branch; you open no pull request. ATTRIBUTION: commits and reports name the friend (By: Stella) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. A friend's REPORT.md is read by the sprint for its first Verdict: and Head: lines wherever they sit (docs/FRIENDS.md, a sprint card), and friends have lost cards by putting a heading or prose before them. Pin the shape: line 1 is exactly 'Verdict: LAND|HOLD|FAIL', line 2 exactly 'Head: <40-hex>' (omitted only for HOLD and FAIL, where line 2 is blank). Make the friend-card sync (cmd/nova-sprint/friendcards.go) keep reading leniently (a report with the lines later still lands) but add a NOTE on the card when they are not the first two lines, naming the line numbers, and make every generated friend card (internal/cardgen and the friend card template) say 'first line exactly Verdict:, second line exactly Head:'. Update docs/FRIENDS.md. The test feeds the reader a report whose pinned lines are on lines 3 and 4 and asserts the card lands with the NOTE, and a pinned report lands with none. The work lives in the files of the PATHS line, and those are the only files you may touch; a change any other file needs goes in the report as a proposed diff, never a commit. The test named on the TEST line is red before the change and green after.

STEP 1. Set JOB=~/stella-working/jobs/<job> from BRIEF.md, create only that directory, and clone inside it: mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo && git log --oneline -1. Create the exact STATUS branch from origin/rowan/card-generator, the open pull request branch this change builds on (BASE dev is where it lands after that branch merges; branch from its current tip so this change merges cleanly after it). Export GOCACHE=~/stella-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Verify the defect or want still exists at that tip before editing; if it is already fixed, write Verdict: HOLD with the exact evidence rather than duplicate work.
STEP 2. Write the red test first, named as the TEST line says, opening with t.Parallel() and using no sleep or wall-clock bound. Run nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s <the TEST line's package> -run <the test's name> and keep the failing line as evidence.
STEP 3. Make it pass in the files PATHS names, and only those, citing the design section or the model from each function that implements a rule. Commit the draft on your own branch as soon as the test is green, before any further probe.
STEP 4. Run the gate: nice -n 19 env GOMAXPROCS=2 go vet <the package> then nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s <the package> ./internal/ci/ and read the last line of each; gofmt -l on every changed Go file prints nothing. When a test fails, name its file and whether that file is in PATHS (yours) or not (already red at origin/sprint/mechanical-2026-10-02: run the same test on the unchanged tip to say so) and report that line first.
STEP 5. Commit on your own branch with your trailer and push normally with git push -q origin HEAD:refs/heads/<the STATUS line's branch>; never force. Verify git ls-remote origin refs/heads/<that branch> equals git rev-parse HEAD.
STEP 6. END. Write ~/stella-working/outbox/<job>/REPORT.md whose first line is exactly Verdict: LAND and whose second line is exactly Head: <the full 40-hex remote tip> (Verdict: HOLD or FAIL with the precise blocker and no Head line when no truthful LAND is possible), then one paragraph: the change, the test names with what each pins, the gate's last lines, and what was not done. Write RESULT.md beside it, whose line 1 is this card's line 1 (the RESULT line) with sha= the first twelve characters of the Head, and which carries the report's paragraph.

COMMIT BODY. Commits may use the machine's git identity for now, but every commit body carries a line `By: <who did the work>` above the Co-Authored-By trailer: the friend's name for a friend card (By: Rowan, By: Emma, ...), the route's model name for a fleet child (e.g. By: deepseek-v4.1-flash).
DONE-WHEN: TestAFriendReportWhoseFirstTwoLinesAreNotPinnedIsReadAllTheSame passes, the gate of STEP 4 is green, and the branch tip is the Head the report names.")))
    (:stream "friends-sprint-v1-0-0" :cards
     ((:id "fs-friends-table-reads-presence" :tier "-" :needs ("fg-presence-off-the-sprint-server")
      :title "The sprint consumes friend presence; it does not own it"
      :brief "REPO: mas-bandwidth/nova-sprint
BASE: main
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: fg-presence-off-the-sprint-server
PATHS: internal/sprint/presence.go,internal/sprint/*_test.go,internal/sprint/store/friends.go,internal/sprint/store/presence.go,internal/sprint/store/*_test.go,cmd/nova-sprint/friends.go,cmd/nova-sprint/*.go,docs/SPEC-SPRINT.md
TEST: ./internal/sprint TestTheFriendsTableReadsPresenceFromTheBusStore
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages (cmd/nova-sprint, internal/sprint, internal/sprintwire) and testify; no new dependency. Counts toward nova-sprint v1.0.0 (stream friends-sprint-v1-0-0): the consumer side of friend comms; the mechanism is nova-tools' (nova-bus, nova-friend) and this card only reads it. A stranger with their own AIs must be able to do this from nova-tools and its docs alone; no Studio paths, names or our friends in the tool (Glenn, 2026-10-04 1:14 PM ET: 'everything we are doing inside nova-sprint and working with friends, other people can do too with their own AIs, using nova-tools ideally').

ATTRIBUTION OVERRIDE. The injected child rules' Claude trailer and Claude-Code PR footer are inapplicable to this friend card. Commits and reports identify Rowan and the actual model/harness; never claim Claude or Claude Code. This sentence overrides only those two attribution lines; every safety and quality rule remains in force.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/rowan-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/rowan-working/jobs/<job>/`, using `~/rowan-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md. Admission inspected the exact base 15f13249e1474b2c943e88e75025b4bbe515d93b; start at the current tip of origin/main.

THE TASK. The sprint consumes friend presence; it does not own it. Today the friends table's up/down comes from `nova-sprint friend beat` (each daemon over sprintwire) and, since PR 5305, `friend health` written by the coordinator's keepalive; while the sprint server is stopped or hung, every friend reads silent (measured 12:54 PM). nova-tools now holds presence (docs/SPEC-FRIEND.md \"Presence\" in mas-bandwidth/nova-tools, landed by fg-presence-off-the-sprint-server: the hash `bus2:presence:<name>` on the bus store, the state up, asleep or down at 10 s by one rule, `nova-friend peers --json` printing it). Read that section first.
Make the friends table read it: each tick reads every friend row's presence record from the bus store (one pipeline of HGETALL for the friend rows; the bus store's address is the fleet row's bus, as nova-bus resolves it) through a reader interface with a fake in the tests, and applies the same rule (copy the one function with a comment naming its source until the generality pass exports it); a friend down by presence is down in the table, its dealt cards follow the existing down rules; a friend whose presence says broken (the provider refuses every turn) is down with that reason in the table and is dealt nothing, never \"up, working n\"; `friend beat` stays accepted and changes nothing (said in its help, removed in the next release); `friend health`'s state writes are replaced by the read. The sprint's own fields of a friend (width, queue, deal, deadlines) stay the sprint's.
Tests (twin store, fake presence reader, fake clock): TestTheFriendsTableReadsPresenceFromTheBusStore, TestAFriendDownByPresenceIsDownInTheTable, TestAStoppedSprintChangesNoFriendsState. SPEC-SPRINT: the friends table section says where presence comes from. TLA+: the friend state in tla/Friend.tla or DirtyTick is now an input; one line in tla/README.md.
Unit tests use no real time and open no socket: an injected clock (or testing/synctest) and the package's fake store; the logic is a function apart from its transport (a server is a function of its input, a client takes a send function). Every new test opens with t.Parallel(), in testify. A test that needs Redis is functional (-tags functional) and runs only through tools/functionalrun --fresh-gocache --deadline 15m, never against a store on this machine.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-sprint.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/main. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the governing spec section first; verify the defect or gap still exists at the tip before editing, and if it is already done, report HOLD with the exact evidence rather than duplicate it.

STEP 2. Do the task above, the red test first where there is code (the TEST line's name is the one to write; rename it in RESULT if the behaviour wants a better name).
  PATHS: internal/sprint/presence.go,internal/sprint/*_test.go,internal/sprint/store/friends.go,internal/sprint/store/presence.go,internal/sprint/store/*_test.go,cmd/nova-sprint/friends.go,cmd/nova-sprint/*.go,docs/SPEC-SPRINT.md
  COMMIT: fs-friends-table-reads-presence: the friends table reads presence from the bus store
  VERDICT: the friends table's up, asleep and down come from presence, pinned by the three tests

STEP 3. Obtain the shared Go-slot grant, then run `nice -n 19 env GOMAXPROCS=2 go test -p 2 -count=1 -timeout 600s ./internal/sprint/... ./cmd/nova-sprint/` and the package tests of every PATHS package, record the exact last lines, release the slot, and commit only PATHS with honest Rowan and actual model/harness attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD` (the full 40-hex sha, never a short one). Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change, the measurements and the exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: the friends table's up, asleep and down come from presence, pinned by the three tests

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "friends-v1-2-0-reliability" :cards
     ((:id "daemon-supervised" :tier "-" :needs ("limits-mean-down")
      :title "nova-tools v1.2.0, stream friends-v1-2-0-reliability (general, never sprint-specific)"
      :brief "REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: fix-red
CLASS: model
LEG: go
DEADLINE: finish within 150 minutes
DEPENDS-ON: limits-mean-down
PATHS: internal/friend/launchd*.go,internal/friend/daemon*.go,internal/friend/state*.go,cmd/nova-friend/main*.go,cmd/nova-sprint/friends*.go,cmd/nova-sprint/seat*.go,docs/SPEC-FRIEND.md,docs/SPEC-SPRINT.md
SHARED: docs/SPEC-FRIEND.md,docs/SPEC-SPRINT.md
TEST: ./internal/friend TestInstalledAgentKeepsAliveAndStatusSaysVersion
Deadline: finish within 150 minutes.
Libraries considered: the tree's own friend and sprint packages (the launchd plist writer, the twin store) and testify; no new dependency.

ATTRIBUTION. Commits and reports name the friend (By: <name>) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

THE TASK. nova-tools v1.2.0, stream friends-v1-2-0-reliability (general, never sprint-specific). Every friend daemon runs under launchd via `nova-friend install` (internal/friend/launchd.go: RunAtLoad, KeepAlive, a five second throttle, per docs/SPEC-FRIEND.md), but nothing shows which binary each daemon runs or catches one that died or was never reinstalled after an update. Make it visible and alarmed. (1) A test pins the installed plist: KeepAlive true, RunAtLoad true, the throttle, the binary by absolute path, and `install` refusing a plist that lacks them. (2) The daemon writes its version (the tool's Stamp) and the binary's path into status.json (`Status` in internal/friend/state.go). (3) `nova-friend status` prints `daemon_version` and `last_beat_age`, and `nova-friend status --all` lists every friend agent installed for this login (the `com.nova.friend-*` agents) with name, daemon up or down (the status file under DaemonStale), version, and last beat, one line each. (4) The beat carries the version: `nova-sprint friend beat <friend> --daemon-version <v>` (cmd/nova-sprint/friends.go, carried on where --json's friends as --load is, no table change), and the daemon's beat sends it. (5) The seat check, `nova-sprint seat` (cmd/nova-sprint/seat.go), prints each friend's daemon version and last beat age, and one ALARM line per friend whose version differs from the newest version any friend beats (version drift) or whose last beat is older than the beat's down bound (a dead daemon). Cite docs/SPEC-FRIEND.md and docs/SPEC-SPRINT.md. Nothing that ships is a shell script: every loop or helper is a Go verb (Glenn, 2026-10-04 5:03 PM). Edit only the files PATHS names; a change that needs another file is a HOLD naming it. Other cards of this batch also edit docs/SPEC-FRIEND.md and docs/SPEC-SPRINT.md (the SHARED line): add your paragraph as its own subsection headed with this card id.

STEP 1. Set `JOB=~/rowan-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-tools.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/rowan/integration-2026-10-04. Export GOCACHE=~/rowan-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md, docs/SPEC-FRIEND.md and docs/SPEC-SPRINT.md first.

STEP 2. Do the task above, writing the red test first with a temporary home directory, the twin store and an injected clock; no test runs launchctl, opens a socket or sleeps on the wall clock.
  PATHS: internal/friend/launchd*.go,internal/friend/daemon*.go,internal/friend/state*.go,cmd/nova-friend/main*.go,cmd/nova-sprint/friends*.go,cmd/nova-sprint/seat*.go,docs/SPEC-FRIEND.md,docs/SPEC-SPRINT.md
  COMMIT: daemon-supervised: status and the seat check say each friend daemon's version and last beat; drift and dead daemons alarm
  VERDICT: the plist carries KeepAlive and RunAtLoad; status and status --all print the version and the last beat age; the seat check prints one ALARM for a drifted version and one for a dead daemon on the twin store.

STEP 3. Run on a Linux bench (vision or hetzner), never on the Studio: `nice -n 10 env GOMAXPROCS=4 go test -p 2 ./internal/friend -run 'TestInstalledAgentKeepsAliveAndStatusSaysVersion' -count=1 -timeout 600s`, then `nice -n 10 env GOMAXPROCS=4 go test -p 2 -count=1 -timeout 600s ./internal/friend ./cmd/nova-friend ./cmd/nova-sprint ./internal/ci`, record exact last lines, and commit only PATHS with honest attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/rowan-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./internal/friend -run TestInstalledAgentKeepsAliveAndStatusSaysVersion` passes at the head.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))
    (:stream "libs" :cards
     ((:id "libs-goleak-t" :tier "-" :needs ("seams-unit-sockets-net-t" "seams-unit-sockets-sprint-t" "libs-pkg-release-pkg-update-tb")
      :title "A tree of 2 work steps (STEP 2 to STEP 3), walked in order"
      :brief "RESULT: libs-goleak-t sha=db6ab4ed953f tier: pro
REPO: mas-bandwidth/nova-tools
BASE: sprint/mechanical-2026-10-02
KIND: sweep
CLASS: model
DEPENDS-ON: seams-unit-sockets-net-t,seams-unit-sockets-sprint-t,libs-pkg-release-pkg-update-tb
PATHS: go.mod, go.sum, internal/delayproxy/*_test.go, internal/sprintdash/*_test.go
NEW: internal/delayproxy/main_test.go, internal/sprintdash/main_test.go
TEST: internal/delayproxy TestStopEndsEveryGoroutineWhileWritesAreHeld
Deadline: finish within 45 minutes.

You are a child of the coordinator: one task, one checkout, one branch, unattended; this card is the whole task. You start in the job directory (export JOB=$PWD): read $JOB/JOB.md first, work in the staged checkout it names (a full clone at BASE), commit as usual and finish as it says. No gh, no push: JOB.md's finish does both.
Libraries considered: go.uber.org/goleak (adopted by the owner's grant of 2026-09-30, \"go for it on all the modules above\", in the order testify, testscript, goleak, ...): VerifyTestMain fails a package whose tests leave a goroutine running, which the standard library cannot check; testify and the package's own tests stay as they are, and nothing else is added.
The contract is docs/STANDARD.md (embedded in AGENTS.md): read the sections a step names first; where this card and it disagree, it wins and the PR body says so.

THE TASK. A tree of 2 work steps (STEP 2 to STEP 3), walked in order. Each work step carries its own PATHS:, COMMIT: and VERDICT: lines: do it, run its check, commit only its PATHS with its COMMIT: line as the message, and write its verdict line in the PR body and RESULT, `step <n>: <ok|broken|not-done|skipped> <the full 40-hex commit sha pasted from git rev-parse HEAD, or - when nothing changed; a short sha is refused: e.g. step 2: ok 3b1f0c2a9d4e7f6a5c8b1d0e2f3a4b5c6d7e8f90 the change> <one line>` (ok with `-` and the commit that did it when the head already holds the step). The first step that is not ok stops the walk: steps before it land, and the coordinator turns the rest into a new card. Library first (docs/STANDARD.md section 7): a goroutine a test leaves behind is a leak in the code or the test, and goleak is the module that finds it. Pilot it on two packages that start goroutines (internal/delayproxy: its connections; internal/sprintdash: its event stream), one `TestMain` each: `func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }` (whole-package, after every parallel test has ended; never `goleak.VerifyNone(t)` inside a parallel test, which would count its neighbours' goroutines). A leak it finds is fixed in the test (a missing Close, a cancel not called) or, when the code leaks, named in the PR body with file:line and the step ends partial: no `goleak.IgnoreTopFunction` without a sentence naming why that goroutine outlives the tests. The PR body gives the measure for the owner's later call on the rest of the tree: leaks found per package, lines added.

HOW THIS CARD IS JUDGED. (1) Each step's change is the one the step names and no other: every +/- line of `git diff -U0` traces to one sentence of its step. (2) Behaviour no step names is unchanged: the package's existing tests pass unedited, except a fix of a leak the step names. (3) A planted leak (a `go func() { select {} }()` in one test, removed before commit) turns the package red, and the PR body shows the line. (4) Nothing outside PATHS and NEW. (5) Nothing is guessed: where the code cannot tell what is right, the PR body says so and the step ends partial.

STEP 1. cd into the staged checkout JOB.md names (cd <that path>) and run git log --oneline -1: the head is db6ab4ed953f or a later commit of BASE. Export GOCACHE=$JOB/gocache GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Sites and counts here were read at db6ab4ed953f: find a moved line by its text.
STEP 2. (hard) goleak for internal/delayproxy.
  PATHS: go.mod, go.sum, internal/delayproxy/*_test.go
  COMMIT: libs-goleak-t: goleak for internal/delayproxy
  VERDICT: ok when go.mod requires go.uber.org/goleak directly, the package's TestMain verifies, its tests pass three times (`go test -timeout 600s -count=3 ./internal/delayproxy/`) and a planted leak turns it red; not-done or broken naming what remains
Add the module with `GOFLAGS=-mod=mod go get go.uber.org/goleak@v1.3.0` (the module proxy; if the download is refused, end not-done naming the refusal, never vendor a copy), then internal/delayproxy/main_test.go with the one TestMain. TestStopEndsEveryGoroutineWhileWritesAreHeld counts goroutines by hand today: keep it (it pins Stop's promise), and say in the PR body whether goleak now makes its count redundant.
STEP 3. (hard) goleak for internal/sprintdash.
  PATHS: internal/sprintdash/*_test.go
  COMMIT: libs-goleak-t: goleak for internal/sprintdash
  VERDICT: ok when the package's TestMain verifies, its tests pass three times (`go test -timeout 600s -count=3 ./internal/sprintdash/`) and a planted leak turns it red; not-done or broken naming what remains
internal/sprintdash/main_test.go with the one TestMain; a stream a test leaves open is closed by the test.
STEP 4. Read `git diff -U0` from the start commit against HOW THIS CARD IS JUDGED, hunk by hunk; fix in the step that owns the hunk.
STEP 5. Run the gate once, each line as written, keeping each last line: `gofmt -l internal/delayproxy internal/sprintdash   (prints nothing)` ; `nice -n 19 go vet ./internal/delayproxy/ ./internal/sprintdash/` ; `nice -n 19 go vet -tags functional ./internal/delayproxy/ ./internal/sprintdash/` ; `nice -n 19 go test -p 2 -count=1 -timeout 600s ./internal/delayproxy/ ./internal/sprintdash/ ./internal/ci/`. If a test in ./internal/ci/ fails naming a ledger row (\"delete the stale entry\", \"below allowed count\", \"shrink the count\", \"lower the row\"), run `NOVA_CI_UPDATE=1 nice -n 19 go test -p 2 -count=1 -timeout 600s ./internal/ci/ -run '<the failing tests, joined with |>'`, then the gate again. Keep only the ledger lines of files PATHS names and the shards internal/ci/testdata/<ledger>/<package>.txt of its packages; restore any other it rewrote; name a needed one (partial). A row only shrinks.
STEP 6. The pull request: title `libs-goleak-t: goleak for internal/delayproxy and 1 more step`; the body gives the diff stat, what was deleted, each step's verdict line, the gate lines with their last lines, each test added and what it pins, the three measures of docs/STANDARD.md (minimal: lines before and after; performant: a number the gate prints; correct: the test or model holding the behaviour) and what was not done, and ends with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
STEP 7. End as JOB.md says: its `gh pr create` is the end, or RESULT.md in JOB.md's shape (head, branch, verdict, gate, output, report, and the step lines); never a green claim.

AS A READ (only when JOB.md's first line is `# JOB: read <card>, attempt <n>`; otherwise you are the work): change nothing; diff from the start commit JOB.md names. THE GATE PROVES: the packages build, vet and pass with their TestMain, and go.mod requires goleak directly with go.sum holding its sums. Run it once (STEP 5). READ ONLY: each TestMain is the one line; no IgnoreTopFunction without its sentence; no VerifyNone in a parallel test; the PR body shows the planted leak going red.
And: only PATHS, NEW and the gate's ledger lines changed; a deleted test has its deleted-tests.txt row; one commit per work step. Approve (`gh pr review --approve`) only when all hold, else `gh pr review --request-changes --body <file:line and what is wrong>`.

RULES.
Work only in the staged checkout JOB.md names; do not clone.
Commit on the checkout's own branch, as usual; touch no other branch.
Export a private GOCACHE (the path this card names) and GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command.
Never `go clean`, and never clean a shared cache.
NEVER start a redis-server on this machine.
Never kill a process you did not start.
Every `go test` gets `-timeout 600s`.
No `rm -rf` outside the job directory.
Never force-push.
Never rebase.
Do not use git stash (the stash list is shared by every worktree).
Functional tests (any test that needs Redis) run ONLY inside the container through `tools/functionalrun` (`--fresh-gocache --deadline 15m`), never against any other store.
Every new test opens with `t.Parallel()`.
Run `go test -count=1 -timeout 600s ./internal/ci/` before you finish.
No names of people, machines or friends in code, comments or docs.
Docs and comments in the present tense.
Cite the model or the design section from every function that implements a rule.
Touch only the files this card names; a fix that needs another file goes into your report as a proposed diff, not a commit.
Keep the diff minimal: every added line traceable to one sentence of this card.
Commit messages end with `Co-Authored-By: Claude <your model> <noreply@anthropic.com>`. Commits may use the machine's git identity for now, but every commit body carries a line `By: <who did the work>` above the Co-Authored-By trailer: the friend's name for a friend card (By: Rowan, By: Emma, ...), the route's model name for a fleet child (e.g. By: deepseek-v4.1-flash).
PR bodies end with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
Finish as JOB.md says, with `gh pr create` against the base this card names; never merge.
The PR body states the diff stat and what was deleted.
The PR body lists the tests, each with what it pins, and every local helper added.
The report is the PR: its title is the one-line report, its body every test package line and what you could not do and why.
\"Not done\" is a welcome report; a green claim you did not run is not.
A card that builds code carries a filled line `Libraries considered: <what the standard library and the adopted modules offered, and why each was used or not>` (docs/STANDARD.md section 7).
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never start a server on this machine.
Report what was not done.")))
    (:stream "nova-sprint-split" :cards
     ((:id "split-one-binary-dashboardf" :tier "-" :needs ("split-after-new-repo") :who "friend.freddy"
      :title "The sprint dashboard server becomes a nova-sprint verb (nova-sprint dashboard, as the help already lists), the separate binary and its plist retired"
      :brief "RESULT: split-one-binary-dashboardf sha=2d720d219ff5 tier: pro
REPO: mas-bandwidth/nova-sprint
BASE: main
KIND: fix-red
CLASS: model
WHO: friend freddy
LEG: go
DEADLINE: finish within 180 minutes
DEPENDS-ON: split-after-new-repo
PATHS: cmd/nova-sprint/*.go,internal/sprintdash/**,docs/
TEST: ./cmd/nova-sprint TestDashboardIsANovaSprintVerb
Deadline: finish within 180 minutes.
Libraries considered: the tree's own packages and testify; no new dependency. (Glenn, 2026-10-04 12:35-12:51 PM: nova-sprint moves to its own repository mas-bandwidth/nova-sprint and is nova-sprint v1.0.0, after nova-tools v1.2.0 lands; nova-work becomes part of nova-sprint.)

ATTRIBUTION: commits and reports name the friend (By: Freddy) and your actual model and harness. Never claim a model or harness you are not running. A Claude worker writes the true Claude trailer.

You are a friend of the coordinator, and this card is one sprint job (docs/FRIENDS.md), delivered as `~/freddy-working/inbox/<job>/BRIEF.md`: its STATUS line names the epoch, attempt and branch to push. Work only under `~/freddy-working/jobs/<job>/`, using `~/freddy-working/.cache/go-build`. In this card, JOB.md means the delivered BRIEF.md.

THE TASK. The sprint dashboard server becomes a nova-sprint verb (nova-sprint dashboard, as the help already lists), the separate binary and its plist retired.

STEP 1. Set `JOB=~/freddy-working/jobs/<job>` from BRIEF.md, create only that directory, and clone inside it: `mkdir -p \"$JOB\" && cd \"$JOB\" && git clone -q https://github.com/mas-bandwidth/nova-sprint.git repo && cd repo`. Create the exact STATUS branch from the current remote tip of origin/main. Export GOCACHE=~/freddy-working/.cache/go-build GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command. Read AGENTS.md and the split plan /Volumes/nova/ai/rowan/working/rowan-new/tmp/nova-sprint-split/PLAN.md first.

STEP 2. Do the task above, writing the red test first where there is code.
  PATHS: cmd/nova-sprint/*.go,internal/sprintdash/**,docs/
  COMMIT: split-one-binary-dashboardf: The sprint dashboard server becomes a nova-sprint verb (nova-sprint dashboard, a
  VERDICT: the task's behaviour is pinned by a test, or for a record the report is on the bus.

STEP 3. Obtain the shared Go-slot grant, then run `nice -n 19 env GOMAXPROCS=2 go test -p 2 ./cmd/nova-sprint -run 'TestDashboardIsANovaSprintVerb' -count=1 -timeout 600s`, then the package tests of every PATHS package, record exact last lines, release the slot, and commit only PATHS with honest Freddy and actual model/harness attribution; `gofmt -l` on the changed files prints nothing.

STEP 4. END. Push normally with `git push -q origin HEAD:refs/heads/<the STATUS line's branch>`; never force. Verify `git ls-remote origin refs/heads/<that branch>` equals `git rev-parse HEAD`. Write `~/freddy-working/outbox/<job>/REPORT.md` beginning `Verdict: LAND` and `Head: <full 40-hex remote tip>`, followed by one paragraph stating the change and exact gates. Use HOLD or FAIL with the precise blocker when no truthful LAND is possible. Write RESULT.md beside it carrying this card's RESULT line. Create no pull request; the sprint consumes the branch and report.

DONE-WHEN: `go test -count=1 -timeout 600s ./cmd/nova-sprint -run TestDashboardIsANovaSprintVerb` passes at the head, or the report lands on the bus.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.")))))))

