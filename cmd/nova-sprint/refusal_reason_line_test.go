package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAFailedStepPrintsOneReasonPerRefusalDirectAndForwarded: a refused ack
// (the same judgment named twice) prints one REFUSED line with its reason and
// a status line carrying why=, identically direct and through the server rig.
func TestAFailedStepPrintsOneReasonPerRefusalDirectAndForwarded(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.timerOnce("stall-check")
	groups := ta.inboxGroups()
	require.NotEmpty(t, groups)
	id := groups[0].Notes[0]
	code, _, errs := ta.do("ack " + id + "," + id + " --reason 'looked'")
	require.Equal(t, 1, code, errs)
	assert.Contains(t, errs, "REFUSED "+id+": named twice")
	assert.Contains(t, errs, "ACK FAILED moved=0 refused=1")
	assert.Contains(t, errs, " why=named\\x20twice")
	ta.clean()

	r := newServerRig(t,
		"nova-sprint init --readers reader-a,reader-b --members m1:2",
		"nova-sprint start",
	)
	fakeNow := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	r.a.now = func() time.Time { return fakeNow }
	r.boss("nova-sprint remind --in 1s --note stall-check")
	fakeNow = fakeNow.Add(2 * time.Second)
	r.boss("nova-sprint tick")
	// find the rig's judgment id from its inbox
	local := func(argv ...string) sprintwire.Result {
		res := r.a.serveFrom(sprintwire.Request{Verbs: [][]string{argv}}, true).Results
		require.Len(t, res, 1)
		return res[0]
	}
	res0 := local("inbox", "--json")
	require.Equal(t, 0, res0.Code, res0.Stderr)
	var in struct {
		Groups []struct {
			Type  string
			Notes []string
		}
	}
	require.NoError(t, json.Unmarshal([]byte(res0.Stdout), &in), res0.Stdout)
	require.NotEmpty(t, in.Groups)
	rid := ""
	for _, g := range in.Groups {
		if g.Type == "timer" && len(g.Notes) > 0 {
			rid = g.Notes[0]
		}
	}
	require.NotEmpty(t, rid, res0.Stdout)
	res := local("ack", rid+","+rid, "--reason", "looked", "--actor", "boss")
	require.Equal(t, 1, res.Code, res.Stderr)
	assert.Contains(t, res.Stderr, "REFUSED "+rid+": named twice")
	assert.Contains(t, res.Stderr, "ACK FAILED moved=0 refused=1")
	assert.Contains(t, res.Stderr, " why=named\\x20twice")
	// forwarded prints exactly what direct prints, modulo the judgment id
	norm := func(s, old string) string { return strings.ReplaceAll(s, old, "<id>") }
	assert.Equal(t, norm(errs, id), norm(res.Stderr, rid))
}

// TestALandingStalledAckPrintsItsReasonDirectAndForwarded: an ack of the
// landing-stalled judgment is refused (its decisions offer no ack), and the
// refusal prints one REFUSED line with its reason and a status line carrying
// why=, identically direct and through the server rig.
func TestALandingStalledAckPrintsItsReasonDirectAndForwarded(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")
	ta.ok("add --stream s1 --count 1 --one")
	ta.deal(1)
	ta.ok("take --as m1 s1-1.w1@1")
	ta.ok("finish --as m1 s1-1.w1@1")
	ta.ok("set --landing-stall-bound 1s")
	ta.ok("start")
	id := ""
	for i := 0; i < 10 && id == ""; i++ {
		ta.mu.Lock()
		ta.now = ta.now.Add(2 * time.Second)
		ta.mu.Unlock()
		ta.ok("tick")
		for _, g := range ta.inboxGroups() {
			if g.Type == "landing stalled" && len(g.Notes) > 0 {
				id = g.Notes[0]
			}
		}
	}
	require.NotEmpty(t, id, "the landing stall judgment reached the inbox")
	code, _, errs := ta.do("ack " + id + " --reason 'looked'")
	require.Equal(t, 1, code, errs)
	assert.Equal(t, 1, strings.Count(errs, "REFUSED "), errs)
	assert.Contains(t, errs, "REFUSED "+id+": ack does not answer")
	assert.Contains(t, errs, "(landing stalled)")
	assert.Contains(t, errs, "ACK FAILED moved=0 refused=1")
	assert.Contains(t, errs, " why=ack\\x20does\\x20not\\x20answer")
	ta.clean()

	r := newServerRig(t,
		"nova-sprint init --readers reader-a,reader-b --members m1:2",
		"nova-sprint add --stream s1 --count 1 --one",
		"nova-sprint start",
	)
	fakeNow := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	r.a.now = func() time.Time { return fakeNow }
	r.boss("nova-sprint set --landing-stall-bound 1s")
	r.boss("nova-sprint tick")
	r.boss("nova-sprint tick")
	cards := taken(t, r.one("take", "--as", "m1", "--limit", "1", "--epoch", "0", "--json"))
	require.Len(t, cards, 1)
	fin := r.one("finish", "--as", "m1", cards[0], "--epoch", "0", "--report", "done", "--head", "0123456")
	require.Equal(t, 0, fin.Code, fin.Stderr)
	local := func(argv ...string) sprintwire.Result {
		res := r.a.serveFrom(sprintwire.Request{Verbs: [][]string{argv}}, true).Results
		require.Len(t, res, 1)
		return res[0]
	}
	rid := ""
	for i := 0; i < 10 && rid == ""; i++ {
		fakeNow = fakeNow.Add(2 * time.Second)
		r.boss("nova-sprint tick")
		res0 := local("inbox", "--json")
		require.Equal(t, 0, res0.Code, res0.Stderr)
		var in struct {
			Groups []struct {
				Type  string
				Notes []string
			}
		}
		require.NoError(t, json.Unmarshal([]byte(res0.Stdout), &in), res0.Stdout)
		for _, g := range in.Groups {
			if g.Type == "landing stalled" && len(g.Notes) > 0 {
				rid = g.Notes[0]
			}
		}
	}
	require.NotEmpty(t, rid, "the rig's landing stall judgment reached the inbox")
	res := local("ack", rid, "--reason", "looked", "--actor", "boss")
	require.Equal(t, 1, res.Code, res.Stderr)
	assert.Equal(t, 1, strings.Count(res.Stderr, "REFUSED "), res.Stderr)
	assert.Contains(t, res.Stderr, "REFUSED "+rid+": ack does not answer")
	assert.Contains(t, res.Stderr, "(landing stalled)")
	assert.Contains(t, res.Stderr, "ACK FAILED moved=0 refused=1")
	assert.Contains(t, res.Stderr, " why=ack\\x20does\\x20not\\x20answer")
	// forwarded prints exactly what direct prints, modulo the judgment id
	norm := func(s, old string) string { return strings.ReplaceAll(s, old, "<id>") }
	assert.Equal(t, norm(errs, id), norm(res.Stderr, rid))
}
