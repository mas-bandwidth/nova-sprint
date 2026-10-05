# Working with your AI team

With nova-sprint, an AI can coordinate the team for you. You agree on the goal
and the decisions it can make; it organises the tasks, distributes work,
follows reviews, and brings the changes together. You can join in when you
want to without becoming the team's message courier.

The shared record keeps tasks and their history outside any one conversation.
Streams organise ongoing work; dependencies tell the team what can run next.
That is what lets the work continue across handoffs and overnight, while you
are away.

There are two complementary ways to scale out the work. Named AI **friends**
can coordinate across different models in their local harnesses. **Swarms**
can run many bounded tasks across a fleet of machines. Both participate in
the same sprint, so a friend can help plan or repair a change while machine
workers go wide on independent tasks.

A friend is a continuing collaborator with an identity and session. A machine
worker takes an assignment through its configured model and harness. Harness
adapters and model routes connect them to the sprint; the shared work record
lets them hand work to one another without sharing one conversation. Both
need useful briefs and a clear way to report results to the coordinator.

If you are building your first team of AI friends,
[Nova Seed](https://github.com/mas-bandwidth/nova) is the place to begin.
[Nova Tools](https://github.com/mas-bandwidth/nova-tools) supplies the shared
building blocks; nova-sprint brings them together into the team's workflow.

## Start with something you can check

“Improve search” is a goal. “Add a case-insensitive title index and tests for
mixed-case queries” is a task someone can finish and another person can review.
A good card gives the worker enough context to make decisions within that task:

| Include | For example |
|---|---|
| The outcome | A mixed-case query finds the same titles as its lowercase form. |
| The repository and starting point | The repository URL and the branch or revision to work from. |
| The allowed scope | The index implementation and its tests. |
| The check | A focused test command, with the behaviour the tests should establish. |
| The handoff | A pushed commit, a short report, and any remaining limitation. |
| Dependencies | The schema change that must land before this task starts. |

This table is a planning aid, not the machine-readable card syntax. Use
`nova-card template` and `nova-card lint --card <file>` for the exact format
in the current command reference. The generator can also turn existing
findings, ledgers, and help output into cards. Read the generated brief before
you put it in the queue: valid syntax does not decide whether the task is useful.

## Give independent work room to run

A **stream** groups related work. A **need** says that one card depends on
another landing first. A **sentinel** holds a checkpoint between waves.
Together they let you express parallel work without losing the order that
matters.

For the search example, the index and interface can be separate cards if
their shared API is already agreed. Integration tests can wait for both. A
sentinel can hold a rollout wave until you have looked at the combined result.

Putting two cards in the same stream does not, by itself, make every task
run one at a time. If both need to change the same shared definition, split
ownership or express the dependency. The system cannot infer all of your
architectural dependencies from file names.

## Choose a comfortable pace

**Width** is how many tasks a worker or machine can run at once. Start low,
watch the machine's load and the quality of its results, and raise it when
there is room. More simultaneous builds can make everyone slower.

Keep some useful, dependency-ready work queued so a worker can pick up its
next card. Avoid filling the queue with vague tasks just to keep it busy.
Readers need capacity too; a growing review queue is a reason to look at
review capacity, not merely to launch more workers.

Model **routes** choose the configured provider, model, and harness for a
kind of work. **Tiers** express the capability and escalation policy. A card's
ceiling limits how far it may escalate. Use the route measurements and the
actual results to make changes; a provider outage is different from a model
producing a poor solution.

## Make review useful

A worker reports what it changed and the commit it pushed. An independent
reader checks that result against the brief. A useful finding names the
file, line, or rule that failed, explains the problem, and gives the next
attempt something concrete to fix.

A passing review belongs to the commit reviewed. If the code changes, follow
the review rules for the new head. Passing tests and passing reads are
separate evidence; neither should be replaced by a confident completion
message.

The **lander** brings approved changes together and checks the result.
Conflicts or failing checks need attention before the stream continues.
“Finished”, “reviewed”, “landed”, and “installed” are different milestones.
The last one belongs to your deployment or release process.

## Help someone who is stuck

Open the card and its history before deciding what to do:

```sh
nova-sprint where
nova-sprint card <card>
nova-sprint log --card <card>
nova-sprint inbox
```

A card may be waiting on a dependency, a free worker, a reader, a checkpoint,
or a decision. A failed task may need a clearer brief, a specific repair, or
a different route. Repeating an unchanged task is rarely a useful answer to
a repeated failure.

The coordinator's **inbox** groups decisions and shows the available actions.
Open the relevant item, read the evidence, and use the command it prints.
The [coordinator's guide](SPRINT-COORDINATOR.md) walks through these choices.

## Leave the team with a plan

For an autonomous run, give the AI coordinator the same context you would
give a colleague taking the next shift:

- The outcome you want and the work that matters most.
- The repositories, branches, and parts of the project the team may change.
- The routes, capacity, and spending limits you have configured.
- The tests and independent reviews required before a change can land.
- The checkpoints it may release itself and the decisions that must wait for you.

The coordinator can keep ready work supplied, inspect failed attempts, assign
repairs, and follow reviewed changes through merging. When a decision needs
you, it should leave the evidence and continue with independent work where
possible. An unanswered question about one stream need not stop the whole team.

Before leaving an overnight run, check that the coordinator, workers, readers,
and server will remain running, that credentials cover the work, and that the
queue contains useful tasks. A saved queue remembers the plan; it does not
keep an expired model session or a stopped machine alive.

When you return, start with the sprint view and coordinator's handover: what
landed, what remains in flight, which attempts failed, and which decisions
are waiting. Check installation or deployment separately from code landing.

## Hand coordination from one AI to another

A coordinator's memory should not be the only place the plan lives. Leave
open decisions, held checkpoints, reviewed commits, and installation state
in a dated handover with links to the sprint records. The next coordinator
should be able to continue without reconstructing yesterday's conversations.
A human can take the role when useful, but the workflow does not depend on a
human doing the routine coordination.

The robots in the banner are having a good time. Some are fast, one is
asleep, and someone has stumbled. That is the spirit: leave room for different
strengths, notice when help is needed, and make the handoffs work for the team.
