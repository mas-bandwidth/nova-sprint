------------------------------- MODULE ReadyOver -------------------------------
\* The ready-over rule (internal/sprint/ready_over.go): a card that sits in
\* ready past the bound raises one judgment per (reason, tier), the reason
\* computed by the deal itself (routeWhys, readyWhy, readyOverConds). A route
\* serves no card when it is disabled, rests (until paid, until woken, or until
\* its time), or its provider is out of budget (its balance read at or under
\* zero); a card's own reason is its hold (one cause per held card, the card's
\* id in the sentence), a bad brief (one per card), or the tier's own cause: its
\* stream held, friends alone serving it, no member able to launch a route, or
\* no member with free width. The judgment names the remedy verb and clears
\* itself the tick the card is dealt. Distinct causes are never merged: cards
\* that share a cause are one alert, never one per card.
\*
\* The state, for one tier and its routes and the cards ready on it: enabled[r],
\* rest[r] (none, paid, woken, timed) and budget[r] (ok, out) decide whether r
\* serves (routeWhys); held, the cards the coordinator holds (each its own
\* cause); badBrief, the cards whose brief's model lines do not read (each its
\* own cause); tierCause, the tier's cause (none, stream, friends, launcher,
\* width); ready, the cards waiting to be dealt, and past, those of them that
\* have sat past ReadyMax (the bound); judged, the open judgments, one per
\* cause; raised, whether the last step was the tick's raise.
\*
\* The design, Broken = {}:
\*   Balance    the poll reads a provider's budget: out (at or under zero) or ok.
\*   Disable    the coordinator disables a route.
\*   Rest       a route rests until paid, until woken, or until its time.
\*   Wake       a rest ends (the coordinator wakes a route; a payment ends a
\*              paid rest; a timed rest lapses).
\*   Hold       the coordinator holds one ready card; Release undoes it.
\*   TierCause  the tier's cause comes and goes (its stream held or released,
\*              friends alone, no launcher, no free width).
\*   Brief      a card's brief stops reading; Unbrief undoes it.
\*   Ready      the deal's resolve step: a card becomes ready to be dealt.
\*   Pass       a ready card sits past the bound (ReadyMax).
\*   Deal       a card whose cause no longer holds is dealt: it leaves ready.
\*   Raise      the tick opens each blocked past card's cause, once per cause,
\*              and closes a cause whose cards are all dealt.
\* Reversed witnesses, each caught by one property below:
\*   "merge"    two causes of one tier collapse into one judgment: OnePerCause
\*   "silent"   a blocked ready card raises nothing: OnePerCause
\*   "stale"    a dealt card's judgment stays open: ClearsWhenDealt
\*   "nobound"  a card is raised before it sits past ReadyMax: RaisedOnlyPastReadyMax
EXTENDS Naturals, FiniteSets

CONSTANTS Routes, Cards, Broken

VARIABLES enabled, rest, budget, held, badBrief, tierCause, ready, past, judged, raised
vars == <<enabled, rest, budget, held, badBrief, tierCause, ready, past, judged, raised>>

\* a route's why, as routeWhys names it
Why == {"disabled", "paid", "woken", "timed", "out", "serves"}

\* a cause names what blocks a ready card: its kind, the card whose own cause
\* it is (a hold or a bad brief; a tier cause names any one card), and, for the
\* route cause, each route's why. The card field makes one held card one cause
\* and two held cards two causes.
CauseKind == {"route", "hold", "brief", "stream", "friends", "launcher", "width", "serves"}
Causes == [kind : CauseKind, card : Cards, routes : [Routes -> Why]]

TypeOK ==
    /\ enabled \in [Routes -> BOOLEAN]
    /\ rest \in [Routes -> {"none", "paid", "woken", "timed"}]
    /\ budget \in [Routes -> {"ok", "out"}]
    /\ held \in SUBSET Cards
    /\ badBrief \in SUBSET Cards
    /\ tierCause \in {"none", "stream", "friends", "launcher", "width"}
    /\ ready \in SUBSET Cards
    /\ past \in SUBSET Cards
    /\ past \subseteq ready
    /\ judged \in SUBSET Causes
    /\ raised \in BOOLEAN

Init ==
    /\ enabled = [r \in Routes |-> TRUE]
    /\ rest = [r \in Routes |-> "none"]
    /\ budget = [r \in Routes |-> "ok"]
    /\ held = {}
    /\ badBrief = {}
    /\ tierCause = "none"
    /\ ready = {}
    /\ past = {}
    /\ judged = {}
    /\ raised = FALSE

\* why a route serves no card, or "serves" (routeWhys): disabled, resting until
\* paid, resting until woken, resting until its time, out of budget
routeWhy(r) == IF ~enabled[r] THEN "disabled"
               ELSE IF rest[r] = "paid" THEN "paid"
               ELSE IF rest[r] = "woken" THEN "woken"
               ELSE IF rest[r] = "timed" THEN "timed"
               ELSE IF budget[r] = "out" THEN "out"
               ELSE "serves"

\* the routes that serve a deal: enabled, not resting, provider in budget
Serves == {r \in Routes : routeWhy(r) = "serves"}

\* a cause, one per kind; the per-card kinds carry the card, and the route kind
\* carries the routes' whys
AnyCard == CHOOSE c \in Cards : TRUE
AllServe == [r \in Routes |-> "serves"]
RouteWhys == [r \in Routes |-> routeWhy(r)]
HoldCause(c) == [kind |-> "hold", card |-> c, routes |-> AllServe]
BriefCause(c) == [kind |-> "brief", card |-> c, routes |-> AllServe]
StreamCause == [kind |-> "stream", card |-> AnyCard, routes |-> AllServe]
FriendsCause == [kind |-> "friends", card |-> AnyCard, routes |-> AllServe]
LauncherCause == [kind |-> "launcher", card |-> AnyCard, routes |-> AllServe]
WidthCause == [kind |-> "width", card |-> AnyCard, routes |-> AllServe]
RouteCause == [kind |-> "route", card |-> AnyCard, routes |-> RouteWhys]
DealtCause == [kind |-> "serves", card |-> AnyCard, routes |-> AllServe]

\* the reason a ready card is not dealt (readyWhy), in the code's order: a
\* hold, then the tier's cause (stream, friends), then the card's bad brief,
\* then the tier's routes (its tier has no serving route), then no launcher,
\* then no free width; else the deal would deal it
Cause(c) == IF c \in held THEN HoldCause(c)
            ELSE IF tierCause = "stream" THEN StreamCause
            ELSE IF tierCause = "friends" THEN FriendsCause
            ELSE IF c \in badBrief THEN BriefCause(c)
            ELSE IF Serves = {} THEN RouteCause
            ELSE IF tierCause = "launcher" THEN LauncherCause
            ELSE IF tierCause = "width" THEN WidthCause
            ELSE DealtCause

\* a ready card is not dealt when its cause is not "serves"
Blocked(c) == c \in ready /\ Cause(c) # DealtCause

\* the causes of the cards blocked in ready past the bound
BlockedCauses == {Cause(c) : c \in {x \in past : Blocked(x)}}

\* the causes of every card blocked in ready, past the bound or not (the
\* "nobound" witness raises these, before ReadyMax)
ReadyBlockedCauses == {Cause(c) : c \in {x \in ready : Blocked(x)}}

\* the causes of the cards past the bound, blocked or not
PastCauses == {Cause(c) : c \in past}

\* the poll reads a provider's budget: out (at or under zero) or ok; a balance
\* never rests a route (balance.go, tla/RouteRest.tla)
Balance(r, b) ==
    /\ budget[r] # b
    /\ budget' = [budget EXCEPT ![r] = b]
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, held, badBrief, tierCause, ready, past, judged>>

Disable(r) ==
    /\ enabled[r]
    /\ enabled' = [enabled EXCEPT ![r] = FALSE]
    /\ raised' = FALSE
    /\ UNCHANGED <<rest, budget, held, badBrief, tierCause, ready, past, judged>>

\* a route rests until paid (its provider's funds), until woken (its key), or
\* until its time
Rest(r, k) ==
    /\ k \in {"paid", "woken", "timed"}
    /\ rest[r] = "none"
    /\ rest' = [rest EXCEPT ![r] = k]
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, budget, held, badBrief, tierCause, ready, past, judged>>

\* a rest ends: the coordinator wakes a route, a payment ends a paid rest, a
\* timed rest lapses
Wake(r) ==
    /\ rest[r] # "none"
    /\ rest' = [rest EXCEPT ![r] = "none"]
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, budget, held, badBrief, tierCause, ready, past, judged>>

Hold(c) ==
    /\ c \notin held
    /\ held' = held \cup {c}
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, budget, badBrief, tierCause, ready, past, judged>>

Release(c) ==
    /\ c \in held
    /\ held' = held \ {c}
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, budget, badBrief, tierCause, ready, past, judged>>

\* the tier's cause comes and goes (its stream held or released, friends alone
\* serving it, no member able to launch a route, no member with free width)
TierCause(t) ==
    /\ t \in {"none", "stream", "friends", "launcher", "width"}
    /\ tierCause # t
    /\ tierCause' = t
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, budget, held, badBrief, ready, past, judged>>

\* a card's brief's model lines stop reading, and read again
Brief(c) ==
    /\ c \notin badBrief
    /\ badBrief' = badBrief \cup {c}
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, budget, held, tierCause, ready, past, judged>>

Unbrief(c) ==
    /\ c \in badBrief
    /\ badBrief' = badBrief \ {c}
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, budget, held, tierCause, ready, past, judged>>

\* the deal's resolve step: a card becomes ready to be dealt; it then waits,
\* blocked until its cause no longer holds
Ready(c) ==
    /\ c \notin ready
    /\ ready' = ready \cup {c}
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, budget, held, badBrief, tierCause, past, judged>>

\* a ready card sits past the bound (ReadyMax): only then is it ready-over
Pass(c) ==
    /\ c \in ready \ past
    /\ past' = past \cup {c}
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, budget, held, badBrief, tierCause, ready, judged>>

\* a card whose cause no longer holds is dealt: it leaves ready, and its
\* cause's judgment clears when its last card is dealt
Deal(c) ==
    /\ c \in ready
    /\ ~Blocked(c)
    /\ ready' = ready \ {c}
    /\ past' = past \ {c}
    /\ raised' = FALSE
    /\ UNCHANGED <<enabled, rest, budget, held, badBrief, tierCause, judged>>

\* the tick raises each blocked past card's cause once (one judgment per
\* (cause, tier)), and closes a cause whose cards are all dealt
Raise ==
    /\ judged' = IF "silent" \in Broken
                   THEN {}
                   ELSE IF "merge" \in Broken
                        THEN IF BlockedCauses = {} THEN {} ELSE {CHOOSE x \in BlockedCauses : TRUE}
                        ELSE IF "stale" \in Broken
                             THEN judged \cup BlockedCauses
                             ELSE IF "nobound" \in Broken
                                  THEN ReadyBlockedCauses
                                  ELSE BlockedCauses
    /\ raised' = TRUE
    /\ UNCHANGED <<enabled, rest, budget, held, badBrief, tierCause, ready, past>>

Next ==
    \/ \E r \in Routes, b \in {"ok", "out"} : Balance(r, b)
    \/ \E r \in Routes : Disable(r)
    \/ \E r \in Routes, k \in {"paid", "woken", "timed"} : Rest(r, k)
    \/ \E r \in Routes : Wake(r)
    \/ \E c \in Cards : Hold(c)
    \/ \E c \in Cards : Release(c)
    \/ \E t \in {"none", "stream", "friends", "launcher", "width"} : TierCause(t)
    \/ \E c \in Cards : Brief(c)
    \/ \E c \in Cards : Unbrief(c)
    \/ \E c \in Cards : Ready(c)
    \/ \E c \in Cards : Pass(c)
    \/ \E c \in Cards : Deal(c)
    \/ Raise

Spec == Init /\ [][Next]_vars /\ WF_vars(Raise)

\* ---------------------------------------------------------------- the rules

\* a route is named out of budget exactly when it is enabled, not resting, and
\* its provider's balance is at or under zero (routeWhys; never "serves")
OutOfBudget ==
    \A r \in Routes : routeWhy(r) = "out" <=> (enabled[r] /\ rest[r] = "none" /\ budget[r] = "out")

\* every blocked ready card past the bound is eventually judged, once per
\* cause, and distinct causes of one tier are never merged away into one
\* judgment. A card may stop being blocked (dealt or its cause cleared)
\* instead of being judged: that discharges the obligation, so a silent tick
\* and a merged tick each leave a still-blocked card unjudged and are caught.
OnePerCause ==
    \A c \in Cards :
        []( (c \in past /\ Blocked(c)) => <>(Cause(c) \in judged \/ ~(c \in past /\ Blocked(c))) )

\* a judgment is open exactly for the causes that block past cards the tick it
\* is written: a cause with no blocked card left is gone that same tick, so a
\* dealt card's judgment clears itself the tick the card is dealt. A stale tick
\* keeps a dealt card's judgment open, and ClearsWhenDealt catches it.
ClearsWhenDealt == []( (raised = TRUE) => (judged = BlockedCauses) )

\* a raise writes a judgment only for a card that has sat past the bound: the
\* ready-over part raises nothing before ReadyMax. The "nobound" witness raises
\* a ready card before the bound, and RaisedOnlyPastReadyMax catches it.
RaisedOnlyPastReadyMax == []( (raised = TRUE) => (judged \subseteq PastCauses) )

=============================================================================
