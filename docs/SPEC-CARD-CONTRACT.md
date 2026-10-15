# SPEC-CARD-CONTRACT

This spec is the contract between a card's brief and the machinery that stages, runs
and lands it: what a brief must carry to be admitted, and what staging does with it.

## The shared-path rule

## The gate-host rule

A card brief may name a `GATE-NEEDS:` line that lists the gate's needs
(e.g., `GATE-NEEDS: linux, tla, landlock`). The deal matches a card to a
member only when the member's facts satisfy every need. A card no member
can satisfy is raised as one judgment naming the need; it is never
silently waited for. The brief lint refuses a brief that tells the worker
to ssh to a named host or run its gate "on hetzner2"; such a brief must
name the gate's needs on `GATE-NEEDS:` instead.

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

## The TEST line

A brief's `TEST:` line names the test the card's DONE-WHEN proves, in one of three forms
(`cardhdr.ParseTest`). The Go form `[-tags <tags>] <package> <TestName>` names one
repository-relative package path and a Go test name (`Test`, `Example` or `Fuzz`), run
`go test <package> -run <TestName>`; its `TestLine.Runner` is `go`. The ctest form
`ctest <TestName>` names one CMake test (letters, digits, `_`, `.`, `-` and `/`, no
spaces), run `ctest --test-dir build -R '^<TestName>$'`; its `Runner` is `ctest`, its
package empty, and its test is held by the brief's `PATHS:` checks, not to a package
directory. `none <why>` names no test and says why. Anything else is refused with the
remedy. The lint holds a ctest `TEST:` as it holds a Go one: the test must be absent from
the base tree (found as `add_test(NAME <TestName>` or `add_test(<TestName>` in a
`CMakeLists.txt` or `*.cmake` at the base, so an existing ctest test is refused), and a
ctest line in a repository with no `CMakeLists.txt` at the base is refused with that
reason. A brief's task, the body and the `TEST:` line it names, is the bound's text (CLI,
`brief`). Preflight's TEST check greps `*.go` for a Go line and `CMakeLists.txt` and
`*.cmake` for a ctest line.

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

## The DEPENDS-ON grammar (the card header, section 2)

This is the one place the grammar is written; [SPEC-SPRINT.md](SPEC-SPRINT.md) (section 2, the
card header; section 11, the add, brief, brief check and needs set verbs) and the help of those
verbs point here.

A need blocks the deal: a card is dealt only once every need has landed, so each `DEPENDS-ON:`
entry serialises two cards' whole runs. The lander already reworks a head that conflicts at the
base tip without stopping the stream, so two cards that touch one file rarely need an edge at all
(the shared-path rule above, which holds the cards of one many-brief `add` and takes a needs edge as its remedy, is the one place a file overlap still asks for an edge: write it as an `overlap:` entry with its reason, or add the cards in separate calls). The reason is therefore part of the
grammar, and an edge that consumes nothing cannot be written. The value of `DEPENDS-ON:` (or of a
`Needs:` line, which the readers take first) is `none`, `-`, or entries separated by commas
**outside parentheses**, each one of:

```
DEPENDS-ON: <id> (uses: <thing>[, <thing>...])
DEPENDS-ON: <id> (overlap: <file>; why: <reason>)
```

- **`uses:`** names what this card consumes from the needed card. Each thing is a repository path,
  a Go or TLA+ symbol, a verb or flag spelling, or a model name that the needed card produces. A
  path must be named by the needed brief's `NEW:` or `PATHS:` (a glob as the gate reads it,
  `diffcheck.Covers`; a directory above a file counts). A symbol, a spelling or a model name must
  appear in the needed brief's `NEW:`, `PATHS:` or task text (the body under the header) as a whole
  word or phrase.
- **`overlap:`** is an overlap kept because a conflict would cost a TLC model rerun or a full redo
  (two cards that both extend one `.tla` model, two that both rewrite the import blocks of the same
  files). The file must be named by both cards' `PATHS:` or `NEW:`, and `why:` gives the reason in
  words. Kept overlaps are counted: `add`, `brief` and `needs set` print `OVERLAP NEEDS <n>` for the
  call.
- A bare id, or an id with any other parenthesis, is neither form and is refused.

`swarm.LintDependsOn(raw, needed)` (pkg/swarm/lintdepends.go) holds a brief to it; each finding is
its own `LINT DRIFT card=<id> check=depends-uses line=<n>: ...` line naming the entry, the reason
and the line, and ending in a remedy that names both forms:

| finding | refuses |
|---|---|
| `depends-uses` (a) | an entry with neither `uses:` nor `overlap:` |
| `depends-uses` (b) | a `uses:` thing the needed card does not produce |
| `depends-uses` (c) | an `overlap:` with no `why:` in words, or whose file is not in both cards' `PATHS:` or `NEW:` |
| `depends-uses` (d) | rank words in place of a reason, on the `DEPENDS-ON:` line or in a sentence of the body that names an id on it: "best after", "independent", "not blocked", "if both are ready", and "after `<id>` lands" or "once `<id>` lands" for an id whose entry gave no `uses:`; an ORDER paragraph that names no id of the line draws nothing |
| `depends-unknown` | an id that is neither a card of the same add nor on the table, never a pass |

The callers are `add` (every brief of the add, a need looked up among the add's briefs before the
table), `brief` (the replacement text, a `DEPENDS-ON:`-only edit too; a `CARRY: head=` text, which
`--widen` writes from the card's own, is not held again), `brief check` (the checked files before
the table, the table when one is reachable) and `needs set` (each `--needs` entry, held as the
card's brief with its needs line replaced by them; `--clear` is never refused). One red entry
refuses the whole call, exit 2, nothing written. The card records the ids alone as its needs, and
its brief's needs line keeps each entry whole. A card already on the table keeps its needs: the
lint holds new text and does not rewrite the table. `add --needs` is not held by this lint.
