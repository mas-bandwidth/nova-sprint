package main

import (
	"bytes"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

type report struct {
	movedPkgs  []string // package dirs under the move roots, as written
	copied     []string // copied packages, as written
	tlaModules []string // TLA+ modules copied by model dependency closure
	files      int
}

type file struct {
	data []byte
	mode fs.FileMode
}

type seeder struct {
	r           *recipe
	from        string
	files       map[string]file // slash path in the new tree -> content
	copiedModel []string
}

// run builds the whole tree in memory, then writes it under out, which must
// not exist yet: the tool never deletes or overwrites anything.
func run(r *recipe, rel release, from, keep, out string) (*report, error) {
	if _, err := os.Lstat(out); err == nil {
		return nil, fmt.Errorf("%s already exists; -out must be a new directory", out)
	}
	if rel.tag == "" || rel.commit == "" {
		return nil, fmt.Errorf("no nova-tools release tag and commit to record in %s; seed from a tagged release", sprint.NovaToolsVersionFile)
	}
	s := &seeder{r: r, from: from, files: map[string]file{}}
	rep := &report{}

	// Moved: every file under each root; the imports of every Go file there
	// (tests included) start the closure.
	var want []string
	pkgs := map[string]bool{}
	for _, root := range r.move {
		err := s.walk(root, func(rel string, data []byte, mode fs.FileMode) error {
			if strings.HasSuffix(rel, ".go") {
				out, deps, err := s.rewrite(data, true)
				if err != nil {
					if lenient(rel) {
						out, deps = data, nil
					} else {
						return fmt.Errorf("%s: %w", rel, err)
					}
				}
				data = out
				want = append(want, deps...)
				if !lenient(rel) {
					pkgs[path.Dir(rel)] = true
				}
			}
			s.files[rel] = file{data, mode}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for p := range pkgs {
		rep.movedPkgs = append(rep.movedPkgs, p)
	}
	sort.Strings(rep.movedPkgs)

	// Copied: the closure over the copied packages' non-test imports.
	seen := map[string]bool{}
	for len(want) > 0 {
		pkg := want[0]
		want = want[1:]
		if seen[pkg] || s.moved(pkg) {
			continue
		}
		seen[pkg] = true
		deps, err := s.copyPackage(pkg)
		if err != nil {
			return nil, err
		}
		want = append(want, deps...)
		rep.copied = append(rep.copied, s.dest(pkg))
	}
	sort.Strings(rep.copied)

	for _, d := range r.doc {
		if err := s.take(d); err != nil {
			return nil, err
		}
	}
	for _, sec := range r.section {
		if err := s.sections(sec, keep); err != nil {
			return nil, err
		}
	}
	if err := s.ratingFiles(); err != nil {
		return nil, err
	}
	if err := s.models(); err != nil {
		return nil, err
	}
	rep.tlaModules = append(rep.tlaModules, s.copiedModel...)
	if r.gomod {
		if err := s.goMod(keep); err != nil {
			return nil, err
		}
	}
	// Kept last: nova-sprint's own files win over anything taken above.
	for _, k := range r.keep {
		err := walkFiles(keep, k, func(rel string, data []byte, mode fs.FileMode) error {
			s.files[rel] = file{data, mode}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("keep %s: %w", k, err)
		}
	}
	// The copied tree's version constant and provenance record describe the
	// same exact source tag. This also permits private prerelease canaries.
	if err := s.setToolsVersion(rel.tag); err != nil {
		return nil, err
	}
	// Last of all: the release the tree came from, which nova-sprint seat check
	// requires at least.
	s.files[sprint.NovaToolsVersionFile] = file{[]byte(sprint.FormatNovaToolsVersion(rel.tag, rel.commit)), 0o644}

	names := make([]string, 0, len(s.files))
	for n := range s.files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		dst := filepath.Join(out, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, s.files[n].data, s.files[n].mode.Perm()); err != nil {
			return nil, err
		}
	}
	rep.files = len(names)
	return rep, nil
}

// lenient is a file the go tool never builds as part of a package: under
// testdata, or under a directory whose name starts with _ or a dot.
func lenient(rel string) bool {
	for _, seg := range strings.Split(path.Dir(rel), "/") {
		if seg == "testdata" || strings.HasPrefix(seg, "_") || strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func (s *seeder) moved(pkg string) bool {
	for _, root := range s.r.move {
		if pkg == root || strings.HasPrefix(pkg, root+"/") {
			return true
		}
	}
	return false
}

// dest is where a nova-tools package lands in this module.
func (s *seeder) dest(pkg string) string {
	for from, to := range s.r.rename {
		if pkg == from {
			return to
		}
		if strings.HasPrefix(pkg, from+"/") {
			return to + strings.TrimPrefix(pkg, from)
		}
	}
	return pkg
}

func (s *seeder) renamed(pkg string) bool { return s.dest(pkg) != pkg }

// rewrite points every import spec of the source module at this module, and
// returns the source module's packages the file imports. Only the import path
// literals change (then gofmt sorts the imports); moved files also get the
// recipe's literal replacements.
func (s *seeder) rewrite(src []byte, moved bool) ([]byte, []string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	type splice struct {
		start, end int
		with       string
	}
	var deps []string
	var sp []splice
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, nil, err
		}
		if p == s.r.fromMod {
			return nil, nil, fmt.Errorf("imports the root of %s", p)
		}
		if !strings.HasPrefix(p, s.r.fromMod+"/") {
			continue
		}
		rel := strings.TrimPrefix(p, s.r.fromMod+"/")
		deps = append(deps, rel)
		sp = append(sp, splice{
			start: fset.Position(spec.Path.Pos()).Offset,
			end:   fset.Position(spec.Path.End()).Offset,
			with:  strconv.Quote(s.r.toMod + "/" + s.dest(rel)),
		})
	}
	out := src
	for i := len(sp) - 1; i >= 0; i-- {
		var b bytes.Buffer
		b.Write(out[:sp[i].start])
		b.WriteString(sp[i].with)
		b.Write(out[sp[i].end:])
		out = b.Bytes()
	}
	if moved {
		for _, l := range s.r.literal {
			out = bytes.ReplaceAll(out, []byte(l[0]), []byte(l[1]))
		}
	}
	if len(sp) > 0 {
		if out, err = format.Source(out); err != nil {
			return nil, nil, err
		}
	}
	return out, deps, nil
}

// copyPackage takes a package's non-test files (only its Go files when it is
// renamed) and the files its //go:embed lines name, and returns its imports.
func (s *seeder) copyPackage(pkg string) ([]string, error) {
	dir := filepath.Join(s.from, filepath.FromSlash(pkg))
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("copy %s: %w", pkg, err)
	}
	dest := s.dest(pkg)
	var deps []string
	var embeds []string
	gofiles := 0
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if !e.Type().IsRegular() {
			return nil, fmt.Errorf("copy %s: %s is not a regular file", pkg, name)
		}
		isGo := strings.HasSuffix(name, ".go")
		if !isGo && s.renamed(pkg) {
			continue
		}
		data, mode, err := readFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if isGo {
			gofiles++
			embeds = append(embeds, embedPatterns(data)...)
			out, d, err := s.rewrite(data, false)
			if err != nil {
				return nil, fmt.Errorf("%s/%s: %w", pkg, name, err)
			}
			data = out
			deps = append(deps, d...)
		}
		s.files[dest+"/"+name] = file{data, mode}
	}
	if gofiles == 0 {
		return nil, fmt.Errorf("copy %s: no Go files", pkg)
	}
	for _, pat := range embeds {
		all := strings.HasPrefix(pat, "all:")
		pat = strings.TrimPrefix(pat, "all:")
		matches, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(pat)))
		if err != nil || len(matches) == 0 {
			return nil, fmt.Errorf("copy %s: //go:embed %s matches nothing", pkg, pat)
		}
		for _, m := range matches {
			rel, _ := filepath.Rel(s.from, m)
			err := walkFiles(s.from, filepath.ToSlash(rel), func(r string, data []byte, mode fs.FileMode) error {
				in := strings.TrimPrefix(r, pkg+"/")
				if !all && hiddenEmbed(in) {
					return nil
				}
				s.files[dest+"/"+in] = file{data, mode}
				// Embedded Go sources are copied as resources, but can still be
				// compiled in the destination package (for example through an
				// embed-driven generated source workflow). Keep their imports in
				// lockstep with the ordinary package files.
				if strings.HasSuffix(in, ".go") {
					out, _, err := s.rewrite(data, false)
					if err != nil {
						return fmt.Errorf("embedded %s: %w", r, err)
					}
					s.files[dest+"/"+in] = file{out, mode}
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return deps, nil
}

// hiddenEmbed is a file a directory embed skips without all: (a path element
// starting with . or _ below the package).
func hiddenEmbed(rel string) bool {
	segs := strings.Split(rel, "/")
	for _, seg := range segs[:len(segs)-1] {
		if strings.HasPrefix(seg, ".") || strings.HasPrefix(seg, "_") {
			return true
		}
	}
	return false
}

// embedPatterns reads the //go:embed lines of a Go file.
func embedPatterns(src []byte) []string {
	var pats []string
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "//go:embed ")
		if !ok {
			continue
		}
		for _, f := range strings.Fields(rest) {
			if u, err := strconv.Unquote(f); err == nil {
				f = u
			}
			pats = append(pats, f)
		}
	}
	return pats
}

func (s *seeder) take(rel string) error {
	data, mode, err := readFile(filepath.Join(s.from, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	s.files[rel] = file{data, mode}
	return nil
}

// sections writes nova-sprint's header of a reference doc (everything before
// its first "## " line) and then each named "## " section of the nova-tools
// doc, in the recipe's order, each followed by one blank line.
func (s *seeder) sections(sec section, keep string) error {
	own, _, err := readFile(filepath.Join(keep, filepath.FromSlash(sec.file)))
	if err != nil {
		return fmt.Errorf("section %s: the header comes from nova-sprint's copy: %w", sec.file, err)
	}
	src, mode, err := readFile(filepath.Join(s.from, filepath.FromSlash(sec.file)))
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, l := range strings.SplitAfter(string(own), "\n") {
		if strings.HasPrefix(l, "## ") {
			break
		}
		b.WriteString(l)
	}
	lines := strings.SplitAfter(string(src), "\n")
	for _, name := range sec.names {
		start := -1
		for i, l := range lines {
			if strings.TrimRight(l, "\n") == "## "+name {
				start = i
				break
			}
		}
		if start < 0 {
			return fmt.Errorf("section %s: no \"## %s\"", sec.file, name)
		}
		end := len(lines)
		for j := start + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "## ") {
				end = j
				break
			}
		}
		body := strings.Join(lines[start:end], "")
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		b.WriteString(body + "\n")
	}
	s.files[sec.file] = file{[]byte(b.String()), mode}
	return nil
}

func (s *seeder) ratingFiles() error {
	if len(s.r.ratings) == 0 {
		return nil
	}
	names := map[string]bool{}
	for _, t := range s.r.ratings {
		names[t+"-read.md"] = true
		names[t+"-use.md"] = true
	}
	return walkFiles(s.from, "docs/ratings", func(rel string, data []byte, mode fs.FileMode) error {
		if names[path.Base(rel)] {
			s.files[rel] = file{data, mode}
		}
		return nil
	})
}

// models takes each model's modules, configs, README and bench, and the rows
// of tla/CASES.tsv and tla/RUNS.tsv whose module is the model's MC module.
func (s *seeder) models() error {
	if len(s.r.model) == 0 {
		return nil
	}
	mods := map[string]bool{}
	selected := map[string]bool{}
	for _, m := range s.r.model {
		mods["MC"+m+".tla"] = true
		for _, f := range []string{m + ".tla", "MC" + m + ".tla"} {
			if err := s.takeModel(f, selected); err != nil {
				return err
			}
		}
		if _, err := os.Stat(filepath.Join(s.from, "tla", "README-"+m+".md")); err == nil {
			if err := s.take("tla/README-" + m + ".md"); err != nil {
				return err
			}
		}
		bench := "tla/" + strings.ToLower(m) + "-bench"
		if st, err := os.Stat(filepath.Join(s.from, filepath.FromSlash(bench))); err == nil && st.IsDir() {
			err := walkFiles(s.from, bench, func(rel string, data []byte, mode fs.FileMode) error {
				s.files[rel] = file{data, mode}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"tla/CASES.tsv", "tla/RUNS.tsv"} {
		data, mode, err := readFile(filepath.Join(s.from, filepath.FromSlash(table)))
		if err != nil {
			return err
		}
		var b strings.Builder
		for i, l := range strings.SplitAfter(string(data), "\n") {
			if l == "" {
				continue
			}
			f := strings.Split(strings.TrimRight(l, "\n"), "\t")
			if i == 0 || (len(f) > 1 && mods[f[1]]) {
				b.WriteString(l)
				if i > 0 && table == "tla/CASES.tsv" {
					if err := s.take("tla/" + f[0]); err != nil {
						return err
					}
				}
			}
		}
		s.files[table] = file{[]byte(b.String()), mode}
	}
	s.copiedModel = s.copiedModel[:0]
	for m := range selected {
		s.copiedModel = append(s.copiedModel, m)
	}
	sort.Strings(s.copiedModel)
	return nil
}

// takeModel copies a TLA+ module and recursively closes over local EXTENDS and
// INSTANCE references. TLC's bundled standard modules are intentionally not
// copied; every other reference must resolve to a file in tla/.
func (s *seeder) takeModel(name string, seen map[string]bool) error {
	if seen[name] {
		return nil
	}
	if filepath.Base(name) != name || !strings.HasSuffix(name, ".tla") {
		return fmt.Errorf("invalid TLA module name %q", name)
	}
	data, mode, err := readFile(filepath.Join(s.from, "tla", name))
	if err != nil {
		return fmt.Errorf("TLA module %s: %w", name, err)
	}
	seen[name] = true
	s.files["tla/"+name] = file{data, mode}
	for _, ref := range moduleReferences(data) {
		if standardTLA[ref] {
			continue
		}
		if err := s.takeModel(ref+".tla", seen); err != nil {
			return fmt.Errorf("TLA module %s references %s: %w", name, ref, err)
		}
	}
	return nil
}

func (s *seeder) setToolsVersion(tag string) error {
	const fileName = "internal/sprint/toolsversion.go"
	f, ok := s.files[fileName]
	if !ok {
		return fmt.Errorf("keep %s so the seeded runtime version matches its provenance", fileName)
	}
	const marker = `const NovaToolsVersion = "`
	text := string(f.data)
	i := strings.Index(text, marker)
	if i < 0 || strings.Contains(text[i+len(marker):], marker) {
		return fmt.Errorf("%s must contain exactly one NovaToolsVersion string constant", fileName)
	}
	start := i + len(marker)
	end := strings.IndexByte(text[start:], '"')
	if end < 0 {
		return fmt.Errorf("%s has a malformed NovaToolsVersion constant", fileName)
	}
	text = text[:start] + tag + text[start+end:]
	f.data = []byte(text)
	s.files[fileName] = f
	return nil
}

// goMod is nova-tools' go.mod under this module's name with nova-sprint's own
// tool lines, and nova-tools' go.sum; `go mod tidy` then prunes both.
func (s *seeder) goMod(keep string) error {
	src, mode, err := readFile(filepath.Join(s.from, "go.mod"))
	if err != nil {
		return err
	}
	own, _, err := readFile(filepath.Join(keep, "go.mod"))
	if err != nil {
		return err
	}
	ownTools := toolLines(string(own))
	var b strings.Builder
	in := false
	for _, l := range strings.SplitAfter(string(src), "\n") {
		t := strings.TrimSpace(l)
		switch {
		case in:
			if t == ")" {
				in = false
			}
			continue
		case t == "tool (":
			in = true
			continue
		case strings.HasPrefix(t, "tool "):
			continue
		case strings.HasPrefix(t, "module "):
			b.WriteString("module " + s.r.toMod + "\n")
			continue
		}
		b.WriteString(l)
		if strings.HasPrefix(t, "go ") && len(ownTools) > 0 {
			b.WriteString("\n")
			for _, tl := range ownTools {
				b.WriteString("tool " + tl + "\n")
			}
		}
	}
	// Removing the tool block leaves a doubled blank line; collapse it.
	text := b.String()
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	s.files["go.mod"] = file{[]byte(text), mode}
	return s.take("go.sum")
}

func toolLines(gomod string) []string {
	var tools []string
	in := false
	for _, l := range strings.Split(gomod, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case in && t == ")":
			in = false
		case in && t != "":
			tools = append(tools, t)
		case t == "tool (":
			in = true
		case strings.HasPrefix(t, "tool "):
			tools = append(tools, strings.TrimSpace(strings.TrimPrefix(t, "tool ")))
		}
	}
	return tools
}

func (s *seeder) walk(root string, fn func(rel string, data []byte, mode fs.FileMode) error) error {
	return walkFiles(s.from, root, fn)
}

// walkFiles calls fn for every regular file at or under base/rel (rel is a
// slash path), with its slash path relative to base.
func walkFiles(base, rel string, fn func(rel string, data []byte, mode fs.FileMode) error) error {
	top := filepath.Join(base, filepath.FromSlash(rel))
	return filepath.WalkDir(top, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		r, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		data, mode, err := readFile(p)
		if err != nil {
			return err
		}
		return fn(filepath.ToSlash(r), data, mode)
	})
}

func readFile(p string) ([]byte, fs.FileMode, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(p)
	return data, st.Mode(), err
}
