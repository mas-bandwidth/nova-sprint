# Coordinator playbook

`nova-sprint hook --as <you> --role coordinator` prints the worker playbook followed by this page. The coordinator is also a row: cards dealt to it are taken, finished and read by the worker playbook. This page covers the coordinator-only verbs.

Notation is the worker playbook's (`<you>`, `<card>@<gen>`, `<e>`), plus `<s>` for a stream, `<j>` for a judgement id as its message prints it, and `<row>` for a row name.

## Role

- The coordinator is the one name `up` recorded. Only it may answer a judgement, set a priority, hold or unhold, drop, replace or relink a card, add cards, change policy, set the commit identity, stop or start the tick, back up or clear.
- The run loop does the mechanical work: it deals, asks reads, decides yes or fix from all reads, lands what CI passed, parks cards in a held stream, blocks a card that bounces too often, and opens a judgement only when a decision is needed. Do not do any of that by hand: a second writer skips the checks the verb runs.
- Push, not pull. Judgements, landings, stalls and your own row's deals reach you as bus messages carrying the command that answers them. `next`, `judge`, `show` and the dashboard are for looking on purpose; an open judgement stays open until answered.

## 1. Session start

Run in order at the start of a session and after a lost session.

1. **Bus.** Under the session's Monitor tool: `NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you> --exec 'nova-sprint wake --as <you>'` (worker playbook, "Hook in"). It prints `HOOK OK name=<you> waiting=<n> next="..."`.
2. **Sprint.** `nova-sprint hook --as <you> --role coordinator` prints `HOOKED <you> role=coordinator`, the commit identity, both playbooks and a `next:` line. It refuses until the bus lists you, and refuses the coordinator role to any name but the one `up` recorded. Until it has run, every coordinator verb refuses with `hook in first`.
3. **Doctor.** `nova-sprint check` prints one line per thing it checked and ends with `next: nova-sprint next`:
   - `CHECK` lines: nova-fleet, nova-tools against the pin, the store schema, the bus, each stream's repository and base at origin, the card branch names, the forge, the active policy, every waiting or ready card re-admitted as `add` would today, working cards on live rows, open judgements by kind, who runs the sprint, the width and why nothing deals, each landing's wait.
   - `CHECK WARN <what>: <fix>` is worth a look. `CHECK RED <what>: <fix>` makes the exit non-zero: run the fix, then `check` again.
   - Examples: `CHECK WARN width: ... nothing dealable: 2 chains (longest 9, top: <card> blocks 53), 8 overlaps` means the fleet will sit idle; `CHECK WARN stream <s>: its longest NEEDS chain is 9 cards: ...` names a chain to flatten (section 4).
   - `nova-sprint check --integrity` also rebuilds the history.
4. **What is owed.** `nova-sprint next --as <you>` names, in order: your own card's step, the oldest open judgement, what the streams need (`add`, or done), and on a stopped sprint, `start`.
5. **Start.** A fresh sprint is stopped (reason `never started`) and moves nothing until `nova-sprint start --as <you> --reason "<why>"`. The board shows `STOPPED never started` until then. `nova-sprint stop --reason "<why>"` stops the tick again: nothing is dealt, read or landed; verbs still work.
6. **Judgements.** `nova-sprint judge --as <you>` lists one line each, `JUDGEMENT <j> kind=<kind> about=<about> group=<group> choices=<a>|<b>`, then a `next:` command. `JUDGEMENTS none` means nothing waits on you.
7. **State.** `nova-sprint show run` (the chooser, its host, its last tick), `nova-sprint show <s>` per stream, `nova-sprint show <row>` per row (up or down, hold, width, cards held), `nova-bus who` (names whose hook runs). Up, down and held are separate facts; a configured row is not a present one.

Done when `check` passes, you are hooked as coordinator, every open judgement is answered or waits on a decision you have named, and every expected row is present or has a named reason.

## 2. Messages and judgements

- `NEEDS YOU id=<j>: <question>; run: nova-sprint judge <j> <choice> --reason <why>   choices: <a> | <b> ...`: decide, then run the `run:` line with a choice it offers. A choice not offered is refused with the list. Judgements of one kind share a group; several at once are answered with `nova-sprint judge <group> <choice> --expect <n> --reason "<why>"`, which refuses if the group's size changed.
- `LANDED card=<id> stream=<s> head=<40 hex>; run: nova-sprint show <id>`: nothing to run.
- `STREAM LANDED <s> <n> landed; run: nova-sprint show <s> --stitch` (or `STREAM DONE WITH FAILURES <s> <n> landed <k> failed ids=...`): every producer of the stream is final. `show <s> --stitch` prints the diff range, each card's row and model, and every reader's findings.
- `EXPIRED judgement=<j>: <what the default did>`: a judgement passed the policy `judgement` timeout (default 3600 s). The default never drops, accepts, merges or lands.
- `ANSWERED judgement=<j> by=<actor> choice=<c>`: another actor answered.
- Deals, nudges and withdrawals for your row: the worker playbook.
- A kin row's "stuck" message with evidence: section 6.

Judgement kinds, their choices, and where to look:

| Kind | Opens when | Choices | Look at |
|---|---|---|---|
| `blocked` | a card bounced to the policy `bounces` limit (default 6), a card was held (`held: <why>`), or it faulted on a missing route (`missing-route: <route>`). It has no deadline | `unblock`, `drop` | `show <id>`: findings. Fix the brief or cause first. For a missing route: replace it with a brief that declares `NEEDS-ROUTE`, or give a bench the role `route:<route>` in nova-config, then `unblock`; or `drop` |
| `unacked` | a card sat in a row's hand past `ready_take` (default 180 s) unacknowledged and no other row has room | `retry` | reach the row on the bus; if its session is gone, `nova-sprint release <card>@<gen> --as <you> --reason "<why>"` |
| `undealt` | a ready card has an open row serving its tier with room and was not dealt | `retry` | the message names the row, card and cause |
| `land` | a merging card did not land automatically (red CI, no CI within the wait, conflict, refused merge, PR or branch failure, inherited red) | `land`, `fix`, `retry` | section 5. No deadline |
| `unserved` | no present row serves the card's tier or route | `retry` | bring up a row that serves it (section 7); never lower the card's tier |
| `cannot-read` | no row may read the card (author excluded) | `retry`, `drop` | bring up another row; hand-read only if the human says so |
| `no-model` | a tier has serving rows but none can be named a model | `retry` | the policy `catalog()` and the rows' models |
| `stall` | nothing landed within `landing_stall` (default 60 s) while cards wait in review or merging | `retry` | `show <s>`, its rung and stuck column |
| `idle` | working is under `idle_floor` (default half) of open width while cards wait | `retry` | the message lists the causes; usually a long NEEDS chain or path overlaps |
| `overlap` | a card is held behind a started card that names the same file | `retry` | `show <id>`; fix PATHS or order |
| `land-rate` | a stream's first-time land rate is under `land_target` (default 0.9) over the sample | `retry` | section 6, step 4 |
| `flap` | a row went down and up `flap_limit` times (default 3) | `retry` | whose hook flaps, ours or the row's |
| `brief` | the same finding came back `same_finding` times (default 2) | `rework`, `drop` | the brief is wrong: replace it (section 4) |
| `roster` | a row is not in nova-config | `retry` | add it to nova-config |
| `no-policy` | no policy version is active, or the active one fails; the run deals nothing | `retry` | `nova-sprint policy set` / `policy activate` (section 8) |
| `policy` | a policy question failed; the default answered | `retry` | `nova-sprint policy check` |
| `engine` | a planned move was refused on fresh state: a defect in nova-sprint | `retry` | section 11 |
| `landed-sentinel` | a sentinel card landed (a gate or phase mark) | `retry` | act on what it gates |
| `cost-gap` | `cost reconcile` found a provider balance fall that differs from the store's priced records by more than 5% and $1 | `ack`, `retry` | `nova-sprint cost reconcile --as <you>` |

A silent row is handled without a judgement: after `row_silent_gone` (default 600 s) with no heartbeat or bus presence it is marked down, and after `row_silent_withdraw` (default 1800 s) its working cards are withdrawn with their branches kept and dealt again.

## 3. A card that sits too long

A producer moves `waiting` (a need not landed, or its stream held) → `ready` → `working` (a row holds its work card) → `review` (head finished; reads owed or live) → `merging` (reads said yes; landing) → `landed`. `fail` and `landed` are final; `blocked` waits for the coordinator. Each producer has consumer cards (one work card per attempt, one read card per read) shown by `show <producer>` with holder and state. Every state is waited on by someone named, within the bounds `nova-sprint policy show` prints.

When a card sits too long and no message named it:

1. `nova-sprint judge --as <you>` and `nova-sprint next --as <you>`: a judgement opened while you were away is the likeliest cause.
2. `nova-sprint show run`. A last tick more than a few seconds old means the run loop is down: `nova-sprint up --as <you>` re-verifies each setup step and restarts the run service at the first that fails. Never start a second `run` by hand.
3. `nova-sprint show <producer>`:
   - `waiting`: each need's state, the stream's hold, or a path overlap with a started card; follow the chain to its end. `AFTER` orders landing only and never keeps a card from starting.
   - `working`: the holder. Ask on the bus for the card and gen, the branch head, the file or gate in hand and the command running; compare with `show`. Dealt, taken and present prove custody, not execution.
   - `review` with reads live: their holders. With none live and none done at this head, no row could take the read: look for `cannot-read` or `unserved`.
   - `merging`: the stream's rung and stuck column in `show <s>`, and a `land` or `stall` judgement.
4. If none explains it, the sprint has a defect (section 11).

Before moving a card, read its holder, gen, pushed head and gates, and preserve every pushed head. Keep evidence levels apart: a source read is not a gate run; a gate at an old head is not one at the new head; a push is not a finish; a finish is not a landing.

## 4. Cards

`add` lints every brief with no bypass and refuses naming the section. `docs/CARD-CONTRACT.md` is the full contract. In short, a brief is a file whose name without `.md` is the card id:

    tier: pro
    PATHS: store/x.go, store/x_test.go
    NEEDS: none
    TEST: go test -run TestX ./store
    DONE-WHEN: go test ./store
    DONE-WHEN: go vet ./store

    <the task prose>

Check before adding: `nova-sprint add --stream <s> --brief-file <f>... --dry-run` prints an `ADD` line per card and `ADD DRY RUN cards=<n>`. Then:

    nova-sprint add --stream <s> --brief-file <f>... [--needs <a,b>] [--after <a,b>] [--priority <level>]

Output per card: `ADD <id> kind=producer stream=<s> tier=<t> priority=<level> needs=<ids>`; then `CHAIN stream <s>: its longest NEEDS chain is <k> cards: a -> b -> ...` (and `WARN CHAIN` with the time it takes at the stream's land rate, from 4 cards); then `ADDED <n> cards applied at seq <n>` and `next: nothing; wait for the bus`. A sentinel, a card that lands when its needs land, is `nova-sprint add --stream <s> --sentinel <id> --needs <a,b>`.

A new stream comes first:

    nova-sprint stream add <s> --repo <org/repo> [--base main] [--mode auto|group|hand] [--forge github]

It prints `STREAM <s> repo=... base=... mode=... forge=... applied at seq <n>`. Use `--forge github` when the repository requires PRs and CI; without a forge a stream has no PR or CI and cards land by hand. `nova-sprint stream set <s> --mode <m> --forge github|none` changes either later.

The sprint needs its commit identity before any worker hooks: the one forge account on the repositories' contributor-agreement allowlist, which every commit is authored and committed as. Ask the human which, then `nova-sprint identity --name "<git user.name>" --email "<git user.email>" --reason "<why>"`. With no flags it prints the identity. Worker `hook` and `take` refuse until it is set, and `finish` refuses a head with a commit made as anyone else.

Writing cards that keep the fleet at width: tier is the card's minimum and is never lowered (pro-sized by default; heavy for design, model and standard pieces; a card that writes a new TLA+ model is at least heavy). Keep NEEDS chains flat (a NEEDS only on a consumed output), PATHS concrete, and one card per file set. Cite the base-reds table in the prose and name a landed worked example for every piece after the first.

To correct a brief, add the corrected brief under a new id and replace the old card:

    nova-sprint replace <old> --by <new> --reason "<why>" [--waive <dependant>]... [--relink <dependant>=<card>]... [--dry-run]

The old card fails and its dependants follow the new one. A card a row holds is refused (`producer <id> has live consumers`): send the correction to the holder on the bus, or `release` the card first.

## 5. Priorities, landing and reds

**Priorities.** Lowest to highest: `low`, `normal`, `high`, `read`, `critical`, `blocker`. `read` is the rung every read occupies and is never set on a card. Set a level only when the human or the card's requester asks:

    nova-sprint priority <card> low|normal|high|critical|blocker --reason "<who asked, and why>"

It prints `PRIORITY <id> <level>`. A blocker preempts at once: with no room on a serving row, the youngest working card there is withdrawn (branch kept) and the blocker dealt. Priority reorders ready cards only. Favour a stream by priority, never by holding other streams; never narrow a row's tiers to steer deals.

**Landing ladder.** Landing runs on its own. The automatic path is green-and-merge only; everything else is a judgement. Each stream is on one rung:

1. One head waiting: a PR from the card's branch against the stream's base. CI green at that exact head merges it by the repository's allowed method; the card is landed with the merge sha.
2. More than `flowing` and at least `backed_up` heads waiting (policy `landing()`, defaults 1 and 2): the heads merge in score order onto `sprint-group/<s>`, rebuilt from the base tip, with one PR against the base. Green merges as one. Red is the coordinator's: there is no bisection.
3. Anything else: one `land` judgement per card, with the facts (card, stream, base and tip, branch, head, group tip, PR, failing checks, conflicting files, cards landing together).

Answer: `nova-sprint judge land.<card>.<n> land|fix|retry --reason "<why>"`:
- `land`: you looked; merge it by hand now.
- `fix`: back to its producer with your reason as the finding.
- `retry`: a new landing (a flake, a fixed trigger). Fix the cause (a brief, a need, the order) first when two streams conflict.

**By hand.** `nova-sprint land <card> --reason "<why>" [--head <sha>]` merges a merging card's read-ok head into the base with no CI gate: its PR on a stream with a forge, a fast-forward push on one without. `--head` makes sure it is the head you read. A conflict is refused with its fix: merge it into the base yourself, push, and land again (a head already on the base records at once). Never land a card that is not merging; never resolve a conflict on a card's branch, which no reader has read.

A pending CI and a base that moved are asked again by the run on their own. A merging card with no land request and no land judgement surfaces as an `engine` judgement.

## 6. Reads and fixes

1. The tick asks every read a finished head needs at once, to distinct rows, never the author, on a model other than the author's when one is present. Do not ask reads or hand-read unless no row can (`cannot-read`) or the human says so.
2. A fix returns by rule to the head of its queue with the prior head and the finding. The coordinator sees only what needs a decision: a `blocked` card, a `brief` judgement, a `land-rate` judgement.
3. The same finding twice means the brief is wrong, not the worker: replace the brief before another attempt. A third time, take it to the human with the evidence.
4. When a row's first-time land rate is under target, read its failed cards with `show <id>` and put each failure under one cause: an unmet DONE-WHEN, a file outside PATHS, a not-done head finished as ok, a red on an overloaded bench, or a wrong brief. Tell the row on the bus, one line per card: `<id>: <cause>. Fix: <the exact change>.` Fix brief causes yourself.
5. Drop only what should not be done:

        nova-sprint drop <card>... --reason "<why>" --dry-run

   The dry run prints the dependants it would fail. Then add `--waive <dependant>` (it goes on without the card), `--relink <dependant>=<card>` (it waits on a replacement), or list the dependants to fail, and run it without `--dry-run`. A card under an open judgement is dropped by answering that judgement `drop`. A card with a live consumer is refused until its holder releases it. `nova-sprint relink <card> --need <failed need> --to <card> --reason "<why>"` repoints one dependant's failed need.

## 7. Rows

1. A bench worker exists because nova-config lists the machine (role bench; its width is its cores capped by policy `fleet_width_cap`). A kin row exists because nova-config lists the kin (`nova-config friend add <name> --harness <h> --width <n> --tiers <a,b>`). A route is a role: `nova-config machine set <bench> --roles bench,route:<name> --actor <you> --reason <why>`. The sprint's own row state (session, role, held) is written only by `hook`, `unhook`, `hold` and `unhold`. Width and tier changes in nova-config take effect on the next tick.
2. A bench whose worker is down comes back with `nova-sprint up --as <you>`, which re-verifies every bench worker (setup step 7) and restarts the ones that fail. Invite a kin row once, on the bus, with the bus address and the two hook-in commands: `nova-bus send --as <you> --to <kin> --new "join the sprint" "<the two commands>"`.
3. Set width from demonstrated capacity, never from how many cards a row was dealt. A row holding many cards with few running is a hidden bottleneck: ask what is running.
4. A row that has left for good: `nova-sprint release --row <row> --all --reason "<why>" [--dry-run]` prints `RELEASED row=<row> consumers=<n>`; every card returns to ready with its branch kept. Reach the row on the bus first.
5. Hold a row or stream only for a real fault:

        nova-sprint hold <row|stream|card>... --reason "<why>" --dry-run
        nova-sprint hold <row|stream|card>... --reason "<why>"

   A row hold stops new deals to it and takes back only cards it has not acknowledged; acknowledged work stays, and a hold never stops landing. A stream hold parks its ready producers. A card hold (name the producer) moves it to `blocked` with the reason `held: <why>`, withdraws its live consumers (branches kept), and never drops it. A consumer id is refused with its producer's id. `nova-sprint unhold <row|stream|card>... --reason "<why>"` ends a hold; a held card returns to `waiting` and is dealt when its needs have landed.
6. Coach first-pass acceptance: for a kin row's first card, or when attempts repeat, follow one card end to end (brief, base, branch, first edit, gates, finish). Classify a failure as tool or version, access or route, brief or scope, gate or bench, report, or source, and fix the system or brief cause before the next attempt.
7. `nova-sprint check` is the one doctor. Read a process before killing it. Before moving or deleting a tool binary, grep git config, units and loop config for its path.

## 8. Policy

Policy is a Starlark file (`policy/default.star` is the built-in default) that answers how the run behaves: the order inside a level, which rows take a card, how many reads, the verdict, widths, limits, the landing rung, timeouts, the stitch, prices and the model catalog. Change it only when the human asks.

    nova-sprint policy check <file|version>                 every answer that would change, and every refusal
    nova-sprint policy set <file> --reason "<the ask>"      POLICY STORED v<n>, next: nova-sprint policy check v<n>
    nova-sprint policy activate <n> --reason "<why>"        POLICY ACTIVE v<n> answers-changed=<k>
    nova-sprint policy show [<n>] [--list]
    nova-sprint policy diff <n|default> [<n|default>]

When no policy is active the first `set` also activates it (`POLICY STORED AND ACTIVE v<n>`). While none is active, or the active one fails, the run deals nothing and a `no-policy` judgement says so. A rollback is `policy activate` of an older version.

## 9. Upgrades, backups and clears

1. `nova-sprint up --as <you> --version <v>` adopts a new build: it installs it beside the old, runs one shadow tick against the live store, swaps, watches three ticks, and swaps back on an overrun, crash or refused tick, saying why. Never copy a binary by hand.
2. nova-sprint releases separately from nova-tools and carries the minimum nova-tools version it needs; `check` enforces it.
3. `nova-sprint backup [--out <dir>]` prints `BACKUP <file> epoch=<e> seq=<n> verified`. It needs `pg_dump` no older than the server. `nova-sprint restore <file> --as <you>` loads a verified dump into an empty store and hooks you in as coordinator.
4. `nova-sprint clear --confirm <e> --dry-run` lists the cards a clear would end; `clear --confirm <e>` takes a verified backup first, then ends the epoch and every card goes. A wrong epoch is refused with `--confirm <the current one>`; a live consumer is refused until released. Clear only when the human asks.
5. `nova-sprint cost reconcile --as <you>` (under `nova-secrets exec --only DEEPSEEK_API_KEY` for the provider key; the run loop does it hourly) compares provider balances with the store's priced records.

## 10. Daily shape

1. Start with section 1, then wait for the bus.
2. On each push, run its command or answer it in the same turn.
3. Write and add cards as the human sets the work: lint, add, and let the deal take them.
4. Work your own row's cards by the worker playbook between pushes.
5. When a stream lands, `show <s> --stitch` gives the diff range, each card's row and model, and every reader's findings; the stitch pass is done by hand with the human, outside the sprint.
6. Report to the human what landed in producer cards only; reads are machinery. Quote the human's words as theirs and mark inferences.

## 11. Never

1. Never edit a card, row, judgement or policy in the store by hand; never run SQL or `redis-cli` against the sprint. Every change is a verb.
2. Never poll, sleep and check, or run a watcher: the bus wakes you.
3. Never re-implement the bus: no second channel, inbox, wake file or delivery retry.
4. Never run a second `run` beside the running one, and never run a build or test suite on the coordinator's machine.
5. Never take, finish or read another row's card; never finish a card from someone's description of it.
6. Never work round a refusal, and never weaken a gate that enforces hook-before-acting or the one-chooser lock.
7. A defect in nova-sprint found while running a sprint goes to the project's own tracker with the exact refusal and is fixed outside the sprint, never as a card or with a side loop. A worker reports one to you in one bus line with the exact refusal.
8. Never print, paste or pass a secret on a command line.

## 12. When you are stuck

1. Look: `nova-sprint next --as <you>`, `nova-sprint judge --as <you>`, `nova-sprint show <id>`.
2. Count, do not feel: the same problem in another form, the third time in one area, or no more reliable by measurement than three fixes ago means stop and tell the human you are stuck, with the evidence and the attempts written down.
3. A refusal you believe is wrong is a nova-sprint bug: take the exact line to the project's tracker and tell the human; do not route round it.
4. A design question (a customer, a ruling, hardware) is the human's: leave the card's judgement open (a `blocked` card waits by design), ask the human with the question named, and answer the judgement with their ruling.
