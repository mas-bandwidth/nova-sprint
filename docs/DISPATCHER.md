# dispatcher: coordinating at low cost

This is a practice for an AI coordinator whose sprint has grown past what one
session can route without spending its whole day on routine steps. The
coordinator keeps the judgment; a second session, the dispatcher, runs the
mechanical loop on a cheaper model or a lower effort. Every verb named here is
in `nova-sprint help`, `nova-hook help` or `nova-bus help`; check the help of
your own build before copying a line.

## 1. Split by judgment, not by size

Sort each kind of step by whether it needs a decision, never by how much work
it is. These are routine, and the dispatcher runs them:

- Take a dealt card and start a worker on it: `nova-sprint take --as <member> <card>@<gen>`,
  or for a friend's lane `nova-sprint take --as friend.<name> <card>@<gen> --lane <n>`.
- Prove its own hook's challenges: `nova-hook prove --as <me> --nonce <n>`, from inside its own turn.
- Merge only on an independent reader's MERGE verdict that names the exact head being
  merged; a head that moved since the read is read again (`nova-sprint ask <id> --another`).
- Re-finish a known head after a bounce caused only by the environment:
  `nova-sprint finish --as <member> <card>@<gen> --epoch <n> --head <commit>`.
- Apply a scope widening that arrives with an exact file list (its report's
  PATHS-PROPOSED line): `nova-sprint brief <id> --widen`.
- Relay: `nova-bus send --as <me> --to <name> --subject <s> --body <text>`.

These are judgment, and go to the coordinator: a new ruling, a HOLD, a red
base with no known cause, anything touching privacy or security, and a card
stuck by count (step 7).

## 2. Run a dispatcher

The dispatcher is a separate session on a cheaper model or a lower effort. It
runs the loop above and sends the coordinator only judgment.

- The coordinator keeps the conversation with the owner, the rulings, and the
  coordinator hook (`nova-hook hook --as <coordinator> --role coordinator`).
  Only the session holding a hook answers its challenges; the dispatcher never
  runs `nova-hook prove` for the coordinator's name.
- The dispatcher hooks in under its own name to use the bus
  (`nova-hook hook --as <dispatcher> --role comms`) and hears the coordinator's
  judgments through the push loop, `nova-sprint inbox --wait --push <dir>`.
- A coordinator verb runs only while the judgments have a way to the seat
  (the gate in `cmd/nova-sprint/coordinator_hook.go`), so the dispatcher's
  routine steps stop when the coordinator's hook is gone. That is intended:
  nothing routes while nobody can judge.
- Write the ownership split down: which cards each session holds
  (`nova-sprint friend cards <friend>`, `nova-sprint view cards --holder <member>`).
  Two workers must never take one card.
- Hard rule: a card on its 4th or later attempt goes to the coordinator.
  `nova-sprint card <id>` shows the attempts.

## 3. Tier workers by effort

- Low effort: base merges, formatting and vet fixes, re-finishes, swaps.
- Medium or high effort: design, models, reviews, research.
- A cold reader, with no memory of the work, for anything high-blast or public.

The card's own tier is set apart from the session's effort: line 1 of its
brief (`tier: flash|pro|heavy|frontier`), `nova-sprint tier <card> --set <t> --reason <text>`,
and `nova-sprint rework <id> --tier <t>`. A stream's readers take
`nova-sprint stream set <stream> --read-tier <t>`.

## 4. Push, never poll

Every state someone waits on should arrive as a push. A slow backstop sweep
stays only to catch a lost push, and it is not the mechanism.

Pushes that exist:

- Judgments and notes for the coordinator: the sprint server sends each to the
  coordinator's name on the bus, and `nova-hook hook` puts it into the session
  (`cmd/nova-sprint/coordinator_push.go`). A local copy: `nova-sprint inbox --wait --push <dir>`.
- A card dealt to a friend: the deal sends the friend a bus message
  (`cmd/nova-sprint/friendcards.go`).
- A friend down: `nova-hook serve` challenges each hooked name every minute and
  unhooks one silent past its bound; `nova-hook friends` shows the record.
- A stream stopped (red, conflict, rejected) and CI red reported by
  `nova-sprint ci <id> --red`: each is a judgment, pushed as above.
- A late tick: pushed to the coordinator (`cmd/nova-sprint/pushlate.go`).
- Messages between sessions: `nova-bus recv --as <me> --forever --exec <command>`,
  or `nova-bus wait --as <me>`.

Not yet pushes:

- `nova-sprint answer` runs a pass every `--every <duration>`; waiting on the
  inbox stream instead is still being built.
- CI finishing reaches the sprint only when something runs `nova-sprint ci`.
- A grant expiring has no push.

The backstop: `nova-sprint watch --wake --check <duration>`.

## 5. A worker for every taken card

A finish frees a lane, and the freed lane takes the next card at once. Start a
worker for that card in the same step, or it sits in working with nobody on
it. Check with `nova-sprint friend cards <friend>` and
`nova-sprint view cards --col working`.

## 6. Commit identity

Set the commit identity in every clone before its first commit
(`git config user.name` and `git config user.email`, without `--global`).
Never inherit the machine's global identity. The owner decides which identity
agents commit under; the sprint machine's own is its row's `git_identity` in
`nova-config`.

## 7. Stuck by count

Count attempts, not feelings. The machine counts too: a card at its attempts
bound (`nova-sprint stream set <stream> --attempts <n>`, default 4) or with the
same finding twice comes to the inbox as "the brief is wrong".

- The same finding three times: stop, and take it to the owner with the evidence
  (`nova-sprint log --card <id>`, the readers' findings).
- When readers judge against a stale brief, re-admit the card fresh, carrying
  the good head: `nova-sprint add --stream <s> <new-id> --replaces <old-id> --brief-file <f>`,
  the brief naming the head on a `CARRY:` line. `nova-sprint continue <card> --carry <sha>`
  keeps the same id instead.

## 8. Measuring it

Before moving more of the loop to low effort, run a trial block and compare it
with a block the coordinator ran alone:

- judgments overdue (`nova-sprint inbox`, the overdue lines; `nova-bus overdue`);
- re-attempts that changed nothing (`nova-sprint log --card <id>`);
- time from a stream stopping to its `nova-sprint resume`
  (`nova-sprint log --stream <s> --since <time>`);
- errors the owner had to catch, counted by hand.

Move more to low only when none of these got worse.
