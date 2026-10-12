package ci

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTheFunctionalJobIsAMatrixOfRestAndShards pins .github/workflows/ci.yml:
// the functional job is a matrix with a rest leg and SHARDS shard legs, each k
// leg running make functional-gate-shard SHARD=k SHARDS=3 and the rest leg make
// functional-gate-rest, and the SHARDS value matching the number of shard legs.
func TestTheFunctionalJobIsAMatrixOfRestAndShards(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/ci.yml")
	require.NoError(t, err)
	jobs := workflowJobs(t, string(b))
	body := jobs["functional"]
	require.NotEmpty(t, body, "ci.yml names no functional job")
	require.Contains(t, body, "strategy:", "the functional job is not a matrix")
	require.Contains(t, body, "matrix:", "the functional job has no matrix")

	legs := matrixLegs(t, body)
	require.Contains(t, legs, "rest", "the matrix has no rest leg")
	shardLegs := []string{}
	for _, leg := range legs {
		if strings.HasSuffix(leg, "/3") {
			shardLegs = append(shardLegs, leg)
		}
	}
	require.NotEmpty(t, shardLegs, "the matrix has no shard legs")

	require.Contains(t, body, "make functional-gate-rest", "the rest leg does not run make functional-gate-rest")
	require.Contains(t, body, "make functional-gate-shard", "the shard legs do not run make functional-gate-shard")
	for _, leg := range shardLegs {
		k := strings.TrimSuffix(leg, "/3")
		require.Contains(t, body, "make functional-gate-shard SHARD="+k+" SHARDS=3",
			"shard leg %q does not run the right SHARD/SHARDS", leg)
	}

	shards := shardsIn(t, body)
	require.Equal(t, len(shardLegs), shards, "SHARDS in ci.yml does not equal the number of k legs")
	require.GreaterOrEqual(t, shards, 1)
}

// TestTheFunctionalJobEveryLegCarriesTheTwoMinuteTimeout pins that every leg's
// command carries -timeout 120s through the Makefile (functional-gate-rest and
// functional-gate-shard) and none a larger one.
func TestTheFunctionalJobEveryLegCarriesTheTwoMinuteTimeout(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	require.NoError(t, err)
	for _, target := range []string{"functional-gate-rest", "functional-gate-shard"} {
		recipe := makeRecipes(t, string(makefile), target)
		require.NotEmpty(t, recipe, "target %q has no recipe", target)
		require.Contains(t, recipe, "-timeout 120s", "target %q does not bound at -timeout 120s", target)
		timeouts := timeoutArgs(recipe)
		require.NotEmpty(t, timeouts, "target %q carries no -timeout", target)
		for _, v := range timeouts {
			require.Equal(t, "120s", v, "target %q carries -timeout %s, want 120s and none larger", target, v)
		}
	}
}

// TestTheFunctionalJobRestLegExcludesCmdNovaSprint pins that the rest leg runs
// every package of ./... but exactly ./cmd/nova-sprint: the functional-gate-rest
// recipe expands the FUNCTIONAL_REST list, which filters the cmd/nova-sprint import path (%/cmd/nova-sprint; go list prints full import paths) out
// of `go list ./...`, and never runs the whole ./... tree.
func TestTheFunctionalJobRestLegExcludesCmdNovaSprint(t *testing.T) {
	makefile, err := os.ReadFile("../../Makefile")
	require.NoError(t, err)
	text := string(makefile)
	require.Contains(t, text, "FUNCTIONAL_REST := $(filter-out %/cmd/nova-sprint,$(shell $(GO) list ./...))",
		"FUNCTIONAL_REST does not filter the cmd/nova-sprint import path out of go list ./... (go list prints full import paths, so the pattern needs the % prefix)")
	recipe := makeRecipes(t, text, "functional-gate-rest")
	require.Contains(t, recipe, "$(FUNCTIONAL_REST)", "the rest recipe does not run the FUNCTIONAL_REST list")
	require.NotContains(t, recipe, "./...", "the rest recipe runs the whole ./... tree")
	require.NotContains(t, recipe, "./cmd/nova-sprint", "the rest recipe names ./cmd/nova-sprint")
	whole := makeRecipes(t, text, "functional-gate")
	require.Contains(t, whole, "./...", "functional-gate no longer runs the whole ./... tree")
}

// matrixLegs returns the matrix's leg values of a job body.
func matrixLegs(t *testing.T, body string) []string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "leg:") {
			continue
		}
		open := strings.Index(trimmed, "[")
		close := strings.Index(trimmed, "]")
		if open < 0 || close < open {
			t.Fatalf("matrix leg line %q is not a list", trimmed)
		}
		inside := trimmed[open+1 : close]
		var legs []string
		for _, item := range strings.Split(inside, ",") {
			legs = append(legs, strings.Trim(strings.TrimSpace(item), `"`))
		}
		return legs
	}
	t.Fatal("no matrix leg line found")
	return nil
}

// shardsIn returns the SHARDS value the job's shard legs run with.
func shardsIn(t *testing.T, body string) int {
	t.Helper()
	re := regexp.MustCompile(`SHARDS=(\d+)`)
	matches := re.FindAllStringSubmatch(body, -1)
	require.NotEmpty(t, matches, "no SHARDS=k found in the functional job")
	value := -1
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		require.NoError(t, err, "non-numeric SHARDS %q", m[1])
		if value == -1 {
			value = n
		} else {
			require.Equal(t, value, n, "SHARDS differs across legs")
		}
	}
	return value
}

// timeoutArgs returns every value the -timeout flag takes in a recipe.
func timeoutArgs(recipe string) []string {
	var out []string
	re := regexp.MustCompile(`-timeout\s+(\S+)`)
	for _, m := range re.FindAllStringSubmatch(recipe, -1) {
		out = append(out, m[1])
	}
	return out
}

// makeRecipes returns the tab-indented recipe lines of a Makefile target.
func makeRecipes(t *testing.T, makefile, target string) string {
	t.Helper()
	lines := strings.Split(makefile, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == target+":" {
			var sb strings.Builder
			for _, r := range lines[i+1:] {
				if r != "" && !strings.HasPrefix(r, "\t") {
					break
				}
				sb.WriteString(r)
				sb.WriteByte('\n')
			}
			return sb.String()
		}
	}
	t.Fatalf("target %q not found in the Makefile", target)
	return ""
}
