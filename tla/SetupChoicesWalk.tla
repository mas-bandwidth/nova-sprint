---------------------------- MODULE SetupChoicesWalk ----------------------------
EXTENDS SetupChoices
CONSTANTS InitialReady, InitialAnswers

\* Explicit initial-condition contract for an actual under-lock walk.
\* Bootstrap may have made prerequisites/answers durable. Verification and
\* counters start with this execution. SetupChoices.Init is separate; Next is unchanged.
WalkInit ==
  /\ world \in [Steps -> BOOLEAN]
  /\ {k \in Steps : world[k]} \subseteq InitialReady
  /\ answers \subseteq InitialAnswers
  /\ servicesChoice \in {"unknown","none","configured"}
  /\ (("services" \in answers) = (servicesChoice # "unknown"))
  /\ verified = {} /\ asked = [q \in Questions |-> 0]
  /\ pc = 1 /\ phase = "probe" /\ disruptions = 0
  /\ keySinks = {} /\ orderOK = TRUE /\ verifiedOnly = TRUE /\ resumeOK = TRUE
WalkSpec == WalkInit /\ [][Next]_vars /\ WF_vars(Progress)
=============================================================================
