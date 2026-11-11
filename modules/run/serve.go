package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/modules/dash"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/bus"
	"github.com/mas-bandwidth/nova-sprint/modules/edge/forge"
	"github.com/mas-bandwidth/nova-sprint/modules/policy"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/steps"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// TickEvery is the tick's period (LAYER-6 section 2: once a second).
const TickEvery = time.Second

// Config is everything run needs, opened by the caller (SEAMS 8): the
// store and the chooser lock's own session, nova-config for the roster,
// the bus and the sprint hook, the policy engine, the git and forge edges,
// and where to serve the board ("" serves none).
type Config struct {
	Store         store.Store
	DSN, Password string
	Roster        *ConfigRoster
	Bus           bus.Bus
	Hook          Hook
	Policy        Asking
	Git           Git
	Forge         forge.Forge
	Listen        string
	Ready         chan<- string // the board's address, once it listens
	Clock         Clock
	Log           io.Writer
	Host          string
	PID           int
	ReposDir      string
	Version       string // this build's release; worker rows behind it are dealt nothing (defect D3)
	// Admit is check's admission of a stream's waiting and ready producers
	// (Admission.Admit); nil proves nothing again after add.
	Admit func(ctx context.Context, snap store.Snapshot, stream string) (map[string]string, error)
}

// Serve is the `run` verb: the chooser lock and the sprint hook, the cold
// start, then a tick every TickEvery until ctx ends or the lock is lost.
func Serve(ctx context.Context, cfg Config) error {
	ch, err := OpenChooser(ctx, cfg.DSN, cfg.Password, cfg.Host, cfg.PID, cfg.Hook)
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close(context.Background()) }()
	return serve(ctx, cfg, ch)
}

// loop is one run's state across ticks.
type loop struct {
	cfg     Config
	ticker  *Ticker
	edges   *Edges
	admit   *Admission
	bases   *BaseWatch
	board   *dash.Cache
	log     *WriterLogger
	mu      sync.Mutex
	streams map[string]store.Stream
}

func serve(ctx context.Context, cfg Config, h Health) error {
	l, err := coldStart(ctx, cfg, h)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	read := make(chan struct{})
	go l.edges.WatchHeads(ctx, cfg.Clock, l.streamsCopy, read)
	served := make(chan error, 1)
	if cfg.Listen != "" {
		go func() { served <- HTTPServer{Address: cfg.Listen, Ready: cfg.Ready}.Serve(ctx, dash.Handler(l.board)) }()
	}
	// No tick before the first heads read is done: a tick before it passes
	// no heads and opens a no-head judgement for every stream with a deal.
	// It reads HeadsAtOnce repositories at once, each bounded by the git
	// edge's own timeouts; a repository it cannot read is said as a heads
	// fault and its streams tick headless.
	select {
	case <-ctx.Done():
		return nil
	case err := <-served:
		return err
	case <-read:
	}
	beat := cfg.Clock.Every(TickEvery)
	defer beat.Stop()
	for n := 1; ; n++ {
		select {
		case <-ctx.Done():
			return nil
		case err := <-served:
			return err
		case <-beat.C():
		}
		if err := l.tick(ctx, n); errors.Is(err, ErrLockLost) {
			return err
		}
	}
}

// coldStart reads the one cold snapshot, seeds the policy's limits, and
// queues each recipient's notices since its cursor.
func coldStart(ctx context.Context, cfg Config, h Health) (*loop, error) {
	now := cfg.Clock.Now()
	t, err := NewTicker(ctx, cfg.Store, h, cfg.Policy, now)
	if err != nil {
		return nil, err
	}
	t.seed(ctx)
	if err := t.push.CatchUp(ctx, cfg.Store, t.snap); err != nil {
		return nil, err
	}
	pause := func(ctx context.Context, d time.Duration) error {
		b := cfg.Clock.Every(d)
		defer b.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.C():
			return nil
		}
	}
	l := &loop{cfg: cfg, ticker: t, log: NewWriterLogger(cfg.Log),
		edges: &Edges{Git: cfg.Git, Forge: cfg.Forge, Bus: cfg.Bus, ReposDir: cfg.ReposDir, Pause: pause},
		board: dash.NewFromFeed(dash.Feed{Snap: t.Snapshot(), Landings: t.Landings(), Stats: t.Stats(), At: now}, cfg.Clock.Now)}
	l.edges.Fault = l.log.EdgeFault
	l.admit = &Admission{Admit: cfg.Admit, Fault: l.log.EdgeFault}
	l.bases = &BaseWatch{Forge: cfg.Forge, Fault: l.log.EdgeFault}
	l.edges.BaseRed = l.bases.LandSawRed
	l.setStreams(t.snap.Streams)
	return l, nil
}

// seed asks the policy once with no questions, so the first roster read
// has the active width cap and the first tick its budget.
func (t *Ticker) seed(ctx context.Context) {
	a := t.asker(ctx, roster.Roster{})
	answers, _ := a.policy.AnswerContext(ctx, a.vs, a.snap, a.ro, policy.Questions{})
	if answers.Version >= 0 {
		t.limits, t.timeouts = answers.Limits, answers.Timeouts
	}
}

// tick is one tick (LAYER-6 section 2): the roster and who is on the bus,
// read once; the Ticker's fold and plan; the edge requests out; the
// envelopes; the board. A failed roster read plans nothing (an empty
// roster would judge every row unknown) but still folds, pushes and shows,
// fails the tick's status and opens the engine judgement (RosterUnread);
// so does a failed who read before any row was ever seen (no row would be
// present, and every ready tier would be judged unserved).
func (l *loop) tick(ctx context.Context, n int) error {
	start := l.cfg.Clock.Now()
	ro, rerr := l.cfg.Roster.ReadRosterAt(ctx, max(1, l.ticker.Limits().FleetWidthCap))
	if rerr == nil {
		l.log.RosterRefused(ro.Refused)
	}
	present, werr := l.present(ctx)
	budget := BudgetDeadline{Duration: func() time.Duration { return time.Duration(l.ticker.Timeouts().Tick) * time.Second }}
	tctx, cancel := budget.Begin(ctx)
	var err error
	if rerr != nil || (werr != nil && !l.ticker.Seen()) {
		err = l.ticker.Fold(tctx)
		if rerr != nil { // logged on the TICK line, status error, and judged
			if err == nil {
				err = l.ticker.RosterUnread(tctx, start, rerr)
			}
			err = errors.Join(err, fmt.Errorf("run: roster read: %w", rerr))
		}
	} else {
		var reqs []steps.EdgeRequest
		reqs, err = l.ticker.Tick(tctx, Observed{Now: start, Present: present, Unread: werr != nil, Roster: ro, Heads: l.edges.Heads(), Results: l.edges.Results(), Version: l.cfg.Version,
			Unadmitted: l.admit.Unadmitted(), Bases: l.bases.Read(), Moves: l.bases.Moves()})
		l.edges.Dispatch(ctx, reqs, l.ticker.snap)
	}
	cancel()
	if errors.Is(err, ErrLockLost) { // nothing more under a lost lock: no push, no board (Run.tla PreparePush)
		l.log.Log(TickMetrics{Number: n, Seq: l.ticker.snap.Seq, Took: l.cfg.Clock.Now().Sub(start), Counts: l.ticker.Counts(), Err: err})
		return err
	}
	for _, r := range l.ticker.Push(ctx, l.cfg.Bus, start) {
		l.log.PushFailed(r.To, r.Err)
	}
	snap := l.ticker.Snapshot()
	l.setStreams(snap.Streams)
	l.admit.Prove(ctx, snap, l.edges.Heads())
	l.bases.Watch(ctx, snap, l.edges.Heads(), start)
	if perr := l.board.Publish(dash.Feed{Snap: snap, Roster: ro.Advertised(snap.Rows, ""), RosterErr: rerr, Landings: l.ticker.Landings(), Stats: l.ticker.Stats(), At: start}); perr != nil && err == nil {
		err = perr
	}
	l.log.Log(TickMetrics{Number: n, Seq: snap.Seq, Took: l.cfg.Clock.Now().Sub(start), Counts: l.ticker.Counts(), Err: err})
	return err
}

// present is who is on the bus, by name; a failed read is no one seen
// this tick, logged. The Ticker holds every row's silence still across a
// failed read (Observed.Unread), so a bus outage deals and judges as the
// last good read did and never counts as rows going silent.
func (l *loop) present(ctx context.Context) ([]string, error) {
	who, err := l.cfg.Bus.Who(ctx)
	if err != nil {
		l.log.EdgeFault("nova-bus who", err)
		return nil, err
	}
	return slices.Sorted(maps.Keys(who)), nil
}

func (l *loop) setStreams(s map[string]store.Stream) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.streams = maps.Clone(s)
}

func (l *loop) streamsCopy() map[string]store.Stream {
	l.mu.Lock()
	defer l.mu.Unlock()
	return maps.Clone(l.streams)
}
