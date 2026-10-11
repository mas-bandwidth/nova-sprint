package sprint

import (
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/pkg/hostload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func beatAt0() Beat { return NextBeat(Beat{}, p0, 12.3, hostload.HowCPU, hostload.State{}) }

// One missed beat window marks nothing (tla/DirtyTick.tla, Lapse needs
// MissedBeatsDown misses): the member stays up.
func TestOneMissedBeatKeepsAMemberUp(t *testing.T) {
	t.Parallel()
	b, ctl := beatAt0(), &Card{Fields: map[string]string{"status": Up}}
	at := p0.Add(BeatDeadline + time.Second)
	assert.Equal(t, 1, b.Missed(at))
	assert.Equal(t, Up, MemberStatus(ctl, b, at))
}

// MissedBeatsDown windows in a row is down, and not one second before.
func TestThreeMissedBeatsAreDown(t *testing.T) {
	t.Parallel()
	b, ctl := beatAt0(), &Card{Fields: map[string]string{"status": Up}}
	edge := p0.Add(MissedBeatsDown * BeatDeadline)
	assert.Equal(t, Up, MemberStatus(ctl, b, edge))
	assert.Equal(t, MissedBeatsDown-1, b.Missed(edge))
	assert.Equal(t, Down, MemberStatus(ctl, b, edge.Add(time.Second)))
	assert.Equal(t, MissedBeatsDown, b.Missed(edge.Add(time.Second)))
}

// A heavily loaded member enters loaded at the ordinary down boundary, measured
// from its last beat, and gets one more missed-beat interval before it is down
// (docs/SPEC-SPRINT.md section 5; tla/DirtyTick.tla, Lapse).
func TestAHeavilyLoadedMemberIsLoadedBeforeItGoesDown(t *testing.T) {
	t.Parallel()
	b := NextBeat(Beat{}, p0, 95, hostload.HowCPU, hostload.State{})
	ctl := &Card{Fields: map[string]string{"status": Up}}

	loaded := p0.Add(MissedBeatsDown*BeatDeadline + time.Second)
	assert.True(t, b.Alive(loaded), "a loaded bench stays available during its loaded interval")
	assert.True(t, b.Loaded(loaded))
	assert.Equal(t, Up, FleetRowStatus(ctl, b, loaded))
	assert.Equal(t, "95.0% loaded", LoadText(b, loaded))

	lastLoaded := p0.Add(2 * MissedBeatsDown * BeatDeadline)
	assert.True(t, b.Alive(lastLoaded), "the loaded interval ends at its declared boundary")
	assert.True(t, b.Loaded(lastLoaded))
	assert.False(t, b.Alive(lastLoaded.Add(time.Second)))
	assert.Equal(t, Down, FleetRowStatus(ctl, b, lastLoaded.Add(time.Second)))
	assert.Empty(t, LoadText(b, lastLoaded.Add(time.Second)))
	belowThreshold := NextBeat(Beat{}, p0, HeavyLoadPercent-0.1, hostload.HowCPU, hostload.State{})
	assert.False(t, belowThreshold.Alive(loaded), "load below saturation does not extend presence")
}

// A beat between misses resets the count: two misses, a beat, two more misses
// is a member that was never down.
func TestABeatBetweenMissesResetsTheCount(t *testing.T) {
	t.Parallel()
	b, ctl := beatAt0(), &Card{Fields: map[string]string{"status": Up}}
	again := p0.Add(2*BeatDeadline + time.Second)
	require.Equal(t, 2, b.Missed(again))
	b = NextBeat(b, again, 5, hostload.HowCPU, hostload.State{})
	assert.Equal(t, 0, b.Missed(again))
	later := again.Add(2*BeatDeadline + time.Second)
	assert.Equal(t, 2, b.Missed(later))
	assert.Equal(t, Up, MemberStatus(ctl, b, later))
}

// A member that never beat has missed none: it is down by never beating.
func TestAMemberThatNeverBeatMissedNone(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, Beat{}.Missed(p0.Add(time.Hour)))
	assert.False(t, Beat{}.Alive(p0))
}
