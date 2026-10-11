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
(* held: "none", "fault" (the machine's), or "person" (a person's).      *)
(* beat: the member's machine beating ("up") or not ("down"). ok: a      *)
(* clean finish since the hold was placed, reset by any new hold, so a   *)
(* clean finish before the hold is no proof of health.                   *)
(*                                                                       *)
(* Broken (the reversed witness):                                        *)
(*  "noautoclear"  - the tick never clears its own fault hold, however   *)
(*                   healthy the member: FaultHoldClears fails.          *)
(*  "clearsperson" - the tick clears a person's hold too:                *)
(*                   PersonHoldStays fails.                              *)
(***************************************************************************)
EXTENDS Naturals

CONSTANT Broken

VARIABLES held, beat, ok

vars == <<held, beat, ok>>

TypeOK ==
    /\ held \in {"none", "fault", "person"}
    /\ beat \in {"down", "up"}
    /\ ok \in {"no", "yes"}

Init == /\ held = "none" /\ beat = "down" /\ ok = "no"

\* the machine holds the member for a failure; any clean finish before it
\* is no proof of health, so ok resets
FaultHold ==
    /\ held = "none"
    /\ held' = "fault"
    /\ ok' = "no"
    /\ UNCHANGED <<beat>>

\* a person holds the member; the hold is now the person's, and it stays until
\* a person lifts it (not modelled). It takes an un-held member only, so the
\* liveness below is not preempted by a person taking over a machine hold.
PersonHold ==
    /\ held = "none"
    /\ held' = "person"
    /\ ok' = "no"
    /\ UNCHANGED <<beat>>

\* the member's machine beats again
Beat ==
    /\ beat = "down"
    /\ beat' = "up"
    /\ UNCHANGED <<held, ok>>

\* the member takes a card and finishes it cleanly: it must be up (the tick
\* released its down for the probe) and this finish is after the hold
FinishClean ==
    /\ beat = "up"
    /\ ok = "no"
    /\ ok' = "yes"
    /\ UNCHANGED <<held, beat>>

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
    /\ ok' = "no"
    /\ UNCHANGED <<beat>>

Next == \/ FaultHold \/ PersonHold \/ Beat \/ FinishClean \/ Clear

Spec == Init /\ [][Next]_vars /\ WF_vars(Clear)

(* Liveness: a machine fault hold clears once the member beats and finishes *)
(* a card cleanly.                                                         *)
FaultHoldClears ==
    (held = "fault" /\ beat = "up" /\ ok = "yes") ~> (held = "none")

(* Safety: a person's hold is never cleared by the machine.               *)
PersonHoldStays ==
    [][held = "person" => held' = "person"]_vars
=============================================================================
