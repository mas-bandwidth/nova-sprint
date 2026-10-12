package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// A card a friend holds, whose attempt came back held with a PATHS-PROPOSED line, is the
// coordinator's judgment, never a twin by rule (the owner's rule: "We gotta stop doing this
// twin shit"; the paths rule of docs/SPEC-SPRINT.md section 8). The rule cut a twin <id>-t
// and left <id> off the table: streams listed the twin working, while card <id> read state
// "-" and brief --widen refused "no primary <id> on the work table", so the coordinator
// could not reach the card she knew. Now the paths rule leaves such a card to the
// coordinator: it stays in review, card prints its real state and stored brief, and brief
// --widen/--brief/--brief-file/--tier reach it by id.
func TestAPathsProposedHoldOnACardAFriendHoldsIsTheCoordinators(t *testing.T) {
	t.Parallel()
	r := newLandRig(t)
	// the friend, on the land rig's in-memory store and local bare origin
	cfg := config.NewMem()
	r.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) {
		return cfg.List(ctx, config.KindFriend)
	}
	addFriendRow(t, cfg, "amy")
	root := t.TempDir()
	r.ok("friend sync --root " + root)
	r.beatUp("amy")

	// the card is cut on main, whose PATHS names a.go, a file at the base
	r.commit("a.go", "package a\n", "a.go at the base")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
	r.git(r.worker, "fetch", "-q", "origin")
	brief := writeBrief(t, "fix the empty case, tier: flash\nREPO: "+r.remote+"\nBASE: main\nPATHS: a.go\nTEST: none a fixture of a friend's widen")
	r.promotionStream("s1")
	r.ok("add --stream s1 --count 1 --one --brief-file " + brief)
	r.ok("start")
	r.ok("tick")

	// no WHO: the tick's friend deal hands it to amy, up with room
	var c cardView
	r.json("card s1-1", &c)
	require.Len(t, c.Work, 1)
	require.Equal(t, sprint.FriendRow("amy"), c.Work[0].Row, "a friend holds the card")
	r.ok("friend sync --root " + root)
	r.startFriend("amy", 1)

	// the friend's work pushes b.go on her branch; she HOLDs with PATHS-PROPOSED: b.go
	head := r.head("s1-1", "main", "b.go", "package b\n")
	r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/sprint/s1-1.w1.g1.e0")
	r.a.tip = func(context.Context, string, string) (string, error) { return head, nil }
	outboxReport(t, root, "amy", "s1-1.w1", "Verdict: HOLD\nHead: "+head+"\n\nThe fix needs b.go.\nPATHS-PROPOSED: b.go\n")
	r.ok("friend sync --root " + root)

	// the rules run: the paths rule leaves the friend's card to the coordinator, no twin
	assert.Contains(t, r.ok("rules"), "rule=paths act=left", "the paths rule leaves the judgment, with why")
	assert.Contains(t, r.ok("rules"), "a\\x20friend\\x20holds", "the reason names the friend-held card")
	r.ok("tick --answer-rules")
	r.ok("tick --answer-rules")
	r.ok("tick")
	assert.NotContains(t, r.ok("card s1-1"), "replaced by", "no twin replaces the id she knows")
	code, _, errs := r.do("card s1-1-t")
	assert.Equal(t, 1, code, "no twin: %s", errs)

	// card prints the real state and the stored brief
	r.json("card s1-1", &c)
	assert.Equal(t, sprint.Review, c.Primary.Col, "the card stays in review, not off the table")
	assert.Equal(t, "failed", c.Primary.F("result"))
	assert.Contains(t, r.ok("card s1-1 --brief"), "\nPATHS: a.go\n", "the stored brief is printed")
	assert.Contains(t, r.ok("card s1-1"), "brief:", "the story shows the brief")

	// brief --widen reaches the card by the id the coordinator knows
	out := r.ok("brief s1-1 --widen --repo-dir " + r.clone)
	assert.Contains(t, out, "next, from its last pushed head")
	assert.Contains(t, out, "PATHS: a.go,b.go", "its PATHS widened by the proposal")
	assert.Contains(t, out, "CARRY: s1-1 attempt 1 head="+head)
	r.ok("tick")
	got := r.ok("card s1-1 --brief")
	assert.Contains(t, got, "\nPATHS: a.go,b.go\n", "the widened brief is stored")
	assert.Contains(t, got, "\nCARRY: s1-1 attempt 1 head="+head+"\n")
}
