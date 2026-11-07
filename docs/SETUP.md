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

Glenn, 2026-11-06, the dogfood gate in his words: "we need to dogfood the new user
experience at least once, that means we set up our own sprint with it, and we
successfully run that sprint with the dashboard working and it works." So two
runs are owed before v2 is done: a cold AI's setup on a fresh fleet from the
docs alone (above), and our own real sprint set up with v2 through this same
walkthrough, run to completion with the dashboard live, by Rowan and the kin as
rows.

nova-sprint and nova-tools release separately (Glenn, 2026-11-06: "We no longer
pin nova-sprint and nova-tools together ... we'll need a pin to the nova-tools
version the nova-sprint release wants"). The binary carries the minimum
nova-tools version it needs; `nova-sprint check` reads the installed tools'
versions (`nova-bus -h`, `nova-fleet -h`, and the rest) and refuses below the
pin, naming the `nova-fleet up` that brings them up.

## 5. What `nova-sprint up` does past the store (steps 6 to 8)

Nothing in this section is typed by the human.

- **Services (6).** `--services none` is a valid answer. A chosen service's key is
  pasted once at a hidden prompt and proven by one model-list request.
- **Workers (7).** Every machine with the bench role, except the one the sprint runs on,
  gets a worker: its seat is granted the sprint's database credential, and nova-config
  gets one service record `sprint-worker-<machine>` running `nova-sprint worker --as
  <machine>` under that seat (the sprint and bus credentials and the provider keys the
  bench's seat holds). nova-fleet applies them all in one play. The step verifies when
  every worker is on the bus and up in the sprint; otherwise it names each machine that
  is not and the last line of its worker's log.
- **First deal (8).** A new sprint is stopped until the coordinator runs `nova-sprint
  start`, so up deals nothing. It checks every worker can be dealt to: on the bus, up,
  and with its core count in nova-config (a bench without one is held at width 0, named
  with the `nova-config machine set <machine> --cores <n>` that fixes it).
- **The public dashboard** may go on a bench: the public service is pull-only and holds
  no secret, and the bench still works cards.
- **The database role's password** is proven on every up by a SCRAM login, or, where
  the database trusts the connection without a password, by its stored verifier; when
  it is not the sprint's credential, up sets it.
