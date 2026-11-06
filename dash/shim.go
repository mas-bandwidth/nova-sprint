package dash

import (
	"github.com/mas-bandwidth/nova-sprint/store"
	"math/big"
	"sort"
	"time"
)

type workRow struct {
	tierMicros map[string]*big.Int
	Blocked    int
	Waiting    int
	Ready      int
	Working    int
	Review     int
	Fix        int
	Merging    int
	Landed     int
	Fail       int
	Cost       *float64
	ByTier     map[string]float64
}
type fleetRow struct {
	Status   string
	Width    int
	Ready    int
	Working  int
	Done     int
	Blocker  int
	Critical int
	Fix      int
	Reads    int
	OK       int
	Failed   int
	OKPct    float64
}
type streamLine struct{ Stream, State string }

// Version is set at build time with -ldflags -X.
var Version = "dev"

type envelope struct {
	Done          bool                   `json:"done"`
	Build         string                 `json:"build"`
	ServerVersion string                 `json:"server_version"`
	Landed        int                    `json:"landed"`
	All           int                    `json:"all"`
	At            time.Time              `json:"at"`
	Epoch         int                    `json:"epoch"`
	Tables        map[string]any         `json:"tables"`
	Streams       []streamLine           `json:"streams"`
	StreamCosts   map[string]*streamCost `json:"stream_costs"`
	OK            bool                   `json:"ok"`
	Error         string                 `json:"error,omitempty"`
}

func mapSnapshot(s store.Snapshot, at time.Time) envelope {
	work := map[string]*workRow{}
	fleet := map[string]*fleetRow{}
	friends := map[string]*fleetRow{}
	costs := map[string]*streamCost{}
	streams := []streamLine{}
	readers := map[string]numberCells{}
	merge := map[string]numberCells{}
	for n, v := range s.Streams {
		work[n] = &workRow{ByTier: map[string]float64{}, tierMicros: map[string]*big.Int{}}
		costs[n] = &streamCost{amounts: map[string]*big.Int{}}
		merge[n] = numberCells{}
		state := "waiting"
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
	for _, w := range work {
		finishCosts(w)
	}
	setStreamStates(streams, work)
	for _, f := range fleet {
		setPercent(f)
	}
	for _, f := range friends {
		setPercent(f)
	}
	landed, failed, all := 0, 0, 0
	for _, w := range work {
		landed += w.Landed
		failed += w.Fail
		all += w.Blocked + w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging + w.Landed + w.Fail
	}
	return envelope{Done: all > 0 && landed+failed == all, Build: buildID, ServerVersion: Version, Landed: landed, All: all, At: at, Epoch: s.Epoch, OK: true, Streams: streams, StreamCosts: costs, Tables: map[string]any{"work": work, "fleet": fleet, "friends": friends, "merge": merge, "readers": readers}}
}
func setStreamStates(streams []streamLine, work map[string]*workRow) {
	for i := range streams {
		w := work[streams[i].Stream]
		total := w.Blocked + w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging + w.Landed + w.Fail
		if total > 0 && w.Landed == total {
			streams[i].State = "landed"
		} else if streams[i].State != "held" && w.Ready+w.Working+w.Review+w.Fix+w.Merging > 0 {
			streams[i].State = "working"
		}
	}
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
func countRead(readers map[string]numberCells, c store.Card) {
	if c.Holder == "" || (c.State != "ready" && c.State != "working" && !(c.State == "done" && (c.Result == "ok" || c.Result == "fix"))) {
		return
	}
	if readers[c.Holder] == nil {
		readers[c.Holder] = numberCells{}
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
func countConsumer(c store.Card, cards map[string]store.Card, work map[string]*workRow, fleet, friends map[string]*fleetRow, costs map[string]*streamCost) {
	if w := work[c.Stream]; w != nil {
		countCost(w, c, costs[c.Stream])
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
func countCost(w *workRow, c store.Card, costs *streamCost) {
	if !c.Priced {
		costs.unpriced++
		return
	}
	d := float64(c.Cost) / 1e6
	if w.Cost == nil {
		z := 0.0
		w.Cost = &z
	}
	*w.Cost += d
	if w.tierMicros[c.Tier] == nil {
		w.tierMicros[c.Tier] = new(big.Int)
	}
	w.tierMicros[c.Tier].Add(w.tierMicros[c.Tier], big.NewInt(c.Cost))
	costs.add("total_cost", c.Cost)
	if c.Kind == "read" {
		costs.add("read_cost", c.Cost)
	} else {
		costs.add("work_cost", c.Cost)
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
	p := 0.0
	if f.OK+f.Failed > 0 {
		p = float64(f.OK) * 100 / float64(f.OK+f.Failed)
	}
	f.OKPct = p
}
