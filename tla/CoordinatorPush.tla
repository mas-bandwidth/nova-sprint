--------------------------- MODULE CoordinatorPush ---------------------------
\* The coordinator's push over the bus, and the sprint push the server holds for it in the
\* nova-hook record (cmd/nova-sprint/coordinator_push.go: coordinatorPass, coordFresh,
\* holdSprintPush; cmd/nova-sprint/coordinator_hook.go: the gate; docs/SPEC-SPRINT.md, "The
\* hook"). v1.3.6 retired the sprint's own seat hook (tla/SeatHook.tla) and seat push: the
\* coordinator hooks in with nova-hook, which owns the record (nova-tools tla/Hook.tla), and
\* the server sends every judgment and note for the coordinator to its name on the bus.
\*
\*   open      the delivery keys open now (a judgment's note, a note to the coordinator):
\*             raised and closed by the outside (the tick, the seat's answers).
\*   told      the keys the server's memory says it told the holder.
\*   sent      the (name, key) pairs that reached the bus (a ghost: what each name received).
\*   holder    the seat's holder.
\*   rec       the holder's nova-hook record as the gate reads it: "none", "hooked" (as the
\*             coordinator) or "unhooked".
\*   bySeat    the record was hooked by the seat's own word (a ghost: nova-hook hook).
\*   answered  the session answered the heartbeat's latest challenge (nova-hook prove).
\*   lease     the sprint push's lease is live (the server renews it).
\*   proof     the sprint push is proven.
\*   up        the server runs.
\*   busUp     the bus takes a send.
\*   failed    the server's last send to the holder failed.
\*
\* The design, Broken = "none": a look sends every open key not told to the holder, in one
\* message; the keys are told only once the send went; keys no longer open are forgotten; a
\* new holder starts from nothing told. The server renews the lease and proves the sprint
\* push only for a record hooked as the coordinator, and proves it only while its last send
\* went. It never arms, hooks or unhooks a record. A restart loses its memory.
\* Reversed witnesses, each broken by one property:
\*   "toldfirst"   the keys are told before the send goes: ToldWasSent.
\*   "keepold"     a new holder keeps the old holder's told keys: ToldWasSent.
\*   "arms"        the server arms a record that is not hooked: HookedOnlyByTheSeat.
\*   "provefailed" the server proves the sprint push after a failed send: GateMeansAWayIn.
\*   "resendsame" each look repeats an unchanged warning before ReminderEvery.
\* The reminder clock is elapsed minutes, saturated at ReminderEvery (30), not
\* an unbounded wall clock. open abstracts the warning-eligible judgment ID set;
\* fresh key delivery and warning delivery remain distinct sends. Resetting the
\* server/holder scopes its in-memory warning receipt, just as coordinatorPass.
EXTENDS Naturals, FiniteSets

CONSTANTS Keys, Names, Broken

ReminderEvery == 30
VARIABLES open, told, sent, holder, rec, bySeat, answered, lease, proof, up, busUp, failed,
          warned, warnAge, sameSetResent, missedChange
reminderVars == <<warned, warnAge, sameSetResent, missedChange>>
vars == <<open, told, sent, holder, rec, bySeat, answered, lease, proof, up, busUp, failed,
          warned, warnAge, sameSetResent, missedChange>>

TypeOK ==
  /\ open \subseteq Keys /\ told \subseteq Keys /\ sent \subseteq (Names \X Keys)
  /\ holder \in Names /\ rec \in {"none", "hooked", "unhooked"}
  /\ bySeat \in BOOLEAN /\ answered \in BOOLEAN /\ lease \in BOOLEAN /\ proof \in BOOLEAN
  /\ up \in BOOLEAN /\ busUp \in BOOLEAN /\ failed \in BOOLEAN
  /\ warned \subseteq Keys /\ warnAge \in 0..ReminderEvery
  /\ sameSetResent \in BOOLEAN /\ missedChange \in BOOLEAN

Init ==
  /\ open = {} /\ told = {} /\ sent = {}
  /\ holder \in Names /\ rec = "none" /\ bySeat = FALSE /\ answered = FALSE
  /\ lease = FALSE /\ proof = FALSE /\ up = TRUE /\ busUp = TRUE /\ failed = FALSE
  /\ warned = {} /\ warnAge = ReminderEvery
  /\ sameSetResent = FALSE /\ missedChange = FALSE

\* ---- the outside ----

\* A judgment or a note for the coordinator is raised, or answered.
Raise(k) == k \notin open /\ open' = open \cup {k} /\ UNCHANGED <<reminderVars, told, sent, holder, rec, bySeat, answered, lease, proof, up, busUp, failed>>
Close(k) == k \in open /\ open' = open \ {k} /\ UNCHANGED <<reminderVars, told, sent, holder, rec, bySeat, answered, lease, proof, up, busUp, failed>>

\* The bus takes sends, or does not.
BusFlip == busUp' = ~busUp /\ UNCHANGED <<reminderVars, open, told, sent, holder, rec, bySeat, answered, lease, proof, up, failed>>

\* nova-hook hook --as <holder> --role coordinator: a new epoch, nothing of the old one counts.
Hook ==
  /\ rec # "hooked"
  /\ rec' = "hooked" /\ bySeat' = TRUE /\ answered' = FALSE /\ lease' = FALSE /\ proof' = FALSE
  /\ UNCHANGED <<reminderVars, open, told, sent, holder, up, busUp, failed>>

\* nova-hook prove: only the model answers the heartbeat's challenge.
Prove == rec = "hooked" /\ ~answered /\ answered' = TRUE /\ UNCHANGED <<reminderVars, open, told, sent, holder, rec, bySeat, lease, proof, up, busUp, failed>>

\* nova-hook unhook, or two challenges missed in a row (nova-hook serve).
Unhook ==
  /\ rec = "hooked"
  /\ rec' = "unhooked" /\ answered' = FALSE /\ lease' = FALSE /\ proof' = FALSE
  /\ UNCHANGED <<reminderVars, open, told, sent, holder, bySeat, up, busUp, failed>>

\* The server stops: its lease lapses (LeaseTTL, here at once) and its memory is gone; it
\* starts again.
Stop == up /\ up' = FALSE /\ lease' = FALSE /\ proof' = FALSE /\ told' = {} /\ failed' = FALSE
        /\ warned' = {} /\ warnAge' = ReminderEvery
        /\ UNCHANGED <<sameSetResent, missedChange, open, sent, holder, rec, bySeat, answered, busUp>>
Start == ~up /\ up' = TRUE /\ UNCHANGED <<reminderVars, open, told, sent, holder, rec, bySeat, answered, lease, proof, busUp, failed>>

\* ---- the server's look (coordinatorPass) ----

\* What the look sends: every open key not told. A look that reads a new holder starts from
\* nothing told (here the told set is the holder's; "keepold" keeps it).
Fresh == open \ told
WarningDue == open # {} /\ (open # warned \/ warnAge = ReminderEvery \/ Broken = "resendsame")

Look ==
  /\ up
  /\ LET fresh == Fresh
         goes == fresh # {} /\ busUp
         toldNow == IF Broken = "toldfirst" THEN (told \cap open) \cup fresh
                    ELSE IF goes THEN (told \cap open) \cup fresh ELSE told \cap open
         warnDue == WarningDue
         warns == warnDue /\ busUp
         failedNow == IF fresh = {} /\ ~warnDue THEN failed ELSE ~busUp
     IN /\ told' = toldNow
        /\ sent' = IF goes THEN sent \cup ({holder} \X fresh) ELSE sent
        /\ failed' = failedNow
        /\ warned' = IF open = {} THEN {} ELSE IF warns THEN open ELSE warned
        /\ warnAge' = IF open = {} THEN ReminderEvery ELSE IF warns THEN 0 ELSE warnAge
        /\ sameSetResent' = (sameSetResent \/ (warns /\ open = warned /\ warnAge < ReminderEvery))
        /\ missedChange' = (missedChange \/ (busUp /\ open # {} /\ open # warned /\ ~warns))
        \* the hold (holdSprintPush): only a record hooked as the coordinator
        /\ IF rec = "hooked"
             THEN /\ lease' = TRUE
                  /\ proof' = IF Broken = "provefailed" THEN TRUE ELSE ~failedNow
                  /\ UNCHANGED <<rec, bySeat>>
             ELSE IF Broken = "arms"
               THEN rec' = "hooked" /\ lease' = TRUE /\ proof' = ~failedNow /\ UNCHANGED bySeat
               ELSE UNCHANGED <<rec, bySeat, lease, proof>>
  /\ UNCHANGED <<open, holder, answered, up, busUp>>

\* The seat moves (coordinator <name>) and the server's look reads the new holder: its record
\* is its own, not hooked yet, and the told keys were another name's.
Follow(n) ==
  /\ up /\ holder' = n /\ n # holder
  /\ told' = IF Broken = "keepold" THEN told ELSE {}
  /\ rec' = "none" /\ bySeat' = FALSE /\ answered' = FALSE /\ lease' = FALSE /\ proof' = FALSE
  /\ warned' = {} /\ warnAge' = ReminderEvery
  /\ UNCHANGED <<sameSetResent, missedChange, open, sent, up, busUp, failed>>

\* If the failed work resolves before retry, one real status send tests the
\* transport. It succeeds only while the bus accepts sends; healthy looks do
\* not probe. This is transport evidence, never session challenge evidence.
Recover == /\ up /\ failed /\ Fresh = {} /\ ~WarningDue /\ busUp /\ failed' = FALSE
           /\ UNCHANGED <<reminderVars, open, told, sent, holder, rec, bySeat, answered, lease, proof, up, busUp>>

\* One minute passes; saturation keeps the graph finite while preserving both
\* sides of the exact 30-minute boundary. Failed sends never reset this clock.
Minute == /\ warnAge < ReminderEvery /\ warnAge' = warnAge + 1
          /\ UNCHANGED <<open, told, sent, holder, rec, bySeat, answered, lease,
                         proof, up, busUp, failed, warned, sameSetResent, missedChange>>

Next ==
  \/ \E k \in Keys : Raise(k) \/ Close(k)
  \/ BusFlip \/ Hook \/ Prove \/ Unhook \/ Stop \/ Start \/ Look \/ Recover \/ Minute
  \/ \E n \in Names : Follow(n)

Spec == Init /\ [][Next]_vars
LiveSpec == Spec /\ WF_vars(Look)

\* ---- what it keeps ----

\* A successful warning of the same nonempty set never repeats before 30m;
\* a changed nonempty set is sent on the very next successful look.
SameSetNotResentWithin == ~sameSetResent
ChangedSetWarnedImmediately == ~missedChange

\* The gate's yes (hook.Status.Gate): hooked as the coordinator, the challenge answered,
\* and the sprint push live and proven.
Gate == rec = "hooked" /\ answered /\ lease /\ proof

\* Every key the server counts told reached the holder on the bus.
ToldWasSent == \A k \in told : <<holder, k>> \in sent

\* A record is hooked only by the seat's own word: the server joins, never arms.
HookedOnlyByTheSeat == rec = "hooked" => bySeat

\* The server holds a lease only for a record hooked as the coordinator.
LeaseOnlyForAHookedRecord == lease => rec = "hooked"

\* A coordinator command runs only while the server runs and its last send to the seat
\* went: the judgments have a way in.
GateMeansAWayIn == Gate => up /\ ~failed

\* An open key reaches the holder once the server runs, the bus takes sends and the seat
\* stays put: it is sent, or answered first.
OpenKeysReachTheHolder ==
  \A k \in Keys, n \in Names :
    <>[](up /\ busUp /\ holder = n) => [](k \in open => <>(k \notin open \/ <<n, k>> \in sent))
=============================================================================
