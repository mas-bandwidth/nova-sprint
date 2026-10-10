package main

// The server-record fence: the half of one land at a time the clone lock does
// not hold (docs/fixes.sexp, land-fenced-while-server-lands). The clone lock
// (land.go, hold) fences two land passes that reach the same clone; it cannot
// fence a hand land before the clone, against the server whose land loop runs
// in its own process (run --land, landloop.go). So the server's record
// (store.ServerRecord, written on the heartbeat by run's sayServer) carries,
// beside its actor and time, whether the server lands and the process that
// wrote it; a land that is not the server's own land loop reads the record
// before any clone is locked or touched and, while a fresh one says --land, is
// refused naming the server. The server's own pass (landLazy) is never
// refused, a record naming a pid on this host that is gone fences nothing (the
// server died between heartbeats), and land --dry-run reads no record: it
// changes nothing and is never refused by the fence.

import (
	"context"
	"fmt"
	"os"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// serverRecord is the server's record as this process writes it on its
// heartbeat (run's sayServer): its actor, whether it lands, and its pid on its
// host (store.ServerRecord; docs/fixes.sexp, land-fenced-while-server-lands).
func (a *app) serverRecord(actor string) store.ServerRecord {
	host, _ := os.Hostname()
	return store.ServerRecord{Actor: actor, Land: a.serverLands, PID: os.Getpid(), Host: host}
}

// serverLanding is why a land must not run beside the server, "" when it may:
// the server land loop's own pass always may; any other pass is refused while
// a fresh server's record says --land, unless the record names a pid on this
// host that is gone (docs/fixes.sexp, land-fenced-while-server-lands; the
// clone lock, land.go hold, is the other half, and cmdLand reads this fence
// before it).
func (a *app) serverLanding(ctx context.Context, st *store.Store) (string, error) {
	if a.landLazy {
		return "", nil
	}
	r, ok, err := st.Server(ctx)
	if err != nil || !ok || !r.Land {
		return "", err
	}
	if host, _ := os.Hostname(); r.Host == host && r.PID > 0 && !swarm.Alive(r.PID, "") {
		return "", nil
	}
	return fmt.Sprintf("the server is running with --land (actor=%s pid=%d host=%s, its record written %s) and lands every %s itself: nothing was fetched, pushed or reported (let the server land, or restart it without --land); run: nova-sprint where",
		oneline.Field(r.Actor), r.PID, oneline.Field(r.Host), r.At.UTC().Format("15:04:05Z"), LandEvery), nil
}
