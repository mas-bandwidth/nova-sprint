---------------------------- MODULE FriendDealtReturn -----------------------------
EXTENDS Naturals, FiniteSets, TLC
\* A dealt, untaken card ages out only while its friend is up and has no work
\* started. Return records that friend in the card's left set, never in a
\* friend-wide deal ban. Started/progress/running work and hard pins stay put.
\* The model uses three cards (one pinned) and abstracts width to two cards and omits stream, role and tier filters.
CONSTANT BadGate, BadReturn

DealtMax == 1

Cards == {"c1", "c2", "c3"}
Friends == {"a", "b"}
Workers == Friends \cup {"fleet"}

PinnedTo(c) == IF c = "c3" THEN "a" ELSE "none"
Pinned(c) == PinnedTo(c) \in Friends

VARIABLES owner, started, progress, left, age, running, up
vars == <<owner, started, progress, left, age, running, up>>

Init == /\ owner = [c \in Cards |-> "none"]
        /\ started = {}
        /\ progress = {}
        /\ left = [c \in Cards |-> {}]
        /\ age = [c \in Cards |-> 0]
        /\ running = {}
        /\ up = Friends

Held(w) == {c \in Cards : owner[c] = w}
Room(w) == Cardinality(Held(w)) < 2            \* DealAhead (two) times a width of one
Dealable(c, f) == f \notin left[c]

Taken(f) == f \in running \/ (\E c \in Cards : owner[c] = f /\ c \in (started \cup progress))

Deal(c, w) == /\ owner[c] = "none"
              /\ w \in Workers
              /\ Room(w)
              /\ (PinnedTo(c) /= "none" => w = PinnedTo(c))
              /\ (w \in Friends => (Dealable(c, w) \/ BadGate = "deal"))
              /\ owner' = [owner EXCEPT ![c] = w]
              /\ age' = [age EXCEPT ![c] = 0]
              /\ UNCHANGED <<started, progress, left, running, up>>

Start(c) == /\ owner[c] \in Friends /\ c \notin started /\ c \notin progress
            /\ started' = started \cup {c}
            /\ UNCHANGED left
            /\ UNCHANGED <<owner, progress, age, running, up>>

Progress(c) == /\ owner[c] \in Friends /\ c \notin started /\ c \notin progress
               /\ progress' = progress \cup {c}
               /\ UNCHANGED left
               /\ UNCHANGED <<owner, started, age, running, up>>

Run(f) == /\ f \in Friends /\ f \notin running
          /\ running' = running \cup {f}
          /\ UNCHANGED left
          /\ UNCHANGED <<owner, started, progress, age, up>>

StopRun(f) == /\ f \in Friends /\ f \in running
              /\ running' = running \ {f}
              /\ UNCHANGED <<owner, started, progress, left, age, up>>

Up(f) == /\ f \in Friends /\ f \notin up
          /\ up' = up \cup {f}
          /\ UNCHANGED <<owner, started, progress, left, age, running>>

Down(f) == /\ f \in Friends /\ f \in up
            /\ up' = up \ {f}
            /\ UNCHANGED <<owner, started, progress, left, age, running>>

Age == /\ age' = [c \in Cards |-> IF owner[c] \in Friends /\ age[c] <= DealtMax THEN age[c] + 1 ELSE age[c]]
       /\ UNCHANGED <<owner, started, progress, left, running, up>>

Returnable(f) == {c \in Cards : /\ owner[c] = f
                                /\ c \notin started /\ c \notin progress
                                /\ age[c] > DealtMax
                                /\ (PinnedTo(c) = "none" \/ BadReturn = "returnpinned")}

LeaveAlone(f) == \/ BadReturn = "mark"
                 \/ BadReturn = "ready" /\ f \notin running /\ (\A c \in Cards : owner[c] = f => c \notin started)
                 \/ ~Taken(f)

Return(f) == /\ f \in Friends
             /\ f \in up
             /\ LeaveAlone(f)
             /\ Returnable(f) /= {}
             /\ left' = [c \in Cards |-> IF c \in Returnable(f) THEN left[c] \cup {f} ELSE left[c]]
             /\ owner' = [c \in Cards |-> IF c \in Returnable(f) THEN "none" ELSE owner[c]]
             /\ UNCHANGED <<started, progress, age, running, up>>

Next == \/ \E c \in Cards, w \in Workers : Deal(c, w)
        \/ \E c \in Cards : Start(c) \/ Progress(c)
        \/ \E f \in Friends : Return(f) \/ Run(f) \/ StopRun(f) \/ Up(f) \/ Down(f)
        \/ Age

Spec == Init /\ [][Next]_vars

ReturnedNeverRedealt == [][\A c \in Cards, w \in Friends : Deal(c, w) => Dealable(c, w)]_vars

TakenNeverReturned == [][\A f \in Friends : Taken(f) => Held(f) \subseteq Held(f)']_vars

PinnedNeverReturned == [][\A c \in Cards : owner[c] \in Friends /\ owner'[c] = "none" => ~Pinned(c)]_vars

TypeOK == /\ owner \in [Cards -> Workers \cup {"none"}]
          /\ started \subseteq Cards
          /\ progress \subseteq Cards
          /\ left \in [Cards -> SUBSET Friends]
          /\ age \in [Cards -> 0 .. (DealtMax + 1)]
          /\ running \subseteq Friends
          /\ up \subseteq Friends
=============================================================================
