package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardhdr"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/log"
	"github.com/mas-bandwidth/nova-sprint/pkg/ntable"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
)

func init() {
	// backup is the schedule of the store's host, like snapshot: it needs no
	// actor and reads and writes no card (the class test, coordinator.go).
	verbClasses["backup"] = classMachine
	// a Redis store's RDB is read from the disk its own host writes it to
	notServed = append(notServed, "backup")
}

// cmdBackup is the sprint backup as one verb: the store is written to --file,
// the file is read back and restored into a twin, the twin is compared with the
// store, and the file is scanned for secrets (SPEC-SPRINT, sprint-backup-verb).
// It uses no clock and opens no socket of its own: on a twin store it runs
// whole in memory and on the file system.
func (a *app) cmdBackup(args []string, stdout, stderr io.Writer) int {
	fs, c := a.verbSetup("backup")
	file := fs.String("file", "", "the file the backup is written to (it must not exist); or --out")
	dry := fs.Bool("dry-run", false, "verify the backup in memory without writing --file; writes nothing")
	outDir := fs.String("out", "", "the directory the RESTORE dump's parts, SHA256SUMS and README.md are written to (it must not exist or be empty); or --file")
	partBytes := fs.Int64("part-bytes", backupPartBytes, "the largest part of the xz with --out, in bytes (under 100 MB)")
	novaSecrets := fs.String("nova-secrets", "nova-secrets", "the nova-secrets program the --out scan runs under")
	secStore := fs.String("secrets-store", "", "the nova-secrets store the --out scan reads (default: the seat login's)")
	secAs := fs.String("secrets-as", "", "the nova-secrets seat the --out scan reads (default: the seat login's)")
	secKey := fs.String("secrets-key", "", "the seat's age key file (default: the seat login's)")
	sops := fs.String("sops", "", "the sops program nova-secrets exec runs (default: the seat login's)")
	xz := fs.String("xz", "xz", "the xz program --out compresses with")
	split := fs.String("split", "split", "the split program --out splits with")
	redisServer := fs.String("redis-server", "redis-server", "the redis-server --out restores a Redis store's dump into, a throwaway on a unix socket")
	scan := fs.String("scan", "", "the child nova-secrets exec runs: count the values of these variables found on stdin (backup --out runs it)")
	pos, err := parse(fs, args)
	if err != nil || len(pos) > 0 {
		return refuse(stderr, "backup", argErr("takes no words ", err, pos...))
	}
	if *scan != "" {
		if err := backupScan(strings.Split(*scan, ","), a.getenv, os.Stdin, stdout); err != nil {
			return backupFailed(stderr, "the scan could not read its stream: "+err.Error())
		}
		return 0
	}
	if *outDir != "" {
		if *file != "" || *dry {
			return refuse(stderr, "backup", "--out takes no --file or --dry-run; run: nova-sprint backup --out <dir>")
		}
		if *partBytes < 1 || *partBytes >= 100_000_000 {
			return refuse(stderr, "backup", "--part-bytes is a size above zero and under 100 MB; run: nova-sprint backup --out <dir>")
		}
		sc := backupScanner{bin: *novaSecrets, store: *secStore, as: *secAs, key: *secKey, sops: *sops}
		if l, ok, err := a.recordedLogin(); err == nil && ok {
			sc.store, sc.as, sc.key, sc.sops = cmp.Or(sc.store, l.Store), cmp.Or(sc.as, l.As), cmp.Or(sc.key, l.Key), cmp.Or(sc.sops, l.Sops)
		}
		exe := os.Executable
		if a.executable != nil {
			exe = a.executable
		}
		if sc.self, err = exe(); err != nil {
			return backupFailed(stderr, "this binary has no path for the scan to run: "+err.Error())
		}
		return a.backupOut(c, &backupOut{out: *outDir, partBytes: *partBytes, xz: *xz, split: *split, scan: sc}, *redisServer, stdout, stderr)
	}
	if *file == "" {
		return refuse(stderr, "backup", "wants --out <dir> or --file <path>, one that does not exist yet; run: nova-sprint backup --out sprint-backup")
	}
	if _, err := os.Lstat(*file); err == nil {
		return backupFailed(stderr, *file+" exists and is never overwritten; run: nova-sprint backup --file <a path that does not exist>")
	}
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "backup", err.Error()) // a store that did not answer: exit 2, as every verb's
	}
	var src store.SnapshotSource
	var twin store.SnapshotTwin = store.RDBTwin{}
	switch b := st.B.(type) {
	case *store.Redis:
		src = &redisSource{b: b}
	case *store.Mem:
		src, twin = store.MemSource{M: b, Names: st.Names}, store.MemTwin{Names: st.Names}
	default:
		return backupFailed(stderr, "this store has no backup; run: nova-sprint backup --redis <a Redis address> --file "+*file)
	}
	var out strings.Builder
	if err := runBackup(context.Background(), src, twin, *file, *dry, &out); err != nil {
		return backupFailed(stderr, err.Error())
	}
	if c.json {
		m := map[string]any{"file": *file, "line": strings.TrimSpace(out.String())}
		if *dry {
			m["dry_run"] = true
		}
		b, _ := json.Marshal(m)
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	fmt.Fprint(stdout, out.String())
	return 0
}

// backupOut opens the store and runs backup --out on it: a twin store
// restores into a throwaway twin, a Redis store into a throwaway redis-server.
func (a *app) backupOut(c *common, b *backupOut, redisServer string, stdout, stderr io.Writer) int {
	st, err := a.store(*c)
	if err != nil {
		return refuse(stderr, "backup", err.Error())
	}
	switch be := st.B.(type) {
	case *store.Mem:
		b.src = memBackup{st: st}
		b.twin = func(context.Context, string) (backupTwin, error) { return &memTwin{names: st.Names}, nil }
		b.load = "-h <host> -p <port>"
	case *store.Redis:
		b.src = redisBackup{b: be, names: st.Names}
		b.twin = func(ctx context.Context, work string) (backupTwin, error) {
			return startServerTwin(ctx, redisServer, work, st.Names)
		}
		b.load = "-h <host> -p <port>"
	default:
		return backupFailed(stderr, "this store has no backup; run: nova-sprint backup --redis <a Redis address> --out "+b.out)
	}
	res, err := b.run(context.Background())
	if err != nil {
		return backupFailed(stderr, err.Error())
	}
	for _, f := range res.files {
		fmt.Fprintln(stdout, f)
	}
	fmt.Fprintln(stdout, res.line)
	return 0
}

// backupFailed is a backup that did not happen: exit 1, the verb's own words
// (usage and a store that did not answer are exit 2).
func backupFailed(stderr io.Writer, what string) int {
	fmt.Fprintf(stderr, "%s backup FAILED: %s\n", prog, oneline.Escape(what))
	return 1
}

// runBackup is the verb's law (the store-snapshot model's checks, over one
// file): write, read back against the checksum, restore into the twin and
// compare counts and, on a twin store, the restored store's sprint state with
// the store's part for part (store.SemanticRestore) and its own document, scan
// for secrets. A file that fails any step is removed, so the path holds only a
// verified, secret-free backup. The one line it prints says what was proved:
// restore=semantic, or restore=integrity (the RDB's header, version and
// checksum alone), which is never reported as a semantic restore.
func runBackup(ctx context.Context, src store.SnapshotSource, twin store.SnapshotTwin, path string, dry bool, out io.Writer) error {
	rdb, live, err := src.Save(ctx)
	if err != nil {
		return fmt.Errorf("the store gave no backup: %w; run: nova-sprint backup --file "+path, err)
	}
	level := store.RestoreLevel(src, twin)
	var state store.SprintState
	if level == store.RestoreSemantic {
		if state, err = src.(store.StateSource).State(ctx); err != nil {
			return fmt.Errorf("the store's sprint state at the backup cannot be read: %v; nothing was written; run: nova-sprint backup --file %s", err, path)
		}
	}
	sum := sha256.Sum256(rdb)
	if dry {
		got, compared, err := verifyBackup(ctx, twin, level, live, state, rdb)
		if err != nil {
			return fmt.Errorf("the backup %s %v", path, err)
		}
		if lines := secretLines(rdb); len(lines) > 0 {
			return fmt.Errorf("the backup %s holds secret-shaped text on line %s (the value is not shown); find the card or key that holds it and remove it; run: nova-sprint backup --file %s", path, joinInts(lines), path)
		}
		fmt.Fprintf(out, "BACKUP DRY-RUN file=%s sha256=%s bytes=%d %s restore=%s compared=%s secrets=none%s; nothing was written\n", path, hex.EncodeToString(sum[:]), len(rdb), countsText(got), level, compared, integrityOnly(level))
		return nil
	}
	if err := writeAtomic(path, rdb); err != nil {
		return fmt.Errorf("%s cannot be written: %v; run: nova-sprint backup --file <a writable path>", path, err)
	}
	fail := func(err error) error {
		_ = os.Remove(path) // ignored: removing the failed backup; the error that failed it is what is returned
		return err
	}
	copied, err := os.ReadFile(path)
	if err != nil {
		return fail(fmt.Errorf("the backup %s cannot be read back: %v; it was removed", path, err))
	}
	if sha256.Sum256(copied) != sum {
		return fail(fmt.Errorf("the backup %s does not match its checksum on reading it back; it was removed", path))
	}
	got, compared, err := verifyBackup(ctx, twin, level, live, state, copied)
	if err != nil {
		return fail(fmt.Errorf("the backup %s %v; it was removed", path, err))
	}
	if lines := secretLines(copied); len(lines) > 0 {
		return fail(fmt.Errorf("the backup %s holds secret-shaped text on line %s (the value is not shown); it was removed; find the card or key that holds it and remove it; run: nova-sprint backup --file %s", path, joinInts(lines), path))
	}
	fmt.Fprintf(out, "BACKUP OK file=%s sha256=%s bytes=%d %s restore=%s compared=%s secrets=none%s\n", path, hex.EncodeToString(sum[:]), len(rdb), countsText(got), level, compared, integrityOnly(level))
	return nil
}

// verifyBackup restores the bytes into the twin and compares them with the
// store's: the counts, and at the semantic level the sprint state and the
// document. The state check is the live one (store.SemanticRestore): the dump
// and the state are two reads of a store a running fleet keeps writing to, so
// a member's beat between them is not a difference, while a held status, a
// missing row or a changed card is (docs/SPEC-SPRINT.md, sprint-backup-verb).
// It returns the counts and what was compared, or the step that failed, worded
// to follow "the backup <path>".
func verifyBackup(ctx context.Context, twin store.SnapshotTwin, level string, live store.SnapshotCounts, state store.SprintState, b []byte) (store.SnapshotCounts, string, error) {
	got, err := twin.Load(b)
	if err != nil {
		return got, "", fmt.Errorf("does not restore into a twin: %v", err)
	}
	if why := backupCountsDiffer(live, got); why != "" {
		return got, "", fmt.Errorf("restored with other counts than the store held (%s)", why)
	}
	if level != store.RestoreSemantic {
		return got, "header+checksum", nil
	}
	if err := store.SemanticRestore(ctx, state, twin.(store.StateTwin), b); err != nil {
		return got, "", fmt.Errorf("does not restore the sprint the store held: %v", err)
	}
	if _, ok := twin.(store.MemTwin); ok {
		// a document is restored into a store and written again; that document
		// restored and written once more is the same one, so the restore
		// loses nothing the document holds (a first write may differ from a
		// store's own, which writes an absent field as null)
		once, err := backupRoundTrip(b)
		if err == nil {
			var twice []byte
			twice, err = backupRoundTrip(once)
			if err == nil && string(twice) != string(once) {
				err = errors.New("the restored store writes a different document when restored again")
			}
		}
		if err != nil {
			return got, "", fmt.Errorf("does not restore into a twin that holds what it holds: %v", err)
		}
		return got, "state+document+counts", nil
	}
	return got, "state+counts", nil
}

// integrityOnly is what a success line says after it when the restore was
// proved at the integrity level: that it is not a semantic restore.
func integrityOnly(level string) string {
	if level == store.RestoreSemantic {
		return ""
	}
	return "; integrity only, not a semantic restore: the sprint's state was not loaded and compared"
}

// backupRoundTrip restores a document into a fresh store and writes it again.
func backupRoundTrip(doc []byte) ([]byte, error) {
	m := store.NewMem()
	if err := m.Restore(doc); err != nil {
		return nil, err
	}
	return m.Snapshot()
}

// backupCountsDiffer says where the restored counts differ from the store's;
// a count of -1 is unknown and not compared.
func backupCountsDiffer(live, got store.SnapshotCounts) string {
	switch {
	case live.Keys >= 0 && got.Keys >= 0 && live.Keys != got.Keys:
		return fmt.Sprintf("keys %d, restored %d", live.Keys, got.Keys)
	case live.Cards >= 0 && got.Cards >= 0 && live.Cards != got.Cards:
		return fmt.Sprintf("cards %d, restored %d", live.Cards, got.Cards)
	}
	return ""
}

// secretLines are the 1-based lines of the file the log package's redaction
// would change: the same rules that keep a secret out of a log keep it out of
// a backup. At most twenty are named.
func secretLines(b []byte) []int {
	var hits []int
	for i, l := range strings.Split(string(backupScanBytes(b)), "\n") {
		if log.Redact(l) != l {
			if hits = append(hits, i+1); len(hits) == 20 {
				break
			}
		}
	}
	return hits
}

func joinInts(n []int) string {
	s := make([]string, len(n))
	for i, v := range n {
		s[i] = fmt.Sprint(v)
	}
	return strings.Join(s, ",")
}

// backupSpan identifies bytes in the original archive, never a rewritten dump.
type backupSpan struct{ start, end int }
type backupString struct {
	path  []string
	value string
	span  backupSpan
}

// backupJSON reads complete JSON and rejects duplicate decoded keys. Offsets
// preserve every original byte, including unknown fields and nested escapes.
func backupJSON(b []byte) (any, []backupString, error) {
	if !utf8.Valid(b) {
		return nil, nil, errors.New("invalid JSON UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var ss []backupString
	var value func([]string, int) (any, error)
	value = func(path []string, depth int) (any, error) {
		if depth > 128 {
			return nil, errors.New("JSON nesting")
		}
		begin := int(d.InputOffset())
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				m := map[string]any{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return nil, err
					}
					k, ok := key.(string)
					if !ok {
						return nil, errors.New("JSON key")
					}
					if _, duplicate := m[k]; duplicate {
						return nil, errors.New("duplicate JSON key")
					}
					x, err := value(append(append([]string(nil), path...), k), depth+1)
					if err != nil {
						return nil, err
					}
					m[k] = x
				}
				end, err := d.Token()
				if err != nil || end != json.Delim('}') {
					return nil, errors.New("JSON object")
				}
				return m, nil
			case '[':
				var a []any
				for d.More() {
					x, err := value(append(append([]string(nil), path...), fmt.Sprint(len(a))), depth+1)
					if err != nil {
						return nil, err
					}
					a = append(a, x)
				}
				end, err := d.Token()
				if err != nil || end != json.Delim(']') {
					return nil, errors.New("JSON array")
				}
				return a, nil
			}
			return nil, errors.New("JSON delimiter")
		case string:
			end := int(d.InputOffset())
			q := bytes.IndexByte(b[begin:end], '"')
			if q < 0 {
				return nil, errors.New("JSON string")
			}
			ss = append(ss, backupString{path, v, backupSpan{begin + q, end}})
		}
		return tok, nil
	}
	root, err := value(nil, 0)
	if err != nil {
		return nil, nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, nil, errors.New("trailing JSON")
	}
	return root, ss, nil
}

func backupAt(root any, path []string) any {
	for _, p := range path {
		switch x := root.(type) {
		case map[string]any:
			root = x[p]
		case []any:
			var n int
			if _, err := fmt.Sscan(p, &n); err != nil || n < 0 || n >= len(x) {
				return nil
			}
			root = x[n]
		default:
			return nil
		}
	}
	return root
}

// backupBudget is the one unambiguous integer budget in the actual parsed pin
// header. Task prose, invalid pins and duplicate tokens headers are not budgets.
func backupBudget(brief string) (backupSpan, bool) {
	m, why := cardhdr.ReadModel(brief)
	if why != "" || m.Pin == "" || m.Tokens == "" {
		return backupSpan{}, false
	}
	for _, c := range m.Tokens {
		if c < '0' || c > '9' {
			return backupSpan{}, false
		}
	}
	offset := strings.IndexByte(brief, '\n') + 1
	if offset == 0 {
		return backupSpan{}, false
	}
	var span backupSpan
	count := 0
	for offset < len(brief) {
		line, _, more := strings.Cut(brief[offset:], "\n")
		key, val, ok := cardhdr.KeyValue(line)
		if !ok {
			break
		}
		if strings.EqualFold(key, "tokens") {
			count++
			if val != m.Tokens {
				return backupSpan{}, false
			}
			start := offset + strings.IndexByte(line, ':') + 1
			for start < len(brief) && (brief[start] == ' ' || brief[start] == '\t') {
				start++
			}
			if start+len(val) > len(brief) || brief[start:start+len(val)] != val {
				return backupSpan{}, false // unsupported separators stay strictly scanned
			}
			span = backupSpan{start, start + len(val)}
		}
		offset += len(line) + 1
		if !more {
			break
		}
	}
	return span, count == 1
}

// backupStringMap maps each decoded byte to its original JSON byte span. A
// nested manifest uses this again, composing offsets without reserializing it.
func backupStringMap(b []byte, s backupString) ([]backupSpan, bool) {
	var spans []backupSpan
	for i := s.span.start + 1; i < s.span.end-1; {
		end := i + 1
		if b[i] == '\\' {
			if end >= s.span.end-1 {
				return nil, false
			}
			end++
			if b[i+1] == 'u' {
				end = i + 6
				if end > s.span.end-1 {
					return nil, false
				}
				if end+6 <= s.span.end-1 && b[end] == '\\' && b[end+1] == 'u' {
					var one string
					if json.Unmarshal(append(append([]byte{'"'}, b[i:end]...), '"'), &one) != nil {
						return nil, false
					}
					if one == "\ufffd" {
						end += 6
					}
				}
			}
			var decoded string
			quoted := append(append([]byte{'"'}, b[i:end]...), '"')
			if json.Unmarshal(quoted, &decoded) != nil {
				return nil, false
			}
			for range []byte(decoded) {
				spans = append(spans, backupSpan{i, end})
			}
		} else {
			_, n := utf8.DecodeRune(b[i : s.span.end-1])
			if n == 0 {
				return nil, false
			}
			end = i + n
			for j := i; j < end; j++ {
				spans = append(spans, backupSpan{j, j + 1})
			}
		}
		i = end
	}
	return spans, len(spans) == len(s.value)
}

// backupScanBytes masks only the proven budget value spans for the scanner.
// The archive is unchanged. Invalid/ambiguous JSON is scanned strictly whole.
func backupScanBytes(b []byte) []byte {
	root, ss, err := backupJSON(b)
	if err != nil || backupAt(root, []string{"version"}) != json.Number("1") {
		return b
	}
	_, tablesOK := backupAt(root, []string{"tables"}).(map[string]any)
	if !tablesOK {
		return b
	}
	if logs := backupAt(root, []string{"logs"}); logs != nil {
		if _, ok := logs.(map[string]any); !ok {
			return b
		}
	}
	work := func(name string) bool {
		return (name == "work" || strings.HasSuffix(name, "-work")) && backupAt(root, []string{"tables", name, "def", "Name"}) == name
	}
	var spans []backupSpan
	add := func(doc []byte, s backupString, outer []backupSpan) bool {
		budget, ok := backupBudget(s.value)
		if !ok {
			return true
		}
		mapping, ok := backupStringMap(doc, s)
		if !ok || budget.end > len(mapping) {
			return false
		}
		r := backupSpan{mapping[budget.start].start, mapping[budget.end-1].end}
		if outer != nil {
			if r.end > len(outer) {
				return false
			}
			r = backupSpan{outer[r.start].start, outer[r.end-1].end}
		}
		spans = append(spans, r)
		return true
	}
	for _, s := range ss {
		p := s.path
		switch {
		case len(p) == 6 && p[0] == "tables" && work(p[1]) && p[2] == "members" && p[4] == "fields" && p[5] == "brief":
			if _, ok := backupAt(root, p[:3]).(map[string]any); !ok {
				return b
			}
			if _, ok := backupAt(root, p[:4]).(map[string]any); !ok {
				return b
			}
			if _, ok := backupAt(root, p[:5]).(map[string]any); !ok {
				return b
			}
			if backupAt(root, p[:5]) == nil || backupAt(root, append(append([]string(nil), p[:5]...), "kind")) != "primary" {
				continue
			}
			if !add(b, s, nil) {
				return b
			}
		case len(p) == 5 && p[0] == "tables" && work(p[1]) && p[2] == "ops" && p[4] == "body":
			if _, ok := backupAt(root, p[:3]).(map[string]any); !ok {
				return b
			}
			if _, ok := backupAt(root, p[:4]).(map[string]any); !ok {
				return b
			}
			manifest, err := ntable.ValidateBatchManifestRaw([]byte(s.value))
			if err != nil || manifest.Table != p[1] {
				return b
			}
			inner, stringsIn, err := backupJSON([]byte(s.value))
			if err != nil {
				return b
			}
			if backupAt(inner, []string{"table"}) != p[1] {
				continue
			}
			if _, ok := inner.(map[string]any); !ok {
				return b
			}
			if _, ok := backupAt(inner, []string{"members"}).([]any); !ok {
				return b
			}
			outer, ok := backupStringMap(b, s)
			if !ok {
				return b
			}
			for _, v := range stringsIn {
				q := v.path
				if len(q) == 4 && q[0] == "members" && q[2] == "set" && q[3] == "brief" && backupAt(inner, []string{"members", q[1], "set", "kind"}) == "primary" {
					if _, ok := backupAt(inner, q[:2]).(map[string]any); !ok {
						return b
					}
					if _, ok := backupAt(inner, q[:3]).(map[string]any); !ok {
						return b
					}
					if !add([]byte(s.value), v, outer) {
						return b
					}
				}
			}
		case len(p) == 7 && p[0] == "logs" && p[2] == "lines" && p[4] == "line" && p[5] == "text" && p[6] == "brief":
			if _, ok := backupAt(root, p[:2]).(map[string]any); !ok {
				return b
			}
			if _, ok := backupAt(root, p[:3]).([]any); !ok {
				return b
			}
			for _, end := range []int{4, 5, 6} {
				if _, ok := backupAt(root, p[:end]).(map[string]any); !ok {
					return b
				}
			}
			if backupAt(root, append(append([]string(nil), p[:5]...), "table")) != "work" || backupAt(root, append(append([]string(nil), p[:5]...), "kind")) != "move" {
				continue
			}
			if !add(b, s, nil) {
				return b
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	var out bytes.Buffer
	at := 0
	for _, r := range spans {
		if r.start < at || r.end > len(b) {
			return b
		}
		out.Write(b[at:r.start])
		out.WriteString(log.Redacted + " ")
		at = r.end
	}
	out.Write(b[at:])
	return out.Bytes()
}
