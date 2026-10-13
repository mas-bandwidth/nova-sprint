----------------------------- MODULE MCHookCache -----------------------------
\* Two identities, two independent off-lock reads, and two invalidations per
\* identity; enough for stale shared and stale refill interleavings.
EXTENDS HookCache
=============================================================================
