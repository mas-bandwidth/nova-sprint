package verbs

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/modules/edge"

	"github.com/mas-bandwidth/nova-sprint/modules/run"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// runStop is `stop` and `start`: the tick stopped or started, one write.
func runStop(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	stop, reason := in.verb == "stop", in.str("reason")
	if stop && reason == "" {
		return "", refuse("a stop needs its reason", "add --reason \"<why>\"")
	}
	label, next := "STARTED", waitNext
	if stop {
		label, next = "STOPPED", "nova-sprint start --as "+in.actor+" --reason \"<why>\""
	}
	if snap.Stopped == stop {
		return alreadyStopped(snap, in.actor), nil
	}
	op := e.newOp(in.verb, in.actor, snap, reason)
	op.Expect = []store.Expect{store.UnchangedSince{Seq: snap.Seq, Stop: true}} // no other stop or start since the read
	op.Writes, op.Note = []store.Write{store.SetStopped{Stopped: stop, Reason: reason}}, reason
	r, err := e.commit(ctx, op, "nova-sprint show run")
	if err != nil {
		return "", err
	}
	return done(next, fmt.Sprintf("%s applied at seq %d", label, r.Seq)), nil
}

// alreadyStopped is stop on a stopped sprint, or start on a running one: a
// success that changes nothing, saying the state and the next verb (defect
// 23: start on a running sprint printed "STARTED already").
func alreadyStopped(snap store.Snapshot, actor string) string {
	if snap.Stopped {
		return done("nova-sprint start --as "+actor+" --reason \"<why>\"", "STOPPED already: the sprint is already stopped ("+orWord(snap.StopReason, "no reason given")+"); nothing changed")
	}
	return done("nova-sprint next --as "+actor, "RUNNING already: the sprint is already running; nothing to start")
}

// atLeast compares releases vMAJOR.MINOR.PATCH; a prerelease or a
// malformed version is an error, never a pass.
func atLeast(have, pin string) (bool, error) {
	a, err := releaseVersion(have)
	if err != nil {
		return false, err
	}
	b, err := releaseVersion(pin)
	if err != nil {
		return false, err
	}
	return slices.Compare(a[:], b[:]) >= 0, nil
}

func releaseVersion(s string) ([3]uint64, error) {
	var out [3]uint64
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return out, fmt.Errorf("invalid version: %s", edge.ParseRefused(fmt.Sprintf("%d dot-separated parts, want 3 (vMAJOR.MINOR.PATCH)", len(parts)), 0, []byte(s)))
	}
	at := len(s) - len(strings.TrimPrefix(s, "v"))
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return out, fmt.Errorf("invalid version: %s", edge.ParseFailure(err, []byte(s)))
		}
		if strings.Trim(p, "0123456789") != "" || (len(p) > 1 && p[0] == '0') {
			return out, fmt.Errorf("invalid version: %s", edge.ParseRefused(fmt.Sprintf("part %q is not a number without leading zeros", p), at, []byte(s)))
		}
		out[i] = n
		at += len(p) + 1
	}
	return out, nil
}

func runVersion(e *Engine, _ context.Context, _ *input, _ store.Snapshot) (string, error) {
	return done("nova-sprint help", VersionLine(e.Version)), nil
}

// runRun, runWorker and runDash are the long-running verbs, the binary's.
func runRun(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if e.Services == nil {
		return "", broken("run is not configured in this build", "nova-sprint check")
	}
	if err := e.Services.Run(ctx, in.str("listen")); err != nil {
		return "", err
	}
	return done("nova-sprint run", "RUN ended"), nil
}

func runWorker(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if e.Services == nil {
		return "", broken("worker is not configured in this build", "nova-sprint check")
	}
	if !validName(in.actor) {
		return "", misuse("worker", "worker takes --as <row>")
	}
	w := WorkerArgs{Row: in.actor, Root: in.str("root"), StateDir: in.str("state-dir"), Harness: in.str("harness"),
		WorkRoot: in.str("work-root"), MirrorRoot: in.str("mirror-root"), Providers: in.str("providers"), WallReads: in.list("wall-read")}
	if err := e.Services.Worker(ctx, w); err != nil {
		return "", err
	}
	return done("nova-sprint worker --as "+in.actor, "WORKER "+in.actor+" ended"), nil
}

func runDash(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if e.Services == nil {
		return "", broken("dash is not configured in this build", "nova-sprint check")
	}
	if in.str("listen") == "" || in.str("pull") == "" {
		return "", misuse("dash", "dash takes --listen <addr> --pull <url>")
	}
	if err := e.Services.Dash(ctx, in.str("listen"), in.str("pull")); err != nil {
		return "", err
	}
	return done("nova-sprint dash --listen "+in.str("listen"), "DASH ended"), nil
}

// runBackup is `backup`: a dump verified by a throwaway restore.
func runBackup(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if e.Archive == nil {
		return "", broken("backup is not configured", "set NOVA_PG_DSN, then nova-sprint check")
	}
	info, err := e.Archive.Backup(ctx, in.str("out"))
	if err != nil {
		return "", err
	}
	return done(waitNext, fmt.Sprintf("BACKUP %s epoch=%d seq=%d verified", info.Path, info.Epoch, info.Seq)), nil
}

// runRestore is `restore` (cold: the store it fills has no rows): the dump
// verified, loaded under the chooser lock with its digest, and the caller
// hooked in as coordinator. The same dump again only hooks; another dump's
// data refuses.
func runRestore(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if e.Archive == nil || e.Store == nil {
		return "", broken("restore is not configured", "set NOVA_PG_DSN, then nova-sprint check")
	}
	if err := e.onBus(ctx, in.actor); err != nil {
		return "", err
	}
	info, err := e.Archive.Verify(ctx, in.arg(0))
	if err != nil {
		return "", err
	}
	digest := info.Verified.Digest()
	have, _, restored, err := e.Store.Restored(ctx)
	if err != nil {
		return "", err
	}
	if restored && have != digest {
		return "", refuse("the store holds another dump's data", "nova-sprint clear --confirm <epoch>, then restore into an empty store")
	}
	if !restored {
		if err := e.load(ctx, info); err != nil {
			return "", err
		}
	}
	text, err := e.restoreHook(ctx, in.actor)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("RESTORED %s epoch=%d seq=%d digest=%s\n", info.Path, info.Epoch, info.Seq, digest) + text + e.restoreAdmission(ctx), nil
}

// restoreAdmission holds the restored producers not yet dealt (waiting or
// ready) to today's admission (checkCards), one ADMIT line each. The data is
// in and the restore stands (a recovery refused for a moved path would leave
// no store at all); a red line names the card and its fix, and check repeats
// it, red, until it is fixed.
func (e *Engine) restoreAdmission(ctx context.Context) string {
	r := &checkReport{}
	if snap, err := e.snapshot(ctx); err != nil {
		r.red(RefusalOr(err, broken("the restored store cannot be read for admission ("+errText(err)+")", "nova-sprint check")))
	} else {
		e.checkCards(ctx, r, snap)
	}
	var b strings.Builder
	for _, line := range r.lines {
		b.WriteString("ADMIT " + strings.TrimPrefix(line, "CHECK ") + "\n")
	}
	return b.String()
}

// load runs the verified dump under the chooser lock, in its session.
func (e *Engine) load(ctx context.Context, info BackupInfo) error {
	lease, err := e.Archive.AcquireRestore(ctx)
	if err != nil {
		return err
	}
	defer lease.Close(context.WithoutCancel(ctx))
	if err := lease.Check(ctx); err != nil {
		return err
	}
	return lease.Restore(ctx, info.Verified.Digest(), func(w run.RestoreWriter) error { return info.Verified.Execute(ctx, w) })
}

// restoreHook is the restorer hooked in as coordinator, unless the dump
// already has it so.
func (e *Engine) restoreHook(ctx context.Context, actor string) (string, error) {
	snap, err := e.snapshot(ctx)
	if err != nil {
		return "", err
	}
	if r := snap.Rows[actor]; r.Session != "" && r.Role == "coordinator" {
		return done("nova-sprint next --as "+actor, "HOOKED "+actor+" role=coordinator (as restored)"), nil
	}
	return e.hookAs(ctx, snap, actor, "coordinator")
}

// runClear is `clear`: after a verified backup, the epoch ends and every
// card goes. Live consumers refuse: release them first. The clear is fenced
// on every event since its snapshot writing rows only (RowsOnlySince, inside
// the clear's own transaction), never on the store's seq: workers beat
// faster than a read and a commit (2026-11-09: refused "the store is at seq
// N+1, expected seq N" on every try), and a row is carried into the new
// epoch, so the backup still holds everything the clear discards.
func runClear(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	n, f := in.num("confirm")
	if f != nil || n != snap.Epoch {
		return "", refuse(fmt.Sprintf("clear confirms the epoch it ends, %d", snap.Epoch), fmt.Sprintf("nova-sprint clear --confirm %d", snap.Epoch))
	}
	var busy []store.Card
	for _, c := range snap.Cards {
		if live(c) {
			busy = append(busy, c)
		}
	}
	if len(busy) > 0 {
		slices.SortFunc(busy, func(a, b store.Card) int { return strings.Compare(a.ID, b.ID) })
		return "", refuse("live consumers: "+idList(busy), "release each: nova-sprint release <card>@<gen> --reason \"<why>\"")
	}
	what := fmt.Sprintf("CLEAR epoch=%d cards=%d", snap.Epoch, len(snap.Cards))
	if in.has("dry-run") {
		return done(fmt.Sprintf("nova-sprint clear --confirm %d", snap.Epoch), what+" (dry run)"), nil
	}
	if e.Archive == nil {
		return "", broken("backup is not configured, and clear needs one", "nova-sprint check")
	}
	info, err := e.Archive.Backup(ctx, "")
	if err != nil {
		return "", err
	}
	again := fmt.Sprintf("nova-sprint clear --confirm %d again", snap.Epoch)
	if info.Epoch != snap.Epoch || info.Seq < snap.Seq {
		return "", refuse("the store changed while the backup was made", again)
	}
	op := e.newOp("clear", in.actor, snap)
	op.Expect = []store.Expect{store.RowsOnlySince{Seq: snap.Seq}}
	r, err := applyOnce(ctx, e.Store, op, e.Store.Clear)
	if err != nil {
		return "", err
	}
	if !r.Accepted {
		return "", refuse(r.Reason, again)
	}
	return done("nova-sprint stream add <name> --repo <org/repo>", what, fmt.Sprintf("CLEARED epoch=%d opened; backup %s", r.NewEpoch, info.Path)), nil
}
