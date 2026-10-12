// Command nova-sprint issues holds every issue of every repository of an organization
// in one tree file, with each issue's full contents, and proves the tree
// holds exactly what the source holds (docs/SPEC-WORK-V1.md section 1).
//
// import reads the source, read-only, and writes the tree; verify reads the
// source again and compares it with the tree field for field. Neither verb
// writes to the source: the GitHub seam refuses any document that is not a
// query. The dispatch, the banner, the help, the version verb, the refusals
// and the output (typed lines or --json of one value) are pkg/tool's.
package issues

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/roadmap"
	"github.com/mas-bandwidth/nova-sprint/internal/workfile"
	"github.com/mas-bandwidth/nova-sprint/internal/workgh"
	"github.com/mas-bandwidth/nova-sprint/pkg/atomicfile"
	"github.com/mas-bandwidth/nova-sprint/pkg/nsprint/verbflag"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
	"github.com/mas-bandwidth/nova-sprint/pkg/tool"
)

var version string

// New is the issues group's tool over the real GitHub.
func New() *tool.Tool { return workTool(realGitHub()) }

// Main is the issues group's entry point: run the sub-tool over the process's
// argument list (once the group's verb already consumed) and its streams.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return New().Run(args, stdin, stdout, stderr)
}

// FlagSet is the flag set of the verb named, named for the app's help (the
// group's word and the verb), so the app renders the group's -h through its
// own help with the concrete synopsis from its verb table and the verb's
// flags, and no [flags] placeholder prints. It is the tool's own flag set
// (tool.Verb.flags) under the app's name.
func FlagSet(name string) *flag.FlagSet {
	for _, v := range New().Verbs {
		if v.Name != name {
			continue
		}
		f := &tool.Flags{FlagSet: verbflag.New("issues " + name)}
		if v.Flags != nil {
			v.Flags(f)
		}
		if v.DryRun {
			f.Bool("dry-run", false, "print what the verb would write and write nothing")
		}
		f.Bool("json", false, "print the result as one JSON object instead of lines")
		return f.FlagSet
	}
	return nil
}

// github is how the verbs reach GitHub and the clock: where gh is found, the
// query that runs it, and the time. main's runs the real gh; a test's answers
// from a recorded conversation (workgh.Replay), starts no process and passes a
// fixed time in, so the logic is tested apart from the transport.
type github struct {
	lookPath func(file string) (string, error)
	query    func(program string) workgh.Query
	now      func() time.Time
}

func realGitHub() github {
	return github{lookPath: exec.LookPath, query: workgh.GhQuery, now: time.Now}
}

// preAlpha is the tool's stage, in the same words in its banner, every verb's
// -h, docs/CLI.md, docs/SPEC-WORK-V1.md and the README row (internal/docs
// holds the README, the banner source and the reference to it).
const preAlpha = "nova-sprint issues is pre-alpha: not ready for production use."

func workTool(gh github) *tool.Tool {
	return &tool.Tool{
		Name:  "nova-sprint issues",
		What:  "every issue of an organization's repositories in one tree file, verified field for field; a repository's roadmap and fixes edited as data",
		Stage: preAlpha,
		Stamp: version,
		How: `import reads every issue through your gh login, read-only, into one tree file.
--dry-run reads GitHub exactly as the import does (every issue, the same calls) and writes nothing.
verify reads GitHub again: one MISSING, EXTRA or DRIFT line per difference; none is the proof.
verify --against compares two tree files and reads no network (a minimal tree: verify -h).
first run: gh logged in (gh auth status); export ORG and REPO, a repository you can read.`,
		ExitTable: "0 done, or verify found no difference; 1 verify found differences, or an import's " +
			"encoded tree did not read back equal, or roadmap check found problems; 2 could not run (a flag, the budget, gh, GitHub, a file)",
		UsageNote:  roadmapUsageNote,
		GroupNotes: map[string]string{"roadmap": roadmapUsageNote},
		Verbs: append(append([]tool.Verb{
			{
				Name: "import",
				Usage: "import --org <org> (--out <tree.lisp> [--replace] | --dry-run) [--repo <owner/name>]... [--fixture <dir>] [--max-calls <n>] " +
					"[--page-size <n>] [--gh <path>] [--timeout <d>]",
				Example: `import --org $ORG --repo $ORG/$REPO --page-size 15 --dry-run
import --org $ORG --repo $ORG/$REPO --page-size 15 --out ./tree.lisp`,
				Effect: "local write: writes the tree file --out names; reads GitHub through gh, read-only; " +
					"--dry-run reads GitHub exactly as the import does and writes nothing",
				Detail:    importDetail,
				ExitTable: "0 the tree is written (with --dry-run: read and checked, nothing written); 1 the encoded tree did not read back equal to what was fetched, nothing written; 2 could not run (a flag, the budget, gh, GitHub, the --out directory)",
				DryRun:    true,
				Flags: func(f *tool.Flags) {
					f.Required("org", "the organization whose repositories are read, as GitHub spells it")
					f.String("out", "", "the tree `file` to write, created, or replaced with --replace; its directory must exist; required unless --dry-run")
					f.String("fixture", "", "a directory of recorded GraphQL pages (call-01.json, call-02.json, each with vars and reply) read instead of gh, so import can be tried with no login")
					f.Bool("replace", false, "replace an existing --out file instead of refusing")
					sourceFlags(f)
					f.Check(checkImport)
				},
				Run: gh.importTree,
			},
			{
				Name: "verify",
				Usage: "verify --tree <tree.lisp> [--repo <owner/name>]... [--max <n>] [--max-calls <n>] [--page-size <n>] " +
					"[--gh <path>] [--timeout <d>] [--max-bytes <n>]\n" +
					"verify --tree <tree.lisp> --against <tree.lisp> [--repo <owner/name>]... [--max <n>] [--max-bytes <n>]",
				Example:   "verify --tree ./tree.lisp --repo $ORG/$REPO --page-size 15",
				Effect:    "inspection: reads the tree, and GitHub through gh, read-only (with --against, a second tree file and no network); writes nothing",
				Detail:    verifyDetail,
				ExitTable: "0 no difference; 1 one or more differences; 2 could not run (a flag, an unreadable or refused tree, the budget, gh, GitHub)",
				Flags: func(f *tool.Flags) {
					f.Required("tree", "the tree file to compare, as import wrote it")
					f.String("against", "", "a second tree `file` to compare with in place of GitHub: no gh and no network")
					f.Max()
					f.Int("max-bytes", 1<<27, "the largest tree file read, in bytes (default 134217728)")
					sourceFlags(f)
					f.Check(func(c *tool.Call) {
						if c.Int("max-bytes") <= 0 {
							c.Problem(fmt.Sprintf("--max-bytes must be positive (got %d); it wants the largest tree file read, in bytes", c.Int("max-bytes")))
						}
						if c.Str("against") == "" {
							return
						}
						for _, name := range []string{"gh", "max-calls", "page-size", "timeout"} {
							if c.Given(name) {
								c.Problem(fmt.Sprintf("--%s reads GitHub and --against reads no network; drop --%s or --against", name, name))
							}
						}
					})
				},
				Run: gh.verifyTree,
			},
		}, gh.roadmapVerbs()...),
			tool.Verb{
				// help is the door every tool has; declaring it puts it in the
				// skeleton's verb list, which an unknown-verb refusal names
				// (STANDARD section 2, the names there are). It carries no
				// usage line: the banner prints its own `help [<verb>]` line.
				Name:   "help",
				Effect: tool.Inspection,
				Run:    func(*tool.Call) *tool.Out { return tool.Done() },
			}),
	}
}

// roadmapUsageNote is the banner's word under the usage block: where the roadmap
// verbs went and what they edit now.
const roadmapUsageNote = `roadmap: the verbs edit this repository's own docs/roadmap.sexp and docs/fixes.sexp and write
ROADMAP.md and FIXES.md in the same step, locally, with no GitHub call per entry. The old roadmap
verbs, which edited the private work record's roadmaps/*.sexp, are gone (the v1.2.3 re-seed);
these replace them. nova-sprint issues roadmap -h lists them; nova-sprint issues help roadmap check gives the shapes.`

const importDetail = `Reads every issue (open and closed) of every repository of --org (or of each
--repo) with its full contents: number, url, node id, title, body, state and
state reason, author and association, origin (internal or external), created,
updated and closed times, lock, labels, assignees, milestone, every comment,
every cross-reference to it, and the pull requests that close it. Nothing is
cut at a bound: a connection longer than one page is read to its end. The tree
is written to --out only after it has been encoded, read back and compared
with what was fetched, with zero differences. --dry-run reads GitHub exactly
as the import does (every issue, read-only, the calls IMPORT PLAN counts),
checks the round trip, and writes nothing: it needs gh and the network.
--fixture <dir> reads recorded GraphQL pages from that directory (call-01.json,
call-02.json, each with vars and reply) instead of gh, so no login is needed.
The first run of import is: nova-sprint issues import --org $ORG --repo $ORG/$REPO --page-size 15 --fixture ./calls --dry-run

output: IMPORT OK with the counts, bytes=, the tree's sha256=, calls= (GraphQL
calls), points= (what GitHub charged), rest=0 and gh= (the gh run); then
IMPORT PLAN (est_calls, checked against --max-calls before any issue is read)
and one IMPORT REPO per repository. A dry run adds dry_run=true, out=-, and an
IMPORT NOTE naming the calls it read and that it wrote nothing.
`

const verifyDetail = `Reads the tree, reads the same repositories from GitHub again (read-only), and
compares them field for field. Every difference is one line:

  VERIFY MISSING path=<path> field=<field> want="<value>"   on GitHub, not in the tree
  VERIFY EXTRA path=<path> field=<field> got="<value>"      in the tree, not on GitHub
  VERIFY DRIFT path=<path> field=<field> want="<value>" got="<value>"

A path is repos/<owner>/<repo>/issues/<n>, with /comments/<id>, /references or
/linked-prs below it. A value longer than 80 bytes, or of more than one line,
is shown as its length and the head of its SHA-256. With no --repo, the scope
is the tree's organization: every repository GitHub lists for it and every
repository the tree holds. An issue edited on GitHub after the import is DRIFT
on its updated time and the fields that changed: that is the check working.

--against <file> puts a second tree where GitHub stands: want is its value,
MISSING is in it and not in --tree. Nothing is read from the network. To try
verify with no gh: save the minimal tree below as a.lisp, copy it to b.lisp
with :archived true, and run nova-sprint issues verify --tree a.lisp --against b.lisp:
one VERIFY DRIFT line for field=archived, under VERIFY FAILED, exit 1.

the tree file (docs/SPEC-WORK-V1.md section 1.2) is one (work-tree "v1" ...)
record. The smallest a reader accepts, one repository and no issue:

` + minimalTree + `
An issue in :issues is (issue <number> :url "..." ...) with every one of these
keys, in any order: url node-id title state state-reason origin author
author-association created updated closed locked lock-reason labels assignees
milestone body comments references linked-prs. import writes them all.

Each key's value shape:

  :source               string
  :org                  string
  :fetched              string
  :url                  string
  :node-id              string
  :title                string
  :author               string
  :created              string
  :updated              string
  :closed               string
  :body                 string
  :kind                 string
  :repo                 string
  :actor                string
  :at                   string
  :state                keyword
  :state-reason         keyword or ()
  :origin               :internal or :external
  :author-association   keyword or ()
  :lock-reason          keyword or ()
  :archived             true or false
  :locked               true or false
  :will-close           true or false
  :labels               list of strings
  :assignees            list of strings
  :repos                list of repo
  :issues               list of issue
  :comments             list of comment
  :references           list of ref
  :linked-prs           list of pr
  :milestone            () or (:number <positive integer> :title <string>)
  :number               positive integer, or 0 when a reference has no source
`

// minimalTree is the smallest tree the reader accepts, as verify -h prints
// it; a test decodes it, so the help cannot show a tree the tool refuses.
const minimalTree = `  (work-tree "v1" :source "github" :org "acme" :fetched "2026-10-02T12:00:00Z"
   :repos ((repo "acme/widgets" :url "https://github.com/acme/widgets"
            :archived false :issues ())))
`

// sourceFlags are the flags of a read of GitHub, the same on both verbs.
func sourceFlags(f *tool.Flags) {
	f.Var(&repoList{}, "repo", "read only this repository, as `owner/name`; repeat for more (default: every repository of the organization)")
	f.Int("max-calls", 1500, "the GitHub call budget of the run (default 1500); the estimate is checked against it before any issue is read; 0 is refused")
	f.Int("page-size", 50, "issues per GraphQL page, 1 to 100 (default 50); a page GitHub fails to answer is asked again at half the size")
	f.String("gh", "", "the GitHub CLI to run, a `path` (default: gh on PATH); the path found is echoed as gh=")
	f.Duration("timeout", 30*time.Minute, "the whole run's deadline (default 30m)")
	f.Check(func(c *tool.Call) {
		if c.Int("max-calls") <= 0 {
			c.Problem(fmt.Sprintf("--max-calls must be positive (got %d); it wants the GitHub call budget of the run", c.Int("max-calls")))
		}
		if p := c.Int("page-size"); p < 1 || p > 100 {
			c.Problem(fmt.Sprintf("--page-size must be 1 to 100 (got %d); it wants the issues per GraphQL page", p))
		}
		if c.Dur("timeout") <= 0 {
			c.Problem("--timeout must be positive; it wants the whole run's deadline, such as 30m")
		}
	})
}

// repoList is --repo, repeated: owner/name each.
type repoList []string

func (r *repoList) String() string { return strings.Join(*r, ",") }
func (r *repoList) Get() any       { return []string(*r) }
func (r *repoList) Set(v string) error {
	owner, name, ok := strings.Cut(v, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return fmt.Errorf("%q is not owner/name", v)
	}
	*r = append(*r, v)
	return nil
}

func repos(c *tool.Call) []string { return c.Get("repo").([]string) }

// open finds gh (the --gh path, else gh on PATH) and returns the query that
// runs it with the path found, or the refusal naming what is missing.
func (g github) open(c *tool.Call, verb string) (workgh.Query, string, *tool.Out) {
	prog := c.Str("gh")
	if prog == "" {
		prog = workgh.DefaultProgram()
	}
	found, err := g.lookPath(prog)
	if err != nil {
		o := tool.Refuse(fmt.Sprintf("the GitHub CLI %q is not found (%v); install it, or name it with --gh <path>", prog, err))
		o.Remedy = "nova-sprint issues " + verb + " -h"
		return nil, "", o
	}
	return g.query(found), found, nil
}

// ghRemedy is the next command when GitHub did not answer: the login check of
// the gh this run used.
func ghRemedy(path string) string { return oneline.ShellWord(path) + " auth status" }

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func dirExists(path string) bool {
	fi, err := os.Stat(filepath.Dir(path))
	return err == nil && fi.IsDir()
}

// fileExists reports whether path names a regular file already on disk, so
// import can refuse to replace it without --replace (docs/SPEC-WORK-V1.md
// section 1.6, the write through a temporary file and a rename).
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// writeFile writes data to path through a temporary file in the same
// directory and a rename, so a reader never sees half a tree.
func writeFile(path string, data []byte) error {
	return atomicfile.Write(filepath.Clean(path), data, 0o600, atomicfile.ExactMode())
}

// checkImport is import's rules over its flags, run with every other rule so
// one invocation names every problem. A dry run with no --repo, and an --out
// that already exists, are refused here, before any GitHub call
// (docs/SPEC-WORK-V1.md section 1.6; docs/STANDARD.md section 2, ONBOARDING
// point 2): the dry run would spend the organization's whole call budget, and
// --out would replace an existing tree without --replace.
func checkImport(c *tool.Call) {
	org, out, dry := c.Str("org"), c.Str("out"), c.Bool("dry-run")
	switch {
	case out == "" && !dry:
		c.Problem("--out is required unless --dry-run; it wants the tree file to write")
	case out != "" && dry:
		c.Problem("--out and --dry-run exclude each other; a dry run writes nothing")
	case !dirExists(out):
		c.Problem(fmt.Sprintf("the directory of --out %q does not exist; make it first, or name a file in one that does", out))
	}
	if dry && len(repos(c)) == 0 {
		c.Problem(fmt.Sprintf("--dry-run with no --repo reads every repository of --org %s, up to --max-calls %d calls; name one repository and run: nova-sprint issues import --org %s --repo %s/<name> --dry-run",
			org, c.Int("max-calls"), oneline.ShellWord(org), oneline.ShellWord(org)))
	}
	if out != "" && !c.Bool("replace") && fileExists(out) {
		c.Problem(fmt.Sprintf("--out %s exists; pass --replace to replace it: nova-sprint issues import --org %s --out %s --replace",
			oneline.ShellWord(out), oneline.ShellWord(org), oneline.ShellWord(out)))
	}
	for _, r := range repos(c) {
		if o, _, _ := strings.Cut(r, "/"); org != "" && o != org {
			c.Problem(fmt.Sprintf("--repo %s is not in --org %s", r, org))
		}
	}
}

// importTree is layer 1's import (SPEC-WORK-V1 section 1.5): read-only,
// non-destructive, every issue of every repository in scope, checked
// through the file before it is written. tla/WorkImport.tla's StartImport,
// Fetch and WriteTree actions. A dry run reads all the same and writes
// nothing.
// --fixture reads recorded GraphQL pages (call-NN.json) through
// workgh.Replay instead of gh (SPEC-WORK-V1 section 1.6).
func (g github) importTree(c *tool.Call) *tool.Out {
	org, out, dry := c.Str("org"), c.Str("out"), c.DryRun()
	fixture := c.Str("fixture")
	var (
		q      workgh.Query
		ghPath string
	)
	if fixture != "" {
		var err error
		q, err = workgh.Replay(fixture)
		if err != nil {
			o := tool.Refuse(err.Error())
			o.Remedy = "nova-sprint issues import -h"
			return o.Fact("org", org).Fact("fixture", fixture)
		}
	} else {
		var refused *tool.Out
		q, ghPath, refused = g.open(c, "import")
		if refused != nil {
			return refused
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.Dur("timeout"))
	defer cancel()
	start := g.now()
	var retries bytes.Buffer
	f := &workgh.Fetcher{Q: q, PageSize: c.Int("page-size"), MaxCalls: c.Int("max-calls"), Log: &retries, Remaining: -1}
	// ended gives every result, OK or not, the calls spent, the gh run and
	// the pages retried; more are facts of its own after the calls.
	ended := func(o *tool.Out, more ...tool.Field) *tool.Out {
		o.Facts = append(tool.Fields{{K: "org", V: org}}, o.Facts...)
		o.Fact("calls", f.Calls).Fact("points", f.Points)
		o.Facts = append(o.Facts, more...)
		if fixture != "" {
			o.Fact("fixture", fixture)
		} else {
			o.Fact("gh", ghPath)
		}
		for _, l := range strings.Split(strings.TrimSpace(retries.String()), "\n") {
			if l != "" {
				o.Note(l)
			}
		}
		return o
	}
	refuse := func(err error) *tool.Out {
		o := tool.Refuse(err.Error())
		o.Remedy = ghRemedy(ghPath)
		if fixture != "" {
			o.Remedy = "nova-sprint issues import -h"
		}
		if errors.Is(err, workgh.ErrBudget) {
			o.Remedy = importAgain(c, 2*c.Int("max-calls"))
		}
		return ended(o)
	}

	metas, err := scope(ctx, f, org, repos(c))
	if err != nil {
		return refuse(err)
	}
	issues, est := 0, f.Calls
	for _, m := range metas {
		issues += m.Issues
		est += pages(m.Issues, c.Int("page-size"))
	}
	plan := []any{"repos", len(metas), "issues", issues, "est_calls", est, "max_calls", c.Int("max-calls"), "page_size", c.Int("page-size")}
	if est > c.Int("max-calls") {
		o := tool.Refuse(fmt.Sprintf("the plan needs about %d calls and --max-calls is %d; narrow it with --repo or raise --max-calls", est, c.Int("max-calls")))
		o.Remedy = importAgain(c, est)
		return ended(o).Item("plan", plan...)
	}

	tree := &workfile.Tree{Source: "github", Org: org, Fetched: start.UTC().Format(time.RFC3339)}
	// A scoped import records its scope (:scope), so verify with no --repo
	// compares the repositories imported, not the whole organization
	// (SPEC-WORK-V1 sections 1.2 and 1.6). A whole-organization import writes
	// no :scope. metas is sorted by name, so the scope is canonical.
	if len(repos(c)) > 0 {
		tree.Scope = make([]string, len(metas))
		for i, m := range metas {
			tree.Scope[i] = m.Name
		}
	}
	var perRepo [][]any
	for _, m := range metas {
		before := f.Calls
		r, err := f.Issues(ctx, m)
		if err != nil {
			return refuse(err)
		}
		tree.Repos = append(tree.Repos, r)
		cnt := (&workfile.Tree{Repos: []workfile.Repo{r}}).Count()
		perRepo = append(perRepo, []any{"repo", m.Name, "issues", cnt.Issues, "comments", cnt.Comments,
			"references", cnt.References, "linked_prs", cnt.LinkedPRs, "calls", f.Calls - before})
	}

	data, err := workfile.Encode(tree)
	if err != nil {
		o := tool.Refuse("the tree cannot be written without loss: " + err.Error())
		o.Remedy = "nova-sprint issues import -h"
		return ended(o)
	}
	// The round trip through the file, before anything is written: the tree
	// read back must equal what was fetched, field for field.
	back, err := workfile.Decode("(encoded)", data, workfile.Limits(len(data)+1))
	if err != nil {
		return ended(tool.Fail("the encoded tree does not read back: " + err.Error()))
	}
	// The scope must survive the file too: a scoped tree that lost its
	// :scope would verify against the whole organization (SPEC-WORK-V1
	// sections 1.2 and 1.6).
	if !slices.Equal(back.Scope, tree.Scope) {
		return ended(tool.Fail(fmt.Sprintf("the encoded tree reads back with scope %v, want %v", back.Scope, tree.Scope)))
	}
	if diffs := workfile.Diff(back, tree, nil); len(diffs) > 0 {
		d := diffs[0]
		return ended(tool.Fail(fmt.Sprintf("the encoded tree reads back with %d differences, first %s %s %s", len(diffs), d.Kind, d.Path, d.Field)))
	}
	outField := "-"
	if !dry {
		if err := writeFile(out, data); err != nil {
			o := tool.Refuse(fmt.Sprintf("write %s: %v", out, err))
			o.Remedy = "nova-sprint issues import -h"
			return ended(o)
		}
		outField = out
	}
	cnt := tree.Count()
	o := tool.Done().Fact("out", outField).Fact("repos", cnt.Repos).Fact("issues", cnt.Issues).
		Fact("comments", cnt.Comments).Fact("references", cnt.References).Fact("linked_prs", cnt.LinkedPRs).
		Fact("bytes", len(data)).Fact("sha256", sum(data))
	o = ended(o, tool.Field{K: "rest", V: 0}, tool.Field{K: "seconds", V: fmt.Sprintf("%.1f", g.now().Sub(start).Seconds())})
	o.Item("plan", plan...)
	for _, r := range perRepo {
		o.Item("repo", r...)
	}
	if dry {
		// A dry run is not offline: it reads what the import reads. The run
		// says so, not only the help. A --fixture run read the recording.
		if fixture != "" {
			o.Note(fmt.Sprintf("the dry run read the recorded pages in the fixture (calls=%d) and wrote nothing", f.Calls))
		} else {
			o.Note(fmt.Sprintf("the dry run read GitHub as the import does (calls=%d, read-only) and wrote nothing", f.Calls))
		}
	}
	return o
}

// importAgain is the import as it was asked, with --max-calls set to n: the
// remedy of a run the budget stopped.
func importAgain(c *tool.Call, n int) string {
	return again(c, "import", []string{"org", "repo", "out", "replace", "dry-run", "fixture", "page-size", "gh", "timeout", "max-calls"},
		map[string]string{"max-calls": fmt.Sprint(n)})
}

// again is the verb as it was asked, every flag given in the order named,
// with set's values in place of the ones given: a remedy that keeps the run's
// inputs (CLI-STYLE (k)). Every value is one shell word (oneline.ShellWord),
// so a path with a blank, a quote or a $ is pasted back as that path.
func again(c *tool.Call, verb string, order []string, set map[string]string) string {
	cmd := []string{"nova-sprint issues", verb}
	for _, name := range order {
		v, override := set[name]
		switch {
		case name == "repo":
			for _, r := range repos(c) {
				cmd = append(cmd, "--repo", oneline.ShellWord(r))
			}
		case name == "dry-run" || name == "replace":
			if c.Bool(name) {
				cmd = append(cmd, "--"+name)
			}
		case override:
			cmd = append(cmd, "--"+name, oneline.ShellWord(v))
		case c.Given(name):
			cmd = append(cmd, "--"+name, oneline.ShellWord(fmt.Sprint(c.Get(name))))
		}
	}
	return strings.Join(cmd, " ")
}

// scope is the repositories a run reads: every repository of org, or the
// ones named, each read from GitHub so its issue count plans the run.
func scope(ctx context.Context, f *workgh.Fetcher, org string, named []string) ([]workgh.RepoMeta, error) {
	if len(named) == 0 {
		return f.Repos(ctx, org)
	}
	var out []workgh.RepoMeta
	seen := map[string]bool{}
	for _, n := range named {
		if seen[n] {
			continue
		}
		seen[n] = true
		m, err := f.Repo(ctx, n)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func pages(issues, size int) int {
	if issues == 0 {
		return 1
	}
	return (issues + size - 1) / size
}

// verifyTree is layer 1's check (SPEC-WORK-V1 section 1.6): a fresh read of
// the source compared with the tree field for field; zero differences is the
// receipt tla/WorkImport.tla's Verify action records. Like that Verify, it
// compares only the repositories the tree was imported with (its :scope),
// not the whole organization (SPEC-WORK-V1 section 1.2).
func (g github) verifyTree(c *tool.Call) *tool.Out {
	path := c.Str("tree")
	start := g.now()
	tree, data, refused := readTree(c, "tree")
	if refused != nil {
		return refused
	}
	if tree.Source != "github" {
		return withTree(tool.Refuse(fmt.Sprintf("the tree's source is %q; this verb reads github trees", tree.Source)), path, "nova-sprint issues verify -h")
	}
	for _, r := range repos(c) {
		if o, _, _ := strings.Cut(r, "/"); o != tree.Org {
			c.Problem(fmt.Sprintf("--repo %s is not in the tree's organization %s", r, tree.Org))
		}
		// A scoped tree names its repositories: a --repo outside that scope
		// is refused here, before any GitHub call (SPEC-WORK-V1 sections 1.2
		// and 1.6).
		if len(tree.Scope) > 0 && !slices.Contains(tree.Scope, r) {
			c.Problem(fmt.Sprintf("--repo %s is not in the tree's scope (%s); the tree was imported with --repo", r, strings.Join(tree.Scope, ", ")))
		}
	}
	if o := c.Refused(); o != nil {
		o.Remedy = "nova-sprint issues verify -h"
		return o
	}
	if against := c.Str("against"); against != "" {
		return verifyAgainst(c, path, tree, data, against, start, g)
	}
	q, ghPath, refused := g.open(c, "verify")
	if refused != nil {
		return refused
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.Dur("timeout"))
	defer cancel()
	f := &workgh.Fetcher{Q: q, PageSize: c.Int("page-size"), MaxCalls: c.Int("max-calls"), Remaining: -1}
	gone := func(err error) *tool.Out {
		return withTree(tool.Refuse(err.Error()), path, ghRemedy(ghPath)).Fact("calls", f.Calls).Fact("gh", ghPath)
	}

	// The default scope is the tree's own: the repositories a scoped import
	// read (tla/WorkImport.tla's Verify reads the scope it imported). A
	// whole-organization tree has none, so the scope is the organization.
	scopeRepos := repos(c)
	if len(scopeRepos) == 0 {
		scopeRepos = tree.Scope
	}
	var metas []workgh.RepoMeta
	var err error
	if len(scopeRepos) > 0 {
		metas, err = scope(ctx, f, tree.Org, scopeRepos)
	} else {
		metas, err = f.Repos(ctx, tree.Org)
	}
	if err != nil {
		return gone(err)
	}
	fresh := &workfile.Tree{Source: "github", Org: tree.Org}
	for _, m := range metas {
		r, err := f.Issues(ctx, m)
		if err != nil {
			return gone(err)
		}
		fresh.Repos = append(fresh.Repos, r)
	}
	diffs := workfile.Diff(tree, fresh, scopeRepos)
	cnt := fresh.Count()
	o := tool.Done()
	if len(diffs) > 0 {
		o = tool.Fail()
	}
	o.Fact("tree", path).Fact("sha256", sum(data)).Fact("repos", cnt.Repos).Fact("issues", cnt.Issues).
		Fact("comments", cnt.Comments).Fact("calls", f.Calls).Fact("points", f.Points).Fact("rest", 0).
		Fact("seconds", fmt.Sprintf("%.1f", g.now().Sub(start).Seconds()))
	return differences(o, diffs).Fact("gh", ghPath)
}

// verifyAgainst is verify with a second tree where GitHub stands: the same
// comparison, no gh, no network.
func verifyAgainst(c *tool.Call, path string, tree *workfile.Tree, data []byte, against string, start time.Time, g github) *tool.Out {
	other, otherData, refused := readTree(c, "against")
	if refused != nil {
		return refused
	}
	// As with the GitHub read, the default scope is the tree's own
	// (SPEC-WORK-V1 sections 1.2 and 1.6); --repo narrows within it and was
	// checked against it before this call.
	scope := repos(c)
	if len(scope) == 0 {
		scope = tree.Scope
	}
	fresh := &workfile.Tree{Source: other.Source, Org: other.Org}
	for _, r := range other.Repos {
		if len(scope) == 0 || slices.Contains(scope, r.Name) {
			fresh.Repos = append(fresh.Repos, r)
		}
	}
	diffs := workfile.Diff(tree, fresh, scope)
	cnt, treeCnt := fresh.Count(), tree.Count()
	o := tool.Done()
	if len(diffs) > 0 {
		o = tool.Fail()
	}
	o.Fact("tree", path).Fact("sha256", sum(data)).Fact("against", against).Fact("against_sha256", sum(otherData)).
		Fact("repos", cnt.Repos).Fact("issues", cnt.Issues).Fact("tree_issues", treeCnt.Issues).
		Fact("against_issues", cnt.Issues).Fact("comments", cnt.Comments).
		Fact("seconds", fmt.Sprintf("%.1f", g.now().Sub(start).Seconds()))
	return differences(o, diffs)
}

// differences adds the count of each kind and one item per difference: the
// value of a field is a Text, quoted with its spaces kept.
func differences(o *tool.Out, diffs []workfile.Difference) *tool.Out {
	kinds := map[string]int{}
	for _, d := range diffs {
		kinds[d.Kind]++
		switch d.Kind {
		case "MISSING":
			o.Item("missing", "path", d.Path, "field", d.Field, "want", tool.Text(d.Want))
		case "EXTRA":
			o.Item("extra", "path", d.Path, "field", d.Field, "got", tool.Text(d.Got))
		default:
			o.Item("drift", "path", d.Path, "field", d.Field, "want", tool.Text(d.Want), "got", tool.Text(d.Got))
		}
	}
	return o.Fact("differences", len(diffs)).Fact("missing", kinds["MISSING"]).Fact("extra", kinds["EXTRA"]).Fact("drift", kinds["DRIFT"])
}

// readTree reads and decodes the tree file the flag names (--tree or
// --against), of at most --max-bytes, or returns the refusal naming why it
// cannot be read and what to run next; a path in a remedy is one shell word.
func readTree(c *tool.Call, flag string) (*workfile.Tree, []byte, *tool.Out) {
	path, maxBytes := c.Str(flag), c.Int("max-bytes")
	refuse := func(why, remedy string) *tool.Out {
		o := tool.Refuse(why)
		o.Remedy = remedy
		return o.Fact(flag, path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil, refuse(err.Error()+"; import writes a tree file with --out", "nova-sprint issues import -h")
	}
	if fi.Size() > int64(maxBytes) {
		return nil, nil, refuse(fmt.Sprintf("the file is %d bytes, past --max-bytes=%d", fi.Size(), maxBytes),
			again(c, "verify", []string{"tree", "against", "repo", "max", "max-calls", "page-size", "gh", "timeout", "max-bytes"},
				map[string]string{"max-bytes": fmt.Sprint(fi.Size())}))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, refuse(err.Error(), "ls -l -- "+oneline.ShellWord(path))
	}
	tree, err := workfile.Decode(path, data, workfile.Limits(maxBytes))
	if err != nil {
		// Every problem of the file, one line each (SPEC-WORK-V1 section 1.2;
		// docs/STANDARD.md section 2, every problem at once).
		return nil, nil, refuseTree(err, flag, path)
	}
	return tree, data, nil
}

// refuseTree is every problem of a tree file in one refusal.
func refuseTree(err error, flag, path string) *tool.Out {
	o := tool.Refuse(treeProblems(err)...)
	o.Remedy = "nova-sprint issues verify -h"
	return o.Fact(flag, path)
}

// treeProblems splits a joined reader error into one reason per problem.
// A single error stays one reason, in the reader's own words.
func treeProblems(err error) []string {
	type joined interface{ Unwrap() []error }
	if u, ok := err.(joined); ok {
		var out []string
		for _, e := range u.Unwrap() {
			if e != nil {
				out = append(out, treeProblems(e)...)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{err.Error()}
}

// withTree names the tree a refusal is about and what to run next.
func withTree(o *tool.Out, path, remedy string) *tool.Out {
	o.Remedy = remedy
	return o.Fact("tree", path)
}

// roadmapVerbs edit and read a repository's own work record, docs/roadmap.sexp
// and docs/fixes.sexp (internal/roadmap; tla/RoadmapEntry.tla), and render
// ROADMAP.md and FIXES.md in the same step, so no one edits the data or the
// pages by hand. They are the verbs of nova-sprint 8c87a4b (check, add, remove,
// pull, note), which edited the private work record's roadmaps/*.sexp and were
// left out by the v1.2.3 re-seed, pointed at the repository's own files, with
// done, list and render added. Every verb reads and writes local files only:
// no GitHub call per entry, and a batch of edits lands as one commit.
func (g github) roadmapVerbs() []tool.Verb {
	return []tool.Verb{
		{
			Name:      "roadmap check",
			Usage:     "roadmap check [--repo <dir>]",
			Example:   "roadmap check",
			Effect:    tool.Inspection,
			Detail:    roadmapDetail,
			ExitTable: "0 both files decode, no id stands in both, and each page is what its data renders; 1 check found problems, one FAILED line each; 2 could not run (a flag, an unreadable file)",
			Flags:     repoFlag,
			Run:       roadmapCheck,
		},
		{
			Name:      "roadmap list",
			Usage:     "roadmap list [--repo <dir>] [--file roadmap|fixes] [--release <v> | --group <g>] [--state <s>] [--text <t>] [--max <n>]",
			Example:   "roadmap list --release v1.2.6 --state planned",
			Effect:    tool.Inspection,
			Detail:    roadmapDetail,
			ExitTable: "0 listed (none is a list too); 2 could not run (a flag, a file that does not decode)",
			Flags: func(f *tool.Flags) {
				repoFlag(f)
				f.String("file", "", "list only one file: roadmap (docs/roadmap.sexp) or fixes (docs/fixes.sexp)")
				f.String("release", "", "list only the fixes of this release, and the scheduled notes for it")
				f.String("group", "", "list only the items of this roadmap group")
				f.String("state", "", "list only entries in this state: todo, scheduled, done, planned, in-progress or shipped")
				f.String("text", "", "list only entries whose id, title or text holds these words (any case)")
				f.Max()
				f.Check(func(c *tool.Call) {
					if v := c.Str("file"); v != "" && v != "roadmap" && v != "fixes" {
						c.Problem(fmt.Sprintf("--file %q wants roadmap or fixes", v))
					}
					if c.Str("release") != "" && c.Str("group") != "" {
						c.Problem("--release and --group name two places; give one")
					}
				})
			},
			Run: roadmapList,
		},
		{
			Name:      "roadmap add",
			Usage:     "roadmap add [--repo <dir>] --id <id> (--release <v> | --group <g>) --title <t> [--text <t>] --origin <o> [--status planned|in-progress] [--why <w>]",
			Example:   `roadmap add --id my-fix --release v1.2.6 --title "What the fix does" --origin "PR #12"`,
			Effect:    tool.LocalWrite + ": rewrites docs/roadmap.sexp or docs/fixes.sexp and its page",
			Detail:    roadmapDetail,
			ExitTable: "0 the entry is added and the data and its page saved; 2 could not run, nothing written (a flag, a duplicate id, a group or release not there, a shipped release, data that would not decode)",
			Flags: func(f *tool.Flags) {
				repoFlag(f)
				f.Required("id", "the new entry's id: lower case, digits, dots and dashes, unique across both files")
				placeFlags(f, "add the entry to")
				f.Required("title", "the entry's title, one line")
				f.String("text", "", "what the entry is (required for a roadmap item)")
				f.String("origin", "", "where the entry came from: an issue, a pull request, a card (required for a fix)")
				f.String("status", "planned", "a new fix's status: planned or in-progress")
				f.String("why", "", "a roadmap item's reason to wait")
				f.Check(func(c *tool.Call) {
					if c.Str("group") != "" && strings.TrimSpace(c.Str("text")) == "" {
						c.Problem("--text is required for a roadmap item; it wants what the item is")
					}
					if c.Str("release") != "" && strings.TrimSpace(c.Str("origin")) == "" {
						c.Problem("--origin is required for a fix; it wants where the fix came from (a pull request, an issue, a card)")
					}
				})
			},
			Run: g.roadmapAdd,
		},
		{
			Name:      "roadmap remove",
			Usage:     "roadmap remove [--repo <dir>] --id <id>...",
			Example:   "roadmap remove --id my-old-fix",
			Effect:    tool.LocalWrite + ": rewrites docs/roadmap.sexp or docs/fixes.sexp and its page",
			Detail:    roadmapDetail,
			ExitTable: "0 the entries are removed and the data and pages saved; 2 could not run, nothing written (a flag, an unknown id, a done or shipped entry, the last entry of its group or release, a shipped release)",
			Flags: func(f *tool.Flags) {
				repoFlag(f)
				idFlag(f, "remove")
			},
			Run: g.roadmapRemove,
		},
		{
			Name:      "roadmap pull",
			Usage:     "roadmap pull [--repo <dir>] --id <id>... (--release <v> | --group <g>)",
			Example:   "roadmap pull --id my-item --release v1.2.6",
			Effect:    tool.LocalWrite + ": rewrites docs/roadmap.sexp and docs/fixes.sexp and their pages",
			Detail:    roadmapDetail,
			ExitTable: "0 the entries are moved and the data and pages saved; 2 could not run, nothing written (a flag, an unknown id, a done or shipped entry, the last entry of its group or release, a group or release not there, a shipped release)",
			Flags: func(f *tool.Flags) {
				repoFlag(f)
				idFlag(f, "move")
				placeFlags(f, "move the entries to")
			},
			Run: g.roadmapPull,
		},
		{
			Name:      "roadmap done",
			Usage:     "roadmap done [--repo <dir>] --id <id>... --evidence <PR #n | commit>",
			Example:   `roadmap done --id my-fix --evidence "PR #12"`,
			Effect:    tool.LocalWrite + ": rewrites docs/roadmap.sexp or docs/fixes.sexp and its page",
			Detail:    roadmapDetail,
			ExitTable: "0 the entries are done and the data and pages saved; 2 could not run, nothing written (a flag, an unknown id, evidence that is no pull request or commit, an entry already done or shipped, the last item of its group, a shipped release)",
			Flags: func(f *tool.Flags) {
				repoFlag(f)
				idFlag(f, "mark done")
				f.Required("evidence", "what shows it is done: a pull request (PR #12 or #12), a commit (7 to 40 hex digits), or a github.com pull or commit link")
			},
			Run: g.roadmapDone,
		},
		{
			Name:      "roadmap note",
			Usage:     "roadmap note [--repo <dir>] --id <id>... [--text <t>] [--title <t>]",
			Example:   `roadmap note --id my-fix --text "Needs the store migration first."`,
			Effect:    tool.LocalWrite + ": rewrites docs/roadmap.sexp or docs/fixes.sexp and its page",
			Detail:    roadmapDetail,
			ExitTable: "0 the text is appended (and the title given) and the data and pages saved; 2 could not run, nothing written (a flag, an unknown id)",
			Flags: func(f *tool.Flags) {
				repoFlag(f)
				idFlag(f, "note")
				f.String("text", "", "the words to append to each entry's :text")
				f.String("title", "", "a new title for each entry; the title it had is kept under :kept (earlier-title)")
				f.Check(func(c *tool.Call) {
					if strings.TrimSpace(c.Str("text")) == "" && strings.TrimSpace(c.Str("title")) == "" {
						c.Problem("--text or --title is required; note wants words to append, a new title, or both")
					}
				})
			},
			Run: g.roadmapNote,
		},
		{
			Name:      "roadmap render",
			Usage:     "roadmap render [--repo <dir>]\nroadmap render --file <data.sexp> --out <page.md> [--source <name>]",
			Example:   "roadmap render",
			Effect:    tool.LocalWrite + ": writes ROADMAP.md and FIXES.md, or --out",
			Detail:    roadmapDetail,
			ExitTable: "0 the pages are written (or were already what the data renders); 2 could not run, nothing written (a flag, data that does not decode, an unwritable page)",
			Flags: func(f *tool.Flags) {
				repoFlag(f)
				f.String("file", "", "render this data `file` instead of the repository's own: a (roadmap \"v1\" ...) or (fixes \"v1\" ...) form held anywhere")
				f.String("out", "", "the page `file` --file renders to (required with --file)")
				f.String("source", "", "the data's name in the page's header (default: --file as given)")
				f.Check(func(c *tool.Call) {
					if (c.Str("file") == "") != (c.Str("out") == "") {
						c.Problem("--file and --out go together: the data and the page it renders to")
					}
					if c.Str("file") != "" && c.Given("repo") {
						c.Problem("--repo renders the repository's own files and --file renders one held elsewhere; give one")
					}
				})
			},
			Run: roadmapRender,
		},
	}
}

const roadmapDetail = `A repository's work record is two files of data and the two pages they render:
docs/roadmap.sexp -> ROADMAP.md (groups of items: work after the release
ladder) and docs/fixes.sexp -> FIXES.md (point releases and their fixes). The
shapes are internal/roadmapdoc's. Every writing verb edits the data, decodes it
again, and writes the data and its page together; nothing is written when any
check fails, and every byte it does not change (comments, spacing, the other
records) is kept. Nothing reaches GitHub: commit the batch of edits as one.

An entry is todo (a roadmap item), open (a fix, planned or in-progress), or
done (an item on the roadmap's :done list, a fix whose status is done or
shipped). Done is final. A shipped release is frozen. Every group and release
keeps one entry, and every entry names a group or release that is there
(tla/RoadmapEntry.tla).

check:  the files decode, no id stands in both, each page is what its data renders.
list:   entries by file, release or group, state and words; --json for a program.
add:    an item under --group, or a fix in --release (planned unless --status).
remove: the entries go.
pull:   to --release, an item becomes a fix (planned) or a fix changes release;
        to --group, a fix becomes an item or an item changes group.
done:   a fix's status becomes done, an item moves to :done; --evidence is
        appended to the text.
note:   --text is appended to each entry's text; --title retitles it, the old
        title kept under :kept.
A pull across the files, and done of an item, carry every field: one the
target has no key for is kept under :kept (item-why on a fix, fix-status on
an item), and a move back restores it.
render: writes the pages from the data; --file/--out renders data kept
        elsewhere (a roadmap held in the private work repository) to a page.

The old nova-sprint issues roadmap verbs edited the private work record's
roadmaps/*.sexp ((:roadmap ... :releases ...) with streams of cards). The
v1.2.3 re-seed left them out; these verbs replace them, on the repository's own
docs/roadmap.sexp and docs/fixes.sexp.`

// repoFlag declares --repo: the repository root whose work record the verb reads.
func repoFlag(f *tool.Flags) {
	f.String("repo", ".", "the repository root holding docs/roadmap.sexp and docs/fixes.sexp (default .)")
}

// placeFlags declares --release and --group, one of which is required.
func placeFlags(f *tool.Flags, what string) {
	f.String("release", "", what+" this fixes release (docs/fixes.sexp)")
	f.String("group", "", what+" this roadmap group (docs/roadmap.sexp)")
	f.Check(func(c *tool.Call) {
		switch r, g := c.Str("release"), c.Str("group"); {
		case r == "" && g == "":
			c.Problem("--release or --group is required; it wants where the entry goes: a fixes release or a roadmap group")
		case r != "" && g != "":
			c.Problem("--release and --group name two places; give one")
		}
	})
}

// idFlag declares --id, repeated: the entries a verb acts on.
func idFlag(f *tool.Flags, verb string) {
	f.Var(&idList{}, "id", "an entry `id` to "+verb+"; repeat for more (required)")
	f.Check(func(c *tool.Call) {
		if len(ids(c)) == 0 {
			c.Problem("--id is required; it wants an entry id, repeated for more; refusing to guess")
		}
	})
}

// idList is --id, repeated.
type idList []string

func (l *idList) String() string { return strings.Join(*l, ",") }
func (l *idList) Get() any       { return []string(*l) }
func (l *idList) Set(v string) error {
	if strings.TrimSpace(v) == "" || strings.ContainsAny(v, " \t\n\"") {
		return fmt.Errorf("%q is not an entry id", v)
	}
	*l = append(*l, v)
	return nil
}

func ids(c *tool.Call) []string { return c.Get("id").([]string) }

// refuse is a refusal whose remedy is the verb's own help.
func refuse(verb, why string) *tool.Out {
	o := tool.Refuse(oneLine(why))
	o.Remedy = "nova-sprint issues roadmap " + verb + " -h"
	return o
}

func oneLine(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "\n", "; ") }

func (g github) today() string { return g.now().Format("2006-01-02") }

// edit loads the work record, applies one edit, and saves the data and pages;
// a refused edit writes nothing.
func (g github) edit(c *tool.Call, verb string, apply func(s *roadmap.Set) error) *tool.Out {
	s, err := roadmap.Load(c.Str("repo"), g.today())
	if err != nil {
		o := refuse(verb, err.Error()+"; nothing written")
		o.Remedy = "nova-sprint issues roadmap check --repo " + c.Str("repo")
		return o
	}
	if err := apply(s); err != nil {
		return refuse(verb, err.Error()+"; nothing written")
	}
	if err := s.Save(); err != nil {
		return refuse(verb, "save: "+err.Error())
	}
	o := tool.Done().Fact("repo", c.Str("repo"))
	for _, f := range s.Changed() {
		o.Item("wrote", "file", f)
	}
	return o
}

func roadmapCheck(c *tool.Call) *tool.Out {
	probs, err := roadmap.Check(c.Str("repo"))
	if err != nil {
		return refuse("check", err.Error())
	}
	if len(probs) > 0 {
		return tool.Fail(probs...).Fact("repo", c.Str("repo")).Fact("problems", len(probs))
	}
	s, err := roadmap.Load(c.Str("repo"), "")
	if err != nil {
		return refuse("check", err.Error())
	}
	return tool.Done().Fact("repo", c.Str("repo")).Fact("entries", len(s.Entries()))
}

func roadmapList(c *tool.Call) *tool.Out {
	s, err := roadmap.Load(c.Str("repo"), "")
	if err != nil {
		o := refuse("list", err.Error())
		o.Remedy = "nova-sprint issues roadmap check --repo " + c.Str("repo")
		return o
	}
	found := s.Find(roadmap.Query{File: c.Str("file"), Place: c.Str("release") + c.Str("group"), State: c.Str("state"), Text: c.Str("text")})
	o := tool.Done().Fact("repo", c.Str("repo")).Fact("entries", len(found))
	for _, e := range found {
		o.Item("entry", "id", e.ID, "file", e.File, "kind", e.Kind, "place", e.Place, "state", e.State, "title", e.Title)
	}
	return o
}

func (g github) roadmapAdd(c *tool.Call) *tool.Out {
	o := g.edit(c, "add", func(s *roadmap.Set) error {
		if c.Str("group") != "" {
			return s.AddItem(roadmap.Item{ID: c.Str("id"), Group: c.Str("group"), Title: c.Str("title"), Text: c.Str("text"), Origin: c.Str("origin"), Why: c.Str("why")})
		}
		return s.AddFix(roadmap.Fix{ID: c.Str("id"), Release: c.Str("release"), Title: c.Str("title"), Text: c.Str("text"), Origin: c.Str("origin"), Status: c.Str("status")})
	})
	if o.Status == tool.OK {
		o.Fact("id", c.Str("id")).Fact("place", c.Str("release")+c.Str("group"))
	}
	return o
}

func (g github) roadmapRemove(c *tool.Call) *tool.Out {
	o := g.edit(c, "remove", func(s *roadmap.Set) error { return s.Remove(ids(c)...) })
	if o.Status == tool.OK {
		o.Fact("removed", len(ids(c)))
	}
	return o
}

func (g github) roadmapPull(c *tool.Call) *tool.Out {
	o := g.edit(c, "pull", func(s *roadmap.Set) error { return s.Pull(c.Str("release"), c.Str("group"), ids(c)...) })
	if o.Status == tool.OK {
		o.Fact("moved", len(ids(c))).Fact("place", c.Str("release")+c.Str("group"))
	}
	return o
}

func (g github) roadmapDone(c *tool.Call) *tool.Out {
	o := g.edit(c, "done", func(s *roadmap.Set) error { return s.MarkDone(c.Str("evidence"), ids(c)...) })
	if o.Status == tool.OK {
		o.Fact("done", len(ids(c))).Fact("evidence", c.Str("evidence"))
	}
	return o
}

func (g github) roadmapNote(c *tool.Call) *tool.Out {
	o := g.edit(c, "note", func(s *roadmap.Set) error { return s.Note(c.Str("text"), c.Str("title"), ids(c)...) })
	if o.Status == tool.OK {
		o.Fact("noted", len(ids(c)))
	}
	return o
}

func roadmapRender(c *tool.Call) *tool.Out {
	if in := c.Str("file"); in != "" {
		source := c.Str("source")
		if source == "" {
			source = filepath.ToSlash(in)
		}
		kind, items, err := roadmap.RenderFile(in, c.Str("out"), source)
		if err != nil {
			return refuse("render", err.Error())
		}
		return tool.Done().Fact("kind", kind).Fact("items", items).Fact("out", c.Str("out"))
	}
	changed, err := roadmap.Render(c.Str("repo"))
	if err != nil {
		return refuse("render", err.Error())
	}
	o := tool.Done().Fact("repo", c.Str("repo")).Fact("changed", len(changed))
	for _, p := range changed {
		o.Item("wrote", "file", p)
	}
	return o
}
