package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
)

// duplicateThreshold is the token overlap at or above which two cards sharing a
// file are one change; measured on these fixtures (the timestamp pair 0.273;
// the two v17 fixtures, which both change cmd/nova-sprint/landgo.go (read) for
// different work, 0.095).
const duplicateThreshold = 0.20

// dupCard is what the duplicate check reads from one brief.
type dupCard struct {
	ID     string
	Repo   string
	Paths  []string
	Shared []string
	Tokens map[string]bool
}

// dupWordRE is a word of a card's id or task paragraph: a letter, then at
// least two more letters, digits or underscores.
var dupWordRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]{2,}`)

// dupStop is the fixed stop list: the words that say nothing about the work.
var dupStop = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true, "to": true,
	"in": true, "on": true, "is": true, "it": true, "its": true, "at": true,
	"by": true, "for": true, "with": true, "as": true, "be": true, "this": true,
	"that": true, "from": true, "not": true, "no": true, "are": true, "was": true,
	"any": true, "one": true, "each": true, "every": true, "when": true,
	"then": true, "than": true, "into": true, "only": true, "also": true,
	"same": true, "card": true, "cards": true, "test": true, "tests": true,
	"brief": true, "file": true, "files": true,
}

// dupCardOf reads one card's duplicate datum: the REPO: line, the PATHS and
// SHARED entries, and the words of its id (hyphens as spaces) and of its THE
// TASK paragraph (from the line starting "THE TASK" up to "STEP 1."; the whole
// brief after the header when it has none).
func dupCardOf(id, brief string) dupCard {
	raw := []byte(brief)
	base := swarm.ReadCardBase(raw)
	c := dupCard{ID: id, Repo: base.Named, Paths: swarm.CardPaths(raw), Shared: dupHeaderGlobs(brief, "SHARED"), Tokens: map[string]bool{}}
	if c.Repo == "" {
		c.Repo = base.Repo
	}
	text := id + " " + dupTaskParagraph(brief)
	for _, w := range dupWordRE.FindAllString(strings.ToLower(text), -1) {
		if !dupStop[w] {
			c.Tokens[w] = true
		}
	}
	return c
}

// dupHeaderGlobs is the files one header line of a brief names (SHARED:), split
// on commas and blanks, none and - left out.
func dupHeaderGlobs(brief, key string) []string {
	for _, l := range strings.Split(brief, "\n") {
		k, v, ok := cardhdr.KeyValue(strings.TrimSpace(l))
		if !ok || k != key {
			continue
		}
		var out []string
		for _, g := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
			if g != "none" && g != "-" {
				out = append(out, g)
			}
		}
		return out
	}
	return nil
}

// dupHeaderLineRE is a card header line: a key, a colon, then a value or nothing.
var dupHeaderLineRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*:(\s|$)`)

// dupTaskParagraph is the text the token check reads: the THE TASK paragraph up
// to STEP 1., else the whole brief after the header.
func dupTaskParagraph(brief string) string {
	lines := strings.Split(brief, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "THE TASK") {
			start = i
			break
		}
	}
	if start >= 0 {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "STEP 1.") {
				end = i
				break
			}
		}
		return strings.Join(lines[start:end], "\n")
	}
	n := 0
	if len(lines) > 0 {
		n = 1 // line 1 is the contract line
	}
	for n < len(lines) && dupHeaderLineRE.MatchString(strings.TrimSpace(lines[n])) {
		n++
	}
	return strings.Join(lines[n:], "\n")
}

// dupFinding is one probable duplicate: the card it duplicates, the overlap
// score, the files it shares and the words it shares.
type dupFinding struct {
	Of     string
	Score  float64
	Files  []string
	Shared []string
}

// String is the one line a duplicate is named by.
func (f dupFinding) String() string {
	return fmt.Sprintf("probable duplicate of %s: shares %s and words %s (overlap %.2f >= %.2f)",
		f.Of, strings.Join(f.Files, ", "), strings.Join(f.Shared, ", "), f.Score, duplicateThreshold)
}

// probableDuplicate answers whether a and b are one change carded twice: the
// same repository, a path of one overlapping a path of the other that neither
// lists as SHARED, and a token overlap at or above duplicateThreshold (the
// Jaccard index of the token sets).
func probableDuplicate(a, b dupCard) (dupFinding, bool) {
	if a.Repo == "" || a.Repo != b.Repo {
		return dupFinding{}, false
	}
	var files []string
	seen := map[string]bool{}
	for _, pa := range a.Paths {
		for _, pb := range b.Paths {
			if !preflightOverlap(pa, pb) {
				continue
			}
			if dupShared(a, pa) || dupShared(a, pb) || dupShared(b, pa) || dupShared(b, pb) {
				continue
			}
			if !seen[pa] {
				seen[pa] = true
				files = append(files, pa)
			}
		}
	}
	if len(files) == 0 {
		return dupFinding{}, false
	}
	score := dupJaccard(a.Tokens, b.Tokens)
	if score < duplicateThreshold {
		return dupFinding{}, false
	}
	shared := dupCommon(a.Tokens, b.Tokens)
	sort.Slice(shared, func(i, j int) bool {
		if len(shared[i]) != len(shared[j]) {
			return len(shared[i]) > len(shared[j])
		}
		return shared[i] < shared[j]
	})
	if len(shared) > 8 {
		shared = shared[:8]
	}
	return dupFinding{Of: b.ID, Score: score, Files: files, Shared: shared}, true
}

// dupShared says the card lists the path, or a glob naming it, on its SHARED: line.
func dupShared(c dupCard, path string) bool {
	for _, s := range c.Shared {
		if preflightOverlap(s, path) {
			return true
		}
	}
	return false
}

// dupJaccard is the Jaccard index of two token sets.
func dupJaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	common := 0
	for w := range a {
		if b[w] {
			common++
		}
	}
	union := len(a) + len(b) - common
	if union == 0 {
		return 0
	}
	return float64(common) / float64(union)
}

// dupCommon is the tokens both sets hold, in no order.
func dupCommon(a, b map[string]bool) []string {
	var out []string
	for w := range a {
		if b[w] {
			out = append(out, w)
		}
	}
	return out
}

// duplicatesRefused is the findings against every open card, less those whose
// Of has a non-empty reason in notDup.
func duplicatesRefused(c dupCard, open []dupCard, notDup map[string]string) []dupFinding {
	var out []dupFinding
	for _, o := range open {
		if notDup[o.ID] != "" {
			continue
		}
		if f, ok := probableDuplicate(c, o); ok {
			out = append(out, f)
		}
	}
	return out
}

// dupOpenCards is the open cards of the work table: any column but landed, and
// none dropped.
func dupOpenCards(snap *sprint.Snapshot) []dupCard {
	var out []dupCard
	if snap == nil || snap.Work == nil {
		return out
	}
	for _, card := range snap.Work.Cards() {
		if !card.Placed() || card.Col == sprint.Landed || card.F("outcome") == "dropped" {
			continue
		}
		brief := card.F("brief")
		if brief == "" {
			continue
		}
		out = append(out, dupCardOf(card.ID, brief))
	}
	return out
}

// parseNotDup reads the repeated --not-a-duplicate-of <card>=<reason> flags. A
// reason is required: an empty one is refused.
func parseNotDup(vals []string) (map[string]string, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, v := range vals {
		id, reason, ok := strings.Cut(v, "=")
		id, reason = strings.TrimSpace(id), strings.TrimSpace(reason)
		if !ok || id == "" || reason == "" {
			return nil, fmt.Errorf("--not-a-duplicate-of wants <card>=<reason>, with a reason: got %q", v)
		}
		out[id] = reason
	}
	return out, nil
}

// dupHeld is the cards of a wave a probable duplicate holds back, with the
// finding that held each; the reason map is the overrides used by the cards
// that were admitted.
func dupHeld(cards []dupCard, open []dupCard, notDup map[string]string) (held map[string]dupFinding, reasons map[string]string) {
	held, reasons = map[string]dupFinding{}, map[string]string{}
	for _, c := range cards {
		all := duplicatesRefused(c, open, nil)
		left := duplicatesRefused(c, open, notDup)
		if len(left) > 0 {
			held[c.ID] = left[0]
			continue
		}
		var used []string
		for _, f := range all {
			if notDup[f.Of] != "" {
				used = append(used, f.Of+"="+notDup[f.Of])
			}
		}
		if len(used) > 0 {
			reasons[c.ID] = strings.Join(used, "; ")
		}
	}
	return held, reasons
}

// dupPrint prints one DUPLICATE line per held card, naming the override that
// would admit it.
func dupPrint(stdout io.Writer, held map[string]dupFinding) {
	ids := make([]string, 0, len(held))
	for id := range held {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := held[id]
		fmt.Fprintf(stdout, "DUPLICATE %s: %s; run: nova-sprint add ... --not-a-duplicate-of %s=<reason>\n", id, f.String(), f.Of)
	}
}

// dupNoteStep records each admitted card's --not-a-duplicate-of reason on its
// admission note.
func dupNoteStep(step store.Step, reasons map[string]string) store.Step {
	if len(reasons) == 0 {
		return step
	}
	plan := step.Plan
	step.Plan = func(s *sprint.Snapshot) sprint.Plan {
		p := plan(s)
		for i := range p.Units {
			why, ok := reasons[p.Units[i].Key]
			if !ok {
				continue
			}
			for j := range p.Units[i].Changes {
				ch := &p.Units[i].Changes[j]
				if ch.Table != sprint.Work || ch.Entry.Create == nil {
					continue
				}
				if ch.Entry.Set == nil {
					ch.Entry.Set = map[string]string{}
				}
				ch.Entry.Set[sprint.FieldNote] = "not a duplicate: " + why
			}
		}
		return p
	}
	return step
}

// dupHold is the duplicate check of the add form that names its cards by id: it
// holds back every brief with a probable duplicate and leaves the rest of the
// wave in rs, one write. It prints one DUPLICATE line per held card and returns
// exit 1 when every real card of the wave was held, 0 when the rest of the wave
// is written (the add then exits with the write's code).
func dupHold(a *app, rs []sprint.AddReq, st *store.Store, notDup map[string]string, stdout, stderr io.Writer) (map[string]dupFinding, map[string]string, int) {
	var wave []dupCard
	for _, r := range rs {
		if r.Count > 0 || r.Sentinel || len(r.IDs) == 0 {
			continue
		}
		for _, id := range r.IDs {
			wave = append(wave, dupCardOf(id, r.Brief))
		}
	}
	if len(wave) == 0 {
		return nil, nil, 0
	}
	snap, err := st.Load(context.Background(), []string{sprint.Work}, nil)
	if err != nil {
		return nil, nil, a.readFailed("add", err, stderr)
	}
	held, reasons := dupHeld(wave, dupOpenCards(snap), notDup)
	if len(held) == 0 {
		return nil, reasons, 0
	}
	dupPrint(stdout, held)
	kept := 0
	for i := range rs {
		if rs[i].Count > 0 || rs[i].Sentinel {
			kept++
			continue
		}
		var ids []string
		for _, id := range rs[i].IDs {
			if _, held := held[id]; !held {
				ids = append(ids, id)
			}
		}
		rs[i].IDs = ids
		if len(ids) > 0 {
			kept++
		}
	}
	if kept == 0 {
		return held, reasons, 1
	}
	return held, reasons, 0
}

// dupHoldMany is the duplicate check of the many-brief add form: it holds back
// every brief with a probable duplicate, prints one DUPLICATE line per held
// card, and leaves the filtering of the wave to its caller. It always returns
// exit 0: the add writes the rest of the wave and exits with the write's code,
// and exits 1 only when every real card was held.
func dupHoldMany(a *app, cards []sprint.CardAdd, st *store.Store, notDup map[string]string, stdout, stderr io.Writer) (map[string]dupFinding, map[string]string, int) {
	var wave []dupCard
	for _, cd := range cards {
		if cd.Sentinel || cd.Brief == "" {
			continue
		}
		wave = append(wave, dupCardOf(cd.ID, cd.Brief))
	}
	if len(wave) == 0 {
		return nil, nil, 0
	}
	snap, err := st.Load(context.Background(), []string{sprint.Work}, nil)
	if err != nil {
		return nil, nil, a.readFailed("add", err, stderr)
	}
	held, reasons := dupHeld(wave, dupOpenCards(snap), notDup)
	if len(held) > 0 {
		dupPrint(stdout, held)
	}
	return held, reasons, 0
}

// takeNotDupArgs reads the repeated --not-a-duplicate-of <card>=<reason> flags
// before the flag set does, so the add flag list keeps the flags it has always
// named. It returns the arguments without them and the values in order.
func takeNotDupArgs(args []string) ([]string, []string) {
	out := make([]string, 0, len(args))
	var vals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--not-a-duplicate-of" {
			if i+1 >= len(args) {
				vals = append(vals, "")
				continue
			}
			i++
			vals = append(vals, args[i])
			continue
		}
		if v, ok := strings.CutPrefix(a, "--not-a-duplicate-of="); ok {
			vals = append(vals, v)
			continue
		}
		out = append(out, a)
	}
	return out, vals
}
