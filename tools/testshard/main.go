// testshard reads the lines of `go test -list` on stdin, keeps the lines that
// are a Go test name (so the trailing "ok <pkg>" line, and Benchmark/Example/
// Fuzz lines, are dropped), sorts them, and prints one anchored -run pattern
// ^(A|B|...)$ of every name whose index in the sorted list is shard-1 mod of.
// A shard that has no names prints a pattern that matches nothing it could be
// confused with (a test name always starts with "Test").
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var testNameRe = regexp.MustCompile(`^Test[A-Za-z0-9_]*$`)

// isTestName reports whether line is a Go test name (a Test* function), not a
// Benchmark/Example/Fuzz line and not the trailing "ok <pkg>" line.
func isTestName(line string) bool {
	return testNameRe.MatchString(line)
}

// shardPattern returns the anchored -run pattern for the shard of `of` shards
// that holds every sorted name whose index is shard-1 mod of. A shard with no
// names gets a pattern that matches nothing.
func shardPattern(names []string, shard, of int) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	var chosen []string
	for i, name := range sorted {
		if i%of == shard-1 {
			chosen = append(chosen, name)
		}
	}
	if len(chosen) == 0 {
		return "^$"
	}
	return "^(" + strings.Join(chosen, "|") + ")$"
}

func main() {
	shard := flag.Int("shard", 0, "this shard's number k (1 <= k <= n)")
	of := flag.Int("of", 0, "the number of shards n")
	flag.Parse()
	if *shard < 1 || *of < *shard {
		fmt.Fprintln(os.Stderr, "testshard: --shard k --of n with 1 <= k <= n")
		os.Exit(2)
	}
	var names []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); isTestName(line) {
			names = append(names, line)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "testshard:", err)
		os.Exit(1)
	}
	fmt.Println(shardPattern(names, *shard, *of))
}
