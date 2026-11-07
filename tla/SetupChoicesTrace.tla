---------------------------- MODULE SetupChoicesTrace ----------------------------
EXTENDS SetupChoicesWalk, SetupChoicesTraceLog, TLC

\* Actual-code observations constrain the unchanged SetupChoices.Next relation.
\* All projected fields are required; safety monitors remain model-derived.
\* See SetupChoicesTrace-MAPPING.md for sampled boundaries and hidden actions.
VARIABLE l
traceVars == <<vars,l>>
TraceActions == {"Init","Probe","Ask","Do","Regress","Crash","Restart","Stutter","Supply","ChooseServices"}
Has(r,f) == f \in DOMAIN r
Logged(r) ==
  /\ servicesChoice = r.servicesChoice
  /\ world = r.world
  /\ verified = r.verified
  /\ answers = r.answers
  /\ asked = r.asked
  /\ pc = r.pc
  /\ phase = r.phase
  /\ disruptions = r.disruptions
  /\ keySinks = r.keySinks
LoggedNext(r) ==
  /\ servicesChoice' = r.servicesChoice
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
   [] r.action = "Supply" -> Supply(r.q)
   [] r.action = "ChooseServices" -> ChooseServices(r.value,r.prompted)
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
          "disruptions","keySinks","servicesChoice"} \subseteq DOMAIN TraceLog[k]
     /\ TraceLog[k].i = k
     /\ TraceLog[k].action \in TraceActions
     /\ (k = 1) = (TraceLog[k].action = "Init")
     /\ TraceLog[k].action \in {"Ask","Supply"} => Has(TraceLog[k],"q")
     /\ TraceLog[k].action = "Regress" => Has(TraceLog[k],"k")
TraceChoiceFields == l \in 1..Len(TraceLog) /\ \A k \in 1..Len(TraceLog) :
  TraceLog[k].action = "ChooseServices" => {"value","prompted"} \subseteq DOMAIN TraceLog[k]
TraceMatched == <>(l = Len(TraceLog))
=============================================================================
