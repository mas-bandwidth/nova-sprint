package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// dupCard is one card as the duplicate check reads it: the repository it names,
// the files its PATHS may change, the files it declares SHARED, and the words of
// its id and task.
type dupCard struct {
	ID     string
	Repo   string
	Paths  []string
	Shared []string
	Tokens map[string]bool
}

// dupStop are the words too common to say two cards do the same work.
var dupStop = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "is": true, "it": true, "its": true,
	"at": true, "by": true, "for": true, "with": true, "as": true, "be": true,
	"this": true, "that": true, "from": true, "not": true, "no": true,
	"are": true, "was": true, "any": true, "one": true, "each": true,
	"every": true, "when": true, "then": true, "than": true, "into": true,
	"only": true, "also": true, "same": true, "card": true, "cards": true,
	"test": true, "tests": true, "brief": true, "file": true, "files": true,
}

// dupWord matches a word of a card's id or task: a letter, then letters,
// digits or underscores, at least three characters long.
var dupWord = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]{2,}`)

// duplicateThreshold is the token overlap at or above which two cards sharing a
// file are one change, measured on these fixtures: the timestamp pair scores
// 0.273; the two v17 fixtures, which both change cmd/nova-sprint/landgo.go
// (read) for different work, score 0.095.
const duplicateThreshold = 0.20

// dupCardOf reads a card for the duplicate check: its REPO: line, its PATHS,
// its SHARED: entries, and the words of its id (hyphens as spaces) and of its
// THE TASK paragraph (from the line starting "THE TASK" up to "STEP 1."; the
// whole brief after the header when it has none).
func dupCardOf(id, brief string) dupCard {
	raw := []byte(brief)
	sharedValue, _ := swarm.CardHeaderValue(raw, "SHARED")
	var shared []string
	for _, s := range strings.Split(sharedValue, ",") {
		if s = strings.TrimSpace(s); s != "" {
			shared = append(shared, s)
		}
	}
	tokens := map[string]bool{}
	dupAddTokens(tokens, strings.ReplaceAll(id, "-", " "))
	dupAddTokens(tokens, dupTaskText(brief))
	return dupCard{ID: id, Repo: dupRepo(brief), Paths: swarm.CardPaths(raw), Shared: shared, Tokens: tokens}
}

// dupRepo is the card's REPO: line, line 1 included: the fixtures are cut to
// their REPO, BASE and PATHS lines, so the repository may sit on line 1, which
// the typed-header reader skips as the contract line.
func dupRepo(brief string) string {
	for _, line := range strings.Split(brief, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "REPO:") {
			return strings.TrimSpace(strings.TrimPrefix(t, "REPO:"))
		}
	}
	return ""
}

// dupTaskText is the text the duplicate check reads: from the line starting
// "THE TASK" up to "STEP 1.", else the whole brief after its header.
func dupTaskText(brief string) string {
	lines := strings.Split(brief, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "THE TASK") {
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
	if cut := strings.Index(brief, "\n\n"); cut >= 0 {
		return brief[cut+2:]
	}
	return brief
}

// dupAddTokens adds the words of text to set, lower-cased and with the stop
// list removed.
func dupAddTokens(set map[string]bool, text string) {
	for _, w := range dupWord.FindAllString(text, -1) {
		w = strings.ToLower(w)
		if !dupStop[w] {
			set[w] = true
		}
	}
}

// dupFinding is one probable duplicate: the card it duplicates (Of), the token
// overlap score, the shared files, and the words the two cards share.
type dupFinding struct {
	Of     string
	Score  float64
	Files  []string
	Shared []string
}

// String is the one line add and preflight print for a probable duplicate.
func (d dupFinding) String() string {
	return fmt.Sprintf("probable duplicate of %s: shares %s and words %s (overlap %.2f >= %.2f)",
		d.Of, strings.Join(d.Files, ", "), strings.Join(d.Shared, ", "), d.Score, duplicateThreshold)
}

// probableDuplicate answers whether a and b are one change carded twice: the
// same repository, a file both PATHS name that neither lists SHARED, and a
// token overlap at or above duplicateThreshold. Of names b, the card already
// there.
func probableDuplicate(a, b dupCard) (dupFinding, bool) {
	if a.Repo == "" || a.Repo != b.Repo {
		return dupFinding{}, false
	}
	files := dupOverlapFiles(a, b)
	if len(files) == 0 {
		return dupFinding{}, false
	}
	shared := dupCommonTokens(a.Tokens, b.Tokens)
	score := dupJaccard(a.Tokens, b.Tokens)
	if score < duplicateThreshold {
		return dupFinding{}, false
	}
	return dupFinding{Of: b.ID, Score: score, Files: files, Shared: shared}, true
}

// dupOverlapFiles is the files the two cards' PATHS overlap on, neither card
// having declared the file SHARED.
func dupOverlapFiles(a, b dupCard) []string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	for _, pa := range a.Paths {
		for _, pb := range b.Paths {
			if !preflightOverlap(pa, pb) {
				continue
			}
			if dupShared(a.Shared, pa) || dupShared(a.Shared, pb) ||
				dupShared(b.Shared, pa) || dupShared(b.Shared, pb) {
				continue
			}
			add(pa)
			add(pb)
		}
	}
	sort.Strings(out)
	return out
}

// dupShared says a card lists path (or a glob naming it) as SHARED.
func dupShared(shared []string, path string) bool {
	for _, s := range shared {
		if s == path || preflightOverlap(s, path) {
			return true
		}
	}
	return false
}

// dupJaccard is the Jaccard index of two token sets.
func dupJaccard(a, b map[string]bool) float64 {
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

// dupCommonTokens is the tokens both cards hold, longest first, at most eight,
// so the field or function both name is in the line.
func dupCommonTokens(a, b map[string]bool) []string {
	var out []string
	for w := range a {
		if b[w] {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// duplicatesRefused is the findings against every open card, less those whose
// Of has a non-empty reason in notDup (--not-a-duplicate-of).
func duplicatesRefused(c dupCard, open []dupCard, notDup map[string]string) []dupFinding {
	var out []dupFinding
	for _, o := range open {
		if d, ok := probableDuplicate(c, o); ok {
			if notDup[d.Of] != "" {
				continue
			}
			out = append(out, d)
		}
	}
	return out
}

// duplicateOpenCards is the open cards of the work table as the duplicate check
// reads them: any placed card but landed, with a brief.
func duplicateOpenCards(snap *sprint.Snapshot) []dupCard {
	var out []dupCard
	if snap == nil || snap.Work == nil {
		return out
	}
	for _, card := range snap.Work.Cards() {
		if !card.Placed() || card.Col == string(sprint.Landed) {
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

// duplicateOpen reads the open cards of the work table for the duplicate check.
func (a *app) duplicateOpen(st *store.Store) ([]dupCard, error) {
	snap, err := st.Load(context.Background(), []string{sprint.Work}, nil)
	if err != nil {
		return nil, err
	}
	return duplicateOpenCards(snap), nil
}

// takeNotDupArgs removes the --not-a-duplicate-of flags from args before the
// flag set parses them, and returns their values. The flag is not registered:
// the flag list keeps the flags add has always named.
func takeNotDupArgs(args []string) (rest, vals []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--not-a-duplicate-of" || a == "-not-a-duplicate-of":
			if i+1 < len(args) {
				vals = append(vals, args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--not-a-duplicate-of="):
			vals = append(vals, strings.TrimPrefix(a, "--not-a-duplicate-of="))
		case strings.HasPrefix(a, "-not-a-duplicate-of="):
			vals = append(vals, strings.TrimPrefix(a, "-not-a-duplicate-of="))
		default:
			rest = append(rest, a)
		}
	}
	return rest, vals
}

// notDupReasons reads the --not-a-duplicate-of values, each <card>=<reason>.
// A reason is required: an empty one is a usage refusal.
func notDupReasons(vals []string) (map[string]string, string) {
	if len(vals) == 0 {
		return nil, ""
	}
	out := map[string]string{}
	for _, v := range vals {
		card, reason, ok := strings.Cut(v, "=")
		card, reason = strings.TrimSpace(card), strings.TrimSpace(reason)
		switch {
		case !ok || card == "":
			return nil, fmt.Sprintf("--not-a-duplicate-of wants <card>=<reason>, got %q", v)
		case !sprint.ValidID(card):
			return nil, fmt.Sprintf("--not-a-duplicate-of wants a card id before =, and %q is not one", card)
		case reason == "":
			return nil, fmt.Sprintf("--not-a-duplicate-of %s wants a reason: <card>=<reason>", card)
		}
		out[card] = reason
	}
	return out, ""
}

// holdDuplicates holds back the cards with a probable duplicate that notDup
// does not answer, returning the cards that stay and one reason line per held
// card. The rest of the wave is admitted in the same write.
func holdDuplicates(cards []sprint.CardAdd, open []dupCard, notDup map[string]string) (keep []sprint.CardAdd, held []string, notes map[string]string) {
	for _, cd := range cards {
		c := dupCardOf(cd.ID, cd.Brief)
		findings := duplicatesRefused(c, open, notDup)
		if len(findings) == 0 {
			if reason := dupAdmittedReason(c, open, notDup); reason != "" {
				if notes == nil {
					notes = map[string]string{}
				}
				notes[cd.ID] = reason
			}
			keep = append(keep, cd)
			continue
		}
		for _, f := range findings {
			held = append(held, fmt.Sprintf("%s: %s; run: nova-sprint add ... --not-a-duplicate-of %s=<reason>", cd.ID, f.String(), f.Of))
		}
	}
	return keep, held, notes
}

// dupAdmittedReason is the admission note of a card admitted over a duplicate:
// the --not-a-duplicate-of reason for each card it duplicates.
func dupAdmittedReason(c dupCard, open []dupCard, notDup map[string]string) string {
	var out []string
	for _, o := range open {
		if _, ok := probableDuplicate(c, o); !ok {
			continue
		}
		if reason := notDup[o.ID]; reason != "" {
			out = append(out, "not a duplicate of "+o.ID+": "+reason)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "; ")
}

// duplicateNoteStep writes the --not-a-duplicate-of reasons on the admitted
// cards' MOVED lines: the admission note the log and card <id> tell.
func duplicateNoteStep(step store.Step, notes map[string]string) store.Step {
	if len(notes) == 0 {
		return step
	}
	plan := step.Plan
	step.Plan = func(s *sprint.Snapshot) sprint.Plan {
		p := plan(s)
		for i := range p.Units {
			if n := notes[p.Units[i].Key]; n != "" {
				p.Units[i].Moved += "; " + n
			}
		}
		return p
	}
	return step
}
