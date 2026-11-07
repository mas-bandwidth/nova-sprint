package run

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/roster"
	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

// ConfigSnapshotReader owns a run-local config connection. Snapshot itself
// reads machines, friends and services in one read-only transaction.
type ConfigSnapshotReader interface {
	Snapshot(context.Context) (config.Snapshot, error)
	Close()
}

// ConfigRoster serializes reads and closes its owned config connection.
type ConfigRoster struct {
	mu     sync.Mutex
	reader ConfigSnapshotReader
	cap    int
}

func NewConfigRoster(reader ConfigSnapshotReader, widthCap int) (*ConfigRoster, error) {
	if reader == nil || widthCap < 1 {
		return nil, fmt.Errorf("run: config roster needs a reader and positive width cap")
	}
	return &ConfigRoster{reader: reader, cap: widthCap}, nil
}

func (r *ConfigRoster) ReadRoster(ctx context.Context) (roster.Roster, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, err := r.reader.Snapshot(ctx)
	if err != nil {
		return roster.Roster{}, err
	}
	return roster.Load(ctx, ConfigRosterSource(snapshot), r.cap)
}

func (r *ConfigRoster) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reader.Close()
}

func copyRunRoster(r roster.Roster) roster.Roster {
	r.Workers = maps.Clone(r.Workers)
	for name, worker := range r.Workers {
		worker.Tiers = slices.Clone(worker.Tiers)
		r.Workers[name] = worker
	}
	r.Kin = maps.Clone(r.Kin)
	for name, kin := range r.Kin {
		kin.Tiers = slices.Clone(kin.Tiers)
		r.Kin[name] = kin
	}
	return r
}
