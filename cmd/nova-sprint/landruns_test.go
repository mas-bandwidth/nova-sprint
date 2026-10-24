package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/pkg/tlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runRow is a tla/RUNS.tsv record for cfg measured on the inputs whose fingerprint is sha
// (with "\t<count>" appended, the count of those inputs; 3 when it is not).
func runRow(cfg, sha, started string) string {
	if !strings.Contains(sha, "\t") {
		sha += "\t3"
	}
	return cfg + "\tMCA.tla\t" + sha + "\tjar1\t21\thost1\t16\t" + started + "\t2\t10\t10\t1.0\t0\tPASS\tpass\t-\t110\tbounded\n"
}

// TestKeepCurrentRunsKeepsTheRecordOnTheMergedInputs: a config a union merge left twice in
// tla/RUNS.tsv keeps the record whose input_sha256 is the case's fingerprint at the merge,
// even when the other is newer; two such records keep the newer started_utc; none is refused,
// as is a fingerprint that cannot be taken; a file with no doubled config is returned as it
// was without a fingerprint being taken.
func TestKeepCurrentRunsKeepsTheRecordOnTheMergedInputs(t *testing.T) {
	t.Parallel()
	older := runRow("MCA.cfg", "fp-x", "2026-10-23T01:00:00Z")
	newer := runRow("MCA.cfg", "fp-y", "2026-10-23T02:00:00Z")
	newerOnX := runRow("MCA.cfg", "fp-x", "2026-10-23T03:00:00Z")
	other := runRow("MCB.cfg", "fp-b", "2026-10-22T00:00:00Z")
	fixed := func(fp string, err error) func(string) (string, error) {
		return func(string) (string, error) { return fp, err }
	}
	for _, tc := range []struct {
		name, merged, fp string
		err              error
		want, errWant    string
	}{
		{"the older record on the merged inputs is kept over the newer stale one",
			runHead + other + older + newer, "fp-x", nil, runHead + other + older, ""},
		{"the newer record on the merged inputs is kept",
			runHead + other + older + newer, "fp-y", nil, runHead + other + newer, ""},
		{"two records on the merged inputs keep the newer started_utc",
			runHead + older + other + newerOnX, "fp-x", nil, runHead + other + newerOnX, ""},
		{"no record on the merged inputs is refused",
			runHead + other + older + newer, "fp-z", nil, "",
			"record tla/RUNS.tsv appears twice for MCA.cfg and no record of it was measured on the merged inputs"},
		{"a fingerprint that cannot be taken is refused",
			runHead + other + older + newer, "", errors.New("no runner"), "",
			"record tla/RUNS.tsv appears twice for MCA.cfg and its fingerprint at the merge cannot be taken: no runner"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, kept, err := keepCurrentRuns([]byte(tc.merged), fixed(tc.fp, tc.err))
			if tc.errWant != "" {
				assert.ErrorContains(t, err, tc.errWant)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(out))
			assert.Equal(t, []string{"MCA.cfg"}, kept)
		})
	}
	t.Run("no doubled config is returned as it was", func(t *testing.T) {
		t.Parallel()
		in := runHead + other + older
		out, kept, err := keepCurrentRuns([]byte(in), func(string) (string, error) {
			t.Fatal("no fingerprint is taken without a doubled config")
			return "", nil
		})
		require.NoError(t, err)
		assert.Equal(t, in, string(out))
		assert.Empty(t, kept)
	})
}

// TestAUnionMergedRunsRecordTwiceLandsTheCurrentOne: in a repository that marks
// tla/RUNS.tsv merge=union, two cards that each re-measure MCA.cfg merge without a conflict
// and the union holds both records. The first card edited MCA.tla, so only its record names
// the merged tree's fingerprint; the second card lands with that record kept and its own,
// newer one dropped. When the second card edited MCA.cfg as well, neither record is on the
// merged inputs and the second card is refused as before.
func TestAUnionMergedRunsRecordTwiceLandsTheCurrentOne(t *testing.T) {
	t.Parallel()
	plan := caseHead + "MCA.cfg\tMCA.tla\tpass\t-\tignore-terminal\tbench\trequired\t-\n"
	module := func(n string) string { return "---- MODULE MCA ----\nEXTENDS Naturals\nx == " + n + "\n====\n" }
	config := func(n string) string { return "INIT Init\nNEXT Next\n\\* " + n + "\n" }
	// fp is MCA.cfg's fingerprint and input count on a tree of these models under the
	// built-in runner, the one a tree with no runner pin is held to.
	fp := func(t *testing.T, mod, cfg string) string {
		root := t.TempDir()
		for name, text := range map[string]string{tlc.CasesFile: plan, "MCA.tla": mod, "MCA.cfg": cfg} {
			require.NoError(t, os.MkdirAll(filepath.Join(root, "tla"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "tla", name), []byte(text), 0o644))
		}
		src, err := tlc.SourceAt(root)
		require.NoError(t, err)
		sum, files, err := src.Fingerprint("MCA.cfg")
		require.NoError(t, err)
		return sum + "\t" + strconv.Itoa(files)
	}
	setup := func(t *testing.T, second map[string]string) (*landRig, int, string, string, string) {
		r := newLandRig(t)
		first := runRow("MCA.cfg", fp(t, module("1"), config("0")), "2026-10-23T01:00:00Z")
		r.git(r.worker, "switch", "-q", "--detach", "origin/main")
		r.files("the models", map[string]string{
			".gitattributes": "tla/RUNS.tsv merge=union\n",
			"tla/CASES.tsv":  plan,
			"tla/MCA.tla":    module("0"),
			"tla/MCA.cfg":    config("0"),
			"tla/RUNS.tsv":   runHead + runRow("MCA.cfg", fp(t, module("0"), config("0")), "2026-10-23T00:00:00Z"),
			"notes.tsv":      "one\n",
		})
		r.git(r.worker, "push", "-q", "origin", "HEAD:refs/heads/main")
		r.git(r.worker, "fetch", "-q", "origin")
		r.ok("add --stream s1 --count 2")
		heads := map[string]string{
			"s1-1": r.card("s1-1", map[string]string{"tla/MCA.tla": module("1"), "tla/RUNS.tsv": runHead + first}),
			"s1-2": r.card("s1-2", second),
		}
		r.queued(heads, "s1-1", "s1-2")
		code, out, errs := r.do("land --repo-dir " + r.clone + " --base main")
		return r, code, out, errs, first
	}

	t.Run("the record on the merged inputs is kept", func(t *testing.T) {
		t.Parallel()
		// the second card re-measured MCA.cfg on the base's models, later than the first
		stale := runRow("MCA.cfg", fp(t, module("0"), config("0")), "2026-10-23T02:00:00Z")
		r, code, out, errs, first := setup(t, map[string]string{"tla/RUNS.tsv": runHead + stale, "notes.tsv": "two\n"})
		require.Equal(t, 0, code, out+errs)
		assert.Contains(t, out, "LAND OK stream=s1 cards=2 base=main")
		assert.Contains(t, out, "record tla/RUNS.tsv: kept the record measured on the merged inputs for MCA.cfg")
		runs := r.git(r.remote, "show", "main:tla/RUNS.tsv") + "\n"
		assert.Equal(t, runHead+first, runs, "one record for MCA.cfg, the one measured on the merged MCA.tla")
	})

	t.Run("no record on the merged inputs is refused", func(t *testing.T) {
		t.Parallel()
		mine := runRow("MCA.cfg", fp(t, module("0"), config("2")), "2026-10-23T02:00:00Z")
		r, code, out, errs, first := setup(t, map[string]string{"tla/MCA.cfg": config("2"), "tla/RUNS.tsv": runHead + mine})
		assert.Equal(t, 1, code, out+errs)
		assert.Contains(t, errs, "record tla/RUNS.tsv appears twice for MCA.cfg and no record of it was measured on the merged inputs")
		assert.Equal(t, map[string]string{"s1-1": "landed/merged", "s1-2": "ready/returned"}, r.places("s1-1", "s1-2"))
		assert.Equal(t, runHead+first, r.git(r.remote, "show", "main:tla/RUNS.tsv")+"\n", "main keeps the first card's record only")
	})
}
