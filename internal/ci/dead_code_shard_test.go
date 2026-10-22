package ci

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The dead code rule's GOOS shards (dead_code_test.go). One GOOS's judged dead
// functions (dead under every tag set, minus the packages a skipped root leaves
// unjudged) depend on nothing measured for another GOOS, and the rule's
// per-package set is their union, so the measurement splits by GOOS exactly:
// CI runs one functional leg per GOOS, each writing its shard, and the
// deadcode-ledger job unions the shards and holds the union to the ledger,
// the same comparison a whole run makes. Nothing is dropped: the join refuses
// unless every GOOS's shard is present and names its own GOOS.
//
// deadCodeGOOS lives here, untagged, so the workflow pins (unit tier) and the
// rule (functional tier) read one list.
var deadCodeGOOS = []string{"linux", "darwin", "windows"}

// deadCodeShard is one GOOS's measurement as a shard file holds it.
type deadCodeShard struct {
	GOOS string   `json:"goos"`
	Dead []string `json:"dead"` // sorted "<package> <func>" keys
}

// encodeDeadCodeShard renders goos's judged dead keys as a shard file.
func encodeDeadCodeShard(goos string, dead map[string]bool) ([]byte, error) {
	s := deadCodeShard{GOOS: goos, Dead: []string{}}
	for k := range dead {
		s.Dead = append(s.Dead, k)
	}
	sort.Strings(s.Dead)
	return json.MarshalIndent(s, "", "  ")
}

// unionDeadCodeShards unions the shard files, one per GOOS in deadCodeGOOS,
// keyed by the GOOS whose file it is. A missing shard, a shard naming another
// GOOS, a shard for a GOOS the rule does not measure, or a malformed key is
// refused: the union must be the whole run's.
func unionDeadCodeShards(files map[string][]byte) (map[string]bool, error) {
	want := map[string]bool{}
	for _, goos := range deadCodeGOOS {
		want[goos] = true
	}
	for goos := range files {
		if !want[goos] {
			return nil, fmt.Errorf("dead code shard for GOOS=%s: the rule measures only %s", goos, strings.Join(deadCodeGOOS, ", "))
		}
	}
	union := map[string]bool{}
	for _, goos := range deadCodeGOOS {
		raw, ok := files[goos]
		if !ok {
			return nil, fmt.Errorf("dead code shard for GOOS=%s is missing: every GOOS's leg must write its shard", goos)
		}
		var s deadCodeShard
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("dead code shard for GOOS=%s: %w", goos, err)
		}
		if s.GOOS != goos {
			return nil, fmt.Errorf("dead code shard for GOOS=%s names GOOS=%q", goos, s.GOOS)
		}
		for _, k := range s.Dead {
			if pkg, name, ok := strings.Cut(k, " "); !ok || pkg == "" || name == "" {
				return nil, fmt.Errorf("dead code shard for GOOS=%s: %q is not `<package> <func>`", goos, k)
			}
			union[k] = true
		}
	}
	return union, nil
}

// deadByPackage groups "<package> <func>" keys into each package's sorted names.
func deadByPackage(keys map[string]bool) map[string][]string {
	byPkg := map[string][]string{}
	for k := range keys {
		pkg, name, _ := strings.Cut(k, " ")
		byPkg[pkg] = append(byPkg[pkg], name)
	}
	for _, names := range byPkg {
		sort.Strings(names)
	}
	return byPkg
}

// TestDeadCodeShardsUnionToTheWholeRun pins the join: the union of the GOOS
// shards is the whole run's set, and a missing, foreign, mislabelled or
// malformed shard is refused rather than unioned short.
func TestDeadCodeShardsUnionToTheWholeRun(t *testing.T) {
	t.Parallel()
	per := map[string]map[string]bool{
		"linux":   {"pkg/a f": true, "pkg/b g": true},
		"darwin":  {"pkg/a f": true, "pkg/a h": true},
		"windows": {},
	}
	files := map[string][]byte{}
	for goos, dead := range per {
		raw, err := encodeDeadCodeShard(goos, dead)
		require.NoError(t, err)
		files[goos] = raw
	}
	union, err := unionDeadCodeShards(files)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"pkg/a f": true, "pkg/a h": true, "pkg/b g": true}, union)
	assert.Equal(t, map[string][]string{"pkg/a": {"f", "h"}, "pkg/b": {"g"}}, deadByPackage(union))

	missing := map[string][]byte{"linux": files["linux"], "darwin": files["darwin"]}
	_, err = unionDeadCodeShards(missing)
	assert.ErrorContains(t, err, "GOOS=windows is missing")

	foreign := map[string][]byte{"plan9": []byte(`{"goos":"plan9","dead":[]}`)}
	for k, v := range files {
		foreign[k] = v
	}
	_, err = unionDeadCodeShards(foreign)
	assert.ErrorContains(t, err, "GOOS=plan9")

	swapped := map[string][]byte{"linux": files["darwin"], "darwin": files["darwin"], "windows": files["windows"]}
	_, err = unionDeadCodeShards(swapped)
	assert.ErrorContains(t, err, `GOOS=linux names GOOS="darwin"`)

	bad := map[string][]byte{"linux": []byte(`{"goos":"linux","dead":["nofunc"]}`), "darwin": files["darwin"], "windows": files["windows"]}
	_, err = unionDeadCodeShards(bad)
	assert.ErrorContains(t, err, "is not `<package> <func>`")
}
