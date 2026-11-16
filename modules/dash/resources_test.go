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

// R64 + Glenn's explicit 2026-11-15 ruling:
// Friends section is 100% unchanged (same columns, working cells against friend width, same done and ok%).
func TestFriendsTablePreserved100Percent(t *testing.T) {
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

	// Check friends panel title and subtitle
	if !strings.Contains(h, `<h2>Friends</h2><span class="muted" id="friends-sub"></span>`) {
		t.Errorf("index.html missing Friends panel head")
	}

	// Check exact friends table columns
	friendsHead := regexp.MustCompile(`id="friends"><div class="row head">(.*?</div>)</div></div></div>`).FindStringSubmatch(h)
	if friendsHead == nil {
		t.Fatalf("no friends table in index.html")
	}
	wantFriendsColumns := `<div>friend</div><div>status</div><div class="num">ready</div><div>working</div><div class="frac"><span class="alt">working</span></div><div class="num">done</div><div class="num">ok%</div><div class="num">load</div>`
	if friendsHead[1] != wantFriendsColumns {
		t.Errorf("friends header = %q, want %q", friendsHead[1], wantFriendsColumns)
	}

	// Verify JS still renders friends with fleetLike preserving VU tracks and widths
	for _, wantJS := range []string{
		`var r = fleetLike(box, table, "friend", sharedScale(d), true, sharedDigits(d));`,
		`setText($("friends-sub"), r.t.up + " up · " + r.t.held + " held · " + r.t.down + " down"`,
		`setTrack(r.track, working, width, scale, m);`,
	} {
		if !strings.Contains(j, wantJS) {
			t.Errorf("app.js missing %q for friends table", wantJS)
		}
	}
}

// R59-R63:
// Fleet section becomes Resources (replacing Fleet).
// Columns on Resources table ONLY: machine, status, cpu, memory, tags.
// Removed from Resources table: width, working cell boxes, working/width, ready, load, done, ok%.
func TestResourcesTableColumnsAndOmissions(t *testing.T) {
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

	// Panel title must be Resources
	if !strings.Contains(h, `<h2>Resources</h2>`) {
		t.Errorf("index.html missing <h2>Resources</h2>")
	}
	if strings.Contains(h, `<h2>Fleet</h2>`) {
		t.Errorf("index.html still contains <h2>Fleet</h2>, should be replaced by Resources")
	}

	// Resources table columns
	resHead := regexp.MustCompile(`id="resources"><div class="row head">(.*?</div>)</div></div></div>`).FindStringSubmatch(h)
	if resHead == nil {
		t.Fatalf("no resources table in index.html")
	}
	wantColumns := `<div>machine</div><div>status</div><div>cpu</div><div>memory</div><div>tags</div>`
	if resHead[1] != wantColumns {
		t.Errorf("resources header = %q, want %q", resHead[1], wantColumns)
	}

	// Verify omitted columns are not in Resources header
	for _, omitted := range []string{"ready", "width", "working", "done", "ok%", "load"} {
		if strings.Contains(resHead[1], omitted) {
			t.Errorf("resources header contains omitted column %q", omitted)
		}
	}

	// Verify JS renderResources builds rows with machine, status, cpu, memory, tags
	for _, wantJS := range []string{
		`var cols = ["machine", "status", "cpu", "memory", "tags"];`,
		`r.cpu = makeResCell("");`,
		`r.memory = makeResCell("GB");`,
		`r.tags = el("div", "tags-cell");`,
		`[r.name, r.pill, r.cpu.cell, r.memory.cell, r.tags]`,
		`updateResCell(r.cpu, m.cpu);`,
		`updateResCell(r.memory, m.memory);`,
		`updateTagsCell(r.tags, m.tags);`,
	} {
		if !strings.Contains(j, wantJS) {
			t.Errorf("app.js missing %q for resources table", wantJS)
		}
	}
}

// R60, R61, Q11:
// ci: distinct cool steel blue / cyan (#38bdf8).
// blocker, critical, fix, read, work: exact existing board colors.
// other: neutral slate.
func TestResourcesColors(t *testing.T) {
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

	// CI color #38bdf8 in CSS and JS
	if !strings.Contains(h, "#38bdf8") {
		t.Errorf("index.html does not define steel blue/cyan #38bdf8")
	}
	if !strings.Contains(j, `"#38bdf8"`) {
		t.Errorf("app.js does not use #38bdf8 for CI")
	}

	// Board colors for card types
	for _, cls := range []string{
		".res-seg.ci", ".res-seg.blocker", ".res-seg.critical",
		".res-seg.fix", ".res-seg.read", ".res-seg.work", ".res-seg.other",
	} {
		if !strings.Contains(h, cls) {
			t.Errorf("index.html missing CSS rule for %s", cls)
		}
	}

	// Check JS mapping for board colors
	for _, pattern := range []string{
		`"ci": "#38bdf8"`,
		`"blocker": "var(--p-blocker, #ff1a1a)"`,
		`"critical": "var(--p-critical, #9b1c1c)"`,
		`"fix": "var(--p-fix, #8b5cf6)"`,
		`"read": "var(--p-reader, #fb8321)"`,
		`"work": "var(--s-working, #3d9bff)"`,
	} {
		if !strings.Contains(j, pattern) {
			t.Errorf("app.js missing color mapping %q", pattern)
		}
	}
}

// R62:
// Paused work rendered visually behind / displaced by CI.
// Handled gracefully without exceeding total.
func TestResourcesPausedWorkBehindCI(t *testing.T) {
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

	// Check underlay and paused styles
	if !strings.Contains(h, ".res-underlay") {
		t.Errorf("index.html missing .res-underlay")
	}
	if !strings.Contains(h, ".res-paused") {
		t.Errorf("index.html missing .res-paused")
	}
	if !strings.Contains(h, ".res-seg.ci.has-paused") {
		t.Errorf("index.html missing .res-seg.ci.has-paused style")
	}

	// Check JS paused underlay logic
	for _, want := range []string{
		`var pausedTotal = 0;`,
		`var pDiv = el("div", "res-paused");`,
		`seg.classList.add("has-paused")`,
	} {
		if !strings.Contains(j, want) {
			t.Errorf("app.js missing paused underlay logic %q", want)
		}
	}
}

// Graceful handling of unmeasured benches (total == 0).
func TestResourcesUnmeasuredHandling(t *testing.T) {
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

	if !strings.Contains(h, ".res-bar.unmeasured") {
		t.Errorf("index.html missing .res-bar.unmeasured style")
	}
	if !strings.Contains(j, `rc.bar.classList.add("unmeasured")`) {
		t.Errorf("app.js missing unmeasured bar class handling")
	}
	if !strings.Contains(j, `setText(rc.label, "unmeasured")`) {
		t.Errorf("app.js missing unmeasured label handling")
	}
}

func sampleResourcesSnapshot() (store.Snapshot, roster.Roster) {
	ros := roster.Roster{
		Workers: map[string]roster.Worker{
			"bench-1": {
				Name:      "bench-1",
				Tags:      []string{"linux", "amd64", "gpu"},
				Resources: map[string]int{"cpu": 16, "memory": 64},
			},
			"bench-2": {
				Name:      "bench-2",
				Tags:      []string{"linux", "arm64", "metal"},
				Resources: map[string]int{"cpu": 8, "memory": 32},
			},
			"bench-unmeasured": {Name: "bench-unmeasured"},
		},
		Kin: map[string]roster.Kin{
			"friend-1": {Name: "friend-1", Width: 4},
		},
	}
	snap := store.Snapshot{
		Epoch: 1,
		Rows: map[string]store.Row{
			"bench-1": {
				Name:  "bench-1",
				State: "up",
				Resources: store.RowResources{
					Total: map[string]int{"cpu": 16, "memory": 64},
					HeldByLabel: map[string]map[string]int{
						"ci":   {"cpu": 4, "memory": 16},
						"work": {"cpu": 8, "memory": 24},
					},
					PausedByLabel: map[string]map[string]int{
						"work": {"cpu": 4, "memory": 8},
					},
				},
			},
			"bench-2": {
				Name:  "bench-2",
				State: "up",
				Resources: store.RowResources{
					Total: map[string]int{"cpu": 8, "memory": 32},
					HeldByLabel: map[string]map[string]int{
						"blocker": {"cpu": 2, "memory": 8},
						"fix":     {"cpu": 1, "memory": 4},
						"read":    {"cpu": 1, "memory": 2},
					},
				},
			},
			"bench-unmeasured": {Name: "bench-unmeasured", State: "down"},
			"friend-1":         {Name: "friend-1", State: "up"},
		},
	}
	return snap, ros
}

// Verify mapSnapshot correctly constructs and maps Resources and Fleet data.
func TestMapSnapshotResourcesBenches(t *testing.T) {
	t.Parallel()
	snap, ros := sampleResourcesSnapshot()
	d := mapSnapshot(snap, ros, time.Time{}, nil)

	resTable, ok := d.Tables["resources"].(map[string]*fleetRow)
	if !ok || resTable == nil {
		t.Fatalf("d.Tables[\"resources\"] missing or wrong type")
	}
	b1 := resTable["bench-1"]
	if b1 == nil || b1.Status != "up" {
		t.Fatalf("bench-1 invalid: %+v", b1)
	}
	if len(b1.Tags) != 3 || b1.Tags[0] != "linux" || b1.Tags[1] != "amd64" || b1.Tags[2] != "gpu" {
		t.Errorf("bench-1 tags = %v", b1.Tags)
	}
	if b1.CPU.Total != 16 || b1.CPU.Held != 12 || b1.CPU.ByLabel["ci"] != 4 || b1.CPU.PausedByLabel["work"] != 4 {
		t.Errorf("bench-1 cpu mismatch: %+v", b1.CPU)
	}
	if b1.Memory.Total != 64 || b1.Memory.Held != 40 || b1.Memory.ByLabel["ci"] != 16 || b1.Memory.PausedByLabel["work"] != 8 {
		t.Errorf("bench-1 mem mismatch: %+v", b1.Memory)
	}

	b2 := resTable["bench-2"]
	if b2 == nil || b2.CPU.Total != 8 || b2.CPU.Held != 4 || b2.CPU.ByLabel["blocker"] != 2 {
		t.Errorf("bench-2 cpu mismatch: %+v", b2.CPU)
	}
	bUnm := resTable["bench-unmeasured"]
	if bUnm == nil || bUnm.CPU.Total != 0 || bUnm.CPU.Held != 0 {
		t.Errorf("bench-unmeasured mismatch: %+v", bUnm)
	}
}

func TestMapSnapshotResourcesJSON(t *testing.T) {
	t.Parallel()
	snap, ros := sampleResourcesSnapshot()
	d := mapSnapshot(snap, ros, time.Time{}, nil)

	resTable := d.Tables["resources"].(map[string]*fleetRow)
	body, err := json.Marshal(resTable["bench-1"])
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err = json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["tags"] == nil || raw["cpu"] == nil || raw["memory"] == nil {
		t.Errorf("json missing expected fields: %s", body)
	}

	friendBody, err := json.Marshal(d.Tables["friends"].(map[string]*fleetRow)["friend-1"])
	if err != nil {
		t.Fatal(err)
	}
	var friendRaw map[string]any
	if err = json.Unmarshal(friendBody, &friendRaw); err != nil {
		t.Fatal(err)
	}
	if friendRaw["cpu"] != nil || friendRaw["memory"] != nil {
		t.Errorf("friends row should not serialize cpu/memory: %s", friendBody)
	}
}
