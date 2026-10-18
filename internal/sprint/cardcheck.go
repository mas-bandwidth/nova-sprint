package sprint

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-tools/pkg/diffcheck"
	"github.com/mas-bandwidth/nova-tools/pkg/swarm"
)

// The core pre-start card check (v1.2.17, start-refuses-until-a-clean-check; docs/SPEC-SPRINT.md
// section 14, "The card check"): nova-sprint check cards reads the epoch's work table and its
// streams and runs the core kinds over them, and start refuses until a clean check record whose
// digest matches the cards now (tla/MachineReady.tla Check, StartOnlyChecked). Everything here is
// pure, over a *Snapshot and the base trees the caller hands in, so a unit test runs no git.

// The core kinds, in the order the check prints them (the numbers of the v1.3 card
// nova-sprint-check-gates-the-start).
const (
	CheckRoutes            = "routes"             // applied provider rows and funded route coverage
	CheckNeeds             = "needs"              // 1: every need names a card of this epoch, not dropped; no self-need, no cycle
	CheckFiles             = "files"              // 3: no file of one repository written by two streams; a path ending in .md is none
	CheckSameFile          = "same-file"          // 5: no two cards of a stream name one PATHS file with no need between them; a path ending in .md is none
	CheckBases             = "bases"              // 6: each stream on its own branch, never the trunk; no need across two bases
	CheckNames             = "names"              // 7: each stream's display name set and readable
	CheckStaleFix          = "stale-fix"          // 8: no card at priority fix with no failed attempt this epoch
	CheckStaleHead         = "stale-head"         // a pushed head from an attempt before the current brief
	CheckLint              = "lint"               // 9: every brief passes the card lint
	CheckBriefSize         = "brief-size"         // 16: every brief within the store's bound and the drain's
	CheckSentinelWide      = "sentinel-wide"      // sentinel waits on producers sharing no PATHS/NEW glob with consumer
	CheckSentinelConsumers = "sentinel-consumers" // missing consumer from sentinel CONSUMERS line
	CheckOwner             = "owner"              // who=friend card missing WHO: or ran on fleet
	CheckFailedInReview    = "failed-in-review"   // card in review with last finish failed
)

// CheckKinds is the core kinds in print order.
var CheckKinds = []string{
	CheckNeeds,
	CheckFiles,
	CheckSameFile,
	CheckBases,
	CheckNames,
	CheckStaleFix,
	CheckStaleHead,
	CheckLint,
	CheckBriefSize,
	CheckSentinelWide,
	CheckSentinelConsumers,
	CheckOwner,
	CheckFailedInReview,
	CheckRoutes,
}

// DrainBriefBytes is the drain's bound on one brief field (a brief over 16 KB poisoned the drain queue
// on 2026-10-11): a card whose brief is over it is a finding of kind brief-size.
const DrainBriefBytes = 16384

// The severity of a finding. A refusing finding fails the check and start; a warning is
// printed and recorded and fails neither. A shared file (files, same-file) is always a
// warning and never an order: a need is only for code that builds on other code, and a
// shared file is worked out in the merge (Glenn, 2026-10-15). Bases, names, needs and the
// other kinds still refuse.
//
// overlapWarn is the warning's serialized words. It names no wait path.
const (
	SeverityRefuse = "refuse"
	SeverityWarn   = "warn"

	overlapWarn   = "work it out in the merge"
	overlapRemedy = "work the overlap out in the merge; add a need only when one card's code builds on the other's"
)

// CardFinding is one finding of the card check: its kind, the card or stream it is about, what
// was found, what to do, its severity, and for a warning what serializes it. It prints as one
// line,
//
//	CHECK <kind> <subject> <finding> remedy=<remedy>
//	CHECK <kind> <subject> <finding> WARN serialized=<why> remedy=<remedy>
type CardFinding struct {
	Kind       string `json:"kind"`
	Subject    string `json:"subject"`
	Finding    string `json:"finding"`
	Remedy     string `json:"remedy"`
	Severity   string `json:"severity"`
	Serialized string `json:"serialized,omitempty"`
}

// refusing is a finding that fails the check and start.
func refusing(kind, subject, finding, remedy string) CardFinding {
	return CardFinding{Kind: kind, Subject: subject, Finding: finding, Remedy: remedy, Severity: SeverityRefuse}
}

// warning is a finding that warns and fails neither the check nor start.
func warning(kind, subject, finding, remedy, serialized string) CardFinding {
	return CardFinding{Kind: kind, Subject: subject, Finding: finding, Remedy: remedy, Severity: SeverityWarn, Serialized: serialized}
}

// Refuses says the finding fails the check and start: every finding but a warning, one with
// no severity included.
func (f CardFinding) Refuses() bool { return f.Severity != SeverityWarn }

// Line is the finding's one line.
func (f CardFinding) Line() string {
	if !f.Refuses() {
		return "CHECK " + f.Kind + " " + f.Subject + " " + f.Finding + " WARN serialized=" + f.Serialized + " remedy=" + f.Remedy
	}
	return "CHECK " + f.Kind + " " + f.Subject + " " + f.Finding + " remedy=" + f.Remedy
}

// RefusingFindings is the findings that fail the check and start, in their order.
func RefusingFindings(fs []CardFinding) []CardFinding {
	var out []CardFinding
	for _, f := range fs {
		if f.Refuses() {
			out = append(out, f)
		}
	}
	return out
}

// WarningFindings is the findings that warn and fail nothing, in their order.
func WarningFindings(fs []CardFinding) []CardFinding {
	var out []CardFinding
	for _, f := range fs {
		if !f.Refuses() {
			out = append(out, f)
		}
	}
	return out
}

// CardTreeKey names one base tree: the repository a brief's REPO line names and its BASE ref.
type CardTreeKey struct{ Repo, Base string }

// CardCheckIn is what the check reads besides the snapshot: the streams it is limited to (none
// is every stream), the base tree of each (repository, base) the open cards name (CardBasesToRead;
// one not handed over, or Missing, compares its PATHS as written), the card lint (the lint brief
// check runs; nil lints nothing) and the store's bound on a brief.
type CardCheckIn struct {
	Streams       []string
	Trees         map[CardTreeKey]AdmissionTree
	Lint          func(brief string) []string
	MaxBriefBytes int
}

// cardCheck is one run's view: the snapshot, the cards it checks, and their streams.
type cardCheck struct {
	s       *Snapshot
	in      CardCheckIn
	open    []*Card // placed, not landed, in a stream checked, in id order
	streams []string
}

// CheckCards runs the core kinds over the snapshot's work table and streams and returns every
// finding, in kind order then subject. The snapshot carries the work table (every placed card,
// and the records the cards' needs name) and the merge table (the streams' control cards).
// docs/SPEC-SPRINT.md section 14, "The card check"; tla/MachineReady.tla Check.
func CheckCards(s *Snapshot, in CardCheckIn) []CardFinding {
	c := &cardCheck{s: s, in: in}
	seen := map[string]bool{}
	for _, card := range s.Work.Cards() {
		if !card.Placed() || card.Col == Landed || !c.inStreams(card.Row) {
			continue
		}
		c.open = append(c.open, card)
		if !seen[card.Row] {
			seen[card.Row] = true
			c.streams = append(c.streams, card.Row)
		}
	}
	sort.Strings(c.streams)
	var out []CardFinding
	out = append(out, c.needs()...)
	out = append(out, c.files()...)
	out = append(out, c.sameFile()...)
	out = append(out, c.bases()...)
	out = append(out, c.names()...)
	out = append(out, c.staleFix()...)
	out = append(out, c.staleHead()...)
	out = append(out, c.lint()...)
	out = append(out, c.briefSize()...)
	out = append(out, c.sentinelWide()...)
	out = append(out, c.sentinelConsumers()...)
	out = append(out, c.owner()...)
	out = append(out, c.failedInReview()...)
	out = append(out, ProviderFindings(s)...)
	return out
}

// staleHead is a pushed head made under an earlier brief: the primary's head or its latest
// pushed attempt is stale unless the card was explicitly kept with brief --keep-head.
func (c *cardCheck) staleHead() []CardFinding {
	var out []CardFinding
	for _, card := range c.open {
		if IsSentinel(card) || card.F("fix") != "" || card.F(FieldFindingAttempt) != "" {
			continue
		}
		head, attempt := PushedHead(card), 0
		var latestHead string
		latestAttempt := 0
		for _, work := range c.s.Work.Of(card.ID) {
			// A brief replacement that bounced this attempt keeps its raw head for audit
			// but marks it superseded; it no longer holds a usable head.
			if work.F("head_superseded") == "true" {
				continue
			}
			n, h := work.Int("attempt"), PushedHead(work)
			if h == "" {
				continue
			}
			if n >= latestAttempt {
				latestHead, latestAttempt = h, n
			}
			if h == head && n > attempt {
				attempt = n
			}
		}
		if head == "" {
			head, attempt = latestHead, latestAttempt
		} else if attempt == 0 {
			attempt = card.Int("attempt")
		}
		if head == "" || !AttemptPredatesBrief(card, attempt) {
			continue
		}
		out = append(out, refusing(CheckStaleHead, card.ID,
			fmt.Sprintf("head %s of attempt %d was made under an earlier brief", head, attempt),
			"nova-sprint rework "+card.ID+" --fix 'brief replaced'"))
	}
	return out
}

func (c *cardCheck) inStreams(stream string) bool {
	return len(c.in.Streams) == 0 || slices.Contains(c.in.Streams, stream)
}

// needs is kind 1: every unresolved need of an open card (TableUnresolved: absent, dropped,
// unknown stream, held, cycle; needs_resolve.go), one finding a card and need,
//
//	CHECK needs <card> <need> <reason>: <words> remedy=<the verbs that decide it>
//
// A waived need is no need. tla/NeedsBlocked.tla Unresolved
func (c *cardCheck) needs() []CardFinding {
	all := TableUnresolved(c.s)
	var out []CardFinding
	for _, card := range c.open {
		for _, u := range all[card.ID] {
			out = append(out, refusing(CheckNeeds, card.ID, u.Need+" "+u.Reason+": "+u.Words, strings.Join(u.Decide, " or ")))
		}
	}
	return out
}

// pathsOf is a brief's PATHS entries as written: commas or blanks between them; none and -
// name no file.
func pathsOf(brief string) []string {
	value, _ := swarm.CardHeaderValue([]byte(brief), "PATHS")
	var out []string
	for _, p := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if p = admitClean(p); p != "" && p != "none" && p != "-" {
			out = append(out, p)
		}
	}
	return out
}

// everyCardWrites says p is a file every card may write: a ledger the lander merges, a test, a
// TLA+ ledger (cardgen.AlwaysInPaths, cardgen.LedgerInPaths). Two cards naming it never collide.
func everyCardWrites(p string) bool {
	return cardgen.AlwaysInPaths(p) || cardgen.LedgerInPaths(p)
}

// CardTreeKeyOf is the base tree a brief names, and whether it names one (a REPO and a BASE).
func CardTreeKeyOf(brief string) (CardTreeKey, bool) {
	cb := swarm.ReadCardBase([]byte(brief))
	if cb.Named == "" || cb.Ref == "" {
		return CardTreeKey{}, false
	}
	return CardTreeKey{Repo: cb.Named, Base: cb.Ref}, true
}

// CardBasesToRead is the base of every (repository, base) the open cards of the streams
// checked name, one each, in key order: the trees the caller reads for kind 3.
func CardBasesToRead(s *Snapshot, streams []string) []swarm.CardBase {
	seen := map[CardTreeKey]bool{}
	var keys []CardTreeKey
	bases := map[CardTreeKey]swarm.CardBase{}
	for _, card := range s.Work.Cards() {
		if !card.Placed() || card.Col == Landed || (len(streams) > 0 && !slices.Contains(streams, card.Row)) {
			continue
		}
		k, ok := CardTreeKeyOf(card.F("brief"))
		if !ok || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
		bases[k] = swarm.ReadCardBase([]byte(card.F("brief")))
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].Repo < keys[j].Repo || keys[i].Repo == keys[j].Repo && keys[i].Base < keys[j].Base
	})
	out := make([]swarm.CardBase, len(keys))
	for i, k := range keys {
		out[i] = bases[k]
	}
	return out
}

// written is what one card writes: the files its PATHS cover at its base tree, and the entries
// no tree was read for (or that match no file there), kept as written.
func (c *cardCheck) written(card *Card) (files map[string]bool, patterns []string) {
	files = map[string]bool{}
	brief := card.F("brief")
	tree, haveTree := AdmissionTree{}, false
	if k, ok := CardTreeKeyOf(brief); ok {
		tree, haveTree = c.in.Trees[k]
		haveTree = haveTree && tree.Missing == ""
	}
	for _, e := range pathsOf(brief) {
		matched := false
		if haveTree {
			for _, f := range tree.Files {
				if diffcheck.Covers(e, f) {
					files[f], matched = true, true
				}
			}
		}
		switch {
		case matched:
		case !strings.ContainsAny(e, "*?["):
			files[e] = true // a literal: a file the card creates, or one no tree was read for
		default:
			patterns = append(patterns, e)
		}
	}
	return files, patterns
}

// files is kind 3: a file of one repository written by two streams is a warning, never a
// failure, and it orders nothing. The key is the brief's named repository
// (ReadCardBase.Named, the REPO: or base-repo: line) plus the path. The same path in two
// repositories is two files. A path ending in .md is no finding: docs merge trivially.
// Each stream's other files are the union of its open cards' PATHS, expanded at their base
// tree (the tree add's admission reads); a file every card may write (everyCardWrites) is
// no finding.
func (c *cardCheck) files() []CardFinding {
	type side struct {
		files    map[string]bool // repo + NUL + path
		patterns []string        // repo + NUL + pattern
	}
	by := map[string]*side{}
	for _, card := range c.open {
		sd := by[card.Row]
		if sd == nil {
			sd = &side{files: map[string]bool{}}
			by[card.Row] = sd
		}
		repo := swarm.ReadCardBase([]byte(card.F("brief"))).Named
		fs, ps := c.written(card)
		for f := range fs {
			sd.files[repo+"\x00"+f] = true
		}
		for _, p := range ps {
			sd.patterns = append(sd.patterns, repo+"\x00"+p)
		}
	}
	var out []CardFinding
	said := map[string]bool{}
	pathOf := func(key string) string {
		_, path, _ := strings.Cut(key, "\x00")
		return path
	}
	repoOf := func(key string) string {
		repo, _, _ := strings.Cut(key, "\x00")
		return repo
	}
	clash := func(a, b, key string) {
		f := pathOf(key)
		if everyCardWrites(f) || strings.HasSuffix(f, ".md") {
			return
		}
		seen := a + "\x00" + b + "\x00" + key
		if said[seen] {
			return
		}
		said[seen] = true
		out = append(out, warning(CheckFiles, a, f+" is written by streams "+a+" and "+b, overlapRemedy, overlapWarn))
	}
	covers := func(patternKey, fileKey string) bool {
		return repoOf(patternKey) == repoOf(fileKey) && diffcheck.Covers(pathOf(patternKey), pathOf(fileKey))
	}
	for i, a := range c.streams {
		for _, b := range c.streams[i+1:] {
			sa, sb := by[a], by[b]
			for _, fk := range checkFilesSorted(sa.files) {
				if sb.files[fk] || slices.ContainsFunc(sb.patterns, func(p string) bool { return covers(p, fk) }) {
					clash(a, b, fk)
				}
			}
			for _, pk := range sa.patterns {
				for _, fk := range checkFilesSorted(sb.files) {
					if covers(pk, fk) {
						clash(a, b, fk)
					}
				}
				for _, qk := range sb.patterns {
					if repoOf(pk) != repoOf(qk) {
						continue
					}
					p, q := pathOf(pk), pathOf(qk)
					if p == q || diffcheck.Covers(p, q) || diffcheck.Covers(q, p) {
						clash(a, b, qk)
					}
				}
			}
		}
	}
	return out
}

func checkFilesSorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// reaches says from needs to through the stored needs of the work table (named needs, not
// waived), at any depth.
func (c *cardCheck) reaches(from, to string) bool {
	seen := map[string]bool{}
	stack := []string{from}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		card := c.s.Work.Card(id)
		waived := Split(card.F("waived"))
		for _, n := range Split(card.F("needs")) {
			if contains(waived, n) {
				continue
			}
			if n == to {
				return true
			}
			stack = append(stack, n)
		}
	}
	return false
}

// sameFile is kind 5: two open cards of one stream that name one PATHS file with no need between
// them, either way. It is a warning and it orders nothing: a need is only for code that builds
// on other code, and the overlap is worked out in the merge. A file every card may write is
// no finding. A path ending in .md is no finding. A pair that already has a need either way
// is not a finding: that need was chosen because the code builds on the other card.
func (c *cardCheck) sameFile() []CardFinding {
	var out []CardFinding
	for _, stream := range c.streams {
		named := map[string][]string{}
		var order []string
		for _, card := range c.open {
			if card.Row != stream || IsSentinel(card) {
				continue
			}
			for _, p := range pathsOf(card.F("brief")) {
				if everyCardWrites(p) || strings.HasSuffix(p, ".md") || contains(named[p], card.ID) {
					continue
				}
				if len(named[p]) == 0 {
					order = append(order, p)
				}
				named[p] = append(named[p], card.ID)
			}
		}
		sort.Strings(order)
		for _, p := range order {
			ids := named[p]
			for i, a := range ids {
				for _, b := range ids[i+1:] {
					if c.reaches(a, b) || c.reaches(b, a) {
						continue
					}
					out = append(out, warning(CheckSameFile, a, p+" is named in PATHS by "+a+" and "+b,
						overlapRemedy, overlapWarn))
				}
			}
		}
	}
	return out
}

// StreamBranchOf is the branch a stream lands on: its control card's stream branch, else
// sprint/<stream> (branchAndTrunk's default).
func StreamBranchOf(s *Snapshot, stream string) string {
	if b := s.StreamCtl(stream).F(FieldStreamBranch); b != "" {
		return b
	}
	return "sprint/" + stream
}

// bases is kind 6: each stream lands on its own branch, never the trunk (dev, main) and never
// another stream's (the promotion stream lands on the protected branch by its mark, and is no
// finding); a need across two streams on different branches is a finding unless the needed card
// is a sentinel of the producer stream (its stop, which the producer lands through).
func (c *cardCheck) bases() []CardFinding {
	var out []CardFinding
	owner := map[string]string{}
	for _, stream := range c.s.Streams() {
		if c.s.StreamCtl(stream) == nil || IsPromotionStream(c.s, stream) {
			continue
		}
		b := StreamBranchOf(c.s, stream)
		if _, ok := owner[b]; !ok {
			owner[b] = stream
		}
	}
	for _, stream := range c.streams {
		if IsPromotionStream(c.s, stream) {
			continue
		}
		b := StreamBranchOf(c.s, stream)
		switch {
		case slices.Contains(ProtectedBranches, b):
			out = append(out, refusing(CheckBases, stream, "lands on the trunk "+b+", not on a branch of its own", "nova-sprint stream set "+stream+" --base sprint/"+stream))
		case owner[b] != "" && owner[b] != stream:
			out = append(out, refusing(CheckBases, stream, "lands on "+b+", the branch of stream "+owner[b], "nova-sprint stream set "+stream+" --base sprint/"+stream))
		}
	}
	for _, card := range c.open {
		for _, n := range Split(card.F("needs")) {
			nc := c.s.Work.Card(n)
			if nc == nil || nc.Row == "" || nc.Row == card.Row {
				continue
			}
			from, to := StreamBranchOf(c.s, card.Row), StreamBranchOf(c.s, nc.Row)
			if from == to || IsSentinel(nc) {
				continue
			}
			out = append(out, refusing(CheckBases, card.ID, "needs "+n+" of stream "+nc.Row+", which lands on "+to+", not on "+from,
				"move "+card.ID+" or "+n+" so both are in one stream, or need the producer stream's sentinel: nova-sprint needs set "+card.ID+" --needs <its sentinel>"))
		}
	}
	return out
}

// hyphenatedID is a display name that is an id, not a name: lower case words joined by hyphens.
var hyphenatedID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9.]+)+$`)

// names is kind 7: each stream checked has a display name (stream set --display), readable: not
// empty, not the stream's id, not a hyphenated id.
func (c *cardCheck) names() []CardFinding {
	var out []CardFinding
	for _, stream := range c.streams {
		d := strings.TrimSpace(c.s.StreamCtl(stream).F(FieldDisplay))
		remedy := "nova-sprint stream set " + stream + " --display '<a name a person reads>'"
		switch {
		case d == "":
			out = append(out, refusing(CheckNames, stream, "has no display name", remedy))
		case d == stream:
			out = append(out, refusing(CheckNames, stream, "is displayed as its id", remedy))
		case hyphenatedID.MatchString(d):
			out = append(out, refusing(CheckNames, stream, "is displayed as "+d+", a hyphenated id", remedy))
		}
	}
	return out
}

// staleFix is kind 8: a primary at priority fix that has had no attempt this epoch (attempt 0:
// the fix level a rework gives after a failed attempt, carried by a brief or a clear into a card
// that never failed) jumps the ladder with nothing to fix.
func (c *cardCheck) staleFix() []CardFinding {
	var out []CardFinding
	for _, card := range c.open {
		if IsSentinel(card) || card.F(FieldPriority) != PriorityFix || card.Int("attempt") > 0 {
			continue
		}
		out = append(out, refusing(CheckStaleFix, card.ID, "is at priority fix with no failed attempt this epoch",
			"nova-sprint priority "+card.ID+" --high --reason '<why it is high>' (or --normal)"))
	}
	return out
}

// lint is kind 9: every open primary's brief passes the card lint brief check runs (lintBriefReads);
// a primary with no brief is a finding, since a dealt card needs one.
func (c *cardCheck) lint() []CardFinding {
	if c.in.Lint == nil {
		return nil
	}
	var out []CardFinding
	for _, card := range c.open {
		if IsSentinel(card) {
			continue
		}
		brief := card.F("brief")
		remedy := "nova-sprint brief " + card.ID + " --brief-file <a brief that passes: nova-sprint brief check <file>>"
		if strings.TrimSpace(brief) == "" {
			out = append(out, refusing(CheckLint, card.ID, "has no brief", remedy))
			continue
		}
		if fs := c.in.Lint(brief); len(fs) > 0 {
			out = append(out, refusing(CheckLint, card.ID, fmt.Sprintf("fails the card lint (%d): %s", len(fs), strings.Join(fs, "; ")), remedy))
		}
	}
	return out
}

// briefSize is kind 16: every open card's brief within the bound the store and the drain hold
// a brief to (DrainBriefBytes, which is store.MaxBriefBytes; a caller's smaller bound is
// taken). The drain bounds each text field an entry sets, the brief at this bound
// (internal/sprint/store/engine.go unwritableQueued and unwritable, by TextBound), never the
// card's whole record: the attempt, consumer and cost history a card keeps is not in a brief
// and is not measured (v1.2.18: v1.2.17 summed every field against the brief's bound and
// refused cards whose brief was well under it). The store refuses a brief field over its
// bound, so a stored brief over it is an older binary's or a direct write's.
func (c *cardCheck) briefSize() []CardFinding {
	bound := DrainBriefBytes
	if c.in.MaxBriefBytes > 0 && c.in.MaxBriefBytes < bound {
		bound = c.in.MaxBriefBytes
	}
	var out []CardFinding
	for _, card := range c.open {
		if n := len(card.F("brief")); n > bound {
			out = append(out, refusing(CheckBriefSize, card.ID, fmt.Sprintf("brief is %d bytes, over the %d bytes a brief may be", n, bound),
				"nova-sprint brief "+card.ID+" --brief-file <a shorter brief>"))
		}
	}
	return out
}

// CardCheckKinds is the findings counted by kind, in kind order: "needs:1,lint:2".
func CardCheckKinds(fs []CardFinding) string {
	n := map[string]int{}
	for _, f := range fs {
		n[f.Kind]++
	}
	var out []string
	for _, k := range CheckKinds {
		if n[k] > 0 {
			out = append(out, fmt.Sprintf("%s:%d", k, n[k]))
		}
	}
	return strings.Join(out, ",")
}

// CheckedCard is one card as the check record keeps it: what a change to the card the start
// gate must see moves (an admit, a move, a brief, needs, priority or tier edit).
type CheckedCard struct {
	ID       string `json:"id"`
	Stream   string `json:"stream"`
	Tier     string `json:"tier,omitempty"`
	Priority string `json:"priority,omitempty"`
	Needs    string `json:"needs,omitempty"`
	Brief    string `json:"brief"` // the sha256 of its brief, hex
}

// CheckedCardsOf is every card on the work table that is not dropped (placed, any state), in id
// order, as the check record keeps it.
func CheckedCardsOf(s *Snapshot) []CheckedCard {
	var out []CheckedCard
	for _, card := range s.Work.Cards() {
		if !card.Placed() {
			continue
		}
		sum := sha256.Sum256([]byte(card.F("brief")))
		out = append(out, CheckedCard{ID: card.ID, Stream: card.Row, Tier: card.F(FieldTier), Priority: card.F(FieldPriority),
			Needs: card.F("needs"), Brief: hex.EncodeToString(sum[:])})
	}
	return out
}

// CardsDigest is one sha256 over the cards checked: the record's digest, recomputed by start
// from the live table (tla/MachineReady.tla digest).
func CardsDigest(cs []CheckedCard) string {
	h := sha256.New()
	for _, c := range cs {
		fmt.Fprintf(h, "%s\t%s\t%s\t%s\t%s\t%s\n", c.ID, c.Stream, c.Tier, c.Priority, c.Needs, c.Brief)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CardsChange is the first difference between the cards a check record kept and the cards now,
// in id order: "<id> added", "<id> removed", or "<id> changed: <field>"; "" when none.
func CardsChange(was, now []CheckedCard) string {
	i, j := 0, 0
	for i < len(was) || j < len(now) {
		switch {
		case j == len(now) || i < len(was) && was[i].ID < now[j].ID:
			return was[i].ID + " removed"
		case i == len(was) || now[j].ID < was[i].ID:
			return now[j].ID + " added"
		}
		a, b := was[i], now[j]
		// the brief last: needs set rewrites the brief's Needs line with the field
		for _, f := range []struct{ name, a, b string }{
			{"stream (a move)", a.Stream, b.Stream}, {"needs", a.Needs, b.Needs},
			{"priority", a.Priority, b.Priority}, {"tier", a.Tier, b.Tier}, {"brief", a.Brief, b.Brief},
		} {
			if f.a != f.b {
				return a.ID + " changed: " + f.name
			}
		}
		i, j = i+1, j+1
	}
	return ""
}

// ProviderFindings is the shared sprint-wide provider check, also recomputed
// by start after a clean record: applied configuration can change independently
// of the cards' digest. Each malformed provider is named exactly once.
func ProviderFindings(s *Snapshot) []CardFinding {
	out := []CardFinding{}
	if len(s.Providers) == 0 {
		out = append(out, refusing(CheckRoutes, "-", "no provider rows applied", "nova-config apply --kind provider"))
	}
	for _, p := range s.ProviderProblems() {
		out = append(out, refusing(CheckRoutes, "provider:"+p.Name, p.Problem, "nova-config apply --kind provider"))
	}
	routes, tiers := s.ProviderGaps("")
	for _, r := range routes {
		array := append(append([]string{}, s.tierArray(r.Tier)...), r.Route)
		out = append(out, refusing(CheckRoutes, r.Route, "enabled route of funded provider "+r.Provider+" is in no tier array", "nova-config tier set "+r.Tier+" --routes "+strings.Join(array, ",")))
	}
	for _, t := range tiers {
		out = append(out, refusing(CheckRoutes, "tier:"+t, "holds no route of a funded provider", "nova-config tier show "+t))
	}
	return out
}

var parenRE = regexp.MustCompile(`\([^)]*\)`)

// ConsumersOfBrief is the cards that need a sentinel (FieldConsumers): read from
// its CONSUMERS: header line; ok is false when the brief carries no such line.
func ConsumersOfBrief(brief string) ([]string, bool) {
	val, ok := swarm.CardHeaderValue([]byte(brief), "CONSUMERS")
	if !ok {
		return nil, false
	}
	cleaned := parenRE.ReplaceAllString(val, " ")
	var out []string
	for _, f := range strings.FieldsFunc(cleaned, func(r rune) bool {
		return r == ',' || r == ';' || unicode.IsSpace(r)
	}) {
		f = strings.TrimSpace(f)
		if f != "" && f != "-" && f != "none" {
			out = append(out, f)
		}
	}
	return out, true
}

func cardGlobs(brief string) []string {
	var out []string
	for _, p := range swarm.CardPaths([]byte(brief)) {
		p = strings.TrimSpace(p)
		if p == "" || p == "none" || p == "-" || everyCardWrites(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func globsShare(ps, cs []string) bool {
	for _, gp := range ps {
		for _, gc := range cs {
			if gp == gc || diffcheck.Covers(gp, gc) || diffcheck.Covers(gc, gp) {
				return true
			}
		}
	}
	return false
}

// sentinelWide warns on a sentinel whose WaitsFor list includes producers sharing
// no non-ledger PATHS/NEW glob with the consumer.
func (c *cardCheck) sentinelWide() []CardFinding {
	var out []CardFinding
	for _, card := range c.open {
		if IsSentinel(card) {
			continue
		}
		cGlobs := cardGlobs(card.F("brief"))
		for _, n := range Split(card.F("needs")) {
			sentinel := c.s.Work.Placed(n)
			if sentinel == nil || !IsSentinel(sentinel) {
				continue
			}
			var unshared []string
			for _, pid := range WaitsFor(c.s, sentinel, nil) {
				pCard := c.s.Work.Placed(pid)
				if pCard == nil || IsSentinel(pCard) {
					continue
				}
				pGlobs := cardGlobs(pCard.F("brief"))
				if !globsShare(pGlobs, cGlobs) {
					unshared = append(unshared, pid)
				}
			}
			if len(unshared) > 0 {
				slices.Sort(unshared)
				out = append(out, warning(
					CheckSentinelWide,
					sentinel.ID,
					fmt.Sprintf("waits on %d producer(s) sharing no PATHS/NEW glob with consumer %s: %s", len(unshared), card.ID, strings.Join(unshared, ", ")),
					fmt.Sprintf("move %s after %s, or narrower sentinel", strings.Join(unshared, ", "), sentinel.ID),
					"sentinel "+sentinel.ID,
				))
			}
		}
	}
	return out
}

// sentinelConsumers refuses a sentinel whose CONSUMERS header does not name
// every open card that needs it.
func (c *cardCheck) sentinelConsumers() []CardFinding {
	var out []CardFinding
	for _, card := range c.open {
		if !IsSentinel(card) || card.F("brief") == "" {
			continue
		}
		var allConsumers []string
		for _, w := range c.s.Work.Cards() {
			if !w.Placed() || w.Col == Landed || IsSentinel(w) {
				continue
			}
			if slices.Contains(Split(w.F("needs")), card.ID) && !slices.Contains(Split(w.F("waived")), card.ID) {
				allConsumers = append(allConsumers, w.ID)
			}
		}
		slices.Sort(allConsumers)
		declared, ok := ConsumersOfBrief(card.F("brief"))
		var missing []string
		for _, consumer := range allConsumers {
			if !ok || !slices.Contains(declared, consumer) {
				missing = append(missing, consumer)
			}
		}
		if len(missing) > 0 {
			remedy := fmt.Sprintf("nova-sprint brief %s --brief-file <a brief naming every consumer: CONSUMERS: %s>", card.ID, strings.Join(allConsumers, ", "))
			out = append(out, refusing(
				CheckSentinelConsumers,
				card.ID,
				fmt.Sprintf("CONSUMERS line missing %s", strings.Join(missing, ", ")),
				remedy,
			))
		}
	}
	return out
}

// owner refuses a card whose who= is friend lacking a WHO: line in brief,
// or whose attempts all ran on fleet members.
func (c *cardCheck) owner() []CardFinding {
	var out []CardFinding
	for _, card := range c.open {
		if IsSentinel(card) {
			continue
		}
		if card.F(FieldWho) == WhoFriend {
			if WhoOfBrief(card.F("brief")) == "" {
				out = append(out, refusing(
					CheckOwner,
					card.ID,
					"who=friend but brief has no WHO: line",
					"nova-sprint card "+card.ID+" --who fleet (or set WHO: line in brief)",
				))
			} else if card.Int("attempt") > 0 && card.F(FieldRoutes) != "" && len(Split(card.F(FieldRoutes))) >= card.Int("attempt") {
				out = append(out, refusing(
					CheckOwner,
					card.ID,
					fmt.Sprintf("who=friend but attempts ran on fleet members: %s", card.F(FieldRoutes)),
					"nova-sprint card "+card.ID+" --who fleet (or set WHO: line in brief)",
				))
			}
		}
	}
	return out
}

// failedInReview refuses a card in review whose last attempt failed.
func (c *cardCheck) failedInReview() []CardFinding {
	var out []CardFinding
	for _, card := range c.open {
		if card.Col != Review {
			continue
		}
		if card.F("result") == "failed" || strings.HasSuffix(card.F("last_finish"), ":failed") {
			out = append(out, refusing(
				CheckFailedInReview,
				card.ID,
				fmt.Sprintf("attempt %s failed and remains in review", orDash(card.F("attempt"))),
				fmt.Sprintf("rework, redo or drop: nova-sprint rework %s (or redo, drop)", card.ID),
			))
		}
	}
	return out
}
