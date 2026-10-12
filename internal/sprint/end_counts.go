package sprint

import "strings"

// EndCounts is the classified fleet ends of one member. Unknown is kept
// separate: a legacy log without a cause cannot establish responsibility.
type EndCounts struct {
	OK int `json:"ok"`
	Failed int `json:"failed_work"`
	NoResult int `json:"no_result"`
	Provider int `json:"provider"`
	Coordination int `json:"coordination"`
	Environment int `json:"environment"`
	Harness int `json:"harness"`
	Clear int `json:"clear"`
	Unknown int `json:"unknown"`
}

func (c EndCounts) OKPercent() float64 {
	denominator := c.OK + c.Failed + c.NoResult
	if denominator == 0 { return 0 }
	return 100 * float64(c.OK) / float64(denominator)
}

func (c *EndCounts) add(class EndClass) {
	switch class {
	case EndClassOK: c.OK++
	case EndClassFailedWork: c.Failed++
	case EndClassNoResult: c.NoResult++
	case EndClassProvider: c.Provider++
	case EndClassCoordination: c.Coordination++
	case EndClassEnvironment: c.Environment++
	case EndClassHarness: c.Harness++
	case EndClassClear: c.Clear++
	default: c.Unknown++
	}
}

// EndCountsFromLines reconstructs the fleet counters from the stored move
// events. It reads the event's explicit end_class, never its report or cause
// prose. A typed drop and a successful finish are also unambiguous in old
// logs; other legacy ends remain unknown.
func EndCountsFromLines(lines []Line) map[string]EndCounts {
	rows := map[string]EndCounts{}
	for _, l := range lines {
		AddEndLine(rows, l)
	}
	return rows
}

// AddEndLine folds one typed fleet end into the projection. It is shared by
// full recomputation and the incremental cursor read.
func AddEndLine(rows map[string]EndCounts, l Line) {
		if l.Kind != LineMove || l.Table != Fleet || strings.Contains(l.Card, ".r") { return }
		member, from, ok := strings.Cut(l.From, ":")
		if !ok || from != Working || (!l.Removed && l.To == l.From) { return }
		class := EndClass(l.Set["end_class"])
		if class == "" {
			switch {
			case l.Verb == "drop": class = EndClassCoordination
			case l.Verb == "clear": class = EndClassClear
			case l.Verb == "finish" && l.Set["ok"] == "yes": class = EndClassOK
			default: class = EndClassUnknown
			}
		}
		n := len(l.Cards)
		if n == 0 { n = 1 }
		counts := rows[member]
		for range n { counts.add(class) }
		rows[member] = counts
}
