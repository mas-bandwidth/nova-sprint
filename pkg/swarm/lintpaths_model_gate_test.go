package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wantModelGate is internal/cardgen's modelGate sentence, which model-gate-named quotes in its
// refusal and remedy when a brief whose PATHS or NEW reaches tla/*.tla or tla/*.cfg omits the
// model gate step or GATE-NEEDS: tla.
const wantModelGate = "The PATHS reach tla/, so the gate also runs the model: on a Linux TLC bench (java, the pinned jar at /opt/tla/tla2tools.jar; never a working machine), for each group g that `go run ./tools/tlacheck groups --root . --stale` lists (the groups of the cases your edit touched), run `make tlc TLC_JAR=/opt/tla/tla2tools.jar TLC_OUT=$JOB/scratch/tlc-$g TLC_GROUP=$g`, then `go run ./tools/tlacheck merge --root . --keep tla/RUNS.tsv --out tla/RUNS.tsv $JOB/scratch/tlc-*/RUNS.tsv`, until the groups command prints []; commit tla/RUNS.tsv with the change, never a row written by hand. The lander refuses a head that edits tla/*.tla or tla/*.cfg without a current RUNS.tsv row for each case it touches, naming the case."

// modelGateFindings is the model-gate-named findings LintBrief draws on raw, with no tree
// handed over (the rule reads the header and STEPs alone).
func modelGateFindings(t *testing.T, raw string) []CardHeaderFinding {
	t.Helper()
	fs, _ := LintBrief([]byte(raw), BriefBase{})
	return findingsWith(fs, "model-gate-named")
}

// rocketnetCookieBrief is the header and steps of rocketnet-cookie-key-rotates-on-its-period
// as sprint 21 admitted it (attempt 3 head 2251e1b was refused by the lander because
// MCRNSecurityTables.cfg had no current RUNS.tsv record), with extraHeader and gateStep
// spliced in.
func rocketnetCookieBrief(extraHeader, gateStep string) string {
	return "RESULT: rocketnet-cookie-key-rotates-on-its-period sha=2251e1b00000 tier: heavy\n" +
		"REPO: mas-bandwidth/rocketnet\n" +
		"BASE: sprint/rocketnet-audit\n" +
		"KIND: fix-red\n" +
		"PATHS: pkg/security/cookie.go, pkg/security/cookie_test.go, tla/RNSecurityTables.tla, tla/MCRNSecurityTables.cfg, tla/RUNS.tsv\n" +
		extraHeader +
		"TEST: pkg/security TestCookieKeyRotatesOnItsPeriod\n\n" +
		"THE TASK. Rotate the cookie key on its period and update tla/RNSecurityTables.tla and tla/MCRNSecurityTables.cfg.\n\n" +
		"STEP 1. Enter the staged checkout with cd $JOB/repo && git log --oneline -1.\n" +
		"STEP 2. Red test first: go test ./pkg/security/ -run TestCookieKeyRotatesOnItsPeriod.\n" +
		"STEP 3. Make it pass in pkg/security/cookie.go and the TLA+ model.\n" +
		"STEP 4. Run the gate: go test -count=1 -timeout 600s ./pkg/security/ ./internal/ci/." + gateStep + "\n"
}

// The model-gate-named rule, replayed from rocketnet-cookie-key-rotates-on-its-period #3:
// when PATHS or NEW reaches a .tla or .cfg under tla/, some STEP must run `make tlc` or
// `tlacheck`, and a GATE-NEEDS line must name tla; else LintBrief refuses with the remedy
// quoting cardgen's modelGate sentence.
func TestAModelCardRunsItsModelInTheGate(t *testing.T) {
	t.Parallel()

	// As admitted in sprint 21: neither a model gate step nor GATE-NEEDS: tla -> refused.
	fs := modelGateFindings(t, rocketnetCookieBrief("", ""))
	require.Len(t, fs, 1, "the rocketnet-cookie brief as admitted is refused")
	assert.Equal(t, 5, fs[0].Line, "the finding points at PATHS")
	assert.Contains(t, fs[0].Excerpt, wantModelGate, "the refusal quotes modelGate's sentence")

	// With cardgen's modelGate step and GATE-NEEDS: linux, tla -> passes.
	assert.Empty(t, modelGateFindings(t, rocketnetCookieBrief("GATE-NEEDS: linux, tla\n", " "+wantModelGate)),
		"with the model gate step and GATE-NEEDS: linux, tla the brief passes")

	// Either half missing still refuses: a STEP with the tool but no GATE-NEEDS: tla, or
	// GATE-NEEDS: linux, tla with no STEP running make tlc or tlacheck.
	require.Len(t, modelGateFindings(t, rocketnetCookieBrief("", " "+wantModelGate)), 1,
		"the model gate step without GATE-NEEDS: tla is refused")
	require.Len(t, modelGateFindings(t, rocketnetCookieBrief("GATE-NEEDS: linux\n", " "+wantModelGate)), 1,
		"GATE-NEEDS without tla is refused")
	require.Len(t, modelGateFindings(t, rocketnetCookieBrief("GATE-NEEDS: linux, tla\n", "")), 1,
		"GATE-NEEDS: linux, tla without a STEP running make tlc or tlacheck is refused")

	// Either `make tlc` or `tlacheck` in a STEP satisfies the tool check when GATE-NEEDS names tla.
	assert.Empty(t, modelGateFindings(t, rocketnetCookieBrief("GATE-NEEDS: linux, tla\n", " Run `make tlc TLC_GROUP=rnsecuritytables`.")))
	assert.Empty(t, modelGateFindings(t, rocketnetCookieBrief("GATE-NEEDS: tla\n", " Run `go run ./tools/tlacheck groups --root . --stale`.")))

	// A .cfg alone under tla/, a glob reaching models, or a new model file on NEW: is held too;
	// non-model files under tla/ (RUNS.tsv, CASES.tsv, README.md) and Go-only cards are not.
	for _, paths := range []string{"tla/MCRNSecurityTables.cfg", "tla/RNSecurityTables.tla", "tla/*.cfg", "tla/*.tla", "tla/*", "tla/**", "tla/"} {
		raw := "card x tier: heavy\nPATHS: " + paths + "\n\nTHE TASK. x\n\nSTEP 1. go test ./...\n"
		require.Len(t, modelGateFindings(t, raw), 1, "PATHS: %s is held to model-gate-named", paths)
	}
	newModel := "card x tier: heavy\nPATHS: pkg/x/a.go\nNEW: tla/MCNew.cfg\n\nTHE TASK. x\n\nSTEP 1. go test ./...\n"
	fs = modelGateFindings(t, newModel)
	require.Len(t, fs, 1, "a new .cfg on NEW: is held to model-gate-named")
	assert.Equal(t, 3, fs[0].Line, "when PATHS has no model, the finding points at NEW")

	for _, paths := range []string{"tla/RUNS.tsv", "tla/CASES.tsv", "tla/README.md", "pkg/security/cookie.go"} {
		raw := "card x tier: heavy\nPATHS: " + paths + "\n\nTHE TASK. x\n\nSTEP 1. go test ./...\n"
		assert.Empty(t, modelGateFindings(t, raw), "PATHS: %s touches no .tla or .cfg under tla/", paths)
	}

	remedy := BriefRemedy("model-gate-named")
	assert.Equal(t, BriefBaseRemedies["model-gate-named"], remedy)
	assert.Contains(t, remedy, wantModelGate, "BriefBaseRemedies quotes modelGate's sentence")
}
