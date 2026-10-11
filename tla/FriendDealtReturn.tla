---------------------------- MODULE FriendDealtReturn -----------------------------
EXTENDS Naturals, FiniteSets, TLC
\* docs/SPEC-SPRINT.md section 1, a friend's card (internal/sprint/friend_deal.go):
\* the deal fills a friend's row (DealAhead times her width: two cards, a width of
\* one) with cards she takes (starts) into working. A card dealt to her and not
\* taken within the dealt bound (s.DealtMax) is returned to the pool and dealt to
\* the next capable member, and a friend that has not taken a dealt card is dealt
\* no more until she takes one (friendDealtReturn, PropFriendDealtNoMore). The
\* model checks the gate, the bound, the pin and the status: each card carries a
\* dealt age (age) and a pin (PinnedTo); Return is the tick's step that returns a
\* friend's cards that are past the bound and not her hard pin, then marks her
\* dealt no more; Start is her take, Progress is a ready card carrying her
\* progress (friendStarted) and Run is a job named on her beat (f.Running), each
\* of which counts as taken (friendHasTaken) and clears the mark. Return leaves
\* alone a friend who has taken, and it runs only for a friend who is up (up).
\* BadGate = "deal" breaks the gate (the reversed witness DealtNoMoreHolds);
\* BadReturn = "mark" breaks the return's leave-alone wholesale (the tick marks a
\* friend dealt no more even though a card she started is working on her row),
\* BadReturn = "ready" breaks only the ready-with-progress half (a ready card
\* carrying progress is invisible), and BadReturn = "returnpinned" breaks the pin
\* skip (the tick returns a hard-pinned card; the reversed witness
\* PinnedNeverReturned). The fleet worker has no take of its own, as the deal
\* fills it as it fills a member; a friend's card is ready until she starts it.
\* The friendUnstartedLevel defect (a stale card moved onto a marked friend with
\* no friendDealable check, internal/sprint/friend_start.go:249) is not modelled:
\* it is the v1.2.14 candidate and its fix card brings the model.
CONSTANT BadGate, BadReturn

DealtMax == 1

Cards == {"c1", "c2", "c3", "c4"}
Friends == {"a", "b"}
Workers == Friends \cup {"fleet"}

\* c4 is the one hard-pinned card (WHO: only friend a; pinnedTo, friend_deal.go:96):
\* the deal hands it to a alone, and the return never hands it back.
PinnedTo(c) == IF c = "c4" THEN "a" ELSE "none"
Pinned(c) == PinnedTo(c) \in Friends

VARIABLES owner, started, progress, dealtNoMore, age, running, up
vars == <<owner, started, progress, dealtNoMore, age, running, up>>

Init == /\ owner = [c \in Cards |-> "none"]
        /\ started = {}
        /\ progress = {}
        /\ dealtNoMore = {}
        /\ age = [c \in Cards |-> 0]
        /\ running = {}
        /\ up = Friends

Held(w) == {c \in Cards : owner[c] = w}
Room(w) == Cardinality(Held(w)) < 2            \* DealAhead (two) times a width of one
Dealable(f) == f \notin dealtNoMore

\* friendHasTaken (friend_deal.go:242): a friend has taken a card when her beat
\* names a job running (running), or a card of hers is started (started) or
\* carries her progress (progress).
Taken(f) == f \in running \/ (\E c \in Cards : owner[c] = f /\ c \in (started \cup progress))

\* the deal: a card with no owner goes to a worker with room; a hard-pinned card
\* goes to its friend alone; a friend marked dealt no more is dealt nothing (the
\* gate), unless BadGate = "deal" breaks it.
Deal(c, w) == /\ owner[c] = "none"
              /\ w \in Workers
              /\ Room(w)
              /\ (PinnedTo(c) /= "none" => w = PinnedTo(c))
              /\ (w \in Friends => (Dealable(w) \/ BadGate = "deal"))
              /\ owner' = [owner EXCEPT ![c] = w]
              /\ age' = [age EXCEPT ![c] = 0]
              /\ UNCHANGED <<started, progress, dealtNoMore, running, up>>

\* a friend's take of a card: it is working once she starts it, and her start
\* clears the dealt-no-more mark (until she stops taking again).
Start(c) == /\ owner[c] \in Friends /\ c \notin started /\ c \notin progress
            /\ started' = started \cup {c}
            /\ dealtNoMore' = dealtNoMore \ {owner[c]}
            /\ UNCHANGED <<owner, progress, age, running, up>>

\* a ready card carrying her progress is started (friendStarted): the tick's
\* friendStartUnits promotes it to working this same tick, and her start clears
\* the dealt-no-more mark, as a take does.
Progress(c) == /\ owner[c] \in Friends /\ c \notin started /\ c \notin progress
               /\ progress' = progress \cup {c}
               /\ dealtNoMore' = dealtNoMore \ {owner[c]}
               /\ UNCHANGED <<owner, started, age, running, up>>

\* a job named on her beat (f.Running) counts as taken, and clears the mark as a
\* take does.
Run(f) == /\ f \in Friends /\ f \notin running
          /\ running' = running \cup {f}
          /\ dealtNoMore' = dealtNoMore \ {f}
          /\ UNCHANGED <<owner, started, progress, age, up>>

StopRun(f) == /\ f \in Friends /\ f \in running
              /\ running' = running \ {f}
              /\ UNCHANGED <<owner, started, progress, dealtNoMore, age, up>>

Up(f) == /\ f \in Friends /\ f \notin up
          /\ up' = up \cup {f}
          /\ UNCHANGED <<owner, started, progress, dealtNoMore, age, running>>

Down(f) == /\ f \in Friends /\ f \in up
            /\ up' = up \ {f}
            /\ UNCHANGED <<owner, started, progress, dealtNoMore, age, running>>

\* the tick ages each card a friend holds toward the dealt bound.
Age == /\ age' = [c \in Cards |-> IF owner[c] \in Friends /\ age[c] <= DealtMax THEN age[c] + 1 ELSE age[c]]
       /\ UNCHANGED <<owner, started, progress, dealtNoMore, running, up>>

\* the cards the return hands back: ready on her row (not started, not carrying
\* progress), past the dealt bound, and not her hard pin — unless the broken
\* returnpinned drops the pin skip.
Returnable(f) == {c \in Cards : /\ owner[c] = f
                                /\ c \notin started /\ c \notin progress
                                /\ age[c] > DealtMax
                                /\ (PinnedTo(c) = "none" \/ BadReturn = "returnpinned")}

\* the return's leave-alone gate: a friend who has taken a card is left alone.
\* BadReturn = "mark" drops the gate outright; BadReturn = "ready" drops only the
\* progress half (a ready card carrying progress is invisible).
LeaveAlone(f) == \/ BadReturn = "mark"
                 \/ BadReturn = "ready" /\ f \notin running /\ (\A c \in Cards : owner[c] = f => c \notin started)
                 \/ ~Taken(f)

\* the tick's return past the bound, for an up friend: every card of hers that is
\* past the bound and unpinned goes back to the pool, and she is dealt no more
\* until she takes one.
Return(f) == /\ f \in Friends
             /\ f \in up
             /\ LeaveAlone(f)
             /\ Returnable(f) /= {}
             /\ dealtNoMore' = dealtNoMore \cup {f}
             /\ owner' = [c \in Cards |-> IF c \in Returnable(f) THEN "none" ELSE owner[c]]
             /\ UNCHANGED <<started, progress, age, running, up>>

Next == \/ \E c \in Cards, w \in Workers : Deal(c, w)
        \/ \E c \in Cards : Start(c) \/ Progress(c)
        \/ \E f \in Friends : Return(f) \/ Run(f) \/ StopRun(f) \/ Up(f) \/ Down(f)
        \/ Age

Spec == Init /\ [][Next]_vars

\* the gate (friendDealable, friend_deal.go:216): the deal hands a marked friend
\* nothing, so no new deal puts a card on a marked friend (the code keeps pinned
\* and not-yet-past cards on her, so "she holds none" is not the rule).
DealtNoMoreHolds == [][\A c \in Cards, w \in Friends : Deal(c, w) => Dealable(w)]_vars

\* a friend who has taken a card (running, started or progress) is never marked
\* dealt no more: the return leaves her alone, and her take clears the mark.
TakenNeverMarked == \A f \in Friends : f \in dealtNoMore => ~Taken(f)

\* a hard-pinned card is never returned to the pool.
PinnedNeverReturned == [][\A c \in Cards : owner[c] \in Friends /\ owner'[c] = "none" => ~Pinned(c)]_vars

TypeOK == /\ owner \in [Cards -> Workers \cup {"none"}]
          /\ started \subseteq Cards
          /\ progress \subseteq Cards
          /\ dealtNoMore \subseteq Friends
          /\ age \in [Cards -> 0 .. (DealtMax + 1)]
          /\ running \subseteq Friends
          /\ up \subseteq Friends
=============================================================================
