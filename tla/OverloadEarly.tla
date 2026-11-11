---------------------------- MODULE OverloadEarly ----------------------------
\* REVERSED WITNESS of Overload.tla: the load watch takes one sample over
\* the bound as overloaded (no span), so a spike opens a judgement.
\* OpenOnlyWhenSustained must be violated.
EXTENDS Overload

FactEarly(r) == IF cur[r] = "over" THEN "over" ELSE Fact(r)   \* FAULT: was Fact(r)

TickEarly ==
  /\ ~ticked
  /\ ticked' = TRUE
  /\ overT' = [r \in Rows |-> O2(r)]
  /\ underT' = [r \in Rows |-> U2(r)]
  /\ fact' = [r \in Rows |-> FactEarly(r)]
  /\ sustained' = [r \in Rows |-> IF O2(r) >= Span THEN TRUE ELSE IF U2(r) >= Span THEN FALSE ELSE sustained[r]]
  /\ js' = [r \in Rows |-> StepJ(r, FactEarly(r))]
  /\ UNCHANGED <<cur, up, quiet, env>>

NextEarly == TickEarly \/ Again \/ \E r \in Rows : Beat(r) \/ UpDown(r) \/ Answer(r) \/ QuietEnds(r) \/ Lapse(r)

SpecEarly == Init /\ [][NextEarly]_vars /\ WF_vars(TickEarly)
=============================================================================
