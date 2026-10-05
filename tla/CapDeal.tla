------------------------------- MODULE CapDeal -------------------------------
\* The attempt cap's default answer as the work pump's part "cap deal", run
\* before the deal (sprint.TickCapDeal, sprint.AttemptCapDeal in
\* internal/sprint/brief_bound.go; docs/SPEC-SPRINT.md, "The attempt cap's
\* default answer is a friend card"), and the deal's side of the same cards
\* (sprint.TickDeal, steps_tick.go), with the friends' side as far as a cap
\* dealt card goes (friendDeal, FriendTake, the friend stall ladder's rung 4).
\*
\* WHY ITS OWN MODULE, NOT DirtyTick. DirtyTick.tla models the tick's shape:
\* tables, queues, dirty bits and the one pump a tick. The cap deal adds no
\* table, queue or pass to that shape; it is one more part inside the pump,
\* between resolve and deal, and what it must keep is about one card's
\* attempts, its cap and redeal bound and the friends' class, status and room,
\* none of which DirtyTick holds. So the part is modelled here. DirtyTick's
\* header does not name it yet: a line there stales every DirtyTick record,
\* and the line is owed with the bench run that records them again.
\*
\* THE STATE.
\*   place[c]     ready, machine (a work card on the machine), friend, review
\*                (held there by the brief judgment), gone (landed or dropped:
\*                its other fields cleared, nothing is owed it)
\*   holder[c]    the friend a card is on ("none" off a friend)
\*   pin[c]       the friend its brief's WHO line names ("none": a machine's
\*                card); the cap deal writes it (briefGainsWho)
\*   left[c]      the friends a take-back took it from (friendsLeft,
\*                FieldTakenFrom): the friends' deal never gives it back to one
\*   att[c]       attempts since its brief last changed (attempt less
\*                brief_attempt, AtBriefBound's since)
\*   wd[c]        its current attempt's work card is withdrawn (a take ended)
\*   rd[c]        the takes of that work card that ended, up to RedealBound
\*                (redealBound, FieldTakeEnded)
\*   held[c]      its stream is held (StreamHeld, the merge table)
\*   judged[c]    the judgments open on it: "bound" (the deal's NBound, with
\*                the brief and drop decisions once capped) and "brief"
\*                (NBriefWrong, a read's at the cap in review)
\*   cap          the attempt cap (AttemptsCap; stream set --attempts and set
\*                --attempts change it while the sprint runs)
\*   status[f]    a friend's status; seen[f] the status the tick read at its
\*                start (TickReq.Friends), which both parts deal by
\*   pc           "idle" between ticks, "between" after the cap deal and
\*                before the deal: each part is its own step on a fresh read
\*                (store.tickRun.parts), so other writers act between them
\*
\* THE ACTIONS.
\*   CapDeal      the tick reads the friends, then the cap deal: every ready
\*                machine's card past the cap, its stream not held, to the
\*                frontier or heavy friend (Strong) seen up with the most free
\*                width (width less her cards, counted down as it deals); the
\*                brief gains her WHO line and the count resets (att 1: the
\*                next attempt on the new brief); the brief-defect judgment
\*                on it closes (capJudgments)
\*   Deal         the deal: a friend's card to the friend it names, seen up
\*                with room and not one it left; a machine's card at its
\*                redeal bound raises the bound's judgment (AtRedealBound, a
\*                one-tier ladder: every bound is the ceiling); below it the
\*                machine takes it when it has room, a withdrawn card at its
\*                attempt again, another as its next attempt; notify writes
\*                each judgment owed and not open and closes each open one
\*                no longer owed
\*   world        between ticks (the work table's changes queue for the
\*                pump): a machine's take ends; work on the machine or a
\*                friend finishes, and lands, is reworked below the cap, or at
\*                it waits in review under the brief judgment; a friend's
\*                card is taken back (the coordinator's friend take, or the
\*                stall ladder's rung 4: FieldTakenFrom set); a frontier or
\*                heavy friend is held down (the hold's take: not set); an
\*                answer, a friend coming up. At any time: a stream is held or
\*                released, the cap is set
\*   Answer       a judgment answered: brief (the count resets, the WHO line
\*                goes: steps_edit.go), drop, or rework below the cap
\*
\* THE RULES. Invariants: OneJudgment (never two judgments open on one
\* card), OnePlace (a card on a friend is on exactly one), WithinWidth (no
\* friend and no machine past its width). Action properties, of every deal:
\*   CapDealtToFriendFirst  a card past its cap is never dealt to the machine
\*                          while a frontier or heavy friend the tick read up
\*                          has room
\*   NoMachineAttemptPastCap  the machine never starts an attempt past the cap
\*   BoundJudgedOnce        a ready machine's card past its cap and at its
\*                          redeal bound, its stream not held (so no frontier or
\*                          heavy friend had room for the cap deal), leaves the
\*                          deal with exactly one judgment open, the bound's;
\*                          and none is left open on a card not owed it
\*   NoCardLost             every ready card its stream does not hold leaves
\*                          the deal placeable by a later tick (a machine's
\*                          card, or a friend's card whose friend it has not
\*                          left) or named by an open judgment
\*
\* THE INSTANCE (MCCapDeal.cfg): three cards, two friends (f1 frontier or
\* heavy, f2 of neither class, up with room throughout: the cap deal must
\* pass her by), one machine; widths 1, the cap 1 or 2 (set at any time),
\* the redeal bound 2
\* (so a capped card below its bound is dealt to the machine again); one
\* stream holdable (Holdable), which the race needs and which keeps the case
\* inside the gate's budget.
\*
\* GAPS. Gaps = {} is the design. Each gap is the code as it stands on
\* 2026-10-05 (nova-sprint main 6fa8077), switched on alone in its own case on
\* the design's instance (MCCapDealBroken*.cfg, tla/CASES.tsv), each trace
\* checked against the code by hand; the fix of each is owed in TickDeal and
\* friendDeal (steps_tick.go, friend_deal.go), outside the cap deal:
\*   "between"    TickDeal deals a capped card to the machine without asking
\*                whether a frontier or heavy friend has room. A capped card
\*                withdrawn below its redeal bound in a held stream is passed
\*                over by the cap deal; the stream is released (hold.go writes
\*                the stream's control card on the merge table at once, not
\*                through the work queue) before the deal, which gives it to
\*                the machine at its next generation while f1 is up with room
\*                (MCCapDealBrokenBetween breaks CapDealtToFriendFirst; stream
\*                set --attempts lowering the cap between the two parts does
\*                the same). The design: the deal leaves a
\*                capped card ready while such a friend has room, for the next
\*                tick's cap deal. The race needs the deal to have other work
\*                that tick, or the tick passes it over on its own view.
\*   "newattempt" a card reworked to ready below the cap, its cap then lowered
\*                to its count, f1 down: AtRedealBound wants a withdrawn work
\*                card and finds the attempt's finished one, so TickDeal
\*                deals it as its next attempt on the machine, past the cap
\*                (MCCapDealBrokenNewAttempt breaks NoMachineAttemptPastCap).
\*                The design: the deal raises the bound's judgment for it,
\*                brief and drop.
\*   "pinned"     a capped card dealt by the cap deal to f1 (its brief now
\*                WHO: friend f1) and taken back from her by friend take or the
\*                stall ladder's rung 4 (FieldTakenFrom set): friendDeal never
\*                gives it back to a friend it left and gives a card that
\*                names a friend to no other, TickDeal gives a friend's card to
\*                no machine, AttemptCapDeal skips a friend's card, and no
\*                judgment names it; the no-stall rule counts it held
\*                (held.go: "brief it for another friend, or drop it"), so no
\*                stalled judgment either (MCCapDealBrokenPinned breaks
\*                NoCardLost). The
\*                design: the deal raises the bound's judgment for it, brief
\*                and drop.

EXTENDS Naturals, FiniteSets

CONSTANTS NCards, Friends, Strong, FriendWidth, MachineWidth, MaxCap,
          RedealBound, Gaps

ASSUME Strong \subseteq Friends /\ MaxCap >= 1 /\ RedealBound >= 1
ASSUME Gaps \subseteq {"between", "newattempt", "pinned"}

Cards == 1..NCards
None == "none"
Places == {"ready", "machine", "friend", "review", "gone"}

VARIABLES place, holder, pin, left, att, wd, rd, held, judged, cap,
          status, seen, pc

vars == <<place, holder, pin, left, att, wd, rd, held, judged, cap,
          status, seen, pc>>
cardv == <<place, holder, pin, left, att, wd, rd, judged>>

TypeOK ==
  /\ place \in [Cards -> Places]
  /\ holder \in [Cards -> Friends \cup {None}]
  /\ pin \in [Cards -> Friends \cup {None}]
  /\ left \in [Cards -> SUBSET Friends]
  /\ att \in [Cards -> 0..MaxCap + 1]
  /\ wd \in [Cards -> BOOLEAN]
  /\ rd \in [Cards -> 0..RedealBound]
  /\ held \in [Cards -> BOOLEAN]
  /\ judged \in [Cards -> SUBSET {"bound", "brief"}]
  /\ cap \in 1..MaxCap
  /\ status \in [Friends -> {"up", "down"}]
  /\ seen \in [Friends -> {"up", "down"}]
  /\ pc \in {"idle", "between"}

Init ==
  /\ place = [c \in Cards |-> "ready"]
  /\ holder = [c \in Cards |-> None]
  /\ pin = [c \in Cards |-> None]
  /\ left = [c \in Cards |-> {}]
  /\ att = [c \in Cards |-> 0]
  /\ wd = [c \in Cards |-> FALSE]
  /\ rd = [c \in Cards |-> 0]
  /\ held = [c \in Cards |-> FALSE]
  /\ judged = [c \in Cards |-> {}]
  /\ cap \in 1..MaxCap
  \* a friend of no capped card's class stays up with room: never dealt one
  /\ status \in {s \in [Friends -> {"up", "down"}] : \A f \in Friends \ Strong : s[f] = "up"}
  /\ seen = status
  /\ pc = "idle"

Load(f) == Cardinality({c \in Cards : holder[c] = f})
OnMachine == Cardinality({c \in Cards : place[c] = "machine"})
Capped(c) == att[c] >= cap
\* the deal's AtRedealBound on a one-tier ladder: at the ceiling
AtRB(c) == wd[c] /\ rd[c] >= RedealBound
\* a frontier or heavy friend the tick read up, with room on this read
StrongRoom == \E f \in Strong : seen[f] = "up" /\ Load(f) < FriendWidth

-----------------------------------------------------------------------------
\* The cap deal (AttemptCapDeal): one plan over the ready cards in order.

CapDealable(c) == place[c] = "ready" /\ pin[c] = None /\ ~held[c] /\ Capped(c)

\* friendWithFree: the Strong friend seen up with the most free width, none
\* without room
Best(free) ==
  IF \E f \in Strong : seen[f] = "up" /\ free[f] > 0
  THEN CHOOSE f \in Strong : seen[f] = "up" /\ free[f] > 0
                             /\ \A g \in Strong : seen[g] = "up" => free[g] <= free[f]
  ELSE None

RECURSIVE Assign(_, _, _)
Assign(i, free, acc) ==
  IF i > NCards THEN acc
  ELSE IF CapDealable(i) /\ Best(free) # None
       THEN LET f == Best(free)
            IN Assign(i + 1, [free EXCEPT ![f] = @ - 1], [acc EXCEPT ![i] = f])
       ELSE Assign(i + 1, free, acc)

CapDeal ==
  /\ pc = "idle"
  /\ seen' = status
  /\ LET to == Assign(1, [f \in Friends |-> FriendWidth - Load(f)],
                      [c \in Cards |-> None])
         got == {c \in Cards : to[c] # None}
     IN /\ place' = [c \in Cards |-> IF c \in got THEN "friend" ELSE place[c]]
        /\ holder' = [c \in Cards |-> IF c \in got THEN to[c] ELSE holder[c]]
        /\ pin' = [c \in Cards |-> IF c \in got THEN to[c] ELSE pin[c]]
        /\ left' = [c \in Cards |-> IF c \in got THEN {} ELSE left[c]]
        /\ att' = [c \in Cards |-> IF c \in got THEN 1 ELSE att[c]]
        /\ wd' = [c \in Cards |-> IF c \in got THEN FALSE ELSE wd[c]]
        /\ rd' = [c \in Cards |-> IF c \in got THEN 0 ELSE rd[c]]
        /\ judged' = [c \in Cards |-> IF c \in got THEN judged[c] \ {"brief"} ELSE judged[c]]
  /\ pc' = "between"
  /\ UNCHANGED <<held, cap, status>>

-----------------------------------------------------------------------------
\* The deal (TickDeal, friendDeal, notify): one plan over the ready cards.
\* Its reads are of this step, the friends' status the tick's (seen).

\* a friend's card it may place: on the friend it names, seen up, not left
FriendPlaceable(c) == pin[c] \notin left[c] /\ seen[pin[c]] = "up"

\* owed the bound's judgment by this deal
Owed(c) ==
  /\ place[c] = "ready" /\ ~held[c]
  /\ \/ pin[c] = None /\ AtRB(c)
     \/ "newattempt" \notin Gaps /\ pin[c] = None /\ Capped(c) /\ ~wd[c] /\ ~StrongRoom
     \/ "pinned" \notin Gaps /\ pin[c] # None /\ pin[c] \in left[c]

\* a machine's card the deal may give the machine
MachineCand(c) ==
  /\ place[c] = "ready" /\ ~held[c] /\ pin[c] = None /\ ~Owed(c)
  /\ ("between" \notin Gaps /\ Capped(c)) => ~StrongRoom

RECURSIVE Pick(_, _, _)
Pick(i, room, acc) ==
  IF i > NCards \/ room = 0 THEN acc
  ELSE IF MachineCand(i) THEN Pick(i + 1, room - 1, acc \cup {i})
       ELSE Pick(i + 1, room, acc)

RECURSIVE PickF(_, _, _)
PickF(i, free, acc) ==
  IF i > NCards THEN acc
  ELSE IF place[i] = "ready" /\ ~held[i] /\ pin[i] # None /\ FriendPlaceable(i)
          /\ free[pin[i]] > 0
       THEN PickF(i + 1, [free EXCEPT ![pin[i]] = @ - 1], acc \cup {i})
       ELSE PickF(i + 1, free, acc)

Deal ==
  /\ pc = "between"
  /\ LET m == Pick(1, MachineWidth - OnMachine, {})
         fr == PickF(1, [f \in Friends |-> FriendWidth - Load(f)], {})
         owed == {c \in Cards : Owed(c)}
     IN /\ place' = [c \in Cards |-> IF c \in m THEN "machine"
                                     ELSE IF c \in fr THEN "friend" ELSE place[c]]
        /\ holder' = [c \in Cards |-> IF c \in fr THEN pin[c] ELSE holder[c]]
        \* a withdrawn card is dealt again at its attempt; else its next attempt
        /\ att' = [c \in Cards |-> IF c \in m /\ ~wd[c] THEN att[c] + 1 ELSE att[c]]
        /\ wd' = [c \in Cards |-> IF c \in m \cup fr THEN FALSE ELSE wd[c]]
        /\ rd' = [c \in Cards |-> IF c \in m /\ ~wd[c] THEN 0 ELSE rd[c]]
        /\ judged' = [c \in Cards |->
                        IF c \in owed THEN judged[c] \cup {"bound"}
                        ELSE judged[c] \ {"bound"}]
  /\ pc' = "idle"
  /\ seen' = status
  /\ UNCHANGED <<pin, left, held, cap, status>>

-----------------------------------------------------------------------------
\* The world, between ticks and between the two parts.

\* Between ticks the status is read afresh by the next tick, so seen follows it
\* there; between the two parts the tick's read stands.
\*
\* Only the pump writes the work table while the machine runs: every other
\* step queues its changes of it, and the next tick's pump drains them before
\* its cap deal (store/engine.go, sprint.QueueOf; DirtyTick.tla). So a take
\* that ends, work that finishes, a take-back and an answer reach the work
\* table between ticks (Queued); between the two parts only what other tables
\* hold changes: a stream held or released and the stream's cap (its control
\* card, the merge table).
World(A) == /\ A
            /\ seen' = IF pc = "idle" THEN status' ELSE seen
            /\ UNCHANGED pc
Queued(A) == pc = "idle" /\ World(A)

TakeEnds(c) == Queued(
  /\ place[c] = "machine"
  /\ place' = [place EXCEPT ![c] = "ready"]
  /\ wd' = [wd EXCEPT ![c] = TRUE]
  /\ rd' = [rd EXCEPT ![c] = IF @ < RedealBound THEN @ + 1 ELSE @]
  /\ UNCHANGED <<holder, pin, left, att, held, judged, cap, status>>)

Gone(c) ==
  /\ place' = [place EXCEPT ![c] = "gone"]
  /\ holder' = [holder EXCEPT ![c] = None]
  /\ pin' = [pin EXCEPT ![c] = None]
  /\ left' = [left EXCEPT ![c] = {}]
  /\ att' = [att EXCEPT ![c] = 0]
  /\ wd' = [wd EXCEPT ![c] = FALSE]
  /\ rd' = [rd EXCEPT ![c] = 0]
  /\ judged' = [judged EXCEPT ![c] = {}]

\* work finished on the machine or a friend, and its review's end: it lands;
\* below the cap it is reworked to ready (the deal cuts its next attempt); at
\* the cap a broken read raises the brief judgment and rework refuses
Finishes(c) == Queued(
  /\ place[c] \in {"machine", "friend"}
  /\ \/ Gone(c)
     \/ /\ ~Capped(c)
        /\ place' = [place EXCEPT ![c] = "ready"]
        /\ holder' = [holder EXCEPT ![c] = None]
        /\ wd' = [wd EXCEPT ![c] = FALSE]
        /\ rd' = [rd EXCEPT ![c] = 0]
        /\ UNCHANGED <<pin, left, att, judged>>
     \/ /\ Capped(c)
        /\ place' = [place EXCEPT ![c] = "review"]
        /\ holder' = [holder EXCEPT ![c] = None]
        /\ judged' = [judged EXCEPT ![c] = {"brief"}]
        /\ UNCHANGED <<pin, left, att, wd, rd>>
  /\ UNCHANGED <<held, cap, status>>)

\* friend take, or the stall ladder's rung 4: FieldTakenFrom set
TakeBack(c) == Queued(
  /\ place[c] = "friend"
  /\ place' = [place EXCEPT ![c] = "ready"]
  /\ holder' = [holder EXCEPT ![c] = None]
  /\ left' = [left EXCEPT ![c] = @ \cup {holder[c]}]
  /\ wd' = [wd EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<pin, att, rd, held, judged, cap, status>>)

\* friend down: the hold takes back every card of hers, FieldTakenFrom unset
FriendDown(f) == Queued(
  /\ f \in Strong
  /\ status[f] = "up"
  /\ status' = [status EXCEPT ![f] = "down"]
  /\ place' = [c \in Cards |-> IF holder[c] = f THEN "ready" ELSE place[c]]
  /\ wd' = [c \in Cards |-> IF holder[c] = f THEN TRUE ELSE wd[c]]
  /\ holder' = [c \in Cards |-> IF holder[c] = f THEN None ELSE holder[c]]
  /\ UNCHANGED <<pin, left, att, rd, held, judged, cap>>)

\* a friend's status between the two parts is the tick's read to both, so it
\* changes between ticks only, which loses no behaviour of theirs
FriendUp(f) == Queued(
  /\ status[f] = "down"
  /\ status' = [status EXCEPT ![f] = "up"]
  /\ UNCHANGED cardv /\ UNCHANGED <<held, cap>>)

\* a stream held or released; only the first card's stream (Holdable), which
\* is all the race needs and keeps the instance in the gate's budget
Holdable == {1}
Hold(c) == World(
  /\ held' = [held EXCEPT ![c] = ~@]
  /\ UNCHANGED cardv /\ UNCHANGED <<cap, status>>)

SetCap(n) == World(
  /\ n # cap /\ cap' = n
  /\ UNCHANGED cardv /\ UNCHANGED <<held, status>>)

\* a judgment answered: brief resets the count and the WHO line, drop drops,
\* rework (the bound below the cap) retires the bound's work card; a judgment
\* left open on a card the cap deal has moved (closed by the deal after it) is
\* not answered here: brief and rework refuse a card that is working
Answer(c) == Queued(
  /\ judged[c] # {} /\ place[c] \in {"ready", "review"}
  /\ \/ /\ place' = [place EXCEPT ![c] = "ready"]
        /\ pin' = [pin EXCEPT ![c] = None]
        /\ left' = [left EXCEPT ![c] = {}]
        /\ att' = [att EXCEPT ![c] = 0]
        /\ wd' = [wd EXCEPT ![c] = FALSE]
        /\ rd' = [rd EXCEPT ![c] = 0]
     \/ Gone(c)
     \/ /\ "brief" \notin judged[c] /\ ~Capped(c)
        /\ place' = [place EXCEPT ![c] = "ready"]
        /\ wd' = [wd EXCEPT ![c] = FALSE]
        /\ rd' = [rd EXCEPT ![c] = 0]
        /\ UNCHANGED <<pin, left, att>>
  /\ judged' = [judged EXCEPT ![c] = {}]
  /\ UNCHANGED <<holder, held, cap, status>>)

Next ==
  \/ CapDeal \/ Deal
  \/ \E c \in Cards : TakeEnds(c) \/ Finishes(c) \/ TakeBack(c) \/ Answer(c)
  \/ \E c \in Holdable : Hold(c)
  \/ \E f \in Strong : FriendDown(f) \/ FriendUp(f)
  \/ \E n \in 1..MaxCap : SetCap(n)

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
\* The rules. The deal's are action properties: each holds of every deal step
\* (Dealt: the step is the deal, and the state it leaves).

Dealt(c) == pc = "between" /\ place[c] = "ready" /\ place'[c] = "machine"

CapDealtToFriendFirst ==
  [][\A c \in Cards : Dealt(c) => ~(Capped(c) /\ StrongRoom)]_vars

NoMachineAttemptPastCap ==
  [][\A c \in Cards : Dealt(c) /\ ~wd[c] => att[c] + 1 <= cap]_vars

DealStep == pc = "between" /\ pc' = "idle"

BoundOwed ==
  \A c \in Cards :
    /\ (place[c] = "ready" /\ ~held[c] /\ pin[c] = None /\ Capped(c) /\ AtRB(c))
         => judged[c] = {"bound"}
    /\ "bound" \in judged[c] => place[c] = "ready" /\ ~held[c]
BoundJudgedOnce == [][DealStep => BoundOwed']_vars

OneJudgment == \A c \in Cards : Cardinality(judged[c]) <= 1

\* a ready card a later deal can place: a machine's card (the machine, or the
\* cap deal), or a friend's card whose friend it has not left
Placeable(c) == pin[c] = None \/ pin[c] \notin left[c]

OnePlace == \A c \in Cards : (holder[c] # None) <=> (place[c] = "friend")
Accounted ==
  \A c \in Cards : (place[c] = "ready" /\ ~held[c]) => (Placeable(c) \/ judged[c] # {})
NoCardLost == [][DealStep => Accounted']_vars

WithinWidth ==
  /\ \A f \in Friends : Load(f) <= FriendWidth
  /\ OnMachine <= MachineWidth

=============================================================================
