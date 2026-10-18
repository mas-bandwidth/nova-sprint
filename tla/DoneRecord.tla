----------------------------- MODULE DoneRecord -----------------------------
\* The done record (internal/sprint/store/done.go; internal/sprint/done.go): every card a row
\* completed, read off the epoch's log after a cursor. The tick keeps it (keepDone): it reads
\* the record, takes up to Page lines after its cursor, and writes the record back, its
\* completions and its new cursor. A record of another epoch is empty (doneRecordOf). where
\* shows the record's count after the stats reset's mark (CountDone, DoneOf), or, with no record
\* of the epoch, the epoch's log read whole (DoneSince). Counting dedupes by card (CountDone:
\* a card of a kind is counted once on its row), so a line read twice is one completion.
\*
\* The environment: a completion appends a log line (a card may complete again: a re-read of
\* the same attempt's line, or the same line logged twice); a clear starts a new epoch with an
\* empty log; a stats reset sets the mark at the log's end (completions after it count); a
\* tick may lose its write (StaleWrite: two ticks read the same record and the slower writes an
\* older cursor back, so lines are read again); a deploy or rollback changes the running build's
\* layout version (BuildChanges). An older build that meets a newer layout refuses the tick
\* (keepDone, doneRefusedNewer) and writes nothing over it (OlderBuildNeverWritesNewerLayout),
\* while where reads the epoch's log whole (DoneSince). BugNoDedup counts every entry;
\* BugKeepEpoch takes a record of another epoch as this one's; BugOverwriteNewer drops the
\* layout_newer write guard so an older build overwrites a newer record.
\*
\* The record is stored in pages (writeDonePages: page 0, with the cursor and the page count,
\* then the follow-on pages one key at a time). A write cut between its pages (TornWrite: page
\* 0 advanced, a later page not written) leaves a record that does not read (torn:
\* missing_page); the tick reads the whole record, so a torn one is Current's empty record and
\* the next tick rebuilds it from the log, quiet log or not (TornRepaired, under a fair Tick).
\* BugPageZeroGuard is the guard of PR #486's first head (2026-10-17): the tick skips when the
\* log has no line past page 0's cursor, read alone, so a torn record on a quiet log is never
\* rebuilt (MCDoneRecordBrokenPageZeroGuard.cfg breaks TornRepaired).

EXTENDS Naturals, Sequences, FiniteSets

CONSTANT BugNoDedup, BugKeepEpoch, BugNoOriginGuard

BugOverwriteNewer == FALSE
BugPageZeroGuard == FALSE

Cards == {"c1", "c2"}
MaxLines == 3
MaxEpoch == 2
MaxVer == 2
Page == 1

\* One representative repo-backed successful finish. Origin may change after
\* review; the invariant records the observation at the transition, not a
\* permanent promise about a mutable remote. A passed rework is a successful
\* finish even when requested with --failed. Actual failed/no-repo/late routes
\* do not enter review through this transition.
VARIABLES originHasHead, inReview, originAtFinish
VARIABLES epoch, log, mark, rec, stale, justReset, build, overwroteNewer
\* torn: the stored record's page 0 is written and a later page is not (a cut write)
VARIABLE torn
vars == <<epoch, log, mark, rec, stale, justReset, build, overwroteNewer, originHasHead, inReview, originAtFinish, torn>>

\* a record: its epoch, its cursor (lines read), its entries (card, line index), and the layout
\* version of the build that wrote it (0 before any fixed record is written)
Rec(e, c, d, v) == [ep |-> e, cur |-> c, done |-> d, ver |-> v]

TypeOK ==
  /\ epoch \in 1..MaxEpoch
  /\ log \in Seq(Cards) /\ Len(log) <= MaxLines
  /\ mark \in 0..MaxLines
  /\ rec.ep \in 1..MaxEpoch /\ rec.cur \in 0..MaxLines /\ rec.ver \in 0..MaxVer
  /\ stale.ep \in 1..MaxEpoch /\ stale.cur \in 0..MaxLines /\ stale.ver \in 0..MaxVer
  /\ build \in 1..MaxVer
  /\ justReset \in BOOLEAN /\ overwroteNewer \in BOOLEAN
  /\ originHasHead \in BOOLEAN /\ inReview \in BOOLEAN /\ originAtFinish \in BOOLEAN
  /\ torn \in BOOLEAN

Init == epoch = 1 /\ log = <<>> /\ mark = 0 /\ rec = Rec(1, 0, <<>>, 0) /\ stale = Rec(1, 0, <<>>, 1) /\ justReset = FALSE
        /\ build = 1 /\ overwroteNewer = FALSE
        /\ originHasHead = FALSE /\ inReview = FALSE /\ originAtFinish = FALSE /\ torn = FALSE

\* the record as the tick or where reads it (doneRecordOf): empty when it is another epoch's or
\* written by a newer layout this build cannot decode (layout_newer)
Readable(r) == (r.ep = epoch \/ BugKeepEpoch) /\ r.ver <= build
\* the stored record reads whole: page 0 readable and no page of it missing (readDoneFrom)
Whole == Readable(rec) /\ ~torn
Current == IF Whole THEN rec ELSE Rec(epoch, 0, <<>>, 0)

\* the entries' count after the mark: distinct cards (CountDone), or every entry (the bug)
After(d) == {i \in 1..Len(d) : d[i][2] > mark}
Count(d) == IF BugNoDedup THEN Cardinality(After(d)) ELSE Cardinality({d[i][1] : i \in After(d)})

\* the truth: the distinct cards the epoch's log completed after the mark
Truth == Cardinality({log[i] : i \in {j \in 1..Len(log) : j > mark}})

\* the read's lines after the cursor, up to n of them, as entries written at this build's layout
Take(r, n) ==
  LET hi == IF r.cur + n > Len(log) THEN Len(log) ELSE r.cur + n
  IN Rec(epoch, hi, r.done \o [i \in 1..(hi - r.cur) |-> <<log[r.cur + i], r.cur + i>>], build)

\* where's figure: the record's count, or the log read whole with no readable record of the epoch
Shown == LET r == Current IN IF r.cur > 0 THEN Count(r.done) ELSE Count(Take(Rec(epoch, 0, <<>>, 0), MaxLines).done)

Complete == \E c \in Cards :
  /\ Len(log) < MaxLines
  /\ log' = Append(log, c)
  /\ justReset' = FALSE
  /\ UNCHANGED <<epoch, mark, rec, stale, build, overwroteNewer, originHasHead, inReview, originAtFinish, torn>>

\* the tick writes when the log has lines past the cursor of the record it read whole
\* (keepDone: readDoneFrom, then doneAfter); the bug reads page 0's cursor alone
TickDue == IF BugPageZeroGuard /\ Readable(rec) THEN rec.cur < Len(log) ELSE Current.cur < Len(log)

\* the tick's write: the record with the lines after its cursor, every page of it
Tick ==
  /\ rec.ver <= build \/ BugOverwriteNewer
  /\ TickDue
  /\ stale' = [Current EXCEPT !.ver = build]
  /\ rec' = Take(Current, Page)
  /\ overwroteNewer' = (overwroteNewer \/ rec.ver > build)
  /\ justReset' = FALSE
  /\ torn' = FALSE
  /\ UNCHANGED <<epoch, log, mark, build, originHasHead, inReview, originAtFinish>>

\* the same write cut after page 0: the new cursor stored, a later page missing
TornWrite ==
  /\ rec.ver <= build \/ BugOverwriteNewer
  /\ TickDue
  /\ rec' = Take(Current, Page)
  /\ torn' = TRUE
  /\ overwroteNewer' = (overwroteNewer \/ rec.ver > build)
  /\ justReset' = FALSE
  /\ UNCHANGED <<epoch, log, mark, stale, build, originHasHead, inReview, originAtFinish>>

\* the slower of two ticks writes back the record it read: its older cursor and entries; the
\* next tick reads those lines again
StaleWrite ==
  /\ stale.ep = epoch
  /\ stale.ver = build
  /\ rec.ver <= build \/ BugOverwriteNewer
  /\ rec' = stale
  /\ overwroteNewer' = (overwroteNewer \/ rec.ver > build)
  /\ justReset' = FALSE
  /\ torn' = FALSE
  /\ UNCHANGED <<epoch, log, mark, stale, build, originHasHead, inReview, originAtFinish>>

BuildChanges == \E b \in 1..MaxVer :
  /\ b /= build
  /\ build' = b
  /\ UNCHANGED <<epoch, log, mark, rec, stale, justReset, overwroteNewer, originHasHead, inReview, originAtFinish, torn>>

Clear ==
  /\ epoch < MaxEpoch
  /\ epoch' = epoch + 1
  /\ log' = <<>>
  /\ mark' = 0
  /\ justReset' = FALSE
  /\ UNCHANGED <<rec, stale, build, overwroteNewer, originHasHead, inReview, originAtFinish, torn>>

Reset ==
  /\ mark' = Len(log)
  /\ justReset' = TRUE
  /\ UNCHANGED <<epoch, log, rec, stale, build, overwroteNewer, originHasHead, inReview, originAtFinish, torn>>

OriginChanges ==
  /\ originHasHead' = ~originHasHead
  /\ UNCHANGED <<epoch, log, mark, rec, stale, justReset, build, overwroteNewer, inReview, originAtFinish, torn>>

Finish ==
  /\ ~inReview
  /\ originHasHead \/ BugNoOriginGuard
  /\ inReview' = TRUE
  /\ originAtFinish' = originHasHead
  /\ UNCHANGED <<epoch, log, mark, rec, stale, justReset, build, overwroteNewer, originHasHead, torn>>

Next == Complete \/ Tick \/ TornWrite \/ StaleWrite \/ BuildChanges \/ Clear \/ Reset \/ OriginChanges \/ Finish

ReviewObservedOnOrigin == inReview => originAtFinish

\* an older build refuses layout_newer and never writes its older layout over a newer record
OlderBuildNeverWritesNewerLayout == ~overwroteNewer

Spec == Init /\ [][Next]_vars

\* the tick runs while it is due (the run loop ticks on a quiet log too)
LiveSpec == Spec /\ WF_vars(Tick)

\* a torn record of the epoch is rebuilt: it reads whole again, unless the epoch moved on (a
\* clear: the record is another epoch's, and empty) or the build that runs cannot read its
\* layout (it refuses and writes nothing, OlderBuildNeverWritesNewerLayout)
TornRepaired == torn ~> (~torn \/ rec.ep # epoch \/ rec.ver > build)

\* no completion is counted twice, however often a line is read again (a stale write, a retried
\* tick), and none of another epoch is counted: where shows at most the truth
NoDoubleCount == Shown <= Truth

\* once caught up, where shows the truth exactly
CaughtUpIsTruth == Current.cur = Len(log) /\ Len(log) > 0 => Shown = Truth

\* a stats reset shows done 0 until the next completion; a clear starts the count again
ResetShowsZero == justReset => Shown = 0
ClearStartsAgain == Len(log) = 0 => Shown = 0

=============================================================================
