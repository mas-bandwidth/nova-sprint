-------------------------------- MODULE SprintTables ----------------------------
\* The card lifecycle of nova-sprint (docs/SPEC-SPRINT.md section 3;
\* internal/sprint/lifecycle.go), the fleet side of the work table
\* (internal/sprint/refmodel/actions.go Start, Take, Finish, FleetDown,
\* FleetUp) and the reads and the accept (refmodel actions.go Ask, AskAnother,
\* ReadStart, Read, Accept, held to the code's ReadsNeededIn). A primary's six
\* states and the legal moves between them, in one table, plus the fleet (each
\* member up or down, a working primary's work card on one member's row in a
\* place none, ready, working, finished or withdrawn) and the readers (a read
\* card on one reader's row in a place none, asked, reading, ok or broken).
\*
\* A primary's state is the work-table column it is placed in. The six states
\* are waiting, ready, working, review, merging, landed; landed is final. Off
\* the table (a drop) is not a state: a primary that stops any other way than
\* landing leaves the table with its record, outcome and reason kept, unplaced.
\*
\* Moves, one per verb (lifecycle.go Moves):
\*   resolve      waiting -> ready    (everything it needs has landed)
\*   start        ready   -> working  (a work card is cut and dealt)
\*   finish       working -> review
\*   fleet down   working -> ready
\*   accept       review  -> merging
\*   rework       review  -> working   (a member is up)
\*   rework       review  -> ready     (no member is up)
\*   return       merging -> review
\*   redo         merging -> working   (a member is up)
\*   redo         merging -> ready     (no member is up)
\*   merge        merging -> landed
\*   release      waiting -> landed    (a sentinel only)
\*   add          ready   -> waiting   (a sentinel inserted in front, or a
\*                                      needs set: a ready card gains a need
\*                                      not landed)
\* Off the table (drop) is legal from every open state.
\*
\* The fleet side (refmodel actions.go) follows the code:
\*   Start(p, m)     the deal: a ready primary's work card is dealt to an up
\*                   member's row (place ready); a withdrawn card is dealt
\*                   again at a new generation.
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
\*                   than its worker (ReadersWanted: one for a flash card, two
\*                   for a pro card).
\*   AskAnother(c)   one more reader for a primary already asked, answering a
\*                   broken read or reads exhausted.
\*   ReadStart(r)    the reader moves its own asked read to reading.
\*   Read(r)         the reader records ok or broken on a read asked or
\*                   reading.
\*   Accept(c)       review -> merging when the primary has the reads its tier
\*                   needs and no broken read stands at its head; its asked
\*                   and reading read cards retire.
\*
\* Invariants: TypeOK, NoNeedCycle, NoCardLostOrTwice, and the action
\* properties MovesOnlyByTable (every change of cell is a Moves row, a drop
\* from an open state, or a remove of a landed card),
\* ReadyToWaitingOnlyByInsertOrNeedsset, LandedIsFinal, NeedsLandFirst and
\* WorkingOnAnUpMember (a card in a member's
\* working cell sits on an up member), plus AcceptHeld (no primary enters
\* merging short of its reads), NeverAcceptBroken (no primary enters merging
\* with a broken read at its head) and NeverReadByItsWorker (a read is never
\* asked of the reader that worked the card).
\*
\* Refmodel invariants not stated here. The pending-operation fence (D1: Free,
\* actions.go free, the refusal "operation %s is pending (D1: Free)" that
\* guards every one of these actions at actions.go:300, 352, 376, 1071, 1122)
\* is left out: this module has no pending operation, a step is one atomic
\* move, so there is nothing for the fence to fence. F3 (a stopped stream's
\* judgment and a skipped repair) and F4 (a merge need placed, in another
\* stream and not landed) are left out too: they belong to the cards that add
\* merge and return, and crash and repair.
\*
\* Reversed witnesses (Broken):
\*   "insert"    the add move runs ready -> waiting without inserting and
\*               without a needs set.
\*   "needless"   resolve drops the needs check, so a card with an unmet need
\*                still moves waiting -> ready.
\*   "downkeeps"  fleet down leaves a working card on the member that went down.
\*   "shortaccept" the accept drops the read guard and accepts with one read
\*                fewer than the tier needs (AcceptHeld fails).
\*   "acceptbroken" the accept drops the no-broken guard and accepts with a
\*                broken read at the head (NeverAcceptBroken fails).
\*
\* Not modelled here (other cards): merge and return, crash and repair.
EXTENDS Naturals, FiniteSets

CONSTANTS
  Card,         \* the card ids
  Sentinels,    \* the sentinel ids, a subset of Card
  NeedsOf,      \* NeedsOf[c] is c's stored needs, a subset of Card
  Score,        \* Score[c] is c's place in line, a distinct number
  InitOf,       \* InitOf[c] is c's cell at the start: waiting or ready
  Member,       \* the fleet's member ids
  InitUp,       \* the members up at the start
  Reader,       \* the readers: one per fleet member (MemberReader) plus friends
  Flash,        \* the flash-tier primaries, a subset of Card (the rest are pro)
  MemberReader, \* the reader on each member (readers == workers per machine)
  Broken        \* "none", or the one reversed witness to break

States == {"waiting", "ready", "working", "review", "merging", "landed"}
Cells == States \cup {"off"}

\* The work card's place on a member's row (refmodel state.go FleetCells).
WPlaces == {"none", "ready", "working", "finished", "withdrawn"}

\* A member's status (refmodel state.go: Up, Down).
MStatus == {"up", "down"}

\* A read card's cell on a reader's row (refmodel state.go ReadCard.Place).
RPlaces == {"none", "asked", "reading", "ok", "broken"}

\* The lifecycle: every legal move, and nothing else (lifecycle.go Moves).
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
    <<"ready", "waiting">> }

Legal(from, to) == <<from, to>> \in Moves

\* IsOpen: a primary in s has not landed, so drop may take it off the table.
IsOpen(s) == s \in {"waiting", "ready", "working", "review", "merging"}

IsSentinel(c) == c \in Sentinels

\* The step's plan flags (lifecycle.go Plan): releasing lands a sentinel,
\* inserting moves a primary ready -> waiting, removing takes a landed primary
\* off the table. Each is a nondeterministic choice of the plan the step runs.
\* The inserting flag stands for both of the code's ready -> waiting causes
\* (lifecycle.go:247 admits the move by the plan's inserting flag or by the
\* entry's needs set, steps_edit.go): the module's needs are a constant, so a
\* needs set has no state of its own and both causes are this one flag.
Flags == [releasing : BOOLEAN, inserting : BOOLEAN, removing : BOOLEAN]

VARIABLES state, waived, flags, members, wplace, wmember, rplace

vars == <<state, waived, flags, members, wplace, wmember, rplace>>

TypeOK ==
  /\ state \in [Card -> Cells]
  /\ waived \in [Card -> SUBSET Card]
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

\* NeedsMet (unmet, lifecycle.go): every stored need has landed or was waived.
NeedsMet(c) ==
  \A n \in NeedsOf[c] : state[n] = "landed" \/ n \in waived[c]

\* Nothing in line before c still blocks it.
PositionClear(c) == \A q \in PositionWaits(c) : state[q] = "landed"

Init ==
  /\ state = InitOf
  /\ \A c \in Sentinels : state[c] = "waiting"
  /\ waived = [c \in Card |-> {}]
  /\ flags = [releasing |-> FALSE, inserting |-> FALSE, removing |-> FALSE]
  /\ members = [m \in Member |-> IF m \in InitUp THEN "up" ELSE "down"]
  /\ wplace = [c \in Card |-> "none"]
  /\ wmember = [c \in Card |-> CHOOSE m \in Member : TRUE]
  /\ rplace = [c \in Card |-> [r \in Reader |-> "none"]]

\* The effect of one move: c to its new cell; the fleet is untouched and the
\* step's flags are chosen freely.
Move(c, to) ==
  /\ state' = [state EXCEPT ![c] = to]
  /\ rplace' = rplace
  /\ UNCHANGED <<waived, members, wplace, wmember>>
  /\ flags' \in Flags

\* ------------------------------------------------------------ the reads

\* Needs (readers.go ReadsNeeded, the tier rule of ReadsNeededIn): how many
\* different readers' ok reads at its head make the primary acceptable: one
\* for a flash card, two for a pro card. The sprint's setting override
\* (nova-sprint set --reads) and a landed card's pinned count are outside this
\* cut (reads and accept under the tier's rule).
Needs(c) == IF c \in Flash THEN 1 ELSE 2

\* The worker of a primary is the reader of the machine that worked its card
\* (reader-<m>, one reader per fleet member): its read is never asked of its
\* own worker (NeverReadByItsWorker).
Worker(c) == MemberReader[wmember[c]]

\* The primary's live read cards (any cell but none) and the readers whose read
\* of it is ok at its head.
LiveReads(c) == {r \in Reader : rplace[c][r] # "none"}
OkReaders(c) == {r \in Reader : rplace[c][r] = "ok"}
BrokenRead(c) == \E r \in Reader : rplace[c][r] = "broken"

\* A reader the primary can still be asked of: another reader, not its worker,
\* with no read card of the primary cut yet.
FreshReader(c) == Reader \ ({Worker(c)} \cup LiveReads(c))

\* Acceptable (readers.go): as many different readers as it needs have an ok
\* read at its head.
Acceptable(c) == Cardinality(OkReaders(c)) >= Needs(c)

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

\* Start (refmodel Start, actions.go line 299): the deal. ready -> working; the
\* work card is dealt to an up member's row (place ready). A withdrawn card is
\* dealt again (a redeal); a card never dealt is cut now. The deal takes any up
\* member (the round-robin NextMember is a choice, not a safety rule).
Start ==
  \E c \in Card, m \in Member :
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    /\ wplace[c] \in {"none", "withdrawn"}
    /\ members[m] = "up"
    /\ state' = [state EXCEPT ![c] = "working"]
    /\ wplace' = [wplace EXCEPT ![c] = "ready"]
    /\ wmember' = [wmember EXCEPT ![c] = m]
    /\ rplace' = rplace
    /\ UNCHANGED <<waived, members>>
    /\ flags' \in Flags

\* Take (refmodel Take, actions.go line 351): the worker on an up member moves
\* a dealt (ready) work card to working. The primary's state does not change.
\* The take names the live generation (F2): a stale take is a refusal, so the
\* model has no step for it and no separate invariant to state.
Take ==
  \E c \in Card :
    /\ state[c] = "working"
    /\ wplace[c] = "ready"
    /\ members[wmember[c]] = "up"
    /\ wplace' = [wplace EXCEPT ![c] = "working"]
    /\ UNCHANGED <<state, waived, members, wmember, rplace>>
    /\ flags' \in Flags

\* Finish (refmodel Finish, actions.go line 375): working -> review; the work
\* card finishes in its member's working cell. Accepted only for the live
\* generation in the member's working cell (D3): a stale finish is a refusal.
Finish ==
  \E c \in Card :
    /\ state[c] = "working"
    /\ wplace[c] = "working"
    /\ members[wmember[c]] = "up"
    /\ state' = [state EXCEPT ![c] = "review"]
    /\ wplace' = [wplace EXCEPT ![c] = "finished"]
    /\ UNCHANGED <<waived, members, wmember, rplace>>
    /\ flags' \in Flags

\* FleetDown (refmodel FleetDown, actions.go line 1070): the member goes down;
\* its ready and working cards are dealt to the other up members (place ready,
\* their primaries stay working), or, with no member up, withdrawn (their
\* primaries return to ready). The reversed witness "downkeeps" leaves a
\* working card on the member that went down.
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
    /\ UNCHANGED <<waived, rplace>>
    /\ flags' \in Flags

\* FleetUp (refmodel FleetUp, actions.go line 1121): the member comes up. The
\* levelling of the ready queues (T4, the Level action) is not modelled.
FleetUp ==
  \E m \in Member :
    /\ members[m] = "down"
    /\ members' = [members EXCEPT ![m] = "up"]
    /\ UNCHANGED <<state, waived, wplace, wmember, rplace>>
    /\ flags' \in Flags

\* Ask (refmodel Ask, actions.go line 420): the primary in review asks one more
\* read of a reader other than its worker, distinct from the readers already
\* standing. Refused with fewer than two readers; never once a read found it
\* broken (the rework asks next).
Ask ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ ~BrokenRead(c)
    /\ Cardinality(Reader) >= 2
    /\ Cardinality(LiveReads(c)) < Needs(c)
    /\ \E r \in FreshReader(c) :
         /\ rplace' = [rplace EXCEPT ![c][r] = "asked"]
         /\ UNCHANGED <<state, waived, members, wplace, wmember>>
    /\ flags' \in Flags

\* AskAnother (refmodel AskAnother, actions.go line 480): one more reader for a
\* primary already asked, answering a broken read or reads exhausted.
AskAnother ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ LiveReads(c) # {}
    /\ (BrokenRead(c) \/ Cardinality(LiveReads(c)) >= Needs(c))
    /\ \E r \in FreshReader(c) :
         /\ rplace' = [rplace EXCEPT ![c][r] = "asked"]
         /\ UNCHANGED <<state, waived, members, wplace, wmember>>
    /\ flags' \in Flags

\* ReadStart (refmodel ReadStart, actions.go line 514): the reader moves its own
\* asked read to reading.
ReadStart ==
  \E r \in Reader :
    /\ \E c \in Card : rplace[c][r] = "asked"
    /\ \E c \in Card :
         /\ rplace[c][r] = "asked"
         /\ rplace' = [rplace EXCEPT ![c][r] = "reading"]
         /\ UNCHANGED <<state, waived, members, wplace, wmember>>
    /\ flags' \in Flags

\* Read (refmodel Read, actions.go line 536): the reader records ok or broken.
\* A report on a read still asked is the begin and the report in one step.
Read ==
  \E r \in Reader :
    /\ \E c \in Card : rplace[c][r] \in {"asked", "reading"}
    /\ \E c \in Card, v \in {"ok", "broken"} :
         /\ rplace[c][r] \in {"asked", "reading"}
         /\ rplace' = [rplace EXCEPT ![c][r] = v]
         /\ UNCHANGED <<state, waived, members, wplace, wmember>>
    /\ flags' \in Flags

\* accept: review -> merging when the primary has the reads its tier needs
\* (refmodel Accept, actions.go line 593, held to ReadsNeededIn: one for a
\* flash card, two for a pro card) and no broken read stands at its head
\* (steps_review.go hasBrokenAtHead: a broken verdict at the same head
\* outweighs any number of oks; NeverAcceptBroken). Named, one primary at a
\* time, all or nothing (D4); its asked and reading read cards retire (F1).
\* The reversed witnesses: "shortaccept" drops the read guard and accepts with
\* one read fewer, and "acceptbroken" drops the no-broken guard and accepts
\* with a broken read at the head.
Accept ==
  \E c \in Card :
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ \/ /\ \/ Acceptable(c)
             \/ /\ Broken = "shortaccept"
                /\ Cardinality(OkReaders(c)) >= Needs(c) - 1
          /\ \/ Broken = "acceptbroken" \/ ~BrokenRead(c)
    /\ state' = [state EXCEPT ![c] = "merging"]
    /\ RetireOutstanding(c)
    /\ UNCHANGED <<waived, members, wplace, wmember>>
    /\ flags' \in Flags

\* rework: review -> working (a member is up) or review -> ready (none is up);
\* the primary's read cards retire (a new attempt).
Rework ==
  \E c \in Card :
    /\ state[c] = "review"
    /\ \/ state' = [state EXCEPT ![c] = "working"]
       \/ state' = [state EXCEPT ![c] = "ready"]
    /\ ClearReads(c)
    /\ UNCHANGED <<waived, members, wplace, wmember>>
    /\ flags' \in Flags

\* return: merging -> review. The read cards at the attempt stand (the refmodel's
\* Return does not retire them); the machine's hold on a returned card is out of
\* this cut.
Return == \E c \in Card : state[c] = "merging" /\ Move(c, "review")

\* redo: merging -> working (a member is up) or merging -> ready (none is up);
\* the read cards retire (a new attempt).
Redo ==
  \E c \in Card :
    /\ state[c] = "merging"
    /\ \/ state' = [state EXCEPT ![c] = "working"]
       \/ state' = [state EXCEPT ![c] = "ready"]
    /\ ClearReads(c)
    /\ UNCHANGED <<waived, members, wplace, wmember>>
    /\ flags' \in Flags

\* merge: merging -> landed.
Merge == \E c \in Card : state[c] = "merging" /\ Move(c, "landed")

\* release: waiting -> landed, a sentinel only, and only by the plan's release.
Release ==
  \E c \in Card :
    /\ IsSentinel(c)
    /\ state[c] = "waiting"
    /\ flags.releasing
    /\ Move(c, "landed")

\* add: ready -> waiting, as the effect of inserting a sentinel in front, or of
\* rewriting the card's needs (needs set, steps_edit.go): a ready card that
\* gains a need not landed moves back to waiting. The module's needs are a
\* constant, so both causes are the one plan flag inserting. The reversed
\* witness "insert" lets it run without inserting and without a needs set.
AddMove ==
  \E c \in Card :
    /\ state[c] = "ready"
    /\ \/ Broken = "insert" \/ flags.inserting
    /\ Move(c, "waiting")

\* drop: off the table, from an open state; a landed primary leaves only by the
\* plan's remove. The primary's work card leaves the fleet with it.
Drop ==
  \E c \in Card :
    /\ state[c] \in States
    /\ IsOpen(state[c]) \/ flags.removing
    /\ state' = [state EXCEPT ![c] = "off"]
    /\ wplace' = [wplace EXCEPT ![c] = "none"]
    /\ ClearReads(c)
    /\ UNCHANGED <<waived, members, wmember>>
    /\ flags' \in Flags

\* waive: the coordinator waives a need; a waived need counts as met.
Waive ==
  \E c \in Card :
    \E n \in NeedsOf[c] :
      /\ waived' = [waived EXCEPT ![c] = waived[c] \cup {n}]
      /\ UNCHANGED <<state, members, wplace, wmember, rplace>>
      /\ flags' \in Flags

Next ==
  \/ Resolve \/ Start \/ Take \/ Finish \/ FleetDown \/ FleetUp
  \/ Ask \/ AskAnother \/ ReadStart \/ Read
  \/ Accept \/ Rework \/ Return \/ Redo \/ Merge \/ Release \/ AddMove
  \/ Drop \/ Waive

Spec == Init /\ [][Next]_vars

\* ------------------------------------------------------------- the rules

\* NoNeedCycle: no card's needs, followed transitively, come back to it.
Reach(c) ==
  LET RECURSIVE Desc(_)
      Desc(x) == {x} \cup UNION {Desc(n) : n \in NeedsOf[x]}
  IN Desc(c)

NoNeedCycle == \A c \in Card : c \notin (Reach(c) \ {c})

\* NoCardLostOrTwice: a card that leaves the table (a drop or a remove) never
\* comes back; its record is kept, unplaced, and no card is admitted twice.
NoCardLostOrTwice ==
  [][\A c \in Card : state[c] = "off" => state'[c] = "off"]_vars

\* MovesOnlyByTable: every change of cell is a row of Moves, a drop from an
\* open state, or a remove of a landed card.
MovesOnlyByTable ==
  [][\A c \in Card :
    \/ state'[c] = state[c]
    \/ <<state[c], state'[c]>> \in Moves
    \/ /\ state'[c] = "off"
       /\ (IsOpen(state[c]) \/ flags.removing)]_vars

\* ReadyToWaitingOnlyByInsertOrNeedsset: a primary moves ready -> waiting only
\* as the effect of inserting a sentinel in front of it, or of rewriting its
\* needs (needs set, steps_edit.go: a ready card that gains a need not landed).
\* The module's needs are a constant, so both causes are the one plan flag
\* inserting.
ReadyToWaitingOnlyByInsertOrNeedsset ==
  [][\A c \in Card :
    state[c] = "ready" /\ state'[c] = "waiting" => flags.inserting]_vars

\* LandedIsFinal: a landed primary stays landed; only a remove takes it off
\* the table, and it never moves to another state.
LandedIsFinal ==
  [][\A c \in Card : state[c] = "landed" => state'[c] \in {"landed", "off"}]_vars

\* NeedsLandFirst: a card leaves waiting for ready only with every need landed
\* or waived.
NeedsLandFirst ==
  [][\A c \in Card :
    state[c] = "waiting" /\ state'[c] = "ready" => NeedsMet(c)]_vars

\* WorkingOnAnUpMember: a card in a member's working cell sits on an up member
\* (a member that goes down keeps no working card; a take moves a ready card to
\* working only on an up member).
WorkingOnAnUpMember ==
  [][\A c \in Card :
    wplace[c] = "working" => members[wmember[c]] = "up"]_vars

\* AcceptHeld: the accept holds to the reads: no primary enters merging without
\* ok read cards from as many different readers at its head as its tier needs
\* (readers.go ReadsNeededIn).
AcceptHeld == \A c \in Card : state[c] = "merging" => Acceptable(c)

\* NeverAcceptBroken: the accept never queues a head a reader found broken
\* (readers.go hasBrokenAtHead, docs/SPEC-SPRINT.md section 8, the read-broken
\* rule): no primary enters merging with a broken read standing at its head.
NeverAcceptBroken == \A c \in Card : state[c] = "merging" => ~BrokenRead(c)

\* NeverReadByItsWorker: a primary's read is never asked of its own worker
\* (readers.go: the reader ran the program itself; a read is asked of readers
\* other than its worker).
NeverReadByItsWorker ==
  \A c \in Card, r \in Reader : rplace[c][r] # "none" => r # Worker(c)

=============================================================================
