package verbs

import (
	"cmp"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The beat every worker sends every few ms: it writes only alice's row
// telemetry, and moves the store's seq.
var aliceBeat = store.SetRowLoad{Row: "alice", Load1Centi: 7, Cores: 8}

// between is the store of w with ws landing, as one op by another actor,
// right before the verb's op of kind reaches it.
func between(w *world, kind string, ws ...store.Write) *movingStore {
	return &movingStore{Fake: w.f, kind: kind, between: store.Op{ID: "between-" + kind, Epoch: 1, Actor: "alice", Kind: "between", At: t0, Writes: ws}}
}

// noCoordinator is a fresh store, as up's walk makes it, with alice hooked
// in as a worker and no coordinator: carol may hook in as one.
func noCoordinator(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	f := store.NewFake()
	w.f, w.e.Store = f, f
	c := Choices{Store: f, Actor: "carol", Now: func() time.Time { return t0 }}
	if err := c.Save(context.Background(), "coordinator", "carol"); err != nil {
		t.Fatal(err)
	}
	op := store.Op{ID: "alice", Epoch: 1, Actor: "alice", Kind: "hook", At: t0, Writes: []store.Write{store.PutRow{Name: "alice", Session: "s-a", Role: "worker"}}}
	if r, err := f.Apply(context.Background(), op); err != nil || !r.Accepted {
		t.Fatalf("hook alice: %+v %v", r, err)
	}
	return w
}

// TestVerbsFencedOnRecordsNotTheSeq: each verb that read a part of the
// store and wrote on it was fenced on the store's seq, so a worker's beat
// landing between its read and its commit refused it ("the store is at seq
// N+1, expected seq N": stream add in TestFunctionalEndToEnd on PR #772,
// clear in #773, add in #764). Each is now fenced on the part it read: a
// beat lands between and the verb commits; a write to that part lands
// between and the verb is still refused, naming it.
func TestVerbsFencedOnRecordsNotTheSeq(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, kind string
		world      func(*testing.T) *world
		as         string
		args       []string
		ok         string
		matters    store.Write
		refused    string
	}{
		{"stream add", "stream-add", newWorld, "carol", []string{"stream", "add", "t", "--repo", "o/r", "--forge", "github"}, "STREAM t repo=o/r",
			store.PutStream{Name: "u", Repo: "o/r", Base: "main", Pos: 9}, "wrote streams"},
		{"stream set", "stream-set", newWorld, "carol", []string{"stream", "set", "s", "--gate", "make check"}, "STREAM s mode=auto",
			store.SetStreamMode{Stream: "s", Mode: "hand"}, "wrote streams"},
		{"identity", "identity", newWorld, "carol", []string{"identity", "--name", "New Bot", "--email", "3+new-bot@users.noreply.example.test", "--reason", "r"}, "IDENTITY New Bot",
			store.SetIdentity{Name: "Other Bot", Email: "2+other-bot@users.noreply.example.test"}, "wrote the identity"},
		{"stop", "stop", newWorld, "carol", []string{"stop", "--reason", "deploy"}, "STOPPED applied at seq",
			store.SetStopped{Stopped: true, Reason: "someone else"}, "wrote the stop"},
		{"hook as coordinator", "hook", noCoordinator, "carol", []string{"hook", "--role", "coordinator"}, "HOOKED carol role=coordinator",
			store.PutRow{Name: "zed", Session: "s-z", Role: "coordinator"}, "wrote row zed's hook"},
		{"take-over", "take-over", takeoverWorld, "dave", []string{"up", "--take-over", "--reason", "carol's host is down"}, "TOOK OVER from carol",
			store.PutRow{Name: "zed", Session: "s-z", Role: "coordinator"}, "wrote row zed's hook"},
	} {
		t.Run(c.name+": a beat between", func(t *testing.T) {
			t.Parallel()
			w := c.world(t)
			w.e.Store = between(w, c.kind, aliceBeat)
			want(t, w.do(c.as, c.args...), 0, c.ok)
			if got := w.snap(t).Rows["alice"].Load1Centi; got != aliceBeat.Load1Centi {
				t.Fatalf("alice's load %d: the beat did not land between", got)
			}
		})
		t.Run(c.name+": its own part written between", func(t *testing.T) {
			t.Parallel()
			w := c.world(t)
			w.e.Store = between(w, c.kind, c.matters)
			want(t, w.do(c.as, c.args...), 1, "the store changed since seq", c.refused)
		})
	}
}

// TestCoordinatorVerbsFencedOnTheirHook: stream add, stream set, identity and stop
// read the caller as the coordinator on their snapshot; unhooked between
// the read and the commit (a take-over), the op is refused, as the seq
// fence refused it.
func TestCoordinatorVerbsFencedOnTheirHook(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"stream", "add", "t", "--repo", "o/r", "--forge", "github"},
		{"stream", "set", "s", "--gate", "make check"},
		{"identity", "--name", "New Bot", "--email", "3+new-bot@users.noreply.example.test", "--reason", "r"},
		{"stop", "--reason", "deploy"},
	} {
		t.Run(args[0]+" "+args[1], func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			kind := map[string]string{"stream add": "stream-add", "stream set": "stream-set"}[args[0]+" "+args[1]]
			w.e.Store = between(w, cmp.Or(kind, args[0]), store.PutRow{Name: "carol"})
			want(t, w.do("carol", args...), 1, "the store changed since seq", "wrote row carol's hook")
		})
	}
}

// TestSetupAnswerFencedOnPolicies: a setup answer (an inactive data
// version) commits over a beat, and is refused by another version stored
// between its read and its commit.
func TestSetupAnswerFencedOnPolicies(t *testing.T) {
	t.Parallel()
	save := func(t *testing.T, ws ...store.Write) error {
		w := noCoordinator(t)
		c := Choices{Store: between(w, "setup answer", ws...), Actor: "carol", Now: func() time.Time { return t0 }}
		return c.Save(context.Background(), "dashboard", "tailnet")
	}
	if err := save(t, aliceBeat); err != nil {
		t.Fatalf("a setup answer over a beat: %v", err)
	}
	err := save(t, store.PutPolicy{Kind: "data", Name: "setup:run-on", Body: "studio"})
	if err == nil || !strings.Contains(err.Error(), "wrote policies") {
		t.Fatalf("a setup answer over another version: %v", err)
	}
}
