package main

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

func TestDoctorFaultsCleanStore(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	r := sprint.JudgeSeatCheck(sprint.SeatCheckMeasures{}, now)
	r.Lines = nil
	r.Down = 0
	rec := store.StopsRecord{Seat: sprint.SeatWaits{}}
	if got := doctorFaults(r, rec, now); len(got) != 0 {
		t.Fatalf("clean doctor has faults: %v", got)
	}
}

func TestDoctorFaultsNamesSeatCheckEvidence(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	r := sprint.SeatCheckReport{Lines: []sprint.SeatCheckLine{{Thing: sprint.SeatCheckFleet, Facts: []string{"member=m1"}}}}
	rec := store.StopsRecord{}
	got := doctorFaults(r, rec, now)
	if len(got) != 1 || got[0] != "MACHINERY fleet DOWN member=m1" {
		t.Fatalf("fault evidence = %v", got)
	}
}

func TestDoctorFaultsNamesOverdueSeatEvidence(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	rec := store.StopsRecord{Seat: sprint.SeatWaits{Judgments: 1, Overdue: 1}}
	got := doctorFaults(sprint.SeatCheckReport{}, rec, now)
	if len(got) != 1 || got[0] == "" {
		t.Fatalf("overdue fault evidence = %v", got)
	}
}
