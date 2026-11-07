----------------------------- MODULE SetupChoicesReprompt -----------------------------
EXTENDS Naturals, Sequences, FiniteSets

(***************************************************************************
 Explicit durable-choice contract. Old Setup evidence remains unchanged.
 KeyQuestions identifies provider keys separately. None discharges the key
 obligation without inventing a saved key or prompt. Ask samples prompt plus
 successful save; failures inside that pair require a finer model. Supply and
 unprompted ChooseServices record actual CLI/default saves with zero prompts.
***************************************************************************)
CONSTANTS MaxDisruptions, KeyQuestions
Steps == 1..8
Questions == {"coordinator", "dashboard", "services"} \cup KeyQuestions
VARIABLE servicesChoice
Need(k) == CASE k = 3 -> {"coordinator"}
            [] k = 5 -> {"dashboard"}
            [] k = 6 -> {"services"} \cup (IF servicesChoice = "none" THEN {} ELSE KeyQuestions)
            [] OTHER -> {}
VARIABLES world, verified, answers, asked, pc, phase, disruptions,
          keySinks, orderOK, verifiedOnly, resumeOK
vars == <<servicesChoice, world, verified, answers, asked, pc, phase, disruptions,
          keySinks, orderOK, verifiedOnly, resumeOK>>
Init == /\ servicesChoice = "unknown"
        /\ world = [k \in Steps |-> FALSE]
        /\ verified = {} /\ answers = {} /\ asked = [q \in Questions |-> 0]
        /\ pc = 1 /\ phase = "probe" /\ disruptions = 0
        /\ keySinks = {} /\ orderOK = TRUE /\ verifiedOnly = TRUE /\ resumeOK = TRUE

\* Every invocation, including after crash or a completed walk, re-probes.
Restart ==
  /\ phase \in {"stopped", "done"}
  /\ pc' = 1 /\ verified' = {} /\ phase' = "probe"
  /\ resumeOK' = (resumeOK /\ pc' = 1)
  /\ UNCHANGED <<servicesChoice, world, answers, asked, disruptions, keySinks, orderOK, verifiedOnly>>
Probe ==
  /\ phase = "probe" /\ pc \in Steps
  /\ (1..(pc-1)) \subseteq verified
  /\ IF world[pc] /\ Need(pc) \subseteq answers
     THEN /\ verified' = verified \cup {pc}
          /\ pc' = pc + 1 /\ phase' = IF pc = 8 THEN "done" ELSE "probe"
          /\ verifiedOnly' = (verifiedOnly /\ world[pc])
     ELSE /\ phase' = "repair" /\ UNCHANGED <<servicesChoice, verified, pc, verifiedOnly>>
  /\ UNCHANGED <<servicesChoice, world, answers, asked, disruptions, keySinks, orderOK, resumeOK>>
Ask(q) ==
  /\ phase = "repair" /\ q \in Need(pc)
  /\ q # "services"
  /\ q \in KeyQuestions => servicesChoice = "configured"
  /\ answers' = answers \cup {q}
  /\ asked' = [asked EXCEPT ![q] = @ + 1]
  /\ keySinks' = IF q \in KeyQuestions THEN keySinks \cup {"secrets-stdin"} ELSE keySinks
  /\ UNCHANGED <<servicesChoice, world, verified, pc, phase, disruptions, orderOK, verifiedOnly, resumeOK>>
Do ==
  /\ phase = "repair" /\ Need(pc) \subseteq answers
  /\ (1..(pc-1)) \subseteq verified
  /\ world' = [world EXCEPT ![pc] = TRUE]
  /\ phase' = "probe"
  /\ orderOK' = (orderOK /\ (1..(pc-1)) \subseteq verified)
  /\ UNCHANGED <<servicesChoice, verified, answers, asked, pc, disruptions, keySinks, verifiedOnly, resumeOK>>
Regress(k) ==
  /\ disruptions < MaxDisruptions /\ world[k]
  /\ world' = [world EXCEPT ![k] = FALSE]
  /\ disruptions' = disruptions + 1
  /\ UNCHANGED <<servicesChoice, verified, answers, asked, pc, phase, keySinks, orderOK, verifiedOnly, resumeOK>>
Crash ==
  /\ disruptions < MaxDisruptions /\ phase \notin {"done", "stopped"}
  /\ phase' = "stopped" /\ verified' = {} /\ pc' = 1
  /\ disruptions' = disruptions + 1
  /\ UNCHANGED <<servicesChoice, world, answers, asked, keySinks, orderOK, verifiedOnly, resumeOK>>
Supply(q) ==
  /\ phase = "repair" /\ q \in Need(pc) /\ q \notin answers
  /\ q \in {"coordinator", "dashboard"}
  /\ answers' = answers \cup {q}
  /\ UNCHANGED <<servicesChoice, world, verified, asked, pc, phase, disruptions,
       keySinks, orderOK, verifiedOnly, resumeOK>>
ChooseServices(value, prompted) ==
  /\ phase = "repair" /\ pc = 6 /\ "services" \notin answers
  /\ value \in {"none","configured"} /\ prompted \in BOOLEAN
  /\ servicesChoice' = value /\ answers' = answers \cup {"services"}
  /\ asked' = [asked EXCEPT !["services"] = @ + IF prompted THEN 1 ELSE 0]
  /\ UNCHANGED <<world, verified, pc, phase, disruptions, keySinks,
       orderOK, verifiedOnly, resumeOK>>
Progress == Restart \/ Probe \/ Do \/ (\E q \in Questions : Ask(q) \/ Supply(q))
         \/ (\E value \in {"none","configured"}, prompted \in BOOLEAN : ChooseServices(value,prompted))
Next == Progress \/ Crash \/ (\E k \in Steps : Regress(k))
Spec == Init /\ [][Next]_vars /\ WF_vars(Progress)
TypeOK == /\ servicesChoice \in {"unknown","none","configured"}
          /\ (("services" \in answers) = (servicesChoice # "unknown"))
          /\ world \in [Steps -> BOOLEAN] /\ verified \subseteq Steps
          /\ answers \subseteq Questions /\ asked \in [Questions -> 0..2]
          /\ pc \in 1..9 /\ phase \in {"probe", "repair", "done", "stopped"}
          /\ disruptions \in 0..MaxDisruptions
NeverAhead == orderOK /\ (phase # "stopped" => (1..(pc-1)) \subseteq verified)
OnlyVerified == verifiedOnly
NoKeyPromptForNone == servicesChoice = "none" => \A q \in KeyQuestions : asked[q] = 0
AskedOnce == \A q \in Questions : asked[q] <= 1
KeyNeverPrinted == keySinks \subseteq {"secrets-stdin"}
RestartFromOne == resumeOK
EventuallyDone == []<>(phase = "done")
=============================================================================
