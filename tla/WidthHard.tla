------------------------------ MODULE WidthHard ------------------------------
(***************************************************************************)
(* THE WIDTH IS HARD, and it has one definition (the owner, 2026-10-18:     *)
(* "all fleet machines and friends must follow the one true definition of  *)
(* width, in nova-config. No exceptions."). A width is the most cards a     *)
(* fleet row runs at once: every card in its working column counts one, a  *)
(* read card the same as a work card, on a member's row and a friend's      *)
(* alike; and a row's width is nova-config's (cfgWidth), which the sprint   *)
(* reads (Apply) into its copy (width) and never writes of its own.         *)
(*                                                                         *)
(* The code (nova-sprint internal/sprint):                                 *)
(*   Take, ReadTake  takeOne (steps_work.go), room = width - working, and  *)
(*                   the store's HoldWidths (width.go, called from         *)
(*                   store/engine.go Run) on every path                    *)
(*   FriendNext      friendNext (steps_work.go): a friend's finish takes   *)
(*                   her next ready card only under her width              *)
(*   DealTo          the deal's room, DealAhead times the width            *)
(*                   (memberWidths, memberLoads: slotLoad, one a card)     *)
(*   ConfigChange    nova-config machine set <m> --width <n>; apply        *)
(*   Apply           the read of nova-config: WidthRefresh at the store,   *)
(*                   fleet sync, friend sync                               *)
(*   FleetUp         fleet up <m> --width <n>: refused unless n is         *)
(*                   nova-config's (cmd/nova-sprint fleet.go fleetStep)    *)
(*   Hold, Release   fleet down / fleet up                                 *)
(*                                                                         *)
(* WidthHeld is stated over the takes: no action that puts a card into a   *)
(* row's working leaves it above the width the sprint holds for the row:   *)
(* nova-config's, or, while a config change is not yet read (stale), the   *)
(* width it read last. A lowered width admits no new take until working is *)
(* under it; the cards already working finish normally.                    *)
(*                                                                         *)
(* Reversed witnesses, each a cfg that must FAIL WidthHeld:                *)
(*   BadHalfRead      a read costs half a slot (the half-slot room that    *)
(*                    let hetzner1 run 20 cards at width 16)               *)
(*   BadFriendNext    friendNext promotes with no width check              *)
(*   BadSecondSource  fleet up writes a width of its own                   *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Rows, Friends, Cards, Reads, MaxW, DealAhead,
          BadHalfRead, BadFriendNext, BadSecondSource

NoRow == "none"
Cols == {"pool", "ready", "working", "done"}

VARIABLES
    row,       \* row[c]: the fleet row card c is on (NoRow in the pool)
    col,       \* col[c]: its column
    width,     \* width[r]: the sprint's copy of the row's width
    cfgWidth,  \* cfgWidth[r]: nova-config's width, the one source
    stale,     \* rows whose config change the sprint has not read yet
    held,      \* held[r]: the row is held (fleet down)
    lastTake   \* the row the last step put a card into working on, else NoRow

vars == <<row, col, width, cfgWidth, stale, held, lastTake>>

Working(r) == {c \in Cards : row[c] = r /\ col[c] = "working"}
Ready(r) == {c \in Cards : row[c] = r /\ col[c] = "ready"}
Load(r) == Cardinality(Working(r)) + Cardinality(Ready(r))

\* the width the sprint holds for the row: its copy, which only the read of
\* nova-config writes (Apply), so it is nova-config's but while a change is stale
Held(r) == IF r \in stale THEN width[r] ELSE cfgWidth[r]

\* a card fits the row's working: one slot a card, a read the same as a work card
\* (takeOne's room, HoldWidths' check); BadHalfRead counts a read at half a slot
\* and a work card at two, in half slots of twice the width
Fits(r, c) ==
    IF BadHalfRead
    THEN LET ww == Cardinality({x \in Working(r) : x \notin Reads})
             wr == Cardinality({x \in Working(r) : x \in Reads})
             cost == IF c \in Reads THEN 1 ELSE 2
         IN 2 * width[r] - 2 * ww - wr >= cost
    ELSE Cardinality(Working(r)) < width[r]

TypeOK ==
    /\ row \in [Cards -> Rows \cup {NoRow}]
    /\ col \in [Cards -> Cols]
    /\ width \in [Rows -> 1..MaxW]
    /\ cfgWidth \in [Rows -> 1..MaxW]
    /\ stale \subseteq Rows
    /\ held \in [Rows -> BOOLEAN]
    /\ lastTake \in Rows \cup {NoRow}

Init ==
    /\ cfgWidth \in [Rows -> 1..MaxW]
    /\ width = cfgWidth
    /\ stale = {}
    /\ row = [c \in Cards |-> NoRow]
    /\ col = [c \in Cards |-> "pool"]
    /\ held = [r \in Rows |-> FALSE]
    /\ lastTake = NoRow

\* the deal places a pool card ready on a row with room: DealAhead times its width,
\* ready and working, one a card (memberWidths, memberLoads, friendLoad)
DealTo(r, c) ==
    /\ ~held[r]
    /\ col[c] = "pool"
    /\ Load(r) < DealAhead * width[r]
    /\ row' = [row EXCEPT ![c] = r]
    /\ col' = [col EXCEPT ![c] = "ready"]
    /\ lastTake' = NoRow
    /\ UNCHANGED <<width, cfgWidth, stale, held>>

\* a take of a work card, by count or by id or by a lane: one atomic check at the
\* one writer (takeOne's room, then HoldWidths in the store's step)
Take(r, c) ==
    /\ c \notin Reads
    /\ ~held[r]
    /\ row[c] = r /\ col[c] = "ready"
    /\ Fits(r, c)
    /\ col' = [col EXCEPT ![c] = "working"]
    /\ lastTake' = r
    /\ UNCHANGED <<row, width, cfgWidth, stale, held>>

\* a take of a read card: the same check, a read costing one slot
ReadTake(r, c) ==
    /\ c \in Reads
    /\ ~held[r]
    /\ row[c] = r /\ col[c] = "ready"
    /\ Fits(r, c)
    /\ col' = [col EXCEPT ![c] = "working"]
    /\ lastTake' = r
    /\ UNCHANGED <<row, width, cfgWidth, stale, held>>

\* a finish: the card leaves working; on a friend's row her next ready card is taken
\* in the same step (friendNext) only while her working, as the finish leaves it, is
\* under her width; BadFriendNext promotes with no check
Finish(r, c) ==
    /\ row[c] = r /\ col[c] = "working"
    /\ LET after == [col EXCEPT ![c] = "done"]
           left == {x \in Cards : row[x] = r /\ after[x] = "working"}
           next == {x \in Ready(r) : TRUE}
       IN IF r \in Friends /\ next # {} /\ (BadFriendNext \/ Cardinality(left) < width[r])
          THEN \E n \in next :
                 /\ col' = [after EXCEPT ![n] = "working"]
                 /\ lastTake' = r
          ELSE /\ col' = after
               /\ lastTake' = NoRow
    /\ UNCHANGED <<row, width, cfgWidth, stale, held>>

\* nova-config lowers or raises a row's width (nova-config machine set, apply; a
\* friend row's width): the sprint has not read it yet
ConfigChange(r, n) ==
    /\ n # cfgWidth[r]
    /\ cfgWidth' = [cfgWidth EXCEPT ![r] = n]
    /\ stale' = stale \cup {r}
    /\ lastTake' = NoRow
    /\ UNCHANGED <<row, col, width, held>>

\* the sprint reads nova-config: every copy is nova-config's again (WidthRefresh at
\* the store, fleet sync, friend sync)
Apply ==
    /\ stale # {}
    /\ width' = cfgWidth
    /\ stale' = {}
    /\ lastTake' = NoRow
    /\ UNCHANGED <<row, col, cfgWidth, held>>

\* fleet up <m> --width <n>: refused unless n is nova-config's; BadSecondSource writes
\* a width of its own into the copy
FleetUp(r, n) ==
    /\ r \notin Friends
    /\ IF BadSecondSource THEN TRUE ELSE n = cfgWidth[r]
    /\ width' = [width EXCEPT ![r] = n]
    /\ stale' = IF n = cfgWidth[r] THEN stale \ {r} ELSE stale
    /\ held' = [held EXCEPT ![r] = FALSE]
    /\ lastTake' = NoRow
    /\ UNCHANGED <<row, col, cfgWidth>>

Hold(r) ==
    /\ ~held[r]
    /\ held' = [held EXCEPT ![r] = TRUE]
    /\ lastTake' = NoRow
    /\ UNCHANGED <<row, col, width, cfgWidth, stale>>

Next ==
    \/ \E r \in Rows, c \in Cards : DealTo(r, c) \/ Take(r, c) \/ ReadTake(r, c) \/ Finish(r, c)
    \/ \E r \in Rows, n \in 1..MaxW : ConfigChange(r, n) \/ FleetUp(r, n)
    \/ Apply
    \/ \E r \in Rows : Hold(r)

Spec == Init /\ [][Next]_vars

\* no step that puts a card into a row's working leaves it above the width the sprint
\* holds for the row, every card counting one (reads whole)
WidthHeld ==
    lastTake # NoRow => Cardinality(Working(lastTake)) <= Held(lastTake)

\* the sprint's copy is nova-config's once it has read it: no second source
OneSource == \A r \in Rows : r \notin stale => width[r] = cfgWidth[r]

=============================================================================
