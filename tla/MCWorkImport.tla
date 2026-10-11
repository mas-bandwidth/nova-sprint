---------------------------- MODULE MCWorkImport ----------------------------
\* The instance: two internal issues in one repository, one external issue in
\* a second; the control imports and verifies both repositories, the "scoped"
\* witness imports the two and verifies a third, empty repository besides.
EXTENDS WorkImport

\* The repository of each issue: internal issues in r1, external in r2.
MCRepo == [i \in Issues |-> IF i \in Internal THEN "r1" ELSE "r2"]
=============================================================================
