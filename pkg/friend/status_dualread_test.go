package friend

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/friendfilewire"
)

func timeNowForTest() time.Time {
	return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
}

func statusJSONKeys() []string {
	var keys []string
	shape := reflect.TypeOf(Status{})
	for i := 0; i < shape.NumField(); i++ {
		tag := shape.Field(i).Tag.Get("json")
		name := tag
		if i := strings.IndexByte(tag, ','); i >= 0 {
			name = tag[:i]
		}
		keys = append(keys, name)
	}
	return keys
}

// TestSprintFriendStatusDualRead pins the friend status dual read: the file
// <state dir>/status.json is read alike from its binary image, its padded
// standard base64, the schema JSON text and today's legacy form, damage is
// refused by name, and reads are counted per form. The table must cover every
// Status field for the four forms to read alike; a field the table lacks stops
// the card HOLD (RECIPE step 2). The generated FriendStatus table (nova-tools
// pkg/friendfilewire, v1.5.0) still lacks the status fields below, so the four
// forms cannot read alike until its schema gains them (a table change is its
// own card); this test is red at head and pins the gap.
func TestSprintFriendStatusDualRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	full := fullStatusForTest()
	if err := write(statusPath(dir), full); err != nil {
		t.Fatal(err)
	}
	got, found, err := ReadStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("status not found")
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("legacy read differs:\n got %+v\nwant %+v", got, full)
	}

	// The FriendStatus table must cover every Status field: a field the table
	// lacks cannot ride the binary, base64 or schema JSON forms, so the forms
	// cannot read alike. This assertion is red while state.schema lags Status.
	covered := map[string]bool{}
	for _, f := range friendfilewire.FriendStatusTableType().Fields {
		covered[f.Json] = true
	}
	missing := []string{}
	for _, key := range statusJSONKeys() {
		if !covered[key] {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		t.Errorf("FriendStatus table lacks Status fields: %v", missing)
	}
}

func fullStatusForTest() Status {
	now := timeNowForTest()
	return Status{
		Friend: "friend", Harness: "harness", Started: now, At: now,
		Connection: "conn", LastPing: now, Seat: "seat", SeatSince: now,
		Challenge: "challenge", Nonce: "nonce", LastPong: now, Pongs: 1,
		LastDaemonPong: now, Delivered: 2, Beats: 3, LastBeat: now,
		BeatError: "", Envelope: 4, EnvelopeBytes: 5,
		Push: "proof", PushNonce: "nonce", PushSince: now, ProofSent: now,
		HarnessSeen: "seen", HarnessAlive: "ok", StoreError: "",
		Width: 6, Session: "ok", SessionID: "id", SessionReason: "",
		SessionLive: "live", Queued: 7, QueueKnown: true, BrokenAt: now,
		LimitKind: "limit", LimitUntil: now, Mode: "batch", Lanes: "1:1:1",
		Window: "5h", Pacing: "62%", HeldKnown: true, Held: 8,
		InboxJobs: 9, Missing: 10, InboxError: "", HeldFrom: "cards",
	}
}
