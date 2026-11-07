package run

import (
	"context"
	"fmt"
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
	return r.ReadRosterAt(ctx, r.cap)
}

// ReadRosterAt reads the roster at width cap widthCap (the active policy's
// Limits.FleetWidthCap, drift row 26): one config snapshot, one Load.
func (r *ConfigRoster) ReadRosterAt(ctx context.Context, widthCap int) (roster.Roster, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, err := r.reader.Snapshot(ctx)
	if err != nil {
		return roster.Roster{}, err
	}
	return roster.Load(ctx, ConfigRosterSource(snapshot), widthCap)
}

func (r *ConfigRoster) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reader.Close()
}
