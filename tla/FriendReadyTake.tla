------------------------ MODULE FriendReadyTake ------------------------
EXTENDS Naturals, FiniteSets
\* Moved unchanged in meaning from nova-tools dev 2314d437f. Historical
\* abstraction: current friendDealPass awaits a start receipt and TickDeadlines
\* skips friend rows; COVERAGE.tsv records these gaps, not a current proof.
\* docs/SPEC-SPRINT.md section 1, a friend takes her own ready cards
\* (internal/sprint/steps_work.go:1486 takeOne, friend_deal.go:586 friendDealPass,
\* steps_work.go:2101 friendNext, friend_take.go:214 FriendReadyMax and :220
\* friendLaneIdle, steps_tick.go:1526 TickDeadlines). One friend's
\* row, "a", in batch or one-shot mode (the rules are one for both: one lane
\* per unit of width), and a stranger, "b". Cards are dealt to her row
\* into working while a lane is free and ready behind otherwise, within her
\* room (twice her width). Her width can rise (a lane frees with no finish).
\* Who moves a ready card into working: the tick's deal (it fills a free
\* lane from her ready cards first), her own take (within her lanes, her own
\* row only, only while she is up), and her finish (her next). A ready card in
\* front of a free lane ages a tick at a time while no one takes it, and at Max
\* it is judged. The model checks who may take and the bound; not the stamps,
\* the transport or the level.
CONSTANTS Cards, MaxWidth, Max, BadNoFill, BadPastWidth, BadStranger, BadNoJudge, BadUnstartedTake
Workers == {"a", "b"}
VARIABLES col, taker, width, up, age, judged, started
vars == <<col, taker, width, up, age, judged, started>>

Working == {c \in Cards : col[c] = "working"}
Ready == {c \in Cards : col[c] = "ready"}
StartedReady == {c \in Ready : c \in started}
LaneFree == Cardinality(Working) < width
Room == Cardinality(Working) + Cardinality(Ready) < 2 * width

Init == /\ col = [c \in Cards |-> "new"]
        /\ taker = [c \in Cards |-> "none"]
        /\ width = 1
        /\ up = TRUE
        /\ age = [c \in Cards |-> 0]
        /\ judged = {}
        /\ started = {}

\* the friend's deal of one new card (friendDealPass): ready on her row,
\* whatever her lanes, until she starts it (friend_start.go)
Deal(c) == /\ col[c] = "new" /\ up /\ Room
           /\ col' = [col EXCEPT ![c] = "ready"]
           /\ taker' = [taker EXCEPT ![c] = "none"]
           /\ UNCHANGED <<width, up, age, judged, started>>

\* her start (friendStarted/friendStartUnits): she has begun a ready card;
\* it may now move to working
Start(c) == /\ col[c] = "ready"
            /\ started' = started \cup {c}
            /\ UNCHANGED <<col, taker, width, up, age, judged>>

\* the tick: the deal first takes her started ready cards into her free
\* lanes, oldest first; a ready card still in front of a free lane ages, and is
\* judged at Max (FriendReadyMax)
Fill == IF BadNoFill \/ ~up THEN {}
        ELSE CHOOSE s \in SUBSET StartedReady : Cardinality(s) = IF Cardinality(StartedReady) < width - Cardinality(Working)
                                                           THEN Cardinality(StartedReady) ELSE width - Cardinality(Working)
Tick == LET fill == Fill
            col1 == [c \in Cards |-> IF c \in fill THEN "working" ELSE col[c]]
            free == Cardinality({c \in Cards : col1[c] = "working"}) < width
        IN /\ col' = col1
           /\ taker' = [c \in Cards |-> IF c \in fill THEN "deal" ELSE taker[c]]
           /\ age' = [c \in Cards |-> IF col1[c] = "ready" /\ free THEN IF age[c] < Max THEN age[c] + 1 ELSE Max ELSE 0]
           /\ judged' = IF BadNoJudge THEN judged
                        ELSE judged \cup {c \in Cards : col1[c] = "ready" /\ free /\ age'[c] >= Max}
           /\ UNCHANGED <<width, up, started>>

\* take --as friend.<w> by count: her own row only, while she is up, within
\* her lanes, and only a card she has started (steps_work.go Take, by count;
\* BadUnstartedTake drops this guard: the reversed witness)
Take(w, c) == /\ col[c] = "ready"
              /\ (w = "a" \/ BadStranger)
              /\ up
              /\ (LaneFree \/ BadPastWidth)
              /\ (c \in started \/ BadUnstartedTake)
              /\ col' = [col EXCEPT ![c] = "working"]
              /\ taker' = [taker EXCEPT ![c] = w]
              /\ age' = [age EXCEPT ![c] = 0]
              /\ UNCHANGED <<width, up, judged, started>>

\* her finish, which takes her oldest started ready card into the lane it frees (friendNext)
Finish(c) == /\ col[c] = "working"
             /\ \/ /\ Ready = {}
                   /\ col' = [col EXCEPT ![c] = "done"]
                   /\ taker' = taker
                   /\ age' = age
                \/ \E n \in StartedReady :
                   /\ col' = [col EXCEPT ![c] = "done", ![n] = "working"]
                   /\ taker' = [taker EXCEPT ![n] = "finish"]
                   /\ age' = [age EXCEPT ![n] = 0]
             /\ UNCHANGED <<width, up, judged, started>>

Raise == /\ width < MaxWidth
         /\ width' = width + 1
         /\ UNCHANGED <<col, taker, up, age, judged, started>>

Presence == /\ up' = ~up
            /\ UNCHANGED <<col, taker, width, age, judged, started>>

Next == \/ \E c \in Cards : Deal(c) \/ Finish(c) \/ Start(c)
        \/ \E w \in Workers, c \in Cards : Take(w, c)
        \/ Tick \/ Raise \/ Presence

Spec == Init /\ [][Next]_vars

TypeOK == /\ col \in [Cards -> {"new", "ready", "working", "done"}]
          /\ taker \in [Cards -> {"none", "deal", "finish", "a", "b"}]
          /\ width \in 1..MaxWidth /\ up \in BOOLEAN
          /\ age \in [Cards -> 0..Max] /\ judged \subseteq Cards
          /\ started \subseteq Cards
\* her lanes are hard, as a member's width
LanesHard == Cardinality(Working) <= width
\* no one but she, her deal or her finish takes a card on her row
OnlyHerTake == \A c \in Cards : taker[c] \notin {"b"}
\* working on her row means she started it
WorkingIsStarted == \A c \in Working : c \in started
\* a ready card in front of a free lane for Max ticks is judged (never silent)
NeverStrandedSilently == \A c \in Cards : (col[c] = "ready" /\ age[c] >= Max) => c \in judged
\* with her up, a tick leaves no started card ready in front of a free lane
FilledAfterTick == [][Tick /\ up => ~(StartedReady' # {} /\ Cardinality(Working') < width')]_vars
=============================================================================
