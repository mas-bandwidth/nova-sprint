--------------------------- MODULE SoftDependency ---------------------------
(***************************************************************************)
(* A soft dependency (`LAND-AFTER: <card-id>`, or `needs set --land-after`) *)
(* orders landing without blocking the deal: cards a and b may be dealt,   *)
(* worked and queued in parallel, and the lander orders a before b only    *)
(* among cards both queued to merge. When a is not queued, b lands and a   *)
(* re-merges onto it later (SoftIsOrderOnly); when a is queued, b lands    *)
(* after it in the batch (SoftLandsInOrder); a queued card that cannot be  *)
(* placed is skipped and the batch lands every other one                  *)
(* (HeldNeverEmptiesBatch). If a is dropped, b's soft dependency is        *)
(* released (docs/SPEC-SPRINT.md, docs/SPEC-CARD-CONTRACT.md).             *)
(*                                                                         *)
(* Abstractions of existing modules (neither extended, as other cards edit *)
(* them):                                                                  *)
(*   - tla/NeedsBlocked.tla: abstracts Admit, UnresolvedNeeds, Land and    *)
(*     Drop into Init, NeedsMet, Resolve, Deal, Land and Drop.             *)
(*   - tla/LandPass.tla: abstracts OpenStream, NextBatch, Pass and         *)
(*     RefuseHead into Queue, LandBatch (every placeable queued card in   *)
(*     one batch, internal/sprint MergeSoftOrder and cmd/nova-sprint       *)
(*     openStream) and Conflict (a landed head moves the base tip and a    *)
(*     conflicting queued head is reworked back to working). Hold and      *)
(*     Release are the outside facts that keep a queued card out of a      *)
(*     batch (a dead base, a need it waits for); each card is held at      *)
(*     most once, so the liveness check is not a hold/release loop.        *)
(*                                                                         *)
(* SoftNeverBlocksDeal is stated as an invariant over DealEligible and the *)
(* history variable dealtWhileSoftUnlanded (which records that b was dealt *)
(* while its soft dependency a was still unlanded).                        *)
(*                                                                         *)
(* Broken witness selection:                                               *)
(*   "none"        standard model (MCSoftDependency)                       *)
(*   "blocksdeal"  Deal waits on soft dependencies as on hard needs        *)
(*                 (violates SoftNeverBlocksDeal)                          *)
(*   "anyorder"    LandBatch takes queued heads ignoring soft dependencies *)
(*                 (violates SoftLandsInOrder)                             *)
(*   "softishard"  an unqueued, unlanded target holds its dependent, the   *)
(*                 rule before 2026-10-16 (violates SoftIsOrderOnly)       *)
(*   "heldblocks"  one queued card that cannot be placed empties the       *)
(*                 batch, the stream refused whole (violates               *)
(*                 HeldNeverEmptiesBatch)                                  *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
    Broken      \* "none", "blocksdeal", "anyorder", "softishard", "heldblocks", "cutunguarded"

Cards == {"a", "b", "c"}
Columns == {"waiting", "ready", "working", "queued", "landed", "dropped"}

VARIABLES
    col,                    \* [Cards -> Columns]: card column
    soft,                   \* [Cards -> SUBSET Cards]: soft dependencies (land_after)
    needs,                  \* [Cards -> SUBSET Cards]: hard needs (DEPENDS-ON / --needs)
    landedOrder,            \* Seq(Cards): order in which cards landed
    reworked,               \* [Cards -> BOOLEAN]: whether card was reworked at tip after conflict
    droppedEver,            \* BOOLEAN: history variable, TRUE once any Drop occurred
    dealtWhileSoftUnlanded, \* BOOLEAN: history variable, TRUE once b was dealt while a was unlanded
    held,                   \* SUBSET Cards: queued cards an outside fact keeps out of the batch
    heldEver,               \* [Cards -> BOOLEAN]: whether the card was ever held (bounds Hold)
    outOfOrder,             \* BOOLEAN: history variable
    cutOrder,               \* Seq(Cards): order pinned by Cut
    markedOrder,            \* Seq(Cards): prefix committed by Mark+Push
    queuedAtMark,           \* [Cards -> SUBSET Cards]: targets queued at mark
    baseCommits,            \* Seq(Cards): pushed base history
    recordedLandings,       \* SUBSET Cards: cards recorded by Report
    inFlightBatch,          \* SUBSET Cards: pushed batch awaiting Report
    reportRefused           \* BOOLEAN: Report refused a pushed batch

phaseVars == <<cutOrder, markedOrder, queuedAtMark, baseCommits, recordedLandings, inFlightBatch, reportRefused>>
vars == <<col, soft, needs, landedOrder, reworked, droppedEver, dealtWhileSoftUnlanded, held, heldEver, outOfOrder, cutOrder, markedOrder, queuedAtMark, baseCommits, recordedLandings, inFlightBatch, reportRefused>>
rest == <<held, heldEver, outOfOrder, cutOrder, markedOrder, queuedAtMark, baseCommits, recordedLandings, inFlightBatch, reportRefused>>

Placed(x) == col[x] # "dropped"

NeedsMet(x) == \A n \in needs[x] : col[n] = "landed"

SoftMet(x) == \A s \in soft[x] : ~Placed(s) \/ col[s] = "landed"

Edge(x, y) == y \in needs[x] \/ y \in soft[x]

Reaches(x, y) ==
    \/ Edge(x, y)
    \/ \E m \in Cards : Edge(x, m) /\ Edge(m, y)
    \/ \E m1, m2 \in Cards : Edge(x, m1) /\ Edge(m1, m2) /\ Edge(m2, y)

DealEligible(x) ==
    /\ col[x] = "ready"
    /\ NeedsMet(x)
    /\ (Broken = "blocksdeal" => SoftMet(x))

\* Free: queued, not held, and its hard needs landed.
Free(x) == col[x] = "queued" /\ x \notin held /\ NeedsMet(x)

\* SoftPlaceable(s): soft dependency s of a queued card lets it be placed. A soft
\* dependency orders only among queued cards: one not queued is ignored for this
\* batch, and a queued one must itself be placed ahead (Free; the soft targets here
\* have no soft dependencies of their own, so one level is the whole order).
SoftPlaceable(s) ==
    IF Broken = "softishard"
    THEN ~Placed(s) \/ col[s] = "landed" \/ Free(s)
    ELSE col[s] # "queued" \/ Free(s)

\* Placeable: what MergeSoftOrder places this batch (LandOrderBlocked is empty).
Placeable(x) == Free(x) /\ (Broken = "anyorder" \/ \A s \in soft[x] : SoftPlaceable(s))

\* Batch: every placeable queued card; a card that cannot be placed is skipped, never
\* the whole batch (under "heldblocks" one such card empties it).
Batch ==
    IF Broken = "heldblocks" /\ \E x \in Cards : col[x] = "queued" /\ ~Placeable(x)
    THEN {}
    ELSE {x \in Cards : Placeable(x)}

LandAllowed(x) == x \in Batch

RECURSIVE SetToSeq(_)
SetToSeq(S) == IF S = {} THEN <<>> ELSE LET e == CHOOSE e \in S : TRUE IN <<e>> \o SetToSeq(S \ {e})

\* The batch's order: the cards with no soft dependency in the batch first, then the rest.
Ahead == {x \in Batch : \A s \in soft[x] : s \notin Batch}
BatchOrder == SetToSeq(Ahead) \o SetToSeq(Batch \ Ahead)

InSeq(s, e) == \E i \in 1..Len(s) : s[i] = e

SeqPos(s, e) == CHOOSE i \in 1..Len(s) : s[i] = e

TypeOK ==
    /\ col \in [Cards -> Columns]
    /\ soft \in [Cards -> SUBSET Cards]
    /\ needs \in [Cards -> SUBSET Cards]
    /\ landedOrder \in Seq(Cards)
    /\ Len(landedOrder) <= Cardinality(Cards)
    /\ reworked \in [Cards -> BOOLEAN]
    /\ droppedEver \in BOOLEAN
    /\ dealtWhileSoftUnlanded \in BOOLEAN
    /\ held \subseteq Cards
    /\ heldEver \in [Cards -> BOOLEAN]
    /\ outOfOrder \in BOOLEAN
    /\ cutOrder \in Seq(Cards)
    /\ markedOrder \in Seq(Cards)
    /\ queuedAtMark \in [Cards -> SUBSET Cards]
    /\ baseCommits \in Seq(Cards)
    /\ recordedLandings \subseteq Cards
    /\ inFlightBatch \subseteq Cards
    /\ reportRefused \in BOOLEAN

Init ==
    /\ col = [x \in Cards |-> IF x = "c" THEN "waiting" ELSE "ready"]
    /\ soft = [x \in Cards |-> IF x = "b" THEN {"a"} ELSE {}]
    /\ needs = [x \in Cards |-> IF x = "c" THEN {"a"} ELSE {}]
    /\ landedOrder = <<>>
    /\ reworked = [x \in Cards |-> FALSE]
    /\ droppedEver = FALSE
    /\ dealtWhileSoftUnlanded = FALSE
    /\ held = {}
    /\ heldEver = [x \in Cards |-> FALSE]
    /\ outOfOrder = FALSE
    /\ cutOrder = <<>>
    /\ markedOrder = <<>>
    /\ queuedAtMark = [x \in Cards |-> {}]
    /\ baseCommits = <<>>
    /\ recordedLandings = {}
    /\ inFlightBatch = {}
    /\ reportRefused = FALSE

\* Waiting card whose hard needs have landed moves to ready.
Resolve(x) ==
    /\ col[x] = "waiting"
    /\ NeedsMet(x)
    /\ (Broken = "blocksdeal" => SoftMet(x))
    /\ col' = [col EXCEPT ![x] = "ready"]
    /\ UNCHANGED <<soft, needs, landedOrder, reworked, droppedEver, dealtWhileSoftUnlanded>>
    /\ UNCHANGED rest

\* Deal a ready card to working; soft dependencies never block the deal.
Deal(x) ==
    /\ DealEligible(x)
    /\ col' = [col EXCEPT ![x] = "working"]
    /\ dealtWhileSoftUnlanded' = (dealtWhileSoftUnlanded \/ (x = "b" /\ col["a"] # "landed"))
    /\ UNCHANGED <<soft, needs, landedOrder, reworked, droppedEver>>
    /\ UNCHANGED rest

\* Finish work and queue the card's head for the lander (Finish + Queue).
Finish(x) ==
    /\ col[x] = "working"
    /\ NeedsMet(x)
    /\ col' = [col EXCEPT ![x] = "queued"]
    /\ UNCHANGED <<soft, needs, landedOrder, reworked, droppedEver, dealtWhileSoftUnlanded>>
    /\ UNCHANGED rest

Queue(x) == Finish(x)

\* Cut pins the lander's snapshot; Queue and Finish may interleave before MarkPush.
Cut ==
    /\ cutOrder = <<>>
    /\ inFlightBatch = {}
    /\ Batch # {}
    /\ cutOrder' = BatchOrder
    /\ UNCHANGED <<col, soft, needs, landedOrder, reworked, droppedEver,
                   dealtWhileSoftUnlanded, held, heldEver, outOfOrder,
                   markedOrder, queuedAtMark, baseCommits, recordedLandings,
                   inFlightBatch, reportRefused>>

LandOrderBlockedAtMark(x, ahead) ==
    \E t \in soft[x] : col[t] = "queued" /\ t \notin ahead

RECURSIVE GuardedPrefix(_, _)
GuardedPrefix(order, ahead) ==
    IF order = <<>> THEN <<>>
    ELSE LET x == Head(order) IN
        IF LandOrderBlockedAtMark(x, ahead)
        THEN <<>>
        ELSE <<x>> \o GuardedPrefix(Tail(order), ahead \cup {x})

MarkOrder == IF Broken \in {"cutunguarded", "anyorder"} THEN cutOrder ELSE GuardedPrefix(cutOrder, {})

\* MarkLanding is the final store step before push. It reuses LandOrderBlocked and
\* pushes only the safe prefix. internal/sprint/landing_mark.go MarkLanding.
MarkPush ==
    /\ cutOrder # <<>>
    /\ inFlightBatch = {}
    /\ MarkOrder # <<>>
    /\ markedOrder' = MarkOrder
    /\ baseCommits' = baseCommits \o MarkOrder
    /\ inFlightBatch' = {MarkOrder[i] : i \in 1..Len(MarkOrder)}
    /\ queuedAtMark' = [x \in Cards |-> IF InSeq(MarkOrder, x) THEN {t \in soft[x] : col[t] = "queued"} ELSE queuedAtMark[x]]
    /\ outOfOrder' = (outOfOrder \/ \E i \in 1..Len(MarkOrder) :
          \E t \in soft[MarkOrder[i]] : col[t] = "queued" /\
              ~(\E j \in 1..(i-1) : MarkOrder[j] = t))
    /\ UNCHANGED <<col, soft, needs, landedOrder, reworked, droppedEver,
                   dealtWhileSoftUnlanded, held, heldEver, cutOrder,
                   recordedLandings, reportRefused>>

SkipCut ==
    /\ cutOrder # <<>>
    /\ inFlightBatch = {}
    /\ MarkOrder = <<>>
    /\ cutOrder' = <<>>
    /\ UNCHANGED <<col, soft, needs, landedOrder, reworked, droppedEver,
                   dealtWhileSoftUnlanded, held, heldEver, outOfOrder,
                   markedOrder, queuedAtMark, baseCommits, recordedLandings,
                   inFlightBatch, reportRefused>>

ReportBlocked ==
    \E i \in 1..Len(markedOrder) : \E t \in soft[markedOrder[i]] :
        col[t] = "queued" /\ ~(\E j \in 1..(i-1) : markedOrder[j] = t)

\* Report honors the fixed mark. The broken witness keeps the former report recheck
\* from steps_merge.go:502 and can refuse a batch already pushed.
Report ==
    /\ inFlightBatch # {}
    /\ IF Broken = "cutunguarded" /\ ReportBlocked
       THEN
         /\ reportRefused' = TRUE
         /\ UNCHANGED <<col, landedOrder, recordedLandings>>
       ELSE
         /\ reportRefused' = FALSE
         /\ col' = [x \in Cards |-> IF x \in inFlightBatch THEN "landed" ELSE col[x]]
         /\ landedOrder' = landedOrder \o markedOrder
         /\ recordedLandings' = recordedLandings \cup inFlightBatch
    /\ cutOrder' = <<>>
    /\ markedOrder' = <<>>
    /\ inFlightBatch' = {}
    /\ UNCHANGED <<soft, needs, reworked, droppedEver, dealtWhileSoftUnlanded,
                   held, heldEver, outOfOrder, queuedAtMark, baseCommits>>

Land(x) == InSeq(markedOrder, x) /\ Report

\* An outside fact keeps a queued card out of the batch (a dead base, a need it waits
\* for), at most once a card.
Hold(x) ==
    /\ col[x] = "queued"
    /\ x \notin held
    /\ ~heldEver[x]
    /\ held' = held \cup {x}
    /\ heldEver' = [heldEver EXCEPT ![x] = TRUE]
    /\ UNCHANGED <<col, soft, needs, landedOrder, reworked, droppedEver, dealtWhileSoftUnlanded, outOfOrder>>
    /\ UNCHANGED phaseVars

\* The fact is answered: the card may be placed again.
Release(x) ==
    /\ x \in held
    /\ held' = held \ {x}
    /\ UNCHANGED <<col, soft, needs, landedOrder, reworked, droppedEver, dealtWhileSoftUnlanded, heldEver, outOfOrder>>
    /\ UNCHANGED phaseVars

\* A landed head moved the base tip and a queued head conflicts at the tip:
\* its head is refused and reworked back to working.
Conflict(x) ==
    /\ col[x] = "queued"
    /\ x \notin held
    /\ Len(landedOrder) > 0
    /\ ~reworked[x]
    /\ col' = [col EXCEPT ![x] = "working"]
    /\ reworked' = [reworked EXCEPT ![x] = TRUE]
    /\ UNCHANGED <<soft, needs, landedOrder, droppedEver, dealtWhileSoftUnlanded>>
    /\ UNCHANGED rest

\* Drop an unlanded card that no unlanded card hard-needs; soft dependents are released.
Drop(x) ==
    /\ col[x] \notin {"landed", "dropped"}
    /\ \A y \in Cards : (Placed(y) /\ col[y] # "landed") => x \notin needs[y]
    /\ col' = [col EXCEPT ![x] = "dropped"]
    /\ droppedEver' = TRUE
    /\ UNCHANGED <<soft, needs, landedOrder, reworked, dealtWhileSoftUnlanded>>
    /\ UNCHANGED rest

NextProgress ==
    \/ Cut
    \/ MarkPush
    \/ SkipCut
    \/ Report
    \/ \E x \in Cards : Resolve(x) \/ Deal(x) \/ Queue(x) \/ Release(x)

Next ==
    \/ NextProgress
    \/ \E x \in Cards : Conflict(x) \/ Drop(x) \/ Hold(x)

Spec == Init /\ [][Next]_vars /\ WF_vars(NextProgress)

---------------------------------------------------------------------------
\* Safety invariants and liveness property.

\* SoftNeverBlocksDeal (stated as an invariant over DealEligible and the history
\* variable dealtWhileSoftUnlanded): a ready card whose hard needs have landed is
\* always DealEligible regardless of unlanded soft dependencies, and whenever b
\* has moved past ready while a is still ready, dealtWhileSoftUnlanded recorded it.
SoftNeverBlocksDeal ==
    /\ \A x \in Cards : (col[x] = "ready" /\ NeedsMet(x)) => DealEligible(x)
    /\ (col["b"] \in {"working", "queued", "landed"} /\ col["a"] = "ready") => dealtWhileSoftUnlanded

\* SoftLandsInOrder: if b soft-depends on a and a is queued to merge, b never lands
\* ahead of it: not while a is queued and left out of the batch, and not before a in
\* the batch they land in together.
SoftLandsInOrder == ~outOfOrder

\* SoftIsOrderOnly: a soft dependency orders only among queued cards. A free queued
\* card none of whose soft dependencies is queued is placed in the batch, whatever
\* column its targets are in (reversed witness "softishard": an unqueued, unlanded
\* target holds it).
SoftIsOrderOnly ==
    \A x \in Cards : (Free(x) /\ \A s \in soft[x] : col[s] # "queued") => Placeable(x)

\* HeldNeverEmptiesBatch: a card that cannot be placed is skipped; every placeable
\* queued card is in the batch (reversed witness "heldblocks": one held card empties it).
HeldNeverEmptiesBatch ==
    \A x \in Cards : Placeable(x) => x \in Batch

\* DropReleasesSoft: once a soft dependency is dropped, the dependent is allowed to land.
DropReleasesSoft ==
    \A x \in Cards :
        (Free(x) /\ (\A s \in soft[x] : col[s] \in {"landed", "dropped"}))
            => Placeable(x)

\* NoSoftAndNeedCycle: the union of soft dependencies and hard needs is acyclic.
NoSoftAndNeedCycle ==
    \A x \in Cards : ~Reaches(x, x)

\* Every base commit is recorded or belongs to the one pushed batch awaiting
\* Report, and Report never refuses a pushed marked batch.
BaseRecorded ==
    /\ \A i \in 1..Len(baseCommits) :
         baseCommits[i] \in recordedLandings \cup inFlightBatch
    /\ ~reportRefused

\* A target queued when x was marked precedes x in the pushed base history.
BaseSoftOrder ==
    \A i \in 1..Len(baseCommits) :
      \A t \in queuedAtMark[baseCommits[i]] :
        \E j \in 1..(i-1) : baseCommits[j] = t

\* BothLand (liveness): under weak fairness of Deal, Finish/Queue, Release and Cut/MarkPush/Report,
\* when no card is dropped and Broken = "none", every card eventually lands.
BothLand ==
    (Broken = "none" /\ []~droppedEver) => <>(\A x \in Cards : col[x] = "landed")

=============================================================================
