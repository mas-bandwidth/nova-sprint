//go:build functional

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/config"
	"github.com/mas-bandwidth/nova-tools/hook"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/fn"
	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/testutil"
)

// ujRig is a sprint on a real store (a redis-server of its own, the function library
// loaded), its server in this process on a clock the test steps, the seat silent, and the
// bus (the owner's and the seat's) a recorder. The environment is the store's address and
// the seat's name, HOME and XDG_CONFIG_HOME in the test's own directory, and nothing else:
// no NOVA_* of the machine the test runs on.
type ujRig struct {
	t    *testing.T
	ctx  context.Context
	addr string
	a    *app
	st   *store.Store
	mu   sync.Mutex
	now  time.Time
	sent []bus.Message
}

const ujSeat = "seat-uj"

func newUJRig(t *testing.T) *ujRig {
	t.Helper()
	r := &ujRig{t: t, ctx: context.Background(), now: time.Now().UTC().Truncate(time.Second)}
	r.addr = testutil.Start(t)
	admin := redis.NewClient(&redis.Options{Addr: r.addr})
	t.Cleanup(func() { _ = admin.Close() })
	require.NoError(t, fn.Load(r.ctx, admin))
	home := t.TempDir()
	env := map[string]string{"NOVA_SPRINT_REDIS": r.addr, "NOVA_SPRINT_ACTOR": ujSeat, "HOME": home, "XDG_CONFIG_HOME": home,
		busRedisEnv: "bus.test:6379"} // Identity resolution only; the bus recorder below never dials it.
	r.a = newApp(func(k string) string { return env[k] })
	// The hook and sprint records share only this rig's private Redis connection.
	// No hook record means down; the independently stored sprint hold still applies.
	r.a.readFriendHooks = (hook.Store{C: admin}).Read
	t.Cleanup(r.a.close)
	r.a.now = func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }
	r.a.sleep = func(time.Duration) {}
	r.a.bus = func(_ context.Context, m bus.Message, _ func(string)) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.sent = append(r.sent, m)
		return nil
	}
	r.a.serveAddr = r.addr
	// the routes nova-config's apply writes: flash and heavy, no pro
	for _, x := range [][2]string{{"flash-a", "flash"}, {"heavy-a", "heavy"}} {
		require.NoError(t, admin.SAdd(r.ctx, config.RoutesKey, x[0]).Err())
		require.NoError(t, admin.HSet(r.ctx, config.RouteKey(x[0]), "name", x[0], "tier", x[1], "provider", "p-"+x[0], "model", "m",
			"tokens", "100000", "deadline", "900", "enabled", "true").Err())
	}
	return r
}

// do runs a verb line in the server's process, as the seat's verbs reach it: exit 0.
func (r *ujRig) do(line string) string {
	r.t.Helper()
	var out, errb bytes.Buffer
	code := runOnStore(r.a, r.addr, append(split(line), "--redis", r.addr), &out, &errb)
	require.Equal(r.t, 0, code, "%s: exit %d\n%s%s", line, code, out.String(), errb.String())
	return out.String()
}

// store is the sprint's store as the server holds it.
func (r *ujRig) store() *store.Store {
	r.t.Helper()
	if r.st == nil {
		st, err := r.a.store(common{redis: r.addr, actor: ujSeat})
		require.NoError(r.t, err)
		r.st = st
	}
	return r.st
}

// epoch is the sprint's epoch now, as a worker's report names it.
func (r *ujRig) epoch() string {
	r.t.Helper()
	es, err := r.store().EpochNow(r.ctx)
	require.NoError(r.t, err)
	return strconv.FormatUint(es.N, 10)
}

// beat is a beat of the member and the readers, so the clock's steps leave them up.
func (r *ujRig) beat() {
	r.t.Helper()
	r.do("fleet beat m1 --load 0")
	require.NoError(r.t, r.store().BeatReaders(r.ctx))
}

// step moves the clock d, beats everyone, and runs one turn of the server's loop: a tick
// with the rules answered, as nova-sprint run does, then the owner told.
func (r *ujRig) step(d time.Duration) {
	r.t.Helper()
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
	r.beat()
	st, _, code := r.a.machineVerb("run", []string{"--redis", r.addr}, io.Discard)
	require.NotNil(r.t, st, "run exit=%d", code)
	st.AnswerRules = true
	r.a.runLoop(r.ctx, st, 20, 1, io.Discard, io.Discard)
}

// col is the primary's column.
func (r *ujRig) col(id string) string {
	r.t.Helper()
	s, err := r.store().Load(r.ctx, []string{sprint.Work}, nil)
	require.NoError(r.t, err)
	c := s.Work.Placed(id)
	require.NotNil(r.t, c, "no primary %s", id)
	return c.Col
}

// work deals, takes and finishes every ready card on m1, and has the readers named read
// everything asked of them, ok.
func (r *ujRig) work(readers ...string) {
	r.t.Helper()
	res, err := r.store().Run(r.ctx, dealStep(sprint.DealReq{Sel: sprint.Sel{Limit: 100}}))
	require.NoError(r.t, err)
	require.Empty(r.t, res.Refused, "deal")
	r.do("take --as m1 --max 100 --epoch " + r.epoch())
	var q struct{ Cards []queueCard }
	require.NoError(r.t, json.Unmarshal([]byte(r.do("queue --as m1 --json")), &q))
	for _, c := range q.Cards {
		r.do("finish --as m1 " + c.ID + "@" + strconv.Itoa(c.Gen) + " --epoch " + r.epoch())
	}
	for range 2 {
		r.do("ask --max 100")
		for _, rd := range readers {
			var rq struct{ Cards []queueCard }
			require.NoError(r.t, json.Unmarshal([]byte(r.do("queue --as "+rd+" --json")), &rq))
			for _, c := range rq.Cards {
				named := c.ID + "@" + strconv.Itoa(c.Gen) + " --epoch " + r.epoch()
				r.do("read --as " + rd + " --begin " + named) // asked -> reading
				r.do("read --as " + rd + " --ok " + named + " --usage 'input=10 cache_read=0 cache_write=0 output=10 model=p-flash-a/m'")
			}
		}
	}
}

// pushes is every message one look of the server's coordinator push sent the seat on the
// bus (coordinator_push.go).
func (r *ujRig) pushes() []bus.Message {
	r.t.Helper()
	r.mu.Lock()
	before := len(r.sent)
	r.mu.Unlock()
	r.a.coordinatorPass(r.ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []bus.Message
	for _, m := range r.sent[before:] {
		if len(m.To) == 1 && m.To[0] == ujSeat {
			out = append(out, m)
		}
	}
	return out
}

// The owner, 2026-10-12: "This must never happen again." A real server and store, the seat
// hooked in and then silent, one card in each kind that held a card that night: within one
// deadline every card with a safe default has moved with no seat verb (returned for its
// missing read and read again, returned for its needs, pinned to a held friend, on a tier no
// route serves), and the cards that need a mind (returned by the seat; pinned by the
// coordinator to a tier no route serves) are named by the folded decisions-owed notice, on the
// seat's bus and on the owner's.
func TestEveryCardHoldingJudgmentIsAnsweredOrEscalatedOnAStore(t *testing.T) {
	t.Parallel()
	began := time.Now()
	r := newUJRig(t)
	r.do("init --readers reader-a,reader-b,reader-c --members m1 --owner glenn")
	r.beat()
	r.do("fleet up m1 --width 8")
	r.beat()
	_, _, _, err := r.store().SyncFriends(r.ctx, []store.FriendSpec{{Name: "amy", Width: 2, Class: "flash,pro"}})
	require.NoError(t, err)
	r.do("hold amy --reason out-of-credits")

	// s1-1: accepted on one read, the count raised to two, returned by repair for its
	// missing read (rule 6)
	r.do("set --reads 1")
	r.do("add --stream s1 --count 1 --one")
	r.work("reader-a")
	r.do("accept s1-1")
	r.do("set --reads 2")
	r.do("repair")
	require.Equal(t, sprint.Review, r.col("s1-1"))
	// s2-1 (to be returned for its needs) and s3-1 (returned by the seat, a mind's), both
	// read twice and accepted; s1-1's missing read is read with them
	r.do("add --stream s2 --count 1 --one")
	r.do("add --stream s3 --count 1 --one")
	r.work("reader-a", "reader-b", "reader-c")
	r.do("accept s2-1 s3-1")
	r.do("return s2-1 --reason 'its needs name a dropped card'")
	r.do("needs set s2-1 --clear")
	r.do("return s3-1 --reason 'look again before it lands'")
	// s4-1: pinned to amy alone, and she is held
	pinned := filepath.Join(t.TempDir(), "pinned.md")
	require.NoError(t, os.WriteFile(pinned, []byte("c: a friend card tier: flash\nWHO: only friend amy\n\nRULES.\n"+
		"Work only in the job directory this card names.\nNever force-push or rebase a shared branch.\n"+
		"Never kill a process you did not start.\nNever start a server on this machine.\n"+
		"No `rm -rf` outside the job directory.\nReport what was not done.\n"), 0o600))
	r.do("add --stream s4 --count 1 --one --brief-file " + pinned)
	// s5-1: its line 1 names pro, which no route serves; s6-1: pinned to pro by the
	// coordinator (brief --tier), which the rule never overrides
	pro := filepath.Join(t.TempDir(), "pro.md")
	require.NoError(t, os.WriteFile(pro, []byte("c: a pro card tier: pro\n\nRULES.\n"+
		"Work only in the job directory this card names.\nNever force-push or rebase a shared branch.\n"+
		"Never kill a process you did not start.\nNever start a server on this machine.\n"+
		"No `rm -rf` outside the job directory.\nReport what was not done.\n"), 0o600))
	r.do("add --stream s5 --count 1 --one --brief-file " + pro)
	r.do("add --stream s6 --count 1 --one")
	r.do("brief s6-1 --tier pro --reason 'coordinator tier change'")
	// check that re-tier without --reason is refused
	var out, errb bytes.Buffer
	code := runOnStore(r.a, r.addr, split("brief s6-1 --tier heavy"), &out, &errb)
	require.Equal(t, 2, code, "re-tier without --reason should be refused: %s", errb.String())
	r.do("start")
	// the seat says nothing from here on
	r.step(time.Second)
	assert.Equal(t, sprint.Merging, r.col("s1-1"), "returned for its read, read again: merging with no seat verb")
	assert.Equal(t, sprint.Merging, r.col("s2-1"), "returned for its needs, set: merging with no seat verb")
	assert.Equal(t, sprint.Ready, r.col("s4-1"), "pinned to a held friend: waits, within the deadline")
	assert.Equal(t, sprint.Ready, r.col("s5-1"), "no route serves pro: waits, within the deadline")
	assert.Equal(t, sprint.Ready, r.col("s6-1"), "no route serves pro: waits, within the deadline")
	assert.Equal(t, sprint.Review, r.col("s3-1"), "returned by the seat: the seat's")

	r.step(sprint.DeadlineJudgment / 2)
	r.step(sprint.DeadlineJudgment/2 + time.Minute)
	r.step(time.Second)
	assert.Equal(t, sprint.Working, r.col("s4-1"), "the pin released at its deadline: dealt on")
	assert.Equal(t, sprint.Working, r.col("s5-1"), "raised to heavy, the nearest served tier: dealt")
	assert.Equal(t, sprint.Ready, r.col("s6-1"), "pinned to pro by the coordinator: stays put, never re-tiered")
	assert.Equal(t, sprint.Review, r.col("s3-1"), "still the seat's")

	// The deadline lane replaces the coordinator's legacy seat-silent alarm.
	// The tier notice must name only the cards still waiting, not s5 which
	// has already moved. Require the first delivery before checking no resend.
	r.mu.Lock()
	before := len(r.sent)
	r.mu.Unlock()
	first := r.pushes()
	require.NotEmpty(t, first, "first look must deliver to the coordinator")
	r.mu.Lock()
	delivered := append([]bus.Message(nil), r.sent[before:]...)
	r.mu.Unlock()
	for _, to := range []string{ujSeat, "glenn"} {
		var notices []bus.Message
		for _, m := range delivered {
			if m.Subject == "nova-sprint: decisions owed" && len(m.To) == 1 && m.To[0] == to {
				notices = append(notices, m)
			}
		}
		require.Len(t, notices, 1, "first look delivers one folded notice to %s: %+v", to, delivered)
		body := notices[0].Body
		assert.Contains(t, body, "s3-1")
		assert.Contains(t, body, "cards=s6-1")
		assert.Contains(t, body, "1 primaries of tier pro wait:")
		assert.NotContains(t, body, "s5-1")
		assert.NotContains(t, body, "2 primaries of tier pro wait:")
	}
	r.mu.Lock()
	before = len(r.sent)
	r.mu.Unlock()
	r.pushes()
	r.mu.Lock()
	resent := append([]bus.Message(nil), r.sent[before:]...)
	r.mu.Unlock()
	for _, m := range resent {
		assert.NotEqual(t, "nova-sprint: decisions owed", m.Subject, "unchanged overdue set is not resent to either recipient")
	}
	assert.Less(t, time.Since(began), 30*time.Second, "under 30 s")
}
