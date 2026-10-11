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

EXTENDS Naturals, FiniteSets

CONSTANT BugReadersOnly, Need

ASSUME Need \in {1, 2}

Readers == {"a", "b"}
Tables == {"readers", "fleet"}

VARIABLES reads, pstate, work
vars == <<reads, pstate, work>>

\* a read: its reader, the table it is on, its row (the reader's on the readers table, empty
\* on the fleet table once retired) and its verdict
Read(r, t, v) == [rd |-> r, tb |-> t, row |-> IF t = "readers" THEN r ELSE "", v |-> v]

TypeOK ==
  /\ pstate \in {"review", "merging"}
  /\ work \in {"finished", "ok", "failed"}
  /\ \A x \in reads : x.rd \in Readers /\ x.tb \in Tables /\ x.v \in {"ok", "broken"}

Init == reads = {} /\ pstate = "review" /\ work = "finished"

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

\* a reader reads the attempt once, on either table; the ask asks no more reads than the
\* attempt needs (ReadCards.tla AtMostNeedStanding)
ReadIt == \E r \in Readers, t \in Tables, v \in {"ok", "broken"} :
  /\ pstate = "review"
  /\ Cardinality(reads) < Need
  /\ \A x \in reads : x.rd # r
  /\ reads' = reads \cup {Read(r, t, v)}
  /\ UNCHANGED <<pstate, work>>

Accept ==
  /\ pstate = "review"
  /\ Cardinality(AcceptOks) >= Need
  /\ ~AcceptBroken
  /\ pstate' = "merging"
  /\ UNCHANGED <<reads, work>>

\* the tick's verdicts part: an accepted primary's work is ok (readVerdict: accepted and not
\* short), a broken read is failed, Need oks are ok
Verdict ==
  /\ work = "finished"
  /\ \/ /\ ~BugReadersOnly /\ pstate = "merging"
        /\ work' = "ok"
     \/ /\ VerdictBroken
        /\ work' = "failed"
     \/ /\ ~VerdictBroken /\ Cardinality(VerdictOks) >= Need
        /\ work' = "ok"
  /\ UNCHANGED <<reads, pstate>>

Next == ReadIt \/ Accept \/ Verdict

Spec == Init /\ [][Next]_vars /\ WF_vars(Verdict) /\ WF_vars(Accept)

\* the verdict never counts ok what the accept would refuse
OkOnlyWhenAcceptable == work = "ok" => (Cardinality(AcceptOks) >= Need /\ ~AcceptBroken)

\* an accepted card's work is never counted failed
AcceptedNeverFailed == pstate = "merging" => work # "failed"

\* the dashboard's truth: an accepted card's work comes to count ok in its worker's done
AcceptedWorkCountsOk == (pstate = "merging") ~> (work = "ok")

=============================================================================
