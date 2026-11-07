-------------------------- MODULE CardMachineTrace --------------------------
(***************************************************************************)
(* Trace validation of the card machine: CardMachine.tla's actions         *)
(* constrained to a recording of the Go machine on the store (the shape    *)
(* of StepsTrace.tla and StoreTrace.tla). The card walk (coldWalk in       *)
(* card/coldread_test.go, recorded by card/trace_test.go) writes the       *)
(* module CardMachineTraceLog, which defines                               *)
(*                                                                         *)
(*   TProds, TSentinels, TAdded, TRows, TRowIdx, TNeeds0, TAfter0: the     *)
(*     walk's world, overriding the model's scenario in the cfg (which     *)
(*     also sets K, MaxHead, MaxAttempt and MaxBounce above the walk's);   *)
(*   TraceLog == << r_1, ..., r_n >>: one record per accepted op.          *)
(*                                                                         *)
(* r_1 is Init: the world's producers (each NewProd, which the trace       *)
(* checks) and its rows, which start as the walk's world has them (r1 at   *)
(* width 2) and not at Init's width 1; that is the one way TraceInit       *)
(* differs from Init. Each later record names a move of the model with its *)
(* arguments (p, c, r, h, res, n, to, by, s, lv, rows) and carries ps, cs, *)
(* rs and held after it, projected by TraceState (card/trace.go, the       *)
(* refinement mapping): for ps, cs and rs the entries that changed since   *)
(* the record before (an entry not named did not change).                  *)
(*                                                                         *)
(* The moves are the model's actions, called with the record's arguments  *)
(* (not through Next, whose quantifiers range over the model's small       *)
(* bounds). Three things are the trace's own, each a bound of the model    *)
(* lifted or a move outside the machine:                                   *)
(*   - env is raised on every environment move and never bounded;          *)
(*   - Gone at a row that already flapped (GoneAt): the model stops flaps  *)
(*     at 1, the code counts on;                                           *)
(*   - RawHold: the walk's raw stream hold (layer 3's SetStreamHeld, to    *)
(*     exercise Park), which parks nothing; HoldStream is the machine's.   *)
(*                                                                         *)
(* l is the index of the last record matched; a refused move leaves the   *)
(* trace at l and TraceMatched is violated (TLC exit 13), so record l + 1 *)
(* is the step the model refuses.                                          *)
(***************************************************************************)
EXTENDS CardMachine, CardMachineTraceLog, Sequences, TLC

VARIABLE l

traceVars == <<vars, l>>

CardMoves == {"Resolve", "Park", "Reached", "Start", "Finished", "Broken", "Faulted", "Released",
              "AskRead", "AskReads", "ReadYes", "ReadFix", "WaiveRed", "Landed", "Cancel", "Replace",
              "NeedFailed", "Waive", "Relink", "Block", "Unblock", "Take", "Report", "Withdraw"}
EnvMoves  == {"Edit", "Rescore", "SetPriority", "Red", "Seen", "Gone", "Width", "Hold", "Unhold",
              "HoldStream", "UnholdStream", "RawHold"}
TraceActions == {"Init"} \cup CardMoves \cup EnvMoves

Has(r, f) == f \in DOMAIN r

\* Gone with the model's flap bound lifted: past 1 the code counts on.
GoneAt(r) ==
  IF rs[r].flaps < 1 THEN Gone(r)
  ELSE /\ rs[r].st = "up"
       /\ RStep("Gone", r, [rs[r] EXCEPT !.st = "down", !.flaps = @ + 1])

\* The walk's raw stream hold: the flag alone, no park.
RawHold ==
  /\ ~held
  /\ held' = TRUE
  /\ UNCHANGED <<ps, cs, rs>>
  /\ last' = L("HoldStream", NoP, NoC, NoR)

\* The model's move a record names, with its arguments.
CardStep(r) ==
  CASE r.action = "Resolve"    -> Resolve(r.p)
    [] r.action = "Park"       -> Park(r.p)
    [] r.action = "Reached"    -> Reached(r.p)
    [] r.action = "Start"      -> Start(r.p, r.r)
    [] r.action = "Finished"   -> Finished(r.p, r.c)
    [] r.action = "Broken"     -> Broken(r.p, r.c)
    [] r.action = "Faulted"    -> Faulted(r.p, r.c)
    [] r.action = "Released"   -> Released(r.p, r.c)
    [] r.action = "AskRead"    -> AskRead(r.p, r.r)
    [] r.action = "AskReads"   -> AskReads(r.p, r.rows)
    [] r.action = "ReadYes"    -> ReadYes(r.p)
    [] r.action = "ReadFix"    -> ReadFix(r.p)
    [] r.action = "WaiveRed"   -> WaiveRed(r.p)
    [] r.action = "Landed"     -> Landed(r.p, r.h)
    [] r.action = "Cancel"     -> Cancel(r.p)
    [] r.action = "Replace"    -> Replace(r.p, r.by)
    [] r.action = "NeedFailed" -> NeedFailed(r.p, r.n)
    [] r.action = "Waive"      -> Waive(r.p, r.n)
    [] r.action = "Relink"     -> Relink(r.p, r.n, r.to)
    [] r.action = "Block"      -> Block(r.p)
    [] r.action = "Unblock"    -> Unblock(r.p)
    [] r.action = "Take"       -> Take(r.c, r.r)
    [] r.action = "Report"     -> Report(r.c, r.r, r.res, r.h)
    [] r.action = "Withdraw"   -> Withdraw(r.c, r.h)
    [] OTHER                   -> FALSE

EnvStep(r) ==
  CASE r.action = "Edit"         -> Edit(r.p)
    [] r.action = "Rescore"      -> Rescore(r.p, r.s)
    [] r.action = "SetPriority"  -> SetPriority(r.p, r.lv)
    [] r.action = "Red"          -> Red(r.p, r.h)
    [] r.action = "Seen"         -> Seen(r.r)
    [] r.action = "Gone"         -> GoneAt(r.r)
    [] r.action = "Width"        -> Width(r.r, r.n)
    [] r.action = "Hold"         -> Hold(r.r)
    [] r.action = "Unhold"       -> Unhold(r.r)
    [] r.action = "HoldStream"   -> HoldStream
    [] r.action = "UnholdStream" -> UnholdStream
    [] r.action = "RawHold"      -> RawHold
    [] OTHER                     -> FALSE

StepAs(r) ==
  IF r.action \in CardMoves THEN CardStep(r) /\ UNCHANGED env
  ELSE EnvStep(r) /\ env' = env + 1

\* The value a record gives entry k: its own, else old's (unchanged).
Upd(d, old, k) == IF k \in DOMAIN d THEN d[k] ELSE old[k]

LoggedNext(r) ==
  /\ \A p \in P    : ps'[p] = Upd(r.ps, ps, p)
  /\ \A c \in CIds : cs'[c] = Upd(r.cs, cs, c)
  /\ \A x \in Rows : rs'[x] = Upd(r.rs, rs, x)
  /\ held' = r.held

TraceInit ==
  /\ l = 1
  /\ TraceLog[1].action = "Init"
  /\ ps   = [p \in P |-> NewProd(p)]
  /\ cs   = [c \in CIds |-> NoCons]
  /\ rs   = [x \in Rows |-> TraceLog[1].rs[x]]
  /\ held = FALSE
  /\ last = [ev |-> "Init", bad |-> {}]
  /\ env  = 0
  /\ \A p \in P : ps[p] = TraceLog[1].ps[p]
  /\ DOMAIN TraceLog[1].cs = {}
  /\ held = TraceLog[1].held

TraceNext ==
  /\ l < Len(TraceLog)
  /\ l' = l + 1
  /\ StepAs(TraceLog[l + 1])
  /\ LoggedNext(TraceLog[l + 1])

TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)

\* The log is well formed: indices 1..n in order, known actions.
TraceIndexed ==
  \A k \in 1..Len(TraceLog) :
    /\ Has(TraceLog[k], "i") /\ TraceLog[k].i = k
    /\ Has(TraceLog[k], "action") /\ TraceLog[k].action \in TraceActions
    /\ (k = 1) = (TraceLog[k].action = "Init")
ASSUME TraceIndexed

\* Every record matched: violated exactly when a move the model refuses
\* (or a projected state it cannot reach) stops the trace at l.
TraceMatched == <>(l = Len(TraceLog))
=============================================================================
