package verbs

import (
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// TestAgreementChecks: a required check looks like a contributor agreement
// by its name; an ordinary check does not.
func TestAgreementChecks(t *testing.T) {
	got := agreementChecks([]string{"Gates", "Contributor Assignment Agreement", "cla/google", "license/cla", "CLA", "build (CLAng)", "Linux Debug", "Individual License Agreement", "Contributor Assignment Agreement"})
	want := []string{"Contributor Assignment Agreement", "cla/google", "license/cla", "CLA", "Individual License Agreement"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestStreamAddWarnsOfAnAgreement: with a required agreement check, stream add
// still adds the stream and prints a WARN naming the check and the identities
// the benches commit as; check prints the same as a CHECK WARN; a repository
// with no such check, or no forge, prints none; a forge that cannot be read
// says so.
func TestStreamAddWarnsOfAnAgreement(t *testing.T) {
	add := []string{"stream", "add", "t", "--repo", "o/r", "--forge", "github"}
	cla := func(_ *testing.T, w *world) { w.forge.checks = []string{"Gates", "Contributor Assignment Agreement"} }
	runCases(t, []tcase{
		{name: "an agreement check", as: "carol", args: add, exit: 0, prep: cla,
			want: []string{"STREAM t repo=o/r base=main", `WARN stream t: o/r's main requires the check "Contributor Assignment Agreement"`,
				"sign it for alice <alice@nova-sprint.local>, bob <bob@nova-sprint.local> before the first card lands", "kin commit as their own git identity"}},
		{name: "checks unreadable", as: "carol", args: add, exit: 0, prep: func(_ *testing.T, w *world) { w.forge.checkE = errBroken },
			want: []string{"STREAM t", "WARN stream t: the checks o/r's main requires cannot be read (connection refused)", "sign it for the identities nova-sprint check lists"}},
		{name: "check warns too", as: "carol", args: []string{"check"}, exit: 0, prep: then(healthy, cla),
			snap: func(s *store.Snapshot) { st := s.Streams["s"]; st.Forge = "github"; s.Streams["s"] = st },
			want: []string{`CHECK WARN stream s: o/r's main requires the check "Contributor Assignment Agreement"`}},
	})
	for name, prep := range map[string]func(*testing.T, *world){
		"no agreement check": func(_ *testing.T, w *world) { w.forge.checks = []string{"Gates", "Linux Debug"} },
		"no checks at all":   func(*testing.T, *world) {},
	} {
		t.Run(name, func(t *testing.T) {
			w := worldFrom(t, sprint())
			prep(t, w)
			r := w.do("carol", add...)
			want(t, r, 0, "STREAM t repo=o/r base=main")
			if strings.Contains(r.Output, "WARN") {
				t.Fatalf("a warning with no agreement: %s", r.Output)
			}
		})
	}
	t.Run("no forge", func(t *testing.T) {
		w := worldFrom(t, sprint())
		cla(t, w)
		r := w.do("carol", "stream", "add", "t", "--repo", "https://example.test/o/r.git")
		if strings.Contains(r.Output, "WARN") {
			t.Fatalf("a warning with no forge: %d %s", r.Exit, r.Output)
		}
	})
}
