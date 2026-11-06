package dash

import (
	"github.com/mas-bandwidth/nova-sprint/store"
	"sort"
	"time"
)

type workRow struct {
	Blocked  int                `json:"blocked"`
	Waiting  int                `json:"waiting"`
	Ready    int                `json:"ready"`
	Working  int                `json:"working"`
	Review   int                `json:"review"`
	Fix      int                `json:"fix"`
	Merging  int                `json:"merging"`
	Landed   int                `json:"landed"`
	Fail     int                `json:"fail"`
	Cost     *float64           `json:"cost"`
	Unpriced bool               `json:"unpriced"`
	ByTier   map[string]float64 `json:"cost_by_tier"`
}
type fleetRow struct {
	Status   string   `json:"status"`
	Width    int      `json:"width"`
	Ready    int      `json:"ready"`
	Working  int      `json:"working"`
	Done     int      `json:"done"`
	Blocker  int      `json:"blocker_working"`
	Critical int      `json:"critical_working"`
	Fix      int      `json:"fix_working"`
	Reads    int      `json:"reads_working"`
	OK       int      `json:"ok"`
	Failed   int      `json:"failed"`
	OKPct    *float64 `json:"okpct"`
}
type streamLine struct{ Stream, State string }
type envelope struct {
	Build         string                        `json:"build"`
	ServerVersion string                        `json:"server_version"`
	Landed        int                           `json:"landed"`
	All           int                           `json:"all"`
	At            time.Time                     `json:"at"`
	Epoch         int                           `json:"epoch"`
	Tables        map[string]any                `json:"tables"`
	Streams       []streamLine                  `json:"streams"`
	StreamCosts   map[string]map[string]float64 `json:"stream_costs"`
	OK            bool                          `json:"ok"`
	Error         string                        `json:"error,omitempty"`
}

func (c *Cache) Data() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	d := c.view
	if c.err != nil {
		d.OK = false
		d.Error = "store read failed"
	}
	return d
}
func mapSnapshot(s store.Snapshot, at time.Time) envelope {
	work := map[string]*workRow{}
	fleet := map[string]*fleetRow{}
	friends := map[string]*fleetRow{}
	costs := map[string]map[string]float64{}
	streams := []streamLine{}
	readers := map[string]map[string]int{}
	merge := map[string]map[string]int{}
	for n, v := range s.Streams {
		work[n] = &workRow{ByTier: map[string]float64{}}
		costs[n] = map[string]float64{}
		merge[n] = map[string]int{}
		state := ""
		if v.Held {
			state = "held"
		}
		streams = append(streams, streamLine{n, state})
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Stream < streams[j].Stream })
	mapRows(s.Rows, fleet, friends)
	for _, c := range s.Cards {
		if c.Kind == "producer" {
			if w := work[c.Stream]; w != nil {
				countWork(w, c)
				if c.State == "merging" {
					merge[c.Stream]["queued"]++
				}
				if c.State == "landed" {
					merge[c.Stream]["merged"]++
				}
			}
		} else if c.Kind == "work" || c.Kind == "read" {
			countConsumer(c, s.Cards, work, fleet, friends, costs)
			if c.Kind == "read" {
				countRead(readers, c)
			}
		}
	}
	for _, f := range fleet {
		setPercent(f)
	}
	for _, f := range friends {
		setPercent(f)
	}
	landed, all := 0, 0
	for _, w := range work {
		landed += w.Landed
		all += w.Blocked + w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging + w.Landed + w.Fail
	}
	return envelope{Build: buildID, ServerVersion: "v2-dev", Landed: landed, All: all, At: at, Epoch: s.Epoch, OK: true, Streams: streams, StreamCosts: costs, Tables: map[string]any{"work": work, "fleet": fleet, "friends": friends, "merge": merge, "readers": readers}}
}
func mapRows(rows map[string]store.Row, fleet, friends map[string]*fleetRow) {
	for n, r := range rows {
		status := r.State
		if r.State == "up" && r.Held {
			status = "held"
		}
		f := &fleetRow{Status: status, Width: r.Width}
		if r.Kind == "kin" {
			friends[n] = f
		} else {
			fleet[n] = f
		}
	}
}
func countRead(readers map[string]map[string]int, c store.Card) {
	if readers[c.Holder] == nil {
		readers[c.Holder] = map[string]int{}
	}
	switch c.State {
	case "ready":
		readers[c.Holder]["asked"]++
	case "working":
		readers[c.Holder]["reading"]++
	case "done":
		if c.Result == "ok" {
			readers[c.Holder]["ok"]++
		} else if c.Result == "fix" {
			readers[c.Holder]["broken"]++
		}
	}
}
func countWork(w *workRow, c store.Card) {
	state := c.State
	if c.Priority != "blocker" && c.Priority != "critical" && ((state == "ready" && c.Attempt >= 1) || (state == "working" && c.Attempt >= 2)) {
		state = "fix"
	}
	switch state {
	case "blocked":
		w.Blocked++
	case "waiting":
		w.Waiting++
	case "ready":
		w.Ready++
	case "working":
		w.Working++
	case "review":
		w.Review++
	case "merging":
		w.Merging++
	case "landed":
		w.Landed++
	case "fail":
		w.Fail++
	case "fix":
		w.Fix++
	}
}
func countConsumer(c store.Card, cards map[string]store.Card, work map[string]*workRow, fleet, friends map[string]*fleetRow, costs map[string]map[string]float64) {
	if w := work[c.Stream]; w != nil {
		d := float64(c.Cost) / 1e6
		if c.Priced {
			if w.Cost == nil {
				z := 0.0
				w.Cost = &z
			}
			*w.Cost += d
			w.ByTier[c.Tier] += d
		} else {
			w.Unpriced = true
		}
		if c.Priced {
			costs[c.Stream]["total_cost"] += d
		}
		if c.Priced {
			if c.Kind == "read" {
				costs[c.Stream]["read_cost"] += d
			} else {
				costs[c.Stream]["work_cost"] += d
			}
		}
		if !c.Priced {
			costs[c.Stream]["unpriced_runs"]++
		}
	}
	f := fleet[c.Holder]
	if f == nil {
		f = friends[c.Holder]
	}
	if f != nil {
		switch c.State {
		case "ready":
			f.Ready++
		case "working":
			f.Working++
			countTrack(f, c, cards[c.Of])
		case "done":
			f.Done++
		}
	}
	if c.Kind == "read" && c.State == "done" {
		author := fleet[c.Author]
		if author == nil {
			author = friends[c.Author]
		}
		if author != nil {
			if c.Result == "ok" {
				author.OK++
			} else if c.Result == "fix" {
				author.Failed++
			}
		}
	}
}
func countTrack(f *fleetRow, c, p store.Card) {
	if p.Priority == "blocker" {
		f.Blocker++
	} else if p.Priority == "critical" {
		f.Critical++
	} else if c.Kind == "read" {
		f.Reads++
	} else if c.Attempt >= 2 {
		f.Fix++
	}
}
func setPercent(f *fleetRow) {
	if f.OK+f.Failed > 0 {
		p := float64(f.OK) * 100 / float64(f.OK+f.Failed)
		f.OKPct = &p
	}
}
