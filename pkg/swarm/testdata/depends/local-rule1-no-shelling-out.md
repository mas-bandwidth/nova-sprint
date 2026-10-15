RESULT: local-rule1-no-shelling-out sha=<sha12> tier: pro
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
STOP: cmd/nova-local reads the one-minute load in process on darwin and linux with no pkg/hostload, pkg/subproc or os/exec in its own import graph, TestNovaLocalShellsOutForNothing pins it, and docs/SPEC-LOCAL.md rule 1 and its Tests row state the measured go list -deps result.
PATHS: cmd/nova-local/box.go, cmd/nova-local/box_darwin.go, cmd/nova-local/box_linux.go, cmd/nova-local/box_other.go, docs/SPEC-LOCAL.md
NEW: cmd/nova-local/deps_test.go, cmd/nova-local/loadavg_test.go
SHARED: docs/SPEC-LOCAL.md
TEST: cmd/nova-local TestNovaLocalShellsOutForNothing
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

THE TASK. docs/SPEC-LOCAL.md rule 1 (the owner: "no deps"): one Go binary of the standard library and this module's packages, "and no shelling out to learn what the standard library cannot read: the box's memory and load are sysctl on darwin and /proc on linux, read in process". The memory is read in process (cmd/nova-local/box_darwin.go:14-29 syscall.Sysctl, box_linux.go:15 /proc/meminfo), but the load is not: box_darwin.go:12, box_linux.go:13 and box_other.go:7 call hostload.Local().Load1, and pkg/hostload/source_darwin.go (read) lines 10-11 import pkg/subproc, whose pkg/subproc/subproc.go (read) line 21 runs top and lsof through os/exec. Line numbers are at the v1.3/comms tip. Rule 1's check (go list -deps ./cmd/nova-local) is wired nowhere.
The change:
(1) Read load1 in process: linux from /proc/loadavg (first field); darwin from the sysctl vm.loadavg (struct loadavg: three uint32 ldavg then a long fscale; syscall.Sysctl returns the raw bytes as a string with one trailing NUL cut, so decode with that in mind and test the decode on fixed bytes); other platforms report load unknown (load1=unknown is already the printed form). The parse is a pure function taking bytes, tested on fixtures.
(2) Drop pkg/hostload from cmd/nova-local. pkg/hostload itself is not changed: other tools use it.
(3) TestNovaLocalShellsOutForNothing in cmd/nova-local/deps_test.go: parse the package's non-test .go files with go/parser (ImportsOnly) and fail naming the file for any import of os/exec, pkg/subproc or pkg/hostload. It runs no go command.
(4) Measure go list -deps ./cmd/nova-local once by hand and paste the packages outside the standard library and this module into the report. At the tip the tool skeleton pkg/tool brings golang.org/x/sys/unix. Do not remove pkg/tool or rewrite it: amend rule 1's sentence and its Tests row to state the measured truth (the module outside the standard library that the skeleton brings, by name), and put "rule 1: x/sys via pkg/tool, the owner's call" as the first line of the report's not-done, so the coordinator takes it to the owner.
Libraries considered: syscall (Sysctl, already used for memory), os.ReadFile for /proc/loadavg, encoding/binary for the darwin struct, go/parser for the import check; pkg/hostload is what is being dropped.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Red test first: TestNovaLocalShellsOutForNothing, opening with t.Parallel(); at the base it fails naming box_darwin.go, box_linux.go and box_other.go for pkg/hostload. Also TestLoadAvgParses in cmd/nova-local/loadavg_test.go on fixed linux and darwin bytes, including a darwin buffer whose last byte was cut. Run go test -count=1 -timeout 600s ./cmd/nova-local/ -run 'TestNovaLocalShellsOutForNothing|TestLoadAvgParses' and keep the failing line.
STEP 3. Make it pass in the PATHS and NEW files only; the Box seam (box.go) stays the way tests fake the box.
STEP 4. Gate: go test -count=1 -timeout 600s ./cmd/nova-local/ ./internal/docs/ ./internal/ci/ and read the last line of each; GOOS=linux go vet ./cmd/nova-local/ and GOOS=windows go vet ./cmd/nova-local/ build the other boxes. A red test is named with its file and whether your work changed it.
STEP 5. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, each test and what it pins, the measured go list -deps lines, and what was not done.
STEP 6. End as JOB.md says.
