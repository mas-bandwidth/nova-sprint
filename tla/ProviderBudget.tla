----------------------------- MODULE ProviderBudget ----------------------------
\* The ready-over rule (internal/sprint/ready_over.go): a card that sits in
\* ready past the bound raises one judgment per (reason, tier), the reason
\* computed by the deal itself (routeWhys, readyWhy, readyOverConds). A route
\* serves no card when it is disabled, rests (until paid or until woken), or
\* its provider is out of budget (its balance read at or under zero); the
\* judgment names the remedy verb (routes wake, funded, fleet up, release) and
\* clears itself the tick the card is dealt. Distinct causes are never merged:
\* cards that share a cause are one alert, never one per card.
\*
\* The state, for one tier and its routes and the cards ready on it: enabled[r],
\* rest[r] (none, paid, woken) and budget[r] (ok, out: the provider's balance
\* read at or under zero) decide whether r serves (routeWhys: disabled, resting
\* until paid, resting until woken, out of budget); held[c], a coordinator hold
\* on c; ready, the cards past the bound; judged, the open judgments, one per
\* (cause, tier), where a cause is a held card's hold or the tier's route whys.
\* A no-member-room reason (noFreeWidth) and a sentinel are the same shape: a
\* cause a ready card carries, judged once and cleared on the deal.
\*
\* The design, Broken = {}:
\*   Balance   the poll reads a provider's budget: out (at or under zero) or ok.
\*   Disable   the coordinator disables a route.
\*   Rest      a route rests until paid, or until woken.
\*   Wake      the coordinator wakes a route; a payment ends a paid rest.
\*   Hold      the coordinator holds one ready card; Release undoes it.
\*   Deal      a card whose cause no longer holds is dealt: it leaves ready.
\*   Raise     the tick opens each blocked ready card's (cause, tier)
\*             judgment, once per cause, and closes a cause whose cards are
\*             all dealt.
\* Reversed witnesses, each caught by one property below:
\*   "merge"   two causes of one tier collapse into one judgment: OnePerCause
\*   "silent"  a blocked ready card raises nothing: OnePerCause
\*   "stale"   a dealt card's judgment stays open: ClearsWhenDealt
EXTENDS Naturals, FiniteSets

CONSTANTS Routes, Cards, Broken

VARIABLES enabled, rest, budget, held, ready, judged, event
vars == <<enabled, rest, budget, held, ready, judged, event>>

\* a route's why, as routeWhys names it
Why == {"disabled", "paid", "woken", "out", "serves"}

Causes == {"hold"} \cup [Routes -> Why]

TypeOK ==
    /\ enabled \in [Routes -> BOOLEAN]
    /\ rest \in [Routes -> {"none", "paid", "woken"}]
    /\ budget \in [Routes -> {"ok", "out"}]
    /\ held \in SUBSET Cards
    /\ ready \in SUBSET Cards
    /\ judged \in SUBSET Causes
    /\ event \in {"balance", "disable", "rest", "wake", "hold", "deal", "raise", "init"}

Init ==
    /\ enabled = [r \in Routes |-> TRUE]
    /\ rest = [r \in Routes |-> "none"]
    /\ budget = [r \in Routes |-> "ok"]
    /\ held = {}
    /\ ready = {}
    /\ judged = {}
    /\ event = "init"

\* why a route serves no card, or "serves" (routeWhys): disabled, resting until
\* paid, resting until woken, out of budget
routeWhy(r) == IF ~enabled[r] THEN "disabled"
               ELSE IF rest[r] = "paid" THEN "paid"
               ELSE IF rest[r] = "woken" THEN "woken"
               ELSE IF budget[r] = "out" THEN "out"
               ELSE "serves"

\* the routes that serve a deal: enabled, not resting, provider in budget
Serves == {r \in Routes : routeWhy(r) = "serves"}

\* a ready card is not dealt when it is held or no route serves its tier
Blocked(c) == c \in held \/ Serves = {}

\* the reason a ready card is not dealt (readyWhy): a hold, else the routes'
\* whys (its tier has no serving route)
Cause(c) == IF c \in held THEN "hold" ELSE [r \in Routes |-> routeWhy(r)]

\* the causes of the cards blocked in ready
BlockedCauses == {Cause(c) : c \in ready /\ Blocked(c)}

\* the poll reads a provider's budget: out (at or under zero) or ok; a balance
\* never rests a route (balance.go, tla/RouteRest.tla)
Balance(r, b) ==
    /\ budget[r] # b
    /\ budget' = [budget EXCEPT ![r] = b]
    /\ event' = "balance"
    /\ UNCHANGED <<enabled, rest, held, ready, judged>>

Disable(r) ==
    /\ enabled[r]
    /\ enabled' = [enabled EXCEPT ![r] = FALSE]
    /\ event' = "disable"
    /\ UNCHANGED <<rest, budget, held, ready, judged>>

\* a route rests until paid (its provider's funds) or until woken (its key)
Rest(r, k) ==
    /\ k \in {"paid", "woken"}
    /\ rest[r] = "none"
    /\ rest' = [rest EXCEPT ![r] = k]
    /\ event' = "rest"
    /\ UNCHANGED <<enabled, budget, held, ready, judged>>

\* the coordinator wakes a route; a payment ends a paid rest
Wake(r) ==
    /\ rest[r] # "none"
    /\ rest' = [rest EXCEPT ![r] = "none"]
    /\ event' = "wake"
    /\ UNCHANGED <<enabled, budget, held, ready, judged>>

Hold(c) ==
    /\ c \notin held
    /\ held' = held \cup {c}
    /\ event' = "hold"
    /\ UNCHANGED <<enabled, rest, budget, ready, judged>>

Release(c) ==
    /\ c \in held
    /\ held' = held \ {c}
    /\ event' = "hold"
    /\ UNCHANGED <<enabled, rest, budget, ready, judged>>

\* a card whose cause no longer holds is dealt: it leaves ready, and its
\* cause's judgment clears when its last card is dealt
Deal(c) ==
    /\ c \in ready
    /\ ~Blocked(c)
    /\ ready' = ready \ {c}
    /\ event' = "deal"
    /\ UNCHANGED <<enabled, rest, budget, held, judged>>

\* the tick raises each blocked cause once (one judgment per (cause, tier)),
\* and closes a cause whose cards are all dealt
Raise ==
    /\ judged' = IF "silent" \in Broken
                   THEN {}
                   ELSE IF "merge" \in Broken
                        THEN IF BlockedCauses = {} THEN {} ELSE {CHOOSE x \in BlockedCauses : TRUE}
                        ELSE IF "stale" \in Broken
                             THEN judged \cup BlockedCauses
                             ELSE BlockedCauses
    /\ event' = "raise"
    /\ UNCHANGED <<enabled, rest, budget, held, ready>>

Next ==
    \/ \E r \in Routes, b \in {"ok", "out"} : Balance(r, b)
    \/ \E r \in Routes : Disable(r)
    \/ \E r \in Routes, k \in {"paid", "woken"} : Rest(r, k)
    \/ \E r \in Routes : Wake(r)
    \/ \E c \in Cards : Hold(c)
    \/ \E c \in Cards : Release(c)
    \/ \E c \in Cards : Deal(c)
    \/ Raise

Spec == Init /\ [][Next]_vars /\ WF_vars(Raise)

\* ---------------------------------------------------------------- the rules

\* a route is named out of budget exactly when it is enabled, not resting, and
\* its provider's balance is at or under zero (routeWhys; never "serves")
OutOfBudget ==
    \A r \in Routes : routeWhy(r) = "out" <=> (enabled[r] /\ rest[r] = "none" /\ budget[r] = "out")

\* every blocked ready card's cause is eventually judged: once per cause, and
\* distinct causes of one tier are never merged away into one judgment
OnePerCause ==
    \A c \in Cards : (c \in ready /\ Blocked(c)) ~> Cause(c) \in judged

\* a cause with no blocked card left is eventually cleared, the tick its last
\* card is dealt
ClearsWhenDealt ==
    \A c \in Cards :
        (Cause(c) \in judged /\ ~(\E d \in Cards : d \in ready /\ Blocked(d) /\ Cause(d) = Cause(c)))
            ~> Cause(c) \notin judged

=============================================================================
