---------------------------- MODULE ServerSwitch ----------------------------
(***************************************************************************)
(* The server switch and its rollback window (internal/sprint/             *)
(* server_switch.go ServerSwitch, ServerRollback, CheckRollbackOnLandFailure; *)
(* cmd/nova-sprint/landloop.go landOnce; cmd/nova-sprint/run.go runLoop;    *)
(* docs/SPEC-SPRINT.md section 14, "server switch <binary> [--rollback]").  *)
(* ServerSwitch backs the target up to <target>.prev (only when a target    *)
(* exists), copies the candidate over the target, then writes                *)
(* <target>.switch.json when rollback is on; a crash between the copy and   *)
(* the state write leaves the switch without a rollback window. A land pass *)
(* fault (exit 2) inside the window rolls the previous binary back to the   *)
(* target and removes the state; past the deadline the state is removed and *)
(* the switch is permanent; a refusal (exit 1) never rolls back.            *)
(* ServerRollback restores the previous binary by hand and removes the      *)
(* state.                                                                   *)
(*                                                                          *)
(* The hook gate (cmd/nova-sprint/hook.go hookedIn; land.go, coordinator.go *)
(* hookGateError): the machine's landing and the coordinator's are split by *)
(* identity. The server's own automatic landing of accepted cards (the      *)
(* loop's pass) is machine work and is exempt from the gate (land.go reopens*)
(* the store as run when the gate refuses, so the automatic pass runs       *)
(* whatever the hook); only a coordinator's hand land (nova-sprint land)    *)
(* still passes the gate. A restart drops the seat's hook (SwitchRestart);  *)
(* the seat hooks in again (SeatHooksIn). A hand land the gate refuses      *)
(* before it acts (LandHookRefused) is a wait, not a fault, so it never     *)
(* rolls the switch back and leaves the window as it was.                   *)
(*                                                                          *)
(*                                                                          *)
(* Two facts of the code are modelled and worth naming:                     *)
(*   a second switch inside the window overwrites <target>.prev with the    *)
(*   first candidate (the backup copies the current target, which after the *)
(*   first switch is the first candidate), so the original binary is lost;  *)
(*   and a switch with no prior target (the fresh install) writes a state   *)
(*   whose Previous file is missing, so every failure inside the window     *)
(*   errors instead of rolling back.                                        *)
(*                                                                          *)
(* The design is Broken = {}. Each other value is a reversed witness:        *)
(*   "nodeadline"  skips the deadline check (a failure past the window      *)
(*                 rolls back): RollbackOnlyInWindow fails;                 *)
(*   "keepstate"   keeps the state after a rollback (a second failure in    *)
(*                 the window rolls back again):                            *)
(*                 AtMostOneAutoRollbackPerSwitch fails;                    *)
(*   "hookfault"   counts a hand land the hook gate refused as a land fault *)
(*                 (the v1.2.12 switch rolled itself back 1.5 s after it    *)
(*                 began): NoRollbackOnAHookRefusal fails;                  *)
(*   "hookgate"    restores the old gate (the automatic pass waits while    *)
(*                 the seat is unhooked): LandingNeverWaitsOnTheHook fails; *)
(*   "silentdedupe" the dedupe before v1.2.17: the same refusal again      *)
(*                 writes nothing, so the beat's line falls back to LAND    *)
(*                 IDLE (epoch 20, 2026-10-13: twenty minutes of IDLE with  *)
(*                 six cards queued): BeatNamesTheRefusal fails;            *)
(*   "latejudge"   the judgment is raised only after the stall bound:       *)
(*                 RefusalJudgedAtOnce fails;                               *)
(*   "partialrefusal" a pass of landed and refused batches (land exits 1)  *)
(*                 is read as a standing refusal (PR 189's first cut, its   *)
(*                 reason a LAND OK line): RefusalOnlyWhenNothingCouldLand  *)
(*                 fails;                                                   *)
(*   "rawkey"      a cause is keyed on its raw text, whose timings change   *)
(*                 every pass, so the same cause is closed and raised again *)
(*                 (PR 189's first cut): AtMostOneJudgmentPerRefusal fails; *)
(*   "rejudge"     the same cause with the same text is judged again on     *)
(*                 every pass: AtMostOneJudgmentPerRefusal fails;           *)
(*   "idleclears"  a round that ran no land (nothing queued, or no          *)
(*                 coordinator) clears a standing refusal:                  *)
(*                 RefusalClearsOnlyWhenItsCauseIsGone fails;               *)
(*   "ackcloses"   the seat's ack closes the judgment and keeps no          *)
(*                 acknowledgement (steps_ack.go before PR 189's read 3),   *)
(*                 so the next beat's re-check raises the same cause again: *)
(*                 AckSticks fails;                                         *)
(*   "staleack"    a cause that cleared and came back before its close was  *)
(*                 written is raised behind the stale acknowledgement,      *)
(*                 which the raise reads as said (PR 189 read 4):           *)
(*                 NoStaleAck fails.                                        *)
(*                                                                          *)
(* The judgment is modelled as the store holds it, never as the loop's      *)
(* in-memory flag: stored[c] is "none", "open" or "acked" for each cause.   *)
(* A write may fail (F3: LAND JUDGMENT NOT WRITTEN); pending is the owed    *)
(* write, retried by the next beat's re-check (Recheck). The seat closes a  *)
(* judgment (SeatCloses): an ack keeps an acknowledgement, so the cause is  *)
(* not raised again while it stands; a check closes it, and the next beat   *)
(* raises it again as a new episode. A clear closes the judgment and its    *)
(* acknowledgement (LandRefusalClose), so a stale ack never blocks a later  *)
(* raise of the same cause.                                                 *)
(*                                                                          *)
(* The lander's standing refusal (cmd/nova-sprint/landloop.go standRefusal, *)
(* clearRefusal, judgeRefusal, writeBeat, writeFlight, writeRefused;        *)
(* internal/sprint/judgments.go LandRefusalRaise, LandRefusalClose,         *)
(* LandRefusalKey). A refusal is a state, not an event. It stands only when *)
(* nothing could land, for one of three kinds of cause: land refused before *)
(* any batch (a setting such as git_identity, the epoch, the fence: Reasons,*)
(* "the lander refuses every landing"); land failed before any batch (exit  *)
(* 2, LandFailed*: "fault"); or the loop could not read the merge queue     *)
(* (QueueUnreadable: "queue"). The two failures are judged as "the lander   *)
(* fails every landing" and their beat line is LAND FAILED, so a store blip *)
(* is never worded as a refusal; all three are one state here. A cause's    *)
(* text varies pass to pass (Texts: timings, counts, an error's words); the *)
(* cause, not its text, is what stands (sprint.LandRefusalKey). Every beat  *)
(* it stands names it, its first beat judges it, the same cause again       *)
(* raises nothing, a pass that gets past it clears it (LandOk, LandPartial),*)
(* a read of the queue clears a "queue" cause, and a round that ran no land *)
(* clears nothing (NoRun).                                                  *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS Binaries,     \* the binary names: the original and the two candidates
          Candidate,    \* [1..MaxSwitch -> Binaries]: the candidate of each switch
          Window,       \* the rollback window length
          MaxClock,     \* the clock's bound
          MaxSwitch,    \* the switch bound (at most two switches)
          Broken        \* the reversed witnesses: a subset of Faults

Faults == {"nodeadline", "keepstate", "hookfault", "hookgate", "silentdedupe", "latejudge",
           "partialrefusal", "rawkey", "rejudge", "idleclears", "ackcloses",
           "staleack"}

\* the causes land refuses before any batch for (the empty git_identity, any other)
Reasons == {"identity", "other"}
\* every cause a refusal stands for: a refusal, a pass fault, an unreadable queue
Causes == Reasons \cup {"fault", "queue"}
\* the texts a cause's line may carry, pass to pass (0 is none)
Texts == {1, 2}
ASSUME Broken \subseteq Faults
ASSUME Candidate \in [1..MaxSwitch -> Binaries]
ASSUME Window > 0 /\ MaxClock >= 0 /\ MaxSwitch >= 1
ASSUME "none" \notin Binaries

VARIABLES target,       \* the binary now at the target: Binaries, or "none" (no target yet)
          prev,         \* the binary at <target>.prev: Binaries, or "none" (no backup)
          hasState,     \* is there a <target>.switch.json
          statePrev,    \* the state's Previous (meaningful when hasState)
          stateAt,      \* the state's SwitchedAt clock (meaningful when hasState)
          clock,        \* the wall clock, 0..MaxClock
          switches,     \* switches made, bounded by MaxSwitch
          midSwitch,    \* TRUE between the copy and the state write (the crash point)
          autoRollbacks,  \* ghost: auto-rollbacks made (AtMostOneAutoRollbackPerSwitch)
          rolledLate,     \* ghost: an auto-rollback past the deadline happened
          hooked,         \* the seat holds a live, proven hook (the hand land's gate)
          rolledOnRefusal, \* ghost: a rollback was made for a land the hook gate refused
          landTried,       \* ghost: the automatic pass landed or refused (never the hook)
          refusal,         \* the lander's standing refusal: a cause, or "none"
          rtext,           \* the text its last line carried (0 when none stands)
          stored,          \* the store's record of each cause's judgment: "none", "open", "acked"
          pending,         \* a judgment write failed and is owed (retried next beat)
          closeOwed,       \* a clear's close of the judgment and its acknowledgement is owed
          reopen,          \* the seat closed the judgment (check): the next beat raises it anew
          raisedFor,       \* ghost: judgments written for each cause in this episode
          ackedOnce,       \* ghost: the seat acked the standing cause
          reraisedAfterAck, \* ghost: an acked cause was raised again while it stood
          refusedAt,       \* the clock when the standing refusal began
          line,            \* the beat's line: "idle", "refused", "ok" or "partial"
          passed,          \* ghost: the last pass got past every check before its batches
          clearedBy        \* ghost: what last cleared a refusal: "none", "pass", "read", "norun"

rvars == <<refusal, rtext, stored, pending, closeOwed, reopen, raisedFor, ackedOnce, reraisedAfterAck,
          refusedAt, line, passed, clearedBy>>

vars == <<target, prev, hasState, statePrev, stateAt, clock, switches,
          midSwitch, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried,
          refusal, rtext, stored, pending, closeOwed, reopen, raisedFor, ackedOnce,
          reraisedAfterAck, refusedAt, line, passed, clearedBy>>

TypeOK ==
  /\ target \in Binaries \cup {"none"}
  /\ prev \in Binaries \cup {"none"}
  /\ hasState \in BOOLEAN
  /\ statePrev \in Binaries \cup {"none"}
  /\ stateAt \in 0..MaxClock
  /\ clock \in 0..MaxClock
  /\ switches \in 0..MaxSwitch
  /\ midSwitch \in BOOLEAN
  /\ autoRollbacks \in 0..MaxSwitch
  /\ rolledLate \in BOOLEAN
  /\ hooked \in BOOLEAN
  /\ rolledOnRefusal \in BOOLEAN
  /\ landTried \in BOOLEAN
  /\ refusal \in Causes \cup {"none"}
  /\ rtext \in Texts \cup {0}
  /\ stored \in [Causes -> {"none", "open", "acked"}]
  /\ pending \in BOOLEAN
  /\ closeOwed \in BOOLEAN
  /\ reopen \in BOOLEAN
  /\ raisedFor \in [Causes -> 0..3]
  /\ ackedOnce \in BOOLEAN
  /\ reraisedAfterAck \in BOOLEAN
  /\ refusedAt \in 0..MaxClock
  /\ line \in {"idle", "refused", "ok", "partial"}
  /\ passed \in BOOLEAN
  /\ clearedBy \in {"none", "pass", "read", "norun"}

Init ==
  /\ target \in Binaries \cup {"none"}
  /\ prev = "none"
  /\ hasState = FALSE
  /\ statePrev = "none"
  /\ stateAt = 0
  /\ clock = 0
  /\ switches = 0
  /\ midSwitch = FALSE
  /\ autoRollbacks = 0
  /\ rolledLate = FALSE
  /\ hooked \in BOOLEAN
  /\ rolledOnRefusal = FALSE
  /\ landTried = FALSE
  /\ refusal = "none"
  /\ rtext = 0
  /\ stored = [c \in Causes |-> "none"]
  /\ pending = FALSE
  /\ closeOwed = FALSE
  /\ reopen = FALSE
  /\ raisedFor = [c \in Causes |-> 0]
  /\ ackedOnce = FALSE
  /\ reraisedAfterAck = FALSE
  /\ refusedAt = 0
  /\ line = "idle"
  /\ passed = FALSE
  /\ clearedBy = "none"

\* the wall clock advances
Tick == /\ midSwitch = FALSE
        /\ clock < MaxClock
        /\ clock' = clock + 1
        /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, switches,
                       midSwitch, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>
        /\ UNCHANGED rvars

\* ServerSwitch: back the target up to .prev (when there is one) and copy the
\* candidate over the target; the state write is the next step, so a crash
\* between the copy and the state write is midSwitch.
Switch == /\ midSwitch = FALSE
          /\ switches < MaxSwitch
          /\ target' = Candidate[switches + 1]
          /\ prev' = IF target = "none" THEN "none" ELSE target
          /\ switches' = switches + 1
          /\ midSwitch' = TRUE
          /\ UNCHANGED <<hasState, statePrev, stateAt, clock, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>
          /\ UNCHANGED rvars

\* the state write: <target>.switch.json records Target, Previous, SwitchedAt.
SwitchWriteState == /\ midSwitch = TRUE
                    /\ hasState' = TRUE
                    /\ statePrev' = prev
                    /\ stateAt' = clock
                    /\ midSwitch' = FALSE
                    /\ UNCHANGED <<target, prev, clock, switches, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>
                    /\ UNCHANGED rvars

\* a crash between the copy and the state write: the target is switched but no
\* state is written, so no rollback window opens.
CrashSwitch == /\ midSwitch = TRUE
               /\ midSwitch' = FALSE
               /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                              switches, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>
               /\ UNCHANGED rvars

\* ServerRollback: the previous binary back to the target, the state removed.
ManualRollback == /\ midSwitch = FALSE
                  /\ prev # "none"
                  /\ target' = prev
                  /\ hasState' = FALSE
                  /\ statePrev' = "none"
                  /\ stateAt' = 0
                  /\ UNCHANGED <<prev, clock, switches, midSwitch, autoRollbacks, rolledLate, hooked, rolledOnRefusal, landTried>>
                  /\ UNCHANGED rvars

\* The automatic pass is enabled whatever the hook; only the reversed witness
\* "hookgate" restores the old gate, under which the pass waits while the seat is
\* unhooked.
AutoLandEnabled == "hookgate" \notin Broken \/ hooked

\* A land that is ok (exit 0) rolls nothing back. A refusal (exit 1) reaches
\* CheckRollbackOnLandFailure (landloop.go calls it on every nonzero exit) but
\* returns without rolling back (the le.Code != 2 filter), so only a pass fault
\* (exit 2) rolls back. The v1.2.11 card made a refusal (exit 1) stop rolling back.
\* The automatic pass runs whatever the hook (the reversed witness "hookgate"
\* restores the old gate, which waited while the seat was unhooked).
\* Cleared: no refusal stands (by: what cleared it); the close of its judgment and
\* any acknowledgement is owed (CloseWrite, which may not land at once: a store
\* blink, the line of control busy).
Cleared(by) == /\ refusal' = "none" /\ rtext' = 0
               /\ stored' = stored
               /\ closeOwed' = (closeOwed \/ \E x \in Causes : stored[x] # "none")
               /\ pending' = FALSE /\ reopen' = FALSE
               /\ raisedFor' = [c \in Causes |-> 0] /\ ackedOnce' = FALSE /\ refusedAt' = 0
               /\ UNCHANGED reraisedAfterAck
               /\ clearedBy' = IF refusal = "none" THEN clearedBy ELSE by

\* Write(c, st0, rf0): the beat's judgment write for cause c, from the store st0 and
\* the count rf0, landing (stored open, one more raise) or failing (owed: pending).
Write(c, st0, rf0, acked) ==
  \/ /\ stored' = [st0 EXCEPT ![c] = "open"]
     /\ raisedFor' = [rf0 EXCEPT ![c] = @ + 1]
     /\ reraisedAfterAck' = (reraisedAfterAck \/ acked)
     /\ pending' = FALSE /\ reopen' = FALSE
  \/ /\ stored' = st0 /\ raisedFor' = rf0 /\ pending' = TRUE
     /\ UNCHANGED <<reraisedAfterAck, reopen>>

\* Stand(c, t): a pass, or a read of the queue, stands for cause c, its line carrying
\* text t (landloop.go standRefusal). The same cause again, whatever its text, keeps
\* the refusal that stands, its judgment and its since (keyed on the class at the
\* source, sprint.LandRefusalKey); another cause replaces it, closes the replaced
\* cause's judgment and acknowledgement, and is judged at once on this beat
\* (judgeRefusal; judgments.go LandRefusalRaise). The beat's line names it.
\* Witnesses: "rawkey" keys on the text too; "rejudge" judges the same cause again;
\* "silentdedupe" writes nothing for the same cause again; "latejudge" judges only
\* past the bound (LateJudge).
Stand(c, t) ==
  LET same == refusal = c /\ (rtext = t \/ "rawkey" \notin Broken)
      st0  == IF refusal # c /\ refusal # "none" THEN [stored EXCEPT ![refusal] = "none"] ELSE stored
      rf0  == IF refusal # c THEN [x \in Causes |-> 0] ELSE raisedFor
  IN /\ refusal' = c /\ rtext' = t /\ passed' = FALSE
     /\ refusedAt' = IF refusal = c THEN refusedAt ELSE clock
     /\ ackedOnce' = IF refusal = c THEN ackedOnce ELSE FALSE
     /\ IF same /\ "rejudge" \notin Broken
          THEN UNCHANGED <<stored, pending, reopen, raisedFor, reraisedAfterAck>>
          ELSE IF "latejudge" \in Broken /\ refusal # c
            THEN /\ stored' = st0 /\ raisedFor' = rf0
                 /\ UNCHANGED <<pending, reopen, reraisedAfterAck>>
          ELSE IF closeOwed /\ "staleack" \notin Broken
            \* the owed close first: nothing is raised until it lands (judgeRefusal)
            THEN /\ stored' = st0 /\ raisedFor' = rf0 /\ pending' = TRUE
                 /\ UNCHANGED <<reopen, reraisedAfterAck>>
          ELSE IF closeOwed /\ st0[c] # "none"
            \* "staleack": the raise finds the stale record of its key and writes nothing
            THEN /\ stored' = st0 /\ raisedFor' = rf0 /\ pending' = FALSE
                 /\ UNCHANGED <<reopen, reraisedAfterAck>>
          ELSE Write(c, st0, rf0, refusal = c /\ ackedOnce)
     /\ UNCHANGED closeOwed
     /\ line' = IF "silentdedupe" \in Broken /\ refusal = c THEN "idle" ELSE "refused"
     /\ UNCHANGED clearedBy

\* The owed close lands (judgeRefusal, before any raise; LandRefusalClose): every
\* lander judgment and acknowledgement is closed.
CloseWrite == /\ midSwitch = FALSE /\ closeOwed
              /\ stored' = [x \in Causes |-> "none"] /\ closeOwed' = FALSE
              /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                             switches, midSwitch, autoRollbacks, rolledLate, hooked,
                             rolledOnRefusal, landTried, refusal, rtext, pending, reopen,
                             raisedFor, ackedOnce, reraisedAfterAck, refusedAt, line,
                             passed, clearedBy>>

\* The beat's re-check against the store (judgeRefusal): a standing cause whose
\* judgment the store does not hold (a write owed, or the seat closed it with check)
\* is written; one held open or acknowledged is left. Not under "latejudge".
Recheck == /\ midSwitch = FALSE /\ "latejudge" \notin Broken /\ ~closeOwed
           /\ refusal # "none" /\ stored[refusal] = "none"
           /\ Write(refusal, stored, raisedFor, ackedOnce)
           /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                          switches, midSwitch, autoRollbacks, rolledLate, hooked,
                          rolledOnRefusal, landTried, refusal, rtext, ackedOnce,
                          refusedAt, line, passed, clearedBy, closeOwed>>

\* The re-check's write that lands: what fairness asks of the store coming back.
RecheckOk == /\ Recheck /\ stored'[refusal] = "open"

\* The seat closes the standing cause's judgment: an ack keeps an acknowledgement
\* (steps_ack.go: IsLanderStanding), a check closes it and the next beat raises it
\* anew, a new episode. With "ackcloses" an ack keeps nothing.
SeatCloses == /\ refusal # "none" /\ stored[refusal] = "open"
              /\ \/ /\ stored' = [stored EXCEPT ![refusal] =
                                   IF "ackcloses" \in Broken THEN "none" ELSE "acked"]
                    /\ ackedOnce' = TRUE
                    /\ reopen' = ("ackcloses" \in Broken) \* the old ack: a close, as check is
                    /\ UNCHANGED raisedFor
                 \/ /\ stored' = [stored EXCEPT ![refusal] = "none"]
                    /\ raisedFor' = [raisedFor EXCEPT ![refusal] = 0] /\ reopen' = TRUE
                    /\ UNCHANGED ackedOnce
              /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                             switches, midSwitch, autoRollbacks, rolledLate, hooked,
                             rolledOnRefusal, landTried, refusal, rtext, pending,
                             reraisedAfterAck, refusedAt, line, passed, clearedBy, closeOwed>>

\* A land that is ok (exit 0) rolls nothing back. A refusal (exit 1) reaches
\* CheckRollbackOnLandFailure (landloop.go calls it on every nonzero exit) but
\* returns without rolling back (the le.Code != 2 filter), so only a pass fault
\* (exit 2) rolls back. The automatic pass runs whatever the hook (the reversed
\* witness "hookgate" restores the old gate). A landing clears the standing refusal
\* and closes its judgment (landloop.go clearRefusal, judgeRefusal; judgments.go
\* LandRefusalClose).
LandOk == /\ midSwitch = FALSE /\ AutoLandEnabled /\ landTried' = TRUE
          /\ Cleared("pass") /\ line' = "ok" /\ passed' = TRUE
          /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                         switches, midSwitch, autoRollbacks, rolledLate, hooked,
                         rolledOnRefusal>>

\* Land refused before any batch (exit 1, no batch line: landloop.go landBatched).
LandRefused == /\ midSwitch = FALSE /\ AutoLandEnabled /\ landTried' = TRUE
               /\ \E r \in Reasons, t \in Texts : Stand(r, t)
               /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                              switches, midSwitch, autoRollbacks, rolledLate, hooked,
                              rolledOnRefusal>>

\* A pass that reached its batches, some landed and some refused one by one (land
\* exits 1; landloop.go landBatched, clearRefusal): no refusal stands, and one that
\* stood is cleared; each refused batch has its own record and judgment, outside this
\* model. With "partialrefusal" the pass is read as a standing refusal.
LandPartial == /\ midSwitch = FALSE /\ AutoLandEnabled /\ landTried' = TRUE
               /\ IF "partialrefusal" \in Broken
                    THEN \E r \in Reasons, t \in Texts :
                           /\ refusal' = r /\ rtext' = t /\ refusedAt' = clock
                           /\ stored' = [x \in Causes |-> IF x = r THEN "open" ELSE "none"]
                           /\ raisedFor' = [x \in Causes |-> IF x = r THEN 1 ELSE 0]
                           /\ pending' = FALSE /\ reopen' = FALSE /\ ackedOnce' = FALSE
                           /\ line' = "refused" /\ passed' = TRUE
                           /\ UNCHANGED <<clearedBy, reraisedAfterAck, closeOwed>>
                    ELSE /\ Cleared("pass") /\ line' = "partial" /\ passed' = TRUE
               /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                              switches, midSwitch, autoRollbacks, rolledLate, hooked,
                              rolledOnRefusal>>

\* The loop could not read the merge queue (landloop.go landOnce, LAND FAILED): the
\* "queue" cause stands; the error's words are its text.
QueueUnreadable == /\ midSwitch = FALSE
                   /\ \E t \in Texts : Stand("queue", t)
                   /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                                  switches, midSwitch, autoRollbacks, rolledLate, hooked,
                                  rolledOnRefusal, landTried>>

\* A round that read the queue and ran no land (nothing queued, or no coordinator):
\* a "queue" cause is gone and cleared (clearQueueRefusal); any other cause stands
\* and the beat names it (landCycle writes the standing line, queued=0 or not). With
\* "idleclears" the round clears any cause.
NoRun == /\ midSwitch = FALSE
         /\ IF refusal = "queue"
              THEN Cleared("read") /\ line' = "idle"
              ELSE IF "idleclears" \in Broken /\ refusal # "none"
                THEN Cleared("norun") /\ line' = "idle"
                ELSE /\ line' = IF refusal # "none" THEN "refused" ELSE "idle"
                     /\ UNCHANGED <<refusal, rtext, stored, pending, reopen, raisedFor, ackedOnce,
                                    reraisedAfterAck, refusedAt, clearedBy, closeOwed>>
         /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                        switches, midSwitch, autoRollbacks, rolledLate, hooked,
                        rolledOnRefusal, landTried, passed>>

\* A beat while a landing is still in flight (landloop.go writeBeat): the standing
\* refusal's line while one stands, else LAND IDLE why=running. With "silentdedupe"
\* (the code before v1.2.17) it is always IDLE.
LandBeat == /\ midSwitch = FALSE
            /\ line' = IF refusal # "none" /\ "silentdedupe" \notin Broken
                       THEN "refused" ELSE "idle"
            /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                           switches, midSwitch, autoRollbacks, rolledLate, hooked,
                           rolledOnRefusal, landTried, refusal, rtext, stored, pending, reopen,
                           raisedFor, ackedOnce, reraisedAfterAck, refusedAt, passed, clearedBy,
                           closeOwed>>

\* The reversed witness "latejudge": the judgment is written only once the refusal
\* has stood past the bound (raiseIfStuck's LandDeadline, the tick's stall alarm).
LateJudge == /\ "latejudge" \in Broken
             /\ refusal # "none" /\ stored[refusal] = "none"
             /\ clock > refusedAt + Window
             /\ stored' = [stored EXCEPT ![refusal] = "open"]
             /\ raisedFor' = [raisedFor EXCEPT ![refusal] = @ + 1]
             /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                            switches, midSwitch, autoRollbacks, rolledLate, hooked,
                            rolledOnRefusal, landTried, refusal, rtext, pending, reopen,
                            ackedOnce, reraisedAfterAck, refusedAt, line, passed, clearedBy,
                            closeOwed>>

\* The server restarts (a switch, a rollback, a crash): the seat's hook is gone with it
\* (the hook record says state=gone, why=the server restarted). SwitchRestart.
SwitchRestart == /\ midSwitch = FALSE
                 /\ hooked
                 /\ hooked' = FALSE
                 /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                                switches, midSwitch, autoRollbacks, rolledLate,
                                rolledOnRefusal, landTried>>
                 /\ UNCHANGED rvars

\* The seat runs nova-sprint hook and answers the challenge: the gate lets a hand
\* land run (the automatic pass never waits on it).
SeatHooksIn == /\ midSwitch = FALSE
               /\ ~hooked
               /\ hooked' = TRUE
               /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                              switches, midSwitch, autoRollbacks, rolledLate,
                              rolledOnRefusal, landTried>>
               /\ UNCHANGED rvars

\* A hand land (a coordinator command, not the loop's pass) the hook gate refuses
\* before it acts on anything (the seat has not hooked in: cmd/nova-sprint/land.go,
\* the store's open): a wait, not a fault. The window, the binary and the state are
\* left as they were, and the hand land tries again. With "hookfault" the refusal is
\* counted as a land fault (LandHookFault below), the v1.2.12 defect.
LandHookRefused == /\ midSwitch = FALSE
                   /\ ~hooked
                   /\ "hookfault" \notin Broken
                   /\ UNCHANGED vars

\* The defect, reversed: the refused hand land reaches the rollback check as a fault,
\* and inside the window with a previous binary it rolls the switch back.
LandHookFault == /\ midSwitch = FALSE
                 /\ ~hooked
                 /\ "hookfault" \in Broken
                 /\ hasState
                 /\ statePrev # "none"
                 /\ clock <= stateAt + Window
                 /\ target' = statePrev
                 /\ hasState' = FALSE
                 /\ statePrev' = "none"
                 /\ stateAt' = 0
                 /\ autoRollbacks' = autoRollbacks + 1
                 /\ rolledOnRefusal' = TRUE
                 /\ UNCHANGED <<prev, clock, switches, midSwitch, rolledLate, hooked, landTried>>
                 /\ UNCHANGED rvars

\* A pass fault (exit 2) before any batch: nothing could land, so the "fault" cause
\* stands (landloop.go landOnce; judged as "the lander fails every landing", its beat
\* line LAND FAILED); a fault after a batch landed is LandPartial's. With no state,
\* nothing rolls back.
LandFailedNoState == /\ midSwitch = FALSE
                     /\ AutoLandEnabled
                     /\ ~hasState
                     /\ landTried' = TRUE
                     /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                                    switches, midSwitch, autoRollbacks, rolledLate, hooked,
                                    rolledOnRefusal>>
                     /\ \E t \in Texts : Stand("fault", t)

\* a pass fault past the deadline: the state is removed, the switch permanent
\* (with "nodeadline" the deadline check is skipped, so this never fires).
LandFailedPastDeadline == /\ midSwitch = FALSE
                          /\ AutoLandEnabled
                          /\ hasState
                          /\ clock > stateAt + Window
                          /\ "nodeadline" \notin Broken
                          /\ hasState' = FALSE
                          /\ statePrev' = "none"
                          /\ stateAt' = 0
                          /\ landTried' = TRUE
                          /\ UNCHANGED <<target, prev, clock, switches, midSwitch,
                                         autoRollbacks, rolledLate, hooked, rolledOnRefusal>>
                          /\ \E t \in Texts : Stand("fault", t)

\* a pass fault in the window with no previous binary (the fresh install): the
\* rollback errors, the state stays, and every such failure errors.
LandFailedError == /\ midSwitch = FALSE
                   /\ AutoLandEnabled
                   /\ hasState
                   /\ statePrev = "none"
                   /\ \/ clock <= stateAt + Window
                      \/ "nodeadline" \in Broken
                   /\ landTried' = TRUE
                   /\ UNCHANGED <<target, prev, hasState, statePrev, stateAt, clock,
                                  switches, midSwitch, autoRollbacks, rolledLate, hooked,
                                  rolledOnRefusal>>
                   /\ \E t \in Texts : Stand("fault", t)

\* a pass fault in the window with a previous binary: roll back (and, with
\* "nodeadline", roll back even past the deadline).
LandFailedRollback == /\ midSwitch = FALSE
                      /\ AutoLandEnabled
                      /\ hasState
                      /\ statePrev # "none"
                      /\ \/ clock <= stateAt + Window
                         \/ "nodeadline" \in Broken
                      /\ target' = statePrev
                      /\ hasState' = ("keepstate" \in Broken)
                      /\ statePrev' = IF "keepstate" \in Broken THEN statePrev ELSE "none"
                      /\ stateAt' = IF "keepstate" \in Broken THEN stateAt ELSE 0
                      /\ autoRollbacks' = autoRollbacks + 1
                      /\ rolledLate' = (rolledLate \/ (clock > stateAt + Window))
                      /\ landTried' = TRUE
                      /\ UNCHANGED <<prev, clock, switches, midSwitch, hooked, rolledOnRefusal>>
                      /\ \E t \in Texts : Stand("fault", t)

Next ==
  \/ Tick
  \/ Switch \/ SwitchWriteState \/ CrashSwitch
  \/ ManualRollback
  \/ LandOk \/ LandRefused \/ LandPartial \/ LandBeat \/ LateJudge
  \/ QueueUnreadable \/ NoRun \/ Recheck \/ SeatCloses \/ CloseWrite
  \/ SwitchRestart \/ SeatHooksIn \/ LandHookRefused \/ LandHookFault
  \/ LandFailedNoState \/ LandFailedPastDeadline \/ LandFailedError
     \/ LandFailedRollback

\* The automatic land pass, whatever its outcome: a land, a refusal, or a failure
\* (which may roll back). The hand land's gate is LandHookRefused, not this.
AutoLand == LandOk \/ LandRefused \/ LandPartial \/ LandFailedNoState \/ LandFailedPastDeadline
            \/ LandFailedError \/ LandFailedRollback

Spec == Init /\ [][Next]_vars

\* The loop runs pass after pass, so a pass enabled (whatever the hook) eventually
\* runs; a switch left mid-write (midSwitch) still resolves, to the state write or
\* the crash. This is the fairness LandingNeverWaitsOnTheHook is read under.
FairSpec == Spec /\ WF_vars(AutoLand) /\ WF_vars(SwitchWriteState) /\ WF_vars(CrashSwitch)
            /\ WF_vars(LateJudge) /\ WF_vars(RecheckOk) /\ WF_vars(CloseWrite)

---------------------------------------------------------------------------
\* The target is always a known binary (the original or a candidate) or absent
\* (the fresh install, no target yet); never any other value.
TargetIsAKnownBinary == target \in Binaries \/ target = "none"

\* A rollback happens only within the window (deadline = SwitchedAt + Window).
RollbackOnlyInWindow == ~rolledLate

\* Each switch rolls back at most once: an auto-rollback consumes its state, so
\* there can be no more auto-rollbacks than switches.
AtMostOneAutoRollbackPerSwitch == autoRollbacks <= switches

\* A land the hook gate refused never rolls a switch back (the v1.2.12 switch rolled
\* itself back 1.5 seconds after it began, on the new server's first pass, before the
\* seat hooked in again). The gate now refuses only a hand land, so the ghost is the
\* hand land's alone.
NoRollbackOnAHookRefusal == ~rolledOnRefusal

\* An accepted card with the seat unhooked and the server running (midSwitch = FALSE) is
\* landed or refused for a reason other than the hook: the automatic pass runs whatever
\* the hook, so it never waits for the seat to hook in (under weak fairness of the pass).
LandingNeverWaitsOnTheHook == (~hooked /\ midSwitch = FALSE) ~> landTried

\* Held: the store holds the standing cause's judgment, open or acknowledged.
Held == refusal # "none" => stored[refusal] # "none"

\* A standing refusal is judged at once: on the beat its cause begins, never after a
\* stall bound (the reversed witness "latejudge"); short of a write that failed (owed,
\* said as LAND JUDGMENT NOT WRITTEN, retried) or the seat's own close (check).
RefusalJudgedAtOnce == (refusal # "none" /\ ~pending /\ ~reopen) => stored[refusal] # "none"

\* One judgment per standing cause in an episode: the same cause again, whatever its
\* text, raises none (the reversed witnesses "rawkey" and "rejudge").
AtMostOneJudgmentPerRefusal == \A c \in Causes : raisedFor[c] <= 1

\* An ack sticks: a cause the seat acknowledged is never raised again while it stands
\* (the reversed witness "ackcloses").
AckSticks == ~reraisedAfterAck

\* No stale acknowledgement: a standing cause held as acknowledged was acknowledged by
\* the seat while it stood, never left over from before it last cleared (the
\* reversed witness "staleack": the cause comes back before the clear's close lands).
NoStaleAck == (refusal # "none" /\ stored[refusal] = "acked") => ackedOnce

\* While a refusal stands, every beat's line names it: never a bare LAND IDLE, a
\* landing in flight, a round with no coordinator or nothing queued included (the
\* reversed witness "silentdedupe", epoch 20's twenty minutes of IDLE).
BeatNamesTheRefusal == refusal # "none" => line = "refused"

\* A refusal stands only when nothing could land: never after a pass that got past
\* every check before its batches (a pass of landed and refused batches is none).
RefusalOnlyWhenNothingCouldLand == refusal # "none" => ~passed

\* A refusal is cleared only when its cause is gone: a pass got past it, or the
\* queue it could not read was read; never by a round that ran no land.
RefusalClearsOnlyWhenItsCauseIsGone == clearedBy # "norun"

\* Every refused landing is eventually reported to the seat: the store holds its
\* judgment (written, retried until a write lands), or it is cleared.
RefusalIsReported == (refusal # "none") ~> Held
=============================================================================
