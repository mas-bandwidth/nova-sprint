package dash

import (
	"fmt"
	"log"
	"math/big"
	"sort"
	"time"

	"github.com/mas-bandwidth/nova-sprint/edge"
	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/mas-bandwidth/nova-sprint/usage"
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
	// BlockedCards is each blocked producer's reason, by id: a card the
	// coordinator holds reads "held: <why>" (card.HeldPrefix; defect 47), a
	// missing route "missing-route: <what>" (defect 48).
	BlockedCards map[string]string
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
	Reason   string // why the roster refused or held the row ("" otherwise)
	Load     string // the bench's last heartbeat as the board shows it ("18.6%"), "" when it has none
	Slots    string // its nova-reserve slots in use and in all ("3 / 8"), "" when unknown
	Tokens   int64  // every token its runs reported (package usage), kin and benches alike
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
	EtaBasis      string                 `json:"eta_basis"`          // "last hour" or "sprint average"
	EtaLandings   int                    `json:"eta_landings"`       // landings inside that window
	Left          int                    `json:"left"`               // cards not yet landed (blocked, waiting, ready, working, review, fix, merging)
	Cost          *costSummary           `json:"cost"`
	OK            bool                   `json:"ok"`
	Error         string                 `json:"error,omitempty"`
	RosterNotes   []string               `json:"roster_notes,omitempty"`  // the page's roster banner: the read's failure, each refused row
	Waits         []waitLine             `json:"waits,omitempty"`         // the Why waiting panel: per stream, why its producers wait
	WaitsSummary  string                 `json:"waits_summary,omitempty"` // over every stream: the chains, the overlaps, the command
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
	waits, summary := waitLines(s.Cards)
	return envelope{Waits: waits, WaitsSummary: summary, Coordinator: coordinatorOf(s.Rows), Left: left, Cost: summarizeCost(s), Done: all > 0 && landed+failed == all, Build: buildID, ServerVersion: Version, Landed: landed, All: all, At: at, Epoch: s.Epoch, Machine: machineLine(s), Stopped: s.Stopped, StopReason: s.StopReason, OK: true, Streams: streams, StreamCosts: costs, Tables: map[string]any{"work": work, "fleet": fleet, "friends": friends, "merge": merge, "readers": readers}, RosterNotes: rosterNotes(ros)}
}

// rosterNotes is one banner line per row the roster refused.
func rosterNotes(ros roster.Roster) []string {
	var notes []string
	for _, r := range ros.Refused {
		notes = append(notes, "nova-config row refused: "+edge.RedactN(r.Reason, edge.FailureMax))
	}
	return notes
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

// mapRows fills the Fleet and Friends tables. Every roster worker is a Fleet
// row and every roster kin a Friends row, at its roster width, down until the
// store has the row (the human, 2026-11-07: "we definitely need all that stuff
// back"); a row the roster refused is shown in its table as refused with the
// reason, by the kind it was read as; a store row the roster does not name is
// shown as down (Stella, v2-l6), by its hook role: the coordinator is a person
// among the friends, never a machine, so an empty roster (a failed read) does
// not put it in Fleet. The tables are maps, so the order is by name.
func mapRows(rows map[string]store.Row, ros roster.Roster, fleet, friends map[string]*fleetRow) {
	for n, w := range ros.Workers {
		fleet[n] = &fleetRow{Status: "down", Width: w.Width, Reason: w.Held}
	}
	for n, k := range ros.Kin {
		friends[n] = &fleetRow{Status: "down", Width: k.Width}
	}
	for _, x := range ros.Refused {
		if x.Name != "" {
			tableOf(x.Kind, fleet, friends)[x.Name] = &fleetRow{Status: "refused", Reason: x.Reason}
		}
	}
	for n, r := range rows {
		kind, width, _, found := ros.Row(n)
		if found {
			status := r.State
			if r.State == "up" && r.Held {
				status = "held"
			}
			f := &fleetRow{Status: status, Width: width}
			if held := ros.Workers[n].Held; held != "" {
				// A bench the roster holds (no cores: width 0) shows held
				// with the roster's reason.
				f.Reason = held
				if status == "up" {
					f.Status = "held"
				}
			}
			f.Load, f.Slots = loadCells(r)
			tableOf(kind, fleet, friends)[n] = f
			continue
		}
		if _, refused := ros.RefusedNamed(n); refused {
			continue
		}
		if r.Role == "coordinator" {
			kind = roster.KindKin
		}
		tableOf(kind, fleet, friends)[n] = &fleetRow{Status: "down"}
	}
}

// loadCells is the board's load cell and its slots title from the row's last
// heartbeat: the 1-minute load average over the cores, as a percent of all of
// them (v1's cell), and the nova-reserve slots in use of the total. A row that
// is down shows none, and so does one that has never beaten (no cores).
func loadCells(r store.Row) (load, slots string) {
	if r.State != "up" || r.Cores < 1 {
		return "", ""
	}
	load = fmt.Sprintf("%.1f%%", float64(r.Load1Centi)/float64(r.Cores))
	if r.SlotsTotal > 0 {
		slots = fmt.Sprintf("%d / %d", r.SlotsUsed, r.SlotsTotal)
	}
	return load, slots
}

// tableOf is the table a row of this roster kind belongs in: Friends for
// kin, Fleet for anything else.
func tableOf(kind string, fleet, friends map[string]*fleetRow) map[string]*fleetRow {
	if kind == roster.KindKin {
		return friends
	}
	return fleet
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
		if w.BlockedCards == nil {
			w.BlockedCards = map[string]string{}
		}
		w.BlockedCards[c.ID] = edge.RedactN(c.Reason, edge.FailureMax)
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
		f.Tokens += usage.MustParse(c.Usage).Tokens.Total()
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
	if c.State == "done" {
		countResult(c, fleet, friends)
	}
}

// countResult is the OK% count of a done consumer (directive 64a, the human:
// "reads count as the reader's done but never in ok%"). Only a work card
// counts, for its holder, the moment it is done: ok is OK; fail and fix are
// Failed; a fault (a harness, bench, route or device failure), a result that
// is none or not one of those (a close, a release) is not counted. A read
// adds to the reader's done and nothing else.
func countResult(c store.Card, fleet, friends map[string]*fleetRow) {
	if c.Kind != "work" {
		return
	}
	f := fleet[c.Holder]
	if f == nil {
		f = friends[c.Holder]
	}
	switch {
	case f == nil:
	case c.Result == "ok":
		f.OK++
	case c.Result == "fail" || c.Result == "fix":
		f.Failed++
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
