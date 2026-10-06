package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/oneline"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
)

// The coordinator bring-up (docs/SPEC-SPRINT.md, "The coordinator bring-up").
// start, coordinator, handover and check --bring-up print one line per thing.
// The run loop runs the same check every pass and prints nothing. A line that
// was running and is then missing raises one judgment, once for that change.

const (
	bringUpStateKey = "bring-up-state"
	bringUpWatchKey = "bring-up-watch"
	// bringUpServerKey is store.keyServer. The bring-up reads that record.
	// It does not dial the server.
	bringUpServerKey     = "server"
	bringUpListenDefault = "127.0.0.1:6390"
)

// bringServerRec is the server record as store.SetServerActor writes it.
type bringServerRec struct {
	Actor string    `json:"actor"`
	At    time.Time `json:"at"`
}

type bringReport struct {
	text   string
	items  []sprint.BringUpItem
	states map[string]string
	save   bool
}

func (r bringReport) command(key string) string {
	for _, it := range r.items {
		if it.Key == key {
			return it.Command
		}
	}
	return "nova-sprint check --bring-up"
}

func (a *app) envOf(k string) string {
	if a.getenv == nil {
		return ""
	}
	return strings.TrimSpace(a.getenv(k))
}

func (a *app) bringClock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// printBringUp prints the bring-up and records it. A failure to measure or to
// write a judgment is one line on stderr. The verb that called still succeeds.
func (a *app) printBringUp(ctx context.Context, st *store.Store, stdout, stderr io.Writer) {
	rep, err := a.gatherBringUp(ctx, st)
	if err != nil {
		fmt.Fprintf(stderr, "%s bring-up: %s\n", prog, oneline.Escape(err.Error()))
	}
	fmt.Fprint(stdout, rep.text)
	if rep.save {
		a.judgeBringUp(ctx, st, rep, stderr)
	}
}

// observeBringUp is the run loop's check: the same measurement, no lines,
// and one judgment when a line goes from running to missing.
func (a *app) observeBringUp(ctx context.Context, st *store.Store, stderr io.Writer) {
	rep, err := a.gatherBringUp(ctx, st)
	if err != nil {
		fmt.Fprintf(stderr, "%s bring-up: %s\n", prog, oneline.Escape(err.Error()))
	}
	if rep.save {
		a.judgeBringUp(ctx, st, rep, stderr)
	}
}

func (a *app) gatherBringUp(ctx context.Context, st *store.Store) (bringReport, error) {
	now := a.bringClock()
	holder, herr := st.B.Coordinator(ctx)
	var soft error
	if herr != nil {
		soft = herr
	}
	if holder == "" {
		holder = st.Actor
	}
	if holder == "" {
		holder = "coordinator"
	}

	snap, lerr := st.Load(ctx, []string{sprint.Work, sprint.Readers, sprint.Fleet}, nil)
	if lerr != nil {
		soft = lerr
	}
	var warns []sprint.ReviewWarn
	var readers []sprint.BringUpItem
	if lerr == nil && snap != nil {
		var names []string
		if snap.Readers != nil {
			names = snap.Readers.Rows()
		}
		states, rerr := st.ReaderStates(ctx, names, now)
		if rerr != nil {
			soft = rerr
		} else {
			if states == nil {
				states = map[string]string{}
			}
			snap.ReaderStates = states
			readers = sprint.ReaderLines(snap)
			warns = sprint.ReviewWarnings(snap)
		}
	}

	rows, ferr := st.FriendRows(ctx, now)
	beats, berr := st.FriendBeats(ctx)
	if ferr != nil {
		soft = ferr
	}
	if berr != nil {
		soft = berr
	}
	if ferr == nil && berr == nil {
		slices.SortFunc(rows, func(x, y store.FriendRow) int { return strings.Compare(x.Name, y.Name) })
	} else {
		rows = nil
		beats = map[string]sprint.Beat{}
	}

	coord, cerr := a.bringCoordinator(ctx, st, snap, holder, rows, beats, now)
	if cerr != nil {
		soft = cerr
		coord = sprint.BringUpItem{
			Key: "coordinator-beat", Words: "coordinator-beat", State: sprint.BringUpMissing,
			Extra: "held=no", Command: "nova-sprint friend beat " + holder,
		}
	}
	var friends []sprint.BringUpItem
	if ferr == nil && berr == nil {
		for _, row := range rows {
			state := sprint.BeatState(beats[row.Name], now, sprint.BringUpFriendFresh)
			friends = append(friends,
				sprint.BringUpItem{
					Key: "friend:" + row.Name + ":daemon", Words: "friend " + row.Name + " daemon",
					State: state, Command: "nova-friend install --as " + row.Name,
				},
				sprint.BringUpItem{
					Key: "friend:" + row.Name + ":beat", Words: "friend " + row.Name + " beat",
					State: state, Command: "nova-sprint friend beat " + row.Name,
				},
			)
		}
	}
	dash := a.bringDashboard(ctx, now)

	// server, store, bus, the push, the watch, the coordinator's beat,
	// readers, warnings, friends, the dashboard.
	items := []sprint.BringUpItem{
		a.bringServer(ctx, st, now),
		{Key: "store", Words: "store", State: sprint.BringUpRunning, Command: "nova-sprint where"},
		a.bringBus(ctx, holder),
		a.bringPush(ctx, st, holder, now),
		a.bringWatch(ctx, st, now),
		coord,
	}
	items = append(items, readers...)
	var b strings.Builder
	for _, it := range items {
		b.WriteString(it.Line() + "\n")
	}
	for _, w := range warns {
		b.WriteString(w.Line() + "\n")
	}
	for _, it := range friends {
		b.WriteString(it.Line() + "\n")
	}
	b.WriteString(dash.Line() + "\n")
	items = append(items, friends...)
	items = append(items, dash)
	states := map[string]string{}
	for _, it := range items {
		states[it.Key] = it.State
	}
	return bringReport{text: b.String(), items: items, states: states, save: soft == nil}, soft
}

func (a *app) bringServer(ctx context.Context, st *store.Store, now time.Time) sprint.BringUpItem {
	addr := bringUpListenDefault
	if v := a.envOf(ServerEnv); v != "" {
		addr = v
	}
	it := sprint.BringUpItem{
		Key: "sprint-server", Words: "sprint-server", State: sprint.BringUpMissing,
		Command: "nova-sprint run --listen " + addr,
	}
	kv, ok := st.B.(store.KV)
	if !ok {
		return it
	}
	raw, found, err := kv.GetKey(ctx, bringUpServerKey)
	if err != nil || !found {
		return it
	}
	var rec bringServerRec
	if json.Unmarshal([]byte(raw), &rec) != nil || rec.At.IsZero() {
		return it
	}
	if now.Sub(rec.At) <= store.ServerTTL {
		it.State = sprint.BringUpRunning
	} else {
		it.State = sprint.BringUpStale
	}
	return it
}

func (a *app) bringBus(ctx context.Context, holder string) sprint.BringUpItem {
	it := sprint.BringUpItem{
		Key: "bus", Words: "bus", State: sprint.BringUpMissing,
		Command: "nova-bus peek --as " + holder,
	}
	addr := a.envOf(BusEnv)
	ping := a.outside.ping
	if ping == nil {
		if addr == "" {
			return it // unset, and no test fake: do not dial
		}
		ping = a.realOutside().ping
	}
	if addr == "" {
		addr = "bus"
	}
	if err := ping(ctx, addr); err == nil {
		it.State = sprint.BringUpRunning
	}
	return it
}

func (a *app) bringPush(ctx context.Context, st *store.Store, holder string, now time.Time) sprint.BringUpItem {
	it := sprint.BringUpItem{
		Key: "judgment-push", Words: "judgment-push", State: sprint.BringUpMissing,
		Command: "nova-sprint inbox --wait --push " + pushSeat,
	}
	rec, ok, err := readPush(ctx, st, holder)
	if err != nil || !ok {
		return it
	}
	if sprint.PushLive(rec, true, now) {
		it.State = sprint.BringUpRunning
	} else {
		it.State = sprint.BringUpStale
	}
	return it
}

func (a *app) bringWatch(ctx context.Context, st *store.Store, now time.Time) sprint.BringUpItem {
	it := sprint.BringUpItem{
		Key: "event-watch", Words: "event-watch", State: sprint.BringUpMissing,
		Command: "nova-sprint watch --events",
	}
	kv, ok := st.B.(store.KV)
	if !ok {
		return it
	}
	raw, found, err := kv.GetKey(ctx, bringUpWatchKey)
	if err != nil || !found {
		return it
	}
	at, perr := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if perr != nil {
		return it
	}
	if now.Sub(at) <= sprint.BringUpFresh {
		it.State = sprint.BringUpRunning
	} else {
		it.State = sprint.BringUpStale
	}
	return it
}

func (a *app) bringCoordinator(ctx context.Context, st *store.Store, snap *sprint.Snapshot, holder string, rows []store.FriendRow, beats map[string]sprint.Beat, now time.Time) (sprint.BringUpItem, error) {
	state := sprint.BringUpMissing
	held := false
	cmd := "nova-sprint friend beat " + holder
	friend := false
	for _, row := range rows {
		if row.Name != holder {
			continue
		}
		friend = true
		held = row.Status == sprint.Held
		state = sprint.BeatState(beats[holder], now, sprint.BringUpFriendFresh)
		break
	}
	if !friend && snap != nil && snap.Fleet != nil && slices.Contains(snap.Fleet.Rows(), holder) {
		got, err := st.Beats(ctx, []string{holder})
		if err != nil {
			return sprint.BringUpItem{}, err
		}
		b := got[holder]
		held = sprint.MemberStatus(snap.MemberCtl(holder), b, now) == sprint.Held
		state = sprint.BeatState(b, now, sprint.BringUpFresh)
		cmd = "nova-sprint fleet beat " + holder
	}
	if state == sprint.BringUpRunning && !held {
		cmd = "nova-sprint hold " + holder + " --reason 'the coordinator is not taking cards'"
	}
	yn := "no"
	if held {
		yn = "yes"
	}
	return sprint.BringUpItem{
		Key: "coordinator-beat", Words: "coordinator-beat", State: state,
		Extra: "held=" + yn, Command: cmd,
	}, nil
}

func (a *app) bringDashboard(ctx context.Context, now time.Time) sprint.BringUpItem {
	addr := DashboardListenDefault
	if v := a.envOf(DashboardEnv); v != "" {
		addr = v
	}
	it := sprint.BringUpItem{
		Key: "dashboard", Words: "dashboard", State: sprint.BringUpMissing,
		Extra: "last=-", Command: "nova-sprint dashboard --listen " + addr,
	}
	get := a.outside.httpGet
	if get == nil {
		if a.envOf(DashboardEnv) == "" {
			return it // unset, and no test fake: do not dial
		}
		get = a.realOutside().httpGet
	}
	status, body, err := get(ctx, "http://"+addr+"/api/sprint")
	if err != nil || status < 200 || status >= 300 {
		return it
	}
	var rec struct {
		At time.Time `json:"at"`
	}
	if json.Unmarshal(body, &rec) != nil || rec.At.IsZero() {
		return it
	}
	it.Extra = "last=" + rec.At.UTC().Format(time.RFC3339)
	if now.Sub(rec.At) <= store.MachineSilence {
		it.State = sprint.BringUpRunning
	} else {
		it.State = sprint.BringUpStale
	}
	return it
}

func (a *app) judgeBringUp(ctx context.Context, st *store.Store, rep bringReport, stderr io.Writer) {
	prev, err := loadBringUp(ctx, st)
	if err != nil {
		fmt.Fprintf(stderr, "%s bring-up: %s\n", prog, oneline.Escape(err.Error()))
		return
	}
	fell := sprint.BringUpFell(prev, rep.states)
	if len(fell) == 0 {
		if err := saveBringUp(ctx, st, rep.states); err != nil {
			fmt.Fprintf(stderr, "%s bring-up: %s\n", prog, oneline.Escape(err.Error()))
		}
		return
	}
	who := st.Actor
	if who == "" {
		who = "coordinator"
	}
	notes := make([]sprint.Note, len(fell))
	for i, key := range fell {
		cmd := rep.command(key)
		notes[i] = sprint.Note{
			Kind: sprint.Judgment, Type: sprint.NBringUpDown, SprintLevel: true, Who: who,
			What: key + " went from running to missing; run: " + cmd, Decisions: []string{cmd},
		}
	}
	res, err := st.Run(ctx, store.Step{
		Verb: "bring-up",
		Plan: func(s *sprint.Snapshot) sprint.Plan {
			out := make([]sprint.Note, len(notes))
			for i, n := range notes {
				n.At = s.Now
				out[i] = n
			}
			return sprint.Plan{Notes: out}
		},
	})
	if err != nil || len(res.Refused) > 0 {
		why := ""
		if err != nil {
			why = err.Error()
		} else {
			why = res.Refused[0].Why
		}
		fmt.Fprintf(stderr, "%s bring-up: the judgment was not written: %s\n", prog, oneline.Escape(why))
		return
	}
	if err := saveBringUp(ctx, st, rep.states); err != nil {
		fmt.Fprintf(stderr, "%s bring-up: %s\n", prog, oneline.Escape(err.Error()))
	}
}

func loadBringUp(ctx context.Context, st *store.Store) (map[string]string, error) {
	kv, ok := st.B.(store.KV)
	if !ok {
		return map[string]string{}, nil
	}
	raw, found, err := kv.GetKey(ctx, bringUpStateKey)
	if err != nil || !found || strings.TrimSpace(raw) == "" {
		return map[string]string{}, err
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

func saveBringUp(ctx context.Context, st *store.Store, states map[string]string) error {
	kv, ok := st.B.(store.KV)
	if !ok {
		return nil
	}
	b, err := json.Marshal(states)
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, bringUpStateKey, string(b))
}

func (a *app) cmdCheckBringUp(orig func(*app, []string, io.Writer, io.Writer) int, args []string, stdout, stderr io.Writer) int {
	hasBringUp := false
	var remaining []string
	for _, arg := range args {
		if arg == "--bring-up" || arg == "-bring-up" {
			hasBringUp = true
		} else {
			remaining = append(remaining, arg)
		}
	}
	if !hasBringUp {
		return orig(a, args, stdout, stderr)
	}
	fs, c := a.verbSetup("check")
	pos, err := parse(fs, remaining)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, "check", argErr("takes no words ", err, pos...))
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "check", err.Error())
	}
	a.printBringUp(context.Background(), st, stdout, stderr)
	return 0
}
