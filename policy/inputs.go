package policy

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mas-bandwidth/nova-sprint/roster"
	"github.com/mas-bandwidth/nova-sprint/store"
)

// index is what one tick's inputs are built from: the snapshot, the roster
// and the counts every question shares, computed once.
type index struct {
	snap    store.Snapshot
	rows    []rostered          // the snapshot's rows the roster names, by name, byte order
	facts   map[string]rostered // the same, by name
	load    map[string][2]int   // row -> working, ready consumers it holds
	reads   map[string][]string // producer -> its done reads of its current review and head, by id
	streams map[string]*streamCount
	sets    map[string]*candidateSet // by tier, kind and (for a read) author
}

// rostered is a store row with its configuration from the roster, by its
// name (a row is sprint state only; nova-config, read by
// roster.Load, is the one source of kind, width, tiers, model and harness).
type rostered struct {
	store.Row
	kind, model, harness string
	width                int
	tiers                []string
}

// rosterOf is r with its roster facts; ok is false when the roster does not
// name it (a worker has no model).
func rosterOf(ro roster.Roster, r store.Row) (rostered, bool) {
	if w, ok := ro.Workers[r.Name]; ok {
		return rostered{Row: r, kind: roster.KindWorker, harness: w.Harness, width: w.Width, tiers: w.Tiers}, true
	}
	if k, ok := ro.Kin[r.Name]; ok {
		return rostered{Row: r, kind: roster.KindKin, model: k.Model, harness: k.Harness, width: k.Width, tiers: k.Tiers}, true
	}
	return rostered{}, false
}

// candidateSet is one list of candidates, built once per tick and shared by
// every rows question with the same tier, kind and author; digest names
// its contents for the answer cache.
type candidateSet struct {
	rows   []rostered
	names  map[string]int // name -> its place in rows
	list   starlark.Tuple
	digest string
}

type streamCount struct{ merging, review, cards, landed int }

// newIndex indexes snap with ro: a row the roster does not name is in no
// question (Judgements reports it).
func newIndex(snap store.Snapshot, ro roster.Roster) *index {
	ix := &index{snap: snap, facts: map[string]rostered{}, load: map[string][2]int{}, reads: map[string][]string{}, streams: map[string]*streamCount{}, sets: map[string]*candidateSet{}}
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
	for _, ids := range ix.reads {
		slices.Sort(ids)
	}
	return ix
}

func (ix *index) count(c store.Card) {
	switch c.Kind {
	case "work", "read":
		l := ix.load[c.Holder]
		l[0] += b2i(c.State == "working")
		l[1] += b2i(c.State == "ready")
		ix.load[c.Holder] = l
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

// candidateSet is rows' third argument for producer c and kind: the rows
// that may take its next consumer (up, not held, serving the card's tier,
// and for a read not its author), by rank, lowest first, then by name.
func (ix *index) candidateSet(c store.Card, kind string) *candidateSet {
	key := c.Tier + " " + kind
	if kind == KindRead {
		key += " " + c.Author
	}
	if s, ok := ix.sets[key]; ok {
		return s
	}
	s := &candidateSet{}
	for _, r := range ix.rows {
		if r.State == "up" && !r.Held && slices.Contains(r.tiers, c.Tier) && (kind != KindRead || r.Name != c.Author) {
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
// minimum), attempt, bounces, priority and writer_rank (the rank of the row
// that wrote the head; the card's tier when that row is unknown). Never the
// prose: a brief of any size would enter every
// input and the answer cache's key, so the inputs would grow with the briefs
// and not with the card count.
func (ix *index) card(c store.Card) starlark.Value {
	writer := c.Tier
	if r, ok := ix.facts[c.Author]; ok && Rank(r.tiers) != "" {
		writer = Rank(r.tiers)
	}
	return newStruct("id", c.ID, "stream", c.Stream, "tier", c.Tier, "attempt", c.Attempt,
		"bounces", c.Bounces, "priority", cmp.Or(c.Priority, "normal"), "writer_rank", writer)
}

// candidate is one row offered to rows: name, tiers, rank, model, harness,
// width, working, ready.
func (ix *index) candidate(r rostered) starlark.Value {
	l := ix.load[r.Name]
	return newStruct("name", r.Name, "tiers", r.tiers, "rank", Rank(r.tiers), "model", r.model, "harness", r.harness,
		"width", r.width, "working", l[0], "ready", l[1])
}

// row is width's subject: name, kind, width, tiers, model, harness (the
// roster's, from nova-config), flaps (the store's).
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
