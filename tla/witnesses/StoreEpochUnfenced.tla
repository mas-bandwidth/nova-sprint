------------------------- MODULE StoreEpochUnfenced -------------------------
(***************************************************************************)
(* REVERSED WITNESS (e). FAULT: ops other than clear are not checked       *)
(* against the store's epoch, so an op planned against epoch 0 that        *)
(* arrives after the clear lands in epoch 1 (e1 puts row r2, which the     *)
(* clear carried forward). TLC must report OpsInTheirEpoch violated.       *)
(* Everything else is Store.tla unchanged.                                 *)
(***************************************************************************)
(***************************************************************************)
(* Layer 1 of nova-sprint v2: the store (docs/LAYER-1-STORE.md).           *)
(*                                                                         *)
(* Each operation is one atomic step. In SQL that step is one transaction  *)
(* from LOCK TABLE sprint.epoch to COMMIT at READ COMMITTED: writers are   *)
(* serialized by the lock, so the conditions are read in the same state    *)
(* the writes change. The op's epoch must equal the store's epoch; then    *)
(* its conditions (Expect) are checked; then either every write lands and  *)
(* an accepted event is appended, or nothing changes and a refused event   *)
(* is appended. Clear is an op with no writes: it closes the epoch, opens  *)
(* the next, carries the rows forward (state down) and leaves the cards    *)
(* behind. An op id is remembered forever (applied); a second Apply of a   *)
(* remembered id, and a Replay, return the stored receipt and change       *)
(* nothing. A transport failure is the caller not learning the outcome     *)
(* (phase "unknown"): the request was lost before the store saw it, or the *)
(* reply was lost after the store applied it; the caller then calls Replay *)
(* or Apply again.                                                         *)
(*                                                                         *)
(* Small instance: 3 cards, 2 rows of width 1, 14 fixed ops. Callers issue *)
(* their ops in program order (Pred): a setup caller first, then six       *)
(* callers concurrently. Transport failures (at most MaxLoss each) only on *)
(* the take a1 and the clear k1 (Lossy). Streams, policy, the seq fence,   *)
(* and the card and row fields no condition reads (attempt, branch, head,  *)
(* pr, reason, score, kind, session, flaps) are left out: they add columns,*)
(* not behaviour. There is no heartbeat: rows have no seen field.          *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANT MaxLoss   \* transport failures allowed per lossy op (bounds the model)

Cards      == {"c1", "c2", "c3"}
Rows       == {"r1", "r2"}
CardStates == {"waiting", "ready", "working", "review", "merging", "landed", "left"}
RowStates  == {"up", "down", "blocked"}
Absent     == "absent"   \* the state of a card or row that does not exist
NoHolder   == "none"

(* Conditions and writes are uniform records so TLC can compare them. In a *)
(* condition, g is the gen for CardGen and the width bound W for RowUnder. *)
Cnd(k, c, r, s, g) == [k |-> k, c |-> c, r |-> r, s |-> s, g |-> g]
CardGen(c, g)     == Cnd("cardGen",     c,   "-", "-", g)
CardState(c, s)   == Cnd("cardState",   c,   "-", s,   0)
HolderIs(c, r)    == Cnd("holderIs",    c,   r,   "-", 0)
NeedsLanded(c)    == Cnd("needsLanded", c,   "-", "-", 0)
CardAbsent(c)     == Cnd("cardAbsent",  c,   "-", "-", 0)
RowIs(r, s)       == Cnd("rowIs",       "-", r,   s,   0)
RowUnder(r, w)    == Cnd("rowUnder",    "-", r,   "-", w)

Wr(k, c, r, s, n, w) == [k |-> k, c |-> c, r |-> r, s |-> s, n |-> n, w |-> w]
AddCard(c, n)     == Wr("addCard",     c,   "-", "-", n,  0)
SetState(c, s)    == Wr("setState",    c,   "-", s,   {}, 0)
SetHolder(c, r)   == Wr("setHolder",   c,   r,   "-", {}, 0)
PutRow(r, w)      == Wr("putRow",      "-", r,   "-", {}, w)
SetRowState(r, s) == Wr("setRowState", "-", r,   s,   {}, 0)

Op(e, x, w)  == [kind |-> "op",    epoch |-> e, expect |-> x,  writes |-> w]
ClearOp(e)   == [kind |-> "clear", epoch |-> e, expect |-> {}, writes |-> <<>>]

(* s1..s5 set up epoch 0: three cards (c3 needs c1), two rows, up.         *)
(* a1/b1 race to take c1 (same expectation, two rows). a1/w1 race to deal  *)
(* two cards to r1, width 1. a2 lands c1 if a1 holds it. b2 reassigns c1   *)
(* to r1 if r1 has room. n1 readies c3 once its need c1 is landed. e1 is   *)
(* an epoch-0 put row that may arrive after the clear. k1 clears epoch 0;  *)
(* k2 is planned against epoch 1.                                          *)
Ops == [
  s1 |-> Op(0, {CardAbsent("c1")}, <<AddCard("c1", {})>>),
  s2 |-> Op(0, {CardAbsent("c2")}, <<AddCard("c2", {})>>),
  s3 |-> Op(0, {CardAbsent("c3")}, <<AddCard("c3", {"c1"})>>),
  s4 |-> Op(0, {}, <<PutRow("r1", 1), SetRowState("r1", "up")>>),
  s5 |-> Op(0, {}, <<PutRow("r2", 1), SetRowState("r2", "up")>>),
  a1 |-> Op(0, {CardState("c1", "waiting"), CardGen("c1", 0), HolderIs("c1", NoHolder),
                RowIs("r1", "up"), RowUnder("r1", 1)},
             <<SetState("c1", "working"), SetHolder("c1", "r1")>>),
  a2 |-> Op(0, {CardState("c1", "working"), HolderIs("c1", "r1")},
             <<SetState("c1", "landed"), SetHolder("c1", NoHolder)>>),
  b1 |-> Op(0, {CardState("c1", "waiting"), CardGen("c1", 0), HolderIs("c1", NoHolder),
                RowIs("r2", "up"), RowUnder("r2", 1)},
             <<SetState("c1", "working"), SetHolder("c1", "r2")>>),
  b2 |-> Op(0, {CardState("c1", "working"), RowIs("r1", "up"), RowUnder("r1", 1)},
             <<SetHolder("c1", "r1")>>),
  w1 |-> Op(0, {CardState("c2", "waiting"), CardGen("c2", 0), HolderIs("c2", NoHolder),
                RowIs("r1", "up"), RowUnder("r1", 1)},
             <<SetState("c2", "working"), SetHolder("c2", "r1")>>),
  n1 |-> Op(0, {CardState("c3", "waiting"), NeedsLanded("c3")},
             <<SetState("c3", "ready")>>),
  e1 |-> Op(0, {}, <<PutRow("r2", 2)>>),
  k1 |-> ClearOp(0),
  k2 |-> Op(1, {CardAbsent("c2")}, <<AddCard("c2", {})>>)
]
OpIds == DOMAIN Ops
Setup == {"s1", "s2", "s3", "s4", "s5"}
Lossy == {"a1", "k1"}   \* ops whose transport can fail: one race op, the clear

(* Program order: an op is asked for only once these ops are done.         *)
Pred == [
  s1 |-> {}, s2 |-> {"s1"}, s3 |-> {"s1", "s2"}, s4 |-> {"s1", "s2", "s3"},
  s5 |-> {"s1", "s2", "s3", "s4"},
  a1 |-> Setup, a2 |-> Setup \cup {"a1"},
  b1 |-> Setup, b2 |-> Setup \cup {"b1"},
  w1 |-> Setup, n1 |-> Setup, e1 |-> Setup,
  k1 |-> Setup, k2 |-> Setup \cup {"k1"}
]

NoCard == [state |-> Absent, gen |-> 0, holder |-> NoHolder, needs |-> {}]
NoRow  == [state |-> Absent, width |-> 0]
Empty  == [epoch |-> 0, cards |-> [c \in Cards |-> NoCard], rows |-> [r \in Rows |-> NoRow]]

Holders(st, r) == {c \in Cards : st.cards[c].holder = r}

Cond(st, x) ==
  CASE x.k = "cardGen"     -> st.cards[x.c].state # Absent /\ st.cards[x.c].gen = x.g
    [] x.k = "cardState"   -> st.cards[x.c].state = x.s
    [] x.k = "holderIs"    -> st.cards[x.c].state # Absent /\ st.cards[x.c].holder = x.r
    [] x.k = "needsLanded" -> st.cards[x.c].state # Absent
                              /\ \A n \in st.cards[x.c].needs : st.cards[n].state = "landed"
    [] x.k = "cardAbsent"  -> st.cards[x.c].state = Absent
    [] x.k = "rowIs"       -> st.rows[x.r].state = x.s
    [] x.k = "rowUnder"    -> st.rows[x.r].state # Absent
                              /\ Cardinality(Holders(st, x.r)) < x.g

Holds(st, conds) == \A x \in conds : Cond(st, x)

(* The write rules: a card is added only if absent, with needs that name   *)
(* other cards of this epoch; a set write needs its target; a state is in  *)
(* the closed set; a holder is a row of this epoch (the foreign key).      *)
Legal(st, w) ==
  CASE w.k = "addCard"     -> st.cards[w.c].state = Absent /\ w.c \notin w.n
                              /\ \A x \in w.n : st.cards[x].state # Absent
    [] w.k = "setState"    -> st.cards[w.c].state # Absent /\ w.s \in CardStates
    [] w.k = "setHolder"   -> st.cards[w.c].state # Absent
                              /\ (w.r = NoHolder \/ st.rows[w.r].state # Absent)
    [] w.k = "putRow"      -> TRUE
    [] w.k = "setRowState" -> st.rows[w.r].state # Absent /\ w.s \in RowStates

(* Every card write raises gen by one. A put row inserts a row as down     *)
(* (the design leaves the insert state open; down is the safe choice) or   *)
(* overwrites its width.                                                   *)
Do(st, w) ==
  CASE w.k = "addCard"     -> [st EXCEPT !.cards[w.c] =
                                 [state |-> "waiting", gen |-> 0, holder |-> NoHolder, needs |-> w.n]]
    [] w.k = "setState"    -> [st EXCEPT !.cards[w.c].state = w.s, !.cards[w.c].gen = @ + 1]
    [] w.k = "setHolder"   -> [st EXCEPT !.cards[w.c].holder = w.r, !.cards[w.c].gen = @ + 1]
    [] w.k = "putRow"      -> [st EXCEPT !.rows[w.r] =
                                 IF @.state = Absent THEN [state |-> "down", width |-> w.w]
                                 ELSE [@ EXCEPT !.width = w.w]]
    [] w.k = "setRowState" -> [st EXCEPT !.rows[w.r].state = w.s]

(* Run the writes in order; a write that breaks a rule refuses the op.     *)
RECURSIVE Run(_, _)
Run(st, ws) ==
  IF ws = <<>> THEN [ok |-> TRUE, st |-> st]
  ELSE IF Legal(st, Head(ws)) THEN Run(Do(st, Head(ws)), Tail(ws))
  ELSE [ok |-> FALSE, st |-> st]

(* Clear: the next epoch, the rows carried forward as down, no cards.      *)
ClearState(st) ==
  [epoch |-> st.epoch + 1,
   cards |-> [c \in Cards |-> NoCard],
   rows  |-> [r \in Rows |-> IF st.rows[r].state = Absent THEN NoRow
                             ELSE [st.rows[r] EXCEPT !.state = "down"]]]

(* What the store does with op o in state st: accept with a new state, or  *)
(* refuse. The epoch check comes first.                                    *)
\* FAULT: only the clear is fenced by epoch.
Outcome(st, o) ==
  IF Ops[o].kind = "clear" /\ Ops[o].epoch # st.epoch THEN [acc |-> FALSE, st |-> st]
  ELSE IF Ops[o].kind = "clear" THEN [acc |-> TRUE, st |-> ClearState(st)]
  ELSE LET run == Run(st, Ops[o].writes)
       IN IF Holds(st, Ops[o].expect) /\ run.ok THEN [acc |-> TRUE, st |-> run.st]
          ELSE [acc |-> FALSE, st |-> st]

(* Rebuild applies an accepted event's writes (a clear the same way the    *)
(* store applies it); it never rechecks the conditions.                    *)
Effect(st, o) == IF Ops[o].kind = "clear" THEN ClearState(st) ELSE Run(st, Ops[o].writes).st

NoReceipt == [id |-> "none", accepted |-> FALSE, epoch |-> 0, before |-> Empty, after |-> Empty]

VARIABLES
  epoch,    \* the store's epoch (sprint.epoch.n)
  cards,    \* card id -> [state, gen, holder, needs] in the current epoch
  rows,     \* row name -> [state, width] in the current epoch
  events,   \* sequence of [id, accepted, epoch, before, after]
  applied,  \* op ids the store has seen (the unique op column of events)
  phase,    \* op id -> "idle" | "unknown" | "done": what the caller knows
  receipt,  \* op id -> the receipt the caller holds (NoReceipt until done)
  lost      \* op id -> transport failures so far (bounded by MaxLoss)

vars == <<epoch, cards, rows, events, applied, phase, receipt, lost>>
Current == [epoch |-> epoch, cards |-> cards, rows |-> rows]
(* The state the log says held just before event i: empty before the       *)
(* first, otherwise the after-state of the one before it.                   *)
Before(i) == IF i = 1 THEN Empty ELSE events[i - 1].after

StoredEvent(o) == events[CHOOSE i \in DOMAIN events : events[i].id = o]

Init ==
  /\ epoch   = Empty.epoch
  /\ cards   = Empty.cards
  /\ rows    = Empty.rows
  /\ events  = <<>>
  /\ applied = {}
  /\ phase   = [o \in OpIds |-> "idle"]
  /\ receipt = [o \in OpIds |-> NoReceipt]
  /\ lost    = [o \in OpIds |-> 0]

Ready(o) == \A p \in Pred[o] : phase[p] = "done"
CanLose(o) == o \in Lossy /\ lost[o] < MaxLoss

(* The caller learns the outcome, or (while failures remain) does not.     *)
Outcomes(o) == IF CanLose(o) THEN {"done", "unknown"} ELSE {"done"}

(* Replay: the stored receipt comes back; nothing in the store changes.    *)
ReplayEffect(o) ==
  /\ phase'   = [phase EXCEPT ![o] = "done"]
  /\ receipt' = [receipt EXCEPT ![o] = StoredEvent(o)]
  /\ UNCHANGED <<epoch, cards, rows, events, applied, lost>>

(* One transaction, lock to commit: epoch, conditions, writes, event.      *)
Execute(o) ==
  LET pre  == Current
      out  == Outcome(pre, o)
      post == out.st
      ev   == [id |-> o, accepted |-> out.acc, epoch |-> pre.epoch, before |-> pre, after |-> post]
  IN /\ epoch'   = post.epoch
     /\ cards'   = post.cards
     /\ rows'    = post.rows
     /\ events'  = Append(events, ev)
     /\ applied' = applied \cup {o}
     /\ \E res \in Outcomes(o) :
          /\ phase'   = [phase EXCEPT ![o] = res]
          /\ receipt' = IF res = "done" THEN [receipt EXCEPT ![o] = ev] ELSE receipt
          /\ lost'    = IF res = "unknown" THEN [lost EXCEPT ![o] = @ + 1] ELSE lost

(* Apply of an id already in applied is the same as Replay.                *)
Apply(o) ==
  /\ phase[o] \in {"idle", "unknown"}
  /\ Ready(o)
  /\ IF o \in applied THEN ReplayEffect(o) ELSE Execute(o)

(* The request is lost before the store sees it: nothing changes, and the  *)
(* caller does not know that.                                              *)
LoseRequest(o) ==
  /\ phase[o] \in {"idle", "unknown"}
  /\ Ready(o)
  /\ CanLose(o)
  /\ phase' = [phase EXCEPT ![o] = "unknown"]
  /\ lost'  = [lost EXCEPT ![o] = @ + 1]
  /\ UNCHANGED <<epoch, cards, rows, events, applied, receipt>>

(* Replay after unknown. When the id is not in applied, Replay says "not   *)
(* found" and changes nothing; the caller then calls Apply again, which    *)
(* stays enabled in phase "unknown", so that branch is not a separate step. *)
Replay(o) ==
  /\ phase[o] = "unknown"
  /\ o \in applied
  /\ ReplayEffect(o)

Terminating == (\A o \in OpIds : phase[o] = "done") /\ UNCHANGED vars

Next ==
  \/ \E o \in OpIds : Apply(o) \/ LoseRequest(o) \/ Replay(o)
  \/ Terminating

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Invariants                                                              *)

StoreState ==
  [epoch : 0..Cardinality(OpIds),
   cards : [Cards -> [state : CardStates \cup {Absent},
                      gen : 0..2 * Cardinality(OpIds),
                      holder : Rows \cup {NoHolder},
                      needs : SUBSET Cards]],
   rows : [Rows -> [state : RowStates \cup {Absent}, width : 0..2]]]

TypeOK ==
  /\ Current \in StoreState
  /\ applied \subseteq OpIds
  /\ phase \in [OpIds -> {"idle", "unknown", "done"}]
  /\ lost \in [OpIds -> 0..MaxLoss]

(* Every card has exactly one state and at most one holder, and the holder *)
(* is a row of this epoch. Structural here (state is one value from the    *)
(* closed set, holder is one value or none); stated anyway.                *)
SingleStateSingleHolder ==
  \A c \in Cards :
    /\ cards[c].state \in CardStates \cup {Absent}
    /\ cards[c].holder \in Rows \cup {NoHolder}
    /\ cards[c].holder # NoHolder => rows[cards[c].holder].state # Absent
    /\ cards[c].state = Absent => cards[c] = NoCard

(* No row holds more cards than its width.                                 *)
WidthKept ==
  \A r \in Rows : rows[r].state # Absent => Cardinality(Holders(Current, r)) <= rows[r].width

(* No op id appears twice in the event log.                                *)
IdempotentById ==
  \A i, j \in DOMAIN events : i # j => events[i].id # events[j].id

(* The set of op ids the store remembers is exactly the ids in the log.    *)
AppliedIsLog == applied = {events[i].id : i \in DOMAIN events}

(* Folding the accepted events from empty (a clear the same way the store  *)
(* applies it) gives the current state. Checked as a chain, which is the   *)
(* fold by induction and linear in the log: the first event starts from    *)
(* empty, each starts where the one before ended, each accepted event's    *)
(* after is its writes applied to its before, and the last one ends at the *)
(* current state. Each event records the epoch it ran in.                  *)
LogMatchesState ==
  /\ (IF events = <<>> THEN Empty ELSE events[Len(events)].after) = Current
  /\ \A i \in DOMAIN events :
       /\ events[i].before = Before(i)
       /\ events[i].after = IF events[i].accepted THEN Effect(Before(i), events[i].id)
                            ELSE Before(i)
       /\ events[i].epoch = Before(i).epoch

(* Every accepted op, clear included, was planned against the epoch the    *)
(* store was at when it applied; so after a clear, a delayed op planned    *)
(* against the old epoch is refused.                                       *)
OpsInTheirEpoch ==
  \A i \in DOMAIN events :
    events[i].accepted => Ops[events[i].id].epoch = Before(i).epoch

(* An accepted op's conditions held against the state immediately before  *)
(* its own writes (where the event before it ended): no lost update.      *)
ConditionsAtomic ==
  \A i \in DOMAIN events :
    events[i].accepted => Holds(Before(i), Ops[events[i].id].expect)

(* A refused op changed nothing: its before and after are equal and are   *)
(* the state immediately before it.                                        *)
NoChangeOnRefusal ==
  \A i \in DOMAIN events :
    ~events[i].accepted =>
      /\ events[i].before = events[i].after
      /\ events[i].before = Before(i)

(* A caller that says it knows the outcome holds the stored receipt:      *)
(* unknown is never reported as unchanged or as a different answer.       *)
ReceiptMatches ==
  \A o \in OpIds :
    phase[o] = "done" => (o \in applied /\ receipt[o] = StoredEvent(o))

-----------------------------------------------------------------------------
(* Reachability: these are expected to be VIOLATED (reach/*.cfg). Each    *)
(* violation shows that the passing run above is not vacuous.             *)

AcceptedIds == {events[i].id : i \in {j \in DOMAIN events : events[j].accepted}}

NeverTaken      == AcceptedIds \cap {"a1", "b1"} = {}
NeverRecovered  == \A o \in OpIds : ~(lost[o] > 0 /\ phase[o] = "done")
NeverStaleEpochRefused ==
  \A i \in DOMAIN events : ~(~events[i].accepted /\ Ops[events[i].id].epoch # events[i].epoch)
NeverNeedsReady == "n1" \notin AcceptedIds
NeverNewEpochOp == "k2" \notin AcceptedIds
NeverWidthFull  == \A r \in Rows : rows[r].state = Absent \/ Cardinality(Holders(Current, r)) < rows[r].width

=============================================================================
