package run

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/dash"
	"github.com/mas-bandwidth/nova-sprint/store"
	"github.com/stretchr/testify/require"
)

type failingEvents struct {
	store.Store
	failed atomic.Bool
}

func (s *failingEvents) Events(ctx context.Context, after int64, max int) ([]store.Event, error) {
	if s.failed.Load() {
		return nil, errors.New("events unavailable")
	}
	return s.Store.Events(ctx, after, max)
}

type immediateDashboard struct {
	cache   *dash.Cache
	updates int
}

func (p *immediateDashboard) Offer(s store.Snapshot, at time.Time) {
	_ = p.cache.PublishSnapshot(s, at)
	p.updates++
}
func TestFunctionalDashboardStaleAfterEventFailures(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pg, err := store.Open(ctx, chooserTestPG(t), "")
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	require.NoError(t, pg.Migrate(ctx))
	source := &failingEvents{Store: pg}
	engine, err := NewEngine(ctx, source, &tickGuard{}, &tickSteps{}, &tickEdges{})
	require.NoError(t, err)
	clock := &fakeClock{at: time.Unix(1000, 0)}
	cache := dash.NewFromSnapshot(engine.State(), clock.Now)
	pub := &immediateDashboard{cache: cache}
	sched, err := NewScheduler(engine, clock, &fakePresence{}, pub, &fakeLogger{}, fakeResults{}, fakeDeadline{})
	require.NoError(t, err)
	require.NoError(t, sched.Step(ctx, nil))
	require.Equal(t, 1, pub.updates)
	source.failed.Store(true)
	clock.at = clock.at.Add(33 * time.Second)
	require.Error(t, sched.Step(ctx, nil))
	require.Equal(t, 1, pub.updates)
	rec := httptest.NewRecorder()
	dash.Handler(cache).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	require.Equal(t, 503, rec.Code)
	source.failed.Store(false)
	require.NoError(t, sched.Step(ctx, nil))
	require.Equal(t, 2, pub.updates)
	rec = httptest.NewRecorder()
	dash.Handler(cache).ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	require.Equal(t, 200, rec.Code)
}
