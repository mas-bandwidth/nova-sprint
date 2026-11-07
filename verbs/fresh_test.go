package verbs

import (
	"context"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// midUp is the store as up's walk leaves it after step 2 (the store made)
// and before step 8: a fresh store, stopped and never started, the walk's
// answers saved, the coordinator hooked in (up hooks carol once the store
// exists); no stream, no card, no start.
func midUp(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	f := store.NewFake()
	w.f, w.e.Store = f, f
	c := Choices{Store: f, Actor: "carol", Now: func() time.Time { return t0 }}
	for _, qa := range [][2]string{{"coordinator", "carol"}, {"run-on", "studio"}, {"dashboard", "tailnet"}} {
		if err := c.Save(context.Background(), qa[0], qa[1]); err != nil {
			t.Fatal(err)
		}
	}
	want(t, w.do("carol", "hook", "--role", "coordinator"), 0, "HOOKED carol role=coordinator")
	return w
}

// stop, start, hook, show and next work on any store with an epoch,
// whatever up's step (Glenn, 2026-11-07: "A sprint that can move must be
// stoppable"); the fresh store is stopped, never started, until start.
func TestVerbsWorkMidUp(t *testing.T) {
	w := midUp(t)
	if s := w.snap(t); !s.Stopped || s.StopReason != store.NeverStarted {
		t.Fatalf("a fresh store: stopped %v (%q)", s.Stopped, s.StopReason)
	}
	want(t, w.do("carol", "show"), 0, `"Stopped": true`, `"StopReason": "never started"`)
	want(t, w.do("carol", "next"), 0, "NEXT no stream exists", "next: nova-sprint stream add")
	want(t, w.do("dave", "hook", "--role", "worker"), 0, "HOOKED dave role=worker")
	want(t, w.do("dave", "next"), 0, "NEXT the sprint is stopped (never started)", "next: nothing; wait for the bus")
	want(t, w.do("dave", "show", "dave"), 0, `"role": "worker"`)
	want(t, w.do("carol", "stop", "--reason", "hold it"), 0, "STOPPED already: the sprint is already stopped (never started)", "next: nova-sprint start --as carol")
	want(t, w.do("carol", "start", "--reason", "go"), 0, "STARTED applied at seq")
	if s := w.snap(t); s.Stopped || s.StopReason != "go" {
		t.Fatalf("after start: stopped %v (%q)", s.Stopped, s.StopReason)
	}
	want(t, w.do("carol", "stop", "--reason", "pause"), 0, "STOPPED applied at seq", "next: nova-sprint start --as carol")
}

// A stopped sprint with nothing owed tells the coordinator to start it and
// a worker to wait; its own card still comes first.
func TestNextNamesStartOnAStoppedSprint(t *testing.T) {
	s := sprint()
	s.Stopped, s.StopReason = true, store.NeverStarted
	w := worldFrom(t, s)
	want(t, w.do("carol", "next"), 0, "NEXT the sprint is stopped (never started)", "next: nova-sprint start --as carol --reason")
	want(t, w.do("alice", "next"), 0, "NEXT work p1.w1 is dealt with alice")
}

// Before the store exists (no epoch: up at step 1), every verb that reads
// it refuses naming up, and next answers up (exit 0).
func TestVerbsBeforeTheStore(t *testing.T) {
	for _, args := range [][]string{{"stop", "--reason", "r"}, {"start"}, {"hook", "--role", "worker"}, {"show"}, {"queue"}, {"stream", "add", "s", "--repo", "o/r"}, {"take", "p1.w1@1"}} {
		w := newWorld(t)
		w.e.Store, w.doctor.schema = failingStore{w.f}, false
		want(t, w.do("carol", args...), 1, "SPRINT REFUSED", "the sprint is not set up: nova-sprint up --as carol")
	}
	w := newWorld(t)
	w.e.Store, w.doctor.schema = failingStore{w.f}, false
	want(t, w.do("carol", "next"), 0, "NEXT the sprint is not set up", "next: nova-sprint up --as carol")
}

// Defect 23: start on a running sprint says it is already running, a
// success that changes nothing, with the next verb.
func TestStartOnARunningSprint(t *testing.T) {
	w := newWorld(t)
	seq := w.snap(t).Seq
	want(t, w.do("carol", "start", "--reason", "again"), 0, "RUNNING already: the sprint is already running", "next: nova-sprint next --as carol")
	if s := w.snap(t); s.Seq != seq || s.Stopped {
		t.Fatalf("start on a running sprint changed the store: seq %d -> %d, stopped %v", seq, s.Seq, s.Stopped)
	}
}

// The help says what each verb needs from up.
func TestHelpSaysWhatUpGives(t *testing.T) {
	w := newWorld(t)
	want(t, w.do("", "help", "stop"), 0, "up: the store up makes at step 2 (an epoch), at any later step of up")
	want(t, w.do("", "help", "check"), 0, "up: nothing")
	want(t, w.do("", "help"), 0, "whatever step up is at")
}
