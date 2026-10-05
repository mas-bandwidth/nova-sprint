# The AI coordinator's guide

The coordinator keeps the team moving from a goal to reviewed, landed work.
This role can be held by an AI: the human does not need to stay awake to hand
out cards, notice finished work, arrange reviews, or follow merges. A human
can use the same commands when they want to take part.

Use this guide after agreeing on the goal and the coordinator's authority.
For an introduction to cards, streams, and overnight work, start with
[Working with AI teams](WORKING-WITH-AI-TEAMS.md). For exact state transitions,
use the [sprint contract](SPEC-SPRINT.md).

The examples below describe the command surface in this checkout. Deployment
examples use Nova Tools for configuration and secrets. Replace placeholders
with values from your deployment; use the installed command's help when its
version differs. `<card>` is a task ID, `<s>` a stream, `<m>` a machine, and
`<id>` an inbox item or group.

## 1. The seat

The **seat** is the coordinator identity recorded when the sprint is
initialised. The person or AI holding it can change the plan and answer the
inbox. Start by reading the previous coordinator's handover and connecting
to the existing server.

One server writes the live sprint. It runs beside Redis and owns the tick
loop. Clients send commands to it through `NOVA_SPRINT_SERVER`; read commands
need that address, and coordinator commands also need the correct actor:

```sh
export NOVA_SPRINT_SERVER=127.0.0.1:<port>
export NOVA_SPRINT_ACTOR=$(nova-sprint where --json | jq -r .coordinator)
```

Read the port and other deployment values from the supervised server's
configuration. Do not guess them from an old handover. In a Nova Tools fleet,
`nova-config loop list` and `nova-config loop show sprint-server-<m>` identify
the loop; its service unit contains the startup arguments. See the
[credential guide](SPRINT-COORDINATOR-SEAT.md) for extracting the values and
running local configuration commands.

The server is started with `nova-sprint run --listen <address>:<port> --land`
when automatic landing is configured. Do not start a second writer beside
it. `run`, `tick`, `land`, and `play` execute locally, as do commands with
an explicit `--redis`; they are not ways to repair an unresponsive server.
Investigate the service first. Make state changes through sprint commands,
not by editing Redis keys.

## 2. The day's loop

Read the current state, decide what needs attention, then check the result:

```sh
nova-sprint where
nova-sprint inbox
nova-sprint inbox --open <id>
nova-sprint card <card>
nova-sprint log --card <card>
```

`where` shows the work and available team capacity. `card` explains one task,
and `log` supplies its history. The inbox separates notifications about what
happened from **judgments** that need a decision.

An AI coordinator can run this loop throughout an unattended session. Use
the wait or push mechanism supported by the installed build to receive new
work, rather than relying on a human to prompt the next check. The exact
behaviour of `inbox --wait` and `--push` is in the installed help and
[command reference](CLI.md). A persistent coordinator session and its wake
mechanism are part of the deployment, not something a saved queue supplies.

Open a judgment before answering it. The inbox prints the commands available
for that situation; fill in their placeholders instead of inventing a state
change. `--group <id> --expect <n>` checks that the group still has the size
you read. `--answers <id>` records which judgment the action answers.

| What happened | What to consider |
|---|---|
| A reader found a defect | Rework with the specific finding, or ask another reader if the finding is disputed. |
| A worker reported failure | Read the report. Repair the brief or the work; distinguish a provider failure from a code defect. |
| A dependency was dropped | Decide whether the dependent task still makes sense. Waiving a need is a decision about the missing change. |
| A card is stalled | Read its held reason before assigning a repair or another read. |
| No reader can take a card | Check reader eligibility, presence, and capacity. |
| A stream stopped on a conflict | Return the affected card for rework, then resume the stream after the conflict is addressed. |
| A sentinel was reached | Check its agreed release condition. Release it only within the coordinator's authority. |
| A provider needs payment | Leave that decision with the account owner and continue independent work where possible. |

Do not repeat the same repair indefinitely. Keep the failed attempts and the
reason for the next change visible on the card. If a decision exceeds the
agreed authority, leave it open with the evidence and the question for the
human. Other ready streams can continue.

### Optional automatic answers

`nova-sprint answer` can use `nova-decide` to propose and apply some routine
judgments. It complements the AI coordinator; it does not replace the plan
or the coordinator's responsibility for decisions outside that policy.

Start with the recorded proposals:

```sh
nova-secrets exec --only JEV_API_KEY -- nova-sprint answer --dry-run
```

The configured `decide_judgment_bar`, or `--bar` for a run, controls which
proposals may be applied. With no bar, decisions are recorded and none are
applied. Review that evidence before enabling a repeating answer loop.
Drops and payment decisions are not automatically applied. See
[Answering the routine judgments](CLI.md#answering-the-routine-judgments)
for the supported cases, records, and retry rules.

## 3. Loading work

Turn the goal into cards with a repository, starting point, allowed paths,
checks, and finish condition. Use the card generator and lint before loading
a batch. A card's filename becomes its ID:

```sh
nova-card template
nova-card lint --card <file>
nova-sprint add --stream <s> --brief-dir <dir>
```

You can also add individual brief files. The sprint checks each brief before
admitting it. Keep ownership disjoint where work can run in parallel, and
use `--needs <a,b>` when one task must wait for other cards to land. A stream
groups work and its integration order; it does not automatically discover
all shared-file conflicts.

A sentinel divides work into waves:

```sh
nova-sprint add --stream <s> --sentinel <s>-wave2
nova-sprint release <sentinel> --reason '<evidence that its gate passed>'
```

Place cards with `--before` or `--after` when their position matters. Load
future work behind its checkpoint so the coordinator can inspect and release
the next wave without constructing it from memory. The
[sentinel rules](SPEC-SPRINT.md#16-sentinel-cards) define when release is allowed.

Keep enough useful, ready work for the team's capacity. Repairs that unblock
other cards usually deserve an early place in their stream. If a machinery
fix is needed before a card can run again, keep the card ID, brief, reason,
and required fix in the handover. Do not discard that context and hope the
next coordinator remembers it.

## 4. The fleet

**Width** is the number of tasks a member can run at once. The configured
machine row is the source of truth; a later sync reapplies it. Read the load
and queue before changing capacity, and keep the change inside the bounds
the human agreed to.

```sh
nova-config machine set <m> --width <n> --as <coordinator> --dry-run
nova-config machine set <m> --width <n> --as <coordinator>
nova-config apply --kind machine --as <coordinator>
nova-sprint fleet sync --check
nova-sprint fleet sync
```

Run the local configuration commands under the
[seat wrapper](SPRINT-COORDINATOR-SEAT.md). A member buffers ready work as
well as running work, so its total assigned cards can exceed its width.
Readers need capacity too; watch the review queue alongside the work queue.

High sustained CPU load, slow staging, missed heartbeats, and overdue work
are reasons to inspect the machine. Increase concurrency only when there is
room. `fleet down <m>` holds a member and redistributes unfinished work;
`fleet up <m>` releases the hold. Those operations affect running assignments,
so use the card history to understand what was moved.

Friends have their own presence and queues. A daemon running is not, by
itself, evidence that the friend is actively handling a task. Check the
session, assignments, and recent progress when a friend looks stuck.

## 5. Routes

A route names the provider, model, and harness used for a kind of work.
`nova-sprint routes` shows attempts and outcomes so you can investigate a
slow or unreliable route. Configuration lives in Nova Tools route and tier
rows.

Cards begin according to the sprint's tier policy and can escalate up to
their ceiling. A provider outage or empty balance is different from a worker
returning bad code. Read the failure category and the route's recent results
before changing the configuration. The [fleet contract](SPEC-SPRINT.md#5-the-fleet)
spells out escalation, bounded retries, and temporary route rests.

Record why a route was changed, the measured evidence, who authorised the
change, and what would bring it back. The next coordinator should not have
to guess why a useful model is disabled.

## 6. Landing

The worker's completion report starts review. Required passing reads belong
to that card's current head: one for a flash card, two distinct readers for
a pro card. Consult the [reader contract](SPEC-SPRINT.md#6-the-readers) for
other tiers and eligibility. The lander then checks and integrates approved
work. `--land` lets the server run that process without a human invoking each
merge.

For a real landing, verify the reviewed commit, required checks, and target
branch. A simulation's `merge` command is not evidence that Git changed.
If the stream uses an intermediate sprint branch, also verify its promotion
to the project's development branch before calling that integration complete.

Conflicts and failing checks stop the affected stream for a decision. Read
the evidence, send a repair with the right base, and resume after the problem
is addressed. Avoid changing unrelated code inside an integration merge;
that code needs its own review.

Projects choose their own branch protections and batch process. Keep reviewed
commit identities intact and follow the repository's contribution rules.
The [landing contract](SPEC-SPRINT.md#7-merging) describes the sprint's part
of the process.

## 7. The install after a landing

A landed change is on the development branch. It is not yet an installed
binary, a deployed service, or a release to users.

If installation is in scope, build from the verified landed revision and
use the project's deployment procedure. The Nova Tools
[fleet guide](https://github.com/mas-bandwidth/nova-tools/blob/dev/docs/FLEET.md)
covers its configuration and supervised services. Check the installed
version on each machine and the service's health after updating it; a push
or a successful local build proves neither.

Record the installed revision, pending migrations or restarts, and any
machines left on an older build. Leave those facts in the handover even if
every code card has landed.

## 8. The hourly habits

For an overnight run, the coordinator should keep checking the parts that
let the team continue:

- **Useful work:** ready cards, dependency holds, and the next checkpoint.
- **Review capacity:** reads waiting, findings to address, and eligible readers.
- **Team health:** current sessions, queues, progress, and overdue assignments.
- **Machine health:** load, free disk, staging failures, and service logs.
- **The server:** recent ticks, responsive read commands, and open fault reports.
- **The plan:** decisions waiting for the human and independent work that can continue.

Use current evidence. A dashboard can retain its last good snapshot after a
read failure; inspect the service log when progress looks suspiciously stale.
An unresponsive server is a service problem to diagnose, not a reason to
edit its store by hand or start another writer.

## 9. Handing the seat over

Leave one dated handover with pointers to the sprint records. Include:

- Open decisions, their IDs, the evidence, and whose answer is needed.
- Held checkpoints, their release conditions, and the agreed order.
- Work and reviews in flight, with branch names and exact commit IDs.
- Capacity and route changes, why they were made, and when to revisit them.
- Repairs waiting for a machinery fix, plus the brief and condition for retrying.
- What is landed, what is installed, and which deployment steps remain.

The next coordinator reads this before taking over. Keep identity and holder
separate: the store's coordinator actor was set by `init`; routing work to
a different friend is a separate configuration change. Follow
[Handing over the seat](SPEC-SPRINT.md#handing-over-the-seat) for the exact
rules, and avoid overlapping coordinators making decisions from different
snapshots.

A good handover lets another AI continue the work, or lets the human return
in the morning and understand it in a few minutes.
