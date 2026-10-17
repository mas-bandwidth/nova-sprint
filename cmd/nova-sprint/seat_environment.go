package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/mas-bandwidth/nova-tools/pkg/nsprint/verbflag"
	"github.com/mas-bandwidth/nova-tools/pkg/schemaread"
	"github.com/mas-bandwidth/nova-tools/pkg/seatrecwire"
)

// seatEnvironment is data, never shell code or an identity assertion. Login
// names a secret reference; the existing login reader resolves it in process.
type seatEnvironment struct {
	Version       int               `json:"version"`
	Environment   map[string]string `json:"environment,omitempty"`
	Login         *storeLogin       `json:"login,omitempty"`
	ForgeIdentity string            `json:"forge_identity,omitempty"`
}

// seatHelp is the command a seat environment refusal points at. S2.
const seatHelp = "nova-sprint check"

// seatSchema is the seat environment as the generated SeatEnvironment table
// (seatrec.schema) carries it: the same record the legacy file holds, in the
// table's schema JSON text keys. S2: the wire, its base64, the JSON text and
// today's legacy text all read to the one seatEnvironment value.
type seatSchema struct {
	Version          *int32           `json:"version"`
	ForgeIdentity    *string          `json:"forge_identity"`
	NovaSprintRedis  *string          `json:"nova_sprint_redis"`
	NovaSprintServer *string          `json:"nova_sprint_server"`
	NovaBusRedis     *string          `json:"nova_bus_redis"`
	Login            *seatLoginSchema `json:"login"`
}

// seatLoginSchema is the SeatLogin sub-table's schema JSON text keys.
type seatLoginSchema struct {
	Redis  *string `json:"redis"`
	User   *string `json:"user"`
	Store  *string `json:"store"`
	As     *string `json:"as"`
	Key    *string `json:"key"`
	Sops   *string `json:"sops"`
	Secret *string `json:"secret"`
}

func seatText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func seatTextOf(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func seatSetEnv(e *seatEnvironment, k, v string) {
	if e.Environment == nil {
		e.Environment = map[string]string{}
	}
	e.Environment[k] = v
}

// seatToSchema is the seat record as the table's schema text: the environment
// map's three allowed keys become the table's three scalar endpoint fields.
// S2.
func seatToSchema(e *seatEnvironment) *seatSchema {
	s := &seatSchema{Version: func() *int32 { v := int32(e.Version); return &v }()}
	if v, ok := e.Environment["NOVA_SPRINT_REDIS"]; ok {
		s.NovaSprintRedis = seatText(v)
	}
	if v, ok := e.Environment["NOVA_SPRINT_SERVER"]; ok {
		s.NovaSprintServer = seatText(v)
	}
	if v, ok := e.Environment["NOVA_BUS_REDIS"]; ok {
		s.NovaBusRedis = seatText(v)
	}
	if e.Login != nil {
		s.Login = &seatLoginSchema{
			Redis: seatText(e.Login.Redis), User: seatText(e.Login.User), Store: seatText(e.Login.Store),
			As: seatText(e.Login.As), Key: seatText(e.Login.Key), Sops: seatText(e.Login.Sops), Secret: seatText(e.Login.Secret),
		}
	}
	s.ForgeIdentity = seatText(e.ForgeIdentity)
	return s
}

// seatFromSchema is seatToSchema read back. S2.
func seatFromSchema(m *seatSchema) (seatEnvironment, bool) {
	var e seatEnvironment
	if m.Version != nil {
		e.Version = int(*m.Version)
	}
	if v := seatTextOf(m.NovaSprintRedis); v != "" {
		seatSetEnv(&e, "NOVA_SPRINT_REDIS", v)
	}
	if v := seatTextOf(m.NovaSprintServer); v != "" {
		seatSetEnv(&e, "NOVA_SPRINT_SERVER", v)
	}
	if v := seatTextOf(m.NovaBusRedis); v != "" {
		seatSetEnv(&e, "NOVA_BUS_REDIS", v)
	}
	if m.Login != nil {
		e.Login = &storeLogin{
			Redis: seatTextOf(m.Login.Redis), User: seatTextOf(m.Login.User), Store: seatTextOf(m.Login.Store),
			As: seatTextOf(m.Login.As), Key: seatTextOf(m.Login.Key), Sops: seatTextOf(m.Login.Sops), Secret: seatTextOf(m.Login.Secret),
		}
	}
	e.ForgeIdentity = seatTextOf(m.ForgeIdentity)
	return e, true
}

// seatTableT is the generated SeatEnvironment table as this card reads it
// (S1, S2): the measure, the variable-table load and the JSON text reader.
type seatTableT struct {
	measure  func([]byte) (int64, error)
	load     func(region, wire []byte, report *seatrecwire.TableReport) *seatrecwire.SeatEnvironment
	fromJSON func(text []byte, rep *seatrecwire.TableReport) (*seatrecwire.SeatEnvironment, bool)
	toJSON   func(w *seatrecwire.SeatEnvironment) []byte
	save     func(w *seatrecwire.SeatEnvironment) []byte
}

func seatTable() seatTableT {
	return seatTableT{
		measure: seatrecwire.SeatEnvironmentLoadMeasureReason,
		load:    seatrecwire.SeatEnvironmentLoad,
		fromJSON: func(text []byte, rep *seatrecwire.TableReport) (*seatrecwire.SeatEnvironment, bool) {
			var b seatrecwire.SeatEnvironmentBuilder
			if !b.Init() {
				return nil, false
			}
			if !seatrecwire.SeatEnvironmentFromJson(&b, text, rep) || !b.Lock() {
				return nil, false
			}
			return b.AsConst(), true
		},
		toJSON: func(w *seatrecwire.SeatEnvironment) []byte {
			buf := make([]byte, max(seatrecwire.SeatEnvironmentToJsonMeasure(w), 0))
			return buf[:max(seatrecwire.SeatEnvironmentToJson(w, buf), 0)]
		},
		save: func(w *seatrecwire.SeatEnvironment) []byte {
			buf := make([]byte, max(seatrecwire.SeatEnvironmentMeasure(w), 0))
			return buf[:max(seatrecwire.SeatEnvironmentSave(w, buf), 0)]
		},
	}
}

// seatRegion is an aligned buffer of n bytes for a variable-table region load.
func seatRegion(n int64) []byte {
	raw := make([]byte, n+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	return raw[off : off+uintptr(n)]
}

// seatReport is the generated report as the strict read decides on it. S2: a
// key given twice is malformed, never last-wins.
func seatReport(r *seatrecwire.TableReport) schemaread.Report {
	return schemaread.Report{
		Unknown: int(r.Unknown), KindMismatch: int(r.KindMismatch), Clamped: int(r.Clamped),
		Malformed: r.Malformed || r.Duplicate > 0, Reason: r.Reason,
	}
}

// seatRefuses says the strict read refuses a load with this report.
func seatRefuses(r schemaread.Report) bool {
	return r.Reason != "" || r.Malformed || r.KindMismatch > 0 || r.Clamped > 0
}

// seatLegacyObject reports today's legacy seat JSON, as distinct from the
// schema JSON text. The generated writer ends the schema text with one newline
// and the reader accepts either; encoding/json writes no newline. S2.
func seatLegacyObject(in []byte) bool {
	t := bytes.TrimLeft(in, " \t\r\n")
	if len(t) == 0 || t[0] != '{' {
		return false
	}
	return !bytes.HasSuffix(bytes.TrimRight(in, " \t"), []byte{'\n'})
}

// seatLegacyReport is a legacy read's error as the strict read reports it. The
// legacy strictness stays: an unknown key, a duplicate key and a null field
// keep their names. S2.
func seatLegacyReport(err error) schemaread.Report {
	switch err.Error() {
	case "duplicate key":
		return schemaread.Report{Malformed: true, Reason: "duplicate key"}
	case "trailing data":
		return schemaread.Report{Malformed: true, Reason: "trailing data"}
	case "unknown field", "null field", "unknown login field":
		return schemaread.Report{Malformed: true, Reason: err.Error()}
	}
	return schemaread.Report{Malformed: true, Reason: "malformed JSON"}
}

// seatLoadLegacy is today's read of the seat file: the same pre-walks and the
// same strict decode the legacy read runs, as the strict read's legacy form.
// S2.
func seatLoadLegacy(v *seatEnvironment, in []byte) schemaread.Report {
	if err := uniqueJSONKeys(in); err != nil {
		return seatLegacyReport(err)
	}
	if err := seatJSONFields(in); err != nil {
		return seatLegacyReport(err)
	}
	if err := json.Unmarshal(in, v); err != nil {
		return schemaread.Report{Malformed: true, Reason: "legacy_json_unreadable"}
	}
	return schemaread.Report{}
}

// newSeatEnvironmentReader is the one seat environment reader, a schemaread
// reader of the generated SeatEnvironment table with today's legacy form (S1,
// S2, S4): the fixed wire, padded standard base64 of that wire, the schema
// JSON text and today's legacy JSON each decode to the one seatEnvironment
// value; damage is refused by name and each read is counted under its form.
func newSeatEnvironmentReader() *schemaread.Reader[seatEnvironment] {
	tab := seatTable()
	value := func(v *seatEnvironment, w *seatrecwire.SeatEnvironment) schemaread.Report {
		dec := json.NewDecoder(bytes.NewReader(tab.toJSON(w)))
		dec.DisallowUnknownFields()
		var m seatSchema
		if err := dec.Decode(&m); err != nil {
			return schemaread.Report{Malformed: true}
		}
		t, _ := seatFromSchema(&m)
		*v = t
		return schemaread.Report{}
	}
	table := func(v *seatEnvironment) *seatrecwire.SeatEnvironment {
		text, err := json.Marshal(seatToSchema(v))
		if err != nil {
			return nil
		}
		var rep seatrecwire.TableReport
		w, ok := tab.fromJSON(text, &rep)
		if !ok || seatRefuses(seatReport(&rep)) {
			return nil
		}
		return w
	}
	return schemaread.NewReader(&schemaread.Codec[seatEnvironment]{
		Name:  "SEATENVIRONMENT",
		Help:  seatHelp,
		Reset: func(v *seatEnvironment) { *v = seatEnvironment{} },
		Forms: []byte{1}, // the table wire: seatrec.schema emits no fixed form (S1)
		Measure: func(wire []byte) error {
			_, err := tab.measure(wire)
			return err
		},
		LoadWire: func(v *seatEnvironment, wire []byte) schemaread.Report {
			n, err := tab.measure(wire)
			if err != nil || n < 0 {
				return schemaread.Report{Malformed: true}
			}
			var rep seatrecwire.TableReport
			w := tab.load(seatRegion(n), wire, &rep)
			if w == nil {
				return schemaread.Report{Malformed: true, Reason: rep.Reason}
			}
			if rr := seatReport(&rep); seatRefuses(rr) {
				return rr
			}
			rr := value(v, w)
			rr.Unknown = int(rep.Unknown)
			return rr
		},
		LoadJSON: func(v *seatEnvironment, text []byte) schemaread.Report {
			var rep seatrecwire.TableReport
			w, ok := tab.fromJSON(text, &rep)
			rr := seatReport(&rep)
			if !ok || w == nil {
				rr.Malformed = true
				return rr
			}
			if seatRefuses(rr) {
				return rr
			}
			out := value(v, w)
			out.Unknown = rr.Unknown
			return out
		},
		Legacy:     seatLegacyObject,
		LoadLegacy: seatLoadLegacy,
		SaveWire: func(v *seatEnvironment) []byte {
			if w := table(v); w != nil {
				return tab.save(w)
			}
			return nil
		},
		SaveJSON: func(v *seatEnvironment) []byte {
			if w := table(v); w != nil {
				return tab.toJSON(w)
			}
			return nil
		},
	})
}

// seatEnvCodec is the one seat environment reader the app uses; its per-form
// counts are the evidence a later writer card reads (S4).
var seatEnvCodec = newSeatEnvironmentReader()

func (a *app) seatEnvironmentPath() (string, error) {
	if x := a.getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "nova-sprint", "seat-environment.json"), nil
	}
	h, err := a.home()
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(h) {
		return "", errors.New("seat environment has no absolute home directory")
	}
	return filepath.Join(h, ".config", "nova-sprint", "seat-environment.json"), nil
}

func (a *app) loadSeatEnvironment() error {
	path, err := a.seatEnvironmentPath()
	if err != nil {
		return nil // No usable optional configuration location; explicit settings still work.
	}
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !seatFileOwned(fi, os.Getuid()) {
		return errors.New("unsafe or unowned seat file")
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil || !seatDirectoryOwned(dir, os.Getuid()) {
		return errors.New("unsafe or unowned seat directory")
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(fi, opened) {
		return errors.New("seat file changed")
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return errors.New("seat file unreadable")
	}
	s, _, err := seatEnvCodec.Read(b)
	if err != nil {
		return err
	}
	if s.Version != 1 {
		return errors.New("unsupported seat version")
	}
	for k, v := range s.Environment {
		switch k {
		case "NOVA_SPRINT_REDIS", "NOVA_SPRINT_SERVER", "NOVA_BUS_REDIS":
		default:
			return errors.New("unsupported seat default")
		}
		if !seatEndpoint(v) {
			return errors.New("invalid seat endpoint")
		}
	}
	if s.Login != nil {
		if !seatEndpoint(s.Login.Redis) || s.Login.User == "" || s.Login.secrets().Missing() != "" {
			return errors.New("incomplete seat login")
		}
		for _, v := range []string{s.Login.User, s.Login.Store, s.Login.As, s.Login.Key, s.Login.Sops, s.Login.Secret} {
			if strings.ContainsAny(v, "\x00\r\n") {
				return errors.New("invalid seat reference")
			}
		}
	}
	if s.ForgeIdentity != "" {
		if !filepath.IsAbs(s.ForgeIdentity) {
			return errors.New("forge identity must be absolute")
		}
		info, err := os.Stat(s.ForgeIdentity)
		if err != nil || !info.IsDir() {
			return errors.New("forge identity directory missing")
		}
	}
	a.seatEnvironment = &s
	env := a.getenv
	a.getenv = func(k string) string {
		if v := env(k); v != "" {
			return v
		}
		if k == "NOVA_SPRINT_REDIS" {
			if v := env("NOVA_REDIS_ADDR"); v != "" {
				return v
			}
		}
		return s.Environment[k]
	}
	return nil
}

// Keep these rules identical to tools pkg/seatcred.ValidSeatEndpoint.
func seatEndpoint(value string) bool {
	if strings.HasPrefix(value, "mem:") || strings.Contains(value, "://") || strings.ContainsAny(value, "@/?#") {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '\'' || r == '"' || r == ' ' {
			return false
		}
	}
	host, port, err := net.SplitHostPort(value)
	if err != nil || host == "" {
		return false
	}
	n, err := strconv.ParseUint(port, 10, 16)
	return err == nil && n > 0
}

func safeSeatJSONError(err error) error {
	switch err.Error() {
	case "duplicate key", "trailing data", "invalid JSON":
		return err
	}
	return errors.New("seat data has malformed JSON")
}

// Walk tokens rather than unmarshalling a map, which silently accepts duplicate
// keys. Diagnostics deliberately never include input values.
func uniqueJSONKeys(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var value func() error
	value = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				k, ok := key.(string)
				if !ok || seen[k] {
					return errors.New("duplicate key")
				}
				seen[k] = true
				if err := value(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON")
		}
		_, err = d.Token()
		return err
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}

func seatEnvironmentNeeded(args []string) bool {
	if len(args) == 0 || args[0] == "help" || args[0] == "version" || args[0] == "--version" || verbflag.IsHelp(args[0]) {
		return false
	}
	v := readVerb(args)
	return v.words != 0 && v.err == nil && !v.help
}

func seatFileOwned(fi os.FileInfo, uid int) bool {
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

func seatJSONFields(b []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	for key, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("null field")
		}
		switch key {
		case "version", "environment", "forge_identity":
		case "login":
			var login map[string]json.RawMessage
			if err := json.Unmarshal(raw, &login); err != nil {
				return err
			}
			for k := range login {
				switch k {
				case "redis", "user", "store", "as", "key", "sops", "secret":
				default:
					return errors.New("unknown login field")
				}
			}
		default:
			return errors.New("unknown field")
		}
	}
	return nil
}

func seatDirectoryOwned(fi os.FileInfo, uid int) bool {
	if !fi.IsDir() || fi.Mode().Perm()&0022 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}
