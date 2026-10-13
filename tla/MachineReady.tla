---------------------------- MODULE MachineReady ----------------------------
(***************************************************************************)
(* The machine's READY state (docs/SPEC-SPRINT.md section 14, "READY"; the *)
(* owner, 2026-10-13: "I would like a new state right after the sprint is  *)
(* cleared 'READY'", "it is different to stop").                           *)
(*                                                                         *)
(* The record holds RUNNING or STOPPED (run) and the last epoch the        *)
(* machine ran in (stamp: StartedIn, written by start and by every stop    *)
(* of a RUNNING machine). The state people read is derived (Shown): READY  *)
(* when not running and the stamp is not the current epoch                 *)
(* (internal/sprint/store/tick.go Machine.Ready). A clear is two steps     *)
(* with no fence between them (internal/sprint/store/clear.go): its stop   *)
(* (ClearStop) and its epoch advance (ClearAdvance); a start may land      *)
(* between them, record the old epoch and run on into the new one.        *)
(* ran is the truth the derived state must agree with: the machine has     *)
(* been RUNNING at some point in the current epoch.                        *)
(*                                                                         *)
(* start moves READY and STOPPED to RUNNING; stop moves RUNNING to STOPPED *)
(* and is refused on a READY machine ("nothing to stop"); the tick's done  *)
(* and funds stops are stops of a RUNNING machine. The read-time judgment  *)
(* machine:stopped (internal/sprint/store/ops.go machineGroups) is raised  *)
(* at a tick's read of a STOPPED machine with moves due, never of a READY  *)
(* one. A record from before StartedIn (the live store at epoch 20) is     *)
(* read off its spans; that reading is not modelled here.                  *)
(*                                                                         *)
(* Broken selects a reversed witness, each red on its own invariant:       *)
(*   "clearstopped"  a clear leaves STOPPED     (ClearLeavesReady)         *)
(*   "readyjudges"   a READY tick judges         (ReadyNeverJudges)        *)
(*   "stopready"     stop accepted in READY      (StoppedOnlyFromRunning)  *)
(*   "nostopstamp"   a stop of a RUNNING machine leaves the stamp as it    *)
(*                   was: a start inside a clear, stopped later, reads     *)
(*                   READY though it ran       (RanIsNotReady)             *)
(*   "nogate"        start reads no setting: a cleared epoch whose clear   *)
(*                   left read_cards non-working starts (StartOnlyWorking) *)
(*   "overrefuse"    start refuses a non-working value a set of this epoch *)
(*                   chose: the owner's deliberate degraded run cannot     *)
(*                   start (DeliberateSetStarts)                           *)
(*   "readersgate"   start refuses while no reader is up: the fleet's live  *)
(*                   state blocks a start the settings allow               *)
(*                   (DeliberateSetStarts)                                 *)
(*                                                                         *)
(* The settings gate (v1.2.17, start-refuses-a-non-working-setting; the    *)
(* owner, 2026-10-13: "OK these MUST be fixed. blocker priority."): every  *)
(* setting the design depends on is working, or the value was chosen by a  *)
(* set of this epoch (setBy = "set"); else start refuses, writing nothing  *)
(* (cmd/nova-sprint/run.go startSettingsGate, sprint.Working). working is   *)
(* each setting's predicate (internal/sprint/defaults_working.go); setBy is *)
(* who wrote its value this epoch: "clear" (the clear, or carried), "write" *)
(* (anything but set: a migration, a hand edit, an older binary), or "set". *)
(* The gate is start's: a value changed while RUNNING is the tick's alarms' *)
(* to name, outside this module.                                           *)
(* Start is one atomic step here; in the code the gate's read             *)
(* (startSettingsGate: DoctorRead, the server record) and the RUNNING      *)
(* write (store.SetMachine) are two steps, so a value written between     *)
(* them is not seen by that start (cold read 1 of PR 188, F3; no fix now). *)
(* The fleet's live state while RUNNING is the judgment                    *)
(* machine:landsnothing's (store/ops.go machineGroups), not this module's. *)
(* readersUp is the fleet's live state (a reader up for the tiers the cards  *)
(* need): never a setting, so never in the gate; start warns on it         *)
(* (SettingFinding.Live) and readers come up after the start.             *)
(*                                                                         *)
(* The card check gate (v1.2.17, start-refuses-until-a-clean-check; the    *)
(* owner, 2026-10-13: "if you start a sprint before checking, then it will *)
(* fail and tell you to check."): nova-sprint check cards writes one check *)
(* record for the epoch (rec: clean or failed; recDigest: the digest of    *)
(* the cards it checked; internal/sprint/store/check_record.go), and start *)
(* refuses unless the record is clean and its digest equals the cards' now *)
(* (cmd/nova-sprint/run.go startCardCheckGate). digest abstracts the       *)
(* sha256 over every card's id, stream, tier, priority, needs and brief    *)
(* (sprint.CardsDigest): a change (an admit, move, brief, needs, priority  *)
(* or tier edit) moves it, and a change undone moves it back, as content   *)
(* does. defectOf is whether the cards at each digest hold a finding of    *)
(* the core kinds (sprint.CheckCards), fixed per epoch: the same cards     *)
(* check the same. The record is the epoch's (recFresh): a clear leaves    *)
(* the old epoch's record in the store, which reads it as none.            *)
(*   "stalerecord"   start reads the record's verdict and not its digest:  *)
(*                   a change after a clean check starts (StartOnlyChecked)*)
(*   "failedrecord"  start takes any record of the cards now, a failed one *)
(*                   too                              (StartOnlyChecked)   *)
(*   "clearkeeps"    start reads another epoch's record as this epoch's:   *)
(*                   a start with no record after a clear                  *)
(*                                                    (StartOnlyChecked)   *)
(***************************************************************************)
EXTENDS Integers

CONSTANTS
    MaxEpoch,   \* clears the model takes
    Settings,   \* the settings the gate reads (two stand for every row)
    Broken      \* "none", or the reversed witness above

States == {"READY", "RUNNING", "STOPPED"}
Acts == {"init", "clearstop", "clearadvance", "start", "stop", "refused", "add", "tick",
         "write", "set", "startrefused", "readers", "check", "change", "startunchecked"}
Writers == {"clear", "write", "set"}
Records == {"none", "clean", "failed"}
Digests == 0..1

VARIABLES
    run,        \* the record says RUNNING
    stamp,      \* the last epoch the machine ran in, as recorded; -1 is none
    epoch,      \* the sprint's epoch
    clearing,   \* a clear has stopped the machine and not yet advanced the epoch
    ran,        \* the truth: the machine has been RUNNING in the current epoch
    due,        \* moves are due on the tables
    judged,     \* the read-time machine:stopped judgment, as the last tick read it
    prev,       \* the derived state before the last action
    last,       \* the last action
    working,    \* each setting's value leaves the machine working (sprint.Working finds none)
    setBy,      \* who wrote each setting's value in this epoch: clear, write or set
    readersUp,  \* the fleet's live state: a reader is up for the tiers the cards need
    rec,        \* the check record's verdict as stored: none, clean or failed
    recDigest,  \* the digest of the cards the stored record checked
    recFresh,   \* the stored record is this epoch's (its Epoch field)
    digest,     \* the digest of the cards now (sprint.CardsDigest of the live table)
    defectOf    \* the cards at each digest hold a finding of the core kinds

vars == <<run, stamp, epoch, clearing, ran, due, judged, prev, last, working, setBy, readersUp,
          rec, recDigest, recFresh, digest, defectOf>>
settings == <<working, setBy, readersUp>>
cards == <<rec, recDigest, recFresh, digest, defectOf>>

\* the gate start reads (startSettingsGate): every setting working, or its non-working
\* value chosen by a set of this epoch (then a START WARN line, never silence)
Gate == \A s \in Settings : working[s] \/ setBy[s] = "set"

\* the gate a witness runs: none (nogate), or set-chosen values refused too (overrefuse)
StartGate ==
    CASE Broken = "nogate"     -> TRUE
      [] Broken = "overrefuse" -> \A s \in Settings : working[s]
      [] Broken = "readersgate" -> Gate /\ readersUp
      [] OTHER                 -> Gate

\* the record as the store reads it (store.CheckRecordOf): another epoch's record is none
RecRead == IF recFresh \/ Broken = "clearkeeps" THEN rec ELSE "none"

\* the card check gate start reads (startCardCheckGate): a clean record of this epoch whose
\* digest equals the cards' now; each witness reads less
CardGate ==
    CASE Broken = "stalerecord"  -> RecRead = "clean"
      [] Broken = "failedrecord" -> RecRead # "none" /\ recDigest = digest
      [] OTHER                   -> RecRead = "clean" /\ recDigest = digest

\* the truth the gate must agree with: the cards now were checked clean in this epoch
CardsChecked == rec = "clean" /\ recFresh /\ recDigest = digest

\* the derived state (Machine.Ready, MachineLine)
ShownOf(r, s, e) == IF r THEN "RUNNING" ELSE IF s # e THEN "READY" ELSE "STOPPED"
Shown == ShownOf(run, stamp, epoch)

TypeOK ==
    /\ run \in BOOLEAN
    /\ stamp \in -1..MaxEpoch
    /\ epoch \in 0..MaxEpoch
    /\ clearing \in BOOLEAN
    /\ ran \in BOOLEAN
    /\ due \in BOOLEAN
    /\ judged \in BOOLEAN
    /\ prev \in States
    /\ last \in Acts
    /\ working \in [Settings -> BOOLEAN]
    /\ setBy \in [Settings -> Writers]
    /\ readersUp \in BOOLEAN
    /\ rec \in Records
    /\ recDigest \in Digests
    /\ recFresh \in BOOLEAN
    /\ digest \in Digests
    /\ defectOf \in [Digests -> BOOLEAN]

\* the first epoch's settings are whatever init left: any value, by no set
Init ==
    /\ run = FALSE
    /\ stamp = -1
    /\ epoch = 0
    /\ clearing = FALSE
    /\ ran = FALSE
    /\ due = FALSE
    /\ judged = FALSE
    /\ prev = "READY"
    /\ last = "init"
    /\ working \in [Settings -> BOOLEAN]
    /\ setBy = [s \in Settings |-> "clear"]
    /\ readersUp \in BOOLEAN
    /\ rec = "none"
    /\ recDigest = 0
    /\ recFresh = FALSE
    /\ digest = 0
    /\ defectOf \in [Digests -> BOOLEAN]

\* the stop's stamp (machine_transition.go): a stop of a RUNNING machine records the
\* epoch it ran in; the reversed witness leaves the stamp as it was
StopStamp == IF Broken = "nostopstamp" THEN stamp ELSE epoch

\* clear's first step (clear.go SetMachine false): stops a RUNNING machine
ClearStop ==
    /\ ~clearing
    /\ epoch < MaxEpoch
    /\ clearing' = TRUE
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE stamp
    /\ prev' = Shown
    /\ last' = "clearstop"
    /\ UNCHANGED <<epoch, ran, due, judged, settings, cards>>

\* clear's second step (clear.go AdvanceEpoch): the new epoch has no work; a start
\* that landed after ClearStop runs on into it. The new epoch's settings are any value
\* by no set: the clear writes the working defaults (clear-sets-every-working-default),
\* but a store an older binary cleared, or a column added later, may leave one
\* non-working; the gate is the second wall and must hold whatever the clear left.
ClearAdvance ==
    /\ clearing
    /\ clearing' = FALSE
    /\ epoch' = epoch + 1
    /\ stamp' = IF Broken = "clearstopped" THEN epoch + 1 ELSE stamp
    /\ ran' = run
    /\ due' = FALSE
    /\ judged' = FALSE
    /\ prev' = Shown
    /\ last' = "clearadvance"
    /\ working' \in [Settings -> BOOLEAN]
    /\ setBy' = [s \in Settings |-> "clear"]
    \* the new epoch's cards: none yet, then whatever is added; the old epoch's record stays
    \* in the store and is read as none (its Epoch is the old one)
    /\ recFresh' = FALSE
    /\ digest' = 0
    /\ defectOf' \in [Digests -> BOOLEAN]
    /\ UNCHANGED <<run, readersUp, rec, recDigest>>

\* start (store.SetMachine true): READY or STOPPED to RUNNING, recording the epoch
\* it reads; it may land between ClearStop and ClearAdvance. Enabled only through
\* the settings gate (run.go startSettingsGate, before setMachine writes anything) and
\* the card check gate (run.go startCardCheckGate).
Start ==
    /\ ~run
    /\ StartGate
    /\ CardGate
    /\ run' = TRUE
    /\ stamp' = epoch
    /\ ran' = TRUE
    /\ judged' = FALSE
    /\ prev' = Shown
    /\ last' = "start"
    /\ UNCHANGED <<epoch, clearing, due, settings, cards>>

\* start refused by the gate: one START REFUSED line per finding, exit 1, nothing written
StartRefusedSettings ==
    /\ ~run
    /\ ~StartGate
    /\ prev' = Shown
    /\ last' = "startrefused"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, cards>>

\* start refused by the card check gate: no record of this epoch, a failed one, or a clean
\* one whose digest is not the cards' now; exit 1, nothing written
StartRefusedCheck ==
    /\ ~run
    /\ ~CardGate
    /\ prev' = Shown
    /\ last' = "startunchecked"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, cards>>

\* nova-sprint check cards: the core kinds over the cards now, and one record of this
\* epoch written, clean or failed, whatever the machine's state (a failed run overwrites
\* a clean one)
Check ==
    /\ rec' = IF defectOf[digest] THEN "failed" ELSE "clean"
    /\ recDigest' = digest
    /\ recFresh' = TRUE
    /\ prev' = Shown
    /\ last' = "check"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, digest, defectOf>>

\* a change to the cards (an admit, a move, a brief, needs, priority or tier edit): the
\* digest moves; no verb touches the record, start recomputes the digest live
CardChange ==
    /\ digest' \in Digests \ {digest}
    /\ prev' = Shown
    /\ last' = "change"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, rec, recDigest, recFresh, defectOf>>

\* a setting written by anything but set: a migration, a hand edit of the table, an
\* older binary (the direct table write of TestStartRefusesEveryNonWorkingSettingNoSetWrote)
\* As the code reads set_by (sprint.SetByRecord): a set's choice holds while the value is
\* still the one the set wrote, so a write of the same value keeps it "set" and a write of
\* another value makes it "write". The model's value is its working bit, an abstraction:
\* in the code two different non-working values (fleet_tiers heavy, then frontier) differ
\* and the second is no set's choice; here both are FALSE and the choice holds. The gate
\* is only stricter in the code than in the model.
Write(s, v) ==
    /\ working' = [working EXCEPT ![s] = v]
    /\ setBy' = [setBy EXCEPT ![s] = IF setBy[s] = "set" /\ v = working[s] THEN "set" ELSE "write"]
    /\ prev' = Shown
    /\ last' = "write"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, readersUp, cards>>

\* nova-sprint set (the work table's change log names it): the owner's choice, working or not
SetSetting(s, v) ==
    /\ working' = [working EXCEPT ![s] = v]
    /\ setBy' = [setBy EXCEPT ![s] = "set"]
    /\ prev' = Shown
    /\ last' = "set"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, readersUp, cards>>

\* stop by hand (store.StopUntil), and the tick's done and funds stops
\* (stopWithCause): RUNNING to STOPPED
Stop ==
    /\ \/ run
       \/ Broken = "stopready" /\ Shown = "READY"
    /\ run' = FALSE
    /\ stamp' = IF run THEN StopStamp ELSE epoch
    /\ prev' = Shown
    /\ last' = "stop"
    /\ UNCHANGED <<epoch, clearing, ran, due, judged, settings, cards>>

\* a stop of a READY machine is refused and changes nothing
\* (machine_transition.go machineRecord, store.ErrNothingToStop)
StopRefused ==
    /\ Broken # "stopready"
    /\ Shown = "READY"
    /\ prev' = Shown
    /\ last' = "refused"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, settings, cards>>

\* work added (add): moves are due
AddWork ==
    /\ due' = TRUE
    /\ prev' = Shown
    /\ last' = "add"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, judged, settings, cards>>

\* a tick: a RUNNING machine moves what is due; every tick's read computes the
\* machine judgment (machineGroups)
Tick ==
    /\ due' = (due /\ ~run)
    /\ judged' = (due' /\ (Shown = "STOPPED" \/ (Broken = "readyjudges" /\ Shown = "READY")))
    /\ prev' = Shown
    /\ last' = "tick"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, settings, cards>>

\* the fleet's live state changes: a reader comes up or goes down (the readers' beats)
ReadersChange ==
    /\ readersUp' = ~readersUp
    /\ prev' = Shown
    /\ last' = "readers"
    /\ UNCHANGED <<run, stamp, epoch, clearing, ran, due, judged, working, setBy, cards>>

Next == ClearStop \/ ClearAdvance \/ Start \/ Stop \/ StopRefused \/ AddWork \/ Tick
        \/ StartRefusedSettings
        \/ StartRefusedCheck \/ Check \/ CardChange
        \/ ReadersChange
        \/ \E s \in Settings, v \in BOOLEAN : Write(s, v) \/ SetSetting(s, v)

Spec == Init /\ [][Next]_vars

---------------------------------------------------------------------------
\* Invariants.

\* a clear always leaves READY, unless a start landed inside it (then RUNNING)
ClearLeavesReady == (last = "clearadvance" /\ ~run) => Shown = "READY"

\* READY never raises the stopped judgment
ReadyNeverJudges == Shown = "READY" => ~judged

\* STOPPED is reachable only from RUNNING (a stop of READY is refused)
StoppedOnlyFromRunning == (Shown = "STOPPED" /\ prev # "STOPPED") => prev = "RUNNING"

\* STOPPED keeps its judgment as before
StoppedWithDueJudges == (last = "tick" /\ Shown = "STOPPED" /\ due) => judged

\* a machine that ran in this epoch and is not running is STOPPED, never READY
RanIsNotReady == (ran /\ ~run) => Shown = "STOPPED"

\* the machine never becomes RUNNING with a non-working setting no set of this epoch
\* chose: every start passed the gate (a value changed after the start is outside it)
StartOnlyWorking == last = "start" => Gate

\* the gate never over-refuses: a machine not running whose every non-working setting a
\* set of this epoch chose may start (with its START WARN lines), whatever readers are up,
\* once a clean check of the cards now is recorded
DeliberateSetStarts == (~run /\ Gate /\ CardGate) => ENABLED Start

\* the machine never becomes RUNNING over cards no clean check of this epoch matches: no
\* record after a clear, a failed record, or a change after the clean check is refused
\* (start-refuses-until-a-clean-check)
StartOnlyChecked == last = "start" => CardsChecked

\* a clean record that matches the cards is true of them: they hold no finding
CleanRecordHasNoDefect == CardsChecked => ~defectOf[digest]

=============================================================================
