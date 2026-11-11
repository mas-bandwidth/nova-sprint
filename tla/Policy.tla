------------------------------ MODULE Policy -------------------------------
(***************************************************************************)
(* Layer 5 of nova-sprint v2: policy versions, activation and the per-tick *)
(* fallback (docs/LAYER-5-POLICY.md, "The model"; the code is              *)
(* modules/policy/answer.go AnswerContext, modules/policy/tick.go race, and               *)
(* modules/policy/verbs.go Set and Activate). Round 3: the tick's one deadline is *)
(* one more way a version breaks on a tick, and a fallback started early  *)
(* (tick.go fallbackAfter) never changes which version answers: the first *)
(* in order that passes, as Tick says.                                    *)
(*                                                                         *)
(* Versions 1..MaxVersion are stored in order and never deleted; Default  *)
(* is the built-in, never stored; it can fail too (a bug in the binary).  *)
(* When every version tried fails the tick is answered by NoAnswer: it    *)
(* still applies every move that needs no answer, and deals nothing.      *)
(* Init is the state after up: the default stored as the next version    *)
(* number and active (modules/policy/verbs.go Up); the model holds no data rows,  *)
(* which the code counts but never runs, so that number is 1 here.        *)
(* Round 5: Activate answers the built-in's judgement too (policy:v0: any *)
(* clean activation answers it), and is one op in the code, as it is one  *)
(* step here, fenced on no version or judgement written since its         *)
(* snapshot (UnchangedSince; a beat moves the seq and none of them). The  *)
(* snapshot moving between its check and its op is Drift after Activate:  *)
(* Drift may set breaks to any set at any step, so the states are these.  *)
(* That reordering is argued here, not checked: TLC checks Activate with  *)
(* a fresh guard, and the PolicyTrace walk is serial.                     *)
(* breaks is the set of                                                  *)
(* versions that fail (a runtime error, a refused answer or the deadline) *)
(* on the current snapshot; Drift moves the snapshot, so breaks changes   *)
(* arbitrarily. One card stands for every card: a tick may move it, a     *)
(* policy change never does, and a wait keeps the bound it started under. *)
(* judgements[v] counts the open policy judgements of version v (group    *)
(* key policy:v<n>).                                                      *)
(* kept is the version whose limits and timeouts the last answered tick   *)
(* had (run's Ticker.Limits and Timeouts; the built-in's before any);     *)
(* applied is the version whose limits and timeouts the last tick ran on  *)
(* (steps' oracle). A tick no version answered runs on kept, never on the *)
(* built-in's (modules/run/asker.go Ask, steps.Answers KeptLimits and     *)
(* KeptTimeouts): NoAnswerKeepsTheBounds.                                 *)
(***************************************************************************)
EXTENDS Integers, FiniteSets

CONSTANT MaxVersion

Versions == 1..MaxVersion
Default == 0
None == -1
NoAnswer == -2
Usable == Versions \cup {Default}
CardStates == {"ready", "working", "done"}
Ops == {"up", "set", "activate", "drift", "tick"}

VARIABLES stored, active, prev, everActive, breaks, answeredBy, tickBreaks,
          judgements, card, cardBefore, waitBound, startBound, lastOp,
          kept, applied

vars == <<stored, active, prev, everActive, breaks, answeredBy, tickBreaks,
          judgements, card, cardBefore, waitBound, startBound, lastOp,
          kept, applied>>

TypeOK ==
  /\ stored \subseteq Versions
  /\ active \in Versions
  /\ prev \in Versions \cup {None}
  /\ everActive \subseteq Versions
  /\ breaks \subseteq Usable
  /\ answeredBy \in Usable \cup {None, NoAnswer}
  /\ tickBreaks \subseteq Usable
  /\ judgements \in [Usable -> Nat]
  /\ card \in CardStates /\ cardBefore \in CardStates
  /\ waitBound \in Versions \cup {Default, None}
  /\ startBound \in Versions \cup {Default, None}
  /\ lastOp \in Ops
  /\ kept \in Usable /\ applied \in Usable

Init ==
  /\ stored = {1} /\ active = 1 /\ prev = None /\ everActive = {1}
  /\ breaks = {} /\ answeredBy = None /\ tickBreaks = {}
  /\ judgements = [v \in Usable |-> 0]
  /\ card = "ready" /\ cardBefore = "ready"
  /\ waitBound = None /\ startBound = None
  /\ lastOp = "up"
  /\ kept = Default /\ applied = Default

(* The card's next state when a tick moves it.                            *)
Advance(c) == CASE c = "ready" -> "working" [] c = "working" -> "done" [] c = "done" -> "ready"

(* policy set: checked on the current snapshot before it is stored; the   *)
(* next number; never active.                                             *)
Set(v) ==
  /\ v = Cardinality(stored) + 1
  /\ v \notin breaks
  /\ stored' = stored \cup {v}
  /\ prev' = prev
  /\ cardBefore' = card
  /\ lastOp' = "set"
  /\ UNCHANGED <<active, everActive, breaks, answeredBy, tickBreaks, judgements, card, waitBound, startBound, kept, applied>>

(* policy activate: a stored version that runs clean on the current       *)
(* snapshot; one op moves the active flag and answers the open policy     *)
(* judgements of the old version, of v and of the built-in.               *)
Activate(v) ==
  /\ v \in stored
  /\ v # active
  /\ v \notin breaks
  /\ active' = v
  /\ prev' = active
  /\ everActive' = everActive \cup {v}
  /\ judgements' = [judgements EXCEPT ![active] = 0, ![v] = 0, ![Default] = 0]
  /\ card' = card   \* a policy change never moves a card
  /\ waitBound' = waitBound
  /\ cardBefore' = card
  /\ lastOp' = "activate"
  /\ UNCHANGED <<stored, breaks, answeredBy, tickBreaks, startBound, kept, applied>>

(* The snapshot moves: any set of versions may now fail, the built-in too. *)
Drift ==
  /\ \E b \in SUBSET Usable : b # breaks /\ breaks' = b
  /\ cardBefore' = card
  /\ lastOp' = "drift"
  /\ UNCHANGED <<stored, active, prev, everActive, answeredBy, tickBreaks, judgements, card, waitBound, startBound, kept, applied>>

(* The version that answers a tick: active, else the previous active,     *)
(* else the built-in (modules/policy/answer.go Versions.tried), else NoAnswer     *)
(* (Answers.Version -1).                                                  *)
Pick ==
  IF active \notin breaks THEN active
  ELSE IF prev # None /\ prev \notin breaks THEN prev
  ELSE IF Default \notin breaks THEN Default
  ELSE NoAnswer

(* The versions tried this tick, in order, and those of them failing: one *)
(* judgement each.                                                        *)
Tried == {active} \cup (IF prev = None THEN {} ELSE {prev}) \cup {Default}
Failing ==
  ({active} \cap breaks) \cup
  (IF active \in breaks /\ prev # None THEN {prev} \cap breaks ELSE {}) \cup
  (IF active \in breaks /\ (prev = None \/ prev \in breaks) THEN {Default} \cap breaks ELSE {})

(* A tick: answered by Pick; opens a judgement for each failing version   *)
(* with none open; may move the card. A wait records the bound of the     *)
(* version that answered the tick that started it. An answered tick runs  *)
(* on its version's limits and timeouts and keeps them; one no version    *)
(* answered runs on those kept.                                           *)
Tick ==
  /\ answeredBy' = Pick
  /\ kept' = IF Pick = NoAnswer THEN kept ELSE Pick
  /\ applied' = IF Pick = NoAnswer THEN kept ELSE Pick
  /\ tickBreaks' = breaks
  /\ judgements' = [u \in Usable |-> IF u \in Failing /\ judgements[u] = 0 THEN 1 ELSE judgements[u]]
  /\ \E c \in {card, Advance(card)} :
       /\ (c = "working" /\ card = "ready") => Pick # NoAnswer   \* a deal needs an answer
       /\ card' = c
       /\ IF c = "working" /\ card # "working" THEN waitBound' = Pick /\ startBound' = Pick
          ELSE IF c # "working" THEN waitBound' = None /\ startBound' = None
          ELSE UNCHANGED <<waitBound, startBound>>
  /\ cardBefore' = card
  /\ lastOp' = "tick"
  /\ UNCHANGED <<stored, active, prev, everActive, breaks>>

Next ==
  \/ \E v \in Versions : Set(v)
  \/ \E v \in Versions : Activate(v)
  \/ Drift
  \/ Tick

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Invariants: the design's six, and FallbackWasActive for its witness.  *)

ActiveStored == active \in stored

AnsweredByUsable == answeredBy \notin tickBreaks

TickAlwaysAnswered ==
  lastOp = "tick" =>
    /\ answeredBy # None
    /\ answeredBy = NoAnswer => Tried \subseteq tickBreaks

NoAnswerStartsNothing ==
  (lastOp = "tick" /\ answeredBy = NoAnswer) => ~(cardBefore = "ready" /\ card = "working")

ActivateMovesNoCard == lastOp \in {"set", "activate"} => card = cardBefore

OneJudgementPerVersion == \A v \in Usable : judgements[v] <= 1

WaitKeepsItsBound == card = "working" => waitBound = startBound

FallbackWasActive == answeredBy \in everActive \cup {Default, None, NoAnswer}

(* A tick no version answered never changes the bounds a wait or a count  *)
(* is held to: it runs on the last answered tick's (776-H1, a custom      *)
(* policy's bounces 16 and pro 14400 fell to the built-in's 12 and 7200). *)
NoAnswerKeepsTheBounds == applied = kept

(* Reachability: each must be violated (tla/reach/Policy*.cfg), or the    *)
(* model never reaches the case the invariants above are about.           *)
NeverPrevAnswers == ~(lastOp = "tick" /\ answeredBy \notin {active, Default})
NeverDefaultAnswers == ~(lastOp = "tick" /\ answeredBy = Default)
NeverJudged == \A v \in Usable : judgements[v] = 0
NeverNoAnswer == ~(lastOp = "tick" /\ answeredBy = NoAnswer)
NeverRollback == ~(lastOp = "activate" /\ active < prev)
NeverWaitAcrossActivate == ~(lastOp = "activate" /\ card = "working")
NeverNoAnswerKeepsAVersion == ~(lastOp = "tick" /\ answeredBy = NoAnswer /\ kept # Default)

=============================================================================
