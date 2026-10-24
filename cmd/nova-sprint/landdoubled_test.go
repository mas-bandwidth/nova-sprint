package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coverRow is a tla/COVERAGE.tsv row for machine with note.
func coverRow(machine, note string) string {
	return machine + "\tinternal/x.go\tX\tcurrent\t-\t" + note + "\n"
}

// TestKeepOneDoubledKeyKeepsTheSideThatChangedIt: a machine a union merge left twice in
// tla/COVERAGE.tsv keeps one row: byte-identical rows are one; a key only the card changed
// keeps the card's row, one only the tip changed the tip's (the two cards that each changed an
// adjacent row, whose union hunk doubles both); a key both sides changed differently is
// refused, as is a doubled row no side wrote; a file with no doubled key is returned as it was.
func TestKeepOneDoubledKeyKeepsTheSideThatChangedIt(t *testing.T) {
	t.Parallel()
	x0, x1, x2 := coverRow("rn-endpoint", "base"), coverRow("rn-endpoint", "tip"), coverRow("rn-endpoint", "card")
	y0, y1 := coverRow("io-uring-driver", "base"), coverRow("io-uring-driver", "card")
	z := coverRow("other", "same")
	base := coverageHeader + z + x0 + y0
	for _, tc := range []struct {
		name, merged, tip, card string
		want, errWant           string
		kept                    []string
	}{
		{"byte-identical rows are one row",
			coverageHeader + z + x1 + x1 + y0, base, base, coverageHeader + z + x1 + y0, "", []string{"rn-endpoint"}},
		{"a key only the card changed keeps the card's row",
			coverageHeader + z + x0 + x2 + y0, base, coverageHeader + z + x2 + y0, coverageHeader + z + x2 + y0, "", []string{"rn-endpoint"}},
		{"adjacent rows each side changed keep each side's change",
			coverageHeader + z + x1 + y0 + x0 + y1, coverageHeader + z + x1 + y0, coverageHeader + z + x0 + y1,
			coverageHeader + z + x1 + y1, "", []string{"rn-endpoint", "io-uring-driver"}},
		{"a key both sides changed alike is one row",
			coverageHeader + z + x1 + x0 + x1 + y0, coverageHeader + z + x1 + y0, coverageHeader + z + x1 + y0,
			coverageHeader + z + x1 + y0, "", []string{"rn-endpoint"}},
		{"a key both sides changed differently is refused",
			coverageHeader + z + x1 + x2 + y0, coverageHeader + z + x1 + y0, coverageHeader + z + x2 + y0, "",
			"record tla/COVERAGE.tsv appears twice for rn-endpoint and both sides changed the record for rn-endpoint", nil},
		{"a doubled row neither side wrote is refused",
			coverageHeader + z + x0 + coverRow("rn-endpoint", "stray") + y0, base, base, "",
			"record tla/COVERAGE.tsv appears twice for rn-endpoint and neither side changed the record", nil},
		{"a card's row that is not among the doubled rows is refused",
			coverageHeader + z + x0 + x1 + y0, base, coverageHeader + z + x2 + y0, "",
			"record tla/COVERAGE.tsv appears twice for rn-endpoint and neither row is the one its side wrote", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, kept, err := keepOneDoubledKey(coverageFile, []byte(tc.merged), []byte(base), []byte(tc.tip), []byte(tc.card))
			if tc.errWant != "" {
				assert.ErrorContains(t, err, tc.errWant)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(out))
			assert.Equal(t, tc.kept, kept)
		})
	}
	t.Run("no doubled key is returned as it was", func(t *testing.T) {
		t.Parallel()
		out, kept, err := keepOneDoubledKey(coverageFile, []byte(base), nil, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, base, string(out))
		assert.Empty(t, kept)
	})
}

// TestAUnionMergedCoverageRowTwiceLandsTheChangedOne: in a repository that marks
// tla/COVERAGE.tsv merge=union, two cards that each change one of two adjacent machine rows
// merge without a conflict and the union holds both machines twice; the second card lands with
// each machine's changed row kept. Two cards that change the same machine's row differently
// are refused as before, naming the machine.
func TestAUnionMergedCoverageRowTwiceLandsTheChangedOne(t *testing.T) {
	t.Parallel()
	x0, x1, x2 := coverRow("rn-endpoint", "base"), coverRow("rn-endpoint", "first"), coverRow("rn-endpoint", "second")
	y0, y1 := coverRow("io-uring-driver", "base"), coverRow("io-uring-driver", "second")
	z := coverRow("other", "same")
	setup := func(t *testing.T, second string) (*landRig, int, string, string) {
		r := newLandRig(t)
		r.git(r.worker, "switch", "-q", "--detach", "origin/main")
		r.files("the coverage", map[string]string{
			".gitattributes":   "tla/COVERAGE.tsv merge=union\n",
			"tla/COVERAGE.tsv": coverageHeader + z + x0 + y0,
			"notes.tsv":        "one\n",
		})
		r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
		r.git(r.worker, "fetch", "-q", "origin")
		r.ok("add --stream s1 --count 2")
		heads := map[string]string{
			"s1-1": r.card("s1-1", map[string]string{"tla/COVERAGE.tsv": coverageHeader + z + x1 + y0}),
			"s1-2": r.card("s1-2", map[string]string{"tla/COVERAGE.tsv": second, "notes.tsv": "two\n"}),
		}
		r.queued(heads, "s1-1", "s1-2")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		return r, code, out, errs
	}

	t.Run("each machine keeps the row of the side that changed it", func(t *testing.T) {
		t.Parallel()
		r, code, out, errs := setup(t, coverageHeader+z+x0+y1)
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
		assert.Contains(t, out, "record tla/COVERAGE.tsv: kept one row, the side that changed it, for rn-endpoint, io-uring-driver")
		assert.Equal(t, coverageHeader+z+x1+y1, r.git(r.remote, "show", "main:tla/COVERAGE.tsv")+"\n")
	})

	t.Run("one machine both cards changed differently is refused", func(t *testing.T) {
		t.Parallel()
		r, code, out, errs := setup(t, coverageHeader+z+x2+y0)
		assert.Equal(t, 1, code, out+errs)
		assert.Contains(t, errs, "record tla/COVERAGE.tsv appears twice for rn-endpoint and both sides changed the record for rn-endpoint")
		assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "ready/returned"}, r.places("s1-1", "s1-2"))
		assert.Equal(t, coverageHeader+z+x1+y0, r.git(r.remote, "show", "main:tla/COVERAGE.tsv")+"\n", "main keeps the first card's row only")
	})
}
