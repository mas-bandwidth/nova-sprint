package main

// TestEnvelopeDualRead is the bus envelope's dual-read interop test on the
// side of nova-sprint (readers first, writers unchanged). The bus entries
// nova-sprint reads (the coordinator's stream, read by watch --wake) arrive as
// today's legacy entry (Message.Fields) or as a typed entry, the Envelope in
// its binary wire, padded standard base64 of that wire, or its JSON text;
// every form reads as the legacy entry reads, a damaged typed entry is refused
// by name and never acted on, and each read is counted once under the form
// read. The writer side here is the test's own: no writer in nova-sprint
// changes in this step.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/bus/bustest"
	"github.com/mas-bandwidth/nova-tools/pkg/buswire"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envTestAt is a fixed store stamp: the test takes no real time, and its clock
// starts well past zero.
var envTestAt = time.UnixMilli(1_792_098_245_000).UTC()

// envTestSent is a message as bus Send stores it: recipients sorted, the stamp
// to the second, a kind always named; an ask carries its deadline.
func envTestSent(kind string) bus.Message {
	m := bus.Message{
		ID: "01JA2B3C4D5E6F7G8H9J0KMNPQ", From: "reader-a", To: []string{"coordinator"},
		CC: []string{"reader-c"}, Subject: "#202 HOLD at 3f9c", Re: "01JA2B3C4D5E6F7G8H9J0KMNPA",
		Kind: kind, At: envTestAt, Body: "the reader found it broken:\n- a line\n",
	}
	if kind == bus.KindAsk {
		m.Due = envTestAt.Add(10 * time.Minute)
	}
	return m
}

// envTestCorpus is every kind, a message with no cc and no re, and the fewest
// fields a sent message has.
func envTestCorpus() []bus.Message {
	var out []bus.Message
	for _, k := range bus.Kinds {
		out = append(out, envTestSent(k))
	}
	one := envTestSent(bus.KindReport)
	one.CC, one.Re = nil, ""
	out = append(out, one, bus.Message{ID: "01JA2B3C4D5E6F7G8H9J0KMNPQ", From: "reader-a", To: []string{"coordinator"}, Subject: "s", Kind: bus.KindStatus, At: envTestAt, Body: "b"})
	return out
}

// envTestBound is a string or array field's declared bound, read from the
// generated descriptors.
func envTestBound(name string) int {
	for _, f := range buswire.EnvelopeTableType().Fields {
		if f.Name == name {
			return int(f.ArrayBound)
		}
	}
	panic("Envelope has no field " + name)
}

// envTestTyped is the typed envelope of m, the writer side a switched writer
// runs; head and card are set when given.
func envTestTyped(t *testing.T, m bus.Message, card, head string) busEnvelope {
	t.Helper()
	var b buswire.EnvelopeBuilder
	require.True(t, b.Init())
	e := b.GetRoot()
	set := func(name string, dst []byte, n *int32, v string) {
		require.LessOrEqual(t, len(v), envTestBound(name), name)
		*n = int32(copy(dst, v))
	}
	names := func(dst []buswire.NameRow, n *int32, vs []string) {
		for i, v := range vs {
			dst[i].SLength = int32(copy(dst[i].S[:], v))
		}
		*n = int32(len(vs))
	}
	kind := m.KindName()
	for k := buswire.Kind(1); k <= buswire.KindCount; k++ {
		if buswire.EnumNameKind(k) == kind {
			e.Kind = k
		}
	}
	require.NotEqual(t, buswire.KindNone, e.Kind, kind)
	set("id", e.Id[:], &e.IdLength, m.ID)
	set("from", e.From[:], &e.FromLength, m.From)
	names(e.To[:], &e.ToCount, m.To)
	names(e.Cc[:], &e.CcCount, m.CC)
	set("re", e.Re[:], &e.ReLength, m.Re)
	set("subject", e.Subject[:], &e.SubjectLength, m.Subject)
	set("token", e.Token[:], &e.TokenLength, m.Token)
	set("card", e.Card[:], &e.CardLength, card)
	set("head", e.Head[:], &e.HeadLength, head)
	if !m.At.IsZero() {
		e.AtMs, e.AtMsPresent = m.At.UnixMilli(), true
	}
	if !m.Due.IsZero() {
		e.DueMs, e.DueMsPresent = m.Due.UnixMilli(), true
	}
	if m.Body != "" {
		buswire.TableStringEmplace(&b.Main, &e.Body, []byte(m.Body))
	}
	require.True(t, b.Lock())
	return busEnvelope{e: b.AsConst(), region: b.Region()}
}

// envTestWrite is the typed envelope of m in form, written unchecked: the
// writer may be damaged or hostile, so the reader is what refuses.
func envTestWrite(t *testing.T, v busEnvelope, form schemaread.Form) string {
	t.Helper()
	var out []byte
	switch form {
	case schemaread.FormBinary, schemaread.FormBase64:
		out = envelopeSaveWire(&v)
		require.NotEmpty(t, out)
		if form == schemaread.FormBase64 {
			out = []byte(base64.StdEncoding.EncodeToString(out))
		}
	case schemaread.FormJSON:
		out = envelopeSaveJSON(&v)
		require.NotEmpty(t, out)
	default:
		t.Fatalf("no typed form %s", form)
	}
	return string(out)
}

// envTestEntry is a typed stream entry: the envelope in its one field, and
// the legacy fields beside it when a switching writer writes both.
func envTestEntry(envelope string, legacy map[string]string) bus.Entry {
	f := map[string]string{}
	for k, v := range legacy {
		f[k] = v
	}
	f[envelopeField] = envelope
	return bus.Entry{Stream: bus.StreamOf("coordinator"), Entry: "1792098245000-1", Fields: f}
}

var envTestForms = []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64, schemaread.FormJSON}

// envTestRefused is the refusal a damaged entry gets, "" when it reads.
func envTestRefused(t *testing.T, r *busReader, e bus.Entry) string {
	t.Helper()
	m, _, err := r.entry(e)
	if err == nil {
		return ""
	}
	assert.Equal(t, bus.Message{}, m, "a refused entry reads as nothing")
	var ref *schemaread.Refusal
	require.True(t, errors.As(err, &ref), "every refusal is a *schemaread.Refusal: %v", err)
	assert.True(t, strings.HasPrefix(err.Error(), "ENVELOPE REFUSED: "), err.Error())
	assert.True(t, strings.HasSuffix(err.Error(), "; run: nova-bus send -h"), err.Error())
	return err.Error()
}

func envTestRefusal(reason, field string) string {
	if field != "" {
		reason += " (" + field + ")"
	}
	return "ENVELOPE REFUSED: " + reason + "; run: nova-bus send -h"
}

func TestEnvelopeDualRead(t *testing.T) {
	t.Parallel()

	t.Run("legacy and typed entries read alike", func(t *testing.T) {
		t.Parallel()
		r := newBusReader()
		corpus := envTestCorpus()
		for i, m := range corpus {
			fields := m.Fields()
			old := bus.Parse(fields) // today's writer, then today's reader
			got, form, err := r.entry(bus.Entry{Entry: "1792098245000-1", Fields: fields})
			require.NoError(t, err, i)
			assert.Equal(t, schemaread.FormLegacy, form, "%d", i)
			assert.Equal(t, old, got, "%d: the legacy entry reads as bus.Parse reads it", i)
			v := envTestTyped(t, old, "", "")
			for _, form := range envTestForms {
				got, read, err := r.entry(envTestEntry(envTestWrite(t, v, form), nil))
				require.NoError(t, err, "%d %s", i, form)
				assert.Equal(t, form, read, "%d", i)
				assert.Equal(t, old, got, "%d %s: the typed entry reads as the legacy entry reads", i, form)
			}
		}
		n := uint64(len(corpus))
		for _, form := range append(envTestForms, schemaread.FormLegacy) {
			assert.Equal(t, n, r.r.Counts().Reads(form), "each entry read once in %s", form)
		}
		assert.Zero(t, r.r.Counts().Unknown())
	})

	t.Run("a legacy entry reads as today, whatever it lacks", func(t *testing.T) {
		t.Parallel()
		r := newBusReader()
		for _, fields := range []map[string]string{
			{},
			{"from": "reader-a", "subject": "ping"},
			{"from": "reader-a", "subject": "s", "kind": "notice", "at": "not a time", "body": "a\xffb"},
		} {
			got, form, err := r.entry(bus.Entry{Entry: "1792098245000-1", Fields: fields})
			require.NoError(t, err, "a legacy entry is never refused: it reads as bus.Parse reads it")
			assert.Equal(t, schemaread.FormLegacy, form)
			assert.Equal(t, bus.Parse(fields), got)
		}
		assert.Equal(t, uint64(3), r.r.Counts().Reads(schemaread.FormLegacy))
	})

	t.Run("the writer switch keeps old readers reading", func(t *testing.T) {
		t.Parallel()
		r := newBusReader()
		for i, m := range envTestCorpus() {
			fields := m.Fields()
			old := bus.Parse(fields)
			for _, form := range envTestForms {
				// a switching writer writes the envelope beside today's fields
				e := envTestEntry(envTestWrite(t, envTestTyped(t, old, "", ""), form), fields)
				assert.Equal(t, old, bus.Parse(e.Fields), "%d %s: an old reader meets the fields it reads today", i, form)
				assert.Equal(t, wakeMsg{ID: e.Entry, From: fields["from"], Subject: fields["subject"], Kind: fields["kind"]},
					wakeMsg{ID: e.Entry, From: e.Fields["from"], Subject: e.Fields["subject"], Kind: e.Fields["kind"]}, "%d %s: the old wake reads alike", i, form)
				got, read, err := r.entry(e)
				require.NoError(t, err, "%d %s", i, form)
				assert.Equal(t, form, read, "%d: the new reader reads the envelope, not the legacy fields", i)
				assert.Equal(t, old, got, "%d %s", i, form)
			}
		}
		assert.Zero(t, r.r.Counts().Reads(schemaread.FormLegacy), "an entry with an envelope is never counted as legacy")
	})

	t.Run("the typed card and head read and a head that is no hex is refused", func(t *testing.T) {
		t.Parallel()
		r := newBusReader()
		m := envTestSent(bus.KindReport)
		head := strings.Repeat("3f9c", 10)
		for _, form := range envTestForms {
			got, _, err := r.entry(envTestEntry(envTestWrite(t, envTestTyped(t, m, "l1-bus-envelope-dual-read-2", head), form), nil))
			require.NoError(t, err, form)
			assert.Equal(t, bus.Parse(m.Fields()), got, form)
			for _, bad := range []string{strings.Repeat("3F9C", 10), strings.Repeat("3f9", 13), strings.Repeat("g", 40)} {
				e := envTestEntry(envTestWrite(t, envTestTyped(t, m, "", bad), form), nil)
				assert.Equal(t, envTestRefusal("malformed", "head"), envTestRefused(t, r, e), "%s %q", form, bad)
			}
		}
	})

	t.Run("a malformed typed entry is refused by name", func(t *testing.T) {
		t.Parallel()
		r := newBusReader()
		good := envTestWrite(t, envTestTyped(t, envTestSent(bus.KindBlocker), "", ""), schemaread.FormBinary)
		newer := []byte(good)
		newer[0] = 9
		b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
		noStamp := envTestSent(bus.KindBlocker)
		noStamp.At = time.Time{}
		ask := envTestSent(bus.KindAsk)
		ask.Due = time.Time{}
		nul := envTestSent(bus.KindBlocker)
		nul.Subject = "a\x00b"
		goodJSON := envTestWrite(t, envTestTyped(t, envTestSent(bus.KindBlocker), "", ""), schemaread.FormJSON)
		// a wider writer's subject: the builder here holds no more than the bound, so the text is edited
		longJSON := strings.Replace(goodJSON, `"subject": "#202 HOLD at 3f9c"`, `"subject": "`+strings.Repeat("s", 201)+`"`, 1)
		require.NotEqual(t, goodJSON, longJSON)
		for _, tc := range []struct {
			name, in, reason string
		}{
			{"truncated wire", good[:len(good)-1], envTestRefusal("wire_damaged", "")},
			{"trailing junk", good + "\x00\x01", envTestRefusal("wire_damaged", "")},
			{"a form this reader does not know", string(newer), envTestRefusal("unknown_form", "")},
			{"the same as base64", b64(string(newer)), envTestRefusal("unknown_form", "")},
			{"truncated base64", b64(good)[:len(b64(good))-3], envTestRefusal("malformed", "")},
			{"an empty envelope", "", envTestRefusal("unknown_form", "")},
			{"not an envelope at all", "#202 HOLD at 3f9c", envTestRefusal("unknown_form", "")},
			{"a JSON text that does not parse", goodJSON[:len(goodJSON)/2], envTestRefusal("malformed", "")},
			{"a JSON key given twice", `{"subject": "a", "subject": "b"}`, envTestRefusal("malformed", "")},
			{"a subject over 200 bytes", longJSON, envTestRefusal("clamped", "")},
			{"no stamp", envTestWrite(t, envTestTyped(t, noStamp, "", ""), schemaread.FormBinary), envTestRefusal("absent_required", "at_ms")},
			{"an ask with no deadline", envTestWrite(t, envTestTyped(t, ask, "", ""), schemaread.FormBase64), envTestRefusal("absent_required", "due_ms")},
			// the generated load refuses an interior NUL itself, naming no field
			{"a NUL in the subject", envTestWrite(t, envTestTyped(t, nul, "", ""), schemaread.FormBinary), envTestRefusal("malformed", "")},
		} {
			assert.Equal(t, tc.reason, envTestRefused(t, r, envTestEntry(tc.in, nil)), tc.name)
			// never a fall back to the legacy fields beside it
			assert.Equal(t, tc.reason, envTestRefused(t, r, envTestEntry(tc.in, envTestSent(bus.KindBlocker).Fields())), "%s, beside legacy fields", tc.name)
		}
		for i := range len(good) {
			assert.NotEmpty(t, envTestRefused(t, r, envTestEntry(good[:i], nil)), "every truncation is refused, never read as a shorter envelope: cut at %d", i)
		}
		assert.Zero(t, r.r.Counts().Reads(schemaread.FormLegacy), "a refused typed entry is never counted as legacy")
		assert.Positive(t, r.r.Counts().Refused("wire_damaged"))
		assert.Positive(t, r.r.Counts().Refused("malformed"))
	})

	t.Run("the per-form counter moves on the form read", func(t *testing.T) {
		t.Parallel()
		r := newBusReader()
		m := envTestSent(bus.KindRequest)
		v := envTestTyped(t, m, "", "")
		want := map[string]uint64{"binary": 0, "base64": 0, "json": 0, "legacy": 0, "unknown_fields": 0}
		assert.Equal(t, want, r.r.Counts().Snapshot())
		read := func(e bus.Entry, key string) {
			t.Helper()
			_, _, err := r.entry(e)
			require.NoError(t, err, key)
			want[key]++
			assert.Equal(t, want, r.r.Counts().Snapshot(), "after a %s read only %s moved", key, key)
		}
		read(bus.Entry{Fields: m.Fields()}, "legacy")
		read(envTestEntry(envTestWrite(t, v, schemaread.FormBinary), nil), "binary")
		read(envTestEntry(envTestWrite(t, v, schemaread.FormBase64), nil), "base64")
		read(envTestEntry(envTestWrite(t, v, schemaread.FormJSON), nil), "json")
		read(envTestEntry(envTestWrite(t, v, schemaread.FormJSON), nil), "json")
		// a refusal counts under its form and its reason, and reads nothing
		_, form, err := r.entry(envTestEntry(envTestWrite(t, v, schemaread.FormBinary)[:9], nil))
		require.Error(t, err)
		assert.Equal(t, schemaread.FormBinary, form)
		want["binary"]++
		want["refused_wire_damaged"] = 1
		assert.Equal(t, want, r.r.Counts().Snapshot())
		assert.Equal(t, "base64=1 binary=2 json=2 legacy=1 refused_wire_damaged=1 unknown_fields=0", r.countsLine())
	})

	t.Run("the wake reads both forms alike and never acts on a refused entry", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.ok("init --readers reader-a --members m1")
		st, err := ta.a.storeCtx(context.Background(), common{verb: "watch", redis: "mem:0", actor: "coordinator"})
		require.NoError(t, err)
		fake := bustest.NewFake(ta.a.now(), "coordinator", "reader-a")
		var said []string
		src := &storeWake{st: st, a: ta.a, bus: &bus.Bus{Store: fake}, stream: bus.StreamOf("coordinator"), reads: newBusReader(),
			say: func(s string) { said = append(said, s) }}
		ctx := context.Background()
		stream := []string{src.stream}
		blocker := envTestSent(bus.KindBlocker)
		v := envTestTyped(t, blocker, "", "")
		require.NoError(t, fake.AddAll(ctx, stream, blocker.Fields()))
		for _, form := range envTestForms {
			require.NoError(t, fake.AddAll(ctx, stream, map[string]string{envelopeField: envTestWrite(t, v, form)}))
		}
		// a damaged typed blocker that would wake the coordinator if it were read
		damaged := envTestWrite(t, v, schemaread.FormBinary)
		require.NoError(t, fake.AddAll(ctx, stream, map[string]string{envelopeField: damaged[:len(damaged)-1]}))

		l, err := src.look(ctx, "0")
		require.NoError(t, err)
		require.Len(t, l.Msgs, 5)
		for i, m := range l.Msgs[:4] {
			assert.Equal(t, wakeMsg{ID: m.ID, From: "reader-a", Subject: blocker.Subject, Kind: bus.KindBlocker}, m, "%d: every form reads as the legacy entry", i)
			assert.True(t, m.wakes("coordinator"), "%d", i)
		}
		bad := l.Msgs[4]
		assert.Equal(t, envTestRefusal("wire_damaged", ""), bad.Refused)
		assert.Empty(t, bad.From+bad.Subject+bad.Kind, "a refused entry carries nothing to act on")
		assert.False(t, bad.wakes("coordinator"), "a refused entry never wakes")
		require.Len(t, said, 1)
		assert.Equal(t, fmt.Sprintf("bus entry %s: %s", bad.ID, envTestRefusal("wire_damaged", "")), said[0])

		// only the refused entry: the cursor passes it and nothing wakes
		s := wakeState{Seeded: true, Bus: l.Msgs[3].ID, Last: map[string]time.Time{wakeCheck: ta.a.now()}}
		w, woke := s.step(defaultWakeCfg(), wakeLook{Coordinator: "coordinator", Msgs: l.Msgs[4:]}, ta.a.now())
		assert.False(t, woke, "a refused entry wakes nothing: %v", w)
		assert.Equal(t, bad.ID, s.Bus, "the cursor passes a refused entry, so it is never read again")
		assert.Equal(t, "base64=1 binary=2 json=1 legacy=1 refused_wire_damaged=1 unknown_fields=0", src.reads.countsLine())
		assert.True(t, slices.ContainsFunc(l.Msgs, func(m wakeMsg) bool { return m.Refused != "" }))
	})
}
