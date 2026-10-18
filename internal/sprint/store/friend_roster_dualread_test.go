package store

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store/friendrowswire"
	"github.com/mas-bandwidth/nova-tools/pkg/hostload"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rosterForms is one value of a record in each form a reader meets: the table wire, the
// padded standard base64 of that wire, the schema JSON text and today's legacy JSON (S2).
type rosterForms struct {
	bin, b64, js, legacy []byte
}

// rosterFormsOf writes v in every form and reads each back through c, equal to v and counted
// under the form it was written in.
func rosterFormsOf[T any](t *testing.T, c *RowCodec[T], v T) rosterForms {
	t.Helper()
	var f rosterForms
	var err error
	f.bin, err = c.Write(&v, schemaread.FormBinary)
	require.NoError(t, err)
	require.NotEmpty(t, f.bin)
	f.b64, err = c.Write(&v, schemaread.FormBase64)
	require.NoError(t, err)
	f.js, err = c.Write(&v, schemaread.FormJSON)
	require.NoError(t, err)
	f.legacy, err = json.Marshal(v)
	require.NoError(t, err)
	for _, tc := range []struct {
		in   []byte
		form schemaread.Form
	}{{f.bin, schemaread.FormBinary}, {f.b64, schemaread.FormBase64}, {f.js, schemaread.FormJSON}, {f.legacy, schemaread.FormLegacy}} {
		got, form, err := c.Read(tc.in)
		require.NoError(t, err, tc.form.String())
		assert.Equal(t, tc.form, form)
		assert.Equal(t, v, got, tc.form.String())
	}
	return f
}

// assertRefused says in is refused by name, `<NAME> REFUSED: <reason>; run: nova-sprint
// check`, and read as the zero value, never as a record.
func assertRefused[T any](t *testing.T, c *RowCodec[T], name string, in []byte, what string) {
	t.Helper()
	got, _, err := c.Read(in)
	require.Error(t, err, what)
	assert.Regexp(t, regexp.MustCompile(`^`+name+` REFUSED: [a-z_]+( \([a-z_]+\))?; run: nova-sprint check$`), err.Error(), what)
	var refusal *schemaread.Refusal
	assert.ErrorAs(t, err, &refusal, what)
	assert.True(t, reflect.ValueOf(&got).Elem().IsZero(), what)
}

// assertDamageRefused feeds every truncation of every form of a record (the empty value
// included) and every single-byte flip of each: a truncation is refused by name in every
// case; a flip is refused by name, or read as a record that is not the zero value, never
// silently as none.
func assertDamageRefused[T any](t *testing.T, c *RowCodec[T], name string, f rosterForms) {
	t.Helper()
	for _, form := range []struct {
		what string
		in   []byte
		end  int
	}{
		{"binary", f.bin, len(f.bin)}, {"base64", f.b64, len(f.b64)},
		// the schema JSON text without only its closing newline is a whole legacy text
		{"json", f.js, len(bytes.TrimRight(f.js, "\n"))},
		{"legacy", f.legacy, len(f.legacy)},
	} {
		for i := 0; i < form.end; i++ {
			assertRefused(t, c, name, form.in[:i], form.what+" truncated")
		}
		refused := 0
		for i := range form.in {
			in := bytes.Clone(form.in)
			in[i] ^= 0xff
			got, _, err := c.Read(in)
			if err != nil {
				assert.Regexp(t, `^`+name+` REFUSED: `, err.Error())
				refused++
				continue
			}
			assert.False(t, reflect.ValueOf(&got).Elem().IsZero(), "%s flipped at %d read as the zero value", form.what, i)
		}
		assert.Positive(t, refused, form.what+" flips")
	}
	// the form byte is the framing check: a flipped one is refused (S2)
	flipped := bytes.Clone(f.bin)
	flipped[0] ^= 0xff
	assertRefused(t, c, name, flipped, "binary form byte")
}

// TestFriendRosterRowsDualRead pins the reader-first dual read of the friend roster and
// presence rows (S4, C13): the roster entry, the derived friend status, the member and friend
// beats and the strangers record read alike from the table wire, the padded standard base64 of
// that wire and today's legacy JSON, at the record and at each store read site, while every
// writer still writes legacy; a damaged or clamped value of each form is refused by name and
// never read as none; reads are counted per record and per form on Check. It runs on the
// in-memory backend only (store.NewMem), with the clock at 1000.
func TestFriendRosterRowsDualRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(1000, 0).UTC()
	st := &Store{
		B: NewMem(), Names: sprint.Names{Prefix: "friend-dual-read-"}, Actor: "tester",
		Now: func() time.Time { return now }, Sleep: func(time.Duration) {},
	}
	require.NoError(t, st.Init(ctx))
	kv, err := st.rootKV()
	require.NoError(t, err)
	rows := st.presenceRows()
	require.Same(t, rows.entry, st.presenceRows().entry)
	pinned, err := st.pin(ctx)
	require.NoError(t, err)
	require.Same(t, rows.beat, pinned.presenceRows().beat, "a pinned copy counts into the store's readers")

	at := func(s int64) time.Time { return time.Unix(1000+s, 0).UTC() }
	entry := friendEntry{
		Held: true, At: at(1), By: "coord", Width: 2, Class: "flash,pro", Mode: "batch", Streams: "v15-*", Kinds: "feature",
		ConfigDir: "/home/f/.claude", Pronouns: "she/her", Session: sprint.FriendOneShot, TokenCap: 0, TokenCapSet: true,
		Roles: "builder,reader", Billing: "plan", Reason: "away", Until: time.Unix(2000, 5).UTC(), Return: true,
	}
	status := friendStatus{Status: sprint.Up, Reason: "proven", Until: at(30), Evidence: "nova-hook proven", WorkerLive: true, Unhooked: true, At: at(2)}
	one, two, three, zero := 1, 2, 3, 0
	beat := sprint.Beat{
		At: at(3), Load: 55.5, Cores: 8, How: sprint.HowGiven,
		Samples: []sprint.LoadSample{{At: at(2), Pct: 40.25}, {At: at(3), Pct: 55.5}},
		Meter: hostload.State{
			Ticks: &hostload.Ticks{Busy: 10, Total: 1 << 60}, TopAt: at(1), TopPct: 12.5,
			Files: &hostload.Files{At: at(1), Open: 300, Max: 1000, Warn: 500, Alarm: 900, Top: []hostload.Holder{{PID: 42, Command: "node", User: "build", Open: 200}}, TopAt: at(1), TopErr: "none"},
		},
		Friend: &sprint.FriendReport{
			Running: []string{"c1", "c2"}, Working: &one, Queue: &two, Width: &three, StopReturns: &zero, Paced: &two,
			Active: at(1), Window: "5h 62% 7d 31%", Until: at(60), Reason: "limit", Build: "v1.5.0", Started: at(-5), Present: at(-4),
		},
		StopReturns: 1, NoRoom: "disk", Live: []string{"c1@1", "c2@1:held"}, LiveKnown: true, LiveSince: at(-10),
		LiveSeen: map[string]time.Time{"c1@1": at(3), "c2@1:held": time.Unix(1003, 7).UTC()}, Draining: true, DrainingSince: at(-1),
		Facts: []string{"linux", "amd64"},
	}
	machine := sprint.Beat{At: at(4), Load: 20, Cores: 4, How: "cpu", Meter: hostload.State{Ticks: &hostload.Ticks{Busy: 1, Total: 2}}}
	strangers := map[string]stranger{"box-1": {Seen: at(5), Told: true}}

	// Every record reads alike from each form (S2).
	ef := rosterFormsOf(t, rows.entry, entry)
	sf := rosterFormsOf(t, rows.status, status)
	bf := rosterFormsOf(t, rows.beat, beat)
	mf := rosterFormsOf(t, rows.beat, machine)

	// The read sites read every form: the roster (friends.go roster), the derived status
	// (storedFriendStatus), the friend beat (FriendBeatOf, FriendBeats), the member beats
	// (Beats) and the strangers record (presence.go strangers).
	require.NoError(t, kv.SetKey(ctx, keyFriends, `{"ann":`+string(ef.legacy)+`,"bea":"`+string(ef.b64)+`","cid":`+string(ef.js)+`}`))
	r, _, err := st.roster(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]friendEntry{"ann": entry, "bea": entry, "cid": entry}, r)

	require.NoError(t, kv.SetKey(ctx, keyFriendStatus, `{"ann":`+string(sf.legacy)+`,"bea":"`+string(sf.b64)+`"}`))
	fs, err := st.storedFriendStatus(ctx, kv)
	require.NoError(t, err)
	assert.Equal(t, map[string]friendStatus{"ann": status, "bea": status}, fs)

	for name, in := range map[string][]byte{"ann": bf.bin, "bea": bf.b64, "cid": bf.legacy} {
		require.NoError(t, kv.SetKey(ctx, friendBeatKey(name), string(in)))
		got, err := st.FriendBeatOf(ctx, name)
		require.NoError(t, err)
		assert.Equal(t, beat, got, name)
	}
	fbs, err := st.FriendBeats(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]sprint.Beat{"ann": beat, "bea": beat, "cid": beat}, fbs)

	members := []string{"m1", "m2", "m3", "m4"}
	for i, in := range [][]byte{mf.bin, mf.b64, mf.js, mf.legacy} {
		require.NoError(t, kv.SetKey(ctx, beatKey(members[i]), string(in)))
	}
	bs, err := st.Beats(ctx, members)
	require.NoError(t, err)
	assert.Equal(t, map[string]sprint.Beat{"m1": machine, "m2": machine, "m3": machine, "m4": machine}, bs)

	legacyStrangers, err := json.Marshal(strangers)
	require.NoError(t, err)
	require.NoError(t, kv.SetKey(ctx, keyStrangers, string(legacyStrangers)))
	gotStrangers, err := st.strangers(ctx)
	require.NoError(t, err)
	assert.Equal(t, strangers, gotStrangers)

	// Reads are counted per record and per form on Check (S4). FriendBeats read the roster
	// again: three legacy entries, one base64 and one more legacy (cid's schema text in the
	// object).
	report, _, err := st.Check(ctx, 1)
	require.NoError(t, err)
	for name, want := range map[string]map[string]uint64{
		"FRIENDENTRY":  {"binary": 1, "base64": 1 + 2, "json": 1, "legacy": 1 + 4},
		"FRIENDSTATUS": {"binary": 1, "base64": 1 + 1, "json": 1, "legacy": 1 + 1},
		"BEAT":         {"binary": 2 + 3, "base64": 2 + 3, "json": 2 + 1, "legacy": 2 + 3},
		"STRANGERS":    {"binary": 0, "base64": 0, "json": 0, "legacy": 1},
	} {
		require.Contains(t, report.RowForms, name)
		for form, n := range want {
			assert.EqualValues(t, n, report.RowForms[name][form], name+" "+form)
		}
	}

	// Writers still write the legacy form (S4: readers first).
	require.NoError(t, putRoster(ctx, kv, map[string]friendEntry{"ann": entry}))
	raw, ok, err := kv.GetKey(ctx, keyFriends)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, LegacyObject([]byte(raw)), raw)
	load := 10.0
	_, err = st.Beat(ctx, "m5", &load, hostload.Source{NCPU: 2})
	require.NoError(t, err)
	raw, ok, err = kv.GetKey(ctx, beatKey("m5"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, LegacyObject([]byte(raw)), raw)

	// The read sites that carry a record into the next write read it from the table forms:
	// the friend's proof and check record (FriendBeatLive), her row (friendRowsOf, with a
	// one-shot friend's member beat, oneShotBeats), a member's last beat (BeatFacts) and the
	// touch of a held member's beat (TouchBeat); each write is still legacy.
	live := entry
	live.Session = ""
	require.NoError(t, putRoster(ctx, kv, map[string]friendEntry{"ann": entry, "dee": live}))
	require.NoError(t, kv.SetKey(ctx, friendBeatKey("dee"), string(bf.bin)))
	require.NoError(t, kv.SetKey(ctx, beatKey(sprint.FriendRow("ann")), string(bf.b64)))
	names, frows, _, err := st.friendRowsOf(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"ann", "dee"}, names)
	assert.Equal(t, beat.At, frows["ann"].Beat, "one-shot member beat from base64")
	assert.Equal(t, beat.Load, frows["ann"].Load)
	assert.Equal(t, beat.At, frows["dee"].Beat, "friend beat from the wire")
	assert.Equal(t, beat.Friend, frows["dee"].Report)
	fb, err := st.FriendBeatLive(ctx, "dee", sprint.FriendReport{}, nil, []string{"c3@1"})
	require.NoError(t, err)
	assert.Equal(t, beat.LiveSince, fb.LiveSince, "the live set's start carried from the wire")
	raw, _, err = kv.GetKey(ctx, friendBeatKey("dee"))
	require.NoError(t, err)
	assert.True(t, LegacyObject([]byte(raw)), raw)

	require.NoError(t, kv.SetKey(ctx, beatKey("m6"), string(bf.b64)))
	mb, err := st.Beat(ctx, "m6", &load, hostload.Source{NCPU: 2})
	require.NoError(t, err)
	assert.Equal(t, beat.Facts, mb.Facts, "facts carried from the base64 beat")
	assert.Equal(t, beat.StopReturns, mb.StopReturns)
	require.NoError(t, kv.SetKey(ctx, beatKey("m7"), string(mf.bin)))
	touched, err := st.TouchBeat(ctx, "m7")
	require.NoError(t, err)
	assert.True(t, touched, "a beat read from the wire is touched")
	raw, _, err = kv.GetKey(ctx, beatKey("m7"))
	require.NoError(t, err)
	assert.True(t, LegacyObject([]byte(raw)), raw)

	// A damaged value of each form is refused by name, never read as none.
	assertDamageRefused(t, rows.entry, "FRIENDENTRY", ef)
	assertDamageRefused(t, rows.status, "FRIENDSTATUS", sf)
	assertDamageRefused(t, rows.beat, "BEAT", bf)
	for i := 0; i < len(legacyStrangers); i++ {
		assertRefused(t, rows.stranger, "STRANGERS", legacyStrangers[:i], "strangers truncated")
	}
	// The strangers codec reads the fixed table the writer now writes (S1, S2): the page
	// reads equal and counts under the binary form.
	require.NoError(t, st.putStrangers(ctx, strangers))
	fixedStrangers, ok, err := kv.GetKey(ctx, keyStrangers)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, friendrowswire.TableFixedForm, fixedStrangers[0])
	sp, form, err := rows.stranger.Read([]byte(fixedStrangers))
	require.NoError(t, err)
	assert.Equal(t, schemaread.FormBinary, form)
	assert.Equal(t, strangers, sp.entries)
	assert.EqualValues(t, 1, sp.pages)
	// A foreign fixed wire (a FriendEntry's) is still refused by name.
	assertRefused(t, rows.stranger, "STRANGERS", ef.bin, "strangers as a foreign wire")
	assertRefused(t, rows.stranger, "STRANGERS", ef.b64, "strangers as foreign base64")

	// A clamped value is refused: an integer past int64 in the schema JSON text is clamped
	// by the table reader, and today's decoder refuses it in legacy text. FriendStatus holds
	// no integer, and an int64 field of the wire carries its whole width, so neither clamps.
	assertRefused(t, rows.entry, "FRIENDENTRY", []byte("{\"width\": 99999999999999999999}\n"), "entry json clamped")
	assertRefused(t, rows.entry, "FRIENDENTRY", []byte(`{"width":99999999999999999999}`), "entry legacy clamped")
	assertRefused(t, rows.beat, "BEAT", []byte("{\"cores\": -99999999999999999999}\n"), "beat json clamped")
	assertRefused(t, rows.beat, "BEAT", []byte(`{"cores":-99999999999999999999}`), "beat legacy clamped")
	_, _, err = rows.entry.Read([]byte("{\"width\": 99999999999999999999}\n"))
	assert.EqualError(t, err, "FRIENDENTRY REFUSED: clamped; run: nova-sprint check")

	// A damaged record is refused by name at the read sites: the roster and the status record
	// hand the refusal to the caller; a damaged beat is counted and reads as no beat, on purpose.
	require.NoError(t, kv.SetKey(ctx, keyFriends, `{"ann":`+string(ef.legacy[:len(ef.legacy)-1])+`}`))
	_, _, err = st.roster(ctx)
	assert.EqualError(t, err, "FRIENDENTRY REFUSED: legacy_json_unreadable; run: nova-sprint check")
	require.NoError(t, kv.SetKey(ctx, keyFriends, `{"ann":"`+string(ef.b64[:len(ef.b64)-4])+`"}`))
	_, _, err = st.roster(ctx)
	assert.Regexp(t, `^FRIENDENTRY REFUSED: [a-z_]+; run: nova-sprint check$`, err)
	require.NoError(t, kv.SetKey(ctx, keyFriends, string(ef.bin)))
	_, _, err = st.roster(ctx)
	assert.EqualError(t, err, "FRIENDENTRY REFUSED: unknown_form; run: nova-sprint check")
	require.NoError(t, kv.SetKey(ctx, keyFriendStatus, `{"ann":"`+string(sf.b64[:len(sf.b64)-4])+`"}`))
	_, err = st.storedFriendStatus(ctx, kv)
	assert.Regexp(t, `^FRIENDSTATUS REFUSED: [a-z_]+; run: nova-sprint check$`, err)

	before := rows.beat.Counts().Snapshot()
	require.NoError(t, kv.SetKey(ctx, beatKey("m1"), string(mf.bin[:len(mf.bin)-1])))
	bs, err = st.Beats(ctx, []string{"m1"})
	require.NoError(t, err)
	assert.Empty(t, bs)
	after := rows.beat.Counts().Snapshot()
	assert.EqualValues(t, before["binary"]+1, after["binary"])
	refusedBefore, refusedAfter := uint64(0), uint64(0)
	for k, v := range before {
		if len(k) > 8 && k[:8] == "refused_" {
			refusedBefore += v
		}
	}
	for k, v := range after {
		if len(k) > 8 && k[:8] == "refused_" {
			refusedAfter += v
		}
	}
	assert.Equal(t, refusedBefore+1, refusedAfter, "the damaged beat's refusal is counted")
}
