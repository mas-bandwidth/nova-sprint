package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCardCheckFindsAHeadFromAnOldBrief(t *testing.T) {
	t.Parallel()
	const head = "0123456789abcdef0123456789abcdef01234567"
	s := cardCheckFixture()
	primary := s.Work.Card("a1")
	primary.Fields["attempt"] = "2"
	primary.Fields[FieldBriefAttempt] = "2"
	primary.Fields[fieldHeadBriefAttempt] = "1"
	primary.Fields["head"] = head
	s.Work.Put(primary)
	s.Work.Put(&Card{ID: "a1.w1", Row: "s1", Col: Landed, Fields: map[string]string{
		"kind": "work", PrimaryField: "a1", "attempt": "1", "head": head,
	}})
	findings := CheckCards(s, cardCheckIn())
	require.Len(t, findings, 1, "a stale head is the only defect in the fixture")
	var stale []CardFinding
	for _, f := range findings {
		if f.Kind == "stale-head" {
			stale = append(stale, f)
		}
	}
	require.Len(t, stale, 1)
	assert.Equal(t, "CHECK stale-head a1 head "+head+" of attempt 1 was made under an earlier brief remedy=nova-sprint rework a1 --fix 'brief replaced'", stale[0].Line())

	primary = s.Work.Card("a1")
	primary.Fields[fieldBriefKeepHead] = "true"
	s.Work.Put(primary)
	for _, f := range CheckCards(s, cardCheckIn()) {
		assert.NotEqual(t, "stale-head", f.Kind)
	}

	primary.Fields[fieldBriefKeepHead] = ""
	primary.Fields["attempt"] = "0"
	primary.Fields[FieldBriefAttempt] = ""
	primary.Fields[fieldHeadBriefAttempt] = ""
	primary.Fields["head"] = ""
	s.Work.Put(primary)
	s.Work.Put(&Card{ID: "a1.w1", Row: "s1", Col: Landed, Fields: map[string]string{"kind": "work", PrimaryField: "a1", "attempt": "1"}})
	for _, f := range CheckCards(s, cardCheckIn()) {
		assert.NotEqual(t, "stale-head", f.Kind)
	}

	primary.Fields["attempt"] = "2"
	primary.Fields[FieldBriefAttempt] = "2"
	primary.Fields[fieldHeadBriefAttempt] = "1"
	s.Work.Put(primary)
	s.Work.Put(&Card{ID: "a1.w1", Row: "s1", Col: Landed, Fields: map[string]string{
		"kind": "work", PrimaryField: "a1", "attempt": "1", "head": head, "head_superseded": "true",
	}})
	for _, f := range CheckCards(s, cardCheckIn()) {
		assert.NotEqual(t, "stale-head", f.Kind, "a bounced head remains in attempt history but no longer holds the card")
	}

	primary.Fields["attempt"] = "3"
	primary.Fields[FieldBriefAttempt] = "2"
	primary.Fields[fieldHeadBriefAttempt] = "2"
	primary.Fields["head"] = head
	s.Work.Put(primary)
	s.Work.Put(&Card{ID: "a1.w3", Row: "s1", Col: Landed, Fields: map[string]string{"kind": "work", PrimaryField: "a1", "attempt": "3", "head": head}})
	for _, f := range CheckCards(s, cardCheckIn()) {
		assert.NotEqual(t, "stale-head", f.Kind, "a reworked attempt under the current brief clears the finding")
	}
}
