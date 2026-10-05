package sprint

import (
	"encoding/json"
	"fmt"
	"maps"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// The release check (docs/SPEC-SPRINT.md, release-check-cold-audit-r-ns.w1): a
// registry of checks, each a pure function over ReleaseFacts, so a release
// ships when the tool says so and never when someone feels it is done. A check
// reads the facts and nothing else (no socket, no clock of its own), says ok or
// fail with the evidence, and on a fail the evidence names what to look at.

// ReleaseFacts is everything a check may read: the clock and the snapshot's
// tables. The verb binds it to the store; the unit tests fake it.
type ReleaseFacts interface {
	Now() time.Time
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

// ReleaseCheck is one entry of the registry: its name, the bar it holds and
// the pure function.
type ReleaseCheck struct {
	Name string
	Bar  string
	Run  func(ReleaseFacts) ReleaseResult
}

// ReleaseChecks is the registry, in the order the verb runs them.
var ReleaseChecks = []ReleaseCheck{
	{CheckColdAudit, fmt.Sprintf("a cold audit of %d cards landed since the last release, sampled uniformly, under %s old, every read ok", ColdAuditCards, ColdAuditMaxAge), ColdAudit},
}

// ReleaseReport is the verb's result: the lines are rendered from it, and
// --json is the same value.
type ReleaseReport struct {
	Results []ReleaseResult `json:"results"`
	Checks  int             `json:"checks"`
	Failed  int             `json:"failed"`
	Ready   bool            `json:"ready"`
	Summary string          `json:"summary"`
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
// registry order, and summarises: RELEASE OK checks=<n> when all pass, RELEASE
// NOT READY failed=<n> otherwise. A name that is no check is an error naming
// the checks there are, and nothing runs.
func RunReleaseChecks(f ReleaseFacts, names []string) (ReleaseReport, error) {
	want := map[string]bool{}
	for _, n := range names {
		if !contains(ReleaseCheckNames(), n) {
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

// CheckColdAudit is the cold audit's name.
const CheckColdAudit = "cold-audit"

// ColdAuditCards is how many landed cards an audit samples, and ColdAuditMaxAge
// how old it may be when a plain check reads it.
const (
	ColdAuditCards  = 20
	ColdAuditMaxAge = 48 * time.Hour
)

// PropColdAudit is the work table's property the last audit is recorded in
// (ColdAuditRecord, as JSON).
const PropColdAudit = "cold_audit"

// FieldColdAudit is an audit read card's mark: the asked stamp of the audit it
// was asked for, so a read of an earlier audit or of the card's own review is
// never counted for this one.
const FieldColdAudit = "cold_audit"

// ColdAuditRecord is an audit as recorded: the seed and the window it sampled
// (so the sample can be redrawn), when it was asked, and each sampled card with
// the read card it was asked on.
type ColdAuditRecord struct {
	Seed  int64           `json:"seed"`
	Since string          `json:"since"`
	Asked time.Time       `json:"asked"`
	Reads []ColdAuditRead `json:"reads"`
}

// ColdAuditRead is one sampled card and its audit read card.
type ColdAuditRead struct {
	Card string `json:"card"`
	Read string `json:"read"`
}

// IDs is the sampled cards, in the record's order.
func (r ColdAuditRecord) IDs() []string {
	out := make([]string, len(r.Reads))
	for i, rd := range r.Reads {
		out[i] = rd.Card
	}
	return out
}

// ReadColdAudit is the audit recorded on the work table; false when none is,
// and an error when the record does not parse.
func ReadColdAudit(s *Snapshot) (ColdAuditRecord, bool, error) {
	var rec ColdAuditRecord
	if s == nil || s.Work == nil {
		return rec, false, nil
	}
	v, ok := s.Work.Prop(PropColdAudit)
	if !ok || v == "" {
		return rec, false, nil
	}
	if err := json.Unmarshal([]byte(v), &rec); err != nil {
		return rec, false, fmt.Errorf("the work table's %s does not parse: %v", PropColdAudit, err)
	}
	return rec, true, nil
}

// ColdAuditExtras is the read cards a plain check must read as records when
// they are not placed (a read retired since it was asked): the recorded
// audit's read cards.
func ColdAuditExtras(s *Snapshot) map[string][]string {
	rec, ok, err := ReadColdAudit(s)
	if !ok || err != nil {
		return nil
	}
	var ids []string
	for _, rd := range rec.Reads {
		ids = append(ids, rd.Read)
	}
	return map[string][]string{Readers: ids}
}

// ColdAudit is ok only when the last audit was asked under ColdAuditMaxAge ago,
// sampled ColdAuditCards cards, and each card's audit read (the read card the
// record names, marked for this audit, of that card) came back ok. A read that
// is missing, retired, of another audit or still asked or reading is
// unanswered; a broken read is named with its finding. No other read of a
// card counts: its review's reads are not cold.
func ColdAudit(f ReleaseFacts) ReleaseResult {
	fail := func(ev string) ReleaseResult { return ReleaseResult{Name: CheckColdAudit, Evidence: ev} }
	s := f.Snapshot()
	rec, ok, err := ReadColdAudit(s)
	switch {
	case err != nil:
		return fail(err.Error() + "; run: nova-sprint release check --audit")
	case !ok:
		return fail("no cold audit has been asked; run: nova-sprint release check --audit")
	}
	now := f.Now()
	age := now.Sub(rec.Asked)
	if age >= ColdAuditMaxAge || age < 0 {
		return fail(fmt.Sprintf("the cold audit asked %s is %s old, limit %s; run: nova-sprint release check --audit", stamp(rec.Asked), age.Truncate(time.Minute), ColdAuditMaxAge))
	}
	if len(rec.Reads) != ColdAuditCards {
		return fail(fmt.Sprintf("the cold audit asked %s sampled %d cards, not %d; run: nova-sprint release check --audit", stamp(rec.Asked), len(rec.Reads), ColdAuditCards))
	}
	var broken, unanswered []string
	for _, rd := range rec.Reads {
		var rc *Card
		if s.Readers != nil {
			rc = s.Readers.Card(rd.Read)
		}
		switch {
		case rc == nil || rc.F("primary") != rd.Card || rc.F(FieldColdAudit) != stamp(rec.Asked):
			unanswered = append(unanswered, rd.Card+" (no read "+rd.Read+" of this audit)")
		case !rc.Placed():
			unanswered = append(unanswered, rd.Card+" ("+rd.Read+" retired at "+orDash(rc.F("retired"))+" by "+orDash(rc.F("retired_by"))+")")
		case rc.Col == OK:
		case rc.Col == Broken:
			broken = append(broken, rd.Card+" ("+rc.Row+"): "+orDash(rc.F("finding")))
		default:
			unanswered = append(unanswered, rd.Card+" ("+rd.Read+" "+rc.Col+")")
		}
	}
	if len(broken) == 0 && len(unanswered) == 0 {
		return ReleaseResult{Name: CheckColdAudit, OK: true, Evidence: fmt.Sprintf("all %d cold reads ok (seed %d, since %s, asked %s, %s ago)",
			len(rec.Reads), rec.Seed, orDash(rec.Since), stamp(rec.Asked), age.Truncate(time.Minute))}
	}
	var parts []string
	if len(broken) > 0 {
		parts = append(parts, fmt.Sprintf("%d broken: %s", len(broken), strings.Join(broken, "; ")))
	}
	if len(unanswered) > 0 {
		parts = append(parts, fmt.Sprintf("%d unanswered: %s", len(unanswered), strings.Join(unanswered, ", ")))
	}
	return fail(strings.Join(parts, "; ") + fmt.Sprintf("; audit seed %d asked %s; look at: nova-sprint card <id> for each card named", rec.Seed, stamp(rec.Asked)))
}

// ColdAuditReq asks an audit: the seed of the sample, and the window it
// samples, cards landed after SinceTime (zero: every card landed), Since
// naming it (the last release tag, or --since).
type ColdAuditReq struct {
	Seed      int64
	Since     string
	SinceTime time.Time
	Who       string
}

// ColdAuditSample is the cards the audit samples: ColdAuditCards of the
// primaries (sentinels aside) landed after since, drawn uniformly by the seed
// from them in id order and returned in id order, so one seed over one table
// is one sample. Fewer landed is an error naming how many.
func ColdAuditSample(s *Snapshot, seed int64, since time.Time, name string) ([]*Card, error) {
	var landed []*Card
	for _, c := range s.Work.Column(Landed) {
		if IsSentinel(c) {
			continue
		}
		if !since.IsZero() {
			t, err := time.Parse(time.RFC3339, c.F("landed"))
			if err != nil || !t.After(since) {
				continue
			}
		}
		landed = append(landed, c)
	}
	if len(landed) < ColdAuditCards {
		return nil, fmt.Errorf("%d cards landed since %s, and the audit samples %d: nothing asked; name an earlier point with --since", len(landed), orDash(name), ColdAuditCards)
	}
	sort.Slice(landed, func(i, j int) bool { return landed[i].ID < landed[j].ID })
	perm := rand.New(rand.NewSource(seed)).Perm(len(landed))
	out := make([]*Card, ColdAuditCards)
	for i := range out {
		out[i] = landed[perm[i]]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ColdAuditSeen is every name that saw the primary pr: a reader with a read
// card of it at any attempt (placed or retired: a retired card is read as a
// record, ColdAuditExtrasForAsk), the readers its asked field names, and the
// member of each attempt's work card (its author), by the member's name and,
// for a friend's row, the friend's.
func ColdAuditSeen(s *Snapshot, pr *Card) map[string]bool {
	seen := map[string]bool{}
	for _, rc := range s.Readers.Of(pr.ID) {
		seen[rc.Row] = true
	}
	for a := 1; a <= max(pr.Int("attempt"), 1); a++ {
		for _, rd := range s.Readers.Rows() {
			if s.Readers.Card(ReadCardID(pr.ID, a, rd)) != nil {
				seen[rd] = true
			}
		}
		if s.Fleet != nil {
			if wc := s.Fleet.Card(WorkCardID(pr.ID, a)); wc != nil {
				for _, m := range []string{wc.F("member"), wc.Row} {
					if m == "" {
						continue
					}
					seen[m] = true
					if f, ok := FriendOfRow(m); ok {
						seen[f] = true
					}
				}
			}
		}
	}
	for _, rd := range Split(pr.F("asked")) {
		seen[rd] = true
	}
	return seen
}

// ColdAuditExtrasForAsk is the cards an audit's ask must read as records when
// they are not placed: every read card each reader could hold of a landed
// primary at each of its attempts, and each attempt's work card, so a reader
// whose read was retired, and an author whose work card was, still saw it.
func ColdAuditExtrasForAsk(s *Snapshot) map[string][]string {
	var reads, works []string
	for _, c := range s.Work.Column(Landed) {
		for a := 1; a <= max(c.Int("attempt"), 1); a++ {
			works = append(works, WorkCardID(c.ID, a))
			for _, rd := range s.Readers.Rows() {
				reads = append(reads, ReadCardID(c.ID, a, rd))
			}
		}
	}
	return map[string][]string{Readers: reads, Fleet: works}
}

// PlanColdAudit asks the audit: it samples the cards (ColdAuditSample) and asks
// each as one read of a reader up that never saw it (ColdAuditSeen), placed as
// the ask places a read (Ask): to the reader with the greatest share of room
// (readerRooms, round.pickByRoom), ties round the readers from the ask's index,
// a reader at width given nothing, the read drawn a route from its primary's
// read tier (readRouteOf), the indexes written with the step. The read card is
// the reader's read of the primary at its last attempt (ReadCardID; the reader
// never saw it, so it holds none), its packet that attempt's work, landed,
// against its base, marked with the audit (FieldColdAudit). The audit is
// recorded on the work table (PropColdAudit). All or nothing: a card no reader
// up that never saw it has room for refuses the whole audit, naming it, and
// no read goes to a reader that saw the card.
func PlanColdAudit(s *Snapshot, req ColdAuditReq) (Plan, ColdAuditRecord) {
	var p Plan
	rec := ColdAuditRecord{Seed: req.Seed, Since: req.Since, Asked: s.Now}
	if s.Work == nil || s.Readers == nil {
		p.refuse("release check", "the audit reads the work and readers tables, and the step loaded none")
		return p, rec
	}
	sample, err := ColdAuditSample(s, req.Seed, req.SinceTime, req.Since)
	if err != nil {
		p.refuse("release check", err.Error())
		return p, rec
	}
	rr := askRound(s)
	room := s.readerRooms(s.Readers.Rows())
	moves := roundMoves{}
	var ri routeIndexes
	if s.Fleet != nil && len(s.Routes) > 0 {
		ri = routeIndexesOf(s)
	}
	up := s.UpReaders()
	for _, c := range sample {
		seen := ColdAuditSeen(s, c)
		var free []string
		for _, rd := range up {
			if !seen[rd] {
				free = append(free, rd)
			}
		}
		picked := rr.pickByRoom(1, free, room)
		if len(picked) == 0 {
			p.refuse(c.ID, fmt.Sprintf("no reader up that never saw it has room (up: %s; saw it: %s; up and cold but at width: %s); a cold read never goes to a reader that saw the card: run: nova-sprint reader add <name>, or nova-sprint reader up <name>",
				orDash(strings.Join(up, ",")), orDash(strings.Join(sortedKeys(seen), ",")), orDash(strings.Join(free, ","))))
			continue
		}
		rd := picked[0]
		rr.moved(rd)
		moves[c.ID] = joinMoves(moves[c.ID], rd)
		attempt := max(c.Int("attempt"), 1)
		id := ReadCardID(c.ID, attempt, rd)
		fields := map[string]string{"kind": "read", "primary": c.ID, "stream": c.Row, "reader": rd, "attempt": itoa(attempt),
			"head": c.F("head"), "base": c.F("base"), "asked": stamp(s.Now), FieldColdAudit: stamp(s.Now)}
		if ri != nil {
			maps.Copy(fields, s.readRouteOf(ri, c, nil))
		} else {
			fields[FieldTier] = s.readTierOf(c)
		}
		p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row,
			Changes: []Change{change(Readers, createEntry(id, rd, Asked, c.Score, fields))},
			Moved:   c.ID + " cold audit read asked of " + rd})
		rec.Reads = append(rec.Reads, ColdAuditRead{Card: c.ID, Read: id})
	}
	if len(p.Refused) > 0 {
		return Plan{Refused: p.Refused}, ColdAuditRecord{Seed: req.Seed, Since: req.Since, Asked: s.Now}
	}
	roundWrites(&p, rr, moves)
	if ri != nil {
		ri.write(&p)
	}
	b, _ := json.Marshal(rec)
	was, had := s.Work.Prop(PropColdAudit)
	p.Props = append(p.Props, PropWrite{Table: Work, Name: PropColdAudit, Value: string(b), Was: was, WasAbsent: !had})
	return p, rec
}

// sortedKeys is a set's names in order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
