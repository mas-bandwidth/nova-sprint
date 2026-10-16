package sprint

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BuildSALiveShape20261013 constructs the exact 2026-10-13 20:30Z live sprint shape
// from Rowan's sentinel and owner audit (sa/all.jsonl, sa/lines.txt, sa/sentinels.txt,
// sa/briefs/*.md) before fixes 2-4 were applied:
//
//   - Stream schema-shared with 7 open primary cards (scores 82, 83, 155, 157, 159, 161, 162)
//     and 3 shared sentinels at the tail (scores 901, 902, 903):
//   - sentinel-schema-shared-fixed-form-corpus-ready (901)
//   - sentinel-schema-shared-guard-chain-ready (902)
//   - sentinel-schema-shared-wide-text-ready (903)
//   - Primary card the-fixed-form-corpus-builds-on-every-bench (score 83, Col=Review)
//     with who="friend", rule_tier="friend", no WHO: line in its brief, 6 fleet attempts
//     (.w1..w6 on batman, hetzner1, hetzner2, studio, mini-m5, superman), result="failed",
//     last_finish="6:failed", and head="e802cb56a9395e30d59b9775632637df22080318".
//   - Consumer streams (schema-c, schema-cards, schema-cpp, schema-cs, schema-dart,
//     schema-elixir, schema-go, schema-java, schema-js, schema-rust) and their leg sentinels
//     (scores 905..912, 929), including:
//   - sentinel-schema-shared-guard-chain-ready declaring only 1 of its 8 live consumers
//     (js-carries-maps-and-union-arms-on-the-table-wire) and omitting 7 consumers.
//   - sentinel-schema-{dart,elixir,java,js}-cards-leg-ready declaring only
//     the-cards-page-and-roadmap-name-every-language and omitting the cross-stream
//     *-rides-the-wire-fuzzer-and-retains-on-every-root consumer.
//   - dart-carries-the-variable-class-beyond-the-card-constructs and
//     java-carries-the-variable-class-beyond-the-card-constructs with who="friend",
//     no WHO: line in brief, and fleet attempts.
//   - fixed-dart-t12-extents-writes-guards-reconciled-6 (142 before sentinel 911),
//     fixed-elixir-t12-ordinals-arms-6 (153 before sentinel 912), and
//     fixed-rust-t12-framing-text-reconciled-6 (103 before sentinel 908) sitting before
//     their stream's cards-leg sentinel.
func BuildSALiveShape20261013() *Snapshot {
	s := &Snapshot{
		Now:         time.Date(2026, 10, 13, 20, 30, 0, 0, time.UTC),
		Work:        NewTable(Work),
		Readers:     NewTable(Readers),
		Merge:       NewTable(Merge),
		Fleet:       NewTable(Fleet),
		Coordinator: "coordinator",
		Actor:       "coordinator",
		Providers:   map[string]Provider{"fixture": {Name: "fixture", Billing: "plan", Funded: false}},
	}

	streams := map[string]string{
		"schema-shared":  "Schema Shared",
		"schema-c":       "Schema C",
		"schema-cards":   "Schema Cards",
		"schema-cpp":     "Schema Cpp",
		"schema-cs":      "Schema CSharp",
		"schema-dart":    "Schema Dart",
		"schema-elixir":  "Schema Elixir",
		"schema-go":      "Schema Go",
		"schema-java":    "Schema Java",
		"schema-js":      "Schema JS",
		"schema-rust":    "Schema Rust",
		"schema-tooling": "Schema Tooling",
	}
	for id, display := range streams {
		addTestStream(s, id, display)
	}

	addLanded := func(id, stream string, score float64, needs string) {
		s.Work.Put(&Card{
			ID:    id,
			Row:   stream,
			Col:   Landed,
			Score: score,
			Fields: map[string]string{
				"needs": needs,
				"brief": "KIND: primary\nPATHS: docs/roadmap.sexp\nTEST: none landed\n",
			},
		})
	}

	// Landed prerequisites referenced by live open cards in sa/lines.txt.
	addLanded("c-fixed-form-emitter-does-not-panic-on-r2", "schema-c", 79, "")
	addLanded("fixed-c-t12-framing-writes-reconciled-6", "schema-c", 88, "c-fixed-form-emitter-does-not-panic-on-r2")
	addLanded("agents-map-is-current-on-the-branch", "schema-tooling", 81, "")
	addLanded("sentinel-schema-tooling-agents-map-ready", "schema-tooling", 904, "agents-map-is-current-on-the-branch")
	addLanded("the-card-corpus-is-a-language-neutral-conformance-set", "schema-cards", 166, "sentinel-schema-tooling-agents-map-ready")
	addLanded("sentinel-schema-cards-corpus-ready", "schema-cards", 166.5, "the-card-corpus-is-a-language-neutral-conformance-set")
	addLanded("dart-fixed-scalar-defaults-probe-compiles", "schema-dart", 67, "")
	addLanded("fixed-dart-t13-layout-hash-collision-6", "schema-dart", 68, "dart-fixed-scalar-defaults-probe-compiles")
	addLanded("elixir-message-form-batch-runs-under-test-elixir", "schema-elixir", 64, "")
	addLanded("fixed-elixir-t13-layout-hash-collision-6", "schema-elixir", 65, "elixir-message-form-batch-runs-under-test-elixir")
	addLanded("go-union-arm-payloads-probe-compiles", "schema-go", 75, "")
	addLanded("go-retain-gate-names-the-tests-that-exist", "schema-go", 76, "")
	addLanded("fixed-go-t13-layout-hash-collision-6", "schema-go", 98, "go-union-arm-payloads-probe-compiles")
	addLanded("fixed-js-t13-layout-hash-collision-6", "schema-js", 70, "")
	addLanded("fixed-rust-t13-layout-hash-collision-6", "schema-rust", 73, "")
	addLanded("shared-fixed-form-gate-admits-direct-and-payload-free-arms", "schema-shared", 84, "")
	addLanded("exact-counters-and-report-semantics", "schema-shared", 156, "shared-fixed-form-gate-admits-direct-and-payload-free-arms")

	addPrimary := func(id, stream, col string, score float64, needs, paths, newPaths string, extra map[string]string) *Card {
		var b strings.Builder
		b.WriteString("KIND: primary\n")
		b.WriteString("PATHS: " + paths + "\n")
		if newPaths != "" {
			b.WriteString("NEW: " + newPaths + "\n")
		}
		b.WriteString("TEST: none fixture\n")
		fields := map[string]string{
			"needs": needs,
			"brief": b.String(),
		}
		for k, v := range extra {
			fields[k] = v
		}
		c := &Card{
			ID:     id,
			Row:    stream,
			Col:    col,
			Score:  score,
			Fields: fields,
		}
		s.Work.Put(c)
		return c
	}

	addSentinel := func(id, stream string, score float64, needs string, declaredConsumers []string) *Card {
		consumersCSV := strings.Join(declaredConsumers, ", ")
		var bullets strings.Builder
		for i, c := range declaredConsumers {
			if i > 0 {
				bullets.WriteString(", ")
			}
			bullets.WriteString("`" + c + "`")
		}
		brief := "KIND: sentinel\n" +
			"PATHS: docs/roadmap.sexp\n" +
			"CONSUMERS: " + consumersCSV + "\n" +
			"TEST: none — sentinel card\n\n" +
			"## Dependencies\n\n" +
			"- Consumers that need this sentinel: " + bullets.String() + ".\n"
		c := &Card{
			ID:    id,
			Row:   stream,
			Col:   Waiting,
			Score: score,
			Fields: map[string]string{
				"kind":  "sentinel",
				"needs": needs,
				"brief": brief,
			},
		}
		s.Work.Put(c)
		return c
	}

	// === Stream: schema-shared (7 open primaries + 3 shared sentinels) ===
	addPrimary(
		"wide-text-in-table-closures-for-the-four-remaining-legs",
		"schema-shared", Working, 82, "",
		"compiler/widetext.go,compiler/widetext_test.go,internal/codegen/rusttable/**,internal/codegen/javatable/**,internal/codegen/jstable/**,internal/codegen/elixirtable/**,internal/tablenames/*.go,docs/roadmap-evidence/fixed-*.sexp",
		"internal/codegen/rusttable/fixedroadmap_t19_test.go,internal/codegen/javatable/fixedroadmap_t19_test.go,internal/codegen/jstable/fixedroadmap_t19_test.go,internal/codegen/elixirtable/fixedroadmap_t19_test.go",
		map[string]string{"priority": "critical"},
	)

	// The failed review card with who=friend, no WHO: line in brief, and 6 fleet attempts.
	addPrimary(
		"the-fixed-form-corpus-builds-on-every-bench",
		"schema-shared", Review, 83, "",
		"tables/messages/schema.lock,internal/lockfile/**,internal/codegen/gotable/fixedversioning_test.go,docs/roadmap-evidence/fixed-*.sexp,build/fixedform-corpus/manifest.txt,tables/messages/go/FixedVersioningProbe/serialize.go",
		"",
		map[string]string{
			"priority":    "critical",
			"who":         "friend",
			"rule_tier":   "friend",
			"attempt":     "6",
			"result":      "failed",
			"last_finish": "6:failed",
			"head":        "e802cb56a9395e30d59b9775632637df22080318",
			"work":        "the-fixed-form-corpus-builds-on-every-bench.w6",
			"routes":      "gpt-5.4-high,gpt-5.4-high,gpt-5.4-high,gpt-5.4-xhigh,gpt-5.4-xhigh,gpt-5.4-xhigh",
		},
	)
	fleetMembers := []string{"batman", "hetzner1", "hetzner2", "studio", "mini-m5", "superman"}
	fleetRoutes := []string{"gpt-5.4-high", "gpt-5.4-high", "gpt-5.4-high", "gpt-5.4-xhigh", "gpt-5.4-xhigh", "gpt-5.4-xhigh"}
	for i, m := range fleetMembers {
		attemptStr := string(rune('1' + i))
		s.Work.Put(&Card{
			ID:  "the-fixed-form-corpus-builds-on-every-bench.w" + attemptStr,
			Row: "",
			Col: "",
			Fields: map[string]string{
				"card":    "the-fixed-form-corpus-builds-on-every-bench",
				"attempt": attemptStr,
				"member":  m,
				"friend":  "",
				"route":   fleetRoutes[i],
				"result":  "failed",
			},
		})
	}

	addPrimary(
		"compressed-float-rides-as-the-float-on-every-leg",
		"schema-shared", Waiting, 155, "the-guard-chain-answers-the-outer-tag",
		"docs/SPEC.md,internal/codegen/cpptable/**,internal/codegen/ctable/**,internal/codegen/cstable/**,internal/codegen/gotable/**,internal/codegen/javatable/**,internal/codegen/elixirtable/**,docs/roadmap-evidence/fixed-*.sexp",
		"",
		map[string]string{"priority": "critical"},
	)

	addPrimary(
		"full-width-ordinal-lanes",
		"schema-shared", Waiting, 157, "wide-text-in-table-closures-for-the-four-remaining-legs",
		"internal/codegen/ctable/**,internal/codegen/gotable/**,internal/codegen/elixirtable/**,docs/roadmap-evidence/fixed-c.sexp,docs/roadmap-evidence/fixed-go.sexp,docs/roadmap-evidence/fixed-elixir.sexp",
		"",
		map[string]string{"priority": "critical"},
	)

	addPrimary(
		"the-128-bit-and-fixed-point-fixed-rows-run-with-the-serialize-sibling",
		"schema-shared", Waiting, 159, "the-fixed-form-corpus-builds-on-every-bench",
		"internal/codegen/cpptable/fixedroadmap_t09_test.go,internal/codegen/ctable/fixedroadmap_t09_test.go,internal/codegen/gotable/fixedroadmap_t09_test.go,docs/roadmap-evidence/fixed-cpp.sexp,docs/roadmap-evidence/fixed-c.sexp,docs/roadmap-evidence/fixed-go.sexp",
		"",
		map[string]string{"priority": "critical"},
	)

	addPrimary(
		"the-guard-chain-answers-the-outer-tag",
		"schema-shared", Waiting, 161, "the-plan-is-partitioned-unguarded-first",
		"internal/codegen/gotable/**,internal/codegen/cstable/**,internal/codegen/javatable/**,internal/codegen/jstable/**,internal/codegen/darttable/**,internal/codegen/js/**,docs/FIXED-FORM-ALGORITHM.md,docs/roadmap-evidence/fixed-*.sexp",
		"",
		map[string]string{"priority": "critical"},
	)

	addPrimary(
		"the-plan-is-partitioned-unguarded-first",
		"schema-shared", Waiting, 162, "exact-counters-and-report-semantics,full-width-ordinal-lanes",
		"internal/codegen/gotable/**,internal/codegen/rusttable/**,internal/codegen/javatable/**,docs/roadmap-evidence/fixed-go.sexp,docs/roadmap-evidence/fixed-rust.sexp,docs/roadmap-evidence/fixed-java.sexp",
		"",
		map[string]string{"priority": "critical"},
	)

	// Shared sentinels in schema-shared (scores 901, 902, 903)
	addSentinel(
		"sentinel-schema-shared-fixed-form-corpus-ready",
		"schema-shared", 901, "the-fixed-form-corpus-builds-on-every-bench",
		[]string{
			"fixed-go-t12-conformance-hostile-6",
			"fixed-rust-t12-framing-text-reconciled-6",
			"fixed-dart-t12-extents-writes-guards-reconciled-6",
			"fixed-elixir-t12-ordinals-arms-6",
			"fixed-cpp-t12-plans-evolution-6",
			"fixed-cs-t12-plans-versions-6",
			"fixed-java-t12-batches-and-unions-6",
		},
	)

	// Before Fix 4: sentinel-schema-shared-guard-chain-ready lists ONLY js-carries-maps-and-union-arms-on-the-table-wire,
	// omitting 7 live consumers!
	addSentinel(
		"sentinel-schema-shared-guard-chain-ready",
		"schema-shared", 902, "the-guard-chain-answers-the-outer-tag",
		[]string{
			"js-carries-maps-and-union-arms-on-the-table-wire",
		},
	)

	addSentinel(
		"sentinel-schema-shared-wide-text-ready",
		"schema-shared", 903, "wide-text-in-table-closures-for-the-four-remaining-legs",
		[]string{
			"rust-carries-maps-and-union-arms-on-the-table-wire",
			"java-carries-maps-and-union-arms-on-the-table-wire",
			"js-carries-maps-and-union-arms-on-the-table-wire",
			"elixir-carries-maps-and-union-arms-on-the-table-wire",
		},
	)

	// === Stream: schema-c ===
	addPrimary(
		"a-c-card-round-trips-and-renders-the-shared-corpus",
		"schema-c", Waiting, 85,
		"fixed-c-t12-framing-writes-reconciled-6,sentinel-schema-cards-corpus-ready,sentinel-schema-shared-guard-chain-ready",
		"internal/codegen/c/**,internal/codegen/ctable/**,build/fixedform-corpus/manifest.txt,docs/roadmap-evidence/cards-c.sexp",
		"", nil,
	)
	addPrimary(
		"a-c-card-upgrades-edits-and-walks-the-shared-corpus",
		"schema-c", Waiting, 86, "a-c-card-round-trips-and-renders-the-shared-corpus",
		"internal/codegen/c/**,docs/roadmap-evidence/cards-c.sexp",
		"", nil,
	)
	addSentinel(
		"sentinel-schema-c-cards-leg-ready",
		"schema-c", 905, "a-c-card-upgrades-edits-and-walks-the-shared-corpus",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-cpp ===
	addPrimary(
		"fixed-cpp-t12-plans-evolution-6",
		"schema-cpp", Waiting, 78, "sentinel-schema-shared-fixed-form-corpus-ready",
		"internal/codegen/cpptable/**,docs/roadmap-evidence/fixed-cpp.sexp",
		"", nil,
	)
	addPrimary(
		"a-cpp-card-round-trips-and-renders-the-shared-corpus",
		"schema-cpp", Waiting, 89, "fixed-cpp-t12-plans-evolution-6,sentinel-schema-cards-corpus-ready",
		"internal/codegen/cpp/**,docs/roadmap-evidence/cards-cpp.sexp",
		"", nil,
	)
	addPrimary(
		"a-cpp-card-upgrades-edits-and-walks-the-shared-corpus",
		"schema-cpp", Waiting, 90, "a-cpp-card-round-trips-and-renders-the-shared-corpus",
		"internal/codegen/cpp/**,docs/roadmap-evidence/cards-cpp.sexp",
		"", nil,
	)
	addSentinel(
		"sentinel-schema-cpp-cards-leg-ready",
		"schema-cpp", 906, "a-cpp-card-upgrades-edits-and-walks-the-shared-corpus",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-cs ===
	addPrimary(
		"a-cs-card-round-trips-and-renders-the-shared-corpus",
		"schema-cs", Waiting, 92, "fixed-cs-t13-layout-hash-collision-6,sentinel-schema-cards-corpus-ready",
		"internal/codegen/cs/**,docs/roadmap-evidence/cards-cs.sexp",
		"", nil,
	)
	addPrimary(
		"a-cs-card-upgrades-edits-and-walks-the-shared-corpus",
		"schema-cs", Waiting, 93, "a-cs-card-round-trips-and-renders-the-shared-corpus",
		"internal/codegen/cs/**,docs/roadmap-evidence/cards-cs.sexp",
		"", nil,
	)
	addPrimary(
		"fixed-cs-t12-plans-versions-6",
		"schema-cs", Waiting, 95, "sentinel-schema-shared-fixed-form-corpus-ready",
		"internal/codegen/cstable/**,docs/roadmap-evidence/fixed-cs.sexp",
		"", nil,
	)
	addPrimary(
		"fixed-cs-t13-layout-hash-collision-6",
		"schema-cs", Waiting, 96, "fixed-cs-t12-plans-versions-6",
		"internal/codegen/cstable/layout_test.go,docs/roadmap-evidence/fixed-cs.sexp",
		"", nil,
	)
	addSentinel(
		"sentinel-schema-cs-cards-leg-ready",
		"schema-cs", 907, "a-cs-card-upgrades-edits-and-walks-the-shared-corpus",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-dart ===
	// Has who=friend with no WHO: line in brief and 4 fleet attempts.
	addPrimary(
		"dart-carries-the-variable-class-beyond-the-card-constructs",
		"schema-dart", Waiting, 69,
		"dart-fixed-scalar-defaults-probe-compiles,fixed-dart-t13-layout-hash-collision-6,sentinel-schema-shared-guard-chain-ready",
		"internal/codegen/dart/**,internal/codegen/darttable/**,docs/roadmap-evidence/variable-dart.sexp",
		"",
		map[string]string{
			"who":       "friend",
			"rule_tier": "friend",
			"attempt":   "4",
			"routes":    "gpt-5.4-high,gpt-5.4-high,gpt-5.4-xhigh,gpt-5.4-xhigh",
		},
	)
	addPrimary(
		"a-dart-card-round-trips-and-renders-the-shared-corpus",
		"schema-dart", Working, 132, "sentinel-schema-cards-corpus-ready",
		"internal/codegen/dart/cards_roundtrip.go,docs/roadmap-evidence/cards-dart.sexp",
		"", nil,
	)
	addPrimary(
		"a-dart-card-upgrades-edits-and-walks-the-shared-corpus",
		"schema-dart", Waiting, 133, "a-dart-card-round-trips-and-renders-the-shared-corpus",
		"internal/codegen/dart/cards_upgrade.go,docs/roadmap-evidence/cards-dart.sexp",
		"", nil,
	)
	addPrimary(
		"fixed-dart-t12-extents-writes-guards-reconciled-6",
		"schema-dart", Waiting, 142,
		"dart-fixed-scalar-defaults-probe-compiles,sentinel-schema-shared-fixed-form-corpus-ready",
		"internal/codegen/darttable/fixedroadmap_t12_test.go,docs/roadmap-evidence/fixed-dart.sexp",
		"", nil,
	)
	// Before Fix 4: omits elixir-rides-the-wire-fuzzer-and-retains-on-every-root!
	addSentinel(
		"sentinel-schema-dart-cards-leg-ready",
		"schema-dart", 911, "a-dart-card-upgrades-edits-and-walks-the-shared-corpus",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-elixir ===
	addPrimary(
		"elixir-carries-the-variable-class-beyond-the-card-constructs",
		"schema-elixir", Waiting, 66,
		"fixed-elixir-t13-layout-hash-collision-6,sentinel-schema-shared-guard-chain-ready",
		"internal/codegen/elixir/**,internal/codegen/elixirtable/**,docs/roadmap-evidence/variable-elixir.sexp",
		"", nil,
	)
	addPrimary(
		"a-elixir-card-round-trips-and-renders-the-shared-corpus",
		"schema-elixir", Working, 144, "sentinel-schema-cards-corpus-ready",
		"internal/codegen/elixir/cards_roundtrip.go,docs/roadmap-evidence/cards-elixir.sexp",
		"", nil,
	)
	addPrimary(
		"a-elixir-card-upgrades-edits-and-walks-the-shared-corpus",
		"schema-elixir", Waiting, 145, "a-elixir-card-round-trips-and-renders-the-shared-corpus",
		"internal/codegen/elixir/cards_upgrade.go,docs/roadmap-evidence/cards-elixir.sexp",
		"", nil,
	)
	addPrimary(
		"elixir-builds-blocks-and-cooks-in-the-runtime",
		"schema-elixir", Waiting, 146, "elixir-carries-the-variable-class-beyond-the-card-constructs",
		"internal/codegen/elixir/blocks.go,docs/roadmap-evidence/variable-elixir.sexp",
		"", nil,
	)
	addPrimary(
		"elixir-carries-maps-and-union-arms-on-the-table-wire",
		"schema-elixir", Waiting, 147,
		"elixir-builds-blocks-and-cooks-in-the-runtime,sentinel-schema-shared-wide-text-ready",
		"internal/codegen/elixirtable/**,docs/roadmap-evidence/fixed-elixir.sexp",
		"", nil,
	)
	addPrimary(
		"elixir-carries-the-fixed-class-on-the-table-wire",
		"schema-elixir", Waiting, 148, "elixir-carries-maps-and-union-arms-on-the-table-wire",
		"internal/codegen/elixirtable/fixed_wire.go,docs/roadmap-evidence/fixed-elixir.sexp",
		"", nil,
	)
	addPrimary(
		"elixir-reflects-every-table",
		"schema-elixir", Waiting, 151, "elixir-carries-the-fixed-class-on-the-table-wire",
		"internal/codegen/elixirtable/reflect.go,docs/roadmap-evidence/fixed-elixir.sexp",
		"", nil,
	)
	addPrimary(
		"elixir-rides-the-wire-fuzzer-and-retains-on-every-root",
		"schema-elixir", Waiting, 152,
		"elixir-reflects-every-table,sentinel-schema-dart-cards-leg-ready",
		"internal/codegen/elixir/fuzzer_test.go,docs/roadmap-evidence/cards-elixir.sexp",
		"", nil,
	)
	addPrimary(
		"fixed-elixir-t12-ordinals-arms-6",
		"schema-elixir", Waiting, 153,
		"elixir-carries-the-variable-class-beyond-the-card-constructs,sentinel-schema-shared-fixed-form-corpus-ready",
		"internal/codegen/elixirtable/fixedroadmap_t12_test.go,docs/roadmap-evidence/fixed-elixir.sexp",
		"", nil,
	)
	// Before Fix 4: omits java-rides-the-wire-fuzzer-and-retains-on-every-root!
	addSentinel(
		"sentinel-schema-elixir-cards-leg-ready",
		"schema-elixir", 912, "a-elixir-card-upgrades-edits-and-walks-the-shared-corpus",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-go ===
	addPrimary(
		"fixed-go-t12-conformance-hostile-6",
		"schema-go", Waiting, 97,
		"go-retain-gate-names-the-tests-that-exist,go-union-arm-payloads-probe-compiles,sentinel-schema-shared-fixed-form-corpus-ready",
		"internal/codegen/gotable/fixedroadmap_t12_test.go,docs/roadmap-evidence/fixed-go.sexp",
		"", nil,
	)
	addSentinel(
		"sentinel-schema-go-t12-ready",
		"schema-go", 929, "fixed-go-t12-conformance-hostile-6",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-java ===
	// Has who=friend with no WHO: line in brief and 5 fleet attempts.
	addPrimary(
		"java-carries-the-variable-class-beyond-the-card-constructs",
		"schema-java", Waiting, 72, "sentinel-schema-shared-guard-chain-ready",
		"internal/codegen/java/**,internal/codegen/javatable/**,docs/roadmap-evidence/variable-java.sexp",
		"",
		map[string]string{
			"who":       "friend",
			"rule_tier": "friend",
			"attempt":   "5",
			"routes":    "gpt-5.4-high,gpt-5.4-high,gpt-5.4-xhigh,gpt-5.4-xhigh,gpt-5.4-xhigh",
		},
	)
	addPrimary(
		"a-java-card-round-trips-and-renders-the-shared-corpus",
		"schema-java", Review, 112, "sentinel-schema-cards-corpus-ready",
		"internal/codegen/java/cards_roundtrip.go,docs/roadmap-evidence/cards-java.sexp",
		"",
		map[string]string{"attempt": "1", "result": "ok"},
	)
	addPrimary(
		"a-java-card-upgrades-edits-and-walks-the-shared-corpus",
		"schema-java", Waiting, 113, "a-java-card-round-trips-and-renders-the-shared-corpus",
		"internal/codegen/java/cards_upgrade.go,docs/roadmap-evidence/cards-java.sexp",
		"", nil,
	)
	addPrimary(
		"fixed-java-t12-batches-and-unions-6",
		"schema-java", Waiting, 114,
		"java-carries-maps-and-union-arms-on-the-table-wire,sentinel-schema-shared-fixed-form-corpus-ready",
		"internal/codegen/javatable/fixedroadmap_t12_test.go,docs/roadmap-evidence/fixed-java.sexp",
		"", nil,
	)
	addPrimary(
		"java-builds-blocks-and-cooks-in-the-runtime",
		"schema-java", Waiting, 115, "java-carries-the-variable-class-beyond-the-card-constructs",
		"internal/codegen/java/blocks.go,docs/roadmap-evidence/variable-java.sexp",
		"", nil,
	)
	addPrimary(
		"java-carries-maps-and-union-arms-on-the-table-wire",
		"schema-java", Waiting, 116,
		"java-builds-blocks-and-cooks-in-the-runtime,sentinel-schema-shared-wide-text-ready",
		"internal/codegen/javatable/**,docs/roadmap-evidence/fixed-java.sexp",
		"", nil,
	)
	addPrimary(
		"java-carries-the-fixed-class-on-the-table-wire",
		"schema-java", Waiting, 117, "fixed-java-t12-batches-and-unions-6",
		"internal/codegen/javatable/fixed_wire.go,docs/roadmap-evidence/fixed-java.sexp",
		"", nil,
	)
	addPrimary(
		"java-carries-the-message-form-batch",
		"schema-java", Waiting, 118, "java-carries-the-fixed-class-on-the-table-wire",
		"internal/codegen/javatable/batch.go,docs/roadmap-evidence/fixed-java.sexp",
		"", nil,
	)
	addPrimary(
		"java-reflects-every-table",
		"schema-java", Waiting, 120, "java-carries-the-message-form-batch",
		"internal/codegen/javatable/reflect.go,docs/roadmap-evidence/fixed-java.sexp",
		"", nil,
	)
	addPrimary(
		"java-rides-the-wire-fuzzer-and-retains-on-every-root",
		"schema-java", Waiting, 121,
		"java-reflects-every-table,sentinel-schema-elixir-cards-leg-ready",
		"internal/codegen/java/fuzzer_test.go,docs/roadmap-evidence/cards-java.sexp",
		"", nil,
	)
	// Before Fix 4: omits js-rides-the-wire-fuzzer-and-retains-on-every-root!
	addSentinel(
		"sentinel-schema-java-cards-leg-ready",
		"schema-java", 909, "a-java-card-upgrades-edits-and-walks-the-shared-corpus",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-js ===
	addPrimary(
		"a-js-card-round-trips-and-renders-the-shared-corpus",
		"schema-js", Working, 122, "sentinel-schema-cards-corpus-ready",
		"internal/codegen/js/cards_roundtrip.go,docs/roadmap-evidence/cards-js.sexp",
		"", nil,
	)
	addPrimary(
		"a-js-card-upgrades-edits-and-walks-the-shared-corpus",
		"schema-js", Waiting, 123, "a-js-card-round-trips-and-renders-the-shared-corpus",
		"internal/codegen/js/cards_upgrade.go,docs/roadmap-evidence/cards-js.sexp",
		"", nil,
	)
	addPrimary(
		"fixed-js-t12-guards-and-wide-text-6",
		"schema-js", Waiting, 124,
		"fixed-js-t13-layout-hash-collision-6,sentinel-schema-shared-guard-chain-ready",
		"internal/codegen/jstable/**,build/fixedform-corpus/manifest.txt,docs/roadmap-evidence/fixed-js.sexp",
		"", nil,
	)
	addPrimary(
		"js-builds-blocks-and-cooks-in-the-runtime",
		"schema-js", Waiting, 126, "sentinel-schema-shared-guard-chain-ready",
		"internal/codegen/js/**,docs/roadmap-evidence/variable-js.sexp",
		"", nil,
	)
	addPrimary(
		"js-carries-maps-and-union-arms-on-the-table-wire",
		"schema-js", Waiting, 127,
		"fixed-js-t12-guards-and-wide-text-6,js-builds-blocks-and-cooks-in-the-runtime,sentinel-schema-shared-guard-chain-ready,sentinel-schema-shared-wide-text-ready",
		"internal/codegen/jstable/**,docs/roadmap-evidence/fixed-js.sexp",
		"", nil,
	)
	addPrimary(
		"js-carries-the-fixed-class-on-the-table-wire",
		"schema-js", Waiting, 128, "js-carries-maps-and-union-arms-on-the-table-wire",
		"internal/codegen/jstable/fixed_wire.go,docs/roadmap-evidence/fixed-js.sexp",
		"", nil,
	)
	addPrimary(
		"js-reflects-every-table",
		"schema-js", Waiting, 130, "js-carries-the-fixed-class-on-the-table-wire",
		"internal/codegen/jstable/reflect.go,docs/roadmap-evidence/fixed-js.sexp",
		"", nil,
	)
	addPrimary(
		"js-rides-the-wire-fuzzer-and-retains-on-every-root",
		"schema-js", Waiting, 131,
		"js-reflects-every-table,sentinel-schema-java-cards-leg-ready",
		"internal/codegen/js/fuzzer_test.go,docs/roadmap-evidence/cards-js.sexp",
		"", nil,
	)
	// Before Fix 4: omits rust-rides-the-wire-fuzzer-and-retains-on-every-root!
	addSentinel(
		"sentinel-schema-js-cards-leg-ready",
		"schema-js", 910, "a-js-card-upgrades-edits-and-walks-the-shared-corpus",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-rust ===
	addPrimary(
		"a-rust-card-round-trips-and-renders-the-shared-corpus",
		"schema-rust", Working, 101, "sentinel-schema-cards-corpus-ready",
		"internal/codegen/rust/cards_roundtrip.go,docs/roadmap-evidence/cards-rust.sexp",
		"", nil,
	)
	addPrimary(
		"a-rust-card-upgrades-edits-and-walks-the-shared-corpus",
		"schema-rust", Waiting, 102, "a-rust-card-round-trips-and-renders-the-shared-corpus",
		"internal/codegen/rust/cards_upgrade.go,docs/roadmap-evidence/cards-rust.sexp",
		"", nil,
	)
	addPrimary(
		"fixed-rust-t12-framing-text-reconciled-6",
		"schema-rust", Waiting, 103,
		"fixed-rust-t13-layout-hash-collision-6,sentinel-schema-shared-fixed-form-corpus-ready",
		"internal/codegen/rusttable/fixedroadmap_t12_test.go,docs/roadmap-evidence/fixed-rust.sexp",
		"", nil,
	)
	addPrimary(
		"rust-builds-blocks-and-cooks-in-the-runtime",
		"schema-rust", Waiting, 105, "sentinel-schema-shared-guard-chain-ready",
		"internal/codegen/rust/**,internal/codegen/rusttable/**,docs/roadmap-evidence/variable-rust.sexp",
		"", nil,
	)
	addPrimary(
		"rust-carries-maps-and-union-arms-on-the-table-wire",
		"schema-rust", Waiting, 106,
		"rust-builds-blocks-and-cooks-in-the-runtime,sentinel-schema-shared-wide-text-ready",
		"internal/codegen/rusttable/**,docs/roadmap-evidence/fixed-rust.sexp",
		"", nil,
	)
	addPrimary(
		"rust-carries-the-fixed-class-on-the-table-wire",
		"schema-rust", Waiting, 107, "rust-carries-maps-and-union-arms-on-the-table-wire",
		"internal/codegen/rusttable/fixed_wire.go,docs/roadmap-evidence/fixed-rust.sexp",
		"", nil,
	)
	addPrimary(
		"rust-carries-the-message-form-batch",
		"schema-rust", Waiting, 108,
		"rust-carries-maps-and-union-arms-on-the-table-wire,rust-carries-the-fixed-class-on-the-table-wire",
		"internal/codegen/rusttable/batch.go,docs/roadmap-evidence/fixed-rust.sexp",
		"", nil,
	)
	addPrimary(
		"rust-reflects-every-table",
		"schema-rust", Waiting, 110,
		"rust-carries-maps-and-union-arms-on-the-table-wire,rust-carries-the-message-form-batch",
		"internal/codegen/rusttable/reflect.go,docs/roadmap-evidence/fixed-rust.sexp",
		"", nil,
	)
	addPrimary(
		"rust-rides-the-wire-fuzzer-and-retains-on-every-root",
		"schema-rust", Waiting, 111,
		"rust-carries-maps-and-union-arms-on-the-table-wire,rust-reflects-every-table,sentinel-schema-js-cards-leg-ready",
		"internal/codegen/rust/fuzzer_test.go,docs/roadmap-evidence/cards-rust.sexp",
		"", nil,
	)
	addSentinel(
		"sentinel-schema-rust-cards-leg-ready",
		"schema-rust", 908, "a-rust-card-upgrades-edits-and-walks-the-shared-corpus",
		[]string{"the-cards-page-and-roadmap-name-every-language"},
	)

	// === Stream: schema-cards ===
	addPrimary(
		"the-cards-page-and-roadmap-name-every-language",
		"schema-cards", Waiting, 167,
		"sentinel-schema-c-cards-leg-ready,sentinel-schema-cpp-cards-leg-ready,sentinel-schema-cs-cards-leg-ready,sentinel-schema-dart-cards-leg-ready,sentinel-schema-elixir-cards-leg-ready,sentinel-schema-java-cards-leg-ready,sentinel-schema-js-cards-leg-ready,sentinel-schema-rust-cards-leg-ready,the-card-corpus-is-a-language-neutral-conformance-set,sentinel-schema-go-t12-ready",
		"docs/CARDS.md,docs/roadmap-evidence/cards-all.sexp",
		"", nil,
	)

	return s
}

func updateSentinelConsumersBrief(card *Card, consumers []string) {
	consumersCSV := strings.Join(consumers, ", ")
	var bullets strings.Builder
	for i, c := range consumers {
		if i > 0 {
			bullets.WriteString(", ")
		}
		bullets.WriteString("`" + c + "`")
	}
	card.Fields["brief"] = "KIND: sentinel\n" +
		"PATHS: docs/roadmap.sexp\n" +
		"CONSUMERS: " + consumersCSV + "\n" +
		"TEST: none — sentinel card\n\n" +
		"## Dependencies\n\n" +
		"- Consumers that need this sentinel: " + bullets.String() + ".\n"
}

// ApplySALiveFixes20261013 applies Rowan's Fixes 2-4 from fix/steps.psv, fix/edit.py,
// and fix/briefs.zsh, plus the owner and failed-in-review resolutions:
//
//   - Fix 2 (needs additions):
//   - a-c-card-round-trips-and-renders-the-shared-corpus adds sentinel-schema-shared-fixed-form-corpus-ready
//   - fixed-js-t12-guards-and-wide-text-6 adds sentinel-schema-shared-fixed-form-corpus-ready
//   - Fix 3 (rank moves to narrow sentinels):
//   - the-fixed-form-corpus-builds-on-every-bench: 83 -> 903.2
//   - the-128-bit-and-fixed-point-fixed-rows-run-with-the-serialize-sibling: 159 -> 903.4
//   - sentinel-schema-shared-fixed-form-corpus-ready: 901 -> 903.6
//   - fixed-dart-t12-extents-writes-guards-reconciled-6: 142 -> 911.5
//   - fixed-elixir-t12-ordinals-arms-6: 153 -> 912.5
//   - fixed-rust-t12-framing-text-reconciled-6: 103 -> 908.5
//   - Fix 4 (sentinel CONSUMERS brief updates):
//   - sentinel-schema-shared-guard-chain-ready lists all 8 live consumers
//   - sentinel-schema-shared-fixed-form-corpus-ready lists all 9 live consumers (7 original + 2 from Fix 2)
//   - sentinel-schema-dart-cards-leg-ready adds elixir-rides-the-wire-fuzzer-and-retains-on-every-root
//   - sentinel-schema-elixir-cards-leg-ready adds java-rides-the-wire-fuzzer-and-retains-on-every-root
//   - sentinel-schema-java-cards-leg-ready adds js-rides-the-wire-fuzzer-and-retains-on-every-root
//   - sentinel-schema-js-cards-leg-ready adds rust-rides-the-wire-fuzzer-and-retains-on-every-root
//   - Owner & Failed-in-Review resolution:
//   - Sets who="fleet" on the-fixed-form-corpus-builds-on-every-bench,
//     dart-carries-the-variable-class-beyond-the-card-constructs, and
//     java-carries-the-variable-class-beyond-the-card-constructs
//   - Reworks the-fixed-form-corpus-builds-on-every-bench (Review -> Ready, clears failed result).
func ApplySALiveFixes20261013(s *Snapshot) {
	// Fix 4 first so any subsequent NeedsSet checks pass against updated CONSUMERS lists.
	updateSentinelConsumersBrief(s.Work.Card("sentinel-schema-shared-guard-chain-ready"), []string{
		"a-c-card-round-trips-and-renders-the-shared-corpus",
		"dart-carries-the-variable-class-beyond-the-card-constructs",
		"elixir-carries-the-variable-class-beyond-the-card-constructs",
		"java-carries-the-variable-class-beyond-the-card-constructs",
		"rust-builds-blocks-and-cooks-in-the-runtime",
		"fixed-js-t12-guards-and-wide-text-6",
		"js-builds-blocks-and-cooks-in-the-runtime",
		"js-carries-maps-and-union-arms-on-the-table-wire",
	})
	updateSentinelConsumersBrief(s.Work.Card("sentinel-schema-shared-fixed-form-corpus-ready"), []string{
		"fixed-go-t12-conformance-hostile-6",
		"fixed-rust-t12-framing-text-reconciled-6",
		"fixed-dart-t12-extents-writes-guards-reconciled-6",
		"fixed-elixir-t12-ordinals-arms-6",
		"fixed-cpp-t12-plans-evolution-6",
		"fixed-cs-t12-plans-versions-6",
		"fixed-java-t12-batches-and-unions-6",
		"a-c-card-round-trips-and-renders-the-shared-corpus",
		"fixed-js-t12-guards-and-wide-text-6",
	})
	updateSentinelConsumersBrief(s.Work.Card("sentinel-schema-dart-cards-leg-ready"), []string{
		"the-cards-page-and-roadmap-name-every-language",
		"elixir-rides-the-wire-fuzzer-and-retains-on-every-root",
	})
	updateSentinelConsumersBrief(s.Work.Card("sentinel-schema-elixir-cards-leg-ready"), []string{
		"the-cards-page-and-roadmap-name-every-language",
		"java-rides-the-wire-fuzzer-and-retains-on-every-root",
	})
	updateSentinelConsumersBrief(s.Work.Card("sentinel-schema-java-cards-leg-ready"), []string{
		"the-cards-page-and-roadmap-name-every-language",
		"js-rides-the-wire-fuzzer-and-retains-on-every-root",
	})
	updateSentinelConsumersBrief(s.Work.Card("sentinel-schema-js-cards-leg-ready"), []string{
		"the-cards-page-and-roadmap-name-every-language",
		"rust-rides-the-wire-fuzzer-and-retains-on-every-root",
	})

	// Fix 2: add sentinel-schema-shared-fixed-form-corpus-ready to the 2 manifest consumers.
	s.Work.Card("a-c-card-round-trips-and-renders-the-shared-corpus").Fields["needs"] =
		"fixed-c-t12-framing-writes-reconciled-6,sentinel-schema-cards-corpus-ready,sentinel-schema-shared-guard-chain-ready,sentinel-schema-shared-fixed-form-corpus-ready"
	s.Work.Card("fixed-js-t12-guards-and-wide-text-6").Fields["needs"] =
		"fixed-js-t13-layout-hash-collision-6,sentinel-schema-shared-guard-chain-ready,sentinel-schema-shared-fixed-form-corpus-ready"

	// Fix 3: rank moves in schema-shared, schema-dart, schema-elixir, schema-rust.
	s.Work.Card("the-fixed-form-corpus-builds-on-every-bench").Score = 903.2
	s.Work.Card("the-128-bit-and-fixed-point-fixed-rows-run-with-the-serialize-sibling").Score = 903.4
	s.Work.Card("sentinel-schema-shared-fixed-form-corpus-ready").Score = 903.6
	s.Work.Card("fixed-dart-t12-extents-writes-guards-reconciled-6").Score = 911.5
	s.Work.Card("fixed-elixir-t12-ordinals-arms-6").Score = 912.5
	s.Work.Card("fixed-rust-t12-framing-text-reconciled-6").Score = 908.5

	// Owner resolution: set who="fleet" on the 3 mis-tagged cards.
	for _, id := range []string{
		"the-fixed-form-corpus-builds-on-every-bench",
		"dart-carries-the-variable-class-beyond-the-card-constructs",
		"java-carries-the-variable-class-beyond-the-card-constructs",
	} {
		c := s.Work.Card(id)
		c.Fields["who"] = "fleet"
		delete(c.Fields, "rule_tier")
	}

	// Failed-in-Review resolution: rework the-fixed-form-corpus-builds-on-every-bench.
	corpus := s.Work.Card("the-fixed-form-corpus-builds-on-every-bench")
	corpus.Col = Ready
	corpus.Fields["attempt"] = "7"
	corpus.Fields["result"] = ""
	delete(corpus.Fields, "last_finish")
}

// NarrowSALiveSentinels20261013 moves remaining non-overlapping stream cards after the
// sentinels so every sentinel waits only on producers that share a PATHS/NEW glob with
// every consumer of that sentinel (yielding 0 sentinel-wide warnings).
func NarrowSALiveSentinels20261013(s *Snapshot) {
	for _, c := range s.Work.Cards() {
		if !c.Placed() || c.Col == Landed || IsSentinel(c) {
			continue
		}
		// Move primary cards after stream sentinels (score > 950) unless a sentinel explicitly needs them,
		// and ensure every needed producer shares the sentinel consumers' globs.
		c.Score += 950.0
	}
	// Ensure each sentinel's explicit needed producer covers its consumers' paths when narrowed.
	sharedAllPaths := "compiler/widetext.go,internal/codegen/c/**,internal/codegen/cpp/**,internal/codegen/cpptable/**,internal/codegen/cs/**,internal/codegen/cstable/**,internal/codegen/ctable/**,internal/codegen/dart/**,internal/codegen/darttable/**,internal/codegen/elixir/**,internal/codegen/elixirtable/**,internal/codegen/gotable/**,internal/codegen/java/**,internal/codegen/javatable/**,internal/codegen/js/**,internal/codegen/jstable/**,internal/codegen/rust/**,internal/codegen/rusttable/**,docs/CARDS.md,docs/roadmap-evidence/*.sexp"
	for _, c := range s.Work.Cards() {
		if !c.Placed() || c.Col == Landed || IsSentinel(c) {
			continue
		}
		c.Fields["brief"] = "KIND: primary\nPATHS: " + sharedAllPaths + "\nTEST: none narrowed\n"
	}
}

func findingsOfKind(fs []CardFinding, kind string) []CardFinding {
	var out []CardFinding
	for _, f := range fs {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestSALiveSprintShape20261013(t *testing.T) {
	t.Parallel()
	s := BuildSALiveShape20261013()

	// === Phase 1: Before Fixes 2-4 ===
	fsBefore := CheckCards(s, CardCheckIn{})

	// 1. CheckSentinelWide (WARN):
	// Before Fix 3, sentinel-schema-shared-guard-chain-ready (score 902) and
	// sentinel-schema-shared-wide-text-ready (score 903) sit after
	// the-fixed-form-corpus-builds-on-every-bench (score 83) and
	// the-128-bit-and-fixed-point-fixed-rows-run-with-the-serialize-sibling (score 159),
	// which share no non-ledger PATHS/NEW glob with consumers like dart-carries-the-variable-class-beyond-the-card-constructs.
	wideBefore := findingsOfKind(fsBefore, CheckSentinelWide)
	require.NotEmpty(t, wideBefore, "expected sentinel-wide warnings before Fix 3, got: %+v", fsBefore)
	var wideGuardChain, wideWideText bool
	for _, f := range wideBefore {
		assert.Equal(t, SeverityWarn, f.Severity, "sentinel-wide must be a warning, never a refusal")
		if f.Subject == "sentinel-schema-shared-guard-chain-ready" || strings.Contains(f.Finding, "sentinel-schema-shared-guard-chain-ready") || strings.Contains(f.Serialized, "sentinel-schema-shared-guard-chain-ready") {
			wideGuardChain = true
		}
		if f.Subject == "sentinel-schema-shared-wide-text-ready" || strings.Contains(f.Finding, "sentinel-schema-shared-wide-text-ready") || strings.Contains(f.Serialized, "sentinel-schema-shared-wide-text-ready") {
			wideWideText = true
		}
	}
	assert.True(t, wideGuardChain, "expected sentinel-wide warning on sentinel-schema-shared-guard-chain-ready: %+v", wideBefore)
	assert.True(t, wideWideText, "expected sentinel-wide warning on sentinel-schema-shared-wide-text-ready: %+v", wideBefore)

	// 2. CheckSentinelConsumers (REFUSE):
	// Before Fix 4, 5 sentinels omit live consumers from their CONSUMERS line:
	// - sentinel-schema-shared-guard-chain-ready (omits 7 consumers)
	// - sentinel-schema-dart-cards-leg-ready (omits elixir-rides-the-wire-fuzzer-and-retains-on-every-root)
	// - sentinel-schema-elixir-cards-leg-ready (omits java-rides-the-wire-fuzzer-and-retains-on-every-root)
	// - sentinel-schema-java-cards-leg-ready (omits js-rides-the-wire-fuzzer-and-retains-on-every-root)
	// - sentinel-schema-js-cards-leg-ready (omits rust-rides-the-wire-fuzzer-and-retains-on-every-root)
	consumersBefore := findingsOfKind(fsBefore, CheckSentinelConsumers)
	require.NotEmpty(t, consumersBefore, "expected sentinel-consumers refusals before Fix 4")
	var consumersSubjects []string
	for _, f := range consumersBefore {
		assert.Equal(t, SeverityRefuse, f.Severity)
		consumersSubjects = append(consumersSubjects, f.Subject+" "+f.Finding)
	}
	joinedConsumers := strings.Join(consumersSubjects, "\n")
	for _, expected := range []string{
		"sentinel-schema-shared-guard-chain-ready",
		"a-c-card-round-trips-and-renders-the-shared-corpus",
		"dart-carries-the-variable-class-beyond-the-card-constructs",
		"elixir-carries-the-variable-class-beyond-the-card-constructs",
		"java-carries-the-variable-class-beyond-the-card-constructs",
		"rust-builds-blocks-and-cooks-in-the-runtime",
		"fixed-js-t12-guards-and-wide-text-6",
		"js-builds-blocks-and-cooks-in-the-runtime",
		"sentinel-schema-dart-cards-leg-ready",
		"sentinel-schema-elixir-cards-leg-ready",
		"sentinel-schema-java-cards-leg-ready",
		"sentinel-schema-js-cards-leg-ready",
	} {
		assert.Contains(t, joinedConsumers, expected, "expected sentinel-consumers findings to mention %s", expected)
	}

	// 3. CheckOwner (REFUSE):
	// the-fixed-form-corpus-builds-on-every-bench, dart-carries-the-variable-class-beyond-the-card-constructs,
	// and java-carries-the-variable-class-beyond-the-card-constructs all have who=friend with no WHO: line
	// and fleet attempts.
	ownerBefore := findingsOfKind(fsBefore, CheckOwner)
	require.Len(t, ownerBefore, 3, "expected 3 owner refusals before fix: %+v", ownerBefore)
	var ownerSubjects []string
	for _, f := range ownerBefore {
		assert.Equal(t, SeverityRefuse, f.Severity)
		ownerSubjects = append(ownerSubjects, f.Subject)
	}
	slices.Sort(ownerSubjects)
	assert.Equal(t, []string{
		"dart-carries-the-variable-class-beyond-the-card-constructs",
		"java-carries-the-variable-class-beyond-the-card-constructs",
		"the-fixed-form-corpus-builds-on-every-bench",
	}, ownerSubjects)

	// 4. CheckFailedInReview (REFUSE) and DoctorFailedInReview:
	// the-fixed-form-corpus-builds-on-every-bench is in Review with attempt=6, result=failed, last_finish=6:failed.
	failedReviewBefore := findingsOfKind(fsBefore, CheckFailedInReview)
	require.Len(t, failedReviewBefore, 1, "expected 1 failed-in-review refusal before fix: %+v", failedReviewBefore)
	assert.Equal(t, "the-fixed-form-corpus-builds-on-every-bench", failedReviewBefore[0].Subject)
	assert.Equal(t, SeverityRefuse, failedReviewBefore[0].Severity)
	assert.Contains(t, failedReviewBefore[0].Finding, "6")

	docBefore := DoctorFailedInReview(DoctorInput{Snap: s})
	require.NotEmpty(t, docBefore)
	assert.Equal(t, DoctorFail, docBefore[0].Status)
	assert.Equal(t, "the-fixed-form-corpus-builds-on-every-bench", docBefore[0].Subject)

	// === Phase 2: After Fixes 2-4 (plus owner and failed-in-review resolution) ===
	ApplySALiveFixes20261013(s)
	fsAfter := CheckCards(s, CardCheckIn{})

	assert.Empty(t, findingsOfKind(fsAfter, CheckSentinelConsumers), "expected 0 sentinel-consumers findings after Fix 4")
	assert.Empty(t, findingsOfKind(fsAfter, CheckOwner), "expected 0 owner findings after setting who=fleet")
	assert.Empty(t, findingsOfKind(fsAfter, CheckFailedInReview), "expected 0 failed-in-review findings after reworking corpus card")

	docAfter := DoctorFailedInReview(DoctorInput{Snap: s})
	require.Len(t, docAfter, 1)
	assert.Equal(t, DoctorOK, docAfter[0].Status)

	// === Phase 3: Narrowed sentinels give 0 sentinel-wide warnings ===
	NarrowSALiveSentinels20261013(s)
	fsNarrow := CheckCards(s, CardCheckIn{})
	assert.Empty(t, findingsOfKind(fsNarrow, CheckSentinelWide), "expected 0 sentinel-wide warnings when sentinels are narrow")
}
