---------------------------- MODULE FinishVerdict ----------------------------
\* A worker's finish and its report's verdict line (internal/sprint/finish_verdict.go,
\* NotDoneVerdictLine and notDoneFinishWhy; steps_work.go finishPlan; docs/SPEC-SPRINT.md
\* section 2, "A plain finish with a HOLD verdict is refused").
\*
\* A working card's worker writes a report whose first verdict line is LAND, HOLD (HOLD
\* and not-done are one class here) or none. It then finishes plain (ok: the card goes to
\* review recorded ok) or --failed (recorded failed). A plain finish of a HOLD report is
\* refused: nothing moves and the refusal is counted on the card (FinishRefusalBound
\* bounds the count; SprintTables.tla FinishRefused holds the bound and its judgment). The
\* worker's next finish, with --failed, records it failed.
\*
\* NoHoldFinishedOk: no card is recorded ok whose report's verdict is HOLD.
\* HoldStillFinishes: a refused HOLD finish leaves the card working, so the --failed
\* finish still ends it (every card leaves working, under fairness of the worker).
\* The reversed witness Broken = "acceptshold" drops the guard (the finish before
\* 2026-10-24) and must break NoHoldFinishedOk.

EXTENDS Naturals

CONSTANTS Card, Broken, RefusalBound

Verdict == {"LAND", "HOLD", "none"}

VARIABLES state, ok, verdict, refusals

vars == <<state, ok, verdict, refusals>>

TypeOK ==
  /\ state \in [Card -> {"working", "finished"}]
  /\ ok \in [Card -> {"unset", "yes", "no"}]
  /\ verdict \in [Card -> Verdict \cup {"unwritten"}]
  /\ refusals \in [Card -> 0..RefusalBound]

Init ==
  /\ state = [c \in Card |-> "working"]
  /\ ok = [c \in Card |-> "unset"]
  /\ verdict = [c \in Card |-> "unwritten"]
  /\ refusals = [c \in Card |-> 0]

\* the worker writes its report; its first verdict line is one of Verdict
Report(c) ==
  /\ state[c] = "working"
  /\ verdict[c] = "unwritten"
  /\ \E v \in Verdict : verdict' = [verdict EXCEPT ![c] = v]
  /\ UNCHANGED <<state, ok, refusals>>

\* notDoneFinishWhy: a plain finish of a HOLD report is refused
PlainRefusable(c) == verdict[c] = "HOLD" /\ Broken # "acceptshold"

\* finish --head <sha> --report <text>: recorded ok, unless refused
PlainFinish(c) ==
  /\ state[c] = "working"
  /\ verdict[c] # "unwritten"
  /\ ~PlainRefusable(c)
  /\ state' = [state EXCEPT ![c] = "finished"]
  /\ ok' = [ok EXCEPT ![c] = "yes"]
  /\ UNCHANGED <<verdict, refusals>>

\* the refusal: nothing moves, the refusal is counted (bounded in the instance)
PlainRefused(c) ==
  /\ state[c] = "working"
  /\ PlainRefusable(c)
  /\ refusals[c] < RefusalBound
  /\ refusals' = [refusals EXCEPT ![c] = @ + 1]
  /\ UNCHANGED <<state, ok, verdict>>

\* finish --failed --report <text>: recorded failed, whatever the verdict
FailedFinish(c) ==
  /\ state[c] = "working"
  /\ verdict[c] # "unwritten"
  /\ state' = [state EXCEPT ![c] = "finished"]
  /\ ok' = [ok EXCEPT ![c] = "no"]
  /\ UNCHANGED <<verdict, refusals>>

Next == \E c \in Card : Report(c) \/ PlainFinish(c) \/ PlainRefused(c) \/ FailedFinish(c)

Spec == Init /\ [][Next]_vars /\ \A c \in Card : WF_vars(Report(c)) /\ WF_vars(FailedFinish(c))

\* -------------------------------------------------------------- the rules

NoHoldFinishedOk == \A c \in Card : ok[c] = "yes" => verdict[c] # "HOLD"

\* a finished card is recorded, ok or failed, and a working one is not
RecordedWhenFinished == \A c \in Card : (state[c] = "finished") <=> (ok[c] # "unset")

HoldStillFinishes == \A c \in Card : <>(state[c] = "finished")

=============================================================================
