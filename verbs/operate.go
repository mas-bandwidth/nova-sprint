package verbs

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/edge/worker"

	"github.com/mas-bandwidth/nova-sprint/run"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// runStop is `stop` and `start`: the tick stopped or started, one write.
func runStop(e *Engine, ctx context.Context, in *input, snap store.Snapshot) (string, error) {
	stop, reason := in.verb == "stop", in.str("reason")
	if stop && reason == "" {
		return "", refuse("a stop needs its reason", "add --reason \"<why>\"")
	}
	label, next := "STARTED", waitNext
	if stop {
		label, next = "STOPPED", "nova-sprint start --reason \"<why>\""
	}
	if snap.Stopped == stop {
		return done(next, fmt.Sprintf("%s already (%s)", label, orWord(snap.StopReason, "no reason given"))), nil
	}
	op := e.newOp(in.verb, in.actor, snap, reason)
	op.Expect = []store.Expect{store.AtSeq{Seq: snap.Seq}}
	op.Writes, op.Note = []store.Write{store.SetStopped{Stopped: stop, Reason: reason}}, reason
	r, err := e.commit(ctx, op, "nova-sprint show run")
	if err != nil {
		return "", err
	}
	return done(next, fmt.Sprintf("%s applied at seq %d", label, r.Seq)), nil
}

// runCheck is `check`, the one doctor: the fleet, nova-tools at or above
// the pin, the store at this build's schema, the bus; --integrity also
// rebuilds the history and compares it with the tables.
func runCheck(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if e.Doctor == nil || e.Bus == nil {
		return "", broken("the doctor is not configured", "set NOVA_PG_DSN and NOVA_BUS_REDIS, then check again")
	}
	if err := e.Doctor.Fleet(ctx); err != nil {
		return "", err
	}
	tools, err := e.toolsAtPin(ctx)
	if err != nil {
		return "", err
	}
	current, err := e.Doctor.Schema(ctx)
	if err != nil {
		return "", broken("the store did not answer ("+clip(err.Error())+")", "check NOVA_PG_DSN and NOVA_SPRINT_PG_PASSWORD")
	}
	if !current {
		return "", refuse("the store is not at this build's schema", "nova-sprint up --as <coordinator>")
	}
	who, err := e.Bus.Who(ctx)
	if err != nil {
		return "", broken("nova-bus who failed ("+clip(err.Error())+")", "check NOVA_BUS_REDIS, then nova-bus who")
	}
	lines := []string{"CHECK fleet green", "CHECK nova-tools " + tools + " (pin " + e.ToolPin + ")", "CHECK store at schema", fmt.Sprintf("CHECK bus answering (%d present)", len(who))}
	if in.has("integrity") {
		if err := e.Doctor.History(ctx); err != nil {
			return "", err
		}
		lines = append(lines, "CHECK history rebuilds the tables")
	}
	return done("nova-sprint next --as "+orWord(in.actor, "<you>"), lines...), nil
}

// toolsAtPin is nova-tools' release, refused below this build's pin.
func (e *Engine) toolsAtPin(ctx context.Context) (string, error) {
	line, err := e.Doctor.ToolsVersion(ctx)
	v, ok := parseNovaVersion(line)
	if err != nil || !ok {
		return "", refuse("nova-tools' version is unknown", "install nova-tools "+e.ToolPin+" or later with nova-update")
	}
	if at, err := atLeast(v, e.ToolPin); err != nil || !at {
		return "", refuse("nova-tools "+v+" is below the pin "+e.ToolPin, "nova-fleet up")
	}
	return v, nil
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
		return out, fmt.Errorf("invalid version")
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil || p == "" || strings.Trim(p, "0123456789") != "" || (len(p) > 1 && p[0] == '0') {
			return out, fmt.Errorf("invalid version")
		}
		out[i] = n
	}
	return out, nil
}

// parseNovaVersion is `nova-version version`'s release.
func parseNovaVersion(line string) (string, bool) {
	f := strings.Fields(line)
	if len(f) < 2 || f[0] != "nova-version" {
		return "", false
	}
	_, err := releaseVersion(f[1])
	return f[1], err == nil
}

func runVersion(e *Engine, _ context.Context, _ *input, _ store.Snapshot) (string, error) {
	return done("nova-sprint help", "nova-sprint "+orWord(e.Version, "(unknown build)")), nil
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
	if !validName(in.actor) || in.str("work-clone") == "" || in.str("coordinator-hosts") == "" {
		return "", misuse("worker", "worker takes --as <row> --work-clone <dir> --coordinator-hosts <h,h>")
	}
	w := WorkerArgs{Row: in.actor, Host: in.str("host"), Root: in.str("root"), StateDir: in.str("state-dir"), Harness: in.str("harness"),
		WorkClone: in.str("work-clone"), Mirror: in.str("mirror"), Origin: in.str("origin"), Once: in.has("once"),
		CoordinatorHosts: worker.SplitList(in.str("coordinator-hosts")), ProviderUpstream: in.str("provider-upstream"),
		ProviderHosts: worker.SplitList(in.str("provider-hosts")), ProviderKey: in.str("provider-key")}
	if err := e.Services.Worker(ctx, w); err != nil {
		return "", err
	}
	return done("nova-sprint worker --as "+in.actor+" --work-clone "+w.WorkClone, "WORKER "+in.actor+" ended"), nil
}

func runDash(e *Engine, ctx context.Context, in *input, _ store.Snapshot) (string, error) {
	if e.Services == nil {
		return "", broken("dash is not configured in this build", "nova-sprint check")
	}
	if in.str("listen") == "" {
		return "", misuse("dash", "dash takes --listen <addr>")
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
	return fmt.Sprintf("RESTORED %s epoch=%d seq=%d digest=%s\n", info.Path, info.Epoch, info.Seq, digest) + text, nil
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

// runClear is `clear`: after a verified backup at this very seq, the epoch
// ends and every card goes. Live consumers refuse: release them first.
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
	if info.Epoch != snap.Epoch || info.Seq != snap.Seq {
		return "", refuse("the store changed while the backup was made", fmt.Sprintf("nova-sprint clear --confirm %d again", snap.Epoch))
	}
	op := e.newOp("clear", in.actor, snap)
	op.Expect = []store.Expect{store.AtSeq{Seq: snap.Seq}}
	r, err := applyOnce(ctx, e.Store, op, e.Store.Clear)
	if err == nil && !r.Accepted {
		err = refuse(r.Reason, fmt.Sprintf("nova-sprint clear --confirm %d again", snap.Epoch))
	}
	if err != nil {
		return "", err
	}
	return done("nova-sprint stream add <name> --repo <org/repo>", what, fmt.Sprintf("CLEARED epoch=%d opened; backup %s", r.NewEpoch, info.Path)), nil
}
