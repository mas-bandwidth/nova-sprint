# Layer 7 box review (cold)

`go build ./...` and `go vet ./...` are green. `go test ./cmd/... ./verbs/...` is green except one functional test in `cmd/nova-worker` (real git and real processes), noted under Missing tests. The layer is not sound: its central cold-setup promise can fail on a fresh fleet, and the documented upgrade path cannot run at all.

## Verdict

The box is not sound. The worst thing is that `up` hard-codes the two provider keys every worker service opens (`DEEPSEEK_API_KEY`, `INCEPTION_API_KEY`) no matter what `--services` said, while no setup step saves or grants those keys to the bench seats; therefore the documented `--services none` path verifies step 6 and then installs worker units that cannot open their keys and cannot pass the worker probe at step 7. The upgrade path is also dead: `up --version` calls `run --shadow`, which the run verb's own parser rejects.

## Defects

1. **blocker — worker services hard-code provider keys that setup never chooses, saves, or grants.** `workerProviderKeys()` always returns `DEEPSEEK_API_KEY` and `INCEPTION_API_KEY` (`cmd/nova-sprint/setup_workload.go:22-26`); every worker record puts both in `--keys` (`cmd/nova-sprint/setup_workload.go:52-59`); and every worker gets the same DeepSeek+Inception routes (`cmd/nova-sprint/setup_workload.go:41-46`), never the services chosen at step 6. Step 6 only saves the selected keys (`verbs/setup_runtime.go:489-518`), and step 7 grants only `NOVA_SPRINT_PG_PASSWORD` through `SprintSeats`/`GrantSeat` (`verbs/setup_runtime.go:520-555`, `verbs/setup_runtime_role_bootstrap.go:100-110`). On a fresh fleet, `up --services none` therefore verifies step 6, installs worker units whose `--keys` name secrets that do not exist, and the worker's `Proxy.MissingKeys()` check (`edge/worker/probe.go:79-80`) keeps the row unhooked, so step 7 cannot verify. Choosing `openrouter` is also useless: it stores `OPENROUTER_API_KEY` but no worker route uses it. This breaks `docs/SETUP.md:73` ("`--services none` is a valid answer") and `docs/SETUP.md:32-36` ("the AI makes the seats, the grants, the worker rows and the service rows"). Repair: derive the worker's `--providers`/`--keys` from the chosen services and grant each chosen key to every bench seat before the worker play, or state and implement that none still requires both hard-coded keys.

2. **serious — `up --version` can never reach its shadow check because `run --shadow` is not a run flag.** `VersionAdoption.Ready` calls `run --shadow` (`verbs/setup_runtime_version.go:65`), but the run verb's table declares only `listen=` (`verbs/table_coordinate.go:51`), and the one parser refuses unknown flags (`verbs/flags.go:54-58`). Reproduced with:

   ```
   $ go run ./cmd/nova-sprint run --shadow
   SPRINT REFUSED run: unknown flag --shadow: nova-sprint help run
   exit status 2
   ```

   So even when the requested release is already the installed one, `up --version` fails at `updated sprint shadow failed`; and `VersionAdoption.Apply` refuses to install anything, naming `nova-fleet install` (`verbs/setup_runtime_version.go:121-128`). This contradicts `docs/PLAYBOOK-COORDINATOR.md:364-366` and `docs/LAYER-6-RUN.md:300-302` (install beside the old build, shadow tick, swap, watch three ticks, roll back). Repair: add `--shadow` to the run verb and handler (and implement the watch/rollback), or remove the shadow step and the promise from the docs.

3. **serious — steps 7 and 8 can print VERIFIED for a row the deal cannot use.** `benchState` keeps only the store row's state, bus presence and cores (`cmd/nova-sprint/setup_workload.go:119-155`); `benchState.up()` is only `hooked && row == "up"` (`cmd/nova-sprint/setup_workload.go:126`), and `SmokeReady` only adds a core check (`cmd/nova-sprint/setup_workload.go:239-258`). The deal's own test is `r.State == "up" && !r.Held && Hooked(r)` (`steps/view.go:311-315`). A held row, or a row unhooked in the sprint but still present on the bus with state `up`, therefore passes steps 7 and 8, and `up` prints `UP DONE` while the run can deal nothing. This breaks `docs/SETUP.md:82-85` ("It checks every worker can be dealt to"). Repair: carry `row.Held` and `row.Session` in `benchState` and use `steps.View.Open` (or the same predicate) in `up()`/`SmokeReady`.

4. **serious — the service-version probe checks a hard-coded path, not the binary the service record runs.** `checkServiceVersion` always probes `<home>/.local/bin/nova-sprint` locally or `~/.local/bin/nova-sprint` over ssh (`cmd/nova-sprint/setup_production.go:851-886`), while `selectRunService` accepts any absolute `argv[0]` (`cmd/nova-sprint/setup_production.go:1005`) and `publicService` accepts an absolute dashboard binary (`cmd/nova-sprint/setup_production.go:1181-1202`). The test fixture itself uses `/remote/bin/nova-sprint` (`cmd/nova-sprint/setup_production_test.go:192-198`). Thus step 4 or 5 can verify a different binary from the one the unit will exec: a stale `~/.local/bin/nova-sprint` can make a correct `/opt/...` service fail, or a correct `~/.local/bin` can make a wrong `/opt/...` service pass. Repair: probe the service's own `Argv[0]` on the target (or require bare `nova-sprint` and refuse absolute service binaries).

5. **serious — the TEST gate promised by the card contract is never run.** `docs/CARD-CONTRACT.md:54-55` says "The TEST line names a test absent at base, and the system runs it red at base and green at head as a gate", and `verbs/brief.go:15-17` says "the gates TEST names are the worker's to run". But the take receipt has no TEST field (`edge/worker/verbs.go:42-54`), `Job` and `JobRecord` carry only `DoneWhen` (`edge/worker/worker.go:71-78`, `edge/worker/worker.go:92-98`), and `rebaseAndTestStep_internal` loops only `rec.DoneWhen` (`edge/worker/runner.go:676`). TEST is parsed and linted, never executed. A card can therefore be accepted although the test it was supposed to add was never made to fail at base or pass at head. Repair: carry TEST through the deal into the job and run it (red at base, green at head), or stop promising it in CARD-CONTRACT and the playbooks.

6. **minor — the coordinator playbook's `drop --expect` does not exist.** `docs/PLAYBOOK-COORDINATOR.md:300-303` tells the coordinator to run `nova-sprint drop ... --expect <dependant-ids>` to fail the dependants the dry run printed. The drop row has only `reason= waive=* relink=* dry-run` (`verbs/table_coordinate.go:29`), and `dropChoices` implements only waive/relink (`verbs/judge.go:209-225`). Reproduced:

   ```
   $ go run ./cmd/nova-sprint drop somecard --reason why --expect other
   SPRINT REFUSED drop: unknown flag --expect: nova-sprint help drop
   exit status 2
   ```

   Repair: add `--expect <ids>` and check it against the dry-run dependant set, or remove it from the playbook.

7. **minor — SETUP says nothing past the store is typed by the human, but two steps ask the human for input.** `docs/SETUP.md:71` says "Nothing in this section is typed by the human." Step 6 still reads a hidden key at the terminal (`docs/SETUP.md:73-74`, `verbs/setup_runtime.go:489-518`), and step 7 stops with `ASK the human which forge account ...` until the commit identity is set (`verbs/setup_runtime.go:155-164`, matching `docs/PLAYBOOK-COORDINATOR.md:219-224`). A cold AI following SETUP is told no input is needed and then stops. Repair: name the two human inputs (the key and the identity) in section 5, or move them out of its "nothing typed" claim.

8. **minor — `check`'s nova-tools check reads only nova-config and its fix is not the one SETUP names.** `docs/SETUP.md:65-67` says check reads the installed tools' versions (`nova-bus -h`, `nova-fleet -h`, and the rest) and names "the `nova-fleet up` that brings them up". `InstalledTools` looks up only `nova-config` and reads its build info (`verbs/tools_version.go:19-29`), `doctor.ToolsVersion` forwards that (`cmd/nova-sprint/edges.go:97-100`), and the fix is `nova-fleet install --tag <pin> <machine>` (`verbs/tools_version.go:54-55`, `verbs/check.go:113-133`). An old `nova-bus` or `nova-fleet` beside a new `nova-config` is never seen, and the printed fix differs from the contract. Repair: read each tool's reported version (or correct the doc) and print the documented fix.

9. **minor — the docs and Setup model exclude the run machine from worker benches; the code and its test include it.** `docs/SETUP.md:75` says "Every machine with the bench role, except the one the sprint runs on", `docs/LAYER-6-RUN.md:291` repeats it, and `tla/Setup.tla:25-27` says `world[7]` is "every bench but the run machine". But `WorkerBenches` explicitly includes "the run machine too when it is one" (`verbs/setup_runtime.go:50-60`), and `TestWorkerBenchesIncludeTheRunMachineWhenItIsABench` asserts inclusion (`verbs/setup_runtime_test.go:209-214`). Repair: decide which rule is the contract and update docs, model, code and test together; today `up` installs and counts a worker the docs and model say is not there.

## Promise gaps

- `docs/SETUP.md:73`: "`--services none` is a valid answer." The code treats none as no step-6 key, but every worker service still names `DEEPSEEK_API_KEY` and `INCEPTION_API_KEY` (`cmd/nova-sprint/setup_workload.go:22-26,52-59`), which no step saves or grants.
- `docs/SETUP.md:32-35`: "The key goes into nova-secrets; the AI makes the seats, the grants, the worker rows and the service rows." Step 6 saves the key only (`verbs/setup_runtime.go:489-518`); step 7 grants only the sprint credential (`verbs/setup_runtime.go:520-555`); no provider-key grant or API-rate service row is created.
- `docs/SETUP.md:71`: "Nothing in this section is typed by the human." Step 6 opens a hidden terminal prompt (`docs/SETUP.md:73-74`), and step 7 waits for the human's commit identity (`verbs/setup_runtime.go:155-164`).
- `docs/SETUP.md:75`: "Every machine with the bench role, except the one the sprint runs on, gets a worker." `WorkerBenches` includes the run machine when it has the bench role (`verbs/setup_runtime.go:50-60`), and the test demands that (`verbs/setup_runtime_test.go:209-214`).
- `docs/SETUP.md:65-67`: check reads "the installed tools' versions (`nova-bus -h`, `nova-fleet -h`, and the rest)" and names "`nova-fleet up`". It reads only `nova-config`'s build info (`verbs/tools_version.go:19-29`) and names `nova-fleet install --tag ...` (`verbs/tools_version.go:54-55`).
- `docs/CARD-CONTRACT.md:54-55`: "The TEST line ... the system runs it red at base and green at head as a gate." The worker job carries only `DoneWhen` (`edge/worker/worker.go:71-78,92-98`) and runs only `rec.DoneWhen` (`edge/worker/runner.go:676`); TEST is lint only.
- `docs/PLAYBOOK-COORDINATOR.md:364-366`: `up --version` "installs the build beside the old, runs one shadow tick ..., swaps, watches three ticks, and swaps back". `VersionAdoption.Ready` only runs `check` and `run --shadow` and `Apply` refuses to install (`verbs/setup_runtime_version.go:35-68,121-128`); `run --shadow` is not even a run flag (`verbs/table_coordinate.go:51`).
- `docs/PLAYBOOK-COORDINATOR.md:300-303`: drop accepts "`--expect <dependant-ids>`". The drop verb's flags and `dropChoices` have no `expect` (`verbs/table_coordinate.go:29`, `verbs/judge.go:209-225`).

## Model vs code

- `tla/Setup.tla:25-27` binds `world[7]`/`world[8]` to "every worker bench (every bench but the run machine) on the bus and up in the store". The implementation's `WorkerBenches` includes the run machine (`verbs/setup_runtime.go:50-60`), and `TestWorkerBenchesIncludeTheRunMachineWhenItIsABench` fixes that inclusion (`verbs/setup_runtime_test.go:209-214`). `TestWorkloadStepsMatchTheSetupModel` only uses two non-run benches (`cmd/nova-sprint/setup_workload_model_test.go:22-26`), so the binding does not see the divergence.
- `tla/Setup.tla:26-28` says `world[8]` is "every worker bench with cores, so dealable once the sprint starts". The code's `SmokeReady` checks only bus presence, row state and cores (`cmd/nova-sprint/setup_workload.go:239-258`), not the deal's `!Held && Hooked(row)` (`steps/view.go:311-315`), so the model's "dealable" is stronger than the code it is bound to.
- `tla/Setup.tla:36-39` makes step 6 need `{"services","key"}` unconditionally; the code accepts `services=none` with no key (`verbs/setup_runtime.go:365-378,489-518`). `tla/SetupChoices.tla:15-18` adds the conditional obligation, and `tla/SetupTrace-MAPPING.md:53-57` records the base model gap; the unextended `Setup.tla` trace still does not cover the supported `none` branch.

## Simplifications

- The public-dashboard contract is written three times: `publicService` (`cmd/nova-sprint/setup_production.go:1181-1202`), `validPublicTarget` (`verbs/setup_runtime.go:591-605`), and the `/usr/bin/env nova-sprint` shape test in `isDashboardService` (`cmd/nova-sprint/setup_production.go:1178-1180`). They can drift independently.
- The run-service contract is written twice: `selectRunService` (`cmd/nova-sprint/setup_production.go:1000-1031`) and `validateRunService` (`cmd/nova-sprint/setup_production.go:692-713`) accept and reject overlapping facts with different rules.
- The dashboard "ask or act" decision exists in both `productionSetup.Verify` (`cmd/nova-sprint/setup_production.go:132-183`) and `RuntimeSetup.dashboardCheck` (`verbs/setup_runtime.go:271-300`); only one is exercised by most tests, so a change to one can silently diverge.
- `workerArgv(machine, _ string)` takes a second parameter (the run machine) and discards it (`cmd/nova-sprint/setup_workload.go:32-39`), so the call site at `:59` reads as if the run machine affects the worker argv.
- `safeSSHName` recompiles its regexp on every call (`cmd/nova-sprint/setup_production.go:928-930`); a package-level regexp is enough.
- `workerHarness` is a single fixed `"opencode"` (`cmd/nova-sprint/setup_workload.go:18-20`), while the provider routes are also fixed (`:41-46`) even though setup asks the human to choose services; both are special-cased setup policy rather than data from the ledger.

## Missing tests

- No test that `up --services none` still installs worker units whose keys exist; `TestInstallWorkersWritesOneRecordPerBenchAndOnePlay` asserts the fixed `DEEPSEEK_API_KEY,INCEPTION_API_KEY` keys (`cmd/nova-sprint/setup_workload_test.go:99-111`) but never checks that those keys were saved or granted. `TestServicesNoneVerifies` only exercises `ServicesReady` (`cmd/nova-sprint/setup_workload_test.go:216-224`).
- `TestSmokeReadyNeedsEveryWorkerDealable` (`cmd/nova-sprint/setup_workload_test.go:194-214`) varies presence and cores only; it never marks a row `Held` or clears its `Session`, so defect 3 is uncovered.
- The version-adoption test fakes the command and only looks at the last argument being `--shadow` (`verbs/setup_runtime_version_test.go:13-47`); it never runs the real parser, so it misses defect 2.
- `checkServiceVersion` is tested against `~/.local/bin/nova-sprint` (`cmd/nova-sprint/setup_production_test.go:397-453`); there is no test with an absolute service `argv[0]` such as `/remote/bin/nova-sprint`, so defect 4 is uncovered.
- There is no test for `drop --expect`; the playbook's flag is not in the table, so the test suite cannot find the documented command.
- There is no test that a brief's TEST line is carried to the worker and run red at base/green at head; the worker tests only cover `DoneWhen` (`edge/worker/worker_test.go:258`, `edge/worker/functional_test.go:139`).
- The Setup-model binding test (`cmd/nova-sprint/setup_workload_model_test.go:21-56`) uses two benches and a non-bench run machine, so it cannot catch the run-machine divergence or the held/unhooked row.
- Observed red, outside the list's unit tests: `go test ./cmd/... ./verbs/...` fails `cmd/nova-worker` `TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot` with `no report reached nova-sprint`. That test is functional (real git, real processes, a fake `nova-sprint` on PATH), so it is not a unit-test violation; see the question below.

## Questions for the coordinator

- Is `--services none` supposed to leave workers without provider routes, or should worker `--providers`/`--keys` follow the services choice? Today none still demands DeepSeek and Inception.
- Are `DEEPSEEK_API_KEY` and `INCEPTION_API_KEY` expected to be pre-granted to every bench seat by nova-tools before `up` runs? If yes, SETUP should say so; if no, setup needs a grant step before the worker play.
- Which is the contract for a run machine that also has the bench role: a worker (the code and its test today) or no worker (SETUP and `Setup.tla` today)?
- Is `up --version` meant to be implemented now, or is the shadow tick still deferred (LESSONS 6.19 says MISSING)? If deferred, the coordinator playbook should not present it as the release path.
- Should TEST be a real gate in the worker (the card contract says so), or is the contract wording ahead of the implementation?
- Is `cmd/nova-worker`'s red `TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot` a stale breaker (its comment claims a wall/slot defect, but `edge/worker/serve.go:95` now wires `OSReserveWall{UseSandbox: true, ReserveBin: ..., SandboxBin: ...}` and `Procs`), or does it still expose a real shipped-worker wiring defect? I could not decide cold from the layer-7 files alone.
