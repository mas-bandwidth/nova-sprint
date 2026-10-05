# Designing a durable sprint program

This is an implementation note, dated 2026-10-05, about keeping a sprint's
plan in a file: repositories, issues, streams, cards, needs, and sentinels.
It separates that durable plan from live assignments, leases, and presence.

**The round-trip is not built in this checkout.** The five `nova-sprint work`
verbs parse their flags and exit 1 without opening a tree or Redis. Their
refusal is `the work-tree round-trip is not built; nothing was read and nothing was written`.
The rest of this note describes the intended contract, not working commands
for an unattended run.

This is separate from the existing GitHub issue mirror in
[the work-tree specification](../../docs/SPEC-WORK-V1.md). For an introduction
to the product, use [the README](../../README.md); for the implemented command
surface, use [the command reference](../../docs/CLI.md).

The note was written against nova-sprint main at
`2763dbb12e72230245d97e7dd024b4696c889f40`. It describes the `internal/work`
package and does not replace the sprint or GitHub-mirror contracts.

## Three records with different jobs

The split plan (2026-10-04) says folding nova-work is `nova-sprint work
import` and `nova-sprint work verify`. What is built agrees.
`cmd/nova-work` has `import` and `verify`. `docs/SPEC-WORK-V1.md` is their
spec: one `(work-tree "v1" ...)` file, a GitHub issue mirror.
`internal/workfile` is that file. That import reads GitHub and writes the
mirror. That page's export (section 1.7, not built) re-opens issues a
destructive mode closed. It does not hold cards, streams, needs or sentinels.

Those names stay on that tool. `nova-work import` and `nova-work verify`
stay on `nova-work`. `work verify` is not a nova-sprint verb. `nova-sprint
work import` is a different act: it loads a sprint program. The two imports
are not one verb. This note does not move the mirror.

The words repos, issues and roadmap are not verbs of `cmd/nova-work`.
`work roadmap` here is an inspection of the sprint program below. It is not
the `(:roadmap 1 ...)` files already in the work repository, and it does not
rewrite them.

`mas-bandwidth/work` is the record. Its README says it holds the cross-repo
sexp, the ingest map and fold receipts, and no code. The tree at
`6667c0de0a718dc35607e8119c613dda39bfda81` (the names of its files, read
2026-10-05) holds `README.md`,
`coordinator/rules-from-memory-2026-10-05.md`, a dashboard snapshot under
`dashboard/live-2026-10-05/`, ratings under `ratings/tools-dev-2026-10-05/`,
two roadmap sexps (`roadmaps/nova-sprint-v1.1.0.sexp`,
`roadmaps/nova-tools-v1.3.0.sexp`) and Redis DUMP snapshots under
`sprints/nova-tools/`. No `(sprint-program ...)` file. No `(work-tree "v1")`
file. The roadmap form is `(:roadmap 1 :repo ... :releases ...)`, cards cut
from sprint epoch 15 for a later release, each card keeping its brief. The
dumps are backups of a live Redis store. This design does not read the
dumps, does not replace them, and does not rewrite a roadmap file.

What was asked on 2026-10-04, the lisp tree as the backing store, and what
no file does today: repos, issues, cards, streams, needs and sentinels
round-trip. Export writes the sprint's program and state. Import loads a
program. That is the design below. The code of it is not in this change.

## What is in the tree

One s-expression file, a sprint program, not a `(work-tree "v1")` and not a
`(:roadmap 1 ...)`. A file whose top form is `work-tree` or `roadmap` is
refused, not loaded as a program. The top form is `(sprint-program "v1" ...)`.
It is canonical: keys in one order, names sorted, and the file's SHA-256
names the bytes. `internal/worklang` reads it and never evaluates it.
Nothing is written by this change, so no file of this shape exists yet.

| record | what it holds |
| --- | --- |
| root | `:exported` (RFC3339), `:epoch` (the Redis epoch the export read) |
| `:repos` | `owner/name`, url, base branch. Redis has no repo table. |
| `:issues` | `owner/name`, number, title, state. Identity only: not a body, not comments, not the GitHub mirror. |
| `:streams` | name, read-tier |
| `:cards` | id, stream, kind primary, column, score, held, needs, brief; a landed card also has its landed head |
| `:sentinels` | id, stream, needs, released. A sentinel is not also listed as a card. |

Needs are the edges on a card and on a sentinel, the same ids the work table
stores on the card. They are not a third table.

Redis has no repo table and no issue table. On the first export, a repo is a
card brief's `REPO:` line, and an issue is an issue URL the brief names. A
brief with neither adds no repo and no issue. A repo or an issue no brief
names is the coordinator's to add. That writer is not built.

Columns that round-trip as themselves are `waiting`, `ready` and `landed`,
plus the held bit, plus a sentinel's released bit. A column that means a live
holder (`working`, `review`, `merging`) is written as `ready`. The holder,
the generation and the lease are not in the tree. Loading `working` with no
member would leave a card nobody holds. A dropped card is not in the tree.
It stays in the Redis log.

## What stays only in Redis

The live machinery, and anything that is a lease or a pulse:

- the fleet, friends, readers and merge tables: presence, beats, width, load, read cards in flight, the merge queue, CI in flight
- takes, generations, operation ids, and the pending fence
- the log, the inbox, the cursor, and judgments
- costs and usage
- routes and provider funds
- the machine running or stopped, and the ETA
- every epoch but the number recorded on the export, including what `clear` left behind
- the Redis DUMP files already in `mas-bandwidth/work`
- the `(:roadmap 1 ...)` files already in `mas-bandwidth/work` (a later release, not this program)

A secret stays out of the tree. The writer, when it exists, refuses a PEM
private key, a GitHub token, an `sk-ant-` key or a JWT rather than write it.
A brief is the program and is in the tree; a secret pasted into a brief is
refused with the brief, not stored. This note holds no secret.

## When it is written

The proposed export is explicit. A tick, an ordinary verb, or a server restart
would not write the file. The five work verbs execute on the caller's machine
because that is where the tree file lives.

`work export` is the only writer of a sprint program. It reads one pinned
epoch and writes the file `--tree` names. It does not commit and does not
push. The caller commits in `mas-bandwidth/work`. It does not write a roadmap
file and it does not write a Redis dump.

`work import` is the only loader of a sprint program. It loads a program into
a sprint that has no card (after `init`, or a fresh epoch) and refuses when
any card is already on the table. It does not resume a lease. A column the
export wrote as `ready` comes back `ready`. It does not load a
`(work-tree "v1")` and it does not load a `(:roadmap 1 ...)`.

`work repos`, `work issues` and `work roadmap` read the sprint-program file
and write nothing, and they do not open Redis. Roadmap is the streams, cards,
needs and sentinels in that file.

None of that runs yet. Each of the five verbs parses its flags, including
`--tree`, and exits 1. The line is `REFUSED: the work-tree round-trip is not built; nothing was read and nothing was written`. Stdout is empty. `--tree` is not opened. Redis is not opened.

Today the five are reads, because a run changes nothing. When the round-trip
is built, `work export` and `work import` are the coordinator's. `work
repos`, `work issues` and `work roadmap` stay reads.

## What this change does not do

No program file is written or read. This package states the design and does
not read or write a tree or a store. `cmd/nova-work` is unchanged.
`internal/workfile` is unchanged. The files in `mas-bandwidth/work` are
unchanged. No model of the round-trip is checked beside this note. The rules
that model would have to keep are the ones above: the tree holds only the
records in the table, the tree holds no lease, import into a sprint that
already has a card is refused, a live column is stored as `ready`, and a
`work-tree` or `roadmap` top form is refused.
