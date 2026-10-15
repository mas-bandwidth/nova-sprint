--------------------------- MODULE DecisionsOwed ---------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Broken
VARIABLES now, open, answered, applied, hookProven, owed, ownerTold,
          coordSet, coordSends, ownerSet, ownerSends, ownerAt, defaultAt,
          action, ambiguous, faults
vars == <<now, open, answered, applied, hookProven, owed, ownerTold,
          coordSet, coordSends, ownerSet, ownerSends, ownerAt, defaultAt,
          action, ambiguous, faults>>
Judgments == {"bound", "reader", "merge"}
Deadline == 2
Safe(j) == CASE j = "bound" -> "hold" [] j = "reader" -> "wait" [] OTHER -> "none"
Due(t, os) == IF t >= Deadline THEN os ELSE {}
Init == /\ now = 0 /\ open = Judgments /\ answered = {} /\ applied = {}
        /\ hookProven \in BOOLEAN /\ owed = {} /\ ownerTold = {}
        /\ coordSet = {} /\ coordSends = 0 /\ ownerSet = {} /\ ownerSends = 0
        /\ ownerAt = 0 /\ defaultAt = [j \in Judgments |-> 0]
        /\ action = [j \in Judgments |-> "none"] /\ ambiguous = FALSE /\ faults = 0
\* At the deadline the server records an obligation, not a human sight claim.
Observe == /\ now < 5 /\ now' = now + 1
           /\ owed' = IF Broken = "silent" THEN owed ELSE owed \cup Due(now', open)
           /\ UNCHANGED <<open, answered, applied, hookProven, ownerTold,
                          coordSet, coordSends, ownerSet, ownerSends, ownerAt,
                          defaultAt, action, ambiguous, faults>>
DeliverOwner == /\ owed # {} /\ ownerSends < 12
                /\ (ownerSet # owed \/ now > ownerAt \/ ambiguous)
                /\ ownerTold' = ownerTold \cup owed /\ ownerSet' = owed
                /\ ownerAt' = now /\ ownerSends' = ownerSends + 1
                /\ owed' = {} /\ ambiguous' = FALSE
                /\ UNCHANGED <<now, open, answered, applied, hookProven,
                               coordSet, coordSends, defaultAt, action, faults>>
DeliverCoordinator == /\ Due(now, open) # {} /\ coordSends < 12
                      /\ (Due(now, open) # coordSet \/ ambiguous)
                      /\ coordSet' = Due(now, open) /\ coordSends' = coordSends + 1
                      /\ ambiguous' = FALSE
                      /\ UNCHANGED <<now, open, answered, applied, hookProven,
                                     owed, ownerTold, ownerSet, ownerSends, ownerAt,
                                     defaultAt, action, faults>>
\* One bounded ambiguity witnesses renewed-token at-least-once retry. The live
\* receiver sees the same judgment IDs; retry never changes sprint state.
AmbiguousSend == /\ owed # {} /\ faults = 0 /\ ambiguous' = TRUE /\ faults' = 1
                 /\ UNCHANGED <<now, open, answered, applied, hookProven, owed,
                                ownerTold, coordSet, coordSends, ownerSet, ownerSends,
                                ownerAt, defaultAt, action>>
Default(j) == /\ j \in open /\ now >= 2 * Deadline
              /\ (Safe(j) # "none" \/ Broken = "unsafe")
              /\ open' = open \ {j} /\ applied' = applied \cup {j}
              /\ defaultAt' = [defaultAt EXCEPT ![j] = now]
              /\ action' = [action EXCEPT ![j] = IF Broken = "unsafe" THEN "merge" ELSE Safe(j)]
              /\ owed' = owed \cup {j}
              /\ UNCHANGED <<now, answered, hookProven, ownerTold, coordSet, coordSends,
                             ownerSet, ownerSends, ownerAt, ambiguous, faults>>
Answer(j) == /\ j \in open /\ open' = open \ {j} /\ answered' = answered \cup {j}
             /\ UNCHANGED <<now, applied, hookProven, owed, ownerTold, coordSet,
                            coordSends, ownerSet, ownerSends, ownerAt, defaultAt,
                            action, ambiguous, faults>>
Hook == /\ hookProven' = ~hookProven
        /\ UNCHANGED <<now, open, answered, applied, owed, ownerTold, coordSet,
                       coordSends, ownerSet, ownerSends, ownerAt, defaultAt,
                       action, ambiguous, faults>>
Next == Observe \/ DeliverOwner \/ DeliverCoordinator \/ AmbiguousSend \/ Hook
        \/ (\E j \in Judgments : Default(j) \/ Answer(j))
Spec == Init /\ [][Next]_vars /\ WF_vars(Observe) /\ WF_vars(DeliverOwner)
TypeOK == /\ now \in 0..5 /\ open \subseteq Judgments /\ applied \subseteq Judgments
          /\ answered \subseteq Judgments /\ owed \subseteq Judgments
          /\ ownerTold \subseteq Judgments /\ hookProven \in BOOLEAN
NoSilentPile == \A j \in Due(now, open) : j \in owed \cup ownerTold
DefaultIsSafe == \A j \in applied : action[j] \in {"wait", "hold", "rest", "retier", "release"}
DefaultOnlyPastTwice == \A j \in applied : defaultAt[j] >= 2 * Deadline
\* Healthy acknowledged normal cadence; explicit ambiguous retry is permitted
\* by the owner's 2026-10-15 12:01:51 ruling. Notifications never mutate work.
NoResendSameSet == [][coordSends' > coordSends => coordSet' # coordSet \/ ambiguous]_vars
EventuallyResolvedOrEscalated == \A j \in Judgments : <>(j \in answered \cup applied \cup ownerTold)
=============================================================================
