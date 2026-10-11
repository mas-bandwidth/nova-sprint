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
(* A working card's child may finish while RUNNING: its report waits held  *)
(* (held[c]) until the machine takes it (ReportTaken), so a finished card  *)
(* is still working, and the owner's beat still names it (held).           *)
(*                                                                         *)
(* The owner's beat names the jobs it runs and the reports it holds at     *)
(* their generation (sprint.Beat.Live, LiveNamedAt), and an owner whose    *)
(* beat carries no live set (a worker from before v1.2.6) instead reports  *)
(* a stop-returns count (sprint.Beat.StopReturns). After a STOP the server *)
(* settles every owed lease whose owner has beaten since the STOP (beaten) *)
(* and whose beat shows the job gone: for a live-set beat, the live set    *)
(* names it neither running nor held; for a count-only beat, the count is  *)
(* zero. The code does an unfenced read, then the stop-return, then the    *)
(* settle (settleStopDebtByBeat, StopReturnStep, settleStopDebt), so the   *)
(* model splits SettleByBeat the same way: ReadSettle, SettleByBeat (the   *)
(* return) and Settle are separate steps. An owner that has not beaten     *)
(* since the STOP keeps its lease owed and is reported to the seat in a    *)
(* judgment (a note only, never a state change, so no Report action).      *)
(*                                                                         *)
(* Broken (reversed witnesses):                                            *)
(*  "nosettle"        - the code before v1.2.6: the debt stays until       *)
(*                 START, so a returned card pins its row for the whole    *)
(*                 STOP (ReturnedFreesItsRow fails);                       *)
(*  "noreceipt"       - Settle takes a debt off without its receipt:       *)
(*                 START crosses a live child (NoLiveChildAcrossStart      *)
(*                 fails);                                                 *)
(*  "nosettlebybeat"  - the code before this fix: a debt whose owner has   *)
(*                 beaten and whose job the beat shows gone still stays    *)
(*                 owed until the owner's own receipt, so START waits on   *)
(*                 it (AbsentJobSettles fails);                            *)
(*  "clearblocked"    - clear drops the live-lease guard and advances     *)
(*                 while a working lease remains                          *)
(*                 (ClearNeverWithALiveLease fails);                       *)
(*  "omitlive"        - a fresh beat omits a live (running) child: its     *)
(*                 live set names held reports only, so the settle returns *)
(*                 a card whose child still runs, and START crosses it     *)
(*                 (NoLiveChildAcrossStart fails).                         *)
(*                                                                         *)
(* Clear (docs/SPEC-SPRINT.md section 13, the blank-slate clear): once the *)
(* machine first stops; with no card still working (every lease returned  *)
(* or its card no longer working), clear advances to a blank epoch: every  *)
(* card ready at generation 1, no debt, no cancelled child remembered, no  *)
(* beat or beaten owner carried. A clear that still owes a debt never      *)
(* refuses while a returned card waits for its settle: it reads the live   *)
(* state (col), not the debt record.                                       *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, Owners, MaxGen, Broken

VARIABLES mach, row, col, gen, sfg, child, debt, old, beaten, held, live, settling, clearLive

\* old: the cards whose child was alive at the last STOP and has not been
\* cancelled since (history, read by NoLiveChildAcrossStart alone)
\* beaten: the owners that have beaten since the last STOP
\* held: the card's child finished and its report waits in the outbox
\* live: the owner's beat carries a live set (LiveKnown); an owner whose beat
\* carries none reports a stop-returns count instead
\* settling: the debts the server's unfenced read has decided to settle
\* clearLive: a ghost recording whether an epoch advance crossed a working lease
vars == <<mach, row, col, gen, sfg, child, debt, old, beaten, held, live, settling, clearLive>>

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
    /\ beaten \subseteq Owners
    /\ held \subseteq Cards
    /\ live \in [Owners -> BOOLEAN]
    /\ settling \subseteq Debt
    /\ clearLive \in BOOLEAN

Init ==
    /\ mach = "run"
    /\ row \in [Cards -> Owners]
    /\ col = [c \in Cards |-> "ready"]
    /\ gen = [c \in Cards |-> 1]
    /\ sfg = [c \in Cards |-> 0]
    /\ child = [c \in Cards |-> 0]
    /\ debt = {}
    /\ old = {}
    /\ beaten = {}
    /\ held = {}
    /\ live \in [Owners -> BOOLEAN]
    /\ settling = {}
    /\ clearLive = FALSE

\* the owner's same-row, next-generation return receipt (stopDebtReturned)
Receipt(d) ==
    /\ row[d.id] = d.row
    /\ col[d.id] = "ready"
    /\ sfg[d.id] = d.gen
    /\ gen[d.id] = d.gen + 1

Pinned(c) == \E d \in debt : d.id = c

\* the row's live set names the card at its generation, running or held
\* (sprint.LiveNamedAt). Broken "omitlive" reads a fresh beat that omits a
\* live (running) child: it names held reports only.
LiveNames(o, c) ==
    /\ row[c] = o
    /\ IF Broken = "omitlive" THEN c \in held ELSE child[c] # 0 \/ c \in held

\* the row's stop-returns count is above zero (sprint.Beat.StopReturns): some
\* working card the owner still runs or holds a report for (a cancelled card
\* is owed its own return and does not count)
StopReturnsOwed(o) ==
    \E c \in Cards : row[c] = o /\ col[c] = "working" /\ (child[c] # 0 \/ c \in held)

\* the settle's read: the owner has beaten since the STOP and its beat shows
\* the job gone -- for a live-set beat the live set names it neither running
\* nor held, for a count-only beat the stop-returns count is zero
JobGone(d) ==
    /\ d.row \in beaten
    /\ col[d.id] = "working"
    /\ gen[d.id] = d.gen
    /\ gen[d.id] < MaxGen
    /\ IF live[d.row] THEN ~LiveNames(d.row, d.id) ELSE ~StopReturnsOwed(d.row)

\* a take while RUNNING starts the owner's child at the card's generation
Take(c) ==
    /\ mach = "run"
    /\ col[c] = "ready"
    /\ child[c] = 0
    /\ col' = [col EXCEPT ![c] = "working"]
    /\ child' = [child EXCEPT ![c] = gen[c]]
    /\ held' = held \ {c}
    /\ UNCHANGED <<mach, row, gen, sfg, debt, old, beaten, live, settling, clearLive>>

\* a child finishes while RUNNING: its report waits held, the card stays working
ChildFinishes(c) ==
    /\ mach = "run"
    /\ col[c] = "working"
    /\ child[c] = gen[c]
    /\ child' = [child EXCEPT ![c] = 0]
    /\ held' = held \cup {c}
    /\ UNCHANGED <<mach, row, col, gen, sfg, debt, old, beaten, live, settling, clearLive>>

\* the machine takes the held report while RUNNING: the card goes on at a new
\* generation
ReportTaken(c) ==
    /\ mach = "run"
    /\ col[c] = "working"
    /\ c \in held
    /\ gen[c] < MaxGen
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ child' = [child EXCEPT ![c] = 0]
    /\ held' = held \ {c}
    /\ UNCHANGED <<mach, row, sfg, debt, old, beaten, live, settling, clearLive>>

\* STOP captures every working card as owed (activeStopLeases)
Stop ==
    /\ mach = "run"
    /\ mach' = "stop"
    /\ debt' = {[id |-> c, row |-> row[c], gen |-> gen[c]] : c \in {x \in Cards : col[x] = "working"}}
    /\ old' = {c \in Cards : child[c] # 0}
    /\ beaten' = {}
    /\ UNCHANGED <<row, col, gen, sfg, child, held, live, settling, clearLive>>

\* the owner cancels its child (the store trusts its acknowledgement)
Cancel(c) ==
    /\ mach = "stop"
    /\ child[c] # 0
    /\ child' = [child EXCEPT ![c] = 0]
    /\ old' = old \ {c}
    /\ UNCHANGED <<mach, row, col, gen, sfg, debt, beaten, held, live, settling, clearLive>>

\* the owner's beat: it names the jobs it runs and the reports it holds (the
\* derived live set, or the stop-returns count where it carries no live set),
\* and a beat while STOPPED marks the owner beaten since the STOP. While
\* STOPPED the live set is drop-only: a member starts no new job, so a job the
\* beat shows gone stays gone.
Beat(o) ==
    /\ beaten' = IF mach = "stop" THEN beaten \cup {o} ELSE beaten
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, debt, old, held, live, settling, clearLive>>

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
    /\ held' = held \ {c}
    /\ UNCHANGED <<mach, row, child, debt, old, beaten, live, settling, clearLive>>

\* settleStopDebt: each receipted debt leaves the machine record
Settle ==
    /\ Broken # "nosettle"
    /\ mach = "stop"
    /\ IF Broken = "noreceipt"
         THEN /\ debt # {}
              /\ \E d \in debt : debt' = debt \ {d}
         ELSE /\ \E d \in debt : Receipt(d)
              /\ debt' = {d \in debt : ~Receipt(d)}
    /\ settling' = settling \cap debt'
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, old, beaten, held, live, clearLive>>

\* the server's beat-based settle, first of three steps: the unfenced read that
\* decides the debts whose beat shows the job gone (settleStopDebtByBeat's read)
ReadSettle ==
    /\ Broken # "nosettlebybeat"
    /\ mach = "stop"
    /\ \E d \in debt :
        /\ JobGone(d)
        /\ d \notin settling
        /\ settling' = settling \cup {d}
    /\ UNCHANGED <<mach, row, col, gen, sfg, child, debt, old, beaten, held, live, clearLive>>

\* the server's beat-based settle, second step: the stop-return (StopReturnStep)
\* for a debt the read decided, with a recorded reason; the settle itself is the
\* ordinary Settle above (the after hook's settleStopDebt)
SettleByBeat ==
    /\ Broken # "nosettlebybeat"
    /\ mach = "stop"
    /\ \E d \in settling :
        /\ col[d.id] = "working"
        /\ gen[d.id] = d.gen
        /\ gen[d.id] < MaxGen
        /\ col' = [col EXCEPT ![d.id] = "ready"]
        /\ sfg' = [sfg EXCEPT ![d.id] = d.gen]
        /\ gen' = [gen EXCEPT ![d.id] = d.gen + 1]
        /\ held' = held \ {d.id}
    /\ UNCHANGED <<mach, row, child, debt, old, beaten, live, settling, clearLive>>

\* a coordinator hold (or fleet down) redeals a ready card no debt pins
Hold(c, o) ==
    /\ col[c] = "ready"
    /\ ~Pinned(c)
    /\ o # row[c]
    /\ gen[c] < MaxGen
    /\ row' = [row EXCEPT ![c] = o]
    /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
    /\ UNCHANGED <<mach, col, sfg, child, debt, old, beaten, held, live, settling, clearLive>>

\* START: every remaining debt has its receipt in place (unsettledStopDebt)
Start ==
    /\ mach = "stop"
    /\ \A d \in debt : Receipt(d)
    /\ mach' = "run"
    /\ debt' = {}
    /\ beaten' = {}
    /\ settling' = {}
    /\ UNCHANGED <<row, col, gen, sfg, child, old, held, live, clearLive>>

\* clear.go first sets STOPPED, then reads the current work/read leases.
\* Splitting those steps preserves a receipt between STOP and the lease read.
\* The readers table is the separate pins/readers card's scope; col represents
\* the working leases here. A retry while already STOPPED reads them again.
ClearAdvance ==
    /\ mach = "stop"
    /\ (Broken = "clearblocked" \/ \A c \in Cards : col[c] = "ready")
    /\ clearLive' = (\E c \in Cards : col[c] = "working")
    /\ col' = [c \in Cards |-> "ready"]
    /\ gen' = [c \in Cards |-> 1]
    /\ sfg' = [c \in Cards |-> 0]
    /\ child' = [c \in Cards |-> 0]
    /\ debt' = {}
    /\ old' = {}
    /\ beaten' = {}
    /\ held' = {}
    /\ settling' = {}
    /\ UNCHANGED <<mach, row, live>>

\* From RUNNING, clear has the same first effect as STOP. With a live lease
\* the second step is refused (stutters); after its receipt, clear can retry.
Clear == Stop \/ ClearAdvance

ClearNeverWithALiveLease == ~clearLive

Next ==
    \/ Stop \/ Start \/ Settle \/ ReadSettle \/ SettleByBeat \/ Clear
    \/ \E c \in Cards : Take(c) \/ ChildFinishes(c) \/ ReportTaken(c) \/ Cancel(c) \/ Return(c)
    \/ \E c \in Cards, o \in Owners : Hold(c, o)
    \/ \E o \in Owners : Beat(o)

Spec == Init /\ [][Next]_vars /\ WF_vars(Settle) /\ WF_vars(ReadSettle) /\ WF_vars(SettleByBeat)

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
(* antecedent carries the settle's generation bound (d.gen < MaxGen),     *)
(* so the property only promises what the settle can deliver: a debt at   *)
(* the top generation is settled by its own receipt (Return/Settle), not  *)
(* by the beat.                                                           *)
AbsentJobSettles ==
    \A d \in Debt :
        JobGone(d) ~> (d \notin debt)

(* With no card still working, clear is enabled from RUNNING or STOPPED: *)
(* STOP comes first when running; the advance ignores a returned lease's *)
(* returned card's unsettled debt notwithstanding).                       *)
ClearEnabled ==
    (\A c \in Cards : col[c] = "ready") => ENABLED Clear
=============================================================================
