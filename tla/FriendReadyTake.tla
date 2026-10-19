----------------------- MODULE FriendReadyTake ------------------------
EXTENDS Naturals, FiniteSets
\* Take/finish behavior preserved from nova-tools dev 2314d437f. Historical
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
\*
\* Read cards occupy her row as work cards do (read_cards.go rowLoad): the set
\* ReadCards (the one card r1), each held working or ready, counts against her
\* room and her lanes. Room counts reads (Live < 2 * width), the queue bound of
\* v17-friend-deal-queues-past-width: width bounds what she WORKS at once, never
\* what is QUEUED in her ready queue, so a card is dealt into her queue even at
\* width (the owner, 2026-10-18). The specs choose the variant, not a config
\* constant: Spec is the preserved model with no reads, SpecReads deals with a
\* read held (the owner's rule), and SpecBrokenReads initializes the mode broken
\* so Deal's room collapses to width - (working+ready) when any read is held, as
\* today's code does; a card then stays undealt while Room holds
\* (MCFriendReadyTakeBrokenReadsCollapseRoom.cfg violates NoUndealtWhileRoom).
\* NoUndealtWhileRoom is stated over the states after Tick, as FilledAfterTick
\* is stated.
CONSTANTS Cards, MaxWidth, Max, BadNoFill, BadPastWidth, BadStranger, BadNoJudge
Workers == {"a", "b"}
\* Snapshot eligibility is independent of up: a proven coordinator/comms hook
\* can be up without a worker. Each friend has worker evidence, and the current
\* coordinator is excluded even if it also carries worker evidence. These facts
\* are fixed during this bounded deal; later hook loss is not a bad past deal.
\* The row being dealt remains a; b is the stranger in the preserved take model.
BadNoWorker == FALSE
VARIABLES col, readCol, taker, width, up, age, judged, workerLive, coordinator, broken
vars == <<col, readCol, taker, width, up, age, judged, workerLive, coordinator, broken>>

Working == {c \in Cards : col[c] = "working"}
Ready == {c \in Cards : col[c] = "ready"}
New == {c \in Cards : col[c] = "new"}
\* the read card the owner's rule and the broken variant are checked with
ReadCards == {"r1"}
ReadWorking == {r \in ReadCards : readCol[r] = "working"}
ReadReady == {r \in ReadCards : readCol[r] = "ready"}
Live == Cardinality(Working) + Cardinality(Ready) + Cardinality(ReadWorking) + Cardinality(ReadReady)
Room == Live < 2 * width
DealsRoom == IF broken /\ ReadCards # {} THEN Live < width ELSE Room
Eligible == workerLive["a"] /\ coordinator # "a"
LaneFree == Cardinality(Working) < width

Init == /\ col = [c \in Cards |-> "new"]
        /\ readCol = [r \in ReadCards |-> "new"]
        /\ taker = [c \in Cards |-> "none"]
        /\ width = 1
        /\ up = TRUE
        /\ age = [c \in Cards |-> 0]
        /\ judged = {}
        /\ workerLive \in [Workers -> BOOLEAN]
        /\ coordinator \in Workers \cup {"none"}
        /\ broken = FALSE

\* the owner's rule with a read held: the read occupies her row, and Deal's room
\* still counts it (Live < 2 * width)
InitReads == /\ col = [c \in Cards |-> "new"]
            /\ readCol \in [ReadCards -> {"working", "ready"}]
            /\ taker = [c \in Cards |-> "none"]
            /\ width = 1
            /\ up = TRUE
            /\ age = [c \in Cards |-> 0]
            /\ judged = {}
            /\ workerLive \in [Workers -> BOOLEAN]
            /\ coordinator \in Workers \cup {"none"}
            /\ broken = FALSE

\* today's broken code: the same read, but Deal's room is width - (working+ready)
InitBrokenReads == /\ readCol \in [ReadCards -> {"working", "ready"}]
                  /\ col = [c \in Cards |-> "new"]
                  /\ taker = [c \in Cards |-> "none"]
                  /\ width = 1
                  /\ up = TRUE
                  /\ age = [c \in Cards |-> 0]
                  /\ judged = {}
                  /\ workerLive \in [Workers -> BOOLEAN]
                  /\ coordinator \in Workers \cup {"none"}
                  /\ broken = TRUE

\* the friend's deal of one new card (friendDealPass): working on a free lane, else ready
Deal(c) == /\ col[c] = "new" /\ up /\ DealsRoom
           /\ (BadNoWorker \/ Eligible)
           /\ col' = [col EXCEPT ![c] = IF LaneFree THEN "working" ELSE "ready"]
           /\ taker' = [taker EXCEPT ![c] = IF LaneFree THEN "deal" ELSE "none"]
           /\ UNCHANGED <<workerLive, coordinator, width, up, age, judged, readCol, broken>>

\* the tick: its deal first places every new card it may, into her ready queue,
\* up to the room bound (the owner's rule: width bounds work, never queued; a
\* card is dealt into her queue even at width), then it takes her ready cards
\* into her free lanes, oldest first; a ready card still in front of a free lane
\* ages, and is judged at Max (FriendReadyMax). The deal runs while she is up
\* and live (Eligible), and is bounded by DealsRoom (correct: Live < 2*width;
\* broken, the read held: Live < width).
Limit == IF broken /\ ReadCards # {} THEN width ELSE 2 * width
CanDeal == IF Live >= Limit THEN 0 ELSE Limit - Live
DealN == IF Cardinality(New) < CanDeal THEN Cardinality(New) ELSE CanDeal
DealSet == CHOOSE s \in SUBSET New : Cardinality(s) = DealN
Fill == IF BadNoFill \/ ~up THEN {}
        ELSE CHOOSE s \in SUBSET Ready : Cardinality(s) = IF Cardinality(Ready) < width - Cardinality(Working)
                                                   THEN Cardinality(Ready) ELSE width - Cardinality(Working)
Tick == LET deal == IF BadNoWorker \/ ~up \/ ~Eligible THEN {} ELSE DealSet
            colD == [c \in Cards |-> IF c \in deal THEN "ready" ELSE col[c]]
            readyD == {c \in Cards : colD[c] = "ready"}
            workingD == {c \in Cards : colD[c] = "working"}
            fill == IF BadNoFill \/ ~up THEN {}
                    ELSE CHOOSE s \in SUBSET readyD : Cardinality(s) = IF Cardinality(readyD) < width - Cardinality(workingD)
                                                        THEN Cardinality(readyD) ELSE width - Cardinality(workingD)
            col1 == [c \in Cards |-> IF c \in fill THEN "working" ELSE colD[c]]
            free == Cardinality({c \in Cards : col1[c] = "working"}) < width
        IN /\ col' = col1
           /\ taker' = [c \in Cards |-> IF c \in deal \/ c \in fill THEN "deal" ELSE taker[c]]
           /\ age' = [c \in Cards |-> IF col1[c] = "ready" /\ free THEN IF age[c] < Max THEN age[c] + 1 ELSE Max ELSE 0]
           /\ judged' = IF BadNoJudge THEN judged
                        ELSE judged \cup {c \in Cards : col1[c] = "ready" /\ free /\ age'[c] >= Max}
           /\ UNCHANGED <<workerLive, coordinator, width, up, readCol, broken>>

\* take --as friend.<w> c: her own row only, while she is up, within her lanes
Take(w, c) == /\ col[c] = "ready"
              /\ (w = "a" \/ BadStranger)
              /\ up
              /\ (LaneFree \/ BadPastWidth)
              /\ col' = [col EXCEPT ![c] = "working"]
              /\ taker' = [taker EXCEPT ![c] = w]
              /\ age' = [age EXCEPT ![c] = 0]
              /\ UNCHANGED <<workerLive, coordinator, width, up, judged, readCol, broken>>

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
             /\ UNCHANGED <<workerLive, coordinator, width, up, judged, readCol, broken>>

Raise == /\ width < MaxWidth
         /\ width' = width + 1
         /\ UNCHANGED <<workerLive, coordinator, col, readCol, taker, up, age, judged, broken>>

Presence == /\ up' = ~up
            /\ UNCHANGED <<workerLive, coordinator, col, readCol, taker, width, age, judged, broken>>

Next == \/ \E c \in Cards : Deal(c) \/ Finish(c)
        \/ \E w \in Workers, c \in Cards : Take(w, c)
        \/ Tick \/ Raise \/ Presence

Spec == Init /\ [][Next]_vars
\* the owner's rule with a read held (MCFriendReadyTake.cfg)
SpecReads == InitReads /\ [][Next]_vars
\* today's broken room with a read held (MCFriendReadyTakeBrokenReadsCollapseRoom.cfg)
SpecBrokenReads == InitBrokenReads /\ [][Next]_vars

TypeOK == /\ workerLive \in [Workers -> BOOLEAN]
          /\ coordinator \in Workers \cup {"none"}
          /\ col \in [Cards -> {"new", "ready", "working", "done"}]
          /\ readCol \in [ReadCards -> {"new", "working", "ready"}]
          /\ taker \in [Cards -> {"none", "deal", "finish", "a", "b"}]
          /\ width \in 1..MaxWidth /\ up \in BOOLEAN /\ broken \in BOOLEAN
          /\ age \in [Cards -> 0..Max] /\ judged \subseteq Cards
\* An observation of the actual placement, independent of the Deal guard.
DealtOnlyToLiveWorker == \A c \in Cards : col[c] # "new" =>
                           (workerLive["a"] /\ coordinator # "a")
\* her lanes are hard, as a member's width
LanesHard == Cardinality(Working) <= width
\* no one but she, her deal or her finish takes a card on her row
OnlyHerTake == \A c \in Cards : taker[c] \notin {"b"}
\* a ready card in front of a free lane for Max ticks is judged (never silent)
NeverStrandedSilently == \A c \in Cards : (col[c] = "ready" /\ age[c] >= Max) => c \in judged
\* with her up, a tick leaves no card ready in front of a free lane
FilledAfterTick == [][Tick /\ up => ~(Ready' # {} /\ Cardinality(Working') < width')]_vars
\* with her up and live, a tick leaves no card undealt while Room holds (the owner's
\* rule of v17-friend-deal-queues-past-width: width bounds work, never the queue, so
\* every card is dealt into her ready queue while room remains). Stated over the states
\* after Tick, as FilledAfterTick is stated. SpecBrokenReads (MCFriendReadyTake-
\* BrokenReadsCollapseRoom.cfg) collapses Deal's room and violates it.
NoUndealtWhileRoom == [][Tick /\ up /\ Eligible => ~(\E c \in Cards : col'[c] = "new" /\ Live' < 2 * width')]_vars
=============================================================================
