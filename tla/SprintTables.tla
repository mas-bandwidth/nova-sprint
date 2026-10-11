-------------------------------- MODULE SprintTables ----------------------------
\* The card lifecycle of nova-sprint (docs/SPEC-SPRINT.md section 3, the
\* lifecycle and its inventory of moves; internal/sprint/lifecycle.go Moves and
\* Lawful), the fleet side of the work table (internal/sprint/refmodel
\* actions.go Start, Take, Finish, FleetDown, FleetUp) and the reads and the
\* accept (refmodel actions.go Ask, AskAnother, ReadStart, Read, Accept, held
\* to the code's ReadsNeededIn). A primary's six states and the lawful moves
\* between them, in one table, plus its named needs (a variable: needs set and
\* a brief edit's DEPENDS-ON rewrite them), the fleet (each member up or down, a
\* working primary's work card on one member's row in a place none, ready,
\* working, finished or withdrawn) and the readers (a read card on one
\* reader's row in a place none, asked, reading, ok or broken).
\*
\* A primary's state is the work-table column it is placed in. The six states
\* are waiting, ready, working, review, merging, landed; landed is final. Off
\* the table (a drop) is not a state: a primary that stops any other way than
\* landing leaves the table with its record, outcome and reason kept, unplaced.
\* A landed card that a remove takes off the table keeps its outcome: its
\* dependents still count it landed (everLanded).
\*
\* THE INVENTORY. Every place the code writes a primary's column is one of
\* these actions (docs/SPEC-SPRINT.md section 3 lists each code site):
\*   Resolve        waiting -> ready    resolve, the tick's resolve, a waiting
\*                                      card's release of its hold (releaseHeld)
\*   Start          ready   -> working  the deal and redeal, machines and friends
\*   Finish         working -> review   finish
\*   Withdraw       working -> ready    the work card withdrawn: route rests,
\*                                      take ended, staging refused, rate
\*                                      limited, lane cap, friend return,
\*                                      friend redeal, finish to ready
\*   FleetDown      working -> ready    no member is up (or the card moves to
\*                                      another up member, primary working)
\*   Accept         review  -> merging  accept, the tick's accept
\*   Rework         review  -> working | ready   rework, a brief edited in
\*                                      place on a card in review
\*   Return         merging -> review   return, the tick's short-reads pass,
\*                                      a refused landing
\*   Redo           merging -> working | ready   redo, a refused landing
\*   Merge          merging -> landed   the lander's merge
\*   Release        waiting -> landed   release of a sentinel (verb or tick)
\*   AddMove        ready   -> waiting  add inserts a sentinel in front
\*   NeedsSet       ready   -> waiting | waiting -> ready | in place
\*                                      needs set, a brief edit's DEPENDS-ON
\*   NeedsWait      ready   -> waiting  the tick's needsWaits (PR #96)
\*   Waive          waiting -> ready | in place   a waiver (the ack of a
\*                                      dropped or missing need)
\*   Drop           open -> off          drop, defer; a landed card by remove
\*   NeedsWithdraw  working -> waiting  needsWithdrawals, needsStopReturned
\*                                      (PR #96): its needs have not landed
\* Admission (add, a stream's reopen) is the initial state: a primary is
\* admitted waiting or ready, a sentinel waiting.
\*
\* The fleet side (refmodel actions.go) follows the code:
\*   Start(p, m)     the deal: a ready primary whose needs have landed has its
\*                   work card dealt to an up member's row (place ready).
\*   Take(c)         the worker on an up member moves a dealt (ready) card to
\*                   working.
\*   Finish(c)       the working card finishes; its primary goes to review.
\*   FleetDown(m)    the member goes down; its ready and working cards are
\*                   dealt to the other up members (place ready), or, with no
\*                   member up, withdrawn and their primaries return to ready.
\*   FleetUp(m)      the member comes up.
\* The round-robin order of the deal (NextMember) and the levelling of the
\* ready queues (T4, the Level action) are choices, not safety rules, and are
\* not modelled here; the deal takes any up member.
\*
\* The reads and the accept (refmodel actions.go, held to readers.go
\* ReadsNeededIn; lifecycle.go Moves names accept Coordinator):
\*   Ask(c)          a primary in review asks one more read of a reader other
\*                   than its worker (one for a flash card, two for a pro card).
\*   AskAnother(c)   one more reader for a primary already asked, answering a
\*                   broken read or reads exhausted.
\*   ReadStart(r)    the reader moves its own asked read to reading.
\*   Read(r)         the reader records ok or broken on a read asked or
\*                   reading.
\*   Accept(c)       review -> merging when the primary has the reads its tier
\*                   needs and no broken read stands at its head; its asked
\*                   and reading read cards retire.
\*
\* THE RULES. Invariants: TypeOK, NoNeedCycle, Rule11 (docs/SPEC-SPRINT.md
\* section 9, rule 11, for the states nothing moves back to waiting: a primary
\* in review, merging or landed has every named need landed or waived; a ready
\* or working card that gains a need not landed is moved to waiting by the
\* tick, NeedsWait and NeedsWithdraw, held by NeedsWithdrawalWaits), AcceptHeld (no primary enters merging short of its
\* reads), NeverAcceptBroken (no primary enters merging with a broken read at
\* its head) and NeverReadByItsWorker (a read is never asked of the reader that
\* worked the card). Action properties: NoCardLostOrTwice, MovesOnlyByTable
\* (every change of cell is a lawful move, a drop from an open state, or a
\* remove of a landed card), ReadyToWaitingOnlyByInsertOrNeeds, LandedIsFinal,
\* NeedsLandFirst (waiting -> ready only with every need landed or waived),
\* NeedsHeldIntoWorking (no deal, rework or redo into working with a need not
\* landed), WorkingToWaitingOnlyOnNeeds, NeedsWithdrawalWaits (PR #96: a
\* working card withdrawn on its needs ends waiting, never ready) and
\* WorkingOnAnUpMember.
\*
\* Refmodel invariants not stated here. The pending-operation fence (D1: Free)
\* is left out: this module has no pending operation, a step is one atomic
\* move, so there is nothing for the fence to fence. F3 (a stopped stream's
\* judgment and a skipped repair) and F4 (a merge need placed, in another
\* stream and not landed) are left out too: they belong to the merge and
\* crash-repair cards.
\*
\* Reversed witnesses (Broken), one per rule, each a cfg that must fail:
\*   "insert"       the add move runs ready -> waiting without inserting
\*                  (ReadyToWaitingOnlyByInsertOrNeeds).
\*   "needsany"     a needs rewrite moves a ready card to waiting with every
\*                  need landed, as lifecycle.go admitted before this card
\*                  (any needs field) (ReadyToWaitingOnlyByInsertOrNeeds).
\*   "needless"     resolve drops the needs check (NeedsLandFirst).
\*   "reviewneeds"  a needs rewrite gives a card in review a need not landed,
\*                  as briefDepends admitted before this card (Rule11).
\*   "dealunmet"    the deal drops the needs check (NeedsHeldIntoWorking).
\*   "withdrawready" PR #96's: the needs withdrawal runs working -> ready
\*                  (NeedsWithdrawalWaits).
\*   "cycle"        a needs rewrite skips the cycle check (NoNeedCycle).
\*   "downkeeps"    fleet down leaves a working card on the member that went
\*                  down (WorkingOnAnUpMember).
\*   "shortaccept"  the accept drops the read guard (AcceptHeld).
\*   "acceptbroken" the accept drops the no-broken guard (NeverAcceptBroken).
\*   "ownread"      a read is asked of the card's own worker
\*                  (NeverReadByItsWorker).
\*   "skip"         the merge lands a card straight from review
\*                  (MovesOnlyByTable).
\*   "unland"       the short-reads pass sends a landed card back to review,
\*                  as ShortReadsBack planned before this card
\*                  (LandedIsFinal).
\*   "comeback"     a dropped card is admitted again (NoCardLostOrTwice).
\*   "withdrawmet"  a working card whose needs have all landed is withdrawn
\*                  to waiting (WorkingToWaitingOnlyOnNeeds).
\* TypeOK has no witness: it is the shape of the variables, not a rule.
EXTENDS Naturals, FiniteSets

CONSTANTS
  Card,         \* the card ids
  Sentinels,    \* the sentinel ids, a subset of Card
  NeedsOf,      \* NeedsOf[c] is c's needs at admission, a subset of Card
  Score,        \* Score[c] is c's place in line, a distinct number
  InitOf,       \* InitOf[c] is c's cell at the start: waiting or ready
  Member,       \* the fleet's member ids
  InitUp,       \* the members up at the start
  Reader,       \* the readers: one per fleet member (MemberReader) plus friends
  Flash,        \* the flash-tier primaries, a subset of Card (the rest are pro)
  MemberReader, \* the reader on each member (readers == workers per machine)
  Rewrites,     \* the needs rewrites NeedsSet may write: pairs <<card, needs>>
                \* (the instance's choice; the code takes any set of cards)
  Broken        \* "none", or the one reversed witness to break

States == {"waiting", "ready", "working", "review", "merging", "landed"}
Cells == States \cup {"off"}

\* The work card's place on a member's row (refmodel state.go FleetCells).
WPlaces == {"none", "ready", "working", "finished", "withdrawn"}

\* A member's status (refmodel state.go: Up, Down).
MStatus == {"up", "down"}

\* A read card's cell on a reader's row (refmodel state.go ReadCard.Place).
RPlaces == {"none", "asked", "reading", "ok", "broken"}

\* The lifecycle: every lawful move, and nothing else (lifecycle.go Moves).
Moves ==
  { <<"waiting", "ready">>,
    <<"ready", "working">>,
    <<"working", "review">>,
    <<"working", "ready">>,
    <<"review", "merging">>,
    <<"review", "working">>,
    <<"review", "ready">>,
    <<"merging", "review">>,
    <<"merging", "working">>,
    <<"merging", "ready">>,
    <<"merging", "landed">>,
    <<"waiting", "landed">>,
    <<"ready", "waiting">>,
    <<"working", "waiting">> }

Legal(from, to) == <<from, to>> \in Moves

\* IsOpen: a primary in s has not landed, so drop may take it off the table.
IsOpen(s) == s \in {"waiting", "ready", "working", "review", "merging"}

IsSentinel(c) == c \in Sentinels

\* The plan flags of the step just taken (lifecycle.go Plan): releasing lands a sentinel,
\* inserting moves a primary ready -> waiting, removing takes a landed primary
\* off the table. Each is a nondeterministic choice of the plan the step runs.
Flags == [releasing : BOOLEAN, inserting : BOOLEAN, removing : BOOLEAN]

\* An ordinary step's plan: no flag. Release, the add that inserts a sentinel and
\* the remove set their own flag on the step they take (flags', read by the
\* rules), so a flag is the plan of that step and never a state carried on.
NoFlags == [releasing |-> FALSE, inserting |-> FALSE, removing |-> FALSE]

VARIABLES state, needs, waived, everLanded, flags, members, wplace, wmember, rplace

vars == <<state, needs, waived, everLanded, flags, members, wplace, wmember, rplace>>

TypeOK ==
  /\ state \in [Card -> Cells]
  /\ needs \in [Card -> SUBSET Card]
  /\ waived \in [Card -> SUBSET Card]
  /\ everLanded \in SUBSET Card
  /\ flags \in Flags
  /\ members \in [Member -> MStatus]
  /\ wplace \in [Card -> WPlaces]
  /\ wmember \in [Card -> Member]
  /\ rplace \in [Card -> [Reader -> RPlaces]]

\* Up: the members up now (refmodel State.Up).
Up == {m \in Member : members[m] = "up"}

\* HeldOn: the working cards a member holds, ready and working (refmodel
\* State.Held): a card a member holds belongs to a primary that is working.
HeldOn(m) ==
  {c \in Card : state[c] = "working" /\ wmember[c] = m /\ wplace[c] \in {"ready", "working"}}

\* StopBefore (position.go): the last unlanded sentinel before a waiting
\* primary in its stream's line; none when there is no such sentinel. The set
\* is empty or a singleton because scores are distinct.
StopBefore(c) ==
  {d \in Card :
    IsSentinel(d) /\ Score[d] < Score[c] /\ state[d] \in States \ {"landed", "off"}
    /\ ~ \E e \in Card :
      IsSentinel(e) /\ Score[e] < Score[c] /\ state[e] \in States \ {"landed", "off"}
      /\ Score[e] > Score[d]}

\* PositionWaits (position.go): what a card waits for by its place in line, none
\* of it stored as a need. A sentinel waits for every card before it that has
\* not landed; a waiting primary for the last unlanded sentinel before it.
PositionWaits(c) ==
  IF IsSentinel(c)
  THEN {d \in Card : Score[d] < Score[c] /\ state[d] \in States \ {"landed", "off"}}
  ELSE IF state[c] = "waiting" THEN StopBefore(c) ELSE {}

\* A need is met once it has landed (a removed landed card keeps its outcome)
\* or was waived (NamedWaits, steps_work.go; unmet, lifecycle.go).
MetWith(c, N) == \A n \in N : n \in everLanded \/ n \in waived[c]
NeedsMet(c) == MetWith(c, needs[c])

\* Nothing in line before c still blocks it.
PositionClear(c) == \A q \in PositionWaits(c) : state[q] = "landed"

\* What c's needs reach, followed transitively (bounded by the card count, so a
\* cycle a reversed witness makes still terminates).
ReachN(c) ==
  LET RECURSIVE R(_, _)
      R(S, k) == IF k = 0 THEN S ELSE R(S \cup UNION {needs[x] : x \in S}, k - 1)
  IN R({c}, Cardinality(Card))

\* Needed(c): a primary still on the table that has not landed names c as a
\* need it has not waived (drop refuses a needed card without its cascade).
Needed(c) ==
  \E d \in Card \ {c} : IsOpen(state[d]) /\ c \in needs[d] /\ c \notin waived[d]

Init ==
  /\ state = InitOf
  /\ \A c \in Sentinels : state[c] = "waiting"
  /\ needs = NeedsOf
  /\ waived = [c \in Card |-> {}]
  /\ everLanded = {}
  /\ flags = NoFlags
  /\ members = [m \in Member |-> IF m \in InitUp THEN "up" ELSE "down"]
  /\ wplace = [c \in Card |-> "none"]
  /\ wmember = [c \in Card |-> CHOOSE m \in Member : TRUE]
  /\ rplace = [c \in Card |-> [r \in Reader |-> "none"]]

\* The effect of one move: c to its new cell; the fleet is untouched and the
\* step carries no plan flag.
Move(c, to) ==
  /\ state' = [state EXCEPT ![c] = to]
  /\ everLanded' = IF to = "landed" THEN everLanded \cup {c} ELSE everLanded
  /\ UNCHANGED <<needs, waived, members, wplace, wmember, rplace>>
  /\ flags' = NoFlags

\* ------------------------------------------------------------ the reads

\* Needs (readers.go ReadsNeeded, the tier rule of ReadsNeededIn): how many
\* different readers' ok reads at its head make the primary acceptable: one
\* for a flash card, two for a pro card.
ReadsNeeded(c) == IF c \in Flash THEN 1 ELSE 2

\* The worker of a primary is the reader of the machine that worked its card
\* (reader-<m>, one reader per fleet member).
Worker(c) == MemberReader[wmember[c]]

LiveReads(c) == {r \in Reader : rplace[c][r] # "none"}
OkReaders(c) == {r \in Reader : rplace[c][r] = "ok"}
BrokenRead(c) == \E r \in Reader : rplace[c][r] = "broken"

\* A reader the primary can still be asked of: another reader, not its worker,
\* with no read card of the primary cut yet. The reversed witness "ownread"
\* lets the worker be asked.
FreshReader(c) ==
  IF Broken = "ownread" THEN Reader \ LiveReads(c)
  ELSE Reader \ ({Worker(c)} \cup LiveReads(c))

Acceptable(c) == Cardinality(OkReaders(c)) >= ReadsNeeded(c)

\* ClearReads: the primary's read cards retire (a new attempt), all to none.
ClearReads(c) == rplace' = [rplace EXCEPT ![c] = [r \in Reader |-> "none"]]

\* RetireOutstanding: the accept retires the asked and reading read cards, the
\* ok and broken stay (the record that made the accept; AcceptHeld).
RetireOutstanding(c) ==
  rplace' = [rplace EXCEPT ![c] = [r \in Reader |-> IF rplace[c][r] \in {"asked", "reading"} THEN "none" ELSE rplace[c][r]]]

\* ------------------------------------------------------------- the moves

\* resolve: waiting -> ready. Nothing in line before it blocks it, and every
\* need has landed or was waived. A sentinel never moves here: only release
\* lands it. The reversed witness "needless" drops the needs check.
Resolve ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "waiting"
    /\ PositionClear(c)
    /\ \/ Broken = "needless" \/ NeedsMet(c)
    /\ Move(c, "ready")

\* Start (refmodel Start; steps_work.go deal and redeal, friend_deal.go): the
\* deal. ready -> working; the work card is dealt to an up member's row (place
\* ready). Lawful's needs rule refuses every move to working whose card's
\* needs have not landed (unmet, lifecycle.go).
Start ==
  \E c \in Card, m \in Member :
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    /\ Broken = "dealunmet" \/ NeedsMet(c)
    /\ wplace[c] \in {"none", "withdrawn"}
    /\ members[m] = "up"
    /\ state' = [state EXCEPT ![c] = "working"]
    /\ wplace' = [wplace EXCEPT ![c] = "ready"]
    /\ wmember' = [wmember EXCEPT ![c] = m]
    /\ UNCHANGED <<needs, waived, everLanded, members, rplace>>
    /\ flags' = NoFlags

\* Take (refmodel Take): the worker on an up member moves a dealt (ready) work
\* card to working. The primary's state does not change.
Take ==
  \E c \in Card :
    /\ state[c] = "working"
    /\ wplace[c] = "ready"
    /\ members[wmember[c]] = "up"
    /\ NeedsMet(c)
    /\ wplace' = [wplace EXCEPT ![c] = "working"]
    /\ UNCHANGED <<state, needs, waived, everLanded, members, wmember, rplace>>
    /\ flags' = NoFlags

\* Finish (refmodel Finish): working -> review; the work card finishes in its
\* member's working cell.
Finish ==
  \E c \in Card :
    /\ state[c] = "working"
    /\ wplace[c] = "working"
    /\ members[wmember[c]] = "up"
    /\ state' = [state EXCEPT ![c] = "review"]
    /\ wplace' = [wplace EXCEPT ![c] = "finished"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, rplace>>
    /\ flags' = NoFlags

\* Withdraw: working -> ready, the work card withdrawn with its attempt
\* unchanged, for every cause but fleet down (withdrawUnit and the route rest,
\* takeEnded, stagingRefused, rateLimitedReturned, capRedeal, FriendReturn,
\* TickFriendRedeal, finishPlan's return to ready).
Withdraw ==
  \E c \in Card :
    /\ state[c] = "working"
    /\ wplace[c] \in {"ready", "working", "finished"}
    /\ state' = [state EXCEPT ![c] = "ready"]
    /\ wplace' = [wplace EXCEPT ![c] = "withdrawn"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, rplace>>
    /\ flags' = NoFlags

\* FleetDown (refmodel FleetDown): the member goes down; its ready and working
\* cards are dealt to the other up members (place ready, their primaries stay
\* working), or, with no member up, withdrawn (their primaries return to
\* ready). The reversed witness "downkeeps" leaves a working card on the member
\* that went down.
FleetDown ==
  \E m \in Up :
    /\ members' = [members EXCEPT ![m] = "down"]
    /\ \/ /\ Broken = "downkeeps"
          /\ UNCHANGED <<state, wplace, wmember>>
       \/ /\ Up \ {m} # {}
          /\ state' = state
          /\ wplace' = [c \in Card |-> IF c \in HeldOn(m) THEN "ready" ELSE wplace[c]]
          /\ \E r \in [HeldOn(m) -> Up \ {m}] :
               wmember' = [c \in Card |-> IF c \in HeldOn(m) THEN r[c] ELSE wmember[c]]
       \/ /\ Up \ {m} = {}
          /\ state' = [c \in Card |-> IF c \in HeldOn(m) THEN "ready" ELSE state[c]]
          /\ wplace' = [c \in Card |-> IF c \in HeldOn(m) THEN "withdrawn" ELSE wplace[c]]
          /\ wmember' = wmember
    /\ UNCHANGED <<needs, waived, everLanded, rplace>>
    /\ flags' = NoFlags

\* FleetUp (refmodel FleetUp): the member comes up.
FleetUp ==
  \E m \in Member :
    /\ members[m] = "down"
    /\ members' = [members EXCEPT ![m] = "up"]
    /\ UNCHANGED <<state, needs, waived, everLanded, wplace, wmember, rplace>>
    /\ flags' = NoFlags

\* Ask (refmodel Ask): the primary in review asks one more read of a reader
\* other than its worker, distinct from the readers already standing.
Ask ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ ~BrokenRead(c)
    /\ Cardinality(Reader) >= 2
    /\ Cardinality(LiveReads(c)) < ReadsNeeded(c)
    /\ \E r \in FreshReader(c) :
         /\ rplace' = [rplace EXCEPT ![c][r] = "asked"]
         /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember>>
    /\ flags' = NoFlags

\* AskAnother (refmodel AskAnother): one more reader for a primary already
\* asked, answering a broken read or reads exhausted.
AskAnother ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ LiveReads(c) # {}
    /\ (BrokenRead(c) \/ Cardinality(LiveReads(c)) >= ReadsNeeded(c))
    /\ \E r \in FreshReader(c) :
         /\ rplace' = [rplace EXCEPT ![c][r] = "asked"]
         /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember>>
    /\ flags' = NoFlags

\* ReadStart (refmodel ReadStart): the reader moves its own asked read to
\* reading.
ReadStart ==
  \E r \in Reader :
    /\ \E c \in Card :
         /\ rplace[c][r] = "asked"
         /\ rplace' = [rplace EXCEPT ![c][r] = "reading"]
         /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember>>
    /\ flags' = NoFlags

\* Read (refmodel Read): the reader records ok or broken.
Read ==
  \E r \in Reader :
    /\ \E c \in Card, v \in {"ok", "broken"} :
         /\ rplace[c][r] \in {"asked", "reading"}
         /\ rplace' = [rplace EXCEPT ![c][r] = v]
         /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember>>
    /\ flags' = NoFlags

\* accept: review -> merging when the primary has the reads its tier needs and
\* no broken read stands at its head (steps_review.go Accept, TickAccept).
Accept ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ \/ Acceptable(c)
       \/ /\ Broken = "shortaccept"
          /\ Cardinality(OkReaders(c)) >= ReadsNeeded(c) - 1
    /\ Broken = "acceptbroken" \/ ~BrokenRead(c)
    /\ state' = [state EXCEPT ![c] = "merging"]
    /\ RetireOutstanding(c)
    /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember>>
    /\ flags' = NoFlags

\* NextAttempt(c, from): rework and redo (and a brief edited in place on a card
\* in review): the next attempt is dealt at once to an up member (to working,
\* the needs rule held as at any deal), or the card goes to ready and the deal
\* cuts it later; the old work card is retired and the read cards retire.
NextAttempt(c, from) ==
  /\ state[c] = from
  /\ \/ \E m \in Up :
          /\ NeedsMet(c)
          /\ state' = [state EXCEPT ![c] = "working"]
          /\ wplace' = [wplace EXCEPT ![c] = "ready"]
          /\ wmember' = [wmember EXCEPT ![c] = m]
     \/ /\ state' = [state EXCEPT ![c] = "ready"]
        /\ wplace' = [wplace EXCEPT ![c] = "none"]
        /\ wmember' = wmember
  /\ ClearReads(c)
  /\ UNCHANGED <<needs, waived, everLanded, members>>
  /\ flags' = NoFlags

\* rework: review -> working (a member is up) or review -> ready.
Rework == \E c \in Card : ~IsSentinel(c) /\ NextAttempt(c, "review")

\* return: merging -> review (return, the tick's short-reads pass, a refused
\* landing). The read cards at the attempt stand. The reversed witness
\* "unland" takes a landed card back too, as ShortReadsBack planned before
\* this card (the store refused it).
Return ==
  \E c \in Card :
    /\ \/ state[c] = "merging"
       \/ Broken = "unland" /\ state[c] = "landed" /\ ~IsSentinel(c)
    /\ Move(c, "review")

\* redo: merging -> working (a member is up) or merging -> ready.
Redo == \E c \in Card : ~IsSentinel(c) /\ NextAttempt(c, "merging")

\* merge: merging -> landed. The reversed witness "skip" lands a card straight
\* from review.
Merge ==
  \E c \in Card :
    /\ \/ state[c] = "merging"
       \/ Broken = "skip" /\ state[c] = "review"
    /\ Move(c, "landed")

\* release: waiting -> landed, a sentinel only, and only by the plan's release.
Release ==
  \E c \in Card :
    /\ IsSentinel(c)
    /\ state[c] = "waiting"
    /\ state' = [state EXCEPT ![c] = "landed"]
    /\ everLanded' = everLanded \cup {c}
    /\ UNCHANGED <<needs, waived, members, wplace, wmember, rplace>>
    /\ flags' = [NoFlags EXCEPT !.releasing = TRUE]

\* add: ready -> waiting as the effect of inserting a sentinel in front. The
\* reversed witness "insert" lets it run without inserting.
AddMove ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    /\ state' = [state EXCEPT ![c] = "waiting"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace>>
    /\ flags' = [NoFlags EXCEPT !.inserting = (Broken # "insert")]

\* NeedsSet: needs set, and a brief edit's DEPENDS-ON (steps_edit.go NeedsSet,
\* Brief, briefInPlace, briefDepends): a primary's needs become N, each a card
\* on the table, never the card itself, never closing a cycle. A ready card
\* that gains a need not landed moves ready -> waiting; a waiting card whose
\* new needs have all landed moves waiting -> ready (refused while its place in
\* line still blocks it); a working card takes it in place and the tick then
\* withdraws it to waiting (NeedsWithdraw, PR #96); a card in review takes only
\* needs that have landed (briefDepends; rule 11). Every rewrite is held to the
\* cycle check add makes (steps_edit.go needsCycle). The reversed witnesses:
\* "reviewneeds" lets a review card take a need not landed, "needsany" moves a
\* ready card to waiting with every need landed, "cycle" skips the cycle check.
NeedsSet ==
  \E rw \in Rewrites :
   LET c == rw[1]
       N == rw[2]
   IN
    /\ ~IsSentinel(c)
    /\ c \notin N
    /\ state[c] \in {"waiting", "ready", "working", "review"}
    /\ N # needs[c]
    /\ \A d \in N : state[d] # "off"
    /\ Broken = "cycle" \/ \A d \in N : c \notin ReachN(d)
    /\ needs' = [needs EXCEPT ![c] = N]
    /\ \/ /\ state[c] = "working"
          \* dealt, not taken: the tick withdraws it (NeedsWithdraw). A taken
          \* card given a need not landed can finish ok before the tick and
          \* reach review with it; that gap is open (PR #103 body), not modelled
          /\ wplace[c] = "ready" \/ MetWith(c, N)
          /\ state' = state
       \/ /\ state[c] = "review"
          /\ Broken = "reviewneeds" \/ MetWith(c, N)
          /\ state' = state
       \/ /\ state[c] = "ready"
          /\ MetWith(c, N)
          /\ \/ state' = state
             \/ Broken = "needsany" /\ N # {} /\ state' = [state EXCEPT ![c] = "waiting"]
       \/ /\ state[c] = "ready"
          /\ ~MetWith(c, N)
          /\ state' = [state EXCEPT ![c] = "waiting"]
       \/ /\ state[c] = "waiting"
          /\ MetWith(c, N)
          /\ PositionClear(c)
          /\ state' = [state EXCEPT ![c] = "ready"]
       \/ /\ state[c] = "waiting"
          /\ ~MetWith(c, N)
          /\ state' = state
    /\ UNCHANGED <<waived, everLanded, members, wplace, wmember, rplace>>
    /\ flags' = NoFlags

\* NeedsWait (steps_tick.go needsWaits, PR #96): a ready primary whose named
\* needs are not all landed or waived, and whose work card is dealt nowhere,
\* waits again: ready -> waiting.
NeedsWait ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    /\ ~NeedsMet(c)
    /\ wplace[c] \notin {"ready", "working"}
    /\ Move(c, "waiting")

\* NeedsWithdraw (steps_tick.go needsWithdrawals, needs_stop.go
\* needsStopReturned, PR #96): a working primary whose named needs are not all
\* landed or waived goes straight to waiting, its work card (dealt or taken)
\* back to the pool. lifecycle.go judges the move by the card's own named needs
\* in the pre-state (namedNeedsOpen), never by the needs the entry writes. The
\* reversed witnesses: "withdrawmet" withdraws a card whose needs have all
\* landed, "withdrawready" (#96's) runs it working -> ready instead.
NeedsWithdraw ==
  \E c \in Card :
    /\ state[c] = "working"
    /\ Broken = "withdrawmet" \/ ~NeedsMet(c)
    /\ wplace[c] \in {"ready", "working"}
    /\ state' = [state EXCEPT ![c] = IF Broken = "withdrawready" THEN "ready" ELSE "waiting"]
    /\ wplace' = [wplace EXCEPT ![c] = "none"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, rplace>>
    /\ flags' = NoFlags

\* drop: off the table, from an open state, never a card another open card
\* still needs (drop refuses it without its cascade; defer is drop from
\* waiting); a landed primary leaves only by the plan's remove. The primary's
\* work card leaves the fleet with it.
Drop ==
  \E c \in Card :
    /\ \/ IsOpen(state[c]) /\ ~Needed(c)
       \/ state[c] = "landed"
    /\ state' = [state EXCEPT ![c] = "off"]
    /\ wplace' = [wplace EXCEPT ![c] = "none"]
    /\ ClearReads(c)
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember>>
    /\ flags' = [NoFlags EXCEPT !.removing = (state[c] = "landed")]

\* The reversed witness "comeback": a dropped card is admitted again.
Comeback ==
  /\ Broken = "comeback"
  /\ \E c \in Card : state[c] = "off" /\ Move(c, "waiting")

\* waive: the coordinator waives a need; a waived need counts as met. The ack
\* moves a waiting card ready in the same step when nothing else holds it
\* (steps_ack.go waive).
Waive ==
  \E c \in Card :
    \E n \in needs[c] \ waived[c] :
      /\ waived' = [waived EXCEPT ![c] = waived[c] \cup {n}]
      /\ \/ state' = state
         \/ /\ ~IsSentinel(c)
            /\ state[c] = "waiting"
            /\ PositionClear(c)
            /\ \A x \in needs[c] : x \in everLanded \/ x \in waived'[c]
            /\ state' = [state EXCEPT ![c] = "ready"]
      /\ UNCHANGED <<needs, everLanded, members, wplace, wmember, rplace>>
      /\ flags' = NoFlags

Next ==
  \/ Resolve \/ Start \/ Take \/ Finish \/ Withdraw \/ FleetDown \/ FleetUp
  \/ Ask \/ AskAnother \/ ReadStart \/ Read
  \/ Accept \/ Rework \/ Return \/ Redo \/ Merge \/ Release \/ AddMove
  \/ NeedsSet \/ NeedsWait \/ NeedsWithdraw
  \/ Drop \/ Comeback \/ Waive

Spec == Init /\ [][Next]_vars

\* ------------------------------------------------------------- the rules

\* NoNeedCycle: no card's needs, followed transitively, come back to it.
NoNeedCycle == \A c \in Card : c \notin UNION {ReachN(n) : n \in needs[c]}

\* Rule11 (docs/SPEC-SPRINT.md section 9, rule 11; check.go): a primary past
\* working (review, merging, landed) has every need landed or waived. A ready
\* or working card may hold a need not landed between a needs rewrite and the
\* tick that moves it back to waiting (NeedsWait, NeedsWithdraw).
Rule11 ==
  \A c \in Card \ Sentinels :
    state[c] \in {"review", "merging", "landed"} => NeedsMet(c)

\* NoCardLostOrTwice: a card that leaves the table (a drop or a remove) never
\* comes back; its record is kept, unplaced, and no card is admitted twice.
NoCardLostOrTwice ==
  [][\A c \in Card : state[c] = "off" => state'[c] = "off"]_vars

\* MovesOnlyByTable: every change of cell is a lawful move, a drop from an
\* open state, or a remove of a landed card.
MovesOnlyByTable ==
  [][\A c \in Card :
    \/ state'[c] = state[c]
    \/ Legal(state[c], state'[c])
    \/ /\ state'[c] = "off"
       /\ (IsOpen(state[c]) \/ flags'.removing)]_vars

\* ReadyToWaitingOnlyByInsertOrNeeds: a primary moves ready -> waiting only as
\* the effect of inserting a sentinel in front of it, or with a named need
\* that has not landed (lifecycle.go needsOpen).
ReadyToWaitingOnlyByInsertOrNeeds ==
  [][\A c \in Card :
    state[c] = "ready" /\ state'[c] = "waiting" => flags'.inserting \/ ~((NeedsMet(c))')]_vars

\* LandedIsFinal: a landed primary stays landed; only a remove takes it off
\* the table, and it never moves to another state.
LandedIsFinal ==
  [][\A c \in Card : state[c] = "landed" => state'[c] \in {"landed", "off"}]_vars

\* NeedsLandFirst: a card leaves waiting for ready only with every need (as
\* the step leaves them) landed or waived.
NeedsLandFirst ==
  [][\A c \in Card :
    state[c] = "waiting" /\ state'[c] = "ready" => (NeedsMet(c))']_vars

\* NeedsHeldIntoWorking: no primary moves into working (a deal, a rework or a
\* redo delegated at once) with a need not landed (unmet, lifecycle.go: the
\* lifecycle refuses every move to working whose card's needs have not landed).
NeedsHeldIntoWorking ==
  [][\A c \in Card \ Sentinels :
    state'[c] = "working" /\ state[c] # "working" => (NeedsMet(c))']_vars

\* NeedsWithdrawalWaits (PR #96; rule 11, check.go, for the withdrawal): a
\* working card whose needs are not all landed or waived and whose dealt work
\* card goes back to the pool ends the step waiting, never ready or working.
NeedsWithdrawalWaits ==
  [][\A c \in Card :
    /\ state[c] = "working"
    /\ ~NeedsMet(c)
    /\ wplace[c] \in {"ready", "working"}
    /\ wplace'[c] = "none"
    /\ state'[c] # "off"
    => state'[c] = "waiting"]_vars

\* WorkingToWaitingOnlyOnNeeds: PR #96's move takes a working card back to
\* waiting only when its named needs, as the step found them, have not landed.
WorkingToWaitingOnlyOnNeeds ==
  [][\A c \in Card :
    state[c] = "working" /\ state'[c] = "waiting" => ~NeedsMet(c)]_vars

\* WorkingOnAnUpMember: a card in a member's working cell sits on an up member.
WorkingOnAnUpMember ==
  [][\A c \in Card :
    wplace[c] = "working" => members[wmember[c]] = "up"]_vars

\* AcceptHeld: no primary enters merging without ok read cards from as many
\* different readers at its head as its tier needs.
AcceptHeld == \A c \in Card : state[c] = "merging" => Acceptable(c)

\* NeverAcceptBroken: no primary enters merging with a broken read standing at
\* its head.
NeverAcceptBroken == \A c \in Card : state[c] = "merging" => ~BrokenRead(c)

\* NeverReadByItsWorker: a primary's read is never asked of its own worker.
NeverReadByItsWorker ==
  \A c \in Card, r \in Reader : rplace[c][r] # "none" => r # Worker(c)

=============================================================================
