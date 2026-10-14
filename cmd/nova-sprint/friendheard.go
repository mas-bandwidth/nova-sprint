package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// A friend's last-heard time (docs/SPEC-SPRINT.md section 1, a friend's health;
// internal/sprint/presence.go FriendPhase; tla/FriendHeard.tla). One time per friend: every
// valid event of hers sets it (Store.FriendHeard), and she is up, deaf or down by its age.
// Three things write it here: a verb a friend's session runs as her (noteHeard, at the one
// place every verb opens the store), the bus store's own record of what she sent and
// received (busEnrollHeard, read by every friend sync pass), and the seat's hand, friend
// heard, for an event neither saw.

// friendEventVerbs are the worker verbs a friend runs as friend.<name>, each a valid event
// of hers: her takes and her reports. Her beat is not one (it is her daemon's; a beat
// carrying an answered check writes the time itself, Store.FriendBeatProof), nor is the
// delivery of her cards (friend cards), nor the queue of her reader.
var friendEventVerbs = map[string]bool{
	"take": true, "finish": true, "progress": true, "read": true, "stop-return": true, "lane take": true, "lane give": true,
}

// noteHeard records the friend a worker verb acts as, when it acts as one: the verb has
// passed the gate that opened the store, so it is a valid packet of hers, and it is recorded
// before the verb runs, so a take by a friend that read down is the event that brings her
// up and is not refused for her silence. A dry run is no event. A name the roster lacks, or a
// store that cannot write, is the verb's own business: nothing is recorded and the verb goes
// on (the record costs her the heard time, never the verb).
func (a *app) noteHeard(ctx context.Context, st *store.Store, c common) {
	if c.dry || !friendEventVerbs[c.verb] {
		return
	}
	name, ok := sprint.FriendOfRow(c.actor)
	if !ok {
		return
	}
	_, _ = st.FriendHeard(ctx, name, a.now()) // ignored: see above
}

// The bus store's heard hashes (nova-bus writes them; pkg/bus): the field is the bus name and
// the value the store's clock at that name's last send, and last recv or ack, RFC3339 UTC
// with milliseconds.
const (
	busHeardSend = "bus2:heard:send"
	busHeardRecv = "bus2:heard:recv"
)

// busEnrollHeard is busEnroll on one connection to the bus store, which also reads the two
// heard hashes: for each of names that appears in either, the later of its last send and its
// last recv. A store with no hash answers none, no error; heardErr is the read that failed
// (the enrolling's own error is err).
func (a *app) busEnrollHeard(ctx context.Context, names ...string) (added []string, heard map[string]time.Time, err, heardErr error) {
	addr := a.getenv(busRedisEnv)
	if addr == "" {
		return nil, nil, nil, nil
	}
	b, closeBus, err := a.busOpen(ctx, addr, a.getenv(busUserEnv))
	if err != nil {
		return nil, nil, err, nil
	}
	if closeBus != nil {
		defer closeBus()
	}
	added, err = b.Enroll(ctx, names...)
	marks, herr := b.Store.Marks(ctx, busHeardSend, busHeardRecv)
	if herr != nil {
		return added, nil, err, herr
	}
	heard = map[string]time.Time{}
	for _, m := range marks {
		for _, n := range names {
			if at, perr := time.Parse(time.RFC3339, m[n]); perr == nil && at.After(heard[n]) {
				heard[n] = at
			}
		}
	}
	return added, heard, err, nil
}

// cmdFriendHeard is the seat's hand for the friend's heard time: friend heard <friend>
// [--at <RFC3339>].
func (a *app) cmdFriendHeard(args []string, stdout, stderr io.Writer) int {
	const name = "friend heard"
	fs, c := a.verbSetup(name)
	at := fs.String("at", "", "when the friend was heard, RFC3339 (default: now); an older time than the record holds changes nothing, and one after the server's clock is the clock now")
	dry := fs.Bool("dry-run", false, "say what would be recorded and record nothing")
	friend, code := oneFriend(name, fs, args, stderr)
	if code != 0 {
		return code
	}
	when := a.now()
	if *at != "" {
		var err error
		if when, err = time.Parse(time.RFC3339, *at); err != nil {
			return refuse(stderr, name, "--at wants an RFC3339 time, found "+oneline.Escape(*at))
		}
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx := context.Background()
	if *dry {
		fmt.Fprintf(stdout, "FRIEND-HEARD DRY-RUN %s at=%s; nothing was changed\n", friend, when.UTC().Truncate(time.Second).Format(time.RFC3339))
		return 0
	}
	heard, err := st.FriendHeard(ctx, friend, when)
	if err != nil {
		fmt.Fprintf(stderr, "%s %s: %s; nothing was changed\n", prog, name, oneline.Escape(err.Error()))
		return 1
	}
	sayOK(stdout, c.json, name, "FRIEND-HEARD OK "+friend+" heard="+heard.Format(time.RFC3339), map[string]any{"friend": friend, "heard": heard})
	return 0
}
