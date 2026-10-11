package sprint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The preflight's rows are a pure function over the facts the verb read
// (docs/SPEC-SPRINT.md section 14, "The server"; doctor): the candidate's version
// answer and its shadow tick's code and text. No store and no binary are needed to
// hold the rule to its line.

// lockedShapeText is the locked-shape refusal's text whole: the columns the store
// lacks and the exact nova-table commands that mend them
// (internal/sprint/store/locked_shape.go).
const lockedShapeText = "fleet is missing finished, redealt; run on a STOPPED machine, before this build is installed (internal/sprint/TABLES.lock): " +
	"nova-table col add fleet finished --after load; nova-table col set fleet --hide finished; nova-table col add fleet redealt --after load"

// failsOf is every FAIL row of the checks.
func failsOf(checks []DoctorCheck) []DoctorCheck {
	var out []DoctorCheck
	for _, c := range checks {
		if c.Status == DoctorFail {
			out = append(out, c)
		}
	}
	return out
}

// a shadow that refused on the locked shape is one FAIL, its evidence the shadow's
// own error text whole and its remedy the commands and the adopt's schema step.
func TestDoctorPreflightLockedShapeIsOneFailCarryingTheTextWhole(t *testing.T) {
	t.Parallel()
	checks := DoctorPreflight(PreflightInput{Binary: "/b/nova-sprint", Version: "v9.9.9", ShadowCode: 2, ShadowText: lockedShapeText})
	fails := failsOf(checks)
	require.Len(t, fails, 1, "one FAIL row: %+v", checks)
	c := fails[0]
	assert.Equal(t, AreaPreflight, c.Area)
	assert.Equal(t, CheckPreflightSchema, c.Check)
	assert.Equal(t, "v9.9.9", c.Subject, "the subject is the candidate's version")
	assert.Equal(t, lockedShapeText, c.Evidence, "the shadow's own error text, whole")
	assert.Equal(t, "run the commands above on a STOPPED machine, or the adopt's schema step", c.Remedy)
}

// a shadow that passed is one OK row naming the candidate's version.
func TestDoctorPreflightPassingShadowIsOK(t *testing.T) {
	t.Parallel()
	checks := DoctorPreflight(PreflightInput{Binary: "/b/nova-sprint", Version: "v9.9.9", ShadowCode: 0})
	assert.Empty(t, failsOf(checks), "no FAIL row: %+v", checks)
	var schema *DoctorCheck
	for i := range checks {
		if checks[i].Check == CheckPreflightSchema {
			schema = &checks[i]
		}
	}
	require.NotNil(t, schema, "a schema row: %+v", checks)
	assert.Equal(t, DoctorOK, schema.Status)
	assert.Equal(t, "v9.9.9", schema.Subject)
}

// a candidate that does not answer version is a FAIL.
func TestDoctorPreflightCandidateThatDoesNotAnswerVersionFails(t *testing.T) {
	t.Parallel()
	checks := DoctorPreflight(PreflightInput{Binary: "/b/nova-sprint", VersionErr: "did not run: exec: no such file or directory"})
	fails := failsOf(checks)
	require.Len(t, fails, 1, "one FAIL row: %+v", checks)
	c := fails[0]
	assert.Equal(t, AreaPreflight, c.Area)
	assert.Equal(t, CheckPreflightVersion, c.Check)
	assert.Contains(t, c.Evidence, "did not run: exec: no such file or directory")
}

// a shadow failure that is not the locked shape is still one FAIL with its text.
func TestDoctorPreflightAnotherShadowFailureIsAFailWithItsText(t *testing.T) {
	t.Parallel()
	checks := DoctorPreflight(PreflightInput{Binary: "/b/nova-sprint", Version: "v9.9.9", ShadowCode: 2, ShadowText: "exited 2: tick: the store refused"})
	fails := failsOf(checks)
	require.Len(t, fails, 1, "one FAIL row: %+v", checks)
	assert.Equal(t, "exited 2: tick: the store refused", fails[0].Evidence)
}

// the gate's later checks are four WARN rows, each saying it was not measured by
// this build, so --preflight always says what it did not check.
func TestDoctorPreflightListsTheLaterChecksUnmeasured(t *testing.T) {
	t.Parallel()
	checks := DoctorPreflight(PreflightInput{Binary: "/b/nova-sprint", Version: "v9.9.9", ShadowCode: 0})
	var warns []DoctorCheck
	for _, c := range checks {
		if c.Status == DoctorWarn {
			warns = append(warns, c)
		}
	}
	require.Len(t, warns, 4, "four later checks: %+v", checks)
	byCheck := map[string]DoctorCheck{}
	for _, c := range warns {
		byCheck[c.Check] = c
	}
	for _, name := range []string{"members", "mirrors", "links", "stable"} {
		c, ok := byCheck[name]
		require.True(t, ok, "the %s check is listed: %+v", name, warns)
		assert.Equal(t, AreaPreflight, c.Area)
		assert.True(t, strings.Contains(c.Evidence, "not measured by this build"), "%s: %q", name, c.Evidence)
	}
}
