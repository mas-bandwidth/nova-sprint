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

// Friends table ends in the load column (the CPU cell) preserving v2 layout (R64),
// while Resources replaces Fleet with columns machine, status, cpu, memory, tags (R59-R63).
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

	// Friends header preserved 100% ending in ok% then load
	friendsHead := regexp.MustCompile(`id="friends"><div class="row head">(.*?</div>)</div></div></div>`).FindStringSubmatch(h)
	if friendsHead == nil || !strings.HasSuffix(friendsHead[1], `<div class="num">ok%</div><div class="num">load</div>`) {
		t.Errorf("the friends header does not end in ok%% then load: %v", friendsHead)
	}

	// Resources header has exactly machine, status, cpu, memory, tags (no load, no ready, no width, no ok%)
	resHead := regexp.MustCompile(`id="resources"><div class="row head">(.*?</div>)</div></div></div>`).FindStringSubmatch(h)
	if resHead == nil {
		t.Fatalf("no resources table found in index.html")
	}
	wantResHead := `<div>machine</div><div>status</div><div>cpu</div><div>memory</div><div>tags</div>`
	if resHead[1] != wantResHead {
		t.Errorf("resources header = %q, want %q", resHead[1], wantResHead)
	}
	for _, omitted := range []string{"ready", "width", "load", "ok%", "done"} {
		if strings.Contains(resHead[1], omitted) {
			t.Errorf("resources header contains omitted column %q: %s", omitted, resHead[1])
		}
	}

	for _, want := range []string{
		`["ok%", "num"], ["load", "num"]];`,                      // the friends header
		`box._total._c.push(el("div"));`,                         // the Total row's spacer under it
		`r.load = quiet(numCell()); r.node.appendChild(r.load);`, // each row's cell, last
		`var lp = pct(m.load); setText(r.load, lp === null ? "-" : lp.toFixed(1) + "%")`,
	} {
		if !strings.Contains(j, want) {
			t.Errorf("app.js has no %q", want)
		}
	}
	// Friends grid ends in ok% and load
	if !strings.Contains(h, "minmax(6.75rem, 1fr) minmax(6.5rem, 1fr)") {
		t.Errorf("index.html does not contain Friends ok%% and load grid tracks")
	}
	// the compact (phone) form hides friends load as it hides ready, working and done
	if !strings.Contains(h, ".friends.compact .row > :nth-child(8)") {
		t.Errorf("the compact friends table does not hide its load column")
	}
}

// A Fleet row carries the load the page shows, from the heartbeat's CPU peak;
// a Friends row never does, and the page shows "-" (the human, 2026-11-10:
// "load is only for fleet machines").
func TestFleetRowsCarryTheCPUPeakFriendsNone(t *testing.T) {
	t.Parallel()
	ros := roster.Roster{Workers: map[string]roster.Worker{"w": {Name: "w", Resources: map[string]int{"cpu": 4, "memory": 8}}}, Kin: map[string]roster.Kin{"k": {Name: "k", Width: 2}}}
	snap := store.Snapshot{Epoch: 1, Rows: map[string]store.Row{
		"w": {Name: "w", State: "up", Load1Centi: 900, CPUPeakDeci: 412, Cores: 8},
		"k": {Name: "k", State: "up", Load1Centi: 900, CPUPeakDeci: 1000, Cores: 4},
	}}
	d := mapSnapshot(snap, ros, time.Time{}, nil)
	for table, want := range map[string]map[string]string{"fleet": {"w": "41.2%"}, "friends": {"k": ""}} {
		body, err := json.Marshal(d.Tables[table])
		if err != nil {
			t.Fatal(err)
		}
		var cells map[string]map[string]any
		if err := json.Unmarshal(body, &cells); err != nil {
			t.Fatal(err)
		}
		for name, load := range want {
			gotLoad := ""
			if v, ok := cells[name]["load"]; ok && v != nil {
				gotLoad = v.(string)
			}
			if gotLoad != load {
				t.Errorf("%s %s load = %q, want %q", table, name, gotLoad, load)
			}
		}
	}
}
