---------------------------- MODULE StopReturn ----------------------------
(***************************************************************************)
(* nova-sprint STOP, stop-return and START (internal/sprint/stop_return.go, *)
(* internal/sprint/store/machine_transition.go, engine.go stopDebtMutation; *)
(* docs/SPEC-SPRINT.md section 14). The module the code has cited since     *)
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
(* The owner's beat names the jobs it runs (sprint.FriendBeatNamesJob).    *)
(* After a STOP the server settles every owed lease whose owner has beaten *)
(* since the STOP (beaten) and whose beat no longer names the job          *)
(* (SettleByBeat), returning the card with a recorded reason, so START     *)
(* never waits on the member's own receipt; a lease whose job the beat     *)
(* still names stays owed, and an owner that has not beaten since the STOP *)
(* is reported (Report).                                                   *)
(*                                                                         *)
(* Broken (reversed witnesses):                                            *)
(*  "nosettle"        - the code before v1.2.6: the debt stays until START, *)
(*                 so a returned card pins its row for the whole STOP       *)
(*                 (ReturnedFreesItsRow fails);                             *)
(*  "noreceipt"       - Settle takes a debt off without its receipt: START  *)
(*                 crosses a live child (NoLiveChildAcrossStart fails);     *)
(*  "nosettlebybeat"  - the code before this fix: a debt whose owner has    *)
(*                 beaten and whose job the beat shows gone still stays     *)
(*                 owed until the owner's own receipt, so START waits on    *)
(*                 it (AbsentJobSettles fails).                             *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, Owners, MaxGen, Broken

VARIABLES mach, row, col, gen, sfg, child, debt, old, beat, beaten

\* old: the cards whose child was alive at the last STOP and has not been
\* cancelled since (history, read by NoLiveChildAcrossStart alone)
\* beat: what each owner's latest beat names running (its jobs)
\* beaten: the owners that have beaten since the last STOP
vars == <<mach, row, col, gen, sfg, child, debt, old, beat, beaten>>

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
    /\ beat \in [Owners -> SUBSET Cards]
    /\ beaten \subseteq Owners

Init ==
    /\ mach = "run"
    /\ row \in [Cards -> Owners]
    /\ col = [c \in Cards |-> "ready"]
    /\ gen = [c \in Cards |-> 1]
    /\ sfg = [c \in Cards |-> 0]
    /\ child = [c \in Cards |-> 0]
    /\ debt = {}
    /\ old = {}
    /\ beat = [o \in Owners |-> {}]
    /\ beaten = {}

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
    /\ UNCHANGED <<mach, row, gen, sfg, debt, old, beat, beaten>>

\* a finish while RUNNING: the child ends, the card goes on at a new generation
Finish(c) ==
    /\ mach = "run"
    /\ col[c] = "working"
    /\ child[c] = gen[c]
    /\ gen[c] < MaxGen
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ child' = [child EXCEPT ![c] = 0]
    /\ UNCHANGED <<mach, row, sfg, debt, old, beat, beaten>>

\* STOP captures every working card as owed (activeStopLeases)
Stop ==
    /\ mach = "run"
    /\ mach' = "stop"
    /\ debt' = {[id |-> c, row |-> row[c], gen |-> gen[c]] : c \in {x \in Cards : col[x] = "working"}}
    /\ old' = {c \in Cards : child[c] # 0}
    /\ beaten' = {}
    /\ UNCHANGED <<row, col, gen, sfg, child, beat>>

\* the owner cancels its child (the store trusts its acknowledgement)
Cancel(c) ==
    /\ mach = "stop"
    /\ child[c] # 0
    /\ child' = [child EXCEPT ![c] = 0]
    /\ old' = old \ {c}
    /\ UNCHANGED <<mach, row, col, gen, sfg, debt, beat, beaten>>

\* the owner's beat: it names the jobs it runs, and a beat while STOPPED marks
\* the owner beaten since the STOP (sprint.FriendBeatNamesJob, Report)
Beat(o, S) ==
    /\ beat' = [beat EXCEPT ![o] = S]
    /\ beaten' = IF mach = "stop" THEN beaten \cup {o} ELSE beaten
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, debt, old>>

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
    /\ UNCHANGED <<mach, row, child, debt, old, beat, beaten>>

\* settleStopDebt: each receipted debt leaves the machine record
Settle ==
    /\ Broken # "nosettle"
    /\ mach = "stop"
    /\ IF Broken = "noreceipt"
         THEN /\ debt # {}
              /\ \E d \in debt : debt' = debt \ {d}
         ELSE /\ \E d \in debt : Receipt(d)
              /\ debt' = {d \in debt : ~Receipt(d)}
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, old, beat, beaten>>

\* the server's beat-based settle: a debt whose owner has beaten since the STOP
\* and whose beat no longer names the job is returned with a recorded reason
\* and leaves the debt, without the owner's own receipt (settleStopDebtByBeat)
SettleByBeat ==
    /\ Broken # "nosettlebybeat"
    /\ mach = "stop"
    /\ \E d \in debt :
        /\ d.row \in beaten
        /\ d.id \notin beat[d.row]
        /\ d.id \notin old
        /\ col[d.id] = "working"
        /\ gen[d.id] = d.gen
        /\ gen[d.id] < MaxGen
        /\ col' = [col EXCEPT ![d.id] = "ready"]
        /\ sfg' = [sfg EXCEPT ![d.id] = d.gen]
        /\ gen' = [gen EXCEPT ![d.id] = d.gen + 1]
        /\ debt' = debt \ {d}
    /\ UNCHANGED <<mach, row, child, old, beat, beaten>>

\* a coordinator hold (or fleet down) redeals a ready card no debt pins
Hold(c, o) ==
    /\ col[c] = "ready"
    /\ ~Pinned(c)
    /\ o # row[c]
    /\ gen[c] < MaxGen
    /\ row' = [row EXCEPT ![c] = o]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ UNCHANGED <<mach, col, sfg, child, debt, old, beat, beaten>>

\* START: every remaining debt has its receipt in place (unsettledStopDebt)
Start ==
    /\ mach = "stop"
    /\ \A d \in debt : Receipt(d)
    /\ mach' = "run"
    /\ debt' = {}
    /\ beaten' = {}
    /\ UNCHANGED <<row, col, gen, sfg, child, old, beat>>

Next ==
    \/ Stop \/ Start \/ Settle \/ SettleByBeat
    \/ \E c \in Cards : Take(c) \/ Finish(c) \/ Cancel(c) \/ Return(c)
    \/ \E c \in Cards, o \in Owners : Hold(c, o)
    \/ \E o \in Owners, S \in SUBSET Cards : Beat(o, S)

Spec == Init /\ [][Next]_vars /\ WF_vars(Settle) /\ WF_vars(SettleByBeat)

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

(* Liveness: a debt whose owner has beaten since the STOP and whose beat  *)
(* shows the job gone is settled without the owner's own receipt, so      *)
(* START never waits on it (the 2026-10-10 seat's finding). The           *)
(* antecedent carries SettleByBeat's generation bound (d.gen < MaxGen),   *)
(* so the property only promises what the action can deliver: a debt at   *)
(* the top generation is settled by its own receipt (Return/Settle), not  *)
(* by the beat (mirrors CardMachine's ncut[c] = MaxCopies convention).    *)
(* A debt whose owner beats the job gone but sits at MaxGen is owed       *)
(* until its own receipt; SettleByBeat cannot reach it (d.gen < MaxGen).  *)
AbsentJobSettles ==
    \A d \in Debt :
        (mach = "stop" /\ d \in debt /\ d.row \in beaten /\ d.id \notin beat[d.row]
         /\ col[d.id] = "working" /\ gen[d.id] = d.gen /\ d.id \notin old /\ d.gen < MaxGen)
            ~> (d \notin debt)
=============================================================================
