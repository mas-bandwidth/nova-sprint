---------------------------- MODULE DealFill ----------------------------
(***************************************************************************)
(* nova-sprint's fault hold, and the tick's clear of it                   *)
(* (internal/sprint/steps_tick.go faultClear, hold.go HeldByFault;       *)
(* docs/SPEC-SPRINT.md section 5, the fleet, and section 11, hold).      *)
(*                                                                       *)
(* A member the machine holds for a failure (a harness fault, a failed   *)
(* take, or the member going down) is held down but still beats; the     *)
(* tick releases it to deal it a card (a probe card if nothing else is   *)
(* dealt), and once the member has taken a card and finished it cleanly  *)
(* the tick clears the machine's own hold and tells the seat. A hold a   *)
(* person made (fleet down, no held_by mark) is never cleared by the     *)
(* machine: only a person lifts it.                                      *)
(*                                                                       *)
(* held: who holds the member, "none", "fault" (the machine's), or      *)
(* "person" (a person's) -- the control card's held_by. down: the        *)
(* member's held-down state, the control card's held, which the probe    *)
(* clears while the mark (held) stays (faultClear releases the down to   *)
(* deal the card, keeping held_by). beat: the member's machine beating   *)
(* ("up") or not ("down"). ok: a clean finish since the hold was placed, *)
(* reset by any new hold, so a clean finish before the hold is no proof  *)
(* of health.                                                            *)
(*                                                                       *)
(* Broken (the reversed witness):                                        *)
(*  "noautoclear"  - the tick never clears its own fault hold, however   *)
(*                   healthy the member: FaultHoldClears fails.          *)
(*  "clearsperson" - the tick clears a person's hold too:                *)
(*                   PersonHoldStays fails.                              *)
(***************************************************************************)
EXTENDS Naturals

CONSTANT Broken

VARIABLES held, down, beat, ok

vars == <<held, down, beat, ok>>

TypeOK ==
    /\ held \in {"none", "fault", "person"}
    /\ down \in {"no", "yes"}
    /\ beat \in {"down", "up"}
    /\ ok \in {"no", "yes"}

Init == /\ held = "none" /\ down = "no" /\ beat = "down" /\ ok = "no"

\* the machine holds the member for a failure; any clean finish before it
\* is no proof of health, so ok resets, and the member is held down
FaultHold ==
    /\ held = "none"
    /\ held' = "fault"
    /\ down' = "yes"
    /\ ok' = "no"
    /\ UNCHANGED <<beat>>

\* a person holds the member, or takes over a machine hold; the hold is now
\* the person's, and it stays until a person lifts it (not modelled)
PersonHold ==
    /\ held # "person"
    /\ held' = "person"
    /\ down' = "yes"
    /\ ok' = "no"
    /\ UNCHANGED <<beat>>

\* the member's machine beats again
Beat ==
    /\ beat = "down"
    /\ beat' = "up"
    /\ UNCHANGED <<held, down, ok>>

\* the tick releases a beating fault-held member's down so the deal gives it
\* a card (the probe): the mark (held) is kept, the down is cleared
Probe ==
    /\ held = "fault"
    /\ beat = "up"
    /\ ok = "no"
    /\ down = "yes"
    /\ down' = "no"
    /\ UNCHANGED <<held, beat, ok>>

\* the member takes a card and finishes it cleanly: it must be up (its down
\* released for the probe) and this finish is after the hold
FinishClean ==
    /\ beat = "up"
    /\ down = "no"
    /\ ok = "no"
    /\ ok' = "yes"
    /\ UNCHANGED <<held, down, beat>>

\* the tick clears the machine's own fault hold once the member is healthy,
\* and only its own: a person's hold is never lifted by this action
Clear ==
    /\ Broken # "noautoclear"
    /\ beat = "up"
    /\ ok = "yes"
    /\ IF Broken = "clearsperson"
         THEN /\ held # "none"        \* the bug: it clears a person's hold too
         ELSE /\ held = "fault"
    /\ held' = "none"
    /\ down' = "no"
    /\ ok' = "no"
    /\ UNCHANGED <<beat>>

Next == \/ FaultHold \/ PersonHold \/ Beat \/ Probe \/ FinishClean \/ Clear

Spec == Init /\ [][Next]_vars /\ WF_vars(Clear)

(* Liveness: a machine fault hold clears once the member beats and finishes *)
(* a card cleanly. A person may take the hold over first (held becomes      *)
(* "person"), which also leaves the fault mark; the fault mark never stays  *)
(* once the member is healthy.                                             *)
FaultHoldClears ==
    (held = "fault" /\ beat = "up" /\ ok = "yes") ~> (held = "none" \/ held = "person")

(* Safety: a person's hold is never cleared by the machine.               *)
PersonHoldStays ==
    [][held = "person" => held' = "person"]_vars
=============================================================================
