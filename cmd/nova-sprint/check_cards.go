package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-sprint/internal/sprint/store"
	"github.com/mas-bandwidth/nova-sprint/pkg/oneline"
	"github.com/mas-bandwidth/nova-sprint/pkg/swarm"
)

// nova-sprint check cards: the core pre-start card check (v1.2.17,
// start-refuses-until-a-clean-check; the owner, 2026-10-13: "OK so all the card checks we do
// now, should be a new verb 'nova-sprint check'", and "if you start a sprint before checking,
// then it will fail and tell you to check."). It reads the epoch's work table and its streams,
// runs the core kinds (sprint.CheckCards), prints one line a finding, and writes the epoch's
// check record (store.PutCardCheck), which start reads (startCardCheckGate); --dry-run and
// --from <export> run the same check without writing a check record. The check verb beside it
// (cmdCheck) is the store's invariants and is unchanged. docs/SPEC-SPRINT.md section 14, "The
// card check"; tla/MachineReady.tla Check.
func init() {
	verbClasses["check cards"] = classCoordinator
}

// checkCardsOut is check cards' --json object.
type checkCardsOut struct {
	Clean    bool                 `json:"clean"`
	DryRun   bool                 `json:"dry_run,omitempty"`
	Cards    int                  `json:"cards"`
	Kinds    string               `json:"kinds,omitempty"`  // the refusing findings by kind
	Warned   string               `json:"warned,omitempty"` // the warnings by kind (v1.2.18): printed, failing nothing
	Streams  []string             `json:"streams,omitempty"`
	Findings []sprint.CardFinding `json:"findings"`
	Digest   string               `json:"digest"`
	Epoch    uint64               `json:"epoch"`
}

func (a *app) cmdCheckCards(args []string, stdout, stderr io.Writer) int {
	const name = "check cards"
	fs, c := a.verbSetup(name)
	var streams listFlag
	fs.Var(&streams, "stream", "check only this stream (again, or comma separated, for more); a check so limited writes a record start does not take: start wants every card checked")
	dry := fs.Bool("dry-run", false, "run the card check and print its findings without writing the epoch's check record")
	from := fs.String("from", "", "read the cards and streams from an export file or directory instead of the store; writes no check record")
	pos, err := parse(fs, args)
	if err != nil {
		return refuse(stderr, name, err.Error())
	}
	if len(pos) > 0 {
		return refuse(stderr, name, "takes no words, found "+pos[0])
	}
	ctx := context.Background()
	var (
		st      *store.Store
		snap    *sprint.Snapshot
		rs      ruleSet
		baseDir string
	)
	if *from != "" {
		var code int
		snap, baseDir, rs, code = a.loadCheckExport(ctx, name, *from, stderr)
		if code != 0 {
			return code
		}
	} else {
		if *dry {
			c.read = true
		}
		st, err = a.store(*c)
		if err != nil {
			return refuse(stderr, name, err.Error())
		}
		// the work table's cards and the records their needs name (a dropped card is kept, not
		// placed), and the merge table's control cards of the streams
		snap, err = st.Load(ctx, []string{sprint.Work, sprint.Merge}, checkNeedsExtras)
		if err != nil {
			return a.readFailed(name, err, stderr)
		}
		if err := st.RoutesInto(ctx, snap); err != nil {
			return a.readFailed(name, err, stderr)
		}
		var code int
		rs, code = a.briefRules(name, "", c, &st, stderr)
		if code != 0 {
			return code
		}
	}
	in := sprint.CardCheckIn{Streams: streams, MaxBriefBytes: store.MaxBriefBytes, Trees: map[sprint.CardTreeKey]sprint.AdmissionTree{},
		Lint: func(brief string) []string { return cardLint(brief, rs) }}
	for _, cb := range sprint.CardBasesToRead(snap, streams) {
		k := sprint.CardTreeKey{Repo: cb.Named, Base: cb.Ref}
		if baseDir != "" {
			if tree, ok := readExportTree(baseDir, cb); ok {
				in.Trees[k] = tree
				continue
			}
		}
		in.Trees[k] = a.cardTree(ctx, cb)
	}
	findings := sprint.CheckCards(snap, in)
	// only a refusing finding fails the check, and so start; a warning (v1.2.18: a file overlap
	// a stream's order or the waits already serialize) is printed and kept with the record's
	// findings, and fails neither
	refusing, warned := sprint.RefusingFindings(findings), sprint.CardCheckKinds(sprint.WarningFindings(findings))
	cards := sprint.CheckedCardsOf(snap)
	rec := store.CheckRecord{At: a.now().UTC(), By: c.actor, Clean: len(refusing) == 0, Streams: streams,
		Kinds: sprint.CardCheckKinds(refusing), Cards: cards, Digest: sprint.CardsDigest(cards), Epoch: snap.Epoch}
	for _, f := range findings {
		rec.Findings = append(rec.Findings, f.Line())
	}
	if *dry || *from != "" {
		if st != nil {
			rec.Epoch = st.PinnedEpoch()
		}
		a.outcome, a.changed = "done", "no"
	} else {
		// every run writes the record, a failed one over a clean one (tla/MachineReady.tla Check)
		if rec, err = st.PutCardCheck(ctx, rec); err != nil {
			return a.readFailed(name, err, stderr)
		}
		a.outcome, a.changed = "done", "yes"
	}
	if c.json {
		o := checkCardsOut{Clean: rec.Clean, DryRun: *dry || *from != "", Cards: len(cards), Kinds: rec.Kinds, Warned: warned, Streams: rec.Streams, Findings: findings, Digest: rec.Digest, Epoch: rec.Epoch}
		if o.Findings == nil {
			o.Findings = []sprint.CardFinding{}
		}
		b, _ := json.Marshal(o) // ignored: strings, ints and slices of them always encode
		fmt.Fprintln(stdout, string(b))
	} else {
		for _, f := range findings {
			fmt.Fprintln(stdout, oneline.Escape(f.Line()))
		}
		also := ""
		if warned != "" {
			also = " warned=" + warned
		}
		if rec.Clean {
			fmt.Fprintf(stdout, "CHECK CARDS OK cards=%d%s\n", len(cards), also)
		} else {
			fmt.Fprintf(stdout, "CHECK CARDS FAILED kinds=%s%s\n", rec.Kinds, also)
		}
	}
	if !rec.Clean {
		return 1
	}
	return 0
}

// loadCheckExport reads a *sprint.Snapshot from an export file or directory (--from <export>):
// a backup --file twin snapshot, a RESTORE dump, a card <id> --json or card --all --json JSONL
// file, or a directory holding one with optional briefs/<id>.md and trees/<repo>__<base>.txt.
func (a *app) loadCheckExport(ctx context.Context, verbName, fromPath string, stderr io.Writer) (*sprint.Snapshot, string, ruleSet, int) {
	info, err := os.Stat(fromPath)
	if err != nil {
		return nil, "", ruleSet{}, refuse(stderr, verbName, "--from: "+err.Error())
	}
	baseDir, filePath := filepath.Dir(fromPath), fromPath
	if info.IsDir() {
		baseDir, filePath = fromPath, ""
		for _, cand := range []string{"cards.jsonl", "live-cards.jsonl", "all.jsonl", "cards.json", "snapshot.json", "backup.json"} {
			p := filepath.Join(baseDir, cand)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				filePath = p
				break
			}
		}
		if filePath == "" {
			ents, _ := os.ReadDir(baseDir)
			for _, e := range ents {
				if e.IsDir() {
					continue
				}
				n := e.Name()
				if strings.HasSuffix(n, ".restore.txt") || strings.HasSuffix(n, ".jsonl") || strings.HasSuffix(n, ".json") || strings.HasSuffix(n, ".backup") {
					filePath = filepath.Join(baseDir, n)
					break
				}
			}
		}
		if filePath == "" {
			return nil, "", ruleSet{}, refuse(stderr, verbName, "--from: no export file found in "+fromPath)
		}
	}
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", ruleSet{}, refuse(stderr, verbName, "--from: "+err.Error())
	}
	rs := ruleSet{rules: swarm.DefaultChildRules}
	if keys, err := store.ParseRestoreDump(b); err == nil && len(keys) > 0 {
		m, err := store.MemRestore(keys)
		if err != nil {
			return nil, "", ruleSet{}, refuse(stderr, verbName, "--from: "+err.Error())
		}
		return a.loadExportMem(ctx, verbName, m, baseDir, rs, stderr)
	}
	m := store.NewMem()
	if err := m.Restore(b); err == nil {
		return a.loadExportMem(ctx, verbName, m, baseDir, rs, stderr)
	}
	snap, err := parseCardExport(b, baseDir, a.now().UTC())
	if err != nil {
		return nil, "", ruleSet{}, refuse(stderr, verbName, "--from: "+err.Error())
	}
	return snap, baseDir, rs, 0
}

func (a *app) loadExportMem(ctx context.Context, verbName string, m *store.Mem, baseDir string, rs ruleSet, stderr io.Writer) (*sprint.Snapshot, string, ruleSet, int) {
	st := &store.Store{B: m, Names: sprint.Names{}, Actor: "coordinator", Now: a.now, NewID: store.NewID}
	var err error
	if st, err = st.Pinned(ctx); err != nil {
		return nil, "", ruleSet{}, a.readFailed(verbName, err, stderr)
	}
	snap, err := st.Load(ctx, []string{sprint.Work, sprint.Merge}, checkNeedsExtras)
	if err != nil {
		return nil, "", ruleSet{}, a.readFailed(verbName, err, stderr)
	}
	if err := st.RoutesInto(ctx, snap); err != nil {
		return nil, "", ruleSet{}, a.readFailed(verbName, err, stderr)
	}
	if len(snap.Providers) == 0 {
		snap.Providers = map[string]sprint.Provider{"export": {Name: "export", Billing: "plan", Funded: false}}
	}
	if path, err := st.RulesPath(ctx); err == nil && path != "" {
		if rules, err := swarm.ReadChildRules(path); err == nil {
			rs = ruleSet{rules: rules}
		}
	}
	return snap, baseDir, rs, 0
}

type exportEntry struct {
	Primary   *sprint.Card               `json:"primary"`
	Work      []*sprint.Card             `json:"work_cards"`
	Merge     *sprint.Card               `json:"merge"`
	ID        string                     `json:"id"`
	IDCap     string                     `json:"ID"`
	Stream    string                     `json:"stream"`
	Row       string                     `json:"Row"`
	Column    string                     `json:"column"`
	Col       string                     `json:"Col"`
	State     string                     `json:"state"`
	Score     float64                    `json:"score"`
	ScoreCap  float64                    `json:"Score"`
	Needs     json.RawMessage            `json:"needs"`
	Brief     string                     `json:"brief"`
	Fields    map[string]string          `json:"fields"`
	FieldsCap map[string]string          `json:"Fields"`
	Cards     []exportEntry              `json:"cards"`
	Rows      []exportEntry              `json:"rows"`
	Providers map[string]sprint.Provider `json:"providers"`
}

func parseExportNeeds(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var ss []string
	if err := json.Unmarshal(raw, &ss); err == nil {
		return ss
	}
	var ns []sprint.NeedState
	if err := json.Unmarshal(raw, &ns); err == nil {
		out := make([]string, 0, len(ns))
		for _, n := range ns {
			if n.ID != "" {
				out = append(out, n.ID)
			}
		}
		return out
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return sprint.Split(s)
	}
	return nil
}

func parseCardExport(b []byte, baseDir string, now time.Time) (*sprint.Snapshot, error) {
	var entries []exportEntry
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); ok && d == '[' {
		for dec.More() {
			var e exportEntry
			if err := dec.Decode(&e); err != nil {
				return nil, err
			}
			entries = append(entries, e)
		}
	} else {
		dec = json.NewDecoder(bytes.NewReader(b))
		for {
			var e exportEntry
			if err := dec.Decode(&e); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				return nil, err
			}
			entries = append(entries, e)
		}
	}
	var workCards, mergeCards []*sprint.Card
	var providers map[string]sprint.Provider
	var collect func(es []exportEntry)
	collect = func(es []exportEntry) {
		for _, e := range es {
			if len(e.Providers) > 0 && providers == nil {
				providers = e.Providers
			}
			if len(e.Cards) > 0 {
				collect(e.Cards)
			}
			if len(e.Rows) > 0 {
				collect(e.Rows)
			}
			if e.Primary != nil && e.Primary.ID != "" {
				p := *e.Primary
				p.Fields = maps.Clone(e.Primary.Fields)
				if p.Fields == nil {
					p.Fields = map[string]string{}
				}
				if p.Col == "" {
					p.Col = e.Column
				}
				if p.Row == "" {
					p.Row = p.Fields["stream"]
				}
				fillExportBrief(&p, baseDir)
				workCards = append(workCards, &p)
				for _, w := range e.Work {
					if w != nil && w.ID != "" {
						wc := *w
						wc.Fields = maps.Clone(w.Fields)
						workCards = append(workCards, &wc)
					}
				}
				if e.Merge != nil && e.Merge.ID != "" {
					mc := *e.Merge
					mc.Fields = maps.Clone(e.Merge.Fields)
					mergeCards = append(mergeCards, &mc)
				}
				continue
			}
			id := e.ID
			if id == "" {
				id = e.IDCap
			}
			if id == "" {
				continue
			}
			row := e.Stream
			if row == "" {
				row = e.Row
			}
			col := e.Column
			if col == "" {
				col = e.Col
			}
			if col == "" {
				col = e.State
			}
			score := e.Score
			if score == 0 && e.ScoreCap != 0 {
				score = e.ScoreCap
			}
			fields := maps.Clone(e.Fields)
			if len(fields) == 0 && len(e.FieldsCap) > 0 {
				fields = maps.Clone(e.FieldsCap)
			}
			if fields == nil {
				fields = map[string]string{}
			}
			if row == "" {
				row = fields["stream"]
			}
			if fields["stream"] == "" && row != "" {
				fields["stream"] = row
			}
			if fields["needs"] == "" {
				if ns := parseExportNeeds(e.Needs); len(ns) > 0 {
					fields["needs"] = strings.Join(ns, ",")
				}
			}
			if fields["brief"] == "" && e.Brief != "" {
				fields["brief"] = e.Brief
			}
			if fields["kind"] == "" {
				fields["kind"] = "primary"
			}
			c := &sprint.Card{ID: id, Row: row, Col: col, Score: score, Fields: fields}
			if col == sprint.Ctl || strings.HasPrefix(id, "ctl:") || fields["kind"] == "stream" {
				if c.Col == "" {
					c.Col = sprint.Ctl
				}
				mergeCards = append(mergeCards, c)
			} else {
				if c.Col == "" {
					c.Col = sprint.Ready
				}
				fillExportBrief(c, baseDir)
				workCards = append(workCards, c)
			}
		}
	}
	collect(entries)
	if len(workCards) == 0 && len(mergeCards) == 0 {
		return nil, errors.New("no cards found in export")
	}
	snap := &sprint.Snapshot{Now: now, Work: sprint.NewTable(sprint.Work), Readers: sprint.NewTable(sprint.Readers),
		Merge: sprint.NewTable(sprint.Merge), Fleet: sprint.NewTable(sprint.Fleet),
		Coordinator: "coordinator", Actor: "coordinator", Providers: providers}
	if len(snap.Providers) == 0 {
		snap.Providers = map[string]sprint.Provider{"export": {Name: "export", Billing: "plan", Funded: false}}
	}
	rowSet := map[string]bool{}
	for _, c := range workCards {
		if c.Row != "" {
			rowSet[c.Row] = true
		}
	}
	for _, c := range mergeCards {
		if c.Row != "" {
			rowSet[c.Row] = true
		}
	}
	rows := slices.Sorted(maps.Keys(rowSet))
	snap.Work.SetRows(rows)
	snap.Merge.SetRows(rows)
	for _, c := range workCards {
		snap.Work.Put(c)
	}
	for _, c := range mergeCards {
		snap.Merge.Put(c)
	}
	for _, r := range rows {
		if snap.Merge.Card(sprint.CtlID(r)) != nil {
			continue
		}
		branch := "sprint/" + r
		for _, c := range snap.Work.Cards() {
			if c.Row != r || !c.Placed() || c.Col == sprint.Landed {
				continue
			}
			if cb := swarm.ReadCardBase([]byte(c.F("brief"))); cb.Ref != "" {
				branch = cb.Ref
				break
			}
		}
		snap.Merge.Put(&sprint.Card{ID: sprint.CtlID(r), Row: r, Col: sprint.Ctl, Fields: map[string]string{
			"kind":                   "stream",
			sprint.FieldDisplay:      "Stream " + strings.ReplaceAll(r, "-", " "),
			sprint.FieldStreamBranch: branch,
		}})
	}
	return snap, nil
}

func fillExportBrief(c *sprint.Card, baseDir string) {
	if c.F("brief") != "" || baseDir == "" || sprint.IsSentinel(c) {
		return
	}
	for _, p := range []string{filepath.Join(baseDir, "briefs", c.ID+".md"), filepath.Join(baseDir, c.ID+".md")} {
		if b, err := os.ReadFile(p); err == nil {
			c.Fields["brief"] = strings.TrimSuffix(string(b), "\n")
			return
		}
	}
}

func readExportTree(baseDir string, cb swarm.CardBase) (sprint.AdmissionTree, bool) {
	slug := strings.ReplaceAll(cb.Named+"__"+cb.Ref, "/", "__") + ".txt"
	for _, p := range []string{filepath.Join(baseDir, "trees", slug), filepath.Join(baseDir, slug)} {
		if b, err := os.ReadFile(p); err == nil {
			return sprint.AdmissionTree{Files: strings.Fields(string(b))}, true
		}
	}
	return sprint.AdmissionTree{}, false
}

// checkNeedsExtras loads the record of every need the placed cards name, placed or kept: a need
// naming a dropped card is told from one naming no card.
func checkNeedsExtras(s *sprint.Snapshot) map[string][]string {
	var ids []string
	for _, c := range s.Work.Cards() {
		ids = append(ids, sprint.Split(c.F("needs"))...)
	}
	return map[string][]string{sprint.Work: ids}
}

// cardLint is the card lint brief check runs over one stored brief (lintBriefReads): its
// model-line why, or each lint finding's check and line, none when it passes. A server's
// rule set (rs.server: the server lints) is linted with the built-in rules here, as the
// server would with the sprint's.
func cardLint(brief string, rs ruleSet) []string {
	if rs.server {
		rs = ruleSet{rules: swarm.DefaultChildRules}
	}
	why, findings := lintBriefReads(brief, rs)
	if why != "" {
		return []string{modelLinesWhy(why)}
	}
	var out []string
	for _, f := range findings {
		out = append(out, fmt.Sprintf("%s (line %d)", f.Check, f.Line))
	}
	return out
}

// cardTree is the base tree of a (repository, base) for kind 3: the test's when it hands one
// (checkTree), else the lander's clone at the BASE tip, as add's PATHS admission reads it.
func (a *app) cardTree(ctx context.Context, cb swarm.CardBase) sprint.AdmissionTree {
	if a.checkTree != nil {
		return a.checkTree(ctx, cb)
	}
	return a.admissionTree(ctx, &lander{a: a}, cb)
}

// startCardCheckGate is start's card check gate (start-refuses-until-a-clean-check): from READY
// or STOPPED, before the machine changes, it reads the epoch's check record and the cards now,
// and refuses, exit 1, nothing written, with no record, a failed one, one a --stream check
// limited, or a clean one whose digest is not the cards' now (recomputed from the live table, so
// any admit, move, brief, needs, priority or tier change voids it with no hook in those verbs).
// A start of a RUNNING machine changes nothing and is not gated. No flag, variable or setting
// skips it: newApp sets cardCheckGate (docs/SPEC-SPRINT.md section 14, "The card check"). The
// one store it does not gate is a mem twin (st.ByHand: --redis mem:<file>, for learning and
// tests, never a fleet's), so the help's walkthrough and the first-run transcript run as
// written; check cards runs on a twin as anywhere.
//
// tla/MachineReady.tla Start, StartRefusedCheck: StartOnlyChecked
func (a *app) startCardCheckGate(ctx context.Context, st *store.Store, stderr io.Writer) int {
	if !a.cardCheckGate || st.ByHand {
		return 0
	}
	m, _, err := st.Machine(ctx)
	if err != nil {
		return a.readFailed("start", err, stderr)
	}
	if m.Running() {
		return 0
	}
	rec, ok, err := st.CardCheck(ctx)
	if err != nil {
		return a.readFailed("start", err, stderr)
	}
	why := ""
	switch {
	case !ok:
		why = fmt.Sprintf("run nova-sprint check cards first: no check record for epoch %d", st.PinnedEpoch())
	case !rec.Clean:
		why = "run nova-sprint check cards: the last check found " + rec.Kinds
	case len(rec.Streams) > 0:
		why = "run nova-sprint check cards: the last check covered stream(s) " + strings.Join(rec.Streams, ",") + " only"
	default:
		snap, err := st.Load(ctx, []string{sprint.Work}, nil)
		if err != nil {
			return a.readFailed("start", err, stderr)
		}
		now := sprint.CheckedCardsOf(snap)
		if sprint.CardsDigest(now) != rec.Digest {
			why = "run nova-sprint check cards: the cards changed since the check (" + sprint.CardsChange(rec.Cards, now) + ")"
		}
		if why == "" {
			if err := st.RoutesInto(ctx, snap); err != nil {
				return a.readFailed("start", err, stderr)
			}
			findings := sprint.ProviderFindings(snap)
			if len(findings) > 0 {
				for _, f := range findings {
					fmt.Fprintf(stderr, "%s start: run nova-sprint check cards: %s\n", prog, oneline.Escape(f.Line()))
				}
				fmt.Fprintf(stderr, "%s start: %d provider finding(s): the machine is not started and nothing was written\n", prog, len(findings))
				return 1
			}
		}
	}
	if why == "" {
		return 0
	}
	fmt.Fprintf(stderr, "%s start: %s; the machine is not started and nothing was written\n", prog, oneline.Escape(why))
	return 1
}
