---------------------------- MODULE AnsweredQuiet ----------------------------
(***************************************************************************)
(* An answered watch judgement stays quiet (2026-11-10; modules/steps/     *)
(* judge.go, judge and answeredQuiet). One watch kind about one thing: a   *)
(* cause a step detects or not, and its judgements, episode by episode.    *)
(* A watch kind's one choice is retry: the answer says "I am on it", not   *)
(* that the cause is gone, so a cause still there at the next tick is no   *)
(* news. Tick is the step: a cause with none open opens one, unless the    *)
(* latest was answered and its quiet (the judgement timeout) has not       *)
(* ended. Answer is the coordinator's retry; QuietEnds is the clock        *)
(* passing the answer's timeout; Cause is the world changing.              *)
(*                                                                         *)
(* Reversed witness beside it: AnsweredQuietReopens.tla (the old judge:    *)
(* an answered cause reopened at the next tick) must violate               *)
(* NoReopenInQuiet.                                                        *)
(***************************************************************************)
EXTENDS Naturals, Sequences

CONSTANTS MaxEnv

VARIABLES cause,   \* a step detects the cause this tick
          js,      \* the episodes' states: open, answered or expired
          quiet,   \* the latest answer's judgement timeout has not ended
          ticked,  \* the tick ran since the last environment move
          env

vars == <<cause, js, quiet, ticked, env>>

Init == cause \in BOOLEAN /\ js = <<>> /\ quiet = FALSE /\ ticked = FALSE /\ env = 0

Last == IF js = <<>> THEN "none" ELSE js[Len(js)]

\* The step, then the clear: a cause gone expires the open one.
Tick ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ IF cause
       THEN IF Last = "open" \/ (Last = "answered" /\ quiet)
              THEN UNCHANGED js
              ELSE js' = Append(js, "open")
       ELSE IF Last = "open" THEN js' = [js EXCEPT ![Len(js)] = "expired"] ELSE UNCHANGED js
  /\ UNCHANGED <<cause, quiet, env>>

Env == env < MaxEnv /\ env' = env + 1 /\ ticked' = FALSE

Cause == Env /\ cause' = ~cause /\ UNCHANGED <<js, quiet>>
Answer == Env /\ Last = "open" /\ js' = [js EXCEPT ![Len(js)] = "answered"] /\ quiet' = TRUE /\ UNCHANGED cause
QuietEnds == Env /\ quiet /\ quiet' = FALSE /\ UNCHANGED <<cause, js>>

Next == Tick \/ Cause \/ Answer \/ QuietEnds

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

-----------------------------------------------------------------------------
TypeOK == /\ cause \in BOOLEAN /\ quiet \in BOOLEAN /\ ticked \in BOOLEAN
          /\ \A i \in 1..Len(js) : js[i] \in {"open", "answered", "expired"}

\* No episode opens over an answered one whose quiet has not ended (the
\* quiet is set only by an answer, so an open one after an answered one
\* with the quiet still running opened inside it).
NoReopenInQuiet == Len(js) >= 2 /\ js[Len(js) - 1] = "answered" /\ Last = "open" => ~quiet

\* A cause that holds is pushed within a tick, unless it was answered and
\* its quiet runs.
CauseIsPushed == ticked /\ cause => Last = "open" \/ (Last = "answered" /\ quiet)

\* Reach (expected violated in AnsweredQuietNeverReopens.cfg): an answered
\* cause opens again once its quiet ends.
NeverReopens == ~(Len(js) >= 2 /\ js[Len(js) - 1] = "answered")
=============================================================================
