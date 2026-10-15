---------------------------- MODULE FriendDealtReturn -----------------------------
EXTENDS Naturals, FiniteSets, TLC
\* A dealt, untaken card ages out only while its friend is up and has no work
\* started. Return records that friend in the card's left set, never in a
\* friend-wide deal ban. Started/progress/running work and hard pins stay put.
\* The model uses three cards (one pinned) and abstracts width to two cards and omits stream, role and tier filters.
\* A one-shot friend (session one-shot) is a member lane with an identity
\* prompt: her row ("o") is dealt as a friend's row is, but her member takes by
\* count (MemberTake: ready into mworking, no friend start receipt), finishes
\* from working (MemberFinish, into mdone), and no friend-lane step touches
\* her: Return, Start, Progress and Run name live friends alone, and her
\* working cards are never bounced for want of a friend start
\* (friendUnstartedWorking skips her seat). BadOneShot = "bounce" treats her
\* as needing a friend start: the tick bounces her member's working card back
\* to ready (the reversed witness OneShotWorkingStays).
CONSTANT BadGate, BadReturn, BadOneShot

DealtMax == 1

Cards == {"c1", "c2", "c3"}
Friends == {"a", "b"}
OneShots == {"o"}
Workers == Friends \cup OneShots \cup {"fleet"}

PinnedTo(c) == IF c = "c3" THEN "a" ELSE "none"
Pinned(c) == PinnedTo(c) \in Friends

VARIABLES owner, started, progress, left, age, running, up, mworking, mdone
vars == <<owner, started, progress, left, age, running, up, mworking, mdone>>

Init == /\ owner = [c \in Cards |-> "none"]
        /\ started = {}
        /\ progress = {}
        /\ left = [c \in Cards |-> {}]
        /\ age = [c \in Cards |-> 0]
        /\ running = {}
        /\ up = Friends
        /\ mworking = {}
        /\ mdone = {}

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
              /\ UNCHANGED <<started, progress, left, running, up, mworking, mdone>>

Start(c) == /\ owner[c] \in Friends /\ c \notin started /\ c \notin progress
            /\ started' = started \cup {c}
            /\ UNCHANGED <<owner, progress, left, age, running, up, mworking, mdone>>

Progress(c) == /\ owner[c] \in Friends /\ c \notin started /\ c \notin progress
               /\ progress' = progress \cup {c}
               /\ UNCHANGED <<owner, started, left, age, running, up, mworking, mdone>>

Run(f) == /\ f \in Friends /\ f \notin running
          /\ running' = running \cup {f}
          /\ UNCHANGED <<owner, started, progress, left, age, up, mworking, mdone>>

StopRun(f) == /\ f \in Friends /\ f \in running
              /\ running' = running \ {f}
              /\ UNCHANGED <<owner, started, progress, left, age, up, mworking, mdone>>

Up(f) == /\ f \in Friends /\ f \notin up
          /\ up' = up \cup {f}
          /\ UNCHANGED <<owner, started, progress, left, age, running, mworking, mdone>>

Down(f) == /\ f \in Friends /\ f \in up
            /\ up' = up \ {f}
            /\ UNCHANGED <<owner, started, progress, left, age, running, mworking, mdone>>

Age == /\ age' = [c \in Cards |-> IF owner[c] \in Friends /\ age[c] <= DealtMax THEN age[c] + 1 ELSE age[c]]
       /\ UNCHANGED <<owner, started, progress, left, running, up, mworking, mdone>>

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
             /\ UNCHANGED <<started, progress, age, running, up, mworking, mdone>>

\* a one-shot's member takes a card dealt to her row by count, as any fleet
\* member does: it is working from the take, with no friend start.
MemberTake(c) == /\ owner[c] \in OneShots /\ c \notin mworking /\ c \notin mdone
                 /\ mworking' = mworking \cup {c}
                 /\ UNCHANGED <<owner, started, progress, left, age, running, up, mdone>>

\* her member finishes from working: the card is done on her row, credited to
\* her (the server's claim).
MemberFinish(c) == /\ c \in mworking
                   /\ mdone' = mdone \cup {c}
                   /\ mworking' = mworking \ {c}
                   /\ UNCHANGED <<owner, started, progress, left, age, running, up>>

\* the broken tick: a one-shot treated as needing a friend start has her
\* member's working card bounced back to ready, again and again.
Bounce(c) == /\ BadOneShot = "bounce" /\ c \in mworking
             /\ mworking' = mworking \ {c}
             /\ UNCHANGED <<owner, started, progress, left, age, running, up, mdone>>

Next == \/ \E c \in Cards, w \in Workers : Deal(c, w)
        \/ \E c \in Cards : MemberTake(c) \/ MemberFinish(c) \/ Bounce(c)
        \/ \E c \in Cards : Start(c) \/ Progress(c)
        \/ \E f \in Friends : Return(f) \/ Run(f) \/ StopRun(f) \/ Up(f) \/ Down(f)
        \/ Age

Spec == Init /\ [][Next]_vars

ReturnedNeverRedealt == [][\A c \in Cards, w \in Friends : Deal(c, w) => Dealable(c, w)]_vars

TakenNeverReturned == [][\A f \in Friends : Taken(f) => Held(f) \subseteq Held(f)']_vars

PinnedNeverReturned == [][\A c \in Cards : owner[c] \in Friends /\ owner'[c] = "none" => ~Pinned(c)]_vars

\* a one-shot's working card stays working until her member finishes it: no
\* tick step bounces it for want of a friend start.
OneShotWorkingStays == [][\A c \in Cards : c \in mworking => c \in mworking' \/ c \in mdone']_vars

TypeOK == /\ owner \in [Cards -> Workers \cup {"none"}]
          /\ mworking \subseteq Cards
          /\ mdone \subseteq Cards
          /\ started \subseteq Cards
          /\ progress \subseteq Cards
          /\ left \in [Cards -> SUBSET Friends]
          /\ age \in [Cards -> 0 .. (DealtMax + 1)]
          /\ running \subseteq Friends
          /\ up \subseteq Friends
=============================================================================
