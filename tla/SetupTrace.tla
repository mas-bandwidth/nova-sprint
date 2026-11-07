---------------------------- MODULE SetupTrace ----------------------------
EXTENDS SetupWalk, SetupTraceLog, TLC

\* Actual-code observations constrain the unchanged Setup.Next relation.
\* All projected fields are required; safety monitors remain model-derived.
\* See AGENTS.md, TLA+ models and trace checks for sampled boundaries and hidden actions.
VARIABLE l
traceVars == <<vars,l>>
TraceActions == {"Init","Probe","Ask","Do","Regress","Crash","Restart","Stutter"}
Has(r,f) == f \in DOMAIN r
Logged(r) ==
  /\ world = r.world
  /\ verified = r.verified
  /\ answers = r.answers
  /\ asked = r.asked
  /\ pc = r.pc
  /\ phase = r.phase
  /\ disruptions = r.disruptions
  /\ keySinks = r.keySinks
LoggedNext(r) ==
  /\ world' = r.world
  /\ verified' = r.verified
  /\ answers' = r.answers
  /\ asked' = r.asked
  /\ pc' = r.pc
  /\ phase' = r.phase
  /\ disruptions' = r.disruptions
  /\ keySinks' = r.keySinks

StepAs(r) ==
 CASE r.action = "Probe" -> Probe
   [] r.action = "Ask" -> Ask(r.q)
   [] r.action = "Do" -> Do
   [] r.action = "Regress" -> Regress(r.k)
   [] r.action = "Crash" -> Crash
   [] r.action = "Restart" -> Restart
   [] r.action = "Stutter" -> UNCHANGED vars
   [] OTHER -> FALSE

\* Assign observed initial state, then CHECK WalkInit as an invariant.
\* Invalid initial observations must fail, never produce vacuous zero states.
TraceInit ==
  /\ l = 1 /\ Logged(TraceLog[1])
  /\ orderOK = TRUE /\ verifiedOnly = TRUE /\ resumeOK = TRUE
TraceInitialAllowed == l = 1 => WalkInit
TraceNext ==
  /\ l < Len(TraceLog)
  /\ l' = l + 1
  /\ StepAs(TraceLog[l+1])
  /\ LoggedNext(TraceLog[l+1])
TraceSpec == TraceInit /\ [][TraceNext]_traceVars /\ WF_traceVars(TraceNext)
TraceIndexed ==
 /\ l \in 1..Len(TraceLog)
 /\ Len(TraceLog) > 0
 /\ \A k \in 1..Len(TraceLog) :
     /\ {"i","action","world","verified","answers","asked","pc","phase",
          "disruptions","keySinks"} \subseteq DOMAIN TraceLog[k]
     /\ TraceLog[k].i = k
     /\ TraceLog[k].action \in TraceActions
     /\ (k = 1) = (TraceLog[k].action = "Init")
     /\ TraceLog[k].action = "Ask" => Has(TraceLog[k],"q")
     /\ TraceLog[k].action = "Regress" => Has(TraceLog[k],"k")
TraceMatched == <>(l = Len(TraceLog))
=============================================================================
