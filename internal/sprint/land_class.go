package sprint

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/internal/cardgen"
)

// Class is one class of the base's class suite (SPEC-SPRINT, the base's class
// gate). Needs are tree packages the clone must hold. Stage is the suite step the
// class runs in: 0 build; 1 the read-only whole-tree checks (gofmt, the vets, the
// analyzers); 2 the package tests. Classes in one stage may go side by side. Batch
// is non-empty for classes that share one run (the whole-tree analyzers): the batch
// names it, and a red run is attributed to the class its FAIL line names.
type Class struct {
	Name  string
	Run   []string
	Test  string
	Needs []string
	Stage int
	Batch string
}

// BaseClasses includes the existing build and every class that guards a base.
// The whole-tree analyzers need the functional tag, but start no servers.
var BaseClasses = []Class{
	{Name: "build", Run: []string{"go", "build", "./..."}, Stage: 0},
	{Name: "gofmt", Run: []string{"gofmt", "-l", "."}, Stage: 1},
	{Name: "vet", Run: []string{"go", "vet", "./..."}, Stage: 1},
	// vet-functional is the base's own functional-tier vet (Makefile vet-functional):
	// a plain vet compiles no file built only behind the functional tag.
	{Name: "vet-functional", Run: []string{"go", "vet", "-tags", "functional", "./..."}, Stage: 1},
	{Name: "staticcheck", Run: []string{"go", "test", "-tags", "functional", "-count=1", "-timeout", "600s", "-run", "^TestStaticcheckFindings$", "./internal/ci/"}, Test: "internal/ci TestStaticcheckFindings", Needs: []string{"internal/ci"}, Stage: 1, Batch: "ci-analyzers"},
	{Name: "errcheck", Run: []string{"go", "test", "-tags", "functional", "-count=1", "-timeout", "600s", "-run", "^TestUncheckedErrors$", "./internal/ci/"}, Test: "internal/ci TestUncheckedErrors", Needs: []string{"internal/ci"}, Stage: 1, Batch: "ci-analyzers"},
	{Name: "dead-code", Run: []string{"go", "test", "-tags", "functional", "-count=1", "-timeout", "600s", "-run", "^TestDeadCode$", "./internal/ci/"}, Test: "internal/ci TestDeadCode", Needs: []string{"internal/ci"}, Stage: 1, Batch: "ci-analyzers"},
	{Name: "class-tests", Run: []string{"go", "test", "-count=1", "-timeout", "600s"}, Needs: []string{"internal/ci", "internal/docs"}, Stage: 2},
}

// ClassForBatch returns the classes that share one run (BaseClasses' Batch): the
// whole-tree analyzer classes, in report order. nil for a class that runs alone.
func ClassForBatch(batch string) []Class {
	var out []Class
	for _, c := range BaseClasses {
		if batch != "" && c.Batch == batch {
			out = append(out, c)
		}
	}
	return out
}

// ClassForOutput returns the class a red suite run is attributed to: for a run
// that covers several classes (the analyzers) the class whose test a `--- FAIL:`
// line names, and for a run of one class that class.
func ClassForOutput(cmd, out string) (Class, bool) {
	if m := classFailRE.FindStringSubmatch(out); m != nil {
		for _, c := range BaseClasses {
			if c.Test != "" && strings.HasSuffix(c.Test, " "+m[1]) && strings.Contains(cmd, m[1]) {
				return c, true
			}
		}
	}
	cmd = strings.TrimSpace(cmd)
	for _, c := range BaseClasses {
		if strings.Join(c.Run, " ") == cmd {
			return c, true
		}
	}
	return Class{}, false
}

var (
	classFileRE    = regexp.MustCompile(`(?m)(?:^|[\s|])(?:\./)?([A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*\.go)\b`)
	classFailRE    = regexp.MustCompile(`--- FAIL: (Test[A-Za-z0-9_]*)`)
	classFailPkgRE = regexp.MustCompile(`FAIL(?:\s|\\x09)+[^\s|]+/(internal/(?:ci|docs))\b`)
)

// UnitPackages names the unit packages a head's merge could break, from the go.mod
// module path and the files the merge changed. For module github.com/mas-bandwidth/nova-sprint:
// production source or data under pkg/ or internal/ gives the whole-tree pattern "...",
// so reverse importers' tests run too. Markdown and docs/ are documentation; _test.go
// and testdata/ are test-only inputs. Selection uses paths, so deleted production data
// still triggers the gate. A test-only change under internal/sprint/ or a .go file or
// testdata under cmd/nova-sprint/ gives both bare directories internal/sprint and
// cmd/nova-sprint; a changed .go file elsewhere gives its own package directory.
// For any other module (nova-tools): the package directory of each changed .go
// file or testdata, deduplicated and sorted; reverse dependencies are not in this card.
// Outside the shared production inputs, docs, ledgers and other non-Go files give none.
func UnitPackages(module string, changed []string) []string {
	seen := map[string]bool{}
	var pkgs []string

	for _, f := range changed {
		isGoFile := strings.HasSuffix(f, ".go")
		isTestData := strings.HasSuffix(f, "testdata") || strings.Contains(f, "/testdata/")
		shared := strings.HasPrefix(f, "pkg/") || strings.HasPrefix(f, "internal/")
		isDoc := strings.HasSuffix(f, ".md") || strings.Contains(f, "/docs/")
		if module == "github.com/mas-bandwidth/nova-sprint" && shared && !isTestData && !isDoc && !strings.HasSuffix(f, "_test.go") {
			return []string{"..."}
		}
		if !isGoFile && !isTestData {
			continue
		}
		dir := filepath.Dir(f)
		if dir == "." || dir == "testdata" {
			continue
		}
		switch module {
		case "github.com/mas-bandwidth/nova-sprint":
			if strings.HasPrefix(dir, "internal/sprint") || strings.HasPrefix(dir, "cmd/nova-sprint") {
				for _, p := range []string{"internal/sprint", "cmd/nova-sprint"} {
					if !seen[p] {
						seen[p] = true
						pkgs = append(pkgs, p)
					}
				}
			} else {
				if !seen[dir] {
					seen[dir] = true
					pkgs = append(pkgs, dir)
				}
			}
		default:
			if !seen[dir] {
				seen[dir] = true
				pkgs = append(pkgs, dir)
			}
		}
	}
	slices.Sort(pkgs)
	return pkgs
}

// ClassGateWhy names the first red run with the repair card the generator
// stamps for it. The existing base-gate rule stores this on its one judgment.
// This is the metadata half of SPEC-SPRINT's base class gate, not another gate.
func ClassGateWhy(dir, base, sha, why string) string {
	var class Class
	for _, c := range BaseClasses {
		if strings.HasPrefix(why, strings.Join(c.Run, " ")+":") || c.Name == "class-tests" && strings.HasPrefix(why, strings.Join(c.Run, " ")+" ") {
			class = c
			break
		}
	}
	if class.Name == "" {
		return why
	}
	test := class.Test
	if m := classFailRE.FindStringSubmatch(why); m != nil {
		pkg := "internal/ci"
		if p := classFailPkgRE.FindStringSubmatch(why); p != nil {
			pkg = p[1]
		}
		test = pkg + " " + m[1]
	}
	var files []string
	seen := map[string]bool{}
	for _, m := range classFileRE.FindAllStringSubmatch(why, -1) {
		f := m[1]
		if !filepath.IsLocal(f) || seen[f] {
			continue
		}
		seen[f] = true
		if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); err == nil && st.Mode().IsRegular() {
			files = append(files, f)
		}
	}
	card := cardgen.PlanClassRed(cardgen.ClassRed{Class: class.Name, Run: strings.Join(class.Run, " "), Test: test, Files: files, Finding: why}, base, "")
	return cutText(fmt.Sprintf("the base %s at %s is red on its class %s; fix-red card %s (TEST: %s); %s", base, sha[:min(12, len(sha))], class.Name, card.ID, card.Test, why), MaxCardTextBytes)
}
