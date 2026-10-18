package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/friendrowswire"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStrangersWriteFixed pins the strangers writer's switch to the fixed table form (S1,
// S4): putStrangers writes the fixed table form and what it writes reads back equal through
// the dual reader; an image too large for one value is paged, never truncated; a value the
// fixed form cannot hold is refused by name before a byte is written, never written in the
// legacy form as a fallback; and a legacy fixture still reads.
func TestStrangersWriteFixed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "strangers-fixed-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)

	strangers := map[string]stranger{
		"box-1": {Seen: now.Add(1 * time.Second), Told: true},
		"box-2": {Seen: now.Add(2 * time.Second), Told: false},
	}

	// The writer writes the fixed table form (S1, S4).
	require.NoError(t, st.putStrangers(ctx, strangers))
	raw, ok, err := kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, friendrowswire.TableFixedForm, raw[0], "the strangers record is written in the fixed table form")

	// What it writes reads back equal through the dual reader (S4).
	got, err := st.strangers(ctx)
	require.NoError(t, err)
	assert.Equal(t, strangers, got)
	// That read went through the codec and is counted under STRANGERS and its form (S2, S4).
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, report.RowForms, "STRANGERS")
	assert.Positive(t, report.RowForms["STRANGERS"]["binary"], "fixed-form strangers reads are counted")

	// The empty record round-trips.
	require.NoError(t, st.putStrangers(ctx, map[string]stranger{}))
	got, err = st.strangers(ctx)
	require.NoError(t, err)
	assert.Empty(t, got)

	// An image too large for one value is paged (S1), never truncated.
	many := map[string]stranger{}
	for i := range 95 {
		many[fmt.Sprintf("box-%03d", i)] = stranger{Seen: now.Add(time.Duration(i) * time.Second), Told: i%2 == 0}
	}
	require.NoError(t, st.putStrangers(ctx, many))
	p0, ok, err := kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, friendrowswire.TableFixedForm, p0[0])
	p1, ok1, err := kv.GetKey(ctx, "strangers:1")
	require.NoError(t, err)
	require.True(t, ok1, "a second page holds the overflow")
	_, _ = p0, p1
	got, err = st.strangers(ctx)
	require.NoError(t, err)
	assert.Equal(t, many, got, "paged strangers read back equal")

	// A narrower record drops the surplus pages the wider record left (S1).
	require.NoError(t, st.putStrangers(ctx, strangers))
	_, ok2, err := kv.GetKey(ctx, "strangers:1")
	require.NoError(t, err)
	require.False(t, ok2, "a narrower record drops the surplus page")
	got, err = st.strangers(ctx)
	require.NoError(t, err)
	assert.Equal(t, strangers, got)

	// A missing follow-on page is a named, counted refusal, not a silent success (S2).
	require.NoError(t, st.putStrangers(ctx, many))
	p0, ok, err = kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = st.B.DeleteKeys(ctx, []string{st.Names.Key("strangers:1")})
	require.NoError(t, err)
	_, _, why, err := st.readStrangersFrom(ctx, kv, p0, true)
	require.NoError(t, err)
	assert.Equal(t, "missing_page", why)
	got, err = st.strangers(ctx)
	require.NoError(t, err)
	assert.Empty(t, got)

	// A page refused as a newer layout is never written over (S1, S4).
	require.NoError(t, st.putStrangers(ctx, strangers))
	p0, ok, err = kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	require.True(t, ok)
	newer := []byte(p0)
	newer[friendrowswire.TableFixedHashAt] ^= 0xff
	require.NoError(t, kv.SetKey(ctx, keyStrangers, string(newer)))
	_, _, why, err = st.readStrangersFrom(ctx, kv, string(newer), true)
	require.NoError(t, err)
	assert.Equal(t, "layout_newer", why)
	require.ErrorContains(t, st.putStrangers(ctx, strangers), "layout_newer")
	afterNewer, _, err := kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	assert.Equal(t, string(newer), afterNewer, "a newer layout is never overwritten")
	require.NoError(t, kv.SetKey(ctx, keyStrangers, p0))

	// A value the fixed form cannot hold is refused by name before a byte is written (a
	// name past the 128-byte ValidID bound), never written in the legacy form as a
	// fallback.
	require.NoError(t, st.putStrangers(ctx, strangers))
	overlong := strings.Repeat("m", 129)
	before, _, err := kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	err = st.putStrangers(ctx, map[string]stranger{overlong: {Seen: now}})
	require.Error(t, err)
	assert.ErrorContains(t, err, "text_over_bound")
	after, _, err := kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	assert.Equal(t, before, after, "no byte is written on refusal")

	// A legacy fixture of the same record still reads (S2, S4).
	legacy := []byte(`{"box-1":{"seen":"1970-01-01T00:16:41Z","told":true}}`)
	require.NoError(t, kv.SetKey(ctx, keyStrangers, string(legacy)))
	got, err = st.strangers(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]stranger{"box-1": {Seen: time.Unix(1001, 0).UTC(), Told: true}}, got)
}
