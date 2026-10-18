--------------------------- MODULE JudgmentRetire ---------------------------
(*
 * JudgmentRetire models the tick's retirement of orphan judgments:
 * a judgment whose primary card was dropped from the table or is absent from
 * the epoch's store, or whose friend has left the friends table.
 *
 * The bugs:
 * 1. "sparse" (epoch 20, j57): the tick loads placed cards only; judgments
 *    whose card was dropped or absent stay open.
 * 2. "friend_stays" (epoch 21, 9bfad3ee / 26da1b92 / a9d81398): a friend is
 *    taken off the friends table while an open judgment names that friend;
 *    without retiring judgments naming off-table friends, the judgment stays
 *    open after the tick and violates rule 8.
 *
 * The fix:
 * 1. The tick loads placed cards plus cards named by open judgments.
 * 2. RetireJudgments retires any judgment whose card is not on the work table
 *    ("on") or whose friend is not on the friends table ("on").
 *
 * Invariant:
 *   NoOrphanAfterTick == ticked => \A j \in open : SubjectOn(j.subject)
 *
 * The deal-class judgment whose cause is gone (2026-10-17): the deal raised
 * "no member up satisfies a card's GATE-NEEDS" as a note of its own plan
 * (steps_work.go dealPlan, gateNeedsJudgment), outside the tick's notify, so
 * nothing closed it when its cause ended. Raised during a server restart while
 * only the studio was up, it held 4 cards and 25 behind them for 35 minutes
 * after the Linux members were back, raised again every 10 minutes, and no
 * verb answered it. gateNeed is its condition (no member up, quiet-free and in
 * the card's bench, carries the card's GATE-NEEDS facts); FleetChange is the
 * outside event that flips it (a member up or down, a facts change, a brief
 * edit). The fix: the tick's deal judges the condition as a cond of notify
 * (steps_tick.go TickDeal), which raises it while it holds and closes it in
 * the same tick when it does not.
 * 3. "gate_stays": the judgment is raised but never closed (the bug).
 *
 * Invariants:
 *   GateClosedAfterTick == ticked => (gateOpen => gateNeed)
 *     a judgment whose condition is false is closed within one tick
 *   GateRaisedAfterTick == ticked => (gateNeed => gateOpen)
 *     a condition that holds is never silent after a tick
 *
 * The STOPPED machine's retire (2026-10-17, v1.5.4): the tick retires only
 * while the machine runs, and start refuses on any check violation, so a
 * judgment whose cause is gone (j24647: the need-blocked judgment of "an
 * earlier card", a bogus need already taken off every card, past its due time:
 * a rule 12 violation) held the machine stopped with nothing to close it. The
 * fix: tick --retire-only (sprint.RetireOnly) runs the retire checks alone, as
 * the machine, STOPPED or RUNNING, and moves no card. A need-blocked
 * judgment's subject is its need, here a subject that is no longer "on" when
 * no card needs it. running is the machine RUNNING; retired is TRUE right
 * after a retire-only.
 * 4. "stopped_stays": a STOPPED machine has no retire (the bug).
 *
 * Invariant:
 *   NoOrphanAfterRetire == retired => \A j \in open : SubjectOn(j.subject)
 *)

EXTENDS FiniteSets

CONSTANTS Cards,        \* The universe of cards
          Judgments,    \* The universe of judgment IDs
          Broken        \* "sparse" | "friend_stays" | "none"

Friends == {"f1"}

Subjects == Cards \cup Friends

VARIABLES cards,        \* [c \in Cards -> {"on", "dropped", "absent"}]
          friends,      \* [f \in Friends -> {"on", "off"}]
          loaded,       \* SUBSET Cards (the cards loaded into the snapshot)
          open,         \* SUBSET [id: Judgments, subject: Subjects]
          ticked,       \* BOOLEAN (TRUE immediately after a tick)
          gateNeed,     \* BOOLEAN: no member up satisfies the card's GATE-NEEDS
          gateOpen,     \* BOOLEAN: the gate-needs judgment is open
          running,      \* BOOLEAN: the machine is RUNNING
          retired       \* BOOLEAN (TRUE immediately after a tick --retire-only)

vars == <<cards, friends, loaded, open, ticked, gateNeed, gateOpen, running, retired>>

SubjectOn(s) ==
  IF s \in Cards THEN cards[s] = "on" ELSE friends[s] = "on"

TypeOK ==
  /\ cards \in [Cards -> {"on", "dropped", "absent"}]
  /\ friends \in [Friends -> {"on", "off"}]
  /\ loaded \subseteq Cards
  /\ open \subseteq [id: Judgments, subject: Subjects]
  /\ ticked \in BOOLEAN
  /\ gateNeed \in BOOLEAN
  /\ gateOpen \in BOOLEAN
  /\ running \in BOOLEAN
  /\ retired \in BOOLEAN

Init ==
  /\ cards = [c \in Cards |-> "on"]
  /\ friends = [f \in Friends |-> "on"]
  /\ loaded = Cards
  /\ open = {}
  /\ ticked = FALSE
  /\ gateNeed = FALSE
  /\ gateOpen = FALSE
  /\ running = TRUE
  /\ retired = FALSE

\* Raise a judgment on a card that is currently on the work table.
Raise(j, c) ==
  /\ cards[c] = "on"
  /\ ~ \E x \in open : x.id = j
  /\ open' = open \cup {[id |-> j, subject |-> c]}
  /\ ticked' = FALSE
  /\ UNCHANGED <<cards, friends, loaded, gateNeed, gateOpen, running>> /\ retired' = FALSE

\* Raise a judgment on a friend that is currently on the friends table.
RaiseFriend(j, f) ==
  /\ friends[f] = "on"
  /\ ~ \E x \in open : x.id = j
  /\ open' = open \cup {[id |-> j, subject |-> f]}
  /\ ticked' = FALSE
  /\ UNCHANGED <<cards, friends, loaded, gateNeed, gateOpen, running>> /\ retired' = FALSE

\* Drop a card: outside event, the card leaves the work table to dropped.
Drop(c) ==
  /\ cards[c] = "on"
  /\ cards' = [cards EXCEPT ![c] = "dropped"]
  /\ ticked' = FALSE
  /\ UNCHANGED <<friends, loaded, open, gateNeed, gateOpen, running>> /\ retired' = FALSE

\* Forget: outside event, the record is gone from the store.
Forget(c) ==
  /\ cards[c] = "dropped"
  /\ cards' = [cards EXCEPT ![c] = "absent"]
  /\ ticked' = FALSE
  /\ UNCHANGED <<friends, loaded, open, gateNeed, gateOpen, running>> /\ retired' = FALSE

\* FriendOff: outside event, the friend is taken off the friends table.
FriendOff(f) ==
  /\ friends[f] = "on"
  /\ friends' = [friends EXCEPT ![f] = "off"]
  /\ ticked' = FALSE
  /\ UNCHANGED <<cards, loaded, open, gateNeed, gateOpen, running>> /\ retired' = FALSE

\* The tick runs:
\* Under Broken = "sparse": loads placed cards only; judgments whose card was
\* not loaded stay open.
\* Under Broken = "friend_stays": retires off-table cards, but leaves judgments
\* naming off-table friends open.
\* Under Broken = "none" (the fix): retires every judgment whose card or friend
\* is no longer "on" its table.
Tick ==
  /\ running
  /\ IF Broken = "sparse"
       THEN /\ loaded' = {c \in Cards : cards[c] = "on"}
            /\ open' = {j \in open :
                          IF j.subject \in Cards
                            THEN j.subject \notin loaded' \/ cards[j.subject] = "on"
                            ELSE friends[j.subject] = "on"}
       ELSE /\ loaded' = {c \in Cards : cards[c] = "on"} \cup ({j.subject : j \in open} \cap {c \in Cards : cards[c] # "absent"})
            /\ IF Broken = "friend_stays"
                 THEN open' = {j \in open : IF j.subject \in Cards THEN cards[j.subject] = "on" ELSE TRUE}
                 ELSE open' = {j \in open : SubjectOn(j.subject)}
  \* the deal's gate-needs cond (steps_tick.go TickDeal, notify): raised while it
  \* holds, closed in the same tick when it does not; "gate_stays" never closes
  /\ gateOpen' = IF Broken = "gate_stays" THEN gateOpen \/ gateNeed ELSE gateNeed
  /\ ticked' = TRUE
  /\ retired' = FALSE
  /\ UNCHANGED <<cards, friends, gateNeed, running>>

\* Stop and Start: the coordinator's stop and start of the machine.
Stop ==
  /\ running /\ running' = FALSE
  /\ ticked' = FALSE /\ retired' = FALSE
  /\ UNCHANGED <<cards, friends, loaded, open, gateNeed, gateOpen>>

Start ==
  /\ ~running /\ running' = TRUE
  /\ ticked' = FALSE /\ retired' = FALSE
  /\ UNCHANGED <<cards, friends, loaded, open, gateNeed, gateOpen>>

\* RetireOnly (tick --retire-only, sprint.RetireOnly): on a STOPPED machine the
\* retire checks alone, loading as the tick loads (placed cards and the cards
\* open judgments name), close every judgment whose subject is gone; no card
\* moves, and the machine stays STOPPED. "stopped_stays" retires nothing.
RetireOnly ==
  /\ ~running
  /\ loaded' = {c \in Cards : cards[c] = "on"} \cup ({j.subject : j \in open} \cap {c \in Cards : cards[c] # "absent"})
  /\ open' = IF Broken = "stopped_stays" THEN open ELSE {j \in open : SubjectOn(j.subject)}
  /\ retired' = TRUE /\ ticked' = FALSE
  /\ UNCHANGED <<cards, friends, gateNeed, gateOpen, running>>

\* FleetChange: outside event, the gate-needs condition flips (a member whose
\* facts carry the need comes up or goes down, or the card is briefed again).
FleetChange ==
  /\ gateNeed' = ~gateNeed
  /\ ticked' = FALSE
  /\ UNCHANGED <<cards, friends, loaded, open, gateOpen, running>> /\ retired' = FALSE

Next ==
  \/ \E j \in Judgments, c \in Cards : Raise(j, c)
  \/ \E j \in Judgments, f \in Friends : RaiseFriend(j, f)
  \/ \E c \in Cards : Drop(c)
  \/ \E c \in Cards : Forget(c)
  \/ \E f \in Friends : FriendOff(f)
  \/ FleetChange
  \/ Tick
  \/ Stop \/ Start \/ RetireOnly

Spec == Init /\ [][Next]_vars

\* Invariant: after a tick, no orphan judgment remains in open.
NoOrphanAfterTick ==
  ticked => \A j \in open : SubjectOn(j.subject)

\* A judgment whose condition is false is closed within one tick.
GateClosedAfterTick == ticked => (gateOpen => gateNeed)

\* A condition that holds is judged within one tick, never a silent wait.
GateRaisedAfterTick == ticked => (gateNeed => gateOpen)

\* After a retire-only, no orphan judgment remains open, STOPPED as it is.
NoOrphanAfterRetire == retired => \A j \in open : SubjectOn(j.subject)

\* A retire-only moves no card and leaves the machine STOPPED.
RetireMovesNothing == [][retired' => UNCHANGED <<cards, friends, running>>]_vars

=============================================================================
