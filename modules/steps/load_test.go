package steps

// Load test, functional tier: the tick's cost at scale (docs/LAYER-3-STEPS.md
// "Tests", Load; guarantee 11). 5,000 producers over 100 rows in 20 streams
// on store.Fake for the planning cost (the fold, the plan, the ops count per
// tick for several packing sizes) and on PostgreSQL for the per-op apply
// cost by packing size, which sets pack. Skipped unless NOVA_LOAD=1; run it
// on a quiet Linux bench:
//
//	NOVA_LOAD=1 go test -count=1 -timeout 60m -run TestFunctionalLoad -v ./modules/steps/
//
// Every measurement is one line, LOAD <name> n=<count> p50=<ms> p95=<ms>
// max=<ms> [more=...], every bar one line, BAR <name> PASS|FAIL <why>.

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/pgbin"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	"github.com/stretchr/testify/require"
)

const (
	loadProducers = 5000
	loadRows      = 100
	loadStreams   = 20
)

func TestFunctionalLoad(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_LOAD") != "1" {
		t.Skip("load test: set NOVA_LOAD=1 to run it (minutes, on a Linux bench, never in per-commit CI)")
	}
	s := loadWorld(t)
	s.fold()
	seed := storeSeed(s.v) // the world before any deal, for the PG half
	t.Run("plan", func(t *testing.T) { loadPlan(t, s) })
	t.Run("apply", func(t *testing.T) { loadApply(t, s, seed) })
}

// loadWorld is 5,000 producers in 20 streams over 100 rows, a fifth of them
// ready, a fifth in review with a done read, a tenth merging, the rest
// waiting on a landed need (every tick has work in every step).
func loadWorld(t *testing.T) *sim {
	s := newSim(t)
	for i := 0; i < loadStreams; i++ {
		s.stream(fmt.Sprintf("s%02d", i))
	}
	for i := 0; i < loadRows; i++ {
		s.row(fmt.Sprintf("r%03d", i), 4, "pro", "heavy")
	}
	var writes []store.Write
	for i := 0; i < loadProducers; i++ {
		id := fmt.Sprintf("p%05d", i)
		writes = append(writes, store.PutCard{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%02d", i%loadStreams), Tier: "pro", Brief: "do " + id, Paths: []string{fmt.Sprintf("a/%05d", i)}, Score: i})
		if len(writes) == 500 {
			s.must("seed", writes...)
			writes = nil
		}
	}
	s.must("seed", writes...)
	s.run(1) // every producer resolves (budget permitting: the default is 250 a tick)
	return s
}

// loadPlan times the fold and the plan at steady state over 20 ticks, with
// the ops count per tick for pack 1, 10, 50 and 250.
func loadPlan(t *testing.T, s *sim) {
	var folds, plans lat
	ops := map[int]int{}
	for i := 0; i < 20; i++ {
		start := time.Now()
		s.fold()
		folds = append(folds, time.Since(start))
		in := s.inputs()
		for _, pack := range []int{1, 10, 50, 250} {
			ask := scripted{Answers{Version: 1, Models: benchModels(), Limits: &Limits{Bounces: 6, Faults: 3, SameFinding: 2, ReportsPerTick: 1000, RouteRestWindow: 10, RouteRestAfter: 3, EscalateAfter: 2, FrontierAfter: 4, FlapLimit: 3, IdleFloor: 0.5, LandTarget: 0.9, Budget: 250, Pack: pack}}}
			start = time.Now()
			plan := Tick(s.v, in, ask)
			d := time.Since(start)
			if pack == 50 {
				plans = append(plans, d)
			}
			ops[pack] += len(plan.Ops)
		}
		s.tick()
	}
	logLoad(t, "fold", folds, "")
	logLoad(t, "plan", plans, fmt.Sprintf(" ops/tick pack1=%d pack10=%d pack50=%d pack250=%d", ops[1]/20, ops[10]/20, ops[50]/20, ops[250]/20))
	bar(t, "plan-under-50ms", plans.at(0.5) < 50*time.Millisecond, "plan p50 %s", ms(plans.at(0.5)))
}

// loadApply measures the per-op apply cost on PostgreSQL by packing size:
// the same tick (the seeded world's first) as 250 ops of 1, 25 of 10, 5 of
// 50 and 1 of 250, each on a fresh store.
func loadApply(t *testing.T, s *sim, seed []store.Op) {
	ctx := context.Background()
	for _, pack := range []int{1, 10, 50, 250} {
		p, err := store.Open(ctx, loadPG(t), "")
		require.NoError(t, err)
		t.Cleanup(p.Close)
		require.NoError(t, p.Migrate(ctx))
		for _, w := range seed {
			r, err := p.Apply(ctx, w)
			require.NoError(t, err)
			require.True(t, r.Accepted, r.Reason)
		}
		snap, err := p.Snapshot(ctx)
		require.NoError(t, err)
		v := Build(snap, t0)
		plan := Tick(v, s.inputs(), scripted{Answers{Version: 1, Models: benchModels(), Limits: &Limits{Bounces: 6, Faults: 3, SameFinding: 2, ReportsPerTick: 1000, RouteRestWindow: 10, RouteRestAfter: 3, EscalateAfter: 2, FrontierAfter: 4, FlapLimit: 3, IdleFloor: 0.5, LandTarget: 0.9, Budget: 250, Pack: pack}}})
		var ls lat
		total := time.Now()
		for _, op := range tickOnly(plan.Ops) {
			start := time.Now()
			r, err := p.Apply(ctx, op)
			ls = append(ls, time.Since(start))
			require.NoError(t, err)
			require.True(t, r.Accepted, "pack %d: %s", pack, r.Reason)
		}
		logLoad(t, fmt.Sprintf("apply-pack%d", pack), ls, fmt.Sprintf(" ops=%d tick=%s", len(ls), ms(time.Since(total))))
	}
}

// storeSeed is the fake's state as ops for a fresh PG store.
func storeSeed(v *View) []store.Op {
	var ops []store.Op
	n := 0
	op := func(ws ...store.Write) {
		n++
		ops = append(ops, store.Op{ID: fmt.Sprintf("seed-%d", n), Epoch: 1, Actor: "load", Kind: "seed", At: time.Now(), Writes: ws})
	}
	for _, s := range sortedKeys(v.Streams) {
		st := v.Streams[s]
		op(store.PutStream{Name: st.Name, Repo: st.Repo, Base: st.Base, Pos: st.Pos})
	}
	for _, r := range sortedKeys(v.Rows) {
		row := v.Rows[r]
		op(store.PutRow{Name: row.Name, Session: row.Session, Role: row.Role}, store.SetRowState{Row: row.Name, State: "up"})
	}
	var ws []store.Write
	for _, id := range sortedKeys(v.Cards) {
		c := v.Cards[id]
		if isConsumer(c) {
			continue
		}
		ws = append(ws, store.PutCard{ID: c.ID, Kind: c.Kind, Stream: c.Stream, Tier: c.Tier, Brief: c.Brief, Paths: c.Paths, Score: c.Score})
		if c.State == "ready" {
			ws = append(ws, store.SetCardState{Card: c.ID, State: "ready"})
		}
		if len(ws) >= 400 {
			op(ws...)
			ws = nil
		}
	}
	if len(ws) > 0 {
		op(ws...)
	}
	return ops
}

// loadPG starts a throwaway PostgreSQL (as store's functional tests do).
func loadPG(t *testing.T) string {
	t.Helper()
	bin := pgbin.Dir(t)
	dir := t.TempDir()
	init := exec.Command(filepath.Join(bin, "initdb"), "-D", filepath.Join(dir, "data"), "-U", "nova", "--auth=trust", "-E", "UTF8", "--no-locale")
	init.Env = append(os.Environ(), "LC_ALL=C")
	out, err := init.CombinedOutput()
	require.NoError(t, err, "%s", out)
	port := fmt.Sprint(54000 + os.Getpid()%1000)
	// pg_ctl starts it and stops it (-m immediate: the postmaster stops its
	// children before it exits, so none writes into the removed TempDir).
	ctl := filepath.Join(bin, "pg_ctl")
	start := exec.Command(ctl, "-D", filepath.Join(dir, "data"), "-l", filepath.Join(dir, "postgres.log"), "-o", "-p "+port+" -c listen_addresses=127.0.0.1 -c unix_socket_directories= -c fsync=on", "-w", "start")
	start.Env = append(os.Environ(), "LC_ALL=C")
	if out, err := start.CombinedOutput(); err != nil {
		t.Fatalf("pg_ctl start: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		stop := exec.Command(ctl, "-D", filepath.Join(dir, "data"), "-m", "immediate", "-w", "stop")
		stop.Env = append(os.Environ(), "LC_ALL=C")
		if out, err := stop.CombinedOutput(); err != nil {
			t.Logf("pg_ctl stop: %v\n%s", err, out)
		}
	})
	dsn := "postgres://nova@127.0.0.1:" + port + "/postgres?sslmode=disable"
	require.Eventually(t, func() bool {
		c, err := pgx.Connect(context.Background(), dsn)
		if err == nil {
			_ = c.Close(context.Background())
		}
		return err == nil
	}, 30*time.Second, 50*time.Millisecond)
	return dsn
}

type lat []time.Duration

func (l lat) at(q float64) time.Duration {
	if len(l) == 0 {
		return 0
	}
	s := slices.Clone(l)
	slices.Sort(s)
	return s[max(0, min(len(s)-1, int(math.Ceil(q*float64(len(s))))-1))]
}

func ms(d time.Duration) string { return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond)) }

func logLoad(t *testing.T, name string, l lat, more string) {
	t.Helper()
	t.Logf("LOAD %s n=%d p50=%s p95=%s max=%s%s", name, len(l), ms(l.at(0.5)), ms(l.at(0.95)), ms(l.at(1)), more)
}

func bar(t *testing.T, name string, ok bool, format string, args ...any) {
	t.Helper()
	word := "PASS"
	if !ok {
		word = "FAIL"
	}
	t.Logf("BAR %s %s %s", name, word, fmt.Sprintf(format, args...))
	if !ok {
		t.Errorf("bar missed: %s", name)
	}
}

// TestFunctionalLoadResolve times the needs step alone (section 64b: the tick resolves
// from the state, every tick) on 5,000 waiting producers whose needs a store
// op landed (no land step, no edge result): all 5,000 resolve. The view is
// not applied between the timed ticks, so every tick is the same worst case.
func TestFunctionalLoadResolve(t *testing.T) {
	t.Parallel()
	if os.Getenv("NOVA_LOAD") != "1" {
		t.Skip("load test: set NOVA_LOAD=1 to run it (on a Linux bench, never in per-commit CI)")
	}
	timeResolve(t, "free", resolveWorld(t))
}

func timeResolve(t *testing.T, name string, s *sim) {
	s.fold()
	var needs, plans lat
	resolved, judged := 0, 0
	for i := 0; i < 20; i++ {
		tk := newTick(s.v, s.inputs())
		tk.o = newOracle(s.ask.Ask(tk.questions()), Engine)
		tk.lim, tk.tmo = tk.o.limits(), tk.o.timeouts()
		start := time.Now()
		tk.needs()
		needs = append(needs, time.Since(start))
		resolved, judged = len(tk.resolved), len(tk.judges)
		start = time.Now()
		Tick(s.v, s.inputs(), s.ask)
		plans = append(plans, time.Since(start))
	}
	open := 0
	for _, j := range s.v.Judgements {
		if j.State == "open" {
			open++
		}
	}
	logLoad(t, "resolve-"+name, needs, fmt.Sprintf(" waiting=%d resolved=%d newly-judged=%d open-judgements=%d", len(s.v.Producers("waiting")), resolved, judged, open))
	logLoad(t, "plan-"+name, plans, "")
	bar(t, "resolve-"+name+"-under-50ms", needs.at(0.5) < 50*time.Millisecond, "needs p50 %s", ms(needs.at(0.5)))
}

// resolveWorld is 100 landed producers (landed by a store op) and 5,000
// producers waiting on one of them each, in 20 streams over 100 rows.
func resolveWorld(t *testing.T) *sim {
	s := newSim(t)
	for i := 0; i < loadStreams; i++ {
		s.stream(fmt.Sprintf("s%02d", i))
	}
	for i := 0; i < loadRows; i++ {
		s.row(fmt.Sprintf("r%03d", i), 4, "pro", "heavy")
	}
	var writes []store.Write
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("n%03d", i)
		writes = append(writes, store.PutCard{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%02d", i%loadStreams), Tier: "pro", Brief: "do " + id, Paths: []string{"n/" + id}})
	}
	s.must("seed needs", writes...)
	writes = nil
	for i := 0; i < 100; i++ {
		writes = append(writes, store.SetCardState{Card: fmt.Sprintf("n%03d", i), State: "landed"})
	}
	s.must("land by hand", writes...)
	writes = nil
	for i := 0; i < loadProducers; i++ {
		id := fmt.Sprintf("p%05d", i)
		writes = append(writes, store.PutCard{ID: id, Kind: "producer", Stream: fmt.Sprintf("s%02d", i%loadStreams), Tier: "pro", Brief: "do " + id, Paths: []string{fmt.Sprintf("a/%05d", i)}, Needs: []string{fmt.Sprintf("n%03d", i%100)}, Score: i})
		if len(writes) == 500 {
			s.must("seed", writes...)
			writes = nil
		}
	}
	s.must("seed", writes...)
	return s
}
