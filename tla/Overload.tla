------------------------------ MODULE Overload ------------------------------
(***************************************************************************)
(* An overloaded bench wakes the coordinator (2026-11-10; modules/run/     *)
(* loadwatch.go, modules/steps/overload.go). The same judgement lifecycle  *)
(* as BaseRed.tla, per row: at most one open, opened by the tick, held     *)
(* while its cause holds, escalated at its deadline, quiet after an answer *)
(* until its quiet ends, cleared by the tick when the cause is gone.       *)
(*                                                                         *)
(* Row b is a bench; row k is a friend (no machine load to judge). cur is  *)
(* the row's last beat: over or under the bound (overload_factor x cores). *)
(* Tick takes one sample per row (the run's LoadWatch: overT and underT    *)
(* count the samples of the current run, capped at Span), sets the fact   *)
(* when a run reaches Span (a shorter spike or dip changes nothing), and   *)
(* runs the steps' overloads and the clear on that fact. sustained is a    *)
(* ghost: whether the latest run to reach Span was over.                   *)
(*                                                                         *)
(* Reversed witnesses beside it: OverloadNoPush.tla (the old behaviour: a  *)
(* bench over for the span, no judgement) must violate OverloadIsPushed;   *)
(* OverloadEarly.tla (a single sample over opens one) must violate         *)
(* OpenOnlyWhenSustained.                                                  *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Span,    \* samples a run must last
          MaxEnv   \* environment moves at most (beats, ups, answers, deadlines)

Rows  == {"b", "k"}
Bench == {"b"}

VARIABLES cur, overT, underT, fact, sustained, up, js, quiet, ticked, env

vars == <<cur, overT, underT, fact, sustained, up, js, quiet, ticked, env>>

Init ==
  /\ cur \in [Rows -> {"over", "under"}]
  /\ overT = [r \in Rows |-> 0] /\ underT = [r \in Rows |-> 0]
  /\ fact = [r \in Rows |-> "none"] /\ sustained = [r \in Rows |-> FALSE]
  /\ up \in [Rows -> BOOLEAN]
  /\ js = [r \in Rows |-> <<>>] /\ quiet = [r \in Rows |-> FALSE]
  /\ ticked = FALSE /\ env = 0

Min(a, b) == IF a < b THEN a ELSE b
Last(r) == IF js[r] = <<>> THEN [st |-> "none"] ELSE js[r][Len(js[r])]
Expired(r) == [js[r] EXCEPT ![Len(js[r])].st = "expired"]

\* The run's LoadWatch, one sample.
O2(r) == IF cur[r] = "over" THEN Min(overT[r] + 1, Span) ELSE 0
U2(r) == IF cur[r] = "under" THEN Min(underT[r] + 1, Span) ELSE 0
Fact(r) == IF O2(r) >= Span THEN "over" ELSE IF U2(r) >= Span THEN "under" ELSE fact[r]

\* The steps' overloads, then the clear, on fact f.
StepJ(r, f) ==
  IF f = "over" /\ r \in Bench /\ up[r]
    THEN IF Last(r).st = "open" \/ (Last(r).st = "answered" /\ quiet[r])
           THEN js[r]
           ELSE Append(js[r], [st |-> "open"])
    ELSE IF f = "under" /\ Last(r).st = "open"
      THEN Expired(r)
      ELSE js[r]

Tick ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ overT' = [r \in Rows |-> O2(r)]
  /\ underT' = [r \in Rows |-> U2(r)]
  /\ fact' = [r \in Rows |-> Fact(r)]
  /\ sustained' = [r \in Rows |-> IF O2(r) >= Span THEN TRUE ELSE IF U2(r) >= Span THEN FALSE ELSE sustained[r]]
  /\ js' = [r \in Rows |-> StepJ(r, Fact(r))]
  /\ UNCHANGED <<cur, up, quiet, env>>

Env == env < MaxEnv /\ env' = env + 1 /\ ticked' = FALSE

Beat(r) == /\ Env /\ cur' = [cur EXCEPT ![r] = IF @ = "over" THEN "under" ELSE "over"]
           /\ UNCHANGED <<overT, underT, fact, sustained, up, js, quiet>>

\* A tick with no new beat samples the same load again.
Again == /\ Env /\ UNCHANGED <<cur, overT, underT, fact, sustained, up, js, quiet>>

UpDown(r) == /\ Env /\ up' = [up EXCEPT ![r] = ~@]
             /\ UNCHANGED <<cur, overT, underT, fact, sustained, js, quiet>>

Answer(r) == /\ Env /\ Last(r).st = "open"
             /\ js' = [js EXCEPT ![r][Len(js[r])].st = "answered"] /\ quiet' = [quiet EXCEPT ![r] = TRUE]
             /\ UNCHANGED <<cur, overT, underT, fact, sustained, up>>

QuietEnds(r) == /\ Env /\ quiet[r] /\ quiet' = [quiet EXCEPT ![r] = FALSE]
                /\ UNCHANGED <<cur, overT, underT, fact, sustained, up, js>>

\* The open one's deadline: reopened escalated while the tick detects it.
Lapse(r) == /\ Env /\ Last(r).st = "open"
            /\ js' = [js EXCEPT ![r] = IF fact[r] = "over" /\ up[r] THEN Append(Expired(r), [st |-> "open"]) ELSE Expired(r)]
            /\ UNCHANGED <<cur, overT, underT, fact, sustained, up, quiet>>

Next == Tick \/ Again \/ \E r \in Rows : Beat(r) \/ UpDown(r) \/ Answer(r) \/ QuietEnds(r) \/ Lapse(r)

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

-----------------------------------------------------------------------------
TypeOK == /\ overT \in [Rows -> 0..Span] /\ underT \in [Rows -> 0..Span]
          /\ fact \in [Rows -> {"none", "over", "under"}]
          /\ \A r \in Rows : \A i \in 1..Len(js[r]) : js[r][i].st \in {"open", "answered", "expired"}

\* At most one open overload per row.
OneOpen == \A r \in Rows : Cardinality({i \in 1..Len(js[r]) : js[r][i].st = "open"}) <= 1

\* A friend row never gets one.
FriendNever == \A r \in Rows \ Bench : js[r] = <<>>

\* A bench row that is up and over for the span has an open overload
\* judgement within a tick (unless answered and its quiet has not ended).
OverloadIsPushed == ticked => \A r \in Bench :
  fact[r] = "over" /\ up[r] => Last(r).st = "open" \/ (Last(r).st = "answered" /\ quiet[r])

\* After the tick, one is open only while the latest run to last the span
\* was over: never on a spike, and never past a recovery.
OpenOnlyWhenSustained == ticked => \A r \in Rows : Last(r).st = "open" => sustained[r]

\* Liveness: a row back under for the span has it cleared.
RecoveryClears == \A r \in Rows : [](underT[r] >= Span => <>(Last(r).st # "open" \/ underT[r] < Span))

\* Reach (expected violated in OverloadNever*.cfg): one opens; one clears.
NeverOpens == js["b"] = <<>>
NeverCleared == \A i \in 1..Len(js["b"]) : js["b"][i].st # "expired"
=============================================================================
