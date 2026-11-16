package roster

import (
	"context"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unit tests: the Fake only. No real time, no sockets, no network, no
// outside services.

var all = []string{"flash", "pro", "heavy", "frontier"}

func fleet() Fake {
	return Fake{
		Ms: []Machine{
			{Name: "hulk", Roles: []string{"bench"}, OS: "linux", Arch: "amd64", Tags: []string{"metal"}, Resources: map[string]int{"cpu": 64, "memory": 256}},
			{Name: "mini", Roles: []string{"bench", "ci"}, OS: "darwin", Arch: "arm64", Resources: map[string]int{"cpu": 4, "memory": 16}},
			{Name: "eight", Roles: []string{"bench"}, OS: "linux", Arch: "amd64", Resources: map[string]int{"cpu": 8, "memory": 32}},
			{Name: "studio", Roles: []string{"keeper"}, OS: "darwin", Arch: "arm64", Resources: map[string]int{"cpu": 24, "memory": 64}},
		},
		Fs: []Friend{
			{Name: "stella", Harness: "codex", Model: "m1", Width: 16, Tiers: []string{"pro", "heavy"}, Pronouns: "she/her", Enabled: true},
			{Name: "alex", Harness: "claude", Model: "m2", Width: 2, Tiers: []string{"flash"}, Pronouns: "he/him", Enabled: true},
			{Name: "off", Harness: "x", Width: 4, Tiers: []string{"pro"}, Enabled: false},
		},
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	r, err := Load(context.Background(), fleet())
	require.NoError(t, err)
	assert.Equal(t, map[string]Worker{
		"hulk": {
			Name:      "hulk",
			Tiers:     all,
			Tags:      []string{"amd64", "linux", "metal"},
			Resources: map[string]int{"cpu": 64, "memory": 256},
			Harness:   "nova-worker",
		},
		"mini": {
			Name:      "mini",
			Tiers:     all,
			Tags:      []string{"arm64", "mac"},
			Resources: map[string]int{"cpu": 4, "memory": 16},
			Harness:   "nova-worker",
		},
		"eight": {
			Name:      "eight",
			Tiers:     all,
			Tags:      []string{"amd64", "linux"},
			Resources: map[string]int{"cpu": 8, "memory": 32},
			Harness:   "nova-worker",
		},
	}, r.Workers, "bench machines only; os/arch derived tags")
	assert.Equal(t, map[string]Kin{
		"stella": {Name: "stella", Model: "m1", Harness: "codex", Width: 16, Tiers: []string{"pro", "heavy"}},
		"alex":   {Name: "alex", Model: "m2", Harness: "claude", Width: 2, Tiers: []string{"flash"}},
	}, r.Kin, "enabled friends only, at their own width")
}

func TestWorkerFitsMax(t *testing.T) {
	t.Parallel()
	w := Worker{
		Name:      "b1",
		Tags:      []string{"amd64", "linux", "metal"},
		Resources: map[string]int{"cpu": 16, "memory": 64, "ps5": 1},
	}

	assert.True(t, w.FitsMax(card.Needs{
		Tags:    []string{"linux", "metal"},
		Amounts: map[string]int{"cpu": 4, "memory": 16, "ps5": 1},
	}))

	assert.False(t, w.FitsMax(card.Needs{
		Tags:    []string{"mac"},
		Amounts: map[string]int{"cpu": 1, "memory": 1},
	}), "missing tag")

	assert.False(t, w.FitsMax(card.Needs{
		Tags:    []string{"linux"},
		Amounts: map[string]int{"cpu": 32, "memory": 16},
	}), "cpu exceeds capacity")

	assert.False(t, w.FitsMax(card.Needs{
		Tags:    []string{"linux"},
		Amounts: map[string]int{"cpu": 4, "gpu": 1},
	}), "missing resource")
}

func TestUnmeasuredResourcesHeld(t *testing.T) {
	t.Parallel()
	src := Fake{
		Ms: []Machine{
			{Name: "no-cpu", Roles: []string{"bench"}, Resources: map[string]int{"memory": 16}},
			{Name: "no-mem", Roles: []string{"bench"}, Resources: map[string]int{"cpu": 8}},
			{Name: "zero-cpu", Roles: []string{"bench"}, Resources: map[string]int{"cpu": 0, "memory": 16}},
			{Name: "ok", Roles: []string{"bench"}, Resources: map[string]int{"cpu": 8, "memory": 16}},
		},
	}
	r, err := Load(context.Background(), src)
	require.NoError(t, err)

	assert.Equal(t, UnmeasuredResources, r.Workers["no-cpu"].Held)
	assert.Equal(t, UnmeasuredResources, r.Workers["no-mem"].Held)
	assert.Equal(t, UnmeasuredResources, r.Workers["zero-cpu"].Held)
	assert.Empty(t, r.Workers["ok"].Held)
}

func TestFleetWideR5Enforcement(t *testing.T) {
	t.Parallel()
	// Conflicting name "special": tag on m1, resource on m2.
	src := Fake{
		Ms: []Machine{
			{Name: "m1", Roles: []string{"bench"}, Tags: []string{"special"}, Resources: map[string]int{"cpu": 4, "memory": 16}},
			{Name: "m2", Roles: []string{"bench"}, Resources: map[string]int{"cpu": 8, "memory": 32, "special": 1}},
			{Name: "m3", Roles: []string{"bench"}, Resources: map[string]int{"cpu": 8, "memory": 32}},
		},
	}
	r, err := Load(context.Background(), src)
	require.NoError(t, err)

	assert.Len(t, r.Refused, 2, "both m1 and m2 refused due to R5 conflict")
	x1, ok1 := r.RefusedNamed("m1")
	assert.True(t, ok1)
	assert.Contains(t, x1.Reason, "R5")

	x2, ok2 := r.RefusedNamed("m2")
	assert.True(t, ok2)
	assert.Contains(t, x2.Reason, "R5")

	assert.Contains(t, r.Workers, "m3", "m3 unaffected")
}

func TestRow(t *testing.T) {
	t.Parallel()
	r, err := Load(context.Background(), fleet())
	require.NoError(t, err)
	kind, width, tiers, ok := r.Row("stella")
	assert.Equal(t, []any{KindKin, 16, []string{"pro", "heavy"}, true}, []any{kind, width, tiers, ok})
	tiers[0] = "changed"
	assert.Equal(t, []string{"pro", "heavy"}, r.Kin["stella"].Tiers, "Row returns a copy")
	for _, name := range []string{"studio", "off", "nobody", ""} {
		kind, width, tiers, ok := r.Row(name)
		assert.Equal(t, []any{"", 0, []string(nil), false}, []any{kind, width, tiers, ok}, name)
	}
}

func TestLoadRefuses(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	_, err := Load(context.Background(), Fake{Err: boom})
	assert.EqualError(t, err, "roster: machines: boom")
}

func TestLoadSkipsABadRow(t *testing.T) {
	t.Parallel()
	bench := []string{"bench"}
	res := map[string]int{"cpu": 4, "memory": 16}
	for name, c := range map[string]struct {
		src  Source
		want []Refused
	}{
		"twice": {Fake{Ms: []Machine{{Name: "m", Roles: bench, Resources: res}, {Name: "m", Roles: bench, Resources: res}}},
			[]Refused{{"m", KindWorker, "machine m is named twice"}}},
		"machine and friend": {Fake{Ms: []Machine{{Name: "f1", Roles: bench, Resources: res}}, Fs: []Friend{{Name: "f1", Enabled: true}}},
			[]Refused{{"f1", KindKin, "friend f1 is named twice"}}},
		"no name": {Fake{Fs: []Friend{{Enabled: true}}},
			[]Refused{{"", KindKin, "friend with no name"}}},
		"width": {Fake{Fs: []Friend{{Name: "f", Width: -1, Enabled: true}}},
			[]Refused{{"f", KindKin, "friend f has width -1"}}},
		"tier": {Fake{Fs: []Friend{{Name: "f", Tiers: []string{"big"}, Enabled: true}}},
			[]Refused{{"f", KindKin, `friend f has tier "big", not one of [flash pro heavy frontier]`}}},
	} {
		f := fleet()
		src := c.src.(Fake)
		f.Ms = append(f.Ms, src.Ms...)
		f.Fs = append(f.Fs, src.Fs...)
		r, err := Load(context.Background(), f)
		require.NoError(t, err, name)
		assert.Equal(t, c.want, r.Refused, name)
		for _, good := range []string{"hulk", "mini", "eight", "stella", "alex"} {
			_, _, _, ok := r.Row(good)
			assert.True(t, ok, "%s: %s still loads", name, good)
		}
	}
}

func TestCloneCopiesRefused(t *testing.T) {
	t.Parallel()
	r := Roster{Refused: []Refused{{"m", KindWorker, "why"}}}
	c := r.Clone()
	c.Refused[0].Reason = "changed"
	assert.Equal(t, "why", r.Refused[0].Reason)
	x, ok := r.RefusedNamed("m")
	assert.True(t, ok)
	assert.Equal(t, "why", x.Reason)
}

type friendsFail struct{ Fake }

func (friendsFail) Friends(context.Context) ([]Friend, error) { return nil, errors.New("down") }

func TestLoadFriendsError(t *testing.T) {
	t.Parallel()
	_, err := Load(context.Background(), friendsFail{})
	assert.EqualError(t, err, "roster: friends: down")
}

func TestAdvertisedTiers(t *testing.T) {
	t.Parallel()
	ro := Roster{
		Workers: map[string]Worker{
			"b1": {Name: "b1", Tiers: store.Tiers, Harness: WorkerHarness},
			"b2": {Name: "b2", Tiers: store.Tiers, Harness: WorkerHarness},
			"b3": {Name: "b3", Tiers: store.Tiers, Harness: WorkerHarness},
		},
		Kin: map[string]Kin{"k1": {Name: "k1", Width: 3, Tiers: []string{"frontier"}}},
	}
	got := ro.Advertised(map[string]store.Row{
		"b1": {Name: "b1", Session: "s", Tiers: "flash"},
		"b2": {Name: "b2", Session: "s"}, // advertised no tiers: keeps all
		"b3": {Name: "b3", Session: "s", Tiers: "flash,pro"},
		"k1": {Name: "k1", Session: "s", Tiers: "flash"},
	}, "")
	assert.Equal(t, []string{"flash"}, got.Workers["b1"].Tiers)
	assert.Equal(t, store.Tiers, got.Workers["b2"].Tiers)
	assert.Equal(t, []string{"flash", "pro"}, got.Workers["b3"].Tiers)
	assert.Equal(t, ro.Kin["k1"], got.Kin["k1"])
	assert.Equal(t, store.Tiers, ro.Workers["b1"].Tiers, "input roster unchanged")
}

func TestAdvertisedDropsAWorkerBehindTheRun(t *testing.T) {
	t.Parallel()
	ro := Roster{
		Workers: map[string]Worker{
			"b1": {Name: "b1", Tiers: store.Tiers, Harness: WorkerHarness},
			"b2": {Name: "b2", Tiers: store.Tiers, Harness: WorkerHarness},
			"b3": {Name: "b3", Tiers: store.Tiers, Harness: WorkerHarness},
			"b4": {Name: "b4", Tiers: store.Tiers, Harness: WorkerHarness},
		},
		Kin: map[string]Kin{"k1": {Name: "k1", Width: 3, Tiers: []string{"frontier"}}},
	}
	rows := map[string]store.Row{
		"b1": {Name: "b1", Session: "s", Tiers: "flash,pro", Version: "v2.0.5"},
		"b2": {Name: "b2", Session: "s", Tiers: "flash,pro", Version: "v2.0.6"},
		"b3": {Name: "b3", Session: "s", Tiers: "flash,pro"},
		"b4": {Name: "b4", Session: "s", Tiers: "flash,pro", Version: "v2.1.0"},
		"k1": {Name: "k1", Session: "s"},
	}
	got := ro.Advertised(rows, "v2.0.6")
	assert.Empty(t, got.Workers["b1"].Tiers, "below the run: serves no tier")
	assert.Equal(t, []string{"flash", "pro"}, got.Workers["b2"].Tiers, "at the run's release")
	assert.Empty(t, got.Workers["b3"].Tiers, "no version is below every release")
	assert.Equal(t, []string{"flash", "pro"}, got.Workers["b4"].Tiers, "above the run")
	assert.Equal(t, ro.Kin["k1"], got.Kin["k1"])
}

func TestBehind(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		have, want string
		behind     bool
	}{
		{"v2.0.5", "v2.0.6", true}, {"v2.0.6", "v2.0.6", false}, {"v2.1.0", "v2.0.6", false}, {"v1.9.9", "v2.0.6", true},
		{"", "v2.0.6", true}, {"dev+0123456789ab", "v2.0.6", true}, {"v2.0.5", "dev", false}, {"", "", false}, {"v2.0.5", "v2.0.0-test", false},
	} {
		assert.Equal(t, c.behind, Behind(c.have, c.want), "%q behind %q", c.have, c.want)
	}
}
