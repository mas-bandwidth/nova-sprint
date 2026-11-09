package verbs

import (
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// githubStream makes stream s a github stream, so its base's checks are read.
func githubStream(s *store.Snapshot) {
	st := s.Streams["s"]
	st.Forge = "github"
	s.Streams["s"] = st
}

// baseChecks gives the fake forge the base tip's rollup.
func baseChecks(r forge.CheckRollup) func(*testing.T, *world) {
	return func(_ *testing.T, w *world) { w.forge.rollups = map[string]forge.CheckRollup{base: r} }
}

var sevenRed = forge.CheckRollup{SHA: base, State: forge.CheckRed, Failing: []string{"unit", "lint", "tsan", "asan", "msan", "bench", "docs"}}

// TestAddRefusesARedBase: add (and --dry-run) reads the base tip's checks
// through the forge the landing reads them with, and refuses every card
// onto a base that is red by the landing's own rule (a completed failure,
// even with other checks still running: 2026-11-08, 171 cards were admitted
// onto a base red on 155 of 205 jobs and none could land), naming the first
// red checks, their count and the fix. Pending checks, no checks, green and
// a gate fault are admitted; a stream with no forge has no checks to read;
// a read that fails is a failure, never an admission.
func TestAddRefusesARedBase(t *testing.T) {
	t.Parallel()
	red := "stream s: o/r's main is red at bbbbbbbbbbbb on 7 checks (unit, lint, tsan, asan, msan and 2 more), so no card of it can land"
	fix := "make o/r's main green at bbbbbbbbbbbb, then add again"
	add := func(t *testing.T, extra ...string) []string {
		return append([]string{"add", "--stream", "s", "--brief-file", brief(t, "a1", "")}, extra...)
	}
	cases := []struct {
		name  string
		snap  func(*store.Snapshot)
		prep  func(*testing.T, *world)
		dry   bool
		exit  int
		parts []string
	}{
		{name: "red", snap: githubStream, prep: baseChecks(sevenRed), exit: 1, parts: []string{red, fix}},
		{name: "red, dry run", snap: githubStream, prep: baseChecks(sevenRed), dry: true, exit: 1, parts: []string{red, fix}},
		{name: "red with checks still running", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckRed, Failing: []string{"unit"}, Waiting: true}), exit: 1,
			parts: []string{"stream s: o/r's main is red at bbbbbbbbbbbb on 1 check (unit), so no card of it can land", fix}},
		{name: "pending", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckPending}), parts: []string{"ADDED 1 cards"}},
		{name: "no checks", snap: githubStream, prep: func(*testing.T, *world) {}, parts: []string{"ADDED 1 cards"}},
		{name: "absent", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckAbsent}), parts: []string{"ADDED 1 cards"}},
		{name: "green", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckGreen}), parts: []string{"ADDED 1 cards"}},
		{name: "gate fault", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckFault, Failing: []string{"unit"}}), parts: []string{"ADDED 1 cards"}},
		{name: "no forge", prep: baseChecks(sevenRed), parts: []string{"ADDED 1 cards"}},
		{name: "unreadable", snap: githubStream, prep: func(_ *testing.T, w *world) { w.forge.rollupE = errBroken }, exit: 2,
			parts: []string{"the checks of o/r's main at bbbbbbbbbbbb cannot be read (connection refused)", "make o/r readable by this host's gh, then add again"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := sprint()
			if c.snap != nil {
				c.snap(&s)
			}
			w := worldFrom(t, s)
			healthy(t, w)
			c.prep(t, w)
			args := add(t)
			if c.dry {
				args = add(t, "--dry-run")
			}
			want(t, w.do("carol", args...), c.exit, c.parts...)
			if _, ok := w.snap(t).Cards["a1"]; ok != (c.exit == 0 && !c.dry) {
				t.Fatalf("card a1 added: %v, exit %d", ok, c.exit)
			}
		})
	}
}

// TestStreamAddRefusesARedBase: stream add refuses a github stream whose
// base tip is red, the same way add does, and adds one whose checks are
// pending.
func TestStreamAddRefusesARedBase(t *testing.T) {
	t.Parallel()
	add := []string{"stream", "add", "t", "--repo", "o/r", "--forge", "github"}
	runCases(t, []tcase{
		{name: "red", as: "carol", args: add, exit: 1, prep: baseChecks(sevenRed),
			want: []string{"stream t: o/r's main is red at bbbbbbbbbbbb on 7 checks (unit, lint, tsan, asan, msan and 2 more), so no card of it can land", "make o/r's main green at bbbbbbbbbbbb, then add the stream again"}},
		{name: "pending", as: "carol", args: add, exit: 0, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckPending}), want: []string{"STREAM t repo=o/r base=main"}},
	})
	w := newWorld(t)
	baseChecks(sevenRed)(t, w)
	want(t, w.do("carol", add...), 1, "is red at")
	if _, ok := w.snap(t).Streams["t"]; ok {
		t.Fatal("a refused stream add put the stream")
	}
}
