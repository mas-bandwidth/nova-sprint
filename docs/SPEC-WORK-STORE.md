# The work tree, nova-sprint's durable store (SPEC-WORK-STORE)

Written 2026-10-04. This note says what the durable tree holds, what stays
only in Redis, and when the tree is written. The round-trip is not built.
`work repos`, `work issues`, `work roadmap`, `work export` and `work import`
are verbs of nova-sprint, and each run exits 1 having read nothing and
written nothing.

Branch-time base of nova-sprint: `15f13249e1474b2c943e88e75025b4bbe515d93b`.

## Where the plan and the trees disagree

The split plan (2026-10-04) says folding nova-work is `nova-sprint work
import` and `nova-sprint work verify`. The nova-sprint tree agrees about the
tool that is built. `cmd/nova-work` has `import` and `verify`.
[SPEC-WORK-V1.md](SPEC-WORK-V1.md) is their spec: one `(work-tree "v1" ...)`
file, a GitHub issue mirror. That import reads GitHub and writes the mirror.
That page's export (section 1.7, not built) re-opens issues a destructive
mode closed. It does not hold cards, streams, needs or sentinels.

Those names win for that tool. `nova-work import` and `nova-work verify` stay
on `nova-work`, and `work verify` is not a nova-sprint verb. `nova-sprint
work import` is a different act: it loads a sprint program. The two imports
are not one verb, and this note does not move the mirror.

The parenthetical verbs repos, issues and roadmap are not verbs of
`cmd/nova-work`. A deprecated nova-tools client had a `roadmap` session verb
among a large vocabulary. That client is not in this repository, and this
note does not adopt it. `work roadmap` here is an inspection of the sprint
program below.

`mas-bandwidth/work` is the record. Its README says it holds the cross-repo
sexp, the ingest map and fold receipts. The tree at
`84f54d79208f98f091c2e4c94f4ce2e7d218dbc7` does not. It holds `README.md` and
Redis DUMP snapshots under `sprints/nova-tools/` (`2026-10-02-mechanical`,
`2026-10-04-mechanical`). No lisp file. The files win over the README and
over a plan that treats a sexp record as already there. The dumps are backups
of the live Redis store. This design does not read them and does not replace
them.

What Glenn asked, and what neither tree does today: the lisp tree is the
durable backing store. Repos, issues, cards, streams, needs and sentinels
round-trip. Export writes the sprint's program and state. Import loads a
program. That is the design below. The code of it is not in this attempt.

## What is in the tree

One s-expression file, a sprint program, not a `(work-tree "v1")`. A file
whose top form is `work-tree` is refused, not loaded as a program. The top
form is `(sprint-program "v1" ...)`. It is canonical: keys in one order,
names sorted, and the file's SHA-256 names the bytes. `internal/worklang`
reads it and never evaluates it. Nothing is written by this attempt, so no
file of this shape exists yet.

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
member would leave a card nobody holds (SPEC-SPRINT.md section 9, rule 12).
A dropped card is not in the tree. It stays in the Redis log.

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

A secret stays out of the tree. The writer, when it exists, refuses a PEM
private key, a GitHub token, an `sk-ant-` key or a JWT rather than write it.
A brief is the program and is in the tree; a secret pasted into a brief is
refused with the brief, not stored. This note holds no secret.

## When it is written

Not on the tick. Not on each verb. Not at process start. Not by the server:
the five work verbs are not served, because the tree is a file on the machine
where the verb is typed.

`work export` is the only writer. It reads one pinned epoch and writes the
file `--tree` names. It does not commit and does not push. The caller commits
in `mas-bandwidth/work`.

`work import` is the only loader. It loads a program into a sprint that has
no card (after `init`, or a fresh epoch) and refuses when any card is already
on the table. It does not resume a lease. A column the export wrote as
`ready` comes back `ready`.

`work repos`, `work issues` and `work roadmap` read the file and write
nothing, and they do not open Redis. Roadmap is the streams, cards, needs and
sentinels in the file.

None of that runs yet. Each of the five verbs parses its flags, including
`--tree`, and exits 1. The line is `REFUSED: the work-tree round-trip is not
built; nothing was read and nothing was written`. Stdout is empty. `--tree`
is not opened. Redis is not opened.

Today the five are reads, because a run changes nothing. When the round-trip
is built, `work export` and `work import` are the coordinator's. `work
repos`, `work issues` and `work roadmap` stay reads.

## What this attempt does not do

No program file is written or read. There is no package under `internal/work/`.
`cmd/nova-work` is unchanged. The Redis dumps in `mas-bandwidth/work` are
unchanged. No model of the round-trip is checked in this attempt: `tla/` is
outside the paths this change may touch. The rules the model would have to
keep are the ones above: the tree holds only the records in the table, the
tree holds no lease, import into a sprint that already has a card is refused,
and a live column is stored as `ready`.
