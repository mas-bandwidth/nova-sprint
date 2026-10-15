package sprint

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The first-time land rate goal (docs/SPEC-SPRINT.md): the share of counted primaries
// (landed or dropped) that landed on attempt 1 with no rework, no brief replacement, and
// no carry, tracked per stream and per route (attempt 1's route) over the sprint and over
// a rolling window of the last N counted primaries of each stream (default 20, at least
// 10 counted before any judgment), against a sprint-wide target (default 90%).
const (
	PropFirstTimeTarget  = "first_time_target"
	PropFirstTimeWindow  = "first_time_window"
	PropFirstTimeSince   = "first_time_since"
	PropFirstTimeSaid    = "first_time_said"
	PropFirstTimeSummary = "first_time_summary"

	FirstTimeTargetDefault = 90
	FirstTimeWindowDefault = 20
	FirstTimeWindowMin     = 10

	FirstTimeCol = "first time"
)

func init() {
	NotSettingProps = append(NotSettingProps,
		PropFirstTimeTarget,
		PropFirstTimeWindow,
		PropFirstTimeSince,
		PropFirstTimeSaid,
		PropFirstTimeSummary,
	)
}

// FirstTimeCount is a first-time land count and total counted primaries.
type FirstTimeCount struct {
	FirstTime int `json:"first_time"`
	Counted   int `json:"counted"`
}

// Pct returns the rounded integer percentage (0..100), or 0 when nothing is counted.
func (c FirstTimeCount) Pct() int {
	if c.Counted <= 0 {
		return 0
	}
	return (c.FirstTime*100 + c.Counted/2) / c.Counted
}

// UnderTarget reports whether at least one primary is counted and the rate is strictly
// below target (0..100).
func (c FirstTimeCount) UnderTarget(target int) bool {
	return c.Counted > 0 && c.FirstTime*100 < c.Counted*target
}

// String formats the rate and count as "<pct>% <first>/<counted>", or "-" when none counted.
func (c FirstTimeCount) String() string {
	if c.Counted <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d%% %d/%d", c.Pct(), c.FirstTime, c.Counted)
}

// FirstTimeStream holds a stream's overall and windowed first-time land rates.
type FirstTimeStream struct {
	Overall     FirstTimeCount `json:"overall"`
	Window      FirstTimeCount `json:"window"`
	TopFailure  string         `json:"top_failure,omitempty"`
	UnderTarget bool           `json:"under_target,omitempty"`
}

// FirstTimeSummary holds the sprint's first-time land rates across streams and routes.
type FirstTimeSummary struct {
	Overall     FirstTimeCount             `json:"overall"`
	Target      int                        `json:"target"`
	Window      int                        `json:"window"`
	Streams     map[string]FirstTimeStream `json:"streams,omitempty"`
	Routes      map[string]FirstTimeCount  `json:"routes,omitempty"`
	UnderTarget []string                   `json:"under_target,omitempty"`
}

// FirstTimeTargetOf parses PropFirstTimeTarget (1..100), falling back to 90.
func FirstTimeTargetOf(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(v, "%")))
	if err != nil || n < 1 || n > 100 {
		return FirstTimeTargetDefault
	}
	return n
}

// FirstTimeWindowOf parses PropFirstTimeWindow (>= 10), falling back to 20.
func FirstTimeWindowOf(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < FirstTimeWindowMin {
		return FirstTimeWindowDefault
	}
	return n
}

func firstTimeTarget(s *Snapshot) int {
	if s == nil || s.Work == nil {
		return FirstTimeTargetDefault
	}
	v, _ := s.Work.Prop(PropFirstTimeTarget)
	return FirstTimeTargetOf(v)
}

func firstTimeWindow(s *Snapshot) int {
	if s == nil || s.Work == nil {
		return FirstTimeWindowDefault
	}
	v, _ := s.Work.Prop(PropFirstTimeWindow)
	return FirstTimeWindowOf(v)
}

// FirstTimeCounted reports whether c is a primary that counts toward the first-time land
// rate: landed or dropped (an open card has not been tried to its end; a sentinel or a
// card whose spend was carried to a replacement twin does not count).
//
// tla/SprintRules.tla Part "firsttime", FtCountUnder and FtRecover
func FirstTimeCounted(c *Card) bool {
	if c == nil || IsSentinel(c) || strings.HasPrefix(c.ID, "ctl-") {
		return false
	}
	switch c.F("kind") {
	case "stream", "work", "read":
		return false
	}
	if c.F(FieldCostCarriedTo) != "" {
		return false
	}
	if c.Placed() && c.Col == Landed && c.F("outcome") != "dropped" {
		return true
	}
	if !c.Placed() && (c.F("outcome") == "dropped" || c.F("dropped_at") != "" || c.F("dropped_from") != "") {
		return true
	}
	return false
}

// FirstTimeLanded reports whether c landed first time: landed on attempt 1 with no rework,
// no brief replacement, and no carry.
//
// tla/SprintRules.tla Part "firsttime", FtCountUnder and FtRecover
func FirstTimeLanded(c *Card) bool {
	if !FirstTimeCounted(c) || !c.Placed() || c.Col != Landed || c.F("outcome") == "dropped" {
		return false
	}
	if c.Int("attempt") > 1 || c.Int(FieldAttempts) > 1 {
		return false
	}
	for att, r := range ranAttempts(c) {
		if att > 1 || !r.ran {
			return false
		}
	}
	if c.Int("reworks") > 0 || c.Int("broken_reads") > 0 ||
		c.F("fix") != "" || c.F("finding") != "" ||
		c.F(FieldFindingAttempt) != "" || c.F(FieldFindings) != "" {
		return false
	}
	if c.F(FieldBriefAttempt) != "" || c.F(FieldHeadBriefAttempt) != "" ||
		c.F(FieldBriefKeepHead) != "" || strings.HasPrefix(c.F("why"), "brief edited") {
		return false
	}
	if c.F(FieldCostCarried) != "" || c.F(FieldReplaces) != "" ||
		strings.HasPrefix(c.F("brief"), "CARRY:") || strings.Contains(c.F("brief"), "\nCARRY:") {
		return false
	}
	for _, con := range CardCostOf(c).Consumers {
		if (con.Kind == "work" || con.Kind == "") && (con.Attempt > 1 || con.Take > 0) {
			return false
		}
		if con.Kind == "read" && con.End == "broken" {
			return false
		}
	}
	return true
}

// AttemptOneRoute returns the route of attempt 1 of primary c, or "" when unknown.
func AttemptOneRoute(s *Snapshot, c *Card) string {
	if c == nil {
		return ""
	}
	for _, con := range CardCostOf(c).Consumers {
		if (con.Kind == "work" || con.Kind == "") && con.Attempt <= 1 && con.Route != "" {
			return con.Route
		}
	}
	if s != nil && s.Fleet != nil {
		if wc := s.Fleet.Card(WorkCardID(c.ID, 1)); wc != nil && wc.F(FieldRoute) != "" {
			return wc.F(FieldRoute)
		}
	}
	return c.F(FieldRoute)
}

func firstTimeEndStamp(c *Card) time.Time {
	for _, k := range []string{"landed", "dropped_at", "since"} {
		if raw := c.F(k); raw != "" {
			if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

func firstTimeCardNum(id string) int {
	if i := strings.LastIndexByte(id, '-'); i >= 0 {
		if n, err := strconv.Atoi(id[i+1:]); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func cardAttemptFailures(s *Snapshot, c *Card) []string {
	var out []string
	add := func(raw string) {
		cl := FailureClass(raw)
		if cl == "" {
			cl = strings.TrimSpace(raw)
		}
		if cl != "" {
			out = append(out, cl)
		}
	}
	if s != nil && s.Fleet != nil {
		maxAtt := max(1, c.Int("attempt"), c.Int(FieldAttempts))
		for att := 1; att <= maxAtt; att++ {
			wc := s.Fleet.Card(WorkCardID(c.ID, att))
			if wc == nil {
				continue
			}
			if b := BoundClass(wc); b != "" {
				out = append(out, b)
				continue
			}
			if (wc.Col == DoneFailed || wc.F("ok") == "no" || wc.Col == Withdrawn) && wc.F("report") != "" {
				add(wc.F("report"))
			}
		}
	}
	if len(out) == 0 {
		for _, r := range ranAttempts(c) {
			if !r.ran && r.fault != "" {
				add(r.fault)
			}
		}
	}
	if len(out) == 0 && c.F(FieldFailure) != "" {
		add(c.F(FieldFailure))
	}
	if len(out) == 0 && (c.Int("broken_reads") > 0 || c.F("finding") != "" || c.F(FieldFindings) != "") {
		f := cmp.Or(c.F("finding"), c.F(FieldFindings))
		if cl := FailureClass(f); cl != "" {
			out = append(out, cl)
		} else {
			out = append(out, "broken read")
		}
	}
	if len(out) == 0 && !c.Placed() {
		if cl := FailureClass(c.F("why")); cl != "" {
			out = append(out, cl)
		} else {
			out = append(out, "dropped")
		}
	}
	return out
}

func topFailureClass(s *Snapshot, cards []*Card) string {
	counts := map[string]int{}
	for _, c := range cards {
		if FirstTimeLanded(c) {
			continue
		}
		for _, cl := range cardAttemptFailures(s, c) {
			counts[cl]++
		}
	}
	best, bestN := "", 0
	for cl, n := range counts {
		if n > bestN || (n == bestN && (best == "" || cl < best)) {
			best, bestN = cl, n
		}
	}
	return best
}

// FirstTimeOf computes the sprint's first-time land rates per stream, per route (attempt 1),
// overall, and over each stream's window of the last N counted primaries.
//
// tla/SprintRules.tla Part "firsttime"
func FirstTimeOf(s *Snapshot) FirstTimeSummary {
	target := firstTimeTarget(s)
	win := firstTimeWindow(s)
	out := FirstTimeSummary{
		Target:  target,
		Window:  win,
		Streams: map[string]FirstTimeStream{},
		Routes:  map[string]FirstTimeCount{},
	}
	if s == nil || s.Work == nil {
		return out
	}
	byStream := map[string][]*Card{}
	for _, r := range s.Work.Rows() {
		byStream[r] = nil
	}
	for _, c := range s.Work.Cards() {
		if !FirstTimeCounted(c) {
			continue
		}
		st := cmp.Or(c.Row, c.F("stream"))
		if st == "" {
			continue
		}
		byStream[st] = append(byStream[st], c)
		ft := FirstTimeLanded(c)
		if r := AttemptOneRoute(s, c); r != "" {
			rc := out.Routes[r]
			rc.Counted++
			if ft {
				rc.FirstTime++
			}
			out.Routes[r] = rc
		}
	}
	streams := make([]string, 0, len(byStream))
	for st := range byStream {
		streams = append(streams, st)
	}
	slices.Sort(streams)
	for _, st := range streams {
		cards := byStream[st]
		slices.SortFunc(cards, func(a, b *Card) int {
			ta, tb := firstTimeEndStamp(a), firstTimeEndStamp(b)
			if !ta.Equal(tb) {
				if ta.Before(tb) {
					return -1
				}
				return 1
			}
			if na, nb := firstTimeCardNum(a.ID), firstTimeCardNum(b.ID); na != nb {
				return cmp.Compare(na, nb)
			}
			return cmp.Compare(a.ID, b.ID)
		})
		var overall FirstTimeCount
		for _, c := range cards {
			overall.Counted++
			if FirstTimeLanded(c) {
				overall.FirstTime++
			}
		}
		out.Overall.Counted += overall.Counted
		out.Overall.FirstTime += overall.FirstTime

		winCards := cards
		if len(winCards) > win {
			winCards = winCards[len(winCards)-win:]
		}
		var wcount FirstTimeCount
		for _, c := range winCards {
			wcount.Counted++
			if FirstTimeLanded(c) {
				wcount.FirstTime++
			}
		}
		under := wcount.UnderTarget(target)
		if under {
			out.UnderTarget = append(out.UnderTarget, st)
		}
		out.Streams[st] = FirstTimeStream{
			Overall:     overall,
			Window:      wcount,
			TopFailure:  topFailureClass(s, winCards),
			UnderTarget: under,
		}
	}
	return out
}

// FormatFirstTimeLine formats the summary line printed under the work table in `where`.
func FormatFirstTimeLine(ft FirstTimeSummary) string {
	line := fmt.Sprintf("first time: %s target %d%%", ft.Overall.String(), ft.Target)
	if len(ft.UnderTarget) > 0 {
		var parts []string
		for _, st := range ft.UnderTarget {
			if sc, ok := ft.Streams[st]; ok && sc.Window.Counted > 0 {
				parts = append(parts, fmt.Sprintf("%s %s", st, sc.Window.String()))
			} else {
				parts = append(parts, st)
			}
		}
		line += " (under target: " + strings.Join(parts, ", ") + ")"
	}
	return line
}

// EncodeFirstTimeSummary encodes a FirstTimeSummary as JSON.
func EncodeFirstTimeSummary(ft FirstTimeSummary) string {
	b, err := json.Marshal(ft)
	if err != nil {
		return ""
	}
	return string(b)
}

// DecodeFirstTimeSummary decodes a stored PropFirstTimeSummary JSON string.
func DecodeFirstTimeSummary(raw string) (FirstTimeSummary, bool) {
	if raw == "" {
		return FirstTimeSummary{}, false
	}
	var ft FirstTimeSummary
	if err := json.Unmarshal([]byte(raw), &ft); err != nil {
		return FirstTimeSummary{}, false
	}
	return ft, true
}
