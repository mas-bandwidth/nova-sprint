package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// Every key the sprint writes has a bound, so a store can never grow without
// limit (tla/StoreRetention.tla Bounded). The log and the inbox are streams
// whose entries are trimmed to RetentionLinesDefault; the notes, the aliases,
// the answered and the open hashes are pruned to the notes the inbox still
// holds; a past epoch's keys are retired after a clear (RetireOldEpochs).
//
// The log's trim never silently passes the coordinator's cursor: when the
// bound forces it to drop a line the coordinator has not read, the first line
// it kept is recorded in keyLogGap, and LogGap answers the gap instead of an
// error (tla/StoreRetention.tla NoTrimPastCursor).
const (
	// RetentionLinesDefault is the log's and the inbox's bound: at least four
	// times the largest epoch seen (sprint:log@15 held 691470 lines on
	// 2026-10-19), so 3,000,000 lines. The sprint's setting overrides it
	// (PropRetentionLogLines).
	RetentionLinesDefault = 3_000_000

	// RetentionKeepEpochs is how many epochs the store keeps: the current one
	// and its previous one. A clear retires everything older.
	RetentionKeepEpochs = 2

	// PropRetentionLogLines is the work table's property: the log's and the
	// inbox's bound, a whole number of lines above zero.
	PropRetentionLogLines = "retention_log_lines"

	// keyLogGap records the first log line kept after a trim passed the
	// coordinator's cursor, so the coordinator is told of the gap.
	keyLogGap = "loggap"
	// keyNoteOrder is the list of note ids the inbox still holds, oldest
	// first: the notes hash is pruned to it.
	keyNoteOrder = "noteorder"
	// keyNoteSeeded is set once keyNoteOrder has been seeded from the epoch's
	// inbox: the notes written before the bound existed are in it too, so the
	// first trim never deletes a note the inbox still holds.
	keyNoteSeeded = "noteseeded"
	// keyRetire is the epoch whose keys a bounded retire pass is still
	// removing: the next clear (or the next call) resumes it.
	keyRetire = "retire"
	// keyRetiredThrough is the newest epoch the store retired: a read of one
	// at or below it says the epoch was retired (log/card --at-epoch).
	keyRetiredThrough = "retiredthrough"
)

// The retire pass's bounds: one SCAN page and one UNLINK batch at a time, so a
// store with a great many keys is retired in bounded steps, never in the
// tick's hot path.
const (
	retentionScanCount   = 100
	retentionUnlinkBatch = 100
)

// the gap and the note order are an epoch's sprint keys: teardown removes them
// with the epoch.
func init() {
	sprintKeys = append(sprintKeys, keyLogGap, keyNoteOrder, keyNoteSeeded)
	machineKeys = append(machineKeys, keyRetiredThrough)
}

// RetentionLines is the log's and the inbox's bound as the work table's
// properties hold them: a whole number above zero, else RetentionLinesDefault.
func RetentionLines(props map[string]string) int {
	if v := strings.TrimSpace(props[PropRetentionLogLines]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return RetentionLinesDefault
}

// OldestRetained is the oldest epoch the store still holds at current: the
// current epoch and its previous one (RetentionKeepEpochs).
func OldestRetained(current uint64) uint64 {
	if current+1 <= RetentionKeepEpochs {
		return 0
	}
	return current + 1 - RetentionKeepEpochs
}

// trimRetained keeps at most bound entries, dropping the oldest. It returns
// the first kept id when the cursor was among the dropped entries: the gap the
// coordinator must be told of (tla/StoreRetention.tla NoTrimPastCursor). A
// cursor at or after the first kept entry is never dropped.
func trimRetained[T any](xs []T, bound int, id func(T) string, cursor string) ([]T, string) {
	if bound <= 0 || len(xs) <= bound {
		return xs, ""
	}
	cut := len(xs) - bound
	first := id(xs[cut])
	if cursor != "" && streamIDLess(cursor, first) {
		return xs[cut:], first
	}
	return xs[cut:], ""
}

// retentionLines is the bound this store's writes use.
func (m *Mem) retentionLines() int {
	if m.retention > 0 {
		return m.retention
	}
	return RetentionLines(nil)
}

// retentionLines is the bound this store's writes use.
func (r *Redis) retentionLines() int {
	if r.Retention > 0 {
		return r.Retention
	}
	return RetentionLines(nil)
}

// trimRetainedLocked trims the pinned epoch's log and inbox and prunes the
// hashes and the log index to what they still hold. The caller holds m.mu.
func (m *Mem) trimRetainedLocked(l *memLog) {
	bound := m.retentionLines()
	var gap string
	l.lines, gap = trimRetained(l.lines, bound, func(x memLine) string { return x.id }, l.cursor)
	if gap != "" {
		l.gap = gap
	}
	l.inbox, _ = trimRetained(l.inbox, bound, func(n memNote) string { return n.id }, "")
	keep := map[string]bool{}
	for _, n := range l.inbox {
		keep[n.note.ID] = true
	}
	for id := range l.notes {
		if !keep[id] {
			delete(l.notes, id)
		}
	}
	for k, id := range l.aliases {
		if !keep[id] {
			delete(l.aliases, k)
		}
	}
	for id := range l.answered {
		if !keep[id] {
			delete(l.answered, id)
		}
	}
	for k, id := range l.open {
		if !keep[id] {
			delete(l.open, k)
		}
	}
	if len(l.doneOrder) > bound {
		for _, op := range l.doneOrder[:len(l.doneOrder)-bound] {
			delete(l.done, op)
		}
		l.doneOrder = l.doneOrder[len(l.doneOrder)-bound:]
	}
	m.pruneIndexLocked(l)
}

// pruneIndexLocked drops the log index's entries that name a line the log no
// longer holds (tla/StoreRetention.tla IndexNeverDangles). The caller holds
// m.mu.
func (m *Mem) pruneIndexLocked(l *memLog) {
	ixKey, _ := m.logIndexKeys()
	raw, ok := m.kv[ixKey]
	if !ok {
		return
	}
	ix := map[string][]string{}
	if err := json.Unmarshal([]byte(raw), &ix); err != nil {
		return
	}
	held := map[string]bool{}
	for _, x := range l.lines {
		held[x.id] = true
	}
	changed := false
	for k, ids := range ix {
		kept := ids[:0]
		for _, id := range ids {
			if held[id] {
				kept = append(kept, id)
			} else {
				changed = true
			}
		}
		if len(kept) == 0 {
			delete(ix, k)
		} else {
			ix[k] = kept
		}
	}
	if changed {
		if b, err := json.Marshal(ix); err == nil {
			m.kv[ixKey] = string(b)
		}
	}
}

// RetireOldEpochs removes every key of an epoch older than the previous one
// (tla/StoreRetention.tla Retire). It is one bounded, resumable step: a SCAN
// page and an UNLINK batch at a time. A clear runs it after it completes,
// never in the tick's hot path.
func (r *Redis) RetireOldEpochs(ctx context.Context, current uint64) error {
	oldest := OldestRetained(current)
	for e := uint64(0); e < oldest; e++ {
		if err := r.retireEpoch(ctx, e); err != nil {
			return err
		}
	}
	if oldest > 0 {
		if err := r.C.Set(ctx, r.key(keyRetiredThrough), strconv.FormatUint(oldest-1, 10), 0).Err(); err != nil {
			return err
		}
	}
	return r.C.Del(ctx, r.key(keyRetire)).Err()
}

// retiredSprintKeys is every per-epoch sprint key the retire step removes.
var retiredSprintKeys = []string{keyFence, keyGen, keyInbox, keyLog, keyNotes, keyOpen,
	keyCursor, keyProgress, keyDone, keyQueue, keyAliases, keyAnswered,
	keyLogIndex, keyLogIndexed, keyLogGap, keyNoteOrder, keyNoteSeeded}

// retireEpoch removes one epoch's keys: the sprint's own keys by exact name,
// and the per-card hashes by a SCAN of their epoch suffix, in UNLINK batches.
func (r *Redis) retireEpoch(ctx context.Context, e uint64) error {
	var batch []string
	flush := func() error {
		for start := 0; start < len(batch); start += retentionUnlinkBatch {
			end := min(start+retentionUnlinkBatch, len(batch))
			if err := r.C.Unlink(ctx, batch[start:end]...).Err(); err != nil {
				return err
			}
		}
		batch = batch[:0]
		return nil
	}
	for _, k := range retiredSprintKeys {
		batch = append(batch, r.Names.KeyAt(k, e))
		if len(batch) >= retentionUnlinkBatch {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	suffix := "~" + strconv.FormatUint(e, 10)
	pattern := r.Names.Prefix + "sprint:[frwm]:*" + suffix
	var cursor uint64
	for {
		keys, next, err := r.C.Scan(ctx, cursor, pattern, retentionScanCount).Result()
		if err != nil {
			return err
		}
		batch = append(batch, keys...)
		if len(batch) >= retentionUnlinkBatch {
			if err := flush(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return flush()
}

// RetireOldEpochs is the in-memory store's retire: the epoch's sprint keys are
// its memLog, removed whole.
func (m *Mem) RetireOldEpochs(current uint64) []uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldest := OldestRetained(current)
	var retired []uint64
	for e := uint64(0); e < oldest; e++ {
		if _, ok := m.logs[e]; ok {
			delete(m.logs, e)
			retired = append(retired, e)
		}
	}
	if oldest > 0 {
		m.retiredFrom = oldest
	}
	return retired
}

// retireOldEpochs runs the store's retire step at current.
func (st *Store) retireOldEpochs(ctx context.Context, current uint64) error {
	switch b := st.B.(type) {
	case *Mem:
		b.RetireOldEpochs(current)
	case *Redis:
		return b.RetireOldEpochs(ctx, current)
	}
	return nil
}

// pruneRetained is the tick's same-pass prune of the notes and the log index
// to the lines still held (tla/StoreRetention.tla IndexNeverDangles). A store
// that keeps no index is left alone.
func (st *Store) pruneRetained(ctx context.Context) error {
	switch b := st.B.(type) {
	case *Redis:
		return b.pruneRetained(ctx)
	case *Mem:
		b.mu.Lock()
		defer b.mu.Unlock()
		b.trimRetainedLocked(b.log())
	}
	return nil
}

// pruneRetained records the gap a trim passed the cursor at and prunes the log
// index to the lines the log still holds. The notes are pruned where they are
// written (trimNoteOrder): a pass never reads the whole notes or open hash, so
// its cost is bounded by the log index pages, not by the store's size.
func (r *Redis) pruneRetained(ctx context.Context) error {
	if err := r.seedNoteOrder(ctx); err != nil {
		return err
	}
	p := r.C.Pipeline()
	firstLog := p.XRangeN(ctx, r.key(keyLog), "-", "+", 1)
	cur := p.Get(ctx, r.key(keyCursor))
	if _, err := p.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	logFirst := firstID(firstLog)
	if logFirst != "" && cur.Val() != "" && streamIDLess(cur.Val(), logFirst) {
		if err := r.C.Set(ctx, r.key(keyLogGap), logFirst, 0).Err(); err != nil {
			return err
		}
	}
	if logFirst != "" {
		if err := r.pruneIndex(ctx, logFirst); err != nil {
			return err
		}
	}
	return nil
}

// firstID is the first stream id a one-entry XRANGE returned, "" for none.
func firstID(c *redis.XMessageSliceCmd) string {
	if ms, err := c.Result(); err == nil && len(ms) > 0 {
		return ms[0].ID
	}
	return ""
}

// seedNoteOrder puts the epoch's judgments already in the inbox before the
// first note this code writes into keyNoteOrder. Without it the first note
// after a deploy would leave every earlier live judgment out of the order and
// a trim would delete it. It runs once per epoch; the marker makes it
// idempotent, and a list that is already non-empty is left alone.
func (r *Redis) seedNoteOrder(ctx context.Context) error {
	seeded := r.key(keyNoteSeeded)
	if n, err := r.C.Exists(ctx, seeded).Result(); err != nil {
		return err
	} else if n > 0 {
		return nil
	}
	order := r.key(keyNoteOrder)
	if n, err := r.C.LLen(ctx, order).Result(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	} else if n > 0 {
		// a list without its marker was written before a crash between the
		// order and the marker: do not seed duplicates over it.
		return r.C.Set(ctx, seeded, "1", 0).Err()
	}
	bound := r.retentionLines()
	// Read the inbox in pages: each entry's note id, in insertion order.
	var ids []string
	after := "-"
	for {
		msgs, err := r.C.XRangeN(ctx, r.key(keyInbox), after, "+", retentionScanCount).Result()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			var n sprint.Note
			if s, ok := m.Values["note"].(string); ok && json.Unmarshal([]byte(s), &n) == nil {
				if n.Kind == sprint.Judgment || n.Kind == sprint.Acknowledged {
					ids = append(ids, n.ID)
				}
			}
		}
		if len(msgs) < retentionScanCount {
			break
		}
		after = "(" + msgs[len(msgs)-1].ID
	}
	if len(ids) == 0 {
		return r.C.Set(ctx, seeded, "1", 0).Err()
	}
	drop := []string(nil)
	if len(ids) > bound {
		drop, ids = ids[:len(ids)-bound], ids[len(ids)-bound:]
	}
	pipe := r.C.Pipeline()
	for start := 0; start < len(ids); start += retentionUnlinkBatch {
		pipe.RPush(ctx, order, stringsToAny(ids[start:min(start+retentionUnlinkBatch, len(ids))])...)
	}
	if err := r.queueDroppedNotes(ctx, pipe, drop, nil); err != nil {
		return err
	}
	pipe.Set(ctx, seeded, "1", 0)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return nil
}

// trimNoteOrder queues keyNoteOrder's trim and the deletion of the hash fields
// of the notes the trim drops, so the notes, the aliases, the answered and the
// open index stay bounded with the inbox. It reads the list and the dropped
// notes before the transaction: the fence serializes commit writers.
func (r *Redis) trimNoteOrder(ctx context.Context, p redis.Pipeliner, added []string, bound int) error {
	order := r.key(keyNoteOrder)
	length, err := r.C.LLen(ctx, order).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	overflow := int(length) + len(added) - bound
	p.LTrim(ctx, order, int64(-bound), -1)
	if overflow <= 0 {
		return nil
	}
	dropped, err := r.C.LRange(ctx, order, 0, int64(overflow-1)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return r.queueDroppedNotes(ctx, p, dropped, added)
}

// queueDroppedNotes queues the removal of the note hashes of dropped ids, in
// bounded HDEL batches. An id still being added is never removed. The notes,
// aliases, answered and open fields go together, so none outlives the inbox.
func (r *Redis) queueDroppedNotes(ctx context.Context, p redis.Pipeliner, dropped, added []string) error {
	if len(dropped) == 0 {
		return nil
	}
	keep := make(map[string]bool, len(added))
	for _, id := range added {
		keep[id] = true
	}
	var ids []string
	for _, id := range dropped {
		if !keep[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	bodies, err := r.C.HMGet(ctx, r.key(keyNotes), ids...).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	for start := 0; start < len(ids); start += retentionUnlinkBatch {
		end := min(start+retentionUnlinkBatch, len(ids))
		fields := ids[start:end]
		p.HDel(ctx, r.key(keyNotes), fields...)
		p.HDel(ctx, r.key(keyAnswered), fields...)
	}
	for i, id := range ids {
		if i >= len(bodies) {
			break
		}
		s, ok := bodies[i].(string)
		if !ok {
			continue
		}
		var n sprint.Note
		if json.Unmarshal([]byte(s), &n) != nil {
			continue
		}
		if n.Alias != "" {
			p.HDel(ctx, r.key(keyAliases), n.Alias)
		}
		for _, subj := range n.Subjects() {
			p.HDel(ctx, r.key(keyOpen), sprint.OpenKey(id, subj))
		}
	}
	return nil
}

// pruneIndex removes the log index's entries whose line id is before first,
// the log's oldest held line, in SCAN pages.
func (r *Redis) pruneIndex(ctx context.Context, first string) error {
	var cursor uint64
	var drop []string
	for {
		members, next, err := r.C.ZScan(ctx, r.key(keyLogIndex), cursor, "*", retentionScanCount).Result()
		if err != nil {
			return err
		}
		for i := 0; i+1 < len(members); i += 2 {
			m := members[i]
			if j := strings.LastIndexByte(m, 0); j >= 0 {
				if streamIDLess(m[j+1:], first) {
					drop = append(drop, m)
				}
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	for start := 0; start < len(drop); start += retentionUnlinkBatch {
		if err := r.C.ZRem(ctx, r.key(keyLogIndex), stringsToAny(drop[start:min(start+retentionUnlinkBatch, len(drop))])...).Err(); err != nil {
			return err
		}
	}
	return nil
}

// stringsToAny is a batch of members for ZRem.
func stringsToAny(xs []string) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// storeRefusal is what a step says when its record did not reach the store:
// an EXECABORT is unwrapped to the first queued command's error, and an OOM
// names the store's memory, never the record's size.
func storeRefusal(op OpRecord, err error, cmds ...redis.Cmder) string {
	err = unwrapExecAbort(err, cmds)
	if isStoreFull(err) {
		return "the store is full (Redis OOM: used memory at maxmemory); nothing was written; the coordinator frees it or raises maxmemory"
	}
	return fmt.Sprintf("the store did not take the step's record (%d bytes): %v; nothing was changed", recordSize(op), err)
}

// unwrapExecAbort is the first queued command's error when execErr is an
// EXECABORT (go-redis returns each command's error on its own Cmd), else
// execErr.
func unwrapExecAbort(execErr error, cmds []redis.Cmder) error {
	if execErr == nil || !strings.Contains(strings.ToUpper(execErr.Error()), "EXECABORT") {
		return execErr
	}
	for _, c := range cmds {
		if e := c.Err(); e != nil {
			return e
		}
	}
	return execErr
}

// isStoreFull says the error is Redis's out-of-memory refusal.
func isStoreFull(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToUpper(err.Error())
	return strings.Contains(s, "OOM COMMAND NOT ALLOWED") || strings.HasPrefix(s, "OOM ") || strings.Contains(s, "MAXMEMORY")
}

// retiredRefusal is the named refusal a caller operation from an epoch the
// store retired gets: it is not run again (tla/StoreRetention.tla
// ReplayAnswered).
func retiredRefusal(callerOp string, current uint64) string {
	return fmt.Sprintf("operation %s is older than the store keeps; it was not run again (the store holds epoch %d and its previous one)", callerOp, current)
}
