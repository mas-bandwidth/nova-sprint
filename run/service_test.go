package run

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/store"
)

type serviceClock struct {
	mu    sync.Mutex
	ticks []chan time.Time
	ready chan struct{}
}

func (c *serviceClock) Now() time.Time { return time.Unix(1000, 0) }
func (c *serviceClock) Every(time.Duration) Ticker {
	ch := make(chan time.Time, 4)
	c.mu.Lock()
	c.ticks = append(c.ticks, ch)
	c.mu.Unlock()
	c.ready <- struct{}{}
	return fakeTicker{ch}
}
func (c *serviceClock) pulse() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.ticks {
		ch <- time.Unix(1000, 0)
	}
}

type serviceLog struct {
	ticks  chan TickMetrics
	failed chan string
}

func (l *serviceLog) Log(m TickMetrics)      { l.ticks <- m }
func (l *serviceLog) PushFailed(name string) { l.failed <- name }

type notifyingStore struct {
	*countStore
	saved chan struct{}
}

func (s *notifyingStore) Apply(ctx context.Context, o store.Op) (store.Receipt, error) {
	r, err := s.countStore.Apply(ctx, o)
	if o.Kind == "push-cursor" && err == nil && r.Accepted {
		select {
		case s.saved <- struct{}{}:
		default:
		}
	}
	return r, err
}

type serviceSender struct{ sent chan string }

func (s *serviceSender) Send(_ context.Context, to, subject, body string) (Delivery, error) {
	s.sent <- to + ":" + subject
	return Delivery{Recipient: to, ID: "1000-1"}, nil
}
func TestServiceThreeTicksAndPush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := store.NewFake()
	counted := &countStore{Store: base}
	source := &notifyingStore{countStore: counted, saved: make(chan struct{}, 1)}
	session := &fakeSession{locked: true}
	lease := &fakeLease{}
	chooser, err := acquireChooser(ctx, session, &fakeHook{lease: lease})
	if err != nil {
		t.Fatal(err)
	}
	clock := &serviceClock{ready: make(chan struct{}, 2)}
	logger := &serviceLog{ticks: make(chan TickMetrics, 4), failed: make(chan string, 4)}
	sender := &serviceSender{sent: make(chan string, 4)}
	service, err := NewService(ctx, ServiceDeps{Store: source, Chooser: chooser, Clock: clock, Presence: &fakePresence{}, Steps: &tickSteps{}, Edges: &tickEdges{}, Results: fakeResults{}, Deadline: fakeDeadline{}, Sender: sender, Coordinator: "rowan", Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	seed := op("deal", store.PutStream{Name: "s"}, store.PutRow{Name: "alice", Kind: "worker", Width: 1, Tiers: []string{"pro"}}, store.PutCard{ID: "p", Kind: "producer", Tier: "pro", Stream: "s"}, store.PutCard{ID: "w", Kind: "work", Of: "p", Tier: "pro", Stream: "s", Holder: "alice"}, store.SetCardState{Card: "w", State: "ready"})
	applyOK(t, base, seed)
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	<-clock.ready
	<-clock.ready
	for i := 0; i < 3; i++ {
		clock.pulse()
		metric := <-logger.ticks
		if metric.Err != nil {
			t.Fatal(metric.Err)
		}
	}
	if got := <-sender.sent; got != "alice:DEAL" {
		t.Fatalf("push=%q", got)
	}
	<-source.saved
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !session.closed || !lease.closed || counted.snapshots != 1 {
		t.Fatalf("session=%v lease=%v snapshots=%d", session.closed, lease.closed, counted.snapshots)
	}
}
