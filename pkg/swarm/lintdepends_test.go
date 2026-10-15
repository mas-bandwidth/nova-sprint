package swarm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// THE CASES: the 21 briefs as they stood before the 2026-10-15 needs rewrite, in
// testdata/depends/, used as given. dependsFixtureSHA pins each one's bytes, so a fixture
// that drifted is a red test and not a quietly different case (fixtures.sha256 of the card).
var dependsFixtureSHA = map[string]string{
	"ci-receipt-tells-the-owner-and-seat-on-the-bus.md":    "495251b2f632946648dfb712dd44b6bc701cbcc4094f1067a5795c4b5ba63dc8",
	"deal-tells-on-commit-not-on-friend-sync.md":           "6b30ed0b2e77e73444a3007cfb3be1e2f03b598629208609461c61d415bee7db",
	"friend-daemon-tells-state-transitions-on-the-bus.md":  "2fcde40af8ea3b476cc17e385aa5a67033270e4e7d4a5d8ac22135d67b2f212c",
	"friend-status-transitions-push-to-the-coordinator.md": "d448910b3f929336833e8e8b08772ce1d619b9f97ef31830351f560f7c9af611",
	"generated-go-is-lint-clean-and-names-its-sources.md":  "2026a8061bc395a7c2e68947dc119bf3cb961f438c74b356a7bc48b28ecec3ca",
	"go-fixed-load-exports-its-plan-capacity.md":           "53dc7b3e1fd56a3a4a2318abe2447a014b3738d226f6673c0e0beeb19968e693",
	"go-fixed-load-names-every-refusal.md":                 "fc8ec62967be677c114f89d0ee7a859666090fa4f82ffce6cdb0b8bcc9fea233",
	"go-fixed-load-sets-up-once-per-plan.md":               "084d2c207262aa9bfab55510ac0c99a48c2f2adc13d9d0860f0eef75ee275975",
	"go-tables-get-a-strict-load.md":                       "a956a5886030a9168b621767a98f44ef1f7c70d65e5c00227a5cd6ef89446511",
	"l1-defs-envelope-pong-ack-beat-hook.md":               "e7b36ac097fa638e9175090e6e0563b65dc70f71538dd304f8c10c9c7df81f80",
	"layout-newer-only-where-the-lineage-proves-it.md":     "9c416f7f9087bc8a024c3609877781c7d9d9f0808f022c1ce4c2878f4d13b5e2",
	"local-config-provider-endpoint.md":                    "3dcf082714c3a9c9ae0bfa80177fc03af10c1d83e067baf166d902a0c3d35539",
	"local-engine-line-store-on-a-down-engine.md":          "d325841dee0aedf15e3acb7418eff4e88a64ebf69732c657e0290377ad9da09d",
	"local-rule1-no-shelling-out.md":                       "9a5a2c52706640d3576b3e1db5fb06de5756c472c5e6831f262f9247addb8e0e",
	"local-serve-prints-the-fleet-rows.md":                 "7da6a8972d806b5d54f5dacc052831813b80912a7c7194cdd04606fe5a356e48",
	"local-serve-refusals-digest-and-exit-code.md":         "0e5bc75b679ff6bfc3404032dd14d1f6bed14209a35dc2d6124614caed8ef37e",
	"local-spec-fleet-provider-per-machine.md":             "c3d9a5fa0dedd204789fbcc1fb08609ed1d6a9dcead5fec8c065e0a0098c8271",
	"local-status-box-fields-always-printed.md":            "c6ce818af06ad97b70cad65f16bb7ee25811b4932a9de3b8aca0f4c920de485d",
	"stream-stop-and-resume-push-a-blocker-to-the-seat.md": "7169b82c8616e280c58e9fc2af90c3a2fda19a912023e309ec70dcb4df9e2c27",
	"table-wire-over-text-transports.md":                   "53b5ac9b619d2e3dd21cdd96ddc615577aafbae84773715ee61f0dda0382cc89",
	"usage-adopting-tables-from-go.md":                     "c804a74106cee02ea0a5568365bfd4e327a861fa63121c4737ef4c9284001a86",
}

// dependsFixtures reads the 21 briefs, each checked against its sha256, keyed by card id.
func dependsFixtures(t *testing.T) map[string][]byte {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "depends", "*.md"))
	require.NoError(t, err)
	require.Len(t, files, len(dependsFixtureSHA), "the card carries 21 fixture briefs")
	out := map[string][]byte{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		sum := sha256.Sum256(raw)
		want, ok := dependsFixtureSHA[filepath.Base(f)]
		require.True(t, ok, "%s is no fixture of the card", f)
		require.Equal(t, want, hex.EncodeToString(sum[:]), "%s differs from the brief the card carries", f)
		out[strings.TrimSuffix(filepath.Base(f), ".md")] = raw
	}
	return out
}

// neededFrom is the lint's lookup over the fixtures: a card the fixtures do not hold is
// unknown, exactly as a card neither in the add nor on the table is.
func neededFrom(fx map[string][]byte) func(string) ([]byte, bool) {
	return func(id string) ([]byte, bool) {
		b, ok := fx[id]
		return b, ok
	}
}

// withDependsOn is a fixture with its DEPENDS-ON: line replaced by value, the rest kept.
func withDependsOn(t *testing.T, raw []byte, value string) []byte {
	t.Helper()
	lines := strings.Split(string(raw), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "DEPENDS-ON:") {
			lines[i] = "DEPENDS-ON: " + value
			return []byte(strings.Join(lines, "\n"))
		}
	}
	t.Fatalf("no DEPENDS-ON: line to replace")
	return nil
}

// reasons counts the findings whose excerpt holds each fragment (a finding with none of
// them counts under "other").
func reasons(fs []CardHeaderFinding) map[string]int {
	out := map[string]int{}
	for _, f := range fs {
		switch {
		case f.Check == DependsUnknownCheck:
			out["unknown"]++
		case strings.Contains(f.Excerpt, "names neither `uses:` nor `overlap:`"):
			out["a"]++
		case strings.Contains(f.Excerpt, "appears nowhere in") || strings.Contains(f.Excerpt, "a path that no PATHS or NEW entry"):
			out["b"]++
		case strings.Contains(f.Excerpt, "`overlap:`") || strings.Contains(f.Excerpt, "overlap file"):
			out["c"]++
		case strings.Contains(f.Excerpt, "rank word"):
			out["d"]++
		default:
			out["other"]++
		}
	}
	return out
}

// TestDependsOnNamesWhatItConsumes pins the DEPENDS-ON grammar (docs/SPEC-CARD-CONTRACT.md
// section 2): an entry that names neither what it uses nor an overlap with its reason is
// refused; a uses the needed card does not produce is refused; an overlap with no why or with
// a file the two cards do not both name is refused; rank words in place of a reason are
// refused. The 21 briefs are the cases the 2026-10-15 needs audit judged.
func TestDependsOnNamesWhatItConsumes(t *testing.T) {
	t.Parallel()
	fx := dependsFixtures(t)
	needed := neededFrom(fx)

	// Refused as written, (a): the edge names no reason. The audit judged every one of these
	// OVERLAP or WRONG. entries is how many entries the line holds; rank is whether a sentence
	// of the body names an id of the line with a rank word, which is (d) as well.
	refused := []struct {
		card    string
		entries int
		rank    bool
	}{
		// the schema-go chain
		{"go-fixed-load-sets-up-once-per-plan", 1, true},
		{"go-fixed-load-exports-its-plan-capacity", 2, true},
		{"go-tables-get-a-strict-load", 1, false}, // its rank words sit in a sentence naming other cards
		{"usage-adopting-tables-from-go", 1, false},
		{"table-wire-over-text-transports", 1, false}, // consumes nothing of the card it needs
		// the push cards' wrong edges to the hook card
		{"ci-receipt-tells-the-owner-and-seat-on-the-bus", 1, true},
		{"deal-tells-on-commit-not-on-friend-sync", 1, false},
		{"stream-stop-and-resume-push-a-blocker-to-the-seat", 1, false},
		// the nova-local main.go chain
		{"local-engine-line-store-on-a-down-engine", 1, false},
		{"local-status-box-fields-always-printed", 1, false},
		{"local-serve-refusals-digest-and-exit-code", 1, false},
		{"local-rule1-no-shelling-out", 1, false},
	}
	for _, c := range refused {
		t.Run("refused as written/"+c.card, func(t *testing.T) {
			t.Parallel()
			got := LintDependsOn(fx[c.card], needed)
			for _, f := range got {
				assert.Contains(t, []string{DependsUsesCheck, DependsUnknownCheck}, f.Check)
				assert.Positive(t, f.Line, "a finding names its line: %+v", f)
				assert.Contains(t, f.Excerpt, "remedy:", "a finding carries its remedy")
				if f.Check == DependsUsesCheck {
					assert.Contains(t, f.Excerpt, "(uses:", "the remedy names the uses form")
					assert.Contains(t, f.Excerpt, "(overlap:", "the remedy names the overlap form")
				}
			}
			r := reasons(got)
			assert.Equal(t, c.entries, r["a"], "one (a) finding per entry: %+v", got)
			assert.Equal(t, c.rank, r["d"] > 0, "(d) is drawn only by a sentence naming an id of the line: %+v", got)
		})
	}

	// Admitted once the line is rewritten: the audit judged these three edges TRUE, each
	// consuming a thing the needed card produces.
	admitted := []struct{ card, line string }{
		{"friend-daemon-tells-state-transitions-on-the-bus", "friend-status-transitions-push-to-the-coordinator (uses: FRIEND STATE)"},
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: endpoint)"},
		{"l1-defs-envelope-pong-ack-beat-hook", "table-wire-over-text-transports (uses: TableRefuseReason)"},
	}
	for _, c := range admitted {
		t.Run("admitted rewritten/"+c.card, func(t *testing.T) {
			t.Parallel()
			raw := withDependsOn(t, fx[c.card], c.line)
			assert.Empty(t, LintDependsOn(raw, needed))
			assert.Equal(t, 0, DependsOverlapCount(raw))
			// the needs the readers take from the entry are its id alone
			assert.Equal(t, []string{DependsOnEntryID(c.line)}, DependsOnIDs(string(raw)))
		})
	}

	// Refused (b): the same three rewritten with a uses the needed card does not produce.
	notProduced := []struct{ card, line string }{
		{"friend-daemon-tells-state-transitions-on-the-bus", "friend-status-transitions-push-to-the-coordinator (uses: SomethingItNeverMakes)"},
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: --no-such-flag)"},
		{"l1-defs-envelope-pong-ack-beat-hook", "table-wire-over-text-transports (uses: TableNoSuchThing)"},
		{"friend-daemon-tells-state-transitions-on-the-bus", "friend-status-transitions-push-to-the-coordinator (uses: pkg/nowhere/made.go)"},
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: endpoint, pkg/config/other.go)"},
		{"l1-defs-envelope-pong-ack-beat-hook", "table-wire-over-text-transports (uses: FRIEND STATE)"},
	}
	for i, c := range notProduced {
		t.Run(fmt.Sprintf("refused not produced/%d/%s", i, c.card), func(t *testing.T) {
			t.Parallel()
			got := LintDependsOn(withDependsOn(t, fx[c.card], c.line), needed)
			require.NotEmpty(t, got)
			r := reasons(got)
			assert.Equal(t, 1, r["b"], "exactly the thing not produced is refused: %+v", got)
			assert.Equal(t, len(got), r["b"], "and nothing else: %+v", got)
			assert.Equal(t, DependsUsesCheck, got[0].Check)
		})
	}

	// What the needed card produces: a path of its PATHS or NEW (a glob as the gate reads it),
	// a symbol of its task text, a verb spelling, a phrase; several things in one entry.
	produced := []struct{ card, line string }{
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: pkg/config/kind.go)"},
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: pkg/config/provider_endpoint_test.go, checkProvider, --local-base)"},
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: cmd/nova-config/anything_test.go)"},                 // inside the glob cmd/nova-config/*_test.go
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: pkg/config/migrations/0045_provider_endpoint.sql)"}, // a NEW path
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: pkg/config)"},                                       // a directory above its files
		{"local-serve-prints-the-fleet-rows", "local-config-provider-endpoint (uses: `nova-sprint routes limit`)"},
		{"friend-daemon-tells-state-transitions-on-the-bus", "friend-status-transitions-push-to-the-coordinator (uses: cmd/nova-hook/serve.go, FRIEND STATE)"},
	}
	for i, c := range produced {
		t.Run(fmt.Sprintf("admitted produced/%d", i), func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, LintDependsOn(withDependsOn(t, fx[c.card], c.line), needed))
		})
	}

	// (c): an overlap kept because a conflict would cost a rerun or a redo; the file in both
	// cards' PATHS or NEW and the reason in words.
	t.Run("overlap/with why is admitted and counted", func(t *testing.T) {
		t.Parallel()
		raw := withDependsOn(t, fx["local-engine-line-store-on-a-down-engine"],
			"local-status-box-fields-always-printed (overlap: cmd/nova-local/main.go; why: both rewrite the status box lines of main.go and a conflict is a full redo)")
		assert.Empty(t, LintDependsOn(raw, needed))
		assert.Equal(t, 1, DependsOverlapCount(raw))
		assert.Equal(t, []string{"local-status-box-fields-always-printed"}, DependsOnIDs(string(raw)))
	})
	t.Run("overlap/the audit's kept overlap is admitted", func(t *testing.T) {
		t.Parallel()
		raw := withDependsOn(t, fx["generated-go-is-lint-clean-and-names-its-sources"],
			"go-fixed-load-exports-its-plan-capacity (overlap: internal/codegen/gotable/fixedform.go; why: it rewrites comment text in many goldens and runs alone in the stream)")
		assert.Empty(t, LintDependsOn(raw, needed))
		assert.Equal(t, 1, DependsOverlapCount(raw))
	})
	overlapRefused := []struct{ name, line, want string }{
		{"no why", "local-status-box-fields-always-printed (overlap: cmd/nova-local/main.go)", "gives no `why:`"},
		{"empty why", "local-status-box-fields-always-printed (overlap: cmd/nova-local/main.go; why: )", "gives no `why:`"},
		{"a why of one word", "local-status-box-fields-always-printed (overlap: cmd/nova-local/main.go; why: conflict)", "gives no `why:`"},
		{"a file only the other card names", "local-status-box-fields-always-printed (overlap: cmd/nova-local/box.go; why: both change the box and a conflict is a redo)", "not in both cards' PATHS or NEW"},
		{"a file only this card names", "local-status-box-fields-always-printed (overlap: docs/TESTS.md; why: both edit the tests page and a conflict is a redo)", ""}, // docs/TESTS.md is in both
		{"a file neither names", "local-status-box-fields-always-printed (overlap: docs/CLI.md; why: both edit the page and a conflict is a redo)", "not in both cards' PATHS or NEW"},
		{"no file", "local-status-box-fields-always-printed (overlap: ; why: both edit the page and a conflict is a redo)", "names no file"},
	}
	for _, c := range overlapRefused {
		t.Run("overlap/refused/"+c.name, func(t *testing.T) {
			t.Parallel()
			got := LintDependsOn(withDependsOn(t, fx["local-engine-line-store-on-a-down-engine"], c.line), needed)
			if c.want == "" {
				assert.Empty(t, got, "docs/TESTS.md is in both cards' PATHS")
				return
			}
			require.NotEmpty(t, got)
			assert.Equal(t, DependsUsesCheck, got[0].Check)
			assert.Contains(t, got[0].Excerpt, c.want)
			assert.Equal(t, len(got), reasons(got)["c"], "only (c): %+v", got)
		})
	}

	// (d): rank words on the line, or in a sentence naming an id of the line, in place of a
	// reason; a lands-sentence is told apart by the entry's uses; an ORDER naming no id of the
	// line draws nothing.
	t.Run("rank/on the DEPENDS-ON line", func(t *testing.T) {
		t.Parallel()
		got := LintDependsOn(withDependsOn(t, fx["friend-daemon-tells-state-transitions-on-the-bus"],
			"friend-status-transitions-push-to-the-coordinator (uses: FRIEND STATE) best after"), needed)
		require.Len(t, got, 1, "%+v", got)
		assert.Contains(t, got[0].Excerpt, `"best after"`)
	})
	t.Run("rank/an ORDER naming no id of the line draws nothing", func(t *testing.T) {
		t.Parallel()
		// go-tables-get-a-strict-load's ORDER says "if both are ready" and "independent of the
		// rest" in sentences that name go-fixed-load-names-every-refusal, which is not on its line
		raw := withDependsOn(t, fx["go-tables-get-a-strict-load"],
			"layout-newer-only-where-the-lineage-proves-it (uses: internal/codegen/gotable/fixedunknownhash_test.go)")
		assert.Contains(t, string(raw), "if both are ready")
		assert.Empty(t, LintDependsOn(raw, needed))
	})
	t.Run("rank/a sentence naming an id of the line is refused", func(t *testing.T) {
		t.Parallel()
		// the same ORDER, with the id it names on the line
		raw := withDependsOn(t, fx["go-tables-get-a-strict-load"],
			"go-fixed-load-names-every-refusal (uses: internal/codegen/gotable/fixedrefusalnames_test.go)")
		got := LintDependsOn(raw, needed)
		require.Len(t, got, 1, "%+v", got)
		assert.Equal(t, 1, reasons(got)["d"])
		assert.Contains(t, got[0].Excerpt, "go-fixed-load-names-every-refusal")
		assert.Equal(t, 32, got[0].Line, "the finding names the line of the sentence")
	})
	t.Run("rank/lands sentence with no uses on the entry", func(t *testing.T) {
		t.Parallel()
		raw := []byte("RESULT: x sha=000000000000 tier: flash\nDEPENDS-ON: a-1 (overlap: f.go; why: a conflict is a full redo)\nPATHS: f.go\n\nOnce a-1 lands the seat sees the line.\n")
		needA := func(id string) ([]byte, bool) {
			return []byte("RESULT: a-1 sha=000000000000 tier: flash\nPATHS: f.go\n\nMake a-1.\n"), id == "a-1"
		}
		got := LintDependsOn(raw, needA)
		require.Len(t, got, 1, "%+v", got)
		assert.Contains(t, got[0].Excerpt, "once a-1 lands")
		assert.Equal(t, 5, got[0].Line)
	})
	t.Run("rank/lands sentence with a uses is a description", func(t *testing.T) {
		t.Parallel()
		raw := []byte("RESULT: x sha=000000000000 tier: flash\nDEPENDS-ON: a-1 (uses: Frobnicate)\nPATHS: f.go\n\nOnce a-1 lands the Frobnicate verb exists and this card calls it.\n")
		needA := func(id string) ([]byte, bool) {
			return []byte("RESULT: a-1 sha=000000000000 tier: flash\nPATHS: f.go\n\nAdd the Frobnicate verb.\n"), id == "a-1"
		}
		assert.Empty(t, LintDependsOn(raw, needA))
	})
	for _, phrase := range []string{"Best after a-1.", "This is independent of a-1.", "Not blocked by a-1.", "Run it after a-1 if both are ready."} {
		t.Run("rank/"+phrase, func(t *testing.T) {
			t.Parallel()
			raw := []byte("RESULT: x sha=000000000000 tier: flash\nDEPENDS-ON: a-1 (uses: Frobnicate)\nPATHS: f.go\n\nORDER. " + phrase + "\n")
			needA := func(id string) ([]byte, bool) {
				return []byte("RESULT: a-1 sha=000000000000 tier: flash\nPATHS: f.go\n\nAdd the Frobnicate verb.\n"), id == "a-1"
			}
			got := LintDependsOn(raw, needA)
			require.Len(t, got, 1, "%+v", got)
			assert.Equal(t, 1, reasons(got)["d"])
		})
	}

	// A needed id the callback cannot find is its own finding, never a pass.
	t.Run("unknown/an id neither in the add nor on the table", func(t *testing.T) {
		t.Parallel()
		got := LintDependsOn(withDependsOn(t, fx["friend-daemon-tells-state-transitions-on-the-bus"],
			"no-such-card-anywhere (uses: FRIEND STATE)"), needed)
		require.Len(t, got, 1, "%+v", got)
		assert.Equal(t, DependsUnknownCheck, got[0].Check)
		assert.Contains(t, got[0].Excerpt, "no-such-card-anywhere")
		assert.Contains(t, got[0].Excerpt, "remedy:")
	})
	t.Run("unknown/a nil lookup finds nothing", func(t *testing.T) {
		t.Parallel()
		got := LintDependsOn(withDependsOn(t, fx["friend-daemon-tells-state-transitions-on-the-bus"],
			"friend-status-transitions-push-to-the-coordinator (uses: FRIEND STATE)"), nil)
		require.Len(t, got, 1)
		assert.Equal(t, DependsUnknownCheck, got[0].Check)
	})
	t.Run("unknown/a needed card with no brief produces nothing", func(t *testing.T) {
		t.Parallel()
		got := LintDependsOn(withDependsOn(t, fx["friend-daemon-tells-state-transitions-on-the-bus"],
			"friend-status-transitions-push-to-the-coordinator (uses: FRIEND STATE)"),
			func(string) ([]byte, bool) { return nil, true })
		require.Len(t, got, 1, "%+v", got)
		assert.Equal(t, DependsUsesCheck, got[0].Check)
		assert.Contains(t, got[0].Excerpt, "has no brief")
	})

	// No line, none and - draw nothing; an owner/repo#n reference is no card.
	for _, v := range []string{"none", "-", "none (first card)", "mas-bandwidth/nova-tools#5096"} {
		t.Run("no need/"+v, func(t *testing.T) {
			t.Parallel()
			raw := withDependsOn(t, fx["friend-daemon-tells-state-transitions-on-the-bus"], v)
			assert.Empty(t, LintDependsOn(raw, needed))
			assert.Empty(t, DependsOnIDs(string(raw)))
		})
	}
	t.Run("no need/no line", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, LintDependsOn(fx["go-fixed-load-names-every-refusal"], needed))
	})
}

// A Needs: line, which the readers take before the DEPENDS-ON: line, takes the same grammar,
// and the split is parenthesis-aware: a reason holds commas.
func TestDependsOnSplitsOutsideParenthesesAndReadsNeedsFirst(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"a (uses: x, y)", "b (overlap: f.go; why: one, two)", "c"},
		SplitDependsOn("a (uses: x, y), b (overlap: f.go; why: one, two) , c"))
	assert.Equal(t, []string{"a", "b", "c"}, DependsOnIDsOf("a (uses: x, y), b (overlap: f.go; why: one, two), c"))
	assert.Equal(t, []string{"a", "a"}, DependsOnIDsOf("a (uses: x), a (uses: y)"), "duplicates are kept for the caller")
	assert.Equal(t, []string{"a"}, DependsOnIDsOf("none, a, -, o/r#4"))
	assert.Equal(t, []string{"a (uses: f(x, y))"}, SplitDependsOn("a (uses: f(x, y))"), "nested parentheses")

	e := ParseDependsEntry("a (uses: `pkg/x/y.go`, Foo(x, y), --flag)")
	assert.Equal(t, "uses", e.Form)
	assert.Equal(t, []string{"pkg/x/y.go", "Foo(x, y)", "--flag"}, e.Uses)
	o := ParseDependsEntry("a (Overlap: docs/CLI.md, docs/SPEC-SPRINT.md; Why: both edit the verb tables)")
	assert.Equal(t, "overlap", o.Form)
	assert.Equal(t, []string{"docs/CLI.md", "docs/SPEC-SPRINT.md"}, o.Files)
	assert.Equal(t, "both edit the verb tables", o.Why)
	assert.Equal(t, "", ParseDependsEntry("a (a note)").Form)
	assert.Equal(t, "", ParseDependsEntry("a").Form)

	needA := func(id string) ([]byte, bool) {
		return []byte("RESULT: a sha=000000000000 tier: flash\nNEW: pkg/x/y.go\n\nAdd Foo.\n"), id == "a"
	}
	good := []byte("RESULT: b sha=000000000000 tier: flash\nNeeds: a (uses: pkg/x/y.go, Foo)\nDEPENDS-ON: a\nPATHS: z.go\n\nUse Foo.\n")
	assert.Empty(t, LintDependsOn(good, needA), "the Needs: line is the one read, as briefNeeds reads it")
	assert.Equal(t, []string{"a"}, DependsOnIDs(string(good)))
	bad := []byte("RESULT: b sha=000000000000 tier: flash\nNeeds: a\nDEPENDS-ON: a (uses: Foo)\nPATHS: z.go\n\nUse Foo.\n")
	got := LintDependsOn(bad, needA)
	require.Len(t, got, 1, "%+v", got)
	assert.Contains(t, got[0].Excerpt, "Needs entry")
	assert.Equal(t, 2, got[0].Line)
}

// The fixtures as written, measured against the needs audit of 2026-10-15 (DEEPEN): every edge
// is refused, and the rewrite the audit's verdict allows passes. TRUE edges keep a uses, a kept
// OVERLAP edge becomes an overlap with its reason, every other edge is dropped. The table is
// logged (go test -v) for the pull request.
func TestDependsOnFixturesAgreeWithTheNeedsAudit(t *testing.T) {
	t.Parallel()
	fx := dependsFixtures(t)
	needed := neededFrom(fx)

	// verdict is the audit's class for the card's edges; rewrite is the line that passes.
	rows := []struct {
		card, verdict, rewrite string
	}{
		{"ci-receipt-tells-the-owner-and-seat-on-the-bus", "WRONG", "none"},
		{"deal-tells-on-commit-not-on-friend-sync", "WRONG", "none"},
		{"stream-stop-and-resume-push-a-blocker-to-the-seat", "WRONG", "none"},
		{"friend-daemon-tells-state-transitions-on-the-bus", "TRUE", "friend-status-transitions-push-to-the-coordinator (uses: FRIEND STATE)"},
		{"friend-status-transitions-push-to-the-coordinator", "OVERLAP-keep", "heartbeat-challenges-only-after-silence (overlap: cmd/nova-hook/serve.go; why: 16 shared files including tla/HookBeat.tla, a model whose conflict costs a TLC rerun)"},
		{"generated-go-is-lint-clean-and-names-its-sources", "OVERLAP-keep", "go-fixed-load-exports-its-plan-capacity (overlap: internal/codegen/gotable/fixedform.go; why: it rewrites comment text in many goldens and runs alone in the stream)"},
		{"go-fixed-load-exports-its-plan-capacity", "OVERLAP", "none"},
		{"go-fixed-load-sets-up-once-per-plan", "OVERLAP", "none"},
		{"go-tables-get-a-strict-load", "OVERLAP", "none"},
		{"l1-defs-envelope-pong-ack-beat-hook", "TRUE and STALE", "table-wire-over-text-transports (uses: TableRefuseReason)"},
		{"layout-newer-only-where-the-lineage-proves-it", "OVERLAP-keep", "fixed-count-and-length-past-bound-refuse-or-clamp (overlap: docs/SPEC-TABLES.md; why: both rewrite section 3.4, the same normative paragraph)"},
		{"local-config-provider-endpoint", "TRUE", "local-spec-fleet-provider-per-machine (uses: TestALocalProviderNamesItsEndpoint)"},
		{"local-engine-line-store-on-a-down-engine", "OVERLAP", "none"},
		{"local-rule1-no-shelling-out", "WRONG", "none"},
		{"local-serve-prints-the-fleet-rows", "TRUE and OVERLAP", "local-config-provider-endpoint (uses: endpoint)"},
		{"local-serve-refusals-digest-and-exit-code", "OVERLAP", "none"},
		{"local-status-box-fields-always-printed", "OVERLAP", "none"},
		{"table-wire-over-text-transports", "OVERLAP", "none"},
		{"usage-adopting-tables-from-go", "OVERLAP", "none"},
	}
	names := make([]string, 0, len(fx))
	for id := range fx {
		names = append(names, id)
	}
	sort.Strings(names)
	var table strings.Builder
	fmt.Fprintf(&table, "\n%-52s %-3s %-16s %-30s %s\n", "card", "n", "verdict", "refused as written", "rewrite that passes")
	byCard := map[string]int{}
	for i, r := range rows {
		byCard[r.card] = i
	}
	for _, id := range names {
		value, _ := CardHeaderValue(fx[id], "DEPENDS-ON")
		entries := DependsOnIDsOf(value)
		got := LintDependsOn(fx[id], needed)
		r := reasons(got)
		var why []string
		for _, k := range []string{"a", "b", "c", "d", "unknown"} {
			if r[k] > 0 {
				why = append(why, fmt.Sprintf("%s x%d", k, r[k]))
			}
		}
		i, judged := byCard[id]
		if len(entries) == 0 {
			assert.Empty(t, got, "%s names no need", id)
			fmt.Fprintf(&table, "%-52s %-3d %-16s %-30s %s\n", id, 0, "-", "-", "(no DEPENDS-ON line)")
			assert.False(t, judged, "%s has no edge for the audit to judge", id)
			continue
		}
		require.True(t, judged, "%s has edges and no audit verdict in the table", id)
		row := rows[i]
		assert.Positive(t, r["a"], "%s: the edge as written names no reason and is refused", id)
		assert.Equal(t, len(entries), r["a"], "%s: every entry is refused for its missing reason", id)
		short := row.rewrite
		if len(short) > 70 {
			short = short[:67] + "..."
		}
		fmt.Fprintf(&table, "%-52s %-3d %-16s %-30s %s\n", id, len(entries), row.verdict, strings.Join(why, " "), short)
		// the rewrite passes when every card it needs is among the fixtures; one outside them
		// (heartbeat-challenges, fixed-count) is judged by its form alone: its only finding is
		// the unknown id
		got = LintDependsOn(withDependsOn(t, fx[id], row.rewrite), needed)
		for _, f := range got {
			assert.Equal(t, DependsUnknownCheck, f.Check, "%s: the rewrite %q draws %+v", id, row.rewrite, f)
		}
		if row.rewrite == "none" {
			assert.Empty(t, got)
		}
	}
	t.Log(table.String())
}
