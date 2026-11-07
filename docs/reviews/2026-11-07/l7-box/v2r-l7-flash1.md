# Layer 7 box: independent cold review

Base: `43d849e29d0e4568ba6a1348a076a59639938`. This is a bounded source review, not an end-to-end setup or a release gate.

## Verdict

Not sound as a cold-start box: the worst issue is that version adoption invokes an unsupported `run --shadow`, so a matching installed release cannot complete the documented verification path. The conflicting gate and test-host instructions also make the box unreliable as the sole guide to correct use.

## Defects

1. **Serious — version adoption invokes a nonexistent shadow flag.** `verbs/setup_runtime_version.go:59-68` runs `nova-sprint run --shadow` after the installed version matches and check succeeds, but `verbs/table_coordinate.go:51-53` registers only `--listen`, and `verbs/flags.go:57` rejects unknown flags. Consequently the documented `up --version` verification cannot succeed at this base; implement and wire the intended shadow operation, or remove the call and replace the advertised rollout contract with the actual supported procedure. The fake in `verbs/setup_runtime_version_test.go:17-25` returns success for that command without executing the parser, so it masks the mismatch.
2. **Serious — selected services do not determine worker credentials.** `docs/SETUP.md:72` makes `--services none` valid, yet `cmd/nova-sprint/setup_workload.go:24` always returns both provider key names and `:52-59` always places them on every worker service; `verbs/setup_runtime_services.go:16-17` says the unit opens the listed keys. A fresh fleet choosing none or just one provider can thus have workers configured to open absent, ungranted credentials and fail to reach step 7; derive each worker's keys and provider routes from the selected, provisioned services and test the none and single-provider cases through service creation. This conclusion is from source; no live installation was attempted.
3. **Serious — admission promises executable gate validation that it does not perform.** `docs/CARD-CONTRACT.md:49-55` promises base execution, rejection of gates that remain red, negative controls and a test red at base/green at head; `verbs/admit_card.go:130-150` checks gate syntax and roster/retired mechanics, while `docs/CARD-CONTRACT.md:133-138` explicitly says nothing runs at add. A coordinator relying on the earlier promise can admit a vacuous or permanently failing gate and learn too late; implement the promised evidence-backed checks or amend the contract and workflow to state exactly which checks are deferred and who must perform them.
4. **Minor — worker playbook asks for a host and lock that the receipt and admission do not provide.** `docs/PLAYBOOK-WORKER.md:155-158` tells a worker to use the host named by `take` and the lock named in the brief; `verbs/take.go:25-54` and `:181-193` contain no test-host field or printed host, and `docs/CARD-CONTRACT.md:139-142` calls named hosts and locks retired mechanics. A cold worker cannot consistently follow both instructions and may stall or select a host on its own; replace the obsolete playbook text with the actual test execution and resource allocation contract.
5. **Minor — contributor-agreement guidance names the wrong commit identities.** `docs/CARD-CONTRACT.md:162-168` says benches use row-local identities and kin use their own, whereas `docs/PLAYBOOK-WORKER.md:162-169` requires the single sprint identity for all commits. A coordinator following the agreement warning may obtain signatures for the wrong accounts while the actual commit identity remains unsigned; update the admission section to match the sprint identity and the warning actually emitted.

6. **Minor — coordinator playbook gives obsolete correction and drop commands.** `docs/PLAYBOOK-COORDINATOR.md:293-294` instructs `edit` although the registered verb table (`verbs/table.go:45-46`, `verbs/table_coordinate.go:3-75`) has no edit, and `docs/PLAYBOOK-COORDINATOR.md:300-303` specifies `--expect` and `--relink <other>` whereas drop accepts `--relink <dependant>=<card>` and no `--expect` (`verbs/table_coordinate.go:29-31`). A coordinator following these recipes hits usage errors at triage; update both to the supported replace/drop workflow.

## Promise gaps

- “it installs the build beside the old, runs one shadow tick ... swaps, watches three ticks, and swaps back” (`docs/PLAYBOOK-COORDINATOR.md:364-366`): `VersionAdoption.Apply` refuses and directs a separate fleet install (`verbs/setup_runtime_version.go:121-127`), while its readiness path calls an unsupported shadow flag (`:62-66`, `verbs/table_coordinate.go:51`). The claimed swap/watch/rollback is not established by this path.
- “The human never types a second thing for that service” (`docs/SETUP.md:35-36`): the unconditional worker key list above is independent of the answer and can require unselected credentials. Whether a particular deployment fails depends on nova-secrets and fleet state; this review did not run them.
- “Each done-when is run at the base before the deal” (`docs/CARD-CONTRACT.md:49`): admission only parses commands (`verbs/admit_card.go:138-142`), and the same document says nothing is run at add (`docs/CARD-CONTRACT.md:137-138`).
- “on the host `take` named” (`docs/PLAYBOOK-WORKER.md:155-156`): the receipt and text have no such host (`verbs/take.go:25-54`, `:181-193`).

## Model vs code

`tla/Setup.tla:23-31` abstracts step 7 as worker presence and records; `:35-39` treats services and key as abstract answers without provider identities. It cannot establish that each generated unit requests exactly the selected services' credentials (`cmd/nova-sprint/setup_workload.go:24-25`, `:52-59`). This is a coverage limitation, not a claimed TLA invariant violation. The complete model suite was not reviewed or run.

## Simplifications

The unconditional provider list and separately hard-coded route string (`cmd/nova-sprint/setup_workload.go:22-25`, `:41-46`) duplicate the service selection's source of truth; derive both from one selected-service description. No refactor was performed.

## Missing tests

- Test adoption through the actual CLI parser rather than a command fake that accepts `run --shadow` (`verbs/setup_runtime_version_test.go:17-25`); cover the promised failure/rollback behavior if retained.
- Exercise `none`, DeepSeek-only and Inception-only selection all the way to generated worker service keys and routes, asserting absent providers are not requested; current configuration unconditionally appends both (`cmd/nova-sprint/setup_workload.go:52-59`).
- Test the shipped worker playbook against the take receipt and admitted brief vocabulary, including test host and locks (`docs/PLAYBOOK-WORKER.md:155-158`, `verbs/take.go:181-193`, `docs/CARD-CONTRACT.md:139-142`).
- Add a contract test or explicit manual gate for the base-red and negative-control promises (`docs/CARD-CONTRACT.md:49-55`, `:137-138`).

## Questions for the coordinator

- Is the gate execution promise intended for admission, a separate human-reviewed preflight, or a later automated gate? The same contract currently says both execution and no execution at add.
- What is the current authorized source of test-host and slot selection for a friend? The take receipt does not name one.
- Coverage: read the five named box documents and ENFORCED, the complete help table source, services, up wrapper, installer and check source, Makefile/placebin, selected setup and admission paths, related installer, version, handoff and workload-model tests, and Setup, SetupChoices, SetupRun and SetupHandoff models. This is not a complete read of all setup paths, tests, model witnesses, or every verb implementation; help was reviewed from its generating table rather than invoked per verb. No Go commands, live services, hostile inputs, or tests were run by this reviewer. Parent-run common validation on the fixed base, Linux hetzner2 under nova-reserve: `go build -p 4 ./...` and `go vet -p 4 ./...` passed; `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` exited 1 because policy TestDeadlineOnTheClock failed (expected -1 got 0; expected 3 refusals got 2); run, verbs, cmd/nova-sprint and dash passed. Root help passed; per-verb captures were pending in the supplied receipt. Functional tests, services, fuzz and TLC were not run. Receipt and logs: `/Volumes/nova/ai/stella/working/deal-1794067989302/VALIDATION.txt` and `/Volumes/nova/ai/stella/working/deal-1794067989302/evidence/`. This is parent evidence, not an independent reproduction.

### Final coordinator validation receipt

This postscript supersedes earlier statements that shared validation or help capture was pending. On the unchanged source base `43d849e29d0e4568ba6a1348a076a59259639938`, the parent ran Linux checks under a shared reservation with `GOMAXPROCS=4`: `go build -p 4 ./...` and `go vet -p 4 ./...` passed. `go test -p 4 -parallel 4 -count=1 -timeout 600s -skip Functional ./policy/... ./run/... ./verbs/... ./cmd/nova-sprint/... ./dash/...` failed in policy's `TestDeadlineOnTheClock` (`policy/round5_test.go:26` and `:27`: expected version -1/three refusals, observed version 0/two refusals); run, verbs, command and dash packages passed. All 44 existing CLI verb/subverb/playbook help queries exited zero. These are parent-run results, not independent reviewer executions. Functional tests, live service setup, fuzzing and TLC were not run; source-review coverage limits above remain. The source archive SHA256 is `73b811e91414713188e0c24dbb6fdafa8580cd476d3e69104751b7c2e573788c`; the 53-file shared evidence manifest SHA256 is `49e12541e3756608ae7205d68b95f85105d510ca9b5e4c26ce003477209e3e3e`.
