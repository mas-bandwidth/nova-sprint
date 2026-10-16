----------------------------- MODULE ReaderTiers ------------------------------
\* The ask and the deal of nova-sprint by tier (docs/SPEC-SPRINT.md section 6;
\* internal/sprint readers.go freeReaders, ReadsWanted; read_route.go
\* readerServesTier, tierRouted; route.go routeOf, readRouteMissing;
\* steps_tick.go TickDeal, TickAsk, readTierUnserved; read_cards.go
\* ReadTierNoRoute): a read is asked only of a reader that serves the card's
\* tier, a card's reads are asked together, the reads it needs at once, and a
\* work card is dealt to a machine only on a tier an enabled route serves.
\*
\* TIER IS RESPECTED (the owner, 2026-10-16: "Tier must be respected. If no
\* configured tier exists then that is a configuration issue, not something to
\* work around."). A fleet reader runs the route its read draws: it serves a
\* tier only while an enabled route of the tier is configured (routed), whatever
\* its tiers cell names (the cell narrows what it reads, it is never a route).
\* Until v1.4.6 a fleet reader whose cell named a tier was asked its reads with
\* no route (f4a0d6a1, "regardless of routes"), and its member refused every one
\* at launch (2026-10-16, frontier reads); BugReadsRegardlessOfRoutes is that
\* code and fails NoReadOnUnroutedTier. A work card of a tier no route serves is
\* never dealt to a machine (route.go routeOf: drawRoute, else tierServed's
\* refusal); BugDealUnrouted deals it routeless and fails NoCardOnUnroutedTier.
\* A tier no route and no reader of its own model serves, with a read or a card
\* of it waiting, is held by the tier's one configuration judgment (NNoRoute,
\* filed under tier:<t>): raised at the tick (UnroutedIsJudged), closed at the
\* tick once a route of the tier is enabled (JudgedOnlyWhileUnrouted), and never
\* doubled by a cannot-ask or fewer-readers judgment of the same cause
\* (OneJudgmentPerCause). BugJudgmentStays keeps the judgment past its route
\* and fails JudgedOnlyWhileUnrouted; BugTwoJudgments raises cannot ask on an
\* unrouted read too and fails OneJudgmentPerCause. Routes are enabled and
\* disabled at any time (EnableRoute, DisableRoute: nova-config apply).
\*
\* A fleet reader whose tiers cell is empty reads every tier a route serves
\* (read_route.go readerServesTier; the flash-only rule is gone,
\* a-cleared-sprint-reads-with-no-seat-action). A friend's reader brings her
\* own harness model by tier (ownModelReader) and draws no route: it reads a
\* tier its cell names (an empty cell every tier) that her seat's tiers reach,
\* at or above (friendAtOrAbove), route or none; BugFriendAnyTier reads her cell
\* alone and fails FriendReadsWithinHerSeat. A held friend's reader reads none
\* (docs/SPEC-SPRINT.md section 11, hold).
\*
\* A STORE WITH NO ROUTE AT ALL (routed = {}; RoutesExist = FALSE starts the
\* store with none and lets the coordinator enable or disable a route of any
\* tier) is fenced off (the owner, 2026-10-16, on it: "Fence it off."). Only the
\* in-memory twin (StoreKind = "mem": store.Mem, a mem:<file> twin, the
\* selftest's, the play and twin sims') takes the shortcut, every reader and
\* member running its own --model with every tier served (read_route.go
\* twinRunsOwnModel, the one predicate tierRouted, routeOf, routeServed, NextTier,
\* readRouteOf and the rebalance ask; store Backend.InMemory, Store.MemTwin,
\* Snapshot.MemTwin). A real store (StoreKind = "real": Redis) with no route
\* serves no tier: no work card and no fleet read is dealt
\* (NoDealOnRealStoreWithoutRoutes), each tier with work waiting is judged
\* unrouted (UnroutedIsJudged), and the judgment closes once a route of the
\* tier is enabled (JudgedOnlyWhileUnrouted; ReachRealStoreDealsOnceRouted says a
\* real store does deal once a route is enabled). BugShortcutOnRealStore is the
\* v1.4.6 head (len(s.Routes) == 0 alone, any store) and fails
\* NoDealOnRealStoreWithoutRoutes. With routes (RoutesExist = TRUE: flash and
\* pro stay configured) the store kind changes nothing.
\*
\* Every up fleet member has a member reader row reader-<member>, created by
\* init, by a member's join and by a clear when missing, and up while its
\* member is up and not held (read_cards.go memberReads, steps_clear.go
\* ShapeOf). A clear keeps member readers and every working default
\* (defaults.go SetDefaults): after Clear, every up member is a reader of
\* every served tier and read-cards stays on.
\*
\* Reversed witnesses, each red: BugIgnoreHeld ignores a hold and fails
\* NoReadToAHeldReader; BugEmptyFlashOnly has an empty cell read flash only
\* and fails EmptyCellReadsEveryServedTier; BugClearDropsReaders has Clear
\* drop member reader rows and fails EveryUpMemberIsAReader;
\* BugClearTurnsOff has Clear write read-cards off and fails
\* ReadCardsStaysOn; BugReadsRegardlessOfRoutes, BugDealUnrouted,
\* BugJudgmentStays, BugTwoJudgments, BugFriendAnyTier and
\* BugShortcutOnRealStore as above.
\*
\* The instance is the module. It is run as the cases MCReaderTiers.cfg and
\* MCReaderTiersBroken*.cfg (tla/CASES.tsv, group readertiers).

EXTENDS Naturals, FiniteSets

CONSTANTS BugIgnoreHeld, BugEmptyFlashOnly, BugClearDropsReaders,
          BugClearTurnsOff, RoutesExist, Need,
          BugReadsRegardlessOfRoutes, BugDealUnrouted, BugJudgmentStays,
          BugTwoJudgments, BugFriendAnyTier, StoreKind, BugShortcutOnRealStore

ASSUME StoreKind \in {"mem", "real"}

\* rf reads flash; ra has an empty cell; rx1 and rx2 name every tier (reader set
\* --tiers flash,pro,heavy,frontier, the live shape of 2026-10-16); hra is a
\* held friend's reader; fr is a pro friend's reader, up, its cell empty.
Readers == {"rf", "ra", "rx1", "rx2", "hra", "fr"}
\* Read primaries in review: one flash, one pro, one frontier.
Cards == {"cf", "cp", "cx"}
\* Work primaries ready for a machine: one flash, one frontier.
WorkCards == {"wf", "wx"}
Members == {"m1", "m2"}
\* Four tiers, the code's routes (reader_tiers.go readerTierOrder), weakest first.
AllTiers == {"flash", "pro", "heavy", "frontier"}
Rank == [flash |-> 1, pro |-> 2, heavy |-> 3, frontier |-> 4]
\* The tiers whose routes the coordinator configures in this instance: with
\* routes, flash and pro are configured from the start and stay; a store that
\* starts with no route at all has every tier's switched on and off.
Switchable == IF RoutesExist THEN {"heavy", "frontier"} ELSE AllTiers
InitRouted == IF RoutesExist THEN {"flash", "pro"} ELSE {}

tierOf == [cf |-> "flash", cp |-> "pro", cx |-> "frontier", wf |-> "flash", wx |-> "frontier"]
readerTiers == [rf |-> {"flash"}, ra |-> {}, rx1 |-> AllTiers, rx2 |-> AllTiers, hra |-> {}, fr |-> {}]

heldReaders == {"hra"}
\* hra and fr bring their own model (ownModelReader); every other reader is the
\* fleet's and runs its read's route.
OwnModelReaders == {"hra", "fr"}
\* Her seat's tiers (FriendSeat.Tiers): what her own model is configured for.
seatTiers == [hra |-> {"pro"}, fr |-> {"pro"}]

memberUp == [m \in Members |-> TRUE]

Held(r) == r \in heldReaders /\ ~BugIgnoreHeld

VARIABLES asked, memberReader, readCardsOn, routed, dealt, judged, cannotAsk,
          tock, badRead, badCard, bareDeal

vars == <<asked, memberReader, readCardsOn, routed, dealt, judged, cannotAsk,
          tock, badRead, badCard, bareDeal>>

\* The store holds no route at all.
NoRouteAtAll == routed = {}

\* The twin's shortcut (read_route.go twinRunsOwnModel): the in-memory store with
\* no route at all runs every tier on the member's own --model. The bug takes it
\* on a real store too (the v1.4.6 head: len(s.Routes) == 0, any store).
TwinRunsOwnModel == NoRouteAtAll /\ (StoreKind = "mem" \/ BugShortcutOnRealStore)

\* A real store holding no route at all: no tier is served there.
RealBare == StoreKind = "real" /\ NoRouteAtAll

\* A tier a fleet reader or a machine may draw: an enabled route of it is
\* configured, or the twin holds no route at all (read_route.go tierRouted).
Routed(t) == TwinRunsOwnModel \/ t \in routed
ServedTiers == {t \in AllTiers : Routed(t)}

\* The tiers at or above one her seat names (friendAtOrAbove): a friend reads her
\* tier or any below it.
AtOrAbove(r) == {t \in AllTiers : \E s \in seatTiers[r] : Rank[s] >= Rank[t]}

Cell(r) == IF readerTiers[r] = {} THEN AllTiers ELSE readerTiers[r]

\* What the ask reads off the cell (read_route.go readerServesTier).
Reads(r) ==
  IF Held(r) THEN {}
  ELSE IF r \in OwnModelReaders THEN
    IF BugFriendAnyTier THEN Cell(r) ELSE Cell(r) \cap AtOrAbove(r)
  ELSE IF readerTiers[r] = {} THEN
    IF BugEmptyFlashOnly THEN {"flash"} \cap ServedTiers
    ELSE ServedTiers
  ELSE IF BugReadsRegardlessOfRoutes THEN readerTiers[r]
  ELSE readerTiers[r] \cap ServedTiers

\* A tier no route and no reader up of its own model serves
\* (route.go readRouteMissing).
Unserved(t) ==
  /\ ~Routed(t)
  /\ ~\E r \in OwnModelReaders : ~Held(r) /\ t \in Cell(r) \cap AtOrAbove(r)

\* A read primary waits while none of its reads is asked (they are asked together).
Waiting(c) == \A r \in Readers : c \notin asked[r]

Eligible(c) == {r \in Readers : c \notin asked[r]}
FreeFor(c) == {r \in Eligible(c) : tierOf[c] \in Reads(r)}

\* The tiers whose one configuration judgment the tick raises: unserved, with a
\* read or a card of it waiting.
WaitingUnserved ==
  {t \in AllTiers :
     /\ Unserved(t)
     /\ \/ \E c \in Cards : tierOf[c] = t /\ Waiting(c)
        \/ \E w \in WorkCards : tierOf[w] = t /\ w \notin dealt}

Init ==
  /\ asked = [r \in Readers |-> {}]
  /\ memberReader = [m \in Members |-> TRUE]
  /\ readCardsOn = TRUE
  /\ routed = InitRouted
  /\ dealt = {}
  /\ judged = {}
  /\ cannotAsk = {}
  /\ tock = FALSE
  /\ badRead = FALSE
  /\ badCard = FALSE
  /\ bareDeal = FALSE

\* Ask places the reads the card needs together, Need[c] of them, one each on a
\* different free reader that serves the card's tier (ReadsWanted). The ghost
\* badRead records a fleet reader asked a read of a tier no route serves, and
\* bareDeal one asked any read on a real store with no route at all.
Ask(c) ==
  /\ Waiting(c)
  /\ Cardinality(FreeFor(c)) >= Need[c]
  /\ \E S \in SUBSET FreeFor(c) :
       /\ Cardinality(S) = Need[c]
       /\ asked' = [r \in Readers |-> IF r \in S THEN asked[r] \cup {c} ELSE asked[r]]
       /\ badRead' = (badRead \/ \E r \in S \ OwnModelReaders : ~Routed(tierOf[c]))
       /\ bareDeal' = (bareDeal \/ (RealBare /\ S \ OwnModelReaders # {}))
  /\ tock' = FALSE
  /\ UNCHANGED <<memberReader, readCardsOn, routed, dealt, judged, cannotAsk, badCard>>

\* The deal gives a work card to a machine only on a routed tier (routeOf);
\* BugDealUnrouted deals it routeless, a member override's model or none. The
\* ghost bareDeal records a card dealt on a real store with no route at all.
Deal(w) ==
  /\ w \notin dealt
  /\ Routed(tierOf[w]) \/ BugDealUnrouted
  /\ dealt' = dealt \cup {w}
  /\ badCard' = (badCard \/ ~Routed(tierOf[w]))
  /\ bareDeal' = (bareDeal \/ RealBare)
  /\ tock' = FALSE
  /\ UNCHANGED <<asked, memberReader, readCardsOn, routed, judged, cannotAsk, badRead>>

\* The coordinator enables or disables a route of a tier (nova-config apply).
EnableRoute(t) ==
  /\ t \notin routed
  /\ routed' = routed \cup {t}
  /\ tock' = FALSE
  /\ UNCHANGED <<asked, memberReader, readCardsOn, dealt, judged, cannotAsk, badRead, badCard, bareDeal>>

DisableRoute(t) ==
  /\ t \in routed
  /\ routed' = routed \ {t}
  /\ tock' = FALSE
  /\ UNCHANGED <<asked, memberReader, readCardsOn, dealt, judged, cannotAsk, badRead, badCard, bareDeal>>

\* The tick's judgments (TickDeal NNoRoute; TickAsk and the read-card ask's
\* cannot ask): the tier's one judgment for every unserved tier with work
\* waiting, closed once it is served; cannot ask for a waiting read with too few
\* free readers only when its tier is served (readTierUnserved, ReadTierNoRoute).
Tick ==
  /\ judged' = IF BugJudgmentStays THEN judged \cup WaitingUnserved ELSE WaitingUnserved
  /\ cannotAsk' = {c \in Cards :
                     /\ Waiting(c)
                     /\ Cardinality(FreeFor(c)) < Need[c]
                     /\ (BugTwoJudgments \/ ~Unserved(tierOf[c]))}
  /\ tock' = TRUE
  /\ UNCHANGED <<asked, memberReader, readCardsOn, routed, dealt, badRead, badCard, bareDeal>>

\* Clear advances the epoch: the work goes, the shape stays. Member reader
\* rows and every working default survive (steps_clear.go ShapeOf,
\* defaults.go SetDefaults); the asked reads and dealt cards are the old
\* epoch's and go.
Clear ==
  /\ asked' = [r \in Readers |-> {}]
  /\ dealt' = {}
  /\ memberReader' = IF BugClearDropsReaders
                       THEN [m \in Members |-> FALSE]
                       ELSE memberReader
  /\ readCardsOn' = IF BugClearTurnsOff THEN FALSE ELSE TRUE
  /\ tock' = FALSE
  /\ UNCHANGED <<routed, judged, cannotAsk, badRead, badCard, bareDeal>>

Next ==
  \/ \E c \in Cards : Ask(c)
  \/ \E w \in WorkCards : Deal(w)
  \/ \E t \in Switchable : EnableRoute(t) \/ DisableRoute(t)
  \/ Tick
  \/ Clear

Spec == Init /\ [][Next]_vars

\* The code's rule, written apart from Reads (read_route.go readerServesTier): the
\* ask never places a read on a held reader, on a reader whose cell leaves the
\* card's tier out, or on a friend's reader her seat does not reach. The route
\* half of the rule is checked at the ask (NoReadOnUnroutedTier): a route
\* disabled since may leave a read asked while it served.
NoReadOutsideTier ==
  \A r \in Readers : \A c \in asked[r] :
    /\ ~Held(r)
    /\ tierOf[c] \in Cell(r)
    /\ r \in OwnModelReaders => \E s \in seatTiers[r] : Rank[s] >= Rank[tierOf[c]]

\* TIER IS RESPECTED: no fleet reader is ever asked a read of a tier no route
\* serves, and no machine is ever dealt a card of one.
NoReadOnUnroutedTier == ~badRead
NoCardOnUnroutedTier == ~badCard

\* THE FENCE (the owner, 2026-10-16: "Fence it off."): a real store with no
\* route at all deals nothing, no work card to a machine and no read to a fleet
\* reader; only the in-memory twin runs every tier on its own --model.
NoDealOnRealStoreWithoutRoutes == ~bareDeal

\* A friend's reader reads only the tiers her seat reaches.
FriendReadsWithinHerSeat ==
  \A r \in OwnModelReaders : \A c \in asked[r] : tierOf[c] \in AtOrAbove(r)

\* Just after a tick: every unserved tier with work waiting is judged, a judged
\* tier is one still unserved (its judgment closed once a route serves it), and
\* no read of a judged tier carries a cannot-ask judgment too.
UnroutedIsJudged == tock => WaitingUnserved \subseteq judged
JudgedOnlyWhileUnrouted == tock => \A t \in judged : Unserved(t)
OneJudgmentPerCause == tock => \A c \in cannotAsk : tierOf[c] \notin judged

\* The tightened rule: a held friend's reader is asked nothing (docs/SPEC-SPRINT.md
\* section 11, hold).
NoReadToAHeldReader ==
  \A r \in heldReaders : asked[r] = {}

\* A fleet reader's empty cell reads every served tier: none is held to flash
\* alone (the flash-only rule is gone).
EmptyCellReadsEveryServedTier ==
  \A r \in Readers \ OwnModelReaders :
    (~Held(r) /\ readerTiers[r] = {}) => Reads(r) = ServedTiers

\* After Clear (and at every point, since only Clear can change it), every
\* up member has its member reader row.
EveryUpMemberIsAReader ==
  \A m \in Members : memberUp[m] => memberReader[m]

\* A clear never turns a working default off: read-cards stays on.
ReadCardsStaysOn == readCardsOn = TRUE

\* Reachability (a reversed witness, written to be false where the design must
\* reach): a frontier read is asked once a frontier route is enabled.
ReachFrontierRead == \A r \in Readers : "cx" \notin asked[r]

\* Reachability: a real store that starts with no route deals a card once a
\* route of its tier is enabled (the fence never stops the deal for good).
ReachRealStoreDealsOnceRouted == ~(StoreKind = "real" /\ "wf" \in dealt)

=============================================================================
