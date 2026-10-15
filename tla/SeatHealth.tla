---------------------------- MODULE SeatHealth ----------------------------
\* FriendHookStatus reads nova-hook's status and server failure word; the only
\* sprint-owned friend state is the coordinator's hold. Down overrides a hold.
\* Handover advances the seat generation independently. Work facts have no effect
\* on status. Hook's challenge, lease and expiry rules live in nova-tools/Hook.
EXTENDS Naturals
CONSTANTS Holders, MaxObs, Broken
VARIABLES holder, gen, moves, hookProven, serverDown, held, work
vars == <<holder, gen, moves, hookProven, serverDown, held, work>>
Init == /\ holder \in Holders /\ gen = 1 /\ moves = 0
        /\ hookProven = FALSE /\ serverDown = FALSE /\ held = FALSE /\ work = FALSE
Status == IF Broken = "holdwins" /\ held THEN "held"
          ELSE IF ~hookProven \/ (serverDown /\ Broken # "serverignored") THEN "down"
          ELSE IF held THEN "held"
          ELSE IF Broken = "workstalls" /\ ~work THEN "stalled"
          ELSE "up"
Handover(h) == /\ h # holder /\ moves < MaxObs
               /\ holder' = h /\ moves' = moves + 1
               /\ gen' = IF Broken = "samegen" THEN gen ELSE gen + 1
               /\ UNCHANGED <<hookProven, serverDown, held, work>>
Hook == hookProven' = ~hookProven /\ UNCHANGED <<holder, gen, moves, serverDown, held, work>>
Server == serverDown' = ~serverDown /\ UNCHANGED <<holder, gen, moves, hookProven, held, work>>
Hold == held' = ~held /\ UNCHANGED <<holder, gen, moves, hookProven, serverDown, work>>
Work == work' = ~work /\ UNCHANGED <<holder, gen, moves, hookProven, serverDown, held>>
Next == Hook \/ Server \/ Hold \/ Work \/ (\E h \in Holders : Handover(h))
Spec == Init /\ [][Next]_vars
TypeOK == /\ holder \in Holders /\ gen \in 1..(MaxObs+1) /\ moves \in 0..MaxObs
          /\ hookProven \in BOOLEAN /\ serverDown \in BOOLEAN /\ held \in BOOLEAN /\ work \in BOOLEAN
ThreeWords == Status \in {"up", "down", "held"}
HookDownWins == (~hookProven \/ serverDown) => Status = "down"
HeldIsTheHold == (Status = "held") <=> (hookProven /\ ~serverDown /\ held)
UpOnlyOnHook == Status = "up" => hookProven /\ ~serverDown /\ ~held
GenerationStepsWithTheSeat == [][gen' = gen + moves' - moves]_vars
=============================================================================
