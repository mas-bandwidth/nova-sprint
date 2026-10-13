package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/mas-bandwidth/nova-sprint/pkg/sprintwire"
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
	fakeNow := time.Now()
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
