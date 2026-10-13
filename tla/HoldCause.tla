---------------------------- MODULE HoldCause ----------------------------
(***************************************************************************)
(* A coordinator's hold carries its cause, and the tick ends it when the  *)
(* cause ends (internal/sprint/hold_cause.go: holdCauseClear,             *)
(* holdNoCauseConds, idleReapHolds, noModelCallRests, ctlWritten;         *)
(* steps_clear.go ShapeOf/RestoreShape; route_rest.go RestsDue; hold.go   *)
(* HoldNames with HoldReq.Cause; docs/SPEC-SPRINT.md section 11, hold).   *)
(* The owner, 2026-10-13: mini-m5, the fastest machine, sat held about    *)
(* ten hours on a free-text hold whose cause had been found to be the     *)
(* providers, fleet-wide, and was never released.                         *)
(*                                                                       *)
(* held[m]: "none"; "cause", a hold naming a cause (a judgment, a doctor *)
(* check on that member); "nocause", a hold with none, whoever made it    *)
(* (a mark is no cause); "idle", the machine's hold of a member whose     *)
(* takes ended with no model call on the route (its cause is the          *)
(* judgment it raised, and the route it names). cause[m] is that cause:   *)
(* "open" until the judgment is answered or retired (a wait parks it and  *)
(* is no answer: no action here resolves on a wait), or the doctor check  *)
(* passes on the member; then "resolved".                                 *)
(*                                                                       *)
(* age[m] counts the ticks a cause-less hold has stood, saturating at    *)
(* MaxAge; Bound ticks stand for the two hours of the report. reported[m] *)
(* is the last period (age \div Bound) the tick reported to the seat.     *)
(*                                                                       *)
(* One route. reaps is the members with an idle reap on it in the window, *)
(* counted as the code counts them (reapersBy over every reap in the      *)
(* window): a hold's mark never takes a reap out of the route's count.    *)
(* count[m] is m's reaps in the window and fresh[m] those after its hold  *)
(* mark (idleReapMark), both saturating at Need (IdleReapHoldAfter, 3 in  *)
(* the code). servedAfter[m] is the other members that ended a take ok on *)
(* the route after m's first reap (servedAfter in the code): an ok end    *)
(* before the first reap says nothing of the route since.                 *)
(*                                                                       *)
(* The route rests (rested) for no model call when two or more members    *)
(* reaped on it, or one member reaped Need times and no one served it     *)
(* after that member's first reap (a dead provider: the route rests, the  *)
(* member is not held, the seat is told). A member is held for its reaps  *)
(* only when it reaped Need times since its mark, alone, on a route       *)
(* another member served after its first reap, and the route rests not;  *)
(* a rest of the route ends that hold in the same tick.                   *)
(*                                                                       *)
(* busy is the members whose control card another unit of the coming     *)
(* tick writes (ctlWritten): the tick defers their release or hold to the *)
(* next tick (deferred). A card is written by another unit at most one    *)
(* tick running (the deal writes no held member's card; faultClear writes *)
(* only fault holds), so a deferral is never two ticks long.              *)
(*                                                                       *)
(* Clear: every hold crosses with its cause (Shape.HeldFields); the       *)
(* judgments of the epoch are retired, so an idle hold's cause resolves;  *)
(* a cause-less hold's clock begins again (its held stamp is the new      *)
(* epoch's); the window and the rest are the old epoch's and are gone.    *)
(*                                                                       *)
(* phase is "env" after an outside step and "ticked" after the tick, so  *)
(* the safety rules are read at the tick's end, where the code states    *)
(* them.                                                                  *)
(*                                                                       *)
(* Broken (the reversed witness):                                        *)
(*  "holdleaks"    - the tick never ends a hold whose cause resolved (the *)
(*                   night of 2026-10-12): NoHoldOutlivesItsCause fails. *)
(*  "silent"       - a cause-less hold is never reported: NoSilentHold   *)
(*                   fails.                                               *)
(*  "blamemember"  - every reaping member is held, whatever the route:    *)
(*                   HeldOnlyWhenAlone fails.                             *)
(*  "restkeeps"    - a rest of the route for no model call leaves the     *)
(*                   idle hold placed for it (the cold read of #206):     *)
(*                   NoHoldForARestedRoute fails.                         *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Members, Bound, MaxAge, Need, Broken

VARIABLES held, cause, age, reported, reaps, count, fresh, servedAfter, rested, busy, deferred, phase

vars == <<held, cause, age, reported, reaps, count, fresh, servedAfter, rested, busy, deferred, phase>>

Holds == {"none", "cause", "nocause", "idle"}

TypeOK ==
    /\ held \in [Members -> Holds]
    /\ cause \in [Members -> {"none", "open", "resolved"}]
    /\ age \in [Members -> 0..MaxAge]
    /\ reported \in [Members -> 0..MaxAge]
    /\ reaps \subseteq Members
    /\ count \in [Members -> 0..Need]
    /\ fresh \in [Members -> 0..Need]
    /\ servedAfter \in [Members -> SUBSET Members]
    /\ rested \in BOOLEAN
    /\ busy \subseteq Members
    /\ deferred \subseteq Members
    /\ phase \in {"env", "ticked"}

Init ==
    /\ held = [m \in Members |-> "none"]
    /\ cause = [m \in Members |-> "none"]
    /\ age = [m \in Members |-> 0]
    /\ reported = [m \in Members |-> 0]
    /\ reaps = {}
    /\ count = [m \in Members |-> 0]
    /\ fresh = [m \in Members |-> 0]
    /\ servedAfter = [m \in Members |-> {}]
    /\ rested = FALSE
    /\ busy = {}
    /\ deferred = {}
    /\ phase = "env"

Win == <<reaps, count, fresh, servedAfter, rested>>

\* the coordinator holds a member and names its cause: the cause is open
HoldWithCause(m) ==
    /\ held[m] = "none"
    /\ held' = [held EXCEPT ![m] = "cause"]
    /\ cause' = [cause EXCEPT ![m] = "open"]
    /\ phase' = "env"
    /\ UNCHANGED <<age, reported, busy, deferred>> /\ UNCHANGED Win

\* a hold with no cause, the coordinator's or a mark's: its clock starts
HoldNoCause(m) ==
    /\ held[m] = "none"
    /\ held' = [held EXCEPT ![m] = "nocause"]
    /\ age' = [age EXCEPT ![m] = 0]
    /\ reported' = [reported EXCEPT ![m] = 0]
    /\ phase' = "env"
    /\ UNCHANGED <<cause, busy, deferred>> /\ UNCHANGED Win

\* the cause ends: the judgment is answered or retired, or the doctor check passes
Resolve(m) ==
    /\ cause[m] = "open"
    /\ cause' = [cause EXCEPT ![m] = "resolved"]
    /\ phase' = "env"
    /\ UNCHANGED <<held, age, reported, busy, deferred>> /\ UNCHANGED Win

\* the coordinator releases a hold by hand (unhold)
Unhold(m) ==
    /\ held[m] # "none"
    /\ held' = [held EXCEPT ![m] = "none"]
    /\ cause' = [cause EXCEPT ![m] = "none"]
    /\ age' = [age EXCEPT ![m] = 0]
    /\ reported' = [reported EXCEPT ![m] = 0]
    /\ phase' = "env"
    /\ UNCHANGED <<busy, deferred>> /\ UNCHANGED Win

\* a take of an up member on the route ends with no result and no model call
Reap(m) ==
    /\ held[m] = "none"
    /\ ~rested
    /\ count[m] < Need
    /\ reaps' = reaps \cup {m}
    /\ count' = [count EXCEPT ![m] = count[m] + 1]
    /\ fresh' = [fresh EXCEPT ![m] = IF fresh[m] < Need THEN fresh[m] + 1 ELSE Need]
    /\ phase' = "env"
    /\ UNCHANGED <<held, cause, age, reported, servedAfter, rested, busy, deferred>>

\* a take of up member o on the route ends ok: it serves every member that reaped
\* before it, and no member that reaps after it
Ok(o) ==
    /\ held[o] = "none"
    /\ ~rested
    /\ \E m \in reaps \ {o} : o \notin servedAfter[m]
    /\ servedAfter' = [m \in Members |-> IF m \in reaps /\ m # o THEN servedAfter[m] \cup {o} ELSE servedAfter[m]]
    /\ phase' = "env"
    /\ UNCHANGED <<held, cause, age, reported, reaps, count, fresh, rested, busy, deferred>>

\* another unit of the coming tick writes m's control card, never two ticks running
CtlWrite(m) ==
    /\ m \notin busy
    /\ m \notin deferred
    /\ busy' = busy \cup {m}
    /\ phase' = "env"
    /\ UNCHANGED <<held, cause, age, reported, deferred>> /\ UNCHANGED Win

\* the window slides past every end it held, and the route's rest ends with it
WindowEnds ==
    /\ reaps # {} \/ rested \/ \E m \in Members : servedAfter[m] # {}
    /\ reaps' = {}
    /\ count' = [m \in Members |-> 0]
    /\ fresh' = [m \in Members |-> 0]
    /\ servedAfter' = [m \in Members |-> {}]
    /\ rested' = FALSE
    /\ phase' = "env"
    /\ UNCHANGED <<held, cause, age, reported, busy, deferred>>

\* a clear: every hold crosses with its cause; the epoch's judgments are retired, so an
\* idle hold's cause resolves; a cause-less hold's clock begins again; the window and
\* the rest are gone
Clear ==
    /\ \E m \in Members : held[m] # "none"
    /\ cause' = [m \in Members |-> IF held[m] = "idle" THEN "resolved" ELSE cause[m]]
    /\ age' = [m \in Members |-> 0]
    /\ reported' = [m \in Members |-> 0]
    /\ reaps' = {}
    /\ count' = [m \in Members |-> 0]
    /\ fresh' = [m \in Members |-> 0]
    /\ servedAfter' = [m \in Members |-> {}]
    /\ rested' = FALSE
    /\ phase' = "env"
    /\ UNCHANGED <<held, busy, deferred>>

\* the route rests now for no model call: two or more members reaped on it, or one
\* member reaped Need times and no one served it after that member's first reap
DeadAlone(m) == reaps = {m} /\ count[m] >= Need /\ servedAfter[m] = {}
RestNow == rested \/ Cardinality(reaps) >= 2 \/ \E m \in Members : DeadAlone(m)

\* the tick holds m for its reaps (the bug: every reaper)
HoldsForReaps(m) ==
    /\ held[m] = "none"
    /\ ~RestNow
    /\ IF Broken = "blamemember"
         THEN m \in reaps
         ELSE reaps = {m} /\ fresh[m] >= Need /\ servedAfter[m] # {}

\* the tick ends m's hold: its cause resolved, or its route rests for no model call
Releases(m) ==
    /\ Broken # "holdleaks"
    /\ \/ held[m] \in {"cause", "idle"} /\ cause[m] = "resolved"
       \/ held[m] = "idle" /\ RestNow /\ Broken # "restkeeps"

\* the tick, every member at once; a member whose card another unit writes this tick
\* waits for the next
Tick ==
    /\ phase' = "ticked"
    /\ rested' = RestNow
    /\ deferred' = {m \in busy : Releases(m) \/ HoldsForReaps(m)}
    /\ busy' = {}
    /\ held' = [m \in Members |->
                  IF m \in busy THEN held[m]
                  ELSE IF Releases(m) THEN "none"
                  ELSE IF HoldsForReaps(m) THEN "idle"
                  ELSE held[m]]
    /\ cause' = [m \in Members |->
                   IF held'[m] = "none" THEN "none"
                   ELSE IF held[m] = "none" /\ held'[m] = "idle" THEN "open"
                   ELSE cause[m]]
    /\ fresh' = [m \in Members |-> IF held[m] = "none" /\ held'[m] = "idle" THEN 0 ELSE fresh[m]]
    /\ age' = [m \in Members |->
                 IF held[m] = "nocause" THEN IF age[m] < MaxAge THEN age[m] + 1 ELSE MaxAge
                 ELSE 0]
    /\ reported' = [m \in Members |->
                      IF held[m] = "nocause" /\ age'[m] >= Bound /\ Broken # "silent"
                        THEN age'[m] \div Bound
                      ELSE IF held[m] = "nocause" THEN reported[m]
                      ELSE 0]
    /\ UNCHANGED <<reaps, count, servedAfter>>

Next ==
    \/ \E m \in Members : HoldWithCause(m) \/ HoldNoCause(m) \/ Resolve(m) \/ Unhold(m) \/ Reap(m) \/ Ok(m) \/ CtlWrite(m)
    \/ WindowEnds
    \/ Clear
    \/ Tick

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

(* Safety: at the tick's end no member is held whose cause has resolved,    *)
(* but one whose card another unit wrote this tick (deferred, one tick).    *)
NoHoldOutlivesItsCause ==
    phase = "ticked" => \A m \in Members :
        (held[m] \in {"cause", "idle"} /\ cause[m] = "resolved") => m \in deferred

(* Safety: at the tick's end no idle hold stands on a route that rests for  *)
(* no model call (the route's fault after all), but a deferred one.        *)
NoHoldForARestedRoute ==
    phase = "ticked" => \A m \in Members : (held[m] = "idle" /\ rested) => m \in deferred

(* Safety: at the tick's end a cause-less hold past its bound has been      *)
(* reported for the period it is in: never silently, once every period.    *)
NoSilentHold ==
    phase = "ticked" => \A m \in Members :
        (held[m] = "nocause" /\ age[m] >= Bound) => reported[m] = age[m] \div Bound

(* Safety: at the tick's end the route rests when its reaps are its own:    *)
(* two or more members, or a dead route only one member used.              *)
RouteFaultRests ==
    phase = "ticked" =>
        /\ (Cardinality(reaps) >= 2 => rested)
        /\ \A m \in Members : DeadAlone(m) => rested

(* Action: the machine holds a member for its reaps only when it reaps      *)
(* alone on a route another member served after its first reap, and the    *)
(* route does not rest: a route's fault never holds a member.              *)
HeldOnlyWhenAlone ==
    [][\A m \in Members : (held[m] = "none" /\ held'[m] = "idle") => (reaps = {m} /\ servedAfter[m] # {} /\ ~rested')]_vars

(* Liveness: a hold whose cause resolved ends.                             *)
CauseEndsHold ==
    \A m \in Members : (held[m] \in {"cause", "idle"} /\ cause[m] = "resolved") ~> (held[m] = "none" \/ cause[m] # "resolved")
=============================================================================
