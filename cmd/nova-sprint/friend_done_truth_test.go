package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
// a bug." Friend stella's row read done 0 and ok% 0.0% while the log recorded her finishes ok
// and their reads ok: the readers' verdict (TickVerdicts) counted the readers table's reads
// alone, and her cards were read on read cards of the fleet table, retired off their rows with
// their verdicts, so her work waited in finished however the accept took it (Stella's finding).

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

// friendRowCells is a friend's friends-table cells as where --json carries them.
func friendRowCells(ta *testApp, friend string) map[string]any {
	var w whereView
	ta.json("where", &w)
	return withoutRowCards(w.Tables[sprint.Friends][friend])
}

// okFinishLine is the log's line of a worker's own ok finish (sprint render_log: the move
// into finished, with the head her report named): the card it names, before the line (a
// card the line does not name by its branch) or in the branch (sprint/<card>.g<gen>.e<epoch>).
// The readers' verdict's move to ok renders as a finish line with no head, never counted.
var okFinishLine = regexp.MustCompile(`(?:(\S+): )?friend\.stella finished attempt \d+: ok, head \S+(?: on sprint/(\S+?)\.g\d+)?`)

// A friend finishes three cards ok by the three paths that record a friend's finish: friend
// sync collecting her report, her own finish verb (--as friend.stella), and friend reconcile
// collecting a report her queue says done. Each is read ok on a read card of the fleet table by
// a reader friend, retired with its verdict. Her friends row then shows done 3 and ok% 100.0%,
// in where --json and in the dashboard's pull of her row, and done is the count of her ok
// finishes the log records, each card once. Red before the fix: done 0, ok% 0.0%.
func TestAFriendsDoneIsHerOkFinishesWhicheverPathRecordedThem(t *testing.T) {
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
		{Name: "stella", Fields: map[string]string{"slots": "4", "tiers": "flash", "width": "8", "roles": "builder"}},
		{Name: "fred", Fields: map[string]string{"slots": "4", "tiers": "flash", "mode": "one-shot", "width": "8", "roles": "reader"}},
	} {
		_, err := cfg.Insert(context.Background(), config.KindFriend, f, "t")
		require.NoError(t, err)
	}
	ta.a.tip = tipIs(t, landHead)
	ta.ok("init --readers reader-a --members m1:8")
	ta.ok("set --read-cards on")
	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("stella")
	ta.beatUp("fred")

	dir := t.TempDir()
	for _, id := range []string{"s1-1", "s1-2", "s1-3"} {
		brief := passingBrief(id + ": a friend's card (s1) tier: flash\nREPO: mas-bandwidth/nova-tools\nWHO: only friend stella\n\nHOW THIS CARD IS JUDGED\nthe flag is reachable\n")
		require.NoError(t, os.WriteFile(filepath.Join(dir, id+".md"), []byte(brief), 0o644))
	}
	ta.ok("add --stream s1 --brief-dir " + dir)
	ta.ok("start")
	ta.ok("tick")
	ta.ok("friend sync --root " + root)
	ta.startFriend("stella", 3)
	require.Equal(t, "3", friendRowCells(ta, "stella")["working"], "her three cards dealt and started")

	report := "Verdict: LAND\nHead: " + landHead + "\n\nthe flag is reachable; go test ./... green\n"
	// s1-1: friend sync collects her report
	outboxReport(t, root, "stella", inboxJobOf(t, root, "stella", "s1-1.w1"), report)
	out := ta.ok("friend sync --root " + root)
	require.Contains(t, out, "FRIEND-CARD FINISHED friend=stella card=s1-1.w1 result=ok", out)
	// s1-2: her own finish verb, as friend.stella
	ta.ok("finish --as " + sprint.FriendRow("stella") + " s1-2.w1@1 --report done --head " + landHead)
	// s1-3: friend reconcile collects the report her queue says done
	job3 := inboxJobOf(t, root, "stella", "s1-3.w1")
	outboxReport(t, root, "stella", job3, report)
	// her session marks the card done in the queue friend sync keeps (friendQueue)
	queuePath := filepath.Join(root, "stella-working", "inbox", "QUEUE.json")
	text, err := os.ReadFile(queuePath)
	require.NoError(t, err)
	var q friendQueue
	require.NoError(t, json.Unmarshal(text, &q))
	marked := false
	for i := range q.Tasks {
		if q.Tasks[i].ID == "s1-3.w1" {
			q.Tasks[i].State, marked = "done", true
		}
	}
	require.True(t, marked, "her queue holds s1-3.w1: %s", text)
	text, err = json.Marshal(q)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(queuePath, text, 0o644))
	out = ta.ok("friend reconcile stella --root " + root)
	require.Contains(t, out, "collected=1", out)

	var card cardView
	for _, id := range []string{"s1-1", "s1-2", "s1-3"} {
		ta.json("card "+id, &card)
		require.Len(t, card.Work, 1, id)
		require.Equal(t, sprint.Finished, card.Work[0].Col, "%s: her finish put her work card in finished", id)
	}
	// her word alone counts nothing (the readers' ok%): done waits for the reads
	assert.Equal(t, "0", friendRowCells(ta, "stella")["done"])

	// the reads: a read card each, dealt to fred, read ok by his LAND, retired with the verdict
	ta.ok("tick")
	ta.ok("friend sync --root " + root)
	for _, id := range []string{"s1-1", "s1-2", "s1-3"} {
		ta.json("card "+id, &card)
		require.Len(t, card.Reads, 1, "%s: one read card", id)
		rc := card.Reads[0]
		require.Equal(t, sprint.FriendRow("fred"), rc.Row, "%s: the reader friend reads it", id)
		outboxReport(t, root, "fred", inboxJobOf(t, root, "fred", rc.ID), report)
	}
	ta.ok("friend sync --root " + root)
	for _, id := range []string{"s1-1", "s1-2", "s1-3"} {
		ta.json("card "+id, &card)
		require.Len(t, card.Reads, 1)
		require.Equal(t, "ok", card.Reads[0].F("verdict"), "%s: read ok", id)
		require.Empty(t, card.Reads[0].Row, "%s: the read card is retired off its row", id)
	}
	ta.ok("tick")
	ta.ok("tick")

	stella := friendRowCells(ta, "stella")
	assert.Equal(t, "3", stella["done"], "done is her three ok finishes: %v", stella)
	assert.Equal(t, "100.0%", stella["okpct"], "ok%% is ok over ok+failed: %v", stella)

	// the dashboard's pull of her row says the same
	srv := &sprintdash.Server{Read: func() ([]byte, error) { return ta.a.whereJSON("", false) }, Now: ta.a.now, Every: time.Second}
	w := httptest.NewRecorder()
	srv.Pull().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/friend/stella", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var view sprintdash.PullView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view), w.Body.String())
	assert.Equal(t, "3", view.Row.Done, "the dashboard's friend row: %+v", view.Row)
	assert.Equal(t, "100.0%", view.Row.OKPct)

	// and done is the count of her ok finishes in the log, each card once (the verdict's
	// move to ok renders as a finish line too: the cards, not the lines, are counted)
	finished := map[string]bool{}
	logText := ta.ok("log")
	for _, l := range strings.Split(logText, "\n") {
		if m := okFinishLine.FindStringSubmatch(l); m != nil {
			finished[m[1]+m[2]] = true
		}
	}
	assert.Len(t, finished, 3, "her ok finishes in the log: %v\n%s", finished, logText)
	assert.Equal(t, view.Row.Done, strconv.Itoa(len(finished)))
}
