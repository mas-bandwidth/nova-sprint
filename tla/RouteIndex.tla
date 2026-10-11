----------------------------- MODULE RouteIndex -----------------------------
\* The deal's route choice (internal/sprint/route.go), written before it is
\* built: the owner, 2026-10-01, "I want model routing to use the same uint64
\* modulo -- all providers/models per-tier, should be an array with this same
\* index approach". It extends the deal of DirtyTick.tla, whose member rule
\* takes a uint64 counter modulo the fleet (deal_index), with one counter per
\* tier over the tier's route array (route_index_flash, route_index_pro on the
\* fleet table); DirtyTick.tla holds which route is taken out of its state, and
\* this module is that choice alone.
\*
\* THE STATE.
\*   ridx      the tiers' route indexes: uint64 counters, never reduced (the
\*             place is the counter modulo the array's length)
\*   st        a card's state: ready, dealt (on a route), or withdrawn (its
\*             take ended: a member lost, a provider failure)
\*   route     the route a card was dealt on last ("none" before its first deal,
\*             "pin" for a card that pins a model)
\*   drawn     the routes taken for a card: what a redeal leaves out
\*   rdl       a card's redeals, bounded by MaxRedeals
\*   memb      the member a card was dealt to ("none" before its first deal, and
\*             for a read, which the ask draws for no member)
\*   deals, skipped, hist, last   ghosts: the tier's cards dealt (not pinned);
\*             the array entries the deals passed over, by the rule; the tier's
\*             picks in order, each with whether it passed over an entry; the
\*             last pick
\*
\* THE RULE (RouteIndexAdvancesOncePerCard, RouteFair, PinNeverAdvances,
\* ExcludedNeverDrawn, FirstPreferred, NeverUnlaunchable). A card of tier t that
\* pins no model takes Arr[t] at the place ridx[t] mod Len(Arr[t]); its deal
\* moves ridx[t] by one. A route with first set is drawn before the others of
\* its tier (preferFirst): the walk takes the first entry from the place whose
\* first is set, and one without it only when none such remains drawable. A
\* redeal and a later attempt leave out the routes already taken for the card
\* (its FieldRoutes, the drawn set) while an entry of the array is not one of
\* them: it takes the first entry from the place that is not left out, and the
\* index moves past every entry it skipped and the one it took. When every
\* entry is left out the exclusion lapses and the entry at the place is taken.
\* The deal walks past an entry the member it deals to cannot launch (its own
\* harness set, MemberUnlaunch) as it walks past a left-out one; a card none of
\* whose pool can launch is never dealt to that member (it waits for one that
\* can, and the tick judges it). A pinned card bypasses the array and moves no
\* index.
\*
\* THE READS (2026-10-01, the owner: a read card carried no route and every
\* reader loop was started by hand with its model). A read card (Reads) is
\* drawn as a work card is, from the reader tier's array at the same index:
\* the ask that creates it takes the entry at the place and moves the index by
\* one, so the work deal and the reads of a tier share one rotation (RouteFair
\* over every pick of the tier). The tier is the primary's collapsed (readTierOf,
\* ReadTier): a pro primary read on flash while pro is unrouted and flash is
\* routed, a heavy primary on pro, a frontier primary on heavy. The routes the
\* read of the primary returned on (Avoid[c]) are left out while another of the
\* tier is served, as a redeal leaves out the routes already taken, and so is a
\* resting route (Rested). A read is never withdrawn and dealt again: a read
\* handed back is asked again on its own card or on a new one, each a deal of
\* its own (internal/sprint Ask).
\*
\* THE MEMBER'S HARNESSES (fault 10, 2026-10-10: cards on heavy-opus-claude were
\* dealt to four members that have no claude, and every
\* launch was refused). MemberUnlaunch[m] is the routes member m cannot launch:
\* a route whose harness is headless (claude, codex, grok) that the member's
\* control card does not name (fleet up --harnesses). The work deal and the
\* redeal walk past such an entry as they walk past one that names no enabled
\* route: it is never taken for that member, however the exclusion of a redeal
\* lapses, and the index moves past it; a card whose tier has no launchable
\* entry is not dealt to that member (it waits for one that can). A read is
\* drawn by the ask, not for a member, and is outside this rule.
\*
\* THE POOL AND ITS JUDGMENT (the cold reads of nova-sprint#51, 2026-10-10). The
\* deal gives a card only to the members of its pool: its bench's (BenchOf: the
\* members its BENCH line names, every member without one), less, for a card dealt
\* again, the members that refused it at staging (Refused). A work card none of
\* whose pool can launch an entry of its tier is stranded: the deal deals it
\* nowhere (Deal and Redeal ask CanLaunchIn the pool), and the tick (TickJudge)
\* judges it, the tier's one judgment (internal/sprint noLauncher over
\* dealPool: StrandedIsJudged). The fault the reads found was the judgment asked
\* over every member up rather than the pool: a bench card whose bench could not
\* launch the route, while a member off its bench could, waited unjudged. tock
\* marks the state just after a tick.
\*
\* Broken: "none" is the design; "random" takes any entry of the array (the
\* weighted draw this replaces: RouteFair fails); "noadvance" is a redeal that
\* takes the entry at the place without moving past the excluded one
\* (ExcludedNeverDrawn fails); "readapart" is a read that takes the entry at
\* the place and leaves the index where it is, a read kept apart from the
\* tier's rotation (RouteIndexAdvancesOncePerCard fails); "harnessblind" is the
\* deal before fault 10, drawing the entry at the place whatever the member can
\* launch (NeverUnlaunchable fails); "judgefleet" judges over every member up,
\* not the card's pool (StrandedIsJudged fails); "nofirst" is the walk that
\* ignores a route's first flag and takes the first entry that is not left out
\* (FirstPreferred fails).
\*
\* THE PIN (v1.2.14, 2026-10-11: a card pinned to anthropic/claude-fable-5-1 was
\* dealt to four members in 22 seconds, its whole redeal bound, each refusing it
\* class=auth for want of a key no member holds; internal/sprint/pin_serve.go). A
\* pinned card is dealt only to a member of its pool that serves its pin: not one
\* of PinBlind (no enabled route of the pin's provider, so no key on the fleet, or
\* the provider resting) and not one whose own take of the pin the provider refused
\* (pinRest, from the refusal's record). Such a refusal (PinRefused) is a
\* coordination failure: the card goes back to ready with no redeal spent and that
\* member's pins rest. A pinned card none of whose pool serves its pin waits ready
\* and the tick judges it (the pin's one judgment: StrandedIsJudged). "pinblind" is
\* the deal before the fix, the pin dealt to any member of its pool
\* (PinDealtServes fails). The rest's end (RouteRestFor after the refusal) is not
\* modelled: pinRest only grows, which is the safe side of the invariant.
\*
\* WHAT IS NOT MODELLED. Members, widths and the tick (DirtyTick.tla); an entry
\* that names no enabled route (the deal skips it as it skips an excluded one,
\* and the Go test pins it); a provider at its concurrency budget, which the
\* deal skips as it skips a resting one, and the fallback that deals a
\* coordinator's rework on a full provider anyway (route.go drawRoute's soft
\* draw); the array changed between deals (config, read once a tick); a plan's
\* dropped unit (round.go's rewrite, the member rule's). The cards of one tier
\* are interchangeable (the instance's symmetry): which card takes an entry does
\* not change what the index does.
EXTENDS Integers, Sequences, FiniteSets, TLC

CONSTANTS Tiers, Arr, Cards, TierOf, Pinned, Reads, MaxRedeals, Broken,
          Members, MemberUnlaunch, BenchOf, Refused, First, Rested, Avoid,
          ReadTier, PinBlind

VARIABLES ridx, st, route, drawn, rdl, deals, skipped, hist, last, judged, tock,
          memb, pinRest

vars == <<ridx, st, route, drawn, rdl, deals, skipped, hist, last, judged, tock,
          memb, pinRest>>

Routes == UNION {{Arr[t][k] : k \in 1..Len(Arr[t])} : t \in Tiers}

\* The entry at the place of counter i in tier t's array.
At(t, i) == Arr[t][(i % Len(Arr[t])) + 1]

\* Whether some entry of tier t's array outside ex has first set: the walk then
\* takes the first such, before the others (preferFirst).
PreferFirst(t, ex) == \E k \in 1..Len(Arr[t]) : Arr[t][k] \notin ex /\ Arr[t][k] \in First

\* The steps a draw from counter i takes, leaving out ex, with no first
\* preference (Broken "nofirst": the walk before preferFirst): one more than the
\* entries it passes over; 1 when every entry is left out (the exclusion lapses).
StepsNoFirst(t, i, ex) ==
  IF \A k \in 1..Len(Arr[t]) : Arr[t][k] \in ex
  THEN 1
  ELSE CHOOSE s \in 1..Len(Arr[t]) :
         /\ At(t, i + s - 1) \notin ex
         /\ \A u \in 1..(s - 1) : At(t, i + u - 1) \in ex

\* The steps a draw from counter i takes, leaving out ex: the first entry from
\* the place that is not left out, a first-set entry before the others
\* (preferFirst); one more than the entries it passes over; 1 when every entry
\* is left out.
StepsFirst(t, i, ex) ==
  IF \A k \in 1..Len(Arr[t]) : Arr[t][k] \in ex
  THEN 1
  ELSE CHOOSE s \in 1..Len(Arr[t]) :
         /\ At(t, i + s - 1) \notin ex
         /\ (PreferFirst(t, ex) => At(t, i + s - 1) \in First)
         /\ \A u \in 1..(s - 1) :
              At(t, i + u - 1) \in ex
              \/ (PreferFirst(t, ex) /\ At(t, i + u - 1) \notin First)

Steps(t, i, ex) == IF Broken = "nofirst" THEN StepsNoFirst(t, i, ex) ELSE StepsFirst(t, i, ex)

\* The tier a draw of c takes its array from: the primary's tier, collapsed for a
\* read (readTierOf, ReadTier: pro to flash while pro is unrouted, heavy to pro).
DrawTier(c) == IF c \in Reads THEN ReadTier[TierOf[c]] ELSE TierOf[c]

\* The routes a draw of c walks past whatever else: those m cannot launch, for a
\* work card (a read is the ask's, drawn for no member), and a resting route
\* (Rested), which is skipped as an unserved one is.
Blind(c, m) == Rested \cup (IF c \in Reads THEN {} ELSE MemberUnlaunch[m])

\* The routes already taken for c that its next draw leaves out: the redeal's
\* drawn set for a work card, the read's avoided routes (Avoid) for a read.
LeftOut(c) == IF c \in Reads THEN Avoid[c] ELSE drawn[c]

\* Whether tier t's array holds an entry outside ex.
Open(t, ex) == \E k \in 1..Len(Arr[t]) : Arr[t][k] \notin ex

\* The work cards: neither pinned nor a read.
WorkCards == Cards \ (Pinned \cup Reads)

\* The members the deal may give card c: its bench's, less those that refused it at
\* staging while it is dealt again (internal/sprint dealPool).
Pool(c) == BenchOf[c] \ (IF st[c] = "withdrawn" THEN Refused[c] ELSE {})

\* Whether a member of P can launch an entry of c's tier (an entry that does not
\* rest, and the member can launch).
CanLaunchIn(c, P) == \E m \in P : Open(TierOf[c], MemberUnlaunch[m] \cup Rested)

\* The members of P that serve the pin: neither blind to it nor resting it.
PinServers(P) == P \ (PinBlind \cup pinRest)

\* A card waiting to be dealt that no member of its pool can launch: a work card
\* whose tier none can launch, or a pinned card whose pin none serves.
Stranded(c) ==
  \/ c \in WorkCards /\ st[c] \in {"ready", "withdrawn"} /\ Pool(c) # {} /\ ~CanLaunchIn(c, Pool(c))
  \/ c \in Pinned /\ st[c] = "ready" /\ Pool(c) # {} /\ PinServers(Pool(c)) = {}

\* The members the tick's judgment asks over (judgefleet: every member).
JudgePool(c) == IF Broken = "judgefleet" THEN Members ELSE Pool(c)

\* A pick: the card, the member, the tier, the route, what it left out, whether an
\* entry was not left out (the exclusion held), and the entries passed over.
Pick(c, r, ex, by, m) == [c |-> c, t |-> DrawTier(c), r |-> r, ex |-> ex,
                          fresh |-> Open(DrawTier(c), ex \cup Blind(c, m)),
                          sk |-> by - 1,
                          m |-> m]

TypeOK ==
  /\ ridx \in [Tiers -> Nat]
  /\ st \in [Cards -> {"ready", "dealt", "withdrawn"}]
  /\ route \in [Cards -> Routes \cup {"none", "pin"}]
  /\ drawn \in [Cards -> SUBSET Routes]
  /\ rdl \in [Cards -> 0..MaxRedeals]
  /\ deals \in [Tiers -> Nat]
  /\ skipped \in [Tiers -> Nat]
  /\ judged \subseteq Cards
  /\ tock \in BOOLEAN
  /\ memb \in [Cards -> Members \cup {"none"}]
  /\ pinRest \subseteq Members

Init ==
  /\ ridx = [t \in Tiers |-> 0]
  /\ st = [c \in Cards |-> "ready"]
  /\ route = [c \in Cards |-> "none"]
  /\ drawn = [c \in Cards |-> {}]
  /\ rdl = [c \in Cards |-> 0]
  /\ deals = [t \in Tiers |-> 0]
  /\ skipped = [t \in Tiers |-> 0]
  /\ hist = [t \in Tiers |-> <<>>]
  /\ last = [c |-> "none", t |-> "none", r |-> "none", ex |-> {}, fresh |-> FALSE, sk |-> 0, m |-> "none"]
  /\ judged = {}
  /\ tock = FALSE
  /\ memb = [c \in Cards |-> "none"]
  /\ pinRest = {}

\* Take route r for card c dealt to member m, the index moved by adv; by is the
\* steps the rule says.
Take(c, m, r, adv, by) ==
  LET t == DrawTier(c) IN
  /\ ridx' = [ridx EXCEPT ![t] = @ + adv]
  /\ route' = [route EXCEPT ![c] = r]
  /\ drawn' = [drawn EXCEPT ![c] = @ \cup {r}]
  /\ deals' = [deals EXCEPT ![t] = @ + 1]
  /\ skipped' = [skipped EXCEPT ![t] = @ + (by - 1)]
  /\ hist' = [hist EXCEPT ![t] = Append(@, [r |-> r, sk |-> by > 1])]
  /\ memb' = [memb EXCEPT ![c] = m]
  /\ last' = Pick(c, r, LeftOut(c), by, m)

\* One deal of card c to member m (m = "none" for a read, the ask's): the entry
\* at the place, or the first from it drawable for m, a first-set entry before
\* the others (preferFirst); the routes already taken for the card (FieldRoutes,
\* or a read's avoided routes) and those m cannot launch or that rest are left out
\* while an entry remains.
DealTo(c, m) ==
  LET t == DrawTier(c)
      b == Blind(c, m)
      ex == IF Open(t, LeftOut(c) \cup b) THEN LeftOut(c) \cup b ELSE b
      s == Steps(t, ridx[t], ex)
  IN
  /\ st[c] = "ready"
  /\ Broken = "harnessblind" \/ Open(t, b)
  /\ st' = [st EXCEPT ![c] = "dealt"]
  /\ tock' = FALSE
  /\ UNCHANGED <<rdl, judged, pinRest>>
  /\ IF Broken = "random"
     THEN \E k \in 1..Len(Arr[t]) : Take(c, m, Arr[t][k], 1, 1)
     ELSE IF Broken = "readapart" /\ c \in Reads
     THEN Take(c, m, At(t, ridx[t]), 0, 1)
     ELSE IF Broken = "harnessblind"
     THEN Take(c, m, At(t, ridx[t]), 1, 1)
     ELSE Take(c, m, At(t, ridx[t] + s - 1), s, s)

\* The deal of a ready card that pins no model: a read to no member, a work card
\* to a member of its pool that can launch it.
Deal(c) ==
  /\ c \notin Pinned
  /\ \/ /\ c \in Reads
        /\ DealTo(c, "none")
     \/ /\ c \in WorkCards
        /\ \E m \in Pool(c) : DealTo(c, m)

\* A pinned card is dealt on its pin, to a member of its pool that serves it
\* (pinblind: to any member of its pool): the array and the index untouched.
Pin(c) ==
  /\ c \in Pinned
  /\ st[c] = "ready"
  /\ \E m \in (IF Broken = "pinblind" THEN Pool(c) ELSE PinServers(Pool(c))) :
       memb' = [memb EXCEPT ![c] = m]
  /\ st' = [st EXCEPT ![c] = "dealt"]
  /\ route' = [route EXCEPT ![c] = "pin"]
  /\ tock' = FALSE
  /\ UNCHANGED <<ridx, drawn, rdl, deals, skipped, hist, last, judged, pinRest>>

\* The provider refuses the pin's take for its key or credit on the member: a
\* coordination failure. The card goes back to ready with no redeal spent, and the
\* member's pins of the provider rest.
PinRefused(c) ==
  /\ c \in Pinned
  /\ st[c] = "dealt"
  /\ st' = [st EXCEPT ![c] = "ready"]
  /\ pinRest' = pinRest \cup {memb[c]}
  /\ tock' = FALSE
  /\ UNCHANGED <<ridx, route, drawn, rdl, deals, skipped, hist, last, judged, memb>>

\* The card's take ends without a finish (its member lost, the provider failed):
\* withdrawn, to be dealt again, while its redeals are under the bound.
Withdraw(c) ==
  /\ c \notin Pinned \cup Reads
  /\ st[c] = "dealt"
  /\ rdl[c] < MaxRedeals
  /\ st' = [st EXCEPT ![c] = "withdrawn"]
  /\ tock' = FALSE
  /\ UNCHANGED <<ridx, route, drawn, rdl, deals, skipped, hist, last, judged, memb, pinRest>>

\* One redeal of card c to member m: the next entry from the place that is not
\* left out, the index moved past it and every entry skipped. The routes already
\* taken are left out while a launchable entry is not one of them (else that
\* exclusion lapses); an entry the member cannot launch or that rests is never
\* taken.
RedealTo(c, m) ==
  LET t == DrawTier(c)
      b == Blind(c, m)
      ex == IF Open(t, LeftOut(c) \cup b) THEN LeftOut(c) \cup b ELSE b
      s == Steps(t, ridx[t], ex)
  IN
  /\ st[c] = "withdrawn"
  /\ Broken = "harnessblind" \/ Open(t, b)
  /\ st' = [st EXCEPT ![c] = "dealt"]
  /\ rdl' = [rdl EXCEPT ![c] = @ + 1]
  /\ tock' = FALSE
  /\ UNCHANGED <<judged, pinRest>>
  /\ IF Broken = "noadvance"
     THEN Take(c, m, At(t, ridx[t]), 1, s)
     ELSE IF Broken = "harnessblind"
     THEN LET sb == Steps(t, ridx[t], drawn[c]) IN Take(c, m, At(t, ridx[t] + sb - 1), sb, sb)
     ELSE Take(c, m, At(t, ridx[t] + s - 1), s, s)

\* The redeal of a withdrawn card, to a member of its pool that can launch it.
Redeal(c) ==
  /\ c \in WorkCards
  /\ \E m \in Pool(c) : RedealTo(c, m)

\* The tick's judgment: every work card waiting to be dealt that no member of its
\* pool can launch, the tier's one judgment (TickDeal; judgefleet: asked over every
\* member).
TickJudge ==
  /\ judged' = {c \in WorkCards : st[c] \in {"ready", "withdrawn"} /\ JudgePool(c) # {} /\ ~CanLaunchIn(c, JudgePool(c))}
                \cup {c \in Pinned : st[c] = "ready" /\ JudgePool(c) # {} /\ PinServers(JudgePool(c)) = {}}
  /\ tock' = TRUE
  /\ UNCHANGED <<ridx, st, route, drawn, rdl, deals, skipped, hist, last, memb, pinRest>>

\* A card that finishes is one that is not withdrawn again: no action of its own.
Next == TickJudge \/ \E c \in Cards : Deal(c) \/ Pin(c) \/ PinRefused(c) \/ Withdraw(c) \/ Redeal(c)

Spec == Init /\ [][Next]_vars

\* After N cards of a tier dealt, the index is N, and one more for every entry
\* the rule passed over (none when no redeal left a route out): the place is
\* N mod Len(Arr[t]).
RouteIndexAdvancesOncePerCard == \A t \in Tiers : ridx[t] = deals[t] + skipped[t]

\* Over any Len(Arr[t]) consecutive picks of a tier that passed over no entry,
\* every entry of the array is taken exactly once: each route as many times as
\* the array names it. This is the walk when no route of the tier has first set.
Count(s, r) == Cardinality({j \in 1..Len(s) : s[j] = r})
RouteFair ==
  \A t \in Tiers :
    LET n == Len(Arr[t]) IN
    \A i \in 1..(Len(hist[t]) - n + 1) :
      (\A j \in i..(i + n - 1) : ~hist[t][j].sk) =>
        \A r \in Routes :
          Cardinality({j \in i..(i + n - 1) : hist[t][j].r = r}) = Count(Arr[t], r)

\* A pinned card's deal moves no index.
PinNeverAdvances == [][\A c \in Pinned : (st[c] = "ready" /\ st'[c] = "dealt") => ridx' = ridx]_vars

\* A redeal never takes a route left out while an entry of the array is not.
ExcludedNeverDrawn == last.fresh => last.r \notin last.ex

\* A work card is never dealt on a route its member cannot launch (fault 10).
NeverUnlaunchable == last.c \in WorkCards => last.r \notin MemberUnlaunch[last.m]

\* A draw never takes a non-first route while a first-set entry is drawable
\* (preferFirst).
FirstPreferred ==
  last.c \in Cards =>
    LET ex == IF last.fresh THEN last.ex \cup Blind(last.c, last.m) ELSE Blind(last.c, last.m)
    IN last.r \in First \/ ~PreferFirst(last.t, ex)

\* Just after a tick, every stranded card is judged (the cold reads of nova-sprint#51).
StrandedIsJudged == tock => \A c \in Cards : Stranded(c) => c \in judged

\* A dealt pinned card's member serves the pin (pin_serve.go): it is not blind to
\* it and its pins of the provider do not rest. One pinned card in the instance: a
\* second card's refusal on the same member is its own coordination failure.
PinDealtServes == \A c \in Pinned : st[c] = "dealt" => memb[c] \notin PinBlind \cup pinRest

\* A pinned card's refusals spend no redeal.
PinSpendsNoRedeal == \A c \in Pinned : rdl[c] = 0

\* Reachability (a reversed witness, written to be false where the design must
\* reach): a provider refused a pin on a member, so its pins rest there.
ReachPinRest == pinRest = {}

\* Reachability (a reversed witness, written to be false where the design must
\* reach): a redeal that passed over an entry it left out.
ReachSkip == \A t \in Tiers : skipped[t] = 0
=============================================================================
