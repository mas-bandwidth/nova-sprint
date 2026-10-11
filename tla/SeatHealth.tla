---------------------------- MODULE SeatHealth ----------------------------
\* nova-sprint's seat generation and a friend's health (docs/SPEC-SPRINT.md
\* section 1, "A friend's health"; internal/sprint/seat.go MoveSeat,
\* friend_health.go NotHealth, ObserveFriend, ClearFriendHealth,
\* presence.go FriendStatus, FriendEvidence, friend_stall.go TickFriendStall,
\* store/friends.go). The sprint server is the authority and the table: the
\* seat (holder and generation) moves by handover, the coordinator's daemon
\* observes one friend and writes what it saw, fenced by the seat it read, and
\* the table's word is derived at every read: up, held or down. The keepalive
\* and its nonces are the daemon's (Friend.tla).
\*
\* The state the server owns: holder, gen (the seat), hstate, hseen, hgen (the
\* friend's health record: the word, the proof's time, the generation it was
\* observed under; hstate = "none" until the first), held (friend down), beat
\* (her last raw beat), proof (her session's last proof as a beat carried it:
\* friend beat --pong, Beat.Proof), finish (when a card of hers last finished).
\* The outside: the daemons' observations in flight (inflight: each carries the
\* generation its daemon read the seat at and the proof's time, delivered later
\* in any order), handovers, her beats and proofs, finishes, the hold, the
\* clock. The health row can be cleared (Clear), and the stall ladder's rung 5
\* writes a down word with Seen = now outside NotHealth (StallWrite).
\*
\* A friend's status (FriendStatus, FriendEvidence) is: held on the
\* coordinator's hold alone; else up on an up word under the current seat fresh
\* within PongWindow, her session's proof on a beat fresh within ProofWindow
\* while the beat is fresh within BeatWindow, or a card of hers finished fresh
\* within FinishWindow; else down. Her raw beat is never evidence for up.
\*
\* Broken = "none" is the design. Every other value is a reversed witness,
\* each caught by one property below:
\*   "nofence"        any generation writes: AcceptedAtTheSeat
\*   "olderproof"     an older proof overwrites: ProofNeverGoesBack
\*   "beatwins"       her raw beat makes an observed friend up:
\*                    ObservedNeverUpByBeat
\*   "noexpiry"       a proof never ages: UpOnlyOnFreshProof
\*   "samegen"        a handover keeps the generation: GenerationStepsWithTheSeat
\*                    (at the first handover; past it OldSeatNeverUp breaks too:
\*                    A->B->A, A's first seat's proof shows up under A's second)
\*   "future"         a proof dated after the server's clock writes:
\*                    ProofNeverAhead (a review of PR 5305: it held her up past
\*                    its time)
\*   "clearkeepsrow"  clear deletes the health but keeps the row, then an older
\*                    proof writes: ProofNeverGoesBack

EXTENDS Naturals, FiniteSets

CONSTANTS Holders, MaxTime, MaxObs,
          PongWindow, ProofWindow, FinishWindow, BeatWindow, Broken

VARIABLES now, holder, gen, moves, hstate, hseen, hgen, hmoves, held, beat,
          proof, finish, inflight, sent, accepted
vars == <<now, holder, gen, moves, hstate, hseen, hgen, hmoves, held, beat,
          proof, finish, inflight, sent, accepted>>

Never == 0 \* a time before the clock: no beat, no proof, no finish

\* An observation: the word, the proof's time, the seat generation its daemon
\* read, and (a ghost) the handover count when it was sent.
Obs == [state: {"up", "asleep", "down"}, seen: 1..MaxTime, gen: 1..(MaxObs + 1), moves: 0..MaxObs]

TypeOK ==
  /\ now \in 0..MaxTime
  /\ holder \in Holders
  /\ gen \in 1..(MaxObs + 1)
  /\ moves \in 0..MaxObs
  /\ hstate \in {"none", "up", "asleep", "down"}
  /\ hseen \in 0..MaxTime
  /\ hgen \in 0..(MaxObs + 1)
  /\ hmoves \in 0..MaxObs
  /\ held \in BOOLEAN
  /\ beat \in 0..MaxTime
  /\ proof \in 0..MaxTime
  /\ finish \in 0..MaxTime
  /\ inflight \subseteq Obs
  /\ sent \in 0..MaxObs
  /\ accepted \in 0..MaxObs

Observed == hstate # "none"

\* The friends' rule (FriendStatus, ObservedStatus, FriendEvidence): held wins;
\* else up only on her session's evidence within its window, else down. Her
\* raw beat is never evidence (a daemon or a loop beating for her proves an app
\* is open, not that her session can work); only the session's proof it carries
\* is. The witnesses let the raw beat decide an observed friend ("beatwins") or
\* let a pong never age ("noexpiry").
BeatFresh == beat # Never /\ now - beat < BeatWindow
PongFresh == hstate = "up" /\ hgen = gen /\ hseen <= now /\ now - hseen < PongWindow
ProofFresh == proof # Never /\ proof <= now /\ now - proof < ProofWindow /\ BeatFresh
FinishFresh == finish # Never /\ finish <= now /\ now - finish < FinishWindow

\* The pong word the observation gives: up only under the current seat, fresh.
\* "noexpiry" lets it never age (the reversed witness).
ObservedWord == IF hstate = "up" /\ hgen = gen /\ (Broken = "noexpiry" \/ (hseen <= now /\ now - hseen < PongWindow)) THEN "up" ELSE "down"

\* "beatwins" lets her raw beat decide an observed friend (the reversed witness).
UpByRawBeat == Broken = "beatwins" /\ BeatFresh /\ Observed

Status ==
  IF held THEN "held"
  ELSE IF UpByRawBeat THEN "up"
  ELSE IF ObservedWord = "up" THEN "up"
  ELSE IF ProofFresh THEN "up"
  ELSE IF FinishFresh THEN "up"
  ELSE "down"

Init ==
  /\ now = 0
  /\ holder \in Holders /\ gen = 1 /\ moves = 0
  /\ hstate = "none" /\ hseen = Never /\ hgen = 0 /\ hmoves = 0
  /\ held = FALSE /\ beat = Never /\ proof = Never /\ finish = Never
  /\ inflight = {} /\ sent = 0 /\ accepted = 0

Tick ==
  /\ now < MaxTime
  /\ now' = now + 1
  /\ UNCHANGED <<holder, gen, moves, hstate, hseen, hgen, hmoves, held, beat, proof, finish, inflight, sent, accepted>>

\* The seat moves to another holder (MoveSeat): the next generation, every
\* time. The witness keeps the generation.
Handover(h) ==
  /\ h # holder
  /\ moves < MaxObs
  /\ holder' = h
  /\ gen' = IF Broken = "samegen" THEN gen ELSE gen + 1
  /\ moves' = moves + 1
  /\ UNCHANGED <<now, hstate, hseen, hgen, hmoves, held, beat, proof, finish, inflight, sent, accepted>>

\* A daemon sends an observation of a proof seen at any time, after now too
\* (a clock ahead of the server's, or a --seen written wrong), under the
\* seat as it reads it now (holder and generation).
Send(st) ==
  /\ sent < MaxObs
  /\ now >= 1
  /\ \E t \in 1..MaxTime :
       inflight' = inflight \cup {[state |-> st, seen |-> t, gen |-> gen, moves |-> moves]}
  /\ sent' = sent + 1
  /\ UNCHANGED <<now, holder, gen, moves, hstate, hseen, hgen, hmoves, held, beat, proof, finish, accepted>>

\* An observation arrives at the server (ObserveFriend under the fence):
\* accepted only at the seat's generation now, with a proof dated no later
\* than the server's clock and newer than the row's; refused otherwise,
\* nothing written. The witnesses take any generation, any older proof, or
\* a proof from the future.
Fenced(o) == o.gen = gen \/ Broken = "nofence"
Newer(o) == o.seen > hseen \/ Broken = "olderproof"
NotAhead(o) == o.seen <= now \/ Broken = "future"
Deliver(o) ==
  /\ o \in inflight
  /\ inflight' = inflight \ {o}
  /\ IF Fenced(o) /\ NotAhead(o) /\ Newer(o)
       THEN /\ hstate' = o.state /\ hseen' = o.seen /\ hgen' = o.gen /\ hmoves' = o.moves
            /\ accepted' = accepted + 1
       ELSE UNCHANGED <<hstate, hseen, hgen, hmoves, accepted>>
  /\ UNCHANGED <<now, holder, gen, moves, held, beat, proof, finish, sent>>

\* Her own beat (friend beat): never evidence for up, whoever sends it. It
\* proves a daemon, not a session.
Beat ==
  /\ now >= 1
  /\ beat' = now
  /\ UNCHANGED <<now, holder, gen, moves, hstate, hseen, hgen, hmoves, held, proof, finish, inflight, sent, accepted>>

\* A beat that carried her session's proof (friend beat --pong, Beat.Proof):
\* the proof is evidence only while her beat is fresh.
Prove ==
  /\ now >= 1
  /\ beat' = now /\ proof' = now
  /\ UNCHANGED <<now, holder, gen, moves, hstate, hseen, hgen, hmoves, held, finish, inflight, sent, accepted>>

\* A card of hers finished (working to done): her last finish.
Finish ==
  /\ now >= 1
  /\ finish' = now
  /\ UNCHANGED <<now, holder, gen, moves, hstate, hseen, hgen, hmoves, held, beat, proof, inflight, sent, accepted>>

\* The coordinator's hold and its release.
Hold == held' = TRUE /\ UNCHANGED <<now, holder, gen, moves, hstate, hseen, hgen, hmoves, beat, proof, finish, inflight, sent, accepted>>
Release == held' = FALSE /\ UNCHANGED <<now, holder, gen, moves, hstate, hseen, hgen, hmoves, beat, proof, finish, inflight, sent, accepted>>

\* Her health is deleted (friend health --clear, the stall ladder's release, or
\* her roster row removed): her record is removed, so her status is her
\* session's evidence alone. The witness "clearkeepsrow" deletes the proof but
\* keeps the row, so an older proof is then accepted and the row's proof goes
\* back.
Clear ==
  /\ hstate # "none"
  /\ IF Broken = "clearkeepsrow"
       THEN /\ hseen' = Never /\ UNCHANGED <<hstate, hgen, hmoves>>
       ELSE /\ hstate' = "none" /\ hseen' = Never /\ hgen' = 0 /\ hmoves' = 0
  /\ UNCHANGED <<now, holder, gen, moves, held, beat, proof, finish, inflight, sent, accepted>>

\* Rung 5 of the stall ladder writes health outside NotHealth: down, Seen = now
\* and the current generation. Benign for the properties: a down word, a fresh
\* proof, the current seat.
StallWrite ==
  /\ hstate' = "down" /\ hseen' = now /\ hgen' = gen /\ hmoves' = moves
  /\ UNCHANGED <<now, holder, gen, moves, held, beat, proof, finish, inflight, sent, accepted>>

Next ==
  \/ Tick
  \/ \E h \in Holders : Handover(h)
  \/ \E st \in {"up", "asleep", "down"} : Send(st)
  \/ \E o \in inflight : Deliver(o)
  \/ Beat \/ Prove \/ Finish \/ Hold \/ Release \/ Clear \/ StallWrite

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick)

\* ---------------------------------------------------------------- the rules

\* The table shows up, held or down and nothing else.
ThreeWords == Status \in {"up", "held", "down"}

\* Held is the coordinator's hold alone: no observation and no beat shows it.
HeldIsTheHold == (Status = "held") <=> held

\* A friend is up only on fresh evidence of her session: an up word under the
\* current seat with a proof under PongWindow old, a session proof on a beat
\* under ProofWindow old while the beat is fresh, or a card finished under
\* FinishWindow old.
UpOnlyOnFreshProof == Status = "up" => PongFresh \/ ProofFresh \/ FinishFresh

\* An old seat's up word never looks up under a new seat: what the row holds
\* was accepted after the last handover (A->B->A rejects A's first seat). Her
\* beat proof and a card she finished stand on no seat, so they may still make
\* her up.
OldSeatNeverUp == Observed => (hmoves = moves \/ ObservedWord # "up")

\* An accepted observation names the seat's generation now: the fence.
AcceptedAtTheSeat == [][accepted' > accepted => hgen' = gen']_vars

\* The row's proof is never dated after the server's clock: a proof from the
\* future is refused, so it never holds a friend up past its time.
ProofNeverAhead == hseen <= now

\* The row's proof never goes back while the row exists: a delayed or repeated
\* proof writes nothing, and a proof written after a clear (her row gone) is a
\* fresh row, not a step back.
ProofNeverGoesBack == [][(Observed /\ Observed') => hseen' >= hseen]_vars

\* Her raw beat never makes an observed friend up: the beat proves a daemon,
\* not a session; only the session's proof it carries is evidence.
ObservedNeverUpByBeat == (Observed /\ ~held) => ~UpByRawBeat

\* Every handover takes the next generation, and nothing else moves it.
GenerationStepsWithTheSeat == [][gen' = gen + moves' - moves]_vars

=============================================================================
