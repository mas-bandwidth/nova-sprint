package dash

import (
	"log"
	"math/big"
	"sort"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
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
	Cost       *big.Int
	ByTier     map[string]*big.Int
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
	Machine       string                 `json:"machine"`
	Stopped       bool                   `json:"stopped"`     // the page's stop banner
	StopReason    string                 `json:"stop_reason"` // the last stop's or start's reason
	Tables        map[string]any         `json:"tables"`
	Streams       []streamLine           `json:"streams"`
	StreamCosts   map[string]*streamCost `json:"stream_costs"`
	Coordinator   string                 `json:"coordinator"`        // the roster row whose role is coordinator ("" when none is hooked)
	Summary       string                 `json:"summary"`            // v1's headline: "landed/all pct -> ETA 1h12m" (set per read, with the rate)
	Eta           string                 `json:"eta"`                // the ETA alone, or "-"
	EtaWindow     float64                `json:"eta_window_minutes"` // minutes of landings the rate was measured over
	EtaLandings   int                    `json:"eta_landings"`       // landings inside that window
	Left          int                    `json:"left"`               // cards not yet landed (blocked, waiting, ready, working, review, fix, merging)
	Cost          *costSummary           `json:"cost"`
	OK            bool                   `json:"ok"`
	Error         string                 `json:"error,omitempty"`
}

// mapSnapshot is the page's view of s. A row's kind and width are the
// roster's (round 22: the store keeps neither); a row the roster does not
// name is shown in the fleet at width 0.
func mapSnapshot(s store.Snapshot, ros roster.Roster, at time.Time) envelope {
	work := map[string]*workRow{}
	fleet := map[string]*fleetRow{}
	friends := map[string]*fleetRow{}
	costs := map[string]*streamCost{}
	streams := []streamLine{}
	readers := map[string]numberCells{}
	merge := map[string]numberCells{}
	for n, v := range s.Streams {
		work[n] = &workRow{ByTier: map[string]*big.Int{}, tierMicros: map[string]*big.Int{}}
		costs[n] = &streamCost{amounts: map[string]*big.Int{}}
		merge[n] = numberCells{}
		state := "waiting"
		if v.Held {
			state = "held"
		}
		streams = append(streams, streamLine{n, state})
	}
	sort.Slice(streams, func(i, j int) bool { return streams[i].Stream < streams[j].Stream })
	mapRows(s.Rows, ros, fleet, friends)
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
	landed, failed, all, left := 0, 0, 0, 0
	for _, w := range work {
		left += w.Blocked + w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging
		landed += w.Landed
		failed += w.Fail
		all += w.Blocked + w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging + w.Landed + w.Fail
	}
	return envelope{Coordinator: coordinatorOf(s.Rows), Left: left, Cost: summarizeCost(s), Done: all > 0 && landed+failed == all, Build: buildID, ServerVersion: Version, Landed: landed, All: all, At: at, Epoch: s.Epoch, Machine: machineLine(s), Stopped: s.Stopped, StopReason: s.StopReason, OK: true, Streams: streams, StreamCosts: costs, Tables: map[string]any{"work": work, "fleet": fleet, "friends": friends, "merge": merge, "readers": readers}}
}

// coordinatorOf is the name of the row whose role is coordinator; the first
// by name when there are several, "" when none is hooked (v1 showed "-").
func coordinatorOf(rows map[string]store.Row) string {
	name := ""
	for n, r := range rows {
		if r.Role == "coordinator" && (name == "" || n < name) {
			name = n
		}
	}
	return name
}
func setStreamStates(streams []streamLine, work map[string]*workRow) {
	for i := range streams {
		w := work[streams[i].Stream]
		total := w.Blocked + w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging + w.Landed + w.Fail
		if total > 0 && w.Landed == total {
			streams[i].State = "landed"
		} else if total > 0 && w.Fail > 0 && w.Landed+w.Fail == total {
			streams[i].State = "fail"
		} else if streams[i].State != "held" && w.Ready+w.Working+w.Review+w.Fix+w.Merging > 0 {
			streams[i].State = "working"
		}
	}
}
func mapRows(rows map[string]store.Row, ros roster.Roster, fleet, friends map[string]*fleetRow) {
	for n, r := range rows {
		status := r.State
		if r.State == "up" && r.Held {
			status = "held"
		}
		kind, width, _, found := ros.Row(n)
		if !found { // not in nova-config: down, whatever the store says (Stella, v2-l6)
			status = "down"
		}
		f := &fleetRow{Status: status, Width: width}
		if kind == roster.KindKin {
			friends[n] = f
		} else {
			fleet[n] = f
		}
	}
	// LOCAL DOGFOOD PATCH (Glenn 2026-11-07 "we definitely need all that stuff back"):
	// every roster row is on the board, down until the store knows it hooked.
	for n, w := range ros.Workers {
		if _, ok := fleet[n]; !ok {
			fleet[n] = &fleetRow{Status: "down", Width: w.Width}
		}
	}
	for n, k := range ros.Kin {
		if _, ok := friends[n]; !ok {
			friends[n] = &fleetRow{Status: "down", Width: k.Width}
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
	default:
		w.Waiting++
		log.Printf("ALARM dashboard: producer %q has unknown state %q; counted as waiting", c.ID, c.State)
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
			// Glenn 2026-11-07: "OK% should be known as soon as a card is done": the
			// worker's own result counts the moment it reports, not after a read.
			// ok% is the worker's own finished work: ok against fail or fix; a
			// fault (harness, bench, route, device) and a close with no result do not
			// count, and reads are the reader's done, not their ok% (Glenn 2026-11-07).
			if c.Kind == "work" {
				switch c.Result {
				case "ok":
					f.OK++
				case "fail", "fix":
					f.Failed++
				}
			}
		}
	}
	if c.Kind == "read" && c.State == "done" {
		author := fleet[c.Author]
		if author == nil {
			author = friends[c.Author]
		}
		if author != nil && c.Result == "fix" { // a read that says fix counts against the author's work
			author.Failed++
		}
	}
}
func countCost(w *workRow, c store.Card, costs *streamCost) {
	if !c.Priced {
		costs.unpriced++
		return
	}
	if w.Cost == nil {
		w.Cost = new(big.Int)
	}
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

// machineLine is the page's machine line (v1's): RUNNING, or STOPPED and the
// reason (drift row 4). The human page shows only STOPPED (Glenn,
// 2026-10-04: "STOPPED is plenty"); /api/sprint carries the reason.
func machineLine(s store.Snapshot) string {
	if s.Stopped {
		return "machine: STOPPED " + s.StopReason
	}
	return "machine: RUNNING"
}
