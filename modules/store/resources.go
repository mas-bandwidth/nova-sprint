package store

import (
	"maps"
	"slices"
)

// RowResources holds the coarse whole-number resources, active holds, pause
// state, and consumer lists of a bench row as reported by nova-reserve status
// (SEAM-CONTRACT §2.3). Stored as JSONB resources_json on sprint.rows.
type RowResources struct {
	Total           map[string]int            `json:"total,omitempty"`
	HeldSprint      map[string]int            `json:"held_sprint,omitempty"`
	HeldOther       map[string]int            `json:"held_other,omitempty"`
	HeldByLabel     map[string]map[string]int `json:"held_by_label,omitempty"`
	PausedByLabel   map[string]map[string]int `json:"paused_by_label,omitempty"`
	AnyPaused       bool                      `json:"any_paused,omitempty"`
	SprintConsumers []string                  `json:"sprint_consumers,omitempty"`
	PausedConsumers []string                  `json:"paused_consumers,omitempty"`
	PausedMillis    map[string]int64          `json:"paused_millis,omitempty"` // monotonic cumulative pause+wait ms per consumer ID
}

// Clone returns a deep copy of RowResources.
func (res RowResources) Clone() RowResources {
	out := RowResources{
		Total:           maps.Clone(res.Total),
		HeldSprint:      maps.Clone(res.HeldSprint),
		HeldOther:       maps.Clone(res.HeldOther),
		AnyPaused:       res.AnyPaused,
		SprintConsumers: slices.Clone(res.SprintConsumers),
		PausedConsumers: slices.Clone(res.PausedConsumers),
		PausedMillis:    maps.Clone(res.PausedMillis),
	}
	if res.HeldByLabel != nil {
		out.HeldByLabel = make(map[string]map[string]int, len(res.HeldByLabel))
		for k, v := range res.HeldByLabel {
			out.HeldByLabel[k] = maps.Clone(v)
		}
	}
	if res.PausedByLabel != nil {
		out.PausedByLabel = make(map[string]map[string]int, len(res.PausedByLabel))
		for k, v := range res.PausedByLabel {
			out.PausedByLabel[k] = maps.Clone(v)
		}
	}
	return out
}

// Equal reports whether two RowResources are equal, treating nil and empty maps/slices as equivalent.
func (res RowResources) Equal(other RowResources) bool {
	if res.AnyPaused != other.AnyPaused {
		return false
	}
	if !equalIntMaps(res.Total, other.Total) {
		return false
	}
	if !equalIntMaps(res.HeldSprint, other.HeldSprint) {
		return false
	}
	if !equalIntMaps(res.HeldOther, other.HeldOther) {
		return false
	}
	if !equalNestedMaps(res.HeldByLabel, other.HeldByLabel) {
		return false
	}
	if !equalNestedMaps(res.PausedByLabel, other.PausedByLabel) {
		return false
	}
	if !equalStringSlices(res.SprintConsumers, other.SprintConsumers) {
		return false
	}
	if !equalStringSlices(res.PausedConsumers, other.PausedConsumers) {
		return false
	}
	if !equalInt64Maps(res.PausedMillis, other.PausedMillis) {
		return false
	}
	return true
}

func equalIntMaps(a, b map[string]int) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	return maps.Equal(a, b)
}

func equalInt64Maps(a, b map[string]int64) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	return maps.Equal(a, b)
}

func equalNestedMaps(a, b map[string]map[string]int) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !equalIntMaps(va, vb) {
			return false
		}
	}
	return true
}

func equalStringSlices(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return slices.Equal(a, b)
}

// rowCopy returns a copy of r with its Resources deep-copied.
func rowCopy(r Row) Row {
	r.Resources = r.Resources.Clone()
	return r
}

// rowLoadEqual reports whether two rows have the same heartbeat/load columns.
func rowLoadEqual(a, b Row) bool {
	return a.Load1Centi == b.Load1Centi &&
		a.Cores == b.Cores &&
		a.SlotsUsed == b.SlotsUsed &&
		a.SlotsTotal == b.SlotsTotal &&
		a.CPUPeakDeci == b.CPUPeakDeci &&
		a.Tiers == b.Tiers &&
		a.Version == b.Version &&
		a.Resources.Equal(b.Resources)
}
