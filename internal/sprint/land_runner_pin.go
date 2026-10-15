package sprint

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// A tree pins its TLC runner in its CI workflows (land_runner.go). The workflows are read
// as YAML, so a YAML comment pins nothing, and only two shapes of step are a pin:
//
//   - a step that uses actions/checkout with `repository: mas-bandwidth/nova-tools` and a
//     `ref:`, the ref the revision;
//   - a step whose run script, read as shell words with its comments dropped, clones
//     nova-tools (`git clone ... <nova-tools URL> <dir>`) and checks that clone out
//     (`git -C <dir> checkout ...`, or `cd <dir>` then `git checkout ...`), or go-installs or
//     go-runs github.com/mas-bandwidth/nova-tools/tools/tlacheck@<rev>.
//
// A checkout of any other repository is not a pin. A pin's revision must be exactly 40
// lowercase hex digits (a branch, a tag, an abbreviation or `<sha>^` is refused, since
// the runner CI builds would not be that commit), and every workflow must read and parse:
// a file the lander cannot read is a refusal naming it, never a silent fall back to the
// built-in runner (Stella's HOLD on #344, 2026-10-15).

// fullRevRE is a full commit: exactly 40 lowercase hex digits.
var fullRevRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

// novaToolsURLs are the spellings of nova-tools a clone in a run script is recognised by.
var novaToolsURLs = []string{
	"https://github.com/mas-bandwidth/nova-tools", "https://github.com/mas-bandwidth/nova-tools.git",
	"git@github.com:mas-bandwidth/nova-tools.git", "ssh://git@github.com/mas-bandwidth/nova-tools.git",
}

// tlacheckModule is the import path a go install or go run pin names, before its "@".
const tlacheckModule = "github.com/mas-bandwidth/nova-tools/tools/tlacheck"

// treeRunnerPin is the tree's pin of its tlacheck runner. ok is false when no workflow
// pins one. fix is one refusal: a workflow that cannot be read or parsed, a pin that is
// not a full commit, or two pins of two revisions (the tree's records have one runner).
func treeRunnerPin(dir string) (pin runnerPin, ok bool, fix *DocFix) {
	wfDir := filepath.Join(dir, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if errors.Is(err, fs.ErrNotExist) {
		return runnerPin{}, false, nil
	}
	if err != nil {
		return runnerPin{}, false, &DocFix{File: ".github/workflows", Line: 1, What: "cannot be read: " + oneLine(err.Error())}
	}
	var names []string
	for _, e := range entries {
		if ext := filepath.Ext(e.Name()); ext == ".yml" || ext == ".yaml" {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	var pins []runnerPin
	for _, name := range names {
		rel := ".github/workflows/" + name
		raw, err := os.ReadFile(filepath.Join(wfDir, name))
		if err != nil {
			return runnerPin{}, false, &DocFix{File: rel, Line: 1, What: "cannot be read to find the TLC runner it pins: " + oneLine(err.Error())}
		}
		found, fix := workflowPins(rel, raw)
		if fix != nil {
			return runnerPin{}, false, fix
		}
		pins = append(pins, found...)
	}
	if len(pins) == 0 {
		return runnerPin{}, false, nil
	}
	for _, p := range pins[1:] {
		if p.Rev != pins[0].Rev {
			return runnerPin{}, false, &DocFix{File: p.File, Line: p.Line, What: fmt.Sprintf("pins the TLC runner at nova-tools %s, and %s:%d pins it at %s; the tree's records have one runner",
				p.Rev, pins[0].File, pins[0].Line, pins[0].Rev)}
		}
	}
	return pins[0], true, nil
}

// workflowPins is the pins of one workflow file, rel its path under the tree.
func workflowPins(rel string, raw []byte) ([]runnerPin, *DocFix) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, &DocFix{File: rel, Line: yamlErrLine(err), What: "is not YAML the lander can read to find the TLC runner it pins: " + oneLine(err.Error())}
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	jobs := mapValue(doc.Content[0], "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil, nil
	}
	var pins []runnerPin
	for i := 1; i < len(jobs.Content); i += 2 {
		steps := mapValue(jobs.Content[i], "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			found, fix := stepPins(rel, step)
			if fix != nil {
				return nil, fix
			}
			pins = append(pins, found...)
		}
	}
	return pins, nil
}

// stepPins is the pins of one step: an actions/checkout of nova-tools, or a run script.
func stepPins(rel string, step *yaml.Node) ([]runnerPin, *DocFix) {
	if step.Kind != yaml.MappingNode {
		return nil, nil
	}
	if uses := mapValue(step, "uses"); uses != nil && uses.Kind == yaml.ScalarNode && strings.HasPrefix(uses.Value, "actions/checkout@") {
		with := mapValue(step, "with")
		repo := mapValue(with, "repository")
		if repo == nil || !strings.EqualFold(repo.Value, "mas-bandwidth/nova-tools") {
			return nil, nil
		}
		ref := mapValue(with, "ref")
		if ref == nil {
			return nil, &DocFix{File: rel, Line: repo.Line, What: "checks out nova-tools at no ref; pin the TLC runner to a full commit (40 hex digits)"}
		}
		return revPin(rel, ref.Line, ref.Value, "checks out nova-tools at")
	}
	run := mapValue(step, "run")
	if run == nil || run.Kind != yaml.ScalarNode {
		return nil, nil
	}
	first := run.Line
	if run.Style == yaml.LiteralStyle || run.Style == yaml.FoldedStyle {
		first++ // a block scalar's text starts on the line after its indicator
	}
	return scriptPins(rel, first, run.Value)
}

// revPin is the pin at rev, or a refusal when rev is not a full commit.
func revPin(rel string, line int, rev, what string) ([]runnerPin, *DocFix) {
	if !fullRevRE.MatchString(rev) {
		return nil, &DocFix{File: rel, Line: line, What: fmt.Sprintf("%s %q, which is not a full commit; pin the TLC runner to one (40 lowercase hex digits)", what, rev)}
	}
	return []runnerPin{{File: rel, Line: line, Rev: rev}}, nil
}

// scriptPins is the pins of a run script whose first line is line first of the file: its
// commands, read as shell words with comments dropped, that check out a nova-tools clone
// the script made, or go-install or go-run tlacheck at a revision.
func scriptPins(rel string, first int, script string) ([]runnerPin, *DocFix) {
	clones := map[string]bool{} // directories the script cloned nova-tools into
	var pins []runnerPin
	for _, c := range shellCommands(script) {
		line := first + c.line
		w := c.words
		// a run of commands joined by && or ; shares a working directory: cd <dir> leads
		if len(w) == 2 && w[0] == "cd" {
			continue
		}
		if len(w) >= 2 && w[0] == "go" && (w[1] == "install" || w[1] == "run") {
			for _, a := range w[2:] {
				if mod, rev, ok := strings.Cut(a, "@"); ok && mod == tlacheckModule {
					found, fix := revPin(rel, line, rev, "installs tlacheck at")
					if fix != nil {
						return nil, fix
					}
					pins = append(pins, found...)
				}
			}
			continue
		}
		if len(w) == 0 || w[0] != "git" {
			continue
		}
		gitDir, rest := c.cwd, w[1:]
		for len(rest) >= 2 && (rest[0] == "-C" || rest[0] == "-c") {
			if rest[0] == "-C" {
				gitDir = rest[1]
			}
			rest = rest[2:]
		}
		if len(rest) == 0 {
			continue
		}
		switch rest[0] {
		case "clone":
			var pos []string
			for i := 1; i < len(rest); i++ {
				a := rest[i]
				if strings.HasPrefix(a, "-") {
					if takesValue(a) && i+1 < len(rest) {
						i++
					}
					continue
				}
				pos = append(pos, a)
			}
			if len(pos) >= 1 && slices.Contains(novaToolsURLs, pos[0]) {
				into := "nova-tools"
				if len(pos) >= 2 {
					into = pos[1]
				}
				clones[into] = true
			}
		case "checkout", "switch":
			if !clones[gitDir] {
				continue // a checkout of some other repository
			}
			var revs []string
			for _, a := range rest[1:] {
				if !strings.HasPrefix(a, "-") {
					revs = append(revs, a)
				}
			}
			if len(revs) != 1 {
				return nil, &DocFix{File: rel, Line: line, What: fmt.Sprintf("checks out nova-tools with %q, which names no one commit; pin the TLC runner to a full commit (40 lowercase hex digits)", strings.Join(rest, " "))}
			}
			found, fix := revPin(rel, line, revs[0], "checks out nova-tools at")
			if fix != nil {
				return nil, fix
			}
			pins = append(pins, found...)
		}
	}
	return pins, nil
}

// takesValue says a git clone option takes the next word as its value.
func takesValue(opt string) bool {
	switch opt {
	case "--filter", "--depth", "-b", "--branch", "-o", "--origin", "--reference", "--separate-git-dir", "-c", "--config", "-u", "--upload-pack", "--template", "--shallow-since", "--shallow-exclude", "-j", "--jobs":
		return true
	}
	return false
}

// shellCommand is one simple command of a script: its words, quotes removed and
// variables left as written, the line (0-based) it starts on, and the directory a cd
// earlier in its && or ; chain moved to ("" when none).
type shellCommand struct {
	words []string
	line  int
	cwd   string
}

// shellCommands splits a script into simple commands: words split on blanks, quotes
// removed, a backslash-newline joining lines, a # at the start of a word ending the line,
// and the operators ; && || | & ( ) and newlines ending a command. A cd in a chain sets the
// cwd of the commands after it on that chain; a newline or a closing ) ends the chain.
func shellCommands(script string) []shellCommand {
	var out []shellCommand
	var words []string
	var word strings.Builder
	inWord := false
	line, start := 0, 0
	cwd := ""
	flushWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endCmd := func(chainEnds bool) {
		flushWord()
		if len(words) > 0 {
			out = append(out, shellCommand{words: words, line: start, cwd: cwd})
			if len(words) == 2 && words[0] == "cd" {
				cwd = words[1]
			}
		}
		words = nil
		if chainEnds {
			cwd = ""
		}
	}
	r := []rune(script)
	for i := 0; i < len(r); i++ {
		ch := r[i]
		if len(words) == 0 && !inWord {
			start = line
		}
		switch {
		case ch == '\\' && i+1 < len(r) && r[i+1] == '\n':
			i++
			line++
		case ch == '\\' && i+1 < len(r):
			word.WriteRune(r[i+1])
			inWord = true
			i++
		case ch == '\'':
			inWord = true
			for i++; i < len(r) && r[i] != '\''; i++ {
				if r[i] == '\n' {
					line++
				}
				word.WriteRune(r[i])
			}
		case ch == '"':
			inWord = true
			for i++; i < len(r) && r[i] != '"'; i++ {
				if r[i] == '\\' && i+1 < len(r) && strings.ContainsRune("\"\\$`", r[i+1]) {
					i++
				}
				if r[i] == '\n' {
					line++
				}
				word.WriteRune(r[i])
			}
		case ch == '#' && !inWord:
			for i+1 < len(r) && r[i+1] != '\n' {
				i++
			}
		case ch == '\n':
			endCmd(true)
			line++
		case ch == ' ' || ch == '\t':
			flushWord()
		case ch == ';' || ch == '&' || ch == '|' || ch == '(':
			endCmd(ch == '|' || (ch == '&' && !(i+1 < len(r) && r[i+1] == '&')) || (ch == '|' && i+1 < len(r) && r[i+1] == '|'))
			if i+1 < len(r) && (r[i+1] == '&' || r[i+1] == '|') && r[i+1] == ch {
				i++
			}
		case ch == ')':
			endCmd(true)
		default:
			word.WriteRune(ch)
			inWord = true
		}
	}
	endCmd(true)
	return out
}

// mapValue is the value of key in the mapping node m, nil when m is no mapping or lacks it.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// yamlLineRE is the line a yaml.v3 error names.
var yamlLineRE = regexp.MustCompile(`line (\d+)`)

// yamlErrLine is the line a YAML error names, 1 when it names none.
func yamlErrLine(err error) int {
	if m := yamlLineRE.FindStringSubmatch(err.Error()); m != nil {
		var n int
		fmt.Sscan(m[1], &n)
		if n > 0 {
			return n
		}
	}
	return 1
}
