--------------------------- MODULE FriendSyncPass ---------------------------
\* One pass of friend sync and its loop (cmd/nova-sprint/friends.go friendSyncPass,
\* cmd/nova-sprint/friend_loop.go friendSyncLoop; docs/FRIENDS.md, "The friend sync
\* loop"). The owner, 2026-10-14: "never wait forever, mechanically": every wait has a
\* deadline on the sprint's clock and ends in a named refusal; the loop's next pass
\* retries. v1.4.6: the pass runs within friendSyncPassBound, read on the sprint's clock
\* between its stages and held as its context's deadline on every read and write.
\*
\*   hook    the coordinator's nova-hook record as the gate reads it: proven, or
\*           lapsed (expired, partial, live, unhooked, or proven without the
\*           heartbeat's evidence: every state the gate refuses). An outside event:
\*           it lapses and is proven again at any time, and may stay lapsed for ever.
\*   pass    where the loop's current pass stands: idle (between passes), gate (the
\*           store open and the coordinator's hook gate), work (its stages: the
\*           config read, the friends table, the starts, the deliveries and the
\*           collects), blocked (a stage that has not answered, and may never), done
\*           (synced) or refused (the gate's refusal, or the deadline's).
\*   age     the pass's time on the sprint's clock, in ticks, from its start to Bound.
\*   proven  the gate this pass passed read the record proven.
\*
\* Broken names the reversed witnesses: "nodeadline" (v1.4.5: the pass ran on
\* context.Background() with no deadline, so a stage that never answers holds the pass
\* for ever), and "skipgate" (a pass that works without the gate).

EXTENDS Naturals

CONSTANTS Bound, Broken

ASSUME Bound \in Nat \ {0}
ASSUME Broken \subseteq {"nodeadline", "skipgate"}

VARIABLES hook, pass, age, proven

vars == <<hook, pass, age, proven>>

InFlight == {"gate", "work", "blocked"}

TypeOK ==
    /\ hook \in {"proven", "lapsed"}
    /\ pass \in {"idle"} \cup InFlight \cup {"done", "refused"}
    /\ age \in 0..Bound
    /\ proven \in BOOLEAN

Init ==
    /\ hook \in {"proven", "lapsed"}
    /\ pass = "idle"
    /\ age = 0
    /\ proven = FALSE

\* the outside world: the session's hook lapses, and is hooked in and proven again
Lapse == hook = "proven" /\ hook' = "lapsed" /\ UNCHANGED <<pass, age, proven>>
Rearm == hook = "lapsed" /\ hook' = "proven" /\ UNCHANGED <<pass, age, proven>>

\* the loop starts a pass: its deadline starts at the sprint's clock now
Begin ==
    /\ pass = "idle"
    /\ pass' = IF "skipgate" \in Broken THEN "work" ELSE "gate"
    /\ age' = 0
    /\ proven' = FALSE
    /\ UNCHANGED hook

\* the gate (storeCtx -> coordinatorGate -> hookedIn -> coordinatorHookWhy): proven, on
GateOK ==
    /\ pass = "gate" /\ hook = "proven"
    /\ pass' = "work" /\ proven' = TRUE
    /\ UNCHANGED <<hook, age>>

\* not proven: refused at once by name (friendSyncPass, hookGateError: "the
\* coordinator's hook is not proven")
GateRefuse ==
    /\ pass = "gate" /\ hook = "lapsed"
    /\ pass' = "refused"
    /\ UNCHANGED <<hook, age, proven>>

\* a stage answers and the pass ends synced, or a stage stops answering
Work ==
    /\ pass = "work"
    /\ pass' \in {"done", "blocked"}
    /\ UNCHANGED <<hook, age, proven>>

\* a stage that stopped answering may answer again, and may never: no fairness
Answer ==
    /\ pass = "blocked"
    /\ pass' = "work"
    /\ UNCHANGED <<hook, age, proven>>

\* the sprint's clock moves while the pass is in flight
Tick ==
    /\ pass \in InFlight /\ age < Bound
    /\ age' = age + 1
    /\ UNCHANGED <<hook, pass, proven>>

\* the deadline: a pass in flight at Bound is refused, naming where it stood
\* (passDeadline.late, the context's deadline)
Deadline ==
    /\ "nodeadline" \notin Broken
    /\ pass \in InFlight /\ age = Bound
    /\ pass' = "refused"
    /\ UNCHANGED <<hook, age, proven>>

\* the loop's next pass: after --every, whatever the last one ended in
Retry ==
    /\ pass \in {"done", "refused"}
    /\ pass' = "idle" /\ age' = 0
    /\ UNCHANGED <<hook, proven>>

Next == Lapse \/ Rearm \/ Begin \/ GateOK \/ GateRefuse \/ Work \/ Answer \/ Tick \/ Deadline \/ Retry

Spec ==
    /\ Init /\ [][Next]_vars
    /\ WF_vars(Begin) /\ WF_vars(GateOK) /\ WF_vars(GateRefuse) /\ WF_vars(Work)
    /\ WF_vars(Tick) /\ WF_vars(Deadline) /\ WF_vars(Retry)

\* a pass works, and syncs, only past a gate that read the record proven
WorksOnlyProven == pass \in {"work", "blocked", "done"} => proven

\* every pass that starts ends: synced, or refused by name; never waits for ever
PassEnds == (pass \in InFlight) ~> (pass \in {"done", "refused"})

\* and the loop goes on: a next pass always comes
LoopGoesOn == []<>(pass = "idle")
=============================================================================
