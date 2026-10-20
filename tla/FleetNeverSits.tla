------------------------- MODULE FleetNeverSits -------------------------
\* The fleet never sits (the owner, 2026-10-19, watching the live fleet:
\* "The fleet getting stuck like this, you'll need to come up with a way to stop
\* this from happening again in v1.9"; docs/SPEC-SPRINT.md section 5, the fleet).
\* Four rules, written before any code, over one small instance: a work card
\* (ready, dealt m, running m on route r, done), its read (none, held by a
\* reader, done), routes with runs and consecutive non-completions, and members,
\* readers and a discrete clock that the sprint advances by one.
\*
\* NoIdleWithWork. When ready work exists and some up member has free width,
\* every such card carries a named cause in the server's `where`: route (no
\* serving route of its tier), gate (no member's tags meet the card's), width
\* (the members that could run it are at width), or none and it is dealt.
\* NameCause writes the cause; Tick will not pass the idle bound D while a
\* ready card in front of a free member is unnamed.
\*
\* DealtStartsOrRedeals. A card dealt to a member and not taken within the
\* start bound T goes back and is dealt to another eligible member; the member
\* it left is suspect (dealt nothing until its next take or beat proves it
\* takes) and never gets the card again in this window (left). This is
\* StartDeadline. The card is never re-dealt to the member it left, in this
\* window: that member is suspect and left when the card moves to another. When
\* no other eligible member can take it, the card goes back ready at once and
\* its old member is suspect; NameCause then names the blocking cause ("width",
\* "gate" or "route") in `where`, so the bound hides no card.
\*
\* RouteProgress. A run in flight with no progress (no token, no heartbeat)
\* for T is ended as a provider fault of its route: the card goes back ready
\* at once and is retried on the tier's next serving route, no attempt counted;
\* N consecutive such ends rest the route automatically. This is
\* ProgressDeadline. Progress is the outside event that a run advances (a
\* token or a heartbeat); Stall is ultramarine: the route stops answering and
\* the run never ends by itself.
\*
\* ReaderLiveness. A reader holding dealt reads and taking none for T is set
\* away and its reads redealt to up, live readers. This is ReaderDeadline.
\* ReaderDeaf is hulk: a reader that still reads as up but takes nothing.
\*
\* MemberDeaf is a member that never takes what it is dealt (js-retain's
\* hetzner1 at 23:34). The outside actions Progress, Stall, MemberDeaf and
\* ReaderDeaf are not fair; the sprint's actions are (Spec).
\*
\* Invariants: NoRunWithoutProgressPastT, NoDealtPastT, NoAwayReaderHolds,
\* IdleHasCause. Liveness: WorkStarts, RunProgress, ReadTaken. Broken names
\* the reversed witness (each its own cfg): "noprogressdeadline",
\* "dealtnodeadline", "deadreader", "silentidle", "redealsame". The model is
\* run as the cases MCFleetNeverSits.cfg and MCFleetNeverSitsBroken*.cfg
\* (tla/CASES.tsv, group fleetneversits).
\*
\* The instances are small: the control is one card, two members of width one,
\* two routes of one tier and two readers at T = 1, N = 1, D = 1 and the clock's
\* bound 2. The noprogressdeadline witness adds a second card on one member and
\* one route: that card is still read-less when a reader is set away, so a
\* later Deal must not hand its read to that away reader (NoAwayReaderHolds). A
\* read is held by one reader here (the code asks a read on two and the hazard
\* is one holder that never takes); a run's route is chosen at its take from
\* the tier's serving routes, which stands for the tier's ordered next route,
\* and a completion resets its route's count. Route rests do not end in this
\* bounded model, so a card with every route rested is named "route" and its
\* work liveness is discharged by the route's absence.
EXTENDS Naturals, FiniteSets

CONSTANTS Cards, Members, Readers, Routes,
          T,        \* the start bound, the progress bound and the reader bound
          N,        \* consecutive non-completions that rest a route
          D,        \* the idle bound: a ready card is named or dealt by D
          MaxTime, MaxWidth, cardTags, memberTags, Broken

VARIABLES now, event, up, width, running, dealt, suspect, deaf, cause,
          wcState, wcMember, wcRoute, lastProgress, stalled,
          nonCompletions, rested,
          readState, readReader, readDealtAt, lastTake, rUp, rAway, rDeaf,
          left

vars == <<now, event, up, width, running, dealt, suspect, deaf, cause,
          wcState, wcMember, wcRoute, lastProgress, stalled,
          nonCompletions, rested,
          readState, readReader, readDealtAt, lastTake, rUp, rAway, rDeaf,
          left>>

Events == {"init", "deal", "take", "finish", "startdeadline",
           "progressdeadline", "readerdeadline", "namecause", "tick",
           "progress", "stall", "memberdeaf", "readerdeaf"}

GateOpen(c, m) == cardTags[c] \subseteq memberTags[m]
AnyRoute == \E r \in Routes : ~rested[r]
FreeWidth(m) == Cardinality(running[m]) < width[m]
Room(m) == Cardinality(running[m])
           + Cardinality({x \in Cards : dealt[m][x] > 0}) < 2 * width[m]

\* the deal's eligible member: up, not suspect, not deaf, tags met, a free lane
EligibleMember(c, m) ==
    /\ up[m] /\ m \notin suspect /\ m \notin deaf
    /\ FreeWidth(m) /\ GateOpen(c, m)

\* some up member has free width: NoIdleWithWork's antecedent (the gate is the
\* card's own cause, not this condition, as the owner stated it)
FreeExists == \E m \in Members : up[m] /\ m \notin suspect /\ m \notin deaf /\ FreeWidth(m)

\* a card some up, non-suspect, eligible member and some serving route can run
CanRun(c) == (\E m \in Members : EligibleMember(c, m)) /\ AnyRoute

\* a member a card could run on if it had a free lane: the gate and liveness
Potential(c, m) == up[m] /\ m \notin suspect /\ m \notin deaf /\ GateOpen(c, m)

\* the named cause in `where`: route, gate, width, or none and dealt
CauseOf(c) ==
    IF ~AnyRoute THEN "route"
    ELSE IF \E m \in Members : Potential(c, m) /\ FreeWidth(m) THEN "none"
    ELSE IF \E m \in Members : Potential(c, m) THEN "width"
    ELSE "gate"

Named(c) == cause[c] \in {"route", "gate", "width"}

ReadHeld(c, r) == readState[c] = "held" /\ readReader[c] = r
LiveReader(r) == rUp[r] /\ ~rAway[r] /\ ~rDeaf[r]
LiveReaderExists == \E r \in Readers : LiveReader(r)
DeafCount == Cardinality({r \in Readers : rDeaf[r]})
NoOtherMember(c, old) ==
    ~\E m \in Members :
        /\ m # old
        /\ up[m] /\ GateOpen(c, m) /\ FreeWidth(m)
        /\ m \notin suspect /\ m \notin deaf /\ m \notin left[c]

TypeOK ==
    /\ now \in 1..MaxTime
    /\ event \in Events
    /\ up \in [Members -> BOOLEAN]
    /\ width \in [Members -> 1..MaxWidth]
    /\ running \in [Members -> SUBSET Cards]
    /\ dealt \in [Members -> [Cards -> 0..MaxTime]]
    /\ suspect \subseteq Members
    /\ deaf \subseteq Members
    /\ cause \in [Cards -> {"", "route", "gate", "width", "none"}]
    /\ wcState \in [Cards -> {"ready", "dealt", "running", "done"}]
    /\ wcMember \in [Cards -> Members]
    /\ wcRoute \in [Cards -> Routes \cup {"none"}]
    /\ lastProgress \in [Cards -> 0..MaxTime]
    /\ stalled \in [Cards -> BOOLEAN]
    /\ nonCompletions \in [Routes -> 0..N]
    /\ rested \in [Routes -> BOOLEAN]
    /\ readState \in [Cards -> {"none", "held", "done"}]
    /\ readReader \in [Cards -> Readers]
    /\ readDealtAt \in [Cards -> 0..MaxTime]
    /\ lastTake \in [Readers -> 0..MaxTime]
    /\ rUp \in [Readers -> BOOLEAN]
    /\ rAway \in [Readers -> BOOLEAN]
    /\ rDeaf \in [Readers -> BOOLEAN]
    /\ left \in [Cards -> SUBSET Members]

Init ==
    /\ now = 1
    /\ event = "init"
    /\ up = [m \in Members |-> TRUE]
    /\ width = [m \in Members |-> MaxWidth]
    /\ running = [m \in Members |-> {}]
    /\ dealt = [m \in Members |-> [c \in Cards |-> 0]]
    /\ suspect = {}
    /\ deaf = {}
    /\ cause = [c \in Cards |-> ""]
    /\ wcState = [c \in Cards |-> "ready"]
    /\ wcMember = [c \in Cards |-> CHOOSE m \in Members : TRUE]
    /\ wcRoute = [c \in Cards |-> "none"]
    /\ lastProgress = [c \in Cards |-> 0]
    /\ stalled = [c \in Cards |-> FALSE]
    /\ nonCompletions = [r \in Routes |-> 0]
    /\ rested = [r \in Routes |-> FALSE]
    /\ readState = [c \in Cards |-> "none"]
    /\ readReader = [c \in Cards |-> CHOOSE r \in Readers : TRUE]
    /\ readDealtAt = [c \in Cards |-> 0]
    /\ lastTake = [r \in Readers |-> 0]
    /\ rUp = [r \in Readers |-> TRUE]
    /\ rAway = [r \in Readers |-> FALSE]
    /\ rDeaf = [r \in Readers |-> FALSE]
    /\ left = [c \in Cards |-> {}]

\* the deal: a ready card to an eligible member, and its read to a reader.
\* DealtStartsOrRedeals reads the deal time; a card nobody may run is named, not
\* dealt.
Deal(c, m) ==
    \E r \in Readers :
        /\ wcState[c] = "ready"
        /\ up[m] /\ m \notin suspect /\ GateOpen(c, m)
        /\ (now <= MaxTime - T \/ m \notin deaf)
        /\ Room(m) /\ AnyRoute
        /\ (readState[c] # "none"
            \/ (rUp[r] /\ ~rAway[r] /\ (now <= MaxTime - T \/ ~rDeaf[r])))
        /\ wcState' = [wcState EXCEPT ![c] = "dealt"]
        /\ wcMember' = [wcMember EXCEPT ![c] = m]
        /\ dealt' = [dealt EXCEPT ![m][c] = now]
        /\ cause' = [cause EXCEPT ![c] = ""]
        /\ IF readState[c] = "none"
             THEN /\ readState' = [readState EXCEPT ![c] = "held"]
                  /\ readReader' = [readReader EXCEPT ![c] = r]
                  /\ readDealtAt' = [readDealtAt EXCEPT ![c] = now]
             ELSE /\ UNCHANGED <<readState, readReader, readDealtAt>>
        /\ event' = "deal"
        /\ UNCHANGED <<now, up, width, running, suspect, deaf, wcRoute,
                       lastProgress, stalled, nonCompletions, rested,
                       lastTake, rUp, rAway, rDeaf, left>>

\* the member takes the dealt card: its lane frees, its run starts on a serving
\* route now. A take clears the member's suspicion (its next take proves it).
TakeWork(c, m) ==
    \E r \in Routes :
        /\ wcState[c] = "dealt" /\ wcMember[c] = m
        /\ up[m] /\ m \notin deaf /\ FreeWidth(m)
        /\ ~rested[r]
        /\ wcState' = [wcState EXCEPT ![c] = "running"]
        /\ running' = [running EXCEPT ![m] = running[m] \cup {c}]
        /\ dealt' = [dealt EXCEPT ![m][c] = 0]
        /\ wcRoute' = [wcRoute EXCEPT ![c] = r]
        /\ lastProgress' = [lastProgress EXCEPT ![c] = now]
        /\ stalled' = [stalled EXCEPT ![c] = FALSE]
        /\ suspect' = suspect \ {m}
        /\ event' = "take"
        /\ UNCHANGED <<now, up, width, deaf, cause, wcMember, nonCompletions,
                       rested, readState, readReader, readDealtAt, lastTake,
                       rUp, rAway, rDeaf, left>>

\* the reader takes its held read
TakeRead(c) ==
    LET r == readReader[c] IN
        /\ readState[c] = "held"
        /\ rUp[r] /\ ~rAway[r] /\ ~rDeaf[r]
        /\ readState' = [readState EXCEPT ![c] = "done"]
        /\ lastTake' = [lastTake EXCEPT ![r] = now]
        /\ event' = "take"
        /\ UNCHANGED <<now, up, width, running, dealt, suspect, deaf, cause,
                       wcState, wcMember, wcRoute, lastProgress, stalled,
                       nonCompletions, rested, readReader, readDealtAt,
                       rUp, rAway, rDeaf, left>>

\* the run finishes: the card is done and its route's consecutive count resets
Finish(c) ==
    LET m == wcMember[c] r == wcRoute[c] IN
        /\ wcState[c] = "running" /\ ~stalled[c]
        /\ wcState' = [wcState EXCEPT ![c] = "done"]
        /\ running' = [running EXCEPT ![m] = running[m] \ {c}]
        /\ nonCompletions' = [nonCompletions EXCEPT ![r] = 0]
        /\ event' = "finish"
        /\ UNCHANGED <<now, up, width, dealt, suspect, deaf, cause, wcMember,
                       wcRoute, lastProgress, stalled, rested, readState,
                       readReader, readDealtAt, lastTake, rUp, rAway, rDeaf,
                       left>>

\* the outside run advances: a token or a heartbeat (Progress)
Progress(c) ==
    /\ wcState[c] = "running" /\ ~stalled[c]
    /\ lastProgress' = [lastProgress EXCEPT ![c] = now]
    /\ event' = "progress"
    /\ UNCHANGED <<now, up, width, running, dealt, suspect, deaf, cause,
                   wcState, wcMember, wcRoute, stalled, nonCompletions, rested,
                   readState, readReader, readDealtAt, lastTake, rUp, rAway,
                   rDeaf, left>>

\* ultramarine: the route stops answering and the run never ends by itself
Stall(c) ==
    /\ wcState[c] = "running" /\ ~stalled[c]
    /\ stalled' = [stalled EXCEPT ![c] = TRUE]
    /\ event' = "stall"
    /\ UNCHANGED <<now, up, width, running, dealt, suspect, deaf, cause,
                   wcState, wcMember, wcRoute, lastProgress, nonCompletions,
                   rested, readState, readReader, readDealtAt, lastTake, rUp,
                   rAway, rDeaf, left>>

\* RouteProgress: a run with no progress for T ends as a provider fault of its
\* route, the card back ready at once for the next serving route (no attempt
\* counted); N consecutive such ends rest the route.
ProgressDeadline(c) ==
    LET m == wcMember[c] r == wcRoute[c] k == nonCompletions[r] + 1 IN
        /\ "noprogressdeadline" \notin Broken
        /\ wcState[c] = "running"
        /\ now - lastProgress[c] >= T
        /\ wcState' = [wcState EXCEPT ![c] = "ready"]
        /\ running' = [running EXCEPT ![m] = running[m] \ {c}]
        /\ lastProgress' = [lastProgress EXCEPT ![c] = 0]
        /\ stalled' = [stalled EXCEPT ![c] = FALSE]
        /\ wcRoute' = [wcRoute EXCEPT ![c] = "none"]
        /\ nonCompletions' = [nonCompletions EXCEPT ![r] = IF k > N THEN N ELSE k]
        /\ rested' = [rested EXCEPT ![r] = rested[r] \/ (k >= N)]
        /\ cause' = [cause EXCEPT ![c] = ""]
        /\ event' = "progressdeadline"
        /\ UNCHANGED <<now, up, width, dealt, suspect, deaf,
                       wcMember, readState, readReader, readDealtAt, lastTake,
                       rUp, rAway, rDeaf, left>>

\* DealtStartsOrRedeals: a card dealt and not taken within T goes to another
\* eligible member; the old one is suspect and left, and never gets the card
\* again in this window. When no other eligible member can take it, the card
\* goes back ready and the old one is suspect and left; NameCause names why it
\* waits. "redealsame" is the reversed witness that may pick the same member
\* even when another can take it.
StartDeadline(c) ==
    LET old == wcMember[c] IN
        \/ \E m2 \in Members :
            /\ "dealtnodeadline" \notin Broken
            /\ wcState[c] = "dealt"
            /\ now - dealt[old][c] >= T
            /\ up[m2] /\ GateOpen(c, m2) /\ FreeWidth(m2)
            /\ ( (m2 # old
                  /\ m2 \notin suspect /\ m2 \notin deaf /\ m2 \notin left[c])
                 \/ (m2 = old /\ "redealsame" \in Broken) )
            /\ wcMember' = [wcMember EXCEPT ![c] = m2]
            /\ dealt' = [dealt EXCEPT ![old][c] = 0, ![m2][c] = now]
            /\ suspect' = suspect \cup {old}
            /\ left' = [left EXCEPT ![c] = left[c] \cup {old}]
            /\ cause' = [cause EXCEPT ![c] = ""]
            /\ event' = "startdeadline"
            /\ UNCHANGED <<now, up, width, running, deaf, wcState, wcRoute,
                           lastProgress, stalled, nonCompletions, rested,
                           readState, readReader, readDealtAt, lastTake, rUp,
                           rAway, rDeaf>>
        \/ /\ "dealtnodeadline" \notin Broken
           /\ wcState[c] = "dealt"
           /\ now - dealt[old][c] >= T
           /\ NoOtherMember(c, old)
           /\ wcState' = [wcState EXCEPT ![c] = "ready"]
           /\ dealt' = [dealt EXCEPT ![old][c] = 0]
           /\ suspect' = suspect \cup {old}
           /\ left' = [left EXCEPT ![c] = left[c] \cup {old}]
           /\ cause' = [cause EXCEPT ![c] = ""]
           /\ event' = "startdeadline"
           /\ UNCHANGED <<now, up, width, running, deaf, wcMember, wcRoute,
                          lastProgress, stalled, nonCompletions, rested,
                          readState, readReader, readDealtAt, lastTake, rUp,
                          rAway, rDeaf>>

\* ReaderLiveness: a reader holding dealt reads and taking none for T is set
\* away; all its reads are redealt to an up, live reader.
ReaderDeadline(c) ==
    LET r == readReader[c] IN
        \E r2 \in Readers :
            /\ "deadreader" \notin Broken
            /\ readState[c] = "held"
            /\ now - readDealtAt[c] >= T
            /\ r2 # r /\ LiveReader(r2)
            /\ rAway' = [rAway EXCEPT ![r] = TRUE]
            /\ readReader' = [x \in Cards |->
                                 IF ReadHeld(x, r) THEN r2 ELSE readReader[x]]
            /\ readDealtAt' = [x \in Cards |->
                                  IF ReadHeld(x, r) THEN now ELSE readDealtAt[x]]
            /\ event' = "readerdeadline"
            /\ UNCHANGED <<now, up, width, running, dealt, suspect, deaf,
                           cause, wcState, wcMember, wcRoute, lastProgress,
                           stalled, nonCompletions, rested, readState,
                           lastTake, rUp, rDeaf, left>>

\* NoIdleWithWork: the server names the blocking cause per card in `where`.
\* "silentidle" is the reversed witness that never names it (and its tick does
\* not wait for a name, so the clock passes D).
NameCause(c) ==
    /\ "silentidle" \notin Broken
    /\ wcState[c] = "ready"
    /\ FreeExists
    /\ cause[c] # CauseOf(c)
    /\ cause' = [cause EXCEPT ![c] = CauseOf(c)]
    /\ event' = "namecause"
    /\ UNCHANGED <<now, up, width, running, dealt, suspect, deaf, wcState,
                   wcMember, wcRoute, lastProgress, stalled, nonCompletions,
                   rested, readState, readReader, readDealtAt, lastTake, rUp,
                   rAway, rDeaf, left>>

\* a member that never takes what it is dealt (at most one, so the fleet keeps
\* a live member to redeal to)
MemberDeaf(m) ==
    /\ m \notin deaf
    /\ Cardinality(deaf) < 1
    /\ now <= MaxTime - T
    /\ deaf' = deaf \cup {m}
    /\ event' = "memberdeaf"
    /\ UNCHANGED <<now, up, width, running, dealt, suspect, cause, wcState,
                   wcMember, wcRoute, lastProgress, stalled, nonCompletions,
                   rested, readState, readReader, readDealtAt, lastTake, rUp,
                   rAway, rDeaf, left>>

\* a reader that stops taking while holding its reads (hulk); at most one, so
\* the fleet keeps a live reader to redeal to
ReaderDeaf(r) ==
    /\ ~rDeaf[r]
    /\ DeafCount < 1
    /\ now <= MaxTime - T
    /\ rDeaf' = [rDeaf EXCEPT ![r] = TRUE]
    /\ event' = "readerdeaf"
    /\ UNCHANGED <<now, up, width, running, dealt, suspect, deaf, cause,
                   wcState, wcMember, wcRoute, lastProgress, stalled,
                   nonCompletions, rested, readState, readReader, readDealtAt,
                   lastTake, rUp, rAway, left>>

\* the discrete clock. It never passes a bound whose deadline has not fired
\* (so the invariants hold even in a reversed witness), and in the code's model
\* it never passes D while a ready card in front of a free member is unnamed.
Tick ==
    /\ now < MaxTime
    /\ \A c \in Cards : wcState[c] = "running" =>
         now + 1 - lastProgress[c] <= T
    /\ \A c \in Cards : wcState[c] = "dealt" =>
         now + 1 - dealt[wcMember[c]][c] <= T
    /\ (now + 1 <= D \/ "silentidle" \in Broken \/
        \A c \in Cards : (wcState[c] = "ready" /\ FreeExists) => Named(c))
    /\ now' = now + 1
    /\ event' = "tick"
    /\ UNCHANGED <<up, width, running, dealt, suspect, deaf, cause, wcState,
                   wcMember, wcRoute, lastProgress, stalled, nonCompletions,
                   rested, readState, readReader, readDealtAt, lastTake, rUp,
                   rAway, rDeaf, left>>

Next ==
    \/ \E c \in Cards, m \in Members : Deal(c, m)
    \/ \E c \in Cards, m \in Members : TakeWork(c, m)
    \/ \E c \in Cards : TakeRead(c)
    \/ \E c \in Cards : Finish(c)
    \/ \E c \in Cards : Progress(c)
    \/ \E c \in Cards : Stall(c)
    \/ \E c \in Cards : StartDeadline(c)
    \/ \E c \in Cards : ProgressDeadline(c)
    \/ \E c \in Cards : ReaderDeadline(c)
    \/ \E c \in Cards : NameCause(c)
    \/ \E m \in Members : MemberDeaf(m)
    \/ \E r \in Readers : ReaderDeaf(r)
    \/ Tick

Spec == Init /\ [][Next]_vars
        /\ WF_vars(Tick)
        /\ \A c \in Cards :
             /\ WF_vars(\E m \in Members : Deal(c, m))
             /\ WF_vars(\E m \in Members : TakeWork(c, m))
             /\ WF_vars(TakeRead(c))
             /\ WF_vars(Finish(c))
             /\ WF_vars(StartDeadline(c))
             /\ WF_vars(ProgressDeadline(c))
             /\ WF_vars(ReaderDeadline(c))
             /\ WF_vars(NameCause(c))

\* ------------------------------------------------------------- invariants

\* RouteProgress's safety: no in-flight run is past its progress bound
NoRunWithoutProgressPastT ==
    \A c \in Cards : wcState[c] = "running" =>
        now - lastProgress[c] <= T

\* DealtStartsOrRedeals' safety: no card is dealt past its start bound
NoDealtPastT ==
    \A c \in Cards : wcState[c] = "dealt" =>
        now - dealt[wcMember[c]][c] <= T

\* ReaderLiveness's safety: a reader set away holds no read
NoAwayReaderHolds ==
    \A r \in Readers : rAway[r] =>
        ~\E c \in Cards : ReadHeld(c, r)

\* NoIdleWithWork: at a tick past D, every ready card in front of a free member
\* has its named cause in `where` (route, gate or width); "none" means dealt
IdleHasCause ==
    (event = "tick" /\ now > D) =>
        \A c \in Cards :
            (wcState[c] = "ready" /\ FreeExists) => Named(c)

\* --------------------------------------------------------------- liveness

\* a card some up, non-suspect, eligible member and some serving route can run
\* is eventually running or done (or its route or member is gone)
WorkStarts ==
    \A c \in Cards :
        ( (wcState[c] \in {"ready", "dealt"} /\ CanRun(c))
          ~> (wcState[c] \in {"running", "done"} \/ ~CanRun(c)) )

\* a stale run is ended or progresses: ultramarine cannot hold its lane forever
RunProgress ==
    \A c \in Cards :
        ( (wcState[c] = "running" /\ now - lastProgress[c] >= T)
          ~> (wcState[c] # "running" \/ now - lastProgress[c] < T) )

\* a read with an up live reader is eventually taken (or no live reader remains)
ReadTaken ==
    \A c \in Cards :
        ( (readState[c] = "held" /\ LiveReaderExists)
          ~> (readState[c] = "done" \/ ~LiveReaderExists) )

=============================================================================
