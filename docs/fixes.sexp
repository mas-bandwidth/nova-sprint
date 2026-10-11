; The nova-sprint point releases: the data FIXES.md is generated from.
; Read by internal/roadmapdoc through internal/worklang (data, never evaluated).
; Edit this file, then run `make roadmap`; never edit FIXES.md by hand.
; The shape is in internal/roadmapdoc/fixes.go.
(fixes "v1"
 :title "nova-sprint fixes"
 :text "The point releases from v1.2.3, the first release from this repository: what each one shipped, is
  shipping or will ship. A point release carries fixes; new work is in ROADMAP.md. The one
  exception is v1.2.5, which also carries the roadmap machinery (the owner's scope for it), including the
  verb that defers cards to the roadmap. nova-tools' point
  releases are in that repository's FIXES.md. The point releases after v1.2.10 (the owner, 2026-10-11): v1.2.11
  small fixes, v1.2.12 tests and models only, v1.2.13 contraction, v1.2.14 the next small fixes, each
  release's scope below. Point releases continue until only architectural items are open; those are
  v1.3's re-architecture, and ROADMAP.md says how the two fit. An area stops taking point fixes when a fix
  there is the same problem in another form, or would be its third fix: from then on it is
  architectural, and its v1.3 item names the point fixes it replaces. Friend work is dogfooded within the
  week once more friends are online: the current point release adopted, friends alone first, then
  friends and fleet together, and each friend bug found becomes a point-release card."
 :releases
 ((release "v1.2.3" :status "shipped" :date "2026-10-10"
   :text "The first release from this repository: nova-sprint, nova-card and nova-work, re-seeded from nova-tools.")
  (release "v1.2.4" :status "shipped" :date "2026-10-10"
   :text "One fix: the sprint store's function library is back at its v1.2.2 digest, so an adopt can load it.")
   (release "v1.2.6" :status "planned"
    :text "The fixes held during the split, the point-release candidates found on 2026-10-10, the owed
     tests, and the sprint store cards that are point-release work. Each is re-applied or written in
     this repository, then cut as one release."
    :date "2026-10-10")
   (release "v1.2.7" :status "planned" :date "2026-10-10"
    :text "The adopt can carry a change of the store's function library: its digest covers code, not
     comments, the pre-window shadow tick passes when only the library differs, and the post-load check
     refuses a library that is not the new build's.")
   (release "v1.2.9" :status "planned" :date "2026-10-10"
   :text "Seat, tick and fleet fixes: judgments waiting on the seat re-pushed, and a standing judgment pushed
    once while it holds; the tick under its gate at load; holds and approvals read at the current head;
    log --since windows wider than 22 hours; a loaded bench not marked down; a reader-only friend dealt no work; a
    machine fault hold that clears itself.")
   (release "v1.2.10" :status "shipped" :date "2026-10-11"
    :text "Cut from main between sprint releases: no deal to a draining member, the dashboard as the live page, main green after the night's landings, and two CI flakes fixed.")
   (release "v1.2.11" :status "planned" :date "2026-10-11"
    :text "Small bug fixes, one concern each, and no model change. They run as cards on v1.2.10 once it is
     adopted, so the sprint dogfoods the release it just cut.")
   (release "v1.2.12" :status "planned" :date "2026-10-11"
    :text "Tests and models only, no product change. More unit and functional tests, and every TLA+ model
     checked against the code it claims to model (its state, actions and invariants) and run again with
     TLC on a bench; owed models and stale or timed-out records are made real. The new tests are written
     against behaviour (the verbs, the store's steps, the wire, the TLA+ invariants), not internals, so
     the same tests run unchanged against v1.3 and say whether it keeps the behaviour. TLC runs where the
     cards run first. A real defect a new test finds is written as a v1.2.14 card, never fixed inside a
     test card.")
   (release "v1.2.13" :status "planned" :date "2026-10-11"
    :text "Contraction: fewer lines, no behaviour change, with v1.2.12's tests and checked models as the net
     under it. Passes in this order, one writer per package: remove dead and stale code; adopt the Go
     standard library or established packages where they replace hand-rolled code; run go fix (and gopls
     modernize) over all Go code; pull code the tools share into modules, then code the modules share
     into modules; make each test minimal, on test frameworks and shared helper modules. The module pass also covers
     the copies across the two repositories: 59 same-named packages exist as separate copies in
     nova-sprint and nova-tools (about 71,500 shared lines, measured 2026-10-11 at nova-sprint 15c779c
     and nova-tools 2a648d8). Each gets one owner, nova-sprint as with nova-roadmap, and the other
     repository imports it: the identical copies first, then the diverged ones (member, fleet, cardtree,
     tool, and parts of friend and swarm), reconciled under v1.2.12's tests. The first named entries are
     the nova-roadmap extraction and the brief boilerplate moving into the card contract. Measured as
     lines per repository and package, product against test, from a baseline taken before the first
     pass.")
   (release "v1.2.14" :status "planned" :date "2026-10-11"
    :text "The next small fixes, one concern each, including the defects v1.2.12's tests find, a circuit
     breaker on the review loop, and a red base pushed to the coordinator.")
   (release "v1.2.15" :status "planned"
    :text "The next small product fixes, one concern each."))
  :items
   ((fix "a-take-refuses-a-card-whose-needs-have-not-landed" :release "v1.2.12" :status "planned"
     :title "A card whose named needs have not landed is neither dealt nor taken"
     :text "One rule, three doors. A primary whose named needs have not all landed (WaitsFor, steps_work.go) was dealt to a friend, so on 2026-10-11 19:17-19:21Z a friend took six cards whose DEPENDS-ON needs had not landed, each raising a rule-11 invariant, and the take-back verb refused them as working. TickDeal's offer (steps_tick.go) now leaves out a card WaitsFor names needs for, as dealPlan's ready predicate does, and friendDealPass skips such a card too, so neither the ladder nor the attempt cap's deal hands it to a friend. A ready work card already dealt on any row, friend or member, whose primary's needs are not landed goes back to the pool at once in the tick's deal (needsWithdrawals): the work card leaves the fleet table at a new generation, its attempt unchanged and no redeal spent, the primary waits for its needs again, and the note names the needs; so a friend unheld with such a card in her set never takes it. takeOne refuses a card by id whose primary's needs are not landed (\"<id> needs <n,...>, not landed: not taken\") and a take by count passes over it. A take by id of a card already working on the same row at the generation named is a no-op, writing no record and no note, so the log shows one take, never one every 15 to 16 s. docs/SPEC-SPRINT.md (needs and the deal) states the rule for friends and takes."
     :origin "this card, a-take-refuses-a-card-whose-needs-have-not-landed.w2, 2026-10-11 ~19:25Z; the coordinator for the owner's \"Please log cards to fix this deal and rebalance. It's shit.\" (a friend took six cards whose needs had not landed; log --card schema-dart-msg2-batch repeated a friend:working -> a friend:ready every 15 to 16 s); code read at nova-sprint main af21db3b")
    (fix "tla-liveruns-rows-with-no-live-set" :release "v1.2.12" :status "planned"
     :title "LiveRuns models the rows whose beat carries no live set and the take's one-pass lag"
     :text "Models only, no product change. tla/LiveRuns.tla gains a Legacy constant (the rows whose beat carries no live set, workers from before v1.2.6) and a live variable, the beat's live set as the member's last pass left it. The reconcile skips a Legacy row as the code does (liveStopped returns early, LiveReturns continues past a beat with no live set, LiveCountKeys counts every such card running), WorkingIsLive is restated over the other rows, and the new LegacyNeverReturned holds that no working card of a Legacy row is returned by the reconcile. Take no longer names its card in the live set at once: it is named at the member's next pass, and the reconcile waits the grace from the take. Two reversed witnesses: Broken \"legacyreturned\" reconciles a Legacy row as an empty set (breaks LegacyNeverReturned) and Broken \"nograce\" drops the grace from the take (breaks NoRunReturned). Every liveruns case runs with Legacy = {\"r2\"} so one row exercises the legacy path in each; every existing case keeps its declared result."
     :origin "this card; v1.2.12 inventory item T12, drift read at nova-sprint main 00c54ba2e 2026-10-11")
   (fix "a-repo-refused-at-staging-is-not-dealt-to-that-member-again" :release "v1.2.12" :status "planned"
    :title "A repository refused at staging is not dealt to that member again"
    :text "A staging refusal for want of a bench mirror says the member cannot stage any card of that repository, so it becomes a capability, the way a harness the member cannot launch is one (Launches, notLaunching): mirrorRefusals, derived each tick from the fleet table, is per member the repositories it refused at staging for want of a bench mirror within StagingMirrorWindow (one hour), from the staging takes (StagingTakes) whose error names \"no bench mirror for <url>\", the repository of that url, and a later ok finish by the member of a card of that repository ends it at once. The member check the deal (launchersOf), every move of a dealt card (notLaunching) and routeOf when it names a member share refuses a member for a card whose repository it refused, with the why \"<member> refused <repo> at staging at <time>: no bench mirror; add the mirror on <member> with the fleet play\"; when no member is left the no-launcher judgment says so. A refusal of any other cause stays per card (StagingRefusers)."
    :origin "this card, a-repo-refused-at-staging-is-not-dealt-to-that-member-again.w2; the owner, 2026-10-11 ~19:05-19:20Z: \"Please log cards to fix this deal and rebalance. It's shit.\"; \"cards only may accrue per-fleet or friend, if there are not other machines/friends ready to do that work right now!\"; studio sat at 4-5 of 16 while its takes were refused 78 times in 2 hours")
   (fix "tla-coordinatorpass-wait-expiry" :release "v1.2.12" :status "planned"
    :title "CoordinatorPass models the wait and its expiry, with a reversed witness for a wait that never expires"
    :text "Models only, no product change. tla/CoordinatorPass.tla gains the wait the code has (internal/sprint/steps_ack.go, Wait): a Wait action that closes the open judgment and holds its condition until a review time chosen within the bound, a tick that raises the condition again when the review time is reached and the condition still holds, and the reraise's skip before the review time (a held judgment is not raised again before it). OneJudgmentAnEpisode, PushedEveryWindow and ClosedWhenCleared still hold, and the new WaitedRaisedAgainOnExpiry (a wait never outlasts its review time) holds. One reversed witness, Broken = \"waitforever\", never expires the hold and breaks WaitedRaisedAgainOnExpiry; the instances and the witness are in tla/README.md and tla/CASES.tsv, and the module's COVERAGE row drops the wait from its gaps. steps_ack.go's Wait gains one comment line citing tla/CoordinatorPass.tla Wait; no other Go file changes."
    :origin "this card, tla-coordinatorpass-wait-expiry-2.w2, 2026-10-11: v1.2.12 inventory item T26, first of two; drift read at nova-sprint main 00c54ba2e")
   (fix "tla-retire-seatproof" :release "v1.2.12" :status "planned"
    :title "The SeatProof model of the deleted push proof is retired"
    :text "tla/SeatProof.tla modelled the push proof (its header names PushPong, PushLive, PushWhy,
     renewSeatProof and a pushGate) with its instance MCSeatProof.tla and three configurations; none of
     that code exists: the PROOF files and the seat pong are gone (internal/sprint/pushproof.go:7-11,
     cmd/nova-sprint/pushproof.go:9-14), the old verbs are gone (cmd/nova-sprint/hook_test.go:221-228),
     and the hook replaced the push proof (docs/SPEC-SPRINT.md:6311), modelled by SeatHook.tla. The only
     pushGate left is a different one, a concurrency limit on pushes (pkg/member/member.go:531-536). No Go
     file cites SeatProof and it has no COVERAGE row. The module, its instance and the three configurations
     are deleted, and their three CASES.tsv rows (MCSeatProof, MCSeatProofBrokenNoRenew,
     MCSeatProofBrokenRenewDest) and three hand-written RUNS.tsv rows (started_utc ending .000000, seconds
     \"-\") are removed, and nothing else in either file. SeatProof (the push proof) was retired in v1.2.12:
     the hook replaced it; SeatHook.tla is its model."
    :origin "this card, tla-retire-seatproof.w1, 2026-10-11: v1.2.12 inventory item T1 (\"SeatProof models deleted code\"); drift read at nova-sprint main 00c54ba2e")
   (fix "tla-dirtytick-held-draining-and-stopped" :release "v1.2.12" :status "planned"
     :title "DirtyTick models held, draining and stopped members"
     :text "Models only, no product change. tla/DirtyTick.tla gains a member status held, a per-member drain flag, and a run state (running or stopped), each turned on by its scenario so the existing configurations keep their state spaces. New invariants NeverDealtToDraining (no deal goes to a draining member), NotAskedOfHeldOrDraining (a held or draining machine's reader is asked nothing) and StoppedMovesNothing (a stopped tick changes no card's column), with the reversed witnesses MCDirtyTickBrokenDealDraining and MCDirtyTickBrokenStoppedPump and the control cases MCDirtyTickHold, MCDirtyTickDrain and MCDirtyTickStopped. The all-providers-out stop is the outside event that sets run to stopped at the end of a tick."
     :origin "this card")
   (fix "cover-store-stats-tidy-and-reset" :release "v1.2.12" :status "planned"
    :title "The store package's own tests pin stats tidy and stats reset"
    :text "Tests only, no product change. New tests in internal/sprint/store drive TidyStats and ResetStats through the unit tier's Mem store and assert on their results, on what StatsTidied and StatsReset read back, and on whether the store changed: a dry run counts and writes nothing; a fleet tidy moves history off the done cells and records the archive done; a second tidy within TidyAgainAfter is refused; a fresh tidy marker refuses a tidy or a reset while a stale one is taken as ended; routes alone moves nothing and streams clears the where record; a refused move keeps its archive failed and clears the marker; a reset writes its mark and clears the where record; operation ids replay, conflict and are refused by shape; a compare-and-set that keeps losing errors after its tries; and a tidy's kinds and archives are kept across a reset."
    :origin "this card; tmp/v1.2.12-inventory-2026-10-11.md section 8 item C5")
   (fix "cover-store-shadow-tick-and-holds" :release "v1.2.12" :status "planned"
    :title "The store package's own tests pin the shadow tick and the holds"
    :text "Tests only, no product change. New tests in internal/sprint/store drive Store.ShadowTick and Store.Holds through the unit tier's Mem store and assert on what a caller sees. ShadowTick: a running sprint with ready cards reports RUNNING with a deal part on the work table and a size the sum of its parts, and writes nothing; a STOPPED machine still plans; an epoch that owes its restore refuses and still owes it after; an operation left pending refuses (it repairs nothing) and is still pending after; queued work-table changes are planned as the drain part and the queue is not emptied; a primary waiting on reads is named in the plan's Reads; a store that moves on every read says the sprint is busy; and a custom Updates list leaves an empty part out, lists a due part with its due count, and a part that panics makes ShadowTick panic. Holds: a store with no sprint returns nil and no error; a held member, reader, friend and stream come back in kind order (members in fleet-table order, streams by name) each with its reason, and the list is empty after unhold; a member held with --return shows Return and without it none; an unreadable reader record is left out with no error and a friend's fleet row is not listed as a member. One defect the tests found (a reader set away with SetReaderAway alone, not held, is listed as held) is written as a v1.2.14 card, never fixed here."
    :origin "this card; tmp/v1.2.12-inventory-2026-10-11.md section 8 item C6")
   (fix "tla-landpass-prefix-landing-and-unblamed-refusal" :release "v1.2.12" :status "planned"
    :title "The land pass model: a red gate lands the prefix and reworks the blamed head"
    :text "Models only, no product change. LandPass.tla's gate now does what cmd/nova-sprint/landpass.go does when the batch's tree is red: a bisection that blames one head cuts the batch to the prefix before it, pushes that prefix and reworks the blamed head with the stream going on (no longer stopped); a refusal that blames no head (a bench that did not run the gate, a cancelled gate, a bisect environment refusal) returns the batch to queued; and a batch whose tree tests read none of its files lands whole. The new invariant NoHeadPastItsBlame (no head at or after the blamed one lands in that pass) holds, as do LandsOnce and BaseKeepsLandings. Two reversed witnesses: landpastblame pushes the whole batch on a blamed red (fails NoHeadPastItsBlame), unblamedstops stops the stream on an unblamed refusal (fails UnblamedRefusalQueues). A comment on treeGateBase cites tla/LandPass.tla BaseRed."
    :origin "this card; v1.2.12 inventory item T18")
   (fix "cardmachine-tlc-record-owed" :release "v1.2.12" :status "planned"
    :title "CardMachine gets a finished record and a reversed witness"
    :text "Models only, no product change. tla/CardMachine.tla is the copy model of nova-tools' deleted Lua
     card machine (02_card_move.lua, its deletion recorded in nova-tools internal/ci/testdata/deleted-tests.txt),
     kept because CardISA EXTENDS it and reuses its actions. Its one case (MCCardMachine.cfg, Cards {c1,c2} with
     c2 needing c1, Consumers {k1,k2} one slot each, MaxCopies 3, HeadBound(2), nine invariants and three temporal
     properties under SF) had never finished: its only record was a timeout. A reduced instance beside it:
     MCCardMachineSmall.cfg (Cards {c1} with no Deps, Consumers {k1,k2}, MaxCopies 2, HeadBound(1)) checks the nine
     invariants only, and MCCardMachineSmallLive.cfg the same instance with ReadyIsOneWay, CopiesEnd and Progress.
     One reversed witness, MCCardMachineBrokenReadsInReview, removes DealRead's where = \"review\" guard so a ready
     card gets a read cut outside review and must violate ReadsInReview. The original over-budget instance moved to
     tla/cardmachine-bench/ (its timeout row removed from RUNS.tsv), and two stale comments in steps_review.go and
     friend_read.go cite EndReadStale now. CardMachine itself, CardISA and the running lifecycle (SprintTables) are
     not touched."
    :origin "this card: cardmachine-tlc-record-owed.w4, from draft tmp/next-sprint-cards/cardmachine-tlc-record-owed.md
     (epoch 16: MCCardMachine.cfg timed out at the 110 s cap, and a --manual rerun at 1800 s timed out too) and the
     v1.2.12 inventory item T3 (at 300 s: 615,742 distinct states, 307,578 on the queue, both growing).")
   (fix "tla-readsbyroom-parallel-rule" :release "v1.2.12" :status "planned"
    :title "ReadsByRoom models the interim parallel ask rule, with a reversed witness for asking while broken"
    :text "Models only, no product change. tla/ReadsByRoom.tla gains a Rule constant (\"sequential\" or \"parallel\"): parallel is the code's interim rule of 2026-10-06 (ReadsWanted returns 0 once a read is broken, else the reads still needed minus the live reads, asked or ok), and sequential stays the design of 2026-10-04 in its own configuration. ReadsPerLanding is restated as Need times (broken attempts plus one) under parallel (a pro card broken once reads four, where sequential read three); OneAtATime and OneBroken hold only under sequential, and the new NoAskWhileBroken (the ask places no read once a read found it broken) holds under both. Five reversed witnesses: the pair asked together breaks OneAtATime and OneBroken, readers asked whatever their room break WidthRespected, the rework's first read of any reader breaks FinderFirst (a three-reader instance, so a card has a reader to spare), and Broken = \"askbroken\" asks a read while one is broken and breaks NoAskWhileBroken. The refmodel Ask comment (actions.go) now says the parallel rule; readers.go's askPicks comment (line 637) still says sequential and is left for the next card."
    :origin "this card; v1.2.12 inventory item T36, drift read at nova-sprint main 00c54ba2e 2026-10-11")
   (fix "tla-attempts-and-epochs" :release "v1.2.12" :status "planned"
    :title "AttemptsEpochs models the sprint's epoch and clear, with a reversed witness for a late writer"
    :text "Models only, no product change. tla/AttemptsEpochs.tla is new: the sprint's epoch n, the restore marker the epoch hash's restore field names, the per-epoch streams, readers and members tables, a step that pins an epoch, clear in its steps (the CAS advance on n, its settle, the members carried and the new epoch's streams and readers blank), the pin's restore (copying the old shape into a new epoch that still owes it), a late writer of the old epoch refused as stale, and a second concurrent clear whose advance lost the CAS writes nothing. It proves OldEpochWritesNothing, NewEpochBlank (but the members), OldEpochReadable and EpochMonotone; one reversed witness, Broken = \"noepochcheck\", drops the write's epoch check (engine.go:566) and fails OldEpochWritesNothing. store/epoch.go and store/clear.go cite the module. The crash cut between AdvanceEpoch and SettleEpoch, which leaves the restore set so the next pin copies the old streams and readers (against section 13), is a v1.2.14 candidate named in the module and not modelled here."
    :origin "this card; v1.2.12 inventory item T31 and section 5, drift read at nova-sprint main 00c54ba2e 2026-10-11")
   (fix "tla-cardisa-lands-only-by-the-table" :release "v1.2.12" :status "planned"
    :title "CardISA lands a card only by the table: merging, or a released wait card"
    :text "Models only, no product change. tla/CardISA.tla narrows IsaLandEvent to the release that lands a
     wait card (a waiting wait card with released, as internal/sprint/lifecycle.go unlawful refuses
     waiting -> landed unless the plan is releasing and the card is a sentinel), so a wait card is landed
     by its release and never dealt; Wait resolves a non-wait card alone, and the inherited
     drop-to-landed (Verdict) and done-already-to-landed (EndWorkDone) branches are replaced by
     IsaVerdict and IsaEndWorkDone without them, neither a Moves row. The new action property
     LandsOnlyByTable (a card lands from merging, or from waiting a wait card) is checked by
     MCCardISA.cfg, which now also checks IsaSafety; WaitDispatchesOnlyWhenOperandHolds is restated as
     a wait card lands only when its operand holds. A fourth reversed witness
     MCCardISABrokenLandAnywhere (Broken = \"landanywhere\") restores the old landing set and breaks
     LandsOnlyByTable; the three earlier witnesses still break the property their rows name."
    :origin "this card; v1.2.12 inventory item T5, drift read at nova-sprint main 00c54ba2e 2026-10-11")
   (fix "tla-friendemptyrun-every-path-to-a-friend" :release "v1.2.12" :status "planned"
    :title "FriendEmptyRun models the four friend moves, the brief-defect return and the cap's working card"
    :text "Models only, no product change. tla/FriendEmptyRun.tla gains FriendMove, one action for the four paths that read cardLeft and move a ready attempt to a friend's lane (friendLevel, friendUnstartedLevel, reclaimUnit, friendCouldTake), reading wleft and left as they do; Rework gains the brief-defect early return, the failed rule's other early return beside the brief's bound, which writes the friend at any attempt; and CapDeal places the card working on her row, as AttemptCapDeal does, not queued. NeverBackToTheEmptyLane still holds, and a reversed witness (MCFriendEmptyRunBrokenLevelLeft) reads only the work card's list and breaks it. ruleHarness and AttemptCapDeal cite the module; friend_start.go and friend_deal.go citations are their own cards."
    :origin "this card, tla-friendemptyrun-every-path-to-a-friend-2.w1, 2026-10-11: v1.2.12 inventory item T36 (\"FriendEmptyRun: uncited by any Go file\"); drift read at nova-sprint main 00c54ba2e")
   (fix "tla-sprintrules-judgment-rules-part" :release "v1.2.12" :status "planned"
    :title "SprintRules Part judgments models the three judgment rules, with one reversed witness"
    :text "Models only, no product change. tla/SprintRules.tla gains Part \"judgments\" (internal/sprint judgment_rules.go ruleFriendTake, ruleReadLate, ruleHoldNeed): friend-take takes back a friend's card she has not started and the deal places it again never on her; read-late asks another reader once an attempt, the second late read of the attempt a mind's; hold-need waits for its HOLD's card and reworks the primary once it lands, a dropped card a mind's. Each rule's guard and answer is one action; a judgment is answered at most once and an answered judgment is never reopened by the same rule, stated as FriendKeepsStarted, AskedOnceAnAttempt and HoldNeedReworksOnlyLanded. One reversed witness, Broken = \"takestarted\" (MCSprintRulesJudgmentsBrokenTakeStarted), drops the friendStarted guard and takes back a card the friend started, breaking FriendKeepsStarted. judgment_rules.go cites the module in a comment line only; no other Go change."
    :origin "this card; v1.2.12 inventory item T36 (SprintRules last of four, the judgments the tick raises and the answers to them), drift read at nova-sprint main 00c54ba2e, 2026-10-11")
   (fix "tla-readertiers-empty-cell-reads-flash-only" :release "v1.2.12" :status "planned"
    :title "ReaderTiers reads an empty cell as flash only while routes exist, on four tiers, reads asked together"
    :text "Models only, no product change. tla/ReaderTiers.tla now reads an empty tiers cell as the code does (read_route.go readerServesTier): a fleet reader whose cell is empty reads flash alone while the store holds routes (a RoutesExist constant), every tier with none; a friend's reader (ownModelReader) reads its own model's tiers, an empty cell every tier, route or none; and a held friend's reader reads none. The tier set is four (flash, pro, heavy, frontier); Need is the value ReadsNeededIn returns (one for a flash card, two for a stronger tier, set by the instance); and Ask places a card's reads together, Need[c] of them, one each on a different free reader. Eligible no longer filters on the tier, so NoReadOutsideTier is a real check: the reversed witness MCReaderTiersBrokenEmptyReadsAll.cfg (BugEmptyReadsAll) has a fleet empty cell read every tier while routes exist and fails NoReadOutsideTier, and MCReaderTiersBrokenHeld.cfg (BugIgnoreHeld) has the held friend's reader read every tier unheld and fails NoReadToAHeldReader. readers.go freeReaders' doc comment now says an empty cell reads flash only while routes exist; readerServesTier, freeReaders, ReadsWanted and enoughReadersUp cite the module."
    :origin "this card, tla-readertiers-empty-cell-reads-flash-only.w5.g8.e17, 2026-10-11: v1.2.12 inventory item T2 (its two hand-written RUNS rows replaced by tlc-runs-inside-the-job-wall); drift read at nova-sprint main 00c54ba2e")
   (fix "tla-serverlanes-reader-leases-and-renew" :release "v1.2.12" :status "planned"
    :title "ServerLanes models the reader-card lease and its renew"
    :text "The lease moves off the read lane's callers and onto a reader card begun by read --begin, a line verb; a Renew action on a served queue renews it (errors ignored), a lease not renewed expires, and restart keeps the live and takes back the lapsed. The reversed witness norenew fails LiveLeaseNeverTakenBack."
    :origin "this card; tmp/v1.2.12-inventory-2026-10-11.md section 8 item T24")
   (fix "nova-sprint-ci-runs-tlc-on-hosted-runners" :release "v1.2.12" :status "planned"
    :title "The TLC cases run on GitHub-hosted runners from a pinned-jar workflow"
    :text "CI only, no product change. The 434 TLC cases in tla/ ran only when someone ran them by hand on a bench; tla/tla2tools.sha256 now pins the jar every tla/RUNS.tsv record names (sha256 936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88, the tlaplus v1.7.4 release), and .github/workflows/tlc.yml runs the models on GitHub-hosted ubuntu-latest runners -- nova-sprint is public, so never self-hosted -- nightly by one cron, on workflow_dispatch, and on a pull request touching tla/**, pkg/tlc/** or tools/tlacheck/**. A groups job runs make tlc-test and prints the groups to run (every required group on schedule and dispatch via tlacheck groups --root ., the stale ones on a pull request via tlacheck groups --root . --stale; an empty list ends the workflow green); a models job depends on it, runs a four-wide fail-fast-false matrix over those groups, uses the runner image's own JDK 21 (no setup-java action), downloads the jar from the tlaplus release and refuses it, exit 2 naming both digests, unless sha256sum matches tla/tla2tools.sha256, then runs make tlc TLC_BUDGET=110 and uploads the run directory as a 14-day artifact. It never writes tla/RUNS.tsv into the repository; records still land through cards. New test internal/ci/tlc_workflow_test.go pins the hosted runner, the five-minute timeout, the digest check before make tlc and the 110-second budget. The workflow is unproven until its first run on the forge."
    :origin "this card, nova-sprint-ci-runs-tlc-on-hosted-runners-after-the-wall.w1, 2026-10-11: v1.2.12 inventory item T30 and note 3; checked at nova-sprint main 00c54ba2e")
   (fix "tla-roadmapentry-note-and-four-write-save" :release "v1.2.12" :status "planned"
    :title "RoadmapEntry models Note and Save's four writes, and scheduled notes' unchecked :release"
    :text "Models only, no product change. tla/RoadmapEntry.tla gains a Note action (an open entry's title and text change in place, the place and the state unchanged) and models Save's four writes in the code's order -- destination data, destination page, source data, source page -- with a crash possible between any two and NoLoss still held. ChildLinked is restated over what decode() checks, with a Scheduled constant of notes whose :release is read and not checked: the fixes' items are checked (fixes.go:130-131) but the scheduled note's is not (roadmap.go:278). A reversed witness MCRoadmapEntryBrokenPageFirst writes each file's page before its data and fails the new PageNotAhead invariant; MCRoadmapEntryBrokenSourceFirst still fails NoLoss. Note on a done fix or a shipped release is not modelled: the code's Note (roadmap.go:546) edits both with no open or release guard, against the package doc's 'a release whose status is shipped is frozen' (roadmap.go:20-21), a v1.2.14 candidate named in the report."
    :origin "this card, tla-roadmapentry-note-and-four-write-save.w6, 2026-10-11: v1.2.12 inventory item T23; drift read at nova-sprint main 00c54ba2e")
   (fix "tla-sprint-tables-fleet-take-and-finish" :release "v1.2.12" :status "planned"
    :title "tla/SprintTables.tla adds the fleet side: member up or down and Start, Take, Finish, FleetDown, FleetUp"
    :text "Models only, no product change. tla/SprintTables.tla gains the fleet side of the work table from
     internal/sprint/refmodel/actions.go: a member is up or down, and a working primary's work card sits on one
     member's row in a place none, ready (dealt, not taken), working (taken), finished or withdrawn. Start deals a
     ready primary's work card to an up member's row (a withdrawn card is dealt again); Take moves a dealt card to
     working on an up member; Finish finishes a working card and moves its primary to review; FleetDown takes a
     member down and deals its ready and working cards to the other up members or, with no member up, withdraws
     them and returns their primaries to ready; FleetUp brings a member up. The primary's moves stay rows of the
     lifecycle's Moves table, so MovesOnlyByTable still holds, and the new WorkingOnAnUpMember (a working card's
     member is up) holds too. Three reversed witnesses: MCSprintTablesBrokenInsert (an add that moves ready ->
     waiting without inserting), MCSprintTablesBrokenNeedless (a resolve that drops the needs check) and
     MCSprintTablesBrokenDownkeeps (a fleet down that keeps a working card on the member that went down, breaking
     WorkingOnAnUpMember). The round-robin order of the deal (NextMember) and the levelling of the ready queues
     (T4, the Level action) are choices, not safety rules, and are left out. The refmodel's pending-operation
     fence (D1: Free) is left out (the module has no pending operation), and F1, F3 and F4 are left to the reads
     and accept, merge and return, and crash and repair cards."
     :origin "card tla-sprint-tables-fleet-take-and-finish.w3, 2026-10-11: v1.2.12 inventory item T4, the second of five cards; drift read at nova-sprint main 00c54ba2e")
   (fix "tla-sprint-tables-reads-and-accept" :release "v1.2.12" :status "planned"
    :title "The SprintTables model holds the reads and the accept"
    :text "Models only, no product change. tla/SprintTables.tla adds the read actions and the accept to the
     sprint tables model, written from internal/sprint/refmodel/actions.go (Ask, AskAnother, ReadStart, Read,
     Accept) with internal/sprint/readers.go ReadsNeededIn and internal/sprint/lifecycle.go Moves as the
     reference: a card in review asks its reads of readers other than its worker, a read begins and returns ok or
     broken, and accept moves it to merging when it has the reads its tier needs (one for a flash card, two for a
     pro card; ReadsNeededIn) and no broken read stands at its head (steps_review.go hasBrokenAtHead). Invariants
     AcceptHeld (no primary enters merging short of its reads), NeverAcceptBroken (no primary enters merging with
     a broken read at its head), NeverReadByItsWorker and MovesOnlyByTable, with the reversed witnesses
     MCSprintTablesBrokenShortAccept (an accept with one read fewer fails AcceptHeld) and
     MCSprintTablesBrokenAcceptBroken (an accept with a broken read at its head fails NeverAcceptBroken). Drift
     kept out and named in the report: refmodel's Accept demands two readers where the code allows one for flash
     and lacks the read-broken guard (v1.2.13 cleanups), and docs/SPEC-SPRINT.md section 3 calls accept mechanical
     (the tick) where lifecycle.go:57 names it Coordinator, so the module models the code."
    :origin "this card; v1.2.12 inventory item T4, the third of five cards")
   (fix "tla-noroom-admission-by-probe" :release "v1.2.12" :status "planned"
    :title "NoRoom models the probe gate and the read return's taker"
    :text "Models only, no product change. tla/NoRoom.tla gains the probe gate: a member is known (accepted into the fleet) only on a passed probe (Accept, the caller's own word, trusted as the code does), and both the deal (Dealable) and the reader's ask (Off) read it, so no deal goes to a member the sprint does not know (NeverDealtUnknown) and a reader whose machine is not a fleet member belongs to no machine and is asked like any reader (steps_work.go fleetStepPlan, drain.go ReaderOwner). ReadReturn is guarded on a taker: a read asked of a held or draining machine's reader is taken back only when another reader can take it (readers.go sweepReads), so it stays otherwise, and ReadsReturn is restated under that guard with NeverReturnedWithoutATaker (a read is never taken back while no other reader can take it). Two reversed witnesses: Broken = \"probeblind\" deals to an unknown member (NeverDealtUnknown) and Broken = \"notaker\" takes a read back with no other reader (NeverReturnedWithoutATaker); the eight existing witnesses keep their results, and the noroom TLC records are refreshed."
    :origin "this card, tla-noroom-admission-by-probe.w3, 2026-10-11: v1.2.12 inventory item T28 (three drain tests went red when the probe gate and the drain landed apart, and no model holds both); drift read at nova-sprint main 00c54ba2e")
   (fix "tla-seathook-live-window-and-server-start" :release "v1.2.12" :status "planned"
    :title "The SeatHook model holds the Live window and the server's start"
    :text "Models only, no product change. tla/SeatHook.tla adds the clock (now, Tick) and the record's Live: Prove sets it from the proof time plus HookEvery + HookAnswerBound, the look (Rechallenge) is delayed by TLC at or after HookEvery and sends the re-challenge without moving Live (ChallengeLive moves it to the re-challenge's own bound), and the gate refuses when Live has lapsed even with a challenge inside its bound, so it can refuse up to one look period early. It adds ServerStart, the next server's start rewriting a dead server's hooked record gone. Eight reversed witnesses: the seven before plus MCSeatHookBrokenNoStartRewrite (\"nostartrewrite\" leaves the record hooked at a server start, and RecordTrue fails). The header and tla/README.md say the gate can refuse up to one look period early, which internal/sprint/hook.go's doc does not (a doc fix named in the report, not made here: the code is the reference)."
    :origin "this card; v1.2.12 inventory 2026-10-11 item T16, section 9 rows D6 and D7; drift read at nova-sprint main 00c54ba2e")
   (fix "tla-readcards-first-cases" :release "v1.2.12" :status "planned"
    :title "tla/ReadCards.tla checked against the read cards code, first cases"
    :text "Tests and models only, no product change. tla/ReadCards.tla, which had no cfg and no CASES row and had never been checked, now does what the code does: MaxGen 2 (MaxReadGen: the plain id and .g1 and .g2), the needed count 0, 1 or 2 (ReadsNeededIn: the setting or the tier's rule), and the merging-short-of-reads return (ShortReadsBack). MCReadCards.cfg holds TypeOK, OneOpenPerReader, AtMostNeedStanding, NeverTheWorker, NeverTwiceAfterSpent, MergingOnlyOnOks and NoOpenReadOfAnOldAttempt; the reversed witness MCReadCardsBrokenIgnoreSpent.cfg (BugIgnoreSpent = TRUE) breaks NeverTwiceAfterSpent. Both cases run on a bench and pass."
    :origin "this card; v1.2.12 inventory item T15")
   (fix "tla-holds-no-stall-liveness" :release "v1.2.12" :status "planned"
    :title "The holds module models the no-stall rule and a card's liveness"
    :text "Models only, no product change. tla/Holds.tla adds the no-stall rule (internal/sprint/held.go Unheld): a stop raises an open judgment on the stream (open), which the coordinator's answer (SayDid then Resume) or the tick's cross resume (TickResume) closes. NoSilentStall says every stopped stream has an open judgment or is the coordinator's hold (holder c, held.go judgment), and Progress, under fairness of the coordinator's answers and the machine's steps, says a card the coordinator does not hold eventually lands, is held, or is stopped with an open judgment (MCHoldsLive, checked in its own liveness config). The reversed witness MCHoldsBrokenSilentStop stops a stream with no judgment and fails NoSilentStall. The held card and member and the sentinel are still outside the module."
    :origin "this card; tmp/v1.2.12-inventory-2026-10-11.md section 8 item T31")
   (fix "tla-holds-stream-stop-resume-reopen" :release "v1.2.12" :status "planned"
    :title "The holds module models a stream's hold, stop, resume and reopen"
    :text "Models only, no product change. tla/Holds.tla holds a stream control card with held and held_reason and a state among waiting, merging, stopped, landed and working, and a stop's cause cross, conflict, red or rejected, with a second stream whose card o the cross need names. HoldStream and Unhold withdraw a held stream's ready cards (its working ones with --return); the deal refuses a held stream while a stopped stream still deals its ready cards; Stop (cross and red) stops the stream and a cross stop sticks the merge card on need_card o; a refusal of the card's own head stops nothing; Resume moves the stream again (a cross stop waits until o has landed, a red stop needs --did); TickResume does the cross resume on its own; and ReopenAdd reopens a stream past a landed stop. Invariants HeldStreamDealsNothing, CrossResumesAfterNeed and OwnRefusalNeverStops, with the reversed witness MCHoldsBrokenResumeEarly (Resume without the needed-card check) failing CrossResumesAfterNeed. The held card and member and the sentinel are the holds machine's first card."
    :origin "this card; v1.2.12 inventory item T31, the second card of the holds machine")
    (fix "tla-holds-sentinels-stops" :release "v1.2.12" :status "planned"
     :title "Holds models a held card, a sentinel and its release"
     :text "Models only, no product change. tla/Holds.tla adds the held card, the sentinel and its release to the holds machine (docs/SPEC-SPRINT.md section 16; internal/sprint held.go, steps_sentinel.go, sentinel.go): one stream whose line is [a, S, b] (S a plain or a cut sentinel) and one held card h, with the actions Resolve, Release, CutRelease, SentinelSet and StreamHold, running beside the stream-hold and no-stall machines already in the module. It proves HeldMovesOnlyByRelease (a held primary stays waiting until the coordinator's release), OnlyCoordinatorReleases (no worker releases), CutNeverWaives (the machine's self-release waives nothing) and SentinelLandsOnlyWhenWaitsUnderWay (a sentinel lands only when each of its waits is landed or in flight), with two reversed witnesses: resolveheld drops the held check at steps_work.go:922 and fails HeldMovesOnlyByRelease, and workerreleases drops the coordinator check at steps_sentinel.go:318 and fails OnlyCoordinatorReleases. DirtyTick's pump still auto-lands sentinels (stale); a held member stays outside the module."
     :origin "this card, tla-holds-sentinels-stops, 2026-10-11: v1.2.12 inventory item T31 and section 5 (COVERAGE row holds-sentinels-stops)")
   (fix "tla-readyover-causes-and-bound" :release "v1.2.12" :status "planned"
    :title "ReadyOver raises only past ReadyMax, one judgment per cause, each held card its own"
    :text "Models only, no product change. tla/ReadyOver.tla was read against internal/sprint/ready_over.go and drifted in four places, each brought to the code. ready is split from past (a card is ready-over only after it sits past ReadyMax), each held card and each bad brief is its own cause (the card's id is in the sentence), and the causes readyWhy names that the model lacked are added: a held stream, friends alone, no launcher and no free width, plus a timed rest (until its time) beside paid and woken. OnePerCause and ClearsWhenDealt still hold. A new invariant RaisedOnlyPastReadyMax (a raise writes a judgment only for a card past the bound) is checked by the reversed witness MCReadyOverBrokenNobound (Broken \"nobound\"): a raise before the bound fails it. ready_over.go gains one comment line citing the module."
    :origin "card tla-readyover-causes-and-bound, 2026-10-11: v1.2.12 inventory item T36, drift read at nova-sprint main 00c54ba2e")
   (fix "tla-routeindex-first-and-exclusions" :release "v1.2.12" :status "planned"
    :title "RouteIndex models the first flag, the per-member harness draw, the FieldRoutes exclusion, a resting route and a read's avoid and tier collapse"
    :text "Models only, no product change. tla/RouteIndex.tla gains a First constant and preferFirst: a route whose first flag is set is drawn before the others of its tier, checked by the new invariant FirstPreferred (a draw never passes over a drawable first-set entry), with the reversed witness MCRouteIndexBrokenNoFirst.cfg (Broken \"nofirst\", the walk that ignores first) and the control MCRouteIndexFirst.cfg. The deal walks past the member's own harness set, not one global set: a new memb variable records the member each card is dealt to, Deal and Redeal leave out MemberUnlaunch[m] for that member, and NeverUnlaunchable is restated per member (last.r notin MemberUnlaunch[last.m]); the harnessblind witness is kept. A new attempt leaves out the routes already in the primary's FieldRoutes: Deal's exclusion is drawn[c] together with the member's set, as Redeal's already was. A resting route (Rested) is walked past as an unserved one. A read leaves out the routes it returned on (Avoid[c]) and is drawn from its primary's collapsed tier (ReadTier: pro to flash, heavy to pro), with the control MCRouteIndexRead.cfg. routeIndexesOf's call in rebalance.go and the route read in routetable.go gain one comment line each citing the module. Still outside the module: a provider at its concurrency budget is not skipped (the deal draws one anyway), and the coordinator's fallback that deals a rework on a full provider."
    :origin "this card; v1.2.12 inventory item T36, drift read at nova-sprint main 00c54ba2e 2026-10-11")
   (fix "tla-server-switch-rollback-window" :release "v1.2.12" :status "planned"
    :title "ServerSwitch models the server switch and its rollback window, with two reversed witnesses"
    :text "Models only, no product change. tla/ServerSwitch.tla models the server switch (internal/sprint/server_switch.go ServerSwitch, ServerRollback, CheckRollbackOnLandFailure): at most two switches, a rollback window of two, a clock to four, a land that is ok, refused or failed, and a crash between the copy and the state write. ServerSwitch backs the target up to <target>.prev (only when a target exists) and copies the candidate over the target, then writes <target>.switch.json when rollback is on; a land pass fault (exit 2) in the window rolls the previous binary back to the target and removes the state, past the deadline the state is removed and the switch is permanent, and a refusal (exit 1) never rolls back. Three invariants: TargetIsAKnownBinary (the target is always a known binary or absent), RollbackOnlyInWindow, and AtMostOneAutoRollbackPerSwitch; two reversed witnesses: nodeadline skips the deadline check and fails RollbackOnlyInWindow, keepstate keeps the state after a rollback and fails AtMostOneAutoRollbackPerSwitch. Two facts of the code are modelled and named: a second switch inside the window overwrites <target>.prev with the first candidate, and a switch with no prior target leaves a state whose Previous file is missing, so every failure inside the window errors."
    :origin "this card, tla-server-switch-rollback-window.w5, 2026-10-11: v1.2.12 inventory items T31 and section 5, and D11; survey at nova-sprint main 00c54ba2e 2026-10-11")
   (fix "tla-dirtytickread-start-and-run-sequence-2" :release "v1.2.12" :status "planned"
    :title "DirtyTickRead models Start, the run sequence, and the stale-run refusal"
    :text "Models only, no product change. tla/DirtyTickRead.tla gains a runseq variable and a Start action as the code has them: Stop and Start each take the fence (the generation steps), Start bumps runseq (machine_transition.go's RunSeq), and a part records the runseq it began under (tick.go's TickRunSeq) so its Commit requires it to be the current one (engine.go's StaleRun refusal). The new invariant NoStaleRunCommit holds that no part commits under a runseq other than the one current at its commit, and the reversed witness MCDirtyTickReadBrokenRunSeq (Broken \"runseq\" drops the runseq check from Commit) violates it; the four existing witnesses keep the property their rows name. The small instances drop MaxParts to two and add MaxStarts = 1 so each finishes in budget."
    :origin "this card, tla-dirtytickread-start-and-run-sequence-2.w4, 2026-10-11: v1.2.12 inventory item T9; drift read at nova-sprint main 00c54ba2e 2026-10-11")
   (fix "reseed-from-nova-tools" :release "v1.2.3" :status "shipped"
   :title "The sprint tools re-seeded from nova-tools"
   :text "The three commands, their packages, models and docs, at nova-tools dev, under the re-seed recipe."
   :origin "the split, layer L0; PR #28")
  (fix "lua-digest-restore" :release "v1.2.4" :status "shipped"
   :title "Six Lua comment lines restored"
   :text "The function library's digest is its v1.2.2 value again, so the adopt's shadow tick accepts it."
   :origin "PR #33, head 1f403558d")
  (fix "adopt-carries-a-lua-change" :release "v1.2.7" :status "shipped"
   :title "The adopt can carry a change of the store's function library"
   :text "libraryMatches judges the library by its code, not its bytes: luaCode keeps each string (quoted
    or long) and each token, and turns every comment and run of blank space outside a string into one
    space, so a library that differs only in comments or blank space matches (v1.2.3's adopt refused the
    store over six comment lines). The shadow tick's pre-window check (libraryShadow) tolerates a library
    whose code differs, saying \"library differs: loaded in the window\"; the post-load check
    (libraryMatches) still refuses a store whose library is not the new build's, so a refusal restores
    the old library. TLA+ model Adopt, property RefusedLeavesOld, with the reversed witness
    MCAdoptBrokenKeepNew."
   :origin "the seat ledger v1.2.4-held-2026-10-10.md (a v1.3 card moved to v1.2.7); this pull request")
  (fix "roadmap-and-fixes-as-data" :release "v1.2.6" :status "shipped"
   :title "ROADMAP.md and FIXES.md generated from s-expression data"
   :text "docs/roadmap.sexp and docs/fixes.sexp are the data; `make roadmap` writes both pages, and a test
    fails when a page differs from what its data renders. The roadmap machinery the v1.2.3 re-seed left
    out comes back."
   :origin "this pull request; the machinery from commit b559389")

  (fix "no-land-without-reads" :release "v1.2.6" :status "shipped"
   :title "Nothing merges or lands short of its reads"
   :text "A merging card needs the ok reads a card in review needs (the reads setting, else its tier's rule).
    One short of them, the setting raised or its tier pinned after its accept, goes back to review on
    the next tick for the read it lacks (never one a lander marked landing before its push); the lander lands none, naming
    it and its count, checked again and marked landing in one step just before the push. The accept records
    the count it ran on.
    tla/Land.tla NoLandWithoutReads and PushedNeverSentBack, with the reversed witnesses
    MCLandBrokenNoCheckReads and MCLandBrokenBounceMarked."
   :origin "the Studio sprint, epoch 16, 2026-10-10: rule 6 raised on cards merging with one read of two, and two landed on one read")

  (fix "the-coordinator-hooks-in" :release "v1.2.6" :status "shipped"
   :title "The coordinator must hook in, and prove it, before any command runs"
   :text "nova-sprint hook holds the seat's subscription on the server, which streams every judgment and
    push with an id and a challenge; the hook counts once the session answers the challenge over the same
    connection, and again every 10 minutes. Every command run as the coordinator fails with exactly 'You
    must hook in first: run nova-sprint hook' until then, and names a push left unacknowledged past 10
    minutes; there is no override. unhook records the seat away, a drop records it gone at once, and each
    is pushed to the owner. seat watch, the PROOF files and seat pong are gone. tla/SeatHook.tla:
    CommandSafe, AckedOrBlocking, ProofMeansRead, ProvenIsSubscribed, RecordTrue, KeepsHook, HookStays,
    with seven reversed witnesses. Supersedes the v1.2.8 card the-coordinator-must-hook-in."
   :origin "the owner, 2026-10-10 ~22:55Z: about 1,200 pushes went unread for hours while the seat answered PROOF files by hand")

  (fix "provider-key-refusal-rests-until-woken" :release "v1.2.6" :status "shipped"
   :title "A provider that refuses its key rests until woken, never for a time"
   :text "A route whose provider refuses the key rests until routes wake, never for a time, and the rest
    reason reads until woken rather than until paid: the providers table, the stop and the judgment all
    say woken. TLA+ model RouteRest, property AuthEndsOnlyWoken, with a reversed witness. Tests cover
    routes wake on a key rest."
   :origin "nova-tools PR #5574, head 0c6706f2d; the providers table's until-paid wording is v1.2.5 candidate 6")
  (fix "bare-merge-stream-refused" :release "v1.2.6" :status "shipped"
   :title "A bare merge --stream is refused on a real store"
   :text "Landings are recorded by name or by land; a bare merge --stream on a real store is refused."
   :origin "nova-tools PR #5565, head 52618fa02")
  (fix "pin-release-and-rule-answers" :release "v1.2.6" :status "shipped"
   :title "A hard pin past its bound is offered on; a rule answer is consumed by its attempt"
   :text "A hard pin past its bound is offered to others, a card cannot ask once it waits, and a rule
    answer is consumed by its attempt. The pin release clock comment is corrected: the hour runs from
    the stop's latest note, so a coordinator's ack or wait starts it again, and a test says so."
   :origin "nova-tools PR #5573, head daab75763, re-applied here")
  (fix "paths-widening-edits-card-in-place" :release "v1.2.6" :status "shipped"
   :title "A PATHS widening on a reader finding edits the card in place"
   :text "A reader finding that widens PATHS edits the card in place, never a twin. Three review notes are
    fixed before merge: the finding reader is unset only when the card holds it, the bound subtest
    reaches the attempt cap, and the friend card claim says a widen does not pin her next attempt."
   :origin "nova-tools PR #5570, head 22d72154d, re-applied here")
  (fix "stop-return-settles-its-debt" :release "v1.2.6" :status "shipped"
   :title "A stop-return settles its STOP debt, so the owner's row can be held before START"
   :text "A same-owner stop-return takes its lease off the machine's STOP debt under the operation fence,
    a replay too, so a returned card no longer pins its owner's row: hold and fleet down may move it
    while STOPPED, and START is not refused. A lease without its receipt on its own row stays owed.
    TLA+ model StopReturn, properties NoLiveChildAcrossStart and ReturnedFreesItsRow, with two
    reversed witnesses."
   :origin "the seat, 2026-10-10: hold alex and hold emma refused after their stop-returns")

  (fix "a-refusal-always-says-why" :release "v1.2.6" :status "shipped"
   :title "Every refusal says why, and what to run instead, in the text output"
   :text "A step's refused card prints its reason and its remedy in the default text output, as --json's
    refused array has carried them all along: the verb result printer appends what changes the card, by
    the lifecycle's moves (docs/SPEC-SPRINT.md section 3), when the step's reason names no remedy of its
    own and is about the card's state, so return on a working card no longer reads RETURN FAILED
    moved=0 refused=1 alone. A refusal that is not about the card's state stands on its reason: a read
    of a primary is refused no such card, its why naming the read card to call
    (<primary>.r<attempt>.<reader>), and takes no work-table remedy. A test walks every verb's refusals
    and fails on any refusal printed without a reason, or a card's without a remedy."
   :origin "dogfood 2026-10-10: BRIEF FAILED and RETURN FAILED with no reason in text; the seat cold
    read of 2026-10-10 found that remedy appended to a read's no-such-card refusal")

  (fix "deal-draws-launchable-route" :release "v1.2.6" :status "in-progress"
   :title "A deal draws only a route its member can launch"
   :text "The deal, the redeal, the escalation and the rebalance onto a machine draw only routes whose
    harness the member can launch, and a move that keeps a card's route never goes to a member that
    cannot launch it (tla/RouteIndex.tla, NeverUnlaunchable). DEPLOY STEP for the final adopt: a member
    names no headless harness (claude, codex, grok) until `nova-sprint fleet up <member> --harnesses
    <h,...>` runs, so it is dealt opencode routes only; after the adopt, run it for each member with
    the headless harnesses on its PATH, or the deal draws no headless route for it."
   :origin "nova-tools PR #5576, head 065bf6b73, re-applied in nova-sprint")
  (fix "lander-bench-fault-and-bisect" :release "v1.2.6" :status "in-progress"
   :title "The lander blames no head for a bench fault and bisects a red batch"
   :text "A bench fault blames no head, a red batch is bisected, and landings go as batches are built. The
    bench fault match reads only the bench's own lines, never a test's output, so a real failure that
    prints a disk string is still blamed."
   :origin "nova-sprint PR #48, re-applying nova-tools PR #5579 (head 5bb7de4f2) with v1.2.5 candidate 4")
  (fix "check-waived-read-and-where-tiers" :release "v1.2.6" :status "shipped"
   :title "check honours a waived second read; where shows tiers once"
   :text "check judges an accept made before the second-read count by the count then; where shows a tier
    once and hides an expired until. A stale health seen time beside a live status is also fixed."
   :origin "nova-tools PR #5584, head 881df6ff")
  (fix "where-json-landed-series-fast" :release "v1.2.6" :status "shipped"
   :title "where --json reads no log line for its landed series"
   :text "The tick counts landings into the where record, so where --json answers in well under a second
    (8.9 to 12.8 s before, 0.4 to 0.7 s after, measured on a copy of the live store); released sentinels
    are not counted as landings. The public dashboard mirror's server.py (internal/sprintdash/stopgap)
    is now versioned here: it stamps fetchedAt with the snapshot's own time and adds dataAgeSeconds.
    AFTER THE DEPLOY, in the final adopt: rebuild the dashboard's nova-sprint-int2
    (~/nova-bench/dashboard/bin/nova-sprint-int2) from this release, copy
    internal/sprintdash/stopgap/server.py over ~/nova-bench/dashboard/server.py, and restart the
    public mirror unit."
   :origin "nova-tools PR #5588, head 08f6af5a8")

  (fix "operator-wording-bare-merge-and-key-rest" :release "v1.2.14" :status "planned"
   :title "What an operator reads matches the bare-merge refusal and the key rest"
   :text "A late-merge judgment prints land for a real store and the bare merge only as the twin's form; the
    CLI, first-lap and test docs mark the bare merge twin-only (play is a twin tool); the spec and comments
    say a key rest ends only by routes wake, never by funded or a payment, and a test shows a balance poll
    cannot end one."
   :origin "readers' notes on nova-sprint PR #49 and PR #50")
  (fix "hook-stays-live-while-its-rechallenge-is-in-bound" :release "v1.2.14" :status "planned"
   :title "A hook stays live while its re-challenge is in bound"
   :text "The record's Live was the proof's bound alone, so a seat answering in time was refused
    HookFirst between the proof's Live and a re-challenge's own bound whenever a slow look sent the
    challenge late. The server now moves Live to the re-challenge's own bound through the record's
    one writer; a write that fails keeps the earlier bound (the safe side) and says so on the
    server's line. Test: TestARechallengeKeepsTheHookLiveUntilItsOwnBound."
   :origin "v1.2.12 inventory 2026-10-11, section 9 row D6; code read at nova-sprint main 00c54ba")
  (fix "brief-check-runs-the-add-checks-without-a-write" :release "v1.2.14" :status "planned"
   :title "brief check runs every add check without a write"
   :text "An add's refusals arrived one class at a time, so a brief writer learned of the next only after
    fixing the last. The new read-only verb brief check runs every check add runs, in add's order, over
    every brief, and collects every finding: the id, the size bound, shared PATHS, the card lint, the WHO,
    base, TLA+ records, card checks, brief-base, paths-admit and sprint-base holds, then the add step's own
    plan on one read-only snapshot. It prints one CHECK line a finding and a BRIEF CHECK files=<n>
    findings=<m> line, --json prints the findings as one array, exits 0 with no finding, 1 with any, 2 for
    usage or an unreadable file, and 3 when the store does not answer and the store-free checks found
    nothing. Nothing is written and the brief decision is never asked. Tests: the twin store names two
    findings of three briefs in one call and leaves its keys equal, the clean brief is exit 0, and a brief
    over the size bound and a lint finding in one file are both said."
   :origin "tmp/next-sprint-cards/TOOLING-FROM-THE-SEAT-2026-10-11.md item 2; the owner, 2026-10-11")
  (fix "add-partial-admits-what-passes" :release "v1.2.14" :status "planned"
   :title "add --partial holds back what fails and admits the rest"
   :text "add was all or none: one failing brief refused the whole call, so the coordinator looped add, read
    the refusals, moved the refused files aside and added again. add --partial, with --brief-dir or a
    repeated --brief-file only, runs brief check's collector over every brief, holds back every brief with a
    finding and every card that needs a held card through its Needs: or DEPENDS-ON: line or --needs,
    transitively, then runs the add step's own plan over the rest and holds back each card its Refused list
    names with the cards that need it, planning the write again without them; the rounds are bounded by the
    number of cards, and nothing is written for a held card. The rest are admitted in one store write, the
    brief decision asked for them only. It prints one HELD <id>: <its first finding or refusal> or HELD
    <id>: needs <other>, held back line per held card, then the add's closing line, and exits 0 when every
    card was admitted, 1 when some were held and the rest written, and 2 when none could be admitted or for
    usage. --partial is refused with ids, --count, --sentinel or --land-protected, since a marked first add
    is one step. Without --partial add is all or none as before. Tests: five briefs, b2 with a PRIORITY add
    refuses and b4 DEPENDS-ON b2, admit b1, b3 and b5, hold b2 and b4 with their two lines, exit 1, and
    leave exactly three new cards; a brief the plan refuses at the write is held with its refusal and the
    others land; with every brief clean the exit is 0, as add without the flag."
   :origin "this card; tmp/next-sprint-cards/TOOLING-FROM-THE-SEAT-2026-10-11.md item 1 (the owner, 2026-10-11: \"Whatever you are doing manually now, consider adding tool support for it to nova sprint.\")")
  (fix "empty-run-harness-fault" :release "v1.2.6" :status "in-progress"
   :title "An empty run is its own harness fault"
   :text "An empty run is classed as a harness fault, and a rework never returns to the friend AI whose
    lane ran it empty, with a TLA+ model and witness. The failed rule writes her on both its early
    returns too (the brief's bound and a brief defect), so the attempt that hits the cap after her
    empty run names her even when it is left to a mind; the attempt cap's deal acts only on a
    machine's card. Test: TestTheFailedRuleCarriesTheEmptyRunFriendOnItsEarlyReturns (reversed: a
    cost-line fault leaves no one)."
   :origin "nova-sprint #45; reader G BLOCK (the rule's early returns); nova-tools PR #5583, head e8fdaecc")
  (fix "done-at-base-is-brief-duplicate" :release "v1.2.6" :status "shipped"
   :title "A card already done at its base is a duplicate brief, not a worker failure"
   :text "Nothing to do because the base already contains the change is a brief defect, a duplicate of
    landed work, outside ok%, and raises a re-cut judgment."
   :origin "nova-tools PR #5585, head 0beb0508")

  (fix "lanes-ask-the-server-what-they-hold" :release "v1.2.6" :status "in-progress"
   :title "A worker's lane asks the server what it holds, and the server keeps it"
   :text "take --as <member> --lane <n> names the lane and nothing else: the server picks the epoch,
    answers the card the lane holds or gives it one (a friend's take is her start, so the tick no
    longer puts it back ready), and says it on a LANE line. Progress, finish and read verdicts name
    the lane, a lane unheard for ten minutes is gone and its card bounces back ready, a reader's
    lanes begin its reads, stop-return is served as a worker's verb, and the worker verb lists in
    serve.go and coordinator.go are unified (take, finish, progress, read, queue, stop-return,
    remind, fleet beat, friend beat, friend cards, lane take, lane give)."
   :origin "a one-shot friend host, 2026-10-10: took logged while the server showed the card ready, progress sent at epoch 0, every stop-return refused by the server; verbs misaligned in serve and coordinator")

  (fix "fleet-verbs-skip-member-under-disk-floor" :release "v1.2.6" :status "shipped"
   :title "Fleet verbs give a member under its disk floor nothing"
   :text "fleet up, down, hold and level, outside the tick, read the members' no-room words themselves,
    so a manual level, an up's level or a down's redeal gives a member under its disk floor nothing;
    the deal and read fix they follow already merged."
   :origin "nova-tools PR #5578, head fcdbaa441, re-applied in nova-sprint")

  (fix "draining-member-clears-no-room" :release "v1.2.6" :status "shipped"
   :title "A draining member does not keep a stale no-room word"
   :text "The room check runs before the drain return, so a draining member's beat carries this tick's
    no-room word, not a stale one (the member code in nova-tools, with its test, and the same lines in
    this repository's copy); the deal and read disk floor fix also gets its TLA+ reversed witness."
   :origin "v1.2.5 candidate list; nova-tools PR #5569 follow-up")

  (fix "bench-tree-standalone" :release "v1.2.6" :status "shipped"
   :title "A bench tree is a standalone clone, and one that is not is refused with its reason"
   :text "The mirror stage ends by checking that the staged tree's .git is a directory in the tree, and a
    .git file (a worktree pointer at a path of another machine) is refused naming the gitdir and whether
    it exists on the host. The tar copy refuses a worktree's .git file instead of carrying it over, so git
    no longer exits 128 on a fleet host with nothing saying why."
   :origin "lander fault 5579, space git exit 128 (held-PR ledger row 5579)")

  (fix "live-library-uses-store-login" :release "v1.2.6" :status "shipped"
   :title "live reads the store's function library through the seat login"
   :text "live runs nova-redis fn check with the recorded seat login's user and the password read in
    process, handing that password to the child's environment: on a store whose default user has NOAUTH
    the library state is now read (loaded, wanted, match), not UNKNOWN. The test observes the delivered
    password, so it is red until the child actually holds it."
   :origin "dogfood 2026-10-10: live LIBRARY state=UNKNOWN; card live-library-check-uses-the-store-loginb.w1")

  (fix "inbox-wait-survives-a-clear" :release "v1.2.14" :status "planned"
   :title "The seat's push loop goes on at the epoch a clear leaves it"
   :text "inbox --wait and --push re-read the sprint's active epoch when a clear advances it under the
    loop: the look at the inbox is never refused MEMBEREPOCH by the epoch the clear left, the wait
    ends at the clear as at a machine stop, the inbox reporting the clear's happened line, and the
    seat's push loop keeps running, pushing the new epoch's judgments. A step in flight still holds
    its epoch: a tick at a clear stops there."
   :origin "the seat ledger v1.2.4-held-2026-10-10.md, bug 6")

  (fix "frontier-read-judged-by-real-cause" :release "v1.2.6" :status "shipped"
   :title "A frontier read no one may take is judged by its real cause"
   :text "A frontier read the ask leaves waiting (the friend ask and the read-card ask alike) raises one
    judgment naming each such read and who is full, or why no one may take it. It is written once,
    rewritten in place when its facts change, never raised anew, and closed when the reads are asked."
   :origin "this pull request; frontier reader judgment")

  (fix "repair-says-what-it-changes" :release "v1.2.6" :status "shipped"
   :title "repair says what it changes, takes --dry-run, and returns a rule-6 merging card to review"
    :text "repair's help names its effect, it takes --dry-run that prints each move it would make (and leaves a
    pending operation open), and it refuses naming the remedy when it can do nothing. It learns check rule
    6: a card merging with ok reads from fewer different readers at its head than it needs goes back to
    review for its missing read, with the reason recorded, and never a card a lander marked landing (or
    pushed and not reported, or with a change queued for it): a landing once committed is completed, so
    repair skips it exactly as the tick's ShortReadsBack does."
   :origin "this pull request")

  (fix "clear-is-a-blank-slate" :release "v1.2.6" :status "shipped"
   :title "Clear starts a blank epoch and reports only live leases"
   :text "Clear lists every live card generation and owner row it refuses on; a returned or nonworking
    lease does not block. A successful clear advances to an empty epoch — no work, merge, stream or
    reader rows — but carries the fleet's members across: their rows, widths, status and holds survive
    (the machines are still there), while the old epoch's streams, cards and judgments reset."
   :origin "seat ledger v1.2.4-held-2026-10-10, bugs 2, 4 and 11")

  (fix "worker-brief-never-rewrites-history" :release "v1.2.6" :status "planned"
   :title "The worker brief says never amend, rebase or reset onto origin"
   :text "One line in the worker brief (the child rule no-rewrite-history, internal/fleetrules/child-rules.txt,
    byte for byte nova-tools' fleet/child-rules.txt, with a test pinning the digest in both repositories)
    stops children rewriting history, which caused the does-not-descend refusals."
   :origin "v1.2.5 candidate list")
  (fix "seat-proof-renews-while-the-seat-works" :release "v1.2.6" :status "in-progress"
   :title "A busy seat's own verb renews its stale push proof, and only its own"
   :text "A coordinator verb refused with PUSH DOWN for a proof past its 15m bound is answered by the
    seat's own session when the folder holds the check the push loop wrote but nothing answered: the verb
    answers the outstanding proof itself and records the pong (the way seat pong does), so a busy seat is
    never locked out; a seat with no outstanding check to answer, or truly unreachable, is still refused.
    The renewal is confined to the acting seat: a coordinator handover to a destination gates on the
    destination's proof with plain pushGate, so the acting session never answers the destination's
    outstanding check, and a destination whose session never answered is still refused."
   :origin "the seat, 2026-10-10: PUSH DOWN mid-add")

  (fix "working-is-a-live-run" :release "v1.2.6" :status "shipped"
   :title "Working means a live run, RUNNING or STOPPED; a returned card is no longer STOP-owned"
   :text "A worker's beat carries its live set (fleet beat and friend beat --live), and the tick returns a
    working card its row's live set has not named for 60 s to ready, while running and while stopped; the
    dashboard's working cell counts live runs. A stop-returned card is no longer STOP-owned, so a hold
    of its row is not refused before start, and a report refused for the stop is kept and taken after.
    Modelled in tla/LiveRuns.tla."
   :origin "the STOPPED sprint of 2026-10-10: 59 stale takes, and the hold of friend.zhi refused; nova-sprint PR #54")

  (fix "defer-cards-to-the-roadmap" :release "v1.2.6" :status "in-progress"
   :title "A verb defers waiting cards to the roadmap with their whole briefs"
   :text "nova-sprint defer writes the named waiting cards, or a stream or repository of them, into the
    roadmap data with each whole brief, and drops them from the store; --expect checks the count."
   :origin "this pull request")

  (fix "cut-sentinel-releases-itself" :release "v1.2.6" :status "shipped"
   :title "A cut sentinel releases itself on the tick, and pushes the seat that the release is ready to cut"
   :text "A sentinel that names its needs (a cut: a stream's reopen stop, a release sentinel) is released by
    the next tick's resolve when every card it needs has landed: released as the coordinator's release
    lands it, released_by the machine, what waited behind it to ready in the same step, its reached
    judgment closed, and one notice addressed to the seat that the release is ready to cut -- the publish
    itself stays a gated step with a cold read. A plain gate (a stop by position alone, no needs named)
    and a held sentinel keep the coordinator's release; the machine releases through no stream's hold and
    waives nothing in flight. The release lives in the resolve part, the duty the reference model names,
    so the tick's shape is the model's."
   :origin "the owner, 2026-10-10: what else is manual that should be automatic from the machine")

  (fix "promotion-mark-at-add" :release "v1.2.14" :status "planned"
   :title "add can mark a new stream for protected branches"
   :text "add --land-protected marks a new stream atomically with its first card, so a point-release
    stream can admit a card based on main or dev without a sentinel card or a follow-up stream set."
   :origin "the seat ledger bug 14")

   (fix "idle-cleared-says-working-only-when-it-works" :release "v1.2.14" :status "planned"
    :title "An idle episode that ends with no width or no work says why, never working again"
    :text "TickIdle ends an idle episode for four reasons and only one is a recovery. The fleet
     working at half its width again (a) keeps NIdleCleared, \"working again\"; no card waits (b),
     no width up -- every member down, held or drained (c) -- and the fleet's work switched off
     (d) push a new happened type NIdleEnded, \"the fleet's idle episode ended\", naming the cause:
     \"no card waits\", \"no width up: every member is down, held or drained (fleet up <member>
     --width <n>)\", or \"the fleet's work is switched off (set --fleet on)\". Both properties are
     cleared on every end as before. docs/SPEC-SPRINT.md section 14 says which end says what. The
     episode machine is unchanged (AlarmOncePerEpisode, ClearFollowsAlarm), so tla/SprintRules.tla,
     Part \"idle\", is not edited. TestAnIdleEpisodeEndedByNoWidthNeverSaysWorkingAgain (red on the
     base), TestAnIdleEpisodeEndedByWorkSaysWorkingAgain, TestAnIdleEpisodeEndedByNoWaitingCardSaysTheCause,
     TestAnIdleEpisodeEndedByTheFleetSwitchSaysTheCause."
    :origin "the seat's v1.2.14 audit, 2026-10-11 (TickIdle says \"working again\" when width is 0),
     checked against nova-sprint main 00c54ba; this card")

  (fix "stream-remove-removes-what-exists" :release "v1.2.6" :status "in-progress"
   :title "stream remove takes every stream that exists and names each that does not"
   :text "One name that is no row of the work or merge table no longer refuses the whole batch: stream
    remove takes off every named stream that may leave, reports each name that may not, and exits 1
    only when nothing was removed or a removal failed."
   :origin "the seat ledger v1.2.4-held-2026-10-10, bug 12; card stream-remove-removes-what-exists")

  (fix "friend-dealt-cards-return" :release "v1.2.6" :status "shipped"
   :title "A friend's dealt card not taken returns to the pool; a friend that has not taken is dealt no more"
   :text "A card dealt to a friend and not taken within the dealt bound (dealt-max) is returned to the
    pool and dealt to the next capable member, and a friend that has not taken a dealt card is dealt
    no more until she takes one (a fleet property marks her, her start clears it). The tick's friend
    deal returns the untaken cards (friendDealtReturn, no redeal of its bound spent) before the level,
    so the idle fleet machines take the cards the friends' daemons could not, instead of the friends
    first deal stranding every card on friends that never start. tla/FriendDealtReturn.tla
    DealtNoMoreHolds, with the reversed witness MCFriendDealtReturnBrokenGate."
   :origin "dogfood 2026-10-10: the deal starved the fleet behind friends that could not take")

  (fix "friend-working-start-clears-dealt-no-more" :release "v1.2.6" :status "shipped"
   :title "A friend's working-card start clears the dealt-no-more mark, and a friend who took one is not marked"
   :text "The re-admission half of the dealt-no-more mark covers the tick's working-card start: a card a
    finish's next moved to working that she is working (friendStarted) is confirmed started by the tick's
    friendUnstartedWorking, which clears the mark as FriendStart and the ready-card start (friendStartUnits)
    do, and friendDealtReturn leaves alone a friend with such a working card, so a friend who has taken a
    card is never dealt no more. tla/FriendDealtReturn.tla Start and DealtNoMoreHolds, with the reversed
    witness MCFriendDealtReturnBrokenReturn."
   :origin "dogfood 2026-10-10: a friend who took a working card stayed dealt no more despite having taken one")

  (fix "friend-has-taken-one-predicate" :release "v1.2.6" :status "shipped"
   :title "A friend who has taken a card is never marked dealt no more, by one predicate"
   :text "The return's leave-alone gate and every clear of the dealt-no-more mark read the one predicate
    friendHasTaken (her beat names a job running, or any card on her row, ready or working, is started or
    carries her progress), so a ready card carrying progress that the tick promotes to working this same
    tick is a start: the friend is not marked dealt no more, and her untaken cards are not returned, in
    the same tick she took one. friendDealPass, friendUnstartedWorking and FriendStart each clear the mark
    through the one helper. tla/FriendDealtReturn.tla Return guards on no card of hers in started, with
    Start and Progress its start paths, and the reversed witness MCFriendDealtReturnBrokenReady."
   :origin "dogfood 2026-10-10; seat rebrief 2026-10-11: a ready card carrying progress was invisible to the return's gate")

  (fix "land-merges-the-roadmap-data" :release "v1.2.6" :status "shipped"
   :title "Land merges the roadmap data, not returns the card"
   :text "A head whose conflict is in docs/fixes.sexp or docs/roadmap.sexp, with or without FIXES.md and
    ROADMAP.md, lands: the data is merged entry by entry (each side only adding or only removing whole
    lines in each conflicted stretch, the tip's additions first), checked by decoding all three sides
    and the result, and each page is taken from the tip and regenerated from the merged data as `make
    roadmap` does. Both sides changing one entry is refused as before. Also: main was red on its class
    gofmt from PR #67 on, so the lander merged no head onto it (PR #74)."
   :origin "the Studio sprint, epoch 16, 2026-10-10: merging stood at 15 to 21 cards with none landed in 30
    minutes, every second card refused on FIXES.md and docs/fixes.sexp and reworked at the tip")

   (fix "dashboard-empty-and-stopped-states" :release "v1.2.6" :status "shipped"
    :title "Cost breakdown empty state, landed tile text, and stopped dot red"
    :text "When the epoch has no spend, the cost breakdown table keeps its header and shows three empty rows with \"-\" in each cell, instead of showing prose. When landed is 0 of 0, the landed tile reads \"nothing complete\" instead of \"- complete\". When the machine is STOPPED, the Updated clock dot is red (the stopped class) instead of green (the ok class). The fix preserves the stopped state across the setLive call that updates the clock."
    :origin "seat fix, 2026-10-10")
   (fix "land-refused-head-never-reoffered" :release "v1.2.6" :status "shipped"
   :title "A head the landing refused is never offered to accept again; a ledger conflict is no brief defect"
   :text "A card whose head the landing refused keeps that head (land_refused_head), and while it is still
    its head no step raises ready to accept on it: at the brief's bound the card is in review with the
    bound's judgment alone, and with nothing open it is stranded (rework or drop), never offered the
    accept that queues the refused head again. A conflict only in the shared ledgers (the roadmap's data
    and pages, the class ledgers, the AGENTS maps, the keyed TLA+ tables, the tables lock) is the tip
    moving under the card: reworked at the tip every time, never counted toward the brief's bound, so
    never marked a brief defect, and its finding seeds no bound for a later file conflict."
   :origin "the Studio sprint, epoch 16, 2026-10-11 00:07-00:20Z: about 30 cards across v126-land, v126-fix,
    v128 and v129 looped accept, refused on FIXES.md and docs/fixes.sexp, ready to accept, accept; rework
    refused as a brief defect (land-ns56-backup-live-beats)")

  (fix "backup-file-needs-no-lastsave" :release "v1.2.6" :status "planned"
   :title "backup --file and snapshot do not need LASTSAVE"
   :text "The Redis snapshot source detects a completed BGSAVE from rdb_saves (or rdb_last_save_time) in
    INFO persistence, so the coordinator ACL user, which may not run LASTSAVE, can take the backup. The
    BGSAVE-failure and timeout refusals are unchanged."
   :origin "pre-clear backup, 2026-10-10 (NOPERM on LASTSAVE)")

  (fix "backup-out-restore-check-live-fleet" :release "v1.2.14" :status "planned"
   :title "backup --out passes on a fleet that is beating"
   :text "The restore check sets aside what a live member's beat rewrites while the dump is taken (the
    fleet table's revision, a fleet row's place, load text and up or down status) and compares
    everything else exactly, so a running fleet no longer fails the check; a held status, a missing
    row or a changed card still does."
   :origin "pre-clear backup, 2026-10-10 (8 fleet parts differed)")

  (fix "backup-restores-the-fleet-rows" :release "v1.2.6" :status "planned"
   :title "backup --file passes on a fleet that is beating"
   :text "The restore check of backup --file sets aside what a live member's beat rewrites while the dump
    is taken (the fleet table's revision, a fleet row's place, load text and up or down status) and
    compares everything else exactly, so the pre-adopt backup of a running fleet no longer refuses with
    the dump does not restore the sprint the store held; a held status, a missing row or a changed card
    still does. store.SemanticRestore (internal/sprint/store/restore.go) is the check, run from runBackup
    (cmd/nova-sprint/backup.go)."
   :origin "dogfood 2026-10-10 evening: fleet-only restart; pre-clear backup, 2026-10-10 (8 fleet parts differed)")

    (fix "inbox-push-write-once-race" :release "v1.2.11" :status "planned"
     :title "Inbox push cannot overwrite a published judgment"
     :text "Two overlapping clients share one temporary path, so a second write can change the published
      file. Use a unique temporary path per writer."
     :origin "issue #5160")

    (fix "dev-is-behind-counts-cards-on-their-own-base" :release "v1.2.11" :status "planned"
     :title "Dev behind judgment excludes cards landed on protected branches"
     :text "After moving to one line, cards land straight onto their own base (main for nova-sprint, dev for nova-tools). The dev behind judgment now only counts cards whose base is not a protected branch (dev or main). Cards landed on sprint branches are counted; cards on protected branches are excluded since they landed directly onto the line. The judgment message lists counted cards by name."
     :origin "this pull request")

    (fix "seat-judgment-repush" :release "v1.2.9" :status "planned"
    :title "Judgments waiting on the seat are re-pushed each look"
    :text "The seat push loop was only pushing judgments once because it cached the 'seen' file list
     for all directories. When following the seat, cached seen prevents re-pushing judgments that are
     still waiting on the seat. The fix modifies pushTarget to not cache seen for seat directories
     (only for fixed directories), so judgments are re-pushed on every look while unanswered."
    :origin "seat-only restart, 2026-10-10: fleet at 18 of 76, ten cards waiting on seat's judgment")

   (fix "rest-properties-cap" :release "v1.2.11" :status "planned"
    :title "Rested routes must not overflow the fleet table property cap"
    :text "Rule 3 writes one property per rested route, so the cap is reached at about 58 routes and the
     tick writes nothing. Use one property for all."
    :origin "issue #5210")

   (fix "doctor-preflight-checks-the-store-schema" :release "v1.2.11" :status "planned"
    :title "Doctor's preflight gate checks the store's schema before a switch or adopt"
    :text "New `doctor --preflight <candidate build>` frame, the release gate's first check, run before
     any server switch or adopt: it runs the candidate's `version` and its `tick --shadow --json`
     against the store this doctor reads (runShadow, read-only), and holds the store's tables against
     the candidate's TABLES.lock. A locked-shape refusal is one FAIL row (area preflight, check schema,
     subject the candidate's version) whose evidence is the shadow's own error text whole, naming each
     missing column and the exact nova-table commands that mend it, and whose remedy is to run those
     commands on a STOPPED machine, or the adopt's schema step; any other shadow failure is a FAIL row
     with its text, and a shadow that passes is one OK row naming the candidate's version. The gate's
     later checks (members, mirrors, links, stable) are WARN rows saying \"not measured by this build\"
     until their cards land, from one table each fills in. The rows are built by a pure function over
     the facts read (internal/sprint/doctor_preflight.go); exit 1 on any FAIL, as doctor does."
    :origin "this card; tmp/next-sprint-cards/TOOLING-FROM-THE-SEAT-2026-10-11.md item 20")

   (fix "server-refuses-to-run-on-a-store-missing-locked-columns" :release "v1.2.11" :status "planned"
    :title "The server and its shadow refuse a store missing the locked columns"
    :text "The live store's fleet table never got finished and redealt (internal/sprint/TABLES.lock, the
     Change of 2026-10-06), so the adopted v1.2.10 server refused every finish that moved a card to done.
     run and tick --shadow now check the store's tables against the build's locked definitions before either
     starts or plans, and refuse with the nova-table commands that mend it; the server switch's shadow tick
     catches a candidate before the swap."
    :origin "the coordinator, 2026-10-11, on v1.2.10 (00c54ba): server-refuses-to-run-on-a-store-missing-locked-columns.w1")

   (fix "tick-under-its-gate-at-load" :release "v1.2.9" :status "planned"
    :title "The deal builds no per-card route map, so the tick stays under its gate"
    :text "Drain and deal take 0.4 to 0.7 seconds at load, so the tick is over its one-second gate, and the
     dominant cost is the deal's routeOf, which built a served map (route.go, served[r.Name] = r) and a
     set of route fields (the map literal and set[FieldTierNow]) for every ready primary each tick, and
     again per up member in the launch check (launchersOf): 65% of the deal's alloc_space, which the GC
     spent near half the deal on (gcDrain 44% of cpu). The model read the deal also paid (cardhdr.ReadModel's
     regexp over line 1) is a minor share (4.7%). routeOf now builds served as an on-demand scan over the
     store's few routes (servedRoute) instead of a map, and the tick's per-card check asks only whether a
     route serves, never its fields (routeServed, called by the machines' pass, launchersOf and noRoute),
     so it builds no set; only the callers that deal (deal, redeal, the rebalance) build the fields, at most
     the width's cards a tick. The model read is also memoized once per brief (modelOf), wired through the
     call sites the tick reaches (friend_deal.go, friend_read.go, readers.go, lane_cap.go). On the loaded
     store of 80,000 ready beside 80,000 done and 8 friends the steady deal drops about 0.64s to 0.29s on
     this bench, both under the one-second gate, and TestTickDrainAndDealUnderTheGateAtLoad shows the deal
     stays under the gate; TestTickDealAllocatesNoPerCardRouteMapAtScale holds the removed allocations with
     a count and no clock (near 57 a ready card to near 23, so it fails on the unfixed code and passes
     after), BenchmarkTickDealAtLoad measures the deal and BenchmarkTickDrainAtLoad the drain, and
     TestTheTickDealReadsEachBriefOnceAtScale holds the parse to one a brief."
    :origin "seat ledger v1.2.4-held-2026-10-10.md 5577 tick gate")

   (fix "hold-pinned-to-head-sha" :release "v1.2.9" :status "shipped"
    :title "A hold pinned to an older head is not a hold at the current head"
    :text "The lander ignores a hold whose sha is not the PR head when a later line at the head clears it."
    :origin "issue #2710")

   (fix "merge-approval-head-from-disposition" :release "v1.2.9" :status "planned"
    :title "Approval at head is read from the disposition line"
    :text "The review API commit id can differ from the head the reviewer read. Merge checks parse the head
     named in the disposition and treat commit id as untrusted."
    :origin "issue #2037")

   (fix "log-since-wide-window" :release "v1.2.9" :status "planned"
    :title "Fix log --since for windows wider than 22 hours"
    :text "A wide --since window returns nothing; pin the window and return every event in it. Did not
     reproduce at 6f3323a6c83f9e7b340bf477886862c39001315e."
    :origin "card moved out of the sprint (work record, 2026-10-04); card from the sprint store (2026-10-10)")

   (fix "fleet-probe-loaded-bench-not-down" :release "v1.2.9" :status "shipped"
    :title "A loaded bench must not be marked down by the fleet probe"
    :text "The fleet-state probe marks a heavily loaded but healthy bench down after a few missed probes,
     and the dealer then kills its loops and sweeps its queue. Define state-entry timing and tell loaded
     from down."
    :origin "issue #2161")

   (fix "bound-judgment-names-its-remedy" :release "v1.2.6" :status "shipped"
    :title "A bound judgment names the exact command that answers it"
    :text "A card at its redeal bound says which tier above to rework on and the drop command whole, and a
     card at its brief's bound names brief --brief-file and the drop command, in the judgment's own text as
     a rework's refusal already said, so the coordinator runs the remedy as printed instead of guessing."
    :origin "dogfood 2026-10-10 evening: a fleet-only restart bounded several cards, and answering them took
     four tries -- brief --tier, redo, rework, rework --group")

   (fix "overload-alarm-covers-friends" :release "v1.2.6" :status "shipped"
    :title "The overload alarm covers friends too"
    :text "A friend up whose cards keep ending on a timeout raises the overload judgment as a member
     does: three timeouts within OverloadWindow on her row friend.<name>, counted after her finishes'
     `friend <name> <VERDICT>: ` prefix, her width the roster's. Its remedy is her width in
     nova-config, applied by friend sync."
    :origin "sprint card overload-alarm-covers-friends")

   (fix "restore-tests-owed-from-split" :release "v1.2.6" :status "in-progress"
    :title "Restore the tests the repository split moved out of nova-tools"
    :text "The four doc-pin tests moved out of nova-tools restored in nova-sprint, each wired to the behavior
     it pins: the coordinator rules (cmd/nova-sprint/handover_rules_test.go,
     TestEveryCoordinatorRuleNamesVerbsThatExist, holds each rule of docs/SPRINT-COORDINATOR.md section 10 to
     nova-sprint's verb table and the verbs of the tools the runbook names), the processor doc
     (cmd/nova-sprint/processor_doc_test.go, pinned to docs/PROCESSOR.md's mapping rows), and the coordinator
     tools register and tool class (cmd/nova-sprint/coordinator_tools_test.go,
     TestEveryCoordinatorToolMapsToARealNovaVerb and TestEveryCoordinatorStopgapNamesTheVerbThatReplacesIt,
     pinned to docs/COORDINATOR-TOOLS.md's table). The handover now prints every rule by number
     (cmd/nova-sprint/handover_rules.go, wired in seat.go, so TestHandoverPrintsEachRuleByNumber exercises
     the real path; docs/SPEC-SPRINT.md's handover note names the numbered rules, not the old single waves
     RULE line). The tick wall-clock gate job is restored too (.github/workflows/ci.yml, tick-gate: the dirty
     tick drive against a real store with NOVA_SPRINT_GATE_STORE=1, every tick held to MaxTickWall). The seat
     play test, the member and contract functional tests and the eight contract cases exercise nova-tools
     code and stay in nova-tools; they are still owed there."
    :origin "repository split cold reads (nova-tools PR #5571 and PR #5591); this card")

   (fix "doctor-checks-the-definition-of-done" :release "v1.2.7" :status "shipped"
    :title "The doctor names every definition-of-done fault with its evidence"
    :text "The read-only doctor joins the seat check's centralized machinery verdicts (the friends and
     the fleet up, the loop ticking, the dashboard answering, and a dashboard whose served snapshot
     is a second or more old) with the coordinator's automatic-stop ledger, section 9's always-true
     rules, the deal dry with cards waiting and the seat's overdue waits, and prints one
     evidence-bearing line per fault. It exits 0 when clean and 1 when red, and --json carries the
     fault lines and the machinery evidence with the push proof redacted. Three faults the card
     names are held for the dependencies that decide them, not judged here: working equals reality
     (a card working whose row's beat names no live run) waits on land-ns54-working-is-live, whose
     beat live set (RowWorkingKeys/LiveCountKeys) is not landed on main; the members' and friends'
     at-width facts are that dependency's width fact, the seat check's fleet and friends lines
     counting up/held/down alone; and the machine's own tick late (pushlate.go) is not joined, the
     loop line holding only the LoopSilence bound."
    :origin "this pull request")

   (fix "friend-card-carries-own-rules" :release "v1.2.6" :status "shipped"
    :title "A friend card that carries its own RULES line is accepted at add"
    :text "A friend is not a child: her card is not held for the child RULES paragraph, the rule- presence
     checks of the card lint, when it carries its own RULES line, and a friend card with none is still
     held to it, as a child's brief is. The step- scans hold a friend's card still: a line of it outside
     its RULES paragraph that contradicts its carried rules is a finding under any rule set."
    :origin "this pull request")

   (fix "per-landed-spend-on-landed" :release "v1.2.7" :status "shipped"
    :title "Per-landed cost uses spend on landed cards only; in-flight shown separately"
    :text "The per-landed figure now divides total spend on landed cards (including failed attempts
     and reworks) by the landed count, rather than dividing all spend so far by landed count.
     In-flight spend on cards in waiting, placed, or merging is shown separately, so the total
     is still visible and nothing is hidden. Stream table and sprint totals now agree."
    :origin "dogfood 2026-10-10: $7.74 per landed at 6 of 82; stream table $0.03 vs total $46.39")

   (fix "swarm-lint-card-false-refusals" :release "v1.2.14" :status "planned"
    :title "Card lint refuses valid cards: make gates and text inside fenced blocks"
    :text "Card lint rejects make-driven gates and flags parent paths or absolute paths quoted inside
     fenced blocks. It should accept both."
    :origin "issue #1994; issue #2302; issue #3470")

   (fix "card-column-single-field" :release "v1.2.14" :status "planned"
    :title "One column field every reader agrees on"
    :text "Card output carries a single column field so a card's state is not confused with the state of
     its need."
    :origin "card moved out of the sprint (work record, 2026-10-04)")

   (fix "a-stream-has-a-display-name" :release "v1.2.14" :status "planned"
    :title "A stream has a display name"
    :text "A stream's key stays its name everywhere (the work and merge rows, the stream= fields, every
     verb), and gains a human label: `stream set <s> --display <name>` records one line of printable
     text, at most 60 bytes, on the stream's control card as its `display` field, cleared with `default`
     or `none` like every other stream field. The dashboard's Work table and the streams listing show
     the display name when set, else `point-fix-<release>` when the stream belongs to a release, else
     the key (sprint.StreamDisplay); the streams verb prints it beside the release, StreamClock and
     StreamRow carry it in `Display`, and a name over the bound or with a newline is refused whole,
     nothing written."
    :origin "the owner, 2026-10-11: \"agree that the name becomes the key, and you add a small display name. good tidy feature. pls add.\"")

   (fix "reader-only-friend-no-work" :release "v1.2.9" :status "shipped"
    :title "A reader-only friend is dealt no work card; a builder-only friend is asked no read"
    :text "The deal and the ask honour a friend's roles: a friend whose nova-config row's roles do not
     name builder (RoleBuilder) is dealt no work card, and one whose roles do not name reader (RoleReader)
     is asked no read, in the read-card ask and the friends' ask alike. A row with no role list is a row
     from before roles and keeps the pre-role behavior: it is dealt work and asked reads (friendHasRole).
     The dogfood of 2026-10-10 evening: a reader-only friend was dealt 16 work cards once it came up,
     and builder-only friends were asked reads."
    :origin "dogfood 2026-10-10 evening: fleet-only restart")

   (fix "stats-tidy-large-fleet" :release "v1.2.11" :status "planned"
    :title "A stats tidy succeeds over a fleet of more than 64 rows"
    :text "Tidying stats works when the fleet table has more than sixty-four rows, instead of hitting a
     property bound."
    :origin "card from the sprint store (2026-10-10)")
   (fix "a-joined-reader-gets-the-fleet-reader-tiers" :release "v1.2.11" :status "planned"
    :title "Fleet readers inherit common tier when added without --tiers"
    :text "A fleet reader added without --tiers now inherits the common tier when all other fleet readers
     agree; otherwise stores empty tier with from=default message. Doctor warns about fleet readers whose
     tiers differ from the fleet's common tier."
    :origin "a-joined-reader-gets-the-fleet-reader-tiers.w5.g4.e17 (2026-10-11)")

   (fix "fleet-sync-retires-a-renamed-machines-row" :release "v1.2.6" :status "shipped"
    :title "The tick retires a fleet row whose machine is gone from the inventory"
    :text "A rename (hetzner -> hetzner1) left the old fleet row held on the table forever, because
     nothing ever removed a row: fleet sync adds and resizes only. The tick now reads the same machine
     records fleet sync reads (nova-config, no new store) and, every tick and with no verb run by anyone,
     retires a row whose record is absent (sprint.TickRetireAbsent, internal/sprint/fleet_retire.go): a row
     with no record and no cards leaves the fleet table and the dashboard, its reader reader-<old> is
     retired with the row and only once it leaves (the rows store.DropMembers deleted that tick), its stats
     stay queryable under the old name, and one HAPPENED note records it; a row with no record that still
     holds cards is held and its cards dealt away, retired on the first tick its set is empty; a config read
     that fails retires nothing (never act on a missing read); a machine whose record exists but is offline
     stays down. fleet sync --check already lists what the next tick would retire, as its remove drift for
     an absent member with no card. Model tla/RetireAbsent.tla: RecordsKeepRows is the state invariant (a
     recorded machine keeps its row) and FleetRowsMatchConfig and LiveRetire are the leads-to the tick's
     fair reads and retirement make true; two reversed witnesses are declared, MCRetireAbsentBroken retiring
     a recorded row on a failed read as the counterexample to RecordsKeepRows. The TLC pass is recorded in
     tla/RUNS.tsv (MCRetireAbsent.cfg 2563 states, 468 distinct, exit 0; MCRetireAbsentBroken.cfg exit 12
     on RecordsKeepRows). Tests: a record removed, a tick, and the row and reader are gone while the stats
     stay; a row with working cards drains then retires; a draining absent row keeps its reader until its
     row leaves; a failed read retires nothing; an offline machine with a record is untouched."
    :origin "the owner, 2026-10-11 ~01:00Z and ~01:05Z, after hetzner was renamed hetzner1 and its old fleet
     row stayed held; this card; attempt 2")

    (fix "worklang-done-field-parsed" :release "v1.2.11" :status "planned"
     :title "The worklang parser keeps the done field of a unit"
     :text "Unit done reads a field the parser never keeps, so a done unit is never counted. The key is
      added and sibling skipped keys are checked, with a test."
     :origin "issue #3499")

   (fix "release-never-archives-a-stream" :release "v1.2.11" :status "planned"
    :title "The tick never archives streams; stream archive is the only way"
    :text "Streams leave the work and merge tables only through the explicit nova-sprint stream archive
     command. The tick no longer archives streams whose every card lands, which took them off the
     table and dashboard without anyone asking. keepArchive only draws again an archived stream that
     takes a new card."
    :origin "the owner, 2026-10-11 ~04:05Z; this card")

   (fix "brief-file-over-the-bound-is-refused" :release "v1.2.11" :status "planned"
    :title "Brief over the bound is refused with exit 2"
    :text "brief <id> --brief-file and brief <id> --brief <text> had no size check before the lint. Add a
     helper that checks the bound with the same message as brief --dir, used by add, brief --dir and
     these two forms. On a RUNNING machine, queued changes are now checked at the step before logging."
    :origin "epoch 16 stall, 2026-10-11 04:17:19Z to 04:54:23Z")

   (fix "add-refuses-card-off-sprint-base" :release "v1.2.6" :status "shipped"
   :title "add and brief refuse a card cut off the sprint's base"
   :text "Once the coordinator records the sprint's base (set --base, the work table's sprint_base), add and
    brief refuse a card whose BASE is any other branch outside the promotion stream, exit 2, nothing
    refused as before, and the promotion stream keeps its mark."
    :origin "the one-base rule; found 2026-10-04, cards folded back onto the base by hand through 17 conflicts")

   (fix "a-rejected-push-is-never-an-environment-fault" :release "v1.2.11" :status "planned"
    :title "EnvFault classifies only the member's reason, not child text; rejected push is not envFault"
    :text "EnvFault now classifies only the text before the first '; ' (the member's own reason), ignoring the child's words after it. A reason carrying '[rejected]' 'non-fast-forward' or '(fetch first)' is a branch refusal, never an environment fault (tla/ProviderBudget.tla, ARejectedPushIsNeverAFault). EnvFault returns the empty string for branch refusals before any class is matched."
    :origin "this card; 2026-10-11; v1.2.11 bug fix")

  (fix "held-friend-reader-serves-nothing" :release "v1.2.6" :status "shipped"
   :title "A held friend's reader serves nothing"
   :text "docs/SPEC-SPRINT.md section 11 (hold): a friend and her reader row are one member to the
    server. A held friend's reader row (reader-<friend>) serves nothing: ownModelReader and
    readerServesTier both say so, no route judgment counts her up, and the TLA+ model ReaderTiers
    carries the held reader beside its reversed witness. A hold with --return also takes back the
    reads asked of her reader row, where a reader up is free to read them."
   :origin "dogfood 2026-10-10 evening: fleet-only restart")

  (fix "brief-on-a-working-card" :release "v1.2.6" :status "shipped"
   :title "A brief replaces a working card's in place, for its next attempt"
   :text "A card working takes a new brief in place, as one in review does (briefKept, docs/SPEC-SPRINT.md,
    the brief verb): the brief is stored on the same card, its id, stream, score and needs kept and no
    twin cut, and the record and the card's next attempt's why read <id> brief edited in place by <actor>
    at attempt <n>: <what changed>, so the coordinator corrects a running card without waiting for
    review. Only a merging or landed card keeps its brief, with what changes it instead (briefStarted).
    The verb's help and docs/SPEC-SPRINT.md say so."
   :origin "the owner, 2026-10-10: what else is manual that should be automatic from the machine; this card")
  (fix "personal-name-check-exempts-paths" :release "v1.2.6" :status "shipped"
   :title "The personal-name check exempts names inside file paths"
   :text "The card's personal-name check refuses a brief whose SOURCE or PATHS line contains a name of the
    deployment outside a double-quoted span. A name inside a file path (a whitespace- or comma-separated
    token containing a slash or ending in .md) is fine; only names in prose are refused. Before this fix,
    a name in a path triggered the check, so a brief like 'See /home/ada/projects/' was refused, and a
    line mixing a path with prose on the same line was incorrectly allowed."
   :origin "dogfood 2026-10-10: add refused briefs over a path segment")

  (fix "nova-work-roadmap-verbs" :release "v1.2.6" :status "planned"
   :title "nova-work roadmap check, list, add, remove, pull, done, note and render"
   :text "Verbs over the roadmap s-expression files with byte-stable round trips; each writing verb
    refuses a file that check rejects. Pulled into v1.2.6 by its own verb: the verbs edit docs/roadmap.sexp and docs/fixes.sexp and write both pages, locally, with no GitHub call per entry."
   :origin "nova-sprint commit 8c87a4b (not carried by the re-seed, nova-sprint PR #28)"
   :kept (:item-group "sprint"
          :item-date "2026-10-10"
          :item-release "after v1.4"
           :earlier-title "nova-work roadmap check, add, remove, pull and note"))

   (fix "machine-fault-hold-clears-when-healthy" :release "v1.2.9" :status "shipped"
    :title "A machine fault hold clears itself once the member is healthy"
    :text "A hold the machine places for a failure (a harness fault, a failed take, or the member going
     down) is marked held_by=fault and clears itself on the tick once the member beats and has finished a
     card cleanly since the hold: the machine's down is its fault hold, presence writes the mark and its
     stamp, and the tick lifts the mark on the clean finish, telling the seat. A hold a person made carries
     no held_by mark and is never lifted by the machine. TLA+ model DealFill, properties FaultHoldClears
     and PersonHoldStays, with two reversed witnesses."
    :origin "the owner, 2026-10-10: what else is manual that should be automatic from the machine")

  (fix "broken-verdict-always-records" :release "v1.2.6" :status "shipped"
   :title "A broken verdict at the same head always records"
   :text "Every reader, the coordinator included, records broken on a card in review at any bound: a
    broken verdict at the same head outweighs any number of oks, so accept never queues the head a
    reader found broken, ask --another never answers it, and the card goes back to work with the
    finding (reworked or widened in place) rather than accepted on a fleet ok. In tla/SprintRules.tla
    the read machine's fixed findings (rdfixed) bound the new rework at the brief's bound, and the
    reversed witness MCSprintRulesReadsBrokenBoundInside leaves an inside finding at the bound,
    violating ReworkAtBound."
   :origin "dogfood 2026-10-10 evening: fleet-only restart")

  (fix "down-member-is-down-not-fault" :release "v1.2.6" :status "planned"
   :title "A member whose host is offline is shown down, not as a fault"
   :text "The seat check reports a member that does not beat on an offline host as down (offline=name, the
    fleet line stays OK); DOWN when the host answers, or gives no clear no-answer (a name that does not resolve
    is named on the line). The seat probes the ssh port of each such member's machine row at once under one
    3 s bound, with a beating member other than the seat's own machine as the control of its network: no
    control answer, no member excused."
   :origin "v1.2.5 candidate list")

  (fix "readers-behind-judgment-once" :release "v1.2.9" :status "planned"
   :title "A standing judgment is one judgment, pushed once while it holds"
   :text "A standing condition is one judgment, keyed by its type and subject, rewritten in place while
    it holds and pushed again only when its own numbers move. The readers-are-behind judgment is keyed
    by type and subject (condKey, NReadersBehind), but its rewrite test compared the whole line,
    review's count included, and review moves every tick as cards flow through it: the tick rewrote and
    pushed the one judgment every tick while the readers' numbers held (dogfood 2026-10-10: j407, j409,
    j410 within a minute). notify now compares a material: for the readers behind, the readers' own
    numbers, never review's count (MaterialWhat, readers_behind.go), so the judgment is pushed once when
    it rises, again when the readers move, and not for review."
   :origin "dogfood 2026-10-10 evening: fleet-only restart; j407, j409, j410 within a minute")

  (fix "server-build-descends-from-base" :release "v1.2.6" :status "in-progress"
   :title "A sprint server build must descend from the origin base"
   :text "server switch refuses a build that does not descend from the origin base, so an off-base build
    cannot become the server."
   :origin "nova-sprint commit 9338f08 (not carried by the re-seed, nova-sprint PR #28)")
   (fix "widen-loop-capped" :release "v1.2.6" :status "shipped"
    :title "The widen loop is capped: every widen counts toward the brief's bound"
    :text "A card whose heads keep naming files outside its PATHS was widened and redealt without end: the
     E12 branch of the merge step skipped the brief's bound, and the widen rule's brief edit reset the
     attempts the bound counts. Now one widen count (FieldWidens, shared by the read-broken and widen
     rules) caps every widen, and the merge step checks the brief's bound before its E12 branch, so a
     card widened MaxWidens (3) times, or refused for files outside PATHS at its brief's bound, goes back
     to review as the brief is wrong, a mind's, never widened again. TLA+ model Land, properties
     AttemptsBounded, SameRefusalHalts and Settles, with the reversed witness ReachWidenLands."
    :origin "nova-sprint #64; the seat ledger v1.2.4-held-2026-10-10.md (the ejection-bound audit of 2026-10-10)")
  (fix "stop-debt-settles-from-the-members-beat" :release "v1.2.6" :status "shipped"
   :title "STOP debt settles from the owner's beat, so START never waits on its receipt"
   :text "After a STOP the server settles each owed lease whose owner has beaten since the STOP and whose
    beat no longer names the job, returning the card with a recorded reason, so START never waits on the
    owner's own stop-return receipt. A friend's beat names the jobs she runs: a lease whose job it still
    names stays owed, one it no longer names is settled. A machine's beat names no job; its stop-returns
    count is its word how many its lanes still owe, so a machine lease (a reader machine's too) is settled
    at zero and stays owed above zero. An owner that has not beaten since the STOP is reported to the seat
    in a judgment. TLA+ model StopReturn, property AbsentJobSettles, with the reversed witness
    nosettlebybeat; the model's Beat is drop-only while STOPPED, so a job a stopped owner's beat shows gone
    stays gone."
   :origin "the seat, 2026-10-10: START refused on 33 fleet jobs whose members never sent stop-return receipts")
  (fix "a-card-ready-over-a-minute-pushes-the-seat" :release "v1.2.6" :status "planned"
   :title "A ready card that sits past a minute pushes the seat"
   :text "A card that sits in ready past the ready bound (the sprint setting ready_max, default one
    minute) raises one judgment per (reason, tier), with the reason the deal leaves it: the tier, each route
    that serves no card and why (disabled, resting until paid, resting until woken, out of budget), no member up
    with free width to launch a route of its tier (fleet up), a hold, or a sentinel, and the remedy verb
    (routes wake, funded, fleet up, release). Cards that share a cause are one alert, never one per card, and it
    clears itself the tick the card is dealt. The rule is tla/ReadyOver.tla."
   :origin "card a-card-ready-over-a-minute-pushes-the-seatb.w8")
  (fix "a-member-environment-fault-holds-it-and-pushes-the-seat" :release "v1.2.6" :status "planned"
   :title "A member's environment fault holds it and pushes the seat"
   :text "A finish failure whose cause is the member's environment -- a push refusal (no credential, auth,
    unreachable remote), a missing tool or harness, a sandbox/wall refusal, a disk-full, or any class the
    member reports as environment -- is the member's, never the card's or the model's. The first one
    returns the card to ready untouched (the work card withdraws without an ended take, so no redeal is
    spent, the attempt is unchanged, and it is never failed work), holds the member so the deal gives it
    no new card, and raises one judgment pushed to the seat for the member, kept naming its current cause
    and its remedy (one per member and cause: a later fault with a different cause is surfaced, never
    left as the first cause's stale remedy; for a push refusal the remedy is the fleet push-credential
    play with --limit <member>). One judgment per member and cause holds within a finish step too: two of
    one member's cards finishing in one multi-card finish call (one report, so one cause) raise one
    judgment and write the member's control card once, not once per card, since the plan the model admits
    changes a control card once a step. An environment fault records itself beside a hold already on the
    member (a coordinator's, the machine's fault hold, the sync's): its clear -- a push, or fleet up's
    passed probe -- releases only the machine's own environment hold (held_by=environment), so a person's
    hold, its reason and its finish marker stay until a person lifts it. The fault clears when the
    member's next card pushes, or when fleet up runs the member's own probe (the push-credential probe,
    the harness check) and it passes; fleet up refuses a member whose last environment fault is not
    cleared, and on a new member runs its own probe before accepting width (init bootstraps by passing
    the probe). Modeled by tla/ProviderBudget.tla."
   :origin "sprint card a-member-environment-fault-holds-it-and-pushes-the-seat; the owner, 2026-10-11Z:
    the failure here is not noticing this until now; attempts 3 and 5")
  (fix "ok-percent-counts-reader-verdicts" :release "v1.2.6" :status "in-progress"
   :title "ok% counts the readers' verdicts; a friend's card past its deadline is a redeal"
   :text "A worker's finish, ok or failed, moves its work card to the fleet's hidden finished cell, where
    it counts in no ok%; the readers' verdict on the attempt moves it on, to ok when as many different
    readers as the primary needs read it ok, to failed when a reader read it broken, and work no reader
    reads stays finished. A friend's card past its deadline unfinished is redealt, counted in the shown
    redealt column on her row, never a failure and never a late judgment. The fleet table gains the
    finished and redealt columns, the friends table the redealt column, and a migration on a stopped
    machine takes the live store to the locked shape (migrations/0001_fleet_finished_redealt.sh)."
   :origin "the owner, 2026-10-04: trust but VERIFY; I want to trust the ok%")
  (fix "sentinels-are-not-work-in-eta-or-throughput" :release "v1.2.6" :status "shipped"
   :title "A sentinel is not work: it leaves the ETA, the landing rate and the summary counts"
   :text "A sentinel (sprint.IsSentinel) is a stop the coordinator releases, not work, so the sprint
    line's x/y and percent, where --json's landed and all (and archived_cards and archived_landed), the
    ETA, its five-landed floor and the landing rate count work cards only, across the epoch's streams on
    the table and archived, and never a sentinel's released stamp (docs/SPEC-SPRINT.md sections 1 and
    16). The work table's cells still count sentinels. The where record carries each stream's sentinels,
    all and landed, counted from the cards the tick already reads, so where reads no card for them;
    sprintLine agrees with where. Releasing a sentinel never archives its stream: the tick's archive part
    leaves a stream holding a sentinel drawn, and a stream is archived only by an explicit stream
    archive. The Go dashboard port (branch dashboard-is-the-live-page) must carry the same exclusion in
    its /api/sprint."
   :origin "the owner, 2026-10-11 ~03:55Z: sentinel cards should not be counted when estimating time
    remaining or cards per hour; the seat, ~04:00Z: releasing cut sentinels made the tick archive their
    streams")
  (fix "tlc-records-match-their-models" :release "v1.2.6" :status "planned"
   :title "TLC records match their models"
   :text "TLC records refreshed for 44 stale groups and plan rows added for MCProviderBudget and MCReadyOver; cardmachine still owed"
   :origin "sprint card tlc-records-match-their-models; the seat ledger v1.2.4-held-2026-10-10.md RUNS.tsv drift")

  (fix "low-balance-pushes-the-seat-before-the-refusalc" :release "v1.2.6" :status "planned"
   :title "A provider's low balance is raised before a refusal; a credit refusal spends no redeal"
   :text "The seat hears a provider is running dry before a refusal costs a card. The balance poll
    reads DeepSeek's balance (GET /user/balance with the key as a bearer token from DEEPSEEK_API_KEY,
    the USD entry's total_balance; an answer with no USD entry is unknown, never a currency
    conversion) beside OpenRouter's, so a provider the poll could not see is no longer unknown. Low
    is true at or under the low_balance_usd threshold (a sprint row setting, default 5 dollars) as
    well as at or under zero or under an hour's spend, and raises the one NProviderLow judgment per
    provider, its words leading with fund <provider> and its decisions naming funded <provider>; its
    routes STILL SERVE, as the owner ruled on 2026-10-10: a balance raises a thing to do, it never
    rests a route by itself. A take the provider refused for credit (class out-of-credit, a 402) on
    a limited route still rests the provider exactly as before, but returns the card through the
    same no-spend path a rate limit uses: withdrawn without FieldTakeEnded, so no redeal and no
    attempt is spent and no failed-work judgment is raised, the refusal still recorded so the
    provider rests; a 401 keeps the path that ends the take. TLA+ model RouteRest, property
    OutOfCreditSpendsNoRedeal (a credit event leaves the redeal count unchanged), with the reversed
    witness MCRouteRestBrokenBalance still failing. Tests TestReadDeepSeekBalance,
    TestALowBalanceAtTheThresholdRaisesOneJudgment and TestAnOutOfCreditTakeSpendsNoRedeal. The run
    loop's nova-secrets exec --only list must include DEEPSEEK_API_KEY for the poll to read it."
   :origin "the owner, 2026-10-11 ~00:20Z, after the fleet stalled (35 ready, 7 up, 0 working, every pro/heavy route resting): \"How can we fix it so this doesn't happen moving forward\"; nova-sprint docs/fixes.sexp entry low-balance-pushes-the-seat-before-the-refusalc")

  (fix "v1-2-12-inventory" :release "v1.2.12" :status "planned"
   :title "Inventory the tests and models before v1.2.12's first test card"
   :text "Coverage per package, the functional tier's state, each TLA+ model against the code it claims
    to model, and each RUNS.tsv record's age and result. The tests and model cards of v1.2.12 are cut
    from it."
   :origin "the owner, 2026-10-11: the v1.2.12 scope (inventory first)")

  (fix "fn-view-after-a-clear-draws-no-old-stream" :release "v1.2.12" :status "planned"
   :title "The view after a clear draws no old stream"
   :text "TestTheViewAfterAClearDrawsNoOldStream (cmd/nova-sprint/view_hide_functional_test.go) pins the blank-slate clear (docs/SPEC-SPRINT.md section 13): after init, an add of the streams a, b and c, and a clear, the view still draws the work, readers, merge and fleet tables with the fleet member m1 carried over, and draws no old stream row; add admits a card on every form, so it cannot open a stream with no cards, and after add --stream b the view draws b and neither a nor c. The old test expected the three stream rows kept at zero, an expectation from before the clear became a blank slate."
   :origin "this card")

  (fix "cover-idle-trace-and-the-bound-rule" :release "v1.2.12" :status "planned"
   :title "Cover the idle trace and the bound rule in their own package"
   :text "internal/sprint's own tests never ran the idle alarm's words (TraceIdle, idle.go:90) or
    its episode (TickIdle, idle.go:248), nor the bound rule (ruleBound, rules.go:337); their only
    tests were in internal/sprint/store. Add idle_trace_behaviour_test.go and
    rule_bound_behaviour_test.go: the idle note's drop-blocked, missing-need and cycle, held and
    sentinel roots, a tier no route serves, a need on a stopped stream, a need an open judgment
    holds, the nine-root truncation, plus the tick's one-note episode and its clear; and the bound
    rule's answers through RuleAnswers with an open NBound judgment (off the table, not at a bound,
    a budget failure raised a tier, a flash card at its real redeal bound raised a tier, a card
    every member up refused at staging, heavy to a friend, the rule off). Tests and models only, no
    product change."
   :origin "this card, cover-idle-trace-and-the-bound-rule.w2")

  (fix "cover-route-table-stats-routes" :release "v1.2.12" :status "planned"
   :title "In-package behaviour tests for the stats route table"
   :text "RouteTable (internal/sprint/routetable.go:25), the table `nova-sprint stats --routes` prints
    (docs/SPEC-SPRINT.md, the verb stats), gets tests from inside its own package in
    internal/sprint/routetable_behaviour_test.go: the window (a finish before since is left out with its
    provider takes, a deal, take or accept before it still counts), the ok/failed split and takes, the
    no-route and no-usage rules, the provider no-result and the more-than-fifty storm, landed and 1stT,
    wrong/reviewed, the money columns and the upper-median wall, and the FLASH (all) and PRO (all) rows.
    RouteTable's in-package statement coverage goes from 0.0% of 163 to 99.4%. No product change."
   :origin "the owner, 2026-10-11: v1.2.12 tests and models only; tmp/v1.2.12-inventory-2026-10-11.md section 8 item C1")

  (fix "fn-dashboard-test-compares-the-page-data" :release "v1.2.12" :status "planned"
   :title "The dashboard functional test reads where --json as the page serves it"
   :text "TestDashboardServesThePageOnEveryListener compared each listener's /api/sprint tables against
    raw where --json; since the page serves internal/sprintdash's pageData, which folds each stream's
    stream_costs record (the tiers and in_flight fields) into the work rows, the two never matched.
    The test now puts where --json through sprintdash.PageData at a fixed time before it unmarshals its
    expectation, and compares tables and landed as before. No product or dashboard change."
   :origin "this card")

  (fix "cover-deal-plan-refusals-and-redeals" :release "v1.2.12" :status "planned"
   :title "In-package behaviour tests for the deal's plan, refusals and redeals"
   :text "Deal (internal/sprint/steps_work.go:954; its doc comment is the contract) gets tests in
    internal/sprint/deal_plan_behaviour_test.go driving Deal and asserting on the plan's units and each
    refusal's text: the fleet's work off refuses every chosen card with \"run: nova-sprint set --fleet on\";
    every up member env-held refuses \"no fleet member is up\"; a BENCH card whose bench is down or at its
    room is refused with the bench's words; one member quiet and the other at its room refuses with the
    room words then \"; quiet: <m> until <t>\"; a withdrawn card is redealt at generation+1 with its attempt
    unchanged, never to a member StagingRefusers names; a withdrawn card every up member refused at staging
    is refused naming them; a withdrawn card at its redeal bound below its ceiling escalates to the next
    tier, its why naming the bound and redeals, and at its ceiling is refused with its bound and
    \": rework it with a fix, or drop it\"; four cards over two members of width 1 are dealt round the fleet
    never past DealAhead times a width. dealPlan's statement coverage goes from 65.0% to 86.4%. A real
    defect was found and is a v1.2.14 candidate: Deal panics when every up member is quiet or out of room
    (benchRefusal indexes bench[0] of an empty bench). No product change."
   :origin "this card")

  (fix "store-two-run-loops-retries-a-busy-fence" :release "v1.2.12" :status "planned"
   :title "The two-run-loops test retries the store's busy refusal"
   :text "TestCRTwoRunLoopsAtOnce's outside world now retries a step when the store answers with its
    documented retryable refusal (FenceBusyError, \"the sprint is busy ... nothing was changed; run the
    verb again\"), up to 20 tries per step, counting the retries per trial; a verb still busy after the
    bound fails the test with its verb and count, and goes in the report as a v1.2.14 candidate (an
    outside verb starved by two tick loops), never papered over by a larger bound. Each trial logs its
    busy retries so a run shows how often the fence was contended. \"changed under the step\" is still
    counted and every other assertion (judgments once, width, two reads per attempt, h.clean) is
    unchanged. Test-side only; no product change."
   :origin "the v1.2.12 plan (tmp/point-release-plan-v1.2.11-v1.2.12.md, last lines); CI run 38140844195 on #89; this card store-two-run-loops-retries-a-busy-fence")

  (fix "tla-coverage-rows-for-every-nova-sprint-module" :release "v1.2.12" :status "planned"
   :title "tla/COVERAGE.tsv names every TLA+ module in the tree exactly once"
   :text "Every module in tla/ (every X.tla whose name does not start with MC, SeatProof retired) is
    named by exactly one row's module column, every row's status is what the drift reads found, and
    every row that is not current names in owed_by the v1.2.12 card that owes it. Twenty-two rows are
    added, nineteen are changed, and a row marked missing names module \"-\"."
   :origin "card tla-coverage-rows-for-every-nova-sprint-module, 2026-10-11: v1.2.12 inventory items T29 and section 5, at nova-sprint main 00c54ba2e")

  (fix "tla-coordinatorwake-batch-and-level-events" :release "v1.2.12" :status "planned"
   :title "CoordinatorWake models the batched wake, the filtered message and the level events"
   :text "Models only, no product change. tla/CoordinatorWake.tla now models the batched wake (one wake
    fires every bus ask the look read, naming three and counting the rest), the filtered message that
    moves the cursor without a line, and the level events (a judgment, a stop, a friend down) that set
    and clear between looks, so NoLapse holds for bus events alone and the new LevelSeenWhileItHolds
    says a level event that holds a whole window wakes a run unless it clears first. A look that sees a
    level event cleared forgets the wake (seen is pruned), so a level event cleared and raised again,
    the clear observed, wakes again, matching watchwake.go's pruning of the judgments, the stop and the
    friends; a clear-and-reappearance between looks is not observed and need not wake. Spec now adds
    WF_vars(Look): held reaches Window only through looks, so a look is fair too, and a level event
    raised and left raised is eventually a whole window old and due, making LevelSeenWhileItHolds
    non-vacuous (a run blocked with nothing to wake keeps looking rather than leaving a held level below
    Window forever). The reversed witness MCCoordinatorWakeBrokenBatchDrops (Broken \"batchdrops\") fires
    only the first ask of a batch and writes the cursor past the rest, failing NoLapse; the two existing
    witnesses (nocursor, skip) keep their results. watchwake.go's comments now say bus events, and that a
    judgment raised and closed inside --judgment-every may never wake. Four cases pass on a bench."
   :origin "this card, tla-coordinatorwake-batch-and-level-events-2.w6, 2026-10-11: v1.2.12 inventory item T25; drift read at nova-sprint main 00c54ba2e, 2026-10-11")

  (fix "tla-serverlanes-control-line-tick-turn" :release "v1.2.12" :status "planned"
   :title "ServerLanes models the control line: batches by ticket and the tick's turn"
   :text "Model only, no product change. ServerLanes.tla held the server's one line as app.serial: a
    write took a free line and the tick took it when free. The code is now sprint.ControlLine
    (internal/sprint/control_line.go): a batch takes the next ticket and is served first come, first
    served; the tick takes the line at its turn, after the holder in flight and before every batch
    still waiting; a gone caller's ticket is served and handed back, its verbs never run. The module
    now models the tickets (next, serving, tkt), the tick's turn (turnDue, with the ghost dueFinishes)
    and serving a gone write's ticket in turn (ServeGone). Two invariants are added: FCFS (no batch is
    served before an earlier ticket still waiting) and TickWaitsOneTurn (once the tick's turn is due,
    at most the batch already holding the line finishes before TickBegin), and the reversed witness
    MCServerLanesBrokenTickQueues (the tick takes a ticket like a batch) fails TickWaitsOneTurn. The
    seriallock.go comment no longer claims the read lane's own line is app.serial, and the
    AbandonedNeverRuns citations are corrected to GoneNeverRuns. GoneNeverRuns,
    BeatNeverWaitsForTheLine, ReadWaitsOnlyForReads, the lease invariants and EveryBatchEnds still hold.
    tla/README.md's ServerLanes row now names the four reversed witnesses and the ticket/tick-turn model."
   :origin "this card, tla-serverlanes-control-line-tick-turn.w3, from the owner on epoch 17,
    2026-10-11: v1.2.12 inventory item T24, first of two; drift read at nova-sprint main 00c54ba2e")

  (fix "tla-stallladder-signs-of-life" :release "v1.2.12" :status "planned"
   :title "StallLadder reads a card's dealt and taken stamps and progress as the code does"
   :text "tla/StallLadder.tla was read against internal/sprint/friend_stall.go and friend_take.go and drifted in four places, each brought to the code. LastActive now counts a card's dealt and taken stamps beside her activity and progress, and the idle clock subtracts stopped time (StoppedTick), so a machine stop never counts against the bound; a progress stamp marks the card started, as the code reads \"started\" (progress, a report, or her beat naming the card), so rung 4 never takes it back; and the rung climbs only when the target rung is higher and resets only at 0, never written outright each tick. The existing invariants hold, and a new invariant NoStampedTaken (no card with a progress stamp is taken back) is checked by the reversed witness MCStallLadderBrokenTakestamped (Broken \"takestamped\"): rung 4 takes back a progress-stamped card, failing NoStampedTaken. friend_take.go gains one comment line citing the module."
   :origin "card tla-stallladder-signs-of-life, 2026-10-11: v1.2.12 inventory item T36, at nova-sprint main 00c54ba2e")

  (fix "tla-providerbudget-every-declared-witness-has-a-cfg" :release "v1.2.12" :status "planned"
   :title "ProviderBudget reads the caller's own probe word, and every declared witness has a config"
   :text "Models only, no product change. tla/ProviderBudget.tla's probe[m] becomes the caller's own word
    (the --probe fleet up is given, cmd/nova-sprint/verbs.go:3286), which the model trusts as the code does:
    TLC chooses it (ProbeNew(m, p) takes p over none, pass and fail) instead of the member's real result. The
    three declared witnesses with no config gain one, each failing as its name says: MCProviderBudgetBrokenClear
    (Broken = {\"clear\"}) violates FleetUpRefusesUncleared, MCProviderBudgetBrokenNoenv violates
    EnvOnlyByEnvironmentFinish, and MCProviderBudgetBrokenNodeal violates HeldDealtNothing; each has its
    CASES.tsv row. One comment-only change: internal/sprint/steps_work.go's new-member gate cites
    tla/ProviderBudget.tla NewMemberProbed, not FleetUpRefusesUncleared. tla/RUNS.tsv and tla/COVERAGE.tsv
    are made current."
   :origin "card tla-providerbudget-every-declared-witness-has-a-cfg, 2026-10-11: v1.2.12 inventory section 4, at nova-sprint main 00c54ba2e")

  (fix "tla-frienddealtreturn-bound-pins-and-status" :release "v1.2.12" :status "planned"
   :title "FriendDealtReturn carries a dealt age and a pin, counts a Running job as taken, and models the up check"
   :text "Models only, no product change. tla/FriendDealtReturn.tla was read against internal/sprint/friend_deal.go friendDealtReturn and drifted in five places, each brought to the code. Each card now carries a dealt age (age) and a pin (PinnedTo), and Return hands back only the cards of an up friend that are past the dealt bound and not her hard pin; a job named on her beat (Running) counts as taken (friendHasTaken), and Return runs only for an up friend. DealtNoMoreHolds is restated as the code keeps it, an action property: no new deal to a marked friend, not \"she holds none\" (the code keeps a marked friend's pinned and not-yet-past cards). TakenNeverMarked (a friend who has taken is never marked dealt no more) is what BrokenReturn and BrokenReady now fail, and PinnedNeverReturned (a hard-pinned card is never returned) is checked by the new reversed witness MCFriendDealtReturnBrokenReturnpinned (BadReturn \"returnpinned\"), which hands back a hard-pinned card. The friendUnstartedLevel defect (friend_start.go:249) is not modelled: it is the v1.2.14 candidate. friend_deal.go friendStarted gains one comment line citing the module. The bench TLC records are owed (no jar on this bench), so tla/RUNS.tsv is unchanged and tla/COVERAGE.tsv stays partial."
   :origin "this card, tla-frienddealtreturn-bound-pins-and-status-2.w2, 2026-10-11: v1.2.12 inventory item T36, drift read at nova-sprint main 00c54ba2e")

  (fix "tla-whopreference-tiers-pins-and-takeback" :release "v1.2.12" :status "planned"
   :title "WhoPreference gates the deal on tiers, honours the preference order, the take-back memory and the pin release"
   :text "Models only, no product change. tla/WhoPreference.tla was read against internal/sprint/friend_deal.go, friend_take.go, rebalance.go and stops.go and drifted in four places, each brought to the code. The deal is now gated on tiers (friendTakes / the fleet's set): a flash card never reaches a friend whose tiers are pro alone, checked by the new invariant DealtAdmitted, which the unchanged model violates (plain, a flash card, dealt to friend b whose tiers are pro). The friends' choice is the code's preference order, an idle lane first then the most room then the first by name, not the fixed a-before-b. A card taken back from a friend is never dealt to her again (takenBack, the take's taken_from), checked by NeverBackToTheSameFriend. A hard pin is released past its bound while its friend is not up and is offered on as a named pin is (stops.go PinReleased), so OnlyToItsFriend holds only before the release. Two reversed witnesses: anytier drops the deal's tier gate (fails DealtAdmitted) and dealback redeals a taken-back card to the same friend (fails NeverBackToTheSameFriend). The rebalance's one-tier-up overflow and cheapest-first order (rebalance.go:143-188) are left out: a two-tier model has no tier above pro. friend_take.go and stops.go each gain one comment line citing the module."
   :origin "this card: tla-whopreference-tiers-pins-and-takeback.w2; v1.2.12 inventory item T36, drift read at nova-sprint main 00c54ba2e, 2026-10-11")

  (fix "tla-stopreturn-settle-by-beat-as-the-code-reads" :release "v1.2.12" :status "planned"
   :title "StopReturn's SettleByBeat reads the beat's live set and stop-returns count as the code does, in the code's three steps"
   :text "Models only, no product change. tla/StopReturn.tla's SettleByBeat returned and settled in one step, read the beat as a set of running job ids, and carried a ghost guard (d.id \\notin old) the code cannot read. The beat now names the jobs it runs and the reports it holds at their generation (LiveNames, sprint.LiveNamedAt), and a beat that carries no live set reports a stop-returns count (StopReturnsOwed, sprint.Beat.StopReturns); the settle reads the count for a count-only beat and the live set otherwise, the ghost guard is gone, and the settle is the code's three steps -- ReadSettle (the unfenced read), SettleByBeat (the stop-return) and Settle (the settle) -- so NoLiveChildAcrossStart holds with a beat fresh at the read. The reversed witness MCStopReturnBrokenOmitLive (Broken \"omitlive\") lets a fresh beat omit a live (running) child, so the settle returns it and START crosses the live child, failing NoLiveChildAcrossStart. A beat gone stale between the read and START is the v1.2.14 card stop-debt-stale-beat-loses-a-held-report, not modelled here. internal/sprint/store/steps.go StopReturnStep gains one comment line citing the module. tla/RUNS.tsv and tla/COVERAGE.tsv are made current."
   :origin "card tla-stopreturn-settle-by-beat-as-the-code-reads, 2026-10-11: v1.2.12 inventory item T13, at nova-sprint main 00c54ba2e")

  (fix "tla-stopreturn-clear-cases" :release "v1.2.12" :status "planned"
   :title "StopReturn clear stops first and refuses an advance over a live lease"
   :text "Models only, no product change. Clear now takes STOP as its first step from RUNNING, then
    checks the current working leases before advancing, also on a retry while STOPPED. The split
    preserves a return receipt between STOP and the lease read. ClearEnabled checks reachability
    from RUNNING and STOPPED with no working lease; ClearNeverWithALiveLease records whether an
    advance crossed a working lease. MCStopReturnClear passes both; BrokenClearBlocked drops only
    the live-lease guard and fails ClearNeverWithALiveLease. All seven stopreturn cases pass inside
    110 seconds, preserving the existing safety and liveness assertions. Readers-table leases,
    generation-pin changes and settle-by-beat changes remain separate cards."
   :origin "this card, tla-stopreturn-clear-cases.w5, 2026-10-11: v1.2.12 inventory item T13; carry attempt 4 redone on current main")

  (fix "cover-gc-loop-and-backup-out-refusals" :release "v1.2.12" :status "planned"
   :title "Unit tests for the gc loop and backup --out's step refusals"
   :text "cmd/nova-sprint's gcLoop (gc.go) had no test of its own behaviour, and backupOut.run's step
    failures were untested (its happy path and mid-dump cases were). gc_loop_behaviour_test.go drives
    a.gcLoop in a testing/synctest bubble: nothing before gcLoopFirst, the first local run at it with
    why=first and its class and summary lines prefixed, no second run within sprint.GCDue's hour though
    the loop wakes every gcLoopEvery, an inventory that errors said once until it answers, and a
    cancelled context ending the loop. backup_out_refusals_test.go breaks one field of backupOut a test:
    --out a regular file, a source that gives no dump, xz and split failing, no throwaway store, a twin
    whose restore holds other counts, no secret scan seat, a source with no state reader (the counts-only
    integrity result and its README), and an existing empty --out replaced; each refusal leaves no --out
    and no private work directory. Measured at main bf7bfe2: gcLoop 54 of 54 and backupOut.run 36 of 129
    statements not run, unchanged at main 00c54ba; after: gcLoop 88.9% and run 77.5% covered."
   :origin "the owner, 2026-10-11: v1.2.12 tests and models only; inventory section 8 item C7; card cover-gc-loop-and-backup-out-refusals.w2")

  (fix "every-worker-verb-runs-as-its-worker" :release "v1.2.12" :status "planned"
   :title "Every worker verb runs as its worker with no --actor"
   :text "TestEveryWorkerVerbRunsAsItsWorkerWithNoActor drives every verb whose class is classWorker through its shortest valid line with no NOVA_SPRINT_ACTOR and no --actor, holding each is not refused for want of an actor and, where it writes, that the record names the worker the verb named. The rows come from verbClasses itself, so a worker verb added to the class table with no case fails the test with add a case for <verb>. remind, any actor's, is checked with --actor and refused without one; a worker verb that names no worker is refused and writes nothing."
   :origin "the owner, 2026-10-11: the v1.2.12 tests; inventory section 8 item C14 and section 6 (the night's missing regression tests)")

  (fix "ns-cmd-tests-under-a-minute-on-four-cores" :release "v1.2.12" :status "planned"
   :title "cmd/nova-sprint's unit tests under a minute on four cores"
   :text "cmd/nova-sprint's unit tests ran 70-115 s on CI's hosted four-core runner (run 38140543980
     #88 114.7 s, run 38138910199 #87 70.1 s), over the one-minute bound. The wall time is the lander's
     own real tree gate (go build, gofmt, go vet, go vet -tags functional, go test ./internal/docs/) in
     landgo.go, not sleeps: the package injects its clock. Six land tests whose assertions are the
     merges, the gate counts and the worktrees now stand the gate in for the bench with the package's
     own gateBench seam, dropping the subprocesses with no assertion lost (gateRan runs before the
     seam). The long-pole tests that pin the gate's findings (landbase_test.go, land_basegate_count_test.go,
     base_gate_rule_test.go, land_go_test.go, the red-combined and bisect tests, the real-binary build)
     keep the real gate and are left for the next pass, which must supply their gate result from a
     content-aware fake while keeping the finding-text assertions."
   :origin "the owner, 2026-10-11: v1.2.12 inventory card A10 (tmp/v1.2.12-inventory-2026-10-11.md)")

  (fix "drain-a-probed-member-drains-and-its-reader-is-asked-nothing" :release "v1.2.12" :status "planned"
   :title "One test runs the night's composition: a probed member that drains is dealt nothing and its reader is asked nothing"
   :text "A new store test, TestAProbedMemberThatDrainsIsDealtNothingAndItsReaderIsAskedNothing in
    internal/sprint/store/drain_test.go, runs the composition the drain tests ran only in halves. Using
    flashSetup (both members up with EnvProbePassed and reader-m1 up, readers a, b and c away), m1
    drains after passing its probe; over ticks readyWork(m1) stays empty and readsOn(reader-m1) holds
    no read asked after the drain. When reader-a is set up the reads waiting for review are asked of
    it; when m1's beat no longer says it drains, m1 is dealt again and reader-m1 is asked again. The
    assertions go through the store's steps and the snapshot only. The comment of
    TestADrainingReadersAskedReadsReturnToAnotherReader now says it runs the reader's own drain and
    names the new test for the machine's. Tests only; no product change."
   :origin "this card; the v1.2.12 inventory card A2 and section 6 (three drain tests red at 04:16Z on 2026-10-11, the probe gate and #84, greened by 2623ab7 with no test of the composition)")

  (fix "tla-matching-models-say-what-they-hold" :release "v1.2.12" :status "planned"
   :title "The four matching models' headers say what their code does"
   :text "Four TLA+ models whose actions already match their code had wrong headers and citations; the
    headers are corrected and the code cited, and no model action or invariant changed (Timer's Set is
    made strict, d > now, to match the code's due time strictly after now; no case's result changes).
    DealFill: a fault is a presence down marked held_by=fault with fault_since, set by the beat-lapse
    down alone and lifted by faultClear after a beat and an ok finish; a harness fault or a failed take
    holds by HeldByEnv, and the one person value stands for the coordinator's, HeldBySync, HeldByAdopt
    and HeldByEnv marks; faultCleanFinish compares second-resolution stamps, so a finish in the hold's
    own second counts, which the model's ok reset forbids (an abstraction). Level: the callers are the
    level op and fleet up's comeUp, not held.go, and the model's ref is StagingRefusers alone, an
    abstraction of the code's avoid set (refusers plus bench plus notLaunching). RetireAbsent: the
    fleet_retire.go citation called FleetRowsMatchConfig the invariant (it is a leads-to; RecordsKeepRows
    is the invariant) and misspelled LiveRetire as LiveRets; an answered read that names no record
    retires nothing, which the code is stricter about than the model's failed-read guard. Timer: the due
    test counts running time and a due time must be strictly after now. Each module already had a
    reversed witness, so none is added. tla/COVERAGE.tsv notes say the same, and the four groups
    (dealfill, level, retire-absent, timer) were rerun with TLC and merged into tla/RUNS.tsv."
   :origin "this card; v1.2.12 inventory item T36 (the deal/route/rules group), drift read at nova-sprint main 00c54ba2e, 2026-10-11")
  (fix "tla-land-gate-ring" :release "v1.2.12" :status "planned"
   :title "The gate bench's hash ring modelled in tla/GateRing.tla"
   :text "tla/GateRing.tla models the land stage's gate-slot ring: two up benches, the start slot the
    stream hashes to, the per-pass skip record (SkipAfter), the ring stepped to the next slot on a
    refusal, the lane always given back, and the local fallback (run here) refused as a remote-bench
    unavailability whenever another member is configured. It proves AskedAtMostSkipAfter (a host is
    asked at most SkipAfter times a pass after refusing), NoLocalGoWhenRemote (the gate never runs in
    the clone while another member is configured), LanesGivenBack, and RefusedStepsToNextSlot (a
    refused stage steps the gate to the next slot). Two reversed witnesses: noskip (the ring ignores
    the skip record) and localwhenremote (the fallback runs the gate here even when remote). Models
    only; no product change."
   :origin "this card; the v1.2.12 inventory items T31 and section 5 (the land stage's gate-slot ring)")

  (fix "tlc-runs-inside-the-job-wall" :release "v1.2.12" :status "planned"
   :title "TLC runs inside the job wall"
   :text "tools/tlacheck is built in this repository on this tree's pkg/tlc, with the groups
    (--root, --stale), inputs, run (--root, --jar, --java, --dir, --group, --shards/--shard,
    --timeout, --workers, --manual) and merge (--root, --keep, --out, then the runs) verbs, and the
    Makefile tlc, tlc-full, tlc-groups and tlc-test targets. Since the split the runner lived in
    nova-tools and refused this root (the two pkg/tlc trees differ in suite.go, which this tree
    builds against pkg/safepath), so no worker in nova-sprint could run TLC, make tlc named a target
    that did not exist, and some cards hand-wrote RUNS.tsv rows. The runner now runs where the cards
    run. Its cmd_checks.go (the table-model suites over internal/tablemodel) and bench.go's --bench
    are not carried: this repository has neither."
   :origin "this card")

  (fix "tla-workimport-scoped-verify" :release "v1.2.12" :status "planned"
   :title "WorkImport models a scoped import and verify, and a wider verify never reports done"
   :text "Models only, no product change. tla/WorkImport.tla's Import and Verify each take a scope, a
    subset of the repositories: the import writes only its scope's issues and records the scope in
    the tree (:scope), verify reads the tree's own scope by default and refuses a --repo outside it
    before any GitHub call, so a verify never reports done outside the import's scope. TreeIsWhole
    is restated as TreeIsWholeForItsScope (the tree is whole for the import's scope), and the new
    invariant VerifyNeverPassesOutsideItsScope states a wider verify never reports done. The receipt
    and Close/Export are marked design-only in the header, as the code's verify prints facts and
    stores none. Six reversed witnesses (noreceipt, stale, external, noreopen, partial, scoped):
    Broken \"scoped\" ignores the tree's scope, so a wider verify reports done and fails
    VerifyNeverPassesOutsideItsScope; MCWorkImportBrokenPartial keeps its result
    (TreeIsWholeForItsScope)."
   :origin "this card, tla-workimport-scoped-verify.w6, 2026-10-11: v1.2.12 inventory item T22 and section 9 row D12; drift read at nova-sprint main 00c54ba2e, re-read at the head that records the scope")

  (fix "the-server-beat-accepts-draining-and-no-room" :release "v1.2.12" :status "planned"
   :title "The server's beat takes a draining or no-room member"
   :text "The member's beat is `fleet beat <member> [--stop-returns <n>] [--no-room <why>]
    [--draining] [--live <set>] --load <percent>` (nova-tools pkg/member: Beat, drainArgs,
    noRoomArgs), the optional words in any order with --load anywhere among them. The
    v1.2.11 server's fleetBeatWords took only `<as> [--stop-returns n] [--live set] --load
    pct` with --load last, so a draining member, or one at its disk floor, was refused on
    every beat: superman and batman each had 1,237 --stop-returns beats refused and then
    every --draining beat refused, and superman read down for twenty minutes with six
    children still finishing. The server now takes the member's full word list; an unknown
    word is refused naming it, and a beat without --load is refused because the server
    cannot measure another machine. cmd/nova-sprint/serve_beat_test.go and
    internal/sprint/fleet_beat_test.go pin it here, nova-tools pkg/member/stop_test.go the
    words sent."
   :origin "this card; the seat's child 2026-10-11 ~18:45Z, reading superman's and batman's member logs after the v1.2.11 adopt")

  (fix "tla-seathealth-deleted-health-and-evidence" :release "v1.2.12" :status "planned"
   :title "SeatHealth models the health delete, the rung-5 write, and the beat proof"
   :text "Models only, no product change. tla/SeatHealth.tla was read against internal/sprint/friend_health.go, friend_stall.go and presence.go and drifted in four places, each brought to the code. A friend's health row can be deleted (Clear: friend health --clear, or the stall ladder's rung-5 release), so the row holds no proof to compare and an older proof is then accepted; ProofNeverGoesBack is restated to hold while the row exists. Rung 5 writes the row outside NotHealth (Rung5Down) with Seen = now and the current generation. FriendEvidence makes her up on her beat's proof (Prove) on a fresh beat and never counts a raw beat, and the code's three evidence windows (pong 10 m, proof 15 m, finish 30 m) are one DownAfter. OldSeatNeverUp is restated over the observation's word alone (the beat proof is not the seat's). A seventh reversed witness MCSeatHealthBrokenClearKeepsRow (Broken \"clearkeepsrow\") deletes the proof but keeps the row observed, then takes an older proof, failing the restated ProofNeverGoesBack; the six existing witnesses keep their results. presence.go and friend_stall.go each gain one comment line citing the module; tla/RUNS.tsv is merged and tla/COVERAGE.tsv notes her beat's down word and a finished card are still outside."
   :origin "card tla-seathealth-deleted-health-and-evidence, 2026-10-11: v1.2.12 inventory item T27 and section 9 (D10), at nova-sprint main 00c54ba2e")

  (fix "the-work-deal-fills-free-lanes-first-round-robin" :release "v1.2.12" :status "planned"
   :title "The deal fills free lanes first, then the room, round the fleet"
   :text "The deal's eligible set was wrong; its cursor was right. Every placement of a work card on a member (dealPlan's next for a new attempt, a redeal and an escalation; downPlan's placement for a held or down member's cards) picked the first member from the deal's cursor below DealAhead times its width, so a member at its width (16 working, 16 < 32) was as eligible as an idle one: on 2026-10-11 hetzner2 sat at 0/16 working and idle while b2 and hetzner1, both 16/16, were each given a ready card. The placement now goes through one function, round.nextFree(up, q, lanes, room, avoid), in two scans from the cursor: first the members with a free lane (q below the width, q the member's ready and working cards, a read at half a slot), and only when no member that may take the card has one, the members with q below DealAhead times the width; the guards each caller passes (launchersOf, StagingRefusers, onlyBench, notLaunching) narrow up before both scans, so a member with a free lane that cannot launch the card's route or refused it at staging is skipped in both. The cursor moves past the member placed, so placements roll ((index++) % n) over the eligible set; TickDeal's widthRoom bound is unchanged. The owner, 2026-10-11 ~19:05-19:20Z: \"Please log cards to fix this deal and rebalance. It's shit.\"; \"cards only may accrue per-fleet or friend, if there are not other machines/friends ready to do that work right now!\"; tla/WhoPreference.tla, DealFreeLanesFirst. The rework's avoid (round.next) and the rebalance are unchanged; the model is a separate card."
   :origin "this card, the-work-deal-fills-free-lanes-first-round-robin.w3, 2026-10-11; code read at nova-sprint main b774f70b")

  (fix "hold-return-leaves-taken-cards-with-their-worker" :release "v1.2.12" :status "planned"
   :title "Keep taken cards with a live fleet member during hold --return"
   :text "A hold of a member whose beat is up hands back only its dealt, untaken ready cards. Taken work and reads stay on the worker at their generation and finish there, even with --return. A down member still returns dead working cards. The hold note names the taken cards that stay."
   :origin "card hold-return-leaves-taken-cards-with-their-worker.w2, 2026-10-11")

  (fix "roadmap-tool-becomes-nova-roadmap" :release "v1.2.13" :status "planned"
   :title "Extract the roadmap tool into nova-roadmap"
   :text "The roadmap and fixes code exists twice: here as internal/roadmap, internal/roadmapdoc,
    tools/roadmap and the nova-work roadmap verbs (about 3,100 lines with their tests), and in
    nova-tools as internal/roadmap, internal/roadmap/sexp and tools/roadmap (about 1,300 lines with
    their tests, about 930 without), with two diverging copies of the generator. Unify the s-expression format first; then one module and one CLI, nova-roadmap, living
    in this repository (a module and cmd/nova-roadmap): it reads and writes the data, renders
    ROADMAP.md and FIXES.md, and runs the sync check. Both repositories use it and delete their own
    copies: this one its internal copies, nova-tools by importing nova-roadmap from this repository.
    Net fewer lines. Its s-expressions stay importable later as a node by nova-work; that import is a
    v1.3 item, not this one."
   :origin "the owner, 2026-10-11: v1.2.13 contraction, pass 4 (shared code into modules); nova-roadmap lives in nova-sprint")
  (fix "briefs-by-reference" :release "v1.2.13" :status "planned"
   :title "Brief boilerplate moves into the card contract"
   :text "A brief names the card contract instead of carrying its text, so briefs are shorter and the
    contract has one copy. The RULES, LEDGERS, WALL AND TOOLS and BUDGET blocks, about 6 KB of every 10 to 11 KB brief, are referenced once from the card contract instead of repeated in each brief, and a card lint refuses repeated instruction text in a brief. Agreed with the owner on 2026-10-11 as a lesson from the Cockroach Labs write-up \"Five months treating bugs like patients\" (MOLT Sinai)."
   :origin "roadmap item briefs-by-reference"
   :kept (:item-group "sprint"
          :item-date "2026-10-09"
          :item-why "A format change."
          :item-area "nova-card"
          :item-cards "1"
          :item-origin ""
          :earlier-title "Briefs carry the card contract by reference"))
  (fix "a-review-loop-has-a-circuit-breaker" :release "v1.2.14" :status "planned"
   :title "A review loop has a circuit breaker"
   :text "A card is escalated to the coordinator after 4 consecutive review rounds that each bring a new blocking finding, or after 6 substantive review rounds of any kind, instead of going round again."
   :origin "the owner, 2026-10-11: a lesson from the Cockroach Labs write-up \"Five months treating bugs like patients\" (MOLT Sinai)")
  (fix "a-red-base-is-pushed-to-the-coordinator" :release "v1.2.14" :status "planned"
   :title "A red base is pushed to the coordinator"
   :text "When the base a card lands onto is red, the machine pushes the coordinator a judgment naming the base and its red run. This is the push half of card a-landing-never-goes-onto-a-red-base; the landing gate itself stays in v1.3."
   :origin "the owner, 2026-10-11: a lesson from the Cockroach Labs write-up \"Five months treating bugs like patients\" (MOLT Sinai)")
  (fix "an-acked-empty-row-is-one-episode" :release "v1.2.14" :status "planned"
   :title "An acknowledged empty row is one episode while it holds"
   :text "emptyConds keys the empty-row episode by its text, and the text was rebuilt from the cards waiting once the judgment no longer sat in Open, so a second ready card she could do changed the text, wrote a second NFriendEmpty judgment and closed the acknowledgement, though the conjunction (she is up, her row empty, cards she could do wait) never cleared. The episode's text now comes from the judgment the coordinator holds (an acknowledgement, or a wait not yet due) as well as from an open one, so tla/CoordinatorPass.tla's OneJudgmentAnEpisode (Kind = \"empty\") holds in the code: the acknowledged episode stays one until the condition clears, a card dealt to her closes it, and a later episode is judged afresh past EmptyRowAfter."
   :origin "v1.2.12 inventory 2026-10-11, section 9 row D9; this card")
  (fix "a-landing-refuses-a-tla-cfg-the-case-plan-does-not-name" :release "v1.2.14" :status "planned"
   :title "A landing refuses a tla/*.cfg the case plan does not name"
   :text "The lander's run-record check held the plan only to its own fields, so a head adding tla/MCFoo.cfg with no tla/CASES.tsv row, or taking a row for a configuration that stays, passed until tlacheck refused the whole plan. recordsRefusals now holds the merged tree to tlacheck's own loader, tlc.LoadCases, after ParseCases, and refuses with one line on tla/CASES.tsv; a tla/CASES.tsv edit alone holds a tree that carries models to its plan. LoadCases names, in name order, every MC*.cfg the plan does not name, every config the plan names that has no file, and every row whose module is missing, keeping its opening words."
   :origin "the seat, 2026-10-11, epoch 16; this card")
   (fix "pagelock-stripper-hardening" :release "v1.2.14" :status "planned"
    :title "The page lock's comment-only check reads the page a browser renders"
    :text "The stripper that proves a comment-only change renders nothing no longer mistakes four visible things for comments: an HTML text comment leaves nothing rather than a space; a comment opener inside a quoted attribute value is code; an inline script with a data-src attribute is read as JS; and a slash after a control if, while or for condition opens a regex, failing closed otherwise. The lock's header and the spec's The page lock state the comment-only form and that the card lint is ask-only."
    :origin "this card")
  (fix "verify-defaults-to-the-trees-import-scope" :release "v1.2.14" :status "planned"
   :title "verify compares the scope its tree was imported with, not the whole organization"
   :text "import --repo <org>/<name> now records the repositories it read as the work-tree's :scope, written only for a scoped import; a whole-organization tree stays byte for byte what it was, and a file without :scope reads as a whole-organization tree. verify with no --repo compares the tree's own scope (tla/WorkImport.tla's Verify already read the scope it imported), --repo narrows within it, and a --repo outside it is refused naming the scope before any GitHub call; verify --against does the same. Before, verify with no --repo read the whole organization and reported MISSING for every repository the scoped import left out."
   :origin "the card verify-defaults-to-the-trees-import-scope w2; the v1.2.12 inventory 2026-10-11 section 9 row D12 (code read at nova-sprint main 00c54ba)")
  (fix "every-producer-card-has-its-consumer-out-after-the-tick" :release "v1.2.14" :status "planned"
   :title "Every producer card has its consumer out after the tick"
   :text "Adds ConsumersOut check at the end of every tick to assert that every producer card (working, fix, review) has its consumer out: an attempt held by an up member/friend for working/fix, or reads asked by up readers for review. Violations are reported as quiet judgments. Also reduces MaxSettle from 64 to 32 as per the owner's design, and changes settle bound handling to push a judgment instead of erroring the tick."
   :origin "the owner, 2026-10-11 ~10:40-11:00 AM ET, on the live epoch 17; card every-producer-card-has-its-consumer-out-after-the-tick.w1")
  (fix "ready-over-reads-the-settled-rests" :release "v1.2.14" :status "planned"
   :title "The ready-over judgment reads the deal's settled rests"
   :text "A ready card whose tier is served only by a route resting at the tick's now raises the ready-over
    judgment (a ready card is not dealt), naming the resting route, whether the provider's recorded rest or
    one the rule rests this tick. The ready-over part settles the rests once as the deal does (withRests),
    on the snapshot the conditions are counted on, so the route the judgment names as resting is exactly one
    the deal does not draw; a rest whose time has ended is not named and the card is dealt."
   :origin "card ready-over-reads-the-settled-rests.w2; the seat's v1.2.14 audit, 2026-10-11")
  (fix "ready-over-key-ignores-the-balance-amount" :release "v1.2.14" :status "planned"
   :title "The ready-over judgment is keyed on the cause, never its figures"
   :text "The ready-over judgment is one per (reason, tier), keyed by the reason the deal leaves the card
    (condKey's NReadyOver case, readyOverWhy). The reason names figures that move while the cause stands --
    a route's balance (out of budget (balance $x)) and a rest's end (resting until <stamp>) -- so as the
    balance moved the key changed, notify closed the old judgment and wrote a second one: one cause, a second
    push, and the coordinator's wait on the first lost. readyOverWhy now takes each moving figure out of the
    key before keying, while the What keeps it, so the judgment is rewritten in place with the latest figures
    and pushed once."
   :origin "the coordinator, 2026-10-11 (v1.2.14 defect ready-over-key-ignores-the-balance-amount); checked against nova-sprint main 00c54ba")
  (fix "tick-watchdog-refuses-a-negative-deadline" :release "v1.2.14" :status "planned"
   :title "run refuses a negative --tick-deadline"
   :text "A negative --tick-deadline is refused right after the flags are parsed, through run's usual refuse helper, before the loop starts or a server listens; the refusal names the flag, the value and that 0 waits for ever. Before, tickDeadlineOf returned 0 for any least <= 0 and tickWithin waited for ever when d <= 0, so --tick-deadline -1s switched the watchdog off with nothing said. 0 still waits for ever and every positive value behaves as today."
   :origin "nova-tools#5173 (transferred here at the split), roadmap item tick-watchdog-bounded-diagnostics, its negative-deadline half; this card")
  (fix "tick-watchdog-exit-never-waits-on-its-dump" :release "v1.2.14" :status "planned"
   :title "The tick watchdog never waits on its own dump"
   :text "An overrun's goroutine stacks are written into memory first (pprof's WriteTo into a bytes.Buffer, which cannot block), then the watchdog acts (the tick's cancel, or the wedged exit), and only then hands the buffer to stderr in a goroutine it waits on at most TickStacksBound (2 s); a stderr nobody drains (a full pipe under a supervisor, a blocked terminal) can no longer hold up the cancel or the exit, and a dump not written within the bound is dropped with one TICK STACKS line saying so."
   :origin "the card tick-watchdog-exit-never-waits-on-its-dump w2; nova-tools issue #5173, roadmap item tick-watchdog-bounded-diagnostics; code read at nova-sprint main 00c54ba")
  (fix "stops-digest-counts-only-held-stops" :release "v1.2.14" :status "planned"
   :title "The stops digest counts only the stops still holding"
   :text "One plan could close a stop and at the same time send an NStopsDigest saying it still holds: notify closes the open NStopMemberDown judgment of a member up again, and stopsDigest then gathered its stops from the snapshot before the plan, counting and naming the stop just closed. stopsDigest now leaves out every judgment the same plan closes, matched by note id; with no stop left it writes neither the digest nor its clock."
   :origin "v1.2.12 inventory 2026-10-11, section 9 row D8; card stops-digest-counts-only-held-stops.w1, id given by the coordinator 2026-10-11 (code read at nova-sprint main 00c54ba)")
  (fix "init-coordinator-moves-the-seat-recorded" :release "v1.2.14" :status "planned"
   :title "init over a keyless store records the seat move, so an earlier observation lapses"
   :text "init writing a coordinator name onto a store whose key holds none while a friend-health record survives (an observation made under an earlier seat) now records the move as a seat change, as the seat's own step does: the seat record and its generation stepped, written with the key, its log line and its note, so the earlier observation lapses by FriendEvidence's generation check. A fresh store's first init writes no seat record, and init over a held key keeps the holder, as before."
   :origin "this card; v1.2.12 inventory 2026-10-11 section 9 row D10 (code read at nova-sprint main 00c54ba)")
  (fix "lander-empty-commit-says-no-result-is-held" :release "v1.2.14" :status "planned"
   :title "The lander's empty-commit refusal says a result is missing when it holds none"
   :text "The lander's empty-commit check refused an empty diff with no result to read (a nil work card: the fleet row absent or the work card tidied away) with the same finding as one whose result claimed changes, \"empty commit: the result claims changes the diff does not show\", naming a claim nothing made. It now returns emptyCommitNoResultFinding, \"empty commit: the head changes nothing from the attempt's start, and the lander holds no result to say why\", and both findings return the card to review through one helper, isEmptyCommitFinding."
   :origin "this card: lander-empty-commit-says-no-result-is-held, from the v1.2.14 point-release plan and the roadmap item lander-empty-commit-handling")
  (fix "a-hook-push-is-sent-only-once-recorded" :release "v1.2.14" :status "planned"
   :title "A hook push is sent only once its record names it"
   :text "When the hook record write fails, a look now delivers none of that look's pushes and undoes what it added (the note keys it marked sent, the pushes it added to the open list, and the sequence), so the next look offers them again, records them, and sends them once. The SEAT HOOK line it prints is unchanged. Before, the push was delivered while the record named no unacknowledged push, so a push delivered over a failed record write never blocked the gate: past HookAckBound a coordinator command ran though the push had been delivered and was never recorded."
   :origin "the card a-hook-push-is-sent-only-once-recorded w1; the v1.2.12 inventory 2026-10-11 section 9 row D7 (code read at nova-sprint main 00c54ba)")
  (fix "a-fault-down-keeps-another-holds-mark" :release "v1.2.14" :status "planned"
   :title "A machine's fault down keeps another's hold mark"
   :text "When presence takes a member down for a missed beat, the machine writes its own fault hold (held_by=fault, fault_since) only on a member that carries no hold: a member the sync, the adopt, its environment or the coordinator already holds keeps its hold, its mark, its reason and its finish marker when it goes down, and still goes down. Overwriting the mark hid the hold from its own release, so a held member could no longer be released and the clear left a markless hold (tla/DealFill.tla FaultHold and PersonHoldStays; docs/SPEC-SPRINT.md section 5)."
   :origin "the seat's audit, 2026-10-11 (a reader finding on card a-member-environment-fault-holds-it-and-pushes-the-seatb, attempt 5, head 38c66ea7542f); this card")
  (fix "load-cache-mark-not-read-after-chunks" :release "v1.2.14" :status "planned"
   :title "A load over more than one read set reads the table's mark"
   :text "readInto read a table of more than ntable.LimitReadSetMembers records in several read sets and checked only that each saw the table's revision, so a store that lost writes and wrote others up to the same revision between two read sets (a restart from an older save, a failover) gave one revision and two histories and the load mixed them. It now reads the mark of the change event that left the table's revision (TableChangesMarked, as markOf reads it) before the first read set and after the last, and a mark that changed is a movedError, as a changed revision is, so the load reads again under its existing retry. A table of at most 1024 records, or a backend that keeps no marks, reads as before (tla/LoadCache.tla, LoadIsSnapshot)."
   :origin "the v1.2.12 inventory 2026-10-11, section 9 row D5; code read at nova-sprint main 00c54ba")

  (fix "version-features-answers-what-a-build-has" :release "v1.2.14" :status "planned"
    :title "The version verb says which fixes a build carries"
    :text "nova-sprint version --features lists the fixes this build carries, one line each
     `<release> <fix-id> <title>`, from the fixes ledger compiled into the binary (docs/embed.go) and
     read with make roadmap's parser: every fix of a release at or below the build's version. A build
     whose version is not a release (a dev build) lists the shipped releases' fixes and says the
     unreleased fixes of the tree it was built from are not listed. nova-sprint version --has
     <fix-id> exits 0 when the build carries the fix and 1 when the ledger names it in a later release
     or not at all, naming which; --json prints {version, fixes:[{release,id,title}]}."
    :origin "tmp/next-sprint-cards/TOOLING-FROM-THE-SEAT-2026-10-11.md item 16 (the owner, 2026-10-11); the v1.2.10 bisect, where the coordinator ran strings on two binaries to learn whether v1.2.9 held the environment-fault fix")

(fix "doctor-reads-nova-config-through-the-seat-wrapper" :release "v1.2.11" :status "planned"
   :title "doctor reads nova-config from the caller's environment"
   :text "doctor now runs where it is typed, not on the sprint server, so its nova-config read uses the caller's NOVA_PG_DSN and NOVA_PG_PASSWORD_ENV. This matches the behavior of fleet sync and friend sync, which also read the config store with their caller's credentials."
   :origin "this card")

 (fix "land-clash-test-sets-git-identity-in-clones" :release "v1.2.11" :status "planned"
   :title "The clash test sets a git identity in every clone it commits in"
   :text "The clash test configured user.name and user.email in its first temporary repository only; the clones it makes do not inherit that config, so their commit failed with exit 128 on a machine without a global git identity. The test now sets GIT_AUTHOR_*/GIT_COMMITTER_* for the whole test, the way the package's other git-using tests set an identity."
   :origin "this card")

  (fix "live-reads-systemd-units-and-launch-daemons" :release "v1.2.11" :status "planned"
   :title "live reads systemd units on Linux and LaunchDaemons on macOS"
   :text "The live manifest read only ~/Library/LaunchAgents, so a Linux host refused with no agents directory and a loop installed as a system LaunchDaemon was never listed. live now reads the nova-loop-*.service systemd units of the user and system unit directories on Linux, each an interval agent when a .timer sits beside it, and, on macOS, the com.nova.* launchd daemons of /Library/LaunchDaemons too; each agent records its manager (launchd or systemd)."
   :origin "this card")

  (fix "hook-finds-the-server-itself" :release "v1.2.11" :status "planned"
   :title "The hook and the coordinator's verbs find the server the seat's record names"
   :text "app.server (cmd/nova-sprint/forward.go) read only NOVA_SPRINT_SERVER, so a seat
    installed with `seat install --server <host:port>` still had to export the variable by
    hand before `hook` or any coordinator verb would run; every other seat verb answered
    'You must hook in first'. When the environment names no server, app.server now uses the
    server the seat's record names (a.seatServer), in the same order seat check already used
    (machinery.go). A process that is the server, or a verb given its own --redis, still
    returns none. hook prints the server it used and where it came from
    (server=<host:port> from=env | from=seat-record), and when neither source names one the
    refusal names both and the commands that set one."
   :origin "the seat, 2026-10-11 ~03:19Z: the seat hook proved only with NOVA_SPRINT_SERVER=127.0.0.1:6390 set by hand")

  (fix "a-refused-verb-is-logged-with-its-refusal" :release "v1.2.11" :status "planned"
   :title "A refused verb is written on the server's own log, with its refusal"
   :text "serveCtx now writes one SERVE REFUSED line for every verb it answers with a non-zero code,
    whether the worker's verb (a refused finish), one the server does not run (exit 2) or a lane verb
    (a friend's beat, a read): the verb word, the worker, the exit and the card argument when the verb
    names one, with the verb's own first stderr line cut to 300 bytes, never the verb's whole argv or
    its stdout. The same verb, worker, exit and first line repeated within a minute is written once,
    and when it changes the line that follows says (repeated <n> times since <stamp>), so 40 minutes
    of refused finishes from ten members is a few hundred lines and never one per verb. The SERVE
    tally counts the refused verbs by verb word and its line says refused=<n>
    refused-by=<verb>:<count>,...; no new verb. On the v1.2.10 server every done-path finish was
    refused for about 40 minutes and the log held no line for a refused verb, so the refusal could
    not be read."
   :origin "this card; the owner, 2026-10-11: everything that has been hard work in this bisect and rollback, make it better with tools")

  (fix "no-deal-to-a-draining-member" :release "v1.2.10" :status "shipped"
   :title "No deal to a draining member; a held or draining machine's reader is asked nothing"
   :origin "PR #84")

  (fix "dashboard-is-the-live-page" :release "v1.2.10" :status "shipped"
   :title "The dashboard is the live page, byte for byte; the page lock and the card lint hold it"
   :origin "PR #83")

  (fix "dashboard-comments-name-the-owner" :release "v1.2.10" :status "shipped"
   :title "The page's comments name the owner as the owner; the lock takes a proven comment-only change"
   :origin "PR #85")

  (fix "main-green-after-the-night-landings" :release "v1.2.10" :status "shipped"
   :title "Main green: four semantic conflicts between the night's landings resolved"
   :origin "PR #87")

  (fix "two-ci-flakes-text-file-busy-and-git-maintenance" :release "v1.2.10" :status "shipped"
   :title "Two CI flakes: exec'd stubs written under the fork lock, and git's auto maintenance off in tests"
   :origin "PR #88")

  (fix "a-stats-reset-zeroes-the-unattributed-spend" :release "v1.2.14" :status "planned"
   :title "A stats reset zeroes the unattributed spend"
   :text "After nova-sprint stats reset every stream's spend counts from the mark, but the grand total
    did not reach zero: the cost line's <other>, the largest TierCosts.Unreconciled on any stream's
    record, is the sprint's UnreconciledSpend since the epoch's clear, never since the mark. ResetMark
    now holds the sprint's unattributed spend at the mark (in cents) and streamsSince subtracts it on
    every stream's record, the streams the mark does not know included, so the cost line, the dashboard's
    cost tile and where --json's unreconciled count from the mark, never below zero. The mark line names
    it beside the total."
   :origin "the seat's v1.2.14 audit, 2026-10-11 (a stats reset does not zero the grand total spent),
    checked against nova-sprint main 00c54ba; the owner's rule of 2026-10-09: a stats reset clears every
    cost figure, the total spent, per card and per tier; this card a-stats-reset-zeroes-the-unattributed-spend")

  (fix "stop-debt-stale-beat-loses-a-held-report" :release "v1.2.14" :status "planned"
   :title "A stale post-STOP beat keeps its word for the settle"
   :text "settleStopDebtByBeat read a machine's live set fresh-only (sprint.LiveNamesAt through
    liveOf), so a machine that beat once after the STOP naming its held report and then went silent
    past BeatDeadline had its lease settled at START: the card went back to ready at gen+1, was dealt
    again, and the held report was refused as stale when it came. The owner's last beat since the STOP
    now keeps its word fresh or stale (sprint.LiveNamedAt, no clock), as tla/StopReturn.tla
    SettleByBeat reads it (d.id \\notin beat); tla/LiveRuns.tla's reversed witness \"staleasabsent\" is
    exactly that wrong reading: a silent row's stale beat as an empty live set. LiveNamesAt, LiveHeldAt,
    liveOf and LiveReturns keep their fresh-only reading. No model change."
   :origin "the coordinator 2026-10-11, added to v1.2.14 beside the v1.2.12 inventory's section 9 row
    D2 (a held finished report across STOP refused as stale); D2's first cause was fixed by
    nova-sprint #87 (15c779c); this is what that fix left; code read at nova-sprint main 00c54ba")
   (fix "fleet-up-probe-releases-an-environment-held-member" :release "v1.2.11" :status "planned"
    :title "fleet up --probe passed releases a member held by an environment fault"
    :text "fleet up --probe passed on a member held by an environment fault (held_by=environment) was
     refused by the store's manifest check: the release path and EnvProbePatch each unset the hold's four
     fields, so one entry named each twice. A field is now unset once per entry (unsetPresent keeps each
     name once, in first-seen order), and fleet up's own unset of the hold is left to EnvProbePatch when
     the passed probe clears the fault, so a coordinator's, the fault's or the sync's hold stays for a
     person to lift (the probe never lifts a person's hold). tla/ProviderBudget.tla, FleetUpRefusesUncleared,
     already said a passed probe clears the fault; this is the implementation catching up."
    :origin "this card")

  (fix "card-lint-a-hold-owner-every-reader-accepts" :release "v1.2.14" :status "planned"
   :title "Every held_by write names a hold mark, and each mark keeps the member off the deal"
   :text "Tests only, no product change. internal/sprint/hold_marks_test.go holds a table of every
    mark a member's held_by carries -- none (the coordinator's), the sync's, the machine's fault, the
    adopt's and the environment's -- each written by its own writer (a fleet hold with its HeldBy,
    presence's down for the fault, holdAdopting for the adopt, a finish whose report is an environment
    fault for envFaulted) and cleared by its own release (fleet up, the sync's release, faultClear,
    clearEnvFault, the adopt's finish). For each it pins the behaviour: the member is not up and dealt
    no card by the deal or the level, a tick presence with a fresh beat leaves held and held_by as they
    were, another mark's mark-scoped release leaves it held with its mark, and its own release clears
    it. It then scans the package's non-test .go files for every held_by assignment (a name[FieldHeldBy]
    = X and FieldHeldBy: X) and fails naming the value and its file:line when no table row names it, so
    a new hold kind cannot ship without the table checked against it. The known-yes proof is
    envFaulted's status line deleted: the environment row then fails at UpMembers."
    :origin "this card, card-lint-a-hold-owner-every-reader-accepts.w1, from the reader finding on
     a-member-environment-fault-holds-it-and-pushes-the-seatb attempt 5 (head 38c66ea7542f): the
     environment hold was written with held_by=environment but the member's status was left Up, so
     the level dealt to it and presence wrote held_by=fault over the mark")
   (fix "a-brief-edit-records-its-needs-and-the-deal-checks-them" :release "v1.2.14" :status "planned"
    :title "A brief edit records its needs, and the deal refuses a card whose needs have not landed"
    :text "A card's needs are only the column's, not the brief's: brief re-points needs only through
     dependsOnly (the DEPENDS-ON line the only difference), and any other edit took the ordinary path,
     wrote the brief and dropped the parsed needs, so on 2026-10-11 a brief --dir moved 85 briefs that
     now carry DEPENDS-ON while every card kept zero needs and stayed ready; and dealPlan's eligibility
     checked stream held, friend pin and inState(c, Ready) only, no guard on needs, so a ready card with
     unlanded needs was dealt. Now the ordinary brief path writes the needs the new brief's DEPENDS-ON
     names whenever they differ from the card's, clearing them when the brief drops its DEPENDS-ON; a
     ready card that gains a need not landed moves ready ->
     waiting in the same unit (the lifecycle accepts the move, ready -> waiting being no longer only a
     sentinel's effect when the edit records needs); dealPlan refuses a card with WaitsFor(s, c, nil)
     non-empty, and unmet judges every move to dealt (Working) against the card's stored needs, so Lawful
     refuses dealing a card whose needs have not landed whatever path asks for it; a card dealt or working
     keeps its attempt and the new needs apply from its next deal. help brief says this. Red tests in
     internal/sprint/needs_guard_test.go (a ready card whose brief gains an unlanded need waits with its
     needs recorded; a waiting or ready card whose edit names landed needs stays or becomes ready; a ready
     card with an unlanded need built in the store is never dealt and the tick's invariant names it; an
     edit with no needs change leaves them; a brief that drops its DEPENDS-ON clears the card's needs).
     The invariant a primary anywhere but waiting has every need
     landed already named such a card; this is the deal and the brief catching up (docs/SPEC-SPRINT.md,
     needs and the deal)."
    :origin "this card, a-brief-edit-records-its-needs-and-the-deal-checks-them.w3, from the owner on
     the live epoch 17, 2026-10-11 ~10:40-11:00 AM ET: 89 v1212 cards ran or sat ready before their
     DEPENDS-ON landed (four failed four attempts each); code read at nova-sprint main 1405d26; the
     owner: \"by definition this must not happen.\"")

  (fix "card-lint-refuses-a-header-key-below-the-prose" :release "v1.2.14" :status "planned"
   :title "The card lint refuses a header key below the prose"
   :text "cardHeaderBlock ends the card header at the first prose line (\"ended = true // the prose begins
    here\") and CardHeaderValue reads only that block, so a header key written below the prose was never
    read and nothing said so: only the five typed keys (KIND, PATHS, TEST, LEGS, SOURCE) drew a stranded
    finding. A new swarm.LintHeaderStranded reuses cardHeaderBlock and returns one finding per header key
    (KIND, PATHS, TEST, LEGS, SOURCE, START, STOP, DEPENDS-ON, PRIORITY, NEW, SHARED, REPO, BASE, RESULT,
    tier) written as a KEY: line below the block, naming the key and its line; a key in the header passes
    and a sentence that merely mentions a key mid-line draws nothing. lintBriefReads calls it, so add,
    brief, recut and preflight refuse a stranded key through the same path as every other lint finding,
    exit 2 (preflight exit 1), nothing written. LintCardDepends (which nothing outside tests called) is
    deleted; its stranded-DEPENDS-ON finding is now this one, and the package doc and lineup helpers
    (Lineup, CardDependsRemedy, ReadLineup and their exclusive readers) that only it used are removed
    with it. This is the bug behind the 2026-10-11 epoch
    17: 89 v1212 briefs carried DEPENDS-ON: below the REPO:/BASE: sentence, briefNeeds read no needs from
    any of them, add admitted every card straight to ready, and cards were dealt before their dependencies
    landed."
    :origin "this card: card-lint-refuses-a-header-key-below-the-prose, from the owner on epoch 17,
     2026-10-11 (~10:40-11:00 AM ET); code read by the seat's reader at nova-sprint main 1405d26; the
     owner: \"this should be caught by card lint.\"")

   (fix "needs-set-repoints-a-cards-needs-in-place" :release "v1.2.14" :status "planned"
    :title "needs set re-points a card's needs in place"
    :text "The only way to change a card's needs after admission was drop and re-add, so a card admitted
     with the wrong needs needed a twin. A card's needs are read once at add (briefNeeds) and a brief edit
     drops them on any change beyond the DEPENDS-ON line, and on 2026-10-11 ~100 frontier cards could not
     be re-pointed or reworked without twins. Now nova-sprint needs set <id> (--needs <a,b> | --clear)
     [--answers <note>] is the direct verb, the coordinator's alone: it rewrites the line the brief names
     its needs on (Needs: or DEPENDS-ON:, keeping that line's key; --clear takes the line off; a brief
     with no such line gains a DEPENDS-ON: line) so card <id> shows the truth, and it moves the card by
     the one lifecycle table: a ready card that gains a need not landed moves ready -> waiting, a waiting
     card whose new needs have all landed moves waiting -> ready, a card working or in review keeps its
     attempt with the new needs applying from its next deal (the brief edit's briefDepends path), and a
     landed or off-table card, a sentinel, and a need that is no primary on the table (named whole) are
     refused, exit 1, nothing written. The path is chosen by the card's column, not by whether it has
     ever been dealt: a card returned to waiting or ready in place after an attempt takes the lifecycle
     move and keeps its attempt field, so it is never sent to drop and re-add a twin. unmet now judges
     a move waiting -> ready by the needs the unit
     writes, not the ones the pre-state holds. help needs names it (docs/SPEC-SPRINT.md, needs). Red tests
     in cmd/nova-sprint/needs_set_test.go pin all seven: ready -> waiting with the move printed, waiting ->
     ready on landed needs, --clear emptying a waiting card to ready, a need not on the table refused
     naming it with nothing written, a dealt card keeping its attempt, a landed and a dropped card refused,
     and the brief's DEPENDS-ON line rewritten with no other line changed."
    :origin "this card: needs-set-repoints-a-cards-needs-in-place.w3 (attempts 1-2 carried), from the owner
      on the live epoch 17,
     2026-10-11 ~11:25 AM ET (\"I hate twins.\" / \"Twinning is the last refuge of the scoundrel. Fix your
     tools.\"); code read at nova-sprint main 1405d26; seat tooling item 26")
   (fix "a-read-is-never-asked-onto-a-resting-route" :release "v1.2.14" :status "planned"
    :title "A read is never asked onto a resting route while another of its tier serves"
    :text "readRouteOf (internal/sprint/route.go), which draws a read card's route from the
     read tier's array, built its served set from every enabled route of the tier and left out
     only avoid (the routes a read of the primary returned on), so it never asked whether a route
     rests. The read-card ask could then put a read on a route resting for its 5xxs or its
     provider's credit, and the next pass's readCardsTakeBack retired it as RetiredByRest because
     cardRest said its route rests, while the ask put it back on the same route, so reads churned
     ask, retire, ask for as long as the rest held even though another route of the tier could
     serve. readRouteOf now leaves out the routes that rest at s.Now while another route of the
     tier serves, adding them to the set preferFirst avoids and holding them only while another
     entry remains drawable, so with every route of the read tier resting the ask draws the first
     as it did before and the take-back still handles it (no new refusal, no new judgment).
     readCardsAskFull settles the view's rests once with withRests() when it holds none, so no
     read scans the fleet table and s.resting sees them (docs/SPEC-SPRINT.md section 6, the reads;
     tla/RouteIndex.tla, THE READS). Red tests first in
     internal/sprint/read_route_rest_test.go, on a pro primary whose read tier's array is pro-a,
     pro-b: with pro-a resting (a 3-in-10 rule rest, and a rate-limit backoff from one
     rate-limited take record) the read the ask creates carries pro-b's FieldRoute, never
     pro-a's; with both routes resting the reads are still asked a route, as today."
    :origin "this card, a-read-is-never-asked-onto-a-resting-route.w1, from the fourth cold read
     of nova-sprint PR #86 (2026-10-11), an open non-blocking finding (readRouteOf ignores rests)
     listed in tmp/next-sprint-cards/rate-limit-never-fails-the-card-v2.md; checked on PR #86's
     head a1c51ee and on main 00c54ba, where readRouteOf is at route.go:813 and holds for every
     rest, not only a backoff.")

  (fix "a-friend-that-serves-a-tier-counts-as-its-route" :release "v1.2.14" :status "planned"
   :title "A friend that serves a tier counts as its route"
   :text "Whether a tier is served is one question with one answer: tierServed says a tier is served by an
    enabled, unrested route of it, or by a friend row up and unheld whose tiers include it (a member row
    likewise when the fleet's tiers allow), and every site that validates a tier reads it. routeTier's
    frontier branch refused every frontier card before it consulted the check, so rework of a frontier card
    was refused, held.go and idle.go raised \"no route serves tier frontier\", readers.go and ready_over.go
    read the same words as its cause, and judgments_retire.go retired the judgment only when a route
    appeared, while the friends' deal already handed frontier cards to a friend row whose tiers include
    frontier. A frontier card is now the friends' by design: a friend up whose row lists frontier serves it
    as a route would, and with no such friend the refusal names both the routes and the friends it looked
    at. docs/SPEC-SPRINT.md, tiers and routes, says it. Tests:
    TestReworkAcceptsAFrontierTierAFriendServes, TestAcceptSeesAFrontierTierAFriendServes,
    TestIdleRootForAFrontierTierAFriendServesIsNoNoRoute, TestAFrontierTierNoFriendServesIsRefused,
    TestAFrontierTierAFriendServesRaisesNoNoRouteJudgment."
   :origin "this card; the owner, 2026-10-11 ~11:25 AM ET, on the live epoch 17: \"I hate twins.\" /
    \"Twinning is the last refuge of the scoundrel. Fix your tools.\"; code read at nova-sprint main
    1405d26")

  (fix "the-dashboard-plays-a-coin-when-a-tick-lands" :release "v1.2.14" :status "planned"
   :title "The dashboard plays a gentle coin when a tick lands a card"
   :text "The dashboard at :7390 gains one sound and nothing else, on the owner's ask: when a refresh's
    page copy carries a landed count higher than the refresh before's, the page plays one short coin --
    two sine partials, the second a fifth above the first, about 0.25 s, a fast decay, peak gain 0.15 --
    synthesized in app.js with WebAudio, so no audio file joins the repository and the page makes no extra
    request. It plays once per refresh however many landed in it, never on the first load and never when
    the count falls (a clear); the tick id the read carried is the copy's at, so two reads of one tick
    play nothing. A browser refuses audio before a gesture, so the page carries one small speaker glyph
    beside the landed count, off until clicked once per browser (the choice kept in localStorage); clicked
    on, it plays the coin once, as the test. One paragraph in docs/SPEC-SPRINT-DASHBOARD.md, \"The coin\",
    and the lock's ask line with pagelock.txt's digest. Tests sound_test.go (the coin's rules against
    the shipped app.js) and page_test.go (the glyph and the inline synthesis)."
   :origin "this card, the-dashboard-plays-a-coin-when-a-tick-lands.w1, 2026-10-11: the owner,
    2026-10-11 ~12:50 PM ET: \"Please add sounds to the sprint dashboard. Whenever a card lands in the
    tick (1 or more), play a single gentle 'ca-ching' sound, like a coin dropping. Keep it subtle.\"")

  (fix "a-5xx-mentioning-rate-limit-still-rests" :release "v1.2.14" :status "planned"
   :title "A 5xx whose words mention a rate limit still spends the take and rests the route"
   :text "IsRateLimited (internal/sprint/provider_funds.go) answered true for class rate-limited or a 429, then
    fell back to words in the whole line (\"too many requests\", \"rate limit\", or \"concurrency\" with
    \"exceed\") whatever the class, so a provider's own 5xx whose message mentioned a rate limit (for example
    `provider: class=provider-5xx status=503 msg=upstream rate limit exceeded, retry later`) counted as one. On a
    limited route the finish then took the rate-limited return in internal/sprint/steps_work.go: the card went back
    unspent, with no FieldTakeEnded and no provider take record, so transient() never counted it, and three such
    5xxs in ten never rested the route for RouteRestFor, reading a real outage as a brief backoff. Now, when the
    line parses a class of class5xx or classTimeout, only class=rate-limited or status=429 makes it a rate limit and
    the word fallback is skipped; a line with no parsed class, or another class, keeps the word fallback. Tests:
    internal/sprint/rate_limit_class_test.go pins the class table (true for class=rate-limited, a 429 under another
    class and a classless \"Too many requests\"; false for class=provider-5xx or class=timeout whose msg says
    \"rate limit\" or \"too many requests\", and for an out-of-credit 429) and the behaviour through Deal, Take and
    Finish (three class=provider-5xx \"rate limit\" takes each spend their take, leaving FieldTakeEnded set, a
    FieldProviderTake record and no rate_take_ record, and RestsDue then rests the route for RouteRestFor with
    cause RestProvider). No model change."
   :origin "this card, a-5xx-mentioning-rate-limit-still-rests.w1, follow-up 2 of
    tmp/next-sprint-cards/rate-limit-followups.md (the second cold read of nova-sprint PR #86, 2026-10-11); checked
    against PR #86's head a1c51ee, the code this card's dependency rate-limit-never-fails-the-card-v2 lands on
    main")
  (fix "a-rate-limited-refusal-never-fails-the-cardc" :release "v1.2.14" :status "planned"
   :title "A rate limit stalls its route briefly; it never fails a card or rests it for 3-in-10"
   :text "A take a provider ends rate-limited (class=rate-limited) never fails the card, on any
    route and however many times: the card returns unspent (no FieldTakeEnded, no redeal, no
    attempt, no failed-work judgment, none of the out-of-credit bound FieldRateReturns) and leaves a
    rate_take_<n> record numbered after the last one the card holds, with no gap and no bound, so
    RateTakes reads every one. The route backs off briefly, then serves again by itself: RestsDue
    computes the backoff purely from those records, k being the route's rate-limited ends since its
    last ok end (a take the worker finished ok, in the finished cell or past it; a failed finish
    resets nothing) and last the latest one's time, ending at last + min(60s*2^(k-1), 15m) (RateBackoff)
    -- a fixed point in time, never relative to now. Its line is kept beside the route's own rest in
    the provider's rule3_rest_ property (<route>+rate-backoff), never over it, so it never moves the
    3-in-10 window's mark, never blocks the coordinator's rest and is never woken; a backoff on every route of a tier makes the card
    wait ready, as a provider at its concurrency budget does, never a no-route judgment, and the no-stall rule holds it on the backoff whenever a route of
    the tier backs off and none can be drawn (never NStalled); the rebalance never moves a card
    onto a machine with no route it can draw;
    restWrites writes
    it and one happened note (\"rate-limit backoff of <d> on <route>, until <t>\") once per end,
    none while the same end holds, never a judgment, and drops a backoff line once its end has passed. A rate limit no longer counts toward the 3-in-10
    transient rest (transient excludes classRateLimited); a 5xx window still rests the route 30
    minutes, and is written over a backoff that holds. member.Judge treats a rate-limited run with no
    pushed head as a provider failure whatever its shape, while a shaped result that pushed a head
    stays the card's own. TLA+ model RateBackoff, properties RateLimitSpendsNoRedeal and
    BackoffBounded with the temporal BackoffEnds, reversed witnesses MCRateBackoffBrokenSpend and
    MCRateBackoffBrokenNoCap. Tests TestARateLimitedTakeReturnsTheCardUnspent,
    TestARateLimitBacksTheRouteOffBriefly, TestARateLimitedRunIsAProviderFailure,
    TestARateTakeIsNumberedAfterTheLastRateTake, TestARateBackoffNeverHoldsOffTheFiveHundredsRest
    and TestARateBackoffIsWrittenAndNotedOncePerEnd. Landed by hand after 15 sprint attempts. The
    no-stall rule and the rebalance test drawability with the deal's own budgets and the tier's array
    (backoffWait): tests TestABackedOffTierWithTheOtherProviderFullIsAWaitNeverAStall and
    TestABackedOffTierWithTheOtherRouteOutOfTheArrayIsAWaitNeverAStall."
   :origin "the owner, 2026-10-11 ~00:20Z, after the fleet stalled (35 ready, 7 up, 0 working, every pro/heavy route resting): \"How can we fix it so this doesn't happen moving forward\"; the owner, 2026-10-11: \"the rate limited should just stall out delay, not fail\"; nova-sprint docs/fixes.sexp entry a-rate-limited-refusal-never-fails-the-cardc; nova-sprint card rate-limit-never-fails-the-card-v2")
    (fix "friend-unstarted-level-honours-dealt-no-more" :release "v1.2.14" :status "planned"
   :title "The unstarted level gates on friendDealable"
   :text "friendUnstartedLevel moves stale cards (not started past FriendStartMax) from one friend to another with an idle lane. It now checks friendDealable(s, up[n]) on the recipient before moving a card. This prevents cards from being moved to a friend marked dealt-no-more (who returned cards without taking), a reader-only friend (no builder role), or when the friends' work is off (friends off). The deal already gates on friendDealable; the level's gate ensures every placement follows the same rule (docs/SPEC-SPRINT.md section 1, a friend's card). Tests: TestTheUnstartedLevelMovesNoCardToAFriendDealtNoMore, TestTheUnstartedLevelMovesNoCardToAReaderOnlyFriend."
   :origin "the coordinator, 2026-10-11 (v1.2.14 defect friend-unstarted-level-honours-dealt-no-more); checked against nova-sprint main 00c54ba")

(fix "friend-give-and-take-say-what-they-mark" :release "v1.2.14" :status "planned"
   :title "friend give hands a named card, and take says the mark it writes"
   :text "friend take --all-unstarted wrote a mark it did not print (taken_from: not dealt to that friend again until friend give) and printed only FRIEND-TAKE OK moved=<n>, so a row that went quiet said nothing; and friend give was only the undo of the mark, so the seat had no verb to hand a named ready card to a friend and the deal drew the queue head instead. FriendTake now appends to each taken card's line \"<id>: back to ready, not dealt to <friend> again until friend give\" (not for the hold, which keeps no card) and plans a summary counting them (marked=<n>, a said line); FriendGive, on a ready card never taken back and never dealt, hands it to the friend named -- the deal's own unit, the work card placed on her fleet row at once with its primary working -- says \"<id>: handed to <friend>\", and clears the mark of a card taken back as it did, the deal filling the rest. A card not ready, one whose tier her row does not serve, and one whose needs have not landed are refused naming why. help friend says take is take back and mark and give is hand or unmark, and the SPEC and FRIENDS docs say the seat hands a named card and the deal fills the rest."
   :origin "this card: friend-give-and-take-say-what-they-mark.w1; the owner, 2026-10-11 ~11:15-11:40 AM ET, driving the coordinator's own friend lane (a 32-card take-back left the row quiet for 10 minutes and five cards could not be put there); code read at nova-sprint main 1405d26")
   (fix "a-backoff-withdrawal-writes-no-note-per-card" :release "v1.2.14" :status "planned"
    :title "A rate-limit backoff withdrawal writes no note per card"
    :text "A rate-limit backoff is a rest the deal withdraws the ready cards it holds from
     (restWithdrawals, Cause RestRateBackoff), but the backoff already notes itself once per new
     end (restWrites, the one happened note \"rate-limit backoff of <d> on <route>, until <t>\").
     The withdrawal added one NRestWithdrawn note per card, so one backoff on a route with N ready
     cards drowned its own note under N more. restWithdrawals now drops the NRestWithdrawn note
     from the unit it builds when the rest's cause is RestRateBackoff (filtering the unit's Notes);
     the withdrawal, its Moved words and its unspent redeal are unchanged, and a 5xx rest still
     notes each withdrawn card as before (tla/RateBackoff.tla; docs/SPEC-SPRINT.md section 5, the
     deal). Tests TestABackoffWithdrawalWritesNoNotePerCard and
     TestAFiveHundredsRestWithdrawalStillNotesEachCard."
    :origin "follow-up 4 of tmp/next-sprint-cards/rate-limit-followups.md, the second cold read of
     nova-sprint PR #86, 2026-10-11; checked against PR #86's head a1c51ee and main 00c54ba")
  (fix "doctor-warns-while-a-tier-backs-off" :release "v1.2.14" :status "planned"
   :title "Doctor warns, not fails, while every route of a tier backs off from a rate limit"
   :text "A route a rate limit backs off is not usable, so DoctorRoutesPerTier counted it resting
    with every other cause: a tier whose every enabled route backed off, holding ready cards with
    no friend serving it, was FAIL, and its remedy pointed at the providers lines that end a rest,
    though a backoff ends by itself. A rest of cause RestRateBackoff is counted apart (backs) and
    named in the evidence as backoff=<route> until <time>; a tier whose every unusable route is
    backing off is at most WARN, and its remedy says the backoff ends by itself
    (tla/RateBackoff.tla, BackoffEnds; docs/SPEC-SPRINT.md, the doctor's routes-per-tier check). A
    rest of any other cause still FAILs, and a tier with one route backing off and another a
    member up can launch is unchanged. Tests TestDoctorRoutesPerTierWarnsWhileATierBacksOff,
    TestDoctorRoutesPerTierFailsWhileATierRestsForIts5xxs and
    TestDoctorRoutesPerTierWithOneBackoffAndOneUsableWarns."
   :origin "this card: doctor-warns-while-a-tier-backs-off; the third cold read of nova-sprint PR
    #86, 2026-10-11, an open non-blocking finding: \"Doctor reports FAIL while every route of a
    tier backs off\"; listed in the rate-limit-never-fails-the-card-v2 card; checked against
    nova-sprint main 00c54ba and PR #86's head a1c51ee")
   (fix "doctor-names-a-member-whose-link-flapped" :release "v1.2.14" :status "planned"
    :title "Doctor names a member whose link flapped in the last hour"
    :text "On 2026-10-11 the coordinator found by hand, in b1's and b3's kernel journals, that
     their network links had gone down and up, and no check named it. The member probe now reads
     one more fact before its end marker (cmd/nova-sprint/doctor_probe.go): on Linux the kernel's
     link changes in DoctorLinkWindow (one hour), journalctl -k -q --no-pager --since \"-1h\"
     filtered by one extended case-insensitive grep for the drivers' link words (Link is Up, Link
     is Down, link up, link down, carrier lost, carrier acquired), printed as \"link <count> <the
     last such line, cut to 160 bytes>\", and on Darwin \"link - not measured on darwin\"; when
     journalctl is absent or cannot read the kernel journal (no lines and a non-zero exit, or the
     insufficient-permissions line) it prints \"link - <why>\". parseProbe reads it into
     MemberProbe (LinkChanges, -1 when not measured; LinkLast; LinkWhy), and DoctorMemberEnv gives
     a member whose link counted DoctorLinkWarnChanges or more changes in the window one WARN,
     never a FAIL, naming the member, the count, the last line and the remedy. doctor --preflight
     replaces its links WARN with the same fact as a gate: a member with that many changes is one
     FAIL row (area preflight, check links), so a switch or adopt is refused while a member's link
     flaps, and a member not measured stays a WARN. No secret is printed and the journal window is
     short, never the whole journal. Tests TestProbeScriptHasTheLinkFactAndQuotesNothingFromConfig
     and TestParseProbeReadsTheLinkFact (cmd/nova-sprint/doctor_probe_link_test.go),
     TestDoctorMemberEnvWarnsOnAFlappingLink and TestDoctorMemberEnvQuietOnOneOrNoLinkFact
     (internal/sprint/doctor_link_test.go); no test runs ssh or journalctl. docs/SPEC-SPRINT.md,
     the doctor's member-env check and the preflight, says the link fact and its bound."
    :origin "this card: doctor-names-a-member-whose-link-flapped.w1; tmp/next-sprint-cards/TOOLING-FROM-THE-SEAT-2026-10-11.md items 7 and 20 (the owner, 2026-10-11: \"Whatever you are doing manually now, consider adding tool support for it to nova sprint.\"); the coordinator found by hand, in b1's and b3's kernel journals, that      their links had gone down and up")
   (fix "route-ends-read-the-finished-cell" :release "v1.2.14" :status "planned"
    :title "routeEnds reads the Finished column too"
    :text "routeEnds (internal/sprint/route_rest.go), which builds the ended takes the 3-in-10 rest
     window counts, walked fleet.Column(Ready, Working, DoneOK, DoneFailed, DoneDefect, Withdrawn)
     and read a card's own finish only for DoneOK and DoneFailed, so a work card in the Finished
     column added nothing to its route's window: neither the provider takes it recorded before it was
     redealt nor its own finish. A 5xx that preceded a card that finished and waits for the readers'
     verdict went uncounted until the verdict moved the card on. routeEnds now walks Finished too and
     counts a Finished card's own finish exactly as a DoneOK or DoneFailed one, the same take number,
     so a card counts once wherever it sits: when it moves on from Finished it is the same card and
     the same record (tla/RouteRest.tla; docs/SPEC-SPRINT.md section 5, the routes' rests: \"each work
     card's own finish\"). rateBackoffs already read Finished; the window's size, the rest's length,
     the provider refusal rests and rateBackoffs are untouched. Tests
     TestRestsDueReadsAFinishedCardsTake and TestAFinishedCardsEndCountsAsAnEndedTake;
     TestRestsDueCountsTransientProviderFailuresNeverNoResults and
     TestARateBackoffNeverHoldsOffTheFiveHundredsRest stay green."
    :origin "this card, route-ends-read-the-finished-cell.w1; the third cold read of nova-sprint PR
     #86 (2026-10-11), an open finding marked main's own: \"routeEnds (the 3-in-10 window) does not
     read the Finished column\"; listed in tmp/next-sprint-cards/rate-limit-never-fails-the-card-v2.md;
     checked on main 00c54ba (route_rest.go near line 376) and on PR #86's head a1c51ee")
  (fix "routes-shows-a-rate-limit-backoff" :release "v1.2.14" :status "planned"
   :title "routes shows a route backing off from a rate limit"
   :text "`nova-sprint routes` printed rested_until=- for a route backing off from a rate limit, so
    a held route read as serving while the deal drew nothing on it. After
    rate-limit-never-fails-the-card-v2, RouteRests reads rule3Rests, which drops every
    <route>+rate-backoff line on purpose (the backoff line is restWrites' alone), so the route's
    backoff, computed by the unexported rateBackoffs and held while now is before its Until, never
    reached the verb. A new exported read in internal/sprint/rate_backoff_view.go,
    RateBackoffsAt(fleet, now), returns the backoffs rateBackoffs computes that hold at now and
    nothing else, so the deal's path is unchanged. cmdRoutes merges it into its rests (cmd/nova-sprint/
    reads.go, cmdRoutes and restAt): a route resting for its 5xxs or its provider's credit prints as
    today, and when a rest and a backoff both hold the one that ends later is shown, as RouteRests
    already picks between a route's rest and its provider's. --json's rested_for begins
    \"rate-backoff: \". Tests TestRoutesShowsARateLimitBackoff (a withdrawn card holding one
    FieldRateTake record prints the backoff's end in the ROUTE line and in --json, and both show -
    once the clock passes the end) and TestRestAtShowsTheRestThatEndsLater, in
    cmd/nova-sprint/routes_backoff_test.go (docs/SPEC-SPRINT.md, the routes verb; tla/RateBackoff.tla,
    BackoffEnds)."
   :origin "this card, routes-shows-a-rate-limit-backoff.w2, from the first cold read of nova-sprint
    PR #86 (2026-10-11), an open non-blocking finding: \"The backoff is hard to see: nova-sprint
    routes doesn't show it\"; listed in tmp/next-sprint-cards/rate-limit-never-fails-the-card-v2.md.
     Checked against main 00c54ba and PR #86's head a1c51ee.")
  (fix "a-refused-rework-names-the-backoff" :release "v1.2.14" :status "planned"
   :title "A refused rework or redo names the backoff, never a wait"
   :text "A coordinator's rework or redo deals on the soft budgets (withSoftBudgets): on the soft
    draw a full provider is dealt anyway, but a route backing off from a rate limit never is. When
    every route of the tier backs off the draw fails and routeOf returned its waiting words
    (\"every route of tier <t> ...: the card waits ready and is dealt when a slot frees or the
    backoff ends\"), which Rework and Redo refuse the card with, telling the coordinator it waits
    ready when in fact the rework did not happen and no deal resumes by itself. routeOf's full
    branch now returns different words when the draw is soft (s.budgets != nil && s.budgets.soft):
    every route of tier <t> that serves is backing off from a rate limit (<route> ..., until
    <end>): a rework or redo is never dealt onto a backing-off route, so the card was not dealt;
    give it again after that time. A deal (not soft) keeps the waiting words, since its card does
    wait ready. Tests in internal/sprint/rate_backoff_rework_test.go:
    TestAReworkOnABackedOffRouteIsRefusedNotToldItWaits and
    TestARedoOnABackedOffRouteIsRefusedNotToldItWaits pin the refusal (no \"waits ready\", names the
    backoff's end, says give it again); TestADealOnABackedOffRouteStillTellsTheCardItWaits pins the
    deal's words; TestASoftReworkWithAFullProviderIsStillDealtOnIt pins that a soft rework with a
    full provider and no backoff is still dealt on the full one (docs/SPEC-SPRINT.md section 5, the
    deal; tla/RateBackoff.tla, BackoffEnds)."
   :origin "this card, a-refused-rework-names-the-backoff (the third cold read of nova-sprint PR
    #86, 2026-10-11): \"A refused rework or redo gets the misleading wording 'waits ready ...
    backoff ends'\"; listed in tmp/next-sprint-cards/rate-limit-never-fails-the-card-v2.md, checked
    against PR #86's head a1c51ee.")
  (fix "take-by-id-starts-the-card-as-a-lane-take-does" :release "v1.2.14" :status "planned"
   :title "take by id starts a friend's card as a lane take does"
   :text "The two forms of take did different things. take --as <friend> --lane <n> moved the card to
    working through the lane's move (laneSet), so it carried the lane and, on a friend's row, her start
    (friendStartSet) and the tick left it working. take --as <friend> <card>@<gen> by id moved it with
    friendTaken instead, which set no start, so the next tick put it back ready (friendUnstartedWorking)
    and her progress was refused \"not working\"; a lane take then re-dealt the card at a new generation
    and her finish was refused stale. The by-id take now goes through the lane's move too
    (sprint.NamedLaneTake, TakeReq.LaneByID): each named card is taken by takeOne -- so every guard a
    take by count holds holds for it too (the row's seat and width, the card's live generation, its
    route rest and the providers' fleet-wide concurrency budget) -- and is then stamped as a lane's take
    stamps the card it is given (laneSet: the lane when --lane <n> names one, the row's lane heard from
    now, and on a friend's row her start), so both forms of the verb write working, holder, lane and
    generation the same way. A member's by-id take is unchanged and names no lane. TAKE-BY-ID says a
    LANE line per card as the lane's take does. On 2026-10-11 the seat lost two takes and one finish to
    this."
   :origin "this card: take-by-id-starts-the-card-as-a-lane-take-does.w2, from the owner,
    2026-10-11 ~11:15-11:40 AM ET (\"How can we distribute work to friends, if you can't even
    distribute it to yourself?\"); code read at nova-sprint main 1405d26")

  (fix "a-lane-take-honours-the-rank" :release "v1.2.14" :status "planned"
   :title "A lane take honours the rank"
   :text "rank <id> --first moves a card to the front of its stream's line and the deal fills a
    friend's dealt set in that order (the priority ladder, then the stream turns, then work order
    within a stream), but take --as <friend> --lane <n> drew the lane's next ready card in
    takeTurns order, which orders by stream name and inside a stream by score and ignores the
    priority ladder, so a critical card dealt last was passed over and a card a later stream's
    lane take never saw the deal's own order. A new friendDealtOrder is the one order a friend's
    dealt set is filled and taken in (the ladder by a card's rank, then the stream turns over a
    card's stream field, then SortCards within a stream), used by the friend deal pass
    (friendDealPass) and by a friend's take (takeOne, through s.friendWorkRank, which reads the
    primary's current level so a priority change after the deal is honoured too); FriendLaneOrder
    is its exported face and the LANE line prints the held card's position in the deal's order.
    The member take is unchanged. Reproduced on BASE: a critical card dealt last, a rank --first
    card passed over by a critical one, and a critical card of a later stream all sat while an
    earlier normal card was taken."
    :origin "this card: a-lane-take-honours-the-rank.w4; the owner driving the coordinator's own
     friend lane by hand from the seat on 2026-10-11 ~11:15-11:40 AM ET (\"How can we distribute
     work to friends, if you can't even distribute it to yourself?\" / \"Make these fixes
     critical.\"); five ranked-first cards sat while lanes started lower-ranked ones; code read at
     nova-sprint main 1405d26")

  (fix "the-lander-merges-evidence-ledgers" :release "v1.2.14" :status "planned"
   :title "The lander merges the evidence ledgers entry by entry"
   :text "The generated ledgers docs/roadmap-evidence/fixed-<leg>.sexp are one entry per card,
    regenerated by the repository that owns them, the same kind the lander already merges for
    docs/fixes.sexp and FIXES.md, but the lander treated a conflict there as the card's own and
    reworked the head at the tip, a full attempt and read lost per landing (2026-10-11, about 120
    schema cards). The lander now merges the evidence ledgers entry by entry (both kept, in file
    order, no duplicate, add/add and content conflicts alike), reads the one ledger set from
    cardgen.LedgerInPaths (the evidence ledgers and any path a repository's own catalog names as a
    ledger with a LEDGERS line in its AGENTS.md or nova.sexp, read at staging, included), and a
    malformed entry stops the stream with the conflict-kind ledger, never a rework."
   :origin "this card, the-lander-merges-evidence-ledgers.w4; the owner, 2026-10-11 ~12:20-12:45 PM ET")
  (fix "a-stall-on-a-provider-budget-says-so" :release "v1.2.14" :status "planned"
   :title "A ready card whose every serving route is at its provider's budget is held, not stalled"
   :text "The no-stall rule read the concurrency budget two ways. The deal never deals past a provider's budget (Deal draws on withBudgets, internal/sprint/rate_budget.go; drawRoute skips a route whose provider is full, route.go), so a card whose every route of its tier that serves is full waits ready by design. The rule did not: held.waits' Ready case asked routeOf with no index (a check of the tier, which by design never treats a full provider as unserved) and newHeld built its snapshot without withBudgets, so s.budgets was nil; with a member up with room it returned \"a member is below its room (DealAhead times its width), and nothing deals it\" and ok=false, a stall for a card that waits for a slot (the seat's inbox pass, 2026-10-11 epoch 16: j1102, j1111 and j1115 raised it on heavy and pro cards while nova-sprint routes showed LIMIT concurrent deepseek budget=8 in_use=8; each cost a manual routes read to see the limiter was doing its job). newHeld now settles the deal's budget view once per held reading (withBudgets), and held.waits, after the noLauncher check and before the room count, holds a ready card whose every route of its tier that serves is at its provider's budget (budgetWait, rate_budget.go) with ok=true and the why naming each such route and its provider's count. A tier with a free route keeps today's path: room above the cards ahead and still undealt is a stall, as before, so a real deal defect is still judged. Tests in internal/sprint/held_test.go: TestAReadyCardWhoseEveryRouteIsFullIsHeldNotStalled (one heavy card ready, a member up with room, concurrent 1 on the only serving provider with one card of it working, the other heavy route resting: held by the limiter, no Finding), TestAReadyCardWithASlotFreeAndUndealtIsStillStalled (the budget raised to 2, a slot free, and a real deal defect still raises the same stall) and TestARouteOutsideTheTierArrayIsNotServed (an enabled heavy route on a provider with room that the tier's array leaves out is never drawn, so the card still waits for a slot). budgetWait walks the deal's own array and service test (tierArray, servedRoute) and names the routes for a member that serves one (unservedWhy), so what serves and what is full are read exactly as the deal reads them. docs/SPEC-SPRINT.md (section 8, No silent stops; section 9 item 12, the no-stall rule's holders) states it; rate_budget.go and held.go cite docs/SPEC-SPRINT.md section 8 and tla/RateBudget.tla (StalledIsGranted)."
   :origin "this card, a-stall-on-a-provider-budget-says-so.w2, attempt 2 of a-stall-on-a-provider-budget-says-so.w1 (draft tmp/next-sprint-cards/a-stall-on-a-provider-budget-says-so.md); the seat's inbox pass, 2026-10-11, epoch 16, judgments j1102, j1111 and j1115; code read at nova-sprint main 0b151f58")
  (fix "the-landing-gate-runs-its-classes-side-by-side" :release "v1.2.15" :status "planned"
   :title "The landing gate runs its classes side by side"
   :text "Every landing gate ran sprint.BaseClasses one class after another: build, gofmt, vet,
    vet -tags functional, then three separate `go test` processes for the internal/ci analyzer
    classes (staticcheck, errcheck, dead-code), then class-tests and the unit packages. The two
    vets and the analyzer tests only read the tree and none depends on another's output. The
    three analyzer classes are now one `go test -tags functional -count=1 -timeout 600s -run
    '^(TestStaticcheckFindings|TestUncheckedErrors|TestDeadCode)$' ./internal/ci/` run, one test
    binary for the tree, and a red one is attributed to the class its `--- FAIL:` line names. A
    class carries a Stage int (0 build; 1 gofmt, vet, vet-functional, analyzers; 2 class-tests
    and unit); gateRuns returns the runs by stage, the local gate runs each stage's runs
    concurrently, waits the stage and reports the first red in class order (not finish order),
    and the bench gate runs each stage's runs as background jobs and prints them in class order.
    The report order is the class order."
   :origin "this card, the-landing-gate-runs-its-classes-side-by-side.w2; the owner, 2026-10-11:
    \"What else is like serial reads, going out one at a time, when they could go out at the same
    time in || ?\" and \"log critical cards to fix all these serial things that should be
    parallel\"; found by the seat's audit child on nova-sprint main")
  (fix "tlc-suite-runs-independent-cases-in-parallel" :release "v1.2.15" :status "planned"
   :title "Run independent TLC cases in a bounded parallel pool"
   :text "RunSuite gives each selected case its own Java process, scratch and log while a bounded
    worker pool runs independent cases concurrently. `--parallel` defaults to one quarter of the
    bench CPUs and refuses widths above half; Makefile tlc and tlc-full pass TLC_PARALLEL when set.
    Each result keeps its case wall time, records stay in plan order, and OnCase callbacks are
    serialized in completion order. The shared 110 second suite deadline and budget-exhausted
    records remain in force. The real land group passes at width one and the default width with
    identical PASS/FAIL results."
   :origin "this card, tlc-suite-runs-independent-cases-in-parallel.w1~17.g7, 2026-10-11")))
