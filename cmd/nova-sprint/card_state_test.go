package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/ntable"
)

// stalePlacementBackend leaves the table placement intact while supplying a
// conflicting legacy hash field, as a reader can encounter in an old record.
type stalePlacementBackend struct{ store.Backend }

func (b stalePlacementBackend) ReadSet(ctx context.Context, table string, ids []string) (ntable.ReadSetResult, error) {
	res, err := b.Backend.ReadSet(ctx, table, ids)
	for i := range res.Members {
		if table == sprint.Work {
			res.Members[i].Fields["place:work"] = "s1:landed"
		}
	}
	return res, err
}

// SPEC-SPRINT section 11: card, needs and bulk readers name the same live
// table column; a waiting card's column is distinct from its merging need's.
func TestCardStateIsTheTableColumnInEveryReader(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.toMerging("s1")
	ta.ok("add --one --stream s2 waiter --needs s1-1")
	ta.a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) {
		return stalePlacementBackend{ta.m}, nil
	}

	type row struct {
		ID     string            `json:"id"`
		Column string            `json:"column"`
		State  string            `json:"state"`
		Fields map[string]string `json:"fields"`
	}
	for _, tc := range []struct {
		id, column string
	}{{"waiter", sprint.Waiting}, {"s1-1", sprint.Merging}} {
		t.Run(tc.id, func(t *testing.T) {
			var card struct {
				Column  string `json:"column"`
				Primary row    `json:"primary"`
			}
			ta.json("card "+tc.id, &card)
			assert.Equal(t, "s1:landed", card.Primary.Fields["place:work"], "the record disagrees with its table row")
			assert.Equal(t, tc.column, card.Column)
		})
	}

	var needs struct {
		Streams []struct {
			Cards []struct {
				row
				Needs []row `json:"needs"`
			} `json:"cards"`
		} `json:"streams"`
	}
	ta.json("needs --stream s2", &needs)
	require.Len(t, needs.Streams, 1)
	require.Len(t, needs.Streams[0].Cards, 1)
	waiting := needs.Streams[0].Cards[0]
	assert.Equal(t, "waiter", waiting.ID)
	assert.Equal(t, sprint.Waiting, waiting.Column)
	require.Len(t, waiting.Needs, 1)
	assert.Equal(t, "s1-1", waiting.Needs[0].ID)
	assert.Equal(t, sprint.Merging, waiting.Needs[0].Column)
	out := ta.ok("needs --stream s2")
	assert.Contains(t, out, "card waiter column waiting")
	assert.Contains(t, out, "need s1-1 column merging")

	var where struct {
		Rows []row `json:"rows"`
	}
	ta.json("where --rows", &where)
	require.Len(t, where.Rows, 4)
	for _, r := range where.Rows {
		want := sprint.Merging
		if r.ID == "waiter" {
			want = sprint.Waiting
		}
		assert.Equal(t, want, r.Column, r.ID)
		assert.Equal(t, r.Column, r.State, "state remains a deprecated alias for one release")
	}
	lines := strings.Split(strings.TrimSpace(ta.ok("card --all --json")), "\n")
	require.Len(t, lines, 4)
	for _, line := range lines {
		var r row
		require.NoError(t, json.Unmarshal([]byte(line), &r))
		want := sprint.Merging
		if r.ID == "waiter" {
			want = sprint.Waiting
		}
		assert.Equal(t, want, r.Column, r.ID)
	}
	ta.clean()
}

// SPEC-SPRINT section 11: a need's place is its column in both readers; card
// --json carries each need's column in the needs verb's words, so a card's
// own column is not confused with the state of its need.
func TestCardJSONNeedsCarryTheirColumn(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	ta.toMerging("s1")
	ta.ok("add --one --stream s2 waiter --needs s1-1")

	type need struct {
		ID     string `json:"id"`
		Column string `json:"column"`
		State  string `json:"state"`
	}
	type cardJSON struct {
		Needs []need `json:"needs"`
	}
	var card cardJSON
	ta.json("card waiter", &card)
	require.Len(t, card.Needs, 1)
	assert.Equal(t, "s1-1", card.Needs[0].ID)
	assert.Equal(t, sprint.Merging, card.Needs[0].Column, "a need's column is the needs verb's word")

	// a waiting card whose need is waiting: the need waits on base, the
	// waiter waits on the need; deferring need without car is refused by the
	// no-stranding rule, so the need remains waiting and both readers report it.
	dir := roadmapDir(t)
	ta.ok("add --one --stream s3 base")
	ta.ok("add --one --stream s3 need --needs base --brief-file " + writeBrief(t, "THE TASK. Do the need."))
	ta.ok("add --one --stream s4 car --needs need")
	code, _, errOut := ta.do("defer need --repo-dir " + dir + " --reason 'deferred to the roadmap'")
	require.NotZero(t, code)
	assert.Contains(t, errOut, "need is needed by car")

	var off cardJSON
	ta.json("card car", &off)
	require.Len(t, off.Needs, 1)
	assert.Equal(t, "need", off.Needs[0].ID)
	require.False(t, off.Needs[0].Column == "", "a need carries its column")

	var graph struct {
		Streams []struct {
			Cards []struct {
				ID    string `json:"id"`
				Needs []need `json:"needs"`
			} `json:"cards"`
		} `json:"streams"`
	}
	ta.json("needs --stream s4", &graph)
	require.Len(t, graph.Streams, 1)
	require.Len(t, graph.Streams[0].Cards, 1)
	require.Len(t, graph.Streams[0].Cards[0].Needs, 1)
	assert.Equal(t, graph.Streams[0].Cards[0].Needs[0].Column, off.Needs[0].Column,
		"card --json and needs --json print the same column for the same need")
	assert.Equal(t, sprint.Waiting, off.Needs[0].Column)
	ta.clean()
}
