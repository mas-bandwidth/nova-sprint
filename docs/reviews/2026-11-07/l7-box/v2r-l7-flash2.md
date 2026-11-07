# Layer 7 box cold review — Stella

Base: `43d849e29d0e4568ba6a1348a076a59259639938`. This is a bounded, report-only source review, not a release approval.

## Verdict

Not sound against the published setup contract: the worst issue is that choosing provider services does not configure their worker-seat grants, while worker records unconditionally request two specific provider keys. A fresh setup therefore has no source-backed path from the selected keys to correctly provisioned worker services. This is a source conclusion; no live installation or reproduction was attempted.

## Defects

1. **Serious — Provider selection does not flow into worker grants and records.** `verbs/setup_runtime.go:346` verifies each selected key, but `cmd/nova-sprint/setup_workload.go:81` makes services readiness unconditional and its install a no-op; `verbs/setup_runtime.go:550` grants only via the sprint-role bootstrap, whose `verbs/setup_runtime_role_bootstrap.go:100` grants the database password. Meanwhile `cmd/nova-sprint/setup_workload.go:24` and `:52` hard-code DeepSeek and Inception keys into every worker record, irrespective of selection. With `--services none`, or a selection without both keys, the record still requests both; with another selected provider it does not request that provider's key. This breaks the promised automatic seats and grants and can leave worker units unable to obtain the requested secrets; derive worker keys/routes and grants from the durable selected services and verify them before declaring readiness.
2. **Serious — Version adoption calls a nonexistent shadow flag.** `verbs/setup_runtime_version.go:65` invokes `run --shadow` after the installed version and check pass, but `verbs/table_coordinate.go:51` defines `run` with only `--listen`; consequently a matching release cannot pass that adoption step in the real CLI. The fake command in `verbs/setup_runtime_version_test.go:17` accepts the unsupported command, hiding the integration failure. Implement and test the actual shadow operation or remove the unsupported adoption path and correct its contract.
3. **Serious — The tools pin checks only one binary.** `verbs/tools_version.go:19` reads build information only from `nova-config`, and `cmd/nova-sprint/edges.go:99` uses that result for `check`; `verbs/check.go:129` then reports the whole nova-tools installation at the pin. A mixed installation can have a current `nova-config` and an older `nova-bus` or `nova-fleet` yet receive that green version line. Inspect each required installed binary and report/refuse the mismatched or unknown one.
4. **Minor — The run host is included among worker benches despite the setup contract.** `docs/SETUP.md:75` excludes the machine running the sprint, but `verbs/setup_runtime.go:50` selects every machine with the bench role and `cmd/nova-sprint/setup_workload.go:97` passes all of them to installation. A run host with the bench role gets a worker, contrary to the stated contract; either implement the exclusion consistently in installation and readiness or explicitly reconcile the contract with the later design decision.

5. **Minor — The coordinator is given nonexistent correction commands.** `docs/PLAYBOOK-COORDINATOR.md:293` directs the coordinator to `edit`, which is absent from the verb table (`verbs/table.go:42`, `verbs/table_coordinate.go:3`); `docs/PLAYBOOK-COORDINATOR.md:300` also directs `drop --expect`, which is not among `drop`'s flags (`verbs/table_coordinate.go:29`). A cold coordinator following either path is refused instead of completing triage; update the playbook to the supported replacement and drop workflow, and test its example commands against the parser.

## Promise gaps

- `docs/SETUP.md:32`: “one API key per service, pasted once ... everything past that point is automatic.” The step saves/probes keys but the worker grant path and fixed records do not carry the selected services through (defect 1).
- `docs/SETUP.md:65`: “check reads the installed tools' versions ... and refuses below the pin.” Only `nova-config`'s stamped module version is read (defect 2); the implementation also names `nova-fleet install`, not the documented `nova-fleet up` (`verbs/tools_version.go:55`).
- `docs/PLAYBOOK-COORDINATOR.md:364` promises up installs, swaps and rolls back the new build, while `verbs/setup_runtime_version.go:121` refuses and asks for a fleet install; the shadow flag itself is unsupported (defect 2).
- `docs/CARD-CONTRACT.md:49` promises base execution and negative-control lint, but its own admission section says nothing runs at add (`docs/CARD-CONTRACT.md:137`), and `verbs/admit_card.go:130` checks gate syntax rather than execution or negative controls. Reconcile the contract and the enforcement before calling those gates proven.
- The two release-gate descriptions conflict: `docs/SETUP.md:49` requires two cold fresh-fleet walks before v2 is done, whereas `docs/README.md:101` moves those full gates to point releases and requires one real dogfood run for 2.0.0. Reconcile the user-facing gate with the current decision before treating either as authoritative.

## Model vs code

`tla/Setup.tla:23` describes worker benches as every bench except the run machine, while `verbs/setup_runtime.go:50` explicitly includes the run machine. The generic `world[7]` Boolean cannot by itself prove the selected inventory or the actual key grants. `tla/SetupChoices.tla:15` correctly abstracts `none` as requiring no key answer, but does not represent the unconditional provider keys in worker service records; it therefore cannot establish the end-to-end none case.

## Simplifications

The fixed provider key list and separate fixed route string (`cmd/nova-sprint/setup_workload.go:24`, `:46`) duplicate the provider choice and can drift. A single selected-provider catalog feeding probes, grants, routes and service records would remove that special case; this is a suggestion, not a proposed code change.

## Missing tests

- Exercise a fresh `none`, a single-provider selection, and another supported provider through step 6, grants, worker service construction and readiness; the existing worker construction assertion at `cmd/nova-sprint/setup_workload_test.go:93` fixes both hard-coded keys into its expected record and does not cover those choices.
- Run the real CLI parser as part of version-adoption tests so `run --shadow` cannot be silently accepted by a stub.
- Test the playbook command examples against the registered verb/flag table.
- Test mixed binary versions and a missing required binary, not just a single tools-release result, against `check` and setup's prerequisite.
- Test the run host with bench role against the resolved contract, including both installation and readiness. The existing fixture says it is included (`cmd/nova-sprint/setup_workload_test.go:64`), so it is not evidence for the documented exclusion.

## Questions for the coordinator

- Which worker-bench rule and which release-gate wording supersede the other? The source comments suggest the inclusion is intentional, but the setup page says otherwise.
- Coverage is partial: I read the five specified docs, the help table and formatter, and targeted setup, service and check paths, with selected tests and Setup and SetupChoices model excerpts. I did not inspect every related setup source, test or model, nor execute every verb help. I did not run Go, Linux gates, service installation, or a live setup; any separately supplied parent receipt is not my own validation. `docs/ENFORCED.md:1` identifies an enforcement ledger for other layers rather than a layer-7 ledger.

Parent-run shared validation at the fixed base (not a reviewer run): on Linux hetzner2, `go build -p 4 ./...` and `go vet -p 4 ./...` passed. The parent's `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` exited 1: policy `TestDeadlineOnTheClock` failed; run, verbs, cmd and dash selected packages passed. Functional-named tests were excluded and PostgreSQL, functional/load/fuzz opt-ins were disabled; no live services or TLC were run. The root CLI help was captured with exit 0; per-verb capture was pending at this receipt. Receipt: `/Volumes/nova/ai/stella/working/deal-1794067989302/VALIDATION.txt`; logs are in its adjacent `evidence/` directory.

### Final coordinator validation receipt

This postscript supersedes earlier statements that shared validation or help capture was pending. On the unchanged source base `43d849e29d0e4568ba6a1348a076a59259639938`, the parent ran Linux checks under a shared reservation with `GOMAXPROCS=4`: `go build -p 4 ./...` and `go vet -p 4 ./...` passed. `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` failed in policy's `TestDeadlineOnTheClock` (`policy/round5_test.go:26` and `:27`: expected version -1/three refusals, observed version 0/two refusals); run, verbs, command and dash packages passed. All 44 existing CLI verb/subverb/playbook help queries exited zero. These are parent-run results, not independent reviewer executions. Functional tests, live service setup, fuzzing and TLC were not run; source-review coverage limits above remain. The source archive SHA256 is `73b811e91414713188e0c24dbb6fdafa8580cd476d3e69104751b7c2e573788c`; the 53-file shared evidence manifest SHA256 is `49e12541e3756608ae7205d68b95f85105d510ca9b5e4c26ce003477209e3e3e`.
