package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

func TestARedealCarriesTheNewestPushedHead(t *testing.T) {
	const newest = "89abcdef0123456789abcdef0123456789abcdef"
	p := sprint.Packet{Card: "s1-1.w6", Primary: "s1-1", Attempt: 6, Gen: 1, Branch: "sprint/s1-1.w6.g1.e0", BaseHead: newest, BaseAttempt: 5,
		Brief: "s1-1: a friend's card\nREPO: mas-bandwidth/nova-tools\nCARRY: s1-1 attempt 4 head=" + landHead + "\nTHE TASK. continue"}
	got := friendServerBrief("amy", p)
	assert.Contains(t, got, "CARRY: s1-1 attempt 5 head="+newest+"\n")
	assert.NotContains(t, got, "CARRY: s1-1 attempt 4 head="+landHead)
}
