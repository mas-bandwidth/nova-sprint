package main

// The bus envelope dual read: the bus entries nova-sprint reads (the
// coordinator's stream, read by watch --wake) arrive as today's legacy entry
// (bus Message.Fields, read by bus.Parse) or as a typed entry, the generated
// Envelope (nova-tools pkg/buswire, schemas/bus/envelope.schema) in one stream
// field. Both are read through the one strict read, pkg/schemaread, which
// counts each read once under the form read: binary, base64, json or legacy.
// Readers change first and writers do not change here: the per-form counter is
// the evidence a cutover reads before writers switch.
//
// A typed entry carries the envelope in the field envelopeField: the binary
// wire, padded standard base64 of the wire, or the JSON text. An entry with
// that field is read as the envelope alone, so a damaged envelope is refused
// by name and never falls back to legacy fields written beside it. An entry
// without it is legacy and reads exactly as bus.Parse reads it: a legacy entry
// is never refused.

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/mas-bandwidth/nova-tools/pkg/bus"
	"github.com/mas-bandwidth/nova-tools/pkg/buswire"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
)

// envelopeField is the stream field a typed entry carries its envelope in.
const envelopeField = "envelope"

// envelopeHelp is the command an envelope refusal points at: the writer's.
const envelopeHelp = "nova-bus send -h"

// busEnvelope is one read: the typed envelope and the region that holds it
// (the body is a self-relative reference into that region, never copied
// out), or the legacy entry's fields.
type busEnvelope struct {
	e      *buswire.Envelope
	region []byte
	legacy map[string]string
}

// busReader reads bus entries in either form and counts them per form.
type busReader struct {
	mu      sync.Mutex
	pending map[string]string // the legacy entry being read, mu held; nil for a typed read
	r       *schemaread.Reader[busEnvelope]
}

// legacyInput is what the strict read is handed for a legacy entry: its
// fields are passed beside it (busReader.pending), because pkg/schemaread
// recognises a legacy input from bytes and a stream entry is a map.
var legacyInput = []byte("legacy")

func newBusReader() *busReader {
	b := &busReader{}
	b.r = schemaread.NewReader(&schemaread.Codec[busEnvelope]{
		Name:  "ENVELOPE",
		Help:  envelopeHelp,
		Reset: func(v *busEnvelope) { *v = busEnvelope{} },
		Measure: func(wire []byte) error {
			_, err := buswire.EnvelopeLoadMeasureReason(wire)
			return err
		},
		LoadWire: envelopeLoadWire,
		LoadJSON: envelopeLoadJSON,
		Legacy:   func([]byte) bool { return b.pending != nil },
		LoadLegacy: func(v *busEnvelope, _ []byte) schemaread.Report {
			v.legacy = b.pending
			return schemaread.Report{}
		},
		Constraints: envelopeConstraints,
		SaveWire:    envelopeSaveWire,
		SaveJSON:    envelopeSaveJSON,
	})
	return b
}

// countsLine is the per-form read counters and the refusal tallies as one
// line, `name=count` in name order.
func (b *busReader) countsLine() string {
	snap := b.r.Counts().Snapshot()
	names := make([]string, 0, len(snap))
	for k := range snap {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = k + "=" + strconv.FormatUint(snap[k], 10)
	}
	return strings.Join(parts, " ")
}

// entry is the message a stream entry holds and the form it was read in. A
// refusal is a *schemaread.Refusal and the message is the zero value.
func (b *busReader) entry(e bus.Entry) (bus.Message, schemaread.Form, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	in := legacyInput
	if v, typed := e.Fields[envelopeField]; typed {
		in, b.pending = []byte(v), nil
	} else {
		b.pending = e.Fields
		if b.pending == nil {
			b.pending = map[string]string{}
		}
	}
	defer func() { b.pending = nil }()
	v, res, err := b.r.Read(in)
	switch {
	case err != nil:
		return bus.Message{}, res.Form, err
	case v.e == nil:
		return bus.Parse(v.legacy), res.Form, nil
	}
	return envelopeMessage(v.e), res.Form, nil
}

// envelopeBound is a string or array field's declared bound, read from the
// generated descriptors, never typed twice.
func envelopeBound(name string) int {
	for _, f := range buswire.EnvelopeTableType().Fields {
		if f.Name == name {
			return int(f.ArrayBound)
		}
	}
	panic("Envelope has no field " + name)
}

func envelopeReport(r *buswire.TableReport) schemaread.Report {
	return schemaread.Report{
		Unknown:      int(r.Unknown),
		KindMismatch: int(r.KindMismatch),
		Clamped:      int(r.Clamped),
		// a key given twice in the JSON text is ambiguous: refused, never last-wins
		Malformed: r.Malformed || r.Duplicate > 0,
		Reason:    r.Reason,
	}
}

// envelopeRegion is an aligned buffer of n bytes for a region load.
func envelopeRegion(n int64) []byte {
	raw := make([]byte, n+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

func envelopeLoadWire(v *busEnvelope, wire []byte) schemaread.Report {
	n := buswire.EnvelopeLoadMeasure(wire)
	if n < 0 {
		return schemaread.Report{Malformed: true}
	}
	var r buswire.TableReport
	v.region = envelopeRegion(n)
	v.e = buswire.EnvelopeLoad(v.region, wire, &r)
	if v.e == nil {
		return schemaread.Report{Malformed: true}
	}
	return envelopeReport(&r)
}

func envelopeLoadJSON(v *busEnvelope, text []byte) schemaread.Report {
	var b buswire.EnvelopeBuilder
	if !b.Init() {
		return schemaread.Report{Malformed: true}
	}
	var r buswire.TableReport
	if !buswire.EnvelopeFromJson(&b, text, &r) || !b.Lock() {
		return schemaread.Report{Malformed: true, Reason: r.Reason}
	}
	v.e, v.region = b.AsConst(), b.Region()
	return envelopeReport(&r)
}

func envelopeSaveWire(v *busEnvelope) []byte {
	n := buswire.EnvelopeMeasure(v.e)
	if n < 0 {
		return nil
	}
	buf := make([]byte, n)
	if buswire.EnvelopeSave(v.e, buf) != n {
		return nil
	}
	return buf
}

func envelopeSaveJSON(v *busEnvelope) []byte {
	n := buswire.EnvelopeToJsonMeasure(v.e)
	if n < 0 {
		return nil
	}
	buf := make([]byte, n)
	if buswire.EnvelopeToJson(v.e, buf) != n {
		return nil
	}
	return buf
}

func envelopeText(b []byte, n int32) string { return string(b[:n]) }

func envelopeBody(e *buswire.Envelope) string {
	s := buswire.TableStringAt(&e.Body)
	return string(s.Data[:s.Length])
}

// isLowerHex40 says s is 40 lowercase hex digits, a commit's full name.
func isLowerHex40(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range []byte(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// envelopeConstraints are the typed envelope's bounds after a load. Every
// field a sent message always has is required, because a variable table reads
// an absent field as zero, and a wake must never read a missing kind as None
// or a missing stamp as the epoch. A legacy entry has none: it reads as
// bus.Parse reads it.
func envelopeConstraints(v *busEnvelope, c *schemaread.Checker) {
	if v.legacy != nil {
		return
	}
	e := v.e
	c.Required("envelope", e != nil)
	if e == nil {
		return
	}
	c.Required("id", e.IdLength > 0)
	c.Required("from", e.FromLength > 0)
	c.Required("to", e.ToCount > 0)
	c.Required("subject", e.SubjectLength > 0)
	c.Required("kind", e.Kind != buswire.KindNone)
	c.Required("at_ms", e.AtMsPresent)
	c.Required("body", e.Body != 0)
	if e.Kind == buswire.Kindask {
		c.Required("due_ms", e.DueMsPresent)
	}
	c.Enum("kind", int(e.Kind), int(buswire.KindCount)+1)
	for _, s := range []struct{ name, v string }{
		{"id", envelopeText(e.Id[:], e.IdLength)}, {"from", envelopeText(e.From[:], e.FromLength)},
		{"re", envelopeText(e.Re[:], e.ReLength)}, {"subject", envelopeText(e.Subject[:], e.SubjectLength)},
		{"card", envelopeText(e.Card[:], e.CardLength)}, {"head", envelopeText(e.Head[:], e.HeadLength)},
		{"token", envelopeText(e.Token[:], e.TokenLength)},
	} {
		c.Text(s.name, s.v, envelopeBound(s.name))
	}
	if head := envelopeText(e.Head[:], e.HeadLength); head != "" && !isLowerHex40(head) {
		// the Checker names no reason of its own for a wrong shape: a text that
		// fails the NUL test is refused as malformed under the field's name
		c.Text("head", "\x00", 1)
	}
	for i := range e.ToCount {
		c.Text("to", envelopeText(e.To[i].S[:], e.To[i].SLength), bus.MaxName)
	}
	for i := range e.CcCount {
		c.Text("cc", envelopeText(e.Cc[i].S[:], e.Cc[i].SLength), bus.MaxName)
	}
	if e.Body != 0 {
		c.Text("body", envelopeBody(e), bus.MaxBody)
	}
}

// envelopeMessage is the bus message a checked envelope holds, as bus.Parse
// would read the same message from its legacy entry.
func envelopeMessage(e *buswire.Envelope) bus.Message {
	list := func(rows []buswire.NameRow) []string {
		var out []string
		for _, r := range rows {
			out = append(out, envelopeText(r.S[:], r.SLength))
		}
		return out
	}
	m := bus.Message{
		ID: envelopeText(e.Id[:], e.IdLength), From: envelopeText(e.From[:], e.FromLength),
		To: list(e.To[:e.ToCount]), CC: list(e.Cc[:e.CcCount]),
		Re: envelopeText(e.Re[:], e.ReLength), Subject: envelopeText(e.Subject[:], e.SubjectLength),
		Token: envelopeText(e.Token[:], e.TokenLength),
	}
	if e.Kind != buswire.KindNone {
		m.Kind = buswire.EnumNameKind(e.Kind)
	}
	if e.AtMsPresent {
		m.At = time.UnixMilli(e.AtMs).UTC()
	}
	if e.DueMsPresent {
		m.Due = time.UnixMilli(e.DueMs).UTC()
	}
	if e.Body != 0 {
		m.Body = envelopeBody(e)
	}
	return m
}
