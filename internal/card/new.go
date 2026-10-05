package card

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
	"github.com/mas-bandwidth/nova-sprint/internal/nsprint/verbflag"
	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
)

// NewCommand is the words a line of the new verb names: it is a verb of the
// nova-card binary, not yet of nova-sprint card.
const NewCommand = "nova-card new"

// New writes a brief skeleton from its parts, held to the add's lint before a byte
// is written: to stdout, or to --out <file>; with --batch <tsv>, one brief per row
// into --out <dir> for nova-sprint add --brief-dir. Exit 0 is written, 1 is a red
// brief and nothing written, 2 is a missing or wrong part, named.
func New(args []string, stdout, stderr io.Writer) int {
	fs := verbflag.New("new")
	batch := fs.String("batch", "", "a tab-separated `file` of cards, one per row; writes one brief per row into --out <dir>")
	repo := fs.String("repo", "", "the `owner/name` the REPO: line carries")
	base := fs.String("base", "", "the `branch` the BASE: line carries")
	taskFile := fs.String("task-file", "", "the `file` whose text is THE TASK.")
	paths := fs.String("paths", "", "the `paths` the PATHS: line carries, comma separated")
	shared := fs.String("shared", "", "the `paths` the SHARED: line carries (default: no SHARED line)")
	test := fs.String("test", "", "the TEST: line, `<package> <TestName>` or `none <why>`")
	gate := fs.String("gate", "", "the `packages` STEP 4 runs go vet and go test on, space separated")
	tier := fs.String("tier", "", "the model `tier`: frontier, heavy, pro or flash")
	needs := fs.String("needs", "", "the card `id`s the DEPENDS-ON: line carries (default: -)")
	kind := fs.String("kind", "", "the KIND: line (default: fix-red)")
	minutes := fs.Int("minutes", 0, "the Deadline line's `minutes` (default: 45, 60 for pro)")
	out := fs.String("out", "", "the `file` the brief is written to (default: stdout); with --batch, the dir")
	var id string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := verbflag.Parse(fs, args); err != nil {
		return refuseNew(stderr, verbflag.Explain(fs, err))
	}
	if fs.NArg() > 0 {
		return refuseNew(stderr, fmt.Sprintf("unexpected argument %q; the card id goes first, every other part is a flag", fs.Arg(0)))
	}
	s := Skeleton{ID: id, Repo: *repo, Base: *base, Paths: *paths, Shared: *shared, Test: *test,
		Gate: *gate, Tier: *tier, Needs: *needs, Kind: *kind, Minutes: *minutes}
	if *batch != "" {
		if id != "" {
			return refuseNew(stderr, fmt.Sprintf("unexpected card id %q with --batch; the ids are the table's", id))
		}
		if *taskFile != "" {
			return refuseNew(stderr, "--task-file with --batch; each row names its own task")
		}
		return newBatch(*batch, *out, s, stdout, stderr)
	}
	if *taskFile != "" {
		raw, err := os.ReadFile(*taskFile)
		if err != nil {
			return refuseNew(stderr, "cannot read --task-file "+*taskFile+": "+err.Error())
		}
		s.Task = string(raw)
		if strings.TrimSpace(s.Task) == "" {
			return refuseNew(stderr, "--task-file "+*taskFile+" is empty")
		}
	}
	if missing := s.Missing(); len(missing) > 0 {
		return refuseNew(stderr, "missing "+strings.Join(missing, ", "))
	}
	if why := s.Check(); why != "" {
		return refuseNew(stderr, why)
	}
	brief := RenderSkeleton(s)
	if red := printLint(stdout, s.ID, brief); red > 0 {
		fmt.Fprintf(stderr, "%s FAILED: %d red line(s) above; nothing written\n", NewCommand, red)
		return 1
	}
	if *out == "" {
		fmt.Fprint(stdout, brief)
		return 0
	}
	if err := os.WriteFile(*out, []byte(brief), 0o644); err != nil {
		return refuseNew(stderr, "cannot write --out "+*out+": "+err.Error())
	}
	fmt.Fprintf(stdout, "CARD OK file=%s\n", oneline.Field(*out))
	return 0
}

func newBatch(table, out string, def Skeleton, stdout, stderr io.Writer) int {
	if out == "" {
		return refuseNew(stderr, "--batch wants --out <dir>")
	}
	raw, err := os.ReadFile(table)
	if err != nil {
		return refuseNew(stderr, "cannot read --batch "+table+": "+err.Error())
	}
	rows, err := ParseBatch(string(raw), def, nil)
	if err != nil {
		return refuseNew(stderr, "--batch "+table+": "+err.Error())
	}
	if len(rows) == 0 {
		return refuseNew(stderr, "--batch "+table+" holds no card")
	}
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.ID] {
			return refuseNew(stderr, fmt.Sprintf("--batch %s: card %s is named twice; a card is its file %s.md", table, r.ID, r.ID))
		}
		seen[r.ID] = true
		if missing := r.Missing(); len(missing) > 0 {
			return refuseNew(stderr, fmt.Sprintf("--batch %s: card %s is missing %s", table, r.ID, strings.Join(missing, ", ")))
		}
		if why := r.Check(); why != "" {
			return refuseNew(stderr, fmt.Sprintf("--batch %s: card %s: %s", table, r.ID, why))
		}
	}
	briefs := make([]string, len(rows))
	red := 0
	for i, r := range rows {
		briefs[i] = RenderSkeleton(r.Skeleton)
		red += printLint(stdout, r.ID, briefs[i])
	}
	if red > 0 {
		fmt.Fprintf(stderr, "%s FAILED: %d red line(s) above; nothing written to %s\n", NewCommand, red, oneline.Field(out))
		return 1
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return refuseNew(stderr, "cannot create --out "+out+": "+err.Error())
	}
	if held, _ := filepath.Glob(filepath.Join(out, "*.md")); len(held) > 0 {
		return refuseNew(stderr, fmt.Sprintf("--out %s already holds %d brief(s); name an empty directory", out, len(held)))
	}
	for i, r := range rows {
		if err := os.WriteFile(filepath.Join(out, r.ID+".md"), []byte(briefs[i]), 0o644); err != nil {
			return refuseNew(stderr, "cannot write "+r.ID+".md: "+err.Error())
		}
		fmt.Fprintf(stdout, "CARD %s task=%s\n", r.ID, oneline.Escape(r.TaskFrom))
	}
	fmt.Fprintf(stdout, "CARDS OK dir=%s cards=%d\n", oneline.Field(out), len(rows))
	return 0
}

// printLint prints the add's lint findings of one brief and returns their count.
func printLint(stdout io.Writer, id, brief string) int {
	findings := cardgen.Lint(id, brief)
	for _, f := range findings {
		fmt.Fprintln(stdout, oneline.Escape(f.String()))
	}
	return len(findings)
}

// refuseNew is one line, `nova-card new REFUSED: <what>; run: nova-card new -h`, exit 2.
func refuseNew(stderr io.Writer, what string) int {
	fmt.Fprintf(stderr, "%s REFUSED: %s; run: %s -h\n", NewCommand, oneline.Escape(what), NewCommand)
	return 2
}
