---------------------------- MODULE AttemptsEpochs ----------------------------
\* The sprint's epoch and clear (docs/SPEC-SPRINT.md section 13, "Epochs and
\* clear"; internal/sprint/store/epoch.go, clear.go, backend.go, redis.go,
\* mem.go, engine.go): four tables bound to one epoch hash; a clear advances
\* the epoch once, blanks the new epoch's streams and readers, and carries the
\* fleet's members over; the old epoch stays where it is and readable, and
\* every writer still holding it is refused as stale.
\*
\* A work card's attempt, generation and redeals (internal/sprint/steps_work.go
\* deal, redeal, nextGen, liveGen; stop_return.go StopReturn; live.go
\* LiveReturns; lane_hold.go laneGoneUnits; rules.go Progress): a launch pushes
\* to the branch sprint/<prefix><primary>.w<attempt>.g<gen>.e<epoch> (packet.go
\* BranchOf), so the branch is one per attempt, generation and epoch; and a
\* report (take, finish or progress) that names a generation other than the
\* card's live one is refused as stale (liveGen). The redeal cap's judgment and
\* the tier ladder are DirtyTick's (RedealsAreEndedTakes, RedealBoundHolds) and
\* SprintRules' (LadderClimbs) and are not here.
\*
\* The state: epoch, the sprint's epoch n; owed, the restore marker (the epoch
\* hash's restore field: true when a clear has advanced and not yet settled);
\* streams, readers and members, the per-epoch tables; pinned, the epoch the
\* one writer holds (its step pinned when it read); clears and cfrom, two
\* clears in flight, each with the epoch it read to advance from (the CAS);
\* ever, the epochs whose streams the writer ever wrote its card into (a ghost
\* for OldEpochReadable); attempt, the primary's attempt; card, whether the
\* work card of this attempt exists; gen, its live generation; redeals, its
\* redeal count; takeEnded, a take of it ended without a finish
\* (sprint.FieldTakeEnded); launched, the current generation pushed already (a
\* launch is one push); launches, how many pushes each branch suffix has had;
\* accepted, the <<named, live>> generation pair of every report taken.
\*
\* The design, Broken = {}:
\*   PinStep   a step pins the store to the sprint's current epoch (every
\*             command builds its store pinned; a step whose epoch moved
\*             re-pins, engine.go:555-562).
\*   WriteStep the writer's step writes its card into the streams table at the
\*             epoch it pinned, and is refused as stale when the sprint has
\*             left that epoch (engine.go:566-575): it writes nothing.
\*   Clear     two clears race on the CAS (AdvanceEpoch, clear.go:96): each
\*             reads the epoch it will advance from (ClearRead), the winner
\*             advances by one and settles the restore in the same step
\*             (ClearAdvance: streams and readers blank, members carried,
\*             clear.go:114-121), and a clear whose epoch another advanced
\*             past writes nothing (ClearRefuse, clear.go:100-110).
\*   Restore   the pin's restore (epoch.go:54, clear.go:184-222): when the
\*             epoch still owes its restore, the next pin copies the old
\*             epoch's streams, readers and members into the new one. A clear
\*             settles the restore it sets in its own step, so no crash cut
\*             between AdvanceEpoch and SettleEpoch is modelled here: owed is
\*             never observed, and Restore never fires.
\*   Deal      a new attempt's work card, attempt + 1 and generation 1, redeals
\*             0: refused when the card exists already (deal: "work card
\*             exists").
\*   Redeal    a withdrawn card dealt again: the same attempt, generation + 1,
\*             and a redeal counted only when a take of it ended (take_ended).
\*   GenStep   the other generation steps (a stop-return, the live reconcile, a
\*             gone lane): generation + 1, redeals unchanged, the same attempt.
\*   TakeEnds  a take of the card ended without a finish: take_ended is set.
\*   Report    a report names a generation: taken when it names the live one
\*             (the card finishes), else refused as stale.
\*   Launch    the card's push to its branch, one per generation.
\* Reversed witnesses, each caught by one property below:
\*   "noepochcheck" the write drops the epoch check: OldEpochWritesNothing
\*                   fails.
\*   "keepgen"      nextGen keeps the generation, so a redeal's push meets the
\*                   first launch's branch: SuffixUnique fails.
\*   "nogenguard"   a report of an old generation is taken: StaleGenRefused
\*                   fails.
EXTENDS Naturals, FiniteSets

CONSTANTS Card,      \* the writer's card
          Reader,    \* a reader row of the readers table
          Member,    \* a member row of the fleet
          MaxEpoch,  \* the epoch bound
          MaxAttempt, \* the attempt bound
          MaxGen,     \* the generation bound
          MaxRedeals, \* the redeal count bound
          Broken      \* the reversed witnesses in effect

VARIABLES epoch, owed, streams, readers, members, pinned, clears, cfrom, ever,
          attempt, gen, redeals, card, takeEnded, launched, launches, accepted

vars == <<epoch, owed, streams, readers, members, pinned, clears, cfrom, ever,
          attempt, gen, redeals, card, takeEnded, launched, launches, accepted>>

Epochs == 0..MaxEpoch
Clears == {"A", "B"}

\* the branch suffix: the attempt, the generation and the epoch a launch pushes to
Suffix == (1..MaxAttempt) \X (1..MaxGen) \X Epochs

TypeOK ==
    /\ epoch \in Epochs
    /\ owed \in BOOLEAN
    /\ streams \in [Epochs -> SUBSET {Card}]
    /\ readers \in [Epochs -> SUBSET {Reader}]
    /\ members \in [Epochs -> SUBSET {Member}]
    /\ pinned \in Epochs
    /\ clears \subseteq Clears
    /\ cfrom \in [Clears -> Epochs]
    /\ ever \in [Epochs -> BOOLEAN]
    /\ attempt \in 0..MaxAttempt
    /\ gen \in 0..MaxGen
    /\ redeals \in 0..MaxRedeals
    /\ card \in BOOLEAN
    /\ takeEnded \in BOOLEAN
    /\ launched \in BOOLEAN
    /\ launches \in [Suffix -> Nat]
    /\ accepted \in SUBSET ((1..MaxGen) \X (1..MaxGen))
    /\ (~card => gen = 0 /\ ~takeEnded /\ ~launched)

Init ==
    /\ epoch = 0
    /\ owed = FALSE
    /\ streams = [e \in Epochs |-> {}]
    /\ readers = [e \in Epochs |-> IF e = 0 THEN {Reader} ELSE {}]
    /\ members = [e \in Epochs |-> IF e = 0 THEN {Member} ELSE {}]
    /\ pinned = 0
    /\ clears = {}
    /\ cfrom = [k \in Clears |-> 0]
    /\ ever = [e \in Epochs |-> FALSE]
    /\ attempt = 0
    /\ gen = 0
    /\ redeals = 0
    /\ card = FALSE
    /\ takeEnded = FALSE
    /\ launched = FALSE
    /\ launches = [s \in Suffix |-> 0]
    /\ accepted = {}

\* a step pins the store to the sprint's current epoch (a re-pin of a moved
\* step, engine.go:555-562; every command pins before it acts)
PinStep ==
    /\ pinned # epoch
    /\ pinned' = epoch
    /\ UNCHANGED <<epoch, owed, streams, readers, members, clears, cfrom, ever,
                  attempt, gen, redeals, card, takeEnded, launched, launches, accepted>>

\* the writer's step writes its card at the epoch it pinned; a step whose
\* epoch moved is refused as stale and writes nothing (engine.go:566-575)
WriteStep ==
    /\ "noepochcheck" \in Broken \/ pinned = epoch
    /\ Card \notin streams[epoch]
    /\ streams' = [streams EXCEPT ![epoch] = streams[epoch] \cup {Card}]
    /\ ever' = [ever EXCEPT ![epoch] = TRUE]
    /\ UNCHANGED <<epoch, owed, readers, members, pinned, clears, cfrom,
                  attempt, gen, redeals, card, takeEnded, launched, launches, accepted>>

\* the pin's restore: when the epoch still owes its restore, copy the old
\* epoch's shape into it (epoch.go:54, clear.go:184-222). A clear settles the
\* restore it sets in its own step, so owed is never observed and this action
\* never fires; the crash cut that would is a v1.2.14 candidate.
Restore ==
    /\ owed
    /\ epoch > 0
    /\ streams' = [streams EXCEPT ![epoch] = streams[epoch-1]]
    /\ readers' = [readers EXCEPT ![epoch] = readers[epoch-1]]
    /\ members' = [members EXCEPT ![epoch] = members[epoch-1]]
    /\ owed' = FALSE
    /\ UNCHANGED <<epoch, pinned, clears, cfrom, ever,
                  attempt, gen, redeals, card, takeEnded, launched, launches, accepted>>

\* a clear reads the epoch it will advance from (clear.go:36-68: the epoch
\* read, the machine stopped and left stopped, then re-pinned)
ClearRead(k) ==
    /\ k \notin clears
    /\ epoch < MaxEpoch
    /\ clears' = clears \cup {k}
    /\ cfrom' = [cfrom EXCEPT ![k] = epoch]
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, ever,
                  attempt, gen, redeals, card, takeEnded, launched, launches, accepted>>

\* the CAS wins (AdvanceEpoch, clear.go:96): advance by one, settle the restore
\* it sets, blank the new streams and readers, carry the members (clear.go:
\* 112-121). The new epoch starts empty, so the card is gone and the attempt
\* begins again. The new epoch's own STOPPED note and the finish-or-abandon of
\* the old pending operation are out of scope here.
ClearAdvance(k) ==
    /\ k \in clears
    /\ cfrom[k] = epoch
    /\ epoch < MaxEpoch
    /\ epoch' = epoch + 1
    /\ streams' = [streams EXCEPT ![epoch + 1] = {}]
    /\ readers' = [readers EXCEPT ![epoch + 1] = {}]
    /\ members' = [members EXCEPT ![epoch + 1] = members[epoch]]
    /\ clears' = clears \ {k}
    /\ attempt' = 0
    /\ gen' = 0
    /\ redeals' = 0
    /\ card' = FALSE
    /\ takeEnded' = FALSE
    /\ launched' = FALSE
    /\ UNCHANGED <<owed, pinned, cfrom, ever, launches, accepted>>

\* the CAS lost: another clear advanced the epoch first, so this one writes
\* nothing (clear.go:100-110)
ClearRefuse(k) ==
    /\ k \in clears
    /\ cfrom[k] # epoch
    /\ clears' = clears \ {k}
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, cfrom, ever,
                  attempt, gen, redeals, card, takeEnded, launched, launches, accepted>>

\* a new attempt's work card: attempt + 1, generation 1; refused when the card
\* exists already
Deal ==
    /\ ~card
    /\ attempt < MaxAttempt
    /\ attempt' = attempt + 1
    /\ gen' = 1
    /\ redeals' = 0
    /\ takeEnded' = FALSE
    /\ launched' = FALSE
    /\ card' = TRUE
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, clears, cfrom, ever, launches, accepted>>

\* a withdrawn card dealt again: the same attempt, generation + 1, a redeal
\* counted only when a take of it ended
Redeal ==
    /\ card
    /\ gen' = IF "keepgen" \in Broken THEN gen ELSE gen + 1
    /\ gen' <= MaxGen
    /\ (takeEnded => redeals < MaxRedeals)
    /\ redeals' = redeals + (IF takeEnded THEN 1 ELSE 0)
    /\ takeEnded' = FALSE
    /\ launched' = FALSE
    /\ attempt' = attempt
    /\ card' = TRUE
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, clears, cfrom, ever, launches, accepted>>

\* the other generation steps (a stop-return, the live reconcile, a gone lane):
\* generation + 1, redeals unchanged, the same attempt
GenStep ==
    /\ card
    /\ gen < MaxGen
    /\ gen' = gen + 1
    /\ redeals' = redeals
    /\ launched' = FALSE
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, clears, cfrom, ever,
                  attempt, takeEnded, card, launches, accepted>>

\* a take of the card ended without a finish (sprint.FieldTakeEnded)
TakeEnds ==
    /\ card
    /\ ~takeEnded
    /\ takeEnded' = TRUE
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, clears, cfrom, ever,
                  attempt, gen, redeals, card, launched, launches, accepted>>

\* a report names a generation: taken when it names the live one, else refused
\* as stale (liveGen)
Report(g) ==
    /\ card
    /\ g \in 1..gen
    /\ (g = gen \/ "nogenguard" \in Broken)
    /\ accepted' = accepted \cup {<<g, gen>>}
    /\ card' = FALSE
    /\ gen' = 0
    /\ redeals' = 0
    /\ takeEnded' = FALSE
    /\ launched' = FALSE
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, clears, cfrom, ever, attempt, launches>>

\* the card's push to its branch, one per generation
Launch ==
    /\ card
    /\ ~launched
    /\ launched' = TRUE
    /\ launches' = [launches EXCEPT ![<<attempt, gen, epoch>>] = @ + 1]
    /\ UNCHANGED <<epoch, owed, streams, readers, members, pinned, clears, cfrom, ever,
                  attempt, gen, redeals, card, takeEnded, accepted>>

Next ==
    \/ PinStep
    \/ WriteStep
    \/ Restore
    \/ \E k \in Clears : ClearRead(k) \/ ClearAdvance(k) \/ ClearRefuse(k)
    \/ Deal
    \/ Redeal
    \/ GenStep
    \/ TakeEnds
    \/ \E g \in 1..MaxGen : Report(g)
    \/ Launch

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* a writer holding an old epoch writes nothing into the new one (section 13:
\* "every writer still holding it is refused as stale: nothing of that epoch
\* lands in the new one")
OldEpochWritesNothing == pinned # epoch => Card \notin streams[epoch]

\* the new epoch is blank except the members: streams and readers are not
\* carried across a clear, and members are carried whole
NewEpochBlank ==
    /\ \A e \in 1..epoch : members[e] = members[e-1]
    /\ \A e \in 1..epoch : readers[e] = {}
    /\ \A e \in 1..epoch : (streams[e] # {} => ever[e])

\* the old epoch stays readable: a card written to an epoch stays there
OldEpochReadable == \A e \in Epochs : ever[e] => Card \in streams[e]

\* the epoch never goes back
EpochMonotone == [][epoch' >= epoch]_vars

\* no two launches share an attempt, a generation and an epoch: the branch a
\* launch pushes to is one launch's own
SuffixUnique == \A s \in Suffix : launches[s] <= 1

\* a report of a generation that is not the live one is refused: every report
\* taken named the card's live generation
StaleGenRefused == \A p \in accepted : p[1] = p[2]

=============================================================================
