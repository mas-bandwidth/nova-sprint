package run

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mas-bandwidth/nova-sprint/dash"
	"github.com/mas-bandwidth/nova-sprint/store"
)

type ServiceLogger interface {
	TickLogger
	PushFailures
}
type DashboardServer interface {
	Serve(context.Context, http.Handler) error
}
type ServiceDeps struct {
	Store       store.Store
	Chooser     *Chooser
	Clock       Clock
	Presence    Presence
	Steps       Steps
	Edges       TickEdges
	Results     Results
	Deadline    TickDeadline
	Sender      Sender
	Coordinator string
	Logger      ServiceLogger
	Server      DashboardServer
}

// Service shares one folded view with its dashboard and its pusher.
type Service struct {
	engine    *Engine
	scheduler *Scheduler
	dashboard *DashboardPublisher
	pusher    *PushWorker
	cache     *dash.Cache
	chooser   *Chooser
	server    DashboardServer
}

func NewService(ctx context.Context, d ServiceDeps) (*Service, error) {
	if d.Store == nil || d.Chooser == nil || d.Clock == nil || d.Presence == nil || d.Steps == nil || d.Edges == nil || d.Results == nil || d.Deadline == nil || d.Sender == nil || d.Logger == nil || !busName(d.Coordinator) {
		return nil, fmt.Errorf("run: service dependencies and coordinator are required")
	}
	e, err := NewEngine(ctx, d.Store, d.Chooser, d.Steps, d.Edges)
	if err != nil {
		return nil, err
	}
	cache := dash.NewFromSnapshot(e.State(), d.Clock.Now)
	dashboard := NewDashboardPublisher(cache, d.Clock)
	pusher := NewPushWorker(NewPusher(d.Store, EventNotices{Coordinator: d.Coordinator}, d.Sender), d.Coordinator, d.Deadline, d.Logger)
	publisher := servicePublisher{dashboard: dashboard, pusher: pusher}
	scheduler, err := NewScheduler(e, d.Clock, d.Presence, publisher, d.Logger, d.Results, d.Deadline)
	if err != nil {
		return nil, err
	}
	return &Service{engine: e, scheduler: scheduler, dashboard: dashboard, pusher: pusher, cache: cache, chooser: d.Chooser, server: d.Server}, nil
}

type servicePublisher struct {
	dashboard *DashboardPublisher
	pusher    *PushWorker
}

func (p servicePublisher) Offer(s store.Snapshot, at time.Time) {
	p.dashboard.Offer(s, at)
	p.pusher.Offer(s, at)
}

// Handler serves the cached dashboard; it performs no store I/O.
func (s *Service) Handler() http.Handler { return dash.Handler(s.cache) }

// Run stops the workers together and releases the chooser even on failure.
func (s *Service) Run(ctx context.Context) (result error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() {
		if err := s.chooser.Close(context.Background()); err != nil && result == nil {
			result = fmt.Errorf("run: close chooser: %w", err)
		}
	}()
	n := 3
	if s.server != nil {
		n++
	}
	done := make(chan error, n)
	go func() { done <- s.dashboard.Run(ctx) }()
	go func() { done <- s.pusher.Run(ctx) }()
	go func() { done <- s.scheduler.Run(ctx) }()
	if s.server != nil {
		go func() { done <- s.server.Serve(ctx, s.Handler()) }()
	}
	first := <-done
	cancel()
	for i := 1; i < n; i++ {
		<-done
	}
	if errors.Is(first, context.Canceled) {
		return nil
	}
	return first
}
