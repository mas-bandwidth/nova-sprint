package swarm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The build-jobs rule on known cases: a build parallelism above four, or one computed from
// the machine, is refused on its line; -j4 and less, a bare -j, and java -jar are not.
func TestABriefBuildsAtFourJobsOrLess(t *testing.T) {
	t.Parallel()
	for _, line := range []string{"STEP 2. make -j16", "cmake --build build -j 8", "ctest -j$(nproc)", "make -j`nproc` all", "make --jobs=12", "run `make -j32`", "make -j $(getconf _NPROCESSORS_ONLN)"} {
		fs := LintBuildJobs([]byte("tier: pro\nPATHS: x.go\n\nTHE TASK. build it.\n" + line + "\n"))
		require.Len(t, fs, 1, line)
		assert.Equal(t, BuildJobsCheck, fs[0].Check)
		assert.Equal(t, 5, fs[0].Line, line)
		assert.Contains(t, fs[0].Excerpt, "-j4 or less")
	}
	for _, line := range []string{"STEP 2. make -j4", "make -j 2 && ctest -j4", "make -j", "java -jar /opt/tla/tla2tools.jar", "go test -p 4 ./...", "see PR-j16-notes"} {
		assert.Empty(t, LintBuildJobs([]byte("tier: pro\n\nTHE TASK.\n"+line+"\n")), line)
	}
	assert.Equal(t, BuildJobsRemedy, ChildRemedy(nil, BuildJobsCheck), "the check names its remedy")
}

// NormalizeBuildJobs reads every -jN as one, so a -jN change alone is an operational edit.
func TestNormalizeBuildJobsReadsEveryCountAsOne(t *testing.T) {
	t.Parallel()
	assert.Equal(t, NormalizeBuildJobs("make -j16 && ctest -j 8"), NormalizeBuildJobs("make -j4 && ctest -j4"))
	assert.Equal(t, NormalizeBuildJobs("make -j$(nproc)"), NormalizeBuildJobs("make -j4"))
	assert.NotEqual(t, NormalizeBuildJobs("make -j4"), NormalizeBuildJobs("make -j4 install"))
}
