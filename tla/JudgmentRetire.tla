--------------------------- MODULE JudgmentRetire ---------------------------
(*
 * JudgmentRetire models the tick's retirement of orphan judgments:
 * a judgment whose primary card was dropped from the table or is absent from
 * the epoch's store.
 *
 * The bug at BASE (epoch 20, j57):
 * The tick's snapshot is sparse: placed cards plus tickExtras. When a primary
 * is dropped, it is no longer placed and not in tickExtras, so offTable sees nil
 * and never retires the judgment. An absent card takes the same nil branch.
 *
 * The fix:
 * 1. The tick loads placed cards plus cards named by open judgments.
 * 2. RetireJudgments retires any judgment whose card is not on the table ("on"),
 *    including dropped cards and absent cards.
 *
 * Invariant:
 *   NoOrphanAfterTick == ticked => \A j \in open : cards[j.card] = "on"
 *)

EXTENDS FiniteSets

CONSTANTS Cards,        \* The universe of cards
          Judgments,    \* The universe of judgment IDs
          Broken        \* "sparse": tick loads placed cards only (BASE bug); "none": fixed

VARIABLES cards,        \* [c \in Cards -> {"on", "dropped", "absent"}]
          loaded,       \* SUBSET Cards (the cards loaded into the snapshot)
          open,         \* SUBSET [id: Judgments, card: Cards]
          ticked        \* BOOLEAN (TRUE immediately after a tick)

vars == <<cards, loaded, open, ticked>>

TypeOK ==
  /\ cards \in [Cards -> {"on", "dropped", "absent"}]
  /\ loaded \subseteq Cards
  /\ open \subseteq [id: Judgments, card: Cards]
  /\ ticked \in BOOLEAN

Init ==
  /\ cards = [c \in Cards |-> "on"]
  /\ loaded = Cards
  /\ open = {}
  /\ ticked = FALSE

\* Raise a judgment on a card that is currently on the table.
Raise(j, c) ==
  /\ cards[c] = "on"
  /\ ~ \E x \in open : x.id = j
  /\ open' = open \cup {[id |-> j, card |-> c]}
  /\ ticked' = FALSE
  /\ UNCHANGED <<cards, loaded>>

\* Drop a card: outside event, the card leaves the table to dropped.
Drop(c) ==
  /\ cards[c] = "on"
  /\ cards' = [cards EXCEPT ![c] = "dropped"]
  /\ ticked' = FALSE
  /\ UNCHANGED <<loaded, open>>

\* Forget: outside event, the record is gone from the store.
Forget(c) ==
  /\ cards[c] = "dropped"
  /\ cards' = [cards EXCEPT ![c] = "absent"]
  /\ ticked' = FALSE
  /\ UNCHANGED <<loaded, open>>

\* The tick runs:
\* Under Broken = "sparse" (BASE): loads placed cards only; judgments whose card
\* was not loaded stay open.
\* Under the fix: loads placed cards plus named cards that exist in the store,
\* and retires every judgment whose card is not "on".
Tick ==
  /\ IF Broken = "sparse"
       THEN /\ loaded' = {c \in Cards : cards[c] = "on"}
            /\ open' = {j \in open : j.card \notin loaded' \/ cards[j.card] = "on"}
       ELSE /\ loaded' = {c \in Cards : cards[c] = "on"} \cup ({j.card : j \in open} \cap {c \in Cards : cards[c] # "absent"})
            /\ open' = {j \in open : cards[j.card] = "on"}
  /\ ticked' = TRUE
  /\ UNCHANGED cards

Next ==
  \/ \E j \in Judgments, c \in Cards : Raise(j, c)
  \/ \E c \in Cards : Drop(c)
  \/ \E c \in Cards : Forget(c)
  \/ Tick

Spec == Init /\ [][Next]_vars

\* Invariant: after a tick, no orphan judgment remains in open.
NoOrphanAfterTick ==
  ticked => \A j \in open : cards[j.card] = "on"

=============================================================================
