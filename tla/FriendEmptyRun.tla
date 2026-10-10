------------------------ MODULE FriendEmptyRun ------------------------
EXTENDS Integers, FiniteSets
\* docs/SPEC-SPRINT.md section 8, the rules table's row failed: an attempt a
\* friend's lane ran empty (it wrote no report: ClassEmptyRun,
\* internal/sprint/harness_fault.go) is reworked by the failed rule (ruleHarness)
\* and never placed again on the friend whose lane ran it empty: the rule writes
\* her on the primary's friends_left (emptyRunLeft). Every way a ready attempt
\* reaches a friend reads it: the friends' deal (friend_deal.go cardLeft), and
\* the rebalance of an attempt a machine was dealt (steps_work.go deal copies the
\* primary's friends_left onto the machine's work card; rebalance.go rebalanceTo
\* reads cardLeft). A card that names its friend (WHO: friend <name>) is hers
\* alone after a rework (ReworkPinned) and is never left. One card, two friends
\* and one machine, attempts up to MaxAttempts. 2026-10-09/10: a friend's
\* opencode lanes ran 46 attempts empty and the rework dealt each straight back.
\*
\* BadNoLeft: the rule as it was, which left no one. BadMachineLeft: the rule
\* leaves her, but the machine's attempt and the rebalance read only the work
\* card's own friends_left (the gap the cold reads of nova-sprint #45 found).
CONSTANTS MaxAttempts, Named, BadNoLeft, BadMachineLeft
Friends == {"amy", "bob"}
Machine == "m1"
None == "none"
VARIABLES place, state, attempt, left, wleft, emptyOn
vars == <<place, state, attempt, left, wleft, emptyOn>>

\* left: the primary's friends_left; wleft: the current work card's
Init == /\ place = None /\ state = "ready" /\ attempt = 0
        /\ left = {} /\ wleft = {} /\ emptyOn = {}

\* the friends' deal: a ready card to a friend it has not left (cardLeft reads
\* the primary's); a named card to its friend alone; the work card carries left
Deal(f) == /\ state = "ready" /\ attempt < MaxAttempts
           /\ f \notin left
           /\ (Named => f = "amy")
           /\ place' = f /\ state' = "queued" /\ attempt' = attempt + 1
           /\ wleft' = left
           /\ UNCHANGED <<left, emptyOn>>

\* the machines' deal of an unnamed card: the work card carries the primary's
\* friends_left (BadMachineLeft: it does not)
MachineDeal == /\ state = "ready" /\ attempt < MaxAttempts /\ ~Named
               /\ place' = Machine /\ state' = "queued" /\ attempt' = attempt + 1
               /\ wleft' = IF BadMachineLeft THEN {} ELSE left
               /\ UNCHANGED <<left, emptyOn>>

\* the rebalance moves a queued attempt off the machine to a friend's idle lane,
\* never to one the card has left (BadMachineLeft: the work card's list alone)
Rebalance(f) == /\ state = "queued" /\ place = Machine
                /\ f \notin (IF BadMachineLeft THEN wleft ELSE wleft \cup left)
                /\ place' = f
                /\ UNCHANGED <<state, attempt, left, wleft, emptyOn>>

\* a lane takes the queued attempt
Start == /\ state = "queued" /\ state' = "working"
         /\ UNCHANGED <<place, attempt, left, wleft, emptyOn>>

\* the lane ends the attempt: ok, or empty (no report; only a friend's lane in
\* this model, a machine's rework avoids its member already: reworkAvoid)
FinishOk == /\ state = "working"
            /\ state' = "done" /\ place' = None
            /\ UNCHANGED <<attempt, left, wleft, emptyOn>>
FinishEmpty == /\ state = "working" /\ place \in Friends
               /\ state' = "review" /\ emptyOn' = emptyOn \cup {place}
               /\ UNCHANGED <<place, attempt, left, wleft>>

\* the failed rule's rework: an empty run leaves the friend unless the card
\* names her (BadNoLeft: no one is left)
Rework == /\ state = "review"
          /\ state' = "ready" /\ place' = None
          /\ left' = IF Named \/ BadNoLeft THEN left ELSE left \cup {place}
          /\ UNCHANGED <<attempt, wleft, emptyOn>>

Next == \/ \E f \in Friends : Deal(f) \/ Rebalance(f)
        \/ MachineDeal \/ Start
        \/ FinishOk \/ FinishEmpty \/ Rework
Spec == Init /\ [][Next]_vars

TypeOK == /\ place \in Friends \cup {Machine, None}
          /\ state \in {"ready", "queued", "working", "review", "done"}
          /\ attempt \in 0..MaxAttempts
          /\ left \subseteq Friends /\ wleft \subseteq Friends
          /\ emptyOn \subseteq Friends
\* an any-friend card is never queued or working on a friend whose lane ran it
\* empty, however it got there
NeverBackToTheEmptyLane ==
    (~Named /\ state \in {"queued", "working"}) => place \notin emptyOn
\* a named card is never left by its friend: it is not stranded ready
NamedNeverLeft == Named => left = {}
\* reach witness for the Named instance: its friend runs it empty and it is
\* dealt to her again (NeverReDealtNamed fails there, so the instance is not vacuous)
NeverReDealtNamed == ~(Named /\ state = "queued" /\ place \in emptyOn)
=============================================================================
