package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/run"
	config "github.com/mas-bandwidth/nova-tools/v2/config/lib"
)

// lazyRoster leaves help and bootstrap usable before the sprint role exists.
type lazyRoster struct {
	mu sync.Mutex
	dsn, password string
	reader *run.ConfigRoster
}

func (l *lazyRoster) ReadRoster(ctx context.Context) (roster.Roster, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reader == nil {
		if l.password == "" { return roster.Roster{}, fmt.Errorf("NOVA_SPRINT_PG_PASSWORD is required for the roster") }
		dsn, err := setupRoleDSN(l.dsn, "nova_sprint")
		if err != nil { return roster.Roster{}, fmt.Errorf("configure a passwordless NOVA_PG_DSN for the roster") }
		conn, err := config.Open(ctx, dsn, l.password)
		if err != nil { return roster.Roster{}, err }
		l.reader, err = run.NewConfigRoster(conn, roster.FleetWidthCap)
		if err != nil { conn.Close(); return roster.Roster{}, err }
	}
	return l.reader.ReadRoster(ctx)
}

func (l *lazyRoster) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reader != nil { l.reader.Close(); l.reader = nil }
}
