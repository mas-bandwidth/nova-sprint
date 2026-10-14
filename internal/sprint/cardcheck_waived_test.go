package sprint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckCardsTakesWaivedNeedsAwayFromConsumers(t *testing.T) {
	t.Parallel()
	s := emptyFixture()
	addTestStream(s, "schema", "Schema")

	// Sentinel S with CONSUMERS: Q
	sBrief := "KIND: sentinel\nCONSUMERS: Q\nPATHS: docs/roadmap.sexp\nTEST: none reason\n"
	sentinel := &Card{ID: "S", Row: "schema", Col: Waiting, Score: 20.0, Fields: map[string]string{
		"kind":  "sentinel",
		"brief": sBrief,
	}}

	// Primary P with needs=S and waived=S (should NOT be counted as consumer)
	pBrief := "KIND: primary\nPATHS: p.go\nTEST: none reason\n"
	p := &Card{ID: "P", Row: "schema", Col: Waiting, Score: 10.0, Fields: map[string]string{
		"brief":  pBrief,
		"needs":  "S",
		"waived": "S",
	}}

	// Primary Q with needs=S (should be counted as consumer)
	qBrief := "KIND: primary\nPATHS: q.go\nTEST: none reason\n"
	q := &Card{ID: "Q", Row: "schema", Col: Waiting, Score: 10.0, Fields: map[string]string{
		"brief": qBrief,
		"needs": "S",
	}}

	s.Work.Put(sentinel)
	s.Work.Put(p)
	s.Work.Put(q)

	fs := CheckCards(s, CardCheckIn{})
	var scFindings []CardFinding
	for _, f := range fs {
		if f.Kind == CheckSentinelConsumers {
			scFindings = append(scFindings, f)
		}
	}

	// P has waived=S, so it should not be counted as a consumer of S
	// Q needs S and is not waived, so Q should be the only consumer
	// Since CONSUMERS: Q is declared and Q is the actual consumer, there should be no "missing" finding
	require.Empty(t, scFindings, "P has waived=S so is not counted as consumer; Q is the only consumer and is declared")

	// Case 2: Remove P's waived field - now P should be counted as a consumer
	// and since P is not declared in CONSUMERS, it should appear as missing
	p.Fields["waived"] = ""
	fs2 := CheckCards(s, CardCheckIn{})
	scFindings2 := []CardFinding{}
	for _, f := range fs2 {
		if f.Kind == CheckSentinelConsumers {
			scFindings2 = append(scFindings2, f)
		}
	}
	require.Len(t, scFindings2, 1, "P without waived should be counted as consumer and be missing from CONSUMERS")
	assert.Contains(t, scFindings2[0].Finding, "CONSUMERS line missing P")
}
