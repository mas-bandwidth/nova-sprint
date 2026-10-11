----------------------------- MODULE LandLedgers -----------------------------
\* The ledger-only landing (docs/SPEC-SPRINT.md section 7, the shared ledgers land;
\* cmd/nova-sprint/land.go mergeHeadLedgers, landappend.go, landsexp.go,
\* land_ledgers.go, land_forms.go). A head's merge stops on a set of unmerged paths.
\* When every one is a shared ledger (Ledgers: cardgen.LedgerInPaths) the lander
\* resolves them, each by its key, and the merge lands if the resolution and the
\* lander's checks (the merged text reads back, decodes, regenerates) pass; it is
\* refused with the reason if they do not. A path outside the set (code) refuses the
\* head as before. This module is the guard the landing stage of Land.tla abstracts
\* as one Build step: Land.tla's "lander" conflict kind is ResolveFails here.
\*
\* Broken names a reversed witness: "anyconflict" resolves whatever conflicted
\* (CodeConflictNeverLands fails), "nocheck" lands a resolution the checks refused
\* (OnlyCheckedResolutionsLand fails), "refuseledgers" is the lander before
\* v1.2.13, which refused a ledger-only conflict (LedgerOnlyLands fails).
EXTENDS FiniteSets, TLC

CONSTANTS Cards, Paths, Ledgers, Broken

ASSUME Ledgers \subseteq Paths
ASSUME Broken \in {"none", "anyconflict", "nocheck", "refuseledgers"}

VARIABLES
  conflict,   \* card -> the paths its merge stopped on ({} merges clean)
  checks,     \* card -> the resolution and the lander's checks pass
  state,      \* card -> "queued" | "landed" | "refused"
  landedWith  \* card -> the conflicted paths it landed with

vars == <<conflict, checks, state, landedWith>>

TypeOK ==
  /\ conflict \in [Cards -> SUBSET Paths]
  /\ checks \in [Cards -> BOOLEAN]
  /\ state \in [Cards -> {"queued", "landed", "refused"}]
  /\ landedWith \in [Cards -> SUBSET Paths]

Init ==
  /\ conflict \in [Cards -> SUBSET Paths]
  /\ checks \in [Cards -> BOOLEAN]
  /\ state = [c \in Cards |-> "queued"]
  /\ landedWith = [c \in Cards |-> {}]

LedgerOnly(c) == conflict[c] # {} /\ conflict[c] \subseteq Ledgers

\* a merge with no conflict lands as git made it
LandClean(c) ==
  /\ state[c] = "queued" /\ conflict[c] = {}
  /\ state' = [state EXCEPT ![c] = "landed"]
  /\ UNCHANGED <<conflict, checks, landedWith>>

\* the guard: every conflicted path is a shared ledger
ResolveGuard(c) ==
  CASE Broken = "anyconflict"   -> conflict[c] # {}
    [] Broken = "refuseledgers" -> FALSE
    [] OTHER                    -> LedgerOnly(c)

\* the lander resolves the ledgers and lands the merge, once its checks pass
ResolveLand(c) ==
  /\ state[c] = "queued"
  /\ ResolveGuard(c)
  /\ checks[c] \/ Broken = "nocheck"
  /\ state' = [state EXCEPT ![c] = "landed"]
  /\ landedWith' = [landedWith EXCEPT ![c] = conflict[c]]
  /\ UNCHANGED <<conflict, checks>>

\* the resolution or a check refused it (one id both sides changed differently, a
\* merged text that does not decode): refused with the reason
ResolveFails(c) ==
  /\ state[c] = "queued" /\ LedgerOnly(c) /\ ~checks[c]
  /\ Broken # "nocheck"
  /\ state' = [state EXCEPT ![c] = "refused"]
  /\ UNCHANGED <<conflict, checks, landedWith>>

\* a conflict outside the ledgers (or, before v1.2.13, any conflict) is refused as before
Refuse(c) ==
  /\ state[c] = "queued" /\ conflict[c] # {}
  /\ ~ResolveGuard(c)
  /\ state' = [state EXCEPT ![c] = "refused"]
  /\ UNCHANGED <<conflict, checks, landedWith>>

Next == \E c \in Cards : LandClean(c) \/ ResolveLand(c) \/ ResolveFails(c) \/ Refuse(c)

Spec == Init /\ [][Next]_vars /\ \A c \in Cards : WF_vars(LandClean(c) \/ ResolveLand(c) \/ ResolveFails(c) \/ Refuse(c))

\* a code conflict never lands: a landed card's conflicted paths are all ledgers
CodeConflictNeverLands == \A c \in Cards : state[c] = "landed" => landedWith[c] \subseteq Ledgers

\* a resolution lands only when its checks passed
OnlyCheckedResolutionsLand == \A c \in Cards : state[c] = "landed" /\ landedWith[c] # {} => checks[c]

\* a refusal is never of a ledger-only conflict whose checks pass
NoLedgerOnlyRefusal == \A c \in Cards : state[c] = "refused" => ~(LedgerOnly(c) /\ checks[c])

\* every ledger-only conflict whose checks pass lands
LedgerOnlyLands == \A c \in Cards : (LedgerOnly(c) /\ checks[c]) ~> state[c] = "landed"

\* every card is decided
AllDecided == <>(\A c \in Cards : state[c] # "queued")
=============================================================================
