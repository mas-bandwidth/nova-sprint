----------------------------- MODULE Setup -----------------------------
EXTENDS Naturals, Sequences, FiniteSets

(***************************************************************************
 Ten-step resumable setup, LAYER-6-RUN section 5 and docs/SETUP.
 verified is evidence from THIS walk, not a durable completion bitmap.
 Restart always begins at step 1. World regressions are separate from the
 last successful probe: a regression after a probe cannot be magically
 detected, so NeverAhead means verified in this walk, not an instantaneous
 distributed transaction across all ten outside services.

 Answers represents completed durable writes to policy/nova-secrets. Ask
 below abstracts prompt AND successful durable save as one action. Therefore
 AskedOnce means no re-prompt AFTER a durable answer exists. A crash after
 human input but before storage may require input again; this abstraction
 cannot prove otherwise. No actual secret bytes are modeled: secret sinks
 are a closed set, and only the stdin-to-secret-store path is allowed.

 An answer can be given again: an explicit flag on the command line wins
 over the saved value and is saved as a newer version (Choices.Effective,
 Choices.Save). The model keeps which questions are answered, not their
 values, so Reanswer leaves the state unchanged; it counts no prompt, which
 keeps AskedOnce.

 Once the finite disruptions are spent, fair execution repairs/verifies
 steps and reaches done. An unavailable external prerequisite or a human
 never supplying an answer is outside that liveness assumption.

 The workload steps, bound to cmd/nova-sprint/setup_workload.go by
 TestWorkloadStepsMatchTheSetupModel (every state of a two-bench fleet):
 world[StepWorkers] is every worker bench on the bus, up in the store and
 its worker process this build, by the build the process itself beats
 (WorkersReady; Do restarts, once, a process of an older build, and a
 process not yet beaten is waited for, never restarted); world[StepSmoke]
 is world[StepWorkers] and every worker bench with cores, so dealable
 once the sprint starts (SmokeReady; a fresh sprint is stopped until
 start, so up deals no card itself). Do at
 StepWorkers writes the worker service records and applies them;
 world[StepWorkers] becomes true only when the units come up, so an Act
 that returns before that maps to repair again, never to Do
 (SetupTrace-MAPPING.md).

 world[StepRun] likewise holds only while the run holding the chooser lock
 is this build (its lock names it, verbs.PGSetupRun); Do at StepRun restarts
 a run of an older build through the same services play.

 The play steps (PlaySteps), bound to modules/verbs/setup_runtime_plays.go
 by TestFleetPlayStepsMatchTheSetupModel (every state of a two-bench
 fleet): world[k] is nova-fleet's play in check mode reporting changed=0,
 unreachable=0 and failed=0 on every bench; a check that ran and found a
 change is not verified (the SetupPlayCheckRan witness counts it and
 breaks OnlyVerified).
***************************************************************************)
CONSTANT MaxDisruptions
\* up's steps by name, as modules/verbs/setup.go numbers them: the fleet,
\* nova-fleet's two plays every bench needs (runner-packages, then netwatch),
\* then the sprint's own setup from the store up.
StepTools == 1
StepPackages == 2
StepNetwatch == 3
StepStore == 4
StepCoordinator == 5
StepRun == 6
StepDashboard == 7
StepServices == 8
StepWorkers == 9
StepSmoke == 10
LastStep == StepSmoke
\* A play step's world is nova-fleet <play> --check reporting nothing to
\* change, nothing unreachable and nothing failed on every bench; its Do
\* is the play itself (FleetPlays in modules/verbs/setup_runtime_plays.go).
PlaySteps == {StepPackages, StepNetwatch}
Steps == 1..LastStep
Questions == {"coordinator", "dashboard", "services", "key"}
Need(k) == CASE k = StepCoordinator -> {"coordinator"}
            [] k = StepDashboard -> {"dashboard"}
            [] k = StepServices -> {"services", "key"}
            [] OTHER -> {}
VARIABLES world, verified, answers, asked, pc, phase, disruptions,
          keySinks, orderOK, verifiedOnly, resumeOK
vars == <<world, verified, answers, asked, pc, phase, disruptions,
          keySinks, orderOK, verifiedOnly, resumeOK>>
Init == /\ world = [k \in Steps |-> FALSE]
        /\ verified = {} /\ answers = {} /\ asked = [q \in Questions |-> 0]
        /\ pc = 1 /\ phase = "probe" /\ disruptions = 0
        /\ keySinks = {} /\ orderOK = TRUE /\ verifiedOnly = TRUE /\ resumeOK = TRUE

\* Every invocation, including after crash or a completed walk, re-probes.
Restart ==
  /\ phase \in {"stopped", "done"}
  /\ pc' = 1 /\ verified' = {} /\ phase' = "probe"
  /\ resumeOK' = (resumeOK /\ pc' = 1)
  /\ UNCHANGED <<world, answers, asked, disruptions, keySinks, orderOK, verifiedOnly>>
Probe ==
  /\ phase = "probe" /\ pc \in Steps
  /\ (1..(pc-1)) \subseteq verified
  /\ IF world[pc] /\ Need(pc) \subseteq answers
     THEN /\ verified' = verified \cup {pc}
          /\ pc' = pc + 1 /\ phase' = IF pc = LastStep THEN "done" ELSE "probe"
          /\ verifiedOnly' = (verifiedOnly /\ world[pc])
     ELSE /\ phase' = "repair" /\ UNCHANGED <<verified, pc, verifiedOnly>>
  /\ UNCHANGED <<world, answers, asked, disruptions, keySinks, orderOK, resumeOK>>
Ask(q) ==
  /\ phase = "repair" /\ q \in Need(pc) /\ q \notin answers
  /\ answers' = answers \cup {q}
  /\ asked' = [asked EXCEPT ![q] = @ + 1]
  /\ keySinks' = IF q = "key" THEN keySinks \cup {"secrets-stdin"} ELSE keySinks
  /\ UNCHANGED <<world, verified, pc, phase, disruptions, orderOK, verifiedOnly, resumeOK>>
Reanswer(q) ==
  /\ phase = "repair" /\ q \in Need(pc) /\ q \in answers
  /\ UNCHANGED vars
Do ==
  /\ phase = "repair" /\ Need(pc) \subseteq answers
  /\ (1..(pc-1)) \subseteq verified
  /\ world' = [world EXCEPT ![pc] = TRUE]
  /\ phase' = "probe"
  /\ orderOK' = (orderOK /\ (1..(pc-1)) \subseteq verified)
  /\ UNCHANGED <<verified, answers, asked, pc, disruptions, keySinks, verifiedOnly, resumeOK>>
Regress(k) ==
  /\ disruptions < MaxDisruptions /\ world[k]
  /\ world' = [world EXCEPT ![k] = FALSE]
  /\ disruptions' = disruptions + 1
  /\ UNCHANGED <<verified, answers, asked, pc, phase, keySinks, orderOK, verifiedOnly, resumeOK>>
Crash ==
  /\ disruptions < MaxDisruptions /\ phase \notin {"done", "stopped"}
  /\ phase' = "stopped" /\ verified' = {} /\ pc' = 1
  /\ disruptions' = disruptions + 1
  /\ UNCHANGED <<world, answers, asked, keySinks, orderOK, verifiedOnly, resumeOK>>
Progress == Restart \/ Probe \/ Do \/ (\E q \in Questions : Ask(q))
Next == Progress \/ (\E q \in Questions : Reanswer(q)) \/ Crash \/ (\E k \in Steps : Regress(k))
Spec == Init /\ [][Next]_vars /\ WF_vars(Progress)
TypeOK == /\ world \in [Steps -> BOOLEAN] /\ verified \subseteq Steps
          /\ answers \subseteq Questions /\ asked \in [Questions -> 0..2]
          /\ pc \in 1..(LastStep+1) /\ phase \in {"probe", "repair", "done", "stopped"}
          /\ disruptions \in 0..MaxDisruptions
NeverAhead == orderOK /\ (phase # "stopped" => (1..(pc-1)) \subseteq verified)
OnlyVerified == verifiedOnly
AskedOnce == \A q \in Questions : asked[q] <= 1
KeyNeverPrinted == keySinks \subseteq {"secrets-stdin"}
RestartFromOne == resumeOK
EventuallyDone == []<>(phase = "done")
=============================================================================
