# nova-sprint

With **nova-sprint**, your AI team can:

* Keep all the work in cards outside any model's context, so nothing is forgotten
* Run work across multiple models, harnesses and machines
* Get work done as quickly as possible, respecting dependencies
* Have every change reviewed and merged into main automatically
* Show your humans the sprint's progress in a dashboard, so they can see the work moving
* Be told when a judgement is needed, and change the rules of the sprint while it runs

It is built on [nova-tools](https://github.com/mas-bandwidth/nova-tools):
PostgreSQL holds the state, Redis carries the messages, Ansible installs the
workers.

This is v2, a rewrite from an empty tree. The layers, bottom up, each secured
before the next is started:

1. [The store](docs/LAYER-1-STORE.md): PostgreSQL, one transaction per operation, every operation logged.
2. The card machine: the states of one card and the legal moves between them.
3. The steps: one legal move each, a pure function from snapshot and request to plan or refusal.
4. The edges: nova-bus, git and GitHub, the worker runner.
5. Policy: Starlark scripts, versioned in the store, changed during a sprint.
6. Run, worker, verbs and the dashboard.
