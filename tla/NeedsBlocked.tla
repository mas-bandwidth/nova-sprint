---------------------------- MODULE NeedsBlocked ----------------------------
(***************************************************************************)
(* A held, dropped or absent need is one blocker judgment, pushed to the   *)
(* owner at once, that quiets its dependents; no drop or defer strands a   *)
(* dependent (v1.2.17 blocker; docs/SPEC-SPRINT.md sections 8 and 11).     *)
(*                                                                         *)
(* Three cards: a, b, c. Initially c needs b, and b needs a.               *)
(* Unresolved(x) reports a card held (bound, brief-wrong) or dropped.      *)
(* OpenDeps(x) is every unlanded card reaching x through needs.            *)
(* A blocker is any card with Unresolved(x) and OpenDeps(x) # {}.          *)
(* bj is the set of blockers with an open NNeedBlocked judgment.           *)
(*                                                                         *)
(* Broken witness selection:                                               *)
(*   "none"     standard model (MCNeedsBlockedRun)                         *)
(*   "nothing"  Tick raises nothing on a hold, as BASE                     *)
(*              (violates BlockedDependentsCovered)                        *)
(*   "defer"    Defer ignores dependents, as BASE                          *)
(*              (violates NoStrandingDrop)                                 *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
    Cards,      \* {a, b, c}
    Broken      \* "none", "nothing", "defer"

VARIABLES
    state,      \* [Cards -> {"ready", "working", "waiting", "landed", "dropped"}]
    needs,      \* [Cards -> SUBSET Cards]
    held,       \* [Cards -> BOOLEAN]
    bj,         \* SUBSET Cards
    ticked,     \* BOOLEAN
    decided     \* [Cards -> BOOLEAN]

vars == <<state, needs, held, bj, ticked, decided>>

Unresolved(x) ==
    \/ held[x]
    \/ state[x] = "dropped"

Reaches(d, x) ==
    \/ x \in needs[d]
    \/ \E mid \in Cards : mid \in needs[d] /\ x \in needs[mid]

OpenDeps(x) ==
    { d \in Cards : state[d] \notin {"landed", "dropped"} /\ Reaches(d, x) }

Init ==
    /\ state = [c \in Cards |-> IF c = "a" THEN "working" ELSE "waiting"]
    /\ needs = [c \in Cards |-> IF c = "c" THEN {"b"} ELSE IF c = "b" THEN {"a"} ELSE {}]
    /\ held = [c \in Cards |-> FALSE]
    /\ bj = {}
    /\ ticked = FALSE
    /\ decided = [c \in Cards |-> FALSE]

TypeOK ==
    /\ state \in [Cards -> {"ready", "working", "waiting", "landed", "dropped"}]
    /\ needs \in [Cards -> SUBSET Cards]
    /\ held \in [Cards -> BOOLEAN]
    /\ bj \in SUBSET Cards
    /\ ticked \in BOOLEAN
    /\ decided \in [Cards -> BOOLEAN]

Hold(x) ==
    /\ state[x] \in {"ready", "working", "waiting"}
    /\ ~held[x]
    /\ x \notin bj
    /\ ~decided[x]
    /\ held' = [held EXCEPT ![x] = TRUE]
    /\ ticked' = FALSE
    /\ UNCHANGED <<state, needs, bj, decided>>


Decide(x) ==
    /\ held[x]
    /\ held' = [held EXCEPT ![x] = FALSE]
    /\ decided' = [decided EXCEPT ![x] = TRUE]
    /\ ticked' = FALSE
    /\ UNCHANGED <<state, needs, bj>>

MoveReady(x) ==
    /\ state[x] = "waiting"
    /\ ~held[x]
    /\ \A n \in needs[x] : state[n] = "landed"
    /\ state' = [state EXCEPT ![x] = "ready"]
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, held, bj, decided>>

MoveWorking(x) ==
    /\ state[x] = "ready"
    /\ ~held[x]
    /\ state' = [state EXCEPT ![x] = "working"]
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, held, bj, decided>>

Land(x) ==
    /\ state[x] = "working"
    /\ ~held[x]
    /\ \A n \in needs[x] : state[n] = "landed"
    /\ state' = [state EXCEPT ![x] = "landed"]
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, held, bj, decided>>

DropCascade(x) ==
    /\ state[x] \notin {"landed", "dropped"}
    /\ LET toDrop == {x} \cup OpenDeps(x)
       IN /\ state' = [c \in Cards |-> IF c \in toDrop THEN "dropped" ELSE state[c]]
          /\ held' = [c \in Cards |-> IF c \in toDrop THEN FALSE ELSE held[c]]
          /\ ticked' = FALSE
          /\ UNCHANGED <<needs, bj, decided>>

DropWaive(x) ==
    /\ state[x] \notin {"landed", "dropped"}
    /\ state' = [state EXCEPT ![x] = "dropped"]
    /\ held' = [held EXCEPT ![x] = FALSE]
    /\ needs' = [c \in Cards |-> needs[c] \ {x}]
    /\ ticked' = FALSE
    /\ UNCHANGED <<bj, decided>>

DropRelink(x, target) ==
    /\ state[x] \notin {"landed", "dropped"}
    /\ target \in Cards
    /\ target # x
    /\ state[target] \notin {"landed", "dropped"}
    /\ ~Unresolved(target)
    /\ state' = [state EXCEPT ![x] = "dropped"]
    /\ held' = [held EXCEPT ![x] = FALSE]
    /\ needs' = [c \in Cards |-> IF x \in needs[c] THEN (needs[c] \ {x}) \cup {target} ELSE needs[c]]
    /\ ticked' = FALSE
    /\ UNCHANGED <<bj, decided>>

DropNoDeps(x) ==
    /\ state[x] \notin {"landed", "dropped"}
    /\ OpenDeps(x) = {}
    /\ state' = [state EXCEPT ![x] = "dropped"]
    /\ held' = [held EXCEPT ![x] = FALSE]
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, bj, decided>>

Defer(x) ==
    /\ state[x] = "waiting"
    /\ IF Broken = "defer"
       THEN TRUE
       ELSE OpenDeps(x) = {}
    /\ state' = [state EXCEPT ![x] = "dropped"]
    /\ held' = [held EXCEPT ![x] = FALSE]
    /\ ticked' = FALSE
    /\ UNCHANGED <<needs, bj, decided>>

Tick ==
    /\ bj' = IF Broken = "nothing"
             THEN { x \in Cards : state[x] = "dropped" /\ OpenDeps(x) # {} }
             ELSE { x \in Cards : Unresolved(x) /\ OpenDeps(x) # {} }
    /\ ticked' = TRUE
    /\ decided' = [c \in Cards |-> FALSE]
    /\ UNCHANGED <<state, needs, held>>

Next ==
    \/ \E x \in Cards : Hold(x)
    \/ \E x \in Cards : Decide(x)
    \/ \E x \in Cards : Land(x)
    \/ \E x \in Cards : MoveReady(x)
    \/ \E x \in Cards : MoveWorking(x)
    \/ \E x \in Cards : DropCascade(x)
    \/ \E x \in Cards : DropWaive(x)
    \/ \E x, target \in Cards : DropRelink(x, target)
    \/ \E x \in Cards : DropNoDeps(x)
    \/ \E x \in Cards : Defer(x)
    \/ Tick

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

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
