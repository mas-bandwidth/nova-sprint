# Tests: nova-sprint, and the tools folding into it

Moved from nova-tools docs/TESTS.md on 2026-10-04.

## nova-sprint

This is a test-binary transcript, not an installed first run. The local twin's
operation-id counter makes this transcript's IDs repeatable; the production
CLI walkthrough is in the
[first-lap note](FIRST-LAP.md).
In the test binary, `--redis mem:<file>` loads an in-memory twin from a file
and saves it after each command. Commands run one at a time. This transcript
follows the card flow in `nova-sprint help`, with
`NOVA_SPRINT_REDIS=mem:sprint.twin` and `NOVA_SPRINT_ACTOR=boss` set. It uses
`finish` without `--head` and `merge` to record a landing without git. The
help's final `tick` moves the card to landed, and `where` shows the sprint.
Those two commands are omitted here because they print clock-dependent times;
`cmd/nova-sprint/twin_test.go` runs them.

A card's move is queued until the next tick prints `MOVED drain`. A member
that comes up in one tick receives cards in the next.
`cmd/nova-sprint/firstrun_test.go` runs this transcript in the unit tier over
a twin file in a temporary directory. The twin counts its operation ids
(`t1`, `t2`), so every value reproduces without normalization. The functional
tests beside it (`cmd/nova-sprint/*_functional_test.go`) run against a real
store.

The worker's verb answers carry a typed outcome (`outcome: accepted|refused|unknown` and
`changed: yes|no|unknown`) on the server's results and each verb's `--json` output, so a worker
tells a definite refusal from an unknown outcome without reading the printed words. The table
`TestResultOutcomeTable`, `TestWorkerVerbOutcomeServed`, `TestWorkerVerbOutcomeFriendBeatJSON` and
`TestResultOutcomeRefusedPaths`
(`cmd/nova-sprint/worker_verb_outcome_test.go`) cover every path #5625 classifies from text
(refuse, the step's `CutError`, `ErrUnknown`, a `ClearedError` with `Finished`, a store error that
answered nothing), check the field each sets over the server's own step, and run the server's
refusal paths (a verb it does not run, a report naming no epoch, a usage refusal) and the three
budget verbs the server runs, whose success is `accepted`/`yes`; `TestWorkerVerbOutcomeFriendBeatJSON`
pins the read's finding that a worker verb answering without the report path (friend beat) carries
the field on its own `--json` output; the functional
`TestWorkerVerbOutcomeOverRealServer`
(`cmd/nova-sprint/worker_verb_outcome_functional_test.go`) checks the outcome over a real server
and a private store, with an explicit `--redis` on every verb and a scrubbed environment. The
store's `unknown` outcome is the store's word that a write was not confirmed or was cut, which a
healthy private store does not give; the functional test checks `accepted` and `refused` over the
real server and the server's own `unknown` (a caller gone before its verb ran, the goneResult
path) over the real store and the server's entry point, and the store's typed `unknown` paths are
covered by the table test over the typed paths.

`TestAddAndTheTickKeepABenchMirrorForEveryNamedRepo` (`internal/sprint/mirror_need_test.go`)
pins the mirror need at both moments the sprint knows a repository is needed. It admits a card
whose `REPO:` names a repository no member carries and checks the plan prints the `MIRROR <repo> needed on <members>` line and records the repository on the stream's control card; then it
records a staging take whose error is `no bench mirror for <url>` for a second repository from
one member and checks the tick keeps one need for that repository, names that member, raises
exactly one judgment, and never redeals a card of that repository to the member that lacks the
mirror while another member is free. `TestMirrorNeedFromAStagingRefusal`,
`TestMirrorNeedEndsAfterAnOk` and `TestMirrorNeedEndsPastTheWindow` (same file) pin the need's
derivation, its end after an ok finish and its lapse past `StagingMirrorWindow`. No test runs
`ssh` or `git push` from inside nova-sprint: the need is a line and a judgment, and the fleet's
own mirror refresh serves it.

The functional tier (`go test -tags functional -count=1 ./...`, `make functional`) runs for every pull request and push to main on a GitHub-hosted
ubuntu-latest runner — `make functional-gate` is the CI form, with `NOVA_CI=1`,
redis-server installed, and each package bounded at two minutes (`-timeout 120s`); over the bound is the test's defect. The job runs the tier as a
matrix: a rest leg (`functional-gate-rest`, every package but `./cmd/nova-sprint`) and three shard legs (`functional-gate-shard SHARD=k SHARDS=3`).
`./cmd/nova-sprint`'s functional package is CPU-bound at a runner's four cores (alone ~62s at default parallelism, 1275 top-level tests, and stretched
past the two-minute bound beside the tier on CI's slower runner), so sharded across three legs each leg finishes well inside the bound and every
top-level test runs exactly once.

### First run

This is a deterministic twin transcript from the test binary. The production
CLI's local-twin walkthrough, run on Linux from v1.2.20 source, is in the
[first-lap note](FIRST-LAP.md).

```text
$ nova-sprint init --readers reader-m2 --members m1,m2
INIT OK tables=work,readers,merge,fleet view=sprint readers=reader-m2
READY (not started; run: nova-sprint start)
MOVED m1 added, down until it beats
FLEET-UP OK moved=1 refused=0 notes=0 op=fleet-release-t1-1
READY
MOVED m2 added, down until it beats
FLEET-UP OK moved=1 refused=0 notes=0 op=fleet-release-t2-1
READY
NOTE a twin beats every member at every verb: each member added is up after the next nova-sprint tick
$ nova-sprint add --stream s1 --count 1 --one --brief-file testdata/brief.txt
MOVED s1-1 -> ready stream=s1 score=1 tier=flash
ADD OK stream=s1 cards=1 before=- moved=1 refused=0 notes=0 op=add-t3-1
READY  0/1 0.0%
$ nova-sprint start
START OK before=READY after=RUNNING changed
nothing is ticking between commands in a twin: tick by hand: nova-sprint tick
0/1 0.0% -> ETA -  machine: running
$ nova-sprint tick
MOVED presence: m1 up
MOVED presence: m2 up
MOVED presence: status seen: m1 up, m2 up
TABLES rows changed: work=0 readers=0 merge=0 fleet=2
TICK OK state=RUNNING idle=no moved=3 notes=3
0/1 0.0% -> ETA -  machine: running
$ nova-sprint tick
MOVED deal: s1-1 work ready -> working card=s1-1.w1 member=m1 (fleet ready)
TABLES rows changed: work=1 readers=0 merge=0 fleet=1
TICK OK state=RUNNING idle=no moved=1 notes=1
0/1 0.0% -> ETA -  machine: running
$ nova-sprint take --as m1 --epoch 0
MOVED s1-1.w1 fleet ready -> working member=m1 gen=1
PACKET s1-1.w1 attempt=1 gen=1 epoch=0
  tier: flash
  branch: sprint/s1-1.w1.g1.e0
  base: the stream's base
  brief:
    RESULT: twin-help-brief sha=0011223344ab tier: flash
    You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
    
    RULES.
    Work only in the job directory this card names.
    Never force-push or rebase a shared branch.
    Never kill a process you did not start.
    Never start a server on this machine.
    No `rm -rf` outside the job directory.
    Report what was not done.
    
    THE TASK. The no-Redis walkthrough in `nova-sprint help` deals its card with no brief, so a worker following it is handed no task with the card: give the card its brief before the first tick, in cmd/nova-sprint/twin.go and docs/TESTS.md, so the walkthrough teaches the right order.
    Libraries considered: none; this card changes only help text and docs.
  notes: none
  report it: nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --branch sprint/s1-1.w1.g1.e0 --head <commit> --report '<what you did>' [--failed]
TAKE OK moved=1 refused=0 notes=0 op=take-t32-1
0/1 0.0% -> ETA -  machine: running
$ nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --report done
MOVED s1-1.w1 working -> done ok; s1-1 working -> review
FINISH OK moved=1 refused=0 notes=1 op=finish-t33-1
0/1 0.0% -> ETA -  machine: running
$ nova-sprint tick
MOVED drain: s1-1.w1 working -> done ok; s1-1 working -> review (finish by m1)
MOVED deal: s1-1 asked of m2 (read cards)
MOVED deadlines: landing-stall watch begins: 1 review, 0 merging, no landing yet
TABLES rows changed: work=1 readers=0 merge=0 fleet=1
TICK OK state=RUNNING idle=no moved=3 notes=0
0/1 0.0% -> ETA -  machine: running
$ nova-sprint take --as m2 --epoch 0
MOVED s1-1.r1.m2 fleet ready -> working member=m2 gen=1
PACKET s1-1.r1.m2 attempt=1 gen=1 epoch=0
  tier: flash
  work: attempt 1 by m1
  head: s1-1.w1
  branch: sprint/s1-1.w1.g1.e0
  brief:
    RESULT: twin-help-brief sha=0011223344ab tier: flash
    You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
    
    RULES.
    Work only in the job directory this card names.
    Never force-push or rebase a shared branch.
    Never kill a process you did not start.
    Never start a server on this machine.
    No `rm -rf` outside the job directory.
    Report what was not done.
    
    THE TASK. The no-Redis walkthrough in `nova-sprint help` deals its card with no brief, so a worker following it is handed no task with the card: give the card its brief before the first tick, in cmd/nova-sprint/twin.go and docs/TESTS.md, so the walkthrough teaches the right order.
    Libraries considered: none; this card changes only help text and docs.
  report:
    done
  notes: none
  attribution: the By: line, the Co-Authored-By trailer and the model or harness named are never a finding and never decide a verdict
  report it: nova-sprint read --as m2 (--ok | --broken) s1-1.r1.m2 --epoch 0 --finding '<file:line, and what to change>'
TAKE OK moved=1 refused=0 notes=0 op=take-t37-1
0/1 0.0% -> ETA -  machine: running
$ nova-sprint read --as m2 --ok s1-1.r1.m2@1 --epoch 0
MOVED s1-1.r1.m2 working -> ok (retired: read by m2)
READ OK moved=1 refused=0 notes=0 op=read-t38-1
0/1 0.0% -> ETA -  machine: running
$ nova-sprint tick
MOVED drain: s1-1.r1.m2 working -> ok (retired: read by m2) (read by m2)
MOVED accept: s1-1 review -> merging queued (ok from s1-1.r1.m2)
MOVED verdicts: s1-1.w1 finished -> ok (the readers' verdict)
TABLES rows changed: work=1 readers=0 merge=1 fleet=1
TICK OK state=RUNNING idle=no moved=3 notes=2
0/1 0.0% -> ETA -  machine: running
$ nova-sprint merge --stream s1 --batch 1
MOVED s1-1 merging -> landed
MERGE OK moved=1 refused=0 notes=3 op=merge-t43-1
0/1 0.0% -> ETA -  machine: running
```

The bare `merge` is the twin's. A real store refuses it (it would record a landing with no push); there, `land` pushes the batch and records the merge.


### Answered by nova-decide

The routine judgments answered by the judgment decision
([SPEC-SPRINT.md section 8](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/SPEC-SPRINT.md#answered-by-nova-decide)): two cards
come back failed in one note, and `answer` asks the decision for each
card. With no `decide_judgment_bar` set (the sprint row ships it empty) and no
`--bar`, it applies nothing: it records each decision and lists what a bar would
apply. Given `--bar 0.8` it applies the recorded decisions, asking nothing again,
and reworks each card by the line the inbox prints for it alone, each line carrying
the decision's op id (`--op decide.<decision id>`), recorded as `applying` before it
runs and `applied` after, so a pass stopped between the two is finished by the next
through the same op and nothing is applied twice. The backend is
the fixed one (`--backend fixed`), answering from
`cmd/nova-sprint/testdata/judgment-answers.json` whatever the state, so no key or
network is needed; with Jev it is `nova-secrets exec --only JEV_API_KEY --
nova-sprint answer`. Run from a checkout root over a fresh twin, with
the first run's environment, by `cmd/nova-sprint/answer_transcript_test.go`,
which keeps the record in a temporary directory and prints it as
`./judgment.jsonl`; nothing else is normalised.

```text
$ nova-sprint init --readers reader-a,reader-b --members m1
INIT OK tables=work,readers,merge,fleet view=sprint readers=reader-a,reader-b
READY (not started; run: nova-sprint start)
MOVED m1 added, down until it beats
FLEET-UP OK moved=1 refused=0 notes=0 op=fleet-release-t1-1
READY
NOTE a twin beats every member at every verb: each member added is up after the next nova-sprint tick

$ nova-sprint add --stream s1 --count 2
MOVED s1-1 -> ready stream=s1 score=1
MOVED s1-2 -> ready stream=s1 score=2
ADD OK stream=s1 cards=2 before=- moved=2 refused=0 notes=0 op=add-t2-1
NOTE the cards have no brief, so a worker is handed no task with them; give each one before it is dealt, on a STOPPED machine: nova-sprint brief <id> --brief-file <path>
READY  0/2 0.0%

$ nova-sprint start
START OK before=READY after=RUNNING changed
nothing is ticking between commands in a twin: tick by hand: nova-sprint tick
0/2 0.0% -> ETA -  machine: running

$ nova-sprint tick
MOVED presence: m1 up
MOVED presence: status seen: m1 up
TABLES rows changed: work=0 readers=0 merge=0 fleet=1
TICK OK state=RUNNING idle=no moved=2 notes=2
0/2 0.0% -> ETA -  machine: running

$ nova-sprint tick
MOVED deal: s1-1 work ready -> working card=s1-1.w1 member=m1 (fleet ready)
MOVED deal: s1-2 work ready -> working card=s1-2.w1 member=m1 (fleet ready)
TABLES rows changed: work=1 readers=0 merge=0 fleet=1
TICK OK state=RUNNING idle=no moved=2 notes=1
0/2 0.0% -> ETA -  machine: running

$ nova-sprint take --as m1 --max 2 --epoch 0
MOVED s1-1.w1 fleet ready -> working member=m1 gen=1
MOVED s1-2.w1 fleet ready -> working member=m1 gen=1
PACKET s1-1.w1 attempt=1 gen=1 epoch=0
  tier: flash
  branch: sprint/s1-1.w1.g1.e0
  base: the stream's base
  notes: none
  report it: nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --branch sprint/s1-1.w1.g1.e0 --head <commit> --report '<what you did>' [--failed]
PACKET s1-2.w1 attempt=1 gen=1 epoch=0
  tier: flash
  branch: sprint/s1-2.w1.g1.e0
  base: the stream's base
  notes: none
  report it: nova-sprint finish --as m1 s1-2.w1@1 --epoch 0 --branch sprint/s1-2.w1.g1.e0 --head <commit> --report '<what you did>' [--failed]
TAKE OK moved=2 refused=0 notes=0 op=take-t31-1
0/2 0.0% -> ETA -  machine: running

$ nova-sprint finish --as m1 s1-1.w1@1 s1-2.w1@1 --epoch 0 --failed --report 'the tests went red'
MOVED s1-1.w1 working -> done failed; s1-1 working -> review
MOVED s1-2.w1 working -> done failed; s1-2 working -> review
FINISH OK moved=2 refused=0 notes=1 op=finish-t32-1
0/2 0.0% -> ETA -  machine: running

$ nova-sprint tick
MOVED drain: s1-1.w1 working -> done failed; s1-1 working -> review (finish by m1)
MOVED drain: s1-2.w1 working -> done failed; s1-2 working -> review (finish by m1)
MOVED deadlines: landing-stall watch begins: 2 review, 0 merging, no landing yet
TABLES rows changed: work=1 readers=0 merge=0 fleet=0
TICK OK state=RUNNING idle=no moved=3 notes=0
0/2 0.0% -> ETA -  machine: running

$ nova-sprint answer --backend fixed --answers ./cmd/nova-sprint/testdata/judgment-answers.json --record ./judgment.jsonl
judgment        card  kind    verb    p     act     why
finish-t32-1.1  s1-1  failed  rework  0.91  listed  no decide_judgment_bar is set, so nothing is applied; at a bar at or under 0.91 it would apply: nova-sprint rework s1-1 --one
finish-t32-1.1  s1-2  failed  rework  0.91  listed  no decide_judgment_bar is set, so nothing is applied; at a bar at or under 0.91 it would apply: nova-sprint rework s1-2 --one
ANSWER OK rows=2 applied=0 would_apply=0 listed=2 refused=0 failed=0 left=0 outcomes=0 bar=- record=./judgment.jsonl; run: nova-sprint inbox

$ nova-sprint answer --bar 0.8 --backend fixed --answers ./cmd/nova-sprint/testdata/judgment-answers.json --record ./judgment.jsonl
judgment        card  kind    verb    p     act      why
finish-t32-1.1  s1-1  failed  rework  0.91  applied  nova-sprint rework s1-1 --one --op decide.finish-t32-1.1_s1-1
finish-t32-1.1  s1-2  failed  rework  0.91  applied  nova-sprint rework s1-2 --one --op decide.finish-t32-1.1_s1-2
ANSWER OK rows=2 applied=2 would_apply=0 listed=0 refused=0 failed=0 left=0 outcomes=0 bar=0.80 record=./judgment.jsonl; run: nova-sprint inbox
```

### No ghost reads (v1.2.14)

A read working on a member is named by its member's beat, or within the grace
it goes back to the read deal (docs/SPEC-SPRINT.md, working means a live run).
The model, tla/ReadCards.tla NoGhostRead (liveness under LiveSpec), proves only
that such a read eventually leaves working; the 60 s bound is pinned by these
tests: `TestAReadTheMemberDoesNotRunIsReturnedAtTheNextBeat`,
`TestAMemberRestartReturnsItsReadsAtOnce` and
`TestAMemberProcessKilledOrRestartedLeavesNoGhostRead`. These tests lock it in;
the functional ones are release acceptance (`make release-accept`).

- Unit, `internal/sprint/read_lost_test.go`:
  `TestAReadTheMemberDoesNotRunIsReturnedAtTheNextBeat`,
  `TestAMemberRestartReturnsItsReadsAtOnce`,
  `TestAWorkingReadTheBeatNamesIsNeverReturned`.
- Unit, `pkg/member/read_retry_test.go`:
  `TestAReadWhoseStageFailedIsRunAgainNotHeldAsAGhost`.
- Functional (`-tags functional`), `cmd/nova-sprint/read_lost_functional_test.go`:
  `TestAMemberProcessKilledOrRestartedLeavesNoGhostRead`,
  `TestAMemberKilledSilentGivesItsReadsBackByTheDownPath`. Both run
  `pkg/member` members in the test process over the command's verbs, on the
  sprint's clock; neither starts an OS process.

### A card in review always has a read the ask can reach (v1.2.14)

A read that ends without a verdict, or is withdrawn by a hold or a return, is
re-asked of another reader at the next tick for the card's current attempt. The
ask verb and the tick share one definition of "asked at this attempt"
(`askedAtAttempt`, `internal/sprint/readers.go`), so the ask verb's two refusals
and the no-stall rule's decisions never contradict each other; a card in review
with no read out and no open judgment is a ConsumersOut violation at `TickCheck`.

- Unit, `internal/sprint/a_card_in_review_always_has_a_read_the_ask_can_reach_test.go`:
  `TestACardInReviewAlwaysHasAReadTheAskCanReach`.
- Functional (`-tags functional`), on a private store (explicit `--redis`, a
  scrubbed env whose HOME and XDG_CONFIG_HOME are the test's own, no `NOVA_*`
  variable from the machine),
  `cmd/nova-sprint/a_card_in_review_always_has_a_read_the_ask_can_reach_functional_test.go`:
  `TestACardInReviewAlwaysHasAReadTheAskCanReachOnAStore`. It runs the verbs to
  put the card in review at attempt 2, withdraws the attempt-1 read by a hold
  and ends the attempt-2 read with no verdict, and asserts `ask` and
  `ask --another` agree (neither refusal is given) and the next tick's ask
  re-asks the attempt of another reader. The consumers-out gap it leaves is the
  tick's postcondition the unit test asserts directly (ConsumersOut).

### A brief edit keeps the needs, tier and bound (v1.2.14)

A `brief` edit replaces the card's whole text, but what a verb set stays the
card's: the needs a relink re-pointed, the tier a re-tier pinned, and the bound
count a routing-only edit must not reset (docs/SPEC-SPRINT.md, the needs, the
brief verb and the bound).

- Unit, `internal/sprint/brief_edit_test.go`:
  `TestBriefKeepsANeedRelinkedToItsTwin`,
  `TestBriefAddsANewNeedAndKeepsADroppedOne`,
  `TestABriefDroppingANeedKeepsItUnlessNeedsFromBrief`,
  `TestABriefChangingOnlyDependsKeepsARelinkedNeed`,
  `TestADependsOnlyBriefReportsTheBoundAndKeepsTheTier`,
  `TestADependsOnlyBriefTierFlagOverridesTheText`,
  `TestBriefKeepsARecordedTierAgainstTheText`,
  `TestBriefTierFlagOverridesTheText`,
  `TestAMetadataOnlyBriefKeepsTheBoundCount`,
  `TestBriefTierRefusesATLAPathBelowFrontier`,
  `TestANewTaskBriefResetsTheBoundCount`,
  `TestABriefBatchHoldsTheKeptNeedsToTheCycleCheck`,
  `TestABriefLeavesOutADroppedNeedAnEarlierRelinkReplaced`,
  `TestABriefTierFlagRefusesATLAPathBelowFrontier`.
- Unit, `cmd/nova-sprint/tier_verb_test.go`:
  `TestTierRefusesAReTierWithoutAReason` (`tier --set` with no, an empty or a
  blank `--reason` is refused and writes nothing).
- Unit, `cmd/nova-sprint/brief_verb_test.go`:
  `TestBriefTierRequiresAReasonWithoutABriefEdit` (`brief --tier` without a brief
  edit requires a non-blank `--reason`, exit 2, nothing written);
  `TestBriefTierWithBriefFileRequiresAReason` (`brief --tier --brief-file` without
  `--reason` is refused, exit 2, nothing written).
- Functional (`-tags functional`), `cmd/nova-sprint/brief_edit_functional_test.go`:
  `TestABriefEditKeepsARelinkedNeedOnTheStore` (a relink, then `brief
  --brief-file` with the stale DEPENDS-ON on the test's own redis, `--redis` on
  every verb, HOME and XDG_CONFIG_HOME the test's own: the need kept
  and its NOTE, a new need added beside it, and `bound: same task` on a card
  dealt); `TestBriefTierWithBriefFileRequiresAReason` (a card, then
  `brief --tier --brief-file` without `--reason` refused with nothing written,
  with `--reason` accepted; reason visible on the card).

- Unit, `cmd/nova-sprint/continue_test.go` (the in-memory harness): `TestContinueAnswersABoundFromTheCarriedHead`
  (v1.2.17, the continue verb) pins the continue step: admits a card, records two attempts with pushed heads
  and a bound judgment, runs `continue <card> --note x`, and asserts the brief has
  exactly one `CARRY:` line at the second head, the note is last, the judgment is
  answered, the bound is reset, and the card is back in ready; `TestContinueCarriesAnExplicitSha`
  pins `--carry <sha>` on a card with no pushed head; `TestContinueRefusesWithoutACarriedHead`
  pins the refusal, exit 1, nothing written, when no attempt pushed a head and no
  `--carry` is given.

### An unserved tier falls to the strongest served tier

The setting `set --unserved-tier-falls-to` gives every card whose brief tier no route and
no up friend serves the strongest served tier at or below the setting, written as a field
of its own, so the deal can run it where something can take it; when the brief tier is
served again the fallback is cleared on every card not yet taken, and a card the seat
pinned is never touched (docs/SPEC-SPRINT.md section 5, "An unserved tier falls to the
strongest served tier").

- Unit, `internal/sprint/unserved_tier_test.go`:
  `TestAnUnservedTierFallsToTheStrongestServedTier` (frontier cards with no frontier route
  and no friend fall to heavy and are dealt on the heavy route with the note naming the
  unserved tier; a seat-pinned card is untouched; when a frontier friend comes up the next
  tick clears the fallback on the untaken cards and they go back to frontier).
- Unit, `cmd/nova-sprint/card_tier_fallback_test.go`:
  `TestCardAndWhereShowAFallbackTier` (`card` and `where --json --rows` show
  `tier=<brief> (falls to <t>)` for a card with a fallback tier).

### One definition of what a PATHS entry covers (v1.2.13)

A directory `PATHS` entry (trailing `/`) covers every file under it, a glob
covers its matches at the head, and a literal names one file that may be new;
the lander's E12 check, the add and brief checks and the card lint read that one
`diffcheck.Covers` (docs/SPEC-SPRINT.md section 7, the lander's checks; section
11, the brief checks). A head refused by E12 names, for each file, the entry it
was expected to match.

- Functional, `cmd/nova-sprint/pathcover_test.go`:
  `TestLanderAcceptsNewFileUnderDirectoryPaths`: a card whose brief names
  `pkg/thing/` and whose head adds `pkg/thing/new.go` is admitted and lands,
  where before the directory entry covered nothing and the head was refused E12.


### Every streaming verb flushes every line

A seat's monitor sees a streaming verb's lines the moment they are written:
every streaming verb writes each line to stdout as one `write(2)` (there is no
user-space buffer to flush), so the line is readable on a real OS pipe within
one second while the verb still runs. A regression that buffered stdout without
flushing it would hide the line until the verb exited and fail the guard
(docs/SPEC-SPRINT.md, "The hook"). The streaming verbs are `hook` (streams
HOOK CHALLENGE and its messages until `unhook`), `where --watch` (a frame every
`--every` until interrupted) and `watch --wake` (blocks, then prints its one
WAKE line). `log` has no follow mode and prints every line it has and exits, so
there is no `log -f` stream to guard.

- Unit, `cmd/nova-sprint/hook_test.go`:
  `TestHookFlushesAfterChallenge` runs `hook` against a private server with its
  stdout a real OS pipe (`os.Pipe`) and asserts the HOOK CHALLENGE line is
  readable within 1s while the hook still runs; then ends it with `unhook`.
- Functional (`-tags functional`), `cmd/nova-sprint/hook_flush_functional_test.go`:
  `TestHookFlushesAfterChallengeOnTheStore` does the same on a private
  redis-server, the store named explicitly with `--redis` on every verb, in a
  scrubbed environment.
- Unit, `cmd/nova-sprint/hook_compact_test.go`:
  `TestHookCompactPrintsOnlyWhatTheSeatActsOn` runs `hook --compact --overdue 2s`
  against a private server and store with its stdout a real OS pipe, proves it,
  raises one card-holding judgment and one HAPPENED note addressed to the seat and
  checks that the JUDGMENT and its first detail line are read as `PUSH <id> JUDGMENT ...` with no acknowledge menu and that the HAPPENED push is not
  printed; it asserts that one `OVERDUE <alias> <age> <kind> :: <cards>` line is
  read past `--overdue` and a second after another interval, and that none is
  read once the judgment is answered. `TestCompactSayPrintsACriticalJudgment`
  pins that a judgment whose cards wait on it (its group line prefixed
  `CRITICAL <n> behind:`) is printed, not dropped. (docs/SPEC-SPRINT.md,
  "The hook"; docs/CLI.md, `nova-sprint hook --compact`.)
- Unit, `cmd/nova-sprint/flush_stream_test.go`:
  `TestWhereWatchFlushesEveryFrame` runs `where --watch --every 20ms` against the
  test store with its stdout a real OS pipe and asserts a frame is readable
  within 1s while the watch still runs; then interrupts it and waits for it to
  end; `TestWatchWakeFlushesTheWake` runs `watch --wake` with its stdout a real
  OS pipe and asserts its WAKE line is readable within 1s.

### The ready to merge notice and its hook push

The "ready to merge" notice names only the cards newly accepted and the group's
count, one line; it never accumulates the primaries of every accept the group has
seen, and an informational HAPPENED push is not re-pushed on reconnect once
acknowledged. `hook --quiet-happened` makes the seat receive judgments and
challenges only, not the informational HAPPENED pushes.

- Unit, `cmd/nova-sprint/ready_merge_push_test.go`:
  `TestReadyToMergePushNamesOnlyTheNewCard` writes two "ready to merge" happened
  notes on one stream, proves a hook, and asserts the push names the newly
  queued card and the group's count only -- never the earlier accepted card --
  and that after acknowledging it, a reconnect re-pushes no "ready to merge"
  group. `TestHookQuietHappenedOmitsInformationalPushes` sets the hook's
  quiet-happened mode and asserts the informational happened push is not
  delivered.

### A need added to a working card is not a broken invariant (v1.2.13)

A card that gains a need while dealt or working is a legal state until that
attempt ends: the card was dealt before the need was recorded, so rule 11 holds
only for a card dealt after the need was recorded. The `brief` verb and rule 11
agree, the state is named on the card (`working, waits for <need> after this attempt`), and the finish moves the card `working -> waiting` when its attempt
ends (docs/SPEC-SPRINT.md section 9, rule 11).

- Functional (`-tags functional`),
  `cmd/nova-sprint/need_on_a_working_card_functional_test.go`:
  `TestANeedAddedToAWorkingCardIsNotABrokenInvariant` (a card dealt to `m1` and
  taken, then `brief --brief-file` adds `DEPENDS-ON: dep` to the working card, on
  the test's own redis with `--redis` on every verb, HOME and XDG_CONFIG_HOME the
  test's own: `check` raises no rule-11 violation, the card names the state,
  the finish moves it to waiting for `dep`, and `check` stays OK).
- Unit, `internal/sprint/need_on_a_working_card_test.go`:
  `TestARule11LeavesAWorkingCardsGainedNeedOut` (a taken card given a need not
  landed is a rule-11 violation without `FieldWaitsAfter`, and none with it;
  the finish moves the card `working -> waiting`).

### The landing stall (v1.2.14)

Cards that wait in review or merging with no landing for the bound (one
minute, `set --landing-stall-bound`) raise one "landing stalled" judgment to
the coordinator with its breakdown, re-raised each bound and cleared by the
next landing with "landing again after <d>"; every landing is pushed to the
coordinator as "landed <card>" (docs/SPEC-SPRINT.md, the landing stall).

- Unit, `internal/sprint/landing_stall_test.go`:
  `TestLandingStallIsJudgedWhenCardsWaitAndNothingLands`,
  `TestLandingStallNamesTheRedCI`, `TestLandingStallIsReRaisedEachBound`,
  `TestLandingStallClearsOnTheNextLanding`, `TestNoStallWithNothingWaiting`,
  `TestLandingStallClosesItsJudgmentWhenWaitingClears`,
  `TestLandingReadCountsCombineFleetAndLegacyConsumersOnce`,
  `TestLandingReadCountsAnActiveUnmarkedFleetRead`,
  `TestLandingPushCountsCompletedFleetReadsAndBegunLegacyReads`,
  `TestLandingReadHistoryDeduplicatesOnlyTheSameAttemptAndHead`,
  `TestAStallOfCardsOnTheCoordinatorIsNamedTheSeatsStall`,
  `TestEveryLandingIsPushedToTheCoordinator`,
  `TestMergeWritesTheLandingStallFacts`, `TestMergeWritesTheLandersLastRefusal`,
  `TestTickDeadlinesRaisesTheLandingStall`,
  `TestLandingStallStartsBeforeTheFirstLanding`,
  `TestLandingStallSurvivesTicksShorterThanTheBound`,
  `TestLandingStallBoundIsSettable`,
  `TestALandingSeenLateEndsTheOldStallBeforeANewOne`,
  `TestALandingPushOfLongLedgerTextsStaysInsideTheFieldBound`,
  `TestALandingStallStampAloneIsAPlanTheTickWrites`,
  `TestALandingPushOfAHundredCardsKeepsEveryCardsLine`,
  `TestLandingLinesNameEveryCardWhenTheFactsAloneOverflow`,
  `TestASubSecondBoundNeverTakesTheLastLandingForANewOne`,
  `TestALandingOverflowOfIDsPastTheBoundCountsAllAndCutsNoID`.
- Unit, `internal/sprint/store/landing_stall_reads_test.go`:
  `TestLandingPushCountsFinishedFriendReadsAfterAccept`,
  `TestLandingPushCountsActiveFriendReadsWithReadCardsOff`.
- Unit, `cmd/nova-sprint/land_test.go`:
  `TestLandingPushReadsTheCardLedgerEntryAtTheLandedHead`.
- Functional (`-tags functional`), `cmd/nova-sprint/landing_stall_functional_test.go`:
  `TestALandingStallIsJudgedAndClearedByALandingOnTheStore`, on a private
  redis-server with the machine's own clock and a one-second bound: the stall
  is raised to the coordinator, and the landing pushes "landed" and closes it.
  Every verb names the private store with `--redis`; the app sees only the
  test's own environment (its home and config directory are temporary, no
  `NOVA_SPRINT_SERVER`), and its forward fails the test, so no verb can reach
  a live sprint server.

### The start level never moves a card off a free lane (v1.2.14)

The level at the start of every tick moves a ready card only off a member
holding more work cards than its width, and only onto the next member from the
deal's index with a free lane (docs/SPEC-SPRINT.md, the rebalance;
tla/Level.tla NeverOffAFreeLane (lands with #127)). A member at or below its width keeps every
card dealt to it.

- Unit, `internal/sprint/level_free_lanes_test.go`:
  `TestTheLevelLeavesADealtCardOnAnIdleMember`,
  `TestTheLevelMovesTheExcessOntoFreeLanesOnly`,
  `TestTheLevelTargetsRollTheIndex`,
  `TestTheLevelSkipsASourceWithNoMovableCard` (the most-over source's cards
  are all refused, a less-over source's card moves onto the free lane).
- Unit, re-pinned from the mean rule: `internal/sprint/level_progress_test.go`
  `TestLevelTargetPicksAFreeLane`, `TestLevelStopsWhenNoFreeLaneRemains`;
  `internal/sprint/level_terminates_test.go`
  `TestLevelLeavesTheWedgeWhenNoLaneIsFree`,
  `TestLevelEndsWithNoFreeLaneWithinTheReadyCount`, `TestLevelMovesNoCardTwice`;
  `internal/sprint/refmodel/level_refuser_test.go`
  `TestLevelRefuserTheEngineAndTheModelAgree`,
  `TestLevelSkipsASourceWithNoMovableCardTheEngineAndTheModelAgree` (the
  engine's moves on that shape, at the model's width, are the model's own);
  `internal/sprint/refmodel/fleet_down_level_test.go`
  `TestFleetDownAndLevelGoRoundTheFleet`.
- Functional (`-tags functional`), `cmd/nova-sprint/start_level_functional_test.go`:
  `TestTheStartLevelLeavesADealtCardOnAMemberThatCanStartItOnTheStore`, on a
  private redis-server: a member of width 4 holding one ready card and a
  member of width 16 with free lanes; the ticks leave the card where it was
  dealt, and once the first member's width falls below what it holds, the
  next tick moves the card onto the free lane. Every verb names the private
  store with `--redis`; HOME and XDG_CONFIG_HOME are the test's own and its
  forward fails the test, so no verb can reach a live sprint server.

### A friend with no free lane is not dealt work (v1.2.14)

The friends' deal hands a work card to a friend whose lanes are all held only
when no machine that serves the card's tier has a free lane: a friend with no
idle lane gives way to a machine that starts the card at once, rather than
queueing it behind her working lanes, where it sits dealt and untaken until the
dealt bound returns it (docs/SPEC-SPRINT.md section 1,
friend-deal-idle-lanes-first.w1; the owner, 2026-10-12). A friend whose tiers
alone serve the tier, or whose card is a hard pin (`WHO: only friend`), still
takes it ahead of her lanes. On 2026-10-12 five heavy cards were dealt to a
friend with no free lane and sat 6-9 minutes while the fleet ran at 15 of 140
slots; the tick's level and the dealt bound's return leave the machines the
rest.

- Unit, `internal/sprint/friend_free_lane_test.go`:
  `TestFriendWithNoFreeLaneIsNotDealt`: a friend at width 2 with both lanes
  working and a machine up that draws flash with free lanes; a new flash card
  is dealt to the machine, never queued on her row.

### An environment exit

A child that ends 134 or 137, or leaves no result at any exit, is the member's
environment, never the card's (docs/SPEC-SPRINT.md, "An environment exit";
a-doctor-reports-the-child-exit-code-rate-per-member). These tests lock it in:

- Unit, `internal/sprint/attempts_test.go`:
  `TestEnvExitClassifiesTheMemberEnvironmentExit`, `TestEnvironmentExitNamesTheCode`,
  `TestHarnessTailKeepsTheLastFortyLines`,
  `TestEnvironmentExitRecordCarriesTheEndedWordAndTail`, `TestEnvExitRateThresholds`,
  `TestEnvExitCheckNamesTheRateAndTheCard`, `TestFinishRecordsAnEnvironmentExitOnTheAttempt`,
  `TestANoResultTakeIsAnEnvironmentExitAndStillAnEndedTake`,
  `TestFinishWithACleanEndOrAVerdictIsNeverAnEnvironmentExit`,
  `TestAReadsEnvironmentExitSpendsNoReader`,
  `TestEnvExitChildrenCountsTheMemberEnvironmentExits`,
  `TestTheDealRefusesACardAtTheEnvironmentExitCap`,
  `TestADecidedNothingToDoIsNeverAnEnvironmentExit`,
  `TestAReadsEnvironmentExitsAreCappedAndJudgedOnce`,
  `TestAReadersTableReadsEnvironmentExitsAreCapped`.
- Unit, `internal/sprint/brief_bound_test.go`:
  `TestACardAtBothCapsIsDealtToNoOne` (a card at the attempt cap and the
  environment-exit cap, with a frontier friend up with room, is dealt no attempt 5
  and held by one judgment).

The attempt cap (docs/SPEC-SPRINT.md, the bounds; the owner, 2026-10-12: "max attempts
should be configurable in nova-config, and default to 4"; "max attempts before you do
judgement and decide what to do"), each test red on the base of 2026-10-12 before the fix
and green after it:

- Behaviour on the twin store, `internal/sprint/max_attempts_test.go`, every rule on,
  driving the real deal, take, finish, read, rule and merge steps:
  `TestACardsFourthAttemptRaisesOneJudgmentAndNoFifthIsDealt` (four attempts ending as a
  deadline, a finding outside PATHS widened by rule, a finding inside PATHS reworked by
  rule and failed work: one judgment naming `max_attempts 4`, and twenty ticks deal no
  attempt 5; red before: the deadline did not count and the widen reset the count, so
  attempt 5 was dealt), `TestACoordinatorsDecisionOpensAFreshWindowOfAttempts` (the
  seat's `brief` answers the judgment and attempts 5 to 8 run, the eighth ending as a
  landing refused in the shared ledgers alone; the second judgment at 8 and no attempt 9;
  red before: no judgment after four attempts), `TestMaxAttemptsFromNovaConfigBoundsTheCap`
  (sprint:max_attempts "2" bounds at 2, a stream's own 6 holds over it, "x", "" and "0"
  are the default 4), `TestSetAttemptsIsRefusedNamingNovaConfig` (`set --attempts` refuses
  naming `nova-config sprint set --max_attempts <n>` and what the sprint reads now; the
  stream's own stays).
- Unit, `internal/sprint/brief_bound_test.go`:
  `TestACardAtTheCapIsDealtToNoOneUntilTheCoordinatorDecides` (a frontier friend up with
  room is dealt no attempt 5; the judgment is raised once and names every attempt's
  finding and the re-tier; `brief --tier` is the decision that answers it),
  `TestACardAtTheCapIsNotRedealtToAMachine`; `internal/sprint/bounds_test.go`:
  `TestAttemptsSinceCountsEveryAttemptSinceTheDecision`,
  `TestAtBriefBoundCountsEveryAttemptSinceTheDecision`.
- Store, `internal/sprint/store/attempt_cap_test.go`: `TestFailedWorkReachesTheAttemptCapToo`
  (the machine's rework between the attempts), `TestTheSeatsReworkAtTheCapIsADecisionAndOpensAFreshWindow`,
  `TestTheAttemptCapIsNovaConfigsMaxAttemptsAndTheStreams`.
- Unit, `pkg/config/kind_test.go`: `TestTheSprintRowsMaxAttemptsDefaultsToFour`,
  `TestCheckSprintHoldsMaxAttemptsToAWholeNumberFromOneToTheCeiling` (0, -1, 101, "x" and
  "2.5" refused naming the field and the remedy; 1, 4, 100 and "" accepted).
- TLC, `tla/SprintRules.tla` Parts reads and answers (`NoAttemptPastCapWithoutDecision`,
  reversed by `MCSprintRulesReadsBrokenWidenResets.cfg`,
  `MCSprintRulesReadsBrokenRuleReworkAtBound.cfg` and
  `MCSprintRulesAnswersBrokenDeadlineFree.cfg`) and `tla/FriendEmptyRun.tla`
  (`MCFriendEmptyRunBrokenCapUndecided.cfg`), recorded in `tla/RUNS.tsv`.
- Unit, `cmd/nova-sprint/doctor_test.go`: `TestDoctorNamesTheMemberEnvExitRate`,
  `TestDoctorNamesTheMemberEnvExitWarn`,
  `TestAnEnvironmentExitGoesBackToReadyAtTheSameAttempt`,
  `TestStatsChildrenPrintsThePerMemberExitHistogram`,
  `TestThreeEnvironmentExitsRaiseTheBoundOnceAndStopTheRedeal` (three 134/137
  exits on one card at one attempt raise one bound judgment naming the card,
  the members and the codes, and the card is dealt no more),
  `TestAReworkResetsTheEnvironmentExitCount`.
- Functional (`-tags functional`), `cmd/nova-sprint/envexit_functional_test.go`:
  `TestAnEnvironmentExitOnARealStore`, on a private redis-server with a
  nova-sprint binary it builds: ten cards finish on m1, two of them as
  environment exits (134, and a no-result at exit 0); each goes back to ready
  at the same attempt with its ended word and harness tail (the 134 spending
  no redeal, the no-result an ended take as the owner's rules say),
  the doctor prints the `member-env` FAIL line and `stats --children` the
  histogram, and the next tick deals the same work cards again. Every
  invocation passes `--redis` and `--actor`, with HOME and XDG_CONFIG_HOME
  its own, no `NOVA_*` variable and PATH the binary's directory and the
  system's, so no verb can reach a seat config or a live sprint server.

### Priority raises a selected set in one call

`cmd/nova-sprint/priority_set_test.go`,
`TestPriorityRaisesASelectedSetInOneCall`: `priority` takes the shared
selector of `cmd/nova-sprint/select.go` (`--who`, `--state`, `--ids-file`, or
`--stream` with any of them, and `--dry-run`). The test admits five cards in
two streams on a private store (named explicitly with `--redis`; HOME and
XDG_CONFIG_HOME are temporary directories and no `NOVA_*` variable is read,
so no host record or server can take a verb), writes an ids file of three and
runs `priority --ids-file raise.txt --critical --reason 'next point release'`:
exactly those three are critical, one `priority set` timeline entry each, and
the stream default is unchanged, so a card added later is still normal; the
output is one line per card and `selected=3`. `--dry-run` plans the same step
and writes nothing. An ids file naming a read card refuses the whole call with
nothing written.

### Release acceptance

`make release-accept` is the release gate of a point release. It replaces the ten-minute live
watch on the server after `server switch --rollback --window 30m` and the hand canary (the
owner, 2026-10-11: "I don't have time to wait 10 minutes for long checks each time you want to
test a new point release."). Each check the watch looked for is a test that pins the
behaviour, each on its own store (a fresh redis-server on a port of its own, or the in-memory
twin) and run in parallel; the set runs in under a minute on a bench and fails at two
(the script's clock starts before the build, and the build and the tests are killed when
the run passes 120 s). Every run of the binary is given `--redis <the test's own store>` and an
environment scrubbed of every `NOVA_` variable, with HOME and XDG_CONFIG_HOME its own, so no
verb is forwarded to a sprint server that NOVA_SPRINT_SERVER or a seat record names
(`TestReleaseAcceptTheBinaryNeverReachesAConfiguredServer` proves it against an outside
listener named both ways). `tools/release-accept.sh` holds the list of names; every name must run and
pass, so a test renamed or deleted fails the gate by its count instead of leaving the set.
`NOVA_SPRINT_BIN=<path>` names the build the canary runs (the release's binary); unset, the
tree's own is built once. It needs `redis-server` on PATH and fails, never skips, without one
(`NOVA_CI=1`). Measured on hetzner2 (32 cores), 2026-10-11, three runs: 5.4 s, 5.3 s and 7.2 s of
wall time, the binary's build included; 41 of 41 passed each time.

| check | tests |
|---|---|
| 1. server switch: the restart drops the seat's hook, the new build never rolls back on the hook gate's refusal (#99), and keeps landing | `TestAHookGateRefusalInTheWindowIsNotARollback`, `TestARealLandFailureInTheWindowStillRollsBack`, `TestASwitchedServerKeepsLandingOnceTheSeatHooksInAgain`, `TestServerSwitchVerbSwitchesAndRollsBack`, `TestTheHookGateRefusalIsATypedError`, `TestCheckRollbackOnLandFailureExitCodes` |
| 2. ticks: N ticks of a busy sprint on a real store, none failed, no panic, no `REFUSED drain`, `check` clean after | `TestReleaseAcceptTicksOfABusySprintNeverFail` |
| 3. needs (#96): never dealt with needs unlanded, a dealt one withdrawn working -> waiting, ready once they land, a needs-stop never the worker's | `TestReleaseAcceptNeedsHoldACardOffTheDealOnTheStore`, `TestTheFriendsDealNeverDealsACardWithUnlandedNeeds`, `TestADealtThenUnheldFriendDoesNotTakeACardWithUnlandedNeeds`, `TestAMemberTakeRefusesACardWithUnlandedNeeds`, `TestAReadyCardWithAnUnlandedNeedIsNeverDealt`, `TestAStopOnUnlandedNeedsIsNotTheWorkers`, `TestANoUsageNeedsStopIsStillOneTake`, `TestACardWithdrawnForItsNeedsIsDealtWhenTheyLand`, `TestLawfulWorkingToWaitingNeedsTheCardsOwnNeedNotLanded` |
| 4. ledger landing (#100): fixes.sexp by id, RUNS/CASES/COVERAGE.tsv as a union, evidence keeps both revisions, FIXES.md regenerated; a code conflict still refused | `TestALedgerOnlyConflictLandsWithBothSidesRows`, `TestASameIdFixConflictIsRefused`, `TestACodeConflictIsStillRefused`, `TestEvidenceFormsKeepBoth`, `TestEvidenceHistoryKeepsEveryRevision` |
| 5. done (#94): a row's done is its completions (work and fix cards that reached ok, reads with a verdict) | `TestADoneIsEveryCardTheRowCompleted`, `TestDoneCountsEveryKindOfCardFromTheLinesThatCompleteIt` |
| 6. idle alarm (#102, #106): once an episode, reads are not work | `TestTheIdleAlarmIsRaisedOnceAnEpisodeNeverOncePerTick`, `TestTickIdleAlarmsOnceAnEpisodeAndClears`, `TestTheIdleAlarmNamesTheRootsOnceAnEpisode`, `TestTickIdleAlarmTellsTheCoordinatorWhy`, `TestTheFleetAlarmIgnoresReadsCrossingTheThreshold`, `TestTheIdleFleetCountsWorkNotReads` |
| 7. lifecycle (#103): every move lawful; no need cycle through needs set, sentinel set, relink or a brief batch | `TestLifecycleIsTheSpecTable`, `TestLawfulRefusesAMoveOutsideTheLifecycle`, `TestTheEngineHoldsEveryPlanToTheLifecycle`, `TestNeedsSetRefusesAnEditThatClosesACycle`, `TestNeedsSetSeesAWorkingCardsNeeds`, `TestSentinelSetSeesAWorkingCardsNeeds`, `TestRelinkSeesAWorkingCardsNeeds`, `TestABriefBatchCannotCloseACycleBetweenItsCards`, `TestAddRefusesACycle` |
| 8. progress: a cleared sprint deals, works, reads, accepts and lands with no seat action after start. The readers are re-added after the clear because of a known defect (the clear blanks them); the card a-cleared-sprint-keeps-its-readers owes the fix, and when it lands the re-add goes and the test proves the defaults too | `TestReleaseAcceptAClearedSprintLandsWithNoSeatAction` |
| 9. canary: never reaching a configured server (`TestReleaseAcceptTheBinaryNeverReachesAConfiguredServer`); on the release's binary, init, the member's beat, the ticks, and one member's take and finish ok of one card (the binary refuses a store whose function library is not its own) | `TestReleaseAcceptCanaryOneMemberFinishesOneCardOnTheBuild`, `TestReleaseAcceptTheBinaryNeverReachesAConfiguredServer` |

After the gate a release may still run a live canary of one card on one member of the
adopted fleet; there is no timed watch.

### Stall and stuck coverage

Every fix of 2026-10-11 for a stall, a card stuck or the machine not working is held by a unit
test and a functional test, and both are in `make release-accept` (the owner, 2026-10-11:
"Each time we find these terrible breakages, we clip in with unit tests and functional tests,
so this way of failing never happens again."). A functional test here is a real store (a
redis-server on a port of its own, the function library loaded) with the binary under
acceptance running the workers', the readers' and the machine's verbs (the tick, `run`), and
the coordinator's verbs in process: the binary arms the seat's hook gate for every coordinator
verb and a test cannot hook in (cmd/nova-sprint/stall_harness_functional_test.go). Each
`TestStall...` test was red against the parent of its fix. A class whose fix is not on main has
no test yet; its GAP line names the card or pull request that owes both, and that work adds them
to this table and to tools/release-accept.sh. With these the gate is 70 tests; measured on
hetzner2, 2026-10-11, three runs: 15.1 s, 8.1 s and 8.1 s of wall time, the build included.

| class | fix | unit tests | functional tests |
|---|---|---|---|
| hook-gate race on a server switch: the restart drops the seat's hook, the first land pass is refused by the gate, and the switch rolled back 1.5 s in | #99 (846c7364) | `TestAHookGateRefusalInTheWindowIsNotARollback`, `TestARealLandFailureInTheWindowStillRollsBack`, `TestTheHookGateRefusalIsATypedError`, `TestCheckRollbackOnLandFailureExitCodes`, `TestServerSwitchVerbSwitchesAndRollsBack` | `TestStallASwitchedServerNeverRollsBackOnTheHookGate` (the switched binary's `run --land`, no seat hooked), `TestASwitchedServerKeepsLandingOnceTheSeatHooksInAgain` |
| a card dealt or taken before its needs land; a needs-stop blamed on the worker | #96 (50aa393d) | `TestTheFriendsDealNeverDealsACardWithUnlandedNeeds`, `TestADealtThenUnheldFriendDoesNotTakeACardWithUnlandedNeeds`, `TestAMemberTakeRefusesACardWithUnlandedNeeds`, `TestAReadyCardWithAnUnlandedNeedIsNeverDealt`, `TestAStopOnUnlandedNeedsIsNotTheWorkers`, `TestANoUsageNeedsStopIsStillOneTake`, `TestACardWithdrawnForItsNeedsIsDealtWhenTheyLand`, `TestLawfulWorkingToWaitingNeedsTheCardsOwnNeedNotLanded` | `TestReleaseAcceptNeedsHoldACardOffTheDealOnTheStore` |
| landings stalled on conflicts in the shared ledgers alone (about 300 refusals to 35 landings an hour) | #100 (b1a020b2) | `TestALedgerOnlyConflictLandsWithBothSidesRows`, `TestASameIdFixConflictIsRefused`, `TestACodeConflictIsStillRefused`, `TestEvidenceFormsKeepBoth`, `TestEvidenceHistoryKeepsEveryRevision` | `TestStallALedgerOnlyConflictLandsOnTheStore` |
| done read 0 for work read on fleet read cards, which never left finished | #94 (ee8fb936) | `TestADoneIsEveryCardTheRowCompleted`, `TestDoneCountsEveryKindOfCardFromTheLinesThatCompleteIt` | `TestStallDoneCountsEveryCompletionOnTheStore` |
| the idle alarm: once an episode, and reads working are not work | #102 (tests), #106 (b2aa60ba) | `TestTheIdleAlarmIsRaisedOnceAnEpisodeNeverOncePerTick`, `TestTickIdleAlarmsOnceAnEpisodeAndClears`, `TestTheIdleAlarmNamesTheRootsOnceAnEpisode`, `TestTickIdleAlarmTellsTheCoordinatorWhy`, `TestTheFleetAlarmIgnoresReadsCrossingTheThreshold`, `TestTheIdleFleetCountsWorkNotReads` | `TestStallTheIdleAlarmCountsWorkNotReadsOnTheStore` |
| a need cycle through a working card: two cards waiting on each other for ever | #103 (8dd283b8, cf237351) | `TestLifecycleIsTheSpecTable`, `TestLawfulRefusesAMoveOutsideTheLifecycle`, `TestTheEngineHoldsEveryPlanToTheLifecycle`, `TestNeedsSetRefusesAnEditThatClosesACycle`, `TestNeedsSetSeesAWorkingCardsNeeds`, `TestSentinelSetSeesAWorkingCardsNeeds`, `TestRelinkSeesAWorkingCardsNeeds`, `TestABriefBatchCannotCloseACycleBetweenItsCards`, `TestAddRefusesACycle` | `TestStallANeedCycleThroughAWorkingCardIsRefusedOnTheStore` |
| a refused tick step poisons the drain: a 40 KiB lander note in a card's why (16:35Z, 20 minutes frozen) | #91 (d30ab7f0) | `TestAnOverBoundMergeNoteIsRefusedAndTheTickStillDrains`, `TestAMergeNoteAtTheBoundIsTaken`, `TestMergeNoteOverTheBoundIsRefusedAtTheVerb`, `TestCardTextBoundIs64KiB`, `TestBoundedChecksKeepsWholeFindingsAndSaysWhatItLeftOut` | `TestStallAnOverBoundMergeNoteNeverPoisonsTheDrain` |
| a refused tick step poisons the drain: a brief over 16 KiB taken on a running machine (04:17Z, 37 minutes frozen) | brief-file-over-the-bound-is-refused (101b2e30) | `TestBriefFileOverTheBoundIsRefused`, `TestABriefOverTheBoundIsRefusedWhileRunning` | `TestStallAnOverBoundBriefNeverPoisonsTheDrain` |
| a server build switched onto a store missing its locked columns: every finish refused for 40 minutes | server-refuses-to-run-on-a-store-missing-locked-columns (2788a093), doctor-preflight-checks-the-store-schema (3e55d518) | `TestShadowTickRefusesAStoreMissingTheLockedFleetColumns`, `TestRunRefusesAStoreMissingTheLockedFleetColumns`, `TestLockedShapeRefusesTheFleetTableBeforeTheVerdictsMigration`, `TestDoctorPreflightRefusesAStoreMissingTheLockedColumns` | `TestStallTheBinaryRefusesAStoreMissingItsLockedColumns` |
| reads queued behind full lanes while a member sat idle (the rebalance skipped reads; fleet level moved 0) | the-rebalance-levels-reads-every-tick-all-at-once (77eb17e6) | `TestTheRebalanceMovesQueuedReadsToAnIdleMemberInOneTick`, `TestTheRebalanceMovesAllTheCardsInOneTickNeverOnePerTick`, `TestTheLevelVerbRebalancesTheReadsToo` | `TestStallQueuedReadsMoveToAMemberThatComesUpOnTheStore` |
| a read the member is not running held working to the 60-minute deadline (ghost reads) | #112 (5d5959c6) | `TestAReadTheMemberDoesNotRunIsReturnedAtTheNextBeat`, `TestAMemberRestartReturnsItsReadsAtOnce`, `TestAWorkingReadTheBeatNamesIsNeverReturned`, `TestAReadWhoseStageFailedIsRunAgainNotHeldAsAGhost` | `TestAMemberProcessKilledOrRestartedLeavesNoGhostRead`, `TestAMemberKilledSilentGivesItsReadsBackByTheDownPath` (#112's own, red-first there) |

GAP lines, owed by work not on main (each adds its unit and functional test to the table and to
tools/release-accept.sh when it lands):

- GAP: any other whole-step refusal inside a tick step is still retried every second with no
  judgment and no skip. Owed by card `a-poisoned-queued-change-never-stops-the-drain`.
- GAP: read cards are off by default and a clear resets them off, so reads crawl one a tick.
  Owed by PR #92 (defaults are the working machine).
- GAP: a clear empties the readers table, so after a clear every finished card waits in review
  with no judgment (`TestReleaseAcceptAClearedSprintLandsWithNoSeatAction` re-adds the readers to
  pass). Owed by card `a-cleared-sprint-keeps-its-readers`.
- GAP: a wait parks a judgment that holds a card (failed work, a broken read), and about 22 of 42
  review cards froze. Owed by PR #111 (its tests are store tests on the twin; its functional test
  is owed with it).
- GAP: a card dropped while live cards need it strands them in merging and waiting with no
  alarm. Owed by card `every-need-names-a-live-card-checked-every-second`.
- GAP: a card the store moved to another member keeps its old run going on the first. Owed by
  card `a-moved-card-stops-its-old-run`.
- GAP: the server's land loop waits on the seat's hook, so landing stops while the seat is away.
  Owed by card `landing-never-waits-on-the-seats-hook` (`TestStallASwitchedServerNeverRollsBackOnTheHookGate`
  asserts no rollback only, so it stays green when landing stops waiting).
- GAP: a seat's wait comes back overdue after a reconnect or a restart. Owed by card
  `a-seat-wait-survives-a-reconnect-and-a-restart`.
- GAP: a taken card given an unlanded need runs to review with it. Owed by card
  `a-taken-card-given-an-unlanded-need-never-reaches-review-with-it`.
- GAP: every producer card has its consumer out after the tick (on main, 56b4f817) is a check
  that cannot fail as written: its working and fix branch reads a `primary` field no primary
  carries, its review branch passes on any read card of the readers table and flags a card read on
  fleet read cards, and `TestEveryProducerCard` asserts nothing. It is not in release-accept; the
  check and its tests are owed again by the card `every-producer-card-has-its-consumer-out-after-the-tick`.


## nova-card

Fixture: `cmd/nova-card/testdata/findings.tsv`, a reader's findings on two
files, typed as `./cmd/nova-card/testdata/findings.tsv` from the root of a
checkout; `./cards` is a directory the first line creates.

### First run

```
$ nova-card generate --from findings --file ./cmd/nova-card/testdata/findings.tsv --repo example/repo --base dev --sha 0123456789abcdef0123456789abcdef01234567 --out ./cards
CARDS OK dir=./cards cards=2 waves=1 tier=pro

$ nova-card lint --card ./cards/finding-internal-bus-send.md
LINT OK file=./cards/finding-internal-bus-send.md

$ nova-card lint --card ./cards/finding-cmd-nova-bus-main.md
LINT OK file=./cards/finding-cmd-nova-bus-main.md
```


## nova-work

Run by `cmd/nova-work/firstrun_test.go` against a recorded conversation with
GitHub (`internal/workgh/testdata/reliable`: one public repository of twenty
issues, read at fifteen a page), so no network is used and no process is
started. `$ORG` and `$REPO` are yours: the test stands them for the recording's
organization and repository, and the counts below are that repository's. `gh=`
names the gh a run used; yours is the gh on your PATH, and here it is `./gh`,
where the test says it found one, while every call is answered from the
recording. The `sha256` is the tree file's, and the tree records the instant it
was fetched, so it differs on every real run; the test passes a fixed time in
so the value below reproduces. `./tree.lisp` is a file in a directory of the
test's own. The usage banner's `example:` block is this same sitting, line for
line.

Requires: a gh login that can read the repository (`gh auth status`); import
(the dry run too) and verify read GitHub through gh and write nothing there.

### First run

```text
$ nova-work import --org $ORG --repo $ORG/$REPO --page-size 15 --dry-run
IMPORT OK org=$ORG out=- repos=1 issues=20 comments=74 references=4 linked_prs=2 bytes=65241 sha256=4c73aa56b83994be50a4ea6da3bf8fb72735b1051d5c74ea320a60e6709dc938 calls=3 points=3 rest=0 seconds=0.0 gh=./gh dry_run=true
IMPORT PLAN repos=1 issues=20 est_calls=3 max_calls=1500 page_size=15
IMPORT REPO repo=$ORG/$REPO issues=20 comments=74 references=4 linked_prs=2 calls=2
IMPORT NOTE the dry run read GitHub as the import does (calls=3, read-only) and wrote nothing

$ nova-work import --org $ORG --repo $ORG/$REPO --page-size 15 --out ./tree.lisp
IMPORT OK org=$ORG out=./tree.lisp repos=1 issues=20 comments=74 references=4 linked_prs=2 bytes=65241 sha256=4c73aa56b83994be50a4ea6da3bf8fb72735b1051d5c74ea320a60e6709dc938 calls=3 points=3 rest=0 seconds=0.0 gh=./gh
IMPORT PLAN repos=1 issues=20 est_calls=3 max_calls=1500 page_size=15
IMPORT REPO repo=$ORG/$REPO issues=20 comments=74 references=4 linked_prs=2 calls=2

$ nova-work verify --tree ./tree.lisp --repo $ORG/$REPO --page-size 15
VERIFY OK tree=./tree.lisp sha256=4c73aa56b83994be50a4ea6da3bf8fb72735b1051d5c74ea320a60e6709dc938 repos=1 issues=20 comments=74 calls=3 points=3 rest=0 seconds=0.0 differences=0 missing=0 extra=0 drift=0 gh=./gh
```

The dry run is not offline: it reads GitHub exactly as the import does (every
issue, read-only, the same calls) and writes nothing, and its last line says so.
`est_calls` is the calls the import will spend, checked against `max_calls`
before any issue is read, and `out=-` says no file was written. The import
prints the same plan, one `IMPORT REPO` per repository, and the `sha256` of the
file it wrote; verify names the same `sha256`, and when the two differ it says
`VERIFY FAILED` with one `VERIFY MISSING`, `EXTRA` or `DRIFT` line per difference.
`differences=0` is the proof the tree holds what GitHub holds.

## Judgments that hold a card (v1.2.14, PR #111)

Failed work and broken reads rework themselves, and no wait parks a card
(docs/SPEC-SPRINT.md section 8, "Judgments that hold a card"). Each test by its exact name:

- `TestFailedWorkReworksItselfWithItsFinding` (internal/sprint/store/no_parked_test.go)
- `TestABrokenReadReworksItself` (internal/sprint/store/no_parked_test.go)
- `TestPastTheBoundItStaysASeatJudgment` (internal/sprint/store/no_parked_test.go)
- `TestWaitIsRefusedOnACardHoldingJudgment` (internal/sprint/store/no_parked_test.go)
- `TestWaitStillWorksOnAnAlarm` (internal/sprint/store/no_parked_test.go)
- `TestASharedFailureIsOneFleetJudgmentAtOnce` (internal/sprint/store/no_parked_test.go)
- `TestAFailurePerTickRaisesOneFleetJudgmentAtTheThird` (internal/sprint/store/no_parked_test.go)
- `TestAnAckedFleetFailureHoldsUntilANewFailure` (internal/sprint/store/no_parked_test.go)
- `TestARepairedFailureDoesNotReopenAnAckedFleetFailure` (internal/sprint/store/no_parked_test.go)
- `TestTheFleetFailureIsRaisedWithTheRulesOff` (internal/sprint/store/no_parked_test.go)
- `TestANoRoomReworkKeepsARealFailureCounted` (internal/sprint/store/no_parked_test.go)
- `TestAnOkFinishThenNoRoomReworkDoesNotReopenTheAlarm` (internal/sprint/store/no_parked_test.go)
- `TestAFailureRecordedBeforeTheUpgradeIsNotCountedUntilItsNextFinish` (internal/sprint/store/no_parked_test.go)
- `TestAPinnedModelFailureReworksOnItsTier` (internal/sprint/store/no_parked_test.go)
- `TestAFrontierTierFailureReworksOnItsTier` (internal/sprint/store/no_parked_test.go)
- `TestAFrontierCardIsReadAtHeavyOfTwoFleetReadersAndACoordinatorReadCounts` (cmd/nova-sprint/frontier_read_functional_test.go)
- `TestTheSameFailureOnManyCardsIsReworkedOnItsTier` (internal/sprint/store/rule_answers_test.go)
- `TestEveryKindThatHoldsACardRefusesWait` (internal/sprint/wait_refused_test.go)
- `TestNoJudgmentOffersADecisionItsVerbRefuses` (internal/sprint/wait_refused_test.go)
- `TestAStoppedStreamCountsTheCardsItHolds` (internal/sprint/wait_refused_test.go)
- `TestAStallRefusesWaitOnlyWhenItNamesACard` (internal/sprint/wait_refused_test.go)
- `TestNoPerNoteDecisionOffersAWaitItsVerbRefuses` (internal/sprint/wait_refused_test.go)
- `TestAPartlyAnsweredJudgmentCountsOnlyTheCardsStillHeld` (internal/sprint/wait_refused_test.go)
- `TestAJudgmentNamingACardRefusesWaitWhateverItsKind` (internal/sprint/wait_refused_test.go)
- `TestWaitByGroupIsRefusedOnFailedWork` (cmd/nova-sprint/no_parked_test.go)
- `TestWaitByAliasIsRefusedOnFailedWork` (cmd/nova-sprint/no_parked_test.go)

### An overdue judgment never offers a refused wait (v1.2.16)

The inbox adds an `act` line for overdue groups. Past a card-holding judgment's deadline that
line must name the action `sprint.WaitRefused` gives for the kind, never the wait the store
would refuse. One fix, in the shared command rendering (`sprint.commands`), covers the three
places it is shown: the inbox group's commands, the hook's push and the act line (all render
from it; `cmd/nova-sprint/reads.go groupText`).

- Unit, `internal/sprint/wait_refused_test.go`:
  `TestOverdueCardHoldingJudgmentOffersNoWait`: a table over every kind of `WaitRefused`
  (and a stall that names a card) renders the group's commands with an overdue `act`, and
  asserts no `wait` decision or `nova-sprint wait` line appears anywhere and the act line
  names the action `WaitRefused` lists.
- Functional (`-tags functional`), `cmd/nova-sprint/overdue_no_wait_functional_test.go`:
  `TestAnOverdueCardHoldingJudgmentOffersNoWaitOnTheStore`, on a private redis-server with
  explicit `--redis` and a scrubbed environment: a failed work judgment holds its card; read
  with a tiny `--deadline`, the inbox (and the hook's rendering of the same group) offers no
  wait and names the action.

### Every producer card has its consumer out (v1.2.14, redone)

After every tick, every producer card (working, review) has its consumer out: a working card
a live attempt on a member (or up friend) at its current generation, a review card a read out
for it — by card, never "any read" — or an open judgment that names it and is refused wait
(docs/SPEC-SPRINT.md, "The tick postconditions"). A missing consumer raises one judgment
naming the card and what is missing, at that tick.

- Unit, `internal/sprint/consumers_out_test.go`:
  `TestConsumersOutWorkingCardWithNoAttempt` (a working card whose work field names no live
  attempt), `TestConsumersOutReviewCardWhoseReadsAreAllForOtherCards` (the readers table holds
  cards, none a read out for this card), `TestConsumersOutReviewCardWithNoReadAndNoJudgment`
  (no read and no open judgment naming it), and
  `TestConsumersOutDoesNotFlagCardsWhoseConsumersAreOut` (the cards whose consumers are out
  raise nothing).
- Functional (`-tags functional`), `cmd/nova-sprint/consumers_functional_test.go`:
  `TestEveryProducerCard`: a real store with a review card, its readers table emptied; one
  tick raises the consumers-out judgment naming the card.

### The machine's commit identity

Every commit the sprint machine makes carries the sprint row's `git_identity`
(`nova-config sprint set --git_identity 'Name <email>'`), never the host's global
git config; the landing refuses to commit when none is configured, and `doctor`
fails when its email equals the host's global `user.email`
(docs/SPEC-SPRINT.md section 7, the machine's commit identity).

- Unit, `cmd/nova-sprint/land_identity_test.go`:
  `TestLandCommitCarriesTheConfiguredIdentity` (a landing commit's author and
  committer are the configured identity even when HOME carries a different global
  user.email), `TestLandRefusesWhenNoIdentityIsConfigured` (the landing refuses,
  naming the setting, when the sprint row names no git_identity).
- Unit, `cmd/nova-sprint/promote_end_to_end_test.go`:
  `TestPromoteCarriesACutToARecordedPromotion/clean` reads the promotion cut's
  `%an <%ae>|%cn <%ce>` and holds it to the configured identity;
  `TestPromoteRefusesWithoutAMachineIdentity` (the promotion refuses to cut,
  naming the setting, when the sprint row names no git_identity).
- Unit, `pkg/swarm/stage_identity_test.go`:
  `TestReworkAtTipCarriesTheConfiguredIdentity`: the rework-at-tip commit
  (`restageAtTip`) carries the configured identity even when the host's global
  git config and environment name the owner; `TestStageJobCloneCarriesTheConfiguredIdentity`:
  the live launcher entry points `StageJob`/`StageCloneIdentity` (and their
  `As` variants) write the machine's identity into the worker's clone's local
  git config at clone time, never the pool's owner; `TestStageCardReworkCarriesTheConfiguredIdentity`:
  the live `StageCard` entry point writes the machine's default identity into the
  clone's local config and the rework-at-tip carry commit carries it, even when
  the host's global config names the owner.
- Functional (`-tags functional`), `cmd/nova-sprint/land_identity_functional_test.go`:
  `TestLandCommitIdentityOnARealStore` (a real landing on a private store, explicit
  `--redis`, scrubbed env with a planted global git config, reads
  `git log --format='%an <%ae>|%cn <%ce>'`).

### The server's version heartbeat (v1.2.16)

While `nova-sprint run` runs it writes one heartbeat a second to the store (one `SET` with an
expiry, TTL three seconds) holding the version `nova-sprint version` prints, its commit, its
start and the write time; `where --json` carries it as `server_version` plus `server`
(`version`, `commit`, `started`, `at`), and both are absent when the heartbeat is missing or
older than the TTL, so a stale version is never shown.

- Unit, `cmd/nova-sprint/server_heartbeat_test.go`:
  `TestServerHeartbeatCarriesTheBinarysVersion`: the heartbeat appears at once, carries the
  binary's version, its commit and its start, and is gone (no server) after the TTL;
  `TestWhereCarriesTheServerVersion`: `where --json` carries `server_version` and `server`
  while the heartbeat is fresh and neither once it is stale.
- Functional (`-tags functional`), `cmd/nova-sprint/server_heartbeat_functional_test.go`:
  `TestServerHeartbeatOnAStoreWhereJSONReads`, a real run loop on a private store with an
  explicit `--redis` and a scrubbed environment (HOME and XDG_CONFIG_HOME in a TempDir, no
  `NOVA_*`): after ticks, `where --json` states the server's own version, and one heartbeat
  write is one SET.

### The audit names every blocked, stuck or looped card

`nova-sprint audit` is the one answer to "is anything blocked, stuck or looped":
one read of the work, fleet and readers tables, one line per unlanded primary
with its id, stream, state, tier and attempt, its holder (the member or friend
whose working card holds it, the lane and since when) or `-`, the age in its
current state and attempt, the ends of its last four attempts and the needs not
yet landed with their states, and its flags: `NO-HOLDER` (ready past the dealt
bound, or working with no holder), `ATT>=4`, `FAILS=<n>` (six or more failed,
idle-reaped, bound or brief-defect ends), `STUCK` (unchanged past `--stuck`,
default 10m) and `NEEDS-GONE`. It ends `AUDIT OK cards=<n> flagged=<n>`;
`--json` is one object a line and `--stream` narrows it.

- Unit, `cmd/nova-sprint/audit_test.go`:
  `TestAuditNamesEveryBlockedStuckOrLoopedCard`, on a private in-memory store
  with the app's own clock: one card of each trouble (a ready card past the dealt
  bound, an attempt-4 card, a card with six failed ends, a card unchanged past
  `--stuck 1m`, a card needing a card that is no card) and one healthy card; each
  flag appears on its line and none on the healthy one, and the same in `--json`.

### The reads review shows who holds each review read

`nova-sprint reads --review` is the one answer to "who holds each review read"
(`reads` is a new verb name beside the worker's `read`): one read of the work,
fleet and readers tables, one line per primary in review with its attempt, then
each of its read cards at that attempt — the reader (a member's reader row, or a
friend), its state (`asked`, `reading`, `returned`, `withdrawn`, `ended`), for
how long, and its last read outcome (its verdict, `no verdict (ran=false)` for a
read that never ran, or the return or hold reason). A card with no read out and
no open judgment is flagged `NO-READ-OUT`. It ends
`READS OK review=<n> no-read-out=<n>`; `--json` is one object a line and
`--stream` narrows it.

- Functional, `cmd/nova-sprint/reads_review_test.go`:
  `TestReadsReviewShowsWhoHoldsEachReviewRead`, on a private in-memory store
  with the app's own clock: three primaries in review, one with a read reading
  for a member, one whose read was withdrawn by a hold and another that ended
  with no verdict, and one with no read at all; the holder and its duration are
  asserted on the first, both outcomes on the second, `NO-READ-OUT` on the
  third, and the same in `--json`.

## An acknowledged rate or health judgment stays quiet (v1.2.14)

A rate or health judgment the seat has acked is not raised again while its numbers are the
same episode: it stays quiet until its numbers cross a new threshold or its stated quiet
period passes, and the push says why it is back.

- `TestAckedHealthJudgmentStaysQuiet` (`internal/sprint/reads_window_twin_test.go`): the
  broken-reads-outrun judgment is raised; the seat acks it (the ack is accepted); more broken
  reads in the same episode stay quiet (one notice, never re-raised with the same numbers);
  after thirty minutes of running time still broken, the quiet period has passed and it is
  raised again.
- `TestAnAckedHealthJudgmentIsRaisedAgainSayingWhy` (`internal/sprint/reads_window_test.go`):
  when an acknowledged broken-reads-outrun judgment's quiet period has passed and its condition
  still holds, the next push says why it is back ("quiet period has passed").

## Release acceptance

The release-acceptance list (PR #110) takes these tests so this way of failing cannot come back unseen:

- `TestAMovedCardsOldRunIsStoppedAtTheNextBeat`: a card moved from m1 g1 to m2 g2; m1's next beat names g1; the answer stops it; m1 ends that child alone.
- `TestOldAttemptIsStopped`: an old attempt (.w9 when the card is at .w10) is stopped the same way.
- `TestRunNeverStopsValidRun`: a run the store holds at that member and generation is never stopped.
- `TestEveryCardHoldingJudgmentIsAnsweredOrEscalatedOnAStore` (functional, `cmd/nova-sprint/unanswered_judgment_functional_test.go`): a real server and store, the seat hooked in and silent, one card of each kind that held a card on 2026-10-12; within one deadline the cards with a safe default have moved with no seat verb, and "the seat is not answering" names the card still held on the hook's stream and on the owner's bus.
- `TestATwoReadsPassedCardIsMergingAfterOneTickWithNoSeatVerb`, `TestACardReturnedForItsNeedsIsMergingAfterOneTick`, `TestASeatAcceptStreamKeepsItsCardForTheSeat`, `TestACardReturnedByTheSeatStaysForTheSeat` (`cmd/nova-sprint/seat_answers_test.go`): the accept defaults of a card-holding judgment.
- `TestAHardPinToAHeldFriendIsReleasedAtItsDeadline`, `TestAPinIsNotReleasedByTheTimeTheMachineWasStopped` (`internal/sprint/seat_answers_test.go`): the pin's default, at its deadline in running time; a stop and resume releases nothing at once.
- `TestABoundJudgmentUnansweredRaisesTheSeatNotAnsweringOnce` (`internal/sprint/store/seat_silent_bound_test.go`): a card at its bound (a mind's judgment) unanswered past its deadline raises the alarm once, never waited or acknowledged, raised again with the bound at the next deadline, cleared by the answer.
- `TestATierNoRouteServesIsRaisedToTheNearestServedTierAtItsDeadline`, `TestAHeavyCardNoRouteServesIsNeverReTieredToFlash`, `TestAPinnedCardNoRouteServesStaysOnItsPinnedTier` (`internal/sprint/seat_answers_rule_test.go`): the no-route default, never down a tier, never over a pin.
- `TestASeatSilentSendThatFailsIsTriedAgainUnderOneToken`, `TestARestartedServerTellsTheOwnerOfARaiseOnce`, `TestASeatSilentRaiseNoLongerOpenIsForgotten` (`cmd/nova-sprint/seat_silent_test.go`): the owner's bus message is marked told only once sent, retried under one token, sent once across a restart, and the marks pruned to the open raises.
- `TestAHeavyCardIsDealtOnAHeavyRouteOnAStore` (functional, `cmd/nova-sprint/tier_line_functional_test.go`): a real store with a flash and a heavy route; a card whose RESULT line says tier: heavy is admitted saying tier=heavy and its first deal draws the heavy route.
- `TestLandingUnhookedLandsOnARealServer` (functional, `cmd/nova-sprint/landing_unhooked_functional_test.go`): a real server and store; the seat hooks in, a card is accepted, the hook lapses (its live-until passes with no prove) and the card lands anyway within one tick of the server's own land loop, while a hand land by the coordinator is still refused until the seat hooks in again.
- `TestTheTierLineIsReadOrRefusedByOneRuleInBothForms` (`internal/card/tier_line_test.go`), `TestTheTierOnLineOneIsReadInBothForms`, `TestAddStoresTheTierLineOneNamesOrItsStreamsDefault`, `TestAHeavyCardsFirstDealDrawsAHeavyRoute`, `TestABriefWhoseLineOneNamesAStrongerTierRaisesItsTierNow` (`internal/sprint/tier_line_test.go`) and `TestAddPrintsTheTierEachCardIsStoredWith`, `TestBriefPrintsTheTierTheCardIsDealtOn` (`cmd/nova-sprint/tier_line_test.go`): the tier line, read or refused by one rule in both forms, a stream's default, printed by add and brief, a stronger re-brief raising tier_now, dealt on.
- `TestFriendWithNoFreeLaneIsNotDealt` (`internal/sprint/friend_free_lane_test.go`): a friend with no free lane is not dealt work while a machine that serves the card's tier has one, so the card is started at once instead of queueing behind her lanes.
- `TestAHookStaysLiveAcrossThreeBoundsWhileItsSeatActs` (functional, `cmd/nova-sprint/hook_live_functional_test.go`): a real server and store, the hook held by `nova-sprint hook` over the server's listener, the server's clock stepped past three 15-minute bounds while the seat acknowledges its pushes: no lapse, one challenge, and an unhook and hook again replays only the push not acknowledged.
- `TestAConnectedSeatThatActsNeverLapses`, `TestASilentSeatLapsesOnceAndSaysSo`, `TestAnotherSeatsProofLeavesALapseOpen` (`cmd/nova-sprint/hook_test.go`): a seat acting once a minute for 40 minutes never lapses; one with no act lapses once, with one judgment naming the seat, the owner told once; another seat's proof leaves that lapse open.
- `TestConsumersOutWorkingCardWithNoAttempt`, `TestConsumersOutReviewCardWhoseReadsAreAllForOtherCards`, `TestConsumersOutReviewCardWithNoReadAndNoJudgment` (`internal/sprint/consumers_out_test.go`) and `TestEveryProducerCard` (functional, `cmd/nova-sprint/consumers_functional_test.go`): the consumers-out postcondition — a working card with no live attempt, a review card whose reads are all for other cards, and a review card with no read and no open judgment each raise one judgment naming the card, and a real store whose readers table is emptied raises it within one tick.
- `TestDropRefusesACardAnUnlandedCardNeeds`, `TestDropWithWaiveLeavesNoStrandedNeed` (`internal/sprint/missing_need_test.go`) and `TestDropRefusesANeededCardAndWaiveLeavesNone`, `TestDropGroupRefusesANeededCard`, `TestDropWithRelinkRepointsTheNeed` (functional, `cmd/nova-sprint/missing_need_functional_test.go`): rule (a), drop of a card an unlanded card needs is refused naming the dependants — by id or as a group — and `--waive` and `--relink` each pass and leave no stranded need.
- `TestEveryStateAlertsTheCoordinatorOfAGoneNeed` (functional, `cmd/nova-sprint/missing_need_functional_test.go`): rule (b), a real store and the seat hooked in, a need taken off the table by a store edit (a path that can strand it) for a card in each state (waiting, ready, working, review, merging), and the coordinator's hook stream receives the judgment naming the card and the missing need within one second of running time each time; `TestAGoneNeedRepeatsAtItsDeadlineAndNeedsSetClearsIt` (functional, `cmd/nova-sprint/missing_need_functional_test.go`): the judgment stays open, repeats at its deadline, and clears when answered with `needs set`; `TestTickNeedsJudgesAGoneNeedInEveryUnlandedState`, `TestTickNeedsHoldsAndClears` (`internal/sprint/missing_need_test.go`): the same judgment per state, its non-doubling while open and its closing when the need is set away.
- `TestNeedsSetOnAMergingCardWithAGoneNeed` (functional, `cmd/nova-sprint/missing_need_functional_test.go`): rule (b)'s second part, needs set on a merging card whose need is gone succeeds in place.
- `TestAcceptRefusesACardWhoseNeedHasNotLanded` (`internal/sprint/missing_need_test.go`): rule (c), accept refuses a card whose need has not landed.
- `make release-accept` runs all of the above and fails if any one is renamed, moved or red.
- `TestFirstAddWritesTheStreamBranchAndTrunk`, `TestAddRewritesADevBaseToTheStreamBranch`, `TestAMainOnlyRepositoryTakesMainAsTrunk`, `TestAClearedSprintMakesTheSameStreamBranch`, `TestLandingFlowIsOneSentence` (`internal/sprint/stream_branch_test.go`) and `TestANewStreamLandsOnItsOwnBranchWithNoSetting` (functional, `cmd/nova-sprint/stream_branch_functional_test.go`): a new stream writes its branch `sprint/<stream>` and trunk on its first add, a card cut on dev or main is re-cut onto the branch with one NOTE quoting the landing flow, a cleared sprint builds the same with no setting, and a real landing with defaults only puts both heads on origin's `sprint/s` while dev is unchanged.

## A finish-PR card starts from the PR's own head

A card cut to finish an open pull request stages from the PR's head, not from its base: the
head rides the brief's machine-read `CARRY:` line (the line `brief --widen` writes), and a
brief that names a PR — a `PR:` line or THE TASK's "Start from the PR's branch ..." — and
carries no such line is refused at add with the fix named (`LINT DRIFT ... check=pr-carry`).
The base is still merged in by the worker, never replaced.

- Unit, `pkg/swarm/prcarry_test.go`: `TestLintBriefRefusesAFinishPRCardWithNoCarry`: an
  uncarried finish-PR brief (a `PR:` line, or the PR named only in THE TASK) draws the
  `pr-carry` finding and the remedy names the `CARRY:` line to add; the same brief with the
  line passes, and a card that names no PR is not held.
- Unit, `internal/sprint/packet_basehead_test.go`:
  `TestAFinishPRPacketStagesFromTheCarriedHead`: a first attempt of a card whose brief carries
  the line stages from the PR head, and a pushed earlier attempt still wins over a stale
  carry.
- Functional (`-tags functional`), `cmd/nova-sprint/finish_pr_carry_functional_test.go`:
  `TestAFinishPRCardStagesFromThePRHeadOnAStore`, on a private redis-server with explicit
  `--redis` and a scrubbed environment: a finish-PR card whose brief carries
  `CARRY: head=<pr head> branch=<pr branch>` is admitted and dealt, the packet names the PR
  head as the base the slot stages from, and a commit built on that staged head has the PR
  head as an ancestor.

## A judgment its card has moved past closes itself (v1.2.14)

A judgment that outlives the state it was raised for closes itself at the next tick,
answered by the card's move, and can be acked with a reason because it holds no card that
still needs its decision (docs/SPEC-SPRINT.md section 8, "A judgment retires with its card";
docs/fixes.sexp, a-judgment-outlived-by-its-card-closes-itself).

- Unit, `internal/sprint/judgment_moves_on_test.go`:
  `TestJudgmentClosesWhenItsCardMovesOn`: a card ends attempt 1 on a brief defect (the
  judgment carries `Attempt: 1`), ack is refused while the card is still that card, the
  coordinator re-briefs it in place, and the next `TickCheck` closes the judgment with one
  decided note, `answered by the card's move: s1-2 was re-briefed`, written by the machine
  and gone from the inbox; a second tick finds nothing more. `TestAJudgmentStaysWhenItsCardHasNotMovedOn`:
  a judgment naming no attempt, and one whose card is still at the attempt it was raised for,
  stay open. `TestAMovedOnJudgmentCanBeAcked`: the re-briefed card's judgment acked with a
  reason before the tick is closed, recorded as the card's move.

## A long PATHS-PROPOSED line reaches the seat whole

A worker's HOLD proposes the PATHS its card lacked on a `PATHS-PROPOSED:` line in its report;
`brief <id> --widen` reads that line and widens the card in place (docs/SPEC-SPRINT.md section
2, "recut-widen-r.w1"). A report long enough to be cut to a cap must never have that cut land
inside the line: a path cut mid-word names no file and the widen is refused, so the seat
widens by hand (the bug of 2026-10-12: two workers' lines reached the sprint cut as
`pkg/release/*_tes` and `internal/sprint/steps_tick.go, interna`). The friend finish drops a
capped paragraph's cut tail and carries the whole line once (`friendFinish`,
`cmd/nova-sprint/friendcards.go`), the member's `CarryProposed` (`pkg/member/member.go`) drops
any line already in the report and keeps the whole line even when it alone fills the 500-byte
report cut, and `PathsProposed` and `proposals` read the last `PATHS-PROPOSED` line, the whole
one a cut report carries at its end.

- Unit, `cmd/nova-sprint/brief_widen_test.go`:
  `TestWidenTakesEveryProposedPathOfALongLine`: a friend's HOLD whose report is one paragraph
  long enough that its cap lands inside a ten-path `PATHS-PROPOSED` line carries the line
  whole, and `brief --widen` widens all ten paths; the member's `CarryProposed` carries a line
  longer than the report cut whole.

## Tests that fail under load are made deterministic

The owner's standing rule: tests are bounded and never flaky; a test that fails
under load is the test's defect. These tests failed on a shared bench (hetzner2,
load average 30-36) and are now deterministic, each by its exact name:

- `TestLandCallerCancelDuringCombinedGateLeavesSecondBatchQueued`
  (`cmd/nova-sprint/landpass_test.go`): the caller's cancel is delivered at a
  synchronization point the test controls — the combined gate suspends in its
  `gateRan` seam until the test has canceled — so the cancel always arrives
  inside the gate and never races wall time, however loaded the bench.
- `TestTickDrainAndDealUnderTheGateAtLoad`
  (`internal/sprint/deal_load_bench_test.go`): the deal's sub-second gate is
  held on the CI path by counts (the deal parses each ready brief exactly once;
  the drain's moves are bounded by the deal's queued output), never by a
  wall-clock bound on a shared bench. The wall itself is measured only by
  `BenchmarkTickDealAtLoad` and `BenchmarkTickDrainAtLoad`, outside the gate.
- `TestTheIdleAlarmNamesTheRootsOnceAnEpisode`
  (`internal/sprint/store/idle_test.go`): it was seen red on a hetzner2 base
  run; its dependency was the stale end-note rule it pinned (every end of an
  idle episode was a working-again note), already fixed by b70990f6 (`idleEnd`:
  only the fleet working at half its width again clears; otherwise the
  episode-ended note). No production change was owed; the inert sort tie-break
  added for it was dropped, and the test passes 50/50 under load.
- `TestCRTwoRunLoopsAtOnce`
  (`internal/sprint/store/machine_twoloops_test.go`): it failed 2 of 8 on clean
  main with "the sprint is busy: other operations kept the fence moving". The
  real cause was the outside store's no-op `Sleep`: it made the store's
  documented fence backoff (the wait between its `FenceTries`) never yield, so
  an outside verb's fenced read spent all its tries inside one tick loop's fence
  hold and gave up. The test now keeps the store's real `Sleep`, so the designed
  backoff spaces the tries as a run loop's verb has in production. The test's
  outer retry of `FenceBusyError` (`busyTries`) is the last-resort bound the
  test exists to assert; it is correct because, with the real backoff, a verb
  reaches it only if a fence stayed busy past the store's own documented
  `RetryBudget` — the starvation the test pins — and its failure names the
  losing verb and the store's own read count.
- The shared helper `setPrimary` (`internal/sprint/store/flash_first_test.go`)
  read `h.snap()`, whose `WithQueue` overlay reads the work table a revision
  ahead while the queue holds a change, so a write guarded on it was refused
  ("mem: table revision mismatch"); it now guards on the unoverlaid table
  (`setPrimaryOnTable`, `internal/sprint/store/no_parked_test.go`), so a test
  may set a primary after a finish and a tick.

The card's owed step — "run the package 10x under load and name the
`cmd/nova-sprint` test that failed once in #110's gate" — was run as
`go test -count=10 ./cmd/nova-sprint` under load. The tests that failed are the
ones that shell out to `git`: `TestTheLanderLandsTheBaseFixFirst`,
`TestLandMergesSameReleaseBlockEntries`,
`TestLandReviewReusedOperationCannotClaimAnotherBatchLanded`,
`TestServerReviewLandingAllowsATickAndCoordinatorWriteBeforePush` and
`TestLandReportsItsCardsWhenAnotherIsQueuedAheadUnderThePush`. Their failures
are the bench's process table, not the tests' logic: every one is
`fork/exec .../shim/git: resource temporarily unavailable` / `Cannot fork` from
a `git` call, except `TestLandReportsItsCardsWhenAnotherIsQueuedAheadUnderThePush`,
which fails its own assertion — `LAND REFUSED ... git ls-tree: fork/exec ...: resource temporarily unavailable` — because the same exhaustion hit its
`git ls-tree`. `TestTheTwinSocketFitsAUnixSocketPath` also failed, and it is
environmental: this sandbox's `TMPDIR` exceeds the 100-byte Unix-socket limit
and its `/tmp` fallback is denied. The test named for #110's gate is therefore
`TestLandReportsItsCardsWhenAnotherIsQueuedAheadUnderThePush`, the one whose own
assertion a loaded bench turns red by starving its `git` call of a process.


## A friend who stops beating is down and her cards come back

`TestAFriendWhoStopsBeatingIsDownAndHerCardsComeBack`
(`internal/sprint/friend_stops_beating_test.go`) starts a friend with a work
card working and two frontier reads working on her fleet row, then stops her
beat past the beat bound (`sprint.FriendBeatBound`). It pins the presence rule:
with her session's pong fresh, `sprint.FriendEvidence` still says down, "down
since <t>", because a friend is up only while her daemon beats within the beat
bound; a session pong alone never makes her up. It pins that the coordinator's
`return` (`sprint.Return`) works on her read, retiring it off her row. And it
pins the tick's reconcile (`sprint.LiveReturns`,
`LiveReturns.friendStoppedReturn`) hands both back to the deal: her work card
is retired off her row by `sprint.FriendReturn` and its primary is ready again
with its carry head kept, and her second read returns to the read deal
(`readLostUnit`). A stopped beat, not just a stopped session, means her cards
come back.
