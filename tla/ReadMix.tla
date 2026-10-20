----------------------------- MODULE ReadMix -----------------------------
\* The read mix (nova-sprint set --read-mix friends=NF,fleet=NM; internal/sprint/read_mix.go,
\* the read-card deal internal/sprint/read_cards.go readCardsAskSelected): a primary in review
\* needs NF+NM ok reads at its head (up to five), all asked at once; the deal gives NF of them
\* to distinct friends and NM to distinct fleet members whose reads draw routes of distinct
\* models. Short of either class it fills from what is up (never waits for the class) and a
\* judgment says so. A read that ends with no verdict (an environment or provider failure)
\* spends its reader, and its read is dealt again on the same class first, a fleet read on a
\* model other than every fleet model already used at the attempt.
\*
\* The state, one primary at one attempt: st, each reader's read ("none" never dealt, "open",
\* "ok", "broken", "nv" no verdict); model, the model a fleet read drew; up, the readers up;
\* served, the models some enabled route of the tier serves; accepted; and two ghosts the
\* invariants read: skip (a deal that took the other class while the class still short had an
\* eligible reader) and waste (a fleet deal on a used model while an unused one was served).
\* The outside: readers come up and go down, routes stop and start serving.
\*
\* Broken = "none" is the design. Every other value is a reversed witness:
\*   "anyclass"   the deal ignores the classes (the deal before the mix): ClassFirst
\*   "anymodel"   a fleet read draws any served model: DistinctModelWhenServed
\*   "nvforgets"  a no-verdict read's model is not avoided on the re-ask: DistinctModelWhenServed
\*   "strict"     the deal never fills a short class from the other: FillNeverWaits
\*   "acceptany"  the accept counts reads dealt, not reads ok: AcceptOnlyAllOk
\*   "nopool"     the need is the mix's total whatever readers the card can have, so a
\*                sprint with fewer readers holds every card: NeverStuckOnPool
\*   "uppool"     the pool counts the readers up (the first cut of the code), so a reader
\*                coming back raises the need of a card already accepted: NeedNeverRises
\*
\* The need is bounded by the pool (internal/sprint/read_mix.go readMixNeed, readMixPool):
\* the readers the sprint has for it, up or not, not yet spent at the attempt, and the readers
\* whose reads stand, never below Floor (its tier's rule, or the total if less). Up or not, so
\* the need never rises when a reader comes back: it only falls as readers are spent. A
\* reader's room is not modelled: in the class phases the code queues a read on a busy
\* reader of the class (roomFor), so a class is "eligible" whenever a reader of it is up.

EXTENDS Naturals, FiniteSets

CONSTANTS Friends, Fleet, Models, NF, NM, Floor, Broken

ASSUME NF + NM \in 1..5 /\ Floor \in 1..(NF + NM)

Readers == Friends \cup Fleet
Need == NF + NM

VARIABLES st, model, up, served, accepted, skip, waste

vars == <<st, model, up, served, accepted, skip, waste>>

Standing(S) == {r \in S : st[r] \in {"open", "ok"}}
FWant == IF NF > Cardinality(Standing(Friends)) THEN NF - Cardinality(Standing(Friends)) ELSE 0
MWant == IF NM > Cardinality(Standing(Fleet)) THEN NM - Cardinality(Standing(Fleet)) ELSE 0
Broke == \E r \in Readers : st[r] = "broken"

\* a reader may be dealt the read: up and never dealt it (open, ok, broken and no verdict all
\* spend it at the attempt)
\* (a fleet reader only while some route of the tier serves: readRefusal's ReadRoutesRest)
Eligible(S) == {r \in S : r \in up /\ st[r] = "none" /\ (r \in Fleet => served # {})}

Pool == IF Broken = "uppool"
          THEN Cardinality(Eligible(Readers) \cup Standing(Readers))
          ELSE Cardinality({r \in Readers : st[r] = "none"} \cup Standing(Readers))
EffNeed == IF Broken = "nopool" THEN Need
           ELSE IF Pool >= Need THEN Need ELSE IF Pool >= Floor THEN Pool ELSE Floor
Want == IF Broke \/ accepted THEN 0
        ELSE IF EffNeed > Cardinality(Standing(Readers)) THEN EffNeed - Cardinality(Standing(Readers)) ELSE 0

\* the fleet models used at the attempt: standing reads' and no-verdict reads'
Used == {model[r] : r \in {x \in Fleet : st[x] \in {"open", "ok"}
                         \/ (st[x] = "nv" /\ Broken # "nvforgets")}}
TrueUsed == {model[r] : r \in {x \in Fleet : st[x] \in {"open", "ok", "nv"}}}

\* the class the deal must take now: friends while friends are short and one may read, then
\* the fleet while it is short and one may read, then either (the fill)
ClassOK(r) ==
    IF Broken \in {"anyclass"} THEN TRUE
    ELSE IF FWant > 0 /\ Eligible(Friends) # {} THEN r \in Friends
    ELSE IF MWant > 0 /\ Eligible(Fleet) # {} THEN r \in Fleet
    ELSE Broken # "strict"

\* the models a fleet read may draw: a served one not used, else any served one (the draw
\* falls back as readRouteOf does when every route is avoided)
Draw == IF Broken = "anymodel" \/ served \ Used = {} THEN served ELSE served \ Used

Init ==
    /\ st = [r \in Readers |-> "none"]
    /\ model = [r \in Fleet |-> "none"]
    /\ up \in SUBSET Readers
    /\ served \in SUBSET Models
    /\ accepted = FALSE
    /\ skip = FALSE
    /\ waste = FALSE

DealFriend(r) ==
    /\ Want > 0
    /\ r \in Eligible(Friends)
    /\ ClassOK(r)
    /\ st' = [st EXCEPT ![r] = "open"]
    /\ skip' = (skip \/ (FWant = 0 /\ ((MWant > 0 /\ Eligible(Fleet) # {}))))
    /\ UNCHANGED <<model, up, served, accepted, waste>>

DealFleet(r, m) ==
    /\ Want > 0
    /\ r \in Eligible(Fleet)
    /\ ClassOK(r)
    /\ m \in Draw
    /\ st' = [st EXCEPT ![r] = "open"]
    /\ model' = [model EXCEPT ![r] = m]
    /\ skip' = (skip \/ (FWant > 0 /\ Eligible(Friends) # {}))
    /\ waste' = (waste \/ (m \in TrueUsed /\ served \ TrueUsed # {}))
    /\ UNCHANGED <<up, served, accepted>>

Deal == (\E r \in Friends : DealFriend(r)) \/ (\E r \in Fleet, m \in Models : DealFleet(r, m))

Finish(r, v) ==
    /\ st[r] = "open"
    /\ st' = [st EXCEPT ![r] = v]
    /\ UNCHANGED <<model, up, served, accepted, skip, waste>>

OkCount == Cardinality({r \in Readers : st[r] = "ok"})

Accept ==
    /\ ~accepted
    /\ IF Broken = "acceptany"
         THEN Cardinality(Standing(Readers)) >= Need
         ELSE OkCount >= EffNeed /\ ~Broke /\ \A r \in Readers : st[r] # "open"
    /\ accepted' = TRUE
    /\ UNCHANGED <<st, model, up, served, skip, waste>>

Toggle(r) ==
    /\ up' = IF r \in up THEN up \ {r} ELSE up \cup {r}
    /\ UNCHANGED <<st, model, served, accepted, skip, waste>>

Serve(m) ==
    /\ served' = IF m \in served THEN served \ {m} ELSE served \cup {m}
    /\ UNCHANGED <<st, model, up, accepted, skip, waste>>

Next ==
    \/ Deal
    \/ \E r \in Readers, v \in {"ok", "broken", "nv"} : Finish(r, v)
    \/ Accept
    \/ \E r \in Readers : Toggle(r)
    \/ \E m \in Models : Serve(m)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ st \in [Readers -> {"none", "open", "ok", "broken", "nv"}]
    /\ model \in [Fleet -> Models \cup {"none"}]
    /\ up \subseteq Readers /\ served \subseteq Models
    /\ accepted \in BOOLEAN /\ skip \in BOOLEAN /\ waste \in BOOLEAN

\* A card is accepted only on every read dealt ok, at least Floor of them, and no broken one
\* (a broken read holds it).
AcceptOnlyAllOk == accepted => OkCount >= Floor /\ ~Broke /\ \A r \in Readers : st[r] # "open"

\* Never more reads standing than the card needs: the reads are dealt at once, not more.
NeverOverAsked == Cardinality(Standing(Readers)) <= Need

\* The deal never takes the other class while the short class has a reader who may read:
\* a no-verdict friend read is dealt again to a friend, a fleet one to the fleet.
ClassFirst == ~skip

\* A fleet read draws a model no fleet read of the attempt used (standing or no verdict)
\* whenever a served route offers one.
DistinctModelWhenServed == ~waste

\* Never blocked forever on the mix: while the card wants a read and any reader up may read it
\* (and, for the fleet, some route serves), the deal can deal.
\* Never held by readers the card cannot have: once every reader the sprint has for it was
\* dealt (each spent or its read standing), every read ok, at least Floor of them and none
\* broken, it is accepted.
NeverStuckOnPool ==
    (~accepted /\ ~Broke /\ OkCount >= Floor
       /\ \A r \in Readers : st[r] \notin {"none", "open"}) => ENABLED Accept

\* The need never rises within the attempt (a reader coming back up never raises it), so a
\* card accepted is never sent back for a read it lacked at its accept.
NeedNeverRises == [][EffNeed' <= EffNeed]_vars

FillNeverWaits ==
    (Want > 0 /\ Eligible(Readers) # {}) => ENABLED Deal
=============================================================================
