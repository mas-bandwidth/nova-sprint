------------------------------ MODULE RowAlive ------------------------------
(***************************************************************************)
(* A row's up and down, and its hand, for friends and benches alike: the  *)
(* sprint's hard requirements 1 and 2 (docs/README.md "Hard requirements"). *)
(*                                                                         *)
(*  1. A row is up while its hook is on the bus and down when it has been *)
(*     off the bus for Bound: nothing manual (no pong, no command).        *)
(*     Code: modules/run/tick.go raiseSeen (up on presence),               *)
(*     modules/steps/limits.go rowLimits (Gone on Bound of bus silence).   *)
(*  2. Every card a row holds when it goes down or is held is returned to *)
(*     the queue in the same op and dealt again to a row that is open.    *)
(*     Code: modules/steps/holds.go handMoves (the Gone and the hold) and  *)
(*     holds (any hand left on a closed row); the deal, Steps.tla.         *)
(*                                                                         *)
(* The run ticks every second and Bound is minutes, so time moves only    *)
(* when no tick step is due (Advance). seen is the run's Seen: the last    *)
(* tick each row's hook was on the bus. Kinds are not modelled: since     *)
(* v2.0.42 no rule reads a row's kind for up or down, and that is the      *)
(* property (v2.0.36 to v2.0.41 raised a friend only on a pong its model   *)
(* ran by hand; Break = "manualup" is that rule, and it breaks AutoUp).   *)
(*                                                                         *)
(* Break names one reversed witness (RowAlive*.cfg, run by ci-tlc.sh):    *)
(*   "none"      the code's rules: every property holds;                  *)
(*   "manualup"  a down row comes up only on an answer its model may never *)
(*               give (v2.0.36..v2.0.41 for friends): AutoUp fails;        *)
(*   "nogone"    no Gone: a row off the bus stays up: HookedWhileUp fails; *)
(*   "gonekeeps" the Gone keeps the hand: HandEmptyWhenClosed fails;       *)
(*   "holdkeeps" the hold keeps the hand: HandEmptyWhenClosed fails;       *)
(*   "noredeal"  a returned card is never dealt again: Redealt fails.      *)
(***************************************************************************)
EXTENDS Naturals, Integers

CONSTANTS
  Bound,     \* ticks of bus silence after which an up row goes down (RowSilentGone)
  MaxClock,  \* model bound on time
  MaxEnv,    \* model bound on environment moves (hooks on and off, holds)
  Break      \* "none", or the reversed witness (above)

Rows  == {"r1", "r2"}
Cards == {"c1", "c2"}
None  == "none"

VARIABLES
  now,       \* the clock, in ticks
  bus,       \* row -> its hook is on nova-bus (environment)
  seen,      \* row -> the last tick its hook was on the bus (Inputs.Seen)
  up,        \* row -> the store's up
  held,      \* row -> the coordinator's hold
  answered,  \* row -> its model answered since it went down (Break = "manualup" only)
  holder,    \* card -> the row holding it, or None (the queue)
  returned,  \* cards returned to the queue at least once (ghost)
  wentDown,  \* row -> it has gone down (ghost, for the reach checks)
  env        \* environment moves so far

vars == <<now, bus, seen, up, held, answered, holder, returned, wentDown, env>>

Init ==
  /\ now = 0
  /\ bus \in [Rows -> BOOLEAN]
  /\ seen = [r \in Rows |-> 0]
  /\ up = [r \in Rows |-> TRUE]
  /\ held = [r \in Rows |-> FALSE]
  /\ answered = [r \in Rows |-> FALSE]
  /\ holder = [c \in Cards |-> None]
  /\ returned = {}
  /\ wentDown = [r \in Rows |-> FALSE]
  /\ env = 0

Hand(r) == {c \in Cards : holder[c] = r}
Open(r) == up[r] /\ ~held[r]

\* The queue after r's hand goes back to it, unless the witness keeps it.
Return(r, keeps) ==
  IF keeps THEN holder ELSE [c \in Cards |-> IF holder[c] = r THEN None ELSE holder[c]]
ReturnedAfter(r, keeps) == IF keeps THEN returned ELSE returned \cup Hand(r)

GoneDue(r)  == Break # "nogone" /\ up[r] /\ now - seen[r] >= Bound
RaiseDue(r) == ~up[r] /\ bus[r] /\ (Break = "manualup" => answered[r])
DealDue(c, r) == holder[c] = None /\ Open(r) /\ (Break = "noredeal" => c \notin returned)

\* rowLimits: silent for Bound, down, its hand returned in the same op.
Gone(r) ==
  /\ GoneDue(r)
  /\ up' = [up EXCEPT ![r] = FALSE]
  /\ wentDown' = [wentDown EXCEPT ![r] = TRUE]
  /\ answered' = [answered EXCEPT ![r] = FALSE]
  /\ holder' = Return(r, Break = "gonekeeps")
  /\ returned' = ReturnedAfter(r, Break = "gonekeeps")
  /\ UNCHANGED <<now, bus, seen, held, env>>

\* raiseSeen: a down row whose hook is on the bus comes up, nothing asked.
Raise(r) ==
  /\ RaiseDue(r)
  /\ up' = [up EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<now, bus, seen, held, answered, holder, returned, wentDown, env>>

\* The coordinator's hold: no deals, its hand returned in the same op.
Hold(r) ==
  /\ env < MaxEnv /\ ~held[r]
  /\ env' = env + 1
  /\ held' = [held EXCEPT ![r] = TRUE]
  /\ holder' = Return(r, Break = "holdkeeps")
  /\ returned' = ReturnedAfter(r, Break = "holdkeeps")
  /\ UNCHANGED <<now, bus, seen, up, answered, wentDown>>

Unhold(r) ==
  /\ env < MaxEnv /\ held[r]
  /\ env' = env + 1
  /\ held' = [held EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<now, bus, seen, up, answered, holder, returned, wentDown>>

\* The deal: a queued card to an open row.
Deal(c, r) ==
  /\ DealDue(c, r)
  /\ holder' = [holder EXCEPT ![c] = r]
  /\ UNCHANGED <<now, bus, seen, up, held, answered, returned, wentDown, env>>

\* The hook goes on or off the bus; a hook coming on is seen this tick.
Hook(r) ==
  /\ env < MaxEnv
  /\ env' = env + 1
  /\ bus' = [bus EXCEPT ![r] = ~@]
  /\ seen' = IF bus'[r] THEN [seen EXCEPT ![r] = now] ELSE seen
  /\ UNCHANGED <<now, up, held, answered, holder, returned, wentDown>>

\* The witness "manualup" only: the model answers, or never does.
Answer(r) ==
  /\ Break = "manualup" /\ ~up[r] /\ bus[r] /\ ~answered[r]
  /\ answered' = [answered EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<now, bus, seen, up, held, holder, returned, wentDown, env>>

\* A tick passes only when no tick step is due; rows on the bus are seen.
Advance ==
  /\ now < MaxClock
  /\ \A r \in Rows : ~GoneDue(r) /\ ~RaiseDue(r)
  /\ \A c \in Cards, r \in Rows : ~DealDue(c, r)
  /\ now' = now + 1
  /\ seen' = [r \in Rows |-> IF bus[r] THEN now + 1 ELSE seen[r]]
  /\ UNCHANGED <<bus, up, held, answered, holder, returned, wentDown, env>>

Next ==
  \/ Advance
  \/ \E r \in Rows : Gone(r) \/ Raise(r) \/ Hold(r) \/ Unhold(r) \/ Hook(r) \/ Answer(r)
  \/ \E c \in Cards, r \in Rows : Deal(c, r)

\* The run's own steps are taken whenever due (the tick); the environment
\* and a model's answer carry no fairness.
Fairness ==
  /\ \A r \in Rows : WF_vars(Gone(r)) /\ WF_vars(Raise(r))
  /\ \A c \in Cards, r \in Rows : WF_vars(Deal(c, r))

Spec == Init /\ [][Next]_vars /\ Fairness

-----------------------------------------------------------------------------
TypeOK ==
  /\ now \in 0..MaxClock
  /\ bus \in [Rows -> BOOLEAN] /\ up \in [Rows -> BOOLEAN] /\ held \in [Rows -> BOOLEAN]
  /\ answered \in [Rows -> BOOLEAN] /\ wentDown \in [Rows -> BOOLEAN]
  /\ seen \in [Rows -> 0..MaxClock]
  /\ holder \in [Cards -> Rows \cup {None}]
  /\ returned \subseteq Cards
  /\ env \in 0..MaxEnv

\* Requirement 1, down: no row is up past Bound of bus silence.
HookedWhileUp == \A r \in Rows : up[r] => now - seen[r] <= Bound

\* Requirement 2, returned: a row that is down or held holds no card.
HandEmptyWhenClosed == \A r \in Rows : ~Open(r) => Hand(r) = {}

\* Requirement 1, up: a down row whose hook is on the bus comes up, with
\* nothing done by the row, unless its hook leaves first.
AutoUp == \A r \in Rows : [](~up[r] /\ bus[r] => <>(up[r] \/ ~bus[r]))

\* Requirement 2, dealt again: a card in the queue while some row is open
\* is dealt, unless every row closes first.
Redealt == \A c \in Cards :
  [](holder[c] = None /\ (\E r \in Rows : Open(r)) => <>(holder[c] # None \/ \A r \in Rows : ~Open(r)))

\* Reach (expected violated in RowAliveNever*.cfg): a row really goes down,
\* and really comes back up by itself.
NeverDown == \A r \in Rows : ~wentDown[r]
NeverBack == \A r \in Rows : ~(wentDown[r] /\ up[r])
=============================================================================
