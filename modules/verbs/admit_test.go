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
