------------------------------- MODULE DealFill -------------------------------
EXTENDS Naturals, FiniteSets, TLC
\* docs/SPEC-SPRINT.md section 1, a friend's card (internal/sprint/friend_deal.go):
\* the deal fills a friend's row (DealAhead times her width: two cards, a width of
\* one) with cards she takes (starts) into working. A card dealt to her and not
\* taken within the dealt bound is returned to the pool and dealt to the next
\* capable member, and a friend that has not taken a dealt card is dealt no more
\* until she takes one (friendDealtReturn, PropFriendDealtNoMore). The model checks
\* the gate, not the bound: Return is the tick's step (the bound passed) that
\* returns a friend's untaken cards and marks her dealt no more; Start is her take
\* and clears the mark. BadGate = "deal" breaks the gate: the reversed witness
\* (DealtNoMoreHolds). The fleet worker has no take of its own, as the deal
\* fills it as it fills a member; a friend's card is ready until she starts it.
CONSTANT BadGate

Cards == {"c1", "c2", "c3", "c4"}
Friends == {"a", "b"}
Workers == Friends \cup {"fleet"}

VARIABLES owner, started, dealtNoMore
vars == <<owner, started, dealtNoMore>>

Init == /\ owner = [c \in Cards |-> "none"]
        /\ started = {}
        /\ dealtNoMore = {}

Held(w) == {c \in Cards : owner[c] = w}
Room(w) == Cardinality(Held(w)) < 2            \* DealAhead (two) times a width of one
Dealable(f) == f \notin dealtNoMore

\* the deal: a card with no owner goes to a worker with room; a friend marked
\* dealt no more is dealt nothing (the gate), unless BadGate = "deal" breaks it.
Deal(c, w) == /\ owner[c] = "none"
              /\ w \in Workers
              /\ Room(w)
              /\ (w \in Friends => (Dealable(w) \/ BadGate = "deal"))
              /\ owner' = [owner EXCEPT ![c] = w]
              /\ UNCHANGED <<started, dealtNoMore>>

\* a friend's take of a card: it is working once she starts it, and her start
\* clears the dealt-no-more mark (until she takes one).
Start(c) == /\ owner[c] \in Friends /\ c \notin started
            /\ started' = started \cup {c}
            /\ dealtNoMore' = dealtNoMore \ {owner[c]}
            /\ UNCHANGED owner

\* the tick's return past the bound: the friend holds a card and none of the
\* cards she holds is started (a friend running a job is left alone; the code's
\* friendDealtReturn skips a friend whose started card is working on her row), so
\* every card of hers goes back to the pool, and she is dealt no more until she
\* takes one.
Return(f) == /\ f \in Friends
             /\ \E c \in Cards : owner[c] = f
             /\ \A c \in Cards : owner[c] = f => c \notin started
             /\ dealtNoMore' = dealtNoMore \cup {f}
             /\ owner' = [c \in Cards |-> IF owner[c] = f /\ c \notin started THEN "none" ELSE owner[c]]
             /\ UNCHANGED started

Next == (\E c \in Cards, w \in Workers : Deal(c, w) \/ Start(c))
        \/ (\E f \in Friends : Return(f))
Spec == Init /\ [][Next]_vars

\* the gate: a friend marked dealt no more holds no card, so the deal hands her
\* none (Deal guards Dealable).
DealtNoMoreHolds == \A c \in Cards : owner[c] \in Friends => owner[c] \notin dealtNoMore

TypeOK == /\ owner \in [Cards -> Workers \cup {"none"}]
          /\ started \subseteq Cards
          /\ dealtNoMore \subseteq Friends
=============================================================================
