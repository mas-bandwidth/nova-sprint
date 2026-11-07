package verbs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// Archive operates on the database and only returns a backup after a throwaway
// restore has rebuilt its event log and matched the restored snapshot.
type Archive interface {
	Backup(context.Context, string) (BackupInfo, error)
	AcquireRestore(context.Context) (RestoreLease, error)
	Verify(context.Context, string) (BackupInfo, error)
}
type BackupInfo struct {
	Path         string
	PrefixDigest string
	SchemaHash   string
	Verified     *VerifiedDump
	Epoch        int
	Seq          int64
}
type RestoreLease interface {
	Check(context.Context) error
	Restore(context.Context, *VerifiedDump) error
	Close() error
}

func archiveArgs(args []string, backup bool) (string, bool) {
	value := ""
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--as" || (backup && args[i] == "--out") {
			key := args[i]
			if seen[key] || i+1 >= len(args) || args[i+1] == "" {
				return "", false
			}
			seen[key] = true
			i++
			if key == "--out" {
				value = args[i]
			}
			continue
		}
		if backup || value != "" || args[i] == "" || strings.HasPrefix(args[i], "-") {
			return "", false
		}
		value = args[i]
	}
	return value, backup || value != ""
}

func (e *Engine) backup(ctx context.Context, args []string) Result {
	out, ok := archiveArgs(args, true)
	if !ok {
		return refused("backup", "invalid arguments", "nova-sprint help backup")
	}
	if e.Archive == nil {
		return unavailable("backup")
	}
	info, err := e.Archive.Backup(ctx, out)
	if err != nil {
		return operationalFailure("backup", err)
	}
	return Result{fmt.Sprintf("BACKUP %s epoch=%d seq=%d verified\nnext: nothing\n", info.Path, info.Epoch, info.Seq), 0}
}
func (e *Engine) restore(ctx context.Context, args []string) Result {
	path, ok := archiveArgs(args, false)
	if !ok {
		return refused("restore", "invalid arguments", "nova-sprint help restore")
	}
	if e.Archive == nil {
		return unavailable("restore")
	}
	lease, err := e.Archive.AcquireRestore(ctx)
	if err != nil {
		return operationalFailure("restore", err)
	}
	defer lease.Close()

	info, err := e.Archive.Verify(ctx, path)
	if err != nil {
		return operationalFailure("restore", err)
	}
	if err = lease.Check(ctx); err != nil {
		return operationalFailure("restore", err)
	}
	if info.Verified == nil {
		return unavailable("restore")
	}
	if err = lease.Restore(ctx, info.Verified); err != nil {
		return operationalFailure("restore", err)
	}

	if err = lease.Check(ctx); err != nil {
		return operationalFailure("restore", err)
	}
	return Result{"RESTORED and verified\nnext: nova-sprint run\n", 0}
}
func (e *Engine) clear(ctx context.Context, args []string) Result {
	epoch, dry, actor, ok := parseClear(args, e.Actor)
	if !ok {
		return refused("clear", "invalid arguments", "nova-sprint help clear")
	}
	st, ok := e.Store.(store.Store)
	if !ok || e.Archive == nil || e.Now == nil || e.Env == nil {
		return unavailable("clear")
	}
	hooked, recipe, err := e.Env.Hooked(ctx, actor)
	if err != nil {
		return operationalFailure("clear", err)
	}
	if !hooked {
		return refused("clear", actor+" is not hooked in", recipe)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return operationalFailure("clear", err)
	}
	if !roleMatchesSnapshot(ctx, actor, snap) {
		return refused("clear", "hook changed", "nova-sprint hook --as "+actor+" --role coordinator")
	}
	if epoch != snap.Epoch {
		return refused("clear", fmt.Sprintf("epoch %d is not current epoch %d", epoch, snap.Epoch), fmt.Sprintf("nova-sprint clear --confirm %d", snap.Epoch))
	}
	live := []string{}
	for _, c := range snap.Cards {
		if (c.Kind == "work" || c.Kind == "read") && (c.State == "ready" || c.State == "working") {
			live = append(live, c.ID)
		}
	}
	slices.Sort(live)
	if len(live) > 0 {
		return refused("clear", "live consumers: "+strings.Join(live, ", "), "release each consumer")
	}
	return e.performClear(ctx, st, snap, epoch, dry, actor)
}
func (e *Engine) performClear(ctx context.Context, st store.Store, snap store.Snapshot, epoch int, dry bool, actor string) Result {
	if dry {
		ids := make([]string, 0, len(snap.Cards))
		for id := range snap.Cards {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		return Result{fmt.Sprintf("CLEAR DRY RUN epoch=%d cards=%s\nnext: nova-sprint clear --confirm %d\n", epoch, strings.Join(ids, ","), epoch), 0}
	}
	if e.Preview == nil {
		return unavailable("clear")
	}
	ids := make([]string, 0, len(snap.Cards))
	for id := range snap.Cards {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if err := e.Preview(ctx, fmt.Sprintf("CLEAR epoch=%d cards=%s\n", epoch, strings.Join(ids, ","))); err != nil {
		return operationalFailure("clear", err)
	}
	info, err := e.Archive.Backup(ctx, "")
	if err != nil {
		return operationalFailure("clear", err)
	}
	if info.Epoch != snap.Epoch || info.Seq != snap.Seq {
		return refused("clear", "state changed while backup was made", "retry nova-sprint clear --confirm "+strconv.Itoa(epoch))
	}
	op := store.Op{ID: consumerOpID("clear:"+actor, snap.Epoch, "clear", int(snap.Seq)), Epoch: snap.Epoch, Actor: actor, Kind: "clear", At: e.Now(), Expect: []store.Expect{store.AtSeq{Seq: snap.Seq}}}
	rec, err := clearExact(ctx, st, op)
	if err != nil {
		return operationalFailure("clear", err)
	}
	if !rec.Accepted {
		return refused("clear", rec.Reason, "retry nova-sprint clear --confirm "+strconv.Itoa(epoch))
	}
	return Result{fmt.Sprintf("CLEARED epoch=%d backup=%s\nnext: nova-sprint stream add <name> --repo <owner/name>\n", rec.NewEpoch, info.Path), 0}
}
func parseClear(args []string, actor string) (int, bool, string, bool) {
	epoch := 0
	dry := false
	for i := 0; i < len(args); i++ {
		if args[i] == "--dry-run" {
			dry = true
			continue
		}
		if i+1 == len(args) {
			return 0, false, "", false
		}
		key := args[i]
		i++
		switch key {
		case "--as":
			actor = args[i]
		case "--confirm":
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				return 0, false, "", false
			}
			epoch = n
		default:
			return 0, false, "", false
		}
	}
	return epoch, dry, actor, epoch > 0 && actor != ""
}

func clearExact(ctx context.Context, st store.Store, op store.Op) (store.Receipt, error) {
	claim, ok := ctx.Value(roleClaimKey{}).(roleClaim)
	if !ok || claim.actor != op.Actor {
		return store.Receipt{}, fmt.Errorf("role claim missing or mismatched")
	}
	r, err := st.Clear(ctx, op)
	if !errors.Is(err, store.ErrUnknown) {
		return r, err
	}
	r, found, err := st.Replay(ctx, op.ID)
	if err != nil || found {
		return r, err
	}
	return st.Clear(ctx, op)
}
