package verbs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// upStore is a store with rowan hooked in as coordinator, or with no rows.
func upStore(hooked bool) *store.Fake {
	rows := map[string]store.Row{}
	if hooked {
		rows["rowan"] = store.Row{Name: "rowan", State: "up", Session: "s-rowan", Role: "coordinator", Gen: 1}
	}
	f, err := store.NewFakeFrom(store.Snapshot{Epoch: 1, Rows: rows, Streams: map[string]store.Stream{}, Cards: map[string]store.Card{}, Judgements: map[string]store.Judgement{}, Cursors: map[string]int64{}})
	if err != nil {
		panic(err)
	}
	return f
}

func upBus() *fakeBus  { return &fakeBus{present: map[string]bool{"rowan": true}} }
func upNow() time.Time { return t0 }

type setupLeaseFake struct{}

func (setupLeaseFake) Acquire(context.Context) (SetupLease, error) { return setupLeaseFake{}, nil }
func (setupLeaseFake) Monitor(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(ctx)
}
func (setupLeaseFake) Check(context.Context) error { return nil }
func (setupLeaseFake) Close() error                { return nil }

type setupFake struct {
	ready [9]bool
	trace []string
	stop  int
}

func (f *setupFake) Completion(context.Context) (string, error) {
	return "UP DONE run on studio; dashboard http://studio; 4 workers up; coordinator rowan", nil
}
func (f *setupFake) Verify(_ context.Context, n int, _ SetupInput) (SetupCheck, error) {
	f.trace = append(f.trace, fmt.Sprintf("verify%d", n))
	if n == f.stop && !f.ready[n] {
		return SetupCheck{Line: "ASK dashboard", Next: "nova-sprint up --dash tailnet"}, nil
	}
	return SetupCheck{Verified: f.ready[n], Action: !f.ready[n], Line: fmt.Sprintf("UP %d/8", n)}, nil
}
func (f *setupFake) Act(_ context.Context, n int, _ SetupInput) error {
	f.trace = append(f.trace, fmt.Sprintf("act%d", n))
	f.ready[n] = true
	return nil
}
func TestSetupVerifiesActionsAndRestarts(t *testing.T) {
	t.Parallel()
	f := &setupFake{stop: 5}
	e := Engine{Actor: "rowan", Store: upStore(true), Bus: upBus(), Now: upNow, Setup: f, Lock: setupLeaseFake{}}
	first := e.Execute(context.Background(), []string{"up"})
	if first.Exit != 1 || !strings.Contains(first.Output, "ASK dashboard\nSPRINT REFUSED up: step 5 is not verified: nova-sprint up --dash tailnet\n") {
		t.Fatal(first)
	}
	if strings.Join(f.trace, ",") != "verify1,act1,verify1,verify1,verify2,act2,verify2,verify3,act3,verify3,verify4,act4,verify4,verify5" {
		t.Fatal(f.trace)
	}
	f.trace = nil
	f.stop = 0
	f.ready[2] = false
	second := e.Execute(context.Background(), []string{"up", "--dash", "tailnet"})
	if second.Exit != 0 || !strings.HasPrefix(strings.Join(f.trace, ","), "verify1,verify1,verify2,act2,verify2") {
		t.Fatal(second, f.trace)
	}
	// up names what the walk does not set: here the commit identity (PR 641 F6)
	if !strings.Contains(second.Output, "UP 8/8\nUP NEEDS the sprint has no commit identity: no worker can hook and nothing can be committed: nova-sprint identity --name ") {
		t.Fatal(second.Output)
	}
	// UP DONE tells a new coordinator to read check's width lines before adding cards
	if !strings.Contains(second.Output, "coordinator rowan\nBEFORE CARDS: keep everybody at width: ") || !strings.Contains(second.Output, "then nova-sprint check and read its width, waits and NEEDS chain lines before start") {
		t.Fatal(second.Output)
	}
}
func TestSetupArguments(t *testing.T) {
	t.Parallel()
	e := Engine{Actor: "rowan"}
	got := e.Execute(context.Background(), []string{"up", "--dash", "invalid"})
	if got.Exit != 2 || got.Output != "SPRINT REFUSED up: --dash is tailnet or public=<machine>[:<port>]: nova-sprint help up\n" {
		t.Fatal(got)
	}
}

// TestSetupNamesTheBenchModel: a store Migrate upgraded keeps its policy,
// which may name no bench model for a tier a bench serves; up names it with
// the policy's fix, beside the identity (PR 641 F6).
func TestSetupNamesTheBenchModel(t *testing.T) {
	t.Parallel()
	f := upStore(true)
	applyTest(t, f, store.Op{ID: "policy-up", Epoch: 1, Actor: "rowan", Kind: "policy", At: t0,
		Writes: []store.Write{store.PutPolicy{Kind: "script", Name: "old", Body: "script"}, store.SetActivePolicy{Version: 1}, store.SetIdentity{Name: "Bot", Email: "1+bot@users.noreply.example.test"}}})
	e := Engine{Actor: "rowan", Store: f, Bus: upBus(), Now: upNow, Setup: &setupFake{}, Lock: setupLeaseFake{}, Answers: noBench{"frontier"}, Roster: newRoster()}
	r := e.Execute(context.Background(), []string{"up", "--dash", "tailnet"})
	if r.Exit != 0 || !strings.Contains(r.Output, "UP 8/8\nUP NEEDS policy v1 names no bench model for tier frontier, so no bench is dealt a card of it: give the policy's catalog() a \"bench\"") ||
		strings.Contains(r.Output, "commit identity") {
		t.Fatal(r.Output)
	}
}

// up never prints UP DONE with its UP NEEDS checks skipped: a roster it
// cannot read is named (checkPolicy's red), as is a store it cannot read
// after the walk, each pointing at its fix.
func TestSetupNeedsNamesWhatItCouldNotCheck(t *testing.T) {
	t.Parallel()
	f := upStore(true)
	applyTest(t, f, store.Op{ID: "policy-up", Epoch: 1, Actor: "rowan", Kind: "policy", At: t0,
		Writes: []store.Write{store.PutPolicy{Kind: "script", Name: "old", Body: "script"}, store.SetActivePolicy{Version: 1}, store.SetIdentity{Name: "Bot", Email: "1+bot@users.noreply.example.test"}}})
	ro := newRoster()
	ro.err = errors.New("nova-config down")
	e := Engine{Actor: "rowan", Store: f, Bus: upBus(), Now: upNow, Setup: &setupFake{}, Lock: setupLeaseFake{}, Answers: fakeAnswers{}, Roster: ro}
	r := e.Execute(context.Background(), []string{"up", "--dash", "tailnet"})
	if r.Exit != 0 || !strings.Contains(r.Output, "UP 8/8\nUP NEEDS nova-config's roster cannot be read (nova-config down): nova-config check, then nova-sprint check\n") {
		t.Fatal(r.Output)
	}
	gone := &snapshotGone{Fake: upStore(true)}
	e = Engine{Actor: "rowan", Store: gone, Bus: upBus(), Now: upNow, Setup: &goneAfterWalk{setupFake: &setupFake{}, gone: gone}, Lock: setupLeaseFake{}}
	r = e.Execute(context.Background(), []string{"up", "--dash", "tailnet"})
	if r.Exit != 0 || !strings.Contains(r.Output, "UP 8/8\nUP NEEDS identity and bench models unchecked: the store did not answer (connection reset): nova-sprint check\n") {
		t.Fatal(r.Output)
	}
}

// snapshotGone is a store whose snapshot fails once gone is set.
type snapshotGone struct {
	*store.Fake
	gone bool
}

func (s *snapshotGone) Snapshot(ctx context.Context) (store.Snapshot, error) {
	if s.gone {
		return store.Snapshot{}, errors.New("connection reset")
	}
	return s.Fake.Snapshot(ctx)
}

// goneAfterWalk is the setup walk that loses the store at its completion.
type goneAfterWalk struct {
	*setupFake
	gone *snapshotGone
}

func (g *goneAfterWalk) Completion(ctx context.Context) (string, error) {
	g.gone.gone = true
	return g.setupFake.Completion(ctx)
}
