------------------------------ MODULE MemberPegged ------------------------------
\* A member pegged with no card working (internal/sprint/pegged.go, tickPegged, run
\* in the presence part of the fleet's update). Found 2026-10-12: a member beat at
\* 100% for 13 hours with no card working, and no judgment was raised. A member up
\* whose beats say HeavyLoadPercent or more with no card working on its row is
\* pegged; the tick stamps the episode's start (the fleet property pegged_since.<m>)
\* the first tick it sees it pegged and removes the stamp the first tick it does not.
\* Once the stamp is the pegged bound old, the tick raises one judgment
\* (member-pegged) through notify: written once, never again while the episode
\* stands, closed when it ends (the load falls under, a card starts working there,
\* the member is held or down). A second episode raises again. The coordinator's
\* wait on the judgment, run out while the episode stands, closes it and the tick
\* raises it again: that is the coordinator's re-arm, so it starts the count over.
\*
\* The state, per member: status (up, held, down), load (low under
\* HeavyLoadPercent, high at or above), working (a card works on its row), since
\* (none: no stamp; fresh: stamped, under the bound; bound: stamped the bound ago),
\* open (its judgment is open, an ack included: notify holds an acked judgment as
\* open), waiting (the coordinator waits on it), raised (judgments written since
\* the episode began or the last wait ran out), ever (a judgment was ever written
\* for it, for the "once" witness), and ticked (the last step was the tick).
\*
\* The design, Broken = {}:
\*   Beat       the member beats a load, low or high.
\*   Start      a card starts working on its row; Finish ends it.
\*   Hold       the coordinator holds it; Unhold releases it.
\*   Lapse      it stops beating and goes down; Back beats again and is up.
\*   Elapse     the pegged bound passes on a stamped episode.
\*   Wait       the coordinator waits on the open judgment; WaitOut runs it out:
\*              the judgment closes (raised again if the episode stands).
\*   Tick       per member: not pegged removes the stamp and closes the judgment;
\*              pegged with no stamp stamps it (a new episode, its count at 0);
\*              pegged the bound with nothing open raises one judgment.
\* Reversed witnesses, each caught by one property below:
\*   "every"    the tick raises every tick the episode stands, open or not:
\*              OneJudgmentPerPeggedEpisode
\*   "nobound"  the tick raises on the stamp, before the bound: RaisedOnlyPastBound
\*   "stale"    the tick keeps the judgment open after the episode ends:
\*              ClosedWhenNotPegged
\*   "once"     the tick raises a member's first episode only: SecondEpisodeRaised
\*   "held"     the tick counts a held member's beats: RaisedOnlyPastBound
EXTENDS Naturals

CONSTANTS Members, Broken

VARIABLES status, load, working, since, open, waiting, raised, ever, ticked
vars == <<status, load, working, since, open, waiting, raised, ever, ticked>>

TypeOK ==
    /\ status \in [Members -> {"up", "held", "down"}]
    /\ load \in [Members -> {"low", "high"}]
    /\ working \in [Members -> BOOLEAN]
    /\ since \in [Members -> {"none", "fresh", "bound"}]
    /\ open \in [Members -> BOOLEAN]
    /\ waiting \in [Members -> BOOLEAN]
    /\ raised \in [Members -> Nat]
    /\ ever \in [Members -> BOOLEAN]
    /\ ticked \in BOOLEAN

Init ==
    /\ status = [m \in Members |-> "up"]
    /\ load = [m \in Members |-> "low"]
    /\ working = [m \in Members |-> FALSE]
    /\ since = [m \in Members |-> "none"]
    /\ open = [m \in Members |-> FALSE]
    /\ waiting = [m \in Members |-> FALSE]
    /\ raised = [m \in Members |-> 0]
    /\ ever = [m \in Members |-> FALSE]
    /\ ticked = FALSE

\* pegged.go PeggedNow: up, its load at HeavyLoadPercent or more, no card working
\* (the "held" witness counts a held member as up)
Up(m) == status[m] = "up" \/ ("held" \in Broken /\ status[m] = "held")
Pegged(m) == Up(m) /\ load[m] = "high" /\ ~working[m]

Env(m, s, l, w) ==
    /\ status' = [status EXCEPT ![m] = s]
    /\ load' = [load EXCEPT ![m] = l]
    /\ working' = [working EXCEPT ![m] = w]
    /\ ticked' = FALSE
    /\ UNCHANGED <<since, open, waiting, raised, ever>>

Beat(m, l) == status[m] # "down" /\ load[m] # l /\ Env(m, status[m], l, working[m])
Start(m) == status[m] = "up" /\ ~working[m] /\ Env(m, status[m], load[m], TRUE)
Finish(m) == working[m] /\ Env(m, status[m], load[m], FALSE)
Hold(m) == status[m] = "up" /\ Env(m, "held", load[m], working[m])
Unhold(m) == status[m] = "held" /\ Env(m, "up", load[m], working[m])
Lapse(m) == status[m] = "up" /\ Env(m, "down", load[m], working[m])
Back(m) == status[m] = "down" /\ Env(m, "up", load[m], working[m])

Elapse(m) ==
    /\ since[m] = "fresh"
    /\ since' = [since EXCEPT ![m] = "bound"]
    /\ ticked' = FALSE
    /\ UNCHANGED <<status, load, working, open, waiting, raised, ever>>

Wait(m) ==
    /\ open[m] /\ ~waiting[m]
    /\ waiting' = [waiting EXCEPT ![m] = TRUE]
    /\ ticked' = FALSE
    /\ UNCHANGED <<status, load, working, since, open, raised, ever>>

\* notify: a wait run out closes its judgment; the condition, when it stands, is
\* raised again (the coordinator's re-arm: the count starts over)
WaitOut(m) ==
    /\ waiting[m]
    /\ waiting' = [waiting EXCEPT ![m] = FALSE]
    /\ open' = [open EXCEPT ![m] = FALSE]
    /\ raised' = [raised EXCEPT ![m] = 0]
    /\ ticked' = FALSE
    /\ UNCHANGED <<status, load, working, since, ever>>

\* the condition notify is given: pegged, its stamp the bound old (the "nobound"
\* witness raises on a fresh stamp)
Stands(m) == Pegged(m) /\ (since[m] = "bound" \/ ("nobound" \in Broken /\ since[m] = "fresh"))

\* notify writes a judgment for a condition with none open ("every": whatever is
\* open; "once": only if none was ever written for the member)
Writes(m) ==
    /\ Stands(m)
    /\ ("every" \in Broken \/ ~open[m])
    /\ ("once" \notin Broken \/ ~ever[m])

\* tickPegged over every member at once: the stamp, then notify's write and close
Tick ==
    /\ since' = [m \in Members |->
                   IF ~Pegged(m) THEN "none"
                   ELSE IF since[m] = "none" THEN "fresh"
                   ELSE since[m]]
    /\ open' = [m \in Members |->
                  IF Writes(m) THEN TRUE
                  ELSE IF ~Pegged(m) /\ "stale" \notin Broken THEN FALSE
                  ELSE open[m]]
    /\ waiting' = [m \in Members |-> IF ~Pegged(m) /\ "stale" \notin Broken THEN FALSE ELSE waiting[m]]
    /\ raised' = [m \in Members |->
                    IF Pegged(m) /\ since[m] = "none" THEN 0
                    ELSE IF Writes(m) THEN raised[m] + 1
                    ELSE raised[m]]
    /\ ever' = [m \in Members |-> ever[m] \/ Writes(m)]
    /\ ticked' = TRUE
    /\ UNCHANGED <<status, load, working>>

Next ==
    \/ \E m \in Members, l \in {"low", "high"} : Beat(m, l)
    \/ \E m \in Members : Start(m) \/ Finish(m) \/ Hold(m) \/ Unhold(m) \/ Lapse(m) \/ Back(m)
    \/ \E m \in Members : Elapse(m) \/ Wait(m) \/ WaitOut(m)
    \/ Tick

Spec == Init /\ [][Next]_vars /\ WF_vars(Tick) /\ \A m \in Members : WF_vars(Elapse(m))

\* a bounded run: the "every" witness counts past one, and the check needs a bound
Bounded == \A m \in Members : raised[m] <= 2

\* ---------------------------------------------------------------- the rules

\* one judgment per pegged episode: between an episode's start (or the
\* coordinator's wait running out) and its end, the tick writes at most one
OneJudgmentPerPeggedEpisode == \A m \in Members : raised[m] <= 1

\* after the tick, an open judgment is of a member pegged the bound: never on the
\* stamp alone, never on a held or down member
RaisedOnlyPastBound ==
    ticked => \A m \in Members : open[m] => (status[m] = "up" /\ since[m] = "bound")

\* after the tick, a member no longer pegged has no judgment open
ClosedWhenNotPegged == ticked => \A m \in Members : ~Pegged(m) => ~open[m]

\* every episode that reaches the bound is told: the judgment opens, or the episode
\* ends first. A second episode is no exception.
SecondEpisodeRaised ==
    \A m \in Members : [](Pegged(m) /\ since[m] = "bound" => <>(open[m] \/ ~Pegged(m)))

=============================================================================
