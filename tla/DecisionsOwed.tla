--------------------------- MODULE DecisionsOwed ---------------------------
EXTENDS Naturals, FiniteSets
CONSTANT Broken
VARIABLES now, open, answered, applied, owed, ownerTold,
          coordSet, coordSends, ownerSet, ownerSends, ownerAt, defaultAt,
          action, ambiguous, faults
\* The owner's send the bus refuses (a recipient it does not know: 2026-10-17, "glenn is no
\* known name") waits before it is sent again (cmd/nova-sprint/coordinator_push.go
\* decisionSend, busRetry: a minute, doubling, up to thirty). refused is that wait running;
\* refusals counts the refusals, bounded: transport liveness assumes the roster is fixed in
\* the end. Reversed witnesses:
\*   "busyretry" the refused send goes again while its wait runs: NoRetryWithinWait.
\*   "noretry"   the wait never ends: EventuallyResolvedOrEscalated.
VARIABLES refused, refusals
refusalVars == <<refused, refusals>>
vars == <<now, open, answered, applied, owed, ownerTold,
          coordSet, coordSends, ownerSet, ownerSends, ownerAt, defaultAt,
          action, ambiguous, faults, refused, refusals>>
Judgments == {"bound", "reader", "merge"}
Deadline == 2
OwnerWindow == 2 \* each model interval is fifteen running minutes
Safe(j) == CASE j = "bound" -> "hold" [] j = "reader" -> "wait" [] OTHER -> "none"
Due(t, os) == IF t >= Deadline THEN os ELSE {}
Init == /\ now = 0 /\ open = Judgments /\ answered = {} /\ applied = {}
        /\ owed = {} /\ ownerTold = {}
        /\ coordSet = {} /\ coordSends = 0 /\ ownerSet = {} /\ ownerSends = 0
        /\ ownerAt = 0 /\ defaultAt = [j \in Judgments |-> 0]
        /\ action = [j \in Judgments |-> "none"] /\ ambiguous = FALSE /\ faults = 0
        /\ refused = FALSE /\ refusals = 0
\* At the deadline the server records an obligation, not a human sight claim.
Observe == /\ now < 5 /\ now' = now + 1
           /\ owed' = IF Broken = "silent" THEN owed ELSE owed \cup Due(now', open)
           /\ UNCHANGED <<open, answered, applied, ownerTold,
                          coordSet, coordSends, ownerSet, ownerSends, ownerAt,
                          defaultAt, action, ambiguous, faults>>
           /\ UNCHANGED refusalVars
OwnerDue == /\ owed # {} /\ ownerSends < 12
            /\ (ownerSends = 0 \/ now >= ownerAt + OwnerWindow
                \/ (Broken = "ownercadence" /\ ownerSet # owed))
            /\ (~refused \/ Broken = "busyretry")
DeliverOwner == /\ OwnerDue
                /\ ownerTold' = ownerTold \cup owed /\ ownerSet' = owed
                /\ ownerAt' = now /\ ownerSends' = ownerSends + 1
                /\ owed' = {} /\ ambiguous' = FALSE /\ refused' = FALSE
                /\ UNCHANGED <<now, open, answered, applied,
                               coordSet, coordSends, defaultAt, action, faults, refusals>>
\* The bus refuses the owner's send: nothing is told, the obligation stays owed, and the
\* send waits.
RefuseOwner == /\ OwnerDue /\ refusals < 2
               /\ refused' = TRUE /\ refusals' = refusals + 1
               /\ UNCHANGED <<now, open, answered, applied, owed, ownerTold,
                              coordSet, coordSends, ownerSet, ownerSends, ownerAt,
                              defaultAt, action, ambiguous, faults>>
\* The wait runs out (at most thirty minutes): the send may go again.
WaitEnds == /\ refused /\ Broken # "noretry" /\ refused' = FALSE
            /\ UNCHANGED <<now, open, answered, applied, owed, ownerTold,
                           coordSet, coordSends, ownerSet, ownerSends, ownerAt,
                           defaultAt, action, ambiguous, faults, refusals>>
DeliverCoordinator == /\ Due(now, open) # {} /\ coordSends < 12
                      /\ (Due(now, open) # coordSet \/ ambiguous)
                      /\ coordSet' = Due(now, open) /\ coordSends' = coordSends + 1
                      /\ ambiguous' = FALSE
                      /\ UNCHANGED <<now, open, answered, applied,
                                     owed, ownerTold, ownerSet, ownerSends, ownerAt,
                                     defaultAt, action, faults>>
                      /\ UNCHANGED refusalVars
\* One bounded ambiguity witnesses renewed-token at-least-once retry. The live
\* receiver sees the same judgment IDs; retry never changes sprint state.
AmbiguousSend == /\ owed # {} /\ faults = 0 /\ ambiguous' = TRUE /\ faults' = 1
                 /\ UNCHANGED <<now, open, answered, applied, owed,
                                ownerTold, coordSet, coordSends, ownerSet, ownerSends,
                                ownerAt, defaultAt, action>>
                 /\ UNCHANGED refusalVars
Default(j) == /\ j \in open \ applied /\ now >= 2 * Deadline
              /\ (Safe(j) # "none" \/ Broken = "unsafe")
              /\ open' = IF Safe(j) = "hold" THEN open ELSE open \ {j}
              /\ applied' = applied \cup {j}
              /\ defaultAt' = [defaultAt EXCEPT ![j] = now]
              /\ action' = [action EXCEPT ![j] = IF Broken = "unsafe" THEN "merge" ELSE Safe(j)]
              /\ owed' = owed \cup {j}
              /\ UNCHANGED <<now, answered, ownerTold, coordSet, coordSends,
                             ownerSet, ownerSends, ownerAt, ambiguous, faults>>
              /\ UNCHANGED refusalVars
Answer(j) == /\ j \in open /\ open' = open \ {j} /\ answered' = answered \cup {j}
             /\ UNCHANGED <<now, applied, owed, ownerTold, coordSet,
                            coordSends, ownerSet, ownerSends, ownerAt, defaultAt,
                            action, ambiguous, faults>>
             /\ UNCHANGED refusalVars
Next == Observe \/ DeliverOwner \/ DeliverCoordinator \/ AmbiguousSend
        \/ RefuseOwner \/ WaitEnds
        \/ (\E j \in Judgments : Default(j) \/ Answer(j))
Spec == Init /\ [][Next]_vars /\ WF_vars(Observe) /\ WF_vars(DeliverOwner) /\ WF_vars(WaitEnds)
TypeOK == /\ now \in 0..5 /\ open \subseteq Judgments /\ applied \subseteq Judgments
          /\ answered \subseteq Judgments /\ owed \subseteq Judgments
          /\ ownerTold \subseteq Judgments
          /\ refused \in BOOLEAN /\ refusals \in 0..2
NoSilentPile == \A j \in Due(now, open) : j \in owed \cup ownerTold
DefaultIsSafe == \A j \in applied : action[j] \in {"wait", "hold", "rest", "retier", "release"}
DefaultOnlyPastTwice == \A j \in applied : defaultAt[j] >= 2 * Deadline
\* Healthy acknowledged normal cadence; explicit ambiguous retry is permitted
\* by the owner's 2026-10-15 12:01:51 ruling. Notifications never mutate work.
NoResendSameSet == [][coordSends' > coordSends => coordSet' # coordSet \/ ambiguous]_vars
OwnerCadence == [][ownerSends' > ownerSends => ownerSends = 0 \/ now >= ownerAt + OwnerWindow]_vars
\* A refused send is never sent again, delivered or refused, while its wait runs.
NoRetryWithinWait == [][(ownerSends' > ownerSends \/ refusals' > refusals) => ~refused]_vars
ParkedRemainsOwed == "bound" \in applied \ answered => "bound" \in open
EventuallyResolvedOrEscalated == \A j \in Judgments : <>(j \in answered \cup applied \cup ownerTold)
=============================================================================
