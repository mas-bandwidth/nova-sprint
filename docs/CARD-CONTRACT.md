# The card contract

What a brief must be before `add` accepts it. Glenn, 2026-11-06: the quality
of the cards into the system "was a BIG PART" of whether a sprint went
smoothly or was rough. `add` lints every brief against this page on every
path, with no bypass, and refuses naming the section; `check` lints a brief
directory before a sprint starts (LESSONS C.1; CORRECT-USE.md step 4). Every
line below is a lesson v1 paid for, cited as LESSONS (section.line) or
PRIORITY (LESSONS-PRIORITY-AND-MERGE.md). Nothing here is new.

## What a brief has

A cold card is complete: closed PATHS, done-when as checkable commands, the
only ref the row may push, and, for a read, the sha and "do not edit"
(LESSONS C.13). Its line 1 is its tier (`tier: <t>`, one of flash, pro,
heavy, frontier); the card's stored field is authoritative after that, and a
card is at its tier from its first deal (PRIORITY T1, T2, T17). An optional
`PRIORITY: <level>` line declares its level, one of `low`, `normal`,
`high`, `critical` or `blocker`; without it the level is `normal`, and any
other spelling, `read` included (the rung of read consumers, never a
brief's), is refused (PRIORITY D5). Glenn 2026-11-06: "There is low, normal,
high, read, critical, blocker in specific order." "Default is normal." and
"Priorities are manually set by request.": the line is written by the
person or the coordinator who asked for that level, and nothing computes it.

## Paths

Every card declares its paths, so the lander never meets the E12 refusals
v1 met after every gate had passed (LESSONS C.2). PATHS is plain comma-separated paths, and scope
notes go on a NOTE line, never inside PATHS: a space-joined PATHS became one
bad entry, and a "(note)" inside it was read as a path (LESSONS C.3). PATHS
is written from the build: generated outputs, registry rows, pinned digests,
the tests and docs that pin the rule, and TLA cfgs, because v1 widened PATHS
by machine 57 times in one day (LESSONS C.4).

## Needs and after

There is one needs form and one parser, which refuses every other spelling:
in v1 `Needs: none` overrode `DEPENDS-ON:` and 15 dependants were dealt at
once (LESSONS C.5). A need is written only when the card consumes the other
card's output, never for file overlap or rank order: 151 of 466 v1 edges
were overlap only, and one held a whole tree (LESSONS C.6; PRIORITY D29).
A cycle through needs or after is refused at add, naming the loop (LESSONS
2.19). `after` is soft and orders landing only (LAYER-2-CARD-MACHINE.md;
LESSONS 2.15).

## Done-when and its gates

Admission parses each done-when as one command and runs nothing (below);
a bench's worker runs every done-when at the head after the work, and a
red one fails the attempt. A gate that would stay red after a correct fix
is a brief defect (LESSONS C.7) that no machine catches before the deal:
the author writes it red at base and green at head. Every gate's negative
control plants its fault in the real artifact the gate covers; a gate that
skips, cannot compile, or excludes by default checks nothing, as three v1
schema gates did (LESSONS C.8). The TEST line names a test absent at base,
red at base and green at head (LESSONS C.9). A gate names
the facts it needs (an OS, a tool), never a host: the lint refuses "ssh to
hetzner2" (LESSONS C.11). Every gate keeps its command, exit code and full
log; a finding names the path, and a read verdict without a file:line
finding is refused (LESSONS C.16).

## What the brief tells the worker

The brief tells the worker to cite the base-reds table instead of
re-running the tree at base, and a fix card never edits that table (LESSONS
C.17). It tells the worker to run the whole package of every file it
touches, never a subset (LESSONS C.18).

A brief never names a commit identity: the sprint has one (`nova-sprint identity`), which
hook and take print and the bench worker writes into the job's clone, and every commit of
every card is authored and committed as it; the contributor's own name goes only in a
`By:` line and a `Co-Authored-By:` trailer of the message. A brief that tells a worker to
commit as itself or as a row is wrong; `finish` refuses such a head (CORRECT-USE, "one commit
identity").

## Size and kind

Cards are pro-sized from the start: the design piece lands first, and each
later piece names a landed worked example; heavy is for design, model and
standard pieces only (LESSONS C.12; PRIORITY D38, T5). A card that writes a
new TLA+ model is written heavy at least (PRIORITY T4; the author's rule,
not checked at add). A card's base is
its stream branch, never another card's branch (LESSONS C.10). A read is
staged detached at the exact head with push disabled (LESSONS C.14). A card
whose work is already on its base is a duplicate brief, refused at add,
never dealt and failed as a worker's fault (LESSONS 3.22).

## Reports

A report is machine-read: verdict and full head first; a hold is a report
with its reason, not a commit message or a file (LESSONS C.15). A HOLD is
honest: LAND only with every done-when gate green at the exact head
(LESSONS C.19).

## The card is a structured record (decided 2026-11-06)

Glenn: "There is some value in having things *structured* that was an idea we
learned from v1." "As long as the *structure* exists and things aren't free form text, I'm happy." "So the structure we choose here is the SQL database." So the card contract above is a typed record, defined once
in `modules/store/schema.sql` (the table and its checks are the structure; the Go types mirror the columns; there is no second definition): the markdown brief is a serialization of it, `add` parses
the brief into the record and the lint is the parser's refusals naming the
field; the store keeps as columns the fields the engine reads (needs, after,
paths, tier, priority, done-when) and the prose as text; the deal message, the
report and the dashboard feed carry the same fields as JSON, and the dashboard reads the table;
the only free text is the prose a mind reads: the task itself and the reason lines;
v1's 400k generated lines came from making every internal message a record,
and the engine's internals stay plain Go.

## Admission: what add proves before a card exists (built 2026-11-07)

Intent rule 34: whatever can be found at add is found at add, and each
refusal names its one fix. `add` (dry run too) parses the brief, then proves
against origin and the roster; `check` re-proves every waiting or ready
producer the same way, and `restore` prints the same proof for the cards it
restored (an `ADMIT` line each). The parse refuses:

- **NEEDS-ROUTE: <name>** (repeatable). The card is dealt only to a row that
  serves every route it names. A machine serves a route through a
  `route:<name>` role in nova-config (`--roles bench,route:<name>`); kin
  serve no routes. The one parse is `card.NeedsRoutes`, which the tick, the
  unserved judgement and admission all read. Uppercase like every other key.
- **DONE-WHEN** that is not one command. A DONE-WHEN is one argv: words
  split at white space, a single or double quote joining what it encloses,
  run directly with no shell. So the line is refused (decided 2026-11-07,
  directive 77) when it carries shell syntax outside a quote (a backquote,
  `;`, `&`, `&&`, `|`, `||`, `<`, `>`, `(`, `)`, a backslash, `$(`, `${`, or
  `$` before a letter or `_`; a lone `$`, as in `^TestX$`, is a literal), when
  a quote is left open, or when its first word is not a program: a path (it
  holds a `/`), or a name that starts with a lower-case letter or digit, holds
  only letters, digits and `. _ + - @`, does not end in a dot, and is not a
  shell builtin (`cd`, `export`, `source`, ...). A sentence is caught by
  that form (`Verify the tests pass`, `tests, pass`), a lower-case sentence is
  not: what the coordinator can check is the form, because the bench's PATH
  is not readable from it. A name of the right form that is not on the bench
  is met where the gate runs, as the wall's own command-not-found. The fix
  line says to wrap a gate that needs a shell: `sh -c '...'`. The one parser
  is `edge.ParseGate`; admission (add, check, restore) and the worker's
  `RunTest` both call it, so what is admitted is what runs. Nothing is run
  at add.
- **Retired mechanics**, named with the line: a `WHO:`, `LOCK:` or `JOB:`
  key; a `JOB.md` the brief has the harness write; a lock (winlock, flock,
  lockfile, a bench or captain lock); `ssh to <host>`; a DONE-WHEN or TEST
  that runs ssh. The take receipt is the job, the deal is the who, and a gate
  names facts, never a host. `JOB.md` itself is the worker's file for its
  harness (the worker writes it in the job directory at stage; the prompt
  says to read it first), so a brief that sends the harness to read it
  passes.

Then, against the roster and origin:

- the stream is in the store and its base is at origin;
- a row up at width above zero serves the card's tier and every route;
- no DONE-WHEN or TEST names a roster bench as a word;
- each PATHS entry is at the base: a glob (`*`, `?`, `[`) matches a file or
  directory there; a literal exists, or its directory does, or its directory
  is declared in PATHS as `dir/` and that directory's parent exists (a new
  directory is declared, never implied);
- the work is not already at the base: the card's branch at origin, a HEAD
  line, or a PR's head is not an ancestor of the base tip, and the PR has not
  merged.

`stream add` (and `check`, per stream) also reads the status checks the base
branch requires (branch protection and rulesets, one `gh api` each) and,
when one is named like a contributor agreement (`CLA`, `CAA`, "contributor",
"assignment agreement", "license agreement"), prints a warning, never a
refusal: the check, and the sprint's one commit identity (`nova-sprint
identity`) that every bench and kin commits as, for the human to sign
before the first card lands. Decided 2026-11-07 (directive 63): a forge exposes who has signed
only where the agreement's bot does, and most do not, so the signed identities
are not listed and an unsigned one is not refused at add; the warning is the
closest correct thing. A required-checks read that fails is itself a warning.
Routing a stream's cards away from a row whose identity cannot land is the
deal's, not admission's, and is not built here.

`stream add` proves the card branches `sprint/<stream>/<card>` can be
created: the name passes git's ref-name rules, the base is not `sprint` or
`sprint/<stream>`, no branch at origin is their parent (git refuses a branch
under a branch), the base is at origin, and with forge github this host's gh
identity has write, maintain or admin on the repository.

A restore refuses a card record whose state this build does not map (one of
waiting, ready, working, review, merging, landed, fail, done; round 29
dropped the old problem state),
naming the record. A dump of another shape (a v1 store with a `landed`
timestamp column) is refused before that by the backup header's schema
hash; there is no import verb.
