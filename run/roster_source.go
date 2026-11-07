package run

import (
	"context"
	"slices"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/roster"
	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

// ConfigRosterSource captures a single already-read config snapshot for both
// roster.Source calls. The caller invokes Snapshot and handles its errors;
// the config library owns its read-only transaction. This function performs
// no I/O and does not read or retain a config connection.
// An unknown core count maps to zero, which roster.Load rejects for benches.
func ConfigRosterSource(snapshot config.Snapshot) roster.Source {
	s := rosterSnapshotSource{}
	for _, m := range snapshot.Machines {
		cores := 0
		if m.Cores != nil {
			cores = *m.Cores
		}
		s.machines = append(s.machines, roster.Machine{Name: m.Name, Roles: configList(m.Roles), Cores: cores, OS: m.OS, Arch: m.Arch})
	}
	for _, f := range snapshot.Friends {
		s.friends = append(s.friends, roster.Friend{Name: f.Name, Harness: f.Harness, Model: f.Model, Width: f.Width, Tiers: configList(f.Tiers), Pronouns: f.Pronouns, Enabled: f.Enabled})
	}
	return s
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
		out[i].Roles = slices.Clone(out[i].Roles)
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
