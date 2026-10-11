-------------------------------- MODULE SprintTables ----------------------------
\* The card lifecycle of nova-sprint (docs/SPEC-SPRINT.md section 3;
\* internal/sprint/lifecycle.go): a primary's six states and the legal moves
\* between them, in one table. This is the table lifecycle.go names: "the same
\* states and the same moves"; compare the two row by row.
\*
\* A primary's state is the work-table column it is placed in. The six states
\* are waiting, ready, working, review, merging, landed; landed is final. Off
\* the table (a drop) is not a state: a primary that stops any other way than
\* landing leaves the table with its record, outcome and reason kept, unplaced.
\*
\* Moves, one per verb (lifecycle.go Moves):
\*   resolve      waiting -> ready    (everything it needs has landed)
\*   deal         ready   -> working
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
\* The guards are the code's (Lawful, lifecycle.go): each move is a row of
\* Moves (Legal); the flags a plan carries (releasing, inserting, removing)
\* gate the one move each names; and a card leaves waiting for ready only when
\* every need has landed or was waived (unmet), with nothing in line before it
\* (PositionWaits). A sentinel is admitted waiting, moves only to landed, and
\* is landed only by release.
\*
\* Invariants: TypeOK, NoNeedCycle (needs are acyclic), OffIsFinal (a card
\* that leaves the table never comes back), and the action properties
\* MovesOnlyByTable (every change of cell is a Moves row, a drop from an open
\* state, or a remove of a landed card), ReadyToWaitingOnlyByInsert, LandedIsFinal
\* and NeedsLandFirst. OffIsFinal proves only the "never returns from off"
\* reading: admission is outside this module (InitOf), so the refmodel's
\* NoCardLostOrTwice (a work or read card cut a second time, refmodel/actions.go)
\* cannot be expressed here; that rule is deal's (tla-sprint-tables-fleet-take-and-finish).
\*
\* Reversed witnesses (Broken):
\*   "insert"   the add move runs ready -> waiting without inserting.
\*   "needless" resolve drops the needs check, so a card with an unmet need
\*              still moves waiting -> ready.
\*
\* Not modelled here (other cards): deal, take, finish and the fleet
\* (tla-sprint-tables-fleet-take-and-finish), reads and accept, merge and
\* return, crash and repair.
EXTENDS Naturals, FiniteSets

CONSTANTS
  Card,        \* the card ids
  Sentinels,   \* the sentinel ids, a subset of Card
  NeedsOf,     \* NeedsOf[c] is c's stored needs, a subset of Card
  Score,       \* Score[c] is c's place in line, a distinct number
  InitOf,      \* InitOf[c] is c's cell at the start: waiting or ready
  Broken       \* "none", or the one reversed witness to break

States == {"waiting", "ready", "working", "review", "merging", "landed"}
Cells == States \cup {"off"}

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

VARIABLES state, waived, flags

vars == <<state, waived, flags>>

TypeOK ==
  /\ state \in [Card -> Cells]
  /\ waived \in [Card -> SUBSET Card]
  /\ flags \in Flags

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

\* The effect of one move: c to its new cell; the step's flags are chosen freely.
Move(c, to) ==
  /\ state' = [state EXCEPT ![c] = to]
  /\ UNCHANGED <<waived>>
  /\ flags' \in Flags

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

\* deal: ready -> working.
Deal == \E c \in Card : state[c] = "ready" /\ Move(c, "working")

\* finish: working -> review.
Finish == \E c \in Card : state[c] = "working" /\ Move(c, "review")

\* fleet down: working -> ready.
FleetDown == \E c \in Card : state[c] = "working" /\ Move(c, "ready")

\* accept: review -> merging.
Accept == \E c \in Card : state[c] = "review" /\ Move(c, "merging")

\* rework: review -> working (a member is up) or review -> ready (none is up).
Rework ==
  \E c \in Card :
    /\ state[c] = "review"
    /\ \/ Move(c, "working") \/ Move(c, "ready")

\* return: merging -> review.
Return == \E c \in Card : state[c] = "merging" /\ Move(c, "review")

\* redo: merging -> working (a member is up) or merging -> ready (none is up).
Redo ==
  \E c \in Card :
    /\ state[c] = "merging"
    /\ \/ Move(c, "working") \/ Move(c, "ready")

\* merge: merging -> landed.
Merge == \E c \in Card : state[c] = "merging" /\ Move(c, "landed")

\* release: waiting -> landed, a sentinel only, and only by the plan's release.
Release ==
  \E c \in Card :
    /\ IsSentinel(c)
    /\ state[c] = "waiting"
    /\ flags.releasing
    /\ Move(c, "landed")

\* add: ready -> waiting, only as the effect of inserting a sentinel in front.
\* The reversed witness "insert" lets it run without inserting.
AddMove ==
  \E c \in Card :
    /\ state[c] = "ready"
    /\ \/ Broken = "insert" \/ flags.inserting
    /\ Move(c, "waiting")

\* drop: off the table, from an open state; a landed primary leaves only by the
\* plan's remove.
Drop ==
  \E c \in Card :
    /\ state[c] \in States
    /\ IsOpen(state[c]) \/ flags.removing
    /\ Move(c, "off")

\* waive: the coordinator waives a need; a waived need counts as met.
Waive ==
  \E c \in Card :
    \E n \in NeedsOf[c] :
      /\ waived' = [waived EXCEPT ![c] = waived[c] \cup {n}]
      /\ UNCHANGED <<state>>
      /\ flags' \in Flags

Next ==
  \/ Resolve \/ Deal \/ Finish \/ FleetDown \/ Accept \/ Rework \/ Return
  \/ Redo \/ Merge \/ Release \/ AddMove \/ Drop \/ Waive

Spec == Init /\ [][Next]_vars

\* ------------------------------------------------------------- the rules

\* NoNeedCycle: no card's needs, followed transitively, come back to it. The
\* closure is bounded by the number of cards: the shortest cycle through a card
\* has at most Cardinality(Card) edges, so reaching a card from itself within
\* that bound is exactly a cycle. An unbounded recursive closure cannot find a
\* cycle (on a cyclic NeedsOf TLC recurses without end), so the depth is bounded.
Reach(c, k) ==
  LET RECURSIVE Desc(_, _)
      Desc(x, i) == IF i = 0 THEN {} ELSE NeedsOf[x] \cup UNION {Desc(n, i-1) : n \in NeedsOf[x]}
  IN Desc(c, k)

NoNeedCycle == \A c \in Card : c \notin Reach(c, Cardinality(Card))

\* OffIsFinal: a card that leaves the table (a drop or a remove) never comes
\* back; its record is kept, unplaced. This is only the "never returns from
\* off" reading of the refmodel's NoCardLostOrTwice (a work or read card cut a
\* second time, refmodel/actions.go): admission is outside this module (InitOf),
\* so no card can be admitted twice here, and cut-a-second-time is deal's rule.
OffIsFinal ==
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

=============================================================================
