-------------------------------- MODULE SprintTables ----------------------------
\* The card lifecycle of nova-sprint (docs/SPEC-SPRINT.md section 3;
\* internal/sprint/lifecycle.go) and the fleet side of the work table
\* (internal/sprint/refmodel/actions.go Start, Take, Finish, FleetDown,
\* FleetUp). A primary's six states and the legal moves between them, in one
\* table, plus the fleet: each member is up or down, and a working primary's
\* work card sits on one member's row in a place none, ready (dealt, not
\* taken), working (taken), finished or withdrawn.
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
\*   add          ready   -> waiting   (a sentinel inserted in front)
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
\* The outside events (refmodel actions.go CiRed, CiGreen, Ack) follow the
\* code: a red CI observation on a placed primary opens the ci judgment (the
\* "red" cell); a green observation and an ack close it. The module keeps only
\* the open red: the green apart from none is read only by the accept-holds
\* logic, which is the merge card's. The other ackable judgments (blocked,
\* skipped) and the reads and merge judgments the exhaust writes are the reads
\* and merge cards', not here.
\*
\* Crash and repair (refmodel actions.go Crash, Repair; SPEC-SPRINT.md section
\* 10) follow the code: the process writing a step over more than one table is
\* cut before its last write, the work table. The work table is state (and, in
\* the code, the whole primary record); the fleet tables are members, wplace
\* and wmember. A crash commits the fleet writes of the step, leaves the work
\* table at its pre-state, and holds the deferred work-table write in pendstate
\* with the operation pending. Repair applies the deferred write and releases
\* the operation. Only the steps over more than one table are cut: Start,
\* Finish, FleetDown and Drop; a move, waive, a CI observation and an ack write
\* one table, and Take and FleetUp write only the fleet table, so none of them
\* is cut. While an operation is pending, the fence (D1: Free) holds: no
\* action but repair runs.
\*
\* Invariants: TypeOK, NoNeedCycle, NoCardLostOrTwice, and the action
\* properties MovesOnlyByTable (every change of cell is a Moves row, a drop
\* from an open state, or a remove of a landed card), ReadyToWaitingOnlyByInsert,
\* LandedIsFinal, NeedsLandFirst and WorkingOnAnUpMember (a card in a member's
\* working cell sits on an up member).
\*
\* Reversed witnesses (Broken):
\*   "insert"    the add move runs ready -> waiting without inserting.
\*   "needless"  resolve drops the needs check, so a card with an unmet need
\*               still moves waiting -> ready.
\*   "downkeeps" fleet down leaves a working card on the member that went down.
\*   "halfstep"  the fence is dropped, so a crash leaves a step half-written
\*               (its fleet writes committed, its work-table write pending) and
\*               other actions may act on the half-written table; a repair then
\*               applies the stale deferred write and fails NoCardLostOrTwice.
\*
\* Not modelled here (other cards): reads and accept, and merge and return.
EXTENDS Naturals, FiniteSets

CONSTANTS
  Card,        \* the card ids
  Sentinels,   \* the sentinel ids, a subset of Card
  NeedsOf,     \* NeedsOf[c] is c's stored needs, a subset of Card
  Score,       \* Score[c] is c's place in line, a distinct number
  InitOf,      \* InitOf[c] is c's cell at the start: waiting or ready
  Member,      \* the fleet's member ids
  InitUp,      \* the members up at the start
  Broken       \* "none", or the one reversed witness to break

States == {"waiting", "ready", "working", "review", "merging", "landed"}
Cells == States \cup {"off"}

\* The work card's place on a member's row (refmodel state.go FleetCells).
WPlaces == {"none", "ready", "working", "finished", "withdrawn"}

\* A member's status (refmodel state.go: Up, Down).
MStatus == {"up", "down"}

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
Flags == [releasing : BOOLEAN, inserting : BOOLEAN, removing : BOOLEAN]

\* A red CI observation on a primary: none or an open red (refmodel Primary.CI
\* and the ci judgment; a green observation and an ack both close the red, and
\* the accept-holds logic that reads the green apart from none is the merge
\* card's, not here).
CIVals == {"", "red"}

\* The verbs a crash can cut: the steps over more than one table, cut before
\* their last write, the work table.
Verbs == {"start", "finish", "fleetdown", "drop"}

VARIABLES state, waived, flags, members, wplace, wmember, pending, ci,
          pendstate

vars == <<state, waived, flags, members, wplace, wmember, pending, ci,
          pendstate>>

TypeOK ==
  /\ state \in [Card -> Cells]
  /\ waived \in [Card -> SUBSET Card]
  /\ flags \in Flags
  /\ members \in [Member -> MStatus]
  /\ wplace \in [Card -> WPlaces]
  /\ wmember \in [Card -> Member]
  /\ pending \in Verbs \cup {""}
  /\ ci \in [Card -> CIVals]
  /\ pendstate \in [Card -> Cells]

\* Free: no operation is pending (the D1 fence). Every action that is not a
\* crash or a repair runs only while it holds; the reversed witness "halfstep"
\* drops it, so a step cut by a crash is left half-written and other actions
\* may act on the half-written table.
Free == pending = ""

\* FenceOK: the step may run (the fence is free, or the witness drops it).
FenceOK == \/ Broken = "halfstep" \/ Free

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
  /\ pending = ""
  /\ ci = [c \in Card |-> ""]
  /\ pendstate = InitOf

\* The effect of one move: c to its new cell; the fleet is untouched and the
\* step's flags are chosen freely. The deferred write tracks the work table.
Move(c, to) ==
  /\ state' = [state EXCEPT ![c] = to]
  /\ UNCHANGED <<waived, members, wplace, wmember, ci>>
  /\ pendstate' = IF Free THEN state' ELSE pendstate
  /\ pending' = pending
  /\ flags' \in Flags

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

\* Start (refmodel Start, actions.go line 299): the deal. ready -> working; the
\* work card is dealt to an up member's row (place ready). A withdrawn card is
\* dealt again (a redeal); a card never dealt is cut now. The deal takes any up
\* member (the round-robin NextMember is a choice, not a safety rule).
Start ==
  \E c \in Card, m \in Member :
    /\ FenceOK
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    /\ wplace[c] \in {"none", "withdrawn"}
    /\ members[m] = "up"
    /\ state' = [state EXCEPT ![c] = "working"]
    /\ wplace' = [wplace EXCEPT ![c] = "ready"]
    /\ wmember' = [wmember EXCEPT ![c] = m]
    /\ UNCHANGED <<waived, members, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* Take (refmodel Take, actions.go line 351): the worker on an up member moves
\* a dealt (ready) work card to working. The primary's state does not change.
\* The take names the live generation (F2): a stale take is a refusal, so the
\* model has no step for it and no separate invariant to state.
Take ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] = "working"
    /\ wplace[c] = "ready"
    /\ members[wmember[c]] = "up"
    /\ wplace' = [wplace EXCEPT ![c] = "working"]
    /\ UNCHANGED <<state, waived, members, wmember, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* Finish (refmodel Finish, actions.go line 375): working -> review; the work
\* card finishes in its member's working cell. Accepted only for the live
\* generation in the member's working cell (D3): a stale finish is a refusal.
Finish ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] = "working"
    /\ wplace[c] = "working"
    /\ members[wmember[c]] = "up"
    /\ state' = [state EXCEPT ![c] = "review"]
    /\ wplace' = [wplace EXCEPT ![c] = "finished"]
    /\ UNCHANGED <<waived, members, wmember, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* FleetDown (refmodel FleetDown, actions.go line 1070): the member goes down;
\* its ready and working cards are dealt to the other up members (place ready,
\* their primaries stay working), or, with no member up, withdrawn (their
\* primaries return to ready). The reversed witness "downkeeps" leaves a
\* working card on the member that went down.
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
    /\ UNCHANGED <<waived, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* FleetUp (refmodel FleetUp, actions.go line 1121): the member comes up. The
\* levelling of the ready queues (T4, the Level action) is not modelled.
FleetUp ==
  \E m \in Member :
    /\ FenceOK
    /\ members[m] = "down"
    /\ members' = [members EXCEPT ![m] = "up"]
    /\ UNCHANGED <<state, waived, wplace, wmember, ci>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* accept: review -> merging.
Accept == \E c \in Card : FenceOK /\ state[c] = "review" /\ Move(c, "merging")

\* rework: review -> working (a member is up) or review -> ready (none is up).
Rework ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] = "review"
    /\ \/ Move(c, "working") \/ Move(c, "ready")

\* return: merging -> review.
Return == \E c \in Card : FenceOK /\ state[c] = "merging" /\ Move(c, "review")

\* redo: merging -> working (a member is up) or merging -> ready (none is up).
Redo ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] = "merging"
    /\ \/ Move(c, "working") \/ Move(c, "ready")

\* merge: merging -> landed.
Merge == \E c \in Card : FenceOK /\ state[c] = "merging" /\ Move(c, "landed")

\* release: waiting -> landed, a sentinel only, and only by the plan's release.
Release ==
  \E c \in Card :
    /\ FenceOK
    /\ IsSentinel(c)
    /\ state[c] = "waiting"
    /\ flags.releasing
    /\ Move(c, "landed")

\* add: ready -> waiting, only as the effect of inserting a sentinel in front.
\* The reversed witness "insert" lets it run without inserting.
AddMove ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] = "ready"
    /\ \/ Broken = "insert" \/ flags.inserting
    /\ Move(c, "waiting")

\* drop: off the table, from an open state; a landed primary leaves only by the
\* plan's remove. The primary's work card leaves the fleet with it.
Drop ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] \in States
    /\ IsOpen(state[c]) \/ flags.removing
    /\ state' = [state EXCEPT ![c] = "off"]
    /\ wplace' = [wplace EXCEPT ![c] = "none"]
    /\ UNCHANGED <<waived, members, wmember, ci>>
    /\ pendstate' = IF Free THEN state' ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* waive: the coordinator waives a need; a waived need counts as met.
Waive ==
  \E c \in Card :
    \E n \in NeedsOf[c] :
      /\ FenceOK
      /\ waived' = [waived EXCEPT ![c] = waived[c] \cup {n}]
      /\ UNCHANGED <<state, members, wplace, wmember, ci>>
      /\ pendstate' = IF Free THEN state ELSE pendstate
      /\ pending' = pending
      /\ flags' \in Flags

\* ------------------------------------------------------------- ci, ack

\* CiRed (refmodel CiRed, actions.go line 1396): a red CI observation on a
\* placed primary records red and opens the ci judgment; it moves nothing.
CiRed ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] \in States
    /\ ci' = [ci EXCEPT ![c] = "red"]
    /\ UNCHANGED <<state, waived, members, wplace, wmember>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* CiGreen (refmodel CiGreen, actions.go line 1413): a green CI observation on
\* a placed primary closes the ci judgment (the module keeps the open red, the
\* only part of the observation the table reads). The exhausted and stranded
\* judgments the code writes when the last judgment in review closes are the
\* reads and merge cards', not here.
CiGreen ==
  \E c \in Card :
    /\ FenceOK
    /\ state[c] \in States
    /\ ci' = [ci EXCEPT ![c] = ""]
    /\ UNCHANGED <<state, waived, members, wplace, wmember>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* Ack (refmodel Ack, actions.go line 1443): the coordinator answers the open
\* ci judgment on one card and closes it. The blocked and skipped judgments the
\* code also answers, and the waive of a dropped need the blocked ack names,
\* are the reads and merge cards' (the module's Waive waives a stored need).
Ack ==
  \E c \in Card :
    /\ FenceOK
    /\ ci[c] = "red"
    /\ ci' = [ci EXCEPT ![c] = ""]
    /\ UNCHANGED <<state, waived, members, wplace, wmember>>
    /\ pendstate' = IF Free THEN state ELSE pendstate
    /\ pending' = pending
    /\ flags' \in Flags

\* ------------------------------------------------------------- crash, repair

\* CrashStart .. CrashDrop (refmodel Crash, actions.go line 1498): the process
\* writing a step over more than one table is cut before its last write, the
\* work table. Only Start, Finish, FleetDown and Drop write more than one table
\* (the fleet tables and the work table); a move, waive, a CI observation and
\* an ack write one table, so they are not cut. The state the crash leaves is
\* the step's but for the work table, which is left at the pre-state and held
\* in pendstate for the repair; the fleet writes are committed; the operation
\* stays pending.
CrashStart ==
  \E c \in Card, m \in Member :
    /\ Free
    /\ ~IsSentinel(c)
    /\ state[c] = "ready"
    /\ wplace[c] \in {"none", "withdrawn"}
    /\ members[m] = "up"
    /\ state' = state
    /\ pendstate' = [state EXCEPT ![c] = "working"]
    /\ wplace' = [wplace EXCEPT ![c] = "ready"]
    /\ wmember' = [wmember EXCEPT ![c] = m]
    /\ UNCHANGED <<waived, members, ci>>
    /\ pending' = "start"
    /\ flags' = flags

CrashFinish ==
  \E c \in Card :
    /\ Free
    /\ state[c] = "working"
    /\ wplace[c] = "working"
    /\ members[wmember[c]] = "up"
    /\ state' = state
    /\ pendstate' = [state EXCEPT ![c] = "review"]
    /\ wplace' = [wplace EXCEPT ![c] = "finished"]
    /\ UNCHANGED <<waived, members, wmember, ci>>
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
    /\ UNCHANGED <<waived, ci>>
    /\ pending' = "fleetdown"
    /\ flags' = flags

CrashDrop ==
  \E c \in Card :
    /\ Free
    /\ state[c] \in States
    /\ IsOpen(state[c]) \/ flags.removing
    /\ state' = state
    /\ pendstate' = [state EXCEPT ![c] = "off"]
    /\ wplace' = [wplace EXCEPT ![c] = "none"]
    /\ UNCHANGED <<waived, members, wmember, ci>>
    /\ pending' = "drop"
    /\ flags' = flags

\* Crash: a crash on any of the steps that write more than one table.
Crash ==
  \/ CrashStart \/ CrashFinish \/ CrashFleetDown \/ CrashDrop

\* Repair (refmodel Repair, actions.go line 1513): the deferred work-table
\* write is applied from the operation record and the operation releases.
Repair ==
  /\ pending \in Verbs
  /\ state' = pendstate
  /\ UNCHANGED <<waived, members, wplace, wmember, ci>>
  /\ pendstate' = state'
  /\ pending' = ""
  /\ flags' = flags

Next ==
  \/ Resolve \/ Start \/ Take \/ Finish \/ FleetDown \/ FleetUp
  \/ Accept \/ Rework \/ Return \/ Redo \/ Merge \/ Release \/ AddMove
  \/ Drop \/ Waive \/ CiRed \/ CiGreen \/ Ack
  \/ Crash \/ Repair

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

\* ReadyToWaitingOnlyByInsert: a primary moves ready -> waiting only as the
\* effect of inserting a sentinel in front of it.
ReadyToWaitingOnlyByInsert ==
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

=============================================================================
