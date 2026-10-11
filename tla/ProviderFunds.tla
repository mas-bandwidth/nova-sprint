----------------------------- MODULE ProviderFunds -----------------------------
\* When every provider is out of credit, and the machine stops (internal/sprint/
\* provider_funds.go, balance.go, route_rest.go; cmd/nova-sprint/run.go). The
\* provider's rest rules are RouteRest's, instantiated once per provider, never
\* copied: a take refused for credit or its key rests the provider, the balance
\* poll writes NO rest (a low balance is the coordinator's judgment, its routes
\* STILL SERVE), a refused take's credit rest ends only on a payment the poll
\* sees or the coordinator's funded, and the coordinator's wake ends a rest of
\* any cause. On top of those, the machine-level rules:
\*
\*   Refuse(p, c)  a take on provider p's one route is refused for credit or
\*                 auth: p rests (RouteRest TakeEnds).
\*   Poll(p, l)    the balance poll reads p low (l TRUE) or not: it writes no
\*                 rest, only the low judgment's input (RouteRest Balance).
\*   Paid(p)       the poll sees a payment: p's credit rest ends (RouteRest Pay).
\*   Funded(p)     the coordinator's funded ends p's credit rest (RouteRest Pay).
\*   Wake(p)       the coordinator's routes wake ends p's rest, any cause.
\*   Stop          the tick stops the machine (running becomes FALSE) when every
\*                 provider is Out (credit only) and no friend is up
\*                 (AllOutOfCredit; friendsKeepRunning cancels the stop).
\*   Start         the coordinator starts the machine: refused while every
\*                 provider is out, unless a friend is up; the machine never
\*                 starts itself (provider_funds.go Funded: a machine the tick
\*                 stopped is the coordinator's to start).
\*   Friend        a friend comes up or goes down (the roster, outside).
\*
\* Reversed witnesses:
\*   "lowisout"          Out counts a low balance (the retired balance rest):
\*                       LowNeverStops
\*   "nofriendexception" friendsKeepRunning is dropped, so an out-of-credit
\*                       fleet stops while a friend is up: NoFundsStopWhileFriendUp
EXTENDS Naturals, FiniteSets

CONSTANTS Providers, W, After, Broken

\* the rest state, one provider per copy: pr = the provider's rest, ow = each
\* route's own rest, wn = the ended-take window, lo = the poll's low read, jd =
\* the low judgment open, ev = the instance's event, rd = redeals spent
VARIABLES running, friendUp, event,
          pr1, ow1, wn1, lo1, jd1, ev1, rd1,
          pr2, ow2, wn2, lo2, jd2, ev2, rd2

\* one route per provider, named by the provider; RouteRest's rest rules are
\* instantiated per provider, never copied (the coordinator's routes rest and
\* the route's own rule-3 window are RouteRest's own module, not this one)
P1 == INSTANCE RouteRest WITH Routes <- {"p1"}, W <- W, After <- After, Broken <- Broken,
  provRest <- pr1, own <- ow1, win <- wn1, low <- lo1, judged <- jd1, event <- ev1, redeal <- rd1
P2 == INSTANCE RouteRest WITH Routes <- {"p2"}, W <- W, After <- After, Broken <- Broken,
  provRest <- pr2, own <- ow2, win <- wn2, low <- lo2, judged <- jd2, event <- ev2, redeal <- rd2

P1vars == <<pr1, ow1, wn1, lo1, jd1, ev1, rd1>>
P2vars == <<pr2, ow2, wn2, lo2, jd2, ev2, rd2>>

vars == <<running, friendUp, event, pr1, ow1, wn1, lo1, jd1, ev1, rd1, pr2, ow2, wn2, lo2, jd2, ev2, rd2>>

Events == {"refuse", "poll", "paid", "funded", "wake", "start", "stop", "friend", "init"}

provRestOf(p) == IF p = "p1" THEN pr1 ELSE pr2
lowOf(p)      == IF p = "p1" THEN lo1 ELSE lo2

\* a provider is out of credit when it rests for a refused take (RouteRest
\* provRest = "credit"): the one cause that counts toward the stop. The
\* "lowisout" witness makes Out count a low balance too, the retired rule.
Out(p) == provRestOf(p) = "credit" \/ ("lowisout" \in Broken /\ lowOf(p))

AllOut == \A p \in Providers : Out(p)

TypeOK ==
    /\ running \in BOOLEAN
    /\ friendUp \in BOOLEAN
    /\ event \in Events
    /\ P1!TypeOK
    /\ P2!TypeOK

Init ==
    /\ P1!Init
    /\ P2!Init
    /\ running = TRUE
    /\ friendUp = FALSE
    /\ event = "init"

\* a take on p's route is refused for credit or its key: p rests
Refuse(p, c) ==
    /\ c \in {"credit", "auth"}
    /\ IF p = "p1" THEN P1!TakeEnds(p, c) ELSE P2!TakeEnds(p, c)
    /\ event' = "refuse"
    /\ UNCHANGED <<running, friendUp>>
    /\ IF p = "p1" THEN UNCHANGED P2vars ELSE UNCHANGED P1vars

\* the poll reads p low (l) or not: it writes no rest, only the low judgment's input
Poll(p, l) ==
    /\ l \in BOOLEAN
    /\ IF p = "p1" THEN P1!Balance(l) ELSE P2!Balance(l)
    /\ event' = "poll"
    /\ UNCHANGED <<running, friendUp>>
    /\ IF p = "p1" THEN UNCHANGED P2vars ELSE UNCHANGED P1vars

\* the poll sees a payment: p's credit rest ends
Paid(p) ==
    /\ IF p = "p1" THEN P1!Pay("payment") ELSE P2!Pay("payment")
    /\ event' = "paid"
    /\ UNCHANGED <<running, friendUp>>
    /\ IF p = "p1" THEN UNCHANGED P2vars ELSE UNCHANGED P1vars

\* the coordinator's funded ends p's credit rest
Funded(p) ==
    /\ IF p = "p1" THEN P1!Pay("funded") ELSE P2!Pay("funded")
    /\ event' = "funded"
    /\ UNCHANGED <<running, friendUp>>
    /\ IF p = "p1" THEN UNCHANGED P2vars ELSE UNCHANGED P1vars

\* the coordinator's wake ends p's rest, any cause
Wake(p) ==
    /\ IF p = "p1" THEN P1!WakeProvider ELSE P2!WakeProvider
    /\ event' = "wake"
    /\ UNCHANGED <<running, friendUp>>
    /\ IF p = "p1" THEN UNCHANGED P2vars ELSE UNCHANGED P1vars

\* the tick stops the machine: every provider out of credit, no friend up
Stop ==
    /\ running
    /\ AllOut
    /\ (~friendUp \/ "nofriendexception" \in Broken)
    /\ running' = FALSE
    /\ event' = "stop"
    /\ UNCHANGED <<friendUp, P1vars, P2vars>>

\* the coordinator starts the machine: refused while every provider is out,
\* unless a friend is up; no other action starts it
Start ==
    /\ ~running
    /\ friendUp \/ ~AllOut
    /\ running' = TRUE
    /\ event' = "start"
    /\ UNCHANGED <<friendUp, P1vars, P2vars>>

\* a friend comes up or goes down
Friend ==
    /\ friendUp' = ~friendUp
    /\ event' = "friend"
    /\ UNCHANGED <<running, P1vars, P2vars>>

Next ==
    \/ \E p \in Providers, c \in {"credit", "auth"} : Refuse(p, c)
    \/ \E p \in Providers, l \in BOOLEAN : Poll(p, l)
    \/ \E p \in Providers : Paid(p)
    \/ \E p \in Providers : Funded(p)
    \/ \E p \in Providers : Wake(p)
    \/ Stop
    \/ Start
    \/ Friend

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* a low balance never stops the machine: the stop is only ever over providers
\* that rest for a refused take (credit), never a low read
LowNeverStops ==
    [][running /\ ~running' => \A p \in Providers : provRestOf(p) = "credit"]_vars

\* the machine stops only when EVERY provider is Out (credit alone): one out of
\* two keeps it running
StopOnlyWhenAllOut ==
    [][running /\ ~running' => \A p \in Providers : Out(p)]_vars

\* a friend up cancels the stop: no funds-stop while a friend is up
NoFundsStopWhileFriendUp ==
    [][running /\ ~running' => friendUp = FALSE]_vars

\* only the coordinator's Start starts the machine: after a payment the
\* coordinator starts it, the machine never does
StartIsTheCoordinators ==
    [][~running /\ running' => event' = "start"]_vars

=============================================================================
