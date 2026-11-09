----------------------------- MODULE Setup -----------------------------
EXTENDS Naturals, Sequences, FiniteSets

(***************************************************************************
 Seven-step resumable setup, LAYER-6-RUN section 5 (modules/verbs/setup.go).
 verified is evidence from THIS walk, not a durable completion bitmap.
 Restart always begins at step 1. World regressions are separate from the
 last successful probe: a regression after a probe cannot be magically
 detected, so NeverAhead means verified in this walk, not an instantaneous
 distributed transaction across all the outside services.

 The one question is the commit identity (step 6): up asks the human and
 stops; the answer is stored by the identity verb, never by up. Ask below
 abstracts the human's answer AND its durable save as one action, so
 AskedOnce means no re-ask AFTER a durable answer exists. up saves no
 answer of its own: the coordinator is step 3's hook, the public board is
 nova-config's sprint-dashboard record (their worlds).

 Handoff is the up process that holds no sprint credential (UpStoreOnly):
 it stops once steps 1 and 2 verify, and the walk under the credential
 Restarts from step 1. No record passes between them.

 Once the finite disruptions are spent, fair execution repairs/verifies
 steps and reaches done. An unavailable external prerequisite or a human
 never supplying an answer is outside that liveness assumption.

 The workload steps, bound to cmd/nova-sprint/setup_workload.go by
 TestWorkloadStepsMatchTheSetupModel (every state of a two-bench fleet):
 world[6] is every worker bench on the bus and up in the store
 (WorkersReady); world[7] is world[6] and every worker bench with cores,
 so dealable once the sprint starts (SmokeReady; a fresh sprint is stopped
 until start, so up deals no card itself). Step 7 has no action. Do at 6
 writes the worker service records and applies them; world[6] becomes true
 only when the units come up, so an Act that returns before that maps to
 repair again, never to Do (SetupTrace-MAPPING.md).
***************************************************************************)
CONSTANT MaxDisruptions
Steps == 1..7
Questions == {"identity"}
Need(k) == IF k = 6 THEN {"identity"} ELSE {}
Actionable == Steps \ {7}
VARIABLES world, verified, answers, asked, pc, phase, disruptions,
          orderOK, verifiedOnly, resumeOK
vars == <<world, verified, answers, asked, pc, phase, disruptions,
          orderOK, verifiedOnly, resumeOK>>
Init == /\ world = [k \in Steps |-> FALSE]
        /\ verified = {} /\ answers = {} /\ asked = [q \in Questions |-> 0]
        /\ pc = 1 /\ phase = "probe" /\ disruptions = 0
        /\ orderOK = TRUE /\ verifiedOnly = TRUE /\ resumeOK = TRUE

\* Every invocation, including after a stop, a handoff or a completed walk, re-probes.
Restart ==
  /\ phase \in {"stopped", "handoff", "done"}
  /\ pc' = 1 /\ verified' = {} /\ phase' = "probe"
  /\ resumeOK' = (resumeOK /\ pc' = 1)
  /\ UNCHANGED <<world, answers, asked, disruptions, orderOK, verifiedOnly>>
Probe ==
  /\ phase = "probe" /\ pc \in Steps
  /\ (1..(pc-1)) \subseteq verified
  /\ IF world[pc] /\ Need(pc) \subseteq answers
     THEN /\ verified' = verified \cup {pc}
          /\ pc' = pc + 1 /\ phase' = IF pc = 7 THEN "done" ELSE "probe"
          /\ verifiedOnly' = (verifiedOnly /\ world[pc])
     ELSE /\ phase' = "repair" /\ UNCHANGED <<verified, pc, verifiedOnly>>
  /\ UNCHANGED <<world, answers, asked, disruptions, orderOK, resumeOK>>
\* A handoff is a stop of the first process, counted with the disruptions
\* (each up invocation hands off at most once).
Handoff ==
  /\ disruptions < MaxDisruptions
  /\ phase = "probe" /\ pc = 3 /\ {1, 2} \subseteq verified
  /\ phase' = "handoff" /\ disruptions' = disruptions + 1
  /\ UNCHANGED <<world, verified, answers, asked, pc, orderOK, verifiedOnly, resumeOK>>
Ask(q) ==
  /\ phase = "repair" /\ q \in Need(pc) /\ q \notin answers
  /\ answers' = answers \cup {q}
  /\ asked' = [asked EXCEPT ![q] = @ + 1]
  /\ UNCHANGED <<world, verified, pc, phase, disruptions, orderOK, verifiedOnly, resumeOK>>
Do ==
  /\ phase = "repair" /\ pc \in Actionable /\ Need(pc) \subseteq answers
  /\ (1..(pc-1)) \subseteq verified
  /\ world' = [world EXCEPT ![pc] = TRUE]
  /\ phase' = "probe"
  /\ orderOK' = (orderOK /\ (1..(pc-1)) \subseteq verified)
  /\ UNCHANGED <<verified, answers, asked, pc, disruptions, verifiedOnly, resumeOK>>
\* Step 7 has no action: up refuses and stops. What it needs is step 6's
\* workers and nova-config's cores, so the world makes it true when step 6
\* holds (a bench coming up, cores set), and the next up Restarts.
Settle ==
  /\ phase = "repair" /\ pc = 7
  /\ world' = [world EXCEPT ![7] = @ \/ world[6]] /\ phase' = "stopped"
  /\ verified' = {} /\ pc' = 1
  /\ UNCHANGED <<answers, asked, disruptions, orderOK, verifiedOnly, resumeOK>>
Regress(k) ==
  /\ disruptions < MaxDisruptions /\ world[k]
  /\ world' = [world EXCEPT ![k] = FALSE]
  /\ disruptions' = disruptions + 1
  /\ UNCHANGED <<verified, answers, asked, pc, phase, orderOK, verifiedOnly, resumeOK>>
Crash ==
  /\ disruptions < MaxDisruptions /\ phase \notin {"done", "stopped", "handoff"}
  /\ phase' = "stopped" /\ verified' = {} /\ pc' = 1
  /\ disruptions' = disruptions + 1
  /\ UNCHANGED <<world, answers, asked, orderOK, verifiedOnly, resumeOK>>
Progress == Restart \/ Probe \/ Do \/ Settle \/ (\E q \in Questions : Ask(q))
Next == Progress \/ Handoff \/ Crash \/ (\E k \in Steps : Regress(k))
Spec == Init /\ [][Next]_vars /\ WF_vars(Progress)
TypeOK == /\ world \in [Steps -> BOOLEAN] /\ verified \subseteq Steps
          /\ answers \subseteq Questions /\ asked \in [Questions -> 0..2]
          /\ pc \in 1..8 /\ phase \in {"probe", "repair", "handoff", "done", "stopped"}
          /\ disruptions \in 0..MaxDisruptions
NeverAhead == orderOK /\ (phase # "stopped" => (1..(pc-1)) \subseteq verified)
OnlyVerified == verifiedOnly
AskedOnce == \A q \in Questions : asked[q] <= 1
RestartFromOne == resumeOK
EventuallyDone == []<>(phase = "done")
=============================================================================
