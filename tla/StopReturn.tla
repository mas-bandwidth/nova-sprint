---------------------------- MODULE StopReturn ----------------------------
(***************************************************************************)
(* nova-sprint STOP, stop-return and START (internal/sprint/stop_return.go, *)
(* internal/sprint/store/machine_transition.go, engine.go stopDebtMutation; *)
(* docs/SPEC-SPRINT.md section 14). The module the code has cited since    *)
(* the durable stop debt landed, written 2026-10-10 for v1.2.6 after a      *)
(* returned card pinned its owner's row: "STOP owns friend.alex:<card>@1"   *)
(* refused a coordinator hold after the stop-return had moved it to gen 2.  *)
(*                                                                         *)
(* A card is on an owner row, ready or working, at a generation. A working *)
(* card has the owner's child, alive or not (child[c] is the generation it *)
(* runs at, 0 for none). STOP captures each working card as a debt record  *)
(* [id, row, gen]. The owner cancels its child, then stop-returns the card *)
(* (ready, same row, gen+1, stopped_from_gen = gen): the receipt. Settle   *)
(* takes each debt whose receipt is in place off the machine record, as    *)
(* the store's settleStopDebt does after every stop-return, under its own  *)
(* fence (a separate write, so TLC sees steps between the two). A debt     *)
(* pins its card: no step may move it (stopDebtMutation). Hold moves a     *)
(* ready card that no debt pins to another row at a new generation. START  *)
(* needs every remaining debt's receipt in place, then clears the debt.    *)
(*                                                                         *)
(* Broken (reversed witnesses):                                            *)
(*  "nosettle"   - the code before v1.2.6: the debt stays until START, so  *)
(*                 a returned card pins its row for the whole STOP         *)
(*                 (ReturnedFreesItsRow fails);                            *)
(*  "noreceipt"  - Settle takes a debt off without its receipt: START      *)
(*                 crosses a live child (NoLiveChildAcrossStart fails).    *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, Owners, MaxGen, Broken

VARIABLES mach, row, col, gen, sfg, child, debt, old

\* old: the cards whose child was alive at the last STOP and has not been
\* cancelled since (history, read by NoLiveChildAcrossStart alone)
vars == <<mach, row, col, gen, sfg, child, debt, old>>

Debt == [id: Cards, row: Owners, gen: 1..MaxGen]

TypeOK ==
    /\ mach \in {"run", "stop"}
    /\ row \in [Cards -> Owners]
    /\ col \in [Cards -> {"ready", "working"}]
    /\ gen \in [Cards -> 1..MaxGen]
    /\ sfg \in [Cards -> 0..MaxGen]
    /\ child \in [Cards -> 0..MaxGen]
    /\ debt \subseteq Debt
    /\ old \subseteq Cards

Init ==
    /\ mach = "run"
    /\ row \in [Cards -> Owners]
    /\ col = [c \in Cards |-> "ready"]
    /\ gen = [c \in Cards |-> 1]
    /\ sfg = [c \in Cards |-> 0]
    /\ child = [c \in Cards |-> 0]
    /\ debt = {}
    /\ old = {}

\* the owner's same-row, next-generation return receipt (stopDebtReturned)
Receipt(d) ==
    /\ row[d.id] = d.row
    /\ col[d.id] = "ready"
    /\ sfg[d.id] = d.gen
    /\ gen[d.id] = d.gen + 1

Pinned(c) == \E d \in debt : d.id = c

\* a take while RUNNING starts the owner's child at the card's generation
Take(c) ==
    /\ mach = "run"
    /\ col[c] = "ready"
    /\ child[c] = 0
    /\ col' = [col EXCEPT ![c] = "working"]
    /\ child' = [child EXCEPT ![c] = gen[c]]
    /\ UNCHANGED <<mach, row, gen, sfg, debt, old>>

\* a finish while RUNNING: the child ends, the card goes on at a new generation
Finish(c) ==
    /\ mach = "run"
    /\ col[c] = "working"
    /\ child[c] = gen[c]
    /\ gen[c] < MaxGen
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ child' = [child EXCEPT ![c] = 0]
    /\ UNCHANGED <<mach, row, sfg, debt, old>>

\* STOP captures every working card as owed (activeStopLeases)
Stop ==
    /\ mach = "run"
    /\ mach' = "stop"
    /\ debt' = {[id |-> c, row |-> row[c], gen |-> gen[c]] : c \in {x \in Cards : col[x] = "working"}}
    /\ old' = {c \in Cards : child[c] # 0}
    /\ UNCHANGED <<row, col, gen, sfg, child>>

\* the owner cancels its child (the store trusts its acknowledgement)
Cancel(c) ==
    /\ mach = "stop"
    /\ child[c] # 0
    /\ child' = [child EXCEPT ![c] = 0]
    /\ old' = old \ {c}
    /\ UNCHANGED <<mach, row, col, gen, sfg, debt>>

\* stop-return --as <row> <c>@<gen>, after the observed cancellation
Return(c) ==
    /\ mach = "stop"
    /\ child[c] = 0
    /\ col[c] = "working"
    /\ gen[c] < MaxGen
    /\ \E d \in debt : d.id = c /\ d.row = row[c] /\ d.gen = gen[c]
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ sfg' = [sfg EXCEPT ![c] = gen[c]]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ UNCHANGED <<mach, row, child, debt, old>>

\* settleStopDebt: each receipted debt leaves the machine record
Settle ==
    /\ Broken # "nosettle"
    /\ mach = "stop"
    /\ IF Broken = "noreceipt"
         THEN /\ debt # {}
              /\ \E d \in debt : debt' = debt \ {d}
         ELSE /\ \E d \in debt : Receipt(d)
              /\ debt' = {d \in debt : ~Receipt(d)}
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, old>>

\* a coordinator hold (or fleet down) redeals a ready card no debt pins
Hold(c, o) ==
    /\ col[c] = "ready"
    /\ ~Pinned(c)
    /\ o # row[c]
    /\ gen[c] < MaxGen
    /\ row' = [row EXCEPT ![c] = o]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ UNCHANGED <<mach, col, sfg, child, debt, old>>

\* START: every remaining debt has its receipt in place (unsettledStopDebt)
Start ==
    /\ mach = "stop"
    /\ \A d \in debt : Receipt(d)
    /\ mach' = "run"
    /\ debt' = {}
    /\ UNCHANGED <<row, col, gen, sfg, child, old>>

Next ==
    \/ Stop \/ Start \/ Settle
    \/ \E c \in Cards : Take(c) \/ Finish(c) \/ Cancel(c) \/ Return(c)
    \/ \E c \in Cards, o \in Owners : Hold(c, o)

Spec == Init /\ [][Next]_vars /\ WF_vars(Settle)

(* Safety: no child alive at STOP is still alive once START runs.        *)
NoLiveChildAcrossStart == mach = "run" => old = {}

(* Safety: an owed lease whose child may still be alive is never moved.   *)
UnreturnedStaysPut ==
    \A d \in debt : child[d.id] # 0 =>
        (row[d.id] = d.row /\ col[d.id] = "working" /\ gen[d.id] = d.gen)

(* Liveness: a returned card does not pin its owner's row for the rest   *)
(* of the STOP; the hold is refused only until the settle.               *)
ReturnedFreesItsRow ==
    \A c \in Cards :
        (mach = "stop" /\ col[c] = "ready" /\ Pinned(c)) ~> (~Pinned(c) \/ mach = "run")
=============================================================================
