---------------------------- MODULE DealThrottle ----------------------------
\* The deal's throttle on new work while landing or review backs up
\* (internal/sprint/deal_throttle.go, WorkThrottled and throttledOut; card
\* v17-deal-throttles-new-work-when-landing-backs-up). The owner, Glenn,
\* 2026-10-18: "if reading, fixing and landing are all higher priority than
\* work, the system balances. if the same, or lower, then the system backs up
\* with more work coming in until it collapses."
\*
\* It is DirtyTick's ReadsBeforeWork carried one rung further: DirtyTick holds
\* that the deal places reads before work in the same tick; this module holds
\* that while the landing queue (the work table's merging column) is over its
\* bound, or review is over its bound, the deal starts no NEW work at all, and
\* lets the queues drain. The vocabulary is DirtyTick's: a card's column (col),
\* its attempt (att, the attempts dealt), the deal, the finish, the accept and
\* the land. A bound of 0 is off (set --land-backlog-bound off, the default):
\* it never throttles.
\*
\* What a card is, as the code sorts it (deal_throttle.go, NewWorkAttempt):
\*   - new work: a work card at attempt 0 (the next deal cuts attempt 1), not at
\*     a fix level and not a read: the throttle holds it in ready;
\*   - a fix or a read (Fix): a fix-level card (blocker, critical, fix) or a
\*     read card: never held by the throttle;
\*   - a next attempt: a work card with an attempt already (a rework, a redo, a
\*     redeal): never held by the throttle (Rework puts one back in ready).
\* Nothing working is ever cut by the throttle: it only refuses a start.
\*
\* Outside the module: the room of each member (one pool of Slots here), the
\* ladder's order among the cards it deals (DirtyTick, priority.go), the routes,
\* the friends' deal and the fleet's as two paths (both run the same filter in
\* TickDeal), and the where line.
EXTENDS Naturals, FiniteSets

CONSTANTS
  Work,        \* the work cards (new work while at attempt 0)
  Fix,         \* the fix-level cards and the read cards
  Slots,       \* the room the deal fills: at most this many working
  LandBound,   \* the land backlog bound (merging); 0 is off
  ReviewBound, \* the review backlog bound; 0 is off
  MaxAtt,      \* the attempts a card may run (Rework bound)
  Broken       \* "none", or the witness: "noresume", "cutsinflight"

Cards == Work \cup Fix
Cols == {"none", "ready", "working", "review", "merging", "landed"}

VARIABLES col, att, latched
vars == <<col, att, latched>>

N(c0) == Cardinality({c \in Cards : col[c] = c0})

\* A bound is over when it is on (above 0) and its count exceeds it.
Over(n, b) == b > 0 /\ n > b

\* WorkThrottled: merging over the land bound, or review over the review bound.
Throttled == Over(N("merging"), LandBound) \/ Over(N("review"), ReviewBound)

\* A new work attempt: a work card at attempt 0.
IsNew(c) == c \in Work /\ att[c] = 0

\* The throttle holds a new work card in ready while it stands; the broken
\* "noresume" throttle, once it has stood, never lifts (latched).
Held(c) == IsNew(c) /\ (Throttled \/ (Broken = "noresume" /\ latched))

TypeOK ==
  /\ col \in [Cards -> Cols]
  /\ att \in [Cards -> 0..MaxAtt]
  /\ latched \in BOOLEAN

Init ==
  /\ col = [c \in Cards |-> "none"]
  /\ att = [c \in Cards |-> 0]
  /\ latched = FALSE

\* The latch the broken throttle keeps: it remembers any state that throttled.
Latch == latched' = (latched \/ Throttled')

\* Arrive: a card is added and is ready (new work, a fix, a read asked).
Arrive(c) ==
  /\ col[c] = "none"
  /\ col' = [col EXCEPT ![c] = "ready"]
  /\ UNCHANGED att
  /\ Latch

\* Deal: a ready card starts in a free slot, unless the throttle holds it.
Deal(c) ==
  /\ col[c] = "ready"
  /\ N("working") < Slots
  /\ ~Held(c)
  /\ att[c] < MaxAtt
  /\ col' = [col EXCEPT ![c] = "working"]
  /\ att' = [att EXCEPT ![c] = @ + 1]
  /\ Latch

\* Finish: working -> review.
Finish(c) ==
  /\ col[c] = "working"
  /\ col' = [col EXCEPT ![c] = "review"]
  /\ UNCHANGED att
  /\ Latch

\* Accept: review -> merging (its reads passed).
Accept(c) ==
  /\ col[c] = "review"
  /\ col' = [col EXCEPT ![c] = "merging"]
  /\ UNCHANGED att
  /\ Latch

\* Rework: review -> ready, its next attempt (never new work again).
Rework(c) ==
  /\ col[c] = "review"
  /\ att[c] < MaxAtt
  /\ col' = [col EXCEPT ![c] = "ready"]
  /\ UNCHANGED att
  /\ Latch

\* Land: merging -> landed, freeing the queue.
Land(c) ==
  /\ col[c] = "merging"
  /\ col' = [col EXCEPT ![c] = "landed"]
  /\ UNCHANGED att
  /\ Latch

\* The broken "cutsinflight" throttle: while it stands it recalls a working
\* card to ready (the code never does: the throttle filters the ready column
\* the deal reads, and writes no working card).
Recall(c) ==
  /\ Broken = "cutsinflight"
  /\ Throttled
  /\ col[c] = "working"
  /\ col' = [col EXCEPT ![c] = "ready"]
  /\ UNCHANGED att
  /\ Latch

Next == \E c \in Cards :
          Arrive(c) \/ Deal(c) \/ Finish(c) \/ Accept(c) \/ Rework(c) \/ Land(c) \/ Recall(c)

\* The lander, the worker's finish and the accept are fair (the tick runs them
\* every pass), and a deal that is enabled again and again is taken (SF: the
\* throttle may stand and lift many times before the card is dealt).
Fairness ==
  /\ \A c \in Cards : WF_vars(Finish(c)) /\ WF_vars(Accept(c)) /\ WF_vars(Land(c))
  /\ \A c \in Cards : SF_vars(Deal(c))

Spec == Init /\ [][Next]_vars /\ Fairness

----------------------------------------------------------------------------
\* NoNewWorkWhileThrottled: a deal of new work happens only while merging is at
\* or under its bound and review at or under its bound.
NoNewWorkWhileThrottled ==
  [][\A c \in Work : (IsNew(c) /\ col[c] = "ready" /\ col'[c] = "working") => ~Throttled]_vars

\* FixesAndReadsAlwaysDeal: a ready fix, read or next attempt with a free slot
\* is never held by the throttle: its deal is enabled.
FixesAndReadsAlwaysDeal ==
  \A c \in Cards : (col[c] = "ready" /\ ~IsNew(c) /\ N("working") < Slots /\ att[c] < MaxAtt)
                     => ENABLED Deal(c)

\* InFlightNeverCut: a working card leaves working only by its finish.
InFlightNeverCut ==
  [][\A c \in Cards : col[c] = "working" => col'[c] \in {"working", "review"}]_vars

\* WorkResumes: a new work card the throttle holds in ready is eventually dealt,
\* once landing and review drain to their bounds.
WorkResumes ==
  \A c \in Work : (col[c] = "ready" /\ Throttled) ~> (col[c] = "working")

\* NoStarvation of the lander: while the throttle stands on a queue, that queue
\* drains to its bound.
LanderDrains == Over(N("merging"), LandBound) ~> ~Over(N("merging"), LandBound)
ReviewDrains == Over(N("review"), ReviewBound) ~> ~Over(N("review"), ReviewBound)

\* Probe: the instance reaches a throttled state with a new work card held in
\* ready (the throttle is exercised, not vacuous). Checked as an invariant that
\* must fail by hand, never in CASES.
ProbeNeverHeld == ~(\E c \in Work : col[c] = "ready" /\ Held(c))
=============================================================================
