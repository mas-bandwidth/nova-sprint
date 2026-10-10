---------------------------- MODULE StopReturn ----------------------------
(***************************************************************************)
(* nova-sprint STOP, stop-return, START and clear                          *)
(* (internal/sprint/stop_return.go, internal/sprint/store/machine_transition.go, *)
(* internal/sprint/store/clear.go, engine.go stopDebtMutation;             *)
(* docs/SPEC-SPRINT.md sections 13 and 14). The module the code has cited  *)
(* since the durable stop debt landed, written 2026-10-10 for v1.2.6 after *)
(* a returned card pinned its owner's row: "STOP owns friend.alex:<card>@1"*)
(* refused a coordinator hold after the stop-return had moved it to gen 2. *)
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
(* Clear stops the machine and advances the epoch once. It refuses only    *)
(* leases still live after STOP: a working card (Fleet Working, Readers    *)
(* Reading in the code) blocks; a stop-returned or otherwise not-working   *)
(* card never blocks. A successful clear starts the next epoch blank: no   *)
(* working card, no debt, no child, no hold (the blank-slate rule,         *)
(* docs/SPEC-SPRINT.md section 13). Nothing of the old epoch is carried.   *)
(*                                                                         *)
(* Broken (reversed witnesses):                                            *)
(*  "nosettle"   - the code before v1.2.6: the debt stays until START, so  *)
(*                 a returned card pins its row for the whole STOP         *)
(*                 (ReturnedFreesItsRow fails);                            *)
(*  "noreceipt"  - Settle takes a debt off without its receipt: START      *)
(*                 crosses a live child (NoLiveChildAcrossStart fails).    *)
(*  "clearkeeps" - Clear advances the epoch but keeps the old epoch's      *)
(*                 working cards and debt, so work of an earlier epoch is  *)
(*                 still on the new one (WorkingIsCurrentEpoch fails).     *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, Owners, MaxGen, Broken

VARIABLES mach, row, col, gen, sfg, child, debt, old, epoch, ep

\* old: the cards whose child was alive at the last STOP and has not been
\* cancelled since (history, read by NoLiveChildAcrossStart alone)
\* epoch: the sprint's epoch; ep[c]: the epoch c was most recently taken at
vars == <<mach, row, col, gen, sfg, child, debt, old, epoch, ep>>

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
    /\ epoch \in Nat
    /\ ep \in [Cards -> Nat]

Init ==
    /\ mach = "run"
    /\ row \in [Cards -> Owners]
    /\ col = [c \in Cards |-> "ready"]
    /\ gen = [c \in Cards |-> 1]
    /\ sfg = [c \in Cards |-> 0]
    /\ child = [c \in Cards |-> 0]
    /\ debt = {}
    /\ old = {}
    /\ epoch = 0
    /\ ep = [c \in Cards |-> 0]

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
    /\ ep' = [ep EXCEPT ![c] = epoch]
    /\ UNCHANGED <<mach, row, gen, sfg, debt, old, epoch>>

\* a finish while RUNNING: the child ends, the card goes on at a new generation
Finish(c) ==
    /\ mach = "run"
    /\ col[c] = "working"
    /\ child[c] = gen[c]
    /\ gen[c] < MaxGen
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ child' = [child EXCEPT ![c] = 0]
    /\ UNCHANGED <<mach, row, sfg, debt, old, epoch, ep>>

\* STOP captures every working card as owed (activeStopLeases)
Stop ==
    /\ mach = "run"
    /\ mach' = "stop"
    /\ debt' = {[id |-> c, row |-> row[c], gen |-> gen[c]] : c \in {x \in Cards : col[x] = "working"}}
    /\ old' = {c \in Cards : child[c] # 0}
    /\ UNCHANGED <<row, col, gen, sfg, child, epoch, ep>>

\* the owner cancels its child (the store trusts its acknowledgement)
Cancel(c) ==
    /\ mach = "stop"
    /\ child[c] # 0
    /\ child' = [child EXCEPT ![c] = 0]
    /\ old' = old \ {c}
    /\ UNCHANGED <<mach, row, col, gen, sfg, debt, epoch, ep>>

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
    /\ UNCHANGED <<mach, row, child, debt, old, epoch, ep>>

\* settleStopDebt: each receipted debt leaves the machine record
Settle ==
    /\ Broken # "nosettle"
    /\ mach = "stop"
    /\ IF Broken = "noreceipt"
         THEN /\ debt # {}
              /\ \E d \in debt : debt' = debt \ {d}
         ELSE /\ \E d \in debt : Receipt(d)
              /\ debt' = {d \in debt : ~Receipt(d)}
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, old, epoch, ep>>

\* a coordinator hold (or fleet down) redeals a ready card no debt pins
Hold(c, o) ==
    /\ col[c] = "ready"
    /\ ~Pinned(c)
    /\ o # row[c]
    /\ gen[c] < MaxGen
    /\ row' = [row EXCEPT ![c] = o]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ UNCHANGED <<mach, col, sfg, child, debt, old, epoch, ep>>

\* START: every remaining debt has its receipt in place (unsettledStopDebt)
Start ==
    /\ mach = "stop"
    /\ \A d \in debt : Receipt(d)
    /\ mach' = "run"
    /\ debt' = {}
    /\ UNCHANGED <<row, col, gen, sfg, child, old, epoch, ep>>

\* Clear (docs/SPEC-SPRINT.md section 13): stops the machine, refuses any
\* lease still live after STOP (a working card: Fleet Working or Readers
\* Reading), then advances the epoch once into a blank slate -- no working
\* card, no debt, no child, no hold. A stop-returned or otherwise not-working
\* card never blocks. The reversed witness "clearkeeps" drops the refusal and
\* the reset, carrying the old epoch's work and debt into the new one.
Clear ==
    /\ (Broken # "clearkeeps" => \A c \in Cards : col[c] # "working")
    /\ mach' = "stop"
    /\ epoch' = epoch + 1
    /\ IF Broken = "clearkeeps"
         THEN UNCHANGED <<row, col, gen, sfg, child, debt, old, ep>>
         ELSE /\ col' = [c \in Cards |-> "ready"]
              /\ child' = [c \in Cards |-> 0]
              /\ debt' = {}
              /\ old' = {}
              /\ sfg' = [c \in Cards |-> 0]
              /\ gen' = [c \in Cards |-> 1]
              /\ row' = row
              /\ ep' = [c \in Cards |-> 0]

Next ==
    \/ Stop \/ Start \/ Settle \/ Clear
    \/ \E c \in Cards : Take(c) \/ Finish(c) \/ Cancel(c) \/ Return(c)
    \/ \E c \in Cards, o \in Owners : Hold(c, o)

Spec == Init /\ [][Next]_vars /\ WF_vars(Settle)

(* Safety: no child alive at STOP is still alive once START runs.        *)
NoLiveChildAcrossStart == mach = "run" => old = {}

(* Safety: an owed lease whose child may still be alive is never moved.   *)
UnreturnedStaysPut ==
    \A d \in debt : child[d.id] # 0 =>
        (row[d.id] = d.row /\ col[d.id] = "working" /\ gen[d.id] = d.gen)

(* Safety: a clear never carries a lease into the new epoch: a card that is *)
(* working was taken at the current epoch, so no old epoch's work or lease  *)
(* survives a clear (the blank-slate rule).                                 *)
WorkingIsCurrentEpoch ==
    \A c \in Cards : col[c] = "working" => ep[c] = epoch

(* Liveness: a returned card does not pin its owner's row for the rest   *)
(* of the STOP; the hold is refused only until the settle.               *)
ReturnedFreesItsRow ==
    \A c \in Cards :
        (mach = "stop" /\ col[c] = "ready" /\ Pinned(c)) ~> (~Pinned(c) \/ mach = "run")
=============================================================================
