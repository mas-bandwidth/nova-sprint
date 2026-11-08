package store

// Load test, functional tier: the performance bar of docs/LAYER-1-STORE.md
// ("Scale"). Thousands of producers with their consumers and judgements, a
// one-second tick, every worker reporting at once, steady-state reads
// incremental from the event log, a dashboard refreshed once a second. It
// starts its own PostgreSQL (as functional_test.go does) and is skipped
// unless NOVA_LOAD=1, so per-commit CI stays fast. Run it on a Linux bench:
//
//	NOVA_LOAD=1 go test -count=1 -timeout 90m -run TestFunctionalLoad -v ./modules/store/
//
// NOVA_LOAD_SIZES (default "2000,5000") picks the producer counts;
// NOVA_LOAD_FSYNC (default "on", as a real store runs) sets the server's
// fsync. Every measurement is one line:
//
//	LOAD <name> n=<count> p50=<ms> p95=<ms> max=<ms> [more=...]
//
// and every bar one line, BAR <name> PASS|FAIL <why>; a FAIL is also a test
// error.

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

const (
	loadStreams = 20
	loadRows    = 50
	loadWidth   = 40
	loadJudge   = 500
	loadPolicy  = 200
)

func TestFunctionalLoad(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_LOAD") != "1" {
		t.Skip("load test: set NOVA_LOAD=1 to run it (minutes per size, on a Linux bench, never in per-commit CI)")
	}
	sizes := []int{2000, 5000}
	if s := os.Getenv("NOVA_LOAD_SIZES"); s != "" {
		sizes = nil
		for _, f := range strings.Split(s, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(f))
			require.NoError(t, err, "NOVA_LOAD_SIZES")
			sizes = append(sizes, n)
		}
	}
	fsync := cmp.Or(os.Getenv("NOVA_LOAD_FSYNC"), "on")
	for _, n := range sizes {
		t.Run(fmt.Sprintf("producers=%d", n), func(t *testing.T) { runLoad(t, n, fsync) })
	}
}

// loadPG is startPG with the server settings a load run wants: fsync as
// asked, and pg_stat_statements when the server has it, so the run can say
// where the time went.
func loadPG(t *testing.T, fsync string) (dsn string, stats bool) {
	t.Helper()
	bin, dir := pgBin(t), t.TempDir()
	init := exec.Command(filepath.Join(bin, "initdb"), "-D", filepath.Join(dir, "data"), "-U", "nova", "--auth=trust", "-E", "UTF8", "--no-locale")
	init.Env = append(os.Environ(), "LC_ALL=C")
	out, err := init.CombinedOutput()
	require.NoError(t, err, "%s", out)
	port := freePort(t)
	args := []string{"-D", filepath.Join(dir, "data"), "-p", port, "-c", "listen_addresses=127.0.0.1", "-c", "unix_socket_directories=", "-c", "fsync=" + fsync, "-c", "max_connections=200"}
	libs, _ := filepath.Glob(filepath.Join(bin, "..", "lib", "pg_stat_statements.*")) // ignored: a bad pattern is a constant's
	libs2, _ := filepath.Glob(filepath.Join(bin, "..", "lib", "postgresql", "pg_stat_statements.*"))
	if stats = len(libs)+len(libs2) > 0; stats {
		args = append(args, "-c", "shared_preload_libraries=pg_stat_statements", "-c", "pg_stat_statements.track=all")
	}
	// pg_ctl starts it and stops it (-m fast, a clean shutdown), never a signal.
	ctl := filepath.Join(bin, "pg_ctl")
	start := exec.Command(ctl, "-D", filepath.Join(dir, "data"), "-l", filepath.Join(dir, "postgres.log"), "-o", strings.Join(args[2:], " "), "-w", "start")
	start.Env = append(os.Environ(), "LC_ALL=C")
	// Stop is registered before start: a start that fails or times out can
	// leave a postmaster running, and t.Fatalf must not orphan it.
	t.Cleanup(func() {
		stop := exec.Command(ctl, "-D", filepath.Join(dir, "data"), "-m", "fast", "-w", "stop")
		stop.Env = append(os.Environ(), "LC_ALL=C")
		if out, err := stop.CombinedOutput(); err != nil {
			t.Logf("pg_ctl stop: %v\n%s", err, out)
		}
	})
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("pg_ctl start: %v\n%s", err, out)
	}
	dsn = "postgres://nova@127.0.0.1:" + port + "/postgres?sslmode=disable"
	require.Eventually(t, func() bool {
		c, err := pgx.Connect(context.Background(), dsn)
		if err == nil {
			_ = c.Close(context.Background()) // ignored: only probing
		}
		return err == nil
	}, wait, 50*time.Millisecond)
	return dsn, stats
}

// lat is a set of latencies.
type lat []time.Duration

func (l lat) at(q float64) time.Duration {
	if len(l) == 0 {
		return 0
	}
	s := slices.Clone(l)
	slices.Sort(s)
	return s[max(0, min(len(s)-1, int(math.Ceil(q*float64(len(s))))-1))]
}

func (l lat) max() time.Duration { return l.at(1) }

func (l lat) over(d time.Duration) int {
	n := 0
	for _, v := range l {
		if v > d {
			n++
		}
	}
	return n
}

func ms(d time.Duration) string { return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond)) }

// logLoad prints one measurement in the fixed format.
func logLoad(t *testing.T, name string, l lat, more string) {
	t.Helper()
	t.Logf("LOAD %s n=%d p50=%s p95=%s max=%s%s", name, len(l), ms(l.at(0.5)), ms(l.at(0.95)), ms(l.max()), more)
}

// bar prints one bar's outcome and fails the test when it is missed.
func bar(t *testing.T, name string, ok bool, format string, args ...any) {
	t.Helper()
	word := "PASS"
	if !ok {
		word = "FAIL"
	}
	why := fmt.Sprintf(format, args...)
	t.Logf("BAR %s %s %s", name, word, why)
	if !ok {
		t.Errorf("bar missed: %s: %s", name, why)
	}
}

// summary is what a dashboard shows of one event.
type summary struct {
	seq      int64
	op, kind string
	accepted bool
}

// load is one sprint under test: the store, and the planner's view of it (the
// gens and states it plans against, the working consumers per row).
type load struct {
	ctx  context.Context
	p    *PG
	n    int
	ids  atomic.Int64
	text string

	mu      sync.Mutex
	gens    map[string]int
	pstate  map[string]string // producer states
	of      map[string]string // consumer -> producer
	attempt map[string]int    // producer -> its next consumer's attempt
	pool    [][]string        // working consumers per row, oldest first
	rowPick int
	next    int // the next producer a tick moves
	judge   int // judgements opened so far

	// the incremental copy: only its owner touches it (the tick subtest, then
	// the dashboard's incremental reader, then the burst, one at a time).
	local  Snapshot
	cursor int64
	recent []summary // the last 50 events
}

func prodID(i int) string     { return fmt.Sprintf("p%05d", i) }
func streamName(i int) string { return fmt.Sprintf("s%02d", i%loadStreams) }
func rowName(r int) string    { return fmt.Sprintf("r%02d", r) }
func prodIndex(id string) int { n, _ := strconv.Atoi(id[1:]); return n } // ignored: our own ids

func (l *load) op(kind string, expect []Expect, writes ...Write) Op {
	return Op{ID: fmt.Sprintf("%s-%d", kind, l.ids.Add(1)), Epoch: 1, Actor: "load", Kind: kind, At: time.Now(), Expect: expect, Writes: writes} // unitlint: only TestFunctionalLoad builds a *load (the lint matches a method by its name, and harness has an op too)
}

// apply applies op, timing it; a refusal or error is a test error (the load
// is planned to be accepted), and the receipt's gens are kept.
func (l *load) apply(t *testing.T, op Op) (Receipt, time.Duration) {
	start := time.Now()
	r, err := l.p.Apply(l.ctx, op)
	d := time.Since(start)
	switch {
	case err != nil:
		t.Errorf("op %s (%s): %v", op.ID, op.Kind, err)
	case !r.Accepted:
		t.Errorf("op %s (%s) refused: %s", op.ID, op.Kind, r.Reason)
	case r.After != nil:
		l.mu.Lock()
		for id, c := range r.After.Cards {
			l.gens[id] = c.Gen
		}
		l.mu.Unlock()
	}
	return r, d
}

// applyAll applies ops in order with conc goroutines, timing each.
func (l *load) applyAll(t *testing.T, ops []Op, conc int) lat {
	out := make(lat, len(ops))
	next := atomic.Int64{}
	var wg sync.WaitGroup
	for range conc {
		wg.Go(func() {
			for i := int(next.Add(1)) - 1; i < len(ops); i = int(next.Add(1)) - 1 {
				_, out[i] = l.apply(t, ops[i])
			}
		})
	}
	wg.Wait()
	return out
}

func runLoad(t *testing.T, n int, fsync string) {
	dsn, stats := loadPG(t, fsync)
	p := openPG(t, dsn)
	read := connect(t, dsn)
	ctx := context.Background()
	var version string
	require.NoError(t, read.QueryRow(ctx, "SHOW server_version").Scan(&version))
	host, _ := os.Hostname() // ignored: a label
	t.Logf("LOAD-INFO host=%s cpus=%d go=%s postgres=%s fsync=%s producers=%d streams=%d rows=%d width=%d pg_stat_statements=%v",
		host, runtime.NumCPU(), runtime.Version(), version, fsync, n, loadStreams, loadRows, loadWidth, stats)
	if stats {
		if _, err := read.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements"); err != nil {
			t.Logf("LOAD-INFO pg_stat_statements unavailable: %v", err)
			stats = false
		}
	}
	l := &load{ctx: ctx, p: p, n: n, gens: map[string]int{}, pstate: map[string]string{}, of: map[string]string{}, attempt: map[string]int{}, pool: make([][]string, loadRows),
		text: strings.Repeat("A paragraph of the brief as a person wrote it: what to change, where, and why. ", 15)}

	if !t.Run("build", l.build) {
		return
	}
	if stats {
		_, _ = read.Exec(ctx, "SELECT pg_stat_statements_reset()") // ignored: the counts are only a report
	}
	t.Run("tick", l.ticks)
	t.Run("dashboard", func(t *testing.T) { l.dashboard(t, read) })
	t.Run("burst", l.burst)
	t.Run("cold", l.cold)
	l.sizes(t, read)
	l.explain(t, read)
	if stats {
		l.statements(t, read)
	}
}

// build makes the sprint: streams and rows; producers with 1-3 needs among
// earlier cards; policy versions; a work consumer per producer, cut and
// taken (working up to the rows' width, ready after it); open judgements;
// then single-card ops until the log holds at least max(50000, 25n)
// accepted events.
func (l *load) build(t *testing.T) {
	start := time.Now()
	var all lat
	phase := func(name string, ops []Op, conc int) {
		s := time.Now()
		d := l.applyAll(t, ops, conc)
		logLoad(t, "build-"+name, d, fmt.Sprintf(" wall=%sms", ms(time.Since(s))))
		all = append(all, d...)
	}

	var setup []Write
	for s := range loadStreams {
		setup = append(setup, PutStream{Name: streamName(s), Repo: "mas-bandwidth/" + streamName(s), Base: "main", Pos: s})
	}
	var up []Write
	for r := range loadRows {
		setup = append(setup, PutRow{Name: rowName(r), Session: "session-1", Role: "worker"})
		up = append(up, SetRowState{rowName(r), "up"})
	}
	phase("setup", []Op{l.op("setup", nil, setup...), l.op("rows-up", nil, up...)}, 1)

	rng := rand.New(rand.NewPCG(1, uint64(l.n)))
	paths, done := []string{"modules/store/load_test.go", "modules/store/README.md", "docs/LAYER-1-STORE.md"}, []string{"go test ./modules/store/ is green", "the load bar is met"}
	var ops []Op
	var ws []Write
	for i := range l.n {
		var needs []string
		for k := min(i, 1+rng.IntN(3)); len(needs) < k; {
			if j := prodID(i - 1 - rng.IntN(min(i, 100))); !slices.Contains(needs, j) {
				needs = append(needs, j)
			}
		}
		id := prodID(i)
		ws = append(ws, PutCard{ID: id, Kind: "producer", Stream: streamName(i), Score: rng.IntN(1000), Needs: needs, Brief: id + ": " + l.text, Tier: Tiers[i%4], Paths: paths, DoneWhen: done, Prose: l.text[:400]})
		l.pstate[id] = "waiting"
		if len(ws) == 100 || i == l.n-1 {
			ops, ws = append(ops, l.op("add", nil, ws...)), nil
		}
	}
	phase("producers", ops, 1) // in order: needs name earlier cards

	ops = nil
	body := strings.Repeat("rule: deal the highest priority first; ", 50)
	for v := 0; v < loadPolicy; v += 50 {
		for k := range 50 {
			ws = append(ws, PutPolicy{Kind: "data", Name: fmt.Sprintf("policy-%d", v+k+1), Body: body})
		}
		ops, ws = append(ops, l.op("policy", nil, ws...)), nil
	}
	phase("policy", ops, 1)
	phase("policy-activate", []Op{l.op("activate", nil, SetActivePolicy{loadPolicy})}, 1)

	ops = nil
	for i := range l.n {
		prod, row := prodID(i), i%loadRows
		c := "w-" + prod + "-0"
		state := "ready"
		if i < loadRows*loadWidth {
			state = "working"
			l.pool[row] = append(l.pool[row], c)
		}
		l.of[c], l.attempt[prod] = prod, 1
		ws = append(ws, PutCard{ID: c, Kind: "work", Of: prod, Stream: streamName(i), Tier: Tiers[i%4], Holder: rowName(row), Paths: paths, DoneWhen: done, Prose: l.text[:400]}, SetCardState{c, state})
		if len(ws) == 100 || i == l.n-1 {
			ops, ws = append(ops, l.op("cut", nil, ws...)), nil
		}
	}
	phase("consumers", ops, 4)

	ops = nil
	for range loadJudge / 100 {
		for range 100 {
			ws = append(ws, l.judgement())
		}
		ops, ws = append(ops, l.op("judge", nil, ws...)), nil
	}
	phase("judgements", ops, 1)

	target := max(50000, 25*l.n)
	ops = nil
	for k := int(l.ids.Load()); k < target; k++ {
		ops = append(ops, l.op("note", nil, SetCardReason{prodID(k % l.n), fmt.Sprintf("note %d: the worker said where it is and what it is waiting on", k)}))
	}
	phase("filler", ops, 8)
	logLoad(t, "build-total", all, fmt.Sprintf(" wall=%sms events=%d", ms(time.Since(start)), l.ids.Load()))
}

// judgement is the next judgement to open (called with mu held or alone).
func (l *load) judgement() PutJudgement {
	i := l.judge
	l.judge++
	kinds := []string{"red", "fix", "stuck", "held", "flap", "conflict"}
	return PutJudgement{ID: fmt.Sprintf("j%06d", i), Kind: kinds[i%len(kinds)], About: prodID(i % l.n), Question: "The card has failed its read three times: retry, split or drop it?",
		Command: fmt.Sprintf("nova-sprint answer j%06d retry|split|drop", i), Group: fmt.Sprintf("g%03d", i%50)}
}

// takeWorking pops the oldest working consumer of the next row that has one
// (mu held).
func (l *load) takeWorking() (int, string, bool) {
	for range loadRows {
		r := l.rowPick % loadRows
		l.rowPick++
		if len(l.pool[r]) > 0 {
			c := l.pool[r][0]
			l.pool[r] = l.pool[r][1:]
			return r, c, true
		}
	}
	return 0, "", false
}

// report is the writes of a worker's report on consumer c (mu held): done,
// ok, its cost, fenced on its state and gen.
func (l *load) report(c string) ([]Expect, []Write) {
	return []Expect{CardState{c, "working"}, CardGen{c, l.gens[c]}},
		[]Write{SetCardState{c, "done"}, SetCardResult{c, "ok"}, SetCardCost{c, 1000 + int64(len(c)), true}}
}

// cut is the writes that cut and take the next work consumer of c's producer
// on row r (mu held), and the bookkeeping to do once it is accepted.
func (l *load) cut(r int, c string) ([]Write, func()) {
	prod := l.of[c]
	a := l.attempt[prod]
	l.attempt[prod]++
	i := prodIndex(prod)
	nc := fmt.Sprintf("w-%s-%d", prod, a)
	w := []Write{PutCard{ID: nc, Kind: "work", Of: prod, Stream: streamName(i), Tier: Tiers[i%4], Holder: rowName(r), Attempt: a, Prose: l.text[:400]}, SetCardState{nc, "working"}}
	return w, func() {
		l.mu.Lock()
		l.of[nc] = prod
		l.pool[r] = append(l.pool[r], nc)
		l.mu.Unlock()
	}
}

var producerNext = map[string]string{"waiting": "ready", "ready": "working", "working": "review", "review": "merging", "merging": "landed", "landed": "waiting"}

// tickOp is the op a one-second tick makes at this size: 200 producers
// moved, 50 reports applied with 50 consumers cut in their place, 20
// judgements opened; every card fenced on its gen.
func (l *load) tickOp() (Op, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var ex []Expect
	var ws []Write
	var after []func()
	for range 200 {
		p := prodID(l.next % l.n)
		l.next++
		s := producerNext[l.pstate[p]]
		l.pstate[p] = s
		ex = append(ex, CardGen{p, l.gens[p]})
		ws = append(ws, SetCardState{p, s})
	}
	for range 50 {
		r, c, ok := l.takeWorking()
		if !ok {
			break
		}
		e, w := l.report(c)
		cw, done := l.cut(r, c)
		ex, ws, after = append(ex, e...), append(append(ws, w...), cw...), append(after, done)
	}
	for range 20 {
		ws = append(ws, l.judgement())
	}
	return l.op("tick", ex, ws...), func() {
		for _, f := range after {
			f()
		}
	}
}

// reportOp is one worker's report with the run loop's re-deal on its row:
// the steady background load of the dashboard window.
func (l *load) reportOp() (Op, func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, c, ok := l.takeWorking()
	if !ok {
		return Op{}, nil, false
	}
	ex, ws := l.report(c)
	cw, done := l.cut(r, c)
	return l.op("report", ex, append(ws, cw...)...), done, true
}

// catchUp reads the log from the saved cursor until caught up and folds it
// into the local copy, timing the read and the fold apart.
func (l *load) catchUp(t *testing.T) (n int, read, fold time.Duration) {
	for {
		s := time.Now()
		evs, err := l.p.Events(l.ctx, l.cursor, MaxEvents)
		read += time.Since(s)
		if err != nil {
			t.Errorf("events after %d: %v", l.cursor, err)
			return n, read, fold
		}
		s = time.Now()
		l.local = Fold(l.local, evs)
		for _, e := range evs {
			l.recent = append(l.recent, summary{e.Seq, e.Op, e.Kind, e.Accepted})
		}
		if len(l.recent) > 50 {
			l.recent = slices.Clone(l.recent[len(l.recent)-50:])
		}
		fold += time.Since(s)
		n += len(evs)
		if len(evs) > 0 {
			l.cursor = evs[len(evs)-1].Seq
		}
		if len(evs) < MaxEvents {
			return n, read, fold
		}
	}
}

// verify checks the incremental copy against a snapshot: guarantee 5 held
// incrementally.
func (l *load) verify(t *testing.T) {
	snap, err := l.p.Snapshot(l.ctx)
	if err != nil {
		t.Errorf("snapshot: %v", err)
		return
	}
	same := func(what string, a, b any) {
		if !reflect.DeepEqual(a, b) {
			t.Errorf("the incremental copy's %s differ from the snapshot's", what)
		}
	}
	same("cards", l.local.Cards, snap.Cards)
	same("rows", l.local.Rows, snap.Rows)
	same("judgements", l.local.Judgements, snap.Judgements)
	same("streams", l.local.Streams, snap.Streams)
	same("seq", l.local.Seq, snap.Seq)
	same("policy", l.local.Policy, snap.Policy)
}

// ticks is subtests 2 and 4: 20 ticks, each followed by the incremental read
// and fold of what it wrote.
func (l *load) ticks(t *testing.T) {
	snap, err := l.p.Snapshot(l.ctx)
	require.NoError(t, err)
	l.local, l.cursor = snap, snap.Seq
	var tick, read, fold, both lat
	events, writes := 0, 0
	for range 20 {
		op, after := l.tickOp()
		writes = len(op.Writes)
		r, d := l.apply(t, op)
		if r.Accepted {
			after()
		}
		tick = append(tick, d)
		n, rd, fd := l.catchUp(t)
		events += n
		read, fold, both = append(read, rd), append(fold, fd), append(both, rd+fd)
	}
	logLoad(t, "tick-apply", tick, fmt.Sprintf(" writes=%d", writes))
	logLoad(t, "incr-read", read, fmt.Sprintf(" events=%d", events))
	logLoad(t, "incr-fold", fold, "")
	logLoad(t, "incr-total", both, "")
	bar(t, "tick-apply", tick.max() < time.Second, "max %sms against a 1000 ms tick", ms(tick.max()))
	bar(t, "incr-total", both.max() < 100*time.Millisecond, "max %sms per tick's worth against 100 ms", ms(both.max()))
	l.verify(t)
}

// resync rebuilds the planner's view (pool, states, attempts) from the local
// copy, so a probe op that was built and never applied leaves no trace.
func (l *load) resync(t *testing.T) {
	if _, _, _ = l.catchUp(t); t.Failed() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pool = make([][]string, loadRows)
	for id, c := range l.local.Cards {
		l.gens[id] = c.Gen
		switch {
		case c.Kind == "producer":
			l.pstate[id] = c.State
		case c.State == "working":
			r, _ := strconv.Atoi(c.Holder[1:]) // ignored: our own names
			l.pool[r] = append(l.pool[r], id)
		}
		if c.Kind == "work" {
			l.attempt[c.Of] = max(l.attempt[c.Of], c.Attempt+1)
		}
	}
	for r := range l.pool {
		slices.Sort(l.pool[r])
	}
	l.judge = len(l.local.Judgements)
}

// board is what a dashboard refresh computes; the counts are kept so the two
// ways can be compared for size.
type board struct {
	counts, rows, open, merging, events int
}

// boardSQL is a full board read straight from the store with plain SQL on a
// read connection, in one consistent read.
func (l *load) boardSQL(c *pgx.Conn) (board, error) {
	var b board
	tx, err := c.BeginTx(l.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return b, err
	}
	defer func() { _ = tx.Rollback(l.ctx) }() // ignored: read only
	q := func(n *int, sql string, args ...any) error {
		rows, err := tx.Query(l.ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if _, err := rows.Values(); err != nil {
				return err
			}
			*n++
		}
		return rows.Err()
	}
	return b, cmp.Or(
		q(&b.counts, "SELECT stream, state, count(*) FROM sprint.cards WHERE epoch = $1 AND kind = 'producer' GROUP BY stream, state", 1),
		q(&b.rows, `SELECT r.name, r.state, r.held, count(c.id) FROM sprint.rows r
			LEFT JOIN sprint.cards c ON c.epoch = r.epoch AND c.holder = r.name AND c.state = 'working'
			WHERE r.epoch = $1 GROUP BY r.name, r.state, r.held ORDER BY r.name`, 1),
		q(&b.open, "SELECT id, kind, about, question, command, group_key, opened_at FROM sprint.judgements WHERE epoch = $1 AND state = 'open' ORDER BY opened_at, id", 1),
		q(&b.merging, "SELECT stream, id, score, priority FROM sprint.cards WHERE epoch = $1 AND kind = 'producer' AND state = 'merging' ORDER BY stream, score DESC, id", 1),
		q(&b.events, "SELECT seq, op, actor, kind, accepted, reason, at FROM sprint.events ORDER BY seq DESC LIMIT 50"),
	)
}

// boardIncr is the same board the incremental way: the local copy advanced
// from the saved cursor, the figures computed from it in memory.
func (l *load) boardIncr(t *testing.T) (board, int) {
	n, _, _ := l.catchUp(t)
	counts := map[[2]string]int{}
	working := map[string]int{}
	merging := map[string][]Card{}
	for _, c := range l.local.Cards {
		switch {
		case c.Kind == "producer":
			counts[[2]string{c.Stream, c.State}]++
			if c.State == "merging" {
				merging[c.Stream] = append(merging[c.Stream], c)
			}
		case c.State == "working":
			working[c.Holder]++
		}
	}
	rows := make([]Row, 0, len(l.local.Rows))
	for _, r := range l.local.Rows {
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b Row) int { return strings.Compare(a.Name, b.Name) })
	var open []Judgement
	for _, j := range l.local.Judgements {
		if j.State == "open" {
			open = append(open, j)
		}
	}
	slices.SortFunc(open, func(a, b Judgement) int { return cmp.Or(a.OpenedAt.Compare(b.OpenedAt), strings.Compare(a.ID, b.ID)) })
	m := 0
	for _, cs := range merging {
		slices.SortFunc(cs, func(a, b Card) int { return cmp.Or(cmp.Compare(b.Score, a.Score), strings.Compare(a.ID, b.ID)) })
		m += len(cs)
	}
	return board{counts: len(counts), rows: len(rows), open: len(open), merging: m, events: len(l.recent)}, n
}

// dashboard is subtest 7: for 30 seconds a tick each second and a steady
// 200 report ops a second, while a dashboard refreshes the full board once a
// second, both ways.
func (l *load) dashboard(t *testing.T, read *pgx.Conn) {
	const window = 30 * time.Second
	ctx, cancel := context.WithTimeout(l.ctx, window)
	defer cancel()
	var mu sync.Mutex
	var ticks, reports, sqlB, incB lat
	var incEvents int
	var lastSQL, lastInc board
	var dropped atomic.Int64
	var wg sync.WaitGroup
	every := func(d time.Duration, f func()) {
		tk := time.NewTicker(d)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				f()
			}
		}
	}
	wg.Go(func() {
		every(time.Second, func() {
			op, after := l.tickOp()
			r, d := l.apply(t, op)
			if r.Accepted {
				after()
			}
			mu.Lock()
			ticks = append(ticks, d)
			mu.Unlock()
		})
	})
	jobs := make(chan struct{}, 1000)
	wg.Go(func() {
		defer close(jobs)
		every(5*time.Millisecond, func() {
			select {
			case jobs <- struct{}{}:
			default:
				dropped.Add(1)
			}
		})
	})
	for range 16 {
		wg.Go(func() {
			for range jobs {
				if ctx.Err() != nil {
					continue
				}
				op, after, ok := l.reportOp()
				if !ok {
					continue
				}
				r, d := l.apply(t, op)
				if r.Accepted {
					after()
				}
				mu.Lock()
				reports = append(reports, d)
				mu.Unlock()
			}
		})
	}
	wg.Go(func() {
		every(time.Second, func() {
			s := time.Now()
			b, err := l.boardSQL(read)
			d := time.Since(s)
			if err != nil {
				t.Errorf("board by SQL: %v", err)
			}
			mu.Lock()
			sqlB, lastSQL = append(sqlB, d), b
			mu.Unlock()
		})
	})
	wg.Go(func() {
		every(time.Second, func() {
			s := time.Now()
			b, n := l.boardIncr(t)
			d := time.Since(s)
			mu.Lock()
			incB, lastInc, incEvents = append(incB, d), b, incEvents+n
			mu.Unlock()
		})
	})
	wg.Wait()
	logLoad(t, "dash-sql", sqlB, fmt.Sprintf(" over1s=%d board=%+v", sqlB.over(time.Second), lastSQL))
	logLoad(t, "dash-incr", incB, fmt.Sprintf(" over1s=%d events=%d board=%+v", incB.over(time.Second), incEvents, lastInc))
	logLoad(t, "dash-tick", ticks, fmt.Sprintf(" over1s=%d", ticks.over(time.Second)))
	logLoad(t, "dash-report", reports, fmt.Sprintf(" rate=%.0f/s dropped=%d over1s=%d", float64(len(reports))/window.Seconds(), dropped.Load(), reports.over(time.Second)))
	bar(t, "dash-sql", sqlB.over(time.Second) == 0, "%d of %d refreshes over 1 s, max %sms", sqlB.over(time.Second), len(sqlB), ms(sqlB.max()))
	bar(t, "dash-incr", incB.over(time.Second) == 0, "%d of %d refreshes over 1 s, max %sms", incB.over(time.Second), len(incB), ms(incB.max()))
	bar(t, "dash-tick", ticks.over(time.Second) == 0, "%d of %d ticks over 1 s while the board reads, max %sms", ticks.over(time.Second), len(ticks), ms(ticks.max()))
	bar(t, "dash-report-rate", float64(len(reports)) >= 0.9*200*window.Seconds(), "%d report ops in %v against 200/s", len(reports), window)
	l.resync(t)
}

// burst is subtest 3: every worker reporting at once: one goroutine per row,
// each applying a single-consumer report for every working consumer it holds.
func (l *load) burst(t *testing.T) {
	l.mu.Lock()
	work := make([][]string, loadRows)
	for r := range loadRows {
		work[r], l.pool[r] = l.pool[r], nil
	}
	l.mu.Unlock()
	go0 := make(chan struct{})
	per := make([]lat, loadRows)
	var wg sync.WaitGroup
	for r := range loadRows {
		wg.Go(func() {
			<-go0
			for _, c := range work[r] {
				l.mu.Lock()
				ex, ws := l.report(c)
				l.mu.Unlock()
				_, d := l.apply(t, l.op("report", ex, ws...))
				per[r] = append(per[r], d)
			}
		})
	}
	start := time.Now()
	close(go0)
	wg.Wait()
	wall := time.Since(start)
	var all lat
	for _, p := range per {
		all = append(all, p...)
	}
	logLoad(t, "burst", all, fmt.Sprintf(" wall=%sms rate=%.0f/s over1s=%d", ms(wall), float64(len(all))/wall.Seconds(), all.over(time.Second)))
	bar(t, "burst-wall", wall < 5*time.Second, "%d reports cleared in %sms against a few seconds (5 s)", len(all), ms(wall))
	bar(t, "burst-wait", all.max() <= time.Second, "slowest op %sms against 1000 ms", ms(all.max()))
	s := time.Now()
	n, rd, fd := l.catchUp(t)
	logLoad(t, "burst-catchup", lat{time.Since(s)}, fmt.Sprintf(" events=%d read=%sms fold=%sms", n, ms(rd), ms(fd)))
	l.verify(t)
}

// cold is subtest 5: one snapshot of the whole epoch, and a rebuild from the
// whole log.
func (l *load) cold(t *testing.T) {
	s := time.Now()
	snap, err := l.p.Snapshot(l.ctx)
	require.NoError(t, err)
	snapD := time.Since(s)
	logLoad(t, "cold-snapshot", lat{snapD}, fmt.Sprintf(" cards=%d judgements=%d seq=%d", len(snap.Cards), len(snap.Judgements), snap.Seq))
	s = time.Now()
	var all []Event
	for cursor := int64(0); ; {
		evs, err := l.p.Events(l.ctx, cursor, MaxEvents)
		require.NoError(t, err)
		all = append(all, evs...)
		if len(evs) < MaxEvents {
			break
		}
		cursor = evs[len(evs)-1].Seq
	}
	readD := time.Since(s)
	logLoad(t, "cold-events-read", lat{readD}, fmt.Sprintf(" events=%d", len(all)))
	s = time.Now()
	reb := Rebuild(all)
	foldD := time.Since(s)
	logLoad(t, "cold-rebuild-fold", lat{foldD}, fmt.Sprintf(" events=%d", len(all)))
	if !reflect.DeepEqual(reb.Cards, snap.Cards) || reb.Seq != snap.Seq || !reflect.DeepEqual(reb.Judgements, snap.Judgements) {
		t.Errorf("rebuild from the whole log differs from the snapshot")
	}
	bar(t, "cold-snapshot", snapD < 10*time.Second, "snapshot %sms against seconds (10 s)", ms(snapD))
	bar(t, "cold-rebuild", readD+foldD < 10*time.Second, "read %sms + fold %sms against seconds (10 s)", ms(readD), ms(foldD))
}

// sizes logs the size of the store's tables.
func (l *load) sizes(t *testing.T, c *pgx.Conn) {
	var cards, events, cardRows, eventRows int64
	err := c.QueryRow(l.ctx, `SELECT pg_total_relation_size('sprint.cards'), pg_total_relation_size('sprint.events'),
		(SELECT count(*) FROM sprint.cards), (SELECT count(*) FROM sprint.events)`).Scan(&cards, &events, &cardRows, &eventRows)
	if err != nil {
		t.Errorf("sizes: %v", err)
		return
	}
	t.Logf("LOAD-INFO cards=%d rows cards_bytes=%d events=%d rows events_bytes=%d", cardRows, cards, eventRows, events)
}

// explain logs the plans of the queries the hot paths run.
func (l *load) explain(t *testing.T, c *pgx.Conn) {
	var last int64
	_ = c.QueryRow(l.ctx, "SELECT max(seq) FROM sprint.events").Scan(&last) // ignored: 0 is a fine plan input
	for _, q := range []string{
		"SELECT count(*) FROM sprint.cards WHERE epoch = 1 AND holder = 'r07' AND state = 'working'",
		"SELECT id FROM sprint.judgements WHERE epoch = 1 AND state = 'open'",
		"SELECT stream, id, score, priority FROM sprint.cards WHERE epoch = 1 AND kind = 'producer' AND state = 'ready' ORDER BY stream, score DESC, id",
		"SELECT receipt FROM sprint.events WHERE op = 'none'",
		"SELECT coalesce(max(seq), 0) FROM sprint.events",
		fmt.Sprintf("SELECT seq, op, actor, kind, accepted, reason, before, after, receipt, at, epoch, policy FROM sprint.events WHERE seq > %d ORDER BY seq LIMIT 1000", last-1000),
		"SELECT r.name, r.state, r.held, count(c.id) FROM sprint.rows r LEFT JOIN sprint.cards c ON c.epoch = r.epoch AND c.holder = r.name AND c.state = 'working' WHERE r.epoch = 1 GROUP BY r.name, r.state, r.held ORDER BY r.name",
		"SELECT stream, state, count(*) FROM sprint.cards WHERE epoch = 1 AND kind = 'producer' GROUP BY stream, state",
	} {
		rows, err := c.Query(l.ctx, "EXPLAIN (ANALYZE, BUFFERS) "+q)
		if err != nil {
			t.Errorf("explain: %v", err)
			continue
		}
		t.Logf("PLAN %s", q)
		for rows.Next() {
			var line string
			if rows.Scan(&line) == nil {
				t.Logf("PLAN   %s", line)
			}
		}
		rows.Close()
	}
}

// statements logs the statements that took the most server time since the
// build, from pg_stat_statements.
func (l *load) statements(t *testing.T, c *pgx.Conn) {
	rows, err := c.Query(l.ctx, `SELECT calls, total_exec_time, mean_exec_time, max_exec_time, regexp_replace(query, '\s+', ' ', 'g')
		FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 15`)
	if err != nil {
		t.Logf("LOAD-INFO pg_stat_statements: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var calls int64
		var total, mean, mx float64
		var q string
		if rows.Scan(&calls, &total, &mean, &mx, &q) == nil {
			t.Logf("STMT calls=%d total=%.0fms mean=%.3fms max=%.1fms q=%.160s", calls, total, mean, mx, q)
		}
	}
}
