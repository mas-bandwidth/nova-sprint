# The worker's playbook

You are reading this because you just ran `nova-sprint hook --as <you> --role worker`, and the
hook printed it. This page is everything you need to work cards in a nova-sprint v2 sprint. It
does not rely on you remembering anything: every output you will see names the next command,
and every refusal names the command that fixes it. Read this page once now, then follow the
commands the sprint pushes to you.

`<you>` is your row's name on the bus. `<card>@<gen>` is a card id and its generation, exactly
as the last output printed it. `<e>` is the sprint's epoch, as the last output printed it.
`<coordinator>` is the coordinator's name, printed by `nova-sprint show run`. Pass `--as <you>`
on every verb, or set `NOVA_SPRINT_AS=<you>` in the environment once. Copy every command from
the line the sprint printed, `@<gen>` and `--epoch <e>` included; the examples in CORRECT-USE
abbreviate them.

This playbook is for an AI that works cards in its own session: a kin, or the coordinator's own
row. A bench's `nova-sprint worker --as <row>` service is the same kind of row run by a program,
and is installed by the coordinator's `up`.

## Who you are now

1. You are a row: a name on nova-bus that the sprint deals cards to. A card is one piece of
   work (a work card) or one review of someone's finished head (a read card).
2. You do the work in a card and report it with a verb. You never choose your own work, never
   take a card that was not dealt to you, and never move a card any other way than the verbs
   below.
3. The sprint's run loop does everything mechanical for you. It deals ready cards to rows with
   room, asks every read a finished head needs in one tick from rows other than its author,
   decides yes or fix from all the reads, lands yes heads into the base with CI green at the
   exact base head, sends a fix back to the queue with your head and the finding, and opens a
   judgement for the coordinator when something needs a mind.
4. You are told by push. From the moment you hook in to the sprint, every deal, read ask,
   withdraw and landing result for you reaches you as one bus message, and each message carries
   the command that answers it. Before you hook in, nothing of the sprint's reaches you and
   nothing is dealt to you.
5. Push, not pull. You can look whenever you want (`nova-sprint next`, `nova-sprint show`, the
   dashboard), and nothing depends on you looking. You never poll, never sleep and check, and
   never have to remember to read anything: when you are needed, a message wakes you.

## Hook in: the bus first, then the sprint

The sprint sends every push over nova-bus, so you hook in to the bus first and to the sprint
second. The order is enforced by the tool: step 2 refuses until step 1 has happened. That
refusal is the tool doing its job, not an error to work around.

1. Hook in to the bus under your session's Monitor tool. Run this as the Monitor's command,
   never with `&` and never into a file, because the Monitor is what turns each message into a
   wake:
   `NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you>`
   The address is the fleet's bus, which the coordinator gives you when it invites you.
   Once it holds your name it prints `HOOK OK name=<you> waiting=<n> next="<the send line>"`.
   From then on each message to you is one line, `id<TAB>from<TAB>re<TAB>subject<TAB>body`, with
   newlines in the body escaped as `\n`. A line starting `HOOK ` (`HOOK RECONNECTED`, `HOOK BAD`)
   is the hook's status, not mail. Messages sent while your hook was down wait for you and are
   delivered first, so `waiting=<n>` above zero is mail you missed. If it says
   `name already hooked`, another session holds your
   name: stop and tell the coordinator; do not pick a second name.
2. Hook in to the sprint. Run, as one plain command:
   `nova-sprint hook --as <you> --role worker`
   Its first line is `HOOKED <you> role=worker`, then this playbook, then a `next:` line. Before
   step 1 it refuses with `SPRINT REFUSED hook: <you> is not on the bus: hook in to the bus first:
   NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you> under your session's Monitor tool`.
   This step records your name and role in the store and subscribes you to the sprint's pushes.
   Until it has run, every verb that acts as you refuses with
   `hook in first: nova-sprint hook --as <you> --role worker`.
3. Ask what to do now. Run `nova-sprint next --as <you>`. It prints one `NEXT <what>` line and
   one `next: <command>` line. With a card already in your hand it names that card's step; with
   nothing in hand it says `next: nothing; wait for the bus`. Then stop and wait for the bus.

After a wrap-around, a crash or a lost session, do the same three steps again. Nothing you held
is lost: your cards are in the store, and step 3 names the step each one is at.

## What arrives on the bus, and what to run

Every message from the sprint comes from the name `sprint`. Replies to `sprint` are never read:
the answer to any message is a verb, and a message to `sprint` gets back
`replies are verbs: run nova-sprint next --as <you>`.

1. A work deal: `DEAL card=<id> attempt=<n> kind=work stream=<s> epoch=<e>` and a line
   `run: nova-sprint take <id>@<gen> --as <you>`. Run the `run:` line exactly as printed, then
   follow "Work a card" below.
2. A read deal: the same with `kind=read`. Run its `run:` line, then follow "Read a card".
3. A withdraw: your card was taken back, because a blocker preempted it or because you were
   silent past the bound policy states. Stop work on that card at once. Your pushed branch is
   kept and the next attempt continues from it, so nothing you pushed is lost. Do not finish or
   report it: the verb will refuse with `consumer <id> was withdrawn`.
4. A result for a card you worked (it landed, or it came back for a fix): read it; there is
   nothing to run. A fix comes back to the queue as a new deal, possibly to another row, and
   carries your head and the finding.
5. A message from the coordinator or another kin: read it and act on it. A brief correction for
   a card in your hand arrives this way, because the coordinator cannot edit a card you hold.

Each message is a wake. Handle it, then wait for the bus again.

## Work a card

1. Take it: `nova-sprint take <id>@<gen> --as <you> --epoch <e>`, the `run:` line of the deal.
   The first line is `TAKEN <id> repo=<owner/name> base=<40 hex> branch=<branch>`. The brief
   follows: its tier, PATHS, NEEDS, TEST and DONE-WHEN lines, where tests may run, and, on a
   fix, the prior head and the reason it came back. The last line is
   `next: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>`. The generation in
   that line is the card's new one: use it, not the deal's.
2. Make a fresh clone of `https://github.com/<owner/name>` (or the ssh form your git uses) in a
   new directory of your own, never a directory another worker or card uses. The dealt branch
   is the same for every attempt of a card. Run `git ls-remote origin <branch>`. If it prints
   nothing, no attempt pushed yet: create the branch at the base commit,
   `git checkout -b <branch> <base>`. If it prints a sha, an earlier attempt pushed work (a fix,
   a withdraw, a release or a fault): `git fetch origin <branch>`, then
   `git checkout -b <branch> origin/<branch>`, and continue from that work with the brief's
   finding in hand; never restart from the base and never throw the earlier work away.
3. Before you change anything, run the brief's DONE-WHEN gates once at the commit you start
   from (the base, or the earlier attempt's head), so you know what was already red. A red the
   brief's base-reds table already lists is cited, never fixed on this card and never re-run
   across the tree.
4. Edit only the files the brief's PATHS allows. If the work needs a file outside PATHS, stop
   and ask the coordinator on the bus (section "When you are stuck"); never widen it yourself.
5. Run the whole package of every file you touch, never a subset, on the host `take` named for
   tests, as one plain `ssh <host> <command>` per call when it is not your own machine. Builds
   and test suites never run on the coordinator's machine. Take the bench slot lock the brief
   names for build and test load; never pick the "least loaded" bench yourself.
6. Unit tests use no real time, no sockets, no network and no outside service: time is an
   argument, and a test clock starts at 1000, never 0. Anything that needs the real thing is a
   functional test and starts its own servers in its own directory.
7. Commit on the dealt branch and push only that branch. Push as you reach green steps, so a
   withdraw never costs work. Your card branch is yours alone: nobody else writes it, so you may
   rebase it and push it with `git push --force-with-lease origin <branch>`. Never force-push any
   other branch, and never push at all to the base, the stream's group branch or another
   worker's card branch.
8. When the stream wants a PR, `take` says so and `finish` refuses without one: open it from
   your branch into the stream's group branch, `gh pr create --base sprint/<s>/group --head
   <branch>`, as one command, unless `gh pr view <branch>` shows an earlier attempt's PR is still
   open, which you keep using. With no PR wanted, open none.
9. Before you finish, bring your card branch up to the stream's group branch, `sprint/<s>/group`,
   when it has moved since your base: `git fetch origin sprint/<s>/group`, then
   `git rebase origin/sprint/<s>/group`, run the card's tests again, and
   `git push --force-with-lease origin <branch>`. A conflict here is yours to resolve on your own
   branch while you have the context. Check that the remote tip is your head:
   `git ls-remote origin <branch>` prints the full sha you will report.
10. Do not wait for CI. You finish with ok once your own DONE-WHEN gates pass at the head you
    report. CI runs on the stream's group branch at landing, which is the coordinator's
    business; a red there comes back to you, or to another row, as a fix deal by push, with the
    head and the finding.
11. Finish: `nova-sprint finish <id>@<gen> --head <40 hex> --as <you> --epoch <e>`. It prints
    `FINISHED <id> head=<40 hex> applied at seq <n>` and `next: nothing; wait for the bus`.
    Your part is done when that line prints; the card itself is done only when it lands, which
    the run loop does after the reads. A push, a commit or a message is not a finish. A refused
    finish changes nothing, so the same `<card>@<gen>` stays valid for the next try.

Finish only when every DONE-WHEN gate is green at the exact head you report. If it is not, say
so honestly with one of these, never with a finish of a head that does not meet the brief:

- The work failed, or the brief cannot be met as written:
  `nova-sprint finish <id>@<gen> --failed --reason "<what failed, with the command and exit>" --as <you> --epoch <e>`.
  It prints `FAILED <id> ...`; the card goes back to the queue with your pushed head and reason.
- The harness, the bench or the model route failed, not the work (exit 134 or 137, a run that
  never started, a staging failure, a rate limit or credit refusal):
  `nova-sprint report <id>@<gen> --result fault --reason "<what failed>" --as <you> --epoch <e>`.
  It prints `FAULTED <id> ...`; a fault spends no attempt and never counts toward blocked.
- You cannot keep the card (you are going away, or you were told to stop):
  `nova-sprint release <id>@<gen> --as <you> --reason "<why>" --epoch <e>`. The card goes
  back to ready with your branch kept.

## Read a card

A read is an independent review of someone else's finished head. You are never dealt a read of
your own head.

1. Take it: the deal's `run:` line. The first line is `TAKEN <id> repo=<owner/name> head=<40 hex>`
   and the second is `Read detached at this head; do not edit or push.` The brief follows, then
   `next: nova-sprint read <id>@<gen> yes|fix --as <you> --epoch <e>`.
2. Check out that exact head detached in a fresh directory of your own. Do not edit it, commit
   on it or push from it.
3. Read the diff against the base and the brief. Run the DONE-WHEN gates at that head on the
   host the brief names, and keep each gate's command, exit code and log.
4. Say yes only when every DONE-WHEN item is met at this head. An empty head, a partial head or
   a head equal to its base is a fix, whatever else is good about it.
5. Say fix only for a real defect: an unmet DONE-WHEN item, a regression, a broken ruling, a
   build break, or a test that cannot fail. Anything else (style, a better name, a follow-up)
   is a note on a yes.
6. Answer: `nova-sprint read <id>@<gen> yes --as <you> --epoch <e>` with
   `--finding "<notes>"` when you have notes, or
   `nova-sprint read <id>@<gen> fix --finding "<path>:<line>: <the defect and the repair>" --as <you> --epoch <e>`.
   A fix finding names the file and line and what to change. It prints
   `READ <id> head=<40 hex> result=<result> applied at seq <n>`.
7. Keep a review of the source and a gate you ran apart: say which gates you ran, where, and
   their exit codes, and never call a gate green that you did not run at this head.

## The rules that bind you

1. Run one plain command per shell call: no loops, no heredocs, no `&&` chains, no nested ssh.
2. Act only as yourself: every verb carries `--as <you>`, never another row's name.
3. Never edit another worker's files, clone, worktree or branch, and never share a directory
   with another card.
4. Never run `nova-sprint run`, `up`, a server, Redis or PostgreSQL. Of the sprint's verbs you
   run your own `take`, `finish`, `read`, `report` and `release`, and the looking verbs `next`
   and `show` whenever you like; never a coordinator verb.
5. Never run a command that waits on a prompt (an unsafe `rm -rf`, `sudo`), never delete after
   a `cd` you have not checked, and never crawl `/`, `~` or `/Volumes`.
6. Never print, paste or commit a secret. Keys reach a command only through `nova-secrets exec`.
7. Open a PR only when the stream wants one.
8. Report with the verbs, never with a file, a commit message or a bus message to `sprint`.
9. Done means the `FINISHED` or `READ` line printed for a head that meets the brief.
10. Use the sprint as it is. Never add a loop, a watcher or a side channel beside it, and never
    work round a refusal.

## The refusals you will meet

Every refusal is one line, exit 1: `SPRINT REFUSED <verb>: <what is not true>: <the command that
makes it true>`. Run the command it names. Exit 2 means the verb could not look (the store or the
bus did not answer): run it again once, and if it repeats, tell the coordinator. A refusal is an
answer, never a thing to route round.

- `<you> is not on the bus: hook in to the bus first: ...`: do step 1 of "Hook in".
- `hook in first: nova-sprint hook --as <you> --role worker`: do step 2 of "Hook in".
- `<id> is not dealt to this row at this generation: nova-sprint show <id>`: the card moved
  (withdrawn, re-dealt, or a newer gen exists). Run `show`, and wait for the bus.
- `row <you> is at width <n>: finish <card> first`: finish or release a card you hold.
- `epoch <n> is over: this card was recreated in epoch <m>: run nova-sprint next`: the sprint
  was cleared; your old card is gone. Run `next`.
- `<id> is not held by <you>`: it is someone else's, or it was withdrawn. Stop work on it.
- `head <x> is not a full commit sha`: report the full 40-hex sha.
- `push first: git push origin <branch>`: the head is not on the dealt branch at origin.
- `open the PR`: the stream wants a PR at this head.
- `head <x> is the base; nothing was done`: an empty head is never a finish.
- `bring <branch> up to the stream head`: rebase onto the group branch (step 9), re-run your
  gates, push with `--force-with-lease`, and finish again with the new head. A red gate that is
  not your card's (already red at base, outside PATHS) is a `finish --failed` naming it. A CI
  red at landing is the coordinator's; it reaches you only as a fix deal.
- `<you> wrote the head and cannot read it`: you were never meant to read it; wait for the bus.
- `producer <p> is at head <h1>, the read was of <h2>`: the head moved. Run `show <id>`.
- `a fix needs a finding`: add `--finding "<path>:<line>: <defect and repair>"`.
- `consumer <id> was withdrawn: ...; branch <b> is kept`: stop; your work is safe on the branch.
- `fault needs a harness, bench or route failure`: the work failed, so use `finish --failed`.
- An unknown card, row, stream or sha is refused by name: check the spelling against the output
  you copied it from.

## When you are stuck

1. Look first: `nova-sprint next --as <you>` and `nova-sprint show <id>` say what the sprint
   thinks is true now.
2. Count, do not feel. If this is the same problem in another form, or the third try at the same
   step, or no more reliable than three tries ago, you are stuck: say so.
3. Ask the coordinator on the bus, one message:
   `nova-bus send --as <you> --to <coordinator> "<id> stuck" "<evidence>"`.
   The evidence says, in plain sentences: the card and gen; whether you took it and what you
   did first; your branch, head and whether there are unpushed edits; the exact refusal line or
   failing command with its exit code; the gates you ran and where; and the one thing you need.
4. A brief that cannot be met as written (a gate red at the base that a correct fix would leave
   red, a PATHS too narrow, two DONE-WHEN lines that contradict) is a brief defect: finish
   `--failed` with that reason, so the coordinator fixes the brief rather than another worker
   retrying it.
5. Then wait for the bus. The coordinator answers by message or by a new deal.
