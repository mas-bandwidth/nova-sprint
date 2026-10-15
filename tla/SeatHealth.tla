---------------------------- MODULE SeatHealth ----------------------------
\* FriendHookStatus reads nova-hook's status and server failure word; the only
\* sprint-owned friend state is the coordinator's hold. Down overrides a hold.
\* Handover advances the seat generation independently. Work facts have no effect
\* on status. Hook's challenge, lease and expiry rules live in nova-tools/Hook.
\*
\* One writer (v1.4.0; Glenn, 2026-10-15: "the dashboard is required to get up/down from
\* the one source of friends being up/down, which is the server in redis"): the server's
\* tick is the only action that makes the status word. Publish stores it (stored, the
\* friend-status record: PublishFriendStatus), and every reader, the dashboard and where,
\* shows what Show reads from the record: none written reads down, never a guess. A reader
\* that derives its own word from the hook records (Broken = "derive") shows a word the
\* server did not write, which OneWriter refuses.
EXTENDS Naturals
CONSTANTS Holders, MaxObs, Broken
VARIABLES holder, gen, moves, hookProven, serverDown, held, work, stored, shown
vars == <<holder, gen, moves, hookProven, serverDown, held, work, stored, shown>>
showing == <<stored, shown>>
Init == /\ holder \in Holders /\ gen = 1 /\ moves = 0
        /\ hookProven = FALSE /\ serverDown = FALSE /\ held = FALSE /\ work = FALSE
        /\ stored = "none" /\ shown = "down"
Status == IF Broken = "holdwins" /\ held THEN "held"
          ELSE IF ~hookProven \/ (serverDown /\ Broken # "serverignored") THEN "down"
          ELSE IF held THEN "held"
          ELSE IF Broken = "workstalls" /\ ~work THEN "stalled"
          ELSE "up"
Handover(h) == /\ h # holder /\ moves < MaxObs
               /\ holder' = h /\ moves' = moves + 1
               /\ gen' = IF Broken = "samegen" THEN gen ELSE gen + 1
               /\ UNCHANGED <<hookProven, serverDown, held, work>> /\ UNCHANGED showing
Hook == hookProven' = ~hookProven /\ UNCHANGED <<holder, gen, moves, serverDown, held, work>> /\ UNCHANGED showing
Server == serverDown' = ~serverDown /\ UNCHANGED <<holder, gen, moves, hookProven, held, work>> /\ UNCHANGED showing
Hold == held' = ~held /\ UNCHANGED <<holder, gen, moves, hookProven, serverDown, work>> /\ UNCHANGED showing
Work == work' = ~work /\ UNCHANGED <<holder, gen, moves, hookProven, serverDown, held>> /\ UNCHANGED showing
\* The server's tick writes the word the rule above makes, and nothing else does.
Publish == /\ stored' = Status /\ UNCHANGED <<holder, gen, moves, hookProven, serverDown, held, work, shown>>
\* A reader shows the server's record; with none written it shows down.
Show == /\ shown' = IF Broken = "derive" THEN Status ELSE IF stored = "none" THEN "down" ELSE stored
        /\ UNCHANGED <<holder, gen, moves, hookProven, serverDown, held, work, stored>>
Next == Hook \/ Server \/ Hold \/ Work \/ Publish \/ Show \/ (\E h \in Holders : Handover(h))
Spec == Init /\ [][Next]_vars
TypeOK == /\ holder \in Holders /\ gen \in 1..(MaxObs+1) /\ moves \in 0..MaxObs
          /\ hookProven \in BOOLEAN /\ serverDown \in BOOLEAN /\ held \in BOOLEAN /\ work \in BOOLEAN
          /\ stored \in {"none", "up", "down", "held"} /\ shown \in {"up", "down", "held"}
ThreeWords == Status \in {"up", "down", "held"}
HookDownWins == (~hookProven \/ serverDown) => Status = "down"
HeldIsTheHold == (Status = "held") <=> (hookProven /\ ~serverDown /\ held)
UpOnlyOnHook == Status = "up" => hookProven /\ ~serverDown /\ ~held
GenerationStepsWithTheSeat == [][gen' = gen + moves' - moves]_vars
\* The only step that makes the stored word is the server's, and it makes the word the
\* status rule makes (down beats held beats up): nothing else writes it.
StoredIsTheServersWord == [][stored' # stored => stored' = Status]_vars
\* What a reader shows, when it changes, is the stored word (down while none is written),
\* never a word of its own: the dashboard and where show what the server wrote.
OneWriter == [][shown' # shown => shown' = IF stored = "none" THEN "down" ELSE stored]_vars
=============================================================================
