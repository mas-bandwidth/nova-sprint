package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/friend"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

// The seat's push target (docs/SPEC-SPRINT.md, "The push target"): the record of the
// harness the seat runs in and its deliver target, seat push, and the push loop's
// delivery of each judgment through nova-friend's Deliverer or, for a harness with no
// deliver command (Claude Code), one file written into the folder (folderAdapter). The
// proof that the seat reads what is pushed is the hook (hook.go), which replaced the
// push proof's PROOF files and seat pong (the owner, 2026-10-10).

// keySeatPush is name's push record, a key of the sprint's (store.KV).
func keySeatPush(name string) string { return store.SeatPushKey(name) }

// pushTests holds the Deliverer a test's push loop delivers through, by name; the
// binary has none and delivers through the harness's own adapter or the folder adapter.
var pushTests sync.Map // name -> friend.Deliverer

// pushDeliverer is the adapter the push loop delivers into rec's session through: a
// test's, the folder adapter for a record that names it or a harness with no deliver
// command (nova-friend's adapter is passive), else the harness's own.
func pushDeliverer(rec sprint.PushRecord, now func() time.Time) (friend.Deliverer, error) {
	if d, ok := pushTests.Load(rec.Name); ok {
		if d, ok := d.(friend.Deliverer); ok {
			return d, nil
		}
	}
	if rec.Adapter == sprint.AdapterFolder {
		return &folderAdapter{Dir: rec.Target, Now: now}, nil
	}
	d, err := friend.NewDeliverer(rec.Harness, rec.Target, rec.Session, friend.RealExec, nil)
	if err != nil {
		return nil, err
	}
	if passive(d) {
		return &folderAdapter{Dir: rec.Target, Now: now}, nil
	}
	return d, nil
}

// passive says d is the adapter of a harness with no deliver command: nova-friend's
// Stub, or Claude Code's wake file, which puts no turn into a session.
func passive(d friend.Deliverer) bool {
	_, ok := d.(interface{ Passive() })
	return ok
}

// folderAdapter is the seat's adapter for a harness with no deliver command
// (sprint.AdapterFolder): Deliver writes the text as one file into Dir,
// PUSH-<clock>-<n>.md, and answers 0. The file is written under a dot name and renamed,
// so a reader never sees half of one. A Dir that is not a directory is a failure naming
// it, and nothing is made.
type folderAdapter struct {
	Dir string
	Now func() time.Time
}

func (f *folderAdapter) Deliver(_ context.Context, text string) (int, error) {
	if fi, err := os.Stat(f.Dir); err != nil || !fi.IsDir() {
		return 0, fmt.Errorf("the folder adapter writes into %s, and it is not a directory: make it, or install the seat with the folder the session reads", f.Dir)
	}
	name := "PUSH-" + f.Now().UTC().Format("20060102T150405Z") + "-" + pushNonce()[:8] + ".md"
	tmp, err := os.CreateTemp(f.Dir, ".push-*")
	if err != nil {
		return 0, err
	}
	_, err = tmp.WriteString(text)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(f.Dir, name))
	}
	if err != nil {
		_ = os.Remove(tmp.Name()) // ignored: the write already failed, and that is the error said
		return 0, fmt.Errorf("the folder delivery failed: %s", err.Error())
	}
	return 0, nil
}

// sameDir says a and b are one directory.
func sameDir(a, b string) bool {
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}

// readPush is name's push record; ok false when there is none.
func readPush(ctx context.Context, st *store.Store, name string) (sprint.PushRecord, bool, error) {
	var rec sprint.PushRecord
	kv, ok := st.B.(store.KV)
	if !ok || name == "" {
		return rec, false, nil
	}
	raw, ok, err := kv.GetKey(ctx, keySeatPush(name))
	if err != nil || !ok {
		return rec, false, err
	}
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		return rec, false, fmt.Errorf("the push record of %s is not JSON: %w", name, err)
	}
	return rec, true, nil
}

// writePush writes rec as its name's push record.
func writePush(ctx context.Context, st *store.Store, rec sprint.PushRecord) error {
	kv, ok := st.B.(store.KV)
	if !ok {
		return errors.New("this store keeps no keys, and the push record is one")
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if err := kv.SetKey(ctx, keySeatPush(rec.Name), string(b)); err != nil {
		return err
	}
	// the name joins the list teardown deletes the records by
	var names []string
	raw, ok, err := kv.GetKey(ctx, store.KeySeatPushers)
	if err != nil {
		return err
	}
	if ok {
		_ = json.Unmarshal([]byte(raw), &names) // ignored: an unreadable list is written again whole
	}
	if slices.Contains(names, rec.Name) {
		return nil
	}
	l, err := json.Marshal(append(names, rec.Name))
	if err != nil {
		return err
	}
	return kv.SetKey(ctx, store.KeySeatPushers, string(l))
}

// pushNonce is a fresh word for a file name.
func pushNonce() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // ignored: crypto/rand.Read never fails (Go 1.24+)
	return hex.EncodeToString(b)
}

// cmdSeatPush is seat push: name's push record. With --harness and --target it records
// the harness and its deliver target (a harness with no deliver command gets the folder
// adapter, and its target must be a directory); with neither it prints the record.
func (a *app) cmdSeatPush(args []string, stdout, stderr io.Writer) int {
	const name = "seat push"
	fs, c := a.verbSetup(name)
	harness := fs.String("harness", "", "the harness the AI holding the seat runs in: its adapter delivers each push into the session (a harness with no deliver command, claude, gets the folder adapter: each push a file in --target)")
	target := fs.String("target", "", "with --harness, the session's directory, where the adapter delivers (for the folder adapter, a directory that must be there)")
	session := fs.String("session", "", "with --harness, the session's id, for a harness that names one (default: the adapter's newest in --target)")
	dry := fs.Bool("dry-run", false, "check the flags and print what would be recorded, and write nothing")
	if pos, err := parse(fs, args); err != nil || len(pos) > 0 {
		return refuse(stderr, name, argErr("takes no words ", err, pos...))
	}
	if c.actor == "" {
		return refuse(stderr, name, "seat push wants --actor <name> (or NOVA_SPRINT_ACTOR): the seat whose push record it is; nothing was changed")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	ctx := context.Background()
	rec, ok, err := readPush(ctx, st, c.actor)
	if err != nil {
		return a.readFailed(name, err, stderr)
	}
	rec.Name = c.actor
	if *harness != "" {
		next, why := seatPushTarget(sprint.PushRecord{Name: c.actor, Harness: *harness, Target: *target, Session: *session})
		if why != "" {
			return refuse(stderr, name, why)
		}
		if *dry {
			fmt.Fprintf(stdout, "SEAT PUSH DRY-RUN name=%s harness=%s target=%s adapter=%s; nothing was written\n", oneline.Field(next.Name), oneline.Field(next.Harness), oneline.Field(next.Target), oneline.Field(next.AdapterName()))
			return 0
		}
		if err := writePush(ctx, st, next); err != nil {
			return a.readFailed(name, err, stderr)
		}
		rec, ok = next, true
	}
	return a.sayPush(rec, ok, c.json, stdout)
}

// seatPushTarget is rec as it is recorded, with its adapter, and why it may not be, ""
// is may: a name, a known harness and a target; a harness with no deliver command
// (nova-friend's adapter is passive) gets the folder adapter, and its target must be a
// directory that is there.
func seatPushTarget(rec sprint.PushRecord) (sprint.PushRecord, string) {
	rec.Adapter = ""
	if why := sprint.NotPushTarget(rec); why != "" {
		return rec, why
	}
	target, err := filepath.Abs(rec.Target)
	if err != nil {
		return rec, "--target cannot be resolved to an absolute directory: " + err.Error() + "; give an absolute --target <dir>; nothing was written"
	}
	rec.Target = target
	d, err := friend.NewDeliverer(rec.Harness, rec.Target, rec.Session, friend.RealExec, nil)
	if err != nil {
		return rec, err.Error()
	}
	if !passive(d) {
		return rec, ""
	}
	rec.Adapter = sprint.AdapterFolder
	if fi, err := os.Stat(rec.Target); err != nil || !fi.IsDir() {
		return rec, rec.Harness + " has no deliver command, so the push loop writes each judgment as a file into --target, a folder that must be there, and " + rec.Target + " is not a directory; nothing was written"
	}
	return rec, ""
}

// sayPush prints the record: PUSH RECORD, or PUSH NONE with the setup (exit 1).
func (a *app) sayPush(rec sprint.PushRecord, ok bool, asJSON bool, stdout io.Writer) int {
	if asJSON {
		b, _ := json.Marshal(map[string]any{"record": rec, "recorded": ok}) // ignored: a record of strings always encodes
		fmt.Fprintln(stdout, string(b))
	} else if ok {
		fmt.Fprintf(stdout, "PUSH RECORD name=%s harness=%s target=%s adapter=%s\n", oneline.Field(rec.Name), oneline.Field(orDashStr(rec.Harness, "-")), oneline.Field(orDashStr(rec.Target, "-")), oneline.Field(rec.AdapterName()))
	} else {
		fmt.Fprintf(stdout, "PUSH NONE name=%s remedy=%s\n", oneline.Field(rec.Name), oneline.Quote(sprint.PushSetup(rec.Name, rec, ok)))
	}
	if !ok {
		return 1
	}
	return 0
}

// pushProver is a source the push loop reads the holder's push target through. A
// source without it (a test's) delivers nothing into the session.
type pushProver interface {
	pushRecord(ctx context.Context, name string) (sprint.PushRecord, bool, error)
}

func (s *storeSource) pushRecord(ctx context.Context, name string) (sprint.PushRecord, bool, error) {
	return readPush(ctx, s.st, name)
}

func (s *serverSource) pushRecord(ctx context.Context, name string) (sprint.PushRecord, bool, error) {
	res, err := s.a.ask(ctx, s.addr, []string{"seat", "push"}, []string{"--json", "--actor", name})
	if err != nil {
		return sprint.PushRecord{}, false, err
	}
	var out struct {
		Record   sprint.PushRecord `json:"record"`
		Recorded bool              `json:"recorded"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return sprint.PushRecord{}, false, fmt.Errorf("the server's seat push is not JSON (exit %d): %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return out.Record, out.Recorded, nil
}

// pushAnswerBound bounds one delivery into a session.
const pushAnswerBound = 5 * time.Minute

// deliverPush delivers text into rec's session through its adapter, bounded by
// pushAnswerBound: "" when it went in, else why not.
func (a *app) deliverPush(ctx context.Context, rec sprint.PushRecord, text string) string {
	d, err := pushDeliverer(rec, a.now)
	if err != nil {
		return err.Error()
	}
	dctx, cancel := context.WithTimeout(ctx, pushAnswerBound)
	defer cancel()
	exit, err := d.Deliver(dctx, text)
	var deferred friend.Deferred
	switch {
	case errors.As(err, &deferred):
		return deferred.Error()
	case err != nil:
		return err.Error()
	case exit != 0:
		return fmt.Sprintf("%s's deliver command exited %d", rec.Harness, exit)
	}
	return ""
}

// pushJudgments delivers the groups just written for holder into the holder's session
// as one turn, through its adapter: the files are the record of what was pushed, and
// the session is where it is read. A folder adapter whose folder is the holder's inbox
// has them already: the files the loop wrote are the delivery, and nothing is written
// twice.
func (a *app) pushJudgments(ctx context.Context, src inboxSource, holder string, texts []string, asJSON bool, stdout io.Writer) {
	say := pushSayer(asJSON, stdout)
	pr, ok := src.(pushProver)
	if !ok || holder == "" || len(texts) == 0 {
		return
	}
	rec, found, err := pr.pushRecord(ctx, holder)
	if err != nil || !found {
		return
	}
	if inbox, _, ok := a.seatInbox(holder); ok && rec.Adapter == sprint.AdapterFolder && sameDir(rec.Target, inbox) {
		say("OK", holder, "")
		return
	}
	if why := a.deliverPush(ctx, rec, "NOVA SPRINT INBOX: new for the coordinator\n"+strings.Join(texts, "\n")); why != "" {
		say("DOWN", holder, "the judgments were written and not delivered: "+why)
		return
	}
	say("OK", holder, "")
}

// pushSayer is how the push loop says a delivery: PUSH <what> name= [why=] on a line, or
// one JSON object under --json.
func pushSayer(asJSON bool, stdout io.Writer) func(what, name, why string) {
	return func(what, name, why string) {
		if asJSON {
			b, _ := json.Marshal(map[string]any{"push": strings.ToLower(what), "name": name, "why": why}) // ignored: strings always encode
			fmt.Fprintln(stdout, string(b))
			return
		}
		line := "PUSH " + what + " name=" + oneline.Field(name)
		if why != "" {
			line += " why=" + oneline.Quote(why)
		}
		fmt.Fprintln(stdout, line)
	}
}
