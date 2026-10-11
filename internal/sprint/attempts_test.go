package sprint

import (
	"strings"
	"testing"
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
