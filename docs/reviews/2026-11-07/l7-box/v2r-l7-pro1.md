# Layer 7 (the box) cold review, v2r-l7-pro1

Reviewer: rowan (sonnet), attempt 4, continuing from d154e495 (amd-2, nova-2 reports), whose valid findings are folded in as 16, 17 and a missing test; its verdict that the box is sound does not hold against findings 1 to 7. Head reviewed: 43d849e2 (v2-stitch). Scope: docs/SETUP.md, PLAYBOOK-COORDINATOR.md, PLAYBOOK-WORKER.md, CORRECT-USE.md, CARD-CONTRACT.md, every verb's help text, cmd/nova-sprint (main, services, open, up_wrapper, internal, setup_*), the setup walk in verbs/setup*.go, tla/Setup*.tla, ENFORCED.md.

Runs: `go build ./... && go vet ./...` clean. `go test -skip Functional ./cmd/nova-sprint ./verbs ./docs` green (0.47 s / 0.43 s). `go test ./cmd/nova-sprint` with no skip is RED on this machine (finding 4 and 7). `nova-sprint help <verb>` run for all 40 verbs/subcommands: every one prints; help text checked against the docs' flags (matches, bar the items below).

## Verdict

The mechanics of `up` (two-phase credential split, lease, continuation record, one verify-act-verify per step, fail-closed fleet and ssh parsing) are carefully built and well tested at the unit level, but the box does not yet deliver what SETUP.md promises a cold AI: provider keys the human pastes in step 6 are never granted to the bench seats that the step-7 worker records open them from, and the worker records hard-code DeepSeek and Inception keys whatever `--services` was chosen (setup_workload.go:24-26, 54; verbs/setup_runtime_role_bootstrap.go:104-108). Worst single thing: with the default flow (`--services deepseek`, or `none`, or `openrouter`) step 7 installs worker units whose `nova-secrets exec` needs `DEEPSEEK_API_KEY` and `INCEPTION_API_KEY` in a bench seat nothing ever filled, so the first real fleet stops at 7/8 and the dogfood gate (a cold AI, zero to a running sprint, using only the tool) cannot pass. Close behind: the walk verifies once straight after each Act with no settling (steps 4, 5, 7), so a first install is likely to refuse even when healthy; every `up` writes a new store record and scans the whole event log three times; and the role bootstrap needs PostgreSQL 16+ unannounced.

## Defects

### 1. blocker: provider keys are never delivered to the bench seats, and the worker records demand fixed keys regardless of `--services`
- verbs/setup_runtime_role_bootstrap.go:99-108: `GrantSeat` grants only `sprintPasswordName` (`NOVA_SPRINT_PG_PASSWORD`). `grep -n "\.Grant(" verbs/*.go` finds this one call site. Step 6's `EnsureKey` (verbs/secret.go:29-67) saves the pasted key into the control seat's store only.
- cmd/nova-sprint/setup_workload.go:24-26,54: every worker record is created with keys `NOVA_SPRINT_PG_PASSWORD,DEEPSEEK_API_KEY,INCEPTION_API_KEY` (+ bus password). The step-6 choice is not consulted (`productionWorkload.ServicesReady` is `true`, setup_workload.go:84). With `--services none` (SETUP.md:73 "a valid answer") or `--services openrouter` the worker still opens two keys the human never gave; with `--services deepseek` the key exists only on the control seat.
- verbs/setup_runtime_model_probe.go:15-18: the only services step 6 accepts are `deepseek` and `openrouter`. SETUP.md:25-28 names DeepSeek, Mercury, OpenCode, OpenRouter; Mercury (`INCEPTION_API_KEY`, setup_workload.go:46) has no paste/probe path at all, so nothing can ever place that key.
- Why it matters: SETUP.md:32-36 promises "one API key per service, pasted once ... the AI makes the seats, the grants, the worker rows", and :79-80 says the worker opens "the provider keys the bench's seat holds". The code assumes the bench seat already holds them; no step makes that true. Step 7 ends in `WorkersReady` refusal naming the log line.
- Repair: derive the worker's key list from the saved `services` answer, grant each chosen provider key to every worker seat in `workersAct` (same Grant+Refresh as the sprint password), and refuse an unsupported service name by name.

### 2. serious: no settling between Act and the re-verify in steps 4, 5, 7 (and 8), so a correct first install refuses
- verbs/setup.go:295-307: `setupStep` does Verify, one Act, one Verify. `runAct` (verbs/setup_runtime.go:456-460), `dashboardAct` (:483-487), `workersAct` (:555) end in `nova-fleet services` plays; their verifies need the chooser lock held (verbs/setup_runtime_run.go:48-53), the dashboard answering, and every worker hooked and `up` (setup_workload.go:164-178). Those happen asynchronously after the unit starts.
- `grep -n "Sleep\|time.After\|NewTimer\|Ticker\|deadline\|retry" verbs/setup_runtime*.go` finds no wait or retry anywhere in the step code. A not-yet-ready answer becomes `Action: true` plus a `Refusal` (verbs/setup_runtime.go:216-221, 294-297, workloadReady :65-71) which is printed as the failure.
- Why it matters: SETUP.md:79-81 "The step verifies when every worker is on the bus and up". tla/Setup.tla:25-29 states `world[7]` "becomes true only when the units come up, so an Act that returns before that maps to repair again". The model has an unbounded repair loop (`WF_vars(Progress)`, EventuallyDone); the code has exactly one, so `up` exits 1 with "run not ready / not hooked" on a healthy fresh fleet and the human must rerun. Unit tests use fakes that become ready instantly, so nothing catches this.
- Repair: after Act, re-verify with a bounded poll (deadline from the context, injected clock) before returning the refusal. Question 1 asks whether nova-fleet's play waits for the unit.

### 3. serious: every `up` writes a store record, scans the whole event log three times, and races live writers
- cmd/nova-sprint/up_wrapper.go:76 always runs phase 1 with the admin credential; verbs/setup.go:188-192 then calls `PrepareHandoff` after step 2 whenever `Continuation == ""`; cmd/nova-sprint/setup_production.go:324-349 returns "not needed" only when the sprint password is already present, which phase 1 never has.
- So a re-run of `up` against a live, set-up sprint (the documented use: SETUP.md/`help up` "re-verifying from step 1 each time") appends a `setup:continuation:<random id>` policy record (setup_production.go:479-484) per invocation, forever.
- `findSetupContinuation` (setup_production.go:515-564) pages the event log from seq 0 to the snapshot head, in pages of `store.MaxEvents` (1000), and runs three times per `up`: verbs/setup.go:118 (prevalidate), :173 (walk), and in `writeSetupContinuation` (setup_production.go:469). Cost grows with sprint age.
- The write is fenced `AtSeq{snap.Seq}` (setup_production.go:484), which the store defines as "nothing at all has entered the store since the snapshot" (store/ops.go:75-77). A live run/worker event in that window gives `!receipt.Accepted`, and `applyHandoff` (setup_production.go:490-514) retries only `ErrUnknown`, never a refusal, so `up` fails with "continuation refused: ..." on a busy sprint.
- Repair: skip the handoff when the schema is already current and owned by `nova_sprint` (nothing for phase 2 to be correlated with), fetch the record by name rather than scanning events, and re-snapshot and retry a stale-seq refusal.

### 4. serious: the role bootstrap needs PostgreSQL 16+, nothing says so, and the failure is a raw SQL error
- verbs/setup_runtime_role.go:221 selects `pg_auth_members.set_option`, a PG16 column; the comment at :214 says "PostgreSQL 16+" but there is no `server_version_num` check (verbs/pg_backup.go:118 has one, for backup only). SETUP.md and `help up` state no PostgreSQL minimum.
- Reproduced here (PostgreSQL 14.24 from Homebrew): `go test ./cmd/nova-sprint` fails `TestFunctionalEndToEnd` with `nova-sprint: schema creation failed: reading the admin's grant on nova_sprint: ERROR: column "set_option" does not exist (SQLSTATE 42703)` (e2e_functional_test.go:148).
- Repair: read `server_version_num` first and refuse "PostgreSQL 16 or newer" naming the fix; say it in SETUP.md section 1.

### 5. serious: verbs and the run service share one unlocked clone directory
- cmd/nova-sprint/edges.go:57-64 (`origin.clone`) and run/edges.go:277 key the clone by the same `sha256(url)[:8]` under the same directory (`reposDir()`, cmd/nova-sprint/open.go:84; services.go:58 `ReposDir: reposDir()`). edges.go:49-51 says run "keeps its own the same way", but it is the same path, not a separate one.
- `git.Client.Clone` decides "already cloned" by `os.Stat(dir/.git)` alone (edge/git/git.go:567), so a verb (`add`, `check`, `restore` admission) running while run is mid-clone, or two verbs at once, can fetch into a half-made clone; concurrent fetches hit git ref-lock errors that surface as `add` refused or `CHECK RED`.
- Repair: give the verbs their own subdirectory (as `lander` already does with `stateDir()/land`, open.go:56), or take a file lock per clone.

### 6. serious: the walkthrough documents are not in the binary, so "only the tool's own help" cannot work
- docs/playbooks.go:5-9 embeds only PLAYBOOK-WORKER.md and PLAYBOOK-COORDINATOR.md. SETUP.md, CORRECT-USE.md, CARD-CONTRACT.md are repo-only; `help playbook <x>` accepts nothing else (verbs/help.go:19-24); `help up` is nine lines (flags, no step list, no fleet-first, no PostgreSQL/provider information).
- SETUP.md:49-52 defines done as "an AI ... using only docs/README.md and the tool's own help". A binary installed by `nova-fleet install` has neither. `up` does print `ASK the human` lines at steps 5/6/7 (verbs/setup_runtime.go:277,334,163), which is real mitigation, but step 1 (nova-tools first) and the PostgreSQL/Tailscale prerequisites are only in SETUP.md.
- Repair: embed the three docs and serve them under `help setup|use|card-contract`; have `help up` name them.

### 7. serious: the layer's default test command runs the functional tier and rewrites a committed file, even when red
- No build tag or env gate: `TestFunctionalEndToEnd` (e2e_functional_test.go:47-58) skips only when a tool is missing; with initdb, pg_ctl, redis-server, nova-bus on PATH, a plain `go test ./cmd/nova-sprint` starts PostgreSQL and Redis on loopback sockets (the unit/functional rule: unit tests use no sockets). docs/TESTING.md:20 relies on `-skip Functional` convention.
- e2e_fixtures_functional_test.go:59-69: `defer x.tr.save(t)` overwrites the tracked `run/testdata/e2e-transcript.txt` on every run, including a failing one. Reproduced: after the failing run above `git status` showed ` M run/testdata/e2e-transcript.txt` (6 lines changed); I reverted it.
- Repair: gate functional tests behind a tag or an env var (`NOVA_FUNCTIONAL=1`), and write the transcript only under `t.TempDir()` or only on pass with an explicit flag.

### 8. minor: `WorkerBenches` includes the run machine; SETUP.md and the model say it is excluded
- verbs/setup_runtime.go:50-61 selects every machine with the bench role (comment at :50-52: "the run machine too when it is one"). SETUP.md:75 "Every machine with the bench role, except the one the sprint runs on"; tla/Setup.tla:27 "every bench but the run machine"; verbs/setup_runtime_inventory.go:79-81 says only the public board is never on the run machine.
- Effect: on a one-box fleet where the run machine is a bench, step 7 installs `sprint-worker-<run machine>` against the doc; the model test (setup_workload_model_test.go:23) uses a services-only run machine and so cannot see it.
- Repair: pick one, and make the doc, the model comment and the code say it.

### 9. minor: the retired-mechanic lint refuses legitimate briefs
- verbs/brief_lint.go:16 (`flock|lockfile|winlock|...`), :30 (`JOB.md` anywhere in the brief, prose included), :18 (`gateSSH`). Run against `parseBrief` (temp test, removed):
  - `"Update the lockfile handling."` -> `c1: line 6 names a lock (lockfile), a retired mechanic`
  - `"Replace flock usage with a lease."` -> refused (flock)
  - `"Read JOB.md first."` -> refused
  - `DONE-WHEN: grep -c ssh docs/SETUP.md` -> `names a gate that runs on a named host (ssh docs)`
- CARD-CONTRACT.md:139-142 promises the refusal but with "no bypass" (:5-6) a card about a package lockfile, or one that removes `flock`, can never be added. This card's own brief contains the string `JOB.md`.
- Repair: match only on gate lines or on the structured keys, and drop the `(?i)` bare-word locks from prose.

### 10. minor: `ensureServiceVersion` turns every probe error into "needs action", and installs on an unreachable host
- cmd/nova-sprint/setup_production.go:823-844: any `checkServiceVersion` error (ssh timeout, DNS, permission, not just "different version") returns `(false, nil)` in Verify (:831-833) and, in Act, calls `nova-fleet install` (:837). The first error is dropped; the human sees only the install's result.
- Repair: distinguish `serviceVersionRefusal` (a real mismatch) from transport errors; return the latter as errors.

### 11. minor: `check` reads only nova-config's build info, and names a different fix than SETUP.md
- verbs/tools_version.go:19-29 reads the build info of the `nova-config` on this machine's PATH. SETUP.md:65-67 says it reads `nova-bus -h`, `nova-fleet -h` "and the rest" and names `nova-fleet up`; `toolsFix` (:55) says `nova-fleet install --tag <pin> <machine>`; `help check` agrees with the code. One binary on one host stands for the fleet's tools (SETUP.md:10-11 "the tools on every machine").
- Repair: correct the doc to what is read, or read the others.

### 12. minor: `help` misuse gives a nonsense fix
- verbs/help.go:28,38 call `misuse("help", ...)`, and `misuse` (verbs/result.go:70) appends `nova-sprint help <verb>` as the fix. Run: `nova-sprint help nosuch` -> `SPRINT REFUSED help: no verb nosuch: nova-sprint help help`; `nova-sprint help playbook` -> `...worker: nova-sprint help playbook worker: nova-sprint help help` (two commands in one fix). Fix should be `nova-sprint help`.

### 13. minor: `check` does not lint a brief directory
- CARD-CONTRACT.md:6-7 "`check` lints a brief directory before a sprint starts"; CORRECT-USE.md:42 and `help check` (`[--integrity]` only) have no such input; `grep -in brief verbs/check*.go` finds only test fixtures. `check` re-proves waiting and ready cards already added, which is a different thing.

### 14. minor: the worker playbook tells the worker about `JOB.md`, which the contract retires
- docs/PLAYBOOK-WORKER.md:19-20 "its harness reads the card from `JOB.md` in the job directory"; CARD-CONTRACT.md:139-142 "`JOB.md` ... The take receipt is the job". Both are printed to a cold AI by `hook`.

### 15. minor: step 6 says "one model-list request", the probe makes two; the service error is unnamed
- SETUP.md:74 vs verbs/setup_runtime_model_probe.go:46 (key endpoint, then models).
- verbs/setup_runtime.go:373 returns "unsupported or duplicate service" without naming which; `help up` shows `--services <a,b>` (no `none`, no list of names), so Mercury or OpenCode from SETUP.md:26 yields that bare line.

### 16. minor: `up --version` (release adoption) is promised and not built
- docs/PLAYBOOK-COORDINATOR.md:364-366 "installs the build beside the old, runs one shadow tick against the live store, swaps, watches three ticks, and swaps back on any overrun, crash or refused tick". verbs/setup_runtime_version.go:62-67 only checks the installed version, `check` and `run --shadow`; `Apply` (:123-128) always refuses with a `nova-fleet install` fix. No swap, watch or rollback exists. It fails safe (a loud refusal). Repair: build it or delete the promise. (Also found by the earlier attempt d154e495.)

### 17. nit
- verbs/check.go:81-87: `check --integrity` prints no history line at all when the store is not at schema (`if current && in.has("integrity")`), against "prints every line". Repair: emit a skipped line. (Earlier attempt d154e495.)
- cmd/nova-sprint/setup_production.go:80 `runDSN, _ :=` drops an error (the wrapper pre-checks, direct engine callers do not).
- cmd/nova-sprint/setup_workload.go:32 `workerArgv(machine, _ string)` ignores its second argument; :84,87,263 `ServicesReady`, `InstallServices`, `RunSmoke` are constant no-ops behind a 6-method interface (verbs/setup_runtime.go:35-42).
- cmd/nova-sprint/up_wrapper.go:236 a 1 s wait for fd 3 to close after the child exits: a grandchild that inherits fd 3 (not CLOEXEC after `os.NewFile`, up_wrapper.go:222) holds the pipe open and fails the walk with "continuation channel did not close".
- verbs/setup.go:266 `text[:strings.LastIndex(text, "next: ")]` panics on -1 if `hookAs` ever returns text without `next: `.
- verbs/help.go:61 `done("nova-sprint "+v.name+" ...")`: every help page ends with the literal `next: nova-sprint take ...`.
- cmd/nova-sprint/open.go:130-135 `lazyStore.Close` leaves `s.pg` set after closing.
- verbs/brief.go:51 reports a missing PATHS/TEST/DONE-WHEN at `len(lines)`, the last line of the brief, not a useful location; brief.go:135 accepts empty names (`NEEDS: a,,b` parses, checked).

## Promise gaps

| Doc sentence | What the code does |
|---|---|
| SETUP.md:32-36 "The key goes into nova-secrets; the AI makes the seats, the grants, the worker rows and the service rows." | Seats and sprint-password grants yes; no grant of any provider key (finding 1). |
| SETUP.md:25-28 "DeepSeek, Mercury, OpenCode, OpenRouter, and the like." | Only `deepseek`, `openrouter` accepted (setup_runtime_model_probe.go:15-18); Mercury hard-coded in the worker record with no way to supply its key. |
| SETUP.md:49-52 "using only docs/README.md and the tool's own help" | SETUP/CORRECT-USE/CARD-CONTRACT not embedded (docs/playbooks.go). |
| SETUP.md:65-67 "reads the installed tools' versions (`nova-bus -h`, `nova-fleet -h`, and the rest) ... naming the `nova-fleet up`" | Reads nova-config's build info only; names `nova-fleet install --tag`. |
| SETUP.md:75 "except the one the sprint runs on" | `WorkerBenches` keeps the run machine when it is a bench. |
| SETUP.md:73 "Nothing in this section is typed by the human" | Services/dashboard/commit identity are asked (setup_runtime.go:163,277,334) and the key is pasted; the page means "past the answers". |
| SETUP.md:74 "proven by one model-list request" | Two requests. |
| CARD-CONTRACT.md:6-7 "`check` lints a brief directory" | No such input. |
| CARD-CONTRACT.md:5-6 "on every path, with no bypass" | True, and over-broad (finding 9). |
| CORRECT-USE.md:35 "`up` refuses until `check` passes" (chain table, step 2) | `check` requires the store `up` creates (`help check`: "the store not set up: nova-sprint up"); `help` index says both run anywhere. Circular as worded. |
| CORRECT-USE.md:17-22 refusal form `SPRINT REFUSED <verb>: <what is not true>: <the command that makes it true>` | `help` misuse fix is `nova-sprint help help` (finding 12). |
| ENFORCED.md (target of the card's criterion (a)) | Covers layers 1, 2, 3, 5 only (`grep -o "^| L[0-9]" docs/ENFORCED.md` -> L1 31, L2 43, L3 19, L5 17); nothing for layer 6 or 7, so no box promise has an enforcement row. |

## Model vs code

- tla/Setup.tla world[7] is "every bench but the run machine on the bus and up". Code: includes the run machine (finding 8), and WorkersReady additionally requires every bench at this binary's version (setup_workload.go:183-209) and a set commit identity (verbs/setup_runtime.go:134-137,155-165). The binding test (setup_workload_model_test.go) fakes the version check and never sets a missing identity, so those two conjuncts are outside the model.
- Setup.tla `Do` sets `world[pc]=TRUE` and goes back to probe; EventuallyDone relies on unbounded repair. The code does one Act and one Verify (finding 2); the model's weak-fairness repair loop corresponds to the human rerunning `up`, which the model's `Restart` does cover but the doc does not say is expected.
- SetupHandoff: the model covers the lease, the record, stale/early/lost records and secret separation (cfg files SetupHandoff*.cfg). Not modelled: the `AtSeq` fence refusing when the live store moves (finding 3), the per-`up` record accumulation, and the 3x full-log scan.
- Not modelled at all: the dashboard choice branch (tailnet vs public=<machine>[:port]) and its service-record validation (setup_production.go:1104-1203; verbs/setup_runtime.go:573-606 repeat the same pull-only rules in two places).

## Simplifications

- The pull-only public dashboard record is validated twice with different code: `publicService` (setup_production.go:1181-1203) and `validPublicTarget` (verbs/setup_runtime.go:591-606); `isDashboardService` and the `/usr/bin/env nova-sprint` special case also appear in both. One validator.
- `ServicesReady`, `InstallServices`, `RunSmoke` (setup_workload.go:84,87,263) are constant no-ops; drop them from `SetupWorkload` and from the model mapping.
- Three near-identical env allow-lists: `toolEnvironment` (environment.go:22), `setupWrapperEnv` (up_wrapper.go:197), `setupCommandEnv`/`continuationEnv` (setup_production.go:376,956). One table keyed by tool.
- `selectRunService` (setup_production.go:1000-1031) plus `validateRunService` (:692) plus `sameFleetService` (verbs/setup_runtime_services.go:175) each re-check the same record against hard-coded shapes; `Ensure` already refuses a differing record.
- setup_production.go is 1203 lines in package main mixing config, handoff storage, service-record validation, ssh, tailnet addressing and dashboard targeting; the handoff (320-564) and the dashboard targeting (1038-1203) are separable.

## Missing tests

- Step-7 worker record with `--services none` / `openrouter` (finding 1): `TestInstallWorkersWritesOneRecordPerBenchAndOnePlay` asserts the fixed key list rather than deriving it.
- A grant of a provider key to a bench seat: no test, since no code.
- First-install readiness after Act (finding 2): no test with a verifier that becomes true only after a delay.
- `up` run twice on a live fake store while another writer commits between the continuation snapshot and apply (finding 3); and a count of continuation records after N ups.
- PostgreSQL < 16 for `createSprintSchema` (finding 4).
- `ensureServiceVersion` with a transport error versus a version mismatch (finding 10); `TestPublicVersionInstallRequiresRecheck` covers the install path only.
- `help` with a mistyped verb asserting the fix line (finding 12); a test that every `help <verb>` flag appears in the verb's parse table (nothing ties help text to the parser).
- `check` against a brief directory (finding 13).
- up_wrapper.go:33-45 (`limitedOutput` overflow) and the 1 s `waitHandoff` timeout branch (up_wrapper.go:235-246) have no test (earlier attempt d154e495).
- Verb-time concurrency with run on the shared clone (finding 5).
- Brief lint false-positive/negative table for `brief_lint.go` (finding 9).
- `docs/` has no test that each `nova-sprint <verb> ...` command quoted in PLAYBOOK-*.md and CORRECT-USE.md parses against the verb table (I checked by hand: all verbs and flags in the playbooks exist; `start --reason` is optional in `help start` but shown required in PLAYBOOK-COORDINATOR.md:56).

## Questions for the coordinator

1. Does `nova-fleet services` return only after the unit is running (and does a launchd/systemd keepalive start make the run lock, bus hook and dashboard answer within the play)? If yes, finding 2 narrows to workers hooking after the play; if no, a fresh `up` always needs a second run.
2. Are provider keys meant to be pre-placed on bench seats by nova-tools (SETUP.md:79-80 says "the bench's seat holds")? If so, finding 1 is a doc and key-list-derivation fix; if not, the grant is missing code.
3. Is the run machine meant to work cards when it is a bench (code comment, 2026-11-07) or not (SETUP.md:75, Setup.tla:27)? One of them is stale.
4. Is PostgreSQL 16 the stated minimum for the fleet (nova-tools installs 18 on the benches)? Then finding 4 is only the missing refusal.
5. Is `nova-sprint up` meant to leave a continuation record on every invocation, or only on a fresh store? The comment at setup_production.go:319 says "only while the first child's setup lease is held", which reads as first-run only.
6. Was the `go test ./<layer>/...` instruction meant to include `Functional`? On this host (PG14, initdb on PATH) it ran and failed; the card said unit tests only.
