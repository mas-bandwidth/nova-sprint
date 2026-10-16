package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/mas-bandwidth/nova-tools/pkg/sprintwire"
)

// THE SERVER'S LANES. The serial lock (app.serial) is the one line of control: a tick, a
// batch's writes and the land, decide and balance lanes' short store steps take it in
// turn. On 2026-10-04 everything the server answered took it too, and the line saturated:
// from 2:06 PM the run loop, which takes the same line for each tick, ticked every 10 to
// 50 s and from 2:14 PM every 36 to 151 s while each tick took 0.5 to 6 s of it; every
// friend's beat (two a second a friend: the daemon's and the beat loop's) and the two
// dashboards' `where --json --cards` (each once a second) waited behind it, the beats
// past their 10 s deadline, and each beat that timed out was still run when its turn
// came, so the line never drained. A verb is answered within a second whatever the tick
// or the lander is doing when it does not need the line:
//
//   - a friend's beat (`friend beat <friend>`, a worker's verb) writes one key outside
//     every table (the friend's beat record; store.FriendBeat: the roster read, the record
//     written): it runs on the beat lane, beside the line and beside every other beat,
//     never waiting for a tick, a batch or another beat;
//   - a read (where, card, log, check, routes, stats, needs, goal show, handover, and
//     inbox without --read) writes nothing: it runs on the read lane, one read at a time
//     on its own process state (its own read twin), beside the line, as a client reading
//     the store directly always has;
//   - the dashboard's read (`where --json --dashboard`, once a second for each dashboard)
//     has a lane of its own, beside the line and the read lane: it is answered from a
//     copy computed one at a time on its own app and lock, which callers within a second
//     of its start share, so it never waits behind the reads ahead of the read lane
//     (dashWhere; on 2026-10-15 it took 2.5 to 11.4 s behind them against 0.3 s on the
//     store);
//   - every other verb (take, finish, read, queue, which records a reader's beat, fleet
//     beat, which can write the fleet table, and every coordinator's write) waits for the
//     line as before, and the line waits for its caller: a batch whose caller has gone is
//     not run (serialLock.LockCtx).
//
// The lanes run only on a store that is safe from more than one goroutine: Redis, and
// the in-memory store a test gives (store.Mem). A twin file (mem:<file>) is written
// whole after every verb, so on a twin file every verb takes the line, as before.
// docs/SPEC-SPRINT.md section 14, The server; tla/ServerLanes.tla.

// readLaneVerbs are the verbs the read lane runs: each reads the store and writes
// nothing (inbox only without --read, which moves the coordinator's cursor).
var readLaneVerbs = map[string]bool{
	"where": true, "card": true, "log": true, "check": true, "routes": true, "stats": true,
	"needs": true, "goal show": true, "handover": true, "inbox": true,
}

// onReadLane says the verb, as its flags read it, writes nothing and runs on the read lane.
func onReadLane(v verbArgs) bool {
	return readLaneVerbs[v.name] && (v.name != "inbox" || !v.on("read"))
}

// dashFloor is how long a dashboard copy answers: callers within it of the start of the
// compute that made it share it.
const dashFloor = time.Second

// onDashLane says the verb, as its flags read it, is the dashboard's read: where with
// --json and --dashboard and no other flag but the actor (the server adds its own
// --redis), and no words. Any other where is a read like the rest.
func onDashLane(v verbArgs, argv []string) bool {
	if v.name != "where" || v.err != nil || v.help || !v.on("json") || !v.on("dashboard") {
		return false
	}
	other := false
	v.fs.Visit(func(f *flag.Flag) { other = other || !slices.Contains([]string{"json", "dashboard", "actor"}, f.Name) })
	if other {
		return false
	}
	// every word after the verb is a flag of those (or its value): no positional word
	rest := normalizeArgs(argv)[v.words:]
	n := 0
	if _, err := parseEach(v.fs, rest, func(_ string, _, k int) { n += k }); err != nil {
		return false
	}
	return n == len(rest)
}

// isFriendBeat says the worker's verb is a friend's beat (workerVerb has held it to
// `friend beat <friend>` and its report's flags).
func isFriendBeat(argv []string) bool {
	return len(argv) >= 3 && argv[0] == "friend" && argv[1] == "beat"
}

// serveLanes is the server's lanes beside the line, made once, on the line, at the first
// batch (app.lanesFor).
type serveLanes struct {
	// off says the store is a twin file: every verb takes the line.
	off bool
	// b is the lanes' backend: the server's store, on its own round-trip counter so a
	// tick's TIMES count the tick's trips alone.
	b   store.Backend
	now func() time.Time
	// line is the read lane's own line: one read at a time on read's state, given up by a
	// caller that has gone as the server's line is.
	line serialLock
	read *app
	// dash is the dashboard lane (dashWhere).
	dash dashLane
}

// dashLane is the dashboard's read lane: the last copy and the lock one compute at a time
// takes, on an app of its own. tla/ServerLanes.tla, DashWaitsOnlyForDash.
type dashLane struct {
	// line is the lane's own lock, never the read lane's nor the server's; a caller that
	// has gone gives up its wait as the read lane's does.
	line serialLock
	// app is the dashboard's own app, made at the first compute (under line).
	app *app
	// kept is the last answer and when its compute began; read without any lock.
	kept atomic.Pointer[dashCopy]
}

// dashCopy is one answer of the dashboard lane: its Result, the clock when its compute
// began (the floor counts from it) and when it ended.
type dashCopy struct {
	res        sprintwire.Result
	began, end time.Time
}

// lanesFor is the server's lanes, made on the first call that can take the line; nil
// when the store is a twin file or the lanes could not be made yet (every verb then takes
// the line, and the next batch tries again).
func (a *app) lanesFor(ctx context.Context) *serveLanes {
	a.lanesMu.Lock()
	defer a.lanesMu.Unlock()
	if a.lanes != nil {
		if a.lanes.off {
			return nil
		}
		return a.lanes
	}
	if a.serveAddr == "" {
		return nil
	}
	if err := a.serial.LockCtx(ctx); err != nil {
		return nil
	}
	b, err := a.backend(ctx, a.serveAddr, sprint.Names{})
	off := a.twinOpen(a.serveAddr)
	a.serial.Unlock()
	if err != nil {
		return nil
	}
	if off {
		a.lanes = &serveLanes{off: true}
		return nil
	}
	if rb, ok := b.(*store.Redis); ok {
		// the same connections (go-redis is safe from many goroutines), its own counter
		own := &store.Redis{C: rb.C, Names: rb.Names, Now: rb.Now}
		own.CountTrips()
		b = own
	}
	l := &serveLanes{b: b, now: a.now}
	r := newApp(a.getenv)
	r.now, r.sleep, r.after, r.meter, r.home, r.screen, r.transport, r.checkTwin = a.now, a.sleep, a.after, a.meter, a.home, a.screen, a.transport, a.checkTwin
	r.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return b, nil }
	r.serveAddr = a.serveAddr
	r.readFriendHooks = a.readFriendHooks
	r.hk, r.hookStoreFn, r.busOpen = a.hk, a.hookStoreFn, a.busOpen // the server's hook record connection: the gate reads it on this lane too
	l.read = r
	a.lanes = l
	return l
}

// friendBeat is a friend's beat on the beat lane: no line taken, answered as friend beat
// answers on the store (a.friendBeat on args, the words after the verb with the server's
// store and actor, its report's flags read as the verb reads them; argv is as sent). A
// beat whose caller has gone is not run (tla/ServerLanes.tla, GoneNeverRuns; TLC found the
// first cut writing it anyway).
func (l *serveLanes) friendBeat(ctx context.Context, a *app, argv, args []string) sprintwire.Result {
	if ctx.Err() != nil {
		return goneResult(argv)
	}
	open := func(c common) (*store.Store, error) {
		return &store.Store{B: l.b, Names: sprint.Names{}, Actor: c.actor, Now: l.now}, nil
	}
	var stdout, stderr bytes.Buffer
	code := a.friendBeat(ctx, args, open, &stdout, &stderr)
	return sprintwire.Result{Code: code, Stdout: stdout.String(), Stderr: stderr.String(), Outcome: a.outcome, Changed: a.changed}
}

// isBudget says the worker's verb is a budget take, renew or release (workerVerb has held it to
// its words): it writes only its own keys, outside every table, and runs on the beat lane,
// so a model request never waits for a tick (rate_budget.go).
func isBudget(argv []string) bool {
	return len(argv) >= 2 && argv[0] == "budget" && (argv[1] == "take" || argv[1] == "renew" || argv[1] == "release")
}

// budget is a budget take or release on the beat lane: no line taken, run by the read
// lane's app on the lanes' backend, and not run for a caller that has gone.
func (l *serveLanes) budget(ctx context.Context, argv, args []string) sprintwire.Result {
	if ctx.Err() != nil {
		return goneResult(argv)
	}
	// its own app on the lanes' backend: many takes run at once, none on another's state
	r := newApp(l.read.getenv)
	b := l.b
	r.now, r.serveAddr = l.now, l.read.serveAddr
	r.readFriendHooks = l.read.readFriendHooks
	r.hk, r.hookStoreFn, r.busOpen = l.read.hk, l.read.hookStoreFn, l.read.busOpen // the server's hook record connection, never a store per take
	r.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return b, nil }
	var stdout, stderr bytes.Buffer
	code := r.run(args, &stdout, &stderr)
	return sprintwire.Result{Code: code, Stdout: stdout.String(), Stderr: stderr.String(), Outcome: r.outcome, Changed: r.changed}
}

// readVerbRun is a read on the read lane: it waits for the reads ahead of it alone, and
// not past its caller.
func (l *serveLanes) readVerbRun(ctx context.Context, args []string) sprintwire.Result {
	if err := l.line.LockCtx(ctx); err != nil {
		return goneResult(args)
	}
	defer l.line.Unlock()
	var stdout, stderr bytes.Buffer
	code := l.read.run(args, &stdout, &stderr)
	return sprintwire.Result{Code: code, Stdout: stdout.String(), Stderr: stderr.String(), Outcome: l.read.outcome, Changed: l.read.changed}
}

// dashWhere is the dashboard's read on the dashboard lane (tla/ServerLanes.tla,
// DashWaitsOnlyForDash; docs/SPEC-SPRINT.md section 14, The server): a caller within a
// second of the start of the last compute is handed that compute's Result, taking no lock;
// any other computes once, one at a time, on the dashboard's own app and lock, and a caller
// that arrives while a compute runs waits for it and shares its Result. It never takes the
// server's line (a.serial) nor the read lane's (l.line), and a caller that has gone is not
// run. Only a Result that succeeded is kept.
func (l *serveLanes) dashWhere(ctx context.Context, args []string) sprintwire.Result {
	if ctx.Err() != nil {
		return goneResult(args)
	}
	d := &l.dash
	arrived := l.now()
	fresh := func() (sprintwire.Result, bool) {
		c := d.kept.Load()
		if c == nil {
			return sprintwire.Result{}, false
		}
		if now := l.now(); now.Sub(c.began) < dashFloor && !now.Before(c.began) || !c.end.Before(arrived) {
			return c.res, true
		}
		return sprintwire.Result{}, false
	}
	if res, ok := fresh(); ok {
		return res
	}
	if err := d.line.LockCtx(ctx); err != nil {
		return goneResult(args)
	}
	defer d.line.Unlock()
	if res, ok := fresh(); ok {
		return res // the compute it waited for
	}
	if d.app == nil {
		// its own app on the lanes' backend, as budget makes its own: never the read
		// lane's state
		r := newApp(l.read.getenv)
		b := l.b
		r.now, r.sleep, r.after, r.meter, r.home, r.screen, r.transport, r.checkTwin = l.read.now, l.read.sleep, l.read.after, l.read.meter, l.read.home, l.read.screen, l.read.transport, l.read.checkTwin
		r.serveAddr = l.read.serveAddr
		r.readFriendHooks = l.read.readFriendHooks
		r.hk, r.hookStoreFn, r.busOpen = l.read.hk, l.read.hookStoreFn, l.read.busOpen // the server's hook record connection
		r.backend = func(context.Context, string, sprint.Names) (store.Backend, error) { return b, nil }
		d.app = r
	}
	began := l.now()
	var stdout, stderr bytes.Buffer
	code := d.app.run(args, &stdout, &stderr)
	res := sprintwire.Result{Code: code, Stdout: stdout.String(), Stderr: stderr.String(), Outcome: d.app.outcome, Changed: d.app.changed}
	if code == 0 {
		d.kept.Store(&dashCopy{res: res, began: began, end: l.now()})
	}
	return res
}

// goneResult is the answer of a verb not run because its caller went away while it
// waited: no one reads it, and nothing was changed.
func goneResult(argv []string) sprintwire.Result {
	verb := ""
	if len(argv) > 0 {
		verb = argv[0]
	}
	return sprintwire.Result{Code: 2, Stderr: fmt.Sprintf("%s server: %s: not run: its caller went away while it waited; nothing was changed\n", prog, oneline.Escape(verb)), Outcome: "unknown", Changed: "no"}
}

// ServeSayEvery is how often the server says what its batches cost (a SERVE line), when
// it answered any.
const ServeSayEvery = time.Minute

// serveTally is what the server's batches cost since its last SERVE line.
type serveTally struct {
	mu                    sync.Mutex
	since                 time.Time
	batches, beats, reads int
	serialVerbs, gone     int
	waitMax, holdMax      time.Duration
	holdWhat              string
	// refused counts the refused verbs since the last SERVE line, by verb word (logRefused,
	// serve.go), so the line says refused=<n> by verb (docs/SPEC-SPRINT.md section 14).
	refused map[string]int
	// tickEnd is when the run loop's last tick ended and tickDeadline that tick's
	// deadline (tickEnded): the SERVE line says tick-age, and tick=stale past twice the
	// deadline, so a server whose tick has stopped reads stopped, not up
	// (a-card-read-never-runs-the-deal). Kept across SERVE lines.
	tickEnd      time.Time
	tickDeadline time.Duration
	// refuseText, refuseSince and refuseRepeat are the refusal rate bound (logRefused): the
	// last line said, when it was said, and how many identical refusals have stood behind it
	// since; unlike the counts above they are kept across SERVE lines.
	refuseText   string
	refuseSince  time.Time
	refuseRepeat int
}

// tally adds one batch: its verbs on the beat lane, on the read lane and on the line, the
// verbs not run because the caller went, how long it waited for the line and held it
// (what held it longest, named by its first verb); and says the SERVE line when one is
// due.
func (a *app) tally(beats, reads, serial, gone int, wait, hold time.Duration, first []string) {
	t := &a.served
	t.mu.Lock()
	defer t.mu.Unlock()
	now := a.now()
	if t.since.IsZero() {
		t.since = now
	}
	t.batches++
	t.beats += beats
	t.reads += reads
	t.serialVerbs += serial
	t.gone += gone
	t.waitMax = max(t.waitMax, wait)
	if hold > t.holdMax || t.holdWhat == "" && len(first) > 0 {
		t.holdMax = hold
		t.holdWhat = strings.Join(first[:min(len(first), 3)], "+")
	}
	if a.serveLog == nil || now.Sub(t.since) < ServeSayEvery {
		return
	}
	total, byVerb := refusedCounts(t.refused)
	fmt.Fprintf(a.serveLog, "%s SERVE batches=%d beat-lane=%d read-lane=%d on-line=%d gone=%d refused=%d refused-by=%s wait-max=%s held-max=%s held-by=%s over=%s %s\n",
		now.Format("15:04:05"), t.batches, t.beats, t.reads, t.serialVerbs, t.gone, total,
		oneline.Field(orDashStr(byVerb, "-")),
		t.waitMax.Round(time.Millisecond), t.holdMax.Round(time.Millisecond), oneline.Field(orDashStr(t.holdWhat, "-")), now.Sub(t.since).Round(time.Second),
		tickAgeField(now, t.tickEnd, t.tickDeadline))
	t.since, t.batches, t.beats, t.reads, t.serialVerbs, t.gone, t.waitMax, t.holdMax, t.holdWhat = now, 0, 0, 0, 0, 0, 0, 0, ""
	t.refused = nil
}

// tickEnded records the end of a tick of the run loop and its deadline, for the SERVE
// line's tick-age (a-card-read-never-runs-the-deal; docs/SPEC-SPRINT.md section 14, The
// server, "The tick's deadline").
func (a *app) tickEnded(at time.Time, deadline time.Duration) {
	t := &a.served
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tickEnd, t.tickDeadline = at, deadline
}

// tickAgeField is the SERVE line's tick-age: the time since the last tick ended ("-"
// before the first), with tick=stale when that is more than twice the deadline (no
// deadline: never stale).
func tickAgeField(now, end time.Time, deadline time.Duration) string {
	if end.IsZero() {
		return "tick-age=-"
	}
	age := now.Sub(end)
	s := "tick-age=" + age.Round(time.Second).String()
	if deadline > 0 && age > 2*deadline {
		s += " tick=stale"
	}
	return s
}

// refusedCounts is the refused verbs by verb word as the SERVE line prints them: the total
// and <verb>:<n> joined with commas, sorted so the same tally is always the same words.
func refusedCounts(m map[string]int) (total int, byVerb string) {
	if len(m) == 0 {
		return 0, ""
	}
	keys := slices.Sorted(maps.Keys(m))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		total += m[k]
		parts = append(parts, fmt.Sprintf("%s:%d", k, m[k]))
	}
	return total, strings.Join(parts, ",")
}
