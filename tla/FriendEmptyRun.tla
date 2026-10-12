------------------------ MODULE FriendEmptyRun ------------------------
EXTENDS Integers, FiniteSets
\* docs/SPEC-SPRINT.md section 8, the rules table's row failed: an attempt a
\* friend's lane ran empty (it wrote no report: ClassEmptyRun,
\* internal/sprint/harness_fault.go) is reworked by the failed rule (ruleHarness)
\* and never placed again on the friend whose lane ran it empty: the rule writes
\* her on the primary's friends_left, on a rework below the brief's bound and on
\* the early return at it (left to a mind); the failed finish itself records it
\* too (steps_work.go, emptyRunFriend), so no path loses her. Every way a ready attempt
\* reaches a friend reads it: the friends' deal (friend_deal.go cardLeft), the
\* rebalance of an attempt a machine was dealt (steps_work.go deal copies the
\* primary's friends_left onto the machine's work card; rebalance.go rebalanceTo
\* reads cardLeft), the attempt cap's default answer (brief_bound.go
\* AttemptCapDeal, friend_deal.go friendWithFree), which deals a card at its
\* brief's bound to a friend working on her row and pins it to her
\* (briefGainsWho), and the four friend moves that read cardLeft too: the
\* friends' level (friend_level.go friendLevel), the start bound's level
\* (friend_start.go friendUnstartedLevel), the deal's reclaim (friend_deal.go
\* reclaimUnit) and the coordinator's could-take (coordinator_pass.go
\* friendCouldTake). The failed rule's answer also writes the friend on a brief
\* defect, its other early return (harness_fault.go). A card that names its
\* friend (WHO: friend <name>), or the cap pinned, is hers alone after a rework
\* (ReworkPinned) and is never left. One card, two friends and one machine;
\* attempts below Cap by the deals, from Cap to MaxAttempts by the cap's answer.
\* 2026-10-09/10: a friend's opencode lanes ran 46 attempts empty and the rework
\* dealt each straight back.
\*
\* BadNoLeft: the rule as it was, which left no one (the finish records none
\* either: the oldest bug). BadMachineLeft: the rule leaves her, but the machine's
\* attempt and the rebalance read only the work card's own friends_left (the cold
\* reads of nova-sprint #45 at b5b2d8a). BadCapLeft: the cap's answer picks a
\* friend without reading friends_left (the cold read of nova-sprint #45 at
\* f41a84d). BadRuleLeft: the finish records none and the failed rule's answer
\* records her only on a rework below the brief's bound, never on the early return
\* at it (the cold reads of nova-sprint #45 at f8a45086; the reversed witness of
\* the early-return write). BadLevelLeft: the four friend moves and the friends'
\* deal that feeds them read only the work card's own friends_left (the deal did
\* not copy the primary's onto it), so a friend the primary left is not seen.
\* FleetCard: the card has no WHO line, so the cap's answer may deal it
\* (AttemptCapDeal skips a friend's card); FALSE is a WHO: friend card.
\* 2026-10-12 (the owner: "max attempts before you do judgement and decide what to do"):
\* the cap's own deal is retired; a card at the cap is dealt to a friend only after the
\* coordinator's decision (Decide: a re-tier), which opens a fresh window of Cap attempts
\* (since: the attempts since the last decision). BadCapUndecided is the deal as it was.
CONSTANTS MaxAttempts, Cap, Named, FleetCard, BadNoLeft, BadMachineLeft, BadCapLeft, BadRuleLeft, BadLevelLeft, BriefDefect, BadCapUndecided
Friends == {"amy", "bob"}
Machine == "m1"
None == "none"
VARIABLES place, state, attempt, left, wleft, pinned, emptyOn, emptyUnpinned, lastEmpty, decided, since
vars == <<place, state, attempt, left, wleft, pinned, emptyOn, emptyUnpinned, lastEmpty, decided, since>>

\* left: the primary's friends_left; wleft: the current work card's; pinned:
\* the friend the cap's answer wrote into the brief; emptyOn: every friend whose
\* lane ran it empty; emptyUnpinned: those whose empty run left her (before any
\* pin), which the invariant is about
Init == /\ place = None /\ state = "ready" /\ attempt = 0
        /\ left = {} /\ wleft = {} /\ pinned = None
        /\ emptyOn = {} /\ emptyUnpinned = {} /\ lastEmpty = FALSE
        /\ decided = FALSE /\ since = 0

\* the friends' deal: a ready card to a friend it has not left (cardLeft reads
\* the primary's); a named card to its friend alone; the work card carries left
Deal(f) == /\ state = "ready" /\ since < Cap /\ attempt < MaxAttempts
           /\ f \notin left
           /\ (Named => f = "amy")
           /\ place' = f /\ state' = "queued" /\ attempt' = attempt + 1 /\ since' = since + 1
           /\ wleft' = IF BadLevelLeft THEN {} ELSE left
           \* a deal spends the decision whose window it runs in (brief_bound.go: the cap
           \* counts every attempt since FieldDecidedAttempt, whichever deal made it)
           /\ decided' = FALSE
           /\ UNCHANGED <<left, pinned, emptyOn, emptyUnpinned, lastEmpty>>

\* the machines' deal of an unnamed card: the work card carries the primary's
\* friends_left (BadMachineLeft: it does not)
MachineDeal == /\ state = "ready" /\ since < Cap /\ attempt < MaxAttempts /\ ~Named /\ FleetCard
               /\ place' = Machine /\ state' = "queued" /\ attempt' = attempt + 1 /\ since' = since + 1
               /\ wleft' = IF BadMachineLeft THEN {} ELSE left
               /\ decided' = FALSE \* the deal spends the decision, as Deal does
               /\ UNCHANGED <<left, pinned, emptyOn, emptyUnpinned, lastEmpty>>

\* the coordinator's decision on a card at its cap (brief, rework --tier, carry: the
\* re-tier that reaches a friend tier; brief_bound.go FieldDecidedAttempt): the attempts
\* since it start again
Decide == /\ state = "ready" /\ since >= Cap /\ ~decided
          /\ decided' = TRUE /\ since' = 0
          /\ UNCHANGED <<place, state, attempt, left, wleft, pinned, emptyOn, emptyUnpinned, lastEmpty>>

\* a card at its cap dealt to a friend (not a friend's card) it has not left (BadCapLeft:
\* any), created working on her row, pinned to her from then on: only after the
\* coordinator's decision (BadCapUndecided: the cap's default answer of 2026-10-04, which
\* dealt it with no decision), the deal spending the decision
CapDeal(f) == /\ state = "ready" /\ Cap <= attempt /\ attempt < MaxAttempts /\ ~Named /\ FleetCard
              /\ (decided \/ BadCapUndecided)
              \* inside the window the decision opened (BadCapUndecided deals past it)
              /\ (since < Cap \/ BadCapUndecided)
              /\ (pinned = None \/ f = pinned)
              /\ (BadCapLeft \/ f \notin left)
              /\ place' = f /\ state' = "working" /\ attempt' = attempt + 1 /\ since' = since + 1
              /\ pinned' = f /\ wleft' = left /\ decided' = FALSE
              /\ UNCHANGED <<left, emptyOn, emptyUnpinned, lastEmpty>>

\* the rebalance moves a queued attempt off the machine to a friend's idle lane,
\* never to one the card has left (BadMachineLeft: the work card's list alone)
Rebalance(f) == /\ state = "queued" /\ place = Machine
                /\ f \notin (IF BadMachineLeft THEN wleft ELSE wleft \cup left)
                /\ place' = f
                /\ UNCHANGED <<state, attempt, left, wleft, pinned, emptyOn, emptyUnpinned, lastEmpty, decided, since>>

\* the four friend moves that read cardLeft (the work card's wleft and the
\* primary's left) and move a ready attempt to a friend's lane: the friends'
\* level, the start bound's level, the deal's reclaim and the coordinator's
\* could-take (BadLevelLeft: the work card's own list alone)
FriendMove(f) == /\ state = "queued" /\ place \in Friends /\ place /= f
                 /\ f \notin (IF BadLevelLeft THEN wleft ELSE wleft \cup left)
                 /\ place' = f
                 /\ UNCHANGED <<state, attempt, left, wleft, pinned, emptyOn, emptyUnpinned, lastEmpty, decided, since>>

\* a lane takes the queued attempt
Start == /\ state = "queued" /\ state' = "working"
         /\ UNCHANGED <<place, attempt, left, wleft, pinned, emptyOn, emptyUnpinned, lastEmpty, decided, since>>

\* the lane ends the attempt: ok, or empty (no report; only a friend's lane in
\* this model, a machine's rework avoids its member already: reworkAvoid)
FinishOk == /\ state = "working"
            /\ state' = "done" /\ place' = None
            /\ UNCHANGED <<attempt, left, wleft, pinned, emptyOn, emptyUnpinned, lastEmpty, decided, since>>
FinishEmpty == /\ state = "working" /\ place \in Friends
               /\ state' = "review" /\ emptyOn' = emptyOn \cup {place}
               /\ emptyUnpinned' = IF Named \/ pinned /= None THEN emptyUnpinned
                                   ELSE emptyUnpinned \cup {place}
               \* the finish records her (emptyRunFriend) unless the card is hers
               /\ left' = IF Named \/ pinned /= None \/ BadNoLeft \/ BadRuleLeft THEN left
                          ELSE left \cup {place}
               /\ lastEmpty' = TRUE
               /\ UNCHANGED <<place, attempt, wleft, pinned, decided, since>>

\* a machine's attempt fails (any failure; the machine is not left, its rework
\* avoids the member: reworkAvoid)
MachineFail == /\ state = "working" /\ place = Machine
               /\ state' = "review" /\ lastEmpty' = FALSE
               /\ UNCHANGED <<place, attempt, left, wleft, pinned, emptyOn, emptyUnpinned, decided, since>>

\* the failed rule's answer: below the brief's bound a rework, at it the
\* brief's judgment left to a mind (ruleHarness returns early; the card waits
\* ready for the cap's answer), and on a brief defect the same early return at
\* any attempt (harness_fault.go). Its answer writes the friend an empty run
\* leaves on every path (the finish wrote her too: emptyRunFriend). BadNoLeft
\* leaves her on neither; BadRuleLeft leaves her only on a rework below the
\* bound, never on an early return (the reversed witness: the cap's answer may
\* then pick her); a brief defect's early return still carries her.
Rework == /\ state = "review"
          /\ state' = "ready" /\ place' = None
          /\ left' = IF lastEmpty /\ ~Named /\ pinned = None /\ ~BadNoLeft /\ (~BadRuleLeft \/ attempt < Cap \/ BriefDefect)
                     THEN left \cup {place} ELSE left
          /\ UNCHANGED <<attempt, wleft, pinned, emptyOn, emptyUnpinned, lastEmpty, decided, since>>

Next == \/ \E f \in Friends : Deal(f) \/ Rebalance(f) \/ CapDeal(f) \/ FriendMove(f)
        \/ MachineDeal \/ Start \/ Decide
        \/ FinishOk \/ FinishEmpty \/ MachineFail \/ Rework
Spec == Init /\ [][Next]_vars

TypeOK == /\ place \in Friends \cup {Machine, None}
          /\ state \in {"ready", "queued", "working", "review", "done"}
          /\ attempt \in 0..MaxAttempts
          /\ left \subseteq Friends /\ wleft \subseteq Friends
          /\ pinned \in Friends \cup {None}
          /\ emptyOn \subseteq Friends /\ emptyUnpinned \subseteq Friends
          /\ decided \in BOOLEAN /\ since \in 0..MaxAttempts
\* THE ATTEMPT CAP (the owner, 2026-10-12): no card is dealt more than Cap attempts without a
\* coordinator decision in between (brief_bound.go AtAttemptCap; the deal's refusal)
NoAttemptPastCapWithoutDecision == since <= Cap
\* an any-friend card is never queued or working on a friend whose lane ran it
\* empty while it was hers to leave, however it got there
NeverBackToTheEmptyLane ==
    (~Named /\ state \in {"queued", "working"}) => place \notin emptyUnpinned
\* a named card is never left by its friend: it is not stranded ready
NamedNeverLeft == Named => left = {}
\* reach witness for the Named instance: its friend runs it empty and it is
\* dealt to her again (NeverReDealtNamed fails there, so the instance is not vacuous)
NeverReDealtNamed == ~(Named /\ state = "queued" /\ place \in emptyOn)
\* reach witness for the cap: the cap's answer pins a card (NeverPinned fails in
\* the base instance, so CapDeal is reachable there)
NeverPinned == pinned = None
=============================================================================
