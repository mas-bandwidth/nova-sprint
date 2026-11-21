package verbs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

// A friend's width and tiers moved from nova-config to the sprint store
// (nova-sprint friend set; the owner, 2026-11-21). up's store step copies
// each friend's from config.friends once: only while those columns still
// exist, and only for a friend the store has no settings for, so a second
// run copies nothing and a friend set is never overwritten.

// configFriend is one config.friends row's old sprint columns.
type configFriend struct {
	Name, Tiers string
	Width       int
}

// friendColumnsSQL counts config.friends' width and tiers columns: 2 while
// nova-config still has them, 0 once it dropped them (or has no such table).
const friendColumnsSQL = `SELECT count(*) FROM information_schema.columns WHERE table_schema = 'config' AND table_name = 'friends' AND column_name IN ('width', 'tiers')`

// friendRowsSQL is every friend's old sprint columns, in name order.
const friendRowsSQL = `SELECT name, width, tiers FROM config.friends ORDER BY name`

// CopyFriendSettings copies the friends' width and tiers from config.friends
// (read on conn) into st as friend:<name> data versions, in one op by actor
// at now, and returns one line per friend copied or left at the defaults.
func CopyFriendSettings(ctx context.Context, conn *pgx.Conn, st store.Store, actor string, now time.Time) ([]string, error) {
	var cols int
	if err := conn.QueryRow(ctx, friendColumnsSQL).Scan(&cols); err != nil {
		return nil, cause("reading config.friends' columns", err)
	}
	if cols != 2 {
		return nil, nil
	}
	rows, err := conn.Query(ctx, friendRowsSQL)
	if err != nil {
		return nil, cause("reading config.friends", err)
	}
	friends, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (configFriend, error) {
		var f configFriend
		return f, r.Scan(&f.Name, &f.Width, &f.Tiers)
	})
	if err != nil {
		return nil, cause("reading config.friends", err)
	}
	snap, err := st.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	have, err := st.NewestData(ctx, roster.FriendPrefix)
	if err != nil {
		return nil, err
	}
	writes, lines := friendCopies(friends, roster.FriendSettings(have))
	if len(writes) == 0 {
		return lines, nil
	}
	op := store.Op{ID: consumerOpID("friend-copy:"+actor, snap.Epoch, fmt.Sprint(writes), int(snap.Seq)), Epoch: snap.Epoch, Actor: actor, Kind: "friend-copy", At: now,
		Expect: []store.Expect{store.UnchangedSince{Seq: snap.Seq, Policies: true}}, Writes: writes, Note: "the friends' width and tiers, copied once from nova-config"}
	r, err := applyOnce(ctx, st, op, st.Apply)
	if err != nil {
		return nil, err
	}
	if !r.Accepted {
		return nil, fmt.Errorf("copying the friends' settings from nova-config refused: %s (run up again)", r.Reason)
	}
	return lines, nil
}

// friendCopies is the writes that copy friends' old settings for each friend
// with no record in have, and one line per friend: copied, or left at the
// defaults when its old settings are no friend's (no tier: it served none).
func friendCopies(friends []configFriend, have map[string]roster.FriendRecord) ([]store.Write, []string) {
	var writes []store.Write
	var lines []string
	for _, f := range friends {
		if _, ok := have[f.Name]; ok {
			continue
		}
		s := roster.FriendSetting{Width: f.Width, Tiers: store.SplitTiers(f.Tiers)}
		if p := s.Problem(); p != "" {
			d := roster.DefaultFriend()
			lines = append(lines, fmt.Sprintf("friend %s: nova-config's width %d tiers %q not copied (%s); it works at width %d tiers %s until %s", f.Name, f.Width, f.Tiers, p, d.Width, store.JoinTiers(d.Tiers), friendFix(f.Name)))
			continue
		}
		writes = append(writes, store.PutPolicy{Kind: "data", Name: roster.FriendPrefix + f.Name, Body: s.Body()})
		lines = append(lines, fmt.Sprintf("friend %s: width %d tiers %s copied from nova-config", f.Name, s.Width, store.JoinTiers(s.Tiers)))
	}
	return writes, lines
}
