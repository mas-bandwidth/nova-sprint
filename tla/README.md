# Checking the coordination rules

Several AIs finishing, retrying, and handing work to one another can expose
ordering bugs that a happy-path example will miss. These TLA+ models explore
small, explicit versions of the sprint's state machines and look for broken
invariants or work that can stop making progress.

You do not need TLA+ to use nova-sprint. This directory is for contributors
changing scheduling, handoffs, recovery, or the work-tree design.

## Choose a model

| Model | What it explores |
|---|---|
| [SprintEvents](README-SprintEvents.md) | Event-driven scheduling, leases, deadlines, notifications, and interruptions. |
| `SprintTables` | The card lifecycle: six states, waiting ready working review merging landed, and the moves between them, with the fleet side, the reads and the merge and return. |
| [DirtyTick](README-DirtyTick.md) | How a tick drains queues and moves work between tables. |
| `DirtyTickRead` | Reader handoffs within the tick. |
| `ReadsByRoom`, `ReaderTiers`, `ReadCards` | Where a read is asked, on which tier, and how a read card is loaded. |
| `RouteIndex`, `RouteRest` | Route selection, its counters, and resting a route. |
| `NoRoom` | A member's no-room word (the disk floor) and its drain (its binary replaced), the deal and fleet verbs that give such a member nothing, the return of what a draining member was dealt and has not taken, and the ask that asks a held or draining machine's reader nothing. |
| `RetireAbsent` | The tick retires a fleet row whose machine record is gone from the inventory (a rename), drains it while it still holds cards, and retires nothing on a failed config read. |
| `Level`, `WhoPreference` | Distribution of queued work across available capacity, and the rebalance. |
| `Land`, `LandPass`, `LatePush` | The landing protocol, the land pass over streams, and a late push. |
| `CardISA` (over `CardMachine`) | The card instruction set; `CardMachine` is the model of nova-tools' deleted Lua card machine, kept for `CardISA` which `EXTENDS` it. |
| `Holds` | A held card, a sentinel and its release, and a stream's hold, stop, resume and reopen. |
| `HoldCause` | A fleet member's hold with its cause (a judgment, a doctor check, the machine's idle-reap judgment) ended by the tick in the step that sees the cause end, a cause-less hold reported once every period, and the attribution of idle reaps: two or more members on one route rest the route and hold no member. Witnesses: `holdleaks`, `silent`, `blamemember`. |
| `CoordinatorPass`, `CoordinatorWake` | The coordinator's pass and its wake. |
| `SprintRules`, `StallLadder` | The rules that answer failures, and a stalled friend's ladder. |
| `SeatHealth`, `ServerLanes` | The seat's generation and a friend's health; the server's lanes. |
| [SeatHook](README-SeatHook.md) | Durable scoped ACKs, reconnect/restart replay, held-to-due delivery, cumulative monotone push IDs and failed-write rollback, plus the coordinator's hook: the server's subscription, its challenge and cadence (asked once a connection, none after), the record's Live from the last act plus `HookSilentBound`, renewed by acts and not a re-challenge (the keepalive proves the transport alive and nothing more), the pushes and their acknowledgement, a drop, unhook, the next server's start rewriting a dead server's record gone, and the gate every coordinator command passes. |
| `LiveRuns` | Working means a live run: the beat's live set, the reconcile while RUNNING and STOPPED, the STOP debt's receipt, held reports. |
| `Timer`, `LoadCache`, `StatsReset` | Timers, the load cache, and a stats reset. |
| `AttemptsEpochs` | The epoch a clear advances and a card's attempt, generation and redeals: the restore marker, the per-epoch tables, a late writer refused, a second concurrent clear, the deal and redeal, the stale-generation guard, and the branch per launch. |
| `StopReturn` | STOP's captured owner leases, the owner's stop-return, its settle off the debt, START, and clear's STOP followed by its live work/read-lease check. `MCStopReturnClear` checks clear reachability and safety; `BrokenClearBlocked` drops the live-lease guard. |
| `FriendReadyTake`, `FriendRedealLate` | Historical friend ready-take and redeal clocks, moved from nova-tools dev `2314d437f`; current start receipts and deadline routing are outside these preserved models (partial coverage). |
| `MachineReady` | The machine's READY state (a clear is its stop and its advance, a start may land between): a clear (and init) leaves READY, start moves READY or STOPPED to RUNNING, stop moves RUNNING to STOPPED and is refused on READY, READY raises no `machine:stopped` judgment while STOPPED keeps it, and READY is read off the record (not RUNNING, not started since the clear). Invariant RanIsNotReady: a machine that ran in the epoch is STOPPED, never READY. Witnesses: `clearstopped`, `readyjudges`, `stopready`, `nostopstamp`. |
| `StopFence` | The distinct nova-tools STOP/START fence model, renamed on arrival: RunSeq, stale tick stop/part, launch/report and row-delete fences. Newer debt settlement and held-report exemptions are outside it. |
| `WorkImport` | Issue import and the specified work-tree modes. |
| `RoadmapEntry` | An entry of a repository's roadmap and fixes files under nova-work roadmap: add, remove, pull, note, done, and Save's four writes (destination data, destination page, source data, source page), with scheduled notes whose `:release` is unchecked. |
| `GateRing` | The land stage's gate bench ring: the hashed start slot, the per-pass skip record, a refusal stepping to the next slot, and the local fallback refused as a remote-bench unavailability. |
| `ProviderFunds` (over `RouteRest`) | A provider's refusal rest and its end mark, the low, funds and key judgments, the all-out stop with the friend exception, and the coordinator's START. |

The `.tla` files define the models. `MC*.tla` and `.cfg` files provide bounded
instances. [CASES.tsv](CASES.tsv) lists the gated cases; [RUNS.tsv](RUNS.tsv)
records results, and [COVERAGE.tsv](COVERAGE.tsv) lists the sprint's state machines
with their code, their module and their status. The two longer guides explain the assumptions, findings,
and repairs for their models.

## Read a result in context

An invariant is a condition that should always hold. A liveness property
asks whether progress eventually happens under the model's stated fairness
assumptions. A **reversed witness** deliberately breaks a rule: its expected
counterexample checks that the property can catch that defect.

A recorded pass applies to the model, bounds, revision, and configuration in
that run. It does not prove that a live deployment has no bugs, that a model
provider is available, or that all possible fleet sizes were checked. Keep
counterexamples and measured results intact when improving their explanation.

## Run checks on a Linux bench

TLC runs only on a bench. The runner is `tools/tlacheck` in this tree: build it
with `go build -o /tmp/tlacheck ./tools/tlacheck`, or run it with
`go run ./tools/tlacheck`. The Makefile wraps it:

- `make tlc TLC_JAR=<jar> TLC_OUT=<dir> TLC_GROUP=<group>` runs one group,
  writing its log and `RUNS.tsv` under `TLC_OUT`;
- `make tlc-full TLC_BUDGET=<seconds> ...` runs a manual group, with `--manual`;
- `make tlc-groups` prints the groups that hold a case with no current record;
- `make tlc-test` runs the runner's own tests.

`go run ./tools/tlacheck groups --root . --stale` names the groups a model edit
made stale, and `go run ./tools/tlacheck merge --root . --keep tla/RUNS.tsv --out tla/RUNS.tsv <runs>...` joins their records. Check the runner's help for
your version, and record new results against the actual model inputs you ran.

The same groups run on GitHub-hosted `ubuntu-latest` runners in
[`.github/workflows/tlc.yml`](../.github/workflows/tlc.yml) (nightly, on
`workflow_dispatch`, and on a pull request touching `tla/`, `pkg/tlc/` or
`tools/tlacheck/`), which downloads the pinned tlaplus v1.7.4 release and
checks it against [`tla2tools.sha256`](tla2tools.sha256) before
`make tlc TLC_GROUP=<group> TLC_BUDGET=110`.

Return to [Contributing](../CONTRIBUTING.md) for the ordinary build and test
checks, or [the documentation index](../docs/README.md) for the user guides.

## LandPass stream routing and certification

`MCLandPassStreamFlow.cfg` checks `StreamFlowFairSpec`, a separate abstraction
alongside the existing LandPass gate/pass specifications. This is **not** a
composition or refinement proof between them. The old batch-set variable is
`batchBase`; `base[c]` is the card destination. Legacy specifications freeze the
flow state and keep their original fairness over `legacyVars`; the flow freezes
the legacy pass with a green oracle. All 13 historical cases retain their expected
outcomes and independently check the old gate and refusal rules.

The flow bound is **two streams and three cards**, represented by `<<stream,
head>>`: s1 has heads 1 and 2, s2 has head 1. A batch contains one card and each
stream lands its oldest unfinished head first. A second batch may land on the
stream while its earlier snapshot is being certified. `sbranch[s]` contains its
landed cards, `integrated[s]` the last snapshot merged into trunk, and `trunk`
records `<<"merge", s>>` or `<<"card", batch>>`. `inflight` and `cert` distinguish
certification admission from branch readiness. `moveFree` retains the **pre-state**
admission guard at every append, so finishing certification cannot erase overlap.

Initially branches are empty and legacy stream destinations name trunk. `AddCard`
inherits its configured destination before or after migration. `AdvanceCard`
visits waiting, ready, working, review and merging; landed is the sixth real
lifecycle column, while absent is model-only. `landProtected` independently ranges
over all subsets of streams. `CardOntoTrunk` requires the actual stream mark:
`BugCardOntoDev` restores the historical **marked** exception; the independent
`BugUnmarkedLanding` mutation removes the mark guard. An unmarked card is refused
before git in the staged Go, rather than somehow landing because its BASE is stale.

`SetBase(s, s)` migrates once to its stream branch. `BaseRewritePolicy` exposes two
policies: the requested `all-unlanded` rewrite, and `preserve-cut-head` selected by
`Broken = "baseskipsreview"`, which preserves working/review card bases in this
bounded abstraction and still repoints merge-queued cards. The Go selector is
broader: among eligible unlanded cards, it keeps every card with `attempt > 0`
unless the card is in the merge queue's `Queued` cell.
**This is an unresolved implementation policy choice.**
The current Go intentionally preserves already-dealt heads on their old base;
this model does not assert that rewriting an active head is safe or that the Go
selector is simply defective. The stronger requested invariant
`NoUnlandedCardBasesOnTrunk` says every existing unlanded card of a migrated stream
names that stream, and the preserve policy violates it immediately at migration,
without a protected mark or any landing. Settling that discrepancy may require
explicit recutting/restaging; no Go repair is made or assumed here.

`BriefEdit(c, proposedBase)` is a real finite event: one attempted edit per card,
with an independently chosen own-stream or trunk BASE. Normally it preserves
routing; `BugBriefBase` writes that input to `base[c]` and independently violates
the same postmigration invariant. The control also checks the action properties
`SetBaseReachesEveryUnlanded` and `BriefEditKeepsBase`.

An empty branch records the trunk revision it follows. A sibling merge can advance
trunk while that branch is empty. `FollowTrunk(s)` fast-forwards it separately;
`StreamLand(s)` must wait for that revision before the **first** landing.
`EmptyStreamFollowsTrunk` compares the actual cut revision and trunk revision
recorded at that landing. `BugEmptyStale` drops the first-land guard and produces a
real stale landing after a sibling merge. The model permits temporary empty-branch
lag before landing; it does not assert atomic updates to every branch on each move.

`StreamMerge(s)` admits a snapshot only when it is ahead, no merge is inflight,
and cert is green or trunk has never moved. `CertDone` may answer green **or red**;
red blocks new admission. `Fix` is explicitly a **successful repair and
recertification of the same trunk revision**, without another trunk move. This
is the recovery assumption, not a proof that arbitrary broken code can be fixed.
The weakly fair specification schedules every card's add/advance, every stream's
migration/follow/landing/merge, certification completion, and successful Fix.
It checks both `LandedEventuallyIntegrated` and the stronger
`AllFlowWorkCompletes`, so a run where nothing lands is not a vacuous progress pass.
Safety invariants are checked over the full reachable graph and do not depend on
these fairness assumptions, even though the same control also checks liveness.

A mere fair retry is insufficient: `BugRedRetry` replaces successful Fix with
red-to-running retry, allowing an adversarial running/red cycle forever. Its
liveness witness leaves real landed cards unintegrated. `BugNoRepair` removes Fix
and exposes an incomplete terminal state. All new cases use deadlock policy
`check`, including the control. `Quiesce` is the only explicit stutter and is
available only when every card is landed and integrated, cert green, and no merge
inflight. Brief edits are one-shot state changes, so cannot conceal a wedge.
`NoPartialCompletionWedge` additionally gives a named invariant counterexample for
the missing-repair case. No terminal state is silently waived.

`TrunkMovesOnlyByStreamMerge` and `OneStreamMergeInflightPerTrunk` are the requested
names; `DevMovesOnlyByStreamMerge` and `StreamMergesSerial` remain aliases for old
callers. `CardLandsOnItsStream` is retained. Direct pushes during running
certification set cert cancelled; serialization rejects both cancellation and
admission while a previous certification is inflight. **GitHub's merge queue
already serializes actual Git merges onto trunk.** The overlap witness concerns
coordinated admission/certification and cancellation, not simultaneous Git writes.

The eight negative cases distinguish historical behavior, a policy discrepancy,
and deliberate mutations. State numbers below are from the revised measured run:

| Configuration suffix | Selector | Expected violation / first bad state |
|---|---|---|
| `BrokenCardOntoDev` | `cardontodev` | Historical marked route: `DevMovesOnlyByStreamMerge`, state 7 with `landProtected = {s1}`. |
| `BrokenReviewKeepsBase` | `baseskipsreview` | Preserve-cut policy versus requested stronger rule: `NoUnlandedCardBasesOnTrunk`, state 5, working card still on trunk immediately after migration. |
| `BrokenBriefBase` | `briefbase` | Mutated brief write: `NoUnlandedCardBasesOnTrunk`, state 4, migrated waiting card edited back to trunk. |
| `BrokenMergeBeforeCert` | `mergebeforecert` | Mutated admission guard: `StreamMergesSerial`, state 16, s1's second snapshot admitted while its first is running, `moveFree = <<TRUE, FALSE>>`. Other-stream overlap is enabled by the same mutation. |
| `BrokenEmptyStale` | `emptystale` | Mutated first-land guard: `EmptyStreamFollowsTrunk`, state 16, s2 first lands at cut 0 while trunk is 1 after s1's merge. |
| `BrokenUnmarkedLanding` | `unmarkedlanding` | Mutated mark guard: `ProtectedLandingRequiresMark`, state 7, direct trunk push with `landProtected = {}`. |
| `BrokenNoRepair` | `norepair` | Missing recovery: `NoPartialCompletionWedge`, state 23, all cards landed, s2 unintegrated, cert red, no enabled step. |
| `BrokenRedRetry` | `redretry` | Retry is not successful repair: `LandedEventuallyIntegrated`, cycle 27-28, both s1 cards landed but never integrated while s2's trunk revision retries red forever. |

Hand mapping against staged Go `c3b0b1b142cddfeec489582a493cb2ff0a0022d9`:

- **Protected routing:** `cmd/nova-sprint/landpass.go:299-302` reads the card's
  BASE and line 323 calls `ProtectedLandWhy`; `internal/sprint/protected.go:48-50`
  permits a matching `land_protected` mark. `cmd/nova-sprint/land.go:756-759`
  (`placeWhy`) returns an unmarked refusal before git. This is the distinction
  between the historical marked witness and the deliberately faulty unmarked one.
- **Migration:** `internal/sprint/streams.go:407-421` selects never-dealt or
  merge-queued cards; `cmd/nova-sprint/land.go:699` (`landQueue`) identifies the
  queued merge candidates. Every eligible unlanded card with `attempt > 0` that
  is not merge-queued is kept, regardless of its work column; `deal` increments
  the attempt at `internal/sprint/steps_work.go:1247`.
  `streams.go:433-436` only constructs candidates. The actual planned
  write is `internal/sprint/settings.go:691-702`, using `briefOnBase` at
  `streams.go:350-356`. The old merging-card trace and old landing-only property
  were incorrect for this selector; the revised witness fails before landing.
- **Brief editing:** `internal/sprint/steps_edit.go:168-187` admits a permitted
  brief edit and writes `b.Brief`; landpass.go:299-302 later reads its BASE. The
  mutant makes that routing change explicit; the desired design instead preserves
  the routing field independently of prose.
- **Admission/certification:** `cmd/nova-sprint/promote.go:354-370` watches an
  existing pending promotion, lines 644-667 wait for checks before enqueue, and
  lines 821-840 read one clone's pending state. They are local guards, not the
  global per-trunk admission rule. `sprint.NextStreamMerge` is absent at this
  staged head. `promote.go:597-620` and `:728-746` identify red runs and judgments;
  successful Fix is the explicit environmental recovery obligation beyond them.
- **Empty branches:** `landpass.go:362-363` uses the card's base to construct a
  landing job. Automatic empty-stream following is absent from that staged path;
  FollowTrunk and its first-land guard specify the sibling fix's intended behavior.
  Revision counters abstract ancestry, not a full Git object graph or E12 check.

Rowan's independent cold read rejected the earlier readiness claim: it found the
missing mark, incorrect merging-selector mapping, coupled skip/landing witness,
missing empty-branch behavior and progress, and permissive terminal handling. This
revision responds to those findings; the earlier run evidence remains historical.
Rowan accepted the revised design, including the unresolved working-head policy,
on 2026-10-12 at 14:00:30Z. Design acceptance is not final implementation approval.
His final cold read of `5c3196d6` at 14:28:33Z approved the models-only revision,
independently reproducing all 22 results and checking the eight negative witnesses
and the necessity of each weak-fairness conjunct. This records model review, not
product implementation or landing.

Run `make tlc TLC_GROUP=landpass TLC_JAR=<pinned-jar> TLC_OUT=<new-directory>` on
Linux, then use `tools/tlacheck merge --keep` as described above. The group contains
13 unchanged historical cases, the revised control, and eight negatives (22 total).
A witness's runner PASS means TLC reported its declared violation. This bounded
model omits crash recovery, dynamic stream membership, arbitrary batch sizes and
full Git ancestry; it makes no claim that the staged Go implements every requested
policy or that certification is guaranteed green without successful repair.

## The models in detail

These rows and the `Level` section moved here from nova-tools' `tla/README.md` at v1.2.3, with the
modules (docs/SPEC-SPRINT.md and the code are the references they cite). Several of these models are stale
against the code: they were written before the owner's ruling of 2026-10-01 that the sprint is changed and
run without a model change each time; [COVERAGE.tsv](COVERAGE.tsv) says which.

| Module | Instance | What it is |
|---|---|---|
| `CardMachine.tla` | `MCCardMachineSmall`, `MCCardMachineSmallLive`, `MCCardMachineBrokenReadsInReview` | the model of nova-tools' deleted Lua card machine (`02_card_move.lua`; no `.lua` of that name is in either repository, and nova-tools `internal/ci/testdata/deleted-tests.txt` records its deletion), kept because `CardISA` `EXTENDS` it and reuses its actions. It is not the machine `lifecycle.go` runs (that is SprintTables). The small instance is Cards `{c1}` with no Deps, Consumers `{k1,k2}` one slot each, MaxCopies 2, HeadBound(1): `MCCardMachineSmall` checks the nine invariants alone, `MCCardMachineSmallLive` adds `ReadyIsOneWay`, `CopiesEnd` and `Progress`, and `MCCardMachineBrokenReadsInReview` removes `DealRead`'s `where = "review"` guard and must violate `ReadsInReview`. The original two-card instance is in `cardmachine-bench/` and does not finish |
| `CardISA.tla` | `MCCardISA*`, `MCCardISALive` | the card instruction set (docs/SPEC-ISA.md, layer 1): a card is one instruction and its kind says what it carries. It `EXTENDS CardMachine` and reuses its actions through one frame; it adds only what the kinds add: the one `wait` kind, whose one guard `OperandHolds(c)` is over an operand that is a card id, `release` or `external` (today's hold, sentinel and wave are one action's data, where CardMachine's `Release` is one path and the code it was read from holds three), and the per-kind results (`think`/`script`/`merge` a head and a verdict, `verify` a verdict, `wait` none). A non-wait card is resolved by `Wait(c)` (waiting -> ready) and a wait card is landed by its release (`IsaLandEvent`, a waiting wait card with released) and never dealt, so a card lands only by the table: from merging (`IsaLand`), or from waiting a wait card. It replaces `CardMachine.Release`, `DealWork`, `Land` and `LandEvent` (and its `Verdict` and `EndWorkDone` drop/done-already-to-landed branches, by `IsaVerdict` and `IsaEndWorkDone`), named in its header. It proves `NoCardRetiresTwice` (no card lands twice), `RetiredHeadOnBase` (a landed card's head is an ancestor of its base), `LandsOnlyByTable` (a card lands from merging, or from waiting a wait card) and `WaitDispatchesOnlyWhenOperandHolds` (a wait card lands only when its operand holds), with a reversed witness each (`MCCardISABrokenSecondLand`, `MCCardISABrokenOtherBase`, `MCCardISABrokenLandAnywhere`, `MCCardISABrokenWaitFirst`), and, on the small `MCCardISALive`, that a wait whose operand comes to hold is landed. The card model's stale note above does not cover this module; it is written to `docs/SPEC-ISA.md` (landed on the base at `817177be66d4a2764bd228c223eb485a8ef7c164`), and `LandTwoLevel.tla` (the `merge-tree-proof` branch, head `40e9dc34ea18f7362f3b1b914ac204fe48263ae7`) is cited by branch and head, not copied: that branch has not landed |
| `SeatHealth.tla` | `MCSeatHealth*` | nova-sprint's seat generation and a friend's health (docs/SPEC-SPRINT.md section 1, "A friend's health"; internal/sprint seat.go, friend_health.go): the seat (holder, generation) moved by handover, one friend observed by the coordinator's daemon under the seat it read (observations in flight, delivered in any order), her own beat, the coordinator's hold, and the table's word derived at every read; two holders, a clock of six, three observations, a proof fresh for three; it proves the table shows up, held or down and nothing else, held is the hold alone, up only on an up word under the current seat with a fresh proof (or, never observed, a fresh beat), an old seat's proof never up under a new seat (A->B->A), an accepted observation at the seat's generation, the row's proof never going back, an observed friend never up by her beat, the row's proof never dated after the server's clock (a daemon's proof may be dated at any time), and the generation stepping with every handover; six reversed witnesses (any generation writes, an older proof overwrites, the beat wins, a proof never ages, a handover keeps the generation, a proof from the future writes) |
| `SprintTables.tla` | `MCSprintTables*` | the card lifecycle (docs/SPEC-SPRINT.md section 3; internal/sprint/lifecycle.go) and the fleet side and the reads of the work table (internal/sprint/refmodel/actions.go): a primary's six states, waiting ready working review merging landed, landed final, and the Moves table, one action per verb whose guard is Legal plus the plan's flags (releasing, inserting, removing), with needs, waivers and sentinels; off the table (a drop) is not a state; each member is up or down with a working primary's work card on its row (Start, Take, Finish, FleetDown, FleetUp), and each read card sits on one reader's row in a place none asked reading ok broken (Ask, AskAnother, ReadStart, Read, Accept); and the merge side: each primary belongs to a stream waiting merging stopped (with a cause) or landed, a primary in merge sits in a merge cell none queued merged stuck or returned, and Rework, Return, Redo, MergeGreen, MergeStop, MergeRefused, MergeRed, Resume, Rank and Drop move and stop it. It proves `TypeOK`, `NoNeedCycle`, `NoCardLostOrTwice`, `MovesOnlyByTable` (every change of cell is a Moves row, a drop from an open state, or a remove of a landed card), `ReadyToWaitingOnlyByInsert` (a primary moves ready -> waiting only by inserting a sentinel or its needs not landed), `WorkingToWaitingOnlyOnNeeds`, `NeedsWithdrawalWaits`, `LandedIsFinal`, `NeedsLandFirst` (a card leaves waiting for ready only with every need landed or waived), `WorkingOnAnUpMember`, `AcceptHeld`, `NeverAcceptBroken`, `NeverReadByItsWorker`, `StreamAfter` (a stream's state follows its merge cells and cause) and `RankRefusesLanded` (a landed primary is never ranked), with eight reversed witnesses: `MCSprintTablesBrokenInsert` (an add that moves ready -> waiting without inserting, `ReadyToWaitingOnlyByInsert` fails), `MCSprintTablesBrokenNeedless` (a resolve that drops the needs check, `NeedsLandFirst` fails), `MCSprintTablesBrokenWithdrawready` (a needs withdrawal run working -> ready, `NeedsWithdrawalWaits` fails), `MCSprintTablesBrokenDownkeeps` (a fleet down that keeps a working card on the member that went down, `WorkingOnAnUpMember` fails), `MCSprintTablesBrokenShortAccept` (`AcceptHeld` fails) and `MCSprintTablesBrokenAcceptBroken` (`NeverAcceptBroken` fails), `MCSprintTablesBrokenLandunmerged` (a card lands from review, `MovesOnlyByTable` fails) and `MCSprintTablesBrokenReturnlost` (a card returned from merging is dropped, `NoCardLostOrTwice` fails). The instance is split into the fleet and reads side (`MCSprintTables.cfg`) and the merge side (`MCSprintTablesMerge.cfg`). The refmodel's pending-operation fence (D1: Free) is left out, and crash and repair are owed by the next card |
| `SprintEvents.tla` | `MCSprintEvents*` | the upper layers of nova-sprint (layers 3 to 8 of the event-driven tick design, standing on the table layer's guarantees): the log turned into keys, due times in running time, the rules with plan and apply separate, the tick, the verbs; 29 reversed witness configurations (W1 to W27, W7 split in two, and W5's reach half, which breaks reach and unreach and not the verb), 27 of which fail with the property their row names while W1 and W25 pass because the design holds there in depth, each with its unbroken control; goal cases that fail for the seventeen holes the model found (thirteen in the design as written, four in repairs errata 3 decided, three of them decided in its amendment 2 and repaired here; H12 and H15 on the bench), each decided repair with a case that passes. The larger runs are in `sprintevents-bench/`, outside `CASES.tsv`. `README-SprintEvents.md` says the rest, including what is not modelled |
| `SprintRules.tla` | `MCSprintRules*` | the machine feeding itself (docs/SPEC-SPRINT.md section 2, a card replaced by its twin; section 8, answered by rule; section 14, the fleet is idle; internal/sprint twins.go, rules.go, rules_read.go, idle.go, cmd/nova-sprint landgo.go), ten small machines each checked alone by its `Part`: **twins** (an old card, its twin, two waiting cards that need it; add --replaces re-points every waiting need in one step and drops the old card raising no blocked judgment, relink repairs a drop and an add made apart): `TwinsInherit` (no waiting card needs a replaced id), `NoBlockedForReplaced`, `NoDanglingNeed` (a waiting card that needs a dropped card has a judgment that says so); **rules** (one card's attempts under the failed and bound rules, tiers flash, pro, heavy, then a friend's card where a mind answers, escalating to the first served tier of the ladder above and skipping tiers no route serves): `RuleAttemptsBounded` (at most Cap attempts a tier on three tiers), `LadderClimbs` (never a lower tier), `EscalatesOnlyToServed` (a rule climbs only to a served tier, or a friend's card), `Answered` (every failure or bound is answered, under fairness of the rules and the mind); **late** (one late work card, its holder's progress stamps as an outside event, the one wait a generation, the hold of a card whose holder never stamped, the return and redeal of one that stamped and went silent, once its holder has had its own whole deadline): `WaitOnce`, `NeverStampedNeverReturned` (a card whose holder never stamps is never returned by the late rule), `HeldToItsOwnDeadline` (a stamped card is not returned before its holder's own deadline, or its one wait is spent), `ProgressIsStamped`, and the reachability witness `MCSprintRulesLateReachReturn` (`NeverReturned` fails: a card is still returned); **reads** (one card's attempts under a reader's broken reads: the finding the fix of a rework, a finding naming a file outside PATHS the card's PATHS widened in place, never a twin, its attempts from the first and never stopped at the brief's bound, any other card at its brief's bound a mind's): `ReadAnswersBounded`, `WidensWiden` (every widening widens PATHS by a file), `OutsideNeverBound` (a finding outside PATHS is never stopped at the bound under the widen cap), `WidensBounded` (at most MaxWidens widenings a card), `ReadAnswered`; **gate** (one base commit gated again and again): `BaseStopsOnThird`; **idle** (an idle fleet each tick, the window, the episode): `AlarmOncePerEpisode`, `ClearFollowsAlarm`; **answers** (one card's attempts under the broken-read, harness-fault and late-report answers: a broken read with a finding and a harness fault or a HOLD with findings reworked with it, a broken read with no finding and a failure no class names a mind's, a deadline's failure racing the worker's late LAND or HOLD): `AnswerAttemptsBounded`, `ReworkKeepsCount` (every rework is one attempt more), `ReworkCarriesHead` (each attempt starts from the head the one before pushed), `ReworkHasAFix`, `LateReportFinishes` (a report for an attempt the deadline failed, before a later attempt started, finishes it), `HarnessFaultBounded` (a harness fault is reworked at most once a card), `HarnessFleetHeld` (the same harness-fault class on `RuleSameFailureCards` cards is the fleet's one grouped judgment, which holds every card and never redeals it), `AnAnswered`, `PartsAgree` (the answers Part's verdict on a read-broken finding equals the reads Part's), with eight reversed witnesses of their own (a rework with no finding, a rework not counted, a rework from the base, a stale refusal of the late report, a harness rework past the bound, a read-broken finding at the bound left to a mind, a harness fault reworked a third time, a card the fleet's grouped judgment holds redealt). **conflict** (one card whose head the lander refused, rules.go ruleConflict, ruleRedo, ruleBrief: the conflict rule returns it reset to flash, the redo reworks it on the tip one attempt more, the same way twice a brief defect held for a mind): `ConflictResetsToFlash` and `OnlyConflictLowers` (`LadderClimbs` restated: no rule but the conflict rule lowers a tier), with its reversed witness `MCSprintRulesConflictBrokenConflictKeeps` (the return leaves the tier a failure raised it to, `ConflictResetsToFlash` fails). Sixteen reversed witnesses, each breaking its property: a replace that does not re-point (`TwinsInherit`), one that drops in silence (`NoDanglingNeed`), a failed rule with no cap (`RuleAttemptsBounded`), a bound rule that lowers the tier (`LadderClimbs`), a rule that climbs to the next tier whether a route serves it or not (`EscalatesOnlyToServed`), a late rule that waits whenever there is progress (`WaitOnce`), a late rule that returns a card with no progress whether its holder stamped or not (`NeverStampedNeverReturned`), a late rule that returns a stamped card before its holder's own deadline (`HeldToItsOwnDeadline`), a base gate that stops on its first failure (`BaseStopsOnThird`), an idle alarm every tick of an episode (`AlarmOncePerEpisode`), a read-broken rule that reworks at the brief's bound (`ReadAnswersBounded`), one that widens on a finding inside PATHS (`WidensWiden`), a read that stops a finding outside PATHS at the bound (`OutsideNeverBound`), a rule that widens past the cap (`WidensBounded`), a read-broken answer that leaves a non-empty finding at the bound to a mind (`PartsAgree`), a harness rule that redeals a third time (`HarnessFaultBounded`), a harness rule that redeals a card the fleet's grouped harness-fault judgment holds (`HarnessFleetHeld`) |
| `CoordinatorPass.tla` | `MCCoordinatorPass*` | the coordinator's pass (docs/SPEC-SPRINT.md section 8, "The coordinator's pass"; internal/sprint coordinator_pass.go): one condition on one subject (a friend's session deaf, a friend holding working cards that finishes none, judgments late on the coordinator) coming and going as an outside event, the tick's pass raising its judgment once an episode, raising it again in place every Every steps of running time while it holds, the coordinator's ack quieting it, the wait that holds it until a review time and raises it again when the time is reached and the condition still holds, and the close when it stops holding: `OneJudgmentAnEpisode`, `PushedEveryWindow`, `ClosedWhenCleared`, `RaisedWhileItHolds`, `PushedOnlyWhileRaised`, `WaitedRaisedAgainOnExpiry`. Four reversed witnesses: a judgment raised once and never again (`PushedEveryWindow`), a raise again that writes a second judgment (`OneJudgmentAnEpisode`), a judgment left open when its condition clears (`ClosedWhenCleared`), a wait that never expires its hold (`WaitedRaisedAgainOnExpiry`); the instance is Every = 2, MaxClock = 6. With Kind = "behind" (`MCCoordinatorPassBehind`, MaxClock = 7) the condition is read off the judgments late on the coordinator: several of them, each named by its own overdue line, and the pass's judgment is of the oldest still late, so when the coordinator answers the oldest the level falls to the next; its overdue line is the first reminder and the pass counts it from that line, so it is pushed once a tick at most (`OnePushATick`) and never a whole window without a push (`LateRemindedEveryWindow`); the reversed witness counts it from its deadline, and the line and the pass push in one tick (`OnePushATick`). With Kind = "empty" (`MCCoordinatorPassEmpty`, MaxClock = 7) the condition is an up friend's empty row while cards she could do wait, judged only after the empty-row clock is Every old and the clock closed whenever the conjunction is gone (`EmptyAWholeWindow`); the reversed witness keeps the clock while she is down and judges her on her first tick back. With Kind = "pin" (`MCCoordinatorPassPin`) the deal writes the judgment of a named pin placed on another row and the pass keeps that one note; the reversed witness has the pass write its own beside it (`OneJudgmentAnEpisode`). With Kind = "drift" (`MCCoordinatorPassDrift`) holds is the drift the facts say holds, and the facts may say nothing of a type: the third value "unknown" carries the open judgment (and the coordinator's wait on it) as they are, neither closing nor raising it (`UnknownCarried`); the reversed witness closes the judgment on "unknown" (`UnknownCarried`). |
| `CoordinatorWake.tla` | `MCCoordinatorWake*` | the coordinator's wake (cmd/nova-sprint watchwake.go): one run reads the bus up to busBatch = 100 entries past the cursor, and one wake fires every ask that look read (named three, the rest counted) while a filtered message moves the cursor with no line; the judgments, the stop and the friends are level state, not cursors, and each can set and clear between looks. `AtMostOnce`, `NoLapse` over bus events alone, and `LevelSeenWhileItHolds` (a level event that holds a whole window wakes a run, unless it clears first). Three reversed witnesses: `nocursor` (the run fires every ask but writes no cursor: `AtMostOnce`), `skip` (the run fires the first ask and writes the cursor at the newest arrival: `NoLapse`), and `batchdrops` (the run fires only the first ask of the batch and writes the cursor past the rest: `NoLapse`); the instance is MaxBus = 4, MaxLevel = 2, Window = 2. |
| `ReadCards.tla` | `MCReadCards*` | the read cards (docs/SPEC-SPRINT.md section 6, "A read is a consumer card"; internal/sprint read_cards.go): the deal cuts every read the attempt still needs at once, each a card on a different reader; a reader that closed, returned or let pass a read is never dealt it again, a read the machine took back spends no one and is dealt again under the next generation up to MaxReadGen 2; the primary merges only on the needed oks (need: 0, 1 or 2 by the setting or the tier's rule, ReadsNeededIn), and a merging card short of them goes back to review (ShortReadsBack); `TypeOK`, `OneOpenPerReader`, `AtMostNeedStanding`, `NeverTheWorker`, `NeverTwiceAfterSpent`, `MergingOnlyOnOks`, `NoOpenReadOfAnOldAttempt`; one reversed witness (a deal that forgets a spent reader breaks `NeverTwiceAfterSpent`). The readers table is ReadsByRoom's, below. |
| `ReadsByRoom.tla` | `MCReadsByRoom*` | the ask of nova-sprint (docs/SPEC-SPRINT.md section 6, the reads, by room; internal/sprint readers.go `ReadsWanted`, `finderFirst`, `askFinders`, `askPicks`), two rules in one machine by a `Rule` constant: **parallel** (the code, the interim rule of 2026-10-06, `MCReadsByRoom.cfg`) asks a card's reads together, the rest it needs each attempt with an outstanding read counted among those that stand, none once a read found it broken; **sequential** (the design of 2026-10-04, `MCReadsByRoomSequential.cfg`) asks the first read alone and the rest once every read that stands came back ok. Two readers of widths 2 and 1, two pro cards, two attempts each; `WidthRespected` and `FinderFirst` hold under both rules, `ReadsPerLanding` is `Need` reads an attempt under parallel (a pro card broken once lands on four reads, `Need` times broken attempts plus one) and `Need` plus broken attempts under sequential; `OneAtATime` and `OneBroken` hold only under sequential (the parallel pair can be asked and can break together), and `NoAskWhileBroken` (the ask places no read once a read found it broken) and the liveness `Settles` hold under both. Five reversed witnesses: the parallel pair asked together breaks `OneAtATime` and `OneBroken`; readers asked whatever their room break `WidthRespected`; the rework's first read of any reader breaks `FinderFirst` (on the three-reader instance, so a card has a reader to spare); and the ask while a read is broken breaks `NoAskWhileBroken` (also three readers) |
| `WorkImport.tla` | `MCWorkImport*` | nova-work v1 (docs/SPEC-WORK-V1.md section 1): an issue under import (absent, fetched, in the tree, mirrored, closed by the destructive mode, re-opened by export) and a repository's sync, with outside edits at any time; import and verify each take a scope, a subset of the repositories, the import writes only its scope's issues and records the scope in the tree (:scope), verify reads the tree's own scope by default and refuses a --repo outside it, so a verify never reports done outside the import's scope; a tree is written only whole for its scope, only a verify receipt at GitHub's current version licenses a close, an issue the import closed is held by the tree exactly, an external issue is never closed, export restores; six reversed witnesses |
| `DirtyTick.tla` | `MCDirtyTick*` | the sprint machine's tick as the owner shaped it on 2026-09-30, before it is built: four tables updated in turn (work, readers, merge, fleet), a queue per table as its dirty bit, readers, merge and fleet drained until empty, the work table written only by the one pump at the start of each tick; placements by counter modulo the count, width, one wake at the tick's end; 16 reversed witnesses and three goals for the holes it found (a placement blind to the fleet's status never ends the tick; room read from an undrained fleet queue over-fills a machine; the v2.1 rules write the work table in their own steps). The readers' presence (`MCDirtyTickReader`, `MCDirtyTickReaderAway`, W21, W22) abstracts two things away from the Go code: `ReadsStandOnReadersUp` says a read is held only by a reader that is up at every state, but the code keeps a read already begun on a reader that went away (only a read asked and not begun is taken back), and the model has one read per card on a host while the code has none. The model's `raway` always finds a reader to ask again, but the code with fewer than two readers up takes nothing back and asks none, raising the one judgment `fewer than two readers up` instead. The deal's held-stream skip (`HeldStreamNotDealt`; control `MCDirtyTickHeldStream`, witness `MCDirtyTickBrokenDealHeldStream`) and, under ReadCost "half" (read cards on), its reads-before-work order at a read's half slot (`ReadsBeforeWork`; control `MCDirtyTickReadsFirst`, witness `MCDirtyTickBrokenWorkFirst`) are modelled, each off unless its scenario turns it on. `README-DirtyTick.md` says the rest |
| `Timer.tla` | `MCTimer*` | nova-sprint's timers (docs/SPEC-SPRINT.md, "Timers"; `internal/sprint/timers.go`, `internal/sprint/store/timers.go`): an actor sets a timer with `remind`, the machine's tick reads the store's open timers and raises each one whose due time the clock has reached as one judgment addressed to its actor, closing it in the same step, and a restart loses the machine's memory and keeps the store; a timer is raised at most once, never before its due time, a cancelled timer never fires, and an uncancelled one is raised once the clock passes its due time under fair ticks; three reversed witnesses |
| `RouteIndex.tla` | `MCRouteIndex*` | the deal's route choice (internal/sprint/route.go): each tier's route array taken at a uint64 counter modulo its length, one step a card dealt, a route with its first flag set drawn before the others of its tier, a redeal moved past the entries it leaves out (the FieldRoutes taken for the card), an entry the member cannot launch walked past as an unserved one, a resting route walked past too, a pinned card moving nothing, a read card drawn from the reader tier's array at the same index with its avoid set and tier collapse (pro to flash); 3 routes x 2 tiers x 7 cards (one a read); reversed witnesses: a random draw breaks RouteFair, a redeal that does not move past the excluded entry breaks ExcludedNeverDrawn, a read that leaves the index where it is breaks RouteIndexAdvancesOncePerCard, the deal before fault 10 breaks NeverUnlaunchable, the tick judging over every member breaks StrandedIsJudged, and the walk that ignores the first flag breaks FirstPreferred; one reach witness (ReachSkip) |
| `Level.tla` | `MCLevel*` | the fleet's level (internal/sprint `level`, `round.levelTo`), the rebalance at the start of every tick and inside every planner that computes holds: one call's loop over every start of a bounded fleet (every up order, every start of the deal's index, members over their width, a card with a refuser). It ends by two guards, each enough alone: every move lowers the sum of squared backlogs by at least two (`PotentialFalls`), and a moved card is never queued again, so a call makes no more moves than there were ready cards (`MovesBounded`); no card lands on a member that refused it at staging, and an older card moves when the newest is blocked (`SelectionComplete`). Five reversed witnesses: the rule of the wedge of 2026-10-02 (nova-tools#5122) does not end, nor does the gap test removed, a moved card queued again moves twice, the gap tested only on the newest card stops early, the refusers not skipped lands a card on one |
| `Land.tla` | `MCLand*` | `nova-sprint land` (cmd/nova-sprint/land.go): a batch of a stream's merge queue pushed to a remote base, then reported to the store through the merge step, with the outside between them (an accept anywhere in the queue, a return, a rework that replaces a card's head and keeps its id and epoch, another lander, a clear that moves the epoch, the base moving, a crash at any step); a head is <<card, attempt>>, and the check before the push and the push are separate steps, as in the code. It proves: the store records a card landed only at a head the base holds (LandedInBase), a card lands only with every card ahead of it (LandsInOrder), no push for a caller whose epoch the store was not at when read (CallerEpochCheckedBeforePush), no report records at an epoch the lander does not hold (ReportHoldsTheEpoch), and a batch pushed and not reported is recorded by running land again (Recovers, under fairness); since v1.2.6, the machine never offers to accept a card at the head the landing refused (NoOfferAtRefusedHead; reversed witness `MCLandBrokenOfferRefused`, the loop of 2026-10-11 in which about 30 cards went accept, refused, ready to accept, accept). It does not claim that no push follows a clear: a clear between the check and the push makes a push for an epoch just left (ReachStalePush reaches it), which nothing records and nothing undoes. Five reversed witnesses (the report before the push, the caller's epoch checked only after the push, a report with no guard, a guard on ids and not heads, a report with no epoch fence) and two reach witnesses (ReachStranded: the lander's own push for the store's epoch left unreported, marked by the ghost lpushed, to which a push for an epoch the store has left adds nothing; ReachStalePush). Recovers is proved only once the outside goes quiet (the instance caps outside events at 4), so it does not cover (a) a lander that crashes between the push and the report on every run, which never records the batch, or (b) a base that moves twice inside every read-to-push window: one rebuild, then the lander gives up (the rejected fact), every run; nothing is pushed or lost, the cards stay queued, and the coordinator resumes the stream and runs land again |
| `LandPass.tla` | `MCLandPass*` | the land pass over several streams (cmd/nova-sprint/landpass.go; the owner, 2026-10-07: the merges in parallel, the merge after serial): each stream's batch cut from the base and gated once in its own worktree, up to a width at once; the green ones landed one at a time in priority order, a batch whose base moved merged again onto the new tip and pushed with no new gate when its files and the files landed since are disjoint, else gated once combined; the oracle of which trees are green enumerated over every tree, so a batch green alone and red combined is a behaviour. It proves `BoundHeld`, `BaseAdvancesGated` (every push is a gated tree or a clean merge of disjoint files onto one), `LandsOnce`, `BaseKeepsLandings` (the merge after never loses a landing), `RefusedStaysMerging` (a collision refusal leaves the batch queued), the action property `RefusalTouchesNoOtherStream`, and under fairness `Lands` (a batch green on every tree lands); six reversed witnesses (no combined gate, the own tip pushed over a moved base, a collision recorded as the conflict fact, a refusal that reverts the base, a disjoint merge's tip recorded as gated, a re-merge of no head reported landed); the next pass's base gate reads the record of gated trees (`CachedIsGreen`: a clean merge of disjoint files is never recorded, since two heads green alone can be red together) and a re-merge may merge fewer heads or none |
| `ServerLanes.tla` | `MCServerLanes*` | the sprint server's line of control and its lanes (cmd/nova-sprint serve.go `serveCtx`, servelanes.go, seriallock.go, internal/sprint/control_line.go, internal/sprint/readers.go; docs/SPEC-SPRINT.md section 14, The server; 2026-10-04): the run loop's tick and each batch of line verbs (a write, read --begin, or a reader's queue, which is its beat) hold the one line in turn; a batch takes the next ticket and is served in ticket order (first come, first served), the tick takes the line at its turn (when no batch waits, or once its turn is due — after the holder in flight and before every batch still waiting), and a gone caller's ticket is served and handed back. A friend's beat runs on the beat lane and a read on the read lane (one read at a time), and any caller can go away at any time. A read's lease lives on a reader card in the Readers Reading column: read --begin (a line verb) begins it, the reader's served queue renews it (best effort, errors ignored), a lease not renewed lapses, and on start the server keeps every live lease and takes back the lapsed. It proves a line verb runs only holding the line (`WriteHoldsTheLine`), a waiting beat can always start whoever holds the line (`BeatNeverWaitsForTheLine`), a read waits for another read alone (`ReadWaitsOnlyForReads`), a verb whose caller went before it started never runs (`GoneNeverRuns`; TLC found the first cut's beat lane breaking it), the lanes never hold the line (`LanesHoldNoLine`), no batch is served before an earlier ticket still waiting (`FCFS`), the tick waits at most one turn (`TickWaitsOneTurn`), a live lease is never taken back (`LiveLeaseNeverTakenBack`), every lapsed read is taken back (`EveryLapsedReadTakenBack`), and every batch sent is answered or dropped (`EveryBatchEnds`, under fairness, on the smaller `MCServerLanesLive`). Five reversed witnesses: a beat on the line (`beatonline`, the code before the lanes), a read on the line (`readonline`), a write whose caller went still run (`rungone`, the sync.Mutex of 2026-10-04, which kept every timed-out beat in the line), the tick queued like a batch (`tickqueues`, which fails `TickWaitsOneTurn`), and the served queue never renewing (`norenew`, which fails `LiveLeaseNeverTakenBack`). It does not model what a verb does to the sprint, nor the tick's own length on the line |
| `RateBudget.tla` | `MCRateBudget*` | the global rate limiter (pkg/ratebudget, internal/sprint/store/rate.go, cmd/nova-sprint rate_budget.go; the owner, 2026-10-11: "the rate limited should just stall out delay, not fail", "i don't want this rate limiter causing errors"): callers on any machines take a model's requests-per-minute window and a provider's concurrency lease before each model request, each take a WATCH read and a MULTI/EXEC that commits only if the key did not move; a lost race or a spent window releases the slot and the caller stalls; the request renews its lease best-effort (a renewal not written lets the lease lapse while the request still runs); a 429 that gets through is retried in place; a dead holder's lease runs out; a store that does not answer grants open. Time is ages, so the space is finite with no clock bound. It proves `NeverOverRate` (at most N recorded grants in any window), `NeverOverConcurrent` and `InFlightHeld` (each while every renewal inside its bound succeeds), `AbortedWriteReleasesSlot` (a lost race releases the slot), `Progress` (while anyone waits someone is eventually in flight, with demand and refusals unbounded) and, with each caller's requests bounded (`MCRateBudgetLive`), `StalledIsGranted` and `SlotReclaimed`. TLC found that without a queue a caller whose rival always asks first can be passed over forever; the waiters jitter their polls, and the bounded instance is the claim. Four reversed witnesses: an unguarded write (`MCRateBudgetBrokenRace`, `NeverOverRate`), a live holder whose renewal fails, so its lease lapses (`MCRateBudgetBrokenNoRenew`, the renew-failure case, `NeverOverConcurrent`), a dead holder's lease that never expires (`MCRateBudgetBrokenNoExpiry`, `SlotReclaimed`), and a holder that keeps its slot after an aborted write (`MCRateBudgetBrokenKeepSlot`, the pre-fix behaviour, `AbortedWriteReleasesSlot`). Clock skew between machines is not modelled: the window is a minute plus two seconds |
| `LiveRuns.tla` | `MCLiveRuns*` | working against what really runs (internal/sprint/live.go `LiveReturns`, the tick's `live` part and the STOPPED tick's `LiveReturnStep`, internal/sprint/store/live.go `stopOwned`, machine_transition.go `unsettledStopDebt`, pkg/member/member.go `liveWords`; 2026-10-10, v1.2.6): a worker's beat carries its live set (each child it runs, each finished report it holds), a beat window counts a working card absent from it, and the tick, RUNNING or STOPPED, returns a card absent for the grace to ready at its next generation with its receipt. A `Legacy` row (a worker from before v1.2.6) carries no live set, so the reconcile skips it and its cards never return, and a take names its card in the live set only at the member's next pass, the grace from the take covering that lag. It proves working is a live run within the grace (`WorkingIsLive`, over the rows that carry a live set), no working card of a legacy row is returned by the reconcile (`LegacyNeverReturned`), a STOP debt entry owns its card only while the card is working at the captured generation (`StopMarkerIsWorking`) and an owned card leaves working only with its receipt (`OwnedLeavesWithReceipt`), no finished report is lost (`NoReportLost`) and no running child's card is returned (`NoRunReturned`) on a row whose beat carries a live set, and a return settles its entry off the debt so clear reads a debt that is owed alone (`DebtIsOwed`). `SettleByBeat` (settleStopDebtByBeat, on START's path) returns a STOP-owned card whose row has beaten since the STOP to ready at gen+1 with its receipt, without the owner's own stop-return, when the row's stop-returns count is zero and, on a row whose beat carries a live set, that set does not name the card running or held (`LiveNamedAt`); a `Legacy` row's beat names nothing, so its lease is settled by the count alone, as before #87, and a held report or a child that has not yet seen the STOP is lost there: `MCLiveRunsSettleByLegacy` checks the every-row forms `NoReportLostAll` and `NoRunReturnedAll` and expects them violated (a v1.2.14 candidate). Eight reversed witnesses: a STOPPED tick that returns nothing (the v1.2.5 tick: 59 stale takes on 2026-10-10), a legacy row reconciled as an empty set (its working cards returned), the grace from the take dropped (a card returned while its child still runs), a debt that owns a card by id until start (the v1.2.5 debt: hold refused friend.zhi's row after her stop-returns), a live set without the held reports (a finished report's card returned, its generation gone), a silent row's stale beat read as an empty live set (a card whose child still runs returned: the first cut of v1.2.6, found by its cold read; the reconcile judges only a fresh beat), and a reconcile that returns a card but leaves its receipt in the debt (the stale STOP of 2026-10-10 that blocked clear), and a beat settle that reads the row's stop-returns count alone, not its live set (`settlebycount`, the code before #87 for a machine lease: a finished card's held report settled and dealt again, its generation gone, `NoReportLost`); `MCLiveRunsReachStoppedReturn` shows a STOPPED tick returning a dead take. It does not model the friend daemon's lanes (nova-friend), the deal, or time beyond beat windows |
| `StatsReset.tla` | `MCStatsReset*` | `nova-sprint stats reset` with `stats tidy` (internal/sprint/stats_reset.go, internal/sprint/store/stats_reset.go, stats_tidy.go): figures that grow, appear, and that a tidy shrinks by any subset of a row's finished cards (not the oldest first); ghosts count the cards since the mark and since a reset's read. The reset is two steps (its read, refused while a tidy is in flight, and its compare-and-set write, which reads again when the record moved), the tidy three (its marker with the mark it saw, the move, its end rebasing that mark by the moved cards finished before it), the tick's where record two (read, write; taken only at the table's revision and the record's stamp). `ShownIsSinceMark` (with no tidy in flight every shown figure is the cards since the mark still on the row: the epoch's less the mark's, never below zero), `RecordShowsShown`, `UnknownReadsAsBefore`; reversed: a tidy that keeps the mark's counter, one that lowers it by count, a reset with no wait and no compare-and-set with a tidy that rebases any mark (each `ShownIsSinceMark`), a where that takes a record by its revision alone (`RecordShowsShown`), a reset that reads every tidy marker as ended and clears it mid-move (`stale`, `ShownIsSinceMark`). A clock and `TidyStale`: a marker older than it is read as ended by the reset and the next tidy (TidyInFlight.Fresh); the design lets a tidy finish inside `TidyStale` (`SlowTidy` FALSE), and `MCStatsResetSlowTidy` (a tidy slower than `TidyStale` racing a reset) expects `ShownIsSinceMark` violated: the tidy's end rebases only the mark it saw, a v1.2.14 candidate |
| `AttemptsEpochs.tla` | `MCAttemptsEpochs*` | the sprint's epoch and clear (docs/SPEC-SPRINT.md section 13; internal/sprint/store epoch.go, clear.go): the epoch hash's n, its restore marker, the per-epoch streams, readers and members, a step's pin, clear as the CAS on n (its advance by one, its settle of the restore it set, the members carried and the new epoch's streams and readers blank, a second concurrent clear writing nothing), a late writer of the old epoch refused as stale, and the pin's restore; and a card's attempt, generation and redeals (internal/sprint/steps_work.go deal, redeal, nextGen, liveGen; stop_return.go StopReturn; live.go LiveReturns; lane_hold.go laneGoneUnits; rules.go Progress): Deal cuts a new attempt's work card (attempt + 1, generation 1, refused when it exists already), Redeal deals a withdrawn card again (the same attempt, generation + 1, a redeal counted only when a take of it ended), the other generation steps step the generation with no redeal, and Launch is the card's push to its branch, one per generation. It proves `OldEpochWritesNothing` (a writer holding an old epoch writes nothing into the new one), `NewEpochBlank` (the new epoch is blank except the members), `OldEpochReadable` (a card written to an epoch stays there), `EpochMonotone`, `SuffixUnique` (no two launches share an attempt, a generation and an epoch) and `StaleGenRefused` (a report naming a generation that is not the live one is refused); three reversed witnesses, `noepochcheck` (breaks `OldEpochWritesNothing`), `keepgen` (keeps the generation on a redeal, breaks `SuffixUnique`) and `nogenguard` (takes a report of an old generation, breaks `StaleGenRefused`). The crash cut between `AdvanceEpoch` and `SettleEpoch` (restore left set, so the next pin copies the old streams and readers) is a v1.2.14 candidate and is not modelled. The redeal cap's judgment and the tier ladder are `DirtyTick`'s (`RedealsAreEndedTakes`, `RedealBoundHolds`) and `SprintRules`' (`LadderClimbs`) |
| `ProviderFunds.tla` | `MCProviderFunds*` | a provider's funds over `RouteRest` (internal/sprint/provider_funds.go `providerRestsDue`, `providerConds`, `AllOutOfCredit`, `Funded`, `friendsKeepRunning`; balance.go; route_coordinator.go): `RouteRest` instantiated once for each of two providers with one enabled route each, so no rest rule is copied; two outstanding takes a provider, each launched after the last rest ended, stale or with no launch time (a finite order in place of `e.taken.After(last.Mark())`), a refusal for credit or the key resting the provider only from a take launched after the last rest ended; a payment the poll sees, funded and the coordinator's wake ending the rest and setting that mark (a timed coordinator's rest the clock ends is the same step as the wake); the poll writes no rest; `Judgments` defines providerConds' one judgment a provider, a definition the model does not check (funds or key before low, low only while no rest of its own holds); the tick stops the machine only when every provider rests for credit and no friend is up, and START is the coordinator's, refused while all are out unless a friend is up. It proves `StopOnlyWhenAllOut`, `LowNeverStops`, `NoFundsStopWhileFriendUp`, `StartIsTheCoordinators` and `RefusalsAfterLastEnd`, with three reversed witnesses: `MCProviderFundsBrokenLowIsOut` (Out counts a low balance: `LowNeverStops`), `MCProviderFundsBrokenNoFriendException` (no friend exception: `NoFundsStopWhileFriendUp`) and `MCProviderFundsBrokenStaleRefusal` (a refusal from a take launched before the last rest ended rests the provider again: `RefusalsAfterLastEnd`). Balance and spend arithmetic, the judgments' words and the STOP debt (`StopReturn`) are outside it |

### The fleet's level (Level)

`Level.tla`: one call of the fleet's level (internal/sprint/steps_work.go
`level`, round.go `levelTo`). On 2026-10-02 at 2:26 PM the call did not end:
the emptiest member had refused the longest queue's newest card at staging, the
card went to a member one below instead, the receiver became the donor and the
card went back, a unit appended per turn under the server's mutex, until the
process held 244 GB (nova-tools#5122). The module's header says what it holds
and what it leaves out; the instances are in `MCLevel.tla`.

The model found one defect beyond the wedge, in PR #5127 as it stood: a card the
call moved was appended to its receiver's queue, so it could be moved again in
the same call. `MCLevelBrokenRequeue` is the call: widths 2, 1 and 3, members up
in the order c, a, b, three ready cards on b, the middle one refused by c; b3
goes b -> a, b2 b -> a, then a is the longest, its newest (b2) is refused by c,
so b3 goes a -> c, and b1 b -> c: four moves for three ready cards. Checked by
hand against the code at 0a79ad873 and by `TestLevelMovesNoCardTwice`, red there:
the second unit of b3 is guarded on b3's place before the first unit, so the plan
the engine writes is not the plan it computed (`fleet: b3.w1: revision 2,
expected 1`). The code no longer queues a moved card again.

Run on a macOS arm64 working machine, not the bench (TLC 2.19, jar SHA-256 `936a2620...`, OpenJDK 27, one worker,
niced, 2026-10-02 ET), the eight cases with `-lncheck final -deadlock`; the bench
records in `RUNS.tsv` are owed (the class test is red for these eight until a
Linux bench run is merged):

| config | result | distinct states | time |
|---|---|---|---|
| `MCLevel` | no error (exit 0), three members, three cards, every up order: TypeOK, NeverOnARefuser, MovesBounded, SelectionComplete, PotentialFalls, Terminates | 649,537 | 57 s |
| `MCLevelFour` | no error (exit 0), the wedge's four members: the same six | 472,115 | 57 s |
| `MCLevelOldRuleNoRequeue` | no error (exit 0): the rule of the wedge with no second move ends, within the ready count, off every refuser | 473,903 | 42 s |
| `MCLevelBrokenOld` | expected Terminates violation (exit 13): c3 goes a -> t -> a for ever | 466,243 | 62 s |
| `MCLevelBrokenNoGap` | expected Terminates violation (exit 13): c2 goes a -> c -> a for ever | 654,916 | 74 s |
| `MCLevelBrokenRequeue` | expected MovesBounded violation (exit 12): four moves for three ready cards, b3 twice | 643,223 | 62 s |
| `MCLevelBrokenNaive` | expected SelectionComplete violation (exit 12): the call returns with c2 able to move | 360,485 | 36 s |
| `MCLevelBrokenNoSkip` | expected NeverOnARefuser violation (exit 12): c3 onto c, its refuser | 360,485 | 37 s |

Every counterexample was read against the code by hand. Old: the wedge's shape
in four members (a 2 above its width with c1 and c3, c3 refused by s; s 0, the
emptiest; t 1, at the mean; u 4 and full): c3 goes a -> t at the mean, then t is
the longest and c3 goes back to a, for ever (levelTo at b7776ca3, the longest
and the refusers avoided, no gap). NoGap: the same shape in three members with
the gap test removed from PR #5127's levelTo. Naive: a holds c2 and c3, c3
refused by c, the emptiest two below; only c3 is tried, and the call returns
with c2 still able to go to c. NoSkip: c3 goes onto c, its refuser (the code
before #5000).

### Models moved from nova-tools

`FriendReadyTake` (five cases), `FriendRedealLate` (two), and `StopFence`
(eight, formerly nova-tools `StopReturn`) preserve the machines and reversed
witnesses at nova-tools dev `2314d437fedb33d148e75346ab5582ec552e7962`.
Only names and source/header comments changed. `StopFence` includes its
`StaleTick` and `StalePart` instances; it does not replace this repository's
`StopReturn`. Their groups are `friendreadytake`, `friendredeallate`, and
`stopfence`. Coverage stays partial for the concrete differences recorded in
`COVERAGE.tsv`; fresh TLC passes do not erase those differences.
