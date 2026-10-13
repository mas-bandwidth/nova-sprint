------------------------------ MODULE ProviderFunds ------------------------------
\* Provider funds: providerRestsDue, providerConds, AllOutOfCredit, Funded,
\* balance.go's payment detection, and the coordinator's wake/start.
\* Each provider has one enabled route. BOTH providers instantiate RouteRest;
\* no rest transition is copied here. Low is the poll's threshold predicate;
\* payment is its already-detected increase over the previous/refusal balance.
\* Credit refusals name a card. Retired cardless credit and balance rests are
\* normalized to none by the source reader and cannot be created by these actions.
\* A timed coordinator's rest the clock ends (RouteRest's ExpireProvider) sets
\* the same mark as Wake (Until = the end), so Wake stands for both ends here.
EXTENDS Naturals, Sequences, FiniteSets
CONSTANTS Routes, W, After, Broken
VARIABLES rest1, own1, win1, low1, redeal1, returns1, judged1,
          rest2, own2, win2, low2, redeal2, returns2, judged2,
          event, takes, ended, running, friendUp, work, live, cause, toldOwner
Providers == {"p1", "p2"}
Slots == {1, 2}
Route(p) == IF p = "p1" THEN "r1" ELSE "r2"
R1 == INSTANCE RouteRest WITH Routes <- {"r1"}, provRest <- rest1,
    own <- own1, win <- win1, low <- low1, judged <- judged1, redeal <- redeal1, returns <- returns1
R2 == INSTANCE RouteRest WITH Routes <- {"r2"}, provRest <- rest2,
    own <- own2, win <- win2, low <- low2, judged <- judged2, redeal <- redeal2, returns <- returns2
rest == [p \in Providers |-> IF p = "p1" THEN rest1 ELSE rest2]
own == [p \in Providers |-> IF p = "p1" THEN own1 ELSE own2]
win == [p \in Providers |-> IF p = "p1" THEN win1 ELSE win2]
low == [p \in Providers |-> IF p = "p1" THEN low1 ELSE low2]
redeal == [p \in Providers |-> IF p = "p1" THEN redeal1 ELSE redeal2]
Resting(p) == IF p = "p1" THEN R1!Resting("r1") ELSE R2!Resting("r2")
pvars == <<rest1, own1, win1, low1, redeal1, returns1, judged1,
           rest2, own2, win2, low2, redeal2, returns2, judged2,
           event, takes, ended, running, friendUp, work, live, cause, toldOwner>>
Tiers == {"flash", "pro", "heavy"}
Unfunded == work # {} /\ work \cap live = {}
UnfundedNext == work' # {} /\ work' \cap live' = {}

\* Finite order abstraction of e.taken.After(last.Mark()), NOT a wrapping clock:
\* after = a known launch strictly after the last rest end (or no prior rest);
\* stale = launched at/before that mark; unknown = missing launch timestamp.
\* Every end advances the abstract mark by relabelling ALL outstanding launches
\* stale. A fresh launch then becomes after. Two slots preserve a delayed take
\* across a different take's refusal, rest and payment/wake, indefinitely.
PFInit ==
    /\ R1!Init /\ R2!Init
    /\ takes = [p \in Providers |-> [s \in Slots |-> "none"]]
    /\ ended = [p \in Providers |-> FALSE]
    /\ running = TRUE
    /\ friendUp = FALSE
    /\ work = {} /\ live = Tiers /\ cause = "none" /\ toldOwner = FALSE

Other(p) == IF p = "p1" THEN UNCHANGED <<rest2, own2, win2, low2, redeal2, returns2, judged2>>
            ELSE UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1>>
MachineStill == UNCHANGED <<running, friendUp, work, live, cause, toldOwner>>
Launch(p, s, stamp) ==
    /\ ~Resting(p)
    /\ takes[p][s] = "none"
    /\ stamp \in {"after", "unknown"}
    /\ takes' = [takes EXCEPT ![p][s] = stamp]
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2, event, ended, running, friendUp, work, live, cause, toldOwner>>

Eligible(p, s) == ~ended[p] \/ takes[p][s] = "after"
Refuse(p, s, cause) ==
    /\ takes[p][s] # "none"
    /\ cause \in {"credit", "auth"}
    /\ IF (Eligible(p, s) \/ "stalerefusal" \in Broken)
           /\ ~Resting(p)
         THEN /\ IF p = "p1" THEN R1!TakeEnds("r1", cause) ELSE R2!TakeEnds("r2", cause)
              /\ Other(p)
         ELSE /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2>>
              /\ event' = "ignored"
    /\ takes' = [takes EXCEPT ![p][s] = "none"]
    /\ UNCHANGED ended
    /\ MachineStill

Poll(p, l) ==
    /\ IF p = "p1" THEN R1!Balance(l) ELSE R2!Balance(l)
    /\ Other(p)
    /\ UNCHANGED <<takes, ended>>
    /\ MachineStill

EndMark(p) ==
    /\ ended' = [ended EXCEPT ![p] = TRUE]
    /\ takes' = [takes EXCEPT ![p] =
         [s \in Slots |-> IF takes[p][s] = "none" THEN "none" ELSE "stale"]]
Pay(p, e) ==
    /\ IF p = "p1" THEN R1!Pay(e) ELSE R2!Pay(e)
    /\ Other(p)
    /\ EndMark(p)
    /\ MachineStill
Wake(p) ==
    /\ IF p = "p1" THEN R1!WakeProvider ELSE R2!WakeProvider
    /\ Other(p)
    /\ EndMark(p)
    /\ MachineStill
Rest(p) ==
    /\ IF p = "p1" THEN R1!RestProvider ELSE R2!RestProvider
    /\ Other(p)
    /\ UNCHANGED <<takes, ended>>
    /\ MachineStill

\* providerConds computes one judgment from the current snapshot, rather than
\* storing RouteRest's separate low Raise flag. Recompute on every transition:
\* credit/auth beat low; coordinator rest suppresses low; ending a rest clears
\* its judgment and reveals low if still present; a non-low read clears low.
\* Judgment is a definition of providerConds' answer, not a checked property:
\* no invariant restates it (one would hold by definition and could not fail).
Judgment(p) == IF rest[p] = "credit" THEN "funds"
               ELSE IF rest[p] = "auth" THEN "key"
               ELSE IF rest[p] = "none" /\ low[p] THEN "low"
               ELSE "none"
Judgments == [p \in Providers |-> Judgment(p)]
Out(p) == rest[p] = "credit"
AllOut == \A p \in Providers : Out(p)
AllOutBroken == \A p \in Providers : Out(p) \/ ("lowisout" \in Broken /\ low[p])
Stop ==
    /\ running /\ AllOutBroken
    /\ ~friendUp \/ "nofriendexception" \in Broken
    /\ running' = FALSE /\ event' = "stop" /\ cause' = "funds"
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2, takes, ended, friendUp, work, live, toldOwner>>
\* The new stop, beside AllOut Stop: fires in the same step the cause arises
\* (WorkArrives/TierRest below stop atomically), so NeverRunningUnfunded is a
\* state invariant, not only a liveness formula.
StopUnfunded ==
    /\ running /\ Unfunded /\ "rununfunded" \notin Broken
    /\ running' = FALSE /\ event' = "stop" /\ cause' = "nothing" /\ toldOwner' = FALSE
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2, takes, ended, friendUp, work, live>>
StopIfUnfunded ==
    IF running /\ UnfundedNext /\ "rununfunded" \notin Broken
       THEN /\ running' = FALSE /\ event' = "stop" /\ cause' = "nothing" /\ toldOwner' = FALSE
       ELSE /\ UNCHANGED <<running, event, cause, toldOwner>>
WorkArrives(w) ==
    /\ w \subseteq Tiers /\ work' = w /\ UNCHANGED live
    /\ StopIfUnfunded
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2, takes, ended, friendUp>>
TierRest(tier) ==
    /\ tier \in Tiers /\ live' = live \ {tier} /\ UNCHANGED work
    /\ StopIfUnfunded
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2, takes, ended, friendUp>>
TierWake(tier) ==
    /\ tier \in Tiers /\ live' = live \cup {tier}
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2, takes, ended, friendUp, work, running, event, cause, toldOwner>>
TellOwner ==
    /\ ~running /\ cause = "nothing" /\ work \cap live # {} /\ ~toldOwner
    /\ toldOwner' = TRUE
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2, takes, ended, friendUp, work, live, running, event, cause>>
Start ==
    /\ ~running /\ (~AllOutBroken \/ friendUp)
    /\ (cause # "nothing" \/ work \cap live # {})
    /\ running' = TRUE /\ event' = "start" /\ cause' = "none" /\ toldOwner' = FALSE
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, returns2, judged2, takes, ended, friendUp, work, live>>
Friend(b) ==
    /\ friendUp' = b
    /\ UNCHANGED <<rest1, own1, win1, low1, redeal1, returns1, judged1, rest2, own2, win2, low2, redeal2, judged2, event, takes, ended, running, work, live, cause, toldOwner>>
PFNext ==
    \/ \E p \in Providers, s \in Slots, stamp \in {"after", "unknown"} : Launch(p,s,stamp)
    \/ \E p \in Providers, s \in Slots, c \in {"credit", "auth"} : Refuse(p,s,c)
    \/ \E p \in Providers, l \in BOOLEAN : Poll(p,l)
    \/ \E p \in Providers, e \in {"payment", "funded"} : Pay(p,e)
    \/ \E p \in Providers : Wake(p) \/ Rest(p)
    \/ Stop \/ StopUnfunded \/ Start \/ TellOwner
    \/ \E w \in SUBSET Tiers : WorkArrives(w)
    \/ \E tier \in Tiers : TierRest(tier) \/ TierWake(tier)
    \/ \E b \in BOOLEAN : Friend(b)
PFSpec == PFInit /\ [][PFNext]_pvars /\ WF_pvars(Start) /\ WF_pvars(TellOwner)
PFTypeOK ==
    /\ judged1 = FALSE /\ judged2 = FALSE
    /\ \A p \in Providers :
        /\ rest[p] \in {"none", "credit", "auth", "coord"}
        /\ own[p] \in [{Route(p)} -> {"none"}]
        /\ win[p] \in [{Route(p)} -> R1!Windows]
        /\ low[p] \in BOOLEAN
        /\ redeal[p] \in 0..W
    /\ takes \in [Providers -> [Slots -> {"none", "after", "stale", "unknown"}]]
    /\ ended \in [Providers -> BOOLEAN]
    /\ running \in BOOLEAN /\ friendUp \in BOOLEAN
    /\ work \subseteq Tiers /\ live \subseteq Tiers /\ cause \in {"none", "funds", "nothing"} /\ toldOwner \in BOOLEAN
    /\ event \in {"init", "credit", "auth", "ignored", "balance", "payment", "funded", "wake", "coord", "stop", "start"}
StopOnlyWhenAllOut == [][running /\ ~running' => AllOut \/ (cause' = "nothing" /\ UnfundedNext)]_pvars
LowNeverStops == [][running /\ ~running' /\ cause' = "funds" => \A p \in Providers : ~low[p] \/ Out(p)]_pvars
NoFundsStopWhileFriendUp == [][friendUp /\ running /\ ~UnfundedNext => running']_pvars
StartIsTheCoordinators == [][~running /\ running' => event' = "start"]_pvars
\* A new refusal rest requires a consumed launch after the previous rest-end
\* mark. Checks the stale completion after payment, funded and wake, including
\* an old unknown timestamp. The reversed witness removes exactly that guard.
RefusalsAfterLastEnd == [][\A p \in Providers :
    (rest[p] = "none" /\ rest'[p] \in {"credit", "auth"} /\ ended[p]) =>
       \E s \in Slots : takes[p][s] = "after" /\ takes'[p][s] = "none"]_pvars
NeverRunningUnfunded == running => (work = {} \/ work \cap live # {})
ResumableWhenFunded == (~running /\ cause = "nothing" /\ work \cap live # {}) ~> (running \/ toldOwner)
=============================================================================
