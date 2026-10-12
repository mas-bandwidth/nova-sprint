# SPEC-CARD-CONTRACT

This spec is the contract between a card's brief and the machinery that stages, runs
and lands it: what a brief must carry to be admitted, and what staging does with it.

## The shared-path rule

Two cards that name the same file in their `PATHS:` lines are refused at admission
unless one of two forms admits them. The lander merges a ledger entry by entry, so two
cards writing the same ledger do not collide; and a card that needs another lands after
it, staged at its tip, so two cards chained by a needs edge edit the file in turn. A path
two cards share that neither form covers — no lander-owned ledger, no needs edge — is
refused at admission with the overlap named: both card ids and the path. There is no flag
that admits it.

- **A ledger the lander merges** admits the shared path. The lander's ledgers are the files
  `cardgen.LedgerInPaths` covers — `docs/fixes.sexp`, `FIXES.md`, `docs/roadmap.sexp`,
  `ROADMAP.md`, `tla/CASES.tsv`, `tla/RUNS.tsv`, `tla/COVERAGE.tsv` and the
  `docs/roadmap-evidence/*.sexp` evidence ledgers — so two cards that name one of them in
  `PATHS:` are admitted.
- **A needs edge** admits the shared path: one card names the other in its `Needs:` (or
  `DEPENDS-ON:`) header, so the later card lands after the earlier and is staged at its tip.

A `SHARED:` header line does not admit a path the lander does not merge: the line declares
a file two cards know they both touch, but the lander merges only ledgers, so two cards
that both name the same non-ledger file still collide and are refused the same way.

The refusal and the plan review are the lint's: `nova-sprint add` of several cards lints
the set as a plan before any card is written — it prints the overlap table (path, the cards
that name it, ledger or not, the needs edge if any) and refuses the whole set on the first
non-ledger overlap without an edge, writing nothing. `nova-sprint add --plan <dir>` prints
the table and exits 0 without admitting, for a seat that wants to read the overlaps first
(see [SPRINT-COORDINATOR.md](SPRINT-COORDINATOR.md), the plan review).

## The RESULT-line tier

A brief's RESULT line (`RESULT: <id> sha=<sha12> tier: <t>`) names the tier the card is
stored with and dealt on. The tier is read by the lint and the store; a card brief whose
line 1 names no tier is refused by the lint with a tier-line finding. `brief --tier`
still changes it later, with `--reason` as today.

## The SIBLINGS header

A brief that names a sibling path (`../<name>`, a sibling repository by name) in its
text is refused at admission unless it carries a SIBLINGS header naming the repositories
the path reads. The header is a line of the shape

    SIBLINGS: owner/name@ref, owner/name@ref

with the entries comma separated, each `owner/name@ref`. The sibling's name (the part
of `owner/name@ref` after the slash) must cover every sibling path the text names: a
brief that reads `../serialize`, `../serialize.c` and `../serialize.cs` names three
siblings, and its SIBLINGS header must name all three. A brief that names a sibling path
without the header, or with a header that does not cover the path, is refused at admission
with the path quoted.

The header is how the card is staged (docs/SPEC-STAGING.md, the siblings): the member
clones each SIBLINGS repository at its ref into `$JOB/siblings/<name>` and points the
harness there from `JOB.md`.
