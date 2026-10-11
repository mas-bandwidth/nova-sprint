-------------------------------- MODULE AttemptsEpochs --------------------------
\* The sprint's attempt, generation and epoch (internal/sprint/steps_work.go
\* deal, redeal, nextGen, liveGen; stop_return.go StopReturn; live.go LiveReturns;
\* lane_hold.go laneGoneUnits; rules.go Progress). A work card's launch pushes to
\* the branch sprint/<prefix><primary>.w<attempt>.g<gen>.e<epoch> (packet.go
\* BranchOf), so the branch is one per attempt, generation and epoch; and a
\* report (take, finish or progress) that names a generation other than the
\* card's live one is refused as stale (liveGen). The redeal cap's judgment and
\* the tier ladder are DirtyTick's (RedealsAreEndedTakes, RedealBoundHolds) and
\* SprintRules' (LadderClimbs) and are not here.
\*
\* The state, one primary:
\*   epoch     the sprint's epoch, advanced by clear;
\*   attempt   the primary's attempt;
\*   card      whether the work card of this attempt exists;
\*   gen       its live generation (meaningful when card);
\*   redeals   its redeal count;
\*   takeEnded a take of it ended without a finish (sprint.FieldTakeEnded);
\*   launched  the current generation pushed already (a launch is one push);
\*   launches  how many pushes each branch suffix has had;
\*   accepted  the <<named, live>> generation pair of every report taken.
\*
\* The design, Broken = {}:
\*   Deal     a new attempt's work card, attempt + 1 and generation 1, redeals 0;
\*            refused when the card exists already (deal: "work card exists").
\*   Redeal   a withdrawn card dealt again: the same attempt, generation + 1, and
\*            a redeal counted only when a take of it ended (take_ended).
\*   GenStep  the other generation steps (a stop-return, the live reconcile, a
\*            gone lane): generation + 1, redeals unchanged, the same attempt.
\*   TakeEnds a take of the card ended without a finish: take_ended is set.
\*   Report   a report names a generation: taken when it names the live one (the
\*            card finishes), else refused as stale.
\*   Launch   the card's push to its branch, one per generation.
\*   Clear    the epoch advances; the new epoch starts empty, so a card id comes
\*            back on a fresh branch.
\* Reversed witnesses, each caught by one property below:
\*   "keepgen"    nextGen keeps the generation, so a redeal's push meets the
\*                first launch's branch: SuffixUnique.
\*   "nogenguard" a report of an old generation is taken: StaleGenRefused.
EXTENDS Naturals

CONSTANTS MaxAttempt, MaxGen, MaxRedeals, MaxEpoch, Broken

VARIABLES epoch, attempt, gen, redeals, card, takeEnded, launched, launches, accepted
vars == <<epoch, attempt, gen, redeals, card, takeEnded, launched, launches, accepted>>

\* the branch suffix: the attempt, the generation and the epoch a launch pushes to
Suffix == (1..MaxAttempt) \X (1..MaxGen) \X (1..MaxEpoch)

TypeOK ==
    /\ epoch \in 1..MaxEpoch
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
    /\ epoch = 1
    /\ attempt = 0
    /\ gen = 0
    /\ redeals = 0
    /\ card = FALSE
    /\ takeEnded = FALSE
    /\ launched = FALSE
    /\ launches = [s \in Suffix |-> 0]
    /\ accepted = {}

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
    /\ UNCHANGED <<epoch, launches, accepted>>

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
    /\ UNCHANGED <<epoch, launches, accepted>>

\* the other generation steps (a stop-return, the live reconcile, a gone lane):
\* generation + 1, redeals unchanged, the same attempt
GenStep ==
    /\ card
    /\ gen < MaxGen
    /\ gen' = gen + 1
    /\ redeals' = redeals
    /\ launched' = FALSE
    /\ UNCHANGED <<epoch, attempt, takeEnded, card, launches, accepted>>

\* a take of the card ended without a finish (sprint.FieldTakeEnded)
TakeEnds ==
    /\ card
    /\ ~takeEnded
    /\ takeEnded' = TRUE
    /\ UNCHANGED <<epoch, attempt, gen, redeals, card, launched, launches, accepted>>

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
    /\ UNCHANGED <<epoch, attempt, launches>>

\* the card's push to its branch, one per generation
Launch ==
    /\ card
    /\ ~launched
    /\ launched' = TRUE
    /\ launches' = [launches EXCEPT ![<<attempt, gen, epoch>>] = @ + 1]
    /\ UNCHANGED <<epoch, attempt, gen, redeals, card, takeEnded, accepted>>

\* clear advances the epoch; the new epoch starts empty
Clear ==
    /\ epoch < MaxEpoch
    /\ epoch' = epoch + 1
    /\ attempt' = 0
    /\ gen' = 0
    /\ redeals' = 0
    /\ card' = FALSE
    /\ takeEnded' = FALSE
    /\ launched' = FALSE
    /\ UNCHANGED <<launches, accepted>>

Next ==
    \/ Deal
    \/ Redeal
    \/ GenStep
    \/ TakeEnds
    \/ \E g \in 1..MaxGen : Report(g)
    \/ Launch
    \/ Clear

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* no two launches share an attempt, a generation and an epoch: the branch a
\* launch pushes to is one launch's own
SuffixUnique == \A s \in Suffix : launches[s] <= 1

\* a report of a generation that is not the live one is refused: every report
\* taken named the card's live generation
StaleGenRefused == \A p \in accepted : p[1] = p[2]

=============================================================================
