----------------------------- MODULE RateBackoff -----------------------------
\* A take a provider ends rate-limited (a-rate-limited-refusal-never-fails-the-cardc;
\* internal/sprint steps_work.go rateLimitedReturned, route_rest.go RestsDue): the
\* card goes back to ready unspent (no redeal, no attempt), and the route backs off
\* for Min(2^(k-1), Cap) time units, k counting the route's rate-limited ends since
\* its last ok end; an ok end resets k; the backoff ends by itself when the clock
\* passes it. The owner, 2026-10-11: a rate limit stalls, it never fails a card.
EXTENDS Naturals
CONSTANTS Cap, MaxTime, MaxK, Broken
VARIABLES now, k, until, redeals, ready
vars == <<now, k, until, redeals, ready>>

Min(a, b) == IF a < b THEN a ELSE b
Backoff(j) == IF "nocap" \in Broken THEN 2^(j - 1) ELSE Min(2^(j - 1), Cap)
Dealable == ready /\ now >= until

Init == now = 0 /\ k = 0 /\ until = 0 /\ redeals = 0 /\ ready = TRUE

\* the take ends rate-limited: the card returns unspent; the route backs off
RateEnd == /\ Dealable /\ now <= MaxTime /\ k < MaxK
           /\ k' = k + 1
           /\ until' = now + Backoff(k + 1)
           /\ redeals' = IF "spend" \in Broken THEN redeals + 1 ELSE redeals
           /\ UNCHANGED <<now, ready>>

\* the take ends ok: the card is done and the route's count resets
OkEnd == /\ Dealable
         /\ k' = 0 /\ ready' = FALSE
         /\ UNCHANGED <<now, until, redeals>>

Tick == /\ now < MaxTime + 2^MaxK
        /\ now' = now + 1
        /\ UNCHANGED <<k, until, redeals, ready>>

Next == RateEnd \/ OkEnd \/ Tick
Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

TypeOK == /\ now \in 0..(MaxTime + 2^MaxK) /\ k \in 0..MaxK
          /\ until \in 0..(MaxTime + 2^MaxK) /\ redeals \in 0..MaxK /\ ready \in BOOLEAN

\* a rate-limited end never spends a redeal
RateLimitSpendsNoRedeal == redeals = 0
\* a backoff never reaches past the cap
BackoffBounded == until <= now + Cap
\* a backed-off card is dealable again by itself
BackoffEnds == (ready /\ now < until) ~> Dealable
=============================================================================
