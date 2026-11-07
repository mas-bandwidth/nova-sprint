# Setting up nova-sprint

`nova-sprint up` sets a sprint up and verifies it. It is idempotent: every run re-verifies from step 1, acts at the first step that does not verify, and stops at a step that needs an answer from the human, printing the question and the command that supplies the answer.

nova-sprint runs on a fleet built by nova-tools (Tailscale, Redis, PostgreSQL, Ansible, and the tools on every machine). Set the fleet up first with nova-tools (`nova-fleet check`, then `nova-fleet up`); nova-sprint does not repeat that work.

## Command

    nova-sprint up --as <coordinator> [--dash tailnet|public=<machine>[:<port>]] [--services <a,b>|none] [--key <service>] [--run-on <host>] [--version <v>]

- `--as <coordinator>`: the coordinator's bus name. It must already be on the bus (`NOVA_BUS_REDIS=<host:port> nova-bus hook --as <coordinator>` under the session's Monitor tool); `up` refuses otherwise, with that command as the fix.
- `--dash`: where the dashboard runs (step 5). The answer is stored; later runs need not repeat it.
- `--services`: the API-rate services to set up (step 6), comma separated, or `none`. Supported: `deepseek`, `openrouter`.
- `--key <service>`: names a service; refused unless it is one of the selected services (`key service is not selected`).
- `--run-on <host>`: the machine the run service runs on (step 4); defaults to this machine.
- `--version <v>`: adopt a build (step 2; see "Upgrading").

`check`, `help` and `version` run anywhere. `up` hooks you in as coordinator once the store exists (after step 2).

## Steps

Each step prints `UP <n>/8 <name>: ...: VERIFIED` when it holds.

1. **nova-tools.** `nova-fleet check` is green and the installed nova-tools is at or above the pin this build carries. Fix: `nova-fleet check`; below the pin, `nova-fleet install --tag <pin> <machine>`.
2. **Store.** The PostgreSQL role and database exist and the schema is at this build's version. The role's password is proven on every run by a SCRAM login (or, where the database trusts the connection, by its stored verifier); when it is not the sprint's credential, `up` sets it.
3. **Coordinator.** `--as` names the coordinator and is hooked in. `up` records the name; it is the only name that may use coordinator verbs.
4. **Run.** The `sprint-run` service (`nova-sprint run --listen <address>:8080`) is installed through nova-fleet on the run machine and running: the policy is active (the built-in default is stored and activated if none is), the chooser lock is held, and the sprint name is hooked on the bus. The service opens its secrets from nova-fleet's secrets store on the run machine, as its own seat.
5. **Dashboard.** One dashboard, in one place:
   - `--dash tailnet` (default choice offered): the run service serves the board on the tailnet at `http://<run-machine>:8080`.
   - `--dash public=<machine>[:<port>]`: a pull-only `sprint-dashboard` service (`nova-sprint dash --pull <private url> --listen :8080`) on a machine that has a public IP set in nova-config (`nova-config machine set <machine> --public <ip> --actor <you> --reason <why>`). It holds no secret and caches the board, so traffic never reaches the store. `:<port>` is the port a front serves on (for example 80); the service itself keeps listening on 8080. The public machine cannot be the run machine.
   - Without an answer `up` stops: `UP 5/8 dashboard: not chosen`, with both forms.
   - Verification fetches the board and checks it. If the board is unreachable the refusal names the port to open on the serving machine (`open 8080/tcp on <machine> for the dashboard`).
6. **Services.** Without an answer `up` stops: `UP 6/8 services: not chosen`, with the command `nova-sprint up --services <names|none>`. For each chosen service with no saved key, `up` runs a hidden prompt on the terminal where the human pastes the API key once; `up` stores it in nova-secrets and proves it with one model-list request (`https://api.deepseek.com/models`, `https://openrouter.ai/api/v1/models`). A failure prints the endpoint and the status. `up` then makes the seats, grants and service rows: the human types nothing else for that service. `--services none` is valid.
7. **Workers.** The sprint needs its commit identity first; without one `up` stops with `UP 7/8 workers: the sprint has no commit identity` and the command to run:

        nova-sprint identity --name "<git user.name>" --email "<git user.email>" --reason "<why>"

   The identity is the forge account on the repositories' contributor-agreement allowlist that every commit is authored and committed as; ask the human which. Then every machine with the bench role, except the one the sprint runs on, gets a worker: its seat is granted the sprint's database credential, and nova-config gets one service record `sprint-worker-<machine>` running `nova-sprint worker --as <machine>` under that seat (with the sprint and bus credentials and the provider keys the seat holds). nova-fleet applies them in one play. The step verifies that each worker bench is at this version, its harness probed with the policy's models and the seat's keys, hooked on the bus and up in the store. Otherwise it names each machine that is not and the last line of its worker log.
8. **First deal.** `up` checks that every worker row can be dealt to: on the bus, up, and with its core count in nova-config. A bench without cores is held at width 0 and named with `nova-config machine set <machine> --cores <n>`. A new sprint is stopped, so `up` deals nothing.

The last line when all eight verify:

    UP DONE run on <host>; dashboard <url>; <n> workers up; coordinator <name>

Then the coordinator runs `nova-sprint start --as <coordinator> --reason "<why>"` (coordinator playbook, section 1). `nova-sprint check` re-verifies the whole setup and the health of the running sprint at any time.

## Notes

- Keys: one API key per service, pasted once at the hidden prompt; it goes into nova-secrets and never into a file, log or command line. All later steps are automatic.
- A bench can also serve the public dashboard: the public service is pull-only and holds no secret.
- A second `up` while one runs waits on the setup lock, held on the database.
- nova-sprint and nova-tools release separately. The nova-sprint binary carries the minimum nova-tools version it needs; `nova-sprint check` reads the installed tools' versions, warns above the pin and refuses below it, naming the `nova-fleet install` that fixes it.

## Upgrading

`nova-sprint up --as <coordinator> --version <v>` installs the build beside the old, runs one shadow tick against the live store, swaps, watches three ticks, and swaps back on an overrun, crash or refused tick, saying why. Never copy a binary by hand.

## Backup and restore

    nova-sprint backup [--out <dir>]          BACKUP <file> epoch=<e> seq=<n> verified
    nova-sprint restore <file> --as <you>     loads a verified dump into an empty store; the same dump again only hooks you in

`backup` needs a `pg_dump` no older than the server.
