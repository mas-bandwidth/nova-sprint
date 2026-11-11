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
(*     the queue, and dealt again to a row that is open. The op that      *)
(*     closes the row returns its hand (modules/steps/holds.go handMoves), *)
(*     except the cards it cannot touch this tick: one an earlier step of  *)
(*     the tick claimed, one whose producer is not free, one whose         *)
(*     withdraw the card machine refuses, and one dealt between the read   *)
(*     the close was planned on and its commit. Those are returned by the  *)
(*     holds step of the next tick (holds.go holds). Here they are the     *)
(*     environment's Skip and RaceDeal, and the next tick's Sweep.         *)
(*                                                                         *)
(* Two stated exceptions to 1, both the code's on purpose:                *)
(*  - A tick whose `nova-bus who` read failed is no evidence of silence:  *)
(*    every row's last-seen moves forward with the clock (tick.go Tick,    *)
(*    o.Unread), so no row goes down during a bus outage (UnreadAdvance).  *)
(*  - Last-seen lives in the run's memory: a restarted run counts every    *)
(*    row's silence afresh from its start (Restart).                       *)
(* HookedWhileUp counts silence as the run counts it, so both hold it.    *)
(*                                                                         *)
(* The run ticks every second and Bound is minutes, so time moves only    *)
(* when no tick step is due (Advance). seen is the run's Seen: the last    *)
(* tick each row's hook was on the bus. Kinds are not modelled: since     *)
(* v2.0.42 no rule reads a row's kind for up or down, and that is the      *)
(* property (v2.0.36 to v2.0.41 raised a friend only on a pong its model   *)
(* ran by hand; Break = "manualup" is that rule, and it breaks AutoUp).   *)
(*                                                                         *)
(* Not yet trace-bound: the code's walk emits no RowAlive traces (owed).  *)
(*                                                                         *)
(* Break names one reversed witness (RowAlive*.cfg, run by ci-tlc.sh):    *)
(*   "none"      the code's rules: every property holds;                  *)
(*   "manualup"  a down row comes up only on an answer its model may never *)
(*               give (v2.0.36..v2.0.41 for friends): AutoUp fails;        *)
(*   "nogone"    no Gone: a row off the bus stays up: HookedWhileUp fails; *)
(*   "gonekeeps" the Gone keeps the hand: HandEmptyWhenClosed fails;       *)
(*   "holdkeeps" the hold keeps the hand: HandEmptyWhenClosed fails;       *)
(*   "nosweep"   no holds step: a skipped card stays on the closed row:    *)
(*               ReturnedByNextTick fails;                                 *)
(*   "noredeal"  a returned card is never dealt again: Redealt fails.      *)
(***************************************************************************)
EXTENDS Naturals, Integers

CONSTANTS
  Bound,     \* ticks of bus silence after which an up row goes down (RowSilentGone)
  MaxClock,  \* model bound on time
  MaxEnv,    \* model bound on environment moves
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
  skipped,   \* cards this tick's ops cannot touch (claimed, not free, refused, raced)
  owed,      \* cards left on a closed row by the last tick: this tick's holds returns them
  owedAt,    \* card -> the tick it became owed
  returned,  \* cards returned to the queue at least once (ghost)
  wentDown,  \* row -> it has gone down (ghost, for the reach checks)
  env        \* environment moves so far

vars == <<now, bus, seen, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown, env>>

Init ==
  /\ now = 0
  /\ bus \in [Rows -> BOOLEAN]
  /\ seen = [r \in Rows |-> 0]
  /\ up = [r \in Rows |-> TRUE]
  /\ held = [r \in Rows |-> FALSE]
  /\ answered = [r \in Rows |-> FALSE]
  /\ holder = [c \in Cards |-> None]
  /\ skipped = {}
  /\ owed = {}
  /\ owedAt = [c \in Cards |-> 0]
  /\ returned = {}
  /\ wentDown = [r \in Rows |-> FALSE]
  /\ env = 0

Hand(r) == {c \in Cards : holder[c] = r}
Open(r) == up[r] /\ ~held[r]
OnClosed(c) == holder[c] # None /\ ~Open(holder[c])

\* What a closing op of r returns: its hand but the skipped cards; nothing
\* when the witness keeps it.
Returns(r, keeps) == IF keeps THEN {} ELSE Hand(r) \ skipped
Release(S) == [c \in Cards |-> IF c \in S THEN None ELSE holder[c]]

GoneDue(r)  == Break # "nogone" /\ up[r] /\ now - seen[r] >= Bound
RaiseDue(r) == ~up[r] /\ bus[r] /\ (Break = "manualup" => answered[r])
DealDue(c, r) == holder[c] = None /\ Open(r) /\ c \notin skipped /\ (Break = "noredeal" => c \notin returned)
SweepDue(r) == Break # "nosweep" /\ ~Open(r) /\ (Hand(r) \ skipped) # {}

\* rowLimits: silent for Bound, down, its hand returned in the same op.
Gone(r) ==
  /\ GoneDue(r)
  /\ up' = [up EXCEPT ![r] = FALSE]
  /\ wentDown' = [wentDown EXCEPT ![r] = TRUE]
  /\ answered' = [answered EXCEPT ![r] = FALSE]
  /\ holder' = Release(Returns(r, Break = "gonekeeps"))
  /\ returned' = returned \cup Returns(r, Break = "gonekeeps")
  /\ owed' = owed \ Returns(r, Break = "gonekeeps")
  /\ UNCHANGED <<now, bus, seen, held, skipped, owedAt, env>>

\* raiseSeen: a down row whose hook is on the bus comes up, nothing asked.
Raise(r) ==
  /\ RaiseDue(r)
  /\ up' = [up EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<now, bus, seen, held, answered, holder, skipped, owed, owedAt, returned, wentDown, env>>

\* The coordinator's hold: no deals, its hand returned in the same op.
Hold(r) ==
  /\ env < MaxEnv /\ ~held[r]
  /\ env' = env + 1
  /\ held' = [held EXCEPT ![r] = TRUE]
  /\ holder' = Release(Returns(r, Break = "holdkeeps"))
  /\ returned' = returned \cup Returns(r, Break = "holdkeeps")
  /\ owed' = owed \ Returns(r, Break = "holdkeeps")
  /\ UNCHANGED <<now, bus, seen, up, answered, skipped, owedAt, wentDown>>

Unhold(r) ==
  /\ env < MaxEnv /\ held[r]
  /\ env' = env + 1
  /\ held' = [held EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<now, bus, seen, up, answered, holder, skipped, owed, owedAt, returned, wentDown>>

\* The holds step: any hand left on a closed row that this tick can touch
\* goes back (owed cards from the last tick included).
Sweep(r) ==
  /\ SweepDue(r)
  /\ holder' = Release(Hand(r) \ skipped)
  /\ returned' = returned \cup (Hand(r) \ skipped)
  /\ owed' = owed \ (Hand(r) \ skipped)
  /\ UNCHANGED <<now, bus, seen, up, held, answered, skipped, owedAt, wentDown, env>>

\* The deal: a queued card to an open row.
Deal(c, r) ==
  /\ DealDue(c, r)
  /\ holder' = [holder EXCEPT ![c] = r]
  /\ UNCHANGED <<now, bus, seen, up, held, answered, skipped, owed, owedAt, returned, wentDown, env>>

\* Environment: a held card this tick's ops cannot touch (claimed by an
\* earlier step, its producer not free, its withdraw refused).
Skip(c) ==
  /\ env < MaxEnv /\ holder[c] # None /\ c \notin skipped
  /\ env' = env + 1
  /\ skipped' = skipped \cup {c}
  /\ UNCHANGED <<now, bus, seen, up, held, answered, holder, owed, owedAt, returned, wentDown>>

\* Environment: a deal planned on a view from before r closed commits after
\* it (a deal writes no row, so the close's fence does not see it).
RaceDeal(c, r) ==
  /\ env < MaxEnv /\ holder[c] = None /\ ~Open(r)
  /\ env' = env + 1
  /\ holder' = [holder EXCEPT ![c] = r]
  /\ skipped' = skipped \cup {c}
  /\ UNCHANGED <<now, bus, seen, up, held, answered, owed, owedAt, returned, wentDown>>

\* The hook goes on or off the bus; a hook coming on is seen this tick.
Hook(r) ==
  /\ env < MaxEnv
  /\ env' = env + 1
  /\ bus' = [bus EXCEPT ![r] = ~@]
  /\ seen' = IF bus'[r] THEN [seen EXCEPT ![r] = now] ELSE seen
  /\ UNCHANGED <<now, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown>>

\* The witness "manualup" only: the model answers, or never does.
Answer(r) ==
  /\ Break = "manualup" /\ ~up[r] /\ bus[r] /\ ~answered[r]
  /\ answered' = [answered EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<now, bus, seen, up, held, holder, skipped, owed, owedAt, returned, wentDown, env>>

\* Exception: a run restart counts every row's silence afresh.
Restart ==
  /\ env < MaxEnv
  /\ env' = env + 1
  /\ seen' = [r \in Rows |-> now]
  /\ UNCHANGED <<now, bus, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown>>

TickDue ==
  \/ \E r \in Rows : GoneDue(r) \/ RaiseDue(r) \/ SweepDue(r)
  \/ \E c \in Cards, r \in Rows : DealDue(c, r)

\* The next tick: what this tick skipped and is still on a closed row is
\* owed to the next tick's holds (again, if it was owed and skipped once
\* more); the skips end with the tick.
NextTick ==
  /\ skipped' = {}
  /\ owed' = {c \in Cards : (c \in owed \/ c \in skipped) /\ OnClosed(c)}
  /\ owedAt' = [c \in Cards |-> IF c \in skipped /\ OnClosed(c) THEN now + 1 ELSE owedAt[c]]

\* A tick passes only when no tick step is due; rows on the bus are seen.
Advance ==
  /\ now < MaxClock /\ ~TickDue
  /\ now' = now + 1
  /\ seen' = [r \in Rows |-> IF bus[r] THEN now + 1 ELSE seen[r]]
  /\ NextTick
  /\ UNCHANGED <<bus, up, held, answered, holder, returned, wentDown, env>>

\* Exception: a tick whose bus read failed moves every row's last-seen
\* forward with the clock: no evidence of silence.
UnreadAdvance ==
  /\ env < MaxEnv /\ now < MaxClock /\ ~TickDue
  /\ env' = env + 1
  /\ now' = now + 1
  /\ seen' = [r \in Rows |-> seen[r] + 1]
  /\ NextTick
  /\ UNCHANGED <<bus, up, held, answered, holder, returned, wentDown>>

Next ==
  \/ Advance \/ UnreadAdvance \/ Restart
  \/ \E r \in Rows : Gone(r) \/ Raise(r) \/ Hold(r) \/ Unhold(r) \/ Sweep(r) \/ Hook(r) \/ Answer(r)
  \/ \E c \in Cards : Skip(c)
  \/ \E c \in Cards, r \in Rows : Deal(c, r) \/ RaceDeal(c, r)

\* The run's own steps are taken whenever due (the tick); the environment
\* and a model's answer carry no fairness.
Fairness ==
  /\ \A r \in Rows : WF_vars(Gone(r)) /\ WF_vars(Raise(r)) /\ WF_vars(Sweep(r))
  /\ \A c \in Cards, r \in Rows : WF_vars(Deal(c, r))

Spec == Init /\ [][Next]_vars /\ Fairness

-----------------------------------------------------------------------------
TypeOK ==
  /\ now \in 0..MaxClock
  /\ bus \in [Rows -> BOOLEAN] /\ up \in [Rows -> BOOLEAN] /\ held \in [Rows -> BOOLEAN]
  /\ answered \in [Rows -> BOOLEAN] /\ wentDown \in [Rows -> BOOLEAN]
  /\ seen \in [Rows -> 0..MaxClock]
  /\ holder \in [Cards -> Rows \cup {None}]
  /\ skipped \subseteq Cards /\ owed \subseteq Cards /\ returned \subseteq Cards
  /\ owedAt \in [Cards -> 0..MaxClock]
  /\ env \in 0..MaxEnv

\* Requirement 1, down: no row is up past Bound of silence as the run counts
\* it (the two exceptions above move that count, on purpose).
HookedWhileUp == \A r \in Rows : up[r] => now - seen[r] <= Bound

\* Requirement 2, returned in the same op: a down or held row holds only the
\* cards its closing op could not touch this tick, or those owed to this
\* tick's holds.
HandEmptyWhenClosed == \A r \in Rows : ~Open(r) => Hand(r) \subseteq skipped \cup owed

\* Requirement 2, returned by the next tick: a card left on a closed row is
\* gone from it by the end of the first later tick whose ops can touch it
\* (a card skipped tick after tick waits that long; the code is the same).
ReturnedByNextTick == \A c \in owed : OnClosed(c) /\ c \notin skipped => owedAt[c] = now

\* Requirement 1, up: a down row whose hook is on the bus comes up, with
\* nothing done by the row, unless its hook leaves first.
AutoUp == \A r \in Rows : [](~up[r] /\ bus[r] => <>(up[r] \/ ~bus[r]))

\* Requirement 2, dealt again: a card in the queue while some row is open
\* is dealt, unless every row closes first.
Redealt == \A c \in Cards :
  [](holder[c] = None /\ (\E r \in Rows : Open(r)) => <>(holder[c] # None \/ \A r \in Rows : ~Open(r)))

\* Reach (expected violated in RowAliveNever*.cfg): a row really goes down,
\* comes back up by itself, and a skipped card is really owed to a tick.
NeverDown == \A r \in Rows : ~wentDown[r]
NeverBack == \A r \in Rows : ~(wentDown[r] /\ up[r])
NeverOwed == owed = {}
=============================================================================
