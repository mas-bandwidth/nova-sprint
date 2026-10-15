RESULT: local-status-box-fields-always-printed sha=<sha12> tier: pro
REPO: mas-bandwidth/nova-tools
BASE: v1.3/comms
STOP: every STATUS OK and SERVE OK line carries mem_used, mem_free, mem_total, wired_cap and load1 on every platform, unknown or unset where the box cannot read one, and TestTheBoxFieldsAreAlwaysPrinted pins it on a box with nothing readable.
PATHS: cmd/nova-local/main.go, cmd/nova-local/box.go, cmd/nova-local/box_linux.go, cmd/nova-local/box_other.go, cmd/nova-local/main_test.go, docs/TESTS.md, docs/SPEC-LOCAL.md
SHARED: cmd/nova-local/main.go, docs/TESTS.md, docs/SPEC-LOCAL.md
TEST: cmd/nova-local TestTheBoxFieldsAreAlwaysPrinted
DEPENDS-ON: local-serve-refusals-digest-and-exit-code
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

THE TASK. docs/SPEC-LOCAL.md rule 10: status prints "on its first line, mem_used, mem_free, mem_total and the live wired_cap (darwin's GPU wired limit, unset when none)", and the STATUS OK and SERVE OK line shapes carry wired_cap=<n|unset>. At the v1.3/comms tip cmd/nova-local/main.go boxFacts (around lines 234-261) prints wired_cap only when Box.WiredCap is non-nil, and it is nil on linux (box_linux.go:11-20) and every other platform (box_other.go:7), so a linux box's line has no wired_cap at all; and when memory cannot be read it prints mem_free and mem_total as unknown but drops mem_used. A program reading the line cannot tell absent from unread.
The change: boxFacts prints all five fields always: mem_used, mem_free and mem_total as numbers or unknown together; wired_cap as bytes, unset when the box has a cap function that reports none, and unset on a platform with no wired limit (the spec's "unset when none"); load1 as today. No verdict is added (rule 10 is a measurement). Update docs/TESTS.md's nova-local first run if its printed lines change, so TestTESTSFirstRunIsWhatTheToolPrints stays green.
Libraries considered: none: a change to how existing facts are printed.

STEP 1. cd into the staged checkout JOB.md names and run git log --oneline -1; it is a NEW worktree on the branch this card names. Export GOFLAGS=-mod=readonly NOVA_TEST_NO_HOST=1 before any go command; GOCACHE is already set (JOB.md names it): keep it.
STEP 2. Red test first: TestTheBoxFieldsAreAlwaysPrinted in cmd/nova-local/main_test.go, opening with t.Parallel(), against the fake engine with a faked Box: a Box whose Mem, WiredCap and Load1 are all nil prints mem_used=unknown mem_free=unknown mem_total=unknown wired_cap=unset load1=unknown on STATUS OK and on serve --dry-run's SERVE OK; a Box with a WiredCap reporting none prints wired_cap=unset; one reporting 96 GiB prints its bytes. Run go test -count=1 -timeout 600s ./cmd/nova-local/ -run TestTheBoxFieldsAreAlwaysPrinted and keep the failing line.
STEP 3. Make it pass in the PATHS files only; TestStatusReadsTheMemoryEachCall stays green unchanged.
STEP 4. Gate: go test -count=1 -timeout 600s ./cmd/nova-local/ ./internal/docs/ ./internal/ci/ and read the last line of each. A red test is named with its file and whether your work changed it.
STEP 5. Commit on your branch with the trailer; the pull request against v1.3/comms states the diff stat, the test and what it pins, and what was not done.
STEP 6. End as JOB.md says.
