package sprint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/internal/diffcheck"
	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// The mechanical proof's parser checks (docs/SPEC-SPRINT.md section 6, the
// mechanical proof). After the work lint and the machine gate pass and before
// any reader is asked, a mechanical card (KIND: delete, rename or
// assertion-rewrite) is run once through its machine proof, in process, by
// go/parser over the tree at the attempt's head, so no model reads it:
//
//   - delete: the declarations the diff removes are gone from the head and no
//     identifier in any of its Go files references them;
//   - rename: the old declaration the diff removes is gone and the new one it
//     adds is present;
//   - assertion-rewrite: every changed line is an assertion line of a test
//     file.
//
// The runner NewParserProof reads the head through a clone and a view (the
// work lint's ReadWorkView); the checks themselves are pure over a WorkView, so
// a test drives them without git.

// ProofBenchEnv names the hosts the mechanical proof is configured for, a comma
// list like the gate's (GateBenchEnv): `NOVA_SPRINT_PROOF_BENCH=bench-a`.
// With none named nova-sprint runs no proof and every mechanical card is read
// as before. The proof runs in process over the head tree, so the hosts are the
// machines the proof is attributed to; the first is recorded.
const ProofBenchEnv = "NOVA_SPRINT_PROOF_BENCH"

// ProofCloneEnv is where the proof keeps its clone cache; empty is
// nova-sprint/proof under the user's cache directory.
const ProofCloneEnv = "NOVA_SPRINT_PROOF_CLONE"

// ProofBudget bounds one proof's whole read, its clone and its view included.
const ProofBudget = 5 * time.Minute

// ProofGit is where the proof finds a card's repository: Clone is the clone of
// the repository a brief names (its REPO or base-repo line), an error when there
// is none; View reads the attempt's view at its head, nil for ReadWorkView; Env
// is the git children's environment; Host is the bench the run is attributed to;
// Budget bounds one proof, zero for ProofBudget; TTL is how long one head's
// proof is kept, zero for two minutes; Now is the clock the cache is kept by,
// nil for the wall clock.
type ProofGit struct {
	Clone  func(repo string) (string, error)
	View   func(ctx context.Context, dir, base, head string, env []string) (WorkView, error)
	Env    []string
	Host   string
	Budget time.Duration
	TTL    time.Duration
	Now    func() time.Time
}

// NewParserProof is the machine proof over g: each mechanical card's head is
// read once (the diff and the files at the head) and its kind is checked by
// go/parser. A clone or a view that does not answer is Waiting, never a
// failure; a check that fails is a rework's finding.
func NewParserProof(g ProofGit) ProofRunner {
	budget := g.Budget
	if budget <= 0 {
		budget = ProofBudget
	}
	view := g.View
	if view == nil {
		view = ReadWorkView
	}
	ttl := g.TTL
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	now := g.Now
	if now == nil {
		now = time.Now
	}
	type kept struct {
		at  time.Time
		run ProofRun
	}
	var mu sync.Mutex
	cache := map[string]kept{}
	return func(s *Snapshot, pr *Card) ProofRun {
		brief := pr.F("brief")
		kind, ok := cardhdr.MechanicalKind(brief)
		if !ok {
			return ProofRun{}
		}
		cb := swarm.ReadCardBase([]byte(brief))
		base := cb.Ref
		if base == "" {
			base = "main"
		}
		head := pr.F("head")
		if cb.Repo == "" || g.Clone == nil {
			return ProofRun{Kind: kind, Host: g.Host, Waiting: "no proof host is configured for the proof"}
		}
		if !headRE.MatchString(head) {
			return ProofRun{Kind: kind, Host: g.Host, Waiting: "the attempt names no pushed head: " + orDash(head)}
		}
		key := strings.Join([]string{cb.Repo, base, head, brief}, "\x00")
		mu.Lock()
		if k, ok := cache[key]; ok && now().Sub(k.at) < ttl {
			mu.Unlock()
			return k.run
		}
		mu.Unlock()
		run := func() ProofRun {
			dir, err := g.Clone(cb.Repo)
			if err != nil {
				return ProofRun{Kind: kind, Host: g.Host, Waiting: err.Error()}
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			v, err := view(ctx, dir, base, head, g.Env)
			if err != nil {
				return ProofRun{Kind: kind, Host: g.Host, Waiting: err.Error()}
			}
			if !v.Pushed {
				return ProofRun{Kind: kind, Host: g.Host, Waiting: "the head " + head + " is not in the repository"}
			}
			r := ParserProof(kind, v)
			r.Kind, r.Host = kind, g.Host
			return r
		}()
		mu.Lock()
		if len(cache) > 4096 {
			clear(cache)
		}
		cache[key] = kept{at: now(), run: run}
		mu.Unlock()
		return run
	}
}

// ParserProof is the go/parser check of one mechanical kind over the tree at the
// head (v): delete, rename or assertion-rewrite. It reads the card's diff and
// the head's Go files alone. A kind that is not mechanical waits, as one whose
// tree cannot be read does.
func ParserProof(kind string, v WorkView) ProofRun {
	switch kind {
	case cardhdr.KindDelete:
		return deletionProof(v)
	case cardhdr.KindRename:
		return renameProof(v)
	case cardhdr.KindRewrite:
		return assertionRewriteProof(v)
	}
	return ProofRun{Kind: kind, Waiting: "the kind " + orDash(kind) + " is not a mechanical kind the parser proves"}
}

// deletionProof proves a delete card: every declaration the diff removes is
// gone from the head, and no identifier of any head Go file references it. A
// declaration still present, or an identifier left that names it, is a finding
// at the file:line the parser found it.
func deletionProof(v WorkView) ProofRun {
	removed, added, _ := proofDiff(v.Diff)
	// a declaration the diff changes is removed and added again: only what the
	// diff takes away and does not put back is deleted
	kept := map[string]bool{}
	for _, n := range changedNames(added) {
		kept[n] = true
	}
	symbols := changedNames(removed)
	var gone []string
	for _, s := range symbols {
		if !kept[s] {
			gone = append(gone, s)
		}
	}
	symbols = gone
	if len(symbols) == 0 {
		return ProofRun{Kind: cardhdr.KindDelete, Failed: []ProofFinding{{What: "the diff removes no declaration the proof can name"}}}
	}
	idx, why := headIdents(v)
	if why != "" {
		return ProofRun{Kind: cardhdr.KindDelete, Waiting: why}
	}
	var fs []ProofFinding
	var lines []string
	for _, sym := range symbols {
		if pos, ok := idx[sym]; ok {
			fs = append(fs, ProofFinding{File: pos.Filename, Line: pos.Line, What: "reference to deleted symbol " + sym + " remains"})
			continue
		}
		lines = append(lines, "deleted: "+sym+"; no references to "+sym+" remain")
	}
	if len(fs) > 0 {
		return ProofRun{Kind: cardhdr.KindDelete, Failed: fs}
	}
	return ProofRun{Kind: cardhdr.KindDelete, Lines: lines}
}

// renameProof proves a rename card: the diff removes one declaration and adds
// one, the old name is gone from the head, and the new name is present. A diff
// that does not name one old and one new declaration is a finding the rework
// reads.
func renameProof(v WorkView) ProofRun {
	removed, added, _ := proofDiff(v.Diff)
	olds, news := changedNames(removed), changedNames(added)
	if len(olds) != 1 || len(news) != 1 {
		return ProofRun{Kind: cardhdr.KindRename, Failed: []ProofFinding{{What: "the diff does not name one rename: removed " + nameList(olds) + ", added " + nameList(news)}}}
	}
	old, new := olds[0], news[0]
	idx, why := headIdents(v)
	if why != "" {
		return ProofRun{Kind: cardhdr.KindRename, Waiting: why}
	}
	var fs []ProofFinding
	if pos, ok := idx[old]; ok {
		fs = append(fs, ProofFinding{File: pos.Filename, Line: pos.Line, What: "the old name " + old + " is still present"})
	}
	if _, ok := idx[new]; !ok {
		fs = append(fs, ProofFinding{What: "the new name " + new + " is not present"})
	}
	if len(fs) > 0 {
		return ProofRun{Kind: cardhdr.KindRename, Failed: fs}
	}
	return ProofRun{Kind: cardhdr.KindRename, Lines: []string{"renamed: " + old + " -> " + new + "; no occurrence of " + old + " is left and " + new + " exists"}}
}

// assertionRewriteProof proves an assertion-rewrite card: every file the diff
// changes is a test file and every line it adds is an assertion line (a blank
// line, a comment, an import or a call to an assertion). A code file, or a
// changed line that asserts nothing, is a finding.
func assertionRewriteProof(v WorkView) ProofRun {
	var fs []ProofFinding
	var lines []string
	files := diffcheck.Parse(v.Diff)
	for _, f := range files {
		if !strings.HasSuffix(f.New, "_test.go") {
			fs = append(fs, ProofFinding{File: f.New, What: "the change touches " + orDash(f.New) + ", which is not a test file"})
			continue
		}
		line := 0
		for _, h := range f.Hunks {
			line = h.NewStart
			for _, l := range h.Lines {
				if l == "" {
					continue
				}
				switch l[0] {
				case ' ':
					line++
				case '-':
					body := strings.TrimSpace(l[1:])
					if body != "" && !assertionLine(body) {
						fs = append(fs, ProofFinding{File: f.New, Line: line, What: "the change is not an assertion line: " + body})
					}
					line++
				case '+':
					body := strings.TrimSpace(l[1:])
					if body != "" && !assertionLine(body) {
						fs = append(fs, ProofFinding{File: f.New, Line: line, What: "the change is not an assertion line: " + body})
					}
					line++
				}
			}
		}
		lines = append(lines, "assertion-rewrite: only assertion lines changed in "+f.New)
	}
	if len(fs) > 0 {
		return ProofRun{Kind: cardhdr.KindRewrite, Failed: fs}
	}
	if len(lines) == 0 {
		return ProofRun{Kind: cardhdr.KindRewrite, Failed: []ProofFinding{{What: "the diff changes no assertion line"}}}
	}
	return ProofRun{Kind: cardhdr.KindRewrite, Lines: lines}
}

// proofDiff is the card's diff split for the kind checks: the removed and the
// added lines of its Go files, and every file (both sides) it touches.
func proofDiff(diff string) (removed, added, touched []string) {
	for _, f := range diffcheck.Parse(diff) {
		if f.New != "" {
			touched = append(touched, f.New)
		}
		if f.Old != "" && f.Old != f.New {
			touched = append(touched, f.Old)
		}
		if !strings.HasSuffix(f.New, ".go") && !strings.HasSuffix(f.Old, ".go") {
			continue
		}
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if l == "" {
					continue
				}
				switch l[0] {
				case '-':
					removed = append(removed, strings.TrimSpace(l[1:]))
				case '+':
					added = append(added, strings.TrimSpace(l[1:]))
				}
			}
		}
	}
	return removed, added, touched
}

// changedNames is the top-level declarations lines name, in order, each once: a
// `func Name`, a method `func (r R) Name`, a `type Name`, a `var Name` or a
// `const Name`. Grouped declarations are not named; a rename the diff makes of
// one declaration is the shape this reads.
func changedNames(lines []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		name := ""
		switch f[0] {
		case "func":
			if strings.HasPrefix(f[1], "(") {
				for i := 1; i < len(f); i++ {
					if strings.HasSuffix(f[i], ")") && i+1 < len(f) {
						name = declName(f[i+1])
						break
					}
				}
			} else {
				name = declName(f[1])
			}
		case "type", "var", "const":
			name = declName(strings.TrimSuffix(f[1], "="))
		}
		if isGoIdent(name) && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// declName is a declaration's name from the token that carries it: the part
// before its type parameters (`Old[T]`) and its parameter list (`Old(`).
func declName(tok string) string {
	name, _, _ := strings.Cut(tok, "[")
	name, _, _ = strings.Cut(name, "(")
	return name
}

// isGoIdent says s is a Go identifier.
func isGoIdent(s string) bool {
	if s == "" || s == "_" {
		return false
	}
	for i, r := range s {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// nameList is names as a finding names them: their words, or "none".
func nameList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// headIdents is every identifier of every Go file at the head, each with the
// first place it appears: the files the merge-base tracks, plus the diff's own,
// so a file the change adds is read too. Go files under vendor/ are not the
// module's. The second value is why no tree could be read at all.
func headIdents(v WorkView) (map[string]token.Position, string) {
	if v.Show == nil {
		return nil, "the proof has no tree at the head to parse"
	}
	idx := map[string]token.Position{}
	for _, p := range headGoPaths(v) {
		src, ok := v.Show(p)
		if !ok {
			continue
		}
		for name, pos := range fileIdents(p, src) {
			if _, seen := idx[name]; !seen {
				idx[name] = pos
			}
		}
	}
	return idx, ""
}

// headGoPaths is the module's Go files at the head: the tracked tree's, and the
// diff's own, each once, sorted, without vendor/.
func headGoPaths(v WorkView) []string {
	set := map[string]bool{}
	add := func(p string) {
		if p == "" || !strings.HasSuffix(p, ".go") || strings.HasPrefix(p, "vendor/") {
			return
		}
		set[p] = true
	}
	for _, p := range v.Tracked {
		add(p)
	}
	for _, f := range diffcheck.Parse(v.Diff) {
		add(f.Old)
		add(f.New)
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// fileIdents is every identifier a Go source names, each with the first place
// it appears. The scanner reads the tokens alone, so a file that does not parse
// still names what it has.
func fileIdents(name string, src []byte) map[string]token.Position {
	out := map[string]token.Position{}
	fset := token.NewFileSet()
	file := fset.AddFile(name, fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, 0)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.IDENT && lit != "" {
			if _, ok := out[lit]; !ok {
				out[lit] = file.Position(pos)
			}
		}
	}
	return out
}

// assertionLine says a changed line of a test file is one an assertion rewrite
// may write: an assertion call, an import, a comment, a paren, or a blank.
func assertionLine(body string) bool {
	t := strings.TrimSpace(body)
	switch {
	case t == "", strings.HasPrefix(t, "//"), strings.HasPrefix(t, "import "), strings.HasPrefix(t, `"`), t == ")" || t == "(":
		return true
	}
	for _, needle := range []string{"assert.", "require.", "t.Error", "t.Fatal", "t.Fail", "t.Log", ".Errorf(", ".Fatalf(", ".Error(", ".Fatal(", ".FailNow("} {
		if strings.HasPrefix(t, needle) {
			return true
		}
	}
	return false
}

// proofNameRE is a run of characters a proof clone directory's name does not
// keep.
var proofNameRE = regexp.MustCompile(`[^A-Za-z0-9.-]+`)

// proofClone is the clone the proof reads. The lander's clone is the command's
// (its root and its directory name live in cmd/nova-sprint), so the proof keeps
// its own cache under ProofCloneEnv, or nova-sprint/proof in the user's cache
// directory, fetched once per repository; ReadWorkView then reads the head and
// the base from it.
func proofClone(repo string) (string, error) {
	root := os.Getenv(ProofCloneEnv)
	if root == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("the proof has no clone directory: %w", err)
		}
		root = filepath.Join(cache, "nova-sprint", "proof")
	}
	proofCloneMu.Lock()
	defer proofCloneMu.Unlock()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("the proof's clone directory %s: %w", root, err)
	}
	dir := filepath.Join(root, proofDirName(repo))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return dir, nil
	}
	os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := gitrun.Output(ctx, gitrun.Options{Env: os.Environ(), OwnRepo: true}, "clone", "--no-checkout", "--quiet", repo, dir); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("the proof could not clone %s: %w", repo, err)
	}
	return dir, nil
}

var proofCloneMu sync.Mutex

// proofDirName is a repository's proof clone directory: a readable tail of the
// repository and the first hex digits of a hash of it, so two repositories
// never share a clone.
func proofDirName(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	name := strings.Trim(proofNameRE.ReplaceAllString(repo, "-"), "-.")
	if len(name) > 40 {
		name = name[len(name)-40:]
	}
	return strings.TrimLeft(name, "-.") + "-" + hex.EncodeToString(sum[:8])
}

// proofHosts is ProofBenchEnv's list: trimmed, without blanks or duplicates.
func proofHosts(v string) []string {
	var out []string
	for _, h := range strings.Split(v, ",") {
		if h = strings.TrimSpace(h); h != "" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// proofFromEnv is the machine proof a start binds as DefaultProof: the parser
// proof over the head tree, its own clone (proofClone) and the work lint's
// view, named by NOVA_SPRINT_PROOF_BENCH. No host named is no proof, and every
// mechanical card is read as before. A test drives it with its own getenv.
func proofFromEnv(getenv func(string) string) ProofRunner {
	hosts := proofHosts(getenv(ProofBenchEnv))
	if len(hosts) == 0 {
		return nil
	}
	return NewParserProof(ProofGit{Clone: proofClone, View: ReadWorkView, Env: os.Environ(), Host: hosts[0]})
}

// nova-sprint sets the machine proof at its start from NOVA_SPRINT_PROOF_BENCH
// (the docs' binding), so the feature is not an unwired seam: a sprint that
// names a host proves its mechanical cards and no reader is asked of them; a
// sprint that names none reads every one as before.
func init() {
	if p := proofFromEnv(os.Getenv); p != nil {
		DefaultProof = p
	}
}
