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
\* Started (2026-10-14, nova-sprint #268; docs/SPEC-SPRINT.md section 1): her beat
\* names a card running (Start, friendStarted). A take by count on her row (no ids, no
\* lane: steps_work.go:1623 takeOne, the friendByCount filter) moves only cards she has
\* started; the tick's return (friend_start.go:103 friendUnstartedWorking) puts a
\* working card she has not started back ready. The deal's working cards stand for the
\* start receipt the current deal awaits, so the return leaves them alone here.
\* BadUnstartedTake drops the take's guard, as the code before #268 did: the take moved
\* unstarted cards and the return put them back, a flap every BatchTakeEvery.
CONSTANTS Cards, MaxWidth, Max, BadNoFill, BadPastWidth, BadStranger, BadNoJudge, BadUnstartedTake
Workers == {"a", "b"}
VARIABLES col, taker, width, up, age, judged, started
vars == <<col, taker, width, up, age, judged, started>>

Working == {c \in Cards : col[c] = "working"}
Ready == {c \in Cards : col[c] = "ready"}
LaneFree == Cardinality(Working) < width
Room == Cardinality(Working) + Cardinality(Ready) < 2 * width

Init == /\ col = [c \in Cards |-> "new"]
        /\ taker = [c \in Cards |-> "none"]
        /\ width = 1
        /\ up = TRUE
        /\ age = [c \in Cards |-> 0]
        /\ judged = {}
        /\ started = {}

\* the friend's deal of one new card (friendDealPass): working on a free lane, else ready
Deal(c) == /\ col[c] = "new" /\ up /\ Room
           /\ col' = [col EXCEPT ![c] = IF LaneFree THEN "working" ELSE "ready"]
           /\ taker' = [taker EXCEPT ![c] = IF LaneFree THEN "deal" ELSE "none"]
           /\ UNCHANGED <<width, up, age, judged, started>>

\* the tick: first the return (friendUnstartedWorking): a card working on her row
\* that she has not started, moved there by a take or her finish's next, goes back
\* ready; then the deal takes her ready cards into her free lanes, oldest first; a
\* ready card still in front of a free lane ages, and is judged at Max (FriendReadyMax)
Returned == {c \in Working : c \notin started /\ taker[c] # "deal"}
Fill(cl) == LET rd == {c \in Cards : cl[c] = "ready"}
                wk == {c \in Cards : cl[c] = "working"}
            IN IF BadNoFill \/ ~up THEN {}
               ELSE CHOOSE s \in SUBSET rd : Cardinality(s) = IF Cardinality(rd) < width - Cardinality(wk)
                                                              THEN Cardinality(rd) ELSE width - Cardinality(wk)
Tick == LET ret == Returned
            col0 == [c \in Cards |-> IF c \in ret THEN "ready" ELSE col[c]]
            fill == Fill(col0)
            col1 == [c \in Cards |-> IF c \in fill THEN "working" ELSE col0[c]]
            free == Cardinality({c \in Cards : col1[c] = "working"}) < width
        IN /\ col' = col1
           /\ taker' = [c \in Cards |-> IF c \in fill THEN "deal" ELSE IF c \in ret THEN "none" ELSE taker[c]]
           /\ age' = [c \in Cards |-> IF col1[c] = "ready" /\ free THEN IF age[c] < Max THEN age[c] + 1 ELSE Max ELSE 0]
           /\ judged' = IF BadNoJudge THEN judged
                        ELSE judged \cup {c \in Cards : col1[c] = "ready" /\ free /\ age'[c] >= Max}
           /\ UNCHANGED <<width, up, started>>

\* take --as friend.<w> [c]: her own row only, while she is up, within her lanes;
\* byCount is a take with no ids and no lane, which moves c only if she has started it
\* (stamped "count"); a take by id or a lane's take is unchanged
Take(w, c, byCount) == /\ col[c] = "ready"
              /\ (w = "a" \/ BadStranger)
              /\ up
              /\ (LaneFree \/ BadPastWidth)
              /\ (~byCount \/ c \in started \/ BadUnstartedTake)
              /\ col' = [col EXCEPT ![c] = "working"]
              /\ taker' = [taker EXCEPT ![c] = IF byCount /\ w = "a" THEN "count" ELSE w]
              /\ age' = [age EXCEPT ![c] = 0]
              /\ UNCHANGED <<width, up, judged, started>>

\* her beat names c running (friendStarted): a card on her row, ready or working
Start(c) == /\ col[c] \in {"ready", "working"}
            /\ c \notin started
            /\ started' = started \cup {c}
            /\ UNCHANGED <<col, taker, width, up, age, judged>>

\* her finish, which takes her oldest ready card into the lane it frees (friendNext)
Finish(c) == /\ col[c] = "working"
             /\ \/ /\ Ready = {}
                   /\ col' = [col EXCEPT ![c] = "done"]
                   /\ taker' = taker
                   /\ age' = age
                \/ \E n \in Ready :
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
        \/ \E w \in Workers, c \in Cards, b \in BOOLEAN : Take(w, c, b)
        \/ Tick \/ Raise \/ Presence

Spec == Init /\ [][Next]_vars

TypeOK == /\ col \in [Cards -> {"new", "ready", "working", "done"}]
          /\ taker \in [Cards -> {"none", "deal", "finish", "count", "a", "b"}]
          /\ width \in 1..MaxWidth /\ up \in BOOLEAN
          /\ age \in [Cards -> 0..Max] /\ judged \subseteq Cards /\ started \subseteq Cards
\* her lanes are hard, as a member's width
LanesHard == Cardinality(Working) <= width
\* no one but she, her deal or her finish takes a card on her row
OnlyHerTake == \A c \in Cards : taker[c] \notin {"b"}
\* a ready card in front of a free lane for Max ticks is judged (never silent)
NeverStrandedSilently == \A c \in Cards : (col[c] = "ready" /\ age[c] >= Max) => c \in judged
\* with her up, a tick leaves no card ready in front of a free lane
FilledAfterTick == [][Tick /\ up => ~(Ready' # {} /\ Cardinality(Working') < width')]_vars
\* a card her take by count moved to working is one she has started
WorkingIsStarted == \A c \in Working : taker[c] = "count" => c \in started
\* no flap: the tick's return never puts back a card her take by count moved
NoCountFlap == [][\A c \in Cards : (col[c] = "working" /\ taker[c] = "count") => col'[c] # "ready"]_vars
=============================================================================
