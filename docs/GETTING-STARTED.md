# Get started with nova-sprint

Start with one AI friend and one small task. You can add more friends,
different models and harnesses, or a fleet of swarm workers as your team grows.

## Have a look

[Open the live dashboard](http://69.67.149.151/) to see a real sprint.
Follow the streams, friends, fleet, costs, and estimated finish time. That is
the view you will have of your own team.

## Get the commands

nova-sprint runs on [Nova Tools](https://github.com/mas-bandwidth/nova-tools):
nova-friend, nova-bus and nova-config. Install those first, at the release
named in [`NOVA-TOOLS-VERSION`](../NOVA-TOOLS-VERSION) (v1.1.0), then
nova-sprint. From a terminal with Git and Go 1.26.6 or newer:

```sh
go install github.com/mas-bandwidth/nova-tools/cmd/nova-friend@v1.1.0
go install github.com/mas-bandwidth/nova-tools/cmd/nova-bus@v1.1.0
go install github.com/mas-bandwidth/nova-tools/cmd/nova-config@v1.1.0
git clone https://github.com/mas-bandwidth/nova-sprint.git
cd nova-sprint
go install ./cmd/nova-sprint
nova-sprint help
```

Make sure your Go binary directory is on `PATH`. A newer Nova Tools release
works too. `nova-sprint seat check` refuses, naming the binary, the version
it found, the version required and the install command, while one of the
three is missing or older. Already have nova-sprint? Start with
`nova-sprint help`.

## Take a first lap together

Ask your AI friend:

> Help me get started with nova-sprint. Read the getting-started guide and
> walk through the local first lap with me. Then help me set up a small live
> sprint with you as coordinator: one useful task, a worker, an independent
> review, and a checked landing. Let's agree on the repository, model access,
> capacity, and what you can decide before adding more work.

The **[local first lap](FIRST-LAP.md)** needs no Redis, model account, or Git
remote. It shows the handoffs without spending model tokens or changing code.
A live team also needs the server, store, configured workers and readers,
and credentials; the **[coordinator's guide](SPRINT-COORDINATOR.md)** covers
those connections.

Once that first real task lands, give your coordinator the next few. Let
independent work run in parallel, then leave it with a plan to continue while
you are away.

Need an AI friend first? **[Begin with Nova Seed](https://github.com/mas-bandwidth/nova)**.
nova-sprint is free and open source; your chosen model providers and machines
have their own costs.
