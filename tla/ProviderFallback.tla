-------------------------- MODULE ProviderFallback --------------------------
\* One provider failing never stalls the fleet: it falls back to the next route in
\* line (Glenn, 2026-10-19, after Ultramarine 429s spent every reader and stalled
\* reviews fleet-wide for hours). Per tier the routes form one ordered list across
\* providers (Routes, a sequence of providers here: one route per provider is
\* enough for the property). A run or read that fails on a PROVIDER fault (429, 5xx,
\* timeout, 402/unfunded) is retried at once on the next route in line: the same
\* card, the same attempt, no reader and no attempt spent, never bounced to ready.
\* The failing provider cools off: "rate" (a backoff from Retry-After, ends by
\* itself: CoolExpire) or "unfunded" (out until the owner funds it: OwnerFund, see
\* ProviderFunding). Only when every route of the tier cools does the card wait, with
\* a server-computed deadline and a coordinator judgment naming the providers.
EXTENDS Naturals, Sequences
CONSTANTS Routes, MaxFaults, Broken
VARIABLES card, on, cool, spent, faults, deadline, judged, dealtCooling
vars == <<card, on, cool, spent, faults, deadline, judged, dealtCooling>>

Providers == {Routes[i] : i \in 1..Len(Routes)}
None == "none"
Avail(p) == cool[p] = "none"
AnyAvail == \E p \in Providers : Avail(p)
\* the first route in line that does not cool
First == Routes[CHOOSE i \in 1..Len(Routes) :
                   Avail(Routes[i]) /\ \A j \in 1..(i-1) : ~Avail(Routes[j])]
\* the route the deal or the fallback takes (Broken "dealcooling": any route at all)
Pick(q) == IF "dealcooling" \in Broken THEN q \in Providers ELSE (AnyAvail /\ q = First)

Init == /\ card = "ready" /\ on = None
        /\ cool \in [Providers -> {"none", "unfunded"}]
        /\ spent = 0 /\ faults = 0 /\ deadline = FALSE /\ judged = FALSE
        /\ dealtCooling = FALSE

Deal == /\ card = "ready"
        /\ \E q \in Providers : /\ Pick(q)
                                /\ card' = "working" /\ on' = q
                                /\ dealtCooling' = (dealtCooling \/ ~Avail(q))
        /\ UNCHANGED <<cool, spent, faults, deadline, judged>>

\* a provider fault ends the run or read on p: p cools; the card is NOT spent
ProviderFault(p, kind) ==
    /\ card = "working" /\ on = p /\ faults < MaxFaults
    /\ cool' = [cool EXCEPT ![p] = IF kind = "unfunded" THEN "unfunded" ELSE "rate"]
    /\ card' = "fault" /\ faults' = faults + 1
    /\ spent' = IF "spend" \in Broken THEN spent + 1 ELSE spent
    /\ UNCHANGED <<on, deadline, judged, dealtCooling>>

\* the same card, the same attempt, on the next route in line; or, every route cooling,
\* a wait with a deadline and one judgment naming the providers
Fallback ==
    /\ card = "fault"
    /\ IF AnyAvail /\ "nofallback" \notin Broken
       THEN \E q \in Providers : /\ Pick(q)
                                 /\ card' = "working" /\ on' = q
                                 /\ dealtCooling' = (dealtCooling \/ ~Avail(q))
                                 /\ UNCHANGED <<deadline, judged>>
       ELSE /\ card' = "waiting"
            /\ deadline' = ("nodeadline" \notin Broken) /\ judged' = TRUE
            /\ UNCHANGED <<on, dealtCooling>>
    /\ UNCHANGED <<cool, spent, faults>>

\* a waiting card goes back to ready once any route of the tier stops cooling
\* (Broken "nofallback": it waits for the provider that failed it, today's stall)
Wake == /\ card = "waiting"
        /\ IF "nofallback" \in Broken THEN Avail(on) ELSE AnyAvail
        /\ card' = "ready" /\ on' = None /\ deadline' = FALSE /\ judged' = FALSE
        /\ UNCHANGED <<cool, spent, faults, dealtCooling>>

CoolExpire(p) == /\ cool[p] = "rate"
                 /\ cool' = [cool EXCEPT ![p] = "none"]
                 /\ UNCHANGED <<card, on, spent, faults, deadline, judged, dealtCooling>>

OwnerFund(p) == /\ cool[p] = "unfunded"
                /\ cool' = [cool EXCEPT ![p] = "none"]
                /\ UNCHANGED <<card, on, spent, faults, deadline, judged, dealtCooling>>

Finish == /\ card = "working" /\ card' = "done"
          /\ UNCHANGED <<on, cool, spent, faults, deadline, judged, dealtCooling>>

Next == \/ Deal \/ Fallback \/ Wake \/ Finish
        \/ \E p \in Providers : \/ CoolExpire(p) \/ OwnerFund(p)
                                \/ \E k \in {"rate", "5xx", "timeout", "unfunded"} : ProviderFault(p, k)
\* fairness on the machine's own steps and on a backoff's end; never on OwnerFund
Spec == /\ Init /\ [][Next]_vars
        /\ WF_vars(Deal) /\ WF_vars(Fallback) /\ WF_vars(Wake) /\ WF_vars(Finish)
        /\ \A p \in Providers : WF_vars(CoolExpire(p))

TypeOK == /\ card \in {"ready", "working", "fault", "waiting", "done"}
          /\ on \in Providers \cup {None}
          /\ cool \in [Providers -> {"none", "rate", "unfunded"}]
          /\ spent \in 0..MaxFaults /\ faults \in 0..MaxFaults
\* a provider fault never spends a reader or a worker attempt
FaultSpendsNothing == spent = 0
\* no route of an unfunded or cooling provider is dealt
NoDealToCooling == ~dealtCooling
\* a card waits only with a server-computed deadline and a judgment naming the providers
WaitHasDeadline == card = "waiting" => (deadline /\ judged)
\* a card with any non-cooling route for its tier is never left unworked
NeverLeftUnworked == (card \in {"ready", "fault", "waiting"} /\ AnyAvail) ~> card \in {"working", "done"}
=============================================================================
