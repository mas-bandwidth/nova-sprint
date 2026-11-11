---------------------------- MODULE AnsweredQuiet ----------------------------
(***************************************************************************)
(* An answered watch judgement stays quiet while its cause holds unbroken  *)
(* (2026-11-10; modules/steps/judge.go, judge, answeredQuiet and broken).  *)
(* One watch kind about one thing: a cause a step detects or not, and its  *)
(* judgements, episode by episode. A watch kind's one choice is retry: the *)
(* answer says "I am on it", not that the cause is gone, so the same cause *)
(* still there at the next tick is no news. Tick is the step: a cause with *)
(* none open opens one, unless the latest was answered, its quiet (the     *)
(* judgement timeout) has not ended, and no tick has seen the cause not    *)
(* holding since the answer (broke: Plan.Broken, which run's Ticker        *)
(* carries to the next tick as Inputs.Broken). A cause that cleared and    *)
(* came back is news, pushed at once. Answer is the coordinator's retry;   *)
(* QuietEnds is the clock passing the answer's timeout; Cause is the world *)
(* changing.                                                               *)
(*                                                                         *)
(* Reversed witnesses beside it: AnsweredQuietReopens.tla (the judge       *)
(* before the quiet: an answered cause reopened at the next tick) must     *)
(* violate NoReopenInQuiet; AnsweredQuietSwallows.tla (the first quiet,    *)
(* keyed by kind and about only: a cause that cleared and came back inside *)
(* the timeout stayed unpushed) must violate CauseIsPushed.                *)
(* AnsweredQuietTrace.tla binds the code to this model                     *)
(* (modules/run/answeredquiet_trace_test.go).                              *)
(***************************************************************************)
EXTENDS Naturals, Sequences

CONSTANTS MaxEnv

VARIABLES cause,   \* a step detects the cause this tick
          js,      \* the episodes' states: open, answered or expired
          quiet,   \* the latest answer's judgement timeout has not ended
          broke,   \* a tick saw the cause not holding since the latest answer
          ticked,  \* the tick ran since the last environment move
          env

vars == <<cause, js, quiet, broke, ticked, env>>

Init == cause \in BOOLEAN /\ js = <<>> /\ quiet = FALSE /\ broke = FALSE /\ ticked = FALSE /\ env = 0

Last == IF js = <<>> THEN "none" ELSE js[Len(js)]

\* An answered one whose quiet runs and whose cause never broke since.
Quiet == Last = "answered" /\ quiet /\ ~broke

\* What a tick sees of the cause: not holding after an answer ends its quiet.
See == broke' = (broke \/ (~cause /\ Last = "answered"))

\* The step, then the clear: a cause gone expires the open one.
Tick ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ IF cause
       THEN IF Last = "open" \/ Quiet
              THEN UNCHANGED js
              ELSE js' = Append(js, "open")
       ELSE IF Last = "open" THEN js' = [js EXCEPT ![Len(js)] = "expired"] ELSE UNCHANGED js
  /\ See
  /\ UNCHANGED <<cause, quiet, env>>

Env == env < MaxEnv /\ env' = env + 1 /\ ticked' = FALSE

Cause == Env /\ cause' = ~cause /\ UNCHANGED <<js, quiet, broke>>
Answer == Env /\ Last = "open" /\ js' = [js EXCEPT ![Len(js)] = "answered"] /\ quiet' = TRUE /\ broke' = FALSE /\ UNCHANGED cause
QuietEnds == Env /\ quiet /\ quiet' = FALSE /\ UNCHANGED <<cause, js, broke>>

Next == Tick \/ Cause \/ Answer \/ QuietEnds

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

-----------------------------------------------------------------------------
TypeOK == /\ cause \in BOOLEAN /\ quiet \in BOOLEAN /\ broke \in BOOLEAN /\ ticked \in BOOLEAN
          /\ \A i \in 1..Len(js) : js[i] \in {"open", "answered", "expired"}

\* No episode opens over an answered one whose quiet runs unbroken (an
\* open one right after an answered one with the quiet running and no break
\* opened inside it).
NoReopenInQuiet == Len(js) >= 2 /\ js[Len(js) - 1] = "answered" /\ Last = "open" => ~(quiet /\ ~broke)

\* A cause that holds is pushed within a tick, unless it was answered, its
\* quiet runs and it has held at every tick since the answer: a cause that
\* went away and came back is pushed.
CauseIsPushed == ticked /\ cause => Last = "open" \/ Quiet

\* Reach (expected violated in AnsweredQuietNeverReopens.cfg): an answered
\* cause opens again (its quiet ended, or it broke).
NeverReopens == ~(Len(js) >= 2 /\ js[Len(js) - 1] = "answered")
=============================================================================
