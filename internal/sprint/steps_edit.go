package sprint

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// The coordinator's edits of primaries: brief replaces a primary's brief in place, on a
// RUNNING machine as on a STOPPED one, for a card waiting, ready or in review (one an
// attempt was dealt for opens its next attempt); move takes primaries that have not started
// to another stream, on a STOPPED sprint only. Each is a pure plan of a snapshot.

// stoppedOnly is the refusal of an edit on a RUNNING machine.
func stoppedOnly(what string) string {
	return "the machine is RUNNING, and " + what + " only on a STOPPED sprint; nothing was changed; run: nova-sprint stop"
}

// unstarted is why a primary may not be edited: "" when it is a primary
// placed waiting or ready that no work card was ever dealt for (its attempt
// is 0). A card dealt, working, in review, merging or landed keeps what it
// has (keeps names it).
func unstarted(s *Snapshot, id, keeps string) string {
	c := s.Work.Placed(id)
	switch {
	case c == nil:
		return "no primary " + id + " on the work table; nothing was changed"
	case IsSentinel(c):
		return id + " is a sentinel, not a primary; nothing was changed"
	case c.Col != Waiting && c.Col != Ready:
		return fmt.Sprintf("%s is %s: a card dealt, working, in review, merging or landed keeps %s; nothing was changed", id, c.Col, keeps)
	case c.Int("attempt") > 0:
		return fmt.Sprintf("%s is %s with attempt %d dealt: a card dealt keeps %s; nothing was changed", id, c.Col, c.Int("attempt"), keeps)
	}
	return ""
}

// BriefCard is one primary's new brief (brief <id>, or one file of brief --dir).
type BriefCard struct {
	ID, Brief string
	Rules     string   // the held rules file the new brief is held to by reference (FieldRules), "" when it carries its own
	Needs     []string // the new brief's needs, as add reads its DEPENDS-ON: line (briefNeeds): a change to that line alone is taken in any state
}

// BriefReq replaces the briefs of primaries that have not started: one card
// (brief <id>; Cards, or the ID and Brief fields) or one per file of a
// directory (brief --dir, Cards). Tier, with no brief, re-tiers the card ID names.
type BriefReq struct {
	Cards          []BriefCard
	ID, Brief, Who string
	Rules          string   // the held rules file, when Cards is empty and Brief is set
	Needs          []string // the new brief's needs, when Cards is empty and Brief is set
	Answers        []string // the judgments the edit answers (--answers): each must be one it closes
	// Tier, with no Brief, re-tiers the card (nova-sprint brief --tier): the tier every
	// later deal of the card draws its route from, written on the primary as FieldTier
	// as rework --tier writes it; taken in any state, on a RUNNING machine and for a card
	// dealt (it applies to the next attempt). With a Brief, it is the tier the edit sets,
	// over the recorded one and the text's line 1.
	Tier string
	// NeedsFromBrief takes the brief edit's needs from the text's DEPENDS-ON whole
	// (--needs-from-brief), over the rule that the card's needs are the truth and a text
	// only adds new ones: the escape hatch when the coordinator means to re-derive them.
	NeedsFromBrief bool
	// Reason is why a re-tier names (nova-sprint tier --set <t> --reason): recorded
	// with the tier, for the log.
	Reason string
}

// Brief replaces primaries' briefs in place (nova-sprint brief; docs/SPEC-SPRINT.md,
// the brief verb): each a primary waiting, ready or in review, on a RUNNING machine
// as on a STOPPED one; a card merging or landed keeps its brief; a working card takes one for its next attempt. A running
// machine's pump holds a card a queued change names until the change drains
// (store.Step's Pump), so the brief is in place before the card can be dealt. The
// card keeps its id, stream and score; its needs follow the new brief's
// DEPENDS-ON lines (a brief that names none clears them; steps_edit.go). One an attempt was dealt for opens its
// next attempt (briefInPlace): a brief correction is never a twin (the owner,
// 2026-10-06: "We gotta stop doing this twin shit. it's waste."). The command line
// holds the brief to the card lint, and the store to its bound, as add's. A card
// refused, or named twice, is named; the step applies all or none.
func Brief(s *Snapshot, r BriefReq) Plan {
	var p Plan
	p.on(s)
	cards := r.Cards
	if len(cards) == 0 && r.Tier != "" && r.Brief == "" {
		// re-tier alone (nova-sprint brief <id> --tier <t>, or nova-sprint tier --set):
		// the tier every later deal draws, no text edit
		return briefTier(s, p, r)
	}
	if len(cards) == 0 {
		cards = []BriefCard{{ID: r.ID, Brief: r.Brief, Rules: r.Rules, Needs: r.Needs}}
	}
	seen, orphans := map[string]bool{}, map[string]bool{}
	// the needs each edit writes: the card's, not the text's (docs/SPEC-SPRINT.md, the
	// needs): the text's DEPENDS-ON is the admission-time list, so a need a relink
	// re-pointed stays its twin and a need the text drops stays, unless
	// --needs-from-brief takes the text whole (briefNeedsKept)
	kept, keptSaid := map[string][]string{}, map[string][]string{}
	// every needs rewrite of the batch, judged together against the state with the
	// whole batch applied (needsCycleWith): two edits that each pass alone may close
	// a cycle between them. It is the needs the edits write, never the text's list,
	// so a twin the rule keeps is held to the cycle check too.
	proposed := map[string][]string{}
	for _, b := range cards {
		c := s.Work.Placed(b.ID)
		if _, twice := kept[b.ID]; twice || c == nil || IsSentinel(c) {
			continue // a card named twice is refused below; its first edit is the one judged
		}
		want := b.Needs
		if !r.NeedsFromBrief {
			want, keptSaid[b.ID] = briefNeedsKept(s, Split(c.F("needs")), b.Needs, c.F(FieldRelinked))
		}
		kept[b.ID] = want
		if !sameNeeds(Split(c.F("needs")), want) {
			proposed[b.ID] = want
		}
	}
	for _, b := range cards {
		if seen[b.ID] {
			p.refuse(b.ID, b.ID+" is named twice; nothing was changed")
			continue
		}
		c := s.Work.Placed(b.ID)
		// a DEPENDS-ON line alone is taken in any state, the machine running or the card dealt
		if c != nil && !IsSentinel(c) && dependsOnly(c.F("brief"), b.Brief) {
			seen[b.ID] = true
			// the needs rule holds on this path too (docs/SPEC-SPRINT.md, the needs):
			// the text's DEPENDS-ON is the admission-time list, so a need the relink
			// record re-pointed stays its twin and a need the text drops stays, unless
			// --needs-from-brief takes the text whole (kept, above).
			if why := tierBelowFrontierForTLA(c.ID, r.Tier, b.Brief); why != "" {
				p.refuse(c.ID, why)
				continue
			}
			needs := kept[b.ID]
			p.Said = append(p.Said, keptSaid[b.ID]...)
			// the bound and the tier behave as on the ordinary path: a DEPENDS-ON-only
			// edit never changes the task, so the bound count stands, and the tier is a
			// card field kept against the text's line 1 unless --tier names one, each
			// said on a NOTE line (docs/SPEC-SPRINT.md, the brief: the bound and the tier).
			if c.Int("attempt") > 0 {
				p.Said = append(p.Said, fmt.Sprintf("bound: same task (%d attempts stand)", c.Int("attempt")-c.Int(FieldBriefAttempt)))
			}
			if r.Tier == "" {
				if recorded := c.F(FieldTier); recorded != "" {
					textTier, _ := cardhdr.ReadModel(b.Brief)
					if textTier.Tier != recorded {
						p.Said = append(p.Said, fmt.Sprintf("tier kept: %s (the brief's line 1 says %s)", recorded, orDash(textTier.Tier)))
					}
				}
			}
			p = briefDepends(s, p, c, b.Brief, needs, r.Tier, proposed, r.Reason)
			continue
		}
		// a card merging or landed is refused with what changes it instead:
		// stopping the machine would not let its brief be replaced. A card waiting, ready, or working takes one in place;
		// or in review takes one on a RUNNING machine as on a STOPPED one.
		why := briefKept(s, b.ID)
		if why != "" && c != nil && !IsSentinel(c) {
			why += "; " + briefStarted(c)
		}
		if why == "" && c.Int("attempt") > 0 && sameLines(c.F("brief"), b.Brief) {
			// the bound counts attempts under one brief: the same brief again, or one only
			// re-spaced or reordered, would reset it with nothing changed
			why = fmt.Sprintf("the new brief is the one %s ran attempt %d under: the bound counts attempts under one brief, so an edit changes it; nothing was changed", b.ID, c.Int("attempt"))
		}
		if why != "" {
			p.refuse(b.ID, why)
			continue
		}
		seen[b.ID] = true
		// a brief that carries its own rules names none; a grade was of the brief replaced; a
		// replaced brief is no longer the one its brief decision was asked over, so the card
		// names that decision no more
		set, unset := map[string]string{"brief": b.Brief}, []string{FieldGrade, FieldBriefOp, FieldBriefRecord}
		// the brief's bound counts attempts under one task, not one text (brief_bound.go):
		// an edit that changes only a routing line or the tier is the same task and keeps
		// the count; a new task resets it (FieldBriefAttempt). The same text again is
		// refused above (sameLines).
		if c.Int("attempt") > 0 && sameTask(c.F("brief"), b.Brief) {
			p.Said = append(p.Said, fmt.Sprintf("bound: same task (%d attempts stand)", c.Int("attempt")-c.Int(FieldBriefAttempt)))
		} else {
			set[FieldBriefAttempt] = c.F("attempt")
			if c.Int("attempt") > 0 {
				p.Said = append(p.Said, "bound: new task (count reset)")
			}
		}
		if b.Rules != "" {
			set[FieldRules] = b.Rules
		} else {
			unset = append(unset, FieldRules)
		}
		if who := WhoOfBrief(b.Brief); who != "" {
			set[FieldWho] = who // the new brief's WHO line names its worker (friend_deal.go)
		} else {
			unset = append(unset, FieldWho)
		}
		// the new brief's BENCH line names its bench (bench_deal.go): a brief step reads no
		// fleet table, so the members it names are add's to hold
		bench, benchWhy := BenchOfBrief(b.Brief)
		if benchWhy != "" {
			p.refuse(b.ID, benchWhy)
			continue
		}
		if len(bench) > 0 {
			set[FieldBench] = strings.Join(bench, ",")
		} else {
			unset = append(unset, FieldBench)
		}
		// the tier is a card field (FieldTier): a brief whose line 1 names another tier
		// than the card's recorded one keeps the recorded one, NOTEd, unless --tier <t>
		// names one (docs/SPEC-SPRINT.md, the tier).
		textTier, _ := cardhdr.ReadModel(b.Brief)
		if why := tierBelowFrontierForTLA(c.ID, r.Tier, b.Brief); why != "" {
			p.refuse(c.ID, why)
			continue
		}
		if r.Tier != "" {
			set[FieldTier] = r.Tier
		} else if recorded := c.F(FieldTier); recorded != "" && textTier.Tier != recorded {
			p.Said = append(p.Said, fmt.Sprintf("tier kept: %s (the brief's line 1 says %s)", recorded, orDash(textTier.Tier)))
		} else if t := LineOneTier(b.Brief); t != "" {
			// the tier the new line 1 names is the tier the card's next attempt is dealt on:
			// a tier the machine raised it to before stands only when it is stronger
			if now := c.F(FieldTierNow); now != "" && stronger(now, t) == t && now != t {
				set[FieldTierNow] = t
			}
			p.Said = append(p.Said, b.ID+" tier="+stronger(t, c.F(FieldTierNow)))
		}
		// the brief's needs are the card's, not the text's (docs/SPEC-SPRINT.md, the needs):
		// the text's DEPENDS-ON is the admission-time list, so a need a relink re-pointed
		// stays its twin and a need the text drops stays, unless --needs-from-brief takes
		// the text whole. A text that names a need the card does not have adds it (kept,
		// above, by briefNeedsKept).
		cur := Split(c.F("needs"))
		want := kept[b.ID]
		p.Said = append(p.Said, keptSaid[b.ID]...)
		toWaiting := false
		if !sameNeeds(cur, want) {
			if why := needsCycleWith(s, c.ID, proposed); why != "" {
				p.refuse(c.ID, why)
				continue
			}
		}
		if !sameNeeds(cur, want) {
			if len(want) > 0 {
				set["needs"] = strings.Join(want, ",")
			} else {
				unset = append(unset, "needs")
			}
			if c.Col == Ready {
				for _, n := range want {
					if s.StateOf(n) != Landed {
						toWaiting = true
					}
				}
			}
		}
		if c.Int("attempt") > 0 {
			u, orphan := briefInPlace(s, c, b.Brief, set, unset, r.Who, toWaiting, r.Reason)
			if orphan {
				orphans[c.ID] = true
			}
			p.Units = append(p.Units, u)
			continue
		}
		if toWaiting {
			moved := fmt.Sprintf("%s brief replaced (%d bytes) stream=%s %s -> waiting, needs %s", c.ID, len(b.Brief), c.Row, c.Col, strings.Join(want, ","))
			if r.Reason != "" {
				moved += "; reason: " + r.Reason
			}
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, moveEntry(c, c.Row, Waiting, set, unset...))},
				Moved: moved})
			continue
		}
		if c.Col == Checked {
			p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, moveEntry(c, c.Row, Planned, set, append(unset, "checked")...))}, Moved: c.ID + " brief replaced; check -> plan"})
			continue
		}
		moved := fmt.Sprintf("%s brief replaced (%d bytes) stream=%s %s", c.ID, len(b.Brief), c.Row, c.Col)
		if r.Reason != "" {
			moved += "; reason: " + r.Reason
		}
		p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, setEntry(c, set, unset...))},
			Moved: moved})
	}
	if len(orphans) > 0 {
		settle(&p, s, r.Who, orphans, nil)
	}
	answered(&p, s, r.Answers, r.Who)
	return Lawful(p)
}

// briefKept is why a primary keeps its brief: "" for a primary waiting, ready, in review, or working;
// a card merging or landed keeps its brief.
func briefKept(s *Snapshot, id string) string {
	c := s.Work.Placed(id)
	switch {
	case c == nil:
		return "no primary " + id + " on the work table; nothing was changed"
	case IsSentinel(c):
		return id + " is a sentinel, not a primary; nothing was changed"
	case c.Col == Merging || c.Col == Landed:
		return fmt.Sprintf("%s is %s: a card %s keeps its brief; nothing was changed", id, c.Col, c.Col)
	}
	return ""
}

// briefInPlace is a brief edited in place on a primary an attempt was dealt for: it
// keeps its id, and the edit opens its next attempt. A card in review goes to ready,
// its read cards retired, the attempt's finding kept in its findings and the judgments
// on it closed, as a rework's (an orphan merge card taken off as a rework takes it,
// orphan true); a ready or waiting card's withdrawn work card is retired, so the deal
// cuts the next attempt instead of dealing the last again. The next attempt is staged
// from the last pushed head, as a rework's (BaseOf over the card's attempts); the bound
// is reset, since it counts attempts under one brief (FieldBriefAttempt, in set); the
// rework's fix is the old brief's and is dropped; and the record, the next attempt's
// why, says who edited it, at which attempt, and what changed (briefChange).
func briefInPlace(s *Snapshot, c *Card, brief string, set map[string]string, unset []string, who string, toWaiting bool, reason string) (u Unit, orphan bool) {
	if c.F(FieldBriefDefect) != "" {
		reworkPriority(s, c, set)
		unset = append(unset, FieldBriefDefect)
	}
	n := c.Int("attempt")
	said := fmt.Sprintf("brief edited in place by %s at attempt %d: %s", orDash(who), n, briefChange(c.F("brief"), brief))
	set["why"] = cutText(said, MaxCardTextBytes)
	unset = append(unset, "fix", "finding", FieldFindingAttempt)
	var changes []Change
	retire := func(table string, x *Card) {
		changes = append(changes, change(table, removeEntry(x, map[string]string{"retired": stamp(s.Now), "retired_by": "brief"})))
	}
	if s.Fleet != nil {
		if wc := s.Fleet.Placed(WorkCardID(c.ID, n)); wc != nil && wc.Col == Withdrawn {
			retire(Fleet, wc)
		}
	}
	to := c.Col
	if c.Col == Review {
		to = Ready
		if s.Readers != nil {
			for _, rc := range s.Readers.Of(c.ID) {
				retire(Readers, rc)
			}
		}
		// a friend's read on her fleet row too: left, it keeps her room and closes against
		// the attempt the edit replaced (FriendReadClose)
		for _, rc := range openFriendReads(s, c.ID) {
			retire(Fleet, rc)
		}
		found := reworkGiven(s, c)["finding"]
		if found == "" {
			found = ownFix(s, c)
		}
		if lines := findingsOf(c, n, found); len(lines) > 0 {
			set[FieldFindings] = findingsLine(lines)
		}
		changes = append(changes, change(Work, moveEntry(c, c.Row, Ready, set, append(unset, "result", "readers", FieldPassedHead)...)))
		if m := orphanMerge(s, c); m != nil {
			changes = append(changes, change(Merge, moveEntry(m, c.Row, Returned, nil, "need_card", "need_stream")))
			orphan = true
		}
	} else {
		if toWaiting {
			// a ready card that gains a need not landed waits with its new needs
			// (steps_edit.go, the ordinary brief path)
			to = Waiting
			changes = append(changes, change(Work, moveEntry(c, c.Row, Waiting, set, unset...)))
		} else {
			changes = append(changes, change(Work, setEntry(c, set, unset...)))
		}
	}
	u = Unit{Key: c.ID, Stream: c.Row, Changes: changes, Closes: closesFor(s.Open, ReworkResolves, c.ID),
		Moved: fmt.Sprintf("%s %s; %s -> %s, attempt %d next, from its last pushed head; %d cards retired", c.ID, said, c.Col, to, n+1, len(changes)-1)}
	if reason != "" {
		u.Moved += "; reason: " + reason
	}
	if orphan {
		u.Moved += "; its orphan merge card off " + s.Merge.Placed(c.ID).Col
	}
	return u, orphan
}

// briefChange is what changed from one brief to the next, line by line: each line of the
// old one the new one lacks ("- "), then each line the new one adds ("+ "), blank lines and
// the lines' indent left out, each line cut short, the whole cut to a record's line; an edit
// with no changed line is refused before it (sameLines).
func briefChange(old, brief string) string {
	var out []string
	for _, l := range linesNotIn(old, brief) {
		out = append(out, "- "+cutText(l, 160))
	}
	for _, l := range linesNotIn(brief, old) {
		out = append(out, "+ "+cutText(l, 160))
	}
	return cutText(strings.Join(out, " | "), 1024)
}

// sameLines says two briefs hold the same lines, trimmed, blank lines left out, in any
// order: no line changed.
func sameLines(old, brief string) bool {
	return len(linesNotIn(old, brief)) == 0 && len(linesNotIn(brief, old)) == 0
}

// sameTask says two briefs are the same task: their taskOf strings match. The
// brief's bound counts attempts under one task, not one text, so an edit that
// changes only a routing line or the tier keeps the count (docs/SPEC-SPRINT.md,
// the bound).
func sameTask(old, brief string) bool { return taskOf(old) == taskOf(brief) }

// tierLineRE is the tier field on line 1 (cardhdr.ReadModel's own), stripped
// from the task hash: the tier is not the task.
var tierLineRE = regexp.MustCompile(`\btier:\s*[A-Za-z0-9_-]+`)

// taskOf is what the brief's bound hashes: the body and the PATHS, NEW, SHARED,
// TEST and DONE-WHEN lines. Line 1's tier and the routing lines (DEPENDS-ON,
// ORDER, TLC-HOSTS, MERGE, TOOLCHAIN-HOSTS) and every other header line are not
// the task, so an edit of them alone does not reset the count (docs/SPEC-SPRINT.md,
// the bound).
func taskOf(brief string) string {
	var keep []string
	inHeader := true
	for i, l := range strings.Split(brief, "\n") {
		if i == 0 {
			// line 1's tier is not the task; the rest of the line (the title) is
			l = strings.TrimSpace(tierLineRE.ReplaceAllString(l, ""))
		} else if inHeader {
			if key, v, ok := cardhdr.KeyValue(l); ok {
				switch key {
				case "PATHS", "NEW", "SHARED", "TEST", "DONE-WHEN":
					keep = append(keep, key+": "+v)
				}
				continue
			}
			if strings.TrimSpace(l) == "" {
				continue // a blank line does not end the header block (the gate's rule)
			}
			inHeader = false
		}
		if strings.TrimSpace(l) == "" {
			continue
		}
		keep = append(keep, strings.TrimSpace(l))
	}
	return strings.Join(keep, "\n")
}

// relinkMap reads a card's relink record (FieldRelinked): the ids it re-pointed,
// the twin they became, and the stamp; nil, "" and "" when there is none.
func relinkMap(relinked string) (olds []string, nw, stamp string) {
	left, right, ok := strings.Cut(relinked, " -> ")
	if !ok {
		return nil, "", ""
	}
	olds = Split(left)
	f := strings.Fields(right)
	if len(f) < 2 {
		return olds, "", ""
	}
	return olds, f[0], f[1]
}

// briefNeedsKept is a brief edit's needs, the card's and not the text's
// (docs/SPEC-SPRINT.md, the needs): it starts from the card's current needs, maps
// a name the relink re-pointed to its twin, and adds only names the text holds
// that the card does not; a need the text drops stays. A name the text holds that
// the card no longer needs and that is off the table (a card kept, not placed: a
// dropped one) is not added back: the relink record keeps only the last relink,
// so an earlier one's old name is told by this, and a dropped need would only
// block the card. notes is the NOTE line per property kept against the text: a
// twin the relink record put in place of a name the text still holds, a stale
// name left out, and a need the text drops.
func briefNeedsKept(s *Snapshot, cur, want []string, relinked string) (needs, notes []string) {
	olds, nw, stamp := relinkMap(relinked)
	needs = append([]string(nil), cur...)
	named := map[string]bool{} // the card's needs the text accounts for
	for _, n := range want {
		switch {
		case nw != "" && contains(olds, n):
			named[nw] = true
			if !contains(needs, nw) {
				needs = append(needs, nw)
			}
			notes = append(notes, fmt.Sprintf("needs kept: %s (relinked from %s %s)", nw, n, orDash(stamp)))
		case contains(needs, n):
			named[n] = true
		case keptOffTable(s, n):
			notes = append(notes, fmt.Sprintf("needs kept: %s not added (%s, and the card's needs no longer name it)", n, placeWord(s.Work.Card(n))))
		default:
			named[n] = true
			needs = append(needs, n)
		}
	}
	for _, n := range cur {
		if !named[n] {
			notes = append(notes, fmt.Sprintf("needs kept: %s (the text's DEPENDS-ON drops it; --needs-from-brief takes the text whole)", n))
		}
	}
	return needs, notes
}

// keptOffTable says the id is a card the work table keeps but has not placed (a card
// dropped, off the table): never a need a brief edit adds back.
func keptOffTable(s *Snapshot, id string) bool {
	c := s.Work.Card(id)
	return c != nil && !c.Placed()
}

// tierBelowFrontierForTLA is why a brief edit's --tier is refused: a tier below
// frontier for a brief whose PATHS or NEW covers tla/ (the lint's tla-is-frontier
// rule, as briefTier refuses it for tier --set); "" when tier is unset or allowed.
func tierBelowFrontierForTLA(id, tier, brief string) string {
	if tier == "" || tier == cardhdr.RouteFrontier || !briefCoversTLA(brief) {
		return ""
	}
	return id + "'s PATHS covers tla/, a TLA+ model, and a model is the frontier tier's work: --tier " + tier + " is below frontier and refused (the lint's tla-is-frontier rule); nothing was changed"
}

// sameNeeds says two need lists are the same, in order: the ordinary brief path
// writes a card's needs only when they differ from the brief's (steps_edit.go).
func sameNeeds(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// linesNotIn is the lines of a, trimmed, that b does not hold, each counted as often as b
// holds it; no blank line.
func linesNotIn(a, b string) []string {
	have := map[string]int{}
	for _, l := range strings.Split(b, "\n") {
		have[strings.TrimSpace(l)]++
	}
	var out []string
	for _, l := range strings.Split(a, "\n") {
		t := strings.TrimSpace(l)
		if have[t] > 0 {
			have[t]--
			continue
		}
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// briefTier is a card re-tiered (nova-sprint brief <id> --tier <t>; the owner, 2026-10-04:
// "If there are pro cards that are really heavy, then let's mark them as heavy"): its tier
// pinned to the one named, as rework --tier pins it (FieldTier: every later deal and read
// of the card draws from it, over its brief's line 1, and the machine never escalates it
// past it), taken in any state, on a RUNNING machine and for a card dealt, since the
// class of model a task needs is not a change of the task: a card working finishes its
// attempt where it is and the next attempt is dealt on the tier. A card landed, a
// sentinel, a card whose brief pins a model, and a tier that is no class are refused.
func briefTier(s *Snapshot, p Plan, r BriefReq) Plan {
	c := s.Work.Placed(r.ID)
	switch {
	case c == nil:
		p.refuse(r.ID, "no primary "+r.ID+" on the work table; nothing was changed")
		return p
	case IsSentinel(c):
		p.refuse(r.ID, r.ID+" is a sentinel, not a primary; nothing was changed")
		return p
	case !cardhdr.IsRoute(r.Tier):
		p.refuse(c.ID, "--tier wants "+cardhdr.RouteList+", found "+r.Tier+"; nothing was changed")
		return p
	case c.Col == Landed:
		p.refuse(c.ID, c.ID+" is landed: a card landed keeps its tier; nothing was changed")
		return p
	case c.F(FieldTier) == r.Tier:
		p.refuse(c.ID, c.ID+" is pinned to tier "+r.Tier+" already; nothing was changed")
		return p
	case r.Tier != cardhdr.RouteFrontier && briefCoversTLA(c.F("brief")):
		p.refuse(c.ID, c.ID+"'s PATHS covers tla/, a TLA+ model, and a model is the frontier tier's work: a tier below frontier is refused (the lint's tla-is-frontier rule); nothing was changed")
		return p
	}
	if m, _ := cardhdr.ReadModel(c.F("brief")); m.Pin != "" {
		p.refuse(c.ID, "its brief pins model "+m.Pin+", which it runs on whatever its tier; nothing was changed")
		return p
	}
	moved := fmt.Sprintf("%s tier pinned to %s (was %s): its next deal draws from it stream=%s %s attempt=%d", c.ID, r.Tier, orDash(c.F(FieldTier)), c.Row, c.Col, c.Int("attempt"))
	if r.Reason != "" {
		moved += "; reason: " + r.Reason
	}
	p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, setEntry(c, map[string]string{FieldTier: r.Tier}))},
		Moved: moved})
	return p
}

// briefCoversTLA says the brief's PATHS or NEW covers tla/ (the lint's
// tla-is-frontier rule): a TLA+ model, the frontier tier's work, so a tier below
// frontier is refused.
func briefCoversTLA(brief string) bool {
	for _, e := range swarm.CardPaths([]byte(brief)) {
		e = strings.TrimSpace(e)
		if e == "tla" || strings.HasPrefix(e, "tla/") {
			return true
		}
	}
	return false
}

// briefStarted is the remedy of a brief refused for a card merging or landed (a
// working card now takes one in place, briefKept): what changes its work instead,
// by the lifecycle's moves (lifecycle.go, Moves). From merging a return puts it
// back in review, where the brief is edited in place, its next attempt; drop takes
// any open card off the table, and the new brief is then a new card.
func briefStarted(c *Card) string {
	readd := "nova-sprint add --stream " + c.Row + " <new id> --brief-file <path>"
	drop := "nova-sprint drop " + c.ID + " --reason '<why>', then " + readd
	brief := "nova-sprint brief " + c.ID + " --brief-file <path> (in place, its next attempt)"
	switch c.Col {
	case Merging:
		return "run: nova-sprint return " + c.ID + " --reason '<why>', then " + brief + ", or " + drop
	case Landed:
		return "run: " + readd + " for the change"
	}
	return "run: " + drop
}

// reworkWhy is why a primary takes no rework (rework is the next attempt of one
// in review), with the command that does what was wanted by its state: a card
// still working is reworked once it finishes, or dropped now; one never dealt
// has its brief replaced; one dealt and not finished is dropped and added again;
// one landed is a new card; one in review that ended on a brief defect is re-cut, never
// reworked: a rework deals the brief as cut again (docs/SPEC-SPRINT.md section 1, a brief
// defect), so it is dropped, then its re-cut brief added. "" for any other primary in review.
func reworkWhy(c *Card) string {
	if c.Placed() && c.Col == Review && c.F(FieldBriefDefect) != "" {
		return "it ended on a brief defect (" + c.F(FieldBriefDefect) + "): a rework deals the same brief again; re-cut the brief: nova-sprint drop " + c.ID +
			" --reason 'a brief defect: re-cut', then nova-sprint add --stream " + c.Row + " '<new id>' --brief-file '<the re-cut brief>'"
	}
	why := inState(c, Review)
	if why == "" || !c.Placed() {
		return why
	}
	drop := "nova-sprint drop " + c.ID + " --reason '<why>'"
	switch {
	case c.Col == Working:
		return why + "; its attempt is still running: once it finishes (review), run: nova-sprint rework " + c.ID + " --fix '<what changes>', or now: " + drop
	case (c.Col == Waiting || c.Col == Ready) && c.Int("attempt") == 0:
		return why + "; no attempt has run, so there is nothing to rework: change its task instead: nova-sprint brief " + c.ID + " --brief-file <path>"
	case c.Col == Landed:
		return why + "; it landed: the change is a new card: nova-sprint add --stream " + c.Row + " <new id> --brief-file <path>"
	}
	return why + "; run: nova-sprint card " + c.ID + " (what holds it), or " + drop
}

// MoveReq moves primaries that have not started to another stream, placed as
// add places cards: Before, After or Score, else at the end of the line.
type MoveReq struct {
	IDs           []string
	Stream        string
	Score         *float64
	Before, After string
	Who           string
}

// MoveCards takes primaries to another stream (nova-sprint move): on a STOPPED
// machine only, each a primary waiting or ready with no work card dealt and
// not of the stream already; all or none. The destination is placed exactly as
// add places cards (Add, on the snapshot without the moved cards): the stream
// made as add makes one when it is new, the cards in line by --before,
// --after or --score, else at the end, waiting or ready by their needs and the
// stream's sentinels, a reached sentinel behind them no longer reached, a
// cycle of needs refused; a ready card the destination would put behind a
// sentinel is refused by the lifecycle (Lawful: ready -> waiting is only the
// effect of inserting a sentinel). A moved card is the same card moved, not a new one:
// its id, brief, needs and admission stay, and a need naming it still holds.
func MoveCards(s *Snapshot, r MoveReq) Plan {
	var p Plan
	p.on(s)
	view := *s
	view.Work = s.Work.Frozen()
	moving := map[string]*Card{}
	var cards []CardAdd
	for _, id := range r.IDs {
		why := unstarted(s, id, "its stream")
		c := s.Work.Placed(id)
		switch {
		case s.Running:
			why = stoppedOnly("a card is moved")
		case why != "":
		case moving[id] != nil:
			why = "named twice"
		case c.Row == r.Stream:
			why = id + " is in stream " + r.Stream + " already; its place in line changes with nova-sprint rank; nothing was changed"
		}
		if why != "" {
			p.refuse(id, why)
			continue
		}
		moving[id] = c
		view.Work.Drop(id)
		cards = append(cards, CardAdd{ID: id, Brief: c.F("brief"), Needs: without(Split(c.F("needs")), Split(c.F("waived"))), File: id})
	}
	if len(cards) == 0 {
		return p
	}
	q := Add(&view, AddReq{Stream: r.Stream, Cards: cards, Score: r.Score, Before: r.Before, After: r.After, Who: r.Who})
	p.Rows, p.Notes, p.inserting = q.Rows, q.Notes, q.inserting
	p.Refused = append(p.Refused, q.Refused...)
	for _, u := range q.Units {
		var notes []Note
		for _, n := range u.Notes {
			// a need dropped was the card's before it moved: its judgment is open already
			if n.Type != NBlocked || moving[u.Key] == nil {
				notes = append(notes, n)
			}
		}
		u.Notes = notes
		for i, ch := range u.Changes {
			c := moving[ch.Entry.ID]
			if ch.Table != Work || ch.Entry.Create == nil || c == nil {
				continue
			}
			to := ch.Entry.Create
			score := to.Score
			u.Changes[i] = change(Work, ntable.BatchMemberEntry{ID: c.ID, Expect: at(c),
				Move: &ntable.MemberMoveOp{Row: to.Row, Col: to.Col, Score: &score}, Set: map[string]string{"stream": to.Row}})
			u.Moved = "moved from stream " + c.Row + " " + c.Col + ": " + u.Moved
		}
		p.Units = append(p.Units, u)
	}
	return p
}

// dependsOnly says two briefs differ in their DEPENDS-ON: line alone: the same
// lines, in order, but for that line (cardhdr.KeyValue), which differs.
func dependsOnly(old, brief string) bool {
	a, b := strings.Split(old, "\n"), strings.Split(brief, "\n")
	if len(a) != len(b) {
		return false
	}
	differ := false
	for i := range a {
		if a[i] == b[i] {
			continue
		}
		ka, _, oka := cardhdr.KeyValue(a[i])
		kb, _, okb := cardhdr.KeyValue(b[i])
		if !oka || !okb || ka != "DEPENDS-ON" || kb != "DEPENDS-ON" {
			return false
		}
		differ = true
	}
	return differ
}

// briefDepends is a brief whose DEPENDS-ON: line alone changed: taken in any
// state, on a RUNNING machine and for a card dealt (it applies to the next
// attempt), since re-pointing a card's needs after a drop is not a change of
// its task (the comfort list of 2026-10-03, item 2). The card's needs become
// needs, which Brief maps through the needs rule (briefNeedsKept) before it
// calls: each a primary on the table, not the card itself, never closing a
// cycle (needsCycle, cycle.go: the check add makes). A ready card takes no need
// that has not landed; a dealt card that does goes working -> waiting at the
// tick (needsWithdrawals, needsStopReturned); a card in review takes none,
// since nothing moves it back and an accept would carry the need past it
// (docs/SPEC-SPRINT.md section 9, rule 11; tla/SprintTables.tla Rule11 and its
// reversed witness "reviewneeds"). The brief decision and the grade were over
// the same task and stay. tier, when non-empty, is the tier the edit sets
// (--tier), written on the primary as the ordinary path writes it (FieldTier).
func briefDepends(s *Snapshot, p Plan, c *Card, brief string, needs []string, tier string, proposed map[string][]string, reason string) Plan {
	for _, n := range needs {
		nc := s.Work.Card(n)
		switch {
		case n == c.ID:
			p.refuse(c.ID, "DEPENDS-ON names the card itself; nothing was changed")
			return p
		case nc == nil || !nc.Placed():
			p.refuse(c.ID, "DEPENDS-ON names "+n+", which is no primary on the table; nothing was changed")
			return p
		case c.Col == string(Ready) && nc.Col != string(Landed):
			p.refuse(c.ID, fmt.Sprintf("%s is ready and DEPENDS-ON names %s (%s), not landed: a ready card would be dealt before its need; nothing was changed; run: nova-sprint drop %s --reason '<why>', then nova-sprint add --stream %s %s --brief-file <path>", c.ID, n, nc.Col, c.ID, c.Row, c.ID))
			return p
		case c.Col == string(Review) && nc.Col != string(Landed):
			p.refuse(c.ID, fmt.Sprintf("%s is in review and DEPENDS-ON names %s (%s), not landed: a card in review has every need landed (rule 11), and nothing moves it back to wait; nothing was changed; name %s once it lands, or rework %s first", c.ID, n, nc.Col, n, c.ID))
			return p
		}
	}
	if proposed == nil {
		proposed = map[string][]string{c.ID: needs}
	}
	if why := needsCycleWith(s, c.ID, proposed); why != "" {
		p.refuse(c.ID, why)
		return p
	}
	set, unset := map[string]string{"brief": brief}, []string(nil)
	if tier != "" {
		set[FieldTier] = tier
	}
	if len(needs) > 0 {
		set["needs"] = strings.Join(needs, ",")
	} else {
		unset = append(unset, "needs")
	}
	moved := fmt.Sprintf("%s DEPENDS-ON replaced: needs %s (%d bytes) stream=%s %s", c.ID, orDash(set["needs"]), len(brief), c.Row, c.Col)
	if tier != "" && reason != "" {
		moved += "; reason: " + reason
	}
	p.Units = append(p.Units, Unit{Key: c.ID, Stream: c.Row, Changes: []Change{change(Work, setEntry(c, set, unset...))},
		Moved: moved})
	return p
}

// NeedsSetReq re-points one primary's needs (nova-sprint needs set).
type NeedsSetReq struct {
	ID      string
	Needs   []string
	Clear   bool
	Answers []string
	Who     string
}

// NeedsSet sets a primary's needs directly, in place of dropping the card and
// adding it again (the owner, 2026-10-11: "I hate twins." / "Fix your tools.").
// A card's needs are otherwise read once at add, and a brief edit keeps them
// (it only adds a name its text holds, briefNeedsKept, unless --needs-from-brief),
// so a card admitted with the wrong needs could be corrected only by a twin or a
// whole-text re-derive. NeedsSet writes the same fields the brief edit does
// (steps_edit.go, Brief and briefDepends): it rewrites the brief's needs line so
// card <id> shows the truth, and moves the card by the lifecycle: a ready card
// that gains a need not landed moves ready -> waiting, a waiting card whose new
// needs have all landed moves waiting -> ready, and a card dealt, working or in
// review keeps its attempt and takes new needs by briefDepends (a dealt card
// with a need not landed waits again at the tick; one in review takes none). A
// rewrite that would close a cycle of needs is refused (needsCycle). A landed or
// off-table card, a sentinel, and a need that is no primary on the table (named
// whole) are refused, nothing written. Coordinator-only (docs/SPEC-SPRINT.md,
// needs). It is the one writer of a primary's needs, with the brief edit.
func NeedsSet(s *Snapshot, r NeedsSetReq) Plan {
	var p Plan
	p.on(s)
	if why := notCoordinator(s, r.Who, "needs set"); why != "" {
		p.refuse(r.ID, why)
		return p
	}
	c := s.Work.Placed(r.ID)
	switch {
	case c == nil:
		p.refuse(r.ID, "no primary "+r.ID+" on the table; nothing was changed")
		return p
	case IsSentinel(c):
		p.refuse(r.ID, r.ID+" is a sentinel, not a primary; nothing was changed")
		return p
	case c.Col == Merging || c.Col == Landed:
		p.refuse(c.ID, c.ID+" is "+c.Col+": a card "+c.Col+" keeps its needs; nothing was changed")
		return p
	}
	wants := r.Needs
	if r.Clear {
		wants = nil
	}
	seen := map[string]bool{}
	var why []string
	for _, n := range wants {
		switch {
		case n == c.ID:
			why = append(why, n+" is the card itself")
		case seen[n]:
			why = append(why, n+" is named twice")
		case s.Work.Placed(n) == nil:
			why = append(why, n+" is no primary on the table")
		}
		seen[n] = true
	}
	if len(why) > 0 {
		p.refuse(c.ID, strings.Join(why, "; ")+"; nothing was changed")
		return p
	}
	if why := needsCycle(s, c.ID, wants); why != "" {
		p.refuse(c.ID, why)
		return p
	}
	brief := setBriefNeeds(c.F("brief"), wants)
	// the path is chosen by the card's column, not by whether it has ever been
	// dealt: a card working or in review is dealt, so it takes the new needs
	// through the DEPENDS-ON rule (steps_edit.go, briefDepends) for its next
	// attempt, keeping its attempt; a card waiting or ready has no work in
	// flight, so it takes the lifecycle move, which computes the column from the
	// new needs and keeps the attempt field the card holds (the same branch a
	// card that was never dealt takes). Choosing by attempt would route a card
	// returned to waiting or ready after an attempt (briefInPlace) through
	// briefDepends, which refuses a ready card a need not landed.
	if c.Col == Working || c.Col == Review {
		q := briefDepends(s, p, c, brief, wants, "", nil, "")
		if c.Col == Review && ReturnedAtAttempt(c) && len(q.Refused) == 0 {
			// returned to review to have its needs set: that was the return's one cause, and
			// the tick takes it back to merging on the reads that stand (FieldReturnedFor;
			// seat_answers.go, rule (a)), never a "returned to review" left to the seat
			markReturnedFor(&q, c, ReturnedForNeeds)
		}
		answered(&q, s, r.Answers, r.Who)
		return Lawful(q)
	}
	to := Ready
	for _, n := range wants {
		if s.Work.Placed(n).Col != Landed {
			to = Waiting
		}
	}
	set := map[string]string{"brief": brief}
	var unset []string
	if len(wants) > 0 {
		set["needs"] = strings.Join(wants, ",")
	} else {
		unset = append(unset, "needs")
	}
	u := Unit{Key: c.ID, Stream: c.Row}
	if to != c.Col {
		u.Changes = []Change{change(Work, moveEntry(c, c.Row, to, set, unset...))}
		u.Moved = fmt.Sprintf("%s needs %s: %s -> %s stream=%s", c.ID, orDash(set["needs"]), c.Col, to, c.Row)
	} else {
		u.Changes = []Change{change(Work, setEntry(c, set, unset...))}
		u.Moved = fmt.Sprintf("%s needs %s stream=%s %s", c.ID, orDash(set["needs"]), c.Row, c.Col)
	}
	p.Units = append(p.Units, u)
	answered(&p, s, r.Answers, r.Who)
	return Lawful(p)
}

// setBriefNeeds rewrites the line a brief names its needs on, to needs: the
// first Needs: header line (cardhdr.KeyValue), else a DEPENDS-ON: line, keeping
// that line's key; --clear (needs empty) takes the line off; a brief with no
// such line and needs gains a DEPENDS-ON: line above it. Every other line is
// kept byte for byte, so card <id> shows the truth and nothing else changes
// (docs/SPEC-SPRINT.md, needs).
func setBriefNeeds(brief string, needs []string) string {
	lines := strings.Split(brief, "\n")
	for i, l := range lines {
		key, _, ok := cardhdr.KeyValue(l)
		if !ok || (key != "Needs" && key != "DEPENDS-ON") {
			continue
		}
		if len(needs) == 0 {
			return strings.Join(append(append([]string(nil), lines[:i]...), lines[i+1:]...), "\n")
		}
		lines[i] = key + ": " + strings.Join(needs, ",")
		return strings.Join(lines, "\n")
	}
	if len(needs) == 0 {
		return brief
	}
	return "DEPENDS-ON: " + strings.Join(needs, ",") + "\n" + brief
}

// needsCycle is why rewriting id's needs to wants would close a cycle of needs,
// "" when it would not (needsCycleWith, the batch of one).
func needsCycle(s *Snapshot, id string, wants []string) string {
	return needsCycleWith(s, id, map[string][]string{id: wants})
}

// needsCycleWith is why the needs rewrites proposed (by card id, its new needs)
// would put id on a cycle of needs, "" when they would not: the check add makes
// (cycle.go, newNeedGraph and closes), on every path that rewrites a card's
// needs (needs set, a brief's DEPENDS-ON, alone or in a whole brief), judged
// against the state with the whole batch applied, so a batch whose edits each
// pass alone (A needs B, B needs A) is refused together. The graph takes every
// open card's stored named needs, whatever its state: a dealt card that gained a
// need not landed keeps its edge until the tick moves it back to waiting
// (needsWithdrawals), and a rewrite that closes a loop through it is a cycle.
// The cards rewritten are taken as waiting, since a card with a need not landed
// waits (or goes back to waiting at the tick). tla/SprintTables.tla NoNeedCycle
// and its reversed witness "cycle".
func needsCycleWith(s *Snapshot, id string, proposed map[string][]string) string {
	if len(proposed[id]) == 0 {
		return ""
	}
	live := func(c *Card, needs []string) []string {
		var waived []string
		if c != nil {
			waived = Split(c.F("waived"))
		}
		var out []string
		for _, n := range needs {
			if !contains(waived, n) {
				out = append(out, n)
			}
		}
		return out
	}
	// every other open card's stored needs are edges whatever its state
	// (cycle.go, needGraph.needs)
	named := map[string][]string{}
	waits := map[string]bool{}
	for pid, wants := range proposed {
		named[pid] = live(s.Work.Card(pid), wants)
		waits[pid] = true
	}
	g := newNeedGraph(s, named, nil, waits)
	if cycle := g.closes([]string{id}); cycle != nil {
		return "the needs would make a cycle: " + g.tellLoop(cycle) + "; nothing was changed"
	}
	return ""
}

// markReturnedFor writes the mechanical cause of a return (FieldReturnedFor) on the
// primary id's work-table change in the plan, adding one when the plan changes it nowhere.
func markReturnedFor(p *Plan, pr *Card, cause string) {
	for i := range p.Units {
		for j := range p.Units[i].Changes {
			c := &p.Units[i].Changes[j]
			if c.Table == Work && c.Entry.ID == pr.ID {
				if c.Entry.Set == nil {
					c.Entry.Set = map[string]string{}
				}
				c.Entry.Set[FieldReturnedFor] = cause
				return
			}
		}
	}
	p.Units = append(p.Units, Unit{Key: pr.ID, Stream: pr.Row, Changes: []Change{change(Work, setEntry(pr, map[string]string{FieldReturnedFor: cause}))},
		Moved: pr.ID + " returned for its needs: the tick accepts it on the reads that stand"})
}
