package dash

import (
	"fmt"
	"log"
	"math/big"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/edge/reserve"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/mas-bandwidth/nova-sprint/modules/usage"
)

type workRow struct {
	tierMicros map[string]*big.Int
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
type ResourceMetric struct {
	Total         int            `json:"total"`
	Held          int            `json:"held"`
	ByLabel       map[string]int `json:"by_label,omitempty"`
	PausedByLabel map[string]int `json:"paused_by_label,omitempty"`
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
	Load     string // the bench's last heartbeat CPU peak ("18.6%"), "" when it has none
	Tokens   int64  // every token its runs reported (package usage), kin and benches alike

	// v3 Resources additions (live from nova-reserve subscribe + nova-config)
	Tags   []string        `json:"tags,omitempty"`
	CPU    *ResourceMetric `json:"cpu,omitempty"`
	Memory *ResourceMetric `json:"memory,omitempty"`
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
	Stopped       bool                   `json:"stopped"` // the sprint is stopped (the ETA tile and machine line show it)
	Tables        map[string]any         `json:"tables"`
	Streams       []streamLine           `json:"streams"`
	StreamCosts   map[string]*streamCost `json:"stream_costs"`
	Coordinator   string                 `json:"coordinator"`        // the roster row whose role is coordinator ("" when none is hooked)
	Summary       string                 `json:"summary"`            // v1's headline: "landed/all pct -> ETA 1h12m" (set per read, with the rate)
	Eta           string                 `json:"eta"`                // the ETA alone, or "-"
	EtaWindow     float64                `json:"eta_window_minutes"` // minutes of landings the rate was measured over
	EtaBasis      string                 `json:"eta_basis"`          // "last hour" or "sprint average"
	EtaLandings   int                    `json:"eta_landings"`       // landings inside that window
	Left          int                    `json:"left"`               // cards not yet landed (waiting, ready, working, review, fix, merging)
	Cost          *costSummary           `json:"cost"`
	OK            bool                   `json:"ok"`
	Error         string                 `json:"error,omitempty"`
}

// mapSnapshot is the page's view of s. A row's kind and width are the
// roster's (round 22: the store keeps neither); a row the roster does not
// name is shown in the fleet at width 0. stats is the stats reset mark, nil
// for none: with it, Fleet and Friends done, ok and failed count only the
// consumers done after it.
func mapSnapshot(s store.Snapshot, ros roster.Roster, at time.Time, stats *store.StatsMark) envelope {
	return mapSnapshotWithReserve(s, ros, reserve.FleetSnapshot{}, at, stats)
}

func mapSnapshotWithReserve(s store.Snapshot, ros roster.Roster, res reserve.FleetSnapshot, at time.Time, stats *store.StatsMark) envelope {
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
	mapRows(s.Rows, ros, res, at, fleet, friends)
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
			countConsumer(c, s.Cards, stats, work, fleet, friends, costs)
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
		left += w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging
		landed += w.Landed
		failed += w.Fail
		all += w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging + w.Landed + w.Fail
	}
	return envelope{Coordinator: coordinatorOf(s.Rows), Left: left, Cost: summarizeCost(s), Done: all > 0 && landed+failed == all, Build: buildID, ServerVersion: Version, Landed: landed, All: all, At: at, Epoch: s.Epoch, Machine: machineLine(s), Stopped: s.Stopped, OK: true, Streams: streams, StreamCosts: costs, Tables: map[string]any{"work": work, "fleet": fleet, "resources": fleet, "friends": friends, "merge": merge, "readers": readers}}
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
		total := w.Waiting + w.Ready + w.Working + w.Review + w.Fix + w.Merging + w.Landed + w.Fail
		if total > 0 && w.Landed == total {
			streams[i].State = "landed"
		} else if streams[i].State != "held" && w.Ready+w.Working+w.Review+w.Fix+w.Merging > 0 {
			streams[i].State = "working"
		}
	}
}

// mapRows fills the Fleet and Friends tables. Every roster worker is a Fleet
// row and every roster kin a Friends row, at its roster width, down until the
// store has the row (the human, 2026-11-07: "we definitely need all that stuff
// back"). A store row nova-config does not name is in neither table (Glenn,
// 2026-11-15: removed machines must not show in the fleet; the run retires
// such a row, and this covers the ticks before it does): exactly the rows
// steps.Retirable judges retired, so a hooked coordinator nova-config does
// not name, which is never retired, stays on the board. With an empty
// roster (a failed read, or nova-config with nothing in it) nothing is
// judged absent: every store row shows, down, at width 0. A row nova-config
// names that is not a roster row (a machine that is not a bench, a disabled
// friend) shows as down. The tables are maps, so the order is by name.
func mapRows(rows map[string]store.Row, ros roster.Roster, res reserve.FleetSnapshot, at time.Time, fleet, friends map[string]*fleetRow) {
	byHost := res.ByHost()
	for n, w := range ros.Workers {
		var mh *reserve.MachineSnapshot
		if m, ok := byHost[n]; ok {
			mh = &m
		}
		tags := benchTags(w, mh)
		cpu, mem := buildResourceMetrics(w, mh, at)
		status := "down"
		if mh != nil && mh.Online {
			status = "up"
			if w.Held != "" {
				status = "held"
			}
		}
		fleet[n] = &fleetRow{Status: status, Tags: tags, CPU: cpu, Memory: mem}
	}
	for n, k := range ros.Kin {
		friends[n] = &fleetRow{Status: "down", Width: k.Width}
	}
	for n, r := range rows {
		if steps.Retirable(ros, r) {
			continue
		}
		status := r.State
		if r.State == "up" && r.Held {
			status = "held"
		}
		kind, width, _, found := ros.Row(n)
		if !found { // named but not a roster row, or the roster is empty: down, whatever the store says
			status = "down"
		}
		var mh *reserve.MachineSnapshot
		if m, ok := byHost[n]; ok {
			mh = &m
			if !mh.Online && kind != roster.KindKin {
				status = "down"
			}
		}
		var tags []string
		var worker roster.Worker
		if w, ok := ros.Workers[n]; ok {
			worker = w
		}
		if kind != roster.KindKin {
			tags = benchTags(worker, mh)
		}
		f := &fleetRow{Status: status, Width: width, Tags: tags}
		if kind != roster.KindKin { // load is only for fleet machines (the human, 2026-11-10)
			f.Load = loadCell(r)
			f.CPU, f.Memory = buildResourceMetrics(worker, mh, at)
		}
		tableOf(kind, fleet, friends)[n] = f
	}
}

func benchTags(w roster.Worker, mh *reserve.MachineSnapshot) []string {
	if mh != nil && len(mh.Tags) > 0 {
		return slices.Clone(mh.Tags)
	}
	if len(w.Tags) > 0 {
		return slices.Clone(w.Tags)
	}
	return nil
}

// buildResourceMetrics computes total, held, by-label, and paused-by-label CPU
// and memory metrics directly from live nova-reserve subscribe state (falling
// back to nova-config capacity when no live snapshot has arrived yet).
func buildResourceMetrics(w roster.Worker, mh *reserve.MachineSnapshot, at time.Time) (cpu, mem *ResourceMetric) {
	var liveTot map[string]int
	if mh != nil {
		liveTot = mh.Resources
	}
	cpuTotal := resourceTotal(liveTot, w.Resources, "cpu")
	memTotal := resourceTotal(liveTot, w.Resources, "memory")
	cpuBy, memBy := map[string]int{}, map[string]int{}
	cpuPaused, memPaused := map[string]int{}, map[string]int{}
	cpuHeld, memHeld := 0, 0

	if mh != nil {
		capTot := map[string]int{"cpu": cpuTotal, "memory": memTotal}
		for _, l := range mh.Leases {
			if l.Sublease {
				continue
			}
			amt := l.ResolvedNeeds(capTot)
			lbl := reserve.DisplayLabel(l.Holder, l.Label, l.Priority)
			if l.IsPaused() {
				if c := amt["cpu"]; c > 0 {
					cpuPaused[lbl] += c
				}
				if g := amt["memory"]; g > 0 {
					memPaused[lbl] += g
				}
				continue
			}
			if c := amt["cpu"]; c > 0 {
				cpuBy[lbl] += c
				cpuHeld += c
			}
			if g := amt["memory"]; g > 0 {
				memBy[lbl] += g
				memHeld += g
			}
		}
		for _, cl := range mh.Claims {
			if cl.Expired(at) {
				continue
			}
			amt := cl.ResolvedNeeds(capTot)
			if c := amt["cpu"]; c > 0 {
				cpuBy["claim"] += c
				cpuHeld += c
			}
			if g := amt["memory"]; g > 0 {
				memBy["claim"] += g
				memHeld += g
			}
		}
	}

	cpu = &ResourceMetric{Total: cpuTotal, Held: cpuHeld, ByLabel: cpuBy, PausedByLabel: cpuPaused}
	mem = &ResourceMetric{Total: memTotal, Held: memHeld, ByLabel: memBy, PausedByLabel: memPaused}
	return cpu, mem
}

func resourceTotal(tot, wRes map[string]int, key string) int {
	if tot != nil && tot[key] > 0 {
		return tot[key]
	}
	if wRes != nil {
		return wRes[key]
	}
	return 0
}

// loadCell is the row's last heartbeat CPU peak ("87.3%"), "" when down or
// never beaten.
func loadCell(r store.Row) string {
	if r.State != "up" || (r.CPUPeakDeci <= 0 && r.Tiers == "" && r.Version == "") {
		return ""
	}
	return fmt.Sprintf("%.1f%%", float64(r.CPUPeakDeci)/10)
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
		if _, said := unknownStates.LoadOrStore(c.ID+"\x00"+c.State, true); !said {
			log.Printf("ALARM dashboard: producer %q has unknown state %q; counted as waiting (said once)", c.ID, c.State)
		}
	}
}

// unknownStates is each producer and unknown state already said: the board
// maps every poll, and a card's ALARM is said once, not once a second.
var unknownStates sync.Map

func countConsumer(c store.Card, cards map[string]store.Card, stats *store.StatsMark, work map[string]*workRow, fleet, friends map[string]*fleetRow, costs map[string]*streamCost) {
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
			if sinceMark(c, stats) {
				f.Done++
			}
		}
	}
	if c.State == "done" && sinceMark(c, stats) {
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

// sinceMark is whether a done consumer counts in the stats: always with no
// mark, else only when it was done after it.
func sinceMark(c store.Card, stats *store.StatsMark) bool {
	if stats == nil {
		return true
	}
	_, after := stats.Done[c.ID]
	return after
}

func countCost(w *workRow, c store.Card, costs *streamCost) {
	if !c.Priced {
		// summarizeCost's count, per stream: a finished API run with no
		// price (a seat run is free; one not yet run has nothing to price).
		if (c.State == "done" || c.State == "fail") && usage.MustParse(c.Usage).Billing != usage.BillingSubscription {
			costs.unpriced++
		}
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
