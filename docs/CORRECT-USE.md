# Correct use, enforced by the tool

Glenn, 2026-11-06: "The key is #1. This is how we enforce correct usage,
instead of relying on LLMs to unreliably remember to do the right thing." And:
"Make a new AI, unsure of how to use nova-sprint, get walked through the
actual best practice way to use it, every time."

This page is the policy. Layer 6 (the verbs) implements it; layers 1 to 3
already refuse the moves below that are about state. Nothing here relies on an
AI remembering anything: every verb checks what must already be true, refuses
if it is not, and says what to do first.

## The one rule

Every verb runs its prerequisite table before doing anything. A prerequisite
that does not hold is a refusal, exit 1, one line, in the nova-fleet form:

```
SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>
```

Every successful verb ends with one line, `next: <the command to run now>`,
or `next: nothing; wait for the bus` when the next move is someone else's.

`nova-sprint` with no arguments does not print usage. It runs `nova-sprint
next --as <me>`: it looks at the machine, the bus and the store, and prints
the one thing to do now with its command. A cold AI types the tool's name and
is told what to do. `help` and `-h` exist for reading, not for finding the
way.

## The chain

Each step refuses until the one before it has happened.

| step | verb | refuses until | the refusal names |
|---|---|---|---|
| 1 | `check` | nova-tools' fleet is up (`nova-fleet check` green: Tailscale, Redis, PostgreSQL); `NOVA_PG_DSN` and `NOVA_BUS_REDIS` reach their servers | the nova-tools command for each missing piece, one line each |
| 2 | `up` | `check` passes | `nova-sprint check` |
| 3 | `stream add` | the store is at the schema `up` makes | `nova-sprint up` |
| 4 | `add` | the stream exists; the brief passes the card contract; every need names a card | `stream add <s>`; the missing sections of the brief by name; the unknown need |
| 5 | rows | a row hooks in (`nova-sprint worker --as <row>` on a bench; `nova-bus hook --as <name>` for a kin) | the recipe for the harness it detects |
| 6 | deals | `run` is a service (made by `up`) and at least one row is up | `nova-sprint up`; "no row is hooked in" with step 5's recipe |
| 7 | `take` | the name is hooked in; the card was dealt to this row at this gen | the hook recipe; "card X moved: run `nova-sprint show X`" |
| 8 | `finish` | this row holds the card; the head is a full sha; it exists on the card's branch at origin; the PR exists when the card wants one | the holder; "not a full sha"; "push first: `git push origin <branch>`"; "open the PR" |
| 9 | `read` | the reader is hooked; the reader is not the author; the head is the card's | the author; "card X is at <head>" |
| 10 | landing | a read said yes to this head; CI is green at this head | "read owed"; "CI red at <head>: see <url>" |

Steps 7 to 10 are also refused by the card machine on state (a finish before a
take, a read by the author, a landed card moving), with the holder and the
state named. The chain is enforced twice: by the verb's table before it acts,
and by the engine when the move is applied.

## Hooked before anything that acts as you

As nova-bus does: `take`, `finish`, `read`, `release`, `run` and every other
verb that acts as a name refuse until that name is hooked in on the bus, and
print the hook-in recipe for the harness they detect. A row that cannot be
reached cannot hold work.

## The deal message is the walkthrough

The one bus message a deal sends is the whole instruction. Its body:

```
DEAL card=<id> attempt=<n> kind=work|read stream=<s> epoch=<e>
run: nova-sprint take <id>@<gen> --as <you>
```

`take` prints the brief, the repository, the base commit, the branch this row
may push, the done-when lines, where tests may run, and ends with
`next: nova-sprint finish <id> --head <sha> --as <you>` (or `read … yes|fix`
for a read). Nothing is carried in a model's memory between steps; every
output names the next command.

## Finish is checked, not trusted

`finish` refuses, naming which: a head that is not forty hex characters; a
head not on the card's branch at origin; a branch other than the one dealt; a
card that wants a PR and has none at that head; a row that is not the holder,
naming who is. v1 accepted `finish --head abc` and printed OK (Johnny,
2026-11-06); v2 never does.

## Unknown outcomes never leak

The store's unknown outcome (a reply lost after commit) is handled inside the
verb: it replays by op id and prints the real result. An AI never sees an op
id. Three exit codes: 0 done, 1 refused with the fix, 2 could not run with
the fix. Never a silent zero.

## Notifications name their verb

Every judgement that reaches the coordinator (a red CI, a fix read, a row
blocked, a card that cannot land) carries the exact command that answers it:

```
NEEDS YOU card=<id>: CI red at <head>: see <url>
run: nova-sprint rework <id> --reason "<why>"   or   nova-sprint drop <id> --reason "<why>"
```

The dashboard's "needs you" column shows the same line. The coordinator's
`next` prints the oldest one.

## What `next` looks at, in order

1. Is nova-tools' fleet up? If not: the nova-fleet command.
2. Is the store at the schema this binary knows? If not: `up`.
3. Is this name hooked in? If not: the recipe.
4. Does this name hold a card? If so: the step it is at and its command.
5. Is a judgement owed to this name? If so: its command.
6. Is a stream empty or everything landed? If so: `add`, or `done`.
7. Otherwise: `nothing; wait for the bus`.
