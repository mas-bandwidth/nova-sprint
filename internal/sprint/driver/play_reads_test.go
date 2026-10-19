package driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// playCard is a card on a member's row or a reader's row in playWorld.
type playCard struct {
	id      string
	kind    string // "work" or "read"
	primary string
	reader  string
	col     string // ready, working, asked, reading, done
	gen     int
}

// playPrimary is one card of the stream the world plays: its work card is taken
// and finished, it waits in review for two reads, then a merge lands it.
type playPrimary struct {
	id    string
	col   string // ready, working, review, merging, landed
	reads int
}

// playWorld is a stateful in-memory sprint for the driver: the verbs the driver
// runs move real cards, so a run that never reports a read cannot land. It is
// the fake world driver_test.go uses, with the sprint's card lifecycle between
// the reads: a work card taken and finished puts its primary in review, its two
// read cards are dealt (on a member's row under read-cards on, on a reader's row
// under read-cards off), and a merge lands every card queued in the stream.
type playWorld struct {
	on          bool
	members     []string
	readers     []string
	order       []string
	primaries   map[string]*playPrimary
	cards       map[string]*playCard
	memberCards map[string][]string
	readerCards map[string][]string
	reads       int
	merges      int
}

func newPlayWorld(on bool) *playWorld {
	w := &playWorld{on: on, members: []string{"m1", "m2"}, readers: []string{"reader-a", "reader-b"},
		primaries: map[string]*playPrimary{}, cards: map[string]*playCard{},
		memberCards: map[string][]string{}, readerCards: map[string][]string{}}
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("s1-%d", i)
		w.order = append(w.order, id)
		w.primaries[id] = &playPrimary{id: id, col: "ready"}
		card := &playCard{id: id + ".w1", kind: "work", primary: id, col: "ready", gen: 1}
		w.cards[card.id] = card
		m := w.members[(i-1)%len(w.members)]
		w.memberCards[m] = append(w.memberCards[m], card.id)
	}
	return w
}

func (w *playWorld) landed() int {
	n := 0
	for _, id := range w.order {
		if w.primaries[id].col == "landed" {
			n++
		}
	}
	return n
}

// promote accepts a primary whose two reads have reported, as the machine's
// accept does, and queues it for its stream's merge step.
func (w *playWorld) promote(p *playPrimary) {
	if p.col == "review" && p.reads >= 2 {
		p.col = "merging"
	}
}

// dealReads cuts the two reads a primary in review needs: read cards on the
// members' rows when read-cards is on, asks on the readers' rows when it is off.
func (w *playWorld) dealReads(p *playPrimary) {
	for i := 0; i < 2; i++ {
		var reader string
		if w.on {
			reader = w.members[i%len(w.members)]
		} else {
			reader = w.readers[i%len(w.readers)]
		}
		id := sprint.ReadCardID(p.id, 1, reader)
		card := &playCard{id: id, kind: "read", primary: p.id, reader: reader, gen: 1}
		if w.on {
			card.col = "ready"
			w.memberCards[reader] = append(w.memberCards[reader], id)
		} else {
			card.col = "asked"
			w.readerCards[reader] = append(w.readerCards[reader], id)
		}
		w.cards[id] = card
	}
}

func (w *playWorld) closeRead(c *playCard) {
	c.col = "done"
	p := w.primaries[c.primary]
	p.reads++
	w.reads++
	w.promote(p)
}

var playValueFlags = map[string]bool{"--as": true, "--limit": true, "--stream": true, "--batch": true,
	"--epoch": true, "--usage": true, "--finding": true, "--report": true, "--redis": true, "--actor": true,
	"--max": true, "--seed": true, "--every": true, "--load": true, "--reason": true, "--base": true, "--base-ref": true}

// playArgs splits a command line into its flags and the words left (the cards).
func playArgs(args []string) (map[string]string, []string) {
	flags := map[string]string{}
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			words = append(words, a)
			continue
		}
		if k, v, ok := strings.Cut(a, "="); ok {
			flags[k] = v
			continue
		}
		if playValueFlags[a] && i+1 < len(args) {
			flags[a] = args[i+1]
			i++
			continue
		}
		flags[a] = "true"
	}
	return flags, words
}

func (w *playWorld) state() string {
	if w.landed() == len(w.order) {
		return "landed"
	}
	return "merging"
}

func (w *playWorld) view() map[string]any {
	landed := w.landed()
	merging := 0
	for _, id := range w.order {
		if w.primaries[id].col == "merging" {
			merging++
		}
	}
	fleet := map[string]any{}
	for _, m := range w.members {
		fleet[m] = map[string]any{"status": "up", "width": "64"}
	}
	readers := map[string]any{}
	for _, r := range w.readers {
		readers[r] = map[string]any{}
	}
	merge := map[string]any{"s1": map[string]any{"state": w.state(), "queued": strconv.Itoa(merging)}}
	return map[string]any{
		"epoch":   0,
		"landed":  landed,
		"all":     len(w.order),
		"summary": fmt.Sprintf("%d/%d", landed, len(w.order)),
		"machine": "machine: running",
		"tables": map[string]any{
			"fleet":   fleet,
			"readers": readers,
			"merge":   merge,
			"work":    map[string]any{"s1": map[string]any{}},
		},
		"streams": []map[string]any{{"Stream": "s1", "State": w.state()}},
	}
}

func (w *playWorld) queueCards(ids []string) map[string]any {
	cards := []map[string]any{}
	for _, id := range ids {
		c := w.cards[id]
		if c == nil || c.col == "done" {
			continue
		}
		cards = append(cards, map[string]any{"id": c.id, "col": c.col, "gen": c.gen})
	}
	return map[string]any{"cards": cards}
}

func (w *playWorld) run(args []string, stdout, stderr io.Writer) int {
	flags, words := playArgs(args[1:])
	switch args[0] {
	case "where":
		_ = json.NewEncoder(stdout).Encode(w.view())
	case "inbox":
		fmt.Fprintln(stdout, `{"groups":[]}`)
	case "fleet":
		// beat: the fake's members are up for the whole run
	case "queue":
		if as := flags["--as"]; as != "" {
			ids := w.memberCards[as]
			if !w.on {
				if rids, ok := w.readerCards[as]; ok {
					ids = rids
				}
			}
			_ = json.NewEncoder(stdout).Encode(w.queueCards(ids))
			break
		}
		var ids []string
		for _, p := range w.primaries {
			if p.col == "merging" {
				ids = append(ids, p.id)
			}
		}
		cards := []map[string]any{}
		for _, id := range ids {
			cards = append(cards, map[string]any{"id": id, "col": "queued"})
		}
		_ = json.NewEncoder(stdout).Encode(map[string]any{"cards": cards})
	case "take":
		for _, m := range strings.Split(flags["--as"], ",") {
			for _, id := range w.memberCards[m] {
				if c := w.cards[id]; c != nil && c.col == "ready" {
					c.col = "working"
				}
			}
		}
	case "finish":
		for _, word := range words {
			id, _, _ := strings.Cut(word, "@")
			c := w.cards[id]
			if c == nil || c.kind != "work" || c.col != "working" {
				fmt.Fprintf(stderr, "FINISH FAILED %s\n", word)
				return 1
			}
			c.col = "done"
			p := w.primaries[c.primary]
			p.col = "review"
			w.dealReads(p)
		}
	case "read":
		authors := strings.Split(flags["--as"], ",")
		reader := true
		for _, r := range authors {
			if _, ok := w.readerCards[r]; !ok {
				reader = false
			}
		}
		if reader {
			for _, word := range words {
				id, _, _ := strings.Cut(word, "@")
				c := w.cards[id]
				if c == nil {
					continue
				}
				switch {
				case flags["--begin"] != "" && c.col == "asked":
					c.col = "reading"
				case flags["--ok"] != "" && c.col == "reading", flags["--broken"] != "" && c.col == "reading":
					w.closeRead(c)
				}
			}
			break
		}
		for _, word := range words {
			id, _, _ := strings.Cut(word, "@")
			c := w.cards[id]
			if c == nil || c.kind != "read" || c.col != "working" {
				fmt.Fprintf(stderr, "READ FAILED %s\n", word)
				return 1
			}
			if flags["--begin"] != "" {
				fmt.Fprintf(stderr, "a read card has no begin: %s\n", id)
				return 1
			}
			w.closeRead(c)
		}
	case "merge":
		batch, _ := strconv.Atoi(flags["--batch"])
		if batch <= 0 {
			batch = 100
		}
		for _, id := range w.order {
			p := w.primaries[id]
			if p.col != "merging" {
				continue
			}
			if batch == 0 {
				break
			}
			p.col = "landed"
			batch--
		}
		w.merges++
	default:
		fmt.Fprintf(stderr, "playWorld: unknown verb %s\n", args[0])
		return 2
	}
	return 0
}

// TestPlayRunsASixCardSprintWithTwoReadsEachToEveryCardLanded plays one stream of
// six cards, each waiting for two reads, with read cards dealt to the members'
// rows and with the readers table's asks: every card lands, every read is
// reported, and at least one merge step runs.
func TestPlayRunsASixCardSprintWithTwoReadsEachToEveryCardLanded(t *testing.T) {
	for _, tc := range []struct {
		name string
		on   bool
	}{
		{"read cards on", true},
		{"read cards off", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newPlayWorld(tc.on)
			var out bytes.Buffer
			d := &Driver{Run: w.run, Base: []string{"--redis", "mem:play"},
				Facts: &scripted{}, Clock: &fakeClock{now: time.Unix(1000, 0)}, Out: &out,
				Config: Config{Every: time.Second, Batch: 100, Ticks: 60}}
			why, err := d.Loop()
			require.NoError(t, err, "loop: %s %v\n%s", why, err, out.String())
			require.Equal(t, "landed", why, "loop stopped at %s\n%s", why, out.String())
			require.Equal(t, 6, w.landed(), "landed %d of 6\n%s", w.landed(), out.String())
			require.GreaterOrEqual(t, w.reads, 12, "reads reported %d, want 12\n%s", w.reads, out.String())
			require.GreaterOrEqual(t, w.merges, 1, "merge steps %d, want at least one\n%s", w.merges, out.String())
			require.Contains(t, out.String(), "nova-sprint read ", "the run logged no read:\n%s", out.String())
			require.Contains(t, out.String(), "nova-sprint merge ", "the run logged no merge:\n%s", out.String())
		})
	}
}
