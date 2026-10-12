package gitidentity

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveDefaultsOnlyWhenBothSettingsAreUnset(t *testing.T) {
	get := func(string) string { return "" }
	id, err := Resolve(get)
	require.NoError(t, err)
	require.Equal(t, Identity{Name: DefaultName, Email: DefaultEmail}, id)

	get = func(k string) string {
		if k == NameSetting {
			return "machine"
		}
		return ""
	}
	_, err = Resolve(get)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), EmailSetting), err.Error())
}

func TestOverrideEnvReplacesBothIdentityPairs(t *testing.T) {
	env := OverrideEnv([]string{"PATH=/bin", "GIT_AUTHOR_NAME=host", "GIT_AUTHOR_EMAIL=glenn@example.com", "GIT_COMMITTER_NAME=host", "GIT_COMMITTER_EMAIL=glenn@example.com"}, Identity{Name: "machine", Email: "machine@example.invalid"})
	require.Equal(t, []string{"PATH=/bin", "GIT_AUTHOR_NAME=machine", "GIT_AUTHOR_EMAIL=machine@example.invalid", "GIT_COMMITTER_NAME=machine", "GIT_COMMITTER_EMAIL=machine@example.invalid"}, env)
}
