------------------------- MODULE StoreRetention -------------------------
\* The sprint store's retention (v17-sprint-store-every-key-bounded): the
\* log's and the inbox's entries per epoch, the coordinator's cursor
\* (keyCursor), the log index, each epoch's done hash, and the current epoch.
\* Actions append a line (the store's XADD MAXLEN), advance the cursor, trim,
\* retire a past epoch, record a caller operation, and replay it.
\*
\* The design (Broken = {}):
\*   Append      appends one line to the current epoch's log and trims the
\*               oldest lines to LogBound. When the trim drops the cursor's
\*               line it records the first kept line in gaps: the coordinator
\*               is told of the gap, never given an error, and no unread line
\*               is trimmed silently (NoTrimPastCursor). The log index keeps
\*               an entry only for a line the log holds (IndexNeverDangles).
\*   Advance     the coordinator reads up to a line.
\*   Record       a caller operation's result goes into the epoch's done hash,
\*               bounded by DoneBound.
\*   Retire       an epoch older than the previous one has its log, index and
\*               done removed; its operations are refused by name (refused).
\*   Replay       a caller operation in a held epoch is answered from done.
\*
\* Reversed witnesses:
\*   "unbounded"   Append trims nothing, so Bounded fails.
\*   "trim"        a trim that passes the cursor records no gap, so
\*                 NoTrimPastCursor fails.
\*   "retire-live" Retire removes the previous epoch's done without refusing
\*                 its operations, so ReplayAnswered fails.
EXTENDS Naturals, Sequences, FiniteSets, TLC

CONSTANTS Broken, Epochs, LogBound, DoneBound, KeepEpochs

VARIABLES log, cursor, index, done, epoch, gaps, retired, retiredOps, refused, answered, next

vars == <<log, cursor, index, done, epoch, gaps, retired, retiredOps, refused, answered, next>>

Ops == {"a", "b"}
MaxLine == LogBound + 6

Held == {e \in Epochs : e + KeepEpochs > epoch}

TypeOK ==
    /\ log \in [Epochs -> Seq(0..MaxLine)]
    /\ cursor \in 0..MaxLine
    /\ index \subseteq [e : Epochs, line : 1..MaxLine]
    /\ done \in [Epochs -> SUBSET Ops]
    /\ epoch \in Epochs
    /\ gaps \subseteq 1..MaxLine
    /\ retired \subseteq Epochs
    /\ retiredOps \subseteq Ops
    /\ refused \subseteq Ops
    /\ answered \subseteq Ops
    /\ next \in 1..MaxLine

Init ==
    /\ log = [e \in Epochs |-> <<>>]
    /\ cursor = 0
    /\ index = {}
    /\ done = [e \in Epochs |-> {}]
    /\ epoch = 0
    /\ gaps = {}
    /\ retired = {}
    /\ retiredOps = {}
    /\ refused = {}
    /\ answered = {}
    /\ next = 1

\* append a line, then trim the oldest to LogBound (the store's
\* XADD MAXLEN ~ bound); the index drops what the trim dropped
AppendLine ==
    LET n == next
        new == Append(log[epoch], n)
        cut == IF Len(new) > LogBound /\ "unbounded" \notin Broken
               THEN Len(new) - LogBound ELSE 0
        first == IF cut < Len(new) THEN new[cut + 1] ELSE 0
    IN /\ n < MaxLine
       /\ next' = n + 1
       /\ log' = [log EXCEPT ![epoch] = SubSeq(new, cut + 1, Len(new))]
       /\ index' = (index \ {[e |-> epoch, line |-> log[epoch][k]] : k \in 1..cut})
                   \cup {[e |-> epoch, line |-> n]}
       /\ gaps' = IF cut = 0 \/ cursor >= first
                    THEN gaps
                    ELSE IF "trim" \in Broken THEN gaps ELSE gaps \cup {first}
       /\ UNCHANGED <<cursor, done, epoch, retired, retiredOps, refused, answered>>

\* the coordinator reads up to a line
AdvanceCursor ==
    \E c \in ({0} \cup {log[epoch][k] : k \in 1..Len(log[epoch])}) :
        /\ cursor' = c
        /\ UNCHANGED <<log, index, done, epoch, gaps, retired, retiredOps, refused, answered, next>>

\* the sprint advances one epoch
AdvanceEpoch ==
    /\ \E e \in Epochs : e > epoch
    /\ epoch' = epoch + 1
    /\ cursor' = 0
    /\ UNCHANGED <<log, index, done, gaps, retired, retiredOps, refused, answered, next>>

\* a caller operation's result is recorded in this epoch's done hash
Record ==
    \E op \in Ops :
        /\ op \notin done[epoch]
        /\ ("unbounded" \in Broken \/ Cardinality(done[epoch]) < DoneBound)
        /\ done' = [done EXCEPT ![epoch] = @ \cup {op}]
        /\ UNCHANGED <<log, cursor, index, epoch, gaps, retired, retiredOps, refused, answered, next>>

\* a past epoch is retired: its log, index and done go; its operations are
\* refused by name. Only an epoch older than the previous one may be retired.
Retire ==
    \E e \in Epochs :
        /\ \/ "retire-live" \in Broken /\ e < epoch
           \/ e + KeepEpochs <= epoch
        /\ log' = [log EXCEPT ![e] = <<>>]
        /\ index' = index \ {[e |-> e, line |-> log[e][n]] : n \in 1..Len(log[e])}
        /\ done' = [done EXCEPT ![e] = {}]
        /\ retired' = retired \cup {e}
        /\ retiredOps' = retiredOps \cup done[e]
        /\ IF "retire-live" \in Broken
           THEN UNCHANGED refused
           ELSE refused' = refused \cup done[e]
        /\ UNCHANGED <<cursor, epoch, gaps, answered, next>>

\* a caller operation inside the retention window is answered from done
Replay ==
    \E op \in Ops :
        /\ \E e \in Held : op \in done[e]
        /\ answered' = answered \cup {op}
        /\ UNCHANGED <<log, cursor, index, done, epoch, gaps, retired, retiredOps, refused, next>>

Next ==
    \/ AppendLine
    \/ AdvanceCursor
    \/ AdvanceEpoch
    \/ Record
    \/ Retire
    \/ Replay

Spec == Init /\ [][Next]_vars

\* each stream and hash is at most its bound
Bounded ==
    /\ \A e \in Epochs : Len(log[e]) <= LogBound
    /\ \A e \in Epochs : Cardinality(done[e]) <= DoneBound
    /\ \A e \in Epochs : Cardinality({i \in index : i.e = e}) <= LogBound

\* a line the coordinator has not read is never trimmed, or the trim is
\* recorded as the gap the coordinator is told of
NoTrimPastCursor ==
    \A e \in Held :
        Len(log[e]) = 0 \/ cursor = 0 \/ cursor >= log[e][1] \/ log[e][1] \in gaps

\* an index entry never names a trimmed line
IndexNeverDangles ==
    \A i \in index :
        \E e \in Epochs : i.e = e /\ \E k \in 1..Len(log[e]) : log[e][k] = i.line

\* every operation of a retired epoch was refused by name; one in a held
\* epoch is still in done and so is answered, never re-run
ReplayAnswered ==
    retiredOps \subseteq refused

============================================================================
