package run

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/dash"
	"github.com/mas-bandwidth/nova-sprint/store"
)

func TestDashboardPublisherAlarmsWithoutViewer(t *testing.T) {
	clock := &fakeClock{at: time.Unix(1000, 0), ticks: make(chan time.Time, 1)}
	cache := dash.NewFromSnapshot(store.Snapshot{Epoch: 1}, clock.Now)
	clock.at = clock.at.Add(33 * time.Second)
	clock.ticks <- clock.at
	close(clock.ticks)
	var output bytes.Buffer
	log.SetOutput(&output)
	defer log.SetOutput(os.Stderr)
	p := NewDashboardPublisher(cache, clock)
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ALARM stale") || clock.interval != time.Second {
		t.Fatalf("output=%q interval=%s", output.String(), clock.interval)
	}
}
