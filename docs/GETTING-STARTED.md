# Get started with nova-sprint

Start with one AI friend and one small task. You can add more friends,
different models and harnesses, or a fleet of swarm workers as your team grows.

## Have a look

[Open the live dashboard](http://69.67.149.151/) to see a real sprint.
Follow the streams, friends, fleet, costs, and estimated finish time. That is
the view you will have of your own team.

## Install Nova Tools first, then nova-sprint

nova-sprint runs on top of [Nova Tools](https://github.com/mas-bandwidth/nova-tools).
Install the latest Nova Tools release first: **v1.1.0 for nova-sprint v1.0.0**.
It supplies the friends (`nova-friend`), message bus (`nova-bus`), workers
(`nova-swarm`) and configuration store commands (`nova-config`). Installing
the nova-sprint command alone does not install those tools.

With Go 1.26.6 or newer, run these commands in order. The first installs the
Nova Tools command suite; the second installs the separate nova-sprint release:

```sh
go install github.com/mas-bandwidth/nova-tools/cmd/...@v1.1.0
go install github.com/mas-bandwidth/nova-sprint/cmd/nova-sprint@v1.0.0
nova-sprint help
```

Make sure your Go binary directory is on `PATH`, ahead of any older Nova
installation. These commands download and build the named releases; they
require the published tags. If either tag is not available yet, wait for the
[Nova Tools](https://github.com/mas-bandwidth/nova-tools/releases) and
[nova-sprint](https://github.com/mas-bandwidth/nova-sprint/releases) releases.
Check an existing installation with `nova-friend version`, `nova-bus version`,
`nova-swarm version`, `nova-config version` and `nova-sprint version`.

## Take a first lap together

Ask your AI friend:

> Help me get started with nova-sprint (https://github.com/mas-bandwidth/nova-sprint).
> Read the getting-started guide and walk through the local first lap with me.
> Then help me set up a small live sprint with you as coordinator: one useful task,
> a worker, an independent review, and a checked landing. Let's agree on the repository,
> model access, capacity, and what you can decide before adding more work.

The **[local first lap](FIRST-LAP.md)** needs no Redis, model account, or Git
remote. It shows the handoffs without spending model tokens or changing code.
A live team also needs a sprint server, Redis for shared runtime state and
communications, PostgreSQL for durable fleet configuration through
`nova-config`, configured workers and reviewers, and model credentials.
Use Tailscale when machines communicate across networks, and Ansible to
provision and maintain the fleet consistently. The
[Nova Tools README](https://github.com/mas-bandwidth/nova-tools#readme)
explains the tool dependencies. Installing binaries is the
first step; the **[coordinator's guide](SPRINT-COORDINATOR.md)** covers the
connections and checks for a live team.

Once that first real task lands, give your coordinator the next few. Let
independent work run in parallel, then leave it with a plan to continue while
you are away.

Need an AI friend first? **[Begin with Nova Seed](https://github.com/mas-bandwidth/nova)**.
nova-sprint is free and open source; your chosen model providers and machines
have their own costs.
