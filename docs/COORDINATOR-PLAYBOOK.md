# The coordinator's playbook

The coordinator seat's workflows. Each one gives the situation, the verbs in the order you run
them, what success looks like, and what not to do by hand. `nova-sprint help playbook` prints this
file; it is compiled into the binary from `docs/COORDINATOR-PLAYBOOK.md`.

Each step quotes the verb's usage line exactly as `nova-sprint help` prints it, then the line to
run, then the spec section the step comes from. Where this file and a verb's help disagree, the
help is right and this file has the defect. A test (`cmd/nova-sprint/playbook_test.go`) holds every
`nova-sprint` usage line quoted here to the help, and every verb and flag named here to the verb
table, so a renamed or removed verb fails the build. The tools of nova-tools are held to their
own help in nova-tools `docs/TOOLS-PLAYBOOK.md`.

[SPRINT-COORDINATOR.md](SPRINT-COORDINATOR.md) is the runbook: the seat's rules, the day's loop
and the table of judgments. This file gives the order of verbs for each situation. Notation: `<s>`
is a stream, `<id>` is an inbox group id as the inbox prints it, `<n>` is the size the inbox
printed for that group, `<card>` is a primary, `<m>` is a fleet member, `<friend>` is a friend.

## Before you do anything by hand

The rule: find the verb first. The tools already do the seat's mechanical work. They deal, ask
reads, answer the mechanical judgments by rule, land, re-check a red base and finish friends'
cards from their reports. When the model does that work by hand, it adds a second writer, skips
the checks the verb runs, and leaves no record in the log.

1. Look for the verb. `nova-sprint help` lists every usage line, `nova-sprint help <verb>` prints
   one verb's flags, examples and exit codes, and `nova-sprint help <group>` prints one group (fleet,
   friend, reader, goal, stream, lane, merge-window). Every judgment in the inbox prints the commands
   that answer it: copy them and fill in each `'<...>'` (`nova-sprint help`, "reading the inbox
   and answering a judgment").
2. Ask what is waiting on you. usage: `nova-sprint view coordinator [--all] [--since <cursor>] [--json]`.
   Run `nova-sprint view coordinator --needs`. It lists every decision waiting on the coordinator
   (open judgments, held sentinels, stopped streams, held cards), ranked by the cards blocked
   behind each (`nova-sprint help view coordinator`; SPEC-SPRINT.md section 11,
   "view-coordinator-needs.w1").
3. If no verb does it, do not keep doing it by hand. Add a roadmap entry in the repository whose
   tool should own it, with `nova-work roadmap add --id <id> --release <v> --title <t> --origin <o>`
   (usage, `nova-work help`: `nova-work roadmap add [--repo <dir>] --id <id> (--release <v> | --group <g>) --title <t> [--text <t>] --origin <o> [--status planned|in-progress] [--why <w>]`).
   The roadmap verbs edit the repository's `docs/roadmap.sexp` and write `ROADMAP.md` in the same
   step. The runbook says the same: when a task keeps needing an outside script or a manual store
   edit, name the missing tool behaviour and propose it (SPRINT-COORDINATOR.md section 1, "Keep
   coordination in the tools").
4. A refusal is an answer. A verb that refuses prints why on its REFUSED line (`nova-sprint help`).
   Read it, and do not work round it (SPRINT-COORDINATOR.md section 1, "Keep coordination in the tools").
5. These are never done by hand (SPRINT-COORDINATOR.md section 1). No `redis-cli` and no edit of
   a store key: every change is a verb. No second `run`, `tick`, `land` or `play` beside the running
   server, because that makes a second writer. No unit file written by hand: `nova-redis install` and
   `nova-sprint install` write them. No secret printed, pasted or put on a command line. No
   `rm -rf`. `take`, `finish`, `read`, `fleet beat`, `friend beat`, `lane take` and `lane give`
   are the workers' verbs, never the seat's (`nova-sprint help`).

## 1. Session start

**Situation.** You take the seat: a new session, or the same session after a wrap-around.

**The verbs, in order.**

1. Hook in under a Monitor. usage: `nova-hook hook --as <me> --role <role> [--once] [--harness <name>] [--redis <addr>] [--dry-run]`.
   Run `nova-hook hook --as $NOVA_SPRINT_ACTOR --role coordinator` as the session's Monitor,
   with `NOVA_BUS_REDIS` exported. Each line it prints is one event: a message to you on the bus
   (the sprint's server sends every judgment and note for the coordinator there) or a
   `HOOK CHALLENGE`. Answer each challenge from inside your turn, using usage
   `nova-hook prove --as <me> --nonce <n> [--redis <addr>] [--dry-run]`, within the bound the
   challenge carries. Until the hook is proven, the coordinator's verbs against a live store are
   refused with `You must hook in first`. Spec: SPEC-SPRINT.md section 11, "The hook";
   SPRINT-COORDINATOR-SEAT.md, "The hook under a Monitor"; nova-tools SPEC-HOOK.md, "Roles and
   pushes".
2. See what changed. usage: `nova-sprint view coordinator [--all] [--since <cursor>] [--json]`.
   Run `nova-sprint view coordinator`, keep the cursor it prints, and next time run
   `nova-sprint view coordinator --since <cursor>`. The second run leaves out every item the
   earlier view showed that has not changed (it counts them as same) and counts the ones that
   stand no more (gone). Spec: SPEC-SPRINT.md section 11, "Role views".
3. Read the inbox. usage: `nova-sprint inbox [--open <group>] [--read] [--wait [--timeout <duration>] [--push <dir>]] [--deadline <duration>] [--stale <duration>]`.
   Run `nova-sprint inbox`, then `nova-sprint inbox --open <id>` for every member of a group.
   `nova-sprint inbox --wait` blocks until a judgment, or a note to the coordinator, that was not
   in the inbox when the wait began, or until the machine stops. Spec: SPEC-SPRINT.md section 8,
   "Push, not poll".
4. Check the units. usage: `nova-sprint units --check [--dir <dir>]`. Exit 1 names each unit that
   is missing or different, with the verb that installs it, for example usage
   `nova-sprint install <server|member|friend-sync|table> [--dir <dir>] [--log <file>] [--dry-run] (each kind's own flags are listed by install <kind> -h)`.
   Spec: SPEC-SPRINT.md section 14, "The system"; SPRINT-COORDINATOR.md section 1.
5. Run the doctors. usage: `nova-sprint doctor [--all] [--area <group>] [--preflight <candidate>] [--json]`.
   `nova-sprint doctor --all` runs nova-doctor and the sprint doctor in parallel, prints both
   reports, and writes nothing. Each definition-of-done fault is one line with its evidence.
   Spec: SPEC-SPRINT.md section 8, "No silent stops"; nova-tools SPEC-DOCTOR.md.
6. Check the seat. usage: `nova-sprint seat check`. It checks the server, store, loop, beats,
   readers, dashboard, installed versions and merge queue, and writes nothing. Spec:
   SPEC-SPRINT.md section 11, "The seat check".

**Success looks like.** `nova-hook status --as <me>` says proven (exit 0). `units --check` exits
0, `doctor --all` reports no FAIL, `seat check` is clean, and every open judgment is answered or
waiting on a decision you have named.

**Not by hand.** Do not prove a challenge from a script: only the model answers a challenge, and
no daemon, loop or script may call prove (`nova-hook help`). Do not write a unit file (step 4
names the verb). Do not start a server to look at the store (SPRINT-COORDINATOR.md section 1).

## 2. Running a friend row, the coordinator's own included

**Situation.** A friend should work cards, and the coordinator's own row is a friend row like any
other. The row lives in nova-config, nova-friend's daemon runs its lanes, and the sprint deals to
it.

**The verbs, in order.**

1. Set the row. usage: `nova-config <kind> set <name> --<field> <value> ... --actor <name> [--dry-run] [--json]`.
   For a claude friend in one-shot mode, run
   `nova-config friend set <friend> --config_dir <absolute dir> --actor <you>`. `--config_dir` is
   the directory a claude one-shot lane runs with as `CLAUDE_CONFIG_DIR`, and nova-friend run
   refuses a claude friend in one-shot mode without it. `--width`, `--tiers` and `--pronouns` go
   on the same row (`nova-config friend set -h`). Spec: nova-tools SPEC-CONFIG.md, "The kinds of
   this cut".
2. Copy the rows into the store. usage: `nova-sprint friend sync [--pg <dsn>] [--root <dir>]`.
   The friend sync loop, installed with `nova-sprint install friend-sync`, does this on its own;
   it writes each friend's width, mode and pronouns from the row. Spec: SPEC-SPRINT.md section 1,
   "The tables"; [FRIENDS.md](FRIENDS.md).
3. Install the daemon. usage: `nova-friend install --as <me> --harness <h> --dir <d> [--session <id>] [--adapter folder --delivery-dir <watched-dir>] [--server <addr>] [--silent-stop <d>] [--broken-after <n>] [--coordinator <seat>] [--state-dir <d>] [--redis <addr>] [--config-dir <d>] [--deny-self <d,...>] [--wall-reads <d,...>] --model <provider/model> [--secrets NAME[,NAME] --seat <seat>] [--launchd-log <file>] [--dry-run]`.
   Run it with `--dry-run` first, then without. It writes the launchd agent
   `com.nova.friend-<me>.plist` (RunAtLoad, KeepAlive), and running it again replaces the agent.
   A daemon whose arguments differ from the installed plist says "plist drift", and the fix is to
   run install again. Spec: nova-tools SPEC-FRIEND.md, "The loop" and "One-shot lanes".
4. Let launchd run the daemon. usage: `nova-friend run --as <me> --harness <h> --dir <d> [--session <id>] [--adapter folder --delivery-dir <watched-dir>] [--server <addr>] [--silent-stop <d>] [--broken-after <n>] [--coordinator <seat>] [--state-dir <d>] [--redis <addr>] [--profile <p>] [--config-dir <d>] [--deny-self <d,...>] [--wall-jobs <d,...>] [--wall-reads <d,...>] [--model <provider/model>] [--db <opencode.db>] [--lane-tiers <t,...>] [--lane-streams <p,...>] [--token-cap <n>] [--load-max <n>] [--load-width <n>] [--pause-on funds|any] [--refuse-go] [--dry-run]`.
   This is the loop launchd runs (install writes it), so you do not start it in a shell. Its
   lanes take work only while the friend is hooked in (`nova-friend help run`).
5. Confirm the daemon. usage: `nova-friend status --as <me> --dir <d> [--state-dir <d>]`. It prints
   `STATUS OK daemon=<up|down> ...` and then the friend's nova-hook record; exit 1 means no daemon.
6. Confirm the row in the sprint. usage: `nova-sprint friend cards <friend> [--json]`, and
   `nova-sprint where` draws the friends table. Spec: SPEC-SPRINT.md section 1.

**Success looks like.** `nova-friend status` says `daemon=up` with the hook proven and
`status=up`. `where` shows the row up with cards working, and finishes arrive without you.

**Not by hand.** Never run a hand take loop: `take --as friend.<name>` is the friend's lane's take
(`nova-sprint help`), and the daemon makes it. Do not finish a friend's card from her REPORT.md by
hand, because `nova-sprint collect` does that (section 6 below). Do not hand-edit the plist; run
install again. Do not use `friend give` to fill a row: the seat hands a named ready card to a
friend with friend give, and the deal fills the rest (`nova-sprint help`).

## 3. Reads and review

**Situation.** Cards are in review, and reads have to be asked, held and finished.

**The verbs, in order.**

1. Set how reads are asked. usage: `nova-sprint set [--rework-priority <fix|high|keep>] [--read-tier <flash|pro|default>] [--read-cards <on|off|default>] [--dealt-max <duration|default>] [--go-lanes <n|default>] [--alarm-review <n|off>] [--alarm-merging <n|off>] [--alarm-fleet <percent|off>] [--alarm-ready <on|off>] [--attempts <n|default>] [--friend-idle <duration|default>] [--landing-stall-bound <duration|default>] [--friend-finish <duration|default>] [--fleet-tiers <tiers|all>] [--friends-tiers <tiers|all>] [--reads <0|1|2|default>]`.
   `nova-sprint set --read-cards on` (the default) makes the tick ask every read a card in review
   needs at once, as read cards on the fleet table, dealt to friends and to members with a reader
   row. `nova-sprint set --reads <0|1|2|default>` sets how many ok reads at its head each card
   needs. Spec: SPEC-SPRINT.md section 6, "A read is a consumer card"; section 11, the `set` row.
2. Name the readers. usage: `nova-sprint reader add <reader>... [--tiers <flash[,pro,heavy,frontier]|all|default>]`
   and usage `nova-sprint reader set <reader>... --tiers <flash[,pro,heavy,frontier]|all|default>`.
   The judgments "review has no reader" and "no frontier reader has room" are answered by
   `nova-sprint reader add <reader>`, or by `nova-sprint reader add <reader> --tiers frontier`
   (`nova-sprint help`, "one answer to each judgment"). Spec: SPEC-SPRINT.md section 6, "The
   readers".
3. See the reads. usage: `nova-sprint reads --review [--stream <s>] [--json]`. It lists every
   primary in review and each of its read cards at its attempt: who holds each read, in what
   state, for how long, and the last read outcome (`nova-sprint help reads`; there is no spec
   section beyond the help).
4. Accept. usage: `nova-sprint accept (<id>... [--heavy --evidence <path> --reason <text>] | --stream <s> | --read-ok | --group <id> [--expect <n>]) [--answers <note>]`.
   The tick accepts every card with the ok reads it needs, every tick, and tells the seat.
   `nova-sprint accept --read-ok` is for a stuck case, and says "nothing waits: the tick accepts"
   when nothing is stuck. A heavy read of your own is recorded as
   `nova-sprint accept <card> --heavy --evidence <path> --reason <text>`. Spec: SPEC-SPRINT.md
   section 6, "accept-heavy-verdict-b.w1".
5. Answer what the rules leave you. A broken read with a finding is reworked by the rule
   read-broken, so only a read with no finding, or one at its brief's bound, reaches you. When
   the reads run out, run usage
   `nova-sprint ask [<id>... | --group <id> [--expect <n>]] [--stream <s>] [--max <n>] [--another] [--answers <note>]`
   as `nova-sprint ask --group <id> --expect <n> --another --answers <id>`. Spec: SPEC-SPRINT.md
   section 8, "Answered by rule".

**Success looks like.** The review count falls without your touch, `reads --review` shows every
read held and moving, and "ready to merge" notes arrive.

**Not by hand.** Do not move a card to merging on your own reading of its diff; `accept --heavy`
records your read with its evidence. Do not rework every broken read yourself, because the rule
read-broken already does. Do not ask reads in a loop, because the tick asks them.

## 4. Fix-column triage

**Situation.** Cards come back needing a decision: failed work, a broken read, a card at its
bound, or a brief that is wrong.

**The verbs, in order.**

1. Let the machine answer what it can. The run loop answers the mechanical judgments by rule
   (failed, bound, late, conflict, brief-defect, base-gate, read-broken). usage:
   `nova-sprint rules` prints what they would answer now. Spec: SPEC-SPRINT.md section 8,
   "Answered by rule".
2. Run the judgment decision. usage: `nova-sprint answer [--dry-run] [--bar <p>] [--every <duration>] [--timeout <duration>] [--backend jev|fixed] [--answers <file>] [--record <file>]`.
   Run `nova-secrets exec --only JEV_API_KEY -- nova-sprint answer --dry-run`, then the same with
   `--every 60s` as the seat's loop (the runbook gives the `--store`, `--as`, `--key` and `--sops`
   flags of `nova-secrets exec`). The verb the decision chose is applied at or above
   `decide_judgment_bar`. The bar ships empty, so nothing is applied and what a bar would apply is
   listed. Every drop, everything under the bar, and a provider's refusal for want of payment are
   listed for you. You set the bar on the sprint row with
   `nova-config sprint set --decide_judgment_bar <p> --actor <you>`, then
   `nova-config apply --kind sprint` (SPRINT-COORDINATOR.md section 2). Spec: SPEC-SPRINT.md
   section 8, "Answered by nova-decide".
3. Answer what is left, whole groups at a time, with the line the inbox prints.
   - usage: `nova-sprint rework (<id>... | --group <id> [--expect <n>] | <selector> [--dry-run]) [--fix <text>] [--tier <tier>] [--answers <note>] [--one]`.
     Run `nova-sprint rework --group <id> --expect <n> --answers <id>`. Each card's fix is its
     work's report or its reader's finding, `--fix` gives one fix for all, and `--tier` pins the
     tier. `nova-sprint rework <card> --carry --fix <text>` keeps the brief and starts from the
     latest pushed head, and it is the one verb that answers a bound judgment
     (`nova-sprint help rework`).
   - usage: `nova-sprint return (<id>... | --group <id> [--expect <n>] | <selector> [--dry-run]) [--reason <text>] [--answers <note>]`.
     This takes a merging card back to review.
   - usage: `nova-sprint drop (<id>... | --stream <s> --col <state> | --group <id> [--expect <n>] | <selector> [--dry-run]) --reason <text> [--answers <note>] [--one]`.
     `--cascade`, `--relink <primary>` or `--waive` decide what happens to the cards that need a
     dropped one (`nova-sprint help drop`).
   - A selector is `--stream <s>`, `--who <friend.<name>|friend|none>`,
     `--state <ready|waiting|held|merging>` or `--ids-file <path>`, combinable. The verb applies
     to every selected card or to none, in one store step. Plan it first with `--dry-run`. Spec:
     SPEC-SPRINT.md section 2, "One selector, one step".
4. Fix a wrong brief in place. usage: `nova-sprint brief <id> (--brief <text> | --brief-file <path>) [--rules <file>] [--answers <note>] | --dir <dir> [--rules <file>] | --group <id> [--expect <n>] (--brief-file <path> | --dir <dir>) [--answers <note>] | <id> --widen [--repo-dir <clone>] | <id> --tier <flash|pro|heavy|frontier> | <selector> (--set-base <branch> | --drop-who | --tier <t>)... [--dry-run]`.
   - `nova-sprint brief <card> --brief-file <path>` replaces the brief and keeps the id, so a
     correction is never a twin. A working card takes a semantic edit only with
     `--replace-in-flight`.
   - Add `--keep-head` to keep the in-flight or held head eligible for review. Without it, a
     finish made under the old brief returns for a fresh attempt.
   - `nova-sprint brief <card> --widen` widens the card's PATHS by the PATHS-PROPOSED line of its
     latest report and writes a CARRY: line naming that attempt's pushed head.
   - `nova-sprint brief <card> --tier <t>` re-tiers the card in place.

   Spec: SPEC-SPRINT.md section 11, the `brief` row; section 2, "recut-widen-r.w1".
5. Continue partial work. usage: `nova-sprint continue <card> [--note <text>] [--tier <flash|pro|heavy|frontier>] [--carry <branch|sha>] [--answers <judgment>] [--dry-run]`.
   This is for a card whose attempts pushed partial work and reached the brief's bound. It
   continues from the carried head (`--carry` names a branch or a 40-hex sha) and does not start
   over. Spec: SPEC-SPRINT.md section 7, "The continue verb".
6. Mend the dependencies. usage: `nova-sprint needs [--stream <s>] [--roots] | set <id> (--needs <a,b> | --land-after <a,b> | --clear) [--answers <note>]`.
   `nova-sprint needs set <card> --needs <a,b>` replaces a card's needs, and `--land-after <a,b>`
   sets soft dependencies: the work runs in parallel, and the lander lands the two in order. Spec:
   SPEC-SPRINT.md section 3, "The lifecycle of a primary"; "Soft dependencies".
7. Make a twin only when you mean a new id. usage: `nova-sprint recut <id> (--tier <flash|pro|heavy|frontier> | --brief-file <path> [--rules <file>]) [--new <id>] | <selector> (--tier <t> | --set-base <branch> | --drop-who)... [--dry-run]`.
   `recut` re-cuts the card as its twin, with the next letter as its id unless you give `--new`.
   `recut --widen` is retired: a PATHS widening uses `brief <id> --widen` (`nova-sprint help recut`).
   Spec: SPEC-SPRINT.md section 2, "One selector, one step".

**Success looks like.** Every decision you make carries `--answers`, so its judgment closes. Each
verb prints a MOVED line for every card, and the inbox holds only what needs a mind.

**Not by hand.** Do not drop a card and add it again to fix its brief: `brief` edits it in place
(SPEC-SPRINT.md section 11, the `brief` row). Do not resolve a conflict on the card's branch, which
no reader has read; rework or drop it instead (SPRINT-COORDINATOR.md section 2). Do not loop over
cards one process at a time when a selector does them in one step (SPEC-SPRINT.md section 2, "One
selector, one step"). Do not compose a command when the inbox prints one.

## 5. Known base reds

**Situation.** The base fails its tree gate, or a card's failing test was already red at the base.

**What the machine does by itself.** A queued head whose tree passes the gate on that base lands
first, as the base fix (the base cure). With no such head, land gates the base again after 2
minutes and after 5. The third failure stops the stream that met it, with one judgment for that
base that names the failing tests. Every other stream that meets the same red is refused under
that judgment and is never stopped. Each land pass re-checks the tip of a base that stopped a
stream, and a green tip resumes, by the rule base-gate, every stream stopped only on that red.
Spec: SPEC-SPRINT.md section 8, "Answered by rule" (`base-gate`), "land-base-gate-stops-stream"
and "v11-base-red-auto-resume-now".

**The verbs, in order.**

1. Classify the card-level reds. On the sprint row, run
   `nova-config sprint set --decide_gate_preexisting <p> --actor <you>`, then
   `nova-config apply --kind sprint`. A work card's failing test whose p(pre-existing) is at or
   above the bar is reported `pre-existing: <test>`: it is the base's or the member's, never the
   card's. The bar is empty by default, which reclassifies nothing. 0.8 is the starting point,
   though at 0.8, 24 of the calibration's 39 flaky failures would have been reported pre-existing
   (`nova-config sprint set -h`). Spec: SPEC-SPRINT.md section 5 (the gate verdict); nova-tools
   SPEC-NOVA-DECIDE.md section 12.
2. Fix the base with a card. usage: `nova-sprint add --stream <s> (<id>... | --count <n> | --sentinel <id> | --brief-dir <dir> | --brief-file <f1> [--brief-file <f2>...]: a card per file, its id the file's name without .md) [--needs <a,b>] [--before <id> | --after <id> | --score <n>] [--brief <text> | --brief-file <path>: once, the brief of the cards named] [--rules <file>] [--held] [--one: a single card is meant] [--replaces <old-id>[,<old-id>]: the one card is their twin] [--allow-personal-base] [--land-protected <owner/name,...|any>] [--plan <dir>: print the plan review's overlap table and exit 0 without admitting]`.
   The base cure lands the fix head first.
3. Report a base red the lander has not seen. usage: `nova-sprint merge --stream <s> [--batch <n>] [--conflict <id> [--conflict-kind file|ledger] [--conflict-path <p>...] | --cross <id>=<other> | --red [--suspect <id>...] | --rejected | --base-red <error>] [--note <text>]`.
   `nova-sprint merge --stream <s> --base-red <error>` reports the fact that the base fails its
   tree gate: the stream stops, and no card moves. merge is a report (`nova-sprint help`), so use
   it only for a fact the lander has not recorded.
4. Resume what the rule did not. usage: `nova-sprint resume --stream <s> [--did <text>] [--answers <note>]`.
   For a stream the rule left stopped, run
   `nova-sprint resume --stream <s> --did '<the base is green again>' --answers <id>`, the line the
   inbox prints for "the base fails its gate".

**Success looks like.** One judgment for the base, not one per stream. The fix lands first, and
the streams resume by rule when the tip is green.

**Not by hand.** Do not fast-forward the base by hand: the base cure exists because the coordinator
did that on 2026-10-05 (SPEC-SPRINT.md section 8, "land-base-gate-stops-stream"). Do not resume
streams one by one while the base's judgment stands, because the rule resumes them on green.

## 6. A friend going down

**What happens without you.** nova-hook's heartbeat server challenges each hooked friend, and a
silent friend's record goes down. The sprint's tick writes each friend's status from that record
and never works it out: down whenever the hook record is down (`nova-sprint help`, "The friends").
The heartbeat server sends the coordinator FRIEND DOWN and FRIEND BACK judgments
(`nova-hook help hook`). The friend's daemon stops a lane turn that stays silent past
`--silent-stop`. After the same provider refusal on `--broken-after` lane turns in a row, it marks
the session broken and tells the seat once. On a provider failure (out of funds) it stops every
lane and writes PAUSED (`nova-friend help run`, `nova-friend help resume`). A friend that is down
shows working 0, and her cards stay on her row and count again when she is up
(`nova-sprint help`). Spec: SPEC-SPRINT.md section 1, "The tables"; nova-tools SPEC-FRIEND.md,
"Presence"; SPEC-HOOK.md, "The heartbeat".

**The verbs, in order.**

1. Look. usage: `nova-friend check [--as <coordinator>] [<friend>...] [--since <duration>] [--shown <file|->] [--harness codex --dir <d> --session <id> --adapter folder --delivery-dir <watched-dir>] [--json]`.
   It prints a CHECK VERDICT line per friend (ok, broken, down or untrue) with the reason.
2. Reach her. usage: `nova-friend reach --as <coordinator> --to <friend> [--step-timeout <duration>] [--from <bus|push|window>] [--harness <h>] [--dir <d>] [--session <id>] [--state-dir <d>] [--redis <addr>] [--dry-run]`.
   It climbs the ladder (a bus message, then a push into the session, then her window) and stops
   at the first proof. Spec: nova-tools SPEC-FRIEND.md, "Reach".
3. Hold her row when she will not be back soon. usage: `nova-sprint hold <member|reader|friend|stream>... --reason <text> [--return] [--dry-run]`.
   The old word for this is usage `nova-sprint friend down <friend> [--reason <text>] [--until <RFC3339>]`,
   kept for one release. A held friend keeps no card: every card on her row goes back to ready,
   and a started card with a push carries its pushed head to the next taker. Release her with
   usage `nova-sprint unhold <member|reader|friend|stream>... [--reason <text>] [--dry-run]`.
   `friend up` cannot prove the friend alive (`nova-sprint help`, "Hold and unhold"). Spec:
   SPEC-SPRINT.md section 11, the `hold` and `friend down, friend up` rows.
4. Finish what her reports already finish. usage: `nova-sprint collect [<friend>...] [--dead-lanes] [--pg <dsn>] [--root <dir>] [--dry-run]`.
   Run `nova-sprint collect --dry-run`, then `nova-sprint collect --dead-lanes`. collect finishes
   every working card that a report in a friend's outbox finishes. `--dead-lanes` finishes, as
   failed, each card with no report whose runner ENDed its job, so the card is dealt again. Spec:
   SPEC-SPRINT.md section 1, "Collect".
5. Settle a stop's debt. usage: `nova-sprint stop-return --as <owner-row> <card>@<gen>... --epoch <n> --reason <cancel acknowledgement> [--dry-run]`.
   This answers "STOP debt: an owner has not beaten since the stop" once you have seen the
   child exit (`nova-sprint help`, "one answer to each judgment"). Spec: SPEC-SPRINT.md section
   14, "The system".
6. On her machine, after a person deals with the provider, use usage
   `nova-friend resume --as <me> [--dir <d>] [--state-dir <d>] [--dry-run]` to clear PAUSED, and
   usage `nova-friend lanes --as <me> [--clear-faults] [--dir <d>] [--state-dir <d>]` to clear a
   faulted lane. A friend a person held with `nova-sprint friend down` stays held until
   `nova-sprint friend up` (`nova-friend help resume`).

**Success looks like.** Her cards are back in the deal or finished, the row reads down or held with
a reason, and a FRIEND BACK note follows when she returns.

**Not by hand.** Do not finish her cards by reading her outbox yourself; that was the stopgap
`finish-loop.py`, which collect replaced (SPEC-SPRINT.md section 1, "Collect"). Do not take her
cards back one by one when `hold` takes them all. Do not answer her challenge for her: only the
model answers a challenge (`nova-hook help`).

## 7. Fleet health

**Situation.** A machine is slow, loaded, missing, full, or not what nova-config says.

**The verbs, in order.**

1. Find the fault. usage: `nova-sprint doctor [--all] [--area <group>] [--preflight <candidate>] [--json]`.
   Run `nova-sprint doctor --area fleet`, or `--all`. On a machine, run usage
   `nova-doctor [run] [--check <name>]... [--dir <path>] [--local] [--strict] [--json]`; each
   check is ok, warn or fail, with evidence and a fix line, and it changes nothing. Spec:
   SPEC-SPRINT.md section 8, "No silent stops"; nova-tools SPEC-DOCTOR.md, "The checks".
2. Match the fleet to nova-config. usage: `nova-sprint fleet sync [--check] [--pg <dsn>]`.
   `nova-sprint fleet sync --check` prints the drift and writes nothing (exit 0 means none, 2
   means some, 3 means the config cannot be read). Then `nova-sprint fleet sync` makes the fleet
   match the machine rows in one step. A width is set on the row with
   `nova-config machine set <m> --width <n> --actor <you>`. Spec: SPEC-SPRINT.md section 5, "The
   fleet".
3. Quiet a loaded machine. usage: `nova-sprint fleet quiet <member> (--for <duration> | --until <RFC3339>) --reason <text> | <member> --end [--dry-run]`.
   The deal gives the member nothing until the quiet ends. Spec: SPEC-SPRINT.md section 5,
   "fleet-quiet-machine-b.w7".
4. Hold a machine that must take nothing. usage: `nova-sprint hold <member|reader|friend|stream>... --reason <text> [--return] [--dry-run]`.
   Give it `--cause <j<n>>`, an open judgment, so the tick ends the hold when that judgment is
   answered. A hold with no cause is reported every 2 hours (`nova-sprint help hold`). Spec:
   SPEC-SPRINT.md section 11, the `hold --cause` row.
5. Add a member, or set its width. usage: `nova-sprint fleet up <member> [--width <n> | --width 0]`.
   `--width 0` drains the member: no new deals, its untaken ready cards are levelled away, and its
   working cards finish. Spec: SPEC-SPRINT.md section 5.
6. Reclaim scratch. usage: `nova-sprint gc [--machine <m>] [--dry-run] [--max-age <d>]`. Run
   `nova-sprint gc --dry-run`, then without `--dry-run`. gc never touches a path under no known
   scratch root, and never a clone with work that is nowhere else. Spec: SPEC-SPRINT.md section
   1, "gc: the system's scratch is reclaimed by a verb".

**Success looks like.** `fleet sync --check` exits 0, `doctor` reports no fleet FAIL, and every hold
names a cause.

**Not by hand.** Do not `rm -rf` job directories or caches; gc removes them. Do not edit the fleet
table; `fleet sync` and `fleet up` write it. A member pegged at 90% or more with no card working is
a judgment that tells you to read the leftover process on the machine (ps), kill it by PID, or
hold the member (`nova-sprint help`, "The fleet"). Never kill a process blindly.

## 8. Priorities and order

**Situation.** The wrong work goes first.

**The verbs, in order.**

1. Choose what a rework's next attempt gets. usage: the `set` line in section 3 above;
   `nova-sprint set --rework-priority <fix|high|keep>`. This sets the level a normal or low card
   gets when its next attempt opens (default fix). Spec: SPEC-SPRINT.md section 1, "Priority:
   reads are a card priority".
2. Set levels. usage: `nova-sprint priority <id>... | (<id>... | --stream <s> | (<selector> [--dry-run])) (--blocker | --critical | --fix | --high | --normal | --low) --reason <text>`.
   With no level, it prints the levels and writes nothing. With a selector, it sets every selected
   card in one step, for example `nova-sprint priority --state ready --critical --reason <why>`.
   Spec: SPEC-SPRINT.md section 2, "One selector, one step".
3. Reorder. usage: `nova-sprint rank (<id>... | <selector> [--dry-run]) (--score <n> | --first | --before <id>) [--answers <note>]`.
   The judgment "needs another stream first" is answered by `nova-sprint rank <other> --first`.
   Spec: SPEC-SPRINT.md section 4, "Order".
4. When work is waiting, not ready, look at what holds it: usage
   `nova-sprint needs [--stream <s>] [--roots] | set <id> (--needs <a,b> | --land-after <a,b> | --clear) [--answers <note>]`,
   usage `nova-sprint held [--stream <s>]` and usage `nova-sprint sentinels [--stream <s>]`.
   Raising priority only reorders cards that are already ready (SPRINT-COORDINATOR.md section 1).

**Success looks like.** The work you meant to go first is dealt first, and `where` shows it.

**Not by hand.** Do not drop a card and add it again to move it; `rank` and `move` place it. Do not
raise priority on waiting cards when their needs hold them (step 4).

## 9. Releases

**Situation.** Landed work is ready to ship, or a built release is ready to install.

**The verbs, in order.**

1. Gate. usage: `nova-sprint release check [--json] [--streams <glob>] [--window <duration>] [--merge-p90 <duration>] [--check <name>]...`.
   The checks are no-stuck-friend, cards-settled, base-gate-green, two-ok-reads, prose-true,
   landings-promoted, no-open-judgment and merge-queue-p90, and it writes nothing. Spec:
   SPEC-SPRINT.md section 11, "release-check-frame"; nova-tools SPEC-RELEASE.md section 16, "release
   check is the gate, and a release ships when it says OK".
2. Cut and build with nova-tools' release verbs (usage lines in `nova-update help release`):
   `nova-update release cut --repo <owner/name> --from <branch> --version <v> --changelog <path> ...`,
   `nova-update release build --version <v> --out <dir> --source <dir> ...`, or the whole
   fix, land and install loop in one command, `nova-update release cycle --version <v> --source <dir> --sprint-release <dir> --out <dir> --inventory <file> --benches <a,b,...> --reason <why> --ansible <path> ...`.
   The cut writes the annotated tag that carries the SUMS digest. Spec: nova-tools SPEC-RELEASE.md
   section 2; SPEC-UPDATE.md, "The release verb".
3. Preflight the candidate. usage: `nova-sprint doctor [--all] [--area <group>] [--preflight <candidate>] [--json]`.
   `nova-sprint doctor --preflight <candidate binary>` runs the release gate's frame before any
   server switch or adopt, and exits 1 on any FAIL (`nova-sprint help doctor`).
4. Adopt. usage: `nova-sprint adopt <version|path> --source <checkout> --sprint-release <dir> --inventory <file> --reason <text> [--limit <host>] [--receipts <dir>] [--dry-run]`.
   Run it with `--dry-run` first. The play runs the new build's checks on the seat before anything
   changes, and a refusal in the window puts the tools of before back. Spec: SPEC-SPRINT.md
   section 14, "Adopting a build".

**Success looks like.** `release check` says OK, the cut prints its receipt, and adopt finishes
with every agent on the new build.

**Not by hand.** No bare `git tag`, because the cut writes the annotated tag with its digest
(nova-tools SPEC-RELEASE.md section 2). Do not copy binaries to machines by hand; adopt and cycle
install them. Do not swap a server binary by hand; `nova-sprint server switch` or adopt do that,
after a shadow tick (SPEC-SPRINT.md section 14, "install-canary-shadow-tick-r.w1").

## 10. Watching

**Situation.** Nothing needs you right now, and you want to be woken when something does.

**The verbs, in order.**

1. Block until something wakes you. usage: `nova-sprint watch --wake [--every <duration>] [--state <file>] [--check <duration>] [--judgment-every <duration>] [--merge-every <duration>] [--backlog-every <duration>] [--land-after <duration>] [--merge-over <n>] [--merging-over <n>] [--review-over <n>]`.
   It prints `WAKE <kind> <time> <evidence>` and exits 0; the session runs it again. Spec:
   SPEC-SPRINT.md section 11, "coordinator-wake-verb.w1".
2. Find stuck cards. usage: `nova-sprint audit [--stream <s>] [--stuck <10m>] [--json]`.
   `nova-sprint audit --stuck 10m` marks STUCK any primary that has stayed in its state for longer
   (`nova-sprint help audit`; there is no spec section beyond the help).
3. Read what happened. usage: `nova-sprint log [--card <id>] [--stream <s>] [--member <m>] [--since <10m|RFC3339>] [--at-epoch <n>]`.
   Spec: SPEC-SPRINT.md section 17, "The log".
4. See the tables. usage: `nova-sprint where [--watch] [--every <duration>] [--all] [--json [--cards] [--rows] [--dashboard] [--archived] [--stale <duration>] [--at-epoch <n>]: includes landedSeries] [--release [<name>]]`.
   Spec: SPEC-SPRINT.md section 1, "The tables".
5. Set a timer. usage: `nova-sprint remind (--in <duration> | --at <time>) --note <text> [--for <actor>] | --list | --cancel <id>`.
   The tick of a RUNNING machine raises it once, as a judgment of kind timer, at its due time.
   Spec: SPEC-SPRINT.md section 19, "Timers".
6. Look again later. usage: `nova-sprint wait (<note>[,<note>]... | --group <id> [--expect <n>]) (--for <duration> | --until <RFC3339>)`.
   It sets a judgment's next review time. Never use it on a judgment that holds a card
   (`nova-sprint help`, "overdue: act"). Spec: SPEC-SPRINT.md section 11, the `wait` row.
7. Keep a person on their goal. usage: `nova-sprint goal set <name> [--file <path>] [--to file:<path>]`.
   While the machine is RUNNING, the tick delivers each person's goal down their route at start
   and every 5 minutes of running time. Spec: SPEC-SPRINT.md section 15, "Reminders".

**Success looks like.** You are woken by a WAKE line, a hook push or a timer, never by a sleep you
chose.

**Not by hand.** Do not poll `where` in a sleep loop; the hook, `inbox --wait` and `watch --wake`
push to you. Do not keep a reminder in your head when `remind` keeps it in the store.
