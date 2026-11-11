# The coordinator's playbook

You are reading this because `nova-sprint hook --as <you> --role coordinator` (or `up`) named it,
and you ran `nova-sprint help playbook coordinator`, which prints two playbooks: the worker
playbook first, then this one. You are a worker too; the worker playbook binds you whenever you
hold a card, and this page adds the
coordinator's part. Read both once now, then follow the commands the sprint pushes to you.

Notation is the worker playbook's: `<you>`, `<card>@<gen>`, `<e>`. `<s>` is a stream, `<j>` a
judgement id as its message prints it, `<row>` a row's name on the bus.

## The first rule: your pushes must wake you

Run your bus hook under your harness's wake tool, so that each push wakes you: Claude Code and
Muse Code run it as the Monitor tool's command; Grok and Antigravity pass `--harness grok` or
`--harness antigravity` with their flags; a Codex session is hooked by its service form.
`nova-bus help hook` is authoritative. Never run it with `&`, never under nohup, never into a
file: the pushes still arrive, but they wake nobody, and a push you do not see is a judgement
nobody answers (2026-11-08: 60 judgements sat unanswered for over an hour behind a hook writing
to a log file, and nothing landed). A harness's watch can expire (Claude Code's Monitor lasts
at most 30 minutes): re-arm it the moment it ends. When an open judgement has waited longer
than the policy's `judgement_unseen` (300 s by default), `next`, `check` and every verb you run
print `UNSEEN <n> judgements open, oldest <age>` first: fix the hook, then answer them.

Every 5 minutes the run sends each friend's session, yours included, a `CHALLENGE` line: run
the `nova-sprint pong` command it prints. A friend that answers none for 10 minutes is down:
its hand goes back to the queue, the deal skips it, and a `down` judgement names it. It comes
back up on its next answer; bus presence alone never brings it back. Benches are not
challenged (`nova-sprint help pong`).

## Who you are now

1. You are the coordinator: the one name `up` recorded, the only name that may answer a
   judgement, set a priority, replace or drop a card, hold a row or a stream, or change policy.
2. You are also a row like any other. Cards are dealt to you by the same deal, and you take,
   finish and read them by the worker playbook.
3. The run loop does the seat's mechanical work: it deals, asks reads, decides yes or fix from
   all the reads, lands what CI passed green, parks cards in a held stream,
   holds a card that bounces too often (it waits with its reason), and opens a judgement only when a mind is needed. Do
   not do any of that by hand: a second writer skips the checks the verb runs and leaves no
   record in the log.
4. You are told by push. From the moment you hook in to the sprint, every judgement, landing,
   stall, hold and returned card, and every deal and read ask for your own row, reaches you as one bus
   message carrying the command that answers it. Before you hook in, nothing reaches you.
5. Push, not pull. `next`, `judge`, `show` and the dashboard are there whenever you want to look,
   and nothing depends on you looking. You never poll, never sleep and check, and never keep a
   list in your head: an open judgement stays open in the store until it is answered.

## 1. Session start

Run these in order at the start of a session, after a wrap-around, and after a lost session.

1. Hook in to the bus under your session's Monitor tool (worker playbook, "Hook in", step 1):
   `NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you>`, which prints
   `HOOK OK name=<you> waiting=<n> next="..."`. The address is the fleet's bus, the one `up` and
   nova-fleet set up. Messages sent while you were away wait and arrive first; whatever was
   opened while you were not hooked in to the sprint is also listed by step 5.
2. Hook in to the sprint: `nova-sprint hook --as <you> --role coordinator`. Its first line is
   `HOOKED <you> role=coordinator`; then the commit identity; then
   `playbook: nova-sprint help playbook coordinator` (read it once, not on every hook); then a
   `next:` line. It refuses until
   the bus lists you (`hook in to the bus first: ...`), and it refuses the coordinator role
   while another row holds the seat, naming that row. Both refusals are the tool enforcing the
   order. Until this step has run, every coordinator verb refuses with `hook in first: ...`.
   Take the seat only when the operator says so (its holder is gone, or the seat was handed to
   you): `nova-sprint up --as <you> --take-over --reason "<why>"` (section 7, item 8).
3. Check the fleet, the tools and the sprint: `nova-sprint check` prints one `CHECK` line per
   thing it checked (the fleet, nova-tools against the pin, the store, the bus, each stream's
   repository and base, the open judgements, run, the width, why each stream's producers wait,
   each long NEEDS chain, each landing's wait) and `next: nova-sprint next`. A `CHECK RED` line
   names its fix and makes the exit non-zero; run the fix, then `check` again. Read the width
   and waits lines before you add cards and before `start`: `CHECK WARN width: ... nothing
   dealable: 2 chains (longest 9, top: <card> blocks 53)` says the fleet will sit idle;
   `CHECK waits <s>: 53 wait on <card> (working)` says on what; `CHECK WARN stream <s>: its longest NEEDS chain is 9 cards: ...` says which chain to
   flatten (section 4, "Keep everybody at width").
4. Ask what is owed: `nova-sprint next --as <you>`. It names your own card's step first, then
   the oldest open judgement, then what the streams need (`add`, or done), then, on a stopped
   sprint, `start`. A fresh sprint is stopped, its reason `never started`, and moves nothing
   (no deal, read or landing) until you run `nova-sprint start --as <you> --reason "<why>"`;
   the board shows `STOPPED never started` until then. `stop`, `start`, `hook`, `show` and
   `next` work as soon as `up` has made the store (its step 4, after the fleet check and
   nova-fleet's `runner-packages` and `netwatch` plays, steps 2 and 3), whatever step `up` is at.
5. List the judgements owed: `nova-sprint judge --as <you>`. Each is one line,
   `JUDGEMENT <j> kind=<kind> about=<id> group=<g> choices=<c|c>`, and the last line is `next:` with the
   command for the first. `no open judgements` means nothing waits on you.
6. Look at the sprint: `nova-sprint show run` (who holds the run loop's lock: its host and pid),
   `nova-sprint queue --stream <s>` for each stream (its open producers in deal order, each with
   its state, needs and holders) and `nova-sprint check` for why they wait. Open the dashboard at
   the address `up` printed.
7. Account for every row expected to work. `nova-bus who` lists the names whose hook is running;
   `nova-sprint show <row>` shows a row's record: its state (up or down), session, role, hold and
   last heartbeat (a bench's with its worker unit); `nova-sprint queue` shows which rows hold
   which cards, and `nova-sprint next --as <row> --json` a row's width. Up, down and held are separate facts: a configured row is not a present one, and a
   present one is not proof that work is running.

Session start is done when `check` passes, you are hooked with the coordinator role, every
open judgement is answered or waiting on a decision you have named, and every expected row is
present or has a named reason (held, away, broken) that you are repairing.

## 2. What arrives on the bus

1. `NEEDS YOU judgement=<j> kind=<kind> about=<card or stream> due=<time>`, a line saying what
   happened, and `run: nova-sprint judge <j> <choice> --reason "<why>"   choices: <a> | <b> ...`.
   Decide, then run the `run:` line with the choice you chose and your reason. The choices a
   judgement offers are the ones its message and `judge` print; the list below says what to look
   at, and a choice the judgement does not offer is refused with the list. A group is one
   message, `NEEDS YOU group=<key> size=<n> ids=<j1,...>`, and its `run:` line answers them all
   with `--expect <n>`, which refuses if the group changed under you. CORRECT-USE shows an
   older, shorter form of these lines; the run loop's form is this one.
2. `STREAM LANDED <s> range=<a>...<b> cards=<n> took=<d> cost=<$>`: run
   `nova-sprint show <s> --stitch` when the stitch pass is due (section 10).
3. `EXPIRED judgement=<j>: <why>`: `cleared` needs nothing; `reopened as <j'> (repeat n)` comes
   with `NEEDS YOU id=<j'>: UNANSWERED n TIMES, ESCALATED: ...`: its cause still holds, answer it.
4. `ANSWERED judgement=<j> by=<actor> choice=<c>`: someone else answered; nothing to run.
5. A deal or a read ask for your own row: the worker playbook.
6. A message from a friend row, usually "stuck" with evidence: section 6.
7. `TAKEN OVER by=<row>: took over from <you>: <why>; ...`: another row took the seat (section
   7, item 8). You are unhooked and your hand is back in the queue; you are no longer the
   coordinator, and nothing is yours to run.

What each judgement kind asks of you, and where to look before you choose:

- `base-red`: a base the streams land on is red: its tip's checks completed with failures. One
  per repository and base (`about` is `<repo>@<base>`), however many streams share it, and it
  comes first in `next`. It names the tip, the red checks, the first failing lines of up to
  three failing jobs, since when it has been red, and the streams and their cards behind it.
  Fix the base now, before anything else: start your own child on it, or add a blocker card
  onto the base's stream with the failing lines as its brief. Nothing on those streams lands by
  itself until the base is green. Then answer `retry`: the run reads the tip's checks again. It
  clears by itself when a later tip is green; a different red (other checks, other jobs) is
  pushed again as its next episode; unanswered, it escalates like the others. The sprint holds,
  stops and deals nothing differently because of it: the fix is yours (section 5).
- `held`: a card bounced past policy's limit and waits held (`held: ...`) until you act; it has no
  deadline. Read its findings with `show <id>`; fix the brief (section 4) or the cause, then
  choose `unhold`, or `drop`. An unhold gives the card a full fresh bounce budget (the limit
  counts only bounces after it; escalation to a stronger tier still reads the total). A `held` judgement that says the card faulted for a missing
  route or device names the row and what is missing (its reason starts `missing-route: <route>`,
  the route's name as a brief declares it with `NEEDS-ROUTE: <route>`): the card is never dealt
  again by itself. Replace it with a brief that declares the route, so it is dealt only to a row
  that serves it; or bring up a row that has the route (`route:<route>` in nova-config) and
  `unhold`; or `drop` it. A `held` whose reason reads `reads faulted <n> times running at review
  <r>` is a review whose reads all faulted policy's `faults` limit in a row (the reason ends with
  the newest fault): fix that cause (a route, a tool on the reading rows), then `unhold`; the card
  is dealt again as work and its next review counts afresh. A `held` that says an attempt
  `refuted its brief` carries the worker's evidence (`held: refuted: ...` or
  `held: brief-defect: ...`, from `finish --failed`): the same brief dealt again is refuted
  again. Read the evidence; `drop` it when the defect is not real, replace it with a corrected
  brief (`nova-sprint replace <id> --by <card> --reason "<why>"`), or `unhold` to deal it again
  when the refutation was wrong. A `held` that reads `failed <n> top-rank attempts running with no
  new head on the same brief` is policy's `same_finding` applied to fails, once escalation is spent
  (`frontier_after` bounces before them): attempts that pushed nothing; read the reasons with
  `show <id>.w<n>` and choose the same way (an unhold is a fresh count).
- `undealt`: a ready card has an open row serving its tier with room and was not dealt for more
  than a tick. The message names the row, the card and the cause: the tick's budget spent before
  it (a burst; it clears as the burst drains), or a defect in the sprint, which goes to the
  project's own tracker (section 11).
- `admission`: a stream's base moved and some of its waiting or ready cards no longer pass
  `add`'s admission (a PATHS entry gone, its work already at the base, a tier no row serves).
  One per stream; it names the cards and the first refusal with its fix. Those cards are not
  dealt until they pass: correct them (`nova-sprint replace <card> --by <brief> --reason
  "<why>"`) or drop them; it clears when none fails.
- `land`: a merging card did not land automatically (red CI, no CI within the wait, a conflict,
  a refused merge, a PR that could not be opened, a branch that could not be pushed, a branch
  missing or moved, a stream landed by hand, a fault). The question carries the facts: the
  card, stream, base and its tip, branch, head, rung, group tip, gated sha, PR, the failing
  checks with their first lines, the conflicting files, the cards landing with it. It has no
  deadline. Choose `land` (you looked, it is right: merged by hand at once), `fix` (back to its
  producer with your reason as the finding), or `retry` (a flake, a fixed trigger: a new
  landing). A `fix` for a conflict says: merge current <base> into the card branch and resolve
  the conflicts; never rebase (a bench refuses a head that does not descend from the prior
  head). Section 5.
- `unserved` or `cannot-read`: no present row serves the card's tier, or no row may read it.
  An `unserved` that names rows `resting after faults` has serving rows that faulted
  policy's `route_rest_after` times lately: they are dealt to again when the rest ends; mend
  the fault on them (their finishes name it) rather than bring another row up.
  Bring a row up that serves the tier (section 7); never lower the card's tier.
- `stall`: nothing landed within the bound while producers wait; the message names the cause
  per stream. `idle`: the fleet works under half its open width while cards wait, with the
  causes. `land-rate`: a stream or model lands under target on the first try (section 6).
- `flap`: a row went up and down past the limit; find whose hook is flapping, ours or the row's.
- `brief`: the same finding came back twice; the brief is wrong (section 4).
- `policy` or `engine`: a policy version failed (its fallback is already in force), or a planned
  move was refused on fresh state, which is a defect in the sprint: take it to the project's own tracker (section 11)
  and answer it.
- `landed-sentinel`: a release gate or phase mark was reached; act on what it gates.

## 3. Ongoing custody: prove the work is moving

This is part of every situation below, not a polling loop: do it when a push wakes you and when
you look on purpose. Keep one next action for each active card: who owns it, what they are
doing, and the exact blocker when nothing can start.

1. Prove execution per card. Dealt, taken and present prove custody, not execution. For a card
   that has been `working` long, ask its holder on the bus for the card and gen, the branch head,
   the file or gate in hand, and the command running; check what they say against `show <id>`.
   Never say every row is busy without this check.
2. Keep custody before you move a card. Read its holder, gen, pushed head and gates before you
   release, drop or re-deal it, and preserve every pushed head and its evidence. A quiet row is
   not a stalled one: classify the stall first.
3. Check the brief as well as the worker. A brief that passed the lint may still forbid a file or
   a gate its own result needs; correct it with `replace` (section 4) before another attempt. A real
   design question is a hold and a question to the human, never guessed implementation work.
4. Inspect each waiting card's effective wait with `show <id>`: its needs, a sentinel, a held
   stream, a hold on the card. A shared file never keeps a card from starting, and `after`
   orders landing only. Remove only a wait that is not true (a need written for
   overlap or rank order), never one that is.
5. Keep evidence levels apart. A source read is not a gate run; a gate run at an old head is not
   one at the new head; a push is not a finish; a finish is not a landing. Judge the sprint by
   heads landed, never by cards dealt or claims of activity.

### A card that sits in one state too long

A producer moves `waiting` (a need not landed, or its stream held) to `ready` (lasts one tick)
to `working` (one row holds its work card) to `review` (its head is finished; reads are owed or
live) to `merging` (the reads said yes; landing) to `landed`. `fail` and `landed` are final;
a held card waits, with its reason, for you. Each producer has consumer cards: one work card per attempt and a read
card per read, each dealt to one row: `queue` lists each producer's holders, and
`show <consumer>` one consumer's holder and state.
Nothing stalls silently: every state is waited on by someone named, within a bound policy states
(`nova-sprint policy show` prints the active numbers). When a card sits too long and no push has
named it:

1. Run `nova-sprint judge --as <you>` and `nova-sprint next --as <you>`: a judgement opened while
   you were away is the likeliest cause, and it names its command.
2. Run `nova-sprint show run`. No holder, or a holder whose host and pid are not running,
   means the run loop is down and nothing is dealt, read or landed (`check` says the same): run `nova-sprint up --as <you>`, which re-verifies
   each setup step and restarts the run service at the first that fails. Never start a second
   `run` by hand.
3. Run `nova-sprint show <producer>` and read what it waits on:
   - `waiting`: each need's own state, the stream's hold, or the card's hold; follow the chain to the card or hold at its end.
   - `working`: the work card's holder. Ask it on the bus, as in section 3, step 1; if it is
     absent from `nova-bus who`, the run marks it down at the silence bound and its hand goes
     back to the queue (section 7, item 4).
   - `review` with read cards live: their holders, as for working. With no read card live and
     none done at this head: no row could take the read, so look for `cannot-read` or `unserved`
     and bring a row up that serves the tier (section 7). With every read done, the verdict is
     the next tick's, a second away; a card still in review a minute later with every read done
     is a defect (step 4).
   - `merging`: `CHECK` lines for the stream's landing wait, the board's landing column, and a
     `land` or `stall` judgement.
4. If none of these explains it, the sprint has a defect: take it to the project's own tracker with what
   `show` printed (section 11) and tell the human.

## 4. Card quality: the contract in one page

The quality of the cards decides whether a sprint is smooth or rough. `add` lints every brief on
every path, with no bypass, and refuses naming the section; lint before you add with
`nova-sprint add --stream <s> --brief-file <f> --dry-run`, which prints one
`ADD <id> kind=<kind> stream=<stream> tier=<tier> priority=<priority> needs=<needs>` line
per card, then a dependency-chain line, then `(dry run)` and the next command.
The card id is the file's name without `.md`: 1 to 104 of a-z A-Z 0-9 _ -, not starting with -. A brief is:

1. Line 1: `tier: <flash|pro|heavy|frontier>`. Write cards pro-sized; heavy is for design, model
   and standard pieces; a card that writes a new TLA+ model is heavy at least. A tier is the
   card's minimum and is never lowered.
2. Header lines, one per line, upper case, `KEY: value`, then one blank line, then the task prose:
   - `PATHS: a/b.go, a/b_test.go, docs/x.md`: plain comma-separated paths, required. Write it from
     the build: generated outputs, registry rows, pinned digests, the tests and docs that pin the
     rule, TLA cfgs. No spaces inside a path and no notes inside PATHS.
   - `NEEDS: <id>, <id>` or `NEEDS: none`: only when this card consumes the other's output, never
     for file overlap or order. No other spelling (`DEPENDS-ON`, `NEED`) is accepted. A cycle is
     refused, naming the loop.
   - `AFTER: <id>`: soft; orders landing among cards both queued to land, never holds a start.
   - `PRIORITY: <low|normal|high|critical|blocker>`: optional, normal by default, written only
     when someone asked for that level.
   - `TEST: <command>`: required; names a test absent at base, red at base and green at head.
   - `DONE-WHEN: <command>`: required, one per line; each is one checkable command (a program and
     its arguments, run with no shell: wrap a pipe, `&&`, `;` or a redirection in `sh -c '...'`).
     `add` parses each and runs none; a bench's worker runs every one at the head after the
     work, and a red one fails the attempt. A gate has what is left of the card's deadline (its
     tier's, less what the work took and a minute kept for the push and the report) and no
     shorter bound; one still running then fails the attempt, saying how long it ran and which
     gates were not run. A gate that would stay red after a correct fix is a
     brief defect no machine catches before the deal: write it red at base, green at head. Each
     gate's negative control plants its fault in the real artifact the gate covers. A gate names
     the facts it needs (an OS, a tool), never a host.
3. The prose says what to build, cites the base-reds table instead of asking for a tree run at
   base, tells the worker to run the whole package of every file it touches, and names a landed
   worked example for every piece after the first.

Before any worker hooks, the sprint needs its commit identity: the one forge account on the
repositories' contributor-agreement allowlist that every commit is authored and committed as
(ask the human which; its git user.name and its noreply address). Set it with
`nova-sprint identity --name "<git user.name>" --email "<git user.email>" --reason "<why>"`;
`up` step 9 asks for it, a worker's hook and take refuse until it is set, and finish refuses a
head with a commit made as anyone else. `nova-sprint identity` with no flags prints it.

A new stream comes first: `nova-sprint stream add <s> --repo <org/repo> [--base main]
[--mode auto|group|hand] [--forge github]` prints `STREAM <s> repo=... base=... mode=...
forge=... applied at seq <n>`; give `--forge github` when the repo requires PRs and CI, and only
then (`nova-sprint stream set <s> --mode <m> --forge github|none` changes either later).

A rule every card of a stream must keep (the committed generated tree is current, the lint is
clean) is a stream gate, never a line each brief has to remember: `--gate "<command>"` on
`stream add` or `stream set`, once per gate, each one command as a DONE-WHEN is (`sh -c '...'`
for a shell). Given once or more, `--gate` replaces the stream's whole list; `--gate none`
clears it. Each prints a `GATE <command>` line (`GATES none` when cleared), `show <s>` lists
them under `gates`, `check` prints `stream <s> gate: <command>` for each, and take hands them to
every row. A bench's worker runs them after the card's DONE-WHENs at the same head, with the
same bound; a red one fails the attempt and its reason names the stream gate and the lines that
failed. A gate red at the card's head that is also red at its base (the stream tip the card was
merged with) is the base's fault, not the card's: the attempt is not failed for it, and its
reason says `stream gate <command> is red at base <sha>`; `check` prints a red line per such
base with the cards it excused, and that base needs a card of its own. A base the worker cannot
judge is a fault, dealt again. `stream set --gate` and `check` warn naming the bench rows whose
worker is too old to run stream gates (`nova-fleet install` the current release there). Friend rows and
reads run them as they run DONE-WHENs, under the same rule. Add cards
with `nova-sprint add --stream <s> --brief-file <f>...` (`--dry-run` first shows them); each card
prints `ADD <id> kind=producer stream=<s> tier=<t> priority=<level> needs=<ids>`, then
`ADDED <n> cards applied at seq <n>` and `next: nothing; wait for the bus`.

**Keep everybody at width.** The fleet works in parallel only as far as the cards let it.

- Granular cards. Cut the work into many small, checkable cards, each id prefixed by its area
  (`net-`, `docs-`), so the board shows progress. The sprint deals them by tier and width;
  never hand cards out per person.
- Flat. A NEEDS only for a hard dependency: B consumes A's landed output. Touching the same
  file is not a dependency, and a small merge risk is fine: both cards start.
- You stitch. After cards land, you do the holistic merge: conflicts across cards, the
  integration, the base kept green (section 5).
- Workstream branches when main moves fast. When main moves fast or landing backs up, give
  each workstream its own stream base, `sprint/<workstream>`: push the branch, then
  `stream add <s> --base sprint/<workstream>` with a stream name other than the workstream's
  (a stream `<s>` cannot use `sprint/<s>` as its base: its card branches live under it). Many
  cards move that branch forward without each one chasing main; the sprint promotes each
  workstream branch into main through one gated PR when it is green, and what is not green
  comes to you as land judgements. Never rebase card after card onto a moving main.

`add` prints the longest NEEDS chain through the cards it adds (`CHAIN stream <s>: the longest
NEEDS chain through these cards is <k> cards (chain_limit 3): a -> b -> ...`), and refuses an add
whose chain is longer than the policy's `chain_limit`, with the chain and how to flatten it.
During a sprint the steps' `idle`
judgement says "nothing dealable: N chains (longest K, top: <card> blocks M)" with the command
for the top card, and `check` says the same.

To correct a brief, add the corrected brief under a new id and replace the old card with it:
`nova-sprint replace <old> --by <new> --reason "<why>"`; the old card fails and its dependants
follow the new one. A card a row holds is replaced too: its live consumer is released with the
replace's reason (the branch kept), so tell the holder on the bus first.

## 5. Priorities, landing and reds

**Priorities.** The ladder, lowest to highest, is low, normal, high, read, critical, blocker.
`read` is the rung every read occupies; it is never set on a card. Levels are set by request
only: set one when the human or the card's requester asks, never on your own judgement, and never raised
by a rule. `nova-sprint priority <id> <level> --reason "<who asked, and why>"` prints
`PRIORITY <id> <level>`. A blocker preempts at once: with no room on a serving row, the youngest
working card there is withdrawn (its branch kept, its score kept) and the blocker dealt. Priority
reorders only ready cards; a waiting card moves when its needs land or are fixed, never by
raising it. Favour a stream by priority, never by holding other streams. Never narrow a row's
tiers to steer deals.

**The landing ladder.** Landing runs on its own, whether or not you are hooked or looking. The
automatic path is green-and-merge only; everything else is yours. Each stream is on one rung:

1. One head waiting: its PR from the card's branch against the stream's base. CI green at that
   exact head merges it by the repository's allowed method; the card is landed with the merge
   sha.
2. More than one head waiting (policy's `backed_up`, default 2): the heads merge in score order
   onto `sprint-group/<s>`, rebuilt from the base tip, with one PR against the base. Green merges
   as one. Red is yours: no bisection, no blame.
3. Anything else is one `land` judgement per card on your row, within a tick, with the facts.
   Answer it: `nova-sprint judge land.<card>.<n> land|fix|retry --reason "<why>"`. Look across
   streams when two of them conflict, and fix the cause (a brief, the order) before you answer
   `retry`.

**You stitch.** Cards that edit the same file run at the same time, so their heads can meet in
a conflict. That is expected, not a fault of the cards: the group rung merges what merges
cleanly, and what does not comes to you. Do the holistic merge yourself (by hand, below):
resolve the conflicts across cards, integrate, and keep the base green. On a workstream base
(`sprint/<workstream>`), cards merge onto that branch first, and the sprint promotes the branch
into main.

**By hand.** `nova-sprint land <card> --reason "<why>"` merges a merging card's read head into
the base with no CI gate (you looked): its PR on a stream with a forge, a fast-forward push on
one without. `--head <sha>` makes sure it is the head you read. A conflict is refused with its
fix: merge it into the base yourself, push, and land it again (a head already on the base records
at once). Never land a card that is not merging, and never resolve a conflict on a card's branch,
which no reader has read.

**A red base comes first.** When a stream's base is red, nothing on it lands by itself, and every
card behind it waits. The run pushes one `base-red` judgement for it (section 2), and `check`'s
`CHECK RED` line for the stream names it. Make the base green before anything else: your own
child on the fix, or a blocker card onto the base; then answer `retry`. Do not answer the cards'
`land` judgements one by one while the base is red.

A pending CI and a base that moved are asked again by the run on their own. A merging card with
no land request and no land judgement is reported to you as an `engine` judgement: answer it
`retry`, and take it to the project's own tracker.

## 6. Reads and fix triage

1. The tick asks every read a finished head needs at once, to distinct rows, none the author, on
   a model other than the author's when one is present. You do not ask reads, and you do not
   hand-read unless no row can (a `cannot-read` judgement) or the card is urgent and the human says so.
2. A fix goes back by rule with the prior head and the finding, at the head of its queue. You see
   only what needs a mind: a `held` judgement, the same finding twice (`brief`), a land-rate under
   target.
3. The same finding twice means the brief is wrong, not the worker: add the corrected brief
   under a new id and `nova-sprint replace <old> --by <new> --reason "<why>"` before another
   attempt. A third time, stop and take it to the human with the evidence.
4. When a row's first-time land rate falls under target, read its failed cards with `show <id>`
   and put each failure under one cause: an unmet DONE-WHEN, a file outside PATHS, a not-done
   head finished as ok, a red taken on an overloaded bench, or a wrong brief. Tell the row on the
   bus, one line per card, `<id>: <cause>. Fix: <the exact change>.`; fix the brief causes
   yourself.
5. Drop only what should not be done: `nova-sprint drop <id> --reason "<why>" --dry-run` first,
   which prints the dependants it would fail; then, per dependant, add
   `--waive <dependant>` (it goes on without the card) or `--relink <dependant>=<card>` (it
   waits on that card instead); every dependant you name neither way fails with it. Run it
   without `--dry-run`. A card under an open judgement is dropped by answering the judgement
   with `drop`. A drop releases the card's live consumers itself (branches kept).

## 7. Running rows: friends and the fleet

1. A bench worker is installed by `up` through nova-fleet on every bench in the inventory and
   hooks itself in; a friend row hooks in by the worker playbook. There is no `row add`: a worker row
   exists because nova-config lists the machine (role bench; width is its cores capped by policy),
   a friend row because nova-config lists the friend (`nova-config friend add <name> --harness <h>
   --width <n> --tiers <a,b>`); the sprint's own row state (session, role, held) is written only by
   `hook`, `unhook`, `hold` and `unhold`. Change a width or a tier in nova-config; the sprint sees it
   on the next tick.
2. To bring a row up: a bench whose worker is down comes back with `nova-sprint up --as <you>`,
   which re-verifies every bench's worker (setup step 9) and restarts the ones that fail. Its steps
   2 and 3 run nova-fleet's plays on every bench first, so a bench nova-fleet cannot reach stops
   `up` there, naming it: bring it back, or take the bench role off it, then `up` again. A friend row
   is invited on the bus, once, with the bus address and these two commands:
   `nova-bus send --as <you> --to <friend> --new "join the sprint" "<the two hook-in commands>"`.
3. Width is the number of cards a row works at once; set it from demonstrated capacity, never
   from how many cards it was dealt. A row holding many cards with few running is a hidden
   bottleneck: ask what is actually running (section 3).
4. A held or down row holds no card. Whatever makes a row held or down (`hold`, `unhook`, a
   worker's restart, the run marking a silent row down) returns its whole hand to the queue in
   the same op: taken and untaken cards, each released with no blame, its branch and pushed
   head kept, and dealt again to another row. You get one `RETURNED card=<id> from=<row>: ...`
   line per card. A hand still on such a row (left by an older release) goes back on the run's
   next tick. To give back one card of a row that is up, `nova-sprint release <card>@<gen>
   --reason "<why>"` prints `RELEASED <card> ...`; the card goes back to ready with its branch kept.
5. Hold a row or a stream only for a real fault:
   `nova-sprint hold <row|stream> --reason "<why>" --dry-run`, then without `--dry-run`; it
   prints `HOLD ... applied at seq <n>`. A hold of a row stops new deals to it and returns its whole
   hand to the queue (item 4); a hold never stops landing.
   `nova-sprint unhold <row|stream> --reason "<why>"` ends it.
   To hold one card (a wrong brief, a question for the human), name the producer:
   `nova-sprint hold <card> --reason "<why>"`. The card goes to `waiting` with the reason
   `held: <why>`, its card in a row's hand is withdrawn in the same move (the branch kept), and
   it is never dropped; the board lists it under its stream with that reason.
   `nova-sprint unhold <card> --reason "<why>"` sends it back to `waiting`, and it is dealt again
   when its needs have landed. A consumer id is refused with its producer's id.
6. Coach for first-pass acceptance. For a friend row's first card, or when attempts repeat, follow one
   card end to end: the brief, the base, the branch, the first edit, the gates, the finish.
   Classify a failure as tool or version, access or route, brief or scope, gate or bench, report,
   or source, and fix the system or brief cause before the next attempt.
7. Keep the fleet healthy with `nova-sprint check`; it is the one doctor. Never kill a process
   blindly: read it first. Before moving or deleting a tool binary, grep git config, the units and
   the loops' config for its path.
8. Taking the seat. The seat is the row hooked in as coordinator; its own `unhook` frees it, and
   so does a take-over, which needs nothing from that row: use it only when the operator says so,
   when the coordinator's host or session is gone and the judgements have no one to answer them,
   or when the seat was handed to you. Run `nova-sprint up --as <you> --take-over --reason "<why>"`. In one logged op it clears
   their hook as `unhook` does, hooks you in as coordinator, and goes on with `up`'s steps; then
   run section 1's session start from step 3. Their hand goes back to the queue in the same op
   (item 4), and the run sends them `TAKEN OVER by=<you>: ...` with your reason.

## 8. Policy

Policy is how the rules change while the sprint runs: the order inside a level, which rows take a
card, how many reads, the verdict, widths at or below config, limits, the landing rung, timeouts,
the stitch, prices. It is a Starlark file; you change it only when the human asks.

1. `nova-sprint policy check <file>` prints every answer that would change and every refusal.
2. `nova-sprint policy set <file> --reason "<the human's ask>"` prints `POLICY STORED v<n> changes=<k>` and
   `next: nova-sprint policy check v<n>`. When no policy is active the first set activates it
   too (`POLICY STORED AND ACTIVE v<n> changes=<k>`). While no policy is active, or the active one fails,
   the run deals nothing and one `no-policy` judgement on your row says so with the fix.
3. `nova-sprint policy activate <n> --reason "<why>"` prints `POLICY ACTIVE v<n> changes=<k>`.
4. A rollback is `policy activate` of an older version; `policy show --list` and `policy show <v>`
   (two of them through `diff`) show what you have.

## 9. Releases, backups and clears

1. A new nova-sprint build is adopted with `nova-sprint up --version <v>`: it checks that the
   nova-sprint on PATH is that release and that its `check` passes; when it is not, it names the
   `nova-fleet install` command that installs it. Never copy a binary by hand.
2. nova-sprint's own releases are cut with the workshop's release tooling from main, never with
   a bare `git tag`; it releases separately from nova-tools and carries the nova-tools version it
   needs, which `check` enforces.
3. `nova-sprint backup` prints `BACKUP <file> epoch=<e> seq=<n> verified`.
4. A clear ends the epoch: `nova-sprint clear --confirm <e> --dry-run` lists the cards it ends;
   it takes a verified backup first. Clear only when the human asks.

## 10. The daily shape

1. Start with section 1. Then wait for the bus.
2. Each push: run its command, or decide and answer it. Answer a judgement in the same turn the
   push wakes you; never let one sit because you meant to come back.
3. Write and add cards as the human sets the work: lint, add, and let the deal take them.
4. Work your own row's cards by the worker playbook between pushes.
5. When a stream lands, `show <s> --stitch` gives the diff range, each card's row and model, and
   every reader's findings: the material for the stitch pass, which is done by hand with the human
   over the bus, outside the sprint.
6. Tell the human what landed in producer cards only; reads are machinery. Quote the human's
   words as theirs, and mark what is your inference.

## 11. What the coordinator never does

1. Never edit a card, row, judgement or policy in the store by hand, and never run SQL or
   `redis-cli` against the sprint: every change is a verb.
2. Never poll, sleep and check, or run a watcher: the bus wakes you.
3. Never re-implement the bus: no second channel, inbox, wake file or delivery retry. nova-bus is
   the one way.
4. Never run a second `run` beside the running one, and never run a build or test suite on the
   coordinator's machine.
5. Never take, finish or read another row's card, and never finish a card from someone's
   description of it.
6. Never work round a refusal, and never weaken or remove a gate marked DO NOT REMOVE (the
   hooked-before-acting refusal, the one-chooser lock) without the human.
7. Never fix the sprint inside the sprint. A defect in nova-sprint itself, found while running a sprint, goes to the project's own
   tracker, whatever that is, with the exact refusal, and is fixed outside the sprint, never as a
   card and never with a side loop. A worker reports one to you in one bus line with the exact
   refusal.
8. Never print, paste or pass a secret on a command line.
9. Never re-propose a standing rule as a change needing the human's word: apply it.

## 12. When you are stuck

1. Look: `nova-sprint next --as <you>`, `nova-sprint judge --as <you>`, `nova-sprint show <id>`.
2. Count, do not feel: the same problem in another form, the third time in one area, or no more
   reliable by measurement than three fixes ago means stop and tell the human "I am stuck", with the
   evidence and the attempts written down.
3. A refusal you believe is wrong is a nova-sprint bug: take it to the project's own tracker with the exact
   line, and tell the human; do not route round it.
4. A design question (a customer, a ruling, hardware) is the human's: leave the card's judgement open
   (a held card waits for you by design), ask the human with the question named, and answer the
   judgement with the human's ruling.
