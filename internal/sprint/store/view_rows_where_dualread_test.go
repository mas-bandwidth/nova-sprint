package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewRowsWhereDualRead is the dual read of the where, store round trip, stats, machine
// and heartbeat records (where.go), the stops record (stops.go), the check record
// (check_record.go) and the stuck record (stuck.go), Aspect 8 of the schema-table adoption:
// each record, every field of it set, reads alike from its variable table's binary wire,
// the padded standard base64 of that wire, the schema JSON text and the legacy JSON every
// writer still writes; every truncation and every flipped byte of the wire, its base64 and
// both texts is refused by name, never read as a value; a clamped value is refused; and the
// reads are counted per record and per form on the Store's readers, which its pinned copies
// share (S1, S2, S4). The store is the in-memory backend and the clock starts at 1000.
func TestViewRowsWhereDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "view-rows-where-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	rows := st.viewRows()
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	require.Same(t, rows, pinned.viewRows(), "a pinned copy reads with its store's readers")

	dualRead(t, rows.where, "WHERERECORD", `"epoch":-1`, "epoch")
	dualRead(t, rows.rtt, "STORERTTRECORD", `"at":9223372036854775808`, "at")
	dualRead(t, rows.stats, "STATSRECORD", `"epoch":-1`, "epoch")
	dualRead(t, rows.machine, "MACHINE", `"run_seq":-1`, "run_seq")
	dualRead(t, rows.heartbeat, "HEARTBEAT", `"revisions":[1,2,3,4,5]`, "revisions")
	dualRead(t, rows.stops, "STOPSRECORD", `"epoch":-1`, "epoch")
	dualRead(t, rows.check, "CHECKRECORD", `"epoch":-1`, "epoch")
	dualRead(t, rows.stuck, "STUCK", `"at":9223372036854775808`, "at")

	t.Run("read sites", func(t *testing.T) { viewReadSites(t, st) })
}

// dualRead pins one record: a value with every field set reads back equal from each form,
// every damaged value is refused by name and the reads are counted on their forms. clamp
// is a schema JSON member whose value no reader holds, set in place of field.
func dualRead[T any](t *testing.T, c *RowCodec[T], name, clamp, field string) {
	t.Helper()
	before := c.Counts().Snapshot()

	var want T
	n := 1
	viewFill(reflect.ValueOf(&want).Elem(), &n)
	wire, err := c.Write(&want, schemaread.FormBinary)
	require.NoError(t, err, name)
	require.NotEmpty(t, wire, "%s: the wire writer maps every field", name)
	require.Equal(t, viewWireForm, wire[0], "%s: the variable table's form byte", name)
	b64, err := c.Write(&want, schemaread.FormBase64)
	require.NoError(t, err, name)
	assert.Equal(t, base64.StdEncoding.EncodeToString(wire), string(b64), "%s: padded standard base64 of the wire", name)
	text, err := c.Write(&want, schemaread.FormJSON)
	require.NoError(t, err, name)
	require.NotEmpty(t, text, name)
	legacy, err := json.Marshal(want)
	require.NoError(t, err, name)

	forms := []struct {
		form schemaread.Form
		in   []byte
	}{{schemaread.FormBinary, wire}, {schemaread.FormBase64, b64}, {schemaread.FormJSON, text}, {schemaread.FormLegacy, legacy}}
	for _, f := range forms {
		got, form, err := c.Read(f.in)
		require.NoError(t, err, "%s reads its %s form", name, f.form)
		assert.Equal(t, f.form, form, name)
		assert.Equal(t, want, got, "%s: the %s form reads the one value", name, f.form)
	}

	refusal := regexp.MustCompile(`^` + name + ` REFUSED: [a-z_]+( \([^)]*\))?; run: nova-sprint check$`)
	refused := func(what string, in []byte) {
		t.Helper()
		got, _, err := c.Read(in)
		var ref *schemaread.Refusal
		require.True(t, errors.As(err, &ref), "%s: %s is refused, not read: %v", name, what, err)
		assert.Regexp(t, refusal, err.Error(), "%s: %s", name, what)
		var zero T
		assert.Equal(t, zero, got, "%s: %s reads nothing", name, what)
	}
	damaged := 0
	for _, f := range forms {
		for _, i := range viewSpots(len(f.in)) {
			refused(fmt.Sprintf("the %s form cut to %d bytes", f.form, i), f.in[:i])
			damaged++
		}
		if f.form == schemaread.FormBinary {
			continue
		}
		for _, i := range viewSpots(len(f.in)) {
			bad := append([]byte(nil), f.in...)
			bad[i] = 0 // a NUL: no text form holds one
			refused(fmt.Sprintf("the %s form with byte %d damaged", f.form, i), bad)
			damaged++
		}
	}
	// a flipped byte of the wire: the framing check (the trailer) refuses every flip of
	// itself and of the framing; a flip inside a value's own bytes or a field id is still a
	// well-formed wire (the variable table carries no checksum over its body), so it reads
	// as another value or with an unknown field, never as the zero value
	for _, i := range viewSpots(len(wire)) {
		bad := append([]byte(nil), wire...)
		bad[i] ^= 0xff
		if i >= len(wire)-8 {
			refused(fmt.Sprintf("the binary form with trailer byte %d flipped", i), bad)
			damaged++
			continue
		}
		got, _, err := c.Read(bad)
		var zero T
		if err == nil {
			assert.NotEqual(t, zero, got, "%s: the binary form with byte %d flipped never reads as zero", name, i)
			continue
		}
		var ref *schemaread.Refusal
		require.True(t, errors.As(err, &ref), "%s: byte %d: %v", name, i, err)
		assert.Regexp(t, refusal, err.Error(), "%s: byte %d", name, i)
		assert.Equal(t, zero, got, "%s: byte %d", name, i)
		damaged++
	}

	// a clamped value: a number no reader of the record holds (S2: clamped is refused)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(text, &doc), name)
	delete(doc, field)
	rest, err := json.Marshal(doc)
	require.NoError(t, err, name)
	clamped := append([]byte("{"+clamp+","), rest[1:]...)
	clamped = append(clamped, '\n')
	_, _, err = c.Read(clamped)
	var ref *schemaread.Refusal
	require.True(t, errors.As(err, &ref), "%s: a clamped value is refused: %v", name, err)
	assert.Equal(t, schemaread.ReasonClamped, ref.Reason, "%s: %s", name, err)
	damaged++

	after := c.Counts().Snapshot()
	for _, form := range []string{"binary", "base64", "json", "legacy"} {
		assert.Positive(t, after[form]-before[form], "%s: reads counted as %s", name, form)
	}
	var refusals uint64
	for k, v := range after {
		if len(k) > len("refused_") && k[:len("refused_")] == "refused_" {
			refusals += v - before[k]
		}
	}
	assert.EqualValues(t, damaged, refusals, "%s: every refusal is counted by its reason", name)
}

// viewReadSites pins the read sites: every writer still writes the legacy JSON, each site
// reads it through the Store's reader, and a damaged value is the named refusal where the
// site hands it on, or a counted refusal where the site reads it as none on purpose.
func viewReadSites(t *testing.T, st *Store) {
	ctx := context.Background()
	rows := st.viewRows()
	kv, err := st.kv()
	require.NoError(t, err)
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	legacyOf := func(c interface{ Counts() *schemaread.Counts }) uint64 {
		return c.Counts().Reads(schemaread.FormLegacy)
	}

	// the stuck record: written legacy by markStuck, read by stuck and by stuckOf
	op := OpRecord{ID: "op-1", Verb: "take", At: time.Unix(990, 0).UTC()}
	n := legacyOf(rows.stuck)
	require.NoError(t, pinned.markStuck(ctx, op, "grace"))
	s, ok, err := pinned.stuck(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "op-1", s.Op)
	assert.Equal(t, n+1, legacyOf(rows.stuck), "markStuck finds none, then stuck reads it")
	raw, _, err := kv.GetKey(ctx, keyStuck)
	require.NoError(t, err)
	got, ok, err := stuckOf(Fence{Stuck: raw})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, s, got)
	require.NoError(t, kv.SetKey(ctx, keyStuck, `{"op":`))
	_, ok, err = pinned.stuck(ctx)
	assert.EqualError(t, err, "the machine's stuck record is unreadable: STUCK REFUSED: legacy_json_unreadable; run: nova-sprint check")
	assert.False(t, ok)
	_, ok, err = stuckOf(Fence{Stuck: `{"op":`})
	assert.EqualError(t, err, "the machine's stuck record is unreadable: STUCK REFUSED: legacy_json_unreadable; run: nova-sprint check")
	assert.False(t, ok)
	require.NoError(t, kv.SetKey(ctx, keyStuck, ""))

	// the check record: written legacy by PutCardCheck, read by CardCheck; a damaged one is
	// none (start refuses with none) and its refusal is counted
	n = legacyOf(rows.check)
	put, err := st.PutCardCheck(ctx, CheckRecord{By: "tester", Clean: true, Cards: []sprint.CheckedCard{{ID: "c1", Stream: "s", Brief: "b"}}, Digest: "d"})
	require.NoError(t, err)
	rec, ok, err := st.CardCheck(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, put.Digest, rec.Digest)
	assert.Equal(t, n+1, legacyOf(rows.check))
	refusedBefore := rows.check.Counts().Refused("legacy_json_unreadable")
	require.NoError(t, kv.SetKey(ctx, keyCheckRecord, `{"epoch":`))
	_, ok, err = st.CardCheck(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "a damaged check record is none: start refuses")
	assert.Equal(t, refusedBefore+1, rows.check.Counts().Refused("legacy_json_unreadable"))

	// the stops record: StopsKept hands a damaged one on by name
	stops := StopsRecord{Epoch: pinned.epoch, At: time.Unix(1000, 0).UTC(), Stops: []sprint.Stop{{Kind: "k", Subject: "s", What: "w"}}}
	b, err := json.Marshal(stops)
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, keyStops, string(b)))
	kept, ok, err := pinned.StopsKept(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, stops, kept)
	require.NoError(t, kv.SetKey(ctx, keyStops, `{"epoch":`))
	_, ok, err = pinned.StopsKept(ctx)
	assert.EqualError(t, err, "STOPSRECORD REFUSED: legacy_json_unreadable; run: nova-sprint check")
	assert.False(t, ok)

	// the store round trip record: written legacy by RecordStoreRTT, read back by the next
	n = legacyOf(rows.rtt)
	require.NoError(t, pinned.RecordStoreRTT(ctx, 2*time.Millisecond))
	require.NoError(t, pinned.RecordStoreRTT(ctx, 3*time.Millisecond))
	assert.Equal(t, n+1, legacyOf(rows.rtt), "the second write reads the first")

	// WhereFacts: the machine and heartbeat records hand a damaged one on by name; the
	// stats, stops and where records read as none on purpose, each refusal counted
	m := Machine{State: "RUNNING", Since: time.Unix(999, 0).UTC(), Who: "tester"}
	hb := Heartbeat{At: time.Unix(1000, 0).UTC(), Ticks: 3, Revisions: [4]uint64{1, 2, 3, 4}}
	for key, v := range map[string]any{keyMachine: m, keyHeartbeat: hb, keyWhere: WhereRecord{Epoch: pinned.epoch, Rev: 1}, keyStats: StatsRecord{Epoch: pinned.epoch}} {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		require.NoError(t, kv.SetKey(ctx, key, string(b)))
	}
	mBefore, hBefore, wBefore, sBefore := legacyOf(rows.machine), legacyOf(rows.heartbeat), legacyOf(rows.where), legacyOf(rows.stats)
	f, err := pinned.WhereFacts(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, m.State, f.Machine.State)
	assert.Equal(t, hb.Revisions, f.Heartbeat.Revisions)
	assert.Equal(t, mBefore+1, legacyOf(rows.machine))
	assert.Equal(t, hBefore+1, legacyOf(rows.heartbeat))
	assert.Equal(t, wBefore+1, legacyOf(rows.where))
	assert.Positive(t, legacyOf(rows.stats)-sBefore)
	require.NoError(t, kv.SetKey(ctx, keyWhere, `{"epoch":`))
	whereRefused := rows.where.Counts().Refused("legacy_json_unreadable")
	_, err = pinned.WhereFacts(ctx, 1)
	require.NoError(t, err, "a damaged where record is none: where counts the cards")
	assert.Equal(t, whereRefused+1, rows.where.Counts().Refused("legacy_json_unreadable"))
	require.NoError(t, kv.SetKey(ctx, keyHeartbeat, `{"at":`))
	_, err = pinned.WhereFacts(ctx, 1)
	assert.EqualError(t, err, "HEARTBEAT REFUSED: legacy_json_unreadable; run: nova-sprint check")
	require.NoError(t, kv.SetKey(ctx, keyMachine, `{"state":`))
	_, err = pinned.WhereFacts(ctx, 1)
	assert.EqualError(t, err, "MACHINE REFUSED: legacy_json_unreadable; run: nova-sprint check")
}

// viewSpots is the offsets a damage is put at in a value of n bytes: every one of a short
// value, and of a long one the first and last 64 and 128 spread between, so the test stays
// cheap and still reaches every part of the value.
func viewSpots(n int) []int {
	if n <= 256 {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	var out []int
	for i := 0; i < 64; i++ {
		out = append(out, i)
	}
	for k := 1; k <= 128; k++ {
		out = append(out, 64+k*(n-128)/129)
	}
	for i := n - 64; i < n; i++ {
		out = append(out, i)
	}
	return out
}

// viewFill sets every exported field of v to a value of its own (n counts them), as a
// writer could: times in UTC to the nanosecond, two entries in each map and list.
func viewFill(v reflect.Value, n *int) {
	*n++
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == viewTimeType {
			v.Set(reflect.ValueOf(time.Unix(1000+int64(*n), int64(*n)).UTC()))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !f.IsExported() || f.Tag.Get("json") == "-" {
				continue
			}
			viewFill(v.Field(i), n)
		}
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		viewFill(p.Elem(), n)
		v.Set(p)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		for _, k := range []string{"a", "b"} {
			val := reflect.New(v.Type().Elem()).Elem()
			viewFill(val, n)
			m.SetMapIndex(reflect.ValueOf(fmt.Sprintf("%s%d", k, *n)).Convert(v.Type().Key()), val)
		}
		v.Set(m)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := 0; i < 2; i++ {
			viewFill(s.Index(i), n)
		}
		v.Set(s)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			viewFill(v.Index(i), n)
		}
	case reflect.String:
		v.SetString(fmt.Sprintf("text-%d", *n))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(*n))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(*n))
	case reflect.Float32, reflect.Float64:
		v.SetFloat(float64(*n) + 0.25)
	case reflect.Bool:
		v.SetBool(true)
	}
}
