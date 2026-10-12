package pronouns

import (
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The names below are made up: no real friend's pronouns are set here, only the
// mechanism (the pronouns setting on a friend row) is held.

// TestSayRendersEachPronounSetAndTheDefault holds the one helper: the three sets
// a row may name, and the set a row that names none (the zero value) goes by.
func TestSayRendersEachPronounSetAndTheDefault(t *testing.T) {
	t.Parallel()
	const text = "{They}/{they}/{them}/{their}/{theirs}/{themself}; {Their} lane, {Them}, {Theirs}, {Themself}"
	for _, tc := range []struct {
		name string
		p    Pronouns
		want string
	}{
		{"she/her", SheHer, "She/she/her/her/hers/herself; Her lane, Her, Hers, Herself"},
		{"he/him", HeHim, "He/he/him/his/his/himself; His lane, Him, His, Himself"},
		{"they/them", TheyThem, "They/they/them/their/theirs/themself; Their lane, Them, Theirs, Themself"},
		{"not stated: the zero value is they/them", Pronouns(""), "They/they/them/their/theirs/themself; Their lane, Them, Theirs, Themself"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.p.Say(text))
		})
	}
	assert.Equal(t, "no braces here", HeHim.Say("no braces here"))
	assert.Equal(t, "kept {literal}", SheHer.Say("kept {literal}"), "a brace word that is not a pronoun is left")
	assert.Equal(t, "his row of 3", HeHim.Sprintf("{their} row of %d", 3), "Sprintf says the pronouns, then formats")
	assert.Equal(t, "their row of {they}", TheyThem.Sprintf("{their} row of %s", "{they}"), "an argument is never read as a pronoun word")
}

func TestParsePronounsReadsTheThreeSetsAndNeverGuesses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   string
		want Pronouns
		ok   bool
	}{
		{"she/her", SheHer, true},
		{"he/him", HeHim, true},
		{"they/them", TheyThem, true},
		{" He/Him ", HeHim, true},
		{"", TheyThem, true},
		{"it/its", TheyThem, false},
		{"she", TheyThem, false},
		{"xe/xem", TheyThem, false},
	} {
		got, ok := ParsePronouns(tc.in)
		assert.Equal(t, tc.want, got, "%q", tc.in)
		assert.Equal(t, tc.ok, ok, "%q", tc.in)
	}
}

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
