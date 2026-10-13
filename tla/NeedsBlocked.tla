---------------------------- MODULE NeedsBlocked ----------------------------
(***************************************************************************)
(* A held, dropped or absent need is one blocker judgment, pushed to the   *)
(* owner at once, that quiets its dependents; no drop or defer strands a   *)
(* dependent (v1.2.17 blocker; docs/SPEC-SPRINT.md sections 8 and 11).     *)
(*                                                                         *)
(* UnresolvedNeeds (internal/sprint/needs_resolve.go): a need is resolved  *)
(* when it is landed, or placed and not held.                             *)
(*                                                                         *)
(* Three cards: a, b, c. Initially c needs b, and b needs a.               *)
(* Unresolved(x) reports a card held (bound, brief-wrong) or dropped.      *)
(* OpenDeps(x) is every unlanded admitted card reaching x through needs.   *)
(* A blocker is any card with Unresolved(x) and OpenDeps(x) # {}.          *)
(* bj is the set of blockers with an open NNeedBlocked judgment.           *)
(*                                                                         *)
(* Broken witness selection:                                               *)
(*   "none"       standard model (MCNeedsBlocked, MCNeedsBlockedRun)       *)
(*   "admitheld"  Admit treats a held need as placed                       *)
(*                (violates AdmittedNeedsResolved)                         *)
(*   "nothing"    Tick raises nothing on a hold, as BASE                   *)
(*                (violates BlockedDependentsCovered)                      *)
(*   "defer"      Defer ignores dependents, as BASE                        *)
(*                (violates NoStrandingDrop)                               *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
    Broken      \* "none", "admitheld", "nothing", "defer"

Cards == {"a", "b", "c"}
States == {"absent", "placed", "held", "landed", "dropped"}

VARIABLES
    state,      \* [Cards -> States]
    needs,      \* [Cards -> SUBSET Cards]
    admitted,   \* [Cards -> BOOLEAN]
    machine,    \* "ready" or "running"
    last,       \* last action name
    lastCard,   \* last card moved, or "none"
    bj,         \* SUBSET Cards: blockers with an open NNeedBlocked
    ticked,     \* BOOLEAN: whether Tick has run
    decided     \* [Cards -> BOOLEAN]: whether a rework was decided

vars == <<state, needs, admitted, machine, last, lastCard, bj, ticked, decided>>

\* The rule (UnresolvedNeeds): landed, or placed and not held; witness reads held as placed
Resolved(n) ==
    \/ state[n] \in {"placed", "landed"}
    \/ Broken = "admitheld" /\ state[n] = "held"

TrulyResolved(n) == state[n] \in {"placed", "landed"}

Unresolved(x) ==
    \/ state[x] = "held"
    \/ state[x] = "dropped"

Reaches(d, x) ==
    \/ x \in needs[d]
    \/ \E mid \in Cards : mid \in needs[d] /\ x \in needs[mid]

OpenDeps(x) ==
    { d \in Cards : admitted[d] /\ state[d] \notin {"landed", "dropped"} /\ Reaches(d, x) }

UnresolvedAdmitted ==
    { c \in Cards : admitted[c] /\ state[c] \notin {"landed", "dropped"}
                    /\ \E n \in needs[c] : ~TrulyResolved(n) }

TypeOK ==
    /\ state \in [Cards -> States]
    /\ needs \in [Cards -> SUBSET Cards]
    /\ admitted \in [Cards -> BOOLEAN]
    /\ machine \in {"ready", "running"}
    /\ last \in {"init", "admit", "hold", "decide", "drop", "land", "start", "stop", "startrefused", "tick", "defer"}
    /\ lastCard \in Cards \cup {"none"}
    /\ bj \in SUBSET Cards
    /\ ticked \in BOOLEAN
    /\ decided \in [Cards -> BOOLEAN]

Init ==
    /\ state = [c \in Cards |-> "absent"]
    /\ needs = [c \in Cards |-> IF c = "c" THEN {"b"} ELSE IF c = "b" THEN {"a"} ELSE {}]
    /\ admitted = [c \in Cards |-> FALSE]
    /\ machine = "ready"
    /\ last = "init"
    /\ lastCard = "none"
    /\ bj = {}
    /\ ticked = FALSE
    /\ decided = [c \in Cards |-> FALSE]

Admit(c) ==
    /\ state[c] = "absent"
    /\ \A n \in needs[c] : Resolved(n)
    /\ state' = [state EXCEPT ![c] = "placed"]
    /\ admitted' = [admitted EXCEPT ![c] = TRUE]
    /\ last' = "admit"
    /\ lastCard' = c
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, machine, bj, decided>>

Hold(c) ==
    /\ state[c] = "placed"
    /\ state' = [state EXCEPT ![c] = "held"]
    /\ last' = "hold"
    /\ lastCard' = c
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, admitted, machine, bj, decided>>

Decide(c) ==
    /\ state[c] = "held"
    /\ state' = [state EXCEPT ![c] = "placed"]
    /\ decided' = [decided EXCEPT ![c] = TRUE]
    /\ last' = "decide"
    /\ lastCard' = c
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, admitted, machine, bj>>

Land(c) ==
    /\ state[c] = "placed"
    /\ \A n \in needs[c] : state[n] = "landed"
    /\ state' = [state EXCEPT ![c] = "landed"]
    /\ last' = "land"
    /\ lastCard' = c
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, admitted, machine, bj, decided>>

DropCascade(x) ==
    /\ state[x] \in {"placed", "held"}
    /\ LET toDrop == {x} \cup OpenDeps(x)
       IN /\ state' = [c \in Cards |-> IF c \in toDrop THEN "dropped" ELSE state[c]]
          /\ last' = "drop"
          /\ lastCard' = x
          /\ ticked' = FALSE
          /\ UNCHANGED <<needs, admitted, machine, bj, decided>>

DropWaive(x) ==
    /\ state[x] \in {"placed", "held"}
    /\ state' = [state EXCEPT ![x] = "dropped"]
    /\ needs' = [c \in Cards |-> needs[c] \ {x}]
    /\ last' = "drop"
    /\ lastCard' = x
    /\ ticked' = FALSE
    /\ UNCHANGED <<admitted, machine, bj, decided>>

DropRelink(x, target) ==
    /\ state[x] \in {"placed", "held"}
    /\ target \in Cards
    /\ target # x
    /\ state[target] \in {"placed", "landed"}
    /\ ~Unresolved(target)
    /\ state' = [state EXCEPT ![x] = "dropped"]
    /\ needs' = [c \in Cards |-> IF x \in needs[c] THEN (needs[c] \ {x}) \cup {target} ELSE needs[c]]
    /\ last' = "drop"
    /\ lastCard' = x
    /\ ticked' = FALSE
    /\ UNCHANGED <<admitted, machine, bj, decided>>

DropNoDeps(x) ==
    /\ state[x] \in {"placed", "held"}
    /\ OpenDeps(x) = {}
    /\ state' = [state EXCEPT ![x] = "dropped"]
    /\ last' = "drop"
    /\ lastCard' = x
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, admitted, machine, bj, decided>>

Defer(x) ==
    /\ state[x] = "placed"
    /\ IF Broken = "defer"
       THEN TRUE
       ELSE OpenDeps(x) = {}
    /\ state' = [state EXCEPT ![x] = "dropped"]
    /\ last' = "defer"
    /\ lastCard' = x
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, admitted, machine, bj, decided>>

Tick ==
    /\ bj' = IF Broken = "nothing"
             THEN { x \in Cards : state[x] = "dropped" /\ OpenDeps(x) # {} }
             ELSE { x \in Cards : Unresolved(x) /\ OpenDeps(x) # {} }
    /\ ticked' = TRUE
    /\ decided' = [c \in Cards |-> FALSE]
    /\ last' = "tick"
    /\ lastCard' = "none"
    /\ UNCHANGED <<state, needs, admitted, machine>>

Start ==
    /\ machine = "ready"
    /\ UnresolvedAdmitted = {}
    /\ machine' = "running"
    /\ last' = "start"
    /\ lastCard' = "none"
    /\ ticked' = FALSE
    /\ UNCHANGED <<state, needs, admitted, bj, decided>>

StartRefused ==
    /\ machine = "ready"
    /\ UnresolvedAdmitted # {}
    /\ last' = "startrefused"
    /\ lastCard' = "none"
    /\ UNCHANGED <<state, needs, admitted, machine, bj, ticked, decided>>

Stop ==
    /\ machine = "running"
    /\ machine' = "ready"
    /\ last' = "stop"
    /\ lastCard' = "none"
    /\ UNCHANGED <<state, needs, admitted, bj, ticked, decided>>

Next ==
    \/ \E c \in Cards : Admit(c) \/ Hold(c) \/ Decide(c) \/ Land(c) \/ Defer(c)
    \/ \E c \in Cards : DropCascade(c) \/ DropWaive(c) \/ DropNoDeps(c)
    \/ \E c, target \in Cards : DropRelink(c, target)
    \/ Start \/ StartRefused \/ Stop \/ Tick

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

---------------------------------------------------------------------------
\* Invariants from first card (check and start).

AdmittedNeedsResolved == last = "admit" => \A n \in needs[lastCard] : TrulyResolved(n)

StartOnlyResolved == last = "start" => UnresolvedAdmitted = {}

---------------------------------------------------------------------------
\* Invariants from second card (runtime blocker judgments and non-stranding drop/defer).

BlockedDependentsCovered ==
    ticked => \A x \in Cards : (Unresolved(x) /\ OpenDeps(x) # {}) => x \in bj

OneBlockerJudgment ==
    ticked => \A x \in bj : Unresolved(x) /\ OpenDeps(x) # {}

NoStrandingDrop ==
    \A x \in Cards : state[x] = "dropped" =>
        \A y \in Cards : state[y] \notin {"landed", "dropped"} => x \notin needs[y]

LiveDecided ==
    \A x \in Cards : decided[x] ~> (x \notin bj)

=============================================================================
