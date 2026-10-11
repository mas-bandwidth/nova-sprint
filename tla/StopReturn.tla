---------------------------- MODULE StopReturn ----------------------------
(***************************************************************************)
(* nova-sprint STOP, stop-return and START (internal/sprint/stop_return.go, *)
(* internal/sprint/store/machine_transition.go, engine.go stopDebtMutation; *)
(* docs/SPEC-SPRINT.md section 14).                                        *)
(*                                                                         *)
(* A card is on an owner row, ready or working, at a generation; a read    *)
(* card is asked or reading on its reader's row. A working card has the    *)
(* owner's child, alive or not (child[c] is the generation it runs at, 0   *)
(* for none); a read has no child. STOP captures every working card and    *)
(* every reading read as a debt record [id, row, gen] (activeStopLeases:   *)
(* the fleet's working column and the readers' reading column). The owner  *)
(* cancels its child, then stop-returns the card (idle, same row, gen+1,   *)
(* stopped_from_gen = gen): the receipt. Settle takes each debt whose      *)
(* receipt is in place off the machine record (settleStopDebt).            *)
(*                                                                         *)
(* A lease owns its card by generation (stopOwned): it owns the card only  *)
(* while the card is at the generation it captured and does not carry the  *)
(* return receipt (sfg = d.gen and gen > d.gen). A returned card moves     *)
(* freely (a hold, a give) and its receipt survives the move. No step may   *)
(* move a still-owned card (stopDebtMutation); Hold moves an idle card     *)
(* that no debt owns to another row at a new generation.                   *)
(*                                                                         *)
(* START (unsettledStopDebt) passes each remaining debt that carries its   *)
(* receipt, or whose card's child finished and its held report is named by *)
(* the row's fresh live set (LiveHeldAt: the report is taken once the      *)
(* machine runs, so nothing is lost), or, on a pre-upgrade record (no      *)
(* stop-issued flag), no live lease at all; then it clears the debt.       *)
(*                                                                         *)
(* The owner's beat names the jobs it runs (sprint.FriendBeatNamesJob).    *)
(* After a STOP the server settles every owed lease whose owner has beaten *)
(* since the STOP (beaten) and whose beat no longer names the job          *)
(* (SettleByBeat), returning the card with a recorded reason, so START     *)
(* never waits on the member's own receipt.                                *)
(*                                                                         *)
(* Broken (reversed witnesses):                                            *)
(*  "nosettle"        - the code before v1.2.6: the debt stays until       *)
(*                 START and matches a card by id, so a returned card      *)
(*                 pins its row for the whole STOP (ReturnedFreesItsRow    *)
(*                 fails);                                                 *)
(*  "noreceipt"       - Settle takes a debt off without its receipt:       *)
(*                 START crosses a live child (NoLiveChildAcrossStart      *)
(*                 fails);                                                 *)
(*  "nosettlebybeat"  - the code before this fix: a debt whose owner has   *)
(*                 beaten and whose job the beat shows gone still stays    *)
(*                 owed until the owner's own receipt, so START waits on   *)
(*                 it (AbsentJobSettles fails);                            *)
(*  "clearblocked"    - clear reads the debt record, not the live state,   *)
(*                 and refuses while any debt remains even when every      *)
(*                 lease is returned, so a stop-returned lease blocks the  *)
(*                 epoch advance (ClearEnabled fails).                     *)
(*  "byid"            - a lease owns its card by id, ignoring the          *)
(*                 generation: a card returned (or redealt) at gen+1 is    *)
(*                 still owned by the lease captured at gen                *)
(*                 (NoStaleGenOwned fails).                                *)
(*                                                                         *)
(* Clear (docs/SPEC-SPRINT.md section 13, the blank-slate clear): once the *)
(* machine is STOPPED and no card is still working (every lease returned   *)
(* or its card no longer working), clear advances to a blank epoch: every  *)
(* card idle at generation 1, no debt, no cancelled child remembered, no   *)
(* beat or beaten owner carried.                                           *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, Owners, Readers, MaxGen, Broken

ASSUME Readers \subseteq Cards

VARIABLES mach, row, col, gen, sfg, child, debt, old, beat, beaten, held, stopissued

\* old: the cards whose child was alive at the last STOP and has not been
\* cancelled since (history, read by NoLiveChildAcrossStart alone)
\* beat: what each owner's latest beat names (its jobs running and its held reports)
\* beaten: the owners that have beaten since the last STOP
\* held: the card's child finished and its report waits in the live set
\* stopissued: the durable STOP-issued flag a post-upgrade STOP writes
vars == <<mach, row, col, gen, sfg, child, debt, old, beat, beaten, held, stopissued>>

Debt == [id: Cards, row: Owners, gen: 1..MaxGen]

\* a card's active column: reads read, fleet cards work
Active(c) == IF c \in Readers THEN "reading" ELSE "working"

\* a card's idle column: reads asked, fleet cards ready
Idle(c) == IF c \in Readers THEN "asked" ELSE "ready"

\* a live lease: the card the STOP captures (working or reading)
LiveLease(c) == col[c] = Active(c)

TypeOK ==
    /\ mach \in {"run", "stop"}
    /\ row \in [Cards -> Owners]
    /\ \A c \in Cards : col[c] \in (IF c \in Readers THEN {"asked", "reading"} ELSE {"ready", "working"})
    /\ gen \in [Cards -> 1..MaxGen]
    /\ sfg \in [Cards -> 0..MaxGen]
    /\ child \in [Cards -> 0..MaxGen]
    /\ debt \subseteq Debt
    /\ old \subseteq Cards
    /\ beat \in [Owners -> SUBSET Cards]
    /\ beaten \subseteq Owners
    /\ held \in [Cards -> BOOLEAN]
    /\ stopissued \in BOOLEAN

Init ==
    /\ mach = "run"
    /\ row \in [Cards -> Owners]
    /\ col = [c \in Cards |-> Idle(c)]
    /\ gen = [c \in Cards |-> 1]
    /\ sfg = [c \in Cards |-> 0]
    /\ child = [c \in Cards |-> 0]
    /\ debt = {}
    /\ old = {}
    /\ beat = [o \in Owners |-> {}]
    /\ beaten = {}
    /\ held = [c \in Cards |-> FALSE]
    /\ stopissued = FALSE

\* the owner's same-row, next-generation return receipt (stopDebtReturned)
Receipt(d) ==
    /\ row[d.id] = d.row
    /\ col[d.id] = Idle(d.id)
    /\ sfg[d.id] = d.gen
    /\ gen[d.id] = d.gen + 1

\* stopOwned: an entry owns its card unless the card carries its return
\* receipt (sfg = d.gen, gen > d.gen). Keyed by generation, not id: a return
\* steps the generation and the receipt survives a later move, so the entry
\* stops owning the card. Broken "byid"/"nosettle" owns by id (the v1.2.5 debt).
Owned(d) ==
    IF Broken \in {"byid", "nosettle"} THEN TRUE
    ELSE ~(sfg[d.id] = d.gen /\ gen[d.id] > d.gen)

\* a debt entry owns its card (Pinned is what stopDebtMutation refuses)
Pinned(c) == \E d \in debt : d.id = c /\ Owned(d)

\* the start reading of a debt entry whose child finished: the fresh live set
\* names the card held at the captured generation (LiveHeldAt)
HeldReportNamed(d) ==
    /\ held[d.id]
    /\ gen[d.id] = d.gen
    /\ d.id \in beat[d.row]

\* a take while RUNNING starts the owner's child at the card's generation
Take(c) ==
    /\ mach = "run"
    /\ c \notin Readers
    /\ col[c] = "ready"
    /\ child[c] = 0
    /\ col' = [col EXCEPT ![c] = "working"]
    /\ child' = [child EXCEPT ![c] = gen[c]]
    /\ held' = [held EXCEPT ![c] = FALSE]
    /\ UNCHANGED <<mach, row, gen, sfg, debt, old, beat, beaten, stopissued>>

\* a child finishes while RUNNING: its report waits held, the card stays working
ChildFinishes(c) ==
    /\ mach = "run"
    /\ c \notin Readers
    /\ col[c] = "working"
    /\ child[c] = gen[c]
    /\ child' = [child EXCEPT ![c] = 0]
    /\ held' = [held EXCEPT ![c] = TRUE]
    /\ UNCHANGED <<mach, row, col, gen, sfg, debt, old, beat, beaten, stopissued>>

\* the machine takes the held report while RUNNING: the card goes on at a new
\* generation
ReportTaken(c) ==
    /\ mach = "run"
    /\ c \notin Readers
    /\ col[c] = "working"
    /\ held[c]
    /\ gen[c] < MaxGen
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ child' = [child EXCEPT ![c] = 0]
    /\ held' = [held EXCEPT ![c] = FALSE]
    /\ UNCHANGED <<mach, row, sfg, debt, old, beat, beaten, stopissued>>

\* a reader starts the read: asked -> reading
Read(c) ==
    /\ mach = "run"
    /\ c \in Readers
    /\ col[c] = "asked"
    /\ col' = [col EXCEPT ![c] = "reading"]
    /\ UNCHANGED <<mach, row, gen, sfg, child, debt, old, beat, beaten, held, stopissued>>

\* a read completes: reading -> asked at the same generation
ReadDone(c) ==
    /\ mach = "run"
    /\ c \in Readers
    /\ col[c] = "reading"
    /\ col' = [col EXCEPT ![c] = "asked"]
    /\ UNCHANGED <<mach, row, gen, sfg, child, debt, old, beat, beaten, held, stopissued>>

\* STOP captures every working card and every reading read as owed
\* (activeStopLeases), and writes the durable flag
Stop ==
    /\ mach = "run"
    /\ mach' = "stop"
    /\ debt' = {[id |-> c, row |-> row[c], gen |-> gen[c]] : c \in {x \in Cards : LiveLease(x)}}
    /\ old' = {c \in Cards : child[c] # 0}
    /\ beaten' = {}
    /\ stopissued' = TRUE
    /\ UNCHANGED <<row, col, gen, sfg, child, beat, held>>

\* the owner cancels its child (the store trusts its acknowledgement)
Cancel(c) ==
    /\ mach = "stop"
    /\ child[c] # 0
    /\ child' = [child EXCEPT ![c] = 0]
    /\ old' = old \ {c}
    /\ UNCHANGED <<mach, row, col, gen, sfg, debt, beat, beaten, held, stopissued>>

\* the owner's beat: it names the jobs it runs and the reports it holds, and a
\* beat while STOPPED marks the owner beaten since the STOP. While STOPPED the
\* beat is drop-only.
Beat(o, S) ==
    /\ mach = "stop" => S \subseteq beat[o]
    /\ beat' = [beat EXCEPT ![o] = S]
    /\ beaten' = IF mach = "stop" THEN beaten \cup {o} ELSE beaten
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, debt, old, held, stopissued>>

\* stop-return --as <row> <c>@<gen>, after the observed cancellation: an idle
\* return (ready or asked) at the next generation with the receipt
Return(c) ==
    /\ mach = "stop"
    /\ col[c] = Active(c)
    /\ child[c] = 0
    /\ gen[c] < MaxGen
    /\ \E d \in debt : d.id = c /\ d.row = row[c] /\ d.gen = gen[c]
    /\ col' = [col EXCEPT ![c] = Idle(c)]
    /\ sfg' = [sfg EXCEPT ![c] = gen[c]]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ held' = [held EXCEPT ![c] = FALSE]
    /\ UNCHANGED <<mach, row, child, debt, old, beat, beaten, stopissued>>

\* settleStopDebt: each receipted debt leaves the machine record
Settle ==
    /\ Broken # "nosettle"
    /\ mach = "stop"
    /\ IF Broken = "noreceipt"
         THEN /\ debt # {}
              /\ \E d \in debt : debt' = debt \ {d}
         ELSE /\ \E d \in debt : Receipt(d)
              /\ debt' = {d \in debt : ~Receipt(d)}
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, old, beat, beaten, held, stopissued>>

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
        /\ held' = [held EXCEPT ![d.id] = FALSE]
        /\ debt' = debt \ {d}
    /\ UNCHANGED <<mach, row, child, old, beat, beaten, stopissued>>

\* a coordinator hold (or fleet down) redeals an idle card no debt owns
Hold(c, o) ==
    /\ col[c] = Idle(c)
    /\ ~Pinned(c)
    /\ o # row[c]
    /\ gen[c] < MaxGen
    /\ row' = [row EXCEPT ![c] = o]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ UNCHANGED <<mach, col, sfg, child, debt, old, beat, beaten, held, stopissued>>

\* START (unsettledStopDebt): every remaining debt carries its receipt, or its
\* card's held report is named by the fresh live set; a pre-upgrade record (no
\* stop-issued flag) refuses while any live lease remains.
Start ==
    /\ mach = "stop"
    /\ stopissued \/ ~(\E c \in Cards : LiveLease(c))
    /\ \A d \in debt : Receipt(d) \/ HeldReportNamed(d)
    /\ mach' = "run"
    /\ debt' = {}
    /\ beaten' = {}
    /\ UNCHANGED <<row, col, gen, sfg, child, old, beat, held, stopissued>>

\* clear: the blank-slate advance. The fixed guard reads the live state (no
\* card working); the reversed witness "clearblocked" reads the debt record
\* instead and blocks on a returned lease that has not settled yet.
Clear ==
    /\ mach = "stop"
    /\ IF Broken = "clearblocked"
         THEN debt = {}
         ELSE \A c \in Cards : col[c] = Idle(c)
    /\ col' = [c \in Cards |-> Idle(c)]
    /\ gen' = [c \in Cards |-> 1]
    /\ sfg' = [c \in Cards |-> 0]
    /\ child' = [c \in Cards |-> 0]
    /\ debt' = {}
    /\ old' = {}
    /\ beat' = [o \in Owners |-> {}]
    /\ beaten' = {}
    /\ held' = [c \in Cards |-> FALSE]
    /\ UNCHANGED <<mach, row, stopissued>>

Next ==
    \/ Stop \/ Start \/ Settle \/ SettleByBeat \/ Clear
    \/ \E c \in Cards : Return(c)
    \/ \E c \in Cards :
         IF c \in Readers THEN Read(c) \/ ReadDone(c)
         ELSE Take(c) \/ ChildFinishes(c) \/ ReportTaken(c) \/ Cancel(c)
    \/ \E c \in Cards, o \in Owners : Hold(c, o)
    \/ \E o \in Owners, S \in SUBSET Cards : Beat(o, S)

Spec == Init /\ [][Next]_vars /\ WF_vars(Settle) /\ WF_vars(SettleByBeat)

(* Safety: no child alive at STOP is still alive once START runs.        *)
NoLiveChildAcrossStart == mach = "run" => old = {}

(* Safety: an owed lease whose child may still be alive is never moved.   *)
UnreturnedStaysPut ==
    \A d \in debt : child[d.id] # 0 =>
        (row[d.id] = d.row /\ col[d.id] = "working" /\ gen[d.id] = d.gen)

(* Safety: a lease never owns a card at a generation other than its current *)
(* one (stopOwned keys by generation).                                       *)
NoStaleGenOwned ==
    \A d \in debt : Owned(d) => gen[d.id] = d.gen

(* Liveness: a returned card does not pin its owner's row for the rest   *)
(* of the STOP; the hold is refused only until the settle.               *)
ReturnedFreesItsRow ==
    \A c \in Cards :
        (mach = "stop" /\ col[c] = Idle(c) /\ Pinned(c)) ~> (~Pinned(c) \/ mach = "run")

(* Liveness: a debt whose owner has beaten since the STOP and whose beat  *)
(* shows the job gone is settled without the owner's own receipt, so      *)
(* START never waits on it.                                               *)
AbsentJobSettles ==
    \A d \in Debt :
        (mach = "stop" /\ d \in debt /\ d.row \in beaten /\ d.id \notin beat[d.row]
         /\ col[d.id] = "working" /\ gen[d.id] = d.gen /\ d.id \notin old /\ d.gen < MaxGen)
            ~> (d \notin debt)

(* A stop-returned lease never blocks clear: once the machine is stopped  *)
(* and no card is still working, clear is enabled.                        *)
ClearEnabled ==
    (mach = "stop" /\ \A c \in Cards : col[c] = Idle(c)) => ENABLED Clear
=============================================================================
