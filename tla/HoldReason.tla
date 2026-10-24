---------------------------- MODULE HoldReason ----------------------------
(***************************************************************************)
(* A coordinator's hold with a reason stays held until it is unheld       *)
(* (internal/sprint/hold_cause.go HeldWithReason, holdCauseClear,         *)
(* holdNoCauseConds; fleet_sync.go FleetDrift; steps_work.go downPlan;    *)
(* stops.go memberStops). 2026-10-24: space, studio, hulk and captain,    *)
(* machines that never run a member loop by design, flipped held -> down  *)
(* every few minutes: the fleet sync held them again (status down, the    *)
(* coordinator's reason wiped), a hold whose cause was its own no-cause   *)
(* judgment was released down when that judgment closed, and each flip   *)
(* raised "a fleet member is down and the coordinator did not hold it"    *)
(* and "a member is held with no cause".                                  *)
(*                                                                       *)
(* One member, which may beat or not. HoldCause.tla models the cause's    *)
(* end abstractly; this module adds the reason and the beat to it.        *)
(*   hold     "none"; "reason", the coordinator's hold with a reason;     *)
(*            "bare", the coordinator's hold with no reason.              *)
(*   status   "held", "down" or "up", as presence and the sync write it.  *)
(*   beats    the member beats (its member loop runs).                    *)
(*   ended    the hold's cause judgment was answered or retired.          *)
(*   noCause  the tick's "held with no cause" judgment is open.           *)
(*   downJ    the pass's "down and the coordinator did not hold it".      *)
(*   how      how the last hold ended: "none", "unhold", "beat" (its      *)
(*            cause ended while the member beats: released up, the rule   *)
(*            of 2026-10-13), "cause" (a hold with no reason, its cause   *)
(*            ended), or "other" (a reasoned hold ended otherwise: the    *)
(*            flip).                                                      *)
(*   ticked   the last step was the tick: the judgment rules are read at  *)
(*            its end, where the code raises and closes them.             *)
(*                                                                       *)
(* Broken (the reversed witnesses):                                       *)
(*  "syncrehold"     the sync holds a held member again, down, its reason *)
(*                   wiped: SyncKeepsHold fails.                          *)
(*  "reasonreported" a hold with a reason is reported as cause-less:      *)
(*                   ReasonIsCause fails.                                 *)
(*  "releasedown"    the cause's end releases a member that does not beat *)
(*                   (before 2026-10-24): ReasonedHoldEndsByUnhold fails.  *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS Broken

VARIABLES hold, status, beats, ended, noCause, downJ, how, ticked

vars == <<hold, status, beats, ended, noCause, downJ, how, ticked>>

TypeOK ==
    /\ hold \in {"none", "reason", "bare"}
    /\ status \in {"held", "down", "up"}
    /\ beats \in BOOLEAN
    /\ ended \in BOOLEAN
    /\ noCause \in BOOLEAN
    /\ downJ \in BOOLEAN
    /\ how \in {"none", "unhold", "beat", "cause", "other"}
    /\ ticked \in BOOLEAN

Init ==
    /\ hold = "none" /\ status = "down" /\ beats \in BOOLEAN
    /\ ended = FALSE /\ noCause = FALSE /\ downJ = FALSE /\ how = "none" /\ ticked = FALSE

\* the coordinator's fleet hold, with or without a reason; its cause judgment open
Hold(r) ==
    /\ hold = "none"
    /\ hold' = r /\ status' = "held" /\ ended' = FALSE /\ how' = "none"
    /\ UNCHANGED <<beats, noCause, downJ>> /\ ticked' = FALSE

Unhold ==
    /\ hold # "none"
    /\ hold' = "none" /\ how' = "unhold"
    /\ status' = IF beats THEN "up" ELSE "down"
    /\ UNCHANGED <<beats, ended, noCause, downJ>> /\ ticked' = FALSE

\* the member's loop starts or stops; the cause judgment is answered
Beat == beats' = ~beats /\ UNCHANGED <<hold, status, ended, noCause, downJ, how>> /\ ticked' = FALSE
CauseEnds == hold # "none" /\ ~ended /\ ended' = TRUE /\ UNCHANGED <<hold, status, beats, noCause, downJ, how>> /\ ticked' = FALSE

\* the fleet sync, for a member the inventory does not want: it holds a member not
\* held; a held member is held already and is left (FleetDrift)
Sync ==
    /\ IF hold = "none"
         THEN status' = "down" /\ UNCHANGED hold
         ELSE IF Broken = "syncrehold"
           THEN status' = "down" /\ hold' = "bare"  \* status down, the reason wiped
           ELSE UNCHANGED <<status, hold>>
    /\ UNCHANGED <<beats, ended, noCause, downJ, how>> /\ ticked' = FALSE

\* the tick: the cause's end releases only a member that beats (or a hold with no reason);
\* the no-cause report is of a hold with no reason; the down stop is of a member not held
Tick ==
    /\ LET release == hold # "none" /\ ended /\ (beats \/ hold = "bare" \/ Broken = "releasedown")
       IN  /\ hold' = IF release THEN "none" ELSE hold
           /\ status' = IF release THEN (IF beats THEN "up" ELSE "down")
                        ELSE IF hold # "none" THEN "held"
                        ELSE IF beats THEN "up" ELSE "down"
           /\ how' = IF ~release THEN how
                     ELSE IF beats THEN "beat"
                     ELSE IF hold = "bare" THEN "cause"
                     ELSE "other"
           /\ noCause' = (hold' = "bare" \/ (Broken = "reasonreported" /\ hold' = "reason"))
           /\ downJ' = (hold' = "none" /\ status' = "down")
    /\ UNCHANGED <<beats, ended>> /\ ticked' = TRUE

Next == Hold("reason") \/ Hold("bare") \/ Unhold \/ Beat \/ CauseEnds \/ Sync \/ Tick

Spec == Init /\ [][Next]_vars

\* ---- the rules ----

\* A held member stays held through the sync: never set down, its reason kept.
SyncKeepsHold == hold # "none" => status = "held"

\* A hold's reason counts as its cause: never reported as a hold with no cause.
ReasonIsCause == ticked => (noCause => hold = "bare")

\* A hold ends by unhold, or by its cause's end while the member beats (released up):
\* never by anything else, so a member that never beats never flips held -> down.
ReasonedHoldEndsByUnhold == how # "other"

\* A held member raises no down judgment.
HeldRaisesNoDown == ticked => (downJ => hold = "none")

=============================================================================
