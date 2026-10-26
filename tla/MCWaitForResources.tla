---------------------- MODULE MCWaitForResources ----------------------
EXTENDS WaitForResources
\* two members, two readers, two routes; the clock runs 0..1 in whole steps
\* (MaxNow > HoldFor, so every lease age saturates past its deadline), and
\* each deadline bound in the module is at most MaxNow.
MCMembers == {"m1", "m2"}
MCReaders == {"r1", "r2"}
MCRoutes  == {"t1", "t2"}
\* the liveness instance: one of each keeps TLC's temporal branches inside the
\* bench budget (the same device as MCBenchLeaseLive.cfg)
MCMembersLive == {"m1"}
MCReadersLive == {"r1"}
MCRoutesLive  == {"t1"}
=========================================================================
