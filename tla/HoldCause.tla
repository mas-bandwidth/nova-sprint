---------------------------- MODULE HoldCause ----------------------------
(***************************************************************************)
(* A coordinator's hold carries its cause, and the tick ends it when the  *)
(* cause ends (internal/sprint/hold_cause.go: holdCauseClear,             *)
(* holdNoCauseConds, idleReapHolds; route_rest.go noModelCallRests;       *)
(* hold.go HoldNames with HoldReq.Cause; docs/SPEC-SPRINT.md section 11,  *)
(* hold). The owner, 2026-10-13: mini-m5, the fastest machine, sat held   *)
(* about ten hours on a free-text hold whose cause had been found to be   *)
(* the providers, fleet-wide, and was never released.                      *)
(*                                                                       *)
(* held[m]: "none"; "cause", the coordinator's hold naming a cause (a    *)
(* judgment j<n>, or a doctor check on that member); "nocause", the      *)
(* coordinator's hold with none; "idle", the machine's hold of a member  *)
(* whose takes ended with no model call on routes that serve elsewhere   *)
(* (its cause is the judgment it raised). cause[m] is that cause's state: *)
(* "open" until the judgment is answered or retired, or the doctor check *)
(* passes on the member; then "resolved".                                 *)
(*                                                                       *)
(* age[m] counts the ticks a cause-less hold has stood, saturating at    *)
(* MaxAge; Bound ticks stand for the two hours of the report. reported[m] *)
(* is the last period (age \div Bound) the tick reported to the seat.     *)
(*                                                                       *)
(* The attribution of an idle reap (a take that ended with no result and *)
(* no model call): reaps is the members with such a take on the route in *)
(* the window, okElse the members with a take that ended ok on it.        *)
(* Two or more members reaping on one route is the route's fault: it      *)
(* rests (rested) and no member is held. A member is held for its reaps   *)
(* only when it reaps alone and the route serves another member.         *)
(*                                                                       *)
(* phase is "env" after an outside step and "ticked" after the tick, so  *)
(* the safety rules are read at the tick's end, where the code states    *)
(* them: "the tick releases the hold in the same step".                   *)
(*                                                                       *)
(* Broken (the reversed witness):                                        *)
(*  "holdleaks"    - the tick never ends a hold whose cause resolved (the *)
(*                   night of 2026-10-12): NoHoldOutlivesItsCause fails. *)
(*  "silent"       - a cause-less hold is never reported: NoSilentHold   *)
(*                   fails.                                               *)
(*  "blamemember"  - every reaping member is held, whatever the route:    *)
(*                   HeldOnlyWhenAlone fails.                             *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Members, Bound, MaxAge, Broken

VARIABLES held, cause, age, reported, reaps, okElse, rested, phase

vars == <<held, cause, age, reported, reaps, okElse, rested, phase>>

Holds == {"none", "cause", "nocause", "idle"}

TypeOK ==
    /\ held \in [Members -> Holds]
    /\ cause \in [Members -> {"none", "open", "resolved"}]
    /\ age \in [Members -> 0..MaxAge]
    /\ reported \in [Members -> 0..MaxAge]
    /\ reaps \subseteq Members
    /\ okElse \subseteq Members
    /\ rested \in BOOLEAN
    /\ phase \in {"env", "ticked"}

Init ==
    /\ held = [m \in Members |-> "none"]
    /\ cause = [m \in Members |-> "none"]
    /\ age = [m \in Members |-> 0]
    /\ reported = [m \in Members |-> 0]
    /\ reaps = {}
    /\ okElse = {}
    /\ rested = FALSE
    /\ phase = "env"

\* the coordinator holds a member and names its cause (hold --cause j<n> or
\* --cause doctor:<check>): the cause is open
HoldWithCause(m) ==
    /\ held[m] = "none"
    /\ held' = [held EXCEPT ![m] = "cause"]
    /\ cause' = [cause EXCEPT ![m] = "open"]
    /\ phase' = "env"
    /\ UNCHANGED <<age, reported, reaps, okElse, rested>>

\* the coordinator holds a member with no cause: its clock starts
HoldNoCause(m) ==
    /\ held[m] = "none"
    /\ held' = [held EXCEPT ![m] = "nocause"]
    /\ age' = [age EXCEPT ![m] = 0]
    /\ reported' = [reported EXCEPT ![m] = 0]
    /\ phase' = "env"
    /\ UNCHANGED <<cause, reaps, okElse, rested>>

\* the cause ends: the judgment is answered or retired, or the doctor check
\* passes on the member (the doctor's pass record)
Resolve(m) ==
    /\ cause[m] = "open"
    /\ cause' = [cause EXCEPT ![m] = "resolved"]
    /\ phase' = "env"
    /\ UNCHANGED <<held, age, reported, reaps, okElse, rested>>

\* the coordinator releases a hold by hand (unhold)
Unhold(m) ==
    /\ held[m] # "none"
    /\ held' = [held EXCEPT ![m] = "none"]
    /\ cause' = [cause EXCEPT ![m] = "none"]
    /\ age' = [age EXCEPT ![m] = 0]
    /\ reported' = [reported EXCEPT ![m] = 0]
    /\ phase' = "env"
    /\ UNCHANGED <<reaps, okElse, rested>>

\* a take of an up member on the route ends with no result and no model call
Reap(m) ==
    /\ held[m] = "none"
    /\ ~rested
    /\ m \notin reaps
    /\ reaps' = reaps \cup {m}
    /\ phase' = "env"
    /\ UNCHANGED <<held, cause, age, reported, okElse, rested>>

\* a take of an up member on the route ends ok
Ok(m) ==
    /\ held[m] = "none"
    /\ ~rested
    /\ m \notin okElse
    /\ okElse' = okElse \cup {m}
    /\ phase' = "env"
    /\ UNCHANGED <<held, cause, age, reported, reaps, rested>>

\* the window slides past every end it held, or the route's rest ends: the
\* ends before it count no more (the rest's mark, the hold's mark)
WindowEnds ==
    /\ reaps # {} \/ okElse # {} \/ rested
    /\ reaps' = {}
    /\ okElse' = {}
    /\ rested' = FALSE
    /\ phase' = "env"
    /\ UNCHANGED <<held, cause, age, reported>>

\* the member reaps alone on a route that serves another member: its own
AloneOnAServingRoute(m) == reaps = {m} /\ okElse \ {m} # {}

\* whether the tick holds m for its reaps: alone on a route that serves
\* another member, and the route not resting (the bug: every reaper)
HoldsForReaps(m) ==
    /\ held[m] = "none"
    /\ ~rested
    /\ IF Broken = "blamemember"
         THEN m \in reaps
         ELSE Cardinality(reaps) < 2 /\ AloneOnAServingRoute(m)

\* the tick, every member at once: a hold whose cause resolved ends; a
\* cause-less hold ages and is reported once a period past its bound; two or
\* more members reaping rest the route; a member reaping alone on a route
\* that serves elsewhere is held, its cause the judgment it raises, and the
\* hold's mark consumes the reaps it was placed for (a take before the mark
\* never holds it again: idleReapMark in the code)
Tick ==
    /\ phase' = "ticked"
    /\ rested' = (rested \/ Cardinality(reaps) >= 2)
    /\ held' = [m \in Members |->
                  IF held[m] \in {"cause", "idle"} /\ cause[m] = "resolved" /\ Broken # "holdleaks"
                    THEN "none"
                  ELSE IF HoldsForReaps(m)
                    THEN "idle"
                  ELSE held[m]]
    /\ reaps' = {r \in reaps : ~HoldsForReaps(r)}
    /\ cause' = [m \in Members |->
                   IF held'[m] = "none" THEN "none"
                   ELSE IF held[m] = "none" /\ held'[m] = "idle" THEN "open"
                   ELSE cause[m]]
    /\ age' = [m \in Members |->
                 IF held[m] = "nocause" THEN IF age[m] < MaxAge THEN age[m] + 1 ELSE MaxAge
                 ELSE 0]
    /\ reported' = [m \in Members |->
                      IF held[m] = "nocause" /\ age'[m] >= Bound /\ Broken # "silent"
                        THEN age'[m] \div Bound
                      ELSE IF held[m] = "nocause" THEN reported[m]
                      ELSE 0]
    /\ UNCHANGED <<okElse>>

Next ==
    \/ \E m \in Members : HoldWithCause(m) \/ HoldNoCause(m) \/ Resolve(m) \/ Unhold(m) \/ Reap(m) \/ Ok(m)
    \/ WindowEnds
    \/ Tick

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

(* Safety: at the tick's end no member is held whose cause has resolved.   *)
NoHoldOutlivesItsCause ==
    phase = "ticked" => \A m \in Members : ~(held[m] \in {"cause", "idle"} /\ cause[m] = "resolved")

(* Safety: at the tick's end a cause-less hold past its bound has been      *)
(* reported for the period it is in: never silently, once every period.    *)
NoSilentHold ==
    phase = "ticked" => \A m \in Members :
        (held[m] = "nocause" /\ age[m] >= Bound) => reported[m] = age[m] \div Bound

(* Safety: at the tick's end, two or more members reaping on the route have *)
(* rested it.                                                              *)
RouteFaultRests ==
    phase = "ticked" => (Cardinality(reaps) >= 2 => rested)

(* Action: the machine holds a member for its reaps only when it reaps      *)
(* alone on a route that serves another member: a route's fault never holds *)
(* a member.                                                               *)
HeldOnlyWhenAlone ==
    [][\A m \in Members : (held[m] = "none" /\ held'[m] = "idle") => (AloneOnAServingRoute(m) /\ ~rested)]_vars

(* Liveness: a hold whose cause resolved ends.                             *)
CauseEndsHold ==
    \A m \in Members : (held[m] \in {"cause", "idle"} /\ cause[m] = "resolved") ~> (held[m] = "none" \/ cause[m] # "resolved")
=============================================================================
