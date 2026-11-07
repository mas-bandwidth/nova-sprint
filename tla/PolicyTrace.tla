---------------------------- MODULE PolicyTrace -----------------------------
(***************************************************************************)
(* Trace validation of the policy engine: Policy.tla's actions            *)
(* constrained to a recording of the Go code on the store (the shape of   *)
(* StoreTrace.tla and CardMachineTrace.tla). The policy walk              *)
(* (policy/trace_test.go) writes the module PolicyTraceLog, which defines *)
(* TraceLog == << r_1, ..., r_n >>: one record per step.                  *)
(*                                                                         *)
(* r_1 is Init (the state after up). Each later record names an action of *)
(* the model with its argument (v for Set and Activate) and carries       *)
(* stored, active, prev, breaks, answeredBy, judgements and card after    *)
(* it, projected by TraceState (policy/trace.go, the refinement mapping). *)
(* Each record carries the whole projected state, which is small; the     *)
(* model's own bookkeeping (everActive, tickBreaks, cardBefore, waitBound, *)
(* startBound, lastOp) is not projected: the action computes it.          *)
(*                                                                         *)
(* The trace adds nothing to the model: the actions are called with the   *)
(* record's argument, and Drift's choice of breaks is fixed by the record. *)
(*                                                                         *)
(* l is the index of the last record matched; a refused step leaves the   *)
(* trace at l and TraceMatched is violated (TLC exit 13), so record l + 1 *)
(* is the step the model refuses.                                          *)
(***************************************************************************)
EXTENDS Policy, PolicyTraceLog, Sequences, TLC

VARIABLE l

traceVars == <<vars, l>>

TraceActions == {"Init", "Set", "Activate", "Drift", "Tick"}

Has(r, f) == f \in DOMAIN r

\* The model's action a record names, with its argument.
StepAs(r) ==
  CASE r.action = "Set"      -> Set(r.v)
    [] r.action = "Activate" -> Activate(r.v)
    [] r.action = "Drift"    -> Drift
    [] r.action = "Tick"     -> Tick
    [] OTHER                 -> FALSE

\* The projected state, of the model and of a record.
Proj == [stored |-> stored, active |-> active, prev |-> prev, breaks |-> breaks,
         answeredBy |-> answeredBy, judgements |-> judgements, card |-> card]
ProjOf(r) == [stored |-> r.stored, active |-> r.active, prev |-> r.prev, breaks |-> r.breaks,
              answeredBy |-> r.answeredBy, judgements |-> r.judgements, card |-> r.card]

TraceInit ==
  /\ l = 1
  /\ TraceLog[1].action = "Init"
  /\ Init
  /\ Proj = ProjOf(TraceLog[1])

TraceNext ==
  /\ l < Len(TraceLog)
  /\ l' = l + 1
  /\ StepAs(TraceLog[l + 1])
  /\ Proj' = ProjOf(TraceLog[l + 1])

TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)

\* The log is well formed: indices 1..n in order, known actions.
TraceIndexed ==
  \A k \in 1..Len(TraceLog) :
    /\ Has(TraceLog[k], "i") /\ TraceLog[k].i = k
    /\ Has(TraceLog[k], "action") /\ TraceLog[k].action \in TraceActions
    /\ (k = 1) = (TraceLog[k].action = "Init")
ASSUME TraceIndexed

\* Every record matched: violated exactly when a step the model refuses
\* (or a projected state it cannot reach) stops the trace at l.
TraceMatched == <>(l = Len(TraceLog))
=============================================================================
