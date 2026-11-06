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

Each done-when is run at the base before the deal, and a gate that would
stay red after a correct fix is a brief defect, refused at add (LESSONS
C.7). Every gate's negative control plants its fault in the real artifact
the gate covers; a gate that skips, cannot compile, or excludes by default
fails the lint, because three v1 schema gates passed while checking nothing
(LESSONS C.8). The TEST line names a test absent at base, and the system
runs it red at base and green at head as a gate (LESSONS C.9). A gate names
the facts it needs (an OS, a tool), never a host: the lint refuses "ssh to
hetzner2" (LESSONS C.11). Every gate keeps its command, exit code and full
log; a finding names the path, and a read verdict without a file:line
finding is refused (LESSONS C.16).

## What the brief tells the worker

The brief tells the worker to cite the base-reds table instead of
re-running the tree at base, and a fix card never edits that table (LESSONS
C.17). It tells the worker to run the whole package of every file it
touches, never a subset (LESSONS C.18).

## Size and kind

Cards are pro-sized from the start: the design piece lands first, and each
later piece names a landed worked example; heavy is for design, model and
standard pieces only (LESSONS C.12; PRIORITY D38, T5). A card that writes a
new TLA+ model is admitted at heavy at least (PRIORITY T4). A card's base is
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
learned from v1." So the card contract above is a typed record, defined once
in one schema file: the markdown brief is a serialization of it, `add` parses
the brief into the record and the lint is the parser's refusals naming the
field; the store keeps as columns the fields the engine reads (needs, after,
paths, tier, priority, done-when) and the prose as text; the deal message, the
report and the dashboard feed carry the same record or parts of it, generated
for Go and the dashboard's language. Few records, at the edges and the card;
v1's 400k generated lines came from making every internal message a record,
and the engine's internals stay plain Go.
