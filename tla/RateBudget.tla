------------------------------ MODULE RateBudget ------------------------------
\* The sprint's global rate limiter (pkg/ratebudget, internal/sprint/store/rate.go,
\* cmd/nova-sprint/rate_budget.go; the owner, 2026-10-09: "The per-model pacing budget
\* should be global btw. across all fleet machines"; 2026-10-11: "the rate limited should
\* just stall out delay, not fail", "i don't want this rate limiter causing errors").
\*
\* Callers on any machines take one shared budget in the store before each model
\* request, through the server's verb budget take:
\*
\*   Slot(c)    the provider's concurrency slot: one atomic take of a lease (WATCH on the
\*              lease set, MULTI/EXEC only while fewer than C are live or c holds one);
\*   Read(c)    the WATCH read of the model's window: how many grants it holds now;
\*   Write(c)   the MULTI/EXEC: a grant recorded only while the count c read is under N
\*              AND the window did not move since c read it (dirty: another caller
\*              wrote); a lost race reads again; a spent window releases c's slot and
\*              c waits (it stalls, never fails);
\*   Release(c) the request ended: its slot is freed;
\*   Refused(c) the provider refused the request with a 429 anyway: the request waits
\*              and is retried in place (the member's backoff), its slot freed; nothing
\*              is spent (no attempt, no route rest: the sprint's side is Go-tested);
\*   Crash(c)   the holder dies holding its slot; Recover(c) once its lease expired;
\*   Tick       time passes: every grant ages one step (one leaves the window at age
\*              Win) and the leases of live holders are renewed, a dead holder's run
\*              down (LeaseTTL);
\*   Down, Up   the store stops or starts answering; OpenGo(c) a waiting caller whose
\*              store does not answer proceeds unlimited (open): the limiter is never a
\*              source of errors, and a grant it could not record is outside its count.
\*
\* Time is ages, not clocks: win[a] is the grants made a ticks ago, so the window is the
\* last Win ticks wherever "now" is and the state space is finite under every behaviour
\* (the liveness properties need no clock bound).
\*
\* Properties: NeverOverRate (at most N recorded grants in any Win ticks),
\* NeverOverConcurrent (at most C requests holding a slot), InFlightHeld (every request
\* in flight that the store granted holds a live lease), StalledIsGranted (a waiting
\* caller is eventually in flight, or died), SlotReclaimed (a dead holder's slot frees).
\* Fairness: the store is fair to a caller that keeps asking (strong fairness on its
\* take when the budget has room): that is the assumption the Waiter's polling makes.
\*
\* Reversed witnesses (Broken): "race" writes without the WATCH guard (a read and an
\* unguarded write) and breaks NeverOverRate; "norenew" lets a live holder's lease run
\* out and breaks NeverOverConcurrent; "noexpiry" keeps a dead holder's lease forever and
\* breaks SlotReclaimed.
EXTENDS Naturals, FiniteSets

\* MaxWants bounds each caller's requests (0: unbounded): the liveness instance's, so that
\* every other caller's demand ends (see StalledIsGranted).
CONSTANTS Callers, N, C, Win, TTL, Broken, MaxWants

VARIABLES st, win, lease, dirty, seen, down, opened, wants
vars == <<st, win, lease, dirty, seen, down, opened, wants>>

States == {"idle", "waiting", "slotted", "read", "inflight", "crashed"}
Holding == {"slotted", "read", "inflight"}
Ages == 0..(Win - 1)

\* the grants the store holds in the window now (ZCOUNT of the last Win ticks)
RECURSIVE SumTo(_)
SumTo(i) == IF i = 0 THEN 0 ELSE SumTo(i - 1) + win[i - 1]
InWin == SumTo(Win)

Live == {c \in Callers : lease[c] > 0}

TypeOK ==
    /\ st \in [Callers -> States]
    /\ win \in [Ages -> 0..(N + 2 * Cardinality(Callers))]
    /\ lease \in [Callers -> 0..TTL]
    /\ dirty \in [Callers -> BOOLEAN]
    /\ seen \in [Callers -> 0..(N + 2 * Cardinality(Callers))]
    /\ down \in BOOLEAN
    /\ opened \in [Callers -> BOOLEAN]
    /\ wants \in [Callers -> Nat]

Init ==
    /\ st = [c \in Callers |-> "idle"]
    /\ win = [a \in Ages |-> 0]
    /\ lease = [c \in Callers |-> 0]
    /\ dirty = [c \in Callers |-> FALSE]
    /\ seen = [c \in Callers |-> 0]
    /\ down = FALSE
    /\ opened = [c \in Callers |-> FALSE]
    /\ wants = [c \in Callers |-> 0]

Want(c) ==
    /\ st[c] = "idle"
    /\ MaxWants = 0 \/ wants[c] < MaxWants
    /\ st' = [st EXCEPT ![c] = "waiting"]
    /\ wants' = IF MaxWants = 0 THEN wants ELSE [wants EXCEPT ![c] = @ + 1]
    /\ UNCHANGED <<win, lease, dirty, seen, down, opened>>

\* one atomic take of a slot (the lease set's WATCH/MULTI retries until it commits)
Slot(c) ==
    /\ st[c] = "waiting"
    /\ ~down
    /\ Cardinality(Live) < C
    /\ lease' = [lease EXCEPT ![c] = TTL]
    /\ st' = [st EXCEPT ![c] = "slotted"]
    /\ UNCHANGED <<win, dirty, seen, down, opened, wants>>

Read(c) ==
    /\ st[c] = "slotted"
    /\ ~down
    /\ seen' = [seen EXCEPT ![c] = IF InWin < N THEN InWin ELSE N]
    /\ dirty' = [dirty EXCEPT ![c] = FALSE]
    /\ st' = [st EXCEPT ![c] = "read"]
    /\ UNCHANGED <<win, lease, down, opened, wants>>

Write(c) ==
    /\ st[c] = "read"
    /\ ~down
    /\ IF (~dirty[c] \/ "race" \in Broken) /\ seen[c] < N
         THEN \* EXEC commits: one grant now; every other open WATCH is dirty
              /\ win' = [win EXCEPT ![0] = @ + 1]
              /\ dirty' = [d \in Callers |-> IF d = c THEN FALSE ELSE IF st[d] = "read" THEN TRUE ELSE dirty[d]]
              /\ seen' = [seen EXCEPT ![c] = 0]
              /\ st' = [st EXCEPT ![c] = "inflight"]
              /\ UNCHANGED <<lease, down, opened, wants>>
       ELSE IF dirty[c]
         THEN \* EXEC aborted: the window moved; read again
              /\ st' = [st EXCEPT ![c] = "slotted"]
              /\ dirty' = [dirty EXCEPT ![c] = FALSE]
              /\ seen' = [seen EXCEPT ![c] = 0]
              /\ UNCHANGED <<win, lease, down, opened, wants>>
         ELSE \* the window is spent: the slot is released and the request waits
              /\ st' = [st EXCEPT ![c] = "waiting"]
              /\ lease' = [lease EXCEPT ![c] = 0]
              /\ seen' = [seen EXCEPT ![c] = 0]
              /\ UNCHANGED <<win, dirty, down, opened, wants>>

Release(c) ==
    /\ st[c] = "inflight"
    /\ st' = [st EXCEPT ![c] = "idle"]
    /\ lease' = [lease EXCEPT ![c] = 0]
    /\ opened' = [opened EXCEPT ![c] = FALSE]
    /\ UNCHANGED <<win, dirty, seen, down, wants>>

\* a 429 that got through: retried in place after a backoff, nothing spent
\* (a retry is one more request: with MaxWants set the provider's refusals end too)
Refused(c) ==
    /\ st[c] = "inflight"
    /\ MaxWants = 0 \/ wants[c] < MaxWants
    /\ st' = [st EXCEPT ![c] = "waiting"]
    /\ lease' = [lease EXCEPT ![c] = 0]
    /\ opened' = [opened EXCEPT ![c] = FALSE]
    /\ wants' = IF MaxWants = 0 THEN wants ELSE [wants EXCEPT ![c] = @ + 1]
    /\ UNCHANGED <<win, dirty, seen, down>>

Crash(c) ==
    /\ st[c] \in Holding
    /\ st' = [st EXCEPT ![c] = "crashed"]
    /\ dirty' = [dirty EXCEPT ![c] = FALSE]
    /\ seen' = [seen EXCEPT ![c] = 0]
    /\ UNCHANGED <<win, lease, down, opened, wants>>

Recover(c) ==
    /\ st[c] = "crashed"
    /\ lease[c] = 0
    /\ st' = [st EXCEPT ![c] = "idle"]
    /\ opened' = [opened EXCEPT ![c] = FALSE]
    /\ UNCHANGED <<win, lease, dirty, seen, down, wants>>

Tick ==
    /\ win' = [a \in Ages |-> IF a = 0 THEN 0 ELSE win[a - 1]]
    /\ lease' = [c \in Callers |->
                   IF st[c] \in Holding /\ ~opened[c] /\ "norenew" \notin Broken THEN TTL
                   ELSE IF st[c] = "crashed" /\ "noexpiry" \in Broken THEN lease[c]
                   ELSE IF lease[c] > 0 THEN lease[c] - 1 ELSE 0]
    /\ UNCHANGED <<st, dirty, seen, down, opened, wants>>

Down == ~down /\ down' = TRUE /\ UNCHANGED <<st, win, lease, dirty, seen, opened, wants>>
Up == down /\ down' = FALSE /\ UNCHANGED <<st, win, lease, dirty, seen, opened, wants>>

\* the store does not answer: the caller proceeds unlimited, one line logged
OpenGo(c) ==
    /\ st[c] \in {"waiting", "slotted", "read"}
    /\ down
    /\ st' = [st EXCEPT ![c] = "inflight"]
    /\ opened' = [opened EXCEPT ![c] = TRUE]
    /\ dirty' = [dirty EXCEPT ![c] = FALSE]
    /\ seen' = [seen EXCEPT ![c] = 0]
    \* an open request holds no slot it can renew: one it took before the store went
    \* down runs out by its lease (Tick renews no open request's)
    /\ UNCHANGED <<win, lease, down, wants>>

Next ==
    \/ Tick \/ Down \/ Up
    \/ \E c \in Callers :
         \/ Want(c) \/ Slot(c) \/ Read(c) \/ Write(c) \/ Release(c) \/ Refused(c)
         \/ Crash(c) \/ Recover(c) \/ OpenGo(c)

\* the store is fair to a caller that keeps asking: a take that can commit infinitely
\* often commits (the Waiter polls every MaxPoll at most). TLC's first liveness run found
\* the schedule this rules out (2026-10-11): two callers and one slot, the other caller
\* always taking the slot while the window has room and the stalled one only while it is
\* spent, forever; no FIFO is kept, so the assumption is the poller's, not the store's.
SlotRoom(c) == Slot(c) /\ InWin < N
ReadRoom(c) == Read(c) /\ InWin < N
WriteClean(c) == Write(c) /\ ~dirty[c] /\ seen[c] < N

Fairness ==
    /\ WF_vars(Tick)
    /\ \A c \in Callers :
         /\ SF_vars(Slot(c)) /\ SF_vars(SlotRoom(c)) /\ SF_vars(ReadRoom(c)) /\ SF_vars(WriteClean(c))
         /\ WF_vars(Write(c)) /\ WF_vars(Release(c)) /\ WF_vars(Recover(c))
         /\ SF_vars(OpenGo(c))

Spec == Init /\ [][Next]_vars /\ Fairness

\* at most N grants the store recorded in any Win ticks
NeverOverRate == InWin <= N

\* at most C requests hold a slot the store granted (an open request holds none)
NeverOverConcurrent == Cardinality({c \in Callers : st[c] \in Holding /\ ~opened[c]}) <= C

\* every request the store granted holds a live lease while it is in flight
InFlightHeld == \A c \in Callers : st[c] \in Holding /\ ~opened[c] => lease[c] > 0

\* the budget never stalls everyone: while any caller waits, some caller is eventually in
\* flight (granted or open), or no one waits (a waiter that died is no longer waiting). It
\* holds with every caller asking forever and the provider refusing forever.
Progress == (\E c \in Callers : st[c] = "waiting") ~>
              (\E c \in Callers : st[c] = "inflight") \/ (\A c \in Callers : st[c] # "waiting")

\* a waiting caller is eventually in flight (granted, or open), unless it died. Checked
\* with each caller's requests bounded (MaxWants): the store keeps no queue, so a caller
\* whose rivals ask forever and always ask first can be passed over forever (TLC's first
\* runs, 2026-10-11: one slot, one grant a window, the rival taking the slot just before
\* each window frees). Every rival's demand ends (a card ends), and then it is granted;
\* the waiter's jittered poll makes "always first" a schedule, not a law.
StalledIsGranted == \A c \in Callers : (st[c] = "waiting") ~> (st[c] \in {"inflight", "crashed"})

\* a dead holder's slot is reclaimed by its lease's expiry
SlotReclaimed == \A c \in Callers : (st[c] = "crashed") ~> (lease[c] = 0)

=============================================================================
