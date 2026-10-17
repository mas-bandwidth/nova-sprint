package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServerRecordDualRead pins the reader-first dual read of the server record
// (ServerRecord) as Server reads it under keyServer (seat.go): its fixed table
// image, the padded standard base64 of that image, its schema JSON text and
// today's legacy JSON all decode to one equal value, while a malformed value of
// each form is refused by name and counted per form (S2, S4, C13).
func TestServerRecordDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "server-dual-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))

	want := ServerRecord{
		Actor: "emma", At: time.Unix(1001, 0).UTC(), Version: "v1.5.0",
		Commit: "abc123", Started: time.Unix(999, 0).UTC(), Land: true, PID: 4242, Host: "h1",
	}

	codec := st.serverRows()
	zero := codec.Counts().Snapshot()
	assert.Zero(t, zero["binary"])
	assert.Zero(t, zero["legacy"])

	binary, err := codec.Write(&want, schemaread.FormBinary)
	require.NoError(t, err)
	b64, err := codec.Write(&want, schemaread.FormBase64)
	require.NoError(t, err)
	jtext, err := codec.Write(&want, schemaread.FormJSON)
	require.NoError(t, err)
	legacy, err := json.Marshal(want)
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"binary", binary},
		{"base64", b64},
		{"schema-json", jtext},
		{"legacy", legacy},
	} {
		got, _, err := codec.Read(tc.in)
		require.NoError(t, err, "%s: read refused", tc.name)
		assert.Equal(t, want, got, "%s: value mismatch", tc.name)
	}

	requireServerRefused := func(t *testing.T, name string, in []byte) {
		t.Helper()
		_, _, err := codec.Read(in)
		require.Error(t, err, "%s: malformed read accepted", name)
		msg := err.Error()
		assert.Contains(t, msg, "SERVERRECORD REFUSED:", name)
		assert.Contains(t, msg, "; run: nova-sprint check", name)
	}

	requireServerRefused(t, "binary truncated", binary[:len(binary)/2])
	flip := append([]byte(nil), binary...)
	flip[0] ^= 0xff
	requireServerRefused(t, "binary flipped", flip)
	requireServerRefused(t, "base64 truncated", []byte(b64[:len(b64)-1]))
	decoded, err := base64.StdEncoding.DecodeString(string(b64))
	require.NoError(t, err)
	decoded[0] ^= 0xff
	bad64 := []byte(base64.StdEncoding.EncodeToString(decoded))
	requireServerRefused(t, "base64 flipped", bad64)
	requireServerRefused(t, "schema-json kind mismatch", []byte("{\"actor\":123}\n"))
	requireServerRefused(t, "legacy truncated", legacy[:len(legacy)/2])
	requireServerRefused(t, "legacy unreadable", []byte(`{"actor":`))

	// The server record declares no bounds (friendrows_golden_test.go
	// onlyRealBounds, want empty), so no value of it clamps; the strict reader
	// does not hand back a clamped value. Every refusal above is named and
	// never read as absent, empty or zero.
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	counts := report.RowForms["SERVERRECORD"]
	require.NotNil(t, counts)
	assert.Equal(t, uint64(3), counts["binary"])
	assert.Equal(t, uint64(3), counts["base64"])
	assert.Equal(t, uint64(2), counts["json"])
	assert.Equal(t, uint64(3), counts["legacy"])
	assert.Equal(t, uint64(2), counts["refused_legacy_json_unreadable"])

	serverCodecs.Delete(st)
}

func TestServerReadsThroughRowCodec(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "server-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	require.NoError(t, st.SetServerActor(ctx, "emma"))

	server, ok, err := st.Server(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "emma", server.Actor)
	assert.Equal(t, now, server.At)

	st.B.(KV).SetKey(ctx, keyServer, `{"actor":`)
	_, _, err = st.Server(ctx)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "SERVERRECORD REFUSED:"))
	assert.True(t, strings.Contains(err.Error(), "; run: nova-sprint check"))

	serverCodecs.Delete(st)
}
