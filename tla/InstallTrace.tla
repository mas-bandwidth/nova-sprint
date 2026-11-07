-------------------------- MODULE InstallTrace --------------------------
EXTENDS Install, InstallTraceLog, Sequences, TLC
\* Callback snapshots precede callback return. Hidden actions are fixed by
\* those observed outcomes; only filesystem endpoint states are sampled.
VARIABLE l
traceVars == <<vars,l>>
Disk(r) == /\ active=r.active /\ candidateBytes=(IF r.candidate THEN 2 ELSE 0)
           /\ previousPresent=r.previousPresent
DiskNext(r) == /\ active'=r.active /\ candidateBytes'=(IF r.candidate THEN 2 ELSE 0)
               /\ previousPresent'=r.previousPresent
TraceInit == Init /\ l=1
TraceInitialAllowed == l=1 =>
 /\ Disk(TraceLog[1]) /\ TraceLog[1].checkOK=FALSE
 /\ TraceLog[1].version="unread" /\ TraceLog[1].problem="none"
Hidden ==
 /\ l < Len(TraceLog)
 /\ LET r == TraceLog[l] n == TraceLog[l+1] IN
    \/ /\ n.action="CheckCall" /\ (Begin \/ FinishWrite)
    \/ /\ r.action="VersionCall"
       /\ ReadVersion(r.version)
    \/ /\ r.action="VersionCall" /\ n.action="ReturnSuccess"
       /\ PrepareLink
 /\ UNCHANGED l
RecordAction(r) ==
 CASE r.action="CheckCall" -> Publish
 [] r.action="VersionCall" ->
    /\ TraceLog[l].action="CheckCall" /\ TraceLog[l].checkOK
    /\ CheckPass
 [] r.action="ReturnSuccess" ->
    /\ TraceLog[l].action="VersionCall" /\ Activate
    /\ r.checkOK /\ r.version=Requested /\ r.problem="none"
 [] r.action="ReturnFailure" ->
    /\ CASE r.problem="check" ->
          /\ TraceLog[l].action="CheckCall" /\ ~TraceLog[l].checkOK
          /\ CheckFail
       [] r.problem="version" ->
          /\ phase="failed" /\ failure="version" /\ UNCHANGED vars
       [] r.problem="external" -> Fail
       [] OTHER -> FALSE
    /\ checked'=r.checkOK /\ reported'=r.version
 [] OTHER -> FALSE
RecordStep ==
 /\ l < Len(TraceLog) /\ l'=l+1
 /\ RecordAction(TraceLog[l+1]) /\ DiskNext(TraceLog[l+1])
TraceNext == Hidden \/ RecordStep
TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)
TraceIndexed ==
 /\ l \in 1..Len(TraceLog)
 /\ \A k \in 1..Len(TraceLog) : LET r == TraceLog[k] IN
    /\ {"i","action","active","candidate","checkOK","version","problem",
        "previousPresent"} \subseteq DOMAIN r
    /\ r.i=k /\ (k=1)=(r.action="Init")
    /\ r.action \in {"Init","CheckCall","VersionCall","ReturnSuccess","ReturnFailure"}
    /\ r.candidate \in BOOLEAN /\ r.checkOK \in BOOLEAN
    /\ r.version \in {"unread",Requested,Other}
    /\ r.action="CheckCall" =>
       /\ r.version="unread"
       /\ r.problem=(IF r.checkOK THEN "none" ELSE "check")
    /\ r.action="VersionCall" =>
       /\ r.checkOK /\ r.version \in {Requested,Other}
       /\ r.problem=(IF r.version=Requested THEN "none" ELSE "version")
TraceMatched == <>(l=Len(TraceLog))
=============================================================================
