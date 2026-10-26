--------------------------- MODULE ProviderFunding ---------------------------
\* A provider out of money flips to unfunded BY ITSELF, and only the owner funds it
\* again (Glenn, 2026-10-19, after openrouter and opencode were retried for the tenth
\* time on rows that still said funded=true: "when a route is not funded, it must FLIP
\* into unfunded *automatically* in the config"). The provider row's funded field in
\* nova-config is the one true source; the deal reads it.
\*
\*   BillingFailure(p)     a take or read on p ends class=out-of-credit (a 402, the
\*                         provider's insufficient-credit shape): the machine writes
\*                         funded=false on p's row, once per provider (idempotent),
\*                         never per route.
\*   RateLimit(p)          a 429 / 5xx / timeout on p: never touches funding.
\*   OwnerFund(p)          the owner sets funded=true (nova-config, actor = owner).
\*   MachineFundAttempt(p) any other actor (the machine, the coordinator) sets
\*                         funded=true: REFUSED, nothing written.
\*   RouteEnable(p)        a route of p is enabled: refused while p is unfunded.
\*   RouteDisable(p)       a route of p is disabled.
\*   Deal(p)               the deal hands a card to a route of p.
EXTENDS Naturals, FiniteSets
CONSTANTS Providers, MaxWrites, Broken
VARIABLES funded, enabled, writes, dealtUnfunded, nonOwnerFunded, rateFlipped, enabledUnfunded
vars == <<funded, enabled, writes, dealtUnfunded, nonOwnerFunded, rateFlipped, enabledUnfunded>>

Init == /\ funded = [p \in Providers |-> TRUE]
        /\ enabled = [p \in Providers |-> TRUE]
        /\ writes = [p \in Providers |-> 0]
        /\ dealtUnfunded = FALSE /\ nonOwnerFunded = FALSE
        /\ rateFlipped = FALSE /\ enabledUnfunded = FALSE

\* one config write per provider: a second billing failure on an unfunded row writes nothing
BillingFailure(p) ==
    /\ writes[p] < MaxWrites
    /\ funded' = [funded EXCEPT ![p] = FALSE]
    /\ writes' = IF funded[p] \/ "perroute" \in Broken
                 THEN [writes EXCEPT ![p] = writes[p] + 1] ELSE writes
    /\ UNCHANGED <<enabled, dealtUnfunded, nonOwnerFunded, rateFlipped, enabledUnfunded>>

RateLimit(p) ==
    /\ IF "ratelimitflips" \in Broken
       THEN /\ funded' = [funded EXCEPT ![p] = FALSE]
            /\ rateFlipped' = (rateFlipped \/ funded[p])
       ELSE UNCHANGED <<funded, rateFlipped>>
    /\ UNCHANGED <<enabled, writes, dealtUnfunded, nonOwnerFunded, enabledUnfunded>>

OwnerFund(p) ==
    /\ ~funded[p]
    /\ funded' = [funded EXCEPT ![p] = TRUE]
    /\ writes' = [writes EXCEPT ![p] = 0]
    /\ UNCHANGED <<enabled, dealtUnfunded, nonOwnerFunded, rateFlipped, enabledUnfunded>>

\* the refuser: another actor's funded=true writes nothing (enabled only when Broken)
MachineFundAttempt(p) ==
    /\ ~funded[p]
    /\ "machinefunds" \in Broken
    /\ funded' = [funded EXCEPT ![p] = TRUE]
    /\ nonOwnerFunded' = TRUE
    /\ UNCHANGED <<enabled, writes, dealtUnfunded, rateFlipped, enabledUnfunded>>

RouteEnable(p) ==
    /\ ~enabled[p]
    /\ funded[p] \/ "enableunfunded" \in Broken
    /\ enabled' = [enabled EXCEPT ![p] = TRUE]
    /\ enabledUnfunded' = (enabledUnfunded \/ ~funded[p])
    /\ UNCHANGED <<funded, writes, dealtUnfunded, nonOwnerFunded, rateFlipped>>

RouteDisable(p) ==
    /\ enabled[p]
    /\ enabled' = [enabled EXCEPT ![p] = FALSE]
    /\ UNCHANGED <<funded, writes, dealtUnfunded, nonOwnerFunded, rateFlipped, enabledUnfunded>>

\* the deal reads funded as well as enabled: a route stays enabled across a flip
\* (one write per provider, never per route), so funded alone keeps it out
Deal(p) ==
    /\ enabled[p]
    /\ funded[p] \/ "dealunfunded" \in Broken
    /\ dealtUnfunded' = (dealtUnfunded \/ ~funded[p])
    /\ UNCHANGED <<funded, enabled, writes, nonOwnerFunded, rateFlipped, enabledUnfunded>>

Next == \E p \in Providers :
          \/ BillingFailure(p) \/ RateLimit(p) \/ OwnerFund(p) \/ MachineFundAttempt(p)
          \/ RouteEnable(p) \/ RouteDisable(p) \/ Deal(p)
Spec == Init /\ [][Next]_vars

TypeOK == /\ funded \in [Providers -> BOOLEAN] /\ enabled \in [Providers -> BOOLEAN]
          /\ writes \in [Providers -> 0..MaxWrites]
\* no card is ever dealt to a route of an unfunded provider
NoDealToUnfunded == ~dealtUnfunded
\* only the owner moves a provider from unfunded to funded
OnlyOwnerFunds == ~nonOwnerFunded
\* a rate limit (or 5xx, timeout) never changes funding
RateLimitKeepsFunding == ~rateFlipped
\* no route of an unfunded provider is enabled by a write
NoEnableOnUnfunded == ~enabledUnfunded
\* the flip is one config write per provider until the owner funds it again
OneWritePerProvider == \A p \in Providers : writes[p] <= 1
\* once flipped, a provider is funded again only by OwnerFund (action property)
StaysUnfundedUntilOwner ==
    [][\A p \in Providers : (~funded[p] /\ funded'[p]) => OwnerFund(p)]_vars
=============================================================================
