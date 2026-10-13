---------------------------- MODULE NeedsBlocked ----------------------------
(***************************************************************************)
(* A need is resolved or not (v1.2.17, a-missing-or-blocked-need-is-found- *)
(* at-check-and-while-running; the owner, 2026-10-13: "This needs to stop  *)
(* happening. A missing card stalls the sprint."). Epoch 20: a card needed *)
(* a-route-may-be-frontier, which hit its attempt bound overnight under an *)
(* open bound judgment no one decided; the card and its stream waited 8+  *)
(* hours, and every check passed, since the need was on the table.        *)
(*                                                                         *)
(* A need is RESOLVED when the card it names is landed, or placed and not  *)
(* held (internal/sprint/needs_resolve.go UnresolvedNeeds): a held card    *)
(* (an open bound, brief-wrong or harness-fault judgment names it) is not  *)
(* landing until someone decides. Two cards: b needs a. state is a card's  *)
(* place as add and the tick see it; admitted is whether add took it in;   *)
(* machine is the start gate's machine, READY or RUNNING.                  *)
(*                                                                         *)
(* Admit(c) is add (and every brief import, all or nothing): its needs     *)
(* must be resolved. Hold(c) is the outside event, a card reaching its     *)
(* bound with no decision; Decide(c) is a rework that closes it; Drop and  *)
(* Land as the table moves them. Start refuses while an admitted card not  *)
(* landed or dropped has an unresolved need, computed live (run.go         *)
(* startNeedsGate), so a hold raised after a clean check is caught.        *)
(* Absent (no card of this epoch), dropped and an unknown stream are       *)
(* unresolved as held is; held is the reason no check saw, and the one     *)
(* modelled here.                                                          *)
(*                                                                         *)
(* Broken selects a reversed witness:                                      *)
(*   "admitheld"  Admit treats a held need as placed, as add did before    *)
(*                this card (AdmittedNeedsResolved)                        *)
(*                                                                         *)
(* Room is left for the run-time card                                      *)
(* (a-blocked-need-is-one-owner-judgment-and-a-drop-never-strands): it     *)
(* adds its variables (the one owner judgment, the drop's cascade or       *)
(* relink) and its invariant beside these, without changing Admit's rule. *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
    Broken      \* "none", or the reversed witness above

Cards == {"a", "b"}
States == {"absent", "placed", "held", "landed", "dropped"}

\* b needs a
NeedOf(c) == IF c = "b" THEN {"a"} ELSE {}

VARIABLES
    state,      \* each card's place: absent (no record), placed, held, landed, dropped
    admitted,   \* add took the card in
    machine,    \* the machine as start sees it: ready (READY or STOPPED) or running
    last,       \* the last action
    lastCard    \* the card the last action moved

vars == <<state, admitted, machine, last, lastCard>>

\* the rule (UnresolvedNeeds): landed, or placed and not held; the witness reads held as placed
Resolved(n) ==
    \/ state[n] \in {"placed", "landed"}
    \/ Broken = "admitheld" /\ state[n] = "held"

\* the truth the rule must agree with
TrulyResolved(n) == state[n] \in {"placed", "landed"}

\* an admitted card still to land, with a need that is not resolved (TableUnresolved)
Unresolved == {c \in Cards : admitted[c] /\ state[c] \notin {"landed", "dropped"}
                             /\ \E n \in NeedOf(c) : ~TrulyResolved(n)}

TypeOK ==
    /\ state \in [Cards -> States]
    /\ admitted \in [Cards -> BOOLEAN]
    /\ machine \in {"ready", "running"}
    /\ last \in {"init", "admit", "hold", "decide", "drop", "land", "start", "stop", "startrefused"}
    /\ lastCard \in Cards \cup {"none"}

Init ==
    /\ state = [c \in Cards |-> "absent"]
    /\ admitted = [c \in Cards |-> FALSE]
    /\ machine = "ready"
    /\ last = "init"
    /\ lastCard = "none"

\* add (steps_work.go Add's needs arm, UnresolvedNeeds): every need resolved, else refused
\* and nothing written; an import of many cards is all or nothing (refuseAll)
Admit(c) ==
    /\ state[c] = "absent"
    /\ \A n \in NeedOf(c) : Resolved(n)
    /\ state' = [state EXCEPT ![c] = "placed"]
    /\ admitted' = [admitted EXCEPT ![c] = TRUE]
    /\ last' = "admit"
    /\ lastCard' = c
    /\ UNCHANGED machine

\* outside event: the card reaches its bound and an open bound judgment holds it
Hold(c) ==
    /\ state[c] = "placed"
    /\ state' = [state EXCEPT ![c] = "held"]
    /\ last' = "hold"
    /\ lastCard' = c
    /\ UNCHANGED <<admitted, machine>>

\* the coordinator decides: a rework closes the judgment and the card is placed again
Decide(c) ==
    /\ state[c] = "held"
    /\ state' = [state EXCEPT ![c] = "placed"]
    /\ last' = "decide"
    /\ lastCard' = c
    /\ UNCHANGED <<admitted, machine>>

Drop(c) ==
    /\ state[c] \in {"placed", "held"}
    /\ state' = [state EXCEPT ![c] = "dropped"]
    /\ last' = "drop"
    /\ lastCard' = c
    /\ UNCHANGED <<admitted, machine>>

\* a card lands once its needs landed (NeedsLandFirst, SprintTables.tla)
Land(c) ==
    /\ state[c] = "placed"
    /\ \A n \in NeedOf(c) : state[n] = "landed"
    /\ state' = [state EXCEPT ![c] = "landed"]
    /\ last' = "land"
    /\ lastCard' = c
    /\ UNCHANGED <<admitted, machine>>

\* start (run.go startNeedsGate, before the record gate): live, not from the record
Start ==
    /\ machine = "ready"
    /\ Unresolved = {}
    /\ machine' = "running"
    /\ last' = "start"
    /\ lastCard' = "none"
    /\ UNCHANGED <<state, admitted>>

StartRefused ==
    /\ machine = "ready"
    /\ Unresolved # {}
    /\ last' = "startrefused"
    /\ lastCard' = "none"
    /\ UNCHANGED <<state, admitted, machine>>

Stop ==
    /\ machine = "running"
    /\ machine' = "ready"
    /\ last' = "stop"
    /\ lastCard' = "none"
    /\ UNCHANGED <<state, admitted>>

Next ==
    \/ \E c \in Cards : Admit(c) \/ Hold(c) \/ Decide(c) \/ Drop(c) \/ Land(c)
    \/ Start \/ StartRefused \/ Stop

Spec == Init /\ [][Next]_vars

---------------------------------------------------------------------------
\* Invariants.

\* at the step that admits a card, every need it names is landed, or placed and not held
AdmittedNeedsResolved == last = "admit" => \A n \in NeedOf(lastCard) : TrulyResolved(n)

\* the machine never starts from ready while an admitted card has an unresolved need
StartOnlyResolved == last = "start" => Unresolved = {}

=============================================================================
