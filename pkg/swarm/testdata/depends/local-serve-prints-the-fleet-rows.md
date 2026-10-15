RESULT: local-serve-prints-the-fleet-rows sha=<sha12> tier: pro
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
STOP: nova-local serve over a tailnet --base with --tier, --concurrency and --deadline prints the three rows that put the served model in the fleet (the provider, the route, the concurrency budget), and TestServeOverTheTailnetNamesTheFleetRows pins them against the fake engine.
PATHS: cmd/nova-local/main.go, cmd/nova-local/host.go, cmd/nova-local/main_test.go, cmd/nova-local/README.md, docs/SPEC-LOCAL.md, docs/TESTS.md, docs/CLI.md
NEW: cmd/nova-local/fleet.go, cmd/nova-local/fleet_test.go
SHARED: cmd/nova-local/main.go, docs/SPEC-LOCAL.md, docs/TESTS.md, docs/CLI.md
TEST: cmd/nova-local TestServeOverTheTailnetNamesTheFleetRows
DEPENDS-ON: local-config-provider-endpoint, local-engine-line-store-on-a-down-engine
SOURCE: Glenn, ~14:13Z 2026-10-15: "Please add the work for nova-local to the current sprint, low priority."; the nova-tools roadmap item nova-local-provider-per-machine ("Restore nova-local built to its spec, with local as a provider per fleet machine that the sprint deals to like any route"; from PR #5307, closed 2026-10-10 when the sprint moved to mas-bandwidth/nova-sprint, its head f322683d4d kept on origin/rowan/nova-local).
PRIORITY: low
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 75 minutes; report what you have with the verdict not-done rather than push past it.

RULES.
Work only in the job directory this card names.
Never force-push or rebase a shared branch.
Never kill a process you did not start.
Never start a server on this machine.
No `rm -rf` outside the job directory.
Report what was not done.
Unit tests use no real time, no sockets, no network and no outside services: time is an argument (a fake clock passed in, never sleep), the engine is a fake behind the injected transport, the store is the mem twin.
No local model on the working machine, and none from this card anywhere: never install, pull, start or stop ollama or any engine, and never run nova-local serve against a real engine; every engine this card touches is a fake. A test that would need a real model belongs in the functional tier, skipped where none is present.
A refusal or a check is never routed around, and no existing check is weakened: the change adds a check or moves one earlier, it never turns one off.
nova-local stays a printer here: it runs no nova-config or nova-sprint command and writes no row (rule 11: it writes exactly one file, worker's --out).

THE DESIGN (the stream's one contract; docs/SPEC-LOCAL.md "Fleet" states it once card local-spec-fleet-provider-per-machine lands). Local is a provider per fleet machine, dealt like any route:
- The provider: one nova-config provider row per serving machine, named local-<host>, with a new provider field endpoint, http://<host>:11434/v1. A provider whose name begins local- carries an endpoint and every other provider carries none. The endpoint obeys nova-local's rule 3 (loopback or the tailnet, 100.64.0.0/10, every address a name resolves to), and an endpoint whose host is a nova-config machine row holding the role coordination is refused: the coordinating machine serves no local model. Billing subscription, funded true, no key.
- The route: --provider local-<host> --model <the served tag>, every price 0, named local-<model>-<host>; its tokens are recorded as any route's.
- The concurrency: the provider concurrency budget nova-sprint already has (nova-sprint routes limit local-<host> --concurrent <n>; internal/sprint/rate_budget.go (read), tla/RateBudget.tla (read)): the deal falls back past a full provider and the card waits ready when none of its tier has room. No route field concurrency, no machine field, no new count on a machine row: a machine's width stays its lanes.
- The deal stamps the provider's endpoint on the work or read card as the card field endpoint, the packet carries it as JSON endpoint, and nova-sprint routes prints endpoint=<url> concurrency=<busy>/<cap> on a local route.
- The launch: the member passes --local-base <endpoint> to nova-swarm native for a packet naming an endpoint; native declares provider local-<host> at that base in the job's harness config (OpenCode's OpenAI-compatible provider, no key), the route's model under it, its read-deadline proxy in front.

THE TASK. Build THE DESIGN's operator step, the nova-local side only. serve gains three optional flags, given together or not at all: --tier <flash|pro|heavy|frontier>, --concurrency <n> (above 0) and --deadline <seconds> (above 0); one or two of them is exit 2 naming the missing ones (no guessed anything). With all three and a --base whose host is not loopback, after the SERVE OK line serve prints three lines, each SERVE FLEET run: <command>:
  nova-config provider add local-<host> --endpoint http://<host>:<port>/v1 --billing subscription --funded true
  nova-config route add local-<serve_as>-<host> --tier <tier> --provider local-<host> --model <serve_as> --deadline <seconds> --price_input 0 --price_output 0
  nova-sprint routes limit local-<host> --concurrent <n> --reason "<serve_as> on <host>"
<host> is --base's host as spelled and <serve_as> the derived tag serve printed; the route is named for the served tag. Read the exact flag spellings of nova-config provider add and route add from pkg/config/kind.go (read) at your base (the endpoint field lands with card local-config-provider-endpoint) and of routes limit from mas-bandwidth/nova-sprint's cmd/nova-sprint/rate_budget.go; do not invent a flag. A loopback --base prints a NOTE that the fleet reaches the engine only over the tailnet and no FLEET line. --dry-run prints the same lines. Update docs/SPEC-LOCAL.md's serve synopsis and line shapes (a subsection headed with this card id), cmd/nova-local/README.md, and docs/TESTS.md if the first run changes.
Libraries considered: none: string formatting in the package; the host rule is host.go's.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Red test first: TestServeOverTheTailnetNamesTheFleetRows in cmd/nova-local/fleet_test.go, opening with t.Parallel(), against the fake engine (the fake resolver maps the host g1 to a tailnet address; no DNS, no socket): serve --base http://g1:11434/v1 --tier flash --concurrency 2 --deadline 1800 --dry-run prints the three FLEET lines exactly; --tier alone is exit 2 naming --concurrency and --deadline; a loopback base prints the NOTE and no FLEET line; the fake records no /api/create in the dry run. Run go test -count=1 -timeout 600s ./cmd/nova-local/ -run TestServeOverTheTailnetNamesTheFleetRows and keep the failing line.
STEP 3. Make it pass in the PATHS and NEW files only.
STEP 4. Gate: go test -count=1 -timeout 600s ./cmd/nova-local/ ./internal/docs/ ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it.
STEP 5. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, the test and what it pins, and what was not done.
STEP 6. End as JOB.md says.
