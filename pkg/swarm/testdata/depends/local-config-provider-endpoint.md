RESULT: local-config-provider-endpoint sha=<sha12> tier: pro
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
STOP: nova-config's provider kind has the field endpoint, set on a local-<host> provider and refused on any other, its host loopback, a tailnet address or a machine row's name, and refused on a machine holding the role coordination; a migration adds the column; TestALocalProviderNamesItsEndpoint and TestALocalEndpointOnTheCoordinatingMachineIsRefused pin it on the file store.
PATHS: pkg/config/kind.go, pkg/config/provider.go, pkg/config/store.go, pkg/config/redis.go, docs/SPEC-CONFIG.md, docs/TESTS.md, cmd/nova-config/*_test.go, internal/ci/testdata/errcheck/pkg/config.txt
NEW: pkg/config/migrations/0045_provider_endpoint.sql, pkg/config/provider_endpoint_test.go
SHARED: docs/SPEC-CONFIG.md, docs/TESTS.md, internal/ci/testdata/errcheck/pkg/config.txt
TEST: pkg/config TestALocalProviderNamesItsEndpoint
DEPENDS-ON: local-spec-fleet-provider-per-machine
SOURCE: Glenn, ~14:13Z 2026-10-15: "Please add the work for nova-local to the current sprint, low priority."; the nova-tools roadmap item nova-local-provider-per-machine ("Restore nova-local built to its spec, with local as a provider per fleet machine that the sprint deals to like any route"; from PR #5307, closed 2026-10-10 when the sprint moved to mas-bandwidth/nova-sprint, its head f322683d4d kept on origin/rowan/nova-local).
PRIORITY: low
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 90 minutes; report what you have with the verdict not-done rather than push past it.

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
Change no live nova-config row and run no migrate against a live Postgres: the migration is applied by the coordinator; tests use the file store (the try.json store of docs/TESTS.md's nova-config first run).

THE DESIGN (the stream's one contract; docs/SPEC-LOCAL.md "Fleet" states it once card local-spec-fleet-provider-per-machine lands). Local is a provider per fleet machine, dealt like any route:
- The provider: one nova-config provider row per serving machine, named local-<host>, with a new provider field endpoint, http://<host>:11434/v1. A provider whose name begins local- carries an endpoint and every other provider carries none. The endpoint obeys nova-local's rule 3 (loopback or the tailnet, 100.64.0.0/10, every address a name resolves to), and an endpoint whose host is a nova-config machine row holding the role coordination is refused: the coordinating machine serves no local model. Billing subscription, funded true, no key.
- The route: --provider local-<host> --model <the served tag>, every price 0, named local-<model>-<host>; its tokens are recorded as any route's.
- The concurrency: the provider concurrency budget nova-sprint already has (nova-sprint routes limit local-<host> --concurrent <n>; internal/sprint/rate_budget.go (read), tla/RateBudget.tla (read)): the deal falls back past a full provider and the card waits ready when none of its tier has room. No route field concurrency, no machine field, no new count on a machine row: a machine's width stays its lanes.
- The deal stamps the provider's endpoint on the work or read card as the card field endpoint, the packet carries it as JSON endpoint, and nova-sprint routes prints endpoint=<url> concurrency=<busy>/<cap> on a local route.
- The launch: the member passes --local-base <endpoint> to nova-swarm native for a packet naming an endpoint; native declares provider local-<host> at that base in the job's harness config (OpenCode's OpenAI-compatible provider, no key), the route's model under it, its read-deadline proxy in front.

THE TASK. Build THE DESIGN's provider, the config side only. Line numbers are at the v1.3/comms tip.
(1) The provider kind (pkg/config/kind.go, KindProvider, around line 594) gains a field endpoint, TypeText, help: the engine's OpenAI-compatible base URL of a local provider, http://<host>:<port>/v1; empty on every other provider.
(2) The row check (checkProvider, pkg/config/provider.go:11): a provider named local-<host> needs an endpoint; any other provider with one is refused; the endpoint parses as an http URL with a host and a path /v1; a literal IP host must be loopback or in the tailnet's range 100.64.0.0/10 (the same range as cmd/nova-local/host.go (read) line 14 and pkg/bus/addr.go (read) line 14). The check resolves no name: it makes no DNS lookup and opens no socket.
(3) The store check (pkg/config/store.go, beside checkTierRoutes): a name host must be loopback (localhost) or the name of a machine row, and a machine row whose roles hold coordination is refused, naming the machine and the remedy (serve the model on a bench machine; the coordinating machine runs no local model). Each refusal names its remedy as the kind's other refusals do.
(4) A migration, the next free number at your base (0045 at the tip; if another card takes it first, renumber and say so), adding the column, NOT NULL default '', idempotent as 0042 is. If the provider's Redis writer (pkg/config/redis.go, ProviderKey around line 61) lists fields, add endpoint so the provider hash carries it.
(5) docs/SPEC-CONFIG.md: the field's row in the provider section, in a subsection headed with this card id. docs/TESTS.md: update the nova-config first run's migration count (to=44 at the tip) and any pinned provider help.
Libraries considered: net/url and net/netip from the standard library for the URL and range; the kind descriptor's own TypeText and Check hooks; no new module.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Red tests first, in pkg/config/provider_endpoint_test.go, each opening with t.Parallel(), on the file store in t.TempDir(): TestALocalProviderNamesItsEndpoint (local-g1 with http://g1:11434/v1, g1 a machine row with roles bench, is accepted and its endpoint read back; local-g1 with no endpoint, an openrouter provider with one, an endpoint 8.8.8.8, a URL that is not http or has no /v1 path and a name that is no machine row are each refused naming the problem); TestALocalEndpointOnTheCoordinatingMachineIsRefused (a machine row with roles bench,coordination named in the endpoint is refused naming it). Run go test -count=1 -timeout 600s ./pkg/config/ -run 'TestALocalProviderNamesItsEndpoint|TestALocalEndpointOnTheCoordinatingMachineIsRefused' and keep the failing line.
STEP 3. Make them pass in the PATHS and NEW files only.
STEP 4. Gate: go test -count=1 -timeout 600s ./pkg/config/ ./cmd/nova-config/ ./internal/docs/ ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it.
STEP 5. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, each test and what it pins, the migration's number, the exact nova-config migrate and provider add lines the coordinator runs, and what was not done.
STEP 6. End as JOB.md says.
