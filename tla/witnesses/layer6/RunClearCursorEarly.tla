---------------------------- MODULE RunClearCursorEarly ----------------------------
\* FAULT: Save the cursor before all required notice and summary sends confirm.
\* Must violate CursorHasCoverage.
EXTENDS Naturals, Sequences, FiniteSets

(***************************************************************************
 Focused refinement of Run's delivery contract when Clear is permitted.
 One recipient, one notice per modeled notice event, and at most one Clear.
 Positions are physical log indexes. Scan folds history one event at a time.
 A real bounded scan may span calls; here the small prefix fits its budget.

 Snapshot epoch is an observation, not an atomic fence around bus Send.
 If Clear commits after Prepare, an old notice may still be sent. Its command
 carries the original epoch, so the verb/store must refuse a later mutation.
 Once a scan observes the new epoch it suppresses old backlog, includes the
 Clear summary, and saves its cursor only after all required sends confirm.

 Cancellation counts are cursor-pending notices, NOT certainly never delivered.
 A prior partial successful send or crash-before-cursor-save can be counted.
 ClearCountIsUndelivered is deliberately false in that execution. Summary text:
 "epoch <n> cleared: <k> pending notices dropped (some may have reached you);
  run nova-sprint next". Repeated summaries after a crash are permitted.

 Send confirms exit 0 plus the intended recipient/message id; failed sends
 stutter. Liveness assumes finite appends/crashes and fair eventual confirmed
 sends. Safety depends on epoch-fenced cursor Apply supplied by the store.
 This module adds no setup, scheduler, or full runtime-wiring proof.
***************************************************************************)
CONSTANTS MaxNotices, MaxCrashes
VARIABLES epoch, log, made, cursor, sent, phase, snapshotEpoch, through, scan,
          needed, cancelled, acknowledged, crashes, oldSent, ambiguousCount
vars == <<epoch, log, made, cursor, sent, phase, snapshotEpoch, through, scan,
          needed, cancelled, acknowledged, crashes, oldSent, ambiguousCount>>
Indexes == 1..Len(log)
ClearCovers(n,c) == /\ log[n].kind = "notice" /\ log[c].kind = "clear"
                    /\ n < c /\ log[n].epoch = log[c].epoch
Covered(n) == IF n \in Indexes
              THEN n \in sent \/ \E c \in sent : ClearCovers(n,c)
              ELSE FALSE
OldPending(c) == {n \in cancelled : ClearCovers(n,c)}
Init == /\ epoch = 1 /\ log = <<>> /\ made = 0 /\ cursor = 0 /\ sent = {}
        /\ phase = "idle" /\ snapshotEpoch = 1 /\ through = 0 /\ scan = 0
        /\ needed = {} /\ cancelled = {} /\ acknowledged = {} /\ crashes = 0
        /\ oldSent = FALSE /\ ambiguousCount = FALSE
AppendNotice ==
  /\ made < MaxNotices
  /\ log' = Append(log, [kind |-> "notice", epoch |-> epoch])
  /\ made' = made + 1
  /\ UNCHANGED <<epoch, cursor, sent, phase, snapshotEpoch, through, scan,
       needed, cancelled, acknowledged, crashes, oldSent, ambiguousCount>>
Clear ==
  /\ epoch = 1
  /\ log' = Append(log, [kind |-> "clear", epoch |-> epoch])
  /\ epoch' = epoch + 1
  /\ UNCHANGED <<made, cursor, sent, phase, snapshotEpoch, through, scan,
       needed, cancelled, acknowledged, crashes, oldSent, ambiguousCount>>
Prepare ==
  /\ phase = "idle" /\ cursor < Len(log)
  /\ snapshotEpoch' = epoch /\ through' = Len(log) /\ scan' = cursor
  /\ phase' = "scan" /\ needed' = {} /\ cancelled' = {} /\ acknowledged' = {}
  /\ UNCHANGED <<epoch, log, made, cursor, sent, crashes, oldSent, ambiguousCount>>
Scan ==
  /\ phase = "scan" /\ scan < through
  /\ LET n == scan + 1
         old == log[n].kind = "notice" /\ log[n].epoch < snapshotEpoch
     IN /\ scan' = n
        /\ needed' = IF old THEN needed ELSE needed \cup {n}
        /\ cancelled' = IF old THEN cancelled \cup {n} ELSE cancelled
  /\ UNCHANGED <<epoch, log, made, cursor, sent, phase, snapshotEpoch, through,
       acknowledged, crashes, oldSent, ambiguousCount>>
FinishScan ==
  /\ phase = "scan" /\ scan = through
  /\ phase' = "send"
  /\ UNCHANGED <<epoch, log, made, cursor, sent, snapshotEpoch, through, scan,
       needed, cancelled, acknowledged, crashes, oldSent, ambiguousCount>>
Send(n) ==
  /\ phase = "send" /\ n \in needed \ acknowledged
  /\ sent' = sent \cup {n} /\ acknowledged' = acknowledged \cup {n}
  /\ oldSent' = (oldSent \/ (log[n].kind = "notice" /\ log[n].epoch < epoch))
  /\ ambiguousCount' = (ambiguousCount \/
         (log[n].kind = "clear" /\ (OldPending(n) \cap sent) # {}))
  /\ UNCHANGED <<epoch, log, made, cursor, phase, snapshotEpoch, through, scan,
       needed, cancelled, crashes>>
SaveCursor ==
  /\ phase = "send"
  /\ snapshotEpoch = epoch
  /\ cursor' = through /\ phase' = "idle"
  /\ UNCHANGED <<epoch, log, made, sent, snapshotEpoch, through, scan, needed,
       cancelled, acknowledged, crashes, oldSent, ambiguousCount>>
RefusedCursor ==
  /\ phase = "send" /\ needed \subseteq acknowledged
  /\ snapshotEpoch # epoch
  /\ phase' = "idle"
  /\ UNCHANGED <<epoch, log, made, cursor, sent, snapshotEpoch, through, scan,
       needed, cancelled, acknowledged, crashes, oldSent, ambiguousCount>>
Crash ==
  /\ phase # "idle" /\ crashes < MaxCrashes
  /\ phase' = "idle" /\ needed' = {} /\ cancelled' = {} /\ acknowledged' = {}
  /\ crashes' = crashes + 1
  /\ UNCHANGED <<epoch, log, made, cursor, sent, snapshotEpoch, through, scan,
       oldSent, ambiguousCount>>
Progress == Prepare \/ Scan \/ FinishScan \/ SaveCursor \/ RefusedCursor
            \/ (\E n \in Indexes : Send(n))
Next == AppendNotice \/ Clear \/ Crash \/ Progress
Spec == Init /\ [][Next]_vars /\ WF_vars(Progress)
TypeOK == /\ epoch \in 1..2 /\ made \in 0..MaxNotices
          /\ cursor \in 0..Len(log) /\ sent \subseteq Indexes
          /\ phase \in {"idle","scan","send"}
          /\ scan \in 0..Len(log) /\ through \in 0..Len(log)
          /\ needed \subseteq Indexes /\ cancelled \subseteq Indexes
          /\ acknowledged \subseteq needed /\ crashes \in 0..MaxCrashes
CursorHasCoverage == \A n \in 1..cursor : Covered(n)
NoUncommittedSend == sent \subseteq Indexes
CancellationHasClear == \A n \in cancelled :
                         \E c \in 1..through : ClearCovers(n,c)
AllAccounted == \A n \in 1..(MaxNotices+1) :
                                 (n <= Len(log)) ~> Covered(n)
AllCursorsAdvance == \A n \in 1..(MaxNotices+1) :
                                 (n <= Len(log)) ~> (n <= cursor)
\* These stronger claims are intentionally NOT passing requirements.
NoOldSendAfterClear == ~oldSent
ClearCountIsUndelivered == ~ambiguousCount
EveryOriginalSent == \A n \in 1..(MaxNotices+1) :
                                 (n <= Len(log)) ~> (n \in sent)
=============================================================================
