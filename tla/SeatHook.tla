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
\*   issued     challenges issued so far (each fresh: 1..MaxChallenges)
\*   answered   the challenge the proof answered, 0 for none
\*   seen       the challenges the session read off its stream
\*   reading    the session reads its stream (a Monitor live, its events read)
\*   lateC      the outstanding challenge passed its answer bound unanswered
\*   delivered, acked, overdue
\*              the pushes delivered over the subscription, acknowledged over
\*              it, and past the ack bound
\*   rec        the record the gate reads: its state and whether it says proven
\*   ends, told the subscription's ends and the pushes of them to the owner
\*   cmdBad     ghost: a coordinator command ran against the rule
\*
\* The outside: the session stops reading, the connection drops, unhook; each
\* at any time (Outside = TRUE). Time is abstract: a challenge passes its bound
\* only while the session does not read (an answering session answers within
\* it), and a delivered push passes its ack bound at any time.
\*
\* What it leaves out: the record is written by the same server step that
\* changes the subscription, under the server's line of control, as a forwarded
\* coordinator verb runs; a verb run on the store directly (--redis) reads the
\* record between two such steps, and a server that died leaves its record
\* hooked until its Live bound lapses (HookLive). A hook while one is open
\* replaces it, which is an end and a hook; the model hooks only when none is
\* open. The ack is cumulative in the code (every id up to the one named); one
\* id at a time here.
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
EXTENDS Naturals, FiniteSets

CONSTANTS Items, MaxChallenges, Broken, Outside

VARIABLES sub, proven, chal, issued, answered, seen, reading, lateC,
          delivered, acked, overdue, rec, ends, told, cmdBad

vars == <<sub, proven, chal, issued, answered, seen, reading, lateC,
          delivered, acked, overdue, rec, ends, told, cmdBad>>

Challenges == 1..MaxChallenges
States == {"none", "hooked", "away", "gone"}

TypeOK ==
    /\ sub \in {"none", "open"}
    /\ proven \in BOOLEAN
    /\ chal \in 0..MaxChallenges
    /\ issued \in 0..MaxChallenges
    /\ answered \in 0..MaxChallenges
    /\ seen \subseteq Challenges
    /\ reading \in BOOLEAN
    /\ lateC \in BOOLEAN
    /\ delivered \subseteq Items
    /\ acked \subseteq delivered
    /\ overdue \subseteq delivered
    /\ rec \in [state : States, proven : BOOLEAN]
    /\ ends \in Nat
    /\ told \in Nat
    /\ cmdBad \in BOOLEAN

Init ==
    /\ sub = "none"
    /\ proven = FALSE
    /\ chal = 0
    /\ issued = 0
    /\ answered = 0
    /\ seen = {}
    /\ reading = FALSE
    /\ lateC = FALSE
    /\ delivered = {}
    /\ acked = {}
    /\ overdue = {}
    /\ rec = [state |-> "none", proven |-> FALSE]
    /\ ends = 0
    /\ told = 0
    /\ cmdBad = FALSE

\* The pushes delivered and neither acknowledged nor within their bound.
Blocking == overdue \ acked

\* The rule: a live subscription, proven by a challenge the session read off it,
\* the current challenge within its bound, no push blocking.
Rule ==
    /\ sub = "open"
    /\ proven
    /\ answered \in seen
    /\ ~lateC
    /\ Blocking = {}

\* The gate as the code computes it, from the record and the clock
\* (sprint.HookGate: HookLive, then the oldest push unacknowledged).
Gate ==
    /\ rec.state = "hooked"
    /\ ("noproof" \in Broken \/ rec.proven)
    /\ ("nomiss" \in Broken \/ ~lateC)
    /\ ("noack" \in Broken \/ Blocking = {})

\* `nova-sprint hook`: a fresh subscription, unproven, its first challenge sent at
\* once. The session may or may not read what it streams.
Hook ==
    /\ sub = "none"
    /\ issued < MaxChallenges
    /\ sub' = "open"
    /\ proven' = FALSE
    /\ issued' = issued + 1
    /\ chal' = issued + 1
    /\ answered' = 0
    /\ reading' \in (IF Outside THEN BOOLEAN ELSE {TRUE})
    /\ lateC' = FALSE
    /\ delivered' = {}
    /\ acked' = {}
    /\ overdue' = {}
    /\ rec' = [state |-> "hooked", proven |-> FALSE]
    /\ UNCHANGED <<seen, ends, told, cmdBad>>

\* The session reads the challenge off its stream (the Monitor's event).
Read ==
    /\ sub = "open"
    /\ reading
    /\ chal # 0
    /\ chal \notin seen
    /\ seen' = seen \cup {chal}
    /\ UNCHANGED <<sub, proven, chal, issued, answered, reading, lateC,
                   delivered, acked, overdue, rec, ends, told, cmdBad>>

\* "nostream": the challenge is somewhere besides the stream; the session that
\* knows it need not have read anything.
Knows(v) == IF "nostream" \in Broken THEN v \in 1..issued ELSE v \in seen

\* hook --prove <v>, over the subscription: the server accepts the outstanding
\* challenge and nothing else.
Prove(v) ==
    /\ Knows(v)
    /\ sub = "open"
    /\ v = chal
    /\ chal # 0
    /\ proven' = TRUE
    /\ answered' = v
    /\ chal' = 0
    /\ lateC' = FALSE
    /\ rec' = [rec EXCEPT !.proven = TRUE]
    /\ UNCHANGED <<sub, issued, seen, reading, delivered, acked, overdue,
                   ends, told, cmdBad>>

\* "nosub": a pong with no subscription (the old seat pong against a record).
HandPong ==
    /\ "nosub" \in Broken
    /\ sub = "none"
    /\ issued > 0
    /\ proven' = TRUE
    /\ answered' = issued
    /\ rec' = [rec EXCEPT !.proven = TRUE]
    /\ UNCHANGED <<sub, chal, issued, seen, reading, lateC, delivered, acked,
                   overdue, ends, told, cmdBad>>

\* The cadence: a proven hook is challenged again; it stays proven while the
\* new challenge is within its bound.
Rechallenge ==
    /\ sub = "open"
    /\ proven
    /\ chal = 0
    /\ issued < MaxChallenges
    /\ issued' = issued + 1
    /\ chal' = issued + 1
    /\ UNCHANGED <<sub, proven, answered, seen, reading, lateC, delivered,
                   acked, overdue, rec, ends, told, cmdBad>>

\* The outstanding challenge passes its bound: only while the session does not
\* read ("strictcadence": whether or not it reads).
Late ==
    /\ sub = "open"
    /\ chal # 0
    /\ ~lateC
    /\ ("strictcadence" \in Broken \/ ~reading)
    /\ lateC' = TRUE
    /\ UNCHANGED <<sub, proven, chal, issued, answered, seen, reading,
                   delivered, acked, overdue, rec, ends, told, cmdBad>>

\* A missed challenge unhooks: the record says gone and the owner is told
\* ("nomiss": the hook stays).
Miss ==
    /\ sub = "open"
    /\ lateC
    /\ "nomiss" \notin Broken
    /\ sub' = "none"
    /\ proven' = FALSE
    /\ chal' = 0
    /\ lateC' = FALSE
    /\ rec' = [state |-> "gone", proven |-> FALSE]
    /\ ends' = ends + 1
    /\ told' = told + 1
    /\ UNCHANGED <<issued, answered, seen, reading, delivered, acked, overdue,
                   cmdBad>>

\* A judgment or push is delivered over the subscription, with its id.
Deliver(i) ==
    /\ sub = "open"
    /\ i \notin delivered
    /\ delivered' = delivered \cup {i}
    /\ UNCHANGED <<sub, proven, chal, issued, answered, seen, reading, lateC,
                   acked, overdue, rec, ends, told, cmdBad>>

\* hook --ack <id>, over the subscription, by a session that read it.
Ack(i) ==
    /\ sub = "open"
    /\ reading
    /\ i \in delivered \ acked
    /\ acked' = acked \cup {i}
    /\ UNCHANGED <<sub, proven, chal, issued, answered, seen, reading, lateC,
                   delivered, overdue, rec, ends, told, cmdBad>>

\* A delivered push passes its ack bound.
Overdue(i) ==
    /\ i \in delivered \ (acked \cup overdue)
    /\ overdue' = overdue \cup {i}
    /\ UNCHANGED <<sub, proven, chal, issued, answered, seen, reading, lateC,
                   delivered, acked, rec, ends, told, cmdBad>>

\* A coordinator command: it runs only through the gate; the ghost marks a run
\* against the rule.
Cmd ==
    /\ Gate
    /\ cmdBad' = (cmdBad \/ ~Rule)
    /\ UNCHANGED <<sub, proven, chal, issued, answered, seen, reading, lateC,
                   delivered, acked, overdue, rec, ends, told>>

\* The outside: the session stops reading (its Monitor died, or it does not
\* read its events) while the hook process stays connected.
StopReading ==
    /\ Outside
    /\ reading
    /\ reading' = FALSE
    /\ UNCHANGED <<sub, proven, chal, issued, answered, seen, lateC, delivered,
                   acked, overdue, rec, ends, told, cmdBad>>

\* The connection drops: recorded gone at once and told ("silentdrop": neither).
Drop ==
    /\ Outside
    /\ sub = "open"
    /\ sub' = "none"
    /\ proven' = FALSE
    /\ chal' = 0
    /\ lateC' = FALSE
    /\ rec' = IF "silentdrop" \in Broken THEN rec
              ELSE [state |-> "gone", proven |-> FALSE]
    /\ ends' = ends + 1
    /\ told' = IF "silentdrop" \in Broken THEN told ELSE told + 1
    /\ UNCHANGED <<issued, answered, seen, reading, delivered, acked, overdue,
                   cmdBad>>

\* nova-sprint unhook: recorded away and told.
Unhook ==
    /\ Outside
    /\ sub = "open"
    /\ sub' = "none"
    /\ proven' = FALSE
    /\ chal' = 0
    /\ lateC' = FALSE
    /\ rec' = [state |-> "away", proven |-> FALSE]
    /\ ends' = ends + 1
    /\ told' = told + 1
    /\ UNCHANGED <<issued, answered, seen, reading, delivered, acked, overdue,
                   cmdBad>>

Next ==
    \/ Hook \/ Read \/ \E v \in Challenges : Prove(v) \/ HandPong
    \/ Rechallenge \/ Late \/ Miss
    \/ \E i \in Items : Deliver(i) \/ Ack(i) \/ Overdue(i)
    \/ Cmd \/ StopReading \/ Drop \/ Unhook

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
