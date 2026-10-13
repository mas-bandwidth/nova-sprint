# Get started with nova-sprint

Start with one AI friend and one small task. You can add more friends,
different models and harnesses, or a fleet of swarm workers as your team grows.

## Install the commands

nova-sprint runs alongside [Nova Tools](https://github.com/mas-bandwidth/nova-tools).
The sprint repository's [`NOVA-TOOLS-VERSION`](../NOVA-TOOLS-VERSION) file
names the minimum compatible Nova Tools release. Nova Tools supplies friends
(`nova-friend`), the message bus (`nova-bus`), workers (`nova-swarm`) and configuration commands
(`nova-config`). Installing nova-sprint alone does not install these tools.

With Go 1.26.6 or newer, run:

```sh
go install github.com/mas-bandwidth/nova-tools/cmd/nova-friend@latest
go install github.com/mas-bandwidth/nova-tools/cmd/nova-bus@latest
go install github.com/mas-bandwidth/nova-tools/cmd/nova-config@latest
go install github.com/mas-bandwidth/nova-tools/cmd/nova-swarm@latest
go install github.com/mas-bandwidth/nova-sprint/cmd/nova-sprint@latest
nova-sprint help
```

Make sure your Go binary directory is on `PATH`, ahead of any older Nova
installation. These commands download and build the named releases.
Check an existing installation with `nova-friend version`, `nova-bus version`,
`nova-swarm version`, `nova-config version` and `nova-sprint version`.

These commands install the latest published versions. If you pin versions,
install Nova Tools at the tag in `NOVA-TOOLS-VERSION` or newer. Once a
coordinator seat is configured, `nova-sprint seat check` checks the companion
tools against that minimum. `nova-sprint version` and each tool's `version`
command show what is on your `PATH`.

## Take a first lap together

Ask your AI friend:

> Help me get started with nova-sprint (https://github.com/mas-bandwidth/nova-sprint).
> Read the getting-started guide and the first-lap note with me. Check my
> installation, then help me set up a small live sprint with you as coordinator:
> one useful task, a worker, an independent review, and a checked landing.
> Let's agree on the repository, model access, capacity, and what you can decide
> before adding more work.

The **[first lap](FIRST-LAP.md)** starts with version and help checks. The
no-Redis card-flow recipe still shown by `nova-sprint help` is currently blocked
by the required coordinator hook, so use a configured sprint for a first card.
A live team also needs a sprint server, a Redis store for shared sprint state,
a separate bus Redis store for hook communication, PostgreSQL for durable fleet
configuration through `nova-config`, configured workers and reviewers, and
model credentials.
Use Tailscale when machines communicate across networks, and Ansible to
provision and maintain the fleet consistently. The
[Nova Tools README](https://github.com/mas-bandwidth/nova-tools#readme)
explains the tool dependencies. Installing binaries is the
first step; the **[coordinator's guide](SPRINT-COORDINATOR.md)** covers the
connections and checks for a live team. The
[environment reference](ENVIRONMENT.md) explains how direct store connections
and a running server fit together.

Once that first real task lands, give your coordinator the next few. Let
independent work run in parallel, then leave it with a plan to continue while
you are away.

Need an AI friend first? **[Begin with Nova Seed](https://github.com/mas-bandwidth/nova)**.
nova-sprint is free and open source; your chosen model providers and machines
have their own costs.
