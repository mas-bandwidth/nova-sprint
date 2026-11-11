package run

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The kin challenge (v1's, brought back): a kin row whose bus hook is up
// with no session behind it answers no challenge, goes down after two
// windows with its hand returned, is dealt nothing, and comes back only on
// a pong to a nonce the run issued. Clocks start at 1000.

var kt0 = time.Unix(1000, 0).UTC()

var window = time.Duration(steps.Engine.ChallengeEvery) * time.Second

func kinMust(t *testing.T, st store.Store, id string, ws ...store.Write) {
	t.Helper()
	r, err := st.Apply(context.Background(), store.Op{ID: id, Epoch: 1, Actor: "test", Kind: "test", At: kt0, Writes: ws})
	if err != nil || !r.Accepted {
		t.Fatalf("%s: %+v %v", id, r, err)
	}
}

// kinSeeded is a started sprint with one stream, two producers and kin row
// bob (width 2) hooked in and up, and a ticker whose nonces are n1, n2, ...
func kinSeeded(t *testing.T) (*store.Fake, roster.Roster, *Ticker) {
	t.Helper()
	f := store.NewFake()
	kinMust(t, f, "start", store.SetStopped{Stopped: false, Reason: "test"})
	kinMust(t, f, "policy", activePolicy()...)
	kinMust(t, f, "stream", store.PutStream{Name: "s", Repo: "https://example.invalid/r.git", Base: "main", Forge: "github"})
	kinMust(t, f, "row", store.PutRow{Name: "bob", Session: "sess-1", Role: "worker"})
	kinMust(t, f, "up", store.SetRowState{Row: "bob", State: "up"})
	kinMust(t, f, "p1", store.PutCard{ID: "p1", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p1", Paths: []string{"a"}})
	kinMust(t, f, "p2", store.PutCard{ID: "p2", Kind: "producer", Stream: "s", Tier: "pro", Brief: "do p2", Paths: []string{"b"}})
	ro := roster.Roster{Kin: map[string]roster.Kin{"bob": {Name: "bob", Model: "kin/model", Width: 2, Tiers: []string{"flash", "pro", "heavy", "frontier"}}}}
	tk, err := NewTicker(context.Background(), f, &healthy{}, &answering{}, kt0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	tk.kin.nonce = func() string { n++; return fmt.Sprintf("n%d", n) }
	return f, ro, tk
}

func kinTick(t *testing.T, tk *Ticker, ro roster.Roster, now time.Time, present ...string) {
	t.Helper()
	if _, err := tk.Tick(context.Background(), Observed{Now: now, Present: present, Roster: ro, Heads: map[string]string{"s": strings.Repeat("a", 40)}}); err != nil {
		t.Fatal(err)
	}
}

func pong(t *testing.T, f store.Store, row, nonce string, at time.Time) {
	t.Helper()
	r, err := f.Apply(context.Background(), store.Op{ID: steps.PongOpID(1, row, nonce), Epoch: 1, Actor: row, Kind: "pong", At: at})
	if err != nil || !r.Accepted {
		t.Fatalf("pong %s: %+v %v", nonce, r, err)
	}
}

// hand is the live consumers row holds.
func hand(tk *Ticker, row string) []string {
	var out []string
	for id, c := range tk.Snapshot().Cards {
		if c.Holder == row && (c.State == "working" || c.State == "ready") {
			out = append(out, id)
		}
	}
	return out
}

// downAndReturned ticks bob, on the bus throughout and answering nothing,
// past two windows: bob is down, its hand is back in the queue, and a down
// judgement names it.
func downAndReturned(t *testing.T, tk *Ticker, ro roster.Roster) time.Time {
	t.Helper()
	for i := 1; i <= 3; i++ { // the first tick resolves, the next deal
		kinTick(t, tk, ro, kt0.Add(time.Duration(i)*time.Second), "bob")
	}
	if got := hand(tk, "bob"); len(got) == 0 {
		t.Fatalf("bob was dealt nothing")
	}
	for at := kt0.Add(window / 2); at.Before(kt0.Add(2 * window)); at = at.Add(window / 2) {
		kinTick(t, tk, ro, at, "bob")
	}
	if r := tk.Snapshot().Rows["bob"]; r.State != "up" {
		t.Fatalf("bob went %s before two windows passed", r.State)
	}
	now := kt0.Add(2*window + 2*time.Second)
	kinTick(t, tk, ro, now, "bob")
	snap := tk.Snapshot()
	if snap.Rows["bob"].State != "down" {
		t.Fatalf("bob, on the bus and answering no challenge for two windows, is %s", snap.Rows["bob"].State)
	}
	if got := hand(tk, "bob"); len(got) != 0 {
		t.Fatalf("bob is down and still holds %v", got)
	}
	for _, p := range []string{"p1", "p2"} {
		if st := snap.Cards[p].State; st != "ready" {
			t.Fatalf("%s is %s, want ready (back in the queue)", p, st)
		}
	}
	found := false
	for _, j := range snap.Judgements {
		found = found || j.Kind == "down" && j.About == "bob" && j.State == "open"
	}
	if !found {
		t.Fatalf("no open down judgement names bob: %+v", snap.Judgements)
	}
	return now
}

// (a) a kin row on the bus that answers no challenge goes down after two
// windows and its cards return to ready.
func TestKinSilentGoesDownAndHandReturns(t *testing.T) {
	t.Parallel()
	_, ro, tk := kinSeeded(t)
	downAndReturned(t, tk, ro)
}

// (b) while down it is dealt nothing.
func TestKinDownIsDealtNothing(t *testing.T) {
	t.Parallel()
	_, ro, tk := kinSeeded(t)
	now := downAndReturned(t, tk, ro)
	for i := 1; i <= 3; i++ {
		kinTick(t, tk, ro, now.Add(time.Duration(i)*time.Second), "bob")
	}
	if got := hand(tk, "bob"); len(got) != 0 {
		t.Fatalf("bob is down and was dealt %v", got)
	}
}

// (c) a presence flicker (off the bus, then on) does not bring it up.
func TestKinPresenceFlickerStaysDown(t *testing.T) {
	t.Parallel()
	_, ro, tk := kinSeeded(t)
	now := downAndReturned(t, tk, ro)
	kinTick(t, tk, ro, now.Add(time.Second))
	kinTick(t, tk, ro, now.Add(2*time.Second), "bob")
	if st := tk.Snapshot().Rows["bob"].State; st != "down" {
		t.Fatalf("a presence flicker brought bob %s", st)
	}
}

// (d) a pong to an issued nonce brings it up, and it is dealt again.
func TestKinPongBringsItUp(t *testing.T) {
	t.Parallel()
	f, ro, tk := kinSeeded(t)
	now := downAndReturned(t, tk, ro)
	b := &fakeBus{}
	for !strings.Contains(fmt.Sprint(b.sent), "CHALLENGE") { // the run keeps challenging a down kin row on the bus
		if now = now.Add(10 * time.Second); now.After(kt0.Add(4 * window)) {
			t.Fatalf("no challenge to bob, down, within a window")
		}
		kinTick(t, tk, ro, now, "bob")
		b.sent = nil
		tk.Push(context.Background(), b, now)
	}
	if st := tk.Snapshot().Rows["bob"].State; st != "down" {
		t.Fatalf("bob came %s before it answered", st)
	}
	nonce := lastChallenge(t, b, "bob")
	pong(t, f, "bob", nonce, now.Add(time.Second))
	kinTick(t, tk, ro, now.Add(2*time.Second), "bob")
	kinTick(t, tk, ro, now.Add(3*time.Second), "bob")
	if st := tk.Snapshot().Rows["bob"].State; st != "up" {
		t.Fatalf("bob answered challenge %s and is %s", nonce, st)
	}
	if got := hand(tk, "bob"); len(got) == 0 {
		t.Fatalf("bob is up again and was dealt nothing")
	}
}

// (e) a pong to a nonce the run did not issue counts for nothing.
func TestKinWrongNonceStaysDown(t *testing.T) {
	t.Parallel()
	f, ro, tk := kinSeeded(t)
	now := downAndReturned(t, tk, ro)
	pong(t, f, "bob", "guessed", now.Add(time.Second))
	kinTick(t, tk, ro, now.Add(2*time.Second), "bob")
	if st := tk.Snapshot().Rows["bob"].State; st != "down" {
		t.Fatalf("a pong to a nonce never issued brought bob %s", st)
	}
}

// A pong two windows old does not raise a row that comes back to the bus
// (KinAlive.tla RaiseDue): it would go straight down again.
func TestKinStalePongStaysDown(t *testing.T) {
	t.Parallel()
	f, ro, tk := kinSeeded(t)
	now := downAndReturned(t, tk, ro)
	b := &fakeBus{}
	for !strings.Contains(fmt.Sprint(b.sent), "CHALLENGE") {
		if now = now.Add(10 * time.Second); now.After(kt0.Add(4 * window)) {
			t.Fatalf("no challenge to bob, down, within a window")
		}
		kinTick(t, tk, ro, now, "bob")
		b.sent = nil
		tk.Push(context.Background(), b, now)
	}
	pong(t, f, "bob", lastChallenge(t, b, "bob"), now.Add(time.Second))
	kinTick(t, tk, ro, now.Add(2*time.Second)) // off the bus as it answered
	later := now.Add(2*window + 2*time.Second)
	kinTick(t, tk, ro, later, "bob")
	if r := tk.Snapshot().Rows["bob"]; r.State != "down" || r.Flaps != 1 {
		t.Fatalf("a pong %s old brought bob up (%s, %d flaps; want down, 1)", later.Sub(now), r.State, r.Flaps)
	}
}

// A row that answers each challenge stays up and keeps its hand past two
// windows; the challenge goes once a window, not every tick.
func TestKinAnsweringStaysUp(t *testing.T) {
	t.Parallel()
	f, ro, tk := kinSeeded(t)
	b := &fakeBus{}
	sent := 0
	for at := kt0.Add(time.Second); at.Before(kt0.Add(3 * window)); at = at.Add(10 * time.Second) {
		kinTick(t, tk, ro, at, "bob")
		b.sent = nil
		tk.Push(context.Background(), b, at)
		if strings.Contains(fmt.Sprint(b.sent), "CHALLENGE") {
			sent++
			pong(t, f, "bob", lastChallenge(t, b, "bob"), at.Add(time.Second))
		}
	}
	if st := tk.Snapshot().Rows["bob"].State; st != "up" {
		t.Fatalf("bob answered every challenge and is %s", st)
	}
	if sent != 3 {
		t.Fatalf("%d challenges in three windows, want 3", sent)
	}
}

// lastChallenge is the nonce of the last challenge sent to row.
func lastChallenge(t *testing.T, b *fakeBus, row string) string {
	t.Helper()
	for i := len(b.sent) - 1; i >= 0; i-- {
		s := b.sent[i]
		if s.to != row {
			continue
		}
		if _, cmd, ok := strings.Cut(s.body, "nova-sprint pong --nonce "); ok {
			return strings.Fields(cmd)[0]
		}
	}
	t.Fatalf("no challenge sent to %s: %+v", row, b.sent)
	return ""
}
