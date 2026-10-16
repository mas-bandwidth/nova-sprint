GO ?= go
GOTEST_P ?= 4

# TLC runs only on a bench. TLC_JAR names the pinned tla2tools.jar, TLC_OUT the
# run's output directory, TLC_GROUP the group to run and TLC_BUDGET its time
# limit in seconds (tlc-full needs it named explicitly).
TLC_JAR ?=
TLC_OUT ?=
TLC_GROUP ?=
TLC_BUDGET ?= 110

.PHONY: build vet test functional functional-gate functional-gate-rest functional-gate-deadcode functional-gate-shard release-accept help roadmap schemawire-check tlc tlc-full tlc-groups tlc-test tlc-require

help:
	@echo "make build       go build ./..."
	@echo "make vet         go vet ./..."
	@echo "make test        go vet ./... and go test -p GOTEST_P ./... (the unit tier)"
	@echo "make functional  go test -tags functional -count=1 ./... (needs redis-server on PATH)"
	@echo "make functional-gate  the tier as CI runs it: NOVA_CI=1 and a 2-minute bound per package"
	@echo "make functional-gate-rest  the tier without ./cmd/nova-sprint and TestDeadCode (its rest leg)"
	@echo "make functional-gate-deadcode  TestDeadCode alone (its deadcode leg)"
	@echo "make functional-gate-shard  one shard of ./cmd/nova-sprint: SHARD=k SHARDS=n (its shard legs)"
	@echo "make release-accept  the release gate: the release checks in parallel, under a minute (NOVA_SPRINT_BIN, redis-server)"
	@echo "make roadmap     regenerate ROADMAP.md and FIXES.md from their sexp"
	@echo "make schemawire-check  go generate every pkg/*wire package, then git diff --exit-code -- pkg schemas (schemas/README.md)"
	@echo "make tlc         run a group on a TLC bench: TLC_JAR, TLC_OUT and TLC_GROUP"
	@echo "make tlc-full    run a manual group: TLC_JAR, TLC_OUT, TLC_GROUP and an explicit TLC_BUDGET"
	@echo "make tlc-groups  print the groups with a case that has no current record"
	@echo "make tlc-test    go test ./tools/tlacheck/ (the runner's own tests)"

build:
	$(GO) build ./...

vet:
	$(GO) vet ./...

# the tick's wall-clock gate runs alone, after the rest: beside sibling packages its
# timing measures the machine's load, not the deal (.github/workflows/ci.yml)
TICK_GATE_TEST := ^TestTickDrainAndDealUnderTheGateAtLoad$$

# auto maintenance off for every git the tests start: git 2.46 and later detach a
# `git maintenance run --auto` after a commit that can outlive a test's scratch repository
# and race its cleanup (.github/workflows/ci.yml)
NO_AUTO_MAINTENANCE := GIT_CONFIG_PARAMETERS="'maintenance.auto'='false'"

test: vet
	$(NO_AUTO_MAINTENANCE) $(GO) test -p $(GOTEST_P) -skip '$(TICK_GATE_TEST)' ./...
	$(NO_AUTO_MAINTENANCE) $(GO) test -count=1 -run '$(TICK_GATE_TEST)' ./internal/sprint/

# the functional tier: the real store's tests. Bounded at two minutes per
# package (the standing rule: one minute ideally, two max; over the bound is
# the test's defect). Needs redis-server on PATH.
functional:
	$(NO_AUTO_MAINTENANCE) $(GO) test -p $(GOTEST_P) -tags functional -count=1 -timeout 120s ./...

# functional-gate is the tier as CI runs it per pull request: the same
# two-minute-per-package bound, with NOVA_CI=1 so a missing redis-server fails
# closed instead of skipping.
functional-gate:
	$(NO_AUTO_MAINTENANCE) NOVA_CI=1 $(GO) test -p $(GOTEST_P) -tags functional -count=1 -timeout 120s ./...

# functional-gate-rest runs the tier without ./cmd/nova-sprint, which the
# functional job's matrix runs sharded across three legs
# (.github/workflows/ci.yml), and without TestDeadCode, which its deadcode leg
# runs alone.
FUNCTIONAL_REST := $(filter-out %/cmd/nova-sprint,$(shell $(GO) list ./...))

# the dead code rule (internal/ci/dead_code_test.go) runs alone, in its own leg,
# under the same two-minute bound: its 18 deadcode runs each load and analyse
# the whole program, and beside the rest leg's sibling packages on a four-core
# runner they were still running at 2m0s (runs 38751459368, 38754975433), where
# alone on four cores they take 45-50 s
DEAD_CODE_TEST := ^TestDeadCode$$

functional-gate-rest:
	$(NO_AUTO_MAINTENANCE) NOVA_CI=1 $(GO) test -p $(GOTEST_P) -tags functional -count=1 -timeout 120s -skip '$(DEAD_CODE_TEST)' $(FUNCTIONAL_REST)

functional-gate-deadcode:
	$(NO_AUTO_MAINTENANCE) NOVA_CI=1 $(GO) test -tags functional -count=1 -timeout 120s -run '$(DEAD_CODE_TEST)' ./internal/ci/

# functional-gate-shard runs one shard of ./cmd/nova-sprint's functional package:
# SHARD names the shard (1..SHARDS) and SHARDS the number of them; both are
# refused unless named, like tlc-require.
SHARD_PATTERN = $(shell $(GO) test -tags functional -list . ./cmd/nova-sprint | $(GO) run ./tools/testshard --shard $(SHARD) --of $(SHARDS))

functional-gate-shard:
	@test -n "$(SHARD)" || { echo "make functional-gate-shard needs SHARD (1..SHARDS)" >&2; exit 2; }
	@test -n "$(SHARDS)" || { echo "make functional-gate-shard needs SHARDS (the number of shards)" >&2; exit 2; }
	$(NO_AUTO_MAINTENANCE) NOVA_CI=1 $(GO) test -tags functional -count=1 -timeout 120s -run '$(SHARD_PATTERN)' ./cmd/nova-sprint

# the release gate (docs/TESTS.md, "Release acceptance"): the checks a point release passed by
# a ten-minute live watch, as tests in parallel, under one minute on a bench and failed past
# two. NOVA_SPRINT_BIN names the build the canary runs; unset, this tree's is built.
release-accept:
	GO=$(GO) NOVA_SPRINT_BIN=$(NOVA_SPRINT_BIN) sh tools/release-accept.sh

roadmap:
	$(GO) tool roadmap --file docs/roadmap.sexp --out ROADMAP.md
	$(GO) tool roadmap --kind fixes --file docs/fixes.sexp --out FIXES.md

# schemawire-check is the one check of the generated wire packages
# (schemas/README.md): a definition in schemas/<area>/ generates pkg/<area>wire/,
# whose hand-written generate.go pins the schema compiler by its go:generate
# line. The check regenerates every generated package, then refuses any
# difference from what is committed under pkg/ and schemas/, and any generated
# file left uncommitted. It is the generated files' only check: internal/ci's
# line linters skip a file with Go's generated header. With no generated package
# it says so and passes. A cold module cache fetches the pinned compiler once.
schemawire-check:
	@set -e; found=; \
	for g in pkg/*wire/generate.go; do \
		test -f "$$g" || continue; \
		found=1; \
		d=$${g%/generate.go}; \
		echo "go generate ./$$d"; \
		$(GO) generate "./$$d"; \
	done; \
	if [ -z "$$found" ]; then echo "schemawire-check: no generated pkg/*wire package yet (none holds a generate.go); nothing to regenerate"; exit 0; fi; \
	git diff --exit-code -- pkg schemas || { echo "schemawire-check: the generated files differ from the committed ones; commit the output of go generate, never a hand edit (schemas/README.md)" >&2; exit 1; }; \
	untracked=$$(git ls-files --others --exclude-standard -- pkg schemas); \
	if [ -n "$$untracked" ]; then echo "schemawire-check: generated files are not committed: $$untracked; commit them (schemas/README.md)" >&2; exit 1; fi; \
	echo "schemawire-check: OK"

# tlc-require refuses (exit 2) unless TLC_JAR, TLC_OUT and TLC_GROUP are named.
tlc-require:
	@test -n "$(TLC_JAR)" || { echo "make tlc needs TLC_JAR (the TLC jar)" >&2; exit 2; }
	@test -n "$(TLC_OUT)" || { echo "make tlc needs TLC_OUT (the run's output directory)" >&2; exit 2; }
	@test -n "$(TLC_GROUP)" || { echo "make tlc needs TLC_GROUP (the group to run)" >&2; exit 2; }

tlc: tlc-require
	$(GO) run ./tools/tlacheck run --root . --jar "$(TLC_JAR)" --dir "$(TLC_OUT)" --group "$(TLC_GROUP)" --timeout "$(TLC_BUDGET)s" $(if $(TLC_PARALLEL),--parallel "$(TLC_PARALLEL)",)

tlc-full: tlc-require
	@test "$(origin TLC_BUDGET)" = "command line" -o "$(origin TLC_BUDGET)" = "environment" || { echo "make tlc-full needs an explicit TLC_BUDGET (a manual run is unbounded by default)" >&2; exit 2; }
	$(GO) run ./tools/tlacheck run --root . --jar "$(TLC_JAR)" --dir "$(TLC_OUT)" --group "$(TLC_GROUP)" --timeout "$(TLC_BUDGET)s" $(if $(TLC_PARALLEL),--parallel "$(TLC_PARALLEL)",) --manual

tlc-groups:
	$(GO) run ./tools/tlacheck groups --root . --stale

tlc-test:
	$(NO_AUTO_MAINTENANCE) $(GO) test -count=1 ./tools/tlacheck/
