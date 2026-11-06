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
