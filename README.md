<picture>
  <source media="(prefers-color-scheme: dark)" srcset="brand/fleet/banner-night.png">
  <img alt="Nova Sprint. Finish faster. Multiple models. Many machines. A white-and-blue robot in orange shoes sprints along a blue track." src="brand/fleet/banner-day.png">
</picture>

**nova-sprint** puts a fleet of AI workers to work on your backlog. Different
models, many machines, and a machine that moves cards from **ready to landed**
automatically—through implementation, review, checks, and merging.

It supplements the way you already work with AIs. Keep your usual workflow
for exploring ideas and shaping the plan. When you have a batch of work to
hand off, give it clear finish conditions, open some lanes, and let the
workers get their little orange shoes on.

[Get started](docs/GETTING-STARTED.md) · [See the live dashboard](http://69.67.149.151/)

## More lanes. More work moving.

Speed comes from **width**: independent work running at the same time.
One worker can implement an API while another updates the client and a
third tackles an unrelated fix. Different models and harnesses can work
across the same fleet, each taking a bounded assignment.

The **fleet** is the set of machines providing capacity. **Workers** carry
out the assignments. A machine's **width** sets how many work assignments
it can run at once. Think of those slots as lanes: when one opens, the
machine can send the next ready card down it.

More lanes help when the work is independent. Dependencies still have to
land first. Even excellent shoes cannot outrun a missing API.

## We put the repeatable parts in a machine

Writing code is only part of finishing work. Someone still has to assign
the next task, arrange review, check the result, merge it, and release the
work waiting behind it. With enough tasks, the human becomes the scheduler.

**nova-sprint** stores the plan, dependencies, assignments, attempts,
reviews, and landing state outside any chat. Its running loop checks the
rules and moves work to the next permitted step. A landed dependency
releases waiting work. Free capacity gets another ready card. A finished
attempt goes to review.

The machine records transitions, recovers interrupted operations, and rejects
stale results that belong to an older assignment. Work has a state and a
history, even when a session loses the thread. Literally.

You and your AI coordinator shape the plan, handle findings, and decide
exceptions. Models supply judgment and skills; the machine keeps the
handoffs moving within the scope and gates you set.

## Give the fleet a plan

Cards and work streams form a **language for describing work**. You specify
the jobs, their relationships, and the gates between phases. The machine
executes that plan across the available workers.

| Part of the plan | What it expresses |
|---|---|
| **Card** | A bounded unit of work: its brief, starting revision, allowed scope, checks, and finish condition. Attempts and reviews stay attached to the task. |
| **Work stream** | A related line of work, such as Backend, App, or Release, with its own ordered cards and integration progress. Several streams can move at once. |
| **Dependency** | “This card needs that card to land first.” Dependencies can connect cards within a stream or across streams. |
| **Sentinel card** | A gate in a stream. Work behind it waits for release. Use it to separate waves or hold a phase for a decision. |

For example, the **Backend** stream defines an API. Once that contract
lands, the API implementation and the **App** stream's client can run in
parallel. Integration work in **Release** depends on both changes landing.
A sentinel can hold the next phase until its checks pass and the
coordinator releases it.

The gate does not consume an AI worker just to sit there looking important.

Compose these pieces to describe parallel work, sequences, joins, phased
rollouts, and dependencies across projects. The coordinator can add work
and revise the plan as findings arrive. Ordering and readiness live in the
workflow, rather than somewhere in a 200,000-token conversation.

## Follow a card all the way home

Ordinary task cards move through the dashboard automatically:

**Waiting → Ready → Working → Review → Merging → Landed**

A card waits for its prerequisites, takes an available lane, and goes
through implementation, independent review, checks, and landing. Findings
go back for rework. Gates and decisions that need attention remain visible.
“Landed” means the change reached the integration branch—not just that a
worker finished typing.

The dashboard shows work streams, fleet capacity, progress, costs, and
estimated finish time. Follow a card or watch the whole fleet make laps.

## Start with one lap. Then go wider.

Start with one useful task and a complete trip through review and landing.
Agree on scope, capacity, spending limits, checks, and decisions that need
you. Then give the fleet the next batch. Keep working alongside it, or
leave it with a plan to continue while you are away.

Built on [Nova Tools](https://github.com/mas-bandwidth/nova-tools).
**Free and open source.** Your chosen model providers and machines have
their own costs.

[Get started](docs/GETTING-STARTED.md) ·
[Coordinator's guide](docs/SPRINT-COORDINATOR.md) ·
[Cards and machine rules](docs/SPEC-SPRINT.md) · [Roadmap](ROADMAP.md) · [All docs](docs/README.md)

If you like this, [please support our work](https://www.patreon.com/MasBandwidth/membership).

[Contributing](CONTRIBUTING.md) · [MIT license](LICENSE) · [Asset credits](docs/ASSET-PROVENANCE.md)
