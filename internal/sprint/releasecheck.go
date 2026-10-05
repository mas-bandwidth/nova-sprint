package sprint

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The release gate's frame (docs/SPEC-SPRINT.md, the release check, card
// release-check-frame; docs/SPEC-RELEASE.md, "release check"): a registry of
// checks, each a pure function over a ReleaseFacts, so a release ships when
// the tool says so and never when someone feels it is done. A check reads the
// facts and nothing else (no socket, no clock of its own), says ok or fail
// with the evidence, and on a fail the evidence names what to look at. The
// verb `nova-sprint release check` runs the registry and writes nothing.

// ReleaseFacts is everything a check may read: the clock, the store's log,
// the sprint's dealt bound and the snapshot's tables. Later checks add git
// facts to this interface; the unit tests fake it with a struct, and the verb
// binds it to the store.
type ReleaseFacts interface {
	Now() time.Time
	Log() []Line
	DealtMax() time.Duration
	Snapshot() *Snapshot
}

// ReleaseResult is one check's answer.
type ReleaseResult struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Evidence string `json:"evidence"`
}

// Line is the check's one printed line: RELEASE CHECK <name> ok|fail <evidence>.
func (r ReleaseResult) Line() string {
	word := "fail"
	if r.OK {
		word = "ok"
	}
	return "RELEASE CHECK " + r.Name + " " + word + " " + r.Evidence
}

// ReleaseCheck is one entry of the registry: its name, the bar it holds
// (stated in docs/SPEC-RELEASE.md, "release check") and the pure function.
type ReleaseCheck struct {
	Name string
	Bar  string
	Run  func(ReleaseFacts) ReleaseResult
}

// ReleaseChecks is the registry, in the order the verb runs them. A later card
// of stream sprint-v1-release adds its check here and its bar to the spec.
var ReleaseChecks = []ReleaseCheck{
	{CheckNoStuckFriend, "no friend was stuck at any moment of the last " + StuckWindow.String(), NoStuckFriend},
	{CheckColdAudit, "a cold audit of 20 cards landed since the last release, sampled uniformly, under 48 hours old with every read ok", ColdAudit},
}

// ReleaseReport is the verb's result value: the lines are rendered from it and
// --json is the same value.
type ReleaseReport struct {
	Results []ReleaseResult `json:"results"`
	Checks  int             `json:"checks"`
	Failed  int             `json:"failed"`
	Ready   bool            `json:"ready"`
	Summary string          `json:"summary"`
}

// ExitCode is 0 when every check passed and 1 when one failed; a refusal of
// the arguments is the verb's exit 2.
func (r ReleaseReport) ExitCode() int {
	if r.Ready {
		return 0
	}
	return 1
}

// ReleaseCheckNames is the registry's names, in order.
func ReleaseCheckNames() []string {
	out := make([]string, len(ReleaseChecks))
	for i, c := range ReleaseChecks {
		out[i] = c.Name
	}
	return out
}

// RunReleaseChecks runs the named checks (every check when names is empty), in
// registry order, and summarises: RELEASE OK checks=<n> when all pass,
// RELEASE NOT READY failed=<n> otherwise. A name that is no check is an error
// naming the checks there are, and nothing runs.
func RunReleaseChecks(f ReleaseFacts, names []string) (ReleaseReport, error) {
	want := map[string]bool{}
	for _, n := range names {
		if !hasName(n) {
			return ReleaseReport{}, fmt.Errorf("no release check named %q; the checks are %s", n, strings.Join(ReleaseCheckNames(), ", "))
		}
		want[n] = true
	}
	rep := ReleaseReport{Results: []ReleaseResult{}}
	for _, c := range ReleaseChecks {
		if len(want) > 0 && !want[c.Name] {
			continue
		}
		r := c.Run(f)
		r.Name = c.Name
		if !r.OK {
			rep.Failed++
		}
		rep.Results = append(rep.Results, r)
	}
	rep.Checks = len(rep.Results)
	rep.Ready = rep.Failed == 0
	if rep.Ready {
		rep.Summary = fmt.Sprintf("RELEASE OK checks=%d", rep.Checks)
	} else {
		rep.Summary = fmt.Sprintf("RELEASE NOT READY failed=%d", rep.Failed)
	}
	return rep, nil
}

func hasName(n string) bool {
	for _, c := range ReleaseChecks {
		if c.Name == n {
			return true
		}
	}
	return false
}

// ReleaseStreamLines is the log lines of the streams the glob names (path.Match
// over the stream's name; "" keeps every line). A card's stream is the first
// stream any of its lines names, so a line that names none (a fleet move) is
// kept with its card's.
func ReleaseStreamLines(lines []Line, glob string) ([]Line, error) {
	if glob == "" {
		return lines, nil
	}
	if _, err := path.Match(glob, ""); err != nil {
		return nil, fmt.Errorf("--streams %q is not a glob: %v", glob, err)
	}
	streamOf := map[string]string{}
	for _, l := range lines {
		if l.Stream == "" {
			continue
		}
		for _, id := range l.Names() {
			if _, ok := streamOf[id]; !ok {
				streamOf[id] = l.Stream
			}
		}
	}
	var out []Line
	for _, l := range lines {
		s := l.Stream
		if s == "" {
			s = streamOf[l.Card]
		}
		if ok, _ := path.Match(glob, s); ok {
			out = append(out, l)
		}
	}
	return out, nil
}

// CheckNoStuckFriend is the name of the first check.
const CheckNoStuckFriend = "no-stuck-friend"

// StuckWindow is how far back no friend may have been stuck.
const StuckWindow = 4 * time.Hour

// NoStuckFriend: no friend was stuck at any moment of the last StuckWindow.
// Stuck is the deadline rule's own lateness (WorkDeadline, docs/SPEC-SPRINT.md
// section 1, a friend's card's deadline; the tick's N4 judgment): a friend held
// a working card past its deadline (FieldFriendDeadline, else DeadlineUnfinished,
// counted from the card's first take, a take-back counting), or was dealt a card
// and did not take it past the dealt bound (ReleaseFacts.DealtMax). A friend
// held down has the unstarted cards taken back (FriendTake), so a ready card
// on the friend's row is a card of a friend who is up. The spells are replayed from the
// log's fleet moves alone, so a spell that has ended still counts while it
// overlaps the window.
func NoStuckFriend(f ReleaseFacts) ReleaseResult {
	now := f.Now()
	from := now.Add(-StuckWindow)
	spans, cards := friendLateSpans(f.Log(), now, f.DealtMax())
	var hits []lateSpan
	for _, s := range spans {
		if s.to.After(from) && s.from.Before(now) {
			hits = append(hits, s)
		}
	}
	if len(hits) == 0 {
		return ReleaseResult{Name: CheckNoStuckFriend, OK: true,
			Evidence: fmt.Sprintf("no friend was stuck since %s (%d friend cards read from the log)", stamp(from), cards)}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].from.Before(hits[j].from) })
	var told []string
	for i, s := range hits {
		if i == 3 {
			break
		}
		begun := s.from
		if begun.Before(from) {
			begun = from
		}
		primary, _, _ := strings.Cut(s.card, ".")
		told = append(told, fmt.Sprintf("friend %s stuck from %s: %s %s (limit %s); look at: nova-sprint card %s, nova-sprint log --card %s",
			s.friend, stamp(begun), s.card, s.word, s.limit, primary, primary))
	}
	more := ""
	if len(hits) > len(told) {
		more = fmt.Sprintf("; and %d more spells", len(hits)-len(told))
	}
	return ReleaseResult{Name: CheckNoStuckFriend, Evidence: strings.Join(told, "; ") + more}
}

// lateSpan is one stretch of time a friend's card was past its deadline.
type lateSpan struct {
	friend, card, word string
	limit              time.Duration
	from, to           time.Time
}

// friendCard is one friend card's replay state.
type friendCard struct {
	friend string
	card   *Card
	since  time.Time
}

const (
	stampUntakenSince = "untaken_since"
	stampFirstTaken   = "first_taken"
	stampTaken        = "taken"
	stampFirstDealt   = "first_dealt"
	stampDealt        = "dealt"
)

// friendLateSpans replays the log's fleet moves onto the friends' rows and
// returns every stretch a card was past its deadline, and how many friend
// cards the log held.
func friendLateSpans(lines []Line, now time.Time, dealtMax time.Duration) (spans []lateSpan, cards int) {
	state := map[string]*friendCard{}
	seenCards := map[string]bool{}
	snap := &Snapshot{Work: NewTable(Work)}
	if dealtMax > 0 {
		snap.Work.SetProp(PropDealtMax, dealtMax.String())
	}
	checkSpan := func(id string, c *friendCard, end time.Time) {
		if c == nil || c.card == nil {
			return
		}
		field, limit, word, _ := WorkDeadline(snap, c.card)
		var base time.Time
		if s := c.card.F(field); s != "" {
			base, _ = time.Parse(time.RFC3339, s)
		}
		if base.IsZero() {
			base = c.since
		}
		dl := base.Add(limit)
		if end.After(dl) {
			from := dl
			if from.Before(c.since) {
				from = c.since
			}
			for i := len(spans) - 1; i >= 0; i-- {
				if spans[i].card == id {
					if spans[i].word == word && (spans[i].to.Equal(from) || spans[i].to.After(from)) {
						if end.After(spans[i].to) {
							spans[i].to = end
						}
						return
					}
					break
				}
			}
			spans = append(spans, lateSpan{
				friend: c.friend,
				card:   id,
				word:   word,
				limit:  limit,
				from:   from,
				to:     end,
			})
		}
	}
	var currentEpoch uint64
	var haveEpoch bool
	for _, l := range lines {
		if haveEpoch && l.Epoch > currentEpoch {
			for id, c := range state {
				checkSpan(id, c, l.At)
			}
			clear(state)
			currentEpoch = l.Epoch
		} else if !haveEpoch && l.Epoch > 0 {
			currentEpoch = l.Epoch
			haveEpoch = true
		}
		if l.Verb == "clear" || (l.Note != nil && l.Note.Type == NMachineStopped && strings.Contains(l.Note.What, "clear")) {
			for id, c := range state {
				checkSpan(id, c, l.At)
			}
			clear(state)
			continue
		}
		if l.Kind != LineMove || l.Table != Fleet {
			continue
		}
		ids := l.Cards
		if len(ids) == 0 {
			ids = []string{l.Card}
		}
		row, col, placed := strings.Cut(l.To, ":")
		friend, isFriend := FriendOfRow(row)
		for _, id := range ids {
			c := state[id]
			if c != nil && (l.Removed || !placed || !isFriend || c.friend != friend || (col != Ready && col != Working && col != Withdrawn)) {
				checkSpan(id, c, l.At)
				delete(state, id)
				c = nil
			}
			if l.Removed || !placed || !isFriend || (col != Ready && col != Working && col != Withdrawn) {
				continue
			}
			if c == nil {
				c = &friendCard{
					friend: friend,
					card:   &Card{ID: id, Row: row, Fields: map[string]string{}},
					since:  l.At,
				}
				state[id] = c
				if !seenCards[id] {
					seenCards[id] = true
					cards++
				}
			} else {
				checkSpan(id, c, l.At)
				c.since = l.At
			}
			c.friend = friend
			c.card.Row = row
			c.card.Col = col
			for k, v := range l.Set {
				c.card.Fields[k] = v
			}
			switch col {
			case Withdrawn:
				delete(c.card.Fields, stampFirstTaken)
				delete(c.card.Fields, stampTaken)
				delete(c.card.Fields, stampDealt)
				if c.card.F(stampUntakenSince) == "" {
					c.card.Fields[stampUntakenSince] = stamp(l.At)
				}
			case Working:
				delete(c.card.Fields, stampUntakenSince)
				if c.card.F(stampFirstTaken) == "" {
					c.card.Fields[stampFirstTaken] = stamp(l.At)
				}
				if c.card.F(stampTaken) == "" {
					c.card.Fields[stampTaken] = stamp(l.At)
				}
			case Ready:
				if c.card.F(stampFirstDealt) == "" {
					c.card.Fields[stampFirstDealt] = stamp(l.At)
				}
				if c.card.F(stampDealt) == "" {
					c.card.Fields[stampDealt] = stamp(l.At)
				}
				if c.card.F(stampUntakenSince) == "" && c.card.F(stampFirstTaken) == "" {
					c.card.Fields[stampUntakenSince] = stamp(l.At)
				}
			}
		}
	}
	for id, c := range state {
		checkSpan(id, c, now)
	}
	return spans, cards
}

// CheckColdAudit is the name of the cold audit check.
const CheckColdAudit = "cold-audit"

// ColdAuditMaxAge is how old a cold audit may be before it expires (48 hours).
const ColdAuditMaxAge = 48 * time.Hour

// ColdAuditRequiredCount is how many landed cards the audit samples.
const ColdAuditRequiredCount = 20

// PropColdAudit is the work table property that records the cold audit JSON.
const PropColdAudit = "cold_audit"

// ColdAuditRecord is the stored record of an audit.
type ColdAuditRecord struct {
	Seed  int64     `json:"seed"`
	IDs   []string  `json:"ids"`
	Asked time.Time `json:"asked"`
	Since string    `json:"since,omitempty"`
}

// ReadColdAudit reads the cold audit record from the snapshot, or nil if none.
func ReadColdAudit(s *Snapshot) (*ColdAuditRecord, error) {
	if s == nil || s.Work == nil {
		return nil, nil
	}
	v, ok := s.Work.Prop(PropColdAudit)
	if ok && v != "" {
		var rec ColdAuditRecord
		if err := json.Unmarshal([]byte(v), &rec); err == nil && len(rec.IDs) > 0 {
			return &rec, nil
		}
	}
	// Fall back to individual properties if set
	seedStr, hasSeed := s.Work.Prop("cold_audit_seed")
	idsStr, hasIDs := s.Work.Prop("cold_audit_ids")
	askedStr, hasAsked := s.Work.Prop("cold_audit_asked")
	if hasSeed && hasIDs && hasAsked {
		seed, _ := strconv.ParseInt(seedStr, 10, 64)
		asked, _ := time.Parse(time.RFC3339, askedStr)
		var ids []string
		for _, id := range strings.Split(idsStr, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			since, _ := s.Work.Prop("cold_audit_since")
			return &ColdAuditRecord{
				Seed:  seed,
				IDs:   ids,
				Asked: asked,
				Since: since,
			}, nil
		}
	}
	return nil, nil
}

// ColdAudit checks that a cold audit was run within the last 48 hours,
// sampled 20 cards landed since the last release, and every read is ok.
// A fail names any broken read with its finding, any still unanswered,
// and what to look at.
func ColdAudit(f ReleaseFacts) ReleaseResult {
	snap := f.Snapshot()
	if snap == nil || snap.Work == nil {
		return ReleaseResult{
			Name:     CheckColdAudit,
			OK:       false,
			Evidence: "coverage incomplete: no work table in snapshot; run: nova-sprint release check",
		}
	}
	rec, err := ReadColdAudit(snap)
	if err != nil {
		return ReleaseResult{
			Name:     CheckColdAudit,
			OK:       false,
			Evidence: fmt.Sprintf("corrupt cold audit in store: %v; run: nova-sprint release check --audit", err),
		}
	}
	if rec == nil || len(rec.IDs) == 0 {
		return ReleaseResult{
			Name:     CheckColdAudit,
			OK:       false,
			Evidence: "no cold audit has been asked; run: nova-sprint release check --audit",
		}
	}
	now := f.Now()
	age := now.Sub(rec.Asked)
	if age >= ColdAuditMaxAge || rec.Asked.After(now) {
		return ReleaseResult{
			Name:     CheckColdAudit,
			OK:       false,
			Evidence: fmt.Sprintf("cold audit expired: asked %s (%s ago, limit 48h); run: nova-sprint release check --audit", stamp(rec.Asked), age.Truncate(time.Minute)),
		}
	}
	if len(rec.IDs) < ColdAuditRequiredCount {
		return ReleaseResult{
			Name:     CheckColdAudit,
			OK:       false,
			Evidence: fmt.Sprintf("cold audit sampled %d cards, want %d; run: nova-sprint release check --audit", len(rec.IDs), ColdAuditRequiredCount),
		}
	}

	var broken []string
	var unanswered []string
	brokenFindings := map[string]string{}

	for _, id := range rec.IDs {
		var readCard *Card
		if snap.Readers != nil {
			for _, rc := range snap.Readers.Of(id) {
				if rc.F("audit") == "cold-audit" {
					readCard = rc
					break
				}
			}
			if readCard == nil {
				for _, rc := range snap.Readers.Of(id) {
					if rc.Col == OK || rc.Col == Broken || rc.Col == Asked || rc.Col == Reading {
						readCard = rc
					}
				}
			}
		}
		if readCard == nil {
			unanswered = append(unanswered, id)
			continue
		}
		switch readCard.Col {
		case OK:
			// read is ok
		case Broken:
			broken = append(broken, id)
			finding := readCard.F("finding")
			if finding == "" {
				finding = "no finding text"
			}
			brokenFindings[id] = fmt.Sprintf("%s (%s): %s", id, readCard.Row, finding)
		default:
			unanswered = append(unanswered, id)
		}
	}

	if len(broken) > 0 || len(unanswered) > 0 {
		var parts []string
		if len(broken) > 0 {
			var bf []string
			for _, id := range broken {
				bf = append(bf, brokenFindings[id])
			}
			parts = append(parts, fmt.Sprintf("%d broken read(s): %s", len(broken), strings.Join(bf, "; ")))
		}
		if len(unanswered) > 0 {
			parts = append(parts, fmt.Sprintf("%d read(s) still unanswered: %s", len(unanswered), strings.Join(unanswered, ", ")))
		}
		lookAt := "look at: nova-sprint read"
		if len(broken) > 0 {
			lookAt = fmt.Sprintf("look at: nova-sprint card %s, nova-sprint read", broken[0])
		} else if len(unanswered) > 0 {
			lookAt = "look at: nova-sprint queue"
		}
		return ReleaseResult{
			Name:     CheckColdAudit,
			OK:       false,
			Evidence: fmt.Sprintf("%s; %s", strings.Join(parts, "; "), lookAt),
		}
	}

	return ReleaseResult{
		Name:     CheckColdAudit,
		OK:       true,
		Evidence: fmt.Sprintf("all %d cold reads ok (audit seed %d, asked %s, %s ago)", len(rec.IDs), rec.Seed, stamp(rec.Asked), age.Truncate(time.Minute)),
	}
}

// ColdAuditReq is the parameters to ask cold audit reads.
type ColdAuditReq struct {
	Seed      int64
	SinceTag  string
	SinceTime time.Time
	Who       string
}

// PlanColdAudit plans the cold audit: samples 20 landed cards since the last
// release, selects readers who never saw each card, creates read cards on their
// rows in Asked, and records the audit in the Work table property.
func PlanColdAudit(s *Snapshot, req ColdAuditReq) (Plan, ColdAuditRecord, error) {
	if s.Work == nil {
		return Plan{}, ColdAuditRecord{}, errors.New("no work table in snapshot")
	}
	if s.Readers == nil {
		return Plan{}, ColdAuditRecord{}, errors.New("no readers table in snapshot")
	}

	var candidates []*Card
	for _, c := range s.Work.Column(Landed) {
		if IsSentinel(c) {
			continue
		}
		if !req.SinceTime.IsZero() {
			landedStr := c.F("landed")
			t, err := time.Parse(time.RFC3339, landedStr)
			if err != nil || !t.After(req.SinceTime) {
				continue
			}
		}
		candidates = append(candidates, c)
	}

	if len(candidates) == 0 {
		sinceMsg := ""
		if req.SinceTag != "" {
			sinceMsg = " since " + req.SinceTag
		}
		return Plan{}, ColdAuditRecord{}, fmt.Errorf("no cards landed%s", sinceMsg)
	}

	// Sort candidates by ID for determinism before shuffling
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })

	r := rand.New(rand.NewSource(req.Seed))
	perm := r.Perm(len(candidates))
	k := ColdAuditRequiredCount
	if k > len(candidates) {
		k = len(candidates)
	}
	sampled := make([]*Card, k)
	for i := 0; i < k; i++ {
		sampled[i] = candidates[perm[i]]
	}
	sort.Slice(sampled, func(i, j int) bool { return sampled[i].ID < sampled[j].ID })

	// Find up readers
	var upReaders []string
	for _, row := range s.Readers.Rows() {
		if s.ReaderIsUp(row) {
			upReaders = append(upReaders, row)
		}
	}
	if len(upReaders) == 0 {
		upReaders = append([]string{}, s.Readers.Rows()...)
	}
	if len(upReaders) == 0 {
		return Plan{}, ColdAuditRecord{}, errors.New("no readers up to ask cold audit")
	}

	// Track load assigned in this step to balance across readers
	assigned := map[string]int{}
	for _, rd := range upReaders {
		assigned[rd] = s.Readers.Count(rd, Asked) + s.Readers.Count(rd, Reading)
	}

	var plan Plan
	var ids []string
	for _, c := range sampled {
		ids = append(ids, c.ID)

		// A reader that never saw the card
		seen := map[string]bool{}
		for _, rc := range s.Readers.Of(c.ID) {
			seen[rc.Row] = true
		}
		if asked := c.F("asked"); asked != "" {
			for _, name := range strings.Split(asked, ",") {
				seen[strings.TrimSpace(name)] = true
			}
		}

		// Filter up readers who never saw this card
		var eligible []string
		for _, rd := range upReaders {
			if !seen[rd] {
				eligible = append(eligible, rd)
			}
		}
		if len(eligible) == 0 {
			eligible = upReaders
		}
		sort.SliceStable(eligible, func(i, j int) bool {
			return assigned[eligible[i]] < assigned[eligible[j]]
		})
		chosen := eligible[0]
		assigned[chosen]++

		attempt := c.Int("attempt")
		if attempt < 1 {
			attempt = 1
		}
		for s.Readers.Card(ReadCardID(c.ID, attempt, chosen)) != nil {
			attempt++
		}
		rcID := ReadCardID(c.ID, attempt, chosen)

		head := c.F("landed_head")
		if head == "" {
			head = c.F("head")
		}
		base := c.F("base")
		if base == "" {
			base = "main"
		}

		fields := map[string]string{
			"kind":    "read",
			"primary": c.ID,
			"stream":  c.Row,
			"reader":  chosen,
			"attempt": strconv.Itoa(attempt),
			"head":    head,
			"base":    base,
			"asked":   stamp(s.Now),
			"audit":   "cold-audit",
		}
		u := Unit{
			Key:    c.ID,
			Stream: c.Row,
			Changes: []Change{
				change(Readers, createEntry(rcID, chosen, Asked, c.Score, fields)),
			},
			Moved: fmt.Sprintf("%s cold audit read asked of %s", c.ID, chosen),
		}
		plan.Units = append(plan.Units, u)
	}

	rec := ColdAuditRecord{
		Seed:  req.Seed,
		IDs:   ids,
		Asked: s.Now,
		Since: req.SinceTag,
	}
	b, _ := json.Marshal(rec)
	propSet := func(name, val string) {
		was, had := s.Work.Prop(name)
		plan.Props = append(plan.Props, PropWrite{
			Table:     Work,
			Name:      name,
			Value:     val,
			Was:       was,
			WasAbsent: !had,
		})
	}
	propSet(PropColdAudit, string(b))
	propSet("cold_audit_seed", strconv.FormatInt(rec.Seed, 10))
	propSet("cold_audit_ids", strings.Join(rec.IDs, ","))
	propSet("cold_audit_asked", stamp(rec.Asked))

	return plan, rec, nil
}
