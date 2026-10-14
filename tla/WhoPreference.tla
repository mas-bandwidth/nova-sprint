-------------------------- MODULE WhoPreference --------------------------
EXTENDS Naturals, FiniteSets, TLC
\* docs/SPEC-SPRINT.md section 1, a friend's card and the rebalance
\* (internal/sprint/friend_deal.go, friend_take.go, rebalance.go, stops.go):
\* one ready card of each kind, two subscription friends, one fleet worker, one
\* lane each and room for DealAhead (two) cards each. A deal consumes room
\* atomically; up/down changes eligibility. The friends' deal offers a card to
\* the friend its WHO line names first (a named pin: WHO: friend a, or a hard
\* pin: WHO: only friend a), then to the friends up whose tiers hold its tier,
\* an idle lane first, then the most room, then the first by name, then to the
\* fleet, whose tier set (fleetTiers) holds its tier. Every deal is gated on the
\* worker's tier set (friendTakes for a friend, the fleet's set for the fleet):
\* a flash card never reaches a friend whose tiers are pro alone. A hard pin
\* waits ready for its friend alone until it is released past its bound while
\* she is not up, then it is offered on as a named pin is (OnlyToItsFriend holds
\* only before the release). A card taken back from a friend (friend take) is
\* never dealt to her again (takenBack). The rebalance moves a card dealt and
\* not started, queued on a worker whose lane works, to a worker up with an idle
\* lane (no card held) whose tier set admits the card's tier and that the card
\* has not left. The model checks selection, not transport, route rotation or
\* retry bounds; the pin release's bound is the tick's step, as
\* tla/FriendDealtReturn.tla reads the dealt bound.
\* BadOnly is the reversed witness: "only" lets a hard pin fall through to the
\* general deal (OnlyToItsFriend breaks); BadRebalance "started" lets the
\* rebalance move a started card (NoStartedCardMoves breaks), "tiers" lets it
\* ignore the tier set (RebalancedAdmitted breaks); BadTier "anytier" drops the
\* deal's tier gate (DealtAdmitted breaks); BadBack "dealback" redeals a
\* taken-back card to the same friend (NeverBackToTheSameFriend breaks); "none"
\* is the code's rule; BadGive "pin" lets friend give hand a hard pin to a friend it does not
\* name (OnlyToItsFriend breaks), "none" is the code's rule.
CONSTANT BadOnly, BadRebalance, BadTier, BadBack, BadGive
Cards == {"plain", "preferred", "only"}
Friends == {"a", "b"}
Workers == Friends \cup {"fleet"}
Tiers == {"flash", "pro"}
\* the hard pin is a pro card, so the second friend (pro alone) could take it: a give that ignored the
\* pin would reach her (BadGive "pin")
CardTier == [c \in Cards |-> IF c \in {"preferred", "only"} THEN "pro" ELSE "flash"]
FriendTiers == [f \in Friends |-> IF f = "a" THEN {"flash", "pro"} ELSE {"pro"}]
VARIABLES owner, pin, up, started, fleetTiers, left, takenBack, rebalanced, released
vars == <<owner, pin, up, started, fleetTiers, left, takenBack, rebalanced, released>>
Init == /\ owner = [c \in Cards |-> "none"]
        /\ pin = [c \in Cards |-> IF c = "plain" THEN "none" ELSE c]
        /\ up \in SUBSET Workers
        /\ started = {}
        /\ fleetTiers \in SUBSET Tiers
        /\ left = [c \in Cards |-> {}]
        /\ takenBack = [c \in Cards |-> {}]
        /\ rebalanced = {}
        /\ released = {}
Held(w) == {c \in Cards : owner[c] = w}
Room(w) == w \in up /\ Cardinality(Held(w)) < 2
Admits(w, t) == IF w = "fleet" THEN t \in fleetTiers ELSE t \in FriendTiers[w]
\* a card's named friend (its WHO line): "a" for a named or hard pin, else "none"
Named(c) == IF pin[c] \in {"preferred", "only"} THEN "a" ELSE "none"
\* a hard pin still waiting: only friend, not released, not broken by the witness
Hard(c) == pin[c] = "only" /\ c \notin released /\ ~BadOnly
\* the deal's tier gate: a worker's tier set holds the tier, dropped by "anytier"
DealAdmits(w, t) == Admits(w, t) \/ BadTier = "anytier"
\* a friend the deal may hand c to: up with room, her tiers hold its tier, and
\* it has not been taken back from her (dropped by "dealback")
MayFriend(c, w) == /\ w \in Friends
                   /\ Room(w)
                   /\ DealAdmits(w, CardTier[c])
                   /\ (w \notin takenBack[c] \/ BadBack = "dealback")
\* the friend the deal picks among those it may hand c to: an idle lane first,
\* then the most room, then the first by name. With one lane each and room for
\* two, idle lanes and room both fall as the held count, so the fewest held
\* cards first, the first name among equals.
Preferred(c) ==
  IF MayFriend(c, "a") /\ MayFriend(c, "b")
  THEN IF Cardinality(Held("a")) <= Cardinality(Held("b")) THEN "a" ELSE "b"
  ELSE IF MayFriend(c, "a") THEN "a" ELSE "b"
Candidate(c) ==
  IF Hard(c) THEN (IF MayFriend(c, "a") THEN {"a"} ELSE {})
  ELSE IF Named(c) = "a" /\ MayFriend(c, "a") THEN {"a"}
  ELSE IF MayFriend(c, "a") \/ MayFriend(c, "b") THEN {Preferred(c)}
  ELSE IF Room("fleet") /\ DealAdmits("fleet", CardTier[c]) THEN {"fleet"}
  ELSE {}
Deal(c,w) == /\ owner[c] = "none" /\ w \in Candidate(c)
             /\ owner' = [owner EXCEPT ![c] = w]
             /\ rebalanced' = rebalanced \ {c}
             /\ UNCHANGED <<pin, up, started, fleetTiers, left, takenBack, released>>
LaneBusy(w) == \E d \in started : owner[d] = w
Start(c) == /\ owner[c] \in Workers /\ c \notin started /\ ~LaneBusy(owner[c])
            /\ started' = started \cup {c}
            /\ UNCHANGED <<owner,pin,up,fleetTiers,left,takenBack,rebalanced,released>>
\* friend take: a dealt but unstarted card goes back to ready, and the deal never
\* hands it to that friend again (takenBack). Started work cannot take this
\* transition.
TakeBack(c) == /\ owner[c] \in Friends /\ c \notin started
               /\ owner' = [owner EXCEPT ![c] = "none"]
               /\ takenBack' = [takenBack EXCEPT ![c] = @ \cup {owner[c]}]
               /\ rebalanced' = rebalanced \ {c}
               /\ UNCHANGED <<pin,up,started,fleetTiers,left,released>>
\* friend give (internal/sprint/friend_take.go FriendGive): the seat hands a named card that is
\* back in ready (never dealt, taken back from a friend, or withdrawn by a hold) to a friend
\* up whose tiers hold its tier, whoever it was taken back from, and the hand clears the
\* memory of her (takenBack): the way back to the friend it was taken from. A hard pin is
\* handed to the friend it names alone. The model gives within her room; the code's hand
\* does not count room (the seat's hand), a superset the model leaves out.
Give(c,w) == /\ owner[c] = "none" /\ c \notin started
             /\ w \in Friends /\ Room(w)
             /\ DealAdmits(w, CardTier[c])
             /\ (BadGive = "pin" \/ ~Hard(c) \/ w = "a")
             /\ owner' = [owner EXCEPT ![c] = w]
             /\ takenBack' = [takenBack EXCEPT ![c] = @ \ {w}]
             /\ rebalanced' = rebalanced \ {c}
             /\ UNCHANGED <<pin, up, started, fleetTiers, left, released>>
Unpin(c) == /\ owner[c] = "none" /\ c \notin started /\ pin[c] # "none"
            /\ pin' = [pin EXCEPT ![c] = "none"]
            /\ UNCHANGED <<owner,up,started,fleetTiers,left,takenBack,rebalanced,released>>
\* a hard pin released past its bound (the tick's step) while its friend is not
\* up: it is a preference from then on, offered on as a named pin is.
Release(c) == /\ pin[c] = "only" /\ c \notin released
              /\ owner[c] = "none" /\ "a" \notin up
              /\ released' = released \cup {c}
              /\ UNCHANGED <<owner,pin,up,started,fleetTiers,left,takenBack,rebalanced>>
\* the rebalance (internal/sprint/rebalance.go Rebalance): a queued card on a
\* worker whose lane works goes to a worker up with an idle lane that may take
\* it; never a started card, never a hard pin, never a worker it left or one it
\* was taken back from. The one-tier-up overflow and cheapest-first order of
\* rebalance.go:143-188 are left out (two tiers have no tier above pro), so the
\* model lets it move, a superset of the code's moves, and what holds here holds
\* of the code.
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
Presence == /\ up' \in SUBSET Workers /\ UNCHANGED <<owner,pin,started,fleetTiers,left,takenBack,rebalanced,released>>
Next == (\E c \in Cards, w \in Workers : Deal(c,w) \/ Rebalance(c,w))
        \/ (\E c \in Cards, w \in Workers : Give(c,w))
        \/ (\E c \in Cards : Start(c) \/ TakeBack(c) \/ Unpin(c) \/ Release(c)) \/ Presence
Spec == Init /\ [][Next]_vars
OnlyToItsFriend == \A c \in Cards : pin[c] = "only" /\ c \notin released => owner[c] \in {"none","a"}
\* every card the deal placed sits on a worker whose tier set admits its tier
DealtAdmitted == \A c \in Cards : owner[c] \in Workers => Admits(owner[c], CardTier[c])
\* a card taken back from a friend is never dealt to her again
NeverBackToTheSameFriend == \A c \in Cards : owner[c] \in Friends => owner[c] \notin takenBack[c]
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
          /\ takenBack \in [Cards -> SUBSET Friends]
          /\ rebalanced \subseteq Cards
          /\ released \subseteq Cards
=============================================================================
