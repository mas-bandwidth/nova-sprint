package verbs

import (
	"context"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"
)

// gatesAtBase refuses a DONE-WHEN that is known to fail at the base before
// any work: `make <target>` when the Makefile make reads has no rule for the
// target, or a repository path run as the program that is not there. What
// the base cannot tell is admitted: a Makefile or program in the card's
// PATHS (the card writes it), a program an earlier DONE-WHEN may build, and
// a Makefile whose targets need what a read cannot resolve (a variable set
// by a command, a missing or computed include). 2026-11-09: rocketnet cards
// dealt with DONE-WHEN make test, a target its Makefile lacks, each cost a
// whole attempt.
func (e *Engine) gatesAtBase(ctx context.Context, sb *streamAtBase, f cardFacts) error {
	for i, d := range f.DoneWhen {
		args, why := edge.ParseGate(d)
		if why != "" {
			continue // cardLint's refusal
		}
		var problem, fix string
		switch {
		case args[0] == "make" || args[0] == "gmake":
			var err error
			if problem, err = e.makeProblem(ctx, sb, f.Paths, args[1:]); err != nil {
				return err
			}
			fix = "use a target the Makefile has, correct -C or -f, or put the Makefile in PATHS if the card adds the rule, then add the card again"
		case strings.Contains(args[0], "/") && i == 0:
			problem = programProblem(sb.tree, f.Paths, args[0])
			fix = "correct the path, add it to PATHS if the card creates it, or put first a DONE-WHEN line that builds it, then add the card again"
		}
		if problem != "" {
			return refuse("card "+f.ID+": DONE-WHEN "+clip(d)+": "+problem+" at "+sb.s.Repo+"'s "+sb.s.Base+" ("+shortSHA(sb.tip)+")", fix)
		}
	}
	return nil
}

// programProblem is why the repository path prog cannot run at the base,
// or "": an absolute or outside path is not the repository's to check.
func programProblem(tree map[string]bool, paths []string, prog string) string {
	p := path.Clean(prog)
	if path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(prog, "~") || tree[p] || inPaths(paths, p) {
		return ""
	}
	return p + " is not there"
}

// inPaths is whether PATHS covers p: an entry, a directory entry above it,
// or a glob matching it.
func inPaths(paths []string, p string) bool {
	for _, q := range paths {
		q = strings.TrimSuffix(strings.TrimSpace(q), "/")
		if ok, _ := path.Match(q, p); ok || q == p || strings.HasPrefix(p, q+"/") {
			return true
		}
	}
	return false
}

// makeProblem is why make with args (after the program) cannot make its
// targets at the base, or "": the Makefile it reads (-C, -f, else
// GNUmakefile, makefile, Makefile) is not there, or has no rule for a target.
func (e *Engine) makeProblem(ctx context.Context, sb *streamAtBase, paths, args []string) (string, error) {
	dir, files, targets, ok := makeArgs(args)
	if dir = path.Clean(dir); dir == "." {
		dir = ""
	}
	if !ok || len(targets) == 0 || path.IsAbs(dir) || dir == ".." || strings.HasPrefix(dir, "../") {
		return "", nil
	}
	var mk []string
	for _, f := range files {
		p := path.Join(dir, f)
		switch {
		case f == "-" || path.IsAbs(f) || inPaths(paths, p):
			return "", nil
		case !sb.tree[p]:
			return "make -f " + f + ": " + p + " is not there", nil
		}
		mk = append(mk, p)
	}
	if len(files) == 0 {
		for _, n := range []string{"GNUmakefile", "makefile", "Makefile"} {
			if p := path.Join(dir, n); sb.tree[p] {
				mk = []string{p}
				break
			}
		}
		if mk == nil {
			if inPaths(paths, path.Join(dir, "Makefile")) {
				return "", nil
			}
			where := "the repository root"
			if dir != "" {
				where = dir + "/"
			}
			return "there is no Makefile in " + where, nil
		}
	}
	if slices.ContainsFunc(mk, func(m string) bool { return inPaths(paths, m) }) {
		return "", nil
	}
	rules, known, err := e.makeRules(ctx, sb, dir, mk)
	if err != nil || !known {
		return "", err
	}
	for _, t := range targets {
		if !makes(rules, sb.tree, dir, t) {
			return mk[0] + " has no rule for " + t + " (its targets: " + targetList(rules) + ")", nil
		}
	}
	return "", nil
}

// makeArgs is make's arguments read: the -C directory, the -f makefiles and
// the targets; ok is false when a flag's argument cannot be told.
func makeArgs(args []string) (dir string, files, targets []string, ok bool) {
	takesArg := map[string]bool{"-C": true, "--directory": true, "-f": true, "--file": true, "--makefile": true,
		"-I": true, "--include-dir": true, "-o": true, "--old-file": true, "--assume-old": true, "-W": true, "--what-if": true, "--new-file": true, "--assume-new": true}
	numeric := regexp.MustCompile(`^-[jl][0-9.]*$`)
	for i := 0; i < len(args); i++ {
		a := args[i]
		long, value, joined := strings.Cut(a, "=")
		switch {
		case a == "--":
			return dir, files, append(targets, args[i+1:]...), true
		case takesArg[a]:
			if i++; i == len(args) {
				return "", nil, nil, false
			}
			long, value, joined = a, args[i], true
		case numeric.MatchString(a) || a == "--jobs" || a == "--load-average":
			if (a == "-j" || a == "-l" || strings.HasPrefix(a, "--")) && i+1 < len(args) && args[i+1] != "" && numeric.MatchString("-j"+args[i+1]) {
				i++ // its count
			}
			continue
		case strings.HasPrefix(a, "--"):
		case len(a) > 2 && a[0] == '-' && strings.ContainsAny(a[1:2], "CfIoW"):
			long, value, joined = a[:2], a[2:], true
		case strings.HasPrefix(a, "-"):
			if strings.ContainsAny(a[1:], "CfIoWjl") {
				return "", nil, nil, false // a cluster with a flag that takes an argument
			}
			continue
		case joined:
			continue // a variable
		default:
			targets = append(targets, a)
			continue
		}
		if !joined {
			continue
		}
		switch long {
		case "-C", "--directory":
			dir = path.Join(dir, value)
		case "-f", "--file", "--makefile":
			files = append(files, value)
		}
	}
	return dir, files, targets, true
}

// makeVar is $(NAME) or ${NAME} in a makefile.
var makeVar = regexp.MustCompile(`\$[({]([A-Za-z0-9_.-]+)[)}]`)

// makeRules is every target the makefiles mk and those they include name,
// a $(VAR) in a target expanded by the assignments read; known is false
// when a target or an include cannot be resolved from the base.
func (e *Engine) makeRules(ctx context.Context, sb *streamAtBase, dir string, mk []string) (map[string]bool, bool, error) {
	raw, vars, seen := map[string]bool{}, map[string]string{}, map[string]bool{}
	for queue := slices.Clone(mk); len(queue) > 0; queue = queue[1:] {
		p := queue[0]
		if seen[p] {
			continue
		}
		if seen[p] = true; len(seen) > 32 {
			return nil, false, nil
		}
		text, err := e.fileAt(ctx, sb, p)
		if err != nil {
			return nil, false, err
		}
		for _, inc := range parseMakefile(text, raw, vars) {
			inc = expandMake(inc, vars)
			q := path.Join(dir, inc)
			if strings.ContainsAny(inc, "$*?[") || path.IsAbs(inc) || !sb.tree[q] {
				return nil, false, nil // computed, absent (generated) or outside: cannot tell
			}
			queue = append(queue, q)
		}
	}
	rules := map[string]bool{}
	for t := range raw {
		x := expandMake(t, vars)
		if strings.Contains(x, "$") {
			return nil, false, nil
		}
		for _, w := range strings.Fields(x) {
			rules[w] = true
		}
	}
	return rules, true, nil
}

// parseMakefile adds the target lists of text's rules to raw (a .PHONY's
// names too) and its assignments to vars, and is the files it includes.
func parseMakefile(text string, raw map[string]bool, vars map[string]string) []string {
	var includes []string
	inDefine := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\\\n", " "), "\n") {
		if strings.HasPrefix(line, "\t") {
			continue // a recipe
		}
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		word, rest, _ := strings.Cut(line, " ")
		switch {
		case inDefine:
			inDefine = word != "endef"
			continue
		case word == "define":
			inDefine = true
			continue
		case word == "include" || word == "-include" || word == "sinclude":
			includes = append(includes, strings.Fields(rest)...)
			continue
		}
		colon, eq := strings.Index(line, ":"), strings.Index(line, "=")
		switch {
		case eq >= 0 && (colon < 0 || eq < colon || strings.Trim(line[colon:eq], ":") == ""):
			name := strings.TrimSpace(strings.TrimRight(line[:eq], ":?+!"))
			name = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(name, "export "), "override "))
			value := strings.TrimSpace(line[eq+1:])
			switch {
			case strings.HasSuffix(line[:eq], "!"):
				vars[name] = "$" // a command's output: cannot tell
			case strings.HasSuffix(line[:eq], "+"):
				vars[name] = strings.TrimSpace(vars[name] + " " + value)
			default:
				vars[name] = value
			}
		case colon > 0:
			raw[line[:colon]] = true
			if strings.TrimSpace(line[:colon]) == ".PHONY" {
				raw[line[colon+1:]] = true
			}
		}
	}
	return includes
}

// expandMake is s with each $(NAME) a makefile set replaced, a few levels.
func expandMake(s string, vars map[string]string) string {
	for range 4 {
		s = makeVar.ReplaceAllStringFunc(s, func(m string) string {
			if v, ok := vars[makeVar.FindStringSubmatch(m)[1]]; ok {
				return v
			}
			return m
		})
	}
	return s
}

// makes is whether make in dir can make t: a rule names it, a pattern rule
// matches it, or a file is there that it is (up to date) or that make's
// built-in rules make it from (t.c, t.sh, ...).
func makes(rules, tree map[string]bool, dir, t string) bool {
	p := path.Join(dir, t)
	if rules[t] || tree[p] {
		return true
	}
	for r := range rules {
		if pre, suf, ok := strings.Cut(r, "%"); ok && len(t) >= len(pre)+len(suf) && strings.HasPrefix(t, pre) && strings.HasSuffix(t, suf) {
			return true
		}
	}
	for k := range tree {
		if strings.HasPrefix(k, p+".") && !strings.Contains(k[len(p):], "/") {
			return true
		}
	}
	return false
}

// targetList is the rules a line names: no special (.X) or pattern target.
func targetList(rules map[string]bool) string {
	var names []string
	for _, r := range slices.Sorted(maps.Keys(rules)) {
		if !strings.HasPrefix(r, ".") && !strings.Contains(r, "%") {
			names = append(names, r)
		}
	}
	if len(names) > 10 {
		return strings.Join(names[:10], ", ") + fmt.Sprintf(" and %d more", len(names)-10)
	}
	return orWord(strings.Join(names, ", "), "none")
}

// fileAt is the text of p at the stream's base tip, read once per add.
func (e *Engine) fileAt(ctx context.Context, sb *streamAtBase, p string) (string, error) {
	if t, ok := sb.texts[p]; ok {
		return t, nil
	}
	t, err := e.Origin.File(ctx, sb.url, sb.tip, p)
	if err != nil {
		return "", RefusalOr(err, broken("the file "+p+" of "+sb.s.Repo+" at "+shortSHA(sb.tip)+" cannot be read ("+errText(err)+")", "make "+sb.url+" cloneable from this host, then add the card again"))
	}
	sb.texts[p] = t
	return t, nil
}
