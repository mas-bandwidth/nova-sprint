---------------------------- MODULE ProviderBudget ----------------------------
\* A member's environment budget (internal/sprint/steps_work.go, balance.go;
\* tla/ProviderBudget.tla). The owner, 2026-10-11, after b1, b2 and b3 joined the
\* fleet without the push credential and failed every card at push for over an
\* hour, each raised only as one card's "work came back failed": "the failure
\* here is not noticing this until now." A failure whose cause is the member's
\* environment -- a push refusal (no credential, auth, unreachable remote), a
\* missing tool or harness, a sandbox or wall refusal, a disk-full -- is the
\* member's, never the card's or the model's. It returns the card to ready
\* untouched (no attempt consumed, nothing counted toward its bound), holds the
\* member so no new card is dealt to it, and raises ONE judgment to the seat per
\* member, naming the member's current cause and the remedy that clears it. It is
\* the shape a provider's rest takes (RouteRest.tla): the provider's balance is
\* its budget; the member's environment is its.
\*
\* The state, for one member m: env[m], the member's last environment fault
\* (none, or a cause); held[m], the member is held by it and is dealt nothing;
\* judged[m], the environment judgment is open on the member; jcause[m], the
\* cause the open judgment names; known[m], the member has been accepted into the
\* fleet; probe[m], the member's own environment probe's last result (its
\* push-credential probe and its harness check); card[m], where the member's card
\* is (ready: dealt and untaken, working: taken, pushed: its next card pushed,
\* failed: a model or card failure); event, what the last step was.
\*
\* The design, Broken = {}:
\*   EnvFinish(m,c)  a card on m finishes with environment cause c: env[m]
\*                   becomes c, held[m] TRUE, judged[m] TRUE, jcause[m] becomes
\*                   c (the open judgment follows the member's current cause),
\*                   and the card returns to ready (no attempt spent).
\*   ModelFinish(m)  a card on m fails the model's way: nothing is held and no
\*                   environment judgment is raised; the card is failed work.
\*   Deal(m)         the deal gives m a card only while it is known and not held.
\*   Push(m)         m's next card pushes: env[m] clears, held[m] FALSE, judged[m]
\*                   FALSE, jcause[m] none. The fault clears itself.
\*   ProbePass(m)    fleet up runs m's own probe and it passes: env[m] clears,
\*                   held[m] FALSE, judged[m] FALSE. The remedy worked.
\*   ProbeFail(m)    the probe fails: the fault stands and fleet up refuses.
\*   ProbeNew(m,p)   a new member runs its own probe, p none (not run) or fail.
\*   Boot(m)         init sets a member up: its probe passed by the machine's own
\*                   setup (EnvProbePassed), so it is accepted.
\*   Accept(m)       fleet up accepts a new member only when its own probe passed.
\*   Refuse(m)       fleet up refuses a new member whose probe did not pass.
\* Reversed witnesses:
\*   "noenv"    a model failure holds the member: EnvOnlyByEnvironmentFinish
\*   "nodeal"   the deal gives a held member a card: HeldDealtNothing
\*   "stale"    a second, different cause leaves the judgment naming the old one:
\*              JudgmentNamesTheCause (per member and cause)
\*   "nopush"   a push never clears the fault: FaultClearsOnPush
\*   "clear"    fleet up accepts a member whose fault is not cleared:
\*              FleetUpRefusesUncleared
\*   "newup"    a new member is accepted with no probe or a failed one:
\*              NewMemberProbed
EXTENDS Naturals, FiniteSets

CONSTANTS Members, Causes, Broken

VARIABLES env, held, judged, jcause, known, probe, card, event
vars == <<env, held, judged, jcause, known, probe, card, event>>

Events == Causes \cup {"model", "deal", "push", "probe-pass", "probe-fail",
                      "probe-new", "boot", "accept", "refuse", "init"}

TypeOK ==
    /\ env \in [Members -> {"none"} \cup Causes]
    /\ held \in [Members -> BOOLEAN]
    /\ judged \in [Members -> BOOLEAN]
    /\ jcause \in [Members -> {"none"} \cup Causes]
    /\ known \in [Members -> BOOLEAN]
    /\ probe \in [Members -> {"none", "pass", "fail"}]
    /\ card \in [Members -> {"ready", "working", "pushed", "failed"}]
    /\ event \in Events

Init ==
    /\ env = [m \in Members |-> "none"]
    /\ held = [m \in Members |-> FALSE]
    /\ judged = [m \in Members |-> FALSE]
    /\ jcause = [m \in Members |-> "none"]
    /\ known = [m \in Members |-> FALSE]
    /\ probe = [m \in Members |-> "none"]
    /\ card = [m \in Members |-> "ready"]
    /\ event = "init"

Held(m) == held[m]

\* a card on m finishes with an environment fault cause c: the card returns to
\* ready untouched, the member is held, and the one judgment names c. A later
\* fault with a different cause rewrites jcause to the member's current cause,
\* unless the "stale" witness leaves it on the first.
EnvFinish(m, c) ==
    /\ c \in Causes
    /\ card[m] \in {"working", "ready"}  \* m may have another card in flight
    /\ env' = [env EXCEPT ![m] = c]
    /\ held' = [held EXCEPT ![m] = TRUE]
    /\ judged' = [judged EXCEPT ![m] = TRUE]
    /\ jcause' = [jcause EXCEPT ![m] = (IF "stale" \in Broken THEN jcause[m] ELSE c)]
    /\ card' = [card EXCEPT ![m] = "ready"]
    /\ event' = c
    /\ UNCHANGED <<known, probe>>

\* a card on m fails the model's way: not the environment's, so nothing is held
\* ("noenv": the broken rule holds it anyway)
ModelFinish(m) ==
    /\ card[m] = "working"
    /\ held' = [held EXCEPT ![m] = "noenv" \in Broken]
    /\ card' = [card EXCEPT ![m] = "failed"]
    /\ event' = "model"
    /\ UNCHANGED <<env, judged, jcause, known, probe>>

\* the deal gives a known, not-held member a card ("nodeal": a held one too)
Deal(m) ==
    /\ known[m]
    /\ (~held[m] \/ "nodeal" \in Broken)
    /\ card[m] = "ready"
    /\ card' = [card EXCEPT ![m] = "working"]
    /\ event' = "deal"
    /\ UNCHANGED <<env, held, judged, jcause, known, probe>>

\* a held member's card in flight pushes: the fault clears itself
\* ("nopush": the broken rule never clears it)
Push(m) ==
    /\ held[m]
    /\ card[m] \in {"working", "ready"}
    /\ \/ "nopush" \in Broken
          /\ UNCHANGED <<env, held, judged, jcause>>
       \/ "nopush" \notin Broken
          /\ env' = [env EXCEPT ![m] = "none"]
          /\ held' = [held EXCEPT ![m] = FALSE]
          /\ judged' = [judged EXCEPT ![m] = FALSE]
          /\ jcause' = [jcause EXCEPT ![m] = "none"]
    /\ card' = [card EXCEPT ![m] = "pushed"]
    /\ event' = "push"
    /\ UNCHANGED <<known, probe>>

\* fleet up runs the member's own probe: a pass clears the fault, a failure
\* leaves it and fleet up refuses
ProbePass(m) ==
    /\ held[m]
    /\ env' = [env EXCEPT ![m] = "none"]
    /\ held' = [held EXCEPT ![m] = FALSE]
    /\ judged' = [judged EXCEPT ![m] = FALSE]
    /\ jcause' = [jcause EXCEPT ![m] = "none"]
    /\ event' = "probe-pass"
    /\ UNCHANGED <<known, probe, card>>

\* "clear": the broken rule lets a failed probe clear the fault
ProbeFail(m) ==
    /\ held[m]
    /\ \/ "clear" \in Broken
          /\ env' = [env EXCEPT ![m] = "none"]
          /\ held' = [held EXCEPT ![m] = FALSE]
          /\ judged' = [judged EXCEPT ![m] = FALSE]
          /\ jcause' = [jcause EXCEPT ![m] = "none"]
       \/ "clear" \notin Broken
          /\ UNCHANGED <<env, held, judged, jcause>>
    /\ event' = "probe-fail"
    /\ UNCHANGED <<known, probe, card>>

\* a new member runs its own environment probe (the push-credential probe, the
\* harness check): p none or fail, since a pass is Boot or Accept
ProbeNew(m, p) ==
    /\ ~known[m]
    /\ p \in {"none", "fail"}
    /\ probe' = [probe EXCEPT ![m] = p]
    /\ event' = "probe-new"
    /\ UNCHANGED <<env, held, judged, jcause, known, card>>

\* init sets a member up: the machine's own setup ran its probe, so it passed
Boot(m) ==
    /\ ~known[m]
    /\ probe' = [probe EXCEPT ![m] = "pass"]
    /\ known' = [known EXCEPT ![m] = TRUE]
    /\ event' = "boot"
    /\ UNCHANGED <<env, held, judged, jcause, card>>

\* fleet up accepts a new member only when its own probe passed; the "newup"
\* witness accepts it with no probe or a failed one
Accept(m) ==
    /\ ~known[m]
    /\ (probe[m] = "pass" \/ "newup" \in Broken)
    /\ known' = [known EXCEPT ![m] = TRUE]
    /\ event' = "accept"
    /\ UNCHANGED <<env, held, judged, jcause, probe, card>>

\* fleet up refuses a new member whose own probe did not pass; nothing changes
Refuse(m) ==
    /\ ~known[m]
    /\ probe[m] # "pass"
    /\ event' = "refuse"
    /\ UNCHANGED <<env, held, judged, jcause, known, probe, card>>

Next ==
    \/ \E m \in Members, c \in Causes : EnvFinish(m, c)
    \/ \E m \in Members : ModelFinish(m)
    \/ \E m \in Members : Deal(m)
    \/ \E m \in Members : Push(m)
    \/ \E m \in Members : ProbePass(m)
    \/ \E m \in Members : ProbeFail(m)
    \/ \E m \in Members, p \in {"none", "fail"} : ProbeNew(m, p)
    \/ \E m \in Members : Boot(m)
    \/ \E m \in Members : Accept(m)
    \/ \E m \in Members : Refuse(m)

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

\* ---------------------------------------------------------------- the rules

\* a member is held only by an environment finish: a model failure holds no one
EnvOnlyByEnvironmentFinish ==
    [][\A m \in Members : (~held[m] /\ held[m]') => event' \in Causes]_vars

\* a held member is dealt nothing
HeldDealtNothing ==
    [][\A m \in Members : (held[m] /\ event' = "deal") => FALSE]_vars

\* one judgment per (member, cause): it names the member's current cause, so a
\* second, different fault is surfaced, never left as a stale remedy
JudgmentNamesTheCause ==
    [][\A m \in Members : judged[m] => jcause[m] = env[m]]_vars

\* the member's next card that pushes clears the fault by itself
FaultClearsOnPush ==
    [][\A m \in Members : (event' = "push") => (env'[m] = "none" /\ held'[m] = FALSE)]_vars

\* a held member is released only by a push or a probe that passes
FleetUpRefusesUncleared ==
    [][\A m \in Members : (held[m] /\ ~held[m]' /\ event' # "push") => event' = "probe-pass"]_vars

\* a new member is accepted only when its own probe passed (init's Boot sets it)
NewMemberProbed ==
    [][\A m \in Members : (~known[m] /\ known[m]') => probe[m] = "pass"]_vars

\* the member has a cause while it is held
HeldHasACause ==
    [][\A m \in Members : held[m] => env[m] # "none"]_vars

=============================================================================
