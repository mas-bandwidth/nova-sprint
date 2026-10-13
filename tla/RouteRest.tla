------------------------------- MODULE RouteRest ------------------------------
\* When a route rests (internal/sprint/route_rest.go, provider_funds.go,
\* balance.go, route_coordinator.go). The owner, 2026-10-10, through the seat:
\* "this sounds like something the machine should not do. it should raise it to
\* you as a thing to do, but not do it automatically." A route rests only on a
\* provider-typed failure or the coordinator's word; a take that ended with no
\* result is the model's output on that card and never rests the route; a low
\* balance is a judgment raised to the coordinator, never a rest.
\*
\* The state, for one provider and its routes: provRest, the provider's one
\* rest property (none, credit: it refused a take for want of credit, auth: it
\* refused the seat's key, coord: the coordinator's timed routes rest, coordopen:
\* the coordinator's routes rest with no --until, which ends only when woken);
\* own[r], the route's own rest line (none, provider: its window of ended takes
\* held After transient provider failures, coord: the coordinator's timed,
\* coordopen: the coordinator's open); win[r], the route's window: its ended
\* takes since its last rest (its own or its provider's), the newest W (ends),
\* and its one take in flight (flight: none, after: launched after the last
\* rest's mark, before: launched before it, so a refusal it ends with is
\* ignored); low, the balance poll's last read is not over one hour of the
\* spend; judged, the low-on-funds judgment is open; event, what the last step
\* was (the record the rules read); redeal, the redeals spent so far: a take
\* that ends spends one, an out-of-credit refusal on a limited route spends
\* none (OutOfCreditSpendsNoRedeal), and neither does a provider 5xx under
\* MaxRateReturns (fivexx: its card is returned untouched, and it still counts
\* toward the route's rest as a transient end does; Provider5xxSpendsNoRedeal).
\*
\* The design, Broken = {}:
\*   Launch     a take is launched on a serving route (in flight).
\*   TakeEnds   a take on a serving route ends (ok, noresult, transient: a 429,
\*              a timeout at the provider, or a 5xx past MaxRateReturns,
\*              fivexx: a 5xx returned untouched, credit: a 402, auth: a 401);
\*              the tick that sees it rests the provider on a refusal, and the
\*              route when its window holds After transient failures. A
\*              no-result counts in the window and never toward a rest.
\*   Refuse     a take in flight ends refused: one launched after the last
\*              rest's mark rests the provider; one launched before it is
\*              ignored (providerRestsDue, e.taken.After(last.Mark())).
\*   Balance    the poll reads the balance low or not: it writes no rest.
\*   Raise      the tick opens the low-on-funds judgment while the balance is
\*              low, and closes it when it is not.
\*   Pay        a payment seen by the poll, or funded: a credit rest ends.
\*   Rest       the coordinator rests the provider or one route, timed or open.
\*   Wake       the coordinator ends the provider's rest (any cause) and its
\*              routes' own, or one route's own while its provider serves.
\*   Expire     a timed rest (transient, the coordinator's with a time) ends by
\*              the clock; a credit rest and an auth rest have no time (a refused
\*              key does not mend itself: the owner replaces it, then Wake), and
\*              an open coordinator rest ends only when woken.
\* Reversed witnesses:
\*   "noresult" the retired rule 3: a no-result counts toward the window's
\*              rest: RestOnlyByProviderOrCoordinator
\*   "balance"  the retired balance rest: a low read rests the provider:
\*              RestOnlyByProviderOrCoordinator
\*   "silent"   the tick never raises the low-on-funds judgment: LowIsRaised
\*   "authclock" the retired timed key rest (RouteRestFor, until 2026-10-10): the
\*              clock ends an auth rest: AuthEndsOnlyWoken
\*   "coordclock" the clock ends an open coordinator rest: CoordOpenEndsOnlyWoken
\*   "fivexxspend" the take ending of BASE 65973f2b: a provider 5xx ends the take
\*              and spends a redeal: Provider5xxSpendsNoRedeal
\*   "unbounded" the exhausted case: a 5xx at MaxReturns is still returned
\*              untouched: Provider5xxReturnsBounded
\* Not modelled: an out-of-credit return on a limited route shares the card's
\* FieldRateReturns in the code; returns here counts the 5xx returns alone, and
\* OutOfCreditSpendsNoRedeal still claims no spend past that bound. Nor is the
\* record's number: Counts takes each fivexx as its own end, while the code keys
\* a take's record on redeals + 1, so returns that spend no redeal on one card
\* write one record and count once (the open finding of this card).
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Routes, W, After, Broken

VARIABLES provRest, own, win, low, judged, event, redeal, returns
vars == <<provRest, own, win, low, judged, event, redeal, returns>>

\* MaxRateReturns (internal/sprint/rate_budget.go, 5), at a small instance: the
\* card's 5xx returns untouched before a 5xx ends its take as any provider
\* failure does. returns is the card's FieldRateReturns as the 5xx path counts it.
MaxReturns == 2

Kinds == {"ok", "noresult", "transient", "fivexx", "credit", "auth"}
\* fivexx is a 5xx returned untouched (returns < MaxReturns); fivexxend is a 5xx
\* at the bound, which ends the take and spends a redeal
Events == Kinds \cup {"fivexxend", "balance", "raise", "payment", "funded", "coord", "wake", "clock", "launch", "ignored"}

\* the bounded windows of ended takes
Ends == UNION {[1..n -> Kinds] : n \in 0..W}

\* a take in flight: none, after the last rest's mark, or before it
Flight == {"none", "after", "before"}

\* a route's window: its ended takes and its take in flight
Windows == [ends : Ends, flight : Flight]

TypeOK ==
    /\ provRest \in {"none", "credit", "auth", "coord", "coordopen", "balance"}
    /\ own \in [Routes -> {"none", "provider", "coord", "coordopen"}]
    /\ win \in [Routes -> Windows]
    /\ low \in BOOLEAN
    /\ judged \in BOOLEAN
    /\ event \in Events \cup {"init"}
    /\ redeal \in 0..W
    /\ returns \in 0..MaxReturns

Init ==
    /\ provRest = "none"
    /\ own = [r \in Routes |-> "none"]
    /\ win = [r \in Routes |-> [ends |-> << >>, flight |-> "none"]]
    /\ low = FALSE
    /\ judged = FALSE
    /\ event = "init"
    /\ redeal = 0
    /\ returns = 0

Resting(r) == provRest # "none" \/ own[r] # "none"

\* the newest W of a window with one more end
Push(s, k) == IF Len(s) < W THEN Append(s, k) ELSE Append(Tail(s), k)

\* the ends that count toward a route's own rest (RestsDue: transient only; a
\* 5xx returned untouched still leaves its FieldProviderTake, so it counts)
Counts(k) == k \in {"transient", "fivexx"} \/ ("noresult" \in Broken /\ k = "noresult")

\* a 5xx is returned untouched while the card's returns are under MaxReturns
\* (steps_work.go: c.Int(FieldRateReturns) < MaxRateReturns); "unbounded"
\* reverses the bound, so a 5xx at it is still returned untouched
Returned(k) == k = "fivexx" /\ (returns < MaxReturns \/ "unbounded" \in Broken)

\* the ends that spend no redeal: an out-of-credit refusal on a limited route,
\* and a 5xx returned untouched (rateLimitedReturned), unless reversed
NoSpend(k) == k = "credit" \/ (Returned(k) /\ "fivexxspend" \notin Broken)

CountIn(s) == Cardinality({i \in 1..Len(s) : Counts(s[i])})

\* a route's window with its take in flight set to f (ends unchanged)
WithFlight(r, f) == [ends |-> win[r].ends, flight |-> f]

\* the provider's rest began: every take in flight is now one launched before
\* its mark, so a refusal it ends with starts no second rest
MarkBegun == win' = [r \in Routes |-> WithFlight(r, IF win[r].flight = "after" THEN "before" ELSE win[r].flight)]

\* the refused take is consumed and its provider rest marks the others
MarkRefuse(r) == win' = [x \in Routes |-> WithFlight(x, IF x = r THEN "none" ELSE IF win[x].flight = "after" THEN "before" ELSE win[x].flight)]

\* a take is launched on a serving route, in flight
Launch(r) ==
    /\ ~Resting(r)
    /\ win[r].flight = "none"
    /\ win' = [win EXCEPT ![r] = WithFlight(r, "after")]
    /\ event' = "launch"
    /\ UNCHANGED <<provRest, own, low, judged, redeal, returns>>

\* a take on a serving route ends; the tick's rests in the same step
TakeEnds(r, k) ==
    /\ ~Resting(r)
    /\ LET w == Push(win[r].ends, k) IN
         /\ provRest' = IF k = "credit" THEN "credit" ELSE IF k = "auth" THEN "auth" ELSE provRest
         /\ IF k \in {"credit", "auth"}
              THEN /\ own' = own
                   /\ win' = [x \in Routes |-> [ends |-> << >>, flight |-> IF win[x].flight = "after" THEN "before" ELSE win[x].flight]]
              ELSE IF CountIn(w) >= After
                   THEN /\ own' = [own EXCEPT ![r] = "provider"]
                        /\ win' = [win EXCEPT ![r] = [ends |-> << >>, flight |-> win[r].flight]]
                   ELSE /\ own' = own
                        /\ win' = [win EXCEPT ![r] = [ends |-> w, flight |-> win[r].flight]]
    /\ event' = IF k = "fivexx" /\ ~Returned(k) THEN "fivexxend" ELSE k
    /\ redeal' = IF NoSpend(k) THEN redeal ELSE IF redeal < W THEN redeal + 1 ELSE redeal
    /\ returns' = IF Returned(k) /\ returns < MaxReturns THEN returns + 1 ELSE returns
    /\ UNCHANGED <<low, judged>>

\* a take in flight ends refused: one launched after the last rest's mark rests
\* the provider; one launched before it (or refused while the provider rests) is
\* ignored
Refuse(r, cause) ==
    /\ win[r].flight # "none"
    /\ cause \in {"credit", "auth"}
    /\ IF win[r].flight = "after" /\ provRest = "none"
         THEN /\ provRest' = cause
              /\ win' = [x \in Routes |-> [ends |-> << >>, flight |-> IF x = r THEN "none" ELSE IF win[x].flight = "after" THEN "before" ELSE win[x].flight]]
              /\ event' = cause
              /\ redeal' = IF cause = "credit" THEN redeal ELSE IF redeal < W THEN redeal + 1 ELSE redeal
         ELSE /\ provRest' = provRest
              /\ win' = [win EXCEPT ![r] = WithFlight(r, "none")]
              /\ event' = "ignored"
              /\ redeal' = redeal
    /\ UNCHANGED <<own, low, judged, returns>>

\* the poll reads the balance: it writes no rest
Balance(l) ==
    /\ low' = l
    /\ provRest' = IF "balance" \in Broken /\ l /\ provRest = "none" THEN "balance" ELSE provRest
    /\ event' = "balance"
    /\ UNCHANGED <<own, win, judged, redeal, returns>>

\* the tick holds the low-on-funds judgment open exactly while the balance is low
Raise ==
    /\ "silent" \notin Broken
    /\ judged # low
    /\ judged' = low
    /\ event' = "raise"
    /\ UNCHANGED <<provRest, own, win, low, redeal, returns>>

\* a payment the poll sees, or funded: a credit rest ends
Pay(e) ==
    /\ provRest = "credit"
    /\ provRest' = "none"
    /\ event' = e
    /\ UNCHANGED <<own, win, low, judged, redeal, returns>>

\* the coordinator rests the provider, timed or open (routes rest, --until or not)
RestProvider ==
    /\ provRest = "none"
    /\ provRest' = "coord"
    /\ win' = [r \in Routes |-> [ends |-> << >>, flight |-> IF win[r].flight = "after" THEN "before" ELSE win[r].flight]]
    /\ event' = "coord"
    /\ UNCHANGED <<own, low, judged, redeal, returns>>

RestProviderOpen ==
    /\ provRest = "none"
    /\ provRest' = "coordopen"
    /\ win' = [r \in Routes |-> [ends |-> << >>, flight |-> IF win[r].flight = "after" THEN "before" ELSE win[r].flight]]
    /\ event' = "coord"
    /\ UNCHANGED <<own, low, judged, redeal, returns>>

RestRoute(r) ==
    /\ own[r] = "none"
    /\ own' = [own EXCEPT ![r] = "coord"]
    /\ win' = [win EXCEPT ![r] = [ends |-> << >>, flight |-> win[r].flight]]
    /\ event' = "coord"
    /\ UNCHANGED <<provRest, low, judged, redeal, returns>>

RestRouteOpen(r) ==
    /\ own[r] = "none"
    /\ own' = [own EXCEPT ![r] = "coordopen"]
    /\ win' = [win EXCEPT ![r] = [ends |-> << >>, flight |-> win[r].flight]]
    /\ event' = "coord"
    /\ UNCHANGED <<provRest, low, judged, redeal, returns>>

\* the coordinator wakes the provider (its rest, any cause, and its routes' own),
\* or one route's own while its provider serves
WakeProvider ==
    /\ \E r \in Routes : Resting(r)
    /\ provRest' = "none"
    /\ own' = [r \in Routes |-> "none"]
    /\ event' = "wake"
    /\ UNCHANGED <<win, low, judged, redeal, returns>>

WakeRoute(r) ==
    /\ provRest = "none"
    /\ own[r] # "none"
    /\ own' = [own EXCEPT ![r] = "none"]
    /\ event' = "wake"
    /\ UNCHANGED <<provRest, win, low, judged, redeal, returns>>

\* the clock ends a timed rest; a credit rest has none, and an open
\* coordinator rest ends only when woken
ExpireRoute(r) ==
    /\ own[r] \in {"provider", "coord"}
    /\ own' = [own EXCEPT ![r] = "none"]
    /\ event' = "clock"
    /\ UNCHANGED <<provRest, win, low, judged, redeal, returns>>

ExpireProvider ==
    /\ provRest \in (IF "authclock" \in Broken THEN {"auth", "coord"} ELSE {"coord"})
                  \cup (IF "coordclock" \in Broken THEN {"coordopen"} ELSE {})
    /\ provRest' = "none"
    /\ event' = "clock"
    /\ UNCHANGED <<own, win, low, judged, redeal, returns>>

Next ==
    \/ \E r \in Routes, k \in Kinds : TakeEnds(r, k)
    \/ \E r \in Routes : Launch(r)
    \/ \E r \in Routes, cause \in {"credit", "auth"} : Refuse(r, cause)
    \/ \E l \in BOOLEAN : Balance(l)
    \/ Raise
    \/ \E e \in {"payment", "funded"} : Pay(e)
    \/ RestProvider
    \/ RestProviderOpen
    \/ \E r \in Routes : RestRoute(r)
    \/ \E r \in Routes : RestRouteOpen(r)
    \/ WakeProvider
    \/ \E r \in Routes : WakeRoute(r)
    \/ \E r \in Routes : ExpireRoute(r)
    \/ ExpireProvider

Spec == Init /\ [][Next]_vars /\ WF_vars(Raise)

\* ---------------------------------------------------------------- the rules

\* a serving route begins to rest only on a provider-typed failure or the
\* coordinator's word: never a no-result, never a balance read
RestOnlyByProviderOrCoordinator ==
    [][\A r \in Routes : (~Resting(r) /\ Resting(r)') => event' \in {"transient", "fivexx", "fivexxend", "credit", "auth", "coord"}]_vars

\* a provider's credit rest ends only on a payment seen, funded, or the
\* coordinator's wake: never by the clock
CreditEndsOnlyPaidOrWoken ==
    [][(provRest = "credit" /\ provRest' # "credit") => event' \in {"payment", "funded", "wake"}]_vars

\* a provider's auth rest ends only on the coordinator's wake: never by the
\* clock, never by a payment
AuthEndsOnlyWoken ==
    [][(provRest = "auth" /\ provRest' # "auth") => event' = "wake"]_vars

\* an open coordinator rest (routes rest with no --until) ends only on the
\* coordinator's wake: never by the clock
CoordOpenEndsOnlyWoken ==
    [][ ((provRest = "coordopen" /\ provRest' # "coordopen")
          \/ \E r \in Routes : (own[r] = "coordopen" /\ own'[r] # "coordopen"))
        => event' = "wake" ]_vars

\* a no-result step never changes a rest
NoResultNeverRests ==
    [][event' = "noresult" => (provRest' = provRest /\ own' = own)]_vars

\* a balance low long enough is raised to the coordinator as a judgment
LowIsRaised == [](low => <>(judged \/ ~low))

\* an out-of-credit refusal (a credit event) rests the provider but spends no redeal: its
\* card is returned without FieldTakeEnded, so the redeal count is unchanged. Every other
\* ended take spends one (TakeEnds), so the property is not vacuous.
OutOfCreditSpendsNoRedeal ==
    [][event' = "credit" => redeal' = redeal]_vars

\* a provider 5xx under MaxReturns (a fivexx event: the eligible returns only)
\* spends no redeal: its card is returned untouched (steps_work.go,
\* rateLimitedReturned with label "provider 5xx"), while Counts still counts it
\* toward the route's rest. The reversed witness "fivexxspend" is the take ending
\* of BASE, which spends one.
Provider5xxSpendsNoRedeal ==
    [][event' = "fivexx" => redeal' = redeal]_vars

\* the returns are bounded: a 5xx is returned untouched only under MaxReturns,
\* and each return counts one. The reversed witness "unbounded" returns a 5xx
\* at the bound untouched.
Provider5xxReturnsBounded ==
    [][event' = "fivexx" => (returns < MaxReturns /\ returns' = returns + 1)]_vars

\* past the bound a 5xx ends the take as any provider failure does (takeEnded):
\* it comes only at the bound and spends a redeal (redeal saturates at W)
Provider5xxPastBoundEndsTheTake ==
    [][event' = "fivexxend" => (returns = MaxReturns /\ (redeal' = redeal + 1 \/ redeal = W))]_vars

=============================================================================
