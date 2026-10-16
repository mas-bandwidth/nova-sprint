------------------------- MODULE StagingRepoWindow -------------------------
\* Repository staging capability (internal/sprint/staging_repo.go).
\* Three recorded refusals replay space's delta-audit incident. A fresh card
\* of that repository must go elsewhere until age > Window or a matching ok.
\* Ages abstract passed-in time; all three takes have the same cause, chosen
\* independently at Init. Other failures remain per-card. No git, credentials,
\* capacity, stores, or timing of an already-running card are modelled.
EXTENDS Naturals, FiniteSets
CONSTANTS Window, BrokenCardOnly
VARIABLES refused, cause, age, cleared, deals
vars == <<refused, cause, age, cleared, deals>>
Members == {"space", "b1"}
Cards == 1..5
Repo(c) == IF c <= 4 THEN "delta-audit" ELSE "other"
Causes == {"mirror", "write", "auth", "publickey", "other"}
RepositoryCauses == Causes \ {"other"}
Active == refused # {} /\ cause \in RepositoryCauses /\ ~cleared /\ age <= Window
Barred(m, c) == m = "space" /\ Repo(c) = "delta-audit" /\ Active
\* The old rule bars credential refusals only on their individual cards.
Eligible(m, c) ==
  /\ ~(m = "space" /\ c \in refused)
  /\ IF BrokenCardOnly /\ cause # "mirror" THEN TRUE ELSE ~Barred(m, c)
Init ==
  /\ refused = {}
  /\ cause \in Causes
  /\ age = 0
  /\ cleared = FALSE
  /\ deals = {}
Refuse ==
  /\ Cardinality(refused) < 3
  /\ refused' = refused \cup {Cardinality(refused) + 1}
  /\ age' = 0
  /\ cleared' = FALSE
  /\ UNCHANGED <<cause, deals>>
Tick ==
  /\ age < Window + 1
  /\ age' = age + 1
  /\ UNCHANGED <<refused, cause, cleared, deals>>
\* Only an ok by space of this repository clears its last refusal.
FinishOK ==
  /\ refused # {}
  /\ cleared' = TRUE
  /\ UNCHANGED <<refused, cause, age, deals>>
Deal(m, c) ==
  /\ Cardinality(refused) = 3
  /\ Eligible(m, c)
  /\ deals' = deals \cup {[member |-> m, card |-> c, barred |-> Barred(m, c)]}
  /\ UNCHANGED <<refused, cause, age, cleared>>
Next == Refuse \/ Tick \/ FinishOK \/ (\E m \in Members, c \in Cards : Deal(m, c))
Spec == Init /\ [][Next]_vars
TypeOK ==
  /\ refused \subseteq 1..3
  /\ cause \in Causes
  /\ age \in 0..(Window + 1)
  /\ cleared \in BOOLEAN
  /\ deals \subseteq [member : Members, card : Cards, barred : BOOLEAN]
\* Admission evidence is frozen at deal time: a later refusal cannot turn an
\* earlier permitted deal into a violation, nor can a later ok erase one.
NoBarredRepositoryDealt == \A d \in deals : ~d.barred
=============================================================================
