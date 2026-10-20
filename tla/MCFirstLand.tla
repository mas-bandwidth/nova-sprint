----------------------------- MODULE MCFirstLand -----------------------------
\* The instance: two cards of one stream; the base tip moves once; a card may
\* spend one attempt on its own red; one file in PATHS, one file outside it;
\* one registry hunk. The control is Broken = "none"; each MCFirstLandBroken*
\* configuration is one reversed witness of FirstLand.tla.
EXTENDS FirstLand, TLC
=============================================================================
