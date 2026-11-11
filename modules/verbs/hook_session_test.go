package verbs

import (
	"reflect"
	"strings"
	"testing"
)

// The sprint hook is bound to the bus hook it was made on (hooked.go;
// tla/Hook.tla): nova-bus who gives each name's hook a session id, new each
// time the hook is started, the sprint hook records it on the row, and a
// name whose recorded id is not the one who shows now is told to hook in.

// A new session that started the bus hook and never hooked in to the sprint
// is refused every acting verb and told how to hook in, in the role its row
// has; the hook then replaces the old one in place (the seat kept, the
// playbook line printed) and the verbs go through. A hook while the recorded
// bus session is the live one is refused as before.
func TestNewBusSessionHooksInBeforeItActs(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.bus.session = map[string]string{"carol": "bus-2"}
	hookIn := "hook in first: nova-sprint hook --as carol --role coordinator"
	want(t, w.do("carol", "hold", "alice", "--reason", "r"), 1, "SPRINT REFUSED hold: "+hookIn)
	want(t, w.do("carol", "queue"), 1, "SPRINT REFUSED queue: "+hookIn)
	want(t, w.do("carol", "next"), 0, "carol is not hooked in to the sprint", "next: nova-sprint hook --as carol --role coordinator")
	if s := w.snap(t); s.Rows["alice"].Held || s.Rows["carol"].Session != "s-carol" {
		t.Fatalf("a refused verb wrote: %+v", s.Rows)
	}
	want(t, w.do("dave", "hook", "--role", "coordinator"), 1, "the sprint's coordinator is carol")
	want(t, w.do("carol", "hook", "--role", "coordinator"), 0, "HOOKED carol role=coordinator", "playbook: nova-sprint help playbook coordinator", "next: nova-sprint next --as carol")
	if r := w.snap(t).Rows["carol"]; r.Session != "bus-2" || r.Role != "coordinator" || r.State != "up" {
		t.Fatalf("row %+v", r)
	}
	want(t, w.do("carol", "hold", "alice", "--reason", "r"), 0, "row alice held=true")
	want(t, w.do("carol", "hook", "--role", "coordinator"), 1, "SPRINT REFUSED hook: carol is already hooked in as coordinator: nova-sprint next --as carol")
}

// A name hooked in on the bus hook it has now is told to go on, never to
// unhook: the line a session follows must not hand back its hand or its
// seat.
func TestAlreadyHookedInNamesNext(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for name, role := range map[string]string{"alice": "worker", "carol": "coordinator"} {
		r := w.do(name, "hook", "--role", role)
		want(t, r, 1, "SPRINT REFUSED hook: "+name+" is already hooked in as "+role+": nova-sprint next --as "+name)
		if strings.Contains(r.Output, "unhook") {
			t.Fatalf("the refusal names unhook:\n%s", r.Output)
		}
	}
}

// A hooked row changes role only through unhook. A hook in another role is
// refused whether the row's bus session is the live one or an earlier one,
// so a coordinator whose bus hook was started again never empties the seat
// by hooking as a worker, and a worker never takes an empty seat in place,
// its worker cards in hand. The refusal names the hook that keeps the role
// and the unhook that changes it; the same role still replaces in place.
func TestHookNeverChangesRoleInPlace(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.bus.session = map[string]string{"carol": "bus-2"}
	want(t, w.do("carol", "hook", "--role", "worker"), 1, "SPRINT REFUSED hook: carol is hooked in as coordinator; unhook first to change role",
		"nova-sprint hook --as carol --role coordinator", "nova-sprint unhook --as carol")
	if s := w.snap(t); s.Rows["carol"].Session != "s-carol" || s.Rows["carol"].Role != "coordinator" || coordinatorOf(s) != "carol" {
		t.Fatalf("the refused hook moved the seat: %+v", s.Rows["carol"])
	}
	want(t, w.do("carol", "hook", "--role", "coordinator"), 0, "HOOKED carol role=coordinator")
	want(t, w.do("carol", "unhook"), 0, "UNHOOKED carol")

	// the seat is empty: a worker on a new bus session does not take it in place
	w.bus.session["alice"] = "bus-3"
	want(t, w.do("alice", "hook", "--role", "coordinator"), 1, "SPRINT REFUSED hook: alice is hooked in as worker; unhook first to change role",
		"nova-sprint hook --as alice --role worker", "nova-sprint unhook --as alice")
	if s := w.snap(t); s.Rows["alice"].Session != "s-alice" || s.Rows["alice"].Role != "worker" || coordinatorOf(s) != "" || s.Cards["p1.w1"].State != "working" {
		t.Fatalf("the refused hook took the seat: %+v", s.Rows["alice"])
	}
	// nor one on the bus session it hooked in on
	want(t, w.do("bob", "hook", "--role", "coordinator"), 1, "SPRINT REFUSED hook: bob is hooked in as worker; unhook first to change role",
		"nova-sprint next --as bob", "nova-sprint unhook --as bob")
	want(t, w.do("alice", "hook", "--role", "worker"), 0, "HOOKED alice role=worker")
}

// A row whose bus hook was started again mid-card gets "hook in first" on
// its next verb, hooks, and goes on with its hand as it was: take, pong,
// finish and read.
func TestBusHookRestartKeepsTheHand(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.bus.session = map[string]string{"alice": "bus-2", "bob": "bus-3"}
	acts := []struct {
		as   string
		args []string
	}{
		{"alice", []string{"take", "p1.w1@1"}},
		{"alice", []string{"pong", "--nonce", "a1b2"}},
		{"alice", []string{"finish", "p1.w1@1", "--head", head, "--cost", "0.04"}},
		{"bob", []string{"read", "p2.r1@1", "yes", "--cost", "0.04"}},
	}
	for _, a := range acts {
		want(t, w.do(a.as, a.args...), 1, "hook in first: nova-sprint hook --as "+a.as+" --role worker")
	}
	before := w.snap(t)
	want(t, w.do("alice", "hook", "--role", "worker"), 0, "HOOKED alice role=worker", "playbook: nova-sprint help playbook worker")
	want(t, w.do("bob", "hook", "--role", "worker"), 0, "HOOKED bob role=worker")
	after := w.snap(t)
	if !reflect.DeepEqual(before.Cards, after.Cards) {
		t.Fatalf("the hook moved a card:\n%+v\n%+v", before.Cards, after.Cards)
	}
	for n, id := range map[string]string{"alice": "bus-2", "bob": "bus-3"} {
		if r, was := after.Rows[n], before.Rows[n]; r.Session != id || r.Role != "worker" || r.State != was.State || r.Held != was.Held {
			t.Fatalf("row %s %+v, was %+v", n, r, was)
		}
	}
	for _, a := range acts {
		want(t, w.do(a.as, a.args...), 0)
	}
}
