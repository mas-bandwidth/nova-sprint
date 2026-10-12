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
(* report waits in the outbox for the machine to take it). The row's live  *)
(* set is the cards its last pass left running or held (live), not the     *)
(* worker state itself: a take names a card only at the next pass, and     *)
(* LiveReturns covers that lag with the grace from the take. A row in      *)
(* Legacy (a worker from before v1.2.6) carries no live set at all, and    *)
(* the reconcile skips it. A beat window passes per row (Beat): each       *)
(* working card of the row not in its live set counts one more window      *)
(* absent. The tick (Tick) returns every working card absent for Grace     *)
(* windows to ready at its next generation with its receipt, RUNNING or    *)
(* STOPPED; a tick runs between two beat windows (the tick is every        *)
(* second, a beat window 15 s): Beat waits for it.                         *)
(*                                                                         *)
(* Reversed witnesses (Broken):                                            *)
(*   "stoppednoreconcile" the v1.2.5 tick: a STOPPED tick returns nothing  *)
(*                        (breaks WorkingIsLive);                          *)
(*   "legacyreturned"     the reconcile reads a Legacy row's missing live  *)
(*                        set as empty: it returns its working cards       *)
(*                        (breaks LegacyNeverReturned);                    *)
(*   "nograce"            the reconcile drops the grace from the take: it  *)
(*                        returns a card on its first window unnamed,      *)
(*                        while its child still runs (breaks               *)
(*                        NoRunReturned);                                  *)
(*   "debtbyid"           the v1.2.5 debt: a card is STOP-owned while its  *)
(*                        id is in the debt, returned or not (breaks       *)
(*                        StopMarkerIsWorking);                            *)
(*   "heldnotlive"        a live set without the held reports: the tick   *)
(*                        returns a finished card and its report's         *)
(*                        generation is gone (breaks NoReportLost);        *)
(*   "staleasabsent"      a silent row's stale beat read as an empty live  *)
(*                        set: the tick returns a card whose child still   *)
(*                        runs (breaks NoRunReturned; the first cut of     *)
(*                        v1.2.6, found by its cold read);                 *)
(*   "debtnotsettled"     the STOPPED reconcile returns a card but leaves  *)
(*                        its receipt in the debt (breaks DebtIsOwed):     *)
(*                        clear read the raw debt and refused until start  *)
(*                        (the stale STOP of 2026-10-10 that this fix      *)
(*                        removes);                                        *)
(*   "settlebycount"      SettleByBeat reads the row's stop-returns count  *)
(*                        instead of its fresh live set (the code before   *)
(*                        #87 for a machine lease): a finished card whose  *)
(*                        report is held is settled and dealt again, and   *)
(*                        its held report's generation is gone (breaks     *)
(*                        NoReportLost).                                   *)
(*                                                                         *)
(* SettleByBeat (settleStopDebtByBeat, machine_transition.go:288-336) is   *)
(* the settle on START's path. A machine lease stays owed while the row's  *)
(* stop-returns count is above zero (:324), here a cancelled child on the  *)
(* row not yet stop-returned; at zero, a row whose beat carries a live set *)
(* keeps the lease owed while that set names the card running or held     *)
(* (:326, sprint.LiveNamedAt). A Legacy row's beat carries no live set, so *)
(* LiveNamedAt is false and its lease is settled by the count alone, as    *)
(* before #87 (live.go:268-269: "its count is read as before"). The settle *)
(* therefore returns a Legacy row's card whose report is held or whose     *)
(* child has not yet seen the STOP: NoReportLost and NoRunReturned claim   *)
(* nothing about a Legacy row (as WorkingIsLive does not), and             *)
(* MCLiveRunsSettleByLegacy checks their every-row forms, NoReportLostAll  *)
(* and NoRunReturnedAll, expecting the violation: the Legacy path's loss,  *)
(* a v1.2.14 candidate, not fixed here (models only).                      *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
  Cards,    \* e.g. {"c1", "c2"}
  Rows,     \* e.g. {"r1", "r2"}
  Home,     \* [Cards -> Rows]: the row each card is dealt to first
  Grace,    \* beat windows a working card may be absent from its row's live set
  MaxGen,   \* the bound on generations (a CONSTRAINT-free bound: actions stop at it)
  Broken,   \* a set of the reversed witnesses above, {} for the design
  Legacy    \* the rows whose beat carries no live set (workers from before v1.2.6)

ASSUME Home \in [Cards -> Rows] /\ Grace \in Nat /\ Grace >= 1 /\ MaxGen \in Nat
ASSUME Legacy \subseteq Rows

Cols   == {"ready", "working", "done"}
WState == {"none", "run", "cancelled", "held"}

VARIABLES
  col,       \* [Cards -> Cols]          the card's column on its row
  row,       \* [Cards -> Rows]          the row it is on
  gen,       \* [Cards -> 1..MaxGen]     its generation
  receipt,   \* [Cards -> 0..MaxGen]     stopped_from_gen: the generation a return left (0 none)
  wk,        \* [Cards -> WState]        the worker's side of the card
  wgen,      \* [Cards -> 0..MaxGen]     the generation the worker holds it at
  frow,      \* [Cards -> Rows]          the row its last finished report was made on (a ghost)
  live,      \* [Cards -> 0..MaxGen]     the generation its beat's live set names it at (0 none)
  absent,    \* [Cards -> 0..Grace+1]    beat windows it has been working and not live
  running,   \* BOOLEAN                  the machine RUNNING
  debt,      \* SUBSET (Cards \X 1..MaxGen): the STOP's captured leases
  ticked,    \* BOOLEAN                  a tick ran since the last beat window
  finished,  \* [Cards -> BOOLEAN]       a child of it finished with a report
  accepted,  \* [Cards -> BOOLEAN]       the machine took that report
  stoppedRet, \* BOOLEAN                 a STOPPED tick has returned a card (reachability)
  silent,    \* [Rows -> BOOLEAN]        the row's beats do not arrive (a partition, a hung
             \*                          beat, a store stall); its children run on
  beaten     \* SUBSET Rows              the rows that have beaten since the STOP

vars == <<col, row, gen, receipt, wk, wgen, frow, live, absent, running, debt, ticked, finished, accepted, stoppedRet, silent, beaten>>

TypeOK ==
  /\ col \in [Cards -> Cols]
  /\ row \in [Cards -> Rows]
  /\ gen \in [Cards -> 1..MaxGen]
  /\ receipt \in [Cards -> 0..MaxGen]
  /\ wk \in [Cards -> WState]
  /\ wgen \in [Cards -> 0..MaxGen]
  /\ live \in [Cards -> 0..MaxGen]
  /\ absent \in [Cards -> 0..(Grace + 1)]
  /\ running \in BOOLEAN
  /\ debt \subseteq (Cards \X (1..MaxGen))
  /\ ticked \in BOOLEAN
  /\ finished \in [Cards -> BOOLEAN]
  /\ accepted \in [Cards -> BOOLEAN]
  /\ frow \in [Cards -> Rows]
  /\ stoppedRet \in BOOLEAN
  /\ silent \in [Rows -> BOOLEAN]
  /\ beaten \subseteq Rows

Init ==
  /\ col = [c \in Cards |-> "ready"]
  /\ row = Home
  /\ gen = [c \in Cards |-> 1]
  /\ receipt = [c \in Cards |-> 0]
  /\ wk = [c \in Cards |-> "none"]
  /\ wgen = [c \in Cards |-> 0]
  /\ live = [c \in Cards |-> 0]
  /\ absent = [c \in Cards |-> 0]
  /\ running = TRUE
  /\ debt = {}
  /\ ticked = TRUE
  /\ finished = [c \in Cards |-> FALSE]
  /\ accepted = [c \in Cards |-> FALSE]
  /\ frow = Home
  /\ stoppedRet = FALSE
  /\ silent = [r \in Rows |-> FALSE]
  /\ beaten = {}

----------------------------------------------------------------------------
(* Derived *)

\* the row's live set as its beat carries it (sprint.Beat.Live): the cards its
\* last pass left, each at the generation the pass saw (live), none before the
\* first pass and none for a row in Legacy (whose beat carries no live set).
LiveOf(r) == {<<c, live[c]>> : c \in {d \in Cards : row[d] = r /\ live[d] > 0}}
Live(c) == live[c] = gen[c]

\* the card a row's pass leaves live at, at the generation the pass saw (wgen):
\* a running child or a held report. Broken "heldnotlive" leaves the held
\* reports out (a live set without them). A Legacy row carries no live set at
\* all, so its pass leaves none.
PassLive(c) ==
  IF wk[c] \in (IF "heldnotlive" \in Broken THEN {"run"} ELSE {"run", "held"})
    THEN wgen[c]
    ELSE 0

\* the debt entries of c
DebtOf(c) == {d \in debt : d[1] = c}

\* a return settles its debt entry off the machine record (engine.go after,
\* settleStopDebt): the STOPPED reconcile (Tick) and stop-return both do, so
\* clear reads a debt that is owed alone. Broken "debtnotsettled" is the
\* reconcile that returns a card but leaves its receipt in the debt (the stale
\* STOP of 2026-10-10 that blocked clear); "debtbyid" is the v1.2.5 debt
\* matched by id, settled by nothing until START.
TickSettles    == "debtnotsettled" \notin Broken /\ "debtbyid" \notin Broken
ReturnSettles  == "debtbyid" \notin Broken

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

\* a working card due back: absent for the grace since its take or last
\* sighting (LiveReturns). Broken "nograce" drops the grace, returning a card
\* on its first window unnamed (the take's own lag), so a child still runs.
Due(c) == col[c] = "working" /\ absent[c] >= (IF "nograce" \in Broken THEN 1 ELSE Grace)

CanGen(c) == gen[c] < MaxGen

\* the reconcile returns c: the code skips a row whose beat carries no live set
\* (Legacy, liveStopped's early return); Broken "legacyreturned" reconciles it
\* as an empty set instead, so its working cards go unnamed and return.
Reconciles(c) == Due(c) /\ CanGen(c) /\ (row[c] \notin Legacy \/ "legacyreturned" \in Broken)

\* the tick returns while RUNNING, and while STOPPED (Broken: not while STOPPED)
Returns == running \/ "stoppednoreconcile" \notin Broken

----------------------------------------------------------------------------
(* The machine and the coordinator *)

\* take: RUNNING, a ready card on its row starts a child at its generation.
\* It is not live yet: the member's beat carries the live set its last pass
\* left (pkg/member/member.go Beat), and the pass that names this take has not
\* run; the grace from the take covers the lag (LiveReturns reads since the
\* take).
Take(c) ==
  /\ running /\ col[c] = "ready" /\ wk[c] = "none"
  /\ col' = [col EXCEPT ![c] = "working"]
  /\ wk' = [wk EXCEPT ![c] = "run"]
  /\ wgen' = [wgen EXCEPT ![c] = gen[c]]
  /\ absent' = [absent EXCEPT ![c] = 0]
  /\ UNCHANGED <<row, gen, receipt, frow, live, running, debt, ticked, finished, accepted, stoppedRet, silent, beaten>>

\* STOP: the machine captures every working card at its generation
Stop ==
  /\ running
  /\ running' = FALSE
  /\ debt' = {<<c, gen[c]>> : c \in {d \in Cards : col[d] = "working"}}
  /\ beaten' = {}
  /\ UNCHANGED <<col, row, gen, receipt, wk, wgen, frow, live, absent, ticked, finished, accepted, stoppedRet, silent>>

\* START (unsettledStopDebt): no card STOP-owned any more; the debt is cleared
Start ==
  /\ ~running
  /\ \A c \in Cards : ~BlocksStart(c)
  /\ running' = TRUE
  /\ debt' = {}
  /\ UNCHANGED <<col, row, gen, receipt, wk, wgen, frow, live, absent, ticked, finished, accepted, stoppedRet, silent, beaten>>

\* a coordinator verb that moves a card (hold --return, give, take back,
\* rebalance): never a STOP-owned card (stopDebtMutation refuses it whole);
\* here a ready card moved to another row
CoordMove(c, r) ==
  /\ col[c] = "ready" /\ r /= row[c] /\ wk[c] = "none"
  /\ ~Owned(c)
  /\ row' = [row EXCEPT ![c] = r]
  /\ UNCHANGED <<col, gen, receipt, wk, wgen, frow, live, absent, running, debt, ticked, finished, accepted, stoppedRet, silent, beaten>>

\* the tick's reconcile (LiveReturns): every card it reconciles back to ready
\* on its row at its next generation, with its receipt; RUNNING or STOPPED. A
\* Legacy row is skipped (its beat carries no live set).
Tick ==
  LET due == {c \in Cards : Reconciles(c)} IN
  /\ ticked' = TRUE
  /\ IF Returns
       THEN /\ col' = [c \in Cards |-> IF c \in due THEN "ready" ELSE col[c]]
            /\ gen' = [c \in Cards |-> IF c \in due THEN gen[c] + 1 ELSE gen[c]]
            /\ receipt' = [c \in Cards |-> IF c \in due THEN gen[c] ELSE receipt[c]]
            /\ absent' = [c \in Cards |-> IF c \in due THEN 0 ELSE absent[c]]
       ELSE UNCHANGED <<col, gen, receipt, absent>>
  /\ stoppedRet' = (stoppedRet \/ (Returns /\ ~running /\ due /= {}))
  /\ debt' = IF Returns /\ TickSettles THEN debt \ {<<c, gen[c]>> : c \in due} ELSE debt
  /\ UNCHANGED <<row, wk, wgen, frow, live, running, finished, accepted, silent, beaten>>

\* one beat window of row r: each working card of r out of its live set
\* counts one more window absent, each in it none; a tick ran since the last,
\* and the row's pass has left the live set the next beat carries (live). A
\* silent row's window carries no beat: no evidence either way, so nothing
\* counts (LiveReturns judges only a fresh beat; Broken "staleasabsent", the
\* first cut, read a stale beat as an empty live set). A Legacy row's beat
\* carries no live set, so its working cards are never unnamed: the reconcile
\* skips them (Broken "legacyreturned" reads an empty set instead and counts).
\* A beat while STOPPED marks the row beaten since the STOP
\* (settleStopDebtByBeat's Beaten).
Beat(r) ==
  /\ ticked
  /\ ticked' = FALSE
  /\ absent' = [c \in Cards |->
                  IF row[c] = r /\ col[c] = "working"
                    THEN IF silent[r]
                           THEN IF "staleasabsent" \in Broken /\ absent[c] <= Grace THEN absent[c] + 1 ELSE absent[c]
                           ELSE IF r \in Legacy /\ "legacyreturned" \notin Broken
                                  THEN absent[c]
                                  ELSE IF Live(c) THEN 0 ELSE IF absent[c] > Grace THEN absent[c] ELSE absent[c] + 1
                    ELSE absent[c]]
  /\ live' = [c \in Cards |->
                IF row[c] = r /\ ~silent[r] /\ r \notin Legacy THEN PassLive(c) ELSE live[c]]
  /\ beaten' = IF ~running /\ ~silent[r] THEN beaten \cup {r} ELSE beaten
  /\ UNCHANGED <<col, row, gen, receipt, wk, wgen, frow, running, debt, finished, accepted, stoppedRet, silent>>

\* the row's beats stop arriving, or arrive again; its children are untouched
Silence(r) ==
  /\ ~silent[r]
  /\ silent' = [silent EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<col, row, gen, receipt, wk, wgen, frow, live, absent, running, debt, ticked, finished, accepted, stoppedRet, beaten>>
Speak(r) ==
  /\ silent[r]
  /\ silent' = [silent EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<col, row, gen, receipt, wk, wgen, frow, live, absent, running, debt, ticked, finished, accepted, stoppedRet, beaten>>

---------------------------------------------------------------------------
(* The worker *)

\* the child dies with nothing reported (a crash, a member restart, a harness
\* gone): the take whose run is gone
ChildDies(c) ==
  /\ wk[c] = "run"
  /\ wk' = [wk EXCEPT ![c] = "none"]
  /\ UNCHANGED <<col, row, gen, receipt, wgen, frow, live, absent, running, debt, ticked, finished, accepted, stoppedRet, silent, beaten>>

\* the child finishes: its report waits in the outbox
ChildFinishes(c) ==
  /\ wk[c] = "run"
  /\ wk' = [wk EXCEPT ![c] = "held"]
  /\ finished' = [finished EXCEPT ![c] = TRUE]
  /\ frow' = [frow EXCEPT ![c] = row[c]]
  /\ UNCHANGED <<col, row, gen, receipt, wgen, live, absent, running, debt, ticked, accepted, stoppedRet, silent, beaten>>

\* the outbox sends the report: RUNNING and still working at its generation,
\* the machine takes it; STOPPED, it is refused and kept (outbox.go: sent
\* again every OutboxRetry); at another generation it is stale and dropped
Report(c) ==
  /\ wk[c] = "held"
  /\ \/ /\ running /\ col[c] = "working" /\ gen[c] = wgen[c]
        /\ col' = [col EXCEPT ![c] = "done"]
        /\ accepted' = [accepted EXCEPT ![c] = TRUE]
        /\ wk' = [wk EXCEPT ![c] = "none"]
        /\ UNCHANGED <<row, gen, receipt, wgen, frow, live, absent, running, debt, ticked, finished, stoppedRet, silent, beaten>>
     \/ /\ ~running /\ gen[c] = wgen[c]
        /\ UNCHANGED vars
     \/ /\ gen[c] /= wgen[c]
        /\ wk' = [wk EXCEPT ![c] = "none"]
        /\ UNCHANGED <<col, row, gen, receipt, wgen, frow, live, absent, running, debt, ticked, finished, accepted, stoppedRet, silent, beaten>>

\* the worker sees the STOP: each child it runs is cancelled and owed a
\* stop-return; a held report is kept (no stop-return for it)
WorkerStops(c) ==
  /\ ~running /\ wk[c] = "run"
  /\ wk' = [wk EXCEPT ![c] = "cancelled"]
  /\ UNCHANGED <<col, row, gen, receipt, wgen, frow, live, absent, running, debt, ticked, finished, accepted, stoppedRet, silent, beaten>>

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
  /\ debt' = IF ReturnSettles /\ col[c] = "working" /\ gen[c] = wgen[c] /\ CanGen(c)
              THEN debt \ {<<c, gen[c]>>}
              ELSE debt
  /\ UNCHANGED <<row, wgen, frow, live, running, ticked, finished, accepted, stoppedRet, silent, beaten>>

\* the server's settle on START's path (settleStopDebtByBeat): a STOP-owned
\* card whose row has beaten since the STOP is returned to ready at its next
\* generation with its receipt, without the owner's own stop-return, when the
\* row's stop-returns count is zero (no cancelled child of the row still owed:
\* machine_transition.go:324) and, for a row whose beat carries a live set, its
\* last beat's set does not name the card running or held (LiveNamedAt, :326).
\* A Legacy row's beat names nothing: its lease is settled by the count alone.
\* Broken "settlebycount" drops the live-set reading for every row: the code
\* before #87 for a machine lease.
CountZero(r) == \A d \in Cards : row[d] = r => wk[d] /= "cancelled"
SettleByBeat(c) ==
  /\ ~running
  /\ Owned(c)
  /\ row[c] \in beaten
  /\ CountZero(row[c])
  /\ (row[c] \in Legacy \/ "settlebycount" \in Broken \/ ~Live(c))
  /\ CanGen(c)
  /\ col' = [col EXCEPT ![c] = "ready"]
  /\ gen' = [gen EXCEPT ![c] = gen[c] + 1]
  /\ receipt' = [receipt EXCEPT ![c] = gen[c]]
  /\ absent' = [absent EXCEPT ![c] = 0]
  /\ debt' = debt \ {<<c, gen[c]>>}
  /\ UNCHANGED <<row, wk, wgen, frow, live, running, ticked, finished, accepted, stoppedRet, silent, beaten>>

----------------------------------------------------------------------------

Next ==
  \/ Stop \/ Start \/ Tick
  \/ \E r \in Rows : Beat(r) \/ Silence(r) \/ Speak(r)
  \/ \E c \in Cards : Take(c) \/ ChildDies(c) \/ ChildFinishes(c) \/ Report(c)
                      \/ WorkerStops(c) \/ StopReturn(c) \/ SettleByBeat(c)
  \/ \E c \in Cards, r \in Rows : CoordMove(c, r)

Spec == Init /\ [][Next]_vars

----------------------------------------------------------------------------
(* What must always hold *)

\* working is a live run within the grace: a card working on a row whose beat
\* carries a live set is out of that set for at most Grace beat windows (the
\* reconcile, RUNNING or STOPPED, returns it before the next window counts). A
\* Legacy row carries no live set, so nothing about it is claimed here.
WorkingIsLive ==
  \A c \in Cards : col[c] = "working" /\ CanGen(c) /\ row[c] \notin Legacy => absent[c] <= Grace

\* no working card of a Legacy row is returned by the reconcile: its beat
\* carries no live set, so the reconcile skips it and its absence never counts
\* (liveStopped returns early; LiveCountKeys counts every such card running).
\* Broken "legacyreturned" reconciles the row as an empty set instead.
LegacyNeverReturned ==
  \A c \in Cards : row[c] \in Legacy /\ col[c] = "working" => absent[c] < Grace

\* a STOP marker (the debt as stopDebtMutation and start read it) implies its
\* card is working: a returned card is never STOP-owned
StopMarkerIsWorking ==
  \A c \in Cards : Owned(c) => col[c] = "working"

\* every debt entry still owns its card working at the generation it captured:
\* a return settles its entry off the record (the reconcile and stop-return), so
\* clear reads a debt that is owed alone. Broken "debtnotsettled" leaves the
\* reconcile's returned card in the debt.
DebtIsOwed ==
  \A d \in debt : col[d[1]] = "working" /\ gen[d[1]] = d[2]

\* a STOP-owned card leaves working only by a return that leaves its receipt
\* (a stop-return or the reconcile), never by a coordinator's move
OwnedLeavesWithReceipt ==
  \A d \in debt : (col[d[1]] = "working" /\ gen[d[1]] = d[2]) \/ receipt[d[1]] >= d[2]

\* no finished report is lost: each is taken, or still held at a generation
\* the machine will take it at
ReportKept(c) ==
  finished[c] =>
    \/ accepted[c]
    \/ wk[c] = "held" /\ gen[c] = wgen[c] /\ col[c] = "working"
    \* a later run of the same card finished after it: that report is the card's
    \/ wk[c] \in {"run", "cancelled"}

\* a child the worker runs is the card's live take: the machine never returns
\* a card whose child still runs
RunKept(c) == wk[c] = "run" => col[c] = "working" /\ gen[c] = wgen[c]

\* over the rows whose beat carries a live set: a Legacy row's lease is settled
\* by its stop-returns count alone (SettleByBeat), so nothing is claimed here
\* about a report finished on a Legacy row (frow, the row its last report was
\* made on) or a child running on one, as WorkingIsLive claims nothing about it
NoReportLost == \A c \in Cards : frow[c] \notin Legacy => ReportKept(c)
NoRunReturned == \A c \in Cards : row[c] \notin Legacy => RunKept(c)

\* the every-row forms (expected violated on MCLiveRunsSettleByLegacy: a Legacy
\* row's held report or not-yet-cancelled child settled by count, a v1.2.14
\* candidate)
NoReportLostAll == \A c \in Cards : ReportKept(c)
NoRunReturnedAll == \A c \in Cards : RunKept(c)

\* reachability (expected violated): a STOPPED tick returns a dead take
NeverStoppedReturn == ~stoppedRet

Safety == TypeOK /\ WorkingIsLive /\ LegacyNeverReturned /\ StopMarkerIsWorking
          /\ OwnedLeavesWithReceipt /\ DebtIsOwed /\ NoReportLost /\ NoRunReturned
=============================================================================
