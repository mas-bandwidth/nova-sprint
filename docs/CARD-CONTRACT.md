# Card contract

A card is a typed record in the store (`store/schema.sql` defines its table and checks; the Go types mirror the columns). Its markdown form is a brief. `nova-sprint add --stream <s> --brief-file <f>... [--dry-run]` parses each brief into the record, then proves it against origin and the roster ("Admission"). Every path through `add` runs the same lint; `check` re-proves every waiting or ready producer the same way, and `restore` prints an `ADMIT` line per restored card. Each refusal names the line or section and its fix.

The card id is the brief file's name without `.md`.

## Brief format

    tier: pro
    PATHS: store/x.go, store/x_test.go
    NEEDS: none
    AFTER: other-card
    PRIORITY: high
    TEST: go test -run TestX ./store
    DONE-WHEN: go test ./store
    DONE-WHEN: go vet ./store
    NEEDS-ROUTE: adb

    <task prose>

- Line 1 is `tier: <flash|pro|heavy|frontier>`. The tier is the card's minimum and is never lowered.
- Metadata lines follow, one `KEY: value` per line, up to the first blank line. A key is upper case; a value is not empty.
- A blank line, then the task prose (required, not empty).
- A key outside the table below is refused as `must be a recognized metadata field`. `DEPENDS-ON`, `DEPENDENCIES` and `NEED` are refused as an invalid NEEDS section: the only spelling is `NEEDS`.

| Key | Required | Repeatable | Value |
|---|---|---|---|
| `PATHS` | yes | no | Comma-separated plain paths relative to the repository root. No spaces, parentheses or notes inside an entry; no absolute path; no `..`. |
| `TEST` | yes | yes | The command naming the test the card adds. |
| `DONE-WHEN` | yes | yes | One command per line ("Gates"). |
| `NEEDS` | no | no | Comma-separated card ids, or `none`. |
| `AFTER` | no | no | Comma-separated card ids, or `none`. |
| `PRIORITY` | no | no | `low`, `normal`, `high`, `critical` or `blocker`; `normal` by default. `read` is refused: it is the rung of read consumers, never a brief's. |
| `NEEDS-ROUTE` | no | yes | One route name per line: 1 to 128 of `a-z A-Z 0-9 . _ -`. |
| `HEAD` | no | no | Full 40-hex sha of work the card already has. |
| `PR` | no | no | Pull request number (at least 1) of work the card already has. |

`WHO`, `LOCK` and `JOB` lines are refused ("Retired mechanics").

## Paths

PATHS lists every file the card may edit, written from the build: generated outputs, registry rows, pinned digests, the tests and docs that pin the rule, TLA configs. A worker edits only these files; `finish` and the landing do not extend them. Scope notes go in the task prose, never in PATHS.

Each entry must be at the stream's base:

- A glob (`*`, `?`, `[`) must match a file or directory there.
- A literal path must exist, or its directory must.
- A new directory is declared as its own PATHS entry ending in `/`, and that directory's parent must exist.

## Needs and after

`NEEDS` is written only when the card consumes the other card's output, never for file overlap or order. A need keeps the card `waiting` until the needed card lands. A cycle through `NEEDS` or `AFTER` is refused, naming the loop. An unknown id is refused by name.

`AFTER` is soft: it orders landing among cards both queued to land and never holds a start.

Overlap between cards is not a need. A card whose PATHS name a file also named by a started card of its stream waits until that card lands; `show <id>` names the card and file.

`add --needs`, `--after` and `--priority` set the same fields from flags.

## Gates

Each `DONE-WHEN` is one argv: words split at white space, a single or double quote joining what it encloses, run directly with no shell. A line is refused when:

- it carries shell syntax outside a quote: a backquote, `;`, `&`, `&&`, `|`, `||`, `<`, `>`, `(`, `)`, a backslash, `$(`, `${`, or `$` before a letter or `_` (a lone `$`, as in `^TestX$`, is literal);
- a quote is left open;
- its first word is not a program: a path (it holds a `/`), or a name that starts with a lower-case letter or digit, holds only letters, digits and `. _ + - @`, does not end in a dot, and is not a shell builtin (`cd`, `export`, `source`, ...).

The fix line is `write the gate as one command ...; wrap a gate that needs a shell: sh -c '...'`. A sentence (`Verify the tests pass`) fails the form check; a lower-case sentence is not detectable, and a name of the right form that is not on the bench fails where the gate runs. `edge.ParseGate` is the one parser: `add`, `check`, `restore` and the worker's test runner all call it, so what is admitted is what runs. Nothing is run at `add`.

A gate names facts it needs (an OS, a tool), never a host: a DONE-WHEN or TEST that names a bench from nova-config as a word, or runs `ssh`, is refused.

Gate requirements on the brief author (not mechanically checked at `add`):

- A gate that would stay red after a correct change is a brief defect; a worker reports it with `finish --failed` and the coordinator replaces the brief.
- Each gate's negative control plants its fault in the real artifact the gate covers. A gate that skips, cannot compile, or excludes by default checks nothing.
- `TEST` names a test absent at the base: red at the base, green at the head.

## What the task prose tells the worker

- Cite the base-reds table instead of re-running the tree at the base; a fix card never edits that table.
- Run the whole package of every file touched, never a subset.
- Name a landed worked example for every piece after the first.
- Never name a commit identity. The sprint has one (`nova-sprint identity`), which `hook` and `take` print and the bench worker writes into the job's clone; every commit of every card is authored and committed as it, and the contributor's own name goes only in a `By:` line and a `Co-Authored-By:` trailer. A brief that tells a worker to commit as itself or as a row is wrong; `finish` refuses such a head.

## Size and kind

Cards are pro-sized: the design piece lands first and each later piece names a landed worked example. `heavy` is for design, model and standard pieces; a card that writes a new TLA+ model is at least heavy. A card's base is its stream branch, never another card's branch.

A read is staged detached at the exact head with push disabled. A report is machine-read: a hold is a report with its reason, not a commit message or a file; a read verdict `fix` without a `<path>:<line>:` finding is refused.

## Admission

`add` (dry run too) parses the brief, then proves:

**From the brief alone**

- Retired mechanics, each named with its line and refused with the fix `remove it: the deal names the row, the take receipt is the job, a gate names the facts it needs (an OS, a tool) and never a host, and nothing is locked by hand`:
  - a `WHO:`, `LOCK:` or `JOB:` key, or the text `JOB.md`;
  - a lock (`winlock`, `flock`, `lockfile`, `captain lock`, `bench lock`);
  - `ssh to|into|on <host>`;
  - a DONE-WHEN or TEST that runs `ssh`.
- A DONE-WHEN that is not one command (above).
- A tier line outside `flash|pro|heavy|frontier`, a missing `PATHS`, `TEST` or `DONE-WHEN`, or empty task prose.

**Against the roster (nova-config) and origin**

- The stream is in the store and its base is at origin.
- A row at width above zero serves the card's tier and every `NEEDS-ROUTE` route. A machine serves a route through a role `route:<name>` in nova-config (`nova-config machine set <bench> --roles bench,route:<name> --actor <you> --reason <why>`); kin serve no routes. The fix when no row serves the tier: `nova-config friend set <name> --tiers ...,<tier>`, give a bench cores, or change the tier line.
- The work is not already at the base: the card's branch at origin, a `HEAD` line, or a `PR`'s head is not an ancestor of the base tip, and the PR has not merged. Otherwise the card is a duplicate and is refused.

## Streams

`nova-sprint stream add <s> --repo <org/repo> [--base main] [--mode auto|group|hand] [--forge github]` refuses when:

- the stream exists;
- the base branch is not at origin;
- the card branch names `sprint/<s>/<card>` cannot be created: the name fails git's ref-name rules, the base is `sprint` or `sprint/<s>`, or a branch at origin is their parent (git refuses a branch under a branch);
- with `--forge github`, this host's `gh` identity lacks write, maintain or admin on the repository.

It also reads the base branch's required status checks (branch protection and rulesets, one `gh api` each). When one is named like a contributor agreement (`CLA`, `CAA`, "contributor", "assignment agreement", "license agreement"), it prints a warning, never a refusal: the check name, and the git identities the benches author commits as (`<row> <row@nova-sprint.local>`, from nova-config's benches with cores), for the human to sign before the first card lands. A forge exposes who has signed only where the agreement's bot does, so unsigned identities are not refused. A required-checks read that fails is itself a warning. `check` repeats these per stream.

## Restore

A restore refuses a card record whose state this build does not map (one of `waiting`, `ready`, `working`, `review`, `merging`, `landed`, `fail`, `blocked`, `done`), naming the record. A dump of another schema is refused earlier by the backup header's schema hash.
