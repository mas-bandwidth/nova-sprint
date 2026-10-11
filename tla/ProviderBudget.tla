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
\* member naming the remedy. It is the shape a provider's rest takes
\* (RouteRest.tla): the provider's balance is its budget; the member's
\* environment is its.
\*
\* The state, for one member m: env[m], the member's last environment fault
\* (none, or a cause); held[m], the member is held by it and is dealt nothing;
\* judged[m], the environment judgment is open on the member; card[m], where the
\* member's card is (ready: dealt and untaken, working: taken, pushed: its next
\* card pushed, failed: a model or card failure); event, what the last step was.
\*
\* The design, Broken = {}:
\*   EnvFinish(m)   a card on m finishes with an environment fault: env[m]
\*                  becomes a cause, held[m] TRUE, judged[m] TRUE, and the card
\*                  returns to ready (no attempt spent). No card judgment.
\*   ModelFinish(m) a card on m fails the model's way: nothing is held and no
\*                  environment judgment is raised; the card is failed work.
\*   Deal(m)        the deal gives m a card only while it is not held (held[m]
\*                  FALSE): a held member is dealt nothing.
\*   Push(m)        m's next card pushes: env[m] clears, held[m] FALSE, judged[m]
\*                  FALSE. The fault clears itself.
\*   ProbePass(m)   fleet up runs m's own probe and it passes: env[m] clears,
\*                  held[m] FALSE, judged[m] FALSE. The remedy worked.
\*   ProbeFail(m)   the probe fails: the fault stands and fleet up refuses.
\* Reversed witnesses:
\*   "noenv"    a model failure holds the member: EnvOnlyByEnvironmentFinish
\*   "nodeal"   the deal gives a held member a card: HeldDealtNothing
\*   "two"      a second environment fault raises a second judgment:
\*              OneJudgmentPerMember
\*   "nopush"   a push never clears the fault: FaultClearsOnPush
\*   "clear"    fleet up accepts a member whose fault is not cleared:
\*              FleetUpRefusesUncleared
EXTENDS Naturals, FiniteSets

CONSTANTS Members, Causes, Broken

VARIABLES env, held, judged, card, event
vars == <<env, held, judged, card, event>>

Events == Causes \cup {"model", "deal", "push", "probe-pass", "probe-fail", "init"}

TypeOK ==
    /\ env \in [Members -> {"none"} \cup Causes]
    /\ held \in [Members -> BOOLEAN]
    /\ judged \in [Members -> BOOLEAN]
    /\ card \in [Members -> {"ready", "working", "pushed", "failed"}]
    /\ event \in Events

Init ==
    /\ env = [m \in Members |-> "none"]
    /\ held = [m \in Members |-> FALSE]
    /\ judged = [m \in Members |-> FALSE]
    /\ card = [m \in Members |-> "ready"]
    /\ event = "init"

Held(m) == held[m]

\* a card on a not-held member finishes with an environment fault: the card
\* returns to ready untouched, the member is held and one judgment is raised
EnvFinish(m, c) ==
    /\ c \in Causes
    /\ (~held[m] \/ "two" \in Broken)  \* "two": a second fault raises a second judgment
    /\ env' = [env EXCEPT ![m] = c]
    /\ held' = [held EXCEPT ![m] = TRUE]
    /\ judged' = [judged EXCEPT ![m] = TRUE]
    /\ card' = [card EXCEPT ![m] = "ready"]
    /\ event' = c
    /\ UNCHANGED <<>>  \* nothing else

\* a card on m fails the model's way: not the environment's, so nothing is held
\* ("noenv": the broken rule holds it anyway)
ModelFinish(m) ==
    /\ ~held[m]
    /\ held' = [held EXCEPT ![m] = "noenv" \in Broken]
    /\ card' = [card EXCEPT ![m] = "failed"]
    /\ event' = "model"
    /\ UNCHANGED <<env, judged>>

\* the deal gives a not-held member a card ("nodeal": it deals to a held one too)
Deal(m) ==
    /\ (~held[m] \/ "nodeal" \in Broken)
    /\ card[m] = "ready"
    /\ card' = [card EXCEPT ![m] = "working"]
    /\ event' = "deal"
    /\ UNCHANGED <<env, held, judged>>

\* a held member's card in flight pushes: the fault clears itself
\* ("nopush": the broken rule never clears it)
Push(m) ==
    /\ held[m]
    /\ card[m] \in {"working", "ready"}
    /\ \/ "nopush" \in Broken
          /\ UNCHANGED <<env, held, judged>>
       \/ "nopush" \notin Broken
          /\ env' = [env EXCEPT ![m] = "none"]
          /\ held' = [held EXCEPT ![m] = FALSE]
          /\ judged' = [judged EXCEPT ![m] = FALSE]
    /\ card' = [card EXCEPT ![m] = "pushed"]
    /\ event' = "push"

\* fleet up runs the member's own probe: a pass clears the fault, a failure
\* leaves it and fleet up refuses
ProbePass(m) ==
    /\ held[m]
    /\ env' = [env EXCEPT ![m] = "none"]
    /\ held' = [held EXCEPT ![m] = FALSE]
    /\ judged' = [judged EXCEPT ![m] = FALSE]
    /\ event' = "probe-pass"
    /\ UNCHANGED card

\* "clear": the broken rule lets a failed probe clear the fault
ProbeFail(m) ==
    /\ held[m]
    /\ \/ "clear" \in Broken
          /\ env' = [env EXCEPT ![m] = "none"]
          /\ held' = [held EXCEPT ![m] = FALSE]
          /\ judged' = [judged EXCEPT ![m] = FALSE]
       \/ "clear" \notin Broken
          /\ UNCHANGED <<env, held, judged>>
    /\ event' = "probe-fail"
    /\ UNCHANGED card

\* a member with no fault may run its probe before its width is accepted
ProbeNew(m) ==
    /\ env[m] = "none"
    /\ held[m] = FALSE
    /\ event' = "probe-pass"
    /\ UNCHANGED <<env, held, judged, card>>

Next ==
    \/ \E m \in Members, c \in Causes : EnvFinish(m, c)
    \/ \E m \in Members : ModelFinish(m)
    \/ \E m \in Members : Deal(m)
    \/ \E m \in Members : Push(m)
    \/ \E m \in Members : ProbePass(m)
    \/ \E m \in Members : ProbeFail(m)
    \/ \E m \in Members : ProbeNew(m)

Spec == Init /\ [][Next]_vars /\ WF_vars(Next)

\* ---------------------------------------------------------------- the rules

\* a member is held only by an environment finish: a model failure holds no one
EnvOnlyByEnvironmentFinish ==
    [][\A m \in Members : (~held[m] /\ held[m]') => event' \in Causes]_vars

\* a held member is dealt nothing
HeldDealtNothing ==
    [][\A m \in Members : (held[m] /\ card'[m] = "working") => event' = "push"]_vars

\* one judgment per member: a member already held takes no second environment
\* finish, so no second judgment
OneJudgmentPerMember ==
    [][\A m \in Members : (held[m] /\ event' \in Causes) => FALSE]_vars

\* the member's next card that pushes clears the fault by itself
FaultClearsOnPush ==
    [][\A m \in Members : (event' = "push") => (env'[m] = "none" /\ held'[m] = FALSE)]_vars

\* fleet up accepts a member whose fault is cleared only by a probe that passes
FleetUpRefusesUncleared ==
    [][\A m \in Members : (event' = "probe-pass" /\ env[m] # "none") => (env'[m] = "none" /\ held'[m] = FALSE)]_vars

\* the environment's cause is kept while the member is held
HeldKeepsItsCause ==
    [][\A m \in Members : (held[m] /\ held'[m]) => env'[m] # "none"]_vars

=============================================================================
