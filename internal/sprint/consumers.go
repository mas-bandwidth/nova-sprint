package sprint

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
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
	Attempts int          `json:"attempts"`
	Reads    int          `json:"reads"`
	Landings int          `json:"landings"`
	Cost     string       `json:"cost"`
	Consumers []ConsumerRec `json:"consumers"`
}

const (
	// FieldConsumerPrefix plus the consumer's key (ConsumerRec.Key) is a consumer
	// record on the primary.
	FieldConsumerPrefix = "consumer:"
)

// AddConsumer records a consumer on the primary as it happens, keyed by its stable key
// and carrying its epoch. It writes to the card's changes (set); it is idempotent per
// key, so a step planned again never doubles a record.
func AddConsumer(pr *Card, set map[string]string, r ConsumerRec) {
	key := FieldConsumerPrefix + r.Key
	if pr.F(key) != "" || set[key] != "" {
		return
	}
	if set == nil {
		set = map[string]string{}
	}
	set[key] = encodeConsumerRec(r)
}

// encodeConsumerRec is a consumer record as the card carries it.
func encodeConsumerRec(r ConsumerRec) string {
	return strings.Join([]string{
		"epoch=" + strconv.FormatUint(r.Epoch, 10),
		"kind=" + r.Kind,
		"who=" + orDash(r.Who),
		"route=" + orDash(r.Route),
		"model=" + orDash(r.Model),
		"taken=" + orDash(r.Taken),
		"ended=" + orDash(strings.ReplaceAll(r.Ended, " ", "-")),
		"outcome=" + orDash(r.Outcome),
		"first_line=" + orDash(r.FirstLine),
	}, " ")
}

// parseConsumerRec reads a consumer record back.
func parseConsumerRec(line string) ConsumerRec {
	var r ConsumerRec
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
			rec.Key = strings.TrimPrefix(k, FieldConsumerPrefix)
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
