------------------------- MODULE ReadBranchMissing -------------------------
\* The judgment "a read's branch is not on origin" (NReadBranchMissing,
\* internal/sprint/read_missing.go) and the verbs its decisions offer
\* (internal/sprint/notes.go Decisions; the ask's closes and ReworkResolves in
\* internal/sprint/steps_review.go). On the live cards of 2026-10-15 (j16495,
\* j16496) the decisions offered "rework with a fix" and the plain ask, and the
\* rework refused the judgment ("resolves no obligation") while the ask said both
\* "not asked yet" and "asked already". The module keeps two rules:
\*   OfferedDecisionResolves: every verb the judgment offers, run while it is open,
\*     either closes it or is refused naming a verb that does;
\*   AskRefusalsAgree: plain ask and ask --another decide from one state
\*     (askStateAt), so two refusals never give different reasons.
\* The judgment is raised only when no branch of the work on origin holds the head
\* (missingBranchUnit), it holds the primary's reads while it is open
\* (readBranchMissingOpen), and the ask re-asks the read, closing it, once a branch
\* on origin holds the head.

EXTENDS Naturals, FiniteSets

CONSTANT ReworkKeeps   \* TRUE: the reversed witness, rework does not close the judgment

VARIABLES present,     \* the card is on the table (drop takes it off)
          judgment,    \* "open" | "closed"
          held,        \* a branch on origin holds the read's head
          asked        \* the read was asked again (the ask closed the judgment)
vars == <<present, judgment, held, asked>>

Verbs == {"ack", "rework", "ask", "drop"}

\* The decisions the judgment offers (notes.go Decisions[NReadBranchMissing]).
Offered == Verbs

Init == present = TRUE /\ judgment = "closed" /\ held = FALSE /\ asked = FALSE

\* missingBranchUnit: a broken read's branch is not on origin and no branch holds the head.
Raise ==
  /\ present /\ judgment = "closed" /\ ~held /\ ~asked
  /\ judgment' = "open"
  /\ UNCHANGED <<present, held, asked>>

\* The seat pushes the branch (or the work's other branch comes to hold the head).
BranchAppears ==
  /\ present /\ ~held
  /\ held' = TRUE
  /\ UNCHANGED <<present, judgment, asked>>

\* What a verb does to the open judgment, and what a refusal names.
Closes(v) ==
  CASE v = "ack"    -> TRUE
    [] v = "drop"   -> TRUE
    [] v = "rework" -> ~ReworkKeeps          \* ReworkResolves lists NReadBranchMissing
    [] v = "ask"    -> held                  \* AskResolves, when a branch holds the head

\* A refusal names the verbs that resolve it (rework with a fix, drop).
RefusalNames(v) == IF v = "ask" /\ ~held THEN {"rework", "drop"} ELSE {}

Ack ==
  /\ present /\ judgment = "open"
  /\ judgment' = "closed"
  /\ UNCHANGED <<present, held, asked>>

Rework ==
  /\ present /\ judgment = "open"
  /\ judgment' = IF Closes("rework") THEN "closed" ELSE "open"
  /\ UNCHANGED <<present, held, asked>>

\* ask: re-asks the read and closes the judgment when a branch holds the head;
\* refused (changing nothing) when none does.
Ask ==
  /\ present /\ judgment = "open"
  /\ IF held THEN judgment' = "closed" /\ asked' = TRUE
             ELSE judgment' = judgment /\ asked' = asked
  /\ UNCHANGED <<present, held>>

Drop ==
  /\ present /\ judgment = "open"
  /\ present' = FALSE /\ judgment' = "closed"
  /\ UNCHANGED <<held, asked>>

Next == Raise \/ BranchAppears \/ Ack \/ Rework \/ Ask \/ Drop
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ present \in BOOLEAN /\ held \in BOOLEAN /\ asked \in BOOLEAN
  /\ judgment \in {"open", "closed"}

\* The one ask state (askStateAt): why the primary is not asked now, "" when it is.
\* plain ask: the judgment holds it until a branch holds the head; --another: the
\* read must be re-asked first, so it refuses too, from the same state.
AskReason(another) ==
  IF judgment = "open"
    THEN (IF ~held THEN "branch-missing" ELSE IF another THEN "reask-first" ELSE "")
    ELSE ""

\* Every offered verb, run while the judgment is open, closes it or is refused naming
\* a verb that does.
OfferedDecisionResolves ==
  judgment = "open" =>
    \A v \in Offered : Closes(v) \/ (RefusalNames(v) \cap {w \in Verbs : Closes(w)}) # {}

\* Plain ask and --another never both refuse with different reasons.
AskRefusalsAgree ==
  (AskReason(FALSE) # "" /\ AskReason(TRUE) # "") => AskReason(FALSE) = AskReason(TRUE)

=============================================================================
