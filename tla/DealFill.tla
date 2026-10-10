------------------------------ MODULE DealFill ------------------------------
\* The deal, and BounceBack: one machinery for every stage a card passes through
\* (v1.2.6; card dealt-cards-a-friend-cannot-take-return; internal/sprint/bounce.go, the
\* tick part PartBounce in internal/sprint/steps_tick.go).
\*
\* The owner's words, 2026-10-10: "We can't have reviews just disappearing like this."
\* "Cards need to bounce back. We can't just have cards disappearing." "If they fail to do
\* the thing they are supposed to do, you MUST be notified via push." "This should
\* continue until the reads are done, retrying, and notifying you of failures." "Now of
\* course, fixes should be automatic too, and so should merges."
\*
\* That day 43 reads were asked of a friend's reader row whose lanes all failed (exit
\* 126) and sat in review 75 minutes; 47 cards were dealt to two friends whose daemons
\* could not take them; Zhi was down holding 30 ready cards. Each moved only on a
\* coordinator verb.
\*
\* A card passes through stages; this layer (v1.2.6) drives two of them: work (deal,
\* take, run) and read (ask, begin, verdict). A broken verdict leaves the read stage for
\* the rework the rules already deal; an ok one with the reads it needs leaves it for the
\* merge. Fixes and merges driven by the same rules are later point releases. At each stage the card has one attempt at a time: in the pool, dealt (dealt to a
\* member's row, or a read asked of a reader's row), or taken (taken, or begun).
\* Reads are consumer cards: the read stage needs NeedReads ok verdicts, asked one attempt
\* at a time here, and keeps asking until it has them.
\*
\* The rules:
\*   (B1) An attempt dealt and not taken within Bound ticks returns to the pool on the
\*        tick; an attempt taken and without progress for Wall ticks returns too. An
\*        attempt that fails (a harness fault, a failed run) returns at once. Nothing else
\*        moves a card: no coordinator verb is in this model.
\*   (B2) Every return, and every member set aside, raises a judgment pushed to the seat
\*        in the same step: never a silent move.
\*   (B3) A member whose attempt bounced or failed, or that failed its last N takes as
\*        harness faults, is set aside: dealt no more until it takes again. So that it can
\*        take again it is dealt again once Cooldown ticks have passed since it was set
\*        aside (the probe); an attempt of the probe that bounces sets it aside again, with
\*        its push. A down member is dealt nothing.
\*   (B4) Every tick, every pool attempt goes to the first capable dealable member in
\*        preference order (Pref: friends first, the fleet after), so a returned attempt
\*        goes to the next capable live member; the read stage re-asks until it has its
\*        reads.
\*
\* Time is ages: age[c] counts ticks since the attempt was dealt or last showed progress,
\* capped one past the larger bound, so liveness is checked over unbounded time.
\*
\* Invariants:
\*   NoHeldPastBound    the bounce invariant shared by all stages: no attempt stays dealt
\*                      beyond Bound, or taken without progress beyond Wall
\*   EveryBouncePushed  every return and every set-aside has its pushed judgment
\*   NoDealToBlocked    no attempt is dealt to a member down, or set aside and not due its probe
\* Liveness (one per stage, under fairness and one live capable member per stage):
\*   WorkAdvances, ReadAdvances: a card at the stage eventually leaves it.
\*   DealtResolves: every dealt attempt is eventually taken or returned.
\*
\* Broken values (each a reversed witness of one guard):
\*   "none"         the design
\*   "nobounce"     the tick never returns a dealt attempt (breaks NoHeldPastBound)
\*   "nowall"       the tick never returns a taken attempt without progress (breaks NoHeldPastBound)
\*   "silent"       a return moves the attempt with no pushed judgment (breaks EveryBouncePushed)
\*   "dealblocked"  the deal ignores down and set-aside members (breaks NoDealToBlocked)
\*   "noflag"       a return does not set its member aside: the attempt is dealt straight
\*                  back to the friend that never takes (breaks WorkAdvances)
\*   "onceread"     the read stage asks once and never re-asks a returned read: the
\*                  per-producer one-off ask (breaks ReadAdvances)

EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
  Members,    \* every row an attempt can be dealt to: friends', readers', fleet
  Pref,       \* the deal's preference order over Members (a sequence)
  Cards,
  Workers,    \* the members that can work a card
  Readers,    \* the members that can be asked a read
  Up,         \* the members that are up (the environment, fixed)
  Able,       \* the members whose harness can take an attempt (the environment, fixed)
  NeedReads,  \* ok verdicts the read stage needs
  Bound,      \* the take bound, ticks
  Wall,       \* the wall budget without progress, ticks
  Cooldown,   \* ticks a set-aside member waits before its probe
  N,          \* consecutive harness faults that set a member aside
  Broken

None == "none"
Stages == {"work", "read", "merge", "rework"}
AgeCap == IF Bound > Wall THEN Bound + 1 ELSE Wall + 1

VARIABLES stage, att, holder, age, oks, asked, fails, aside, cool, unpushed, badDeal

vars == <<stage, att, holder, age, oks, asked, fails, aside, cool, unpushed, badDeal>>

Min(a, b) == IF a < b THEN a ELSE b

Capable(c, m) == IF stage[c] = "read" THEN m \in Readers ELSE m \in Workers

\* B3: up, and either not set aside or due its probe.
Dealable(m) ==
  /\ m \in Up
  /\ ~aside[m] \/ cool[m] >= Cooldown

TypeOK ==
  /\ stage \in [Cards -> Stages]
  /\ att \in [Cards -> {"pool", "dealt", "taken", "none"}]
  /\ holder \in [Cards -> Members \cup {None}]
  /\ age \in [Cards -> 0..AgeCap]
  /\ oks \in [Cards -> 0..NeedReads]
  /\ asked \in [Cards -> BOOLEAN]
  /\ fails \in [Members -> 0..N]
  /\ aside \in [Members -> BOOLEAN]
  /\ cool \in [Members -> 0..Cooldown]
  /\ unpushed \in 0..1
  /\ badDeal \in BOOLEAN

\* The incident's start: a card at work or in review may already be dealt or asked to any
\* capable member, the down reader and the friend that never takes included.
Init ==
  /\ stage \in [Cards -> {"work", "read"}]
  /\ att \in [Cards -> {"pool", "dealt"}]
  /\ holder \in [Cards -> Members \cup {None}]
  /\ \A c \in Cards : (att[c] = "pool") <=> (holder[c] = None)
  /\ \A c \in Cards : att[c] = "dealt" => Capable(c, holder[c])
  /\ age = [c \in Cards |-> 0]
  /\ oks = [c \in Cards |-> 0]
  /\ asked = [c \in Cards |-> att[c] = "dealt"]
  /\ fails = [m \in Members |-> 0]
  /\ aside = [m \in Members |-> FALSE]
  /\ cool = [m \in Members |-> 0]
  /\ unpushed = 0
  /\ badDeal = FALSE

\* B4: the first capable dealable member in preference order. "onceread": a read
\* returned after its one ask is never asked again.
DealIs(c) ==
  {i \in 1..Len(Pref) : /\ Capable(c, Pref[i])
                        /\ Broken = "dealblocked" \/ Dealable(Pref[i])}

CanDeal(c) ==
  /\ att[c] = "pool"
  /\ ~(Broken = "onceread" /\ stage[c] = "read" /\ asked[c])
  /\ DealIs(c) # {}

Deal(c) ==
  /\ CanDeal(c)
  /\ LET is == DealIs(c)
     IN /\ LET m == Pref[CHOOSE j \in is : \A k \in is : j <= k]
           IN /\ att' = [att EXCEPT ![c] = "dealt"]
              /\ holder' = [holder EXCEPT ![c] = m]
              /\ age' = [age EXCEPT ![c] = 0]
              /\ asked' = [asked EXCEPT ![c] = (stage[c] = "read")]
              /\ badDeal' = (badDeal \/ ~Dealable(m))
  /\ UNCHANGED <<stage, oks, fails, aside, cool, unpushed>>

\* The member takes (or begins) an attempt dealt to it: it has taken again (B3).
Take(c) ==
  /\ att[c] = "dealt"
  /\ holder[c] \in Up \cap Able
  /\ att' = [att EXCEPT ![c] = "taken"]
  /\ age' = [age EXCEPT ![c] = 0]
  /\ fails' = [fails EXCEPT ![holder[c]] = 0]
  /\ aside' = [aside EXCEPT ![holder[c]] = FALSE]
  /\ UNCHANGED <<stage, holder, oks, asked, cool, unpushed, badDeal>>

\* A take that fails as a harness fault (exit 126): the attempt stays dealt until its
\* bound; the Nth fault in a row sets the member aside with a pushed judgment (B2, B3).
TakeFault(m) ==
  /\ m \in Up \ Able
  /\ \E c \in Cards : holder[c] = m /\ att[c] = "dealt"
  /\ fails[m] < N
  /\ fails' = [fails EXCEPT ![m] = @ + 1]
  /\ IF fails[m] + 1 = N /\ ~aside[m] /\ Broken # "noflag"
       THEN /\ aside' = [aside EXCEPT ![m] = TRUE]
            /\ cool' = [cool EXCEPT ![m] = 0]
            /\ unpushed' = IF Broken = "silent" THEN 1 ELSE unpushed
       ELSE UNCHANGED <<aside, cool, unpushed>>
  /\ UNCHANGED <<stage, att, holder, age, oks, asked, badDeal>>

\* A taken attempt shows progress (a progress stamp, a beat naming it running).
Progress(c) ==
  /\ att[c] = "taken"
  /\ holder[c] \in Able
  /\ age[c] > 0
  /\ age' = [age EXCEPT ![c] = 0]
  /\ UNCHANGED <<stage, att, holder, oks, asked, fails, aside, cool, unpushed, badDeal>>

\* A taken attempt fails (its run fails, the reader hands the read back): it returns to
\* the pool at once with its pushed judgment, and its member is set aside (B1-B3).
Fail(c) ==
  /\ att[c] = "taken"
  /\ att' = [att EXCEPT ![c] = "pool"]
  /\ holder' = [holder EXCEPT ![c] = None]
  /\ age' = [age EXCEPT ![c] = 0]
  /\ aside' = IF Broken = "noflag" THEN aside ELSE [aside EXCEPT ![holder[c]] = TRUE]
  /\ cool' = [cool EXCEPT ![holder[c]] = 0]
  /\ unpushed' = IF Broken = "silent" THEN 1 ELSE unpushed
  /\ UNCHANGED <<stage, oks, asked, fails, badDeal>>

\* The attempt's stage is done; the card moves to its next stage. Work goes to read,
\* whose attempt is in the pool. A read's verdict is ok or broken: ok counts toward
\* NeedReads (the read stage asks again until it has them, then the card leaves for the
\* merge); broken leaves for the rework the rules deal (both terminal in this layer).
Next1(c, s, o) ==
  /\ stage' = [stage EXCEPT ![c] = s]
  /\ oks' = [oks EXCEPT ![c] = o]
  /\ att' = [att EXCEPT ![c] = IF s \in {"merge", "rework"} THEN "none" ELSE "pool"]
  /\ holder' = [holder EXCEPT ![c] = None]
  /\ age' = [age EXCEPT ![c] = 0]
  /\ asked' = [asked EXCEPT ![c] = FALSE]

Finish(c) ==
  /\ att[c] = "taken"
  /\ holder[c] \in Able
  /\ \/ stage[c] = "work" /\ Next1(c, "read", 0)
     \/ /\ stage[c] = "read"
        /\ \/ oks[c] + 1 = NeedReads /\ Next1(c, "merge", NeedReads)
           \/ oks[c] + 1 < NeedReads /\ Next1(c, "read", oks[c] + 1)
           \/ Next1(c, "rework", 0)
  /\ UNCHANGED <<fails, aside, cool, unpushed, badDeal>>

\* B1, B2, B3: the tick ages every held attempt and returns the ones past their bound,
\* each with its pushed judgment, and sets their members aside. The deal is a part of
\* the tick (TickDeal runs in every tick, after the bounce): time does not pass while a
\* pool attempt has a member it can be dealt to.
Late(c) ==
  \/ att[c] = "dealt" /\ age[c] + 1 > Bound /\ Broken # "nobounce"
  \/ att[c] = "taken" /\ age[c] + 1 > Wall /\ Broken # "nowall"

Tick ==
  LET B == {c \in Cards : Late(c)}
      who == {holder[c] : c \in B}
  IN
  /\ \A c \in Cards : ~CanDeal(c)
  /\ att' = [c \in Cards |-> IF c \in B THEN "pool" ELSE att[c]]
  /\ holder' = [c \in Cards |-> IF c \in B THEN None ELSE holder[c]]
  /\ age' = [c \in Cards |->
               IF c \in B THEN 0
               ELSE IF att[c] \in {"dealt", "taken"} THEN Min(age[c] + 1, AgeCap)
               ELSE 0]
  /\ aside' = [m \in Members |-> IF m \in who /\ Broken # "noflag" THEN TRUE ELSE aside[m]]
  /\ cool' = [m \in Members |-> IF m \in who THEN 0 ELSE Min(cool[m] + 1, Cooldown)]
  /\ unpushed' = IF B # {} /\ Broken = "silent" THEN 1 ELSE unpushed
  /\ UNCHANGED <<stage, oks, asked, fails, badDeal>>

Next ==
  \/ Tick
  \/ \E c \in Cards : Deal(c) \/ Take(c) \/ Progress(c) \/ Fail(c) \/ Finish(c)
  \/ \E m \in Members : TakeFault(m)

\* Fairness: time passes, the deal runs, and a live member takes and finishes. A failed
\* run is the environment's (no fairness): a member may fail any number of times, but
\* not every time forever (the live member's Finish is strongly fair).
Fairness ==
  /\ WF_vars(Tick)
  /\ \A c \in Cards : WF_vars(Deal(c))
  /\ \A c \in Cards : SF_vars(Take(c))
  /\ \A c \in Cards : SF_vars(Finish(c))

Spec == Init /\ [][Next]_vars

LiveSpec == Spec /\ Fairness

\* The bounce invariant shared by all stages.
NoHeldPastBound ==
  \A c \in Cards :
    /\ att[c] = "dealt" => age[c] <= Bound
    /\ att[c] = "taken" => age[c] <= Wall

EveryBouncePushed == unpushed = 0

NoDealToBlocked == ~badDeal

Advances(s) == \A c \in Cards : (stage[c] = s) ~> (stage[c] # s)

WorkAdvances == Advances("work")
ReadAdvances == Advances("read")

DealtResolves == \A c \in Cards : (att[c] = "dealt") ~> (att[c] # "dealt")

=============================================================================
