package verbs

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/card"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestMachineReportReturnsRealReceiptAndReplays(t *testing.T) {
	ctx := context.Background()
	f, at := breakEpoch(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Roster: fixtureRoster{}, Actor: "worker", Now: func() time.Time { return at }, Reports: CardReportPlanner{}, Heads: headFake{}}
	if r := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2"}); r.Exit != 0 {
		t.Fatal(r)
	}
	id := consumerOpID("report", 2, "p.w1", 1)
	f.Drop(id, true) // Accepted, but the transport returns an unknown outcome.
	head := strings.Repeat("b", 40)
	args := []string{"report", "p.w1@1", "--result", "ok", "--head", head, "--id", id, "--epoch", "2", "--cost-microusd", "123", "--priced", "true", "--json"}
	got := e.Execute(ctx, args)
	if got.Exit != 0 {
		t.Fatal(got)
	}
	var parsed store.Receipt
	if err := json.Unmarshal([]byte(got.Output), &parsed); err != nil {
		t.Fatal(err)
	}
	actual, found, err := f.Replay(ctx, id)
	if err != nil || !found || !actual.Accepted || !reflect.DeepEqual(parsed, actual) {
		t.Fatal(parsed, actual, err)
	}
	if again := e.Execute(ctx, args); again != got {
		t.Fatal(got, again)
	}
}
func TestMachineReportRequiresCanonicalIDAndUnits(t *testing.T) {
	ctx := context.Background()
	f, at := breakEpoch(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Actor: "worker", Now: func() time.Time { return at }}
	args := []string{"report", "p.w1@0", "--result", "ok", "--id", "wrong", "--epoch", "2", "--cost-microusd", "1", "--priced", "false", "--json"}
	before, _ := f.Snapshot(ctx)
	got := e.Execute(ctx, args)
	after, _ := f.Snapshot(ctx)
	if got.Exit != 1 || !strings.Contains(got.Output, "op id must be consumer:") || before.Seq != after.Seq {
		t.Fatal(got)
	}
	mixed := append(append([]string{}, args...), "--cost", "1.0")
	if _, ok := parseReport("report", mixed, "worker"); ok {
		t.Fatal("mixed units accepted")
	}
}

func TestMachineFinishUsesItsOwnCanonicalReceipt(t *testing.T) {
	ctx := context.Background()
	f, at := breakEpoch(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Roster: fixtureRoster{}, Actor: "worker", Now: func() time.Time { return at }, Reports: CardReportPlanner{}, Heads: headFake{}}
	if got := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2"}); got.Exit != 0 {
		t.Fatal(got)
	}
	id := consumerOpID("finish", 2, "p.w1", 1)
	args := []string{"finish", "p.w1@1", "--head", strings.Repeat("b", 40), "--epoch", "2", "--id", id, "--cost-microusd", "0", "--priced", "false", "--json"}
	got := e.Execute(ctx, args)
	if got.Exit != 0 {
		t.Fatal(got)
	}
	var receipt store.Receipt
	if err := json.Unmarshal([]byte(got.Output), &receipt); err != nil || !receipt.Accepted || receipt.Op != id {
		t.Fatal(receipt, err)
	}
	if again := e.Execute(ctx, args); again != got {
		t.Fatal(got, again)
	}
}
func TestMachineOutcomeRejectsMissingOrMixedFields(t *testing.T) {
	for _, args := range [][]string{
		{"p.w1@1", "--json", "--id", "id", "--epoch", "1", "--priced", "true"},
		{"p.w1@1", "--json", "--id", "id", "--epoch", "1", "--priced", "true", "--cost-microusd", "1", "--cost", "1"},
		{"p.w1@1", "--json", "--id", "id", "--epoch", "1", "--priced", "maybe", "--cost-microusd", "1"},
	} {
		if _, ok := parseReport("finish", args, "worker"); ok {
			t.Fatal(args)
		}
	}
}

func TestMachineReportReadFixPersistsFinding(t *testing.T) {
	ctx := context.Background()
	f, at := breakEpoch(t)
	e := Engine{Store: f, Env: fakeEnv{hooked: true}, Roles: SnapshotRoles{Store: f}, Roster: fixtureRoster{}, Actor: "worker", Now: func() time.Time { return at }, Reports: CardReportPlanner{}, Heads: headFake{}}
	if got := e.Execute(ctx, []string{"take", "p.w1@0", "--epoch", "2"}); got.Exit != 0 {
		t.Fatal(got)
	}
	if got := e.Execute(ctx, []string{"finish", "p.w1@1", "--head", strings.Repeat("b", 40), "--epoch", "2"}); got.Exit != 0 {
		t.Fatal(got)
	}
	applyTest(t, f, store.Op{ID: "reader-hook", Epoch: 2, Actor: "test", Kind: "hook", At: at, Writes: []store.Write{store.PutRow{Name: "reader", Session: "reader-session", Role: "worker"}}})
	snap, _ := f.Snapshot(ctx)
	seen, why := card.MoveRow(card.RowFromStore(snap.Rows["reader"], 2, []string{"pro"}), card.Seen{})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "reader-up", Epoch: 2, Actor: "test", Kind: "seen", At: at, Expect: seen.Expect, Writes: seen.Writes})
	snap, _ = f.Snapshot(ctx)
	p := snap.Cards["p"]
	rows := map[string]card.Row{"reader": card.RowFromStore(snap.Rows["reader"], 2, []string{"pro"})}
	ask, why := card.Move(card.FromStore(p), consumers(snap, p.ID), rows, card.StreamFromStore(snap.Streams[p.Stream]), nil, card.AskRead{Row: "reader"})
	if why != nil {
		t.Fatal(why)
	}
	applyTest(t, f, store.Op{ID: "read-deal", Epoch: 2, Actor: "test", Kind: "ask", At: at, Expect: ask.Expect, Writes: ask.Writes})
	e.Actor = "reader"
	if got := e.Execute(ctx, []string{"take", "p.r1.1@0", "--epoch", "2"}); got.Exit != 0 {
		t.Fatal(got)
	}
	id := consumerOpID("report", 2, "p.r1.1", 1)
	args := []string{"report", "p.r1.1@1", "--result", "fix", "--finding", "src/a.go:42 defect", "--epoch", "2", "--id", id, "--cost-microusd", "0", "--priced", "false", "--json"}
	got := e.Execute(ctx, args)
	if got.Exit != 0 {
		t.Fatal(got)
	}
	var receipt store.Receipt
	if err := json.Unmarshal([]byte(got.Output), &receipt); err != nil || !receipt.Accepted || receipt.Op != id {
		t.Fatal(receipt, err)
	}
	snap, _ = f.Snapshot(ctx)
	if snap.Cards["p.r1.1"].Reason != "src/a.go:42 defect" {
		t.Fatal(snap.Cards["p.r1.1"])
	}
}
