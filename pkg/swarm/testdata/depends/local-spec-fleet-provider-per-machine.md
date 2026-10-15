RESULT: local-spec-fleet-provider-per-machine sha=<sha12> tier: pro
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
STOP: docs/SPEC-LOCAL.md's Fleet section and Tests table state THE DESIGN below, docs/SPEC-CONFIG.md's provider section names the endpoint field, and TestSpecLocalTestsAreRealOrOwed pins that every test the spec names exists or names the card that owes it.
PATHS: docs/SPEC-LOCAL.md, docs/SPEC-CONFIG.md, internal/docs/spec_*_test.go
SHARED: docs/SPEC-CONFIG.md
TEST: internal/docs TestSpecLocalTestsAreRealOrOwed
SOURCE: Glenn, ~14:13Z 2026-10-15: "Please add the work for nova-local to the current sprint, low priority."; the nova-tools roadmap item nova-local-provider-per-machine ("Restore nova-local built to its spec, with local as a provider per fleet machine that the sprint deals to like any route"; from PR #5307, closed 2026-10-10 when the sprint moved to mas-bandwidth/nova-sprint, its head f322683d4d kept on origin/rowan/nova-local).
PRIORITY: low
You are a child of the coordinator: one task, one worktree, one branch, unattended. This card is the whole of the task and it stands alone in front of a stranger; nothing outside it is owed to you.
Deadline: finish within 60 minutes; report what you have with the verdict not-done rather than push past it.

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
No names of people, machines or friends in a doc this card writes: the class test TestGeneralityText refuses them; write "the owner" and "the coordinating machine".

THE TASK. docs/SPEC-LOCAL.md's Fleet section (from "## Fleet", around line 235 at the v1.3/comms tip) was designed on 2026-10-04 against code that has since moved: it asks for nova-config route fields endpoint and concurrency in "migration 0028", but 0028 is pkg/config/migrations/0028_heavy_tier_route_harness.sql (read) and the latest is 0044; the sprint (internal/sprint/route.go) now lives in mas-bandwidth/nova-sprint, which since 2026-10-11 has a provider concurrency budget that already does the cap the section designs. Rewrite the Fleet section to THE DESIGN below, so every other card of this stream builds against one text.
THE DESIGN (the stream's one contract; docs/SPEC-LOCAL.md "Fleet" states it once card local-spec-fleet-provider-per-machine lands). Local is a provider per fleet machine, dealt like any route:
- The provider: one nova-config provider row per serving machine, named local-<host>, with a new provider field endpoint, http://<host>:11434/v1. A provider whose name begins local- carries an endpoint and every other provider carries none. The endpoint obeys nova-local's rule 3 (loopback or the tailnet, 100.64.0.0/10, every address a name resolves to), and an endpoint whose host is a nova-config machine row holding the role coordination is refused: the coordinating machine serves no local model. Billing subscription, funded true, no key.
- The route: --provider local-<host> --model <the served tag>, every price 0, named local-<model>-<host>; its tokens are recorded as any route's.
- The concurrency: the provider concurrency budget nova-sprint already has (nova-sprint routes limit local-<host> --concurrent <n>; internal/sprint/rate_budget.go (read), tla/RateBudget.tla (read)): the deal falls back past a full provider and the card waits ready when none of its tier has room. No route field concurrency, no machine field, no new count on a machine row: a machine's width stays its lanes.
- The deal stamps the provider's endpoint on the work or read card as the card field endpoint, the packet carries it as JSON endpoint, and nova-sprint routes prints endpoint=<url> concurrency=<busy>/<cap> on a local route.
- The launch: the member passes --local-base <endpoint> to nova-swarm native for a packet naming an endpoint; native declares provider local-<host> at that base in the job's harness config (OpenCode's OpenAI-compatible provider, no key), the route's model under it, its read-deadline proxy in front.
The change, docs only plus one test:
(1) docs/SPEC-LOCAL.md, "## Fleet": replace the route, concurrency, deal and launch bullets with THE DESIGN; keep the store and endpoint bullets; keep "Status: designed, not built", now naming what is owed: nova-config's provider field endpoint (card local-config-provider-endpoint), the deal's stamp and the routes view (card local-deal-stamps-the-endpoint, in mas-bandwidth/nova-sprint), the launch (card local-native-launches-at-the-endpoint), and serve's printed rows (card local-serve-prints-the-fleet-rows). Say where the deal lives (mas-bandwidth/nova-sprint).
(2) docs/SPEC-LOCAL.md, "## Tests", the Fleet row: the tests and the card owing each: TestALocalProviderNamesItsEndpoint and TestALocalEndpointOnTheCoordinatingMachineIsRefused (local-config-provider-endpoint); TestADealOnALocalProviderStampsItsEndpoint, TestALocalProviderAtItsConcurrencyDrawsTheNextRoute and TestRoutesShowsALocalRoutesEndpointAndConcurrency (local-deal-stamps-the-endpoint; written (in mas-bandwidth/nova-sprint) once it lands); TestAMemberPassesTheLocalBaseToNative, TestNativeTakesALocalBaseOnlyWithALocalProvider and TestDeclareLocalProviderPointsTheHarnessAtTheEndpoint (local-native-launches-at-the-endpoint); TestServeOverTheTailnetNamesTheFleetRows (local-serve-prints-the-fleet-rows). Write each owed test as `TestName` (owed: <card id>).
(3) docs/SPEC-CONFIG.md, the provider kind's section: one paragraph headed with this card id for the field endpoint (only on a local-<host> provider, rule 3, the coordinating machine refused), changing no other paragraph.
Libraries considered: none: the test reads two markdown files and greps Go sources with the standard library (os, regexp, path/filepath).

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Red test first: TestSpecLocalTestsAreRealOrOwed in internal/docs/spec_local_tests_test.go, opening with t.Parallel(): every `TestX` docs/SPEC-LOCAL.md's Tests table names is either a func TestX in a _test.go file of this repository, or is followed by (owed: <card id>), or by (in mas-bandwidth/nova-sprint) for a test of the sprint's repository; a name in none is a failure naming it. At the base it is red: the Fleet row names TestALocalRouteNamesItsEndpointAndConcurrency and others that exist nowhere and name no card. Run go test -count=1 -timeout 600s ./internal/docs/ -run TestSpecLocalTestsAreRealOrOwed and keep the failing line.
STEP 3. Rewrite the two docs as THE TASK says; the test turns green.
STEP 4. Gate: go test -count=1 -timeout 600s ./internal/docs/ ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it (run it on the unchanged base to say so).
STEP 5. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, the test and what it pins, and what was not done.
STEP 6. End as JOB.md says.
