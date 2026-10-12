# nova-sprint 1.2.15

DRAFT: this file lists what v1.2.15 contains so far. The cut reviewer fills the
sections marked "to be confirmed at the cut" and resolves every line marked
"(pending: PR not yet merged)" before the tag.

nova-sprint 1.2.15 is a point release of fixes, one concern each, paired with
Nova Tools 1.2.15. It carries what landed on the comfort stream after the
1.2.14 cut. Every entry is in [FIXES.md](../FIXES.md) under v1.2.15.

## Upgrading

To be confirmed at the cut. No ordered upgrade steps are known yet.

## Landing

- **The LandPass model covers stream-only landing and serialized trunk
  certification.** The model keeps its 13 legacy gate-pass cases and adds a
  two-stream, three-card abstraction of stream routing, empty branches and
  certification progress, with eight negative cases and a liveness check that
  every landed batch is eventually integrated. (#139)
- **`brief --set-base` takes a review card.** (#143, pending: PR not yet
  merged)
- **`stream set --base` reaches every unlanded card,** and is refused
  atomically while any affected card's landing is marked. (#143, pending: PR
  not yet merged)
- **An empty stream branch follows its trunk,** and a branch level with a trunk
  is never moved. (#143, pending: PR not yet merged)

## Also labelled v1.2.15 in the ledger

These entries carry the v1.2.15 label in `docs/fixes.sexp`, each with ledger
status "planned"; the cut reviewer confirms each is on the cut before the tag.

- Two cards sharing a path no lander-owned ledger covers are refused at
  admission.
- The lander merges the evidence ledgers entry by entry.
- The landing gate runs its classes side by side.
- A land pass pushes each repository and base on its own lane.
- Independent TLC cases run in a bounded parallel pool.
- An attempt that never ran is not evidence against the brief.
- A card that reads a sibling repository stages it from the mirror or the
  forge.
- A PATHS-PROPOSED line reaches the seat whole.
- A producer card carries its consumers' history and the check that names the
  stall.
- Tests that failed on a loaded bench are made deterministic by their real
  cause.
- Every stall and stuck class of 2026-10-11 is held by a unit test and a
  functional test in release-accept.

## Landing in this cut

- **`max_attempts` in nova-config defaults to 4,** with a judgment at the
  bound. (pending: PR not yet merged)
- **The dashboard held cell equals the store.** (pending: PR not yet merged)
- **Readers survive a clear or are re-declared after it.** (pending: PR not yet
  merged)
- **The hook liveness functional test is deterministic.** (pending: PR not yet
  merged)

## What is not in this release

To be confirmed at the cut.
