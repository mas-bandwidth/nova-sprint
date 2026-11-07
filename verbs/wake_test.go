package verbs

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/store"
)

// wakeOf runs `wake` as name on one bus line (nova-bus hook --exec's stdin).
func wakeOf(w *world, name, line string) Result {
	w.e.Stdin = strings.NewReader(line + "\n")
	return w.do(name, "wake")
}

func dealLine(id, ref, row string) string {
	return "card=" + id + " attempt=1 kind=work stream=s tier=pro epoch=1; run: nova-sprint take " + ref + " --epoch 1 --as " + row
}

func TestWakeTakesEveryDealtCard(t *testing.T) {
	t.Parallel()
	s := sprint()
	setCard("p1.w1", func(c *store.Card) { c.Holder = "bob" })(&s)
	w := worldFrom(t, s)
	body := dealLine("p1.w1", "p1.w1@1", "bob") + `\n` + dealLine("p2.r1", "p2.r1@1", "bob")
	r := wakeOf(w, "bob", "7-0\tsprint\t-\tDEAL\t"+body)
	has(t, r, "7-0\tsprint\t-\tDEAL\tTAKEN p1.w1 repo=o/r", `\nTAKEN p2.r1 repo=o/r head=`, `do p1`)
	if strings.Count(r.Output, "\n") != 1 || strings.Contains(r.Output, "run: nova-sprint take") {
		t.Fatalf("one line, the deals replaced:\n%s", r.Output)
	}
	for _, id := range []string{"p1.w1", "p2.r1"} {
		if s := w.snap(t).Cards[id].State; s != "working" {
			t.Fatalf("%s is %s, want working", id, s)
		}
	}
	if again := wakeOf(w, "bob", "7-0\tsprint\t-\tDEAL\t"+body); again != r {
		t.Fatalf("a redelivered deal is the same wake:\n%s\n%s", again.Output, r.Output)
	}
}

func TestWakeRefusedTakeWakesWithTheRefusal(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	r := wakeOf(w, "alice", "8-0\tsprint\t-\tDEAL\t"+dealLine("p1.w1", "p1.w1@2", "alice"))
	has(t, r, "8-0\tsprint\t-\tDEAL\tSPRINT REFUSED take: p1.w1 is not dealt to this row at this generation")
	if s := w.snap(t).Cards["p1.w1"].State; s != "ready" {
		t.Fatalf("p1.w1 is %s, want ready", s)
	}
}

func TestWakePassesOtherMessagesThrough(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	for _, line := range []string{
		"9-0\tcarol\t-\tNOTE\thello\\nworld",
		"9-1\tsprint\t-\tWITHDRAWN\tWITHDRAWN card=p1.w1 reason=x; stop the runner and push its work",
		"9-2\tdave\t-\tDEAL\t" + dealLine("p1.w1", "p1.w1@1", "alice"), // not the sprint's: never taken
		"9-3\tsprint\t-\tDEAL\t" + dealLine("p2.r1", "p2.r1@1", "bob"), // another row's
	} {
		if r := wakeOf(w, "alice", line); r.Exit != 0 || r.Output != line+"\n" {
			t.Fatalf("%q: exit %d\n%s", line, r.Exit, r.Output)
		}
	}
	if s := w.snap(t).Cards["p1.w1"].State; s != "ready" {
		t.Fatalf("p1.w1 is %s, want ready", s)
	}
}

// has checks a delivered wake: exit 0 and every part.
func has(t *testing.T, r Result, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if r.Exit != 0 || !strings.Contains(r.Output, p) {
			t.Fatalf("exit %d, output lacks %q:\n%s", r.Exit, p, r.Output)
		}
	}
}
