-------------------------- MODULE StoreStaleCheck --------------------------
(***************************************************************************)
(* REVERSED WITNESS (c). FAULT: Apply is split into Begin (read a snapshot *)
(* of the state) and Commit (check the conditions against that snapshot,   *)
(* then write to the live state), so another op's writes can land between  *)
(* them: a lost update. This is a transaction that reads without locking   *)
(* at READ COMMITTED. TLC must report ConditionsAtomic violated. Adds the  *)
(* variable snap and the phase "inflight"; otherwise Store.tla unchanged.  *)
(***************************************************************************)
(***************************************************************************)
(* Layer 1 of nova-sprint v2: the store (docs/LAYER-1-STORE.md).           *)
(*                                                                         *)
(* Each operation is one atomic step: its conditions (Expect) are checked  *)
(* against the state as it is in that step, and either every write lands   *)
(* and an accepted event is appended, or nothing changes and a refused     *)
(* event is appended. An op id is remembered forever (applied); a second   *)
(* Apply of a remembered id, and a Replay, return the stored receipt and   *)
(* change nothing. A transport failure is modelled as the caller not       *)
(* learning the outcome (phase "unknown"), either because the request was  *)
(* lost before the store saw it or because the reply was lost after the    *)
(* store applied it; the caller then calls Replay, or Apply again.         *)
(*                                                                         *)
(* Small instance: 2 cards, 2 rows, 7 fixed ops; transport failures (at   *)
(* most MaxLoss each) only on the racing ops o4 and o5 (Lossy), so TLC    *)
(* finishes in minutes. Epochs, streams, policy and card fields other than *)
(* state, gen and holder are left out: they add rows, not new behaviour.   *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANT MaxLoss   \* transport failures allowed per op (bounds the model)

Cards      == {"c1", "c2"}
Rows       == {"r1", "r2"}
CardStates == {"waiting", "ready", "working", "review", "merging", "landed", "left"}
RowStates  == {"up", "down", "blocked"}
Absent     == "absent"   \* a card or row that does not exist (yet)
NoHolder   == "none"

(* Conditions and writes are uniform records so TLC can compare them.     *)
CardIs(c, s, g)   == [k |-> "cardIs",     c |-> c,   r |-> "-", s |-> s,   g |-> g]
HolderIs(c, r)    == [k |-> "holderIs",   c |-> c,   r |-> r,   s |-> "-", g |-> 0]
RowIs(r, s)       == [k |-> "rowIs",      c |-> "-", r |-> r,   s |-> s,   g |-> 0]
CardAbsent(c)     == [k |-> "cardAbsent", c |-> c,   r |-> "-", s |-> "-", g |-> 0]

AddCard(c, s)     == [k |-> "addCard",     c |-> c,   r |-> "-", s |-> s]
SetState(c, s)    == [k |-> "setState",    c |-> c,   r |-> "-", s |-> s]
SetHolder(c, r)   == [k |-> "setHolder",   c |-> c,   r |-> r,   s |-> "-"]
PutRow(r, s)      == [k |-> "putRow",      c |-> "-", r |-> r,   s |-> s]
SetRowState(r, s) == [k |-> "setRowState", c |-> "-", r |-> r,   s |-> s]

(* The ops the callers ask for. o4 and o5 race on the same expectation    *)
(* (only one may be accepted); o6 depends on o4 or o5 and on row r2       *)
(* existing (a foreign-key refusal otherwise); o7 touches a card and a    *)
(* row in one op and is refused unless row r1 is up.                      *)
Ops == [
  o1 |-> [expect |-> {CardAbsent("c1")},
          writes |-> <<AddCard("c1", "waiting")>>],
  o2 |-> [expect |-> {},
          writes |-> <<PutRow("r1", "up")>>],
  o3 |-> [expect |-> {},
          writes |-> <<PutRow("r2", "up")>>],
  o4 |-> [expect |-> {CardIs("c1", "waiting", 0), HolderIs("c1", NoHolder), RowIs("r1", "up")},
          writes |-> <<SetState("c1", "working"), SetHolder("c1", "r1")>>],
  o5 |-> [expect |-> {CardIs("c1", "waiting", 0), HolderIs("c1", NoHolder), RowIs("r2", "up")},
          writes |-> <<SetState("c1", "working"), SetHolder("c1", "r2")>>],
  o6 |-> [expect |-> {CardIs("c1", "working", 1)},
          writes |-> <<SetHolder("c1", "r2")>>],
  o7 |-> [expect |-> {CardAbsent("c2"), RowIs("r1", "up")},
          writes |-> <<AddCard("c2", "ready"), SetRowState("r1", "down")>>]
]
OpIds == DOMAIN Ops
Lossy == {"o4", "o5"}  \* ops whose transport can fail: the race, kept small

NoCard  == [state |-> Absent, gen |-> 0, holder |-> NoHolder]
Empty   == [cards |-> [c \in Cards |-> NoCard], rows |-> [r \in Rows |-> Absent]]

Cond(st, x) ==
  CASE x.k = "cardIs"     -> st.cards[x.c].state = x.s /\ st.cards[x.c].gen = x.g
    [] x.k = "holderIs"   -> st.cards[x.c].state # Absent /\ st.cards[x.c].holder = x.r
    [] x.k = "rowIs"      -> st.rows[x.r] = x.s
    [] x.k = "cardAbsent" -> st.cards[x.c].state = Absent

Holds(st, conds) == \A x \in conds : Cond(st, x)

(* What the schema refuses: primary key on add, a state outside the closed *)
(* set (CHECK), an update of a missing card or row, a holder that is not a *)
(* row (foreign key).                                                     *)
Legal(st, w) ==
  CASE w.k = "addCard"     -> st.cards[w.c].state = Absent /\ w.s \in CardStates
    [] w.k = "setState"    -> st.cards[w.c].state # Absent /\ w.s \in CardStates
    [] w.k = "setHolder"   -> st.cards[w.c].state # Absent
                              /\ (w.r = NoHolder \/ st.rows[w.r] # Absent)
    [] w.k = "putRow"      -> w.s \in RowStates
    [] w.k = "setRowState" -> st.rows[w.r] # Absent /\ w.s \in RowStates

Do(st, w) ==
  CASE w.k = "addCard"     -> [st EXCEPT !.cards[w.c] = [state |-> w.s, gen |-> 0, holder |-> NoHolder]]
    [] w.k = "setState"    -> [st EXCEPT !.cards[w.c].state = w.s, !.cards[w.c].gen = @ + 1]
    [] w.k = "setHolder"   -> [st EXCEPT !.cards[w.c].holder = w.r]
    [] w.k = "putRow"      -> [st EXCEPT !.rows[w.r] = w.s]
    [] w.k = "setRowState" -> [st EXCEPT !.rows[w.r] = w.s]

(* Run the writes in order; any illegal write refuses the whole op.        *)
RECURSIVE Run(_, _)
Run(st, ws) ==
  IF ws = <<>> THEN [ok |-> TRUE, st |-> st]
  ELSE IF Legal(st, Head(ws)) THEN Run(Do(st, Head(ws)), Tail(ws))
  ELSE [ok |-> FALSE, st |-> st]

(* Rebuild a state by folding the accepted events from empty.              *)
RECURSIVE FoldFrom(_, _)
FoldFrom(st, es) ==
  IF es = <<>> THEN st
  ELSE FoldFrom(IF Head(es).accepted THEN Run(st, Ops[Head(es).id].writes).st ELSE st,
                Tail(es))
Fold(es) == FoldFrom(Empty, es)

NoReceipt == [id |-> "none", accepted |-> FALSE, before |-> Empty, after |-> Empty]

VARIABLES
  cards,    \* card id -> [state, gen, holder]
  rows,     \* row name -> state, or Absent
  events,   \* sequence of [id, accepted, before, after]
  applied,  \* op ids the store has seen (the unique op column of events)
  phase,    \* op id -> "idle" | "unknown" | "done": what the caller knows
  receipt,  \* op id -> the receipt the caller holds (NoReceipt until done)
  lost,     \* op id -> transport failures so far (bounded by MaxLoss)
  snap      \* op id -> the state its transaction read at Begin (FAULT)

vars == <<cards, rows, events, applied, phase, receipt, lost, snap>>
Current == [cards |-> cards, rows |-> rows]
Prefix(i) == SubSeq(events, 1, i)

StoredEvent(o) == events[CHOOSE i \in DOMAIN events : events[i].id = o]

Init ==
  /\ cards   = Empty.cards
  /\ rows    = Empty.rows
  /\ events  = <<>>
  /\ applied = {}
  /\ phase   = [o \in OpIds |-> "idle"]
  /\ receipt = [o \in OpIds |-> NoReceipt]
  /\ lost    = [o \in OpIds |-> 0]
  /\ snap    = [o \in OpIds |-> Empty]

(* The caller learns the outcome, or (while failures remain) does not.     *)
Outcomes(o) == IF (o \in Lossy /\ lost[o] < MaxLoss) THEN {"done", "unknown"} ELSE {"done"}

(* Replay: the stored receipt comes back; nothing in the store changes.    *)
ReplayEffect(o) ==
  /\ phase'   = [phase EXCEPT ![o] = "done"]
  /\ receipt' = [receipt EXCEPT ![o] = StoredEvent(o)]
  /\ UNCHANGED <<cards, rows, events, applied, lost, snap>>

(* FAULT: the conditions are checked against snap[o], read at Begin; the   *)
(* writes (and the schema's own key checks) run against the live state.    *)
Execute(o) ==
  LET pre == Current
      run == Run(pre, Ops[o].writes)
      acc == Holds(snap[o], Ops[o].expect) /\ run.ok
      post == IF acc THEN run.st ELSE pre
      ev == [id |-> o, accepted |-> acc, before |-> pre, after |-> post]
  IN /\ cards'   = post.cards
     /\ rows'    = post.rows
     /\ events'  = Append(events, ev)
     /\ applied' = applied \cup {o}
     /\ \E out \in Outcomes(o) :
          /\ phase'   = [phase EXCEPT ![o] = out]
          /\ receipt' = IF out = "done" THEN [receipt EXCEPT ![o] = ev] ELSE receipt
          /\ lost'    = IF out = "unknown" THEN [lost EXCEPT ![o] = @ + 1] ELSE lost
     /\ UNCHANGED snap

(* FAULT: Apply is two steps. Begin reads the snapshot.                    *)
Begin(o) ==
  /\ phase[o] \in {"idle", "unknown"}
  /\ phase' = [phase EXCEPT ![o] = "inflight"]
  /\ snap'  = [snap EXCEPT ![o] = Current]
  /\ UNCHANGED <<cards, rows, events, applied, receipt, lost>>

(* Commit: an id already in applied is the same as Replay.                 *)
Apply(o) ==
  /\ phase[o] = "inflight"
  /\ IF o \in applied THEN ReplayEffect(o) ELSE Execute(o)

(* The request is lost before the store sees it: nothing changes, and the  *)
(* caller does not know that.                                              *)
LoseRequest(o) ==
  /\ phase[o] \in {"idle", "unknown"}
  /\ (o \in Lossy /\ lost[o] < MaxLoss)
  /\ phase' = [phase EXCEPT ![o] = "unknown"]
  /\ lost'  = [lost EXCEPT ![o] = @ + 1]
  /\ UNCHANGED <<cards, rows, events, applied, receipt, snap>>

(* Replay after unknown. When the id is not in applied, Replay says "not   *)
(* found" and changes nothing; the caller then calls Apply again, which    *)
(* stays enabled in phase "unknown", so that branch is not a separate step. *)
Replay(o) ==
  /\ phase[o] = "unknown"
  /\ o \in applied
  /\ ReplayEffect(o)

Terminating == (\A o \in OpIds : phase[o] = "done") /\ UNCHANGED vars

Next ==
  \/ \E o \in OpIds : Begin(o) \/ Apply(o) \/ LoseRequest(o) \/ Replay(o)
  \/ Terminating

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Invariants                                                              *)

StoreState ==
  [cards : [Cards -> [state : CardStates \cup {Absent},
                      gen : 0..Cardinality(OpIds),
                      holder : Rows \cup {NoHolder}]],
   rows : [Rows -> RowStates \cup {Absent}]]

TypeOK ==
  /\ Current \in StoreState
  /\ applied \subseteq OpIds
  /\ phase \in [OpIds -> {"idle", "unknown", "inflight", "done"}]
  /\ lost \in [OpIds -> 0..MaxLoss]

(* Every card has exactly one state and at most one holder, and the holder *)
(* is a row that exists. Structural: state is one value from the closed   *)
(* set, holder is one value (or none); stated anyway.                     *)
SingleStateSingleHolder ==
  \A c \in Cards :
    /\ cards[c].state \in CardStates \cup {Absent}
    /\ cards[c].holder \in Rows \cup {NoHolder}
    /\ cards[c].holder # NoHolder => rows[cards[c].holder] # Absent
    /\ cards[c].state = Absent => cards[c] = NoCard

(* No op id appears twice in the event log.                                *)
IdempotentById ==
  \A i, j \in DOMAIN events : i # j => events[i].id # events[j].id

(* The set of op ids the store remembers is exactly the ids in the log.    *)
AppliedIsLog == applied = {events[i].id : i \in DOMAIN events}

(* Folding the accepted events from empty gives the current state, and     *)
(* each event's recorded after-state is the fold up to and including it.  *)
LogMatchesState ==
  /\ Fold(events) = Current
  /\ \A i \in DOMAIN events : events[i].after = Fold(Prefix(i))

(* An accepted op's conditions held against the state immediately before  *)
(* its own writes (the fold of every event before it): no lost update.    *)
ConditionsAtomic ==
  \A i \in DOMAIN events :
    events[i].accepted => Holds(Fold(Prefix(i - 1)), Ops[events[i].id].expect)

(* A refused op changed nothing: its before and after are equal and are   *)
(* the state immediately before it.                                        *)
NoChangeOnRefusal ==
  \A i \in DOMAIN events :
    ~events[i].accepted =>
      /\ events[i].before = events[i].after
      /\ events[i].before = Fold(Prefix(i - 1))

(* A caller that says it knows the outcome holds the stored receipt:      *)
(* unknown is never reported as unchanged or as a different answer.       *)
ReceiptMatches ==
  \A o \in OpIds :
    phase[o] = "done" => (o \in applied /\ receipt[o] = StoredEvent(o))

=============================================================================
