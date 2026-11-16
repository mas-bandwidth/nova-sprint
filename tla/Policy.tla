------------------------------ MODULE Policy -------------------------------
(***************************************************************************)
(* Layer 5 of nova-sprint v3: policy versions, activation, per-tick        *)
(* fallback, and R65 bench capability without width (docs/LAYER-5-POLICY.md; *)
(* modules/policy/answer.go AnswerContext & jointRows, modules/policy/tick.go *)
(* race, and modules/policy/verbs.go Set and Activate).                    *)
(*                                                                         *)
(* Versions 1..MaxVersion are stored in order and never deleted; Default   *)
(* is the built-in, never stored; it can fail too (a bug in the binary).   *)
(* When every version tried fails the tick is answered by NoAnswer: it     *)
(* still applies every move that needs no answer, and deals nothing.       *)
(* Init is the state after up: the default stored as the next version      *)
(* number and active (modules/policy/verbs.go Up).                         *)
(*                                                                         *)
(* R65 (v3): width(row) is asked only for kin (friend) rows                *)
(* (WidthAskedKinds = {"kin"}); worker benches carry tags, resources and   *)
(* free capacity instead of width, and jointRows treats candidate benches  *)
(* as inherently capable (JointCapable("worker", ...) = TRUE) rather than  *)
(* rejecting them for having no width entry.                               *)
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
RowKinds == {"kin", "worker"}
WidthAskedKinds == {"kin"}

VARIABLES stored, active, prev, everActive, breaks, answeredBy, tickBreaks,
          judgements, card, cardBefore, waitBound, startBound, benchDealt, lastOp

vars == <<stored, active, prev, everActive, breaks, answeredBy, tickBreaks,
          judgements, card, cardBefore, waitBound, startBound, benchDealt, lastOp>>

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
  /\ benchDealt \in BOOLEAN
  /\ lastOp \in Ops

Init ==
  /\ stored = {1} /\ active = 1 /\ prev = None /\ everActive = {1}
  /\ breaks = {} /\ answeredBy = None /\ tickBreaks = {}
  /\ judgements = [v \in Usable |-> 0]
  /\ card = "ready" /\ cardBefore = "ready"
  /\ waitBound = None /\ startBound = None
  /\ benchDealt = FALSE
  /\ lastOp = "up"

(* The card's next state when a tick moves it.                            *)
Advance(c) == CASE c = "ready" -> "working" [] c = "working" -> "done" [] c = "done" -> "ready"

(* R65: width(row) is asked only for WidthAskedKinds ({"kin"}); jointRows *)
(* treats worker benches as inherently capable without a width entry.     *)
JointCapable(kind, widthMap) ==
  IF kind = "worker" THEN TRUE
  ELSE kind \in DOMAIN widthMap /\ widthMap[kind] > 0

(* policy set: checked on the current snapshot before it is stored; the   *)
(* next number; never active.                                             *)
Set(v) ==
  /\ v = Cardinality(stored) + 1
  /\ v \notin breaks
  /\ stored' = stored \cup {v}
  /\ prev' = prev
  /\ cardBefore' = card
  /\ lastOp' = "set"
  /\ UNCHANGED <<active, everActive, breaks, answeredBy, tickBreaks, judgements, card, waitBound, startBound, benchDealt>>

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
  /\ UNCHANGED <<stored, breaks, answeredBy, tickBreaks, startBound, benchDealt>>

(* The snapshot moves: any set of versions may now fail, the built-in too. *)
Drift ==
  /\ \E b \in SUBSET Usable : b # breaks /\ breaks' = b
  /\ cardBefore' = card
  /\ lastOp' = "drift"
  /\ UNCHANGED <<stored, active, prev, everActive, answeredBy, tickBreaks, judgements, card, waitBound, startBound, benchDealt>>

(* The version that answers a tick: active, else the previous active,     *)
(* else the built-in, else NoAnswer.                                      *)
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
(* with none open; may move the card.                                     *)
Tick ==
  LET widthMap == [k \in WidthAskedKinds |-> 1]
  IN /\ answeredBy' = Pick
     /\ tickBreaks' = breaks
     /\ judgements' = [u \in Usable |-> IF u \in Failing /\ judgements[u] = 0 THEN 1 ELSE judgements[u]]
     /\ benchDealt' = (Pick # NoAnswer /\ JointCapable("worker", widthMap))
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
(* Invariants: the design's six, FallbackWasActive, and R65 bench capable. *)

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

BenchCapableWithoutWidth ==
  /\ "worker" \notin WidthAskedKinds
  /\ (lastOp = "tick" /\ answeredBy # NoAnswer) => benchDealt

(* Reachability: each must be violated (tla/reach/Policy*.cfg), or the    *)
(* model never reaches the case the invariants above are about.           *)
NeverPrevAnswers == ~(lastOp = "tick" /\ answeredBy \notin {active, Default})
NeverDefaultAnswers == ~(lastOp = "tick" /\ answeredBy = Default)
NeverJudged == \A v \in Usable : judgements[v] = 0
NeverNoAnswer == ~(lastOp = "tick" /\ answeredBy = NoAnswer)
NeverRollback == ~(lastOp = "activate" /\ active < prev)
NeverWaitAcrossActivate == ~(lastOp = "activate" /\ card = "working")

=============================================================================
