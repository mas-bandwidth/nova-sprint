package sprint

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-tools/pkg/hygiene"
)

// THE DEAL'S TWO PRECONDITIONS ON BASE (2026-10-24: stream rocketnet-final landed 1 of 20
// first time; the attempts failed on a BASE already red on their own platforms, on PATHS
// files renamed at BASE, and on a parent not landed behind a "Needs: none"). Before the
// tick deals a primary's next attempt it holds the attempt to its BASE as the binding last
// read it (TickReq.BaseCI, TickReq.BaseTrees; the binding reads them off the tick's path,
// cmd/nova-sprint dealfacts.go). The model is tla/DealBase.tla.
//
//   - Main green: a card whose GATE-NEEDS names a platform (linux, windows, macos or
//     darwin, tla, linux-xdp, linux-vm) is not dealt while its BASE's CI is red on that
//     platform at the newest BASE commit whose CI completed. It waits ready, its attempt
//     unspent and no bound counted, under one judgment per BASE that names each waiting
//     card with its reason ("BASE red on <platform>: <failing checks>, at <sha>"), and the
//     tick that reads BASE green deals it (the judgment closes as its cause goes). A card
//     whose KIND is fix-red is the cure, never held by the red it fixes. A BASE with no fact
//     read (the binding has not read it yet, or cannot) holds nothing.
//   - The brief at BASE: every PATHS entry not under NEW names a file (or, for a glob or a
//     directory, at least one file) of BASE's tree; every START entry does too; and every
//     DEPENDS-ON card on the table has landed or is named by the brief's Needs: line (or the
//     card's needs). A brief that fails is not dealt: one judgment per card, a brief defect
//     for the coordinator, naming the exact missing paths and parents; it closes in the tick
//     the brief passes (a brief edit, or the parent landing). A BASE whose tree was not read
//     checks no path; a DEPENDS-ON card with no record on the table is not judged here (an
//     earlier epoch's landing), preflight names it.
//   - An inherited red: a failed finish whose report says its failure is reproduced at BASE
//     (InheritedRedOf) is not the card's failure: the primary goes back to ready, no
//     failure, no rework and no judgment of its own, and the attempt is recorded
//     (FieldInheritedAttempts), so it counts toward neither the attempt cap nor first-time
//     stats; the main-green precondition then holds the next attempt until BASE is green.

// The judgments of the deal's preconditions.
const (
	// NBaseRedWait is BASE red on a platform a ready card's GATE-NEEDS names: the cards
	// wait ready, undealt, until BASE is green there. One per BASE (BaseSubject).
	NBaseRedWait = "cards wait for their BASE to be green on their platforms"
	// NBriefPreflight is a ready card whose brief fails against BASE: a PATHS or START file
	// missing at BASE, or a DEPENDS-ON parent not landed and not in Needs. One per card.
	NBriefPreflight = "a brief fails its preflight against BASE"
	// NInheritedRed is the happened note of a failed finish whose failure is BASE's.
	NInheritedRed = "an attempt failed on a red inherited from BASE"
)

// FieldInheritedAttempts is the primary's attempts that failed on a red inherited from
// BASE, comma separated: they count toward no bound and no first-time stat.
const FieldInheritedAttempts = "inherited_attempts"

func init() {
	TickDecisions[NBaseRedWait] = []string{"act", "wait"} // act: fix BASE (or a fix-red card); the cards deal by themselves when it is green
	TickDecisions[NBriefPreflight] = []string{"brief", "look at the card", "drop"}
	WaitRefused[NBriefPreflight] = "brief the card again with PATHS, START and Needs that hold at BASE (nova-sprint brief <id> --brief-file <f>), or drop it"
}

// BaseSubject is the subject of a BASE's judgments: base:<owner/name>@<branch>.
func BaseSubject(repo, base string) string { return "base:" + RepoName(repo) + "@" + base }

// BaseCheck is one failing check of a BASE commit and the platforms it runs on.
type BaseCheck struct {
	Name      string
	Platforms []string
}

// BaseTree is BASE's tree at its tip as the binding read it: every file path, sorted.
type BaseTree struct {
	Repo  string // owner/name
	Base  string // the branch
	Sha   string // the tip the files are of
	Files []string
}

// has says the tree holds the entry: the file, a file under it as a directory, or a file
// a glob matches.
func (t *BaseTree) has(entry string) bool {
	entry = strings.TrimPrefix(strings.TrimSpace(entry), "./")
	if entry == "" {
		return true
	}
	if strings.ContainsAny(entry, "*?[") {
		for _, f := range t.Files {
			if hygiene.MatchGlob(entry, f) {
				return true
			}
		}
		return false
	}
	dir := strings.TrimSuffix(entry, "/")
	i := sort.SearchStrings(t.Files, dir)
	if i < len(t.Files) && t.Files[i] == dir {
		return true
	}
	j := sort.SearchStrings(t.Files, dir+"/")
	return j < len(t.Files) && strings.HasPrefix(t.Files[j], dir+"/")
}

// The platform words a check or a GATE-NEEDS word maps to.
const (
	PlatformLinux   = "linux"
	PlatformWindows = "windows"
	PlatformMacOS   = "macos"
	PlatformTLA     = "tla"
	PlatformXDP     = "xdp"
)

// GatePlatforms is the CI platforms a card's GATE-NEEDS names: linux, windows, macos (or
// darwin), tla, and linux-<x> as linux and, for linux-xdp, xdp. A word that names no
// platform (lan, landlock, arm64, go) names none.
func GatePlatforms(needs []string) []string {
	var out []string
	add := func(p string) {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	for _, n := range needs {
		switch n = strings.ToLower(strings.TrimSpace(n)); {
		case n == "linux":
			add(PlatformLinux)
		case n == "windows":
			add(PlatformWindows)
		case n == "macos", n == "darwin":
			add(PlatformMacOS)
		case n == "tla":
			add(PlatformTLA)
		case strings.HasPrefix(n, "linux-"):
			add(PlatformLinux)
			if strings.TrimPrefix(n, "linux-") == "xdp" {
				add(PlatformXDP)
			}
		}
	}
	return out
}

var (
	tlaCheckRE  = regexp.MustCompile(`(?i)\b(tla|tlc)\b`)
	xdpCheckRE  = regexp.MustCompile(`(?i)\bxdp\b`)
	winCheckRE  = regexp.MustCompile(`(?i)\bwindows\b|\bwin(32|64)?\b`)
	macCheckRE  = regexp.MustCompile(`(?i)\bmacos\b|\bdarwin\b|\bmac\b`)
	linuxLikeRE = regexp.MustCompile(`(?i)\bubuntu\b|\blinux\b|\bdebian\b|\bs390x\b`)
)

// CheckPlatforms is the platforms one CI job runs on, from its workflow's name, its own
// name and its runner labels: a TLA or TLC job is tla alone; an XDP job is xdp; a windows,
// macos or linux runner (or name) is that platform; a job named for no platform on a
// hosted ubuntu runner is linux. A job with no word for its platform and no label is none.
func CheckPlatforms(workflow, name string, labels []string) []string {
	text := workflow + " " + name
	if tlaCheckRE.MatchString(text) {
		return []string{PlatformTLA}
	}
	if xdpCheckRE.MatchString(text) {
		return []string{PlatformXDP}
	}
	all := text + " " + strings.Join(labels, " ")
	var out []string
	switch {
	case winCheckRE.MatchString(text):
		out = append(out, PlatformWindows)
	case macCheckRE.MatchString(text):
		out = append(out, PlatformMacOS)
	case linuxLikeRE.MatchString(text):
		out = append(out, PlatformLinux)
	case winCheckRE.MatchString(all):
		out = append(out, PlatformWindows)
	case macCheckRE.MatchString(all):
		out = append(out, PlatformMacOS)
	case linuxLikeRE.MatchString(all):
		out = append(out, PlatformLinux)
	}
	return out
}

// cardBase is the repository (owner/name) and branch a card's brief starts from: its
// REPO: and BASE: lines, else the card's base field; "" when it names none.
func cardBase(c *Card) (repo, base string) {
	b := swarm.ReadCardBase([]byte(c.F("brief")))
	repo = RepoName(cmp.Or(b.Repo, b.Named, c.F("repo")))
	base = cmp.Or(b.Ref, c.F("base"))
	return repo, base
}

// isFixRed says the card's KIND is fix-red: its work is the cure of a red at BASE.
func isFixRed(c *Card) bool {
	k, _ := swarm.CardHeaderValue([]byte(c.F("brief")), "KIND")
	return strings.EqualFold(strings.TrimSpace(k), "fix-red")
}

// baseFact is the CI fact the tick read for (repo, base), nil when none.
func baseFact(r TickReq, repo, base string) *BaseCIFacts {
	for i := range r.BaseCI {
		f := &r.BaseCI[i]
		if f.Repo != "" && RepoName(f.Repo) == repo && f.Base == base && f.Tip != "" {
			return f
		}
	}
	return nil
}

// baseTree is the tree the tick read for (repo, base), nil when none.
func baseTree(r TickReq, repo, base string) *BaseTree {
	for i := range r.BaseTrees {
		t := &r.BaseTrees[i]
		if RepoName(t.Repo) == repo && t.Base == base {
			return t
		}
	}
	return nil
}

// BaseRedWhy is why the card waits for its BASE: "BASE red on <platform>: <checks>[; on
// <platform>: <checks>], at <sha>" for each platform its GATE-NEEDS names that BASE's
// newest completed CI is red on; "" when it may be dealt (BASE green there, no fact read,
// no platform named, or a fix-red card).
func BaseRedWhy(c *Card, r TickReq) string {
	if c == nil || !anyRed(r.BaseCI) {
		return "" // no red BASE read: nothing to parse, nothing allocated (the deal asks every ready card)
	}
	if isFixRed(c) {
		return ""
	}
	plats := GatePlatforms(GateNeeds(c.F("brief")))
	if len(plats) == 0 {
		return ""
	}
	repo, base := cardBase(c)
	if repo == "" || base == "" {
		return ""
	}
	f := baseFact(r, repo, base)
	if f == nil || !f.Red {
		return ""
	}
	var parts []string
	for _, p := range plats {
		var names []string
		for _, ch := range f.TipFailed {
			if slices.Contains(ch.Platforms, p) && !slices.Contains(names, ch.Name) {
				names = append(names, ch.Name)
			}
		}
		if len(names) > 0 {
			parts = append(parts, p+": "+Preview(names, ", "))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "BASE red on " + strings.Join(parts, "; on ") + ", at " + shortSha(f.Tip)
}

// anyRed says some fact read is red.
func anyRed(fs []BaseCIFacts) bool {
	for i := range fs {
		if fs[i].Red && len(fs[i].TipFailed) > 0 {
			return true
		}
	}
	return false
}

// shortSha is a commit as a reason line names it: 12 hex digits.
func shortSha(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// headerList is a header line's comma list as entries: each trimmed, a parenthetical
// note cut off ("x.go (read)"), and "-" or "none" naming nothing; an entry with a space
// left in it is prose, not a path, and is skipped.
func headerList(brief, key string) []string {
	v, _ := swarm.CardHeaderValue([]byte(brief), key)
	var out []string
	for _, e := range strings.Split(v, ",") {
		if cut, _, ok := strings.Cut(e, "("); ok {
			e = cut
		}
		e = strings.TrimSpace(e)
		if e == "" || e == "-" || strings.EqualFold(e, "none") || strings.ContainsAny(e, " \t") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// briefNeedsLine is the brief's Needs: line as ids, and whether it has one.
func briefNeedsLine(brief string) ([]string, bool) {
	for _, line := range strings.Split(brief, "\n") {
		if key, v, ok := cardhdr.KeyValue(line); ok && strings.EqualFold(key, "needs") {
			var out []string
			for _, id := range strings.Split(v, ",") {
				if cut, _, ok := strings.Cut(id, "("); ok {
					id = cut
				}
				id = strings.TrimSpace(id)
				if id != "" && id != "-" && !strings.EqualFold(id, "none") {
					out = append(out, id)
				}
			}
			return out, true
		}
	}
	return nil, false
}

// BriefPreflightWhy is why a ready card's brief fails against its BASE (PreflightAtBase, the
// check the preflight verb runs too): "" when it passes, or when nothing could be checked
// (no tree read, no parent on the table).
func BriefPreflightWhy(s *Snapshot, c *Card, r TickReq) string {
	if c == nil {
		return ""
	}
	brief := c.F("brief")
	if len(r.BaseTrees) == 0 && !strings.Contains(brief, "DEPENDS-ON") {
		return "" // nothing to check: nothing parsed, nothing allocated (the deal asks every ready card)
	}
	var tree *BaseTree
	if len(r.BaseTrees) > 0 {
		if repo, base := cardBase(c); repo != "" && base != "" {
			tree = baseTree(r, repo, base)
		}
	}
	findings := PreflightAtBase(s, brief, c.F("needs"), tree)
	if len(findings) == 0 {
		return ""
	}
	var whys []string
	for _, f := range findings {
		whys = append(whys, f.Why)
	}
	return fmt.Sprintf("%s: the brief fails against BASE: %s; brief it again (nova-sprint brief %s --brief-file <f>) or drop it", c.ID, strings.Join(whys, "; "), c.ID)
}

// PreflightFinding is one reason a brief fails against BASE: the header it is of (PATHS,
// START, DEPENDS-ON) and the line that says it.
type PreflightFinding struct {
	Key string
	Why string
}

// PreflightAtBase holds a brief to BASE (the deal's precondition, and the preflight verb's
// check of a brief directory): every PATHS entry not under NEW, and every START entry, is in
// tree (a file, a directory holding one, or a glob matching one); every DEPENDS-ON card on
// the table has landed or is named by the brief's Needs: line or cardNeeds. A nil tree checks
// no path; a nil snapshot, or a parent with no record on it, checks no parent.
func PreflightAtBase(s *Snapshot, brief, cardNeeds string, tree *BaseTree) []PreflightFinding {
	var findings []PreflightFinding
	if tree != nil {
		news := headerList(brief, "NEW")
		isNew := func(p string) bool {
			for _, n := range news {
				if n == p || hygiene.MatchGlob(n, p) {
					return true
				}
			}
			return false
		}
		var paths, start []string
		for _, p := range headerList(brief, "PATHS") {
			if !isNew(p) && !tree.has(p) && !slices.Contains(paths, p) {
				paths = append(paths, p)
			}
		}
		for _, p := range headerList(brief, "START") {
			if !isNew(p) && !tree.has(p) && !slices.Contains(start, p) && !slices.Contains(paths, p) {
				start = append(start, p)
			}
		}
		at := " at " + orDash(tree.Base) + " " + shortSha(tree.Sha)
		if len(paths) > 0 {
			findings = append(findings, PreflightFinding{"PATHS", "PATHS not" + at + " (and not under NEW): " + strings.Join(paths, ", ")})
		}
		if len(start) > 0 {
			findings = append(findings, PreflightFinding{"START", "START not" + at + ": " + strings.Join(start, ", ")})
		}
	}
	if s != nil && s.Work != nil && strings.Contains(brief, "DEPENDS-ON") {
		needs, hasLine := briefNeedsLine(brief)
		named := append(needs, Split(cardNeeds)...)
		var deps []string
		for _, d := range headerList(brief, "DEPENDS-ON") {
			if slices.Contains(named, d) {
				continue // the deal waits for it (WaitsFor)
			}
			dc := s.Work.Card(d)
			if dc == nil {
				continue // no record on the table: not judged here
			}
			if dc.Placed() && dc.Col == Landed || !dc.Placed() && dc.F("outcome") == "landed" {
				continue
			}
			state := dc.Col
			if !dc.Placed() {
				state = "off the table (" + orDash(dc.F("outcome")) + ")"
			}
			deps = append(deps, d+" ("+state+")")
		}
		if len(deps) > 0 {
			line := "Needs: does not name it"
			if hasLine && len(needs) == 0 {
				line = `"Needs: none" beside a non-empty DEPENDS-ON`
			}
			findings = append(findings, PreflightFinding{"DEPENDS-ON", "DEPENDS-ON not landed and not in Needs (" + line + "): " + strings.Join(deps, ", ")})
		}
	}
	return findings
}

// BaseTreeOf is a tree of these files at sha (the binding's read, git ls-tree), sorted.
func BaseTreeOf(repo, base, sha string, files []string) BaseTree {
	fs := slices.Clone(files)
	slices.Sort(fs)
	return BaseTree{Repo: RepoName(repo), Base: base, Sha: sha, Files: fs}
}

// baseRedConds groups the cards waiting on a red BASE into one condition per BASE.
func baseRedConds(waits map[string][]string, whys map[string][]string) []cond {
	var out []cond
	for _, sub := range slices.Sorted(maps.Keys(waits)) {
		ids := waits[sub]
		out = append(out, cond{typ: NBaseRedWait, stream: sub, streamLevel: true, primaries: ids,
			what: fmt.Sprintf("%d cards wait undealt, no attempt spent, until BASE is green on their platforms: %s",
				len(ids), strings.Join(whys[sub], "; "))})
	}
	return out
}

// inheritedRedRE is a report saying its failure is BASE's: an INHERITED-RED: line, or the
// failure said red, failing or reproduced at (or on) BASE.
// BASE is the brief's own word, in capitals: "fails at the base" in prose is no claim.
var inheritedRedRE = regexp.MustCompile(`(?m)(?i:` + negated + `)(?:^\s*INHERITED-RED:|\b(?i:inherited\s+red)\b|\b(?i:red|fails|failing|failed|reproduced|reproduces|reproducible)\s+(?i:at|on)\s+(?i:the\s+)?BASE\b)`)

// InheritedRedOf is the line of a failed finish's report that says its failure is
// reproduced at BASE (an inherited red), "" when none does: an INHERITED-RED: <failures>
// at <sha> line, or words such as "fails at BASE", "red at BASE", "reproduced at BASE",
// "inherited red", unnegated. The worker's word is all the sprint has, as for a brief
// defect (brief_defect.go); the main-green precondition (BaseRedWhy) checks BASE's CI before
// the next attempt is dealt.
func InheritedRedOf(report string) string {
	i := firstUnnegated(inheritedRedRE, report)
	if i < 0 {
		return ""
	}
	start := strings.LastIndexByte(report[:i], '\n') + 1
	end := strings.IndexByte(report[i:], '\n')
	if end < 0 {
		end = len(report) - i
	}
	return strings.TrimSpace(report[start : i+end])
}

// inheritedAttempts is the primary's attempts that failed on an inherited red.
func inheritedAttempts(c *Card) []int {
	var out []int
	for _, v := range Split(c.F(FieldInheritedAttempts)) {
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil && n > 0 && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// inheritedSince is how many of the primary's inherited attempts come after attempt n.
func inheritedSince(c *Card, n int) int {
	k := 0
	for _, a := range inheritedAttempts(c) {
		if a > n {
			k++
		}
	}
	return k
}

// inheritedBefore is how many of the primary's inherited attempts come before attempt n.
func inheritedBefore(c *Card, n int) int {
	k := 0
	for _, a := range inheritedAttempts(c) {
		if a < n {
			k++
		}
	}
	return k
}

// RepoBase is one repository (owner/name) and branch a ready card starts from.
type RepoBase struct {
	Repo string
	Base string
}

// DealBases is every BASE the ready primaries start from, in order: what the binding reads
// (CI and tree) for the deal's preconditions.
func DealBases(s *Snapshot) []RepoBase {
	if s == nil || s.Work == nil {
		return nil
	}
	var out []RepoBase
	for _, c := range s.Work.Column(Ready) {
		if IsSentinel(c) {
			continue
		}
		repo, base := cardBase(c)
		if repo == "" || base == "" {
			continue
		}
		if rb := (RepoBase{repo, base}); !slices.Contains(out, rb) {
			out = append(out, rb)
		}
	}
	slices.SortFunc(out, func(a, b RepoBase) int { return cmp.Or(cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.Base, b.Base)) })
	return out
}
