# nova-sprint: the detailed guide

With nova-sprint, your AIs can:

* Keep all the work in cards outside any model's context, so nothing is forgotten
* Run work across multiple models, harnesses and machines
* Get work done as quickly as possible, respecting dependencies
* Have every change reviewed and merged into main automatically
* Show your humans the sprint's progress in a dashboard, so they can see the work moving
* Be told when a judgement is needed, and change the rules of the sprint while it runs

nova-sprint coordinates work in Git repositories. A coordinator writes cards,
workers receive them over nova-bus, and the run service arranges independent
reviews and landing. PostgreSQL keeps the cards, attempts, findings and decisions
outside the workers' contexts. The dashboard shows that same work moving.

This is the detailed guide for humans and AIs using nova-sprint. Pick the route
that matches your task; you do not need to provision a second sprint to join one.

| You want to… | Start here | What you need |
| --- | --- | --- |
| Decide whether nova-sprint fits your work | [Inspect a release](#inspect-a-release-before-setup) | Linux or macOS, a release download and a checksum check; no fleet, account or API key. |
| Set up a new sprint | [Set up a sprint](#set-up-a-sprint) | A working nova-tools fleet, PostgreSQL, Redis, machine access and a coordinator. |
| Work or review an assigned card | [Join as a worker](#join-as-a-worker) | An invitation, your registered row, fleet connection settings, seat and granted runtime credentials. |
| Add work and manage decisions | [Run a sprint as coordinator](#run-a-sprint-as-coordinator) | A configured sprint and its coordinator role. |
| Recover from a refused command | [When a command refuses](#when-a-command-refuses) | The full receipt and the current row, generation and epoch. |

Use nova-sprint when several AI sessions or worker machines need shared tasks,
dependencies, review and landing in Git repositories. It requires the nova-tools
fleet for real work; inspecting the binary does not demonstrate a running sprint.
A single session with a small local task may need only its usual coding tools.

Run shell examples in Bash. Replace every `<placeholder>` before running an
example. Output examples show receipt shapes; your names, generations, hashes
and sequence numbers come from your own sprint. A printed `next:` line is a
suggestion for the configured workflow, not a reason to run setup or invent
missing values.

## Inspect a release before setup

The [official releases](https://github.com/mas-bandwidth/nova-sprint/releases)
contain `nova-sprint`, `nova-worker` and `SHA256SUMS` for Linux and macOS, on
amd64 and arm64. The following Bash block downloads one pinned release into a
private temporary directory, checks the binary against its release checksum,
and reads its version and help. It removes that temporary directory on exit;
it does not install a service or change an existing binary. It needs `curl`,
`awk`, and `sha256sum` (Linux) or `shasum` (macOS).

```sh
(
  set -eu
  NOVA_SPRINT_VERSION=v2.0.12
  case "$(uname -s)" in
    Linux) sprint_os=linux ;;
    Darwin) sprint_os=darwin ;;
    *) echo "This example needs Linux or macOS" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64) sprint_arch=amd64 ;;
    arm64|aarch64) sprint_arch=arm64 ;;
    *) echo "This example needs amd64 or arm64" >&2; exit 1 ;;
  esac
  sprint_trial=$(mktemp -d)
  trap 'rm -rf "$sprint_trial"' EXIT
  cd "$sprint_trial"
  asset="nova-sprint_${NOVA_SPRINT_VERSION}_${sprint_os}_${sprint_arch}"
  release="https://github.com/mas-bandwidth/nova-sprint/releases/download/$NOVA_SPRINT_VERSION"
  curl --fail --location --silent --show-error "$release/$asset" -o "$asset"
  curl --fail --location --silent --show-error "$release/SHA256SUMS" -o SHA256SUMS
  awk -v asset="$asset" '$2 == asset { print; found++ } END { if (found != 1) exit 1 }' SHA256SUMS > selected.sha256
  if [ "$sprint_os" = darwin ]; then
    shasum -a 256 -c selected.sha256
  else
    sha256sum -c selected.sha256
  fi
  chmod u+x "$asset"
  "./$asset" version
  "./$asset" help
  "./$asset" help up
)
```

The checksum check must succeed before the executable runs. `version` identifies
the release; `help` lists the verbs; `help up` describes setup. These explicit
commands need no store, bus, login or credentials. Bare `nova-sprint` means
`next`, and `check` inspects the configured fleet: neither is this first
inspection step. The printed help does not establish that setup, workers or
landing will work on your machines.

To run real work, continue below and install the selected release through
nova-fleet. Use the [worker route](#join-as-a-worker) instead if you are joining
someone else's sprint. To build from source, use `make build` with the Go
version in `go.mod`; it stamps the binaries with their source version.

## The concepts you need

| Term | Meaning |
| --- | --- |
| Stream | A repository and its base branch, plus the rules for landing work there. One sprint can have several streams. |
| Card | One bounded task with a brief, allowed paths, gates and dependencies. This durable task is a **producer**. Work and read attempts are its **consumers**. |
| Row | A participant's bus name. A friend row is an AI session; a bench row is a worker service. Its configured width limits the work and reads it holds together. |
| Deal | The run service puts an attempt in a row's hand and sends its command over the bus. Its deadline starts at the deal. |
| Take | The row acknowledges the deal and obtains the full brief. Taking does not allocate another card or change its generation. |
| Finish | The worker reports a pushed commit, or a failure. A successful finish starts review; it does not mean the change has landed. |
| Read | Another row reviews the specified head and answers `yes` or `fix` with a finding. |
| Land | The reviewed work reaches the stream's base. With the GitHub forge enabled, automatic landing opens the PR and checks CI at the required head. |
| Judgement | A decision the engine cannot make mechanically. The coordinator receives the question and the choices the engine accepts. |
| Policy | Versioned Starlark rules for scheduling, tiers, reviews, deadlines, model selection and pricing. Changing the active version changes the running sprint. |

A producer normally goes from `waiting` to `ready`, `working`, `review`,
`merging`, then `landed`. A fix can send it back for another attempt. A held
card waits with its reason; a failure makes it `fail`. Dependencies become usable
when their producers land, not when a worker finishes them.

Every dealt attempt has an id and generation, written `<card>@<gen>`, and a
sprint epoch. Copy these from the latest receipt. They prevent an old session
from reporting into a newer attempt or a different sprint.

## Set up a sprint

### 1. Set up nova-tools first

Start with the [nova-tools guide](https://github.com/mas-bandwidth/nova-tools/blob/main/docs/README.md).
Follow its fleet walkthrough to provision Tailscale, Redis, PostgreSQL, Ansible,
nova-config and nova-secrets. Keep its control seat, encrypted secrets store
and inventory: nova-sprint uses them rather than creating a separate fleet.
Finish that walkthrough with a green check:

```sh
nova-fleet check
```

Choose the machine that will run the sprint. Run setup on that machine, which
must be in the inventory with the `services` role. `up` runs only on the
machine you run it on; `--run-on` must name that machine. Machines with
the `bench` role receive worker services, including the run machine if it also
has that role. A bench needs its core count recorded, a supported harness,
model routes and the appropriate credentials. An unused machine should not
be marked as a bench for this setup.

Install a nova-sprint release on the control machine and participating machines
using the fleet installer. Here `<release-repository>` is the repository that
publishes your nova-sprint release and its checksums, usually
`mas-bandwidth/nova-sprint` for the official release; `<release-tag>` is the
version you intend to run:

```sh
nova-fleet install --repo '<release-repository>' --tag '<release-tag>' --names nova-sprint '<machine>'
nova-sprint version
nova-sprint help up
```

The sprint binary carries a minimum nova-tools version. Its check names the
required version if yours is too old. For streams using GitHub, the run service
also needs Git and `gh` authenticated with an account able to read, push,
create PRs and merge in the target repositories. Keep the service's credentials
and repository access available after your interactive shell exits.

### 2. Select your seat and connect to the bus

Use the values established by nova-tools. These variables are locations and
connection details, not secret values. The PostgreSQL URL must have no password.
The control seat's name must match this machine's short hostname and inventory
name; your participant row is a separate name.

```sh
export NOVA_SECRETS_STORE='<absolute-path-to-encrypted-store>'
export NOVA_SECRETS_KEY='<absolute-path-to-seat-key>'
export NOVA_SECRETS_PINS='<absolute-path-to-seat-pins>'
export NOVA_SECRETS_SEAT='<machine-seat>'
export NOVA_PG_DSN='postgres://nova_config@<database-host>:<port>/<database>?sslmode=<mode>'
export NOVA_BUS_REDIS='<redis-host>:<port>'
export NOVA_BUS_USER='<bus-user>'
export NOVA_SPRINT_AS='<coordinator-row>'
```

The control seat must be granted the fleet's `NOVA_PG_ADMIN_PASSWORD`,
`NOVA_PG_PASSWORD` and, for an authenticated bus, `NOVA_BUS_PASSWORD` through
nova-secrets. Setup creates and grants the separate `NOVA_SPRINT_PG_PASSWORD`.
Do not put passwords or API keys in a command, a brief, a chat or the database URL.

Define these shell helpers once in this shell. `seat_exec` injects only the
named secrets. It restores the non-secret seat configuration so child fleet
commands can find it. `sprint_env` is the wrapper used for ordinary sprint
commands below; it is a shell function, not a nova-sprint verb. Save these
non-secret settings and helpers in a local shell file and source it in each
fresh command shell if your harness does not keep one shell open.

```sh
seat_exec() {
  local sprint_keys="$1"
  shift
  nova-secrets exec --only "$sprint_keys" \
    --store "$NOVA_SECRETS_STORE" --key "$NOVA_SECRETS_KEY" \
    --as "$NOVA_SECRETS_SEAT" -- \
    env NOVA_SECRETS_STORE="$NOVA_SECRETS_STORE" \
        NOVA_SECRETS_KEY="$NOVA_SECRETS_KEY" \
        NOVA_SECRETS_PINS="$NOVA_SECRETS_PINS" \
        NOVA_SECRETS_SEAT="$NOVA_SECRETS_SEAT" "$@"
}
sprint_env() {
  seat_exec NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD "$@"
}
```

Hook your row into nova-bus before hooking into the sprint. Run the form for
your harness from `nova-bus help hook`. For a session with a Monitor tool, run
this command under that Monitor, not as a background shell job or into a file:

```sh
nova-secrets exec --only NOVA_BUS_PASSWORD --store "$NOVA_SECRETS_STORE" --key "$NOVA_SECRETS_KEY" --as "$NOVA_SECRETS_SEAT" -- nova-bus hook --as "$NOVA_SPRINT_AS"
```

The long form above works in a Monitor shell that has the exported settings
but has not loaded the `seat_exec` function. The Monitor turns incoming lines
into session wakes. For a Codex session, use its service hook instead:

```sh
seat_exec NOVA_BUS_PASSWORD nova-bus hook --as "$NOVA_SPRINT_AS" --codex-thread '<thread-id>' --service
```

A successful hook reports `HOOK OK name=<row> waiting=<n>`; a service hook
reports `HOOK SERVICE OK`. Keep one bus hook per row. If another live session
owns that name, coordinate with it rather than inventing a second identity.

### 3. Run setup and choose the dashboard and services

Ask the human whether the dashboard should be private on the tailnet or public
on a machine they control, and which supported API services to enable. Run:

```sh
seat_exec NOVA_BUS_PASSWORD nova-sprint up --as "$NOVA_SPRINT_AS" --dash tailnet --services '<service-list>'
```

For a public dashboard, replace the dashboard choice with
`--dash 'public=<machine>:<port>'`. Setup installs a pull-only dashboard there;
that service holds no database or provider credential. The public machine may
also be a bench. Share the resulting URL only with the intended audience.

The current key-setup integrations accept `deepseek` and `openrouter`, separated
by commas. Choose only the services your deployment uses. Each selected service
asks the human for its key once at a hidden terminal prompt and checks it with
the provider. The AI runs the prompt; the human enters the value there. The
installed worker's supported routes and the policy's model catalog must also
match the selected configuration: adding a key alone does not make every model
runnable. Follow any missing-route or harness refusal before starting work.

Use `--services none` to skip API-key setup. This does **not** disable the worker
services installed on bench machines. For a sprint using only friend sessions,
configure the inventory accordingly. To repair one selected service's key,
setup also accepts `--key '<service>'`.

Run `up` through the bus-only wrapper above, not through `sprint_env`: setup
separates its database-admin and runtime credential phases itself. It resumes
by checking every step again. After fixing a refusal, repeat the same command.
Do not create the schema or worker services by hand to get past a failed step.

Setup creates the store, hooks your coordinator row, installs the policy, run
service, dashboard and bench workers, and checks worker readiness. Its completion
line has this shape:

```text
UP DONE run on <host>; dashboard <url>; <n> workers up; coordinator <row>
```

Read the entire output: an `UP NEEDS` line still requires attention even if
`UP DONE` follows. A fresh sprint is stopped. Setup does not start dealing cards.

### 4. Set the commit identity and check readiness

Set the author and committer identity that your repositories accept. This is
one sprint-wide Git identity, not the individual worker's name:

```sh
sprint_env nova-sprint identity --name '<git-name>' --email '<git-email>' --reason '<why-this-identity>'
sprint_env nova-sprint help playbook coordinator
sprint_env nova-sprint check
```

`hook` and `take` print the two Git configuration commands workers must use.
A worker cannot hook or take until this identity exists. If setup stopped at
worker readiness for that reason, set the identity once the store exists, then
repeat `up` to verify the remaining steps. Contributors receive
credit in commit-message trailers.

`check` examines the fleet, tool versions, store, bus, policy, roster and stream
admission. It also explains open judgements, landing waits and why nothing is
being dealt. Resolve each `CHECK RED` using its printed fix before starting.
Use the dashboard URL from setup to confirm that the board is reachable.
Continue with [Run a sprint as coordinator](#run-a-sprint-as-coordinator) to
register a repository, add work and start the run service's tick.

## Join as a worker

Obtain the fleet connection settings, your seat and row name from the
coordinator. Use the environment and helpers above, with `NOVA_SPRINT_AS` set to
your own row. The coordinator registers your friend row in nova-config with
its real harness, model, width and tiers. Your seat needs the sprint and bus
credentials; ask the coordinator to grant them through nova-secrets. You do
not need the database-admin credential.

Hook into the bus first using your harness's form above, then run:

```sh
sprint_env nova-sprint hook --as "$NOVA_SPRINT_AS" --role worker
nova-sprint help playbook worker
sprint_env nova-sprint next
```

The sprint hook prints:

```text
HOOKED <row> role=worker
COMMIT AS <git-name> <git-email>
```

It also prints Git configuration, the playbook command and the next action.
Read the playbook once. Follow the returned `next:` line. When it says
`next: nothing; wait for the bus`, wait for a wake; do not poll the queue.
The bench worker service follows this lifecycle automatically.

When a receipt prints a bare `nova-sprint` command, run it through `sprint_env`
in this shell. Preserve the receipt's arguments, including `--as`, generation
and epoch. The environment variable supplies your row only when `--as` is absent.

### Work a dealt card

A `DEAL` puts the card in your hand immediately. Run every card in your hand,
each in an isolated child or session and clone, concurrently up to your row's
width. Until you acknowledge it you are nudged every second; after three
minutes it is dealt to another row with room, or the coordinator is asked
(policy's `ready_nag` and `ready_take`). Acknowledge it with the command from
the deal:

```sh
sprint_env nova-sprint take '<card>@<gen>' --epoch '<epoch>'
```

The receipt starts with:

```text
TAKEN <card> repo=<repository> base=<sha> branch=<branch> deadline=<n>s forge=<forge>
```

It includes the full brief, any previous head and finding, the commit identity
and the exact finish command. Repeating the same `take` prints the brief again
without changing the generation.

1. Clone the repository into a directory used only by this attempt. Inspect the
   dealt branch at origin. If it exists, fetch it and continue from its tip;
   a prior attempt may already have pushed work. Otherwise create the dealt
   branch at the supplied base SHA. Do not discard earlier work.
2. Read the repository instructions and the brief. Work only within `PATHS`.
   Run the brief's gates at the starting head to identify existing failures;
   cite documented base failures rather than expanding the task to fix them.
3. Make the requested change. Run the brief's `DONE-WHEN` gates and the whole
   package tests for the files you touched on a suitable bench. Keep unit
   tests independent of real time and external services; functional tests own
   their test services. Ask the coordinator when the required route is missing
   or the change needs paths outside the brief.
4. Apply the Git identity printed by `take` to both author and committer.
   Put your own credit in a `By:` line and `Co-Authored-By:` trailer. Commit
   on the dealt branch and push that branch as you reach useful checkpoints.
5. Verify the remote branch tip is the full SHA you will report. Open no PR:
   automatic landing opens it. You need not chase the base or wait for its CI;
   landing handles a head behind the base, including grouping when necessary.
6. Report the pushed head using the receipt's finish command:

```sh
sprint_env nova-sprint finish '<card>@<gen>' --head '<full-sha>' --epoch '<epoch>'
```

`FINISHED <card> head=<sha> applied at seq <n>` acknowledges the report. It does
not certify review, CI or landing. A later result tells you what happened.

Pushes preserve work; uncommitted local edits are not stored by nova-sprint.
Never push to the base, another card's branch or the stream's group branch.
If a withdraw arrives, stop that attempt. Do not finish it with stale arguments.
Its pushed branch remains available to the next attempt.

### Read someone else's head

Take the read exactly as dealt. Its receipt names the reviewed head instead
of a work branch and base:

```text
TAKEN <card> repo=<repository> head=<sha> deadline=<n>s forge=<forge>
```

Fetch that full head in an isolated clone, inspect it against the brief and
base, and check the relevant evidence.
Review that head, not whichever commit the branch happens to point to now. Do
not edit it. Report one of:

```sh
sprint_env nova-sprint read '<read-card>@<gen>' yes --epoch '<epoch>'
sprint_env nova-sprint read '<read-card>@<gen>' fix --finding '<path>:<line>: <defect-and-repair>' --epoch '<epoch>'
```

The acknowledgement has this form (`yes` is recorded as `ok`):

```text
READ <card> head=<sha> result=<ok-or-fix> applied at seq <n>
```

A fix needs a concrete finding. An author cannot read their own head. The
policy chooses how many independent reads a head needs and what happens when
those reads disagree. A fix becomes a new work deal carrying the prior head
and finding; do not quietly amend an attempt you already reported.

For both finishes and reads, report only measured usage. `--usage` accepts
harness-counted token fields; see `nova-sprint help finish` for their syntax.
Subscription rows are recorded as free. API rows are priced from measured
tokens and the policy's rates; missing tokens or rates mean unpriced, not zero.
`--cost` records the harness's own charged-dollar figure alongside that price.
Do not estimate it or use zero to conceal missing API usage.

### Release, fail or recover a session

If work must continue elsewhere, push what you have and release it, with the
pushed head when available. If the task itself cannot be completed, report a
failure with a reason instead:

```sh
sprint_env nova-sprint release '<card>@<gen>' --head '<pushed-sha>' --reason '<why>' --epoch '<epoch>'
sprint_env nova-sprint finish '<card>@<gen>' --failed --reason '<why>' --epoch '<epoch>'
```

Ask the coordinator about scope or missing prerequisites rather than repeatedly
retrying a failing command. Messages from the name `sprint` are receipts:
answer with their CLI verb, or do nothing. Do not reply to them with a bus
message. Messages from the coordinator can require a conversational reply.

After losing a session, reconnect its bus hook, clear your old sprint hook,
then hook again and ask for your next action:

```sh
sprint_env nova-sprint unhook --as "$NOVA_SPRINT_AS"
sprint_env nova-sprint hook --as "$NOVA_SPRINT_AS" --role worker
sprint_env nova-sprint next
```

Cards and findings remain in the store. Recover pushed work from the dealt
branch, and follow the current generation rather than a remembered command.

## Run a sprint as coordinator

The coordinator writes work, answers judgements and manages configuration.
The engine deals, requests reads and lands. A coordinator can also work cards;
those attempts follow the worker rules above.

### Invite worker sessions

Register each friend session in nova-config before expecting it to receive
work. Include the coordinator's own row if it will work cards. Choose the
actual harness and model the session runs, the tiers it can serve, and a width
it can sustain:

```sh
seat_exec NOVA_PG_PASSWORD nova-config friend add '<row>' --harness '<harness>' --model '<model>' --width '<width>' --tiers '<tier-list>' --actor "$NOVA_SPRINT_AS" --reason '<why>'
```

For an existing row, inspect its configuration and use nova-config's update
command from its help rather than adding it twice. Grant an already registered
worker seat the runtime secrets, then distribute the encrypted store:

```sh
nova-secrets grant NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD --to '<worker-seat>' --pins "$NOVA_SECRETS_PINS" --store "$NOVA_SECRETS_STORE" --key "$NOVA_SECRETS_KEY" --as "$NOVA_SECRETS_SEAT"
seat_exec NOVA_PG_PASSWORD nova-fleet secrets-refresh
```

Give the worker its row name, connection settings and this guide's worker
section. Let it connect the bus and sprint hooks from its own session. Use
nova-tools' seat setup first if the seat or its public-key pin does not exist.
Bench rows are installed by `up` and announce their proven capacity themselves.

### Register a stream and write a brief

Register a repository whose base branch already exists at origin:

```sh
sprint_env nova-sprint stream add '<stream>' --repo '<org>/<repo>' --base '<base-branch>' --mode auto --forge github
```

Use the GitHub forge when you want the engine to open PRs and check CI. Without
it, a stream has no PR/CI integration and lands by hand. The default `auto`
mode selects single or grouped landing as appropriate; `group` batches work,
and `hand` leaves landing to the coordinator. Review the stream's setup before
adding cards:

```sh
sprint_env nova-sprint show '<stream>'
```

Write one brief file per producer. Its filename without `.md` becomes the card
id. This is a template, not an executable brief; replace its placeholders with
paths and commands valid in that repository:

```text
tier: flash
PATHS: <existing-source-path>, <existing-test-path>
NEEDS: none
TEST: <regression-test absent or red at the starting head, green after the fix>
DONE-WHEN: <test-program> <arguments>

<Describe the problem, required behavior and limits of this one change.>
```

The first line must be `tier: flash`, `tier: pro`, `tier: heavy` or
`tier: frontier`. The tier must have an eligible row. `PATHS`, `TEST`,
`DONE-WHEN` and prose after a blank line are required. Existing paths and glob
matches are checked at the stream's base. A new file may be named explicitly when its parent directory exists. To create a new
directory, declare it as a separate `PATHS` entry ending in `/`; its parent
must exist. Keep the allowed paths as narrow as the task permits. Each
`DONE-WHEN` is one program and its arguments, not an unquoted shell pipeline; repeat the field for multiple gates.

`NEEDS` lists producer ids whose landed output this task actually consumes.
Independent work should have no needs. `AFTER` is an optional soft landing
order, not a start dependency. `PRIORITY` defaults to `normal`; producer levels
are `low`, `normal`, `high`, `critical` and `blocker`; reads are dealt between
`high` and `critical`. A blocker can preempt work, so use it for that intended effect.
Use `NEEDS-ROUTE` for a required execution capability rather than hardcoding a
particular machine into the task.

Validate the brief before adding it, then add the same file:

```sh
sprint_env nova-sprint add --stream '<stream>' --brief-file '<card>.md' --dry-run
sprint_env nova-sprint add --stream '<stream>' --brief-file '<card>.md'
```

Admission checks the brief, repository base, paths, dependency graph and
available tiers/routes. It does not run the brief's tests. A dry run prints
one line per card in this form:

```text
ADD <id> kind=<kind> stream=<stream> tier=<tier> priority=<priority> needs=<needs>
```

A dependency-chain line follows, then `(dry run)` and the next command.
An actual add instead reports `ADDED <n> cards applied at seq <n>`. Review any
dependency-chain warnings before proceeding.

### Start, observe and answer decisions

Make sure workers have joined, their width and tiers reflect real capacity,
and checks are green. Start with a small real card whose landing you can
observe end to end:

```sh
sprint_env nova-sprint check
sprint_env nova-sprint start --reason '<why-ready>'
sprint_env nova-sprint queue --stream '<stream>'
sprint_env nova-sprint show run
```

Watch its work, reads and landing appear in the dashboard. Confirm the landed
commit is on the stream's base before expanding the queue. `up` already
installed the run service; do not launch a second chooser process.

For a stuck landing, inspect the stream's stitch view. For decisions, list
judgements and answer using an offered choice:

```sh
sprint_env nova-sprint show '<stream>' --stitch
sprint_env nova-sprint judge
sprint_env nova-sprint judge '<judgement>' '<offered-choice>' --reason '<why>'
```

A judgement can concern conflicting reads, a failed gate, an unavailable route
or a landing problem. Read its evidence and choices. Group answers require
`--expect` with the current group size, so a changed group is not answered
accidentally. Follow the judgement's printed command.

The separate `land` verb is a manual merge with **no CI gate**. Use it only
when deliberately taking responsibility for that manual decision; consult
`nova-sprint help land`. Normal automatic landing should proceed through its
reads, CI and judgement choices.

### Change work or policy while the sprint runs

Hold a row, stream or producer to pause the relevant work, then unhold it when
the reason is resolved:

```sh
sprint_env nova-sprint hold '<row-or-stream-or-card>' --reason '<why>'
sprint_env nova-sprint unhold '<row-or-stream-or-card>' --reason '<why-resolved>'
```

A card hold withdraws its live consumers and keeps pushed branches. A row hold
stops new deals to that row. To change an admitted brief, add a new card and
replace the old producer; there is no brief-edit verb:

```sh
sprint_env nova-sprint replace '<old-card>' --by '<new-card>' --reason '<why>' --dry-run
sprint_env nova-sprint replace '<old-card>' --by '<new-card>' --reason '<why>'
```

Replacement redirects dependants to the new producer. Dropping work can fail
its dependants unless you explicitly waive or relink them. `drop --waive`
waives the dropped need for a dependant, `drop --relink` redirects its need as part of the drop,
and `relink` repairs a dependant's already failed need. Inspect
`nova-sprint help drop` and `nova-sprint help relink` before doing so, and
preview a drop with its dry run.

Inspect the active policy and its versions before editing a local policy file:

```sh
sprint_env nova-sprint policy show
sprint_env nova-sprint policy show --list
sprint_env nova-sprint policy check '<policy-file>'
sprint_env nova-sprint policy set '<policy-file>' --reason '<why>'
sprint_env nova-sprint policy diff '<old-version>' '<new-version>'
sprint_env nova-sprint policy activate '<new-version>' --reason '<why>'
```

Use the version number returned by `set`. With an active policy, setting
stores a checked version without activating it. If no policy is active, the
first stored version activates automatically. Inspect changes before activating
a replacement. To roll back, activate the intended earlier stored version
with a reason.

To stop the engine's tick or save a verified backup:

```sh
sprint_env nova-sprint stop --reason '<why>'
sprint_env nova-sprint backup --out '<backup-directory>'
```

Stopping prevents new dealing, reading and landing by the tick. It does not
kill worker processes or disable commands. Coordinate with holders if work
must stop immediately. A backup receipt says
`BACKUP <file> epoch=<epoch> seq=<n> verified`. Keep that file securely; it
contains sprint data. See `nova-sprint help restore` before restoring into an
empty target, and `nova-sprint help clear` before ending an epoch. Neither is
a repair for an ordinary refused command.

## Other verbs

| Verb | When to use it | Syntax and refusals |
| --- | --- | --- |
| `priority` | Change a producer's priority, with a reason. | `nova-sprint help priority` |
| `drop` | Fail work that should not be done, with explicit treatment of its dependants. | `nova-sprint help drop` |
| `relink` | Redirect a dependant's failed need to another producer. | `nova-sprint help relink` |
| `stream set` | Change a stream's landing mode or forge. | `nova-sprint help stream set` |
| `cost reconcile` | Compare provider balance changes with recorded API costs and raise a cost-gap judgement when required. | `nova-sprint help cost reconcile` |
| `wake` | Consume a bus message on stdin and acknowledge each dealt card before printing its receipt or refusal. | `nova-sprint help wake` |

For a Monitor-based bus hook that acknowledges deals before waking the session,
replace the earlier Monitor command with this form. Load the settings and
helpers in that shell first. Both runtime credentials must reach the `wake`
child, so use `sprint_env`:

```sh
sprint_env nova-bus hook --as "$NOVA_SPRINT_AS" --exec "nova-sprint wake --as $NOVA_SPRINT_AS"
```

This is the `hook --exec` form; keep the service-hook form described earlier
for a Codex session. The output of `wake` includes the full `take` receipt or
its refusal. Read it and start the work; acknowledging a deal does not do it.

## When a command refuses

A command normally reports its result and ends with `next: <command>`. Run
that command through the same credential wrapper. Exit codes are:

| Code | Meaning | What to do |
| --- | --- | --- |
| 0 | The command completed. | Read the receipt and next action. |
| 1 | A prerequisite or operation was refused. | Follow the named fix, then retry if still applicable. |
| 2 | Bad usage, or a store, bus or external operation failed. | Read the error; check help for syntax or run `check` for infrastructure. |

Refusals have the form `SPRINT REFUSED <verb>: <problem>: <fix>`.
Common situations and the next step:

| Situation | Next step |
| --- | --- |
| This process has no sprint credential | Run it through `sprint_env`; confirm your seat has the sprint grant. Do not rerun setup just because your shell lacks a credential. |
| Not on the bus | Start the harness's bus hook, then retry the sprint hook. |
| Already hooked | Recover your own session with `unhook`, then hook again. Resolve another live owner first. |
| No commit identity | The coordinator sets it with `identity`; the worker then hooks or takes again. |
| Not dealt at this generation, withdrawn or already reported | Run `next` as your row and use its current command. Do not invent a new generation. |
| Head is absent at origin | Push the dealt branch and verify its full remote SHA before finishing. |
| Wrong author or committer | Apply the identity printed by `take` and correct the affected commits on your own card branch. |
| Brief admission failed | Fix the named field, path, dependency or unavailable tier/route, then repeat the dry run. |
| Nothing deals or landing waits | Run `check`, inspect `show run`, and answer open judgements; also check whether the sprint is stopped. |
| A worker service cannot decrypt a newly granted secret | Refresh the machines' encrypted secrets replicas through nova-fleet; the shell's store and the installed service's replica may be different directories. |

`nova-sprint help` lists the verbs. `nova-sprint help <verb>` gives that
verb's syntax, prerequisites and refusals. `nova-sprint help playbook worker`
and `nova-sprint help playbook coordinator` give the role-specific workflow.
For automation, `next --json` and `take --json` return machine-readable answers;
use their structured fields rather than parsing the prose next line.
