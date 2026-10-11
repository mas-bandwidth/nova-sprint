----------------------------- MODULE ReadVerdict -----------------------------
\* The readers' verdict on a worker's finish agrees with the accept (internal/sprint/
\* verdicts.go readVerdict and TickVerdicts; steps_review.go standingReads, okReaders;
\* readers.go hasBrokenAtHead). The owner, 2026-10-11: "The dashboard has to reflect the
\* truth." A fleet row's done and ok% (and a friend's) count the work cards in its ok and
\* failed cells; a worker's finish puts its card in finished, and only the verdict moves it
\* on. Stella's finding of 2026-10-11: the verdict read the readers table alone, keyed by the
\* read's row, while the accept read the readers table and the fleet table's read cards
\* (retired off their rows with their verdicts, so with no row), keyed by the reader. A card
\* accepted on two fleet reads left its work in finished for ever: friend stella's row read
\* done 0, ok% 0.0%.
\*
\* One primary at one attempt and head, its work card finished by the worker; readers each
\* read it once, ok or broken, on the readers table (a placed read, its row the reader's)
\* or on the fleet table (a read card retired with its verdict, its row empty). The accept
\* takes the primary to merging on Need different readers' oks and no broken read; the tick's
\* verdict moves the work card finished -> ok or failed. BugReadersOnly is the reversed
\* witness: the verdict reads the readers table alone and keys an ok by the read's row, as
\* readVerdict did before the fix; TLC finds the accepted card whose work never counts.
\*
\* Two paths reach an accepted card with a broken read at its head, and the model has both:
\* the coordinator's heavy read overrules a broken read (accept --heavy, HeavyAccept), and a
\* fleet read comes back broken after the accept (Accept retires only the readers table's
\* outstanding reads; LateFleetRead). The decision (no rule of docs/SPEC-SPRINT.md reworks
\* a merging card on a late read, and the lander holds a merging card to its count of ok
\* reads, ReadsShort): the verdict follows the accept. An accepted card's work not yet judged
\* counts ok (readVerdict's first case), whatever read comes after; a verdict already given
\* is never rewritten (a heavy accept after the work was judged failed leaves it failed).

EXTENDS Naturals, FiniteSets

CONSTANT BugReadersOnly, Need

ASSUME Need \in {1, 2}

Readers == {"a", "b", "c"}
Tables == {"readers", "fleet"}

VARIABLES reads, pstate, work, heavy
vars == <<reads, pstate, work, heavy>>

\* a read: its reader, the table it is on, its row (the reader's on the readers table, empty
\* on the fleet table once retired) and its verdict
Read(r, t, v) == [rd |-> r, tb |-> t, row |-> IF t = "readers" THEN r ELSE "", v |-> v]

TypeOK ==
  /\ pstate \in {"review", "merging"}
  /\ work \in {"finished", "ok", "failed"}
  /\ heavy \in BOOLEAN
  /\ \A x \in reads : x.rd \in Readers /\ x.tb \in Tables /\ x.v \in {"ok", "broken"}

Init == reads = {} /\ pstate = "review" /\ work = "finished" /\ heavy = FALSE

\* the accept's count (okReaders, hasBrokenAtHead): every table, one a reader
AcceptOks == {x.rd : x \in {y \in reads : y.v = "ok"}}
AcceptBroken == \E x \in reads : x.v = "broken"

\* the verdict's count: the accept's (standingReads), or before the fix the readers table's
\* alone keyed by the row
VerdictOks == IF BugReadersOnly
                THEN {x.row : x \in {y \in reads : y.v = "ok" /\ y.tb = "readers"}}
                ELSE AcceptOks
VerdictBroken == IF BugReadersOnly
                   THEN \E x \in reads : x.v = "broken" /\ x.tb = "readers"
                   ELSE AcceptBroken

Fresh(r) == \A x \in reads : x.rd # r

\* a reader reads the attempt once, on either table; the ask asks no more reads than the
\* attempt needs (ReadCards.tla AtMostNeedStanding)
ReadIt == \E r \in Readers, t \in Tables, v \in {"ok", "broken"} :
  /\ pstate = "review"
  /\ Cardinality(reads) < Need
  /\ Fresh(r)
  /\ reads' = reads \cup {Read(r, t, v)}
  /\ UNCHANGED <<pstate, work, heavy>>

\* a fleet read dealt before the accept comes back after it, ok or broken (the accept retires
\* the readers table's outstanding reads, never a fleet read card)
LateFleetRead == \E r \in Readers, v \in {"ok", "broken"} :
  /\ pstate = "merging"
  /\ Fresh(r)
  /\ reads' = reads \cup {Read(r, "fleet", v)}
  /\ UNCHANGED <<pstate, work, heavy>>

Accept ==
  /\ pstate = "review"
  /\ Cardinality(AcceptOks) >= Need
  /\ ~AcceptBroken
  /\ pstate' = "merging"
  /\ UNCHANGED <<reads, work, heavy>>

\* accept --heavy: the coordinator's own read counts as one ok and overrules a broken read
HeavyAccept ==
  /\ pstate = "review"
  /\ Cardinality(AcceptOks) + 1 >= Need
  /\ pstate' = "merging"
  /\ heavy' = TRUE
  /\ UNCHANGED <<reads, work>>

\* the tick's verdicts part (readVerdict), its cases in order: an accepted card not short of
\* reads is ok; else a broken read at the head is failed; else Need oks are ok
VerdictOf ==
  IF ~BugReadersOnly /\ pstate = "merging" THEN "ok"
  ELSE IF VerdictBroken THEN "failed"
  ELSE IF Cardinality(VerdictOks) >= Need THEN "ok"
  ELSE "none"

Verdict ==
  /\ work = "finished"
  /\ VerdictOf # "none"
  /\ work' = VerdictOf
  /\ UNCHANGED <<reads, pstate, heavy>>

Next == ReadIt \/ LateFleetRead \/ Accept \/ HeavyAccept \/ Verdict

Spec == Init /\ [][Next]_vars /\ WF_vars(Verdict)

\* the verdict counts ok only what the accept took, or what the accept would take: Need oks
\* and no broken read (a broken read after the accept, or one a heavy read overruled, leaves
\* the accepted card's work ok)
OkOnlyWhenAcceptedOrAcceptable == work = "ok" => (pstate = "merging" \/ (Cardinality(AcceptOks) >= Need /\ ~AcceptBroken))

\* a card accepted on its reads (no heavy overrule) never has its work counted failed, a late
\* broken read or not
AcceptedOnReadsNeverFailed == (pstate = "merging" /\ ~heavy) => work # "failed"

\* the dashboard's truth: an accepted card's work not yet judged comes to count ok
AcceptedWorkCountsOk == (pstate = "merging" /\ work = "finished") ~> (work = "ok")

=============================================================================
