----------------------------- MODULE MCGateRing -----------------------------
\* The instance: two benches, three gates a pass, two passes; the oracle over
\* every host and gate is enumerated at Init, and the ring starts at h1 (the
\* batch's key hashes to it). The configurations set Broken and the invariants.
EXTENDS GateRing

MCRing == <<"h1", "h2">>
=============================================================================
