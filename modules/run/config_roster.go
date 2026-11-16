package run

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
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
	pool   *pgxpool.Pool
	reader ConfigSnapshotReader
	cap    int
}

// NewPGXConfigRoster creates a ConfigRoster that reads directly via pgx in REPEATABLE READ (SEAM-CONTRACT 2.5).
func NewPGXConfigRoster(pool *pgxpool.Pool) *ConfigRoster {
	return &ConfigRoster{pool: pool}
}

// NewConfigRoster creates a ConfigRoster from a legacy ConfigSnapshotReader (for tests).
func NewConfigRoster(reader ConfigSnapshotReader, widthCap ...int) (*ConfigRoster, error) {
	if reader == nil {
		return nil, fmt.Errorf("run: config roster needs a reader")
	}
	cap := 8
	if len(widthCap) > 0 && widthCap[0] > 0 {
		cap = widthCap[0]
	}
	return &ConfigRoster{reader: reader, cap: cap}, nil
}

func (r *ConfigRoster) ReadRoster(ctx context.Context) (roster.Roster, error) {
	return r.ReadRosterAt(ctx, r.cap)
}

// ReadRosterAt reads the roster in one REPEATABLE READ transaction, removing FleetWidthCap (R46).
func (r *ConfigRoster) ReadRosterAt(ctx context.Context, widthCap ...int) (roster.Roster, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pool != nil {
		return r.readPGX(ctx)
	}
	if r.reader != nil {
		snapshot, err := r.reader.Snapshot(ctx)
		if err != nil {
			return roster.Roster{}, err
		}
		cap := r.cap
		if len(widthCap) > 0 && widthCap[0] > 0 {
			cap = widthCap[0]
		}
		return roster.Load(ctx, ConfigRosterSource(snapshot), cap)
	}
	return roster.Roster{}, fmt.Errorf("run: no config roster reader configured")
}

func (r *ConfigRoster) readPGX(ctx context.Context) (roster.Roster, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return roster.Roster{}, fmt.Errorf("run: begin config tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var hasV3Cols bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns 
			WHERE table_schema = 'config' AND table_name = 'machines' AND column_name = 'cpu_units'
		)
	`).Scan(&hasV3Cols); err != nil {
		return roster.Roster{}, fmt.Errorf("run: check config.machines schema: %w", err)
	}

	var machines []roster.Machine
	if hasV3Cols {
		rows, qerr := tx.Query(ctx, `SELECT name, roles, os, arch, cpu_units, memory_gb, tags, resources FROM config.machines ORDER BY name`)
		if qerr != nil {
			return roster.Roster{}, fmt.Errorf("run: query config.machines: %w", qerr)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				name, roles, osName, arch, tags string
				cpuUnits, memoryGB              *int
				resJSON                         []byte
			)
			if err := rows.Scan(&name, &roles, &osName, &arch, &cpuUnits, &memoryGB, &tags, &resJSON); err != nil {
				return roster.Roster{}, fmt.Errorf("run: scan config.machines: %w", err)
			}
			var res map[string]int
			if len(resJSON) > 0 {
				if err := json.Unmarshal(resJSON, &res); err != nil {
					return roster.Roster{}, fmt.Errorf("run: parse resources for %s: %w", name, err)
				}
			}
			if res == nil {
				res = make(map[string]int)
			}
			cores := 0
			if cpuUnits != nil {
				cores = *cpuUnits
				res["cpu"] = *cpuUnits
			}
			if memoryGB != nil {
				res["memory"] = *memoryGB
			}
			var tagList []string
			for _, t := range strings.Split(tags, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tagList = append(tagList, t)
				}
			}
			effOS := osName
			if effOS == "darwin" {
				effOS = "mac"
			}
			if effOS != "" && !slices.Contains(tagList, effOS) {
				tagList = append(tagList, effOS)
			}
			if arch != "" && !slices.Contains(tagList, arch) {
				tagList = append(tagList, arch)
			}
			slices.Sort(tagList)

			machines = append(machines, roster.Machine{
				Name:      name,
				Roles:     configList(roles),
				Cores:     cores,
				OS:        osName,
				Arch:      arch,
				Tags:      tagList,
				Resources: res,
			})
		}
		if rows.Err() != nil {
			return roster.Roster{}, rows.Err()
		}
	} else {
		rows, qerr := tx.Query(ctx, `SELECT name, roles, os, arch, cores FROM config.machines ORDER BY name`)
		if qerr != nil {
			return roster.Roster{}, fmt.Errorf("run: query config.machines: %w", qerr)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				name, roles, osName, arch string
				cores                     *int
			)
			if err := rows.Scan(&name, &roles, &osName, &arch, &cores); err != nil {
				return roster.Roster{}, fmt.Errorf("run: scan config.machines: %w", err)
			}
			c := 0
			res := make(map[string]int)
			if cores != nil {
				c = *cores
				res["cpu"] = *cores
				res["memory"] = 1
			}
			var tagList []string
			effOS := osName
			if effOS == "darwin" {
				effOS = "mac"
			}
			if effOS != "" && !slices.Contains(tagList, effOS) {
				tagList = append(tagList, effOS)
			}
			if arch != "" && !slices.Contains(tagList, arch) {
				tagList = append(tagList, arch)
			}
			slices.Sort(tagList)

			machines = append(machines, roster.Machine{
				Name:      name,
				Roles:     configList(roles),
				Cores:     c,
				OS:        osName,
				Arch:      arch,
				Tags:      tagList,
				Resources: res,
			})
		}
		if rows.Err() != nil {
			return roster.Roster{}, rows.Err()
		}
	}

	fRows, ferr := tx.Query(ctx, `SELECT name, harness, model, width, tiers, pronouns, enabled FROM config.friends ORDER BY name`)
	if ferr != nil {
		return roster.Roster{}, fmt.Errorf("run: query config.friends: %w", ferr)
	}
	defer fRows.Close()
	var friends []roster.Friend
	for fRows.Next() {
		var (
			name, harness, model, tiers string
			pronouns                    *string
			width                       int
			enabled                     bool
		)
		if err := fRows.Scan(&name, &harness, &model, &width, &tiers, &pronouns, &enabled); err != nil {
			return roster.Roster{}, fmt.Errorf("run: scan config.friends: %w", err)
		}
		proStr := ""
		if pronouns != nil {
			proStr = *pronouns
		}
		friends = append(friends, roster.Friend{
			Name:     name,
			Harness:  harness,
			Model:    model,
			Width:    width,
			Tiers:    configList(tiers),
			Pronouns: proStr,
			Enabled:  enabled,
		})
	}
	if fRows.Err() != nil {
		return roster.Roster{}, fRows.Err()
	}

	src := rosterSnapshotSource{machines: machines, friends: friends}
	return roster.Load(ctx, src)
}

func (r *ConfigRoster) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pool != nil {
		r.pool.Close()
		r.pool = nil
	}
	if r.reader != nil {
		r.reader.Close()
		r.reader = nil
	}
}
