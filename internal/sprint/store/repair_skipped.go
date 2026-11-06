package store

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Reconciling a repair's skips (nova-sprint repair --skipped <op>.skip; the
// v1.5.4 hotfix). Before v1.5.4 a repair that sent a cut operation's later
// manifest again entry by entry, under new operation ids, judged an entry the
// manifest had already applied under its own id as skipped (MEMBERREVISION:
// the member one revision past what it expected, at the entry's target), and
// dropped its move line: the table holds the change and the log does not, a
// rule 13 violation per card. On 2026-10-17 a tick drain's repair skipped 173
// entries this way, 159 cards and 14 table properties, and check reported 106
// rule 13 violations.
//
// The reconcile reads the skip judgment, and for each card it lists:
//   - restore: the store still holds the member as the skip found it (its
//     revision, its place), one revision past what the entry expected, and at
//     the place the card's queued changes took it: the entry applied, and the
//     missing move line is appended (expected -> found, the queued lines'
//     words and fields, its generation as the store holds it), saying why;
//     Its evidence is required: the card's last move line before the repair
//     ends at the place the entry expected, and its queued lines end at the
//     place the store holds it;
//   - settled: a later move line of the log moves the card on from the place
//     the skip found it (the entry applied, and the log already places the
//     card: a line now would misplace it), the log already has the line
//     restored (a run before this one), or the repair kept the line (a set
//     line whose first card was another, before v1.5.4);
//   - unverified: a table property (the skip records no value the manifest
//     set); it shows the value the store holds, and only --accept-props
//     settles it;
//   - refused, by name: anything else; nothing is written for it.
// Only a tick drain's skips are reconciled (the restored lines are the
// drain's: its verb, the machine). The judgment is closed when nothing is
// refused or unverified and the log, with the restored lines, replays to
// every card (no rule 13 violation). tla/RepairLog.tla Reconcile.

// SkipEntry is one entry of a skip judgment as reconcile judged it.
type SkipEntry struct {
	Card     string `json:"card,omitempty"`
	Prop     string `json:"prop,omitempty"`
	Value    string `json:"value,omitempty"` // a property's value as the store holds it
	Table    string `json:"table"`
	Expected string `json:"expected,omitempty"` // row:col the entry expected
	Found    string `json:"found,omitempty"`    // row:col the repair found
	Outcome  string `json:"outcome"`            // restore, settled, unverified or refused
	Why      string `json:"why"`
}

// Reconcile outcomes of one entry.
const (
	SkipRestore    = "restore"
	SkipSettled    = "settled"
	SkipUnverified = "unverified"
	SkipRefused    = "refused"
)

// ReconcileResult is what reconciling one skip judgment did (or, dry, would do).
type ReconcileResult struct {
	Judgment string         `json:"judgment"`
	Op       string         `json:"op"` // the repaired operation
	Entries  []SkipEntry    `json:"entries"`
	Lines    []sprint.Line  `json:"-"`
	Closed   bool           `json:"closed"`
	Dry      bool           `json:"dry_run,omitempty"`
	Counts   map[string]int `json:"counts"`
	// Rule13 is every rule 13 violation the log, with the restored lines,
	// still has against the tables: the judgment stays open while there is one.
	Rule13  []string `json:"rule13,omitempty"`
	Written string   `json:"written,omitempty"` // the reconcile's own operation id
	Replay  bool     `json:"replay,omitempty"`
}

// parsedSkip is one entry of a skip judgment's text (Skip.String).
type parsedSkip struct {
	card, table, expected, found, prop string
}

var (
	skipCard  = regexp.MustCompile(`^card (\S+) \(primary \S+\) on (\S+): expected (.*?), found (.*?); the store: `)
	skipProp  = regexp.MustCompile(`^table property (\S+) on (\S+); the store: `)
	skipHead  = regexp.MustCompile(`^repair of (\S+) \((.*?)\) skipped (\d+) entries the store refused as recorded: `)
	skipStart = regexp.MustCompile(`(?:^|; )(card \S+ \(primary |table property )`)
	revWord   = regexp.MustCompile(`(?:^|, )revision (\d+)(?:,|$)`)
	atWord    = regexp.MustCompile(`(?:^|, )at (\S+?:\S+?)(?:,|$)`)
)

// parseSkips reads a skip judgment's text back into the repaired operation,
// its verb and its entries: the head names the operation, the verb and the
// count, and the entries are Skip.String joined by "; " (a store's text may
// hold "; " itself, so an entry begins only at "card " or "table property ").
// A text whose entries do not come to its count is refused.
func parseSkips(what string) (string, string, []parsedSkip, error) {
	h := skipHead.FindStringSubmatch(what)
	if h == nil {
		return "", "", nil, fmt.Errorf("the judgment's text is not a repair's skips")
	}
	n, _ := strconv.Atoi(h[3])
	rest := what[len(h[0]):]
	starts := skipStart.FindAllStringIndex(rest, -1)
	var out []parsedSkip
	for i, s := range starts {
		b := s[0]
		if b > 0 {
			b += 2
		}
		e := len(rest)
		if i+1 < len(starts) {
			e = starts[i+1][0]
		}
		part := rest[b:e]
		if m := skipCard.FindStringSubmatch(part); m != nil {
			out = append(out, parsedSkip{card: m[1], table: m[2], expected: m[3], found: m[4]})
			continue
		}
		if m := skipProp.FindStringSubmatch(part); m != nil {
			out = append(out, parsedSkip{prop: m[1], table: m[2]})
			continue
		}
		return "", "", nil, fmt.Errorf("entry %d of the judgment cannot be read: %.120s", i+1, part)
	}
	if len(out) != n {
		return "", "", nil, fmt.Errorf("the judgment lists %d entries and %d were read", n, len(out))
	}
	return h[1], h[2], out, nil
}

// placeRev is the revision and place (row:col) a skip's words name.
func placeRev(words string) (uint64, string, bool) {
	r := revWord.FindStringSubmatch(words)
	a := atWord.FindStringSubmatch(words)
	if r == nil || a == nil {
		return 0, "", false
	}
	n, err := strconv.ParseUint(r[1], 10, 64)
	return n, a[1], err == nil
}

// lineCards is every card a move line is about.
func lineCards(l sprint.Line) []string {
	if len(l.Cards) > 0 {
		return l.Cards
	}
	return []string{l.Card}
}

// cutCause is a cause cut to sprint.MaxCause bytes on a UTF-8 boundary.
func cutCause(c string) string {
	if len(c) <= sprint.MaxCause {
		return c
	}
	n := sprint.MaxCause - 3
	for n > 0 && !utf8.RuneStart(c[n]) {
		n--
	}
	return c[:n] + "..."
}

// ReconcileSkipped reconciles the open skip judgment id (see above): dry plans
// and writes nothing; acceptProps settles the table properties it lists at the
// values the store holds. callerOp, when given, records the result under it,
// and the same id again returns that result and writes nothing.
func (st *Store) ReconcileSkipped(ctx context.Context, id, callerOp string, dry, acceptProps bool) (ReconcileResult, error) {
	pinned, err := st.pin(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	if callerOp != "" {
		if body, ok, err := pinned.B.Done(ctx, callerOp); err != nil {
			return ReconcileResult{}, err
		} else if ok {
			var res ReconcileResult
			if json.Unmarshal([]byte(body), &res) == nil && res.Judgment == id {
				res.Replay = true
				return res, nil
			}
			return ReconcileResult{}, fmt.Errorf("operation id %s already holds another result; give a new --op", callerOp)
		}
	}
	r := pinned.retry(ctx)
	for r.next(pinned.attempts()) {
		_, gen, err := pinned.Fenced(ctx, nil, nil, nil)
		if err != nil {
			return ReconcileResult{}, err
		}
		res, op, err := pinned.reconcilePlan(ctx, id, acceptProps)
		if err != nil || dry {
			res.Dry = dry
			return res, err
		}
		if len(op.Log)+len(op.Decided) == 0 {
			return res, nil
		}
		op.CallerOp = callerOp
		if op.CallerOp == "" {
			op.CallerOp = op.ID
		}
		res.Written = op.ID
		body, _ := json.Marshal(res)
		op.Result = string(body)
		ok, err := pinned.B.Acquire(ctx, gen, op)
		if err != nil {
			return res, fmt.Errorf("%w: acquiring the fence for %s: %v; run: nova-sprint repair", ErrUnknown, op.ID, err)
		}
		if !ok {
			continue
		}
		if err := pinned.B.Release(ctx, op, true); err != nil {
			return res, fmt.Errorf("%w: releasing %s: %v; run: nova-sprint repair", ErrUnknown, op.ID, err)
		}
		return res, nil
	}
	return ReconcileResult{}, fmt.Errorf("repair --skipped: the sprint kept changing under it (%d tries); nothing was changed; run it again", r.tries)
}

// reconcilePlan judges every entry of the skip judgment against the store and
// the log, and builds the operation that writes the restored lines and, when
// nothing is refused or unverified and the log then replays to the tables,
// closes the judgment.
func (st *Store) reconcilePlan(ctx context.Context, id string, acceptProps bool) (ReconcileResult, OpRecord, error) {
	res := ReconcileResult{Judgment: id, Counts: map[string]int{}}
	open, err := st.B.OpenNotes(ctx)
	if err != nil {
		return res, OpRecord{}, err
	}
	var entries []sprint.Open
	for _, o := range open {
		if o.Note.ID == id && o.Note.Kind == sprint.Judgment {
			entries = append(entries, o)
		}
	}
	if len(entries) == 0 {
		return res, OpRecord{}, fmt.Errorf("no open judgment is %s; run: nova-sprint inbox (a reconciled one is closed)", id)
	}
	n := entries[0].Note
	if n.Type != NRepairSkipped {
		return res, OpRecord{}, fmt.Errorf("judgment %s is %q, not a repair's skips (%q)", id, n.Type, NRepairSkipped)
	}
	opID, verb, skips, err := parseSkips(n.What)
	if err != nil {
		return res, OpRecord{}, fmt.Errorf("judgment %s: %v", id, err)
	}
	res.Op = opID
	if verb != sprint.DrainVerb {
		return res, OpRecord{}, fmt.Errorf("judgment %s is the repair of a %q operation; repair --skipped restores the lines of a %q only, whose verb and actor the restored lines carry", id, verb, sprint.DrainVerb)
	}
	lines, err := st.Log(ctx)
	if err != nil {
		return res, OpRecord{}, err
	}
	at := -1 // the skip judgment's line: the repair's commit
	for i, l := range lines {
		if l.Note != nil && l.Note.ID == id {
			at = i
			break
		}
	}
	if at < 0 {
		return res, OpRecord{}, fmt.Errorf("judgment %s has no line in this epoch's log", id)
	}
	logical := map[string]string{}
	for _, t := range []string{sprint.Work, sprint.Readers, sprint.Merge, sprint.Fleet} {
		logical[st.Names.Table(t)] = t
	}
	props := map[string]map[string]string{} // stored table -> its properties, read once
	now := st.now()
	opRec := OpRecord{ID: "repair-skipped-" + st.newID() + "~" + strconv.FormatUint(st.epoch, 10), Verb: "repair skipped", At: now}
	for _, k := range skips {
		e := SkipEntry{Card: k.card, Prop: k.prop, Table: k.table}
		if k.prop != "" {
			if _, ok := props[k.table]; !ok {
				shapes, err := st.B.Shapes(ctx, []string{k.table})
				if err != nil {
					return res, OpRecord{}, err
				}
				props[k.table] = map[string]string{}
				if len(shapes) > 0 {
					props[k.table] = shapes[0].Props
				}
			}
			v, held := props[k.table][k.prop]
			e.Value = v
			now := "absent"
			if held {
				now = k.prop + "=" + v
			}
			if acceptProps {
				e.Outcome, e.Why = SkipSettled, "a table property, accepted by --accept-props as the store holds it: "+now
			} else {
				e.Outcome, e.Why = SkipUnverified, "a table property: the skip records no value the manifest set, so it cannot be checked; the store holds "+now+"; settle it with --accept-props"
			}
			res.add(e)
			continue
		}
		table, ok := logical[k.table]
		if !ok {
			e.Outcome, e.Why = SkipRefused, "table "+k.table+" is none of the sprint's tables"
			res.add(e)
			continue
		}
		// an entry that creates its card expects it absent: it comes from
		// no place, at no revision (a created member is at revision 1)
		created := k.expected == "absent" || strings.HasPrefix(k.expected, "absent, ")
		xr, xp, okx := placeRev(k.expected)
		if created {
			xr, xp, okx = 0, "", true
		}
		fr, fp, okf := placeRev(k.found)
		e.Expected, e.Found = xp, fp
		if !okx || !okf {
			e.Outcome, e.Why = SkipRefused, "the skip names no revision and place expected and found: expected "+k.expected+", found "+k.found
			res.add(e)
			continue
		}
		about := func(l sprint.Line) bool {
			return l.Kind == sprint.LineMove && l.Table == table && slices.Contains(lineCards(l), k.card)
		}
		// the log after the repair: the line restored by an earlier run, or
		// the card moved on from where the repair found it
		var later *sprint.Line
		restored := false
		for i := at + 1; i < len(lines) && later == nil; i++ {
			if !about(lines[i]) {
				continue
			}
			if lines[i].Op == opID && lines[i].From == xp && lines[i].To == fp {
				restored = true
				continue
			}
			later = &lines[i]
		}
		// the log before the repair: the card's last move line, and the
		// changes its steps queued for the drain since
		var before *sprint.Line
		var queued []sprint.Line
		for i := at - 1; i >= 0; i-- {
			if about(lines[i]) {
				before = &lines[i]
				break
			}
			if l := lines[i]; l.Kind == sprint.LineQueued && l.Table == table && l.Card == k.card {
				queued = append([]sprint.Line{l}, queued...)
			}
		}
		switch {
		case later != nil && later.From == fp:
			e.Outcome, e.Why = SkipSettled, fmt.Sprintf("moved on from %s by %s (%s): the entry applied and the log places the card; a line now would misplace it", fp, later.Op, later.Verb)
		case later != nil:
			e.Outcome, e.Why = SkipRefused, fmt.Sprintf("the next move line of the card after the repair (%s, %s) moves it from %s, not from %s where the repair found it", later.Op, later.Verb, later.From, fp)
		case restored:
			e.Outcome, e.Why = SkipSettled, "its move line is restored already"
		case before != nil && before.Op == opID && before.To == fp:
			e.Outcome, e.Why = SkipSettled, "the repair kept its move line (a set line whose first card was another)"
		case created && before != nil && !before.Removed:
			e.Outcome, e.Why = SkipRefused, fmt.Sprintf("the entry created the card, and the log placed it before the repair (%s, %s, at %s)", before.Op, before.Verb, before.To)
		case before == nil && !created:
			e.Outcome, e.Why = SkipRefused, "the log has no move line of the card before the repair: nothing says where the entry took it from"
		case !created && before.To != xp:
			e.Outcome, e.Why = SkipRefused, fmt.Sprintf("the card's last move line before the repair (%s, %s) leaves it at %s, not at %s where the entry expected it", before.Op, before.Verb, before.To, xp)
		case len(queued) == 0:
			e.Outcome, e.Why = SkipRefused, "no queued line of the card names the change the drain applied: nothing to restore the line from"
		case queued[len(queued)-1].To != fp:
			e.Outcome, e.Why = SkipRefused, fmt.Sprintf("its queued changes take it to %s, and the repair found it at %s", queued[len(queued)-1].To, fp)
		case fr != xr+1:
			e.Outcome, e.Why = SkipRefused, fmt.Sprintf("the repair found revision %d, not one past the %d the entry expected", fr, xr)
		}
		if e.Outcome != "" {
			res.add(e)
			continue
		}
		rs, err := st.readSet(ctx, k.table, []string{st.sid(k.card)})
		if err != nil {
			return res, OpRecord{}, err
		}
		m, ok := rs.Member(st.sid(k.card))
		switch {
		case !ok:
			e.Outcome, e.Why = SkipRefused, "the store holds no member "+st.sid(k.card)
		case m.Revision != fr || !m.Placed || m.Row+":"+m.Col != fp:
			place := "not placed"
			if m.Placed {
				place = "at " + m.Row + ":" + m.Col
			}
			e.Outcome, e.Why = SkipRefused, fmt.Sprintf("the store holds revision %d %s, not revision %d at %s as the repair found it", m.Revision, place, fr, fp)
		}
		if e.Outcome != "" {
			res.add(e)
			continue
		}
		gen, _ := strconv.Atoi(m.Fields["gen"])
		row, _, _ := strings.Cut(fp, ":")
		l := sprint.Line{Kind: sprint.LineMove, At: now, Epoch: st.epoch, Op: opID, Card: k.card, Primary: k.card, Stream: row, Table: table,
			From: xp, To: fp, Gen: gen, Actor: sprint.MachineActor, Verb: verb}
		if table != sprint.Work {
			l.Primary = m.Fields["primary"]
			if s := m.Fields["stream"]; s != "" {
				l.Stream = s
			}
		}
		var causes []string
		for _, q := range queued {
			words := strings.TrimSpace(q.Cause)
			if words == "" {
				words = q.Verb
			}
			words += " (" + q.Verb
			if q.Actor != "" {
				words += " by " + q.Actor
			}
			causes = append(causes, words+")")
			if len(q.Text) > 0 && l.Text == nil {
				l.Text = map[string]string{}
			}
			maps.Copy(l.Text, q.Text)
			if len(q.Set) > 0 && l.Set == nil {
				l.Set = map[string]string{}
			}
			maps.Copy(l.Set, q.Set)
		}
		l.Cause = cutCause(strings.Join(causes, "; "))
		why := fmt.Sprintf("restored by %s (repair --skipped %s): the store holds revision %d at %s, one past the revision %d the entry expected, at its queued target: the entry applied and its line was dropped", st.Actor, id, fr, fp, xr)
		if l.Text == nil {
			l.Text = map[string]string{}
		}
		l.Text["why"] = why
		e.Outcome, e.Why = SkipRestore, why
		res.add(e)
		res.Lines = append(res.Lines, l)
	}
	opRec.Log = res.Lines
	// the log with the restored lines, replayed against the tables (check
	// rule 13): the judgment closes only when it replays to every card
	snap, err := st.Load(ctx, All, tickExtras)
	if err != nil {
		return res, OpRecord{}, err
	}
	for _, v := range sprint.PlaceViolations(snap, sprint.Replay(append(slices.Clone(lines), res.Lines...))) {
		if v.Rule == 13 {
			res.Rule13 = append(res.Rule13, v.Detail)
		}
	}
	if res.Counts[SkipRefused]+res.Counts[SkipUnverified] == 0 && len(res.Rule13) == 0 {
		res.Closed = true
		what := fmt.Sprintf("reconciled by %s: %d move lines restored, %d entries settled", st.Actor, res.Counts[SkipRestore], res.Counts[SkipSettled])
		d := sprint.Note{Kind: sprint.Decided, Type: n.Type, Stream: n.Stream, Answers: n.ID, What: what, Who: st.Actor, At: now, ID: opRec.ID + ".d1"}
		for _, o := range entries {
			opRec.Closes = append(opRec.Closes, o.Key)
			if sub := o.Subject(); sub != "" {
				d.Primaries = append(d.Primaries, sub)
			}
		}
		slices.Sort(d.Primaries)
		d.Count = len(d.Primaries)
		opRec.Decided = []sprint.Note{d}
	}
	return res, opRec, nil
}

func (r *ReconcileResult) add(e SkipEntry) {
	r.Entries = append(r.Entries, e)
	r.Counts[e.Outcome]++
}
