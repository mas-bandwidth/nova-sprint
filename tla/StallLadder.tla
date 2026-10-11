---------------------------- MODULE StallLadder ----------------------------
\* The friend stall ladder (docs/SPEC-SPRINT.md, section friend-stall-ladder-r.w1).
\* A friend that stalls holds her dealt cards until the coordinator notices by hand.
\* The stall ladder makes recovery fully mechanical as a tick part in
\* internal/sprint/friend_stall.go, the fleet's update after presence (TickTables,
\* internal/sprint/steps_tick.go).
\*
\* A friend is stalled when she holds dealt cards and the newest of her activity,
\* a progress stamp, or a dealt/taken stamp on one of her cards is older than
\* friend_stall_after (20m). Her activity is her session's, a beat of hers naming
\* running cards, and a finish on her row; the one action FriendActivity stands for
\* all three. A dealt/taken stamp is the deal's `dealt` field or the `taken` field
\* she set on a card she took into working (friend_stall.go reads all four of
\* `dealt`, `first_dealt`, `taken`, `first_taken`; the one variable cardDealt stands
\* for them). The idle clock counts running time: the time the machine spent STOPPED
\* is subtracted (friend_stall.go, r.Stopped; StoppedTick stands for it), so a stop
\* never counts against the bound.
\* The ladder climbs one rung per friend_stall_step (5m) while she stays stalled:
\*   (1) a wake turn: a bus message to her that her daemon pushes in as a turn
\*   (2) a second wake
\*   (3) a coordinator note (a pushed judgment, "friend <f> stalled <d>: two wakes unanswered")
\*   (4) every card of hers she has not started taken back (FriendTake with All;
\*       a started card stays with her and finishes), ready for the next deal
\*   (5) she is marked down with reason "stalled", and released to up by the tick itself
\*       at her first activity after it (never card progress alone).
\* "Started" is as the code reads it (friend_stall.go): progress stamped on the card,
\* a report of hers on it, or her beat naming it running; the model's FriendStartCard
\* stands for the beat, CardProgressStamp for a progress stamp, and both mark the card
\* started, so the rung 4 take back skips it. Every rung is a happened note pushed like
\* the rest; any activity, progress, or dealt/taken stamp resets her to rung 0.
\* The rung climbs only upward and resets only at 0 (friend_stall.go: the rung is
\* written only when the target is higher, or cleared at 0).
\*
\* The wakes (pr-friend-stall-completeb.w1): the store's tick hands the part a
\* TickReq.WakeFriend that only collects the wakes of the plan being made, and sends
\* the last plan's through Store.WakeFriend (cmd/nova-sprint stallWaker, wakeFriendStall)
\* once the part's step commits (internal/sprint/store/tick.go, tickRun.parts). The tick
\* also plans a part it does not commit: to see whether it has work, and again after a
\* commit lost to another writer. PlanUncommitted is that plan; it sends nothing. woken
\* counts the wakes sent to her in this stall episode, and returns to 0 with her rung.
\*
\* Invariants:
\*   NoCardHeldPastBound: no card is held by a stalled friend for more than the
\*     bound (stall_after plus four steps) of running time.
\*   NoStartedRedealt: no card is redealt while it has started.
\*   NoStampedTaken: no card with a progress stamp is taken back (it has started).
\*   ReleasedOnlyByActivity: a friend is released only by her own activity.
\*   WokenAtEveryWakeRung: a friend past rung n (n = 1, 2) was sent wake n.
\*   NoWakeWithoutRung: no wake is sent but at a rung the ladder climbed, once.
\*
\* Broken values:
\*   "none"          the design
\*   "notaken"       rung 4 fails to take back unstarted cards (breaks NoCardHeldPastBound)
\*   "takestarted"   rung 4 takes back started cards too, allowing redeal (breaks NoStartedRedealt)
\*   "takestamped"   rung 4 takes back a card with a progress stamp too (breaks NoStampedTaken)
\*   "releaseother"  friend is released to up without activity of hers (breaks ReleasedOnlyByActivity)
\*   "nowake"        the wake is planned and never sent: wakeFriendStall never called, as
\*                   friend-stall-ladder-r left it (breaks WokenAtEveryWakeRung)
\*   "wakeinplan"    the planner's TickReq.WakeFriend sends the wake as it plans (the
\*                   store's hook handed to it whole, as before this card): a plan not
\*                   committed wakes her again
\*                   (breaks NoWakeWithoutRung)

EXTENDS Naturals, FiniteSets

CONSTANTS Friends, Cards, StallAfter, StallStep, MaxTime, Broken

VARIABLES clock, rung, cardState, cardHolder, activity, cardProgress, cardDealt, stopTime, status, wasStarted, releasedWithoutActivity, woken

vars == <<clock, rung, cardState, cardHolder, activity, cardProgress, cardDealt, stopTime, status, wasStarted, releasedWithoutActivity, woken>>

\* The wake rungs: a wake goes at each of them.
WakeRungs == {1, 2}

Min(a, b) == IF a < b THEN a ELSE b

CardStates == {"dealt", "started", "taken", "redealt"}
FriendStatuses == {"up", "down"}

TypeOK ==
  /\ clock \in 0..MaxTime
  /\ rung \in [Friends -> 0..5]
  /\ cardState \in [Cards -> CardStates]
  /\ cardHolder \in [Cards -> Friends]
  /\ activity \in [Friends -> 0..MaxTime]
  /\ cardProgress \in [Cards -> 0..MaxTime]
  /\ cardDealt \in [Cards -> 0..MaxTime]
  /\ stopTime \in [Friends -> 0..MaxTime]
  /\ status \in [Friends -> FriendStatuses]
  /\ wasStarted \in [Cards -> BOOLEAN]
  /\ releasedWithoutActivity \in BOOLEAN
  /\ woken \in [Friends -> 0..MaxTime]

Init ==
  /\ clock = 0
  /\ rung = [f \in Friends |-> 0]
  /\ cardState = [c \in Cards |-> "dealt"]
  /\ cardHolder = [c \in Cards |-> CHOOSE f \in Friends : TRUE]
  /\ activity = [f \in Friends |-> 0]
  /\ cardProgress = [c \in Cards |-> 0]
  /\ cardDealt = [c \in Cards |-> 0]
  /\ stopTime = [f \in Friends |-> 0]
  /\ status = [f \in Friends |-> "up"]
  /\ wasStarted = [c \in Cards |-> FALSE]
  /\ releasedWithoutActivity = FALSE
  /\ woken = [f \in Friends |-> 0]

\* The cards of friend f she still holds (dealt or started).
HeldCards(f) == {c \in Cards : cardHolder[c] = f /\ cardState[c] \in {"dealt", "started"}}

\* Latest evidence of life for friend f: her activity, a progress stamp, or a
\* dealt/taken stamp, on a card she holds.
LastActive(f) ==
  LET allTimes == {activity[f]}
                  \cup {cardProgress[c] : c \in HeldCards(f)}
                  \cup {cardDealt[c] : c \in HeldCards(f)}
  IN CHOOSE t \in allTimes : \A other \in allTimes : t >= other

\* Friend holds cards in dealt or started state.
HoldsCards(f) == HeldCards(f) # {}

\* Running idle time of friend f at clock time t: the wall time since her latest
\* evidence of life less the time the machine spent STOPPED, never negative.
Idle(f, t) ==
  LET raw == t - LastActive(f) - stopTime[f]
  IN IF raw < 0 THEN 0 ELSE raw

\* The rung the ladder is at for friend f at clock time t, idle for Idle running time.
RungAt(f, t) ==
  IF ~HoldsCards(f) \/ Idle(f, t) < StallAfter THEN 0
  ELSE IF Idle(f, t) < StallAfter + StallStep THEN 1
  ELSE IF Idle(f, t) < StallAfter + 2 * StallStep THEN 2
  ELSE IF Idle(f, t) < StallAfter + 3 * StallStep THEN 3
  ELSE IF Idle(f, t) < StallAfter + 4 * StallStep THEN 4
  ELSE 5

\* The wakes a climb from rung a to rung b sends: one for each wake rung it passes.
Climbed(a, b) == Cardinality({n \in WakeRungs : a < n /\ n <= b})

\* Clock tick (the machine running): advances clock and updates the stall ladder;
\* the part's step commits, and the binding sends the wakes its plan carries. The
\* rung climbs only when the target rung is higher and resets only at 0.
Tick ==
  /\ clock < MaxTime
  /\ clock' = clock + 1
  /\ rung' = [f \in Friends |->
       LET next == RungAt(f, clock + 1)
       IN IF next = 0 THEN 0
          ELSE IF next > rung[f] THEN next
          ELSE rung[f]]
  /\ woken' = [f \in Friends |->
       IF rung'[f] = 0 THEN 0
       ELSE IF Broken = "nowake" THEN woken[f]
       ELSE woken[f] + Climbed(rung[f], rung'[f])]
  /\ cardState' = [c \in Cards |->
       LET f == cardHolder[c]
       IN IF HoldsCards(f) /\ Idle(f, clock + 1) >= StallAfter + 3 * StallStep /\ rung[f] < 4 THEN
            IF Broken = "notaken" THEN cardState[c]
            ELSE IF Broken = "takestarted" THEN
              IF cardState[c] \in {"dealt", "started"} THEN "taken" ELSE cardState[c]
            ELSE IF Broken = "takestamped" THEN
              IF cardState[c] = "dealt" \/ cardProgress[c] > 0 THEN "taken" ELSE cardState[c]
            ELSE
              IF cardState[c] = "dealt" THEN "taken" ELSE cardState[c]
          ELSE cardState[c]]
  /\ status' = [f \in Friends |->
       IF HoldsCards(f) /\ Idle(f, clock + 1) >= StallAfter + 4 * StallStep THEN "down"
       ELSE status[f]]
  /\ UNCHANGED <<cardHolder, activity, cardProgress, cardDealt, stopTime, wasStarted, releasedWithoutActivity>>

\* A tick the machine spent STOPPED: wall time advances but the ladder's idle clock
\* does not, so stopped time never counts against the stall (r.Stopped in the code).
StoppedTick ==
  /\ clock < MaxTime
  /\ clock' = clock + 1
  /\ stopTime' = [f \in Friends |-> IF stopTime[f] < MaxTime THEN stopTime[f] + 1 ELSE stopTime[f]]
  /\ UNCHANGED <<rung, cardState, cardHolder, activity, cardProgress, cardDealt, status, wasStarted, releasedWithoutActivity, woken>>

\* The tick plans the stall part for friend f and does not commit the plan: a part
\* planned to see whether it has work, or planned again after a commit lost to another
\* writer. The design sends nothing from a plan (a stutter); "wakeinplan" sends the wake
\* of a rung the plan would climb to.
PlanUncommitted(f) ==
  /\ Broken = "wakeinplan"
  /\ woken[f] < MaxTime
  /\ LET next == RungAt(f, clock + 1)
     IN Climbed(rung[f], next) > 0
  /\ woken' = [woken EXCEPT ![f] = @ + 1]
  /\ UNCHANGED <<clock, rung, cardState, cardHolder, activity, cardProgress, cardDealt, stopTime, status, wasStarted, releasedWithoutActivity>>

\* Friend has activity: her session's, a beat naming running cards, or a finish on her row
FriendActivity(f) ==
  /\ activity' = [activity EXCEPT ![f] = clock]
  /\ stopTime' = [stopTime EXCEPT ![f] = 0]
  /\ rung' = [rung EXCEPT ![f] = 0]
  /\ woken' = [woken EXCEPT ![f] = 0]
  /\ status' = [status EXCEPT ![f] = "up"]
  /\ UNCHANGED <<clock, cardState, cardHolder, cardProgress, cardDealt, wasStarted, releasedWithoutActivity>>

\* Friend stamps progress on card c: a progress stamp is a sign of life and, as the
\* code reads "started", marks the card started so rung 4 never takes it back.
CardProgressStamp(c) ==
  /\ cardState[c] \in {"dealt", "started"}
  /\ cardProgress' = [cardProgress EXCEPT ![c] = clock]
  /\ cardState' = [cardState EXCEPT ![c] = "started"]
  /\ wasStarted' = [wasStarted EXCEPT ![c] = TRUE]
  /\ stopTime' = [stopTime EXCEPT ![cardHolder[c]] = 0]
  /\ rung' = [rung EXCEPT ![cardHolder[c]] = 0]
  /\ woken' = [woken EXCEPT ![cardHolder[c]] = 0]
  /\ UNCHANGED <<clock, cardHolder, activity, cardDealt, status, releasedWithoutActivity>>

\* Friend starts card c (her beat names it running): started, and its taken stamp is a
\* sign of life.
FriendStartCard(c) ==
  /\ cardState[c] = "dealt"
  /\ cardState' = [cardState EXCEPT ![c] = "started"]
  /\ wasStarted' = [wasStarted EXCEPT ![c] = TRUE]
  /\ cardDealt' = [cardDealt EXCEPT ![c] = clock]
  /\ stopTime' = [stopTime EXCEPT ![cardHolder[c]] = 0]
  /\ rung' = [rung EXCEPT ![cardHolder[c]] = 0]
  /\ woken' = [woken EXCEPT ![cardHolder[c]] = 0]
  /\ UNCHANGED <<clock, cardHolder, activity, cardProgress, status, releasedWithoutActivity>>

\* Taken card is redealt (its dealt stamp is a sign of life)
RedealCard(c) ==
  /\ cardState[c] = "taken"
  /\ cardState' = [cardState EXCEPT ![c] = "redealt"]
  /\ cardDealt' = [cardDealt EXCEPT ![c] = clock]
  /\ stopTime' = [stopTime EXCEPT ![cardHolder[c]] = 0]
  /\ rung' = [rung EXCEPT ![cardHolder[c]] = 0]
  /\ woken' = [woken EXCEPT ![cardHolder[c]] = 0]
  /\ UNCHANGED <<clock, cardHolder, activity, cardProgress, status, wasStarted, releasedWithoutActivity>>

\* Spurious release without session activity (reversed witness)
SpuriousRelease(f) ==
  /\ Broken = "releaseother"
  /\ status[f] = "down"
  /\ status' = [status EXCEPT ![f] = "up"]
  /\ releasedWithoutActivity' = TRUE
  /\ UNCHANGED <<clock, rung, cardState, cardHolder, activity, cardProgress, cardDealt, stopTime, wasStarted, woken>>

Next ==
  \/ Tick
  \/ StoppedTick
  \/ \E f \in Friends : FriendActivity(f)
  \/ \E c \in Cards : CardProgressStamp(c)
  \/ \E c \in Cards : FriendStartCard(c)
  \/ \E c \in Cards : RedealCard(c)
  \/ \E f \in Friends : SpuriousRelease(f)
  \/ \E f \in Friends : PlanUncommitted(f)

Spec == Init /\ [][Next]_vars

\* Invariant 1: No card is held by a stalled friend for more than the bound (stall_after plus four steps)
NoCardHeldPastBound ==
  \A f \in Friends :
    \A c \in Cards :
      (cardHolder[c] = f /\ cardState[c] = "dealt") =>
        (Idle(f, clock) <= StallAfter + 4 * StallStep)

\* Invariant 2: No card is redealt while it has started
NoStartedRedealt ==
  \A c \in Cards :
    wasStarted[c] => (cardState[c] # "taken" /\ cardState[c] # "redealt")

\* Invariant 3: No card with a progress stamp is taken back (a progress stamp marks it started)
NoStampedTaken ==
  \A c \in Cards :
    cardProgress[c] > 0 => (cardState[c] # "taken" /\ cardState[c] # "redealt")

\* Invariant 4: A friend is released only by her own activity
ReleasedOnlyByActivity ==
  ~releasedWithoutActivity

\* Invariant 5: A friend past a wake rung was sent its wake
WokenAtEveryWakeRung ==
  \A f \in Friends : woken[f] >= Min(rung[f], 2)

\* Invariant 6: No wake is sent but at a rung the ladder climbed, and that once
NoWakeWithoutRung ==
  \A f \in Friends : woken[f] <= Min(rung[f], 2)

=============================================================================
