// Command nova-card generates a directory of pre-linted briefs from a structured
// source, ready for one `nova-sprint add --brief-dir`. The planning is
// internal/cardgen (pure functions over text, docs/SPEC-CARD-CONTRACT.md,
// "generated cards"); this file is the transport: it reads the source from a
// checkout, resolves the base, runs the lint over every brief and writes the
// directory, or refuses to with the red line printed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/card"
	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"
	"github.com/mas-bandwidth/nova-sprint/pkg/gitrun"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/verbflag"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
	"github.com/mas-bandwidth/nova-sprint/pkg/subproc"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

const preAlpha = "nova-card is pre-alpha: not ready for production use."

const usage = `nova-card: writes a directory of pre-linted briefs from a ledger, a findings file or a tool's help
` + preAlpha + `

how it works: a source is read from a checkout of the target repository (a ratchet ledger of
internal/ci, a findings TSV, a tool's rendered help); the planner cuts one card per file with
its PATHS, TEST and tier computed from the row, plans every ledger in one wave with each card
chained to the one before it, and holds every brief to the lint nova-sprint add runs before the directory is written.
State: none; the directory, its manifest.tsv and the one CARDS OK line are the whole result.

the flow, three lines:
  nova-card generate --from ledger --ledger serial-tests --repo-dir ./repo --out ./cards
  nova-sprint add --stream debt --brief-dir ./cards
  nova-sprint where

usage:
  nova-card template
  nova-card lint --card <file> [--card <file>...] [--name <n>...] [--dropped <id>...] [--json]
  nova-card help [<verb>]
  nova-card version
  nova-card generate --from ledger --ledger <name> --repo-dir <dir> --out <dir> [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--base <branch>] [--repo <owner/name>] [--name <n>...] [--dropped <id>...] [--dry-run]
  nova-card generate --from findings --file <tsv> --out <dir> (--repo-dir <dir> | --repo <owner/name> --base <branch> --sha <40hex>) [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--name <n>...] [--dropped <id>...] [--dry-run]
  nova-card generate --from help --tool <name> [--tool <name>...] --out <dir> [--bin-dir <dir>] (--repo-dir <dir> | --repo --base --sha) [--tier flash|pro] [--prefix <p>] [--minutes <n>] [--max <n>] [--name <n>...] [--dropped <id>...] [--dry-run]

generate reads the repository, the branch and the base sha from --repo-dir (its origin URL,
its branch, its HEAD); --repo, --base and --sha each override one, and all three together
need no checkout. A card's PATHS are computed from its START line, never typed: every directory
a START file lives in, as its Go files and its tests (<dir>/*.go, <dir>/*_test.go), and the docs
the card names. With a checkout every PATHS entry is checked to exist at it, so a card never
names a path the add would reject. The ledgers: ` + "`nova-card generate -h`" + ` lists them.
A ledger card is flash and a findings or help card is pro unless --tier says otherwise; a card
whose PATHS name TLA+ model work (a .tla module, an MC config under tla/) is frontier, as
nova-sprint add tiers it, and --tier flash or pro on such a card is a red line
(check=model-tier). The TLC run records tla/RUNS.tsv and tla/CASES.tsv alone are no model.
A ledger plan is one wave with every card chained to the one before it with DEPENDS-ON: the
lander resolves a ledger conflict as the union of removals, so adjacent deletions of one
file no longer conflict at land (docs/SPEC-SPRINT.md section 7). Every card is wave 1, and
cards that share the ledger's path are chained, so no two independent cards share a file; the
lint holds a brief to the lint nova-sprint add runs (the model lines, the child rules under the
default rule set, a tree card's steps), and past the add to the typed header and the template's
unfilled <...> lines, which the add does not read, one LINT DRIFT line each; and to the card
checks: a tier on line 1, a TEST whose package PATHS names, no name --name gives outside
double-quoted words, no card --dropped gives. generate holds every brief the same before it
writes. A sprint initialised with --rules holds a brief to that file at
the add. template prints nova-swarm's card template, the shape every generated brief has.

what it prints:
  CARDS OK dir=<dir> cards=<n> waves=<k> tier=<t> [frontier=<n>] [shared-paths=yes]   then manifest.tsv in <dir> (--dry-run: the manifest on stdout, dry-run=yes)
  CARDS NOTE <what was skipped: a row the ledger did not read, a tool with no help>
  LINT DRIFT card=<id> check=<check> line=<n>: <excerpt>              and nothing is written
  LINT OK file=<file>

exit codes: 0 done; 1 a brief is red, named on its LINT DRIFT line, and nothing was written;
2 could not run: a missing flag, a source that cannot be read, a checkout with no HEAD

example:
  nova-card generate --from findings --file ./cmd/nova-card/testdata/findings.tsv --repo example/repo --base dev --sha 0123456789abcdef0123456789abcdef01234567 --out ./cards
  nova-card lint --card ./cards/finding-internal-bus-send.md
  nova-card lint --card ./cards/finding-cmd-nova-bus-main.md
`

var verbs = []string{"generate", "lint", "template", "version", "help"}

// effects is each verb's effect line for its -h (docs/CLI-STYLE.md rule (b)).
var effects = map[string]string{
	"generate": "local write: creates --out and writes one .md per card and manifest.tsv into it; nothing when a brief is red; --dry-run plans, lints and prints the manifest, and writes nothing",
	"lint":     "inspection: reads, writes nothing",
	"template": "inspection: prints the card template, writes nothing",
	"version":  "inspection: prints the build identity",
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// refusalJSON is a refusal as a machine reads it under --json: the verb, the
// fleet's refused status, the exit code, why and the remedy, the fields the
// other tools' refusal objects carry (docs/STANDARD.md section 3).
type refusalJSON struct {
	Verb   string `json:"verb"`
	Status string `json:"status"`
	Exit   int    `json:"exit"`
	Reason string `json:"reason"`
	Remedy string `json:"remedy"`
}

// refuse is one line on stderr, `nova-card[ <verb>] REFUSED: <what>; run: nova-card help[ <verb>]`,
// exit 2 (docs/STANDARD.md section 3).
func refuse(stderr io.Writer, verb, what string) int {
	return refuseWith(io.Discard, stderr, false, verb, what)
}

// refuseWith is refuse, and under --json the same refusal as one object on
// stdout with an empty stderr, so a caller reads the refusal as data the way it
// reads every other --json result (docs/STANDARD.md section 3). A caller for a
// verb that takes --json passes asJSON from jsonAsked.
func refuseWith(stdout, stderr io.Writer, asJSON bool, verb, what string) int {
	where, door := "nova-card", "nova-card help"
	if verb != "" {
		where += " " + verb
		door += " " + verb
	}
	if asJSON {
		if b, err := json.Marshal(refusalJSON{Verb: verb, Status: "refused", Exit: 2, Reason: what, Remedy: door}); err == nil {
			fmt.Fprintln(stdout, string(b))
			return 2
		}
	}
	fmt.Fprintf(stderr, "%s REFUSED: %s; run: %s\n", where, oneline.Escape(what), door)
	return 2
}

// jsonAsked reports whether args ask for --json, read as the flag package reads
// a boolean flag: `--json` and `-json` ask, `--json=false` does not, and a
// `--json` after `--` is an argument, not the flag. It reads a refusal before
// the flag set parsed, so a refusal asked for as --json is rendered as JSON
// (docs/STANDARD.md section 3).
func jsonAsked(args []string) bool {
	for _, a := range args {
		if a == "--" {
			break
		}
		if len(a) < 2 || a[0] != '-' {
			continue
		}
		name, val, inline := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if name != "json" {
			continue
		}
		if !inline {
			return true
		}
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
		return false
	}
	return false
}

func run(args []string, stdout, stderr io.Writer) (code int) {
	defer verbflag.RecoverWith(stdout, "nova-card", usage, &code, func(verb string) string {
		return "effect: " + effects[verb] + "\n"
	})
	if len(args) == 0 {
		return refuse(stderr, "", "no verb; one of "+strings.Join(verbs, ", "))
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		if args[0] == "help" && len(args) > 1 && args[1] != "help" {
			if slices.Contains(verbs, args[1]) {
				return run([]string{args[1], "-h"}, stdout, stderr)
			}
			// `help <unknown>` refuses, naming the nearest verb and the list,
			// instead of dumping the banner at exit 0 (docs/STANDARD.md
			// section 3, refusing to guess)
			what := fmt.Sprintf("unknown verb %q; one of %s", args[1], strings.Join(verbs, ", "))
			if near := verbflag.Nearest(args[1], verbs); near != "" {
				what = fmt.Sprintf("unknown verb %q; did you mean %s? one of %s", args[1], near, strings.Join(verbs, ", "))
			}
			return refuseWith(stdout, stderr, jsonAsked(args[1:]), "", what)
		}
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "--version":
		verbflag.HelpIfAsked(args[1:], "version")
		return cmdVersion(args[1:], stdout, stderr)
	case "template":
		verbflag.HelpIfAsked(args[1:], "template")
		asJSON := jsonAsked(args[1:])
		if len(args) > 1 {
			return refuseWith(stdout, stderr, asJSON, "template", "takes no flags and no arguments")
		}
		text, err := swarm.Template("card")
		if err != nil {
			return refuseWith(stdout, stderr, asJSON, "template", err.Error())
		}
		fmt.Fprint(stdout, text)
		return 0
	case "lint":
		return cmdLint(args[1:], stdout, stderr)
	case "generate":
		return cmdGenerate(args[1:], stdout, stderr)
	}
	return refuse(stderr, "", fmt.Sprintf("unknown verb %q; one of %s", args[0], strings.Join(verbs, ", ")))
}

// lintFlags adds --name and --dropped to fs, the card checks' inputs from outside the
// brief (card.Options; the tree holds no deployment name), and returns their reader.
func lintFlags(fs *flag.FlagSet) func() card.Options {
	var names, dropped multi
	fs.Var(&names, "name", "a person, friend or machine `name` no brief may carry outside double-quoted words; repeat or comma separate for more")
	fs.Var(&dropped, "dropped", "the `id` of a card dropped off the table, which no brief may name; repeat or comma separate for more")
	return func() card.Options {
		return card.Options{Names: splitList(names), Dropped: splitList(dropped)}
	}
}

// splitList is a repeatable flag's values, each split on commas, blanks dropped.
func splitList(m multi) []string {
	var out []string
	for _, v := range m {
		for _, f := range strings.Split(v, ",") {
			if f = strings.TrimSpace(f); f != "" {
				out = append(out, f)
			}
		}
	}
	return out
}

// multi is a repeatable string flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// lintJSONFinding is one finding under --json.
type lintJSONFinding struct {
	Rule     string `json:"rule"`
	RuleName string `json:"rule_name"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Excerpt  string `json:"excerpt"`
	Remedy   string `json:"remedy"`
}

// lintJSONOutput is the structured JSON object emitted by nova-card lint --json.
type lintJSONOutput struct {
	Findings []lintJSONFinding `json:"findings"`
}

func cmdLint(args []string, stdout, stderr io.Writer) int {
	fs := verbflag.New("lint")
	var cards multi
	fs.Var(&cards, "card", "a brief `file` to hold to the add's lint; repeat for more")
	jsonOut := fs.Bool("json", false, "emit a structured JSON object with findings")
	opts := lintFlags(fs)
	// a refusal asked for as --json is one object on stdout (refuseWith;
	// docs/STANDARD.md section 3)
	asJSON := jsonAsked(args)
	deny := func(what string) int { return refuseWith(stdout, stderr, asJSON, "lint", what) }
	if err := verbflag.Parse(fs, args); err != nil {
		return deny(verbflag.Explain(fs, err))
	}
	if len(cards) == 0 || fs.NArg() > 0 {
		return deny("wants --card <file>, one per brief, and nothing else")
	}
	red := 0
	var jsonFindings []lintJSONFinding
	if *jsonOut {
		jsonFindings = make([]lintJSONFinding, 0)
	}
	for _, file := range cards {
		raw, err := os.ReadFile(file)
		if err != nil {
			return deny("cannot read " + file + ": " + err.Error())
		}
		id := strings.TrimSuffix(filepath.Base(file), ".md")
		findings := card.Lint(id, string(raw), opts())
		if *jsonOut {
			for _, f := range findings {
				jsonFindings = append(jsonFindings, lintJSONFinding{
					Rule:     f.Check,
					RuleName: f.Check,
					File:     file,
					Line:     f.Line,
					Excerpt:  f.Excerpt,
					Remedy:   card.Remedy(f.Check),
				})
			}
		} else {
			for _, f := range findings {
				fmt.Fprintln(stdout, oneline.Escape(f.String()))
			}
			if len(findings) == 0 {
				fmt.Fprintf(stdout, "LINT OK file=%s\n", oneline.Field(file))
			}
		}
		if len(findings) > 0 {
			red++
		}
	}
	if *jsonOut {
		b, err := json.Marshal(lintJSONOutput{Findings: jsonFindings})
		if err != nil {
			return deny(err.Error())
		}
		fmt.Fprintln(stdout, string(b))
	}
	if red > 0 {
		return 1
	}
	return 0
}

func cmdGenerate(args []string, stdout, stderr io.Writer) int {
	fs := verbflag.New("generate")
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
	opts := lintFlags(fs)
	// a refusal asked for as --json is one object on stdout (refuseWith;
	// docs/STANDARD.md section 3)
	asJSON := jsonAsked(args)
	deny := func(what string) int { return refuseWith(stdout, stderr, asJSON, "generate", what) }
	if err := verbflag.Parse(fs, args); err != nil {
		return deny(verbflag.Explain(fs, err))
	}
	if fs.NArg() > 0 {
		return deny(fmt.Sprintf("unexpected argument %q; every input is a flag", fs.Arg(0)))
	}
	if *out == "" {
		return deny("wants --out <dir>")
	}
	if *tier != "" && *tier != "flash" && *tier != "pro" {
		return deny(fmt.Sprintf("--tier %q; want flash or pro", *tier))
	}
	// the header: from the checkout, each line overridable
	h := cardgen.Header{Repo: *repo, Base: *base, Sha: *sha, Minutes: *minutes}
	if *repoDir != "" {
		if err := readCheckout(*repoDir, &h); err != nil {
			return deny(err.Error())
		}
	}
	switch {
	case h.Repo == "":
		return deny("no repository: --repo <owner/name>, or --repo-dir a checkout whose origin names one")
	case h.Base == "":
		return deny("no base branch: --base <branch>, or --repo-dir a checkout on a branch (a detached HEAD names none)")
	case !shaRE.MatchString(h.Sha):
		return deny(fmt.Sprintf("base sha %q is not 40 hex: --sha <40hex>, or --repo-dir a checkout with a HEAD", h.Sha))
	}
	var plan cardgen.Plan
	var notes []string
	switch *from {
	case "ledger":
		l, ok := cardgen.Ledgers[*ledger]
		if !ok {
			return deny(fmt.Sprintf("--ledger %q is not a ledger this build knows; one of %s", *ledger, strings.Join(cardgen.LedgerNames(), ", ")))
		}
		if *repoDir == "" {
			return deny("--from ledger reads the ledger from --repo-dir <dir>, a checkout of the repository at the base")
		}
		raw, err := os.ReadFile(filepath.Join(*repoDir, filepath.FromSlash(l.File)))
		if err != nil {
			return deny("cannot read the ledger " + l.File + " in " + *repoDir + ": " + err.Error())
		}
		rows, skipped := cardgen.ParseLedger(l, string(raw))
		notes = append(notes, skipped...)
		plan = cardgen.PlanLedger(l, rows, *prefix, *tier, *maxCards)
	case "findings":
		if *file == "" {
			return deny("--from findings wants --file <tsv>")
		}
		raw, err := os.ReadFile(*file)
		if err != nil {
			return deny("cannot read " + *file + ": " + err.Error())
		}
		findings, skipped := cardgen.ParseFindings(string(raw))
		notes = append(notes, skipped...)
		plan = cardgen.PlanFindings(findings, *prefix, *tier, *maxCards)
	case "help":
		if len(tools) == 0 {
			return deny("--from help wants --tool <name>, one per tool")
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
		return deny("wants --from ledger|findings|help")
	default:
		return deny(fmt.Sprintf("--from %q; want ledger, findings or help", *from))
	}
	if len(plan.Cards) == 0 {
		return deny("the source yields no card; nothing to write")
	}
	if dup := cardgen.DuplicateID(plan.Cards); dup != "" {
		return deny(fmt.Sprintf("card %s is planned twice (a --tool named twice?); a card is its file %s.md and a second one would overwrite it", dup, dup))
	}
	// render, lint, and check the paths against the checkout: nothing is written while
	// one brief is red
	briefs := make([]string, len(plan.Cards))
	red, frontier := 0, 0
	for i := range plan.Cards {
		c := &plan.Cards[i]
		c.Paths = card.Paths(h, *c) // computed from the START line, never typed (docs/SPEC-CARD-CONTRACT.md section 6)
		if *repoDir != "" {
			cardgen.NewTestFile(c, func(glob string) bool { return existsAt(*repoDir, glob) })
		}
		// a card whose PATHS name TLA+ model work is tiered frontier, as nova-sprint add
		// tiers it (sprint.ModelTier; docs/SPEC-SPRINT.md, the card decides its model): the
		// source's tier gives way, and a --tier below it is a red line with the add's reason
		model := len(sprint.ModelPaths(c.Paths)) > 0
		if model && *tier == "" {
			c.Tier = cardhdr.RouteFrontier
			frontier++
		}
		briefs[i] = cardgen.Render(h, *c)
		if _, _, why := sprint.ModelTier(briefs[i]); model && why != "" {
			fmt.Fprintln(stdout, oneline.Escape(cardgen.LintFinding{ID: c.ID, Check: "model-tier", Line: 1, Excerpt: why + "; or generate without --tier " + *tier}.String()))
			red++
		}
		for _, f := range card.Lint(c.ID, briefs[i], opts()) {
			fmt.Fprintln(stdout, oneline.Escape(f.String()))
			red++
		}
		if *repoDir != "" {
			for _, p := range c.Paths {
				if !existsAt(*repoDir, p) && !card.Answered(*c, p) {
					fmt.Fprintln(stdout, oneline.Escape(cardgen.LintFinding{ID: c.ID, Check: "paths-at-base", Line: 6, Excerpt: "PATHS entry " + p + " names nothing in " + *repoDir}.String()))
					red++
				}
			}
		}
	}
	plan.Shared = card.Shared(plan.Cards)
	if red > 0 {
		fmt.Fprintf(stderr, "nova-card generate FAILED: %d red line(s) above; nothing written to %s\n", red, oneline.Field(*out))
		return 1
	}
	if *dryRun {
		for _, n := range notes {
			fmt.Fprintf(stdout, "CARDS NOTE skipped %s\n", oneline.Escape(n))
		}
		fmt.Fprint(stdout, cardgen.Manifest(plan))
		fmt.Fprintln(stdout, cardgen.OKLine(*out, plan)+frontierWord(frontier)+" dry-run=yes (nothing written)")
		return 0
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return deny("cannot create --out " + *out + ": " + err.Error())
	}
	if held, _ := filepath.Glob(filepath.Join(*out, "*.md")); len(held) > 0 {
		return deny(fmt.Sprintf("--out %s already holds %d brief(s); name an empty directory", *out, len(held)))
	}
	for i, c := range plan.Cards {
		if err := os.WriteFile(filepath.Join(*out, c.ID+".md"), []byte(briefs[i]), 0o644); err != nil {
			return deny("cannot write " + c.ID + ".md: " + err.Error())
		}
	}
	if err := os.WriteFile(filepath.Join(*out, "manifest.tsv"), []byte(cardgen.Manifest(plan)), 0o644); err != nil {
		return deny("cannot write manifest.tsv: " + err.Error())
	}
	for _, n := range notes {
		fmt.Fprintf(stdout, "CARDS NOTE skipped %s\n", oneline.Escape(n))
	}
	line := cardgen.OKLine(*out, plan) + frontierWord(frontier)
	fmt.Fprintln(stdout, line)
	return 0
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// frontierWord is the CARDS OK line's count of the cards generate tiered frontier because
// their PATHS name TLA+ model work; "" when none.
func frontierWord(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" frontier=%d", n)
}

// readCheckout fills the header's empty fields from a checkout: the repository from
// origin's URL, the branch from HEAD's name, the sha from HEAD.
func readCheckout(dir string, h *cardgen.Header) error {
	git := func(args ...string) (string, error) {
		res, err := gitrun.Run(context.Background(), gitrun.Options{C: dir}, args...)
		if err != nil {
			return "", fmt.Errorf("git %s in %s: %s", strings.Join(args, " "), dir, strings.TrimSpace(cmp(string(res.Stderr), err.Error())))
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
		// a detached HEAD names no branch: symbolic-ref exits 1 and Base stays "",
		// refused by the caller (pkg/bus/git.go CurrentBranch reads it the same way)
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

func cmp(a, b string) string {
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
// lines (cardgen.ExampleTest over the package's _test.go files); "" with no checkout, no
// package or no such test.
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
