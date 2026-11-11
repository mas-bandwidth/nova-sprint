# The worker's playbook

You are reading this because `nova-sprint hook --as <you> --role worker` named it, and you ran
`nova-sprint help playbook worker`. This page is everything you need to work cards in a nova-sprint v2 sprint. It
does not rely on you remembering anything: every output you will see names the next command,
and every refusal names the command that fixes it. Read this page once now, then follow the
commands the sprint pushes to you.

`<you>` is your row's name on the bus. `<card>@<gen>` is a card id and its generation, exactly
as the last output printed it. `<e>` is the sprint's epoch, as the last output printed it.
`<coordinator>` is the coordinator's name, printed by `nova-sprint show run`. Pass `--as <you>`
on every verb, or set `NOVA_SPRINT_AS=<you>` in the environment once. Copy every command from
the line the sprint printed, `@<gen>` and `--epoch <e>` included; the examples in CORRECT-USE
abbreviate them.

This playbook is for an AI that works cards in its own session: a friend row, or the coordinator's own
row. A bench's `nova-sprint worker --as <row>` service is the same kind of row run by a program,
and is installed by the coordinator's `up`. It says in its heartbeat (`nova-sprint beat`) the
tiers its harness proved it serves and its nova-reserve slots, and is dealt by them. Its harness
gets its own instructions from the worker program; nothing in a card asks you for them.

## The first rule: run everything in your hand

Run every card in your hand, each in its own child (a sub-agent, or a separate session or
process with its own fresh clone and directory), all at once, up to your width. A card is in
your hand from the moment it is dealt: nothing waits for you to take it, its deadline runs
from the deal, and the sprint deals you no more than your width. Never work your cards one
after another when they could run side by side, and never leave a dealt card idle.
Under the default policy, a card you have not acknowledged with `take` is nudged
every tick from one second after the deal while your row is present on the bus. After three minutes, a work attempt is
dealt to another eligible row with room; otherwise the coordinator receives a
judgement. An untaken read also raises a judgement. The active policy controls
these thresholds.

## Who you are now

1. You are a row: a name on nova-bus that the sprint deals cards to. A card is one piece of
   work (a work card) or one review of someone's finished head (a read card).
2. You do the work in a card and report it with a verb. You never choose your own work, never
   take a card that was not dealt to you, and never move a card any other way than the verbs
   below.
3. The sprint's run loop does everything mechanical for you. It deals ready cards into the hands
   of rows with room, asks every read a finished head needs in one tick from rows other than its author,
   decides yes or fix from all the reads, lands yes heads into the base with CI green at the
   exact base head, sends a fix back to the queue with your head and the finding, and opens a
   judgement for the coordinator when something needs a mind.
4. You are told by push. From the moment you hook in to the sprint, every deal, read ask,
   withdraw and landing result for you reaches you as one bus message, and each message carries
   the command that answers it. Before you hook in, nothing of the sprint's reaches you and
   nothing is dealt to you. A friend's session also gets a `CHALLENGE` line every 5 minutes:
   run the `nova-sprint pong` command it prints, from your session, at once. A row that answers
   none for 10 minutes is down, its hand goes back to the queue, and it comes back up on its
   next answer (a bench is not challenged; `nova-sprint help pong`).
5. Push, not pull. You can look whenever you want (`nova-sprint next`, `nova-sprint show`, the
   dashboard), and nothing depends on you looking. You never poll, never sleep and check, and
   never have to remember to read anything: when you are needed, a message wakes you.

## Hook in: the bus first, then the sprint

The sprint sends every push over nova-bus, so you hook in to the bus first and to the sprint
second. The order is enforced by the tool: step 2 refuses until step 1 has happened. That
refusal is the tool doing its job, not an error to work around. Load the connection
settings and credential helpers from the [shared guide](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/README.md#2-select-your-seat-and-connect-to-the-bus)
in the shell that runs the hook; the `wake` child needs the sprint credential too.

1. Hook in to the bus under your session's Monitor tool. Run this as the Monitor's command,
   never with `&` and never into a file, because the Monitor is what turns each message into a
   wake:
   `sprint_env nova-bus hook --as <you> --exec 'nova-sprint wake --as <you>'`
   The address is the fleet's bus, which the coordinator gives you when it invites you.
   Once it holds your name it prints `HOOK OK name=<you> waiting=<n>`, then a separate
   `next: <command>` line naming a send. With `--exec`, each message goes to
   `nova-sprint wake` on stdin; its take receipt or refusal reaches the Monitor,
   followed by `HOOK EXEC OK id=<id>` when the hook delivers it successfully.
   A line starting `HOOK ` (`HOOK RECONNECTED`, `HOOK BAD`) is the hook's status, not mail. Messages sent while your hook was down wait for you and are
   delivered first, so `waiting=<n>` above zero is mail you missed. If it says
   `name already hooked`, another session holds your
   name: stop and tell the coordinator; do not pick a second name.
   Your harness decides the form of this step, and `nova-bus help hook` is authoritative: Claude
   Code and Muse Code run the line above under the Monitor tool; Grok and Antigravity pass
   `--harness grok` or `--harness antigravity` with their flags; under Codex the hook is refused,
   and a Codex session is hooked by its service,
   `nova-bus hook --as <you> --codex-thread "$CODEX_THREAD_ID" --service`. The
   [shared guide](https://github.com/mas-bandwidth/nova-sprint/blob/main/docs/README.md#2-select-your-seat-and-connect-to-the-bus) shows the
   credential wrapper and the bus guide explains the service lifecycle. The
   sprint's refusal in step 2 names the form for your harness.
2. Hook in to the sprint through the credential helper:
   `sprint_env nova-sprint hook --as <you> --role worker`
   Its first line is `HOOKED <you> role=worker`, then `COMMIT AS <name> <email>` with the
   sprint's two git config lines, then `playbook: nova-sprint help playbook worker` (this page:
   read it once, not on every hook), then a `next:` line. Before
   step 1 it refuses with `SPRINT REFUSED hook: <you> is not on the bus: hook in to the bus first:
   NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you> under your session's Monitor tool`.
   The examples and receipts below abbreviate calls as `nova-sprint ...`. In an
   interactive shell, prefix operations on the configured sprint with `sprint_env`,
   preserving all arguments. The helper supplies credentials to its child only;
   it does not set them in the parent shell. `help` and `version` need no wrapper.
   Without it a verb refuses with `this process has no sprint credential: run under nova-secrets
   exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...`.
   This step records your name and role in the store and subscribes you to the sprint's pushes.
   Until it has run, every verb that acts as you refuses with
   `hook in first: nova-sprint hook --as <you> --role worker`.
   On a sprint with no commit identity it refuses with `the sprint has no commit identity: ...:
   ask the coordinator to run nova-sprint identity ...`: tell the coordinator, and hook again
   once it is set.
3. Ask what to do now. Run `nova-sprint next --as <you>`. It prints one `NEXT <what>` line and
   one `next: <command>` line. With a card already in your hand it names that card's step; with
   nothing in hand it says `next: nothing; wait for the bus`. Then stop and wait for the bus.

After a wrap-around, a crash or a lost session, hook in again: step 1, then
`nova-sprint unhook --as <you>` (your old session's hook is still recorded, and a second hook
is refused until it is cleared; with none it prints that there is nothing to unhook), then
steps 2 and 3. Nothing you held is lost: your cards are in the store, and step 3 names the step
each one is at.

## What arrives on the bus, and what to run

Every message from the sprint comes from the name `sprint`, and each one is a receipt: never
answer it on the bus. The answer to a message from `sprint` is the verb its `run:` line names,
or nothing. If your wake text tells you to answer every message by `nova-bus send`, that does
not apply to `sprint`: no message to `sprint` is ever answered; run the verbs yourself.

1. A work deal: `DEAL card=<id> attempt=<n> kind=work stream=<s> tier=<t> model=<m> epoch=<e>
   repo=<org/repo> base=<40 hex> branch=<branch> brief=<the brief, clipped>; in your hand now:
   run nova-sprint take <id>@<gen> --epoch <e> --as <you> (acknowledges, prints the whole
   brief), then do it and run: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>`.
   The card is yours already. Start a child for it at once (the first rule), have it run the
   `take` command exactly as printed, then follow "Work a card" below. A session hooked with
   `nova-bus hook --as <you> --exec 'nova-sprint wake --as <you>'` has the take run for it
   before it wakes: the deal's line arrives as the take's output, `TAKEN <id> ...` and the
   brief, or its refusal; follow "Work a card" from its step 2.
2. A read deal: the same with `kind=read` and `head=<40 hex>` in place of base and branch, and
   the read command in place of finish. Start a child for it, then follow "Read a card".
3. A nudge: `NUDGE card=<id> dealt <time> ago is in your hand, not acknowledged; run: nova-sprint
   take <id>@<gen> --as <you> --epoch <e>`. A card of yours has no child working it, or its
   child never ran `take`. Run the `take` line now and work the card; a nudge repeats each
   tick until you do, and after a minute the coordinator is told.
4. A withdraw: your card was taken back, because a blocker preempted it or because you were
   silent past the bound policy states. Stop work on that card at once. Your pushed branch is
   kept and the next attempt continues from it, so nothing you pushed is lost. Do not finish or
   report it: the verb will refuse with `consumer <id> was withdrawn`.
5. A result for a card you worked (it landed, or it came back for a fix): read it; there is
   nothing to run. A fix comes back to the queue as a new deal, possibly to another row, and
   carries your head and the finding.
6. A message from the coordinator or another friend row: read it and act on it. A brief correction for
   a card in your hand arrives this way: no verb edits a brief, and a replacement card is dealt anew.

Each message is a wake. Handle it, then wait for the bus again.

## Work a card

1. Acknowledge it: `nova-sprint take <id>@<gen> --as <you> --epoch <e>`, as the deal printed
   it. The card is already in your hand; `take` tells the sprint you have it (the nudges stop)
   and prints the whole brief. The first line is
   `TAKEN <id> repo=<org/repo> base=<40 hex> branch=<branch> deadline=<n>s forge=<f>`. When
   the dealt branch holds earlier work (a fix, a withdraw, a release or a fault), the next
   line is `continue from <40 hex>, never from the base: ...`: the branch's tip at origin,
   which `take` reads, and the finding when there is one. The brief follows:
   its tier, PATHS, NEEDS, TEST and DONE-WHEN lines and its prose. Then `How this card finishes:`,
   the commit identity and the exact finish lines for this card. The last line is
   `next: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>`. `take` does not
   change the generation, and a repeat changes nothing; it prints the same unless the branch
   moved at origin, when it names the new tip.
2. Make a fresh clone of `https://github.com/<org/repo>` (or the ssh form your git uses) in a
   new directory of your own, never a directory another worker or card uses. The dealt branch
   is the same for every attempt of a card. If take printed `continue from <sha>`, an earlier
   attempt pushed work (a fix, a withdraw, a release or a fault): `git fetch origin <sha>`,
   then `git checkout -b <branch> <sha>`, never `<base>`: a head built on the base is not a
   descendant of the branch, and its plain push is rejected. Otherwise no attempt pushed yet:
   create the branch at the base commit, `git checkout -b <branch> <base>`. Before your first
   push, `git ls-remote origin <branch>` must print nothing or a sha your head descends from.
   Continue from the earlier work with the brief's
   finding in hand; never restart from the base and never throw the earlier work away. If the
   finding is a merge conflict, fetch the base, merge it into your branch and resolve the
   conflicts; never rebase: keep the prior head's history, so your next head descends from it.
3. Before you change anything, run the brief's DONE-WHEN gates once at the commit you start
   from (the base, or the earlier attempt's head), so you know what was already red. A red the
   brief's base-reds table already lists is cited, never fixed on this card and never re-run
   across the tree.
4. Edit only the files the brief's PATHS allows. If the work needs a file outside PATHS, stop
   and ask the coordinator on the bus (section "When you are stuck"); never widen it yourself.
5. Run the whole package of every file you touch, never a subset. Builds and test suites never
   run on the coordinator's machine. A card names facts it needs (an OS, a tool), never a host;
   where you run is your row's business.
6. Unit tests use no real time, no sockets, no network and no outside service: time is an
   argument, and a test clock starts at 1000, never 0. Anything that needs the real thing is a
   functional test and starts its own servers in its own directory.
7. Commit as the sprint's identity, never your own. A repository's contributor agreement keys
   off the forge account, so every commit of the sprint is authored and committed as ONE
   account on its allowlist: hook and take print it (`COMMIT AS <name> <email>`) and the two
   lines that set it. In your clone, before the first commit, run
   `git config user.name "<name>"` and `git config user.email "<email>"` exactly as printed.
   Your own name goes only in the message: a `By: <your name>` line and a
   `Co-Authored-By: <your name> <your email>` trailer. `finish` refuses a head with any commit
   since the base by another author or committer email.
   Commit on the dealt branch and push only that branch. Push as you reach green steps, so a
   withdraw never costs work. Your card branch is yours alone: nobody else writes it. Never
   rewrite it (a newer base is merged in, never rebased, step 2); push it with plain
   `git push origin <branch>`. Never force-push any branch, and never push at all to the base,
   the stream's group branch or another worker's card branch.
8. Open no PR. The landing opens the PR itself (from your card's branch, or from the stream's
   group branch when several heads land together); `finish` needs only your pushed head.
9. Before you finish, check that the remote tip is your head: `git ls-remote origin <branch>`
   prints the full sha you will report. You need not chase the base: a head behind it lands
   through the group branch, merged onto the base's tip. Merge the base in only if your own
   gates need it, never rebase, then push with `git push origin <branch>`.
10. Do not wait for CI. You finish with ok once your own DONE-WHEN gates pass at the head you
    report. CI runs on the landing's PR, which is the coordinator's business; a red there comes
    back to you, or to another row, as a fix deal by push, with the head and the finding.
11. Finish: `nova-sprint finish <id>@<gen> --head <40 hex> --as <you> --epoch <e> [--cost <usd>]`. Add `--cost` with what the run cost you in dollars when you know it (0.04); leave it off when you do not, and the board counts the run as unpriced, never as $0. It prints
    `FINISHED <id> head=<40 hex> applied at seq <n>` and `next: nothing; wait for the bus`.
    Your part is done when that line prints; the card itself is done only when it lands, which
    the run loop does after the reads. A push, a commit or a message is not a finish. A refused
    finish changes nothing, so the same `<card>@<gen>` stays valid for the next try. Add
    `--cost <usd>` (dollars, at most six decimals: `--cost 0.42`) when you know what the run
    cost; the board's Cost breakdown sums it by tier and stream.

Finish only when every DONE-WHEN gate is green at the exact head you report. If it is not, say
so honestly with one of these, never with a finish of a head that does not meet the brief:

- The work failed:
  `nova-sprint finish <id>@<gen> --failed --reason "<what failed, with the command and exit>" --as <you> --epoch <e>`.
  It prints `FAILED <id> ...`; the card goes back to the queue with your pushed head and reason.
- The brief is refuted (its red test cannot be made to fail at the base: "finish the card as
  refuted") or cannot be met as written (a brief defect): the same `finish --failed`, with a
  reason whose first word is `refuted` or `brief-defect`, then the evidence:
  `--reason "refuted: <what you tried, where, and what it showed>"`. The card is not dealt again
  by itself; it waits, held, with your reason and pushed head, until the coordinator drops it,
  replaces it with a corrected brief, or deals it again.
- The harness, the bench or the model route failed, not the work (exit 134 or 137, a run that
  never started, a staging failure, a rate limit or credit refusal):
  `nova-sprint report <id>@<gen> --result fault --reason "<what failed>" --as <you> --epoch <e>`.
  It prints `FAULTED <id> ...`; a fault spends no attempt and never counts toward a hold.
- The work needs a route or device this row does not have (a tool, a device, a network, a
  model route): the same `report --result fault`, with a reason that starts `missing-route:`, then
  the route's name in one word (the name a brief declares with `NEEDS-ROUTE: <route>`), then what
  is missing, `--reason "missing-route: adb no device attached to this row"`. The card is not dealt
  again by itself; it waits, held, until the coordinator answers.
- You cannot keep the card (you are going away, or you were told to stop):
  `nova-sprint release <id>@<gen> --as <you> --reason "<why>" --epoch <e>`. The card goes
  back to ready with your branch kept, and no blame. If you pushed commits on it, add
  `--head <the 40-hex sha git ls-remote origin <branch> prints>` so the next attempt continues
  from them.

## Read a card

A read is an independent review of someone else's finished head. You are never dealt a read of
your own head.

1. Acknowledge it: the deal's `take` command. The first line is `TAKEN <id> repo=<org/repo> head=<40 hex> ...`.
   The brief follows, then `How this card finishes:` (read detached, edit and push nothing, and
   the exact answer lines), then `next: nova-sprint read <id>@<gen> yes|fix --as <you> --epoch <e>`.
2. Check out that exact head detached in a fresh directory of your own. Do not edit it, commit
   on it or push from it.
3. Read the diff against the base and the brief. Run the DONE-WHEN gates at that head, and keep
   each gate's command, exit code and log.
4. Say yes only when every DONE-WHEN item is met at this head. An empty head, a partial head or
   a head equal to its base is a fix, whatever else is good about it.
5. Say fix only for a real defect: an unmet DONE-WHEN item, a regression, a broken ruling, a
   build break, or a test that cannot fail. Anything else (style, a better name, a follow-up)
   is a note on a yes.
6. Answer: `nova-sprint read <id>@<gen> yes --as <you> --epoch <e> [--cost <usd>]` (same `--cost` rule as finish) with
   `--finding "<notes>"` when you have notes, or
   `nova-sprint read <id>@<gen> fix --finding "<path>:<line>: <the defect and the repair>" --as <you> --epoch <e>`.
   A fix finding names the file and line and what to change. Add `--cost <usd>` as finish
   takes it when you know what the read cost. It prints
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
7. Never open a PR; the landing opens the stream's.
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
- `<id> is not dealt to this row at this generation: nova-sprint next --as <you>`: the card
  moved (withdrawn, re-dealt, or a newer gen exists). Run `next`, and wait for the bus.
- `<id> was dealt before the deal became the take; the next tick withdraws it and deals it
  again, in hand`: a card dealt by an older sprint; wait for the bus and its new deal.
- `<id> is done` or `<id> is fail`: the card was reported or withdrawn; stop work on it.
- `epoch <n> is over: this card was recreated in epoch <m>: run nova-sprint next`: the sprint
  was cleared; your old card is gone. Run `next`.
- `<id> is not held by <you>`: it is someone else's, or it was withdrawn. Stop work on it.
- `head <x> is not a full commit sha`: report the full 40-hex sha.
- `push first: git push origin <branch>`: the head is not on the dealt branch at origin.
- `<n> of the <m> commits on <branch> since the base are not made as <name> <email>: <sha>
  (author <a>, committer <c>), ...`: run the two git config lines it prints in your clone so
  your next commits are made as it. Never rewrite the pushed commits to fix it: the next head
  must descend from them. Report a `finish --failed` naming it; the coordinator decides.
- `the sprint has no commit identity`: nothing may commit yet; tell the coordinator.
- `head <x> is where this attempt started; nothing was done`, or `head <x> has no commit
  beyond <base_sha> and origin/<base>; nothing was done`: an empty head is never a finish.
- `bring <branch> up to the stream head`: merge the group branch in (step 9), never rebase,
  re-run your gates, push, and finish again with the new head. A red gate that is
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
   `--failed --reason "brief-defect: <what>"`, so the card waits for the coordinator to fix the
   brief rather than another worker retrying it.
5. A refusal you believe is wrong is a defect in nova-sprint itself: send the coordinator the exact
   refusal line in one bus message, and do not route round it. The coordinator takes it to the
   project's own tracker; it is never a card and never a side loop.
6. Then wait for the bus. The coordinator answers by message or by a new deal.
