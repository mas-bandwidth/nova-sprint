package main

import (
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var sampleNames = []string{
	"TestZebra",
	"TestApple",
	"TestMango",
	"TestBanana",
	"TestCherry",
	"TestDate",
	"TestElderberry",
}

func patternsFor(names []string, n int) []string {
	patterns := make([]string, n)
	for k := 1; k <= n; k++ {
		patterns[k-1] = shardPattern(names, k, n)
	}
	return patterns
}

func matches(t *testing.T, pattern, name string) bool {
	t.Helper()
	ok, err := regexp.MatchString(pattern, name)
	require.NoErrorf(t, err, "pattern %q is not a valid regexp", pattern)
	return ok
}

func TestShardsAreDisjointAndCoverTheList(t *testing.T) {
	t.Parallel()
	patterns := patternsFor(sampleNames, 3)
	for _, name := range sampleNames {
		hits := 0
		for _, p := range patterns {
			if matches(t, p, name) {
				hits++
			}
		}
		assert.Equalf(t, 1, hits, "name %q matched %d shards, want exactly 1", name, hits)
	}
}

func TestShardPatternIndependentOfInputOrder(t *testing.T) {
	t.Parallel()
	ordered := shardPattern(sampleNames, 2, 3)
	shuffled := []string{
		"TestCherry",
		"TestApple",
		"TestZebra",
		"TestDate",
		"TestMango",
		"TestElderberry",
		"TestBanana",
	}
	assert.Equalf(t, ordered, shardPattern(shuffled, 2, 3), "shard 2/3 changed with input order")
}

func TestNameMatchedByItsShardOnly(t *testing.T) {
	t.Parallel()
	patterns := patternsFor(sampleNames, 3)
	sorted := sortedNames(sampleNames)
	for k := 1; k <= 3; k++ {
		for i, name := range sorted {
			want := i%3 == k-1
			assert.Equalf(t, want, matches(t, patterns[k-1], name), "shard %d/3 name %q", k, name)
		}
	}
}

func sortedNames(names []string) []string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return sorted
}

func TestOnlyTestLinesAreNames(t *testing.T) {
	t.Parallel()
	want := map[string]bool{
		"TestAnActualTest": true,
		"Test123":          true,
	}
	for _, line := range []string{
		"TestAnActualTest",
		"BenchmarkShouldBeDropped",
		"ExampleShouldBeDropped",
		"FuzzShouldBeDropped",
		"ok  github.com/mas-bandwidth/nova-sprint/cmd/nova-sprint",
		"Test123",
	} {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			assert.Equalf(t, want[line], isTestName(line), "isTestName(%q)", line)
		})
	}
}

func TestEmptyShardMatchesNothing(t *testing.T) {
	t.Parallel()
	pattern := shardPattern(sampleNames, 9, 9)
	require.NotEmpty(t, pattern, "empty shard pattern is blank")
	for _, name := range sampleNames {
		assert.Falsef(t, matches(t, pattern, name), "empty shard pattern %q matched %q", pattern, name)
	}
}
