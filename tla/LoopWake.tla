------------------------------ MODULE LoopWake ------------------------------
\* One server loop that wakes on the sprint log (cmd/nova-sprint/landloop.go
\* landLoop, decidelane.go decideLoop, seat_silent.go tellSeatSilent after a
\* tick, and run.go pace, which already did): the loop blocks on Store.WaitLog
\* from the last line it has seen and runs its pass when a line arrives, and
\* its interval (LandEvery 2s, DecideEvery 5s, seatSilentLook 5s) stays as the
\* quiet-log backstop, so a push that was lost is still caught.
\*
\* The state: log, the number of lines committed (1..log are the lines);
\* cursor, the last line the loop has seen: every line up to it was handled by
\* a pass; mark, the tail WaitLog returned as it woke, the next cursor; handled,
\* the lines a pass has read (a pass reads the state whole, so it handles every
\* line committed before it reads); pc, whether the loop waits, or a pass has
\* been woken and has not read, or has read and not closed; timer, the wait's
\* clock, which a quiet log runs out at Interval; overtook, whether a backstop
\* ever ran while a line after the cursor stood and a wake could have taken it.
\*
\* The design, Broken = {}:
\*   Commit    a line is committed; it can come at any time, in a pass too.
\*   Tick      the wait's clock moves on.
\*   Wake      WaitLog returns, at once, a line after the cursor: mark is its
\*             tail, and the pass is woken.
\*   Backstop  the interval has passed with no line after the cursor (WaitLog
\*             returns a line that is already there at once, so the clock cannot
\*             overtake it): mark is the tail, the pass is woken.
\*   PassRead  the pass reads the state: every line committed so far is handled.
\*   PassEnd   the cursor is mark, the tail as the wait woke, before the pass
\*             read: a line committed during the pass stays after the cursor
\*             and wakes the next wait.
\* Constants: Lose, a push that is lost (Wake never returns, only the interval
\* wakes the loop), which the fair Backstop must make good; FairBackstop, whether
\* the interval is a fair action (off, the wake alone must carry every line).
\* Reversed witnesses:
\*   "clockonly"   Wake is removed and Backstop fires at any time: the loop
\*                 looks on the clock only (PushedWithinOneEvent).
\*   "cursorahead" the cursor is the tail read after the pass, past lines the
\*                 pass did not read (CursorNeverSkips, EveryChangeIsHandled).
EXTENDS Naturals

CONSTANTS MaxLines, Interval, Lose, FairBackstop, Broken

VARIABLES log, cursor, mark, handled, pc, timer, overtook
vars == <<log, cursor, mark, handled, pc, timer, overtook>>

TypeOK ==
    /\ log \in 0..MaxLines
    /\ cursor \in 0..MaxLines
    /\ mark \in 0..MaxLines
    /\ handled \in 0..MaxLines
    /\ pc \in {"wait", "woken", "read"}
    /\ timer \in 0..Interval
    /\ overtook \in BOOLEAN

Init ==
    /\ log = 0 /\ cursor = 0 /\ mark = 0 /\ handled = 0
    /\ pc = "wait" /\ timer = 0 /\ overtook = FALSE

Commit ==
    /\ log < MaxLines
    /\ log' = log + 1
    /\ UNCHANGED <<cursor, mark, handled, pc, timer, overtook>>

Tick ==
    /\ pc = "wait"
    /\ timer < Interval
    /\ timer' = timer + 1
    /\ UNCHANGED <<log, cursor, mark, handled, pc, overtook>>

\* WaitLog returns a line after the cursor
Wake ==
    /\ "clockonly" \notin Broken
    /\ ~Lose
    /\ pc = "wait"
    /\ log > cursor
    /\ pc' = "woken" /\ mark' = log /\ timer' = 0
    /\ UNCHANGED <<log, cursor, handled, overtook>>

\* the interval has passed; WaitLog would have returned a line that stood
Backstop ==
    /\ pc = "wait"
    /\ timer = Interval
    /\ ("clockonly" \in Broken \/ log = cursor \/ Lose)
    /\ pc' = "woken" /\ mark' = log /\ timer' = 0
    /\ overtook' = (overtook \/ (log > cursor /\ ~Lose))
    /\ UNCHANGED <<log, cursor, handled>>

PassRead ==
    /\ pc = "woken"
    /\ handled' = log
    /\ pc' = "read"
    /\ UNCHANGED <<log, cursor, mark, timer, overtook>>

PassEnd ==
    /\ pc = "read"
    /\ cursor' = IF "cursorahead" \in Broken THEN log ELSE mark
    /\ pc' = "wait"
    /\ UNCHANGED <<log, mark, handled, timer, overtook>>

Next == Commit \/ Tick \/ Wake \/ Backstop \/ PassRead \/ PassEnd

Spec == Init /\ [][Next]_vars
        /\ WF_vars(Tick) /\ WF_vars(Wake) /\ WF_vars(PassRead) /\ WF_vars(PassEnd)
        /\ (FairBackstop => WF_vars(Backstop))

\* no backstop ran while a line stood after the cursor and a wake was there to
\* take it
PushedWithinOneEvent == ~overtook

\* a loop at rest has handled every line up to its cursor
CursorNeverSkips == pc = "wait" => cursor <= handled

\* every committed line is eventually handled by a pass
EveryChangeIsHandled == \A l \in 1..MaxLines : (log >= l) ~> (handled >= l)
=============================================================================
