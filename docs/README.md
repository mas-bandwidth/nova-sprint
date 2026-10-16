# Find your way around nova-sprint

Welcome! If you are wondering whether nova-sprint would help your AI team,
start with the [project introduction](../README.md). You can check your
installation and find the path to a first card without learning the table
layout or reading a specification.

nova-sprint builds on [Nova Tools](https://github.com/mas-bandwidth/nova-tools).
The system coordinates workers across fleet machines, from ready cards
through review and landing.

## Start here

| Your next question | Where to go |
|---|---|
| What does nova-sprint do? | [Project introduction](../README.md) |
| Can I watch a sprint doing real work? | [Live dashboard](http://37.27.135.206) and [the README tour](../README.md#give-the-team-a-plan-go-have-a-life) |
| How do I get started? | [Getting started](GETTING-STARTED.md) |
| What can I try before connecting a team? | [First-lap note](FIRST-LAP.md) |
| How does the system keep work moving? | [Follow a card](../README.md#follow-a-card-from-waiting-to-landed) |
| What are cards, streams, dependencies, and sentinels? | [Workflow glossary](sprint/GLOSSARY.md), then [the sprint contract](SPEC-SPRINT.md) |
| What do I do as the coordinator? | [Coordinator's guide](SPRINT-COORDINATOR.md) |
| Which verbs, in what order, for a situation? | [Coordinator's playbook](COORDINATOR-PLAYBOOK.md) (`nova-sprint help playbook`) |
| How do I coordinate at low cost? | [dispatcher](DISPATCHER.md) |
| How do commands connect to a store or server? | [Environment reference](ENVIRONMENT.md) |
| What is the exact command? | [Command reference](CLI.md) |
| How do I open the dashboard? | [Dashboard guide and specification](SPEC-SPRINT-DASHBOARD.md#serving-and-publishing) |
| How do I contribute a change? | [Contributing](../CONTRIBUTING.md) |

Command references, implementation notes, and recorded results describe the
code or revision they name. Use the help shipped with your build for exact
command spelling. A planned feature in a design note is not an executable
example.

## When you need the details

| Reference | What it helps you understand |
|---|---|
| [Sprint contract](SPEC-SPRINT.md) | Cards, dependencies, scheduling, review, landing, and the rules each state change follows. |
| [Coordinator credentials](SPRINT-COORDINATOR-SEAT.md) | How locally executed configuration commands get the credentials they need. |
| [Environment variables](ENVIRONMENT.md) | How commands select a twin, direct store, server, and actor. |
| [Work tree format](SPEC-WORK-V1.md) | The GitHub issue mirror, the fields it preserves, and import/verify behaviour. |
| [Work language](SPEC-WORKLANG.md) | The small data grammar used to read tree files without evaluating them. |
| [Executable examples](TESTS.md) | Exact command transcripts checked by tests. |
| [Test harness](https://github.com/mas-bandwidth/nova-tools/blob/main/pkg/testkit/HARNESS.md) | Shared helpers for contributors writing command tests. |
| [State-machine models](../tla/README.md) | What the TLA+ models explore and how to interpret their results. |
| [Brand and voice](BRAND.md) | The robot friends, name, colours, and writing style. |
| [Asset provenance](ASSET-PROVENANCE.md) | Where the artwork and font came from. |
| [Recorded reviews](ratings/README.md) | Feedback from earlier versions, preserved with its original context. |

The specs keep precise rules, owner quotations, and examples that tests read.
Those details are there when you need them. The guides explain the same
workflow from the point of view of the person using it.
