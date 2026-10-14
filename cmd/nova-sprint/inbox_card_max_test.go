package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// inboxJudgmentLines is the JUDGMENT lines of an inbox read.
func inboxJudgmentLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "JUDGMENT ") {
			lines = append(lines, l)
		}
	}
	return lines
}

// cardAttemptLines is the ATTEMPT rows of a card read.
func cardAttemptLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "ATTEMPT ") {
			lines = append(lines, l)
		}
	}
	return lines
}

// TestInboxAndCardHonourMax is the defect of sprint-inbox-and-card-honour-max:
// `inbox` and `card` register --max (default 20) and ignore it. The inbox honours
// it over the judgments and happened groups, newest or most-overdue first as the
// store lists them, and --open prints only its group; card honours it on its
// history rows, the newest --max with a MORE line naming how many are older.
// --max 0 is all. Unit; the mem twin.
func TestInboxAndCardHonourMax(t *testing.T) {
	t.Parallel()

	t.Run("inbox", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.ok("init --readers reader-a,reader-b --members m1")
		const n = 30
		var words []string
		for i := 0; i < n; i++ {
			stream := "s" + strconv.Itoa(i)
			ta.ok("add --one --stream " + stream + " --count 1")
			words = append(words, stream+"-1.w1@1")
		}
		ta.deal(n)
		ta.ok("take --as m1 --limit " + strconv.Itoa(n))
		ta.ok("finish --as m1 " + strings.Join(words, " ") + " --failed --report 'tests red'")

		out := ta.ok("inbox")
		require.Len(t, inboxJudgmentLines(out), 20, "the default --max 20 bounds the judgments:\n%s", out)
		require.Contains(t, out, "MORE", "a bounded listing names the rest:\n%s", out)

		all := ta.ok("inbox --max 0")
		require.Len(t, inboxJudgmentLines(all), n, "--max 0 is all:\n%s", all)
		require.NotContains(t, all, "MORE", "--max 0 cuts nothing:\n%s", all)

		var in struct{ Groups []sprint.Group }
		ta.json("inbox --max 0", &in)
		var judgments []sprint.Group
		for _, g := range in.Groups {
			if g.Kind == sprint.Judgment {
				judgments = append(judgments, g)
			}
		}
		require.Len(t, judgments, n)
		only := judgments[0]
		opened := ta.ok("inbox --open " + only.ID)
		require.Len(t, inboxJudgmentLines(opened), 1, "--open prints one group:\n%s", opened)
		require.Contains(t, opened, only.ID)
		ta.clean()
	})

	t.Run("card", func(t *testing.T) {
		t.Parallel()
		ta := newTestApp(t)
		ta.ok("init --readers reader-a,reader-b --members m1")
		ta.m.SetMaxAttempts("30")
		ta.ok("add --stream s1 s1-1 --one")
		ta.deal(1)
		const n = 30
		for i := 1; i <= n; i++ {
			card := fmt.Sprintf("s1-1.w%d@1", i)
			ta.ok("take --as m1 " + card)
			ta.a.sleep(time.Second)
			head := fmt.Sprintf("%040x", i)
			ta.ok("finish --as m1 " + card + " --head " + head + " --failed --report 'failure " + strconv.Itoa(i) + "'")
			if i < n {
				// --carry resets the bound and the review circuit breaker, so the card
				// keeps opening attempts instead of stopping at four (brief_bound.go)
				ta.ok("rework s1-1 --carry --fix 'fix " + strconv.Itoa(i) + "'")
				ta.deal(1)
			}
		}

		out := ta.ok("card s1-1")
		require.Len(t, cardAttemptLines(out), 20, "the default --max 20 bounds the attempt rows:\n%s", out)
		require.Contains(t, out, "MORE", "a bounded history names the older rows:\n%s", out)
		require.Contains(t, out, "card=s1-1.w30", "the newest attempt is shown:\n%s", out)
		require.NotContains(t, out, " card=s1-1.w1 ", "the oldest attempt is older than the shown 20:\n%s", out)

		all := ta.ok("card s1-1 --max 0")
		require.Len(t, cardAttemptLines(all), n, "--max 0 is every attempt:\n%s", all)
		require.NotContains(t, all, "MORE", "--max 0 cuts nothing:\n%s", all)
		ta.clean()
	})
}
