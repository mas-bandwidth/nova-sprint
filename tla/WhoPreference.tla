-------------------------- MODULE WhoPreference --------------------------
EXTENDS Naturals, FiniteSets, TLC
\* docs/SPEC-SPRINT.md: WHO preference and unpin, and the rebalance (section 1).
\* One ready card of each kind, two subscription friends, one fleet worker,
\* one lane each and room for DealAhead (two) cards each. A deal consumes room
\* atomically; up/down changes eligibility. Every deal is gated on the worker's
\* tiers (Admits) and never deals to a friend the card was taken back from
\* (takenBack). A card with a named friend goes to her first while she takes it;
\* else it goes to the friend the code prefers (an idle lane, then room, then
\* name), then the fleet. A hard pin waits for its friend alone and is released
\* past a bound while she is down (released), after which it is a preference. The
\* rebalance moves a card dealt and not started, queued on a worker whose lane
\* works, to a worker up with an idle lane whose tier set admits the card's tier
\* and that the card has not left; the one-tier-up overflow and cheapest-first
\* order of internal/sprint/rebalance.go rebalanceTo do not fit these two tiers
\* and are not modelled. The model checks selection, not transport, route
\* rotation or retry bounds. BadRebalance is the rebalance's reversed witness
\* ("started" lets it move a started card, "tiers" lets it ignore the tier set);
\* BadOnly lets a hard pin leave its friend; BadTiers drops the deal's tier gate
\* (the "anytier" witness); BadDealback redeals a taken-back card to the same
\* friend (the "dealback" witness).
CONSTANT BadOnly, BadRebalance, BadTiers, BadDealback
Cards == {"plain", "preferred", "only"}
Friends == {"a", "b"}
Workers == Friends \cup {"fleet"}
Tiers == {"flash", "pro"}
CardTier == [c \in Cards |-> IF c = "preferred" THEN "pro" ELSE "flash"]
FriendTiers == [f \in Friends |-> IF f = "a" THEN {"flash", "pro"} ELSE {"pro"}]
VARIABLES owner, pin, up, started, fleetTiers, left, rebalanced, takenBack, released
vars == <<owner, pin, up, started, fleetTiers, left, rebalanced, takenBack, released>>
Init == /\ owner = [c \in Cards |-> "none"]
        /\ pin = [c \in Cards |-> IF c = "plain" THEN "none" ELSE c]
        /\ up \in SUBSET Workers
        /\ started = {}
        /\ fleetTiers \in SUBSET Tiers
        /\ left = [c \in Cards |-> {}]
        /\ rebalanced = {}
        /\ takenBack = [c \in Cards |-> {}]
        /\ released = {}
Held(w) == {c \in Cards : owner[c] = w}
Room(w) == w \in up /\ Cardinality(Held(w)) < 2
Admits(w, t) == IF w = "fleet" THEN t \in fleetTiers ELSE t \in FriendTiers[w]
\* A worker takes card c: up with room, its tier set admits the card's tier, and
\* the card has not been taken back from it. BadTiers drops the tier gate (the
\* "anytier" witness); BadDealback drops the take-back memory (the "dealback"
\* witness).
Takes(w, c) == /\ Room(w)
               /\ (Admits(w, CardTier[c]) \/ BadTiers)
               /\ (w \notin takenBack[c] \/ BadDealback)
\* NameFirst is the code's name tie-break: "a" before "b" (the only two
\* friends).
NameFirst(f, g) == f = "a" /\ g = "b"
\* Prefer(f, g) is the code's order among friends (internal/sprint/friend_deal.go
\* preferredFriend): a friend with an idle lane first, the most idle lanes, then
\* the most room free, then the first by name. With one lane each, idle lanes and
\* room both order by how many cards the friend holds, so this is fewer cards
\* first, ties by name.
Prefer(f, g) == \/ Cardinality(Held(f)) < Cardinality(Held(g))
                \/ /\ Cardinality(Held(f)) = Cardinality(Held(g))
                   /\ NameFirst(f, g)
\* Candidate is the worker the deal offers card c to, or "none": the named
\* friend first while she takes it, else a hard pin waits for her alone, else
\* the preferred friend that takes it, else the fleet, else nobody.
Candidate(c) ==
  IF pin[c] # "none" /\ Takes("a", c) THEN "a"
  ELSE IF pin[c] = "only" /\ c \notin released /\ ~BadOnly THEN "none"
  ELSE LET may == {f \in Friends : Takes(f, c)}
       IN  IF may # {} THEN (CHOOSE f \in may : \A g \in may : (g = f) \/ Prefer(f, g))
           ELSE IF Takes("fleet", c) THEN "fleet"
           ELSE "none"
Deal(c) == /\ owner[c] = "none" /\ Candidate(c) # "none"
           /\ owner' = [owner EXCEPT ![c] = Candidate(c)]
           /\ rebalanced' = rebalanced \ {c}
           /\ UNCHANGED <<pin, up, started, fleetTiers, left, takenBack, released>>
LaneBusy(w) == \E d \in started : owner[d] = w
Start(c) == /\ owner[c] \in Workers /\ c \notin started /\ ~LaneBusy(owner[c])
            /\ started' = started \cup {c}
            /\ UNCHANGED <<owner,pin,up,fleetTiers,left,rebalanced,takenBack,released>>
\* friend take returns a dealt but unstarted card and records the friend it was
\* taken back from, so it is never dealt to that friend again.
TakeBack(c) == /\ owner[c] \in Friends /\ c \notin started
               /\ owner' = [owner EXCEPT ![c] = "none"]
               /\ rebalanced' = rebalanced \ {c}
               /\ takenBack' = [takenBack EXCEPT ![c] = @ \cup {owner[c]}]
               /\ UNCHANGED <<pin,up,started,fleetTiers,left,released>>
Unpin(c) == /\ owner[c] = "none" /\ c \notin started /\ pin[c] # "none"
            /\ pin' = [pin EXCEPT ![c] = "none"]
            /\ released' = released \ {c}
            /\ UNCHANGED <<owner,up,started,fleetTiers,left,rebalanced,takenBack>>
\* a hard pin whose friend is not up is released past its bound
\* (internal/sprint/stops.go PinReleased): it becomes a preference and may be
\* offered to another worker.
PinRelease(c) == /\ pin[c] = "only" /\ c \notin released /\ owner[c] = "none" /\ "a" \notin up
                 /\ released' = released \cup {c}
                 /\ UNCHANGED <<owner,pin,up,started,fleetTiers,left,rebalanced,takenBack>>
\* the rebalance (internal/sprint/rebalance.go Rebalance): a queued card on a
\* worker whose lane works goes to a worker up with an idle lane that may take
\* it; never a started card, never a hard pin, never a worker it left or was
\* taken back from. The code also keeps a preference on the friend it names and
\* the one-tier-up overflow and cheapest-first order; the model lets it move, a
\* superset of the code's moves, so what holds here holds of the code.
Rebalance(c, w) ==
  LET g == owner[c] IN
  /\ g \in Workers /\ w \in up /\ w # g
  /\ (c \notin started \/ BadRebalance = "started")
  /\ pin[c] # "only"
  /\ LaneBusy(g)
  /\ Held(w) = {}
  /\ (Admits(w, CardTier[c]) \/ BadRebalance = "tiers")
  /\ w \notin left[c]
  /\ w \notin takenBack[c]
  /\ owner' = [owner EXCEPT ![c] = w]
  /\ left' = [left EXCEPT ![c] = @ \cup {g}]
  /\ rebalanced' = rebalanced \cup {c}
  /\ UNCHANGED <<pin, up, started, fleetTiers, takenBack, released>>
Presence == /\ up' \in SUBSET Workers /\ UNCHANGED <<owner,pin,started,fleetTiers,left,rebalanced,takenBack,released>>
Next == (\E c \in Cards : Deal(c) \/ (\E w \in Workers : Rebalance(c, w)))
        \/ (\E c \in Cards : Start(c) \/ TakeBack(c) \/ Unpin(c) \/ PinRelease(c)) \/ Presence
Spec == Init /\ [][Next]_vars
\* a hard pin never leaves its friend while the pin is not released past its bound
OnlyToItsFriend == \A c \in Cards : pin[c] = "only" /\ c \notin released => owner[c] \in {"none","a"}
\* every card dealt to a worker sits on a worker whose tier set admits its tier
DealtAdmitted == \A c \in Cards : owner[c] \in Workers => Admits(owner[c], CardTier[c])
\* a card taken back from a friend is never dealt to that friend again
NeverBackToTheSameFriend == \A c \in Cards : \A f \in takenBack[c] : owner[c] # f
\* room: DealAhead (two) times a width of one
WidthRespected == \A w \in Workers : Cardinality(Held(w)) <= 2
LanesRespected == \A w \in Workers : Cardinality({c \in started : owner[c] = w}) <= 1
\* every card the rebalance placed sits on a worker whose tier set admits it
RebalancedAdmitted == \A c \in rebalanced : owner[c] \in Workers => Admits(owner[c], CardTier[c])
\* no started card moves, by any action
NoStartedCardMoves == [][\A c \in started : owner'[c] = owner[c]]_vars
TypeOK == /\ owner \in [Cards -> Workers \cup {"none"}]
          /\ pin \in [Cards -> {"none","preferred","only"}]
          /\ up \subseteq Workers /\ started \subseteq Cards
          /\ fleetTiers \subseteq Tiers
          /\ left \in [Cards -> SUBSET Workers]
          /\ rebalanced \subseteq Cards
          /\ takenBack \in [Cards -> SUBSET Friends]
          /\ released \subseteq Cards
=============================================================================
