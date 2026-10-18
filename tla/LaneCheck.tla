----------------------------- MODULE LaneCheck -----------------------------
EXTENDS Naturals

\* A friend's live lane is never bounced or redealt for a stamp it did not need
\* (internal/sprint/friend_deadline.go LiveLane and laneLive,
\* internal/sprint/lane_hold.go laneGoneUnits, internal/sprint/verdicts.go
\* TickFriendRedeal).
\*
\* Instance: 1 friend, 2 cards (Cards), MaxGen generations, MaxTick ticks,
\* unfinishedLimit = Deadline.
\* A lane is live (LiveLane) when the friend's beat is fresh and names the card
\* running (beatFresh) OR the card's lane renewal is fresh inside LaneGoneAfter
\* (laneAtFresh).
\* 1. Bounce (laneGoneUnits) is enabled only when ~laneAtFresh /\ ~LiveLane.
\* 2. Redeal (TickFriendRedeal) is enabled only when PastDeadline /\ ~LiveLane.
\* 3. NoticePastDeadline fires on PastDeadline /\ LiveLane /\ ~noticed[c, gen],
\*    setting noticed[c, gen] and raising one coordinator notice per generation.

CONSTANTS Cards, MaxGen, MaxTick, Deadline,
          BadBounceLive, BadRedealLive, BadNoNotice, BadRepeatNotice

VARIABLES tick, col, gen, age, beatFresh, laneAtFresh,
          noticed, notices, bouncedLive, redealtLive

vars == <<tick, col, gen, age, beatFresh, laneAtFresh,
          noticed, notices, bouncedLive, redealtLive>>

CardGens == Cards \X (1..MaxGen)

LiveLane(c) ==
    /\ col[c] \in {"Ready", "Working"}
    /\ (beatFresh[c] \/ laneAtFresh[c])

PastDeadline(c) ==
    /\ col[c] = "Working"
    /\ age[c] >= Deadline

CanBounce(c) ==
    /\ col[c] = "Working"
    /\ gen[c] < MaxGen
    /\ ~laneAtFresh[c]
    /\ (~LiveLane(c) \/ BadBounceLive)

CanRedeal(c) ==
    /\ PastDeadline(c)
    /\ (~LiveLane(c) \/ BadRedealLive)

CanNoticePastDeadline(c) ==
    /\ PastDeadline(c)
    /\ LiveLane(c)
    /\ ~CanRedeal(c)
    /\ ~BadNoNotice
    /\ (~noticed[<<c, gen[c]>>] \/ BadRepeatNotice)

Init ==
    /\ tick = 0
    /\ col = [c \in Cards |-> "Working"]
    /\ gen = [c \in Cards |-> 1]
    /\ age = [c \in Cards |-> 0]
    /\ beatFresh \in [Cards -> BOOLEAN]
    /\ laneAtFresh \in [Cards -> BOOLEAN]
    /\ noticed = [cg \in CardGens |-> FALSE]
    /\ notices = [cg \in CardGens |-> 0]
    /\ bouncedLive = FALSE
    /\ redealtLive = FALSE

\* Bounce (laneGoneUnits): bounces a working card whose lane renewal expired
\* only when the lane is not live (unless BadBounceLive).
Bounce(c) ==
    /\ CanBounce(c)
    /\ col' = [col EXCEPT ![c] = "Ready"]
    /\ gen' = [gen EXCEPT ![c] = @ + 1]
    /\ age' = [age EXCEPT ![c] = 0]
    /\ bouncedLive' = (bouncedLive \/ LiveLane(c))
    /\ UNCHANGED <<tick, beatFresh, laneAtFresh, noticed, notices, redealtLive>>

\* Start: a bounced ready card is started again on a live signal.
Start(c) ==
    /\ col[c] = "Ready"
    /\ (beatFresh[c] \/ laneAtFresh[c])
    /\ col' = [col EXCEPT ![c] = "Working"]
    /\ age' = [age EXCEPT ![c] = 0]
    /\ UNCHANGED <<tick, gen, beatFresh, laneAtFresh, noticed, notices, bouncedLive, redealtLive>>

\* Signal changes before a card reaches the deadline.
UpdateSignals(c, b, l) ==
    /\ ~PastDeadline(c)
    /\ beatFresh' = [beatFresh EXCEPT ![c] = b]
    /\ laneAtFresh' = [laneAtFresh EXCEPT ![c] = l]
    /\ UNCHANGED <<tick, col, gen, age, noticed, notices, bouncedLive, redealtLive>>

\* Tick advances running time by one unit and runs TickFriendRedeal:
\* Redeal on PastDeadline /\ ~LiveLane; NoticePastDeadline on
\* PastDeadline /\ LiveLane /\ ~noticed[c, gen].
Tick ==
    /\ tick < MaxTick
    /\ tick' = tick + 1
    /\ LET age1 == [c \in Cards |-> IF col[c] = "Working" THEN age[c] + 1 ELSE age[c]]
           past(c) == col[c] = "Working" /\ age1[c] >= Deadline
           redeal(c) == past(c) /\ (~LiveLane(c) \/ BadRedealLive)
           notice(c) == past(c) /\ LiveLane(c) /\ ~redeal(c) /\ ~BadNoNotice
                        /\ (~noticed[<<c, gen[c]>>] \/ BadRepeatNotice)
       IN /\ col' = [c \in Cards |-> IF redeal(c) THEN "Redealt" ELSE col[c]]
          /\ gen' = [c \in Cards |-> IF redeal(c) /\ gen[c] < MaxGen THEN gen[c] + 1 ELSE gen[c]]
          /\ age' = [c \in Cards |-> IF redeal(c) THEN 0 ELSE age1[c]]
          /\ noticed' = [cg \in CardGens |->
                            IF \E c \in Cards : cg = <<c, gen[c]>> /\ notice(c)
                            THEN TRUE
                            ELSE noticed[cg]]
          /\ notices' = [cg \in CardGens |->
                            IF \E c \in Cards : cg = <<c, gen[c]>> /\ notice(c)
                            THEN notices[cg] + 1
                            ELSE notices[cg]]
          /\ redealtLive' = (redealtLive \/ \E c \in Cards : redeal(c) /\ LiveLane(c))
          /\ UNCHANGED <<beatFresh, laneAtFresh, bouncedLive>>

\* Explicit action aliases for Redeal and NoticePastDeadline when stepped per card.
Redeal(c) ==
    /\ CanRedeal(c)
    /\ col' = [col EXCEPT ![c] = "Redealt"]
    /\ gen' = [gen EXCEPT ![c] = IF @ < MaxGen THEN @ + 1 ELSE @]
    /\ age' = [age EXCEPT ![c] = 0]
    /\ redealtLive' = (redealtLive \/ LiveLane(c))
    /\ UNCHANGED <<tick, beatFresh, laneAtFresh, noticed, notices, bouncedLive>>

NoticePastDeadline(c) ==
    /\ CanNoticePastDeadline(c)
    /\ noticed' = [noticed EXCEPT ![<<c, gen[c]>>] = TRUE]
    /\ notices' = [notices EXCEPT ![<<c, gen[c]>>] = @ + 1]
    /\ UNCHANGED <<tick, col, gen, age, beatFresh, laneAtFresh, bouncedLive, redealtLive>>

Next ==
    \/ \E c \in Cards : Bounce(c) \/ Start(c) \/ Redeal(c) \/ NoticePastDeadline(c)
    \/ \E c \in Cards, b \in BOOLEAN, l \in BOOLEAN : UpdateSignals(c, b, l)
    \/ Tick

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ tick \in 0..MaxTick
    /\ col \in [Cards -> {"Ready", "Working", "Redealt"}]
    /\ gen \in [Cards -> 1..MaxGen]
    /\ age \in [Cards -> 0..MaxTick]
    /\ beatFresh \in [Cards -> BOOLEAN]
    /\ laneAtFresh \in [Cards -> BOOLEAN]
    /\ noticed \in [CardGens -> BOOLEAN]
    /\ notices \in [CardGens -> 0..(MaxTick + 1)]
    /\ bouncedLive \in BOOLEAN
    /\ redealtLive \in BOOLEAN

\* Invariant 1: a live lane is never bounced or redealt.
LiveLaneNeverBouncedOrRedealt ==
    /\ ~bouncedLive
    /\ ~redealtLive

\* Invariant 2: a live card past the deadline has exactly one notice.
LivePastDeadlineHasOneNotice ==
    /\ \A c \in Cards :
        (PastDeadline(c) /\ LiveLane(c)) =>
            (noticed[<<c, gen[c]>>] /\ notices[<<c, gen[c]>>] = 1)
    /\ \A cg \in CardGens : notices[cg] <= 1

=============================================================================
