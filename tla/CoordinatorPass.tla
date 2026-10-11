--------------------------- MODULE CoordinatorPass ---------------------------
\* The coordinator's pass (docs/SPEC-SPRINT.md section 8, "The coordinator's pass";
\* internal/sprint/coordinator_pass.go, TickCoordinatorPass). The owner, 2026-10-05:
\* "everything I described above needs to be mechanical, so you have a reminder to do it
\* (notification) coming from the machine every 10 minutes. Otherwise, you will eventually
\* drift and forget."
\*
\* One condition on one subject (a friend's session deaf, a friend holding working cards
\* that finishes none, judgments late on the coordinator: each is this machine), and its
\* judgment:
\*   holds   the condition as the world has it now: an outside event, it comes and goes
\*           at any time (Flip).
\*   seen    the condition as the last tick saw it.
\*   open    the judgment is open (the tick's notify wrote it, and has not closed it).
\*   acked   the coordinator acknowledged it (ack): kept on the subject as an
\*           acknowledgement, never raised again, until the condition clears.
\*   first   the running clock it was written at (its At); -1 while none.
\*   again   how many times it was raised again (its Before): the k-th raise again is
\*           due k times Every after first.
\*   clk     running time, one step a tick (PassEvery is Every steps).
\*   written the judgments written this episode (the notes of the type on the subject).
\*   pushes  the pushes to the coordinator this episode: the first raise, then each
\*           NRaisedAgain note.
\*
\* With Kind = "behind" the condition is not an outside event: it is read off the
\* judgments late on the coordinator (internal/sprint/coordinator_pass.go behindConds).
\* There are several of them, and each overdue part names its own judgment (its NOverdue
\* hold, marks); the pass's judgment is of the oldest of them still late, so when the
\* coordinator answers the oldest the level falls to the next.
\*   marks    the overdue lines the late judgments were named at; -1, written as a mark
\*            of clock, is never a mark (each mark is a clock).
\*   newLate  how many judgments are late but not yet named (their overdue line is
\*            written this tick); the oldest of them is the line of the episode.
\*   lastPush the running clock of the last push about the late judgments: an overdue
\*            line, or the pass's raise or raise again; -1 while none.
\*   tickPushes the pushes about the oldest late judgment the last tick wrote.
\* The condition holds once Every of running time has run since the oldest overdue line
\* (behindConds): the line at the deadline, then the pass Every on, then every Every.
\* The pass escalates it (internal/sprint/stops.go, BehindLevel; the owner, 2026-10-10: no
\* silent waits): every EscEvery of running time since the overdue line is a new level, and
\* the level is part of the condition, so a new level is a new episode: the judgment of the
\* level before, or the acknowledgement of it, closes and a new judgment is raised. When
\* the oldest late judgment closes, the level falls to the next and the pass raises the new
\* (lower) level.
\*   lvl        the level the open (or acknowledged) judgment was raised at; 0 while none.
\*
\* With Kind = "stop" (internal/sprint/stops.go: an automatic stop the machine made, a fleet
\* member down that the coordinator never held, a hard pin waiting on a friend who is not up;
\* the owner, 2026-10-10: "i still dislike these silent stops/failures", and the night
\* before, "it should raise it to you as a thing to do, but not do it automatically") holds
\* is the stop in force. The machine makes it, and the coordinator's undo verb or the stop's
\* own end clears it, between two ticks (Flip); the pass reads it at the next tick.
\*   stoppedAt  the running clock the stop was made at; -1 while none holds.
\* In the code a stop's raise again is pushed in the tick's one digest of the stops
\* (stopsDigest, NStopsDigest), due every PassEvery while any stop has gone that long without a
\* push: for the one stop modelled here that is a push every window, as Tick writes it.
\*
\* With Kind = "empty" (an up friend has an empty row while cards wait) holds is the
\* conjunction the pass reads each tick: she is up and not held, her row is empty, and
\* cards she could do wait elsewhere. The judgment waits EmptyRowAfter (Every, in the
\* code both ten minutes) of that conjunction, kept by the empty-row clock:
\*   watchAt the running clock the clock (NFriendRowEmpty, an acknowledgement) was
\*           written at, the first tick that finds the conjunction; -1 while none. A
\*           tick that finds it gone closes the clock, so time down, held or busy does
\*           not count.
\*   streak  how many ticks in a row have found the conjunction (a ghost: the code
\*           keeps no such count).
\* With Kind = "pin" (a named pin dealt away from its friend) holds is the card ready or
\* working off her row, and the deal writes the judgment on the unit that places it
\* there (Deal); the pass keeps that one note and raises it again in place:
\*   dealt   the open judgment is the deal's, not yet read by a pass.
\*
\* With Kind = "drift" (internal/sprint/drift.go, TickDrift) holds is the drift the facts
\* say holds, and the facts may say nothing of a type at all: the third value "unknown"
\* carries the open judgment (and the coordinator's wait on it) as they are, neither
\* closing nor raising it, so no fact is never a clear.
\*   unknown  the condition is "unknown": the facts said nothing of it this tick.
\*   sawUnknown the last tick saw "unknown" (so the open judgment it kept was carried).
\*
\* The design, Broken = "none":
\*   Tick  the clock steps; when the condition holds, a judgment is written if none is
\*         open or acknowledged, else the open one is raised again in place (again counts
\*         the raises due, one push) once its next raise is due; when it does not hold,
\*         the open judgment (or the acknowledgement) is closed and the episode ends;
\*         when it is "unknown", the open judgment is left as it is.
\*   Ack   the coordinator acknowledges an open judgment.
\* Reversed witnesses, each broken by one property:
\*   "noreraise"  the judgment is raised once and never again (the tick's judgments before
\*                the pass): PushedEveryWindow.
\*   "reopen"     raising again writes a second judgment instead of rewriting the open one:
\*                OneJudgmentAnEpisode.
\*   "noclose"    the judgment stays open when the condition clears: ClosedWhenCleared.
\*   "doublepush" (Kind = "behind") the pass counts a late judgment from its deadline, not
\*                from its overdue line: the line and the pass push in one tick:
\*                OnePushATick.
\*   "noreset"    (Kind = "empty") the empty-row clock is kept when the conjunction is
\*                gone, so time down counts and she is judged on her first tick back:
\*                EmptyAWholeWindow.
\*   "pinrekey"   (Kind = "pin") the pass does not know the deal's judgment as its own
\*                (the text it would write differs from the deal's) and writes a
\*                second: OneJudgmentAnEpisode.
\*   "silentstop" (Kind = "stop") the stop's type is not one the pass reads (the night of
\*                2026-10-09: two machine rows down since they never beat, and a hard pin
\*                to a friend who was down, each found by a person reading the
\*                dashboard): StopSignalled.
\*   "ackforever" (Kind = "behind") the condition carries no level, so one quieting (a
\*                wait to a far review time) quiets the judgments late on the coordinator
\*                for as long as any is late (2026-10-09: 111 judgments waiting, the oldest
\*                51 hours): EscalatedPastAck.
\*   "unknowncloses" (Kind = "drift") the pass closes the open judgment when the facts
\*                say nothing of it, instead of carrying it: RaisedWhileItHolds.
EXTENDS Integers

CONSTANTS Every, MaxClock, Broken, Kind

VARIABLES holds, seen, open, acked, first, again, clk, written, pushes,
          marks, newLate, lastPush, tickPushes, watchAt, streak, dealt, stoppedAt, lvl, unknown, sawUnknown
vars == <<holds, seen, open, acked, first, again, clk, written, pushes,
          marks, newLate, lastPush, tickPushes, watchAt, streak, dealt, stoppedAt, lvl, unknown, sawUnknown>>

\* The escalation step: three windows (the code: BehindEscalateEvery, three PassEvery).
EscEvery == 3 * Every

TypeOK ==
  /\ holds \in BOOLEAN
  /\ seen \in BOOLEAN
  /\ open \in BOOLEAN
  /\ acked \in BOOLEAN
  /\ first \in (0..MaxClock) \cup {-1}
  /\ again \in 0..MaxClock
  /\ clk \in 0..MaxClock
  /\ written \in 0..MaxClock
  /\ pushes \in 0..MaxClock
  /\ marks \in SUBSET (0..MaxClock)
  /\ newLate \in 0..MaxClock
  /\ lastPush \in (0..MaxClock) \cup {-1}
  /\ tickPushes \in 0..2
  /\ watchAt \in (0..MaxClock) \cup {-1}
  /\ streak \in 0..MaxClock
  /\ dealt \in BOOLEAN
  /\ stoppedAt \in (0..MaxClock) \cup {-1}
  /\ lvl \in 0..MaxClock
  /\ unknown \in BOOLEAN
  /\ sawUnknown \in BOOLEAN

Init ==
  /\ holds = FALSE /\ seen = FALSE /\ open = FALSE /\ acked = FALSE
  /\ first = -1 /\ again = 0 /\ clk = 0 /\ written = 0 /\ pushes = 0
  /\ marks = {} /\ newLate = 0 /\ lastPush = -1 /\ tickPushes = 0
  /\ watchAt = -1 /\ streak = 0 /\ dealt = FALSE /\ stoppedAt = -1 /\ lvl = 0
  /\ unknown = FALSE /\ sawUnknown = FALSE

\* The world: the friend's session answers or not, her cards finish or not, the
\* coordinator answers the late judgments or not, the friend comes up or goes down, her
\* row fills or empties, the cards she could do come and go, the pinned card moves; for a
\* stop, the machine makes it, and the coordinator's undo (or its own end) clears it;
\* for a drift, the facts come and go, and may say nothing of a type (unknown).
Flip ==
  /\ IF Kind = "behind"
       THEN /\ unknown' = FALSE
            /\ holds' = holds
            /\ (\/ newLate < MaxClock /\ newLate' = newLate + 1 /\ marks' = marks
                \/ \E m \in marks : marks' = marks \ {m} /\ newLate' = newLate
                \/ newLate > 0 /\ newLate' = newLate - 1 /\ marks' = marks)
       ELSE IF Kind = "drift"
              THEN /\ marks' = marks /\ newLate' = newLate
                   /\ (\/ holds' = ~holds /\ unknown' = FALSE
                       \/ unknown' = ~unknown /\ holds' = FALSE)
              ELSE /\ holds' = ~holds
                   /\ marks' = marks /\ newLate' = newLate /\ unknown' = FALSE
  /\ stoppedAt' = IF Kind = "stop" /\ ~holds THEN clk ELSE -1
  /\ UNCHANGED <<seen, open, acked, first, again, clk, written, pushes, lastPush, tickPushes,
                 watchAt, streak, dealt, lvl, sawUnknown>>

\* Pin: the deal places the pinned card on another row and writes the judgment on that
\* unit (friendDeal, pinIgnoredNote). The deal is a part of the tick, which leaves the
\* condition holding; the next pass reads the deal's note.
Deal ==
  /\ Kind = "pin"
  /\ ~holds /\ ~open /\ ~acked
  /\ holds' = TRUE /\ seen' = TRUE
  /\ open' = TRUE /\ first' = clk /\ again' = 0 /\ dealt' = TRUE
  /\ written' = written + 1 /\ pushes' = pushes + 1
  /\ UNCHANGED <<acked, clk, marks, newLate, lastPush, tickPushes, watchAt, streak, stoppedAt, lvl, unknown, sawUnknown>>

\* The coordinator acknowledges the open judgment.
Ack ==
  /\ open /\ ~acked
  /\ open' = FALSE /\ acked' = TRUE
  /\ UNCHANGED <<holds, seen, first, again, clk, written, pushes, marks, newLate, lastPush, tickPushes,
                 watchAt, streak, dealt, stoppedAt, lvl, unknown, sawUnknown>>

\* The overdue mark after this tick, and the condition the tick's pass reads off it
\* (behind only: every other kind is at level 0).
Late == marks # {} \/ newLate > 0
MarkAt == IF marks = {} THEN -1 ELSE CHOOSE m \in marks : \A n \in marks : m <= n
NextMarks == IF newLate > 0 THEN marks \cup {clk + 1} ELSE marks
NextMarkAt == IF NextMarks = {} THEN -1 ELSE CHOOSE m \in NextMarks : \A n \in NextMarks : m <= n

\* The condition the tick's pass reads. Behind: the oldest overdue line, written by an
\* earlier tick, is Every old. Drift: holds and not unknown. Empty: the empty-row clock is
\* Every old (or the judgment is already open). Stop: the stop is in force, unless the pass
\* does not read its type (silentstop).
CondHolds ==
  CASE Kind = "behind" ->
         Late /\ IF Broken = "doublepush" THEN TRUE ELSE MarkAt # -1 /\ clk + 1 - MarkAt >= Every
    [] Kind = "drift" ->
         holds /\ ~unknown
    [] Kind = "empty" ->
         holds /\ (open \/ acked \/ (watchAt # -1 /\ clk + 1 - watchAt >= Every))
    [] Kind = "stop" ->
         holds /\ Broken # "silentstop"
    [] OTHER -> holds

\* Drift: the facts say nothing of this type, so the condition is "unknown".
CondUnknown == Kind = "drift" /\ unknown

\* This tick's overdue line about a late judgment: the first tick that finds one late and
\* not yet named (the oldest of them becomes the episode's line).
Line == Kind = "behind" /\ newLate > 0 /\ marks = {}

\* The escalation level this tick's pass reads off the oldest overdue mark after this tick,
\* and the level the judgments late on the coordinator have reached now.
NextLevel ==
  IF Kind = "behind" /\ NextMarkAt # -1 THEN (clk + 1 - NextMarkAt) \div EscEvery ELSE 0
Level ==
  IF Kind = "behind" /\ MarkAt # -1 THEN (clk - MarkAt) \div EscEvery ELSE 0

\* This tick's pass meets a judgment (or an acknowledgement) of a level different from the
\* condition's: a new episode, up or down. With ackforever the condition's key carries no
\* level, so the pass never sees one.
Escalate == CondHolds /\ (open \/ acked) /\ lvl # NextLevel /\ Broken # "ackforever"

\* This tick's pass pushes: it raises the judgment, raises it at a new level, or raises it
\* again.
PassPush ==
  /\ CondHolds
  /\ \/ ~open /\ ~acked
     \/ Escalate
     \/ open /\ (clk + 1 - first) \div Every > again /\ Broken # "noreraise"

\* One tick: the overdue part's line, then the pass (TickCoordinatorPass: notify, then
\* reraise).
Tick ==
  /\ clk < MaxClock
  /\ clk' = clk + 1
  /\ seen' = CondHolds
  /\ sawUnknown' = CondUnknown
  /\ UNCHANGED <<holds, stoppedAt, unknown>>
  /\ marks' = IF Kind = "behind" /\ newLate > 0 THEN marks \cup {clk + 1} ELSE marks
  /\ newLate' = IF Kind = "behind" THEN 0 ELSE newLate
  /\ watchAt' = IF Kind # "empty" THEN -1
                ELSE IF holds THEN (IF watchAt = -1 THEN clk + 1 ELSE watchAt)
                ELSE IF Broken = "noreset" THEN watchAt ELSE -1
  /\ streak' = IF Kind = "empty" /\ holds THEN streak + 1 ELSE 0
  /\ dealt' = FALSE
  /\ tickPushes' = (IF Line THEN 1 ELSE 0) + (IF PassPush THEN 1 ELSE 0)
  /\ lastPush' = IF ~(Kind = "behind" /\ Late) THEN -1
                 ELSE IF newLate > 0 \/ PassPush THEN clk + 1 ELSE lastPush
  /\ IF CondHolds
       THEN IF ~open /\ ~acked
              THEN \* raised: the judgment, once an episode
                   /\ open' = TRUE /\ first' = clk + 1 /\ again' = 0 /\ lvl' = NextLevel
                   /\ written' = written + 1 /\ pushes' = pushes + 1
                   /\ UNCHANGED acked
            ELSE IF Escalate
              THEN \* a new level: the judgment (or acknowledgement) of the level before
                   \* closes and the new level's is raised, a new episode, up or down
                   /\ open' = TRUE /\ acked' = FALSE /\ first' = clk + 1 /\ again' = 0
                   /\ lvl' = NextLevel /\ written' = 1 /\ pushes' = 1
            ELSE IF open /\ (clk + 1 - first) \div Every > again /\ Broken # "noreraise"
              THEN \* raised again: in place, every raise due counted, one push
                   /\ again' = (clk + 1 - first) \div Every /\ pushes' = pushes + 1
                   /\ written' = IF Broken = "reopen" THEN written + 1 ELSE written
                   /\ UNCHANGED <<open, acked, first, lvl>>
            ELSE IF open /\ dealt /\ Broken = "pinrekey"
              THEN \* the deal's note not known as the pass's: a second judgment
                   /\ written' = written + 1
                   /\ UNCHANGED <<open, acked, first, again, pushes, lvl>>
            ELSE UNCHANGED <<open, acked, first, again, written, pushes, lvl>>
       ELSE IF CondUnknown
              THEN IF Broken = "unknowncloses"
                THEN \* closed on "unknown", instead of carried
                     /\ open' = FALSE /\ acked' = FALSE /\ first' = -1 /\ again' = 0
                     /\ written' = 0 /\ pushes' = 0 /\ lvl' = 0
                ELSE IF open /\ (clk + 1 - first) \div Every > again /\ Broken # "noreraise"
                  THEN \* carried, raised again on its last facts: no new judgment and no
                       \* close, but the reminder is due
                       /\ again' = (clk + 1 - first) \div Every /\ pushes' = pushes + 1
                       /\ written' = IF Broken = "reopen" THEN written + 1 ELSE written
                       /\ UNCHANGED <<open, acked, first, lvl>>
                  ELSE \* carried: nothing to raise again, and never closed
                       UNCHANGED <<open, acked, first, again, written, pushes, lvl>>
              ELSE IF Broken = "noclose"
                THEN UNCHANGED <<open, acked, first, again, written, pushes, lvl>>
                ELSE \* closed: the episode ends
                     /\ open' = FALSE /\ acked' = FALSE /\ first' = -1 /\ again' = 0
                     /\ written' = 0 /\ pushes' = 0 /\ lvl' = 0

Next == Flip \/ Ack \/ Deal \/ Tick

Spec == Init /\ [][Next]_vars

\* One judgment an episode: raising again rewrites it, never writes a second.
OneJudgmentAnEpisode == written <= 1

\* An open judgment the coordinator has not acknowledged is never left unraised for a
\* whole window: each tick that finds its next raise due raises it.
PushedEveryWindow == (open /\ ~acked) => clk - first < (again + 1) * Every

\* A judgment is open only while the last tick saw its condition hold, or it is carried
\* on "unknown" (a drift type the facts say nothing of is never closed).
ClosedWhenCleared == (open \/ acked) => (seen \/ sawUnknown)

\* The last tick saw the condition hold: its judgment is open, or acknowledged.
RaisedWhileItHolds == (seen /\ clk > 0) => (open \/ acked)

\* Drift: no tick closes an open judgment while its condition is "unknown" (the facts say
\* nothing of it), so the open judgment is carried, never closed.
UnknownCarried == [][~(Kind = "drift" /\ open /\ ~open' /\ unknown)]_vars

\* Every push of an episode is of its one judgment: the first raise and one a raise again.
PushedOnlyWhileRaised == pushes > 0 => (written >= 1 /\ pushes <= again + 1)

\* Behind: the oldest late judgment is pushed at most once a tick, by its overdue line or
\* by the pass, never both.
OnePushATick == tickPushes <= 1

\* Behind: a late judgment the coordinator has not acknowledged is never a whole window
\* without a push once its overdue line is written: the line, then the pass every Every.
LateRemindedEveryWindow ==
  (Kind = "behind" /\ Late /\ MarkAt # -1 /\ ~acked) => clk - lastPush <= Every

\* Empty: a friend is judged only after a whole window of ticks has found her up and not
\* held, her row empty and cards she could do waiting; time down, held or busy restarts it.
EmptyAWholeWindow == (Kind = "empty" /\ (open \/ acked)) => streak > Every

\* Stop: every automatic stop has a raised signal while it holds. Once a tick has run since
\* the machine made the stop, its judgment is open (pushed, and pushed again every Every by
\* PushedEveryWindow) or the coordinator acknowledged it; it is never only in a table cell.
StopSignalled ==
  (Kind = "stop" /\ holds /\ stoppedAt # -1 /\ clk > stoppedAt) => (open \/ acked)

\* Behind: the judgment (or acknowledgement) of the judgments late on the coordinator is
\* never below the level they have reached, so an acknowledgement never outlives its level:
\* a wait that grows is pushed again however it was quieted. A level that falls closes the
\* judgment of the level before and raises the new (lower) one the next tick.
EscalatedPastAck ==
  (Kind = "behind" /\ (open \/ acked)) => lvl >= Level

=============================================================================
