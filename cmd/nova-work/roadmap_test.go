package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestRoadmapCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmapCLI), 0644))

	m := workMain(unreachable(t))

	// 1. Success check
	res := m.Run("roadmap", "check", path)
	require.Equal(t, 0, res.Code)
	assert.Contains(t, res.Stdout, "ROADMAP-CHECK OK")
	assert.Contains(t, res.Stdout, "cards=2")

	// 2. Success check with --json
	resJSON := m.Run("roadmap", "check", path, "--json")
	require.Equal(t, 0, resJSON.Code)
	var j map[string]any
	require.NoError(t, json.Unmarshal([]byte(resJSON.Stdout), &j))
	assert.Equal(t, "ok", j["result"].(map[string]any)["status"])

	// 3. Count mismatch -> Exit 1
	mismatched := strings.Replace(sampleRoadmapCLI, ":cards 2", ":cards 5", 1)
	require.NoError(t, os.WriteFile(path, []byte(mismatched), 0644))
	resFail := m.Run("roadmap", "check", path)
	require.Equal(t, 1, resFail.Code)
	assert.Contains(t, resFail.Stdout, "ROADMAP-CHECK FAILED")
	assert.Contains(t, resFail.Stderr, "count mismatch")
}

func TestRoadmapRemove(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmapCLI), 0644))

	m := workMain(unreachable(t))

	// Missing ID -> refuse
	resRefuse := m.Run("roadmap", "remove", path)
	require.Equal(t, 2, resRefuse.Code)
	assert.Contains(t, resRefuse.Stderr, "ROADMAP-REMOVE REFUSED")

	// Card not found -> refuse
	resMissing := m.Run("roadmap", "remove", path, "c-99")
	require.Equal(t, 2, resMissing.Code)
	assert.Contains(t, resMissing.Stderr, "not found")

	// Remove c-1
	res := m.Run("roadmap", "remove", path, "c-1")
	require.Equal(t, 0, res.Code)
	assert.Contains(t, res.Stdout, "ROADMAP-REMOVE OK")
	assert.Contains(t, res.Stdout, "removed=1 remaining=1")

	// Check passes on updated file
	resCheck := m.Run("roadmap", "check", path)
	require.Equal(t, 0, resCheck.Code)
}

func TestRoadmapPull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmapCLI), 0644))

	m := workMain(unreachable(t))

	outDir := filepath.Join(dir, "briefs")
	res := m.Run("roadmap", "pull", path, "c-1", "--out", outDir)
	require.Equal(t, 0, res.Code)
	assert.Contains(t, res.Stdout, "ROADMAP-PULL OK")
	assert.Contains(t, res.Stdout, "pulled=1")

	// Verify c-1.md was written
	b1, err := os.ReadFile(filepath.Join(outDir, "c-1.md"))
	require.NoError(t, err)
	assert.Equal(t, "Brief 1 text\n", string(b1))

	// Verify roadmap has 1 card left and passes check
	resCheck := m.Run("roadmap", "check", path)
	require.Equal(t, 0, resCheck.Code)
	assert.Contains(t, resCheck.Stdout, "cards=1")
}

func TestRoadmapAdd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmapCLI), 0644))

	briefDir := filepath.Join(dir, "new-briefs")
	require.NoError(t, os.MkdirAll(briefDir, 0755))

	b := `REPO: mas-bandwidth/nova-tools
TIER: pro
DEPENDS-ON: c-1

THE TASK. New brief title.
`
	require.NoError(t, os.WriteFile(filepath.Join(briefDir, "c-3.md"), []byte(b), 0644))

	m := workMain(unreachable(t))

	res := m.Run("roadmap", "add", path, "--stream", "stream-s", "--brief-dir", briefDir)
	require.Equal(t, 0, res.Code)
	assert.Contains(t, res.Stdout, "ROADMAP-ADD OK")
	assert.Contains(t, res.Stdout, "added=1 total=3")

	resCheck := m.Run("roadmap", "check", path)
	require.Equal(t, 0, resCheck.Code)
	assert.Contains(t, resCheck.Stdout, "cards=3")
}

func TestRoadmapNote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "roadmap.sexp")
	require.NoError(t, os.WriteFile(path, []byte(sampleRoadmapCLI), 0644))

	m := workMain(unreachable(t))

	res := m.Run("roadmap", "note", path, "c-1", "--text", "Extra reading note")
	require.Equal(t, 0, res.Code)
	assert.Contains(t, res.Stdout, "ROADMAP-NOTE OK")
	assert.Contains(t, res.Stdout, "noted=1")

	// Verify brief in file was modified
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Extra reading note")
}

func TestRoadmapHelp(t *testing.T) {
	m := workMain(unreachable(t))

	resGroup := m.Run("roadmap", "-h")
	require.Equal(t, 0, resGroup.Code)
	assert.Contains(t, resGroup.Stdout, "roadmap add")
	assert.Contains(t, resGroup.Stdout, "roadmap remove")
	assert.Contains(t, resGroup.Stdout, "roadmap pull")
	assert.Contains(t, resGroup.Stdout, "roadmap check")
	assert.Contains(t, resGroup.Stdout, "roadmap note")

	resAddHelp := m.Run("roadmap", "add", "-h")
	require.Equal(t, 0, resAddHelp.Code)
	assert.Contains(t, resAddHelp.Stdout, "--stream")
	assert.Contains(t, resAddHelp.Stdout, "--brief-dir")
}
