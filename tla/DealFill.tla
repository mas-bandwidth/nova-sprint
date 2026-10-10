------------------------------ MODULE DealFill ------------------------------
\* The deal and BounceBack (v1.2.6; card dealt-cards-a-friend-cannot-take-return;
\* internal/sprint/bounce.go, the tick part PartBounce in internal/sprint/steps_tick.go).
\*
\* The owner, 2026-10-10: "Cards need to bounce back. We can't just have cards
\* disappearing." "If they fail to do the thing they are supposed to do, you MUST be
\* notified via push." That day 43 reads were asked of a friend's reader row whose lanes
\* all failed (exit 126), 47 cards were dealt to two friends whose daemons could not take
\* them, and a friend down held 30 ready cards; each moved only on a coordinator verb.
\*
\* One machinery for work and reads (reads are consumer cards). A card is at the work stage
\* (deal, take, run) or the read stage (ask, begin, verdict); at each stage it has one
\* attempt at a time, in the pool, dealt (a work card dealt to a row, a read asked of one)
\* or taken (taken, begun). The rules this module checks, and only they:
\*   (B1) A dealt attempt not taken within Bound ticks of running time returns to the pool
\*        on the tick, when its holder has a lane free (not Busy) and has taken or finished
\*        nothing within Bound ticks (Quiet): a card waiting behind a member at work is that
\*        member's queue (rowHasRoom, rowQuiet). TickBounce is the one path that returns
\*        it; the deal's take-back retires no unstarted read.
\*   (B2) Every return is pushed to the seat in the same step, on each return path (a work
\*        card withdrawn, a read card retired).
\*   (B3) A card returned from a friend's row is not dealt to her again (the friends it has
\*        left, FieldTakenFrom then FieldFriendsLeft); one returned from a machine goes to
\*        another capable member up when there is one (lastFrom, FieldBouncedFrom), and to
\*        it again only when there is none. The deal is part of the tick and gives a pool
\*        attempt to the first capable member up in preference order (friends first), so a
\*        returned card goes to the next capable live member.
\*   (B4) The read stage asks again, every tick, until it has the reads it needs.
\*
\* Time is ages, capped one past Bound, so liveness is checked over unbounded time.
\*
\* Invariants:
\*   NoHeldPastBound   a dealt attempt held beyond Bound has a holder at work (Busy or not
\*                     Quiet): never one that does not take
\*   EveryReturnPushed on each return path, the pushes equal the returns
\* Liveness (one per stage, under fairness and one live capable member):
\*   WorkAdvances, ReadAdvances: a card at the stage eventually leaves it
\*   DealtResolves: every dealt attempt is eventually taken or returned
\*
\* Broken values (each a reversed witness of one guard):
\*   "none"        the design
\*   "nobounce"    the tick never returns a dealt attempt (breaks NoHeldPastBound)
\*   "silentwork"  a work return writes no push (breaks EveryReturnPushed)
\*   "silentread"  a read return writes no push (breaks EveryReturnPushed)
\*   "noexclude"   a returned card is dealt straight back to the row it left (breaks
\*                 WorkAdvances: the friend who never takes holds it for ever)
\*   "onceread"    a returned read is never asked again (breaks ReadAdvances)

EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
  Members,    \* every row an attempt can be dealt to
  Friends,    \* the friends among them: a card returned from one is not dealt to her again
  Pref,       \* the deal's preference order over Members (a sequence): friends first
  Cards,
  Workers,    \* the members that can work a card
  Readers,    \* the members that can be asked a read
  Up,         \* the members up (the environment, fixed)
  Able,       \* the members whose harness can take (the environment, fixed)
  NeedReads,  \* ok verdicts the read stage needs
  Bound,      \* the take bound, ticks
  Broken

None == "none"
Paths == {"work", "read"}
Stages == {"work", "read", "merge", "rework"}
AgeCap == Bound + 1

VARIABLES stage, att, holder, age, oks, asked, left, lastFrom, act, returned, pushed

vars == <<stage, att, holder, age, oks, asked, left, lastFrom, act, returned, pushed>>

Min(a, b) == IF a < b THEN a ELSE b

Capable(c, m) == IF stage[c] = "read" THEN m \in Readers ELSE m \in Workers

\* width 1: a member with a taken attempt has no lane free
Busy(m) == \E c \in Cards : holder[c] = m /\ att[c] = "taken"

\* it has taken or finished nothing within Bound ticks
Quiet(m) == act[m] >= Bound

PathOf(c) == IF stage[c] = "read" THEN "read" ELSE "work"

TypeOK ==
  /\ stage \in [Cards -> Stages]
  /\ att \in [Cards -> {"pool", "dealt", "taken", "none"}]
  /\ holder \in [Cards -> Members \cup {None}]
  /\ age \in [Cards -> 0..AgeCap]
  /\ oks \in [Cards -> 0..NeedReads]
  /\ asked \in [Cards -> BOOLEAN]
  /\ left \in [Cards -> SUBSET Friends]
  /\ lastFrom \in [Cards -> Members \cup {None}]
  /\ act \in [Members -> 0..AgeCap]
  /\ returned \in [Paths -> 0..1]
  /\ pushed \in [Paths -> 0..1]

\* The incident's start: a card at work or in review may already be dealt or asked to any
\* capable member, the down reader and the friend who never takes included.
Init ==
  /\ stage \in [Cards -> {"work", "read"}]
  /\ att \in [Cards -> {"pool", "dealt"}]
  /\ holder \in [Cards -> Members \cup {None}]
  /\ \A c \in Cards : (att[c] = "pool") <=> (holder[c] = None)
  /\ \A c \in Cards : att[c] = "dealt" => Capable(c, holder[c])
  /\ age = [c \in Cards |-> 0]
  /\ oks = [c \in Cards |-> 0]
  /\ asked = [c \in Cards |-> att[c] = "dealt"]
  /\ left = [c \in Cards |-> {}]
  /\ lastFrom = [c \in Cards |-> None]
  /\ act = [m \in Members |-> AgeCap]
  /\ returned = [p \in Paths |-> 0]
  /\ pushed = [p \in Paths |-> 0]

DealIs(c) ==
  LET may == {i \in 1..Len(Pref) : /\ Capable(c, Pref[i])
                                   /\ Pref[i] \in Up
                                   /\ Pref[i] \notin left[c]}
      other == {i \in may : Pref[i] # lastFrom[c]}
  IN IF other # {} THEN other ELSE may

CanDeal(c) ==
  /\ att[c] = "pool"
  /\ ~(Broken = "onceread" /\ stage[c] = "read" /\ asked[c])
  /\ DealIs(c) # {}

\* B3: the first capable member up in preference order the card has not left, another than
\* the one it last left when there is one.
Deal(c) ==
  /\ CanDeal(c)
  /\ LET m == Pref[CHOOSE j \in DealIs(c) : \A k \in DealIs(c) : j <= k]
     IN /\ att' = [att EXCEPT ![c] = "dealt"]
        /\ holder' = [holder EXCEPT ![c] = m]
        /\ age' = [age EXCEPT ![c] = 0]
        /\ asked' = [asked EXCEPT ![c] = (stage[c] = "read")]
  /\ UNCHANGED <<stage, oks, left, lastFrom, act, returned, pushed>>

Take(c) ==
  /\ att[c] = "dealt"
  /\ holder[c] \in Up \cap Able
  /\ ~Busy(holder[c])
  /\ att' = [att EXCEPT ![c] = "taken"]
  /\ age' = [age EXCEPT ![c] = 0]
  /\ act' = [act EXCEPT ![holder[c]] = 0]
  /\ UNCHANGED <<stage, holder, oks, asked, left, lastFrom, returned, pushed>>

Next1(c, s, o) ==
  /\ stage' = [stage EXCEPT ![c] = s]
  /\ oks' = [oks EXCEPT ![c] = o]
  /\ att' = [att EXCEPT ![c] = IF s \in {"merge", "rework"} THEN "none" ELSE "pool"]
  /\ holder' = [holder EXCEPT ![c] = None]
  /\ age' = [age EXCEPT ![c] = 0]
  /\ asked' = [asked EXCEPT ![c] = FALSE]
  /\ left' = [left EXCEPT ![c] = {}]
  /\ lastFrom' = [lastFrom EXCEPT ![c] = None]

\* The taken attempt's stage is done: work goes to read; a read's verdict counts toward
\* NeedReads (the stage asks again until it has them), or is broken and leaves for the
\* rework the rules deal; both later stages are terminal here.
Finish(c) ==
  /\ att[c] = "taken"
  /\ holder[c] \in Able
  /\ act' = [act EXCEPT ![holder[c]] = 0]
  /\ \/ stage[c] = "work" /\ Next1(c, "read", 0)
     \/ /\ stage[c] = "read"
        /\ \/ oks[c] + 1 = NeedReads /\ Next1(c, "merge", NeedReads)
           \/ oks[c] + 1 < NeedReads /\ Next1(c, "read", oks[c] + 1)
           \/ Next1(c, "rework", 0)
  /\ UNCHANGED <<returned, pushed>>

\* B1: past the bound, its holder with a lane free and quiet.
Late(c) ==
  /\ att[c] = "dealt"
  /\ age[c] + 1 > Bound
  /\ ~Busy(holder[c])
  /\ act[holder[c]] + 1 >= Bound
  /\ Broken # "nobounce"

\* B1, B2, B3: the tick ages what is held and returns the late attempts, each pushed in
\* the same step on its path, and a friend's card leaves her. The deal is a part of the
\* tick: time does not pass while a pool attempt has a member it can be dealt to.
Tick ==
  LET B == {c \in Cards : Late(c)}
      Ret(p) == {c \in B : PathOf(c) = p}
      silent(p) == Broken = "silent" \o p
  IN
  /\ \A c \in Cards : ~CanDeal(c)
  /\ att' = [c \in Cards |-> IF c \in B THEN "pool" ELSE att[c]]
  /\ holder' = [c \in Cards |-> IF c \in B THEN None ELSE holder[c]]
  /\ left' = [c \in Cards |->
               IF c \in B /\ holder[c] \in Friends /\ Broken # "noexclude"
                 THEN left[c] \cup {holder[c]} ELSE left[c]]
  /\ lastFrom' = [c \in Cards |->
               IF c \in B /\ Broken # "noexclude" THEN holder[c] ELSE lastFrom[c]]
  /\ age' = [c \in Cards |->
               IF c \in B THEN 0
               ELSE IF att[c] \in {"dealt", "taken"} THEN Min(age[c] + 1, AgeCap)
               ELSE 0]
  /\ act' = [m \in Members |-> Min(act[m] + 1, AgeCap)]
  /\ returned' = [p \in Paths |-> IF Ret(p) # {} THEN 1 ELSE returned[p]]
  /\ pushed' = [p \in Paths |-> IF Ret(p) # {} /\ ~silent(p) THEN 1 ELSE pushed[p]]
  /\ UNCHANGED <<stage, oks, asked>>

Next ==
  \/ Tick
  \/ \E c \in Cards : Deal(c) \/ Take(c) \/ Finish(c)

Fairness ==
  /\ WF_vars(Tick)
  /\ \A c \in Cards : WF_vars(Deal(c))
  /\ \A c \in Cards : SF_vars(Take(c))
  /\ \A c \in Cards : SF_vars(Finish(c))

Spec == Init /\ [][Next]_vars

LiveSpec == Spec /\ Fairness

\* The bounce invariant shared by both stages.
NoHeldPastBound ==
  \A c \in Cards :
    (att[c] = "dealt" /\ age[c] > Bound) => (Busy(holder[c]) \/ ~Quiet(holder[c]))

EveryReturnPushed == \A p \in Paths : pushed[p] = returned[p]

Advances(s) == \A c \in Cards : (stage[c] = s) ~> (stage[c] # s)

WorkAdvances == Advances("work")
ReadAdvances == Advances("read")

DealtResolves == \A c \in Cards : (att[c] = "dealt") ~> (att[c] # "dealt")

=============================================================================
