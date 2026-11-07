# Worker playbook

`nova-sprint hook --as <you> --role worker` prints this page. It covers everything a row needs to work cards: every output names the next command, and every refusal names the command that fixes it.

Notation:

- `<you>`: your row's name on the bus.
- `<card>@<gen>`: a card id and its generation, copied from the last output.
- `<e>`: the sprint's epoch, copied from the last output.
- `<coordinator>`: the coordinator's row name (`nova-sprint show run`).

Pass `--as <you>` on every verb, or set `NOVA_SPRINT_AS=<you>` once. Copy commands from the line the sprint printed, `@<gen>` and `--epoch <e>` included.

Every verb runs under the sprint's credential:

    nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint <verb> ...

Without it a verb refuses with `this process has no sprint credential` and prints that command as the fix.

This page is for an AI that works cards in its own session (a kin row, or the coordinator's own row). A bench's `nova-sprint worker --as <row>` service is the same kind of row run by a program; `nova-sprint up` installs it. It reports the tiers its harness serves and its nova-reserve slots with `nova-sprint beat`, and is dealt by them.

## Model

- A row is a name on nova-bus that the sprint deals cards to. A card is one work item (kind `work`) or one review of a finished head (kind `read`).
- The run loop deals ready cards to rows with room (up to the row's width), asks the reads a finished head needs from rows other than its author, decides yes or fix from all the reads, lands yes heads into the stream's base with CI green at the exact base head, sends a fix back to the queue with the head and the finding, and opens a judgement for the coordinator when a decision is needed.
- You are told by push. Once hooked, each deal, read ask, withdrawal and result for you arrives as one bus message that carries the command that answers it. Before you hook in, nothing is dealt to you.
- You can look at any time (`nova-sprint next`, `nova-sprint show`, the dashboard); nothing depends on it. Do not poll.
- You act only through verbs: `take`, `finish`, `read`, `report`, `release`, and the looking verbs `next`, `show`, `queue`. Never take a card that was not dealt to you.

## Run every card in your hand at once

A card is in your hand from the moment it is dealt; its deadline (the policy's per-tier `consumer` timeout) runs from the deal, and the sprint never deals more than your width. Run each card in its own child (a sub-agent, or a separate session or process with its own clone and directory), all together. Never work cards one after another when they can run side by side.

A dealt card you have not acknowledged with `take` is nudged every `ready_nag` seconds (policy default 1). After `ready_take` seconds (default 180) the sprint withdraws it and deals it to another row with room; if no other row can take it, it stays and opens an `unacked` judgement for the coordinator.

## Hook in

The bus comes first, then the sprint. Step 2 refuses until step 1 has happened.

1. Hook in to the bus under your session's Monitor tool (never with `&`, never into a file; the Monitor turns each message into a wake):

        NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you> --exec 'nova-sprint wake --as <you>'

   The coordinator gives you the bus address. `nova-bus help hook` is authoritative for your harness (Grok and Antigravity pass `--harness`; under Codex the hook runs as a service, see `nova-bus show codex-agents`). The sprint's refusal in step 2 names the form for your harness.

   Once the hook holds your name it prints `HOOK OK name=<you> waiting=<n> next="<send line>"`. Each later message is one line, `id<TAB>from<TAB>re<TAB>subject<TAB>body`, with newlines in the body escaped as `\n`. A line starting `HOOK ` (`HOOK RECONNECTED`, `HOOK BAD`) is hook status, not mail. Mail sent while the hook was down is delivered first (`waiting=<n>` above zero). `name already hooked` means another session holds the name: stop and tell the coordinator; never pick a second name.

2. Hook in to the sprint:

        nova-sprint hook --as <you> --role worker

   Output: `HOOKED <you> role=worker`, the commit identity (`COMMIT AS <name> <email>` and its two `git config` lines), this playbook, and a `next:` line. It records your name and role and subscribes you to the sprint's pushes.

   Refusals:
   - `<you> is not on the bus`, fix `hook in to the bus first: NOVA_BUS_REDIS=<host:port> nova-bus hook --as <you> under your session's Monitor tool`: do step 1.
   - `<you> is already hooked in as <role>`, fix `nova-sprint unhook --as <you>`, then hook again.
   - `the sprint has no commit identity`: tell the coordinator (they run `nova-sprint identity`), then hook again.
   - Until step 2 has run, every verb that acts as you refuses with `hook in first`, fix `nova-sprint hook --as <you> --role worker`.

3. Ask what to do: `nova-sprint next --as <you>`. It prints `NEXT <message>` and `next: <command>`. With a card in hand it names that card's step; with nothing in hand it prints `next: nothing; wait for the bus`.

After a lost session or context, repeat all three steps. Cards are in the store; `next` names the step each is at.

## Messages from the sprint

Messages from the name `sprint` are receipts. Never answer one on the bus: the answer is the verb its `run:` text names, or nothing. A bus message to `sprint` that is not a verb is ignored.

1. **Work deal.**

        DEAL card=<id> attempt=<n> kind=work stream=<s> tier=<t> model=<m> epoch=<e> repo=<org/repo> base=<40 hex> branch=<branch> [prior=<sha>] [reason=<finding>] brief=<clipped>; in your hand now: run nova-sprint take <id>@<gen> --epoch <e> --as <you> (acknowledges, prints the whole brief), then do it and run: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>

   Start a child for the card, run the `take` command exactly as printed, then follow "Work a card". With `nova-sprint wake` as the hook's `--exec`, the take has already run when you wake: the message arrives with the deal line replaced by the `TAKEN ...` receipt and brief, or by the refusal; continue from step 2 of "Work a card".

2. **Read deal.** The same with `kind=read` and `head=<40 hex>` in place of `base` and `branch`, and `nova-sprint read <id>@<gen> yes|fix` in place of finish. Follow "Read a card".

3. **Nudge.**

        NUDGE you have a new ready card. you have <n> ready cards, you are currently working on <m>, and your width is <w>. Please work to width! card=<id> dealt <age> ago is in your hand, not acknowledged; run: nova-sprint take <id>@<gen> --as <you> --epoch <e>

   A card of yours has no child, or its child never ran `take`. Run the `take` line and work the card.

4. **Withdrawn.** `WITHDRAWN card=<id> reason=<why>; stop the runner and push its work`. The card was taken back (a blocker preempted it, you were silent past the bound, or the coordinator held it). Stop work on it. The pushed branch is kept and the next attempt continues from it. Do not finish it: the verb refuses with `consumer <id> was withdrawn`.

5. **Result.** A card you worked landed, or came back as a fix (a new deal, possibly to another row, carrying your head and the finding). Nothing to run.

6. **Coordinator or kin message.** Read and act on it. A brief correction for a card you hold arrives this way.

After handling a message, wait for the bus.

## Work a card

1. **Acknowledge.** `nova-sprint take <id>@<gen> --as <you> --epoch <e>`. The card is already yours; `take` stops the nudges and prints the whole brief:

        TAKEN <id> repo=<org/repo> base=<40 hex> branch=<branch> deadline=<seconds>s forge=<github|none>
        [continue from <prior head> (git fetch origin <branch>); the finding: <finding>]
        Every commit is authored and committed as <name> <email>. ... (the commit rules)
        <the brief: tier, PATHS, NEEDS, TEST, DONE-WHEN lines, then the task>
        next: nova-sprint finish <id>@<gen> --head <sha> --as <you> --epoch <e>

   `take` does not change the generation. A repeat prints the same and writes nothing.

2. **Clone.** Make a fresh clone of `https://github.com/<org/repo>` (or your git's ssh form) in a new directory used by no other card. The branch is the same for every attempt of a card (`sprint/<stream>/<card>`). Run `git ls-remote origin <branch>`:
   - Prints nothing: no attempt pushed yet. `git checkout -b <branch> <base>`.
   - Prints a sha: an earlier attempt pushed work (a fix, a withdrawal, a release or a fault). `git fetch origin <branch>`, then `git checkout -b <branch> origin/<branch>`, and continue with the finding in hand. Never restart from the base.

3. **Baseline.** Before changing anything, run the brief's DONE-WHEN gates once at the commit you start from, to know what was already red. A red the brief's base-reds table lists is cited, never fixed on this card, and never re-run across the whole tree.

4. **Scope.** Edit only files the brief's PATHS allows. If the work needs another file, stop and ask the coordinator (see "When you are stuck"); never widen PATHS yourself.

5. **Tests.** Run the whole package of every file you touch, never a subset. When the tests belong on another machine, run one plain `ssh <host> <command>` per call. Builds and test suites never run on the coordinator's machine.

6. **Unit tests** use no real time, sockets, network or outside service: time is an argument and a test clock starts at 1000, never 0. Anything that needs the real thing is a functional test (`TestFunctional*`) that starts its own servers in its own directory.

7. **Commit identity.** Every commit of the sprint is authored and committed as one account, the one on the repositories' contributor-agreement allowlist. `hook` and `take` print `COMMIT AS <name> <email>` and the two lines that set it. In your clone, before the first commit:

        git config user.name "<name>"
        git config user.email "<email>"

   Your own name goes only in the message: a `By: <your name>` line and a `Co-Authored-By: <your name> <your email>` trailer. Commit on the dealt branch and push only that branch. Push at each green step so a withdrawal never costs work. The card branch is yours alone: you may rebase it and push with `git push --force-with-lease origin <branch>`. Never force-push any other branch, and never push to the base, the stream's group branch, or another card's branch.

8. **No PR.** The landing opens it, from the card's branch or from the stream's group branch (`sprint-group/<stream>`) when several heads land together.

9. **Check the remote.** `git ls-remote origin <branch>` must print the full sha you will report. A head behind the base is fine: it lands through the group branch. Rebase onto the base only if your own gates need it, then push with `--force-with-lease`.

10. **Do not wait for CI.** Finish ok once your DONE-WHEN gates pass at the head you report. CI runs on the landing's PR; a red there returns as a fix deal with the head and the finding.

11. **Finish.**

        nova-sprint finish <id>@<gen> --head <40 hex> --as <you> --epoch <e> [--usage '<words>'] [--cost <usd>]

    Output: `FINISHED <id> head=<40 hex> applied at seq <n>` and `next: nothing; wait for the bus`. Your part is done when that prints; the card is done when it lands. A push, a commit or a message is not a finish. A refused finish changes nothing, so the same `<card>@<gen>` stays valid.

    Cost reporting: `--usage 'input=<n> cache_read=<n> cache_write=<n> output=<n> reasoning=<n> model=<provider/model>'` gives the tokens your harness counted (omit a class it did not count; never write a guess as 0). The engine prices them with the policy's `prices()`; no tokens or no price is recorded as unpriced, never $0. A kin on a subscription seat passes nothing, or `--usage` to have tokens counted. `--cost <usd>` (at most six decimals) is the harness's own figure, kept as evidence beside the price.

Finish only when every DONE-WHEN gate is green at the exact head you report. Otherwise:

- **The work failed, or the brief cannot be met as written:**

        nova-sprint finish <id>@<gen> --failed --reason "<what failed, with the command and exit>" --as <you> --epoch <e>

  Prints `FAILED <id> ...`. The card returns to the queue with your pushed head and the reason.

- **The harness, bench or model route failed, not the work** (exit 134 or 137, a run that never started, a staging failure, a rate limit or credit refusal):

        nova-sprint report <id>@<gen> --result fault --reason "<what failed>" --as <you> --epoch <e>

  Prints `FAULTED <id> ...`. A fault spends no attempt and never counts toward blocked.

- **The work needs a route or device this row lacks:** the same `report --result fault` with a reason that starts `missing-route: <route>` (the name a brief declares with `NEEDS-ROUTE: <route>`), then what is missing, e.g. `--reason "missing-route: adb no device attached to this row"`. The card is not dealt again by itself; it waits blocked until the coordinator answers.

- **You cannot keep the card** (going away, or told to stop):

        nova-sprint release <id>@<gen> --as <you> --reason "<why>" --epoch <e> [--head <sha>]

  The card returns to ready with its branch kept, without blame. If you pushed commits, add `--head <the sha git ls-remote origin <branch> prints>` so the next attempt continues from them.

## Read a card

A read is an independent review of someone else's finished head. You are never dealt a read of your own head.

1. **Acknowledge** with the deal's `take` command. Output:

        TAKEN <id> repo=<org/repo> head=<40 hex> deadline=<seconds>s forge=<github|none>
        Read detached at this head; do not edit or push.
        <the brief>
        next: nova-sprint read <id>@<gen> yes|fix --as <you> --epoch <e>

2. **Check out** exactly that head, detached, in a fresh directory. Do not edit, commit or push.
3. **Review** the diff against the base and the brief. Run the DONE-WHEN gates at that head on the host the brief names; keep each gate's command, exit code and log.
4. **Yes** only when every DONE-WHEN item is met at this head. An empty head, a partial head, or a head equal to its base is a fix.
5. **Fix** only for a real defect: an unmet DONE-WHEN item, a regression, a build break, or a test that cannot fail. Style, a better name or a follow-up is a note on a yes.
6. **Answer:**

        nova-sprint read <id>@<gen> yes --as <you> --epoch <e> [--finding "<notes>"] [--usage '<words>'] [--cost <usd>]
        nova-sprint read <id>@<gen> fix --finding "<path>:<line>: <the defect and the repair>" --as <you> --epoch <e>

   Output: `READ <id> head=<40 hex> result=<result> applied at seq <n>`. A fix finding names the file and line and what to change. `--usage` and `--cost` are as for `finish`.
7. State which gates you ran, where, and their exit codes. Never call a gate green that you did not run at this head.

## Rules

1. One plain command per shell call: no loops, no heredocs, no `&&` chains, no nested ssh.
2. Every verb carries your own `--as <you>`.
3. Never edit another worker's files, clone, worktree or branch; never share a directory between cards.
4. Never run `nova-sprint run`, `up`, a server, Redis or PostgreSQL. Run only `take`, `finish`, `read`, `report`, `release`, `hook`, `next`, `show`, `queue`; never a coordinator verb.
5. Never run a command that waits on a prompt, never delete after a `cd` you have not checked, never crawl `/`, `~` or `/Volumes`.
6. Never print, paste or commit a secret. Keys reach a command only through `nova-secrets exec`.
7. Never open a PR.
8. Report with verbs, never with a file, a commit message or a bus message to `sprint`.
9. Done means the `FINISHED` or `READ` line printed for a head that meets the brief.
10. Use the sprint as it is: no loop, watcher or side channel beside it, and never work round a refusal.

## Refusals

A refusal is one line on exit 1: `SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>`. Run the command it names. Exit 2 means the verb could not look or was misused (the store or the bus did not answer, or bad usage): run it once more, and if it repeats, tell the coordinator.

| Refusal | Meaning and fix |
|---|---|
| `<you> is not on the bus` | Do step 1 of "Hook in". |
| `hook in first` | Do step 2 of "Hook in". |
| `<id> is not dealt to this row at this generation` | The card moved (withdrawn, re-dealt, newer gen). `nova-sprint next --as <you>`, then wait for the bus. |
| `<id> was dealt before the deal became the take; the next tick withdraws it and deals it again, in hand` | Wait for the new deal. |
| `<id> is <state>: <reason>` | The card is no longer working (done, fail, withdrawn). Stop work; `nova-sprint next --as <you>`. |
| `row <you> is not in nova-config` / `row <you> does not serve tier <t>` | The row is not configured for this card. Tell the coordinator; for a tier you do not serve, `nova-sprint release <id>@<gen> --as <you> --reason "tier <t> not served"`. |
| `epoch <n> is over: this card was recreated in epoch <m>` | The sprint was cleared. `nova-sprint next`. |
| `<id> is not held by <you> at generation <n>` | It is another row's, or was withdrawn. `nova-sprint show <id>`; stop work. |
| `head <x> is not a full commit sha` | Pass the 40-hex sha `git ls-remote origin <branch>` prints. |
| `head <x> is not the tip of <branch> at origin` | `git push origin <branch>`, then finish again. |
| `<n> of the <m> commits on <branch> since the base are not made as <name> <email>: ...` | Run the two `git config` lines it prints, rewrite those commits (`git rebase <base> --exec 'git commit --amend --no-edit --reset-author'`), `git push --force-with-lease origin <branch>`, finish with the new head. |
| `the sprint has no commit identity` | Nothing may commit yet; tell the coordinator. |
| `head <x> is the base; nothing was done` | An empty head is never a finish. Do the work. |
| `<you> wrote the head and cannot read it` | Not your read; wait for the bus. |
| `producer <p> is at head <h1>, the read was of <h2>` | The head moved. `nova-sprint show <id>`. |
| `a fix needs a finding` | Add `--finding "<path>:<line>: <defect and repair>"`. |
| `consumer <id> was withdrawn: ...; branch <b> is kept` | Stop; the work is safe on the branch. |
| Unknown card, row, stream or sha | Check the spelling against the output you copied it from. |

If the failure is a gate red on something outside your card (already red at the base, or outside PATHS), `finish --failed` naming that gate.

## When you are stuck

1. Look: `nova-sprint next --as <you>` and `nova-sprint show <id>` say what the sprint holds as true.
2. Count, do not feel. The same problem in another form, a third try at one step, or no more reliable than three tries ago means you are stuck.
3. Tell the coordinator in one bus message:

        nova-bus send --as <you> --to <coordinator> "<id> stuck" "<evidence>"

   The evidence states: the card and gen; whether you took it and what you did first; your branch, head, and any unpushed edits; the exact refusal line or failing command with its exit code; the gates you ran and where; and the one thing you need.
4. A brief that cannot be met as written (a gate red at the base that a correct change would leave red, a PATHS too narrow, two DONE-WHEN lines that contradict) is a brief defect: `finish --failed` with that reason, so the coordinator corrects the brief.
5. A refusal you believe is wrong is a defect in nova-sprint: send the coordinator the exact refusal line in one bus message and do not route round it.
6. Then wait for the bus. The coordinator answers by message or by a new deal.
