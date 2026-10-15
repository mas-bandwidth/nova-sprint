package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprintdash"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
)

// The owner, 2026-10-11: "The dashboard has to reflect the truth." "Anytime when it doesn't is
// a bug." and "Done is every card completed." Friend stella's row read done 0 and ok% 0.0% while
// the log recorded her finishes ok and their reads ok: the readers' verdict (TickVerdicts)
// counted the readers table's reads alone, and her cards were read on read cards of the fleet
// table, retired off their rows with their verdicts, so her work waited in finished however
// the accept took it (Stella's finding). A row's done is now every card it completed, of every
// kind (sprint.DoneKinds): work and fix cards the readers' verdict moved to ok, and read cards
// completed with a verdict.

// inboxJobOf is the job of her inbox whose name begins with the card's id.
func inboxJobOf(t *testing.T, root, friend, id string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, friend+"-working", "inbox"))
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), id) {
			return e.Name()
		}
	}
	require.Failf(t, "no job", "%s's inbox holds no job of %s: %v", friend, id, entries)
	return ""
}

// rowCells is a row's cells of the named table as where --json carries them.
func rowCells(ta *testApp, table, row string) map[string]any {
	var w whereView
	ta.json("where", &w)
	return withoutRowCards(w.Tables[table][row])
}

// A friend completes six cards by every path that records a completion: three work cards
// (friend sync collecting her report, and her own finish verb as friend.stella, twice), one fix card (the rework of a card a reader read
// broken), each read ok on a read card of the fleet table by the reader friend fred, and two
// read cards of fred's work she completes herself. Her row shows done 6 (3 work + 1 fix + 2
// reads) in where --json and in the dashboard's pull of her row; ok% is her work's alone, the
// readers' verdicts on her ok and failed cells, which her reads never move. Fred, a friend who both builds and reads,
// shows his 2 work cards and his 6 reads; member m1 shows the work card it completed. A stats
// reset counts done from its mark: 0 for every row. Red before the fix: stella's done 0.
func TestADoneIsEveryCardTheRowCompleted(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	home, prev := t.TempDir(), ta.a.getenv
	ta.a.getenv = func(k string) string {
		if k == "HOME" {
			return home
		}
		return prev(k)
	}
	cfg := config.NewMem()
	ta.a.friends = func(ctx context.Context, _ string) ([]config.Row, error) {
		return cfg.List(ctx, config.KindFriend)
	}
	for _, f := range []config.Row{
		{Name: "stella", Fields: map[string]string{"tiers": "flash", "width": "8", "roles": "builder,reader"}},
		{Name: "fred", Fields: map[string]string{"tiers": "flash", "mode": "one-shot", "width": "8", "roles": "builder,reader"}},
	} {
		_, err := cfg.Insert(context.Background(), config.KindFriend, f, "t")
		require.NoError(t, err)
	}
	ta.a.tip = tipIs(t, landHead)
	ta.ok("init --readers reader-a --members m1:8")
	ta.ok("set --read-cards on")
	root := t.TempDir()
	ta.ok("friend sync --root " + root) // roster only: both down, so the member's card is the machine's

	dir := t.TempDir()
	brief := func(id, who string) {
		lead := id + ": a card (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\n"
		if who != "" {
			lead += "WHO: only friend " + who + "\n"
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".md"), []byte(passingBrief(lead+"\nHOW THIS CARD IS JUDGED\nthe flag is reachable\n")), 0o644))
	}
	for _, id := range []string{"s1-1", "s1-2", "s1-3", "s1-6"} {
		brief(id, "stella")
	}
	brief("s1-4", "fred")
	brief("s1-5", "fred")
	brief("s1-7", "")
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")

	land := "Verdict: LAND\nHead: " + landHead + "\n\nthe flag is reachable; go test ./... green\n"
	hold := "Verdict: HOLD\n\ninternal/x.go:1: the flag is not reachable from the verb\n"
	// answer every read card dealt to the friend: LAND, or HOLD for the primaries in broken
	answerReads := func(friend string, ids []string, broken map[string]bool) {
		t.Helper()
		ta.ok("tick")
		ta.ok("friend sync --root " + root)
		var card cardView
		answered := 0
		for _, id := range ids {
			ta.json("card "+id, &card)
			for _, rc := range card.Reads {
				if rc.Row != sprint.FriendRow(friend) || !rc.Placed() {
					continue
				}
				report := land
				if broken[id] {
					report = hold
				}
				outboxReport(t, root, friend, inboxJobOf(t, root, friend, rc.ID), report)
				answered++
			}
		}
		require.Equal(t, len(ids), answered, "%s holds one read of each of %v", friend, ids)
		ta.ok("friend sync --root " + root)
	}

	// member m1 works s1-7 while no friend is up
	ta.ok("take --as m1 --limit 1")
	ta.ok("finish --as m1 s1-7.w1@1 --report done --head " + landHead)

	// fred comes up: he reads m1's card (the only reader up) and works his two
	ta.beatUp("fred")
	answerReads("fred", []string{"s1-7"}, nil)
	ta.startFriend("fred", 2)
	for _, id := range []string{"s1-4.w1", "s1-5.w1"} {
		outboxReport(t, root, "fred", inboxJobOf(t, root, "fred", id), land)
	}
	ta.ok("friend sync --root " + root)

	// stella comes up: she reads fred's two (fred never reads his own) and works her four
	ta.beatUp("stella")
	answerReads("stella", []string{"s1-4", "s1-5"}, nil)
	ta.startFriend("stella", 4)
	// s1-1 and s1-6: friend sync collects her reports
	outboxReport(t, root, "stella", inboxJobOf(t, root, "stella", "s1-1.w1"), land)
	outboxReport(t, root, "stella", inboxJobOf(t, root, "stella", "s1-6.w1"), land)
	out := ta.ok("friend sync --root " + root)
	require.Contains(t, out, "FRIEND-CARD FINISHED friend=stella card=s1-1.w1 result=ok", out)
	// s1-2: her own finish verb, as friend.stella
	ta.ok("finish --as " + sprint.FriendRow("stella") + " s1-2.w1@1 --report done --head " + landHead)
	// s1-3: her own finish verb again, the one way a friend lane finishes (the QUEUE.json
	// reconcile that once collected it is retired: the server's records alone decide)
	ta.ok("finish --as " + sprint.FriendRow("stella") + " s1-3.w1@1 --report done --head " + landHead)

	// fred reads her four: ok, but s1-6 broken, which is reworked with its finding: its next
	// attempt, the fix card, is hers again
	answerReads("fred", []string{"s1-1", "s1-2", "s1-3", "s1-6"}, map[string]bool{"s1-6": true})
	ta.ok("rework s1-6")
	ta.ok("tick")
	ta.ok("friend sync --root " + root)
	ta.startFriend("stella", 1)
	outboxReport(t, root, "stella", inboxJobOf(t, root, "stella", "s1-6.w2"), land)
	ta.ok("friend sync --root " + root)
	answerReads("fred", []string{"s1-6"}, nil)
	ta.ok("tick")
	ta.ok("tick")

	st, err := ta.a.store(common{redis: "mem:0", actor: "tester"})
	require.NoError(t, err)
	done, err := st.DoneSince(context.Background(), time.Time{})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{sprint.DoneKindWork: 3, sprint.DoneKindFix: 1, sprint.DoneKindRead: 2}, done[sprint.FriendRow("stella")], "stella's completions by kind")
	assert.Equal(t, map[string]int{sprint.DoneKindWork: 2, sprint.DoneKindRead: 6}, done[sprint.FriendRow("fred")], "fred's: s1-7, s1-1..3 and both reads of s1-6")
	assert.Equal(t, map[string]int{sprint.DoneKindWork: 1}, done["m1"], "the member's work")

	stella := rowCells(ta, sprint.Friends, "stella")
	assert.Equal(t, "6", stella["done"], "3 work + 1 fix + 2 reads: %v", stella)
	assert.Equal(t, "100.0%", stella["okpct"], "ok%% is her work's alone (4 ok; the rework took the broken attempt off her row): %v", stella)
	fred := rowCells(ta, sprint.Friends, "fred")
	assert.Equal(t, "8", fred["done"], "2 work + 6 reads: %v", fred)
	assert.Equal(t, "100.0%", fred["okpct"], "his reads never move his ok%%: %v", fred)
	m1 := rowCells(ta, sprint.Fleet, "m1")
	assert.Equal(t, "1", m1["done"], "the member's work: %v", m1)
	assert.Equal(t, "100.0%", m1["okpct"], "%v", m1)

	// the dashboard's pull of her row says the same
	srv := &sprintdash.Server{Read: func() ([]byte, error) { return ta.a.whereJSON("", false) }, Now: ta.a.now, Every: time.Second}
	w := httptest.NewRecorder()
	srv.Pull().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/friend/stella", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var view sprintdash.PullView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view), w.Body.String())
	assert.Equal(t, "6", view.Row.Done, "the dashboard's friend row: %+v", view.Row)
	assert.Equal(t, "100.0%", view.Row.OKPct)
	w = httptest.NewRecorder()
	srv.Pull().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/machine/m1", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view), w.Body.String())
	assert.Equal(t, "1", view.Row.Done, "the dashboard's member row: %+v", view.Row)

	// a stats reset counts done from its mark
	ta.ok("stats reset --reason 'count from now'")
	for _, r := range []struct{ table, row string }{{sprint.Friends, "stella"}, {sprint.Friends, "fred"}, {sprint.Fleet, "m1"}} {
		assert.Equal(t, "0", rowCells(ta, r.table, r.row)["done"], "%s after the reset", r.row)
	}
}
