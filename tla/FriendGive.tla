------------------------------ MODULE FriendGive ------------------------------
EXTENDS Naturals, FiniteSets
\* friend give on a ready card (docs/SPEC-SPRINT.md section 1, a friend's card taken back;
\* internal/sprint/friend_take.go FriendGive and friendGiveEligibility, friend_deal.go
\* friendDealPass and friendDealUnit, steps_work.go AttemptPredatesBrief). One card: its
\* primary (ready or working) and its CURRENT attempt's work card, which is none, Ready or
\* Working on a row (live), Done (a brief-replaced bounce leaves it so, the primary ready with
\* no taken_from written), or Withdrawn (taken back by friend take, taken_from the friend).
\* Two friends, "a" and "b", of whom Eligible may be handed the card (her tiers, her
\* restriction, the card's needs and a hard pin: friendGiveEligibility). The friend deal deals
\* a ready card whose work card is not live, at its next attempt; a Withdrawn one it redeals.
\* The verb must call a card dealt exactly when the deal does: only a live work card.
\* Bug found 2026-10-15: the verb called a Done work card dealt and refused "<id>.w2 was never
\* taken back from friend <f> (taken from no friend)" on a card the deal would have dealt.
\* Not modelled: the tiers' content, the stamps, the transport, the lanes.
CONSTANTS Friends, Eligible, MaxAttempt, BadDoneIsDealt
VARIABLES wc, prim, takenFrom, attempt, live
vars == <<wc, prim, takenFrom, attempt, live>>

WcStates == {"none", "Ready", "Working", "Done", "Withdrawn"}

TypeOK == /\ wc \in WcStates
          /\ prim \in {"ready", "working"}
          /\ takenFrom \in Friends \cup {"none"}
          /\ attempt \in 0..MaxAttempt
          /\ live \in 0..2

Init == /\ wc = "none" /\ prim = "ready" /\ takenFrom = "none" /\ attempt = 0 /\ live = 0

\* a work card is dealt when it is live on a row; the broken witness counts a Done one dealt
Dealt(s) == s \in {"Ready", "Working"} \/ (BadDoneIsDealt /\ s = "Done")

\* the guard of a give of the card to f: ready, her eligibility, the work card not dealt and not
\* a taken-back one (a Withdrawn card is the unmark's, below), and an attempt left to deal
GiveGuard(f) == /\ prim = "ready" /\ f \in Eligible
                /\ ~Dealt(wc) /\ wc # "Withdrawn"
                /\ attempt < MaxAttempt

\* friendDealPass: a ready card with no live work card is dealt at its next attempt; a
\* Withdrawn one not taken from any friend is redealt as the same card
Deal == /\ prim = "ready" /\ takenFrom = "none" /\ wc # "Ready" /\ wc # "Working"
        /\ \/ /\ wc # "Withdrawn" /\ attempt < MaxAttempt
              /\ attempt' = attempt + 1
           \/ /\ wc = "Withdrawn" /\ UNCHANGED attempt
        /\ wc' = "Ready" /\ prim' = "working" /\ live' = live + 1
        /\ UNCHANGED takenFrom

Start == /\ wc = "Ready" /\ wc' = "Working" /\ UNCHANGED <<prim, takenFrom, attempt, live>>

\* the finish plan's brief-replaced bounce: the work card to Done, the primary ready, nothing
\* writing taken_from
Bounce == /\ wc \in {"Ready", "Working"} /\ prim = "working"
          /\ wc' = "Done" /\ prim' = "ready" /\ live' = live - 1
          /\ UNCHANGED <<takenFrom, attempt>>

\* friend take f: the work card withdrawn, the primary ready, taken_from f
TakeBack(f) == /\ wc \in {"Ready", "Working"} /\ prim = "working"
               /\ wc' = "Withdrawn" /\ prim' = "ready" /\ takenFrom' = f /\ live' = live - 1
               /\ UNCHANGED attempt

\* friend give f on a ready card whose work card is none or Done: a new work card on her row at
\* the next attempt, the primary working (friendDealUnit)
GiveHand(f) == /\ GiveGuard(f)
               /\ attempt' = attempt + 1
               /\ wc' = "Ready" /\ prim' = "working" /\ live' = live + 1
               /\ UNCHANGED takenFrom

\* friend give f on a card taken back from f: the mark cleared, the deal does the rest
GiveUnmark(f) == /\ prim = "ready" /\ wc = "Withdrawn" /\ takenFrom = f
                 /\ takenFrom' = "none"
                 /\ UNCHANGED <<wc, prim, attempt, live>>

Next == \/ Deal \/ Start \/ Bounce
        \/ \E f \in Friends : TakeBack(f) \/ GiveHand(f) \/ GiveUnmark(f)

Spec == Init /\ [][Next]_vars

\* a give never makes a second live work card
GiveOnlyWhenNotLive == live <= 1

\* a ready card whose work card is Done or none is givable to every eligible friend
GiveReachesABouncedCard ==
    (prim = "ready" /\ wc \in {"none", "Done"} /\ attempt < MaxAttempt)
        => \A f \in Eligible : GiveGuard(f)

=============================================================================
