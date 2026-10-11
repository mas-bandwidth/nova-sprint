------------------------------ MODULE ProviderFunds ------------------------------
\* Provider funds: providerRestsDue, providerConds, AllOutOfCredit, Funded,
\* balance.go's payment detection, and the coordinator's wake/start.
\* Each provider has one enabled route. BOTH providers instantiate RouteRest;
\* no rest transition is copied here. Low is the poll's threshold predicate;
\* payment is its already-detected increase over the previous/refusal balance.
\* Credit refusals name a card. Retired cardless credit and balance rests are
\* normalized to none by the source reader and cannot be created by these actions.
EXTENDS Naturals, Sequences, FiniteSets
CONSTANTS Routes, W, After, Broken
VARIABLES rest, own, win, low, redeal, event, takes, ended, running, friendUp
Providers == {"p1", "p2"}
Slots == {1, 2}
Route(p) == IF p = "p1" THEN "r1" ELSE "r2"
RR(p) == INSTANCE RouteRest WITH
    Routes <- {Route(p)}, provRest <- rest[p], own <- own[p], win <- win[p],
    low <- low[p], judged <- FALSE, redeal <- redeal[p]
pvars == <<rest, own, win, low, redeal, event, takes, ended, running, friendUp>>

\* Finite order abstraction of e.taken.After(last.Mark()), NOT a wrapping clock:
\* after = a known launch strictly after the last rest end (or no prior rest);
\* stale = launched at/before that mark; unknown = missing launch timestamp.
\* Every end advances the abstract mark by relabelling ALL outstanding launches
\* stale. A fresh launch then becomes after. Two slots preserve a delayed take
\* across a different take's refusal, rest and payment/wake, indefinitely.
PFInit ==
    /\ \A p \in Providers : RR(p)!Init
    /\ takes = [p \in Providers |-> [s \in Slots |-> "none"]]
    /\ ended = [p \in Providers |-> FALSE]
    /\ running = TRUE
    /\ friendUp = FALSE

Other(p) == \A q \in Providers \ {p} :
    UNCHANGED <<rest[q], own[q], win[q], low[q], redeal[q]>>
MachineStill == UNCHANGED <<running, friendUp>>
Launch(p, s, stamp) ==
    /\ ~RR(p)!Resting(Route(p))
    /\ takes[p][s] = "none"
    /\ stamp \in {"after", "unknown"}
    /\ takes' = [takes EXCEPT ![p][s] = stamp]
    /\ UNCHANGED <<rest, own, win, low, redeal, event, ended, running, friendUp>>

Eligible(p, s) == ~ended[p] \/ takes[p][s] = "after"
Refuse(p, s, cause) ==
    /\ takes[p][s] # "none"
    /\ cause \in {"credit", "auth"}
    /\ IF (Eligible(p, s) \/ "stalerefusal" \in Broken)
           /\ ~RR(p)!Resting(Route(p))
         THEN /\ RR(p)!TakeEnds(Route(p), cause)
              /\ Other(p)
         ELSE /\ UNCHANGED <<rest, own, win, low, redeal>>
              /\ event' = "ignored"
    /\ takes' = [takes EXCEPT ![p][s] = "none"]
    /\ UNCHANGED ended
    /\ MachineStill

Poll(p, l) ==
    /\ RR(p)!Balance(l)
    /\ Other(p)
    /\ UNCHANGED <<takes, ended>>
    /\ MachineStill

EndMark(p) ==
    /\ ended' = [ended EXCEPT ![p] = TRUE]
    /\ takes' = [takes EXCEPT ![p] =
         [s \in Slots |-> IF takes[p][s] = "none" THEN "none" ELSE "stale"]]
Pay(p, e) ==
    /\ RR(p)!Pay(e)
    /\ Other(p)
    /\ EndMark(p)
    /\ MachineStill
Wake(p) ==
    /\ RR(p)!WakeProvider
    /\ Other(p)
    /\ EndMark(p)
    /\ MachineStill
Rest(p) ==
    /\ RR(p)!RestProvider
    /\ Other(p)
    /\ UNCHANGED <<takes, ended>>
    /\ MachineStill

\* providerConds computes one judgment from the current snapshot, rather than
\* storing RouteRest's separate low Raise flag. Recompute on every transition:
\* credit/auth beat low; coordinator rest suppresses low; ending a rest clears
\* its judgment and reveals low if still present; a non-low read clears low.
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
    /\ running' = FALSE /\ event' = "stop"
    /\ UNCHANGED <<rest, own, win, low, redeal, takes, ended, friendUp>>
Start ==
    /\ ~running /\ (~AllOutBroken \/ friendUp)
    /\ running' = TRUE /\ event' = "start"
    /\ UNCHANGED <<rest, own, win, low, redeal, takes, ended, friendUp>>
Friend(b) ==
    /\ friendUp' = b
    /\ UNCHANGED <<rest, own, win, low, redeal, event, takes, ended, running>>
PFNext ==
    \/ \E p \in Providers, s \in Slots, stamp \in {"after", "unknown"} : Launch(p,s,stamp)
    \/ \E p \in Providers, s \in Slots, c \in {"credit", "auth"} : Refuse(p,s,c)
    \/ \E p \in Providers, l \in BOOLEAN : Poll(p,l)
    \/ \E p \in Providers, e \in {"payment", "funded"} : Pay(p,e)
    \/ \E p \in Providers : Wake(p) \/ Rest(p)
    \/ Stop \/ Start
    \/ \E b \in BOOLEAN : Friend(b)
PFSpec == PFInit /\ [][PFNext]_pvars
PFTypeOK ==
    /\ \A p \in Providers :
        /\ rest[p] \in {"none", "credit", "auth", "coord"}
        /\ own[p] \in [{Route(p)} -> {"none"}]
        /\ win[p] \in [{Route(p)} -> RR(p)!Windows]
        /\ low[p] \in BOOLEAN
        /\ redeal[p] \in 0..W
    /\ takes \in [Providers -> [Slots -> {"none", "after", "stale", "unknown"}]]
    /\ ended \in [Providers -> BOOLEAN]
    /\ running \in BOOLEAN /\ friendUp \in BOOLEAN
    /\ event \in {"init", "credit", "auth", "ignored", "balance", "payment", "funded", "wake", "coord", "stop", "start"}
JudgmentsMatch == \A p \in Providers :
    /\ (Judgments[p] = "funds") <=> (rest[p] = "credit")
    /\ (Judgments[p] = "key") <=> (rest[p] = "auth")
    /\ (Judgments[p] = "low") <=> (low[p] /\ rest[p] = "none")
StopOnlyWhenAllOut == [][running /\ ~running' => AllOut]_pvars
LowNeverStops == [][running /\ ~running' => \A p \in Providers : ~low[p] \/ Out(p)]_pvars
NoFundsStopWhileFriendUp == [][friendUp /\ running => running']_pvars
StartIsTheCoordinators == [][~running /\ running' => event' = "start"]_pvars
\* A new refusal rest requires a consumed launch after the previous rest-end
\* mark. Checks the stale completion after payment, funded and wake, including
\* an old unknown timestamp. The reversed witness removes exactly that guard.
RefusalsAfterLastEnd == [][\A p \in Providers :
    (rest[p] = "none" /\ rest'[p] \in {"credit", "auth"} /\ ended[p]) =>
       \E s \in Slots : takes[p][s] = "after" /\ takes'[p][s] = "none"]_pvars
=============================================================================
