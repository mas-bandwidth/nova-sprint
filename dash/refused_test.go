package dash

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// Unit tests: the real handler on a recorder; no socket, no clock.

// boardOf is /api/sprint's data as the page reads it, from the real handler.
func boardOf(t *testing.T, c *Cache) (tables map[string]map[string]map[string]string, notes []string) {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler(c).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sprint", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sprint: %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Data struct {
			Tables      map[string]json.RawMessage `json:"tables"`
			RosterNotes []string                   `json:"roster_notes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	tables = map[string]map[string]map[string]string{}
	for _, name := range []string{"fleet", "friends"} {
		var rows map[string]map[string]string
		if err := json.Unmarshal(body.Data.Tables[name], &rows); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		tables[name] = rows
	}
	return tables, body.Data.RosterNotes
}

// One bench with 0 cores among good ones (live: it emptied the whole
// board): the good machines render at their widths, the bad one is shown
// refused with its reason, and the page's banner names it.
func TestOneBadMachineAmongGoodOnesRenders(t *testing.T) {
	bench := []string{"bench"}
	ro, err := roster.Load(context.Background(), roster.Fake{
		Ms: []roster.Machine{{Name: "bench-1", Roles: bench, Cores: 4}, {Name: "bench-2", Roles: bench, Cores: 0}, {Name: "bench-3", Roles: bench, Cores: 8}},
		Fs: []roster.Friend{{Name: "friend-1", Width: 2, Tiers: []string{"pro"}, Enabled: true}},
	}, 8)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	rows := map[string]store.Row{}
	for _, n := range []string{"bench-1", "bench-2", "bench-3", "friend-1"} {
		rows[n] = store.Row{Name: n, State: "up", Session: "s-" + n, Role: "worker"}
	}
	c := NewFromFeed(Feed{Snap: store.Snapshot{Epoch: 1, Seq: 1, Rows: rows}, Roster: ro, At: now}, func() time.Time { return now })
	tables, notes := boardOf(t, c)
	fleet := tables["fleet"]
	if fleet["bench-1"]["status"] != "up" || fleet["bench-1"]["width"] != "4" || fleet["bench-3"]["status"] != "up" || fleet["bench-3"]["width"] != "8" {
		t.Fatalf("good machines not rendered: %v", fleet)
	}
	if tables["friends"]["friend-1"]["status"] != "up" {
		t.Fatalf("friend not rendered: %v", tables["friends"])
	}
	bad := fleet["bench-2"]
	if bad["status"] != "refused" || bad["width"] != "0" || bad["reason"] != "machine bench-2 has 0 cores; set its cores in nova-config" {
		t.Fatalf("bad machine: %v", bad)
	}
	if len(notes) != 1 || notes[0] != "nova-config row refused: machine bench-2 has 0 cores; set its cores in nova-config" {
		t.Fatalf("banner: %q", notes)
	}
}

// A roster that could not be read is on the banner, and a hooked row it
// does not name goes by its hook role: the coordinator among the friends,
// never dumped into Fleet; a worker row stays in Fleet.
func TestRosterErrorBannerAndCoordinatorByRole(t *testing.T) {
	now := time.Unix(1000, 0)
	rows := map[string]store.Row{
		"coord-1": {Name: "coord-1", State: "up", Session: "s1", Role: "coordinator"},
		"bench-1": {Name: "bench-1", State: "up", Session: "s2", Role: "worker"},
	}
	c := NewFromFeed(Feed{Snap: store.Snapshot{Epoch: 1, Seq: 1, Rows: rows}, RosterErr: errors.New("config read: connection refused"), At: now}, func() time.Time { return now })
	tables, notes := boardOf(t, c)
	if _, ok := tables["friends"]["coord-1"]; !ok || tables["friends"]["coord-1"]["status"] != "down" {
		t.Fatalf("coordinator not among the friends: %v", tables)
	}
	if _, ok := tables["fleet"]["coord-1"]; ok {
		t.Fatalf("coordinator in Fleet: %v", tables["fleet"])
	}
	if tables["fleet"]["bench-1"]["status"] != "down" {
		t.Fatalf("worker row: %v", tables["fleet"])
	}
	if len(notes) != 1 || notes[0] != "nova-config roster could not be read: config read: connection refused" {
		t.Fatalf("banner: %q", notes)
	}
}

// The banner is on the page, under the title beside the stop banner, and
// app.js fills it from roster_notes: a refusal is never only in JSON.
func TestPageShowsTheRosterBanner(t *testing.T) {
	html, err := page.ReadFile("page/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	stop := strings.Index(string(html), `id="stop-banner"`)
	banner := strings.Index(string(html), `<div id="roster-banner" class="roster-banner" role="status" hidden></div>`)
	if stop < 0 || banner < stop {
		t.Fatalf("roster banner missing or above the stop banner (%d, %d)", stop, banner)
	}
	for _, want := range []string{`$("roster-banner")`, "d.roster_notes", `refused: "critical"`, "m.reason"} {
		if !strings.Contains(string(js), want) {
			t.Errorf("app.js lacks %s", want)
		}
	}
}
