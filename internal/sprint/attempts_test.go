package sprint

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// A child that ends 134 or 137, or that leaves no result at all, did not judge the card:
// it is an environment event of the member, recorded on the attempt as
// `ended=environment exit=<n>`, never as failed or broken work (docs/SPEC-SPRINT.md,
// "An environment exit"). This pins the classifier the finish and the doctor read.
func TestEnvExitClassifiesTheMemberEnvironmentExit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report string
		exit   int
		ended  string
		env    bool
	}{
		{"134 under landlock", "no result: Error: Unknown: ChildProcess.spawn (opencode)", 134, "environment exit=134", true},
		{"137 killed", "no result: killed", 137, "environment exit=137", true},
		{"any exit with no result", "no result: the child wrote nothing", 1, "environment exit=1", true},
		{"a verdict is the child's", "verdict not-done; pushed=abc: wrong scope", 1, "", false},
		{"no result but exit zero", "no result: nothing done", 0, "", false},
		{"a clean finish", "verdict done; pushed=abc", 0, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ended, env := EnvExit(tc.report, tc.exit)
			if ended != tc.ended || env != tc.env {
				t.Fatalf("EnvExit(%q, %d) = %q, %v; want %q, %v", tc.report, tc.exit, ended, env, tc.ended, tc.env)
			}
		})
	}
}

// EnvironmentExit is the attempt's word for the finish, "exit=<n>" when it is the
// environment's, so a card's record says which exit ended it.
func TestEnvironmentExitNamesTheCode(t *testing.T) {
	if got := EnvironmentExit(134); got != "environment exit=134" {
		t.Fatalf("EnvironmentExit(134) = %q", got)
	}
	if got := EnvironmentExit(137); got != "environment exit=137" {
		t.Fatalf("EnvironmentExit(137) = %q", got)
	}
}

// The finish record keeps the LAST 40 lines of the harness output, so the cause is on the
// card (docs/SPEC-SPRINT.md, "An environment exit"; a-card-carries-its-consumer-history
// reads it). More than 40 keeps the tail; fewer keeps all; the empty output is empty.
func TestHarnessTailKeepsTheLastFortyLines(t *testing.T) {
	if got := HarnessTail("", 40); got != "" {
		t.Fatalf("an empty tail = %q", got)
	}
	short := "a\nb\nc\n"
	if got := HarnessTail(short, 40); got != short {
		t.Fatalf("fewer than 40 lines = %q", got)
	}
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		b.WriteString(itoa(i) + "\n")
	}
	got := strings.Split(strings.TrimRight(HarnessTail(b.String(), 40), "\n"), "\n")
	if len(got) != 40 || got[0] != "61" || got[39] != "100" {
		t.Fatalf("the tail's last 40 = %d lines, first %q last %q", len(got), got[0], got[39])
	}
}

// EnvironmentExitRecord is the finish's one record of an environment exit: the ended word at
// the child's exit and the last HarnessTailLines of its harness output, so the cause rides
// the card. An empty output leaves an empty tail; the ended word still names the code.
func TestEnvironmentExitRecordCarriesTheEndedWordAndTail(t *testing.T) {
	if got := EnvironmentExitRecord(134, ""); got.Ended != "environment exit=134" || got.Harness != "" {
		t.Fatalf("EnvironmentExitRecord(134, \"\") = %+v", got)
	}
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		b.WriteString(itoa(i) + "\n")
	}
	got := EnvironmentExitRecord(137, b.String())
	if got.Ended != "environment exit=137" {
		t.Fatalf("Ended = %q", got.Ended)
	}
	lines := strings.Split(strings.TrimRight(got.Harness, "\n"), "\n")
	if len(lines) != 40 || lines[0] != "61" || lines[39] != "100" {
		t.Fatalf("Harness tail = %d lines, first %q last %q", len(lines), lines[0], lines[39])
	}
}

// The doctor's rate thresholds (docs/SPEC-SPRINT.md, "An environment exit"): over 10% with
// n >= 10 is FAIL, 3% to 10% is WARN, below is OK; the counts come from the attempt records.
func TestEnvExitRateThresholds(t *testing.T) {
	for _, tc := range []struct {
		env, n int
		want   string
	}{
		{0, 0, DoctorOK},
		{0, 50, DoctorOK},
		{1, 10, DoctorWarn},
		{2, 20, DoctorWarn},
		{1, 100, DoctorOK},
	} {
		if got := EnvExitStatus(tc.env, tc.n); got != tc.want {
			t.Fatalf("EnvExitStatus(%d, %d) = %q; want %q", tc.env, tc.n, got, tc.want)
		}
	}
	if got := EnvExitStatus(5, 12); got != DoctorFail {
		t.Fatalf("EnvExitStatus(5, 12) = %q; want FAIL", got)
	}
	if got := EnvExitStatus(1, 9); got != DoctorOK {
		t.Fatalf("n below the floor is never a fault: %q", got)
	}
}

// The member-env line names the member, the rate and the last harness tail line, and its
// remedy is the card to read; an OK rate is no line at all.
func TestEnvExitCheckNamesTheRateAndTheCard(t *testing.T) {
	c, ok := EnvExitCheck("b1", 12, 30, "Error: Unknown: ChildProcess.spawn (opencode)", "s1-7.w1")
	if !ok {
		t.Fatal("a 40% rate is named")
	}
	want := "DOCTOR FAIL member-env b1 child exits: 12 of 30 children in the last hour ended 134/137 with no result (40%), last: Error: Unknown: ChildProcess.spawn (opencode) remedy=nova-sprint card s1-7.w1"
	if got := c.Line(); got != want {
		t.Fatalf("EnvExitCheck line = %q\nwant %q", got, want)
	}
	if _, ok := EnvExitCheck("m1", 0, 50, "", "x"); ok {
		t.Fatal("an OK rate is no check")
	}
}

// A finish whose child exit is 134 or 137, or any non-zero exit with a no-result report, is
// an environment exit (docs/SPEC-SPRINT.md, "An environment exit"): the finish records it on
// the attempt as `ended=environment exit=<n>` with the last HarnessTailLines of the harness
// output, returns the card to ready at the same attempt, spends no attempt and no redeal
// (no FieldTakeEnded), and does not hold the member. It is never failed work.
func TestFinishRecordsAnEnvironmentExitOnTheAttempt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exit   int
		report string
	}{
		{"134 under landlock", 134, "no result: Error: Unknown: ChildProcess.spawn (opencode)"},
		{"137 killed", 137, "no result: killed"},
		{"any exit with no result", 1, "no result: the child wrote nothing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			for i := 1; i <= 100; i++ {
				b.WriteString(itoa(i) + "\n")
			}
			s, r := envExitSprint(t, tc.exit, tc.report, b.String())
			p := Finish(s, r)
			if len(p.Refused) != 0 {
				t.Fatalf("the finish refused: %+v", p.Refused)
			}
			if len(p.Units) != 1 {
				t.Fatalf("one unit, got %d: %+v", len(p.Units), p.Units)
			}
			u := p.Units[0]
			wc := envExitEntryOf(u, "c1")
			if wc.Move == nil || wc.Move.Col != Withdrawn {
				t.Fatalf("the work card withdraws, not %+v", wc.Move)
			}
			if got := wc.Set[FieldEnvExit]; got != "environment exit="+itoa(tc.exit) {
				t.Fatalf("ended = %q; want environment exit=%d", got, tc.exit)
			}
			if _, ok := wc.Set[FieldTakeEnded]; ok {
				t.Fatal("an environment exit spends no redeal: FieldTakeEnded must not be set")
			}
			lines := strings.Split(strings.TrimRight(wc.Set[FieldHarnessTail], "\n"), "\n")
			if len(lines) != HarnessTailLines || lines[0] != "61" || lines[HarnessTailLines-1] != "100" {
				t.Fatalf("harness tail = %d lines first %q last %q", len(lines), lines[0], lines[HarnessTailLines-1])
			}
			pr := envExitEntryOf(u, "p1")
			if pr.Move == nil || pr.Move.Col != Ready {
				t.Fatalf("the primary returns to ready, not %+v", pr.Move)
			}
			if _, ok := pr.Set["attempt"]; ok {
				t.Fatal("the attempt number is unchanged")
			}
			if _, ok := pr.Set["failed"]; ok {
				t.Fatal("an environment exit is never failed work")
			}
			found := false
			for _, n := range u.Notes {
				if n.Type == NMemberEnvExit {
					found = true
				}
			}
			if !found {
				t.Fatalf("no %s note: %+v", NMemberEnvExit, u.Notes)
			}
		})
	}
}

// A clean end (exit 0) or a verdict is the card's own, never an environment exit: the
// attempt is not marked `ended=environment exit=<n>`.
func TestFinishWithACleanEndOrAVerdictIsNeverAnEnvironmentExit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exit   int
		report string
	}{
		{"no result but exit zero", 0, "no result: nothing done"},
		{"a verdict is the child's", 1, "verdict not-done; wrong scope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := envExitSprint(t, tc.exit, tc.report, "")
			p := Finish(s, r)
			if len(p.Units) != 1 {
				t.Fatalf("one unit, got %d: %+v", len(p.Units), p.Units)
			}
			u := p.Units[0]
			wc := envExitEntryOf(u, "c1")
			if got := wc.Set[FieldEnvExit]; got != "" {
				t.Fatalf("ended = %q; want none", got)
			}
		})
	}
}

// envExitSprint is a sprint with the member m1's work card c1 working for the primary p1, and
// m1's failed finish of it at exit with report and the child's harness output.
func envExitSprint(t *testing.T, exit int, report, harness string) (*Snapshot, FinishReq) {
	t.Helper()
	s := &Snapshot{Now: time.Date(2026, 10, 11, 18, 25, 0, 0, time.UTC), Work: NewTable(Work), Readers: NewTable(Readers),
		Merge: NewTable(Merge), Fleet: NewTable(Fleet), Coordinator: "coordinator", Actor: "coordinator"}
	s.Work.SetRows([]string{"s1"})
	s.Fleet.SetRows([]string{"m1"})
	s.Work.Put(&Card{ID: "p1", Row: "s1", Col: Working, Fields: map[string]string{"kind": "primary", "stream": "s1", "work": "c1", "attempt": "3", "brief": "p1: a card\n\nThe task."}})
	s.Fleet.Put(&Card{ID: "c1", Row: "m1", Col: Working, Fields: map[string]string{"kind": "work", "primary": "p1", "stream": "s1", "attempt": "3", "gen": "1", "member": "m1"}})
	return s, FinishReq{Sel: Sel{IDs: []string{"c1"}}, As: "m1", Gens: map[string]int{"c1": 1},
		Failed: true, Exit: exit, Report: report, Harness: harness}
}

// envExitEntryOf is the change the unit makes to the card id.
func envExitEntryOf(u Unit, id string) ntable.BatchMemberEntry {
	i := slices.IndexFunc(u.Changes, func(c Change) bool { return c.Entry.ID == id })
	if i < 0 {
		return ntable.BatchMemberEntry{}
	}
	return u.Changes[i].Entry
}

// EnvExitChildren counts each member's environment exits out of its children in the window
// from the attempt records on the work cards (docs/SPEC-SPRINT.md, "An environment exit"),
// and keeps the first line of the last harness tail and its primary for the doctor's line.
func TestEnvExitChildrenCountsTheMemberEnvironmentExits(t *testing.T) {
	now := time.Date(2026, 10, 11, 18, 25, 0, 0, time.UTC)
	s := &Snapshot{Now: now, Work: NewTable(Work), Fleet: NewTable(Fleet), Readers: NewTable(Readers), Merge: NewTable(Merge)}
	s.Fleet.SetRows([]string{"m1", "m2"})
	end := stamp(now)
	old := stamp(now.Add(-2 * time.Hour))
	put := func(id, member, ended, harness, at string) {
		f := map[string]string{"kind": "work", "member": member, "primary": "p-" + id, "withdrawn": at}
		if ended != "" {
			f[FieldEnvExit], f[FieldHarnessTail] = ended, harness
		}
		s.Fleet.Put(&Card{ID: id, Row: member, Col: Withdrawn, Fields: f})
	}
	// m1: 8 clean children and 2 environment exits in the window
	for i := 1; i <= 8; i++ {
		put("c"+itoa(i), "m1", "", "", end)
	}
	put("c9", "m1", "environment exit=134", "Error: Unknown: ChildProcess.spawn (opencode)\nmore lines\n", end)
	put("c10", "m1", "environment exit=137", "killed\n", stamp(now.Add(time.Minute)))
	// m2: one environment exit, older than the window
	put("d1", "m2", "environment exit=134", "stale\n", old)

	got := EnvExitChildren(s, now, time.Hour)
	if len(got) != 1 {
		t.Fatalf("counts = %+v; want m1 alone", got)
	}
	c := got[0]
	if c.Member != "m1" || c.Env != 2 || c.N != 10 {
		t.Fatalf("m1 = %+v; want env=2 n=10", c)
	}
	if c.Card != "p-c10" || c.Last != "killed" {
		t.Fatalf("m1 last = %q card %q; want the last exit's (c10)", c.Last, c.Card)
	}
}
