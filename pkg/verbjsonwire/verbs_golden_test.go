package verbjsonwire

// TestVerbJSONGolden is the golden test of the verbjson area's definitions
// (schemas/verbjson/verbs.schema, a VARIABLE-table area: S1, S2). For every
// root table (the object a verb prints with --json) it holds that
//
//   - a sample of today's output reads through pkg/schemaread as JSON text,
//     and the table's text form keeps every non-empty key and value of it;
//   - the value round-trips in its three forms, the binary wire, padded
//     standard base64 of the wire and the JSON text, each read strictly and
//     counted once on the form read;
//   - a fixture built from the schema itself (every field of every table, each
//     optional present, each list, array and map with an entry) round-trips the
//     same way, so every field of every table is carried by the wire;
//   - a value one past each declared bound (a string, a bounded array's count,
//     a map's key) is refused by name as clamped, never read shortened;
//   - a damaged image of each form is refused by name, never partly read;
//   - a field a newer writer adds is skipped and counted, never refused (S1);
//   - every table of the schema is reached from a root, generate.go pins the
//     repository's schema pin, and the area's tables.baseline names every table.
//
// The fixtures are the schema's own text: this file parses
// schemas/verbjson/verbs.schema, an engine independent of the generated Go, so
// the test and the definition cannot drift apart. No time, socket, network or
// program is used.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- the schema, parsed from its text ----------------------------------------

// typ is a field's declared type, as the schema spells it.
type typ struct {
	kind  string // string, bool, num, table, list, array, map
	bound int    // a string's bytes, a bounded array's count, a map's key bytes
	elem  *typ   // a list's, array's or map's element
	table string // a table's name
}

type field struct {
	name string
	key  string // the JSON text key
	t    *typ
}

type tableDef struct {
	name   string
	array  bool // | json = "array": the table's text is its one field's array
	fields []field
}

var (
	fieldLine = regexp.MustCompile(`^\s*([a-z_][a-z0-9_]*)\s+(\S+)(?:\s*\|\s*json\s*=\s*"([^"]+)")?\s*$`)
	tableHead = regexp.MustCompile(`^table\s+([A-Za-z0-9]+)(?:\s*\|\s*json\s*=\s*"array")?\s*$`)
	strType   = regexp.MustCompile(`^string\((\d+)\)$`)
	arrType   = regexp.MustCompile(`^\[\.\.(\d+)\](.+)$`)
	mapType   = regexp.MustCompile(`^map\[string\((\d+)\)\](.+)$`)
)

func parseType(s string) *typ {
	s = strings.TrimPrefix(s, "?")
	if m := strType.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &typ{kind: "string", bound: n}
	}
	if m := arrType.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &typ{kind: "array", bound: n, elem: parseType(m[2])}
	}
	if m := mapType.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &typ{kind: "map", bound: n, elem: parseType(m[2])}
	}
	if rest, ok := strings.CutPrefix(s, "[]"); ok {
		return &typ{kind: "list", elem: parseType(rest)}
	}
	switch s {
	case "string":
		return &typ{kind: "string"}
	case "bool":
		return &typ{kind: "bool"}
	case "int64", "uint64", "float64":
		return &typ{kind: "num"}
	}
	return &typ{kind: "table", table: s}
}

// loadSchema parses schemas/verbjson/verbs.schema into its tables.
func loadSchema(t *testing.T) (map[string]*tableDef, []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "verbjson", "verbs.schema"))
	require.NoError(t, err)
	defs := map[string]*tableDef{}
	var order []string
	var cur *tableDef
	for _, line := range strings.Split(string(raw), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "package ") {
			continue
		}
		if m := tableHead.FindStringSubmatch(line); m != nil {
			cur = &tableDef{name: m[1], array: strings.Contains(line, `json = "array"`)}
			defs[m[1]] = cur
			order = append(order, m[1])
			continue
		}
		if strings.TrimSpace(line) == "{" {
			continue
		}
		if strings.TrimSpace(line) == "}" {
			cur = nil
			continue
		}
		if cur == nil {
			continue
		}
		m := fieldLine.FindStringSubmatch(line)
		require.NotNil(t, m, "a field line the test cannot parse: %q", line)
		key := m[3]
		if key == "" {
			key = m[1]
		}
		cur.fields = append(cur.fields, field{name: m[1], key: key, t: parseType(m[2])})
	}
	require.NotEmpty(t, defs)
	return defs, order
}

// base is the smallest value of t that carries every field below it: a string
// of "x", a number, true, a list, array or map of one entry.
func base(defs map[string]*tableDef, t *typ) any {
	switch t.kind {
	case "string":
		return "x"
	case "bool":
		return true
	case "num":
		return 7.0
	case "table":
		if defs[t.table].array {
			f := defs[t.table].fields[0]
			return []any{base(defs, f.t.elem)}
		}
		o := map[string]any{}
		for _, f := range defs[t.table].fields {
			o[f.key] = base(defs, f.t)
		}
		return o
	case "list", "array":
		return []any{base(defs, t.elem)}
	case "map":
		return map[string]any{"k": base(defs, t.elem)}
	}
	panic("unknown kind " + t.kind)
}

// overs is every variant of base(t) with exactly one value one past its
// declared bound: a string of N+1 bytes, a bounded array of N+1 elements, a
// map key of N+1 bytes.
func overs(defs map[string]*tableDef, t *typ) []any {
	var out []any
	switch t.kind {
	case "string":
		out = append(out, strings.Repeat("x", t.bound+1))
	case "table":
		if defs[t.table].array {
			f := defs[t.table].fields[0]
			for _, v := range overs(defs, f.t) {
				out = append(out, v)
			}
			return out
		}
		for _, f := range defs[t.table].fields {
			for _, v := range overs(defs, f.t) {
				o := base(defs, t).(map[string]any)
				o[f.key] = v
				out = append(out, o)
			}
		}
	case "list":
		if t.elem.kind != "string" || t.elem.bound > 0 {
			for _, v := range overs(defs, t.elem) {
				out = append(out, []any{v})
			}
		}
	case "array":
		more := make([]any, t.bound+1)
		for i := range more {
			more[i] = base(defs, t.elem)
		}
		out = append(out, more)
		if t.elem.kind != "string" || t.elem.bound > 0 {
			for _, v := range overs(defs, t.elem) {
				out = append(out, []any{v})
			}
		}
	case "map":
		out = append(out, map[string]any{strings.Repeat("k", t.bound+1): base(defs, t.elem)})
		for _, v := range overs(defs, t.elem) {
			out = append(out, map[string]any{"k": v})
		}
	}
	return out
}

// reaches adds every table below t to seen.
func reaches(defs map[string]*tableDef, name string, seen map[string]bool) {
	if seen[name] {
		return
	}
	seen[name] = true
	for _, f := range defs[name].fields {
		for e := f.t; e != nil; e = e.elem {
			if e.kind == "table" {
				reaches(defs, e.table, seen)
			}
		}
	}
}

// prune drops every zero value, so "absent reads as zero" compares equal to a
// written zero (a variable table elides what is default).
func prune(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			e = prune(e)
			if e == nil {
				delete(x, k)
			} else {
				x[k] = e
			}
		}
		if len(x) == 0 {
			return nil
		}
		return x
	case []any:
		for i, e := range x {
			if p := prune(e); p != nil {
				x[i] = p
			}
		}
		if len(x) == 0 {
			return nil
		}
		return x
	case string:
		if x == "" {
			return nil
		}
	case bool:
		if !x {
			return nil
		}
	case float64:
		if x == 0 {
			return nil
		}
	}
	return v
}

func decode(t *testing.T, text []byte) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(text, &v), "%s", text)
	return v
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// ---- the strict codec of one root table -----------------------------------------

type builderOf[R any] interface {
	Init(allocator ...TableAllocator) bool
	Lock() bool
	AsConst() *R
	Shutdown()
}

// held is a value read from a builder: the root lives in the builder's region,
// so the builder stays alive until the read is done with.
type held[R any] struct{ root *R }

// erased is one root table's strict read and write, whatever its type.
type erased interface {
	// read reads in, in whatever form it is, and writes it back as JSON text.
	read(in []byte) (text []byte, res schemaread.Result, err error)
	// write reads the JSON text and writes it as form.
	write(text []byte, form schemaread.Form) ([]byte, error)
	counts() *schemaread.Counts
}

type codec[R, B any, PB interface {
	*B
	builderOf[R]
}] struct {
	rd     *schemaread.Reader[held[R]]
	live   []PB
	jsonOf func(*R) []byte
}

func (c *codec[R, B, PB]) release() {
	clear(c.live)
	c.live = nil
}

func (c *codec[R, B, PB]) read(in []byte) ([]byte, schemaread.Result, error) {
	defer c.release()
	v, res, err := c.rd.Read(in)
	if err != nil {
		return nil, res, err
	}
	return c.jsonOf(v.root), res, nil
}

func (c *codec[R, B, PB]) write(text []byte, form schemaread.Form) ([]byte, error) {
	defer c.release()
	v, _, err := c.rd.Read(text)
	if err != nil {
		return nil, err
	}
	return c.rd.Write(&v, form)
}

func (c *codec[R, B, PB]) counts() *schemaread.Counts { return c.rd.Counts() }

// whole runs a generated Measure and Save into one exact buffer; nil when
// either refuses.
func whole(measure func() int64, save func([]byte) int64) []byte {
	n := measure()
	if n < 0 {
		return nil
	}
	buf := make([]byte, n)
	if save(buf) != n {
		return nil
	}
	return buf
}

// report is the generated TableReport reduced to what the strict read decides on.
func report(rep *TableReport, ok bool) schemaread.Report {
	out := schemaread.Report{Unknown: int(rep.Unknown), KindMismatch: int(rep.KindMismatch), Clamped: int(rep.Clamped), Malformed: rep.Malformed || !ok}
	if rep.Verdict == TableOpenRefused || rep.Verdict == TableOpenDamaged {
		out.Reason = rep.Err().Error()
	}
	return out
}

// newCodec is the strict read and write of one table from the functions the
// generator gives it. The facade rules are UNION.md section 4.6: refuse every
// clamp, refuse a kind mismatch, name the damage, and check before a write.
func newCodec[R, B any, PB interface {
	*B
	builderOf[R]
}](name, help string,
	measure func([]byte) (int64, error),
	loadBuilder func(PB, []byte, *TableReport) bool,
	fromJSON func(PB, []byte, *TableReport) bool,
	wireMeasure func(*R, ...TableWriteContext) int64,
	wireSave func(*R, []byte, ...TableWriteContext) int64,
	jsonMeasure func(*R, ...TableAllocator) int64,
	jsonSave func(*R, []byte, ...TableAllocator) int64,
) erased {
	c := &codec[R, B, PB]{}
	via := func(h *held[R], load func(PB, *TableReport) bool) schemaread.Report {
		b := PB(new(B))
		if !b.Init() {
			return schemaread.Report{Malformed: true}
		}
		c.live = append(c.live, b)
		var rep TableReport
		ok := load(b, &rep)
		out := report(&rep, ok)
		if out.Malformed || out.Reason != "" || out.Clamped > 0 || out.KindMismatch > 0 {
			return out // refused by the strict read; nothing is kept
		}
		// A table with a list or a map keeps its references in the builder's
		// arena; Lock packs them into one relocatable region, whose root the
		// generated Measure, Save and ToJson read without the arena.
		if !b.Lock() {
			return schemaread.Report{Malformed: true}
		}
		h.root = b.AsConst()
		if h.root == nil {
			return schemaread.Report{Malformed: true}
		}
		return out
	}
	c.jsonOf = func(v *R) []byte {
		return whole(func() int64 { return jsonMeasure(v) }, func(b []byte) int64 { return jsonSave(v, b) })
	}
	c.rd = schemaread.NewReader(&schemaread.Codec[held[R]]{
		Name:  name,
		Help:  help,
		Reset: func(h *held[R]) { h.root = nil },
		Measure: func(wire []byte) error {
			_, err := measure(wire)
			return err
		},
		LoadWire: func(h *held[R], wire []byte) schemaread.Report {
			return via(h, func(b PB, rep *TableReport) bool { return loadBuilder(b, wire, rep) })
		},
		LoadJSON: func(h *held[R], text []byte) schemaread.Report {
			return via(h, func(b PB, rep *TableReport) bool { return fromJSON(b, text, rep) })
		},
		SaveWire: func(h *held[R]) []byte {
			if h.root == nil {
				return nil
			}
			return whole(func() int64 { return wireMeasure(h.root) }, func(b []byte) int64 { return wireSave(h.root, b) })
		},
		SaveJSON: func(h *held[R]) []byte {
			if h.root == nil {
				return nil
			}
			return c.jsonOf(h.root)
		},
	})
	return c
}

// root is one table a verb prints with --json.
type root struct {
	table   string // the schema's table
	name    string // the refusal prefix
	help    string // the command a refusal points at
	newCode func() erased
	samples []string // today's output, one per shape the verb prints
}

// ---- today's output --------------------------------------------------------------

const (
	instant = "2026-10-17T04:07:11.123456789Z"
	zeroAt  = "0001-01-01T00:00:00Z"
	hex64   = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
)

const handoverSample = `{"at":"` + instant + `","seat":{"holder":"rowan","generation":4,"since":"` + instant + `","last":{"holder":"rowan","generation":4,"from":"stella","at":"` + instant + `","by":"rowan","taken":true,"approved_by":"glenn","reason":"handover"}},
"owner":"glenn","server":{"actor":"rowan","change":"set the unit to rowan"},"machine":"RUNNING since 04:00","summary":"38 of 118 in line",
"streams":[{"stream":"v15-schema-tables","counts":{"waiting":3,"ready":0,"working":1,"review":0,"merging":0,"landed":9}}],
"sentinels":[{"id":"v15.sentinel","stream":"v15-schema-tables","reached":false,"behind":["a12-defs.w1","a12-from"]}],
"judgments":[{"id":"v15-schema-tables/stop","alias":"j3","kind":"judgment","type":"stream stopped","what":"a card conflicted","stream":"v15-schema-tables","size":2,"cards":["a12-defs.w1"],"notes":["n1"],"marked":true,"overdue":false,"waited":"4m0s","due":"` + instant + `","answers":[{"decision":"retry","commands":["nova-sprint retry a12-defs.w1"]}],"decisions":["retry","drop"]}],
"members":[{"member":"m1","status":"held","by":"rowan"},{"member":"m2","status":"down"}],
"routes":{"enabled":3,"disabled":["slow-route"]},
"decisions":[{"at":"` + instant + `","verb":"release","what":"card a12-defs","by":"rowan","reason":"gate green"}],
"rules":["never wait"],"first":["nova-sprint where"]}`

const logLineSample = `{"kind":"move","at":"` + instant + `","epoch":3,"op":"01HXYZ","card":"a12-defs.w1","cards":["a12-defs.w1","a12-from.w1"],"primary":"a12-defs","stream":"v15-schema-tables","table":"work","from":"m1:waiting","to":"m1:working","removed":false,"gen":2,"actor":"rowan","verb":"release","cause":"gate green","answers":["j3"],"text":{"report":"done","brief":"RESULT: x"},"set":{"head":"abcdef"},
"note":{"stream_transition":{"state":"stopped","cause":"conflict","waiting":2,"owners":["m1"]},"id":"n1","safe_default":"retry","decision_delivery":{"lane":"friend.amy","to":"friend.amy","from":"rowan","set":"s","body":"b","token":"t","prepared_at":"` + instant + `","retry_before":"` + instant + `","sent_at":"` + instant + `","fault":"","reports":["r1"],"unknown_receipts":1},"default_coordinator_told":true,"coordinator_delivery":{"CoveredBy":"n0","From":"rowan","To":"glenn","Token":"t","Subject":"s","Body":"b","Episode":"e","Sent":true,"RetryBefore":"` + instant + `"},"alias":"j3","kind":"judgment","type":"stream stopped","stream":"v15-schema-tables","primaries":["a12-defs"],"count":2,"what":"a card conflicted","who":"m1","attempt":2,"before":1,"at":"` + instant + `","decisions":["retry"],"marked":true,"answers":"j2","suspects":["a12-from"],"card":"a12-defs","other":"a12-from","tier":"pro","other_stream":"v16","stream_level":true,"needs":["a12-from"],"review":"` + instant + `","review_set":"` + instant + `","to":"rowan","hint":"run: nova-sprint where"}}`

const partSample = `{"name":"deal","verb":"deal","op":"01HXYZ","moved":["a12-defs.w1"],"refused":[{"id":"a12-from","why":"needs a12-defs"}],"notes":1,"raised":["j3 stop"],"said":["x"],"attempts":2,"replay":true,"repaired":["01H"],"pending":"01HP","skipped":["card"],"args":"a b","lost":true,"tables":{"work":2,"fleet":1},"drained":[{"verb":"drain","moved":["z"],"refused":[],"notes":0,"attempts":1,"tables":{"work":1}}]}`

const planSample = `{"epoch":3,"state":"running","parts":[{"table":"work","name":"deal","size":2,"due":1}],"size":2,"took_ns":123456,"reads":["read cards wait"]}`

const measureSample = `{"median_s":1.5,"max_s":3,"n":2}`

var samples = map[string][]string{
	"SayOK": {`{"verb":"seat","status":"ok","exit":0}`},
	"UnitRemoved": {
		`{"path":"/home/u/.config/systemd/user/nova-sprint-server.service","present":true,"dry_run":true}`,
		`{"path":"/home/u/.config/systemd/user/nova-sprint-server.service","removed":true}`,
	},
	"UnitInstalled": {
		`{"path":"/home/u/.config/systemd/user/nova-sprint-server.service","unit":"[Unit]\nDescription=nova-sprint\n[Service]\nExecStart=/usr/bin/nova-sprint serve\n","args":["nova-sprint","serve","--listen",":7000"],"dry_run":true}`,
		`{"path":"/home/u/.config/systemd/user/nova-sprint-server.service","written":true,"loaded":true,"args":["nova-sprint","serve"]}`,
	},
	"UnitsCheck":      {`{"dir":"/home/u/.config/systemd/user","units":[{"kind":"server","path":"/x/server.service","state":"installed","install":"nova-sprint server install"},{"kind":"seat","owed":"login","path":"/x/seat.service","state":"missing","why":"no file","install":"nova-sprint seat install"}],"ok":false}`},
	"SeatInstall":     {`{"server":"sprint.example:7000","dry_run":true,"seat":["SEAT INSTALL DRY-RUN login=/x","  record=/y"]}`},
	"SeatCoordinator": {`{"holder":"rowan","from":"stella","by":"rowan","taken":true,"approved_by":"glenn","op":"01HXYZ","handover":` + handoverSample + `}`},
	"Handover":        {handoverSample},
	"SeatCheck": {
		`{"verb":"seat","status":"ok","exit":0,"holder":"rowan","epoch":3,"generation":4,"record":"rowan","server":"sprint.example:7000"}`,
		`{"holder":"rowan","epoch":3,"generation":4,"record":"stella","server":"sprint.example:7000","drift":"the key holds rowan, the record stella","status":"drift","exit":1}`,
	},
	"SeatRepaired": {`{"verb":"seat","status":"ok","exit":0,"holder":"rowan","was":"","by":"rowan","op":"01HXYZ","drift":""}`},
	"SeatRecord":   {`{"server":"sprint.example:7000","config_seat":"rowan"}`},
	"StoreLogin":   {`{"redis":"studio.local:6379","user":"rowan","store":"/home/u/secrets","as":"seat","key":"/home/u/.key","sops":"sops","secret":"NOVA_SPRINT_REDIS"}`},
	"SyncReport": {
		`{"verb":"fleet sync","check":false,"members":3,"drift":[{"member":"m1","kind":"width","from":2,"to":4},{"member":"m2","kind":"add"}],"held":["m2"],"holding":["m3 holds"],"waiting":["m4 waits"],"moved":["m1"],"refused":[],"error":"the step is not finished"}`,
	},
	"WakeState":     {`{"seeded":true,"bus":"1760000000000-0","judgments":["j1"],"stop":"","down":{"friend.amy":2},"told":["friend.amy"],"last":{"judgment":"` + instant + `"}}`},
	"WakeAnswer":    {`{"cursor":"1760000000000-3","woke":true}`},
	"GoalList":      {`{"goals":[{"name":"rowan","route":"rowan","last":"` + instant + `","count":3,"state":"ok","text":"keep going\n"}]}`},
	"GoalShow":      {`[{"name":"rowan","route":"rowan","last":"` + instant + `","count":3,"state":"ok","text":"keep going\n"}]`},
	"GoalSet":       {`{"name":"rowan","route":"rowan","last":"` + zeroAt + `","count":0,"state":"waiting","new":true}`},
	"GoalDrop":      {`{"name":"rowan","dropped":true}`},
	"SnapshotDrill": {`{"file":"/x/snapshot-1.rdb","sha256":"` + hex64 + `","counts":{"keys":1200,"cards":300},"restore":"integrity"}`},
	"SnapshotTaken": {`{"file":"/x/snapshot-2.rdb","sha256":"` + hex64 + `","bytes":123456,"counts":{"keys":1200,"cards":300},"restore":"semantic","pruned":["/x/snapshot-0.rdb"]}`},
	"ShadowOut":     {`{"shadow":` + planSample + `}`, `{"error":"the store is not reachable"}`},
	"ShadowRecord":  {`{"binary":"/x/nova-sprint","at":"` + instant + `","wall_ns":1500000,"plan":` + planSample + `}`},
	"SettingValue":  {`{"flag":"--stall","prop":"stall","value":"10m","default":"10m","set":false,"accessor":"Stall"}`},
	"SettingsOk":    {`{"count":12,"set":2,"epoch":3}`},
	"MachineOut": {
		`{"before":"stopped","after":"running","changed":true,"notes":1,"sprint":"38 of 118 in line"}`,
		`{"before":"running","after":"stopped","changed":true,"notes":0,"error":"x","sprint":"s","reason":"maintenance","until":"` + instant + `"}`,
		`{"tick":{"state":"running","tick_end":1,"idle":false,"repaired":[{"op":"01H","verb":"release","done":"finished-with-skips","detail":"d","skipped":["card"]}],"parts":[` + partSample + `],"stale":"s","halted":"h","due":2,"quiet":[{"type":"lateness","subject":"a12","why":"live","friend":"friend.amy","run":"nova-sprint x"}],"tables":[{"table":"work","rows":["a12-defs"]},{"table":"fleet","rows":[]}],"done":"the sprint is done","hint":"run: x","archive":{"archived":["v14"],"shown":["v15"]},"order":["work","readers","merge","fleet","end"],"times":[{"table":"work","name":"deal","took_ns":1000,"trips":3,"reads":1,"rows":10,"stale":1,"mismatch":0,"asked":2,"refused":1,"lost":1}],"took_ns":55000,"busy_retries":1,"route_trips":2,"said":["a grant is lacking"]},"changed":false,"notes":2,"sprint":"38 of 118 in line","moved":["a12-defs.w1"]}`,
	},
	"RemindView":  {`{"id":"t1","for":"rowan","due":"` + instant + `","note":"check the gate","by":"rowan","set":"` + instant + `","dry_run":true,"outcome":"accepted","changed":"no"}`},
	"RemindList":  {`{"items":[{"id":"t1","for":"rowan","due":"` + instant + `","note":"n","by":"rowan","set":"` + instant + `"}],"more":{"shown":1,"total":3},"outcome":"accepted","changed":"no"}`},
	"LogOut":      {`{"lines":[` + logLineSample + `,{"kind":"note","at":"` + instant + `","epoch":3}]}`},
	"StreamsView": {`{"streams":[{"stream":"v15-schema-tables","repos":["mas-bandwidth/nova-sprint"],"bases":["sprint/v15-schema-tables"],"release":"v1.5","display":"v1.5 schema tables","state":"open","open":4,"landed":9,"cards":[{"id":"a12-defs.w1","state":"working","tier":"pro","title":"defs","needs":["a12-from"]}]}],"mixed":["v14"]}`},
	"PassStats":   {`{"epoch":3,"since":"` + instant + `","primaries":4,"stages":{"deal_wait":` + measureSample + `,"finish_to_2reads":` + measureSample + `,"accept_to_land":` + measureSample + `,"total":` + measureSample + `},"work":[{"member":"m1","cards":3,"failed":1,"take_wait":` + measureSample + `,"run_wall":` + measureSample + `,"report_lag":` + measureSample + `}],"reads":[{"reader":"r1","cards":2,"begin_wait":` + measureSample + `,"run_wall":` + measureSample + `,"report_lag":` + measureSample + `}],"routes":[{"route":"opus","takes":5,"ok":3,"failed":1,"provider_failures":1,"run_wall":` + measureSample + `}]}`},
}

// roots are the objects the verbs print, each with its table and its samples.
func roots() []root {
	return []root{
		{table: "SayOK", name: "SAYOK", help: "nova-sprint help", samples: samples["SayOK"], newCode: func() erased {
			return newCodec("SAYOK", "nova-sprint help", SayOKLoadMeasureReason, SayOKLoadBuilder, SayOKFromJson, SayOKMeasure, SayOKSave, SayOKToJsonMeasure, SayOKToJson)
		}},
		{table: "UnitRemoved", name: "UNIT-REMOVED", help: "nova-sprint seat uninstall -h", samples: samples["UnitRemoved"], newCode: func() erased {
			return newCodec("UNIT-REMOVED", "nova-sprint seat uninstall -h", UnitRemovedLoadMeasureReason, UnitRemovedLoadBuilder, UnitRemovedFromJson, UnitRemovedMeasure, UnitRemovedSave, UnitRemovedToJsonMeasure, UnitRemovedToJson)
		}},
		{table: "UnitInstalled", name: "UNIT-INSTALLED", help: "nova-sprint seat install -h", samples: samples["UnitInstalled"], newCode: func() erased {
			return newCodec("UNIT-INSTALLED", "nova-sprint seat install -h", UnitInstalledLoadMeasureReason, UnitInstalledLoadBuilder, UnitInstalledFromJson, UnitInstalledMeasure, UnitInstalledSave, UnitInstalledToJsonMeasure, UnitInstalledToJson)
		}},
		{table: "UnitsCheck", name: "UNITS-CHECK", help: "nova-sprint units -h", samples: samples["UnitsCheck"], newCode: func() erased {
			return newCodec("UNITS-CHECK", "nova-sprint units -h", UnitsCheckLoadMeasureReason, UnitsCheckLoadBuilder, UnitsCheckFromJson, UnitsCheckMeasure, UnitsCheckSave, UnitsCheckToJsonMeasure, UnitsCheckToJson)
		}},
		{table: "SeatInstall", name: "SEAT-INSTALL", help: "nova-sprint seat install -h", samples: samples["SeatInstall"], newCode: func() erased {
			return newCodec("SEAT-INSTALL", "nova-sprint seat install -h", SeatInstallLoadMeasureReason, SeatInstallLoadBuilder, SeatInstallFromJson, SeatInstallMeasure, SeatInstallSave, SeatInstallToJsonMeasure, SeatInstallToJson)
		}},
		{table: "SeatCoordinator", name: "COORDINATOR", help: "nova-sprint coordinator -h", samples: samples["SeatCoordinator"], newCode: func() erased {
			return newCodec("COORDINATOR", "nova-sprint coordinator -h", SeatCoordinatorLoadMeasureReason, SeatCoordinatorLoadBuilder, SeatCoordinatorFromJson, SeatCoordinatorMeasure, SeatCoordinatorSave, SeatCoordinatorToJsonMeasure, SeatCoordinatorToJson)
		}},
		{table: "Handover", name: "HANDOVER", help: "nova-sprint handover -h", samples: samples["Handover"], newCode: func() erased {
			return newCodec("HANDOVER", "nova-sprint handover -h", HandoverLoadMeasureReason, HandoverLoadBuilder, HandoverFromJson, HandoverMeasure, HandoverSave, HandoverToJsonMeasure, HandoverToJson)
		}},
		{table: "SeatCheck", name: "SEAT", help: "nova-sprint seat -h", samples: samples["SeatCheck"], newCode: func() erased {
			return newCodec("SEAT", "nova-sprint seat -h", SeatCheckLoadMeasureReason, SeatCheckLoadBuilder, SeatCheckFromJson, SeatCheckMeasure, SeatCheckSave, SeatCheckToJsonMeasure, SeatCheckToJson)
		}},
		{table: "SeatRepaired", name: "SEAT-REPAIRED", help: "nova-sprint seat -h", samples: samples["SeatRepaired"], newCode: func() erased {
			return newCodec("SEAT-REPAIRED", "nova-sprint seat -h", SeatRepairedLoadMeasureReason, SeatRepairedLoadBuilder, SeatRepairedFromJson, SeatRepairedMeasure, SeatRepairedSave, SeatRepairedToJsonMeasure, SeatRepairedToJson)
		}},
		{table: "SeatRecord", name: "SEAT-RECORD", help: "nova-sprint seat install -h", samples: samples["SeatRecord"], newCode: func() erased {
			return newCodec("SEAT-RECORD", "nova-sprint seat install -h", SeatRecordLoadMeasureReason, SeatRecordLoadBuilder, SeatRecordFromJson, SeatRecordMeasure, SeatRecordSave, SeatRecordToJsonMeasure, SeatRecordToJson)
		}},
		{table: "StoreLogin", name: "STORE-LOGIN", help: "nova-sprint seat login -h", samples: samples["StoreLogin"], newCode: func() erased {
			return newCodec("STORE-LOGIN", "nova-sprint seat login -h", StoreLoginLoadMeasureReason, StoreLoginLoadBuilder, StoreLoginFromJson, StoreLoginMeasure, StoreLoginSave, StoreLoginToJsonMeasure, StoreLoginToJson)
		}},
		{table: "SyncReport", name: "FLEET-SYNC", help: "nova-sprint fleet sync -h", samples: samples["SyncReport"], newCode: func() erased {
			return newCodec("FLEET-SYNC", "nova-sprint fleet sync -h", SyncReportLoadMeasureReason, SyncReportLoadBuilder, SyncReportFromJson, SyncReportMeasure, SyncReportSave, SyncReportToJsonMeasure, SyncReportToJson)
		}},
		{table: "WakeState", name: "WAKE-STATE", help: "nova-sprint watch -h", samples: samples["WakeState"], newCode: func() erased {
			return newCodec("WAKE-STATE", "nova-sprint watch -h", WakeStateLoadMeasureReason, WakeStateLoadBuilder, WakeStateFromJson, WakeStateMeasure, WakeStateSave, WakeStateToJsonMeasure, WakeStateToJson)
		}},
		{table: "WakeAnswer", name: "WAKE-ANSWER", help: "nova-sprint log -h", samples: samples["WakeAnswer"], newCode: func() erased {
			return newCodec("WAKE-ANSWER", "nova-sprint log -h", WakeAnswerLoadMeasureReason, WakeAnswerLoadBuilder, WakeAnswerFromJson, WakeAnswerMeasure, WakeAnswerSave, WakeAnswerToJsonMeasure, WakeAnswerToJson)
		}},
		{table: "GoalList", name: "GOAL-LIST", help: "nova-sprint goal list -h", samples: samples["GoalList"], newCode: func() erased {
			return newCodec("GOAL-LIST", "nova-sprint goal list -h", GoalListLoadMeasureReason, GoalListLoadBuilder, GoalListFromJson, GoalListMeasure, GoalListSave, GoalListToJsonMeasure, GoalListToJson)
		}},
		{table: "GoalSet", name: "GOAL-SET", help: "nova-sprint goal set -h", samples: samples["GoalSet"], newCode: func() erased {
			return newCodec("GOAL-SET", "nova-sprint goal set -h", GoalSetLoadMeasureReason, GoalSetLoadBuilder, GoalSetFromJson, GoalSetMeasure, GoalSetSave, GoalSetToJsonMeasure, GoalSetToJson)
		}},
		{table: "GoalDrop", name: "GOAL-DROP", help: "nova-sprint goal drop -h", samples: samples["GoalDrop"], newCode: func() erased {
			return newCodec("GOAL-DROP", "nova-sprint goal drop -h", GoalDropLoadMeasureReason, GoalDropLoadBuilder, GoalDropFromJson, GoalDropMeasure, GoalDropSave, GoalDropToJsonMeasure, GoalDropToJson)
		}},
		{table: "SnapshotDrill", name: "SNAPSHOT-DRILL", help: "nova-sprint snapshot -h", samples: samples["SnapshotDrill"], newCode: func() erased {
			return newCodec("SNAPSHOT-DRILL", "nova-sprint snapshot -h", SnapshotDrillLoadMeasureReason, SnapshotDrillLoadBuilder, SnapshotDrillFromJson, SnapshotDrillMeasure, SnapshotDrillSave, SnapshotDrillToJsonMeasure, SnapshotDrillToJson)
		}},
		{table: "SnapshotTaken", name: "SNAPSHOT", help: "nova-sprint snapshot -h", samples: samples["SnapshotTaken"], newCode: func() erased {
			return newCodec("SNAPSHOT", "nova-sprint snapshot -h", SnapshotTakenLoadMeasureReason, SnapshotTakenLoadBuilder, SnapshotTakenFromJson, SnapshotTakenMeasure, SnapshotTakenSave, SnapshotTakenToJsonMeasure, SnapshotTakenToJson)
		}},
		{table: "ShadowOut", name: "SHADOW", help: "nova-sprint tick -h", samples: samples["ShadowOut"], newCode: func() erased {
			return newCodec("SHADOW", "nova-sprint tick -h", ShadowOutLoadMeasureReason, ShadowOutLoadBuilder, ShadowOutFromJson, ShadowOutMeasure, ShadowOutSave, ShadowOutToJsonMeasure, ShadowOutToJson)
		}},
		{table: "ShadowRecord", name: "SHADOW-RECORD", help: "nova-sprint server switch -h", samples: samples["ShadowRecord"], newCode: func() erased {
			return newCodec("SHADOW-RECORD", "nova-sprint server switch -h", ShadowRecordLoadMeasureReason, ShadowRecordLoadBuilder, ShadowRecordFromJson, ShadowRecordMeasure, ShadowRecordSave, ShadowRecordToJsonMeasure, ShadowRecordToJson)
		}},
		{table: "SettingValue", name: "SETTING", help: "nova-sprint settings -h", samples: samples["SettingValue"], newCode: func() erased {
			return newCodec("SETTING", "nova-sprint settings -h", SettingValueLoadMeasureReason, SettingValueLoadBuilder, SettingValueFromJson, SettingValueMeasure, SettingValueSave, SettingValueToJsonMeasure, SettingValueToJson)
		}},
		{table: "SettingsOk", name: "SETTINGS", help: "nova-sprint settings -h", samples: samples["SettingsOk"], newCode: func() erased {
			return newCodec("SETTINGS", "nova-sprint settings -h", SettingsOkLoadMeasureReason, SettingsOkLoadBuilder, SettingsOkFromJson, SettingsOkMeasure, SettingsOkSave, SettingsOkToJsonMeasure, SettingsOkToJson)
		}},
		{table: "MachineOut", name: "MACHINE", help: "nova-sprint tick -h", samples: samples["MachineOut"], newCode: func() erased {
			return newCodec("MACHINE", "nova-sprint tick -h", MachineOutLoadMeasureReason, MachineOutLoadBuilder, MachineOutFromJson, MachineOutMeasure, MachineOutSave, MachineOutToJsonMeasure, MachineOutToJson)
		}},
		{table: "RemindView", name: "REMIND", help: "nova-sprint remind -h", samples: samples["RemindView"], newCode: func() erased {
			return newCodec("REMIND", "nova-sprint remind -h", RemindViewLoadMeasureReason, RemindViewLoadBuilder, RemindViewFromJson, RemindViewMeasure, RemindViewSave, RemindViewToJsonMeasure, RemindViewToJson)
		}},
		{table: "RemindList", name: "REMIND-LIST", help: "nova-sprint remind -h", samples: samples["RemindList"], newCode: func() erased {
			return newCodec("REMIND-LIST", "nova-sprint remind -h", RemindListLoadMeasureReason, RemindListLoadBuilder, RemindListFromJson, RemindListMeasure, RemindListSave, RemindListToJsonMeasure, RemindListToJson)
		}},
		{table: "LogOut", name: "LOG", help: "nova-sprint log -h", samples: samples["LogOut"], newCode: func() erased {
			return newCodec("LOG", "nova-sprint log -h", LogOutLoadMeasureReason, LogOutLoadBuilder, LogOutFromJson, LogOutMeasure, LogOutSave, LogOutToJsonMeasure, LogOutToJson)
		}},
		{table: "StreamsView", name: "STREAMS", help: "nova-sprint streams -h", samples: samples["StreamsView"], newCode: func() erased {
			return newCodec("STREAMS", "nova-sprint streams -h", StreamsViewLoadMeasureReason, StreamsViewLoadBuilder, StreamsViewFromJson, StreamsViewMeasure, StreamsViewSave, StreamsViewToJsonMeasure, StreamsViewToJson)
		}},
		{table: "PassStats", name: "STATS", help: "nova-sprint stats -h", samples: samples["PassStats"], newCode: func() erased {
			return newCodec("STATS", "nova-sprint stats -h", PassStatsLoadMeasureReason, PassStatsLoadBuilder, PassStatsFromJson, PassStatsMeasure, PassStatsSave, PassStatsToJsonMeasure, PassStatsToJson)
		}},
	}
}

// ---- the test ------------------------------------------------------------------------

var allForms = []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64, schemaread.FormJSON}

// refused checks err is a *schemaread.Refusal in the house style
// `<NAME> REFUSED: <reason>; run: <help>` and returns the reason.
func refused(t *testing.T, err error, r root, what ...any) string {
	t.Helper()
	require.Error(t, err, what...)
	var ref *schemaread.Refusal
	require.True(t, errors.As(err, &ref), "every refusal is a *schemaread.Refusal: %v", err)
	assert.True(t, strings.HasPrefix(err.Error(), r.name+" REFUSED: "), err.Error())
	assert.True(t, strings.HasSuffix(err.Error(), "; run: "+r.help), err.Error())
	assert.NotEmpty(t, ref.Reason)
	return ref.Reason
}

// roundTrip writes text (JSON) in every form, reads each form back strictly and
// holds the JSON each read writes equal to want; the counter of each form
// ends at one read.
func roundTrip(t *testing.T, r root, text []byte, want any) {
	t.Helper()
	c := r.newCode()
	for _, form := range allForms {
		img, err := c.write(text, form)
		require.NoError(t, err, "%s: write %s", r.name, form)
		require.NotEmpty(t, img)
		back, res, err := c.read(img)
		require.NoError(t, err, "%s: read %s", r.name, form)
		assert.Equal(t, form, res.Form, "%s: the form read", r.name)
		assert.Zero(t, res.Unknown, "%s: nothing unknown in what this writer wrote", r.name)
		assert.Equal(t, want, prune(decode(t, back)), "%s: %s round trip", r.name, form)
	}
	// each write reads the text once (three), and one read back of the JSON image
	for _, form := range []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64} {
		assert.Equal(t, uint64(1), c.counts().Reads(form), "%s: one %s read counted", r.name, form)
	}
	assert.Equal(t, uint64(4), c.counts().Reads(schemaread.FormJSON), "%s: the JSON reads counted", r.name)
}

func TestVerbJSONGolden(t *testing.T) {
	t.Parallel()
	defs, order := loadSchema(t)
	rs := roots()

	// the roots are tables of the schema, and together reach every table.
	seen := map[string]bool{}
	reaches(defs, "GoalShow", seen)
	for _, r := range rs {
		require.Contains(t, defs, r.table, "root %s is no table of the schema", r.table)
		require.NotEmpty(t, r.samples, "root %s has no sample of today's output", r.table)
		reaches(defs, r.table, seen)
	}
	for _, name := range order {
		assert.True(t, seen[name], "table %s is reached from no root: a verb prints it or it is dead", name)
	}

	for _, r := range rs {
		t.Run(r.table, func(t *testing.T) {
			t.Parallel()
			rootType := &typ{kind: "table", table: r.table}

			// today's output keeps its keys and values (S2: the JSON text form).
			for i, s := range r.samples {
				t.Run(fmt.Sprintf("sample%d", i), func(t *testing.T) {
					want := prune(decode(t, []byte(s)))
					require.NotNil(t, want)
					roundTrip(t, r, encode(t, decode(t, []byte(s))), want)
				})
			}

			// every field of every table below the root rides every form.
			t.Run("every field", func(t *testing.T) {
				full := base(defs, rootType)
				roundTrip(t, r, encode(t, full), prune(base(defs, rootType)))
			})

			// the generated JSON text is the bytes encoding/json writes for
			// today's Go value: same keys, same field order, same shapes.
			t.Run("json interop", func(t *testing.T) {
				full := base(defs, rootType)
				text := encode(t, full)
				got, _, err := r.newCode().read(text)
				require.NoError(t, err, "%s: read JSON", r.name)
				var compact bytes.Buffer
				require.NoError(t, json.Compact(&compact, got))
				want := encode(t, reflectValue(defs, rootType, full))
				assert.Equal(t, string(want), compact.String(), "%s: the generated JSON text is encoding/json's bytes", r.name)
			})

			// a value past a declared bound is refused by name, never clamped.
			t.Run("past each bound", func(t *testing.T) {
				c := r.newCode()
				n := 0
				for _, v := range overs(defs, rootType) {
					n++
					_, _, err := c.read(encode(t, v))
					assert.Equal(t, schemaread.ReasonClamped, refused(t, err, r), "%s: %s", r.name, firstDiff(encode(t, v)))
				}
				assert.Equal(t, uint64(n), c.counts().Refused(schemaread.ReasonClamped))
			})

			// a damaged image of each form is refused by name.
			t.Run("damaged", func(t *testing.T) {
				c := r.newCode()
				text := encode(t, base(defs, rootType))
				wire, err := c.write(text, schemaread.FormBinary)
				require.NoError(t, err)
				b64, err := c.write(text, schemaread.FormBase64)
				require.NoError(t, err)
				damaged := map[string][]byte{
					"binary truncated":      wire[:len(wire)/2],
					"binary one byte short": wire[:len(wire)-1],
					"binary form byte":      append([]byte{0x7f}, wire[1:]...),
					"base64 one char short": b64[:len(b64)-1],
					"base64 truncated":      b64[:len(b64)/2+1],
					"base64 alphabet":       append([]byte("!"), b64[1:]...),
					"json truncated":        text[:len(text)/2],
					"json garbage":          []byte(`{"zz":`),
					"nothing":               []byte("  "),
				}
				names := make([]string, 0, len(damaged))
				for name := range damaged {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					_, _, err := c.read(damaged[name])
					assert.NotEmpty(t, refused(t, err, r, "%s: the image %q is read, not refused", r.name, name))
				}
				var total uint64
				for name, n := range c.counts().Snapshot() {
					if strings.HasPrefix(name, "refused_") {
						total += n
					}
				}
				assert.Equal(t, uint64(len(damaged)), total, "%s: every damaged image is counted refused", r.name)
			})

			// a newer writer's field is skipped and counted (S1), in every form.
			t.Run("newer writer", func(t *testing.T) {
				c := r.newCode()
				v := base(defs, rootType)
				o, isMap := v.(map[string]any)
				if !isMap {
					t.Skip("an array root has no field to add a newer writer's field to")
				}
				o["zz_a_field_a_newer_writer_adds"] = 1.0
				_, res, err := c.read(encode(t, o))
				require.NoError(t, err)
				assert.Equal(t, 1, res.Unknown)
				assert.Equal(t, uint64(1), c.counts().Unknown())
			})
		})
	}

	// GoalShow is the area's array root (goal.go:188-190): its JSON text is a
	// top-level array. pkg/schemaread's Detect recognises JSON text only by a
	// leading '{', so that one form is refused by name (unknown_form) until
	// schemaread learns the array root; the binary and base64 forms round-trip.
	t.Run("GoalShow", func(t *testing.T) {
		t.Parallel()
		text := []byte(samples["GoalShow"][0])
		var builder GoalShowBuilder
		require.True(t, builder.Init())
		var rep TableReport
		require.True(t, GoalShowFromJson(&builder, text, &rep), "%s", rep.Err())
		require.True(t, builder.Lock())
		root := builder.AsConst()
		wire := whole(func() int64 { return GoalShowMeasure(root) }, func(b []byte) int64 { return GoalShowSave(root, b) })
		require.NotEmpty(t, wire)
		c := newCodec("GOAL-SHOW", "nova-sprint goal show -h", GoalShowLoadMeasureReason, GoalShowLoadBuilder, GoalShowFromJson, GoalShowMeasure, GoalShowSave, GoalShowToJsonMeasure, GoalShowToJson)
		for _, form := range []schemaread.Form{schemaread.FormBinary, schemaread.FormBase64} {
			img := wire
			if form == schemaread.FormBase64 {
				img = []byte(base64.StdEncoding.EncodeToString(wire))
			}
			back, res, err := c.read(img)
			require.NoError(t, err, "GOAL-SHOW: read %s", form)
			assert.Equal(t, form, res.Form)
			assert.Equal(t, prune(decode(t, text)), prune(decode(t, back)), "GOAL-SHOW: %s round trip", form)
		}
		_, _, err := c.read(text)
		require.Error(t, err)
		var ref *schemaread.Refusal
		require.True(t, errors.As(err, &ref))
		assert.Equal(t, schemaread.ReasonUnknownForm, ref.Reason, "schemaread has not learned the array root's JSON text")
	})
}

// reflectValue builds today's Go value of t (the struct encoding/json writes)
// from a generic value: fields in schema order, the schema's JSON key as the tag.
func reflectValue(defs map[string]*tableDef, t *typ, v any) any {
	_, rv := reflected(defs, t, v)
	return rv.Interface()
}

func reflected(defs map[string]*tableDef, t *typ, v any) (reflect.Type, reflect.Value) {
	switch t.kind {
	case "string":
		return reflect.TypeOf(""), reflect.ValueOf(v.(string))
	case "bool":
		return reflect.TypeOf(false), reflect.ValueOf(v.(bool))
	case "num":
		return reflect.TypeOf(float64(0)), reflect.ValueOf(v.(float64))
	case "list", "array":
		a, _ := v.([]any)
		et, ev := reflected(defs, t.elem, base(defs, t.elem))
		st := reflect.SliceOf(et)
		out := reflect.MakeSlice(st, len(a), len(a))
		for i, x := range a {
			_, e := reflected(defs, t.elem, x)
			out.Index(i).Set(e)
		}
		_ = ev
		return st, out
	case "map":
		m, _ := v.(map[string]any)
		et, _ := reflected(defs, t.elem, base(defs, t.elem))
		mt := reflect.MapOf(reflect.TypeOf(""), et)
		out := reflect.MakeMap(mt)
		for k, x := range m {
			_, e := reflected(defs, t.elem, x)
			out.SetMapIndex(reflect.ValueOf(k), e)
		}
		return mt, out
	case "table":
		def := defs[t.table]
		if def.array {
			return reflected(defs, def.fields[0].t, v)
		}
		o, _ := v.(map[string]any)
		var fs []reflect.StructField
		var vs []reflect.Value
		for i, f := range def.fields {
			ft, fv := reflected(defs, f.t, o[f.key])
			fs = append(fs, reflect.StructField{Name: fmt.Sprintf("F%d", i), Type: ft, Tag: reflect.StructTag(`json:"` + f.key + `"`)})
			vs = append(vs, fv)
		}
		st := reflect.StructOf(fs)
		out := reflect.New(st).Elem()
		for i := range vs {
			out.Field(i).Set(vs[i])
		}
		return st, out
	}
	panic("unknown kind " + t.kind)
}

// firstDiff names the longest string of the text, the value that crossed the bound.
func firstDiff(text []byte) string {
	best := ""
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if len(x) > len(best) {
				best = x
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				walk(k)
				walk(e)
			}
		}
	}
	var v any
	_ = json.Unmarshal(text, &v)
	walk(v)
	if len(best) > 24 {
		return fmt.Sprintf("a text of %d bytes", len(best))
	}
	return best
}

// TestVerbJSONPin holds the area's generate.go to the repository's schema pin
// (the pin internal/sprint/store/donewire/generate.go names), the baseline to
// the schema's tables, and the generated package to the schema's table list.
func TestVerbJSONPin(t *testing.T) {
	t.Parallel()
	pin := regexp.MustCompile(`//go:generate go run github\.com/mas-bandwidth/schema/v2/cmd/schema@(\S+) generate --lang go --out \. (\S+)`)
	read := func(path string) []byte {
		b, err := os.ReadFile(filepath.FromSlash(path))
		require.NoError(t, err)
		return b
	}
	mine := pin.FindSubmatch(read("generate.go"))
	theirs := pin.FindSubmatch(read("../../internal/sprint/store/donewire/generate.go"))
	require.NotNil(t, mine)
	require.NotNil(t, theirs)
	assert.Equal(t, string(theirs[1]), string(mine[1]), "one compiler writes the whole tree (schemas/README.md, The pin)")
	assert.Equal(t, "../../schemas/verbjson", string(mine[2]))

	defs, order := loadSchema(t)
	baseline := string(read("../../schemas/verbjson/tables.baseline"))
	for _, name := range order {
		assert.Contains(t, baseline, name, "tables.baseline names table %s", name)
	}
	assert.NotContains(t, baseline, "schema.lock", "a variable area keeps no schema.lock")
	_, isLocked := os.Stat(filepath.FromSlash("../../schemas/verbjson/schema.lock"))
	assert.True(t, os.IsNotExist(isLocked), "a variable area keeps no schema.lock")

	// every table is a generated type: its descriptor names it.
	infos := map[string]*TableTypeInfo{
		"SayOK": SayOKTableType(), "GoalList": GoalListTableType(), "GoalShow": GoalShowTableType(), "PassStats": PassStatsTableType(),
		"LogLine": LogLineTableType(), "TickResult": TickResultTableType(),
	}
	for name, info := range infos {
		assert.Equal(t, name, info.Name)
		assert.True(t, info.Variable, "%s is a variable table", name)
	}
	assert.Len(t, defs, len(order))
	assert.True(t, reflect.DeepEqual(order, uniq(order)), "a table is declared once")
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
