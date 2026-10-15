package sprint

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The waiting-judgment warning (the owner, 2026-10-11: "Any card waiting for judgement
// should alert you via warning every second once it has been in this state for 10
// seconds or longer."): a judgment open 9 s is not warned about; at 10 s it is; still
// open at 11 s, again; answered (no longer in the inbox), none and forgotten. A judgment
// computed at read time (its oldest is always now) is warned about from when the look
// first saw it. Notes that are not judgments are never warned about.
func TestAJudgmentWaitingTenSecondsIsWarnedEverySecond(t *testing.T) {
	t.Parallel()
	opened := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	j := Group{ID: "n-1", Alias: "j7", Kind: Judgment, Oldest: opened, Members: []string{"s1-1", "s1-2"},
		Commands: []Command{{Decision: "accept", Lines: []string{"nova-sprint accept --group n-1 --expect 2"}}}}
	happened := Group{ID: "n-2", Kind: Happened, Oldest: opened.Add(-time.Hour)}
	inbox := []Group{happened, j}
	seen := map[string]time.Time{}

	assert.Empty(t, JudgmentWarnings(inbox, opened.Add(9*time.Second), seen), "9 s: no warning")
	at10 := JudgmentWarnings(inbox, opened.Add(10*time.Second), seen)
	require.Len(t, at10, 1, "10 s: warned")
	assert.Equal(t, JudgmentWarning{Alias: "j7", ID: "n-1", Cards: []string{"s1-1", "s1-2"}, Waited: 10 * time.Second,
		Answer: []string{"accept: nova-sprint accept --group n-1 --expect 2"}}, at10[0])
	at11 := JudgmentWarnings(inbox, opened.Add(11*time.Second), seen)
	require.Len(t, at11, 1, "11 s: warned again")
	assert.Equal(t, 11*time.Second, at11[0].Waited)
	assert.Empty(t, JudgmentWarnings([]Group{happened}, opened.Add(time.Hour), seen), "answered: none")
	assert.Empty(t, seen, "an answered judgment is forgotten")

	// read time: its oldest is the read's own now, every look
	stopped := func(at time.Time) []Group {
		return []Group{{ID: "machine:stopped", Kind: Judgment, Oldest: at}}
	}
	seen = map[string]time.Time{}
	assert.Empty(t, JudgmentWarnings(stopped(opened), opened, seen))
	assert.Empty(t, JudgmentWarnings(stopped(opened.Add(9*time.Second)), opened.Add(9*time.Second), seen))
	ws := JudgmentWarnings(stopped(opened.Add(10*time.Second)), opened.Add(10*time.Second), seen)
	require.Len(t, ws, 1, "a read-time judgment first seen 10 s ago is warned about")
	assert.Equal(t, 10*time.Second, ws[0].Waited)

	// one warning a look, every judgment in it; its one line, and its full text
	k := j
	k.ID, k.Alias, k.Commands, k.Oldest = "n-3", "j8", nil, opened.Add(-time.Minute)
	ws = JudgmentWarnings([]Group{j, k}, opened.Add(10*time.Second), map[string]time.Time{})
	require.Len(t, ws, 2)
	assert.Equal(t, "WARNING 2 judgment(s) waiting on the coordinator 10s or longer: j7 j8", JudgmentWarningSet(ws))
	text := JudgmentWarningText(ws)
	assert.True(t, strings.HasPrefix(text, JudgmentWarningSet(ws)+"; answer each"), text)
	assert.Contains(t, text, "j7 n-1 cards=s1-1,s1-2 waited=10s\n  accept: nova-sprint accept --group n-1 --expect 2\n")
	assert.Contains(t, text, "j8 n-3 cards=s1-1,s1-2 waited=1m10s\n  read it: nova-sprint inbox --open n-3\n")
	assert.Contains(t, text, "it repeats once a minute until they are answered")
}
