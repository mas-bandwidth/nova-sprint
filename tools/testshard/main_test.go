package main

import (
	"regexp"
	"sort"
	"testing"
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
	if err != nil {
		t.Fatalf("pattern %q is not a valid regexp: %v", pattern, err)
	}
	return ok
}

func TestShardsAreDisjointAndCoverTheList(t *testing.T) {
	patterns := patternsFor(sampleNames, 3)
	for _, name := range sampleNames {
		hits := 0
		for _, p := range patterns {
			if matches(t, p, name) {
				hits++
			}
		}
		if hits != 1 {
			t.Errorf("name %q matched %d shards, want exactly 1", name, hits)
		}
	}
}

func TestShardPatternIndependentOfInputOrder(t *testing.T) {
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
	if got := shardPattern(shuffled, 2, 3); got != ordered {
		t.Errorf("shard 2/3 changed with input order: %q vs %q", got, ordered)
	}
}

func TestNameMatchedByItsShardOnly(t *testing.T) {
	patterns := patternsFor(sampleNames, 3)
	sorted := sortedNames(sampleNames)
	for k := 1; k <= 3; k++ {
		own := patterns[k-1]
		for i, name := range sorted {
			want := i%3 == k-1
			if got := matches(t, own, name); got != want {
				t.Errorf("shard %d/3 %s name %q; want matching=%v", k, map[bool]string{true: "matches", false: "does not match"}[want], name, want)
			}
		}
	}
}

func sortedNames(names []string) []string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	return sorted
}

func TestOnlyTestLinesAreNames(t *testing.T) {
	lines := []string{
		"TestAnActualTest",
		"BenchmarkShouldBeDropped",
		"ExampleShouldBeDropped",
		"FuzzShouldBeDropped",
		"ok  github.com/mas-bandwidth/nova-sprint/cmd/nova-sprint",
		"Test123",
	}
	want := map[string]bool{
		"TestAnActualTest": true,
		"Test123":          true,
	}
	for _, line := range lines {
		if got := isTestName(line); got != want[line] {
			t.Errorf("isTestName(%q) = %v, want %v", line, got, want[line])
		}
	}
}

func TestEmptyShardMatchesNothing(t *testing.T) {
	pattern := shardPattern(sampleNames, 9, 9)
	if pattern == "" {
		t.Fatal("empty shard pattern is blank")
	}
	for _, name := range sampleNames {
		if matches(t, pattern, name) {
			t.Errorf("empty shard pattern %q matched %q", pattern, name)
		}
	}
}
