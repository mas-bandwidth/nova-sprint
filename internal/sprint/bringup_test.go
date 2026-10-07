package sprint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBringUpFellIsRunningToMissingOnce(t *testing.T) {
	t.Parallel()
	assert.Empty(t, BringUpFell(nil, map[string]string{"sprint-server": BringUpMissing}))
	assert.Empty(t, BringUpFell(
		map[string]string{"sprint-server": BringUpMissing},
		map[string]string{"sprint-server": BringUpMissing},
	))
	assert.Empty(t, BringUpFell(
		map[string]string{"sprint-server": BringUpRunning},
		map[string]string{"sprint-server": BringUpStale},
	))
	got := BringUpFell(
		map[string]string{"sprint-server": BringUpRunning, "bus": BringUpRunning, "store": BringUpRunning},
		map[string]string{"store": BringUpRunning, "bus": BringUpMissing},
	)
	assert.Equal(t, []string{"bus", "sprint-server"}, got)
}

func TestWatchEventsFoldsARepeatAndKeepsEachKind(t *testing.T) {
	t.Parallel()
	lines := []Line{
		{Verb: "ask"},
		{Verb: "ask"},
		{Verb: "read"},
		{Verb: "finish"},
		{Verb: "merge", To: "s1:merging"},
		{Verb: "land"},
		{Note: &Note{Kind: Judgment, Type: NConflict}},
		{Verb: "drain", Cause: "refused: the queue is held"},
		{Note: &Note{Kind: Judgment, Type: NWorkFailed}},
		{Note: &Note{Kind: Judgment, Type: NConflict}},
	}
	got := FormatWatchEvents(lines)
	for _, kind := range EventKinds {
		assert.Contains(t, got, "EVENT "+kind+" count=")
	}
	assert.Contains(t, got, "EVENT read-asked count=2")
	assert.Contains(t, got, "EVENT blocked-merge count=2")
	assert.Contains(t, got, "EVENT pushed-judgment count=1")
	assert.Equal(t, 8, len(FoldEvents(lines)))
	// a blocked merge is not also a pushed judgment
	assert.NotContains(t, got, "EVENT pushed-judgment count=3")
}

func TestReviewWarningNamesATierFewerThanTwoReadersCanRead(t *testing.T) {
	t.Parallel()
	s := &Snapshot{
		Work:    NewTable(Work),
		Readers: NewTable(Readers),
		ReaderStates: map[string]string{
			"reader-a": ReaderUp,
			"reader-b": ReaderAway,
		},
	}
	s.Work.SetRows([]string{"s1"})
	s.Readers.SetRows([]string{"reader-a", "reader-b"})
	s.Work.cards = map[string]*Card{
		"s1-1": {ID: "s1-1", Row: "s1", Col: Review, Fields: map[string]string{}},
	}
	warns := ReviewWarnings(s)
	require.Len(t, warns, 1)
	assert.Equal(t, "flash", warns[0].Tier)
	assert.Equal(t, 1, warns[0].Review)
	assert.Equal(t, 1, warns[0].Readers)
	assert.Contains(t, warns[0].Line(), "fewer than two readers can read it")

	s.ReaderStates["reader-b"] = ReaderUp
	assert.Empty(t, ReviewWarnings(s))
}

func TestReaderLinesCarryWidthAndState(t *testing.T) {
	t.Parallel()
	s := &Snapshot{Readers: NewTable(Readers), ReaderStates: map[string]string{"reader-a": ReaderUp}}
	s.Readers.SetRows([]string{"reader-a"})
	lines := ReaderLines(s)
	require.Len(t, lines, 1)
	assert.Equal(t, BringUpRunning, lines[0].State)
	assert.Contains(t, lines[0].Extra, "width=unbounded")
	assert.Contains(t, lines[0].Extra, "tiers=all")
	assert.True(t, NovaVerb(lines[0].Command))
	assert.Contains(t, lines[0].Line(), "BRING-UP reader reader-a running")
}

func TestBeatStateIsMissingThenRunningThenStale(t *testing.T) {
	t.Parallel()
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	assert.Equal(t, BringUpMissing, BeatState(Beat{}, now, BringUpFresh))
	assert.Equal(t, BringUpRunning, BeatState(Beat{At: now}, now, BringUpFresh))
	assert.Equal(t, BringUpStale, BeatState(Beat{At: now.Add(-time.Minute)}, now, BringUpFresh))
}

func TestABringUpCommandIsANovaVerb(t *testing.T) {
	t.Parallel()
	assert.True(t, NovaVerb("nova-sprint inbox --wait --push seat"))
	assert.True(t, NovaVerb("nova-bus peek --as coordinator"))
	assert.True(t, NovaVerb("nova-friend install --as amy"))
	assert.False(t, NovaVerb("launchctl kickstart system/redis"))
	assert.False(t, NovaVerb("nova-sprint run | tee out"))
	assert.False(t, NovaVerb("sh -c nova-sprint run"))
	assert.False(t, NovaVerb("redis-cli ping"))
}
