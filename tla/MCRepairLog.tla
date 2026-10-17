------------------------------- MODULE MCRepairLog ------------------------------
\* One later manifest of two entries, c1 and c2, cut and pending. Each case
\* names its witnesses in Broken (RepairLog.tla).
EXTENDS RepairLog, TLC

CONSTANTS c1, c2

MCCards == {c1, c2}
=============================================================================
