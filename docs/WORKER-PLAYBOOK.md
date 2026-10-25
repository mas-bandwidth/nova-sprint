# The worker's playbook

This is the starting path for an AI friend who has been assigned a sprint card.
Fleet members use the same card and finish contract, but their own staged packet
and harness setup govern their work. Read the current brief and `JOB.md` for
the exact commands; they override examples here. [FRIENDS.md](FRIENDS.md)
defines a friend's directories, staging and report format. `nova-sprint help
<verb>` is the command authority. At this source revision,
`nova-sprint help worker-playbook` is planned but does not exist; read this
file directly.

The short path is: prove your hook, check that your daemon and model route
can run, read the **current** card and `JOB.md`, work only in its staged
checkout, then use its finish line and confirm sprint accepted the result.
If any step refuses, keep its exact reason and use section 5.

## 1. Join and prove the work path

1. Check the installed versions and help: `nova-sprint version`, `nova-hook version`, `nova-friend version`, and `nova-sprint help`. The coordinator owns the configured friend row and its width/tiers. A friend hooks her own session with `nova-hook hook --as <name> --role friend --harness <harness>` and answers each `HOOK CHALLENGE` herself with `nova-hook prove --as <name> --nonce <nonce>`. A coordinator working her own friend row keeps **one coordinator-role hook**; her daemon joins that role. Never replace it with a second friend-role hook under the same name.
2. The installed `nova-friend` service runs work and read lanes. Confirm it with `nova-friend status --as <name> --dir <working-dir>` and inspect current assignments with `nova-sprint friend cards <name> --json`. The status file's age, hook proof, row hold, configured width, and lane refusals are different facts. An up hook or width alone does not prove a model ran a task.
3. For a Codex one-shot lane, check `nova-sprint routes --json` before interpreting a model refusal: the card's tier needs exactly one enabled subscription-backed Codex route admitted by that tier's **applied** route array. A configured route outside the applied array cannot launch a model. The coordinator fixes route configuration through supported nova-config/apply verbs; do not edit the store or the daemon's files.

## 2. Read the current assignment, then work

The daemon normally takes cards and renews their lanes. Do not start a competing take loop. A ready card is eligible; a card waiting on dependencies is not. A card shown as working needs a live lane and actual progress. If there is no card, report that the worker is ready but execution has not yet been exercised. Check the card with `nova-sprint card <card>` and its current events with `nova-sprint log --card <card>`; an old inbox, outbox or dashboard colour is not current ownership proof.

For a friend work card, use its delivered `BRIEF.md` and the staged `jobs/<job>/JOB.md`. If staging is needed, `nova-sprint friend stage <card>@<gen> --as friend.<name>` creates the checkout and names the exact finish command; it is idempotent for the same card, generation, branch and head ([FRIENDS.md](FRIENDS.md#where-a-jobs-work-lives)). Do not clone into the inbox or invent a branch. Use the current `CARD`, `GEN`, `EPOCH`, `BASE`, `NEXT`/`CARRY`, `PATHS`, `NEW`, and `SHARED` in the packet. Work only in allowed paths; propose a needed scope change to the coordinator rather than touching it. Keep checkout, worktree and build output in `jobs/<job>/`, and secrets out of source, reports, process listings and logs.

Before editing, inspect the base and run the required gate once to establish any pre-existing red. Then implement and run the brief's meaningful gates on the assigned bench or slot. For Go work, use the allocated Linux bench, not the coordinator's desktop; take and give an explicit shared lane when the brief requires one (`nova-sprint lane take`, `nova-sprint lane give`). Never rerun an unchanged head merely to get another result. Use a controlled test clock, not real time, sockets or network, where the brief calls for deterministic unit tests. Do a cold read of the final diff, check `git diff --check`, and record which checks ran, their host and result.

When blocked or coached, give the coordinator one compact evidence statement: whether the card was merely auto-taken, delivered, staged or actually executed; current branch/head and whether any edits exist; the exact refusal or failing command and exit status; gates already run; and the next help needed. Say explicitly when an auto-taken card never reached a worker, or when a result exists only locally and has not been admitted or accepted. Preserve active work, identify untouched cards that can be recovered, then demonstrate the corrected path through one accepted finish rather than treating configured width as running capacity.

## 3. Finish work or report a stop

Commit, push the **assigned** branch, and verify its remote tip equals the
full head you report. Follow the current packet's finish path. A staged
`JOB.md` may name `FINISH:` and `FINISH-FAILED:` commands using `nova-sprint
finish`; run the applicable complete line with the actual pushed head and
report. A job delivered through the friend inbox/outbox path instead ends by
writing `outbox/<job>/REPORT.md`: `Verdict: LAND` and `Head: <full sha>` for
completed work, or `Verdict: HOLD`/`FAIL` with a reason for stopped work
([FRIENDS.md](FRIENDS.md#a-sprint-card)). The friend sync loop collects that
report. Use the finish path the packet names; do not independently close the
same card a second time. Confirm sprint accepted the result: creating a file
or invoking a command is not proof the card finished. Use `nova-sprint
progress --as <worker> <card>@<gen> --epoch <n>` when the packet says to renew
an in-flight card, not as a substitute for a result.

Do not force-push or push to a base branch. Preserve a prior attempt's pushed head when the next packet names `CARRY` or `NEXT`; start from the current base and carry the named work as instructed. A partial or blocked attempt is reported honestly, not silently marked LAND.

## 4. Read a card independently

A read card is staged at the exact head under review, detached and push-disabled ([FRIENDS.md](FRIENDS.md#where-a-jobs-work-lives)). Inspect the brief, diff and relevant gates without editing or pushing that checkout. Return the verdict through `nova-sprint read --as <reader> --ok <read>@<gen> --epoch <n> --finding <evidence>` or `--broken ... --finding <file:line and repair>` as its `JOB.md` directs. A read ends with `read`, never `finish`; if evidence is unavailable, return it with the documented reason. Keep a source review distinct from a gate you personally executed.

## 5. Recover without inventing state

| Observation | Next check |
| --- | --- |
| Width is positive but no work starts | Check `friend cards` for **ready versus dependency-waiting** cards, `nova-friend status` for lane faults, and the applied route for the card's tier. Capacity is not an assignment or a running model. |
| Hook says up, daemon is stale/down | Check the installed service with `nova-friend status`; repeat the complete `nova-friend install` command with `--dry-run`, then use the supported install path if its arguments or binary need repair. The daemon does not prove model presence. |
| Hook down or messages not reaching the session | The friend inspects her own `nova-hook status --as <name>` and delivery route, reconnects via the supported hook path, then personally answers a **fresh** challenge. The coordinator may use `nova-friend check` and `nova-friend reach` to diagnose; neither can answer her challenge for her. |
| Lane reports a model/provider refusal | Read the exact refusal, verify installed versions, route enabled state and applied tier arrays, then fix the supported configuration. Clear a faulted lane with `nova-friend lanes --as <name> --clear-faults` only after the cause is corrected. |
| Report exists but card still working | Compare the current card owner/generation, branch tip and full `Head:` with `JOB.md`; inspect sync/collect refusal and report the mismatch. Do not hand-edit sprint state. |

The coordinator's [playbook](COORDINATOR-PLAYBOOK.md) owns cross-friend recovery, holds and upgrades. This page owns a worker's current assignment and evidence. Read the [tools playbook](https://github.com/mas-bandwidth/nova-tools/blob/main/docs/TOOLS-PLAYBOOK.md) for hook, daemon, config and secret-tool setup details; use installed `help` when it differs from a page.

## 6. Harness-specific first checks

| Harness | Check before the first card | Common refusal to resolve |
| --- | --- | --- |
| **Codex** | The active account is signed in for the one-shot CLI, the Codex session has a supported delivery route, and `nova-sprint routes --json` admits exactly one enabled subscription model for the card's applied tier. | A model is configured but excluded by the applied tier array, or the native delivery/session route cannot reach the current chat. A green hook does not clear either refusal. |
| **Claude Code** | The friend row has the required private `config_dir` for one-shot lanes; check `nova-friend check --settings` with its required `--as`, `--harness`, and `--dir` flags after install. | A missing or drifting harness settings directory, or a child Stop hook mistaken for the live friend's proof. |
| **OpenCode** | The installed daemon's `--model` resolves to the intended provider, and its settings pass `nova-friend check --settings` with `--as`, `--harness`, and `--dir`; the row/tier and provider route are applied. | A provider key or model route differs after a machine move; inspect the exact refusal before clearing lane faults. |

The current route table, not this page, chooses the model for each tier. Do not infer a tier's model from a past job or another friend's installation.
