GO ?= go
GOTEST_P ?= 4

# TLC runs only on a bench. TLC_JAR names the pinned tla2tools.jar, TLC_OUT the
# run's output directory, TLC_GROUP the group to run and TLC_BUDGET its time
# limit in seconds (tlc-full needs it named explicitly).
TLC_JAR ?=
TLC_OUT ?=
TLC_GROUP ?=
TLC_BUDGET ?= 110

.PHONY: build vet test functional help roadmap tlc tlc-full tlc-groups tlc-test tlc-require

help:
	@echo "make build       go build ./..."
	@echo "make vet         go vet ./..."
	@echo "make test        go vet ./... and go test -p GOTEST_P ./... (the unit tier)"
	@echo "make functional  go test -tags functional -timeout 120s ./... (needs redis-server; runs per PR on the fleet runner)"
	@echo "make roadmap     regenerate ROADMAP.md and FIXES.md from their sexp"
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

functional:
	$(NO_AUTO_MAINTENANCE) $(GO) test -p $(GOTEST_P) -count=1 -timeout 120s -tags functional ./...

roadmap:
	$(GO) run ./tools/roadmap --file docs/roadmap.sexp --out ROADMAP.md
	$(GO) run ./tools/roadmap --kind fixes --file docs/fixes.sexp --out FIXES.md

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
