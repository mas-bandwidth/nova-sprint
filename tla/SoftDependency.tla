--------------------------- MODULE SoftDependency ---------------------------
(***************************************************************************)
(* A soft dependency (`LAND-AFTER: <card-id>`, or `needs set --land-after`) *)
(* orders landing without blocking the deal: cards a and b may be dealt,   *)
(* worked and queued in parallel, while the lander and merge step land a   *)
(* before b whenever a is on the table; if a is dropped, b is released to  *)
(* land on its own (docs/SPEC-SPRINT.md, docs/SPEC-CARD-CONTRACT.md).      *)
(*                                                                         *)
(* Abstractions of existing modules (neither extended, as other cards edit *)
(* them):                                                                  *)
(*   - tla/NeedsBlocked.tla: abstracts Admit, UnresolvedNeeds, Land and    *)
(*     Drop into Init, NeedsMet, Resolve, Deal, Land and Drop.             *)
(*   - tla/LandPass.tla: abstracts OpenStream, NextBatch, Pass and         *)
(*     RefuseHead into Queue, Land (single queued head honouring           *)
(*     SoftLandsInOrder) and Conflict (a landed head moves the base tip    *)
(*     and a conflicting queued head is reworked back to working).         *)
(*                                                                         *)
(* SoftNeverBlocksDeal is stated as an invariant over DealEligible and the *)
(* history variable dealtWhileSoftUnlanded (which records that b was dealt *)
(* while its soft dependency a was still unlanded).                        *)
(*                                                                         *)
(* Broken witness selection:                                               *)
(*   "none"        standard model (MCSoftDependency)                       *)
(*   "blocksdeal"  Deal waits on soft dependencies as on hard needs        *)
(*                 (violates SoftNeverBlocksDeal)                          *)
(*   "anyorder"    Land takes any queued head ignoring soft dependencies   *)
(*                 (violates SoftLandsInOrder)                             *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
    Broken      \* "none", "blocksdeal", "anyorder"

Cards == {"a", "b", "c"}
Columns == {"waiting", "ready", "working", "queued", "landed", "dropped"}

VARIABLES
    col,                    \* [Cards -> Columns]: card column
    soft,                   \* [Cards -> SUBSET Cards]: soft dependencies (land_after)
    needs,                  \* [Cards -> SUBSET Cards]: hard needs (DEPENDS-ON / --needs)
    landedOrder,            \* Seq(Cards): order in which cards landed
    reworked,               \* [Cards -> BOOLEAN]: whether card was reworked at tip after conflict
    droppedEver,            \* BOOLEAN: history variable, TRUE once any Drop occurred
    dealtWhileSoftUnlanded  \* BOOLEAN: history variable, TRUE once b was dealt while a was unlanded

vars == <<col, soft, needs, landedOrder, reworked, droppedEver, dealtWhileSoftUnlanded>>

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

LandAllowed(x) ==
    /\ col[x] = "queued"
    /\ NeedsMet(x)
    /\ (Broken = "anyorder" \/ SoftMet(x))

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

Init ==
    /\ col = [x \in Cards |-> IF x = "c" THEN "waiting" ELSE "ready"]
    /\ soft = [x \in Cards |-> IF x = "b" THEN {"a"} ELSE {}]
    /\ needs = [x \in Cards |-> IF x = "c" THEN {"a"} ELSE {}]
    /\ landedOrder = <<>>
    /\ reworked = [x \in Cards |-> FALSE]
    /\ droppedEver = FALSE
    /\ dealtWhileSoftUnlanded = FALSE

\* Waiting card whose hard needs have landed moves to ready.
Resolve(x) ==
    /\ col[x] = "waiting"
    /\ NeedsMet(x)
    /\ (Broken = "blocksdeal" => SoftMet(x))
    /\ col' = [col EXCEPT ![x] = "ready"]
    /\ UNCHANGED <<soft, needs, landedOrder, reworked, droppedEver, dealtWhileSoftUnlanded>>

\* Deal a ready card to working; soft dependencies never block the deal.
Deal(x) ==
    /\ DealEligible(x)
    /\ col' = [col EXCEPT ![x] = "working"]
    /\ dealtWhileSoftUnlanded' = (dealtWhileSoftUnlanded \/ (x = "b" /\ col["a"] # "landed"))
    /\ UNCHANGED <<soft, needs, landedOrder, reworked, droppedEver>>

\* Finish work and queue the card's head for the lander (Finish + Queue).
Finish(x) ==
    /\ col[x] = "working"
    /\ NeedsMet(x)
    /\ col' = [col EXCEPT ![x] = "queued"]
    /\ UNCHANGED <<soft, needs, landedOrder, reworked, droppedEver, dealtWhileSoftUnlanded>>

Queue(x) == Finish(x)

\* Lander merges one queued head whose soft dependencies (and hard needs) have landed.
Land(x) ==
    /\ LandAllowed(x)
    /\ col' = [col EXCEPT ![x] = "landed"]
    /\ landedOrder' = Append(landedOrder, x)
    /\ UNCHANGED <<soft, needs, reworked, droppedEver, dealtWhileSoftUnlanded>>

\* A landed head moved the base tip and a queued head conflicts at the tip:
\* its head is refused and reworked back to working.
Conflict(x) ==
    /\ col[x] = "queued"
    /\ Len(landedOrder) > 0
    /\ ~reworked[x]
    /\ col' = [col EXCEPT ![x] = "working"]
    /\ reworked' = [reworked EXCEPT ![x] = TRUE]
    /\ UNCHANGED <<soft, needs, landedOrder, droppedEver, dealtWhileSoftUnlanded>>

\* Drop an unlanded card that no unlanded card hard-needs; soft dependents are released.
Drop(x) ==
    /\ col[x] \notin {"landed", "dropped"}
    /\ \A y \in Cards : (Placed(y) /\ col[y] # "landed") => x \notin needs[y]
    /\ col' = [col EXCEPT ![x] = "dropped"]
    /\ droppedEver' = TRUE
    /\ UNCHANGED <<soft, needs, landedOrder, reworked, dealtWhileSoftUnlanded>>

NextProgress ==
    \E x \in Cards : Resolve(x) \/ Deal(x) \/ Queue(x) \/ Land(x)

Next ==
    \/ NextProgress
    \/ \E x \in Cards : Conflict(x) \/ Drop(x)

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

\* SoftLandsInOrder: if b soft-depends on a, b never lands while a is on the table
\* and unlanded, and if both a and b have landed, a appears before b in landedOrder.
SoftLandsInOrder ==
    /\ \A x \in Cards : col[x] = "landed" =>
        \A s \in soft[x] :
            /\ (~Placed(s) \/ col[s] = "landed")
            /\ (InSeq(landedOrder, s) /\ InSeq(landedOrder, x) => SeqPos(landedOrder, s) < SeqPos(landedOrder, x))

\* DropReleasesSoft: once a soft dependency is dropped, the dependent is allowed to land.
DropReleasesSoft ==
    \A x \in Cards :
        (col[x] = "queued" /\ NeedsMet(x) /\ (\A s \in soft[x] : col[s] \in {"landed", "dropped"}))
            => LandAllowed(x)

\* NoSoftAndNeedCycle: the union of soft dependencies and hard needs is acyclic.
NoSoftAndNeedCycle ==
    \A x \in Cards : ~Reaches(x, x)

\* BothLand (liveness): under weak fairness of Deal, Finish/Queue and Land,
\* when no card is dropped and Broken = "none", every card eventually lands.
BothLand ==
    (Broken = "none" /\ []~droppedEver) => <>(\A x \in Cards : col[x] = "landed")

=============================================================================
