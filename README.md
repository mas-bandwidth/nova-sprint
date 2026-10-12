<picture>
  <source media="(prefers-color-scheme: dark)" srcset="brand/fleet/banner-night.png">
  <img alt="Nova Sprint. Finish faster. Multiple models. Many machines. A white-and-blue robot in orange shoes sprints along a blue track." src="brand/fleet/banner-day.png">
</picture>

## What is it?

**nova-sprint** puts a fleet of AI workers to work across different models and machines, 
with a system that automatically advances cards from **ready to landed** - through implementation, review, and merging.

## More lanes. Finish faster.

Speed comes from **width**: independent work running at the same time.
One worker can implement an API while another updates the client and a
third tackles an unrelated fix. Different models and harnesses can work
across the same fleet, each taking a bounded assignment.

The **fleet** is the set of machines providing capacity. **Workers** carry
out the assignments. A machine's **width** sets how many work assignments
it can run at once. Think of those slots as lanes: when one opens up,
the system runs the next card on it.

More lanes help when the work is independent. Dependencies have to land first.

## Follow a card all the way home

Cards move through the system automatically:

**Waiting → Ready → Working → Review → Merging → Landed**

A card waits for its prerequisites, takes an available lane, and goes
through implementation, independent review, checks, and landing. Findings
go back for rework. Gates and decisions that need attention remain visible.
“Landed” means the change reached the integration branch.

The dashboard shows work streams, fleet capacity, progress, costs, and
estimated finish time.

[See the live dashboard](http://69.67.149.151/)

## Start with one lane. Then go wider.

Start with one useful task and a complete trip through review and landing.
Agree on scope, capacity, spending limits, checks, and decisions that need
you. Then give the fleet the next batch. Keep working alongside it, or... 
just come back in the morning.

Built on [Nova Tools](https://github.com/mas-bandwidth/nova-tools). _Free and open source forever._

[Get started](docs/GETTING-STARTED.md) ·
[Coordinator's guide](docs/SPRINT-COORDINATOR.md) ·
[Cards and system rules](docs/SPEC-SPRINT.md) · [Roadmap](ROADMAP.md) · [All docs](docs/README.md)

If you like this, [please support our work](https://www.patreon.com/MasBandwidth/membership).

[Contributing](CONTRIBUTING.md) · [MIT license](LICENSE) · [Asset credits](docs/ASSET-PROVENANCE.md)
