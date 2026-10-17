package store

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLanesRecordDualRead pins the reader-first dual read of the lanes record (sprint.Lanes,
// lane.go lanes, S4, C13): each machine's value under laneKey(kind) reads alike from its table
// wire, the padded standard base64 of that wire, the schema JSON text and today's legacy JSON;
// a malformed value of each form is refused by name (<RECORD> REFUSED: <reason>; run:
// nova-sprint check), never read as no lane held; reads are counted per record and per form.
// The record is a map of machines, so its envelope is today's JSON object and each value reads
// through the generated Lane table, as the friend roster reads each entry (presenceRowMap).
// Every writer still writes the legacy form (S4). It runs on the in-memory backend only
// (store.NewMem), with the clock at 1000.
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
	rows := st.lanesRows()
	require.Same(t, rows, st.lanesRows(), "one reader per store")
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	require.Same(t, rows, pinned.lanesRows(), "a pinned copy counts into the store's reader")

	at := func(s int64) time.Time { return time.Unix(1000+s, 0).UTC() }
	lane := sprint.Lane{
		Holders: []sprint.LaneHold{{Who: "worker-a", Since: at(1), Seen: at(2), Claimed: true}},
		Queue:   []sprint.LaneWait{{Who: "worker-b", Since: at(3), Seen: at(4)}},
	}

	// One value reads alike from every form (S2).
	lf := rosterFormsOf(t, rows, lane)

	// The read site reads each machine's value in any form: today's legacy object, the base64
	// of the table wire held as a JSON string, the schema JSON text held whole (it loses its
	// closing newline in the object and reads as legacy) and the schema JSON text as a JSON
	// string, which keeps the newline and reads as the json form (S2).
	js, err := json.Marshal(string(lf.js))
	require.NoError(t, err)
	written := `{"ann":` + string(lf.legacy) + `,"bea":"` + string(lf.b64) + `","cid":` + string(lf.js) + `,"dee":` + string(js) + `}`
	require.NoError(t, kv.SetKey(ctx, laneKey("go"), written))
	got, _, err := st.lanes(ctx, "go")
	require.NoError(t, err)
	assert.Equal(t, sprint.Lanes{"ann": lane, "bea": lane, "cid": lane, "dee": lane}, got)

	// The read is a read: the legacy form is still what is stored (S4: readers first).
	raw, ok, err := kv.GetKey(ctx, laneKey("go"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, written, raw)

	// Reads are counted per record and per form on Check (S4). rosterFormsOf read each form
	// once; the record read added two legacy (ann, cid), one base64 (bea) and one json (dee).
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	counts := report.RowForms["LANES"]
	require.NotNil(t, counts)
	assert.EqualValues(t, 1, counts["binary"])
	assert.EqualValues(t, 2, counts["base64"])
	assert.EqualValues(t, 2, counts["json"])
	assert.EqualValues(t, 3, counts["legacy"])

	// A malformed value of each form is refused by name, never read as none.
	for _, tc := range []struct {
		what string
		in   []byte
	}{
		{"binary truncated", lf.bin[:len(lf.bin)/2]},
		{"binary form byte", append([]byte{lf.bin[0] ^ 0xff}, lf.bin[1:]...)},
		{"base64 truncated", lf.b64[:len(lf.b64)-1]},
		{"base64 not text", []byte("!!!!")},
		{"json truncated", lf.js[:len(lf.js)/2]},
		{"json broken", []byte(`{"holders":`)},
		{"legacy truncated", lf.legacy[:len(lf.legacy)/2]},
		{"legacy broken", []byte(`{"who":`)},
		// Lane declares no bounded field (schemas/friendrows/friendrows.schema Lane), so a
		// clamped value cannot arise; a value of the wrong kind is refused as malformed.
		{"wrong kind", []byte(`{"holders":1}`)},
	} {
		assertRefused(t, rows, "LANES", tc.in, tc.what)
	}

	// A damaged record at the read site is refused by name, never read as no lane held.
	for _, in := range []string{
		`{"ann":`,
		`{"ann":"!!!!"}`,
		`{"ann":` + string(lf.legacy[:len(lf.legacy)/2]) + `}`,
	} {
		require.NoError(t, kv.SetKey(ctx, laneKey("go"), in))
		out, _, err := st.lanes(ctx, "go")
		require.Error(t, err, in)
		assert.Nil(t, out, in)
		assert.Regexp(t, regexp.MustCompile(`^LANES REFUSED: [a-z_]+; run: nova-sprint check$`), err.Error(), in)
	}

	// The refusals are counted under the record and their reason (S4).
	report, _, err = st.Check(ctx, 1)
	require.NoError(t, err)
	refused := uint64(0)
	for form, n := range report.RowForms["LANES"] {
		if bytes.HasPrefix([]byte(form), []byte("refused_")) {
			refused += n
		}
	}
	assert.Positive(t, refused)
}
