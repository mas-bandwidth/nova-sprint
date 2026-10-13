---------------------------- MODULE MCClearDefaults ----------------------------
EXTENDS ClearDefaults, TLC

\* the two clears are interchangeable: the pass config checks one of each pair of states
\* that differ only by which clear is which
ProcPerms == Permutations(Procs)
=============================================================================
