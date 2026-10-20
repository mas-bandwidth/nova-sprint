---- MODULE MCReadMix ----
EXTENDS ReadMix, TLC
\* readers of a class, and models, are interchangeable: the instance is checked up to them
Sym == Permutations(Friends) \cup Permutations(Fleet) \cup Permutations(Models)
====
