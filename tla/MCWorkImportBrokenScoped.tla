---------------------------- MODULE MCWorkImportBrokenScoped ----------------------------
\* The "scoped" witness: three repositories. The import's scope is {r1, r2},
\* and a third repository r3 holds an issue (e2) the import did not fetch. A
\* verify asked for all three repositories is refused before any read (r3
\* outside the tree's scope); Broken = "scoped" drops that refusal, so the
\* wider verify compares only the tree's scope and reports done, failing
\* VerifyNeverPassesOutsideItsScope.
EXTENDS WorkImport

\* The repository of each issue: internal in r1, e1 in r2, e2 in r3.
MCRepo == [i \in Issues |-> IF i \in Internal THEN "r1" ELSE IF i = "e1" THEN "r2" ELSE "r3"]
==========================================================================================
