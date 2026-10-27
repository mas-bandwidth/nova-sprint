---------------------------- MODULE MCStoreFence ----------------------------
\* The instance of StoreFence.tla (tla/CASES.tsv, group storefence): two writers, two
\* caller operation ids and two argument sets, two manifests an operation, two attempts
\* a writer, two epochs. Broken = "none" with Timely = TRUE holds every invariant; each
\* MCStoreFenceBroken*.cfg is a reversed witness, and MCStoreFenceLateWriter.cfg drops the
\* timing assumption.
EXTENDS StoreFence
=============================================================================
