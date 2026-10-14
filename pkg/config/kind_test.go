package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sprint row's max_attempts (docs/SPEC-SPRINT.md, the bounds; the owner, 2026-10-12: "max
// attempts should be configurable in nova-config, and default to 4"): a whole number from 1 to
// MaxAttemptsCeiling, 4 by default; the check names the field and the remedy.

func sprintKind(t *testing.T) *Kind {
	t.Helper()
	for _, k := range Kinds {
		if k.Name == KindSprint {
			return k
		}
	}
	require.FailNow(t, "no sprint kind")
	return nil
}

func TestTheSprintRowsMaxAttemptsDefaultsToFour(t *testing.T) {
	t.Parallel()
	for _, f := range sprintKind(t).Fields {
		if f.Name == FieldMaxAttempts {
			assert.Equal(t, "4", f.Default, "the default is 4")
			assert.Equal(t, TypeInt, f.Type)
			assert.Contains(t, f.Help, "stream set <s> --attempts <n>", "the help names the stream's own override")
			return
		}
	}
	require.FailNow(t, "the sprint row has no max_attempts field")
}

func TestCheckSprintHoldsMaxAttemptsToAWholeNumberFromOneToTheCeiling(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"0", "-1", "101", "x", "2.5"} {
		err := checkSprint(Row{Fields: map[string]string{FieldMaxAttempts: bad}})
		require.Error(t, err, "max_attempts %q is refused", bad)
		assert.Contains(t, err.Error(), "sprint: "+FieldMaxAttempts, "the field is named")
		assert.Contains(t, err.Error(), "--"+FieldMaxAttempts+" <n>", "and the remedy")
	}
	assert.Contains(t, checkSprint(Row{Fields: map[string]string{FieldMaxAttempts: "101"}}).Error(), "the sprint refuses a cap above 100")
	for _, ok := range []string{"1", "4", "100", ""} {
		require.NoError(t, checkSprint(Row{Fields: map[string]string{FieldMaxAttempts: ok}}), "max_attempts %q is accepted", ok)
	}
	require.NoError(t, checkSprint(Row{Fields: map[string]string{}}), "an absent max_attempts is the default")
}
