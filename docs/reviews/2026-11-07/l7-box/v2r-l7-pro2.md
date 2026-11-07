# Layer 7 (the box a new AI unpacks): cold review, v2r-l7-pro2

Reviewer: row `rowan` (model sonnet), 2026-11-07, at 43d849e2 (branch v2-stitch).
Read: docs/SETUP.md, PLAYBOOK-COORDINATOR.md, PLAYBOOK-WORKER.md, CORRECT-USE.md, CARD-CONTRACT.md,
README.md, ENFORCED.md (skimmed), the `help` text of every verb (run from a binary built at the base),
cmd/nova-sprint/{services,main,open,up_wrapper,setup_production,setup_workload,setup_installer,setup_bus_wiring}.go,
verbs/{setup,setup_runtime*,choices,brief,brief_lint,admit_card,check,next,take}.go, run/notices.go,
cmd/nova-worker, the release workflow and Makefile.
Ran: `go build ./... && go vet ./...` (clean); `go test -count=1 -skip Functional` for `./cmd/nova-sprint/`,
`./verbs/`, `./cmd/nova-worker/`, `./tools/...` (all ok); `./docs/` has no test files. The unit tier in these
packages uses no real time or sockets (the one `net.Listen`/`time.Sleep` is in `TestFunctionalSetupBusRedis`).
One functional test is red here (see Missing tests). Not read in depth: setup_production.go lines 520-780
(service-row selection), the TLA+ Setup*.tla models, ENFORCED.md ledger; the PostgreSQL store was never touched.

## Verdict

The mechanics of `up` (two-phase credential handoff, lease, immutable-ish answers, idempotent walk) are carefully
built, but the box itself is not sound as a cold-start package: the documents a new AI is told to use disagree
with the code in ways that send it to commands that do not exist or cannot work. The single worst thing: a cold
AI that types `nova-sprint` (CORRECT-USE: "A cold AI types the tool's name and is told what to do") on a fresh
fleet is told to re-run under `nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,...`, a secret that `up` has not
yet minted and that `up` itself refuses to run with (verbs/next.go:110-118, cmd/nova-sprint/up_wrapper.go:58-61);
behind it, the coordinator playbook documents a verb (`edit`), a flag (`drop --expect`) and a grouped push
(`NEEDS YOU group=...`) that the code does not have, and README/SETUP never state the environment `up` needs.

Defect count: 1 blocker, 9 serious, 8 minor, 4 nit.

## Defects

### D1 (blocker) A cold `next` on a fresh fleet gives an impossible instruction
- verbs/next.go:110-118 (`unready`): any `OperationError` whose exit is not "failed" becomes guidance. The
  no-credential refusal (`verbs.NoCredential`, cmd/nova-sprint/open.go:33-36) is such an error, and it fires before
  the "the sprint is not set up -> nova-sprint up" branch (next.go:111-114).
- Reproduce (built binary, empty environment):
  `env -i HOME=/tmp PATH=/usr/bin:/bin nsl7` prints
  `NEXT this process has no sprint credential` / `next: run under nova-secrets exec --only NOVA_SPRINT_PG_PASSWORD,NOVA_BUS_PASSWORD -- nova-sprint ...`.
  `check` likewise prints `CHECK RED this process has no sprint credential` and exits 2.
- Why it matters: `NOVA_SPRINT_PG_PASSWORD` is minted by `up` step 2 (setup_runtime_role_bootstrap.go:42-47), and
  `up` refuses when it is set (up_wrapper.go:58-61: "NOVA_PG_ADMIN_PASSWORD and NOVA_SPRINT_PG_PASSWORD must be unset").
  The one entry point CORRECT-USE:29-33 promises ("runs `next`... prints the one thing to do now") dead-ends on
  the gate Glenn named (SETUP.md:46-52: a cold AI from docs/README.md and the tool's own help).
- Repair: in `unready`, when the cause is `NoCredential`, ask `Doctor.Fleet`/`Schema`-free questions first: answer
  "fleet up? then `nova-sprint up --as <you>` (unwrapped; it mints the credential)" and give the wrapped form only
  when a sprint already exists (e.g. when NOVA_SECRETS_* is set and the store answers).

### D2 (serious) Worker service names keys that nothing grants or proves, and ignores the services the human chose
- cmd/nova-sprint/setup_workload.go:24-26,46,54-59: every worker record is created with `Keys` =
  `NOVA_SPRINT_PG_PASSWORD, DEEPSEEK_API_KEY, INCEPTION_API_KEY` (+ `NOVA_BUS_PASSWORD` with a bus user) and the
  provider routes are the constant `deepseek=...,inception=...`.
- `Grants.Grant` has exactly one call site, with `sprintPasswordName` (verbs/setup_runtime_role_bootstrap.go:105;
  `grep -rn '\.Grant(' verbs cmd`). Nothing grants `DEEPSEEK_API_KEY`, `INCEPTION_API_KEY` or `NOVA_BUS_PASSWORD`
  to a bench seat (`SprintSeats`, setup_runtime_inventory.go:83-117, builds seats for the sprint credential only).
- Step 6 can only paste/prove `deepseek` and `openrouter` (verbs/setup_runtime_model_probe.go:15-18); `inception`
  (Mercury) is not selectable, so up never creates or probes `INCEPTION_API_KEY`, yet every worker requires it;
  `--services none` ("a valid answer", SETUP.md:70) and `--services openrouter` change nothing about the workers.
- Why it matters: SETUP.md:32-37 promises "the AI makes the seats, the grants, the worker rows and the service
  rows" for the chosen services. In code the chosen set never reaches step 7. If `nova-secrets exec --only <KEY>`
  fails on a missing key (not verified cold), every worker unit fails its start and step 7 reports only "last log line".
- Repair: derive the worker's `Keys`/`Providers` from the saved `services` answer (and a service->(key,route) table
  beside `modelProbeProviders`), grant exactly those keys (and the bus password) to each bench seat in `workersAct`.

### D3 (serious) Setup answers are write-once and a changed flag is dropped silently
- verbs/setup_runtime.go:176-192 (`answer`): a stored value is returned before the flag is looked at; verbs/choices.go:95-99
  refuses a different value, but `Save` is only reached when nothing is stored (`if !saved`, setup_runtime.go:442,474,504).
  So `nova-sprint up --services deepseek,openrouter` after `--services deepseek`, or `--dash public=bench1` after
  `--dash tailnet`, prints the old answer's lines and exits 0; `--run-on` likewise (setup_runtime.go:194).
- Why it matters: "The human chooses" (SETUP.md:27-31) but there is no documented way to change a choice and the
  tool does not say it ignored the flag (CORRECT-USE: "Never a silent zero").
- Repair: when a flag differs from the stored answer, refuse naming the stored value and the one way to change it
  (or implement the change); never take the stored one quietly.

### D4 (serious) The coordinator playbook names a verb, a flag and a push that do not exist
- `edit`: docs/PLAYBOOK-COORDINATOR.md:293 ("fix the brief with `edit` before another attempt"), :196-197 ("fix it in
  place"); `nova-sprint edit ...` -> `SPRINT REFUSED nova-sprint: unknown verb edit: nova-sprint help`
  (verbs/core_test.go:68 even uses `edit` as the example of an unknown verb), yet docs/LAYER-6-RUN.md:130 tabulates it.
  The playbook's own fallback (section 4, `replace`) works, so the AI is told a step that fails.
- `drop --expect <dependant-ids>`: PLAYBOOK-COORDINATOR.md:301-302. `nova-sprint drop x --reason r --expect y --dry-run`
  -> `SPRINT REFUSED drop: unknown flag --expect: nova-sprint help drop` (verbs/table_coordinate.go:29, flags
  `reason= waive=* relink=* dry-run`). Also `--relink <other>` (playbook) vs `--relink <dependant>=<card>` (help).
- Grouped push: PLAYBOOK-COORDINATOR.md:80-83 ("A group is one message, `NEEDS YOU group=<key> size=<n> ids=...`,
  answered with `--expect <n>`"). run/notices.go:112-117 sets `Notice.Group` and sends one line per judgement
  (`NEEDS YOU id=%s: ...`); `Group` is never read (`grep -n Group run/push.go` shows only the type, push.go:18), and
  nothing in the repo prints `group=<key> size=`. 28 open judgements (the case CORRECT-USE:176-179 cites) mean 28 pushes.
- Message shapes in section 2 are not the shipped ones: `NEEDS YOU judgement=<j> kind=.. about=.. due=..` + a
  `choices:` line (playbook :76-79) vs `NEEDS YOU id=<j>: <question>; run: <cmd>` (notices.go:116); `LANDED ... took=<d> cost=<$>`
  with "nothing to run" (:84) vs `LANDED card= stream= head=; run: nova-sprint show <id>` (notices.go:39); `STREAM LANDED
  <s> range=<a>...<b> cards=<n> took cost` (:85) vs `STREAM LANDED <s> <n> landed` and an unlisted `STREAM DONE WITH FAILURES` (notices.go:174-187).
- Repair: delete or build `edit`/`--expect`/the group push; make the playbook examples come from the code (D-test below).

### D5 (serious) CORRECT-USE says `up` refuses until `check` passes; `check` cannot pass before `up`
- docs/CORRECT-USE.md:41-42 (chain steps 1-2). `check` includes "the store at this build's schema" and the sprint
  credential as lines of its own (verbs/check.go:62-90, 96-104: `CHECK RED the store is not at this build's schema ...
  nova-sprint up`), so on a fresh fleet it exits non-zero by design (seen: exit 2 with four RED lines), and `up`
  never calls `check` (help up lists no such refusal; up_wrapper.go has no check call).
- Why it matters: the table is the "one rule" a cold AI is told; following it literally loops (check red -> up -> ...)
  or leads the AI to believe `up` is blocked. 
- Repair: say step 1 is `nova-fleet check` (fleet up, tools at pin) and that `nova-sprint check` is the post-`up` doctor;
  or split check's plumbing lines (pre-up) from sprint lines and let only the first set gate `up`.

### D6 (serious) The worker playbook's test-running instructions are v1 relics the admission refuses and `take` never supplies
- docs/PLAYBOOK-WORKER.md:155-158: run tests "on the host `take` named for tests, as one plain `ssh <host> <command>`
  ... Take the bench slot lock the brief names". CORRECT-USE.md:100-101 likewise: `take` prints "where tests may run".
- `takenText` (verbs/take.go:177-191) prints no host, no slot, no lock; `grep -rniE 'tests may run|bench slot|slot lock'`
  finds nothing in verbs/ or edge/worker. And `add` refuses any brief that names a lock or `ssh to <host>`
  (verbs/brief_lint.go:16-18,30-39; CARD-CONTRACT.md "Retired mechanics").
- Why it matters: a friend row (no bench worker) is told to ssh to a host no output names, and to take a lock the
  contract forbids; "never run builds on the coordinator's machine" (PLAYBOOK-WORKER.md:157, COORDINATOR:394) has no
  other instruction behind it.
- Repair: state where a friend row runs tests (its own machine, its own clone) or add the line to `take`; delete the lock sentence.

### D7 (serious) CARD-CONTRACT promises about TEST and gates that the lint does not keep
- `TEST:` is only required to be present and non-empty (verbs/brief.go:49,114-118; `field()` has no `TEST` case and
  nothing stores it); `TEST: none` is accepted (this very card's brief carries it). Not parsed by `edge.ParseGate`, not run
  by `add`, and the worker runs only `DoneWhen` (edge/worker/runner.go:675-686).
  CARD-CONTRACT.md:54-55 ("The TEST line names a test absent at base, and the system runs it red at base and green at
  head as a gate") and COORDINATOR.md:208 ("TEST: required ... run red at base and green at head") are not implemented.
- CARD-CONTRACT.md:49-50 ("Each done-when is run at the base before the deal ... refused at add"), :51-53 ("a gate that
  skips, cannot compile, or excludes by default fails the lint") and :80 ("a card that writes a new TLA+ model is
  admitted at heavy at least") have no code (`grep -rniE 'negative control|stay red|cannot compile'` and
  `grep -rn tla verbs card` find none); the same document says "Nothing is run at add" (:131-133 region, the DONE-WHEN
  bullet), contradicting :49.
- Repair: either build the run-at-base/heavy checks or reword the contract to what `add` proves; give `TEST` a real
  grammar (a gate, or an explicit "none" the contract allows).

### D8 (serious) Workers are installed on the run machine, against the setup doc and the coordinator rule
- docs/SETUP.md:75 ("Every machine with the bench role, except the one the sprint runs on, gets a worker"). Code:
  verbs/setup_runtime.go:50-61 (`WorkerBenches`: "the run machine too when it is one"), used by `InstallWorkers`
  (setup_workload.go:97-110) and `benchStates` (:148). 
- Why it matters: PLAYBOOK-COORDINATOR.md:394-395 ("never run a build or test suite on the coordinator's machine") and
  the README's "the chooser" separation; either the doc or the code is wrong, and the two release notes disagree about who decided (2026-11-07 comment vs SETUP.md).
- Repair: pick one; if the run machine may be a bench, say so in SETUP.md and the coordinator playbook.

### D9 (serious) README/SETUP are not the walkthrough the gate requires
- docs/SETUP.md:3-4 says "this page becomes the walkthrough an AI runs", but it holds no command, flag or
  environment. `up` fails first on `control-seat secrets configuration is incomplete: set NOVA_SECRETS_STORE,
  NOVA_SECRETS_KEY, NOVA_SECRETS_PINS and NOVA_SECRETS_SEAT` (reproduced: `nsl7 up --as me`; setup_production.go:225-236),
  needs a passwordless `NOVA_PG_DSN`, `NOVA_BUS_REDIS`, `NOVA_PG_ADMIN_PASSWORD` for phase 1 (up_wrapper.go:62-76), and the
  seat must equal the machine name (:229) so `up` runs only on the control machine (`--run-on` elsewhere is refused,
  setup_production.go:131-133). None of that is in README.md, SETUP.md, or `help up`.
- docs/README.md:16-18 promises "a section per layer ... with the lines the first try prints"; there is none. It never
  links the two playbooks, and line 90 names `cmd/nova-sprint-dash`, which does not exist (`ls cmd`: nova-sprint, nova-worker).
- Repair: write the numbered walkthrough in SETUP.md from `up`'s real preconditions and first-try output; fix README links.

### D10 (serious) A public dashboard listener has no idle/read/write timeouts and a spoofable-by-IPv6 limiter
- cmd/nova-sprint/services.go:140: `http.Server{Handler, ReadHeaderTimeout: 5s}` only: no IdleTimeout, ReadTimeout or
  WriteTimeout, so idle keep-alives and slow readers hold connections without bound. SETUP.md:22-23 requires a public
  dashboard that "is not taken down by a lot of traffic".
- dash/pull_handler.go:25-38,58-65: 4096 per-address buckets, new addresses refused (429) while the table is full for
  the second it takes to age out; one /64 of IPv6 (or a shared front-proxy address, 60 req/s total) exhausts or
  throttles it for everyone.
- Repair: set IdleTimeout/ReadTimeout/WriteTimeout and MaxHeaderBytes; key the limiter on /64 for IPv6 and honour a
  configured proxy header (or document the single-address limit).

### D11 (minor) `Choices.Get`/`findSetupContinuation` rescan the whole event log on every read
- verbs/choices.go:40-75 reads events from seq 0 to the snapshot's seq on every `Get`; `up` calls it about ten times
  per pass (coordinator, run-on, dashboard x3, services x2, workers, completion) and the coordinator playbook says to
  re-run `up` to restart the run service (COORDINATOR.md:170-172). On a long sprint (README: thousands of cards, a
  one-second tick) this is O(history) x ~10-20 per `up`; setup_production.go:`findSetupContinuation` the same.
- Repair: read the data policies from a snapshot field, or cache one scan per walk.

### D12 (minor) `up` buffers all child output and a >64 KiB child loses its exit code and message
- cmd/nova-sprint/up_wrapper.go:117-119,142-145: stdout/stderr go to a 64 KiB buffer shown only after the child exits
  (no progress for a multi-minute walk or a nova-fleet play), and `if out.overflow || errOut.overflow` returns exit 2 with the
  command line before the real exit code or output is considered, hiding a success or the cause of a failure.
- Repair: stream output (tee to stderr) and report overflow after surfacing the exit status; there is also no deadline
  on either phase (only the caller's context).

### D13 (minor) Fix lines carry a literal `<org/repo>`
- setup_production.go:919 and setup_workload.go:203: `nova-fleet install --repo <org/repo> --tag ...`. The installer
  itself knows the repo (`mas-bandwidth/nova-sprint`, setup_installer.go:24). The rule is "each refusal names its one
  fix" (CARD-CONTRACT "Admission"); this one is not copy-pasteable. After any `up --version`/release the benches need that
  per-bench install by hand (versionsReady, setup_workload.go:181-209) while the run/public machines are installed
  automatically (setup_production.go:855-868); COORDINATOR.md:364-366 says "adopted whole".
- Repair: put the real repo in the fix; say in the playbook that benches need `nova-fleet install` first.

### D14 (minor) `nova-sprint worker` swallows a hostname error that `nova-worker` refuses
- cmd/nova-sprint/services.go:83 `host, _ := os.Hostname()` vs cmd/nova-worker/main.go:46-51 (refuses, asks for `--host`).
  `w.Host` stays "" on failure and is printed into the WORKER PASS receipt (edge/worker/serve.go:72). Also
  `run` records the machine from the first label via `run.ResolveMachine`, while the worker uses the raw hostname.
- Repair: one code path (see Simplifications).

### D15 (minor) `retiredMechanic` refuses prose that merely mentions a retired word
- verbs/brief_lint.go:16-19,30-39 scans every line, including the task prose, for `JOB.md`, `flock`, `lockfile`,
  `winlock`, and `ssh on|to|into <word>`. "No bypass" (CARD-CONTRACT.md:5-6) means a card that edits
  edge/worker/jobmd.go or a lockfile-handling function, or documents `ssh on port 22`, cannot be added.
- Repair: lint only the lines that instruct (the gates and the metadata), or give the prose a quoted-span exemption.

### D16 (minor) `up --version` upgrade path versus bench version equality
- setup_workload.go:183-209 requires every bench to report exactly this binary's version string; a source build
  (`dev+<hash>-dirty`, main.go:116-135) can never match an installed release, so the dogfood run on a working tree cannot
  pass step 7. The doc does not say a tagged release is a precondition of `up` (SETUP.md:61-66 talks only of the tools pin).
- Repair: state it in the walkthrough, or let step 7 accept a stated `--allow-dev`-style override for a dev fleet.

### D17 (minor) Several verbs print no refusal table although "Every verb runs its prerequisite table"
- `help` for version, queue, relink, replace, stop, start, unhold, worker, dash has no `refuses when` section (checked
  by running `nsl7 help <verb>`); `replace` and `stop/start` do refuse (COORDINATOR.md:244-246). 
- Repair: fill the `needs:` rows in verbs/table_coordinate.go for these.

### D18 (minor) The release workflow does not gate on CI and its tag glob is wider than the installer's grammar
- .github/workflows/release.yml:7-9 (`v[0-9]+.[0-9]+.[0-9]+*`) vs `publicRelease` (setup_installer.go:13: strict
  semver with an optional `-pre`). A tag such as `v2.0.4x` builds and publishes a release that `up` then refuses to install.
  The job builds from the tag without a test/lint step or a check that the commit is on main (COORDINATOR.md:367-369 says "from main").
- Repair: reuse one regexp, and add `go vet`/unit test or a `needs` on the CI run for the sha.

### D19 (nit) Stale or misleading references
- CORRECT-USE.md:145 `add`, or `done` (no `done` verb; the next line printed is `stream add`/`add`); card/brief.go:9 cites
  CARD-CONTRACT "Routes" (no such heading); setup_workload.go:181 comment says "step 5's check" for step 7;
  `--harness script` is accepted (edge/worker/harness.go:67) but not in `help worker` (`claude|codex|opencode`); Makefile `test`
  omits `-race` that TESTING.md says CI uses; PLAYBOOK-WORKER.md:20 says the bench harness reads `JOB.md` while CARD-CONTRACT bans the name
  (consistent only if you know the ban is for briefs).

## Promise gaps

1. "A cold AI types the tool's name and is told what to do." (CORRECT-USE.md:31-32) -> on a fresh fleet it is told a
   command that cannot work (D1).
2. "`up` | refuses until `check` passes" (CORRECT-USE.md:42) -> `up` never calls check and check is red until `up` has run (D5).
3. "Every verb runs its prerequisite table before doing anything." (CORRECT-USE.md:15) -> nine verbs print no table (D17).
4. "The brief tells ... where tests may run" via `take` (CORRECT-USE.md:100-101) -> `takenText` prints none (D6).
5. "`nova-sprint check` reads the installed tools' versions (`nova-bus -h`, `nova-fleet -h`, and the rest) and refuses below
   the pin" (SETUP.md:65-67) -> `InstalledTools` reads the build info of one binary, `nova-config`, on the machine `check` runs
   on (verbs/tools_version.go:20-31); the other tools and the benches' tools are never compared with the pin.
6. "The AI makes the seats, the grants, the worker rows and the service rows" for the chosen services (SETUP.md:35-37) -> D2.
7. "Every machine with the bench role, except the one the sprint runs on, gets a worker" (SETUP.md:75) -> D8.
8. "A TEST line ... run red at base and green at head"; "done-when run at the base before the deal"; "a gate that skips ... fails
   the lint"; "a card that writes a TLA+ model is admitted at heavy" (CARD-CONTRACT.md:49-55,80) -> not in code (D7).
9. "A group is one message, `NEEDS YOU group=<key> size=<n> ...`" (COORDINATOR.md:80-82) -> never sent (D4).
10. "Never a silent zero" (CORRECT-USE.md:119) -> a changed `--services/--dash/--run-on` exits 0 having done nothing (D3).
11. "A public dashboard must be robust: it caches, and it is not taken down by a lot of traffic" (SETUP.md:22-23) -> D10.
12. "`up` ... re-verifies each setup step and restarts the run service at the first that fails" (COORDINATOR.md:170-172)
    vs PLAYBOOK-WORKER.md:82-85 "every nova-sprint verb ... run it under `nova-secrets exec ...`": `up` refuses with that
    credential in its environment (up_wrapper.go:58-61). The coordinator playbook never says `up` is the one verb run bare.

## Model vs code

Not exhaustive (I did not run TLC; read the code side and the file list). Observations:
- tla/ has Setup, SetupWalk, SetupChoices*, SetupHandoff*, SetupRun* models with traces (cmd/nova-sprint/*_trace_test.go and
  verbs/setup_*_trace_test.go). The code's `Choices` model (immutable answer, `setup answer already stored`) matches
  SetupChoices*; but the user-facing consequence (D3: a differing flag is neither refused nor reported) sits outside the
  model's alphabet: the Walk/Choices models cannot express "the input differs from the stored answer", so no witness
  would catch it.
- No model covers worker-service content (which keys, which providers) or the seat grants of step 7; D2 is therefore
  invisible to every witness.
- `Setup.tla` treats `check` as no part of the walk; CORRECT-USE's "up refuses until check passes" has no model counterpart (D5).
- I could not confirm from the files read whether the model checks that `Completion` runs only after step 8 is verified at the
  same lease (setup.go:199,234-247 does so in code).

## Simplifications

1. `cmd/nova-worker` and `nova-sprint worker` are two entry points to `worker.Serve`, with different defaults
   (harness `claude` vs the service's `opencode`; `underHome` path resolution only in services.go:86-90; hostname handling
   differs, D14). Keep one (the release ships both: .github/workflows/release.yml builds every `cmd/*`).
2. Two `up` code paths for the version/ssh checks (`checkServiceVersion` over ssh, `lastServiceLogLine` over ssh,
   `versionsReady` via `w.version`) each build their own ssh command line (setup_production.go:~890, setup_workload.go:274);
   one helper would remove three copies of `-o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=yes`.
3. `commitIdentity` in step 7 duplicates `identity` verbs' validity text; `widthAdvice` is a 400-byte constant inside
   `completeSetup` (setup.go:288-289) that restates CORRECT-USE "Keep everybody at width" a third time.
4. Four docs (CORRECT-USE "Built" sections, README, SETUP, LAYER-6) each repeat parts of the `up` contract; the playbook
   notes CORRECT-USE is "older, shorter" (COORDINATOR.md:82-83). One owner per fact would have caught D4/D5.
5. `PLAYBOOK-*.md` embed ~730 lines into every `hook`; the "refusals you will meet" list (WORKER.md:257-292) restates
   strings the code can generate from the same table the help uses.

## Missing tests

1. A test that every `nova-sprint <verb> [--flag]` in docs/PLAYBOOK-*.md, CORRECT-USE.md and SETUP.md exists in the verb table
   with that flag (`docs/` has no test files; `docs.Playbook` is unreferenced by any test). D4's `edit` and `--expect`
   would have failed it.
2. A golden test per push notice in run/notices.go against the formats quoted in the playbooks (DEAL, NUDGE, WITHDRAWN,
   NEEDS YOU, LANDED, STREAM LANDED, EXPIRED, ANSWERED).
3. `next` / `check` / `up` on a fresh fleet (no credential, no store): the command `next` prints must be one `up` accepts (D1, D5).
4. A second `up` with a changed `--services`, `--dash` or `--run-on` after the first answer (D3).
5. `setupWorkload` tests that the worker service record follows the saved services answer, and that each key it names has
   been granted to the bench seat (D2).
6. cmd/nova-worker/coldread2_test.go: all five `TestColdread2Defect*` tests guard with `i >= 0 && ...` or `strings.Contains` on
   text that is no longer in main.go (55 lines now; it has no `forge.New`, no `if once {`, no `secrets-bin`), so they pass vacuously
   and cannot fail; delete them or assert the real wiring.
7. `TestFunctionalBreakShippedWorkerRunsHarnessOutsideAnySlot` is red on this machine at 43d849e2:
   `go test -count=1 -run TestFunctionalBreakShipped -v ./cmd/nova-worker/` ->
   `break_test.go:80: no report reached nova-sprint (exit 0, stderr "", sprint log "unhook --as r1\nhook --as r1 --role worker\nbeat ... --slots-used 0 --slots-total 0 ...\nnext --as r1 --json\nnext --as r1 --json\n")`.
   The log shows `take` never ran and the worker exited 0 with nothing on stderr. I could not isolate the cause cold (host load was
   124/32 cores; `serve.go:98` now builds the wall with `UseSandbox: true`, which this test's PATH stubs do not provide); if
   the test is stale it is a "breaker finding" for a defect already changed, and if the worker really exits 0 silently when it
   declines a ready card, that is a silent failure.
8. `serveBoardOn` (services.go:137-157) has no unit test (shutdown order, poll error vs shutdown error: a shutdown error
   replaces the poll error at :148-151) and `services.Worker`/`underHome` is tested only through the functional tier.
9. A brief-lint test that prose mentioning `lockfile` or `JOB.md` is accepted/refused as intended (D15), and a test that
   `TEST: none` is refused or allowed deliberately (D7).
10. up_wrapper: the overflow path (up_wrapper.go:143-145) with a child that exits 0 and with a child that exits 1.

## Questions for the coordinator

1. Is the run machine meant to carry a worker (code) or not (SETUP.md:75)? Which is the ruling? (D8)
2. Where do bench seats get `DEEPSEEK_API_KEY`, `INCEPTION_API_KEY` and `NOVA_BUS_PASSWORD`: does nova-fleet's `services` play
   fetch them from the control store, or must a human `nova-secrets grant` them? I found no grant in this repository; I did
   not verify what `nova-secrets exec --only` does when a granted key is missing. (D2)
3. Is `edit` meant to exist in v2.0.x (LAYER-6 table, playbook) or was it dropped (core_test.go treats it as unknown)? (D4)
4. Is the TEST line meant to be a gate the system runs, or only a human-readable pointer? This card's own brief says
   `TEST: none` and also tells the reviewer to read `JOB.md`, which `brief_lint.go:30` would refuse; how was it admitted? (D7, D15)
5. Is the single-address pull limiter (4096 buckets x 60/s) deliberate for a dashboard that may sit behind a proxy? (D10)
6. For the dogfood gate: is a dev (non-tag) build supposed to reach `UP DONE`, given D16?
7. The red functional test in cmd/nova-worker: environmental (loaded host, no nova-sandbox) or a real regression? (Missing tests 7)
