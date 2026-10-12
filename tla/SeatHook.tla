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
\* same connection (`hook --prove`, relayed by the hook process). The challenge is
\* asked once a connection; a challenge unanswered past its bound unhooks. Proven,
\* the hook stays live while acts reach the server (Part "cadence" below).
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
\*   live       the server's record's Live, the gate reads it: the last act plus
\*              HookSilentBound (sprint.HookActed; the proof is the first act)
\*   cad        the cadence's own state (Part "cadence"): the last act, the
\*              proof's time, and the lapse judgments of this subscription
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
\* the look takes its turn, so TLC chooses when an act arrives and when a lapse
\* is judged (the look's own 2 s period and the serial line's delay are folded
\* into that choice) and how late a look has run. A challenge passes its
\* bound only while the session does not read (an answering session answers
\* within it), and a delivered push passes its ack bound at any time.
\*
\* Part "cadence" (the seat, 2026-10-11/12: the hook dropped about 15 minutes after
\* each proof though the stream stayed connected and the seat kept acting; the old
\* cadence re-challenged every HookEvery and moved Live only by a challenge). Now:
\* no challenge after the first on a connection (NoRechallenge); every act (a
\* coordinator verb, an acknowledgement or answer, and the hook client's keepalive,
\* which proves only that the transport is alive, never that the seat attends)
\* moves Live to the act plus HookSilentBound (Act); a proven hook with no act past
\* HookSilentBound lapses, once, with one judgment, gone and the owner told (Lapse;
\* SingleLapse); and a seat whose keepalive arrives every KeepaliveEvery (less than
\* HookSilentBound) never lapses (HookStays, Outside = FALSE). Two code bounds are
\* abstracted: the server writes Live only when an act moves it on by 30 s or more
\* (renewLive), so the recorded Live trails the last act's by at most 30 s and one
\* look; and a lapse whose judgment write fails ends nothing and is tried again at
\* the next look (lapse), so the judgment is always written before the end. Replay
\* and the durable acknowledgement are DurableSpec's, unchanged.
\*
\* What it leaves out: the record is written by the same server step that
\* changes the subscription, under the server's line of control, as a forwarded
\* coordinator verb runs; a verb run on the store directly (--redis) reads the
\* record between two such steps, and a server that died leaves its record
\* hooked until the next server's start rewrites it gone (ServerStart) or its
\* Live bound lapses (HookLive). The challenge specification hooks only when none is open and
\* acknowledges one item at a time. DurableSpec below separately models
\* replacement, cumulative ACK, write failure, reconnect and durable identity.
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
\*   "strictcadence" the silent bound runs from the proof, not the last act: an
\*                   acting seat loses its hook (HookStays; the 2026-10-12 fault)
\*   "rechallenge"   the old cadence: a proven hook is challenged again on an open
\*                   connection (NoRechallenge)
\*   "lapsetwice"    a lapse that does not end the subscription: the look lapses
\*                   it again, a second judgment (SingleLapse)
\*   "nostartrewrite" the next server's start leaves a dead server's record
\*                   hooked: rec.state = "hooked" with no subscription, so no
\*                   rewrite and the owner not told (RecordTrue)
EXTENDS Naturals, FiniteSets

CONSTANTS Items, MaxChallenges, MaxTime, HookSilentBound, KeepaliveEvery, HookAnswerBound, Broken, Outside

VARIABLES d, sub, proven, chal, chalAt, issued, answered, seen, reading, now, live,
          delivered, acked, overdue, rec, ends, told, cmdBad, cad

vars == <<sub, proven, chal, chalAt, issued, answered, seen, reading, now, live,
          delivered, acked, overdue, rec, ends, told, cmdBad, cad>>

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
    /\ live \in 0..(MaxTime + HookSilentBound)
    /\ delivered \subseteq Items
    /\ acked \subseteq delivered
    /\ overdue \subseteq delivered
    /\ rec \in [state : States, proven : BOOLEAN]
    /\ ends \in Nat
    /\ told \in Nat
    /\ cmdBad \in BOOLEAN
    /\ cad \in [acted : 0..MaxTime, provedAt : 0..MaxTime, lapses : 0..2]

LegacyInit ==
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
    /\ cad = [acted |-> 0, provedAt |-> 0, lapses |-> 0]

\* The pushes delivered and neither acknowledged nor within their bound.
Blocking == overdue \ acked

\* The time the silent bound runs from: the last act ("strictcadence": the proof).
SilentFrom == IF "strictcadence" \in Broken THEN cad.provedAt ELSE cad.acted

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
    /\ cad' = [acted |-> now, provedAt |-> now, lapses |-> 0]
    /\ UNCHANGED <<now, seen, ends, told, cmdBad>>

\* The session reads the challenge off its stream (the Monitor's event).
Read ==
    /\ sub = "open"
    /\ reading
    /\ chal # 0
    /\ chal \notin seen
    /\ seen' = seen \cup {chal}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, reading, now, live,
                   delivered, acked, overdue, rec, ends, told, cmdBad, cad>>

\* "nostream": the challenge is somewhere besides the stream; the session that
\* knows it need not have read anything.
Knows(v) == IF "nostream" \in Broken THEN v \in 1..issued ELSE v \in seen

\* hook --prove <v>, over the subscription: the server accepts the outstanding
\* challenge and nothing else. The proof is the first act: Live is its time plus
\* HookSilentBound (HookProven).
Prove(v) ==
    /\ Knows(v)
    /\ sub = "open"
    /\ v = chal
    /\ chal # 0
    /\ proven' = TRUE
    /\ answered' = v
    /\ chal' = 0
    /\ chalAt' = 0
    /\ live' = now + HookSilentBound
    /\ rec' = [rec EXCEPT !.proven = TRUE]
    /\ cad' = [cad EXCEPT !.acted = now, !.provedAt = now]
    /\ UNCHANGED <<sub, issued, seen, reading, now, delivered, acked, overdue,
                   ends, told, cmdBad>>

\* "nosub": a pong with no subscription (the old seat pong against a record).
HandPong ==
    /\ "nosub" \in Broken
    /\ sub = "none"
    /\ issued > 0
    /\ proven' = TRUE
    /\ answered' = issued
    /\ rec' = [rec EXCEPT !.proven = TRUE]
    /\ UNCHANGED <<sub, chal, chalAt, issued, seen, reading, now, live, delivered,
                   acked, overdue, ends, told, cmdBad, cad>>

\* "rechallenge": the old cadence, a proven hook challenged again on a connection
\* that stays up. The cadence asks none (NoRechallenge).
Rechallenge ==
    /\ "rechallenge" \in Broken
    /\ sub = "open"
    /\ proven
    /\ chal = 0
    /\ issued < MaxChallenges
    /\ issued' = issued + 1
    /\ chal' = issued + 1
    /\ chalAt' = now
    /\ UNCHANGED <<sub, proven, answered, seen, reading, now, live, delivered,
                   acked, overdue, rec, ends, told, cmdBad, cad>>

\* An act reaches the server over a proven, open hook: a coordinator verb, an
\* acknowledgement or answer, or the hook client's keepalive (the transport alive,
\* no more). Live moves to the act plus HookSilentBound (renewLive, HookActed; the
\* code writes it once the act moves it 30 s or more, a bound abstracted here).
Act ==
    /\ sub = "open"
    /\ proven
    /\ cad.acted < now
    /\ cad' = [cad EXCEPT !.acted = now]
    /\ live' = now + HookSilentBound
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading, now,
                   delivered, acked, overdue, rec, ends, told, cmdBad>>

\* No act past HookSilentBound: the hook lapses, once, with one judgment (written
\* first), gone, the owner told ("lapsetwice": the subscription stays, so the look
\* lapses it again).
Lapse ==
    /\ sub = "open"
    /\ proven
    /\ now > SilentFrom + HookSilentBound
    /\ cad.lapses < 2
    /\ cad' = [cad EXCEPT !.lapses = @ + 1]
    /\ IF "lapsetwice" \in Broken
       THEN UNCHANGED <<sub, proven, chal, chalAt, live, rec, ends, told>>
       ELSE /\ sub' = "none"
            /\ proven' = FALSE
            /\ chal' = 0
            /\ chalAt' = 0
            /\ live' = 0
            /\ rec' = [state |-> "gone", proven |-> FALSE]
            /\ ends' = ends + 1
            /\ told' = told + 1
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad>>

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
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad, cad>>

\* A judgment or push is delivered over the subscription, with its id.
Deliver(i) ==
    /\ sub = "open"
    /\ i \notin delivered
    /\ delivered' = delivered \cup {i}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, live, acked, overdue, rec, ends, told, cmdBad, cad>>

\* hook --ack <id>, over the subscription, by a session that read it.
Ack(i) ==
    /\ sub = "open"
    /\ reading
    /\ i \in delivered \ acked
    /\ acked' = acked \cup {i}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, live, delivered, overdue, rec, ends, told, cmdBad, cad>>

\* A delivered push passes its ack bound.
Overdue(i) ==
    /\ i \in delivered \ (acked \cup overdue)
    /\ overdue' = overdue \cup {i}
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, live, delivered, acked, rec, ends, told, cmdBad, cad>>

\* A coordinator command: it runs only through the gate; the ghost marks a run
\* against the rule.
Cmd ==
    /\ Gate
    /\ cmdBad' = (cmdBad \/ ~Rule)
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading,
                   now, live, delivered, acked, overdue, rec, ends, told, cad>>

\* The outside: the session stops reading (its Monitor died, or it does not
\* read its events) while the hook process stays connected.
StopReading ==
    /\ Outside
    /\ reading
    /\ reading' = FALSE
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, now, live,
                   delivered, acked, overdue, rec, ends, told, cmdBad, cad>>

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
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad, cad>>

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
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad, cad>>

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
    /\ UNCHANGED <<issued, answered, seen, reading, now, delivered, acked, overdue,
                   cmdBad, cad>>

\* The look's clock: one step a turn of the look, so TLC chooses when the
\* re-challenge goes and how late a look has run.
Tick ==
    /\ now < MaxTime
    \* Outside = FALSE: the hook client's keepalive reaches the server at least every
    \* KeepaliveEvery while the hook is open and proven (Act takes it)
    /\ (Outside \/ sub # "open" \/ ~proven \/ now + 1 - cad.acted <= KeepaliveEvery)
    /\ now' = now + 1
    /\ UNCHANGED <<sub, proven, chal, chalAt, issued, answered, seen, reading, live,
                   delivered, acked, overdue, rec, ends, told, cmdBad, cad>>

LegacyNext ==
    \/ Hook \/ Read \/ \E v \in Challenges : Prove(v) \/ HandPong
    \/ Rechallenge \/ Act \/ Lapse \/ Miss
    \/ \E i \in Items : Deliver(i) \/ Ack(i) \/ Overdue(i)
    \/ Cmd \/ StopReading \/ Drop \/ Unhook \/ ServerStart \/ Tick


\* Durable delivery (PR115). An independent specification below holds the
\* challenge variables fixed; their safety/fairness remains checked by Spec.
\* Items are NOTE-GENERATION keys, not UI group IDs: two keys can be siblings
\* of one group or successive review/computed episodes. A push here carries
\* one key; coalescing several keys into one frame and clock arithmetic are
\* abstracted out. Eligibility is snapshotted at record time, as in look().
\* ACKs persist by key within the current seat/epoch scope. ScopeChange models
\* either an epoch advance or a different holder; sequence IDs never reset.
\* The pending list is session-local, reconstructed from the current inbox on
\* reconnect; no durable outbox is invented. Durable ACK and seq are the record.
\* Three delivery phases (initial plus two outside events), two scopes, at most
\* Cardinality(Items) deliveries per phase. A key closes or becomes due at most
\* once per scope. Outside events are bounded, hence eventually stop; WF of
\* connect, successful record, successful send and Due is an EXPLICIT recovery
\* assumption, not a promise during endless churn/store failure/held time.
DLimit == 3 * Cardinality(Items)
DZero == [i \in Items |-> 0]
DurableInit ==
    d = [connected |-> FALSE, session |-> 0, scope |-> 1, outside |-> 0,
         relevant |-> Items, held |-> Items, acked |-> {}, committed |-> {},
         seq |-> 0, maxSeq |-> 0, oldMax |-> 1, pending |-> DZero, sent |-> {},
         queued |-> {}, seen |-> DZero,
         badHeld |-> FALSE, badReplay |-> FALSE, badStale |-> FALSE]
DTypeOK ==
    /\ d.connected \in BOOLEAN
    /\ d.session \in 0..3
    /\ d.scope \in 1..2
    /\ d.outside \in 0..2
    /\ d.relevant \subseteq Items
    /\ d.held \subseteq d.relevant
    /\ d.acked \subseteq Items
    /\ d.committed \subseteq Items
    /\ d.seq \in 0..DLimit
    /\ d.maxSeq \in 0..DLimit
    /\ d.oldMax \in 0..DLimit
    /\ d.pending \in [Items -> 0..DLimit]
    /\ d.sent \subseteq Items
    /\ d.queued \subseteq Items
    /\ d.seen \in [Items -> 0..3]
    /\ \A b \in {d.badHeld, d.badReplay, d.badStale} : b \in BOOLEAN
DEligible == d.relevant \ d.held
DConnect ==
    /\ ~d.connected
    /\ d.session < 3
    /\ d' = [d EXCEPT !.connected = TRUE, !.session = @ + 1,
              !.oldMax = IF d.seq > d.oldMax THEN d.seq ELSE d.oldMax,
              !.seq = IF "resetseq" \in Broken THEN 0 ELSE @,
              !.pending = DZero, !.queued = {},
              !.sent = IF "noreplay" \in Broken THEN @ ELSE {},
              !.acked = IF "loseacks" \in Broken THEN {} ELSE @]
\* Drop includes server restart and failed frame write. These preserve durable
\* ACK keys and high-water, but discard session bookkeeping, just as start does.
DDrop ==
    /\ d.connected /\ d.outside < 2
    /\ d' = [d EXCEPT !.connected = FALSE, !.outside = @ + 1,
              !.queued = {}, !.pending = DZero]
\* Successful replacement persists the new record before retiring old stream.
DReplace ==
    /\ d.connected /\ d.outside < 2 /\ d.session < 3
    /\ d' = [d EXCEPT !.session = @ + 1, !.outside = @ + 1,
              !.oldMax = IF d.seq > d.oldMax THEN d.seq ELSE d.oldMax,
              !.seq = IF "resetseq" \in Broken THEN 0 ELSE @,
              !.pending = DZero, !.queued = {},
              !.sent = IF "noreplay" \in Broken THEN @ ELSE {},
              !.acked = IF "loseacks" \in Broken THEN {} ELSE @]
DReplaceFails ==
    /\ d.connected
    /\ d' = IF "retirefirst" \in Broken
            THEN [d EXCEPT !.connected = FALSE]
            ELSE d
DScopeChange ==
    /\ d.scope = 1 /\ d.outside < 2
    /\ d' = [d EXCEPT !.scope = 2, !.outside = @ + 1,
              !.relevant = Items, !.held = Items,
              !.acked = IF "keepepochacks" \in Broken THEN @ ELSE {},
              !.committed = {}, !.sent = {}, !.queued = {}, !.seen = DZero,
              !.pending = IF "keepepochpending" \in Broken THEN @ ELSE DZero]
DDue(i) ==
    /\ i \in d.held
    /\ d' = [d EXCEPT !.held = @ \ {i}]
DClose(i) ==
    /\ i \in d.relevant
    /\ d' = [d EXCEPT !.relevant = @ \ {i}, !.held = @ \ {i}]
DCanRecord(i) ==
    /\ d.connected /\ i \in d.relevant
    /\ (i \notin d.held \/ "sendheld" \in Broken)
    /\ (i \notin d.acked \/ "replayacked" \in Broken)
    /\ i \notin d.sent /\ d.seq < DLimit
DRecord(i) ==
    /\ DCanRecord(i)
    /\ d' = [d EXCEPT !.seq = @ + 1, !.maxSeq = @ + 1, !.pending[i] = d.seq + 1,
              !.sent = @ \cup {i}, !.queued = @ \cup {i},
              !.badHeld = @ \/ i \in d.held,
              !.badReplay = @ \/ i \in d.committed]
\* A refused look must restore seq/open/sent/acked/epoch and emit no frame.
DRecordFails(i) ==
    /\ DCanRecord(i)
    /\ d' = IF "sendunrecorded" \in Broken
            THEN [d EXCEPT !.seen[i] = d.session]
            ELSE IF "norollback" \in Broken
                 THEN [d EXCEPT !.sent = @ \cup {i}]
                 ELSE d
\* Successful transport write, not proof that the client consumed the frame.
DSend(i) ==
    /\ d.connected /\ i \in d.queued
    /\ d' = [d EXCEPT !.queued = @ \ {i}, !.seen[i] = d.session]
DAckKeys(n) == {i \in Items : d.pending[i] # 0 /\ d.pending[i] <= n}
\* Token 0 is missing/legacy; oldMax starts at 1 while durable seq starts at
\* 0, representing a pre-upgrade record with an unknown old wire-ID high-water.
\* Thus monotone new IDs alone cannot reject all pre-upgrade stale numeric ACKs.
\* Session token must match before cumulative numeric ACK is considered.
DAck(t, n) ==
    /\ d.connected /\ n \in 1..DLimit /\ t \in 0..d.session
    /\ t = d.session \/ n <= d.oldMax
    /\ d' = IF n > d.seq \/ (t # d.session /\ "nosession" \notin Broken) THEN d ELSE
        [d EXCEPT !.acked = @ \cup DAckKeys(n),
           !.committed = @ \cup DAckKeys(n),
           !.pending = [i \in Items |-> IF i \in DAckKeys(n) THEN 0 ELSE d.pending[i]],
           !.badStale = @ \/ (t # d.session /\ DAckKeys(n) # {})]
DAckFails(t, n) ==
    /\ t = d.session
    /\ d.connected /\ n \in 1..d.seq /\ DAckKeys(n) # {}
    /\ d' = IF "acknorollback" \in Broken
            THEN [d EXCEPT !.pending = DZero]
            ELSE d
DNext ==
    /\ \/ DConnect \/ DDrop \/ DReplace \/ DReplaceFails \/ DScopeChange
       \/ \E i \in Items : DDue(i) \/ DClose(i) \/ DRecord(i) \/ DRecordFails(i) \/ DSend(i)
       \/ \E t \in 0..d.session, n \in 1..DLimit : DAck(t, n) \/ DAckFails(t, n)
    /\ UNCHANGED vars
DurableSpec ==
    /\ LegacyInit /\ DurableInit
    /\ [][DNext]_<<vars, d>>
    /\ WF_d(DConnect)
    /\ \A i \in Items : WF_d(DRecord(i)) /\ WF_d(DSend(i))
                         /\ WF_d(DDue(i))
DurableAckHistory == d.acked = d.committed
PersistBeforeSend ==
    \A i \in Items : (d.connected /\ d.seen[i] = d.session) => i \in d.sent
HeldExcluded == ~d.badHeld
NoAckedReplay == ~d.badReplay
StaleAckSafe == ~d.badStale
SequenceHighWater == d.seq = d.maxSeq
\* Inspect actual before/after state, independently of each failure action's
\* implementation. No mutant-controlled verdict flag participates in these
\* action properties: even an unflagged mutation must violate equality.
WriteFailureRollback ==
    [][((\E i \in Items : DRecordFails(i))
        \/ (\E t \in 0..d.session, n \in 1..DLimit : DAckFails(t, n)))
       => UNCHANGED d]_d
ReplacementFailureKeepsOld ==
    [][DReplaceFails => UNCHANGED d]_d
EpochClearsPending == (d.scope = 2 /\ d.sent = {}) => d.pending = DZero
\* Each obligation either becomes irrelevant (closed/scope replaced), commits
\* an ACK, or is sent on the CURRENT session. Re-evaluated after reconnect,
\* so a frame sent only on a prior session cannot discharge the new obligation.
EligibleUnackedDelivered ==
    \A e \in 1..2, i \in Items :
      (d.scope = e /\ i \in DEligible \ d.acked)
      ~> (d.scope # e \/ i \notin d.relevant \/ i \in d.acked
          \/ (d.connected /\ d.seen[i] = d.session))
HeldEventuallyDue ==
    \A e \in 1..2, i \in Items :
      (d.scope = e /\ i \in d.held)
      ~> (d.scope # e \/ i \notin d.relevant \/ i \notin d.held)
EventuallyConnected == <>[]d.connected

Spec == LegacyInit /\ DurableInit /\ [][LegacyNext /\ UNCHANGED d]_<<vars, d>>
        /\ WF_vars(Read) /\ WF_vars(\E v \in Challenges : Prove(v))
        /\ WF_vars(Act) /\ WF_vars(Lapse) /\ WF_vars(Miss) /\ WF_vars(Hook)

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

\* Part "cadence": no challenge is asked again on a proven, open hook.
NoRechallenge == proven => chal = 0

\* Part "cadence": a subscription lapses at most once, with one judgment.
SingleLapse == cad.lapses <= 1
=============================================================================
