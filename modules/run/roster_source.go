package run

import (
	"context"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	config "github.com/mas-bandwidth/nova-tools/v2/modules/config"
)

// ConfigRosterSource captures a single already-read config snapshot for both
// roster.Source calls, each friend with its sprint settings from friends
// (roster.FriendSettings of the store's friend:<name> versions; a friend
// with none gets the defaults). The caller invokes Snapshot and handles its
// errors; the config library owns its read-only transaction. This function
// performs no I/O and does not read or retain a config connection.
// An unknown core count maps to zero, which roster.Load rejects for benches.
func ConfigRosterSource(snapshot config.Snapshot, friends map[string]roster.FriendRecord) roster.Source {
	s := rosterSnapshotSource{}
	for _, m := range snapshot.Machines {
		cores := 0
		if m.Cores != nil {
			cores = *m.Cores
		}
		s.machines = append(s.machines, roster.Machine{Name: m.Name, Roles: configList(m.Roles), Routes: configList(m.Routes), Cores: cores, OS: m.OS, Arch: m.Arch})
	}
	for _, f := range snapshot.Friends {
		s.friends = append(s.friends, roster.Friend{Name: f.Name, Harness: f.Harness, Model: f.Model, Pronouns: f.Pronouns, Enabled: f.Enabled}.Settings(friends))
	}
	return s
}

// ConfigPrices is each model's price from its nova-config model record, by
// the record's name (<provider>/<model id>): only a record with both prices
// set; the rest are unpriced, never $0. A cache read is its price_cache_read,
// price_in when that is unset.
func ConfigPrices(snapshot config.Snapshot) map[string]roster.Price {
	out := map[string]roster.Price{}
	for _, m := range snapshot.Models {
		if m.PriceIn != nil && m.PriceOut != nil {
			p := roster.Price{In: int64(*m.PriceIn), CacheRead: int64(*m.PriceIn), Out: int64(*m.PriceOut)}
			if m.PriceCacheRead != nil {
				p.CacheRead = int64(*m.PriceCacheRead)
			}
			out[m.Name] = p
		}
	}
	return out
}

// ConfigConcurrency is each enabled provider's cap on runs at once from its
// nova-config provider record (0, no cap, is left out).
func ConfigConcurrency(snapshot config.Snapshot) map[string]int {
	out := map[string]int{}
	for _, p := range snapshot.Providers {
		if p.Enabled && p.Concurrency > 0 {
			out[p.Name] = p.Concurrency
		}
	}
	return out
}

func configList(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}

type rosterSnapshotSource struct {
	machines []roster.Machine
	friends  []roster.Friend
}

func (s rosterSnapshotSource) Machines(ctx context.Context) ([]roster.Machine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := slices.Clone(s.machines)
	for i := range out {
		out[i].Roles, out[i].Routes = slices.Clone(out[i].Roles), slices.Clone(out[i].Routes)
	}
	return out, nil
}
func (s rosterSnapshotSource) Friends(ctx context.Context) ([]roster.Friend, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := slices.Clone(s.friends)
	for i := range out {
		out[i].Tiers = slices.Clone(out[i].Tiers)
	}
	return out, nil
}
