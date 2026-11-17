package policy

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mas-bandwidth/nova-sprint/modules/card"
	"github.com/mas-bandwidth/nova-sprint/modules/roster"
	"github.com/mas-bandwidth/nova-sprint/modules/store"
)

var (
	emptyTuple           = starlark.Tuple{}
	emptyResourcesDict   = makeFrozenIntDict(nil)
	defaultAmounts       = map[string]int{"cpu": 1, "memory": 1}
	defaultResourcesDict = makeFrozenIntDict(defaultAmounts)
	defaultNeeds         = card.Needs{Amounts: defaultAmounts}
)

func makeFrozenIntDict(m map[string]int) *starlark.Dict {
	d := starlark.NewDict(len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if err := d.SetKey(starlark.String(k), starlark.MakeInt(m[k])); err != nil {
			panic(err)
		}
	}
	d.Freeze()
	return d
}

func frozenIntDict(m map[string]int) *starlark.Dict {
	if len(m) == 0 {
		return emptyResourcesDict
	}
	if len(m) == 2 && m["cpu"] == 1 && m["memory"] == 1 {
		return defaultResourcesDict
	}
	return makeFrozenIntDict(m)
}

// index is what one tick's inputs are built from: the snapshot, the roster
// and the counts every question shares, computed once.
type index struct {
	snap           store.Snapshot
	knownTags      map[string]bool
	knownResources map[string]bool
	rows           []rostered          // the snapshot's rows the roster names, by name, byte order
	facts          map[string]rostered // the same, by name
	load           map[string][2]int   // row -> working, ready consumers it holds
	workingByRow   map[string][]string // row -> working consumer ids
	candVals       map[string]starlark.Value
	reads          map[string][]string    // producer -> its done reads of its current review and head, by id
	cons           map[string][]card.Card // producer -> its consumers, for last_bounce
	streams        map[string]*streamCount
	sets           map[string]*candidateSet // by the deal's list; with none, by tier, kind, needs and (for a read) author
}

// rostered is a store row with its configuration from the roster, by its
// name (store round 22: a row is sprint state only; nova-config, read by
// roster.Load, is the one source of kind, tags, resources, width, tiers,
// model and harness).
type rostered struct {
	store.Row
	kind, model, harness, held string
	width                      int
	tiers, tags                []string
	resources, free            map[string]int
}

// rosterOf is r with its roster facts; ok is false when the roster does not
// name it (a worker has no model and carries tags and resources instead of width).
func rosterOf(ro roster.Roster, r store.Row) (rostered, bool) {
	if w, ok := ro.Workers[r.Name]; ok {
		return rostered{
			Row:       r,
			kind:      roster.KindWorker,
			harness:   w.Harness,
			held:      w.Held,
			tiers:     w.Tiers,
			tags:      w.Tags,
			resources: w.Resources,
		}, true
	}
	if k, ok := ro.Kin[r.Name]; ok {
		return rostered{Row: r, kind: roster.KindKin, model: k.Model, harness: k.Harness, width: k.Width, tiers: k.Tiers}, true
	}
	return rostered{}, false
}

// candidateSet is one list of candidates, built once per tick and shared by
// every rows question with the same list; digest names its contents for the
// answer cache.
type candidateSet struct {
	rows   []rostered
	names  map[string]int // name -> its place in rows
	list   starlark.Tuple
	digest string
}

type streamCount struct{ merging, review, cards, landed int }

func rosterVocabulary(ro roster.Roster) (tags, resources map[string]bool) {
	if len(ro.Workers) == 0 {
		return nil, nil
	}
	tags, resources = map[string]bool{}, map[string]bool{}
	for _, w := range ro.Workers {
		for _, t := range w.Tags {
			tags[t] = true
		}
		for r := range w.Resources {
			resources[r] = true
		}
	}
	return tags, resources
}

// newIndex indexes snap with ro: a row the roster does not name is in no
// question (Judgements reports it).
func newIndex(snap store.Snapshot, ro roster.Roster) *index {
	knownTags, knownResources := rosterVocabulary(ro)
	ix := &index{
		snap:           snap,
		knownTags:      knownTags,
		knownResources: knownResources,
		facts:          map[string]rostered{},
		load:           map[string][2]int{},
		workingByRow:   map[string][]string{},
		candVals:       map[string]starlark.Value{},
		reads:          map[string][]string{},
		cons:           map[string][]card.Card{},
		streams:        map[string]*streamCount{},
		sets:           map[string]*candidateSet{},
	}
	for _, r := range snap.Rows {
		if f, ok := rosterOf(ro, r); ok {
			ix.rows, ix.facts[r.Name] = append(ix.rows, f), f
		}
	}
	slices.SortFunc(ix.rows, func(a, b rostered) int { return cmp.Compare(a.Name, b.Name) })
	for name := range snap.Streams {
		ix.streams[name] = &streamCount{}
	}
	for _, c := range snap.Cards {
		ix.count(c)
	}
	for i, r := range ix.rows {
		if r.kind == roster.KindWorker {
			r.free = ix.benchFree(r)
			ix.rows[i] = r
			ix.facts[r.Name] = r
		}
	}
	for _, ids := range ix.reads {
		slices.Sort(ids)
	}
	return ix
}

func (ix *index) count(c store.Card) {
	switch c.Kind {
	case "work", "read":
		l := ix.load[c.Holder]
		if c.State == "working" {
			l[0]++
			ix.workingByRow[c.Holder] = append(ix.workingByRow[c.Holder], c.ID)
		} else if c.State == "ready" {
			l[1]++
		}
		ix.load[c.Holder] = l
		ix.cons[c.Of] = append(ix.cons[c.Of], card.FromStore(c))
		if p, ok := ix.snap.Cards[c.Of]; ok && c.Kind == "read" && c.State == "done" && c.Review == p.Review && c.Head == p.Head {
			ix.reads[c.Of] = append(ix.reads[c.Of], c.ID)
		}
	case "producer":
		if s := ix.streams[c.Stream]; s != nil {
			s.cards++
			s.merging += b2i(c.State == "merging")
			s.review += b2i(c.State == "review")
			s.landed += b2i(c.State == "landed")
		}
	}
}

func (ix *index) resolveNeeds(kind, brief string) card.Needs {
	if kind == "read" || brief == "" {
		return defaultNeeds
	}
	prefix := brief[:min(len(brief), 4096)]
	if !strings.Contains(prefix, card.ResourcesField) {
		return defaultNeeds
	}
	return card.ResolveNeeds(kind, brief, ix.knownTags, ix.knownResources)
}

// benchFree computes baseline available resources on bench r when live
// Questions.BenchFree is not supplied (e.g. cold policy check).
func (ix *index) benchFree(r rostered) map[string]int {
	if len(r.resources) == 0 {
		return nil
	}
	free := make(map[string]int, len(r.resources))
	if r.held != "" {
		for k := range r.resources {
			free[k] = 0
		}
		return free
	}
	var sprintUsed map[string]int
	if ids := ix.workingByRow[r.Name]; len(ids) > 0 {
		sprintUsed = map[string]int{}
		for _, id := range ids {
			needs := defaultNeeds
			if cons, ok := ix.snap.Cards[id]; ok {
				needs = ix.resolveNeeds(cons.Kind, cons.Brief)
			}
			for k, v := range needs.Amounts {
				sprintUsed[k] += v
			}
		}
	}
	for k, total := range r.resources {
		free[k] = max(0, total-sprintUsed[k])
	}
	return free
}

func (ix *index) applyBenchFree(override map[string]map[string]int) {
	if len(override) == 0 {
		return
	}
	changed := false
	for i, r := range ix.rows {
		if r.kind != roster.KindWorker {
			continue
		}
		ov, ok := override[r.Name]
		if !ok {
			continue
		}
		free := make(map[string]int, len(r.resources))
		for k := range r.resources {
			if r.held != "" {
				free[k] = 0
			} else {
				free[k] = max(0, ov[k])
			}
		}
		r.free = free
		ix.rows[i] = r
		ix.facts[r.Name] = r
		delete(ix.candVals, r.Name)
		changed = true
	}
	if changed {
		clear(ix.sets)
	}
}

func rosteredFitsMax(r rostered, n card.Needs) bool {
	if r.kind == roster.KindKin {
		return true
	}
	w := roster.Worker{Name: r.Name, Tiers: r.tiers, Tags: r.tags, Resources: r.resources, Held: r.held}
	return w.FitsMax(n)
}

func needsCacheKey(n card.Needs) string {
	if len(n.Tags) == 0 && len(n.Amounts) == 2 && n.Amounts["cpu"] == 1 && n.Amounts["memory"] == 1 {
		return ""
	}
	var b strings.Builder
	for _, t := range n.Tags {
		b.WriteString("t:")
		b.WriteString(t)
		b.WriteByte(',')
	}
	for _, k := range slices.Sorted(maps.Keys(n.Amounts)) {
		fmt.Fprintf(&b, "%s=%d,", k, n.Amounts[k])
	}
	return b.String()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Rank is the highest of a row's tiers, in ladder order; "" when none.
func Rank(tiers []string) string {
	rank := ""
	for _, t := range store.Tiers {
		if slices.Contains(tiers, t) {
			rank = t
		}
	}
	return rank
}

func rung(tier string) int { return slices.Index(store.Tiers, tier) }

// candidateSet is rows' third argument for producer c and question rq, by
// rank, lowest first, then by name. In a tick it is the deal's own list,
// dealt[rq], row for row. With no list (check) it is the rows the store and
// the roster show may take the consumer: up, not held, hooked in as worker or
// coordinator, serving the card's tier and tags/max-resources, and for a read
// not its author.
func (ix *index) candidateSet(c store.Card, rq RowsQuestion, dealt map[RowsQuestion][]string) *candidateSet {
	offered, tick := dealt[rq]
	needs := ix.resolveNeeds(rq.Kind, c.Brief)
	key := c.Tier + " " + rq.Kind + " " + needsCacheKey(needs)
	if rq.Kind == KindRead {
		key += " " + c.Author
	}
	if tick {
		key = "\x00" + strings.Join(offered, "\x00")
	}
	if s, ok := ix.sets[key]; ok {
		return s
	}
	s := &candidateSet{}
	for _, r := range ix.rows {
		hooked := r.Session != "" && (r.Role == "worker" || r.Role == "coordinator") // steps.Hooked
		open := r.State == "up" && !r.Held && hooked && slices.Contains(r.tiers, c.Tier) &&
			rosteredFitsMax(r, needs) && (rq.Kind != KindRead || r.Name != c.Author)
		if (tick && slices.Contains(offered, r.Name)) || (!tick && open) {
			s.rows = append(s.rows, r)
		}
	}
	slices.SortStableFunc(s.rows, func(a, b rostered) int { return cmp.Compare(rung(Rank(a.tiers)), rung(Rank(b.tiers))) })
	s.list, s.names = make(starlark.Tuple, len(s.rows)), map[string]int{}
	for i, r := range s.rows {
		s.list[i], s.names[r.Name] = ix.candidate(r), i
	}
	sum := sha256.Sum256([]byte(s.list.String()))
	s.digest = hex.EncodeToString(sum[:])
	ix.sets[key] = s
	return s
}

func newStruct(kv ...any) *starlarkstruct.Struct {
	d := starlark.StringDict{}
	for i := 0; i < len(kv); i += 2 {
		d[kv[i].(string)] = value(kv[i+1])
	}
	s := starlarkstruct.FromStringDict(starlarkstruct.Default, d)
	s.Freeze()
	return s
}

func value(v any) starlark.Value {
	switch x := v.(type) {
	case string:
		return starlark.String(x)
	case int:
		return starlark.MakeInt(x)
	case []string:
		if len(x) == 0 {
			return emptyTuple
		}
		return tupleOf(x)
	}
	return v.(starlark.Value)
}

// orderCard is order's subject: id, stream, score, priority, tier, attempt,
// bounces, prior_head (the head a fix attempt continues from, "" on a first).
func orderCard(c store.Card) starlark.Value {
	return newStruct("id", c.ID, "stream", c.Stream, "score", c.Score, "priority", cmp.Or(c.Priority, "normal"),
		"tier", c.Tier, "attempt", c.Attempt, "bounces", c.Bounces, "prior_head", c.Head)
}

// card is the subject of rows, reads and verdict: id, stream, tier (its
// minimum), attempt, bounces, priority, writer_rank, last_bounce, tags and
// resources. Never the prose.
func (ix *index) card(c store.Card) starlark.Value {
	writer := c.Tier
	if r, ok := ix.facts[c.Author]; ok && Rank(r.tiers) != "" {
		writer = Rank(r.tiers)
	}
	needs := ix.resolveNeeds("work", c.Brief)
	return newStruct("id", c.ID, "stream", c.Stream, "tier", c.Tier, "attempt", c.Attempt,
		"bounces", c.Bounces, "priority", cmp.Or(c.Priority, "normal"), "writer_rank", writer,
		"last_bounce", card.LastBounce(card.FromStore(c), ix.cons[c.ID]),
		"tags", needs.Tags, "resources", frozenIntDict(needs.Amounts))
}

// candidate is one row offered to rows: benches expose tags, resources and free
// (and no width); kin expose width (and empty tags, resources, free).
func (ix *index) candidate(r rostered) starlark.Value {
	if v, ok := ix.candVals[r.Name]; ok {
		return v
	}
	l := ix.load[r.Name]
	var v starlark.Value
	if r.kind == roster.KindKin {
		v = newStruct("name", r.Name, "kind", r.kind, "tiers", r.tiers, "rank", Rank(r.tiers), "model", r.model, "harness", r.harness,
			"width", r.width, "tags", emptyTuple, "resources", emptyResourcesDict, "free", emptyResourcesDict,
			"working", l[0], "ready", l[1])
	} else {
		v = newStruct("name", r.Name, "kind", r.kind, "tiers", r.tiers, "rank", Rank(r.tiers), "model", r.model, "harness", r.harness,
			"tags", r.tags, "resources", frozenIntDict(r.resources), "free", frozenIntDict(r.free),
			"working", l[0], "ready", l[1])
	}
	ix.candVals[r.Name] = v
	return v
}

// row is width's subject (kin only): name, kind, width, tiers, model, harness,
// flaps.
func row(r rostered) starlark.Value {
	return newStruct("name", r.Name, "kind", r.kind, "width", r.width, "tiers", r.tiers, "model", r.model,
		"harness", r.harness, "flaps", r.Flaps)
}

// read is one done read for verdict: row, rank, result (ok or fix), finding
// (the read's reason).
func (ix *index) read(c store.Card) starlark.Value {
	return newStruct("row", c.Holder, "rank", Rank(ix.facts[c.Holder].tiers), "result", c.Result, "finding", c.Reason)
}

// landingStream is landing's subject: name, merging, review, rung.
func (ix *index) landingStream(q LandingQuestion) starlark.Value {
	s := ix.streams[q.Stream]
	return newStruct("name", q.Stream, "merging", s.merging, "review", s.review, "rung", q.Rung)
}

// stitchStream is stitch's subject: name, cards (its producers), landed.
func (ix *index) stitchStream(name string) starlark.Value {
	s := ix.streams[name]
	return newStruct("name", name, "cards", s.cards, "landed", s.landed)
}

// FullQuestions are every question the snapshot poses: order and work rows
// for each ready producer, reads and read rows for each in review, verdict
// for each in review with a done read, landing and stitch for each stream
// (on the rung rungs names, flowing when it names none). Check and the
// conformance tests ask these, so check predicts a tick that asks them.
func FullQuestions(snap store.Snapshot, rungs map[string]string) Questions {
	var q Questions
	ix := newIndex(snap, roster.Roster{}) // the reads counted; no question here reads a row
	for _, id := range slices.Sorted(maps.Keys(snap.Cards)) {
		c := snap.Cards[id]
		switch {
		case c.Kind != "producer":
		case c.State == "ready":
			q.Order = append(q.Order, id)
			q.Rows = append(q.Rows, RowsQuestion{id, KindWork})
		case c.State == "review":
			q.Rows = append(q.Rows, RowsQuestion{id, KindRead})
			q.Reads = append(q.Reads, id)
			if len(ix.reads[id]) > 0 {
				q.Verdict = append(q.Verdict, id)
			}
		}
	}
	for _, s := range slices.Sorted(maps.Keys(snap.Streams)) {
		q.Landing = append(q.Landing, LandingQuestion{s, cmp.Or(rungs[s], RungFlowing)})
		q.Stitch = append(q.Stitch, s)
	}
	return q
}
