package sprint

import (
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-tools/pkg/cardcost"
)

// The consumer history is the producer card's record of every consumer the machine
// launched for it: an attempt (wN: the member or friend), a read (rN: the reader),
// and the landing. Each is recorded on the card as it happens, in order, keyed by
// epoch, so the card itself carries who ran what, when and how it ended (SPEC-SPRINT,
// "the consumer record"). A clear drops it with the epoch: every key carries it.

// ConsumerRec is one consumer the machine launched for a producer card. It is put on
// the card when it starts (Taken), and its Ended/Outcome/FirstLine are written when
// it ends; a consumer still out has them empty. Key is the launch's stable identity
// (an attempt wN, a read rN, a landing), so a step planned again never records it twice.
type ConsumerRec struct {
	Key       string `json:"key"`
	Epoch     uint64 `json:"epoch"`
	Kind      string `json:"kind"` // work, read, land
	Who       string `json:"who,omitempty"`
	Route     string `json:"route,omitempty"`
	Model     string `json:"model,omitempty"`
	Taken     string `json:"taken"`
	Ended     string `json:"ended,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
	FirstLine string `json:"first_line,omitempty"`
}

// ConsumersView is a producer card's consumer history as the card and view print it:
// the counts, then one line per consumer, newest last.
type ConsumersView struct {
	Attempts  int           `json:"attempts"`
	Reads     int           `json:"reads"`
	Landings  int           `json:"landings"`
	Cost      string        `json:"cost"`
	Consumers []ConsumerRec `json:"consumers"`
}

const (
	// FieldConsumerPrefix plus the consumer's key (ConsumerRec.Key) is a consumer
	// record on the primary.
	FieldConsumerPrefix = "consumer:"

	// consumerFirstLineBytes bounds the first line a consumer record keeps. The
	// record is one JSON-encoded field on the card, and the store bounds a field
	// at MaxCardTextBytes, so the first line is cut well under it to leave the
	// rest of the record room.
	consumerFirstLineBytes = 8192
)

// AddConsumer records a consumer on the primary as it happens, keyed by its stable key
// and carrying its epoch. It writes to the card's changes (set); it is idempotent per
// key, so a step planned again never doubles a record.
func AddConsumer(pr *Card, set map[string]string, r ConsumerRec) {
	key := consumerField(r)
	if pr.F(key) != "" || set[key] != "" {
		return
	}
	if set == nil {
		set = map[string]string{}
	}
	set[key] = encodeConsumerRec(r)
}

func consumerField(r ConsumerRec) string {
	return FieldConsumerPrefix + strconv.FormatUint(r.Epoch, 10) + ":" + r.Key
}

// encodeConsumerRec is a consumer record as the card carries it.
func encodeConsumerRec(r ConsumerRec) string {
	b, _ := json.Marshal(r)
	return string(b)
}

// parseConsumerRec reads a consumer record back.
func parseConsumerRec(line string) ConsumerRec {
	var r ConsumerRec
	if strings.HasPrefix(line, "{") {
		_ = json.Unmarshal([]byte(line), &r)
		return r
	}
	undash := func(v string) string {
		if v == "-" {
			return ""
		}
		return v
	}
	for _, w := range strings.Fields(line) {
		k, v, _ := strings.Cut(w, "=")
		switch k {
		case "epoch":
			r.Epoch, _ = strconv.ParseUint(v, 10, 64)
		case "kind":
			r.Kind = v
		case "who":
			r.Who = undash(v)
		case "route":
			r.Route = undash(v)
		case "model":
			r.Model = undash(v)
		case "taken":
			r.Taken = undash(v)
		case "ended":
			r.Ended = strings.ReplaceAll(undash(v), "-", " ")
		case "outcome":
			r.Outcome = undash(v)
		case "first_line":
			r.FirstLine = undash(v)
		}
	}
	return r
}

// ConsumersOf is the producer card's consumer history as the primary carries it: the
// records, in the order they were launched (keyed by epoch), and the counts.
func ConsumersOf(pr *Card) ConsumersView {
	v := ConsumersView{Consumers: []ConsumerRec{}}
	if pr == nil {
		return v
	}
	for k, line := range pr.Fields {
		if strings.HasPrefix(k, FieldConsumerPrefix) {
			rec := parseConsumerRec(line)
			if rec.Key == "" {
				rec.Key = strings.TrimPrefix(k, FieldConsumerPrefix)
			}
			v.Consumers = append(v.Consumers, rec)
		}
	}
	sort.Slice(v.Consumers, func(i, j int) bool {
		a, b := v.Consumers[i], v.Consumers[j]
		if a.Epoch != b.Epoch {
			return a.Epoch < b.Epoch
		}
		if a.Taken != b.Taken {
			return a.Taken < b.Taken
		}
		return a.Key < b.Key
	})
	for _, r := range v.Consumers {
		switch r.Kind {
		case "work":
			v.Attempts++
		case "read":
			v.Reads++
		case "land":
			v.Landings++
		}
	}
	v.Cost = cardcost.ParseTotal(pr.F(FieldCostTotal)).String()
	return v
}

// recordConsumerTransitions implements the consumer record in SPEC-SPRINT.
// The store calls it after lifecycle filtering, so the producer record and the
// consumer's actual launch/end are one fenced plan, including friend and member
// paths that reach the same table transitions through different verbs.
func recordConsumerTransitions(s *Snapshot, p Plan) Plan {
	if s == nil || s.Work == nil {
		return p
	}
	updates := map[string]map[string]string{}
	last := map[string]int{}
	for i, u := range p.Units {
		for _, ch := range u.Changes {
			e := ch.Entry
			if ch.Table == Work {
				// A landing: the producer card itself moves to Landed. Record it
				// as a kind "land" consumer (SPEC-SPRINT, "the consumer record"):
				// its taken (when it landed) and its outcome. The primary is the
				// card itself; it is the unit's own Work change that carries the
				// record, so the attach below finds it.
				pr := s.Work.Card(e.ID)
				if pr == nil || IsSentinel(pr) {
					continue
				}
				newCol := pr.Col
				if e.Move != nil {
					newCol = e.Move.Col
				}
				if newCol != Landed {
					continue
				}
				r := ConsumerRec{Key: "land", Epoch: s.Epoch, Kind: "land", Taken: e.Set["landed"], Outcome: "ok"}
				if r.Taken == "" {
					r.Taken = stamp(s.Now)
				}
				f := consumerField(r)
				if line := updates[e.ID][f]; line != "" {
					r = parseConsumerRec(line)
				} else if line := pr.F(f); line != "" {
					r = parseConsumerRec(line)
				}
				if updates[e.ID] == nil {
					updates[e.ID] = map[string]string{}
				}
				updates[e.ID][f] = encodeConsumerRec(r)
				last[e.ID] = i
				continue
			}
			if ch.Table != Fleet && ch.Table != Readers {
				continue
			}
			var c *Card
			if ch.Table == Fleet && s.Fleet != nil {
				c = s.Fleet.Card(e.ID)
			} else if ch.Table == Readers && s.Readers != nil {
				c = s.Readers.Card(e.ID)
			}
			field := func(k string) string {
				if v := e.Set[k]; v != "" {
					return v
				}
				if c != nil {
					return c.F(k)
				}
				return ""
			}
			primary := field("primary")
			pr := s.Work.Card(primary)
			if pr == nil || primary == "" {
				continue
			}
			kind := field("kind")
			if ch.Table == Readers {
				kind = "read"
			}
			if kind != "work" && kind != "read" {
				continue
			}
			oldCol, newCol := "", ""
			if c != nil {
				oldCol, newCol = c.Col, c.Col
			}
			if e.Create != nil {
				newCol = e.Create.Col
			}
			if e.Move != nil {
				newCol = e.Move.Col
			}
			if e.Remove {
				newCol = "retired"
			}
			start := (kind == "work" && oldCol != Working && newCol == Working) ||
				(kind == "read" && (e.Create != nil || (e.Set["asked"] != "" && oldCol == Asked)))
			end := (kind == "work" && oldCol == Working && newCol != Working) ||
				(kind == "read" && (oldCol == Asked || oldCol == Reading || oldCol == Working) &&
					(newCol == OK || newCol == Broken || newCol == "retired" || (e.Set[FieldReturned] != "")))
			if !start && !end {
				continue
			}
			gen := max(atoiOr(field("gen")), 1)
			key := e.ID + "#g" + itoa(gen)
			if kind == "read" {
				key = e.ID + "#r" + itoa(max(1, atoiOr(field(FieldReasked))+1))
			}
			r := ConsumerRec{Key: key, Epoch: s.Epoch, Kind: kind, Who: field("reader"), Route: field(FieldRoute), Model: field(FieldModel)}
			if r.Who == "" {
				if c != nil {
					r.Who = c.Row
				} else if e.Create != nil {
					r.Who = e.Create.Row
				}
			}
			f := consumerField(r)
			if line := updates[primary][f]; line != "" {
				r = parseConsumerRec(line)
			} else if line := pr.F(f); line != "" {
				r = parseConsumerRec(line)
			}
			if start && r.Taken == "" {
				r.Taken = field("asked")
				if kind == "work" {
					r.Taken = field("taken")
				}
				if r.Taken == "" {
					r.Taken = stamp(s.Now)
				}
			}
			if end {
				if r.Taken == "" {
					r.Taken = field("asked")
					if kind == "work" {
						r.Taken = field("taken")
					}
					if r.Taken == "" {
						r.Taken = stamp(s.Now)
					}
				}
				r.Ended = stamp(s.Now)
				r.Outcome = field("verdict")
				if r.Outcome == "" {
					r.Outcome = field("result")
				}
				if r.Outcome == "" {
					r.Outcome = field("retired_by")
				}
				if r.Outcome == "" {
					r.Outcome = newCol
				}
				words := field("report")
				if kind == "read" {
					words = field("finding")
					if words == "" {
						words = field(FieldRefused)
					}
				}
				r.FirstLine = cutText(strings.TrimSpace(strings.SplitN(words, "\n", 2)[0]), consumerFirstLineBytes)
			}
			if updates[primary] == nil {
				updates[primary] = map[string]string{}
			}
			updates[primary][f] = encodeConsumerRec(r)
			last[primary] = i
		}
	}
	for primary, set := range updates {
		i := last[primary]
		attached := false
		for j := range p.Units[i].Changes {
			ch := &p.Units[i].Changes[j]
			if ch.Table == Work && ch.Entry.ID == primary {
				// The record goes into a map this plan owns, never into the one
				// the change arrived with: a drained change keeps the queued
				// entry's own Set map (Drain copies the entry, not its maps, and
				// the backing hands every reader of the queue the same entry), so
				// writing it here wrote the backing's map under another run
				// loop's read of it (consumers_race_test.go).
				merged := make(map[string]string, len(ch.Entry.Set)+len(set))
				maps.Copy(merged, ch.Entry.Set)
				maps.Copy(merged, set)
				ch.Entry.Set = merged
				attached = true
				break
			}
		}
		if !attached {
			p.Units[i].Changes = append(p.Units[i].Changes, change(Work, setEntry(s.Work.Card(primary), set)))
		}
	}
	return p
}

// Stalled is the producer/consumer check's read of the record (docs/
// SPEC-SPRINT.md, "the consumer record"): a card whose newest consumer has ended
// and no newer one is out is the stall it names. The consumers are in launch
// order (newest last), so the newest is the last; it is out while its Ended is
// empty. A card with no consumer history is not a stall.
func (v ConsumersView) Stalled() bool {
	if len(v.Consumers) == 0 {
		return false
	}
	return v.Consumers[len(v.Consumers)-1].Ended != ""
}

// ConsumerLines are the consumers block's lines as `card <id>` prints them: the
// counts, then one line per consumer, newest last.
func (v ConsumersView) ConsumerLines() []string {
	out := []string{fmt.Sprintf("CONSUMERS attempts=%d reads=%d landings=%d cost=%s", v.Attempts, v.Reads, v.Landings, orDash(v.Cost))}
	for _, r := range v.Consumers {
		out = append(out, fmt.Sprintf("CONSUMER epoch=%d kind=%s who=%s route=%s model=%s taken=%s ended=%s outcome=%s first_line=%s",
			r.Epoch, r.Kind, orDash(r.Who), orDash(r.Route), orDash(r.Model), orDash(r.Taken), orDash(r.Ended), orDash(r.Outcome), orDash(r.FirstLine)))
	}
	return out
}
