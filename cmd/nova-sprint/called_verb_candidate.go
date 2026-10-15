package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The lander's E6 check holds a changed line that runs `nova-<tool> <verb>` to the verb
// table of the candidate tree it is landing, not to the running lander's own: a head that
// adds a verb and documents it lands under an older lander, a head that removes a verb a
// doc still calls is refused, and a table that cannot be read is refused, never passed
// (docs/SPEC-SPRINT.md section 7, the lander's checks;
// lander-checks-docs-against-the-candidate-verbs).

// calledVerbFindings is the E6 check over the diff of one card's merge in dir: each
// changed line that runs a repository tool with a verb the candidate tree does not
// register is one finding (calledVerbs), ending (E6). The table is read from the
// candidate (l.verbTable, else candidateVerbs with the lander's runner) lazily, at most
// once, and only when the diff holds a line that runs a tool; a table that cannot be
// read is one finding saying so and never a pass.
func (l *lander) calledVerbFindings(ctx context.Context, dir, diff string) []string {
	read := l.verbTable
	if read == nil {
		read = func(ctx context.Context, dir string) (func(string) []string, error) {
			return candidateVerbs(ctx, dir, func(ctx context.Context, dir string, argv ...string) (string, error) {
				return l.goRun(ctx, dir, argv)
			})
		}
	}
	var table func(string) []string
	var readErr error
	read1 := false
	verbs := func(tool string) []string {
		if !read1 {
			read1 = true
			table, readErr = read(ctx, dir)
		}
		if readErr != nil || table == nil {
			return nil
		}
		return table(tool)
	}
	var out []string
	for _, f := range calledVerbs(diff, verbs) {
		out = append(out, f.String()+" (E6)")
	}
	if readErr != nil {
		return []string{"the candidate's verb table could not be read: " + readErr.Error() + " (E6)"}
	}
	return out
}

// candidateRunner runs argv in dir and returns its combined output.
type candidateRunner = func(ctx context.Context, dir string, argv ...string) (string, error)

// candidateVerbs reads the verb table of the candidate tree in dir. When dir holds a main
// package at cmd/nova-sprint it builds that command into a file under the lander's scratch
// (not the clone's working tree; removed after), runs the binary's `help`, parses the
// sprint verbs from it (sprintVerbsFromHelp) and reads the foreign tools' verbs from dir's
// cmd/nova-sprint/foreign_verbs.go (foreignVerbsFromSource). A tree without
// cmd/nova-sprint (nova-tools and every other repository) keeps the installed tables
// (registeredVerbs). A build, run or parse error names the step and its first line.
func candidateVerbs(ctx context.Context, dir string, run candidateRunner) (func(tool string) []string, error) {
	if _, err := os.Stat(filepath.Join(dir, "cmd", "nova-sprint", "main.go")); err != nil {
		return registeredVerbs, nil
	}
	scratch, err := os.MkdirTemp("", "lander-verbs-*")
	if err != nil {
		return nil, fmt.Errorf("scratch for the candidate build: %s", firstLine("", err))
	}
	defer os.RemoveAll(scratch)
	bin := filepath.Join(scratch, "nova-sprint")
	if out, err := run(ctx, dir, "go", "build", "-o", bin, "./cmd/nova-sprint"); err != nil {
		return nil, fmt.Errorf("go build ./cmd/nova-sprint: %s", firstLine(out, err))
	}
	help, err := run(ctx, dir, bin, "help")
	if err != nil {
		return nil, fmt.Errorf("the candidate's help: %s", firstLine(help, err))
	}
	sprintVerbs := sprintVerbsFromHelp(help)
	if len(sprintVerbs) <= 2 {
		return nil, fmt.Errorf("the candidate's help names no verb: %s", firstLine(help, nil))
	}
	src, err := os.ReadFile(filepath.Join(dir, "cmd", "nova-sprint", "foreign_verbs.go"))
	if err != nil {
		return nil, fmt.Errorf("reading foreign_verbs.go: %s", firstLine("", err))
	}
	foreign, err := foreignVerbsFromSource(src)
	if err != nil {
		return nil, fmt.Errorf("parsing foreign_verbs.go: %s", firstLine("", err))
	}
	return func(tool string) []string {
		if tool == "sprint" {
			return slices.Clone(sprintVerbs)
		}
		return slices.Clone(foreign["nova-"+tool])
	}, nil
}

var helpVerbRE = regexp.MustCompile(`^  nova-sprint ([a-z][a-z0-9-]*)`)

// sprintVerbsFromHelp reads the verbs from a nova-sprint help text: every line
// `  nova-sprint <word> ...` gives <word>, and help and version are added as verbNames()
// adds them. Each verb appears once.
func sprintVerbsFromHelp(help string) []string {
	var out []string
	for _, line := range strings.Split(help, "\n") {
		if m := helpVerbRE.FindStringSubmatch(line); m != nil && !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	for _, v := range []string{"help", "version"} {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// foreignVerbsFromSource reads the foreignVerbs map literal of foreign_verbs.go: its keys
// ("nova-<tool>") to the verbs whose value is true. It is an error when src does not parse
// or holds no foreignVerbs literal of string keys and bool values.
func foreignVerbsFromSource(src []byte) (map[string][]string, error) {
	file, err := parser.ParseFile(gotoken.NewFileSet(), "foreign_verbs.go", src, 0)
	if err != nil {
		return nil, err
	}
	str := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != gotoken.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		return s, err == nil
	}
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != gotoken.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			if len(vs.Names) != 1 || vs.Names[0].Name != "foreignVerbs" || len(vs.Values) != 1 {
				continue
			}
			outer, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				return nil, fmt.Errorf("foreignVerbs is not a map literal")
			}
			out := map[string][]string{}
			for _, e := range outer.Elts {
				kv, ok := e.(*ast.KeyValueExpr)
				if !ok {
					return nil, fmt.Errorf("foreignVerbs holds an entry that is not key: value")
				}
				tool, ok := str(kv.Key)
				inner, ok2 := kv.Value.(*ast.CompositeLit)
				if !ok || !ok2 {
					return nil, fmt.Errorf("foreignVerbs holds an entry that is not a string key and a map literal")
				}
				verbs := []string{}
				for _, ie := range inner.Elts {
					ikv, ok := ie.(*ast.KeyValueExpr)
					if !ok {
						return nil, fmt.Errorf("foreignVerbs[%q] holds an entry that is not key: value", tool)
					}
					name, ok := str(ikv.Key)
					val, ok2 := ikv.Value.(*ast.Ident)
					if !ok || !ok2 || (val.Name != "true" && val.Name != "false") {
						return nil, fmt.Errorf("foreignVerbs[%q] holds an entry that is not a string key and a bool", tool)
					}
					if val.Name == "true" {
						verbs = append(verbs, name)
					}
				}
				out[tool] = verbs
			}
			return out, nil
		}
	}
	return nil, fmt.Errorf("no foreignVerbs table")
}
