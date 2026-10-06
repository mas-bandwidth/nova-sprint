package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// release check (docs/SPEC-SPRINT.md, release-check-cold-audit-r-ns-b2.w1), on the
// twin store with the clock the test moves and no socket: --audit asks each
// sampled card of a reader that never saw it, and a plain check is ok only once
// every one of the audit's own reads came back ok.
func TestReleaseCheckColdAuditOnTheStore(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b,reader-c --members m1,m2 --coordinator coordinator")
	ta.ok("add --stream s1 --count 25")
	ta.ok("start")
	ta.ok("tick")
	for _, m := range []string{"m1", "m2"} {
		ta.ok("take --as " + m + " --max 20")
		var q struct{ Cards []queueCard }
		ta.json("queue --as "+m, &q)
		var words []string
		for _, c := range q.Cards {
			words = append(words, fmt.Sprintf("%s@%d", c.ID, c.Gen))
		}
		if len(words) > 0 {
			ta.ok("finish --as " + m + " " + strings.Join(words, " "))
		}
	}
	ta.ok("ask")
	for _, r := range []string{"reader-a", "reader-b", "reader-c"} {
		ta.do("read --as " + r + " --ok --max 30 --epoch 0")
	}
	ta.ok("accept --read-ok")
	ta.ok("merge --stream s1 --batch 30")
	ta.ok("tick")
	since := t0.Add(-time.Hour).Format(time.RFC3339)

	code, out, _ := ta.do("release check")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "RELEASE CHECK cold-audit fail no cold audit has been asked")
	assert.Contains(t, out, "RELEASE NOT READY failed=1")

	code, _, errs := ta.do("release check --audit --actor anon --since " + since)
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "coordinator's alone")
	code, _, errs = ta.do("release check --audit --actor coordinator --since nope --repo-dir " + t.TempDir())
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "--since nope is not an RFC 3339 time")

	out = ta.ok("release check --audit --actor coordinator --seed 42 --since " + since)
	assert.Contains(t, out, "COLD AUDIT seed=42 since="+since+" cards=20")
	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
	s, err := st.Load(t.Context(), []string{sprint.Work, sprint.Readers}, nil)
	require.NoError(t, err)
	rec, ok, err := sprint.ReadColdAudit(s)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, rec.Reads, sprint.ColdAuditCards)
	for _, r := range rec.Reads {
		rc := s.Readers.Card(r.Read)
		require.NotNil(t, rc, r.Read)
		assert.NotContains(t, sprint.Split(s.Work.Card(r.Card).F("asked")), rc.Row, "%s asked of a reader that read it", r.Card)
	}

	code, out, _ = ta.do("release check")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "20 unanswered")

	// a broken cold read is named with its finding; the rest come back ok
	first := rec.Reads[0]
	rc := s.Readers.Card(first.Read)
	ta.ok(fmt.Sprintf("read --as %s --broken --finding 'docs/SPEC.md:12: the comma is missing; add it' --epoch 0 %s", rc.Row, first.Read))
	for _, r := range []string{"reader-a", "reader-b", "reader-c"} {
		ta.do("read --as " + r + " --ok --max 30 --epoch 0")
	}
	code, out, _ = ta.do("release check")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "1 broken: "+first.Card+" ("+rc.Row+"): docs/SPEC.md:12: the comma is missing")

	// the same seed redraws the same sample; every read ok is RELEASE OK
	ta.a.sleep(time.Minute)
	ta.ok("release check --audit --actor coordinator --seed 42 --since " + since)
	for _, r := range []string{"reader-a", "reader-b", "reader-c"} {
		ta.do("read --as " + r + " --ok --max 30 --epoch 0")
	}
	code, out, errs = ta.do("release check")
	require.Equal(t, 0, code, "%s %s", out, errs)
	assert.Contains(t, out, "RELEASE CHECK cold-audit ok all 20 cold reads ok (seed 42")
	assert.Contains(t, out, "RELEASE OK checks=1")
	code, out, _ = ta.do("release check --json")
	assert.Equal(t, 0, code)
	var rep sprint.ReleaseReport
	require.NoError(t, json.Unmarshal([]byte(out), &rep))
	assert.True(t, rep.Ready)

	ta.a.sleep(sprint.ColdAuditMaxAge)
	code, out, _ = ta.do("release check")
	assert.Equal(t, 1, code)
	assert.Contains(t, out, "limit 48h0m0s; run: nova-sprint release check --audit")
}

func TestReleaseCheckRefusesAnUnknownCheckAndWords(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	code, _, errs := ta.do("release check --check nope")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "no release check named nope; the checks are cold-audit")
	code, _, errs = ta.do("release check s1-1")
	assert.Equal(t, 2, code, errs)
}

func TestAddRefusesACardCalledCheckAndReleaseOfACardStillWorks(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1 --coordinator lead")
	code, _, errs := ta.do("add --stream s1 check --one --actor lead --brief-file " + proBriefFile(t))
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, errs, "a card cannot be called check")
	code, _, errs = ta.do("add --stream s1 --sentinel check --actor lead")
	assert.Equal(t, 2, code, errs)
	assert.Contains(t, errs, "a card cannot be called check")
	ta.ok("add --stream s1 held-1 --one --held --actor lead --brief-file " + proBriefFile(t))
	assert.Contains(t, ta.ok("release held-1 --reason 'read it' --actor lead"), "held-1")
}
