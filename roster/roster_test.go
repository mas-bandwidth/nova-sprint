package roster

import (
	"context"
	"errors"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Unit tests: the Fake only. No real time, no sockets, no network, no
// outside services.

var all = []string{"flash", "pro", "heavy", "frontier"}

func fleet() Fake {
	return Fake{
		Ms: []Machine{
			{Name: "hulk", Roles: []string{"bench"}, Cores: 64, OS: "linux", Arch: "amd64"},
			{Name: "mini", Roles: []string{"bench", "ci"}, Cores: 4, OS: "darwin", Arch: "arm64"},
			{Name: "eight", Roles: []string{"bench"}, Cores: 8},
			{Name: "studio", Roles: []string{"keeper"}, Cores: 24},
		},
		Fs: []Friend{
			{Name: "grace", Harness: "codex", Model: "m1", Width: 16, Tiers: []string{"pro", "heavy"}, Pronouns: "she/her", Enabled: true},
			{Name: "alice", Harness: "claude", Model: "m2", Width: 2, Tiers: []string{"flash"}, Pronouns: "he/him", Enabled: true},
			{Name: "off", Harness: "x", Width: 4, Tiers: []string{"pro"}, Enabled: false},
		},
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	r, err := Load(context.Background(), fleet(), 8)
	require.NoError(t, err)
	assert.Equal(t, map[string]Worker{
		"hulk":  {Name: "hulk", Width: 8, Tiers: all, Harness: "nova-worker"},
		"mini":  {Name: "mini", Width: 4, Tiers: all, Harness: "nova-worker"},
		"eight": {Name: "eight", Width: 8, Tiers: all, Harness: "nova-worker"},
	}, r.Workers, "bench machines only; width min(cores, 8)")
	assert.Equal(t, map[string]Kin{
		"grace": {Name: "grace", Model: "m1", Harness: "codex", Width: 16, Tiers: []string{"pro", "heavy"}},
		"alice": {Name: "alice", Model: "m2", Harness: "claude", Width: 2, Tiers: []string{"flash"}},
	}, r.Kin, "enabled friends only, at their own width: no cap")
}

func TestHulkAt64CoresIsWidth8(t *testing.T) {
	t.Parallel()
	r, err := Load(context.Background(), Fake{Ms: []Machine{{Name: "hulk", Roles: []string{"bench"}, Cores: 64}}}, 8)
	require.NoError(t, err)
	kind, width, tiers, ok := r.Row("hulk")
	assert.Equal(t, []any{KindWorker, 8, all, true}, []any{kind, width, tiers, ok})
	r, err = Load(context.Background(), Fake{Ms: []Machine{{Name: "hulk", Roles: []string{"bench"}, Cores: 64}}}, 16)
	require.NoError(t, err)
	assert.Equal(t, 16, r.Workers["hulk"].Width, "policy may pass its own cap")
}

func TestRow(t *testing.T) {
	t.Parallel()
	r, err := Load(context.Background(), fleet(), 8)
	require.NoError(t, err)
	kind, width, tiers, ok := r.Row("grace")
	assert.Equal(t, []any{KindKin, 16, []string{"pro", "heavy"}, true}, []any{kind, width, tiers, ok})
	tiers[0] = "changed"
	assert.Equal(t, []string{"pro", "heavy"}, r.Kin["grace"].Tiers, "Row returns a copy")
	for _, name := range []string{"studio", "off", "nobody", ""} {
		kind, width, tiers, ok := r.Row(name)
		assert.Equal(t, []any{"", 0, []string(nil), false}, []any{kind, width, tiers, ok}, name)
	}
}

func TestLoadRefuses(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for want, c := range map[string]struct {
		src Source
		cap int
	}{
		"roster: width cap 0 is below 1": {Fake{}, 0},
		"roster: machines: boom":         {Fake{Err: boom}, 8},
	} {
		_, err := Load(context.Background(), c.src, c.cap)
		assert.EqualError(t, err, want)
	}
}

// TestLoadSkipsABadRow: a row that cannot be loaded is skipped and named,
// and the rest load (live: one bench with 0 cores emptied the whole board).
func TestLoadSkipsABadRow(t *testing.T) {
	t.Parallel()
	bench := []string{"bench"}
	for name, c := range map[string]struct {
		src  Source
		want []Refused
	}{
		"twice": {Fake{Ms: []Machine{{Name: "m", Roles: bench, Cores: 1}, {Name: "m", Roles: bench, Cores: 2}}},
			[]Refused{{"m", KindWorker, "machine m is named twice"}}},
		"machine and friend": {Fake{Ms: []Machine{{Name: "f1", Roles: bench, Cores: 1}}, Fs: []Friend{{Name: "f1", Enabled: true}}},
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
		r, err := Load(context.Background(), f, 8)
		require.NoError(t, err, name)
		assert.Equal(t, c.want, r.Refused, name)
		for _, good := range []string{"hulk", "mini", "eight", "grace", "alice"} {
			_, _, _, ok := r.Row(good)
			assert.True(t, ok, "%s: %s still loads", name, good)
		}
	}
}

// A bench with no cores in nova-config loads at width 0, held with the
// reason, never refused; the rest of the roster loads.
func TestLoadHoldsABenchWithNoCores(t *testing.T) {
	t.Parallel()
	f := fleet()
	f.Ms = append(f.Ms, Machine{Name: "m", Roles: []string{"bench"}})
	r, err := Load(context.Background(), f, 8)
	require.NoError(t, err)
	assert.Empty(t, r.Refused)
	assert.Equal(t, Worker{Name: "m", Width: 0, Tiers: store.Tiers, Harness: WorkerHarness, Held: NoCores}, r.Workers["m"])
	kind, width, _, ok := r.Row("m")
	assert.True(t, ok)
	assert.Equal(t, KindWorker, kind)
	assert.Equal(t, 0, width)
	_, _, _, ok = r.Row("hulk")
	assert.True(t, ok, "the rest still loads")
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
	_, err := Load(context.Background(), friendsFail{}, 8)
	assert.EqualError(t, err, "roster: friends: down")
}

// TestRoutesFromRoles: a bench serves the routes its route:<name> roles name
// (sorted, once each); kin and unknown names serve none; ServesRoutes is
// every route served (none needed is always served).
func TestRoutesFromRoles(t *testing.T) {
	t.Parallel()
	src := Fake{Ms: []Machine{{Name: "b1", Roles: []string{"bench", "route:usb", "route:ipad", "route:ipad", "route:"}, Cores: 2}, {Name: "b2", Roles: []string{"bench"}, Cores: 2}},
		Fs: []Friend{{Name: "k1", Harness: "h", Width: 1, Tiers: []string{"pro"}, Enabled: true}}}
	r, err := Load(context.Background(), src, 8)
	require.NoError(t, err)
	assert.Equal(t, []string{"ipad", "usb"}, r.Routes("b1"))
	assert.Empty(t, r.Routes("b2"))
	assert.True(t, r.ServesRoutes("b1", []string{"usb", "ipad"}))
	assert.False(t, r.ServesRoutes("b2", []string{"ipad"}))
	assert.False(t, r.ServesRoutes("k1", []string{"ipad"}))
	assert.True(t, r.ServesRoutes("k1", nil))
	c := r.Clone()
	c.Workers["b1"].Routes[0] = "x"
	assert.Equal(t, "ipad", r.Workers["b1"].Routes[0], "a clone keeps its own routes")
}

// A worker that advertised in its heartbeat is dealt its own
// tiers at width min(width, slots); one that advertised nothing, or read no
// slots, keeps nova-config's shape; kin are untouched; the input is not
// changed.
func TestAdvertisedWidthAndTiers(t *testing.T) {
	t.Parallel()
	ro := Roster{
		Workers: map[string]Worker{
			"b1": {Name: "b1", Width: 8, Tiers: store.Tiers, Harness: WorkerHarness},
			"b2": {Name: "b2", Width: 8, Tiers: store.Tiers, Harness: WorkerHarness},
			"b3": {Name: "b3", Width: 4, Tiers: store.Tiers, Harness: WorkerHarness},
			"b4": {Name: "b4", Width: 2, Tiers: store.Tiers, Harness: WorkerHarness},
		},
		Kin: map[string]Kin{"k1": {Name: "k1", Width: 3, Tiers: []string{"frontier"}}},
	}
	got := ro.Advertised(map[string]store.Row{
		"b1": {Name: "b1", Session: "s", Tiers: "flash", SlotsTotal: 3},
		"b2": {Name: "b2", Session: "s", SlotsTotal: 3},      // advertised no tiers: nova-config's shape
		"b3": {Name: "b3", Session: "s", Tiers: "flash,pro"}, // no slots read: width stays
		"b4": {Name: "b4", Session: "s", Tiers: "pro", SlotsTotal: 6},
		"k1": {Name: "k1", Session: "s", Tiers: "flash", SlotsTotal: 1},
	})
	assert.Equal(t, []string{"flash"}, got.Workers["b1"].Tiers)
	assert.Equal(t, 3, got.Workers["b1"].Width)
	assert.Equal(t, store.Tiers, got.Workers["b2"].Tiers)
	assert.Equal(t, 8, got.Workers["b2"].Width)
	assert.Equal(t, []string{"flash", "pro"}, got.Workers["b3"].Tiers)
	assert.Equal(t, 4, got.Workers["b3"].Width)
	assert.Equal(t, 2, got.Workers["b4"].Width, "slots above the cores-cap width never widen it")
	assert.Equal(t, ro.Kin["k1"], got.Kin["k1"])
	assert.Equal(t, 8, ro.Workers["b1"].Width, "the input roster is not changed")
	assert.Equal(t, store.Tiers, ro.Workers["b1"].Tiers)
}
