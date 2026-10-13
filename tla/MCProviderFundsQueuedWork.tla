----------------------- MODULE MCProviderFundsQueuedWork -----------------------
EXTENDS ProviderFunds

\* Bounded projection of queuedWorkLive: a work card already dealt into
\* Fleet.Ready has a Working primary and a persisted pin. Validity is not
\* availability of a fresh route in the read tier. Positive width is required,
\* but a currently occupied slot is a temporary wait, not a funding failure.
\* The cases name the deterministic runtime matrix; this wrapper does not
\* parse token strings or prove the whole dispatch protocol.
QueueCases == {"none", "validpin", "fleetoff", "held", "stale", "missing",
               "down", "zerowidth", "badbudget", "foreignmodel", "routedrest",
               "livepersisted", "disabledroute", "missingroute", "friend",
               "unlandedneed", "baddeadline"}
VARIABLES queueCase, fullWidth, badQueuedStop
queueVars == <<queueCase, fullWidth, badQueuedStop>>
qvars == <<pvars, queueVars>>

FleetEnabled == queueCase # "fleetoff"
PrimaryCurrent == queueCase \notin {"none", "stale", "missing"}
StreamOpen == queueCase # "held"
MemberUp == queueCase # "down"
WidthPositive == queueCase # "zerowidth"
NativeProvenance == queueCase # "friend"
NeedsLanded == queueCase # "unlandedneed"
ValidPersistedPin == queueCase \notin {"none", "badbudget", "foreignmodel", "baddeadline",
                                        "routedrest", "livepersisted", "disabledroute", "missingroute"}
PersistedRouteLive == queueCase = "livepersisted"
\* Named persisted routes use their current enable/launch/rest capability;
\* changes to a fresh draw array do not revoke an already-dealt lease.
ExecutableQueuedWork == FleetEnabled /\ PrimaryCurrent /\ StreamOpen
                        /\ MemberUp /\ WidthPositive /\ NativeProvenance
                        /\ NeedsLanded /\ (ValidPersistedPin \/ PersistedRouteLive)
\* The routed-rest case is deliberately not a valid independent pin.
\* fullWidth is excluded: capacity can free while the same lease remains valid.
CountedQueuedLive == IF "invalidqueued" \in Broken THEN queueCase # "none"
                    ELSE ExecutableQueuedWork

QueueInit ==
    /\ PFInit /\ workMode
    /\ queueCase \in QueueCases /\ fullWidth \in BOOLEAN
    /\ IF "ignorequeued" \in Broken THEN queueCase = "validpin" /\ fullWidth
       ELSE IF "invalidqueued" \in Broken THEN queueCase = "fleetoff"
       ELSE TRUE
    /\ badQueuedStop = FALSE
QueueTypeOK == PFTypeOK /\ queueCase \in QueueCases
               /\ fullWidth \in BOOLEAN /\ badQueuedStop \in BOOLEAN

QueuedTickNothing ==
    /\ UNCHANGED <<queueCase, fullWidth>>
    /\ TickNothing
    /\ (~CountedQueuedLive \/ "ignorequeued" \in Broken)
    /\ badQueuedStop' = (badQueuedStop \/ ExecutableQueuedWork)

\* Preserve ordinary work/rest/wake and funds-stop precedence; replace only
\* the nothing-funded stop with the queued execution predicate.
OtherWorkNext ==
    \/ \E w \in SUBSET Tiers : SetWork(w)
    \/ \E t \in Tiers : RestAll(t) \/ WakeTier(t)
    \/ Stop \/ Start \/ TellOwner \/ CreditAll
QueuedTellOwner == UNCHANGED queueVars /\ TellOwner
QueueNext == (OtherWorkNext /\ UNCHANGED queueVars) \/ QueuedTickNothing
QueueSpec == QueueInit /\ [][QueueNext]_qvars
             /\ WF_qvars(QueuedTickNothing) /\ WF_qvars(QueuedTellOwner)
NoNothingStopWithExecutableQueue == ~badQueuedStop
\* Invalid/off/held/stale queues do not hide blocked primary/read tiers.
\* As in NeverRunningUnfunded, transient blockage may end by a route wake;
\* stable blockage without executable queued work must eventually stop.
InvalidQueueDoesNotHideBlockedWork ==
    (running /\ WorkBlocked(work, live) /\ ~ExecutableQueuedWork)
      ~> (~running \/ ~WorkBlocked(work, live))
=============================================================================
