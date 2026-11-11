package verbs

import (
	"strings"
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

// baseRedOpen is githubStream with the run's base-red judgement open on its
// repository and base, which check's CHECK RED line names.
func baseRedOpen(s *store.Snapshot) {
	githubStream(s)
	if s.Judgements == nil {
		s.Judgements = map[string]store.Judgement{}
	}
	s.Judgements["base-red.o/r@main.1"] = store.Judgement{ID: "base-red.o/r@main.1", Kind: "base-red", About: "o/r@main", State: "open",
		Question: "BASE RED at bbbbbbbbbbbb, o/r main: 7 checks red", Command: "nova-sprint judge base-red.o/r@main.1 <choice> --reason <why>   choices: retry"}
}

// baseChecks gives the fake forge the base tip's rollup.
func baseChecks(r forge.CheckRollup) func(*testing.T, *world) {
	return func(_ *testing.T, w *world) { w.forge.rollups = map[string]forge.CheckRollup{base: r} }
}

var sevenRed = forge.CheckRollup{SHA: base, State: forge.CheckRed, Failing: []string{"unit", "lint", "tsan", "asan", "msan", "bench", "docs"}}

const baseRedLine7 = "BASE RED o/r main at bbbbbbbbbbbb: 7 checks red (unit, lint, tsan, asan, msan and 2 more); cards land only once it is green: make it green first (cards for that are admitted)"

// TestAddSaysTheBaseIsRed: add (and --dry-run) reads the base tip's checks
// through the forge the landing reads them with and, when the base is red by
// the landing's own rule (a completed failure, even with other checks still
// running), admits the cards and prints BASE RED with the first red checks,
// their count and what to do: the cards that make the base green must get in
// (2026-11-08: 171 cards went onto a base red on 155 of 205 jobs unseen, and
// none could land). Pending, no checks, green and a gate fault print
// nothing; a stream with no forge has no checks to read; a read that fails
// is a warning (not red, as the landing reads it).
func TestAddSaysTheBaseIsRed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		snap   func(*store.Snapshot)
		prep   func(*testing.T, *world)
		dry    bool
		parts  []string
		silent bool // no BASE RED and no WARN
	}{
		{name: "red", snap: githubStream, prep: baseChecks(sevenRed), parts: []string{baseRedLine7, "ADDED 1 cards"}},
		{name: "red, dry run", snap: githubStream, prep: baseChecks(sevenRed), dry: true, parts: []string{baseRedLine7, "(dry run)"}},
		{name: "red with checks still running", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckRed, Failing: []string{"unit"}, Waiting: true}),
			parts: []string{"BASE RED o/r main at bbbbbbbbbbbb: 1 check red (unit); cards land only once it is green", "ADDED 1 cards"}},
		{name: "pending", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckPending}), parts: []string{"ADDED 1 cards"}, silent: true},
		{name: "no checks", snap: githubStream, prep: func(*testing.T, *world) {}, parts: []string{"ADDED 1 cards"}, silent: true},
		{name: "absent", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckAbsent}), parts: []string{"ADDED 1 cards"}, silent: true},
		{name: "green", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckGreen}), parts: []string{"ADDED 1 cards"}, silent: true},
		{name: "gate fault", snap: githubStream, prep: baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckFault, Failing: []string{"unit"}}), parts: []string{"ADDED 1 cards"}, silent: true},
		{name: "no forge", prep: baseChecks(sevenRed), parts: []string{"ADDED 1 cards"}, silent: true},
		{name: "unreadable", snap: githubStream, prep: func(_ *testing.T, w *world) { w.forge.rollupE = errBroken },
			parts: []string{"WARN stream s: the checks of o/r's main at bbbbbbbbbbbb cannot be read (connection refused), so it is not known green", "ADDED 1 cards"}},
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
			args := []string{"add", "--stream", "s", "--brief-file", brief(t, "a1", "")}
			if c.dry {
				args = append(args, "--dry-run")
			}
			r := w.do("carol", args...)
			want(t, r, 0, c.parts...)
			if c.silent && (strings.Contains(r.Output, "BASE RED") || strings.Contains(r.Output, "WARN")) {
				t.Fatalf("a base line with nothing to say:\n%s", r.Output)
			}
			if _, ok := w.snap(t).Cards["a1"]; ok == c.dry {
				t.Fatalf("card a1 added: %v on a dry run %v", ok, c.dry)
			}
		})
	}
}

// TestStreamAddSaysTheBaseIsRed: stream add adds a github stream whose base
// tip is red and prints BASE RED, as add does; pending prints nothing.
func TestStreamAddSaysTheBaseIsRed(t *testing.T) {
	t.Parallel()
	add := []string{"stream", "add", "t", "--repo", "o/r", "--forge", "github"}
	runCases(t, []tcase{
		{name: "red", as: "carol", args: add, exit: 0, prep: baseChecks(sevenRed), want: []string{"STREAM t repo=o/r base=main", baseRedLine7}},
		{name: "unreadable", as: "carol", args: add, exit: 0, prep: func(_ *testing.T, w *world) { w.forge.rollupE = errBroken },
			want: []string{"STREAM t repo=o/r base=main", "WARN stream t: the checks of o/r's main at bbbbbbbbbbbb cannot be read (connection refused)"}},
	})
	w := newWorld(t)
	baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckPending})(t, w)
	r := w.do("carol", add...)
	want(t, r, 0, "STREAM t repo=o/r base=main")
	if strings.Contains(r.Output, "BASE RED") {
		t.Fatalf("BASE RED on pending checks:\n%s", r.Output)
	}
}

// TestCheckSaysTheBaseIsRed: check shows a CHECK RED line for a github
// stream whose base tip is red, one checks read per stream, and a warning
// when the read fails; a green base adds no line.
func TestCheckSaysTheBaseIsRed(t *testing.T) {
	t.Parallel()
	runCases(t, []tcase{
		{name: "red", as: "carol", args: []string{"check"}, exit: 1, snap: githubStream, prep: then(healthy, baseChecks(sevenRed)),
			want: []string{"CHECK RED stream s: base o/r main is red at bbbbbbbbbbbb: 7 checks red (unit, lint, tsan, asan, msan and 2 more), so no card of it lands; no base-red judgement is open for it yet: make o/r's main green at bbbbbbbbbbbb (cards for that are admitted), then check again"}},
		{name: "red, judged", as: "carol", args: []string{"check"}, exit: 1, snap: baseRedOpen, prep: then(healthy, baseChecks(sevenRed)),
			want: []string{"CHECK RED stream s: base o/r main is red at bbbbbbbbbbbb: 7 checks red (unit, lint, tsan, asan, msan and 2 more), so no card of it lands; judgement base-red.o/r@main.1 is open: fix the base first, then answer it: make o/r's main green"}},
		{name: "unreadable", as: "carol", args: []string{"check"}, exit: 0, snap: githubStream, prep: then(healthy, func(_ *testing.T, w *world) { w.forge.rollupE = errBroken }),
			want: []string{"CHECK WARN stream s: the checks of o/r's main at bbbbbbbbbbbb cannot be read (connection refused), so it is not known green"}},
	})
	s := sprint()
	githubStream(&s)
	w := worldFrom(t, s)
	healthy(t, w)
	baseChecks(forge.CheckRollup{SHA: base, State: forge.CheckGreen})(t, w)
	r := w.do("carol", "check")
	want(t, r, 0)
	if strings.Contains(r.Output, "is red at") || strings.Contains(r.Output, "not known green") {
		t.Fatalf("a base line on a green base:\n%s", r.Output)
	}
	if n := w.forge.checksCalls(); n != 1 {
		t.Fatalf("check read the base's checks %d times, want 1", n)
	}
}
