package verbs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
)

// TestStreamAdmission: stream add proves the card branches creatable (git's
// ref-name rules, no base or origin branch that is their parent) and, with
// forge github, the host's gh identity able to act; each refusal names its
// one fix (intent rule 34).
func TestStreamAdmission(t *testing.T) {
	t.Parallel()
	add := func(name string, more ...string) []string {
		return append([]string{"stream", "add", name, "--repo", "o/r"}, more...)
	}
	runCases(t, []tcase{
		{name: "a name git refuses", as: "carol", args: add("x.lock"), exit: 1, want: []string{"the card branches sprint/x.lock/<card> cannot be created", "ends with .lock", "name the stream or the base otherwise"}},
		{name: "a base that is the parent", as: "carol", args: add("t", "--base", "sprint/t"), exit: 1, want: []string{"the base sprint/t is a branch"}},
		{name: "a parent branch at origin", as: "carol", args: add("t"), exit: 1, prep: func(_ *testing.T, w *world) { w.origin.tips["o/r sprint/t"] = tip },
			want: []string{"branch sprint/t is at o/r's origin", "git push origin --delete sprint/t"}},
		{name: "a sprint branch at origin", as: "carol", args: add("t"), exit: 1, prep: func(_ *testing.T, w *world) { w.origin.tips["o/r sprint"] = tip },
			want: []string{"branch sprint is at o/r's origin"}},
		{name: "a forge that cannot act", as: "carol", args: add("t", "--forge", "github"), exit: 1,
			prep: func(_ *testing.T, w *world) {
				w.forge.act = refuse("this host's gh identity has READ permission on o/r", "give it write")
			},
			want: []string{"READ permission on o/r", "give it write"}},
		{name: "a forge that cannot run", as: "carol", args: add("t", "--forge", "github"), exit: 2,
			prep: func(_ *testing.T, w *world) { w.forge.act = errBroken }, want: []string{"gh cannot act on o/r", "gh auth login"}},
		{name: "no forge", as: "carol", args: add("t", "--forge", "github"), exit: 2,
			prep: func(_ *testing.T, w *world) { w.e.Forge = nil }, want: []string{"the forge is not configured"}},
	})
}

// briefFile writes text as dir/id.md.
func briefFile(t *testing.T, dir, id, text string) string {
	t.Helper()
	path := filepath.Join(dir, id+".md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCardAdmission: add proves each producer: its stream live, its tier
// and routes served, its PATHS at the base, DONE-WHEN parsing, no retired
// mechanic or named bench, and its work not already at the base.
func TestCardAdmission(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	b := func(id, meta string) []string {
		text := "tier: pro\nPATHS: a.go\nTEST: go test ./...\nDONE-WHEN: go test ./...\n" + meta + "\nDo it.\n"
		return []string{"add", "--stream", "s", "--brief-file", briefFile(t, dir, id, text)}
	}
	paths := func(id, p string) []string {
		return []string{"add", "--stream", "s", "--brief-file", briefFile(t, dir, id, "tier: pro\nPATHS: "+p+"\nTEST: t\nDONE-WHEN: true\n\nDo it.\n")}
	}
	tree := func(_ *testing.T, w *world) { w.origin.tree = []string{"README.md", "b/x.go", "b/c/y.go"} }
	noPro := func(_ *testing.T, w *world) {
		w.roster.ro = roster.Roster{Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 1, Tiers: []string{"flash"}}}}
	}
	runCases(t, []tcase{
		{name: "an unserved tier", as: "carol", args: b("u1", ""), exit: 1, prep: noPro, want: []string{"card u1: no row in nova-config serves tier pro", "nova-config friend set"}},
		{name: "an unserved route", as: "carol", args: b("u2", "NEEDS-ROUTE: ipad\n"), exit: 1, want: []string{"serves tier pro with route(s) ipad", "--roles bench,route:ipad"}},
		{name: "a served route", as: "carol", args: b("u3", "NEEDS-ROUTE: ipad\n"), exit: 0,
			prep: func(_ *testing.T, w *world) {
				a := w.roster.ro.Workers["alice"]
				a.Routes = []string{"ipad"}
				w.roster.ro.Workers["alice"] = a
			}, want: []string{"ADDED 1 cards"}},
		{name: "a missing directory", as: "carol", args: paths("p3", "z/q.go"), exit: 1, prep: tree, want: []string{"card p3: PATHS z/q.go: neither it nor its directory z/ is there", "ending in /"}},
		{name: "a new file in a directory", as: "carol", args: paths("p4", "b/new.go, b/c/y.go"), exit: 0, prep: tree, want: []string{"ADDED 1 cards"}},
		{name: "a declared new directory", as: "carol", args: paths("p5", "b/d/, b/d/n.go"), exit: 0, prep: tree, want: []string{"ADDED 1 cards"}},
		{name: "a glob that matches", as: "carol", args: paths("p6", "b/*/y.go"), exit: 0, prep: tree, want: []string{"ADDED 1 cards"}},
		{name: "a glob that matches nothing", as: "carol", args: paths("p7", "q/*.go"), exit: 1, prep: tree, want: []string{"the glob matches nothing"}},
		{name: "a dead stream", as: "carol", args: b("d1", ""), exit: 1, prep: func(_ *testing.T, w *world) { delete(w.origin.tips, "o/r main") }, want: []string{"branch main is not at o/r's origin"}},
		{name: "an unreadable tree", as: "carol", args: b("d2", ""), exit: 2, prep: func(_ *testing.T, w *world) { w.e.Origin = &failingTree{w.origin} }, want: []string{"the tree of o/r", "cloneable"}},
		{name: "its branch already at the base", as: "carol", args: b("x1", ""), exit: 1,
			prep: func(_ *testing.T, w *world) { w.origin.tips["o/r sprint/s/x1"] = tip; w.origin.contains = true },
			want: []string{"card x1: its work is already at the base: its branch sprint/s/x1", "do not add it"}},
		{name: "its branch not at the base", as: "carol", args: b("x2", ""), exit: 0, prep: func(_ *testing.T, w *world) { w.origin.tips["o/r sprint/s/x2"] = tip }, want: []string{"ADDED 1 cards"}},
		{name: "a gate naming a bench", as: "carol", args: b("n1", "DONE-WHEN: make test HOST=alice\n"), exit: 1, want: []string{"card n1: line 5's gate names the bench alice", "never a host"}},
	})
}

// TestCardAdmissionAtBase: a brief's facts about the repository are checked
// at the base: a new directory beside its near namesake (test/ where tests/
// is), a make target no Makefile rule names, and a repository path a
// DONE-WHEN runs that is not there; each refusal names what is missing and
// its fix (2026-11-09: rocketnet cards dealt with test/ and make test).
func TestCardAdmissionAtBase(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	card := func(id, paths string, gates ...string) []string {
		text := "tier: pro\nPATHS: " + paths + "\nTEST: t\n"
		for _, g := range gates {
			text += "DONE-WHEN: " + g + "\n"
		}
		return []string{"add", "--stream", "s", "--brief-file", briefFile(t, dir, id, text+"\nDo it.\n")}
	}
	repo := func(_ *testing.T, w *world) {
		w.origin.tree = []string{"Makefile", "tests/a_test.c", "sub/Makefile", "mk/rules.mk", "tools/run.sh", "b/x.go", "Docs/x.md", "dyn/Makefile"}
		w.origin.files = map[string]string{
			"Makefile":     ".DEFAULT_GOAL := build\n# test: not a rule\n.PHONY: build happy-path\nbuild:\n\tcc -o bin/check x.c\nhappy-path: build\n\t./x \\\n\t  y\ninclude mk/rules.mk\nBIN := out\n$(BIN): build\n%.o: %.c\n",
			"mk/rules.mk":  "lint:\n\ttrue\n",
			"sub/Makefile": "test:\n\ttrue\n",
			"dyn/Makefile": "include $(X).mk\n",
		}
	}
	runCases(t, []tcase{
		{name: "test/ beside tests/", as: "carol", args: card("q1", "test/, test/b_test.c", "true"), exit: 1, prep: repo,
			want: []string{"card q1: PATHS test/: test/ is not there, and tests/ is", "o/r's main"}},
		{name: "a file under test/ beside tests/", as: "carol", args: card("q2", "test/b_test.c", "true"), exit: 1, prep: repo,
			want: []string{"neither it nor its directory test/ is there (tests/ is)"}},
		{name: "a case namesake", as: "carol", args: card("q3", "docs/, docs/y.md", "true"), exit: 1, prep: repo, want: []string{"docs/ is not there, and Docs/ is"}},
		{name: "a new directory with no namesake", as: "carol", args: card("q4", "b/d/, b/d/n.go", "true"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
		{name: "make with no such target", as: "carol", args: card("m1", "README.md", "make test"), exit: 1, prep: repo,
			want: []string{"card m1: DONE-WHEN make test: Makefile has no rule for test", "its targets: build, happy-path, lint, out", "o/r's main", "a target the Makefile has"}},
		{name: "make with its targets", as: "carol", args: card("m2", "README.md", "make -j 4 build V=1", "make happy-path", "make lint", "make out", "make x.o"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
		{name: "make -C a directory", as: "carol", args: card("m3", "README.md", "make -C sub test"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
		{name: "make -C a directory without the target", as: "carol", args: card("m4", "README.md", "make -C sub build"), exit: 1, prep: repo, want: []string{"sub/Makefile has no rule for build"}},
		{name: "make -C a directory with no Makefile", as: "carol", args: card("m5", "README.md", "make -C tools all"), exit: 1, prep: repo, want: []string{"there is no Makefile in tools/"}},
		{name: "make with an unreadable include", as: "carol", args: card("m6", "README.md", "make -C dyn anything"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
		{name: "make when the card writes the Makefile", as: "carol", args: card("m7", "Makefile", "make test"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
		{name: "a script that is not there", as: "carol", args: card("r1", "README.md", "./bin/check --all"), exit: 1, prep: repo,
			want: []string{"card r1: DONE-WHEN ./bin/check --all: bin/check is not there", "add it to PATHS"}},
		{name: "a script built first", as: "carol", args: card("r2", "README.md", "make build", "./bin/check"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
		{name: "a script at the base", as: "carol", args: card("r3", "README.md", "tools/run.sh"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
		{name: "a script the card writes", as: "carol", args: card("r4", "tools/new.sh", "./tools/new.sh"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
		{name: "an absolute program", as: "carol", args: card("r5", "README.md", "/usr/bin/true"), exit: 0, prep: repo, want: []string{"ADDED 1 cards"}},
	})
}

// failingTree is origin whose tree cannot be read.
type failingTree struct{ *fakeOrigin }

func (o *failingTree) Tree(context.Context, string, string) ([]string, error) { return nil, errBroken }

// TestBriefContractLint: the parser refuses the new fields' bad forms,
// naming the line and the fix.
func TestBriefContractLint(t *testing.T) {
	t.Parallel()
	good := "tier: pro\nPATHS: a.go\nTEST: go test ./...\nDONE-WHEN: go test ./...\n\nDo it.\n"
	for rule, text := range map[string]string{
		`line 4 DONE-WHEN echo "open is not one command: a " is left open`:                                       strings.Replace(good, "DONE-WHEN: go test ./...", `DONE-WHEN: echo "open`, 1),
		"line 5 DONE-WHEN go test | is not one command: | is shell syntax":                                       strings.Replace(good, "go test ./...\n\n", "go test ./...\nDONE-WHEN: go test |\n\n", 1),
		"line 4 DONE-WHEN go test ./... && go vet ./... is not one command: && is shell syntax":                  strings.Replace(good, "DONE-WHEN: go test ./...", "DONE-WHEN: go test ./... && go vet ./...", 1),
		"line 4 DONE-WHEN Verify the tests pass is not one command: the first word Verify is not a program name": strings.Replace(good, "DONE-WHEN: go test ./...", "DONE-WHEN: Verify the tests pass", 1),
		"must contain a valid NEEDS-ROUTE section":                                                               strings.Replace(good, "PATHS: a.go", "PATHS: a.go\nNEEDS-ROUTE: two words", 1),
	} {
		_, err := parseBrief("b1", text)
		if err == nil || !strings.Contains(err.Error(), rule) {
			t.Fatalf("%q: %v", rule, err)
		}
	}
	a, err := parseBrief("b1", strings.Replace(good, "PATHS: a.go", "PATHS: a.go\nNEEDS-ROUTE: ipad\nNEEDS-ROUTE: usb", 1))
	if err != nil || len(a.DoneWhen) != 1 {
		t.Fatalf("%+v %v", a, err)
	}
}

// TestGateFixSaysShC: a brief refused for a gate that needs a shell says to
// wrap it in sh -c; a stored card is re-linted by the same parser (check).
func TestGateFixSaysShC(t *testing.T) {
	t.Parallel()
	_, err := parseBrief("b1", "tier: pro\nPATHS: a.go\nTEST: t\nDONE-WHEN: `make` ; true\n\nDo it.\n")
	if f := classify(err); !strings.Contains(f.Fix, "sh -c '...'") || !strings.Contains(f.What, "a backquote is shell syntax") {
		t.Fatalf("%+v", f)
	}
	ro := roster.Roster{Workers: map[string]roster.Worker{"alice": {Name: "alice", Width: 1, Tiers: []string{"pro"}}}}
	f := cardFacts{ID: "c1", Tier: "pro", Brief: "tier: pro\n", DoneWhen: []string{"go test ./...", "Tests must all pass"}}
	err = cardLint(f, ro)
	if g := classify(err); !strings.Contains(g.What, "card c1: DONE-WHEN Tests must all pass is not one command") || !strings.Contains(g.Fix, "sh -c '...'") {
		t.Fatalf("%+v %v", g, err)
	}
	f.DoneWhen = []string{"sh -c 'cd x && make | tee log'"}
	if err := cardLint(f, ro); err != nil {
		t.Fatalf("%v", err)
	}
}
