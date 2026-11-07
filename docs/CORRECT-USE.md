# Correct use, enforced by the tool

nova-sprint enforces correct use instead of relying on a model to remember it. Every verb checks
what must already be true, refuses if it is not, and says what to do first. A new AI that does not
know the tool is walked through the intended order every time: it types `nova-sprint` and is told
the next command.

This page is the policy. [LAYER-6-RUN.md](LAYER-6-RUN.md) describes the verbs that implement it;
layers 1 to 3 refuse the moves that are about state.

## The one rule

Every verb runs its prerequisite table before doing anything. A prerequisite that does not hold is
a refusal: exit 1, one line, in the nova-fleet form:

```
SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>
```

A name, id or sha that does not exist (a row, a card, a stream, a commit) is a refusal naming it,
never an empty answer.

Every successful verb ends with one line, `next: <the command to run now>`, or
`next: nothing; wait for the bus` when the next move is someone else's.

Exit codes: 0 done; 1 refused, with the fix; 2 could not run (bad usage, or the store, bus or an
edge failed), with the fix. Never a silent zero.

`nova-sprint` with no arguments does not print usage. It runs `nova-sprint next --as <me>`: it
looks at the machine, the bus and the store, and prints the one thing to do now with its command.
`help` and `-h` exist for reading, not for finding the way.

An unknown outcome (a reply lost after a commit) is handled inside the verb: it replays by op id and
prints the real result. A caller never sees an op id.

## The chain

Each step refuses until the one before it has happened.

| step | verb | refuses until | the refusal names |
|---|---|---|---|
| 1 | `check` | (reports; its own fix per red line) | every line it checked; each `CHECK RED` line names its fix; exit non-zero while any is red |
| 2 | `up` | nova-tools' fleet is up (`nova-fleet check` green); nova-tools at or above the pin; the caller is on the bus | the first step that does not verify, with its fix |
| 3 | `stream add` | the store is at the schema `up` makes; the repository is readable and its base is there; the card branches `sprint/<s>/<card>` can be created (git's ref-name rules; no branch `sprint` or `sprint/<s>` at origin); with `--forge github`, this host's gh identity can write the repository and the group branch can be created | `nova-sprint up`; the name to change, the branch to delete, or the gh login to make |
| 4 | `add` | the stream exists and its base is at origin; the brief passes the card contract ([CARD-CONTRACT.md](CARD-CONTRACT.md)); a row serves its tier and every NEEDS-ROUTE; its PATHS are at the base; its work is not already there; every need names a card | `stream add <s>`; the missing sections of the brief by name; the row to give the tier or route; the path; the unknown need |
| 5 | rows | each row hooks in: `nova-bus hook --as <name>`, then `nova-sprint hook --as <name> --role worker\|coordinator` (a bench runs `nova-sprint worker --as <row>`, which hooks itself) | the hook-in recipe |
| 6 | deals | `run` is a service (installed by `up`), the sprint is started (`start`), a policy is active, and a row is hooked, up and has room | `nova-sprint up`; `nova-sprint start`; "no row is hooked in" with step 5's recipe |
| 7 | `take` | the name is hooked in; the card was dealt to this row at this generation | the hook recipe; "not dealt to this row at this generation: `next --as <row>`" |
| 8 | `finish` | this row holds the card; the head is a full sha; it is the tip of the card's branch at origin; every commit on it is made as the sprint's commit identity | the holder; "not a full sha"; "push first: `git push origin <branch>`"; the identity's two `git config` lines |
| 9 | `read` | the reader is hooked; the reader is not the author; the head is the card's | the author; the producer's current head |
| 10 | landing | a read said yes to this head; CI is green at this head (or the coordinator lands it by hand) | a `land` judgement with the facts and `judge ... land\|fix\|retry`; see [LANDING.md](LANDING.md) |

Steps 7 to 10 are also refused by the card machine on state (a finish before a take, a read by the
author, a landed card moving), with the holder and the state named. The chain is enforced twice: by
the verb's table before it acts, and by the engine when the move is applied.

## Hooked before anything that acts as you

`take`, `finish`, `read`, `release`, `beat` and every other verb that acts as a name refuse until
that name is on the bus (`nova-bus who`) and hooked in to the sprint, and name the hook-in
recipe. Coordinator verbs additionally need the name to be the sprint's one
coordinator. A row that cannot be reached cannot hold work.

## The deal message is the walkthrough

The bus message a deal sends is the whole instruction:

```
DEAL card=<id> attempt=<n> kind=work|read stream=<s> tier=<t> model=<m> epoch=<e> repo=<r> base=<sha> branch=<b> ... brief=<clipped>; in your hand now: run nova-sprint take <id>@<gen> --epoch <e> --as <you> (acknowledges, prints the whole brief), then do it and run: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>
```

The card is in the row's hand from the deal. `take` is the acknowledgement: it stops the nudges and
prints the brief, the repository, the base commit, the branch this row may push, the done-when
lines, the commit-identity rule, and ends with
`next: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>` (or `read ... yes|fix`
for a read). A row that does not acknowledge is nudged every tick and, at the policy's `ready_take`,
the card is dealt elsewhere or an `unacked` judgement is opened. Nothing is carried in a model's
memory between steps; every output names the next command.

A friend hooked with `nova-bus hook --as <you> --exec 'nova-sprint wake --as <you>'` has its
deals taken before its session wakes: each `take` line in a message from the sprint is replaced by
the `TAKEN` receipt.

## Finish is checked, not trusted

`finish` refuses, naming which:

- a row that is not the holder (naming who is), or a generation that is not current;
- a head that is not a full 40-hex sha;
- a head that is not the tip of the card's branch at origin (`git push origin <branch>` first);
- a head equal to where the attempt started (nothing was done);
- a commit since the base whose author or committer email is not the sprint's commit identity
  (compared case-folded; the refusal names up to ten commits and both emails).

It asks for no PR, no forge and no token: the landing opens the stream's PR.

## One commit identity for the whole sprint

A repository's contributor agreement (CAA, CLA) keys off the forge account, and its allowlist holds
accounts, never people. So every agent, friend and bench worker of a sprint commits as one
configured identity: the git `user.name` and `user.email` of an account on the allowlist, typically
its noreply address. A contributor's own name goes only in the message, as a `By: <name>` line and a
`Co-Authored-By: <name> <email>` trailer.

The identity is the sprint's state, never a literal in the code. The coordinator sets it with
`nova-sprint identity --name "<git user.name>" --email "<git user.email>" --reason "<why>"`, and the
tool enforces it in order: `up` step 7 asks the human for it; `hook --role worker` and `take` refuse
until it is set, and both print the rule and the two `git config` lines (the `take` receipt carries
`commit_name` and `commit_email` for the bench worker, which writes them into the job's clone);
`finish` refuses a commit by any other identity, with the fix (the two config lines and a
`--reset-author` rebase); `check` has a line for it, red when unset. `stream add` and `check` warn
when the base requires a contributor-agreement check, naming the identity to sign.

## Keep everybody at width

The promise is parallel to width: every friend and bench busy while there is work. Cards decide
whether that can happen; the deal cannot deal a card that waits.

- **Flat chains.** A NEEDS only when the card consumes the other's output, never for order or
  overlap. A chain of N cards lands one after another while the rest of the fleet waits. `add`
  prints the stream's longest NEEDS chain once the cards are in (`CHAIN ...`), and a `WARN CHAIN`
  from 4 cards (`steps.ChainWarn`) with what it takes at the stream's land rate; `--dry-run` prints
  it before anything is added. It warns, never refuses: flatten it before `start`.
- **Concrete PATHS.** Overlap is a file both cards name (a glob or a directory both add to holds
  nothing); a card waiting behind another's file is "held behind <file> of <card>". List the files a
  card edits.
- **One card per file set.** Cards that edit the same file run one at a time; split work so
  independent cards name different files.

Before `start`, and whenever the fleet is under width, run `nova-sprint check` and read its lines:
the width line (a `WARN ... nothing dealable: N chains (longest K, top: <card> blocks M), H
overlaps` when nothing is ready), one `waits <stream>:` line per stream ("53 wait on <card>
(<state>); 3 held behind <file> of <card>"), and a `WARN stream <s>: its longest NEEDS chain is K
cards` line per long chain. The board's Why waiting panel and the steps' idle judgement say the same
thing from the same function (`steps.Waits`), which groups each stream's waiting producers by the
front of the NEEDS chain they wait on and by the producer and file they are held behind, and finds
the longest chain of cards not landed (sentinels add no length). The land rate behind the hours
estimate is the stream's landings over the store's last 5,000 events of the epoch; under two
landings it says "no land rate yet".

## Notifications name their verb

Every judgement that reaches the coordinator (a red CI, a producer sent back by its reads, a
producer blocked after too many bounces, a row that flapped, a card that cannot land) is a stored
record, pushed from that record, and carries the exact command that answers it:

```
NEEDS YOU id=land.fix-typos.1: card fix-typos (...) did not land: ci_red: ...; run: nova-sprint judge land.fix-typos.1 <choice> --reason <why>   choices: land | fix | retry
```

`judge` is the one verb that answers a judgement, and its choices are those the judgement offers
(LAYER-6-RUN section 7). The board has no "needs you" column; the coordinator's `next` prints the
oldest open judgement, and the bus push carries the same line.

## What `next` looks at, in order

1. Can the store be read? If not: the nova-fleet command, `up`, or the `nova-secrets exec` line.
2. Is a name given? If not: choose one.
3. Is this name on the bus? If not: the `nova-bus hook` recipe.
4. Is this name hooked in? If not: `nova-sprint hook --as <name> --role worker`.
5. Does this name hold a card? If so: the step it is at and its command.
6. (Coordinator) is a judgement open? If so: its command, oldest first.
7. Is the sprint stopped? If so: `start` for the coordinator, wait for a worker.
8. Is there a stream, and cards in it? Otherwise `stream add`, `add`, or `nothing needs you`.

## There is one way

Everything that needs someone to act moves by push, over nova-bus, and there is one way.

- Every change of state that needs someone is one bus message to that someone, sent by the run loop
  after the change commits: a deal to the row; a withdrawal; a landing, a finished stream and a
  judgement to the coordinator. The message carries the one command that answers it.
- Nobody polls. `next` is for a cold start or a lost session; the mechanism is the hook waking the
  session.
- nova-sprint sends by running `nova-bus send` and reads presence with `nova-bus who`. It never
  opens Redis, never defines a stream, never keeps an inbox, never retries delivery on its own: a
  failed send is resent from the unmoved push cursor on the next tick.
- The coordinator is a row like any other: hooked in, driven by pushes, with judgement verbs instead
  of `take` and `finish`.
- The sprint's own bus hook answers only with receipts. A reply (a `re` set, or a REPLY subject) is
  never answered; a subject that is not a verb is logged once on its stderr and gets no reply; a
  verb sent to `sprint` is answered once with the command to run instead, because the sprint takes
  no verbs over the bus.

## What the tool refuses early

What can be found at `add` is found at `add`, and each refusal names its one fix (the list is in
[CARD-CONTRACT.md](CARD-CONTRACT.md), "Admission"). `stream add` proves the repository, the base,
the card branch names and the forge identity; `add` proves each producer against the roster and the
base tree; `hook` proves the store credential and that every stream's base is readable from this
process, and asks no forge token of a worker; `up` step 7 says VERIFIED only when every worker
bench runs this version and its worker has probed its harness for the policy's model ids and its
seat's provider keys; `restore` prints an `ADMIT` line per restored waiting or ready producer and
refuses a card record whose state this build does not map; `check` re-proves all of it, one line
each, and also says: no policy active (the tick would run on the built-in default and deal
nothing), a policy that fails a question the tick asks, and a working card whose row is unhooked,
down or off the bus.

A DONE-WHEN is one argv, not a shell line: `add` and `check` refuse a backticked chain, `a && b`, a
pipe, a redirection or a sentence, and the fix says to wrap it in `sh -c '...'`. The same parser
runs the gate on the bench.

`drop` (and `replace`, and a judgement answered `drop`) releases the dropped producer's live
consumers itself, in the same op, recorded released with the drop's reason (no result, no blame).
