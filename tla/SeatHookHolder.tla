--------------------------- MODULE SeatHookHolder ---------------------------
\* The seat's push loop holds the seat's hook (cmd/nova-sprint/pushhook.go,
\* hookholder.go; docs/SPEC-SPRINT.md, "The hook"; the seat, 2026-10-12: the
\* Studio's standing loops, friend sync among them, were refused "You must hook
\* in first" for every pass of the hours no session held `nova-sprint hook`
\* under a Monitor). The loop (`inbox --wait --push seat`, the unit seat install
\* writes) is the coordinator's own process and runs as long as the machine
\* does: it holds the hook's connection across every session of the seat, so a
\* proof stands until the connection ends, not until the session that proved it
\* ends. It answers no challenge: the proof is the session's alone. It delivers
\* each challenge into the seat's inbox, where the session reads its judgments,
\* and acknowledges a push only once the judgment it is of is written there.
\*
\* This module extends SeatHook (the server's subscription, its challenge and
\* cadence, the record and the gate, all unchanged) with the loop:
\*   loop      the loop's hold: "down" (no connection; it opens one at its next
\*             beat), "held" (its connection is open), "away" (the seat unhooked:
\*             nothing until the seat's word), "missed" (a challenge went
\*             unanswered past its bound: nothing until the seat's word, so the
\*             owner is told once, not every bound)
\*   word      the seat's word arrived (`nova-sprint hook --actor <seat>`, over
\*             the loop's socket): hook in now, whatever ended the last one
\*   why       what the record's reason says of the last end: "-", "miss" (gone
\*             on an unanswered challenge) or "end" (anything else): the loop
\*             reads it before each connection, and so after its own restart
\*   inbox     the judgments the loop has written into the seat's inbox (its own
\*             delivery, independent of the stream)
\*   chalFile  the outstanding challenge is in the inbox (the loop delivered it)
\*   waited    the ticks spent down with a connection wanted (the beat)
\*   armAway, armMissed
\*             ghosts: the loop opened a connection the record forbade (an unhook
\*             standing; a missed challenge standing) with no word of the seat
\*
\* Reversed witnesses (Broken), beside SeatHook's:
\*   "rearmaway"    the loop re-arms an unhooked record on its own: an unhook
\*                  does not stand across the loop's beat or restart (UnhookStands)
\*   "rearmmissed"  the loop re-arms after a missed challenge on its own: the
\*                  owner is told every bound (MissStands)
\*   "norearm"      the loop never re-arms a dropped hook without the seat's
\*                  word: a lost connection is not re-armed within a beat
\*                  (ReArmWithinBeat)
\*   "ackunwritten" the loop acknowledges a push whose judgment it has not
\*                  written into the inbox (AckMeansWritten)
\*   "nofile"       the loop delivers no challenge into the inbox: a reading
\*                  session never sees one, and the held hook is never proven
\*                  (HeldIsProven, Outside = FALSE)
EXTENDS SeatHook

VARIABLES loop, word, why, inbox, chalFile, waited, armAway, armMissed

lvars == <<loop, word, why, inbox, chalFile, waited, armAway, armMissed>>
hvars == <<vars, d, loop, word, why, inbox, chalFile, waited, armAway, armMissed>>

HTypeOK ==
    /\ TypeOK
    /\ loop \in {"down", "held", "away", "missed"}
    /\ word \in BOOLEAN
    /\ why \in {"-", "miss", "end"}
    /\ inbox \subseteq Items
    /\ chalFile \in BOOLEAN
    /\ waited \in 0..2
    /\ armAway \in BOOLEAN /\ armMissed \in BOOLEAN

HInit ==
    /\ LegacyInit /\ DurableInit
    /\ loop = "down" /\ word = FALSE /\ why = "-" /\ inbox = {}
    /\ chalFile = FALSE /\ waited = 0 /\ armAway = FALSE /\ armMissed = FALSE

\* The record forbids a connection the loop opens on its own: the seat unhooked
\* (away), or the last hook went on an unanswered challenge (gone, miss). The
\* seat's word lifts both. The witnesses drop each read.
Forbidden ==
    /\ ~word
    /\ \/ (rec.state = "away" /\ "rearmaway" \notin Broken)
       \/ (rec.state = "gone" /\ why = "miss" /\ "rearmmissed" \notin Broken)

\* The loop wants a connection: down, none open, and the record not forbidding
\* one ("norearm": only on the seat's word).
Wants ==
    /\ loop = "down" /\ sub = "none" /\ ~Forbidden
    /\ ("norearm" \notin Broken \/ word)

\* The loop opens a connection (Hook: a fresh subscription, its challenge sent):
\* its hold is held, the seat's word spent, the challenge not yet delivered.
LoopHook ==
    /\ Wants
    /\ Hook
    /\ loop' = "held" /\ word' = FALSE /\ why' = "-" /\ chalFile' = FALSE /\ waited' = 0
    /\ armAway' = (armAway \/ (~word /\ rec.state = "away"))
    /\ armMissed' = (armMissed \/ (~word /\ rec.state = "gone" /\ why = "miss"))
    /\ UNCHANGED <<inbox, d>>

\* The loop delivers the outstanding challenge into the seat's inbox, once its bus
\* half came back (busSub): the file the session reads ("nofile": never).
DeliverChallenge ==
    /\ "nofile" \notin Broken
    /\ loop = "held" /\ sub = "open" /\ chal # 0 /\ busSub /\ ~chalFile
    /\ chalFile' = TRUE
    /\ UNCHANGED <<vars, d, loop, word, why, inbox, waited, armAway, armMissed>>

\* The session reads the challenge off the inbox the loop delivered it into
\* (SeatHook's Read: a reading session; "nostream" lets it know the challenge
\* from anywhere).
HRead ==
    /\ (chalFile \/ "nostream" \in Broken)
    /\ Read
    /\ UNCHANGED <<d, lvars>>

\* The session answers over the loop's socket; the loop carries it (Prove).
HProve(v) == Prove(v) /\ UNCHANGED <<d, lvars>>

\* The loop's own delivery writes a judgment into the inbox, hook or no hook.
Write(i) ==
    /\ i \in Items \ inbox
    /\ inbox' = inbox \cup {i}
    /\ UNCHANGED <<vars, d, loop, word, why, chalFile, waited, armAway, armMissed>>

\* The loop acknowledges a push over its connection once the judgment is written
\* into the inbox ("ackunwritten": before).
LoopAck(i) ==
    /\ loop = "held" /\ sub = "open"
    /\ i \in delivered \ acked
    /\ (i \in inbox \/ "ackunwritten" \in Broken)
    /\ acked' = acked \cup {i}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading, now,
                   live, delivered, overdue, rec, ends, told, cmdBad, cad, busVars, d, lvars>>

\* The server's side and the gate, as SeatHook has them; each end of the
\* subscription moves the loop's hold as the code does (pushhook.go run).
HAct == Act /\ UNCHANGED <<d, lvars>>
HDeliver(i) == Deliver(i) /\ UNCHANGED <<d, lvars>>
HOverdue(i) == Overdue(i) /\ UNCHANGED <<d, lvars>>
HCmd == Cmd /\ UNCHANGED <<d, lvars>>
HStopReading == StopReading /\ UNCHANGED <<d, lvars>>

\* A lapse ends the subscription (gone): the loop opens another at its beat
\* ("lapsetwice" keeps the subscription, and the hold with it).
HLapse ==
    /\ Lapse
    /\ loop' = IF "lapsetwice" \in Broken THEN loop ELSE "down"
    /\ why' = IF "lapsetwice" \in Broken THEN why ELSE "end"
    /\ chalFile' = FALSE
    /\ UNCHANGED <<d, word, inbox, waited, armAway, armMissed>>

\* A missed challenge: gone on the record with its reason; the loop holds nothing
\* until the seat's word.
HMiss ==
    /\ Miss
    /\ loop' = "missed" /\ why' = "miss" /\ chalFile' = FALSE
    /\ UNCHANGED <<d, word, inbox, waited, armAway, armMissed>>

\* A drop, the bus subscription failing, or the next server's start: gone; the
\* loop opens another at its beat.
HDrop ==
    /\ Drop
    /\ loop' = "down" /\ why' = "end" /\ chalFile' = FALSE
    /\ UNCHANGED <<d, word, inbox, waited, armAway, armMissed>>

HBusDrop ==
    /\ BusDrop
    /\ loop' = IF "nobus" \in Broken THEN loop ELSE "down"
    /\ why' = IF "nobus" \in Broken THEN why ELSE "end"
    /\ chalFile' = IF "nobus" \in Broken THEN chalFile ELSE FALSE
    /\ UNCHANGED <<d, word, inbox, waited, armAway, armMissed>>

HServerStart ==
    /\ ServerStart
    /\ loop' = IF loop = "held" THEN "down" ELSE loop
    /\ why' = IF loop = "held" THEN "end" ELSE why
    /\ chalFile' = FALSE
    /\ UNCHANGED <<d, word, inbox, waited, armAway, armMissed>>

\* nova-sprint unhook, over the loop's socket: away; the loop holds nothing until
\* the seat's word, across its own restarts (the record says away).
HUnhook ==
    /\ Unhook
    /\ loop' = "away" /\ why' = "end" /\ chalFile' = FALSE
    /\ UNCHANGED <<d, word, inbox, waited, armAway, armMissed>>

\* The seat's word, nova-sprint hook --actor <seat>, to a loop holding nothing:
\* hook in now.
SeatWord ==
    /\ Outside
    /\ loop \in {"away", "missed"}
    /\ word' = TRUE /\ loop' = "down"
    /\ UNCHANGED <<vars, d, why, inbox, chalFile, waited, armAway, armMissed>>

\* The loop restarts (its supervisor): its memory is gone, the record is not. It
\* holds nothing until it has read the record (Wants reads it: Forbidden).
LoopRestart ==
    /\ Outside
    /\ loop # "held"
    /\ loop' = "down" /\ word' = FALSE /\ waited' = 0
    /\ UNCHANGED <<vars, d, why, inbox, chalFile, armAway, armMissed>>

\* The clock, as SeatHook's Tick, counting the ticks spent down with a connection
\* wanted; the loop's beat opens one before a second tick passes ("norearm": the
\* clock runs on).
HTick ==
    /\ Tick
    /\ ("norearm" \in Broken \/ ~(Wants /\ issued < MaxChallenges /\ waited >= 1))
    /\ waited' = IF loop = "down" /\ sub = "none" THEN (IF waited < 2 THEN waited + 1 ELSE 2) ELSE 0
    /\ UNCHANGED <<d, loop, word, why, inbox, chalFile, armAway, armMissed>>

HNext ==
    \/ LoopHook \/ DeliverChallenge \/ HRead \/ \E v \in Challenges : HProve(v)
    \/ \E i \in Items : Write(i) \/ LoopAck(i) \/ HDeliver(i) \/ HOverdue(i)
    \/ HAct \/ HLapse \/ HMiss \/ HCmd \/ HStopReading
    \/ HDrop \/ HBusDrop \/ HServerStart \/ HUnhook \/ SeatWord \/ LoopRestart \/ HTick

HSpec ==
    /\ HInit /\ [][HNext]_hvars
    /\ WF_hvars(HRead) /\ WF_hvars(\E v \in Challenges : HProve(v))
    /\ WF_hvars(HAct) /\ WF_hvars(HLapse) /\ WF_hvars(HMiss)
    /\ WF_hvars(LoopHook) /\ WF_hvars(DeliverChallenge)
    /\ WF_hvars(\E i \in Items : Write(i)) /\ WF_hvars(\E i \in Items : LoopAck(i))

\* An unhook stands: the loop never opens a connection on an unhooked record
\* without the seat's word, at its beat or after its restart.
UnhookStands == ~armAway

\* A missed challenge stands: the loop never opens a connection after one without
\* the seat's word (the owner told once, not every bound).
MissStands == ~armMissed

\* A lost connection is re-armed within one beat: the loop is never down and
\* wanting for two ticks.
ReArmWithinBeat == (Wants /\ issued < MaxChallenges) => waited <= 1

\* The loop acknowledges only what it has written into the inbox.
AckMeansWritten == acked \subseteq inbox

\* A held hook is proven, when the session reads and nothing outside ends it
\* (Outside = FALSE): the challenge reaches the inbox, the session reads it and
\* answers, the loop carries the answer.
HeldIsProven == (loop = "held" /\ sub = "open") ~> proven
=============================================================================
