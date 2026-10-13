package main

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// TestStreamsAndBasesAgreeOnEveryStream asserts that streams reads repos and bases
// from the same source as bases (v1.2.17): on an in-memory store, a stream added with
// briefs that carry REPO and BASE but a control card without the fields prints them
// in streams; and for every stream of a fixture with three streams, streams and bases
// name the same repos and bases. Unit tests use no real time, sockets, network or services.
func TestStreamsAndBasesAgreeOnEveryStream(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.ok("init --readers reader-a,reader-b --members m1")

	st := &store.Store{B: ta.m, Names: sprint.Names{}, Now: ta.a.now}
	ctx := context.Background()

	// Stream s1: added with briefs carrying REPO and BASE, but control card without the fields.
	// store.AddStep with empty AddReq.Repo and Base creates the stream control card without
	// FieldRepo or FieldBase.
	_, err := st.Run(ctx, store.AddStep(sprint.AddReq{
		Stream: "s1",
		IDs:    []string{"s1-1", "s1-2"},
		Brief:  "RESULT: c1 sha=0123456789ab tier: pro\nREPO: acme/widgets\nBASE: sprint/widgets-base\n\nTHE TASK. Fix the widget parser.",
		Who:    "coordinator",
	}))
	require.NoError(t, err)

	// Stream s2: added with gears brief
	_, err = st.Run(ctx, store.AddStep(sprint.AddReq{
		Stream: "s2",
		IDs:    []string{"s2-1"},
		Brief:  "RESULT: c2 sha=0123456789ab tier: pro\nREPO: acme/gears\nBASE: sprint/gears-base\n\nTHE TASK. Fix the gear cutter.",
		Who:    "coordinator",
	}))
	require.NoError(t, err)

	// Stream s3: mixed stream with two cards over different repos and bases
	_, err = st.Run(ctx, store.AddStep(sprint.AddReq{
		Stream: "s3",
		Cards: []sprint.CardAdd{
			{
				ID:    "s3-1",
				Brief: "RESULT: c3 sha=0123456789ab tier: pro\nREPO: acme/widgets\nBASE: sprint/widgets-base\n\nTHE TASK. Fix widget cutter.",
			},
			{
				ID:    "s3-2",
				Brief: "RESULT: c3 sha=0123456789ab tier: pro\nREPO: acme/gears\nBASE: sprint/gears-base\n\nTHE TASK. Fix gear cutter.",
			},
		},
		Who: "coordinator",
	}))
	require.NoError(t, err)

	// Verify control card of s1 has no FieldRepo or FieldBase stored
	s, err := st.Load(ctx, []string{sprint.Work, sprint.Merge}, nil)
	require.NoError(t, err)
	ctl1 := s.StreamCtl("s1")
	require.NotNil(t, ctl1)
	require.Empty(t, ctl1.F(sprint.FieldRepo), "control card must not have FieldRepo")
	require.Empty(t, ctl1.F(sprint.FieldBase), "control card must not have FieldBase")

	// streams text output must print the repos and bases read from briefs for s1
	streamsOut := ta.ok("streams")
	assert.Contains(t, streamsOut, "STREAM s1 repos=acme/widgets bases=sprint/widgets-base")

	// Read both views via JSON
	var sv streamsView
	ta.json("streams", &sv)
	require.Len(t, sv.Streams, 3)

	var bv struct {
		Bases []baseRow `json:"bases"`
		Cards int       `json:"cards"`
		Notes []string  `json:"notes"`
	}
	ta.json("bases", &bv)

	// Build map of stream -> (repos, bases) from basesInUse / bases view
	// Each baseRow records Base, Repo, and Cards by state for all open cards.
	basesStreamRepos := map[string][]string{}
	basesStreamBases := map[string][]string{}
	for _, br := range bv.Bases {
		for _, cardIDs := range br.Cards {
			for _, cid := range cardIDs {
				card := s.Work.Card(cid)
				require.NotNil(t, card, "card %s in bases must exist", cid)
				stream := card.Row
				if !slices.Contains(basesStreamRepos[stream], br.Repo) {
					basesStreamRepos[stream] = append(basesStreamRepos[stream], br.Repo)
				}
				if !slices.Contains(basesStreamBases[stream], br.Base) {
					basesStreamBases[stream] = append(basesStreamBases[stream], br.Base)
				}
			}
		}
	}

	for stream := range basesStreamRepos {
		slices.Sort(basesStreamRepos[stream])
		slices.Sort(basesStreamBases[stream])
	}

	// For every stream of the fixture, streams and bases must name the same repos and bases
	for _, sr := range sv.Streams {
		stream := sr.Stream
		assert.Equal(t, basesStreamRepos[stream], sr.Repos, "stream %s repos must agree between streams and bases", stream)
		assert.Equal(t, basesStreamBases[stream], sr.Bases, "stream %s bases must agree between streams and bases", stream)
	}
}
