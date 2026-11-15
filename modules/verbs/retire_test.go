package verbs

import (
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
)

// TestRetire: the coordinator retires alice once nova-config no longer
// names her: a dry run applies nothing; the retire returns her hand (p1.w1,
// working) to the queue and retires the row in one op, and she is then in
// no read (show, check, next).
func TestRetire(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	delete(w.roster.ro.Workers, "alice")
	want(t, w.do("carol", "retire", "alice", "--reason", "machine removed", "--dry-run"), 0, "p1.w1->fail", "row alice retired (dry run)", "next: nova-sprint retire alice --reason")
	if _, ok := w.snap(t).Rows["alice"]; !ok {
		t.Fatal("a dry run retired alice")
	}
	want(t, w.do("carol", "retire", "alice", "--reason", "machine removed"), 0, "p1.w1->fail", "row alice retired", "next: "+waitNext)
	s := w.snap(t)
	if _, ok := s.Rows["alice"]; ok {
		t.Fatalf("alice is still in a read: %+v", s.Rows["alice"])
	}
	if c := s.Cards["p1.w1"]; c.State != "fail" || c.Holder != "alice" {
		t.Fatalf("p1.w1 = %+v: released, its holder kept", c)
	}
	want(t, w.do("carol", "retire", "alice", "--reason", "again"), 1, "no row alice in the store")
}

// TestRetireRefusals: retire refuses with no reason, a row the store lacks,
// a row nova-config still names (naming the nova-config verbs), a hooked
// coordinator, a worker asking, and anything at all when nova-config cannot
// be read or names nothing (a fault must never retire the fleet).
func TestRetireRefusals(t *testing.T) {
	t.Parallel()
	gone := func(_ *testing.T, w *world) { delete(w.roster.ro.Workers, "alice"); delete(w.roster.ro.Kin, "carol") }
	runCases(t, []tcase{
		{name: "no reason", as: "carol", prep: gone, args: []string{"retire", "alice"}, exit: 1, want: []string{"a retire needs its reason"}},
		{name: "no name", as: "carol", args: []string{"retire", "--reason", "r"}, exit: 2, want: []string{"retire takes"}},
		{name: "no such row", as: "carol", prep: gone, args: []string{"retire", "zz", "--reason", "r"}, exit: 1, want: []string{"no row zz in the store"}},
		{name: "still in nova-config", as: "carol", args: []string{"retire", "bob", "--reason", "r"}, exit: 1,
			want: []string{"row bob is still in nova-config", "nova-config machine remove bob", "nova-config friend remove bob"}},
		{name: "a hooked coordinator", as: "carol", prep: gone, args: []string{"retire", "carol", "--reason", "r"}, exit: 1,
			want: []string{"row carol is hooked as the coordinator", "nova-sprint unhook carol"}},
		{name: "a worker", as: "alice", prep: gone, args: []string{"retire", "alice", "--reason", "r"}, exit: 1, want: []string{"coordinator"}},
		{name: "nova-config unreadable", as: "carol", prep: func(_ *testing.T, w *world) { w.roster.err = errors.New("postgres: connect failed") }, args: []string{"retire", "alice", "--reason", "r"}, exit: 2,
			want: []string{"nothing is retired on a failed read", "postgres: connect failed"}},
		{name: "nova-config empty", as: "carol", prep: func(_ *testing.T, w *world) { w.roster.ro = roster.Roster{} }, args: []string{"retire", "alice", "--reason", "r"}, exit: 1,
			want: []string{"nothing is retired on an empty roster"}},
	})
}

// TestCheckNamesRowsNovaConfigDoesNotName: check prints a WARN line for a
// store row nova-config does not name (the run retires it on its next tick)
// and one for an unnamed hooked coordinator (never retired).
func TestCheckNamesRowsNovaConfigDoesNotName(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	healthy(t, w)
	delete(w.roster.ro.Workers, "alice")
	delete(w.roster.ro.Kin, "carol")
	r := w.do("", "check")
	want(t, r, r.Exit, "CHECK WARN row alice is not in nova-config; the run retires it on its next tick: wait one tick, or nova-sprint retire alice --reason",
		"CHECK WARN row carol is not in nova-config and is hooked as the coordinator, so it is never retired")
}

// TestHookWaitsForNovaConfig: a retired row's next --json says it is not
// hooked in ("unhooked", the typed answer its worker hooks again on); the
// worker's hook is refused while nova-config does not name the row, naming
// the nova-config verbs, and goes through once nova-config names it again
// (tla/RowAlive.tla RehookDue). A failed or empty roster read refuses no
// hook; a hooked row's next carries no "unhooked".
func TestHookWaitsForNovaConfig(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	alice := w.roster.ro.Workers["alice"]
	delete(w.roster.ro.Workers, "alice")
	want(t, w.do("carol", "retire", "alice", "--reason", "machine removed"), 0, "row alice retired")
	want(t, w.do("alice", "next", "--json"), 0, `"unhooked":true`)
	want(t, w.do("alice", "hook", "--role", "worker"), 1, "alice is not in nova-config", "nova-config machine add alice", "nova-config friend add alice")
	w.roster.ro.Workers["alice"] = alice
	want(t, w.do("alice", "hook", "--role", "worker"), 0, "HOOKED alice role=worker")
	if r := w.do("alice", "next", "--json"); strings.Contains(r.Output, `"unhooked"`) {
		t.Fatalf("a hooked row's next says unhooked: %s", r.Output)
	}
	for _, prep := range []func(*world){
		func(w *world) { w.roster.err = errors.New("postgres: connect failed") },
		func(w *world) { w.roster.ro = roster.Roster{} },
	} {
		w := newWorld(t)
		delete(w.roster.ro.Workers, "alice")
		want(t, w.do("carol", "retire", "alice", "--reason", "machine removed"), 0, "row alice retired")
		prep(w)
		want(t, w.do("alice", "hook", "--role", "worker"), 0, "HOOKED alice role=worker")
	}
}
