------------------------------ MODULE LiveRuns ------------------------------
(***************************************************************************)
(* Working means a live run (v1.2.6, 2026-10-10).                          *)
(*                                                                         *)
(* The fleet table's working column on a row (a fleet member or a friend)  *)
(* against what that row's worker really runs, while the machine RUNS and  *)
(* while it is STOPPED, with the STOP debt and the worker's finished       *)
(* reports. Code: internal/sprint/live.go (LiveReturns, the return of a    *)
(* working card its row's live set has lacked for LiveGrace), the tick     *)
(* (internal/sprint/store/tick.go: the reconcile runs on a STOPPED tick as *)
(* on a running one, as one step of the one writer), the beat's live set   *)
(* (sprint.Beat.Live: fleet beat --live, friend beat --live), the STOP     *)
(* debt (internal/sprint/store/machine_transition.go stopOwned, engine.go  *)
(* stopDebtMutation) and stop-return (internal/sprint/stop_return.go).     *)
(*                                                                         *)
(* Found 2026-10-10: the sprint STOPPED at ~15:00Z showed 30 working on    *)
(* six fleet hosts and 29 on friend.zhi at ~18:25Z with no harness run     *)
(* anywhere: a take whose run had gone stayed working, and a STOPPED       *)
(* machine runs no tick (Defect A). And a card stop-returned (working ->   *)
(* ready, gen+1) still read as STOP-owned, so hold refused every card of   *)
(* the row until start (Defect B: the debt matched a card by its id).      *)
(*                                                                         *)
(* The model. Each card has one owner row (a give or hold moving it to     *)
(* another row is CoordMove; who the row is does not matter here, only     *)
(* that a STOP-owned card does not move). A card's worker state is none,   *)
(* run (a child runs it), cancelled (the stop cancelled the child and the  *)
(* card is owed a stop-return), or held (the child finished and its        *)
(* report waits in the outbox for the machine to take it). A beat window   *)
(* passes per row (Beat): the row's live set is the cards it runs or       *)
(* holds a report for, and each working card of the row not in it counts  *)
(* one more window absent. The tick (Tick) returns every working card      *)
(* absent for Grace windows to ready at its next generation with its      *)
(* receipt, RUNNING or STOPPED; a tick runs between two beat windows (the  *)
(* tick is every second, a beat window 15 s): Beat waits for it.          *)
(*                                                                         *)
(* Reversed witnesses (Broken):                                            *)
(*   "stoppednoreconcile" the v1.2.5 tick: a STOPPED tick returns nothing  *)
(*                        (breaks WorkingIsLive);                          *)
(*   "debtbyid"           the v1.2.5 debt: a card is STOP-owned while its  *)
(*                        id is in the debt, returned or not (breaks       *)
(*                        StopMarkerIsWorking);                            *)
(*   "heldnotlive"        a live set without the held reports: the tick   *)
(*                        returns a finished card and its report's         *)
(*                        generation is gone (breaks NoReportLost).        *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
  Cards,    \* e.g. {"c1", "c2"}
  Rows,     \* e.g. {"r1", "r2"}
  Home,     \* [Cards -> Rows]: the row each card is dealt to first
  Grace,    \* beat windows a working card may be absent from its row's live set
  MaxGen,   \* the bound on generations (a CONSTRAINT-free bound: actions stop at it)
  Broken    \* a set of the reversed witnesses above, {} for the design

ASSUME Home \in [Cards -> Rows] /\ Grace \in Nat /\ Grace >= 1 /\ MaxGen \in Nat

Cols   == {"ready", "working", "done"}
WState == {"none", "run", "cancelled", "held"}

VARIABLES
  col,       \* [Cards -> Cols]          the card's column on its row
  row,       \* [Cards -> Rows]          the row it is on
  gen,       \* [Cards -> 1..MaxGen]     its generation
  receipt,   \* [Cards -> 0..MaxGen]     stopped_from_gen: the generation a return left (0 none)
  wk,        \* [Cards -> WState]        the worker's side of the card
  wgen,      \* [Cards -> 0..MaxGen]     the generation the worker holds it at
  absent,    \* [Cards -> 0..Grace+1]    beat windows it has been working and not live
  running,   \* BOOLEAN                  the machine RUNNING
  debt,      \* SUBSET (Cards \X 1..MaxGen): the STOP's captured leases
  ticked,    \* BOOLEAN                  a tick ran since the last beat window
  finished,  \* [Cards -> BOOLEAN]       a child of it finished with a report
  accepted,  \* [Cards -> BOOLEAN]       the machine took that report
  stoppedRet \* BOOLEAN                  a STOPPED tick has returned a card (reachability)

vars == <<col, row, gen, receipt, wk, wgen, absent, running, debt, ticked, finished, accepted, stoppedRet>>

TypeOK ==
  /\ col \in [Cards -> Cols]
  /\ row \in [Cards -> Rows]
  /\ gen \in [Cards -> 1..MaxGen]
  /\ receipt \in [Cards -> 0..MaxGen]
  /\ wk \in [Cards -> WState]
  /\ wgen \in [Cards -> 0..MaxGen]
  /\ absent \in [Cards -> 0..(Grace + 1)]
  /\ running \in BOOLEAN
  /\ debt \subseteq (Cards \X (1..MaxGen))
  /\ ticked \in BOOLEAN
  /\ finished \in [Cards -> BOOLEAN]
  /\ accepted \in [Cards -> BOOLEAN]
  /\ stoppedRet \in BOOLEAN

Init ==
  /\ col = [c \in Cards |-> "ready"]
  /\ row = Home
  /\ gen = [c \in Cards |-> 1]
  /\ receipt = [c \in Cards |-> 0]
  /\ wk = [c \in Cards |-> "none"]
  /\ wgen = [c \in Cards |-> 0]
  /\ absent = [c \in Cards |-> 0]
  /\ running = TRUE
  /\ debt = {}
  /\ ticked = TRUE
  /\ finished = [c \in Cards |-> FALSE]
  /\ accepted = [c \in Cards |-> FALSE]
  /\ stoppedRet = FALSE

----------------------------------------------------------------------------
(* Derived *)

\* the row's live set as its beat carries it (sprint.Beat.Live): the cards it
\* runs and those it holds a finished report for, each at its generation
LiveOf(r) == {<<c, wgen[c]>> : c \in {d \in Cards : row[d] = r /\ wk[d] \in
               (IF "heldnotlive" \in Broken THEN {"run"} ELSE {"run", "held"})}}
Live(c) == <<c, gen[c]>> \in LiveOf(row[c])

\* the debt entries of c
DebtOf(c) == {d \in debt : d[1] = c}

\* stopOwned (machine_transition.go): a debt entry owns its card while the card
\* is still working at the generation captured; a stop-return or a reconcile
\* return bumps the generation, which settles it
Owned(c) ==
  IF "debtbyid" \in Broken THEN DebtOf(c) /= {}
  ELSE \E d \in DebtOf(c) : col[c] = "working" /\ gen[c] = d[2]

\* start's reading of the debt (unsettledStopDebt): an owned card holds the
\* start back unless its row's live set says its child finished and its report
\* is held at that generation (the report is taken once the machine runs)
BlocksStart(c) ==
  Owned(c) /\ ~(wk[c] = "held" /\ <<c, gen[c]>> \in LiveOf(row[c]))

\* a working card due back: absent for Grace beat windows
Due(c) == col[c] = "working" /\ absent[c] >= Grace

\* the tick returns while RUNNING, and while STOPPED (Broken: not while STOPPED)
Returns == running \/ "stoppednoreconcile" \notin Broken

CanGen(c) == gen[c] < MaxGen

----------------------------------------------------------------------------
(* The machine and the coordinator *)

\* take: RUNNING, a ready card on its row starts a child at its generation
Take(c) ==
  /\ running /\ col[c] = "ready" /\ wk[c] = "none"
  /\ col' = [col EXCEPT ![c] = "working"]
  /\ wk' = [wk EXCEPT ![c] = "run"]
  /\ wgen' = [wgen EXCEPT ![c] = gen[c]]
  /\ absent' = [absent EXCEPT ![c] = 0]
  /\ UNCHANGED <<row, gen, receipt, running, debt, ticked, finished, accepted, stoppedRet>>

\* STOP: the machine captures every working card at its generation
Stop ==
  /\ running
  /\ running' = FALSE
  /\ debt' = {<<c, gen[c]>> : c \in {d \in Cards : col[d] = "working"}}
  /\ UNCHANGED <<col, row, gen, receipt, wk, wgen, absent, ticked, finished, accepted, stoppedRet>>

\* START (unsettledStopDebt): no card STOP-owned any more; the debt is cleared
Start ==
  /\ ~running
  /\ \A c \in Cards : ~BlocksStart(c)
  /\ running' = TRUE
  /\ debt' = {}
  /\ UNCHANGED <<col, row, gen, receipt, wk, wgen, absent, ticked, finished, accepted, stoppedRet>>

\* a coordinator verb that moves a card (hold --return, give, take back,
\* rebalance): never a STOP-owned card (stopDebtMutation refuses it whole);
\* here a ready card moved to another row
CoordMove(c, r) ==
  /\ col[c] = "ready" /\ r /= row[c] /\ wk[c] = "none"
  /\ ~Owned(c)
  /\ row' = [row EXCEPT ![c] = r]
  /\ UNCHANGED <<col, gen, receipt, wk, wgen, absent, running, debt, ticked, finished, accepted, stoppedRet>>

\* the tick's reconcile (LiveReturns): every due card back to ready on its
\* row at its next generation, with its receipt; RUNNING or STOPPED
Tick ==
  LET due == {c \in Cards : Due(c) /\ CanGen(c)} IN
  /\ ticked' = TRUE
  /\ IF Returns
       THEN /\ col' = [c \in Cards |-> IF c \in due THEN "ready" ELSE col[c]]
            /\ gen' = [c \in Cards |-> IF c \in due THEN gen[c] + 1 ELSE gen[c]]
            /\ receipt' = [c \in Cards |-> IF c \in due THEN gen[c] ELSE receipt[c]]
            /\ absent' = [c \in Cards |-> IF c \in due THEN 0 ELSE absent[c]]
       ELSE UNCHANGED <<col, gen, receipt, absent>>
  /\ stoppedRet' = (stoppedRet \/ (Returns /\ ~running /\ due /= {}))
  /\ UNCHANGED <<row, wk, wgen, running, debt, finished, accepted>>

\* one beat window of row r: each working card of r out of its live set
\* counts one more window absent, each in it none; a tick ran since the last
Beat(r) ==
  /\ ticked
  /\ ticked' = FALSE
  /\ absent' = [c \in Cards |->
                  IF row[c] = r /\ col[c] = "working"
                    THEN IF Live(c) THEN 0 ELSE IF absent[c] > Grace THEN absent[c] ELSE absent[c] + 1
                    ELSE absent[c]]
  /\ UNCHANGED <<col, row, gen, receipt, wk, wgen, running, debt, finished, accepted, stoppedRet>>

----------------------------------------------------------------------------
(* The worker *)

\* the child dies with nothing reported (a crash, a member restart, a harness
\* gone): the take whose run is gone
ChildDies(c) ==
  /\ wk[c] = "run"
  /\ wk' = [wk EXCEPT ![c] = "none"]
  /\ UNCHANGED <<col, row, gen, receipt, wgen, absent, running, debt, ticked, finished, accepted, stoppedRet>>

\* the child finishes: its report waits in the outbox
ChildFinishes(c) ==
  /\ wk[c] = "run"
  /\ wk' = [wk EXCEPT ![c] = "held"]
  /\ finished' = [finished EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<col, row, gen, receipt, wgen, absent, running, debt, ticked, accepted, stoppedRet>>

\* the outbox sends the report: RUNNING and still working at its generation,
\* the machine takes it; STOPPED, it is refused and kept (outbox.go: sent
\* again every OutboxRetry); at another generation it is stale and dropped
Report(c) ==
  /\ wk[c] = "held"
  /\ \/ /\ running /\ col[c] = "working" /\ gen[c] = wgen[c]
        /\ col' = [col EXCEPT ![c] = "done"]
        /\ accepted' = [accepted EXCEPT ![c] = TRUE]
        /\ wk' = [wk EXCEPT ![c] = "none"]
        /\ UNCHANGED <<row, gen, receipt, wgen, absent, running, debt, ticked, finished, stoppedRet>>
     \/ /\ ~running /\ gen[c] = wgen[c]
        /\ UNCHANGED vars
     \/ /\ gen[c] /= wgen[c]
        /\ wk' = [wk EXCEPT ![c] = "none"]
        /\ UNCHANGED <<col, row, gen, receipt, wgen, absent, running, debt, ticked, finished, accepted, stoppedRet>>

\* the worker sees the STOP: each child it runs is cancelled and owed a
\* stop-return; a held report is kept (no stop-return for it)
WorkerStops(c) ==
  /\ ~running /\ wk[c] = "run"
  /\ wk' = [wk EXCEPT ![c] = "cancelled"]
  /\ UNCHANGED <<col, row, gen, receipt, wgen, absent, running, debt, ticked, finished, accepted, stoppedRet>>

\* stop-return (StopReturn): the cancelled child ended; working -> ready at the
\* next generation with the receipt; STOPPED only
StopReturn(c) ==
  /\ ~running /\ wk[c] = "cancelled"
  /\ wk' = [wk EXCEPT ![c] = "none"]
  /\ IF col[c] = "working" /\ gen[c] = wgen[c] /\ CanGen(c)
       THEN /\ col' = [col EXCEPT ![c] = "ready"]
            /\ gen' = [gen EXCEPT ![c] = @ + 1]
            /\ receipt' = [receipt EXCEPT ![c] = gen[c]]
            /\ absent' = [absent EXCEPT ![c] = 0]
       ELSE UNCHANGED <<col, gen, receipt, absent>>
  /\ UNCHANGED <<row, wgen, running, debt, ticked, finished, accepted, stoppedRet>>

----------------------------------------------------------------------------

Next ==
  \/ Stop \/ Start \/ Tick
  \/ \E r \in Rows : Beat(r)
  \/ \E c \in Cards : Take(c) \/ ChildDies(c) \/ ChildFinishes(c) \/ Report(c)
                      \/ WorkerStops(c) \/ StopReturn(c)
  \/ \E c \in Cards, r \in Rows : CoordMove(c, r)

Spec == Init /\ [][Next]_vars

----------------------------------------------------------------------------
(* What must always hold *)

\* working is a live run within the grace: a card working on a row is out of
\* that row's live set for at most Grace beat windows (the reconcile, RUNNING
\* or STOPPED, returns it before the next window counts)
WorkingIsLive ==
  \A c \in Cards : col[c] = "working" /\ CanGen(c) => absent[c] <= Grace

\* a STOP marker (the debt as stopDebtMutation and start read it) implies its
\* card is working: a returned card is never STOP-owned
StopMarkerIsWorking ==
  \A c \in Cards : Owned(c) => col[c] = "working"

\* a STOP-owned card leaves working only by a return that leaves its receipt
\* (a stop-return or the reconcile), never by a coordinator's move
OwnedLeavesWithReceipt ==
  \A d \in debt : (col[d[1]] = "working" /\ gen[d[1]] = d[2]) \/ receipt[d[1]] >= d[2]

\* no finished report is lost: each is taken, or still held at a generation
\* the machine will take it at
NoReportLost ==
  \A c \in Cards : finished[c] =>
    \/ accepted[c]
    \/ wk[c] = "held" /\ gen[c] = wgen[c] /\ col[c] = "working"
    \* a later run of the same card finished after it: that report is the card's
    \/ wk[c] \in {"run", "cancelled"}

\* a child the worker runs is the card's live take: the machine never returns
\* a card whose child still runs
NoRunReturned ==
  \A c \in Cards : wk[c] = "run" => col[c] = "working" /\ gen[c] = wgen[c]

\* reachability (expected violated): a STOPPED tick returns a dead take
NeverStoppedReturn == ~stoppedRet

Safety == TypeOK /\ WorkingIsLive /\ StopMarkerIsWorking /\ OwnedLeavesWithReceipt
          /\ NoReportLost /\ NoRunReturned
=============================================================================
