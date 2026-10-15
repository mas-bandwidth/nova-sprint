package swarm

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/diffcheck"
)

// A NEED NAMES WHAT IT CONSUMES (docs/SPEC-CARD-CONTRACT.md section 2, the DEPENDS-ON
// grammar). In nova-sprint a need blocks the deal: a card is dealt only once every need
// has landed, so each DEPENDS-ON entry serialises two cards' whole runs. The lander already
// reworks a head that conflicts at the base tip without stopping the stream, so two cards
// that touch one file rarely need an edge at all. The reason is therefore part of the
// grammar, and an edge that consumes nothing cannot be written:
//
//	DEPENDS-ON: <id> (uses: <thing>[, <thing>...])
//	DEPENDS-ON: <id> (overlap: <file>; why: <reason>)
//	DEPENDS-ON: none        (or -)
//
// Entries are separated by commas outside parentheses. A thing is a repository path, a Go
// or TLA+ symbol, a verb or flag spelling, or a model name that the needed card produces.
// An overlap is kept only because a conflict would cost a TLC model rerun or a full redo.
// A `Needs:` line, which the readers take before the DEPENDS-ON: line, takes the same grammar.

// DependsUsesCheck and DependsUnknownCheck are the two tokens LintDependsOn draws.
const (
	DependsUsesCheck    = "depends-uses"
	DependsUnknownCheck = "depends-unknown"
)

// DependsRemedy is what depends-uses wants, in one line naming the two allowed forms.
const DependsRemedy = "write each DEPENDS-ON entry as `<id> (uses: <path|symbol|flag|model>[, ...])`, naming what the needed card produces (it appears in that card's NEW, PATHS or task text), or as `<id> (overlap: <file>; why: <reason>)`, a file both cards name, kept only because a conflict would cost a TLC model rerun or a full redo; a card that consumes nothing of the other names no entry (the lander reworks a conflicting head at the base tip), and the line is `none` or `-`"

// DependsUnknownRemedy is what depends-unknown wants.
const DependsUnknownRemedy = "name a card on the table or in the same add; the needed card's brief is how its uses and overlap are checked, so an id that is neither is no need"

// DependsEntry is one entry of a DEPENDS-ON: or Needs: value.
type DependsEntry struct {
	// Text is the entry as written, trimmed; ID is the card it needs.
	Text, ID string
	// Form is "uses", "overlap" or "" for an entry that gives neither reason.
	Form string
	// Uses are the things a uses entry names; Files and Why an overlap entry's file list and
	// its reason, HasWhy whether a `why:` was written.
	Uses, Files []string
	Why         string
	HasWhy      bool
}

// SplitDependsOn cuts a DEPENDS-ON: value into its entries: at commas outside parentheses,
// each trimmed, an empty one dropped. A reason can hold commas (`uses: a, b`), so the cut
// is parenthesis-aware.
func SplitDependsOn(value string) []string {
	return splitOutsideParens(value, ',')
}

func splitOutsideParens(s string, sep rune) []string {
	var out []string
	depth, from := 0, 0
	flush := func(end int) {
		if part := strings.TrimSpace(s[from:end]); part != "" {
			out = append(out, part)
		}
	}
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth = max(depth-1, 0)
		case sep:
			if depth == 0 {
				flush(i)
				from = i + len(string(r))
			}
		}
	}
	flush(len(s))
	return out
}

// DependsOnEntryID is the card id of one entry: its text up to an opening parenthesis,
// trimmed.
func DependsOnEntryID(entry string) string {
	if cut, _, ok := strings.Cut(entry, "("); ok {
		entry = cut
	}
	return strings.TrimSpace(entry)
}

// isNoNeed says an entry id is no card: empty, `-`, `none`, or an owner/repo#n reference
// (a pull request or an issue).
func isNoNeed(id string) bool {
	return id == "" || id == "-" || strings.EqualFold(id, "none") || strings.Contains(id, "#")
}

// DependsOnIDsOf is the card ids a DEPENDS-ON: value names, in order, duplicates kept
// (a duplicate is the caller's to catch): each entry's id, with `none`, `-` and an
// owner/repo#n reference no need.
func DependsOnIDsOf(value string) []string {
	var out []string
	for _, e := range SplitDependsOn(value) {
		if id := DependsOnEntryID(e); !isNoNeed(id) {
			out = append(out, id)
		}
	}
	return out
}

// DependsOnIDs is the needs a brief names: the first `Needs:` line, else the DEPENDS-ON: line
// of its typed header block, read as the add and brief verbs read them.
func DependsOnIDs(brief string) []string {
	_, value, _, _ := DependsOnLine([]byte(brief))
	return DependsOnIDsOf(value)
}

// DependsOnLine is the line a brief names its needs on, as the readers take it: the first
// `Needs:` header line anywhere (cardhdr.KeyValue), else the DEPENDS-ON: line of the typed
// header block. line is 1-based; ok is false when the brief has neither.
func DependsOnLine(raw []byte) (key, value string, line int, ok bool) {
	for i, l := range strings.Split(string(raw), "\n") {
		if k, v, isKV := cardhdr.KeyValue(l); isKV && strings.EqualFold(k, "needs") {
			return k, v, i + 1, true
		}
	}
	block, _ := cardHeaderBlock(raw)
	if f := block["DEPENDS-ON"]; f.found {
		return "DEPENDS-ON", f.value, f.line, true
	}
	return "", "", 0, false
}

// ParseDependsEntry reads one entry: its id, and the parenthesis after it as `uses:` or
// `overlap:` with `why:`; Form is "" when the parenthesis is absent or opens with neither.
func ParseDependsEntry(entry string) DependsEntry {
	e := DependsEntry{Text: strings.TrimSpace(entry), ID: DependsOnEntryID(entry)}
	_, rest, ok := strings.Cut(entry, "(")
	if !ok {
		return e
	}
	inner, depth := rest, 1
	for i, r := range rest {
		if r == '(' {
			depth++
		} else if r == ')' {
			if depth--; depth == 0 {
				inner = rest[:i]
				break
			}
		}
	}
	inner = strings.TrimSpace(inner)
	lower := strings.ToLower(inner)
	switch {
	case strings.HasPrefix(lower, "uses:"):
		e.Form = "uses"
		for _, t := range splitOutsideParens(inner[len("uses:"):], ',') {
			if t = strings.Trim(strings.TrimSpace(t), "`'\""); t != "" {
				e.Uses = append(e.Uses, t)
			}
		}
	case strings.HasPrefix(lower, "overlap:"):
		e.Form = "overlap"
		body := inner[len("overlap:"):]
		files := body
		if i := strings.Index(strings.ToLower(body), "why:"); i >= 0 {
			files, e.Why, e.HasWhy = body[:i], strings.TrimSpace(body[i+len("why:"):]), true
		}
		for _, f := range splitOutsideParens(strings.TrimRight(strings.TrimSpace(files), "; "), ',') {
			if f = strings.Trim(strings.TrimSpace(f), "`'\""); f != "" {
				e.Files = append(e.Files, cleanEntry(f))
			}
		}
	}
	return e
}

// DependsOverlapCount is how many overlap entries a brief's needs line keeps: the count the
// add, brief and needs set output prints as OVERLAP NEEDS.
func DependsOverlapCount(raw []byte) int {
	_, value, _, ok := DependsOnLine(raw)
	if !ok {
		return 0
	}
	return DependsOverlapCountOf(value)
}

// DependsOverlapCountOf is how many entries of a DEPENDS-ON: value are overlaps.
func DependsOverlapCountOf(value string) int {
	n := 0
	for _, s := range SplitDependsOn(value) {
		if e := ParseDependsEntry(s); !isNoNeed(e.ID) && e.Form == "overlap" {
			n++
		}
	}
	return n
}

// rankPhrases are the words a writer puts in place of a reason, on a DEPENDS-ON line or in a
// sentence naming one of its ids. "after <id> lands" and "once <id> lands" are built per id.
var rankPhrases = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bbest after\b`),
	regexp.MustCompile(`(?i)\bindependent\b`),
	regexp.MustCompile(`(?i)\bnot blocked\b`),
	regexp.MustCompile(`(?i)\bif both are ready\b`),
}

// pathExtRE is a bare file name a thing may be (no slash): a known file extension.
var pathExtRE = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*\.(go|md|tla|cfg|tsv|sql|sh|json|sexp|schema|baseline|mk|c|h|cc|cpp|hpp|rs|py|yml|yaml|toml|txt|mod|sum|csv|lua|js|ts)$`)

// pathShaped says a uses thing is a repository path: it holds a slash, or is a file name with
// a known extension. Any other thing is a symbol, a spelling or a model name.
func pathShaped(t string) bool {
	return strings.Contains(t, "/") || pathExtRE.MatchString(t)
}

// producedPath says one of the needed card's PATHS and NEW entries names the path t: the
// gate's reading of an entry (diffcheck.Covers), or a directory t holding an entry.
func producedPath(globs []string, t string) bool {
	t = cleanEntry(t)
	return slices.ContainsFunc(globs, func(g string) bool {
		g = cleanEntry(g)
		return g == t || diffcheck.Covers(g, t) || strings.HasPrefix(g, t+"/")
	})
}

// wholeWord says text holds thing as a whole word or phrase: its words in order with any
// blanks between them, and no letter, digit or underscore touching either end that is itself
// a word character.
func wholeWord(text, thing string) bool {
	words := strings.Fields(thing)
	if len(words) == 0 {
		return false
	}
	for i := range words {
		words[i] = regexp.QuoteMeta(words[i])
	}
	pat := strings.Join(words, `\s+`)
	if isWordByte(thing[0]) {
		pat = `(?:^|[^A-Za-z0-9_])` + pat
	}
	if isWordByte(thing[len(thing)-1]) {
		pat += `(?:$|[^A-Za-z0-9_])`
	}
	return regexp.MustCompile(pat).MatchString(text)
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// headerBodyStart is the 1-based line the typed header block ends at: the first line under
// the contract line that is not blank and not a `KEY: value` line (the block's own rule,
// cardHeaderBlock). A brief that is all header has no body: the line past its end.
func headerBodyStart(raw []byte) int {
	lines := strings.Split(string(raw), "\n")
	for i := 1; i < len(lines); i++ {
		l := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		if !headerKeyRE.MatchString(l) {
			return i + 1
		}
	}
	return len(lines) + 1
}

// producedText is what a needed brief offers a symbol, a spelling or a model name: its PATHS
// and NEW values and its task text (the body under the header).
func producedText(raw []byte) string {
	paths, _ := CardHeaderValue(raw, "PATHS")
	news, _ := CardHeaderValue(raw, "NEW")
	lines := strings.Split(string(raw), "\n")
	start := headerBodyStart(raw)
	body := ""
	if start-1 < len(lines) {
		body = strings.Join(lines[start-1:], "\n")
	}
	return paths + "\n" + news + "\n" + body
}

// sentenceEndRE ends a sentence: a stop, a bang or a question mark before a blank or the end.
var sentenceEndRE = regexp.MustCompile(`[.!?](?:\s+|$)`)

// bodySentence is one sentence of a brief's body and the line it sits on.
type bodySentence struct {
	line int
	text string
}

// bodySentences cuts the body (the lines from start) into sentences, line by line.
func bodySentences(raw []byte, start int) []bodySentence {
	var out []bodySentence
	for i, l := range strings.Split(string(raw), "\n") {
		if i+1 < start {
			continue
		}
		l = strings.TrimRight(l, "\r")
		from := 0
		for _, m := range sentenceEndRE.FindAllStringIndex(l, -1) {
			if s := strings.TrimSpace(l[from:m[0]]); s != "" {
				out = append(out, bodySentence{line: i + 1, text: s})
			}
			from = m[1]
		}
		if s := strings.TrimSpace(l[from:]); s != "" {
			out = append(out, bodySentence{line: i + 1, text: s})
		}
	}
	return out
}

// namesID says text names the card id as a whole token: ids hold letters, digits, hyphens,
// dots, underscores and tildes, so a longer id that begins with this one is not it.
func namesID(text, id string) bool {
	re := regexp.MustCompile(`(?:^|[^A-Za-z0-9._~-])` + regexp.QuoteMeta(id) + `(?:$|[^A-Za-z0-9_~.-]|\.(?:[^A-Za-z0-9]|$))`)
	return re.MatchString(text)
}

// rankWords are the rank phrases text holds that stand in place of a reason for the id: the
// four plain phrases, and "after <id> lands" or "once <id> lands" unless the id's entry gave
// a uses (then the sentence only tells what the uses is).
func rankWords(text, id string, hasUses bool) []string {
	var found []string
	for _, re := range rankPhrases {
		if m := re.FindString(text); m != "" {
			found = append(found, strings.ToLower(m))
		}
	}
	if !hasUses {
		re := regexp.MustCompile(`(?i)\b(?:after|once)\s+` + regexp.QuoteMeta(id) + `\s+lands\b`)
		if m := re.FindString(text); m != "" {
			found = append(found, strings.ToLower(m))
		}
	}
	return found
}

// LintDependsOn holds a brief's needs line to the grammar above. needed gives the brief of a
// card the line names (the same add first, then the table); ok is false for a card neither
// holds. Each finding is its own CardHeaderFinding under the token depends-uses, or
// depends-unknown for an id needed cannot find (never a pass), and its excerpt names the
// entry, the reason and the line, and ends with the remedy. It refuses:
//
//	(a) an entry with neither `uses:` nor `overlap:`;
//	(b) a `uses:` thing the needed card does not produce: a path must match its NEW or PATHS
//	    (CardPaths, the gate's reading of a glob); a symbol, spelling or model name must be a
//	    whole word of its NEW, PATHS or task text;
//	(c) an `overlap:` with no `why:`, or whose file is not in both cards' PATHS or NEW;
//	(d) rank words on the DEPENDS-ON line, or in a sentence of the body naming an id on it:
//	    "best after", "independent", "not blocked", "if both are ready", and "after <id>
//	    lands" or "once <id> lands" for an id whose entry gave no uses. A paragraph naming no
//	    id of the line draws nothing.
//
// A brief with no needs line, or `none` or `-`, draws nothing.
func LintDependsOn(raw []byte, needed func(id string) (brief []byte, ok bool)) []CardHeaderFinding {
	key, value, line, ok := DependsOnLine(raw)
	if !ok {
		return nil
	}
	var out []CardHeaderFinding
	add := func(check string, at int, entry, why, remedy string) {
		out = append(out, CardHeaderFinding{Check: check, Line: at, Excerpt: fmt.Sprintf("%s entry %q: %s; remedy: %s", key, entry, why, remedy)})
	}
	host := CardPaths(raw)
	var entries []DependsEntry
	for _, s := range SplitDependsOn(value) {
		if e := ParseDependsEntry(s); !isNoNeed(e.ID) {
			entries = append(entries, e)
		}
	}
	hasUses := map[string]bool{}
	for _, e := range entries {
		hasUses[e.ID] = hasUses[e.ID] || e.Form == "uses"
	}
	// (d), on the line itself
	for _, e := range entries {
		if words := rankWords(value, e.ID, hasUses[e.ID]); len(words) > 0 {
			add(DependsUsesCheck, line, e.Text, fmt.Sprintf("the %s line holds the rank word %q, which is no reason", key, words[0]), DependsRemedy)
			break
		}
	}
	for _, e := range entries {
		var brief []byte
		found := false
		if needed != nil {
			brief, found = needed(e.ID)
		}
		if !found {
			add(DependsUnknownCheck, line, e.Text, fmt.Sprintf("%s is no card of this add and none on the table", e.ID), DependsUnknownRemedy)
		}
		switch e.Form {
		case "":
			add(DependsUsesCheck, line, e.Text, "it names neither `uses:` nor `overlap:`, so nothing says what it consumes", DependsRemedy)
		case "uses":
			if len(e.Uses) == 0 {
				add(DependsUsesCheck, line, e.Text, "`uses:` names no thing", DependsRemedy)
			}
			if !found {
				continue
			}
			if strings.TrimSpace(string(brief)) == "" {
				add(DependsUsesCheck, line, e.Text, e.ID+" has no brief, so nothing it produces is known", DependsRemedy)
				continue
			}
			globs, text := CardPaths(brief), producedText(brief)
			for _, t := range e.Uses {
				switch {
				case pathShaped(t) && !producedPath(globs, t):
					add(DependsUsesCheck, line, e.Text, fmt.Sprintf("uses %q, a path that no PATHS or NEW entry of %s names", t, e.ID), DependsRemedy)
				case !pathShaped(t) && !wholeWord(text, t):
					add(DependsUsesCheck, line, e.Text, fmt.Sprintf("uses %q, which appears nowhere in the NEW, PATHS or task text of %s", t, e.ID), DependsRemedy)
				}
			}
		case "overlap":
			switch {
			case !e.HasWhy || len(strings.Fields(e.Why)) < 2:
				add(DependsUsesCheck, line, e.Text, "`overlap:` gives no `why:` in words", DependsRemedy)
			case len(e.Files) == 0:
				add(DependsUsesCheck, line, e.Text, "`overlap:` names no file", DependsRemedy)
			}
			if !found {
				continue
			}
			their := CardPaths(brief)
			for _, f := range e.Files {
				if !producedPath(host, f) || !producedPath(their, f) {
					add(DependsUsesCheck, line, e.Text, fmt.Sprintf("overlap file %q is not in both cards' PATHS or NEW", f), DependsRemedy)
				}
			}
		}
	}
	// (d), in the body: a sentence that names an id of the line
	for _, s := range bodySentences(raw, headerBodyStart(raw)) {
		done := map[string]bool{}
		for _, e := range entries {
			if done[e.ID] || !namesID(s.text, e.ID) {
				continue
			}
			done[e.ID] = true
			if words := rankWords(s.text, e.ID, hasUses[e.ID]); len(words) > 0 {
				add(DependsUsesCheck, s.line, e.Text, fmt.Sprintf("line %d names %s and holds the rank word %q, which is no reason: %q", s.line, e.ID, words[0], cutRunes(s.text, 100)), DependsRemedy)
			}
		}
	}
	return out
}

// cutRunes is s cut to at most n runes, with an ellipsis when it was cut.
func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// quoteDepends names each refused entry, so the drift says which token it refused.
func quoteDepends(ids []string) string {
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = fmt.Sprintf("%q", id)
	}
	return strings.Join(q, ", ")
}

// firstLine is a text's first line, without its line ending: the contract line the
// card's own id and base are read from.
func firstLine(raw []byte) string {
	line, _, _ := bytes.Cut(raw, []byte("\n"))
	return strings.TrimRight(string(line), "\r")
}
