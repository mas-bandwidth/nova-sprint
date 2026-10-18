-------------------------------- MODULE SprintTables ----------------------------
\* The card lifecycle of nova-sprint (docs/SPEC-SPRINT.md section 3, the
\* lifecycle and its inventory of moves; internal/sprint/lifecycle.go Moves and
\* Lawful), the fleet side of the work table (internal/sprint/refmodel
\* actions.go Start, Take, Finish, FleetDown, FleetUp), the reads and the
\* accept (refmodel actions.go Ask, AskAnother, ReadStart, Read, Accept, held
\* to the code's ReadsNeededIn) and the merge side (refmodel actions.go Rework,
\* Drop, Rank, Return, MergeGreen, MergeStop, MergeRefused, MergeRed, Resume),
\* and the outside events and the repair (refmodel actions.go CiRed, CiGreen,
\* Ack, Crash, Repair).
\* A primary's six states and the lawful moves
\* between them, in one table, plus its named needs (a variable: needs set and
\* a brief edit's DEPENDS-ON rewrite them), the fleet (each member up or down, a
\* working primary's work card on one member's row in a place none, ready,
\* working, finished or withdrawn), the readers (a read card on one
\* reader's row in a place none, asked, reading, ok or broken) and the merge
\* side: each primary belongs to a stream; a stream is waiting, merging,
\* stopped (with a cause) or landed, and a primary in merge sits in a merge
\* cell none, queued, merged, stuck (a fact on it stopped the stream) or
\* returned (sent back from merging).
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
\*   Finish         working -> review | waiting | ready   finish: waiting when
\*                                      a named need is open (the needs
\*                                      record, the lifecycle's own
\*                                      predicate), review otherwise
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
\*   MergeGreen     merging -> landed   the lander's merge, the queued batch
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
\* The merge side (refmodel actions.go) follows the code:
\*   Rework(p)       review -> working or review -> ready (a member up or not).
\*   Return(p)       merging -> review; the card leaves merge into returned and
\*                   the stream's state follows (G4).
\*   Redo(p)         merging -> working or merging -> ready (redo a conflicted
\*                   card); the card leaves merge into returned.
\*   MergeGreen(s)   the batch (the stream's queued cards, all of them here: a
\*                   prefix is always a batch) merges to dev: merging ->
\*                   landed, into merged; the stream's state follows (G4).
\*   MergeStop(s, p) a fact on a queued card stops the stream: the card goes
\*                   stuck, the stream stops (conflict).
\*   MergeRefused(s, p) the landing's refusal of the card's head: back to
\*                   review (paths or at the brief's bound) or ready (redo);
\*                   the card leaves merge into returned; the stream follows.
\*   MergeRed(s)     the branch red: the stream stops, no card moves.
\*   Resume(s)       a stopped stream resumes: the stuck cards queue again,
\*                   the cause clears, the stream's state follows.
\*   Drop(p)         off the table; the merge place goes, the stream follows.
\*   Rank(p)         a card ranked first (marked ranked), refused for a landed
\*                   primary (G6).
\*
\* The outside events and the repair (refmodel actions.go) follow the code:
\*   CiRed(p)       a red CI observation on a placed primary in any state opens
\*                  the ci judgment (the "red" cell) and moves nothing.
\*   CiGreen(p)     a green observation closes the ci judgment. The module keeps
\*                  only the open red: the green apart from none is read only by
\*                  the accept-holds logic, which is the merge card's.
\*   Ack(n)         the coordinator answers the open ci judgment on a card and
\*                  closes it (the blocked and skipped judgments the code also
\*                  answers are the reads and merge cards').
\*   Crash          a step over more than one table is cut before its last
\*                  write, the work table: the fleet writes are committed, the
\*                  work table is left at its pre-state, the deferred work-table
\*                  write is held in pendstate and the operation is pending.
\*   Repair         the deferred work-table write is applied and the operation
\*                  releases.
\* Only the steps over more than one table are cut: Start, Finish, FleetDown and
\* Drop; a move, waive, a CI observation and an ack write one table, and Take
\* and FleetUp write only the fleet table, so none of them is cut. While an
\* operation is pending, the fence (D1: Free) holds: no action but repair runs.
\*
\* Two specs: Spec (the ordinary steps alone) and CrashSpec (Spec plus the
\* outside events and the repair). The MCSprintTables and MCSprintTablesMerge
\* instances run Spec, whose state space the ci, pending and pendstate
\* variables (frozen there) do not change; the crash instance runs CrashSpec.
\*
\* THE RULES. Invariants: TypeOK, NoNeedCycle, Rule11 (docs/SPEC-SPRINT.md
\* section 9, rule 11, for the states nothing moves back to waiting: a primary
\* in review, merging or landed has every named need landed or waived; a ready
\* or working card that gains a need not landed is moved to waiting by the
\* tick, NeedsWait and NeedsWithdraw, held by NeedsWithdrawalWaits), AcceptHeld (no primary enters merging short of its
\* reads), NeverAcceptBroken (no primary enters merging with a broken read at
\* its head) and NeverReadByItsWorker (a read is never asked of the reader that
\* worked the card), HeldNeverWorks (a primary held by server-default is never
\* working: a rework answers the hold, ReworkHeldInPlace). Action properties: NoCardLostOrTwice, MovesOnlyByTable
\* (every change of cell is a lawful move, a drop from an open state, or a
\* remove of a landed card), ReadyToWaitingOnlyByInsertOrNeeds, LandedIsFinal,
\* NeedsLandFirst (waiting -> ready only with every need landed or waived),
\* NeedsHeldIntoWorking (no deal, rework or redo into working with a need not
\* landed), WorkingToWaitingOnlyOnNeeds, NeedsWithdrawalWaits (PR #96: a
\* working card withdrawn on its needs ends waiting, never ready),
\* WorkingOnAnUpMember, StreamAfter (G4: a stream's state is what its merge
\* cells and cause say it is) and RankRefusesLanded (G6: a landed primary is
\* never ranked).
\*
\* Refmodel invariants not stated here. The pending-operation fence (D1: Free,
\* actions.go free, the refusal "operation %s is pending (D1: Free)" that
\* guards every one of these actions) is held: a step over more than one table
\* is cut before its last write, the work table, and the operation stays pending
\* until repair; the crash's writes are all or nothing, so NoCardLostOrTwice and
\* every earlier invariant hold across a crash at any step. G3 (ExhaustNote, the
\* reads-exhausted or stranded judgment) needs read cards and their judgments,
\* which this module's accept stands in for and does not have. F3 (a stopped
\* stream's judgment and a skipped repair) is left out: this module's ack
\* answers only the ci judgment. F4 and D6 (a merge need placed, in another
\* stream and not landed; a stop on a cross-stream need) are left out: a stop
\* here is always a conflict on a card of the stream itself, and no merge card
\* names a need.
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
\*   "landunmerged" a card lands from review, no merge (MovesOnlyByTable).
\*   "returnlost"   a card returned from merging is dropped, its record lost
\*                  (NoCardLostOrTwice).
\*   "unland"       the short-reads pass sends a landed card back to review,
\*                  as ShortReadsBack planned before this card
\*                  (LandedIsFinal).
\*   "comeback"     a dropped card is admitted again (NoCardLostOrTwice).
\*   "withdrawmet"  a working card whose needs have all landed is withdrawn
\*                  to waiting (WorkingToWaitingOnlyOnNeeds).
\*   "finishunmet"  a finish drops the needs guard: a taken card given a need
\*                  not landed finishes to review with it (Rule11).
\*   "staleafter"   a needs write leaves waitsAfter as it was and the finish
\*                  reads it, not the needs: the go-table case (deal, gain a
\*                  need, needs set --clear, finish) sends the card to waiting
\*                  for a need it no longer names, which the lifecycle refuses
\*                  (WaitsAfterWithinNeeds, FinishAgreesWithLifecycle).
\*   "finishloop"   a refused finish is neither counted nor judged, so it is
\*                  sent again without end (FinishRefusalsBounded).
\*   "parkmoves"    the server-default hold of a ready primary moves it to
\*                  waiting, as seat_answers.go TickDecisionDefaults did
\*                  before 2026-10-17: a primary whose work card was
\*                  withdrawn is left waiting on no need (Rule2Withdrawn).
\*   "parkinplaceall" the hold keeps every ready primary in place, its work
\*                  card withdrawn or not: a held ready card a sentinel
\*                  insert would pull, which the lifecycle's hold guard
\*                  refuses with the whole add (HeldReadyIsWithdrawn).
\*   "reworkkeepshold" a rework of a held in-place primary deals it and leaves
\*                  the server-default hold on, as steps_review.go Rework did
\*                  before 2026-10-18 (deadlock j25715): the primary works
\*                  while held, and every finish is refused (HeldNeverWorks).
\*   "halfstep"     the fence is dropped, so a crash leaves a step half-written
\*                  (its fleet writes committed, its work-table write pending)
\*                  and other actions may act on the half-written table; a
\*                  repair then applies the stale deferred write
\*                  (NoCardLostOrTwice).
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
  StreamOf,     \* StreamOf[c] is c's stream
  Broken,       \* "none", or the one reversed witness to break
  MergeSide     \* the merge side is modelled (an instance turns it off)

States == {"waiting", "ready", "working", "review", "merging", "landed"}
Cells == States \cup {"off", "lost"}

\* The work card's place on a member's row (refmodel state.go FleetCells).
WPlaces == {"none", "ready", "working", "finished", "withdrawn"}

\* A member's status (refmodel state.go: Up, Down).
MStatus == {"up", "down"}

\* A read card's cell on a reader's row (refmodel state.go ReadCard.Place).
RPlaces == {"none", "asked", "reading", "ok", "broken"}

\* A primary's merge place (refmodel state.go MergeCells).
MCells == {"none", "queued", "merged", "stuck", "returned"}

\* A stream's state (refmodel state.go Stream.State) and the cause of a stop.
SCells == {"waiting", "merging", "stopped", "landed"}
SCauses == {"none", "conflict", "red"}

\* The streams of the cards.
Streams == {StreamOf[c] : c \in Card}

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
Flags == [releasing : BOOLEAN, inserting : BOOLEAN, removing : BOOLEAN, decisionPark : BOOLEAN, decisionRelease : BOOLEAN, finishing : BOOLEAN]

\* An ordinary step's plan: no flag. Release, the add that inserts a sentinel and
\* the remove set their own flag on the step they take (flags', read by the
\* rules), so a flag is the plan of that step and never a state carried on.
NoFlags == [releasing |-> FALSE, inserting |-> FALSE, removing |-> FALSE, decisionPark |-> FALSE, decisionRelease |-> FALSE, finishing |-> FALSE]

\* A red CI observation on a primary: none or an open red (refmodel Primary.CI
\* and the ci judgment; a green observation and an ack both close the red, and
\* the accept-holds logic that reads the green apart from none is the merge
\* card's, not here).
CIVals == {"", "red"}

\* The verbs a crash can cut: the steps over more than one table, cut before
\* their last write, the work table.
Verbs == {"start", "finish", "fleetdown", "drop"}

\* FinishBound is FinishRefusalBound (internal/sprint, 5) at the model's size: a
\* finish refused more than this many times on one attempt raises one judgment.
FinishBound == 1

VARIABLES briefGeneration, attemptGeneration, headGeneration, keptHead, operationalEdits, state, needs, waived, everLanded, flags, members, wplace, wmember, rplace, mg, sstream, scause, ranked, pending, ci, pendstate

GenerationVars == <<briefGeneration, attemptGeneration, headGeneration, keptHead, operationalEdits>>

vars == <<briefGeneration, attemptGeneration, headGeneration, keptHead, operationalEdits, state, needs, waived, everLanded, flags, members, wplace, wmember, rplace, mg, sstream, scause, ranked, pending, ci, pendstate>>

VARIABLES parkState, parkSaved, parkBound, parkCause, parkHead, parkReads, parkEvent
parkingVars == <<parkState,parkSaved,parkBound,parkCause,parkHead,parkReads,parkEvent>>

\* The needs record's views (steps_edit.go FieldWaitsAfter, steps_work.go
\* FieldFinishRefusals): waitsAfter[c] is the needs c gained while working, held
\* inside needs[c] (the rule-11 marker; WaitsAfterWithinNeeds); finishTries[c]
\* is the finishes of c's attempt the lifecycle refused (a ghost count, always
\* kept), finishRefusals[c] is the counter the code records on the work card,
\* and finishJudged[c] the NFinishRefused judgments raised for the attempt.
VARIABLES waitsAfter, finishTries, finishRefusals, finishJudged
finishVars == <<waitsAfter, finishTries, finishRefusals, finishJudged>>
allTableVars == <<vars,parkingVars,finishVars>>

TypeOK ==
  /\ briefGeneration \in [Card -> 0..1]
  /\ attemptGeneration \in [Card -> 0..1]
  /\ headGeneration \in [Card -> 0..1]
  /\ keptHead \in [Card -> BOOLEAN]
  /\ operationalEdits \subseteq Card
  /\ state \in [Card -> Cells]
  /\ needs \in [Card -> SUBSET Card]
  /\ waived \in [Card -> SUBSET Card]
  /\ everLanded \in SUBSET Card
  /\ flags \in Flags
  /\ members \in [Member -> MStatus]
  /\ wplace \in [Card -> WPlaces]
  /\ wmember \in [Card -> Member]
  /\ rplace \in [Card -> [Reader -> RPlaces]]
  /\ mg \in [Card -> MCells]
  /\ sstream \in [Streams -> SCells]
  /\ scause \in [Streams -> SCauses]
  /\ ranked \in [Card -> BOOLEAN]
  /\ pending \in Verbs \cup {""}
  /\ ci \in [Card -> CIVals]
  /\ pendstate \in [Card -> Cells]
  /\ waitsAfter \in [Card -> SUBSET Card]
  /\ finishTries \in [Card -> 0..(FinishBound + 1)]
  /\ finishRefusals \in [Card -> 0..(FinishBound + 1)]
  /\ finishJudged \in [Card -> 0..2]

\* Up: the members up now (refmodel State.Up).
Up == {m \in Member : members[m] = "up"}

\* Free: no operation is pending (the D1 fence). Every action that is not a
\* crash or a repair runs only while it holds; the reversed witness "halfstep"
\* drops it, so a step cut by a crash is left half-written and other actions may
\* act on the half-written table.
Free == pending = ""

\* FenceOK: the step may run (the fence is free, or the witness drops it).
FenceOK == \/ Broken = "halfstep" \/ Free

\* ------------------------------------------------------------- the merge side

\* Queued/Stuck: the stream's cards in each merge cell.
Queued(s) == {c \in Card : StreamOf[c] = s /\ mg[c] = "queued"}
Stuck(s)  == {c \in Card : StreamOf[c] = s /\ mg[c] = "stuck"}

\* StreamState (refmodel streamAfter, G4): the stream state its cause, merge
\* cells and primaries say it should hold. A stopped stream stays stopped (its
\* cause persists); with cards queued it is merging; landed when every primary
\* of it on the table has landed and at least one has; else waiting (an empty
\* or all-dropped stream is waiting, not landed).
StreamState(s, stv, mgv, scausev) ==
  IF scausev[s] # "none" THEN "stopped"
  ELSE IF \E c \in Card : StreamOf[c] = s /\ mgv[c] = "queued" THEN "merging"
  ELSE IF /\ \E c \in Card : StreamOf[c] = s /\ stv[c] = "landed"
          /\ \A c \in Card : StreamOf[c] = s => stv[c] \in {"landed", "off"}
       THEN "landed"
  ELSE "waiting"

\* Follows: the stream s's state in the next state is what its merge cells and
\* cause say it is; the other streams are untouched.
Follows(s) ==
  sstream' = [t \in Streams |-> IF t = s THEN StreamState(s, state', mg', scause') ELSE sstream[t]]

\* Queue(c): c queues in merge and its stream follows; ranked is untouched.
Queue(c) ==
  /\ mg' = [mg EXCEPT ![c] = "queued"]
  /\ scause' = scause
  /\ Follows(StreamOf[c])
  /\ UNCHANGED ranked

\* Returned(c): c leaves merge (queued or stuck) into returned, its stream follows.
Returned(c) ==
  /\ mg' = [mg EXCEPT ![c] = "returned"]
  /\ scause' = scause
  /\ Follows(StreamOf[c])
  /\ UNCHANGED ranked

\* The merge variables, frozen when the merge side is off.
MergeVars == <<mg, sstream, scause, ranked>>

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

TableInit ==
  /\ briefGeneration = [c \in Card |-> 0]
  /\ attemptGeneration = [c \in Card |-> 0]
  /\ headGeneration = [c \in Card |-> 0]
  /\ keptHead = [c \in Card |-> FALSE]
  /\ operationalEdits = {}
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
  /\ mg = [c \in Card |-> "none"]
  /\ sstream = [s \in Streams |-> "waiting"]
  /\ scause = [s \in Streams |-> "none"]
  /\ ranked = [c \in Card |-> FALSE]
  /\ pending = ""
  /\ ci = [c \in Card |-> ""]
  /\ pendstate = InitOf

ParkCard == CHOOSE c \in Card \ Sentinels : TRUE
Init == TableInit
        /\ parkState = "none" /\ parkSaved = "" /\ parkBound = TRUE
        /\ parkCause = FALSE /\ parkHead = headGeneration[ParkCard]
        /\ parkReads = rplace[ParkCard] /\ parkEvent = "init"
        /\ waitsAfter = [c \in Card |-> {}]
        /\ finishTries = [c \in Card |-> 0]
        /\ finishRefusals = [c \in Card |-> 0]
        /\ finishJudged = [c \in Card |-> 0]

\* The effect of one move: c to its new cell; the fleet is untouched and the
\* step carries no plan flag. The deferred write tracks the work table.
Move(c, to) ==
  /\ state' = [state EXCEPT ![c] = to]
  /\ everLanded' = IF to = "landed" THEN everLanded \cup {c} ELSE everLanded
  /\ UNCHANGED <<needs, waived, members, wplace, wmember, rplace, mg, sstream, scause, ranked, ci>>
  /\ pendstate' = IF Free THEN state' ELSE pendstate
  /\ pending' = pending
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
    /\ FenceOK
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
    /\ FenceOK
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    /\ Broken = "dealunmet" \/ NeedsMet(c)
    /\ wplace[c] \in {"none", "withdrawn"}
    /\ members[m] = "up"
    /\ state' = [state EXCEPT ![c] = "working"]
    /\ wplace' = [wplace EXCEPT ![c] = "ready"]
    /\ wmember' = [wmember EXCEPT ![c] = m]
    /\ UNCHANGED <<needs, waived, everLanded, members, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* Take (refmodel Take): the worker on an up member moves a dealt (ready) work
\* card to working. The primary's state does not change.
Take ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] = "working"
    /\ wplace[c] = "ready"
    /\ members[wmember[c]] = "up"
    /\ NeedsMet(c)
    /\ wplace' = [wplace EXCEPT ![c] = "working"]
    /\ UNCHANGED <<state, needs, waived, everLanded, members, wmember, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* GenerationTarget: where a finish with no named need open leaves the card: review,
\* or ready when the brief generation moved (finishPlan's AttemptPredatesBrief).
GenerationTarget(c) ==
  IF Broken = "oldheadreviewed" \/ attemptGeneration[c] = briefGeneration[c] \/ keptHead[c]
  THEN "review" ELSE "ready"

\* WaitsOpen: waitsAfter[c] names a need that has not landed and was not waived.
WaitsOpen(c) == \E n \in waitsAfter[c] : n \notin everLanded /\ n \notin waived[c]

\* FinishTarget (steps_work.go finishPlan): the finish ends where the needs record
\* says, by the predicate lifecycle.go judges working -> waiting by
\* (namedNeedsOpen = ~NeedsMet): waiting with a named need open, GenerationTarget
\* otherwise. The reversed witness "staleafter" reads waitsAfter, the second
\* record finishPlan read before this card; "finishunmet" drops the waiting
\* branch and lets a card given a need not landed finish with it.
FinishTarget(c) ==
  IF Broken = "finishunmet" THEN GenerationTarget(c)
  ELSE IF (IF Broken = "staleafter" THEN WaitsOpen(c) ELSE ~NeedsMet(c)) THEN "waiting"
  ELSE GenerationTarget(c)

\* Finish (refmodel Finish): the working card finishes in its member's working
\* cell and its primary goes where its needs say (FinishTarget): a finish of a
\* card whose named needs have not landed is a coordination failure
\* (needs_stop.go), taken whatever the worker reports, so it goes to waiting, not
\* review, and Rule11 holds. Every finish is one the lifecycle accepts
\* (FinishAgreesWithLifecycle).
Finish ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] = "working"
    /\ wplace[c] = "working"
    /\ members[wmember[c]] = "up"
    /\ state' = [state EXCEPT ![c] = FinishTarget(c)]
    /\ wplace' = [wplace EXCEPT ![c] = IF state'[c] = "review" THEN "finished" ELSE "withdrawn"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = [NoFlags EXCEPT !.finishing = TRUE]

\* Withdraw: working -> ready, the work card withdrawn with its attempt
\* unchanged, for every cause but fleet down (withdrawUnit and the route rest,
\* takeEnded, stagingRefused, rateLimitedReturned, capRedeal, FriendReturn,
\* TickFriendRedeal, finishPlan's return to ready).
Withdraw ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] = "working"
    /\ wplace[c] \in {"ready", "working", "finished"}
    /\ state' = [state EXCEPT ![c] = "ready"]
    /\ wplace' = [wplace EXCEPT ![c] = "withdrawn"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* FleetDown (refmodel FleetDown): the member goes down; its ready and working
\* cards are dealt to the other up members (place ready, their primaries stay
\* working), or, with no member up, withdrawn (their primaries return to
\* ready). The reversed witness "downkeeps" leaves a working card on the member
\* that went down.
FleetDown ==
  \E m \in Up :
    /\ FenceOK
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
    /\ UNCHANGED <<needs, waived, everLanded, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* FleetUp (refmodel FleetUp): the member comes up.
FleetUp ==
  \E m \in Member :
    /\ FenceOK
    /\ members[m] = "down"
    /\ members' = [members EXCEPT ![m] = "up"]
    /\ UNCHANGED <<state, needs, waived, everLanded, wplace, wmember, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* Ask (refmodel Ask): the primary in review asks one more read of a reader
\* other than its worker, distinct from the readers already standing.
Ask ==
  \E c \in Card :
    /\ FenceOK
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ ~BrokenRead(c)
    /\ Cardinality(Reader) >= 2
    /\ Cardinality(LiveReads(c)) < ReadsNeeded(c)
    /\ \E r \in FreshReader(c) :
         /\ rplace' = [rplace EXCEPT ![c][r] = "asked"]
         /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* AskAnother (refmodel AskAnother): one more reader for a primary already
\* asked, answering a broken read or reads exhausted.
AskAnother ==
  \E c \in Card :
    /\ FenceOK
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ LiveReads(c) # {}
    /\ (BrokenRead(c) \/ Cardinality(LiveReads(c)) >= ReadsNeeded(c))
    /\ \E r \in FreshReader(c) :
         /\ rplace' = [rplace EXCEPT ![c][r] = "asked"]
         /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* ReadStart (refmodel ReadStart): the reader moves its own asked read to
\* reading.
ReadStart ==
  \E r \in Reader :
    /\ FenceOK
    /\ \E c \in Card :
         /\ rplace[c][r] = "asked"
         /\ rplace' = [rplace EXCEPT ![c][r] = "reading"]
         /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* Read (refmodel Read): the reader records ok or broken.
Read ==
  \E r \in Reader :
    /\ FenceOK
    /\ \E c \in Card, v \in {"ok", "broken"} :
         /\ rplace[c][r] \in {"asked", "reading"}
         /\ rplace' = [rplace EXCEPT ![c][r] = v]
         /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* accept: review -> merging when the primary has the reads its tier needs and
\* no broken read stands at its head (steps_review.go Accept, TickAccept).
Accept ==
  \E c \in Card :
    /\ FenceOK
    /\ ~IsSentinel(c)
    /\ state[c] = "review"
    /\ \/ Acceptable(c)
       \/ /\ Broken = "shortaccept"
          /\ Cardinality(OkReaders(c)) >= ReadsNeeded(c) - 1
    /\ Broken = "acceptbroken" \/ ~BrokenRead(c)
    /\ state' = [state EXCEPT ![c] = "merging"]
    /\ RetireOutstanding(c)
    /\ IF MergeSide THEN Queue(c) ELSE UNCHANGED MergeVars
    /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* NextAttempt(c, from): rework and redo (and a brief edited in place on a card
\* in review): the next attempt is dealt at once to an up member (to working,
\* the needs rule held as at any deal), or the card goes to ready and the deal
\* cuts it later; the old work card is retired and the read cards retire. Redo
\* (from = merging) leaves the merge cell returned and the stream follows (G4);
\* rework (from = review) does not touch the merge side.
NextAttempt(c, from) ==
  /\ FenceOK
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
  /\ IF MergeSide /\ from = "merging"
     THEN Returned(c)
     ELSE UNCHANGED <<mg, sstream, scause, ranked>>
  /\ UNCHANGED <<needs, waived, everLanded, members, ci>>
  /\ pendstate' = IF Free THEN state' ELSE pendstate
  /\ pending' = pending
  /\ flags' = NoFlags

\* rework: review -> working (a member is up) or review -> ready.
Rework == \E c \in Card : ~IsSentinel(c) /\ NextAttempt(c, "review")

\* return: merging -> review (return, the tick's short-reads pass, a refused
\* landing). The read cards at the attempt stand. The card leaves merge into
\* returned and the stream's state follows (G4). The reversed witness "unland"
\* takes a landed card back too, as ShortReadsBack planned before this card
\* (the store refused it).
Return ==
  \E c \in Card :
    /\ FenceOK
    /\ \/ /\ state[c] = "merging"
          /\ state' = [state EXCEPT ![c] = "review"]
          /\ IF MergeSide THEN Returned(c) ELSE UNCHANGED MergeVars
          /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace, ci>>
       \/ Broken = "unland" /\ state[c] = "landed" /\ ~IsSentinel(c)
          /\ Move(c, "review")
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* redo: merging -> working (a member is up) or merging -> ready.
Redo == \E c \in Card : ~IsSentinel(c) /\ NextAttempt(c, "merging")

\* merge: the one-card move, merging -> landed, always a lawful move. With the
\* merge side on the card lands into merged and its stream follows (G4); with
\* it off the merge variables stay frozen (the merge side's instance merges by
\* the batch MergeGreen instead).
Merge ==
  \E c \in Card :
    /\ FenceOK
    /\ ~IsSentinel(c)
    /\ state[c] = "merging"
    /\ state' = [state EXCEPT ![c] = "landed"]
    /\ everLanded' = everLanded \cup {c}
    /\ IF MergeSide
       THEN /\ mg' = [mg EXCEPT ![c] = "merged"]
            /\ scause' = scause
            /\ Follows(StreamOf[c])
            /\ ranked' = [ranked EXCEPT ![c] = FALSE]
       ELSE UNCHANGED <<mg, sstream, scause, ranked>>
    /\ UNCHANGED <<needs, waived, members, wplace, wmember, rplace, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* merge green: the batch (the stream's queued cards, all of them here: a prefix
\* is always a batch) merges to dev: merging -> landed, into merged, the
\* stream's state follows (G4).
MergeGreen ==
  \E s \in Streams :
    /\ FenceOK
    /\ sstream[s] = "merging"
    /\ Queued(s) # {}
    /\ state' = [c \in Card |-> IF c \in Queued(s) THEN "landed" ELSE state[c]]
    /\ everLanded' = everLanded \cup Queued(s)
    /\ mg' = [c \in Card |-> IF c \in Queued(s) THEN "merged" ELSE mg[c]]
    /\ scause' = scause
    /\ Follows(s)
    /\ ranked' = [c \in Card |-> IF c \in Queued(s) THEN FALSE ELSE ranked[c]]
    /\ UNCHANGED <<needs, waived, members, wplace, wmember, rplace, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* SkipMerge (Broken "skip"): the reads card's witness, a card lands straight
\* from review (MovesOnlyByTable fails).
SkipMerge ==
  /\ Broken = "skip"
  /\ FenceOK
  /\ \E c \in Card :
      /\ state[c] = "review"
      /\ ~IsSentinel(c)
      /\ Move(c, "landed")

\* MergeStop (refmodel MergeStop): a fact on a queued card stops the stream; the
\* card goes stuck, the stream stops (conflict). The cross-stream need (D6) and
\* F4 are not modelled: a stop here is always a conflict on a card of the
\* stream itself.
MergeStop ==
  \E s \in Streams, c \in Card :
    /\ FenceOK
    /\ sstream[s] = "merging"
    /\ c \in Queued(s)
    /\ state' = state
    /\ mg' = [mg EXCEPT ![c] = "stuck"]
    /\ scause' = [scause EXCEPT ![s] = "conflict"]
    /\ sstream' = [sstream EXCEPT ![s] = "stopped"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* MergeRefused (refmodel MergeRefused): the landing's refusal of the card's
\* head, the way the card's own (a way "" is the lander's failure and stops the
\* stream, MergeStop). Back to review (paths or at the brief's bound) or ready
\* (redo); the card leaves merge into returned, the stream follows (G4).
MergeRefused ==
  \E s \in Streams, c \in Card :
    /\ FenceOK
    /\ sstream[s] = "merging"
    /\ c \in Queued(s)
    /\ \/ state' = [state EXCEPT ![c] = "review"]
       \/ state' = [state EXCEPT ![c] = "ready"]
    /\ Returned(c)
    /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* MergeRed (refmodel MergeRed): the branch red (or the queue rejected the
\* batch); the stream stops, no card moves.
MergeRed ==
  \E s \in Streams :
    /\ FenceOK
    /\ sstream[s] = "merging"
    /\ state' = state
    /\ mg' = mg
    /\ scause' = [scause EXCEPT ![s] = "red"]
    /\ sstream' = [sstream EXCEPT ![s] = "stopped"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* Resume (refmodel Resume): a stopped stream resumes; the stuck cards queue
\* again, the cause clears, the stream's state follows (G4).
Resume ==
  \E s \in Streams :
    /\ FenceOK
    /\ sstream[s] = "stopped"
    /\ state' = state
    /\ mg' = [c \in Card |-> IF c \in Stuck(s) THEN "queued" ELSE mg[c]]
    /\ scause' = [scause EXCEPT ![s] = "none"]
    /\ Follows(s)
    /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace, ranked, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* Rank (refmodel Rank): a card ranked first (the mark ranked set), refused for
\* a landed primary (G6). The score change is not modelled: Score is a constant
\* and the merge takes the whole queue, so the merge order is a choice.
Rank ==
  \E c \in Card :
    /\ FenceOK
    /\ IsOpen(state[c])
    /\ ranked' = [ranked EXCEPT ![c] = TRUE]
    /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember, rplace, mg, sstream, scause, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* release: waiting -> landed, a sentinel only, and only by the plan's release.
Release ==
  \E c \in Card :
    /\ FenceOK
    /\ IsSentinel(c)
    /\ state[c] = "waiting"
    /\ state' = [state EXCEPT ![c] = "landed"]
    /\ everLanded' = everLanded \cup {c}
    /\ IF MergeSide
       THEN /\ ranked' = [ranked EXCEPT ![c] = FALSE]
            /\ mg' = mg
            /\ scause' = scause
            /\ Follows(StreamOf[c])
       ELSE UNCHANGED <<mg, sstream, scause, ranked>>
    /\ UNCHANGED <<needs, waived, members, wplace, wmember, rplace, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = [NoFlags EXCEPT !.releasing = TRUE]

\* add: ready -> waiting as the effect of inserting a sentinel in front. The
\* reversed witness "insert" lets it run without inserting.
AddMove ==
  \E c \in Card :
    /\ FenceOK
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    \* a ready card whose work card was withdrawn is past the stop, never pulled
    \* behind it (steps_work.go Add, withdrawnCard): rule 2 (Rule2Withdrawn)
    /\ wplace[c] # "withdrawn"
    /\ state' = [state EXCEPT ![c] = "waiting"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = [NoFlags EXCEPT !.inserting = (Broken # "insert")]

\* NeedsSet: needs set, and a brief edit's DEPENDS-ON (steps_edit.go NeedsSet,
\* Brief, briefInPlace, briefDepends): a primary's needs become N, each a card
\* on the table, never the card itself, never closing a cycle. A ready card
\* that gains a need not landed moves ready -> waiting; a waiting card whose
\* new needs have all landed moves waiting -> ready (refused while its place in
\* line still blocks it); a working card, dealt or taken, takes it in place and
\* the tick then withdraws it to waiting (NeedsWithdraw, PR #96), and a taken
\* card's finish holds NeedsMet (Finish, PR #103), so no primary reaches review
\* with a need not landed; a card in review takes only needs that have landed
\* (briefDepends; rule 11). Every rewrite is held to the cycle check add makes
\* (steps_edit.go needsCycle). The reversed witnesses: "reviewneeds" lets a
\* review card take a need not landed, "needsany" moves a ready card to waiting
\* with every need landed, "cycle" skips the cycle check, "finishunmet" lets a
\* finish drop the needs guard.
NeedsSet ==
  \E rw \in Rewrites :
   LET c == rw[1]
       N == rw[2]
   IN
    /\ FenceOK
    /\ ~IsSentinel(c)
    /\ c \notin N
    /\ state[c] \in {"waiting", "ready", "working", "review"}
    /\ N # needs[c]
    /\ \A d \in N : state[d] # "off"
    /\ Broken = "cycle" \/ \A d \in N : c \notin ReachN(d)
    /\ needs' = [needs EXCEPT ![c] = N]
    /\ \/ /\ state[c] = "working"
          \* a working card, dealt or taken, takes a new need in place; the tick
          \* withdraws it (NeedsWithdraw), and a taken card's finish holds
          \* NeedsMet (Finish), so no primary reaches review with a need not
          \* landed (PR #103)
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
    /\ UNCHANGED <<waived, everLanded, members, wplace, wmember, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* NeedsWait (steps_tick.go needsWaits, PR #96): a ready primary whose named
\* needs are not all landed or waived, and whose work card is dealt nowhere,
\* waits again: ready -> waiting.
NeedsWait ==
  \E c \in Card :
    /\ FenceOK
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
    /\ FenceOK
    /\ state[c] = "working"
    /\ Broken = "withdrawmet" \/ ~NeedsMet(c)
    /\ wplace[c] \in {"ready", "working"}
    /\ state' = [state EXCEPT ![c] = IF Broken = "withdrawready" THEN "ready" ELSE "waiting"]
    /\ wplace' = [wplace EXCEPT ![c] = "none"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, rplace, mg, sstream, scause, ranked, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* drop: off the table, from an open state, never a card another open card
\* still needs (drop refuses it without its cascade; defer is drop from
\* waiting); a landed primary leaves only by the plan's remove. The primary's
\* work card leaves the fleet with it.
Drop ==
  \E c \in Card :
    /\ FenceOK
    /\ \/ IsOpen(state[c]) /\ ~Needed(c)
       \/ state[c] = "landed"
    /\ state' = [state EXCEPT ![c] = "off"]
    /\ wplace' = [wplace EXCEPT ![c] = "none"]
    /\ ClearReads(c)
    /\ IF MergeSide
       THEN /\ mg' = [mg EXCEPT ![c] = "none"]
            /\ scause' = scause
            /\ Follows(StreamOf[c])
            /\ UNCHANGED ranked
       ELSE UNCHANGED <<mg, sstream, scause, ranked>>
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' = [NoFlags EXCEPT !.removing = (state[c] = "landed")]

\* The reversed witness "comeback": a dropped card is admitted again.
Comeback ==
  /\ Broken = "comeback"
  /\ FenceOK
  /\ \E c \in Card : state[c] = "off" /\ Move(c, "waiting")

\* waive: the coordinator waives a need; a waived need counts as met. The ack
\* moves a waiting card ready in the same step when nothing else holds it
\* (steps_ack.go waive).
Waive ==
  \E c \in Card :
    /\ FenceOK
    /\ \E n \in needs[c] \ waived[c] :
         /\ waived' = [waived EXCEPT ![c] = waived[c] \cup {n}]
         /\ \/ state' = state
            \/ /\ ~IsSentinel(c)
               /\ state[c] = "waiting"
               /\ PositionClear(c)
               /\ \A x \in needs[c] : x \in everLanded \/ x \in waived'[c]
               /\ state' = [state EXCEPT ![c] = "ready"]
         /\ UNCHANGED <<needs, everLanded, members, wplace, wmember, rplace, mg, sstream, scause, ranked, ci>>
         /\ pendstate' = IF Free THEN state' ELSE pendstate
         /\ pending' = pending
         /\ flags' = NoFlags

\* LandUnmerged (Broken "landunmerged"): a card lands from review, no merge;
\* review -> landed is not a Moves row (MovesOnlyByTable fails).
LandUnmerged ==
  /\ Broken = "landunmerged"
  /\ FenceOK
  /\ \E c \in Card :
      /\ state[c] = "review"
      /\ ~IsSentinel(c)
      /\ state' = [state EXCEPT ![c] = "landed"]
      /\ everLanded' = everLanded \cup {c}
      /\ mg' = [mg EXCEPT ![c] = "merged"]
      /\ scause' = scause
      /\ Follows(StreamOf[c])
      /\ ranked' = [ranked EXCEPT ![c] = FALSE]
      /\ UNCHANGED <<needs, waived, members, wplace, wmember, rplace, ci>>
      /\ pendstate' = IF Free THEN state' ELSE pendstate
      /\ pending' = pending
      /\ flags' = NoFlags

\* ReturnLost (Broken "returnlost"): a card returned from merging is dropped,
\* its record lost (state "lost"); NoCardLostOrTwice fails.
ReturnLost ==
  /\ Broken = "returnlost"
  /\ FenceOK
  /\ \E c \in Card :
      /\ state[c] = "merging"
      /\ state' = [state EXCEPT ![c] = "lost"]
      /\ mg' = [mg EXCEPT ![c] = "returned"]
      /\ scause' = scause
      /\ Follows(StreamOf[c])
      /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace, ranked, ci>>
      /\ pendstate' = IF Free THEN state' ELSE pendstate
      /\ pending' = pending
      /\ flags' = NoFlags

\* ------------------------------------------------------------- ci, ack

\* CiRed (refmodel CiRed): a red CI observation on a placed primary records
\* red and opens the ci judgment; it moves nothing.
CiRed ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] \in States
    /\ ci' = [ci EXCEPT ![c] = "red"]
    /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember, rplace, mg, sstream, scause, ranked>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* CiGreen (refmodel CiGreen): a green CI observation on a placed primary
\* closes the ci judgment (the module keeps the open red, the only part of the
\* observation the table reads). The exhausted and stranded judgments the code
\* writes when the last judgment in review closes are the reads and merge
\* cards', not here.
CiGreen ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] \in States
    /\ ci' = [ci EXCEPT ![c] = ""]
    /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember, rplace, mg, sstream, scause, ranked>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* Ack (refmodel Ack): the coordinator answers the open ci judgment on one
\* card and closes it. The blocked and skipped judgments the code also answers,
\* and the waive of a dropped need the blocked ack names, are the reads and
\* merge cards' (the module's Waive waives a stored need).
Ack ==
  \E c \in Card :
    /\ FenceOK
    /\ ci[c] = "red"
    /\ ci' = [ci EXCEPT ![c] = ""]
    /\ UNCHANGED <<state, needs, waived, everLanded, members, wplace, wmember, rplace, mg, sstream, scause, ranked>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' = NoFlags

\* ------------------------------------------------------------- crash, repair

\* CrashStart .. CrashDrop (refmodel Crash): the process writing a step over
\* more than one table is cut before its last write, the work table. The steps
\* cut here are Start, Finish, FleetDown and Drop (the fleet tables and the
\* work table); a move, waive, a CI observation and an ack write one table, and
\* Take and FleetUp write only the fleet table, so none of them is cut. The
\* reads and merge steps (accept, rework, return, redo and the merge moves)
\* write the work table too, but they are modelled atomic here. The state the
\* crash leaves is the step's but for the work table, which is left at the
\* pre-state and held in pendstate for the repair; the fleet writes are
\* committed; the operation stays pending.
CrashStart ==
  \E c \in Card, m \in Member :
    /\ Free
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    /\ Broken = "dealunmet" \/ NeedsMet(c)
    /\ wplace[c] \in {"none", "withdrawn"}
    /\ members[m] = "up"
    /\ state' = state
    /\ pendstate' = [state EXCEPT ![c] = "working"]
    /\ wplace' = [wplace EXCEPT ![c] = "ready"]
    /\ wmember' = [wmember EXCEPT ![c] = m]
    /\ UNCHANGED <<needs, waived, everLanded, members, rplace, mg, sstream, scause, ranked, ci>>
    /\ pending' = "start"
    /\ flags' = flags

CrashFinish ==
  \E c \in Card :
    /\ Free
    /\ state[c] = "working"
    /\ wplace[c] = "working"
    /\ members[wmember[c]] = "up"
    /\ state' = state
    /\ pendstate' = [state EXCEPT ![c] = FinishTarget(c)]
    /\ wplace' = [wplace EXCEPT ![c] = IF pendstate'[c] = "review" THEN "finished" ELSE "withdrawn"]
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, rplace, mg, sstream, scause, ranked, ci>>
    /\ pending' = "finish"
    /\ flags' = flags

CrashFleetDown ==
  \E m \in Up :
    /\ Free
    /\ members' = [members EXCEPT ![m] = "down"]
    /\ \/ /\ Broken = "downkeeps"
          /\ UNCHANGED <<state, wplace, wmember>>
          /\ pendstate' = state
       \/ /\ Up \ {m} # {}
          /\ state' = state
          /\ pendstate' = state
          /\ wplace' = [c \in Card |-> IF c \in HeldOn(m) THEN "ready" ELSE wplace[c]]
          /\ \E r \in [HeldOn(m) -> Up \ {m}] :
               wmember' = [c \in Card |-> IF c \in HeldOn(m) THEN r[c] ELSE wmember[c]]
       \/ /\ Up \ {m} = {}
          /\ state' = state
          /\ pendstate' = [c \in Card |-> IF c \in HeldOn(m) THEN "ready" ELSE state[c]]
          /\ wplace' = [c \in Card |-> IF c \in HeldOn(m) THEN "withdrawn" ELSE wplace[c]]
          /\ wmember' = wmember
    /\ UNCHANGED <<needs, waived, everLanded, rplace, mg, sstream, scause, ranked, ci>>
    /\ pending' = "fleetdown"
    /\ flags' = flags

CrashDrop ==
  \E c \in Card :
    /\ Free
    /\ \/ IsOpen(state[c]) /\ ~Needed(c)
       \/ state[c] = "landed"
    /\ state' = state
    /\ pendstate' = [state EXCEPT ![c] = "off"]
    /\ wplace' = [wplace EXCEPT ![c] = "none"]
    /\ RetireOutstanding(c)
    /\ IF MergeSide
       THEN /\ mg' = [mg EXCEPT ![c] = "none"]
            /\ scause' = scause
            /\ Follows(StreamOf[c])
            /\ UNCHANGED ranked
       ELSE UNCHANGED <<mg, sstream, scause, ranked>>
    /\ UNCHANGED <<needs, waived, everLanded, members, wmember, ci>>
    /\ pending' = "drop"
    /\ flags' = [NoFlags EXCEPT !.removing = (state[c] = "landed")]

\* Crash: a crash on any of the steps that write more than one table.
Crash ==
  \/ CrashStart \/ CrashFinish \/ CrashFleetDown \/ CrashDrop

\* Repair (refmodel Repair): the deferred work-table write is applied from the
\* operation record and the operation releases.
Repair ==
  /\ pending \in Verbs
  /\ state' = pendstate
  /\ UNCHANGED <<needs, waived, everLanded, members, wplace, wmember, rplace, mg, sstream, scause, ranked, ci>>
  /\ pendstate' = state'
  /\ pending' = ""
  /\ flags' = flags

\* The ordinary steps, without the outside events. The instances that leave the
\* outside events off run Spec; its state space is the module's own, the ci,
\* pending and pendstate variables frozen (so it does not grow). The crash
\* instance runs CrashSpec, which adds the outside events and the repair.
LegacyNext ==
  \/ Resolve \/ Start \/ Take \/ Finish \/ Withdraw \/ FleetDown \/ FleetUp
  \/ Ask \/ AskAnother \/ ReadStart \/ Read
  \/ Accept \/ Rework \/ Return \/ Redo \/ Merge \/ Release \/ AddMove
  \/ NeedsSet \/ NeedsWait \/ NeedsWithdraw
  \/ Drop \/ Comeback \/ Waive
  \/ MergeSide /\ (MergeGreen \/ MergeStop \/ MergeRefused \/ MergeRed \/ Resume \/ Rank)
  \/ SkipMerge \/ LandUnmerged \/ ReturnLost

\* The outside events and the repair are only in the crash instance's spec, so
\* every MCSprintTables case (which runs Spec) keeps its budget.
\* Brief: one bounded generation change, with the explicit keep-head escape.
Brief ==
  \E c \in Flash :
    /\ Free
    /\ state[c] = "working"
    /\ briefGeneration[c] = 0
    /\ briefGeneration' = [briefGeneration EXCEPT ![c] = 1]
    /\ keptHead' = keptHead
    /\ operationalEdits' = {}
    /\ state' = state
    /\ wplace' = wplace
    /\ rplace' = rplace
    /\ pendstate' = state'
    /\ UNCHANGED <<attemptGeneration, headGeneration, needs, waived, everLanded, flags, members, wmember, mg, sstream, scause, ranked, pending, ci>>

\* An operational note or scope-widening edit (PATHS, NEW or SHARED entries
\* added, or START (read) entries added; steps_edit.go operationalBriefOnly,
\* scopeWidenedOnly) refines the contract without invalidating the in-flight or
\* reviewed head: on a working card briefGeneration stands, and on a review card
\* state, rplace and headGeneration stand. The one-step event marker makes the
\* edit observable without recording irrelevant history; the mutants reproduce
\* the old blanket working bounce and review redeal.
OperationalEdit ==
  \E c \in Flash :
    /\ Free
    /\ c \notin operationalEdits
    /\ \/ /\ state[c] = "working"
          /\ briefGeneration[c] = attemptGeneration[c]
          /\ briefGeneration[c] = 0
          /\ operationalEdits' = operationalEdits \cup {c}
          /\ briefGeneration' = IF Broken \in {"operationalbounce", "scopebounce"}
               THEN [briefGeneration EXCEPT ![c] = 1] ELSE briefGeneration
          /\ state' = state
          /\ wplace' = wplace
          /\ rplace' = rplace
          /\ pendstate' = state'
       \/ /\ (Cardinality(Card) = 1 \/ Broken = "scoperedeal")
          /\ state[c] = "review"
          /\ operationalEdits' = operationalEdits \cup {c}
          /\ briefGeneration' = briefGeneration
          /\ IF Broken = "scoperedeal"
             THEN /\ state' = [state EXCEPT ![c] = "ready"]
                  /\ wplace' = [wplace EXCEPT ![c] = "none"]
                  /\ ClearReads(c)
             ELSE /\ state' = state
                  /\ wplace' = wplace
                  /\ rplace' = rplace
          /\ pendstate' = state'
    /\ UNCHANGED <<attemptGeneration, headGeneration, keptHead, needs,
         waived, everLanded, flags, members, wmember, mg, sstream, scause,
         ranked, pending, ci>>

ScopeWiden == OperationalEdit

OperationalEditPreservesHead ==
  [][\A c \in operationalEdits' \ operationalEdits :
       briefGeneration'[c] = briefGeneration[c]]_allTableVars

ScopeWidenPreservesHead ==
  [][\A c \in operationalEdits' \ operationalEdits :
       briefGeneration'[c] = briefGeneration[c]]_allTableVars

ScopeWidenKeepsReview ==
  [][\A c \in operationalEdits' \ operationalEdits :
       state[c] = "review" => state'[c] = "review" /\ rplace'[c] = rplace[c]]_allTableVars

\* Attempt/head provenance follows the same lifecycle step; metadata never
\* changes a legacy action's enabledness.
GenerationStep ==
  /\ attemptGeneration' = [c \in Card |-> IF state'[c] = "working" /\ state[c] # "working" THEN briefGeneration[c] ELSE attemptGeneration[c]]
  /\ headGeneration' = [c \in Card |-> IF state'[c] = "review" /\ state[c] = "working" THEN attemptGeneration[c] ELSE headGeneration[c]]
  /\ operationalEdits' = {}
  /\ UNCHANGED <<briefGeneration, keptHead>>

\* A parked card is fenced against lifecycle/provenance edits until release.
ParkFence == parkState = "held" =>
  /\ state'[ParkCard] = state[ParkCard]
  /\ needs'[ParkCard] = needs[ParkCard]
  /\ briefGeneration'[ParkCard] = briefGeneration[ParkCard]
  /\ headGeneration'[ParkCard] = headGeneration[ParkCard]
  /\ attemptGeneration'[ParkCard] = attemptGeneration[ParkCard]
  /\ rplace'[ParkCard] = rplace[ParkCard]
  /\ wplace'[ParkCard] = wplace[ParkCard]

\* Bounded composition: parking is enabled for the one-primary full-Spec
\* instances; historical three-card instances retain their original action set.
\* A ready primary whose work card was withdrawn is held in place
\* (seat_answers.go TickDecisionDefaults, withdrawnCard): it stays ready, the
\* hold fences it from the deal (ParkFence, dealPlan IsHeld), so it is never
\* left waiting on no need (Rule2Withdrawn), and a sentinel insert never pulls
\* it (AddMove). Any other ready primary, and one in review, is parked in
\* waiting (lifecycle.go decisionPark), so an insert never meets a held ready
\* card (HeldReadyIsWithdrawn). Enabled in the one- and two-primary instances.
InPlace(c) == state[c] = "ready" /\ (wplace[c] = "withdrawn" \/ Broken = "parkinplaceall")
DecisionParkStep ==
  /\ Cardinality(Card) <= 2
  /\ Free /\ parkState = "none" /\ state[ParkCard] \in {"ready", "review"}
  /\ parkBound
  /\ state' = IF InPlace(ParkCard) /\ Broken # "parkmoves"
               THEN state ELSE [state EXCEPT ![ParkCard] = "waiting"]
  /\ pendstate' = state'
  /\ flags' = [NoFlags EXCEPT !.decisionPark = TRUE]
  /\ parkSaved' = state[ParkCard] /\ parkState' = "held"
  /\ parkCause' = (Broken # "parkwithoutcause") /\ parkEvent' = "park"
  /\ parkHead' = headGeneration[ParkCard] /\ parkReads' = rplace[ParkCard]
  /\ UNCHANGED <<briefGeneration, attemptGeneration, headGeneration, keptHead, operationalEdits,
       needs, waived, everLanded, members, wplace, wmember, rplace, mg, sstream,
       scause, ranked, pending, ci, parkBound>>
DecisionReleaseStep ==
  /\ Free /\ parkState = "held" /\ parkCause
  /\ (parkSaved = "review" => NeedsMet(ParkCard))
  /\ (parkSaved = "ready" => NeedsMet(ParkCard))
  /\ state' = [state EXCEPT ![ParkCard] = IF Broken = "releaseprestate" THEN "ready" ELSE parkSaved]
  /\ pendstate' = state'
  /\ flags' = [NoFlags EXCEPT !.decisionRelease = TRUE]
  /\ parkState' = "released" /\ parkEvent' = "release"
  /\ UNCHANGED <<briefGeneration, attemptGeneration, headGeneration, keptHead, operationalEdits,
       needs, waived, everLanded, members, wplace, wmember, rplace, mg, sstream,
       scause, ranked, pending, ci, parkBound, parkSaved, parkCause, parkHead, parkReads>>
\* ReworkHeldInPlace: a rework of a held in-place primary answers the hold (this
\* card v17-rework-clears-the-server-default-hold; deadlock j25715): the primary
\* is dealt again (state -> working, its work card dealt) and the hold is removed
\* (parkState' = "released"); like Start, the deal stamps the attempt generation
\* (GenerationStep). Enabled in the instances DecisionParkStep is. The
\* reversed witness "reworkkeepshold" leaves parkState "held", so HeldNeverWorks
\* fails: the primary works while still held. Only Rework's deal-at-once branch
\* is modelled: the code's no-member-up branch (reworkAnswersHold with
\* e.Move == nil, lifecycle.go), which leaves the primary in place in ready, is
\* not modelled (nor is ClearReads), so the model admits less than the code
\* there.
ReworkHeldInPlace ==
  /\ Cardinality(Card) <= 2
  /\ Free /\ parkState = "held" /\ InPlace(ParkCard)
  /\ NeedsMet(ParkCard)
  /\ \E m \in Up :
        /\ state' = [state EXCEPT ![ParkCard] = "working"]
        /\ wplace' = [wplace EXCEPT ![ParkCard] = "ready"]
        /\ wmember' = [wmember EXCEPT ![ParkCard] = m]
  /\ pendstate' = state'
  /\ flags' = NoFlags
  /\ parkState' = IF Broken = "reworkkeepshold" THEN "held" ELSE "released"
  /\ parkEvent' = "rework"
  /\ GenerationStep
  /\ UNCHANGED <<needs, waived, everLanded, members, rplace, mg, sstream,
       scause, ranked, pending, ci, parkBound, parkSaved, parkCause, parkHead, parkReads>>

HeldNeverWorks == parkState = "held" => state[ParkCard] # "working"
\* Follow: the needs record's views follow every step that is not a refused finish.
\* waitsAfter[c] (steps_edit.go NeedsSet, briefDepends, the brief path; the
\* rule-11 marker) is, for a card working before and after the step, its old
\* marker plus the needs the step gained, intersected with the new needs: held
\* inside the needs record at every write (WaitsAfterWithinNeeds); a step that
\* ends the attempt spends it. The reversed witness "staleafter" leaves it as
\* it was (no intersection). The refusal counts reset with the attempt: a card
\* that changes state, member or is dealt anew starts a clean count.
Fresh(c) == state'[c] # state[c] \/ wmember'[c] # wmember[c] \/ (wplace'[c] = "ready" /\ wplace[c] # "ready")

Follow ==
  /\ waitsAfter' = [c \in Card |->
       IF ~(state[c] = "working" /\ state'[c] = "working") THEN {}
       ELSE IF Broken = "staleafter" THEN waitsAfter[c] \cup (needs'[c] \ needs[c])
       ELSE (waitsAfter[c] \cup (needs'[c] \ needs[c])) \cap needs'[c]]
  /\ finishTries' = [c \in Card |-> IF Fresh(c) THEN 0 ELSE finishTries[c]]
  /\ finishRefusals' = [c \in Card |-> IF Fresh(c) THEN 0 ELSE finishRefusals[c]]
  /\ finishJudged' = [c \in Card |-> IF Fresh(c) THEN 0 ELSE finishJudged[c]]

\* FinishRefused (steps_work.go Finish, FieldFinishRefusals, NFinishRefused): a
\* finish of a working card the lifecycle refuses changes no card but is counted,
\* and the refusal past FinishBound raises one judgment for the attempt, never a
\* second. The reversed witness "finishloop" neither counts nor judges, so the
\* same finish is refused without end. Modelled in the one-primary instances
\* (as DecisionParkStep is) to keep the three-card instances at their size.
FinishRefused ==
  /\ Cardinality(Card) = 1
  /\ \E c \in Card :
    /\ FenceOK
    /\ state[c] = "working"
    /\ wplace[c] = "working"
    /\ members[wmember[c]] = "up"
    /\ finishTries[c] <= FinishBound
    /\ finishTries' = [finishTries EXCEPT ![c] = @ + 1]
    /\ finishRefusals' = IF Broken = "finishloop" THEN finishRefusals ELSE [finishRefusals EXCEPT ![c] = @ + 1]
    /\ finishJudged' = IF Broken = "finishloop" \/ finishRefusals'[c] <= FinishBound \/ finishJudged[c] >= 1
                       THEN finishJudged ELSE [finishJudged EXCEPT ![c] = @ + 1]
    /\ UNCHANGED <<waitsAfter, vars, parkingVars>>

Next == \/ (((LegacyNext /\ GenerationStep) \/ Brief \/ OperationalEdit) /\ ParkFence /\ UNCHANGED parkingVars /\ Follow)
        \/ ((DecisionParkStep \/ DecisionReleaseStep \/ ReworkHeldInPlace) /\ Follow)
        \/ FinishRefused

NoOldBriefHeadInReview ==
  \A c \in Card : state[c] = "review" => (headGeneration[c] = briefGeneration[c] \/ keptHead[c])

CrashNext ==
  \/ Next
  \/ (CiRed \/ CiGreen \/ Ack \/ Crash \/ Repair) /\ GenerationStep /\ ParkFence /\ UNCHANGED parkingVars /\ Follow

Spec == Init /\ [][Next]_allTableVars
CrashSpec == Init /\ [][CrashNext]_allTableVars

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

\* WaitsAfterWithinNeeds (steps_edit.go NeedsSet, briefDepends; check.go): the
\* rule-11 marker of the needs a card gained while working is held inside its
\* needs record at every write, so no second record can name a need the first
\* does not. Reversed witness "staleafter".
WaitsAfterWithinNeeds == \A c \in Card : waitsAfter[c] \subseteq needs[c]

\* FinishAgreesWithLifecycle (steps_work.go finishPlan; lifecycle.go
\* WorkingToWaitingOnlyOnNeeds and Rule11): a finish ends in waiting only when
\* the card's named needs have not landed, and in review or ready only when they
\* have, so the lifecycle accepts every finish the plan builds and none is
\* refused for the needs. Reversed witnesses "staleafter" and "finishunmet".
FinishAgreesWithLifecycle ==
  [][flags'.finishing =>
     \A c \in Card :
       /\ state[c] = "working" /\ state'[c] = "waiting" => ~NeedsMet(c)
       /\ state[c] = "working" /\ state'[c] \in {"review", "ready"} => NeedsMet(c)]_allTableVars

\* FinishRefusalsBounded (FinishRefusalBound, NFinishRefused): a finish refused past
\* the bound has raised its one judgment, and never two for one attempt. Reversed
\* witness "finishloop".
FinishRefusalsBounded ==
  \A c \in Card :
    /\ finishTries[c] > FinishBound => finishJudged[c] = 1
    /\ finishJudged[c] <= 1

\* NoCardLostOrTwice: a card that leaves the table (a drop or a remove) never
\* comes back; its record is kept, unplaced, no card is admitted twice, and no
\* card's record is lost (a card returned from merging is never dropped).
NoCardLostOrTwice ==
  [][\A c \in Card :
    /\ state[c] = "off" => state'[c] = "off"
    /\ state'[c] # "lost"]_vars

\* MovesOnlyByTable: every change of cell is a lawful move, a drop from an
\* open state, or a remove of a landed card.
MovesOnlyByTable ==
  [][\A c \in Card :
    \/ state'[c] = state[c]
    \/ Legal(state[c], state'[c])
    \/ /\ c = ParkCard /\ flags'.decisionPark /\ parkBound /\ parkCause'
       /\ (state[c] = "review" \/ (state[c] = "ready" /\ wplace[c] # "withdrawn"))
       /\ state'[c] = "waiting"
    \/ /\ c = ParkCard /\ flags'.decisionRelease /\ parkCause
       /\ state[c] \in {"waiting", "ready"} /\ state'[c] = parkSaved
    \/ /\ state'[c] = "off"
       /\ (IsOpen(state[c]) \/ flags'.removing)]_vars

\* ReadyToWaitingOnlyByInsertOrNeeds: a primary moves ready -> waiting only as
\* the effect of inserting a sentinel in front of it, or with a named need
\* that has not landed (lifecycle.go needsOpen).
ReadyToWaitingOnlyByInsertOrNeeds ==
  [][\A c \in Card :
    state[c] = "ready" /\ state'[c] = "waiting" => flags'.inserting \/ ~((NeedsMet(c))')
      \/ (c = ParkCard /\ flags'.decisionPark /\ parkBound /\ parkCause' /\ wplace[c] # "withdrawn")]_allTableVars

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

\* StreamAfter (G4): a stream's state is what its merge cells and cause say it
\* is.
StreamAfter == \A s \in Streams : sstream[s] = StreamState(s, state, mg, scause)

\* RankRefusesLanded (G6): a landed primary is never ranked. Rank refuses a
\* landed primary (its guard is IsOpen); the mark ranked clears when the card
\* lands (merge or release), so the invariant holds.
RankRefusesLanded == \A c \in Card : state[c] = "landed" => ~ranked[c]

\* These invariants observe actual lifecycle transitions inside Spec, including
\* the same-card head and live-read provenance; no disconnected projection.
ParkingHasCause == parkState = "held" => parkBound /\ parkCause
  /\ state[ParkCard] \in {"ready", "waiting"}

\* HeldReadyIsWithdrawn: a held ready primary is one whose work card was
\* withdrawn, the one kind a sentinel insert never pulls (AddMove,
\* steps_work.go Add withdrawnCard), so the lifecycle's hold guard never
\* refuses an insert for it.
HeldReadyIsWithdrawn == parkState = "held" /\ state[ParkCard] = "ready" => wplace[ParkCard] = "withdrawn"

\* Rule2Withdrawn (check.go rule 2, judged when no operation is in flight): a
\* primary whose work card was withdrawn is ready, or waiting on a named need
\* that has not landed, or waiting with its needs met and resolvable: the
\* tick's resolve (Resolve, steps_work.go resolveAfter) moves it to ready
\* before the tick's check reads it (a waive or a landing that met its needs
\* in another step). A held card is resolved by no step (ParkFence; resolve
\* skips IsHeld), so a hold that parked it in waiting is a rule 2 break no
\* step repairs: the server-default hold of a ready primary holds it in place
\* (DecisionParkStep), and the reversed witness parkmoves breaks this.
Resolvable(c) == PositionClear(c) /\ NeedsMet(c) /\ ~(c = ParkCard /\ parkState = "held")
Rule2Withdrawn == Free =>
  \A c \in Card \ Sentinels :
    wplace[c] = "withdrawn" /\ IsOpen(state[c]) =>
      \/ state[c] = "ready"
      \/ state[c] = "waiting" /\ (~NeedsMet(c) \/ Resolvable(c))
ParkingRestoresPrestate == [][flags'.decisionRelease => state'[ParkCard] = parkSaved]_allTableVars
ParkingPreservesWork == parkState = "held" => headGeneration[ParkCard] = parkHead /\ rplace[ParkCard] = parkReads
=============================================================================
