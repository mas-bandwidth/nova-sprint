package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A state judgment names its machine (the member subject) or its friend (the first
// primary) and its reason (the first line of its text); any other group is none.
func TestAStateJudgmentNamesItsMachineOrFriend(t *testing.T) {
	t.Parallel()
	who, why, ok := StateJudgment(Group{Kind: Judgment, Type: NMemberEnvFault, Stream: MemberSubject("m1"), What: "member m1's environment faulted (push): no credential\nmore"})
	assert.True(t, ok)
	assert.Equal(t, "m1", who)
	assert.Equal(t, "member m1's environment faulted (push): no credential", why)
	who, why, ok = StateJudgment(Group{Kind: Judgment, Type: NFriendStalled, Primaries: []string{"amy"}})
	assert.True(t, ok)
	assert.Equal(t, "amy", who)
	assert.Equal(t, NFriendStalled, why, "no text: the type is the reason")
	_, _, ok = StateJudgment(Group{Kind: Judgment, Type: NTimer})
	assert.False(t, ok, "a timer changes no member's state")
	_, _, ok = StateJudgment(Group{Kind: Happened, Type: NMemberEnvFault})
	assert.False(t, ok, "only a judgment")
}
