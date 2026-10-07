---------------------------- MODULE CapDeal -----------------------------
\* The attempt cap's cap deal, the tick's part before the deal (Promotion 3 of
\* 2026-10-05, PR 5348; internal/sprint/brief_bound.go, AttemptCapDeal;
\* internal/sprint/steps_tick.go, PartCapDeal and TickCapDeal, run between
\* "resolve" and "deal" on the work table). It is modelled beside DirtyTick.tla,
\* which holds the pump and the machine's deal on a card's attempts and its
\* redeal bound; this module adds the friends the cap deal is decided against
\* and the machine deal in its shadow, on a small instance: three cards, two
\* friends, one machine (width one), a stream's cap of two attempts and a
\* redeal bound of three takes (the code's MaxRedeals).
\*
\* A ready primary past its attempt cap (`since`, the attempts on the brief it
\* carries, against `Cap`; AttemptsCap and AtBriefBound with no finding) is dealt
\* as a friend card to a frontier or heavy-class friend up with room (her free
\* width), the most free first, the first by name among equals (AttemptCapDeal,
\* friendWithFree). Dealing it resets its count as a replaced brief does
\* (since := 0) and closes a brief-defect judgment open on it (capJudgments).
\* With no such friend up with room the card is the deal's: at its redeal bound
\* (`ended`, a take of the withdrawn work card ended, at MaxRedeals; AtRB,
\* redealBound) it is not dealt again, and the tick raises the cap's judgment
\* (NBriefWrong: brief defect after n attempts, the findings and the spend,
\* decisions brief and drop). Below its bound its attempt is dealt to a machine
\* again.
\*
\* THE PHASES. The pump's parts run in one order inside a tick: the cap deal over
\* the ready cards, then the deal (TickTables: resolve, cap deal, deal, accept).
\* The model keeps that order as the tick's three phases, idle -> cap -> deal ->
\* idle: the friends come up and go down and a judgment is answered between
\* ticks (idle); the cap deal takes every ready card past its cap that a
\* friend-tier friend has room for (cap); the deal then takes the rest (deal).
\* CapDone is enabled only when no cap deal is left: that is the order the code
\* fixes by running the parts in turn.
\*
\* THE INVARIANTS.
\*   FriendFirst: in the deal phase no ready card past its cap is left while a
\*     frontier or heavy friend is up with room: the deal never gives a machine
\*     a capped card a friend-tier friend could take.
\*   ExactlyOneJudgment: a card's open judgments never exceed one.
\*   NoCardLost: no card is lost between the cap deal and the deal. The cap deal
\*     passes a card over only when no friend-tier friend has room; at its
\*     redeal bound the deal then judges it, opens exactly one judgment and
\*     writes nothing onto a machine (which would clear its take_ended and lose
\*     the bound); `lostAtBound` is set by exactly that defect.
\*
\* BROKEN VALUES (the reversed witnesses).
\*   "none"          the design.
\*   "machinefirst"  the deal runs without the cap deal first: a capped card a
\*                   friend-tier friend has room for is dealt to the machine
\*                   (breaks FriendFirst).
\*   "nojudge"       a card at its redeal bound is dealt to the machine instead
\*                   of judged (breaks NoCardLost).
\*   "twice"         the bound's judgment is raised twice (breaks
\*                   ExactlyOneJudgment).
\*
\* The model reads internal/sprint/brief_bound.go's AttemptCapDeal, friendWithFree
\* and capJudgments; internal/sprint/steps_tick.go's TickCapDeal, PartCapDeal,
\* TickDeal, AtRedealBound and redealBound; internal/sprint/friend_deal.go's
\* friendDeal and friendWithFree; and the tests
\* TestTheAttemptCapJudgmentsDefaultAnswerDealsAFriendCard and
\* TestTwoCappedCardsDoNotExceedAFriendsWidth.

EXTENDS Naturals, FiniteSets

CONSTANTS Cards, Friends, Class, Width, MaxWidth, Cap, MaxRedeals, MaxSince, Broken

VARIABLES col, who, since, ended, rdl, judge, lostAtBound, status, room, phase

vars == <<col, who, since, ended, rdl, judge, lostAtBound, status, room, phase>>

Cols == {"ready", "friend", "machine", "judgment", "dropped"}
Phases == {"idle", "cap", "deal"}
FriendTier == {"frontier", "heavy"}

Min(a, b) == IF a < b THEN a ELSE b

\* The friends the cap deal may choose: up, of a friend tier class, with room.
Candidates == {f \in Friends : status[f] = "up" /\ Class[f] \in FriendTier /\ room[f] > 0}
FriendRoom == Candidates # {}
Best == CHOOSE f \in Candidates : \A g \in Candidates : room[f] >= room[g]

\* The cards a friend holds, and the machine's one slot.
Load(f) == Cardinality({c \in Cards : col[c] = "friend" /\ who[c] = f})
MachineFree == ~\E c \in Cards : col[c] = "machine"

\* The card's attempts on its brief, and the withdrawn work card's redeal bound.
PastCap(c) == since[c] >= Cap
AtBound(c) == ended[c] /\ rdl[c] >= MaxRedeals

\* The cap deal a ready card past its cap is owed, and the deal the rest.
CapWaiting(c) == col[c] = "ready" /\ PastCap(c) /\ Candidates # {}
JudgeNow(c) == col[c] = "ready" /\ AtBound(c) /\ Broken # "nojudge"
MachineNow(c) ==
  /\ col[c] = "ready"
  /\ (Broken = "nojudge" \/ ~AtBound(c))
  /\ MachineFree
  /\ (Broken = "machinefirst" \/ ~PastCap(c) \/ ~FriendRoom)
DealTodo == \E c \in Cards : JudgeNow(c) \/ MachineNow(c)

TypeOK ==
  /\ col \in [Cards -> Cols]
  /\ who \in [Cards -> Friends \cup {"m1", "none"}]
  /\ since \in [Cards -> 0..MaxSince]
  /\ ended \in [Cards -> BOOLEAN]
  /\ rdl \in [Cards -> 0..(MaxRedeals + 1)]
  /\ judge \in [Cards -> 0..2]
  /\ lostAtBound \in [Cards -> BOOLEAN]
  /\ status \in [Friends -> {"up", "down"}]
  /\ Width \in [Friends -> 1..MaxWidth]
  /\ room \in [Friends -> 0..MaxWidth]
  /\ \A f \in Friends : room[f] <= Width[f]
  /\ phase \in Phases

Init ==
  /\ col = [c \in Cards |-> "ready"]
  /\ who = [c \in Cards |-> "none"]
  /\ since = [c \in Cards |-> 0]
  /\ ended = [c \in Cards |-> FALSE]
  /\ rdl = [c \in Cards |-> 0]
  /\ judge = [c \in Cards |-> 0]
  /\ lostAtBound = [c \in Cards |-> FALSE]
  /\ status = [f \in Friends |-> "up"]
  /\ room = [f \in Friends |-> Width[f]]
  /\ phase = "idle"

\* The tick opens: the cap deal's phase.
TickStart ==
  /\ phase = "idle"
  /\ phase' = "cap"
  /\ UNCHANGED <<col, who, since, ended, rdl, judge, lostAtBound, status, room>>

\* The cap deal: a ready card past its cap goes to the friend-tier friend up with
\* the most room; her room is spent as she is given the card, and the card's
\* count resets as a replaced brief does.
CapPart(c) ==
  /\ phase = "cap"
  /\ Broken # "machinefirst"
  /\ CapWaiting(c)
  /\ LET f == Best IN
     /\ col' = [col EXCEPT ![c] = "friend"]
     /\ who' = [who EXCEPT ![c] = f]
     /\ since' = [since EXCEPT ![c] = 0]
     /\ judge' = [judge EXCEPT ![c] = 0]
     /\ room' = [room EXCEPT ![f] = room[f] - 1]
     /\ ended' = [ended EXCEPT ![c] = FALSE]
     /\ UNCHANGED <<rdl, lostAtBound, status, phase>>

\* No cap deal is left: the deal's phase opens. (The broken "machinefirst" runs
\* the deal with no cap deal at all.)
CapDone ==
  /\ phase = "cap"
  /\ (Broken = "machinefirst" \/ ~\E c \in Cards : CapWaiting(c))
  /\ phase' = "deal"
  /\ UNCHANGED <<col, who, since, ended, rdl, judge, lostAtBound, status, room>>

\* The deal: a ready card at its redeal bound is the coordinator's judgment; any
\* other is dealt to the machine, unsets the ended take and keeps its redeal
\* count. A card past its cap is dealt to the machine only when no friend-tier
\* friend has room for it.
DealPart(c) ==
  /\ phase = "deal"
  /\ LET j == JudgeNow(c)
         m == MachineNow(c)
     IN
     /\ (j \/ m)
     /\ IF j
          THEN /\ col' = [col EXCEPT ![c] = "judgment"]
               /\ judge' = [judge EXCEPT ![c] = judge[c] + (IF Broken = "twice" THEN 2 ELSE 1)]
               /\ who' = [who EXCEPT ![c] = "none"]
               /\ lostAtBound' = lostAtBound
          ELSE /\ col' = [col EXCEPT ![c] = "machine"]
               /\ who' = [who EXCEPT ![c] = "m1"]
               /\ judge' = judge
               /\ lostAtBound' = [lostAtBound EXCEPT ![c] = AtBound(c)]
     /\ ended' = IF j THEN ended ELSE [ended EXCEPT ![c] = FALSE]
     /\ UNCHANGED <<since, rdl, status, room, phase>>

\* No card the deal owes is left: the tick closes.
DealDone ==
  /\ phase = "deal"
  /\ ~DealTodo
  /\ phase' = "idle"
  /\ UNCHANGED <<col, who, since, ended, rdl, judge, lostAtBound, status, room>>

\* The friends between ticks: one comes up with her free width, or goes down.
FriendUp(f) ==
  /\ phase = "idle"
  /\ status[f] = "down"
  /\ status' = [status EXCEPT ![f] = "up"]
  /\ room' = [room EXCEPT ![f] = Width[f] - Load(f)]
  /\ UNCHANGED <<col, who, since, ended, rdl, judge, lostAtBound, phase>>

FriendDown(f) ==
  /\ phase = "idle"
  /\ status[f] = "up"
  /\ status' = [status EXCEPT ![f] = "down"]
  /\ room' = [room EXCEPT ![f] = 0]
  /\ UNCHANGED <<col, who, since, ended, rdl, judge, lostAtBound, phase>>

\* A take of a card on a machine or a friend ends without a finish: the card is
\* withdrawn to ready, its take recorded and its redeal count spent; a new
\* attempt on the same brief spends another of the cap.
AttemptEnds(c) ==
  /\ phase = "idle"
  /\ col[c] \in {"machine", "friend"}
  /\ LET f == who[c]
         back == col[c] = "friend" /\ status[f] = "up"
         free == IF back THEN [room EXCEPT ![f] = room[f] + 1] ELSE room
     IN
     /\ col' = [col EXCEPT ![c] = "ready"]
     /\ who' = [who EXCEPT ![c] = "none"]
     /\ ended' = [ended EXCEPT ![c] = TRUE]
     /\ rdl' = [rdl EXCEPT ![c] = Min(rdl[c] + 1, MaxRedeals + 1)]
     /\ since' = [since EXCEPT ![c] = Min(since[c] + 1, MaxSince)]
     /\ room' = free
     /\ UNCHANGED <<judge, lostAtBound, status, phase>>

\* The coordinator answers the cap's judgment: the brief replaced (the count and
\* the bound reset) or the card dropped.
AnswerJudgment(c) ==
  /\ phase = "idle"
  /\ col[c] = "judgment"
  /\ \/ /\ col' = [col EXCEPT ![c] = "ready"]
        /\ who' = [who EXCEPT ![c] = "none"]
        /\ since' = [since EXCEPT ![c] = 0]
        /\ ended' = [ended EXCEPT ![c] = FALSE]
        /\ rdl' = [rdl EXCEPT ![c] = 0]
        /\ judge' = [judge EXCEPT ![c] = 0]
     \/ /\ col' = [col EXCEPT ![c] = "dropped"]
        /\ who' = who
        /\ since' = since
        /\ ended' = ended
        /\ rdl' = rdl
        /\ judge' = [judge EXCEPT ![c] = 0]
  /\ UNCHANGED <<lostAtBound, status, room, phase>>

Next ==
  \/ TickStart
  \/ \E c \in Cards : CapPart(c)
  \/ CapDone
  \/ \E c \in Cards : DealPart(c)
  \/ DealDone
  \/ \E f \in Friends : FriendUp(f)
  \/ \E f \in Friends : FriendDown(f)
  \/ \E c \in Cards : AttemptEnds(c)
  \/ \E c \in Cards : AnswerJudgment(c)

Spec == Init /\ [][Next]_vars

\* THE INVARIANTS.
FriendFirst ==
  phase = "deal" =>
    \A c \in Cards : (col[c] = "ready" /\ PastCap(c)) => ~FriendRoom
ExactlyOneJudgment ==
  \A c \in Cards : judge[c] <= 1
NoCardLost ==
  \A c \in Cards : ~lostAtBound[c]

=============================================================================
