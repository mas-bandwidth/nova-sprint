package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mas-bandwidth/nova-sprint/internal/sprint"
	"github.com/mas-bandwidth/nova-tools/pkg/testbin"
)

// TestEmptyCommitJudgesACarriedHeadFromTheBase pins checkCard's empty-commit judgment
// when a rework attempt carries an earlier attempt's pushed head unchanged (c.head == c.start):
// diffing c.head against c.start would diff a commit against itself (always empty), so
// checkCard judges emptiness from merge-base(origin/base, head), as on a first attempt.
//
// Replays on a faked git and the in-memory store:
//  1. rocketnet-cookie-key-rotates-on-its-period (head == start == 027b4b5f9e2ee39f2dd15a4a4d3f12df25e49330,
//     base v1.3/comms, non-empty diff from merge-base, claiming result) -> not refused.
//  2. go-table-writers-refuse-what-their-readers-refuse (e4f0f09767bf, head == merge-base,
//     empty diff from the base, claiming result) -> refused with emptyCommitFinding.
//  3. unsigned-range-implies-min-zero (50f2be7f94e4, head == merge-base, empty diff from
//     the base, claiming result) -> refused with emptyCommitFinding.
//  4. A first attempt with an empty diff (start == "") -> refused with emptyCommitFinding.
//  5. A later attempt whose head moved from start (start != "" && start != head) with an
//     empty diff from start -> still judged from start and refused with emptyCommitFinding.
func TestEmptyCommitJudgesACarriedHeadFromTheBase(t *testing.T) {
	const (
		baseRef      = "v1.3/comms"
		baseSHA      = "7af81ba899ca7d9f421d38f7a6c782d12b27c22a"
		cookieHead   = "027b4b5f9e2ee39f2dd15a4a4d3f12df25e49330"
		writersHead  = "e4f0f09767bf"
		unsignedHead = "50f2be7f94e4"
		emptyOnCarry = "9999999999999999999999999999999999999999"
	)

	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
	case "$1" in
		-C|-c) shift 2 ;;
		--*=*) shift ;;
		-*) shift ;;
		*) break ;;
	esac
done
cmd="$1"
shift
case "$cmd" in
	rev-parse)
		printf 'merged-tip\n'
		exit 0
		;;
	ls-tree)
		printf 'internal/rocketnet/cookie.go\ninternal/rocketnet/cookie_test.go\n'
		exit 0
		;;
	merge-base)
		head="$2"
		case "$head" in
			` + writersHead + `|` + unsignedHead + `)
				printf '%s\n' "$head"
				;;
			*)
				printf '%s\n' "` + baseSHA + `"
				;;
		esac
		exit 0
		;;
	diff)
		if [ "$1" = "--quiet" ]; then
			start="$2"
			head="$3"
			if [ "$start" = "$head" ]; then
				exit 0
			fi
			if [ "$head" = "` + cookieHead + `" ] && [ "$start" = "` + baseSHA + `" ]; then
				exit 1
			fi
			exit 0
		fi
		printf 'diff --git a/internal/rocketnet/cookie.go b/internal/rocketnet/cookie.go\n--- a/internal/rocketnet/cookie.go\n+++ b/internal/rocketnet/cookie.go\n@@ -1 +1,2 @@\n package rocketnet\n+// rotated\n'
		exit 0
		;;
	reset)
		exit 0
		;;
	*)
		exit 1
		;;
esac
`
	require.NoError(t, testbin.WriteExecutable(filepath.Join(bin, "git"), []byte(script), 0o755))

	newFakeLander := func(t *testing.T, ta *testApp) *lander {
		t.Helper()
		ta.a.gitEnv = []string{
			"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
			"GIT_CONFIG_GLOBAL=" + os.DevNull,
			"GIT_CONFIG_NOSYSTEM=1",
		}
		return &lander{
			a:      ta.a,
			base:   baseRef,
			diffs:  map[string]string{},
			scope:  map[string][]string{},
			prose:  map[string][]string{},
			trunks: map[string]string{},
		}
	}

	t.Run("replayed rocketnet-cookie carried head changes the base and is not refused", func(t *testing.T) {
		ta := newTestApp(t)
		ta.ok("init --readers reader-a,reader-b --members m1")
		brief := writeNeedsBrief(t, t.TempDir(), "rocketnet-cookie-key-rotates-on-its-period",
			"Rotate the cookie key. tier: flash\nBASE: "+baseRef+"\nPATHS: internal/rocketnet/cookie.go, internal/rocketnet/cookie_test.go", "")
		ta.ok("add --stream s1 rocketnet-cookie-key-rotates-on-its-period --one --brief-file " + brief)

		// Attempt .w1 pushes cookieHead, then is reworked from review.
		ta.deal(1)
		ta.ok("take --as m1 --limit 100")
		ta.ok("finish --as m1 rocketnet-cookie-key-rotates-on-its-period.w1@1 --head " + cookieHead + " --report 'verdict: ok\n2 files changed, 97 insertions(+)'")
		ta.ok("ask")
		ta.ok("rework rocketnet-cookie-key-rotates-on-its-period --fix 'carry the pushed head'")

		// Attempt .w2 carries cookieHead unchanged and is accepted by two readers.
		ta.deal(1)
		ta.ok("take --as m1 --limit 100")
		ta.ok("finish --as m1 rocketnet-cookie-key-rotates-on-its-period.w2@1 --head " + cookieHead + " --report 'verdict: ok\n2 files changed, 97 insertions(+)'")
		ta.ok("ask")
		ta.ok("read --as reader-a --ok --limit 100")
		ta.ok("read --as reader-b --ok --limit 100")
		ta.ok("accept --read-ok")

		st, err := ta.a.store(common{redis: "mem:0", actor: "coordinator"})
		require.NoError(t, err)
		s, err := st.Load(t.Context(), []string{sprint.Work, sprint.Merge, sprint.Fleet, sprint.Readers}, nil)
		require.NoError(t, err)

		l := newFakeLander(t, ta)
		cards, ok := l.openStream(s, "s1")
		require.True(t, ok)
		require.Len(t, cards, 1)
		require.Equal(t, cookieHead, cards[0].head)
		require.Equal(t, cookieHead, cards[0].start, "sprint.BaseOf records the carried head as start")
		require.NotEmpty(t, checkEmptyCommit(true, cards[0].result), "the recorded result claims changes")

		cardWhy, envWhy, _ := l.checkCard(t.Context(), t.TempDir(), "s1", cards[0], baseSHA)
		assert.Empty(t, envWhy)
		assert.Empty(t, cardWhy, "a carried head that changes the base must not be refused as an empty commit")
	})

	t.Run("replayed go-table-writers and unsigned-range true empty landings are refused", func(t *testing.T) {
		for _, tc := range []struct {
			id    string
			head  string
			start string
		}{
			{
				id:    "go-table-writers-refuse-what-their-readers-refuse",
				head:  writersHead,
				start: writersHead,
			},
			{
				id:    "go-table-writers-refuse-what-their-readers-refuse",
				head:  writersHead,
				start: "",
			},
			{
				id:    "unsigned-range-implies-min-zero",
				head:  unsignedHead,
				start: unsignedHead,
			},
			{
				id:    "unsigned-range-implies-min-zero",
				head:  unsignedHead,
				start: "",
			},
		} {
			name := tc.id + "/start=" + tc.start
			t.Run(name, func(t *testing.T) {
				ta := newTestApp(t)
				l := newFakeLander(t, ta)
				c := landCard{
					id:     tc.id,
					base:   baseRef,
					head:   tc.head,
					start:  tc.start,
					paths:  []string{"internal/rocketnet/cookie.go"},
					result: "verdict: ok\nhead: " + tc.head + "\n1 file changed, 5 insertions(+)\n",
				}
				cardWhy, envWhy, _ := l.checkCard(t.Context(), t.TempDir(), "s1", c, baseSHA)
				assert.Empty(t, envWhy)
				assert.Equal(t, emptyCommitFinding, cardWhy)
			})
		}
	})

	t.Run("first attempt with an empty diff is refused and an empty commit on top of start is still refused", func(t *testing.T) {
		ta := newTestApp(t)
		l := newFakeLander(t, ta)

		firstEmpty := landCard{
			id:     "first-attempt-empty",
			base:   baseRef,
			head:   emptyOnCarry,
			start:  "",
			paths:  []string{"internal/rocketnet/cookie.go"},
			result: "verdict: ok\nhead: " + strings.Repeat("9", 12) + "\n1 file changed, 2 insertions(+)\n",
		}
		cardWhy, envWhy, _ := l.checkCard(t.Context(), t.TempDir(), "s1", firstEmpty, baseSHA)
		assert.Empty(t, envWhy)
		assert.Equal(t, emptyCommitFinding, cardWhy)

		advancedEmpty := landCard{
			id:     "carried-then-empty-commit",
			base:   baseRef,
			head:   emptyOnCarry,
			start:  cookieHead,
			paths:  []string{"internal/rocketnet/cookie.go"},
			result: "verdict: ok\nhead: " + strings.Repeat("9", 12) + "\n1 file changed, 2 insertions(+)\n",
		}
		cardWhy, envWhy, _ = l.checkCard(t.Context(), t.TempDir(), "s1", advancedEmpty, baseSHA)
		assert.Empty(t, envWhy)
		assert.Equal(t, emptyCommitFinding, cardWhy, "when head != start, emptiness is still judged from start")
	})
}
