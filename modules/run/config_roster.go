package run

import (
	"context"
	"fmt"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
	config "github.com/mas-bandwidth/nova-tools/v2/modules/config"
)

// ConfigSnapshotReader owns a run-local config connection. Snapshot itself
// reads machines, friends, services, providers and models in one read-only
// transaction.
type ConfigSnapshotReader interface {
	Snapshot(context.Context) (config.Snapshot, error)
	Close()
}

// FriendReader reads the friends' sprint settings: the sprint store's
// newest friend:<name> data versions (store.Store.NewestData).
type FriendReader interface {
	NewestData(ctx context.Context, prefix string) ([]store.PolicyVersion, error)
}

// ConfigRoster serializes reads and closes its owned config connection.
type ConfigRoster struct {
	mu      sync.Mutex
	reader  ConfigSnapshotReader
	friends FriendReader
	cap     int
}

func NewConfigRoster(reader ConfigSnapshotReader, friends FriendReader, widthCap int) (*ConfigRoster, error) {
	if reader == nil || friends == nil || widthCap < 1 {
		return nil, fmt.Errorf("run: config roster needs a reader, a friend settings reader and positive width cap")
	}
	return &ConfigRoster{reader: reader, friends: friends, cap: widthCap}, nil
}

func (r *ConfigRoster) ReadRoster(ctx context.Context) (roster.Roster, error) {
	return r.ReadRosterAt(ctx, r.cap)
}

// ReadRosterAt reads the roster at width cap widthCap (the active policy's
// Limits.FleetWidthCap, drift row 26): one config snapshot, the friends'
// sprint settings, one Load, and the model prices and provider caps the
// snapshot's records give.
func (r *ConfigRoster) ReadRosterAt(ctx context.Context, widthCap int) (roster.Roster, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot, err := r.reader.Snapshot(ctx)
	if err != nil {
		return roster.Roster{}, err
	}
	versions, err := r.friends.NewestData(ctx, roster.FriendPrefix)
	if err != nil {
		return roster.Roster{}, err
	}
	ro, err := roster.Load(ctx, ConfigRosterSource(snapshot, roster.FriendSettings(versions)), widthCap)
	if err != nil {
		return roster.Roster{}, err
	}
	ro.Prices, ro.Concurrency = ConfigPrices(snapshot), ConfigConcurrency(snapshot)
	return ro, nil
}

func (r *ConfigRoster) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reader.Close()
}
