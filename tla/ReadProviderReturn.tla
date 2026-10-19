----------------------------- MODULE ReadProviderReturn -----------------------------
\* A read handed back on a provider failure spends its reader only at Cap of them
\* (internal/sprint/read_cards.go readSpender, ProviderFailureReturn, ProviderReadCap;
\* the read return path internal/sprint/friend_read.go Read). It is computed from the
\* read cards themselves, so a return recorded before the rule spends nothing either.
\*
\* The state, for one reader and one attempt of a primary: ends, the sequence of how
\* each generation of the reader's read card ended ("verdict", "returned" by the reader's
\* own hand, "late", or "provider", a return on a rate limit, a 5xx or a timeout); open,
\* whether a card of the reader is placed. The outside: the deal places a card when the
\* reader is not spent; the reader ends it any of the four ways.
\*
\* Broken = "none" is the design. Every other value is a reversed witness:
\*   "providerspends" a provider return spends the reader at once (the code before
\*                    nova-sprint v1.6.6, which left cards "cannot ask" after one 429 from
\*                    each reader): ProviderBelowCapIsAskable
\*   "nocap"          a provider return never spends, so a provider that never recovers
\*                    deals the read to the reader forever: ProviderReturnsBounded

EXTENDS Naturals, Sequences

CONSTANTS Cap, MaxGen, Broken

VARIABLES ends, open

vars == <<ends, open>>

Ends == {"verdict", "returned", "late", "provider"}

ProviderCount == Len(SelectSeq(ends, LAMBDA e : e = "provider"))

OwnEnd == \E i \in 1..Len(ends) : ends[i] \in {"verdict", "returned", "late"}

Spent ==
    \/ open
    \/ OwnEnd
    \/ (Broken = "providerspends" /\ ProviderCount > 0)
    \/ (Broken # "nocap" /\ ProviderCount >= Cap)

Init == ends = <<>> /\ open = FALSE

Deal ==
    /\ ~Spent
    /\ Len(ends) < MaxGen
    /\ open' = TRUE
    /\ UNCHANGED ends

End(e) ==
    /\ open
    /\ open' = FALSE
    /\ ends' = Append(ends, e)

Next == Deal \/ \E e \in Ends : End(e)

Spec == Init /\ [][Next]_vars

TypeOK == open \in BOOLEAN /\ ends \in Seq(Ends)

\* Only provider returns, fewer than Cap: the reader may be dealt the read again.
ProviderBelowCapIsAskable ==
    (~open /\ ~OwnEnd /\ ProviderCount < Cap /\ Len(ends) < MaxGen) => ENABLED Deal

\* A reader is dealt one read at most Cap times on provider returns alone.
ProviderReturnsBounded == ProviderCount <= Cap

\* After an end of the reader's own, it is never dealt the read again.
NeverDealtAfterOwnEnd == OwnEnd => ~open
=============================================================================
