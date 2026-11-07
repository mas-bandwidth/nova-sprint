# Layer 7 box review — v2r-l7-pro1 (2026-11-07)

Reviewed cold, box scope. The box a new AI unpacks: `docs/SETUP.md`,
`docs/PLAYBOOK-COORDINATOR.md`, `docs/PLAYBOOK-WORKER.md`,
`docs/CORRECT-USE.md`, `docs/CARD-CONTRACT.md`, `docs/ENFORCED.md`, every
verb's help text (`go run ./cmd/nova-sprint help <verb>`), and the
installer/services (`cmd/nova-sprint/services.go`, the up walk, check, dash,
worker, run).

Commands run:

```
go build ./...
go vet ./...
go test -count=1 -race -skip Functional ./cmd/nova-sprint/... ./verbs/...
go run ./cmd/nova-sprint help          # index, and up/check tables
go run ./cmd/nova-sprint               # no args -> next
```

All green (build, vet, unit+race pass). Unit tests use no real time, sockets
or services: the only test that starts real `redis-server`/`age-keygen`/
`nova-secrets` is `TestFunctionalSetupBusRedis`
(`cmd/nova-sprint/setup_bus_test.go:65-93`), which is named `Functional` (so
`-skip Functional` drops it) and self-skips unless `NOVA_FUNCTIONAL_BUS=1`;
the rest of `setup_bus_test.go` injects a fake `run` and never touches a host.

## Verdict

The box is sound and unusually well-tested: the verb prerequisite tables, the
`SPRINT REFUSED <verb>: <why>: <fix>` one-line refusal form, the
hooked-before-acting gates, the one-commit-identity enforcement
(hook/take/finish) and `check`'s line-for-line health output all match their
docs, and the layer's unit tests pass under `-race`. The single worst thing is
that the one big documented capability of this box — the release-adoption flow
(`nova-sprint up --version <v>`: install beside the old, one shadow tick, swap,
watch three ticks, roll back on overrun/crash/refusal) promised in
PLAYBOOK-COORDINATOR §9.1 and LAYER-6-RUN §"Adopting a new build" — is not
implemented: `up --version` only verifies the installed binary's version and
then refuses with a `nova-fleet install` fix, and there is no swap, no watch,
no rollback. It fails safe (a loud refusal, not a silent swap), and LESSONS.md
itself records "the shadow tick and rollback MISSING", so this is a
documented-but-unbuilt promise, not a correctness defect.

## Defects

(ranked, most severe first; all reproducible from the cited lines)

1. **[minor] A documented release-adoption flow is absent; `up --version` only verifies then refuses.**
   PLAYBOOK-COORDINATOR.md:364-366 and LAYER-6-RUN.md:300-303 promise `up
   --version <v>` "installs the build beside the old, runs one shadow tick
   against the live store, swaps, watches three ticks, and swaps back on any
   overrun, crash or refused tick". The code runs the version check, `check`
   and `run --shadow` (`Ready`, verbs/setup_runtime_version.go:62-67) but then
   `Apply` (verbs/setup_runtime_version.go:123-128) always refuses with
   `OperationError{Problem: "nova-sprint <v> is not the one installed here",
   Fix: "nova-fleet install ..."}`; the step that uses it
   (verbs/setup_runtime.go:421-428) returns that refusal. Nothing performs the
   swap, the three-tick watch or the rollback.
   `TestVersionAdoptionApplyNamesNovaFleet`
   (verbs/setup_runtime_version_test.go:52-66) codifies the refusal, so the
   behaviour is intentional; the *promise* is unmet. It fails safe (loud
   refusal, not silent). Repair: implement the swap/watch/rollback behind `up
   --version`, or delete the promise from the two docs (LESSONS.md:170 already
   marks "the shadow tick and rollback MISSING").

2. **[minor] `check` labels ready cards in an unreachable stream "no base head", not "repository unreachable".**
   `verbs/check_width.go:122` counts `heads[c.Stream] == ""` as "streams with
   no base head". But `checkStreams` (`verbs/check_sprint.go:67-72`) leaves
   `heads` unset for a stream whose *admission fails* (repository unreachable,
   forge cannot act) exactly as for one with no base head. So when a repo is
   down, the width line mis-diagnoses the cause (the true cause is already a
   red line above, so this is cosmetic but misleading). Repair: carry the
   stream-admission failure separately from a missing base head in
   `undealt`/`readyCauses`.

3. **[minor] SETUP.md says `check` reads the installed tools' versions via `-h`; the code reads `nova-config`'s build info, and the named fix differs.**
   `docs/SETUP.md:65-67` ("reads the installed tools' versions (`nova-bus
   -h`, `nova-fleet -h`, and the rest) and refuses below the pin, naming the
   `nova-fleet up` that brings them up"). Actual: `verbs/tools_version.go:19-29`
   (`InstalledTools`) reads the module version from `nova-config`'s Go build
   info and never runs `-h`; and the fix the code prints is `toolsFix`
   (`verbs/tools_version.go:55`) = `nova-fleet install --tag <pin> <machine>`,
   not `nova-fleet up`. Functionally equivalent for the pin check, but the doc
   names a mechanism and a fix that do not exist. Repair: update SETUP.md to
   "reads each installed tool's build info, and names the `nova-fleet install
   --tag` that brings them to the pin".

4. **[nit] `check --integrity` silently omits the history line when the store is not at schema.**
   `verbs/check.go:81-87` only runs `Doctor.History` `if current && integrity`;
   when the schema is not current there is no line at all saying the history
   check was skipped — a silent omission in a verb whose contract is "prints
   every line". Repair: emit a `CHECK WARN history: skipped (store not at
   schema)` line.

5. **[nit] `limitedOutput` truncation and the `waitHandoff` timeout branch are untested.**
   `cmd/nova-sprint/up_wrapper.go:33-45` (overflow detection) and the 1s
   continuation-channel timeout (`up_wrapper.go:235-246`, `<-timer.C` branch)
   have no unit test: `TestWaitHandoffCancellation`
   (`up_wrapper_test.go:71-77`) covers only the cancellation branch and
   `TestHandoffReaderCompletesAfterWriterClose` (`:79-101`) only the success
   path; grep finds no `overflow`/`handle notOutputLimit`/`did not close` test.
   Both are failure paths of the two-phase `up` wrapper. Repair: add tests for
   the overflow branch and the timeout.

## Promise gaps

(doc sentence quoted, then what the code does)

- "A new nova-sprint build is adopted whole with `nova-sprint up --version <v>`:
  it installs the build beside the old, runs one shadow tick against the live
  store, swaps, watches three ticks, and swaps back on any overrun, crash or
  refused tick, saying why." (PLAYBOOK-COORDINATOR.md:364-366; LAYER-6-RUN.md:300-303)
  Code: `up --version` runs the version check + `check` + `run --shadow`
  (`Ready`, setup_runtime_version.go:62-67) and then, if the installed version
  is not the one requested, refuses (`Apply`, setup_runtime_version.go:123-128)
  with a `nova-fleet install` fix. No install-beside-old, no swap, no
  three-tick watch, no rollback. (LESSONS.md:170 records it MISSING.)
- "The binary carries the minimum nova-tools version it needs; `nova-sprint
  check` reads the installed tools' versions (`nova-bus -h`, `nova-fleet -h`,
  and the rest) and refuses below the pin, naming the `nova-fleet up` that
  brings them up." (SETUP.md:64-67)
  Code: `check` reads `nova-config`'s build info (`tools_version.go:19-29`),
  not `-h` output; it refuses below the pin but the fix it names is
  `nova-fleet install --tag <pin> <machine>` (`toolsFix`, tools_version.go:55),
  not `nova-fleet up`.
- "Every worker bench ... hooked on the bus and up in the store, each committing
  as <identity>: VERIFIED ... otherwise it names each machine that is not and
  the last line of its worker's log." (SETUP.md:79-81) — kept: `setup_workload.go:164-233`
  lists each down bench and its worker log's last line (sshed). No gap.
- "The database role's password is proven on every up by a SCRAM login, or ...
  by its stored verifier; when it is not the sprint's credential, up sets it."
  (SETUP.md:88-90) — kept: `verbs/setup_runtime_role_password.go:43-58` +
  `ensureSprintPassword` (line 109). No gap.

## Model vs code

The box's own behaviour is modelled in `tla/Setup.tla` (the walk),
`tla/SetupHandoff.tla` (the two-phase continuation), `tla/SetupChoices*.tla`
(choices) and `tla/SetupRun.tla` (the run service), bound to Go by the trace
tests in `verbs/setup*_trace_test.go` and `verbs/setup_handoff_test.go`.
Divergences:

- `docs/ENFORCED.md`'s ledger covers layers 1, 2, 3 and 5 (sections 1-7;
  Summary Counts, ENFORCED.md:179-184, 113 rules). None of the box's own
  guarantees — the verb prerequisite tables, the hook/identity gates, `check`'s
  line-for-line health output, the up steps — is a row in the ledger, so they
  are not counted and there is no `nothing` row that would surface the adoption
  gap of Defect 1. The `Setup*.tla` models exist but are not referenced by
  ENFORCED.md.
- The `-h`/refusal/playbook text (`runHelp`, `verbs/help.go`) is not modelled;
  a help-string drift would not be caught by TLA.

## Simplifications

- The public-dashboard service shape is validated in two places with duplicated
  argv/`/keys/seat logic: `verbs/setup_runtime.go:591-606` (`validPublicTarget`)
  and `cmd/nova-sprint/setup_production.go:1181-1203` (`publicService`). A
  fourth accepted argv shape would have to change in both; this is more
  special-cased than it needs to be. Consider one shared validator.
- `up`'s two-phase handoff (`cmd/nova-sprint/up_wrapper.go`) is elaborate (fd 3
  continuation channel, JSON envelope, 1s timeout, 64KiB output cap). It is
  justified by credential separation, but the `limitedOutput`/`waitHandoff`
  machinery is complexity on top of the store-recorded continuation, and its
  failure branches are untested (Defect 5).
- `runAddress` (`cmd/nova-sprint/setup_production.go:1064-1093`) carries several
  special cases (tailnet IP prefixes, MagicDNS name normalization, DNS fallback
  to the name); all are guarded, but the fallback-to-the-name path means a
  temporarily unresolvable tailnet name silently proceeds to use the name as
  the listener address.

## Missing tests

- No test asserts `up --version`'s swap/watch-three-ticks/rollback (the feature
  is absent; a test would codify either the implementation or the refusal, as
  `TestVersionAdoptionApplyNamesNovaFleet` already does for the refusal).
- No test covers the `limitedOutput` overflow branch or the `waitHandoff` 1s
  timeout (Defect 5).
- No test asserts that `check` with `--integrity` on a not-at-schema store emits
  the skipped-history line (currently it is silent; Defect 4).
- No test distinguishes the "repo unreachable" and "no base head" causes in
  `check`'s width line (Defect 2).
- `TestFunctionalSetupBusRedis` (cmd/nova-sprint/setup_bus_test.go:65) is a
  real-service test living in a non-`_functional_test.go` file; it survives only
  by its `Functional` name and the `NOVA_FUNCTIONAL_BUS` env gate. It is safe
  today, but the naming convention is not uniform.
- No unit test exercises `serveBoardOn`'s shutdown path (covered only by
  `board_functional_test.go`).

## Questions for the coordinator

- Is the `up --version` adoption flow (swap/watch/rollback) deliberately
  deferred to `nova-fleet install` with `up` as a pure verifier, or should the
  PLAYBOOK-COORDINATOR/LAYER-6-RUN text be corrected to say "verify + name the
  install"? LESSONS.md:170 already lists it MISSING.
- In SETUP.md §4, is "reads `-h`" aspirational, or should the code actually read
  each tool's `-h` (the current build-info read is equivalent but different)? And
  should the refused fix name `nova-fleet up` (as the doc says) or
  `nova-fleet install --tag` (as the code prints)?
- For Defect 2, is it acceptable for the width line to fold an unreachable repo
  into "no base head" given the stream red line already names the real cause, or
  should the two be separated?
