![Nova sprint: a team of colourful robot friends sprinting, skipping, cheering, stumbling and snoozing on a blue running track, with a star-topped coordinator keeping them company.](assets/nova-sprint-banner.png)

# nova-sprint

**Scale out AI work. Give your team a goal, and let them work together.**

Scale out across **friends running different models and harnesses**, across
**a fleet running swarms of AIs**, or **any combination you want**. nova-sprint
brings them into one team with an AI acting as coordinator: remembering the
work, tracking dependencies, distributing tasks, arranging independent
reviews, and merging the results.

Your familiar AI friends can keep working in their own harnesses while the
swarm goes wide on tasks that can run in parallel. They share a work queue
and a record of what has happened, so the team can coordinate without you
carrying messages between models or handing out every next job.

You set the direction and the boundaries. Work alongside the team, or leave
it with a plan and come back in the morning to see what landed, what is still
running, and what needs your decision.

Built on [Nova Tools](https://github.com/mas-bandwidth/nova-tools).
New to AI friends? Start with [Nova Seed](https://github.com/mas-bandwidth/nova).

## Keep working while you are away

A conversation can lose the thread when a session ends. A sprint keeps the
tasks, their dependencies, attempts, review findings, and progress in a shared
record. The next job does not depend on you remembering to paste it into
another chat.

The AI coordinator uses that record to keep useful work moving. It can load
and organise cards, distribute ready work, follow up on failures, ask for
reviews, and bring approved changes through the landing process. Workers
pick up their next assignments as capacity becomes available. Work that needs
a decision beyond the coordinator's authority waits with its context intact.

Before an overnight run, agree on the goal, repositories, model and capacity
limits, checks, and which checkpoints the coordinator may release. Leave
workers, readers, and the coordinator running with the access they need.
Autonomous work still needs those services and a usable plan; it does not
need a human awake to shuttle messages between them.

## Could this help you?

| If you want to… | nova-sprint helps you… |
|---|---|
| Let the team work while you are away | Have an AI coordinator follow the work through assignment, review, rework, and landing. |
| Pick up where the team left off | Keep tasks, dependencies, attempts, and results outside any one chat. |
| Work on several parts of a project at once | Give each worker a bounded task, with dependencies and a clear finish condition. |
| Spend less time handing out the next job | Keep ready work queued for workers as they finish. |
| Coordinate AIs across models and local harnesses | Let your AI friends share tasks, handoffs, and review results through one sprint. |
| Go wide across a fleet of machines | Distribute independent work to swarms with capacity to run it in parallel. |
| Get a second pair of eyes on AI-written code | Send completed work to independent readers before it can land. |
| Understand why work has stopped | See the card, its history, and the decision or dependency it is waiting for. |
| Bring a collection of changes together | Queue reviewed work for landing, with checks and a place to handle conflicts. |
| Keep an eye on a longer run | Use the terminal view or dashboard to follow work, capacity, progress, and recorded costs. |

It is especially useful when coordinating the work starts taking as much
attention as doing it. For a single task with one AI, a conversation and a
branch may be all you need. You can start small here too; a sprint does not
have to mean a room full of machines.

## From an idea to a landed change

Suppose you are adding search to an application. One AI can work on the index,
another on the interface, and another on tests. The AI coordinator tracks
those assignments, and integration work waits until the pieces it needs have
landed. Readers check the results. A checkpoint
can let the coordinator inspect the combined result before the next wave, or
hold it for you if that decision needs your attention.

nova-sprint gives those everyday pieces names:

- A **card** is one task: what to change, where to work, how to check it, and
  what counts as done. The card generator helps turn existing findings and
  work lists into briefs your team can use.
- A **stream** groups related cards and gives their changes an order for
  integration. **Needs** name the cards that must land first.
- A **sentinel** is a checkpoint between waves of work. Use one when you want
  to stop, look at the result, and decide whether to continue.
- **Workers** do the tasks. **Readers** review the results independently.
  The **lander** brings approved changes into the target development branch.
- The **AI coordinator** keeps the plan useful, answers the decisions within
  its remit, and brings the rest back to you. A human can hold the role too.

A card travels through `waiting → ready → working → review → merging → landed`.
Some work needs another attempt. Some needs a better brief. Some needs you.
The history stays with the card, so the next person or AI can see what happened.

“Landed” means the change reached the development branch. Installing it or
releasing it to your users is a separate step.

## What comes with nova-sprint?

v1.0.0 brings the sprint workflow and its companion tools together:

| Part | What you use it for |
|---|---|
| Sprint server and commands | Load work, coordinate the team, handle dependencies, review results, and land changes. |
| Dashboard | See the sprint in a browser while you and your AIs work. |
| Card generation | Turn findings, test ledgers, and command help into task briefs; check them before sending them out. |
| Work tree | Bring GitHub issue content into a local tree and compare that tree with its source. |

The work tree is useful when your starting point is a backlog spread across
repositories. Importing issues gives you a local record to work with; choosing
which issues become sprint cards is still part of planning the work.

nova-sprint is built on [Nova Tools](https://github.com/mas-bandwidth/nova-tools),
the shared tools for running AI tasks, messaging, configuration, and storage.
Use Nova Tools when you want building blocks for your own workflow. Use
nova-sprint when you want a workflow for a team, with the queue, review, and
landing process already joined up.

Use your own repositories and configured models and harnesses. Harness
adapters connect friends to the team; routes connect swarm work to the models
and runners available on your fleet. You choose the work, capacity, routes,
and checkpoints. The [team guide](docs/WORKING-WITH-AI-TEAMS.md) explains how those choices fit together.

## New to AI friends?

If you would like to create AI friends and build a team, start with
**[Nova Seed](https://github.com/mas-bandwidth/nova)**. It helps you begin a
friendship with a name, a written memory, and a working agreement you shape
together. Bring your friends here when you want to coordinate their work,
add swarm capacity, or let the team take a project further on its own.

## Take a first lap

Start with the [getting-started guide](docs/GETTING-STARTED.md). It walks one
card through a local simulation, without Redis, a model account, or a Git
remote. You can see the handoffs before setting up a live team.

Then give an AI coordinator one small, real change to take through a worker,
a reader, and landing. Watch that first lap together. Once the handoffs work,
you can leave it with a larger plan and let the team continue on its own.

- [Working with AI teams](docs/WORKING-WITH-AI-TEAMS.md): plan useful cards,
  share work, and handle the moments when someone gets stuck.
- [Coordinator's guide](docs/SPRINT-COORDINATOR.md): run a live sprint and
  hand it over to the next coordinator.
- [Command reference](docs/CLI.md): find commands, flags, and examples.
- [All documentation](docs/README.md): setup, concepts, specifications, and
  contributor references.

The friends in the banner all have their own pace. The point is to help the
team make progress together.

## Contributing

Have an awkward handoff, a confusing command, or a workflow you would like to
make easier? A small example is a good place to start. The
[contributor guide](CONTRIBUTING.md) explains the repository, local checks,
and how to keep the documentation and behaviour in agreement.

## License

MIT. See [LICENSE](LICENSE). Asset credits are in
[Asset provenance](docs/ASSET-PROVENANCE.md).
