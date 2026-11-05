---------------------------- MODULE MCStoreFence ----------------------------
\* The instance of StoreFence.tla (tla/CASES.tsv, group storefence): two writers, two
\* caller operation ids and two argument sets, two manifests an operation, two attempts
\* a writer, two epochs, one stray write. MCStoreFenceSharedOpIds.cfg is the code as it
\* is (Nonce = FALSE) and fails NoPartialOutsideFence; MCStoreFenceRevivedRefusal.cfg
\* (Revive = TRUE) fails AbandonedAppliedNothing: the two holes. MCStoreFence.cfg and
\* MCStoreFenceLateWriter.cfg are the code with the proposed fix (Nonce = TRUE), with and
\* without the timing assumption; each MCStoreFenceBroken*.cfg is a reversed witness on
\* the fixed code.
EXTENDS StoreFence
=============================================================================
