package verbs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// A build one schema hash ahead of the store (or another build's store) is
// a live sprint: every verb, next and check name the two hashes and the
// command that reconciles them, never "not set up" (L6.6).
func TestNextOnSchemaSkewNamesVersion(t *testing.T) {
	t.Parallel()
	older := store.SchemaIdentity{Present: true, Hash: strings.Repeat("a", 64), Expected: strings.Repeat("b", 64), KnownOlder: true, Round: 25}
	other := store.SchemaIdentity{Present: true, Hash: strings.Repeat("c", 64), Expected: strings.Repeat("b", 64)}
	for _, c := range []struct {
		id   store.SchemaIdentity
		want []string
	}{
		{older, []string{"the store is at schema aaaaaaaaaaaa (round 25), this build wants bbbbbbbbbbbb", "on the coordinator, with this build: nova-sprint up --as <coordinator>"}},
		{other, []string{"the store is at schema cccccccccccc (an unknown round), this build wants bbbbbbbbbbbb", "run the coordinator's build"}},
	} {
		skewed := func(_ *testing.T, w *world) { w.e.Store = failingStore{w.f}; w.doctor.schemaErr = schemaSkew(c.id) }
		w := newWorld(t)
		skewed(t, w)
		r := w.do("alice", "next")
		want(t, r, 0, append([]string{"NEXT "}, c.want...)...)
		if strings.Contains(r.Output, "not set up") {
			t.Fatal(r.Output)
		}
		want(t, w.do("alice", "show"), 1, c.want...)
		want(t, w.do("carol", "check"), 1, append([]string{"CHECK RED "}, c.want...)...)
	}
}

// --epoch is every verb's: left out it is the active epoch, given it must be
// the active one on every verb that reads the store, next included (L6.23).
func TestEpochOptionalEverywhere(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	activePolicy(t, w)
	want(t, w.do("carol", "next", "--epoch", "1"), 0, "NEXT ")
	want(t, w.do("carol", "next", "--epoch", "2"), 0, "NEXT epoch 2 is over: the sprint is in epoch 1", "next: run nova-sprint next")
	want(t, w.do("alice", "show", "--epoch", "1"), 0)
	want(t, w.do("alice", "show", "--epoch", "2"), 1, "epoch 2 is over")
	want(t, w.do("carol", "hold", "p1", "--reason", "r", "--epoch", "3"), 1, "epoch 3 is over")
	want(t, w.do("alice", "take", "p1.w1@1"), 0)
	want(t, w.do("alice", "next", "--epoch", "x"), 2, "--epoch takes a positive whole number")
	want(t, w.do("", "help", "--epoch", "1"), 0)
}

// next tells the coordinator what check reds before the sprint can deal,
// identity, then policy, then run, before it says start or wait; a worker
// is told it is the coordinator's and waits (L6.5).
func TestNextOrdersIdentityPolicyRunBeforeStart(t *testing.T) {
	t.Parallel()
	s := sprint()
	s.Cards = map[string]store.Card{}
	s.Identity = store.Identity{}
	s.Stopped, s.StopReason = true, store.NeverStarted
	w := worldFrom(t, s)
	want(t, w.do("carol", "next"), 0, "NEXT the sprint has no commit identity", "next: "+identityFix)
	want(t, w.do("alice", "next"), 0, "NEXT the sprint has no commit identity: no worker can hook and nothing can be committed (the coordinator's to fix)", "next: "+waitNext)
	setIdentity(t, w.f)
	want(t, w.do("carol", "next"), 0, "NEXT no policy is active", "next: "+strings.ReplaceAll(policy.FirstPolicyFix, "<coordinator>", "carol"))
	activePolicy(t, w)
	w.doctor.run = run.ChooserStatus{}
	want(t, w.do("carol", "next"), 0, "NEXT nothing runs the sprint", "next: read the run service's log (nova-sprint show run), then nova-sprint up --as carol")
	w.doctor.run = run.ChooserStatus{Held: true, Operation: "run", Host: "studio", PID: 41}
	want(t, w.do("carol", "next"), 0, "NEXT stream s has no cards")
}

// policy show default prints the built-in as a file policy set takes, so
// check's no-policy fix works on a store up did not make (L5.21).
func TestPolicyShowDefaultThenSetActivates(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.e.Policy = policy.NewEngine(stillClock{})
	r := w.do("carol", "policy", "show", "default")
	if r.Exit != 0 || !strings.HasPrefix(r.Output, strings.TrimRight(policy.DefaultBody(), "\n")) || !strings.HasSuffix(r.Output, "\n# next: nova-sprint policy set <file> --reason \"<why>\"\n") {
		t.Fatalf("show default: %+v", r)
	}
	file := filepath.Join(t.TempDir(), "policy.star")
	if err := os.WriteFile(file, []byte(r.Output), 0o600); err != nil {
		t.Fatal(err)
	}
	want(t, w.do("carol", "policy", "set", file, "--reason", "the first policy"), 0, "POLICY STORED AND ACTIVE v1")
	if w.snap(t).Policy != 1 {
		t.Fatal("not active")
	}
}
