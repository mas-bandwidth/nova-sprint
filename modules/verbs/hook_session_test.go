package verbs

import (
	"reflect"
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
	want(t, w.do("carol", "hook", "--role", "coordinator"), 1, "carol is already hooked in as coordinator", "nova-sprint unhook --as carol")
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
