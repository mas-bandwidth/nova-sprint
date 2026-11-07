----------------------------- MODULE RunTrace -----------------------------
EXTENDS Run, RunTraceLog, TLC, Integers

\* Finite actual-code observations, following the shared StepsTrace pattern.
\* See AGENTS.md, TLA+ models and trace checks. Partial logging has per-action required fields.
\* Unlogged scheduler abstractions remain assumptions, not code evidence.
VARIABLE l, physicalSeq, observationSeq, capturedSeq
traceVars == <<vars,l,physicalSeq,observationSeq,capturedSeq>>
NoSeq == -1
SequenceAppends == {"Stop","Start","Report","Apply","Refuse","SaveCursor"}
Has(r,f) == f \in DOMAIN r
TraceActions == {"Init","Take","Hook","HookFailure","LoseLock","LoseHook",
 "Crash","NoticeLost","Fold","Select","Report","Refuse","Apply","EndTick",
 "PreparePush","SendOK","SaveCursor","DeliverEdge","Stutter",
 "Stop","Start","ReadControl","CaptureFence"}
ProcessActions == TraceActions \ {"Init","Report","DeliverEdge","Stutter","Stop","Start"}
Logged(r) ==
 /\ Has(r,"physicalSeq") => physicalSeq = r.physicalSeq
 /\ Has(r,"stopped") => sprintStopped = r.stopped
 /\ Has(r,"observedSeq") => observationSeq[r.p] = r.observedSeq
 /\ Has(r,"expectedSeq") => capturedSeq[r.p] = r.expectedSeq
 /\ Has(r,"observedStopped") => local[r.p].control = (IF r.observedStopped THEN "stopped" ELSE "running")
 /\ Has(r,"procFenced") => fenceFrozen[r.p] = r.procFenced
 /\ Has(r,"lock") => lock = r.lock
 /\ Has(r,"hook") => hook = r.hook
 /\ Has(r,"local") => local = r.local
 /\ Has(r,"gens") => gens = r.gens
 /\ Has(r,"done") => done = r.done
 /\ Has(r,"counts") => counts = r.counts
 /\ Has(r,"events") => events = r.events
 /\ Has(r,"reports") => reports = r.reports
 /\ Has(r,"failures") => failures = r.failures
 /\ Has(r,"cursor") => cursor = r.cursor
 /\ Has(r,"sent") => sent = r.sent
 /\ Has(r,"push") => push = r.push
 /\ Has(r,"edgeDone") => edgeDone = r.edgeDone
 /\ Has(r,"procPhase") => local[r.p].phase = r.procPhase
 /\ Has(r,"procBatch") => local[r.p].batch = r.procBatch
 /\ Has(r,"procCarry") => local[r.p].carry = r.procCarry
 /\ Has(r,"procPutback") => local[r.p].putback = r.procPutback
 /\ Has(r,"procCopy") => local[r.p].copy = r.procCopy
 /\ Has(r,"procFold") => local[r.p].fold = r.procFold
 /\ Has(r,"procApplied") => local[r.p].tickApplied = r.procApplied
 /\ Has(r,"procChecked") => local[r.p].checked = r.procChecked
 /\ Has(r,"procSeen") => local[r.p].seen = r.procSeen
LoggedNext(r) ==
 /\ Has(r,"physicalSeq") => physicalSeq' = r.physicalSeq
 /\ Has(r,"stopped") => sprintStopped' = r.stopped
 /\ Has(r,"observedSeq") => observationSeq'[r.p] = r.observedSeq
 /\ Has(r,"expectedSeq") => capturedSeq'[r.p] = r.expectedSeq
 /\ Has(r,"observedStopped") => local'[r.p].control = (IF r.observedStopped THEN "stopped" ELSE "running")
 /\ Has(r,"procFenced") => fenceFrozen'[r.p] = r.procFenced
 /\ Has(r,"lock") => lock' = r.lock
 /\ Has(r,"hook") => hook' = r.hook
 /\ Has(r,"local") => local' = r.local
 /\ Has(r,"gens") => gens' = r.gens
 /\ Has(r,"done") => done' = r.done
 /\ Has(r,"counts") => counts' = r.counts
 /\ Has(r,"events") => events' = r.events
 /\ Has(r,"reports") => reports' = r.reports
 /\ Has(r,"failures") => failures' = r.failures
 /\ Has(r,"cursor") => cursor' = r.cursor
 /\ Has(r,"sent") => sent' = r.sent
 /\ Has(r,"push") => push' = r.push
 /\ Has(r,"edgeDone") => edgeDone' = r.edgeDone
 /\ Has(r,"procPhase") => local'[r.p].phase = r.procPhase
 /\ Has(r,"procBatch") => local'[r.p].batch = r.procBatch
 /\ Has(r,"procCarry") => local'[r.p].carry = r.procCarry
 /\ Has(r,"procPutback") => local'[r.p].putback = r.procPutback
 /\ Has(r,"procCopy") => local'[r.p].copy = r.procCopy
 /\ Has(r,"procFold") => local'[r.p].fold = r.procFold
 /\ Has(r,"procApplied") => local'[r.p].tickApplied = r.procApplied
 /\ Has(r,"procChecked") => local'[r.p].checked = r.procChecked
 /\ Has(r,"procSeen") => local'[r.p].seen = r.procSeen

Required(r) == {"physicalSeq"} \cup CASE r.action = "Init" -> {"lock","hook","gens","done","events","cursor","sent","edgeDone","stopped"}
 [] r.action \in {"Take","Hook","HookFailure","LoseLock","LoseHook","Crash","NoticeLost"}
    -> {"lock","hook","procPhase"}
 [] r.action \in {"Stop","Start"} -> {"stopped"}
 [] r.action = "ReadControl" -> {"observedSeq","observedStopped"}
 [] r.action = "CaptureFence" -> {"expectedSeq","procFenced"}
 [] r.action = "Fold" -> {"procCopy","procFold","procCarry","procPhase"}
 [] r.action = "Select" -> {"procBatch","procCarry","procPhase"}
 [] r.action = "Report" -> {"gens","events"}
 [] r.action = "Apply" -> {"gens","done","events","procBatch","procApplied","expectedSeq","receiptSeq","procFenced"}
 [] r.action = "Refuse" -> {"procBatch","procPutback","expectedSeq","receiptSeq","procFenced"}
 [] r.action = "EndTick" -> {"procCarry","procBatch","procPutback","procPhase"}
 [] r.action = "PreparePush" -> {"push"}
 [] r.action = "SendOK" -> {"sent","push"}
 [] r.action = "SaveCursor" -> {"cursor","push"}
 [] r.action = "DeliverEdge" -> {"edgeDone"}
 [] OTHER -> {}
StepAs(r) ==
 CASE r.action = "Stop" -> Stop
 [] r.action = "Start" -> Start
 [] r.action = "ReadControl" -> ReadControl(r.p)
 [] r.action = "CaptureFence" -> CaptureFence(r.p)
 [] r.action = "Take" -> Take(r.p)
 [] r.action = "Hook" -> Hook(r.p)
 [] r.action = "HookFailure" -> HookFailure(r.p)
 [] r.action = "LoseLock" -> LoseLock(r.p)
 [] r.action = "LoseHook" -> LoseHook(r.p)
 [] r.action = "Crash" -> Crash(r.p)
 [] r.action = "NoticeLost" -> NoticeLost(r.p)
 [] r.action = "Fold" -> Fold(r.p)
 [] r.action = "Select" -> Select(r.p)
 [] r.action = "Report" -> Report(r.c)
 [] r.action = "Refuse" -> Refuse(r.p)
 [] r.action = "Apply" -> Apply(r.p)
 [] r.action = "EndTick" -> EndTick(r.p)
 [] r.action = "PreparePush" -> PreparePush(r.p,r.r)
 [] r.action = "SendOK" -> SendOK(r.p,r.r)
 [] r.action = "SaveCursor" -> SaveCursor(r.p,r.r)
 [] r.action = "DeliverEdge" -> DeliverEdge(r.j)
 [] r.action = "Stutter" -> UNCHANGED vars
 [] OTHER -> FALSE
TraceInit == Init /\ l = 1
 /\ physicalSeq = TraceLog[1].physicalSeq
 /\ observationSeq = [p \in Processes |-> NoSeq]
 /\ capturedSeq = [p \in Processes |-> NoSeq]
SequenceStep(r) ==
 /\ physicalSeq' = IF r.action \in SequenceAppends THEN physicalSeq+1 ELSE physicalSeq
 /\ observationSeq' = IF r.action = "ReadControl"
       THEN [observationSeq EXCEPT ![r.p] = physicalSeq] ELSE observationSeq
 /\ capturedSeq' = IF r.action = "CaptureFence"
       THEN [capturedSeq EXCEPT ![r.p] = observationSeq[r.p]] ELSE capturedSeq
 /\ r.action \in {"Apply","Refuse"} =>
       /\ r.receiptSeq = physicalSeq + 1
       /\ r.expectedSeq = capturedSeq[r.p]
SequenceProjection ==
 /\ physicalSeq \in Nat
 /\ \A p \in Processes :
      /\ observedFresh[p] = (observationSeq[p] = physicalSeq)
      /\ sequenceFresh[p] = (capturedSeq[p] = physicalSeq)
TraceInitialAllowed == l = 1 => Logged(TraceLog[1])
TraceNext ==
 /\ l < Len(TraceLog)
 /\ l' = l+1
 /\ StepAs(TraceLog[l+1])
 /\ SequenceStep(TraceLog[l+1])
 /\ LoggedNext(TraceLog[l+1])
TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)
TraceIndexed ==
 /\ l \in 1..Len(TraceLog)
 /\ Len(TraceLog) > 0
 /\ \A k \in 1..Len(TraceLog) :
    LET r == TraceLog[k] IN
    /\ {"i","action"} \subseteq DOMAIN r
    /\ r.i = k /\ r.action \in TraceActions
    /\ (k = 1) = (r.action = "Init")
    /\ Required(r) \subseteq DOMAIN r
    /\ r.action \in ProcessActions => Has(r,"p") /\ r.p \in Processes
    /\ r.action \in {"PreparePush","SendOK","SaveCursor"} => Has(r,"r") /\ r.r \in Recipients
    /\ r.action = "Report" => Has(r,"c") /\ r.c \in Cards
    /\ r.action = "DeliverEdge" => Has(r,"j") /\ r.j \in Jobs
TraceMatched == <>(l = Len(TraceLog))
=============================================================================
