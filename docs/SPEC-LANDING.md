# SPEC: Landing the generated ledgers

This names the set of generated ledgers the lander merges rather than refuses, and the
rule by which a conflict in one is resolved. It is the one place the set lives: the brief
generator and the lander read it through `cardgen.LedgerInPaths` / `cardgen.AlwaysInPaths`,
so a file a change must touch to keep the tree green is inside every card's PATHS.

## The ledger set

The built-in set is:

- the roadmap data and the pages generated from it: `docs/fixes.sexp`, `FIXES.md`,
  `docs/roadmap.sexp`, `ROADMAP.md`;
- the per-card evidence ledgers: every `.sexp` under `docs/roadmap-evidence/` (one entry
  per card, regenerated).

A repository may name more with a `LEDGERS` line in its `AGENTS.md` or `nova.sexp`, read
at staging. Absent such a line, the built-in set stands.

`cardgen.LedgerInPaths(p)` is the predicate; `cardgen.AlwaysInPaths(p)` is true for a
ledger as it is for a test, a `testdata/` file, `tla/RUNS.tsv`, `tla/CASES.tsv`,
`internal/docs/catalog.go` and every `AGENTS.md` map.

## The merge rule

When a landing merges a card's head and the only unmerged paths are ledgers, the lander
resolves them instead of returning the card to rework:

1. **Data** (`docs/fixes.sexp`, `docs/roadmap.sexp`, and the `docs/roadmap-evidence/*.sexp`
   entries): merge entry by entry. Two sides that each add an entry in the same release
   block keep both, in file order, no duplicate; each side's additions and removals are
   kept. A side that changes a line both sides change, or an addition inside an entry the
   other side removes, is refused.
2. **Pages** (`FIXES.md`, `ROADMAP.md`): take the tip's side, then regenerate from the
   merged data with the owning tests' update mode (`NOVA_CI_UPDATE=1`), the same run
   `make roadmap` makes, until it changes nothing.
3. **Evidence ledgers** (`docs/roadmap-evidence/*.sexp`): merge the entries as the data
   above; where the entry's `make` target exists, regenerate what the entry generates.

A ledger conflict the lander cannot resolve (a malformed entry) stops the stream with the
path and both entries in the judgment, as `merge --conflict-kind ledger` does today; it
never reworks the card. A conflict outside the ledger set is refused as before.
