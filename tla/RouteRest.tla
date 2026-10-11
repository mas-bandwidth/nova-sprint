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
\* refused the seat's key, coord: the coordinator's rest for a time, open: the
\* coordinator's rest until woken); own[r], the route's own rest line (none,
\* provider: its window of ended takes held After transient provider failures,
\* coord: the coordinator's rest for a time, open: the coordinator's until
\* woken); win[r], the route's ended takes since its last rest, the newest W;
\* take[r], the take in flight on the route (none, fresh: launched after the
\* provider's last rest ended, so its refusal rests the provider, stale:
\* launched before that end, so its refusal is ignored); low, the balance
\* poll's last read is not over one hour of the spend; judged, the low-on-funds
\* judgment is open; event, what the last step was (the record the rules read);
\* redeal, the redeals spent so far: a take that ends spends one, an
\* out-of-credit refusal on a limited route spends none (OutOfCreditSpendsNoRedeal).
\*
\* The design, Broken = {}:
\*   Launch     a take is launched on a serving route and ends later; the deal
\*              draws no work card on a resting route, so none launches there.
\*   TakeEnds   a take on a serving route ends (ok, noresult, transient: a 429,
\*              a 5xx, a timeout at the provider, credit: a 402, auth: a 401);
\*              the tick that sees it rests the provider on a refusal launched
\*              after the provider's last rest ended (one launched before is
\*              ignored), and the route when its window holds After transient
\*              failures. A no-result counts in the window and never toward a
\*              rest.
\*   Balance    the poll reads the balance low or not: it writes no rest.
\*   Raise      the tick opens the low-on-funds judgment while the balance is
\*              low, and closes it when it is not.
\*   Pay        a payment seen by the poll, or funded: a credit rest ends.
\*   Rest       the coordinator rests the provider or one route, for a time or
\*              until woken; the provider's rest restarts every route's window,
\*              a route's own rest its window.
\*   Wake       the coordinator ends the provider's rest (any cause) and its
\*              routes' own, or one route's own while its provider serves; a
\*              provider's rest ending leaves any take still in flight launched
\*              before its end (stale).
\*   Expire     a timed rest (transient, the coordinator's with a time) ends by
\*              the clock; a credit rest, an auth rest and an open coordinator
\*              rest have no time (a refused key does not mend itself: the
\*              owner replaces it, then Wake).
\* Reversed witnesses:
\*   "noresult" the retired rule 3: a no-result counts toward the window's
\*              rest: RestOnlyByProviderOrCoordinator
\*   "balance"  the retired balance rest: a low read rests the provider:
\*              RestOnlyByProviderOrCoordinator
\*   "silent"   the tick never raises the low-on-funds judgment: LowIsRaised
\*   "authclock" the retired timed key rest (RouteRestFor, until 2026-10-10): the
\*              clock ends an auth rest: AuthEndsOnlyWoken
\*   "coordclock" the clock ends an open coordinator rest: CoordOpenEndsOnlyWoken
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS Routes, W, After, Broken

VARIABLES provRest, own, win, take, low, judged, event, redeal
vars == <<provRest, own, win, take, low, judged, event, redeal>>

Kinds == {"ok", "noresult", "transient", "credit", "auth"}
Events == Kinds \cup {"balance", "raise", "payment", "funded", "coord", "wake", "clock", "launch"}

\* the bounded windows of ended takes
Windows == UNION {[1..n -> Kinds] : n \in 0..W}

TypeOK ==
    /\ provRest \in {"none", "credit", "auth", "coord", "open", "balance"}
    /\ own \in [Routes -> {"none", "provider", "coord", "open"}]
    /\ win \in [Routes -> Windows]
    /\ take \in [Routes -> {"none", "fresh", "stale"}]
    /\ low \in BOOLEAN
    /\ judged \in BOOLEAN
    /\ event \in Events \cup {"init"}
    /\ redeal \in 0..W

Init ==
    /\ provRest = "none"
    /\ own = [r \in Routes |-> "none"]
    /\ win = [r \in Routes |-> << >>]
    /\ take = [r \in Routes |-> "none"]
    /\ low = FALSE
    /\ judged = FALSE
    /\ event = "init"
    /\ redeal = 0

Resting(r) == provRest # "none" \/ own[r] # "none"

\* the newest W of a window with one more end
Push(s, k) == IF Len(s) < W THEN Append(s, k) ELSE Append(Tail(s), k)

\* the ends that count toward a route's own rest (RestsDue: transient only)
Counts(k) == k = "transient" \/ ("noresult" \in Broken /\ k = "noresult")

CountIn(s) == Cardinality({i \in 1..Len(s) : Counts(s[i])})

\* a take is launched on a serving route and ends later (TakeEnds). A take
\* launched now is after the provider's last rest ended, so its refusal rests.
Launch(r) ==
    /\ take[r] = "none"
    /\ ~Resting(r)
    /\ take' = [take EXCEPT ![r] = "fresh"]
    /\ event' = "launch"
    /\ UNCHANGED <<provRest, own, win, low, judged, redeal>>

\* a take on a serving route ends; the tick's rests in the same step. A refusal
\* (credit or auth) rests the provider only when the take was launched after
\* the provider's last rest ended (take[r] = "fresh"); one launched before
\* (stale) is ignored. A rest the provider begins restarts every route's
\* window, and a route's own rest its window.
TakeEnds(r, k) ==
    /\ take[r] # "none"
    /\ ~Resting(r)
    /\ LET w == Push(win[r], k)
           rested == k \in {"credit", "auth"} /\ take[r] = "fresh"
       IN
       /\ provRest' = IF rested THEN k ELSE provRest
       /\ IF rested
            THEN /\ own' = own
                 /\ win' = [x \in Routes |-> << >>]
            ELSE /\ IF k \notin {"credit", "auth"} /\ CountIn(w) >= After
                     THEN /\ own' = [own EXCEPT ![r] = "provider"]
                          /\ win' = [win EXCEPT ![r] = << >>]
                     ELSE /\ own' = own
                          /\ win' = [win EXCEPT ![r] = w]
    /\ take' = [take EXCEPT ![r] = "none"]
    /\ event' = k
    /\ redeal' = IF k = "credit" THEN redeal ELSE IF redeal < W THEN redeal + 1 ELSE redeal
    /\ UNCHANGED <<low, judged>>

\* the poll reads the balance: it writes no rest
Balance(l) ==
    /\ low' = l
    /\ provRest' = IF "balance" \in Broken /\ l /\ provRest = "none" THEN "balance" ELSE provRest
    /\ event' = "balance"
    /\ UNCHANGED <<own, win, take, judged, redeal>>

\* the tick holds the low-on-funds judgment open exactly while the balance is low
Raise ==
    /\ "silent" \notin Broken
    /\ judged # low
    /\ judged' = low
    /\ event' = "raise"
    /\ UNCHANGED <<provRest, own, win, take, low, redeal>>

\* a payment the poll sees, or funded: a credit rest ends
Pay(e) ==
    /\ provRest = "credit"
    /\ provRest' = "none"
    /\ take' = [r \in Routes |-> IF take[r] = "none" THEN "none" ELSE "stale"]
    /\ event' = e
    /\ UNCHANGED <<own, win, low, judged, redeal>>

\* the coordinator rests the provider for a time ("coord") or until woken
\* ("open"); the provider's rest restarts every route's window
RestProvider ==
    /\ provRest = "none"
    /\ provRest' = "coord"
    /\ win' = [r \in Routes |-> << >>]
    /\ event' = "coord"
    /\ UNCHANGED <<own, take, low, judged, redeal>>

RestProviderOpen ==
    /\ provRest = "none"
    /\ provRest' = "open"
    /\ win' = [r \in Routes |-> << >>]
    /\ event' = "coord"
    /\ UNCHANGED <<own, take, low, judged, redeal>>

\* the coordinator rests one route for a time ("coord") or until woken ("open");
\* a route's own rest restarts its window
RestRoute(r) ==
    /\ own[r] = "none"
    /\ own' = [own EXCEPT ![r] = "coord"]
    /\ win' = [win EXCEPT ![r] = << >>]
    /\ event' = "coord"
    /\ UNCHANGED <<provRest, take, low, judged, redeal>>

RestRouteOpen(r) ==
    /\ own[r] = "none"
    /\ own' = [own EXCEPT ![r] = "open"]
    /\ win' = [win EXCEPT ![r] = << >>]
    /\ event' = "coord"
    /\ UNCHANGED <<provRest, take, low, judged, redeal>>

\* the coordinator wakes the provider (its rest, any cause, and its routes' own),
\* or one route's own while its provider serves; a provider's rest ending leaves
\* any take still in flight launched before its end (stale): a route-only wake
\* (the provider not resting) moves no mark, so an in-flight take stays fresh
WakeProvider ==
    /\ \E r \in Routes : Resting(r)
    /\ provRest' = "none"
    /\ own' = [r \in Routes |-> "none"]
    /\ take' = IF provRest = "none" THEN take ELSE [r \in Routes |-> IF take[r] = "none" THEN "none" ELSE "stale"]
    /\ event' = "wake"
    /\ UNCHANGED <<win, low, judged, redeal>>

WakeRoute(r) ==
    /\ provRest = "none"
    /\ own[r] # "none"
    /\ own' = [own EXCEPT ![r] = "none"]
    /\ event' = "wake"
    /\ UNCHANGED <<provRest, win, take, low, judged, redeal>>

\* the clock ends a timed rest; a credit rest, an auth rest and an open
\* coordinator rest have none
ExpireRoute(r) ==
    /\ own[r] \in {"provider", "coord"} \cup (IF "coordclock" \in Broken THEN {"open"} ELSE {})
    /\ own' = [own EXCEPT ![r] = "none"]
    /\ event' = "clock"
    /\ UNCHANGED <<provRest, win, take, low, judged, redeal>>

ExpireProvider ==
    /\ provRest \in {"coord"}
       \cup (IF "authclock" \in Broken THEN {"auth"} ELSE {})
       \cup (IF "coordclock" \in Broken THEN {"open"} ELSE {})
    /\ provRest' = "none"
    /\ take' = [r \in Routes |-> IF take[r] = "none" THEN "none" ELSE "stale"]
    /\ event' = "clock"
    /\ UNCHANGED <<own, win, low, judged, redeal>>

Next ==
    \/ \E r \in Routes : Launch(r)
    \/ \E r \in Routes, k \in Kinds : TakeEnds(r, k)
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
    [][\A r \in Routes : (~Resting(r) /\ Resting(r)') => event' \in {"transient", "credit", "auth", "coord"}]_vars

\* a provider's credit rest ends only on a payment seen, funded, or the
\* coordinator's wake: never by the clock
CreditEndsOnlyPaidOrWoken ==
    [][(provRest = "credit" /\ provRest' # "credit") => event' \in {"payment", "funded", "wake"}]_vars

\* a provider's auth rest ends only on the coordinator's wake: never by the
\* clock, never by a payment
AuthEndsOnlyWoken ==
    [][(provRest = "auth" /\ provRest' # "auth") => event' = "wake"]_vars

\* a provider's or a route's open coordinator rest ends only on the
\* coordinator's wake: never by the clock
CoordOpenEndsOnlyWoken ==
    [][((provRest = "open" /\ provRest' # "open")
         \/ \E r \in Routes : (own[r] = "open" /\ own[r]' # "open"))
        => event' = "wake"]_vars

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

=============================================================================
