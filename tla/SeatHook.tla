------------------------------ MODULE SeatHook ------------------------------
\* The coordinator hooks in (cmd/nova-sprint/hook.go, internal/sprint/hook.go;
\* docs/SPEC-SPRINT.md, "The hook"; the owner, 2026-10-10: "You MUST be required
\* to 'hook' in as coordinator." ... "And you must PROVE YOU HAVE HOOKED IN before
\* it will allow you to do anything."). It replaces the push proof: the seat had
\* answered a PROOF file by hand while nothing in its session read the pushes,
\* and about 1,200 went unread for hours.
\*
\* The server owns the subscription. The seat's session runs `nova-sprint hook`
\* under a Monitor: one connection to the server, over which the server streams
\* every judgment and push, each with an id, and a challenge. The hook counts only
\* once the session has answered the challenge it read off that stream, over the
\* same connection (`hook --prove`, relayed by the hook process). The server
\* challenges again on a cadence; a challenge unanswered past its bound unhooks.
\* A drop of the connection is recorded as gone at once, and `nova-sprint unhook`
\* as away; each is pushed to the owner. Every coordinator command reads the
\* server's record (the store key seat-hook) and is refused unless the hook is
\* live and proven and no delivered push is unacknowledged past its bound.
\*
\* The state:
\*   sub        the server's subscription: "none" or "open"
\*   proven     the server's word: the subscription answered a challenge it sent
\*   chal       the challenge outstanding on the subscription, 0 for none
\*   chalAt     when the outstanding challenge was sent (0 for none)
\*   issued     challenges issued so far (each fresh: 1..MaxChallenges)
\*   answered   the challenge the proof answered, 0 for none
\*   seen       the challenges the session read off its stream
\*   reading    the session reads its stream (a Monitor live, its events read)
\*   now        the running clock: one step a look
\*   live       the server's record's Live, the gate reads it, not a variable of
\*              the server: the proof's own bound (the proof time plus HookEvery +
\*              HookAnswerBound, so RechallengeDue is live - HookAnswerBound)
\*              until the look sends a re-challenge, then that challenge's own
\*              bound (its send time plus HookAnswerBound)
\*   delivered, acked, overdue
\*              the pushes delivered over the subscription, acknowledged over
\*              it, and past the ack bound
\*   rec        the record the gate reads: its state and whether it says proven
\*   ends, told the subscription's ends and the pushes of them to the owner
\*   cmdBad     ghost: a coordinator command ran against the rule
\*
\* The outside: the session stops reading, the connection drops, unhook, and the
\* server starts again (a record left hooked by a dead server is rewritten gone);
\* each at any time (Outside = TRUE). Time is a look: the clock steps (Tick) and
\* the look takes its turn, so TLC chooses when the re-challenge goes (at or
\* after HookEvery; the look's own 2 s period and the serial line's delay are
\* folded into that choice) and how late a look has run. A challenge passes its
\* bound only while the session does not read (an answering session answers
\* within it), and a delivered push passes its ack bound at any time.
\*
\* The live window: Live is the proof's own bound (the proof time plus HookEvery +
\* HookAnswerBound, internal/sprint/hook.go HookProven). The look re-challenges
\* once HookEvery has passed, and only then does the record's Live move on to the
\* re-challenge's own bound (ChallengeLive, the code's challengeLive). So between
\* the proof's Live and the re-challenge's bound the gate refuses HookFirst even
\* though the re-challenge is inside its bound, and it can refuse up to one look
\* period early. The model's Gate reads the record's Live; the old model refused
\* only on the outstanding challenge's bound (lateC), which is looser than the
\* code.
\*
\* What it leaves out: the record is written by the same server step that
\* changes the subscription, under the server's line of control, as a forwarded
\* coordinator verb runs; a verb run on the store directly (--redis) reads the
\* record between two such steps, and a server that died leaves its record
\* hooked until the next server's start rewrites it gone (ServerStart) or its
\* Live bound lapses (HookLive). A hook while one is open replaces it, which is
\* an end and a hook; the model hooks only when none is open. The ack is
\* cumulative in the code (every id up to the one named); one id at a time here.
\*
\* Reversed witnesses (Broken):
\*   "nostream"      the challenge reaches the session off the stream (the PROOF
\*                   file's name): a proof by a session that read nothing
\*                   (ProofMeansRead)
\*   "nosub"         a proof counts with no live subscription (a hand pong):
\*                   proven with sub = "none" (ProvenIsSubscribed)
\*   "noproof"       the gate reads the record's state and not its proof: a
\*                   command runs on a hook never proven (CommandSafe)
\*   "noack"         the gate ignores the pushes not acknowledged: a delivered
\*                   push past its bound neither acknowledged nor blocking
\*                   (AckedOrBlocking)
\*   "nomiss"        a missed challenge does not unhook and the gate does not
\*                   read the deadline: a session that stopped reading keeps
\*                   the seat (CommandSafe)
\*   "silentdrop"    a drop is not recorded: the record says hooked with no
\*                   subscription, and the drop is never told (RecordTrue)
\*   "strictcadence" the cadence's bound runs whether or not the session reads:
\*                   an answering seat loses its hook (HookStays)
\*   "nostartrewrite" the next server's start leaves a dead server's record
\*                   hooked: rec.state = "hooked" with no subscription, so no
\*                   rewrite and the owner not told (RecordTrue)
\*   "nobus"         the bus push is not wanted: the challenge reaches the session
\*                   without its bus half, the gate does not read the shared
\*                   record, and a bus subscription that fails does not end the
\*                   hook: a command runs with no bus push (CommandSafe)
\*
\* The bus push (2026-10-11, the owner: "For a coordinator, this is hooking into
\* to sprint push notifications and message bus push notifications."; docs/
\* SPEC-SPRINT.md, "The two pushes"; the shared hook's own model is nova-tools'
\* tla/Hook.tla once that module lands). The hook process holds a second
\* subscription, to the coordinator's own bus stream (busSub), and arms the
\* shared record hook:<name> in the bus store (bus = "armed"). Each challenge has
\* a bus half the hook process sends to its own stream; the session sees the
\* challenge only once that half comes back off the subscription (Read wants
\* busSub), and the one answer proves both: the server takes its half, then the
\* hook process records the bus push proven (bus = "proven", live until busLive,
\* the proof's plus HookEvery + HookAnswerBound: HookSharedBound). A bus
\* subscription that fails ends the hook (the hook process closes its server
\* connection: BusDrop is a Drop). Unhook records the shared record unhooked
\* (bus = "none"). The gate wants both (sprint.HookGateCall).
EXTENDS Naturals, FiniteSets

CONSTANTS Items, MaxChallenges, MaxTime, HookEvery, HookAnswerBound, Broken, Outside

VARIABLES sub, proven, chal, chalAt, issued, answered, seen, reading, now, live,
          delivered, acked, overdue, rec, ends, told, cmdBad, busSub, bus, busLive

vars == <<sub, proven, chal, chalAt, issued, answered, seen, reading, now, live,
          delivered, acked, overdue, rec, ends, told, cmdBad, busSub, bus, busLive>>

busVars == <<busSub, bus, busLive>>

Challenges == 1..MaxChallenges
States == {"none", "hooked", "away", "gone"}

TypeOK ==
    /\ sub \in {"none", "open"}
    /\ proven \in BOOLEAN
    /\ chal \in 0..MaxChallenges
    /\ chalAt \in 0..MaxTime
    /\ issued \in 0..MaxChallenges
    /\ answered \in 0..MaxChallenges
    /\ seen \subseteq Challenges
    /\ reading \in BOOLEAN
    /\ now \in 0..MaxTime
    /\ live \in 0..(MaxTime + HookEvery + HookAnswerBound)
    /\ delivered \subseteq Items
    /\ acked \subseteq delivered
    /\ overdue \subseteq delivered
    /\ rec \in [state : States, proven : BOOLEAN]
    /\ ends \in Nat
    /\ told \in Nat
    /\ cmdBad \in BOOLEAN
    /\ busSub \in BOOLEAN
    /\ bus \in {"none", "armed", "proven"}
    /\ busLive \in 0..(MaxTime + HookEvery + HookAnswerBound)

Init ==
    /\ sub = "none"
    /\ proven = FALSE
    /\ chal = 0
    /\ chalAt = 0
    /\ issued = 0
    /\ answered = 0
    /\ seen = {}
    /\ reading = FALSE
    /\ now = 0
    /\ live = 0
    /\ delivered = {}
    /\ acked = {}
    /\ overdue = {}
    /\ rec = [state |-> "none", proven |-> FALSE]
    /\ ends = 0
    /\ told = 0
    /\ cmdBad = FALSE
    /\ busSub = FALSE
    /\ bus = "none"
    /\ busLive = 0

\* The pushes delivered and neither acknowledged nor within their bound.
Blocking == overdue \ acked

\* The proof's cadence deadline: the record's Live is the proof's HookEvery +
\* HookAnswerBound, so the look may send the next challenge once now reaches
\* live - HookAnswerBound (the proof's own time plus HookEvery). It is read only
\* while chal = 0, when live is the latest proof's bound.
RechallengeDue == live - HookAnswerBound

\* The outstanding challenge passed its answer bound unanswered: only while the
\* session does not read ("strictcadence": whether or not it reads).
LateC ==
    /\ chal # 0
    /\ now > chalAt + HookAnswerBound
    /\ ("strictcadence" \in Broken \/ ~reading)

\* The rule: a live subscription, proven by a challenge the session read off it,
\* the current challenge within its bound, no push blocking.
Rule ==
    /\ sub = "open"
    /\ proven
    /\ answered \in seen
    /\ ~LateC
    /\ Blocking = {}
    /\ busSub

\* The gate as the code computes it, from the record and the clock
\* (sprint.HookGate: HookLive, then the oldest push unacknowledged). HookLive
\* reads the record's Live: now <= live. "noproof" (the gate reads the record's
\* state and not its proof) and "nomiss" (the gate does not read the deadline)
\* each drop the Live check; "nomiss" also drops the challenge's own bound.
Gate ==
    /\ rec.state = "hooked"
    /\ ("noproof" \in Broken \/ rec.proven)
    /\ ("noproof" \in Broken \/ "nomiss" \in Broken \/ now <= live)
    /\ ("nomiss" \in Broken \/ ~LateC)
    /\ ("noack" \in Broken \/ Blocking = {})
    /\ ("nobus" \in Broken \/ bus # "none")
    /\ ("nobus" \in Broken \/ "noproof" \in Broken \/ bus = "proven")
    /\ ("nobus" \in Broken \/ "noproof" \in Broken \/ "nomiss" \in Broken \/ now <= busLive)

\* `nova-sprint hook`: a fresh subscription, unproven, its first challenge sent at
\* once (chalAt = now). The session may or may not read what it streams. The
\* record has no Live until a proof sets it.
Hook ==
    /\ sub = "none"
    /\ issued < MaxChallenges
    /\ sub' = "open"
    /\ proven' = FALSE
    /\ issued' = issued + 1
    /\ chal' = issued + 1
    /\ chalAt' = now
    /\ answered' = 0
    /\ reading' \in (IF Outside THEN BOOLEAN ELSE {TRUE})
    /\ live' = 0
    /\ delivered' = {}
    /\ acked' = {}
    /\ overdue' = {}
    /\ rec' = [state |-> "hooked", proven |-> FALSE]
    /\ busSub' = TRUE
    /\ bus' = "armed"
    /\ busLive' = 0
    /\ UNCHANGED <<now, seen, ends, told, cmdBad>>

\* The session reads the challenge off its stream (the Monitor's event).
Read ==
    /\ sub = "open"
    /\ reading
    /\ ("nobus" \in Broken \/ busSub)
    /\ chal # 0
    /\ chal \notin seen
    /\ seen' = seen \cup {chal}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, reading, now, live,
                   delivered, acked, overdue, rec, ends, told, cmdBad, busVars>>

\* "nostream": the challenge is somewhere besides the stream; the session that
\* knows it need not have read anything.
Knows(v) == IF "nostream" \in Broken THEN v \in 1..issued ELSE v \in seen

\* hook --prove <v>, over the subscription: the server accepts the outstanding
\* challenge and nothing else. The proof sets the record's Live to the proof's
\* own bound, the proof time plus HookEvery + HookAnswerBound (HookProven).
Prove(v) ==
    /\ Knows(v)
    /\ sub = "open"
    /\ v = chal
    /\ chal # 0
    /\ proven' = TRUE
    /\ answered' = v
    /\ chal' = 0
    /\ chalAt' = 0
    /\ live' = now + HookEvery + HookAnswerBound
    /\ rec' = [rec EXCEPT !.proven = TRUE]
    /\ bus' = IF bus = "none" THEN "none" ELSE "proven"
    /\ busLive' = IF bus = "none" THEN busLive ELSE now + HookEvery + HookAnswerBound
    /\ UNCHANGED <<sub, issued, seen, reading, now, delivered, acked, overdue,
                   ends, told, cmdBad, busSub>>

\* "nosub": a pong with no subscription (the old seat pong against a record).
HandPong ==
    /\ "nosub" \in Broken
    /\ sub = "none"
    /\ issued > 0
    /\ proven' = TRUE
    /\ answered' = issued
    /\ rec' = [rec EXCEPT !.proven = TRUE]
    /\ UNCHANGED <<sub, chal, chalAt, issued, seen, reading, now, live, delivered,
                   acked, overdue, ends, told, cmdBad, busVars>>

\* The cadence: the look re-challenges a proven hook once HookEvery has passed
\* (now >= RechallengeDue); it sends the fresh challenge but does not move the
\* record's Live (ChallengeLive does that, as the code's challengeLive does).
Rechallenge ==
    /\ sub = "open"
    /\ proven
    /\ chal = 0
    /\ issued < MaxChallenges
    /\ now >= RechallengeDue
    /\ issued' = issued + 1
    /\ chal' = issued + 1
    /\ chalAt' = now
    /\ UNCHANGED <<sub, proven, answered, seen, reading, now, live, delivered,
                   acked, overdue, rec, ends, told, cmdBad, busVars>>

\* The look moves the record's Live to the re-challenge's own bound, its send time
\* plus HookAnswerBound, when that is later than the Live the record holds
\* (challengeLive; a failed write keeps the earlier bound). Until this step the
\* record still holds the proof's Live, so the gate refuses HookFirst while the
\* re-challenge is inside its bound: the code is stricter than a gate that reads
\* only the outstanding challenge's bound.
ChallengeLive ==
    /\ sub = "open"
    /\ proven
    /\ chal # 0
    /\ live < chalAt + HookAnswerBound
    /\ live' = chalAt + HookAnswerBound
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, delivered, acked, overdue, rec, ends, told, cmdBad, busVars>>

\* A missed challenge unhooks: the record says gone and the owner is told
\* ("nomiss": the hook stays).
Miss ==
    /\ sub = "open"
    /\ LateC
    /\ "nomiss" \notin Broken
    /\ sub' = "none"
    /\ proven' = FALSE
    /\ chal' = 0
    /\ chalAt' = 0
    /\ live' = 0
    /\ rec' = [state |-> "gone", proven |-> FALSE]
    /\ ends' = ends + 1
    /\ told' = told + 1
    /\ busSub' = FALSE
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad, bus, busLive>>

\* A judgment or push is delivered over the subscription, with its id.
Deliver(i) ==
    /\ sub = "open"
    /\ i \notin delivered
    /\ delivered' = delivered \cup {i}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, live, acked, overdue, rec, ends, told, cmdBad, busVars>>

\* hook --ack <id>, over the subscription, by a session that read it.
Ack(i) ==
    /\ sub = "open"
    /\ reading
    /\ i \in delivered \ acked
    /\ acked' = acked \cup {i}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, live, delivered, overdue, rec, ends, told, cmdBad, busVars>>

\* A delivered push passes its ack bound.
Overdue(i) ==
    /\ i \in delivered \ (acked \cup overdue)
    /\ overdue' = overdue \cup {i}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, live, delivered, acked, rec, ends, told, cmdBad, busVars>>

\* A coordinator command: it runs only through the gate; the ghost marks a run
\* against the rule.
Cmd ==
    /\ Gate
    /\ cmdBad' = (cmdBad \/ ~Rule)
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, live, delivered, acked, overdue, rec, ends, told, busVars>>

\* The outside: the session stops reading (its Monitor died, or it does not
\* read its events) while the hook process stays connected.
StopReading ==
    /\ Outside
    /\ reading
    /\ reading' = FALSE
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, now, live,
                   delivered, acked, overdue, rec, ends, told, cmdBad, busVars>>

\* The connection drops: recorded gone at once and told ("silentdrop": neither).
Drop ==
    /\ Outside
    /\ sub = "open"
    /\ sub' = "none"
    /\ proven' = FALSE
    /\ chal' = 0
    /\ chalAt' = 0
    /\ live' = IF "silentdrop" \in Broken THEN live ELSE 0
    /\ rec' = IF "silentdrop" \in Broken THEN rec
              ELSE [state |-> "gone", proven |-> FALSE]
    /\ ends' = ends + 1
    /\ told' = IF "silentdrop" \in Broken THEN told ELSE told + 1
    /\ busSub' = FALSE
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad, bus, busLive>>

\* The bus subscription fails (three reads in a row): the hook process closes its
\* server connection, which the server records as a drop ("nobus": the hook goes
\* on without its bus subscription).
BusDrop ==
    /\ Outside
    /\ busSub
    /\ busSub' = FALSE
    /\ IF "nobus" \in Broken
       THEN UNCHANGED <<sub, proven, chal, chalAt, live, rec, ends, told>>
       ELSE /\ sub = "open"
            /\ sub' = "none"
            /\ proven' = FALSE
            /\ chal' = 0
            /\ chalAt' = 0
            /\ live' = 0
            /\ rec' = [state |-> "gone", proven |-> FALSE]
            /\ ends' = ends + 1
            /\ told' = told + 1
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad, bus, busLive>>

\* nova-sprint unhook: recorded away and told.
Unhook ==
    /\ Outside
    /\ sub = "open"
    /\ sub' = "none"
    /\ proven' = FALSE
    /\ chal' = 0
    /\ chalAt' = 0
    /\ live' = 0
    /\ rec' = [state |-> "away", proven |-> FALSE]
    /\ ends' = ends + 1
    /\ told' = told + 1
    /\ busSub' = FALSE
    /\ bus' = "none"
    /\ busLive' = 0
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad>>

\* The next server's start: a record left hooked by a server that died is a
\* subscription that server held, gone with it; it is rewritten gone, its Live
\* cleared, and the owner told ("nostartrewrite": neither, so the record still
\* says hooked with no subscription).
ServerStart ==
    /\ Outside
    /\ rec.state = "hooked"
    /\ sub' = "none"
    /\ proven' = FALSE
    /\ chal' = 0
    /\ chalAt' = 0
    /\ live' = IF "nostartrewrite" \in Broken THEN live ELSE 0
    /\ rec' = IF "nostartrewrite" \in Broken THEN rec
              ELSE [state |-> "gone", proven |-> FALSE]
    /\ ends' = ends + 1
    /\ told' = IF "nostartrewrite" \in Broken THEN told ELSE told + 1
    /\ busSub' = FALSE
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad, bus, busLive>>

\* The look's clock: one step a turn of the look, so TLC chooses when the
\* re-challenge goes and how late a look has run.
Tick ==
    /\ now < MaxTime
    /\ now' = now + 1
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading, live,
                   delivered, acked, overdue, rec, ends, told, cmdBad, busVars>>

Next ==
    \/ Hook \/ Read \/ \E v \in Challenges : Prove(v) \/ HandPong
    \/ Rechallenge \/ ChallengeLive \/ Miss
    \/ \E i \in Items : Deliver(i) \/ Ack(i) \/ Overdue(i)
    \/ Cmd \/ StopReading \/ Drop \/ BusDrop \/ Unhook \/ ServerStart \/ Tick

Spec == Init /\ [][Next]_vars
        /\ WF_vars(Read) /\ WF_vars(\E v \in Challenges : Prove(v))
        /\ WF_vars(Rechallenge) /\ WF_vars(Miss) /\ WF_vars(Hook)

\* No coordinator command succeeds without a live, proven hook and every
\* delivered push past its bound acknowledged.
CommandSafe == ~cmdBad

\* Every delivered push is acknowledged or blocks the gate.
AckedOrBlocking == \A i \in delivered : i \in acked \/ i \notin overdue \/ ~Gate

\* A proof is the answer to a challenge the session read off its stream.
ProofMeansRead == proven => answered \in seen

\* A proof stands only on a live subscription.
ProvenIsSubscribed == proven => sub = "open"

\* The hook's server subscription is open only while its bus subscription is:
\* one hook, both pushes.
BothOrNeither == sub = "open" => busSub

\* The record the gate reads says hooked only while the subscription is open,
\* and every end of a subscription is told to the owner.
RecordTrue ==
    /\ rec.state = "hooked" => sub = "open"
    /\ rec.proven => proven
    /\ told = ends

\* A hooked, answering seat keeps the hook (Outside = FALSE: it reads, and
\* nothing drops or unhooks it): once open, the subscription is proven, and it
\* stays open.
KeepsHook == (sub = "open") ~> (sub = "open" /\ proven)
HookStays == [](sub = "open" => [](sub = "open"))
=============================================================================
