package roster

import (
	"context"
	"errors"
	"testing"

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
			{Name: "stella", Harness: "codex", Model: "m1", Width: 16, Tiers: []string{"pro", "heavy"}, Pronouns: "she/her", Enabled: true},
			{Name: "alex", Harness: "claude", Model: "m2", Width: 2, Tiers: []string{"flash"}, Pronouns: "he/him", Enabled: true},
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
		"stella": {Name: "stella", Model: "m1", Harness: "codex", Width: 16, Tiers: []string{"pro", "heavy"}},
		"alex":   {Name: "alex", Model: "m2", Harness: "claude", Width: 2, Tiers: []string{"flash"}},
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
	bench := []string{"bench"}
	boom := errors.New("boom")
	for want, c := range map[string]struct {
		src Source
		cap int
	}{
		"roster: width cap 0 is below 1":                                         {Fake{}, 0},
		"roster: machines: boom":                                                 {Fake{Err: boom}, 8},
		"roster: machine m has 0 cores":                                          {Fake{Ms: []Machine{{Name: "m", Roles: bench}}}, 8},
		"roster: m is named twice":                                               {Fake{Ms: []Machine{{Name: "m", Roles: bench, Cores: 1}, {Name: "m", Roles: bench, Cores: 2}}}, 8},
		"roster: stella is named twice":                                          {Fake{Ms: []Machine{{Name: "stella", Roles: bench, Cores: 1}}, Fs: []Friend{{Name: "stella", Enabled: true}}}, 8},
		"roster: a machine or friend with no name":                               {Fake{Fs: []Friend{{Enabled: true}}}, 8},
		"roster: friend f has width -1":                                          {Fake{Fs: []Friend{{Name: "f", Width: -1, Enabled: true}}}, 8},
		`roster: friend f has tier "big", not one of [flash pro heavy frontier]`: {Fake{Fs: []Friend{{Name: "f", Tiers: []string{"big"}, Enabled: true}}}, 8},
	} {
		_, err := Load(context.Background(), c.src, c.cap)
		assert.EqualError(t, err, want)
	}
}

type friendsFail struct{ Fake }

func (friendsFail) Friends(context.Context) ([]Friend, error) { return nil, errors.New("down") }

func TestLoadFriendsError(t *testing.T) {
	t.Parallel()
	_, err := Load(context.Background(), friendsFail{}, 8)
	assert.EqualError(t, err, "roster: friends: down")
}
