# Command reference: nova-sprint, and the tools folding into it

Moved from nova-tools docs/CLI.md on 2026-10-04.

## nova-sprint

nova-sprint: a sprint of work cards, dealt to a fleet of workers and read before they land

One store (Redis or a twin file) holds the work, readers, merge and fleet
tables and the `sprint` view. A card is one unit of work in a stream. Each tick
deals ready cards to members (machines with a width), sends finished work to
readers and queues passed work for merging by stream. Decisions it cannot
make go to the coordinator's inbox. The contract is
[SPEC-SPRINT.md](SPEC-SPRINT.md).

### First run

Try one card's whole flow with no Redis or git. `--redis mem:<file>` (or
`NOVA_SPRINT_REDIS=mem:<file>`) loads an in-memory twin from a file and saves it
after each command. A twin is for learning and tests; run one command at a
time. The first line sets the store and actor. `finish` without `--head` and
`merge` stand in for a worker's pushed commit and its landing, so this flow
needs no forge:

```sh
export NOVA_SPRINT_REDIS=mem:sprint.twin NOVA_SPRINT_ACTOR=boss
nova-sprint init --readers reader-a,reader-b --members m1
nova-sprint add --stream s1 --count 1 --one
nova-sprint start
nova-sprint tick
nova-sprint tick
nova-sprint take --as m1 --epoch 0
nova-sprint finish --as m1 s1-1.w1@1 --epoch 0 --report done
nova-sprint tick
nova-sprint read --as reader-a --begin --epoch 0
nova-sprint read --as reader-a --ok --epoch 0
nova-sprint tick
nova-sprint merge --stream s1 --batch 1
```

A twin beats every member and reader at every verb, so `m1` is up after the
first tick. Nothing runs between commands: tick by hand with `nova-sprint
tick`. `run`, `inbox --wait` and `where --watch` are refused. A card's move is
queued until the next tick prints `MOVED drain`; the tick after `merge`
completes its move to landed. The flow's output shows results, moves (`MOVED`),
refusals with reasons (`REFUSED`, on stderr), and the sprint's summary
(`landed/all percent -> ETA ...`).
[TESTS.md](TESTS.md#nova-sprint) carries the exact transcript through `merge`;
`cmd/nova-sprint/firstrun_test.go` runs it line by line.

### Verbs

```
nova-sprint init [--readers <a,b,...>] [--members <m1[:<width>],m2,...>] [--coordinator <name>] [--rules <file>]
nova-sprint add --stream <s> (<id>... | --count <n> | --sentinel <id> | --brief-dir <dir> | --brief-file <f1> --brief-file <f2>...: a card per file, its id the file's name without .md) [--needs <a,b>] [--before <id> | --after <id> | --score <n>] [--brief <text> | --brief-file <path>: once, the brief of the cards named] [--rules <file>]
nova-sprint quack --streams <a,b,...> --count <n> --repo <clone url> [--tiers <t,...>] [--base <branch>]
nova-sprint release <sentinel>... --reason <text> [--answers <note>]
nova-sprint resolve [<id>...] [--stream <s>] [--limit <n>]
nova-sprint start
nova-sprint stop
nova-sprint run
nova-sprint tick
nova-sprint goal set <name> [--file <path>] [--to file:<path>]
nova-sprint goal show [<name>]
nova-sprint goal drop <name>
nova-sprint take --as <member> [<card>@<gen>...] [--epoch <n>] [--limit <n>]
nova-sprint finish --as <member> <card>@<gen>... --epoch <n> (--head <commit> | --failed) [--report <text>] [--usage <text>]
nova-sprint ask [<id>... | --group <id> [--expect <n>]] [--stream <s>] [--limit <n>] [--another] [--answers <note>]
nova-sprint queue --as <reader|member> | --stream <s>
nova-sprint read --as <reader> (--begin | --ok | --broken) [<card>...] --epoch <n> [--limit <n>] [--finding <text>] [--usage <text>] | --as <reader> --return <card> --reason <text> --epoch <n> [--usage <text>]
nova-sprint accept (<id>... | --stream <s> | --read-ok | --group <id> [--expect <n>]) [--answers <note>]
nova-sprint rework (<id>... | --group <id> [--expect <n>]) [--fix <text>] [--answers <note>]
nova-sprint return (<id>... | --group <id> [--expect <n>]) [--reason <text>] [--answers <note>]
nova-sprint drop (<id>... | --stream <s> --col <state> | --group <id> [--expect <n>]) --reason <text> [--answers <note>]
nova-sprint rank <id>... (--score <n> | --first) [--answers <note>]
nova-sprint brief <id> (--brief <text> | --brief-file <path>) [--rules <file>] | <id> --tier <flash|pro|heavy|frontier>
nova-sprint move <id>... --stream <s> [--before <id> | --after <id> | --score <n>]
nova-sprint merge --stream <s> [--batch <n>] [--conflict <id> | --cross <id>=<other> | --red [--suspect <id>...] | --rejected] [--note <text>]
nova-sprint land [--stream <s>...] [--repo-dir <clone>] [--base <branch>] [--check <command>] [--dry-run]
nova-sprint resume --stream <s> [--did <text>] [--answers <note>]
nova-sprint fleet beat <member> [--load <percent>]
nova-sprint fleet up <member> [--width <n>]
nova-sprint fleet down <member>
nova-sprint fleet sync [--check] [--pg <dsn>]
nova-sprint fleet level
nova-sprint friend sync [--pg <dsn>]
nova-sprint friend beat <friend> [--working <n>] [--queue <n>] [--width <n>] [--running <id>,...] [--load <percent>]
nova-sprint friend down <friend> [--reason <text>] [--until <RFC3339>]
nova-sprint friend up <friend> [--width <n>]
nova-sprint friend take <friend> (<id>... | --all-unstarted) [--reason <text>]
nova-sprint friend level
nova-sprint friend health <friend> --state up|asleep|down --seen <RFC3339> --generation <n> [--queue <n>] [--working <n>] [--width <n>] [--reason <text>] [--until <RFC3339>]
nova-sprint reader add <reader>...
nova-sprint reader away <reader>...
nova-sprint reader up <reader>...
nova-sprint reader remove <reader>...
nova-sprint stream remove <stream>...
nova-sprint ci <id>... (--red | --green) --epoch <n> [--head <h>] [--run <id>] [--source <s>] [--note <text>]
nova-sprint wait <note> (--for <duration> | --until <RFC3339>)
nova-sprint ack <note>... --reason <text>
nova-sprint answer [--dry-run] [--bar <p>] [--every <duration>] [--timeout <duration>] [--backend jev|fixed] [--answers <file>] [--record <file>]
nova-sprint inbox [--open <group>] [--read] [--wait [--timeout <duration>]] [--deadline <duration>] [--stale <duration>]
nova-sprint card <id>
nova-sprint log [--card <id>] [--stream <s>] [--member <m>] [--since <10m|RFC3339>] [--at-epoch <n>]
nova-sprint check
nova-sprint repair
nova-sprint where [--watch] [--every <duration>] [--all] [--json [--cards]]
nova-sprint view coordinator [--all] [--since <cursor>] [--json]
nova-sprint view worker --as <member|friend> [--since <cursor>] [--json]
nova-sprint dashboard [--listen <address:port>[,...] | none] [--pull <address:port>[,...] | none] [--logo <file>] [--every <duration>]
nova-sprint seat
nova-sprint routes
nova-sprint funded <provider> --reason <text>
nova-sprint stats
nova-sprint play [--simulation] [--seed <n>] [--every <duration>] [--broken <p>] [--fail <p>] [--stuck <p>] [--cross <p>] [--down <p>] [--up <p>] [--red <p>] [--flap <p>] [--batch <n>] [--hold] [--silent <member>@<from>+<for>]... [--ticks <n>]
nova-sprint clear --confirm sprint
nova-sprint teardown --confirm sprint
```

Every store verb takes `--redis <addr>` (else `NOVA_SPRINT_REDIS`, then
`NOVA_REDIS_ADDR`), `--actor <name>` (else `NOVA_SPRINT_ACTOR`; no default — a
verb that writes wants one), `--op <id>` (the same id again returns the recorded
result), `--json` and `--max <n>` (listed items; 0 is all). The coordinator's
verbs are the coordinator's alone (the first `init` names it: `--coordinator`,
else the actor); `take`, `finish`, `read`, `fleet beat` and `friend beat` are the
workers', whose actor is the member, reader or friend named; `merge` and `ci`
are reports; `tick`, `run` and `friend clean` are the machine's. Reads need no
actor except `inbox --read`, which moves the coordinator's cursor. A set is
ids, a stream, a column, `--max n` (`--limit` is an alias), or an inbox group:
`--group <id>`, the id `inbox` prints, with `--expect <n>` the size it printed,
which refuses a group that has changed. `nova-sprint help <verb>` (or
`<verb> -h`) prints one verb's usage, flags and exit codes; `nova-sprint help
<group>` (fleet, friend, reader, goal, stream) prints one group's.

### Role views: what a model reads instead of the dashboard

The owner, 2026-10-04: "i'd rather you hit this vs. hitting my dashboard which is for human
eyes". `nova-sprint view coordinator` is everything that needs the seat now, ranked by the
cards behind each item: open judgments, notes addressed to the coordinator, alarms (the
machine stopped, the fleet idle, nothing ready, a review or merge backlog, a stream stopped),
sentinels reached, and friends and machines that need a look, each with `next`, the exact
command that acts on it. `nova-sprint view worker --as <member|friend>` is one worker's cards
in order (brief, BASE, PATHS, deadline, attempt), its next step and its results not landed.
Both are reads, `--json` (schema 1), compact for the tokens a model pays: only what needs
action (`--all` adds every machine's and friend's row), a summary line first, and a `cursor`
that `--since <cursor>` takes to leave out what the last read showed unchanged:

```sh
nova-sprint view coordinator                 # the summary and up to 19 items, then cursor=
nova-sprint view coordinator --json --since <the cursor the last read printed>
nova-sprint view worker --as m1 --json
curl -s --compressed http://<tailnet address>:<port>/api/view/coordinator
curl -s --compressed 'http://<tailnet address>:<port>/api/view/worker?as=stella'
```

The sprint's server (`run --listen`) serves them read-only at `/api/view/coordinator` and
`/api/view/worker?as=<name>`. The contract is [SPEC-SPRINT.md](SPEC-SPRINT.md), section 11,
"Role views".

### A worker's own view: the dashboard's pull routes

`dashboard` serves the page on `--listen` (default `127.0.0.1:7390`) and, on listeners of
their own, the pull routes on `--pull` (default `127.0.0.1:7395`; `none` for either serves
nothing there): `curl -s http://<tailnet address>:7395/friend/<name>` is one friend's view
as plain text, a line an item (the sprint line, her friends-table row, a line per card
dealt to her: id, stream, state, how long, the time to its deadline, the branch; then the
open judgments on them); `/machine/<name>` is a fleet row's; `/team` is every friend and
the cards she holds; `/api/team`, `/api/friend/<name>`,
`/api/machine/<name>` and `/api/sprint` are JSON; `/events`, `/events/friend/<name>` and
`/events/machine/<name>` push each new copy as server-sent events. All of it is read-only,
no-store, carries the copy's time in `Sprint-At`, and comes from one copy of `where --json
--cards` read at most once a second however many pull. An unknown name is a 404 of one
line. The contract is [SPEC-SPRINT.md](SPEC-SPRINT.md), the dashboard.

### A provider out of funds

The owner, 2026-10-03: "provider out of funds should never be a mystery failure." `run` reads
each provider's balance every 10 minutes through the seat's key in its environment
(`OPENROUTER_API_KEY` for openrouter; opencode publishes no balance and reads `unknown`) and
prints a `BALANCE` line. A provider out of credit by a take it refused is rested until a
payment is seen (a balance read higher than the read before it, or than the balance at the
refusal) or `funded`: a balance over zero that is not higher never ends it. One out of credit by
a balance at zero is rested until a balance over zero; one low on funds, a balance not over an
hour of its spend, until the balance is over it. One judgment of the provider says which (`a
payment is the owner's`). `where --json` carries the `providers` table (balance, spend an hour,
state) and `routes` each route's `balance=`. When every provider is out of credit, the tick
stops the machine (`machine: STOPPED (every provider is out of credit)`) and `start` is refused
until one is paid; a provider low on funds never stops it. `funded <provider> --reason <text>`
says one was paid. The contract is [SPEC-SPRINT.md](SPEC-SPRINT.md), "A provider out of funds".

### Answering the routine judgments

`nova-sprint answer` answers the routine judgments (a reader found it
broken, work came back failed, blocked on something dropped, stalled, a conflict,
past its deadline, cannot ask, ready to accept, a card at its bound) by
nova-decide's judgment decision, card by card: it applies the verb chosen when
its probability is at or above `decide_judgment_bar` (nova-config's sprint row,
empty by default: with no bar it applies nothing, records every decision and lists
what a bar would apply; `--bar` gives one for a run; 0.8 is a starting point measured on 100 of the coordinator's own judgments,
not an independent calibration), by the line the inbox prints for that card,
and lists the rest for you: every drop, everything under the bar, and a provider
refusal for want of payment, which it never asks about. It prints one table, a
row a card, and records every decision (`--record`, default
`~/nova-sprint/decide/judgment.jsonl`, its directory made 0700) with its outcome once the card lands, is dropped
or comes back. Each verb it applies carries the decision's op id (`--op
decide.<decision id>`), recorded as `applying` before the verb runs and `applied`
or `refused` after, so a pass stopped between the two is finished by the next
through the same op and nothing is applied twice. One ask may take `--timeout`
(60s by default); an ask past it, or one that fails, is that card's `failed` row,
nothing is applied for it, and the pass exits 1. `--dry-run` applies and records
nothing; `--every 60s` runs it as the seat's loop until the machine is STOPPED. Jev's key comes from `JEV_API_KEY`:
`nova-secrets exec --only JEV_API_KEY -- nova-sprint answer`. The
contract is [SPEC-SPRINT.md section 8](SPEC-SPRINT.md#answered-by-nova-decide)
and [SPEC-NOVA-DECIDE.md section 13](SPEC-NOVA-DECIDE.md#13-the-judgment-decision).

`add` under `JEV_API_KEY` (`nova-secrets exec --only JEV_API_KEY -- nova-sprint add
...`) asks nova-decide's brief decision of every card it names with a brief after its
own checks and before it writes, one deadline for the batch: one `BRIEF card=<id>
op=<card>@brief-<hex> p_converges= minutes= failed= uncalibrated=true recorded=` line
per card, recorded in `~/nova-sprint/decide/brief.jsonl` (or `--decide-record <file>`),
the op stored on the card for land and drop to attach its end. The decision is
uncalibrated: nova-config's `sprint` row `decide_brief_bar` stays empty, which reports
only, until the brief record's own outcomes support a bar
([SPEC-NOVA-DECIDE.md](SPEC-NOVA-DECIDE.md) section 14).

### Exit codes

| exit | meaning |
|---|---|
| 0 | done |
| 1 | failed or incomplete (including refused; `start` while every provider is out of credit) |
| 2 | usage, or a store that did not answer (`fleet sync --check`: there is drift) |
| 3 | unreadable config (`fleet sync`, `friend sync`, `friend clean`), missing friend rows (`friend sync`, `friend clean`), or a replaced binary (`run`, `dashboard`) |

### What it does not prove

A landed card records a completed flow: the worker reports done, two different
readers pass that head, the tick (or the coordinator's `accept`) queues it for
merging, and the landing is reported. These are recorded judgments, not a
proof that the work is correct. A twin exercises that flow one command at a
time. It has no beats or ticks between commands, so it does not test fleet
timing, the `run` loop, `inbox --wait` or liveness. `finish` without `--head`,
then `merge`, records a landing without a push. The work table's cost column
is, per stream, the sum of its landed cards' total cost in US dollars — each
card's actual cost where one was priced, else its predicted one, `-` when
none was — so a total is a ledger of recorded spend, not a proof of it.


## nova-card

nova-card is pre-alpha: not ready for production use.

```
nova-card generate --from ledger --ledger <name> --repo-dir <dir> --out <dir> [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--base <branch>] [--repo <owner/name>] [--dry-run]
nova-card generate --from findings --file <tsv> --out <dir> (--repo-dir <dir> | --repo <owner/name> --base <branch> --sha <40hex>) [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--dry-run]
nova-card generate --from help --tool <name> [--tool <name>...] --out <dir> [--bin-dir <dir>] (--repo-dir <dir> | --repo --base --sha) [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--dry-run]
nova-card lint --card <file> [--card <file>...]
nova-card template
nova-card version
nova-card help [<verb>]
```

A card a model writes by hand takes it half an hour and comes back with guessed
PATHS; one wrong PATHS line was rejected 262 times in one night. nova-card
writes the cards from the source the work comes from, with the PATHS computed,
the lint already green, and the waves already laid out, so the one thing left
to do is `nova-sprint add --stream <s> --brief-dir <dir>`.

The flow is three lines:

```sh
nova-card generate --from ledger --ledger serial-tests --repo-dir ./repo --out ./cards
nova-sprint add --stream debt --brief-dir ./cards --allow-shared-paths
nova-sprint where
```

### First run

The included findings file is two files' worth of a reader's findings. No
checkout is needed when `--repo`, `--base` and `--sha` are given; with
`--repo-dir` the three are read off the checkout and every PATHS entry is
checked to exist in it:

```sh
nova-card generate --from findings --file ./cmd/nova-card/testdata/findings.tsv --repo example/repo --base dev --sha 0123456789abcdef0123456789abcdef01234567 --out ./cards
nova-card lint --card ./cards/finding-internal-bus-send.md
nova-card lint --card ./cards/finding-cmd-nova-bus-main.md
```

`./cards` then holds one `.md` per card, its name the card's id, and a
`manifest.tsv` (id, file, test, wave, deps). [TESTS.md](TESTS.md#nova-card)
carries the transcript; `cmd/nova-card/firstrun_test.go` runs it.

### Sources

`--from ledger --ledger <name>` reads one of internal/ci's ratchet ledgers from
the checkout: `serial-tests`, `slowwaits`, `sleeps-skips`, `fixed-waits`
(flash), `dead-code`, `namedpaths`, `transcripts`, `generality-fixtures` (pro).
One card per file the rows name; the card's PATHS are the file, its package's
test files and the ledger; its TEST is the class test that holds the ledger;
its task is the ledger's template with the rows substituted, and it says to
write the draft early and commit before any probe. `--from findings --file
<tsv>` reads `file:line`, finding, remedy, test columns (a header row is
skipped); one card per file, the first finding's test as TEST, a card with no
test named given the one it must write. `--from help --tool <name>` runs
`<name> help` and writes one card per tool: the lines over 100 characters,
the undefined terms and the examples that do not run as printed.

### Waves and dependencies

Cards of one ordinary ledger delete adjacent lines of one file and would
conflict at land, so odd cards are wave 1 and even cards wave 2, each wave 2
card depending on its wave 1 neighbours (`DEPENDS-ON`). A generated ledger
(SPEC-SPRINT.md section 7, the generality family) is regenerated by `land`, so
its cards are one wave with no dependency. Wave 1 cards of one ledger share its
path and neither needs the other, so the add wants `--allow-shared-paths`; the
`CARDS OK` line says `shared-paths=yes` when it does.

### What it refuses

Every brief is held to the lint `nova-sprint add` runs (the model lines, the
child rules under the default rule set, a tree card's steps), and past the add
to the typed header and the template's unfilled `<...>` lines, which the add
does not read, before anything is written (a sprint initialised with `--rules`
holds a brief to that file at the add); one red brief prints its
`LINT DRIFT card=<id> check=<check> line=<n>: <excerpt>` line and nothing is
written, exit 1. A PATHS entry that names nothing in `--repo-dir` is the same
refusal. An `--out` that already holds a brief is refused, exit 2. `--dry-run`
plans and lints, prints the manifest and the `CARDS OK` line with
`dry-run=yes`, and writes nothing.

### Exit codes

0 done; 1 a brief is red and nothing was written; 2 could not run.


## nova-work

nova-work is pre-alpha: not ready for production use.

Every issue of every repository of a GitHub organization in one tree file, with
each issue's full contents, and a check that the file holds exactly what GitHub
holds. The design is [SPEC-WORK-V1.md](SPEC-WORK-V1.md); this section is how to
use it. It reads GitHub only (no GitLab, no Gitea), never writes to GitHub (the
seam refuses any GraphQL document that is not a query), and captures the fields
SPEC-WORK-V1 section 1.3 lists, not reactions or other timeline events.

**Try it with no login.** `nova-work verify -h` prints the tree's grammar and a
minimal tree. Save that tree as `a.lisp`, copy it to `b.lisp` with
`:archived true`, and run `nova-work verify --tree a.lisp --against b.lisp`: one
`VERIFY DRIFT ... field=archived` line under `VERIFY FAIL`, exit 1. The same
file against itself is `VERIFY OK ... differences=0`, exit 0.

**First run against GitHub.** Needs `gh auth status` to pass and one repository
you can read: export `ORG` and `REPO`, then run the three lines of the banner's
`example:` block in a scratch directory (a dry run, the import to
`./tree.lisp`, the verify). The executed transcript of that sitting, with every
line of output, is in [TESTS.md](TESTS.md#nova-work).

**Output.** One result per run: `IMPORT OK`, `VERIFY OK`, `VERIFY FAIL` (exit 1,
the differences as `VERIFY MISSING`, `EXTRA` or `DRIFT` lines, values quoted),
or `<VERB> REFUSED: <why>; run: <next command>` (exit 2). `--json` prints the
same result as one JSON object.

### import

Reads every issue of `--org` (or of each `--repo`) through your `gh` login,
read-only, and writes one local tree file, `--out`, only after the encoded tree
has read back equal to what was fetched. `--dry-run` is not offline: it reads
GitHub exactly as the import does (every issue, the same calls), checks the
round trip, and writes nothing. A dry run costs what the import costs:
`IMPORT PLAN` names `est_calls`, and `--max-calls` (default 1500) refuses a plan
past it before any issue is read.

### verify

Reads the tree and GitHub again and writes nothing: zero differences is
`VERIFY OK ... differences=0`, the receipt that the tree holds what GitHub
holds. `--against <tree>` puts a second tree file where GitHub stands and reads
no network at all.

