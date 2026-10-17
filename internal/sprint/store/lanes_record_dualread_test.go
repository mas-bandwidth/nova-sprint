package store

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/require"
)

// TestLanesRecordDualRead pins the reader-first dual read of the lanes record (sprint.Lanes,
// lane.go lanes, S4, C13): the record lanes reads under laneKey(kind) reads alike from its
// table wire, the padded standard base64 of that wire and today's legacy JSON, damage is
// refused by name (<RECORD> REFUSED: <reason>; run: nova-sprint check), a clamped value is
// refused, and reads are counted per form.
//
// The schema declares no table for the whole lanes record: friendrows.schema defines Lane as
// one machine's lanes (holders and queue), not the Lanes object lanes reads as one value under
// lanes:<kind>. So this test stays red until that table exists and the read site is adapted;
// the card body names it as a schema improvement wanted.
func TestLanesRecordDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "lanes-dual-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)

	// A malformed lanes record is refused by name (S2), never read as absent, empty or
	// zero. Today the read site returns a generic json.Unmarshal error, so this is red.
	require.NoError(t, kv.SetKey(ctx, laneKey("go"), "{not-json"))
	_, _, err = st.lanes(ctx, "go")
	require.Error(t, err)
	require.Regexp(t, regexp.MustCompile(`^LANES REFUSED: [a-z_]+; run: nova-sprint check$`), err.Error())
}
