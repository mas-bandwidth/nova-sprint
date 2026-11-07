# Layer 7 (the box) cold review: v2r-l7-heavy2

Reviewer: rowan (Claude Opus 5.5), cold, 2026-11-07, at 43d849e2 (`v2-stitch`).
Scope: docs/SETUP.md, docs/PLAYBOOK-COORDINATOR.md, docs/PLAYBOOK-WORKER.md,
docs/CORRECT-USE.md, docs/CARD-CONTRACT.md, every verb's `help`, and the installer and
services (cmd/nova-sprint/services.go, setup_*.go, up_wrapper.go, internal.go; the `up` walk
in verbs/setup*.go; tla/Setup*.tla).

What was run:

```
$ go build ./... && go vet ./...
BUILD OK
VET OK
$ go test -count=1 -skip Functional ./cmd/nova-sprint/
ok  	github.com/mas-bandwidth/nova-sprint/cmd/nova-sprint	0.746s
$ go test -count=1 -skip Functional ./verbs/
ok  	github.com/mas-bandwidth/nova-sprint/verbs	0.522s
$ go test ./docs/
?   	github.com/mas-bandwidth/nova-sprint/docs	[no test files]
```

The unit tests (run with `-skip Functional`, as Makefile:26 and docs/TESTING.md:20 say) use no
real time, sockets or services: the one real Redis (`fixtureRedis`, setup_bus_test.go:108-139) is
only reached from `TestFunctionalSetupBusRedis`. A first plain `go test ./cmd/nova-sprint/`
(without `-skip Functional`) ran the functional end-to-end test against its own throwaway
`initdb` cluster on this Mac (PostgreSQL 14.24); its result is defect 6 and 7 below. It wrote
into the tree (defect 6); that write was reverted before this report was committed.

Every verb's help was run from a binary built at this head in an empty environment
(`env -i HOME=... PATH=/usr/bin:/bin ./nova-sprint help <verb>`): all 40 exit 0.

## Verdict

The layer is not sound yet: the documented box promises behaviours the code does not have, and
two of the setup paths a cold AI is sent down cannot complete. The single worst thing is that
`nova-sprint up --version <v>`, the playbook's only way to adopt a new build (PLAYBOOK-COORDINATOR
section 9.1), can never verify: its readiness probe runs `nova-sprint run --shadow`, and `run`
has no `--shadow` flag, so step 2 always fails; the unit test fakes that command as a success.
Close behind: the worker service records demand two provider keys that `up` never stores or
grants (and one of which `up` cannot store at all), any name can take the coordinator role when
none is hooked, a saved setup choice silently overrides a new flag, and a cold `next` on a fresh
machine never names the fleet or `up`. The docs also contradict the code and each other in
several places a stranger would act on (`drop --expect`, drop of a held card, the run machine
as a worker).

## Defects

1. **blocker: `up --version` always fails at step 2.**
   verbs/setup_runtime_version.go:65 runs `v.secretCommand(ctx, "run", "--shadow")`, but `run`'s
   only flag is `listen=` (verbs/table_coordinate.go:51). Reproduced at this head:
   ```
   $ env -i HOME=.. PATH=/usr/bin:/bin ./nova-sprint run --shadow; echo "exit $?"
   SPRINT REFUSED run: unknown flag --shadow: nova-sprint help run
   exit 2
   ```
   `Ready` returns that error (setup_runtime_version.go:65-67), `RuntimeSetup.Verify` step 2
   returns it (verbs/setup_runtime.go:120-121), and `storeAct` too (setup_runtime.go:421-424), so
   every `up --version <v>` stops at step 2 once the asked release is installed. Breaks
   PLAYBOOK-COORDINATOR.md:364-366 ("installs the build beside the old, runs one shadow tick
   against the live store, swaps, watches three ticks, and swaps back"); the code installs
   nothing, swaps nothing and watches nothing (setup_runtime_version.go:14-16, 121-128). Repair:
   implement the shadow tick as a real `run` mode (or drop it from `Ready`) and rewrite section
   9.1 to what `up --version` does.

2. **serious: worker services require provider keys that `up` never stores or grants.**
   cmd/nova-sprint/setup_workload.go:24-26 and :54 put `DEEPSEEK_API_KEY` and
   `INCEPTION_API_KEY` in every worker record's `Keys`, whatever the human chose at step 6. Step
   6 accepts only `deepseek` and `openrouter` (verbs/setup_runtime_model_probe.go:15-18,
   setup_runtime.go:365-373), so the Inception (Mercury) key can never be stored by `up`; and
   step 7 grants a bench seat only `NOVA_SPRINT_PG_PASSWORD`
   (verbs/setup_runtime_role_bootstrap.go:100-110, called from setup_runtime.go:550-554), never a
   provider key. With `--services none` (SETUP.md:73 "a valid answer") or `--services openrouter`,
   each worker unit is asked to open keys its seat does not hold. Breaks SETUP.md:32-36 ("the AI
   makes the seats, the grants ... The human never types a second thing"). Repair: derive the
   worker record's keys and providers from the step 6 choice and grant each chosen key to every
   bench seat in step 7. (Whether `nova-secrets exec --only` refuses or skips a missing key
   decides whether this is a hang at step 7 or a worker with dead routes; see Questions.)

3. **serious: any name can hook in as coordinator when none is hooked.**
   verbs/hook.go:72-74 refuses the coordinator role only when `coordinatorOf(snap)` finds a
   currently hooked coordinator (verbs/hooked.go:70-77); the coordinator `up` saved
   (`Choices.Save("coordinator")`, setup_runtime.go:384-385, `SaveCoordinator`) is never read. A
   worker can `unhook` itself and `hook --role coordinator` in the window after the real
   coordinator unhooks (which PLAYBOOK-COORDINATOR.md:31 tells it to do after every lost session).
   Breaks PLAYBOOK-COORDINATOR.md:13-14 ("the one name `up` recorded") and :40-41 ("it refuses the
   coordinator role to any name but the one `up` recorded"). Repair: in `hookAs`, refuse role
   coordinator unless the name equals the saved `coordinator` choice.

4. **serious: a saved setup choice silently wins over a new flag, and one refusal's fix loops.**
   verbs/setup_runtime.go:185-186 returns the saved answer and ignores `input` whenever one is
   found, for `services`, `dashboard` and `run-on` alike. `up --services openrouter` after a
   saved `none` prints `UP 6/8 services: none selected: VERIFIED`; and `up --key openrouter` is
   refused with fix `nova-sprint up --services <names>` (setup_runtime.go:339-341), a flag that
   cannot change the saved value, so the refusal's own fix never makes it true. Breaks
   CORRECT-USE.md:15-20 (each refusal names "the command that makes it true") and the "no default
   taken quietly" bar. Repair: refuse a flag that differs from the saved answer, naming the verb
   that changes it (or let a flag on a re-run replace the answer, logged).

5. **serious: `next` on a fresh machine names a credential that does not exist yet.**
   verbs/next.go:109-116: when the snapshot fails with an `OperationError` that is not exit 2
   (the missing credential, verbs/result.go:55), `unready` returns it as guidance before it asks
   `Doctor.Fleet` or `Doctor.Schema`. On a machine with no fleet and no store:
   ```
   $ env -i HOME=.. PATH=/usr/bin:/bin ./nova-sprint
   NEXT this process has no sprint credential
   next: run under nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...
   ```
   That password is made by `up` step 2, and `up` itself refuses to run with it in the
   environment (cmd/nova-sprint/up_wrapper.go:58-61). Breaks CORRECT-USE.md:29-33 and :138-146
   ("1. Is nova-tools' fleet up? If not: the nova-fleet command. 2. Is the store at the schema
   ...? If not: `up`") and the SETUP.md:49-52 gate (a cold AI from the docs and the tool's help).
   Repair: in `unready`, ask the fleet and the schema first and name `up` when the credential is
   missing because nothing is set up.

6. **serious: the functional end-to-end test writes into the source tree, and the committed
   transcript is a failing run.** cmd/nova-sprint/e2e_fixtures_functional_test.go:58-70 writes
   `../../run/testdata/e2e-transcript.txt` on every run, pass or fail (deferred at
   e2e_functional_test.go:54). At this head the committed file is a 23-line failure from build
   `dev+f3f08b6729bb`:
   ```
   $ git show HEAD:run/testdata/e2e-transcript.txt | grep -n "exit 1\|set_option"
   21:exit 1
   22:nova-sprint: schema creation failed: reading the admin's grant on nova_sprint: ERROR: column "set_option" does not exist (SQLSTATE 42703)
   $ git diff 3fd14ed1 69280b9f --stat -- run/testdata/e2e-transcript.txt
    run/testdata/e2e-transcript.txt | 1343 +--------------------------------------
   ```
   3fd14ed1 committed the PASS transcript, 4c8cab89 restored it once after "a local functional run
   overwrote" it, and merge 69280b9f overwrote it again. The evidence for the end-to-end claim is
   gone from the tree, and any functional run dirties a worker's checkout (this review had to
   revert it). Repair: write the transcript under `t.TempDir()` (or only with an explicit
   `-update` flag on success) and restore the PASS transcript.

7. **serious: `up` needs PostgreSQL 16 or later and does not say so.**
   verbs/setup_runtime_role.go:221 reads `pg_auth_members.set_option` and :226 runs
   `GRANT ... WITH SET TRUE`, both PostgreSQL 16+. On 14 the step fails with a raw SQL error and no
   fix (the transcript in defect 6 and this review's run on 14.24). Nothing in SETUP.md, the
   prerequisites step (verbs/setup_runtime_prerequisites.go:19) or `check` names a minimum server
   version. Repair: check `server_version_num` in step 1 or 2 and refuse below 160000, naming the
   upgrade (as verbs/pg_backup.go:118 already does for pg_dump).

8. **serious: a multi-card `drop`, `replace` or `hold` applies some cards and reports none.**
   verbs/judge.go:179-195 and verbs/moves.go:83-98 commit one op per argument; when argument
   *i* is refused, the function returns `"", err`, dropping the `applied at seq` lines of the
   cards already committed. `drop a b --reason x` with `b` refused fails `a` and its whole
   dependant closure while printing only `SPRINT REFUSED drop: ...` for `b`. Breaks the one-line
   refusal contract's meaning (CORRECT-USE.md:15-20: a refusal says what is not true, not what
   was done). Repair: either plan every card into one op, or print the applied lines in the
   refusal's detail.

9. **serious: the box contradicts itself on `drop`.**
   (a) `help drop` lists "refuses when: a live consumer: its holder's release"
   (verbs/table_coordinate.go:31), and PLAYBOOK-COORDINATOR.md:244-246 says `replace` of a held
   card "is refused (`producer <id> has live consumer ...`)"; the code releases the live
   consumers in the same op (steps/request.go:204-262, `releasing`), as CORRECT-USE.md:229-232
   and PLAYBOOK-COORDINATOR.md:304-305 say. (b) PLAYBOOK-COORDINATOR.md:300-303 tells the
   coordinator to pass `--expect <dependant-ids>` "to fail them as printed"; `drop` has no such
   flag (table_coordinate.go:29):
   ```
   $ ./nova-sprint drop x --reason y --expect a; echo "exit $?"
   SPRINT REFUSED drop: unknown flag --expect: nova-sprint help drop
   exit 2
   ```
   and a plain `drop` fails the transitive dependant closure (steps/request.go:220-238,
   `NeedFailed{InThisOp: true}`) with no confirmation. Repair: add the `--expect` guard (refuse
   when the failed closure differs from the list) and fix the help table and section 4.

10. **serious: the run machine gets a worker when it is a bench.**
    verbs/setup_runtime.go:50-61 (`WorkerBenches`) includes every bench, the run machine too
    (pinned by `TestWorkerBenchesIncludeTheRunMachineWhenItIsABench`,
    verbs/setup_runtime_test.go:207-215), and the run machine is the control machine `up` runs on
    (cmd/nova-sprint/setup_production.go:136-137 refuses any other). SETUP.md:75 says "Every
    machine with the bench role, except the one the sprint runs on, gets a worker", and
    PLAYBOOK-COORDINATOR.md:394-395 says "never run a build or test suite on the coordinator's
    machine". Repair: decide which is the rule and make SETUP.md, the playbook and Setup.tla say
    it (see Model vs code 1).

11. **minor: every `up` scans the whole event log many times and grows the policy table.**
    `Choices.Get` folds every event from seq 0 in pages of 1000 (verbs/choices.go:40-48) and is
    called from 17 places in the walk; `findSetupContinuation` does the same
    (cmd/nova-sprint/setup_production.go:515-564) up to three times per `up`; and every `up`
    writes one new permanent `data` policy version for its continuation
    (setup_production.go:484), on an append-only table (ENFORCED L1-S6). `up` is the documented
    restart path for a down run or worker (PLAYBOOK-COORDINATOR.md:169-172, :316-317), so its cost
    grows without bound with the sprint's history. Repair: keep setup answers and the continuation
    in a small keyed record read in one query, not by folding the log.

12. **minor: `up`'s output cap is 83% spent on the playbooks.** cmd/nova-sprint/up_wrapper.go:20
    caps each phase's output at 64 KiB (65,536 bytes) and on overflow discards all of it with exit
    2 (:143-145), after the walk's effects. The coordinator hook inside the walk prints both
    playbooks (verbs/setup.go:266; docs/playbooks.go:18), 53,291 bytes at this head
    (`wc -c docs/PLAYBOOK-*.md`). About 12 KB more of playbook, or a long step-7 refusal on a
    large fleet, makes the first `up` exit 2 with no output. No test pins the margin. Repair:
    print the playbook path or `help playbook coordinator` from `up` instead of the text, or test
    the real output against the cap.

13. **minor: a dev build's version refusal names an impossible fix.**
    setup_production.go:834-836 never installs when `BinaryVersion` is not a release, then
    `serviceVersionRefusal` (:918-920) says `nova-fleet install --repo <org/repo> --tag
    dev+43d849e29d0e <machine>`, a tag that cannot exist. Repair: for a dev build, name `make
    install` on that machine.

14. **minor: 15 verbs have no refusal table.** `help` promises "what refuses it and the fix"
    (verbs/help.go:12-15) and CORRECT-USE.md:15 "every verb runs its prerequisite table"; at this
    head `help` prints no "refuses when" block for queue, unhold, relink, replace, the five policy
    verbs, worker, dash, stop and start (the `needs` field is empty in verbs/table.go:129-130 and
    verbs/table_coordinate.go:24-25, 32-45, 54-61), though e.g. `runDrop` refuses a missing reason
    (verbs/judge.go:163-165). Repair: fill `needs` for each, from the refusals the handler returns.

15. **minor: CARD-CONTRACT.md is stale in two places.** (a) CARD-CONTRACT.md:160-164 says the CLA
    warning lists "`<row> <row@nova-sprint.local>`" and "kin commit as their own git identity";
    verbs/admit_cla.go:52-54 names the one sprint identity, as CARD-CONTRACT.md:68-73 itself says.
    (b) CARD-CONTRACT.md:6-7 says "`check` lints a brief directory before a sprint starts"; `check`
    takes no argument (verbs/table.go:88, `max: 0`). Repair: delete or rewrite both sentences.

16. **nit: printed lines the playbooks quote do not match the code.** `ADD DRY RUN cards=<n>`
    (PLAYBOOK-COORDINATOR.md:192) is `(dry run)` (verbs/add.go:38); `POLICY ACTIVE v<n>
    answers-changed=<k>` (:358) is `changes=<k>` (verbs/policy.go:63); SETUP.md:65-67 says the pin
    refusal names `nova-fleet up`, `check` names `nova-fleet install --tag <pin> <machine>`
    (verbs/table.go:90). Repair: quote the code's lines.

17. **nit: `upHook` slices without checking.** verbs/setup.go:266
    `text[:strings.LastIndex(text, "next: ")]` panics if the hook text ever lacks `next: `.
    Repair: cut with `strings.Cut`/`LastIndex >= 0`.

18. **nit:** cmd/nova-sprint/setup_production.go:929 compiles a regexp on every `safeSSHName`
    call; hoist it beside :298-299.

## Promise gaps

- PLAYBOOK-COORDINATOR.md:364-366: "`nova-sprint up --version <v>`: it installs the build beside
  the old, runs one shadow tick against the live store, swaps, watches three ticks, and swaps
  back on any overrun, crash or refused tick". Code: verbs/setup_runtime_version.go:14-16
  "It installs nothing"; `Apply` only refuses (:121-128); the shadow is an unknown flag (defect 1).
- PLAYBOOK-COORDINATOR.md:40-41: "it refuses the coordinator role to any name but the one `up`
  recorded". Code: verbs/hook.go:72 checks only a currently hooked coordinator (defect 3).
- SETUP.md:32-36: "The key goes into nova-secrets; the AI makes the seats, the grants, the worker
  rows and the service rows." Code: no provider key is ever granted to a bench seat
  (setup_runtime_role_bootstrap.go:105, sprint password only); worker keys are fixed
  (setup_workload.go:24-26) (defect 2).
- SETUP.md:27-30: services "DeepSeek, Mercury, OpenCode, OpenRouter, and the like". Code: step 6
  knows deepseek and openrouter only (setup_runtime_model_probe.go:15-18), and an unknown name
  is refused as `unsupported or duplicate service` with no list and no fix
  (setup_runtime.go:373).
- SETUP.md:75: "Every machine with the bench role, except the one the sprint runs on, gets a
  worker". Code: setup_runtime.go:50-61 includes it (defect 10).
- CORRECT-USE.md:138-141: "What next looks at, in order: 1. Is nova-tools' fleet up? ... 2. Is
  the store at the schema ...". Code: next.go:114-116 answers the credential first (defect 5).
- CORRECT-USE.md:42: step 2 `up` "refuses until `check` passes". Code: `runUp`
  (verbs/setup.go:110-129) never runs `check`; its step 1 is `Prerequisites.Tools`.
- CORRECT-USE.md:86-88: "`take`, `finish`, `read`, `release`, `run` and every other verb that acts
  as a name refuse until that name is hooked in on the bus". Code: `run` is a cold verb
  (verbs/table_coordinate.go:51, no role) and acts as no hooked name.
- PLAYBOOK-COORDINATOR.md:300-303: `--expect <dependant-ids>`. Code: no such flag (defect 9).
- PLAYBOOK-COORDINATOR.md:244-246 and `help drop`: a held card's drop/replace "is refused". Code:
  released in the same op (steps/request.go:204-262) (defect 9).
- PLAYBOOK-WORKER.md:259: "Every refusal is one line, exit 1". Code: a bad flag is exit 2
  (`SPRINT REFUSED drop: unknown flag --expect`, above), as `help`'s own index says
  (verbs/help.go:43); the playbook should say exit 2 also covers bad usage.
- CARD-CONTRACT.md:6-7 and :160-164: see defect 15.

## Model vs code

1. tla/Setup.tla:25-26 says world[7] is "every worker bench (every bench but the run machine)";
   the code includes the run machine (verbs/setup_runtime.go:50-61). The binding test
   `TestWorkloadStepsMatchTheSetupModel` never gives the run machine the bench role
   (cmd/nova-sprint/setup_workload_model_test.go:25, studio is `"roles":"services"`), so the
   divergence passes. Repair: pick the rule, then add the run-machine-is-a-bench state to the
   binding test.
2. tla/Setup.tla:36-39 gives step 7 no question (`OTHER -> {}`), and `EventuallyDone`
   (Setup.tla:99) is checked on that; the code's step 7 first needs a human answer, the commit
   identity, that no act can make true (verbs/setup_runtime.go:134-136, 155-165). The model
   therefore claims liveness for a walk that, in the code, waits on a person at step 7. Repair:
   add `identity` to `Questions` with `Need(7) = {"identity"}`.
3. tla/SetupChoices.tla:6-7 and :17: "None discharges the key obligation", step 6 needs no key
   with `servicesChoice = "none"`, and step 7's `Do` is unconditional. In the code, step 7's
   worker records open fixed provider keys whatever step 6 chose (setup_workload.go:54), so
   "none" does not discharge them (defect 2). The model does not cover the step 6 -> step 7 key
   dependency at all.
4. The `--version` adoption inside step 2 (setup_runtime.go:116-121, 418-428) is not in any
   Setup model; its one unit test fakes the shadow command (defect 1).

## Simplifications

- Setup answers and continuations stored as `data` policy versions and found by folding the
  whole event log (verbs/choices.go:40-48; setup_production.go:484, 515-564) are bigger and
  slower than a keyed row read once; they also put setup state in the policy table, which
  `policy show --list` and version numbering share.
- `VersionAdoption` (verbs/setup_runtime_version.go, 128 lines) checks a version on PATH, runs
  `check`, and runs a shadow that does not exist; since `Apply` only refuses, the whole step-2
  branch could be one version compare plus the refusal naming `nova-fleet install`, until a real
  shadow exists.
- `workerHarness`, `workerProviderKeys` and `workerProviders` (setup_workload.go:20-46) are three
  hand-kept constants that must agree with each other and with step 6's
  `modelProbeProviders`; one table keyed by service would remove the drift in defect 2.
- `ensureServiceVersion` and `ensurePublicVersion` (setup_production.go:801-844) and
  `versionsReady` (setup_workload.go:183-209) are three version checks of the same binary on
  machines; one "this machine runs this version" helper over a machine list would do.

## Missing tests

- A test that the version adoption's commands are real verbs and flags of this binary (would
  have caught defect 1); verbs/setup_runtime_version_test.go:22-25 tests a fake that accepts
  anything.
- `hook --role coordinator` by a name other than the saved coordinator while none is hooked
  (defect 3).
- `up --services <other>` and `up --dash <other>` after a saved answer (defect 4).
- `next` with no credential, no fleet and no store (defect 5): CORRECT-USE's order is untested.
- A worker record's keys against the step 6 choice, and the grants step 7 makes for them
  (defect 2).
- `drop a b` / `hold a b` with the second refused (defect 8).
- `drop --dry-run` on a producer with dependants, then the guarded drop (defect 9), and `drop`
  of a producer with a live consumer asserting the release (the help table says refuse).
- `up`'s real phase-2 output (both playbooks, eight step lines, `UP DONE`) under
  `handoffOutputLimit` (defect 12).
- The Setup binding test with the run machine as a bench (Model vs code 1).
- A server-version refusal below PostgreSQL 16 (defect 7).

## Questions for the coordinator

1. Does `nova-secrets exec --only K1,K2` refuse when the seat lacks one of the keys, or run
   without it? Defect 2 is a step-7 hang in the first case and workers with dead routes in the
   second; I could not read nova-tools from this card.
2. A friend row on a machine that is neither the run machine nor a bench: PLAYBOOK-WORKER.md:82-85
   requires `nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD` for every verb,
   but `up` grants that password only to the run seat and bench seats
   (verbs/setup_runtime_inventory.go:83-117). Who grants it to a friend's seat, and is it meant to
   be written down?
3. `--internal-write-continuation` checks that `os.Getppid()` holds the setup lease
   (cmd/nova-sprint/internal.go:120), but it is started as `nova-secrets exec ... -- <binary>`
   (setup_production.go:357-359). That holds only if `nova-secrets exec` replaces itself (execve)
   rather than forking a child. Is that nova-tools' stated contract?
4. Is PostgreSQL 18 (or at least 16) guaranteed by `nova-fleet up` on every fleet, so defect 7 is
   only a test-bench issue, or is a distro PostgreSQL in scope?
5. Is the run machine a worker bench when it has the role (the code, 2026-11-07) or not
   (SETUP.md, Setup.tla, and the playbook's "never build on the coordinator's machine")?
