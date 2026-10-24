---------------------------- MODULE FriendReadSlot ----------------------------
\* Reads never wait behind a full row (roadmap reads-never-wait-behind-a-full-row, critical;
\* internal/sprint/read_mix.go roomFor, read_cards.go readCardsLevel and the read-card ask,
\* friend_give_read.go friendGiveRead). On 2026-10-24 four reads were dealt to a friend whose
\* row was at 8 of 8 and sat in her ready column for an hour while 18 cards waited in review,
\* and friend give refused their ids. Three rules, one module:
\*   - the deal (Deal) hands a read to a reader only while its row has a free slot (its width
\*     less its work cards and its read cards, ready and working): a read it places is working,
\*     never ready behind a full row (ReadOnlyToAFreeSlot, SlotsHard);
\*   - a read ready on a row and not begun is aged by the tick (Tick), and once it has waited
\*     the start bound the tick's ask levels it to another reader with a free slot, never its
\*     author, before the tick ends (LevelledByTheTick, an action property); with none free it
\*     stays where it is;
\*   - friend give (Give) moves a read ready and not begun to the friend named, never its
\*     author, only into a free slot (NeverTheAuthor, ReadOnlyToAFreeSlot).
\* The reads in Init may already sit ready on a friend's row past her width: the state the
\* old rule left (roomFor dealt the mix's friend read at her room), which the level drains.
\* Readers are three friends (c is the author of every primary, the worker who wrote it) and a
\* fleet member m, each of width one; a row's other work comes and goes (Busy, Idle) within
\* its width. The readers' tiers, the mix's classes, the read top and the transport are not
\* modelled (ReadMix, ReadCards, WhoPreference hold those).
\*
\* Reversed witnesses, each alone: BadDeal ("fullrow": the deal hands a friend a read at her
\* width, queued ready: the incident) breaks ReadOnlyToAFreeSlot; BadLevel (the tick never
\* levels: the deal bound alone, six hours, took an untaken read back) breaks
\* LevelledByTheTick; BadGive ("author": give hands the read to its author) breaks
\* NeverTheAuthor.
EXTENDS Naturals, FiniteSets

CONSTANTS Reads, Bound, BadDeal, BadLevel, BadGive

Friends == {"a", "b", "c"}
Worker == "c"
Readers == Friends \cup {"m"}
Width == 1
None == "none"

VARIABLES loc, col, begun, age, held, placed
vars == <<loc, col, begun, age, held, placed>>

\* the reads on a reader's row that hold a slot: ready or working
OnRow(r) == {p \in Reads : loc[p] = r /\ col[p] \in {"ready", "working"}}
Load(r) == held[r] + Cardinality(OnRow(r))
Free(r) == Load(r) < Width

TypeOK ==
  /\ loc \in [Reads -> Readers \cup {None}]
  /\ col \in [Reads -> {"none", "ready", "working", "done"}]
  /\ begun \in [Reads -> BOOLEAN]
  /\ age \in [Reads -> 0..Bound]
  /\ held \in [Readers -> 0..Width]
  /\ placed \subseteq Reads

\* a read waits in review, or sits ready, not begun, on a friend other than its author: the
\* old rule's queue on a busy friend
Init ==
  /\ held \in [Readers -> 0..Width]
  /\ loc \in [Reads -> {None, "a", "b"}]
  /\ col = [p \in Reads |-> IF loc[p] = None THEN "none" ELSE "ready"]
  /\ begun = [p \in Reads |-> FALSE]
  /\ age = [p \in Reads |-> 0]
  /\ placed = {}

\* the readers that may read p where it is not: never its author, never where it sits
Others(p) == Readers \ {Worker, loc[p]}

\* the read-card ask's deal: a read waiting in review to a reader with a free slot, working
\* (roomFor: a friend's half > 0 in every phase); "fullrow" deals a friend at her width,
\* queued ready, as the mix's friend phase did
Deal(p, r) ==
  /\ loc[p] = None
  /\ r \in Readers \ {Worker}
  /\ Free(r) \/ (BadDeal = "fullrow" /\ r \in Friends)
  /\ loc' = [loc EXCEPT ![p] = r]
  /\ col' = [col EXCEPT ![p] = IF Free(r) THEN "working" ELSE "ready"]
  /\ age' = [age EXCEPT ![p] = 0]
  /\ placed' = placed \cup {p}
  /\ UNCHANGED <<begun, held>>

\* the reader begins it (her take, her beat naming it running): it is hers to finish
Begin(p) ==
  /\ col[p] \in {"ready", "working"} /\ ~begun[p]
  /\ begun' = [begun EXCEPT ![p] = TRUE]
  /\ col' = [col EXCEPT ![p] = "working"]
  /\ UNCHANGED <<loc, age, held, placed>>

Finish(p) ==
  /\ col[p] = "working" /\ begun[p]
  /\ col' = [col EXCEPT ![p] = "done"]
  /\ UNCHANGED <<loc, begun, age, held, placed>>

\* a row's other work: a work card dealt into a free slot, or one finished
Busy(r) == /\ Free(r) /\ held' = [held EXCEPT ![r] = @ + 1] /\ UNCHANGED <<loc, col, begun, age, placed>>
Idle(r) == /\ held[r] > 0 /\ held' = [held EXCEPT ![r] = @ - 1] /\ UNCHANGED <<loc, col, begun, age, placed>>

Untaken(p) == col[p] = "ready" /\ ~begun[p]

\* a read the tick's ask must level: untaken the start bound, with another reader free
Levellable(p) == Untaken(p) /\ age[p] >= Bound /\ \E r \in Others(p) : Free(r)

\* the tick: every untaken read ages (to Bound), then the read-card ask levels each one that
\* has waited the bound to another reader with a free slot (readCardsLevel: taken back, dealt
\* working in the same ask, one free slot each), as many as the free slots allow, the rest
\* staying; BadLevel levels none
Tick ==
  LET a == [p \in Reads |-> IF Untaken(p) THEN IF age[p] < Bound THEN age[p] + 1 ELSE Bound ELSE age[p]]
      due(p) == Untaken(p) /\ a[p] >= Bound
      got(t, r) == Cardinality({p \in Reads : t[p] = r})
  IN \E t \in [Reads -> Readers \cup {None}] :
       /\ \A p \in Reads : t[p] # None => (~BadLevel /\ due(p) /\ t[p] \in Others(p))
       /\ \A r \in Readers : got(t, r) > 0 => Load(r) + got(t, r) <= Width
       /\ \A p \in Reads : (t[p] = None /\ due(p) /\ ~BadLevel) =>
            \A r \in Others(p) : Load(r) + got(t, r) >= Width
       /\ loc' = [p \in Reads |-> IF t[p] = None THEN loc[p] ELSE t[p]]
       /\ col' = [p \in Reads |-> IF t[p] = None THEN col[p] ELSE "working"]
       /\ age' = [p \in Reads |-> IF t[p] = None THEN a[p] ELSE 0]
       /\ placed' = placed \cup {p \in Reads : t[p] # None}
       /\ UNCHANGED <<begun, held>>

\* friend give <f> <read>: a read ready and not begun moves to the friend named, working,
\* only into her free slot, never to its author ("author" drops that refusal)
Give(p, f) ==
  /\ Untaken(p)
  /\ f \in Friends /\ f # loc[p]
  /\ f # Worker \/ BadGive = "author"
  /\ Free(f)
  /\ loc' = [loc EXCEPT ![p] = f]
  /\ col' = [col EXCEPT ![p] = "working"]
  /\ age' = [age EXCEPT ![p] = 0]
  /\ placed' = placed \cup {p}
  /\ UNCHANGED <<begun, held>>

Next ==
  \/ \E p \in Reads, r \in Readers : Deal(p, r) \/ Give(p, r)
  \/ \E p \in Reads : Begin(p) \/ Finish(p)
  \/ \E r \in Readers : Busy(r) \/ Idle(r)
  \/ Tick

Spec == Init /\ [][Next]_vars

\* a read the deal, the level or a give placed is never ready: it went to a free slot
ReadOnlyToAFreeSlot == \A p \in placed : col[p] # "ready"

\* the rows the system filled hold no more than their width
SlotsHard == \A r \in Readers : held[r] + Cardinality(placed \cap OnRow(r)) <= Width

\* no read sits with its author
NeverTheAuthor == \A p \in Reads : loc[p] # Worker

\* the tick leaves no read untaken past the bound while another reader had a free slot through
\* the tick (a slot the tick itself frees, a read levelled off it, is the next tick's: the ask
\* reads the free slots once, readCardsLevel)
LevelledByTheTick ==
  [][Tick => \A p \in Reads : (Untaken(p)' /\ age'[p] >= Bound) =>
                \A r \in Readers \ {Worker, loc'[p]} : ~(Free(r) /\ Free(r)')]_vars

=============================================================================
