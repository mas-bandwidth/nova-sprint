package dash

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestPageJudgesPulseByServerReportedAge checks that app.js judges the bar's health from the server's
// dataAgeSeconds plus the client's monotonic elapsed time, never by comparing the viewer's clock with the
// server's time, so viewer clock skew does not change it.
func TestPageJudgesPulseByServerReportedAge(t *testing.T) {
	t.Parallel()
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	for _, bad := range []string{"Date.now() - Date.parse(j.fetchedAt)", "Date.now() - shownAt"} {
		if strings.Contains(src, bad) {
			t.Errorf("app.js compares the viewer clock with server time: %q", bad)
		}
	}
	if !strings.Contains(src, "dataAgeSeconds") {
		t.Error("app.js never reads the server's dataAgeSeconds")
	}
}

// TestInflightTooltipOmitsBlockedWhenZero checks that the In flight tooltip appends ", N blocked" only
// when a card is blocked.
func TestInflightTooltipOmitsBlockedWhenZero(t *testing.T) {
	t.Parallel()
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), `" merging, " + (sum.blocked || 0) + " blocked"`) {
		t.Error(`app.js renderInflight: setTitle always appends ", " + (sum.blocked || 0) + " blocked"`)
	}
}

// TestEtaFallbackSublineIsBlank checks that the ETA tile's subline is blank when there is no ETA, not
// stopped and not done.
func TestEtaFallbackSublineIsBlank(t *testing.T) {
	t.Parallel()
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	const withNote = `else { setText($("eta"), "-"); setText($("eta-at"), "not in the summary"); }`
	const blank = `else { setText($("eta"), "-"); setText($("eta-at"), ""); }`
	if strings.Contains(string(js), withNote) || !strings.Contains(string(js), blank) {
		t.Errorf("ETA fallback subline must be blank, not %q", "not in the summary")
	}
}

// TestPageIgnoresObjectPrototypeNames checks that page/app.js does not match row or stream names against
// Object.prototype members: a bud named "toString-1" is not "toString (1)", and a stream named "constructor"
// is not archived.
func TestPageIgnoresObjectPrototypeNames(t *testing.T) {
	t.Parallel()
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(js)
	if strings.Contains(src, "table && table[name.slice(0, i)]") {
		t.Errorf("page/app.js budLabel indexes `table[name.slice(0, i)]` without hasOwnProperty/Object.hasOwn, matching Object.prototype properties (e.g. 'toString-1' -> 'toString (1)')")
	}
	if strings.Contains(src, "function archivedSet(d) { var a = {};") {
		t.Errorf("page/app.js archivedSet uses plain `{}` instead of `Object.create(null)`, so `arch[name]` treats Object.prototype names as archived")
	}
}

// TestServedPageCarriesNoConversation checks that nothing the dashboard serves (the page, the stream page
// and their scripts and styles) names a person or quotes a dated remark: view-source shows it to anyone.
func TestServedPageCarriesNoConversation(t *testing.T) {
	t.Parallel()
	bad := regexp.MustCompile(`(?i)gl` + `enn|the owner|/\*[^*]*\d{4}-\d\d-\d\d[^*]*\*/|<!--[^>]*\d{4}-\d\d-\d\d[^>]*-->`)
	err := fs.WalkDir(page, "page", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch filepath.Ext(path) {
		case ".html", ".css", ".js":
		default:
			return nil
		}
		if filepath.Base(path) == "pele.js" {
			return nil
		}
		b, err := page.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range bad.FindAllString(string(b), -1) {
			if len(m) > 80 {
				m = m[:80]
			}
			t.Errorf("%s carries conversation: %q", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
