package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/nova-sprint/internal/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleRoadmapCLI = `; sample roadmap
(:roadmap 1 :repo "mas-bandwidth/nova-sprint" :product "nova-sprint"
 :releases
 ((:release "v1.1.0" :status :planned :cards 2
   :streams
   ((:stream "stream-s" :cards
     ((:id "c-1" :tier "-" :needs ()
       :title "Title 1"
       :brief "Brief 1 text")
      (:id "c-2" :tier "1" :needs ("c-1")
       :title "Title 2"
       :brief "Brief 2 text")))))))
`

func writeRoadmap(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(text), 0644))
	return path
}

func TestRoadmapCheck(t *testing.T) {
	t.Parallel()
	path := writeRoadmap(t, sampleRoadmapCLI)
	m := workMain(unreachable(t))

	res := m.Run("roadmap", "check", "--file", path)
	require.Equal(t, 0, res.Code, res.Stderr)
	assert.Contains(t, res.Stdout, "ROADMAP-CHECK OK")
	assert.Contains(t, res.Stdout, "cards=2")

	resJSON := m.Run("roadmap", "check", "--file", path, "--json")
	require.Equal(t, 0, resJSON.Code)
	var j map[string]any
	require.NoError(t, json.Unmarshal([]byte(resJSON.Stdout), &j))
	assert.Equal(t, "ok", j["result"].(map[string]any)["status"])
	assert.Equal(t, "roadmap check", j["result"].(map[string]any)["verb"])

	mismatched := strings.Replace(sampleRoadmapCLI, ":cards 2", ":cards 5", 1)
	require.NoError(t, os.WriteFile(path, []byte(mismatched), 0644))
	resFail := m.Run("roadmap", "check", "--file", path)
	require.Equal(t, 1, resFail.Code)
	assert.Contains(t, resFail.Stderr, "ROADMAP-CHECK FAILED")
	assert.Contains(t, resFail.Stderr, "count mismatch")
}

// An unbalanced file is a problem check names, exit 1, as its help says.
func TestRoadmapCheckNamesAnUnbalancedFileAtExit1(t *testing.T) {
	t.Parallel()
	path := writeRoadmap(t, strings.TrimSuffix(sampleRoadmapCLI, ")\n")+"\n")
	res := workMain(unreachable(t)).Run("roadmap", "check", "--file", path)
	require.Equal(t, 1, res.Code, res.Stderr)
	assert.Contains(t, res.Stderr, "ROADMAP-CHECK FAILED")
	help := workMain(unreachable(t)).Run("roadmap", "check", "-h")
	assert.Contains(t, help.Stdout, "1 check found problems (unbalanced or unparsable")
}

func TestRoadmapRemove(t *testing.T) {
	t.Parallel()
	path := writeRoadmap(t, sampleRoadmapCLI)
	m := workMain(unreachable(t))

	resRefuse := m.Run("roadmap", "remove", "--file", path)
	require.Equal(t, 2, resRefuse.Code)
	assert.Contains(t, resRefuse.Stderr, "ROADMAP-REMOVE REFUSED: --id is required")

	resMissing := m.Run("roadmap", "remove", "--file", path, "--id", "c-99")
	require.Equal(t, 2, resMissing.Code)
	assert.Contains(t, resMissing.Stderr, `card "c-99" not found`)
	assert.Equal(t, sampleRoadmapCLI, testkit.ReadFile(t, path))

	res := m.Run("roadmap", "remove", "--file", path, "--id", "c-1")
	require.Equal(t, 0, res.Code, res.Stderr)
	assert.Contains(t, res.Stdout, "ROADMAP-REMOVE OK")
	assert.Contains(t, res.Stdout, "removed=1 remaining=1")

	require.Equal(t, 0, m.Run("roadmap", "check", "--file", path).Code)
}

func TestRoadmapPull(t *testing.T) {
	t.Parallel()
	path := writeRoadmap(t, sampleRoadmapCLI)
	m := workMain(unreachable(t))

	outDir := filepath.Join(t.TempDir(), "briefs")
	res := m.Run("roadmap", "pull", "--file", path, "--id", "c-1", "--out", outDir)
	require.Equal(t, 0, res.Code, res.Stderr)
	assert.Contains(t, res.Stdout, "ROADMAP-PULL OK")
	assert.Contains(t, res.Stdout, "pulled=1")
	assert.Contains(t, res.Stdout, "ROADMAP-PULL CARD id=c-1 release=v1.1.0 stream=stream-s tier=-")

	assert.Equal(t, "Brief 1 text\n", testkit.ReadFile(t, filepath.Join(outDir, "c-1.md")))

	resCheck := m.Run("roadmap", "check", "--file", path)
	require.Equal(t, 0, resCheck.Code)
	assert.Contains(t, resCheck.Stdout, "cards=1")
}

func TestRoadmapPullRefusesAnEmptyBrief(t *testing.T) {
	t.Parallel()
	text := strings.Replace(sampleRoadmapCLI, `"Brief 1 text"`, `""`, 1)
	path := writeRoadmap(t, text)
	outDir := filepath.Join(t.TempDir(), "briefs")
	res := workMain(unreachable(t)).Run("roadmap", "pull", "--file", path, "--id", "c-1", "--out", outDir)
	require.Equal(t, 2, res.Code)
	assert.Contains(t, res.Stderr, "ROADMAP-PULL REFUSED")
	assert.Contains(t, res.Stderr, "empty :brief")
	assert.NoDirExists(t, outDir)
	assert.Equal(t, text, testkit.ReadFile(t, path))
}

func TestRoadmapAdd(t *testing.T) {
	t.Parallel()
	path := writeRoadmap(t, sampleRoadmapCLI)
	briefDir := t.TempDir()
	b := `REPO: mas-bandwidth/nova-tools
TIER: pro
DEPENDS-ON: c-1

THE TASK. New brief title.
`
	require.NoError(t, os.WriteFile(filepath.Join(briefDir, "c-3.md"), []byte(b), 0644))
	m := workMain(unreachable(t))

	res := m.Run("roadmap", "add", "--file", path, "--stream", "stream-s", "--brief-dir", briefDir)
	require.Equal(t, 0, res.Code, res.Stderr)
	assert.Contains(t, res.Stdout, "ROADMAP-ADD OK")
	assert.Contains(t, res.Stdout, "added=1 total=3")

	resCheck := m.Run("roadmap", "check", "--file", path)
	require.Equal(t, 0, resCheck.Code)
	assert.Contains(t, resCheck.Stdout, "cards=3")
}

// The reader's probe: adding a card whose id is already in the roadmap is
// refused at exit 2 and the file is not written, so check still passes.
func TestRoadmapAddRefusesADuplicateIDAndWritesNothing(t *testing.T) {
	t.Parallel()
	path := writeRoadmap(t, sampleRoadmapCLI)
	briefDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(briefDir, "c-2.md"), []byte("THE TASK. Again.\n"), 0644))
	m := workMain(unreachable(t))

	res := m.Run("roadmap", "add", "--file", path, "--stream", "stream-s", "--brief-dir", briefDir)
	require.Equal(t, 2, res.Code, res.Stdout)
	assert.Contains(t, res.Stderr, `ROADMAP-ADD REFUSED: card "c-2" is already in the roadmap`)
	assert.Equal(t, sampleRoadmapCLI, testkit.ReadFile(t, path))
	require.Equal(t, 0, m.Run("roadmap", "check", "--file", path).Code)
}

// A verb that writes refuses a file check already rejects.
func TestRoadmapVerbsRefuseAFileCheckRejects(t *testing.T) {
	t.Parallel()
	text := strings.Replace(sampleRoadmapCLI, ":cards 2", ":cards 5", 1)
	path := writeRoadmap(t, text)
	res := workMain(unreachable(t)).Run("roadmap", "remove", "--file", path, "--id", "c-1")
	require.Equal(t, 2, res.Code)
	assert.Contains(t, res.Stderr, "fails check")
	assert.Contains(t, res.Stderr, "run: nova-work roadmap check --file "+path)
	assert.Equal(t, text, testkit.ReadFile(t, path))
}

func TestRoadmapNote(t *testing.T) {
	t.Parallel()
	m := workMain(unreachable(t))
	for _, text := range []string{"Extra reading note", "help", "-h", "--dry-run note"} {
		path := writeRoadmap(t, sampleRoadmapCLI)
		res := m.Run("roadmap", "note", "--file", path, "--id", "c-1", "--text", text)
		require.Equal(t, 0, res.Code, "text %q: %s", text, res.Stderr)
		assert.Contains(t, res.Stdout, "ROADMAP-NOTE OK")
		assert.Contains(t, res.Stdout, "noted=1")
		assert.Contains(t, testkit.ReadFile(t, path), "Brief 1 text\n\n"+text+"\n", "text %q", text)
	}
}

// The verbs are the tool's: help and the banner name them, a group -h lists
// them, and a verb's -h gives its flags.
func TestRoadmapHelp(t *testing.T) {
	t.Parallel()
	m := workMain(unreachable(t))
	verbs := []string{"roadmap add", "roadmap remove", "roadmap pull", "roadmap check", "roadmap note"}

	for _, args := range [][]string{{"help"}, {"-h"}, {"roadmap", "-h"}} {
		res := m.Run(args...)
		require.Equal(t, 0, res.Code, "%v", args)
		for _, v := range verbs {
			assert.Contains(t, res.Stdout, "nova-work "+v+" --file", "%v names %s", args, v)
		}
	}

	resAddHelp := m.Run("roadmap", "add", "-h")
	require.Equal(t, 0, resAddHelp.Code)
	assert.Contains(t, resAddHelp.Stdout, "--stream")
	assert.Contains(t, resAddHelp.Stdout, "--brief-dir")
	assert.Contains(t, resAddHelp.Stdout, "effect: local write")

	positional := m.Run("roadmap", "remove", "--file", "x.sexp", "c-1")
	require.Equal(t, 2, positional.Code)
	assert.Contains(t, positional.Stderr, "ROADMAP-REMOVE REFUSED")
}
