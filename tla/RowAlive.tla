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
(*  3. The sprint follows nova-config's set of machines (Glenn,           *)
(*     2026-11-15): a row nova-config no longer names (RosterChange) is   *)
(*     retired by the tick, its whole hand returned in the same op        *)
(*     (modules/steps/roster.go retire, store RetireRow), and is then in  *)
(*     no read until its hook puts it again (Rehook). The store refuses a *)
(*     retire while the row holds a card (so a race deal refuses the      *)
(*     retire, and the next tick returns that card first: RetireDue needs *)
(*     no skipped card in the hand), and refuses any deal to a retired    *)
(*     row (so a deal planned before the retire never strands a card on   *)
(*     it: RaceDeal is not onto a retired row). A tick retires nothing    *)
(*     unless its roster read is ok: a failed read plans no tick (run      *)
(*     RosterUnread) and an empty one (no worker, no kin) is a fault      *)
(*     (roster.Empty, steps.Retirable). read is that tick's read (ReadOf). *)
(*     A hooked coordinator is never retired; the model leaves roles out, *)
(*     so that exception is the code's and its tests' alone.             *)
(*  4. A row nova-config names again comes back by itself while its hook  *)
(*     is alive: the worker's pass reads next --json, which says the row  *)
(*     is not hooked in (unhooked: retired, or its hook replaced), and    *)
(*     hooks again (modules/edge/worker/pass.go rehook_internal). The     *)
(*     hook verb refuses a worker nova-config does not name on a read     *)
(*     that is ok and not empty (verbs/hook.go), so Rehook needs it named *)
(*     or the read bad. Rehook is the worker's own step: fair.            *)
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
(*   "noredeal"  a returned card is never dealt again: Redealt fails;      *)
(*   "retireempty" retire on any read, an empty or failed one too:        *)
(*               NoRetireOnBadRead fails;                                  *)
(*   "retirekeeps" the retire keeps the hand (no withdraws, no store      *)
(*               refusal): HandEmptyWhenRetired fails;                     *)
(*   "nofence"   the store takes a deal to a retired row: a deal planned  *)
(*               before the retire strands its card: HandEmptyWhenRetired *)
(*               fails;                                                    *)
(*   "norehook"  the worker never hooks again after a retire (the pass    *)
(*               loop before this fix only logged): ReNamedComesBack     *)
(*               fails.                                                    *)
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
Reads == {"ok", "failed", "empty"}   \* a tick's roster read: read, failed, or no worker and no kin

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
  env,       \* environment moves so far
  named,     \* row -> nova-config names it (environment: machine or friend add and remove)
  read,      \* this tick's roster read (environment), one of Reads
  retired,   \* row -> retired: in no read (store RetireRow)
  badRetire  \* a retire landed on a read that was not ok (ghost)

vars == <<now, bus, seen, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown, env, named, read, retired, badRetire>>
roster == <<named, read, retired, badRetire>>

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
  /\ named = [r \in Rows |-> TRUE]
  /\ read = "ok"
  /\ retired = [r \in Rows |-> FALSE]
  /\ badRetire = FALSE

Hand(r) == {c \in Cards : holder[c] = r}
\* Closed by an op that returns its hand: down or held.
Closed(r) == ~up[r] \/ held[r]
\* Dealt to: open, in a read, and named by nova-config (steps listed).
Open(r) == ~Closed(r) /\ ~retired[r] /\ named[r]
OnClosed(c) == holder[c] # None /\ Closed(holder[c])

\* What a closing op of r returns: its hand but the skipped cards; nothing
\* when the witness keeps it.
Returns(r, keeps) == IF keeps THEN {} ELSE Hand(r) \ skipped
Release(S) == [c \in Cards |-> IF c \in S THEN None ELSE holder[c]]

\* A tick retires only on a read that is ok (steps.Retirable: not empty; a
\* failed read plans no tick).
RetireRead == Break = "retireempty" \/ read = "ok"
Unnamed(r)  == ~named[r] /\ ~retired[r] /\ RetireRead

GoneDue(r)  == Break # "nogone" /\ up[r] /\ ~retired[r] /\ now - seen[r] >= Bound
RaiseDue(r) == ~up[r] /\ ~retired[r] /\ bus[r] /\ (Break = "manualup" => answered[r])
DealDue(c, r) == holder[c] = None /\ Open(r) /\ c \notin skipped /\ (Break = "noredeal" => c \notin returned)
\* The holds step for a closed row, and the retire step's withdraws when it
\* cannot return the whole hand this tick (roster.go retire, not whole).
SweepDue(r) == Break # "nosweep" /\ ~retired[r] /\ (Closed(r) \/ Unnamed(r)) /\ (Hand(r) \ skipped) # {}
\* The retire step: nova-config does not name r and this tick can return its
\* whole hand (the store refuses a retire while r holds a card).
RetireDue(r) == Unnamed(r) /\ (Break = "retirekeeps" \/ Hand(r) \cap skipped = {})

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
  /\ UNCHANGED roster

\* raiseSeen: a down row whose hook is on the bus comes up, nothing asked.
Raise(r) ==
  /\ RaiseDue(r)
  /\ up' = [up EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<now, bus, seen, held, answered, holder, skipped, owed, owedAt, returned, wentDown, env>>
  /\ UNCHANGED roster

\* The coordinator's hold: no deals, its hand returned in the same op. The
\* verb refuses a retired row (it is in no read).
Hold(r) ==
  /\ env < MaxEnv /\ ~held[r] /\ ~retired[r]
  /\ env' = env + 1
  /\ held' = [held EXCEPT ![r] = TRUE]
  /\ holder' = Release(Returns(r, Break = "holdkeeps"))
  /\ returned' = returned \cup Returns(r, Break = "holdkeeps")
  /\ owed' = owed \ Returns(r, Break = "holdkeeps")
  /\ UNCHANGED <<now, bus, seen, up, answered, skipped, owedAt, wentDown>>
  /\ UNCHANGED roster

Unhold(r) ==
  /\ env < MaxEnv /\ held[r] /\ ~retired[r]
  /\ env' = env + 1
  /\ held' = [held EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<now, bus, seen, up, answered, holder, skipped, owed, owedAt, returned, wentDown>>
  /\ UNCHANGED roster

\* The holds step: any hand left on a closed row that this tick can touch
\* goes back (owed cards from the last tick included); and the retire
\* step's withdraws for an unnamed row whose whole hand this tick cannot
\* return.
Sweep(r) ==
  /\ SweepDue(r)
  /\ holder' = Release(Hand(r) \ skipped)
  /\ returned' = returned \cup (Hand(r) \ skipped)
  /\ owed' = owed \ (Hand(r) \ skipped)
  /\ UNCHANGED <<now, bus, seen, up, held, answered, skipped, owedAt, wentDown, env>>
  /\ UNCHANGED roster

\* The retire step (steps/roster.go retire): the whole hand withdrawn and
\* the row retired, one op; unhooked, down and not held (store RetireRow).
Retire(r) ==
  /\ RetireDue(r)
  /\ retired' = [retired EXCEPT ![r] = TRUE]
  /\ badRetire' = (badRetire \/ read # "ok")
  /\ up' = [up EXCEPT ![r] = FALSE]
  /\ held' = [held EXCEPT ![r] = FALSE]
  /\ holder' = Release(Returns(r, Break = "retirekeeps"))
  /\ returned' = returned \cup Returns(r, Break = "retirekeeps")
  /\ owed' = owed \ Returns(r, Break = "retirekeeps")
  /\ UNCHANGED <<now, bus, seen, answered, skipped, owedAt, wentDown, env, named, read>>

\* The deal: a queued card to an open row.
Deal(c, r) ==
  /\ DealDue(c, r)
  /\ holder' = [holder EXCEPT ![c] = r]
  /\ UNCHANGED <<now, bus, seen, up, held, answered, skipped, owed, owedAt, returned, wentDown, env>>
  /\ UNCHANGED roster

\* Environment: a held card this tick's ops cannot touch (claimed by an
\* earlier step, its producer not free, its withdraw refused).
Skip(c) ==
  /\ env < MaxEnv /\ holder[c] # None /\ c \notin skipped
  /\ env' = env + 1
  /\ skipped' = skipped \cup {c}
  /\ UNCHANGED <<now, bus, seen, up, held, answered, holder, owed, owedAt, returned, wentDown>>
  /\ UNCHANGED roster

\* Environment: a deal planned on a view from before r closed commits after
\* it (a deal writes no row, so the close's fence does not see it). The
\* store refuses a deal to a retired row (mustRow), so none lands there.
RaceDeal(c, r) ==
  /\ env < MaxEnv /\ holder[c] = None /\ ~Open(r)
  /\ Break = "nofence" \/ ~retired[r]
  /\ env' = env + 1
  /\ holder' = [holder EXCEPT ![c] = r]
  /\ skipped' = skipped \cup {c}
  /\ UNCHANGED <<now, bus, seen, up, held, answered, owed, owedAt, returned, wentDown>>
  /\ UNCHANGED roster

\* The hook goes on or off the bus; a hook coming on is seen this tick.
Hook(r) ==
  /\ env < MaxEnv
  /\ env' = env + 1
  /\ bus' = [bus EXCEPT ![r] = ~@]
  /\ seen' = IF bus'[r] THEN [seen EXCEPT ![r] = now] ELSE seen
  /\ UNCHANGED <<now, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown>>
  /\ UNCHANGED roster

\* Environment: nova-config adds or removes the row's machine or friend.
RosterChange(r) ==
  /\ env < MaxEnv
  /\ env' = env + 1
  /\ named' = [named EXCEPT ![r] = ~@]
  /\ UNCHANGED <<now, bus, seen, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown, read, retired, badRetire>>

\* Environment: the next roster read is ok, fails, or comes back empty.
ReadChange(x) ==
  /\ env < MaxEnv /\ x # read
  /\ env' = env + 1
  /\ read' = x
  /\ UNCHANGED <<now, bus, seen, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown, named, retired, badRetire>>

\* The worker hooks again (pass.go rehook_internal): its hook is on the bus,
\* next says the row is not hooked in, and the hook verb takes it unless an
\* ok read does not name it. The row is back, a new row, down and not held;
\* Raise brings it up. Break = "norehook" is the worker that never does.
RehookDue(r) == Break # "norehook" /\ retired[r] /\ bus[r] /\ ~(read = "ok" /\ ~named[r])
Rehook(r) ==
  /\ RehookDue(r)
  /\ retired' = [retired EXCEPT ![r] = FALSE]
  /\ UNCHANGED <<now, bus, seen, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown, env, named, read, badRetire>>

\* The witness "manualup" only: the model answers, or never does.
Answer(r) ==
  /\ Break = "manualup" /\ ~up[r] /\ bus[r] /\ ~answered[r]
  /\ answered' = [answered EXCEPT ![r] = TRUE]
  /\ UNCHANGED <<now, bus, seen, up, held, holder, skipped, owed, owedAt, returned, wentDown, env>>
  /\ UNCHANGED roster

\* Exception: a run restart counts every row's silence afresh.
Restart ==
  /\ env < MaxEnv
  /\ env' = env + 1
  /\ seen' = [r \in Rows |-> now]
  /\ UNCHANGED <<now, bus, up, held, answered, holder, skipped, owed, owedAt, returned, wentDown>>
  /\ UNCHANGED roster

TickDue ==
  \/ \E r \in Rows : GoneDue(r) \/ RaiseDue(r) \/ SweepDue(r) \/ RetireDue(r)
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
  /\ UNCHANGED roster

\* Exception: a tick whose bus read failed moves every row's last-seen
\* forward with the clock: no evidence of silence.
UnreadAdvance ==
  /\ env < MaxEnv /\ now < MaxClock /\ ~TickDue
  /\ env' = env + 1
  /\ now' = now + 1
  /\ seen' = [r \in Rows |-> seen[r] + 1]
  /\ NextTick
  /\ UNCHANGED <<bus, up, held, answered, holder, returned, wentDown>>
  /\ UNCHANGED roster

Next ==
  \/ Advance \/ UnreadAdvance \/ Restart
  \/ \E r \in Rows : Gone(r) \/ Raise(r) \/ Hold(r) \/ Unhold(r) \/ Sweep(r) \/ Hook(r) \/ Answer(r)
  \/ \E r \in Rows : Retire(r) \/ RosterChange(r) \/ Rehook(r)
  \/ \E x \in Reads : ReadChange(x)
  \/ \E c \in Cards : Skip(c)
  \/ \E c \in Cards, r \in Rows : Deal(c, r) \/ RaceDeal(c, r)

\* The run's own steps are taken whenever due (the tick), the clock moves
\* when no step is due (Advance: the run ticks every second), and a live
\* worker hooks again (Rehook); the environment and a model's answer carry
\* no fairness.
Fairness ==
  /\ \A r \in Rows : WF_vars(Gone(r)) /\ WF_vars(Raise(r)) /\ WF_vars(Sweep(r)) /\ WF_vars(Retire(r))
  /\ \A r \in Rows : WF_vars(Rehook(r))
  /\ WF_vars(Advance)
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
  /\ named \in [Rows -> BOOLEAN] /\ read \in Reads /\ retired \in [Rows -> BOOLEAN]
  /\ badRetire \in BOOLEAN

\* Requirement 1, down: no row is up past Bound of silence as the run counts
\* it (the two exceptions above move that count, on purpose).
HookedWhileUp == \A r \in Rows : up[r] => now - seen[r] <= Bound

\* Requirement 2, returned in the same op: a down or held row holds only the
\* cards its closing op could not touch this tick, or those owed to this
\* tick's holds.
HandEmptyWhenClosed == \A r \in Rows : Closed(r) => Hand(r) \subseteq skipped \cup owed

\* Requirement 2, returned by the next tick: a card left on a closed row is
\* gone from it by the end of the first later tick whose ops can touch it
\* (a card skipped tick after tick waits that long; the code is the same).
ReturnedByNextTick == \A c \in owed : OnClosed(c) /\ c \notin skipped => owedAt[c] = now

\* Requirement 3: no card stays in the hand of a retired row.
HandEmptyWhenRetired == \A r \in Rows : retired[r] => Hand(r) = {}

\* Requirement 3, nothing lost: every card is in the queue or on a row a
\* read shows (one not retired), where a tick can return or work it.
NothingLost == \A c \in Cards : holder[c] = None \/ ~retired[holder[c]]

\* Requirement 3, the guard: nothing is retired on a read that failed or
\* came back empty.
NoRetireOnBadRead == ~badRetire

\* Requirement 1, up: a down row whose hook is on the bus comes up, with
\* nothing done by the row, unless its hook leaves first.
AutoUp == \A r \in Rows : [](~up[r] /\ bus[r] /\ ~retired[r] => <>(up[r] \/ ~bus[r] \/ retired[r]))

\* Requirement 2, dealt again: a card in the queue while some row is open
\* is dealt, unless every row closes first.
Redealt == \A c \in Cards :
  [](holder[c] = None /\ (\E r \in Rows : Open(r)) => <>(holder[c] # None \/ \A r \in Rows : ~Open(r)))

\* Requirement 3, automatic: a row nova-config does not name is retired by
\* the run, nothing done by anyone, unless nova-config names it again or the
\* read stops being ok first. A card in its hand this tick cannot touch makes
\* the retire wait for a later tick (RetireDue); the clock is fair, so that
\* tick comes, unless the model's clock bound ends first.
AutoRetire == \A r \in Rows :
  [](~named[r] /\ ~retired[r] /\ read = "ok" =>
       <>(retired[r] \/ named[r] \/ read # "ok" \/ now = MaxClock))

\* Requirement 4: a row nova-config names again, whose hook is alive, is
\* eventually up and not retired, nothing done by anyone, unless
\* nova-config stops naming it or its hook leaves first.
ReNamedComesBack == \A r \in Rows :
  [](named[r] /\ bus[r] => <>((up[r] /\ ~retired[r]) \/ ~named[r] \/ ~bus[r]))

\* Reach (expected violated in RowAliveNever*.cfg): a row really goes down,
\* comes back up by itself, a skipped card is really owed to a tick, and a
\* row really is retired.
NeverDown == \A r \in Rows : ~wentDown[r]
NeverBack == \A r \in Rows : ~(wentDown[r] /\ up[r])
NeverOwed == owed = {}
NeverRetired == \A r \in Rows : ~retired[r]
=============================================================================
