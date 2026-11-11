package dash

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// The Fleet and Friends tables end in the load column (the CPU cell): the
// highest CPU busy percent of the machine's last 10 seconds. Both tables are
// drawn by one renderer, fleetLike, from the same header, row and Total-row
// code and the same grid, so these checks on the page cover both.
func TestFleetAndFriendsTablesEndInTheLoadColumn(t *testing.T) {
	t.Parallel()
	html, err := page.ReadFile("page/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js, err := page.ReadFile("page/app.js")
	if err != nil {
		t.Fatal(err)
	}
	h, j := string(html), string(js)
	for _, id := range []string{"fleet", "friends"} {
		head := regexp.MustCompile(`id="` + id + `"><div class="row head">(.*?)</div></div></div>`).FindStringSubmatch(h)
		if head == nil || !strings.HasSuffix(head[1], `<div class="num">ok%</div><div class="num">load`) {
			t.Errorf("the %s header does not end in ok%% then load: %v", id, head)
		}
	}
	for _, want := range []string{
		`["ok%", "num"], ["load", "num"]];`,                      // the header
		`box._total._c.push(el("div"));`,                         // the Total row's spacer under it
		`r.load = quiet(numCell()); r.node.appendChild(r.load);`, // each row's cell, last
		`var lp = pct(m.load); setText(r.load, lp === null ? "-" : lp.toFixed(1) + "%")`,
	} {
		if !strings.Contains(j, want) {
			t.Errorf("app.js has no %q", want)
		}
	}
	// eight tracks: the name, status, ready, bars, fraction, done and ok% columns, then load
	grid := regexp.MustCompile(`\.fleet \.row, \.friends \.row \{ grid-template-columns: ([^;]*);`).FindStringSubmatch(h)
	if grid == nil || !strings.HasSuffix(grid[1], "minmax(6.75rem, 1fr) minmax(6.5rem, 1fr)") {
		t.Errorf("the Fleet and Friends grid does not end in the ok%% and load tracks: %v", grid)
	}
	// the compact (phone) form hides load as it hides ready, working and done
	for _, side := range []string{".fleet", ".friends"} {
		if !strings.Contains(h, side+".compact .row > :nth-child(8)") {
			t.Errorf("the compact %s table does not hide its load column", side)
		}
	}
}

// Both tables' rows carry the load the page shows, from the heartbeat's CPU peak.
func TestFleetAndFriendsRowsCarryTheCPUPeak(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{Workers: map[string]roster.Worker{"w": {Name: "w", Width: 4}}, Kin: map[string]roster.Kin{"k": {Name: "k", Width: 2}}}
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{
		"w": {Name: "w", State: "up", Load1Centi: 900, CPUPeakDeci: 412, Cores: 8},
		"k": {Name: "k", State: "up", Load1Centi: 900, CPUPeakDeci: 1000, Cores: 4},
	}}
	d := mapSnapshot(snap, ros, time.Time{}, nil)
	for table, want := range map[string]map[string]string{"fleet": {"w": "41.2%"}, "friends": {"k": "100.0%"}} {
		body, err := json.Marshal(d.Tables[table])
		if err != nil {
			t.Fatal(err)
		}
		var cells map[string]map[string]string
		if err := json.Unmarshal(body, &cells); err != nil {
			t.Fatal(err)
		}
		for name, load := range want {
			if cells[name]["load"] != load {
				t.Errorf("%s %s load = %q, want %q", table, name, cells[name]["load"], load)
			}
		}
	}
}
