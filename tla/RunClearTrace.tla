-------------------------- MODULE RunClearTrace --------------------------
EXTENDS RunClear, RunClearTraceLog, TLC
\* Deterministic hidden Scan steps connect actual collector entry/completion.
\* Every Collected record binds actual end, needed and cancelled observations.
\* EnvelopeConfirmed binds one concrete confirmed envelope to a finite deterministic Send sequence;
\* only actual rendered member IDs are acknowledged, never omitted IDs.
VARIABLE l
traceVars == <<vars,l>>
Has(r,f) == f \in DOMAIN r
TraceActions == {"Init","AppendNotice","Clear","Prepare","Collected","Send",
                 "SaveCursor","RefusedCursor","Crash","Stutter","EnvelopeConfirmed"}
Logged(r) ==
 /\ Has(r,"epoch") => epoch = r.epoch
 /\ Has(r,"log") => log = r.log
 /\ Has(r,"made") => made = r.made
 /\ Has(r,"cursor") => cursor = r.cursor
 /\ Has(r,"sent") => sent = r.sent
 /\ Has(r,"phase") => phase = r.phase
 /\ Has(r,"snapshotEpoch") => snapshotEpoch = r.snapshotEpoch
 /\ Has(r,"through") => through = r.through
 /\ Has(r,"scan") => scan = r.scan
 /\ Has(r,"needed") => needed = r.needed
 /\ Has(r,"cancelled") => cancelled = r.cancelled
 /\ Has(r,"acknowledged") => acknowledged = r.acknowledged
 /\ Has(r,"crashes") => crashes = r.crashes
LoggedNext(r) ==
 /\ Has(r,"epoch") => epoch' = r.epoch
 /\ Has(r,"log") => log' = r.log
 /\ Has(r,"made") => made' = r.made
 /\ Has(r,"cursor") => cursor' = r.cursor
 /\ Has(r,"sent") => sent' = r.sent
 /\ Has(r,"phase") => phase' = r.phase
 /\ Has(r,"snapshotEpoch") => snapshotEpoch' = r.snapshotEpoch
 /\ Has(r,"through") => through' = r.through
 /\ Has(r,"scan") => scan' = r.scan
 /\ Has(r,"needed") => needed' = r.needed
 /\ Has(r,"cancelled") => cancelled' = r.cancelled
 /\ Has(r,"acknowledged") => acknowledged' = r.acknowledged
 /\ Has(r,"crashes") => crashes' = r.crashes

Required(r) ==
 CASE r.action = "Init" -> {"epoch","log","cursor","sent","phase"}
 [] r.action \in {"AppendNotice","Clear"} -> {"epoch","log"}
 [] r.action = "Prepare" -> {"snapshotEpoch","through","scan","phase"}
 [] r.action = "Collected" -> {"through","scan","needed","cancelled","phase"}
 [] r.action = "Send" -> {"sent","acknowledged"}
 [] r.action = "EnvelopeConfirmed" -> {"members","omitted","recipient","messageID","sent","acknowledged"}
 [] r.action \in {"SaveCursor","RefusedCursor"} -> {"cursor","phase"}
 [] r.action = "Crash" -> {"phase","cursor","needed","cancelled","acknowledged"}
 [] OTHER -> {}
EnvelopeMembers(r) ==
 /\ r.members \subseteq needed /\ r.members # {}
 /\ r.omitted = needed \ r.members
 /\ r.recipient = "r1" /\ r.messageID # ""
EnvelopeEnd(r) ==
 /\ phase = "send" /\ EnvelopeMembers(r)
 /\ r.members \subseteq acknowledged
 /\ UNCHANGED vars
StepAs(r) ==
 CASE r.action = "AppendNotice" -> AppendNotice
 [] r.action = "Clear" -> Clear
 [] r.action = "Prepare" -> Prepare
 [] r.action = "Collected" -> FinishScan
 [] r.action = "Send" -> Send(r.n)
 [] r.action = "EnvelopeConfirmed" -> EnvelopeEnd(r)
 [] r.action = "SaveCursor" -> SaveCursor
 [] r.action = "RefusedCursor" -> RefusedCursor
 [] r.action = "Crash" -> Crash
 [] r.action = "Stutter" -> UNCHANGED vars
 [] OTHER -> FALSE
TraceInit == Init /\ l=1
TraceInitialAllowed == l=1 => Logged(TraceLog[1])
InternalScan ==
 /\ l < Len(TraceLog)
 /\ TraceLog[l+1].action="Collected"
 /\ Scan /\ UNCHANGED l
\* The next observed row pins every member before expansion begins. Only the
\* least remaining member may move; the immutable collection/log cannot change.
\* These are abstract acknowledgements of ONE confirmed physical envelope.
InternalEnvelope ==
 /\ l < Len(TraceLog)
 /\ LET r == TraceLog[l+1] IN
      /\ r.action = "EnvelopeConfirmed"
      /\ EnvelopeMembers(r)
      /\ \E n \in r.members \ acknowledged :
           /\ \A m \in r.members \ acknowledged : n <= m
           /\ Send(n)
 /\ UNCHANGED l
RecordStep ==
 /\ l < Len(TraceLog)
 /\ l'=l+1
 /\ StepAs(TraceLog[l+1])
 /\ LoggedNext(TraceLog[l+1])
TraceNext == InternalScan \/ InternalEnvelope \/ RecordStep
TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)
TraceIndexed ==
 /\ l \in 1..Len(TraceLog)
 /\ \A k \in 1..Len(TraceLog) :
    LET r == TraceLog[k] IN
    /\ {"i","action"} \subseteq DOMAIN r
    /\ r.i=k /\ r.action \in TraceActions
    /\ (k=1)=(r.action="Init")
    /\ Required(r) \subseteq DOMAIN r
    /\ r.action="Send" => Has(r,"n")
TraceMatched == <>(l=Len(TraceLog))
=============================================================================
