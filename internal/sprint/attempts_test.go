package sprint

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/decide"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
)

// A child that ends 134 or 137, or that leaves no result at all at ANY exit, did not judge
// the card: it is an environment event of the member, recorded on the attempt as
// `ended=environment exit=<n>`, never as failed or broken work (docs/SPEC-SPRINT.md, "An
// environment exit"). This pins the classifier the finish, the read's return and the doctor
// read. The no-result case at exit 0 is the attempt-3 reader's finding: the card says any
// exit, and 0 is also what a member that passes no exit reports.
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
		{"134 whatever the report", "no verdict (ran=false verdict=\"\"): aborted", 134, "environment exit=134", true},
		{"no result at exit 1", "no result: the child wrote nothing", 1, "environment exit=1", true},
		{"no result at exit 0", "no result: no RESULT.md shape", 0, "environment exit=0", true},
		{"a verdict is the child's", "verdict not-done; pushed=abc: wrong scope", 1, "", false},
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
	if !IsEnvironmentExit(EnvironmentExit(0)) || IsEnvironmentExit("failed") {
		t.Fatal("IsEnvironmentExit reads the ended word")
	}
}

// hundredLines is "1\n2\n...100\n": its last forty lines are 61 to 100.
func hundredLines() string {
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		b.WriteString(itoa(i) + "\n")
	}
	return b.String()
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
	got := strings.Split(strings.TrimRight(HarnessTail(hundredLines(), 40), "\n"), "\n")
	if len(got) != 40 || got[0] != "61" || got[39] != "100" {
		t.Fatalf("the tail's last 40 = %d lines, first %q last %q", len(got), got[0], got[39])
	}
}

// EnvironmentExitRecord is the finish's one record of an environment exit: the ended word at
// the child's exit and the last HarnessTailLines of its harness output, so the cause rides
// the card; forty very long lines are cut to their last HarnessTailBytes, the end kept.
func TestEnvironmentExitRecordCarriesTheEndedWordAndTail(t *testing.T) {
	if got := EnvironmentExitRecord(134, ""); got.Ended != "environment exit=134" || got.Harness != "" {
		t.Fatalf("EnvironmentExitRecord(134, \"\") = %+v", got)
	}
	got := EnvironmentExitRecord(137, hundredLines())
	if got.Ended != "environment exit=137" {
		t.Fatalf("Ended = %q", got.Ended)
	}
	lines := strings.Split(strings.TrimRight(got.Harness, "\n"), "\n")
	if len(lines) != 40 || lines[0] != "61" || lines[39] != "100" {
		t.Fatalf("Harness tail = %d lines, first %q last %q", len(lines), lines[0], lines[39])
	}
	long := strings.Repeat(strings.Repeat("x", 2000)+"\n", 39) + "the cause\n"
	cut := EnvironmentExitRecord(134, long).Harness
	if len(cut) > HarnessTailBytes || !strings.HasSuffix(cut, "the cause\n") || !strings.HasPrefix(cut, "...") {
		t.Fatalf("a long tail is cut to its end: %d bytes, ends %q", len(cut), cut[max(0, len(cut)-12):])
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
		{3, 100, DoctorWarn},
		{2, 100, DoctorOK},
		{2, 10, DoctorFail},
		{5, 12, DoctorFail},
		{9, 9, DoctorOK}, // under the floor: a small sample says nothing
	} {
		if got := EnvExitStatus(tc.env, tc.n); got != tc.want {
			t.Fatalf("EnvExitStatus(%d, %d) = %q; want %q", tc.env, tc.n, got, tc.want)
		}
	}
}

// The member-env line names the member, the rate and the last harness tail line, and its
// remedy is the card to read; an OK rate is an OK line with no card; a member under the
// floor is no line at all.
func TestEnvExitCheckNamesTheRateAndTheCard(t *testing.T) {
	c, ok := EnvExitCheck("b1", 12, 30, "Error: Unknown: ChildProcess.spawn (opencode)", "s1-7")
	if !ok {
		t.Fatal("a 40% rate is named")
	}
	want := "DOCTOR FAIL member-env b1 child exits: 12 of 30 children in the last hour ended 134/137 with no result (40%), last: Error: Unknown: ChildProcess.spawn (opencode) remedy=nova-sprint card s1-7"
	if got := c.Line(); got != want {
		t.Fatalf("EnvExitCheck line = %q\nwant %q", got, want)
	}
	c, ok = EnvExitCheck("m1", 0, 50, "", "")
	if !ok || c.Line() != "DOCTOR OK member-env m1 child exits: 0 of 50 children in the last hour ended 134/137 with no result (0%), last: - remedy=-" {
		t.Fatalf("an OK rate is an OK line: %v %q", ok, c.Line())
	}
	if _, ok := EnvExitCheck("m1", 5, 9, "x", "s1-1"); ok {
		t.Fatal("under EnvExitMinN children is no line")
	}
}

// A finish whose child exit is 134 or 137 is an environment exit that spends nothing
// (docs/SPEC-SPRINT.md, "An environment exit"; HardEnvExit): the finish records it on the
// attempt as `ended=environment exit=<n>` with the last HarnessTailLines of the harness
// output, on the work card and on its primary, with the take's consumer record ending so;
// returns the card to ready at the same attempt, spends no attempt and no redeal (no
// FieldTakeEnded), and does not hold the member. It is never failed work. Before this card
// such a finish was takeEnded's (a spent redeal) or, at another report, failed work.
func TestFinishRecordsAnEnvironmentExitOnTheAttempt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exit   int
		report string
	}{
		{"134 under landlock", 134, "no result: Error: Unknown: ChildProcess.spawn (opencode)"},
		{"137 killed", 137, "no result: killed"},
		{"134 with any report", 134, "not-done: the run aborted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := envExitSprint(t, tc.exit, tc.report, hundredLines())
			p := Finish(s, r)
			if len(p.Refused) != 0 {
				t.Fatalf("the finish refused: %+v", p.Refused)
			}
			if len(p.Units) != 1 {
				t.Fatalf("one unit, got %d: %+v", len(p.Units), p.Units)
			}
			u := p.Units[0]
			wc := envExitEntryOf(u, "c1")
			if wc.Move == nil || wc.Move.Col != Environment {
				t.Fatalf("the work card withdraws, not %+v", wc.Move)
			}
			ended := "environment exit=" + itoa(tc.exit)
			if got := wc.Set[FieldEnvExit]; got != ended {
				t.Fatalf("ended = %q; want %s", got, ended)
			}
			if _, ok := wc.Set[FieldTakeEnded]; ok {
				t.Fatal("an environment exit spends no redeal: FieldTakeEnded must not be set")
			}
			if _, ok := wc.Set["redeals"]; ok {
				t.Fatal("an environment exit counts no redeal")
			}
			lines := strings.Split(strings.TrimRight(wc.Set[FieldHarnessTail], "\n"), "\n")
			if len(lines) != HarnessTailLines || lines[0] != "61" || lines[HarnessTailLines-1] != "100" {
				t.Fatalf("harness tail = %d lines first %q last %q", len(lines), lines[0], lines[HarnessTailLines-1])
			}
			pr := envExitEntryOf(u, "p1")
			if pr.Move == nil || pr.Move.Col != Ready {
				t.Fatalf("the primary returns to ready, not %+v", pr.Move)
			}
			for _, f := range []string{"attempt", "failed", "result"} {
				if _, ok := pr.Set[f]; ok {
					t.Fatalf("the primary's %s is unchanged by an environment exit: %+v", f, pr.Set)
				}
			}
			if pr.Set[FieldHarnessTail] != wc.Set[FieldHarnessTail] {
				t.Fatal("the primary carries the same harness tail")
			}
			rec := pr.Set[FieldCostRecord+"c1#g1"]
			if !strings.Contains(rec, "end=environment-exit="+itoa(tc.exit)) || !strings.Contains(rec, "who=m1") {
				t.Fatalf("the take's consumer record on the primary ends on the environment: %q", rec)
			}
			found := false
			for _, n := range u.Notes {
				found = found || n.Type == NMemberEnvExit
			}
			if !found {
				t.Fatalf("no %s note: %+v", NMemberEnvExit, u.Notes)
			}
		})
	}
}

// A no-result child at an exit other than 134 or 137 is an environment exit too, recorded
// and counted as one (EnvExit; the attempt-3 reader's finding: any exit), but its take is
// still an ended take, as the owner's rules for a take that left no result say (2026-10-01
// redealt counting the take, 2026-10-02 rule 2, 2026-10-03 flash first): FieldTakeEnded is
// set, the work card carries `ended=environment exit=<n>` and the tail, and the take's
// consumer record ends `no result environment exit=<n>`, which the doctor counts.
func TestANoResultTakeIsAnEnvironmentExitAndStillAnEndedTake(t *testing.T) {
	for _, exit := range []int{0, 1} {
		t.Run("exit "+itoa(exit), func(t *testing.T) {
			s, r := envExitSprint(t, exit, "no result: no RESULT.md shape", hundredLines())
			p := Finish(s, r)
			if len(p.Refused) != 0 || len(p.Units) != 1 {
				t.Fatalf("one unit, no refusal: %+v %+v", p.Refused, p.Units)
			}
			wc := envExitEntryOf(p.Units[0], "c1")
			if wc.Set[FieldEnvExit] != "environment exit="+itoa(exit) || wc.Set[FieldTakeEnded] == "" {
				t.Fatalf("an ended take with the environment's word: %+v", wc.Set)
			}
			if lines := strings.Split(strings.TrimRight(wc.Set[FieldHarnessTail], "\n"), "\n"); len(lines) != HarnessTailLines {
				t.Fatalf("the tail rides the card: %d lines", len(lines))
			}
			pr := envExitEntryOf(p.Units[0], "p1")
			if pr.Move == nil || pr.Move.Col != Ready || pr.Set["failed"] != "" {
				t.Fatalf("the primary is ready, never failed: %+v %+v", pr.Move, pr.Set)
			}
			rec := pr.Set[FieldCostRecord+"c1#g1"]
			if !strings.Contains(rec, "end=no-result-environment-exit="+itoa(exit)) || pr.Set[FieldHarnessTail] == "" {
				t.Fatalf("the take's record names the exit and the primary the tail: %q", rec)
			}
		})
	}
}

// A verdict at a non-environment exit, or a clean finish, is the card's own, never an
// environment exit: the attempt is not marked `ended=environment exit=<n>`. A friend's row
// is no member's environment: her no-result finish is judged as before.
func TestFinishWithACleanEndOrAVerdictIsNeverAnEnvironmentExit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exit   int
		report string
		failed bool
	}{
		{"a verdict is the child's", 1, "verdict not-done; wrong scope", true},
		{"a clean finish", 0, "verdict done", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r := envExitSprint(t, tc.exit, tc.report, "")
			r.Failed = tc.failed
			p := Finish(s, r)
			if len(p.Units) != 1 {
				t.Fatalf("one unit, got %d: %+v", len(p.Units), p.Units)
			}
			if got := envExitEntryOf(p.Units[0], "c1").Set[FieldEnvExit]; got != "" {
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

// A member's read whose child ended on its environment is handed back with no verdict and
// spends no reader (docs/SPEC-SPRINT.md, "An environment exit"): retired by environment
// (not in spentBy), so the read-card deal may deal the same member the read again; the
// ended word and the tail ride the card and the primary, and the primary gets the run's
// record ending so. Before this card the same return was retired by returned, which spends
// the member at the attempt (a card read once by every member ended `cannot ask`).
func TestAReadsEnvironmentExitSpendsNoReader(t *testing.T) {
	s := &Snapshot{Now: time.Date(2026, 10, 11, 18, 25, 0, 0, time.UTC), Work: NewTable(Work), Readers: NewTable(Readers),
		Merge: NewTable(Merge), Fleet: NewTable(Fleet), Coordinator: "coordinator", Actor: "coordinator"}
	s.Work.SetRows([]string{"s1"})
	s.Fleet.SetRows([]string{"m1"})
	s.Work.Put(&Card{ID: "p1", Row: "s1", Col: Review, Fields: map[string]string{"kind": "primary", "stream": "s1", "attempt": "1", "head": "abc"}})
	rid := ReadCardID("p1", 1, "m1")
	s.Fleet.Put(&Card{ID: rid, Row: "m1", Col: Working, Fields: map[string]string{"kind": "read", "primary": "p1", "stream": "s1", "reader": "m1",
		"attempt": "1", "head": "abc", "gen": "1", "asked": stamp(s.Now.Add(-time.Minute)), FieldReadCard: "1"}})
	p := Read(s, ReadReq{Sel: Sel{IDs: []string{rid}}, As: "m1", Return: true, Reason: "no verdict (ran=false verdict=\"\"): aborted",
		Exit: 134, Harness: "Error: Unknown: ChildProcess.spawn (opencode)\nAborted (core dumped)\n", Who: "m1"})
	if len(p.Refused) != 0 || len(p.Units) != 1 {
		t.Fatalf("one unit, no refusal: %+v %+v", p.Refused, p.Units)
	}
	rc := envExitEntryOf(p.Units[0], rid)
	if rc.Set["retired_by"] != RetiredByEnvironment || rc.Set[FieldEnvExit] != "environment exit=134" ||
		!strings.HasPrefix(rc.Set[FieldHarnessTail], "Error: Unknown: ChildProcess.spawn") {
		t.Fatalf("the read card is retired by the environment with its record: %+v", rc.Set)
	}
	after := &Card{ID: rid, Row: "m1", Fields: rc.Set}
	if readSpent([]*Card{after}) {
		t.Fatal("an environment exit spends no reader: the member may read the card again")
	}
	if !readSpent([]*Card{{ID: rid, Row: "m1", Fields: map[string]string{"retired_by": RetiredByReturned}}}) {
		t.Fatal("a return of the reader's own still spends it")
	}
	pr := envExitEntryOf(p.Units[0], "p1")
	found := false
	for k, v := range pr.Set {
		found = found || strings.HasPrefix(k, FieldCostRecord) && strings.Contains(v, "end=environment-exit=134")
	}
	if !found || pr.Set[FieldHarnessTail] == "" {
		t.Fatalf("the primary carries the run's record and the tail: %+v", pr.Set)
	}
}

// EnvExitChildren counts each member's children in the window from the attempt records on
// the epoch's cards (docs/SPEC-SPRINT.md, "An environment exit"): every take's and read
// run's consumer record on its primary, by its end; a record older than the window, a
// friend's run and a name with no fleet row are left out; Env counts 134/137 only (the
// doctor's "ended 134/137"), the histogram every environment exit by code; and the latest
// 134/137's card and the first line of its harness tail are kept for the doctor's line.
func TestEnvExitChildrenCountsTheMemberEnvironmentExits(t *testing.T) {
	now := time.Date(2026, 10, 11, 18, 25, 0, 0, time.UTC)
	s := &Snapshot{Now: now, Work: NewTable(Work), Fleet: NewTable(Fleet), Readers: NewTable(Readers), Merge: NewTable(Merge)}
	s.Work.SetRows([]string{"s1"})
	s.Fleet.SetRows([]string{"m1", "m2", "friend.zhi"})
	put := func(id, tail string, cons ...Consumer) {
		f := map[string]string{"kind": "primary", "stream": "s1"}
		if tail != "" {
			f[FieldHarnessTail] = tail
		}
		for _, c := range cons {
			f[FieldCostRecord+c.Key] = c.line()
		}
		s.Work.Put(&Card{ID: id, Row: "s1", Col: Ready, Fields: f})
	}
	con := func(key, kind, who, end string, at time.Time) Consumer {
		return Consumer{Kind: kind, Card: key, Who: who, End: end, At: stamp(at), Key: key + "#g1"}
	}
	// m1: 8 children of the card's own and 2 environment exits in the window, one of them a read
	var own []Consumer
	for i := 1; i <= 8; i++ {
		own = append(own, con("p1.w"+itoa(i), "work", "m1", "ok", now.Add(-time.Duration(i)*time.Minute)))
	}
	put("p1", "", own...)
	put("p2", "Error: Unknown: ChildProcess.spawn (opencode)\nmore lines\n",
		con("p2.w1", "work", "m1", "environment exit=134", now.Add(-10*time.Minute)))
	put("p3", "killed\n", con("p3.r1.reader-m1", "read", "reader-m1", "environment exit=137", now.Add(-time.Minute)))
	// m1 again: a no-result take's end naming its exit (takeEnded), and a decided no-result,
	// which is no environment exit
	put("p5", "", con("p5.w1", "work", "m1", "no result environment exit=0", now.Add(-20*time.Minute)),
		con("p5.w2", "work", "m1", "no result", now.Add(-20*time.Minute)))
	// m2: one environment exit, older than the window; a friend's run; a name with no row
	put("p4", "stale\n", con("p4.w1", "work", "m2", "environment exit=134", now.Add(-2*time.Hour)),
		con("p4.w2", "work", "friend.zhi", "environment exit=134", now.Add(-time.Minute)),
		con("p4.w3", "work", "ghost", "environment exit=134", now.Add(-time.Minute)))

	got := EnvExitChildren(s, now, time.Hour)
	if len(got) != 1 {
		t.Fatalf("counts = %+v; want m1 alone", got)
	}
	c := got[0]
	if c.Member != "m1" || c.Env != 2 || c.N != 12 || c.Exits["134"] != 1 || c.Exits["137"] != 1 || c.Exits["0"] != 1 {
		t.Fatalf("m1 = %+v; want env=2 (134/137 only) n=12, one each of 134, 137 and 0 in the histogram", c)
	}
	if c.Card != "p3" || c.Last != "killed" {
		t.Fatalf("m1 last = %q card %q; want the latest exit's (p3)", c.Last, c.Card)
	}
}

// The deal step refuses a card at EnvExitCardCap environment exits at its attempt, naming the
// card, the members and the codes (docs/SPEC-SPRINT.md, "An environment exit"): below the cap
// each 134 is dealt again at the same attempt, at the cap it is not.
func TestTheDealRefusesACardAtTheEnvironmentExitCap(t *testing.T) {
	t.Parallel()
	w := setup(t, 1)
	w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
	wc := w.s.Work.Card("s1-1").F("work")
	for i := 1; i <= EnvExitCardCap; i++ {
		member := w.s.Fleet.Card(wc).Row
		w.must(Take(w.s, TakeReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc)}))
		w.must(Finish(w.s, FinishReq{As: member, Sel: Sel{IDs: []string{wc}}, Gens: w.gens(wc), Failed: true, Exit: 134,
			Report: "no result: aborted", Harness: "Error: Unknown: ChildProcess.spawn (opencode)\n"}))
		if i < EnvExitCardCap {
			w.must(Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}}))
		}
	}
	if got := len(envExitHistory(w.s.Fleet.Card(wc))); got != EnvExitCardCap {
		t.Fatalf("the attempt's history holds %d exits; want %d", got, EnvExitCardCap)
	}
	p := Deal(w.s, DealReq{Sel: Sel{IDs: []string{"s1-1"}}})
	if len(p.Units) != 0 || len(p.Refused) != 1 || !strings.Contains(p.Refused[0].Why, "ended on the member's environment 3 times (the cap is 3) and is not dealt again") ||
		!strings.Contains(p.Refused[0].Why, "exits 134, 134, 134") {
		t.Fatalf("the deal refuses it at the cap: %+v", p)
	}
	if w.s.Work.Card("s1-1").F("attempt") != "1" {
		t.Fatalf("the same attempt: %s", w.s.Work.Card("s1-1").F("attempt"))
	}
}

// A no-result at exit 0 whose finish carries a nothing-to-do decision over its bar is routed by
// that decision as failed work (finishKind), never as an environment exit: only a 134 or 137
// spends nothing (HardEnvExit). Before the fix finishPlan sent it to envExited, which spent
// nothing, skipped the failed-work routing and added `m1=0` to the 134/137 cap's history.
func TestADecidedNothingToDoIsNeverAnEnvironmentExit(t *testing.T) {
	s, r := envExitSprint(t, 0, "no result: no RESULT.md shape", "")
	s.Fleet.Card("c1").Fields[FieldDecideAttemptNothingToDo] = "0.5"
	r.Decided = decide.ClassNothingToDo + " p=0.9 op=" + decide.AttemptOp("p1", 3, "state")
	p := Finish(s, r)
	if len(p.Refused) != 0 || len(p.Units) != 1 {
		t.Fatalf("one unit, no refusal: %+v %+v", p.Refused, p.Units)
	}
	wc := envExitEntryOf(p.Units[0], "c1")
	if wc.Move == nil || wc.Move.Col == Withdrawn || wc.Set[FieldEnvExitHistory] != "" || wc.Set[FieldEnvExit] != "" {
		t.Fatalf("routed as the decision's failed work, never an environment exit: %+v %+v", wc.Move, wc.Set)
	}
}

// readEnvSprint is a sprint with the primary p1 in review at attempt 1 and m1's read card of it
// working on m1's fleet row, the primary's read history holding prior (FieldEnvExitReads).
func readEnvSprint(t *testing.T, prior string) (*Snapshot, string) {
	t.Helper()
	s := &Snapshot{Now: time.Date(2026, 10, 11, 18, 25, 0, 0, time.UTC), Work: NewTable(Work), Readers: NewTable(Readers),
		Merge: NewTable(Merge), Fleet: NewTable(Fleet), Coordinator: "coordinator", Actor: "coordinator"}
	s.Work.SetRows([]string{"s1"})
	s.Fleet.SetRows([]string{"m1"})
	f := map[string]string{"kind": "primary", "stream": "s1", "attempt": "1", "head": "abc"}
	if prior != "" {
		f[FieldEnvExitReads] = prior
	}
	s.Work.Put(&Card{ID: "p1", Row: "s1", Col: Review, Fields: f})
	rid := ReadCardID("p1", 1, "m1") + ".g2"
	s.Fleet.Put(&Card{ID: rid, Row: "m1", Col: Working, Fields: map[string]string{"kind": "read", "primary": "p1", "stream": "s1", "reader": "m1",
		"attempt": "1", "head": "abc", "gen": "1", "asked": stamp(s.Now.Add(-time.Minute)), FieldReadCard: "1"}})
	return s, rid
}

// A read identity's environment exits are bounded (EnvReadCap, 3; the cold reader on #129):
// below the cap a 134/137 return spends no reader and counts on the primary; the return that
// reaches the cap retires the read as a return of the reader's own (spentBy: the read is dealt
// to the member no more) and raises one bound judgment, a kind wait refuses, naming the read,
// the reader and the exit codes. An exit of another attempt or reader does not count. Before
// the fix a read whose child always died 134 was dealt again forever.
func TestAReadsEnvironmentExitsAreCappedAndJudgedOnce(t *testing.T) {
	ret := func(s *Snapshot, rid string, exit int) Plan {
		return Read(s, ReadReq{Sel: Sel{IDs: []string{rid}}, As: "m1", Return: true, Reason: "no verdict (ran=false verdict=\"\"): aborted",
			Exit: exit, Harness: "Error: Unknown: ChildProcess.spawn (opencode)\n", Who: "m1"})
	}
	// the second: below the cap, unspent, counted (another attempt's and reader's exits not)
	s, rid := readEnvSprint(t, "0:m1=134,1:m2=134,1:m1=134")
	p := ret(s, rid, 137)
	if len(p.Units) != 1 || envExitEntryOf(p.Units[0], rid).Set["retired_by"] != RetiredByEnvironment {
		t.Fatalf("below the cap: retired by the environment, unspent: %+v", p.Units)
	}
	if got := envExitEntryOf(p.Units[0], "p1").Set[FieldEnvExitReads]; got != "0:m1=134,1:m2=134,1:m1=134,1:m1=137" {
		t.Fatalf("the primary counts it: %q", got)
	}
	for _, n := range p.Units[0].Notes {
		if n.Type == NBound {
			t.Fatalf("no judgment below the cap: %+v", n)
		}
	}
	// the third: the cap
	s, rid = readEnvSprint(t, "1:m1=134,1:m1=137")
	p = ret(s, rid, 134)
	if len(p.Units) != 1 {
		t.Fatalf("one unit: %+v", p)
	}
	rc := envExitEntryOf(p.Units[0], rid)
	if rc.Set["retired_by"] != RetiredByReturned || !readSpent([]*Card{{ID: rid, Row: "m1", Fields: rc.Set}}) {
		t.Fatalf("at the cap the reader is spent: %+v", rc.Set)
	}
	var bounds []Note
	for _, n := range p.Units[0].Notes {
		if n.Type == NBound && n.Kind == Judgment {
			bounds = append(bounds, n)
		}
	}
	if len(bounds) != 1 || !strings.Contains(bounds[0].What, rid+": m1's read of attempt 1 ended on the environment 3 times (the cap is 3) and is not dealt to m1 again: exits 134, 137, 134") ||
		WaitRefusal(bounds[0]) == "" {
		t.Fatalf("one bound judgment naming the read, the reader and the codes, wait refused: %+v", bounds)
	}
}

// The readers table's read is capped the same way: below the cap a 134/137 return goes back to
// asked without counting a re-ask; at EnvReadCap it is retired as a return of the reader's own,
// with one bound judgment.
func TestAReadersTableReadsEnvironmentExitsAreCapped(t *testing.T) {
	s := &Snapshot{Now: time.Date(2026, 10, 11, 18, 25, 0, 0, time.UTC), Work: NewTable(Work), Readers: NewTable(Readers),
		Merge: NewTable(Merge), Fleet: NewTable(Fleet), Coordinator: "coordinator", Actor: "coordinator"}
	s.Work.SetRows([]string{"s1"})
	s.Readers.SetRows([]string{"reader-a"})
	s.Work.Put(&Card{ID: "p1", Row: "s1", Col: Review, Fields: map[string]string{"kind": "primary", "stream": "s1", "attempt": "1", "head": "abc",
		FieldEnvExitReads: "1:reader-a=134,1:reader-a=134"}})
	s.Readers.Put(&Card{ID: "p1.r1", Row: "reader-a", Col: Reading, Fields: map[string]string{"kind": "read", "primary": "p1", "stream": "s1",
		"reader": "reader-a", "attempt": "1", "head": "abc", "gen": "1", "asked": stamp(s.Now.Add(-time.Minute)), "begun": stamp(s.Now)}})
	p := Read(s, ReadReq{Sel: Sel{IDs: []string{"p1.r1"}}, As: "reader-a", Return: true, Reason: "aborted", Exit: 137, Who: "reader-a"})
	if len(p.Refused) != 0 || len(p.Units) != 1 {
		t.Fatalf("one unit: %+v %+v", p.Refused, p.Units)
	}
	rc := envExitEntryOf(p.Units[0], "p1.r1")
	if !rc.Remove || rc.Set["retired_by"] != RetiredByReturned {
		t.Fatalf("at the cap the read is retired as returned: %+v", rc)
	}
	found := 0
	for _, n := range p.Units[0].Notes {
		if n.Type == NBound && strings.Contains(n.What, "exits 134, 134, 137") {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("one bound judgment: %+v", p.Units[0].Notes)
	}
}

// Two readers' environment exits of one primary in the one Read step accumulate on the
// primary, never overwriting each other (the cold reader's fix on #132): the step reads each
// exit off the history it is building, not the primary's original field each time, so both
// readers' entries survive and the reader that reached the cap is judged once.
func TestTwoReadersEnvironmentExitsOfOnePrimaryAccumulate(t *testing.T) {
	s := &Snapshot{Now: time.Date(2026, 10, 11, 18, 25, 0, 0, time.UTC), Work: NewTable(Work), Readers: NewTable(Readers),
		Merge: NewTable(Merge), Fleet: NewTable(Fleet), Coordinator: "coordinator", Actor: "coordinator"}
	s.Work.SetRows([]string{"s1"})
	s.Readers.SetRows([]string{"reader-a", "reader-b"})
	s.Work.Put(&Card{ID: "p1", Row: "s1", Col: Review, Fields: map[string]string{"kind": "primary", "stream": "s1", "attempt": "1", "head": "abc",
		FieldEnvExitReads: "1:reader-a=134,1:reader-a=137"}})
	rc := func(id, reader string) *Card {
		return &Card{ID: id, Row: reader, Col: Reading, Fields: map[string]string{"kind": "read", "primary": "p1", "stream": "s1",
			"reader": reader, "attempt": "1", "head": "abc", "gen": "1", "asked": stamp(s.Now.Add(-time.Minute)), "begun": stamp(s.Now)}}
	}
	s.Readers.Put(rc("p1.r1.reader-a", "reader-a"))
	s.Readers.Put(rc("p1.r1.reader-b", "reader-b"))
	p := Read(s, ReadReq{Sel: Sel{IDs: []string{"p1.r1.reader-a", "p1.r1.reader-b"}}, As: "reader-a,reader-b", Return: true,
		Reason: "no verdict (ran=false verdict=\"\"): aborted", Exit: 137, Harness: "Error: Unknown: ChildProcess.spawn (opencode)\n", Who: "m1"})
	if len(p.Refused) != 0 {
		t.Fatalf("no refusal: %+v", p.Refused)
	}
	var final string
	for _, u := range p.Units {
		for _, ch := range u.Changes {
			if ch.Entry.ID == "p1" && ch.Entry.Set[FieldEnvExitReads] != "" {
				final = ch.Entry.Set[FieldEnvExitReads]
			}
		}
	}
	want := "1:reader-a=134,1:reader-a=137,1:reader-a=137,1:reader-b=137"
	if final != want {
		t.Fatalf("both readers' exits accumulate on the primary:\n got %q\nwant %q", final, want)
	}
	bounds := 0
	for _, u := range p.Units {
		for _, n := range u.Notes {
			if n.Type == NBound && n.Kind == Judgment && strings.Contains(n.What, "exits 134, 137, 137") {
				bounds++
			}
		}
	}
	if bounds != 1 {
		t.Fatalf("one bound judgment for the reader that reached the cap: %d", bounds)
	}
}
