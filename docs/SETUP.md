# Setting up nova-sprint: the contract

Glenn, 2026-11-06. This is what the setup must be; layer 6 makes it exact and
this page becomes the walkthrough an AI runs.

## 1. nova-tools first

nova-sprint is built on nova-tools. The AI setting it up is told so at the
start, and sets up the fleet first with nova-tools: Tailscale, Redis,
PostgreSQL, Ansible, the tools on every machine (`nova-fleet check`, the
human's one paste of the Tailscale API token, `nova-fleet up`). Nothing in
nova-sprint works without that fleet, and nothing in nova-sprint repeats it.

## 2. Then: where does the dashboard run?

Once the fleet is up, the next question to the human is where they want the
dashboard. There is one dashboard, in one place:

- private, on the tailnet (the default), or
- public, if the human has a machine with a public IP and wants it.

A public dashboard must be robust: it caches, and it is not taken down by a
lot of traffic.

## 3. Then: the API-rate workers

After the dashboard, configuration is about the API-rate services the sprint
will run work on: DeepSeek, Mercury, OpenCode, OpenRouter, and the like. The
AI offers to set these up by default and the human chooses which ones they
want.

The rule, the same as the Tailscale key: **one API key per service, pasted
once by the human at a hidden prompt the AI runs for them, and everything
past that point is automatic.** The key goes into nova-secrets; the AI makes
the seats, the grants, the worker rows and the service rows. The human never
types a second thing for that service.

## 4. The setup is part of the tool, and it is dogfooded

Glenn, 2026-11-06: "The whole point of nova-sprint is to deliver this sprint
system on top of nova tools, help the human configure it, and setup the
dashboard. Just like the whole fleet and tailscale stuff and github ci
runners, it's useless if we don't ship this setup stuff as part of the tool,
and we must dogfood this setup to make sure it's simple and repeatable."

So: the setup above is a verb of the tool (`nova-sprint up`, running the
plays through nova-fleet: the store schema and role, the dashboard service
where the human chose, the worker services, the API-rate seats), not a page
of instructions. The gate before v2 is called done: an AI with no memory of
this project takes a fresh fleet (VMs are enough) from zero to a running
sprint with a dashboard, using only docs/README.md and the tool's own help,
and then does it again. Simple and repeatable, or not done.
