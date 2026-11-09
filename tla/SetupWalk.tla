---------------------------- MODULE SetupWalk ----------------------------
EXTENDS Setup
CONSTANTS InitialReady, InitialAnswers

\* Explicit initial-condition contract for an actual walk: earlier walks may
\* have made steps and the identity durable. Verification and counters start
\* with this execution. Setup.Init and Next stay unchanged.
WalkInit ==
  /\ world \in [Steps -> BOOLEAN]
  /\ {k \in Steps : world[k]} \subseteq InitialReady
  /\ answers \subseteq InitialAnswers
  /\ verified = {} /\ asked = [q \in Questions |-> 0]
  /\ pc = 1 /\ phase = "probe" /\ disruptions = 0
  /\ orderOK = TRUE /\ verifiedOnly = TRUE /\ resumeOK = TRUE
WalkSpec == WalkInit /\ [][Next]_vars /\ WF_vars(Progress)
=============================================================================
