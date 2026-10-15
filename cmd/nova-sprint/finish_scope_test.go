package main

import (
	"encoding/json"
	"github.com/mas-bandwidth/nova-sprint/pkg/member"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestFinishScopeFlagReachesServerJudgment(t *testing.T) {
	r := newServerRig(t)
	r.a.now = func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }
	for _, line := range []string{
		"nova-sprint init --readers reader-m2 --members m1:1,m2:1",
		"nova-sprint set --read-cards off",
		"nova-sprint add --one --stream s1 --count 1 --brief-file " + writeBrief(t, "tier: flash\nPATHS: src/a.c\n"),
		"nova-sprint start", "nova-sprint tick", "nova-sprint tick"} {
		r.boss(line)
	}
	done := false
	for _, m := range []string{"m1", "m2"} {
		for _, c := range taken(t, r.one("take", "--as", m, "--limit", "1", "--epoch", "0", "--json")) {
			e := member.FinishScopeEvidence{Card: strings.Split(c, "@")[0], Gen: 1, Head: strings.Repeat("2", 40), Base: strings.Repeat("1", 40), Tracked: []string{"src/a.c"}, Diff: "diff --git a/Makefile b/Makefile\n--- a/Makefile\n+++ b/Makefile\n@@ -1 +1 @@\n-old\n+new\n"}
			wire, err := json.Marshal(e)
			require.NoError(t, err)
			res := r.one("finish", "--as", m, c, "--epoch", "0", "--head", e.Head, "--scope", string(wire))
			require.Equal(t, 0, res.Code, res.Stderr)
			card := r.boss("nova-sprint card s1-1 --json")
			require.Contains(t, card, "E12")
			require.Contains(t, card, "Makefile")
			done = true
		}
	}
	require.True(t, done)
}

// Existing machine-member twins have fake pushes and no checkout. Their collector
// supplies the same bound git envelope as a real machine, with an empty fixture diff.
func twinFinishScope(p member.Packet, head string) (string, error) {
	b, err := json.Marshal(member.FinishScopeEvidence{Card: p.Card, Gen: p.Gen, Epoch: p.Epoch, Base: strings.Repeat("1", 40), Head: head, Tracked: []string{}})
	return string(b), err
}
