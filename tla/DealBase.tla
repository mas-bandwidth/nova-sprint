----------------------------- MODULE DealBase -----------------------------
(***************************************************************************)
(* The deal's two preconditions on BASE (internal/sprint/deal_base.go:    *)
(* BaseRedWhy, BriefPreflightWhy, PreflightAtBase, InheritedRedOf; the    *)
(* tick's deal, steps_tick.go TickDeal; the finish, steps_work.go         *)
(* finishPlan; the bound, brief_bound.go AttemptsSince; the binding,      *)
(* cmd/nova-sprint/dealfacts.go). 2026-10-24: stream rocketnet-final      *)
(* landed 1 of 20 first time; attempts were dealt onto a BASE already red *)
(* on their platforms, onto PATHS files BASE had renamed, and before a    *)
(* parent behind "Needs: none" landed, and each failure counted against  *)
(* the card.                                                              *)
(*                                                                       *)
(* One ready card and its BASE, as the binding last read it.              *)
(*   red      BASE's newest completed CI is red on a platform the card's  *)
(*            GATE-NEEDS names (the outside: CI runs, BASE moves).        *)
(*   defect   the brief fails against BASE: a PATHS or START file BASE    *)
(*            lacks, or a DEPENDS-ON parent not landed and not in Needs   *)
(*            (the outside: a brief edit, the parent landing, BASE        *)
(*            moving).                                                    *)
(*   col      the primary: "ready", "working" (an attempt dealt), "done". *)
(*   spent    attempts dealt; counted those that count toward the bound   *)
(*            and the first-time stat; inherited those that failed on a   *)
(*            red inherited from BASE.                                    *)
(*   judged   the open judgment on the card: "none", "basered" (the       *)
(*            BASE's NBaseRedWait naming it), "defect" (NBriefPreflight). *)
(*   dealtOn  a ghost: an attempt was dealt while red or defect held.     *)
(*   phase    "env" after an outside step, "ticked" after the tick: the   *)
(*            rules are read at the tick's end, where the code states    *)
(*            them.                                                      *)
(*                                                                       *)
(* The deal always has room in this model (a full fleet is the deal's    *)
(* own wait, dealt by DirtyTick.tla); MaxAttempts bounds the state space. *)
(*                                                                       *)
(* Broken (the reversed witnesses):                                       *)
(*  "dealonred"       the deal ignores the BASE's CI (before 2026-10-24): *)
(*                    NeverDealtOnRed fails.                              *)
(*  "keepjudgment"    the BASE's judgment is kept after BASE is green, the *)
(*                    card waiting on it: GreenDealsInOneTick fails.      *)
(*  "inheritedcounts" an inherited red counts as the card's failure:      *)
(*                    InheritedNotCounted fails.                          *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS MaxAttempts, Broken

VARIABLES red, defect, col, spent, counted, inherited, judged, dealtOn, phase

vars == <<red, defect, col, spent, counted, inherited, judged, dealtOn, phase>>

TypeOK ==
    /\ red \in BOOLEAN
    /\ defect \in BOOLEAN
    /\ col \in {"ready", "working", "done"}
    /\ spent \in 0..MaxAttempts
    /\ counted \in 0..MaxAttempts
    /\ inherited \in 0..MaxAttempts
    /\ judged \in {"none", "basered", "defect"}
    /\ dealtOn \in BOOLEAN
    /\ phase \in {"env", "ticked"}

Init ==
    /\ red \in BOOLEAN
    /\ defect \in BOOLEAN
    /\ col = "ready"
    /\ spent = 0 /\ counted = 0 /\ inherited = 0
    /\ judged = "none"
    /\ dealtOn = FALSE
    /\ phase = "env"

\* ---- the outside ----

\* BASE's CI turns, red or green, as the binding reads it.
BaseTurns ==
    /\ red' = ~red
    /\ phase' = "env"
    /\ UNCHANGED <<defect, col, spent, counted, inherited, judged, dealtOn>>

\* The brief is cut again, the parent lands, or BASE moves: the defect comes or goes.
BriefTurns ==
    /\ defect' = ~defect
    /\ phase' = "env"
    /\ UNCHANGED <<red, col, spent, counted, inherited, judged, dealtOn>>

\* The worker finishes the attempt: landed (done), failed (its failure, counted), or
\* failed on a red inherited from BASE (back to ready, not counted; finishPlan).
FinishDone ==
    /\ col = "working"
    /\ col' = "done"
    /\ phase' = "env"
    /\ UNCHANGED <<red, defect, spent, counted, inherited, judged, dealtOn>>

FinishFailed ==
    /\ col = "working"
    /\ col' = "ready"
    /\ phase' = "env"
    /\ UNCHANGED <<red, defect, spent, counted, inherited, judged, dealtOn>>

FinishInherited ==
    /\ col = "working"
    /\ col' = "ready"
    /\ inherited' = inherited + 1
    /\ counted' = IF Broken = "inheritedcounts" THEN counted ELSE counted - 1
    /\ phase' = "env"
    /\ UNCHANGED <<red, defect, spent, judged, dealtOn>>

\* ---- the tick ----

\* The deal holds a ready card to BASE: a defective brief is judged a brief defect, a red
\* BASE holds it under the BASE's judgment, and a card clear of both is dealt in this tick,
\* its judgment closed (notify closes a condition that no longer holds).
Hold == IF Broken = "dealonred" THEN defect ELSE defect \/ red

Tick ==
    /\ phase' = "ticked"
    /\ UNCHANGED <<red, defect>>
    /\ IF col = "ready" /\ ~Hold /\ spent < MaxAttempts
            /\ ~(Broken = "keepjudgment" /\ judged = "basered")
          THEN /\ col' = "working"
               /\ spent' = spent + 1
               /\ counted' = counted + 1
               /\ judged' = "none"
               /\ dealtOn' = (dealtOn \/ red \/ defect)
               /\ UNCHANGED inherited
          ELSE /\ judged' = IF col # "ready" THEN "none"
                            ELSE IF defect THEN "defect"
                            ELSE IF red /\ Broken # "dealonred" THEN "basered"
                            ELSE IF Broken = "keepjudgment" /\ judged = "basered" THEN "basered"
                            ELSE "none"
               /\ UNCHANGED <<col, spent, counted, inherited, dealtOn>>

Next == BaseTurns \/ BriefTurns \/ FinishDone \/ FinishFailed \/ FinishInherited \/ Tick

Spec == Init /\ [][Next]_vars

FairSpec == Spec /\ WF_vars(Tick)

\* ---- the rules ----

\* No attempt is ever dealt while BASE is red on the card's platforms or its brief fails
\* against BASE.
NeverDealtOnRed == ~dealtOn

\* A red that turns green deals the card within one tick: at a tick's end a ready card
\* with room, its BASE green and its brief clear, is no longer ready.
GreenDealsInOneTick ==
    phase = "ticked" => ~(col = "ready" /\ ~red /\ ~defect /\ spent < MaxAttempts)

\* A card held ready is never held silently: at a tick's end it carries the judgment
\* naming why, and a judgment stands only while its cause does.
HeldIsJudged ==
    phase = "ticked" /\ col = "ready" /\ spent < MaxAttempts =>
        /\ defect => judged = "defect"
        /\ (~defect /\ red) => judged = "basered"
        /\ (~defect /\ ~red) => judged = "none"

\* An attempt that failed on a red inherited from BASE counts toward no bound and no
\* first-time stat: the attempts counted are those dealt less those inherited.
InheritedNotCounted == counted + inherited = spent

\* The liveness form: a ready card whose BASE stays green and whose brief stays clear is
\* dealt (under fairness of the tick).
GreenIsDealt ==
    [](col = "ready" /\ ~red /\ ~defect /\ spent < MaxAttempts
        => <>(col # "ready" \/ red \/ defect))

=============================================================================
