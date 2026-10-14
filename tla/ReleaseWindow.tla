-------------------------------- MODULE ReleaseWindow ----------------------------
\* nova-sprint's release-window sentinel (docs/SPEC-SPRINT.md, the release
\* window; cmd/nova-sprint/release_window.go ReadReleaseWindow): the seat play
\* writes one JSON file when the window opens and removes it when it ends or is
\* rolled back; a verb that fails to reach the store reads it once, after the
\* failure, and its refusal says whether the outage is the planned window
\* (open, now before until) or an expired sentinel (stale, now not before
\* until). The play may crash between open and close, leaving the file open
\* while time passes past until.
\*
\* The state: now, the clock; sentinel, the file (none or open); until, the
\* play's upper bound while the file is open; said, what the last verb refusal
\* said (nothing, planned or stale): the verdict of the most recent Read, reset
\* to nothing by every action that is not a Read (the verdict is fresh only
\* immediately after the verb reads, nothing intervenes between its read and
\* its refusal); play, whether the play is up to close the window. The outside:
\* the play opens and closes the window and can crash at any time, and the
\* clock moves. The verb's read never writes the file, so the sentinel changes
\* only by the play's Open and Close.
\*
\* The design, Broken = {}:
\*   Open      the play writes the file open, bound to a time strictly after
\*             now (the code never writes the file; the play does).
\*   Close     the play removes the file: the window ends.
\*   Crash     the play goes down between open and close; the file stays.
\*   Advance   the clock moves one unit; it never goes back.
\*   Read      a verb that failed to reach the store reads the file once and
\*             says planned when now is before until, stale when it is not,
\*             nothing when the file is none.
\* Reversed witness, caught by StaleNeverPlanned:
\*   "staleopen"  a reader that ignores until and says planned whenever the
\*             file is open: a refusal after until is worded planned.
EXTENDS Naturals

CONSTANTS MaxTime, Broken

VARIABLES now, sentinel, until, said, play
vars == <<now, sentinel, until, said, play>>

Window == {"none", "open"}

TypeOK ==
    /\ now \in 0..MaxTime
    /\ sentinel \in Window
    /\ until \in 0..MaxTime
    /\ said \in {"nothing", "planned", "stale"}
    /\ play \in BOOLEAN

Init ==
    /\ now = 0
    /\ sentinel = "none"
    /\ until = 0
    /\ said = "nothing"
    /\ play = TRUE

\* the play writes the file open, bound to a time strictly after now
Open(d) ==
    /\ play
    /\ sentinel = "none"
    /\ d \in (now + 1)..MaxTime
    /\ sentinel' = "open"
    /\ until' = d
    /\ said' = "nothing"
    /\ UNCHANGED <<now, play>>

\* the play removes the file: the window ends
Close ==
    /\ play
    /\ sentinel = "open"
    /\ sentinel' = "none"
    /\ said' = "nothing"
    /\ UNCHANGED <<now, until, play>>

\* the play crashes between open and close: the file stays, the play is gone
Crash ==
    /\ play
    /\ play' = FALSE
    /\ said' = "nothing"
    /\ UNCHANGED <<now, sentinel, until>>

\* what a verb that reads the file says, after its dial failed: planned only
\* while now is before until, stale once until is reached, nothing when the
\* file is none; the broken reader ignores until and says planned whenever the
\* file is open
ReadSays ==
    IF "staleopen" \in Broken
    THEN (IF sentinel = "open" THEN "planned" ELSE "nothing")
    ELSE (IF sentinel = "open"
          THEN (IF now < until THEN "planned" ELSE "stale")
          ELSE "nothing")

\* a verb that failed to reach the store reads the sentinel once, never before
Read ==
    /\ said' = ReadSays
    /\ UNCHANGED <<now, sentinel, until, play>>

Advance ==
    /\ now < MaxTime
    /\ now' = now + 1
    /\ said' = "nothing"
    /\ UNCHANGED <<sentinel, until, play>>

Next ==
    \/ \E d \in 0..MaxTime : Open(d)
    \/ Close
    \/ Crash
    \/ Read
    \/ Advance

Spec == Init /\ [][Next]_vars

\* ---------------------------------------------------------------- the rules

\* a refusal after until is never worded planned: an expired sentinel never
\* says the outage is the window (the safety property)
StaleNeverPlanned == (sentinel = "open" /\ now >= until) => said # "planned"

\* a refusal is worded planned only inside the window (now before until)
PlannedOnlyInsideWindow == (said = "planned") => (sentinel = "open" /\ now < until)

=============================================================================
