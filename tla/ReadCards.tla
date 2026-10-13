----------------------------- MODULE ReadCards -----------------------------
\* A read is a consumer card (docs/SPEC-SPRINT.md section 6, "A read is a consumer
\* card"; internal/sprint/read_cards.go). One primary in review, three readers, the
\* first of them its worker. The deal cuts every read the attempt still needs at
\* once (need less the reads that stand), each a card on a different reader; a card
\* is ready, then working, then closed ok or broken by its reader, handed back
\* (returned), let pass its deadline (late), or taken back by the machine (away:
\* its reader down or held, a resting route, its primary moved). A reader that
\* closed, returned or let pass a read of the attempt is never dealt it again; a
\* read the machine took back spends nothing and its reader may be dealt it again
\* under the next generation (bounded to MaxGen = 2 for model checking; readNextGen
\* in code has no cap). The primary leaves review only by the
\* existing rules: accepted on the needed count of oks at the attempt (need,
\* ReadsNeededIn: 0, 1 or 2, the setting or the tier's rule, readers.go), reworked
\* on a broken read (a new attempt, its open cards retired), and a merging card
\* short of the needed count goes back to review for the read it lacks
\* (ShortReadsBack, readers.go; the setting raised after its accept).
\*
\* A read is late only from its start (working): one never started is taken back by
\* the machine (TakeBack: the deal bound, a member held), spending no one. The
\* setting is modelled only as it raises (SetReads: need steps up): a lower count
\* strands no card, and a card needing two reads cannot be made to need a third.
\* The instance is the module, as ReaderTiers' is, run with a cfg setting
\* BugIgnoreSpent: FALSE holds every invariant; TRUE, the reversed witness (a deal
\* that forgets a spent reader), breaks NeverTwiceAfterSpent. Its cases are in
\* tla/CASES.tsv and their bench records in tla/RUNS.tsv.
\*
\* Live reads (v1.2.14, a-member-hands-back-a-read-it-is-not-running; the ghost reads of
\* 2026-10-11, schema-c-t04-versions.r8.b2 working with no child for an hour). A working
\* card's member may run a child for it (run) or not (Live: the cfgs that check the live reads,
\* MCReadCardsLive and its witnesses, bounded to one attempt at the cfg's need by LiveBound in
\* MCReadCards.tla; with Live FALSE the live actions are off and the space is the deal's alone): a child its member lost (Lose: a
\* restart, a crash, a stage failure, or a take recorded with no child started). The
\* member beats (Beat, unguarded): its live set names a card while a child runs it, and gone
\* counts the beats since one last did, to Grace + 1. The reconcile takes a working card the
\* beats have not named for Grace windows back to the read deal (LiveReturn: lost, spending
\* no one). Under LiveSpec (the beats and the reconcile keep going) no read stays a ghost:
\* NoGhostRead, a liveness property. Two reversed witnesses break it, each alone:
\* BugNamesHeld, a member whose live set names a read it runs no child for (pkg/member's
\* stage retry before v1.2.14: the launch kept its held result, waiting for a packet the
\* queue never sends), and BugNoLiveReturn, a reconcile that leaves an unnamed read card
\* working (returned only at ReadCardDeadline); Beat is the same in both.

EXTENDS Naturals, FiniteSets

CONSTANT BugIgnoreSpent, Need, Live, BugNamesHeld, BugNoLiveReturn

ASSUME Need \in {0, 1, 2}

Readers == {"w", "a", "b"}
Worker == "w"
MaxAttempt == 2
MaxGen == 2
Grace == 1

Open == {"ready", "working"}
Spent == {"ok", "broken", "returned", "late"}

VARIABLES attempt, pstate, need, cards
vars == <<attempt, pstate, need, cards>>

\* cards: a set of records, one per card ever cut
Card(a, r, g, st) == [att |-> a, rd |-> r, gen |-> g, st |-> st, run |-> FALSE, gone |-> 0]

TypeOK ==
  /\ attempt \in 1..MaxAttempt
  /\ pstate \in {"review", "merging"}
  /\ need \in {0, 1, 2}
  /\ \A c \in cards : c.att \in 1..MaxAttempt /\ c.rd \in Readers /\ c.gen \in 0..MaxGen
                      /\ c.st \in Open \cup Spent \cup {"away", "rework", "lost"}
                      /\ c.run \in BOOLEAN /\ c.gone \in 0..(Grace + 1)

Init == attempt = 1 /\ pstate = "review" /\ need = Need /\ cards = {}

At(a) == {c \in cards : c.att = a}
Mine(a, r) == {c \in At(a) : c.rd = r}
Standing(a) == {c \in At(a) : c.st \in Open \cup {"ok", "broken"}}
Broken(a) == \E c \in At(a) : c.st = "broken"
BugPlainOnly == FALSE
OkReaders(a) == {c.rd : c \in {x \in At(a) : x.st = "ok"}}
OkCards(a) == {c \in At(a) : c.st = "ok"}
CountedCards(a) == {c \in OkCards(a) : ~BugPlainOnly \/ c.gen = 0}
Wanted == IF Broken(attempt) THEN 0 ELSE need - Cardinality(Standing(attempt))
SpentBy(a, r) == \E c \in Mine(a, r) : c.st \in Spent \cup Open
NextGen(a, r) == Cardinality(Mine(a, r))

Eligible ==
  {r \in Readers \ {Worker} :
     /\ (BugIgnoreSpent \/ ~SpentBy(attempt, r))
     /\ \A c \in Mine(attempt, r) : c.st \notin Open
     /\ NextGen(attempt, r) <= MaxGen}

\* the deal: some of the reads wanted, at once, each to a different eligible reader (the
\* room decides how many: any number up to the wanted)
Deal ==
  /\ pstate = "review"
  /\ Wanted > 0
  /\ \E S \in SUBSET Eligible :
       /\ S # {}
       /\ Cardinality(S) <= Wanted
       /\ cards' = cards \cup {Card(attempt, r, NextGen(attempt, r), "ready") : r \in S}
  /\ UNCHANGED <<attempt, pstate, need>>

\* a card leaving working keeps no child and no count: what it was is its st alone
Move(c, st) == cards' = (cards \ {c}) \cup {[c EXCEPT !.st = st, !.run = FALSE, !.gone = 0]}

Set(c, d) == cards' = (cards \ {c}) \cup {d}

\* the take starts a child (run); a take recorded with no child started is Take then Lose
\* (the same states: a separate action would add none)
Take == \E c \in At(attempt) : c.st = "ready" /\ Set(c, [c EXCEPT !.st = "working", !.run = TRUE, !.gone = 0]) /\ UNCHANGED <<attempt, pstate, need>>
\* the member loses the child of a working card: restarted, crashed, its stage failed, or
\* its take started none
Lose == Live /\ \E c \in cards : c.st = "working" /\ c.run /\ Set(c, [c EXCEPT !.run = FALSE]) /\ UNCHANGED <<attempt, pstate, need>>
Close == \E c \in At(attempt), v \in {"ok", "broken"} : c.st = "working" /\ c.run /\ Move(c, v) /\ UNCHANGED <<attempt, pstate, need>>

\* the member's live set names the card: while a child runs it, or, under BugNamesHeld,
\* whenever it is working (a held launch with no child)
Named(c) == c.run \/ BugNamesHeld
\* the member beats: a card its live set names was seen now, any other working card one
\* beat longer unnamed (to Grace + 1). Unguarded: beats keep coming whether or not the
\* reconcile has run, so only LiveReturn takes a ghost out
Beat ==
  /\ Live
  /\ cards' = {IF c.st = "working"
                THEN [c EXCEPT !.gone = IF c.run THEN 0 ELSE IF c.gone > Grace THEN Grace + 1 ELSE c.gone + 1]
                ELSE c : c \in cards}
  /\ UNCHANGED <<attempt, pstate, need>>
\* the reconcile (sprint.LiveReturns): a working read its member's beats have not named for
\* Grace windows goes back to the read deal, lost, spending no one
LiveReturn ==
  /\ Live
  /\ ~BugNoLiveReturn
  /\ \E c \in cards : c.st = "working" /\ ~Named(c) /\ c.gone >= Grace /\ Move(c, "lost")
  /\ UNCHANGED <<attempt, pstate, need>>
Return == \E c \in At(attempt) : c.st \in Open /\ Move(c, "returned") /\ UNCHANGED <<attempt, pstate, need>>
Late == \E c \in At(attempt) : c.st = "working" /\ Move(c, "late") /\ UNCHANGED <<attempt, pstate, need>>
TakeBack == \E c \in At(attempt) : c.st \in Open /\ Move(c, "away") /\ UNCHANGED <<attempt, pstate, need>>

Accept ==
  /\ pstate = "review"
  /\ Cardinality(CountedCards(attempt)) >= need
  /\ pstate' = "merging"
  /\ UNCHANGED <<attempt, need, cards>>

Rework ==
  /\ pstate = "review"
  /\ Broken(attempt)
  /\ attempt < MaxAttempt
  /\ attempt' = attempt + 1
  /\ cards' = {IF c.st \in Open THEN [c EXCEPT !.st = "rework"] ELSE c : c \in cards}
  /\ UNCHANGED <<pstate, need>>

\* the setting raised (nova-sprint set --reads, or a tier pinned after the accept):
\* the count a card needs rises. A lower count is not modelled: it strands no card.
SetReads ==
  /\ need < 2
  /\ need' \in (need + 1)..2
  /\ UNCHANGED <<attempt, pstate, cards>>

\* ShortReadsBack: a merging card short of its reads goes back to review for the
\* read it lacks (sprint.ShortReadsBack, steps_tick.go)
ShortReadsBack ==
  /\ pstate = "merging"
  /\ Cardinality(CountedCards(attempt)) < need
  /\ pstate' = "review"
  /\ UNCHANGED <<attempt, need, cards>>

Next == Deal \/ Take \/ Lose \/ Close \/ Return \/ Late \/ TakeBack \/ Beat \/ LiveReturn
        \/ Accept \/ Rework \/ SetReads \/ ShortReadsBack

Spec == Init /\ [][Next]_vars

\* the live reads' spec: the member keeps beating and the tick keeps reconciling
LiveSpec == Spec /\ WF_vars(Beat) /\ WF_vars(LiveReturn)

\* one open read card per (attempt, reader)
OneOpenPerReader == \A a \in 1..MaxAttempt, r \in Readers : Cardinality({c \in Mine(a, r) : c.st \in Open}) <= 1

\* at most the needed count of reads stand at an attempt: the deal never asks more
\* than it needs
AtMostNeedStanding == \A a \in 1..MaxAttempt : Cardinality(Standing(a)) <= need

\* the worker never reads its own attempt
NeverTheWorker == \A c \in cards : c.rd # Worker

\* a reader that closed, returned or let pass a read of the attempt has no later card of it
NeverTwiceAfterSpent ==
  \A c, d \in cards : (c.att = d.att /\ c.rd = d.rd /\ c.st \in Spent /\ d.gen > c.gen) => FALSE

\* the primary leaves review only by the rules: it merges only with at least Need
\* different oks (the accept asks the current need, never below Need), and a merging
\* card short of the current need goes back to review
MergingOnlyOnOks == pstate = "merging" => Cardinality(CountedCards(attempt)) >= Need

\* OK READS ACCEPT: if a card in review has no broken reads and has at least need ok
\* read cards, Accept is enabled. The witness "plainonly" breaks it by counting only
\* generation 0 cards.
OkReadsAccept ==
  (pstate = "review" /\ ~Broken(attempt) /\ Cardinality(OkCards(attempt)) >= need) => ENABLED Accept

\* the open cards of an attempt past are retired: no read of a reworked attempt stays open
NoOpenReadOfAnOldAttempt == \A c \in cards : c.att < attempt => c.st \notin Open

\* a read working on a member with no child running it eventually leaves working: the
\* member's beats stop naming it and, past the grace, the reconcile takes it back to the
\* deal (or a verdict, a return, a take-back or a rework ends it first). The rule as the
\* server states it, "a read working on a member is named by its latest beat, or within the
\* grace", is this same property while the beat tells the truth (Named = run); it is one
\* property, not two, and its two reversed witnesses are the two ways to break it: the
\* member's word (BugNamesHeld: the beat names a read no child runs) and the server's rule
\* (BugNoLiveReturn: the reconcile leaves an unnamed read working)
Key(c) == <<c.att, c.rd, c.gen>>
Keys == (1..MaxAttempt) \X Readers \X (0..MaxGen)
Ghost(k) == \E c \in cards : Key(c) = k /\ c.st = "working" /\ ~c.run
LeftWorking(k) == ~\E c \in cards : Key(c) = k /\ c.st = "working"
NoGhostRead == \A k \in Keys : Ghost(k) ~> LeftWorking(k)

=============================================================================
