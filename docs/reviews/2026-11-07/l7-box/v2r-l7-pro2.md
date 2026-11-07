# Layer 7 box review — v2r-l7-pro2 (cold, by the box a new AI unpacks)

Scope reviewed: `docs/SETUP.md`, `docs/PLAYBOOK-COORDINATOR.md`, `docs/PLAYBOOK-WORKER.md`,
`docs/CORRECT-USE.md`, `docs/CARD-CONTRACT.md`, the verb help tables
(`nova-sprint help <verb>` for every verb), and the installer/services:
`cmd/nova-sprint/services.go`, `setup_production.go`, `setup_workload.go`,
`setup_installer.go`, `up_wrapper.go`, `verbs/check*.go`, `verbs/tools_version.go`,
`verbs/setup_runtime*.go`, `verbs/secret.go`, `verbs/hook.go`, `verbs/identity.go`,
with their tests and the `tla/Setup*.tla` models. `go build ./...` and `go vet ./...`
are clean; `go test ./cmd/nova-sprint/ ./verbs/` passes (0.035s/0.079s). The functional
tests skip without real Postgres/Redis/sshd (`initdb required`, `requires isolated Redis...`);
`TestFunctionalPublicDashboardRealHTTPAndShutdown` is the one test that binds a real
loopback socket (it passed). The unit tests are hermetic.

## Verdict

The layer is sound in its mechanics — the two-phase handoff, the verb table/`next`
walkthrough, the hidden-prompt key path, the SCRAM password proof, and the `check`
doctor are all carefully built, build clean, and keep most of their documented promises.
The single worst thing is that the box's own centrepiece promise is not kept: SETUP.md
promises `up` configures "DeepSeek, Mercury, OpenCode, OpenRouter, and the like" as
services, but the code accepts only `deepseek` and `openrouter` and refuses every other
name, while the worker service is hard-wired to open the *inception* (Mercury) key it can
never collect — so a cold AI following the docs cannot provision the documented services,
and a deepseek-only setup still stalls on an un-collectable INCEPTION key. There is also a
worker-benches divergence that contradicts both SETUP.md and the TLA+ model (the run
machine is included as a worker when it carries the bench role).

## Defects

1. **serious — the documented service catalog is not implemented; the worker requires a key the flow never collects.**
   `verbs/setup_runtime_model_probe.go:15-18` defines `modelProbeProviders` with only
   `deepseek` and `openrouter`; `verbs/setup_runtime.go:372` refuses any other service name
   ("unsupported or duplicate service"). SETUP.md:28 promises "DeepSeek, Mercury, OpenCode,
   OpenRouter, and the like", so `--services inception`/`--services opencode` (Mercury/
   OpenCode) are refused even though the docs say the AI offers them. Worse, the worker
   service unconditionally opens `INCEPTION_API_KEY` and `DEEPSEEK_API_KEY`
   (`cmd/nova-sprint/setup_workload.go:24-26,46`), and `WorkersReady` fails until every
   worker's probe passes, so a setup that selects only `deepseek` still requires an inception
   key that step 6 never collects from the human. Repair: add mercury/opencode providers to
   `modelProbeProviders` and derive the worker's key set from the chosen services (or
   document that only deepseek/openrouter are services and drop the unconditional inception key).

2. **serious — the run machine is included as a worker bench, contradicting both SETUP.md and the TLA+ model.**
   `verbs/setup_runtime.go:53-61` (`WorkerBenches`) returns every machine with the `bench`
   role, the run machine included (its comment says so). SETUP.md:75-77 promises "Every
   machine with the bench role, **except the one the sprint runs on**, gets a worker", and
   `tla/Setup.tla:25-27` models `world[7]` as "every worker bench (every bench but the run
   machine)". The binding test `TestWorkloadStepsMatchTheSetupModel` only ever uses non-run
   benches (`cmd/nova-sprint/setup_workload_model_test.go:25,34`), so the run-machine-as-bench
   case is unexercised. Repair: exclude the run machine in `WorkerBenches` (or update SETUP.md
   and Setup.tla in lockstep and add the run-machine-as-bench case to the model test).

3. **minor — check's tools-pin fix names a different command than SETUP.md promises.**
   SETUP.md:67 says check "refuses below the pin, naming the `nova-fleet up` that brings them
   up"; the code names `nova-fleet install --tag <pin> <machine>` instead
   (`verbs/tools_version.go:55`, `verbs/check.go:131`). Also SETUP.md:65-66 says check "reads
   the installed tools' versions (`nova-bus -h`, `nova-fleet -h`, and the rest)", but
   `InstalledTools` reads only `nova-config`'s build info (`verbs/tools_version.go:19-29`).
   Repair: make SETUP.md say `nova-fleet install --tag` and "reads nova-config's version stamp",
   or widen the read to the other tools and keep the promised command.

4. **minor — on an unprovable host, up re-sets the sprint password on every run, not only when it differs.**
   `verbs/setup_runtime_role_password.go:43-74` returns `Unprovable` when pg_hba trusts the
   connection and pg_authid is unreadable; `ensureSprintPassword`
   (`verbs/setup_runtime_role_password.go:103-134`) then always sets a fresh SCRAM verifier with
   a new random salt on every `up`. SETUP.md:88-90 promises the password is "proven ... or, where
   the database trusts the connection without a password, by its stored verifier; **when it is
   not the sprint's credential, up sets it**" — i.e. it should set only when it is not the
   credential, which cannot be determined here, so it over-writes a correct password each time
   (harmless functionally, but not what the doc promises and it churns the stored verifier).
   Repair: document that an unprovable host is re-set every run, or skip the set when the stored
   verifier is readable and matches.

## Promise gaps

- SETUP.md:28 — "DeepSeek, Mercury, OpenCode, OpenRouter, and the like" as API-rate services
  `up` sets up. Code: only `deepseek` and `openrouter` are accepted
  (`verbs/setup_runtime_model_probe.go:15-18`, `verbs/setup_runtime.go:372`). (Defect 1.)
- SETUP.md:75-77 — "Every machine with the bench role, except the one the sprint runs on, gets
  a worker." Code: `WorkerBenches` includes the run machine when it has the bench role
  (`verbs/setup_runtime.go:53-61`). (Defect 2.)
- SETUP.md:67 — check names "the `nova-fleet up`" to bring tools up to the pin. Code: names
  `nova-fleet install --tag <pin> <machine>` (`verbs/tools_version.go:55`). (Defect 3.)
- SETUP.md:88-90 — "when it is not the sprint's credential, up sets it". Code: on an
  unprovable host it always sets it (`verbs/setup_runtime_role_password.go:126-128`).
  (Defect 4.)
- SETUP.md:79-81 — the worker step "verifies when every worker is on the bus and up in the
  sprint; otherwise it names each machine that is not and the last line of its worker's log."
  Code keeps this (`setup_workload.go:164-233`): `WorkersReady` returns a `WorkloadNotReady`
  naming each bench and its last log line. Held.
- SETUP.md:73-74 — "A chosen service's key is pasted once at a hidden prompt and proven by one
  model-list request." Code keeps this (`verbs/secret.go:29-67`, `verbs/setup_runtime_model_probe.go:29-62`):
  hidden `/dev/tty` read, key on stdin only, proven by a 2xx model-list/balance probe. Held.
- CORRECT-USE "The one rule" / "Unknown outcomes never leak" — three exit codes, a refusal names
  its fix, never a silent zero. Code: `checkReport.result` (`verbs/check.go:41-54`) exits 1 on
  red, 2 when a red could not be read, and prints a `next:` line only when green. Held.
- CORRECT-USE "Finish is checked, not trusted" — finish refuses a non-40-hex head, a head not on
  the branch at origin, and a non-holder. Match: `verbs/check.go` does not cover this, but the
  finish verb and its `needs` table (`verbs/verbs.go`) and the store's card-machine refusals do;
  help `finish` lists exactly those refusals. Held (by the verb table and store, not by a
  dedicated test in this package — see Missing tests).
- PLAYBOOK-WORKER "On a sprint with no commit identity it refuses" and take refuses until set.
  Code: `verbs/hook.go:32-34`, `verbs/take.go:84`, `verbs/identity.go:61-64`. Held.
- SETUP.md:83-85 (first deal step 8) — every worker "on the bus, up, and with its core count in
  nova-config (a bench without one is held at width 0)". Code: `SmokeReady`
  (`setup_workload.go:239-259`) uses `roster.NoCores` and refuses with the `nova-config machine
  set ... --cores` fix. Held.

## Model vs code

- `tla/Setup.tla:25-27` models `world[7]` as "every worker bench (every bench but the run
  machine)". Code `WorkerBenches` includes the run machine when it is a bench
  (`verbs/setup_runtime.go:53-61`). Divergence (Defect 2). The model test
  `TestWorkloadStepsMatchTheSetupModel` (`cmd/nova-sprint/setup_workload_model_test.go:21-56`)
  never places the run machine also on a bench, so the divergence is invisible to the harness.
- `tla/Setup.tla:35` models `Questions == {"coordinator","dashboard","services","key"}` and
  `Need(4) == {}`. The code also asks/records "run-on" at step 4 (`verbs/setup_runtime.go:193-224`)
  and "dashboard" at step 5. The model abstracts run-on into the default host rather than a
  question; this is a benign abstraction, not a behavior mismatch (step 4 still resolves a host
  and verifies the chooser lock).
- `tla/SetupHandoff.tla` (pre-released, early-record, stale-record, wrong-seat, reuse-skips
  configs) matches the two-phase wrapper: `up_wrapper.go:49-94` runs phase 1 under the admin
  secret, records the continuation, and phase 2 validates digest/epoch/actor
  (`verbs/setup_runtime.go:388-437`, `setup_production.go:301-564`). The unit tests
  `TestFunctionalUpChildUsesPrivateChannelAndSeparateSecrets` skip without `NOVA_FUNCTIONAL=1`;
  the handoff logic is otherwise exercised by `setup_production_test.go`/`setup_runtime_test.go`.

## Simplifications

- `modelProbeProviders` (`verbs/setup_runtime_model_probe.go:15-18`) hard-codes two providers and
  duplicates endpoints that nova-tools' `internal/doctor/check_providers.go` already owns (its
  own comment says so). The set is tiny and closed, which is fine for the engine but is the root
  of Defect 1 and is not configurable.
- `cmd/nova-sprint/setup_workload.go:20` hard-codes `workerHarness = "opencode"` for every bench
  worker ("nova-config's machines carry no harness field, so this one default"). This is a real
  simplification versus PLAYBOOK-WORKER's per-harness recipes; a bench that must run a different
  harness cannot be configured without editing code. Glenn's "tight engine, no special cases"
  ideal is served, but it silently narrows the worker's capability.
- The public-dashboard service is validated by hand-rolled argv inspection
  (`setup_production.go:1181-1203`, `verbs/setup_runtime.go:591-606`) rather than a shared
  "dashboard service" model; it is correct and tight but duplicated in two files (a second
  definition of what a pull-only dashboard looks like).

## Missing tests

- No test places the run machine on a bench to check `WorkerBenches` exclusion
  (`setup_workload_model_test.go:25,34` use only `amd-1`/`amd-2`); the Setup.tla world[7]
  promise is therefore unbound for exactly the case it excludes. (Defect 2.)
- No test asserts that every documented service (deepseek, mercury, opencode, openrouter) is
  selectable; `setup_runtime_key_test.go` and `setup_runtime_model_probe_test.go` cover only the
  two implemented providers. (Defect 1.)
- No test verifies the worker's hard-coded key set against the chosen `--services`; a
  deepseek-only setup still demands INCEPTION_API_KEY, and no test catches that.
  (Defect 1.)
- No test pins `check`'s tools-pin fix text to SETUP.md's promised command (the doc/behavior
  mismatch in Defect 3 is untested).
- The full cold walk is only covered by `TestFunctionalEndToEnd`/`TestFunctionalLiveHistoryCheck`
  which skip without `initdb`/Redis; nothing in `cmd/nova-sprint` runs the eight steps end to end
  against a fake store without external services.

## Questions for the coordinator

- Are Mercury/OpenCode supposed to be `up` services at all, or did SETUP.md's list outrun the
  implementation? If they are, should the worker's provider-key set be derived from the chosen
  services instead of the hard-coded DEEPSEEK+INCEPTION pair?
- Was including the run machine in `WorkerBenches` (when it carries the bench role) a deliberate
  ruling, and should SETUP.md and Setup.tla be updated to match it?
- Is checking only `nova-config`'s module stamp (not `nova-bus -h`/`nova-fleet -h`) the intended
  pin check, or should tools_version.go read all the tools?
- On an unprovable host (pg_hba trust, pg_authid unreadable), is re-setting the password every
  run acceptable, or should up carry a stored fingerprint to avoid churn?
