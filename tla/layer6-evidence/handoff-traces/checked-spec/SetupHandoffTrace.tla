-------------------------- MODULE SetupHandoffTrace --------------------------
EXTENDS SetupHandoff, SetupHandoffTraceLog, Sequences, TLC
VARIABLE l
traceVars == <<vars,l>>
Has(r,f) == f \in DOMAIN r
Logged(r) ==
 /\ Has(r,"reuses") => reuses=r.reuses
 /\ Has(r,"epoch") => epoch=r.epoch
 /\ Has(r,"recordEpoch") => recordEpoch=r.recordEpoch
 /\ Has(r,"binding") => binding=r.binding
 /\ Has(r,"seat") => seat=r.seat
 /\ Has(r,"pc") => pc=r.pc
 /\ Has(r,"lease") => lease=r.lease
 /\ Has(r,"credential") => credential=r.credential
 /\ Has(r,"schema") => schema=r.schema
 /\ Has(r,"record") => record=r.record
 /\ Has(r,"prefix") => prefix=r.prefix
 /\ Has(r,"fresh") => fresh=r.fresh
 /\ Has(r,"validated") => validated=r.validated
 /\ Has(r,"attemptEffect") => attemptEffect=r.attemptEffect
 /\ Has(r,"exposed") => exposed=r.exposed
 /\ Has(r,"crashes") => crashes=r.crashes
LoggedNext(r) ==
 /\ Has(r,"reuses") => reuses'=r.reuses
 /\ Has(r,"epoch") => epoch'=r.epoch
 /\ Has(r,"recordEpoch") => recordEpoch'=r.recordEpoch
 /\ Has(r,"binding") => binding'=r.binding
 /\ Has(r,"seat") => seat'=r.seat
 /\ Has(r,"pc") => pc'=r.pc
 /\ Has(r,"lease") => lease'=r.lease
 /\ Has(r,"credential") => credential'=r.credential
 /\ Has(r,"schema") => schema'=r.schema
 /\ Has(r,"record") => record'=r.record
 /\ Has(r,"prefix") => prefix'=r.prefix
 /\ Has(r,"fresh") => fresh'=r.fresh
 /\ Has(r,"validated") => validated'=r.validated
 /\ Has(r,"attemptEffect") => attemptEffect'=r.attemptEffect
 /\ Has(r,"exposed") => exposed'=r.exposed
 /\ Has(r,"crashes") => crashes'=r.crashes
Required(r) ==
 CASE r.action="Init" -> {"epoch","recordEpoch","binding","seat","lease","credential","schema","record","exposed"}
 [] r.action="Prefix1" -> {"seat","lease"}
 [] r.action \in {"Acquire1","Acquire2","Prefix2","Verify2","Compete"} -> {"lease"}
 [] r.action="SaveCredential" -> {"credential","lease"}
 [] r.action \in {"SaveSchema","PreparedRole"} -> {"schema","credential","lease"}
 [] r.action="SaveRecord" -> {"record","recordEpoch","epoch","credential","schema","lease"}
 [] r.action="Release" -> {"lease","exposed"}
 [] r.action \in {"Effect","Refuse"} -> {"lease","exposed","attemptEffect"}
 [] r.action="Launch2" -> {"seat","lease"}
 [] r.action \in {"Prevalidate","Validate"} -> {"record","recordEpoch","epoch","binding","credential","schema","lease"}
 [] r.action="ChangeEpoch" -> {"epoch"}
 [] r.action="ChangeBinding" -> {"binding"}
 [] r.action="Crash" -> {"seat","lease","record","credential","schema"}
 [] r.action \in {"Retry","Reuse"} -> {"seat","record","lease"}
 [] OTHER -> {}
StepAs(r) ==
 CASE r.action="Prefix1" -> Prefix1
 [] r.action="Acquire1" -> Acquire1
 [] r.action="SaveCredential" -> SaveCredential
 [] r.action="SaveSchema" -> SaveSchema
 [] r.action="SaveRecord" -> SaveRecord
 [] r.action="Release" -> Release
 [] r.action="Launch2" -> Launch2
 [] r.action="Prevalidate" -> Prevalidate
 [] r.action="Prefix2" -> Prefix2
 [] r.action="Acquire2" -> Acquire2
 [] r.action="Validate" -> Validate
 [] r.action="Verify2" -> Verify2
 [] r.action="Effect" -> Effect
 [] r.action="Refuse" -> Refuse
 [] r.action="Compete" -> Compete
 [] r.action="ChangeEpoch" -> ChangeEpoch
 [] r.action="ChangeBinding" -> ChangeBinding
 [] r.action="Crash" -> Crash
 [] r.action="Retry" -> Retry
 [] r.action="Reuse" -> Reuse
 [] r.action="PreparedRole" -> SaveSchema
 [] r.action="Stutter" -> UNCHANGED vars
 [] OTHER -> FALSE
TraceInit == Init /\ l=1
TraceInitialAllowed == l=1 => Logged(TraceLog[1])
HiddenRole ==
 /\ l < Len(TraceLog)
 /\ TraceLog[l+1].action="PreparedRole"
 /\ SaveCredential /\ UNCHANGED l
RecordStep ==
 /\ l < Len(TraceLog) /\ l'=l+1
 /\ StepAs(TraceLog[l+1]) /\ LoggedNext(TraceLog[l+1])
TraceNext == HiddenRole \/ RecordStep
TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)
TraceIndexed ==
 /\ l \in 1..Len(TraceLog)
 /\ \A k \in 1..Len(TraceLog) : LET r == TraceLog[k] IN
    /\ {"i","action"} \subseteq DOMAIN r
    /\ r.i=k /\ (k=1)=(r.action="Init")
    /\ Required(r) \subseteq DOMAIN r
    /\ r.action \in {"Init","PreparedRole","Stutter","Prefix1","Acquire1","SaveCredential","SaveSchema","SaveRecord","Release","Launch2","Prevalidate","Prefix2","Acquire2","Validate","Verify2","Effect","Refuse","Compete","ChangeEpoch","ChangeBinding","Crash","Retry","Reuse"}
TraceMatched == <>(l=Len(TraceLog))
=============================================================================
