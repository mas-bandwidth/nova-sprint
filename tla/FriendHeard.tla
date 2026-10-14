---------------------------- MODULE FriendHeard ----------------------------
\* A friend's presence as one last-heard time (the owner, Glenn, 2026-10-13:
\* "any activity from the friend that proves their session is live = timeout
\* resets ... just like in a network protocol, any valid packet from that client
\* resets the timeout accum"). Written before the code that implements it. The
\* table's older rule (FriendStatus, internal/sprint/presence.go) reads three
\* separate evidences; FriendPresence.tla (nova-tools) is the layer above this
\* one. This module is the state machine Glenn described, and nothing else:
\*
\*   heardAge   ticks since the friend's last valid event, capped at Total
\*   phase      held | up | deaf | asleep | down, derived at every read
\*   Status     the one word the table shows: up, down or held
\*
\* The phases (HeardUp and DeafHold are FriendHeardUp and FriendDeafHold, ten
\* minutes each; Total is their sum):
\*   up      heardAge < HeardUp
\*   deaf    HeardUp <= heardAge < Total: "it's held during the 10 minutes before
\*           they actually go down". Shown up, dealt to, keeps its cards.
\*   asleep  heardAge = Total and her daemon answers (her session does not):
\*           "if you are asleep, you are down". Shown down, same behaviour as down.
\*   down    heardAge = Total and her daemon does not answer; or her beat says down.
\*   held    the coordinator's hold, which wins.
\* Any valid event (Event: a bus message of any kind, a recv or ack, a take, a
\* finish, a push, a PR, a hook proof, a pong) sets heardAge to 0, and is
\* possible only while her session answers: a silent session produces none. A
\* send counts as an event like any other (Glenn's "sending but not receiving"
\* deaf is modelled as: every event, a send included, resets last-heard). The
\* seat's wake (Wake) is a message to her; her answer (Answer) is an event.
\* A beat that says down (her harness at its limit; daemon = "saysdown") wins
\* over last-heard, as it does today. A daemon that merely stops answering
\* ("silent") does not: it only turns asleep into down, a label.
\*
\* Time is ages, not a clock: Tick adds one to every age and stops at Total
\* ("Total or older"), so Tick is always available and the liveness is checked on
\* an infinite clock over finitely many states. `since` is a ghost: the true age
\* of the last event, which a reversed witness may make heardAge disagree with.
\*
\* Delta from the code at v1.2.23 (what the presence code is to implement to):
\*  1. No last-heard. FriendEvidence (presence.go) is up on any of three separate
\*     evidences: a wake ping her session answered (Health.Seen, FriendPongWindow
\*     10m), a session proof on a fresh beat (Beat.Proof, FriendProofLive 15m), a
\*     card finished (Finished, FriendFinishWindow 30m). A bus send, recv or ack,
\*     a push, a PR, or a take is no evidence there. Model: one heardAge, every
\*     valid event resets it (store key friend-heard:<name>, monotone max).
\*  2. No DEAF. Today up goes straight to down when the last evidence ages out.
\*     Model: up for FriendHeardUp, deaf for FriendDeafHold more (shown up, dealt
\*     to, keeps cards), down after. A DEAF entry raises a seat judgment.
\*  3. No ASLEEP. FriendEvidence has two words, up and down, and a health word
\*     "asleep" is only an observation the code stores (Health.State). Model:
\*     asleep is the label of silence with her daemon answering; it displays and
\*     behaves as down.
\*  4. Display. Today up, down or held. Model: the same three words; deaf shows
\*     up, asleep shows down (DisplayIsTwoWords, DeafDisplaysUp, AsleepDisplaysDown).
\*  5. Cards back. Today a down (not held) friend's cards leave her on the hold
\*     (hold.go FriendTake Hold), a stall ladder rung (friend_stall.go) or a
\*     rule; the tick does not return them when her status turns down. Model:
\*     the tick that makes her down or asleep takes back every card she holds in
\*     the same step, never from up or deaf (CardsReturnOnlyFromDownOrAsleep).
\*  6. FriendBeatBound. FriendEvidence returns down when her last beat is older
\*     than FriendBeatBound (30m) whatever else it saw. Model: a silent daemon
\*     does not override last-heard (activity within the bound is never down,
\*     ActivityWithinBoundNeverDown); only a beat that says down (SaysDown) or a
\*     hold does. OPEN: does a stopped daemon with a live session stay up?
\*  7. Take and deal. Today friend_deal.go and friend_read.go deal only to
\*     Status = Up (friend_deal.go:280, :784). Unchanged: deaf is up, asleep is
\*     down, held is never dealt to.
\*
\* Broken = "none" is the design. Every other value is a reversed witness, each
\* caught by one property (its cfg lists TypeOK and that property alone):
\*   "noreset"          an event does not reset heardAge: ActivityWithinBoundNeverDown
\*                      (and DownComesUpOnNextEvent on SpecLive)
\*   "skipdeaf"         up goes straight to down at HeardUp (DeafHold 0): UpToDownPassesDeaf
\*   "earlydown"        down one tick before the bound: DownOnlyAfterFullTimeout
\*   "deafforever"      deaf never expires: DeafNeverLongerThanBound (and DeafLeavesDeaf)
\*   "deafreturnscards" a deaf friend's cards are taken back: CardsReturnOnlyFromDownOrAsleep
\*   "asleepkeepscards" an asleep friend keeps her cards: NoCardOnDownOrAsleep
\*   "wakenoreset"      an answered wake does not reset heardAge: AsleepWokenReachesUp
\*   "extradisplay"     the table shows the phase word: DisplayIsTwoWords
\*   "deafdown"         deaf shows down: DeafDisplaysUp
\*   "asleepup"         asleep shows up: AsleepDisplaysDown
\*   "dealdown"         the deal reaches any friend not held: DealOnlyToShownUp
\* Dealing to a deaf friend is the design, not a witness.

EXTENDS Naturals, FiniteSets

CONSTANTS Friends, Cards, HeardUp, DeafHold, MaxEvents, Broken

ASSUME HeardUp >= 1 /\ DeafHold >= 1 /\ MaxEvents \in Nat

VARIABLES heardAge, since, held, daemon, session, wake, holder, taken, events
vars == <<heardAge, since, held, daemon, session, wake, holder, taken, events>>

Total == HeardUp + DeafHold
Pool == "pool"

TypeOK ==
  /\ heardAge \in [Friends -> 0..Total]
  /\ since \in [Friends -> 0..Total]
  /\ held \in [Friends -> BOOLEAN]
  /\ daemon \in [Friends -> {"answering", "silent", "saysdown"}]
  /\ session \in [Friends -> {"answering", "silent"}]
  /\ wake \in [Friends -> BOOLEAN]
  /\ holder \in [Cards -> Friends \cup {Pool}]
  /\ taken \in [Cards -> BOOLEAN]
  /\ events \in 0..MaxEvents

\* ----------------------------------------------------------- the derived words

\* The phase over any held, daemon and heardAge, so a step can read its own result.
DownAt == IF Broken = "earlydown" THEN Total - 1 ELSE Total
Quiet(f, dm) == IF dm[f] = "answering" THEN "asleep" ELSE "down"
PhaseIn(f, hd, dm, ag) ==
  IF hd[f] THEN "held"
  ELSE IF dm[f] = "saysdown" THEN "down"
  ELSE IF ag[f] < HeardUp THEN "up"
  ELSE IF Broken = "skipdeaf" THEN Quiet(f, dm)
  ELSE IF Broken = "deafforever" \/ ag[f] < DownAt THEN "deaf"
  ELSE Quiet(f, dm)
Phase(f) == PhaseIn(f, held, daemon, heardAge)

\* The one word the table shows.
StatusOf(p) ==
  CASE Broken = "extradisplay"                  -> p
    [] Broken = "deafdown" /\ p = "deaf"        -> "down"
    [] Broken = "asleepup" /\ p = "asleep"      -> "up"
    [] p = "held"                               -> "held"
    [] p \in {"up", "deaf"}                     -> "up"
    [] OTHER                                    -> "down"
Status(f) == StatusOf(Phase(f))

CardsOf(f) == {c \in Cards : holder[c] = f}

\* The phases whose cards go back to the pool in the step that makes them so.
BackPhases ==
  CASE Broken = "deafreturnscards" -> {"held", "down", "asleep", "deaf"}
    [] Broken = "asleepkeepscards" -> {"held", "down"}
    [] OTHER                       -> {"held", "down", "asleep"}
TakeBack(hd, dm, ag) ==
  /\ holder' = [c \in Cards |->
       IF holder[c] \in Friends /\ PhaseIn(holder[c], hd, dm, ag) \in BackPhases
         THEN Pool ELSE holder[c]]
  /\ taken' = [c \in Cards |->
       IF holder[c] \in Friends /\ PhaseIn(holder[c], hd, dm, ag) \in BackPhases
         THEN FALSE ELSE taken[c]]

Init ==
  /\ heardAge = [f \in Friends |-> Total]
  /\ since = [f \in Friends |-> Total]
  /\ held = [f \in Friends |-> FALSE]
  /\ daemon = [f \in Friends |-> "answering"]
  /\ session = [f \in Friends |-> "answering"]
  /\ wake = [f \in Friends |-> FALSE]
  /\ holder = [c \in Cards |-> Pool]
  /\ taken = [c \in Cards |-> FALSE]
  /\ events = 0

Inc(n) == IF n < Total THEN n + 1 ELSE Total

\* ------------------------------------------------------------------- the clock

\* Time passes. The tick that makes a friend down or asleep takes back her cards.
Tick ==
  /\ heardAge' = [f \in Friends |-> Inc(heardAge[f])]
  /\ since' = [f \in Friends |-> Inc(since[f])]
  /\ TakeBack(held, daemon, heardAge')
  /\ UNCHANGED <<held, daemon, session, wake, events>>

\* ---------------------------------------------------------------- the friend

\* A valid event from her: possible only while her session answers.
Event(f) ==
  /\ session[f] = "answering"
  /\ heardAge' = [heardAge EXCEPT ![f] = IF Broken = "noreset" THEN heardAge[f] ELSE 0]
  /\ since' = [since EXCEPT ![f] = 0]
  /\ UNCHANGED <<held, daemon, session, wake, holder, taken, events>>

\* Her session goes silent or takes turns again; her daemon stops, says down, or starts again.
SessionSilence(f) ==
  /\ session[f] = "answering" /\ events < MaxEvents
  /\ session' = [session EXCEPT ![f] = "silent"]
  /\ events' = events + 1
  /\ UNCHANGED <<heardAge, since, held, daemon, wake, holder, taken>>
SessionResume(f) ==
  /\ session[f] = "silent"
  /\ session' = [session EXCEPT ![f] = "answering"]
  /\ UNCHANGED <<heardAge, since, held, daemon, wake, holder, taken, events>>
DaemonStop(f) ==
  /\ daemon[f] = "answering" /\ events < MaxEvents
  /\ daemon' = [daemon EXCEPT ![f] = "silent"]
  /\ events' = events + 1
  /\ UNCHANGED <<heardAge, since, held, session, wake, holder, taken>>
DaemonSaysDown(f) ==
  /\ daemon[f] = "answering" /\ events < MaxEvents
  /\ daemon' = [daemon EXCEPT ![f] = "saysdown"]
  /\ TakeBack(held, daemon', heardAge)
  /\ events' = events + 1
  /\ UNCHANGED <<heardAge, since, held, session, wake>>
DaemonStart(f) ==
  /\ daemon[f] # "answering"
  /\ daemon' = [daemon EXCEPT ![f] = "answering"]
  /\ UNCHANGED <<heardAge, since, held, session, wake, holder, taken, events>>

\* -------------------------------------------------------------- the coordinator

\* The seat wakes a friend it has not heard from for HeardUp; her answer, when her
\* session answers, is an event. Her daemon answering alone is not.
Wake(f) ==
  /\ ~wake[f] /\ ~held[f] /\ heardAge[f] >= HeardUp
  /\ wake' = [wake EXCEPT ![f] = TRUE]
  /\ UNCHANGED <<heardAge, since, held, daemon, session, holder, taken, events>>
Answer(f) ==
  /\ wake[f] /\ session[f] = "answering"
  /\ wake' = [wake EXCEPT ![f] = FALSE]
  /\ heardAge' = [heardAge EXCEPT ![f] = IF Broken = "wakenoreset" THEN heardAge[f] ELSE 0]
  /\ since' = [since EXCEPT ![f] = 0]
  /\ UNCHANGED <<held, daemon, session, holder, taken, events>>

\* The hold takes back her cards in the same step; the release is no evidence.
Hold(f) ==
  /\ ~held[f] /\ events < MaxEvents
  /\ held' = [held EXCEPT ![f] = TRUE]
  /\ TakeBack(held', daemon, heardAge)
  /\ events' = events + 1
  /\ UNCHANGED <<heardAge, since, daemon, session, wake>>
Release(f) ==
  /\ held[f]
  /\ held' = [held EXCEPT ![f] = FALSE]
  /\ UNCHANGED <<heardAge, since, daemon, session, wake, holder, taken, events>>

\* The deal gives a pool card to a friend the table shows up (deaf included).
Deal(c, g) ==
  /\ holder[c] = Pool
  /\ IF Broken = "dealdown" THEN Status(g) # "held" ELSE Status(g) = "up"
  /\ holder' = [holder EXCEPT ![c] = g]
  /\ UNCHANGED <<heardAge, since, held, daemon, session, wake, taken, events>>

\* A card dealt to a friend is taken into working, while the table shows her up.
Take(c) ==
  /\ holder[c] \in Friends /\ ~taken[c] /\ Status(holder[c]) = "up"
  /\ taken' = [taken EXCEPT ![c] = TRUE]
  /\ UNCHANGED <<heardAge, since, held, daemon, session, wake, holder, events>>

Next ==
  \/ Tick
  \/ \E f \in Friends :
       Event(f) \/ SessionSilence(f) \/ SessionResume(f) \/ DaemonStop(f) \/ DaemonSaysDown(f)
       \/ DaemonStart(f) \/ Wake(f) \/ Answer(f) \/ Hold(f) \/ Release(f)
  \/ \E c \in Cards, g \in Friends : Deal(c, g)
  \/ \E c \in Cards : Take(c)

\* The safety specification: no fairness.
Spec == Init /\ [][Next]_vars

\* The clock alone is fair: a friend may stay silent for ever.
SpecClock == Spec /\ WF_vars(Tick)

\* The clock, her answer to a wake, and every recovery (the finite disruptions end).
SpecWake ==
  /\ SpecClock
  /\ \A f \in Friends : WF_vars(Answer(f)) /\ WF_vars(SessionResume(f)) /\ WF_vars(DaemonStart(f)) /\ WF_vars(Release(f))

\* Also: a friend whose session answers keeps producing events.
SpecLive == SpecWake /\ \A f \in Friends : WF_vars(Event(f))

\* ---------------------------------------------------------------- the rules

\* The words in the table are up, down or held, and nothing else.
DisplayIsTwoWords == \A f \in Friends : Status(f) \in {"up", "down", "held"}
DeafDisplaysUp == \A f \in Friends : Phase(f) = "deaf" => Status(f) = "up"
AsleepDisplaysDown == \A f \in Friends : Phase(f) = "asleep" => Status(f) = "down"

\* DOWN is reached only after the full silence bound (or a hold, or a beat that
\* says down): a down or asleep friend has heard nothing for Total ticks.
DownOnlyAfterFullTimeout ==
  \A f \in Friends :
    Phase(f) \in {"down", "asleep"} /\ ~held[f] /\ daemon[f] # "saysdown" => since[f] = Total

\* A friend never stays deaf longer than DeafHold: deaf for since - HeardUp ticks.
DeafNeverLongerThanBound ==
  \A f \in Friends : Phase(f) = "deaf" => HeardUp <= since[f] /\ since[f] - HeardUp < DeafHold

\* A step that carries an event leaves the friend up (unless held, or her beat says down).
ActivityWithinBoundNeverDown ==
  [][\A f \in Friends :
       since'[f] = 0 /\ ~held'[f] /\ daemon'[f] # "saysdown" =>
         PhaseIn(f, held', daemon', heardAge') = "up"]_vars

\* No friend goes up to down in one step (a hold, or a beat that says down, aside).
UpToDownPassesDeaf ==
  [][\A f \in Friends :
       Phase(f) = "up" /\ ~held'[f] /\ daemon'[f] # "saysdown" =>
         PhaseIn(f, held', daemon', heardAge') \notin {"down", "asleep"}]_vars

\* A card leaves a friend for the pool only when she is down, asleep or held after
\* the step: never from up or deaf.
CardsReturnOnlyFromDownOrAsleep ==
  [][\A c \in Cards :
       holder[c] \in Friends /\ holder'[c] = Pool =>
         PhaseIn(holder[c], held', daemon', heardAge') \in {"held", "down", "asleep"}]_vars

\* A held, down or asleep friend holds no card, at any state.
NoCardOnDownOrAsleep ==
  \A f \in Friends : Phase(f) \in {"held", "down", "asleep"} => CardsOf(f) = {}

\* The deal reaches only a friend the table shows up; the take likewise.
DealOnlyToShownUp ==
  [][\A c \in Cards : (holder[c] = Pool /\ holder'[c] \in Friends) => Status(holder'[c]) = "up"]_vars
TakeOnlyWhenUp ==
  [][\A c \in Cards : (~taken[c] /\ taken'[c]) => Status(holder[c]) = "up"]_vars

\* ---------------------------------------------------------------- the liveness

\* The friend is up, or the hold or her beat that says down wins over last-heard.
UpOrOverridden(f) == Phase(f) = "up" \/ held[f] \/ daemon[f] = "saysdown"

\* A down friend whose session answers is up on her next event (SpecLive).
DownComesUpOnNextEvent ==
  \A f \in Friends :
    (Phase(f) \in {"down", "asleep"} /\ session[f] = "answering") ~> UpOrOverridden(f)

\* An asleep friend who is woken and answers reaches up (SpecWake: the wake's
\* answer alone, with no other event fair).
AsleepWokenReachesUp ==
  \A f \in Friends : (Phase(f) = "asleep" /\ wake[f]) ~> UpOrOverridden(f)

\* A deaf friend does not stay deaf (SpecClock: the clock alone).
DeafLeavesDeaf == \A f \in Friends : Phase(f) = "deaf" ~> Phase(f) # "deaf"

=============================================================================
