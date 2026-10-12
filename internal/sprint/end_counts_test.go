package sprint

import "testing"

func TestEndCountsFromTypedEvents(t *testing.T) {
	classes := []EndClass{EndClassOK, EndClassFailedWork, EndClassNoResult, EndClassProvider, EndClassCoordination, EndClassEnvironment, EndClassHarness, EndClassClear}
	var lines []Line
	for _, class := range classes {
		lines = append(lines, Line{Kind: LineMove, Table: Fleet, Card: "p.w1", From: "m1:working", To: "m1:withdrawn", Verb: "finish", Set: map[string]string{"end_class": string(class)}})
	}
	lines = append(lines, Line{Kind: LineMove, Table: Fleet, Card: "legacy.w1", From: "m1:working", To: "m1:failed", Verb: "finish", Set: map[string]string{"ok": "no", "report": "134 exit"}})
	got := EndCountsFromLines(lines)["m1"]
	if got != (EndCounts{OK: 1, Failed: 1, NoResult: 1, Provider: 1, Coordination: 1, Environment: 1, Harness: 1, Clear: 1, Unknown: 1}) || got.OKPercent() != 100.0/3.0 {
		t.Fatalf("counts=%+v ok%%=%v", got, got.OKPercent())
	}
}
