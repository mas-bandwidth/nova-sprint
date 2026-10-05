---------------------------- MODULE StallLadder ----------------------------
\* The friend stall ladder (docs/SPEC-SPRINT.md, section friend-stall-ladder-r.w1).
\* A friend that stalls holds her dealt cards until the coordinator notices by hand.
\* The stall ladder makes recovery fully mechanical as a tick part in
\* internal/sprint/friend_stall.go added to TickParts (internal/sprint/steps_tick.go).
\*
\* A friend is stalled when she holds dealt cards and no evidence of her work is newer
\* than friend_stall_after (20m). Evidence is everything the store has (stall-ladder-counts-
\* real-work.w1, 2026-10-05: the ladder counting session writes alone marked three working
\* friends down within the hour, a one-shot lane runner whose beat named running jobs and
\* the coordinator's row whose cards child agents ran): her session activity, a beat whose
\* running list is non-empty, a finish or a report collected for her, a read she recorded,
\* and a progress stamp on any of her cards.
\* The ladder climbs one rung per friend_stall_step (5m) while she stays stalled:
\*   (1) a wake turn: a bus message to her that her daemon pushes in as a turn
\*   (2) a second wake
\*   (3) a coordinator note (a pushed judgment, "friend <f> stalled <d>: two wakes unanswered")
\*   (4) every card of hers she has not started taken back (FriendTake with All;
\*       a started card stays with her and finishes), ready for the next deal
\*   (5) she is marked down with reason "stalled", and released to up by the tick itself
\*       at her first evidence of work after it.
\* Every rung is a happened note pushed like the rest; any evidence resets her to rung 0.
\*
\* Invariants:
\*   NoCardHeldPastBound: no card is held by a stalled friend for more than the
\*     bound (stall_after plus four steps).
\*   NoStartedRedealt: no card is redealt while it has started.
\*   ReleasedOnlyByActivity: the name the MC configs check, which is now both of
\*     ReleasedOnlyByEvidence: a friend is released only by evidence of her own work, and
\*     NeverDownWhileWorking: a friend down for stall has no evidence of work within the
\*       bound, so any evidence after the down releases her.
\*
\* Broken values:
\*   "none"          the design
\*   "notaken"       rung 4 fails to take back unstarted cards (breaks NoCardHeldPastBound)
\*   "takestarted"   rung 4 takes back started cards too, allowing redeal (breaks NoStartedRedealt)
\*   "releaseother"  friend is released to up without evidence (breaks ReleasedOnlyByActivity)
\*   "sessiononly"   the build of 2026-10-05: only session activity and progress keep her off the
\*                   ladder, only session activity releases her (breaks NeverDownWhileWorking)

EXTENDS Naturals, FiniteSets

CONSTANTS Friends, Cards, StallAfter, StallStep, MaxTime, Broken

\* activity is her session activity; evidence is her newest other evidence of work that is
\* not on a card she holds (a running beat, a finish or a collected report, a read).
VARIABLES clock, rung, cardState, cardHolder, activity, evidence, cardProgress, status, wasStarted, releasedWithoutEvidence

vars == <<clock, rung, cardState, cardHolder, activity, evidence, cardProgress, status, wasStarted, releasedWithoutEvidence>>

CardStates == {"dealt", "started", "taken", "redealt", "done"}
FriendStatuses == {"up", "down"}

TypeOK ==
  /\ clock \in 0..MaxTime
  /\ rung \in [Friends -> 0..5]
  /\ cardState \in [Cards -> CardStates]
  /\ cardHolder \in [Cards -> Friends]
  /\ activity \in [Friends -> 0..MaxTime]
  /\ evidence \in [Friends -> 0..MaxTime]
  /\ cardProgress \in [Cards -> 0..MaxTime]
  /\ status \in [Friends -> FriendStatuses]
  /\ wasStarted \in [Cards -> BOOLEAN]
  /\ releasedWithoutEvidence \in BOOLEAN

Init ==
  /\ clock = 0
  /\ rung = [f \in Friends |-> 0]
  /\ cardState = [c \in Cards |-> "dealt"]
  /\ cardHolder = [c \in Cards |-> CHOOSE f \in Friends : TRUE]
  /\ activity = [f \in Friends |-> 0]
  /\ evidence = [f \in Friends |-> 0]
  /\ cardProgress = [c \in Cards |-> 0]
  /\ status = [f \in Friends |-> "up"]
  /\ wasStarted = [c \in Cards |-> FALSE]
  /\ releasedWithoutEvidence = FALSE

Max(S) == CHOOSE t \in S : \A other \in S : t >= other

\* Latest evidence of work for friend f: every kind, whatever the ladder counts
LastEvidence(f) ==
  LET progCards == {c \in Cards : cardHolder[c] = f /\ cardState[c] \in {"dealt", "started"}}
  IN Max({cardProgress[c] : c \in progCards} \cup {activity[f], evidence[f]})

\* The evidence the ladder counts: all of it, or under "sessiononly" session activity and
\* progress alone, as the build of 2026-10-05 did
LastActive(f) ==
  LET progCards == {c \in Cards : cardHolder[c] = f /\ cardState[c] \in {"dealt", "started"}}
  IN IF Broken = "sessiononly"
     THEN Max({cardProgress[c] : c \in progCards} \cup {activity[f]})
     ELSE LastEvidence(f)

\* Evidence other than session activity releases a stall-down, except under "sessiononly"
ReleaseOn(f) == IF Broken = "sessiononly" THEN status[f] ELSE "up"

\* Friend holds cards in dealt or started state
HoldsCards(f) ==
  \E c \in Cards : cardHolder[c] = f /\ cardState[c] \in {"dealt", "started"}

\* Clock tick: advances clock and updates stall ladder
Tick ==
  /\ clock < MaxTime
  /\ clock' = clock + 1
  /\ rung' = [f \in Friends |->
       LET idleDuration == clock + 1 - LastActive(f)
       IN IF ~HoldsCards(f) \/ idleDuration < StallAfter THEN 0
          ELSE IF idleDuration < StallAfter + StallStep THEN 1
          ELSE IF idleDuration < StallAfter + 2 * StallStep THEN 2
          ELSE IF idleDuration < StallAfter + 3 * StallStep THEN 3
          ELSE IF idleDuration < StallAfter + 4 * StallStep THEN 4
          ELSE 5]
  /\ cardState' = [c \in Cards |->
       LET f == cardHolder[c]
           idleDuration == clock + 1 - LastActive(f)
       IN IF HoldsCards(f) /\ idleDuration >= StallAfter + 3 * StallStep /\ rung[f] < 4 THEN
            IF Broken = "notaken" THEN cardState[c]
            ELSE IF Broken = "takestarted" THEN
              IF cardState[c] \in {"dealt", "started"} THEN "taken" ELSE cardState[c]
            ELSE
              IF cardState[c] = "dealt" THEN "taken" ELSE cardState[c]
          ELSE cardState[c]]
  /\ status' = [f \in Friends |->
       LET idleDuration == clock + 1 - LastActive(f)
       IN IF HoldsCards(f) /\ idleDuration >= StallAfter + 4 * StallStep THEN "down"
          ELSE status[f]]
  /\ UNCHANGED <<cardHolder, activity, evidence, cardProgress, wasStarted, releasedWithoutEvidence>>

\* Friend has session activity
FriendActivity(f) ==
  /\ activity' = [activity EXCEPT ![f] = clock]
  /\ rung' = [rung EXCEPT ![f] = 0]
  /\ status' = [status EXCEPT ![f] = "up"]
  /\ UNCHANGED <<clock, cardState, cardHolder, evidence, cardProgress, wasStarted, releasedWithoutEvidence>>

\* Her beat names a running job (a one-shot lane, a child agent), with no session activity,
\* or she records a read: evidence of work off her held cards
OtherEvidence(f) ==
  /\ evidence' = [evidence EXCEPT ![f] = clock]
  /\ rung' = [rung EXCEPT ![f] = IF Broken = "sessiononly" THEN rung[f] ELSE 0]
  /\ status' = [status EXCEPT ![f] = ReleaseOn(f)]
  /\ UNCHANGED <<clock, cardState, cardHolder, activity, cardProgress, wasStarted, releasedWithoutEvidence>>

\* Friend stamps progress on card c
CardProgressStamp(c) ==
  /\ cardState[c] \in {"dealt", "started"}
  /\ cardProgress' = [cardProgress EXCEPT ![c] = clock]
  /\ rung' = [rung EXCEPT ![cardHolder[c]] = 0]
  /\ status' = [status EXCEPT ![cardHolder[c]] = ReleaseOn(cardHolder[c])]
  /\ UNCHANGED <<clock, cardState, cardHolder, activity, evidence, wasStarted, releasedWithoutEvidence>>

\* Card c goes working to done: a finish, or a report collected for her by friend sync
FinishCard(c) ==
  LET f == cardHolder[c] IN
  /\ cardState[c] \in {"dealt", "started"}
  /\ cardState' = [cardState EXCEPT ![c] = "done"]
  /\ evidence' = [evidence EXCEPT ![f] = clock]
  /\ rung' = [rung EXCEPT ![f] = IF Broken = "sessiononly" THEN rung[f] ELSE 0]
  /\ status' = [status EXCEPT ![f] = ReleaseOn(f)]
  /\ UNCHANGED <<clock, cardHolder, activity, cardProgress, wasStarted, releasedWithoutEvidence>>

\* Friend starts card c
FriendStartCard(c) ==
  /\ cardState[c] = "dealt"
  /\ cardState' = [cardState EXCEPT ![c] = "started"]
  /\ wasStarted' = [wasStarted EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<clock, rung, cardHolder, activity, evidence, cardProgress, status, releasedWithoutEvidence>>

\* Taken card is redealt
RedealCard(c) ==
  /\ cardState[c] = "taken"
  /\ cardState' = [cardState EXCEPT ![c] = "redealt"]
  /\ UNCHANGED <<clock, rung, cardHolder, activity, evidence, cardProgress, status, wasStarted, releasedWithoutEvidence>>

\* Spurious release without evidence (reversed witness)
SpuriousRelease(f) ==
  /\ Broken = "releaseother"
  /\ status[f] = "down"
  /\ status' = [status EXCEPT ![f] = "up"]
  /\ releasedWithoutEvidence' = TRUE
  /\ UNCHANGED <<clock, rung, cardState, cardHolder, activity, evidence, cardProgress, wasStarted>>

Next ==
  \/ Tick
  \/ \E f \in Friends : FriendActivity(f)
  \/ \E f \in Friends : OtherEvidence(f)
  \/ \E c \in Cards : CardProgressStamp(c)
  \/ \E c \in Cards : FinishCard(c)
  \/ \E c \in Cards : FriendStartCard(c)
  \/ \E c \in Cards : RedealCard(c)
  \/ \E f \in Friends : SpuriousRelease(f)

Spec == Init /\ [][Next]_vars

\* Invariant 1: No card is held by a stalled friend for more than the bound (stall_after plus four steps)
NoCardHeldPastBound ==
  \A f \in Friends :
    \A c \in Cards :
      (cardHolder[c] = f /\ cardState[c] = "dealt") =>
        (clock - LastActive(f) <= StallAfter + 4 * StallStep)

\* Invariant 2: No card is redealt while it has started
NoStartedRedealt ==
  \A c \in Cards :
    wasStarted[c] => (cardState[c] # "taken" /\ cardState[c] # "redealt")

\* Invariant 3: A friend is released only by evidence of her own work
ReleasedOnlyByEvidence ==
  ~releasedWithoutEvidence

\* Invariant 4: A friend down for stall has no evidence of work within the bound: whatever
\* evidence arrives after the down releases her (the 2026-10-05 incident is its violation)
NeverDownWhileWorking ==
  \A f \in Friends :
    status[f] = "down" => clock - LastEvidence(f) >= StallAfter + 4 * StallStep

\* The released-by-evidence invariant under the name MCStallLadder.cfg and
\* MCStallLadderBrokenRelease.cfg check
ReleasedOnlyByActivity ==
  ReleasedOnlyByEvidence /\ NeverDownWhileWorking

=============================================================================
