package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/oneline"
	"github.com/redis/go-redis/v9"
)

// CIReceiptTell.tla: the shared producer stream is deliberately not namespaced
// by sprint. The server-owned cursor is durable, unlike its heartbeat's TTL.
const (
	ciReceiptStream = "ev:github"
	ciReceiptSweep  = 10 * time.Minute
	ciReceiptCursor = "server:ci-receipt:cursor"
	ciReceiptBatch  = 128
)

type ciReceiptReader interface {
	BlockRead(context.Context, string, string, time.Duration, int) ([]bus.Entry, error)
	Range(context.Context, string, string, string, int) ([]bus.Entry, error)
	// End is the id of the stream's newest entry, or "0-0" for an empty stream.
	End(context.Context, string) (string, error)
}

// ciRedis adapts the existing go-redis universal client, without a second
// connection or polling transport (CIReceiptTell.tla Read/Sweep).
type ciRedis struct{ redis.UniversalClient }

func (r ciRedis) BlockRead(ctx context.Context, stream, after string, block time.Duration, count int) ([]bus.Entry, error) {
	rows, err := r.XRead(ctx, &redis.XReadArgs{Streams: []string{stream, after}, Block: max(block, time.Millisecond), Count: int64(count)}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []bus.Entry
	for _, batch := range rows {
		out = append(out, ciEntries(batch.Stream, batch.Messages)...)
	}
	return out, nil
}
func (r ciRedis) End(ctx context.Context, stream string) (string, error) {
	rows, err := r.XRevRangeN(ctx, stream, "+", "-", 1).Result()
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "0-0", nil
	}
	return rows[0].ID, nil
}
func (r ciRedis) Range(ctx context.Context, stream, from, to string, count int) ([]bus.Entry, error) {
	rows, err := r.XRangeN(ctx, stream, from, to, int64(count)).Result()
	if err != nil {
		return nil, err
	}
	return ciEntries(stream, rows), nil
}
func ciEntries(stream string, rows []redis.XMessage) []bus.Entry {
	out := make([]bus.Entry, 0, len(rows))
	for _, row := range rows {
		fields := make(map[string]string, len(row.Values))
		for key, value := range row.Values {
			fields[key] = fmt.Sprint(value)
		}
		out = append(out, bus.Entry{Stream: stream, Entry: row.ID, Fields: fields})
	}
	return out
}

type ciTell struct {
	a       *app
	kv      store.KV
	reader  ciReceiptReader
	cursor  string
	sweepAt time.Time
}

// ciDelivery freezes the once-key's arguments before sending; Sent survives bus
// token expiry. Pending retries stop before expiry instead of risking a duplicate.
// CIReceiptTell.tla Send/SendLost abstract the bus's atomic once-key transaction.
type ciDelivery struct {
	Message     bus.Message `json:"message"`
	RetryBefore time.Time   `json:"retry_before"`
	Sent        bool        `json:"sent"`
}

// newCITell resumes the last completed stream prefix (CIReceiptTell.tla Init).
func newCITell(a *app, kv store.KV, reader ciReceiptReader) (*ciTell, error) {
	cursor, _, err := kv.GetKey(context.Background(), ciReceiptCursor)
	if err != nil {
		return nil, err
	}
	if cursor == "" {
		// The first start has no history to tell: begin at the stream's current end,
		// never at 0-0, which would replay every past CI event to the coordinator.
		end, err := reader.End(context.Background(), ciReceiptStream)
		if err != nil {
			return nil, err
		}
		if err := kv.SetKey(context.Background(), ciReceiptCursor, end); err != nil {
			return nil, err
		}
		cursor = end
	}
	return &ciTell{a: a, kv: kv, reader: reader, cursor: cursor, sweepAt: a.now().Add(ciReceiptSweep)}, nil
}

// ciTellLoop uses the sprint's existing authenticated Redis client. Only the
// blocking read waits for events; the ten-minute XRANGE is the recovery backstop
// (CIReceiptTell.tla Read/Sweep). No GitHub service or API is polled.
func (a *app) ciTellLoop(ctx context.Context) {
	var tell *ciTell
	for ctx.Err() == nil {
		var err error
		if tell == nil {
			err = a.onLine(ctx, func(st *store.Store) error {
				r, ok := st.B.(*store.Redis)
				if !ok {
					return errors.New("CI receipts need the sprint Redis event stream")
				}
				tell, err = newCITell(a, r, ciRedis{r.C})
				return err
			})
		}
		if err == nil {
			err = tell.step(ctx)
		}
		if err != nil {
			a.serveSay("CI RECEIPT: retry with cursor unchanged: %s", oneline.Err(err))
			select {
			case <-ctx.Done():
				return
			case <-a.after(time.Second):
			}
		}
	}
}

// step handles one transport batch without a timer on the push path. A missed
// response leaves the cursor standing for Sweep; AdvanceCursor follows every
// successful send, never precedes it (CIReceiptTell.tla).
func (c *ciTell) step(ctx context.Context) error {
	now := c.a.now()
	var rows []bus.Entry
	var err error
	if !now.Before(c.sweepAt) {
		rows, err = c.reader.Range(ctx, ciReceiptStream, "("+c.cursor, "+", ciReceiptBatch)
		if err == nil {
			c.sweepAt = now.Add(ciReceiptSweep)
		}
	} else {
		rows, err = c.reader.BlockRead(ctx, ciReceiptStream, c.cursor, c.sweepAt.Sub(now), ciReceiptBatch)
	}
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := c.tell(ctx, row); err != nil {
			return err
		}
		if err := c.kv.SetKey(ctx, ciReceiptCursor, row.Entry); err != nil {
			return err
		}
		c.cursor = row.Entry
	}
	return nil
}

// ciKey is a bounded, unambiguous durable key; a run's token depends only on
// repository, head and run id (CIReceiptTell.tla Key/NeverToldTwice).
func ciKey(parts ...string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\x00"))))
}

// tell records PR branch observations without notifying, skips other events,
// then freezes and sends a completed receipt once (CIReceiptTell.tla Send).
// Replaying an already sent key advances the cursor without another bus send.
func (c *ciTell) tell(ctx context.Context, row bus.Entry) error {
	f := row.Fields
	if f["kind"] == "pull_request" && f["repo"] != "" && f["number"] != "" && f["branch"] != "" {
		return c.kv.SetKey(ctx, "server:ci-receipt:pr:"+ciKey(f["repo"], f["number"]), f["branch"])
	}
	if f["kind"] != "workflow_run" || f["status"] != "completed" {
		return nil
	}
	for _, field := range []string{"repo", "head", "run_id", "conclusion"} {
		if f[field] == "" {
			return fmt.Errorf("completed receipt %s lacks %s; repair the producer row before retry", row.Entry, field)
		}
	}
	token := "ci-receipt:" + ciKey(f["repo"], f["head"], f["run_id"])
	key := "server:ci-receipt:sent:" + token
	raw, exists, err := c.kv.GetKey(ctx, key)
	if err != nil {
		return err
	}
	var d ciDelivery
	if exists {
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return fmt.Errorf("CI receipt record %s: %w", key, err)
		}
	} else {
		branch, _, err := c.kv.GetKey(ctx, "server:ci-receipt:pr:"+ciKey(f["repo"], f["number"]))
		if err != nil {
			return err
		}
		d.Message, err = c.a.ciMessage(ctx, f, branch)
		if err != nil {
			return err
		}
		d.Message.Token = token
		d.RetryBefore = c.a.now().Add(bus.DefaultTokenLife)
		if err := c.save(ctx, key, d); err != nil {
			return err
		}
	}
	if d.Sent {
		return nil
	}
	if !c.a.now().Before(d.RetryBefore) {
		return fmt.Errorf("CI receipt %s has an expired unresolved send; inspect its bus token before repairing the record", token)
	}
	login, err := c.a.coordinatorBusLogin()
	if err != nil {
		return err
	}
	if why := coordinatorSenderRefusal(d.Message.From, login); why != "" {
		return errors.New(why)
	}
	if err := c.a.bus(ctx, d.Message, func(s string) { c.a.serveSay("%s", s) }); err != nil {
		return err
	}
	d.Sent = true
	return c.save(ctx, key, d)
}

// save persists the frozen send and its receipt before advancing the cursor
// (CIReceiptTell.tla CursorNeverPassesUntold). A failed write retries that token.
func (c *ciTell) save(ctx context.Context, key string, d ciDelivery) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return c.kv.SetKey(ctx, key, string(raw))
}

// ciMessage resolves one owner from the current sprint snapshot, head before
// branch, always within the receipt's repository. Unmatched events go to the
// seat as card=-. The recipient decision is frozen even if a PR mapping arrives
// later (CIReceiptTell.tla's atomic recipient pair; docs/SPEC-SPRINT.md).
func (a *app) ciMessage(ctx context.Context, f map[string]string, branch string) (bus.Message, error) {
	var m bus.Message
	login, err := a.coordinatorBusLogin()
	if err != nil {
		return m, err
	}
	err = a.onLine(ctx, func(st *store.Store) error {
		s, err := st.Load(ctx, []string{sprint.Work, sprint.Fleet}, nil)
		if err != nil {
			return err
		}
		if s.Coordinator == "" {
			return errors.New("CI receipt waits for the coordinator seat to be named")
		}
		card, owner := "-", ""
		candidates := s.Fleet.Cards()
		for _, byHead := range []bool{true, false} {
			for _, w := range candidates {
				if w.F("kind") != "work" {
					continue
				}
				p := s.Work.Card(w.F("primary"))
				repo := w.F("repo")
				if repo == "" && p != nil {
					repo = cmp.Or(p.F("repo"), swarm.ReadCardBase([]byte(p.F("brief"))).Repo)
				}
				if sprint.RepoName(repo) != sprint.RepoName(f["repo"]) {
					continue
				}
				match := byHead && strings.EqualFold(w.F("head"), f["head"])
				if !byHead {
					match = branch != "" && sprint.PushedBranch(s.Prefix, s.Epoch, w) == branch
				}
				if match {
					card, owner = w.ID, strings.TrimPrefix(cmp.Or(w.F("member"), w.Row), "friend.")
					break
				}
			}
			if card != "-" {
				break
			}
		}
		to := []string{s.Coordinator}
		if owner != "" && owner != s.Coordinator {
			to = append(to, owner)
		}
		head := f["head"]
		if len(head) > 12 {
			head = head[:12]
		}
		subject := fmt.Sprintf("CI %s %s@%s pr=%s run=%s card=%s", oneline.Escape(f["conclusion"]), oneline.Escape(f["repo"]), oneline.Escape(head), oneline.Escape(cmp.Or(f["number"], "0")), oneline.Escape(f["run_id"]), card)
		m = bus.Message{From: cmp.Or(login, s.Coordinator), To: to, Kind: bus.KindStatus, Subject: subject, Body: subject}
		return nil
	})
	return m, err
}
