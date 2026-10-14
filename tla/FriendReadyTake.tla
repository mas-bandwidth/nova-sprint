------------------------ MODULE FriendReadyTake ------------------------
EXTENDS Naturals, FiniteSets
\* Moved unchanged in meaning from nova-tools dev 2314d437f. Historical
\* abstraction: current friendDealPass awaits a start receipt and TickDeadlines
\* skips friend rows; COVERAGE.tsv records these gaps, not a current proof.
\* docs/SPEC-SPRINT.md section 1, a friend takes her own ready cards
\* (internal/sprint/steps_work.go:1708 takeOne, friend_deal.go:586 friendDealPass,
\* steps_work.go:2101 friendNext, friend_take.go:214 FriendReadyMax and :220
\* friendLaneIdle, steps_tick.go:1526 TickDeadlines). One friend's
\* row, "a", in batch or one-shot mode (the rules are one for both: one lane
\* per unit of width), and a stranger, "b". Cards are dealt to her row
\* into working while a lane is free and ready behind otherwise, within her
\* room (twice her width). Her width can rise (a lane frees with no finish).
\* Who moves a ready card into working: the tick's deal (it fills a free
\* lane from her ready cards first), her own take by count of cards she has
\* started (within her lanes, her own row only, only while she is up), and
\* her finish (her next). A ready card in front of a free lane ages a tick at
\* a time while no one takes it, and at Max it is judged.
CONSTANTS Cards, MaxWidth, Max, BadNoFill, BadPastWidth, BadStranger, BadNoJudge
BadUnstartedTake == FALSE
Workers == {"a", "b"}
VARIABLES col, taker, started, width, up, age, judged
vars == <<col, taker, started, width, up, age, judged>>

Working == {c \in Cards : col[c] = "working"}
Ready == {c \in Cards : col[c] = "ready"}
LaneFree == Cardinality(Working) < width
Room == Cardinality(Working) + Cardinality(Ready) < 2 * width

Init == /\ col = [c \in Cards |-> "new"]
        /\ taker = [c \in Cards |-> "none"]
        /\ started = [c \in Cards |-> FALSE]
        /\ width = 1
        /\ up = TRUE
        /\ age = [c \in Cards |-> 0]
        /\ judged = {}

\* the friend's deal of one new card (friendDealPass): working on a free lane, else ready (unstarted on deal)
Deal(c) == /\ col[c] = "new" /\ up /\ Room
           /\ col' = [col EXCEPT ![c] = IF LaneFree THEN "working" ELSE "ready"]
           /\ taker' = [taker EXCEPT ![c] = IF LaneFree THEN "deal" ELSE "none"]
           /\ started' = [started EXCEPT ![c] = LaneFree]
           /\ UNCHANGED <<width, up, age, judged>>

\* her start on a ready card of hers (friend_start.go friendStarted, lane_hold.go laneSet)
FriendStart(c) == /\ col[c] = "ready"
                  /\ ~started[c]
                  /\ started' = [started EXCEPT ![c] = TRUE]
                  /\ UNCHANGED <<col, taker, width, up, age, judged>>

\* the tick: the deal first takes her ready cards into her free
\* lanes, oldest first; a ready card still in front of a free lane ages, and is
\* judged at Max (FriendReadyMax)
Fill == IF BadNoFill \/ ~up THEN {}
        ELSE CHOOSE s \in SUBSET Ready : Cardinality(s) = IF Cardinality(Ready) < width - Cardinality(Working)
                                                           THEN Cardinality(Ready) ELSE width - Cardinality(Working)
Tick == LET fill == Fill
            col1 == [c \in Cards |-> IF c \in fill THEN "working" ELSE col[c]]
            free == Cardinality({c \in Cards : col1[c] = "working"}) < width
        IN /\ col' = col1
           /\ taker' = [c \in Cards |-> IF c \in fill THEN "deal" ELSE taker[c]]
           /\ started' = [c \in Cards |-> IF c \in fill THEN TRUE ELSE started[c]]
           /\ age' = [c \in Cards |-> IF col1[c] = "ready" /\ free THEN IF age[c] < Max THEN age[c] + 1 ELSE Max ELSE 0]
           /\ judged' = IF BadNoJudge THEN judged
                        ELSE judged \cup {c \in Cards : col1[c] = "ready" /\ free /\ age'[c] >= Max}
           /\ UNCHANGED <<width, up>>

\* take --as friend.a <count>: her take by count moves only cards she has started
\* (steps_work.go takeOne) and stamps started' = [c \in Cards |-> IF c \in S THEN TRUE ELSE started[c]]
\* (under BadUnstartedTake, the pre-fix takeOne moved unstarted cards with friendTaken, leaving started unchanged)
FriendTake(S) == /\ S \in SUBSET Ready
                 /\ S # {}
                 /\ (BadUnstartedTake \/ S \subseteq {c \in Ready : started[c]})
                 /\ up
                 /\ (Cardinality(Working) + Cardinality(S) <= width \/ BadPastWidth)
                 /\ col' = [c \in Cards |-> IF c \in S THEN "working" ELSE col[c]]
                 /\ taker' = [c \in Cards |-> IF c \in S THEN "a" ELSE taker[c]]
                 /\ started' = IF BadUnstartedTake THEN started
                               ELSE [c \in Cards |-> IF c \in S THEN TRUE ELSE started[c]]
                 /\ age' = [c \in Cards |-> IF c \in S THEN 0 ELSE age[c]]
                 /\ UNCHANGED <<width, up, judged>>

\* take --as friend.<w> c: her own row only, while she is up, within her lanes
Take(w, c) == /\ col[c] = "ready"
              /\ (w = "a" \/ BadStranger)
              /\ (BadUnstartedTake \/ started[c])
              /\ up
              /\ (LaneFree \/ BadPastWidth)
              /\ col' = [col EXCEPT ![c] = "working"]
              /\ taker' = [taker EXCEPT ![c] = w]
              /\ started' = [started EXCEPT ![c] = IF BadUnstartedTake THEN started[c] ELSE TRUE]
              /\ age' = [age EXCEPT ![c] = 0]
              /\ UNCHANGED <<width, up, judged>>

\* her finish, which takes her oldest ready card into the lane it frees (friendNext)
Finish(c) == /\ col[c] = "working"
             /\ \/ /\ Ready = {}
                   /\ col' = [col EXCEPT ![c] = "done"]
                   /\ taker' = taker
                   /\ started' = started
                   /\ age' = age
                \/ \E n \in Ready :
                   /\ col' = [col EXCEPT ![c] = "done", ![n] = "working"]
                   /\ taker' = [taker EXCEPT ![n] = "finish"]
                   /\ started' = [started EXCEPT ![n] = TRUE]
                   /\ age' = [age EXCEPT ![n] = 0]
             /\ UNCHANGED <<width, up, judged>>

Raise == /\ width < MaxWidth
         /\ width' = width + 1
         /\ UNCHANGED <<col, taker, started, up, age, judged>>

Presence == /\ up' = ~up
            /\ UNCHANGED <<col, taker, started, width, age, judged>>

Next == \/ \E c \in Cards : Deal(c) \/ FriendStart(c) \/ Finish(c)
        \/ \E S \in SUBSET Cards : FriendTake(S)
        \/ \E w \in Workers, c \in Cards : Take(w, c)
        \/ Tick \/ Raise \/ Presence

Spec == Init /\ [][Next]_vars

TypeOK == /\ col \in [Cards -> {"new", "ready", "working", "done"}]
          /\ taker \in [Cards -> {"none", "deal", "finish", "a", "b"}]
          /\ started \in [Cards -> BOOLEAN]
          /\ width \in 1..MaxWidth /\ up \in BOOLEAN
          /\ age \in [Cards -> 0..Max] /\ judged \subseteq Cards
\* her lanes are hard, as a member's width
LanesHard == Cardinality(Working) <= width
\* no one but she, her deal or her finish takes a card on her row
OnlyHerTake == \A c \in Cards : taker[c] \notin {"b"}
\* a ready card in front of a free lane for Max ticks is judged (never silent)
NeverStrandedSilently == \A c \in Cards : (col[c] = "ready" /\ age[c] >= Max) => c \in judged
\* a friend's take by count never moves an unstarted card into working
NoUnstartedMovedByCount == \A c \in Working : started[c]
\* with her up, a tick leaves no card ready in front of a free lane
FilledAfterTick == [][Tick /\ up => ~(Ready' # {} /\ Cardinality(Working') < width')]_vars
=============================================================================
