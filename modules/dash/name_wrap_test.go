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

// Fleet and Friends row names show in full: rows named alike differ only at
// the end, so an ellipsized name cannot be told from its siblings (Glenn
// 2026-11-14: "aws-schema-bench-1" showed as "aws-schema-benc"). The board
// carries each long name whole, and the page's rule for those names wraps
// them and never ellipsizes, with no later rule clipping them again.
func TestFleetAndFriendsLongNamesShowInFull(t *testing.T) {
	t.Parallel()
	names := []string{"aws-schema-bench-1", "aws-schema-bench-2", "aws-schema-bench-3", "aws-schema-bench-4"}
	kin := "a-friend-with-a-very-long-row-name"
	ros := roster.Roster{Workers: map[string]roster.Worker{}, Kin: map[string]roster.Kin{kin: {Name: kin, Width: 2}}}
	rows := map[string]store.Row{kin: {Name: kin, State: "up"}}
	for _, n := range names {
		ros.Workers[n] = roster.Worker{Name: n, Resources: map[string]int{"cpu": 4, "memory": 8}}
		rows[n] = store.Row{Name: n, State: "up"}
	}
	d := mapSnapshot(store.Snapshot{Epoch: 1, Rows: rows}, ros, time.Time{}, nil)
	for table, want := range map[string][]string{"fleet": names, "friends": {kin}} {
		body, err := json.Marshal(d.Tables[table])
		if err != nil {
			t.Fatal(err)
		}
		var cells map[string]json.RawMessage
		if err := json.Unmarshal(body, &cells); err != nil {
			t.Fatal(err)
		}
		for _, n := range want {
			if _, ok := cells[n]; !ok {
				t.Errorf("the %s table has no row named in full %q", table, n)
			}
		}
	}

	html, err := page.ReadFile("page/index.html")
	if err != nil {
		t.Fatal(err)
	}
	h := string(html)
	rule := regexp.MustCompile(`\.fleet \.name, \.friends \.name \{([^}]*)\}`).FindStringSubmatch(h)
	if rule == nil {
		t.Fatal("index.html has no rule for the Fleet and Friends names")
	}
	for _, want := range []string{"white-space: normal", "overflow: visible", "text-overflow: clip", "overflow-wrap: anywhere"} {
		if !strings.Contains(rule[1], want) {
			t.Errorf("the Fleet and Friends name rule lacks %q: %q", want, rule[1])
		}
	}
	// nothing after that rule clips a Fleet or Friends name again
	after := h[strings.Index(h, rule[0])+len(rule[0]):]
	for _, sel := range regexp.MustCompile(`[^{}]*\{[^}]*(?:ellipsis|nowrap|overflow: hidden)[^}]*\}`).FindAllString(after, -1) {
		head := sel[:strings.Index(sel, "{")]
		if strings.Contains(head, ".name") && (strings.Contains(head, "fleet") || strings.Contains(head, "friends")) {
			t.Errorf("a later rule clips the Fleet or Friends names: %q", strings.TrimSpace(sel))
		}
	}
}
