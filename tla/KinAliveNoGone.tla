--------------------------- MODULE KinAliveNoGone ---------------------------
\* REVERSED WITNESS of KinAlive.tla: no row is put down for silence, so a
\* dead hand keeps its cards forever. DeadHandReturned must be violated.
(***************************************************************************)
(* A kin row is up while its session answers the run's challenge (v1's    *)
(* nova-hook challenge, brought back in v2: modules/run/challenge.go,      *)
(* modules/steps/limits.go rowLimits, modules/run/tick.go raiseSeen).      *)
(*                                                                         *)
(* A kin row's bus hook can stay on the bus with no session behind it (a   *)
(* launchd hook restarted with the session gone): presence is not life.   *)
(* The run challenges each kin row on the bus every window with a fresh    *)
(* nonce, and only the session's model can answer it (nova-sprint pong     *)
(* with that nonce); here a Pong needs the model and the bus, and nothing  *)
(* else moves lastPong. The tick puts an up row with no answer for Bound   *)
(* (two windows) down and returns its hand to the queue in the same op     *)
(* (Gone with handMoves); it brings a down row up only on a pong since it  *)
(* went down, never on presence. The deal gives a queued card only to a    *)
(* row up and on the bus. Time moves only when no tick step is due: the    *)
(* run ticks every second and a window is minutes.                         *)
(*                                                                         *)
(* lastPong starts at 0: the run's start, or the first sight of the row's  *)
(* session, is the baseline (Inputs.Alive), so a restart gives each row    *)
(* one fresh window. downAt is -1 for a row that never went down.         *)
(*                                                                         *)
(* Reversed witnesses beside it (KinAlive*.tla, run by ci-tlc.sh): no     *)
(* Gone (a dead hand keeps its cards forever), presence counted as life    *)
(* (the incident), and presence raising a down row.                        *)
(***************************************************************************)
EXTENDS Naturals, Integers

CONSTANTS
  W,         \* ticks per challenge window
  MaxClock,  \* model bound on time
  MaxEnv     \* model bound on environment moves (bus and model flips)

Rows  == {"k1", "k2"}
Cards == {"c1", "c2"}
None  == "none"
Bound == 2 * W   \* two missed windows

VARIABLES
  now,       \* the clock, in ticks
  bus,       \* row -> its hook is on nova-bus
  model,     \* row -> a session's model is behind the hook (environment)
  up,        \* row -> the store's up
  alive,     \* row -> its last evidence of life, as the steps read it (Inputs.Alive)
  lastPong,  \* row -> its last counted pong (ghost: the truth alive must follow)
  downAt,    \* row -> when it last went down; -1 never
  holder,    \* card -> the row holding it, or None (the queue)
  env        \* environment moves so far

vars == <<now, bus, model, up, alive, lastPong, downAt, holder, env>>

Init ==
  /\ now = 0
  /\ bus \in [Rows -> BOOLEAN]
  /\ model \in [Rows -> BOOLEAN]
  /\ up = [r \in Rows |-> TRUE]
  /\ alive = [r \in Rows |-> 0]
  /\ lastPong = [r \in Rows |-> 0]
  /\ downAt = [r \in Rows |-> -1]
  /\ holder = [c \in Cards |-> None]
  /\ env = 0

Hand(r) == {c \in Cards : holder[c] = r}

\* The steps' rule (rowLimits): an up kin row silent for Bound goes down.
GoneDue(r)  == FALSE   \* FAULT: was up[r] /\ now - alive[r] >= Bound
\* raiseSeen: a down row on the bus comes up on a pong since it went down.
RaiseDue(r) == ~up[r] /\ bus[r] /\ lastPong[r] > downAt[r] /\ now - alive[r] < Bound

\* Gone, its hand returned to the queue in the same op.
Gone(r) ==
  /\ GoneDue(r)
  /\ up' = [up EXCEPT ![r] = FALSE]
  /\ downAt' = [downAt EXCEPT ![r] = now]
  /\ holder' = [c \in Cards |-> IF holder[c] = r THEN None ELSE holder[c]]
  /\ UNCHANGED <<now, bus, model, alive, lastPong, env>>

Raise(r) ==
  /\ RaiseDue(r)
  /\ up' = [up EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<now, bus, model, alive, lastPong, downAt, holder, env>>

\* The deal: a queued card to a row up and on the bus.
Deal(c, r) ==
  /\ holder[c] = None
  /\ up[r] /\ bus[r]
  /\ holder' = [holder EXCEPT ![c] = r]
  /\ UNCHANGED <<now, bus, model, up, alive, lastPong, downAt, env>>

\* The session answers the challenge the bus carried: only a model can.
Pong(r) ==
  /\ model[r] /\ bus[r]
  /\ lastPong[r] # now
  /\ lastPong' = [lastPong EXCEPT ![r] = now]
  /\ alive' = [alive EXCEPT ![r] = now]
  /\ UNCHANGED <<now, bus, model, up, downAt, holder, env>>

\* The hook drops or returns; the session dies or comes back.
Env(r) ==
  /\ env < MaxEnv
  /\ env' = env + 1
  /\ \/ bus' = [bus EXCEPT ![r] = ~@] /\ model' = model
     \/ model' = [model EXCEPT ![r] = ~@] /\ bus' = bus
  /\ UNCHANGED <<now, up, alive, lastPong, downAt, holder>>

\* Time moves only when no tick step is due.
Advance ==
  /\ now < MaxClock
  /\ \A r \in Rows : ~GoneDue(r) /\ ~RaiseDue(r)
  /\ now' = now + 1
  /\ UNCHANGED <<bus, model, up, alive, lastPong, downAt, holder, env>>

Next ==
  \/ Advance
  \/ \E r \in Rows : Gone(r) \/ Raise(r) \/ Pong(r) \/ Env(r)
  \/ \E c \in Cards, r \in Rows : Deal(c, r)

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
TypeOK ==
  /\ now \in 0..MaxClock
  /\ bus \in [Rows -> BOOLEAN] /\ model \in [Rows -> BOOLEAN] /\ up \in [Rows -> BOOLEAN]
  /\ alive \in [Rows -> 0..MaxClock] /\ lastPong \in [Rows -> 0..MaxClock]
  /\ downAt \in [Rows -> -1..MaxClock]
  /\ holder \in [Cards -> Rows \cup {None}]
  /\ env \in 0..MaxEnv

\* A row holds cards only while its session answered within two windows: a
\* dead hand never keeps its cards past the bound (the incident: a row on
\* the bus with no session held twelve cards and took none).
DeadHandReturned == \A c \in Cards : holder[c] # None => now - lastPong[holder[c]] <= Bound

\* A closed row holds no card.
HandEmptyWhenDown == \A r \in Rows : ~up[r] => Hand(r) = {}

\* A row that went down is up again only on an answer since: presence alone
\* never brings it back.
UpOnlyOnAnswer == \A r \in Rows : up[r] /\ downAt[r] >= 0 => lastPong[r] > downAt[r]

\* Reach (expected violated in KinAliveReach*.cfg): a row really goes down,
\* and really comes back.
KinNeverDown == \A r \in Rows : downAt[r] = -1
KinNeverBack == \A r \in Rows : ~(up[r] /\ downAt[r] >= 0)
=============================================================================
