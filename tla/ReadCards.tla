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

\* The load window and the read top (v1.4.4, 2026-10-15: forty review cards and no reads
\* dealt; every fleet reader's reads taken back off a resting route five times, then refused
\* "read generations past the load window" for good). The store reads a reader's read cards
\* only through a bound it names from the placed cards and the properties alone
\* (ReadCardExtras): the window past the highest placed card (Window; K = 4 in code, 0 in
\* this instance so a generation past it is reached in two take-backs), or the primary's
\* read top (top: PropReadGenTop) when higher, and one past it. The deal sees only those
\* (Seen), so it deals a reader's next generation only inside the bound (Admit), and it
\* raises the top in the same plan as a card past the window and the top (Deal). CompleteView:
\* every card lies inside what the store reads, so the spent check sees all of them.
\* NeverWedged: a reader not spent at the attempt, under the model's MaxGen, is always
\* admitted. Two reversed witnesses break it, each alone: BugPlacedWindow (the bound is the
\* window past the highest placed card alone, v1.4.3's) and BugNoTop (the deal cuts past the
\* window without raising the top).

\* The findings of a reworked attempt (sprint-spec-fix-retired-read-keeps-findings, 2026-10-18:
\* three heavy reads refused "retired at ... by rework: nothing to report", one a BROKEN
\* verdict with real findings, one an OK on the correct head; the next attempt undid a fix the
\* lost read had confirmed). Under Findings (the cfg MCReadCardsFindings and its witnesses;
\* FALSE elsewhere, where the variables stay empty and the actions below are off) every
\* verdict a reader reports is a finding, tagged with its attempt, reader and generation
\* (findings). The rule's Rework waits: a broken read reworks only when no other read of the
\* attempt is open (reworkWaitsForReads, read_cards.go); the deadline is the open read's own,
\* Late (a working read past ReadCardDeadline) and TakeBack (a ready read past the deal bound),
\* each leaving it open no more. Rework carries every finding of the attempt, and the late
\* reads, into the next attempt's brief (carried; reworkGiven, steps_review.go). The seat's
\* own rework (SeatRework, a mind's) runs at once and retires the open reads (st "seat"). A
\* reader still running a retired read reports it later (LateClose; lateReadUnit,
\* friend_read.go): its finding is a late read (late), and it reaches the next attempt's
\* brief at once while that attempt's work has not started (the work card's finding), else
\* by the rework after it (FieldLateReads). StartWork is that work starting.
\* NoFindingLost: every finding is of the current attempt, or carried, or a late read of a
\* read the seat retired: the machine never makes a finding late. Two reversed witnesses break
\* it, each alone: BugDropLate (a late close drops its finding, the code before this card) and
\* BugReworkAtFirstBroken (the rule reworks at the first broken read, retiring the other
\* while it works, so its finding misses the next attempt's brief). The code also retires a
\* read past its deadline that the take-back has not yet taken at the rework (RetiredByRework);
\* its verdict is then a late read too, a record beyond what NoFindingLost asks.

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

\* the load window (readCardLoadWindow) and the reversed witnesses of the read top: plain
\* definitions, overridden by the witness cfgs
Window == 0
BugPlacedWindow == FALSE
BugNoTop == FALSE

\* the findings (above): plain definitions, overridden by the findings cfgs
Findings == FALSE
BugDropLate == FALSE
BugReworkAtFirstBroken == FALSE

VARIABLES attempt, pstate, need, cards, top, findings, carried, late, started
vars == <<attempt, pstate, need, cards, top, findings, carried, late, started>>

\* cards: a set of records, one per card ever cut
Card(a, r, g, st) == [att |-> a, rd |-> r, gen |-> g, st |-> st, run |-> FALSE, gone |-> 0]

\* a reported verdict: its card's attempt, reader and generation, and the verdict
Finding(c, v) == [att |-> c.att, rd |-> c.rd, gen |-> c.gen, v |-> v]
FindingRecs == [att : 1..MaxAttempt, rd : Readers, gen : 0..MaxGen, v : {"ok", "broken"}]

TypeOK ==
  /\ attempt \in 1..MaxAttempt
  /\ pstate \in {"review", "merging"}
  /\ need \in {0, 1, 2}
  /\ \A c \in cards : c.att \in 1..MaxAttempt /\ c.rd \in Readers /\ c.gen \in 0..MaxGen
                      /\ c.st \in Open \cup Spent \cup {"away", "rework", "seat", "lost"}
                      /\ c.run \in BOOLEAN /\ c.gone \in 0..(Grace + 1)
  /\ top \in [1..MaxAttempt -> 0..MaxGen]
  /\ findings \subseteq FindingRecs /\ carried \subseteq FindingRecs /\ late \subseteq FindingRecs
  /\ started \in BOOLEAN

Init == attempt = 1 /\ pstate = "review" /\ need = Need /\ cards = {} /\ top = [a \in 1..MaxAttempt |-> 0]
        /\ findings = {} /\ carried = {} /\ late = {} /\ started = FALSE

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

Max(x, y) == IF x >= y THEN x ELSE y
\* the generation after the reader's highest placed card (placedNext): a placed card is an
\* open one
PlacedNext(a, r) == IF \E c \in Mine(a, r) : c.st \in Open
                    THEN 1 + CHOOSE g \in 0..MaxGen : \E c \in Mine(a, r) : c.st \in Open /\ c.gen = g
                    ELSE 0
\* readGenBound, and the highest generation the store reads (ReadCardExtras: one past it;
\* v1.4.3 read the window past the placed card alone and refused one past it)
Bound(a, r) == IF BugPlacedWindow THEN PlacedNext(a, r) + Window ELSE Max(PlacedNext(a, r) + Window, top[a])
LoadTo(a, r) == IF BugPlacedWindow THEN Bound(a, r) ELSE Bound(a, r) + 1
Seen(a, r) == {c \in Mine(a, r) : c.gen <= LoadTo(a, r)}
\* readNextGen over what the store read: the generations are cut in order, so the count
SeenNext(a, r) == Cardinality(Seen(a, r))
\* readCardIDFor: the next generation lies inside the bound and the store read it absent
Admit(a, r) == SeenNext(a, r) <= LoadTo(a, r) /\ ~\E c \in Seen(a, r) : c.gen = SeenNext(a, r)
SeenSpent(a, r) == \E c \in Seen(a, r) : c.st \in Spent \cup Open

Eligible ==
  {r \in Readers \ {Worker} :
     /\ (BugIgnoreSpent \/ ~SeenSpent(attempt, r))
     /\ \A c \in Seen(attempt, r) : c.st \notin Open
     /\ SeenNext(attempt, r) <= MaxGen
     /\ Admit(attempt, r)}

\* the read top the plan writes with the cards it cuts (readGenTops): the highest generation
\* cut past the window and past the top it read; BugNoTop writes none
TopAfter(S) ==
  LET gens == {SeenNext(attempt, r) : r \in S}
      hi == CHOOSE g \in gens : \A h \in gens : h <= g
  IN IF BugNoTop \/ hi <= Max(Window, top[attempt]) THEN top ELSE [top EXCEPT ![attempt] = hi]

\* the deal: some of the reads wanted, at once, each to a different eligible reader (the
\* room decides how many: any number up to the wanted)
Deal ==
  /\ pstate = "review"
  /\ Wanted > 0
  /\ \E S \in SUBSET Eligible :
       /\ S # {}
       /\ Cardinality(S) <= Wanted
       /\ cards' = cards \cup {Card(attempt, r, SeenNext(attempt, r), "ready") : r \in S}
       /\ top' = TopAfter(S)
  /\ UNCHANGED <<attempt, pstate, need, findings, carried, late, started>>

\* a card leaving working keeps no child and no count: what it was is its st alone
Move(c, st) == cards' = (cards \ {c}) \cup {[c EXCEPT !.st = st, !.run = FALSE, !.gone = 0]}

Set(c, d) == cards' = (cards \ {c}) \cup {d}

\* the take starts a child (run); a take recorded with no child started is Take then Lose
\* (the same states: a separate action would add none)
Take == \E c \in At(attempt) : c.st = "ready" /\ Set(c, [c EXCEPT !.st = "working", !.run = TRUE, !.gone = 0]) /\ UNCHANGED <<attempt, pstate, need, top, findings, carried, late, started>>
\* the member loses the child of a working card: restarted, crashed, its stage failed, or
\* its take started none
Lose == Live /\ \E c \in cards : c.st = "working" /\ c.run /\ Set(c, [c EXCEPT !.run = FALSE]) /\ UNCHANGED <<attempt, pstate, need, top, findings, carried, late, started>>
\* a verdict on time: under Findings it is a finding of the attempt
Close == \E c \in At(attempt), v \in {"ok", "broken"} :
  /\ c.st = "working" /\ c.run /\ Move(c, v)
  /\ findings' = IF Findings THEN findings \cup {Finding(c, v)} ELSE findings
  /\ UNCHANGED <<attempt, pstate, need, top, carried, late, started>>

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
  /\ UNCHANGED <<attempt, pstate, need, top, findings, carried, late, started>>
\* the reconcile (sprint.LiveReturns): a working read its member's beats have not named for
\* Grace windows goes back to the read deal, lost, spending no one
LiveReturn ==
  /\ Live
  /\ ~BugNoLiveReturn
  /\ \E c \in cards : c.st = "working" /\ ~Named(c) /\ c.gone >= Grace /\ Move(c, "lost")
  /\ UNCHANGED <<attempt, pstate, need, top, findings, carried, late, started>>
Return == \E c \in At(attempt) : c.st \in Open /\ Move(c, "returned") /\ UNCHANGED <<attempt, pstate, need, top, findings, carried, late, started>>
Late == \E c \in At(attempt) : c.st = "working" /\ Move(c, "late") /\ UNCHANGED <<attempt, pstate, need, top, findings, carried, late, started>>
TakeBack == \E c \in At(attempt) : c.st \in Open /\ Move(c, "away") /\ UNCHANGED <<attempt, pstate, need, top, findings, carried, late, started>>

Accept ==
  /\ pstate = "review"
  /\ Cardinality(CountedCards(attempt)) >= need
  /\ pstate' = "merging"
  /\ UNCHANGED <<attempt, need, cards, top, findings, carried, late, started>>

OpenAt(a) == {c \in At(a) : c.st \in Open}

\* what a rework carries into the next attempt's brief: every finding of the attempt and the
\* late reads (reworkGiven)
Carry == carried \cup {f \in findings : f.att = attempt} \cup late

\* the rule's rework (read-broken: TickRuleRework, Rework): it waits while another read of
\* the attempt is open (reworkWaitsForReads); BugReworkAtFirstBroken does not wait
Rework ==
  /\ pstate = "review"
  /\ Broken(attempt)
  /\ BugReworkAtFirstBroken \/ OpenAt(attempt) = {}
  /\ attempt < MaxAttempt
  /\ attempt' = attempt + 1
  /\ cards' = {IF c.st \in Open THEN [c EXCEPT !.st = "rework"] ELSE c : c \in cards}
  /\ carried' = Carry
  /\ started' = FALSE
  /\ UNCHANGED <<pstate, need, top, findings, late>>

\* the seat's rework (rework <card>, a mind's): at once, its open reads retired
SeatRework ==
  /\ Findings
  /\ pstate = "review"
  /\ Broken(attempt)
  /\ attempt < MaxAttempt
  /\ attempt' = attempt + 1
  /\ cards' = {IF c.st \in Open THEN [c EXCEPT !.st = "seat"] ELSE c : c \in cards}
  /\ carried' = Carry
  /\ started' = FALSE
  /\ UNCHANGED <<pstate, need, top, findings, late>>

\* the next attempt's work starts: its brief is read (the take reads its packet)
StartWork == Findings /\ attempt > 1 /\ ~started /\ started' = TRUE
             /\ UNCHANGED <<attempt, pstate, need, cards, top, findings, carried, late>>

\* a reader still running a read a rework retired reports it: a late read (lateReadUnit). Its
\* finding is kept (late), and reaches the next attempt's brief at once while that work has
\* not started; BugDropLate drops it ("retired at ... by rework: nothing to report")
LateClose ==
  /\ Findings
  /\ \E c \in cards, v \in {"ok", "broken"} :
       /\ c.st \in {"rework", "seat"} /\ c.run
       /\ Set(c, [c EXCEPT !.run = FALSE])
       /\ findings' = findings \cup {Finding(c, v)}
       /\ late' = IF BugDropLate THEN late ELSE late \cup {Finding(c, v)}
       /\ carried' = IF BugDropLate \/ started THEN carried ELSE carried \cup {Finding(c, v)}
  /\ UNCHANGED <<attempt, pstate, need, top, started>>

\* the setting raised (nova-sprint set --reads, or a tier pinned after the accept):
\* the count a card needs rises. A lower count is not modelled: it strands no card.
SetReads ==
  /\ need < 2
  /\ need' \in (need + 1)..2
  /\ UNCHANGED <<attempt, pstate, cards, top, findings, carried, late, started>>

\* ShortReadsBack: a merging card short of its reads goes back to review for the
\* read it lacks (sprint.ShortReadsBack, steps_tick.go)
ShortReadsBack ==
  /\ pstate = "merging"
  /\ Cardinality(CountedCards(attempt)) < need
  /\ pstate' = "review"
  /\ UNCHANGED <<attempt, need, cards, top, findings, carried, late, started>>

Next == Deal \/ Take \/ Lose \/ Close \/ Return \/ Late \/ TakeBack \/ Beat \/ LiveReturn
        \/ Accept \/ Rework \/ SetReads \/ ShortReadsBack
        \/ SeatRework \/ StartWork \/ LateClose

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

\* the store reads every card of every reader at its attempt (ReadCardExtras' bound): the
\* deal's spent check and its next identity are never judged on a card it did not read
CompleteView == \A c \in cards : c.gen <= LoadTo(c.att, c.rd)

\* no reader is refused for good: one not spent at the attempt and under the model's bound
\* is admitted its next generation (the owner, 2026-10-06: "I don't want any serial reads
\* again"; v1.4.3 refused every fleet reader of forty primaries "read generations past the
\* load window"). BugPlacedWindow and BugNoTop each break it.
NeverWedged ==
  pstate = "review" =>
    \A r \in Readers \ {Worker} : (~SpentBy(attempt, r) /\ NextGen(attempt, r) <= MaxGen) => Admit(attempt, r)

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

\* NO FINDING LOST: every reported finding is of the attempt under review (or accepted at it),
\* or carried into the next attempt's brief, or a late read of a read the seat's own rework
\* retired, kept and carried by the rework after it: the machine never makes a finding late.
\* BugDropLate and BugReworkAtFirstBroken each break it.
NoFindingLost ==
  \A f \in findings :
    \/ f.att = attempt
    \/ f \in carried
    \/ f \in late /\ \E c \in cards : c.att = f.att /\ c.rd = f.rd /\ c.gen = f.gen /\ c.st = "seat"

=============================================================================
