----------------------------- MODULE StepsTrace -----------------------------
(***************************************************************************)
(* Trace validation of the steps: Steps.tla's next-state relation         *)
(* constrained to a recorded trace (the Kuppe, Merz et al. pattern). The  *)
(* Go walk of steps/ (a test driving one of Steps.tla's scenarios on the  *)
(* fake store) writes the module StepsTraceLog, which defines              *)
(*                                                                         *)
(*   TraceLog == << r_1, r_2, ..., r_n >>                                  *)
(*                                                                         *)
(* one record per step, r_1 the initial state. Each record has i (its     *)
(* index, 1..n), action (one of TraceActions, r_1's is "Init"), the       *)
(* action's arguments, and any of the logged variables (Logged): a field  *)
(* that is present must equal the model's variable after the step; one    *)
(* that is absent is left to the model (partial logging). The refinement  *)
(* mapping from the Go state to these fields is in README.md, "Trace      *)
(* validation".                                                           *)
(*                                                                         *)
(* l is the index of the last record matched. TraceNext takes record      *)
(* l + 1 only by the model action it names, with its arguments, landing   *)
(* exactly on its logged fields. A trace the model allows reaches         *)
(* l = Len(TraceLog) (TraceMatched holds, TLC exit 0). A step the model   *)
(* forbids leaves no successor: TraceMatched is violated (TLC exit 13)    *)
(* and the counterexample's last state has l = k, so record k + 1 is the  *)
(* step the model forbids. TraceIndexed (an ASSUME) checks the records'  *)
(* i fields and action names, so a malformed log fails before any state   *)
(* (TLC's assumption error) and is never read as a forbidden step.       *)
(*                                                                         *)
(* The model's bounds (MaxAtt, MaxEnv, MaxArrive, K) are set in the trace *)
(* cfg above any trace's length; the scenario (cards, rows, streams,      *)
(* paths, tiers, which results occur) is the one the Go walk drives. The  *)
(* oracle's answer for a Tick is the record's answer field when present   *)
(* ([victim, rows], in the model's terms: "oldest" or "failed", a row or  *)
(* "failed"), any of AnswersFor(snap) when absent.                        *)
(***************************************************************************)
EXTENDS Steps, StepsTraceLog, TLC

VARIABLE l

traceVars == <<vars, l>>

TraceActions == {"Init", "Snap", "Tick", "Take", "Report", "Hold", "Unhold", "Gone", "Seen",
                 "EnvMove", "EdgeResult"}

Has(r, f) == f \in DOMAIN r

\* The logged variables: a present field equals the variable.
Logged(r) ==
  /\ Has(r, "w")        => w = r.w
  /\ Has(r, "snap")     => snap = r.snap
  /\ Has(r, "phase")    => phase = r.phase
  /\ Has(r, "inbox")    => inbox = r.inbox
  /\ Has(r, "inflight") => inflight = r.inflight
  /\ Has(r, "js")       => js = r.js
  /\ Has(r, "split")    => split = r.split

\* The same after a step (written out, so the record index is not primed).
LoggedNext(r) ==
  /\ Has(r, "w")        => w' = r.w
  /\ Has(r, "snap")     => snap' = r.snap
  /\ Has(r, "phase")    => phase' = r.phase
  /\ Has(r, "inbox")    => inbox' = r.inbox
  /\ Has(r, "inflight") => inflight' = r.inflight
  /\ Has(r, "js")       => js' = r.js
  /\ Has(r, "split")    => split' = r.split

\* The environment move a record names (EnvMove leaves it to the model).
EnvKind(r) ==
  CASE r.action = "Hold"   -> \E s \in Streams : ~w.sh[s] /\ w'.sh[s]
    [] r.action = "Unhold" -> \E s \in Streams : w.sh[s] /\ ~w'.sh[s]
    [] r.action = "Gone"   -> \E x \in Rows : w.up[x] /\ ~w'.up[x]
    [] r.action = "Seen"   -> \E x \in Rows : ~w.up[x] /\ w'.up[x]
    [] OTHER               -> TRUE

\* The model action a record names, with its arguments.
StepAs(r) ==
  CASE r.action = "Snap"       -> Snap
    [] r.action = "Tick"       -> \E a \in AnswersFor(snap) :
                                    /\ Has(r, "answer") => a = r.answer
                                    /\ Tick(a)
    [] r.action = "Take"       -> Take(r.card)
    [] r.action = "Report"     -> Report(r.card, r.res)
    [] r.action \in {"Hold", "Unhold", "Gone", "Seen", "EnvMove"} -> EnvMove /\ EnvKind(r)
    [] r.action = "EdgeResult" -> \E f \in inflight : f.id = r.id /\ EdgeResult(f, r.out, r.n)
    [] OTHER                   -> FALSE

TraceInit ==
  /\ l = 1
  /\ Init
  /\ TraceLog[1].action = "Init"
  /\ Logged(TraceLog[1])

TraceNext ==
  /\ l < Len(TraceLog)
  /\ l' = l + 1
  /\ StepAs(TraceLog[l + 1])
  /\ LoggedNext(TraceLog[l + 1])

TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)

\* The log is well formed: indices 1..n in order, known action names.
TraceIndexed ==
  \A k \in 1..Len(TraceLog) :
    /\ Has(TraceLog[k], "i") /\ TraceLog[k].i = k
    /\ Has(TraceLog[k], "action") /\ TraceLog[k].action \in TraceActions
    /\ (k = 1) = (TraceLog[k].action = "Init")
ASSUME TraceIndexed

\* Every record matched: violated exactly when a step the model forbids
\* (or a logged value it cannot reach) stops the trace at l.
TraceMatched == <>(l = Len(TraceLog))
=============================================================================
