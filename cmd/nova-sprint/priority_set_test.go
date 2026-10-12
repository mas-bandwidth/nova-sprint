package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// priorityRaisesASelectedSetInOneCall is the test of
// priority-raises-a-selected-set-in-one-call: `priority` takes the shared selector of
// cmd/nova-sprint/select.go, so a shell loop of `priority <id> --critical` (or `priority
// --stream`, which also changes the stream's default) becomes one call over the set the
// selector names (docs/SPEC-SPRINT.md, "One selector, one step"). The store is private and
// named explicitly with --redis; the environment names no store or actor (scrubbed env), so
// nothing of the host can hold a verb. It fails at the base: `priority --ids-file` admits no
// selector and is refused.
func TestPriorityRaisesASelectedSetInOneCall(t *testing.T) {
	t.Parallel()
	ta := newTestApp(t)
	home := t.TempDir()
	ta.a.getenv = func(k string) string {
		switch k {
		case "HOME":
			return home
		case "XDG_CONFIG_HOME":
			return filepath.Join(home, "config")
		}
		return ""
	}
	// every verb names its store and its actor: no NOVA_* variable is read
	line := func(words string) string { return words + " --redis mem:0 --actor coordinator" }
	priorityOf := func(id string) (string, string) {
		var v struct {
			Priority       string `json:"priority"`
			PrioritySource string `json:"priority_source"`
		}
		ta.json(line("card "+id), &v)
		return v.Priority, v.PrioritySource
	}

	ta.ok(line("init --readers reader-a,reader-b --members m1"))
	ta.ok(line("add --stream s1 --count 3"))
	ta.ok(line("add --stream s2 --count 2"))
	ta.ok(line("check"))

	// an ids file of three cards in the two streams, raised in one call
	ids := filepath.Join(t.TempDir(), "raise.txt")
	require.NoError(t, os.WriteFile(ids, []byte("# the next point release\ns1-1\ns2-1\ns2-2\n"), 0o644))
	out := ta.ok(line("priority --ids-file " + ids + " --critical --reason 'next point release'"))
	assert.Contains(t, out, "PRIORITY OK", out)
	assert.Contains(t, out, "selected=3", out)
	for _, id := range []string{"s1-1", "s2-1", "s2-2"} {
		l, src := priorityOf(id)
		assert.Equal(t, [2]string{sprint.PriorityCritical, "set"}, [2]string{l, src}, id)
		logged := ta.ok(line("log --card " + id))
		assert.Equal(t, 1, strings.Count(logged, "priority normal -> critical"), "%s: one timeline entry\n%s", id, logged)
		assert.Contains(t, logged, "next point release", "the reason is on %s's timeline", id)
	}
	for _, id := range []string{"s1-2", "s1-3"} {
		l, _ := priorityOf(id)
		assert.Equal(t, sprint.PriorityNormal, l, "%s is not selected", id)
	}
	// the selector with another word selects only: the stream's default is unchanged,
	// so a card added later is still normal
	assert.NotContains(t, ta.ok(line("log --stream s1")), "stream priority set", "no stream default change")
	ta.ok(line("add --stream s1 --count 1 --one"))
	l, src := priorityOf("s1-4")
	assert.Equal(t, [2]string{sprint.PriorityNormal, "default"}, [2]string{l, src}, "the stream default is unchanged")

	// --dry-run writes nothing
	applies := ta.applies()
	out = ta.ok(line("priority --ids-file " + ids + " --high --reason 'a dry run' --dry-run"))
	assert.Contains(t, out, "DRY-RUN", out)
	assert.Contains(t, out, "selected=", out)
	assert.Equal(t, applies, ta.applies(), "a dry run wrote")
	l, _ = priorityOf("s1-1")
	assert.Equal(t, sprint.PriorityCritical, l, "a dry run changed s1-1")

	// an ids file naming a read card refuses the whole call, nothing written
	ta.deal(1)
	ta.ok(line("take --as m1 --limit 1"))
	var q struct{ Cards []queueCard }
	ta.json(line("queue --as m1"), &q)
	require.NotEmpty(t, q.Cards, "a card is taken")
	wc := q.Cards[0]
	ta.ok(line("finish --as m1 " + wc.ID + "@" + strconv.Itoa(wc.Gen) + " --report 'done'"))
	ta.ok(line("ask --limit 100"))
	var view cardView
	ta.json(line("card "+wc.Primary), &view)
	require.NotEmpty(t, view.Reads, "the ask made a read card of %s", wc.Primary)
	readID := view.Reads[0].ID
	assert.Contains(t, readID, wc.Primary+".", "the read is of the dealt card")
	readIDs := filepath.Join(t.TempDir(), "read.txt")
	require.NoError(t, os.WriteFile(readIDs, []byte(readID+"\n"), 0o644))
	before, _ := priorityOf(wc.Primary)
	applies = ta.applies()
	code, out2, err2 := ta.do(line("priority --ids-file " + readIDs + " --critical --reason r"))
	assert.NotEqual(t, 0, code, "%s%s", out2, err2)
	assert.Contains(t, out2+err2, readID, "the read card named is refused")
	assert.Equal(t, applies, ta.applies(), "a refused selection wrote")
	l, _ = priorityOf(wc.Primary)
	assert.Equal(t, before, l, "a refused read card changed nothing")
}
