package run

import (
	"maps"
	"slices"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// snapshotCopy isolates the planner from the engine's retained state.
func snapshotCopy(s store.Snapshot) store.Snapshot {
	s.Streams = maps.Clone(s.Streams)
	s.Cards = maps.Clone(s.Cards)
	for k, c := range s.Cards {
		c.Needs = slices.Clone(c.Needs)
		c.After = slices.Clone(c.After)
		c.Paths = slices.Clone(c.Paths)
		c.DoneWhen = slices.Clone(c.DoneWhen)
		s.Cards[k] = c
	}
	s.Rows = maps.Clone(s.Rows)
	s.Judgements = maps.Clone(s.Judgements)
	s.Cursors = maps.Clone(s.Cursors)
	return s
}
