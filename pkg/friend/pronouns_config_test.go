package friend

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-tools/pkg/config"
)

// TestPronounSetsMatchConfig: the pronoun sets nova-tools' pkg/friend renders are the ones
// this tree's pkg/config accepts on a friend row, in the same order, with the same default.
// Carried from the deleted pkg/pronouns (its TestPronounSetsMatchConfig): nova-tools checks
// its own pkg/config, and this tree's copy of pkg/config can drift from it.
func TestPronounSetsMatchConfig(t *testing.T) {
	t.Parallel()
	var got []string
	for _, p := range AllPronouns {
		got = append(got, string(p))
	}
	assert.Equal(t, config.FriendPronounSets, got)
	assert.Equal(t, config.DefaultFriendPronouns, string(DefaultPronouns))
	assert.Equal(t, strings.Join(config.FriendPronounSets, ", "), PronounList())
}
