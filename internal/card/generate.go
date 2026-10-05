// Package card is nova-card's generate, template and lint, as verbs of the one
// nova-sprint binary. The planning is internal/cardgen. This package is the
// transport: it reads the source, resolves the base, lints every brief and
// writes the directory, or refuses.
package card

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/gitrun"
	"github.com/mas-bandwidth/nova-sprint/internal/nsprint/verbflag"
	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/subproc"
	"github.com/mas-bandwidth/nova-sprint/internal/swarm"
)

// Command is the words a line of these verbs names.
const Command = "nova-sprint card"

// Generate writes a directory of pre-linted briefs. Exit 0 is done, 1 is a red
// brief and nothing written, 2 is usage or a source that cannot be read.
func Generate(args []string, stdout, stderr io.Writer) int {
	fs := verbflag.New("card generate")
	from := fs.String("from", "", "the source `kind`: ledger, findings or help")
	ledger := fs.String("ledger", "", "with --from ledger: the ledger's `name`, one of "+strings.Join(cardgen.LedgerNames(), ", "))
	file := fs.String("file", "", "with --from findings: the TSV `file` of file:line, finding, remedy, test (a header row is skipped)")
	var tools multi
	fs.Var(&tools, "tool", "with --from help: a tool `name` whose help the card is about; repeat for more")
	binDir := fs.String("bin-dir", "", "with --from help: the `dir` holding the tools' binaries (default: PATH)")
	repoDir := fs.String("repo-dir", "", "a checkout `dir` of the target repository at the base: the source is read from it, the repository, branch and sha are read off it, and every PATHS entry is checked to exist in it")
	repo := fs.String("repo", "", "the `owner/name` the REPO: line carries (default: --repo-dir's origin)")
	base := fs.String("base", "", "the `branch` the BASE: line carries (default: --repo-dir's branch)")
	sha := fs.String("sha", "", "the base `sha`, 40 hex (default: --repo-dir's HEAD)")
	out := fs.String("out", "", "the `dir` the briefs and manifest.tsv are written into; created, and refused when it already holds a brief")
	tier := fs.String("tier", "", "flash or pro; default by source: a ledger's own (mechanical ledgers flash, decisions pro), findings and help pro")
	prefix := fs.String("prefix", "", "the `word` every card id opens with (default: the ledger's name, finding, or help)")
	minutes := fs.Int("minutes", 0, "the Deadline line's `minutes` (default: 45 flash, 60 pro)")
	maxCards := fs.Int("max", 0, "write at most this many cards, in source order; 0 is all")
	dryRun := fs.Bool("dry-run", false, "plan and lint, print the manifest and the CARDS line, and write nothing")
	if err := verbflag.Parse(fs, args); err != nil {
		return refuse(stderr, "generate", verbflag.Explain(fs, err))
	}
	if fs.NArg() > 0 {
		return refuse(stderr, "generate", fmt.Sprintf("unexpected argument %q; every input is a flag", fs.Arg(0)))
	}
	if *out == "" {
		return refuse(stderr, "generate", "wants --out <dir>")
	}
	if *tier != "" && *tier != "flash" && *tier != "pro" {
		return refuse(stderr, "generate", fmt.Sprintf("--tier %q; want flash or pro", *tier))
	}
	h := cardgen.Header{Repo: *repo, Base: *base, Sha: *sha, Minutes: *minutes}
	if *repoDir != "" {
		if err := readCheckout(*repoDir, &h); err != nil {
			return refuse(stderr, "generate", err.Error())
		}
	}
	switch {
	case h.Repo == "":
		return refuse(stderr, "generate", "no repository: --repo <owner/name>, or --repo-dir a checkout whose origin names one")
	case h.Base == "":
		return refuse(stderr, "generate", "no base branch: --base <branch>, or --repo-dir a checkout on a branch (a detached HEAD names none)")
	case !shaRE.MatchString(h.Sha):
		return refuse(stderr, "generate", fmt.Sprintf("base sha %q is not 40 hex: --sha <40hex>, or --repo-dir a checkout with a HEAD", h.Sha))
	}
	var plan cardgen.Plan
	var notes []string
	switch *from {
	case "ledger":
		l, ok := cardgen.Ledgers[*ledger]
		if !ok {
			return refuse(stderr, "generate", fmt.Sprintf("--ledger %q is not a ledger this build knows; one of %s", *ledger, strings.Join(cardgen.LedgerNames(), ", ")))
		}
		if *repoDir == "" {
			return refuse(stderr, "generate", "--from ledger reads the ledger from --repo-dir <dir>, a checkout of the repository at the base")
		}
		raw, err := os.ReadFile(filepath.Join(*repoDir, filepath.FromSlash(l.File)))
		if err != nil {
			return refuse(stderr, "generate", "cannot read the ledger "+l.File+" in "+*repoDir+": "+err.Error())
		}
		rows, skipped := cardgen.ParseLedger(l, string(raw))
		notes = append(notes, skipped...)
		plan = cardgen.PlanLedger(l, rows, *prefix, *tier, *maxCards)
	case "findings":
		if *file == "" {
			return refuse(stderr, "generate", "--from findings wants --file <tsv>")
		}
		raw, err := os.ReadFile(*file)
		if err != nil {
			return refuse(stderr, "generate", "cannot read "+*file+": "+err.Error())
		}
		findings, skipped := cardgen.ParseFindings(string(raw))
		notes = append(notes, skipped...)
		plan = cardgen.PlanFindings(findings, *prefix, *tier, *maxCards)
	case "help":
		if len(tools) == 0 {
			return refuse(stderr, "generate", "--from help wants --tool <name>, one per tool")
		}
		plan = cardgen.Plan{Tier: *tier, Waves: 1}
		if plan.Tier == "" {
			plan.Tier = "pro"
		}
		if *maxCards > 0 && len(tools) > *maxCards {
			tools = tools[:*maxCards]
		}
		for _, tool := range tools {
			help, err := renderedHelp(*binDir, tool)
			if err != nil {
				notes = append(notes, tool+": "+err.Error())
				continue
			}
			plan.Cards = append(plan.Cards, cardgen.PlanHelp(tool, help, exampleTest(*repoDir, tool), *prefix, *tier))
		}
	case "":
		return refuse(stderr, "generate", "wants --from ledger|findings|help")
	default:
		return refuse(stderr, "generate", fmt.Sprintf("--from %q; want ledger, findings or help", *from))
	}
	if len(plan.Cards) == 0 {
		return refuse(stderr, "generate", "the source yields no card; nothing to write")
	}
	if dup := cardgen.DuplicateID(plan.Cards); dup != "" {
		return refuse(stderr, "generate", fmt.Sprintf("card %s is planned twice (a --tool named twice?); a card is its file %s.md and a second one would overwrite it", dup, dup))
	}
	briefs := make([]string, len(plan.Cards))
	red := 0
	for i := range plan.Cards {
		c := &plan.Cards[i]
		if *repoDir != "" {
			cardgen.NewTestFile(c, func(glob string) bool { return existsAt(*repoDir, glob) })
		}
		briefs[i] = cardgen.Render(h, *c)
		for _, f := range cardgen.Lint(c.ID, briefs[i]) {
			fmt.Fprintln(stdout, oneline.Escape(f.String()))
			red++
		}
		if *repoDir != "" {
			for _, p := range c.Paths {
				if !existsAt(*repoDir, p) && !c.Creates(p) {
					fmt.Fprintln(stdout, oneline.Escape(cardgen.LintFinding{ID: c.ID, Check: "paths-at-base", Line: 6, Excerpt: "PATHS entry " + p + " names nothing in " + *repoDir}.String()))
					red++
				}
			}
		}
	}
	if red > 0 {
		fmt.Fprintf(stderr, "%s generate FAILED: %d red line(s) above; nothing written to %s\n", Command, red, oneline.Field(*out))
		return 1
	}
	if *dryRun {
		for _, n := range notes {
			fmt.Fprintf(stdout, "CARDS NOTE skipped %s\n", oneline.Escape(n))
		}
		fmt.Fprint(stdout, cardgen.Manifest(plan))
		fmt.Fprintln(stdout, cardgen.OKLine(*out, plan)+" dry-run=yes (nothing written)")
		return 0
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return refuse(stderr, "generate", "cannot create --out "+*out+": "+err.Error())
	}
	if held, _ := filepath.Glob(filepath.Join(*out, "*.md")); len(held) > 0 {
		return refuse(stderr, "generate", fmt.Sprintf("--out %s already holds %d brief(s); name an empty directory", *out, len(held)))
	}
	for i, c := range plan.Cards {
		if err := os.WriteFile(filepath.Join(*out, c.ID+".md"), []byte(briefs[i]), 0o644); err != nil {
			return refuse(stderr, "generate", "cannot write "+c.ID+".md: "+err.Error())
		}
	}
	if err := os.WriteFile(filepath.Join(*out, "manifest.tsv"), []byte(cardgen.Manifest(plan)), 0o644); err != nil {
		return refuse(stderr, "generate", "cannot write manifest.tsv: "+err.Error())
	}
	for _, n := range notes {
		fmt.Fprintf(stdout, "CARDS NOTE skipped %s\n", oneline.Escape(n))
	}
	line := cardgen.OKLine(*out, plan)
	if plan.Shared {
		line += " shared-paths=yes (add with --allow-shared-paths)"
	}
	fmt.Fprintln(stdout, line)
	return 0
}

// Lint holds each named brief to the lint nova-sprint add runs. Exit 1 is a red brief.
func Lint(args []string, stdout, stderr io.Writer) int {
	fs := verbflag.New("card lint")
	var cards multi
	fs.Var(&cards, "card", "a brief `file` to hold to the add's lint; repeat for more")
	if err := verbflag.Parse(fs, args); err != nil {
		return refuse(stderr, "lint", verbflag.Explain(fs, err))
	}
	if len(cards) == 0 || fs.NArg() > 0 {
		return refuse(stderr, "lint", "wants --card <file>, one per brief, and nothing else")
	}
	red := 0
	for _, file := range cards {
		raw, err := os.ReadFile(file)
		if err != nil {
			return refuse(stderr, "lint", "cannot read "+file+": "+err.Error())
		}
		id := strings.TrimSuffix(filepath.Base(file), ".md")
		findings := cardgen.Lint(id, string(raw))
		for _, f := range findings {
			fmt.Fprintln(stdout, oneline.Escape(f.String()))
		}
		if len(findings) > 0 {
			red++
			continue
		}
		fmt.Fprintf(stdout, "LINT OK file=%s\n", oneline.Field(file))
	}
	if red > 0 {
		return 1
	}
	return 0
}

// Template prints nova-swarm's card template. It takes no flags and no arguments.
func Template(args []string, stdout, stderr io.Writer) int {
	fs := verbflag.New("card template")
	if err := verbflag.Parse(fs, args); err != nil {
		return refuse(stderr, "template", verbflag.Explain(fs, err))
	}
	if fs.NArg() > 0 {
		return refuse(stderr, "template", "takes no flags and no arguments")
	}
	text, err := swarm.Template("card")
	if err != nil {
		return refuse(stderr, "template", err.Error())
	}
	fmt.Fprint(stdout, text)
	return 0
}

// multi is a repeatable string flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// refuse is one line, `nova-sprint card <verb> REFUSED: <what>; run: nova-sprint card <verb> -h`, exit 2.
func refuse(stderr io.Writer, verb, what string) int {
	where := Command + " " + verb
	fmt.Fprintf(stderr, "%s REFUSED: %s; run: %s -h\n", where, oneline.Escape(what), where)
	return 2
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// readCheckout fills the header's empty fields from a checkout: the repository from
// origin's URL, the branch from HEAD's name, the sha from HEAD.
func readCheckout(dir string, h *cardgen.Header) error {
	git := func(args ...string) (string, error) {
		res, err := gitrun.Run(context.Background(), gitrun.Options{C: dir}, args...)
		if err != nil {
			return "", fmt.Errorf("git %s in %s: %s", strings.Join(args, " "), dir, strings.TrimSpace(nonEmpty(string(res.Stderr), err.Error())))
		}
		return strings.TrimSpace(string(res.Stdout)), nil
	}
	if h.Sha == "" {
		sha, err := git("rev-parse", "HEAD")
		if err != nil {
			return err
		}
		h.Sha = sha
	}
	if h.Base == "" {
		// a detached HEAD names no branch: symbolic-ref exits 1 and Base stays empty
		if branch, err := git("symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
			h.Base = branch
		}
	}
	if h.Repo == "" {
		url, err := git("remote", "get-url", "origin")
		if err == nil {
			h.Repo = repoOfURL(url)
		}
	}
	return nil
}

func nonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

var repoURLRE = regexp.MustCompile(`[:/]([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// repoOfURL is the owner/name of a forge URL, ssh or https; "" when it has none.
func repoOfURL(url string) string {
	m := repoURLRE.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return ""
	}
	return m[1]
}

// existsAt says whether a PATHS entry (a file or a glob, repository-relative) names
// at least one file in the checkout.
func existsAt(dir, p string) bool {
	matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(p)))
	return err == nil && len(matches) > 0
}

// exampleTest is the test of cmd/<tool> in the checkout that runs the help's example
// lines; "" with no checkout, no package or no such test.
func exampleTest(repoDir, tool string) string {
	if repoDir == "" {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(repoDir, "cmd", tool, "*_test.go"))
	var texts []string
	for _, f := range files {
		if raw, err := os.ReadFile(f); err == nil {
			texts = append(texts, string(raw))
		}
	}
	return cardgen.ExampleTest(texts...)
}

// renderedHelp runs `<tool> help` and returns what it printed.
func renderedHelp(binDir, tool string) (string, error) {
	bin := tool
	if binDir != "" {
		bin = filepath.Join(binDir, tool)
	}
	cmd, cancel := subproc.Command(context.Background(), subproc.Tool, bin, "help")
	defer cancel()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil && out.Len() == 0 {
		return "", fmt.Errorf("`%s help` printed nothing: %v", bin, err)
	}
	return out.String(), nil
}
