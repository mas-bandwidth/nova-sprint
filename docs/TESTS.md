# Tests: nova-sprint, and the tools folding into it

Moved from nova-tools docs/TESTS.md on 2026-10-04.

## nova-sprint

The first run needs no Redis. `--redis mem:<file>` loads an in-memory twin
from a file and saves it after each command. The twin is for learning and
tests; commands run one at a time. This transcript follows the card flow in
`nova-sprint help`, with `NOVA_SPRINT_REDIS=mem:sprint.twin` and
`NOVA_SPRINT_ACTOR=boss` set. It uses `finish` without `--head` and `merge`
to record a landing without git. The help's final `tick` moves the card to
landed, and `where` shows the sprint. Those two commands are omitted here
because they print clock-dependent times; `cmd/nova-sprint/twin_test.go`
runs them.

A card's move is queued until the next tick prints `MOVED drain`. A member
that comes up in one tick receives cards in the next.
`cmd/nova-sprint/firstrun_test.go` runs this transcript in the unit tier over
a twin file in a temporary directory. The twin counts its operation ids
(`t1`, `t2`), so every value reproduces without normalization. The functional
tests beside it (`cmd/nova-sprint/*_functional_test.go`) run against a real
store.

### First run

```text
$ nova-sprint init --readers reader-a,reader-b --members m1
INIT OK tables=work,readers,merge,fleet view=sprint readers=reader-a,reader-b
MOVED m1 added, down until it beats
FLEET-UP OK moved=1 refused=0 notes=0 op=fleet-release-t1-1
STOPPED
NOTE a twin beats every member at every verb: each member added is up after the next nova-sprint tick

$ nova-sprint add --stream s1 --count 1 --one
MOVED s1-1 -> ready stream=s1 score=1
ADD OK stream=s1 cards=1 before=- moved=1 refused=0 notes=0 op=add-t2-1
NOTE the cards have no brief, so a worker is handed no task with them; give each one before it is dealt, on a STOPPED machine: nova-sprint brief <id> --brief-file <path>
STOPPED  0/1 0.0%

$ nova-sprint start
START OK before=STOPPED after=RUNNING changed
nothing is ticking between commands in a twin: tick by hand: nova-sprint tick
0/1 0.0% -> ETA -  machine: running

$ nova-sprint tick
MOVED presence: m1 up
MOVED presence: status seen: m1 up
TABLES rows changed: work=0 readers=0 merge=0 fleet=1
TICK OK state=RUNNING idle=no moved=2 notes=2
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
  notes: none
  report it: nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --branch sprint/s1-1.w1.g1.e0 --head <commit> --report '<what you did>' [--failed]
TAKE OK moved=1 refused=0 notes=0 op=take-t31-1
0/1 0.0% -> ETA -  machine: running

$ nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --report done
MOVED s1-1.w1 working -> done ok; s1-1 working -> review
FINISH OK moved=1 refused=0 notes=1 op=finish-t32-1
0/1 0.0% -> ETA -  machine: running

$ nova-sprint tick
MOVED drain: s1-1.w1 working -> done ok; s1-1 working -> review (finish by m1)
MOVED ask: s1-1 asked of reader-a
MOVED deadlines: landing-stall watch begins: 1 review, 0 merging, no landing yet
TABLES rows changed: work=1 readers=1 merge=0 fleet=0
TICK OK state=RUNNING idle=no moved=3 notes=0
0/1 0.0% -> ETA -  machine: running

$ nova-sprint read --as reader-a --begin --epoch 0
MOVED s1-1.r1.reader-a asked -> reading
READ OK moved=1 refused=0 notes=0 op=read-t36-1
0/1 0.0% -> ETA -  machine: running

$ nova-sprint read --as reader-a --ok --epoch 0
MOVED s1-1.r1.reader-a reading -> ok
READ OK moved=1 refused=0 notes=0 op=read-t37-1
0/1 0.0% -> ETA -  machine: running

$ nova-sprint tick
MOVED drain: s1-1 asked of reader-a (tick ask by machine); s1-1.r1.reader-a reading -> ok (read by reader-a)
MOVED accept: s1-1 review -> merging queued (ok from reader-a)
MOVED verdicts: s1-1.w1 finished -> ok (the readers' verdict)
TABLES rows changed: work=1 readers=0 merge=1 fleet=1
TICK OK state=RUNNING idle=no moved=3 notes=2
0/1 0.0% -> ETA -  machine: running

$ nova-sprint merge --stream s1 --batch 1
MOVED s1-1 merging -> landed
MERGE OK moved=1 refused=0 notes=3 op=merge-t42-1
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
MOVED m1 added, down until it beats
FLEET-UP OK moved=1 refused=0 notes=0 op=fleet-release-t1-1
STOPPED
NOTE a twin beats every member at every verb: each member added is up after the next nova-sprint tick

$ nova-sprint add --stream s1 --count 2
MOVED s1-1 -> ready stream=s1 score=1
MOVED s1-2 -> ready stream=s1 score=2
ADD OK stream=s1 cards=2 before=- moved=2 refused=0 notes=0 op=add-t2-1
NOTE the cards have no brief, so a worker is handed no task with them; give each one before it is dealt, on a STOPPED machine: nova-sprint brief <id> --brief-file <path>
STOPPED  0/2 0.0%

$ nova-sprint start
START OK before=STOPPED after=RUNNING changed
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
- `TestTheTierLineIsReadOrRefusedByOneRuleInBothForms` (`internal/card/tier_line_test.go`), `TestTheTierOnLineOneIsReadInBothForms`, `TestAddStoresTheTierLineOneNamesOrItsStreamsDefault`, `TestAHeavyCardsFirstDealDrawsAHeavyRoute`, `TestABriefWhoseLineOneNamesAStrongerTierRaisesItsTierNow` (`internal/sprint/tier_line_test.go`) and `TestAddPrintsTheTierEachCardIsStoredWith`, `TestBriefPrintsTheTierTheCardIsDealtOn` (`cmd/nova-sprint/tier_line_test.go`): the tier line, read or refused by one rule in both forms, a stream's default, printed by add and brief, a stronger re-brief raising tier_now, dealt on.
