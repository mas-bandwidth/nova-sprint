package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/hook"
)

func friendStartedFixture(t *testing.T, count int, repoCount int, friends ...string) *testApp {
	t.Helper()
	ta, _ := friendApp(t, friends...)
	ta.ok("friend sync")
	for _, friend := range friends {
		ta.beatUp(friend)
	}
	dirs := make([]string, repoCount)
	for i := range dirs {
		dirs[i] = t.TempDir()
	}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("started-%03d", i)
		repo := fmt.Sprintf("mas-bandwidth/repo-%d", i%repoCount)
		path := filepath.Join(dirs[i%repoCount], id+".md")
		brief := passingBrief(fmt.Sprintf("%s: friend started fixture\nREPO: %s\nWHO: friend", id, repo))
		require.NoError(t, os.WriteFile(path, []byte(brief), 0o644))
	}
	for _, dir := range dirs {
		ta.ok(fmt.Sprintf("add --stream s1 --brief-dir %s", dir))
	}
	ta.deal(count)
	return ta
}

func TestFriendStartedReadsEachRepoOnce(t *testing.T) {
	t.Parallel()
	friends := []string{"amy", "bob", "cy"}
	ta := friendStartedFixture(t, 30, 2, friends...)
	calls := map[string]int{}
	var callsMu sync.Mutex
	ta.a.tips = func(_ context.Context, repo string) (map[string]string, error) {
		callsMu.Lock()
		calls[repo]++
		callsMu.Unlock()
		if repo == "mas-bandwidth/repo-0" {
			return map[string]string{"sprint/started-000.w1.g1.e0": landHead}, nil
		}
		return map[string]string{}, nil
	}
	st, err := ta.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	started, err := ta.a.friendStartedFor(context.Background(), st, friends)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"started-000.w1": "a push on its branch sprint/started-000.w1.g1.e0 at " + landHead}, started)
	assert.Equal(t, map[string]int{"mas-bandwidth/repo-0": 1, "mas-bandwidth/repo-1": 1}, calls)
}

func TestFriendStartedAnUnreadRepoKeepsItsCards(t *testing.T) {
	t.Parallel()
	ta := friendStartedFixture(t, 10, 1, "amy")
	ta.a.tips = func(context.Context, string) (map[string]string, error) { return nil, errors.New("offline") }
	st, err := ta.a.store(common{redis: "mem:0"})
	require.NoError(t, err)
	started, err := ta.a.friendStarted(context.Background(), st, "amy")
	require.NoError(t, err)
	require.Len(t, started, 10)
	for id, why := range started {
		assert.Contains(t, why, "cannot be read (offline)", id)
	}
	out := ta.ok("friend level")
	assert.Contains(t, out, "FRIEND-LEVEL OK moved=0")
}

// BenchmarkFriendStarted has 100 held cards on three friends, across three repositories.
// The fake tips reader reports calls per operation so the per-card regression is visible.
func BenchmarkFriendStarted(b *testing.B) {
	mem := store.NewMem()
	cfg := config.NewMem()
	friends := []string{"amy", "bob", "cy"}
	for _, name := range friends {
		if _, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: name, Fields: map[string]string{"tiers": "flash", "width": "8"}}, "bench"); err != nil {
			b.Fatal(err)
		}
	}
	env := map[string]string{"NOVA_SPRINT_REDIS": "mem:0", "NOVA_SPRINT_ACTOR": "coordinator"}
	a := newApp(func(k string) string { return env[k] })
	a.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return mem, nil }
	at := time.Unix(1000, 0).UTC()
	a.now = func() time.Time { return at }
	a.cardCheckGate = false
	a.checkTree = func(context.Context, swarm.CardBase) sprint.AdmissionTree {
		return sprint.AdmissionTree{Missing: "benchmark fixture"}
	}
	a.friends = func(ctx context.Context, _ string) ([]config.Row, error) { return cfg.List(ctx, config.KindFriend) }
	a.readFriendHooks = func(_ context.Context, names ...string) ([]hook.Status, time.Time, error) {
		var statuses []hook.Status
		for _, name := range names {
			statuses = append(statuses, hook.Status{Name: name, Role: hook.Friend, State: hook.StateProven})
		}
		return statuses, at, nil
	}
	run := func(args ...string) {
		var out, stderr bytes.Buffer
		if code := a.run(args, &out, &stderr); code != 0 {
			b.Fatalf("%v: exit %d: %s%s", args, code, out.String(), stderr.String())
		}
	}
	run("init", "--readers", "reader-a,reader-b", "--members", "m1,m2")
	run("friend", "sync")
	for _, name := range friends {
		run("friend", "up", name)
		run("friend", "beat", name)
	}
	dirs := []string{b.TempDir(), b.TempDir(), b.TempDir()}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("bench-%03d", i)
		repo := i % len(dirs)
		brief := passingBrief(fmt.Sprintf("%s: friend started benchmark\nREPO: mas-bandwidth/repo-%d\nWHO: friend", id, repo))
		if err := os.WriteFile(filepath.Join(dirs[repo], id+".md"), []byte(brief), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	for _, dir := range dirs {
		run("add", "--stream", "s1", "--brief-dir", dir)
	}
	st, err := a.store(common{redis: "mem:0"})
	if err != nil {
		b.Fatal(err)
	}
	if _, err := st.Run(context.Background(), dealStep(sprint.DealReq{Sel: sprint.Sel{Limit: 100}})); err != nil {
		b.Fatal(err)
	}
	tipsCalls := 0
	var tipsMu sync.Mutex
	a.tips = func(context.Context, string) (map[string]string, error) {
		tipsMu.Lock()
		tipsCalls++
		tipsMu.Unlock()
		return map[string]string{}, nil
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := a.friendStartedFor(context.Background(), st, friends); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(tipsCalls)/float64(b.N), "tips/op")
}

// The verbs friends lacked that machines have (the owner, 2026-10-04: "What else is like
// this? Missing verbs we need for friends, that machines already have"), on the twin.

// friendWidth is the friend's width as where --json shows it.
func friendWidth(ta *testApp, friend string) string {
	var w whereView
	ta.json("where", &w)
	return cellText(w.Tables[sprint.Friends][friend]["width"])
}

func TestFriendUpWidthSetsHerWidthAsFleetUpWidthSetsAMembers(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync")
	assert.Equal(t, "8", friendWidth(ta, "amy"))
	out := ta.ok("friend up amy --width 3")
	assert.Contains(t, out, "FRIEND-UP OK amy held=false width=3")
	assert.Equal(t, "3", friendWidth(ta, "amy"))
	ta.ok("friend down amy")
	ta.ok("friend up amy")
	assert.Equal(t, "3", friendWidth(ta, "amy"), "a release without --width leaves it as it is")

	code, _, errs := ta.do("friend up amy --width 0")
	assert.Equal(t, 2, code)
	assert.Contains(t, errs, "--width: a width wants a whole number from 1 to")
	code, _, _ = ta.do("friend down amy --width 3")
	assert.Equal(t, 2, code, "friend down takes no width")

	// friend sync sets her nova-config row's again, as fleet sync sets a member's
	ta.ok("friend sync")
	assert.Equal(t, "8", friendWidth(ta, "amy"))
}

// whereFriends is where --json's friends, by name.
func whereFriends(ta *testApp) map[string]store.FriendRow {
	var w struct {
		Friends []store.FriendRow `json:"friends"`
	}
	ta.json("where", &w)
	out := map[string]store.FriendRow{}
	for _, f := range w.Friends {
		out[f.Name] = f
	}
	return out
}

// A friend's machinery beats with her counts and her load, as a machine's beats with its
// load; the beat took her name alone and refused them (Johnny, 2026-10-04).
func TestFriendBeatTakesHerCountsAndLoadAsFleetBeatTakesALoad(t *testing.T) {
	t.Parallel()
	ta, _ := friendApp(t, "amy")
	ta.ok("friend sync")
	ta.pong("amy")
	out := ta.ok("friend beat amy --working 2 --queue 3 --width 4 --running s1-1.w1,s1-2.w1 --load 40%")
	assert.Contains(t, out, "FRIEND-BEAT OK amy at=")
	assert.Contains(t, out, " working=2 queue=3 width=4 load=40.0% running=s1-1.w1,s1-2.w1")
	f := whereFriends(ta)["amy"]
	require.NotNil(t, f.Report)
	assert.Equal(t, 2, *f.Report.Working)
	assert.Equal(t, 3, *f.Report.Queue)
	assert.Equal(t, 4, *f.Report.Width)
	assert.Equal(t, []string{"s1-1.w1", "s1-2.w1"}, f.Report.Running)
	assert.Equal(t, 40.0, f.Load)
	assert.Equal(t, 8, f.Width, "her width is the roster's, not her word")
	assert.Equal(t, sprint.Up, f.Status, "on her session's pong, never on the beat")

	// a beat with none reports none: the last one's counts are not kept
	ta.beatUp("amy")
	f = whereFriends(ta)["amy"]
	assert.Nil(t, f.Report)
	assert.Zero(t, f.Load)

	for _, bad := range []string{"--working -1", "--queue x", "--width 0", "--load lots"} {
		code, _, errs := ta.do("friend beat amy " + bad)
		assert.Equal(t, 2, code, bad)
		assert.Contains(t, errs, "wants", bad)
	}
	// and the server runs it with them
	_, _, why := workerVerb([]string{"friend", "beat", "amy", "--working", "2", "--queue", "3", "--width", "4", "--load", "40"})
	assert.Empty(t, why)
	_, _, why = workerVerb([]string{"friend", "beat", "amy", "--width", "0"})
	assert.NotEmpty(t, why)
}

// friend level on the twin: a card queued on one friend moves to another of her class with
// room, which friend sync delivers as a new job, and the queue file of the friend it left
// marks it taken.
func TestFriendLevelMovesAQueuedCardAndTheQueueFilesFollow(t *testing.T) {
	t.Parallel()
	ta, root := takeApp(t, 4, nil, "amy", "bob")
	ta.ok("friend up amy --width 1")
	ta.ok("friend up bob --width 1")
	ta.ok("friend down bob")
	ta.ok("tick") // amy alone: s1-1 and s1-2 ready, and she starts s1-1
	ta.startFriend("amy", 1)
	ta.ok("friend sync --root " + root)
	require.Equal(t, "queued", queueStates(t, root, "amy")["s1-2.w1"])
	ta.ok("friend up bob")
	// friend sync set her width back to the config's 8; at 1 her lane is her started card's,
	// and s1-2.w1 waits behind it (an unstarted card in a free lane of hers is hers to start)
	ta.ok("friend up amy --width 1")
	ta.beatUp("bob")

	assert.Contains(t, ta.dry("friend level --dry-run"), "FRIEND-LEVEL DRY-RUN up=amy,bob; nothing was changed")
	out := ta.ok("friend level")
	assert.Contains(t, out, "s1-2.w1 friend.amy:ready -> friend.bob:ready gen=2; moved=1 to bob(1) from amy(1)")
	assert.Contains(t, out, "FRIEND-LEVEL OK moved=1")
	out = ta.ok("friend sync --root " + root)
	assert.Contains(t, out, "FRIEND-CARD DELIVERED friend=bob card=s1-2.w1 job=s1-2.w1.g2")
	assert.Equal(t, "taken", queueStates(t, root, "amy")["s1-2.w1"], "not hers to start any more")
	assert.Equal(t, "queued", queueStates(t, root, "bob")["s1-2.w1"], "ready on his row until he starts it")
	assert.Contains(t, ta.ok("friend level"), "FRIEND-LEVEL OK moved=0")
	ta.clean()
}

// friend level treats a one-shot friend as her width (one lane per unit of width, the owner
// 2026-10-10): bob, one-shot at width 2, takes both of amy's ready cards.
func TestFriendLevelRespectsOneShotDeliveryMode(t *testing.T) {
	t.Parallel()
	ta, cfg := friendApp(t)
	ta.a.tip = func(_ context.Context, _, _ string) (string, error) { return "", nil }
	_, err := cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "amy", Fields: map[string]string{"tiers": "flash", "width": "2", "mode": "batch"}}, "t")
	require.NoError(t, err)
	_, err = cfg.Insert(context.Background(), config.KindFriend, config.Row{Name: "bob", Fields: map[string]string{"tiers": "flash", "width": "2", "mode": "one-shot"}}, "t")
	require.NoError(t, err)

	root := t.TempDir()
	ta.ok("friend sync --root " + root)
	ta.beatUp("amy")
	ta.beatUp("bob")

	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("s1-%d", i)
		brief := filepath.Join(t.TempDir(), id+".md")
		require.NoError(t, os.WriteFile(brief, []byte(passingBrief(id+": a friend's card\nREPO: mas-bandwidth/nova-tools\nWHO: friend")), 0o644))
		ta.ok("add --stream s1 --brief-dir " + filepath.Dir(brief))
	}
	ta.ok("start")

	ta.ok("friend down bob")
	ta.ok("tick") // amy (batch, width 2) fills her room of 4, all ready, and starts 2
	ta.startFriend("amy", 2)
	ta.ok("friend sync --root " + root)

	ta.ok("friend up bob")
	ta.beatUp("bob")

	out := ta.ok("friend level")
	// bob's room is his width's (one-shot as batch), so both of amy's ready cards move
	assert.Contains(t, out, "FRIEND-LEVEL OK moved=2")
	assert.Contains(t, ta.ok("friend level"), "FRIEND-LEVEL OK moved=0")

	f := whereFriends(ta)
	assert.Equal(t, 2, f["amy"].Working)
	assert.Equal(t, 0, f["amy"].Ready)
	assert.Equal(t, 0, f["bob"].Working, "ready on his row until he starts them")
	assert.Equal(t, 2, f["bob"].Ready)
	ta.clean()
}

// stats fills a friend's run wall and report lag from her finish records, where they were
// dashes: her card reports no usage, so her run is her take to her report (REPORT.md's
// time, kept on the card as reported) and her lag her report to the sync that finished it.
func TestStatsTimesAFriendsRunFromHerReport(t *testing.T) {
	t.Parallel()
	ta, root := friendCardApp(t, "friend amy", "amy")
	ta.ok("tick") // dealt at t0, and she starts it then
	ta.startFriend("amy", 1)
	ta.ok("friend sync --root " + root)
	taken := ta.a.now()
	ta.a.sleep(10 * time.Minute)
	outboxReport(t, root, "amy", "s1-1.w1", "Verdict: LAND\nHead: "+landHead+"\n\nDone.\n")
	report := filepath.Join(root, "amy-working", "outbox", "s1-1.w1", "REPORT.md")
	require.NoError(t, os.Chtimes(report, taken.Add(8*time.Minute), taken.Add(8*time.Minute)))
	ta.ok("friend sync --root " + root)

	var c cardView
	ta.json("card s1-1", &c)
	assert.Equal(t, taken.Add(8*time.Minute).UTC().Format(time.RFC3339), c.Work[0].F(sprint.FieldReported))
	var ps sprint.PassStats
	ta.json("stats", &ps)
	require.Len(t, ps.Work, 1)
	assert.Equal(t, sprint.FriendRow("amy"), ps.Work[0].Member)
	assert.Equal(t, sprint.Measure{Median: 480, Max: 480, N: 1}, ps.Work[0].RunWall, "her take to her report")
	assert.Equal(t, sprint.Measure{Median: 120, Max: 120, N: 1}, ps.Work[0].ReportLag, "her report to the finish")
	ta.clean()
}

// On the twin: once amy has an ok attempt whose run (her take to her report) was an hour,
// a card she takes after it carries her deadline, three hours from its take, past the
// fleet's two, as where --json --cards shows it; one taken before it keeps the fleet's.
func TestAFriendsNextCardsDeadlineFollowsHerRunWall(t *testing.T) {
	t.Parallel()
	ta, root := takeApp(t, 3, map[string]string{"sprint/s1-1.w1.g1.e0": landHead}, "amy")
	ta.ok("friend up amy --width 1")
	ta.ok("tick") // s1-1 and s1-2 ready, s1-3 waits (friend sync sets her width back to 8); she starts s1-1
	ta.startFriend("amy", 1)
	ta.ok("friend sync --root " + root)
	taken := ta.a.now()
	ta.a.sleep(70 * time.Minute)
	outboxReport(t, root, "amy", "s1-1.w1", "Verdict: LAND\nHead: "+landHead+"\n\nDone.\n")
	report := filepath.Join(root, "amy-working", "outbox", "s1-1.w1", "REPORT.md")
	require.NoError(t, os.Chtimes(report, taken.Add(time.Hour), taken.Add(time.Hour)))
	ta.ok("friend sync --root " + root)
	ta.beatUp("amy")
	ta.ok("tick")            // s1-3 dealt to her, s1-1's hour behind her
	ta.startFriend("amy", 2) // she starts s1-2 and s1-3 after her ok attempt

	var w whereView
	ta.json("where --cards", &w)
	limit := map[string]time.Duration{}
	for _, c := range w.Cards {
		assert.Equal(t, sprint.Working, c.State, c.ID)
		limit[c.ID] = c.Deadline.Sub(c.Since)
	}
	assert.Equal(t, map[string]time.Duration{"s1-2.w1": 3 * time.Hour, "s1-3.w1": 3 * time.Hour}, limit, "each started after her ok attempt: three times her one hour, from her start")
	ta.clean()
}

// A friend's beat proves her session only by naming a check her daemon's run asked: a bare
// time (outside the server's first hour), a nonce never asked, another run's answer, or
// the same answer twice proves nothing (recorded as a beat with no proof); the answer to
// a check her daemon's run asked within fifteen minutes makes her up, and only while her
// beats go on. It does not stop a caller who beats as her from asking and answering at
// once (TestTheBeatTrustsItsCallersActor).
