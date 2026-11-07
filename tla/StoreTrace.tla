----------------------------- MODULE StoreTrace -----------------------------
(***************************************************************************)
(* Trace validation of the store: Store.tla's next-state relation          *)
(* constrained to a recording of the Go store (the shape of               *)
(* StepsTrace.tla). A walk in store/trace_test.go applies ops to the Fake *)
(* or to PostgreSQL and writes the module StoreTraceLog, which defines    *)
(*                                                                         *)
(*   TCards, TRows, TJudgements, TCursors, TBodies, TVersions: the names  *)
(*     the walk uses (Store.tla's universes, overridden in the cfg);       *)
(*   TOps: every op of the walk, in the model's terms (TraceOp in          *)
(*     store/trace.go), overriding Store.tla's fixed op table;             *)
(*   TraceLog == << r_1, ..., r_n >>: one record per step.                 *)
(*                                                                         *)
(* r_1 is Init. Each later record names a step of Store.tla: Apply,       *)
(* LoseRequest or Replay of op r.op; an Apply says whether the caller      *)
(* learned the outcome (known) and, when it did, accepted. Every record    *)
(* carries the store's state after the step, projected by TraceState      *)
(* (store/trace.go, the refinement mapping): epoch, and for cards, rows,   *)
(* judgements, cursors and policies the entries that changed since the     *)
(* record before (an entry not named did not change). So every step pins   *)
(* the whole projected state, not a part of it.                            *)
(*                                                                         *)
(* l is the index of the last record matched. TraceNext takes record      *)
(* l + 1 only by the action it names, landing exactly on its state. A     *)
(* trace the model allows reaches l = Len(TraceLog) (TraceMatched holds,  *)
(* TLC exit 0). A step the model refuses has no successor: TraceMatched   *)
(* is violated (exit 13) and the last state has l = k, so record k + 1 is *)
(* the step refused. TraceIndexed (an ASSUME) catches a malformed log.     *)
(*                                                                         *)
(* Every op may be lost (TLossy), at most MaxLoss times, and none waits   *)
(* on another (TPred): the walk's caller is one caller, in order.          *)
(***************************************************************************)
EXTENDS Store, StoreTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

\* The overrides the cfg makes of Store.tla's fixed scenario.
TActive    == DOMAIN TOps
TLossy     == DOMAIN TOps
TPred(o)   == {}

TraceActions == {"Init", "Apply", "LoseRequest", "Replay"}

Has(r, f) == f \in DOMAIN r

\* The value a record gives entry k: its own, else old's (unchanged).
Upd(d, old, k) == IF k \in DOMAIN d THEN d[k] ELSE old[k]

LoggedInit(r) ==
  /\ epoch = r.epoch
  /\ \A c \in Cards      : cards[c] = Upd(r.cards, Empty.cards, c)
  /\ \A x \in Rows       : rows[x] = Upd(r.rows, Empty.rows, x)
  /\ \A j \in Judgements : judgements[j] = Upd(r.judgements, Empty.judgements, j)
  /\ \A x \in Cursors    : cursors[x] = Upd(r.cursors, Empty.cursors, x)
  /\ \A v \in Versions   : policies[v] = Upd(r.policies, Empty.policies, v)
  /\ stopped = r.stopped

LoggedNext(r) ==
  /\ epoch' = r.epoch
  /\ \A c \in Cards      : cards'[c] = Upd(r.cards, cards, c)
  /\ \A x \in Rows       : rows'[x] = Upd(r.rows, rows, x)
  /\ \A j \in Judgements : judgements'[j] = Upd(r.judgements, judgements, j)
  /\ \A x \in Cursors    : cursors'[x] = Upd(r.cursors, cursors, x)
  /\ \A v \in Versions   : policies'[v] = Upd(r.policies, policies, v)
  /\ stopped' = r.stopped
  /\ Has(r, "accepted") =>
       \E k \in DOMAIN events' : events'[k].id = r.op /\ events'[k].accepted = r.accepted

\* The model action a record names.
StepAs(r) ==
  CASE r.action = "Apply"       -> Apply(r.op) /\ phase'[r.op] = IF r.known THEN "done" ELSE "unknown"
    [] r.action = "LoseRequest" -> LoseRequest(r.op)
    [] r.action = "Replay"      -> Replay(r.op)
    [] OTHER                    -> FALSE

TraceInit ==
  /\ l = 1
  /\ Init
  /\ LoggedInit(TraceLog[1])

TraceNext ==
  /\ l < Len(TraceLog)
  /\ l' = l + 1
  /\ StepAs(TraceLog[l + 1])
  /\ LoggedNext(TraceLog[l + 1])

TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)

\* The log is well formed: indices 1..n in order, known actions, known ops.
TraceIndexed ==
  \A k \in 1..Len(TraceLog) :
    /\ Has(TraceLog[k], "i") /\ TraceLog[k].i = k
    /\ Has(TraceLog[k], "action") /\ TraceLog[k].action \in TraceActions
    /\ (k = 1) = (TraceLog[k].action = "Init")
    /\ k > 1 => TraceLog[k].op \in DOMAIN TOps
ASSUME TraceIndexed

\* Every record matched: violated exactly when a step the model refuses
\* (or a projected state it cannot reach) stops the trace at l.
TraceMatched == <>(l = Len(TraceLog))
=============================================================================
