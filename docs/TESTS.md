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
TABLES rows changed: work=1 readers=1 merge=0 fleet=0
TICK OK state=RUNNING idle=no moved=2 notes=0
0/1 0.0% -> ETA -  machine: running

$ nova-sprint read --as reader-a --begin --epoch 0
MOVED s1-1.r1.reader-a asked -> reading
READ OK moved=1 refused=0 notes=0 op=read-t35-1
0/1 0.0% -> ETA -  machine: running

$ nova-sprint read --as reader-a --ok --epoch 0
MOVED s1-1.r1.reader-a reading -> ok
READ OK moved=1 refused=0 notes=0 op=read-t36-1
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
MERGE OK moved=1 refused=0 notes=2 op=merge-t41-1
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
TABLES rows changed: work=1 readers=0 merge=0 fleet=0
TICK OK state=RUNNING idle=no moved=2 notes=0
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


### Release acceptance

`make release-accept` is the release gate of a point release. It replaces the ten-minute live
watch on the server after `server switch --rollback --window 30m` and the hand canary (the
owner, 2026-10-11: "I don't have time to wait 10 minutes for long checks each time you want to
test a new point release."). Each check the watch looked for is a test that pins the
behaviour, each on its own store (a fresh redis-server on a port of its own, or the in-memory
twin) and run in parallel; the set runs in under a minute on a bench and fails at two
(`-timeout 2m`, and the script fails a run whose wall time is over 120 s). `tools/release-accept.sh` holds the list of names; every name must run and
pass, so a test renamed or deleted fails the gate by its count instead of leaving the set.
`NOVA_SPRINT_BIN=<path>` names the build the canary runs (the release's binary); unset, the
tree's own is built once. It needs `redis-server` on PATH and fails, never skips, without one
(`NOVA_CI=1`). Measured on hetzner2 (32 cores), 2026-10-11, three runs: 10.5 s, 4.7 s and 4.8 s of
wall time, the binary's build included; 40 of 40 passed each time.

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
| 9. canary: on the release's binary, init, the member's beat, the ticks, and one member's take and finish ok of one card (the binary refuses a store whose function library is not its own) | `TestReleaseAcceptCanaryOneMemberFinishesOneCardOnTheBuild` |

After the gate a release may still run a live canary of one card on one member of the
adopted fleet; there is no timed watch.


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


