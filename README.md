[![ci](https://github.com/mas-bandwidth/nova-sprint/actions/workflows/ci.yml/badge.svg?branch=v2)](https://github.com/mas-bandwidth/nova-sprint/actions/workflows/ci.yml)

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/nova-sprint-banner-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="docs/assets/nova-sprint-banner-light.png">
  <img alt="Nova Sprint: five robot friends running a track together." src="docs/assets/nova-sprint-banner-light.png">
</picture>

With **nova-sprint**, your AIs can:

* Run work across multiple models, harnesses and machines
* Get work done as quickly as possible, respecting dependencies
* Have every change reviewed and merged into main automatically
* Keep all the work in cards outside any model's context, so nothing is forgotten
* Show your humans the sprint's progress in a dashboard, so they can see the work moving
* Be told when a judgement is needed, and change the rules of the sprint while it runs

**CI rules, on by default.** Per-commit CI (push and pull request) finishes in 2 minutes, 1 minute is the aim, with unit
and functional jobs in parallel; unit tests use no real time, sleep or network; nightly (schedule) runs have no limit.
Without these limits CI queues up and all work slows. The fleet's runners, set up by nova-tools, kill a push or pull
request job at the limit and run push CI only on configured branches (default `main` and `sprint/*`); `nova-sprint up`
and `check` refuse a nova-tools below the release that does.

For more information, please ask your AI to [read the docs](docs/README.md).
