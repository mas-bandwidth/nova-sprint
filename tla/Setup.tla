----------------------------- MODULE Setup -----------------------------
EXTENDS Naturals, Sequences, FiniteSets

(***************************************************************************
 Eight-step resumable setup, LAYER-6-RUN section 5 and docs/SETUP.
 verified is evidence from THIS walk, not a durable completion bitmap.
 Restart always begins at step 1. World regressions are separate from the
 last successful probe: a regression after a probe cannot be magically
 detected, so NeverAhead means verified in this walk, not an instantaneous
 distributed transaction across all eight outside services.

 Answers represents completed durable writes to policy/nova-secrets. Ask
 below abstracts prompt AND successful durable save as one action. Therefore
 AskedOnce means no re-prompt AFTER a durable answer exists. A crash after
 human input but before storage may require input again; this abstraction
 cannot prove otherwise. No actual secret bytes are modeled: secret sinks
 are a closed set, and only the stdin-to-secret-store path is allowed.

 Once the finite disruptions are spent, fair execution repairs/verifies
 steps and reaches done. An unavailable external prerequisite or a human
 never supplying an answer is outside that liveness assumption.
***************************************************************************)
CONSTANT MaxDisruptions
Steps == 1..8
Questions == {"coordinator", "dashboard", "services", "key"}
Need(k) == CASE k = 3 -> {"coordinator"}
            [] k = 5 -> {"dashboard"}
            [] k = 6 -> {"services", "key"}
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
          /\ pc' = pc + 1 /\ phase' = IF pc = 8 THEN "done" ELSE "probe"
          /\ verifiedOnly' = (verifiedOnly /\ world[pc])
     ELSE /\ phase' = "repair" /\ UNCHANGED <<verified, pc, verifiedOnly>>
  /\ UNCHANGED <<world, answers, asked, disruptions, keySinks, orderOK, resumeOK>>
Ask(q) ==
  /\ phase = "repair" /\ q \in Need(pc) /\ q \notin answers
  /\ answers' = answers \cup {q}
  /\ asked' = [asked EXCEPT ![q] = @ + 1]
  /\ keySinks' = IF q = "key" THEN keySinks \cup {"secrets-stdin"} ELSE keySinks
  /\ UNCHANGED <<world, verified, pc, phase, disruptions, orderOK, verifiedOnly, resumeOK>>
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
Next == Progress \/ Crash \/ (\E k \in Steps : Regress(k))
Spec == Init /\ [][Next]_vars /\ WF_vars(Progress)
TypeOK == /\ world \in [Steps -> BOOLEAN] /\ verified \subseteq Steps
          /\ answers \subseteq Questions /\ asked \in [Questions -> 0..2]
          /\ pc \in 1..9 /\ phase \in {"probe", "repair", "done", "stopped"}
          /\ disruptions \in 0..MaxDisruptions
NeverAhead == orderOK /\ (phase # "stopped" => (1..(pc-1)) \subseteq verified)
OnlyVerified == verifiedOnly
AskedOnce == \A q \in Questions : asked[q] <= 1
KeyNeverPrinted == keySinks \subseteq {"secrets-stdin"}
RestartFromOne == resumeOK
EventuallyDone == []<>(phase = "done")
=============================================================================
