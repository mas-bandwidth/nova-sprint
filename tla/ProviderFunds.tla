------------------------------ MODULE ProviderFunds ------------------------------
\* Provider funds, out of credit, and the all-out stop
\* (internal/sprint/provider_funds.go, balance.go, route_rest.go, steps_tick.go;
\* docs/SPEC-SPRINT.md section 5, "the routes' rests" and "every provider out
\* stops the sprint"). The owner, 2026-10-03: "if all providers are out, then you
\* stop the sprint"; 2026-10-04: "the machine can continue sending work to
\* friends. So it's just a WRONG error message."
\*
\* This module EXTENDS RouteRest and reuses its rest rules: RouteRest holds ONE
\* provider's rest (its provRest, the fleet table's property provider_rest_<p>)
\* and its routes' own rule-3 rests. That provider is p1 here, its one route r1.
\* This module adds the second provider p2 (rest2, its one route r2), the balance
\* poll's low flag (low for p1 is RouteRest's; low2 for p2), the machine's
\* running flag, and a friend's up-or-down (friendUp, an outside event, TRUE or
\* FALSE). It does NOT copy RouteRest's rest rules: p1's actions are RouteRest's
\* (TakeEnds for a refusal, Balance, Pay, WakeProvider, RestProvider) framed
\* over the new variables; p2's actions restate the same rules for the second
\* provider, each citing the RouteRest action it mirrors.
\*
\* The design, Broken = {}:
\*   Refuse(p)   a take the provider refused for credit (class out-of-credit) or
\*               for its key (class auth) rests the provider; only a take
\*               launched after the last rest ended counts, so a refusal while
\*               the provider rests starts nothing (RouteRest.TakeEnds, the
\*               ~Resting guard; providerRestsDue).
\*   Poll(p, l)  the balance poll reads low or not: it writes no rest (Balance),
\*               and a read that sees a payment (l = FALSE) ends a refused credit
\*               rest (Pay "payment"). A low read is a judgment, never a rest.
\*   Funded(p)   the coordinator's word that the provider was paid ends its funds
\*               rest (Pay "funded"; Funded).
\*   Wake(p)     routes wake ends the provider's rest, any cause (WakeProvider).
\*   Rest(p)     the coordinator rests the provider (RestProvider, Rest2).
\*   Stop        the tick stops the machine when every enabled route rests for
\*               its provider's OUT of credit (AllOutOfCredit), and a friend who
\*               is up cancels the stop (friendsKeepRunning).
\*   Start       after a payment the coordinator starts the machine; the machine
\*               never does. START is refused while all are out, unless a friend
\*               is up.
\*
\* Reversed witnesses (Broken, the same constant RouteRest uses):
\*   "lowisout"           Out() counts a low balance: LowNeverStops
\*   "nofriendexception"  the stop drops friendsKeepRunning: NoFundsStopWhileFriendUp
\*
\* Not modelled (named so they are not forgotten): the per-card judgment words
\* (out of funds / refuses its key / low, providerConds), the one-hour spend, a
\* credit rest with no cards and a balance rest (both Retired, route_rest.go, so
\* they hold no route and are read as none here), and the STOP debt (StopReturn).
EXTENDS RouteRest

\* the second provider's rest, the balance poll's low flag for it, the machine's
\* running flag, and a friend's up-or-down (an outside event, as RouteRest's low)
VARIABLES rest2, low2, running, friendUp

\* every variable as one tuple: RouteRest's own vars and this module's
pvars == <<provRest, own, win, low, judged, event, redeal, rest2, low2, running, friendUp>>

\* the events RouteRest's actions write, plus the stop and the start
PFEvents == Events \cup {"start", "stop"}

PFTypeOK ==
    /\ provRest \in {"none", "credit", "auth", "coord", "balance"}
    /\ own \in [Routes -> {"none", "provider", "coord"}]
    /\ win \in [Routes -> Windows]
    /\ low \in BOOLEAN
    /\ judged \in BOOLEAN
    /\ event \in PFEvents \cup {"init"}
    /\ redeal \in 0..W
    /\ rest2 \in {"none", "credit", "auth", "coord", "balance"}
    /\ low2 \in BOOLEAN
    /\ running \in BOOLEAN
    /\ friendUp \in BOOLEAN

PFInit ==
    /\ Init
    /\ rest2 = "none"
    /\ low2 = FALSE
    /\ running = TRUE
    /\ friendUp = FALSE

\* Frame(A): a reused RouteRest action, with this module's own variables still
FR(A) == A /\ UNCHANGED <<rest2, low2, running, friendUp>>

\* ------------------------------------------------------------- p1 (RouteRest's)
\* A refusal of p1's route rests the provider; the window is RouteRest's, so a
\* refusal while p1 rests starts no new rest (~Resting, TakeEnds).
Refuse1(c) == FR(TakeEnds("r1", c))

\* the poll reads p1's balance low or not; it writes no rest (RouteRest.Balance)
Poll1(l) == FR(Balance(l))

\* a payment the poll sees, or funded: p1's credit rest ends (RouteRest.Pay)
Pay1(e) == FR(Pay(e))

\* the coordinator wakes p1 (RouteRest.WakeProvider), or rests it (RestProvider)
Wake1 == FR(WakeProvider)
Rest1 == FR(RestProvider)

\* ------------------------------------------------------------- p2 (the mirror)
\* The same rest rules as RouteRest's, for the second provider: a refusal rests
\* it (RouteRest.TakeEnds, the credit/auth paths), a balance read never rests it
\* (RouteRest.Balance), a payment or funded ends its credit rest (RouteRest.Pay),
\* and wake ends any rest (RouteRest.WakeProvider), rest the coordinator's word
\* (RouteRest.RestProvider).
Refuse2(c) ==
    /\ c \in {"credit", "auth"}
    /\ rest2 = "none"
    /\ rest2' = c
    /\ event' = c
    /\ UNCHANGED <<provRest, own, win, low, judged, redeal, low2, running, friendUp>>

Poll2(l) ==
    /\ l \in BOOLEAN
    /\ low2' = l
    /\ event' = "balance"
    /\ UNCHANGED <<provRest, own, win, low, judged, redeal, rest2, running, friendUp>>

Pay2(e) ==
    /\ e \in {"payment", "funded"}
    /\ rest2 = "credit"
    /\ rest2' = "none"
    /\ event' = e
    /\ UNCHANGED <<provRest, own, win, low, judged, redeal, low2, running, friendUp>>

Wake2 ==
    /\ rest2 # "none"
    /\ rest2' = "none"
    /\ event' = "wake"
    /\ UNCHANGED <<provRest, own, win, low, judged, redeal, low2, running, friendUp>>

Rest2 ==
    /\ rest2 = "none"
    /\ rest2' = "coord"
    /\ event' = "coord"
    /\ UNCHANGED <<provRest, own, win, low, judged, redeal, low2, running, friendUp>>

\* ------------------------------------------------- the all-out stop and start
\* Out: the provider is out of credit (RouteRest.Out, a take refused for credit).
\* "lowisout" counts a low balance as out, which the code never does.
Out1 == provRest = "credit"
Out2 == rest2 = "credit"
Out1Broken == Out1 \/ ("lowisout" \in Broken /\ low)
Out2Broken == Out2 \/ ("lowisout" \in Broken /\ low2)
AllOutBroken == Out1Broken /\ Out2Broken

\* the tick stops the machine when every provider is out, unless a friend is up
\* (friendsKeepRunning); "nofriendexception" drops the friend's exception
Stop ==
    /\ running
    /\ AllOutBroken
    /\ ~friendUp \/ "nofriendexception" \in Broken
    /\ running' = FALSE
    /\ event' = "stop"
    /\ UNCHANGED <<provRest, own, win, low, judged, redeal, rest2, low2, friendUp>>

\* the coordinator starts the machine; refused while all are out, unless a friend
\* is up (cmd/nova-sprint/run.go, store/routes.go OutOfCredit)
Start ==
    /\ ~running
    /\ ~AllOutBroken \/ friendUp
    /\ running' = TRUE
    /\ event' = "start"
    /\ UNCHANGED <<provRest, own, win, low, judged, redeal, rest2, low2, friendUp>>

\* a friend comes up or goes down, an outside event
FriendUp ==
    /\ ~friendUp
    /\ friendUp' = TRUE
    /\ UNCHANGED <<provRest, own, win, low, judged, event, redeal, rest2, low2, running>>

FriendDown ==
    /\ friendUp
    /\ friendUp' = FALSE
    /\ UNCHANGED <<provRest, own, win, low, judged, event, redeal, rest2, low2, running>>

PFNext ==
    \/ \E c \in {"credit", "auth"} : Refuse1(c) \/ Refuse2(c)
    \/ \E l \in BOOLEAN : Poll1(l) \/ Poll2(l)
    \/ \E e \in {"payment", "funded"} : Pay1(e) \/ Pay2(e)
    \/ Wake1 \/ Wake2
    \/ Rest1 \/ Rest2
    \/ Stop
    \/ Start
    \/ FriendUp \/ FriendDown

PFSpec == PFInit /\ [][PFNext]_pvars

\* ---------------------------------------------------------------- the rules

\* the machine stops only when every provider is out of credit (RouteRest.Out):
\* never for a provider low on funds, its key, or the coordinator's rest
StopOnlyWhenAllOut ==
    [][ running /\ ~running' => Out1 /\ Out2 ]_pvars

\* a low balance is never out: a provider low on funds still has money, and the
\* machine never stops for it (RouteRest.Out, RestBalance; providerConds)
LowNeverStops ==
    [][ running /\ ~running' => (~low \/ Out1) /\ (~low2 \/ Out2) ]_pvars

\* a friend who is up cancels the stop: while one is up, the machine keeps running
\* (friendsKeepRunning)
NoFundsStopWhileFriendUp ==
    [][ friendUp /\ running => running' ]_pvars

\* after a payment the coordinator starts the machine; the machine never does.
\* The only step that starts it is the "start" event (Start)
StartIsTheCoordinators ==
    [][ ~running /\ running' => event' = "start" ]_pvars

=============================================================================
